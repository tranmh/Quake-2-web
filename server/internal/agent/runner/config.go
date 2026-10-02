package runner

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/sv"
)

// Backends (Config.Backend).
const (
	// BackendScripted is the rule policy (backend/scripted): the baseline.
	BackendScripted = "scripted"
	// BackendJev is the Jev API (backend/jev; TYPESAFE_API_KEY).
	BackendJev = "jev"
	// BackendMock is the jev client against an in-process fake Jev server
	// (backend/jevtest) answering with its noisy, model-like policy.
	BackendMock = "mock"
	// BackendReplay answers from a recorded trace (Config.ReplayTrace).
	BackendReplay = "replay"
	// BackendConstant and BackendRandom are the ablations
	// (backend/ablate).
	BackendConstant = "constant"
	BackendRandom   = "random"
)

// Backends lists the backend names.
func Backends() []string {
	return []string{BackendScripted, BackendJev, BackendMock, BackendReplay, BackendConstant, BackendRandom}
}

// modelBackend reports the backends that query a model.
func modelBackend(name string) bool { return name == BackendJev || name == BackendMock }

// Sessions (Config.Session).
const (
	// SessionLockstep runs the server on a virtual clock, deterministic and
	// far faster than real time.
	SessionLockstep = "lockstep"
	// SessionInProc runs a realtime host instance (for watching).
	SessionInProc = "inproc"
)

// Latency is the simulated backend latency of a lockstep run: a fixed
// duration, or samples drawn per request (decide.SampledLatency). The zero
// value is "unset": the backend's default (DefaultModelLatency for the
// model backends, none for the local ones, the recorded latencies for
// replay).
type Latency struct {
	Fixed   time.Duration
	Samples []time.Duration
	Set     bool
}

// DefaultModelLatency is the simulated latency of the model backends in
// lockstep when none is configured (the plan's 212 ms).
const DefaultModelLatency = 212 * time.Millisecond

// ParseLatency parses "212ms" (fixed) or "80ms,150ms,212ms,300ms"
// (sampled); "" is unset.
func ParseLatency(s string) (Latency, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Latency{}, nil
	}
	var out []time.Duration
	for _, p := range strings.Split(s, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(p))
		if err != nil {
			return Latency{}, fmt.Errorf("runner: latency %q: %w", p, err)
		}
		if d < 0 {
			return Latency{}, fmt.Errorf("runner: negative latency %q", p)
		}
		out = append(out, d)
	}
	if len(out) == 1 {
		return Latency{Fixed: out[0], Set: true}, nil
	}
	return Latency{Samples: out, Set: true}, nil
}

// String formats the latency as ParseLatency reads it.
func (l Latency) String() string {
	if !l.Set {
		return ""
	}
	if len(l.Samples) == 0 {
		return l.Fixed.String()
	}
	parts := make([]string, len(l.Samples))
	for i, d := range l.Samples {
		parts[i] = d.String()
	}
	return strings.Join(parts, ",")
}

// model returns the scheduler's latency model for an episode seed.
func (l Latency) model(seed uint64) decide.LatencyModel {
	if len(l.Samples) > 0 {
		return &decide.SampledLatency{Seed: seed, Samples: append([]time.Duration(nil), l.Samples...)}
	}
	return decide.FixedLatency(l.Fixed)
}

// meanMs is the latency's mean in ms (run_start's sim_latency_ms).
func (l Latency) meanMs() float64 {
	if len(l.Samples) == 0 {
		return float64(l.Fixed) / float64(time.Millisecond)
	}
	var sum time.Duration
	for _, d := range l.Samples {
		sum += d
	}
	return float64(sum) / float64(len(l.Samples)) / float64(time.Millisecond)
}

// TraceMode is how much of the lane state the decision events carry:
// every n-th decision (TraceMode(n)) has the full state and questions, the
// others only their digests.
type TraceMode int

// Trace modes.
const (
	TraceDigest TraceMode = 0 // digests only
	TraceFull   TraceMode = 1 // every decision's state
)

// ParseTrace parses "full", "digest" or "every:N" ("": full).
func ParseTrace(s string) (TraceMode, error) {
	switch s = strings.TrimSpace(s); {
	case s == "" || s == "full":
		return TraceFull, nil
	case s == "digest":
		return TraceDigest, nil
	case strings.HasPrefix(s, "every:"):
		n, err := strconv.Atoi(strings.TrimPrefix(s, "every:"))
		if err != nil || n < 1 {
			return TraceDigest, fmt.Errorf("runner: trace mode %q: want every:N with N >= 1", s)
		}
		return TraceMode(n), nil
	}
	return TraceDigest, fmt.Errorf("runner: trace mode %q (full|digest|every:N)", s)
}

// String formats the mode as ParseTrace reads it.
func (m TraceMode) String() string {
	switch {
	case m <= 0:
		return "digest"
	case m == 1:
		return "full"
	}
	return "every:" + strconv.Itoa(int(m))
}

// options returns what the n-th (from 0) decision event carries.
func (m TraceMode) options(n int) decide.RecordOptions {
	full := m > 0 && n%int(m) == 0
	return decide.RecordOptions{State: full, Questions: full}
}

// FileSystem is the game data a run reads (the demo pak): sv.FileSystem.
type FileSystem = sv.FileSystem

// Config configures a run: Episodes episodes of the campaign (or the
// first visits of it that Maps names) with one backend.
type Config struct {
	// FS is the game data (required), e.g. a pak.FS of the demo pak.
	FS FileSystem

	// Campaign is the campaign (nil: route.Load(RoutesDir)); RoutesDir
	// its directory ("": campaign.DefaultRoutesDir()).
	Campaign  *route.Campaign
	RoutesDir string
	// Maps limits the run to the campaign's first visits whose maps it
	// names (it must name the start map): "demo1" plays demo1 alone,
	// "demo1,demo2,demo3" the whole campaign (nil: the whole campaign).
	Maps []string
	// Skill is the game skill (nil: the campaign's). Another skill than
	// the campaign's is checked against the route tables first.
	Skill *int

	// Backend is one of Backends() ("": BackendScripted); Session one of
	// SessionLockstep ("") and SessionInProc.
	Backend string
	Session string
	// SimLatency is the simulated backend latency in lockstep (unset:
	// DefaultModelLatency for jev and mock, none for local backends). A
	// replay answers with each recorded request's recorded latency and
	// uses this one (unset: the recorded backend's default) for the
	// requests its trace lacks.
	SimLatency Latency

	// Episodes is the number of episodes (0: 1); Seed the run's seed
	// (episode i plays with seed Seed+i).
	Episodes int
	Seed     uint64

	// OutDir is where the run directory <RunID> is created ("": "runs");
	// RunID names it ("": NewRunID(), generated here).
	OutDir string
	RunID  string
	// Trace selects the decision events' state detail (zero: digests
	// only; use TraceFull for full states).
	Trace TraceMode
	// Record writes a .dm2 demo per level attempt (ep-NNN/demos).
	Record bool

	// Budget bounds the run's spend; Account is the process-wide account
	// every run shares (nil: none).
	Budget  budget.Limits
	Account *budget.Account
	// MinModelShare and MaxStaleRate configure the provenance gate
	// (0: metrics.DefaultMinModelShare and DefaultMaxStaleRate); the run's
	// summary is ModelDriven only when it passes.
	MinModelShare float64
	MaxStaleRate  float64
	// RequireComplete makes a run that is not completed (every episode
	// to its end, the terminal reached when the whole campaign is played)
	// an error of Run.
	RequireComplete bool

	// Jev configures the jev client (BackendJev: jev.FromEnv; the key is
	// taken from the environment only). For BackendMock only Model is
	// used.
	Jev jev.Config
	// MockFaults injects faults into the mock server (nil:
	// DefaultMockFaults).
	MockFaults *jevtest.Faults
	// ReplayTrace is the recorded trace BackendReplay answers from.
	ReplayTrace string

	// EntryCommands are client commands sent at every level entry
	// (cheats such as "god" and "notarget" for a test run).
	EntryCommands []string
	// LevelTimeout, EpisodeTimeout and MaxDeaths override the campaign's
	// watchdogs (0: its defaults).
	LevelTimeout, EpisodeTimeout time.Duration
	MaxDeaths                    int

	// NavDir is the nav cache directory ("": nav.DefaultDir()); Library
	// shares map data and graphs between runs (nil: one per run).
	NavDir  string
	Library *campaign.Library
	// MapCache shares loaded maps between the sessions (nil: one per run).
	MapCache *sv.MapCache

	// OnServerMessage, when set, also receives the bot client's server
	// messages (a spectate.Stream, say), after the demo recorder.
	OnServerMessage func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span)
	// Logf receives progress lines (episode starts and ends); with
	// Verbose also everything the episodes' log.txt gets.
	Logf    func(format string, args ...any)
	Verbose bool
	// Now is the wall clock (nil: time.Now): trace wall stamps, run ids.
	Now func() time.Time
}

// DefaultMockFaults are the mock server's faults by default: 2 % server
// errors and 2 % answers missing a question, drawn from the request
// content (the decide package's mock-Jev smoke).
func DefaultMockFaults() jevtest.Faults {
	return jevtest.Faults{Seed: 5, Server: 0.02, Missing: 0.02}
}

// errConfig marks a configuration error.
var errConfig = errors.New("runner: bad config")

func (c *Config) check() error {
	if c.FS == nil {
		return fmt.Errorf("%w: no game data (FS)", errConfig)
	}
	if c.Backend == "" {
		c.Backend = BackendScripted
	}
	known := false
	for _, b := range Backends() {
		known = known || b == c.Backend
	}
	if !known {
		return fmt.Errorf("%w: unknown backend %q (%s)", errConfig, c.Backend, strings.Join(Backends(), "|"))
	}
	switch c.Session {
	case "":
		c.Session = SessionLockstep
	case SessionLockstep, SessionInProc:
	default:
		return fmt.Errorf("%w: unknown session %q (lockstep|inproc)", errConfig, c.Session)
	}
	if c.Backend == BackendReplay {
		if c.ReplayTrace == "" {
			return fmt.Errorf("%w: backend replay needs a recorded trace", errConfig)
		}
		if c.Session != SessionLockstep {
			return fmt.Errorf("%w: backend replay needs a lockstep session", errConfig)
		}
	}
	if c.Episodes < 0 {
		return fmt.Errorf("%w: %d episodes", errConfig, c.Episodes)
	}
	if c.Episodes == 0 {
		c.Episodes = 1
	}
	if c.Skill != nil && (*c.Skill < 0 || *c.Skill > 3) {
		return fmt.Errorf("%w: skill %d not in 0..3", errConfig, *c.Skill)
	}
	if c.MinModelShare < 0 || c.MinModelShare > 1 || c.MaxStaleRate < 0 || c.MaxStaleRate > 1 {
		return fmt.Errorf("%w: gate thresholds %v, %v not in 0..1", errConfig, c.MinModelShare, c.MaxStaleRate)
	}
	if c.Budget.USD < 0 || c.Budget.Queries < 0 || c.Budget.MaxQPS < 0 {
		return fmt.Errorf("%w: negative budget", errConfig)
	}
	if c.OutDir == "" {
		c.OutDir = "runs"
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return nil
}
