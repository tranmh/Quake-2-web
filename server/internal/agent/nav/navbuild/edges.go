package navbuild

import (
	"math"
	"sort"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

type candClass uint8

const (
	candNeighbor candClass = iota
	candLedge
	candLadder
	candWater
)

type cand struct {
	to    int32
	class candClass
}

// Candidate ranges.
const (
	neighborRadius = 56 // grid diagonal (45.3) plus the settle nudges
	neighborDZ     = 64
	waterShoreR    = 64
	ladderLinkR    = 48
	maxLedgeCands  = 24
	ledgeSectors   = 16
	ledgeBand      = 48
	// maxJumpRise is the highest a standing jump lifts the origin
	// (270² / 1600).
	maxJumpRise = 45.5
)

func (n *bnode) standing() bool { return n.flags&(nav.NodeLadder) == 0 && !n.swim() }

// swim nodes float in water; standing nodes in deep water are standing.
func (n *bnode) swim() bool { return n.flags&nav.NodeWater != 0 && n.prio == prioWater }

func (n *bnode) crouch() bool { return n.flags&nav.NodeCrouch != 0 }

func (n *bnode) arrive() navsim.ArriveMode {
	switch {
	case n.flags&nav.NodeLadder != 0:
		return navsim.ArriveAny
	case n.swim():
		return navsim.ArriveWater
	}
	return navsim.ArriveGround
}

// compatible reports whether an edge may join a and b: the same mover only
// at the same pose (rides join poses).
func compatible(a, b *bnode) bool {
	return a.blocker < 0 || b.blocker < 0 || a.blocker != b.blocker || a.pose == b.pose
}

// jumpReach is how far (horizontally) a running player can get while
// falling dz (negative: down) with or without a jump, plus a hull's margin.
func jumpReach(dz float32) float32 {
	best := float32(0)
	if d := 72900 - 1600*float64(dz); d >= 0 {
		best = float32(300 * (270 + math.Sqrt(d)) / 800)
	}
	if dz < 0 {
		best = max32(best, float32(300*math.Sqrt(float64(-dz)/400)))
	}
	return best + 40
}

// candidates lists the edges to try from node i, sorted.
func (wk *worker) candidates(i int32) []cand {
	b := wk.b
	a := &b.nodes[i]
	var out []cand
	switch {
	case a.flags&nav.NodeLadder != 0:
		b.near(a.o, ladderLinkR, func(j int32) {
			m := &b.nodes[j]
			if j == i || abs32(m.o[2]-a.o[2]) > neighborDZ {
				return
			}
			if m.flags&nav.NodeLadder != 0 {
				if m.ladder == a.ladder && abs32(m.o[2]-a.o[2]) <= 40 {
					out = append(out, cand{j, candLadder})
				}
				return
			}
			if m.standing() {
				out = append(out, cand{j, candLadder})
			}
		})
	case a.swim():
		r := b.p.WaterGrid * 1.75
		b.near(a.o, max32(r, waterShoreR), func(j int32) {
			m := &b.nodes[j]
			if j == i || !compatible(a, m) {
				return
			}
			if m.swim() {
				if dist3(a.o, m.o) <= r {
					out = append(out, cand{j, candWater})
				}
				return
			}
			if m.standing() && hdist(a.o, m.o) <= waterShoreR && abs32(m.o[2]-a.o[2]) <= neighborDZ {
				out = append(out, cand{j, candWater})
			}
		})
	default:
		b.near(a.o, neighborRadius, func(j int32) {
			m := &b.nodes[j]
			if j == i || !compatible(a, m) {
				return
			}
			dz := m.o[2] - a.o[2]
			switch {
			case m.flags&nav.NodeLadder != 0:
				if hdist(a.o, m.o) <= ladderLinkR && abs32(dz) <= neighborDZ {
					out = append(out, cand{j, candLadder})
				}
			case m.swim():
				if dz >= -128 && dz <= 32 {
					out = append(out, cand{j, candWater})
				}
			case abs32(dz) <= neighborDZ:
				out = append(out, cand{j, candNeighbor})
			}
		})
		out = append(out, wk.ledgeCands(i)...)
	}
	sort.Slice(out, func(x, y int) bool {
		if out[x].to != out[y].to {
			return out[x].to < out[y].to
		}
		return out[x].class < out[y].class
	})
	// one candidate per target (the first class wins)
	k := 0
	for x := range out {
		if k > 0 && out[k-1].to == out[x].to {
			continue
		}
		out[k] = out[x]
		k++
	}
	return out[:k]
}

// ledgeCands picks jump/drop targets from a ledge: per 22.5° sector and
// 48-unit height band the nearest node that is in a drop-off direction and
// within jumping reach, at most maxLedgeCands of them.
func (wk *worker) ledgeCands(i int32) []cand {
	b := wk.b
	a := &b.nodes[i]
	if a.ledge == 0 {
		return nil
	}
	type pick struct {
		j int32
		d float32
	}
	const bands = 24
	var best [ledgeSectors * bands]pick
	for k := range best {
		best[k].j = -1
	}
	rise := min32(b.p.MaxRise, maxJumpRise)
	b.near(a.o, b.p.LedgeReach, func(j int32) {
		m := &b.nodes[j]
		if j == i || m.flags&nav.NodeLadder != 0 || !compatible(a, m) {
			return
		}
		dz := m.o[2] - a.o[2]
		if dz < -b.p.MaxDrop || dz > rise {
			return
		}
		hd := hdist(a.o, m.o)
		if hd <= neighborRadius && dz >= -neighborDZ {
			return // a neighbor candidate
		}
		if hd > jumpReach(dz) {
			return
		}
		yaw, ok := navsim.YawTo(a.o, m.o)
		if ok {
			s8 := int(math.Floor(float64(yaw)/45+0.5)) % 8
			if a.ledge&(1<<uint(s8)|1<<uint((s8+1)%8)|1<<uint((s8+7)%8)) == 0 {
				return
			}
		}
		band := int(-dz / ledgeBand)
		if band < 0 {
			band = 0
		}
		if band >= bands {
			band = bands - 1
		}
		k := int(yaw/(360/ledgeSectors))%ledgeSectors*bands + band
		if p := &best[k]; p.j < 0 || hd < p.d || (hd == p.d && j < p.j) {
			*p = pick{j, hd}
		}
	})
	var picks []pick
	for _, p := range best {
		if p.j >= 0 {
			picks = append(picks, p)
		}
	}
	sort.Slice(picks, func(x, y int) bool {
		if picks[x].d != picks[y].d {
			return picks[x].d < picks[y].d
		}
		return picks[x].j < picks[y].j
	})
	if len(picks) > maxLedgeCands {
		picks = picks[:maxLedgeCands]
	}
	out := make([]cand, len(picks))
	for k, p := range picks {
		out[k] = cand{p.j, candLedge}
	}
	return out
}

// edgesFrom validates the candidates of node i.
func (wk *worker) edgesFrom(i int32) []nav.Edge {
	var out []nav.Edge
	for _, c := range wk.candidates(i) {
		wk.b.cands.Add(1)
		if e, ok := wk.validate(i, c); ok {
			out = append(out, e)
		}
	}
	return out
}

func supports(a, b *bnode) []support {
	s := a.sup()
	if b.blocker >= 0 && (a.blocker != b.blocker) {
		s = append(s, support{b.blocker, b.pose})
	}
	return s
}

// validate tries the candidate's recipes in cost order and returns the
// first edge that arrives.
func (wk *worker) validate(i int32, c cand) (nav.Edge, bool) {
	b := wk.b
	a, m := &b.nodes[i], &b.nodes[c.to]
	sup := supports(a, m)
	wk.setWorld(sup...)
	edge := nav.Edge{From: nav.NodeID(i), To: nav.NodeID(c.to)}
	if a.blocker >= 0 || m.blocker >= 0 {
		if a.blocker != m.blocker {
			edge.Flags |= nav.EdgeBoard
		}
	}
	dz := m.o[2] - a.o[2]

	if c.class == candNeighbor && a.standing() && m.standing() && a.flags&nav.NodeWater == 0 && m.flags&nav.NodeWater == 0 {
		if samples, step, ok := wk.fastPath(a, m); ok {
			b.fast.Add(1)
			d := pathLen(samples)
			speed := float32(300)
			edge.Kind, edge.Recipe = nav.EdgeWalk, navsim.RecipeWalk
			if a.crouch() || m.crouch() {
				speed, edge.Kind, edge.Recipe = 100, nav.EdgeCrouch, navsim.RecipeCrouch
			}
			edge.Flags |= nav.EdgeFast
			if step {
				edge.Flags |= nav.EdgeStep
			}
			edge.Cost = d / speed
			reqs, ok := wk.conds(samples, sup)
			if !ok {
				return edge, false
			}
			edge.Reqs = reqs
			edge.Effects = wk.sampleEffects(samples, 1/speed, sup)
			wk.markHazard(&edge, samples, edge.Cost/float32(max(len(samples)-1, 1)))
			return edge, true
		}
	}

	var plans []navsim.Plan
	base := navsim.Plan{From: a.o, Target: m.o, StepMsec: b.p.StepMsec}
	add := func(r navsim.Recipe, f func(p *navsim.Plan)) {
		p := base
		p.Recipe = r
		if f != nil {
			f(&p)
		}
		plans = append(plans, p)
	}
	switch c.class {
	case candNeighbor:
		if a.crouch() || m.crouch() {
			add(navsim.RecipeCrouch, nil)
		} else {
			add(navsim.RecipeWalk, nil)
		}
		if dz > navsim.StepHeight && dz <= maxJumpRise+2 {
			add(navsim.RecipeJump, func(p *navsim.Plan) { p.Takeoff = a.o })
			lip := wk.lip(a, m)
			if hdist(lip, a.o) > 4 {
				add(navsim.RecipeJump, func(p *navsim.Plan) { p.Takeoff = lip })
			}
		}
	case candLedge:
		// walking (off the ledge, or around a corner of it) is cheaper and
		// safer than jumping, so it is tried first
		add(navsim.RecipeWalk, nil)
		if dz <= -navsim.StepHeight {
			add(navsim.RecipeDrop, nil)
		}
		lip := wk.lip(a, m)
		add(navsim.RecipeJump, func(p *navsim.Plan) { p.Takeoff = lip })
		add(navsim.RecipeJump, func(p *navsim.Plan) { p.Takeoff = lip; p.BackupMsec = 300 })
	case candLadder:
		yaw := a.ladderYaw
		if a.flags&nav.NodeLadder == 0 {
			yaw = m.ladderYaw
		}
		add(navsim.RecipeLadder, func(p *navsim.Plan) { p.Yaw = yaw })
	case candWater:
		switch {
		case a.swim() && m.swim():
			add(navsim.RecipeSwim, nil)
		case a.swim():
			add(navsim.RecipeWaterJump, nil)
			add(navsim.RecipeSwim, nil)
			add(navsim.RecipeWalk, nil)
		default:
			add(navsim.RecipeWalk, nil)
			add(navsim.RecipeDrop, nil)
			// from the bottom of a pool: swim up to the swim node
			add(navsim.RecipeSwim, nil)
		}
	}
	base0 := edge
	for _, p := range plans {
		if !wk.simulate(a, m, p) {
			continue
		}
		edge := base0 // each attempt starts clean
		out := &wk.out
		edge.Recipe = p.Recipe
		edge.Kind = kindOf(p.Recipe, out, dz)
		edge.Cost = float32(out.Msec) / 1000
		edge.FallDamage = int16(min(out.FallDamage, math.MaxInt16))
		edge.BackupMsec = int16(p.BackupMsec)
		edge.Forward = p.Forward
		if p.Recipe == navsim.RecipeLadder {
			edge.Yaw = p.Yaw
		}
		switch {
		case p.Recipe == navsim.RecipeJump:
			// the jumper presses jump at the planned point
			edge.Takeoff, edge.TakeoffSpeed = p.Takeoff, out.TakeoffSpeed
		case out.TookOff && edge.Kind == nav.EdgeDrop:
			edge.Takeoff, edge.TakeoffSpeed = out.Takeoff, out.TakeoffSpeed
		}
		if p.Recipe == navsim.RecipeJump && !out.TookOff {
			continue
		}
		reqs, ok := wk.conds(out.Samples, sup)
		if !ok {
			continue
		}
		edge.Reqs = reqs
		edge.Effects = wk.outcomeEffects(out, sup)
		if wk.touchesPushable(out) {
			edge.Flags |= nav.EdgePushes
		}
		wk.markHazard(&edge, out.Samples, float32(b.p.StepMsec)/1000)
		return edge, true
	}
	return edge, false
}

func (wk *worker) touchesPushable(out *navsim.Outcome) bool {
	for _, c := range out.Touched {
		if wk.b.sc.pushable[c.ID] {
			return true
		}
	}
	return false
}

func kindOf(r navsim.Recipe, out *navsim.Outcome, dz float32) nav.EdgeKind {
	switch r {
	case navsim.RecipeCrouch:
		return nav.EdgeCrouch
	case navsim.RecipeJump:
		return nav.EdgeJump
	case navsim.RecipeDrop:
		return nav.EdgeDrop
	case navsim.RecipeLadder:
		return nav.EdgeLadder
	case navsim.RecipeSwim:
		return nav.EdgeSwim
	case navsim.RecipeWaterJump:
		return nav.EdgeWaterJump
	case navsim.RecipeRide:
		return nav.EdgeRide
	}
	if out.TookOff && dz < -navsim.StepHeight {
		return nav.EdgeDrop
	}
	return nav.EdgeWalk
}

// place puts the worker's player at node n at rest (one idle step, so it
// stands on ground with PMF_ON_GROUND like a player that got there).
func (wk *worker) place(n *bnode) {
	r := wk.r
	r.Reset(n.o, n.crouch())
	c := navsim.Cmd{}
	if n.crouch() {
		c.Up = -400
	}
	if n.flags&nav.NodeLadder != 0 {
		c.Yaw = n.ladderYaw // facing the ladder holds the player (nav.HoldCmd)
	}
	r.Step(c)
}

// simulate runs plan p from node a and reports whether it arrives at m.
// The outcome is left in wk.out.
func (wk *worker) simulate(a, m *bnode, p navsim.Plan) bool {
	wk.b.sims.Add(1)
	wk.place(a)
	limit := navsim.TimeLimitMsec(dist3(a.o, m.o)) + p.BackupMsec
	return runToArrival(wk.r, p.Executor(), m.o, m.arrive(), limit, &wk.out)
}

// runToArrival runs ex until the player arrives at target (true), falls
// past it, is stuck on the ground for 400 ms, is teleported, or the time
// limit passes.
func runToArrival(r *navsim.Runner, ex navsim.Executor, target Vec3, mode navsim.ArriveMode, limit int, out *navsim.Outcome) bool {
	stuck := 0
	arrived := false
	msec := r.Phys.StepMsec
	stop := func(s *navsim.State, res *navsim.StepResult) bool {
		if navsim.Arrived(s, target, mode) {
			arrived = true
			return true
		}
		o := s.Origin()
		if !s.OnGround() && s.WaterLevel == 0 && o[2] < target[2]-64 && s.PM.Velocity[2] < 0 {
			return true // fell past the target
		}
		if s.OnGround() && s.HSpeed() < 5 {
			stuck += msec
			if stuck >= 400 {
				return true
			}
		} else {
			stuck = 0
		}
		return res.Teleported != 0
	}
	r.Run(ex, stop, limit, out)
	return arrived
}

// lip marches from a towards m and returns the last point where the hull
// still has floor under it (the takeoff of a gap jump) or, when something
// blocks the way, the point in front of it (a jump up onto a ledge).
func (wk *worker) lip(a, m *bnode) Vec3 {
	mins, maxs := navsim.StandMins(), navsim.StandMaxs()
	hd := hdist(a.o, m.o)
	if hd < 1 {
		return a.o
	}
	dx, dy := (m.o[0]-a.o[0])/hd, (m.o[1]-a.o[1])/hd
	prev := a.o
	for s := float32(8); s <= hd; s += 8 {
		p := Vec3{a.o[0] + dx*s, a.o[1] + dy*s, a.o[2]}
		if tr := wk.w.Trace(prev, mins, maxs, p, q2const.MASK_PLAYERSOLID); tr.Fraction < 1 || tr.StartSolid {
			return prev
		}
		down := p
		down[2] -= 24
		if tr := wk.w.Trace(p, mins, maxs, down, q2const.MASK_PLAYERSOLID); tr.Fraction >= 1 {
			return prev
		}
		prev = p
	}
	return prev
}

// fastPath accepts a flat or one-step edge without simulating: a clear
// straight hull trace (or up 18, across, down) with walkable floor under
// the hull every 16 units and no deep water. It returns the swept samples
// (every 8 units).
func (wk *worker) fastPath(a, m *bnode) ([]navsim.Sample, bool, bool) {
	A, B := a.o, m.o
	if abs32(B[2]-A[2]) > navsim.StepHeight {
		return nil, false, false
	}
	ducked := a.crouch() || m.crouch()
	mins, maxs := navsim.StandMins(), navsim.StandMaxs()
	if ducked {
		maxs = navsim.DuckMaxs()
	}
	w := wk.w
	// a hull grown by a unit sideways must pass too: a hull that only
	// grazes something low makes pmove take its step-up path onto it
	wmins, wmaxs := Vec3{mins[0] - 1, mins[1] - 1, mins[2]}, Vec3{maxs[0] + 1, maxs[1] + 1, maxs[2]}
	tr := w.Trace(A, mins, maxs, B, q2const.MASK_PLAYERSOLID)
	if wide := w.Trace(A, wmins, wmaxs, B, q2const.MASK_PLAYERSOLID); !tr.StartSolid && tr.Fraction == 1 && !wide.StartSolid && wide.Fraction == 1 {
		// flat (or an even ramp): floor right under the line all the way,
		// so the walker never leaves the ground and cannot overshoot
		if !wk.floorAlong(A, B, mins, maxs, 4) {
			return nil, false, false
		}
		return lineSamples(nil, A, B, ducked), false, true
	}
	// only a real step up a vertical riser; anything else (going down,
	// where running makes pmove airborne, or a steep slope pmove refuses to
	// step onto) is simulated
	if n := tr.Plane.Normal[2]; tr.StartSolid || abs32(n) >= 0.01 || B[2] <= A[2]+2 {
		return nil, false, false
	}
	up := Vec3{A[0], A[1], A[2] + navsim.StepHeight}
	over := Vec3{B[0], B[1], A[2] + navsim.StepHeight}
	if over[2] < B[2] {
		return nil, false, false
	}
	if tr := w.Trace(A, mins, maxs, up, q2const.MASK_PLAYERSOLID); tr.StartSolid || tr.Fraction < 1 {
		return nil, false, false
	}
	if tr := w.Trace(up, wmins, wmaxs, over, q2const.MASK_PLAYERSOLID); tr.StartSolid || tr.Fraction < 1 {
		return nil, false, false
	}
	down := Vec3{B[0], B[1], B[2] - 4}
	tr = w.Trace(over, mins, maxs, down, q2const.MASK_PLAYERSOLID)
	if tr.StartSolid || tr.Fraction >= 1 || abs32(tr.EndPos[2]-B[2]) > 1 || tr.Plane.Normal[2] < 0.7 {
		return nil, false, false
	}
	// floor under the raised line: either the lower floor before the riser
	// or the upper one after it
	if !wk.floorAlong(up, over, mins, maxs, navsim.StepHeight+4) {
		return nil, false, false
	}
	s := lineSamples(nil, A, up, ducked)
	s = lineSamples(s, up, over, ducked)
	s = lineSamples(s, over, B, ducked)
	return s, true, true
}

// levelFloor is the floor normal the fast path accepts: ramps and slopes
// (where running can leave the ground, or jam on a crease) are simulated.
const levelFloor = 0.99

// floorAlong probes for level floor within depth below the hull every 16
// units from a to b (both included), and rejects deep water.
func (wk *worker) floorAlong(a, b, mins, maxs Vec3, depth float32) bool {
	d := dist3(a, b)
	n := int(d/16) + 1
	for k := 0; k <= n; k++ {
		t := float32(k) / float32(n)
		p := Vec3{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
		down := p
		down[2] -= depth
		tr := wk.w.Trace(p, mins, maxs, down, q2const.MASK_PLAYERSOLID)
		if tr.StartSolid || tr.Fraction >= 1 || tr.Plane.Normal[2] < levelFloor {
			return false
		}
		if wk.w.PointContents(Vec3{p[0], p[1], p[2] - 1})&q2const.MASK_WATER != 0 {
			return false
		}
		// no steep ground right under the center either (the hull can rest
		// on a flat edge above a slope pmove would slide off)
		c := Vec3{p[0], p[1], p[2] + mins[2]}
		cd := c
		cd[2] -= depth
		if ct := wk.w.Trace(c, Vec3{}, Vec3{}, cd, q2const.MASK_PLAYERSOLID); ct.Fraction < 1 && !ct.StartSolid && ct.Plane.Normal[2] < levelFloor {
			return false
		}
	}
	return true
}

// lineSamples appends samples every 8 units from a to b (a included when s
// is empty).
func lineSamples(s []navsim.Sample, a, b Vec3, ducked bool) []navsim.Sample {
	yaw, ok := navsim.YawTo(a, b)
	if !ok && len(s) > 0 {
		yaw = s[len(s)-1].Yaw
	}
	if len(s) == 0 {
		s = append(s, navsim.Sample{Origin: a, Ducked: ducked, OnGround: true, Yaw: yaw})
	}
	d := dist3(a, b)
	n := int(d/8) + 1
	for k := 1; k <= n; k++ {
		t := float32(k) / float32(n)
		s = append(s, navsim.Sample{Origin: Vec3{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t},
			Ducked: ducked, OnGround: true, Yaw: yaw})
	}
	return s
}

func pathLen(s []navsim.Sample) float32 {
	var d float32
	for i := 1; i < len(s); i++ {
		d += dist3(s[i-1].Origin, s[i].Origin)
	}
	return d
}

// buildEdges validates the candidates of every node, then adds rides.
func (b *builder) buildEdges() (int, error) {
	results := make([][]nav.Edge, len(b.nodes))
	if err := b.parallel(len(b.nodes), func(wk *worker, i int) {
		results[i] = wk.edgesFrom(int32(i))
	}); err != nil {
		return 0, err
	}
	var all []nav.Edge
	for _, r := range results {
		all = append(all, r...)
	}
	all = append(all, b.touchEdges...)
	all = append(all, b.rideEdges()...)
	tele, err := b.teleportEdges()
	if err != nil {
		return 0, err
	}
	all = append(all, tele...)
	sort.SliceStable(all, func(x, y int) bool {
		if all[x].From != all[y].From {
			return all[x].From < all[y].From
		}
		if all[x].To != all[y].To {
			return all[x].To < all[y].To
		}
		return all[x].Kind < all[y].Kind
	})
	b.edges = all
	return len(all), nil
}

// ridePairs returns the pose moves (from, to) a rider can ride on blocker
// bl: a train corner to corner along its path, and from the last corner
// back only when that corner leads somewhere (train_next stops at a corner
// without a target); other movers between consecutive poses both ways,
// except that a plat at its top never goes down with a rider inside its
// center trigger (Touch_Plat_Center keeps delaying plat_go_down) unless
// it is a LOW_TRIGGER plat, whose trigger covers just the bottom.
// C: game/g_func.c:1529 train_next, game/g_func.c:466 Touch_Plat_Center
func ridePairs(bl *nav.Blocker, g *blockerGeo) [][2]int {
	var pairs [][2]int
	switch bl.Kind {
	case nav.BlockTrain:
		for p := 0; p+1 < len(bl.Poses); p++ {
			pairs = append(pairs, [2]int{p, p + 1})
		}
		if last := len(bl.Poses) - 1; g.loopTo >= 0 && g.loopTo != last {
			pairs = append(pairs, [2]int{last, g.loopTo})
		}
	default:
		for p := 0; p+1 < len(bl.Poses); p++ {
			if !(bl.Kind == nav.BlockPlat && p == 0 && !g.lowTrigger) {
				pairs = append(pairs, [2]int{p, p + 1})
			}
			pairs = append(pairs, [2]int{p + 1, p})
		}
	}
	return pairs
}

// rideNeedsUse reports whether the move of blocker bl from pose a to pose
// b needs something to use the mover: a train waits at its first corner
// unless it starts on, and at a corner with wait -1; another mover moves by
// itself when a touch or nothing at all sets it off, and returns to its
// spawn pose by itself after a wait >= 0.
// C: game/g_func.c:1490 train_wait, game/g_func.c:1601 func_train_find
func rideNeedsUse(bl *nav.Blocker, g *blockerGeo, a, b int) bool {
	if bl.Kind == nav.BlockTrain {
		if a == 0 && g.act&mapdata.ActAuto == 0 {
			return true
		}
		return a < len(g.cornerWait) && g.cornerWait[a] < 0
	}
	if g.act&(mapdata.ActTouch|mapdata.ActAuto) != 0 {
		return false
	}
	return !(b == int(bl.Spawn) && g.wait >= 0)
}

// rideEdges joins the nodes of a mover at consecutive poses that stand on
// the same spot of the mover: door, plat and secret door poses both ways,
// train corners in path order.
func (b *builder) rideEdges() []nav.Edge {
	type key struct {
		blocker int32
		x, y, z int32
	}
	byPose := map[key][]int32{}
	for i := range b.nodes {
		n := &b.nodes[i]
		if n.blocker < 0 || n.flags&nav.NodeMover == 0 {
			continue
		}
		k := key{n.blocker, int32(math.Round(float64(n.local[0]))), int32(math.Round(float64(n.local[1]))), int32(math.Round(float64(n.local[2])))}
		byPose[k] = append(byPose[k], int32(i))
	}
	keys := make([]key, 0, len(byPose))
	for k := range byPose {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, c := keys[i], keys[j]
		if a.blocker != c.blocker {
			return a.blocker < c.blocker
		}
		if a.x != c.x {
			return a.x < c.x
		}
		if a.y != c.y {
			return a.y < c.y
		}
		return a.z < c.z
	})
	wk := b.workers[0]
	var out []nav.Edge
	for _, k := range keys {
		ids := byPose[k]
		bl := &b.sc.blockers[k.blocker]
		g := &b.sc.geo[k.blocker]
		at := func(pose int) int32 {
			for _, id := range ids {
				if int(b.nodes[id].pose) == pose {
					return id
				}
			}
			return -1
		}
		for _, pr := range ridePairs(bl, g) {
			from, to := at(pr[0]), at(pr[1])
			if from < 0 || to < 0 {
				continue
			}
			p0, p1 := bl.Poses[pr[0]].Origin, bl.Poses[pr[1]].Origin
			cost := g.travel
			if bl.Kind == nav.BlockTrain || cost <= 0 {
				sp := g.speed
				if sp <= 0 {
					sp = 100
				}
				cost = dist3(p0, p1) / sp
			}
			e := nav.Edge{From: nav.NodeID(from), To: nav.NodeID(to), Kind: nav.EdgeRide, Recipe: navsim.RecipeRide, Cost: cost,
				Reqs: []nav.Req{{Blocker: k.blocker, States: nav.Pose(pr[0])}}}
			if rideNeedsUse(bl, g, pr[0], pr[1]) {
				e.Flags |= nav.EdgeNeedsUse
				e.Target = bl.Entity
			}
			// the rider's box along the move
			o0 := b.nodes[from].o
			var s []navsim.Sample
			for t := 0; t <= 16; t++ {
				f := float32(t) / 16
				d := shared.VectorScale(shared.VectorSubtract(p1, p0), f)
				s = append(s, navsim.Sample{Origin: shared.VectorAdd(o0, d), OnGround: true})
			}
			e.Effects = wk.sampleEffects(s, cost/pathLen(s), nil) // the item on the mover moves along
			out = append(out, e)
		}
	}
	return out
}
