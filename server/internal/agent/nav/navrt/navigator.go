package navrt

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
)

// Defaults of Config.
const (
	DefaultRepath         = 2000 // ms
	DefaultLookahead      = 128  // units
	DefaultFragilePenalty = 10   // s
	DefaultDamageCost     = 0.2  // s per hit point
	DefaultButtonPenalty  = 3    // s
	// StopCost is added to edges that start with a stop at their start
	// node (EdgeFromRest, and the jump, drop and ladder recipes, which
	// stop there themselves): the edge costs were measured from rest.
	StopCost = 0.75 // s
	// SwimStartCost is added to swim and water jump edges: the follower
	// swims to their (underwater) start first, and a water jump waits
	// there until the water has slowed the bot down (they were validated
	// from rest and the builder checks no running entry into them).
	SwimStartCost = 0.5 // s
	// OccupiedCost is added to an edge that ends where a visible monster
	// (or player) stands: the planner prefers a way around it.
	OccupiedCost = 2 // s
	// RememberedPenalty is added to edges an earlier attempt at the level
	// found blocked (the level memory's Blocked keys).
	RememberedPenalty = 5 // s
)

// CmdMsec is the command length the follower runs at (the sessions' and
// the builder's 25 ms).
const CmdMsec = 25

// Config configures a Navigator. The zero value uses the defaults.
type Config struct {
	// Avoid lists lump entities the bot must never set off (the route's
	// avoid list, exits it should not take): edges with an effect on one
	// of them, or walking at one, are left out of every path.
	Avoid []int32
	// RepathInterval is the period of routine repaths (ms; 0: 2000).
	RepathInterval int64
	// Lookahead bounds the pursuit lookahead along plain walks (units; 0:
	// 128).
	Lookahead float32
	// FragilePenalty is added to EdgeFragile edges (s; 0: 10).
	FragilePenalty float32
	// DamageCost is the seconds a hit point of expected damage (falls,
	// hazards) costs (0: 0.2).
	DamageCost float32
	// ButtonPenalty is added to an edge that presses a button the goal is
	// not about (s; 0: 3).
	ButtonPenalty float32
	// AllowNeedsUse plans rides that only start when something uses the
	// mover (the route executor presses the button first); otherwise
	// they are used only while the mover is seen moving.
	AllowNeedsUse bool
	// OnBlocked, when set, is called when the navigator marks an edge
	// blocked (to record EdgeKey in the level memory with
	// worldmodel.World.MarkBlocked).
	OnBlocked func(edge int, key string, until int64)
}

func (c *Config) defaults() {
	if c.RepathInterval <= 0 {
		c.RepathInterval = DefaultRepath
	}
	if c.Lookahead <= 0 {
		c.Lookahead = DefaultLookahead
	}
	if c.FragilePenalty <= 0 {
		c.FragilePenalty = DefaultFragilePenalty
	}
	if c.DamageCost <= 0 {
		c.DamageCost = DefaultDamageCost
	}
	if c.ButtonPenalty <= 0 {
		c.ButtonPenalty = DefaultButtonPenalty
	}
}

// FollowStatus is the state of the navigator.
type FollowStatus uint8

const (
	// Idle: no goal.
	Idle FollowStatus = iota
	// Following a path.
	Following
	// Waiting for a door, plat or other mover before the next edge.
	Waiting
	// Arrived: the goal is reached.
	Arrived
	// Stuck: recovering from a lack of progress (a manoeuvre or a repath).
	Stuck
	// Failed: no path, or recovery gave up (Status.Reason says why).
	Failed
	// OffGraph: not near any node; walking back to the graph.
	OffGraph
)

// String returns the status name.
func (s FollowStatus) String() string {
	switch s {
	case Following:
		return "following"
	case Waiting:
		return "waiting"
	case Arrived:
		return "arrived"
	case Stuck:
		return "stuck"
	case Failed:
		return "failed"
	case OffGraph:
		return "offgraph"
	}
	return "idle"
}

// Cause classifies what stopped the bot.
type Cause uint8

const (
	CauseNone Cause = iota
	// CauseEntity: a monster, player or other solid entity in the way.
	CauseEntity
	// CauseDoor: a door or other mover in the way, or one that did not
	// open.
	CauseDoor
	// CauseWorld: the level geometry (or an unknown reason).
	CauseWorld
	// CauseNoPath: the goal is unreachable in the believed state.
	CauseNoPath
	// CauseOffGraph: the bot is not near the graph.
	CauseOffGraph
)

// String returns the cause name.
func (c Cause) String() string {
	switch c {
	case CauseEntity:
		return "entity"
	case CauseDoor:
		return "door"
	case CauseWorld:
		return "world"
	case CauseNoPath:
		return "nopath"
	case CauseOffGraph:
		return "offgraph"
	}
	return "none"
}

// Status describes what the navigator is doing.
type Status struct {
	Follow FollowStatus
	// Cause and Reason explain Stuck, Failed, Waiting and OffGraph; they
	// are empty while following.
	Cause  Cause
	Reason string
	// LastCause and LastReason keep the latest stuck or failure report
	// for the current goal after the navigator went back to following.
	LastCause  Cause
	LastReason string
	// Node is the last node the bot was localized at (NoNode if none).
	Node nav.NodeID
	// Edge is the edge being run (-1 if none), Step its index in the path.
	Edge, Step int
	// Remaining is the planned time left (s).
	Remaining float32
	// Repaths counts plans made for the current goal, Stucks the times it
	// got stuck, Level the current escalation level.
	Repaths, Stucks, Level int
	// WaitFor is the blocker waited for (-1 if none).
	WaitFor int32
}

// TickInput is what the navigator decides one command from.
type TickInput struct {
	// Self is the bot's movement state when the command runs: the last
	// player state with the commands sent since replayed (Predictor).
	Self navsim.State
	// Belief is the world model's belief (nil: the level as it spawns).
	// The navigator folds it into its MapState when it changed.
	Belief *worldmodel.Belief
	// Now is the clock (ms, the belief's clock) when the command runs.
	Now int64
	// Last is the predicted result of the previous command (contacts,
	// volumes entered, teleports); nil when unknown.
	Last *navsim.StepResult
}

// trackBase is the first solid ID used for tracked monsters in the
// prediction world (lump indexes stay below it).
const trackBase = 1 << 20

// TrackMemory is how long (ms) a monster that went out of view and holds
// the bot up still counts as standing where it was last seen (in the
// prediction world) once the bot is off the ground: one the bot stands on
// is below its view, and counts while the bot stands.
const TrackMemory = 1000

// OccupiedMemory is how long (ms) a monster out of view still makes the
// planner route around where it was last seen.
const OccupiedMemory = 5000

// Navigator moves the bot towards a goal over the nav graph (see the
// package documentation). Use SetGoal, then call Tick once per usercmd.
// It is not safe for concurrent use.
type Navigator struct {
	g   *nav.Graph
	md  *mapdata.Map
	cfg Config
	ms  *MapState
	pl  *Planner
	bl  *Blocked
	// w is the prediction world (static solids, believed blockers,
	// visible monsters); static the same without the monsters, for the
	// line checks of localization and lookahead (a monster touching the
	// bot would make every trace start in solid).
	w      *navsim.World
	static *navsim.World

	avoid      map[int32]bool
	dirYaw     map[int32]float32  // directional trigger -> movedir yaw
	volsOf     map[int32][]int    // entity -> indexes into Graph.Volumes
	remembered map[int]bool       // edges an earlier attempt found blocked
	floor      map[*nav.Edge]bool // walk edges with floor under them (floored)
	landing    map[int]bool       // ledge check verdicts from rest (trial)
	sim        *navsim.Runner     // scratch runner of the flight checks
	inside     map[int]bool       // conditional edges that start inside their blocker (startsInside)
	probe      *navsim.World      // scratch world of startsInside
	memKeys    int
	keyIndex   map[string]int

	solids []navsim.Solid
	tracks []string // tracks[k] is the track ID of solid trackBase+k
	occ    []occupant
	// planOcc are the visible occupants on the path when it was planned
	// (sorted), occAt the last repath for a new one (if occRepathed).
	planOcc     []string
	occAt       int64
	occRepathed bool
	// blockedBy is the index (in occ) of the monster the last stuck
	// classification found in the way, -1 if none.
	blockedBy int
	// doorHit is the mover blocker it found in the way (-1 if none), and
	// doorWaited reports that the current step already waited for one.
	doorHit    int32
	doorWaited bool

	now          int64
	st           navsim.State
	belief       *worldmodel.Belief
	beliefFrames int
	beliefTime   int64
	lastOrigin   Vec3
	haveOrigin   bool

	goal    Goal
	hasGoal bool
	// arrived latches reaching the goal; arriveSpot is where (see
	// settle).
	arrived    bool
	arriveSpot Vec3
	target     *Target
	goalSince  int64
	gout       navsim.Outcome // contacts and volumes since the goal was set
	shootAt    int64

	path     Path
	rem      []float32
	cur      int
	ph       phase
	ex       navsim.Executor
	swimTo   bool // phaseStop swims to the edge start instead of stopping there
	airTicks int  // commands in the air during phaseStop
	flight   int  // commands in the air during phaseRun
	runStart int64
	deadline int64
	waitLim  int64
	waitFor  int32
	waitSpot Vec3
	out      navsim.Outcome // contacts and volumes of the current step
	hit      bool
	node     nav.NodeID

	// landed reports that the step before the current one was a jump or
	// a drop (see begin); airSince is when the bot last stood or swam.
	landed   bool
	airSince int64

	noLookahead int64 // no pursuit lookahead before this time (ms)
	gapAt       int64 // last relocalization for a pit in front of the bot

	lastPlan  int64
	replan    bool
	replanWhy string
	replanEv  bool
	offSince  int64
	final     bool

	stuck  stuckState
	status Status
}

// New returns a navigator over graph g (resolved for the level's skill)
// of map md: the same level's map data, whose collision model the
// navigator traces in and predicts with. Without md (or its CM) it can
// plan but not trace: every line counts as clear, there are no mover
// timings or directional triggers, and NewRunner must not be used. The
// level is believed as it spawns until the first belief arrives.
func New(g *nav.Graph, md *mapdata.Map, cfg Config) *Navigator {
	cfg.defaults()
	n := &Navigator{
		g: g, md: md, cfg: cfg,
		ms: NewMapState(g, md), pl: NewPlanner(g), bl: NewBlocked(),
		avoid: map[int32]bool{}, dirYaw: map[int32]float32{}, volsOf: map[int32][]int{},
		remembered: map[int]bool{}, node: nav.NoNode, waitFor: -1, blockedBy: -1, doorHit: -1,
	}
	if md != nil && md.CM != nil {
		n.w, n.static = navsim.NewWorld(md.CM), navsim.NewWorld(md.CM)
	}
	for _, a := range cfg.Avoid {
		n.avoid[a] = true
	}
	if md != nil {
		for i := range md.Triggers {
			if t := &md.Triggers[i]; t.Directional() {
				if y, ok := navsim.YawTo(Vec3{}, t.Movedir); ok {
					n.dirYaw[int32(t.Entity)] = y
				}
			}
		}
	}
	for vi := range g.Volumes {
		e := g.Volumes[vi].Entity
		n.volsOf[e] = append(n.volsOf[e], vi)
	}
	n.status = Status{Node: nav.NoNode, Edge: -1, WaitFor: -1}
	n.rebuildWorld(nil)
	return n
}

// Graph returns the navigator's graph.
func (n *Navigator) Graph() *nav.Graph { return n.g }

// MapState returns the belief about the level's blockers.
func (n *Navigator) MapState() *MapState { return n.ms }

// World returns the prediction world: the graph's static solids, the
// blockers as believed and the visible monsters (for a Predictor's
// runner). It changes with every belief.
func (n *Navigator) World() *navsim.World { return n.w }

// NewRunner returns a runner in the prediction world with the graph's
// physics, volumes and hooks (for NewPredictor).
func (n *Navigator) NewRunner() *navsim.Runner { return n.g.NewRunner(n.w) }

// Blocked returns the set of blocked edges.
func (n *Navigator) Blocked() *Blocked { return n.bl }

// Status returns what the navigator is doing.
func (n *Navigator) Status() Status { return n.status }

// Goal returns the current goal (ok=false without one).
func (n *Navigator) Goal() (Goal, bool) { return n.goal, n.hasGoal }

// Path returns the current path and the index of the edge being run.
func (n *Navigator) Path() (Path, int) { return n.path, n.cur }

// ErrNoTarget is returned by SetGoal for a goal nothing on the graph
// satisfies.
var ErrNoTarget = errors.New("navrt: nothing on the graph satisfies the goal")

// SetGoal sets the goal (now is the clock, ms). A goal nothing satisfies
// is refused; the navigator then has no goal.
func (n *Navigator) SetGoal(goal Goal, now int64) error {
	n.ClearGoal()
	var t *Target
	if goal.Kind == GoalShoot {
		t = n.shootTarget(goal)
	} else {
		t = CompileTarget(n.g, goal)
	}
	if t.Empty() {
		return fmt.Errorf("%w: %s", ErrNoTarget, goal)
	}
	n.goal, n.target, n.hasGoal, n.goalSince = goal, t, true, now
	n.now = now
	n.stuck = stuckState{}
	n.status = Status{Follow: Following, Node: n.node, Edge: -1, WaitFor: -1}
	n.requestPlan("new goal", true)
	return nil
}

// ClearGoal stops: the navigator idles until the next SetGoal.
func (n *Navigator) ClearGoal() {
	n.goal, n.target, n.hasGoal = Goal{}, nil, false
	n.arrived = false
	n.path = Path{Start: nav.NoNode}
	n.rem = n.rem[:0]
	n.cur = 0
	n.resetStep()
	n.gout.Reset()
	n.final = false
	n.shootAt = 0
	n.status = Status{Follow: Idle, Node: n.node, Edge: -1, WaitFor: -1}
}

// Retry clears a final failure and plans again for the same goal.
func (n *Navigator) Retry() {
	if !n.hasGoal {
		return
	}
	n.final = false
	n.stuck = stuckState{}
	n.status.Follow, n.status.Cause, n.status.Reason = Following, CauseNone, ""
	n.requestPlan("retry", true)
}

// MarkBlocked leaves edge i out for a while (BlockBase, doubling each time
// it is marked again, at most BlockMax), until a related blocker's belief
// changes, and plans again. It returns until when (ms).
func (n *Navigator) MarkBlocked(i int, now int64) int64 {
	if i < 0 || i >= len(n.g.Edges) {
		return 0
	}
	e := &n.g.Edges[i]
	until := n.bl.Mark(i, n.related(e), n.ms, now)
	if n.cfg.OnBlocked != nil {
		n.cfg.OnBlocked(i, EdgeKey(e), until)
	}
	if n.onPath(i) {
		n.requestPlan("edge "+EdgeKey(e)+" blocked", true)
	}
	return until
}

// related returns the blockers whose belief decides about edge e: its
// conditions, and every blocker whose sweep overlaps the boxes of its end
// nodes (a door next to it).
func (n *Navigator) related(e *nav.Edge) []int32 {
	var out []int32
	add := func(b int32) {
		for _, x := range out {
			if x == b {
				return
			}
		}
		out = append(out, b)
	}
	for _, r := range e.Reqs {
		add(r.Blocker)
	}
	for b := range n.g.Blockers {
		lo, hi := n.ms.Sweep(int32(b))
		for _, id := range []nav.NodeID{e.From, e.To} {
			nd := &n.g.Nodes[id]
			if boxTouch(nd.Origin, nd.Flags&nav.NodeCrouch != 0, lo, hi) {
				add(int32(b))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (n *Navigator) onPath(i int) bool {
	for k := n.cur; k < len(n.path.Edges); k++ {
		if n.path.Edges[k] == i {
			return true
		}
	}
	return false
}

// observe folds a new belief in: blocker states (a change clears related
// blocked marks and repaths when the path depends on it), the prediction
// world, and the blocked edges the level memory remembers.
func (n *Navigator) observe(b *worldmodel.Belief) {
	changed := n.ms.Update(b, n.now)
	if len(changed) > 0 {
		cleared := n.bl.Refresh(n.ms)
		noPath := n.status.Follow == Failed && n.status.Cause == CauseNoPath
		if len(cleared) > 0 || noPath || n.pathDependsOn(changed) {
			n.requestPlan("blocker belief changed", false)
		}
	}
	n.rebuildWorld(b)
	n.checkOccupants()
	if len(b.Memory.Blocked) != n.memKeys {
		n.memKeys = len(b.Memory.Blocked)
		if n.keyIndex == nil {
			n.keyIndex = make(map[string]int, len(n.g.Edges))
			for i := range n.g.Edges {
				n.keyIndex[EdgeKey(&n.g.Edges[i])] = i
			}
		}
		for _, k := range b.Memory.Blocked {
			if i, ok := n.keyIndex[k]; ok {
				n.remembered[i] = true
			}
		}
	}
}

// pathDependsOn reports whether an edge left on the path has a condition
// on one of the blockers, or a blocker changed that an edge waits on.
func (n *Navigator) pathDependsOn(blockers []int32) bool {
	for k := n.cur; k < len(n.path.Edges); k++ {
		for _, r := range n.g.Edges[n.path.Edges[k]].Reqs {
			for _, b := range blockers {
				if r.Blocker == b {
					return true
				}
			}
		}
	}
	return false
}

// rebuildWorld sets the prediction world (static solids, the blockers as
// believed, the visible live monsters and players as boxes, plus one the
// bot stands on, seen within TrackMemory) and the occupants the planner
// routes around (those and the ones seen within OccupiedMemory).
func (n *Navigator) rebuildWorld(b *worldmodel.Belief) {
	n.tracks = n.tracks[:0]
	n.occ = n.occ[:0]
	if n.w != nil {
		n.solids = append(n.solids[:0], n.g.Solids...)
		n.solids = n.ms.Solids(n.solids)
		n.static.SetSolids(n.solids)
	}
	if b != nil {
		for i := range b.Tracks {
			t := &b.Tracks[i]
			if !t.PosKnown || t.Life != worldmodel.LifeAlive || t.Kind == perception.KindBarrel.String() || t.Maxs == t.Mins {
				continue
			}
			// the wire quantizes origins to 1/8 unit: a box that touches
			// the bot exactly may have a gap on the server, so the box is
			// a quantum smaller horizontally
			mins, maxs := add(t.Mins, Vec3{quantum, quantum, 0}), add(t.Maxs, Vec3{-quantum, -quantum, 0})
			lo, hi := add(t.Pos, mins), add(t.Pos, maxs)
			slo, shi := add(b.Self.Origin, b.Self.Mins), add(b.Self.Origin, b.Self.Maxs)
			solid := false
			switch {
			case overlaps(lo, hi, slo, shi):
				// the server never lets them overlap: the box is stale and
				// would leave the prediction stuck in it
				continue
			case t.Visible:
				solid = true
			case t.LastSeen > 0 && (b.Self.OnGround || b.Time-t.LastSeen <= TrackMemory) && supports(lo, hi, slo, shi):
				// out of view right under the bot: it stands on its head
				// (for as long as the server has it on the ground)
				solid = true
			case t.LastSeen > 0 && b.Time-t.LastSeen <= OccupiedMemory:
				// out of view a moment: probably still there
			default:
				continue
			}
			oc := occupant{id: t.ID, visible: t.Visible, solid: -1,
				lo: add(add(t.Pos, t.Mins), Vec3{-occMargin, -occMargin, 0}), hi: add(add(t.Pos, t.Maxs), Vec3{occMargin, occMargin, 0})}
			if solid && n.w != nil {
				oc.solid = len(n.tracks)
				n.solids = append(n.solids, navsim.Solid{ID: trackBase + len(n.tracks), Box: true, Origin: t.Pos, Mins: mins, Maxs: maxs})
				n.tracks = append(n.tracks, t.ID)
			}
			n.occ = append(n.occ, oc)
		}
	}
	if n.w != nil {
		n.w.SetSolids(n.solids)
	}
}

// occupant is a monster (or player) the planner routes around: its box
// grown by occMargin, and its prediction solid (an index into tracks, -1
// for none: out of view).
type occupant struct {
	id      string
	lo, hi  Vec3
	visible bool
	solid   int
}

// occMargin grows an occupant's box: nodes that close are taken too (the
// bot would brush it).
const occMargin = 8

// occupantOf returns the index into occ of track id, -1 if none.
func (n *Navigator) occupantOf(id string) int {
	for k := range n.occ {
		if n.occ[k].id == id {
			return k
		}
	}
	return -1
}

// occupiedAt reports whether the player box at node nd touches an
// occupant.
func (n *Navigator) occupiedAt(nd *nav.Node) bool {
	for k := range n.occ {
		if boxTouch(nd.Origin, nd.Flags&nav.NodeCrouch != 0, n.occ[k].lo, n.occ[k].hi) {
			return true
		}
	}
	return false
}

// pathOccupants returns the visible occupants standing on the path ahead
// (on the end node of one of the next occAhead steps), sorted.
func (n *Navigator) pathOccupants() []string {
	var out []string
	for k := n.cur; k < len(n.path.Edges) && k < n.cur+occAhead; k++ {
		nd := &n.g.Nodes[n.g.Edges[n.path.Edges[k]].To]
		for j := range n.occ {
			oc := &n.occ[j]
			if oc.visible && boxTouch(nd.Origin, nd.Flags&nav.NodeCrouch != 0, oc.lo, oc.hi) && !containsStr(out, oc.id) {
				out = append(out, oc.id)
			}
		}
	}
	sort.Strings(out)
	return out
}

// occAhead is how many steps ahead pathOccupants looks (about 600 units
// of grid walks).
const occAhead = 20

func containsStr(s []string, x string) bool {
	for _, y := range s {
		if y == x {
			return true
		}
	}
	return false
}

// checkOccupants asks for a plan when a monster the last plan did not
// know about stands on the path ahead (at most every occRepath).
func (n *Navigator) checkOccupants() {
	if len(n.occ) == 0 || n.cur >= len(n.path.Edges) || n.occRepathed && n.now-n.occAt < occRepath {
		return
	}
	for _, id := range n.pathOccupants() {
		if !containsStr(n.planOcc, id) {
			n.occAt, n.occRepathed = n.now, true
			n.requestPlan("monster "+id+" on the path", false)
			return
		}
	}
}

// occRepath is the shortest time between two repaths for monsters (ms).
const occRepath = 500

// lineOccupied reports whether the player hull moving from a to b passes
// through an occupant that does not already touch the bot at a.
func (n *Navigator) lineOccupied(a, b Vec3, ducked bool) bool {
	mins, maxs := hull(ducked)
	for k := range n.occ {
		oc := &n.occ[k]
		lo, hi := sub(oc.lo, maxs), sub(oc.hi, mins)
		if inside(a, lo, hi) {
			continue // touching it already: the stuck detection deals with that
		}
		if segHitsBox(a, b, lo, hi) {
			return true
		}
	}
	return false
}

func sub(a, b Vec3) Vec3 { return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

func inside(p, lo, hi Vec3) bool {
	for c := 0; c < 3; c++ {
		if p[c] < lo[c] || p[c] > hi[c] {
			return false
		}
	}
	return true
}

// segHitsBox reports whether the segment a-b meets the box lo..hi (slab
// test).
func segHitsBox(a, b, lo, hi Vec3) bool {
	t0, t1 := 0.0, 1.0
	for c := 0; c < 3; c++ {
		d := float64(b[c] - a[c])
		if math.Abs(d) < 1e-9 {
			if a[c] < lo[c] || a[c] > hi[c] {
				return false
			}
			continue
		}
		u, v := float64(lo[c]-a[c])/d, float64(hi[c]-a[c])/d
		if u > v {
			u, v = v, u
		}
		t0, t1 = math.Max(t0, u), math.Min(t1, v)
		if t0 > t1 {
			return false
		}
	}
	return true
}

// trackOf returns the track ID of a prediction-world solid ID ("" if it
// is not a track).
func (n *Navigator) trackOf(id int) string {
	if k := id - trackBase; k >= 0 && k < len(n.tracks) {
		return n.tracks[k]
	}
	return ""
}

// Localize returns the node the bot at p is at: the nearest node (with
// height weighted, see nav.ZWeight) the bot can be on in the believed
// blocker states and can walk to in a straight line, over floor if any
// is, searching 48, 96 and 192 units around; NoNode when there is none.
// A bot standing (not ducked) under a low ceiling that only a crawl gets
// out of is localized with the ducked hull as a last resort (the
// follower crouches where only that hull passes).
func (n *Navigator) Localize(p Vec3, ducked bool) nav.NodeID {
	masks := n.ms.Masks()
	hulls := []bool{ducked}
	if !ducked {
		hulls = append(hulls, true)
	}
	for _, d := range hulls {
		for _, line := range []func(a, b Vec3, ducked bool) bool{n.clearWalk, n.clear} {
			for _, r := range []float32{48, 96, 192} {
				id := n.g.LocalizeIn(p, r, masks, func(id nav.NodeID, nd *nav.Node) bool {
					return n.usable(id) && line(p, nd.Origin, d || nd.Flags&nav.NodeCrouch != 0)
				})
				if id != nav.NoNode {
					return id
				}
			}
		}
	}
	return nav.NoNode
}

// usable reports whether node id can start a path in the believed state:
// it has no way out at all (an end node), or one whose conditions hold or
// will by waiting. A node all of whose ways out need a blocker gone or
// elsewhere is a spot the blocker takes now (a floor node a func_explosive
// overlaps, until it is blown up).
func (n *Navigator) usable(id nav.NodeID) bool {
	lo, hi := n.g.OutRange(id)
	if lo == hi {
		return true
	}
	for i := lo; i < hi; i++ {
		if ok, _ := n.holds(&n.g.Edges[i]); ok {
			return true
		}
	}
	return false
}

// crawl reports whether the straight way from the bot to p is clear for
// the ducked hull only: the bot must crouch to get there.
func (n *Navigator) crawl(p Vec3) bool {
	o := n.st.Origin()
	return n.static != nil && !n.clear(o, p, false) && n.clear(o, p, true)
}

// clear reports whether the player hull can move from a to b in a
// straight line: directly, or raised by a step (stairs).
func (n *Navigator) clear(a, b Vec3, ducked bool) bool {
	w := n.static
	if w == nil {
		return true
	}
	mins, maxs := hull(ducked)
	if dist3(a, b) < 1 {
		return true
	}
	if tr := w.Trace(a, mins, maxs, b, q2const.MASK_PLAYERSOLID); !tr.StartSolid && tr.Fraction == 1 {
		return true
	}
	up := Vec3{0, 0, navsim.StepHeight}
	a2, b2 := add(a, up), add(b, up)
	if tr := w.Trace(a, mins, maxs, a2, q2const.MASK_PLAYERSOLID); tr.StartSolid || tr.Fraction < 1 {
		return false
	}
	tr := w.Trace(a2, mins, maxs, b2, q2const.MASK_PLAYERSOLID)
	return !tr.StartSolid && tr.Fraction == 1
}

// clearWalk is clear plus floor under the line (floorBetween).
func (n *Navigator) clearWalk(a, b Vec3, ducked bool) bool {
	return n.clear(a, b, ducked) && n.floorBetween(a, b, ducked)
}

// floorBetween reports whether there is ground under the line from a to b
// every 16 units, within a step below the lower end (no pit to cut
// across).
func (n *Navigator) floorBetween(a, b Vec3, ducked bool) bool {
	if n.static == nil {
		return true
	}
	mins, maxs := hull(ducked)
	d := dist3(a, b)
	steps := int(d / 16)
	for k := 1; k <= steps; k++ {
		f := float32(k) / float32(steps+1)
		p := Vec3{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f, max(a[2], b[2]) + 2}
		down := p
		down[2] = min(a[2], b[2]) - navsim.StepHeight - 2
		if tr := n.static.Trace(p, mins, maxs, down, q2const.MASK_PLAYERSOLID); tr.Fraction == 1 && !tr.StartSolid {
			return false
		}
	}
	return true
}

// quantum is the precision of entity origins on the wire.
const quantum = 0.125

// overlaps reports whether two boxes share some volume (more than a
// quarter unit in every axis: touching faces do not count).
func overlaps(amin, amax, bmin, bmax Vec3) bool {
	for c := 0; c < 3; c++ {
		if min(amax[c], bmax[c])-max(amin[c], bmin[c]) <= 0.25 {
			return false
		}
	}
	return true
}

// supports reports whether box a is right under the player box b,
// overlapping it horizontally: b stands on it.
func supports(amin, amax, bmin, bmax Vec3) bool {
	for c := 0; c < 2; c++ {
		if min(amax[c], bmax[c])-max(amin[c], bmin[c]) <= 0 {
			return false
		}
	}
	return amax[2] <= bmin[2]+0.25 && amax[2] >= bmin[2]-2
}

func hull(ducked bool) (Vec3, Vec3) {
	if ducked {
		return navsim.StandMins(), navsim.DuckMaxs()
	}
	return navsim.StandMins(), navsim.StandMaxs()
}

// cost is the planner's edge cost in the current belief.
func (n *Navigator) cost(i int, e *nav.Edge) (float32, bool) {
	if n.bl.Active(i, n.now) {
		return 0, false
	}
	if safe, checked := n.landing[i]; checked && !safe {
		return 0, false
	}
	if len(e.Reqs) > 0 && n.startsInside(i, e) {
		return 0, false
	}
	switch e.Kind {
	case nav.EdgeTouch:
		// a touch edge presses a button or walks into a trigger on
		// purpose: only to reach a goal about it
		if !n.target.Edge(i) {
			return 0, false
		}
	case nav.EdgeRide:
		if e.Flags&nav.EdgeNeedsUse != 0 && !n.cfg.AllowNeedsUse && !n.riding(e) {
			return 0, false
		}
	}
	if e.Target != 0 && n.avoid[e.Target] {
		return 0, false
	}
	pen := float32(0)
	for k := range e.Effects {
		f := &e.Effects[k]
		if n.avoid[f.Entity] {
			return 0, false
		}
		if f.Kind == nav.EffButton && !(n.hasGoal && n.goal.Entity == f.Entity && n.goal.Kind == GoalTouch) {
			pen += n.cfg.ButtonPenalty
		}
	}
	ok, wait := n.holds(e)
	if !ok {
		return 0, false
	}
	c := e.Cost + wait + pen
	switch {
	case e.Flags&nav.EdgeFromRest != 0 || selfStopping(e):
		c += StopCost
	case swimStart(e):
		c += SwimStartCost
	}
	if e.Flags&nav.EdgeFragile != 0 {
		c += n.cfg.FragilePenalty
	}
	if dmg := int(e.FallDamage) + int(e.Damage); dmg > 0 {
		c += float32(dmg) * n.cfg.DamageCost
	}
	if n.remembered[i] {
		c += RememberedPenalty
	}
	if len(n.occ) > 0 && n.occupiedAt(&n.g.Nodes[e.To]) {
		c += OccupiedCost
	}
	return c, true
}

// holds reports whether edge e's conditions hold in the believed state,
// or will by waiting (with the longest wait; MapState.Holds), counting a
// condition on the mover the edge starts on as met (carried).
func (n *Navigator) holds(e *nav.Edge) (ok bool, wait float32) {
	for _, r := range e.Reqs {
		if n.carried(e, r) {
			continue
		}
		ok, w := n.ms.Satisfied(r, n.now)
		if !ok {
			return false, 0
		}
		wait = max(wait, w)
	}
	return true, wait
}

// carried reports whether condition r of edge e is on the mover e starts
// on, at the pose of that start node: a bot standing there has the mover
// at that pose under its feet (it boarded or rode it there), whatever the
// belief says from before. So a ride up a plat and the walk off at the top
// plan together.
func (n *Navigator) carried(e *nav.Edge, r nav.Req) bool {
	from := &n.g.Nodes[e.From]
	return from.Blocker >= 0 && r.Blocker == from.Blocker && from.Pose >= 0 && r.States&nav.Pose(int(from.Pose)) != 0
}

// startsInside reports whether edge e cannot start where it does: for one
// of its conditions the player box at its start node is inside the
// blocker at every pose the condition allows. Such a spot only exists
// while the mover is elsewhere (the floor under a plat at its top, for a
// step onto the plat once it is down): the builder's simulation pops the
// player out of the solid, the game would not. A pose the mover rises to
// is no obstacle when it would lift the player standing there onto the
// edge's end (a pedestal coming up a few units under the bot's feet:
// SV_Push carries it). The verdict is static and cached.
func (n *Navigator) startsInside(i int, e *nav.Edge) bool {
	if n.probe == nil {
		if n.md == nil || n.md.CM == nil {
			return false
		}
		n.probe = navsim.NewWorld(n.md.CM)
	}
	if v, ok := n.inside[i]; ok {
		return v
	}
	from := &n.g.Nodes[e.From]
	mins, maxs := hull(from.Flags&nav.NodeCrouch != 0)
	// a quarter unit in: touching faces are not inside (the position test
	// calls them solid)
	mins, maxs = add(mins, Vec3{0.25, 0.25, 0.25}), add(maxs, Vec3{-0.25, -0.25, -0.25})
	v := false
	for _, r := range e.Reqs {
		if n.carried(e, r) || r.States&nav.StateGone != 0 || r.Blocker < 0 || int(r.Blocker) >= len(n.g.Blockers) {
			continue
		}
		bl := &n.g.Blockers[r.Blocker]
		if !bl.Solid {
			continue
		}
		all := true
		for k := range bl.Poses {
			if r.States&nav.Pose(k) == 0 {
				continue
			}
			sol := n.g.BlockerSolid(r.Blocker, k)
			n.probe.SetSolids([]navsim.Solid{sol})
			if tr := n.probe.Trace(from.Origin, mins, maxs, from.Origin, q2const.MASK_PLAYERSOLID); !tr.StartSolid || tr.Ent != sol.ID || n.lifts(e, r.Blocker, k, mins, maxs) {
				all = false
				break
			}
		}
		if all {
			v = true
			break
		}
	}
	if n.inside == nil {
		n.inside = map[int]bool{}
	}
	n.inside[i] = v
	return v
}

// lifts reports whether blocker b rising to pose k lifts a player at edge
// e's start onto e's end: the end stands on b, k is not b's lowest pose,
// and the player fits within a step above the start (the probe world
// holds b at k).
func (n *Navigator) lifts(e *nav.Edge, b int32, k int, mins, maxs Vec3) bool {
	if n.g.Nodes[e.To].Blocker != b {
		return false
	}
	bl := &n.g.Blockers[b]
	lowest := true
	for j := range bl.Poses {
		if bl.Poses[j].Origin[2] < bl.Poses[k].Origin[2] {
			lowest = false
		}
	}
	if lowest {
		return false
	}
	o := n.g.Nodes[e.From].Origin
	for dz := float32(1); dz <= navsim.StepHeight+1; dz++ {
		p := add(o, Vec3{0, 0, dz})
		if tr := n.probe.Trace(p, mins, maxs, p, q2const.MASK_PLAYERSOLID); !tr.StartSolid {
			return true
		}
	}
	return false
}

// riding reports whether the mover of ride e is seen moving (someone used
// it).
func (n *Navigator) riding(e *nav.Edge) bool {
	b := n.g.Nodes[e.From].Blocker
	return b >= 0 && n.ms.Belief(b).Status == BlockerMoving
}

// requestPlan asks for a plan on the next tick; event marks a change
// that resets the stuck detection's progress window (a new goal, a
// blocked edge, a relocalization).
func (n *Navigator) requestPlan(why string, event bool) {
	n.replan = true
	n.replanWhy = why
	n.replanEv = n.replanEv || event
}

// shootTarget compiles a shoot goal: the standing nodes within
// ShootRange of the aim point with a clear shot (MASK_SHOT from the eye)
// at it or at the target entity's solid.
func (n *Navigator) shootTarget(goal Goal) *Target {
	t := NewTarget(n.g)
	cands := n.g.Nearby(goal.Point, ShootRange)
	tested := 0
	for _, c := range cands {
		nd := &n.g.Nodes[c.Node]
		if nd.Flags&(nav.NodeCrouch|nav.NodeLadder|nav.NodeWater|nav.NodeMover) != 0 {
			continue
		}
		if dist3(nd.Origin, goal.Point) > ShootRange {
			continue
		}
		if tested++; tested > 256 {
			break
		}
		if n.clearShot(add(nd.Origin, Vec3{0, 0, 22}), goal) {
			t.AddNode(n.g, c.Node)
		}
	}
	return t
}

func (n *Navigator) clearShot(eye Vec3, goal Goal) bool {
	if n.static == nil {
		return true
	}
	tr := n.static.Trace(eye, Vec3{}, Vec3{}, goal.Point, q2const.MASK_SHOT)
	return !tr.StartSolid && (tr.Fraction == 1 || int32(tr.Ent) == goal.Entity)
}

// Vector helpers.

func add(a, b Vec3) Vec3 { return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func distH(a, b Vec3) float32 {
	return float32(math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1])))
}

// idleIntent stands still with the view of state s.
func idleIntent(s *navsim.State) control.MoveIntent { return control.Idle(s.ViewYaw, 0) }
