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

func TestZZExplore(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"demo1", "demo2", "demo3"} {
		raw, _ := p.ReadFile("maps/" + name + ".bsp")
		md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
		if err != nil {
			t.Fatal(err)
		}
		s := nav.NewStore(nav.DefaultDir(), navbuild.StoreBuilder(p.ReadFile, navbuild.Config{}))
		s.Logf = t.Logf
		g, err := s.Load(context.Background(), md, nav.DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		st := g.Stats()
		fmt.Printf("== %s nodes %d edges %d cond %d bykind %v spawns %v\n", name, st.Nodes, st.Edges, st.Conditional, st.ByKind, g.Spawns)
		flags := map[string]int{}
		for i := range g.Edges {
			e := &g.Edges[i]
			for b := 0; b < 9; b++ {
				if e.Flags&(1<<b) != 0 {
					flags[nav.EdgeFlags(1<<b).String()]++
				}
			}
		}
		fmt.Printf("flags %v\n", flags)
		for i, b := range g.Blockers {
			mv := md.Mover(int(b.Entity))
			act := ""
			trig := ""
			if mv != nil {
				act = fmt.Sprint(mv.Activation, " wait ", mv.Wait, " travel ", mv.TravelTime, " speed ", mv.Speed)
				if mv.Trigger != nil {
					trig = fmt.Sprint(mv.Trigger.Min, mv.Trigger.Max)
				}
			}
			names := []string{}
			for _, ps := range b.Poses {
				names = append(names, ps.Name)
			}
			fmt.Printf("  blocker %d ent %d %s %s %s poses %v spawn %d gone %v team %d act %s trig %s\n", i, b.Entity, b.Class, b.Model, b.Kind, names, b.Spawn, b.Gone, b.Team, act, trig)
		}
		for _, ex := range md.Exits {
			fmt.Printf("  exit ent %d tn %s -> %s\n", ex.Entity, ex.Targetname, ex.Level.Raw)
		}
		for _, tr := range md.Triggers {
			fmt.Printf("  trigger %d %s %s dir %v movedir %v tn %q t %q box %v %v\n", tr.Entity, tr.Classname, tr.Model, tr.Directional(), tr.Movedir, tr.Targetname, tr.Target, tr.Box.Min, tr.Box.Max)
		}
		reach := g.Reachable([]nav.NodeID{g.Spawns[0].Node}, func(e *nav.Edge) bool { return nav.Holds(e, g.SpawnStates()) })
		n := 0
		for _, r := range reach {
			if r {
				n++
			}
		}
		fmt.Printf("  reachable from spawn under spawn states: %d\n", n)
	}
}
