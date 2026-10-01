package decide

import (
	"errors"
	"time"

	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

// PipelineConfig configures a Pipeline.
type PipelineConfig struct {
	// Backend answers the requests (required).
	Backend DecisionBackend
	// Fallback is the scripted policy the arbiter falls back to (nil:
	// none).
	Fallback DecisionBackend
	// Projector configures the lane states.
	Projector ProjectorConfig
	// Scheduler configures the cadence and the calls; its Backend is
	// Backend and its StaleAfter the arbiter's TTL unless set.
	Scheduler SchedulerConfig
	// Arbiter configures the arbitration; its Fallback is Fallback and its
	// P95 and Interval the scheduler's unless set.
	Arbiter ArbiterConfig
	// OnRecord sees every collected result and how it was applied (for
	// the trace); it runs in Tick.
	OnRecord func(*Record)
}

// Record is a collected result and how it was applied.
type Record struct {
	Backend string
	Result  Result
	Applied Applied
}

// RecordOptions selects what a trace decision event carries besides the
// digests, fields and raw response.
type RecordOptions struct {
	State     bool // the full lane state
	Questions bool // the questions object
}

// Decision returns the trace body of the record.
func (r *Record) Decision(opt RecordOptions) trace.Decision {
	req := r.Result.Req
	d := trace.Decision{Lane: req.Lane.String(), Req: req.Seq, SnapGMs: req.SnapTime, Backend: r.Backend,
		LatencyMs: float64(r.Result.Latency) / float64(time.Millisecond), ReqDigest: req.Digest(), Stale: r.Result.Stale}
	d.StateDigest, _ = trace.Digest(req.State)
	if req.View != nil {
		d.Combat = len(req.View.Enemies) > 0 || len(req.View.Incoming) > 0
	}
	if opt.State {
		d.State = req.State
	}
	if opt.Questions {
		d.Questions = req.QuestionsJSON()
	}
	if resp := r.Result.Resp; resp != nil {
		d.Response, d.Model = resp.Raw, resp.Model
		d.InputTokens, d.OutputTokens, d.CostUSD = resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.CostUSD
	}
	if r.Result.Err != nil {
		d.Err = r.Result.Err.Error()
	}
	for i := range r.Applied.Fields {
		d.Fields = append(d.Fields, r.Applied.Fields[i].Trace())
	}
	return d
}

// APICall returns the trace api_call body of the record.
func (r *Record) APICall() trace.APICall {
	req := r.Result.Req
	c := trace.APICall{Backend: r.Backend, Lane: req.Lane.String(), Req: req.Seq,
		LatencyMs: float64(r.Result.Latency) / float64(time.Millisecond), Stale: r.Result.Stale}
	if req.View != nil {
		c.Combat = len(req.View.Enemies) > 0 || len(req.View.Incoming) > 0
	}
	if resp := r.Result.Resp; resp != nil {
		c.Model, c.Status, c.Retry = resp.Model, resp.Status, resp.Retries
		c.InputTokens, c.OutputTokens, c.CostUSD = resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.CostUSD
	}
	if r.Result.Err != nil {
		c.Err = r.Result.Err.Error()
		var st interface{ HTTPStatus() int }
		if errors.As(r.Result.Err, &st) {
			c.Status = st.HTTPStatus()
		}
	}
	return c
}

// PipelineStats are the pipeline's counters.
type PipelineStats struct {
	Ticks    int
	Requests [NumLanes]int // built (submitted or dropped)
	// Records counts collected results; OldLevel those dropped because
	// they were asked before a level change or reload.
	Records, OldLevel int
	Scheduler         SchedulerStats
	Arbiter           ArbiterStats
}

// triggers detects the events that make the slow lane ask early.
type triggers struct {
	seen       map[string]bool
	lastDamage int64
	lastPickup int64
	objective  string
}

func (t *triggers) reset() { *t = triggers{} }

// update reports a new enemy in the fast state, new damage, a new pickup
// or a changed objective since the last call.
func (t *triggers) update(b *worldmodel.Belief, fast *State, obj *ObjectiveView) bool {
	ev := false
	if t.seen == nil {
		t.seen = map[string]bool{}
	}
	for i := range fast.Enemies {
		if id := fast.Enemies[i].ID; !t.seen[id] {
			t.seen[id], ev = true, true
		}
	}
	if n := len(b.Damage); n > 0 && b.Damage[n-1].At > t.lastDamage {
		t.lastDamage, ev = b.Damage[n-1].At, true
	}
	if b.Self.PickupAt > t.lastPickup {
		t.lastPickup, ev = b.Self.PickupAt, true
	}
	o := ""
	if obj != nil {
		o = obj.Kind + "\x00" + obj.Desc
	}
	if o != t.objective {
		t.objective, ev = o, true
	}
	return ev
}

// Pipeline runs the decision layer once per tick of the control loop:
// project the belief, submit the lanes that are due, apply what came
// back and return the Intent. It is not safe for concurrent use.
type Pipeline struct {
	cfg   PipelineConfig
	proj  *Projector
	sched *Scheduler
	arb   *Arbiter
	trig  triggers

	seq      uint64
	epochSeq uint64
	level    worldmodel.LevelKey
	frames   int
	started  bool
	last     Intent
	stats    PipelineStats
}

// ErrNoBackend is returned by NewPipeline without a backend.
var ErrNoBackend = errors.New("decide: no backend")

// NewPipeline returns a pipeline.
func NewPipeline(cfg PipelineConfig) (*Pipeline, error) {
	if cfg.Backend == nil {
		return nil, ErrNoBackend
	}
	p := &Pipeline{cfg: cfg, proj: NewProjector(cfg.Projector), last: DefaultIntent()}
	ac := cfg.Arbiter
	if ac.Fallback == nil {
		ac.Fallback = cfg.Fallback
	}
	sc := cfg.Scheduler
	sc.Backend = cfg.Backend
	if ac.P95 == nil {
		ac.P95 = func() time.Duration { return p.sched.P95() }
	}
	if ac.Interval == nil {
		ac.Interval = func(l Lane) time.Duration { return p.sched.Interval(l) }
	}
	p.arb = NewArbiter(ac)
	if sc.StaleAfter == nil {
		sc.StaleAfter = p.arb.TTL
	}
	p.sched = NewScheduler(sc)
	return p, nil
}

// Scheduler returns the pipeline's scheduler.
func (p *Pipeline) Scheduler() *Scheduler { return p.sched }

// Arbiter returns the pipeline's arbiter.
func (p *Pipeline) Arbiter() *Arbiter { return p.arb }

// Projector returns the pipeline's projector.
func (p *Pipeline) Projector() *Projector { return p.proj }

// Intent returns the last Intent.
func (p *Pipeline) Intent() Intent { return p.last }

// Tick runs one step at now (session ms) on belief b (live; it is copied
// for the requests; nil returns the last Intent) with the caller's
// objective (nil: none) and returns the Intent. It never blocks on the backend in realtime mode; in
// lockstep it waits only for results due by now.
func (p *Pipeline) Tick(now int64, b *worldmodel.Belief, obj *ObjectiveView) Intent {
	if b == nil {
		return p.last
	}
	p.stats.Ticks++
	if !p.started || b.Level != p.level || b.Frames < p.frames {
		// a new level or a reload: ids start over
		if p.started {
			p.arb.Reset()
			p.last = DefaultIntent()
		}
		p.trig.reset()
		p.started, p.level, p.epochSeq = true, b.Level, p.seq+1
	}
	p.frames = b.Frames

	cx := Context{Target: p.last.Target, Mode: p.last.Mode, Objective: obj}
	fast := p.proj.Fast(b, cx)
	act := Activity{Combat: len(fast.Enemies) > 0 || len(fast.Incoming) > 0}
	act.Urgent = len(fast.Incoming) > 0 || p.last.Danger >= DangerModerate
	for i := range fast.Enemies {
		act.Urgent = act.Urgent || fast.Enemies[i].Visible
	}
	act.Event = p.trig.update(b, &fast, obj)
	wantFast, wantSlow := p.sched.Want(now, act)
	var snap *worldmodel.Belief
	if wantFast || wantSlow {
		c := b.Clone()
		snap = &c
	}
	if wantFast {
		p.submit(LaneFast, now, &fast, snap)
	}
	if wantSlow {
		slow := fast
		p.proj.Extend(&slow, b, cx)
		p.submit(LaneSlow, now, &slow, snap)
	}
	for _, r := range p.sched.Collect(now) {
		p.stats.Records++
		var ap Applied
		if r.Req.Seq < p.epochSeq {
			ap = Applied{Seq: r.Req.Seq, Lane: r.Req.Lane, Dropped: "old_level"}
			p.stats.OldLevel++
		} else {
			ap = p.arb.Apply(r)
		}
		if p.cfg.OnRecord != nil {
			p.cfg.OnRecord(&Record{Backend: p.cfg.Backend.Name(), Result: r, Applied: ap})
		}
	}
	p.last = p.arb.Intent(now, b)
	return p.last
}

func (p *Pipeline) submit(l Lane, now int64, st *State, snap *worldmodel.Belief) {
	p.seq++
	req, err := NewRequest(p.seq, l, now, st, snap, p.proj.MaxBytes())
	if err != nil {
		return // a state that cannot fit the cap: skip the lane this tick
	}
	p.stats.Requests[l]++
	p.arb.Observe(req)
	p.sched.Submit(req)
}

// Stats returns the counters.
func (p *Pipeline) Stats() PipelineStats {
	s := p.stats
	s.Scheduler = p.sched.Stats()
	s.Arbiter = p.arb.Stats()
	return s
}

// Close stops the scheduler (cancelling calls in flight).
func (p *Pipeline) Close() error { return p.sched.Close() }
