package navrt

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// randomGraph returns a random graph on a plane (with some height) whose
// edge costs are at least the horizontal distance at pmove's top speed, as
// the builder's measured costs are: the A* heuristic is admissible on it.
func randomGraph(rng *rand.Rand, nodes, degree int) *nav.Graph {
	ns := make([]nav.Node, nodes)
	for i := range ns {
		ns[i] = nav.Node{Origin: nav.Vec3{float32(rng.Intn(4096) - 2048), float32(rng.Intn(4096) - 2048), float32(rng.Intn(512))}, Blocker: -1}
	}
	var es []nav.Edge
	for i := range ns {
		for k := 0; k < degree; k++ {
			j := rng.Intn(nodes)
			if j == i {
				continue
			}
			d := distH(ns[i].Origin, ns[j].Origin)
			c := d/300 + float32(rng.Float64()*2)
			if rng.Intn(10) == 0 {
				c = d / 300 // exactly the bound: ties are common in real graphs
			}
			es = append(es, nav.Edge{From: nav.NodeID(i), To: nav.NodeID(j), Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: c})
		}
	}
	sort.SliceStable(es, func(a, b int) bool { return es[a].From < es[b].From })
	g, err := nav.New(ns, es)
	if err != nil {
		panic(err)
	}
	return g
}

func pathCost(g *nav.Graph, p Path, cost CostFunc) float32 {
	c := float32(0)
	at := p.Start
	for _, i := range p.Edges {
		e := &g.Edges[i]
		if e.From != at {
			return float32(math.NaN())
		}
		x, _ := cost(i, e)
		c += x
		at = e.To
	}
	return c
}

// TestAStarMatchesDijkstra: on random graphs with admissible costs A*
// finds paths exactly as cheap as Dijkstra, valid edge chains whose cost
// is what they report, and never expands more nodes.
func TestAStarMatchesDijkstra(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var expA, expD int
	for trial := 0; trial < 60; trial++ {
		g := randomGraph(rng, 50+rng.Intn(400), 1+rng.Intn(5))
		pl := NewPlanner(g)
		blocked := map[int]bool{}
		for i := range g.Edges {
			if rng.Intn(8) == 0 {
				blocked[i] = true
			}
		}
		cost := func(i int, e *nav.Edge) (float32, bool) { return e.Cost, !blocked[i] }
		for q := 0; q < 20; q++ {
			start := nav.NodeID(rng.Intn(len(g.Nodes)))
			tg := NewTarget(g)
			for k := 0; k < 1+rng.Intn(3); k++ {
				tg.AddNode(g, nav.NodeID(rng.Intn(len(g.Nodes))))
			}
			if rng.Intn(3) == 0 {
				tg.AddEdge(g, rng.Intn(len(g.Edges)))
			}
			pa, oka := pl.Find(start, tg, cost)
			pd, okd := pl.Dijkstra(start, tg, cost)
			if oka != okd {
				t.Fatalf("trial %d: A* found %v, Dijkstra %v", trial, oka, okd)
			}
			if !oka {
				continue
			}
			if d := math.Abs(float64(pa.Cost - pd.Cost)); d > 1e-3 {
				t.Fatalf("trial %d: A* cost %v, Dijkstra %v", trial, pa.Cost, pd.Cost)
			}
			for _, p := range []Path{pa, pd} {
				if c := pathCost(g, p, cost); math.Abs(float64(c-p.Cost)) > 1e-3 {
					t.Fatalf("trial %d: path %v costs %v, reports %v", trial, p.Edges, c, p.Cost)
				}
				end := p.End(g)
				if p.GoalEdge {
					if len(p.Edges) == 0 || !tg.Edge(p.Edges[len(p.Edges)-1]) {
						t.Fatalf("trial %d: goal edge path %v does not end with a goal edge", trial, p.Edges)
					}
				} else if !tg.Node(end) {
					t.Fatalf("trial %d: path ends at %d, not a goal", trial, end)
				}
			}
			expA += pa.Expanded
			expD += pd.Expanded
		}
	}
	if expA > expD {
		t.Errorf("A* expanded %d nodes, Dijkstra %d", expA, expD)
	}
	t.Logf("expanded: A* %d, Dijkstra %d", expA, expD)
}

func TestPlannerTrivialAndUnreachable(t *testing.T) {
	g := randomGraph(rand.New(rand.NewSource(2)), 20, 2)
	pl := NewPlanner(g)
	all := func(i int, e *nav.Edge) (float32, bool) { return e.Cost, true }
	tg := NewTarget(g)
	tg.AddNode(g, 3)
	if p, ok := pl.Find(3, tg, all); !ok || len(p.Edges) != 0 || p.Cost != 0 {
		t.Errorf("start at the goal: %+v %v", p, ok)
	}
	none := func(i int, e *nav.Edge) (float32, bool) { return 0, false }
	if _, ok := pl.Find(0, tg, none); ok {
		t.Error("found a path without edges")
	}
	if _, ok := pl.Find(0, NewTarget(g), all); ok {
		t.Error("found a path to an empty target")
	}
	if _, ok := pl.Find(nav.NoNode, tg, all); ok {
		t.Error("found a path from no node")
	}
}

// lineGraph: 0 -> 1 -> 2 -> 3 along x, 32 units apart, plus a detour
// 0 -> 4 -> 3. Edge 1->2 needs blocker 0 (an auto door, 1 s travel) at
// pose 1 (open); 4 is reached through trigger #77 and touch edge 2->2
// presses button #88.
func lineGraph(t testing.TB) *nav.Graph {
	t.Helper()
	g := &nav.Graph{
		Format: nav.FormatVersion, Map: "line", Params: nav.DefaultParams(), Skill: 1,
		Blockers: []nav.Blocker{
			{Entity: 50, Class: "func_door", Model: "*1", Kind: nav.BlockDoor, Skills: nav.AllSkills,
				Poses: []nav.BlockerPose{{Name: "pos1"}, {Name: "pos2", Origin: nav.Vec3{0, 0, 100}}}},
			{Entity: 51, Class: "func_door", Model: "*2", Kind: nav.BlockDoor, Skills: nav.AllSkills,
				Poses: []nav.BlockerPose{{Name: "pos1"}, {Name: "pos2", Origin: nav.Vec3{0, 0, 100}}}},
		},
		Nodes: []nav.Node{
			{Origin: nav.Vec3{0, 0, 24}, Blocker: -1},
			{Origin: nav.Vec3{32, 0, 24}, Blocker: -1},
			{Origin: nav.Vec3{64, 0, 24}, Blocker: -1},
			{Origin: nav.Vec3{96, 0, 24}, Blocker: -1},
			{Origin: nav.Vec3{48, 300, 24}, Blocker: -1},
		},
		Edges: []nav.Edge{
			{From: 0, To: 1, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.1},
			{From: 0, To: 4, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 1.5,
				Effects: []nav.Effect{{Kind: nav.EffTrigger, Entity: 77, Pose: -1, Blocker: -1}}},
			{From: 1, To: 2, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.1, Reqs: []nav.Req{{Blocker: 0, States: nav.Pose(1)}}},
			{From: 2, To: 2, Kind: nav.EdgeTouch, Recipe: navsim.RecipeWalk, Cost: 0.4, Target: 88, Aim: nav.Vec3{80, 0, 24},
				Effects: []nav.Effect{{Kind: nav.EffButton, Entity: 88, Pose: -1, Blocker: -1}}},
			{From: 2, To: 3, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 0.1, Reqs: []nav.Req{{Blocker: 1, States: nav.Pose(1)}}},
			{From: 4, To: 3, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 1.5},
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	return g
}

func edgeOf(g *nav.Graph, from, to nav.NodeID, kind nav.EdgeKind) int {
	lo, hi := g.OutRange(from)
	for i := lo; i < hi; i++ {
		if g.Edges[i].To == to && g.Edges[i].Kind == kind {
			return i
		}
	}
	return -1
}

func planTo(t *testing.T, n *Navigator, from nav.NodeID, goal Goal) (Path, bool) {
	t.Helper()
	return n.pl.Find(from, CompileTarget(n.g, goal), n.cost)
}

// TestConditionalCosts: an edge whose door the bot can open by walking up
// costs its travel time on top; one that waiting cannot open is left out
// and the detour taken; seeing the door open makes it free.
func TestConditionalCosts(t *testing.T) {
	g := lineGraph(t)
	n := New(g, nil, Config{})
	n.ms.info[0].auto, n.ms.info[0].travel = true, 1
	// door 1 (#51) is use-only: closed, nothing opens it
	p, ok := planTo(t, n, 0, NodeGoal(3))
	if !ok || len(p.Edges) != 2 || g.Edges[p.Edges[0]].To != 4 {
		t.Fatalf("with door 1 shut the path goes round by node 4: %+v %v", p, ok)
	}
	// door 1 seen open: through the line, paying door 0's travel time
	n.ms.bel[1] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
	p, ok = planTo(t, n, 0, NodeGoal(3))
	if !ok || len(p.Edges) != 3 {
		t.Fatalf("path %+v %v", p, ok)
	}
	if want := float32(0.1 + 0.1 + 1 + 0.1); math.Abs(float64(p.Cost-want)) > 1e-5 {
		t.Errorf("cost %v, want %v (door 0 unknown: its travel time added)", p.Cost, want)
	}
	// door 0 seen open too: no wait
	n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
	if p, _ = planTo(t, n, 0, NodeGoal(3)); math.Abs(float64(p.Cost-0.3)) > 1e-5 {
		t.Errorf("cost %v with both doors open", p.Cost)
	}
	// a blocked edge is left out until its mark ends
	e12 := edgeOf(g, 1, 2, nav.EdgeWalk)
	until := n.MarkBlocked(e12, 0)
	if until != BlockBase {
		t.Errorf("first mark ends at %d", until)
	}
	if p, _ = planTo(t, n, 0, NodeGoal(3)); g.Edges[p.Edges[0]].To != 4 {
		t.Errorf("blocked edge used: %+v", p)
	}
	n.now = BlockBase
	if p, _ = planTo(t, n, 0, NodeGoal(3)); len(p.Edges) != 3 {
		t.Errorf("expired mark still blocks: %+v", p)
	}
}

// TestAvoidAndTouchEdges: edges setting off an avoided entity are left out
// of every path; touch edges are used only for a goal about their target;
// a touch goal is satisfied by any edge with the effect (here the walk
// through trigger #77).
func TestAvoidAndTouchEdges(t *testing.T) {
	g := lineGraph(t)
	n := New(g, nil, Config{Avoid: []int32{77}})
	n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
	if _, ok := planTo(t, n, 0, NodeGoal(4)); ok {
		t.Error("reached node 4 through the avoided trigger")
	}
	if _, ok := planTo(t, n, 0, TouchGoal(77)); ok {
		t.Error("planned to set off an avoided trigger")
	}
	n = New(g, nil, Config{})
	n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
	p, ok := planTo(t, n, 0, TouchGoal(77))
	if !ok || !p.GoalEdge || len(p.Edges) != 1 || g.Edges[p.Edges[0]].To != 4 {
		t.Errorf("touch goal on trigger #77: %+v %v", p, ok)
	}
	touch := edgeOf(g, 2, 2, nav.EdgeTouch)
	if c, ok := n.cost(touch, &g.Edges[touch]); ok {
		t.Errorf("touch edge usable without a goal about it (cost %v)", c)
	}
	if err := n.SetGoal(TouchGoal(88), 0); err != nil {
		t.Fatal(err)
	}
	p, ok = planTo(t, n, 0, TouchGoal(88))
	if !ok || p.Edges[len(p.Edges)-1] != touch || !p.GoalEdge {
		t.Errorf("button goal path %+v %v", p, ok)
	}
	// a touch goal on an entity no edge sets off is refused
	if err := n.SetGoal(TouchGoal(99), 0); err == nil {
		t.Error("SetGoal accepted an unreachable touch target")
	}
}

func TestCostPenalties(t *testing.T) {
	g := lineGraph(t)
	n := New(g, nil, Config{})
	e := g.Edges[0]
	base, _ := n.cost(0, &e)
	e.Flags = nav.EdgeFragile
	if c, _ := n.cost(0, &e); c != base+DefaultFragilePenalty {
		t.Errorf("fragile %v, base %v", c, base)
	}
	e.Flags = nav.EdgeFromRest
	if c, _ := n.cost(0, &e); c != base+StopCost {
		t.Errorf("from rest %v", c)
	}
	e.Flags, e.FallDamage, e.Damage = 0, 10, 5
	if c, _ := n.cost(0, &e); math.Abs(float64(c-(base+15*DefaultDamageCost))) > 1e-5 {
		t.Errorf("damage %v", c)
	}
	e.FallDamage, e.Damage = 0, 0
	e.Effects = []nav.Effect{{Kind: nav.EffButton, Entity: 88, Pose: -1, Blocker: -1}}
	if c, _ := n.cost(0, &e); c != base+DefaultButtonPenalty {
		t.Errorf("incidental button %v", c)
	}
	ride := nav.Edge{From: 0, To: 1, Kind: nav.EdgeRide, Recipe: navsim.RecipeRide, Flags: nav.EdgeNeedsUse, Cost: 1}
	if _, ok := n.cost(0, &ride); ok {
		t.Error("a ride needing a use is planned without AllowNeedsUse")
	}
	n.cfg.AllowNeedsUse = true
	if _, ok := n.cost(0, &ride); !ok {
		t.Error("AllowNeedsUse")
	}
	// a node a visible monster stands on costs more to walk to
	n.occ = []occupant{{id: "e1", lo: Vec3{16, -16, 0}, hi: Vec3{48, 16, 56}, visible: true, solid: -1}}
	if c, _ := n.cost(0, &g.Edges[0]); c != base+OccupiedCost {
		t.Errorf("occupied %v", c)
	}
}

func TestBlockedBackoff(t *testing.T) {
	g := lineGraph(t)
	ms := NewMapState(g, nil)
	b := NewBlocked()
	var want int64 = BlockBase
	for k := 0; k < 6; k++ {
		now := int64(k) * 1000000
		if until := b.Mark(3, []int32{0}, ms, now); until-now != want {
			t.Errorf("mark %d lasts %d, want %d", k, until-now, want)
		}
		if !b.Active(3, now+want-1) || b.Active(3, now+want) {
			t.Errorf("mark %d active window", k)
		}
		want = min(want*2, BlockMax)
	}
	if b.Count(3) != 6 || len(b.Edges(5000000)) != 1 {
		t.Errorf("count %d, edges %v", b.Count(3), b.Edges(5000000))
	}
	// a change of the related blocker's belief clears the mark (and the
	// backoff)
	if got := b.Refresh(ms); len(got) != 0 {
		t.Errorf("nothing changed, cleared %v", got)
	}
	ms.rev[0]++
	if got := b.Refresh(ms); len(got) != 1 || got[0] != 3 || b.Active(3, 5000000) || b.Count(3) != 0 {
		t.Errorf("cleared %v, still active %v, count %d", got, b.Active(3, 5000000), b.Count(3))
	}
}
