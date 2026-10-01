package navbuild

import (
	"context"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/testutil"
)

// builds demo1 up to the entry stage and reports failing chains of a kind
func TestZZProbeChains(t *testing.T) {
	p, _ := pak.Open(testutil.DemoPak(t))
	defer p.Close()
	name := "demo1"
	raw, _ := p.ReadFile("maps/" + name + ".bsp")
	f, _ := bsp.Parse(raw)
	var maps [4]*mapdata.Map
	for s := range maps {
		maps[s], _ = mapdata.Load(name, raw, mapdata.Options{Skill: s})
	}
	prm := nav.DefaultParams()
	b := &builder{ctx: context.Background(), p: prm, phys: prm.Physics(), name: name, geo: &fileGeo{f: f, cm: maps[1].CM}, rep: &Report{}}
	b.sc, _ = newScene(maps, b.geo)
	for i := 0; i < 4; i++ {
		b.workers = append(b.workers, b.newWorker())
	}
	for _, fn := range []func() (int, error){b.buildNodes, b.buildTouchEnds, b.findLedges, b.buildEdges} {
		if _, err := fn(); err != nil {
			t.Fatal(err)
		}
	}
	b.capDegree()
	in := make([][]int32, len(b.nodes))
	for i := range b.edges {
		if e := &b.edges[i]; entryFeeder(e, b.nodes) {
			in[e.To] = append(in[e.To], int32(i))
		}
	}
	wk := b.workers[0]
	shown := 0
	for i := range b.edges {
		out := &b.edges[i]
		if !entryChecked(out, &b.nodes[out.From]) {
			continue
		}
		for _, j := range b.entrySources(out, in[out.From]) {
			ok, ran := wk.chain(&b.edges[j], out, true)
			if !ran || ok {
				continue
			}
			// replay with details
			inE := &b.edges[j]
			s, a, m := &b.nodes[inE.From], &b.nodes[out.From], &b.nodes[out.To]
			sup, _ := mergeSupports(supports(s, a), supports(a, m))
			wk.setWorld(sup...)
			wk.place(s)
			pin := b.plan(inE)
			runToArrival(wk.r, pin.Executor(), a.o, a.arrive(), navsim.TimeLimitMsec(dist3(s.o, a.o)), &wk.out)
			wk.r.Run(navsim.StopAt(a.o, 25), func(s *navsim.State, _ *navsim.StepResult) bool { return navsim.Stopped(s, a.o) || !s.OnGround() }, 2000, &wk.out)
			st := wk.r.State()
			pl := b.plan(out)
			wk.r.SetState(st, false)
			okk := runToArrival(wk.r, pl.Executor(), m.o, m.arrive(), navsim.TimeLimitMsec(dist3(a.o, m.o))+pl.BackupMsec, &wk.out)
			wk.place(a)
			okr := runToArrival(wk.r, pl.Executor(), m.o, m.arrive(), navsim.TimeLimitMsec(dist3(a.o, m.o))+pl.BackupMsec, &wk.out)
			t.Logf("from rest ok %v end %v; flags %v state %+v", okr, wk.r.State().Origin(), out.Flags, st)
			t.Logf("%v/%v %v -> %v (in from %v %v): stopped at %v vel %v; ok %v end %v after %d ms takeoff %v speed %v ; validated takeoff %v speed %v", out.Kind, out.Recipe, a.o, m.o, s.o, inE.Kind, st.Origin(), st.Velocity(), okk, wk.r.State().Origin(), wk.out.Msec, wk.out.Takeoff, wk.out.TakeoffSpeed, out.Takeoff, out.TakeoffSpeed)
			for k := 0; k < len(wk.out.Samples) && k < 0; k++ {
				t.Logf("    %v g %v cmd %+v", wk.out.Samples[k].Origin, wk.out.Samples[k].OnGround, func() any { if k < len(wk.out.Cmds) { return wk.out.Cmds[k] }; return nil }())
			}
			shown++
			break
		}
		if shown >= 8 {
			break
		}
	}
}
