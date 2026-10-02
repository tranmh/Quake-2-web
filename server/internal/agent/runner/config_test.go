package runner

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/qcommon/shared"
)

func TestParseLatency(t *testing.T) {
	cases := map[string]Latency{
		"":                {},
		"212ms":           {Fixed: 212 * time.Millisecond, Set: true},
		"0":               {Set: true},
		"80ms, 150ms,1s":  {Samples: []time.Duration{80 * time.Millisecond, 150 * time.Millisecond, time.Second}, Set: true},
		"100ms,100ms,1ms": {Samples: []time.Duration{100 * time.Millisecond, 100 * time.Millisecond, time.Millisecond}, Set: true},
	}
	for in, want := range cases {
		got, err := ParseLatency(in)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %+v %v", in, got, err)
			continue
		}
		if again, err := ParseLatency(got.String()); err != nil || !reflect.DeepEqual(again, want) {
			t.Errorf("%q: round trip %q -> %+v %v", in, got.String(), again, err)
		}
	}
	for _, bad := range []string{"fast", "-5ms", "10ms,x"} {
		if _, err := ParseLatency(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	l, _ := ParseLatency("100ms,300ms")
	if l.meanMs() != 200 {
		t.Errorf("mean %v", l.meanMs())
	}
	req := &decide.Request{Seq: 9}
	if d := l.model(3).Latency(req); d != 100*time.Millisecond && d != 300*time.Millisecond {
		t.Errorf("sampled %v", d)
	}
	if d := (Latency{Fixed: 50 * time.Millisecond, Set: true}).model(1).Latency(req); d != 50*time.Millisecond {
		t.Errorf("fixed %v", d)
	}
}

func TestParseTrace(t *testing.T) {
	for in, want := range map[string]TraceMode{"": TraceFull, "full": TraceFull, "digest": TraceDigest, "every:10": TraceMode(10)} {
		got, err := ParseTrace(in)
		if err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
		if again, _ := ParseTrace(got.String()); again != got {
			t.Errorf("%q: round trip %q", in, got.String())
		}
	}
	for _, bad := range []string{"every:0", "every:x", "all"} {
		if _, err := ParseTrace(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	m := TraceMode(3)
	for n, full := range []bool{true, false, false, true, false} {
		if o := m.options(n); o.State != full || o.Questions != full {
			t.Errorf("every:3 decision %d: %+v", n, o)
		}
	}
	if o := TraceDigest.options(0); o.State {
		t.Error("digest mode logs states")
	}
}

func campaignOf(maps ...string) *route.Campaign {
	c := &route.Campaign{Name: "test", Skill: 1, Terminal: route.Terminal{Exit: "victory.pcx"}}
	for _, m := range maps {
		c.Tables = append(c.Tables, &route.Table{Map: m})
	}
	return c
}

func TestSelectVisits(t *testing.T) {
	camp := campaignOf("demo1", "demo2", "demo3", "demo2")
	cases := []struct {
		maps   []string
		played []string
		stop   int
		err    string
	}{
		{nil, []string{"demo1", "demo2", "demo3", "demo2"}, 0, ""},
		{[]string{"demo1"}, []string{"demo1"}, 1, ""},
		{[]string{"demo1", "demo2"}, []string{"demo1", "demo2"}, 2, ""},
		{[]string{" Demo1", "demo3", "demo2"}, []string{"demo1", "demo2", "demo3", "demo2"}, 0, ""},
		{[]string{"demo2"}, nil, 0, "must include the campaign's start"},
		{[]string{"demo1", "demo3"}, nil, 0, "demo3 is not among"},
	}
	for _, tc := range cases {
		played, stop, err := selectVisits(camp, tc.maps)
		switch {
		case tc.err != "":
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%v: %v, want %q", tc.maps, err, tc.err)
			}
		case err != nil || !reflect.DeepEqual(played, tc.played) || stop != tc.stop:
			t.Errorf("%v: %v %d %v", tc.maps, played, stop, err)
		}
	}
	if _, _, err := selectVisits(campaignOf(), nil); err == nil {
		t.Error("an empty campaign accepted")
	}
}

type nopFS struct{}

func (nopFS) ReadFile(string) ([]byte, error) { return nil, errors.New("no data") }

func TestConfigCheck(t *testing.T) {
	skill := 7
	bad := []struct {
		cfg  Config
		want string
	}{
		{Config{}, "no game data"},
		{Config{FS: nopFS{}, Backend: "gpt"}, "unknown backend"},
		{Config{FS: nopFS{}, Session: "remote"}, "unknown session"},
		{Config{FS: nopFS{}, Backend: BackendReplay}, "needs a recorded trace"},
		{Config{FS: nopFS{}, Backend: BackendReplay, ReplayTrace: "x", Session: SessionInProc}, "needs a lockstep session"},
		{Config{FS: nopFS{}, Episodes: -1}, "episodes"},
		{Config{FS: nopFS{}, Skill: &skill}, "skill 7"},
		{Config{FS: nopFS{}, MinModelShare: 1.5}, "gate thresholds"},
		{Config{FS: nopFS{}, Budget: budget.Limits{USD: -1}}, "negative budget"},
	}
	for _, tc := range bad {
		c := tc.cfg
		err := c.check()
		if err == nil || !errors.Is(err, ErrConfig) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: %v, want %q", tc.cfg, err, tc.want)
		}
	}
	c := Config{FS: nopFS{}}
	if err := c.check(); err != nil || c.Backend != BackendScripted || c.Session != SessionLockstep || c.Episodes != 1 ||
		c.OutDir != "runs" || c.Now == nil {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	lat := func(c Config, recorded string) Latency {
		return (&Runner{cfg: c, be: &backends{recordedKind: recorded}}).latency()
	}
	if l := lat(c, ""); !l.Set || l.Fixed != 0 {
		t.Errorf("scripted latency %+v", l)
	}
	c.Backend = BackendMock
	if l := lat(c, ""); l.Fixed != DefaultModelLatency {
		t.Errorf("mock latency %+v", l)
	}
	c.Backend = BackendReplay
	if l := lat(c, BackendMock); l.Fixed != DefaultModelLatency {
		t.Errorf("the replay of a mock run: latency %+v", l)
	}
	if l := lat(c, BackendScripted); l.Fixed != 0 || !l.Set {
		t.Errorf("the replay of a scripted run: latency %+v", l)
	}
	c.SimLatency = Latency{Fixed: time.Second, Set: true}
	if l := lat(c, BackendMock); l.Fixed != time.Second {
		t.Errorf("a configured latency: %+v", l)
	}
	// New refuses a bad config before touching the disk
	if _, err := New(Config{FS: nopFS{}, Backend: "x", OutDir: t.TempDir()}); !errors.Is(err, ErrConfig) {
		t.Errorf("New: %v", err)
	}
	if _, err := New(Config{FS: nopFS{}, RunID: "../escape", OutDir: t.TempDir(), Campaign: campaignOf("demo1")}); !errors.Is(err, ErrConfig) {
		t.Errorf("run id: %v", err)
	}
}

func TestRunID(t *testing.T) {
	now := time.Date(2026, 10, 2, 15, 30, 45, 0, time.FixedZone("x", 3600))
	a, err := NewRunID(now)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewRunID(now)
	if !strings.HasPrefix(a, "20261002T143045Z-") || len(a) != len("20261002T143045Z-")+8 || a == b || !ValidRunID(a) {
		t.Fatalf("ids %q %q", a, b)
	}
	for _, bad := range []string{"", ".hidden", "a/b", "a..b/c", strings.Repeat("x", 200)} {
		if ValidRunID(bad) {
			t.Errorf("%q valid", bad)
		}
	}
}

// TestRunStartRoundTrip: what a replay rebuilds from run_start is the
// run's episode-shaping configuration.
func TestRunStartRoundTrip(t *testing.T) {
	skill := 2
	cfg := Config{FS: nopFS{}, Backend: BackendMock, Seed: 41, Episodes: 3, Skill: &skill, Trace: TraceMode(4), Record: true,
		SimLatency:    Latency{Samples: []time.Duration{100 * time.Millisecond, 300 * time.Millisecond}, Set: true},
		EntryCommands: []string{"god", "notarget"},
		Budget:        budget.Limits{USD: 2, Queries: 500, MaxQPS: 8, OnExhausted: budget.Stop},
		MinModelShare: 0.75, MaxStaleRate: 0.1, LevelTimeout: 5 * time.Minute, EpisodeTimeout: time.Hour, MaxDeaths: 3}
	if err := cfg.check(); err != nil {
		t.Fatal(err)
	}
	r := &Runner{cfg: cfg, camp: campaignOf("demo1", "demo2"), visits: []string{"demo1"}, stopAfter: 1, skill: 2,
		be: &backends{kind: BackendMock, model: "jev-1.13.0"}, bud: budget.New(cfg.Budget, nil)}
	rs := r.runStartBody()
	if rs.Backend != BackendMock || !rs.ModelBackend || rs.Model != "jev-1.13.0" || rs.SimLatencyMs != 200 || rs.BudgetUSD != 2 ||
		rs.Skill != 2 || rs.Episodes != 3 {
		t.Fatalf("run_start %+v", rs)
	}
	bus := trace.NewBus("run-1", nil)
	e := bus.Publish(trace.Event{Type: trace.TypeRunStart, Body: rs})
	raw, _ := json.Marshal(e)
	var dec trace.Event
	if err := json.Unmarshal(raw, &dec); err != nil {
		t.Fatal(err)
	}
	rec, err := configFromRunStart(dec)
	if err != nil {
		t.Fatal(err)
	}
	got := rec.cfg
	if rec.run != "run-1" || got.Backend != cfg.Backend || got.Session != cfg.Session || got.Seed != 41 || *got.Skill != 2 ||
		got.Trace != cfg.Trace || !got.Record || got.SimLatency.String() != cfg.SimLatency.String() ||
		!reflect.DeepEqual(got.EntryCommands, cfg.EntryCommands) || got.Budget.USD != 2 || got.Budget.Queries != 500 ||
		got.Budget.MaxQPS != 8 || got.Budget.OnExhausted != budget.Stop || got.MinModelShare != 0.75 || got.MaxStaleRate != 0.1 ||
		got.LevelTimeout != cfg.LevelTimeout || got.EpisodeTimeout != cfg.EpisodeTimeout || got.MaxDeaths != 3 ||
		!reflect.DeepEqual(got.Maps, []string{"demo1"}) {
		t.Fatalf("rebuilt %+v", got)
	}
	if m, s := gateFromRunStart(rec.start); m != 0.75 || s != 0.1 {
		t.Fatalf("gate %v %v", m, s)
	}
	bad := trace.Event{Type: trace.TypeRunStart, Body: trace.RunStart{Schema: "other/1"}}
	if _, err := configFromRunStart(bad); err == nil {
		t.Fatal("foreign schema accepted")
	}
}

func TestDiffJSON(t *testing.T) {
	want := json.RawMessage(`{"me":{"hp":"ok","health":80},"enemies":[{"id":"e1","dist":"mid"}],"space":{"front":"open"}}`)
	got := json.RawMessage(`{"me":{"hp":"ok","health":75},"enemies":[{"id":"e1","dist":"far"},{"id":"e2"}],"items":[]}`)
	d := diffJSON(want, got, 50)
	paths := map[string]string{}
	for _, e := range d {
		paths[e.Path] = string(e.Want) + "|" + string(e.Got)
	}
	exp := map[string]string{
		"$.me.health":       "80|75",
		"$.enemies[0].dist": `"mid"|"far"`,
		"$.enemies[1]":      `|{"id":"e2"}`,
		"$.space":           `{"front":"open"}|`,
		"$.items":           `|[]`,
	}
	if !reflect.DeepEqual(paths, exp) {
		t.Fatalf("diff %v", paths)
	}
	if len(diffJSON(want, want, 50)) != 0 || len(diffJSON(want, got, 2)) != 2 {
		t.Fatal("equal or limited diffs")
	}
	if d := diffJSON(json.RawMessage(`{`), json.RawMessage(`1`), 5); len(d) != 1 || d[0].Path != "$" {
		t.Fatalf("invalid json %v", d)
	}
}

// TestComparator feeds a recorded stream back with one change, one cut and
// one addition.
func TestComparator(t *testing.T) {
	bus := trace.NewBus("rec", nil)
	var rec []trace.Event
	mk := func(typ string, gms int64, body any) trace.Event {
		return bus.Publish(trace.Event{Type: typ, GMs: gms, Body: body})
	}
	rec = append(rec,
		mk(trace.TypeEpisodeStart, 0, trace.EpisodeStart{Seed: 1}),
		mk(trace.TypeDecision, 100, trace.Decision{Lane: "fast", Req: 1, State: json.RawMessage(`{"me":{"hp":"ok"}}`), StateDigest: "a"}),
		mk(trace.TypeAPICall, 100, trace.APICall{Lane: "fast", Req: 1, Status: 200, Retry: 1}),
		mk(trace.TypeEpisodeEnd, 200, trace.EpisodeEnd{Outcome: "completed"}),
	)
	run := func(strict bool, evs ...trace.Event) (*comparator, bool) {
		cancelled := false
		c := &comparator{want: rec, strict: strict, cancel: func() { cancelled = true }}
		other := trace.NewBus("replay", nil) // another run id and seq: not compared
		other.AddSink(c)
		other.Publish(trace.Event{Type: trace.TypeRunStart, Body: trace.RunStart{}})
		for _, e := range evs {
			e.Seq, e.Run, e.Wall = 0, "", 0
			other.Publish(e)
		}
		c.finish()
		return c, cancelled
	}
	// the same events (an api call without transport details, a decision
	// without its state) match
	same := append([]trace.Event(nil), rec...)
	same[2] = trace.Event{Type: trace.TypeAPICall, GMs: 100, Body: trace.APICall{Lane: "fast", Req: 1}}
	same[1] = trace.Event{Type: trace.TypeDecision, GMs: 100, Body: trace.Decision{Lane: "fast", Req: 1, StateDigest: "a"}}
	c, _ := run(true, same...)
	var r ReplayReport
	c.report(&r)
	if r.Divergence != nil || r.Matched != 4 || r.Mismatches != 0 {
		t.Fatalf("same: %+v", r)
	}

	// a changed decision: the lane states are diffed
	changed := append([]trace.Event(nil), rec...)
	changed[1] = trace.Event{Type: trace.TypeDecision, GMs: 100, Body: trace.Decision{Lane: "fast", Req: 1,
		State: json.RawMessage(`{"me":{"hp":"low"}}`), StateDigest: "b"}}
	c, cancelled := run(true, changed...)
	r = ReplayReport{}
	c.report(&r)
	d := r.Divergence
	if d == nil || d.Kind != "event" || d.Index != 1 || d.Type != trace.TypeDecision || !d.StateDiff || !cancelled ||
		len(d.Diff) != 1 || d.Diff[0].Path != "$.me.hp" {
		t.Fatalf("changed: %+v", d)
	}
	// not strict: it goes on counting
	c, cancelled = run(false, changed...)
	r = ReplayReport{}
	c.report(&r)
	if cancelled || r.Mismatches != 1 || r.Matched != 3 {
		t.Fatalf("lenient: %+v", r)
	}
	// cut short, and run past the end
	c, _ = run(false, rec[:2]...)
	r = ReplayReport{}
	c.report(&r)
	if r.Divergence == nil || r.Divergence.Kind != "missing" || r.Divergence.Index != 2 || r.Mismatches != 2 {
		t.Fatalf("missing: %+v", r.Divergence)
	}
	c, _ = run(false, append(append([]trace.Event(nil), rec...), trace.Event{Type: trace.TypeError, Body: trace.Error{Msg: "x"}})...)
	r = ReplayReport{}
	c.report(&r)
	if r.Divergence == nil || r.Divergence.Kind != "extra" || r.Divergence.Index != 4 {
		t.Fatalf("extra: %+v", r.Divergence)
	}

	// the usercmd feed sends the recorded command and notes the bot's
	cc := &comparator{cancel: func() {}}
	rec1 := trace.StepCmd{UserCmd: trace.UserCmd{Msec: 25, Forward: 400}}
	feed := cc.feed(map[int64][]trace.StepCmd{3: {rec1}})
	if got := feed(3, 0, shared.UserCmd{Msec: 25, ForwardMove: 400}); got.ForwardMove != 400 || cc.badCmds != 0 {
		t.Fatalf("equal command: %+v", got)
	}
	if got := feed(3, 0, shared.UserCmd{Msec: 25, SideMove: 400}); got.ForwardMove != 400 || got.SideMove != 0 || cc.badCmds != 1 ||
		cc.first == nil || cc.first.Kind != "cmd" || cc.first.Step != 3 {
		t.Fatalf("differing command: %+v %+v", got, cc.first)
	}
	if got := feed(4, 0, shared.UserCmd{Msec: 25, UpMove: 9}); got.UpMove != 9 || cc.badCmds != 2 {
		t.Fatalf("unrecorded step: %+v", got)
	}
}
