package navrt

import (
	"math"

	"quake2web/server/internal/agent/nav"
)

// CostFunc returns the cost (seconds) of using edge i (e is
// &Graph.Edges[i]), or ok=false to leave it out. Costs must not be
// negative.
type CostFunc func(i int, e *nav.Edge) (cost float32, ok bool)

// Path is a planned route: the edges to run from Start, in order.
type Path struct {
	Start nav.NodeID
	// Edges are indexes into Graph.Edges; Edges[0] starts at Start and
	// each edge starts where the previous one ends.
	Edges []int
	// Cost is the planned time in seconds.
	Cost float32
	// GoalEdge reports that the last edge satisfies the goal by its
	// effects (a touch, a trigger on the way) rather than by its end node.
	GoalEdge bool
	// Expanded is the number of nodes the search expanded.
	Expanded int
}

// End returns the node the path ends at.
func (p *Path) End(g *nav.Graph) nav.NodeID {
	if len(p.Edges) == 0 {
		return p.Start
	}
	return g.Edges[p.Edges[len(p.Edges)-1]].To
}

// Planner runs A* searches over one graph. It keeps its scratch arrays
// between searches; it is not safe for concurrent use.
type Planner struct {
	g *nav.Graph
	// ClimbWeight adds seconds per unit the goal lies above a node to the
	// heuristic. 0 (the default) keeps the heuristic admissible: the
	// search returns optimal paths. Graphs with teleporters or pushers
	// always search with a zero heuristic (a teleport moves faster than
	// any bound).
	ClimbWeight float32

	dist  []float32
	via   []int32
	stamp []uint32
	gen   uint32
	heap  pqueue
}

// NewPlanner returns a planner for g.
func NewPlanner(g *nav.Graph) *Planner {
	n := len(g.Nodes)
	return &Planner{g: g, dist: make([]float32, n), via: make([]int32, n), stamp: make([]uint32, n)}
}

// Graph returns the planner's graph.
func (p *Planner) Graph() *nav.Graph { return p.g }

// Heuristic returns the lower bound of the cost from node id to t: the
// horizontal distance to the target box at HorizontalSpeed (plus the
// optional climb weight).
func (p *Planner) Heuristic(id nav.NodeID, t *Target) float32 {
	if len(p.g.Teleports) > 0 || len(p.g.Pushes) > 0 {
		return 0
	}
	o := p.g.Nodes[id].Origin
	h := t.distance(o) / HorizontalSpeed
	if p.ClimbWeight > 0 {
		h += t.climb(o) * p.ClimbWeight
	}
	return h
}

// Find returns the cheapest path from start to target t over the edges
// cost accepts (A*: optimal for the default heuristic), or ok=false when t
// is unreachable. A start that already is a goal node gives an empty path.
func (p *Planner) Find(start nav.NodeID, t *Target, cost CostFunc) (Path, bool) {
	return p.search(start, t, cost, true)
}

// Dijkstra is Find without the heuristic (for tests and comparisons).
func (p *Planner) Dijkstra(start nav.NodeID, t *Target, cost CostFunc) (Path, bool) {
	return p.search(start, t, cost, false)
}

const noVia = -1

func (p *Planner) search(start nav.NodeID, t *Target, cost CostFunc, astar bool) (Path, bool) {
	g := p.g
	if g.Node(start) == nil || t == nil || t.Empty() {
		return Path{Start: start}, false
	}
	p.gen++
	if p.gen == 0 { // wrapped: clear the stamps
		for i := range p.stamp {
			p.stamp[i] = 0
		}
		p.gen = 1
	}
	h := func(id nav.NodeID) float32 {
		if !astar {
			return 0
		}
		return p.Heuristic(id, t)
	}
	p.heap = p.heap[:0]
	p.set(start, 0, noVia)
	p.heap.push(item{f: h(start), g: 0, node: int32(start), edge: noVia})
	best := float32(math.Inf(1))
	bestEdge := int32(noVia)
	expanded := 0
	for len(p.heap) > 0 {
		it := p.heap.pop()
		if it.node < 0 { // the virtual goal reached through a goal edge
			if it.edge == bestEdge && it.g == best {
				return p.path(start, int(bestEdge), best, true, expanded), true
			}
			continue
		}
		u := nav.NodeID(it.node)
		if it.g > p.dist[u] {
			continue // stale entry
		}
		if t.Node(u) {
			if it.g <= best {
				return p.path(start, int(p.via[u]), it.g, false, expanded), true
			}
		}
		if it.f >= best {
			// everything left costs at least as much as the goal edge found
			continue
		}
		expanded++
		lo, hi := g.OutRange(u)
		for i := lo; i < hi; i++ {
			e := &g.Edges[i]
			c, ok := cost(i, e)
			if !ok {
				continue
			}
			nd := it.g + c
			if t.Edge(i) && nd < best {
				best, bestEdge = nd, int32(i)
				p.heap.push(item{f: nd, g: nd, node: -1, edge: int32(i)})
			}
			v := e.To
			if p.stamp[v] == p.gen && nd >= p.dist[v] {
				continue
			}
			p.set(v, nd, int32(i))
			p.heap.push(item{f: nd + h(v), g: nd, node: int32(v), edge: int32(i)})
		}
	}
	if bestEdge != noVia {
		return p.path(start, int(bestEdge), best, true, expanded), true
	}
	return Path{Start: start, Expanded: expanded}, false
}

func (p *Planner) set(id nav.NodeID, d float32, via int32) {
	p.stamp[id], p.dist[id], p.via[id] = p.gen, d, via
}

// path rebuilds the path ending with edge last (or at start when last is
// noVia).
func (p *Planner) path(start nav.NodeID, last int, cost float32, goalEdge bool, expanded int) Path {
	out := Path{Start: start, Cost: cost, GoalEdge: goalEdge, Expanded: expanded}
	for i := last; i != noVia; {
		out.Edges = append(out.Edges, i)
		from := p.g.Edges[i].From
		if from == start {
			break
		}
		i = int(p.via[from])
	}
	for l, r := 0, len(out.Edges)-1; l < r; l, r = l+1, r-1 {
		out.Edges[l], out.Edges[r] = out.Edges[r], out.Edges[l]
	}
	return out
}

// item is a search queue entry: node -1 is the virtual goal behind a goal
// edge.
type item struct {
	f, g float32
	node int32
	edge int32
}

// pqueue is a binary min-heap on f with deterministic ties (lower g
// second, so deeper entries of equal f come first, then node and edge).
type pqueue []item

func (q pqueue) less(i, j int) bool {
	a, b := &q[i], &q[j]
	if a.f != b.f {
		return a.f < b.f
	}
	if a.g != b.g {
		return a.g > b.g
	}
	if a.node != b.node {
		return a.node < b.node
	}
	return a.edge < b.edge
}

func (q *pqueue) push(it item) {
	*q = append(*q, it)
	h := *q
	for i := len(h) - 1; i > 0; {
		parent := (i - 1) / 2
		if !h.less(i, parent) {
			break
		}
		h[i], h[parent] = h[parent], h[i]
		i = parent
	}
}

func (q *pqueue) pop() item {
	h := *q
	top := h[0]
	last := len(h) - 1
	h[0] = h[last]
	h = h[:last]
	for i := 0; ; {
		l, r := 2*i+1, 2*i+2
		m := i
		if l < len(h) && h.less(l, m) {
			m = l
		}
		if r < len(h) && h.less(r, m) {
			m = r
		}
		if m == i {
			break
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
	*q = h
	return top
}
