package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/runner"
	"quake2web/server/internal/assets/pak"
)

// runOptions are the run command's settings beyond runner.Config.
type runOptions struct {
	pak        string
	json       bool
	accountQPS float64
	gate       bool // -min-model-share given: exit 1 unless model-driven
}

// runConfig parses the run command's flags into a runner configuration
// (without the game data).
func runConfig(e *env, args []string) (runner.Config, runOptions, error) {
	fs := newFlags("run", e)
	maps := fs.String("maps", "", "the campaign's first visits to play, by map: demo1 plays demo1 alone, demo1,demo2,demo3 the whole campaign (\"\": the whole campaign)")
	camp := fs.String("campaign", "", "the campaign: its campaign.json or the route tables' directory (default: fixtures/agent/routes)")
	skill := fs.Int("skill", -1, "game skill 0..3 (-1: the campaign's)")
	backend := fs.String("backend", runner.BackendScripted, "decision backend: "+strings.Join(runner.Backends(), "|"))
	sess := fs.String("session", runner.SessionLockstep, "session: lockstep (virtual clock) or inproc (realtime)")
	simLat := fs.String("sim-latency", "", "simulated backend latency in lockstep: 212ms, or samples 80ms,150ms,300ms (default: 212ms for jev and mock, none for the local backends, the recorded ones for replay)")
	episodes := fs.Int("episodes", 1, "episodes to play (episode i plays with seed+i)")
	seed := fs.Uint64("seed", 1, "the run's seed")
	out := fs.String("out", "runs", "where the run directory <id> is made")
	traceMode := fs.String("trace", "full", "lane state in the decision events: full, digest or every:N")
	record := fs.Bool("record", true, "record a .dm2 demo per level attempt")
	budgetUSD := fs.Float64("budget-usd", 0, "the run's spend cap in USD (0: none)")
	budgetQ := fs.Int("budget-queries", 0, "the run's cap on backend requests (0: none)")
	maxQPS := fs.Float64("max-qps", 0, "the run's cap on requests per second of game time (0: none)")
	onEx := fs.String("on-exhausted", string(budget.Fallback), "at the end of the budget: fallback (scripted only) or stop")
	accountQPS := fs.Float64("account-qps", 0, "the API account's request rate, wall clock, shared by all the run's calls (0: none)")
	reqComplete := fs.Bool("require-complete", false, "exit 1 unless every episode completes (to the campaign's end when it is played whole)")
	minShare := fs.Float64("min-model-share", 0, "provenance gate: exit 1 unless the run is model-driven with at least this share of target, fire_policy and mode from the model (0: not enforced; the summary's verdict uses 0.7)")
	maxStale := fs.Float64("max-stale-rate", 0, "the gate's highest stale-answer rate (0: 0.15)")
	asJSON := fs.Bool("json", false, "print run.json instead of a summary")
	pakPath := fs.String("pak", defaultPak(e.getenv), "the game data (the demo pak)")
	navDir := fs.String("nav-dir", "", "the nav graph cache (default: assets/nav)")
	cheats := fs.String("cheats", "", "client commands sent at every level entry, comma separated (god,notarget: a test run, not a benchmark)")
	levelTO := fs.Duration("level-timeout", 0, "a level's game-time watchdog (0: 20m)")
	epTO := fs.Duration("episode-timeout", 0, "an episode's game-time watchdog (0: 90m)")
	maxDeaths := fs.Int("max-deaths", 0, "deaths allowed per level (0: 5, -1: none)")
	replayTrace := fs.String("replay-trace", "", "the recorded trace -backend replay answers from")
	verbose := fs.Bool("v", false, "copy the episodes' log lines to stderr")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return runner.Config{}, runOptions{}, err
	}
	if len(pos) > 0 {
		return runner.Config{}, runOptions{}, fmt.Errorf("%w: unexpected argument %q", errUsage, pos[0])
	}
	usageErr := func(err error) (runner.Config, runOptions, error) {
		return runner.Config{}, runOptions{}, fmt.Errorf("%w: %v", errUsage, err)
	}
	cfg := runner.Config{Maps: splitList(*maps), Backend: *backend, Session: *sess, Episodes: *episodes, Seed: *seed, OutDir: *out,
		Record: *record, RequireComplete: *reqComplete, MinModelShare: *minShare, MaxStaleRate: *maxStale, NavDir: *navDir,
		EntryCommands: splitList(*cheats), LevelTimeout: *levelTO, EpisodeTimeout: *epTO, MaxDeaths: *maxDeaths,
		ReplayTrace: *replayTrace, Verbose: *verbose}
	if *camp != "" {
		dir := *camp
		if st, err := os.Stat(dir); err == nil && !st.IsDir() {
			dir = filepath.Dir(dir)
		}
		cfg.RoutesDir = dir
	}
	if *skill >= 0 {
		cfg.Skill = skill
	}
	if cfg.SimLatency, err = runner.ParseLatency(*simLat); err != nil {
		return usageErr(err)
	}
	if cfg.Trace, err = runner.ParseTrace(*traceMode); err != nil {
		return usageErr(err)
	}
	pol, err := budget.ParsePolicy(*onEx)
	if err != nil {
		return usageErr(err)
	}
	cfg.Budget = budget.Limits{USD: *budgetUSD, Queries: *budgetQ, MaxQPS: *maxQPS, OnExhausted: pol}
	known := false
	for _, b := range runner.Backends() {
		known = known || b == cfg.Backend
	}
	switch {
	case !known:
		return usageErr(fmt.Errorf("unknown backend %q", cfg.Backend))
	case cfg.Session != runner.SessionLockstep && cfg.Session != runner.SessionInProc:
		return usageErr(fmt.Errorf("unknown session %q", cfg.Session))
	case cfg.Episodes < 1:
		return usageErr(fmt.Errorf("-episodes %d", cfg.Episodes))
	case *skill > 3:
		return usageErr(fmt.Errorf("-skill %d not in 0..3", *skill))
	case *minShare < 0 || *minShare > 1 || *maxStale < 0 || *maxStale > 1:
		return usageErr(errors.New("-min-model-share and -max-stale-rate are shares in 0..1"))
	case *budgetUSD < 0 || *budgetQ < 0 || *maxQPS < 0 || *accountQPS < 0:
		return usageErr(errors.New("negative budget"))
	case cfg.Backend == runner.BackendReplay && cfg.ReplayTrace == "":
		return usageErr(errors.New("-backend replay needs -replay-trace"))
	}
	// the key comes from the environment only
	cfg.Jev = jev.FromEnv(e.getenv)
	return cfg, runOptions{pak: *pakPath, json: *asJSON, accountQPS: *accountQPS, gate: *minShare > 0}, nil
}

// openPak opens the game data.
func openPak(path string) (*pak.FS, error) {
	p, err := pak.Open(path)
	if err != nil {
		return nil, fmt.Errorf("game data: %w (make demo fetches the demo pak; or pass -pak)", err)
	}
	fs := &pak.FS{}
	fs.AddPak(p)
	return fs, nil
}

// signalContext is cancelled by an interrupt (the run ends as aborted,
// with its files written).
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func runRun(e *env, args []string) error {
	cfg, o, err := runConfig(e, args)
	if err != nil {
		return err
	}
	if cfg.Backend == runner.BackendJev && cfg.Jev.APIKey.Reveal() == "" {
		return fmt.Errorf("%w: -backend jev needs %s in the environment", errUsage, jev.EnvAPIKey)
	}
	fs, err := openPak(o.pak)
	if err != nil {
		return err
	}
	defer fs.Close()
	cfg.FS = fs
	cfg.Logf = func(format string, args ...any) { fmt.Fprintf(e.stderr, format+"\n", args...) }
	ctx, stop := signalContext()
	defer stop()
	if o.accountQPS > 0 {
		acct, err := budget.NewAccount(ctx, budget.AccountConfig{QPS: o.accountQPS})
		if err != nil {
			return err
		}
		cfg.Account = acct
	}
	r, err := runner.New(cfg)
	if err != nil {
		return err
	}
	s, runErr := r.Run(ctx)
	if o.json {
		writeJSON(e.stdout, s)
	} else {
		printSummary(e.stdout, s, r.Dir())
	}
	if runErr != nil {
		fmt.Fprintf(e.stderr, "q2bot run: %v\n", runErr)
		return errFailed
	}
	if o.gate && !s.ModelDriven {
		reasons := "no gate"
		if s.Gate != nil {
			reasons = strings.Join(s.Gate.Reasons, "; ")
		}
		fmt.Fprintf(e.stderr, "q2bot run: provenance gate not met: %s\n", reasons)
		return errFailed
	}
	return nil
}

func writeJSON(w io.Writer, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(w, "{\"error\": %q}\n", err.Error())
		return
	}
	fmt.Fprintf(w, "%s\n", b)
}

// printSummary prints the essentials of a run summary.
func printSummary(w io.Writer, s *metrics.RunSummary, dir string) {
	fmt.Fprintf(w, "run %s: %s", s.Run, s.Outcome)
	if s.Reason != "" {
		fmt.Fprintf(w, " (%s)", s.Reason)
	}
	fmt.Fprintln(w)
	if dir != "" {
		fmt.Fprintf(w, "  dir         %s\n", dir)
	}
	model := ""
	if s.Model != "" {
		model = ", model " + s.Model
	}
	fmt.Fprintf(w, "  backend     %s%s, %s session, skill %d, seed %d, maps %s\n", s.Backend, model, s.Session, s.Skill, s.Seed,
		strings.Join(s.Maps, ","))
	t := s.Totals
	fmt.Fprintf(w, "  episodes    %d: %d of %d levels done, %d deaths, %d kills seen, %.1f s game (%.1f s in combat), %.1f s wall\n",
		len(s.Episodes), t.LevelsCompleted, t.Levels, t.Deaths, t.BotKills, float64(s.GameMs)/1000, float64(t.CombatMs)/1000,
		float64(s.WallMs)/1000)
	for _, ep := range s.Episodes {
		var lv []string
		for _, l := range ep.Levels {
			lv = append(lv, fmt.Sprintf("%s:%s", l.Map, l.Outcome))
		}
		fmt.Fprintf(w, "    ep %d     %s (%s) %s\n", ep.Index, ep.Outcome, ep.Reason, strings.Join(lv, " "))
	}
	a := s.API
	fmt.Fprintf(w, "  api         %d calls (%d ok, %d errors, %d stale), latency p50/p95/p99 %.0f/%.0f/%.0f ms, %.1f QPS in combat, %d input tokens, $%.6f\n",
		a.Calls, a.OK, a.Errors, a.Stale, a.LatencyMs.P50, a.LatencyMs.P95, a.LatencyMs.P99, a.CombatQPS, a.InputTokens, a.CostUSD)
	if s.Ticks != nil {
		names := make([]string, 0, len(s.Ticks.Fields))
		for n := range s.Ticks.Fields {
			names = append(names, n)
		}
		sort.Strings(names)
		var parts []string
		for _, n := range names {
			p := s.Ticks.Fields[n]
			if p.Decided > 0 {
				parts = append(parts, fmt.Sprintf("%s %.0f%%", n, 100*p.ModelShare))
			}
		}
		fmt.Fprintf(w, "  provenance  model share of decided ticks: %s; disagreement with the script %.1f%%\n", strings.Join(parts, ", "),
			100*s.Decisions.DisagreementRate)
	}
	if b := s.Budget; b != nil {
		fmt.Fprintf(w, "  budget      $%.6f of $%.2f, fast lane %g Hz, scripted only %v %s\n", b.SpentUSD, b.LimitUSD, b.RateHz, b.ScriptedOnly, b.Reason)
	}
	if g := s.Gate; g != nil {
		verdict := "model-driven"
		if !g.Passed {
			verdict = "not model-driven: " + strings.Join(g.Reasons, "; ")
		}
		fmt.Fprintf(w, "  gate        %s (min share %.2f, max stale %.2f, basis %s)\n", verdict, g.MinModelShare, g.MaxStaleRate, g.Basis)
	}
	if s.Errors > 0 {
		fmt.Fprintf(w, "  errors      %d, last: %s\n", s.Errors, s.LastError)
	}
}
