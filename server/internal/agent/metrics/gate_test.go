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
