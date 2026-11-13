package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]string{"": "off", "off": "off", "exact": "exact", "approx": "approx:fine",
		"approx:fine": "approx:fine", " APPROX:coarse ": "approx:coarse"} {
		c, err := Parse(in)
		if err != nil || c.String() != want {
			t.Errorf("Parse(%q) = %v, %v; want %s", in, c, err, want)
		}
		if again, _ := Parse(c.String()); again != c {
			t.Errorf("%q does not round-trip", c)
		}
	}
	if _, err := Parse("fuzzy"); err == nil {
		t.Error("Parse(fuzzy) accepted")
	}
	if c, _ := Parse("approx:coarse"); c.Marker() != MarkApprox || !c.Enabled() {
		t.Errorf("approx marker %q", c.Marker())
	}
	if c, _ := Parse("off"); c.Marker() != "" || c.Enabled() {
		t.Error("off is enabled")
	}
}

func intp(v int) *int           { return &v }
func floatp(v float64) *float64 { return &v }

// baseState is a slow-lane state with every field set.
func baseState() decide.State {
	return decide.State{
		Mode: "fight",
		Me: decide.Me{HP: "ok", Health: 70, Armor: 10, Weapon: "shotgun", Ammo: "ok", OnGround: true, DamageLast1s: 5,
			HitFromBearing: intp(30), Moving: "strafe", TargetSinceS: floatp(1.2), Weapons: []string{"blaster", "shotgun"}},
		Enemies: []decide.Enemy{
			{ID: "e1", Class: "soldier", Bearing: 30, Elev: -2, Dist: "mid", Units: 420, Visible: true, Shootable: true, Aim: "near",
				State: "attacking", Threat: "med", Current: true},
			{ID: "e2", Class: "gunner", Bearing: 10, Elev: 1, Dist: "far", Units: 900, Visible: true, Aim: "off", State: "alert",
				Threat: "high", Wounded: true},
		},
		Incoming:  []decide.Incoming{{Kind: "rocket", Bearing: 30, ETA: "soon", Dodge: "left"}},
		Space:     &decide.Space{Front: "open", Back: "tight", Left: "open", Right: "blocked"},
		Items:     []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Bearing: 100, Path: 300}},
		Objective: &decide.Objective{Kind: "goto", Desc: "press button *34", Bearing: 30, Path: 800, Next: 100},
		Level:     &decide.LevelInfo{Map: "demo1", Kills: "3/20", Secrets: "0/3", DeathsHere: 1},
		Events:    []string{"took 12 damage from e1 0.4s ago", "spotted e2 monster_gunner 1.5s ago"},
	}
}

func keyOf(t *testing.T, g Granularity, lane decide.Lane, mapName string, st decide.State) string {
	t.Helper()
	req, err := decide.NewRequest(1, lane, 1000, &st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return Config{Mode: Approx, Granularity: g}.Key(req, mapName)
}

// TestApproxKey: states that differ only in dropped fields or within a
// bucket share a key (their option descriptions differ too: the key
// ignores them); a bucketed field or an option key changes it. The coarse
// key merges what the fine one tells apart.
func TestApproxKey(t *testing.T) {
	both := []Granularity{Fine, Coarse}
	same := map[string]func(*decide.State){
		"health within its bucket":     func(s *decide.State) { s.Me.Health = 80 },
		"armor within its band":        func(s *decide.State) { s.Me.Armor = 20 },
		"damage within its band":       func(s *decide.State) { s.Me.DamageLast1s = 9 },
		"hit bearing within a sector":  func(s *decide.State) { s.Me.HitFromBearing = intp(40) },
		"target since within its band": func(s *decide.State) { s.Me.TargetSinceS = floatp(1.8) },
		"enemy units within dist":      func(s *decide.State) { s.Enemies[0].Units = 600 },
		"enemy elev":                   func(s *decide.State) { s.Enemies[0].Elev = 25 },
		"enemy bearing within sector":  func(s *decide.State) { s.Enemies[0].Bearing = 40 },
		"incoming bearing in sector":   func(s *decide.State) { s.Incoming[0].Bearing = 50 },
		"item path within its bucket":  func(s *decide.State) { s.Items[0].Path = 600 },
		"item bearing within a sector": func(s *decide.State) { s.Items[0].Bearing = 95 },
		"objective path in its bucket": func(s *decide.State) { s.Objective.Path = 1500 },
		"objective next in its sector": func(s *decide.State) { s.Objective.Next = 80 },
		"kills and secrets":            func(s *decide.State) { s.Level.Kills, s.Level.Secrets = "4/20", "1/3" },
		"event ages and amounts": func(s *decide.State) {
			s.Events[0], s.Events[1] = "took 7 damage from e1 0.9s ago", "spotted e2 monster_gunner 2.0s ago"
		},
	}
	differ := map[string]func(*decide.State){
		"hp bucket":         func(s *decide.State) { s.Me.HP, s.Me.Health = "low", 30 },
		"armor none":        func(s *decide.State) { s.Me.Armor = 0 },
		"damage none":       func(s *decide.State) { s.Me.DamageLast1s = 0 },
		"no target since":   func(s *decide.State) { s.Me.TargetSinceS = nil },
		"enemy dist bucket": func(s *decide.State) { s.Enemies[0].Dist, s.Enemies[0].Units = "far", 800 },
		"enemy behind":      func(s *decide.State) { s.Enemies[0].Bearing = 170 },
		"enemy aim":         func(s *decide.State) { s.Enemies[0].Aim = "on" },
		"enemy visible":     func(s *decide.State) { s.Enemies[0].Visible = false },
		"enemy order":       func(s *decide.State) { s.Enemies[0], s.Enemies[1] = s.Enemies[1], s.Enemies[0] },
		"another enemy id":  func(s *decide.State) { s.Enemies[1].ID = "e3" },
		"an enemy more": func(s *decide.State) {
			s.Enemies = append(s.Enemies, decide.Enemy{ID: "e9", Class: "flyer", Dist: "far", Aim: "off", State: "idle", Threat: "low"})
		},
		"another item":    func(s *decide.State) { s.Items[0].ID = "i2" },
		"mode":            func(s *decide.State) { s.Mode = "explore" },
		"objective":       func(s *decide.State) { s.Objective.Desc = "press button *35" },
		"deaths":          func(s *decide.State) { s.Level.DeathsHere = 0 },
		"an event":        func(s *decide.State) { s.Events = s.Events[:1] },
		"incoming dodge":  func(s *decide.State) { s.Incoming[0].Dodge = "right" },
		"space open":      func(s *decide.State) { s.Space.Back = "open" },
		"weapons":         func(s *decide.State) { s.Me.Weapons = append(s.Me.Weapons, "machinegun") },
		"weapon in hand":  func(s *decide.State) { s.Me.Weapon = "blaster" },
		"objective sides": func(s *decide.State) { s.Objective.Bearing = -100 },
	}
	coarseOnly := map[string]func(*decide.State){
		"enemy bearing across fine sectors": func(s *decide.State) { s.Enemies[0].Bearing = 10 },
		"damage across fine bands":          func(s *decide.State) { s.Me.DamageLast1s = 30 },
		"wounded":                           func(s *decide.State) { s.Enemies[1].Wounded = false },
		"armor across fine bands":           func(s *decide.State) { s.Me.Armor = 150 },
		"target since across fine bands":    func(s *decide.State) { s.Me.TargetSinceS = floatp(0.5) },
		"incoming bearing":                  func(s *decide.State) { s.Incoming[0].Bearing = -120 },
		"space tight to blocked":            func(s *decide.State) { s.Space.Back = "blocked" },
		"on ground":                         func(s *decide.State) { s.Me.OnGround = false },
		"event order":                       func(s *decide.State) { s.Events[0], s.Events[1] = s.Events[1], s.Events[0] },
		"item bearing":                      func(s *decide.State) { s.Items[0].Bearing = -100 },
		"objective next":                    func(s *decide.State) { s.Objective.Next = -100 },
		"deaths 1 to 2":                     func(s *decide.State) { s.Level.DeathsHere = 2 },
	}
	for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
		for _, g := range both {
			base := keyOf(t, g, lane, "demo1", baseState())
			if base == "" {
				t.Fatal("no key")
			}
			check := func(name string, edit func(*decide.State), want bool) {
				st := baseState()
				edit(&st)
				if got := keyOf(t, g, lane, "demo1", st) == base; got != want {
					t.Errorf("%s %v: %s: same key %v, want %v", lane, g, name, got, want)
				}
			}
			for name, edit := range same {
				check(name, edit, true)
			}
			for name, edit := range differ {
				if lane == decide.LaneFast && (name == "mode" || name == "weapons" || name == "another item" || name == "objective" ||
					name == "deaths" || name == "an event" || name == "objective sides") {
					continue // slow-lane parts: a fast request carries none of them
				}
				check(name, edit, false)
			}
			for name, edit := range coarseOnly {
				if lane == decide.LaneFast && (name == "event order" || name == "item bearing" || name == "objective next" ||
					name == "deaths 1 to 2") {
					continue
				}
				check(name, edit, g == Coarse)
			}
			if keyOf(t, g, lane, "demo2", baseState()) == base {
				t.Errorf("%s %v: another map shares the key", lane, g)
			}
		}
	}
	if keyOf(t, Fine, decide.LaneFast, "demo1", baseState()) == keyOf(t, Fine, decide.LaneSlow, "demo1", baseState()) {
		t.Error("the lanes share a key")
	}
	if keyOf(t, Fine, decide.LaneSlow, "demo1", baseState()) == keyOf(t, Coarse, decide.LaneSlow, "demo1", baseState()) {
		t.Error("fine and coarse share a key")
	}
}

// TestKeyFromTrace: a request's key from its typed view (live) equals its
// key from the encoded state and questions a trace records (cache-sim).
func TestKeyFromTrace(t *testing.T) {
	for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
		st := baseState()
		req, err := decide.NewRequest(7, lane, 1000, &st, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		var dec decide.State
		if err := json.Unmarshal(req.State, &dec); err != nil {
			t.Fatal(err)
		}
		qs, err := decide.ParseQuestions(req.QuestionsJSON())
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"exact", "approx:fine", "approx:coarse"} {
			cfg, _ := Parse(mode)
			live, rec := cfg.Key(req, "demo1"), cfg.KeyOf(lane, "demo1", &dec, qs, req.Digest())
			if live == "" || live != rec {
				t.Errorf("%s %s: live %q, from the trace %q", lane, mode, live, rec)
			}
		}
	}
}

func TestEventTextAndSectors(t *testing.T) {
	for in, want := range map[string]string{
		"took 12 damage from e3 1.5s ago": "took damage from e3",
		"took 4 damage (lava) 0.0s ago":   "took damage (lava)",
		"e3 died 2.5s ago":                "e3 died",
		"picked up Shells 10.1s ago":      "picked up Shells",
	} {
		if got := EventText(in); got != want {
			t.Errorf("EventText(%q) = %q, want %q", in, got, want)
		}
	}
	for b, want := range map[int]string{0: "ahead", 22: "ahead", 23: "ahead-left", 90: "left", 157: "behind-left", 158: "behind",
		180: "behind", -179: "behind", -90: "right", -23: "ahead-right", -22: "ahead"} {
		if got := sector(b, true); got != want {
			t.Errorf("sector8(%d) = %s, want %s", b, got, want)
		}
	}
	for b, want := range map[int]string{0: "ahead", 44: "ahead", 45: "left", 135: "left", 136: "behind", -136: "behind", -60: "right"} {
		if got := sector(b, false); got != want {
			t.Errorf("sector4(%d) = %s, want %s", b, got, want)
		}
	}
}

func entry(ep int, snap int64) *Entry {
	return &Entry{Raw: json.RawMessage(`{"answers":{}}`), Ep: ep, SnapTime: snap}
}

func TestStore(t *testing.T) {
	s := NewStore(Config{Mode: Exact, TTL: 2 * time.Second, MaxEntries: 2})
	if e, inflight := s.Lookup("a", 0, 100); e != nil || inflight {
		t.Fatal("empty store answered")
	}
	s.Asked("a")
	if _, inflight := s.Lookup("a", 0, 200); !inflight {
		t.Fatal("an asked key is not in flight")
	}
	s.Answered("a", entry(0, 100))
	if e, _ := s.Lookup("a", 0, 2100); e == nil {
		t.Fatal("within the TTL: miss")
	}
	if e, _ := s.Lookup("a", 1, 200); e != nil {
		t.Fatal("another episode's answer under a TTL")
	}
	if e, inflight := s.Lookup("a", 0, 2101); e != nil || inflight {
		t.Fatal("past the TTL: hit")
	}
	if n, _ := s.Len(); n != 0 {
		t.Fatalf("an expired entry kept (%d)", n)
	}
	// least recently used out
	s.Asked("b")
	s.Answered("b", entry(0, 3000))
	s.Asked("c")
	s.Answered("c", entry(0, 3000))
	s.Lookup("b", 0, 3100) // c is now the least recently used
	s.Asked("d")
	s.Answered("d", entry(0, 3000))
	if e, _ := s.Lookup("c", 0, 3100); e != nil {
		t.Fatal("the least recently used entry was kept")
	}
	if e, _ := s.Lookup("b", 0, 3100); e == nil {
		t.Fatal("a recently used entry was evicted")
	}
	if n, ev := s.Len(); n != 2 || ev != 1 {
		t.Fatalf("%d entries, %d evicted", n, ev)
	}
	// a newer answer replaces the older one; a failed one keeps it
	s.Asked("b")
	s.Answered("b", entry(0, 4000))
	s.Asked("b")
	s.Answered("b", nil)
	if e, _ := s.Lookup("b", 0, 4100); e == nil || e.SnapTime != 4000 {
		t.Fatalf("replaced entry %+v", e)
	}
	// no TTL: any age, any episode
	s = NewStore(Config{Mode: Exact})
	s.Asked("a")
	s.Answered("a", entry(0, 100))
	if e, _ := s.Lookup("a", 3, 10_000_000); e == nil {
		t.Fatal("no TTL: miss")
	}
}

// fake is an inner backend answering every question with its first option.
type fake struct{ calls atomic.Int32 }

func (*fake) Name() string { return "fake" }

func (f *fake) Decide(_ context.Context, req *decide.Request) (*decide.Response, error) {
	f.calls.Add(1)
	ans := map[string]decide.Answer{}
	for i := range req.Questions {
		q := &req.Questions[i]
		ans[q.ID] = decide.OneHot(q, q.Options[0].Key)
	}
	usage := decide.Usage{InputTokens: 1000, OutputTokens: 50}
	raw, err := decide.MarshalResponse("fake-1", req.Questions, ans, usage)
	if err != nil {
		return nil, err
	}
	resp, err := decide.DecodeResponse(raw, req.Questions)
	if err != nil {
		return nil, err
	}
	resp.Seq, resp.CostUSD = req.Seq, 0.001
	return resp, nil
}

func request(t *testing.T, seq uint64, snap int64, edit func(*decide.State)) *decide.Request {
	t.Helper()
	st := baseState()
	if edit != nil {
		edit(&st)
	}
	req, err := decide.NewRequest(seq, decide.LaneFast, snap, &st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func record(req *decide.Request, resp *decide.Response, err error) *decide.Record {
	return &decide.Record{Backend: "fake", Result: decide.Result{Req: req, Resp: resp, Err: err}}
}

// TestBackendLockstep: the lookup at submission (Latency), the answer from
// Decide without the inner backend, the keeping in Record.
func TestBackendLockstep(t *testing.T) {
	inner := &fake{}
	b := NewStore(Config{Mode: Approx}).Episode(inner, 0, true)
	if b.Name() != "fake" {
		t.Fatalf("name %q", b.Name())
	}
	lat := b.Latency(decide.FixedLatency(200 * time.Millisecond))
	ctx := context.Background()

	r1 := request(t, 1, 1000, nil)
	if d := lat.Latency(r1); d != 200*time.Millisecond {
		t.Fatalf("a miss's latency %v", d)
	}
	resp1, err := b.Decide(ctx, r1)
	if err != nil || resp1.Cache != "" || inner.calls.Load() != 1 {
		t.Fatalf("miss: %+v %v", resp1, err)
	}
	// asked before the first answer came back: a miss, in flight
	r2 := request(t, 2, 1100, func(s *decide.State) { s.Me.Health = 75 })
	if d := lat.Latency(r2); d == 0 {
		t.Fatal("a request answered before the answer it reuses came back")
	}
	b.Record(record(r1, resp1, nil))
	resp2, _ := b.Decide(ctx, r2)
	b.Record(record(r2, resp2, nil))

	// a similar request after the answer: a hit, without the backend
	r3 := request(t, 3, 1500, func(s *decide.State) { s.Enemies[0].Units = 500 })
	if d := lat.Latency(r3); d != 0 {
		t.Fatalf("a hit's latency %v", d)
	}
	resp3, err := b.Decide(ctx, r3)
	if err != nil || inner.calls.Load() != 2 {
		t.Fatalf("hit: %v, %d calls", err, inner.calls.Load())
	}
	if resp3.Cache != MarkApprox || resp3.Seq != 3 || resp3.Usage != (decide.Usage{}) || resp3.CostUSD != 0 ||
		resp3.SavedTokens != 1000 || resp3.SavedUSD != 0.001 || resp3.CacheAgeMs != 400 || resp3.Model != "fake-1" {
		t.Fatalf("hit response %+v", resp3)
	}
	if !bytes.Contains(resp3.Raw, []byte(`"usage":{"input_tokens":0,"output_tokens":0}`)) {
		t.Fatalf("hit body %s", resp3.Raw)
	}
	// the body decodes to the same answers (what a replay reads)
	again, err := decide.DecodeResponse(resp3.Raw, r3.Questions)
	if err != nil {
		t.Fatal(err)
	}
	for id, a := range resp1.Answers {
		if again.Answers[id].Choice != a.Choice || resp3.Answers[id].Choice != a.Choice {
			t.Fatalf("%s: reused %+v, decoded %+v, original %+v", id, resp3.Answers[id], again.Answers[id], a)
		}
	}
	b.Record(record(r3, resp3, nil)) // a reused answer is not kept again
	if n, _ := b.store.Len(); n != 1 {
		t.Fatalf("%d entries", n)
	}

	// a hit the budget refused (Decide never ran) leaves nothing behind
	r4 := request(t, 4, 1600, nil)
	if lat.Latency(r4) != 0 {
		t.Fatal("no hit")
	}
	b.Record(record(r4, nil, errors.New("budget: refused")))
	if len(b.found) != 0 || len(b.asked) != 0 {
		t.Fatalf("left behind: %d found, %d asked", len(b.found), len(b.asked))
	}

	// another lane state (an enemy closer): a miss
	r5 := request(t, 5, 1700, func(s *decide.State) { s.Enemies[0].Dist, s.Enemies[0].Units = "close", 100 })
	if lat.Latency(r5) == 0 {
		t.Fatal("a different situation hit")
	}
	st := b.Stats()
	// counted as their records come back: r1 and r2 missed (r2 in
	// flight), r3 hit; r4's hit was refused, r5 is not back
	if st.Hits[decide.LaneFast] != 1 || st.Misses[decide.LaneFast] != 2 || st.InFlight[decide.LaneFast] != 1 {
		t.Fatalf("stats %+v", st)
	}
	if b.Summary() == "" {
		t.Fatal("no summary")
	}
}

// TestBackendRealtime: without the submission hook, Decide looks up.
func TestBackendRealtime(t *testing.T) {
	inner := &fake{}
	b := NewStore(Config{Mode: Exact}).Episode(inner, 0, false)
	ctx := context.Background()
	r1 := request(t, 1, 1000, nil)
	resp1, _ := b.Decide(ctx, r1)
	b.Record(record(r1, resp1, nil))
	r2 := request(t, 2, 1500, nil)
	resp2, err := b.Decide(ctx, r2)
	if err != nil || resp2.Cache != MarkExact || inner.calls.Load() != 1 {
		t.Fatalf("realtime hit %+v %v (%d calls)", resp2, err, inner.calls.Load())
	}
	r3 := request(t, 3, 1600, func(s *decide.State) { s.Me.Health = 71 })
	if resp3, _ := b.Decide(ctx, r3); resp3.Cache != "" || inner.calls.Load() != 2 {
		t.Fatal("an exact cache reused an answer to another state")
	}
}

// simEvents builds a run's events: a level_start and decision events for
// requests (lane fast) given by snap and collection times; failed ones
// carry an error.
func simEvents(t *testing.T, reqs []struct {
	snap, arrived int64
	health        int
	err           string
}) []trace.Event {
	t.Helper()
	inner := &fake{}
	evs := []trace.Event{{Type: trace.TypeLevelStart, Seq: 1, GMs: 0, Map: "demo1", Body: trace.LevelStart{}}}
	for i, r := range reqs {
		req := request(t, uint64(i+1), r.snap, func(s *decide.State) { s.Me.Health = r.health })
		resp, _ := inner.Decide(context.Background(), req)
		rec := record(req, resp, nil)
		if r.err != "" {
			rec = record(req, nil, errors.New(r.err))
		}
		d := rec.Decision(decide.RecordOptions{State: true, Questions: true})
		evs = append(evs, trace.Event{Type: trace.TypeDecision, Seq: uint64(i + 2), GMs: r.arrived, Map: "demo1", Body: d})
	}
	return evs
}

// TestSimulate: the simulator's causal rule (an answer is reusable from
// the tick after it was collected), its in-flight count, failures kept
// out, budget refusals not counted, and the fidelity of the hits.
func TestSimulate(t *testing.T) {
	evs := simEvents(t, []struct {
		snap, arrived int64
		health        int
		err           string
	}{
		{1000, 1300, 70, ""},             // a miss
		{1100, 1400, 70, ""},             // the same, asked before the first came back: a miss in flight
		{1300, 1600, 70, ""},             // submitted in the tick the first was collected: still a miss, in flight
		{1400, 1700, 70, ""},             // a hit
		{1500, 1800, 71, "jev: timeout"}, // another state (exact), failed: a miss, nothing kept
		{1900, 2200, 71, ""},             // a miss again (not in flight: the failed one is back)
		{2000, 2300, 72, "budget: max"},  // refused: not counted
	})
	reqs, err := SimRequests(evs)
	if err != nil || len(reqs) != 7 || reqs[0].Map != "demo1" || reqs[0].State == nil || len(reqs[0].Qs) == 0 || reqs[0].Resp == nil {
		t.Fatalf("%d requests %+v: %v", len(reqs), reqs, err)
	}
	if n := ExactRepeats(reqs); n != 4 {
		t.Fatalf("%d repeats", n)
	}
	exact, _ := Parse("exact")
	res := Simulate(reqs, exact)
	f := res.Lanes["fast"]
	if f.Requests != 6 || f.Hits != 1 || f.Misses != 5 || f.InFlight != 2 || f.SavedTokens != 1000 || res.Total.Hits != 1 {
		t.Fatalf("exact: %+v", f)
	}
	if a := res.AllEqual; a.N != 1 || a.Equal != 1 || res.Fidelity["target"].N != 1 || len(res.Ages) != 1 || res.Ages[0] != 400 {
		t.Fatalf("fidelity %+v %+v ages %v", res.AllEqual, res.Fidelity, res.Ages)
	}
	// approx: health 71 is the same bucket, so request 5 hits too, and 6
	ap, _ := Parse("approx")
	if res := Simulate(reqs, ap); res.Total.Hits != 3 {
		t.Fatalf("approx: %+v", res.Total)
	}
	// a TTL shorter than the ages: no hit
	exact.TTL = 300 * time.Millisecond
	if res := Simulate(reqs, exact); res.Total.Hits != 0 {
		t.Fatalf("ttl: %+v", res.Total)
	}
}
