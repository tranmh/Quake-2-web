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
	// OK: the player arrived (or, for a touch edge, set the target off).
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

// Done reports whether a run that ended in state s with outcome out
// completed edge e: arrival at To, or for a touch edge the target set off.
func (v *Verifier) Done(e *nav.Edge, s *navsim.State, out *navsim.Outcome) bool {
	if e.Kind == nav.EdgeTouch {
		return v.touched(e, out)
	}
	return navsim.Arrived(s, v.g.Nodes[e.To].Origin, v.g.ArriveMode(e.To))
}

func (v *Verifier) touched(e *nav.Edge, out *navsim.Outcome) bool {
	for _, f := range e.Effects {
		if e.Target != 0 && f.Entity != e.Target {
			continue
		}
		switch f.Kind {
		case nav.EffButton:
			if out.HasTouched(int(f.Entity)) {
				return true
			}
		case nav.EffTrigger, nav.EffItem:
			for vi := range v.g.Volumes {
				if v.g.Volumes[vi].Entity == f.Entity && out.HasVolume(vi) {
					return true
				}
			}
		}
	}
	return false
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
		limit = navsim.TimeLimitMsec(dist3(from, p.Target)) + 500
		hit := false
		stop := func(s *navsim.State, r *navsim.StepResult) bool {
			// Run records contacts and volumes before calling stop
			hit = v.touched(e, &v.out)
			return hit
		}
		v.r.Run(p.Executor(), stop, limit, &v.out)
		res.OK = hit
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
	if !res.OK {
		res.Reason = fmt.Sprintf("%s %d->%d did not arrive: ended at %v after %d ms (target %v)", e.Kind, e.From, e.To, res.End, res.Msec, to)
	}
	return res
}

// Runner returns the verifier's runner (its state is where the last
// re-simulation ended; stepping it continues from there).
func (v *Verifier) Runner() *navsim.Runner { return v.r }

// World returns the verifier's world (set up for the last edge).
func (v *Verifier) World() *navsim.World { return v.w }
