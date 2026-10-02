package navrt

import (
	"math"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/q2const"
)

// Support for the combat layer, which moves the bot itself (strafes,
// dodges, short retreats) between the navigator's goals: a safety check
// for straight moves and path distances for the decision layer.

// SafeDir reports whether running from the bot's position at the last
// Tick in the horizontal direction dir (unit length) for msec at full
// speed is safe: floor stays under the bot (no drop of more than a step),
// it touches no avoided entity (Config.Avoid, SetAvoid), and it does not
// enter a hazard: lava or slime contents, or a laser the belief does not
// know to be off. Call it right after Tick (the Driver's Move hook).
func (n *Navigator) SafeDir(dir Vec3, msec int) bool {
	if !n.haveOrigin {
		return false
	}
	return n.safeDir(dir, msec) && !n.hazardAlong(dir, msec)
}

// hazardAlong reports a hazard on the straight run from the bot's
// position in direction dir for msec: hazardous contents at the feet or
// the waist every 16 units, or a laser that may be on across the swept box.
func (n *Navigator) hazardAlong(dir Vec3, msec int) bool {
	o := n.st.Origin()
	d := float32(msec) / 1000 * HorizontalSpeed
	end := add(o, Vec3{dir[0] * d, dir[1] * d, 0})
	if n.static != nil {
		const bad = q2const.CONTENTS_LAVA | q2const.CONTENTS_SLIME
		for k := float32(0); k <= d; k += 16 {
			p := add(o, Vec3{dir[0] * k, dir[1] * k, 0})
			for _, z := range []float32{-23, 0} {
				if n.static.PointContents(add(p, Vec3{0, 0, z}))&bad != 0 {
					return true
				}
			}
		}
	}
	mins, maxs := hull(n.st.Ducked())
	lo := Vec3{min(o[0], end[0]) + mins[0] - 4, min(o[1], end[1]) + mins[1] - 4, min(o[2], end[2]) + mins[2]}
	hi := Vec3{max(o[0], end[0]) + maxs[0] + 4, max(o[1], end[1]) + maxs[1] + 4, max(o[2], end[2]) + maxs[2]}
	for i := range n.g.Blockers {
		bl := &n.g.Blockers[i]
		if bl.Kind != nav.BlockLaser || n.ms.Possible(int32(i))&^nav.StateGone == 0 {
			continue
		}
		if segHitsBox(bl.Start, bl.End, lo, hi) {
			return true
		}
	}
	return false
}

// pathCache is the last single-source search of PathDistance.
type pathCache struct {
	valid  bool
	start  nav.NodeID
	frames int
	time   int64
	dist   []float32
}

// PathDistance returns the travel distance (units: the planned time at
// HorizontalSpeed) from point from to point to over the graph in the
// believed state, and ok false when no path joins their nearest nodes.
// The search from a node is kept until the belief changes, so asking for
// many destinations from one place costs one search. It is the decision
// layer's PathFunc (decide.ProjectorConfig.Path).
func (n *Navigator) PathDistance(from, to Vec3) (float32, bool) {
	a := n.g.Localize(from, 192)
	b := n.g.Localize(to, 192)
	if a == nav.NoNode || b == nav.NoNode {
		return 0, false
	}
	c := &n.paths
	if !c.valid || c.start != a || c.frames != n.beliefFrames || c.time != n.beliefTime {
		c.dist = n.pl.Distances(a, n.cost, c.dist)
		c.valid, c.start, c.frames, c.time = true, a, n.beliefFrames, n.beliefTime
	}
	s := c.dist[b]
	if math.IsInf(float64(s), 1) {
		return 0, false
	}
	return s*HorizontalSpeed + dist3(from, n.g.Nodes[a].Origin) + dist3(to, n.g.Nodes[b].Origin), true
}

// Distances returns the least cost (seconds) from start to every node of
// the graph (+Inf where unreachable) under cost, reusing buf when it is
// large enough. It is a full Dijkstra search.
func (p *Planner) Distances(start nav.NodeID, cost CostFunc, buf []float32) []float32 {
	g := p.g
	n := len(g.Nodes)
	if cap(buf) < n {
		buf = make([]float32, n)
	}
	buf = buf[:n]
	inf := float32(math.Inf(1))
	for i := range buf {
		buf[i] = inf
	}
	if g.Node(start) == nil {
		return buf
	}
	p.heap = p.heap[:0]
	buf[start] = 0
	p.heap.push(item{node: int32(start), edge: noVia})
	for len(p.heap) > 0 {
		it := p.heap.pop()
		u := nav.NodeID(it.node)
		if it.g > buf[u] {
			continue
		}
		lo, hi := g.OutRange(u)
		for i := lo; i < hi; i++ {
			e := &g.Edges[i]
			c, ok := cost(i, e)
			if !ok {
				continue
			}
			if nd := it.g + c; nd < buf[e.To] {
				buf[e.To] = nd
				p.heap.push(item{f: nd, g: nd, node: int32(e.To), edge: int32(i)})
			}
		}
	}
	return buf
}
