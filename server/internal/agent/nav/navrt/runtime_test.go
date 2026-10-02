package navrt

import (
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
)

// platGraph: floor node 0 boards a touch plat (blocker 0, spawning at its
// bottom = pose 1) at node 1, rides it up to node 2 (pose 0) and walks off
// at the top to node 3; node 4 is a floor node beside the shaft whose
// walk to node 3 needs the plat up (it passes over it).
func platGraph(t testing.TB) *nav.Graph {
	t.Helper()
	g := &nav.Graph{
		Format: nav.FormatVersion, Map: "plat", Params: nav.DefaultParams(), Skill: 1,
		Blockers: []nav.Blocker{{Entity: 60, Class: "func_plat", Model: "*1", Kind: nav.BlockPlat, Skills: nav.AllSkills, Spawn: 1,
			Poses: []nav.BlockerPose{{Name: "top"}, {Name: "bottom", Origin: Vec3{0, 0, -128}}}}},
		Nodes: []nav.Node{
			{Origin: Vec3{-64, 0, -104}, Blocker: -1},
			{Origin: Vec3{0, 0, -104}, Blocker: 0, Pose: 1, Flags: nav.NodeMover},
			{Origin: Vec3{0, 0, 24}, Blocker: 0, Pose: 0, Flags: nav.NodeMover},
			{Origin: Vec3{64, 0, 24}, Blocker: -1},
			{Origin: Vec3{0, 64, 24}, Blocker: -1},
		},
		Edges: []nav.Edge{
			{From: 0, To: 1, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.2, Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(1)}}},
			{From: 1, To: 2, Kind: nav.EdgeRide, Recipe: navsim.RecipeRide, Cost: 1.5, Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(1)}}},
			{From: 2, To: 3, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.2, Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(0)}}},
			{From: 4, To: 3, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.3, Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(0)}}},
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestCarriedCondition: the walk off a plat at its top needs the plat up,
// which the belief (the plat as it spawns, down) does not meet; but a bot
// on the plat's top node has it there under its feet, so the ride and the
// walk off plan together. The same condition on an edge that does not
// start on the plat is not carried.
func TestCarriedCondition(t *testing.T) {
	g := platGraph(t)
	n := New(g, nil, Config{})
	n.ms.info[0].auto, n.ms.info[0].travel = true, 1.5
	p, ok := planTo(t, n, 0, NodeGoal(3))
	if !ok || len(p.Edges) != 3 {
		t.Fatalf("board, ride and walk off: %+v %v", p, ok)
	}
	if !n.carried(&g.Edges[2], g.Edges[2].Reqs[0]) {
		t.Error("the walk off the plat's top does not count its own plat as carried")
	}
	if n.carried(&g.Edges[3], g.Edges[3].Reqs[0]) {
		t.Error("an edge from the floor counts the plat as carried")
	}
	if _, ok := planTo(t, n, 4, NodeGoal(3)); ok {
		t.Error("planned over the plat's shaft with the plat believed down")
	}
	// at runtime, standing on the plat's top node the edge needs no wait
	if ok, wait := n.holds(&g.Edges[2]); !ok || wait != 0 {
		t.Errorf("holds the walk off: %v %v", ok, wait)
	}
}

// TestLocalizeSkipsUnusable: a node all of whose ways out need a blocker
// gone (a spot a func_explosive takes now) is passed over by Localize for
// the next node that has a way out.
func TestLocalizeSkipsUnusable(t *testing.T) {
	g := &nav.Graph{
		Format: nav.FormatVersion, Map: "x", Params: nav.DefaultParams(), Skill: 1,
		Blockers: []nav.Blocker{{Entity: 70, Class: "func_explosive", Model: "*1", Kind: nav.BlockExplosive, Skills: nav.AllSkills,
			Poses: []nav.BlockerPose{{Name: "there"}}}},
		Nodes: []nav.Node{
			{Origin: Vec3{0, 0, 24}, Blocker: -1},
			{Origin: Vec3{40, 0, 24}, Blocker: -1},
			{Origin: Vec3{80, 0, 24}, Blocker: -1},
		},
		Edges: []nav.Edge{
			{From: 0, To: 2, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.3, Reqs: []nav.Req{{Blocker: 0, States: nav.StateGone}}},
			{From: 1, To: 2, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.2},
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	n := New(g, nil, Config{})
	if n.usable(0) || !n.usable(1) || !n.usable(2) {
		t.Errorf("usable: %v %v %v", n.usable(0), n.usable(1), n.usable(2))
	}
	if id := n.Localize(Vec3{2, 0, 24}, false); id != 1 {
		t.Errorf("localized at node %d, want 1 (node 0 has no way out while the explosive stands)", id)
	}
	// once the explosive is seen gone, node 0 is fine again
	n.ms.bel[0] = BlockerBelief{Status: BlockerGone, Pose: -1, Observed: true}
	if id := n.Localize(Vec3{2, 0, 24}, false); id != 0 {
		t.Errorf("explosive gone: localized at node %d, want 0", id)
	}
}

// TestSimArrivalBrakes: on reaching a node goal the bot brakes to rest on
// it (a run into a node at full speed would coast 50 units past it, off
// a ledge maybe), and stays arrived.
func TestSimArrivalBrakes(t *testing.T) {
	s := newSim(t, Config{})
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	goal := s.nodeAt(64, 0)
	if st := s.run(NodeGoal(goal), 5000); st.Follow != Arrived {
		t.Fatalf("%s: %s %s", st.Follow, st.Cause, st.Reason)
	}
	for k := 0; k < 40; k++ {
		s.tick()
		if st := s.nav.Status(); st.Follow != Arrived {
			t.Fatalf("command %d after arriving: %s", k, st.Follow)
		}
	}
	st := s.srv.State()
	if !navsim.Stopped(&st, s.g.Nodes[goal].Origin) {
		t.Errorf("not at rest on the goal node: at %v, %.1f units/s", st.Origin(), st.HSpeed())
	}
}

// TestSimMonsterOnPathRepaths: a monster that comes into view on the path
// ahead makes the navigator plan again at once, round it.
func TestSimMonsterOnPathRepaths(t *testing.T) {
	s := newSim(t, Config{})
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	if err := s.nav.SetGoal(NodeGoal(s.nodeAt(64, 0)), 0); err != nil {
		t.Fatal(err)
	}
	s.tick()
	onPath := func() bool {
		path, cur := s.nav.Path()
		for k := cur; k < len(path.Edges); k++ {
			if to := s.g.Nodes[s.g.Edges[path.Edges[k]].To].Origin; to[0] == 0 && to[1] == 0 {
				return true
			}
		}
		return false
	}
	if !onPath() {
		t.Skip("the straight path does not cross the middle node")
	}
	repaths := s.nav.Status().Repaths
	s.b.Tracks = []worldmodel.Track{{ID: "e1", Class: "soldier", Kind: "monster", Pos: Vec3{0, 0, 24}, PosKnown: true,
		Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}, Visible: true, LastSeen: s.now, Life: worldmodel.LifeAlive}}
	s.b.Frames++
	s.b.Time = s.now
	s.tick()
	if st := s.nav.Status(); st.Repaths <= repaths {
		t.Errorf("no repath for the monster on the path (%d repaths)", st.Repaths)
	}
	if onPath() {
		t.Error("the new path still walks onto the monster's spot")
	}
}

// TestPlatWaitSpot: waiting for a plat to come down, the bot stands out of
// its center trigger (inside, it keeps a plat at its top up for ever);
// for a door the trigger does not matter.
func TestPlatWaitSpot(t *testing.T) {
	g := platGraph(t)
	n := New(g, nil, Config{})
	n.ms.info[0].trigger = &mapdata.Box{Min: Vec3{-40, -40, -140}, Max: Vec3{40, 40, 40}}
	in, out := Vec3{-30, 0, -104}, Vec3{-96, 0, -104}
	if n.safeWait(in, 0) {
		t.Error("a spot in the plat's trigger is a safe wait")
	}
	if !n.safeWait(out, 0) {
		t.Error("a spot outside the trigger and the plat's path is not a safe wait")
	}
	if !n.safeWait(in, -1) {
		t.Error("the trigger matters for a plat only")
	}
}

// TestDemoStatic checks the static data the navigator derives from each
// demo map (no session).
func TestDemoStatic(t *testing.T) {
	for _, name := range []string{"demo1", "demo2", "demo3"} {
		t.Run(name, func(t *testing.T) {
			d := loadDemo(t, name)
			t.Run("MapState", func(t *testing.T) { testMapStateFromMapData(t, d) })
			t.Run("StartsInside", func(t *testing.T) { testStartsInside(t, d) })
		})
	}
}

// testStartsInside: on the demo maps the edges that start where a mover
// goes down to (the floor of a plat's shaft, under demo1's car) are left
// out, those where a mover comes up under the bot's feet and lifts it
// onto the edge's end (demo3's key pedestal *19) are not, and no edge
// without a mover condition is.
func testStartsInside(t *testing.T, d *demoLevel) {
	c, ok := map[string]struct {
		out    []string // models with excluded edges
		keep   string   // a model none of whose edges is excluded
		minOut int
	}{
		"demo1": {[]string{"*31"}, "", 100},
		"demo2": {[]string{"*46", "*52"}, "", 100},
		"demo3": {[]string{"*36", "*40"}, "*19", 50},
	}[d.md.Name]
	if !ok {
		t.Skip("no expectations for", d.md.Name)
	}
	g := d.g
	n := New(g, d.md, Config{})
	excluded := map[string]int{}
	total := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if !n.startsInside(i, e) {
			continue
		}
		total++
		if len(e.Reqs) == 0 {
			t.Errorf("edge %d has no condition but starts inside", i)
		}
		for _, r := range e.Reqs {
			excluded[g.Blockers[r.Blocker].Model]++
		}
	}
	for _, m := range c.out {
		if excluded[m] == 0 {
			t.Errorf("no edge on %s left out", m)
		}
	}
	if c.keep != "" && excluded[c.keep] > 0 {
		t.Errorf("%d edges on %s left out (it lifts the bot)", excluded[c.keep], c.keep)
	}
	if total < c.minOut {
		t.Errorf("only %d edges left out", total)
	}
	t.Logf("%d edges start inside a mover: %v", total, excluded)
}

// TestSimPointAndVolumeGoals: a point goal is reached standing within its
// radius (and the bot brakes there), a volume goal once the player box
// touches the box.
func TestSimPointAndVolumeGoals(t *testing.T) {
	s := newSim(t, Config{})
	s.g.Place(s.srv, s.nodeAt(-64, -64))
	p := Vec3{48, 40, 24}
	if st := s.run(PointGoal(p, 24), 5000); st.Follow != Arrived {
		t.Fatalf("point goal: %s %s %s", st.Follow, st.Cause, st.Reason)
	}
	for k := 0; k < 40; k++ {
		s.tick()
	}
	o := s.srv.State().Origin()
	if distH(o, p) > 24 {
		t.Errorf("point goal: settled at %v, %.1f units from %v", o, distH(o, p), p)
	}
	lo, hi := Vec3{-80, 50, 0}, Vec3{-60, 80, 60}
	if st := s.run(VolumeGoal(lo, hi), 5000); st.Follow != Arrived {
		t.Fatalf("volume goal: %s %s %s", st.Follow, st.Cause, st.Reason)
	}
	if o := s.srv.State().Origin(); !boxTouch(o, false, lo, hi) {
		t.Errorf("volume goal: arrived at %v, not touching %v..%v", o, lo, hi)
	}
}
