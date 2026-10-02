package navrt

import (
	"strings"
	"testing"

	"quake2web/server/internal/q2const"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/qcommon/shared"
)

// Live goals on the real game: what the random walks of the gate never
// do (they avoid triggers and buttons, and plan in the level as it
// spawns), with the route tables' entities. Each map's graph is loaded
// once and shared by the subtests, which start their own sessions.

// TestLiveDemo1 runs the demo1 lockstep tests: the route smoke into the
// car, a directional trigger, the car exit, a shot button, and the
// prediction against the server.
func TestLiveDemo1(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep runs")
	}
	d := loadDemo(t, "demo1")
	t.Run("RouteSmokeCar", func(t *testing.T) { testRouteSmokeCar(t, d) })
	t.Run("DirectionalTrigger", func(t *testing.T) { testDirectionalTrigger(t, d) })
	t.Run("CarExit", func(t *testing.T) { testCarExit(t, d) })
	t.Run("ShootButton", func(t *testing.T) { testShootButton(t, d) })
	t.Run("ItemPickup", func(t *testing.T) { testItemPickup(t, d) })
	t.Run("PredictionMatchesServer", func(t *testing.T) { testPredictionMatchesServer(t, d) })
}

// TestLiveDemo2 runs the demo2 lockstep tests: the hatch exit.
func TestLiveDemo2(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep runs")
	}
	d := loadDemo(t, "demo2")
	t.Run("HatchDrop", func(t *testing.T) { testHatchDrop(t, d) })
}

// budgetFor plans goal from where the bot is and returns the gate's time
// budget for it (gateBudget: the path's travel time x 1.5 + 5 s) and that
// travel time. A goal that waits on
// something the bot set off (a car that starts after its button, a hatch
// swinging open) may have no plan yet: the bot stands still for up to 5 s
// until it has one, and the wait counts towards the budget.
func (b *liveBot) budgetFor(goal Goal) (int64, float32) {
	b.t.Helper()
	var t *Target
	if goal.Kind == GoalShoot {
		t = b.nav.shootTarget(goal)
	} else {
		t = CompileTarget(b.g, goal)
	}
	start := b.now()
	for {
		here := b.nav.Localize(b.origin(), b.l.Client().Frame.PlayerState.PMove.PmFlags&1 != 0)
		b.nav.now = b.now()
		if p, ok := b.nav.pl.Find(here, t, b.nav.cost); ok {
			budget, secs := gateBudget(b.nav, p)
			return budget + b.now() - start, secs
		}
		if b.now()-start > 5000 {
			b.t.Fatalf("no plan for %s from node %d at %v", goal, here, b.origin())
		}
		b.step()
	}
}

// mustArrive runs goal within its budget and fails the test unless the
// navigator arrives.
func (b *liveBot) mustArrive(goal Goal) Status {
	b.t.Helper()
	budget, planned := b.budgetFor(goal)
	st, ms, err := b.runGoal(goal, budget)
	if err != nil {
		b.t.Fatalf("%s: %v", goal, err)
	}
	if st.Follow != Arrived {
		b.t.Fatalf("%s: %s after %.1fs of %.1fs (planned %.1fs) at %v: %s %s (last %s %s)", goal, st.Follow, float64(ms)/1000, float64(budget)/1000, planned, b.origin(), st.Cause, st.Reason, st.LastCause, st.LastReason)
	}
	b.t.Logf("%s: arrived in %.1fs (planned %.1fs), %d repaths, %d stucks", goal, float64(ms)/1000, planned, st.Repaths, st.Stucks)
	return st
}

// mustExit runs goal (a trigger that ends the level) and fails the test
// unless the client goes on to map next within the budget (the server
// sends the next level a few frames after the trigger fires).
func (b *liveBot) mustExit(goal Goal, next string) {
	b.t.Helper()
	budget, planned := b.budgetFor(goal)
	start := b.now()
	st, _, err := b.runGoal(goal, budget)
	for err == nil && b.now()-start < budget && b.l.Client().MapName() == b.g.Map {
		if err = b.l.Step(b.ctx, b.drv.Cmd); err != nil {
			b.t.Fatal(err)
		}
	}
	ms := b.now() - start
	if b.l.Client().MapName() == b.g.Map {
		b.t.Fatalf("%s: still on %s after %.1fs of %.1fs (planned %.1fs) at %v: %s %s %s (last %s %s)", goal, b.g.Map, float64(ms)/1000, float64(budget)/1000, planned, b.origin(), st.Follow, st.Cause, st.Reason, st.LastCause, st.LastReason)
	}
	for i := 0; i < 50 && b.l.Client().MapName() != next; i++ {
		if err := b.l.Step(b.ctx, nil); err != nil {
			b.t.Fatal(err)
		}
	}
	if got := b.l.Client().MapName(); got != next {
		b.t.Fatalf("%s: went on to %q, want %q", goal, got, next)
	}
	b.t.Logf("%s: on to %s in %.1fs (planned %.1fs)", goal, next, float64(ms)/1000, planned)
}

// testDirectionalTrigger walks into demo1's directional trigger_once
// *18 (#286, angle 270, "Crouch here"): Touch_Multi only fires for a
// player facing within 90 degrees of its movedir, so the server's
// centerprint proves the facing; and the navigator must have faced 270
// degrees (MustFace) for at least 200 ms before the bot entered it.
func testDirectionalTrigger(t *testing.T, d *demoLevel) {
	b := startBotOn(t, d, 1, Config{})
	const ent = 286
	tr := b.md.Trigger(ent)
	if tr == nil || !tr.Directional() {
		t.Fatalf("#%d is not a directional trigger", ent)
	}
	type cmd struct {
		in     control.MoveIntent
		inside bool
	}
	var cmds []cmd
	b.drv.OnCmd = func(in control.MoveIntent, _ shared.UserCmd, s *navsim.State) {
		cmds = append(cmds, cmd{in, boxTouch(s.Origin(), s.Ducked(), tr.Box.Min, tr.Box.Max)})
	}
	b.mustArrive(TouchGoal(ent))
	b.drv.OnCmd = nil
	for i := 0; i < 5; i++ {
		b.step()
	}
	found := false
	for _, m := range b.l.Client().CenterPrints {
		found = found || strings.Contains(m, "Crouch here")
	}
	if !found {
		t.Errorf("the trigger did not fire: centerprints %q", b.l.Client().CenterPrints)
	}
	first := -1
	for k, c := range cmds {
		if c.inside {
			first = k
			break
		}
	}
	if first < 0 {
		t.Fatal("no command ran inside the trigger")
	}
	const lead = 8 // 200 ms of 25 ms commands
	if first < lead {
		t.Fatalf("entered the trigger %d commands after the goal was set", first)
	}
	for k := first - lead; k < first; k++ {
		if in := cmds[k].in; !in.MustFace || absf(control.AngleDelta(in.FaceYaw, 270)) > 1 {
			t.Errorf("command %d (%d before entering): face %.1f must %v", k, first-k, in.FaceYaw, in.MustFace)
		}
	}
}

func absf(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// testCarExit runs the demo1 exit by navigator goals alone: into
// the car *31, press its button *34 (a touch goal: reached once the
// button is seen moving or touched), then touch the exit trigger *27,
// which only the car's ride down leads into (an edge whose effects
// include the trigger satisfies the goal; the ride is planned once the
// car is seen moving).
func testCarExit(t *testing.T, d *demoLevel) {
	b := startBotOn(t, d, 1, Config{})
	car := b.md.ByModel("*31")
	bl := b.g.BlockerOf(car.Index)
	b.mustArrive(NodeGoal(b.g.MoverNodes(bl, 0)...))
	button, exit := b.md.ByModel("*34"), b.md.ByModel("*27")
	b.mustArrive(TouchGoal(int32(button.Index)))
	b.mustExit(TouchGoal(int32(exit.Index)), "demo2")
}

// testHatchDrop runs the first demo2 exit by navigator goals:
// press the hatch button *48 (avoiding the arrival car's button *43 and
// the other exits), then touch the mid-air trigger *40 under the hatch,
// which the drop down the shaft passes through once the hatch is seen
// open.
func testHatchDrop(t *testing.T, d *demoLevel) {
	b := startBotOn(t, d, 1, Config{})
	exit := b.md.ByModel("*40")
	b.avoidExits(int32(exit.Index))
	t.Logf("avoiding %v", b.nav.cfg.Avoid)
	button := b.md.ByModel("*48")
	b.mustArrive(TouchGoal(int32(button.Index)))
	b.mustExit(TouchGoal(int32(exit.Index)), "demo3")
}

// testShootButton shoots demo1's shootable button *13 (#167, health
// 1): the navigator goes to a spot with a clear shot at it, faces it
// (MustFace) and fires the blaster until it sees the button move.
func testShootButton(t *testing.T, d *demoLevel) {
	b := startBotOn(t, d, 1, Config{})
	b.avoidExits()
	button := b.md.ByModel("*13")
	mv := b.md.Mover(button.Index)
	if mv == nil || mv.Health <= 0 {
		t.Fatal("*13 is not a shootable button")
	}
	fired := 0
	b.drv.OnCmd = func(in control.MoveIntent, _ shared.UserCmd, _ *navsim.State) {
		if in.Fire {
			fired++
		}
	}
	b.mustArrive(ShootGoal(int32(button.Index), mv.Box.Center()))
	if fired == 0 {
		t.Error("arrived without firing")
	}
	t.Logf("fired %d commands", fired)
}

// testItemPickup picks up demo1's armor shard #155 (always taken, whatever
// the bot carries): an item goal is reached once the bot touched the item
// (or saw it taken), and the server's armor stat proves the pickup.
func testItemPickup(t *testing.T, d *demoLevel) {
	b := startBotOn(t, d, 1, Config{})
	b.avoidExits()
	const shard = 155
	if it := b.md.Entity(shard); it == nil || it.Classname != "item_armor_shard" {
		t.Fatalf("#%d is not an armor shard", shard)
	}
	before := b.l.Client().Frame.PlayerState.Stats[q2const.STAT_ARMOR]
	b.mustArrive(ItemGoal(shard))
	for i := 0; i < 3; i++ {
		b.step()
	}
	if after := b.l.Client().Frame.PlayerState.Stats[q2const.STAT_ARMOR]; after <= before {
		t.Errorf("armor %d after the pickup, %d before", after, before)
	}
}
