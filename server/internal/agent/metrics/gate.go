package metrics

import (
	"fmt"
	"sort"

	"quake2web/server/internal/agent/trace"
)

// The plan's provenance gate (phase 9): at least 70 % of the target,
// fire_policy and mode decisions from the model, at most 15 % stale
// answers.
const (
	DefaultMinModelShare = 0.7
	DefaultMaxStaleRate  = 0.15
)

// Gate bases (Gate.Basis).
const (
	// GateTicks: the shares are over the decided ticks (the bot's lane
	// tick events, else the runner's provenance events): how often the
	// value acted on came from the model.
	GateTicks = "ticks"
	// GateDecisions: the trace has no provenance events, so the shares are
	// over the decision events' fields (answers as taken per request).
	GateDecisions = "decisions"
)

// GateConfig configures the provenance gate (Collector.SetGate). Zero
// values take the defaults.
type GateConfig struct {
	MinModelShare float64
	MaxStaleRate  float64
}

func (g GateConfig) withDefaults() GateConfig {
	if g.MinModelShare <= 0 {
		g.MinModelShare = DefaultMinModelShare
	}
	if g.MaxStaleRate <= 0 {
		g.MaxStaleRate = DefaultMaxStaleRate
	}
	return g
}

// Gate is the provenance gate's verdict (run.json "gate"): whether the run
// was driven by the model. It passes when a model backend answered for
// the whole run (no budget switched it to scripted-only), every gate field
// (target, fire_policy, mode) with decided ticks took at least
// MinModelShare of them from the model, at most MaxStaleRate of the
// answers arrived stale, and (ticks basis) no gate field acted on a stale
// answer on more than MaxStaleRate of its decided ticks.
type Gate struct {
	MinModelShare float64 `json:"min_model_share"`
	MaxStaleRate  float64 `json:"max_stale_rate"`
	Basis         string  `json:"basis"`
	// ModelShares are the gate fields' model shares; ModelShare is the
	// lowest of them (the one the threshold is held against).
	ModelShares map[string]float64 `json:"model_shares"`
	ModelShare  float64            `json:"model_share"`
	// StaleRate is the API stale-answer rate (APIStats.StaleRate).
	StaleRate float64 `json:"stale_rate"`
	// StaleShares are the gate fields' shares of decided ticks that acted
	// on a stale answer (ticks basis); TickStaleShare is the highest.
	StaleShares     map[string]float64 `json:"stale_shares,omitempty"`
	TickStaleShare  float64            `json:"tick_stale_share"`
	ModelBackend    bool               `json:"model_backend"`
	BudgetExhausted bool               `json:"budget_exhausted"`
	Passed          bool               `json:"passed"`
	Reasons         []string           `json:"reasons,omitempty"`
}

// TickProvenance counts one decision field's ticks by the source of the
// value acted on.
type TickProvenance struct {
	Default  int `json:"default"`
	Model    int `json:"model"`
	Scripted int `json:"scripted"`
	Stale    int `json:"stale"`
	Reflex   int `json:"reflex"`
	// Decided counts the ticks something decided the field (all but
	// default); ModelShare and StaleShare are over Decided.
	Decided    int     `json:"decided"`
	ModelShare float64 `json:"model_share"`
	StaleShare float64 `json:"stale_share"`
}

func (p *TickProvenance) add(o TickProvenance) {
	p.Default += o.Default
	p.Model += o.Model
	p.Scripted += o.Scripted
	p.Stale += o.Stale
	p.Reflex += o.Reflex
}

func (p *TickProvenance) finish() {
	p.Decided = p.Model + p.Scripted + p.Stale + p.Reflex
	p.ModelShare = ratio(p.Model, p.Decided)
	p.StaleShare = ratio(p.Stale, p.Decided)
}

// TickStats is the per-tick provenance of the decision fields: what the
// bot acted on at every decision tick. An episode's counts come from its
// lane tick events when the trace has them (they are written as the
// episode runs, so a crash keeps the ticks so far), else from its
// provenance event.
type TickStats struct {
	Ticks  int                       `json:"ticks"`
	Fields map[string]TickProvenance `json:"fields"`
	// GateModelShare is the model share of the gate fields' decided ticks
	// together.
	GateModelShare float64 `json:"gate_model_share"`
	// TickEvents is how many of Ticks were counted from lane tick events
	// (the rest from provenance events).
	TickEvents int `json:"tick_events,omitempty"`
}

// tickAcc accumulates provenance events, or lane tick events.
type tickAcc struct {
	ticks      int
	tickEvents int
	fields     map[string]*TickProvenance
}

func (a *tickAcc) field(name string) *TickProvenance {
	if a.fields == nil {
		a.fields = map[string]*TickProvenance{}
	}
	p := a.fields[name]
	if p == nil {
		p = &TickProvenance{}
		a.fields[name] = p
	}
	return p
}

func (a *tickAcc) add(b trace.Provenance) {
	if a.fields == nil {
		a.fields = map[string]*TickProvenance{}
	}
	a.ticks += b.Ticks
	for _, f := range b.Fields {
		a.field(f.Name).add(TickProvenance{Default: f.Default, Model: f.Model, Scripted: f.Scripted, Stale: f.Stale, Reflex: f.Reflex})
	}
}

// addTick accounts one lane tick event: every field of its Intent by the
// source of the value acted on (an unknown source counts as default).
func (a *tickAcc) addTick(in *trace.Intent) {
	if a.fields == nil {
		a.fields = map[string]*TickProvenance{}
	}
	a.ticks++
	a.tickEvents++
	if in == nil {
		return
	}
	for _, f := range in.Fields {
		var tf trace.TickField
		tf.Count(f.Source, 1)
		a.field(f.Name).add(TickProvenance{Default: tf.Default, Model: tf.Model, Scripted: tf.Scripted, Stale: tf.Stale, Reflex: tf.Reflex})
	}
}

// merge adds o's counts.
func (a *tickAcc) merge(o *tickAcc) {
	if o == nil || o.fields == nil {
		return
	}
	a.ticks += o.ticks
	a.tickEvents += o.tickEvents
	for name, p := range o.fields {
		a.field(name).add(*p)
	}
}

// stats returns the summary of the accumulated ticks (nil without any).
func (a *tickAcc) stats() *TickStats {
	if a == nil || a.fields == nil {
		return nil
	}
	s := &TickStats{Ticks: a.ticks, TickEvents: a.tickEvents, Fields: map[string]TickProvenance{}}
	var gate TickProvenance
	for name, p := range a.fields {
		q := *p
		q.finish()
		s.Fields[name] = q
		if isGateField(name) {
			gate.add(q)
		}
	}
	gate.finish()
	s.GateModelShare = gate.ModelShare
	return s
}

func isGateField(name string) bool {
	for _, g := range gateFields {
		if g == name {
			return true
		}
	}
	return false
}

// evaluateGate computes the gate on a finished summary.
func evaluateGate(s *RunSummary, cfg GateConfig, modelBackend, scriptedOnly bool) Gate {
	cfg = cfg.withDefaults()
	g := Gate{MinModelShare: cfg.MinModelShare, MaxStaleRate: cfg.MaxStaleRate, ModelShares: map[string]float64{},
		StaleRate: s.API.StaleRate, ModelBackend: modelBackend, BudgetExhausted: scriptedOnly}
	type share struct {
		v, stale float64
		ok       bool // the field was decided at all
	}
	shares := map[string]share{}
	if s.Ticks != nil {
		g.Basis = GateTicks
		g.StaleShares = map[string]float64{}
		for _, name := range gateFields {
			p, ok := s.Ticks.Fields[name]
			shares[name] = share{v: p.ModelShare, stale: p.StaleShare, ok: ok && p.Decided > 0}
		}
	} else {
		g.Basis = GateDecisions
		for _, name := range gateFields {
			p, ok := s.Decisions.Fields[name]
			shares[name] = share{v: p.ModelShare, ok: ok && p.total() > 0}
		}
	}
	first := true
	for _, name := range gateFields {
		sh := shares[name]
		if !sh.ok {
			continue
		}
		g.ModelShares[name] = sh.v
		if first || sh.v < g.ModelShare {
			g.ModelShare = sh.v
		}
		if g.StaleShares != nil {
			g.StaleShares[name] = sh.stale
			g.TickStaleShare = max(g.TickStaleShare, sh.stale)
		}
		first = false
	}
	if !modelBackend {
		g.Reasons = append(g.Reasons, fmt.Sprintf("backend %q does not query a model", s.Backend))
	}
	if scriptedOnly {
		g.Reasons = append(g.Reasons, "the budget switched the run to scripted-only")
	}
	if first {
		g.Reasons = append(g.Reasons, "no gate field (target, fire_policy, mode) was decided")
	}
	names := make([]string, 0, len(g.ModelShares))
	for name := range g.ModelShares {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if v := g.ModelShares[name]; v < cfg.MinModelShare-1e-12 {
			g.Reasons = append(g.Reasons, fmt.Sprintf("%s: model share %.3f < %.3f", name, v, cfg.MinModelShare))
		}
	}
	if g.StaleRate > cfg.MaxStaleRate+1e-12 {
		g.Reasons = append(g.Reasons, fmt.Sprintf("stale rate %.3f > %.3f", g.StaleRate, cfg.MaxStaleRate))
	}
	for _, name := range names {
		if v, ok := g.StaleShares[name]; ok && v > cfg.MaxStaleRate+1e-12 {
			g.Reasons = append(g.Reasons, fmt.Sprintf("%s: stale share of ticks %.3f > %.3f", name, v, cfg.MaxStaleRate))
		}
	}
	g.Passed = len(g.Reasons) == 0
	return g
}
