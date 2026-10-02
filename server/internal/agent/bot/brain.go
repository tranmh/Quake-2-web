package bot

import (
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/perception"
)

// BrainConfig configures the decision pipeline NewBrain builds: the
// decide.Pipeline (lane states, questions, scheduler, arbiter) that is the
// bot's Policy. The zero value is the scripted policy, answering at once
// (realtime mode).
type BrainConfig struct {
	// Backend answers the decision questions (nil: the scripted policy,
	// whose answers are labelled scripted, not model).
	Backend decide.DecisionBackend
	// Fallback stands in for missing, unconfident or expired answers (nil:
	// the scripted policy; none when Backend is the scripted policy).
	Fallback decide.DecisionBackend
	// NoFallback runs without a fallback (expired answers are kept as
	// stale, then the defaults).
	NoFallback bool
	// Seed seeds the scripted policy (its strafe rhythm).
	Seed uint64
	// Classes is the class table (nil: perception.NewClassTable()).
	Classes *perception.ClassTable
	// Mode is the scheduler's run mode: decide.Lockstep for a lockstep
	// session (a request is due SimLatency after its snapshot, and the
	// sim waits for it), decide.Realtime for a realtime one.
	Mode decide.RunMode
	// SimLatency is the lockstep latency model (nil: zero, the scripted
	// policy's; e.g. a decide.SampledLatency around 212 ms for a mock
	// model).
	SimLatency decide.LatencyModel
	// Scheduler, Arbiter and Projector set the pipeline's other knobs;
	// their Backend, Fallback, Mode and SimLatency fields are overridden
	// by the ones above. The projector's SpaceProbe and PathFunc are
	// the level's: the bot sets them at every level entry
	// (decide.Pipeline.SetProbes).
	Scheduler decide.SchedulerConfig
	Arbiter   decide.ArbiterConfig
	Projector decide.ProjectorConfig
	// OnRecord sees every collected request result (for a trace of the
	// requests: decide.Record.Decision). The bot can emit them itself
	// instead (Config.TraceRequests).
	OnRecord func(*decide.Record)
}

// NewBrain returns the decision pipeline of cfg, ready to be a bot's
// Policy (Config.Policy). Close it when the bot is done (it cancels the
// backend calls in flight).
func NewBrain(cfg BrainConfig) (*decide.Pipeline, error) {
	backend := cfg.Backend
	answer := decide.SourceModel
	if backend == nil {
		backend = scripted.New(scripted.Config{Seed: cfg.Seed, Classes: cfg.Classes})
	}
	if backend.Name() == scripted.Name {
		answer = decide.SourceScripted
	}
	fallback := cfg.Fallback
	switch {
	case cfg.NoFallback || answer == decide.SourceScripted && fallback == nil:
		fallback = nil
	case fallback == nil:
		fallback = scripted.New(scripted.Config{Seed: cfg.Seed, Classes: cfg.Classes})
	}
	sc := cfg.Scheduler
	sc.Mode, sc.SimLatency = cfg.Mode, cfg.SimLatency
	ac := cfg.Arbiter
	if ac.AnswerSource == decide.SourceDefault {
		ac.AnswerSource = answer
	}
	pc := cfg.Projector
	if pc.Classes == nil {
		pc.Classes = cfg.Classes
	}
	return decide.NewPipeline(decide.PipelineConfig{Backend: backend, Fallback: fallback, Projector: pc, Scheduler: sc, Arbiter: ac, OnRecord: cfg.OnRecord})
}

// levelProber is a Policy that takes the level's static probes: the
// SpaceProbe of its collision model and a PathFunc over its nav graph
// (decide.Pipeline).
type levelProber interface {
	SetProbes(space decide.SpaceProbe, path decide.PathFunc)
}

// tickReporter is a Policy that reports what its last Tick did (the fast
// state it projected, the requests and results: decide.Pipeline).
type tickReporter interface {
	LastTick() decide.TickInfo
}
