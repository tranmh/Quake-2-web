package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/assets/pak"
)

// long reports whether the long variants run (Q2_AGENT_LONG=1).
func long() bool { return os.Getenv("Q2_AGENT_LONG") == "1" }

// gameCap bounds an episode's game time: demo1 takes one to two minutes
// of game time and 2-4 s of wall time, ten times that under -race, where
// the runs stop after 10 s of game time (the episode watchdog) unless
// Q2_AGENT_LONG=1.
func gameCap(normal time.Duration) time.Duration {
	if raceEnabled && !long() {
		return 10 * time.Second
	}
	return normal
}

func runDemo1(t *testing.T, fs *pak.FS, cfg Config) (*Runner, *metrics.RunSummary, error) {
	t.Helper()
	cfg.FS, cfg.Maps = fs, []string{"demo1"}
	if cfg.OutDir == "" {
		cfg.OutDir = t.TempDir()
	}
	cfg.Logf = func(format string, args ...any) { t.Logf(format, args...) }
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Run(context.Background())
	if s == nil {
		t.Fatalf("no summary: %v", err)
	}
	return r, s, err
}

// rateOver returns the rate (per second) of the times ts (ms, sorted) over
// their stretches, split where a gap exceeds gap ms (each request counts
// for 100 ms), and the stretches' total length (s).
func rateOver(ts []int64, gap int64) (float64, float64) {
	var active int64
	for i := 0; i < len(ts); {
		j := i
		for j+1 < len(ts) && ts[j+1]-ts[j] <= gap {
			j++
		}
		active += ts[j] - ts[i] + 100
		i = j + 1
	}
	if active == 0 {
		return 0, 0
	}
	return float64(len(ts)) / (float64(active) / 1000), float64(active) / 1000
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// bodies returns the decoded bodies of the events of type typ.
func bodies[T any](t *testing.T, evs []trace.Event, typ string) []T {
	t.Helper()
	var out []T
	for i := range evs {
		if evs[i].Type != typ {
			continue
		}
		var b T
		if err := evs[i].DecodeBody(&b); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func mustValid(t *testing.T, dir string, opt ValidateOptions) *ValidateReport {
	t.Helper()
	vr, err := Validate(dir, opt)
	if err != nil {
		t.Fatal(err)
	}
	if !vr.OK() {
		t.Fatalf("validate %s: %v", dir, vr.Errors)
	}
	return vr
}

// TestRunDemo1Scripted is the runner's main integration test: a seeded
// lockstep run of demo1 with the scripted backend writes the run
// directory, its run.json validates and recomputes from the trace, the
// fast lane asks about 10 times a second in combat, every demo plays, and
// both replay modes repeat the episode exactly.
func TestRunDemo1Scripted(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	limit := gameCap(0)
	r, s, err := runDemo1(t, fs, Config{Seed: 1, Trace: TraceFull, Record: true, EpisodeTimeout: limit})
	if err != nil {
		t.Fatal(err)
	}
	if limit == 0 && (s.Outcome != OutcomeCompleted || s.Totals.LevelsCompleted != 1) {
		t.Fatalf("demo1 not completed: %s (%s)", s.Outcome, s.Reason)
	}

	// the layout
	dir := r.Dir()
	if filepath.Base(dir) != r.ID() || !ValidRunID(r.ID()) {
		t.Fatalf("run dir %s", dir)
	}
	for _, f := range []string{RunFile, "ep-000/" + EpisodeFile, "ep-000/" + TraceFile, "ep-000/" + LogFile, "ep-000/demos/00-demo1.dm2"} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || st.Size() == 0 {
			t.Fatalf("%s: %v", f, err)
		}
	}
	logText, _ := os.ReadFile(filepath.Join(dir, "ep-000", LogFile))
	if !strings.Contains(string(logText), "campaign: level 0: demo1") {
		t.Errorf("log.txt:\n%s", logText)
	}

	// run.json: the schema, valid traces and demos, and what the traces
	// recompute to
	var onDisk metrics.RunSummary
	readJSON(t, filepath.Join(dir, RunFile), &onDisk)
	if onDisk.Schema != metrics.Schema || onDisk.Run != r.ID() || onDisk.Backend != BackendScripted || onDisk.Session != SessionLockstep ||
		len(onDisk.Maps) != 1 || onDisk.Maps[0] != "demo1" || len(onDisk.EpisodeSeeds) != 1 || onDisk.EpisodeSeeds[0] != 1 {
		t.Fatalf("run.json header %+v", onDisk)
	}
	vr := mustValid(t, dir, ValidateOptions{})
	if len(vr.Demos) == 0 || vr.Demos[0].Stats.Map != "maps/demo1.bsp" || vr.Demos[0].Stats.Frames < 100 || !vr.Demos[0].Stats.Terminated {
		t.Fatalf("demos %+v", vr.Demos)
	}
	re, err := SummarizeDir(dir, SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := trace.Canonical(re)
	b, _ := trace.Canonical(onDisk)
	if !bytes.Equal(a, b) {
		t.Fatalf("summarize differs from run.json:\n%s\n%s", a, b)
	}
	g := onDisk.Gate
	if g == nil || g.Passed || onDisk.ModelDriven || g.ModelBackend || g.Basis != metrics.GateTicks || onDisk.Ticks == nil {
		t.Fatalf("a scripted run is not model-driven: gate %+v", g)
	}
	if fp := onDisk.Ticks.Fields["fire_policy"]; fp.Model != 0 || fp.Scripted == 0 {
		t.Fatalf("scripted provenance %+v", fp)
	}

	// the trace: a cmds event per active step, the decision rate in combat
	evs, err := RunEvents(dir)
	if err != nil {
		t.Fatal(err)
	}
	steps := bodies[trace.Cmds](t, evs, trace.TypeCmds)
	if len(steps) < 100 {
		t.Fatalf("%d cmds events", len(steps))
	}
	for _, c := range steps {
		if len(c.Cmds) != 4 || c.Cmds[0].Msec != 25 {
			t.Fatalf("step %d: %+v", c.Step, c.Cmds)
		}
	}
	// the fast lane asks while enemies are in view or remembered: at 10 Hz
	// while one is visible (or a projectile flies at the bot), at 5 Hz
	// while they are only remembered; the full trace's lane states tell
	// which
	var fastAt, urgentAt []int64
	slow := 0
	for _, d := range bodies[trace.Decision](t, evs, trace.TypeDecision) {
		switch {
		case d.Lane == "fast" && d.Combat:
			fastAt = append(fastAt, d.SnapGMs)
			var st decide.State
			if err := json.Unmarshal(d.State, &st); err != nil {
				t.Fatalf("fast decision %d state: %v", d.Req, err)
			}
			urgent := len(st.Incoming) > 0
			for _, en := range st.Enemies {
				urgent = urgent || en.Visible
			}
			if urgent {
				urgentAt = append(urgentAt, d.SnapGMs)
			}
		case d.Lane == "slow":
			slow++
		}
	}
	fastRate, fastS := rateOver(fastAt, 1000)
	urgentRate, urgentS := rateOver(urgentAt, 300)
	gameS := float64(onDisk.GameMs) / 1000
	t.Logf("fast lane: %d decisions in %.1f s of activity (%.1f/s), %d in %.1f s with a visible enemy (%.1f/s); %d slow in %.1f s",
		len(fastAt), fastS, fastRate, len(urgentAt), urgentS, urgentRate, slow, gameS)
	if urgentS > 5 {
		if urgentRate < 8.5 || urgentRate > 10.5 {
			t.Errorf("fast lane at %.1f decisions per game second with a visible enemy, want about 10", urgentRate)
		}
		if fastRate < 5 || fastRate > 10.5 {
			t.Errorf("fast lane at %.1f decisions per game second in combat", fastRate)
		}
	} else if limit == 0 {
		t.Errorf("only %.1f s of combat with a visible enemy on demo1", urgentS)
	}
	if rate := float64(slow) / gameS; rate < 1.5 || rate > 3.5 {
		t.Errorf("slow lane at %.1f decisions per game second", rate)
	}

	// episode.json
	var ep EpisodeReport
	readJSON(t, filepath.Join(dir, "ep-000", EpisodeFile), &ep)
	if ep.Index != 0 || ep.Seed != 1 || len(ep.Demos) == 0 || ep.Decide == nil || ep.Decide.Requests["fast"] == 0 || len(ep.Routes) == 0 ||
		ep.Routes[0].Map != "demo1" || ep.Ticks == nil {
		t.Fatalf("episode.json %+v", ep)
	}

	// replay: the recorded usercmds, then the recorded responses, repeat
	// the episode exactly
	modes := []string{ReplayActions, ReplayResponses}
	if testing.Short() || raceEnabled && !long() {
		modes = modes[:1]
	}
	for _, mode := range modes {
		rep, err := Replay(context.Background(), ReplayConfig{Trace: filepath.Join(dir, "ep-000", TraceFile), Mode: mode, Strict: true, FS: fs})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Diverged() || rep.Err != "" || rep.Matched != rep.Recorded || rep.Replayed != rep.Recorded || rep.Responses.Missing != 0 {
			js, _ := json.MarshalIndent(rep.Divergence, "", " ")
			t.Fatalf("%s replay diverged: %+v\n%s", mode, rep, js)
		}
		if mode == ReplayActions && (rep.Cmds != 4*len(steps) || rep.CmdMismatches != 0) {
			t.Fatalf("actions replay compared %d usercmds (%d recorded steps), %d differ", rep.Cmds, len(steps), rep.CmdMismatches)
		}
		t.Logf("%s replay: %d events equal, %d usercmds, responses %+v", mode, rep.Matched, rep.Cmds, rep.Responses)
	}
}

// TestReplayDetectsDivergence edits a recorded trace (one usercmd and the
// seed) and expects both replay modes to name the first difference.
func TestReplayDetectsDivergence(t *testing.T) {
	if testing.Short() || raceEnabled && !long() {
		t.Skip("a lockstep run and two replays (about 2 s, 20 s under -race: Q2_AGENT_LONG=1)")
	}
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	r, _, err := runDemo1(t, fs, Config{Seed: 3, Trace: TraceDigest, EpisodeTimeout: 12 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(r.Dir(), "ep-000", TraceFile)
	evs, err := trace.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	// a different usercmd at the 60th step: actions mode sends it, the
	// game departs from the recording
	edited := 0
	for i := range evs {
		if evs[i].Type != trace.TypeCmds {
			continue
		}
		var b trace.Cmds
		_ = evs[i].DecodeBody(&b)
		if b.Step == 60 {
			b.Cmds[0].Side, b.Cmds[1].Side, b.Cmds[2].Side, b.Cmds[3].Side = 400, 400, 400, 400
			b.Cmds[0].Up = 400
			evs[i] = trace.Event{V: evs[i].V, Type: evs[i].Type, Run: evs[i].Run, Ep: evs[i].Ep, Seq: evs[i].Seq, Wall: evs[i].Wall,
				GMs: evs[i].GMs, Lvl: evs[i].Lvl, Map: evs[i].Map, SF: evs[i].SF, Body: b}
			edited++
		}
	}
	if edited != 1 {
		t.Fatalf("step 60 not recorded (%d)", edited)
	}
	path := filepath.Join(t.TempDir(), "edited.jsonl.gz")
	writeTrace(t, path, evs)
	rep, err := Replay(context.Background(), ReplayConfig{Trace: path, Mode: ReplayActions, Strict: true, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	d := rep.Divergence
	if d == nil || d.Kind != "cmd" || d.Step != 60 || d.Cmd != 0 || len(d.Diff) == 0 || !rep.Diverged() {
		js, _ := json.MarshalIndent(rep, "", " ")
		t.Fatalf("actions replay of an edited usercmd: %s", js)
	}
	js, _ := json.Marshal(d.Diff)
	t.Logf("actions: %s step %d: %s", d.Kind, d.Step, js)

	// responses mode, lenient, on the original recording with one level
	// outcome reason changed: the replay names that event alone
	if evs, err = trace.ReadFile(src); err != nil {
		t.Fatal(err)
	}
	for i := range evs {
		if evs[i].Type == trace.TypeLevelEnd {
			var b trace.LevelEnd
			_ = evs[i].DecodeBody(&b)
			b.Reason += " (edited)"
			evs[i] = trace.Event{V: evs[i].V, Type: evs[i].Type, Run: evs[i].Run, Ep: evs[i].Ep, Seq: evs[i].Seq, GMs: evs[i].GMs,
				Lvl: evs[i].Lvl, Map: evs[i].Map, SF: evs[i].SF, Body: b}
		}
	}
	writeTrace(t, path, evs)
	rep, err = Replay(context.Background(), ReplayConfig{Trace: path, Mode: ReplayResponses, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	d = rep.Divergence
	if d == nil || d.Kind != "event" || d.Type != trace.TypeLevelEnd || rep.Mismatches != 1 || len(d.Diff) != 1 || d.Diff[0].Path != "$.reason" {
		js, _ := json.MarshalIndent(rep, "", " ")
		t.Fatalf("responses replay of an edited level_end: %s", js)
	}
}

func writeTrace(t *testing.T, path string, evs []trace.Event) {
	t.Helper()
	fs, err := trace.CreateFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if err := fs.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestRunMock runs demo1 on the jev client against the in-process noisy
// fake server: the run is model-backed and its run.json carries the
// provenance, latency, token and cost numbers.
func TestRunMock(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	r, s, err := runDemo1(t, fs, Config{Backend: BackendMock, Seed: 1, EpisodeTimeout: gameCap(0)})
	if err != nil {
		t.Fatal(err)
	}
	mustValid(t, r.Dir(), ValidateOptions{})
	g := s.Gate
	if s.Model != jev.DefaultModel || g == nil || !g.ModelBackend || g.Basis != metrics.GateTicks || s.Ticks == nil {
		t.Fatalf("model %q gate %+v", s.Model, g)
	}
	a := s.API
	if a.Calls == 0 || a.OK == 0 || a.InputTokens == 0 || a.CostUSD <= 0 || a.LatencyMs.P50 != float64(DefaultModelLatency/time.Millisecond) {
		t.Fatalf("api %+v", a)
	}
	if s.Decisions.Comparisons == 0 || s.Decisions.Disagreements == 0 || s.Decisions.DisagreementRate <= 0 {
		t.Fatalf("no model/script disagreement measured: %+v", s.Decisions)
	}
	if m := s.Ticks.Fields["mode"]; m.Model == 0 || m.ModelShare < 0.5 {
		t.Fatalf("mode provenance %+v", m)
	}
	if len(g.ModelShares) == 0 {
		t.Fatalf("gate shares %+v", g)
	}
	// the gate with a threshold the run cannot miss, and one it cannot meet
	if pass, _ := SummarizeDir(r.Dir(), SummarizeOptions{MinModelShare: 0.01, MaxStaleRate: 1}); !pass.ModelDriven {
		t.Fatalf("gate at 1%%: %+v", pass.Gate)
	}
	vr, _ := Validate(r.Dir(), ValidateOptions{MinModelShare: 1})
	if vr.OK() || vr.Gate == nil || vr.Gate.Passed {
		t.Fatalf("a 100%% gate passed: %+v", vr.Gate)
	}
	t.Logf("mock: gate %+v; api p50 %.0f ms, %d tokens, $%.6f, disagreement %.2f", g, a.LatencyMs.P50, a.InputTokens, a.CostUSD,
		s.Decisions.DisagreementRate)

	// the mock backend is deterministic in lockstep: its trace replays,
	// including the requests still in flight when the episode ended
	// (never recorded: they take the run's simulated latency)
	if testing.Short() {
		return
	}
	rep, err := Replay(context.Background(), ReplayConfig{Trace: filepath.Join(r.Dir(), "ep-000", TraceFile), Mode: ReplayResponses,
		Strict: true, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Diverged() || rep.Err != "" || rep.ResponseDivergence != nil || rep.Backend != BackendMock {
		js, _ := json.MarshalIndent(rep, "", " ")
		t.Fatalf("mock replay: %s", js)
	}
	t.Logf("mock replay: %d events equal, %d requests in flight at the end", rep.Matched, rep.InFlight)
}

// TestRunAblations runs the constant and random backends (the mechanics:
// their answers are acted on as the backend's, and the run is valid).
func TestRunAblations(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	for _, be := range []string{BackendConstant, BackendRandom} {
		t.Run(be, func(t *testing.T) {
			t.Parallel()
			r, s, err := runDemo1(t, fs, Config{Backend: be, Seed: 2, EpisodeTimeout: gameCap(30 * time.Second)})
			if err != nil {
				t.Fatal(err)
			}
			mustValid(t, r.Dir(), ValidateOptions{})
			if s.Backend != be || s.ModelDriven || s.Gate.ModelBackend {
				t.Fatalf("summary %+v", s)
			}
			evs, _ := RunEvents(r.Dir())
			values := map[string]map[string]bool{}
			n := 0
			for _, d := range bodies[trace.Decision](t, evs, trace.TypeDecision) {
				if !requestLane(d.Lane) {
					continue
				}
				n++
				if d.Backend != be || d.Model != be {
					t.Fatalf("decision of backend %q model %q", d.Backend, d.Model)
				}
				for _, f := range d.Fields {
					if f.Source != trace.SourceModel {
						continue
					}
					if values[f.Name] == nil {
						values[f.Name] = map[string]bool{}
					}
					values[f.Name][f.Value] = true
				}
			}
			if n == 0 {
				t.Fatal("no decisions")
			}
			switch be {
			case BackendConstant:
				for name, want := range map[string]string{"fire_policy": "hold", "movement": "advance", "mode": "fight", "danger": "0.00"} {
					if len(values[name]) != 1 || !values[name][want] {
						t.Errorf("constant %s: %v, want only %q", name, values[name], want)
					}
				}
			case BackendRandom:
				if len(values["mode"]) < 3 || len(values["danger"]) < 3 {
					t.Errorf("random answers do not vary: %v", values)
				}
			}
		})
	}
}

// TestRunBudget degrades a mock run along the ladder until it is
// exhausted (scripted only, not model-driven), and stops one whose budget
// policy is stop.
func TestRunBudget(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	t.Run("fallback", func(t *testing.T) {
		t.Parallel()
		r, s, err := runDemo1(t, fs, Config{Backend: BackendMock, Seed: 1, Budget: budget.Limits{Queries: 12}, EpisodeTimeout: gameCap(40 * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		mustValid(t, r.Dir(), ValidateOptions{})
		evs, _ := RunEvents(r.Dir())
		var rates []float64
		only := false
		for _, b := range bodies[trace.Budget](t, evs, trace.TypeBudget) {
			rates = append(rates, b.RateHz)
			only = only || b.ScriptedOnly
		}
		if len(rates) < 3 || rates[0] != budget.DefaultFullHz || !only {
			t.Fatalf("budget events: rates %v, scripted only %v", rates, only)
		}
		for i := 1; i < len(rates); i++ {
			if rates[i] > rates[i-1] {
				t.Fatalf("rate went up: %v", rates)
			}
		}
		refused := 0
		for _, d := range bodies[trace.Decision](t, evs, trace.TypeDecision) {
			if strings.HasPrefix(d.Err, "budget: ") {
				refused++
			}
		}
		if refused == 0 || s.API.Calls > 12+8 {
			t.Fatalf("%d refused, %d api calls for a cap of 12", refused, s.API.Calls)
		}
		if s.ModelDriven || s.Gate == nil || !s.Gate.BudgetExhausted || s.Budget == nil || !s.Budget.ScriptedOnly {
			t.Fatalf("an exhausted run: gate %+v budget %+v", s.Gate, s.Budget)
		}
		var ep EpisodeReport
		readJSON(t, filepath.Join(r.Dir(), "ep-000", EpisodeFile), &ep)
		if ep.Decide.BudgetRefused["fast"]+ep.Decide.BudgetRefused["slow"] != refused {
			t.Fatalf("episode refused %v, trace %d", ep.Decide.BudgetRefused, refused)
		}
		t.Logf("budget: rates %v, %d refused, %d api calls", rates, refused, s.API.Calls)
	})
	t.Run("stop", func(t *testing.T) {
		t.Parallel()
		_, s, err := runDemo1(t, fs, Config{Backend: BackendMock, Seed: 1, Budget: budget.Limits{Queries: 6, OnExhausted: budget.Stop},
			RequireComplete: true, EpisodeTimeout: gameCap(40 * time.Second)})
		if !errors.Is(err, ErrIncomplete) || s.Outcome != OutcomeAborted || !strings.Contains(s.Reason, "budget exhausted") {
			t.Fatalf("stop: %v, %s (%s)", err, s.Outcome, s.Reason)
		}
	})
}

// TestAccountSharedByRunners runs two mock runs at once on one account:
// its limiter (frozen clock: no refill) lets 20 requests through in all.
func TestAccountSharedByRunners(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	frozen := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	acct, err := budget.NewAccount(context.Background(), budget.AccountConfig{QPS: 1, Burst: 20, DailyUSD: 100,
		Now: func() time.Time { return frozen }})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	sums := make([]*metrics.RunSummary, 2)
	errs := make([]error, 2)
	for i := range sums {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cfg := Config{FS: fs, Maps: []string{"demo1"}, Backend: BackendMock, Seed: uint64(10 + i), Account: acct, OutDir: t.TempDir(),
				EpisodeTimeout: gameCap(12 * time.Second)}
			sums[i], errs[i] = Run(context.Background(), cfg)
		}(i)
	}
	wg.Wait()
	ok := 0
	for i, s := range sums {
		if errs[i] != nil || s == nil {
			t.Fatalf("run %d: %v", i, errs[i])
		}
		ok += s.API.OK
	}
	st := acct.Stats()
	if st.Allowed != 20 || st.Refused == 0 || ok > 20 || ok == 0 {
		t.Fatalf("account %+v, %d answers in both runs", st, ok)
	}
	if spent, _ := acct.Daily(); spent <= 0 {
		t.Fatalf("daily spend %v", spent)
	}
	t.Logf("account %+v; answers %d + %d", st, sums[0].API.OK, sums[1].API.OK)
}

// TestRunInProc plays a few seconds of demo1 in a realtime session (the
// host instance, the wall-paced session and its cleanup, the trace), and
// shows that a realtime trace does not replay.
func TestRunInProc(t *testing.T) {
	if testing.Short() {
		t.Skip("realtime: about 4 s of wall time")
	}
	fs := sessiontest.DemoFS(t)
	r, s, err := runDemo1(t, fs, Config{Session: SessionInProc, Seed: 1, Record: true, EpisodeTimeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	vr := mustValid(t, r.Dir(), ValidateOptions{})
	if s.Session != SessionInProc || s.Outcome != OutcomeFailed || !strings.Contains(s.Reason, "episode watchdog") ||
		vr.Types[trace.TypeCmds] < 10 || vr.Types[trace.TypeDecision] == 0 || len(vr.Demos) == 0 {
		t.Fatalf("inproc run: %s (%s), types %v, %d demos", s.Outcome, s.Reason, vr.Types, len(vr.Demos))
	}
	_, err = Replay(context.Background(), ReplayConfig{Trace: filepath.Join(r.Dir(), "ep-000", TraceFile), FS: fs})
	if err == nil || !strings.Contains(err.Error(), "only lockstep runs replay") {
		t.Fatalf("replay of a realtime trace: %v", err)
	}
}
