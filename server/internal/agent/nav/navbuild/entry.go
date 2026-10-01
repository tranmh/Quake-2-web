package navbuild

import (
	"math"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Running entries and fragility. Every edge is validated from rest exactly
// at its start node, but a follower pursuing a path does not stop between
// edges: it switches to the next edge as soon as it arrives (within the
// arrival tolerance) at full speed, and it is never exactly on a node.
// The entry stage replays both for every simulated edge:
//   - it runs a walk edge into the start node from rest, then the edge
//     right away from the moving state, once per incoming direction; when
//     one of these chains does not arrive the edge gets nav.EdgeFromRest,
//     which tells the follower to stop at From first (navsim.StopAt);
//   - it runs the edge from rest a fraction of a unit away from From in
//     four directions; when one of them does not arrive the edge gets
//     nav.EdgeFragile (it only works from an exact start, which a real
//     player does not reproduce: a planner should avoid it).

// entrySectors splits the incoming directions; one in-edge per sector is
// replayed.
const entrySectors = 8

// entryChecked reports whether edge e from node a gets the running-entry
// check: simulated edges a running player starts on the ground (fast-path
// walks are straight level runs, swims and the special edges start from
// rest or in water by nature).
func entryChecked(e *nav.Edge, a *bnode) bool {
	if e.Flags&nav.EdgeFast != 0 || !a.standing() || a.flags&nav.NodeWater != 0 {
		return false
	}
	switch e.Kind {
	case nav.EdgeWalk, nav.EdgeCrouch, nav.EdgeJump, nav.EdgeDrop, nav.EdgeLadder:
		return true
	}
	return false
}

// entryFeeder reports whether edge e can bring a running player into its
// end: a walk or crouch between standing dry nodes.
func entryFeeder(e *nav.Edge, nodes []bnode) bool {
	if e.Kind != nav.EdgeWalk && e.Kind != nav.EdgeCrouch {
		return false
	}
	f, t := &nodes[e.From], &nodes[e.To]
	return f.standing() && t.standing() && f.flags&nav.NodeWater == 0 && t.flags&nav.NodeWater == 0 && e.From != e.To
}

// checkEntries caps the degree (so the in-edges replayed are the final
// ones) and flags the edges that fail from a running entry (EdgeFromRest)
// or from a start a fraction of a unit off (EdgeFragile).
func (b *builder) checkEntries() (int, error) {
	b.capDegree()
	in := make([][]int32, len(b.nodes))
	for i := range b.edges {
		if e := &b.edges[i]; entryFeeder(e, b.nodes) {
			in[e.To] = append(in[e.To], int32(i))
		}
	}
	var jobs []int
	for i := range b.edges {
		if e := &b.edges[i]; entryChecked(e, &b.nodes[e.From]) {
			jobs = append(jobs, i)
		}
	}
	type verdict struct{ fragile, fromRest, stopFails bool }
	res := make([]verdict, len(jobs))
	err := b.parallel(len(jobs), func(wk *worker, k int) {
		out := &b.edges[jobs[k]]
		v := &res[k]
		v.fragile = wk.fragile(out)
		for _, j := range b.entrySources(out, in[out.From]) {
			if ok, ran := wk.chain(&b.edges[j], out, false); ran && !ok {
				v.fromRest = true
				if ok, ran := wk.chain(&b.edges[j], out, true); ran && !ok {
					v.stopFails = true
					return
				}
			}
		}
	})
	if err != nil {
		return 0, err
	}
	for k, v := range res {
		e := &b.edges[jobs[k]]
		if v.fragile {
			e.Flags |= nav.EdgeFragile
			b.rep.Fragile++
		}
		if v.fromRest {
			e.Flags |= nav.EdgeFromRest
			b.rep.FromRest++
		}
		if v.stopFails {
			b.rep.StopFails++
		}
	}
	return b.rep.FromRest + b.rep.Fragile, nil
}

// entrySources picks the in-edges to replay before out: per incoming
// direction sector the first one (edges are sorted, so the choice is
// deterministic), skipping the edge's own reverse.
func (b *builder) entrySources(out *nav.Edge, in []int32) []int32 {
	var pick [entrySectors]int32
	for k := range pick {
		pick[k] = -1
	}
	a := b.nodes[out.From].o
	for _, j := range in {
		e := &b.edges[j]
		if e.From == out.To {
			continue
		}
		yaw, ok := navsim.YawTo(b.nodes[e.From].o, a)
		if !ok {
			continue
		}
		s := int(math.Floor(float64(yaw)/(360/entrySectors)+0.5)) % entrySectors
		if pick[s] < 0 {
			pick[s] = j
		}
	}
	var srcs []int32
	for _, j := range pick {
		if j >= 0 {
			srcs = append(srcs, j)
		}
	}
	return srcs
}

// fragileOffset is how far from From the fragility test starts an edge:
// inside navsim.StopRadius, so a follower that stopped at From can be
// that far off.
const fragileOffset = 0.75

// fragile reports whether edge e fails from rest at a point within
// fragileOffset of its start (four points around it; the ones where the
// hull does not fit are skipped).
func (wk *worker) fragile(e *nav.Edge) bool {
	b := wk.b
	a, m := &b.nodes[e.From], &b.nodes[e.To]
	wk.setWorld(supports(a, m)...)
	p := b.plan(e)
	limit := navsim.TimeLimitMsec(dist3(a.o, m.o)) + p.BackupMsec
	maxs := navsim.StandMaxs()
	if a.crouch() {
		maxs = navsim.DuckMaxs()
	}
	for _, d := range [4][2]float32{{fragileOffset, 0}, {-fragileOffset, 0}, {0, fragileOffset}, {0, -fragileOffset}} {
		o := Vec3{a.o[0] + d[0], a.o[1] + d[1], a.o[2]}
		if !wk.w.Fits(o, navsim.StandMins(), maxs) {
			continue
		}
		wk.b.sims.Add(1)
		n := *a
		n.o = o
		wk.place(&n)
		if !runToArrival(wk.r, p.Executor(), m.o, m.arrive(), limit, &wk.out) {
			return true
		}
	}
	return false
}

// plan returns the executor plan of a builder edge (as nav.Graph.Plan does
// for a graph edge).
func (b *builder) plan(e *nav.Edge) navsim.Plan {
	p := navsim.Plan{Recipe: e.Recipe, From: b.nodes[e.From].o, Target: b.nodes[e.To].o, Takeoff: e.Takeoff,
		Forward: e.Forward, BackupMsec: int(e.BackupMsec), Yaw: e.Yaw, StepMsec: b.p.StepMsec}
	if e.Aim != (Vec3{}) {
		p.Target = e.Aim
	}
	return p
}

// chain runs edge in from rest and then edge out from wherever in arrived,
// without stopping (or, with stop, after navsim.StopAt brought the player
// to rest at out's start). ran is false when the chain could not be set
// up (the two edges need one mover at different poses, or in does not
// arrive in this world); ok reports that out arrived.
func (wk *worker) chain(in, out *nav.Edge, stop bool) (ok, ran bool) {
	b := wk.b
	s, a, m := &b.nodes[in.From], &b.nodes[out.From], &b.nodes[out.To]
	sup, fits := mergeSupports(supports(s, a), supports(a, m))
	if !fits {
		return false, false
	}
	wk.setWorld(sup...)
	wk.b.sims.Add(1)
	wk.place(s)
	pin := b.plan(in)
	if !runToArrival(wk.r, pin.Executor(), a.o, a.arrive(), navsim.TimeLimitMsec(dist3(s.o, a.o)), &wk.out) {
		return false, false
	}
	if stop {
		wk.r.Run(navsim.StopAt(a.o, b.p.StepMsec), func(s *navsim.State, _ *navsim.StepResult) bool {
			return navsim.Stopped(s, a.o)
		}, 2000, &wk.out)
		if !navsim.Stopped(wk.r.StatePtr(), a.o) {
			return false, true
		}
	}
	p := b.plan(out)
	limit := navsim.TimeLimitMsec(dist3(a.o, m.o)) + p.BackupMsec
	return runToArrival(wk.r, p.Executor(), m.o, m.arrive(), limit, &wk.out), true
}

// mergeSupports joins two support lists; fits is false when they need one
// blocker at two poses.
func mergeSupports(x, y []support) (out []support, fits bool) {
	out = append(out, x...)
next:
	for _, t := range y {
		for _, u := range out {
			if u.b == t.b {
				if u.pose != t.pose {
					return nil, false
				}
				continue next
			}
		}
		out = append(out, t)
	}
	return out, true
}
