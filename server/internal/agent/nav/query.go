package nav

import (
	"math"
	"sort"
)

// cellSize is the spatial index cell (xy).
const cellSize = 64

type spatial struct {
	cells map[uint64][]NodeID
}

func cellKey(cx, cy int32) uint64 { return uint64(uint32(cx))<<32 | uint64(uint32(cy)) }

func cellOf(v float32) int32 { return int32(math.Floor(float64(v) / cellSize)) }

func (g *Graph) spatialIndex() *spatial {
	g.once.Do(func() {
		s := &spatial{cells: map[uint64][]NodeID{}}
		for i := range g.Nodes {
			o := g.Nodes[i].Origin
			k := cellKey(cellOf(o[0]), cellOf(o[1]))
			s.cells[k] = append(s.cells[k], NodeID(i))
		}
		g.index = s
	})
	return g.index
}

// ZWeight scales height differences in Localize: a node one floor down is
// much further than one a few steps away on the same floor.
const ZWeight = 3

// Candidate is a node near a point with its weighted squared distance.
type Candidate struct {
	Node NodeID
	D2   float32
}

// Nearby returns the nodes within radius (horizontally, and ZWeight*|dz|
// within radius too) of p, nearest first (weighted distance, then id).
func (g *Graph) Nearby(p Vec3, radius float32) []Candidate {
	s := g.spatialIndex()
	r2 := radius * radius
	var out []Candidate
	add := func(ids []NodeID) {
		for _, id := range ids {
			o := g.Nodes[id].Origin
			dx, dy, dz := o[0]-p[0], o[1]-p[1], (o[2]-p[2])*ZWeight
			if dx*dx+dy*dy > r2 || dz*dz > r2 {
				continue
			}
			out = append(out, Candidate{id, dx*dx + dy*dy + dz*dz})
		}
	}
	x0, x1, y0, y1 := cellOf(p[0]-radius), cellOf(p[0]+radius), cellOf(p[1]-radius), cellOf(p[1]+radius)
	if span := (int64(x1) - int64(x0) + 1) * (int64(y1) - int64(y0) + 1); span > int64(len(s.cells)) || !finite(radius) {
		for _, ids := range s.cells { // a radius larger than the level: scan it all
			add(ids)
		}
	} else {
		for cx := x0; cx <= x1; cx++ {
			for cy := y0; cy <= y1; cy++ {
				add(s.cells[cellKey(cx, cy)])
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].D2 != out[j].D2 {
			return out[i].D2 < out[j].D2
		}
		return out[i].Node < out[j].Node
	})
	return out
}

// Localize returns the node nearest to p (weighted distance) within
// radius, or NoNode. Callers that can trace should prefer the first of
// Nearby with a clear line.
func (g *Graph) Localize(p Vec3, radius float32) NodeID {
	if c := g.Nearby(p, radius); len(c) > 0 {
		return c[0].Node
	}
	return NoNode
}

// EdgeIndex returns the index in Edges of the first edge from -> to (any
// kind), or -1.
func (g *Graph) EdgeIndex(from, to NodeID) int {
	lo, hi := g.OutRange(from)
	for i := lo; i < hi; i++ {
		if g.Edges[i].To == to {
			return i
		}
	}
	return -1
}

// LocalizeIn is Localize restricted to the nodes the player can be on with
// the blockers in states (a mask per blocker, as SpawnStates returns; nil:
// any state): a node on a mover only when the mover can be at that pose.
// ok, when set, rejects further nodes (for example end nodes, or nodes
// without a clear line from p).
func (g *Graph) LocalizeIn(p Vec3, radius float32, states []StateMask, ok func(id NodeID, n *Node) bool) NodeID {
	for _, c := range g.Nearby(p, radius) {
		n := &g.Nodes[c.Node]
		if n.Blocker >= 0 && states != nil && (int(n.Blocker) >= len(states) || states[n.Blocker]&Pose(int(n.Pose)) == 0) {
			continue
		}
		if ok != nil && !ok(c.Node, n) {
			continue
		}
		return c.Node
	}
	return NoNode
}

// lookups are the per-entity and per-blocker indexes, built on first use.
type lookups struct {
	effects   map[int32][]int    // entity -> edges with an effect on it
	movers    map[int32][]NodeID // blocker -> nodes on it
	blockerOf map[int32]int32    // entity -> blocker
}

func (g *Graph) lookup() *lookups {
	g.lookOnce.Do(func() {
		l := &lookups{effects: map[int32][]int{}, movers: map[int32][]NodeID{}, blockerOf: map[int32]int32{}}
		for i := range g.Edges {
			e := &g.Edges[i]
			for k := range e.Effects {
				ent := e.Effects[k].Entity
				if list := l.effects[ent]; len(list) == 0 || list[len(list)-1] != i {
					l.effects[ent] = append(list, i)
				}
			}
		}
		for i := range g.Nodes {
			if b := g.Nodes[i].Blocker; b >= 0 {
				l.movers[b] = append(l.movers[b], NodeID(i))
			}
		}
		for i := len(g.Blockers) - 1; i >= 0; i-- { // the first blocker of an entity wins
			l.blockerOf[g.Blockers[i].Entity] = int32(i)
		}
		g.looks = l
	})
	return g.looks
}

// EffectEdges returns the indexes of the edges that set off entity ent (in
// increasing order; the slice is shared, do not modify it).
func (g *Graph) EffectEdges(ent int) []int { return g.lookup().effects[int32(ent)] }

// MoverNodes returns the nodes standing on blocker b (any pose when pose <
// 0).
func (g *Graph) MoverNodes(b int32, pose int) []NodeID {
	var out []NodeID
	for _, id := range g.lookup().movers[b] {
		if pose < 0 || int(g.Nodes[id].Pose) == pose {
			out = append(out, id)
		}
	}
	return out
}
