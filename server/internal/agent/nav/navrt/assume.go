package navrt

import (
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/worldmodel"
)

// Assume makes the belief about blocker b its pose (gone when pose < 0)
// as of now (ms) without an observation: what the route claims a step
// caused that the bot could not see (a door "opened elsewhere", a wall a
// killtarget removed). Observations made before now no longer count; a
// newer one replaces the assumption. A returning door assumed open goes
// back to its spawn pose after its wait, like one seen open. It reports
// whether the belief changed (gone needs a blocker that can be gone, a
// pose one of its poses).
func (m *MapState) Assume(b int32, pose int, now int64) bool {
	if b < 0 || int(b) >= len(m.bel) {
		return false
	}
	bl := &m.g.Blockers[b]
	nb := m.bel[b]
	switch {
	case pose < 0:
		if !bl.Gone {
			return false
		}
		nb.Status, nb.Pose = BlockerGone, -1
	case pose < len(bl.Poses):
		nb.Status, nb.Pose = BlockerAt, pose
		nb.Origin, nb.Angles = bl.Poses[pose].Origin, bl.Poses[pose].Angles
	default:
		return false
	}
	nb.Assumed, nb.Seen = true, now
	m.assumedAt[b] = now
	changed := nb.Status != m.bel[b].Status || nb.Pose != m.bel[b].Pose
	if changed {
		nb.Since = now
		m.rev[b]++
	}
	m.bel[b] = nb
	return changed
}

// holdOpen keeps an auto door open in the belief while the bot stands in
// its trigger: Touch_DoorTrigger opens the door and keeps resetting its
// wait for as long as a player touches the trigger (C: game/g_func.c
// Touch_DoorTrigger, door_go_up), whether or not the bot sees it (a door
// that rises into the ceiling is out of view once open). After the
// door's travel time in the trigger an unseen door counts as open; a door
// seen open does not expire while the bot stays in the trigger. A fresh
// observation of the door closed or moving wins.
func (m *MapState) holdOpen(i int32, nb *BlockerBelief, b *worldmodel.Belief, now int64) {
	bl, in := &m.g.Blockers[i], &m.info[i]
	if !in.auto || in.trigger == nil || len(bl.Poses) != 2 || bl.Spawn < 0 || bl.Spawn > 1 {
		return
	}
	switch bl.Kind {
	case nav.BlockDoor, nav.BlockRotating:
	default:
		return
	}
	if b == nil || b.Self.Dead || !boxTouch(b.Self.Origin, b.Self.Ducked, in.trigger.Min, in.trigger.Max) {
		m.inside[i] = noAssume
		return
	}
	if m.inside[i] == noAssume {
		m.inside[i] = now
	}
	open := 1 - int(bl.Spawn)
	fresh := !nb.Assumed && now-nb.Seen < freshSight
	switch {
	case nb.Status == BlockerAt && nb.Pose == open:
	case fresh && (nb.Status == BlockerMoving || nb.Status == BlockerAt):
		return // what the bot sees now says it all
	case now-m.inside[i] >= int64(in.travel*1000):
		p := &bl.Poses[open]
		nb.Status, nb.Pose, nb.Origin, nb.Angles, nb.Assumed = BlockerAt, open, p.Origin, p.Angles, true
	default:
		return
	}
	m.held[i] = now
}

// freshSight is how old (ms) an observation may be to count as what the
// bot sees now.
const freshSight = 500

// Assume makes the navigator count on blocker b at pose (gone when pose <
// 0) from now on (see MapState.Assume) and plans again when that changed
// the belief: the route executor assumes the effects its steps claim when
// the bot could not see them and the plan needs them.
func (n *Navigator) Assume(b int32, pose int, now int64) bool {
	if !n.ms.Assume(b, pose, now) {
		return false
	}
	n.bl.Refresh(n.ms)
	n.requestPlan("assumed "+n.blockerName(b), false)
	return true
}
