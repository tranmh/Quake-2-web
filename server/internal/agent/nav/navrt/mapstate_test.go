package navrt

import (
	"math"
	"testing"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/worldmodel"
)

// blockerGraph has an auto door (*1, travel 1.4 s, wait 3 s), a use-only
// door (*2), a touch plat (*3, top = pose 0, spawns at its bottom), a
// laser (#13, on at spawn) and a rotating door (*5) with angle poses.
func blockerGraph(t testing.TB) (*nav.Graph, *MapState) {
	t.Helper()
	door := func(ent int32, model string) nav.Blocker {
		return nav.Blocker{Entity: ent, Class: "func_door", Model: model, Kind: nav.BlockDoor, Skills: nav.AllSkills, Solid: true,
			Mins: Vec3{-8, -32, 0}, Maxs: Vec3{8, 32, 96},
			Poses: []nav.BlockerPose{{Name: "pos1", Origin: Vec3{100, 0, 0}}, {Name: "pos2", Origin: Vec3{100, 0, 88}}}}
	}
	g := &nav.Graph{Format: nav.FormatVersion, Skill: 1, Params: nav.DefaultParams(),
		Blockers: []nav.Blocker{
			door(10, "*1"), door(11, "*2"),
			{Entity: 12, Class: "func_plat", Model: "*3", Kind: nav.BlockPlat, Skills: nav.AllSkills, Solid: true, Spawn: 1,
				Mins: Vec3{-32, -32, -8}, Maxs: Vec3{32, 32, 0},
				Poses: []nav.BlockerPose{{Name: "top", Origin: Vec3{0, 200, 0}}, {Name: "bottom", Origin: Vec3{0, 200, -128}}}},
			{Entity: 13, Class: "target_laser", Kind: nav.BlockLaser, Skills: nav.AllSkills, Gone: true,
				Poses: []nav.BlockerPose{{Name: "on", Origin: Vec3{0, -100, 30}}}, Start: Vec3{0, -100, 30}, End: Vec3{0, 100, 30}},
			{Entity: 14, Class: "func_door_rotating", Model: "*5", Kind: nav.BlockRotating, Skills: nav.AllSkills, Solid: true,
				Mins: Vec3{0, -4, 0}, Maxs: Vec3{64, 4, 96},
				Poses: []nav.BlockerPose{{Name: "pos1", Origin: Vec3{300, 0, 0}}, {Name: "pos2", Origin: Vec3{300, 0, 0}, Angles: Vec3{0, 90, 0}}}},
		},
		Nodes: []nav.Node{{Origin: Vec3{0, 0, 24}, Blocker: -1}},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	ms := NewMapState(g, nil)
	ms.info[0] = blockerInfo{auto: true, returns: true, wait: 3, travel: 1.4, sweep: ms.info[0].sweep}
	ms.info[1] = blockerInfo{returns: true, wait: 3, travel: 1, sweep: ms.info[1].sweep}
	ms.info[2] = blockerInfo{auto: true, wait: PlatReturn, travel: 2, sweep: ms.info[2].sweep}
	ms.info[4] = blockerInfo{travel: 2, sweep: ms.info[4].sweep}
	return g, ms
}

func sat(t *testing.T, ms *MapState, b int32, states nav.StateMask, now int64, ok bool, wait float32) {
	t.Helper()
	gotOK, gotWait := ms.Satisfied(nav.Req{Blocker: b, States: states}, now)
	if gotOK != ok || math.Abs(float64(gotWait-wait)) > 1e-4 {
		t.Errorf("blocker %d (%s) states %#x at %d: (%v, %v), want (%v, %v)", b, ms.Belief(b).Status, states, now, gotOK, gotWait, ok, wait)
	}
}

func TestMapStateDoors(t *testing.T) {
	_, ms := blockerGraph(t)
	// unknown: assumed as spawned (closed); an auto door is passable after
	// its travel time, a use door is not
	sat(t, ms, 0, nav.Pose(0), 0, true, 0)
	sat(t, ms, 0, nav.Pose(1), 0, true, 1.4)
	sat(t, ms, 1, nav.Pose(1), 0, false, 0)

	mover := func(model string, o Vec3, at int64) worldmodel.Mover {
		return worldmodel.Mover{Model: model, Origin: o, LastUpdate: at, Visible: true}
	}
	// seen closed, then halfway, then open
	b := &worldmodel.Belief{Movers: []worldmodel.Mover{mover("*1", Vec3{100, 0, 0}, 1000)}}
	if ch := ms.Update(b, 1000); len(ch) != 1 || ch[0] != 0 || ms.Belief(0).Status != BlockerAt || ms.Belief(0).Pose != 0 {
		t.Fatalf("closed: changed %v, %+v", ch, ms.Belief(0))
	}
	rev := ms.Revision(0)
	if ch := ms.Update(b, 1100); len(ch) != 0 || ms.Revision(0) != rev {
		t.Errorf("the same observation changed %v", ch)
	}
	b.Movers[0] = mover("*1", Vec3{100, 0, 50}, 1500)
	ms.Update(b, 1500)
	if bb := ms.Belief(0); bb.Status != BlockerMoving || bb.Pose != 1 {
		t.Fatalf("halfway: %+v", bb)
	}
	if ms.Possible(0) != ms.g.Blockers[0].All() || ms.Assumed(0) != 0 {
		t.Errorf("moving: possible %#x assumed %#x", ms.Possible(0), ms.Assumed(0))
	}
	sat(t, ms, 0, nav.Pose(1), 2000, true, 0.9) // 1.4 s travel, 0.5 s since it started moving
	b.Movers[0] = mover("*1", Vec3{100, 0, 88.125}, 2500)
	ms.Update(b, 2500)
	if bb := ms.Belief(0); bb.Status != BlockerAt || bb.Pose != 1 {
		t.Fatalf("open: %+v", bb)
	}
	sat(t, ms, 0, nav.Pose(1), 2500, true, 0)
	// it closes again after its wait: 3 s + 1.4 s from arriving
	sat(t, ms, 0, nav.Pose(0), 3500, true, 3.4)
	// unobserved long enough to have returned: unknown, assumed closed
	ms.Update(b, 2500+4400+returnSlack+1)
	if bb := ms.Belief(0); bb.Status != BlockerUnknown || bb.Pose != 0 {
		t.Errorf("expired: %+v", bb)
	}

	// the use door seen open returns too, but nothing opens it again
	b.Movers = append(b.Movers, mover("*2", Vec3{100, 0, 88}, 8000))
	ms.Update(b, 8000)
	sat(t, ms, 1, nav.Pose(0), 8500, true, 3.5)
	b.Movers[1] = mover("*2", Vec3{100, 0, 0}, 9000)
	ms.Update(b, 9000)
	sat(t, ms, 1, nav.Pose(1), 9000, false, 0)
}

func TestMapStatePlatLaserRotating(t *testing.T) {
	g, ms := blockerGraph(t)
	// the plat spawns at its bottom; at the top it goes back down after
	// PlatReturn; at the bottom only a rider sends it up
	sat(t, ms, 2, nav.Pose(1), 0, true, 0)
	sat(t, ms, 2, nav.Pose(0), 0, false, 0)
	b := &worldmodel.Belief{Movers: []worldmodel.Mover{{Model: "*3", Origin: Vec3{0, 200, 0}, LastUpdate: 1000}}}
	ms.Update(b, 1000)
	sat(t, ms, 2, nav.Pose(1), 2000, true, PlatReturn+2-1)

	// lasers: seen on is pose 0, seen off is gone
	sat(t, ms, 3, nav.StateGone, 0, false, 0)
	b.Lasers = []worldmodel.Laser{{Lump: 13, State: worldmodel.LaserOff, LastUpdate: 1500}}
	ms.Update(b, 1500)
	if ms.Belief(3).Status != BlockerGone || ms.Possible(3) != nav.StateGone {
		t.Errorf("laser off: %+v", ms.Belief(3))
	}
	sat(t, ms, 3, nav.StateGone, 1500, true, 0)
	sat(t, ms, 3, nav.Pose(0), 1500, false, 0)

	// a rotating door's angles arrive in 360/256 degree steps
	b.Movers = append(b.Movers, worldmodel.Mover{Model: "*5", Origin: Vec3{300, 0, 0}, Angles: Vec3{0, 88.59375, 0}, LastUpdate: 1600})
	ms.Update(b, 1600)
	if bb := ms.Belief(4); bb.Status != BlockerAt || bb.Pose != 1 {
		t.Errorf("rotating door at 88.6 degrees: %+v", bb)
	}

	// the prediction world: the plat where it was seen, the laser and
	// the unseen doors as assumed
	solids := ms.Solids(nil)
	byID := map[int]Vec3{}
	for _, s := range solids {
		byID[s.ID] = s.Origin
	}
	if o, ok := byID[12]; !ok || o != (Vec3{0, 200, 0}) {
		t.Errorf("plat solid %v %v", o, ok)
	}
	if o, ok := byID[10]; !ok || o != g.Blockers[0].Poses[0].Origin {
		t.Errorf("unknown door solid %v %v", o, ok)
	}
	if _, ok := byID[13]; ok {
		t.Error("a laser is not solid")
	}
}

// testMapStateFromMapData: the static facts come from the map data: the
// demo1 door *19 is an auto door that closes 3 s after opening, the car
// *31 is used by its button, demo2's plat *52 is a touch plat.
func testMapStateFromMapData(t *testing.T, d *demoLevel) {
	for _, c := range []struct {
		name, model   string
		auto, returns bool
		trigger       bool
	}{
		{"demo1", "*19", true, true, true},
		{"demo1", "*31", false, true, false},
		{"demo2", "*52", true, false, true},
	} {
		if c.name != d.md.Name {
			continue
		}
		ms := NewMapState(d.g, d.md)
		e := d.md.ByModel(c.model)
		b := d.g.BlockerOf(e.Index)
		if b < 0 {
			t.Fatalf("%s %s: no blocker", c.name, c.model)
		}
		in := ms.info[b]
		if in.auto != c.auto || in.returns != c.returns || (in.trigger != nil) != c.trigger || in.travel <= 0 {
			t.Errorf("%s %s: auto %v returns %v trigger %v travel %v", c.name, c.model, in.auto, in.returns, in.trigger != nil, in.travel)
		}
		if c.model == "*52" && in.wait != PlatReturn {
			t.Errorf("plat wait %v", in.wait)
		}
	}
}
