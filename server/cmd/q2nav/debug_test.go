package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

func TestDebugLiveEdge(t *testing.T) {
	spec := os.Getenv("Q2NAV_EDGE") // map:edge
	if spec == "" {
		t.Skip("Q2NAV_EDGE")
	}
	parts := strings.Split(spec, ":")
	ei, _ := strconv.Atoi(parts[1])
	src, err := openPak(testutil.DemoPak(t), mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	g, md, err := loadGraph(src, parts[0], navDir(""))
	if err != nil {
		t.Fatal(err)
	}
	e := &g.Edges[ei]
	t.Logf("edge %+v", *e)
	ctx := context.Background()
	ls, err := startLive(ctx, src.p, g.Map, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer ls.Close()
	v := navbuild.NewVerifier(g, md.CM)
	start, _ := ls.place(ctx, g.Node(e.From))
	poses := g.SpawnPoses()
	g.SetWorld(v.World(), poses)
	st := stateOf(start, v.World())
	sim := v.EdgeIn(ei, st, poses)
	for k := len(sim.Cmds); k%4 != 0; k++ {
		v.Runner().Step(nav.HoldCmd(g.Node(e.To)))
	}
	t.Logf("sim padded end %v", v.Runner().State().Origin())
	t.Logf("sim ok %v cmds %d end %v", sim.OK, len(sim.Cmds), sim.End)
	frames, _ := ls.run(ctx, sim.Cmds, nav.HoldCmd(g.Node(e.To)))
	for _, f := range frames {
		t.Logf("server frame origin %v", shortVec(f.Origin))
	}
	// solid entities near the path on the server
	a, b := g.Nodes[e.From].Origin, g.Nodes[e.To].Origin
	eds := ls.g.Edicts()
	for i := range eds {
		x := &eds[i]
		if !x.InUse || x.Solid == q2const.SOLID_NOT || x.Solid == q2const.SOLID_TRIGGER || i == 1 {
			continue
		}
		c := x.AbsMin
		near := true
		for k := 0; k < 2; k++ {
			lo, hi := min(a[k], b[k])-64, max(a[k], b[k])+64
			if x.AbsMax[k] < lo || x.AbsMin[k] > hi {
				near = false
			}
		}
		if near && (x.AbsMax[2] > min(a[2], b[2])-40 && x.AbsMin[2] < max(a[2], b[2])+40) {
			inWorld := false
			for _, s := range g.Solids {
				if s.ID == i {
					inWorld = true
				}
			}
			t.Logf("server solid #%d %s solid %d abs %v-%v origin %v (in graph statics %v, blocker %d) c=%v", i, x.Classname, x.Solid, x.AbsMin, x.AbsMax, x.S.Origin, inWorld, g.BlockerOf(i), c)
		}
	}
	_ = fmt.Sprint
	var _ game.Edict
}
