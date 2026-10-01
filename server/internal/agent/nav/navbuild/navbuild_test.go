package navbuild_test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/testutil"
)

// Full-map tests share the nav cache: $Q2_NAV_DIR, else <repo>/assets/nav
// (what "q2nav build" writes). A missing graph is built and cached; under
// the race detector that takes about half a minute per map, so it is only
// done with Q2_AGENT_LONG=1 (otherwise the test skips; run "q2nav build
// -all" or the tests without -race first).

func navDir() string { return nav.DefaultDir() }

func openDemo(t *testing.T) *pak.Pak {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func loadMap(t *testing.T, p *pak.Pak, name string, skill int) (*mapdata.Map, []byte) {
	t.Helper()
	raw, err := p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: skill})
	if err != nil {
		t.Fatal(err)
	}
	return md, raw
}

// demoGraph returns the skill-resolved graph of a demo map from the shared
// cache, building it when allowed.
func demoGraph(t *testing.T, p *pak.Pak, name string, skill int) (*nav.Graph, *mapdata.Map) {
	t.Helper()
	md, _ := loadMap(t, p, name, skill)
	params := nav.DefaultParams()
	dir := navDir()
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		g, err := nav.ReadFile(filepath.Join(dir, nav.CacheName(name, md.Checksum, params)))
		if err != nil || g.Matches(md, params) != nil {
			t.Skipf("%s: no cached nav graph in %s and building one under -race is slow: run q2nav build -all (or set Q2_AGENT_LONG=1)", name, dir)
		}
	}
	s := nav.NewStore(dir, navbuild.StoreBuilder(p.ReadFile, navbuild.Config{}))
	s.Logf = t.Logf
	g, err := s.Load(context.Background(), md, params)
	if err != nil {
		t.Fatal(err)
	}
	return g, md
}

func synthetic(t *testing.T) []byte { return bsp.Encode(bsp.SyntheticFloorMap()) }

func encode(t *testing.T, g *nav.Graph) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := g.Encode(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestBuildSynthetic(t *testing.T) {
	raw := synthetic(t)
	g1, rep, err := navbuild.Build(context.Background(), "synthetic", raw, navbuild.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	g3, _, err := navbuild.Build(context.Background(), "synthetic", raw, navbuild.Config{Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encode(t, g1), encode(t, g3)) {
		t.Fatal("1 and 3 workers built different graphs")
	}
	t.Logf("%s", rep)
	// the 128x128 slab: a 5x5 grid of standing spots (the spawn among
	// them), connected by straight walks
	if len(g1.Nodes) < 9 || len(g1.Nodes) > 30 || len(g1.Spawns) != 1 || g1.Spawns[0].Node == nav.NoNode {
		t.Fatalf("%d nodes, spawns %+v", len(g1.Nodes), g1.Spawns)
	}
	if sp := g1.Nodes[g1.Spawns[0].Node]; sp.Origin != (nav.Vec3{0, 0, 24.125}) || sp.Flags&nav.NodeSpawn == 0 {
		t.Errorf("spawn node %+v", sp)
	}
	for i, n := range g1.Nodes {
		o := n.Origin
		if o[2] != 24.125 || o[0] < -64 || o[0] > 64 || o[1] < -64 || o[1] > 64 {
			t.Errorf("node %d at %v is not on the slab", i, o)
		}
	}
	walks := 0
	for _, e := range g1.Edges {
		if e.Kind == nav.EdgeWalk {
			walks++
		}
		if len(e.Reqs) > 0 {
			t.Errorf("conditional edge on an empty map: %+v", e)
		}
	}
	if walks < 2*len(g1.Nodes) {
		t.Errorf("only %d walk edges for %d nodes", walks, len(g1.Nodes))
	}
	if seen := g1.Reachable([]nav.NodeID{g1.Spawns[0].Node}, nil); countTrue(seen) != len(g1.Nodes) {
		t.Error("pruning left unreachable nodes")
	}
	md, err := mapdata.Load("synthetic", raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := g1.Matches(md, nav.DefaultParams()); err != nil {
		t.Error(err)
	}
	// every edge re-simulates
	v := navbuild.NewVerifier(g1, md.CM)
	for i := range g1.Edges {
		if r := v.Edge(i); !r.OK && !r.Skipped {
			t.Errorf("edge %d: %s", i, r.Reason)
		}
	}
}

func countTrue(b []bool) int {
	n := 0
	for _, x := range b {
		if x {
			n++
		}
	}
	return n
}

func TestBuildCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := navbuild.Build(ctx, "synthetic", synthetic(t), navbuild.Config{}); err == nil {
		t.Fatal("a cancelled build succeeded")
	}
	if _, _, err := navbuild.Build(context.Background(), "x", []byte("not a bsp"), navbuild.Config{}); err == nil {
		t.Fatal("garbage BSP accepted")
	}
	bad := nav.DefaultParams()
	bad.StepMsec = 0
	if _, _, err := navbuild.Build(context.Background(), "synthetic", synthetic(t), navbuild.Config{Params: bad}); err == nil {
		t.Fatal("bad params accepted")
	}
}

// TestBuildDemo1Clipped builds the area around the demo1 start with 1 and 4
// workers (fast enough for the race detector) and checks the output is the
// same and every edge re-simulates.
func TestBuildDemo1Clipped(t *testing.T) {
	p := openDemo(t)
	md, raw := loadMap(t, p, "demo1", 1)
	clip := &[2]nav.Vec3{{-256, -704, -128}, {512, 64, 128}}
	if raceEnabled {
		clip = &[2]nav.Vec3{{-64, -576, -128}, {384, -128, 128}} // the race detector is ~15x slower
	}
	g1, rep, err := navbuild.Build(context.Background(), "demo1", raw, navbuild.Config{Workers: 1, Clip: clip})
	if err != nil {
		t.Fatal(err)
	}
	g4, _, err := navbuild.Build(context.Background(), "demo1", raw, navbuild.Config{Workers: 4, Clip: clip})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", rep)
	if !bytes.Equal(encode(t, g1), encode(t, g4)) {
		t.Fatal("1 and 4 workers built different graphs")
	}
	if len(g1.Nodes) < 40 {
		t.Fatalf("only %d nodes around the start", len(g1.Nodes))
	}
	g := g1.ForSkill(1)
	v := navbuild.NewVerifier(g, md.CM)
	for i := range g.Edges {
		if r := v.Edge(i); !r.OK && !r.Skipped {
			t.Errorf("edge %d: %s", i, r.Reason)
		}
	}
}

// TestBuildDeterministic builds demo1 twice (4 and 2 workers) and requires
// byte-identical files.
func TestBuildDeterministic(t *testing.T) {
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("two full builds under -race take about a minute; set Q2_AGENT_LONG=1 (TestBuildDemo1Clipped covers the workers)")
	}
	if testing.Short() {
		t.Skip("short")
	}
	p := openDemo(t)
	_, raw := loadMap(t, p, "demo1", 1)
	a, rep, err := navbuild.Build(context.Background(), "demo1", raw, navbuild.Config{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", rep)
	b, _, err := navbuild.Build(context.Background(), "demo1", raw, navbuild.Config{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	ea, eb := encode(t, a), encode(t, b)
	if !bytes.Equal(ea, eb) {
		t.Fatalf("two builds differ (%d vs %d bytes)", len(ea), len(eb))
	}
	t.Logf("%d bytes gzipped", len(ea))
}

func routes(t *testing.T) *route.Campaign {
	t.Helper()
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	c, err := route.Load(filepath.Join(root, "fixtures", "agent", "routes"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestRouteCoverage: from each visit's arrival spawn, every entity a route
// step refers to (doors, the car, buttons, triggers, the key, the monsters
// to kill) is reachable over edges whose conditions hold after the earlier
// steps' effects.
func TestRouteCoverage(t *testing.T) {
	p := openDemo(t)
	c := routes(t)
	type loaded struct {
		g  *nav.Graph
		md *mapdata.Map
	}
	graphs := map[string]loaded{}
	for _, tb := range c.Tables {
		l, ok := graphs[tb.Map]
		if !ok {
			l.g, l.md = demoGraph(t, p, tb.Map, c.Skill)
			graphs[tb.Map] = l
		}
		probs, err := navbuild.CheckRoute(l.g, l.md, tb)
		if err != nil {
			t.Fatal(err)
		}
		for _, pr := range probs {
			t.Errorf("not reachable: %v", pr)
		}
		t.Logf("%s: %d steps covered", tb.Name, len(tb.Steps)-len(probs))
	}
}

// TestRouteCoverageNeedsConditions: the checker is not trivially satisfied.
// Without the gunner's death switching the lasers off, demo3's route is
// blocked at y=-400; a goto far outside the level fails anywhere.
func TestRouteCoverageNeedsConditions(t *testing.T) {
	p := openDemo(t)
	c := routes(t)
	var demo3 *route.Table
	for _, tb := range c.Tables {
		if tb.Name == "demo3" {
			cp := *tb
			cp.Steps = append([]route.Step(nil), tb.Steps...)
			for i := range cp.Steps {
				if cp.Steps[i].Op == route.OpKill || cp.Steps[i].Op == route.OpConfirm {
					cp.Steps[i].Effects = nil
				}
			}
			demo3 = &cp
		}
	}
	g, md := demoGraph(t, p, "demo3", c.Skill)
	probs, err := navbuild.CheckRoute(g, md, demo3)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) == 0 {
		t.Fatal("route still covered with the lasers on")
	}
	for _, pr := range probs {
		t.Logf("expected: %v", pr)
	}
	far := route.Vec{5000, 5000, 0}
	bad := &route.Table{Name: "far", Map: "demo3", From: "base2a", Steps: []route.Step{{Op: route.OpGoto, Pos: &far}}}
	if probs, err := navbuild.CheckRoute(g, md, bad); err != nil || len(probs) != 1 {
		t.Fatalf("goto outside the level: %v %v", probs, err)
	}
}

// TestPropertyResim re-simulates a sample of demo1 edges of every kind
// (fast-path edges included) with the navsim executors.
func TestPropertyResim(t *testing.T) {
	p := openDemo(t)
	g, md := demoGraph(t, p, "demo1", 1)
	v := navbuild.NewVerifier(g, md.CM)
	byKind := map[nav.EdgeKind][]int{}
	for i := range g.Edges {
		e := &g.Edges[i]
		k := e.Kind
		if e.Flags&nav.EdgeFast != 0 {
			k += 100 // its own stratum
		}
		byKind[k] = append(byKind[k], i)
	}
	n, per := 0, 60
	if raceEnabled {
		per = 15
	}
	for k, list := range byKind {
		step := len(list)/per + 1
		for j := (int(k) * 7) % step; j < len(list); j += step {
			r := v.Edge(list[j])
			if r.Skipped {
				continue
			}
			n++
			if !r.OK {
				t.Errorf("edge %d: %s", list[j], r.Reason)
			}
		}
	}
	t.Logf("%d edges re-simulated", n)
}

// TestDemo1Graph checks the shape of the demo1 graph: spawn nodes, the car
// *31 with nodes at both poses joined by rides, conditional edges through
// the auto doors, button and trigger effects.
func TestDemo1Graph(t *testing.T) {
	p := openDemo(t)
	g, md := demoGraph(t, p, "demo1", 1)
	s := g.Stats()
	t.Logf("%+v", s)
	for _, sp := range g.Spawns {
		if sp.Node == nav.NoNode {
			t.Errorf("spawn #%d has no node", sp.Entity)
		}
	}
	car := g.BlockerOf(582)
	if car < 0 || len(g.MoverNodes(car, 0)) == 0 || len(g.MoverNodes(car, 1)) == 0 {
		t.Fatalf("car *31: blocker %d", car)
	}
	rides := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind == nav.EdgeRide && g.Nodes[e.From].Blocker == car {
			rides++
			if len(e.Reqs) != 1 || e.Reqs[0].Blocker != car || e.Cost < 1.5 || e.Cost > 2 {
				t.Errorf("car ride %+v", e)
			}
			if g.Nodes[e.From].Pose == 0 && !e.HasEffect(419) {
				t.Errorf("the ride down does not enter the exit trigger *27: %+v", e.Effects)
			}
		}
	}
	if rides == 0 {
		t.Error("no rides on the car")
	}
	door := g.BlockerOf(589) // *32, the auto door to the elevator
	through := 0
	for i := range g.Edges {
		for _, r := range g.Edges[i].Reqs {
			if r.Blocker == door && r.States == nav.Pose(1) {
				through++
			}
		}
	}
	if door < 0 || through == 0 {
		t.Errorf("no edge needs the door *32 open (%d)", through)
	}
	if len(g.EffectEdges(591)) == 0 {
		t.Error("nothing presses the car button *34")
	}
	if b := g.Blockers[g.BlockerOf(590)]; b.Team != 589 || g.Blockers[door].Team != 589 {
		t.Errorf("door *33 is a slave of *32: team %d / %d", b.Team, g.Blockers[door].Team)
	}
	_ = md
}

// TestNoTraps: every node a spawn reaches has a way out (an end node is
// left by the edges of the node it was reached from). A node without one
// is a trap a follower could fall into: a hull wedged where pmove cannot
// move it, or a pit a func_water fills at every pose.
func TestNoTraps(t *testing.T) {
	p := openDemo(t)
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		g, _ := demoGraph(t, p, m, 1)
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if len(g.Out(nav.NodeID(i))) == 0 && n.Flags&nav.NodeEnd == 0 {
				t.Errorf("%s: node %d %v (%v) has no way out", m, i, n.Origin, n.Flags)
			}
		}
	}
}

// TestChainedEntries replays edges the way a pursuing follower runs them:
// a random walk edge into the start node from rest, then the edge right
// away. Edges without EdgeFromRest must arrive (the jump, drop and ladder
// executors stop at their start first); EdgeFromRest edges after
// navsim.StopAt at their start, unless they are EdgeFragile.
func TestChainedEntries(t *testing.T) {
	p := openDemo(t)
	g, md := demoGraph(t, p, "demo1", 1)
	v := navbuild.NewVerifier(g, md.CM)
	rng := rand.New(rand.NewSource(1))
	every := 3
	if raceEnabled {
		every = 20
	}
	type count struct{ pass, total int }
	var running, stopped count
	var fails []string
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Flags&(nav.EdgeFast|nav.EdgeFragile) != 0 || g.Nodes[e.From].Flags&(nav.NodeWater|nav.NodeLadder) != 0 {
			continue
		}
		switch e.Kind {
		case nav.EdgeWalk, nav.EdgeCrouch, nav.EdgeJump, nav.EdgeDrop, nav.EdgeLadder:
		default:
			continue
		}
		if i%every != 0 {
			continue
		}
		var in []int32
		for _, j := range g.In(e.From) {
			f := &g.Edges[j]
			if (f.Kind == nav.EdgeWalk || f.Kind == nav.EdgeCrouch) && f.From != e.To && !f.Conditional() &&
				g.Nodes[f.From].Flags&(nav.NodeWater|nav.NodeLadder) == 0 {
				in = append(in, j)
			}
		}
		if len(in) == 0 {
			continue
		}
		stop := e.Flags&nav.EdgeFromRest != 0
		r := v.Chain(int(in[rng.Intn(len(in))]), i, stop)
		if r.Skipped {
			continue
		}
		c := &running
		if stop {
			c = &stopped
		}
		c.total++
		if r.OK {
			c.pass++
		} else if len(fails) < 10 {
			fails = append(fails, fmt.Sprintf("edge %d %s (%v) stop %v: %s", i, e.Kind, e.Flags, stop, r.Reason))
		}
	}
	t.Logf("running entries %d/%d, after stopping %d/%d", running.pass, running.total, stopped.pass, stopped.total)
	for _, f := range fails {
		t.Log(f)
	}
	// the build replays one in-edge per incoming direction; another one
	// from the same direction rarely differs
	if running.total < 50 || float64(running.pass) < 0.99*float64(running.total) {
		t.Errorf("running entries: %d/%d arrive", running.pass, running.total)
	}
	if stopped.total > 0 && float64(stopped.pass) < 0.9*float64(stopped.total) {
		t.Errorf("from-rest edges after stopping: %d/%d arrive", stopped.pass, stopped.total)
	}
}

// TestFallDamageRecorded: the falling damage of the frame an edge lands in
// is part of the edge's FallDamage (demo3's drop into the pit below
// (320, 192) deals 27 when the landing frame is judged).
func TestFallDamageRecorded(t *testing.T) {
	p := openDemo(t)
	g, _ := demoGraph(t, p, "demo3", 1)
	from := g.Localize(nav.Vec3{320, 192, -600}, 2)
	to := g.Localize(nav.Vec3{320, 224, -808}, 2)
	i := g.EdgeIndex(from, to)
	if from == nav.NoNode || to == nav.NoNode || i < 0 {
		t.Fatalf("no drop (320,192,-600) -> (320,224,-808): nodes %d %d", from, to)
	}
	if e := &g.Edges[i]; e.FallDamage < 20 {
		t.Errorf("drop %+v: fall damage %d", e, e.FallDamage)
	}
	hurt := 0
	for i := range g.Edges {
		if e := &g.Edges[i]; e.Kind == nav.EdgeDrop && g.Nodes[e.From].Origin[2]-g.Nodes[e.To].Origin[2] >= 250 && e.FallDamage > 0 {
			hurt++
		}
	}
	if hurt == 0 {
		t.Error("no drop of 250 units or more hurts")
	}
}

// TestTouchEdgesAnyAllowedPose: a touch edge ends at To (at rest) with any
// nearby solid blocker in any pose its conditions allow, not just in the
// world it was built in (a spawn-world edge that leans on a door needs the
// door in its spawn state).
func TestTouchEdgesAnyAllowedPose(t *testing.T) {
	p := openDemo(t)
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		g, md := demoGraph(t, p, m, 1)
		v := navbuild.NewVerifier(g, md.CM)
		runs := 0
		for i := range g.Edges {
			e := &g.Edges[i]
			if e.Kind != nav.EdgeTouch {
				continue
			}
			base := g.EdgePoses(e)
			a, b := g.Nodes[e.From].Origin, g.Nodes[e.To].Origin
			var lo, hi nav.Vec3
			for k := 0; k < 3; k++ {
				lo[k] = min(a[k], b[k], e.Aim[k]) - 96
				hi[k] = max(a[k], b[k], e.Aim[k]) + 96
			}
			for bi := range g.Blockers {
				bl := &g.Blockers[bi]
				if !bl.Solid {
					continue
				}
				for k := range bl.Poses {
					mn, mx := navsim.LinkBox(true, bl.Poses[k].Origin, bl.Poses[k].Angles, bl.Mins, bl.Maxs)
					if k == base[bi] || !nav.Holds(&nav.Edge{Reqs: e.Reqs}, poseStates(g, bi, k)) ||
						mn[0] > hi[0] || mn[1] > hi[1] || mn[2] > hi[2] || mx[0] < lo[0] || mx[1] < lo[1] || mx[2] < lo[2] {
						continue
					}
					poses := append([]int(nil), base...)
					poses[bi] = k
					g.SetWorld(v.World(), poses)
					g.Place(v.Runner(), e.From)
					runs++
					if r := v.EdgeIn(i, v.Runner().State(), poses); !r.OK {
						t.Errorf("%s edge %d with %s %s at %s: %s", m, i, bl.Class, bl.Model, bl.Poses[k].Name, r.Reason)
					}
				}
			}
		}
		t.Logf("%s: %d runs", m, runs)
	}
}

// poseStates is "every blocker in any state, blocker b at pose k".
func poseStates(g *nav.Graph, b, k int) []nav.StateMask {
	s := make([]nav.StateMask, len(g.Blockers))
	for i := range s {
		s[i] = g.Blockers[i].All()
	}
	s[b] = nav.Pose(k)
	return s
}

// TestDemoScenes checks scene details on the demo graphs: demo2's
// TRIGGERED trigger *58 starts disabled, rides that need a use carry the
// mover.
func TestDemoScenes(t *testing.T) {
	p := openDemo(t)
	g, _ := demoGraph(t, p, "demo2", 1)
	found := false
	for _, e := range g.Ents {
		if e.Model == "*58" {
			found = e.Disabled
		}
	}
	if !found {
		t.Error("demo2 *58 (TRIGGERED) is not disabled")
	}
	needs := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Flags&nav.EdgeNeedsUse != 0 {
			needs++
			if e.Kind != nav.EdgeRide || e.Target != g.Blockers[g.Nodes[e.From].Blocker].Entity {
				t.Errorf("needs-use edge %+v", e)
			}
		}
	}
	if needs == 0 {
		t.Error("no ride on demo2's used movers needs a use")
	}
}
