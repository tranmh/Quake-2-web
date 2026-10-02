package routeexec

import (
	"fmt"
	"sort"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
)

// Kill step timing (ms).
const (
	// occludedHold is how long the bot keeps its firing position when the
	// monster drops out of view before it looks for another one.
	occludedHold = 1000
	// lookAround is how long the bot looks at the monster's spot from a
	// firing position before it walks up closer.
	lookAround = 3000
	// dyingConfirm is how long a monster seen dying (death animation or
	// cry) must stay down to count as dead.
	dyingConfirm = 1500
	// minFireDist keeps firing positions off the monster itself.
	minFireDist = 64
	// maxFireNodes bounds the goal nodes of a firing position.
	maxFireNodes = 48
	// trackMatch bounds how far from its spawn origin an unmatched track of
	// the monster's class may be to stand for it (units).
	trackMatch = 384
)

// killOrder returns the monster of kill step p as the bot knows it: where
// it was last seen (its spawn origin before), or where hearing places it
// when it was heard since from where that disagrees with. A monster never
// seen has no track here (the world model ties a track to its lump entity
// only once seen): the step goes for its spawn origin.
func (x *Executor) killOrder(p *plan) *KillOrder {
	k := &KillOrder{Lump: p.ent, Class: p.class, Pos: x.spawnCenter(p)}
	if t := x.killTrack(p); t != nil {
		k.Track = t.ID
		if t.PosKnown {
			k.Pos = boxCenter(t, t.Pos)
		}
		k.Seen = k.Pos
		if t.LocKnown && !t.LocSeen {
			k.Pos, k.ByEar = boxCenter(t, t.Loc), true
		}
		k.Visible = t.Visible && t.Shootable
	}
	return k
}

// spawnCenter is the middle of the monster's box at its spawn origin.
func (x *Executor) spawnCenter(p *plan) Vec3 {
	o := p.point
	for _, c := range x.classes.ForClassname(p.class) {
		if c.Maxs != c.Mins {
			return add(o, Vec3{(c.Mins[0] + c.Maxs[0]) / 2, (c.Mins[1] + c.Maxs[1]) / 2, (c.Mins[2] + c.Maxs[2]) / 2})
		}
	}
	return o
}

// boxCenter is the middle of track t's box at origin o.
func boxCenter(t *worldmodel.Track, o Vec3) Vec3 {
	return add(o, Vec3{(t.Mins[0] + t.Maxs[0]) / 2, (t.Mins[1] + t.Maxs[1]) / 2, (t.Mins[2] + t.Maxs[2]) / 2})
}

// killTrack finds the track of kill step p's monster: the one the world
// model tied to its lump entity (by class and spawn origin at level entry,
// once seen), else the nearest track of its class near its spawn origin
// that is tied to no lump entity.
func (x *Executor) killTrack(p *plan) *worldmodel.Track {
	b := x.belief
	if b == nil {
		return nil
	}
	if t := b.TrackByLump(p.ent); t != nil {
		return t
	}
	names := map[string]bool{}
	for _, c := range x.classes.ForClassname(p.class) {
		names[c.Name] = true
	}
	var best *worldmodel.Track
	bd := float32(trackMatch)
	for i := range b.Tracks {
		t := &b.Tracks[i]
		if t.Lump >= 0 || !names[t.Class] || !t.PosKnown {
			continue
		}
		if d := dist3(t.Pos, p.point); d < bd {
			best, bd = t, d
		}
	}
	return best
}

// killed reports whether the belief shows kill step p's monster dead.
func (x *Executor) killed(p *plan) (bool, string) {
	b := x.belief
	if b == nil {
		return false, ""
	}
	if f := b.Effect(worldmodel.EffectMonsterDead, p.ent); f != nil {
		return true, "seen dead"
	}
	t := x.killTrack(p)
	if t == nil {
		return false, ""
	}
	switch {
	case t.Life == worldmodel.LifeDying && x.now-t.LifeAt >= dyingConfirm:
		return true, "seen dying"
	case t.Life != worldmodel.LifeAlive && t.Life != worldmodel.LifeDying:
		return true, "seen " + t.Life.String()
	}
	return false, ""
}

// runKill moves the bot to a firing position (a node with a line of fire
// at the monster within FireRange) and holds there while the bot's shoot
// executor fights, until the belief shows the monster dead.
func (x *Executor) runKill(p *plan) Directive {
	k := x.killOrder(p)
	d := Directive{Kill: k, Look: k.Pos, HasLook: true}
	if ok, why := x.killed(p); ok {
		x.complete(why)
		return d
	}
	if x.now > x.deadline {
		x.fail(fmt.Sprintf("%s still alive after %.0fs", p.desc, float64(x.now-x.steps[x.cur].Started)/1000))
		return Directive{Hold: true}
	}
	b := x.belief
	inRange := b != nil && dist3(b.Self.Eye, k.Pos) <= x.cfg.FireRange
	if k.Visible && inRange {
		// a clear shot: stand and fight
		x.killSeen = x.now
		if x.issued {
			x.nav.ClearGoal()
			x.issued = false
		}
		d.Hold = true
		return d
	}
	if x.killSeen != 0 && x.now-x.killSeen < occludedHold {
		d.Hold = true
		return d
	}
	st := x.nav.Status()
	approach := x.issued && (st.Follow == navrt.Arrived && x.now-x.killGoalAt > lookAround ||
		st.Follow == navrt.Failed && x.now-x.killGoalAt > lookAround)
	if !x.issued || dist3(x.killAnchor, k.Pos) > 96 || approach {
		at := k.Pos
		nodes := x.firingNodes(at, approach)
		if len(nodes) == 0 && k.ByEar {
			// hearing's stand-in has no firing position (it is only a side
			// and a loudness: past a wall, in one): go on from where the
			// monster was seen, looking where it sounded from
			at = k.Seen
			nodes = x.firingNodes(at, approach)
		}
		var goal navrt.Goal
		if len(nodes) > 0 {
			goal = navrt.NodeGoal(nodes...)
		} else {
			goal = navrt.PointGoal(at, 96)
		}
		if err := x.nav.SetGoal(goal, x.now); err != nil {
			if err := x.nav.SetGoal(navrt.PointGoal(at, 192), x.now); err != nil {
				x.fail(err.Error())
				return Directive{Hold: true}
			}
		}
		x.issued, x.killAnchor, x.killGoalAt = true, k.Pos, x.now
	}
	return d
}

// firingNodes returns standing nodes within FireRange of target with a
// clear line of fire (MASK_SHOT from the eye, in the static world and the
// believed blockers) at it, nearest to the bot first; close keeps only
// the nearer half of the range (walking up when the far ones did not
// show the monster).
func (x *Executor) firingNodes(target Vec3, close bool) []nav.NodeID {
	r := x.cfg.FireRange
	if close {
		r /= 2
	}
	if x.world != nil {
		solids := append(x.g.Solids[:len(x.g.Solids):len(x.g.Solids)], nil...)
		if ms := x.ms(); ms != nil {
			solids = ms.Solids(solids)
		}
		x.world.SetSolids(solids)
	}
	type cand struct {
		id nav.NodeID
		d  float32
	}
	var out []cand
	var self Vec3
	if x.belief != nil {
		self = x.belief.Self.Origin
	}
	tested := 0
	for _, c := range x.g.Nearby(target, r) {
		nd := &x.g.Nodes[c.Node]
		if nd.Flags&(nav.NodeCrouch|nav.NodeLadder|nav.NodeWater|nav.NodeMover) != 0 {
			continue
		}
		dt := dist3(nd.Origin, target)
		if dt > r || dt < minFireDist {
			continue
		}
		if tested++; tested > 512 {
			break
		}
		if !x.clearShot(add(nd.Origin, Vec3{0, 0, 22}), target) {
			continue
		}
		out = append(out, cand{c.Node, dist3(nd.Origin, self)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].d != out[j].d {
			return out[i].d < out[j].d
		}
		return out[i].id < out[j].id
	})
	ids := make([]nav.NodeID, 0, min(len(out), maxFireNodes))
	for i := 0; i < len(out) && i < maxFireNodes; i++ {
		ids = append(ids, out[i].id)
	}
	return ids
}

// clearShot reports whether a shot from eye reaches target.
func (x *Executor) clearShot(eye, target Vec3) bool {
	if x.world == nil {
		return true
	}
	tr := x.world.Trace(eye, Vec3{}, Vec3{}, target, q2const.MASK_SHOT)
	return !tr.StartSolid && (tr.Fraction == 1 || dist3(tr.EndPos, target) < 24)
}
