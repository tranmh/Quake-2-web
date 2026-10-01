package navbuild

import (
	"math"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// Running entries. Every edge is validated from rest at its start node,
// but a follower pursuing a path does not stop between edges: it switches
// to the next edge as soon as it arrives (within the arrival tolerance) at
// full speed. The entry stage replays that for every simulated edge: it
// runs a walk edge into the start node from rest, then the edge right
// away from the moving state, once per incoming direction. When one of
// these chains does not arrive the edge gets nav.EdgeFromRest, which tells
// the follower to stop at From first.

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
// ones) and flags the edges that fail from a running entry.
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
		if e := &b.edges[i]; entryChecked(e, &b.nodes[e.From]) && len(in[e.From]) > 0 {
			jobs = append(jobs, i)
		}
	}
	// 0: fine, 1: from rest, 2: not even after stopping at From
	verdict := make([]uint8, len(jobs))
	err := b.parallel(len(jobs), func(wk *worker, k int) {
		out := &b.edges[jobs[k]]
		for _, j := range b.entrySources(out, in[out.From]) {
			if ok, ran := wk.chain(&b.edges[j], out, false); ran && !ok {
				verdict[k] = 1
				if ok, ran := wk.chain(&b.edges[j], out, true); ran && !ok {
					verdict[k] = 2
					return
				}
			}
		}
	})
	if err != nil {
		return 0, err
	}
	for k, v := range verdict {
		if v > 0 {
			b.edges[jobs[k]].Flags |= nav.EdgeFromRest
			b.rep.FromRest++
		}
		if v > 1 {
			b.rep.StopFails++
		}
	}
	return b.rep.FromRest, nil
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
			return navsim.Stopped(s, a.o) || !s.OnGround()
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
