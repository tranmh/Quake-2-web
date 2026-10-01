package navbuild

import (
	"context"
	"math"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/testutil"
)

// testBuilder sets a builder up for map name (its scene and one worker),
// without running any stage.
func testBuilder(t *testing.T, name string, raw []byte) *builder {
	t.Helper()
	f, err := bsp.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var maps [4]*mapdata.Map
	for s := range maps {
		if maps[s], err = mapdata.Load(name, raw, mapdata.Options{Skill: s}); err != nil {
			t.Fatal(err)
		}
	}
	p := nav.DefaultParams()
	b := &builder{ctx: context.Background(), p: p, phys: p.Physics(), name: name, geo: &fileGeo{f: f, cm: maps[1].CM}, rep: &Report{}}
	if b.sc, err = newScene(maps, b.geo); err != nil {
		t.Fatal(err)
	}
	b.workers = []*worker{b.newWorker()}
	return b
}

func demoRaw(t *testing.T, name string) []byte {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestSettleCrawlspace: under a ceiling 36 units above the floor only the
// ducked hull fits; the settle traces start low enough to find it.
func TestSettleCrawlspace(t *testing.T) {
	b := testBuilder(t, "synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	// the slab's top is z=0; a box solid from z=36 to 60 over its west half
	b.sc.statics = append(b.sc.statics, navsim.Solid{ID: 99, Box: true, Origin: Vec3{-32, 0, 48}, Mins: Vec3{-32, -64, -12}, Maxs: Vec3{32, 64, 12}})
	wk := b.workers[0]
	wk.setWorld()
	n, ok := wk.settle(Vec3{-32, 0, 0}, 0)
	if !ok || !n.crouch() || math.Abs(float64(n.o[2]-24.125)) > 0.2 {
		t.Fatalf("crawlspace: node %+v ok %v", n, ok)
	}
	if n, ok := wk.settle(Vec3{32, 0, 0}, 0); !ok || n.crouch() {
		t.Fatalf("open floor: node %+v ok %v", n, ok)
	}
	// a 20-unit gap fits nothing
	b.sc.statics[len(b.sc.statics)-1].Origin[2] = 32
	wk.setWorld()
	if n, ok := wk.settle(Vec3{-32, 0, 0}, 0); ok {
		t.Fatalf("a node under a 20-unit ceiling: %+v", n)
	}
}

// TestLadderNodesInStatics: ladder brushes of static brush entities give
// ladder nodes like the world's. demo2's ladders are func_walls that only
// spawn in deathmatch (spawnflags 1792: not in any single-player skill),
// so the single-player scene has none; with the wall added as a static
// solid the builder finds the ladder.
func TestLadderNodesInStatics(t *testing.T) {
	raw := demoRaw(t, "demo2")
	b := testBuilder(t, "demo2", raw)
	for _, s := range b.sc.statics {
		if s.ID == 74 {
			t.Fatal("the deathmatch-only func_wall *8 is in the single-player scene")
		}
	}
	if n := len(b.ladderNodes()); n != 0 {
		t.Fatalf("%d ladder nodes in single player", n)
	}
	m := b.geo.f.Models[8]
	b.sc.statics = append(b.sc.statics, navsim.Solid{ID: 74, Headnode: m.Headnode, Mins: m.Mins, Maxs: m.Maxs})
	b.workers[0].setWorld()
	nodes := b.ladderNodes()
	near := 0
	for _, n := range nodes {
		if n.flags&nav.NodeLadder == 0 {
			t.Errorf("node %+v", n)
		}
		if hdist(n.o, Vec3{-252, -416, 0}) < 64 {
			near++
		}
	}
	if near == 0 {
		t.Fatalf("no ladder node at brush 2040 (%d ladder nodes)", len(nodes))
	}
}

func TestRidePairs(t *testing.T) {
	poses := func(n int) []nav.BlockerPose { return make([]nav.BlockerPose, n) }
	train := &nav.Blocker{Kind: nav.BlockTrain, Poses: poses(3)}
	if p := ridePairs(train, &blockerGeo{loopTo: -1}); len(p) != 2 || p[1] != [2]int{1, 2} {
		t.Errorf("a path ending at its last corner: %v", p)
	}
	if p := ridePairs(train, &blockerGeo{loopTo: 0}); len(p) != 3 || p[2] != [2]int{2, 0} {
		t.Errorf("a looping path: %v", p)
	}
	if p := ridePairs(train, &blockerGeo{loopTo: 1}); len(p) != 3 || p[2] != [2]int{2, 1} {
		t.Errorf("a path looping to its second corner: %v", p)
	}
	plat := &nav.Blocker{Kind: nav.BlockPlat, Poses: poses(2)}
	if p := ridePairs(plat, &blockerGeo{}); len(p) != 1 || p[0] != [2]int{1, 0} {
		t.Errorf("plat: %v", p)
	}
	if p := ridePairs(plat, &blockerGeo{lowTrigger: true}); len(p) != 2 {
		t.Errorf("low trigger plat: %v", p)
	}

	cases := []struct {
		name string
		bl   *nav.Blocker
		g    blockerGeo
		a, b int
		want bool
	}{
		{"untargeted train leaves its first corner", train, blockerGeo{act: mapdata.ActAuto, cornerWait: []float32{0, 0, 0}}, 0, 1, false},
		{"targeted train waits to be used", train, blockerGeo{act: mapdata.ActUse, cornerWait: []float32{0, 0, 0}}, 0, 1, true},
		{"wait -1 corner", train, blockerGeo{act: mapdata.ActAuto, cornerWait: []float32{0, -1, 0}}, 1, 2, true},
		{"timed corner", train, blockerGeo{act: mapdata.ActAuto, cornerWait: []float32{0, 3, 0}}, 1, 2, false},
		{"touch plat", plat, blockerGeo{act: mapdata.ActTouch}, 1, 0, false},
		{"used door opens", &nav.Blocker{Kind: nav.BlockDoor, Poses: poses(2)}, blockerGeo{act: mapdata.ActUse, wait: 3}, 0, 1, true},
		{"used door returns", &nav.Blocker{Kind: nav.BlockDoor, Poses: poses(2)}, blockerGeo{act: mapdata.ActUse, wait: 3}, 1, 0, false},
		{"toggle door returns when used", &nav.Blocker{Kind: nav.BlockDoor, Poses: poses(2)}, blockerGeo{act: mapdata.ActUse, wait: -1}, 1, 0, true},
	}
	for _, c := range cases {
		if got := rideNeedsUse(c.bl, &c.g, c.a, c.b); got != c.want {
			t.Errorf("%s: needs use %v", c.name, got)
		}
	}
}

// TestHazard: a walk through a trigger_hurt is a hazard with about its
// damage per second times the time inside.
func TestHazard(t *testing.T) {
	b := testBuilder(t, "synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	b.sc.vols = append(b.sc.vols, vol{kind: nav.EffTrigger, entity: 5, pose: -1, blocker: -1,
		min: Vec3{-8, -64, 0}, max: Vec3{8, 64, 64}, hurt: 50})
	wk := b.workers[0]
	wk.setWorld()
	var s []navsim.Sample
	for x := float32(-60); x <= 60; x += 7.5 { // 300 u/s at 25 ms a step
		s = append(s, navsim.Sample{Origin: Vec3{x, 0, 24.125}, OnGround: true})
	}
	dmg, hit := wk.hazard(s, 0.025)
	// inside while |x| <= 8+16+1: 50 units, a sixth of a second
	if !hit || dmg < 7 || dmg > 10 {
		t.Fatalf("damage %d hit %v", dmg, hit)
	}
	clear := []navsim.Sample{{Origin: Vec3{-60, 0, 24.125}}, {Origin: Vec3{-40, 0, 24.125}}}
	if dmg, hit := wk.hazard(clear, 0.025); hit || dmg != 0 {
		t.Fatalf("clear walk: %d %v", dmg, hit)
	}
	var e nav.Edge
	wk.markHazard(&e, s, 0.025)
	if e.Flags&nav.EdgeHazard == 0 || e.Damage < 7 {
		t.Fatalf("edge %+v", e)
	}
}

// TestSceneEntities checks how the scene takes point entities: items with
// ITEM_TRIGGER_SPAWN start disabled, ITEM_NO_TOUCH items are solid boxes
// without a touch volume, a teleporter carries the skills it spawns at.
func TestSceneEntities(t *testing.T) {
	f := bsp.SyntheticFloorMap()
	f.Entities = []byte(`{
"classname" "worldspawn"
}
{
"classname" "info_player_start"
"origin" "0 0 24"
}
{
"classname" "item_health"
"origin" "32 32 16"
"spawnflags" "1"
}
{
"classname" "item_armor_shard"
"origin" "-32 32 16"
"spawnflags" "2"
}
{
"classname" "item_health_small"
"origin" "-32 -32 16"
}
{
"classname" "misc_teleporter"
"origin" "40 -40 0"
"target" "dest"
"spawnflags" "256"
}
{
"classname" "misc_teleporter_dest"
"origin" "-40 -40 10"
"targetname" "dest"
}
` + "\x00")
	b := testBuilder(t, "synthetic", bsp.Encode(f))
	sc := b.sc
	ents := map[string]nav.Ent{}
	for _, e := range sc.ents {
		ents[e.Class] = e
	}
	if e, ok := ents["item_health"]; !ok || !e.Disabled {
		t.Errorf("trigger-spawned item: %+v", e)
	}
	if e := ents["item_health_small"]; e.Disabled || e.Skills != nav.AllSkills {
		t.Errorf("plain item: %+v", e)
	}
	if _, ok := ents["item_armor_shard"]; ok {
		t.Error("a no-touch item has a touch effect")
	}
	box := false
	for _, s := range sc.statics {
		if s.Box && s.Origin[0] == -32 && s.Maxs == (Vec3{15, 15, 15}) {
			box = true
		}
	}
	if !box {
		t.Errorf("no solid box for the no-touch item: %+v", sc.statics)
	}
	vols := 0
	for _, v := range sc.vols {
		if v.kind == nav.EffItem {
			vols++
		}
	}
	if vols != 2 {
		t.Errorf("%d item volumes, want 2", vols)
	}
	// NOT_EASY: skills 1..3 only
	if e := ents["misc_teleporter"]; e.Skills != 0x0e || len(sc.teles) != 1 || sc.teles[0].dest != (Vec3{-40, -40, 10}) {
		t.Errorf("teleporter %+v %+v", e, sc.teles)
	}
}
