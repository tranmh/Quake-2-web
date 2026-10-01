// Package nav holds the agent's navigation graph of a level: nodes where a
// player can stand (or swim, or hang on a ladder), directed edges validated
// by simulating the bit-exact pmove with the navsim executors, the
// conditions an edge needs (a door open, a laser off, a plat at its top)
// and the effects it has (triggers entered, buttons pressed, items picked
// up). navbuild builds a Graph from a BSP; Store caches it as gzipped JSON
// under assets/nav.
//
// A Graph is built once, ignoring skill: blockers and effect entities carry
// the skills they spawn at, and ForSkill (applied by Store.Load) resolves
// the conditions for one skill. A Graph is immutable after construction
// and safe for concurrent use.
package nav

import (
	"fmt"
	"math"
	"sync"

	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// FormatVersion is the version of the cache file layout and semantics.
const FormatVersion = 1

// BuildVersion is bumped whenever the builder (navbuild) produces a
// different graph for the same input; it is part of the cache key.
const BuildVersion = 1

// AllSkills is the skill mask of something present at every skill.
const AllSkills = 0x0f

// NodeID indexes Graph.Nodes.
type NodeID int32

// NoNode is the invalid node id.
const NoNode NodeID = -1

// NodeFlags describe a node.
type NodeFlags uint16

const (
	// NodeCrouch: only the ducked hull fits; the player is ducked there.
	NodeCrouch NodeFlags = 1 << iota
	// NodeWater: a swimming position (water level 2 or 3).
	NodeWater
	// NodeBreath: in water with the head out (water level < 3).
	NodeBreath
	// NodeLadder: hanging on a ladder.
	NodeLadder
	// NodeMover: standing on mover Blocker at pose Pose.
	NodeMover
	// NodeLedge: next to a drop-off.
	NodeLedge
	// NodeSpawn: where a spawn point puts the player.
	NodeSpawn
	// NodeEnd: the resting point of a touch edge (in front of a button).
	NodeEnd
)

var nodeFlagNames = [...]string{"crouch", "water", "breath", "ladder", "mover", "ledge", "spawn", "end"}

// String lists the flags ("water|breath").
func (f NodeFlags) String() string { return flagString(uint32(f), nodeFlagNames[:]) }

func flagString(f uint32, names []string) string {
	s := ""
	for i, n := range names {
		if f&(1<<i) != 0 {
			if s != "" {
				s += "|"
			}
			s += n
		}
	}
	return s
}

// Node is a place the player can be.
type Node struct {
	// Origin is the player origin there, on the 1/8 unit pmove grid.
	Origin Vec3
	Flags  NodeFlags
	// Blocker and Pose say which mover the node stands on (NodeMover);
	// Blocker is -1 otherwise.
	Blocker int32
	Pose    int8
	// Region groups nearby nodes connected by walking (greedy flood fill).
	Region int32
	// Yaw faces the ladder (NodeLadder).
	Yaw float32

	first, count int32 // out edges: Graph.Edges[first : first+count]
}

// OnMover reports whether the node stands on a mover.
func (n *Node) OnMover() bool { return n.Flags&NodeMover != 0 }

// EdgeKind is how an edge is traversed.
type EdgeKind uint8

const (
	EdgeWalk EdgeKind = iota + 1
	EdgeCrouch
	EdgeJump
	EdgeDrop
	EdgeLadder
	EdgeSwim
	EdgeWaterJump
	// EdgeRide: stay on a mover while it moves from one pose to another.
	EdgeRide
	// EdgeTeleport: walk into a teleporter.
	EdgeTeleport
	// EdgeTouch: walk at Aim until the player touches a button (or enters
	// a small trigger); To is where that leaves the player.
	EdgeTouch
)

var edgeKindNames = [...]string{"", "walk", "crouch", "jump", "drop", "ladder", "swim", "waterjump", "ride", "teleport", "touch"}

// String returns the kind name.
func (k EdgeKind) String() string {
	if int(k) < len(edgeKindNames) && k > 0 {
		return edgeKindNames[k]
	}
	return "?"
}

// EdgeFlags qualify an edge.
type EdgeFlags uint8

const (
	// EdgeFast: validated by the straight-line fast path (a clear hull
	// trace with ground under it) rather than simulated; property tests
	// re-simulate a sample of these.
	EdgeFast EdgeFlags = 1 << iota
	// EdgeStep: the fast path stepped up (+18).
	EdgeStep
	// EdgeBoard: starts off a mover and ends on one (boarding), or the
	// other way round (alighting), or between two movers.
	EdgeBoard
	// EdgeSpawnWorld: validated with the other movers in their spawn state
	// (closed doors) instead of out of the way; see Graph.EdgeWorld.
	EdgeSpawnWorld
)

var edgeFlagNames = [...]string{"fast", "step", "board", "spawnworld"}

// String lists the flags.
func (f EdgeFlags) String() string { return flagString(uint32(f), edgeFlagNames[:]) }

// StateMask is a set of blocker states: bit i is pose i, StateGone the
// state where the blocker is not there (destroyed, removed, switched off,
// not spawned yet).
type StateMask uint32

// StateGone is the "not there" state.
const StateGone StateMask = 1 << 31

// MaxPoses bounds the poses of one blocker.
const MaxPoses = 31

// Pose returns the mask of pose i.
func Pose(i int) StateMask { return 1 << uint(i) }

// Req is a condition: blocker Blocker must be in one of States.
type Req struct {
	Blocker int32
	States  StateMask
}

// EffectKind is what an edge sets off.
type EffectKind uint8

const (
	// EffTrigger: enters a trigger_* volume (Entity is the trigger).
	EffTrigger EffectKind = iota + 1
	// EffDoorTrigger: enters the trigger box a door team spawns
	// (Entity is the team master door).
	EffDoorTrigger
	// EffPlatTrigger: enters a plat's center trigger (Entity is the plat).
	EffPlatTrigger
	// EffButton: touches a button (Entity is the func_button).
	EffButton
	// EffItem: touches an item (Entity is the item). Pose is the pose of
	// the mover the item rests on (-1 when static).
	EffItem
)

var effectKindNames = [...]string{"", "trigger", "doortrigger", "plattrigger", "button", "item"}

// String returns the kind name.
func (k EffectKind) String() string {
	if int(k) < len(effectKindNames) && k > 0 {
		return effectKindNames[k]
	}
	return "?"
}

// Effect is something an edge sets off.
type Effect struct {
	Kind EffectKind
	// Entity is the lump index of what is set off.
	Entity int32
	// Yaw is the view yaw when entering (Touch_Multi's facing test for
	// directional triggers).
	Yaw float32
	// T is the seconds into the edge when it happens.
	T float32
	// Pose is the pose of the mover Blocker an item rests on: the effect
	// only happens with that blocker in that pose (-1: static item).
	Pose    int8
	Blocker int32
}

// Holds reports whether the effect can happen in the blocker states (an
// item on a mover needs the mover at its pose).
func (f *Effect) Holds(states []StateMask) bool {
	if f.Pose < 0 || f.Blocker < 0 {
		return true
	}
	return int(f.Blocker) < len(states) && states[f.Blocker]&Pose(int(f.Pose)) != 0
}

// Edge is a validated way from one node to another.
type Edge struct {
	From, To NodeID
	Kind     EdgeKind
	Recipe   navsim.Recipe
	Flags    EdgeFlags
	// Cost is the expected travel time in seconds: measured by the
	// simulation, estimated at full speed for fast-path edges, the mover's
	// travel time for rides.
	Cost float32
	// Aim is where the executor heads when it is not To's origin (touch
	// edges); zero otherwise.
	Aim Vec3
	// Takeoff is where a jump is pressed (EdgeJump), or where the player
	// left the ground (drops).
	Takeoff Vec3
	// TakeoffSpeed is the horizontal speed the simulation left the ground
	// with; a follower should not be slower for jumps.
	TakeoffSpeed float32
	// Forward is the plan's forwardmove override (0: recipe default).
	Forward int16
	// BackupMsec is a jump's run-up back-off.
	BackupMsec int16
	// Yaw is the ladder facing (EdgeLadder).
	Yaw float32
	// FallDamage is the falling damage the simulation took.
	FallDamage int16
	// Target is the lump index of what a touch edge walks at (a button, a
	// trigger, an item) or of a teleporter; 0 for other edges (worldspawn
	// is never a target).
	Target  int32
	Reqs    []Req
	Effects []Effect
}

// HasEffect reports whether the edge sets off entity ent (any kind).
func (e *Edge) HasEffect(ent int) bool {
	for i := range e.Effects {
		if int(e.Effects[i].Entity) == ent {
			return true
		}
	}
	return false
}

// Conditional reports whether the edge has conditions.
func (e *Edge) Conditional() bool { return len(e.Reqs) > 0 }

// BlockerKind classifies blockers.
type BlockerKind uint8

const (
	BlockDoor BlockerKind = iota + 1
	BlockRotating
	BlockSecret
	BlockPlat
	BlockTrain
	BlockWall
	BlockExplosive
	BlockLaser
	BlockWater
)

var blockerKindNames = [...]string{"", "door", "rotating", "secret", "plat", "train", "wall", "explosive", "laser", "water"}

// String returns the kind name.
func (k BlockerKind) String() string {
	if int(k) < len(blockerKindNames) && k > 0 {
		return blockerKindNames[k]
	}
	return "?"
}

// BlockerPose is one state of a blocker.
type BlockerPose struct {
	Name   string `json:"name"`
	Origin Vec3   `json:"origin"`
	Angles Vec3   `json:"angles,omitempty"`
}

// Blocker is an entity whose state decides whether an edge can be used: a
// mover (each pose a state), a removable wall or explosive, a laser (on =
// pose 0, off = Gone).
type Blocker struct {
	Entity int32
	Class  string
	Model  string
	Kind   BlockerKind
	Poses  []BlockerPose
	// Gone: the blocker can disappear (StateGone is one of its states).
	Gone bool
	// Spawn is the state it starts in: a pose index, or -1 for Gone.
	Spawn int8
	// Skills is the mask of skills (bit s) it spawns at.
	Skills uint8
	// Laser segment (BlockLaser).
	Start, End Vec3
	// Headnode, Mins and Maxs are the inline model's collision headnode and
	// bounds (brush blockers).
	Headnode   int32
	Mins, Maxs Vec3
	// Solid: it blocks movement at its poses (not water, not a laser).
	Solid bool
}

// Volume is a box whose entry is an effect: a trigger (absmin/absmax), a
// door team's or plat's trigger, an item's touch box (one per pose of the
// mover Blocker it rests on).
type Volume struct {
	Kind     EffectKind
	Entity   int32
	Pose     int8
	Blocker  int32
	Min, Max Vec3
}

// All returns the mask of every state of the blocker.
func (b *Blocker) All() StateMask {
	m := StateMask(1)<<uint(len(b.Poses)) - 1
	if b.Gone {
		m |= StateGone
	}
	return m
}

// SpawnState returns the mask of the spawn state.
func (b *Blocker) SpawnState() StateMask {
	if b.Spawn < 0 {
		return StateGone
	}
	return Pose(int(b.Spawn))
}

// Ent is an entity edges refer to in their effects, with its skill mask.
type Ent struct {
	Entity int32
	Class  string
	Model  string
	Skills uint8
}

// Spawn is an info_player_start and the node it puts the player on.
type Spawn struct {
	Entity     int32
	Targetname string
	Origin     Vec3
	Node       NodeID
	Skills     uint8
}

// Stats are counts of a graph (no timings, so files stay byte-stable).
type Stats struct {
	Nodes, Edges, Conditional, Fast int
	ByKind                          map[string]int
}

// Graph is the navigation graph of one map.
type Graph struct {
	Format      int
	Map         string
	Checksum    uint32
	PhysicsHash string
	Params      Params
	// Skill is the skill ForSkill resolved the conditions for, or -1 for
	// the skill-independent graph a build produces.
	Skill    int
	Nodes    []Node
	Edges    []Edge // grouped by From, sorted by (From, To, Kind)
	Blockers []Blocker
	Ents     []Ent
	Spawns   []Spawn
	// Solids are the static solids every edge was validated with (brush
	// entities that never move, barrels dropped to the floor, ...).
	Solids []navsim.Solid
	// Volumes, Pushes and Teleports are what the runner reports and applies.
	Volumes   []Volume
	Pushes    []navsim.Push
	Teleports []navsim.Teleport

	once  sync.Once
	index *spatial
	inMu  sync.Once
	in    [][]int32
}

// New assembles a graph from nodes and edges (sorted by From as Finish
// requires) and indexes it.
func New(nodes []Node, edges []Edge) (*Graph, error) {
	g := &Graph{Format: FormatVersion, Skill: -1, Nodes: nodes, Edges: edges}
	if err := g.Finish(); err != nil {
		return nil, err
	}
	return g, nil
}

// Finish checks the graph and computes the per-node edge ranges. Edges must
// be grouped by From in increasing order.
func (g *Graph) Finish() error {
	n := int32(len(g.Nodes))
	for i := range g.Nodes {
		g.Nodes[i].first, g.Nodes[i].count = 0, 0
	}
	prev := int32(-1)
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.From < 0 || int32(e.From) >= n || e.To < 0 || int32(e.To) >= n {
			return fmt.Errorf("nav: edge %d: node out of range (%d -> %d of %d)", i, e.From, e.To, n)
		}
		if int32(e.From) < prev {
			return fmt.Errorf("nav: edge %d: edges not grouped by From", i)
		}
		if int32(e.From) != prev {
			g.Nodes[e.From].first = int32(i)
			prev = int32(e.From)
		}
		g.Nodes[e.From].count++
		for _, r := range e.Reqs {
			if r.Blocker < 0 || int(r.Blocker) >= len(g.Blockers) {
				return fmt.Errorf("nav: edge %d: blocker %d out of range", i, r.Blocker)
			}
		}
		if !finite(e.Cost) || e.Cost < 0 {
			return fmt.Errorf("nav: edge %d: bad cost %v", i, e.Cost)
		}
	}
	for i := range g.Nodes {
		nd := &g.Nodes[i]
		if nd.Blocker < -1 || int(nd.Blocker) >= len(g.Blockers) {
			return fmt.Errorf("nav: node %d: blocker %d out of range", i, nd.Blocker)
		}
		if nd.OnMover() != (nd.Blocker >= 0) {
			return fmt.Errorf("nav: node %d: mover flag and blocker %d disagree", i, nd.Blocker)
		}
		if nd.Blocker >= 0 && (nd.Pose < 0 || int(nd.Pose) >= len(g.Blockers[nd.Blocker].Poses)) {
			return fmt.Errorf("nav: node %d: pose %d out of range", i, nd.Pose)
		}
		for k := 0; k < 3; k++ {
			if !finite(nd.Origin[k]) {
				return fmt.Errorf("nav: node %d: bad origin", i)
			}
		}
	}
	for i := range g.Blockers {
		if len(g.Blockers[i].Poses) > MaxPoses {
			return fmt.Errorf("nav: blocker %d: %d poses", i, len(g.Blockers[i].Poses))
		}
	}
	for i := range g.Spawns {
		if s := g.Spawns[i].Node; s < NoNode || int32(s) >= n {
			return fmt.Errorf("nav: spawn %d: node %d out of range", i, s)
		}
	}
	return nil
}

func finite(f float32) bool { return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0) }

// Node returns node id (nil when out of range).
func (g *Graph) Node(id NodeID) *Node {
	if id < 0 || int(id) >= len(g.Nodes) {
		return nil
	}
	return &g.Nodes[id]
}

// Out returns the out edges of node id.
func (g *Graph) Out(id NodeID) []Edge {
	n := g.Node(id)
	if n == nil {
		return nil
	}
	return g.Edges[n.first : n.first+n.count]
}

// OutRange returns the indexes [lo, hi) of id's out edges in Edges.
func (g *Graph) OutRange(id NodeID) (lo, hi int) {
	n := g.Node(id)
	if n == nil {
		return 0, 0
	}
	return int(n.first), int(n.first + n.count)
}

// In returns the indexes (into Edges) of the edges ending at id.
func (g *Graph) In(id NodeID) []int32 {
	g.inMu.Do(func() {
		g.in = make([][]int32, len(g.Nodes))
		for i := range g.Edges {
			t := g.Edges[i].To
			g.in[t] = append(g.in[t], int32(i))
		}
	})
	if id < 0 || int(id) >= len(g.in) {
		return nil
	}
	return g.in[id]
}

// Neighbors returns the distinct nodes id has an edge to, in edge order.
func (g *Graph) Neighbors(id NodeID) []NodeID {
	var out []NodeID
	for _, e := range g.Out(id) {
		dup := false
		for _, n := range out {
			if n == e.To {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, e.To)
		}
	}
	return out
}

// Plan returns the navsim plan that executes edge e.
func (g *Graph) Plan(e *Edge) navsim.Plan {
	p := navsim.Plan{Recipe: e.Recipe, Takeoff: e.Takeoff, Forward: e.Forward, BackupMsec: int(e.BackupMsec), Yaw: e.Yaw, StepMsec: g.Params.StepMsec}
	if f := g.Node(e.From); f != nil {
		p.From = f.Origin
	}
	p.Target = e.Aim
	if e.Aim == (Vec3{}) {
		if t := g.Node(e.To); t != nil {
			p.Target = t.Origin
		}
	}
	return p
}

// ArriveMode returns how arrival at node id is judged.
func (g *Graph) ArriveMode(id NodeID) navsim.ArriveMode {
	n := g.Node(id)
	switch {
	case n == nil:
		return navsim.ArriveGround
	case n.Flags&NodeLadder != 0:
		return navsim.ArriveAny
	case n.Flags&NodeWater != 0:
		return navsim.ArriveWater
	}
	return navsim.ArriveGround
}

// BlockerOf returns the index of the blocker for lump entity ent, or -1.
func (g *Graph) BlockerOf(ent int) int32 {
	for i := range g.Blockers {
		if int(g.Blockers[i].Entity) == ent {
			return int32(i)
		}
	}
	return -1
}

// SpawnStates returns every blocker's spawn state (the level as it starts).
func (g *Graph) SpawnStates() []StateMask {
	s := make([]StateMask, len(g.Blockers))
	for i := range g.Blockers {
		s[i] = g.Blockers[i].SpawnState()
	}
	return s
}

// Holds reports whether all of e's conditions hold for the blocker states
// (a mask per blocker, as SpawnStates returns; a set bit means "possibly in
// that state").
func Holds(e *Edge, states []StateMask) bool {
	for _, r := range e.Reqs {
		if int(r.Blocker) >= len(states) || states[r.Blocker]&r.States == 0 {
			return false
		}
	}
	return true
}

// Stats counts the graph.
func (g *Graph) Stats() Stats {
	s := Stats{Nodes: len(g.Nodes), Edges: len(g.Edges), ByKind: map[string]int{}}
	for i := range g.Edges {
		e := &g.Edges[i]
		s.ByKind[e.Kind.String()]++
		if len(e.Reqs) > 0 {
			s.Conditional++
		}
		if e.Flags&EdgeFast != 0 {
			s.Fast++
		}
	}
	return s
}

// Reachable returns which nodes can be reached from the start nodes over
// the edges ok accepts (nil: all edges).
func (g *Graph) Reachable(from []NodeID, ok func(e *Edge) bool) []bool {
	seen := make([]bool, len(g.Nodes))
	stack := make([]NodeID, 0, 64)
	for _, f := range from {
		if f >= 0 && int(f) < len(seen) && !seen[f] {
			seen[f] = true
			stack = append(stack, f)
		}
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out := g.Out(n)
		for i := range out {
			e := &out[i]
			if seen[e.To] || (ok != nil && !ok(e)) {
				continue
			}
			seen[e.To] = true
			stack = append(stack, e.To)
		}
	}
	return seen
}

// ForSkill resolves the skill-dependent parts for one skill: a blocker that
// does not spawn at that skill counts as Gone (conditions allowing Gone
// hold and are dropped; edges needing it there are removed), and effects on
// entities that do not spawn are dropped. Node ids are unchanged. A graph
// that already has a skill is returned as is when the skill matches.
func (g *Graph) ForSkill(skill int) *Graph {
	if g.Skill == skill {
		return g
	}
	bit := uint8(1) << uint(skill&3)
	absent := make([]bool, len(g.Blockers))
	for i := range g.Blockers {
		absent[i] = g.Blockers[i].Skills&bit == 0
	}
	entAbsent := map[int32]bool{}
	for _, e := range g.Ents {
		if e.Skills&bit == 0 {
			entAbsent[e.Entity] = true
		}
	}
	out := &Graph{
		Format: g.Format, Map: g.Map, Checksum: g.Checksum, PhysicsHash: g.PhysicsHash, Params: g.Params,
		Skill: skill, Blockers: g.Blockers, Ents: g.Ents, Solids: g.Solids, Volumes: g.Volumes, Pushes: g.Pushes, Teleports: g.Teleports,
	}
	out.Nodes = append([]Node(nil), g.Nodes...)
	out.Edges = make([]Edge, 0, len(g.Edges))
edges:
	for i := range g.Edges {
		e := g.Edges[i]
		if len(e.Reqs) > 0 {
			var reqs []Req
			for _, r := range e.Reqs {
				if absent[r.Blocker] {
					if r.States&StateGone == 0 {
						continue edges
					}
					continue
				}
				reqs = append(reqs, r)
			}
			e.Reqs = reqs
		}
		if fn, tn := &g.Nodes[e.From], &g.Nodes[e.To]; (fn.Blocker >= 0 && absent[fn.Blocker]) || (tn.Blocker >= 0 && absent[tn.Blocker]) {
			continue
		}
		if e.Target != 0 && entAbsent[e.Target] {
			continue // a touch edge whose target does not spawn
		}
		if len(e.Effects) > 0 {
			var eff []Effect
			for _, f := range e.Effects {
				if !entAbsent[f.Entity] && (f.Blocker < 0 || !absent[f.Blocker]) {
					eff = append(eff, f)
				}
			}
			e.Effects = eff
		}
		out.Edges = append(out.Edges, e)
	}
	for _, s := range g.Spawns {
		if s.Skills&bit != 0 {
			out.Spawns = append(out.Spawns, s)
		}
	}
	if err := out.Finish(); err != nil {
		panic("nav: ForSkill broke a valid graph: " + err.Error())
	}
	return out
}
