package nav

import (
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/cmodel"
)

// BlockerSolid returns blocker b at pose k as a navsim solid.
func (g *Graph) BlockerSolid(b int32, k int) navsim.Solid {
	bl := &g.Blockers[b]
	p := bl.Poses[k]
	return navsim.Solid{ID: int(bl.Entity), Headnode: bl.Headnode, Origin: p.Origin, Angles: p.Angles, Mins: bl.Mins, Maxs: bl.Maxs}
}

// SetWorld makes w hold the graph's static solids plus every solid blocker
// at the pose poses[b] gives (a negative pose leaves it out; nil poses
// leaves all blockers out).
func (g *Graph) SetWorld(w *navsim.World, poses []int) {
	solids := append([]navsim.Solid(nil), g.Solids...)
	for b := range g.Blockers {
		bl := &g.Blockers[b]
		if !bl.Solid || b >= len(poses) || poses[b] < 0 || poses[b] >= len(bl.Poses) {
			continue
		}
		solids = append(solids, g.BlockerSolid(int32(b), poses[b]))
	}
	w.SetSolids(solids)
}

// NewWorld returns a navsim world for the graph's map (cm must be the map
// the graph was built from) with the static solids and the blockers at
// poses (see SetWorld).
func (g *Graph) NewWorld(cm *cmodel.Map, poses []int) *navsim.World {
	w := navsim.NewWorld(cm)
	g.SetWorld(w, poses)
	return w
}

// EdgePoses returns blocker poses edge e was validated with: the movers
// its end nodes stand on at their poses, every blocker a condition pins to
// one pose (the mover an item to pick up rests on, a door that must be
// open) at that pose, and, for an EdgeSpawnWorld edge, every other solid
// blocker in its spawn state. Blockers left at -1 are out of the way.
func (g *Graph) EdgePoses(e *Edge) []int {
	poses := make([]int, len(g.Blockers))
	for b := range poses {
		poses[b] = -1
		if e.Flags&EdgeSpawnWorld != 0 {
			poses[b] = int(g.Blockers[b].Spawn)
		}
	}
	for _, r := range e.Reqs {
		if k := singlePose(r.States); k >= 0 && k < len(g.Blockers[r.Blocker].Poses) {
			poses[r.Blocker] = k
		}
	}
	for _, id := range []NodeID{e.From, e.To} {
		if n := g.Node(id); n != nil && n.Blocker >= 0 {
			poses[n.Blocker] = int(n.Pose)
		}
	}
	return poses
}

// SpawnPoses returns every blocker's spawn pose (-1 when it starts gone),
// the level as it starts.
func (g *Graph) SpawnPoses() []int {
	poses := make([]int, len(g.Blockers))
	for b := range poses {
		poses[b] = int(g.Blockers[b].Spawn)
	}
	return poses
}

// EdgeWorld sets w up as the world edge e was validated in.
func (g *Graph) EdgeWorld(w *navsim.World, e *Edge) { g.SetWorld(w, g.EdgePoses(e)) }

// RunnerVolumes returns the volumes as navsim volumes (IDs are indexes into
// Volumes), for a runner that reports what an edge sets off.
func (g *Graph) RunnerVolumes() []navsim.Volume {
	out := make([]navsim.Volume, len(g.Volumes))
	for i, v := range g.Volumes {
		out[i] = navsim.Volume{ID: i, Min: v.Min, Max: v.Max}
	}
	return out
}

// NewRunner returns a runner in w with the graph's physics, volumes and
// hooks.
func (g *Graph) NewRunner(w *navsim.World) *navsim.Runner {
	r := navsim.NewRunner(w, g.Params.Physics())
	r.Volumes = g.RunnerVolumes()
	r.Pushes = g.Pushes
	r.Teleports = g.Teleports
	return r
}

// Place puts the runner's player at node id at rest, as the builder starts
// every simulated edge: reset there, then one HoldCmd.
func (g *Graph) Place(r *navsim.Runner, id NodeID) {
	n := g.Node(id)
	r.Reset(n.Origin, n.Flags&NodeCrouch != 0)
	r.Step(HoldCmd(n))
}

// HoldCmd is the idle command that keeps a player at node n: ducked on a
// crouch node, facing the ladder on a ladder node (pmove's ladder mode
// then holds the player in place without input).
func HoldCmd(n *Node) navsim.Cmd {
	c := navsim.Cmd{}
	if n.Flags&NodeCrouch != 0 {
		c.Up = -400
	}
	if n.Flags&NodeLadder != 0 {
		c.Yaw = n.Yaw
	}
	return c
}

// Touched reports whether a run with outcome out set off touch edge e's
// target: touched the button, or entered a volume of the trigger or item
// (out's volume ids must be indexes into Volumes, as RunnerVolumes makes
// them). An item on a mover counts only in the pose e's conditions put
// the mover in.
func (g *Graph) Touched(e *Edge, out *navsim.Outcome) bool {
	for _, f := range e.Effects {
		if e.Target != 0 && f.Entity != e.Target {
			continue
		}
		if f.Blocker >= 0 && f.Pose >= 0 && !reqAllows(e, f.Blocker, Pose(int(f.Pose))) {
			continue
		}
		switch f.Kind {
		case EffButton:
			if out.HasTouched(int(f.Entity)) {
				return true
			}
		case EffTrigger, EffItem:
			for vi := range g.Volumes {
				v := &g.Volumes[vi]
				if v.Entity == f.Entity && v.Kind == f.Kind && v.Blocker == f.Blocker && v.Pose == f.Pose && out.HasVolume(vi) {
					return true
				}
			}
		}
	}
	return false
}

// reqAllows reports whether e's conditions allow blocker b in a state of
// m (an edge without a condition on b allows every state).
func reqAllows(e *Edge, b int32, m StateMask) bool {
	for _, r := range e.Reqs {
		if r.Blocker == b && r.States&m == 0 {
			return false
		}
	}
	return true
}

// EdgeDone reports whether a run of edge e that is in state s with outcome
// out has completed it:
//   - a touch edge runs its plan until Touched, then coasts to rest
//     (navsim.Coast); it is done when the target was set off and the
//     player is at rest (navsim.AtRest), which the builder found within
//     the arrival tolerances of To;
//   - a teleport edge is done when the teleporter fired;
//   - any other edge when the player arrived at To (navsim.Arrived with
//     ArriveMode(To)).
func (g *Graph) EdgeDone(e *Edge, s *navsim.State, out *navsim.Outcome) bool {
	switch e.Kind {
	case EdgeTouch:
		return g.Touched(e, out) && navsim.AtRest(s)
	case EdgeTeleport:
		return out.Teleported != 0
	}
	to := g.Node(e.To)
	return to != nil && navsim.Arrived(s, to.Origin, g.ArriveMode(e.To))
}

// singlePose returns i when m is exactly Pose(i), else -1.
func singlePose(m StateMask) int {
	if m == 0 || m&StateGone != 0 || m&(m-1) != 0 {
		return -1
	}
	for i := 0; i < MaxPoses; i++ {
		if m == Pose(i) {
			return i
		}
	}
	return -1
}
