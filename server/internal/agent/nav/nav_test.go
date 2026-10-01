package nav_test

import (
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// testGraph is a small hand-made graph using every feature of the format:
//
//	0 spawn --walk--> 1 --jump--> 2 --touch(button 9)--> 2
//	1 --walk, needs door 0 open--> 3 (on plat 1 at its top) --ride--> 4 (bottom)
//	3 --walk--> 1, 4 --walk, needs laser 2 off--> 0
func testGraph(t testing.TB) *nav.Graph {
	t.Helper()
	g := &nav.Graph{
		Format: nav.FormatVersion, Map: "test", Checksum: 0xdeadbeef, Params: nav.DefaultParams(), Skill: -1,
		Blockers: []nav.Blocker{
			{Entity: 10, Class: "func_door", Model: "*1", Kind: nav.BlockDoor, Skills: nav.AllSkills, Solid: true, Headnode: 5,
				Mins: nav.Vec3{-8, -64, 0}, Maxs: nav.Vec3{8, 64, 128},
				Poses: []nav.BlockerPose{{Name: "pos1", Origin: nav.Vec3{}}, {Name: "pos2", Origin: nav.Vec3{0, 0, 120}}}},
			{Entity: 11, Class: "func_plat", Model: "*2", Kind: nav.BlockPlat, Skills: nav.AllSkills, Solid: true, Spawn: 1,
				Poses: []nav.BlockerPose{{Name: "top"}, {Name: "bottom", Origin: nav.Vec3{0, 0, -100}}}},
			{Entity: 12, Class: "target_laser", Kind: nav.BlockLaser, Skills: 0x0c, Gone: true,
				Poses: []nav.BlockerPose{{Name: "on", Origin: nav.Vec3{1, 2, 3}}}, Start: nav.Vec3{1, 2, 3}, End: nav.Vec3{100, 2, 3}},
		},
		Ents: []nav.Ent{
			{Entity: 9, Class: "func_button", Model: "*3", Skills: nav.AllSkills},
			{Entity: 13, Class: "item_health", Skills: 0x01},
			{Entity: 14, Class: "trigger_once", Model: "*4", Skills: nav.AllSkills},
		},
		Spawns: []nav.Spawn{{Entity: 20, Origin: nav.Vec3{0, 0, 24}, Node: 0, Skills: nav.AllSkills}, {Entity: 21, Targetname: "base", Node: 4, Skills: 0x02}},
		Solids: []navsim.Solid{{ID: 30, Box: true, Origin: nav.Vec3{50, 50, 0}, Mins: nav.Vec3{-16, -16, 0}, Maxs: nav.Vec3{16, 16, 40}}},
		Volumes: []nav.Volume{
			{Kind: nav.EffTrigger, Entity: 14, Pose: -1, Blocker: -1, Min: nav.Vec3{0, 0, 0}, Max: nav.Vec3{10, 10, 10}},
			{Kind: nav.EffItem, Entity: 13, Pose: 0, Blocker: 1, Min: nav.Vec3{-15, -15, -15}, Max: nav.Vec3{15, 15, 15}},
		},
		Pushes:    []navsim.Push{{ID: 40, Min: nav.Vec3{300, 300, 0}, Max: nav.Vec3{310, 310, 10}, Velocity: nav.Vec3{0, 0, 1000}, Once: true}},
		Teleports: []navsim.Teleport{{ID: 41, Min: nav.Vec3{400, 400, 0}, Max: nav.Vec3{410, 410, 10}, Dest: nav.Vec3{500, 0, 24}, Angles: nav.Vec3{0, 90, 0}}},
		Nodes: []nav.Node{
			{Origin: nav.Vec3{0, 0, 24.125}, Flags: nav.NodeSpawn, Blocker: -1},
			{Origin: nav.Vec3{32, 0, 24.125}, Flags: nav.NodeLedge, Blocker: -1, Region: 0},
			{Origin: nav.Vec3{160, 0, 24.125}, Blocker: -1, Region: 1},
			{Origin: nav.Vec3{64, 64, 24.125}, Flags: nav.NodeMover, Blocker: 1, Pose: 0, Region: 2},
			{Origin: nav.Vec3{64, 64, -75.875}, Flags: nav.NodeMover | nav.NodeSpawn, Blocker: 1, Pose: 1, Region: 3, Yaw: 45},
		},
		Edges: []nav.Edge{
			{From: 0, To: 1, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Flags: nav.EdgeFast, Cost: 0.1,
				Effects: []nav.Effect{{Kind: nav.EffTrigger, Entity: 14, Yaw: 0, T: 0.05, Pose: -1, Blocker: -1}}},
			{From: 1, To: 2, Kind: nav.EdgeJump, Recipe: navsim.RecipeJump, Cost: 0.7, Takeoff: nav.Vec3{40, 0, 24.125}, TakeoffSpeed: 290, BackupMsec: 300, FallDamage: 3},
			{From: 1, To: 3, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Flags: nav.EdgeBoard, Cost: 0.3,
				Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(1)}, {Blocker: 1, States: nav.Pose(0)}}},
			{From: 2, To: 2, Kind: nav.EdgeTouch, Recipe: navsim.RecipeWalk, Flags: nav.EdgeSpawnWorld, Cost: 0.4, Aim: nav.Vec3{180, 0, 24}, Target: 9,
				Effects: []nav.Effect{{Kind: nav.EffButton, Entity: 9, Yaw: 0, T: 0.4, Pose: -1, Blocker: -1}}},
			{From: 3, To: 1, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.3, Reqs: []nav.Req{{Blocker: 1, States: nav.Pose(0)}},
				Effects: []nav.Effect{{Kind: nav.EffItem, Entity: 13, Pose: 0, Blocker: 1}}},
			{From: 3, To: 4, Kind: nav.EdgeRide, Recipe: navsim.RecipeRide, Cost: 1.2, Reqs: []nav.Req{{Blocker: 1, States: nav.Pose(0)}}},
			{From: 4, To: 0, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.5, Reqs: []nav.Req{{Blocker: 1, States: nav.Pose(1)}, {Blocker: 2, States: nav.StateGone}}},
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	return g
}

func encode(t testing.TB, g *nav.Graph) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := g.Encode(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCodecRoundTrip(t *testing.T) {
	g := testGraph(t)
	a := encode(t, g)
	if b := encode(t, g); !bytes.Equal(a, b) {
		t.Fatal("two encodings of the same graph differ")
	}
	d, err := nav.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	if b := encode(t, d); !bytes.Equal(a, b) {
		t.Fatal("decode/encode is not the identity")
	}
	if d.Map != "test" || d.Checksum != 0xdeadbeef || d.Skill != -1 || d.Params != nav.DefaultParams() || len(d.Nodes) != 5 || len(d.Edges) != 7 {
		t.Fatalf("header %q %x %d %+v", d.Map, d.Checksum, d.Skill, d.Params)
	}
	if e := d.Edges[1]; e.Takeoff != (nav.Vec3{40, 0, 24.125}) || e.BackupMsec != 300 || e.FallDamage != 3 || e.TakeoffSpeed != 290 {
		t.Errorf("jump edge %+v", e)
	}
	if e := d.Edges[3]; e.Target != 9 || e.Aim != (nav.Vec3{180, 0, 24}) || e.Flags != nav.EdgeSpawnWorld || e.Effects[0].Pose != -1 || e.Effects[0].Blocker != -1 {
		t.Errorf("touch edge %+v", e)
	}
	if e := d.Edges[4]; e.Effects[0].Pose != 0 || e.Effects[0].Blocker != 1 {
		t.Errorf("item effect %+v", e.Effects[0])
	}
	if n := d.Nodes[4]; n.Blocker != 1 || n.Pose != 1 || n.Yaw != 45 || !n.OnMover() || d.Nodes[0].Blocker != -1 {
		t.Errorf("nodes %+v %+v", n, d.Nodes[0])
	}
	if b := d.Blockers[0]; b.Headnode != 5 || !b.Solid || b.Maxs != (nav.Vec3{8, 64, 128}) || d.Blockers[2].End != (nav.Vec3{100, 2, 3}) {
		t.Errorf("blockers %+v", d.Blockers)
	}
	if len(d.Solids) != 1 || !d.Solids[0].Box || len(d.Pushes) != 1 || !d.Pushes[0].Once || len(d.Teleports) != 1 || d.Volumes[1].Blocker != 1 {
		t.Errorf("solids/hooks/volumes %+v %+v %+v %+v", d.Solids, d.Pushes, d.Teleports, d.Volumes)
	}
	if lo, hi := d.OutRange(1); lo != 1 || hi != 3 {
		t.Errorf("out range of node 1: %d..%d", lo, hi)
	}
}

func TestDecodeRejectsCorruptFiles(t *testing.T) {
	good := encode(t, testGraph(t))
	gz := func(s string) []byte {
		var b bytes.Buffer
		w := gzip.NewWriter(&b)
		w.Write([]byte(s))
		w.Close()
		return b.Bytes()
	}
	// a valid file edited as JSON
	edit := func(old, new string) []byte {
		zr, err := gzip.NewReader(bytes.NewReader(good))
		if err != nil {
			t.Fatal(err)
		}
		var raw bytes.Buffer
		raw.ReadFrom(zr)
		s := raw.String()
		if !strings.Contains(s, old) {
			t.Fatalf("%q not in the encoding", old)
		}
		return gz(strings.Replace(s, old, new, 1))
	}
	cases := map[string][]byte{
		"empty":           nil,
		"not gzip":        []byte("{\"format\":1}"),
		"truncated":       good[:len(good)/2],
		"bad json":        gz("{\"format\":1,\"nodes\":[{"),
		"other format":    edit(`"format":1`, `"format":99`),
		"edge past nodes": edit(`{"f":4,"t":0`, `{"f":4,"t":77`),
		"unsorted edges":  edit(`{"f":4,"t":0`, `{"f":0,"t":0`),
		"bad edge kind":   edit(`"k":1,"r":1,"x":1`, `"k":42,"r":1,"x":1`),
		"bad blocker req": edit(`"q":[[1,1]]`, `"q":[[7,1]]`),
		"bad node mover":  edit(`"m":2,"r":2`, `"m":9,"r":2`),
		"bad pose":        edit(`"m":2,"p":1`, `"m":2,"p":5`),
		"bad spawn node":  edit(`"node":4`, `"node":40`),
		"bad blocker":     edit(`"kind":1`, `"kind":0`),
		"bad solid id":    edit(`"id":30`, `"id":0`),
		"bad effect kind": edit(`"e":[{"k":4`, `"e":[{"k":0`),
		"negative cost":   edit(`"c":0.7`, `"c":-0.7`),
	}
	for name, data := range cases {
		if g, err := nav.Decode(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: decoded %d nodes without error", name, len(g.Nodes))
		}
	}
}

func FuzzDecode(f *testing.F) {
	good := encode(f, testGraph(f))
	f.Add(good)
	f.Add(good[:len(good)-7])
	f.Add([]byte{0x1f, 0x8b})
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := nav.Decode(bytes.NewReader(data))
		if err != nil {
			return
		}
		// a graph that decodes is consistent enough to use
		for i := range g.Nodes {
			g.Out(nav.NodeID(i))
			g.In(nav.NodeID(i))
		}
		for s := 0; s < 4; s++ {
			g.ForSkill(s)
		}
		g.Localize(nav.Vec3{}, 128)
		g.Reachable([]nav.NodeID{0}, nil)
		if len(g.Edges) > 0 {
			g.Plan(&g.Edges[0])
			nav.Holds(&g.Edges[0], g.SpawnStates())
		}
	})
}

func TestForSkill(t *testing.T) {
	g := testGraph(t)
	// skill 2: the laser spawns (skills 0x0c), the item does not (0x01),
	// spawn 21 does not (0x02)
	s2 := g.ForSkill(2)
	if s2.Skill != 2 || len(s2.Nodes) != len(g.Nodes) || len(s2.Edges) != len(g.Edges) {
		t.Fatalf("skill 2: %d edges", len(s2.Edges))
	}
	if e := s2.Edges[4]; len(e.Effects) != 0 {
		t.Errorf("item that does not spawn kept: %+v", e.Effects)
	}
	if len(s2.Spawns) != 1 || s2.Spawns[0].Entity != 20 {
		t.Errorf("spawns %+v", s2.Spawns)
	}
	if e := s2.Edges[6]; len(e.Reqs) != 2 {
		t.Errorf("laser req resolved although the laser spawns: %+v", e.Reqs)
	}
	// skill 0: no laser, so "laser gone" holds and is dropped
	s0 := g.ForSkill(0)
	if e := s0.Edges[6]; len(e.Reqs) != 1 || e.Reqs[0].Blocker != 1 {
		t.Errorf("skill 0 reqs %+v", e.Reqs)
	}
	if e := s0.Edges[4]; len(e.Effects) != 1 {
		t.Errorf("item spawns at skill 0: %+v", e.Effects)
	}
	if s0.ForSkill(0) != s0 {
		t.Error("ForSkill of a resolved graph for the same skill should be a no-op")
	}

	// an edge that needs a blocker present at a skill where it is not
	g2 := testGraph(t)
	g2.Edges[6].Reqs = []nav.Req{{Blocker: 2, States: nav.Pose(0)}}
	if err := g2.Finish(); err != nil {
		t.Fatal(err)
	}
	if s := g2.ForSkill(0); len(s.Edges) != len(g2.Edges)-1 || s.EdgeIndex(4, 0) >= 0 {
		t.Errorf("edge needing an absent laser on was kept")
	}
	// a touch edge whose target does not spawn is removed
	g3 := testGraph(t)
	g3.Edges[3].Target = 13
	if s := g3.ForSkill(1); s.EdgeIndex(2, 2) >= 0 {
		t.Error("touch edge at an absent item kept")
	}
}

func TestQueries(t *testing.T) {
	g := testGraph(t)
	if out := g.Out(1); len(out) != 2 || out[0].To != 2 || out[1].To != 3 {
		t.Fatalf("Out(1) %+v", out)
	}
	if g.Out(-1) != nil || g.Node(5) != nil || g.Out(99) != nil {
		t.Error("out of range ids")
	}
	if in := g.In(1); len(in) != 2 || g.Edges[in[0]].From != 0 || g.Edges[in[1]].From != 3 {
		t.Errorf("In(1) %v", in)
	}
	if n := g.Neighbors(3); len(n) != 2 || n[0] != 1 || n[1] != 4 {
		t.Errorf("Neighbors(3) %v", n)
	}
	if id := g.Localize(nav.Vec3{30, 4, 30}, 64); id != 1 {
		t.Errorf("Localize %d", id)
	}
	if id := g.Localize(nav.Vec3{1000, 0, 0}, 64); id != nav.NoNode {
		t.Errorf("Localize far %d", id)
	}
	// a node one floor down is further than one a few units away
	if c := g.Nearby(nav.Vec3{64, 64, 0}, 200); len(c) < 2 || c[0].Node != 3 {
		t.Errorf("Nearby %+v", c)
	}
	if g.EdgeIndex(3, 4) != 5 || g.EdgeIndex(0, 4) != -1 {
		t.Error("EdgeIndex")
	}
	if e := g.EffectEdges(9); len(e) != 1 || e[0] != 3 {
		t.Errorf("EffectEdges %v", e)
	}
	if m := g.MoverNodes(1, -1); len(m) != 2 || len(g.MoverNodes(1, 1)) != 1 {
		t.Errorf("MoverNodes %v", m)
	}
	if g.BlockerOf(11) != 1 || g.BlockerOf(999) != -1 {
		t.Error("BlockerOf")
	}
	if a := g.ArriveMode(4); a != navsim.ArriveGround {
		t.Errorf("arrive %v", a)
	}
	p := g.Plan(&g.Edges[3])
	if p.Target != (nav.Vec3{180, 0, 24}) || p.From != g.Nodes[2].Origin || p.StepMsec != 25 {
		t.Errorf("touch plan %+v", p)
	}
	if p := g.Plan(&g.Edges[1]); p.Target != g.Nodes[2].Origin || p.Takeoff != (nav.Vec3{40, 0, 24.125}) || p.BackupMsec != 300 {
		t.Errorf("jump plan %+v", p)
	}

	spawn := g.SpawnStates()
	if spawn[0] != nav.Pose(0) || spawn[1] != nav.Pose(1) || spawn[2] != nav.Pose(0) {
		t.Fatalf("spawn states %v", spawn)
	}
	// at spawn: the door is closed, the plat at the bottom
	seen := g.Reachable([]nav.NodeID{0}, func(e *nav.Edge) bool { return nav.Holds(e, spawn) })
	if !seen[0] || !seen[1] || !seen[2] || seen[3] || seen[4] {
		t.Errorf("reachable at spawn %v", seen)
	}
	open := append([]nav.StateMask(nil), spawn...)
	open[0], open[1] = nav.Pose(0)|nav.Pose(1), nav.Pose(0)|nav.Pose(1)
	if seen := g.Reachable([]nav.NodeID{0}, func(e *nav.Edge) bool { return nav.Holds(e, open) }); !seen[3] || !seen[4] {
		t.Errorf("reachable with the door open %v", seen)
	}
	if all := g.Reachable([]nav.NodeID{0}, nil); !all[4] {
		t.Error("reachable over all edges")
	}
	if f := (nav.Effect{Pose: 0, Blocker: 1}); f.Holds(spawn) || !f.Holds(open) {
		t.Error("item effect on the plat top")
	}
	st := g.Stats()
	if st.Nodes != 5 || st.Edges != 7 || st.Conditional != 4 || st.Fast != 1 || st.ByKind["walk"] != 4 {
		t.Errorf("stats %+v", st)
	}
	b := g.Blockers[2]
	if b.All() != nav.Pose(0)|nav.StateGone || g.Blockers[1].All() != nav.Pose(0)|nav.Pose(1) {
		t.Errorf("All %b %b", b.All(), g.Blockers[1].All())
	}
}

func TestNamesAndFlags(t *testing.T) {
	if s := (nav.NodeWater | nav.NodeBreath).String(); s != "water|breath" {
		t.Errorf("node flags %q", s)
	}
	if s := (nav.EdgeFast | nav.EdgeStep).String(); s != "fast|step" {
		t.Errorf("edge flags %q", s)
	}
	for k := nav.EdgeWalk; k <= nav.EdgeTouch; k++ {
		if k.String() == "?" || k.String() == "" {
			t.Errorf("kind %d unnamed", k)
		}
	}
	if nav.EdgeKind(0).String() != "?" || nav.BlockerKind(99).String() != "?" || nav.EffectKind(0).String() != "?" {
		t.Error("unknown names")
	}
}

func TestParams(t *testing.T) {
	p := nav.DefaultParams()
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	h := p.PhysicsHash()
	if len(h) != 16 || h != nav.DefaultParams().PhysicsHash() {
		t.Fatalf("hash %q", h)
	}
	q := p
	q.Gravity = 600
	if q.PhysicsHash() == h {
		t.Error("gravity does not change the physics hash")
	}
	q = p
	q.Grid = 16
	if q.PhysicsHash() == h {
		t.Error("grid does not change the physics hash")
	}
	if name := nav.CacheName("demo1", 0xbfd75753, p); name != "demo1-bfd75753-v1-"+h+".json.gz" {
		t.Errorf("cache name %q", name)
	}
	for _, bad := range []func(*nav.Params){
		func(p *nav.Params) { p.Grid = 1 },
		func(p *nav.Params) { p.StepMsec = 0 },
		func(p *nav.Params) { p.MaxDegree = 1 },
		func(p *nav.Params) { p.LedgeReach = -1 },
		func(p *nav.Params) { p.WaterGrid = 1000 },
	} {
		q := p
		bad(&q)
		if q.Validate() == nil {
			t.Errorf("params %+v accepted", q)
		}
	}
	if ph := p.Physics(); ph.Gravity != 800 || ph.AirAccelerate != 0 || ph.StepMsec != 25 || ph.FrameMsec != 100 {
		t.Errorf("physics %+v", ph)
	}
}
