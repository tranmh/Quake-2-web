package routeexec

import (
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
)

// The executor's step logic, op by op, on the demo maps' real entities
// and graphs with a fake navigator (the live runs are campaign's tests).

// The op tests share each map's data and graph (loading a graph takes
// seconds under the race detector).

func TestExecutorDemo1(t *testing.T) {
	lv := demo(t, "demo1")
	t.Run("GotoAutoDoorAndPoint", func(t *testing.T) { testGotoAutoDoorAndPoint(t, lv) })
	t.Run("RetriesTimeoutsAndStall", func(t *testing.T) { testRetriesTimeoutsAndStall(t, lv) })
	t.Run("TouchPressShootPickup", func(t *testing.T) { testTouchPressShootPickup(t, lv) })
	t.Run("RideCarriedIntoNextTrigger", func(t *testing.T) { testRideCarriedIntoNextTrigger(t, lv) })
	t.Run("Face", func(t *testing.T) { testFace(t, lv) })
	t.Run("DirectionalTouchNeedsFacing", func(t *testing.T) { testDirectionalTouchNeedsFacing(t, lv) })
	t.Run("ExitWaitsForTheLevelChange", func(t *testing.T) { testExitWaitsForTheLevelChange(t, lv) })
	t.Run("ObjectiveView", func(t *testing.T) { testObjectiveView(t, lv) })
}

func TestExecutorDemo2(t *testing.T) {
	lv := demo(t, "demo2")
	t.Run("WaitForEffectsSeen", func(t *testing.T) { testWaitForEffectsSeen(t, lv) })
	t.Run("WaitContradictedGoesBack", func(t *testing.T) { testWaitContradictedGoesBack(t, lv) })
	t.Run("WaitUnseenEffectsAssumedAtTimeout", func(t *testing.T) { testWaitUnseenEffectsAssumedAtTimeout(t, lv) })
	t.Run("AvoidSet", func(t *testing.T) { testAvoidSet(t, lv) })
	t.Run("AssumeClaimsWhenNoPath", func(t *testing.T) { testAssumeClaimsWhenNoPath(t, lv) })
}

func TestExecutorDemo3(t *testing.T) {
	lv := demo(t, "demo3")
	t.Run("ConfirmLasers", func(t *testing.T) { testConfirmLasers(t, lv) })
	t.Run("Kill", func(t *testing.T) { testKill(t, lv) })
}

func testGotoAutoDoorAndPoint(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{
		{Op: route.OpGoto, Target: modelRef("*32")},
		{Op: route.OpGoto, Pos: vec(128, -320, 24), Radius: 40},
	})
	h.tick(1)
	g := h.nav.last()
	tr := lv.md.Mover(lv.md.TeamMaster(lv.md.ByModel("*32").Index)).Trigger
	if g.Kind != navrt.GoalNodes || len(g.Nodes) == 0 {
		t.Fatalf("auto door goal %s, want the nodes in its trigger", g)
	}
	for _, id := range g.Nodes {
		o := lv.g.Nodes[id].Origin
		if o[0] < tr.Min[0] || o[0] > tr.Max[0] || o[1] < tr.Min[1] || o[1] > tr.Max[1] {
			t.Fatalf("goal node %d at %v outside the trigger %v..%v", id, o, tr.Min, tr.Max)
		}
	}
	h.wantStep(0, StepRunning)
	h.nav.arrive()
	h.tick(1)
	h.wantStep(1, StepRunning)
	if g := h.nav.last(); g.Kind != navrt.GoalPoint || g.Radius != 40 || g.Point != (Vec3{128, -320, 24}) {
		t.Fatalf("point goal %s", g)
	}
	h.nav.arrive()
	if d := h.tick(1); !d.Done || !h.x.Done() {
		t.Fatalf("route not done: %+v", d)
	}
}

func testRetriesTimeoutsAndStall(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{{Op: route.OpGoto, Pos: vec(128, -320, 24)}})
	h.tick(1)
	if st := h.x.Current(); st.Attempts != 1 {
		t.Fatalf("attempts %d", st.Attempts)
	}
	// a final navigation failure ends the attempt at once; the next one
	// starts after a pause, holding still meanwhile
	h.nav.fail(navrt.CauseWorld, "gave up")
	if d := h.tick(1); !d.Hold {
		t.Fatalf("no hold after a failed attempt: %+v", d)
	}
	if st := h.x.Current(); st.Attempts != 2 || !strings.Contains(st.Reason, "gave up") {
		t.Fatalf("after the failure: %+v", st)
	}
	n := len(h.nav.goals)
	h.tick(RetryPause/100 + 1)
	if len(h.nav.goals) != n+1 {
		t.Fatalf("the second attempt set no goal (%d goals)", len(h.nav.goals))
	}
	// no path is not final: it waits for the belief to change, up to
	// the attempt's timeout; a plan extends the timeout to its cost
	h.nav.fail(navrt.CauseNoPath, "no path")
	h.tick(MoveTimeout/100 - 20)
	if st := h.x.Current(); st.Attempts != 2 {
		t.Fatalf("no path ended the attempt early: %+v", st)
	}
	h.tick(30)
	if st := h.x.Current(); st.Attempts != 3 || !strings.Contains(st.Reason, "timed out") {
		t.Fatalf("after the timeout: %+v", st)
	}
	h.tick(RetryPause/100 + 1)
	h.nav.follow(60) // a minute's walk: 2.5 x 60 s + 20 s
	h.tick(MoveTimeout/100 + 50)
	if st := h.x.Current(); st.Attempts != 3 || st.Status != StepRunning {
		t.Fatalf("a planned path did not extend the timeout: %+v", st)
	}
	h.tick(int(170000-MoveTimeout)/100 - 50 + 10)
	if !h.x.Stalled() {
		t.Fatalf("not stalled after %d attempts: %+v", DefaultMaxAttempts, h.x.Current())
	}
	if d := h.tick(5); !d.Hold {
		t.Error("a stalled step must hold")
	}
	h.x.Retry()
	h.tick(1)
	if st := h.x.Current(); st.Status != StepRunning || st.Attempts != 1 || h.nav.retries != 1 {
		t.Fatalf("after Retry: %+v (nav retries %d)", st, h.nav.retries)
	}
}

func testTouchPressShootPickup(t *testing.T, lv level) {
	shard := 155
	h := newHarness(t, lv, []route.Step{
		{Op: route.OpTouch, Target: ref(286)}, // trigger_once *18, angle 270: directional
		{Op: route.OpPress, Target: modelRef("*34")},
		{Op: route.OpShoot, Target: modelRef("*13")},
		{Op: route.OpPickup, Class: "item_armor_shard", Pos: vec(lv.md.Entity(shard).Origin[0], lv.md.Entity(shard).Origin[1], lv.md.Entity(shard).Origin[2])},
	})
	tr := lv.md.Trigger(286)
	h.b.Self.Origin = Vec3{tr.Box.Max[0] + 400, tr.Box.Max[1] + 400, tr.Box.Min[2] + 24}
	if d := h.tick(1); d.MustFace {
		t.Fatal("faces the directional trigger from afar")
	}
	if g := h.nav.last(); g.Kind != navrt.GoalTouch || g.Entity != 286 {
		t.Fatalf("touch goal %s", g)
	}
	h.b.Self.Origin = Vec3{(tr.Box.Min[0] + tr.Box.Max[0]) / 2, tr.Box.Max[1] + 40, tr.Box.Min[2] + 24}
	if d := h.tick(1); !d.MustFace || math.Abs(float64(angleDelta(d.FaceYaw, 270))) > 0.01 {
		t.Fatalf("near the directional trigger: %+v", d)
	}
	h.nav.arrive()
	h.tick(1)
	button := lv.md.ByModel("*34").Index
	if g := h.nav.last(); g.Kind != navrt.GoalTouch || g.Entity != int32(button) {
		t.Fatalf("press goal %s", g)
	}
	h.nav.arrive()
	h.tick(1)
	shot := lv.md.ByModel("*13")
	if g := h.nav.last(); g.Kind != navrt.GoalShoot || g.Entity != int32(shot.Index) || g.Point != lv.md.Mover(shot.Index).Box.Center() {
		t.Fatalf("shoot goal %s", g)
	}
	h.nav.arrive()
	h.tick(1)
	if g := h.nav.last(); g.Kind != navrt.GoalItem || g.Entity != int32(shard) {
		t.Fatalf("pickup goal %s", g)
	}
	// seen taken (before the navigator says so)
	h.b.Effects = append(h.b.Effects, worldmodel.Effect{Kind: worldmodel.EffectItemTaken, Lump: shard, At: h.now})
	if d := h.tick(1); !d.Done {
		t.Fatalf("pickup not done: %+v", h.x.Current())
	}
}

// testDirectionalTouchNeedsFacing: a directional trigger reached while the
// bot faces away from its movedir did not fire (Touch_Multi): the step
// holds the facing until the server saw it for a frame.
func testDirectionalTouchNeedsFacing(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{
		{Op: route.OpTouch, Target: ref(286)}, // trigger_once *18, angle 270
		{Op: route.OpGoto, Pos: vec(128, -320, 24)},
	})
	box := lv.md.Trigger(286).Box
	h.b.Self.Origin = Vec3{(box.Min[0] + box.Max[0]) / 2, (box.Min[1] + box.Max[1]) / 2, box.Min[2] + 24}
	h.b.Self.ViewAngles = Vec3{0, 90, 0}
	h.tick(1)
	h.nav.arrive()
	for i := 0; i < 20; i++ {
		d := h.tick(1)
		if !d.MustFace || math.Abs(float64(angleDelta(d.FaceYaw, 270))) > 0.01 {
			t.Fatalf("frame %d in the trigger facing away: %+v", i, d)
		}
	}
	h.wantStep(0, StepRunning)
	// turned: the server must see the facing for a frame before it counts
	h.b.Self.ViewAngles = Vec3{0, 265, 0}
	h.tick(1)
	h.wantStep(0, StepRunning)
	h.tick(1)
	h.wantStep(1, StepRunning)
	if r := h.x.Steps()[0].Reason; r != "arrived" {
		t.Errorf("touch done: %q", r)
	}
}

// testExitWaitsForTheLevelChange: the step that claims the exit is not
// done when its goal is reached (the level change ends the level); when
// no level change comes within ExitGrace the attempt fails and the next
// one sets the goal again.
func testExitWaitsForTheLevelChange(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{{Op: route.OpTouch, Target: modelRef("*27"),
		Effects: []route.Effect{{Kind: route.EffExit, Target: route.Ref{Entity: ref(418).Entity}}}}})
	box := lv.md.Trigger(lv.md.ByModel("*27").Index).Box
	h.b.Self.Origin = Vec3{(box.Min[0] + box.Max[0]) / 2, (box.Min[1] + box.Max[1]) / 2, box.Min[2] + 24}
	h.tick(1)
	n := len(h.nav.goals)
	h.nav.arrive()
	d := h.tick(ExitGrace/100 - 1)
	if h.x.Done() || d.Done || d.Hold {
		t.Fatalf("an exit reached is done or holds before the level change: %+v, %+v", d, h.x.Current())
	}
	h.wantStep(0, StepRunning)
	if st := h.x.Current(); st.Attempts != 1 {
		t.Fatalf("attempt %d while waiting", st.Attempts)
	}
	h.tick(2)
	if st := h.x.Current(); st.Attempts != 2 || !strings.Contains(st.Reason, "did not fire") {
		t.Fatalf("after %d ms without a level change: %+v", ExitGrace, st)
	}
	h.tick(RetryPause/100 + 1)
	if len(h.nav.goals) != n+1 || h.nav.last().Kind != navrt.GoalTouch {
		t.Fatalf("the exit was not attempted again: %d goals", len(h.nav.goals)-n)
	}
}

func testRideCarriedIntoNextTrigger(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{
		{Op: route.OpRide, Target: modelRef("*31"), Until: "pos2"},
		{Op: route.OpTouch, Target: modelRef("*27")},
	})
	h.tick(1)
	car := lv.g.BlockerOf(lv.md.ByModel("*31").Index)
	g := h.nav.last()
	if g.Kind != navrt.GoalNodes || len(g.Nodes) == 0 {
		t.Fatalf("ride goal %s", g)
	}
	pos2 := PoseIndex(&lv.g.Blockers[car], "pos2")
	for _, id := range g.Nodes {
		if nd := &lv.g.Nodes[id]; nd.Blocker != car || int(nd.Pose) != pos2 {
			t.Fatalf("ride goal node %d on blocker %d pose %d", id, nd.Blocker, nd.Pose)
		}
	}
	// the car carries the bot into the exit trigger: both steps are done
	exit := lv.md.ByModel("*27")
	box := lv.md.Trigger(exit.Index).Box
	h.b.Self.Origin = Vec3{(box.Min[0] + box.Max[0]) / 2, (box.Min[1] + box.Max[1]) / 2, box.Min[2] + 10}
	h.tick(1)
	if h.x.Index() != 1 || h.x.Steps()[0].Reason != "carried into the next trigger" {
		t.Fatalf("ride: %+v", h.x.Steps()[0])
	}
}

// hatch returns demo2's hatch doors *49/*50 as graph blockers.
func hatch(t *testing.T, lv level) []int32 {
	t.Helper()
	var out []int32
	for _, m := range []string{"*49", "*50"} {
		b := lv.g.BlockerOf(lv.md.ByModel(m).Index)
		if b < 0 {
			t.Fatalf("%s is not a blocker", m)
		}
		out = append(out, b)
	}
	return out
}

func moverAt(lv level, b int32, pose int, at int64) worldmodel.Mover {
	bl := &lv.g.Blockers[b]
	return worldmodel.Mover{Model: bl.Model, Origin: bl.Poses[pose].Origin, Angles: bl.Poses[pose].Angles, LastUpdate: at, Visible: true}
}

func hatchSteps() []route.Step {
	return []route.Step{
		{Op: route.OpPress, Target: modelRef("*48")},
		{Op: route.OpWait, Seconds: 3, Effects: []route.Effect{
			{Kind: route.EffDoorOpen, Target: route.Ref{Model: "*49"}}, {Kind: route.EffDoorOpen, Target: route.Ref{Model: "*50"}}}},
	}
}

func testWaitForEffectsSeen(t *testing.T, lv level) {
	hb := hatch(t, lv)
	h := newHarness(t, lv, hatchSteps())
	h.tick(1)
	h.nav.arrive()
	h.tick(1)
	h.wantStep(1, StepRunning)
	d := h.tick(5)
	if !d.Hold || !d.HasLook {
		t.Fatalf("waiting: %+v", d)
	}
	// both halves seen open
	h.b.Movers = []worldmodel.Mover{moverAt(lv, hb[0], 1, h.now), moverAt(lv, hb[1], 1, h.now)}
	h.tick(2)
	if !h.x.Done() || h.x.Steps()[1].Reason != "effects seen" {
		t.Fatalf("wait: %+v", h.x.Steps()[1])
	}
}

func testWaitContradictedGoesBack(t *testing.T, lv level) {
	hb := hatch(t, lv)
	h := newHarness(t, lv, hatchSteps())
	h.tick(1)
	h.nav.arrive()
	h.tick(1)
	presses := len(h.nav.goals)
	// the hatch stays shut in plain view: the press did not take
	for i := 0; i < 50; i++ {
		h.b.Movers = []worldmodel.Mover{moverAt(lv, hb[0], 0, h.now), moverAt(lv, hb[1], 0, h.now)}
		h.tick(1)
	}
	h.wantStep(0, StepRunning)
	if h.x.Steps()[1].Attempts != 1 || !strings.Contains(h.x.Steps()[1].Reason, "did not happen") {
		t.Fatalf("wait after going back: %+v", h.x.Steps()[1])
	}
	h.tick(RetryPause/100 + 1)
	if len(h.nav.goals) != presses+1 {
		t.Fatal("the press was not attempted again")
	}
}

func testWaitUnseenEffectsAssumedAtTimeout(t *testing.T, lv level) {
	h := newHarness(t, lv, hatchSteps())
	h.tick(1)
	h.nav.arrive()
	h.tick(1)
	// nothing seen: after the wait's 3 s plus its overdue time it goes on
	h.tick(int(3000+1500+5000)/100 + 2)
	if !h.x.Done() || !strings.Contains(h.x.Steps()[1].Reason, "assumed") {
		t.Fatalf("wait: %+v", h.x.Steps()[1])
	}
}

func testConfirmLasers(t *testing.T, lv level) {
	steps := []route.Step{
		{Op: route.OpPress, Target: modelRef("*44")},
		{Op: route.OpConfirm, Effects: []route.Effect{
			{Kind: route.EffLaserOff, Target: route.Ref{Entity: ref(176).Entity}}, {Kind: route.EffLaserOff, Target: route.Ref{Entity: ref(177).Entity}}}},
	}
	t.Run("seen", func(t *testing.T) {
		h := newHarness(t, lv, steps)
		h.tick(1)
		h.nav.arrive()
		h.tick(1)
		h.b.Lasers = []worldmodel.Laser{{Lump: 176, State: worldmodel.LaserOff, LastUpdate: h.now}, {Lump: 177, State: worldmodel.LaserOff, LastUpdate: h.now}}
		if d := h.tick(1); !d.Done {
			t.Fatalf("confirm: %+v", h.x.Steps()[1])
		}
	})
	t.Run("unseen", func(t *testing.T) {
		h := newHarness(t, lv, steps)
		h.tick(1)
		h.nav.arrive()
		h.tick(1)
		n := len(h.nav.goals)
		h.tick(lookAround/100 + 2)
		if len(h.nav.goals) != n+1 || h.nav.last().Kind != navrt.GoalNodes || h.d.Hold || !h.d.HasLook {
			t.Fatalf("no walk to see the lasers: %d goals, %+v", len(h.nav.goals)-n, h.d)
		}
		h.tick(ConfirmTimeout / 100)
		h.wantStep(0, StepRunning)
		if !strings.Contains(h.x.Steps()[1].Reason, "not seen") {
			t.Errorf("confirm: %+v", h.x.Steps()[1])
		}
	})
	t.Run("contradicted", func(t *testing.T) {
		h := newHarness(t, lv, steps)
		h.tick(1)
		h.nav.arrive()
		h.tick(1)
		for i := 0; i < 30; i++ {
			h.b.Lasers = []worldmodel.Laser{{Lump: 176, State: worldmodel.LaserOn, LastUpdate: h.now}, {Lump: 177, State: worldmodel.LaserOff, LastUpdate: h.now}}
			h.tick(1)
		}
		h.wantStep(0, StepRunning)
	})
}

func testKill(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{{Op: route.OpKill, Class: "monster_gunner", Pos: vec(-536, -472, -272), Target: ref(418)}})
	spawn := lv.md.Entity(418).Origin
	h.b.Self.Origin, h.b.Self.Eye = Vec3{-536, -100, -280}, Vec3{-536, -100, -258}
	d := h.tick(1)
	if d.Kill == nil || d.Kill.Lump != 418 || d.Kill.Track != "" || dist3(d.Kill.Pos, spawn) > 40 {
		t.Fatalf("kill order before the monster is seen: %+v", d.Kill)
	}
	g := h.nav.last()
	if g.Kind != navrt.GoalNodes || len(g.Nodes) == 0 || len(g.Nodes) > maxFireNodes {
		t.Fatalf("firing position goal %s", g)
	}
	for _, id := range g.Nodes {
		if d := dist3(lv.g.Nodes[id].Origin, d.Kill.Pos); d > DefaultFireRange || d < minFireDist {
			t.Fatalf("firing node %d at %.0f units", id, d)
		}
	}
	// seen and shootable within range: stand and fight
	tr := worldmodel.Track{ID: "e7", Class: "gunner", Lump: 418, Pos: spawn, PosKnown: true, Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32},
		Visible: true, Shootable: true, Life: worldmodel.LifeAlive}
	h.b.Tracks = []worldmodel.Track{tr}
	d = h.tick(1)
	if !d.Hold || d.Kill == nil || d.Kill.Track != "e7" || !d.Kill.Visible {
		t.Fatalf("in view: %+v %+v", d, d.Kill)
	}
	// dying, then down for good
	h.b.Tracks[0].Life, h.b.Tracks[0].LifeAt = worldmodel.LifeDying, h.now
	h.tick(5)
	if h.x.Done() {
		t.Fatal("done on a death animation alone")
	}
	h.tick(dyingConfirm/100 + 1)
	if !h.x.Done() {
		t.Fatalf("not done: %+v", h.x.Current())
	}
}

func testFace(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{{Op: route.OpFace, Yaw: yaw(90)}})
	h.b.Self.ViewAngles = Vec3{0, 45, 0}
	if d := h.tick(1); !d.MustFace || d.FaceYaw != 90 || !d.Hold {
		t.Fatalf("face: %+v", d)
	}
	h.b.Self.ViewAngles = Vec3{0, 89.5, 0}
	if d := h.tick(1); !d.Done {
		t.Fatalf("not done facing 89.5: %+v", h.x.Current())
	}
}

func testAvoidSet(t *testing.T, lv level) {
	camp, err := route.Load("../../../../fixtures/agent/routes")
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range []*route.Table{camp.Tables[1], camp.Tables[3]} {
		n := newFakeNav(lv)
		x, err := New(tab, lv.md, lv.g, n, Config{})
		if err != nil {
			t.Fatal(err)
		}
		set := map[int32]bool{}
		for _, a := range x.Avoid() {
			set[a] = true
		}
		if !equalInt32(n.avoid, x.Avoid()) {
			t.Errorf("%s: navigator avoid %v, executor %v", tab.Name, n.avoid, x.Avoid())
		}
		for _, a := range tab.Avoid {
			e, _ := route.Resolve(a.Target, lv.md)
			if !set[int32(e.Index)] {
				t.Errorf("%s: avoid entry #%d missing from %v", tab.Name, e.Index, x.Avoid())
			}
		}
		// no step's own entity, nor anything leading to the table's exit
		for _, p := range x.plans {
			if p.ent >= 0 && set[int32(p.ent)] {
				t.Errorf("%s: step entity #%d avoided", tab.Name, p.ent)
			}
		}
		for a := range set {
			if leadsTo(lv.md, int(a), tab.Exit.Map) {
				t.Errorf("%s: avoids #%d, which leads to its own exit", tab.Name, a)
			}
		}
	}
}

func equalInt32(a, b []int32) bool {
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

func testObjectiveView(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{{Op: route.OpGoto, Pos: vec(0, 500, 24)}})
	h.b.Self.Origin, h.b.Self.Eye, h.b.Self.ViewAngles = Vec3{0, 0, 24}, Vec3{0, 0, 46}, Vec3{0, 0, 0}
	h.tick(1)
	ov := h.x.Objective()
	if ov == nil || ov.Kind != "goto" || !strings.HasPrefix(ov.Desc, "go to") {
		t.Fatalf("objective %+v", ov)
	}
	if math.Abs(float64(ov.Bearing-90)) > 0.5 {
		t.Errorf("bearing %v to a point on the left, want +90", ov.Bearing)
	}
	if ov.PathDist != -1 {
		t.Errorf("path distance %v without a path, want -1", ov.PathDist)
	}
	// a path of two edges: the distance to the first edge's end plus the
	// second's length, the waypoint the first edge's end
	var e1, e2 = -1, -1
	for i := range lv.g.Edges {
		e := &lv.g.Edges[i]
		lo, hi := lv.g.OutRange(e.To)
		if e.Kind == nav.EdgeWalk && hi > lo {
			e1, e2 = i, lo
			break
		}
	}
	h.nav.path, h.nav.cur = navrt.Path{Edges: []int{e1, e2}}, 0
	a, b2 := &lv.g.Edges[e1], &lv.g.Edges[e2]
	h.b.Self.Origin = lv.g.Nodes[a.From].Origin
	h.b.Self.Eye = add(h.b.Self.Origin, Vec3{0, 0, 22})
	ov = h.x.Objective()
	want := dist3(h.b.Self.Origin, lv.g.Nodes[a.To].Origin) + dist3(lv.g.Nodes[b2.From].Origin, lv.g.Nodes[b2.To].Origin)
	if math.Abs(float64(ov.PathDist-want)) > 0.01 {
		t.Errorf("path distance %v, want %v", ov.PathDist, want)
	}
	if ov.Stalled {
		t.Error("stalled at once")
	}
	h.tick(StalledAfter/100 + 2)
	if !h.x.Objective().Stalled {
		t.Error("not stalled after StalledAfter without progress")
	}
	h.nav.arrive()
	h.tick(1)
	if h.x.Objective() != nil {
		t.Error("an objective after the route is done")
	}
}

func testAssumeClaimsWhenNoPath(t *testing.T, lv level) {
	h := newHarness(t, lv, []route.Step{
		{Op: route.OpTouch, Target: modelRef("*22"), Effects: []route.Effect{
			{Kind: route.EffDoorOpen, Target: route.Ref{Model: "*55"}},
			{Kind: route.EffRemove, Target: route.Ref{Model: "*17"}},
			{Kind: route.EffMoverAt, Target: route.Ref{Model: "*51"}, Pose: "pos2"}}},
		{Op: route.OpTouch, Target: modelRef("*15")},
	})
	h.tick(1)
	h.nav.arrive()
	h.tick(1)
	h.wantStep(1, StepRunning)
	h.nav.fail(navrt.CauseNoPath, "no path")
	h.tick(assumeAfter/100 - 2)
	if len(h.nav.assumed) != 0 {
		t.Fatalf("assumed after %d ms of no path: %v", assumeAfter-200, h.nav.assumed)
	}
	h.tick(3)
	door, wall := lv.g.BlockerOf(lv.md.ByModel("*55").Index), lv.g.BlockerOf(lv.md.ByModel("*17").Index)
	lever := lv.g.BlockerOf(lv.md.ByModel("*51").Index)
	if p, ok := h.nav.assumed[door]; !ok || p != 1-int(lv.g.Blockers[door].Spawn) {
		t.Errorf("door *55 assumed %v %v", p, ok)
	}
	if p, ok := h.nav.assumed[wall]; !ok || p != -1 {
		t.Errorf("wall *17 assumed %v %v", p, ok)
	}
	if _, ok := h.nav.assumed[lever]; ok && lever >= 0 {
		t.Error("a mover's pose was assumed")
	}
	if bb := h.nav.ms.Belief(door); !bb.Assumed || bb.Status != navrt.BlockerAt {
		t.Errorf("door belief %+v", bb)
	}
	// an assumed belief does not count as seen for a later wait
	x := h.x
	f := &x.plans[0].effects[0]
	if v := x.judge(f, &h.b, 0); v != unseen {
		t.Errorf("assumed door judged %s", v)
	}
}

// TestCheckedInTables makes an executor for every checked-in table on its
// map with the real navigator and checks that every step's goal is one
// the graph can satisfy (SetGoal refuses the others).
func TestCheckedInTables(t *testing.T) {
	camp, err := route.Load("../../../../fixtures/agent/routes")
	if err != nil {
		t.Fatal(err)
	}
	levels := map[string]level{}
	for _, tab := range camp.Tables {
		lv, ok := levels[tab.Map]
		if !ok {
			lv = demo(t, tab.Map)
			levels[tab.Map] = lv
		}
		n := navrt.New(lv.g, lv.md, navrt.Config{})
		x, err := New(tab, lv.md, lv.g, n, Config{})
		if err != nil {
			t.Fatalf("%s: %v", tab.Name, err)
		}
		for i := range x.plans {
			p := &x.plans[i]
			switch p.op {
			case route.OpGoto, route.OpTouch, route.OpPress, route.OpShoot, route.OpRide, route.OpPickup:
				x.cur = i
				g, err := x.moveGoal(p)
				if err != nil {
					t.Errorf("%s step %d (%s): %v", tab.Name, i, p.desc, err)
					continue
				}
				if err := n.SetGoal(g, 0); err != nil {
					t.Errorf("%s step %d (%s): %v", tab.Name, i, p.desc, err)
				}
			case route.OpKill:
				if nodes := x.firingNodes(x.spawnCenter(p), false); len(nodes) == 0 {
					t.Errorf("%s step %d (%s): no firing position", tab.Name, i, p.desc)
				}
			}
		}
	}
}
