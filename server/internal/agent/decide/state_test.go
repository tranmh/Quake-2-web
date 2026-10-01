package decide

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/testutil"
)

func encode(t *testing.T, st State) []byte {
	t.Helper()
	_, raw, err := FitState(st, HardCap)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestFastStateGolden projects the fixture's fast lane: the bytes match
// the golden file, repeat across 100 projections and stay within the
// typical size of 1 KB.
func TestFastStateGolden(t *testing.T) {
	p := testProjector()
	cx := Context{Target: "e1"}
	first := encode(t, p.Fast(testBelief(), cx))
	golden(t, "fast_state.golden.json", first)
	for i := 0; i < 100; i++ {
		if got := encode(t, p.Fast(testBelief(), cx)); !bytes.Equal(got, first) {
			t.Fatalf("run %d: state not byte-stable:\n%s\n%s", i, got, first)
		}
	}
	if len(first) > 1024 {
		t.Errorf("fast state is %d bytes, want <= 1024 typical", len(first))
	}
	t.Logf("fast state: %d bytes", len(first))
}

// TestSlowStateGolden does the same for the slow lane (under the 3 KB cap).
func TestSlowStateGolden(t *testing.T) {
	p := testProjector()
	cx := Context{Target: "e1", Mode: ModeFight, Objective: testObjective()}
	first := encode(t, p.Project(LaneSlow, testBelief(), cx))
	golden(t, "slow_state.golden.json", first)
	for i := 0; i < 100; i++ {
		if got := encode(t, p.Project(LaneSlow, testBelief(), cx)); !bytes.Equal(got, first) {
			t.Fatalf("run %d: state not byte-stable", i)
		}
	}
	if len(first) > HardCap {
		t.Errorf("slow state is %d bytes, cap %d", len(first), HardCap)
	}
	t.Logf("slow state: %d bytes", len(first))
}

// TestRequestBodyGolden checks the full wire bodies of both lanes.
func TestRequestBodyGolden(t *testing.T) {
	p := testProjector()
	b := testBelief()
	cx := Context{Target: "e1", Mode: ModeFight, Objective: testObjective()}
	for _, tc := range []struct {
		lane Lane
		file string
	}{{LaneFast, "request_fast.golden.json"}, {LaneSlow, "request_slow.golden.json"}} {
		st := p.Project(tc.lane, b, cx)
		req, err := NewRequest(7, tc.lane, b.Time, &st, nil, HardCap)
		if err != nil {
			t.Fatal(err)
		}
		body, err := RequestBody("jev-1.13.0", req)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(body) {
			t.Fatalf("%s: invalid JSON", tc.file)
		}
		golden(t, tc.file, body)
		for i := range req.Questions {
			if err := req.Questions[i].Validate(); err != nil {
				t.Error(err)
			}
		}
		t.Logf("%s lane: %d questions, body %d bytes", tc.lane, len(req.Questions), len(body))
	}
}

func TestProjectionRules(t *testing.T) {
	p := testProjector()
	b := testBelief()
	st := p.Fast(b, Context{Target: "e1"})

	var ids []string
	for _, e := range st.Enemies {
		ids = append(ids, e.ID)
	}
	// threat order; dead e6, forgotten e7 and the barrel are left out
	if got := strings.Join(ids, ","); got != "e2,e1,e3,e4" {
		t.Fatalf("enemies %s, want e2,e1,e3,e4", got)
	}
	e1 := st.Enemies[1]
	// e1 at (100, 300) seen from the origin facing +y: to the right
	if e1.Bearing >= 0 || e1.Bearing < -30 || !e1.Current || e1.Dist != "mid" || e1.Threat != "med" || e1.State != "attacking" {
		t.Errorf("e1 %+v", e1)
	}
	if e2 := st.Enemies[0]; e2.Bearing <= 0 || e2.Threat != "high" {
		t.Errorf("e2 (ahead-left) %+v", e2)
	}
	if e3 := st.Enemies[2]; e3.Aim != "on" || e3.Dist != "close" || !e3.Wounded {
		t.Errorf("e3 (straight ahead, close) %+v", e3)
	}
	if e4 := st.Enemies[3]; e4.Visible || e4.Bearing > -90 {
		t.Errorf("e4 (remembered, behind-right) %+v", e4)
	}

	// the current target stays in the list even when it ranks fifth
	st5 := p.Fast(b, Context{Target: "e5"})
	if n := len(st5.Enemies); n != 4 || st5.Enemies[3].ID != "e5" || !st5.Enemies[3].Current {
		t.Errorf("current target e5 not kept: %+v", st5.Enemies)
	}

	// incoming: the dangerous rocket first, own and receding ones out
	if len(st.Incoming) != 2 || st.Incoming[0].Kind != "rocket" || st.Incoming[0].ETA != "imminent" || st.Incoming[0].Dodge != "right" ||
		st.Incoming[1].Kind != "blaster_bolt" || st.Incoming[1].ETA != "soon" || st.Incoming[1].Dodge != "none" {
		t.Errorf("incoming %+v", st.Incoming)
	}
	me := st.Me
	if me.HP != "low" || me.Weapon != "shotgun" || me.Ammo != "ok" || me.DamageLast1s != 12 || me.HitFromBearing == nil || *me.HitFromBearing != -30 {
		t.Errorf("me %+v", me)
	}
	if st.Space == nil || *st.Space != (Space{"open", "blocked", "tight", "open"}) {
		t.Errorf("space %+v", st.Space)
	}

	slow := p.Project(LaneSlow, b, Context{Mode: ModeFight, Objective: testObjective()})
	// owned weapons with ammo; hand grenades never offered
	if got := strings.Join(slow.Me.Weapons, ","); got != "blaster,shotgun,machinegun" {
		t.Errorf("weapons %s", got)
	}
	var items []string
	for _, it := range slow.Items {
		items = append(items, it.ID+":"+it.Gives)
	}
	// rockets (no launcher) and the taken health are not useful
	for _, it := range items {
		if strings.HasPrefix(it, "i5") || strings.HasPrefix(it, "i6") {
			t.Errorf("useless item listed: %v", items)
		}
	}
	if len(items) != 4 {
		t.Errorf("items %v, want 4", items)
	}
	if slow.Level == nil || slow.Level.Kills != "2/12" || slow.Level.DeathsHere != 1 || slow.Mode != "fight" {
		t.Errorf("level %+v mode %q", slow.Level, slow.Mode)
	}
	if len(slow.Events) != 4 || !strings.HasPrefix(slow.Events[0], "picked up Shells") {
		t.Errorf("events %q", slow.Events)
	}

	// full health: no health item is useful
	b.Self.Health = 100
	for _, it := range p.Project(LaneSlow, b, Context{}).Items {
		if strings.HasPrefix(it.Gives, "health") {
			t.Errorf("health item at full health: %+v", it)
		}
	}
	// buckets
	for hp, want := range map[int]string{1: "critical", 24: "critical", 25: "low", 49: "low", 50: "ok", 99: "ok", 100: "full"} {
		if got := hpBucket(hp); got != want {
			t.Errorf("hp %d: %s, want %s", hp, got, want)
		}
	}
	for _, tc := range []struct {
		k    WeaponKey
		ammo int
		want string
	}{{WeaponBlaster, 0, "inf"}, {WeaponShotgun, 0, "none"}, {WeaponShotgun, 9, "low"}, {WeaponShotgun, 10, "ok"},
		{WeaponSuperShotgun, 1, "none"}, {WeaponBFG, 49, "none"}, {WeaponBFG, 60, "low"}, {"", 50, "none"}} {
		if got := ammoBucket(tc.k, tc.ammo); got != tc.want {
			t.Errorf("ammo %s %d: %s, want %s", tc.k, tc.ammo, got, tc.want)
		}
	}
}

// TestPathFunc: items are ranked by path distance and unreachable ones
// are left out.
func TestPathFunc(t *testing.T) {
	b := testBelief()
	p := NewProjector(ProjectorConfig{Path: func(_, to Vec3) (float32, bool) {
		if to[0] == 200 { // the health is unreachable
			return 0, false
		}
		return 100, true
	}})
	for _, it := range p.Project(LaneSlow, b, Context{}).Items {
		if it.ID == "i1" {
			t.Fatalf("unreachable item listed: %+v", it)
		}
		if it.Path != 100 {
			t.Errorf("path %d, want 100", it.Path)
		}
	}
}

// TestFitStateTrims drops events, items, incoming and enemies in that
// order, deterministically, without changing the input.
func TestFitStateTrims(t *testing.T) {
	p := testProjector()
	st := p.Project(LaneSlow, testBelief(), Context{Target: "e1", Objective: testObjective()})
	full := encode(t, st)
	nEnemies, nItems := len(st.Enemies), len(st.Items)
	for _, limit := range []int{len(full) - 1, 1500, 1100, 900, 700} {
		fit, raw, err := FitState(st, limit)
		if err != nil {
			t.Fatalf("cap %d: %v", limit, err)
		}
		if len(raw) > limit {
			t.Fatalf("cap %d: %d bytes", limit, len(raw))
		}
		if len(fit.Events) > 0 && (len(fit.Items) < nItems || len(fit.Enemies) < nEnemies) {
			t.Errorf("cap %d: items or enemies trimmed before events", limit)
		}
		if len(fit.Items) > 0 && len(fit.Enemies) < nEnemies {
			t.Errorf("cap %d: enemies trimmed before items", limit)
		}
		for i := 0; i < 20; i++ {
			_, again, _ := FitState(st, limit)
			if !bytes.Equal(again, raw) {
				t.Fatalf("cap %d: trimming not deterministic", limit)
			}
		}
		t.Logf("cap %d: %d bytes, %d events, %d items, %d incoming, %d enemies", limit, len(raw), len(fit.Events), len(fit.Items), len(fit.Incoming), len(fit.Enemies))
	}
	if !bytes.Equal(encode(t, st), full) {
		t.Fatal("FitState modified its input")
	}
	if _, _, err := FitState(st, 50); !errors.Is(err, ErrStateTooLarge) {
		t.Fatalf("cap 50: %v, want ErrStateTooLarge", err)
	}
	// a request built under a tight cap asks only about what is left
	req, err := NewRequest(1, LaneSlow, fixtureNow, &st, nil, 700)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.State) > 700 || len(req.View.Items) != 0 || req.Question(QPickup) != nil {
		t.Fatalf("trimmed request: %d bytes, view items %d, pickup question %v", len(req.State), len(req.View.Items), req.Question(QPickup) != nil)
	}
}

// TestEmptyBelief: an empty level projects valid, small states.
func TestEmptyBelief(t *testing.T) {
	p := NewProjector(ProjectorConfig{})
	b := &worldmodel.Belief{Map: "demo1", Self: worldmodel.Self{Health: 100, Weapon: "Blaster"}}
	for _, l := range []Lane{LaneFast, LaneSlow} {
		st := p.Project(l, b, Context{})
		req, err := NewRequest(1, l, 0, &st, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(req.State), `"enemies":[]`) {
			t.Errorf("%s: %s", l, req.State)
		}
		if l == LaneFast && (req.Question(QTarget) != nil || len(req.Questions) != 2) {
			t.Errorf("fast questions without enemies: %d", len(req.Questions))
		}
		if l == LaneSlow && (req.Question(QWeapon) != nil || req.Question(QPickup) != nil || len(req.Questions) != 2) {
			t.Errorf("slow questions with nothing to choose: %d", len(req.Questions))
		}
	}
}

func TestTraceSpaceFloor(t *testing.T) {
	cm, err := cmodel.LoadMapBytes("maps/floor.bsp", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	ts := NewTraceSpace(cm, 200)
	if got := ts.Clearance(Vec3{0, 0, 24}, 0); got != [4]float32{200, 200, 200, 200} {
		t.Errorf("open floor: %v", got)
	}
	if got := ts.Clearance(Vec3{0, 0, -8}, 0); got != [4]float32{} {
		t.Errorf("inside the slab: %v", got)
	}
	// beside the slab at its height: the slab is a wall towards +x
	if got := ts.Clearance(Vec3{-150, 0, -10}, 0); got[0] < 60 || got[0] > 80 || got[1] != 200 {
		t.Errorf("beside the slab: %v", got)
	}
}

// TestTraceSpaceDemo1: at demo1's start (a corridor) walls are near on
// some side.
func TestTraceSpaceDemo1(t *testing.T) {
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	defer fs.Close()
	raw, err := fs.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	cm, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load("demo1", raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	sp, ok := md.SpawnPoint("")
	if !ok {
		t.Fatal("no spawn point")
	}
	ts := NewTraceSpace(cm, 256)
	// the player's spawn origin (C: game/p_client.c SelectSpawnPoint +9)
	origin := Vec3{sp.Origin[0], sp.Origin[1], sp.Origin[2] + 9}
	c := ts.Clearance(origin, sp.Angles[1])
	short := 0
	for _, d := range c {
		if d < 0 || d > 256 {
			t.Fatalf("clearance %v", c)
		}
		if d < 256 {
			short++
		}
	}
	if short == 0 || c == [4]float32{} {
		t.Errorf("clearance at the start %v: want walls near, but not inside one", c)
	}
	t.Logf("demo1 start %v yaw %.0f clearance %v", origin, sp.Angles[1], c)
}
