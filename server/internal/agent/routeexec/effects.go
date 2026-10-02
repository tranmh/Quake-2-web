package routeexec

import (
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
)

// verdict is what the belief says about a claimed effect.
type verdict uint8

const (
	// unseen: the belief cannot tell (not observed since it was caused,
	// or not observable at all: an exit, an enabled trigger).
	unseen verdict = iota
	// pending: it is under way (a door seen moving).
	pending
	// seen: the belief shows it.
	seen
	// contradicted: the belief shows it did not happen (the laser still
	// on, the door seen at rest where it started, well after the cause).
	contradicted
)

func (v verdict) String() string {
	switch v {
	case pending:
		return "pending"
	case seen:
		return "seen"
	case contradicted:
		return "contradicted"
	}
	return "unseen"
}

// observable reports effect kinds the belief can show at all.
func observable(k route.EffectKind) bool {
	switch k {
	case route.EffLaserOff, route.EffLaserOn, route.EffDoorOpen, route.EffMoverAt, route.EffRemove, route.EffWake:
		return true
	}
	return false
}

// contradictGrace is how long after the cause (plus the mover's travel)
// an observation of the old state still counts as "not yet" (ms).
const contradictGrace = 1500

// judge returns the belief's verdict on effect f, caused at since (ms).
func (x *Executor) judge(f *effect, b *worldmodel.Belief, since int64) verdict {
	if b == nil {
		return unseen
	}
	switch f.kind {
	case route.EffLaserOff, route.EffLaserOn:
		want, other := worldmodel.LaserOff, worldmodel.LaserOn
		eff := worldmodel.EffectLaserOff
		if f.kind == route.EffLaserOn {
			want, other, eff = worldmodel.LaserOn, worldmodel.LaserOff, worldmodel.EffectLaserOn
		}
		if e := b.Effect(eff, f.ent); e != nil && e.At >= since {
			return seen
		}
		l := b.Laser(f.ent)
		switch {
		case l == nil:
			return unseen
		case l.State == want:
			return seen
		case l.State == other && l.LastUpdate >= since+contradictGrace:
			return contradicted
		}
		return unseen
	case route.EffDoorOpen, route.EffMoverAt:
		return x.judgeMover(f, b, since)
	case route.EffRemove:
		if f.blocker >= 0 && x.ms() != nil {
			bb := x.ms().Belief(f.blocker)
			switch {
			case bb.Assumed:
			case bb.Status == navrt.BlockerGone:
				return seen
			case bb.Status == navrt.BlockerAt && bb.Seen >= since+contradictGrace:
				return contradicted
			}
		}
		return unseen
	case route.EffWake:
		if t := b.TrackByLump(f.ent); t != nil && (t.Awareness >= worldmodel.Alert || t.Life != worldmodel.LifeAlive) {
			return seen
		}
		return unseen
	}
	return unseen
}

// judgeMover judges a door opening (any pose but its spawn pose) or a
// mover arriving at a pose, from the navigator's blocker belief (which
// folds the world model's mover observations), else from the belief's
// mover directly.
func (x *Executor) judgeMover(f *effect, b *worldmodel.Belief, since int64) verdict {
	ms := x.ms()
	if f.blocker >= 0 && ms != nil && !ms.Belief(f.blocker).Assumed {
		bl := &x.g.Blockers[f.blocker]
		bb := ms.Belief(f.blocker)
		travel := int64(ms.Travel(f.blocker) * 1000)
		target := func(pose int) bool {
			if f.kind == route.EffMoverAt {
				return pose == f.pose
			}
			return pose != int(bl.Spawn)
		}
		switch bb.Status {
		case navrt.BlockerAt:
			if target(bb.Pose) && bb.Seen >= since-100 {
				return seen
			}
			if bb.Seen >= since+travel+contradictGrace {
				return contradicted
			}
			if target(bb.Pose) {
				// seen there before the cause: an old observation
				return unseen
			}
		case navrt.BlockerMoving:
			if bb.Seen >= since {
				return pending
			}
		}
		if e := b.Effect(worldmodel.EffectMoverMoved, f.ent); e != nil && e.At >= since {
			return pending
		}
		return unseen
	}
	// no blocker (a mover the graph keeps static): the belief's mover
	mv := b.Mover(f.model)
	if mv == nil || mv.LastUpdate < since {
		return unseen
	}
	switch {
	case mv.Moving:
		return pending
	case mv.Moved:
		return seen
	case mv.LastUpdate >= since+contradictGrace:
		return contradicted
	}
	return unseen
}
