package navrt

import (
	"fmt"
	"math"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// GoalKind is what a goal asks for.
type GoalKind uint8

const (
	// GoalNodes: be at one of Nodes (navsim.Arrived at its origin).
	GoalNodes GoalKind = iota + 1
	// GoalPoint: stand with the origin within Radius of Point
	// horizontally (and within max(Radius, PointZ) vertically).
	GoalPoint
	// GoalVolume: touch the box Min..Max with the player box.
	GoalVolume
	// GoalTouch: set off entity Entity by touch: enter its trigger, press
	// its button. Any edge with that effect satisfies the goal, wherever
	// it ends: also a ride that carries the bot into a trigger or a drop
	// through a trigger in mid-air.
	GoalTouch
	// GoalItem: pick up item Entity (an edge with its item effect).
	GoalItem
	// GoalShoot: shoot entity Entity (a shootable button) at Point from a
	// spot with a clear shot within ShootRange; reached once the target
	// is seen moving.
	GoalShoot
)

// PointZ is the vertical tolerance of a point goal with a small radius.
const PointZ = 40

// ShootRange bounds the distance of a shot at a shoot goal's target.
const ShootRange = 768

func goalKindNames() [7]string {
	return [7]string{"", "nodes", "point", "volume", "touch", "item", "shoot"}
}

// String returns the kind name.
func (k GoalKind) String() string {
	if n := goalKindNames(); int(k) < len(n) && k > 0 {
		return n[k]
	}
	return "?"
}

// Goal is where the navigator should take the bot.
type Goal struct {
	Kind     GoalKind
	Nodes    []nav.NodeID
	Point    Vec3
	Radius   float32
	Min, Max Vec3
	// Entity is the lump index of what a touch, item or shoot goal is about.
	Entity int32
}

// NodeGoal is a goal of being at one of nodes.
func NodeGoal(nodes ...nav.NodeID) Goal { return Goal{Kind: GoalNodes, Nodes: nodes} }

// PointGoal is a goal of standing within r of p.
func PointGoal(p Vec3, r float32) Goal { return Goal{Kind: GoalPoint, Point: p, Radius: r} }

// VolumeGoal is a goal of touching the box min..max.
func VolumeGoal(min, max Vec3) Goal { return Goal{Kind: GoalVolume, Min: min, Max: max} }

// TouchGoal is a goal of setting off entity ent by touching it.
func TouchGoal(ent int32) Goal { return Goal{Kind: GoalTouch, Entity: ent} }

// ItemGoal is a goal of picking up item entity ent.
func ItemGoal(ent int32) Goal { return Goal{Kind: GoalItem, Entity: ent} }

// ShootGoal is a goal of shooting entity ent, aiming at at.
func ShootGoal(ent int32, at Vec3) Goal { return Goal{Kind: GoalShoot, Entity: ent, Point: at} }

// String describes the goal.
func (g Goal) String() string {
	switch g.Kind {
	case GoalNodes:
		if len(g.Nodes) == 1 {
			return fmt.Sprintf("node %d", g.Nodes[0])
		}
		return fmt.Sprintf("%d nodes", len(g.Nodes))
	case GoalPoint:
		return fmt.Sprintf("point %v r %g", g.Point, g.Radius)
	case GoalVolume:
		return fmt.Sprintf("volume %v..%v", g.Min, g.Max)
	case GoalTouch, GoalItem:
		return fmt.Sprintf("%s #%d", g.Kind, g.Entity)
	case GoalShoot:
		return fmt.Sprintf("shoot #%d at %v", g.Entity, g.Point)
	}
	return "no goal"
}

// Target is a goal compiled for the planner: the nodes that are goals and
// the edges that satisfy it by their effects, plus the box the heuristic
// measures to.
type Target struct {
	nodes  []bool
	edges  map[int]bool
	lo, hi Vec3
	n      int
}

// NewTarget returns an empty target for graph g.
func NewTarget(g *nav.Graph) *Target {
	return &Target{nodes: make([]bool, len(g.Nodes)), edges: map[int]bool{}}
}

// AddNode makes node id a goal.
func (t *Target) AddNode(g *nav.Graph, id nav.NodeID) {
	if n := g.Node(id); n != nil && !t.nodes[id] {
		t.nodes[id] = true
		t.grow(n.Origin)
	}
}

// AddEdge makes reaching the end of edge i (setting its effects off) a
// goal.
func (t *Target) AddEdge(g *nav.Graph, i int) {
	if i < 0 || i >= len(g.Edges) || t.edges[i] {
		return
	}
	t.edges[i] = true
	t.grow(g.Nodes[g.Edges[i].From].Origin)
}

func (t *Target) grow(p Vec3) {
	if t.n == 0 {
		t.lo, t.hi = p, p
	}
	for c := 0; c < 3; c++ {
		t.lo[c], t.hi[c] = min(t.lo[c], p[c]), max(t.hi[c], p[c])
	}
	t.n++
}

// Empty reports a target nothing satisfies.
func (t *Target) Empty() bool { return t == nil || t.n == 0 }

// Node reports whether node id is a goal.
func (t *Target) Node(id nav.NodeID) bool {
	return t != nil && id >= 0 && int(id) < len(t.nodes) && t.nodes[id]
}

// Edge reports whether edge i satisfies the goal.
func (t *Target) Edge(i int) bool { return t != nil && t.edges[i] }

// Nodes returns the goal nodes.
func (t *Target) Nodes() []nav.NodeID {
	var out []nav.NodeID
	for i, ok := range t.nodes {
		if ok {
			out = append(out, nav.NodeID(i))
		}
	}
	return out
}

// HorizontalSpeed bounds the speed the heuristic assumes (units per
// second): pmove caps the ground speed at 300.
const HorizontalSpeed = 320

// distance returns the horizontal distance from p to the target's box.
func (t *Target) distance(p Vec3) float32 {
	var d2 float64
	for c := 0; c < 2; c++ {
		var d float32
		switch {
		case p[c] < t.lo[c]:
			d = t.lo[c] - p[c]
		case p[c] > t.hi[c]:
			d = p[c] - t.hi[c]
		}
		d2 += float64(d) * float64(d)
	}
	return float32(math.Sqrt(d2))
}

// climb returns how far below the target's box p is.
func (t *Target) climb(p Vec3) float32 { return max(0, t.lo[2]-p[2]) }

// CompileTarget compiles a goal of a kind other than GoalShoot (which needs
// line-of-sight tests: Navigator does it) for graph g. Touch and item
// goals take every edge with an effect on the entity whose effect can
// happen in the states the bot may meet (an item on a mover only in its
// pose); a node standing inside one of the entity's volumes counts too.
func CompileTarget(g *nav.Graph, goal Goal) *Target {
	t := NewTarget(g)
	switch goal.Kind {
	case GoalNodes:
		for _, id := range goal.Nodes {
			t.AddNode(g, id)
		}
	case GoalPoint:
		r := max(goal.Radius, 1)
		for _, c := range g.Nearby(goal.Point, r+max(r, PointZ)*nav.ZWeight) {
			if pointReached(g.Nodes[c.Node].Origin, goal) {
				t.AddNode(g, c.Node)
			}
		}
	case GoalVolume:
		for i := range g.Nodes {
			n := &g.Nodes[i]
			if boxTouch(n.Origin, n.Flags&nav.NodeCrouch != 0, goal.Min, goal.Max) {
				t.AddNode(g, nav.NodeID(i))
			}
		}
	case GoalTouch, GoalItem:
		for _, i := range g.EffectEdges(int(goal.Entity)) {
			e := &g.Edges[i]
			for _, f := range e.Effects {
				if f.Entity == goal.Entity && (goal.Kind == GoalTouch || f.Kind == nav.EffItem) {
					t.AddEdge(g, i)
					break
				}
			}
		}
		for vi := range g.Volumes {
			v := &g.Volumes[vi]
			if v.Entity != goal.Entity || v.Blocker >= 0 || (goal.Kind == GoalItem && v.Kind != nav.EffItem) {
				continue
			}
			for _, c := range g.Nearby(boxCenter(v.Min, v.Max), boxRadius(v.Min, v.Max)+32) {
				n := &g.Nodes[c.Node]
				if boxTouch(n.Origin, n.Flags&nav.NodeCrouch != 0, v.Min, v.Max) {
					t.AddNode(g, c.Node)
				}
			}
		}
	}
	return t
}

// pointReached reports whether origin o satisfies point goal g.
func pointReached(o Vec3, g Goal) bool {
	dx, dy := float64(o[0]-g.Point[0]), float64(o[1]-g.Point[1])
	return dx*dx+dy*dy <= float64(g.Radius)*float64(g.Radius) && abs32(o[2]-g.Point[2]) <= max(g.Radius, PointZ)
}

// boxTouch reports whether the player box at origin o (link box grown by
// 1, as G_TouchTriggers tests) touches min..max.
func boxTouch(o Vec3, ducked bool, lo, hi Vec3) bool {
	mins, maxs := navsim.StandMins(), navsim.StandMaxs()
	if ducked {
		maxs = navsim.DuckMaxs()
	}
	for c := 0; c < 3; c++ {
		if o[c]+mins[c]-1 > hi[c] || o[c]+maxs[c]+1 < lo[c] {
			return false
		}
	}
	return true
}

func boxCenter(lo, hi Vec3) Vec3 {
	return Vec3{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2}
}

func boxRadius(lo, hi Vec3) float32 {
	return float32(math.Hypot(float64(hi[0]-lo[0]), float64(hi[1]-lo[1]))) / 2
}
