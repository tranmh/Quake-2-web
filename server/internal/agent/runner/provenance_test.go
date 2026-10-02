package runner

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
)

// tickDecision is a lane tick event body whose Intent reports target and
// fire_policy from the given sources (the other fields from the model).
func tickDecision(target, fire string) trace.Decision {
	in := &trace.Intent{}
	for f := decide.Field(0); f < decide.NumFields; f++ {
		src := trace.SourceModel
		switch f {
		case decide.FieldTarget:
			src = target
		case decide.FieldFirePolicy:
			src = fire
		}
		in.Fields = append(in.Fields, trace.Field{Name: f.ID(), Source: src})
	}
	return trace.Decision{Lane: trace.LaneTick, Intent: in}
}

// TestPolicyTickProvenance: the episode's provenance event counts what
// the bot acted on (its tick reports, overrides as reflex), with the
// arbiter's own counts beside them; request events are not ticks.
func TestPolicyTickProvenance(t *testing.T) {
	pol, err := newPolicy(policyConfig{backend: scripted.New(scripted.Config{Seed: 1}), lockstep: true,
		latency: decide.FixedLatency(0), probe: &levelProbe{logf: t.Logf}})
	if err != nil {
		t.Fatal(err)
	}
	defer pol.Close()

	// no tick reports: the arbiter's counts stand in
	p := pol.provenance()
	if p.Ticks != 0 || len(p.Fields) != int(decide.NumFields) || len(p.Arbiter) != int(decide.NumFields) {
		t.Fatalf("empty provenance %+v", p)
	}

	for i := 0; i < 10; i++ {
		target, fire := trace.SourceModel, trace.SourceScripted
		if i < 3 {
			target = trace.SourceReflex // route_kill
		}
		if i%2 == 0 {
			fire = trace.SourceReflex
		}
		d := tickDecision(target, fire)
		pol.onTick(&d)
	}
	req := trace.Decision{Lane: trace.LaneFast, Req: 7, Fields: []trace.Field{{Name: "target", Source: trace.SourceModel}}}
	pol.onTick(&req) // a request event: not a tick
	partial := trace.Decision{Lane: trace.LaneTick, Intent: &trace.Intent{Fields: []trace.Field{{Name: "mode", Source: "bogus"}}}}
	pol.onTick(&partial) // unknown source and missing fields: default

	p = pol.provenance()
	if p.Ticks != 11 {
		t.Fatalf("ticks %d", p.Ticks)
	}
	got := map[string]trace.TickField{}
	for _, f := range p.Fields {
		got[f.Name] = f
		if f.Total() != p.Ticks {
			t.Errorf("%s counts %d of %d ticks", f.Name, f.Total(), p.Ticks)
		}
	}
	if tg := got["target"]; tg.Reflex != 3 || tg.Model != 7 || tg.Default != 1 {
		t.Errorf("target %+v", tg)
	}
	if fp := got["fire_policy"]; fp.Reflex != 5 || fp.Scripted != 5 || fp.Default != 1 {
		t.Errorf("fire_policy %+v", fp)
	}
	if m := got["mode"]; m.Model != 10 || m.Default != 1 {
		t.Errorf("mode %+v", m)
	}
	for _, f := range p.Arbiter {
		if f.Reflex != 0 {
			t.Errorf("arbiter %+v has reflex", f)
		}
	}
}

// writeRun writes a one-episode run directory from bodies (published in
// order on a bus into ep-000's trace) and its run.json, and returns it.
func writeRun(t *testing.T, events []trace.Event) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "20261002T120000Z-0123abcd")
	if err := os.MkdirAll(filepath.Join(dir, EpisodeDir(0)), 0o755); err != nil {
		t.Fatal(err)
	}
	fs, err := trace.CreateFile(filepath.Join(dir, EpisodeDir(0), TraceFile), 0)
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	bus := trace.NewBus(filepath.Base(dir), func() time.Time { return t0 })
	bus.AddSink(fs)
	var all []trace.Event
	for _, e := range events {
		all = append(all, bus.Publish(e))
	}
	bus.Close()
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Summarize(all, SummarizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := metrics.WriteJSON(filepath.Join(dir, RunFile), s); err != nil {
		t.Fatal(err)
	}
	return dir
}

// provRun is a one-level mock episode with 10 tick events (target from a
// reflex on 3 of them) and the provenance event prov.
func provRun(prov trace.Provenance) []trace.Event {
	evs := []trace.Event{
		{Type: trace.TypeRunStart, Body: trace.RunStart{Schema: trace.Schema, Backend: BackendMock, ModelBackend: true,
			Session: SessionLockstep, Config: map[string]string{keyRecord: "false"}}},
		{Type: trace.TypeEpisodeStart, Body: trace.EpisodeStart{Seed: 1}},
		{Type: trace.TypeLevelStart, Map: "demo1", Body: trace.LevelStart{}},
	}
	for i := 0; i < 10; i++ {
		target := trace.SourceModel
		if i < 3 {
			target = trace.SourceReflex
		}
		evs = append(evs, trace.Event{Type: trace.TypeDecision, GMs: int64(100 * i), Map: "demo1", Body: tickDecision(target, trace.SourceModel)})
	}
	return append(evs,
		trace.Event{Type: trace.TypeLevelEnd, GMs: 1000, Map: "demo1", Body: trace.LevelEnd{Outcome: trace.OutcomeExit}},
		trace.Event{Type: trace.TypeEpisodeEnd, GMs: 1000, Body: trace.EpisodeEnd{Outcome: OutcomeCompleted}},
		trace.Event{Type: trace.TypeProvenance, GMs: 1000, Body: prov},
		trace.Event{Type: trace.TypeRunEnd, Body: trace.RunEnd{Outcome: OutcomeCompleted}},
	)
}

// provOf is the provenance of provRun's ticks, the arbiter having
// answered target from the model on all ten.
func provOf() trace.Provenance {
	p := trace.Provenance{Ticks: 10, ArbiterTicks: 10}
	for f := decide.Field(0); f < decide.NumFields; f++ {
		acted := trace.TickField{Name: f.ID(), Model: 10}
		if f == decide.FieldTarget {
			acted = trace.TickField{Name: f.ID(), Model: 7, Reflex: 3}
		}
		p.Fields = append(p.Fields, acted)
		p.Arbiter = append(p.Arbiter, trace.TickField{Name: f.ID(), Model: 10})
	}
	return p
}

// TestValidateProvenance: Validate cross-checks the provenance event
// against the tick events and the arbiter's counts, and the summary's
// tick provenance (with the reflex override) is the tick events'.
func TestValidateProvenance(t *testing.T) {
	dir := writeRun(t, provRun(provOf()))
	vr := mustValid(t, dir, ValidateOptions{})
	if tk := vr.Summary.Ticks; tk == nil || tk.TickEvents != 10 || tk.Fields["target"].Reflex != 3 || tk.Fields["target"].ModelShare != 0.7 {
		t.Fatalf("ticks %+v", vr.Summary.Ticks)
	}
	if vr.Summary.Decisions.Decisions != 0 || vr.Summary.Decisions.Ticks != 10 {
		t.Fatalf("decisions %+v", vr.Summary.Decisions)
	}

	cases := []struct {
		name string
		edit func(p *trace.Provenance)
		want string
	}{
		{"acted differs from the tick events", func(p *trace.Provenance) {
			p.Fields[decide.FieldTarget] = trace.TickField{Name: "target", Model: 10}
		}, "the tick events count"},
		{"tick count", func(p *trace.Provenance) {
			p.Ticks, p.ArbiterTicks = 11, 11
			for i := range p.Fields {
				p.Fields[i].Default++
				p.Arbiter[i].Default++
			}
		}, "the trace has 10 tick events"},
		{"arbiter below the acted", func(p *trace.Provenance) {
			p.Arbiter[decide.FieldMode] = trace.TickField{Name: "mode", Model: 8, Scripted: 2}
		}, "more than the arbiter decided"},
		{"arbiter reflex", func(p *trace.Provenance) {
			p.Arbiter[decide.FieldMode] = trace.TickField{Name: "mode", Model: 9, Reflex: 1}
		}, "arbiter field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := provOf()
			tc.edit(&p)
			vr, err := Validate(writeRun(t, provRun(p)), ValidateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if vr.OK() || !strings.Contains(strings.Join(vr.Errors, "\n"), tc.want) {
				t.Fatalf("errors %q, want %q", vr.Errors, tc.want)
			}
		})
	}
}

// TestRunMockCampaignReflex plays the campaign through demo3 on the mock
// backend: the bot's overrides (the route's kills, a dry or splash
// weapon) reach run.json as reflex ticks and lower the model shares
// below the arbiter's, and Validate's cross-check holds on a long trace.
func TestRunMockCampaignReflex(t *testing.T) {
	if !long() || raceEnabled {
		t.Skip("a mock campaign run (about 50 s; Q2_AGENT_LONG=1, without -race)")
	}
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	r, err := New(Config{FS: fs, Maps: []string{"demo1", "demo2", "demo3"}, Backend: BackendMock, Seed: 1, OutDir: t.TempDir(),
		Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Run(context.Background())
	if s == nil {
		t.Fatal(err)
	}
	mustValid(t, r.Dir(), ValidateOptions{})
	evs, _ := RunEvents(r.Dir())
	provs := bodies[trace.Provenance](t, evs, trace.TypeProvenance)
	if len(provs) != 1 || s.Ticks == nil {
		t.Fatalf("%d provenance events, ticks %+v", len(provs), s.Ticks)
	}
	arb := map[string]trace.TickField{}
	for _, f := range provs[0].Arbiter {
		arb[f.Name] = f
	}
	reflex := 0
	for _, name := range []string{"target", "fire_policy", "weapon"} {
		f := s.Ticks.Fields[name]
		reflex += f.Reflex
		if a := arb[name]; f.Reflex > 0 && f.Model >= a.Model {
			t.Errorf("%s: %d reflex ticks, yet %d model ticks acted on of the arbiter's %d", name, f.Reflex, f.Model, a.Model)
		}
		t.Logf("%s: acted %+v, arbiter %+v", name, f, arb[name])
	}
	if reflex == 0 {
		t.Fatalf("no reflex override reached run.json: %+v", s.Ticks.Fields)
	}
}

// TestMockLockstepClient: a lockstep mock run's jev client keeps its
// wall-clock state out of the answers (no breaker after a run of
// failures, no cooldown after a 429), where a realtime one protects the
// API as the client always does.
func TestMockLockstepClient(t *testing.T) {
	st := &decide.State{Mode: "fight", Me: decide.Me{HP: "ok", Health: 70, Weapon: "shotgun", Ammo: "ok", Weapons: []string{"shotgun"}},
		Enemies: []decide.Enemy{{ID: "e1", Class: "soldier", Dist: "mid", Units: 420, Visible: true, Shootable: true, Threat: "med"}}}
	classes := func(session string, faults jevtest.Faults) []jev.ErrorClass {
		b, err := newBackends(&Config{Backend: BackendMock, Session: session, Seed: 1, MockFaults: &faults}, t.Logf)
		if err != nil {
			t.Fatal(err)
		}
		defer b.close()
		var out []jev.ErrorClass
		for seq := uint64(1); seq <= 8; seq++ {
			req, err := decide.NewRequest(seq, decide.LaneFast, 1000*int64(seq), st, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = b.client.Decide(context.Background(), req)
			out = append(out, jev.ClassOf(err))
		}
		return out
	}
	for _, tc := range []struct {
		name   string
		faults jevtest.Faults
		want   jev.ErrorClass // every call's class in lockstep
		guard  jev.ErrorClass // what the realtime client answers locally
	}{
		{"server errors", jevtest.Faults{Server: 1}, jev.ClassServer, jev.ClassBreaker},
		{"rate limits", jevtest.Faults{RateLimit: 1, RetryAfter: time.Minute}, jev.ClassRateLimit, jev.ClassCooldown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, c := range classes(SessionLockstep, tc.faults) {
				if c != tc.want {
					t.Fatalf("lockstep call %d: %q, want %q", i, c, tc.want)
				}
			}
			got := classes(SessionInProc, tc.faults)
			if got[len(got)-1] != tc.guard {
				t.Fatalf("realtime calls %v: the client's own guard (%q) never acted", got, tc.guard)
			}
		})
	}
}

// TestRunJevKeyNeverWritten runs the jev backend configured from the
// environment (TYPESAFE_API_KEY, JEV_BASE_URL on a loopback fake): the
// key reaches the server, and no file of the run directory (traces
// unzipped, demos included) nor any log line holds it.
func TestRunJevKeyNeverWritten(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	const key = "sk-q2bot-test-9f3c71d2e8a45b06-never-written"
	srv := jevtest.NewServer(jevtest.Options{APIKey: key, Policy: jevtest.NewScripted(scripted.Config{Seed: 1})})
	defer srv.Close()
	env := map[string]string{jev.EnvAPIKey: key, jev.EnvBaseURL: srv.URL(), jev.EnvAllowCustomBase: "1"}
	var logs strings.Builder
	var mu sync.Mutex
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprintf(&logs, format+"\n", args...)
	}
	r, err := New(Config{FS: fs, Maps: []string{"demo1"}, Backend: BackendJev, Seed: 1, OutDir: t.TempDir(), Record: true, Trace: TraceFull,
		Jev: jev.FromEnv(func(k string) string { return env[k] }), EpisodeTimeout: gameCap(8 * time.Second), Verbose: true, Logf: logf})
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Run(context.Background())
	if s == nil {
		t.Fatal(err)
	}
	if s.API.OK == 0 || s.Backend != BackendJev {
		t.Fatalf("the jev backend made no successful call: %+v", s.API)
	}
	needles := []string{key, key[len("sk-"):], "9f3c71d2e8a45b06"}
	files := 0
	err = filepath.WalkDir(r.Dir(), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if data, err = io.ReadAll(zr); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		files++
		for _, n := range needles {
			if bytes.Contains(data, []byte(n)) {
				t.Errorf("%s holds the API key (%q)", path, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 5 {
		t.Fatalf("only %d files in the run directory", files)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, n := range needles {
		if strings.Contains(logs.String(), n) {
			t.Errorf("the log holds the API key (%q)", n)
		}
	}
	t.Logf("%d files, %d log bytes, %d api calls: no key", files, logs.Len(), s.API.Calls)
}

// TestRunJevBudgetSpentFinal runs the jev client on a loopback fake under a
// USD budget the run degrades along: run.json's budget must report the
// whole spend (the API's cost), not the spend when the fast lane's rate
// last changed. A live run once reported $0.50 of $0.61 spent.
func TestRunJevBudgetSpentFinal(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	srv := jevtest.NewServer(jevtest.Options{APIKey: "sk-test", Policy: jevtest.NewScripted(scripted.Config{Seed: 1})})
	defer srv.Close()
	env := map[string]string{jev.EnvAPIKey: "sk-test", jev.EnvBaseURL: srv.URL(), jev.EnvAllowCustomBase: "1"}
	run := func(lim budget.Limits) *metrics.RunSummary {
		t.Helper()
		_, s, err := runDemo1(t, fs, Config{Backend: BackendJev, Seed: 1, Budget: lim,
			Jev: jev.FromEnv(func(k string) string { return env[k] }), EpisodeTimeout: gameCap(8 * time.Second)})
		if s == nil {
			t.Fatal(err)
		}
		return s
	}
	free := run(budget.Limits{})
	if free.API.CostUSD <= 0 {
		t.Fatalf("the fake reports no cost: %+v", free.API)
	}
	// a cap the run degrades on (past half of it) but does not exhaust: it
	// keeps spending at the lower rate after the last budget event
	s := run(budget.Limits{USD: 1.6 * free.API.CostUSD})
	if s.Budget == nil || s.Budget.RateHz >= budget.DefaultFullHz || s.Budget.ScriptedOnly {
		t.Fatalf("the budget never degraded: %+v", s.Budget)
	}
	if d := s.Budget.SpentUSD - s.API.CostUSD; d > 1e-9 || d < -1e-9 {
		t.Fatalf("budget spent $%.9f, api cost $%.9f", s.Budget.SpentUSD, s.API.CostUSD)
	}
	t.Logf("spent $%.9f of $%.9f at %g Hz", s.Budget.SpentUSD, s.Budget.LimitUSD, s.Budget.RateHz)
}
