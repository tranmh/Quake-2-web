package navbuild

import (
	"sort"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Conditions and effects of an edge, from the hull positions it sweeps.

// sweepShrink is how much the hull shrinks for the blocker tests: none.
// The swept positions come from a world without the blocker, so the real
// hull touching it at all means pmove would collide with it there (pmove's
// own resting and sliding positions keep a positive gap, and a trace
// parallel to a face at a positive distance does not hit it).
const sweepShrink = 0

func sampleHull(s *navsim.Sample, shrink float32) (Vec3, Vec3) {
	mins, maxs := s.Mins(), s.Maxs()
	for k := 0; k < 3; k++ {
		mins[k] += shrink
		maxs[k] -= shrink
	}
	return mins, maxs
}

// conds returns the edge's conditions: the support poses it was validated
// on, plus, for every other blocker the swept hull comes near, the states
// that leave the sweep clear (when not all of them). ok is false when no
// state does.
func (wk *worker) conds(samples []navsim.Sample, sup []support) ([]nav.Req, bool) {
	b := wk.b
	var smin, smax Vec3
	for i := range samples {
		mins, maxs := sampleHull(&samples[i], 0)
		o := samples[i].Origin
		for k := 0; k < 3; k++ {
			lo, hi := o[k]+mins[k], o[k]+maxs[k]
			if i == 0 || lo < smin[k] {
				smin[k] = lo
			}
			if i == 0 || hi > smax[k] {
				smax[k] = hi
			}
		}
	}
	var reqs []nav.Req
	for bi := range b.sc.geo {
		isSup := false
		for _, s := range sup {
			if int(s.b) == bi {
				reqs = append(reqs, nav.Req{Blocker: int32(bi), States: nav.Pose(int(s.pose))})
				isSup = true
			}
		}
		if isSup {
			continue
		}
		g := &b.sc.geo[bi]
		if !boxesOverlap(smin, smax, g.umin, g.umax) {
			continue
		}
		bl := &b.sc.blockers[bi]
		var mask nav.StateMask
		for k := range bl.Poses {
			if !wk.hitsPose(bi, k, samples) {
				mask |= nav.Pose(k)
			}
		}
		if bl.Gone {
			mask |= nav.StateGone
		}
		if mask == bl.All() {
			continue
		}
		if mask == 0 {
			return nil, false
		}
		reqs = append(reqs, nav.Req{Blocker: int32(bi), States: mask})
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Blocker < reqs[j].Blocker })
	return reqs, true
}

func boxesOverlap(amin, amax, bmin, bmax Vec3) bool {
	return !(amin[0] > bmax[0] || amin[1] > bmax[1] || amin[2] > bmax[2] ||
		amax[0] < bmin[0] || amax[1] < bmin[1] || amax[2] < bmin[2])
}

// hitsPose reports whether the swept hull overlaps blocker bi at pose k: a
// laser segment crossing a hull box, or the brush model at that pose
// blocking a hull trace between consecutive samples or a position test at
// a sample. Both are needed: a moving trace that ends exactly touching a
// face passes (CM_RecursiveHullCheck keeps it on the front side of the
// node), while the position test PM_SnapPosition runs there calls it solid
// and leaves the player stuck.
func (wk *worker) hitsPose(bi, k int, samples []navsim.Sample) bool {
	b := wk.b
	g := &b.sc.geo[bi]
	pmin, pmax := g.poseMin[k], g.poseMax[k]
	if g.laser {
		for i := range samples {
			j := i - 1
			if j < 0 {
				j = 0
			}
			lo, hi := segBox(&samples[j], &samples[i], 0)
			if segmentBox(g.start, g.end, toD(lo), toD(hi)) {
				return true
			}
		}
		return false
	}
	pose := b.sc.blockers[bi].Poses[k]
	cs := wk.w.State()
	for i := range samples {
		j := i - 1
		if j < 0 {
			j = 0
		}
		lo, hi := segBox(&samples[j], &samples[i], sweepShrink)
		if !boxesOverlap(lo, hi, pmin, pmax) {
			continue
		}
		mins, maxs := sampleHull(&samples[i], sweepShrink)
		tr := cs.TransformedBoxTrace(samples[j].Origin, samples[i].Origin, mins, maxs, g.headnode, g.mask, pose.Origin, pose.Angles)
		if tr.StartSolid || tr.AllSolid || tr.Fraction < 1 {
			return true
		}
		if j != i {
			if tr := cs.TransformedBoxTrace(samples[i].Origin, samples[i].Origin, mins, maxs, g.headnode, g.mask, pose.Origin, pose.Angles); tr.AllSolid {
				return true
			}
		}
	}
	return false
}

// segBox is the box swept by the hull from sample a to sample b.
func segBox(a, b *navsim.Sample, shrink float32) (Vec3, Vec3) {
	mins, maxs := sampleHull(b, shrink)
	var lo, hi Vec3
	for k := 0; k < 3; k++ {
		lo[k] = min32(a.Origin[k], b.Origin[k]) + mins[k]
		hi[k] = max32(a.Origin[k], b.Origin[k]) + maxs[k]
	}
	return lo, hi
}

// outcomeEffects turns a simulation's volume entries and button contacts
// into effects.
func (wk *worker) outcomeEffects(out *navsim.Outcome) []nav.Effect {
	b := wk.b
	sec := float32(b.p.StepMsec) / 1000
	var eff []nav.Effect
	for _, v := range out.Volumes {
		vo := &b.sc.vols[v.ID]
		eff = addEffect(eff, nav.Effect{Kind: vo.kind, Entity: vo.entity, Yaw: v.Yaw, T: float32(v.Step) * sec, Pose: vo.pose, Blocker: vo.blocker})
	}
	for _, c := range out.Touched {
		if !b.sc.buttons[c.ID] {
			continue
		}
		yaw := float32(0)
		if c.Step-1 < len(out.Cmds) && c.Step > 0 {
			yaw = out.Cmds[c.Step-1].Yaw
		}
		eff = addEffect(eff, nav.Effect{Kind: nav.EffButton, Entity: int32(c.ID), Yaw: yaw, T: float32(c.Step) * sec, Pose: -1, Blocker: -1})
	}
	return sortEffects(eff)
}

// sampleEffects tests synthesized samples (fast-path walks, rides) against
// the volumes; secPerUnit converts path length to time.
func (wk *worker) sampleEffects(samples []navsim.Sample, secPerUnit float32) []nav.Effect {
	b := wk.b
	var eff []nav.Effect
	var d float32
	for i := range samples {
		if i > 0 {
			d += dist3(samples[i-1].Origin, samples[i].Origin)
		}
		mins, maxs := sampleHull(&samples[i], 0)
		o := samples[i].Origin
		var lo, hi Vec3
		for k := 0; k < 3; k++ {
			lo[k], hi[k] = o[k]+mins[k]-1, o[k]+maxs[k]+1
		}
		for vi := range b.sc.vols {
			vo := &b.sc.vols[vi]
			if boxesOverlap(lo, hi, vo.min, vo.max) {
				eff = addEffect(eff, nav.Effect{Kind: vo.kind, Entity: vo.entity, Yaw: samples[i].Yaw, T: d * secPerUnit, Pose: vo.pose, Blocker: vo.blocker})
			}
		}
	}
	return sortEffects(eff)
}

func addEffect(eff []nav.Effect, e nav.Effect) []nav.Effect {
	for _, x := range eff {
		if x.Kind == e.Kind && x.Entity == e.Entity && x.Pose == e.Pose {
			return eff
		}
	}
	return append(eff, e)
}

func sortEffects(eff []nav.Effect) []nav.Effect {
	sort.SliceStable(eff, func(i, j int) bool {
		if eff[i].T != eff[j].T {
			return eff[i].T < eff[j].T
		}
		if eff[i].Entity != eff[j].Entity {
			return eff[i].Entity < eff[j].Entity
		}
		return eff[i].Kind < eff[j].Kind
	})
	return eff
}
