package runner

import (
	"context"
	"fmt"
	"math"
	"time"

	"quake2web/server/internal/agent/backend/ablate"
	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/replay"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// mockKey is the mock server's API key (it never leaves the process).
const mockKey = "q2bot-mock-key"

// backends holds a run's backend resources: the jev client (jev, mock)
// and the mock server, shared by the episodes, or the recorded trace
// (replay).
type backends struct {
	kind   string
	client *jev.Client
	mock   *jevtest.Server
	// model is the pinned model id (jev, mock).
	model string
	// recorded: the replay backend's trace, the name its decisions were
	// made under and that run's backend kind; recLatency the recorded
	// latency of every request by episode and Seq, recMaxSeq the last
	// recorded Seq by episode.
	recorded     []trace.Event
	recordedName string
	recordedKind string
	recLatency   map[int]map[uint64]time.Duration
	recMaxSeq    map[int]uint64
}

func newBackends(cfg *Config, logf func(string, ...any)) (*backends, error) {
	b := &backends{kind: cfg.Backend}
	jc := cfg.Jev
	if cfg.Session == SessionLockstep {
		// the client's own bucket runs on the wall clock, which lockstep
		// compresses: the run's MaxQPS is the budget gate's, in game time
		jc.RatePerSec = -1
	} else if cfg.Budget.MaxQPS > 0 {
		jc.RatePerSec = cfg.Budget.MaxQPS
	}
	if jc.Limiter == nil && cfg.Account != nil {
		jc.Limiter = cfg.Account
	}
	if jc.Logf == nil {
		jc.Logf = logf
	}
	if jc.Model == "" {
		jc.Model = jev.DefaultModel
	}
	switch cfg.Backend {
	case BackendJev:
		c, err := jev.New(jc)
		if err != nil {
			return nil, err
		}
		b.client, b.model = c, c.Model()
	case BackendMock:
		faults := mockFaults(cfg)
		b.mock = jevtest.NewServer(jevtest.Options{
			APIKey:   mockKey,
			Model:    jc.Model,
			Policy:   mockPolicy(cfg),
			Faults:   faults,
			MaxCalls: mockMaxCalls,
		})
		jc.BaseURL, jc.APIKey, jc.AllowCustomBase = b.mock.URL(), trace.NewSecret(mockKey), true
		if cfg.Session == SessionLockstep {
			mockLockstep(&jc)
		}
		c, err := jev.New(jc)
		if err != nil {
			b.mock.Close()
			return nil, err
		}
		b.client, b.model = c, c.Model()
	case BackendReplay:
		evs, err := trace.ReadFile(cfg.ReplayTrace)
		if err != nil {
			return nil, fmt.Errorf("runner: replay trace: %w", err)
		}
		b.recorded = evs
		b.recordedName, b.recordedKind = recordedBackend(evs)
		b.recLatency, b.recMaxSeq = map[int]map[uint64]time.Duration{}, map[int]uint64{}
		for i := range evs {
			e := &evs[i]
			var d trace.Decision
			if e.Type != trace.TypeDecision || e.DecodeBody(&d) != nil || !requestLane(d.Lane) {
				continue
			}
			if b.recLatency[e.Ep] == nil {
				b.recLatency[e.Ep] = map[uint64]time.Duration{}
			}
			b.recLatency[e.Ep][d.Req] = time.Duration(d.LatencyMs * float64(time.Millisecond))
			b.recMaxSeq[e.Ep] = max(b.recMaxSeq[e.Ep], d.Req)
		}
	}
	return b, nil
}

// mockPolicy is the mock server's answer policy (Config.MockPolicy): the
// scripted policy on the received lane state, perturbed by the noisy
// policy unless the clean one is asked for.
func mockPolicy(cfg *Config) jevtest.Policy {
	base := jevtest.NewScripted(scripted.Config{Seed: cfg.Seed})
	if cfg.MockPolicy == MockPolicyScripted {
		return base
	}
	return &jevtest.Noisy{Base: base, Seed: cfg.Seed, Noise: cfg.MockNoise, SecondBest: cfg.MockSwap, LowConfidence: cfg.MockLowConfidence}
}

// mockMaxCalls bounds the calls the mock server keeps (the runner never
// reads them; a long realtime mock run would otherwise keep every body).
const mockMaxCalls = 64

// mockFaults returns the mock server's faults: Config.MockFaults (nil:
// DefaultMockFaults) and, in an InProc session, the simulated latency as
// the replies' delay unless the faults set one.
func mockFaults(cfg *Config) jevtest.Faults {
	f := DefaultMockFaults()
	if cfg.MockFaults != nil {
		f = *cfg.MockFaults
	}
	if cfg.Session == SessionInProc && cfg.SimLatency.Set && len(f.Latency) == 0 {
		if len(cfg.SimLatency.Samples) > 0 {
			f.Latency = append([]time.Duration(nil), cfg.SimLatency.Samples...)
		} else if cfg.SimLatency.Fixed > 0 {
			f.Latency = []time.Duration{cfg.SimLatency.Fixed}
		}
	}
	return f
}

// mockLockstep keeps the jev client's wall-clock state out of a lockstep
// mock run, whose answers must depend on the run's seed alone: lockstep
// compresses the wall clock, so a circuit breaker, a 429/529 cooldown, a
// locally refused question set or an attempt timeout (all timed on the
// wall clock) would decide answers by how fast the machine plays. The
// breaker never opens, cooldowns and refused sets last a nanosecond, and
// both lanes' attempts get mockAttemptTimeout, the fast lane's cap lifted
// (a loaded machine once took longer than the 800 ms cap for a loopback
// round trip, and that one timeout changed the run): the timeouts only
// guard against a hang, as the scheduler's lockstep wait does.
func mockLockstep(jc *jev.Config) {
	jc.BreakerFailures = math.MaxInt32
	jc.DefaultCooldown, jc.MaxCooldown, jc.BadSetTTL = time.Nanosecond, time.Nanosecond, time.Nanosecond
	jc.UncapFast = true
	jc.FastTimeout, jc.SlowTimeout = mockAttemptTimeout, mockAttemptTimeout
}

// mockAttemptTimeout is a lockstep mock run's attempt timeout.
const mockAttemptTimeout = 30 * time.Second

// replayLatency is a replay's latency model: the recorded latency of a
// recorded request, the run's simulated latency for the others (requests
// still in flight when the recorded episode ended, which the trace never
// saw: they must stay in flight in the replay too).
type replayLatency struct {
	rec      map[uint64]time.Duration
	fallback decide.LatencyModel
}

func (l *replayLatency) Latency(req *decide.Request) time.Duration {
	if d, ok := l.rec[req.Seq]; ok {
		return d
	}
	return l.fallback.Latency(req)
}

// recordedBackend returns the name a trace's decisions were made under
// (their backend field) and the run's backend kind (run_start).
func recordedBackend(evs []trace.Event) (name, kind string) {
	for i := range evs {
		e := &evs[i]
		switch e.Type {
		case trace.TypeRunStart:
			var rs trace.RunStart
			if e.DecodeBody(&rs) == nil && kind == "" {
				kind = rs.Backend
			}
		case trace.TypeDecision:
			var d trace.Decision
			if name == "" && e.DecodeBody(&d) == nil && d.Backend != "" && requestLane(d.Lane) {
				name = d.Backend
			}
		}
		if name != "" && kind != "" {
			break
		}
	}
	if name == "" {
		name = kind
	}
	return name, kind
}

// requestLane reports the decision events of a request (lanes fast and
// slow), as opposed to other decision events a trace may carry (a bot's
// per-tick events).
func requestLane(lane string) bool { return lane == "fast" || lane == "slow" }

// answerSource is the provenance the arbiter gives a backend's accepted
// answers: the scripted backend's are the scripted policy's.
func answerSource(kind string) decide.Source {
	if kind == BackendScripted {
		return decide.SourceScripted
	}
	return decide.SourceModel
}

// episodeBackend is one episode's decision backend.
type episodeBackend struct {
	backend decide.DecisionBackend
	// latency overrides the configured simulated latency (replay: the
	// recorded latencies first), source the answers' provenance.
	latency decide.LatencyModel
	source  decide.Source
	replay  *replay.Backend
}

// episode returns episode ep's backend; lat is the run's simulated
// latency (a replay uses it for the requests its trace lacks).
func (b *backends) episode(ep int, seed uint64, strict bool, lat decide.LatencyModel) (*episodeBackend, error) {
	switch b.kind {
	case BackendScripted:
		return &episodeBackend{backend: scripted.New(scripted.Config{Seed: seed}), source: decide.SourceScripted}, nil
	case BackendJev, BackendMock:
		return &episodeBackend{backend: b.client, source: decide.SourceModel}, nil
	case BackendConstant:
		return &episodeBackend{backend: ablate.NewConstant(), source: decide.SourceModel}, nil
	case BackendRandom:
		return &episodeBackend{backend: ablate.NewRandom(seed), source: decide.SourceModel}, nil
	case BackendReplay:
		rb, err := replay.New(b.recorded, replay.Options{Strict: strict, Episode: ep})
		if err != nil {
			return nil, err
		}
		if rb.Len() == 0 {
			return nil, fmt.Errorf("runner: the replay trace has no decisions for episode %d", ep)
		}
		return &episodeBackend{backend: named{rb, b.recordedName}, latency: &replayLatency{rec: b.recLatency[ep], fallback: lat},
			source: answerSource(b.recordedKind), replay: rb}, nil
	}
	return nil, fmt.Errorf("runner: unknown backend %q", b.kind)
}

func (b *backends) close() {
	if b.mock != nil {
		b.mock.Close()
	}
}

// named is a backend under another name (a replay answering as the
// recorded backend, so its decision events compare equal).
type named struct {
	decide.DecisionBackend
	name string
}

func (n named) Name() string {
	if n.name == "" {
		return n.DecisionBackend.Name()
	}
	return n.name
}

func (n named) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	return n.DecisionBackend.Decide(ctx, req)
}
