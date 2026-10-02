package runner

import (
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

// publisher stamps and publishes the runner's own events (decisions, api
// calls, budget, cmds, provenance) on the run's bus, with the envelope the
// campaign's events have: the episode, the session clock, the level the
// campaign announced last and the bot's latest server frame. It is used
// from the episode's goroutine only.
type publisher struct {
	bus *trace.Bus
	st  *stamp
	ep  int
	s   session.Session
}

func (p *publisher) publish(typ string, body any) trace.Event {
	lvl, m := p.st.get()
	if m == "" {
		m = p.s.MapName()
	}
	e := trace.Event{Type: typ, Ep: p.ep, GMs: p.s.GameTimeMs(), Lvl: lvl, Map: m, Body: body}
	if c := p.s.Client(); c != nil {
		e.SF = c.Frame.ServerFrame
	}
	return p.bus.Publish(e)
}

// levelProbe is the projector's SpaceProbe: a TraceSpace on the collision
// model of the level the belief is on (static map knowledge), switched
// when the bot changes level.
type levelProbe struct {
	lib  *campaign.Library
	name string
	ts   *decide.TraceSpace
	logf func(format string, args ...any)
}

// use switches the probe to map name.
func (p *levelProbe) use(name string) {
	if name == p.name {
		return
	}
	p.name, p.ts = name, nil
	md, err := p.lib.Map(name)
	if err != nil || md.CM == nil {
		p.logf("runner: no collision model for the space probe on %q: %v", name, err)
		return
	}
	p.ts = decide.NewTraceSpace(md.CM, 256)
}

// Clearance implements decide.SpaceProbe (no room at all without a map).
func (p *levelProbe) Clearance(origin decide.Vec3, yaw float32) [4]float32 {
	if p.ts == nil {
		return [4]float32{}
	}
	return p.ts.Clearance(origin, yaw)
}

// policy is the bot's Policy: the episode's decide.Pipeline (embedded,
// so the pipeline's methods stay visible to the bot) with the level probe
// kept on the bot's level, the budget gate planned before every tick, and
// every collected result published as a decision (and api_call) event.
type policy struct {
	*decide.Pipeline
	probe *levelProbe
	gate  *budget.Gate
	pub   *publisher
	mode  TraceMode
	// stop is called once when the budget is exhausted under the Stop
	// policy (it ends the episode).
	stop func(budget.State)

	decisions int
	// acted counts the bot's decision ticks by the source of every
	// field's acted-on value (onTick)
	acted      tickCounts
	budgetSeen bool
	lastHz     float64
	lastOnly   bool
	stopped    bool
	stoppedWhy string
	accepted   [decide.NumLanes]int
}

type policyConfig struct {
	backend  decide.DecisionBackend
	fallback decide.DecisionBackend
	source   decide.Source
	lockstep bool
	latency  decide.LatencyModel
	probe    *levelProbe
	gate     *budget.Gate
	pub      *publisher
	mode     TraceMode
	stop     func(budget.State)
}

func newPolicy(c policyConfig) (*policy, error) {
	p := &policy{probe: c.probe, gate: c.gate, pub: c.pub, mode: c.mode, stop: c.stop}
	be := c.backend
	if c.gate != nil {
		be = c.gate.Wrap(be)
	}
	sc := decide.SchedulerConfig{Mode: decide.Realtime}
	if c.lockstep {
		sc.Mode, sc.SimLatency = decide.Lockstep, c.latency
	}
	pl, err := decide.NewPipeline(decide.PipelineConfig{
		Backend:   be,
		Fallback:  c.fallback,
		Projector: decide.ProjectorConfig{Space: c.probe},
		Scheduler: sc,
		Arbiter:   decide.ArbiterConfig{AnswerSource: c.source},
		OnRecord:  p.onRecord,
	})
	if err != nil {
		return nil, err
	}
	p.Pipeline = pl
	return p, nil
}

// Tick implements bot.Policy.
func (p *policy) Tick(now int64, b *worldmodel.Belief, obj *decide.ObjectiveView) decide.Intent {
	if b != nil {
		p.probe.use(b.Map)
	}
	if p.gate == nil || b == nil {
		return p.Pipeline.Tick(now, b, obj)
	}
	st := p.gate.Plan(now)
	p.budgetEvent(st)
	in := p.Pipeline.Tick(now, b, obj)
	// the requests the scheduler took this tick count against the rates
	lanes := p.Pipeline.Scheduler().Stats().Lanes
	for l := range lanes {
		if n := lanes[l].Submitted - lanes[l].Dropped; n > p.accepted[l] {
			p.accepted[l] = n
			p.gate.Submitted(now, decide.Lane(l))
		}
	}
	return in
}

// budgetEvent publishes the budget's state when its level changed (and at
// the episode's first tick), and stops the episode when it is exhausted
// under the Stop policy.
func (p *policy) budgetEvent(st budget.State) {
	hz, only := st.Level()
	if p.budgetSeen && hz == p.lastHz && only == p.lastOnly {
		return
	}
	p.budgetSeen, p.lastHz, p.lastOnly = true, hz, only
	limit := st.LimitUSD
	if limit == 0 {
		limit = st.DailyLimitUSD
	}
	p.pub.publish(trace.TypeBudget, trace.Budget{SpentUSD: st.SpentUSD, LimitUSD: limit, RateHz: hz, ScriptedOnly: only,
		Reason: st.Reason})
	if st.Exhausted && p.gate.Budget().Limits().OnExhausted == budget.Stop && !p.stopped {
		p.stopped, p.stoppedWhy = true, st.Reason
		if p.stop != nil {
			p.stop(st)
		}
	}
}

// finalBudgetEvent publishes the budget's state at the episode's end, so
// the last budget event (run.json's budget) carries the spend of every
// result collected since the level last changed.
func (p *policy) finalBudgetEvent() {
	if p.gate == nil || !p.budgetSeen {
		return
	}
	st := p.gate.Budget().State()
	limit := st.LimitUSD
	if limit == 0 {
		limit = st.DailyLimitUSD
	}
	hz, only := st.Level()
	p.pub.publish(trace.TypeBudget, trace.Budget{SpentUSD: st.SpentUSD, LimitUSD: limit, RateHz: hz, ScriptedOnly: only,
		Reason: st.Reason})
}

// onRecord publishes a collected result (and charges it to the budget).
func (p *policy) onRecord(r *decide.Record) {
	if p.gate != nil {
		p.gate.Charge(r)
	}
	p.pub.publish(trace.TypeDecision, r.Decision(p.mode.options(p.decisions)))
	p.decisions++
	if !budget.IsRefused(r.Result.Err) {
		// a refused request never reached the backend: no call to account
		p.pub.publish(trace.TypeAPICall, r.APICall())
	}
}

// onTick is the bot's OnDecision hook: it counts the provenance of every
// field of a lane tick event (what the bot acted on, its overrides marked
// reflex). The campaign publishes the events themselves.
func (p *policy) onTick(d *trace.Decision) {
	if d == nil || d.Lane != trace.LaneTick || d.Intent == nil {
		return
	}
	p.acted.add(d.Intent.Fields)
}

// provenance returns the per-tick provenance of the episode so far: the
// acted-on counts of the bot's tick reports, and the arbiter's.
func (p *policy) provenance() trace.Provenance {
	st := p.Pipeline.Stats()
	out := trace.Provenance{ArbiterTicks: st.Ticks}
	for f := decide.Field(0); f < decide.NumFields; f++ {
		tf := trace.TickField{Name: f.ID()}
		for src, n := range st.Arbiter.Fields[f].Ticks {
			tf.Count(decide.Source(src).String(), n)
		}
		out.Arbiter = append(out.Arbiter, tf)
	}
	if p.acted.ticks == 0 {
		// a bot that reports no ticks: the arbiter's view is all there is
		out.Ticks, out.Fields = out.ArbiterTicks, append([]trace.TickField(nil), out.Arbiter...)
		return out
	}
	out.Ticks = p.acted.ticks
	for f := decide.Field(0); f < decide.NumFields; f++ {
		out.Fields = append(out.Fields, p.acted.field(f.ID()))
	}
	return out
}

// tickCounts counts tick reports' fields by source.
type tickCounts struct {
	ticks  int
	fields map[string]*trace.TickField
}

// add counts one tick's fields; a decision field the tick does not
// report counts as default (every field counts every tick).
func (c *tickCounts) add(fields []trace.Field) {
	if c.fields == nil {
		c.fields = map[string]*trace.TickField{}
	}
	c.ticks++
	seen := map[string]bool{}
	for _, f := range fields {
		if _, ok := decide.FieldOf(f.Name); !ok || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		c.get(f.Name).Count(f.Source, 1)
	}
	for f := decide.Field(0); f < decide.NumFields; f++ {
		if !seen[f.ID()] {
			c.get(f.ID()).Default++
		}
	}
}

func (c *tickCounts) get(name string) *trace.TickField {
	tf := c.fields[name]
	if tf == nil {
		tf = &trace.TickField{Name: name}
		c.fields[name] = tf
	}
	return tf
}

// field returns field name's counts.
func (c *tickCounts) field(name string) trace.TickField {
	if tf := c.fields[name]; tf != nil {
		return *tf
	}
	return trace.TickField{Name: name}
}
