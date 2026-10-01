package navbuild_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
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
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if leaf := md.CM; leaf == nil {
			break
		}
		if len(g.Out(nav.NodeID(i))) == 0 && n.Flags&nav.NodeEnd == 0 {
			t.Errorf("node %d %v has no way out", i, n.Origin)
			break
		}
	}
}
