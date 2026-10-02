package navrt

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// TestSafeDir: on the synthetic slab, a short run over the floor is safe;
// one off the slab's edge, or long enough to run off the far edge, is not;
// nor is one into an avoided entity.
func TestSafeDir(t *testing.T) {
	box := nav.Volume{Kind: nav.EffTrigger, Entity: 900, Pose: -1, Blocker: -1, Min: Vec3{-24, -16, -24}, Max: Vec3{-8, 16, 32}}
	s := newSimGraph(t, Config{}, func(g *nav.Graph) { g.Volumes = append(g.Volumes, box) })
	if s.nav.SafeDir(Vec3{1, 0, 0}, 100) {
		t.Error("safe before the first Tick")
	}
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	s.tick()
	if !s.nav.SafeDir(Vec3{1, 0, 0}, 300) {
		t.Error("open floor ahead reported unsafe")
	}
	if s.nav.SafeDir(Vec3{-1, 0, 0}, 300) {
		t.Error("the slab's edge reported safe")
	}
	if s.nav.SafeDir(Vec3{1, 0, 0}, 1500) {
		t.Error("a run off the far edge reported safe")
	}
	s.nav.SetAvoid([]int32{900})
	if s.nav.SafeDir(Vec3{1, 0, 0}, 300) {
		t.Error("a run into an avoided entity reported safe")
	}
}

// pitGraph is two floors: an upper one (nodes 0-2, region 0) and a pit
// (nodes 3-4, region 1) the upper floor drops into (2 -> 3) and nothing
// leads out of.
func pitGraph(t *testing.T) *nav.Graph {
	t.Helper()
	walk := func(a, b nav.NodeID, c float32) nav.Edge {
		return nav.Edge{From: a, To: b, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: c}
	}
	g := &nav.Graph{Format: nav.FormatVersion, Map: "pit", Params: nav.DefaultParams(), Skill: 1,
		Nodes: []nav.Node{
			{Origin: Vec3{0, 0, 24}, Blocker: -1}, {Origin: Vec3{300, 0, 24}, Blocker: -1}, {Origin: Vec3{600, 0, 24}, Blocker: -1},
			{Origin: Vec3{700, 0, -200}, Blocker: -1, Region: 1}, {Origin: Vec3{1000, 0, -200}, Blocker: -1, Region: 1},
		},
		Edges: []nav.Edge{
			walk(0, 1, 1), walk(1, 0, 1), walk(1, 2, 1), walk(2, 1, 1),
			{From: 2, To: 3, Kind: nav.EdgeDrop, Recipe: navsim.RecipeWalk, Cost: 0.5},
			walk(3, 4, 1), walk(4, 3, 1),
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestCanReturnAndReachSize: a spot down a one-way drop is not one to
// come back from, and the pit's reach is its own two nodes.
func TestCanReturnAndReachSize(t *testing.T) {
	g := pitGraph(t)
	n := New(g, nil, Config{})
	up, mid, pit := g.Nodes[0].Origin, g.Nodes[1].Origin, g.Nodes[4].Origin
	if !n.CanReturn(up, mid) || !n.CanReturn(mid, up) {
		t.Error("the upper floor is not returnable")
	}
	if n.CanReturn(up, pit) {
		t.Error("the pit is returnable")
	}
	if !n.CanReturn(pit, up) {
		t.Error("cannot go from the pit's view back to the upper floor (the drop is the way)")
	}
	if !n.CanReturn(up, Vec3{5000, 5000, 5000}) {
		t.Error("a point off the graph is not returnable")
	}
	if k, total := n.ReachSize(pit); k != 2 || total != 5 {
		t.Errorf("pit reach %d of %d, want 2 of 5", k, total)
	}
	if k, total := n.ReachSize(up); k != 5 || total != 5 {
		t.Errorf("upper floor reach %d of %d, want 5 of 5", k, total)
	}
}

// TestPathDistance: PathDistance is the planner's least cost at full
// speed plus the legs to and from the nodes, Distances agrees with the
// planner on random graphs, and an unreachable point has none.
func TestPathDistance(t *testing.T) {
	g := pitGraph(t)
	n := New(g, nil, Config{})
	from, to := Vec3{10, 0, 24}, Vec3{1000, 10, -200}
	d, ok := n.PathDistance(from, to)
	want := float32(1+1+0.5+1)*HorizontalSpeed + 10 + 10
	if !ok || math.Abs(float64(d-want)) > 0.5 {
		t.Errorf("path distance %v (%v), want %v", d, ok, want)
	}
	if _, ok := n.PathDistance(to, from); ok {
		t.Error("a path out of the pit")
	}
	if _, ok := n.PathDistance(from, Vec3{5000, 0, 0}); ok {
		t.Error("a path to a point off the graph")
	}

	rng := rand.New(rand.NewSource(3))
	for k := 0; k < 5; k++ {
		rg := randomGraph(rng, 200, 4)
		rn := New(rg, nil, Config{})
		dist := rn.pl.Distances(0, rn.cost, nil)
		for to := 1; to < len(rg.Nodes); to += 7 {
			p, ok := rn.pl.Find(0, CompileTarget(rg, NodeGoal(nav.NodeID(to))), rn.cost)
			inf := math.IsInf(float64(dist[to]), 1)
			if ok == inf {
				t.Fatalf("graph %d node %d: planner found %v, distance %v", k, to, ok, dist[to])
			}
			if ok && math.Abs(float64(pathCost(rg, p, rn.cost)-dist[to])) > 1e-3 {
				t.Fatalf("graph %d node %d: path cost %v, distance %v", k, to, pathCost(rg, p, rn.cost), dist[to])
			}
		}
	}
}

// wallGraph is a room (nodes 0-7 in a row) and a pocket behind an
// explosive wall (nodes 8-9): the only edges between them (1 <-> 8) need
// the wall gone.
func wallGraph(t *testing.T, extra ...nav.Edge) *nav.Graph {
	t.Helper()
	walk := func(a, b nav.NodeID) nav.Edge {
		return nav.Edge{From: a, To: b, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 1}
	}
	gone := []nav.Req{{Blocker: 0, States: nav.StateGone}}
	through := func(a, b nav.NodeID) nav.Edge {
		e := walk(a, b)
		e.Reqs = gone
		return e
	}
	g := &nav.Graph{Format: nav.FormatVersion, Map: "wall", Params: nav.DefaultParams(), Skill: 1,
		Blockers: []nav.Blocker{{Entity: 40, Class: "func_explosive", Model: "*9", Kind: nav.BlockExplosive, Skills: nav.AllSkills, Solid: true,
			Gone: true, Poses: []nav.BlockerPose{{Name: "spawn"}}}},
	}
	for i := 0; i < 8; i++ {
		g.Nodes = append(g.Nodes, nav.Node{Origin: Vec3{300 - float32(i)*100, 0, 24}, Blocker: -1})
	}
	g.Nodes = append(g.Nodes, nav.Node{Origin: Vec3{400, 0, 24}, Blocker: -1}, nav.Node{Origin: Vec3{700, 0, 24}, Blocker: -1})
	g.Edges = append(g.Edges, through(1, 8), through(8, 1), walk(8, 9), walk(9, 8))
	for i := nav.NodeID(0); i < 7; i++ {
		g.Edges = append(g.Edges, walk(i, i+1), walk(i+1, i))
	}
	g.Edges = append(g.Edges, extra...)
	sort.SliceStable(g.Edges, func(a, b int) bool { return g.Edges[a].From < g.Edges[b].From })
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	return g
}

// TestInferPassage: standing in the pocket behind an explosive wall the
// belief holds intact, the bot must have come through it: the wall is
// assumed gone and the way out planned. Not when the pocket has an open
// way in (a drop: the bot may have come that way) or a spawn point (it
// may have started there).
func TestInferPassage(t *testing.T) {
	g := wallGraph(t)
	n := New(g, nil, Config{})
	if p, ok := n.pl.Find(9, CompileTarget(g, NodeGoal(0)), n.cost); ok {
		t.Fatalf("a way out through the intact wall: %v", p)
	}
	if !n.inferPassage(9) {
		t.Fatal("no passage inferred from the pocket")
	}
	if st := n.MapState().Belief(0).Status; st != BlockerGone {
		t.Fatalf("wall belief %s, want gone", st)
	}
	if _, ok := n.pl.Find(9, CompileTarget(g, NodeGoal(0)), n.cost); !ok {
		t.Error("no way out once the wall is gone")
	}
	if n.inferPassage(9) {
		t.Error("inferred again with the wall already gone")
	}

	// a drop into the pocket: no proof
	drop := nav.Edge{From: 0, To: 9, Kind: nav.EdgeDrop, Recipe: navsim.RecipeWalk, Cost: 1}
	n = New(wallGraph(t, drop), nil, Config{})
	if n.inferPassage(9) {
		t.Error("inferred a passage into a pocket with a drop into it")
	}
	// a spawn point in the pocket: no proof
	md := &mapdata.Map{Spawns: []mapdata.Spawn{{Origin: Vec3{700, 0, 24}}}}
	n = New(wallGraph(t), md, Config{})
	if n.inferPassage(9) {
		t.Error("inferred a passage into a pocket with a spawn point")
	}
	// from the room the wall is not proven gone either
	n = New(wallGraph(t), nil, Config{})
	if n.inferPassage(0) {
		t.Error("inferred a passage from the room")
	}
}

// TestDriverMoveHook: the Driver's Move hook replaces the navigator's
// intent before the command is composed, sees the predicted state, and
// Navigator.SafeDir is valid in it.
func TestDriverMoveHook(t *testing.T) {
	s := newSim(t, Config{})
	d := NewDriver(s.nav)
	var sawOrigin Vec3
	var safeAhead, safeBack bool
	d.Move = func(in control.MoveIntent, st *navsim.State) control.MoveIntent {
		sawOrigin = st.Origin()
		safeAhead, safeBack = s.nav.SafeDir(Vec3{1, 0, 0}, 300), s.nav.SafeDir(Vec3{-1, 0, 0}, 300)
		return control.MoveIntent{WishDir: Vec3{0, 1, 0}, Speed: control.MaxMove, FaceYaw: 0}
	}
	var got control.MoveIntent
	d.OnCmd = func(in control.MoveIntent, _ shared.UserCmd, _ *navsim.State) { got = in }
	c := fakeclient.NewPassive(fakeclient.Options{})
	c.Frame.Valid, c.Frame.ServerFrame = true, 10
	ps := &c.Frame.PlayerState
	ps.PMove.PmType = q2const.PM_NORMAL
	ps.PMove.PmFlags = q2const.PMF_ON_GROUND
	ps.PMove.Origin = [3]int16{-64 * 8, 0, 24 * 8}
	u := d.Cmd(c, CmdMsec)
	if sawOrigin != (Vec3{-64, 0, 24}) {
		t.Errorf("the hook saw origin %v", sawOrigin)
	}
	if !safeAhead || safeBack {
		t.Errorf("SafeDir in the hook: ahead %v, off the edge %v", safeAhead, safeBack)
	}
	if got.WishDir != (Vec3{0, 1, 0}) {
		t.Errorf("the command was composed from %+v, not the hook's intent", got)
	}
	// facing +x, a wish to +y is a strafe to the left
	if u.SideMove >= 0 || u.ForwardMove != 0 {
		t.Errorf("usercmd forward %d side %d, want a left strafe", u.ForwardMove, u.SideMove)
	}
}
