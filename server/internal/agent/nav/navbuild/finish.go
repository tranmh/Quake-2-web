package navbuild

import (
	"sort"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// finish prunes what no spawn point reaches (checkEntries capped the out
// degree before).
func (b *builder) finish() (int, error) {
	keep := b.reachable()
	b.final = make([]nav.NodeID, len(b.nodes))
	next := nav.NodeID(0)
	for i := range b.nodes {
		if keep[i] {
			b.final[i] = next
			next++
		} else {
			b.final[i] = nav.NoNode
		}
	}
	return int(next), nil
}

func special(k nav.EdgeKind) bool {
	switch k {
	case nav.EdgeRide, nav.EdgeLadder, nav.EdgeTouch, nav.EdgeTeleport, nav.EdgeWaterJump:
		return true
	}
	return false
}

// capDegree keeps, per node, every special edge and every short neighbor
// edge, then the cheapest longer edges whose targets are not within 64
// units of a kept target, up to MaxDegree regular edges.
func (b *builder) capDegree() {
	out := b.edges[:0:0]
	for lo := 0; lo < len(b.edges); {
		hi := lo
		for hi < len(b.edges) && b.edges[hi].From == b.edges[lo].From {
			hi++
		}
		group := b.edges[lo:hi]
		from := b.nodes[group[0].From].o
		var keep []nav.Edge
		var long []nav.Edge
		regular := 0
		for _, e := range group {
			to := b.nodes[e.To].o
			switch {
			case special(e.Kind):
				keep = append(keep, e)
			case hdist(from, to) <= neighborRadius && abs32(to[2]-from[2]) <= neighborDZ:
				keep = append(keep, e)
				regular++
			default:
				long = append(long, e)
			}
		}
		sort.SliceStable(long, func(i, j int) bool {
			if long[i].Cost != long[j].Cost {
				return long[i].Cost < long[j].Cost
			}
			return long[i].To < long[j].To
		})
		for _, e := range long {
			if regular >= b.p.MaxDegree {
				break
			}
			to := b.nodes[e.To].o
			redundant := false
			for _, k := range keep {
				if !special(k.Kind) && dist3(b.nodes[k.To].o, to) < 64 && hdist(from, b.nodes[k.To].o) > neighborRadius {
					redundant = true
					break
				}
			}
			if redundant {
				continue
			}
			keep = append(keep, e)
			regular++
		}
		sort.SliceStable(keep, func(i, j int) bool {
			if keep[i].To != keep[j].To {
				return keep[i].To < keep[j].To
			}
			return keep[i].Kind < keep[j].Kind
		})
		out = append(out, keep...)
		lo = hi
	}
	b.edges = out
}

// reachable marks the nodes reachable from a spawn node over any edge
// (conditions ignored: optimistic). Without spawn nodes everything is kept.
func (b *builder) reachable() []bool {
	keep := make([]bool, len(b.nodes))
	first := make([]int, len(b.nodes)+1)
	for i := range first {
		first[i] = -1
	}
	for i := len(b.edges) - 1; i >= 0; i-- {
		first[b.edges[i].From] = i
	}
	var stack []int32
	for i := range b.nodes {
		if b.nodes[i].prio == prioSpawn {
			keep[i] = true
			stack = append(stack, int32(i))
		}
	}
	if len(stack) == 0 {
		for i := range keep {
			keep[i] = true
		}
		return keep
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := first[n]; i >= 0 && i < len(b.edges) && b.edges[i].From == nav.NodeID(n); i++ {
			t := b.edges[i].To
			if !keep[t] {
				keep[t] = true
				stack = append(stack, int32(t))
			}
		}
	}
	return keep
}

// graph assembles the nav.Graph from the kept nodes and edges and floods
// the regions.
func (b *builder) graph() (*nav.Graph, error) {
	g := &nav.Graph{Format: nav.FormatVersion, Map: b.name, Checksum: b.geo.cm.Checksum, PhysicsHash: b.p.PhysicsHash(),
		Params: b.p, Skill: -1, Blockers: b.sc.blockers, Ents: b.sc.ents, Solids: b.sc.statics, Pushes: b.sc.pushes}
	for s, md := range b.sc.maps {
		g.Scene[s] = nav.SceneDigest(md)
	}
	for _, v := range b.sc.vols {
		g.Volumes = append(g.Volumes, nav.Volume{Kind: v.kind, Entity: v.entity, Pose: v.pose, Blocker: v.blocker, Min: v.min, Max: v.max})
	}
	for _, t := range b.sc.teles {
		g.Teleports = append(g.Teleports, navsim.Teleport{ID: int(t.entity), Min: t.min, Max: t.max, Dest: t.dest, Angles: t.angles})
	}
	for i := range b.nodes {
		if b.final[i] == nav.NoNode {
			continue
		}
		n := &b.nodes[i]
		g.Nodes = append(g.Nodes, nav.Node{Origin: n.o, Flags: n.flags, Blocker: n.blocker, Pose: n.pose, Yaw: n.ladderYaw})
		if n.blocker < 0 {
			g.Nodes[len(g.Nodes)-1].Pose = 0
		}
	}
	for _, e := range b.edges {
		f, t := b.final[e.From], b.final[e.To]
		if f == nav.NoNode || t == nav.NoNode {
			continue
		}
		e.From, e.To = f, t
		g.Edges = append(g.Edges, e)
	}
	for si, s := range b.sc.spawns {
		sp := nav.Spawn{Entity: s.entity, Targetname: s.targetname, Origin: s.origin, Node: nav.NoNode, Skills: s.skills}
		for i := range b.nodes {
			if b.nodes[i].prio == prioSpawn && b.nodes[i].spawn == si {
				sp.Node = b.final[i]
			}
		}
		g.Spawns = append(g.Spawns, sp)
	}
	if err := g.Finish(); err != nil {
		return nil, err
	}
	regions(g, b.p.RegionRadius)
	return g, nil
}

// regions floods regions greedily: in node order, an unassigned node seeds
// a region that takes in the nodes reachable over walk and crouch edges
// (either direction) within radius of the seed (and 64 units of height).
func regions(g *nav.Graph, radius float32) {
	adj := make([][]nav.NodeID, len(g.Nodes))
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind != nav.EdgeWalk && e.Kind != nav.EdgeCrouch {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}
	for i := range g.Nodes {
		g.Nodes[i].Region = -1
	}
	r := int32(0)
	var queue []nav.NodeID
	for s := range g.Nodes {
		if g.Nodes[s].Region >= 0 {
			continue
		}
		seed := g.Nodes[s].Origin
		g.Nodes[s].Region = r
		queue = append(queue[:0], nav.NodeID(s))
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, m := range adj[n] {
				nd := &g.Nodes[m]
				if nd.Region >= 0 || hdist(nd.Origin, seed) > radius || abs32(nd.Origin[2]-seed[2]) > 64 {
					continue
				}
				nd.Region = r
				queue = append(queue, m)
			}
		}
		r++
	}
}
