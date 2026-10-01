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

// EdgePoses returns the blocker poses edge e was validated with: the movers
// its end nodes stand on at their poses, and, for an EdgeSpawnWorld edge,
// every other solid blocker in its spawn state.
func (g *Graph) EdgePoses(e *Edge) []int {
	poses := make([]int, len(g.Blockers))
	for b := range poses {
		poses[b] = -1
		if e.Flags&EdgeSpawnWorld != 0 {
			poses[b] = int(g.Blockers[b].Spawn)
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
