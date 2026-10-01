package navrt

import (
	"fmt"
	"math"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
)

// Following limits.
const (
	// ShootTimeout is how long the navigator fires at a shoot goal's
	// target before it gives up (ms).
	ShootTimeout = 10000
	// OffGraphLimit is how long the bot may be off the graph before the
	// navigator gives up (ms).
	OffGraphLimit = 8000
	// stopSlack is the time a stop at an edge start gets (ms).
	stopSlack = 2500
	// stopAir is the number of commands in the air that end a stop at an
	// edge start (the player left the ground for good).
	stopAir = 4
	// flightMin is the number of commands in the air that make a flight
	// (fewer is a ramp hop).
	flightMin = 4
	// noLookaheadMsec is how long the follower pursues node by node after
	// it got stuck.
	noLookaheadMsec = 3000
	// teleportJump is the distance between two commands' positions that
	// counts as a teleport (or a respawn, a reload): the bot relocalizes.
	teleportJump = 96
)

// phase is where the navigator is in running the current path step.
type phase uint8

const (
	phaseStart    phase = iota // not begun
	phaseWait                  // holding at the wait spot until the conditions hold
	phaseApproach              // walking into an auto door's trigger before waiting
	phaseStop                  // stopping at the edge start (EdgeFromRest)
	phaseRun                   // running the edge
	phaseCoast                 // a touch edge set its target off: coasting to rest
)

func (n *Navigator) resetStep() {
	n.ph, n.ex = phaseStart, nil
	n.out.Reset()
	n.hit = false
	n.waitFor = -1
	n.status.WaitFor = -1
}

// Tick returns the movement intent for one command (see TickInput). Call
// it once per usercmd, with the prediction of where that command runs.
func (n *Navigator) Tick(in TickInput) control.MoveIntent {
	n.now, n.st = in.Now, in.Self
	if b := in.Belief; b != nil && (b != n.belief || b.Frames != n.beliefFrames || b.Time != n.beliefTime) {
		n.belief, n.beliefFrames, n.beliefTime = b, b.Frames, b.Time
		n.observe(b)
	}
	if in.Last != nil {
		n.note(in.Last)
	}
	o := n.st.Origin()
	if n.haveOrigin && dist3(o, n.lastOrigin) > teleportJump {
		n.relocalize(fmt.Sprintf("moved %.0f units in one command", dist3(o, n.lastOrigin)), true)
	}
	n.lastOrigin, n.haveOrigin = o, true

	if !n.hasGoal {
		n.status.Follow = Idle
		return n.hold()
	}
	if n.reached() {
		n.status.Follow, n.status.Cause, n.status.Reason = Arrived, CauseNone, ""
		n.status.Edge, n.status.WaitFor, n.status.Remaining = -1, -1, 0
		return n.hold()
	}
	if n.final {
		return n.hold()
	}
	if man, ok := n.manoeuvre(); ok {
		return man
	}
	n.maybePlan()
	out := n.follow()
	n.faceDirectional(&out)
	out = n.watch(out)
	n.status.Remaining = n.remainingCost()
	return out
}

// note folds the predicted result of the last command into the step's
// and the goal's contacts and volumes.
func (n *Navigator) note(res *navsim.StepResult) {
	for _, o := range []*navsim.Outcome{&n.out, &n.gout} {
		for _, id := range res.Touched {
			if !o.HasTouched(id) {
				o.Touched = append(o.Touched, navsim.Contact{ID: id})
			}
		}
		for _, v := range res.Inside {
			if !o.HasVolume(v.ID) {
				o.Volumes = append(o.Volumes, navsim.VolumeHit{ID: v.ID, Yaw: v.Yaw})
			}
		}
		if res.Teleported != 0 {
			o.Teleported = res.Teleported
		}
		if res.Pushed != 0 {
			o.Pushed = res.Pushed
		}
	}
}

// reached reports whether the goal is reached.
func (n *Navigator) reached() bool {
	o := n.st.Origin()
	switch n.goal.Kind {
	case GoalNodes:
		for _, id := range n.goal.Nodes {
			nd := n.g.Node(id)
			if nd == nil || !navsim.Arrived(&n.st, nd.Origin, n.g.ArriveMode(id)) {
				continue
			}
			if nd.Blocker < 0 || n.ms.Possible(nd.Blocker)&nav.Pose(int(nd.Pose)) != 0 {
				return true
			}
		}
	case GoalPoint:
		return (n.st.OnGround() || n.st.WaterLevel >= 1) && pointReached(o, n.goal)
	case GoalVolume:
		return boxTouch(o, n.st.Ducked(), n.goal.Min, n.goal.Max)
	case GoalTouch:
		return n.touched(n.goal.Entity, &n.gout) || n.entityMoved(n.goal.Entity)
	case GoalItem:
		return n.itemTaken(n.goal.Entity) || n.touched(n.goal.Entity, &n.gout)
	case GoalShoot:
		return n.entityMoved(n.goal.Entity)
	}
	return false
}

// touched reports whether the bot touched entity ent: its solid in the
// predicted contacts, one of its volumes entered, or the bot inside one
// now.
func (n *Navigator) touched(ent int32, out *navsim.Outcome) bool {
	if out.HasTouched(int(ent)) {
		return true
	}
	o, ducked := n.st.Origin(), n.st.Ducked()
	for _, vi := range n.volsOf[ent] {
		v := &n.g.Volumes[vi]
		if v.Blocker >= 0 && v.Pose >= 0 && n.ms.Possible(v.Blocker)&nav.Pose(int(v.Pose)) == 0 {
			continue
		}
		if out.HasVolume(vi) || boxTouch(o, ducked, v.Min, v.Max) {
			return true
		}
	}
	return false
}

// entityMoved reports whether the brush entity ent was seen moving since
// the goal was set (a button pressed or shot).
func (n *Navigator) entityMoved(ent int32) bool {
	if n.belief == nil || n.md == nil {
		return false
	}
	if f := n.belief.Effect(worldmodel.EffectMoverMoved, int(ent)); f != nil && f.At >= n.goalSince {
		return true
	}
	e := n.md.Entity(int(ent))
	if e == nil || e.Model == "" {
		return false
	}
	mv := n.belief.Mover(e.Model)
	return mv != nil && mv.Moving && mv.LastUpdate >= n.goalSince
}

func (n *Navigator) itemTaken(ent int32) bool {
	if n.belief == nil {
		return false
	}
	f := n.belief.Effect(worldmodel.EffectItemTaken, int(ent))
	return f != nil && f.At >= n.goalSince
}

// maybePlan plans when asked to, when there is no path, and every
// RepathInterval, unless the bot is in the air on a plain walk (it plans
// once it lands) or off the graph (walkOff relocalizes).
func (n *Navigator) maybePlan() {
	due := n.replan || n.now-n.lastPlan >= n.cfg.RepathInterval
	if !due || n.status.Follow == OffGraph && !n.replan {
		return
	}
	special := n.cur < len(n.path.Edges) && (n.ph == phaseRun || n.ph == phaseCoast || n.ph == phaseStop) && !n.plainNow(&n.g.Edges[n.path.Edges[n.cur]])
	if !special && !n.st.OnGround() && n.st.WaterLevel < 2 && len(n.path.Edges) > 0 {
		return // in the air: plan where it lands
	}
	n.plan(special)
}

// plan plans from where the bot is: from the end of the special edge
// being run (which it finishes), else from the node it localizes at.
func (n *Navigator) plan(keepSpecial bool) {
	why, ev := n.replanWhy, n.replanEv
	n.replan, n.replanWhy, n.replanEv = false, "", false
	n.lastPlan = n.now
	keep := -1
	var start nav.NodeID
	if keepSpecial && n.cur < len(n.path.Edges) {
		keep = n.path.Edges[n.cur]
		start = n.g.Edges[keep].To
	} else {
		start = n.Localize(n.st.Origin(), n.st.Ducked())
		if start == nav.NoNode {
			n.goOffGraph()
			return
		}
		n.node, n.status.Node = start, start
	}
	p, ok := n.pl.Find(start, n.target, n.cost)
	if !ok {
		if keep >= 0 {
			return // finish the edge, then plan again
		}
		n.path = Path{Start: start}
		n.rem = n.rem[:0]
		n.cur = 0
		n.resetStep()
		n.status.Follow, n.status.Cause = Failed, CauseNoPath
		n.status.Reason = fmt.Sprintf("no path to %s from node %d (%s)", n.goal, start, why)
		return
	}
	edges := p.Edges
	if keep >= 0 {
		edges = append([]int{keep}, edges...)
	}
	if n.status.Follow == Failed || n.status.Follow == OffGraph {
		n.status.Follow, n.status.Cause, n.status.Reason = Following, CauseNone, ""
	}
	if equalInts(edges, n.path.Edges[min(n.cur, len(n.path.Edges)):]) && len(edges) > 0 {
		return // the same way: keep the step's state
	}
	// a repath while waiting for the same next edge keeps waiting
	same := keep < 0 && (n.ph == phaseWait || n.ph == phaseApproach) && len(edges) > 0 &&
		n.cur < len(n.path.Edges) && edges[0] == n.path.Edges[n.cur]
	n.setPath(start, edges, p, keep >= 0 || same)
	n.status.Repaths++
	n.stuck.repathed(n.remaining(), ev)
}

func (n *Navigator) setPath(start nav.NodeID, edges []int, p Path, keepStep bool) {
	n.path = Path{Start: start, Edges: edges, Cost: p.Cost, GoalEdge: p.GoalEdge, Expanded: p.Expanded}
	if keepStep && len(edges) > 0 {
		n.path.Start = n.g.Edges[edges[0]].From
	}
	n.cur = 0
	n.rem = n.rem[:0]
	for range edges {
		n.rem = append(n.rem, 0)
	}
	for k := len(edges) - 2; k >= 0; k-- {
		e := &n.g.Edges[edges[k+1]]
		n.rem[k] = n.rem[k+1] + dist3(n.g.Nodes[e.From].Origin, n.g.Nodes[e.To].Origin)
	}
	if !keepStep {
		n.resetStep()
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// relocalize drops the path and plans again from where the bot is.
func (n *Navigator) relocalize(why string, event bool) {
	n.path = Path{Start: nav.NoNode}
	n.rem = n.rem[:0]
	n.cur = 0
	n.resetStep()
	n.requestPlan(why, event)
}

func (n *Navigator) goOffGraph() {
	if n.status.Follow != OffGraph {
		n.offSince = n.now
	}
	n.path = Path{Start: nav.NoNode}
	n.rem = n.rem[:0]
	n.cur = 0
	n.resetStep()
	n.status.Follow, n.status.Cause = OffGraph, CauseOffGraph
	n.status.Reason = fmt.Sprintf("no node within reach of %v", n.st.Origin())
}

// remaining is the path length (units) left from the bot to the path's
// end.
func (n *Navigator) remaining() float32 {
	o := n.st.Origin()
	if n.cur >= len(n.path.Edges) {
		if end := n.g.Node(n.path.End(n.g)); end != nil {
			return dist3(o, end.Origin)
		}
		return 0
	}
	e := &n.g.Edges[n.path.Edges[n.cur]]
	return dist3(o, n.g.Nodes[e.To].Origin) + n.rem[n.cur]
}

// remainingCost is the planned time left (s).
func (n *Navigator) remainingCost() float32 {
	c := float32(0)
	for k := n.cur; k < len(n.path.Edges); k++ {
		c += n.g.Edges[n.path.Edges[k]].Cost
	}
	return c
}

// follow returns the intent of the current step, moving on to the next
// steps that are already done.
func (n *Navigator) follow() control.MoveIntent {
	for iter := 0; iter < 8; iter++ {
		if n.status.Follow == OffGraph {
			return n.walkOff()
		}
		if n.status.Follow == Failed {
			return n.hold()
		}
		if n.cur >= len(n.path.Edges) {
			return n.pathEnd()
		}
		i := n.path.Edges[n.cur]
		e := &n.g.Edges[i]
		n.status.Edge, n.status.Step = i, n.cur
		if n.ph != phaseWait && n.ph != phaseApproach && n.status.Follow != Stuck {
			if n.status.Follow == Waiting {
				n.status.Cause, n.status.Reason = CauseNone, ""
			}
			n.status.Follow = Following
		}
		var in control.MoveIntent
		again := false
		switch n.ph {
		case phaseStart:
			in, again = n.begin(i, e)
		case phaseWait, phaseApproach:
			in, again = n.waiting(i, e)
		case phaseStop:
			in, again = n.stopping(i, e)
		default:
			in, again = n.running(i, e)
		}
		if !again {
			return in
		}
	}
	return n.hold()
}

// advance moves on to the next step.
func (n *Navigator) advance() {
	if n.cur < len(n.path.Edges) {
		n.node = n.g.Edges[n.path.Edges[n.cur]].To
		n.status.Node = n.node
	}
	n.cur++
	n.resetStep()
	n.doorWaited = false
	if n.status.Follow == Stuck || n.status.Follow == Waiting {
		n.status.Follow = Following
	}
}

// plainNow reports whether edge e is a plain walk the follower may pursue
// with lookahead: a walk or crawl that works from a running start, whose
// conditions hold now without waiting.
func (n *Navigator) plainNow(e *nav.Edge) bool {
	if e.Kind != nav.EdgeWalk && e.Kind != nav.EdgeCrouch {
		return false
	}
	if e.Recipe != navsim.RecipeWalk && e.Recipe != navsim.RecipeCrouch {
		return false
	}
	if e.Flags&(nav.EdgeFromRest|nav.EdgeFragile) != 0 {
		return false
	}
	if len(e.Reqs) == 0 {
		return true
	}
	ok, wait := n.ms.Holds(e, n.now)
	return ok && wait == 0
}

// begin starts step i: plain walks run right away; other edges wait for
// their conditions first and stop at their start when they need to.
func (n *Navigator) begin(i int, e *nav.Edge) (control.MoveIntent, bool) {
	if n.plainNow(e) {
		n.ph = phaseRun
		return control.MoveIntent{}, true
	}
	ok, wait := n.ms.Holds(e, n.now)
	if !ok {
		n.requestPlan(fmt.Sprintf("the conditions of edge %s do not hold", EdgeKey(e)), false)
		return n.hold(), false
	}
	if wait > 0 {
		n.ph = phaseWait
		n.waitLim = n.now + int64((wait+1)*1000)
		n.waitFor = n.unmet(e)
		n.status.WaitFor = n.waitFor
		n.waitSpot = n.safeSpot(e)
		n.status.Follow = Waiting
		n.status.Reason = fmt.Sprintf("waiting up to %.1fs for %s", wait+1, n.blockerName(n.waitFor))
		return control.MoveIntent{}, true
	}
	return n.start(i, e)
}

// unmet returns the first blocker of e whose condition does not hold now.
func (n *Navigator) unmet(e *nav.Edge) int32 {
	for _, r := range e.Reqs {
		if ok, w := n.ms.Satisfied(r, n.now); !ok || w > 0 {
			return r.Blocker
		}
	}
	return -1
}

func (n *Navigator) blockerName(b int32) string {
	if b < 0 || int(b) >= len(n.g.Blockers) {
		return "nothing"
	}
	bl := &n.g.Blockers[b]
	return fmt.Sprintf("%s %s (#%d, %s)", bl.Class, bl.Model, bl.Entity, n.ms.Belief(b).Status)
}

// start runs edge e: after a stop at its start when it needs one, with
// the executor the builder validated it with.
func (n *Navigator) start(i int, e *nav.Edge) (control.MoveIntent, bool) {
	if n.status.Follow == Waiting {
		n.status.Follow, n.status.Reason = Following, ""
	}
	n.out.Reset()
	n.hit = false
	n.waitFor, n.status.WaitFor = -1, -1
	from := n.g.Nodes[e.From].Origin
	o := n.st.Origin()
	switch {
	case swimStart(e) && n.st.WaterLevel >= 1 && (dist3(o, from) > swimReach || e.Recipe == navsim.RecipeWaterJump && vlen(n.st.Velocity()) > swimStill):
		// a swim or water jump validated from its (underwater) start: swim
		// there first (holding up from the surface never gets down)
		n.ph, n.swimTo = phaseStop, true
		n.ex = navsim.Plan{Recipe: navsim.RecipeSwim, From: o, Target: from, StepMsec: CmdMsec}.Executor()
		n.deadline = n.now + stopSlack
		return control.MoveIntent{}, true
	case (e.Flags&nav.EdgeFromRest != 0 || selfStopping(e)) && n.st.OnGround() && !navsim.Stopped(&n.st, from):
		// the jump, drop and ladder executors stop at the start
		// themselves, but give up when the ground flickers (walking down
		// a ramp leaves the ground for a command): stop here first
		n.ph, n.swimTo, n.airTicks = phaseStop, false, 0
		n.ex = navsim.StopAt(from, CmdMsec)
		n.deadline = n.now + stopSlack
		return control.MoveIntent{}, true
	}
	n.run(e)
	return control.MoveIntent{}, true
}

// swimReach is how close (units) a swim or water jump edge must start to
// its start node, and swimStill how slow (units/s) a water jump must start.
const (
	swimReach = 12
	swimStill = 30
)

func vlen(v Vec3) float32 {
	return float32(math.Sqrt(float64(v[0])*float64(v[0]) + float64(v[1])*float64(v[1]) + float64(v[2])*float64(v[2])))
}

// swimStart reports an edge whose recipe swims from its start.
func swimStart(e *nav.Edge) bool {
	return e.Recipe == navsim.RecipeSwim || e.Recipe == navsim.RecipeWaterJump
}

func (n *Navigator) run(e *nav.Edge) {
	n.ph = phaseRun
	n.out.Reset()
	n.hit = false
	p := n.g.Plan(e)
	switch e.Kind {
	case nav.EdgeTouch:
		n.ex = navsim.Coast(p.Executor(), &n.hit)
	default:
		n.ex = p.Executor()
	}
	n.runStart = n.now
	n.flight = 0
	n.deadline = n.now + n.limit(e)
}

// selfStopping: the jump, drop and ladder executors stop at the edge
// start themselves.
func selfStopping(e *nav.Edge) bool {
	switch e.Recipe {
	case navsim.RecipeJump, navsim.RecipeDrop, navsim.RecipeLadder:
		return true
	}
	return false
}

// limit is the time an edge run gets (ms): the builder's validation limit
// plus the stop at the start, the run-up and some slack; a ride its
// travel time plus a plat's return.
func (n *Navigator) limit(e *nav.Edge) int64 {
	from, to := n.g.Nodes[e.From].Origin, n.g.Nodes[e.To].Origin
	switch e.Kind {
	case nav.EdgeRide:
		return int64((e.Cost+PlatReturn)*1000) + 3000
	case nav.EdgeTouch:
		p := n.g.Plan(e)
		return int64(navsim.TimeLimitMsec(dist3(from, p.Target))) + 500 + 1500 + stopSlack
	}
	return int64(navsim.TimeLimitMsec(dist3(from, to))+int(e.BackupMsec)) + stopSlack + 1000
}

func (n *Navigator) stopping(i int, e *nav.Edge) (control.MoveIntent, bool) {
	from := n.g.Nodes[e.From].Origin
	if n.swimTo {
		near := dist3(n.st.Origin(), from) <= swimReach
		// a water jump was validated from rest: let the water stop the bot
		still := e.Recipe != navsim.RecipeWaterJump || vlen(n.st.Velocity()) <= swimStill
		if near && still || n.now > n.deadline || n.st.WaterLevel == 0 {
			n.run(e)
			return control.MoveIntent{}, true
		}
		if near {
			in := idleIntent(&n.st)
			in.Stop, in.StopAt = true, from
			return in, false
		}
		in := control.FromCmd(n.ex.Next(n.w, &n.st), n.st.WaterLevel >= 2)
		in.MustFace = true
		return in, false
	}
	if n.st.OnGround() {
		n.airTicks = 0
	} else {
		n.airTicks++
	}
	if navsim.Stopped(&n.st, from) || n.now > n.deadline || n.airTicks >= stopAir {
		n.run(e)
		return control.MoveIntent{}, true
	}
	if !n.st.OnGround() {
		// a ramp hop: keep still until the ground is back
		in := idleIntent(&n.st)
		in.Stop, in.StopAt = true, from
		return in, false
	}
	if n.ex == nil {
		n.ex = navsim.StopAt(from, CmdMsec)
	}
	c := n.ex.Next(n.w, &n.st)
	in := control.FromCmd(c, n.st.WaterLevel >= 2)
	in.Stop, in.StopAt = true, from
	return in, false
}

// running runs the current edge until it is done.
func (n *Navigator) running(i int, e *nav.Edge) (control.MoveIntent, bool) {
	if n.plainNow(e) {
		return n.walk()
	}
	if n.ex == nil {
		n.run(e)
	}
	if e.Kind == nav.EdgeTouch && !n.hit && (n.g.Touched(e, &n.out) || n.entityMoved(e.Target)) {
		n.hit = true
		n.ph = phaseCoast
	}
	if n.edgeDone(e) {
		n.advance()
		return control.MoveIntent{}, true
	}
	if n.st.OnGround() || n.st.WaterLevel >= 2 {
		if n.flight >= flightMin && (e.Kind == nav.EdgeJump || e.Kind == nav.EdgeDrop) && n.landedWrong(e) {
			// it flew and landed somewhere else: this edge does not work
			// from where the bot started it
			n.status.Cause = CauseWorld
			n.status.Reason = fmt.Sprintf("%s %s landed at %v", e.Kind, EdgeKey(e), n.st.Origin())
			n.MarkBlocked(i, n.now)
			n.relocalize(n.status.Reason, true)
			return n.hold(), false
		}
		n.flight = 0
	} else {
		n.flight++
	}
	if n.now > n.deadline {
		return n.stuckNow(n.hold(), fmt.Sprintf("edge %s (%s) timed out", EdgeKey(e), e.Kind)), false
	}
	if n.fellOff(e) {
		n.relocalize(fmt.Sprintf("left edge %s (%s)", EdgeKey(e), e.Kind), true)
		return n.hold(), false
	}
	if e.Kind == nav.EdgeRide {
		return n.ride(e), false
	}
	c := n.ex.Next(n.w, &n.st)
	in := control.FromCmd(c, n.st.WaterLevel >= 2)
	in.MustFace = true
	return in, false
}

// landedWrong reports whether a jump or drop that just landed came down
// away from its end: at another height, or far beside it (landing short
// on the right floor and walking on is fine).
func (n *Navigator) landedWrong(e *nav.Edge) bool {
	o, to := n.st.Origin(), n.g.Nodes[e.To].Origin
	return abs32(o[2]-to[2]) > navsim.ArriveZ+navsim.StepHeight || distH(o, to) > 96
}

// edgeDone reports whether the current (special) edge is complete. An
// edge to a dry node is not done while pmove still carries the bot out of
// the water (PMF_TIME_WATERJUMP: no control until it lands), although the
// arrival test already holds there: the next edge was validated from rest.
func (n *Navigator) edgeDone(e *nav.Edge) bool {
	if n.st.PM.PmFlags&q2const.PMF_TIME_WATERJUMP != 0 && n.g.Nodes[e.To].Flags&nav.NodeWater == 0 {
		return false
	}
	switch e.Kind {
	case nav.EdgeTouch:
		return n.hit && navsim.AtRest(&n.st)
	case nav.EdgeTeleport:
		return n.out.Teleported != 0
	case nav.EdgeRide:
		return n.rideDone(e)
	}
	return n.g.EdgeDone(e, &n.st, &n.out)
}

// fellOff reports that the bot left a special edge for good: it stands
// on something well below both ends, or far beside the edge.
func (n *Navigator) fellOff(e *nav.Edge) bool {
	if !n.st.OnGround() || e.Kind == nav.EdgeRide || e.Kind == nav.EdgeTeleport {
		return false
	}
	o := n.st.Origin()
	from, to := n.g.Nodes[e.From].Origin, n.g.Nodes[e.To].Origin
	if o[2] < min(from[2], to[2])-64 {
		return true
	}
	return segDistH(o, from, to) > 160
}

// segDistH is the horizontal distance from p to the segment a-b.
func segDistH(p, a, b Vec3) float32 {
	dx, dy := float64(b[0]-a[0]), float64(b[1]-a[1])
	px, py := float64(p[0]-a[0]), float64(p[1]-a[1])
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, (px*dx+py*dy)/l2))
	}
	return float32(math.Hypot(px-t*dx, py-t*dy))
}

// walk pursues the plain walks of the path: it moves on past the nodes the
// bot reached or passed, and steers at the farthest node ahead within the
// lookahead that it can walk to in a straight line, as long as only plain
// walks lead there.
func (n *Navigator) walk() (control.MoveIntent, bool) {
	o := n.st.Origin()
	for n.cur < len(n.path.Edges) {
		e := &n.g.Edges[n.path.Edges[n.cur]]
		if !n.plainNow(e) {
			n.ph = phaseStart
			return control.MoveIntent{}, true
		}
		to := n.g.Nodes[e.To].Origin
		if navsim.Arrived(&n.st, to, n.g.ArriveMode(e.To)) {
			n.advance()
			n.ph = phaseRun
			continue
		}
		if n.cur+1 < len(n.path.Edges) {
			ne := &n.g.Edges[n.path.Edges[n.cur+1]]
			nx := n.g.Nodes[ne.To].Origin
			if n.plainNow(ne) && ne.Recipe == e.Recipe && n.st.OnGround() && distH(o, nx) < distH(to, nx) &&
				abs32(o[2]-to[2]) <= navsim.ArriveZ+navsim.StepHeight && n.clear(o, nx, e.Recipe == navsim.RecipeCrouch) {
				n.advance()
				n.ph = phaseRun
				continue
			}
		}
		if n.st.OnGround() && distH(o, to) > distH(n.g.Nodes[e.From].Origin, to)+96 {
			n.relocalize(fmt.Sprintf("strayed from edge %s", EdgeKey(e)), true)
			return n.hold(), false
		}
		break
	}
	if n.cur >= len(n.path.Edges) {
		return control.MoveIntent{}, true
	}
	e := &n.g.Edges[n.path.Edges[n.cur]]
	target, gap := n.lookahead(o, e)
	if gap && n.st.OnGround() {
		// off the edge with a pit in between (on a monster's head, pushed
		// aside): walk back to its start, or start over from where it is
		// (a wall in the way is fine: pmove slides along it)
		if from := n.g.Nodes[e.From].Origin; n.clearWalk(o, from, e.Recipe == navsim.RecipeCrouch) {
			target = from
		} else if n.now-n.gapAt > 1000 {
			n.gapAt = n.now
			n.relocalize(fmt.Sprintf("no floor between %v and edge %s", o, EdgeKey(e)), true)
			return n.hold(), false
		}
	}
	p := navsim.Plan{Recipe: e.Recipe, From: o, Target: target, Forward: e.Forward, StepMsec: CmdMsec}
	c := p.Executor().Next(n.w, &n.st)
	return control.FromCmd(c, n.st.WaterLevel >= 2), false
}

// lookahead returns the node to steer at on the plain walks from the
// current step: the farthest one within the lookahead the bot can walk to
// in a straight line over floor, with only plain walks leading there; the
// current step's end when none is. gap reports that the straight line to
// that end is open but has no floor under it (a pit to fall into).
func (n *Navigator) lookahead(o Vec3, cur *nav.Edge) (target Vec3, gap bool) {
	best := n.g.Nodes[cur.To].Origin
	ducked := cur.Recipe == navsim.RecipeCrouch
	gap = distH(o, best) >= 2*navsim.ArriveXY && n.clear(o, best, ducked) && !n.floorBetween(o, best, ducked)
	if n.now < n.noLookahead {
		return best, gap // after being stuck: node by node, no corners cut through what stopped it
	}
	for j := n.cur + 1; j < len(n.path.Edges) && j <= n.cur+8; j++ {
		e := &n.g.Edges[n.path.Edges[j]]
		if !n.plainNow(e) || e.Recipe != cur.Recipe {
			break
		}
		p := n.g.Nodes[e.To].Origin
		if dist3(o, p) > n.cfg.Lookahead || !n.clearWalk(o, p, ducked) {
			break
		}
		best, gap = p, false
	}
	return best, gap
}

// pathEnd handles a path that is used up without reaching the goal: walk
// onto the end node when close (an empty path from a goal node the bot
// is near), shoot at a shoot goal's target, else plan again.
func (n *Navigator) pathEnd() control.MoveIntent {
	end := n.g.Node(n.path.End(n.g))
	o := n.st.Origin()
	if end != nil && !navsim.Arrived(&n.st, end.Origin, n.g.ArriveMode(n.path.End(n.g))) && distH(o, end.Origin) < 96 {
		return n.stopAt(end.Origin)
	}
	if n.goal.Kind == GoalShoot && end != nil {
		return n.shoot()
	}
	if n.now-n.lastPlan >= 500 {
		n.requestPlan("the path ended before the goal", false)
	}
	return n.hold()
}

// shoot faces the shoot goal's target and fires once the view is on it.
func (n *Navigator) shoot() control.MoveIntent {
	if n.shootAt == 0 {
		n.shootAt = n.now
	}
	if n.now-n.shootAt > ShootTimeout {
		n.final = true
		n.status.Follow, n.status.Cause = Failed, CauseWorld
		n.status.Reason = fmt.Sprintf("%s: no reaction after %ds of fire", n.goal, ShootTimeout/1000)
		return n.hold()
	}
	eye := add(n.st.Origin(), Vec3{0, 0, n.st.ViewHeight})
	yaw, _ := navsim.YawTo(eye, n.goal.Point)
	pitch := navsim.PitchTo(eye, n.goal.Point)
	in := control.Idle(yaw, pitch)
	in.MustFace = true
	in.Fire = math.Abs(float64(control.AngleDelta(n.st.ViewYaw, yaw))) < 2
	return in
}

// waiting holds at the wait spot until the step's conditions hold (an
// auto door it first walks into the trigger of), for at most the
// estimated wait plus a second; then it tries the edge when the blocker
// is out of sight, or marks the edge blocked.
func (n *Navigator) waiting(i int, e *nav.Edge) (control.MoveIntent, bool) {
	ok, wait := n.ms.Holds(e, n.now)
	switch {
	case !ok:
		n.requestPlan(fmt.Sprintf("the conditions of edge %s no longer hold", EdgeKey(e)), false)
		return n.hold(), false
	case wait == 0:
		n.status.Follow, n.status.Reason = Following, ""
		return n.start(i, e)
	case n.now >= n.waitLim:
		if b := n.waitFor; b >= 0 && n.now-n.ms.Belief(b).Seen > 1000 {
			// out of sight: it may well be open by now
			n.status.Follow, n.status.Reason = Following, ""
			return n.start(i, e)
		}
		n.status.Cause = CauseDoor
		n.status.Reason = fmt.Sprintf("waited too long for %s", n.blockerName(n.waitFor))
		n.MarkBlocked(i, n.now)
		return n.hold(), false
	}
	n.status.Follow = Waiting
	o, ducked := n.st.Origin(), n.st.Ducked()
	if b := n.waitFor; b >= 0 && n.ms.Auto(b) {
		if tr := n.ms.Trigger(b); tr != nil && !boxTouch(o, ducked, tr.Min, tr.Max) {
			if n.ph != phaseApproach {
				n.ph = phaseApproach
				n.ex = n.g.Plan(e).Executor()
			}
			return control.FromCmd(n.ex.Next(n.w, &n.st), n.st.WaterLevel >= 2), false
		}
	}
	n.ph = phaseWait
	return n.stopAt(n.waitSpot), false
}

// safeSpot returns where to wait before edge e: its start, or the start of
// an earlier step when the start is in the path of a mover.
func (n *Navigator) safeSpot(e *nav.Edge) Vec3 {
	first := n.g.Nodes[e.From].Origin
	if !n.inMoverPath(first) {
		return first
	}
	for k := n.cur - 1; k >= 0 && k >= n.cur-4; k-- {
		p := n.g.Nodes[n.g.Edges[n.path.Edges[k]].From].Origin
		if !n.inMoverPath(p) {
			return p
		}
	}
	return first
}

// inMoverPath reports whether a player standing at p is somewhere a door,
// plat or train can move through.
func (n *Navigator) inMoverPath(p Vec3) bool {
	for b := range n.g.Blockers {
		switch n.g.Blockers[b].Kind {
		case nav.BlockDoor, nav.BlockRotating, nav.BlockSecret, nav.BlockPlat, nav.BlockTrain:
			lo, hi := n.ms.Sweep(int32(b))
			if boxTouch(p, false, lo, hi) {
				return true
			}
		}
	}
	return false
}

// stopAt steers the bot to rest at p.
func (n *Navigator) stopAt(p Vec3) control.MoveIntent {
	in := control.FromCmd(navsim.StopAt(p, CmdMsec).Next(n.w, &n.st), n.st.WaterLevel >= 2)
	in.Stop, in.StopAt = true, p
	return in
}

// ride stands on the mover until it carries the bot to the edge's end; on
// a plat it first moves into the plat's center trigger, which sends the
// plat up and keeps it there.
func (n *Navigator) ride(e *nav.Edge) control.MoveIntent {
	from := &n.g.Nodes[e.From]
	o := n.st.Origin()
	if from.Blocker >= 0 && n.g.Blockers[from.Blocker].Kind == nav.BlockPlat && n.st.OnGround() {
		if tr := n.ms.Trigger(from.Blocker); tr != nil && !boxTouch(o, n.st.Ducked(), tr.Min, tr.Max) {
			spot := o
			for c := 0; c < 2; c++ {
				spot[c] = min(max(spot[c], tr.Min[c]+1), tr.Max[c]-1)
			}
			return n.stopAt(spot)
		}
	}
	c := n.ex.Next(n.w, &n.st)
	return control.FromCmd(c, n.st.WaterLevel >= 2)
}

// rideDone reports whether the mover brought the bot to the ride's end.
func (n *Navigator) rideDone(e *nav.Edge) bool {
	to := &n.g.Nodes[e.To]
	o := n.st.Origin()
	if abs32(o[2]-to.Origin[2]) > navsim.ArriveZ || !n.st.OnGround() {
		return false
	}
	if to.Blocker >= 0 {
		if bb := n.ms.Belief(to.Blocker); bb.Status == BlockerAt && bb.Pose == int(to.Pose) {
			return true
		}
	}
	return abs32(o[2]-to.Origin[2]) <= 1 && n.st.PM.Velocity[2] == 0 && n.now-n.runStart > 500
}

// hold stands still: ducked on a crouch node, facing the ladder on a
// ladder node.
func (n *Navigator) hold() control.MoveIntent {
	if nd := n.g.Node(n.node); nd != nil && nd.Flags&(nav.NodeCrouch|nav.NodeLadder) != 0 && navsim.Near(n.st.Origin(), nd.Origin) {
		c := nav.HoldCmd(nd)
		in := control.FromCmd(c, false)
		if nd.Flags&nav.NodeLadder != 0 {
			in.MustFace = true
		} else {
			in.FaceYaw = n.st.ViewYaw
		}
		return in
	}
	return idleIntent(&n.st)
}

// walkOff walks back to the nearest node when the bot is off the graph,
// relocalizing every 250 ms, and gives up after OffGraphLimit.
func (n *Navigator) walkOff() control.MoveIntent {
	if n.now-n.offSince > OffGraphLimit {
		n.final = true
		n.status.Follow = Failed
		n.status.Reason = fmt.Sprintf("off the graph for %ds at %v", OffGraphLimit/1000, n.st.Origin())
		return n.hold()
	}
	o := n.st.Origin()
	if (n.now-n.offSince)%250 < CmdMsec && (n.st.OnGround() || n.st.WaterLevel >= 2) {
		if id := n.Localize(o, n.st.Ducked()); id != nav.NoNode {
			n.status.Follow, n.status.Cause, n.status.Reason = Following, CauseNone, ""
			n.requestPlan("back on the graph", true)
			return n.hold()
		}
	}
	id := n.g.LocalizeIn(o, 512, n.ms.Masks(), nil)
	if id == nav.NoNode {
		return n.hold()
	}
	p := navsim.Plan{Recipe: navsim.RecipeWalk, From: o, Target: n.g.Nodes[id].Origin, StepMsec: CmdMsec}
	return control.FromCmd(p.Executor().Next(n.w, &n.st), n.st.WaterLevel >= 2)
}

// faceDirectional makes the bot face a directional trigger's direction
// (Touch_Multi only fires for a player facing along its movedir, judged
// on the facing of the last server frame) from well before it enters one
// the path sets off.
func (n *Navigator) faceDirectional(in *control.MoveIntent) {
	if len(n.dirYaw) == 0 {
		return
	}
	reach := max(64, n.st.HSpeed()*0.4)
	o, ducked := n.st.Origin(), n.st.Ducked()
	for j := n.cur; j < len(n.path.Edges) && j <= n.cur+2; j++ {
		e := &n.g.Edges[n.path.Edges[j]]
		for _, f := range e.Effects {
			yaw, ok := n.dirYaw[f.Entity]
			if !ok || f.Kind != nav.EffTrigger {
				continue
			}
			for _, vi := range n.volsOf[f.Entity] {
				v := &n.g.Volumes[vi]
				lo, hi := add(v.Min, Vec3{-reach, -reach, -reach}), add(v.Max, Vec3{reach, reach, reach})
				if boxTouch(o, ducked, lo, hi) {
					in.MustFace, in.FaceYaw, in.FacePitch = true, yaw, 0
					return
				}
			}
		}
	}
}

// Stuck detection and recovery.

// Stuck thresholds: less than ProgressMin units of progress along the
// path in ProgressWindow of following (waits excluded), or less than
// PushMin units of movement in PushWindow commands of pushing.
const (
	ProgressMin    = 16
	ProgressWindow = 1500 // ms
	PushMin        = 4
	PushWindow     = 20 // commands (0.5 s)
	// levelReset is the progress that resets the escalation.
	levelReset = 64
)

type manKind uint8

const (
	manNone manKind = iota
	manJump
	manStrafe
	manBack
	manWait // a door in the way: wait at a spot out of its path
)

type stuckState struct {
	init   bool
	active int64 // ms of following that counts
	best   float32
	bestAt int64
	// stuckRem is the remaining path length when it last got stuck: the
	// escalation starts over once the bot is levelReset closer.
	stuckRem float32
	ring     [PushWindow]Vec3
	nring    int
	level    int
	man      manKind
	manEnd   int64
	manDir   Vec3
	jumped   bool
	sign     float32
}

// repathed resets the progress window after a path change that came from
// an event, or that made the way longer.
func (s *stuckState) repathed(rem float32, event bool) {
	if !s.init {
		return
	}
	if event || rem > s.best+levelReset {
		s.best, s.bestAt = rem, s.active
		s.nring = 0
	}
}

// exempt reports intents and phases the stuck detection ignores: waits,
// stops, rides, and anything but following.
func (n *Navigator) exempt(in control.MoveIntent) bool {
	if n.status.Follow != Following && n.status.Follow != Stuck {
		return true
	}
	if in.Stop || n.ph == phaseWait || n.ph == phaseApproach || n.ph == phaseStop {
		return true
	}
	if n.cur >= len(n.path.Edges) || n.g.Edges[n.path.Edges[n.cur]].Kind == nav.EdgeRide {
		return true // standing at the end (shooting, stepping onto the goal node), riding
	}
	return false
}

// watch runs the stuck detection on the intent about to be sent and
// starts a recovery when the bot is stuck.
func (n *Navigator) watch(in control.MoveIntent) control.MoveIntent {
	s := &n.stuck
	if n.exempt(in) {
		s.nring = 0
		return in
	}
	s.active += CmdMsec
	rem := n.remaining()
	if !s.init {
		s.init = true
		s.best, s.bestAt = rem, s.active
	}
	if rem < s.best-ProgressMin {
		s.best, s.bestAt = rem, s.active
	}
	if s.level > 0 && rem < s.stuckRem-levelReset {
		// got well past where it was stuck: the obstacle is behind
		s.level = 0
		n.status.Level = 0
	}
	o := n.st.Origin()
	if in.Speed >= 100 && n.st.OnGround() {
		s.ring[s.nring%PushWindow] = o
		s.nring++
	} else {
		s.nring = 0
	}
	switch {
	case s.active-s.bestAt > ProgressWindow:
		return n.stuckNow(in, fmt.Sprintf("less than %d units of progress in %.1fs", ProgressMin, float32(ProgressWindow)/1000))
	case s.nring >= PushWindow && dist3(s.ring[s.nring%PushWindow], o) < PushMin:
		return n.stuckNow(in, fmt.Sprintf("moved less than %d units in %.1fs of pushing", PushMin, float32(PushWindow*CmdMsec)/1000))
	}
	return in
}

// stuckNow escalates: jump, strafe, back off, relocalize and repath, mark
// the edge blocked and repath, give up.
func (n *Navigator) stuckNow(in control.MoveIntent, why string) control.MoveIntent {
	s := &n.stuck
	cause, detail := n.classify(in)
	if cause == CauseDoor && n.doorHit >= 0 && !n.doorWaited && n.cur < len(n.path.Edges) {
		// pushed against a door: it may be opening (or about to close and
		// reopen); wait out its travel away from its path, once per step
		n.doorWaited = true
		s.bestAt, s.nring = s.active, 0
		n.status.Follow, n.status.Cause = Waiting, cause
		n.status.Reason = why + ": " + detail + "; waiting for it"
		s.man, s.manEnd = manWait, n.now+int64((n.ms.Travel(n.doorHit)+1)*1000)
		s.manDir = n.safeSpot(&n.g.Edges[n.path.Edges[n.cur]])
		m, _ := n.manoeuvre()
		return m
	}
	s.level++
	s.bestAt, s.nring = s.active, 0
	s.stuckRem = n.remaining()
	n.noLookahead = n.now + noLookaheadMsec
	n.status.Stucks++
	n.status.Level = s.level
	n.status.Follow, n.status.Cause = Stuck, cause
	n.status.Reason = why + ": " + detail
	dir := flatDir(in)
	if s.sign == 0 {
		s.sign = 1
	}
	switch s.level {
	case 1:
		s.man, s.manEnd, s.manDir, s.jumped = manJump, n.now+150, dir, false
	case 2:
		// strafe to a side with floor (the other one first next time)
		side := Vec3{-dir[1] * s.sign, dir[0] * s.sign, 0}
		if !n.floorAlong(side, 400) {
			side = Vec3{-side[0], -side[1], 0}
		}
		s.sign = -s.sign
		if n.floorAlong(side, 400) {
			s.man, s.manEnd, s.manDir = manStrafe, n.now+400, side
		}
	case 3:
		if back := (Vec3{-dir[0], -dir[1], 0}); n.floorAlong(back, 300) {
			s.man, s.manEnd, s.manDir = manBack, n.now+300, back
		}
	case 4:
		n.relocalize("stuck: "+n.status.Reason, true)
		return n.hold()
	case 5:
		switch {
		case cause == CauseEntity && n.blockedBy >= 0:
			// a monster stands in the way: every edge through its spot
			n.markArea(n.occupied[n.blockedBy])
		case n.cur < len(n.path.Edges):
			n.MarkBlocked(n.path.Edges[n.cur], n.now)
		}
		n.relocalize("stuck: "+n.status.Reason, true)
		return n.hold()
	default:
		n.final = true
		n.status.Follow = Failed
		n.status.Reason = fmt.Sprintf("gave up after %d recoveries: %s", s.level-1, n.status.Reason)
		return n.hold()
	}
	n.resetStep()
	if m, ok := n.manoeuvre(); ok {
		return m
	}
	return n.hold() // no safe manoeuvre at this level: the next one escalates
}

// manoeuvre returns the intent of the recovery manoeuvre in progress.
func (n *Navigator) manoeuvre() (control.MoveIntent, bool) {
	s := &n.stuck
	if s.man == manNone {
		return control.MoveIntent{}, false
	}
	if n.now >= s.manEnd {
		if s.man == manWait && n.status.Follow == Waiting {
			n.status.Follow = Stuck
		}
		s.man = manNone
		s.bestAt, s.nring = s.active, 0
		return control.MoveIntent{}, false
	}
	if s.man == manWait {
		return n.stopAt(s.manDir), true // manDir holds the wait spot
	}
	in := control.MoveIntent{WishDir: s.manDir, Speed: control.MaxMove, FaceYaw: n.st.ViewYaw}
	if s.man == manJump && !s.jumped {
		in.Jump, s.jumped = true, true
	}
	return in, true
}

// floorAlong reports whether running in direction dir for msec keeps
// floor under the bot (no ledge to fall off): ground below the hull every
// 16 units of the way the hull can go (a wall stops it, which is safe).
func (n *Navigator) floorAlong(dir Vec3, msec int) bool {
	if n.static == nil {
		return true
	}
	o := n.st.Origin()
	mins, maxs := hull(n.st.Ducked())
	d := float32(msec) / 1000 * 320
	tr := n.static.Trace(o, mins, maxs, add(o, Vec3{dir[0] * d, dir[1] * d, 0}), q2const.MASK_PLAYERSOLID)
	reach := d * tr.Fraction
	for k := float32(16); k <= reach; k += 16 {
		p := add(o, Vec3{dir[0] * k, dir[1] * k, 0})
		down := add(p, Vec3{0, 0, -(navsim.StepHeight + 8)})
		if g := n.static.Trace(p, mins, maxs, down, q2const.MASK_PLAYERSOLID); g.Fraction == 1 && !g.StartSolid {
			return false
		}
	}
	return true
}

// flatDir is the horizontal direction an intent pushes in (its facing
// when it does not move).
func flatDir(in control.MoveIntent) Vec3 {
	if l := math.Hypot(float64(in.WishDir[0]), float64(in.WishDir[1])); l > 1e-3 && in.Speed > 0 {
		return Vec3{float32(float64(in.WishDir[0]) / l), float32(float64(in.WishDir[1]) / l), 0}
	}
	s, c := math.Sincos(float64(in.FaceYaw) * math.Pi / 180)
	return Vec3{float32(c), float32(s), 0}
}

// classify finds what is in the way: a hull trace along the push
// direction hitting a monster's box, a mover, another solid or the world;
// else a door the current edge waits on.
func (n *Navigator) classify(in control.MoveIntent) (Cause, string) {
	o := n.st.Origin()
	n.blockedBy, n.doorHit = -1, -1
	if n.w != nil {
		mins, maxs := hull(n.st.Ducked())
		dir := flatDir(in)
		end := add(o, Vec3{dir[0] * 24, dir[1] * 24, 0})
		tr := n.w.Trace(o, mins, maxs, end, q2const.MASK_PLAYERSOLID)
		if tr.Fraction < 1 || tr.StartSolid {
			if id := n.trackOf(tr.Ent); id != "" {
				n.blockedBy = tr.Ent - trackBase
				class := ""
				if n.belief != nil {
					if t := n.belief.Track(id); t != nil {
						class = " " + t.Class
					}
				}
				return CauseEntity, fmt.Sprintf("blocked by%s %s at %v", class, id, o)
			}
			if tr.Ent > 0 {
				if b := n.g.BlockerOf(tr.Ent); b >= 0 {
					switch n.g.Blockers[b].Kind {
					case nav.BlockDoor, nav.BlockRotating, nav.BlockSecret, nav.BlockPlat, nav.BlockTrain:
						n.doorHit = b
					}
					return CauseDoor, fmt.Sprintf("blocked by %s at %v", n.blockerName(b), o)
				}
				return CauseEntity, fmt.Sprintf("blocked by solid #%d at %v", tr.Ent, o)
			}
			return CauseWorld, fmt.Sprintf("blocked by the level at %v (normal %v)", o, tr.Plane.Normal)
		}
	}
	if n.cur < len(n.path.Edges) {
		e := &n.g.Edges[n.path.Edges[n.cur]]
		if b := n.unmet(e); b >= 0 {
			return CauseDoor, fmt.Sprintf("edge %s needs %s", EdgeKey(e), n.blockerName(b))
		}
	}
	return CauseWorld, fmt.Sprintf("no way forward at %v", o)
}

// markArea marks the edges into every node whose player box touches box
// (a monster standing there) blocked, except into the node the bot is at
// (it must be able to leave), and repaths.
func (n *Navigator) markArea(box [2]Vec3) {
	here := n.Localize(n.st.Origin(), n.st.Ducked())
	for _, c := range n.g.Nearby(boxCenter(box[0], box[1]), boxRadius(box[0], box[1])+48) {
		nd := &n.g.Nodes[c.Node]
		if c.Node == here || !boxTouch(nd.Origin, nd.Flags&nav.NodeCrouch != 0, box[0], box[1]) {
			continue
		}
		for _, i := range n.g.In(c.Node) {
			n.MarkBlocked(int(i), n.now)
		}
	}
}
