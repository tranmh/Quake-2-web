package metrics

import (
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/agent/trace"
)

// gateTrace is a two-episode model run: provenance events per episode,
// a few api calls (one stale) and decision events.
func gateTrace(backend string, model bool, extra ...trace.Event) []trace.Event {
	evs := []trace.Event{
		{Type: trace.TypeRunStart, Body: trace.RunStart{Schema: trace.Schema, Backend: backend, ModelBackend: model, Session: "lockstep"}},
		{Type: trace.TypeEpisodeStart, Ep: 0, Body: trace.EpisodeStart{Seed: 1}},
		{Type: trace.TypeLevelStart, Ep: 0, Map: "demo1", Body: trace.LevelStart{}},
	}
	for i := 0; i < 20; i++ {
		evs = append(evs, trace.Event{Type: trace.TypeAPICall, Ep: 0, GMs: int64(100 * i), Body: trace.APICall{Backend: backend, Lane: "fast",
			Status: 200, LatencyMs: 200, Stale: i == 0}})
		evs = append(evs, trace.Event{Type: trace.TypeDecision, Ep: 0, GMs: int64(100 * i), Body: trace.Decision{Lane: "fast", Backend: backend,
			Fields: []trace.Field{{Name: "target", Value: "e1", Source: trace.SourceModel}}}})
	}
	evs = append(evs,
		trace.Event{Type: trace.TypeLevelEnd, Ep: 0, GMs: 5000, Map: "demo1", Body: trace.LevelEnd{Outcome: trace.OutcomeExit}},
		trace.Event{Type: trace.TypeEpisodeEnd, Ep: 0, GMs: 5000, Body: trace.EpisodeEnd{Outcome: "completed"}},
		trace.Event{Type: trace.TypeProvenance, Ep: 0, GMs: 5000, Body: trace.Provenance{Ticks: 50, Fields: []trace.TickField{
			{Name: "target", Default: 10, Model: 36, Scripted: 4},
			{Name: "fire_policy", Default: 10, Model: 32, Scripted: 6, Stale: 2},
			{Name: "mode", Model: 45, Scripted: 5},
			{Name: "movement", Default: 50},
		}}},
		trace.Event{Type: trace.TypeEpisodeStart, Ep: 1, Body: trace.EpisodeStart{Seed: 2}},
		trace.Event{Type: trace.TypeEpisodeEnd, Ep: 1, GMs: 3000, Body: trace.EpisodeEnd{Outcome: "completed"}},
		trace.Event{Type: trace.TypeProvenance, Ep: 1, GMs: 3000, Body: trace.Provenance{Ticks: 30, Fields: []trace.TickField{
			{Name: "target", Default: 30},
			{Name: "fire_policy", Default: 0, Model: 28, Scripted: 2},
			{Name: "mode", Model: 25, Scripted: 5},
		}}},
	)
	evs = append(evs, extra...)
	return append(evs, trace.Event{Type: trace.TypeRunEnd, Body: trace.RunEnd{Outcome: "completed"}})
}

func summarize(t *testing.T, evs []trace.Event, gate *GateConfig) RunSummary {
	t.Helper()
	c := NewCollector()
	if gate != nil {
		c.SetGate(*gate)
	}
	for _, e := range evs {
		if err := c.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	return c.Summary()
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestTickProvenance(t *testing.T) {
	s := summarize(t, gateTrace("jev", true), nil)
	if s.Gate != nil || !s.ModelDriven {
		t.Fatalf("without a gate: gate %+v model_driven %v", s.Gate, s.ModelDriven)
	}
	if s.Ticks == nil || s.Ticks.Ticks != 80 {
		t.Fatalf("ticks %+v", s.Ticks)
	}
	tg := s.Ticks.Fields["target"]
	if tg.Default != 40 || tg.Model != 36 || tg.Scripted != 4 || tg.Decided != 40 || !near(tg.ModelShare, 0.9) {
		t.Errorf("target %+v", tg)
	}
	fp := s.Ticks.Fields["fire_policy"]
	if fp.Decided != 70 || !near(fp.ModelShare, 60.0/70) || !near(fp.StaleShare, 2.0/70) {
		t.Errorf("fire_policy %+v", fp)
	}
	if mv := s.Ticks.Fields["movement"]; mv.Decided != 0 || mv.ModelShare != 0 {
		t.Errorf("movement %+v", mv)
	}
	// target 36/40, fire 60/70, mode 70/80
	if !near(s.Ticks.GateModelShare, 166.0/190) {
		t.Errorf("gate model share %v", s.Ticks.GateModelShare)
	}
	if len(s.Episodes) != 2 || s.Episodes[0].Ticks == nil || s.Episodes[0].Ticks.Ticks != 50 || s.Episodes[1].Ticks.Fields["target"].Decided != 0 {
		t.Fatalf("episode ticks %+v", s.Episodes)
	}
}

func TestGate(t *testing.T) {
	cases := []struct {
		name    string
		evs     []trace.Event
		cfg     GateConfig
		pass    bool
		reason  string
		basis   string
		minimum float64
	}{
		{name: "pass", evs: gateTrace("jev", true), pass: true, basis: GateTicks, minimum: 60.0 / 70},
		{name: "threshold", evs: gateTrace("jev", true), cfg: GateConfig{MinModelShare: 0.88}, reason: "fire_policy: model share 0.857 < 0.880", basis: GateTicks},
		{name: "stale", evs: gateTrace("jev", true), cfg: GateConfig{MaxStaleRate: 0.01}, reason: "stale rate 0.050 > 0.010", basis: GateTicks},
		{name: "not a model", evs: gateTrace("scripted", false), reason: `backend "scripted" does not query a model`, basis: GateTicks},
		{name: "budget", evs: gateTrace("jev", true, trace.Event{Type: trace.TypeBudget, Ep: 1, Body: trace.Budget{ScriptedOnly: true, Reason: "exhausted"}}),
			reason: "scripted-only", basis: GateTicks},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := summarize(t, tc.evs, &tc.cfg)
			g := s.Gate
			if g == nil {
				t.Fatal("no gate")
			}
			if g.Passed != tc.pass || s.ModelDriven != tc.pass || g.Basis != tc.basis {
				t.Fatalf("gate %+v, model_driven %v", g, s.ModelDriven)
			}
			if tc.reason != "" && !strings.Contains(strings.Join(g.Reasons, "; "), tc.reason) {
				t.Fatalf("reasons %q, want %q", g.Reasons, tc.reason)
			}
			if tc.minimum > 0 && !near(g.ModelShare, tc.minimum) {
				t.Fatalf("model share %v, want %v", g.ModelShare, tc.minimum)
			}
			if !near(g.StaleRate, 0.05) {
				t.Fatalf("stale rate %v", g.StaleRate)
			}
		})
	}

	// without provenance events the gate falls back to the decision
	// fields, and with no gate field decided at all it fails
	var plain []trace.Event
	for _, e := range gateTrace("jev", true) {
		if e.Type != trace.TypeProvenance {
			plain = append(plain, e)
		}
	}
	s := summarize(t, plain, &GateConfig{})
	if g := s.Gate; g.Basis != GateDecisions || !g.Passed || g.ModelShare != 1 || len(g.ModelShares) != 1 {
		t.Fatalf("decisions basis: %+v", g)
	}
	s = summarize(t, []trace.Event{{Type: trace.TypeRunStart, Body: trace.RunStart{Backend: "jev", ModelBackend: true}}}, &GateConfig{})
	if g := s.Gate; g.Passed || !strings.Contains(strings.Join(g.Reasons, ";"), "no gate field") {
		t.Fatalf("empty run: %+v", g)
	}
}

// tickEvent is a lane tick event of episode ep whose Intent reports the
// gate fields' sources.
func tickEvent(ep int, gms int64, target, fire, mode string) trace.Event {
	return trace.Event{Type: trace.TypeDecision, Ep: ep, GMs: gms, Body: trace.Decision{Lane: trace.LaneTick, Intent: &trace.Intent{
		Fields: []trace.Field{{Name: "mode", Source: mode}, {Name: "target", Source: target}, {Name: "fire_policy", Source: fire},
			{Name: "movement", Source: trace.SourceDefault}}}}}
}

// TestTickEventProvenance: the bot's lane tick events are what was acted
// on: where an episode has them they replace its provenance event's
// counts (a reflex override lowers the model share), they are not
// counted as request decisions, and an episode without them keeps its
// provenance event.
func TestTickEventProvenance(t *testing.T) {
	var ticks []trace.Event
	for i := 0; i < 40; i++ {
		fire := trace.SourceModel
		if i%4 == 0 {
			fire = trace.SourceReflex // the route's kill overrode fire_policy
		}
		target := trace.SourceModel
		if i < 4 {
			target = trace.SourceReflex
		}
		ticks = append(ticks, tickEvent(0, int64(100*i), target, fire, trace.SourceModel))
	}
	base := summarize(t, gateTrace("jev", true), &GateConfig{})
	s := summarize(t, gateTrace("jev", true, ticks...), &GateConfig{})

	if s.Decisions.Decisions != base.Decisions.Decisions || s.Decisions.Ticks != 40 || base.Decisions.Ticks != 0 {
		t.Fatalf("decisions %d (want %d), ticks %d", s.Decisions.Decisions, base.Decisions.Decisions, s.Decisions.Ticks)
	}
	ep0 := s.Episodes[0].Ticks
	if ep0 == nil || ep0.Ticks != 40 || ep0.TickEvents != 40 {
		t.Fatalf("episode 0 ticks %+v", ep0)
	}
	fp := ep0.Fields["fire_policy"]
	if fp.Reflex != 10 || fp.Model != 30 || !near(fp.ModelShare, 0.75) {
		t.Fatalf("episode 0 fire_policy %+v", fp)
	}
	// episode 1 has no tick events: its provenance event stands
	if ep1 := s.Episodes[1].Ticks; ep1 == nil || ep1.Ticks != 30 || ep1.TickEvents != 0 || ep1.Fields["fire_policy"].Model != 28 {
		t.Fatalf("episode 1 ticks %+v", ep1)
	}
	// the run: fire_policy 30+28 model of 40+30 decided
	run := s.Ticks
	if run.Ticks != 70 || run.TickEvents != 40 || !near(run.Fields["fire_policy"].ModelShare, 58.0/70) || run.Fields["target"].Reflex != 4 {
		t.Fatalf("run ticks %+v", run)
	}
	if !near(s.Gate.ModelShares["fire_policy"], 58.0/70) || s.Gate.ModelShares["fire_policy"] >= base.Gate.ModelShares["fire_policy"]-0.02 {
		t.Fatalf("reflex must lower the gate share: %v (provenance events alone: %v)", s.Gate.ModelShares, base.Gate.ModelShares)
	}
	s = summarize(t, gateTrace("jev", true, ticks...), &GateConfig{MinModelShare: 0.85})
	if s.Gate.Passed || !strings.Contains(strings.Join(s.Gate.Reasons, ";"), "fire_policy: model share 0.829 < 0.850") {
		t.Fatalf("gate %+v", s.Gate)
	}
}

// TestGateTickStale: answers that all arrived in time (API stale rate 0)
// can still be acted on past their TTL; the gate holds the gate fields'
// stale share of ticks to MaxStaleRate too.
func TestGateTickStale(t *testing.T) {
	evs := []trace.Event{
		{Type: trace.TypeRunStart, Body: trace.RunStart{Schema: trace.Schema, Backend: "mock", ModelBackend: true}},
		{Type: trace.TypeEpisodeStart, Body: trace.EpisodeStart{Seed: 1}},
		{Type: trace.TypeAPICall, Body: trace.APICall{Backend: "mock", Lane: "fast", Status: 200, LatencyMs: 212}},
	}
	for i := 0; i < 50; i++ {
		fire := trace.SourceModel
		if i%5 == 0 {
			fire = trace.SourceStale // 20 %
		}
		evs = append(evs, tickEvent(0, int64(100*i), trace.SourceModel, fire, trace.SourceModel))
	}
	evs = append(evs, trace.Event{Type: trace.TypeEpisodeEnd, GMs: 5000, Body: trace.EpisodeEnd{Outcome: "completed"}},
		trace.Event{Type: trace.TypeRunEnd, Body: trace.RunEnd{Outcome: "completed"}})

	s := summarize(t, evs, &GateConfig{MinModelShare: 0.7})
	g := s.Gate
	if g.StaleRate != 0 || !near(g.StaleShares["fire_policy"], 0.2) || !near(g.TickStaleShare, 0.2) || g.Passed {
		t.Fatalf("gate %+v", g)
	}
	if !strings.Contains(strings.Join(g.Reasons, ";"), "fire_policy: stale share of ticks 0.200 > 0.150") {
		t.Fatalf("reasons %q", g.Reasons)
	}
	if s = summarize(t, evs, &GateConfig{MinModelShare: 0.7, MaxStaleRate: 0.25}); !s.Gate.Passed {
		t.Fatalf("at 25 %%: %+v", s.Gate)
	}
}
