package navbuild

import (
	"fmt"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/cmodel"
)

// Verifier re-simulates graph edges with navsim: the edge's plan from its
// start node at rest, in the world it was validated in. It is what the
// property tests and q2nav verify use. A Verifier is not safe for
// concurrent use.
type Verifier struct {
	g   *nav.Graph
	w   *navsim.World
	r   *navsim.Runner
	out navsim.Outcome
}

// NewVerifier returns a verifier for graph g of collision map cm.
func NewVerifier(g *nav.Graph, cm *cmodel.Map) *Verifier {
	w := navsim.NewWorld(cm)
	return &Verifier{g: g, w: w, r: g.NewRunner(w)}
}

// Resim is the outcome of re-simulating one edge.
type Resim struct {
	// OK: the player arrived (a touch edge: set the target off and came to
	// rest at To).
	OK bool
	// Skipped: the edge is not simulated (rides follow the mover).
	Skipped bool
	Reason  string
	Msec    int
	Start   navsim.State
	End     navsim.Vec3
	// Cmds are the commands the executor produced (one per step).
	Cmds []navsim.Cmd
}

// Edge re-simulates edge i from its start node at rest (Graph.Place).
func (v *Verifier) Edge(i int) Resim {
	e := &v.g.Edges[i]
	v.g.EdgeWorld(v.w, e)
	v.g.Place(v.r, e.From)
	return v.run(e)
}

// EdgeFrom re-simulates edge i from state st (for example the live
// server's player state at the start node).
func (v *Verifier) EdgeFrom(i int, st navsim.State) Resim {
	return v.EdgeIn(i, st, v.g.EdgePoses(&v.g.Edges[i]))
}

// EdgeIn re-simulates edge i from state st with the blockers in poses (see
// Graph.SetWorld), for example the level as it starts (SpawnPoses).
func (v *Verifier) EdgeIn(i int, st navsim.State, poses []int) Resim {
	e := &v.g.Edges[i]
	v.g.SetWorld(v.w, poses)
	v.r.SetState(st, false) // a consistent server state: no initial snap
	return v.run(e)
}

// Chain re-simulates edge in from rest at its start and then edge out
// right away from wherever in arrived, the way a follower that does not
// stop between edges runs them; with stop, navsim.StopAt first brings the
// player to rest at out's start, as an nav.EdgeFromRest edge requires. The
// result is out's (Start is the state out started from). It is Skipped
// when in does not end where out starts, the two edges need one mover at
// different poses, or in itself does not arrive.
func (v *Verifier) Chain(in, out int, stop bool) Resim {
	ei, eo := &v.g.Edges[in], &v.g.Edges[out]
	if ei.To != eo.From || ei.Kind == nav.EdgeRide || ei.Kind == nav.EdgeTouch || ei.Kind == nav.EdgeTeleport {
		return Resim{Skipped: true, Reason: "not a chain"}
	}
	pi, po := v.g.EdgePoses(ei), v.g.EdgePoses(eo)
	for b := range po {
		switch {
		case po[b] < 0:
			po[b] = pi[b]
		case pi[b] >= 0 && pi[b] != po[b]:
			return Resim{Skipped: true, Reason: "the edges need a mover at two poses"}
		}
	}
	v.g.SetWorld(v.w, po)
	v.g.Place(v.r, ei.From)
	if r := v.run(ei); !r.OK {
		return Resim{Skipped: true, Reason: "in-edge: " + r.Reason}
	}
	if stop {
		a := v.g.Nodes[eo.From].Origin
		v.r.Run(navsim.StopAt(a, v.g.Params.StepMsec), func(s *navsim.State, _ *navsim.StepResult) bool { return navsim.Stopped(s, a) }, 2000, &v.out)
	}
	return v.run(eo)
}

// Done reports whether a run that ended in state s with outcome out
// completed edge e (nav.Graph.EdgeDone).
func (v *Verifier) Done(e *nav.Edge, s *navsim.State, out *navsim.Outcome) bool {
	return v.g.EdgeDone(e, s, out)
}

func (v *Verifier) run(e *nav.Edge) Resim {
	res := Resim{Start: v.r.State()}
	if e.Kind == nav.EdgeRide {
		res.Skipped, res.Reason = true, "ride: the mover carries the player"
		return res
	}
	p := v.g.Plan(e)
	from, to := v.g.Nodes[e.From].Origin, v.g.Nodes[e.To].Origin
	limit := navsim.TimeLimitMsec(dist3(from, to)) + p.BackupMsec
	switch e.Kind {
	case nav.EdgeTouch:
		// walk at the target until it is set off, then coast to rest
		limit = navsim.TimeLimitMsec(dist3(from, p.Target)) + 500 + coastMsec
		hit := false
		stop := func(s *navsim.State, r *navsim.StepResult) bool {
			// Run records contacts and volumes before calling stop
			hit = hit || v.g.Touched(e, &v.out)
			return v.g.EdgeDone(e, s, &v.out)
		}
		v.r.Run(navsim.Coast(p.Executor(), &hit), stop, limit, &v.out)
		res.OK = v.out.Done && navsim.Near(v.r.State().Origin(), to)
		if v.out.Done && !res.OK {
			res.Reason = fmt.Sprintf("touch %d->%d set its target off but came to rest at %v, not at %v", e.From, e.To, v.r.State().Origin(), to)
		}
	case nav.EdgeTeleport:
		tele := false
		v.r.Run(p.Executor(), func(s *navsim.State, r *navsim.StepResult) bool { tele = r.Teleported != 0; return tele }, limit+500, &v.out)
		res.OK = tele
	default:
		res.OK = runToArrival(v.r, p.Executor(), to, v.g.ArriveMode(e.To), limit, &v.out)
	}
	res.Msec = v.out.Msec
	res.End = v.r.State().Origin()
	res.Cmds = append([]navsim.Cmd(nil), v.out.Cmds...)
	if !res.OK && res.Reason == "" {
		res.Reason = fmt.Sprintf("%s %d->%d did not arrive: ended at %v after %d ms (target %v)", e.Kind, e.From, e.To, res.End, res.Msec, to)
	}
	return res
}

// Runner returns the verifier's runner (its state is where the last
// re-simulation ended; stepping it continues from there).
func (v *Verifier) Runner() *navsim.Runner { return v.r }

// World returns the verifier's world (set up for the last edge).
func (v *Verifier) World() *navsim.World { return v.w }
