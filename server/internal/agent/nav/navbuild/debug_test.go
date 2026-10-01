package navbuild

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/testutil"
)

func debugBuilder(t *testing.T, name string) *builder {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	f, _ := bsp.Parse(raw)
	var maps [4]*mapdata.Map
	for s := range maps {
		maps[s], _ = mapdata.Load(name, raw, mapdata.Options{Skill: s})
	}
	pr := nav.DefaultParams()
	b := &builder{ctx: context.Background(), p: pr, phys: pr.Physics(), name: name, geo: &fileGeo{f: f, cm: maps[1].CM}, rep: &Report{}}
	b.sc, err = newScene(maps, b.geo)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		b.workers = append(b.workers, b.newWorker())
	}
	for _, fn := range []func() (int, error){b.buildNodes, b.buildTouchEnds, b.findLedges, b.buildEdges} {
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
	}
	return b
}

func TestDebugReach(t *testing.T) {
	if os.Getenv("NAVDEBUG") == "" {
		t.Skip("NAVDEBUG")
	}
	b := debugBuilder(t, os.Getenv("NAVDEBUG"))
	keep := b.reachable()
	// components over undirected edges
	comp := make([]int, len(b.nodes))
	for i := range comp {
		comp[i] = -1
	}
	adj := make([][]int32, len(b.nodes))
	for _, e := range b.edges {
		adj[e.From] = append(adj[e.From], int32(e.To))
		adj[e.To] = append(adj[e.To], int32(e.From))
	}
	nc := 0
	sizes := map[int]int{}
	for i := range b.nodes {
		if comp[i] >= 0 {
			continue
		}
		st := []int32{int32(i)}
		comp[i] = nc
		for len(st) > 0 {
			n := st[len(st)-1]
			st = st[:len(st)-1]
			sizes[nc]++
			for _, m := range adj[n] {
				if comp[m] < 0 {
					comp[m] = nc
					st = append(st, m)
				}
			}
		}
		nc++
	}
	type cs struct{ c, n int }
	var list []cs
	for c, n := range sizes {
		list = append(list, cs{c, n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	for _, x := range list[:min(15, len(list))] {
		// sample node
		var ex []string
		reach := 0
		for i := range b.nodes {
			if comp[i] == x.c {
				if keep[i] {
					reach++
				}
				if len(ex) < 3 {
					ex = append(ex, fmt.Sprintf("%v f=%v", b.nodes[i].o, b.nodes[i].flags))
				}
			}
		}
		t.Logf("component %d: %d nodes (%d reachable) e.g. %v", x.c, x.n, reach, ex)
	}
	for i := range b.nodes {
		if b.nodes[i].prio == prioSpawn {
			t.Logf("spawn node %d %v comp %d out %d", i, b.nodes[i].o, comp[i], len(adj[i]))
		}
	}
}

func TestDebugComp(t *testing.T) {
	if os.Getenv("NAVDEBUG") == "" {
		t.Skip("NAVDEBUG")
	}
	b := debugBuilder(t, os.Getenv("NAVDEBUG"))
	keep := b.reachable()
	cs := b.workers[0].w.State()
	m := cs.Map()
	clusters := map[string]int{}
	var mn, mx Vec3
	first := true
	zh := map[int]int{}
	for i := range b.nodes {
		if keep[i] {
			continue
		}
		o := b.nodes[i].o
		leaf := cs.PointLeafnum(o)
		cl := m.LeafCluster(int(leaf))
		key := "in"
		if cl < 0 {
			key = "void"
		}
		clusters[key]++
		if key == "void" {
			continue
		}
		zh[int(o[2]/64)]++
		for k := 0; k < 3; k++ {
			if first || o[k] < mn[k] {
				mn[k] = o[k]
			}
			if first || o[k] > mx[k] {
				mx[k] = o[k]
			}
		}
		first = false
	}
	t.Logf("unreachable by cluster: %v bbox %v %v zhist %v", clusters, mn, mx, zh)
	n := 0
	for i := range b.nodes {
		if !keep[i] && n < 40 {
			o := b.nodes[i].o
			if m.LeafCluster(int(cs.PointLeafnum(o))) >= 0 {
				if i%97 == 0 {
					t.Logf("unreachable in-world node %d %v flags %v", i, o, b.nodes[i].flags)
					n++
				}
			}
		}
	}
}

func TestDebugGap(t *testing.T) {
	if os.Getenv("NAVDEBUG") == "" {
		t.Skip("NAVDEBUG")
	}
	b := debugBuilder(t, os.Getenv("NAVDEBUG"))
	keep := b.reachable()
	cs := b.workers[0].w.State()
	m := cs.Map()
	type pr struct {
		u, r int32
		d    float32
	}
	var prs []pr
	for i := range b.nodes {
		if keep[i] || m.LeafCluster(int(cs.PointLeafnum(b.nodes[i].o))) < 0 {
			continue
		}
		best := pr{int32(i), -1, 1e9}
		b.near(b.nodes[i].o, 200, func(j int32) {
			if keep[j] {
				if d := dist3(b.nodes[i].o, b.nodes[j].o); d < best.d {
					best = pr{int32(i), j, d}
				}
			}
		})
		if best.r >= 0 {
			prs = append(prs, best)
		}
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].d < prs[j].d })
	seen := 0
	wk := b.workers[0]
	for _, p := range prs {
		if seen > 25 {
			break
		}
		if p.u%7 != 0 {
			continue
		}
		seen++
		u, r := &b.nodes[p.u], &b.nodes[p.r]
		ok1 := wk.simulate(r, u, navsim.Plan{Recipe: navsim.RecipeWalk, From: r.o, Target: u.o})
		end1 := wk.r.StatePtr().Origin()
		ok2 := wk.simulate(u, r, navsim.Plan{Recipe: navsim.RecipeWalk, From: u.o, Target: r.o})
		end2 := wk.r.StatePtr().Origin()
		cands := wk.candidates(p.r)
		has := false
		for _, c := range cands {
			if c.to == p.u {
				has = true
			}
		}
		t.Logf("u %v (%v) r %v (%v) d=%.1f cand=%v walk r->u %v end %v; u->r %v end %v", u.o, u.flags, r.o, r.flags, p.d, has, ok1, end1, ok2, end2)
	}
}

func TestDebugRoutes(t *testing.T) {
	if os.Getenv("NAVROUTES") == "" {
		t.Skip("NAVROUTES")
	}
	root, _ := testutil.RepoRoot()
	c, err := route.Load(root + "/fixtures/agent/routes")
	if err != nil {
		t.Fatal(err)
	}
	p, _ := pak.Open(testutil.DemoPak(t))
	defer p.Close()
	graphs := map[string]*nav.Graph{}
	for _, tb := range c.Tables {
		raw, _ := p.ReadFile("maps/" + tb.Map + ".bsp")
		md, _ := mapdata.Load(tb.Map, raw, mapdata.Options{Skill: c.Skill})
		g := graphs[tb.Map]
		if g == nil {
			g, _, err = Build(context.Background(), tb.Map, raw, Config{})
			if err != nil {
				t.Fatal(err)
			}
			graphs[tb.Map] = g
			t.Logf("%s: %+v", tb.Map, g.Stats())
		}
		probs, err := CheckRoute(g.ForSkill(c.Skill), md, tb)
		if err != nil {
			t.Fatal(err)
		}
		for _, pr := range probs {
			t.Logf("PROBLEM %v", pr)
		}
		t.Logf("%s: %d problems", tb.Name, len(probs))
	}
}

func TestDebugTrigger(t *testing.T) {
	if os.Getenv("NAVTRIG") == "" {
		t.Skip("NAVTRIG")
	}
	b := debugBuilder(t, "demo3")
	keep := b.reachable()
	mn, mx := Vec3{1558, 378, -490}, Vec3{1650, 390, -358}
	c := Vec3{(mn[0] + mx[0]) / 2, (mn[1] + mx[1]) / 2, mn[2]}
	b.near(c, 200, func(i int32) {
		n := &b.nodes[i]
		if n.o[2] < -560 || n.o[2] > -300 {
			return
		}
		t.Logf("node %d %v flags %v reach %v blocker %d", i, n.o, n.flags, keep[i], n.blocker)
	})
	for vi, v := range b.sc.vols {
		if v.entity == 298 {
			t.Logf("vol %d %+v", vi, v)
		}
	}
	for _, e := range b.touchEdges {
		for _, f := range e.Effects {
			if f.Entity == 298 {
				t.Logf("touch edge %+v", e)
			}
		}
	}
	for _, e := range b.edges {
		for _, f := range e.Effects {
			if f.Entity == 298 {
				t.Logf("edge %d->%d kind %v reach %v reqs %v", e.From, e.To, e.Kind, keep[e.From], e.Reqs)
			}
		}
	}
	for bi, bl := range b.sc.blockers {
		g := b.sc.geo[bi]
		if boxesOverlap(g.umin, g.umax, Vec3{mn[0] - 100, mn[1] - 100, mn[2]}, Vec3{mx[0] + 100, mx[1] + 100, mx[2]}) {
			t.Logf("blocker %d %v %s %v poses %v", bi, bl.Entity, bl.Model, bl.Kind, bl.Poses)
		}
	}
}

func TestDebugTouch12(t *testing.T) {
	if os.Getenv("NAVTRIG") == "" {
		t.Skip("NAVTRIG")
	}
	b := debugBuilder(t, "demo3")
	keep := b.reachable()
	for _, tg := range b.touchables() {
		if tg.entity != 298 {
			continue
		}
		t.Logf("target %+v", tg)
		center := Vec3{(tg.min[0] + tg.max[0]) / 2, (tg.min[1] + tg.max[1]) / 2, (tg.min[2] + tg.max[2]) / 2}
		b.near(center, 200, func(i int32) {
			n := &b.nodes[i]
			if n.o[2]-24-48 > tg.max[2] || n.o[2]+32+48 < tg.min[2] {
				return
			}
			wk := b.workers[0]
			p := wk.touchSim(&tg, i)
			t.Logf("src %d %v reach %v d=%.0f -> ok %v end %v edge %+v final %v", i, n.o, keep[i], boxDistXY(n.o, tg.min, tg.max), p.ok, p.end, p.edge.Reqs, wk.r.StatePtr().Origin())
		})
	}
}

func TestDebugResimAll(t *testing.T) {
	name := os.Getenv("NAVRESIM")
	if name == "" {
		t.Skip("NAVRESIM")
	}
	p, _ := pak.Open(testutil.DemoPak(t))
	defer p.Close()
	raw, _ := p.ReadFile("maps/" + name + ".bsp")
	g, _, err := Build(context.Background(), name, raw, Config{})
	if err != nil {
		t.Fatal(err)
	}
	md, _ := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	v := NewVerifier(g, md.CM)
	fails := map[string]int{}
	total := map[string]int{}
	shown := 0
	for i := range g.Edges {
		e := &g.Edges[i]
		k := e.Kind.String()
		if e.Flags&nav.EdgeFast != 0 {
			k += "/fast"
		}
		r := v.Edge(i)
		if r.Skipped {
			continue
		}
		total[k]++
		if !r.OK {
			fails[k]++
			if shown < 15 {
				shown++
				t.Logf("FAIL %d %s flags %v: %s from %v", i, k, e.Flags, r.Reason, g.Nodes[e.From].Origin)
			}
		}
	}
	t.Logf("totals %v fails %v", total, fails)
}

func TestDebugTraj(t *testing.T) {
	if os.Getenv("NAVTRAJ") == "" {
		t.Skip("NAVTRAJ")
	}
	mp := os.Getenv("NAVMAP")
	if mp == "" {
		mp = "demo1"
	}
	b := debugBuilder(t, mp)
	wk := b.workers[0]
	var a, m *bnode
	var av, mv Vec3
	fmt.Sscanf(os.Getenv("NAVTRAJ"), "%f,%f,%f,%f,%f,%f", &av[0], &av[1], &av[2], &mv[0], &mv[1], &mv[2])
	for i := range b.nodes {
		if dist3(b.nodes[i].o, av) < 0.5 {
			a = &b.nodes[i]
		}
		if dist3(b.nodes[i].o, mv) < 0.5 {
			m = &b.nodes[i]
		}
	}
	if a == nil || m == nil {
		t.Fatal("nodes not found")
	}
	wk.setWorld()
	s, step, ok := wk.fastPath(a, m)
	t.Logf("fast %v step %v samples %d", ok, step, len(s))
	ok2 := wk.simulate(a, m, navsim.Plan{Recipe: navsim.RecipeWalk, From: a.o, Target: m.o})
	t.Logf("sim %v", ok2)
	for i, sm := range wk.out.Samples {
		if i < 30 {
			t.Logf("  %d %v ground %v cmd %+v", i, sm.Origin, sm.OnGround, func() any { if i > 0 { return wk.out.Cmds[i-1] }; return nil }())
		}
	}
	for k := 0; k <= 10; k++ {
		f := float32(k) / 10
		p := Vec3{a.o[0] + (m.o[0]-a.o[0])*f, a.o[1] + (m.o[1]-a.o[1])*f, a.o[2] + 30}
		tr := wk.w.Trace(p, navsim.StandMins(), navsim.StandMaxs(), Vec3{p[0], p[1], p[2] - 100}, 33619971)
		c := wk.w.Trace(Vec3{p[0], p[1], p[2] - 24}, Vec3{}, Vec3{}, Vec3{p[0], p[1], p[2] - 124}, 33619971)
		t.Logf("floor at %v: hull z=%v n=%v | point z=%v n=%v", p, tr.EndPos[2], tr.Plane.Normal, c.EndPos[2]+24, c.Plane.Normal)
	}
}
