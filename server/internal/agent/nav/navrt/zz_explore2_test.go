package navrt

import (
	"context"
	"fmt"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func TestZZExplore2(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo1", "demo2", "demo3"} {
		raw, _ := p.ReadFile("maps/" + name + ".bsp")
		md, _ := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
		s := nav.NewStore(nav.DefaultDir(), navbuild.StoreBuilder(p.ReadFile, navbuild.Config{}))
		g, _ := s.Load(context.Background(), md, nav.DefaultParams())
		var start nav.NodeID = -1
		for _, sp := range g.Spawns {
			if sp.Targetname == "" {
				start = sp.Node
			}
		}
		rs := navbuild.RouteStates(g, md)
		reach := g.Reachable([]nav.NodeID{start}, func(e *nav.Edge) bool { return nav.Holds(e, rs) })
		n := 0
		for _, r := range reach {
			if r {
				n++
			}
		}
		fmt.Printf("== %s start node %d %v reachable(routestates) %d of %d\n", name, start, g.Nodes[start].Origin, n, len(g.Nodes))
		for _, mv := range md.Movers {
			if mv.Kind == mapdata.MoverButton {
				ee := g.EffectEdges(mv.Entity)
				kinds := map[string]int{}
				for _, i := range ee {
					kinds[g.Edges[i].Kind.String()]++
				}
				fmt.Printf("  button %d %s act %v wait %v target %s effect edges %v reach:", mv.Entity, mv.Model, mv.Activation, mv.Wait, mv.Target, kinds)
				for _, r := range md.Reach(mv.Entity) {
					e := md.Entity(r.Entity)
					fmt.Printf(" %s#%d(%v)", e.Classname, r.Entity, r.Response)
				}
				fmt.Println()
			}
		}
		for _, tr := range md.Triggers {
			if !tr.HasVolume {
				continue
			}
			exit := false
			for _, r := range md.Reach(tr.Entity) {
				if r.Response == mapdata.RespExit {
					exit = true
				}
			}
			if exit {
				ee := g.EffectEdges(tr.Entity)
				kinds := map[string]int{}
				for _, i := range ee {
					kinds[g.Edges[i].Kind.String()]++
				}
				fmt.Printf("  exit trigger %d %s edges %v\n", tr.Entity, tr.Model, kinds)
			}
		}
		// edges with effects summary
		effk := map[string]int{}
		for i := range g.Edges {
			for _, f := range g.Edges[i].Effects {
				effk[f.Kind.String()+"/"+g.Edges[i].Kind.String()]++
			}
		}
		fmt.Printf("  effect kinds %v\n", effk)
		// ride edges
		for i := range g.Edges {
			e := &g.Edges[i]
			if e.Kind == nav.EdgeRide && i%3 == 0 {
				fn, tn := g.Nodes[e.From], g.Nodes[e.To]
				fmt.Printf("  ride %d: %d(b%d p%d) -> %d(b%d p%d) cost %.2f flags %v reqs %v eff %v\n", i, e.From, fn.Blocker, fn.Pose, e.To, tn.Blocker, tn.Pose, e.Cost, e.Flags, e.Reqs, e.Effects)
			}
		}
	}
}
