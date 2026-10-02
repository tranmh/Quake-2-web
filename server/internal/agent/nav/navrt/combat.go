package navrt

import (
	"math"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
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

// Clearance returns how far (units, up to d) the bot's hull can run from
// its position at the last Tick in the horizontal direction dir (unit
// length) before a solid stops it: the world, the brush entities and the
// bodies as the navigator's world poses them. A step the bot walks up
// (navsim.StepHeight) does not stop it. Call it right after Tick, like
// SafeDir: SafeDir judges the floor and the hazards of a run, Clearance
// the room for it.
func (n *Navigator) Clearance(dir Vec3, d float32) float32 {
	if !n.haveOrigin {
		return 0
	}
	if n.w == nil || d <= 0 {
		return max(d, 0)
	}
	o := n.st.Origin()
	mins, maxs := hull(n.st.Ducked())
	run := Vec3{dir[0] * d, dir[1] * d, 0}
	room := float32(0)
	if tr := n.w.Trace(o, mins, maxs, add(o, run), q2const.MASK_PLAYERSOLID); !tr.StartSolid {
		room = d * tr.Fraction
	}
	if room < d {
		// over a step: the same run lifted by a step's height
		up := add(o, Vec3{0, 0, navsim.StepHeight})
		if lift := n.w.Trace(o, mins, maxs, up, q2const.MASK_PLAYERSOLID); lift.Fraction == 1 && !lift.StartSolid {
			if tr := n.w.Trace(up, mins, maxs, add(up, run), q2const.MASK_PLAYERSOLID); !tr.StartSolid {
				room = max(room, d*tr.Fraction)
			}
		}
	}
	return room
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

// regionReach is the region-level reachability of the graph: which
// regions each region can reach through any edge (conditions ignored:
// doors count as passable), computed per source region on first use.
type regionReach struct {
	adj   [][]int32
	nodes []int // nodes per region
	reach map[int32][]bool
	size  map[int32]int // nodes in the regions a region reaches
}

func (r *regionReach) init(g *nav.Graph) {
	n := int32(0)
	for i := range g.Nodes {
		n = max(n, g.Nodes[i].Region+1)
	}
	r.adj = make([][]int32, n)
	r.nodes = make([]int, n)
	for i := range g.Nodes {
		if rg := g.Nodes[i].Region; rg >= 0 {
			r.nodes[rg]++
		}
	}
	seen := map[[2]int32]bool{}
	for i := range g.Edges {
		e := &g.Edges[i]
		a, b := g.Nodes[e.From].Region, g.Nodes[e.To].Region
		if a < 0 || b < 0 || a == b || seen[[2]int32{a, b}] {
			continue
		}
		seen[[2]int32{a, b}] = true
		r.adj[a] = append(r.adj[a], b)
	}
	r.reach = map[int32][]bool{}
	r.size = map[int32]int{}
}

// from returns the regions region a reaches (a breadth-first search, cached).
func (r *regionReach) from(a int32) []bool {
	if out, ok := r.reach[a]; ok {
		return out
	}
	out := make([]bool, len(r.adj))
	out[a] = true
	queue := []int32{a}
	for len(queue) > 0 {
		x := queue[0]
		queue = queue[1:]
		for _, y := range r.adj[x] {
			if !out[y] {
				out[y] = true
				queue = append(queue, y)
			}
		}
	}
	r.reach[a] = out
	return out
}

// CanReturn reports whether the bot could come back from point to to point
// from: the region of to's nearest node reaches the region of from's over
// the graph's edges (conditions ignored). A spot down a one-way drop the
// bot cannot climb out of fails it: a detour there is a trap. Points off
// the graph count as returnable.
func (n *Navigator) CanReturn(from, to Vec3) bool {
	a, b := n.g.Localize(to, 192), n.g.Localize(from, 192)
	if a == nav.NoNode || b == nav.NoNode {
		return true
	}
	ra, rb := n.g.Nodes[a].Region, n.g.Nodes[b].Region
	if ra < 0 || rb < 0 || ra == rb {
		return true
	}
	if n.regions.adj == nil {
		n.regions.init(n.g)
	}
	return n.regions.from(ra)[rb]
}

// ReachSize returns how many nodes of the graph the region of p's nearest
// node reaches over its edges (conditions ignored, as CanReturn), and the
// graph's node count; reach is total for a point off the graph. A reach
// of a few dozen nodes out of thousands is a pit: whatever the bot was
// after, it cannot get there from p.
func (n *Navigator) ReachSize(p Vec3) (reach, total int) {
	total = len(n.g.Nodes)
	a := n.g.Localize(p, 192)
	if a == nav.NoNode || n.g.Nodes[a].Region < 0 {
		return total, total
	}
	if n.regions.adj == nil {
		n.regions.init(n.g)
	}
	ra := n.g.Nodes[a].Region
	if k, ok := n.regions.size[ra]; ok {
		return k, total
	}
	k := 0
	for rg, ok := range n.regions.from(ra) {
		if ok {
			k += n.regions.nodes[rg]
		}
	}
	n.regions.size[ra] = k
	return k, total
}

// inferPassage concludes from where the bot stands that a removable
// blocker is gone: when the nodes it can reach from start in the believed
// state are a pocket (at most a quarter of the graph, no spawn point in
// it) that the rest of the graph enters only through edges needing one
// and the same removable blocker gone, the bot came in that way, so the
// blocker is gone (an explosive wall destroyed out of view, whose absence
// the belief cannot tell from it being out of sight). It assumes so
// (MapState.Assume) and reports whether the belief changed. Its own
// position and the static map are all it uses.
func (n *Navigator) inferPassage(start nav.NodeID) bool {
	g := n.g
	holds := func(e *nav.Edge) bool {
		ok, _ := n.ms.Holds(e, n.now)
		return ok
	}
	in := g.Reachable([]nav.NodeID{start}, holds)
	size := 0
	for _, ok := range in {
		if ok {
			size++
		}
	}
	if size == 0 || size > len(g.Nodes)/4 {
		return false
	}
	if n.md != nil {
		for i := range n.md.Spawns {
			if id := g.Localize(n.md.Spawns[i].Origin, 64); id != nav.NoNode && in[id] {
				return false // the bot may have started in here
			}
		}
	}
	gone := int32(-1)
	for i := range g.Edges {
		e := &g.Edges[i]
		if !in[e.To] || in[e.From] {
			continue
		}
		var fail []nav.Req
		for _, r := range e.Reqs {
			if ok, _ := n.ms.Satisfied(r, n.now); !ok {
				fail = append(fail, r)
			}
		}
		switch {
		case len(fail) == 0:
			return false // an open way in (a drop): the bot may have come that way
		case len(fail) > 1 || fail[0].States&^nav.StateGone != 0 || fail[0].Blocker < 0 || !g.Blockers[fail[0].Blocker].Gone:
			return false
		case gone >= 0 && gone != fail[0].Blocker:
			return false
		}
		gone = fail[0].Blocker
	}
	if gone < 0 || !n.ms.Assume(gone, -1, n.now) {
		return false
	}
	n.bl.Refresh(n.ms)
	return true
}
