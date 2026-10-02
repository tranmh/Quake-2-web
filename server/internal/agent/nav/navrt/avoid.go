package navrt

import (
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Avoided entities (Config.Avoid, SetAvoid) are lump entities the bot must
// never set off: exits it should not take, the activators of other exits.
// The planner leaves out every edge that sets one off (cost). The
// follower's own straight-line steering is no validated edge, so it keeps
// the player box off them as well: the pursuit lookahead and catch-up do
// not cut a corner through one, the recovery manoeuvres (jump, strafe,
// back off, step off a monster) do not head into one, and the walk back
// to the graph, the wait spots and localization only pick nodes the
// straight way to which stays clear of them. The boxes checked are the
// entities' volumes (triggers, items; Graph.Volumes) and the spawn bounds
// of avoided buttons (which fire on contact).

// avoidMargin is how far (units, horizontally) the follower's own
// steering keeps the player box from an avoided entity: the real path
// curves away from the straight line it checks.
const avoidMargin = 8

// avoidBox is a box an avoided entity fires on contact with: a volume of
// a trigger or item (only while its mover is in pose, for a volume with
// blocker >= 0), or a button's bounds.
type avoidBox struct {
	ent     int32
	lo, hi  Vec3
	blocker int32
	pose    int8
}

// SetAvoid replaces the avoid set (see Config.Avoid). It keeps the
// navigator's blocked marks and caches, and plans again when a goal is
// set: a route executor sets a different avoid list for each leg.
func (n *Navigator) SetAvoid(ents []int32) {
	n.cfg.Avoid = append([]int32(nil), ents...)
	n.buildAvoid()
	if n.hasGoal {
		n.requestPlan("avoid set changed", true)
	}
}

// Avoid returns a copy of the avoid set.
func (n *Navigator) Avoid() []int32 { return append([]int32(nil), n.cfg.Avoid...) }

// buildAvoid derives the avoid lookup and boxes from cfg.Avoid.
func (n *Navigator) buildAvoid() {
	n.avoid = map[int32]bool{}
	n.avoidBoxes = n.avoidBoxes[:0]
	for _, a := range n.cfg.Avoid {
		if n.avoid[a] {
			continue
		}
		n.avoid[a] = true
		for _, vi := range n.volsOf[a] {
			v := &n.g.Volumes[vi]
			n.avoidBoxes = append(n.avoidBoxes, avoidBox{ent: a, lo: v.Min, hi: v.Max, blocker: v.Blocker, pose: v.Pose})
		}
		if n.md == nil {
			continue
		}
		if mv := n.md.Mover(int(a)); mv != nil && mv.Kind == mapdata.MoverButton {
			n.avoidBoxes = append(n.avoidBoxes, avoidBox{ent: a, lo: mv.Box.Min, hi: mv.Box.Max, blocker: -1, pose: -1})
		}
		if tr := n.md.Trigger(int(a)); tr != nil && tr.HasVolume && !tr.NotPlayer && len(n.volsOf[a]) == 0 {
			n.avoidBoxes = append(n.avoidBoxes, avoidBox{ent: a, lo: tr.Box.Min, hi: tr.Box.Max, blocker: -1, pose: -1})
		}
	}
}

// avoidHit reports whether the player hull (ducked or standing) moving in
// a straight line from a to b touches an avoided entity, or with margin
// comes within margin units of one horizontally. A box the hull touches
// at a already is skipped (it went off, or the bot started in it: getting
// out is fine), and one the hull is within the margin of at a is only
// checked for contact.
func (n *Navigator) avoidHit(a, b Vec3, ducked bool, margin float32) bool {
	if len(n.avoidBoxes) == 0 {
		return false
	}
	mins, maxs := hull(ducked)
	// boxTouch's contact test: the player's abs box is grown by a unit
	one := Vec3{1, 1, 1}
	m := Vec3{margin, margin, 0}
	for k := range n.avoidBoxes {
		ab := &n.avoidBoxes[k]
		if ab.blocker >= 0 && ab.pose >= 0 && n.ms.Possible(ab.blocker)&nav.Pose(int(ab.pose)) == 0 {
			continue
		}
		lo, hi := sub(sub(ab.lo, maxs), one), add(sub(ab.hi, mins), one)
		if inside(a, lo, hi) {
			continue
		}
		if glo, ghi := sub(lo, m), add(hi, m); !inside(a, glo, ghi) {
			lo, hi = glo, ghi
		}
		if segHitsBox(a, b, lo, hi) {
			return true
		}
	}
	return false
}

// avoidSweep reports whether running from where the bot is in direction
// dir (horizontal, unit length) for msec at full speed could touch an
// avoided entity (avoidHit with the margin).
func (n *Navigator) avoidSweep(dir Vec3, msec int) bool {
	if len(n.avoidBoxes) == 0 {
		return false
	}
	o := n.st.Origin()
	d := float32(msec) / 1000 * HorizontalSpeed
	return n.avoidHit(o, add(o, Vec3{dir[0] * d, dir[1] * d, 0}), n.st.Ducked(), avoidMargin)
}

// safeDir reports whether a manoeuvre running in direction dir for msec
// keeps floor under the bot and stays off the avoided entities.
func (n *Navigator) safeDir(dir Vec3, msec int) bool {
	return n.floorAlong(dir, msec) && !n.avoidSweep(dir, msec)
}

// avoidDetour checks the straight way from o to target, the pursuit's
// end of the current walk e, when the bot is off the edge's line (more
// than twice the arrival tolerance; on it the validated edge is trusted):
// when that way would touch an avoided entity it returns the edge's start
// to go back to instead, the walk having been validated from there; ok is
// false when that way touches one too (the bot should relocalize).
func (n *Navigator) avoidDetour(o Vec3, e *nav.Edge, target Vec3, ducked bool) (Vec3, bool) {
	if len(n.avoidBoxes) == 0 {
		return target, true
	}
	from, to := n.g.Nodes[e.From].Origin, n.g.Nodes[e.To].Origin
	if segDistH(o, from, to) <= 2*navsim.ArriveXY || !n.avoidHit(o, target, ducked, 0) {
		return target, true
	}
	if !n.avoidHit(o, from, ducked, 0) {
		return from, true
	}
	return target, false
}
