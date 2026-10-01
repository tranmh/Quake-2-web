package metrics

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"quake2web/server/internal/agent/trace"
)

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("Q2_UPDATE_FIXTURES") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with Q2_UPDATE_FIXTURES=1)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// run builds the trace of a scripted story: demo1 is left through its exit,
// demo2 costs a death and a reload before its exit, demo3 is cut short by a
// failed run; the jev backend is throttled to scripted-only on the way.
func run() []trace.Event {
	var evs []trace.Event
	gms := int64(0)
	add := func(typ string, lvl int, mapname string, body any) {
		evs = append(evs, trace.Event{Type: typ, Ep: 0, GMs: gms, Lvl: lvl, Map: mapname, SF: int32(gms / 100), Body: body})
	}
	call := func(lat float64, status int, combat, stale bool, retry int, err string) {
		add(trace.TypeAPICall, -1, "", trace.APICall{Backend: "jev", Model: "jev-1.13", Lane: "fast", Status: status,
			LatencyMs: lat, InputTokens: 1000, OutputTokens: 40, CostUSD: 0.000042, Combat: combat, Stale: stale, Retry: retry, Err: err})
	}
	decide := func(lvl int, m string, fields ...trace.Field) {
		add(trace.TypeDecision, lvl, m, trace.Decision{Lane: "fast", Backend: "jev", Model: "jev-1.13", LatencyMs: 200, Fields: fields})
	}
	f := func(name, value, source, scripted string) trace.Field {
		return trace.Field{Name: name, Value: value, Source: source, Scripted: scripted}
	}

	add(trace.TypeRunStart, 0, "", trace.RunStart{Schema: trace.Schema, Backend: "jev", ModelBackend: true, Model: "jev-1.13",
		Session: "lockstep", Maps: []string{"demo1", "demo2", "demo3"}, Skill: 1, Seed: 77, Episodes: 1})
	add(trace.TypeEpisodeStart, 0, "", trace.EpisodeStart{Seed: 78})

	// demo1
	gms = 600
	add(trace.TypeLevelStart, 0, "demo1", trace.LevelStart{Visit: 0, Gen: 1})
	for i, lat := range []float64{120, 180, 210, 250, 90, 300, 400, 150, 220, 199} {
		gms += 100
		call(lat, 200, i < 6, i == 3, 0, "")
		decide(0, "demo1", f("target", "e1", trace.SourceModel, "e1"), f("fire_policy", "fire_when_aligned", trace.SourceModel, "hold"))
	}
	add(trace.TypeDamage, 0, "demo1", trace.Damage{Amount: 12, Health: 88})
	add(trace.TypeKill, 0, "demo1", trace.Kill{Target: "e1", Class: "soldier_light"})
	gms = 30600
	add(trace.TypeLevelEnd, 0, "demo1", trace.LevelEnd{Outcome: trace.OutcomeExit, CombatMs: 2000,
		KilledMonsters: 4, TotalMonsters: 11, FoundSecrets: 1, TotalSecrets: 2})

	// demo2: a death, a reload, throttling
	gms = 31000
	add(trace.TypeLevelStart, 1, "demo2", trace.LevelStart{Visit: 0, Gen: 2})
	gms += 500
	call(0, 429, true, false, 0, "429 Too Many Requests")
	call(480, 200, true, false, 1, "")
	decide(1, "demo2", f("target", "e7", trace.SourceStale, ""), f("mode", "fight", trace.SourceModel, "fight"))
	add(trace.TypeDamage, 1, "demo2", trace.Damage{Amount: 40, Health: 30})
	add(trace.TypeDamage, 1, "demo2", trace.Damage{Amount: 35, Health: -5})
	add(trace.TypeDeath, 1, "demo2", trace.Death{Cause: "gunner", Health: -5})
	add(trace.TypeError, 1, "demo2", trace.Error{Msg: "jev: 429 Too Many Requests"})
	gms += 1500
	add(trace.TypeReload, 1, "demo2", trace.Reload{Slot: "save0", Deaths: 1})
	add(trace.TypeStuck, 1, "demo2", trace.Stuck{Stage: "jump"})
	add(trace.TypeBudget, 1, "demo2", trace.Budget{SpentUSD: 1.9, LimitUSD: 2, RateHz: 0, ScriptedOnly: true, Reason: "budget"})
	decide(1, "demo2", f("target", "e9", trace.SourceScripted, ""), f("mode", "objective", trace.SourceScripted, ""),
		f("fire_policy", "hold", trace.SourceReflex, ""), f("danger", "low", "mystery", ""))
	gms = 61000
	add(trace.TypeLevelEnd, 1, "demo2", trace.LevelEnd{Outcome: trace.OutcomeExit, CombatMs: 3000,
		KilledMonsters: 6, TotalMonsters: 14, TotalSecrets: 1})

	// demo3: cut short
	gms = 61500
	add(trace.TypeLevelStart, 2, "demo3", trace.LevelStart{Visit: 0, Gen: 3})
	gms = 70000
	add(trace.TypeDamage, 2, "demo3", trace.Damage{Amount: 5, Health: 95})
	add(trace.TypeRunEnd, 0, "", trace.RunEnd{Outcome: "failed", Reason: "aborted by the operator"})
	return evs
}

func publish(evs []trace.Event, sinks ...trace.Sink) []trace.Event {
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	n := 0
	b := trace.NewBus("run-golden", func() time.Time { n++; return t0.Add(time.Duration(n) * 10 * time.Millisecond) })
	for _, s := range sinks {
		b.AddSink(s)
	}
	out := make([]trace.Event, 0, len(evs))
	for _, e := range evs {
		out = append(out, b.Publish(e))
	}
	return out
}

func TestSummaryGolden(t *testing.T) {
	c := NewCollector()
	publish(run(), c)
	s := c.Summary()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "run.golden.json", append(b, '\n'))

	// spot checks of the arithmetic behind the golden file
	if s.Schema != "q2bot.run/1" || s.Outcome != "failed" || s.ModelDriven || s.Model != "jev-1.13" ||
		len(s.Episodes) != 1 || s.EpisodeSeeds[0] != 78 || s.Seed != 77 {
		t.Fatalf("run fields %+v", s)
	}
	lv := s.Episodes[0].Levels
	if len(lv) != 3 || lv[0].TimeMs != 30000 || lv[1].Deaths != 1 || lv[1].Reloads != 1 || lv[1].DamageTaken != 75 ||
		lv[2].Outcome != OutcomeIncomplete || lv[2].TimeMs != 8500 {
		t.Fatalf("levels %+v", lv)
	}
	tot := s.Totals
	if tot.Levels != 3 || tot.LevelsCompleted != 2 || tot.Kills != 10 || tot.Monsters != 25 || tot.Secrets != 1 ||
		tot.DamageTaken != 92 || tot.CombatMs != 5000 || tot.BotKills != 1 || tot.Stuck != 1 {
		t.Fatalf("totals %+v", tot)
	}
	d := s.Decisions
	// target: 10 model, 1 stale, 1 scripted; fire_policy: 10 model, 1 reflex; mode: 1 model, 1 scripted
	if d.Decisions != 12 || d.Fields["target"].Model != 10 || d.Fields["target"].Stale != 1 ||
		d.Fields["danger"].Other != 1 || math.Abs(d.GateModelShare-21.0/25) > 1e-12 {
		t.Fatalf("decisions %+v", d)
	}
	if d.Comparisons != 21 || d.Disagreements != 10 {
		t.Fatalf("comparisons %d disagreements %d", d.Comparisons, d.Disagreements)
	}
	a := s.API
	// 11 OK answers: latencies 90 120 150 180 199 210 220 250 300 400 480
	if a.Calls != 12 || a.OK != 11 || a.Errors != 1 || a.Retries != 1 || a.Stale != 1 ||
		a.LatencyMs != (Percentiles{P50: 210, P95: 480, P99: 480, Max: 480}) || a.ByStatus["429"] != 1 {
		t.Fatalf("api %+v", a)
	}
	if a.CombatCalls != 7 || math.Abs(a.CombatQPS-7.0/5) > 1e-12 || math.Abs(a.CostUSD-12*0.000042) > 1e-12 {
		t.Fatalf("combat %d qps %v cost %v", a.CombatCalls, a.CombatQPS, a.CostUSD)
	}
	if s.Errors != 1 || s.Budget == nil || !s.Budget.ScriptedOnly || s.GameMs != 70000 || s.Events != len(run()) {
		t.Fatalf("errors %d budget %+v game %d events %d", s.Errors, s.Budget, s.GameMs, s.Events)
	}
}

// TestSummaryFromTraceFile: a summary rebuilt from the trace file equals the
// live one (the decode path of every body).
func TestSummaryFromTraceFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl.gz")
	sink, err := trace.CreateFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	live := NewCollector()
	publish(run(), sink, live)
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	evs, err := trace.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replayed := NewCollector()
	for _, e := range evs {
		if err := replayed.Add(e); err != nil {
			t.Fatal(err)
		}
	}
	a, _ := json.Marshal(live.Summary())
	b, _ := json.Marshal(replayed.Summary())
	if !bytes.Equal(a, b) {
		t.Fatalf("live and replayed summaries differ:\n%s\n%s", a, b)
	}
}

func TestSummaryEdgeCases(t *testing.T) {
	c := NewCollector()
	s := c.Summary()
	if s.Outcome != OutcomeIncomplete || len(s.Episodes) != 0 || s.Episodes == nil || s.ModelDriven {
		t.Fatalf("empty summary %+v", s)
	}
	// a scripted run is never model driven; level_end without level_start is reported
	evs := publish([]trace.Event{
		{Type: trace.TypeRunStart, Body: trace.RunStart{Backend: "scripted"}},
		{Type: trace.TypeAPICall, Body: trace.APICall{Backend: "scripted", LatencyMs: 0}},
		{Type: trace.TypeLevelEnd, Lvl: 4, Body: trace.LevelEnd{Outcome: trace.OutcomeExit}},
	})
	for i, e := range evs {
		err := c.Add(e)
		if (i == 2) != (err != nil) {
			t.Fatalf("event %d: %v", i, err)
		}
	}
	s = c.Summary()
	if s.ModelDriven || s.API.OK != 1 || s.API.ByStatus["0"] != 1 || s.Events != 3 {
		t.Fatalf("scripted summary %+v", s)
	}
	// each episode runs a fresh session: its game clock restarts at 0 and
	// the run's game time is the episodes' sum
	two := func(endSecond bool) []trace.Event {
		evs := []trace.Event{
			{Type: trace.TypeRunStart, Body: trace.RunStart{Backend: "scripted", Episodes: 2}},
			{Type: trace.TypeEpisodeStart, Ep: 0, Body: trace.EpisodeStart{Seed: 1}},
			{Type: trace.TypeLevelStart, Ep: 0, GMs: 600, Map: "demo1", Body: trace.LevelStart{Gen: 1}},
			{Type: trace.TypeLevelEnd, Ep: 0, GMs: 10000, Map: "demo1", Body: trace.LevelEnd{Outcome: trace.OutcomeExit}},
			{Type: trace.TypeEpisodeEnd, Ep: 0, GMs: 10000, Body: trace.EpisodeEnd{Outcome: "failed"}},
			{Type: trace.TypeEpisodeStart, Ep: 1, GMs: 0, Body: trace.EpisodeStart{Seed: 2}},
			{Type: trace.TypeLevelStart, Ep: 1, GMs: 500, Map: "demo1", Body: trace.LevelStart{Gen: 1}},
			{Type: trace.TypeDamage, Ep: 1, GMs: 4000, Map: "demo1", Body: trace.Damage{Amount: 3}},
		}
		if endSecond {
			evs = append(evs, trace.Event{Type: trace.TypeEpisodeEnd, Ep: 1, GMs: 5000, Body: trace.EpisodeEnd{Outcome: "failed"}},
				trace.Event{Type: trace.TypeRunEnd, Ep: 1, GMs: 5000, Body: trace.RunEnd{Outcome: "failed"}})
		}
		return evs
	}
	for _, tc := range []struct {
		ended            bool
		run, first, last int64
	}{{true, 15000, 10000, 5000}, {false, 14000, 10000, 4000}} {
		c := NewCollector()
		publish(two(tc.ended), c)
		s := c.Summary()
		if len(s.Episodes) != 2 || s.GameMs != tc.run || s.Episodes[0].GameMs != tc.first || s.Episodes[1].GameMs != tc.last {
			t.Fatalf("ended %v: game %d, episodes %+v", tc.ended, s.GameMs, s.Episodes)
		}
		if lv := s.Episodes[1].Levels; len(lv) != 1 || lv[0].StartGMs != 500 || lv[0].TimeMs != tc.last-500 {
			t.Fatalf("ended %v: second episode levels %+v", tc.ended, lv)
		}
	}

	if p := percentiles([]float64{5}); p != (Percentiles{5, 5, 5, 5}) {
		t.Fatalf("single percentile %+v", p)
	}
	// nearest rank on 1..100
	v := make([]float64, 100)
	for i := range v {
		v[100-1-i] = float64(i + 1)
	}
	if p := percentiles(v); p != (Percentiles{50, 95, 99, 100}) {
		t.Fatalf("percentiles %+v", p)
	}
}

func TestWriteJSON(t *testing.T) {
	c := NewCollector()
	publish(run(), c)
	path := filepath.Join(t.TempDir(), "run.json")
	if err := WriteJSON(path, c.Summary()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join("testdata", "run.golden.json"))
	if !bytes.Equal(b, want) {
		t.Fatal("run.json differs from the golden summary")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary files left: %v", entries)
	}
	if err := WriteJSON(filepath.Join(t.TempDir(), "missing", "run.json"), 1); err == nil {
		t.Fatal("write into a missing directory")
	}
}
