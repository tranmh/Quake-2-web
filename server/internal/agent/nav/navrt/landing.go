package navrt

import (
	"fmt"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Flight checks. The builder validates a jump or a drop from rest on its
// start node, and from running entries off walks in a few directions;
// the follower comes in at full speed from wherever the path leads. So
// before it runs a flight that is not flagged nav.EdgeFromRest, the
// navigator simulates it in its prediction world from the bot's actual
// state, and stops on the start node first (the validated way) when the
// simulation does not arrive.
//
// Ledges get a stricter check. The builder takes a flight as done at its
// arrival on the end node: the first step on the ground within the
// arrival tolerance. On a ledge (nav.NodeLedge: a drop-off next to the
// node) a fast landing can be right at the lip, and the next step slides
// the bot over it, whatever the follower does then. A flight onto a ledge
// must therefore also come to rest there after the landing (a simulated
// braking stop, navsim.StopAt), and an edge that does not even from rest
// is left out for good. That verdict is judged in the world the builder
// validated the edge in (Graph.EdgeWorld: the static solids and the
// movers at the poses the edge was checked with), not in the prediction
// world: a monster standing on the ledge, or a door caught halfway, must
// not take the edge away for the rest of the level.

// landingStopMsec is how long the simulated stop after a landing gets.
const landingStopMsec = 600

// flight reports a jump or drop edge.
func flight(e *nav.Edge) bool { return e.Kind == nav.EdgeJump || e.Kind == nav.EdgeDrop }

// checksLanding reports whether edge e gets the ledge check: a jump or
// drop onto a ledge node.
func (n *Navigator) checksLanding(e *nav.Edge) bool {
	return flight(e) && n.g.Nodes[e.To].Flags&nav.NodeLedge != 0
}

// trial simulates edge i and reports whether it arrives and, with rest,
// whether the bot then comes to rest on its end node. From the bot's
// state now it runs in the prediction world (the believed movers and the
// monsters in view count). The ledge check from rest (fromRest and rest:
// at rest on the start node, after the follower's stop there) runs in the
// edge's validation world instead; its verdict only depends on static
// knowledge and is cached.
func (n *Navigator) trial(i int, e *nav.Edge, fromRest, rest bool) bool {
	if n.w == nil {
		return true
	}
	cache := fromRest && rest
	if v, ok := n.landing[i]; ok && cache {
		return v
	}
	var r *navsim.Runner
	if cache {
		if n.landSim == nil {
			n.landWorld = navsim.NewWorld(n.md.CM)
			n.landSim = n.g.NewRunner(n.landWorld)
		}
		n.g.EdgeWorld(n.landWorld, e)
		r = n.landSim
	} else {
		if n.sim == nil {
			n.sim = n.g.NewRunner(n.w)
		}
		r = n.sim
	}
	if fromRest {
		n.g.Place(r, e.From)
	} else {
		r.SetState(n.st, false)
	}
	var out navsim.Outcome
	r.Run(n.g.Plan(e).Executor(), func(s *navsim.State, _ *navsim.StepResult) bool {
		return n.g.EdgeDone(e, s, &out)
	}, int(n.limit(e)), &out)
	ok := out.Done
	if ok && rest {
		to := n.g.Nodes[e.To].Origin
		r.Run(navsim.StopAt(to, CmdMsec), func(s *navsim.State, _ *navsim.StepResult) bool {
			return navsim.Stopped(s, to)
		}, landingStopMsec, &out)
		s := r.State()
		o := s.Origin()
		ok = s.OnGround() && distH(o, to) <= 2*navsim.ArriveXY && abs32(o[2]-to[2]) <= navsim.ArriveZ
	}
	if cache {
		if n.landing == nil {
			n.landing = map[int]bool{}
		}
		n.landing[i] = ok
	}
	return ok
}

// rejectLanding gives up on flight edge e after it failed the ledge check
// from rest (cached by trial: cost leaves it out for the level), and
// plans again.
func (n *Navigator) rejectLanding(e *nav.Edge) {
	n.status.Cause = CauseWorld
	n.status.Reason = fmt.Sprintf("%s %s would not come to rest on its ledge", e.Kind, EdgeKey(e))
	n.relocalize(n.status.Reason, true)
}
