package navbuild

import (
	"math"
	"sort"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Touch edges walk from a node at a button, a trigger or an item until the
// player touches it, then coast to rest, so every touchable thing near the
// graph has an edge whose effects include it. The spot where the player
// comes to rest becomes the edge's end: an existing node within the
// arrival tolerances, or a new end node.

const (
	touchRadius  = 160
	touchSources = 8
	endMatchXY   = navsim.ArriveXY
	endMatchZ    = navsim.ArriveZ
	// coastMsec bounds the coast to rest after the touch.
	coastMsec = 1500
)

// touchable is something touch edges aim at.
type touchable struct {
	button int // solid id of a button, or 0
	vol    int // volume index, or -1
	sup    []support
	min    Vec3
	max    Vec3
	entity int32
	kind   nav.EffectKind
}

func (b *builder) touchables() []touchable {
	var out []touchable
	for _, s := range b.sc.statics {
		if !b.sc.touchButtons[s.ID] {
			continue
		}
		mn, mx := s.AbsBox()
		out = append(out, touchable{button: s.ID, vol: -1, min: mn, max: mx, entity: int32(s.ID), kind: nav.EffButton})
	}
	for vi, v := range b.sc.vols {
		if v.kind != nav.EffTrigger && v.kind != nav.EffItem {
			continue
		}
		t := touchable{vol: vi, min: v.min, max: v.max, entity: v.entity, kind: v.kind}
		if v.blocker >= 0 {
			t.sup = []support{{v.blocker, v.pose}}
		}
		out = append(out, t)
	}
	return out
}

// boxDistXY is the horizontal distance from p to the box.
func boxDistXY(p, mn, mx Vec3) float32 {
	var d2 float32
	for k := 0; k < 2; k++ {
		var d float32
		if p[k] < mn[k] {
			d = mn[k] - p[k]
		} else if p[k] > mx[k] {
			d = p[k] - mx[k]
		}
		d2 += d * d
	}
	return float32(math.Sqrt(float64(d2)))
}

type pendingTouch struct {
	edge   nav.Edge
	end    Vec3
	endSup support
	ok     bool
	// leaned: a spawn-world run that touched a blocker (see spawnReqs)
	leaned bool
}

func (b *builder) buildTouchEnds() (int, error) {
	targets := b.touchables()
	type job struct {
		t   int
		src int32
	}
	var jobs []job
	for ti := range targets {
		t := &targets[ti]
		type c struct {
			i int32
			d float32
		}
		var cs []c
		center := Vec3{(t.min[0] + t.max[0]) / 2, (t.min[1] + t.max[1]) / 2, (t.min[2] + t.max[2]) / 2}
		b.near(center, touchRadius+max32(t.max[0]-t.min[0], t.max[1]-t.min[1])/2, func(i int32) {
			n := &b.nodes[i]
			if !n.standing() || n.flags&nav.NodeWater != 0 {
				return
			}
			if len(t.sup) > 0 && n.blocker == t.sup[0].b && n.pose != t.sup[0].pose {
				return // on the mover the target rides, at another pose
			}
			// the player box must be able to reach the target's height
			if n.o[2]-24-48 > t.max[2] || n.o[2]+32+48 < t.min[2] {
				return
			}
			d := boxDistXY(n.o, t.min, t.max)
			if d > touchRadius {
				return
			}
			cs = append(cs, c{i, d})
		})
		sort.Slice(cs, func(x, y int) bool {
			if cs[x].d != cs[y].d {
				return cs[x].d < cs[y].d
			}
			return cs[x].i < cs[y].i
		})
		if len(cs) > touchSources {
			cs = cs[:touchSources]
		}
		for _, x := range cs {
			jobs = append(jobs, job{ti, x.i})
		}
	}
	per := make([][]pendingTouch, len(jobs))
	if err := b.parallel(len(jobs), func(wk *worker, i int) {
		per[i] = wk.touchSim(&targets[jobs[i].t], jobs[i].src)
	}); err != nil {
		return 0, err
	}
	var res []pendingTouch
	for _, p := range per {
		res = append(res, p...)
	}

	// resolve the ends: an existing node close by, else a new end node
	var ends []bnode
	for i := range res {
		p := &res[i]
		if !p.ok {
			continue
		}
		if j := b.matchNode(p.end, p.endSup); j >= 0 {
			p.edge.To = nav.NodeID(j)
			continue
		}
		n := bnode{o: p.end, flags: nav.NodeEnd, blocker: p.endSup.b, pose: p.endSup.pose, ladder: -1, spawn: -1, prio: prioEnd}
		if n.blocker >= 0 {
			n.flags |= nav.NodeMover
			n.local = subV(n.o, b.sc.blockers[n.blocker].Poses[n.pose].Origin)
		}
		ends = append(ends, n)
	}
	ends = dedupe(ends)
	base := len(b.nodes)
	nodes := append(b.nodes, ends...)
	b.setNodes(nodes)
	for i := range res {
		p := &res[i]
		if !p.ok {
			continue
		}
		if p.edge.To < 0 {
			j := b.matchNodeFrom(p.end, p.endSup, base)
			if j < 0 {
				continue
			}
			p.edge.To = nav.NodeID(j)
		}
		// a self loop is fine: walking at the aim point sets the target off
		// and leaves the player where it started
		b.touchEdges = append(b.touchEdges, p.edge)
	}
	return len(b.touchEdges), nil
}

func subV(a, c Vec3) Vec3 { return Vec3{a[0] - c[0], a[1] - c[1], a[2] - c[2]} }

// matchNode returns the nearest node within the end tolerances on the same
// support, or -1.
func (b *builder) matchNode(p Vec3, sup support) int32 { return b.matchNodeFrom(p, sup, 0) }

func (b *builder) matchNodeFrom(p Vec3, sup support, from int) int32 {
	best, bd := int32(-1), float32(0)
	b.near(p, endMatchXY, func(j int32) {
		if int(j) < from {
			return
		}
		n := &b.nodes[j]
		if n.blocker != sup.b || (sup.b >= 0 && n.pose != sup.pose) || !n.standing() || abs32(n.o[2]-p[2]) > endMatchZ {
			return
		}
		if d := dist3(n.o, p); best < 0 || d < bd || (d == bd && j < best) {
			best, bd = j, d
		}
	})
	return best
}

// touchSim walks from node src at the target until it is touched: first
// with the movers in their spawn state (a trigger in front of a closed door
// is touched by walking up to the door), then with them out of the way.
// A spawn-world run that touched a blocker needs that blocker in its spawn
// state (it rests against a closed door), so the out-of-the-way run is
// kept as well for the other states.
func (wk *worker) touchSim(t *touchable, src int32) []pendingTouch {
	sup := wk.b.nodes[src].sup()
	for _, x := range t.sup {
		if len(sup) == 0 || sup[0].b != x.b {
			sup = append(sup, x)
		}
	}
	wk.setSpawnWorld(sup...)
	p := wk.touchSimIn(t, src, sup, true)
	if p.ok && !p.leaned {
		return []pendingTouch{p}
	}
	wk.setWorld(sup...)
	q := wk.touchSimIn(t, src, sup, false)
	var out []pendingTouch
	for _, x := range []pendingTouch{p, q} {
		if x.ok {
			out = append(out, x)
		}
	}
	return out
}

// touchSimIn simulates one touch edge in the worker's current world;
// spawnWorld marks a world with the other blockers in their spawn state.
func (wk *worker) touchSimIn(t *touchable, src int32, sup []support, spawnWorld bool) pendingTouch {
	b := wk.b
	a := &b.nodes[src]
	// aim at the closest point of the target box at the player's height
	// (buttons) or at its center (volumes)
	aim := Vec3{clamp32(a.o[0], t.min[0], t.max[0]), clamp32(a.o[1], t.min[1], t.max[1]), a.o[2]}
	if t.button == 0 {
		aim = Vec3{(t.min[0] + t.max[0]) / 2, (t.min[1] + t.max[1]) / 2, a.o[2]}
	}
	wk.b.sims.Add(1)
	wk.place(a)
	plan := navsim.Plan{Recipe: navsim.RecipeWalk, From: a.o, Target: aim, StepMsec: b.p.StepMsec}
	if a.crouch() {
		plan.Recipe = navsim.RecipeCrouch
	}
	hit := false
	stuck := 0
	stop := func(s *navsim.State, res *navsim.StepResult) bool {
		if res.Teleported != 0 {
			return true
		}
		if !hit {
			if t.button != 0 {
				hit = wk.out.HasTouched(t.button)
			} else {
				hit = wk.out.HasVolume(t.vol)
			}
		}
		if hit {
			return navsim.AtRest(s) // coasting after the touch
		}
		if s.OnGround() && s.HSpeed() < 5 {
			stuck += b.p.StepMsec
			return stuck >= 300
		}
		stuck = 0
		return false
	}
	wk.r.Run(navsim.Coast(plan.Executor(), &hit), stop, navsim.TimeLimitMsec(dist3(a.o, aim))+500+coastMsec, &wk.out)
	st := wk.r.StatePtr()
	if !hit || !navsim.AtRest(st) || wk.out.Teleported != 0 {
		return pendingTouch{}
	}
	reqs, ok := wk.conds(wk.out.Samples, sup)
	if !ok {
		return pendingTouch{}
	}
	leaned := false
	if spawnWorld {
		// the run leans on blockers in their spawn state: it needs them so
		if reqs, leaned, ok = wk.spawnReqs(reqs, sup); !ok {
			return pendingTouch{}
		}
	}
	e := nav.Edge{From: nav.NodeID(src), To: -1, Kind: nav.EdgeTouch, Recipe: plan.Recipe, Aim: aim, Target: t.entity,
		Cost: float32(wk.out.Msec) / 1000, Reqs: reqs, Effects: wk.outcomeEffects(&wk.out, sup)}
	if spawnWorld {
		e.Flags |= nav.EdgeSpawnWorld
	}
	if wk.touchesPushable(&wk.out) {
		e.Flags |= nav.EdgePushes
	}
	wk.markHazard(&e, wk.out.Samples, float32(b.p.StepMsec)/1000)
	end := support{-1, 0}
	if bi, ok := b.sc.blockerByID[st.Ground]; ok {
		end = support{bi, -1}
		for _, s := range sup {
			if s.b == bi {
				end.pose = s.pose
			}
		}
		if end.pose < 0 {
			return pendingTouch{}
		}
	}
	return pendingTouch{edge: e, end: st.Origin(), endSup: end, ok: true, leaned: leaned}
}

// spawnReqs adds to reqs a condition "in its spawn state" for every solid
// blocker (not a support) the last run touched; leaned reports that there
// was one, ok is false when that contradicts a condition reqs has.
func (wk *worker) spawnReqs(reqs []nav.Req, sup []support) (out []nav.Req, leaned, ok bool) {
	sc := wk.b.sc
	for _, c := range wk.out.Touched {
		bi, ok := sc.blockerByID[c.ID]
		if !ok || !sc.geo[bi].solid || sc.blockers[bi].Spawn < 0 {
			continue
		}
		isSup := false
		for _, s := range sup {
			isSup = isSup || s.b == bi
		}
		if isSup {
			continue
		}
		leaned = true
		want := sc.blockers[bi].SpawnState()
		found := false
		for k := range reqs {
			if reqs[k].Blocker == bi {
				if reqs[k].States &= want; reqs[k].States == 0 {
					return nil, true, false
				}
				found = true
			}
		}
		if !found {
			reqs = append(reqs, nav.Req{Blocker: bi, States: want})
		}
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Blocker < reqs[j].Blocker })
	return reqs, leaned, true
}

func clamp32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// teleportEdges walks from nearby nodes into each teleporter and links to
// the node where the destination puts the player.
func (b *builder) teleportEdges() ([]nav.Edge, error) {
	type job struct {
		t   int
		src int32
	}
	var jobs []job
	for ti, t := range b.sc.teles {
		c := Vec3{(t.min[0] + t.max[0]) / 2, (t.min[1] + t.max[1]) / 2, t.min[2]}
		var srcs []int32
		b.near(c, 128, func(i int32) {
			if n := &b.nodes[i]; n.standing() && n.blocker < 0 && abs32(n.o[2]-c[2]) < 64 {
				srcs = append(srcs, i)
			}
		})
		sort.Slice(srcs, func(x, y int) bool {
			dx, dy := hdist(b.nodes[srcs[x]].o, c), hdist(b.nodes[srcs[y]].o, c)
			if dx != dy {
				return dx < dy
			}
			return srcs[x] < srcs[y]
		})
		if len(srcs) > touchSources {
			srcs = srcs[:touchSources]
		}
		for _, s := range srcs {
			jobs = append(jobs, job{ti, s})
		}
	}
	res := make([]nav.Edge, len(jobs))
	okv := make([]bool, len(jobs))
	err := b.parallel(len(jobs), func(wk *worker, i int) {
		t := b.sc.teles[jobs[i].t]
		a := &b.nodes[jobs[i].src]
		wk.setWorld()
		aim := Vec3{(t.min[0] + t.max[0]) / 2, (t.min[1] + t.max[1]) / 2, a.o[2]}
		wk.place(a)
		plan := navsim.Plan{Recipe: navsim.RecipeWalk, From: a.o, Target: aim, StepMsec: b.p.StepMsec}
		ex := plan.Executor()
		tele := false
		wk.r.Run(ex, func(s *navsim.State, r *navsim.StepResult) bool { tele = r.Teleported != 0; return tele },
			navsim.TimeLimitMsec(dist3(a.o, aim))+500, &wk.out)
		if !tele {
			return
		}
		msec := wk.out.Msec
		samples := append([]navsim.Sample(nil), wk.out.Samples...)
		for k := 0; k < 80 && !(wk.r.StatePtr().OnGround() && wk.r.StatePtr().PM.PmTime == 0); k++ {
			wk.r.Step(navsim.Cmd{})
			msec += b.p.StepMsec
		}
		j := b.matchNode(wk.r.StatePtr().Origin(), support{-1, 0})
		if j < 0 {
			return
		}
		reqs, ok := wk.conds(samples[:len(samples)-1], nil)
		if !ok {
			return
		}
		res[i] = nav.Edge{From: nav.NodeID(jobs[i].src), To: nav.NodeID(j), Kind: nav.EdgeTeleport, Recipe: navsim.RecipeWalk,
			Aim: aim, Cost: float32(msec) / 1000, Reqs: reqs, Target: t.entity,
			Effects: []nav.Effect{{Kind: nav.EffTrigger, Entity: t.entity, T: float32(wk.out.Msec) / 1000, Pose: -1, Blocker: -1}}}
		okv[i] = true
	})
	var out []nav.Edge
	for i := range res {
		if okv[i] {
			out = append(out, res[i])
		}
	}
	return out, err
}
