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
	for cx := cellOf(p[0] - radius); cx <= cellOf(p[0]+radius); cx++ {
		for cy := cellOf(p[1] - radius); cy <= cellOf(p[1]+radius); cy++ {
			for _, id := range s.cells[cellKey(cx, cy)] {
				o := g.Nodes[id].Origin
				dx, dy, dz := o[0]-p[0], o[1]-p[1], (o[2]-p[2])*ZWeight
				if dx*dx+dy*dy > r2 || dz*dz > r2 {
					continue
				}
				out = append(out, Candidate{id, dx*dx + dy*dy + dz*dz})
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

// EffectEdges returns the indexes of the edges that set off entity ent.
func (g *Graph) EffectEdges(ent int) []int {
	var out []int
	for i := range g.Edges {
		if g.Edges[i].HasEffect(ent) {
			out = append(out, i)
		}
	}
	return out
}

// MoverNodes returns the nodes standing on blocker b (any pose when pose <
// 0).
func (g *Graph) MoverNodes(b int32, pose int) []NodeID {
	var out []NodeID
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Blocker == b && (pose < 0 || int(n.Pose) == pose) {
			out = append(out, NodeID(i))
		}
	}
	return out
}
