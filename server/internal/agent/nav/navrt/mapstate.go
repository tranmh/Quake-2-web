package navrt

import (
	"math"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// BlockerStatus is what the bot knows about a blocker.
type BlockerStatus uint8

const (
	// BlockerUnknown: never observed, or the last observation is too old
	// to hold (a door that went back since): assumed in its spawn state.
	BlockerUnknown BlockerStatus = iota
	// BlockerAt: observed resting at a pose.
	BlockerAt
	// BlockerMoving: observed between poses (moving, or stopped halfway).
	BlockerMoving
	// BlockerGone: observed not there (a laser switched off).
	BlockerGone
)

// String returns the status name.
func (s BlockerStatus) String() string {
	switch s {
	case BlockerAt:
		return "at"
	case BlockerMoving:
		return "moving"
	case BlockerGone:
		return "gone"
	}
	return "unknown"
}

// BlockerBelief is the belief about one graph blocker.
type BlockerBelief struct {
	Status BlockerStatus
	// Pose is the pose it rests at (BlockerAt), the pose it is assumed to
	// be in (BlockerUnknown: the spawn pose, -1 when it starts gone) or the
	// pose nearest to the last observation (BlockerMoving).
	Pose int
	// Observed reports a pose observation; Origin and Angles are the last
	// one.
	Observed       bool
	Origin, Angles Vec3
	// Seen is the clock (ms) of the last admitted observation, Since when
	// Status or Pose last changed.
	Seen, Since int64
}

// Timing assumptions about movers.
const (
	// PlatReturn is how long a plat stays at its top before it goes back
	// down (plat_hit_top: 3 s, whatever its wait says).
	PlatReturn = 3
	// DefaultTravel is the travel time assumed for movers whose map data
	// has none (trains).
	DefaultTravel = 3
	// minWait is the shortest wait estimate (a mover about to arrive).
	minWait = 0.2
	// poseTolerance is how close (units, degrees) an observed pose must be
	// to a blocker pose to count as resting there.
	poseTolerance = 0.5
	// returnSlack is added to a returning mover's wait and travel before an
	// unobserved one is assumed back at its spawn pose (ms).
	returnSlack = 1000
)

type blockerInfo struct {
	// auto: the team master moves when a player walks up to it (a touch
	// door's trigger, a touch plat) or by itself (trains without a
	// targetname).
	auto bool
	// returns: it goes back to its spawn pose by itself, wait seconds
	// after arriving at another pose (doors with a wait >= 0 that are not
	// toggles).
	returns      bool
	wait, travel float32
	// trigger is the door team's or the plat's trigger box (nil if none).
	trigger *mapdata.Box
	// sweep is the union of the blocker's link boxes over its poses:
	// where it can be.
	sweep [2]Vec3
}

// MapState is the belief about the dynamic state of the level that edge
// conditions depend on: each graph blocker's pose, gone or unknown. It is
// fed by Update from the world model's belief only (mover poses and
// lasers the bot saw or heard) and otherwise assumes the level as it
// spawns. A MapState is not safe for concurrent use.
type MapState struct {
	g    *nav.Graph
	info []blockerInfo
	bel  []BlockerBelief
	rev  []uint32
}

// NewMapState returns the map state of graph g (resolved for a skill) and
// its map data md, as the level spawns.
func NewMapState(g *nav.Graph, md *mapdata.Map) *MapState {
	m := &MapState{g: g, info: make([]blockerInfo, len(g.Blockers)), bel: make([]BlockerBelief, len(g.Blockers)), rev: make([]uint32, len(g.Blockers))}
	for i := range g.Blockers {
		bl := &g.Blockers[i]
		in := &m.info[i]
		in.travel = DefaultTravel
		if md != nil {
			master := md.Mover(md.TeamMaster(int(bl.Entity)))
			if master != nil {
				in.auto = master.Activation&(mapdata.ActTouch|mapdata.ActAuto) != 0
				in.trigger = master.Trigger
			}
			if mv := md.Mover(int(bl.Entity)); mv != nil {
				if mv.TravelTime > 0 {
					in.travel = mv.TravelTime
				}
				switch bl.Kind {
				case nav.BlockDoor, nav.BlockRotating, nav.BlockSecret:
					in.returns = mv.Wait >= 0 && !mv.Toggle
					in.wait = mv.Wait
				case nav.BlockPlat:
					in.wait = PlatReturn
				}
				if in.trigger == nil {
					in.trigger = mv.Trigger
				}
			}
		}
		for k, p := range bl.Poses {
			lo, hi := navsim.LinkBox(true, p.Origin, p.Angles, bl.Mins, bl.Maxs)
			if bl.Kind == nav.BlockLaser {
				lo, hi = segBox(bl.Start, bl.End)
			}
			if k == 0 {
				in.sweep = [2]Vec3{lo, hi}
				continue
			}
			for c := 0; c < 3; c++ {
				in.sweep[0][c] = min(in.sweep[0][c], lo[c])
				in.sweep[1][c] = max(in.sweep[1][c], hi[c])
			}
		}
		m.bel[i] = BlockerBelief{Status: BlockerUnknown, Pose: int(bl.Spawn)}
	}
	return m
}

func segBox(a, b Vec3) (lo, hi Vec3) {
	for c := 0; c < 3; c++ {
		lo[c], hi[c] = min(a[c], b[c])-1, max(a[c], b[c])+1
	}
	return lo, hi
}

// Graph returns the graph the state belongs to.
func (m *MapState) Graph() *nav.Graph { return m.g }

// Belief returns the belief about blocker b.
func (m *MapState) Belief(b int32) BlockerBelief { return m.bel[b] }

// Revision counts the changes of blocker b's belief (status or pose).
func (m *MapState) Revision(b int32) uint32 { return m.rev[b] }

// Auto reports whether blocker b moves when the bot walks up to it (an
// auto door, a touch plat) or by itself.
func (m *MapState) Auto(b int32) bool { return m.info[b].auto }

// Trigger returns the box of blocker b's trigger (a door team's or a plat's
// center trigger), or nil.
func (m *MapState) Trigger(b int32) *mapdata.Box { return m.info[b].trigger }

// Travel returns the seconds blocker b takes between its poses.
func (m *MapState) Travel(b int32) float32 { return m.info[b].travel }

// Sweep returns the box blocker b can occupy over all its poses.
func (m *MapState) Sweep(b int32) (lo, hi Vec3) { return m.info[b].sweep[0], m.info[b].sweep[1] }

// Update folds the world model's belief in (now is the belief's clock, ms)
// and returns the blockers whose status or pose changed.
func (m *MapState) Update(b *worldmodel.Belief, now int64) []int32 {
	var changed []int32
	for i := range m.g.Blockers {
		bl := &m.g.Blockers[i]
		nb := m.bel[i]
		switch {
		case bl.Kind == nav.BlockLaser:
			if l := b.Laser(int(bl.Entity)); l != nil && l.State != worldmodel.LaserUnknown {
				nb.Seen = l.LastUpdate
				if l.State == worldmodel.LaserOn {
					nb.Status, nb.Pose = BlockerAt, 0
				} else {
					nb.Status, nb.Pose = BlockerGone, -1
				}
			}
		case bl.Model != "":
			mv := b.Mover(bl.Model)
			if mv == nil {
				break
			}
			if !nb.Observed || mv.LastUpdate != nb.Seen {
				nb.Observed, nb.Origin, nb.Angles, nb.Seen = true, mv.Origin, mv.Angles, mv.LastUpdate
				k, d := nearestPose(bl, mv.Origin, mv.Angles)
				if d <= poseTolerance {
					nb.Status, nb.Pose = BlockerAt, k
				} else {
					nb.Status, nb.Pose = BlockerMoving, k
				}
			}
			m.expire(int32(i), &nb, now)
		}
		if nb.Status != m.bel[i].Status || nb.Pose != m.bel[i].Pose {
			nb.Since = now
			m.rev[i]++
			changed = append(changed, int32(i))
		}
		m.bel[i] = nb
	}
	return changed
}

// expire turns an old observation of a mover that does not stay where it
// was seen into the spawn-state assumption: a returning door seen open
// longer ago than its wait and travel, a plat seen at its top, anything
// seen moving longer ago than its travel time.
func (m *MapState) expire(b int32, nb *BlockerBelief, now int64) {
	bl, in := &m.g.Blockers[b], &m.info[b]
	age := now - nb.Seen
	switch {
	case nb.Status == BlockerMoving && age > int64(in.travel*1000)+returnSlack:
	case nb.Status == BlockerAt && nb.Pose != int(bl.Spawn) && (in.returns || bl.Kind == nav.BlockPlat) &&
		age > int64((in.wait+in.travel)*1000)+returnSlack:
	default:
		return
	}
	nb.Status, nb.Pose = BlockerUnknown, int(bl.Spawn)
}

// nearestPose returns the pose of bl closest to an observed origin and
// angles, and the distance: the larger of the origin distance in units and
// a third of the angle distance in degrees (the network sends origins in
// 1/8 units but angles in 360/256 degree steps).
func nearestPose(bl *nav.Blocker, o, a Vec3) (int, float32) {
	best, bd := -1, float32(math.MaxFloat32)
	for k, p := range bl.Poses {
		d := float32(0)
		for c := 0; c < 3; c++ {
			d = max(d, abs32(p.Origin[c]-o[c]), abs32(angleDiff(p.Angles[c], a[c]))/3)
		}
		if d < bd {
			best, bd = k, d
		}
	}
	return best, bd
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

func angleDiff(a, b float32) float32 {
	d := math.Mod(float64(a-b), 360)
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	return float32(d)
}

// Possible returns the states blocker b may be in now: the pose it was
// seen resting at, gone, every state while it moves or when unknown (the
// spawn state is only an assumption).
func (m *MapState) Possible(b int32) nav.StateMask {
	bb := &m.bel[b]
	switch bb.Status {
	case BlockerAt:
		return nav.Pose(bb.Pose)
	case BlockerGone:
		return nav.StateGone
	}
	return m.g.Blockers[b].All()
}

// Assumed returns the state the planner counts on now: the observed pose
// or gone, the spawn state when unknown, nothing while it moves.
func (m *MapState) Assumed(b int32) nav.StateMask {
	bb := &m.bel[b]
	switch bb.Status {
	case BlockerAt:
		return nav.Pose(bb.Pose)
	case BlockerGone:
		return nav.StateGone
	case BlockerMoving:
		return 0
	}
	return m.g.Blockers[b].SpawnState()
}

// Masks returns Possible for every blocker (for nav.Graph.LocalizeIn).
func (m *MapState) Masks() []nav.StateMask {
	out := make([]nav.StateMask, len(m.bel))
	for i := range out {
		out[i] = m.Possible(int32(i))
	}
	return out
}

// Satisfied reports whether condition r holds in the believed state, or
// will hold by waiting where the bot is: ok with wait 0 when it holds now
// (or is assumed to: the blocker is in that state, or unknown and spawns
// in it), ok with the estimated wait (seconds) when waiting makes it hold,
// not ok when only something else would (a button, a kill). Waiting
// counts for
//   - a mover seen moving: it arrives within its travel time;
//   - an auto door (touch or automatic): it opens when the bot walks up to
//     it, and one that returns closes again after its wait; an unknown
//     auto door is passable after its travel time;
//   - a plat at its top: it goes back down after PlatReturn (a plat at
//     its bottom only rises with a rider: a ride edge);
//   - any other returning door away from its spawn pose: it goes back.
func (m *MapState) Satisfied(r nav.Req, now int64) (ok bool, wait float32) {
	if r.Blocker < 0 || int(r.Blocker) >= len(m.bel) {
		return false, 0
	}
	if m.Assumed(r.Blocker)&r.States != 0 {
		return true, 0
	}
	bl, in, bb := &m.g.Blockers[r.Blocker], &m.info[r.Blocker], &m.bel[r.Blocker]
	poses := r.States &^ nav.StateGone
	elapsed := float32(now-bb.Since) / 1000
	switch {
	case poses == 0 || bb.Status == BlockerGone:
		return false, 0 // only a kill or a switch makes it go (or come back)
	case bb.Status == BlockerMoving:
		return true, max(in.travel-elapsed, minWait)
	case bl.Kind == nav.BlockPlat:
		if in.auto && (bb.Status == BlockerAt || bb.Status == BlockerUnknown) && bb.Pose == 0 && poses&nav.Pose(1) != 0 {
			return true, max(PlatReturn+in.travel-elapsed, minWait)
		}
		return false, 0
	case in.auto && (bl.Kind == nav.BlockDoor || bl.Kind == nav.BlockRotating || bl.Kind == nav.BlockSecret):
		cur := bb.Pose
		if poses&nav.Pose(cur) != 0 {
			return true, 0
		}
		if cur == int(bl.Spawn) {
			return true, in.travel // it opens as the bot walks up
		}
		if in.returns && poses&nav.Pose(int(bl.Spawn)) != 0 {
			return true, max(in.wait+in.travel-elapsed, minWait)
		}
		return true, in.travel // a toggle: walking up moves it again
	case in.auto && bl.Kind == nav.BlockTrain:
		return true, in.travel
	case in.returns && bb.Pose != int(bl.Spawn) && poses&nav.Pose(int(bl.Spawn)) != 0:
		return true, max(in.wait+in.travel-elapsed, minWait)
	}
	return false, 0
}

// Holds reports whether all of e's conditions are met by waiting at most
// some time (ok), with the longest wait.
func (m *MapState) Holds(e *nav.Edge, now int64) (ok bool, wait float32) {
	for _, r := range e.Reqs {
		ok, w := m.Satisfied(r, now)
		if !ok {
			return false, 0
		}
		wait = max(wait, w)
	}
	return true, wait
}

// Solids appends the solid blockers as the bot believes them to dst: at
// the observed pose (exactly as seen, also while moving), at the spawn
// pose when unknown, none when gone. These and the graph's static solids
// are the prediction world.
func (m *MapState) Solids(dst []navsim.Solid) []navsim.Solid {
	for i := range m.g.Blockers {
		bl := &m.g.Blockers[i]
		bb := &m.bel[i]
		if !bl.Solid || bb.Status == BlockerGone {
			continue
		}
		switch {
		case bb.Observed && bb.Status != BlockerUnknown:
			dst = append(dst, navsim.Solid{ID: int(bl.Entity), Headnode: bl.Headnode, Origin: bb.Origin, Angles: bb.Angles, Mins: bl.Mins, Maxs: bl.Maxs})
		case bb.Pose >= 0 && bb.Pose < len(bl.Poses):
			dst = append(dst, m.g.BlockerSolid(int32(i), bb.Pose))
		}
	}
	return dst
}
