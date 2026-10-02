package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/trace"
)

// TestRunConfig: run.json's config section is run_start mapped, the same
// from the published body and from the trace's JSON, with the mock
// policy's noise in effect and the budget; parts a run_start lacks are
// left empty.
func TestRunConfig(t *testing.T) {
	skill := 2
	cfg := Config{FS: nopFS{}, Backend: BackendMock, Seed: 41, Episodes: 3, Skill: &skill, Trace: TraceMode(4), Record: true,
		SimLatency:    Latency{Samples: []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}, Set: true},
		EntryCommands: []string{"god", "notarget"}, MockSwap: -1, MockNoise: 0.5,
		Budget:        budget.Limits{USD: 2, Queries: 500, MaxQPS: 8, OnExhausted: budget.Stop},
		MinModelShare: 0.75, MaxStaleRate: 0.1, LevelTimeout: 5 * time.Minute, EpisodeTimeout: time.Hour, MaxDeaths: -1}
	if err := cfg.check(); err != nil {
		t.Fatal(err)
	}
	r := &Runner{cfg: cfg, camp: campaignOf("demo1", "demo2"), visits: []string{"demo1"}, stopAfter: 1, skill: 2,
		be: &backends{kind: BackendMock, model: "jev-1.13.0"}, bud: budget.New(cfg.Budget, nil)}
	rs := r.runStartBody()
	got := runConfig(rs)

	// what Summarize gets: the body through the trace's JSON
	raw, err := json.Marshal(trace.NewBus("run-1", nil).Publish(trace.Event{Type: trace.TypeRunStart, Body: rs}))
	if err != nil {
		t.Fatal(err)
	}
	var e trace.Event
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	var dec trace.RunStart
	if err := e.DecodeBody(&dec); err != nil {
		t.Fatal(err)
	}
	if again := runConfig(dec); !reflect.DeepEqual(again, got) {
		t.Fatalf("from the trace %+v, from the body %+v", again, got)
	}

	want := &metrics.RunConfig{Backend: BackendMock, ModelBackend: true, Model: "jev-1.13.0", Session: SessionLockstep,
		Campaign: r.camp.Name, Maps: []string{"demo1"}, StopAfter: 1, Skill: 2, Seed: 41, Episodes: 3,
		SimLatency: "100ms,300ms", SimLatencyMs: 200,
		Mock: &metrics.MockConfig{Policy: MockPolicyNoisy, Noise: &metrics.MockNoise{Noise: 0.5, Swap: 0, LowConfidence: 0.1},
			Faults: rs.Config[keyMockFaults]},
		Budget:    &metrics.BudgetConfig{USD: 2, Queries: 500, MaxQPS: 8, OnExhausted: string(budget.Stop)},
		MaxDeaths: -1, LevelTimeout: "5m0s", EpisodeTimeout: "1h0m0s", MinModelShare: 0.75, MaxStaleRate: 0.1,
		EntryCommands: []string{"god", "notarget"}, Trace: "every:4", Record: true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config\n%+v\nwant\n%+v", got, want)
	}
	if !strings.HasPrefix(got.Mock.Faults, "seed=5 server=0.02 missing=0.02") {
		t.Fatalf("mock faults %q", got.Mock.Faults)
	}

	// the clean mock: no noise
	r.cfg.MockPolicy = MockPolicyScripted
	if m := runConfig(r.runStartBody()).Mock; m == nil || m.Policy != MockPolicyScripted || m.Noise != nil {
		t.Fatalf("clean mock %+v", m)
	}

	// a local backend without a budget: no mock, no budget, no gate asked
	sc := Config{FS: nopFS{}}
	if err := sc.check(); err != nil {
		t.Fatal(err)
	}
	r = &Runner{cfg: sc, camp: campaignOf("demo1"), visits: []string{"demo1"}, skill: 1, be: &backends{kind: BackendScripted}}
	if c := runConfig(r.runStartBody()); c.Backend != BackendScripted || c.ModelBackend || c.Mock != nil || c.Budget != nil ||
		c.SimLatency != "0s" || c.Episodes != 1 || c.MinModelShare != 0 || c.MaxDeaths != 0 || c.Record {
		t.Fatalf("scripted config %+v", c)
	}

	// a run_start from before mock.policy was recorded
	old := trace.RunStart{Schema: trace.Schema, Backend: BackendMock, ModelBackend: true, Session: SessionLockstep,
		Config: map[string]string{keyMockFaults: "seed=5 server=0.02"}}
	if c := runConfig(old); c.Mock == nil || c.Mock.Policy != "" || c.Mock.Noise != nil || c.Mock.Faults != "seed=5 server=0.02" ||
		c.Episodes != 1 || c.Budget != nil {
		t.Fatalf("old mock config %+v (mock %+v)", c, c.Mock)
	}
}

func TestParseMockPolicy(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		policy string
		noise  *metrics.MockNoise
	}{
		{"", "", nil},
		{"scripted", "scripted", nil},
		{"noisy noise=0.3 swap=0.1 lowconf=0.1", "noisy", &metrics.MockNoise{Noise: 0.3, Swap: 0.1, LowConfidence: 0.1}},
		{"noisy noise=0 swap=0.25 lowconf=0", "noisy", &metrics.MockNoise{Swap: 0.25}},
		{"noisy noise=0.3 swap=x lowconf=0.1", "noisy", nil},
		{"noisy noise=0.3", "noisy", nil},
	} {
		p, n := parseMockPolicy(tc.desc)
		if p != tc.policy || !reflect.DeepEqual(n, tc.noise) {
			t.Errorf("%q: %q %+v, want %q %+v", tc.desc, p, n, tc.policy, tc.noise)
		}
	}
	for _, c := range []Config{{MockPolicy: MockPolicyNoisy}, {MockPolicy: MockPolicyNoisy, MockNoise: -1, MockSwap: 0.2},
		{MockPolicy: MockPolicyScripted}} {
		if p, n := parseMockPolicy(mockPolicyDesc(&c)); p != c.MockPolicy || (n != nil) != (c.MockPolicy == MockPolicyNoisy) {
			t.Errorf("%+v: %q %+v", c, p, n)
		}
	}
}

// TestValidateRunJSONConfig: a run.json carries the config section its
// traces recompute; one written before the section existed still
// validates (with a warning), and is otherwise still compared.
func TestValidateRunJSONConfig(t *testing.T) {
	dir := writeRun(t, provRun(provOf()))
	vr := mustValid(t, dir, ValidateOptions{})
	if c := vr.Summary.Config; c == nil || c.Backend != BackendMock || c.Mock == nil || c.Record {
		t.Fatalf("config %+v", vr.Summary.Config)
	}
	for _, w := range vr.Warnings {
		if strings.Contains(w, "config") {
			t.Fatalf("warning on a run.json with its config: %q", w)
		}
	}

	path := filepath.Join(dir, RunFile)
	edit := func(f func(m map[string]any)) {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		if err := metrics.WriteJSON(path, m); err != nil {
			t.Fatal(err)
		}
	}
	edit(func(m map[string]any) { delete(m, "config") })
	vr = mustValid(t, dir, ValidateOptions{})
	if !strings.Contains(strings.Join(vr.Warnings, "\n"), "no config section") {
		t.Fatalf("warnings %q", vr.Warnings)
	}
	edit(func(m map[string]any) { m["outcome"] = "failed" })
	if vr, err := Validate(dir, ValidateOptions{}); err != nil || vr.OK() {
		t.Fatalf("an old run.json that does not match its traces: %v, errors %q", err, vr.Errors)
	}

	// a changed config section does not match
	dir = writeRun(t, provRun(provOf()))
	path = filepath.Join(dir, RunFile)
	edit(func(m map[string]any) { m["config"].(map[string]any)["skill"] = 3 })
	if vr, err := Validate(dir, ValidateOptions{}); err != nil || vr.OK() {
		t.Fatalf("a run.json whose config differs from its trace: %v, errors %q", err, vr.Errors)
	}
	// summarize puts it back
	s, err := SummarizeDir(dir, SummarizeOptions{})
	if err != nil || s.Config == nil || s.Config.Skill != 0 {
		t.Fatalf("summarized config %+v: %v", s.Config, err)
	}
}
