package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
)

// Run outcomes (run_end, RunSummary.Outcome): the episodes' outcomes
// (campaign.Outcome*) folded: completed when every episode completed,
// aborted when one was aborted (cancelled, or a budget that stops),
// failed otherwise.
const (
	OutcomeCompleted = campaign.OutcomeCompleted
	OutcomeFailed    = campaign.OutcomeFailed
	OutcomeAborted   = campaign.OutcomeAborted
)

// File names of a run directory: runs/<id>/{run.json, ep-NNN/{episode.json,
// trace.jsonl.gz, demos/NN-<map>.dm2, log.txt}}.
const (
	RunFile     = "run.json"
	EpisodeFile = "episode.json"
	TraceFile   = "trace.jsonl.gz"
	LogFile     = "log.txt"
	DemoDir     = "demos"
)

// EpisodeDir is the directory name of episode ep ("ep-003").
func EpisodeDir(ep int) string { return fmt.Sprintf("ep-%03d", ep) }

// ErrIncomplete is wrapped by Run's error when RequireComplete is set and
// the run did not complete.
var ErrIncomplete = errors.New("runner: the run did not complete")

// Runner is one run (see Run).
type Runner struct {
	cfg       Config
	id        string
	dir       string
	camp      *route.Campaign
	visits    []string // the maps of the visits played, in order
	stopAfter int
	skill     int
	lib       *campaign.Library
	maps      *sv.MapCache
	be        *backends
	bud       *budget.Budget

	bus    *trace.Bus
	col    *metrics.Collector
	router *fileRouter
	st     *stamp
	host   *host.Host

	runStart trace.Event
	logf     func(format string, args ...any)

	// replay hooks (Replay): the episode to start at, a usercmd feed,
	// strict replay responses, no run directory, extra bus sinks
	runnerHooks
	onEpisode func(*episodeRun)

	mu      sync.Mutex
	results []EpisodeReport
}

// EpisodeReport is episode.json: the metrics summary of the episode plus
// what only the run knows.
type EpisodeReport struct {
	metrics.EpisodeSummary
	// Victory: the campaign's terminal was reached.
	Victory bool  `json:"victory"`
	WallMs  int64 `json:"wall_ms"`
	// Routes is how far each level's route got.
	Routes []RouteProgress `json:"routes,omitempty"`
	// Demos are the episode's demo files (relative to the episode).
	Demos []string `json:"demos,omitempty"`
	// Decide counts the decision layer's requests.
	Decide *DecideStats `json:"decide,omitempty"`
	// Error is why the episode could not be played; Diagnostics the bot's
	// state when a level failed.
	Error       string `json:"error,omitempty"`
	Diagnostics string `json:"diagnostics,omitempty"`
}

// RouteProgress is one level of an episode as the campaign saw it.
type RouteProgress struct {
	Map       string `json:"map"`
	Visit     int    `json:"visit"`
	Route     string `json:"route"`
	Outcome   string `json:"outcome"`
	Steps     int    `json:"steps"`
	StepsDone int    `json:"steps_done"`
	WallMs    int64  `json:"wall_ms"`
}

// DecideStats are an episode's decision-layer counters.
type DecideStats struct {
	Ticks     int               `json:"ticks"`
	Requests  map[string]int    `json:"requests"`
	Scheduler map[string]LaneIO `json:"scheduler"`
	// BudgetRefused counts the requests the budget refused, by lane.
	BudgetRefused map[string]int `json:"budget_refused,omitempty"`
	OldLevel      int            `json:"old_level,omitempty"`
}

// LaneIO is a lane's scheduler counters.
type LaneIO struct {
	Submitted int `json:"submitted"`
	Dropped   int `json:"dropped"`
	Completed int `json:"completed"`
	Errors    int `json:"errors"`
	Timeouts  int `json:"timeouts"`
	Stale     int `json:"stale"`
}

// Run runs cfg's episodes and returns the run's summary (also written as
// run.json). The summary is returned whenever the run directory was made;
// the error is non-nil when an episode could not be played (a session or
// data failure), when RequireComplete is set and the run did not complete
// (ErrIncomplete), or when writing the run's files failed. ctx
// cancellation aborts the run (outcome aborted, no error).
func Run(ctx context.Context, cfg Config) (*metrics.RunSummary, error) {
	r, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return r.Run(ctx)
}

// New prepares a run: it checks the configuration, loads the campaign,
// makes the run id and directory and the backend (a jev client checks its
// key and base URL here).
func New(cfg Config) (*Runner, error) { return newRunner(cfg, runnerHooks{}) }

// runnerHooks adapt a Runner to a replay.
type runnerHooks struct {
	// epFrom is the first episode played (the recorded one).
	epFrom int
	// feed replaces the bot's usercmds (actions mode).
	feed func(step int64, i int, cmd shared.UserCmd) shared.UserCmd
	// strict makes the replay backend refuse diverging requests.
	strict bool
	// noFiles: no run directory at all.
	noFiles bool
	// sinks are extra bus sinks (the replay's comparator).
	sinks []trace.Sink
}

func newRunner(cfg Config, h runnerHooks) (*Runner, error) {
	if err := cfg.check(); err != nil {
		return nil, err
	}
	r := &Runner{cfg: cfg, logf: cfg.Logf, runnerHooks: h}
	if r.logf == nil {
		r.logf = func(string, ...any) {}
	}
	if err := r.prepare(); err != nil {
		return nil, err
	}
	id := cfg.RunID
	if id == "" {
		var err error
		if id, err = NewRunID(cfg.Now()); err != nil {
			return nil, err
		}
	}
	if !ValidRunID(id) {
		return nil, fmt.Errorf("%w: run id %q", ErrConfig, id)
	}
	r.id = id
	if !r.noFiles {
		r.dir = filepath.Join(cfg.OutDir, id)
		if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
			return nil, err
		}
		if err := os.Mkdir(r.dir, 0o755); err != nil {
			return nil, fmt.Errorf("runner: run directory: %w", err)
		}
	}
	be, err := newBackends(&r.cfg, r.logf)
	if err != nil {
		if r.dir != "" {
			_ = os.Remove(r.dir)
		}
		return nil, err
	}
	r.be = be
	r.initBus()
	return r, nil
}

// prepare loads the campaign, selects the visits and makes the library.
func (r *Runner) prepare() error {
	cfg := &r.cfg
	camp := cfg.Campaign
	if camp == nil {
		dir := cfg.RoutesDir
		if dir == "" {
			dir = campaign.DefaultRoutesDir()
		}
		c, err := route.Load(dir)
		if err != nil {
			return err
		}
		camp = c
	}
	visits, stop, err := selectVisits(camp, cfg.Maps)
	if err != nil {
		return err
	}
	r.visits, r.stopAfter = visits, stop
	r.skill = camp.Skill
	if cfg.Skill != nil {
		r.skill = *cfg.Skill
	}
	lib := cfg.Library
	if lib == nil {
		lib = campaign.NewLibrary(campaign.LibraryConfig{ReadFile: cfg.FS.ReadFile, Skill: r.skill, NavDir: cfg.NavDir, Logf: r.logf})
	} else if lib.Skill() != r.skill {
		return fmt.Errorf("%w: the library resolves skill %d, the run plays skill %d", ErrConfig, lib.Skill(), r.skill)
	}
	if r.skill != camp.Skill {
		// the route tables were written for the campaign's skill: check
		// them against the maps at this one (monsters and items differ)
		c := *camp
		c.Skill = r.skill
		if err := route.ValidateCampaign(&c, func(name string) (*mapdata.Map, error) { return lib.Map(name) }); err != nil {
			return fmt.Errorf("runner: the route tables do not hold at skill %d: %w", r.skill, err)
		}
		camp = &c
	}
	r.camp, r.lib = camp, lib
	r.maps = cfg.MapCache
	if r.maps == nil {
		r.maps = sv.NewMapCache()
	}
	if _, daily := accountDaily(cfg.Account); cfg.Budget.Enabled() || daily > 0 {
		r.bud = budget.New(cfg.Budget, cfg.Account)
	}
	return nil
}

// accountDaily returns an account's daily spend and cap (0, 0 without
// one).
func accountDaily(a *budget.Account) (spent, limit float64) {
	if a == nil {
		return 0, 0
	}
	return a.Daily()
}

// selectVisits returns the maps of the campaign's visits a run plays and
// the campaign's StopAfter for them: the longest run of visits from the
// start whose maps maps names (all of them: the whole campaign, to its
// terminal).
func selectVisits(camp *route.Campaign, maps []string) ([]string, int, error) {
	if len(camp.Tables) == 0 {
		return nil, 0, fmt.Errorf("%w: campaign %s has no route tables", ErrConfig, camp.Name)
	}
	all := make([]string, len(camp.Tables))
	for i, t := range camp.Tables {
		all[i] = t.Map
	}
	if len(maps) == 0 {
		return all, 0, nil
	}
	set := map[string]bool{}
	for _, m := range maps {
		set[strings.ToLower(strings.TrimSpace(m))] = true
	}
	if !set[strings.ToLower(all[0])] {
		return nil, 0, fmt.Errorf("%w: the maps %v must include the campaign's start %s", ErrConfig, maps, all[0])
	}
	n := 0
	for n < len(all) && set[strings.ToLower(all[n])] {
		n++
	}
	played := map[string]bool{}
	for _, m := range all[:n] {
		played[strings.ToLower(m)] = true
	}
	for m := range set {
		if !played[m] {
			return nil, 0, fmt.Errorf("%w: map %s is not among the campaign's first visits %v", ErrConfig, m, all[:n])
		}
	}
	if n == len(all) {
		return all, 0, nil
	}
	return all[:n], n, nil
}

func (r *Runner) initBus() {
	r.bus = trace.NewBus(r.id, r.cfg.Now)
	r.col = metrics.NewCollector()
	r.col.SetGate(metrics.GateConfig{MinModelShare: r.cfg.MinModelShare, MaxStaleRate: r.cfg.MaxStaleRate})
	r.router = &fileRouter{}
	r.st = &stamp{}
	r.bus.AddSink(r.col)
	r.bus.AddSink(r.router)
	r.bus.AddSink(r.st)
	for _, s := range r.sinks {
		r.bus.AddSink(s)
	}
}

// ID returns the run id.
func (r *Runner) ID() string { return r.id }

// Dir returns the run directory.
func (r *Runner) Dir() string { return r.dir }

// Bus returns the run's trace bus (Subscribe for a live view).
func (r *Runner) Bus() *trace.Bus { return r.bus }

// Episodes returns the reports of the episodes played so far.
func (r *Runner) Episodes() []EpisodeReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]EpisodeReport(nil), r.results...)
}

// Run plays the run (see the package function Run). A Runner runs once.
func (r *Runner) Run(ctx context.Context) (*metrics.RunSummary, error) {
	defer r.be.close()
	defer func() {
		if r.host != nil {
			r.host.Shutdown()
		}
	}()
	var errs []error
	outcome, reason := OutcomeCompleted, ""
	for ep := r.epFrom; ep < r.cfg.Episodes; ep++ {
		if ctx.Err() != nil {
			outcome, reason = OutcomeAborted, ctx.Err().Error()
			break
		}
		rep, err := r.episode(ctx, ep)
		if err != nil {
			errs = append(errs, fmt.Errorf("episode %d: %w", ep, err))
		}
		switch {
		case rep.Outcome == OutcomeAborted:
			if outcome != OutcomeAborted {
				outcome, reason = OutcomeAborted, fmt.Sprintf("episode %d: %s", ep, rep.Reason)
			}
		case rep.Outcome != OutcomeCompleted && outcome == OutcomeCompleted:
			outcome, reason = OutcomeFailed, fmt.Sprintf("episode %d: %s", ep, rep.Reason)
		}
		if outcome == OutcomeAborted {
			break
		}
	}
	if outcome == OutcomeCompleted {
		reason = r.completedReason()
	}
	r.publishRun(trace.TypeRunEnd, trace.RunEnd{Outcome: outcome, Reason: reason})
	if err := r.router.swap(nil); err != nil {
		errs = append(errs, fmt.Errorf("trace: %w", err))
	}
	if err := r.bus.Err(); err != nil {
		errs = append(errs, fmt.Errorf("trace: %w", err))
	}
	r.bus.Close()
	if r.cfg.Account != nil {
		if err := r.cfg.Account.Flush(context.WithoutCancel(ctx)); err != nil {
			errs = append(errs, fmt.Errorf("daily spend: %w", err))
		}
	}
	s := r.col.Summary()
	if !r.noFiles {
		if err := metrics.WriteJSON(filepath.Join(r.dir, RunFile), s); err != nil {
			errs = append(errs, err)
		}
	}
	if r.cfg.RequireComplete && s.Outcome != OutcomeCompleted {
		errs = append(errs, fmt.Errorf("%w: %s (%s)", ErrIncomplete, s.Outcome, s.Reason))
	}
	r.logf("run %s: %s (%s); model_driven %v", r.id, s.Outcome, s.Reason, s.ModelDriven)
	return &s, errors.Join(errs...)
}

// completedReason describes a completed run.
func (r *Runner) completedReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	wins := 0
	for _, e := range r.results {
		if e.Victory {
			wins++
		}
	}
	if r.stopAfter > 0 {
		return fmt.Sprintf("%d episodes through %s", len(r.results), strings.Join(r.visits, ", "))
	}
	return fmt.Sprintf("%d of %d episodes to %s", wins, len(r.results), r.camp.Terminal.Exit)
}

// publishRun publishes a run-level event (no episode, level or frame).
func (r *Runner) publishRun(typ string, body any) trace.Event {
	return r.bus.Publish(trace.Event{Type: typ, Body: body})
}

// runStartBody describes the run (run_start); its Config map is what a
// replay rebuilds the run from (see configFromRunStart).
func (r *Runner) runStartBody() trace.RunStart {
	cfg := &r.cfg
	rs := trace.RunStart{Schema: trace.Schema, Backend: cfg.Backend, ModelBackend: modelBackend(cfg.Backend), Model: r.be.model,
		Session: cfg.Session, Maps: append([]string(nil), r.visits...), Skill: r.skill, Seed: cfg.Seed, Episodes: cfg.Episodes,
		BudgetUSD: cfg.Budget.USD, Config: map[string]string{}}
	m := rs.Config
	if cfg.Session == SessionLockstep {
		lat := r.latency()
		rs.SimLatencyMs = lat.meanMs()
		m[keySimLatency] = lat.String()
	}
	m[keyCampaign] = r.camp.Name
	m[keyStopAfter] = strconv.Itoa(r.stopAfter)
	m[keyTrace] = cfg.Trace.String()
	m[keyRecord] = strconv.FormatBool(cfg.Record)
	if len(cfg.EntryCommands) > 0 {
		m[keyEntryCommands] = strings.Join(cfg.EntryCommands, ";")
	}
	if cfg.Budget.Queries > 0 {
		m[keyBudgetQueries] = strconv.Itoa(cfg.Budget.Queries)
	}
	if cfg.Budget.MaxQPS > 0 {
		m[keyBudgetMaxQPS] = strconv.FormatFloat(cfg.Budget.MaxQPS, 'g', -1, 64)
	}
	if cfg.Budget.Enabled() {
		m[keyBudgetOnExhausted] = string(r.bud.Limits().OnExhausted)
	}
	if cfg.MinModelShare > 0 {
		m[keyMinModelShare] = strconv.FormatFloat(cfg.MinModelShare, 'g', -1, 64)
	}
	if cfg.MaxStaleRate > 0 {
		m[keyMaxStaleRate] = strconv.FormatFloat(cfg.MaxStaleRate, 'g', -1, 64)
	}
	if cfg.ReplayTrace != "" {
		m[keyReplayTrace] = cfg.ReplayTrace
	}
	if cfg.LevelTimeout > 0 {
		m[keyLevelTimeout] = cfg.LevelTimeout.String()
	}
	if cfg.EpisodeTimeout > 0 {
		m[keyEpisodeTimeout] = cfg.EpisodeTimeout.String()
	}
	if cfg.MaxDeaths != 0 {
		m[keyMaxDeaths] = strconv.Itoa(cfg.MaxDeaths)
	}
	if cfg.Backend == BackendMock {
		f := DefaultMockFaults()
		if cfg.MockFaults != nil {
			f = *cfg.MockFaults
		}
		m[keyMockFaults] = fmt.Sprintf("seed=%d server=%g missing=%g ratelimit=%g overload=%g malformed=%g", f.Seed, f.Server,
			f.Missing, f.RateLimit, f.Overload, f.Malformed)
	}
	return rs
}

// latency is the run's simulated latency (lockstep): the configured one,
// else the backend's default; a replay's default is that of the recorded
// backend (its recorded latencies come first, see replayLatency).
func (r *Runner) latency() Latency {
	cfg := &r.cfg
	if cfg.SimLatency.Set {
		return cfg.SimLatency
	}
	kind := cfg.Backend
	if kind == BackendReplay {
		kind = r.be.recordedKind
	}
	if modelBackend(kind) {
		return Latency{Fixed: DefaultModelLatency, Set: true}
	}
	return Latency{Set: true}
}

// episodeRun is one episode being played.
type episodeRun struct {
	ep   int
	seed uint64
	dir  string
	stp  *stepper
	pub  *publisher
	log  *episodeLog
	pol  *policy
	be   *episodeBackend
	rec  *demo.Recorder
}

// episode plays episode ep.
func (r *Runner) episode(ctx context.Context, ep int) (EpisodeReport, error) {
	cfg := &r.cfg
	seed := cfg.Seed + uint64(ep)
	er := &episodeRun{ep: ep, seed: seed, stp: &stepper{}}
	tag := EpisodeDir(ep)
	rep := EpisodeReport{EpisodeSummary: metrics.EpisodeSummary{Index: ep, Seed: seed, Outcome: OutcomeFailed}}
	fail := func(err error) (EpisodeReport, error) {
		rep.Reason, rep.Error = err.Error(), err.Error()
		r.addResult(rep)
		return rep, err
	}

	// the episode's directory, trace file and log
	if !r.noFiles {
		er.dir = filepath.Join(r.dir, tag)
		if err := os.MkdirAll(er.dir, 0o755); err != nil {
			return fail(err)
		}
		fs, err := trace.CreateFile(filepath.Join(er.dir, TraceFile), defaultFlush)
		if err != nil {
			return fail(err)
		}
		if ep > r.epFrom {
			// every episode's trace starts with the run's run_start
			if err := fs.Write(r.runStart); err != nil {
				_ = fs.Close()
				return fail(err)
			}
		}
		if err := r.router.swap(fs); err != nil {
			return fail(err)
		}
		if er.log, err = createLog(filepath.Join(er.dir, LogFile), tag, &er.stp.gms, r.verboseLogf()); err != nil {
			return fail(err)
		}
	} else {
		er.log = discardLog(tag, &er.stp.gms, r.verboseLogf())
	}
	defer er.log.close()
	if ep == r.epFrom {
		r.runStart = r.publishRun(trace.TypeRunStart, r.runStartBody())
	}

	// the backend, the session and the bot's policy
	be, err := r.be.episode(ep, seed, r.strict, r.latency().model(seed))
	if err != nil {
		return fail(err)
	}
	er.be = be
	var hooks []func(*fakeclient.Client, []byte, []fakeclient.Span)
	if cfg.Record && !r.noFiles {
		er.rec = demo.NewRecorder(demo.DirCreator(filepath.Join(er.dir, DemoDir)))
		hooks = append(hooks, er.rec.OnServerMessage)
	}
	if cfg.OnServerMessage != nil {
		hooks = append(hooks, cfg.OnServerMessage)
	}
	sess, closeSession, err := r.newSession(ep, seed, hooks, er.log.logf)
	if err != nil {
		return fail(err)
	}
	er.stp.Session = sess
	er.stp.feed = r.feed
	er.pub = &publisher{bus: r.bus, st: r.st, ep: ep, s: er.stp}
	er.stp.onStep = func(step int64, cmds []trace.StepCmd) {
		er.pub.publish(trace.TypeCmds, trace.Cmds{Step: step, Cmds: cmds})
	}
	ectx, cancel := context.WithCancel(ctx)
	defer cancel()
	lat := be.latency
	if lat == nil {
		lat = r.latency().model(seed)
	}
	var gate *budget.Gate
	if r.bud != nil {
		gate = r.bud.Gate()
	}
	pol, err := newPolicy(policyConfig{
		backend: be.backend, fallback: scripted.New(scripted.Config{Seed: seed}), source: be.source,
		lockstep: cfg.Session == SessionLockstep, latency: lat,
		probe: &levelProbe{lib: r.lib, logf: er.log.logf}, gate: gate, pub: er.pub, mode: cfg.Trace,
		stop: func(st budget.State) {
			er.log.logf("runner: %s: stopping the run", st.Reason)
			cancel()
		},
	})
	if err != nil {
		closeSession()
		return fail(err)
	}
	er.pol = pol
	if r.onEpisode != nil {
		r.onEpisode(er)
	}

	r.logf("%s: %s, %s session, seed %d, maps %s", tag, cfg.Backend, cfg.Session, seed, strings.Join(r.visits, ","))
	start := cfg.Now()
	// the bot's tick reports (OnDecision) feed the episode's acted-on
	// provenance; the campaign publishes the tick events themselves
	res, runErr := campaign.Run(ectx, er.stp, campaign.Config{
		Campaign:       r.camp,
		Library:        r.lib,
		Bot:            bot.Config{Policy: pol, Seed: int64(seed), OnDecision: pol.onTick},
		Bus:            r.bus,
		Episode:        ep,
		Seed:           seed,
		EntryCommands:  cfg.EntryCommands,
		LevelTimeout:   cfg.LevelTimeout,
		EpisodeTimeout: cfg.EpisodeTimeout,
		MaxDeaths:      cfg.MaxDeaths,
		StopAfter:      r.stopAfter,
		Logf:           er.log.logf,
		Now:            cfg.Now,
	})
	wall := cfg.Now().Sub(start).Milliseconds()
	er.pub.publish(trace.TypeProvenance, pol.provenance())
	stats := pol.Stats()
	_ = pol.Close()
	closeSession()
	var errs []error
	if runErr != nil {
		errs = append(errs, runErr)
	}
	if er.rec != nil {
		if err := er.rec.Close(); err != nil {
			errs = append(errs, fmt.Errorf("demo: %w", err))
		}
	}

	// episode.json
	rep = r.episodeReport(ep, seed, res, stats, gate)
	rep.WallMs = wall
	if pol.stopped && res.Outcome == campaign.OutcomeAborted && ctx.Err() == nil {
		rep.Reason = pol.stoppedWhy
	}
	if runErr != nil {
		rep.Error = runErr.Error()
	}
	if er.rec != nil {
		for _, f := range er.rec.Files() {
			rep.Demos = append(rep.Demos, filepath.ToSlash(filepath.Join(DemoDir, f)))
		}
	}
	if !r.noFiles {
		if err := metrics.WriteJSON(filepath.Join(er.dir, EpisodeFile), rep); err != nil {
			errs = append(errs, err)
		}
		// a progress snapshot of the run so far
		if err := metrics.WriteJSON(filepath.Join(r.dir, RunFile), r.col.Summary()); err != nil {
			errs = append(errs, err)
		}
	}
	r.addResult(rep)
	r.logf("%s: %s (%s), %d deaths, %.1f s game, %.1f s wall", tag, rep.Outcome, rep.Reason, res.Deaths,
		float64(res.GameMs)/1000, float64(wall)/1000)
	return rep, errors.Join(errs...)
}

// defaultFlush is how often an episode's trace file is flushed.
const defaultFlush = 2 * time.Second

func (r *Runner) addResult(rep EpisodeReport) {
	r.mu.Lock()
	r.results = append(r.results, rep)
	r.mu.Unlock()
}

func (r *Runner) verboseLogf() func(string, ...any) {
	if r.cfg.Verbose {
		return r.logf
	}
	return nil
}

// episodeReport builds episode.json from the run's collector and the
// campaign's result.
func (r *Runner) episodeReport(ep int, seed uint64, res campaign.EpisodeResult, st decide.PipelineStats, gate *budget.Gate) EpisodeReport {
	rep := EpisodeReport{EpisodeSummary: metrics.EpisodeSummary{Index: ep, Seed: seed}}
	for _, es := range r.col.Summary().Episodes {
		if es.Index == ep {
			rep.EpisodeSummary = es
		}
	}
	if rep.Outcome == "" || rep.Outcome == metrics.OutcomeIncomplete {
		rep.Outcome, rep.Reason = res.Outcome, res.Reason
	}
	rep.Victory, rep.Diagnostics = res.Victory, res.Diagnostics
	for _, l := range res.Levels {
		rep.Routes = append(rep.Routes, RouteProgress{Map: l.Map, Visit: l.Visit, Route: l.Route, Outcome: l.Outcome,
			Steps: l.Steps, StepsDone: l.StepsDone, WallMs: l.WallMs})
	}
	ds := &DecideStats{Ticks: st.Ticks, Requests: map[string]int{}, Scheduler: map[string]LaneIO{}, OldLevel: st.OldLevel}
	for l := decide.Lane(0); l < decide.NumLanes; l++ {
		ds.Requests[l.String()] = st.Requests[l]
		s := st.Scheduler.Lanes[l]
		ds.Scheduler[l.String()] = LaneIO{Submitted: s.Submitted, Dropped: s.Dropped, Completed: s.Completed, Errors: s.Errors,
			Timeouts: s.Timeouts, Stale: s.Stale}
	}
	if gate != nil {
		ref := gate.Refused()
		ds.BudgetRefused = map[string]int{}
		for l := decide.Lane(0); l < decide.NumLanes; l++ {
			ds.BudgetRefused[l.String()] = ref[l]
		}
	}
	rep.Decide = ds
	return rep
}

// newSession returns the episode's session (not started: the campaign
// preloads the maps and starts it) and its cleanup.
func (r *Runner) newSession(ep int, seed uint64, hooks []func(*fakeclient.Client, []byte, []fakeclient.Span),
	logf func(string, ...any)) (session.Session, func(), error) {
	cfg := &r.cfg
	opt := fakeclient.Options{MaxHistory: 256}
	if len(hooks) > 0 {
		opt.OnServerMessage = func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
			for _, h := range hooks {
				h(c, payload, spans)
			}
		}
	}
	printf := func(format string, args ...any) {
		if s := strings.TrimSpace(fmt.Sprintf(format, args...)); s != "" {
			logf("sv: %s", s)
		}
	}
	spec := session.Spec{Map: r.visits[0], Skill: r.skill}
	switch cfg.Session {
	case SessionInProc:
		if r.host == nil {
			r.host = host.New()
		}
		inst, err := session.NewInstance(r.host, session.InstanceConfig{ID: fmt.Sprintf("bot-%s-%03d", r.id, ep), FS: cfg.FS,
			Spec: spec, Seed: uint32(seed), Maps: r.maps, Printf: printf})
		if err != nil {
			return nil, nil, err
		}
		p := session.NewInProc(inst, session.InProcConfig{FS: cfg.FS, Client: opt})
		return p, func() { _ = p.Close(); inst.Stop() }, nil
	default:
		l := session.NewLockstep(session.LockstepConfig{FS: cfg.FS, Spec: spec, Seed: uint32(seed), Client: opt, Maps: r.maps,
			Printf: printf})
		return l, func() { _ = l.Close() }, nil
	}
}
