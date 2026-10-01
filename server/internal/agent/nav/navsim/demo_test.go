package navsim

import (
	"math"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/testutil"
)

// Recipes on real demo1 geometry (the spots come from the built graph; the
// world here has no entity solids, and none stand near these spots).

func demo1World(t *testing.T) *World {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	m, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return NewWorld(m)
}

// runPlan places the player at from at rest (reset plus one idle command,
// as the graph builder starts every edge) and runs plan until it arrives at
// to or the edge time limit passes.
func runPlan(t *testing.T, w *World, plan Plan, to Vec3) (*Runner, *Outcome, bool) {
	t.Helper()
	r := NewRunner(w, DefaultPhysics())
	r.Reset(plan.From, false)
	r.Step(Cmd{})
	if !r.State().OnGround() {
		t.Fatalf("start %v is not on ground", plan.From)
	}
	var out Outcome
	arrived := false
	r.Run(plan.Executor(), func(s *State, _ *StepResult) bool {
		arrived = Arrived(s, to, ArriveGround)
		return arrived
	}, TimeLimitMsec(dist(plan.From, to))+plan.BackupMsec, &out)
	return r, &out, arrived
}

func dist(a, b Vec3) float32 {
	var d float64
	for k := 0; k < 3; k++ {
		d += float64(a[k]-b[k]) * float64(a[k]-b[k])
	}
	return float32(math.Sqrt(d))
}

func TestDemo1Stairs(t *testing.T) {
	w := demo1World(t)
	from, to := Vec3{648, 264, -72.375}, Vec3{672, 224, -31.875}
	_, out, ok := runPlan(t, w, Plan{Recipe: RecipeWalk, From: from, Target: to}, to)
	if !ok {
		t.Fatalf("did not climb the stairs: %d ms", out.Msec)
	}
	// 40 units up in two risers: pmove steps up (no jump), and the walk
	// takes a fraction of a second
	rise := false
	for i := 1; i < len(out.Samples); i++ {
		if out.Samples[i].Origin[2] > out.Samples[i-1].Origin[2]+10 {
			rise = true
		}
	}
	if !rise || out.Msec > 600 {
		t.Errorf("stairs: stepped %v in %d ms", rise, out.Msec)
	}
	for _, c := range out.Cmds {
		if c.Up != 0 {
			t.Fatalf("walk pressed up: %+v", c)
		}
	}
}

func TestDemo1GapJump(t *testing.T) {
	w := demo1World(t)
	from, to := Vec3{320, 32, -39.875}, Vec3{448, 128, -39.875}
	takeoff := Vec3{339.2, 46.4, -39.875}
	_, out, ok := runPlan(t, w, Plan{Recipe: RecipeJump, From: from, Target: to, Takeoff: takeoff}, to)
	if !ok {
		t.Fatalf("jump did not make it across: ended after %d ms at %v", out.Msec, out.Samples[len(out.Samples)-1].Origin)
	}
	ups := 0
	for _, c := range out.Cmds {
		if c.Up > 0 {
			ups++
		}
	}
	if ups != 1 || !out.TookOff || dist(out.Takeoff, takeoff) > 12 {
		t.Errorf("one jump at the takeoff: %d presses, took off at %v", ups, out.Takeoff)
	}
	// without the jump the player falls into the gap
	if _, out, ok := runPlan(t, w, Plan{Recipe: RecipeWalk, From: from, Target: to}, to); ok {
		t.Errorf("walking arrived too (%d ms): not a gap", out.Msec)
	}
}

func TestDemo1RunUpJump(t *testing.T) {
	w := demo1World(t)
	// standing at the lip: only backing up 300 ms first gives the speed
	from, to := Vec3{928, 96, -31.875}, Vec3{800, 64, -31.875}
	plan := Plan{Recipe: RecipeJump, From: from, Target: to, Takeoff: from, BackupMsec: 300}
	_, out, ok := runPlan(t, w, plan, to)
	if !ok {
		t.Fatalf("run-up jump failed after %d ms", out.Msec)
	}
	if out.Cmds[0].Forward >= 0 || out.TakeoffSpeed < 250 {
		t.Errorf("no back-up (first forward %d) or slow takeoff %v", out.Cmds[0].Forward, out.TakeoffSpeed)
	}
	plan.BackupMsec = 0
	if _, _, ok := runPlan(t, w, plan, to); ok {
		t.Error("a standing jump made it: the run-up is not needed")
	}
}

func TestDemo1Drop(t *testing.T) {
	w := demo1World(t)
	from, to := Vec3{-320, 800, -47.875}, Vec3{-320, 1120, -245.25}
	r, out, ok := runPlan(t, w, Plan{Recipe: RecipeWalk, From: from, Target: to}, to)
	if !ok {
		t.Fatalf("did not land at the bottom: %d ms at %v", out.Msec, r.State().Origin())
	}
	if !out.TookOff || out.Takeoff[1] < 810 || out.Takeoff[1] > 840 {
		t.Errorf("walked off at %v (%v)", out.Takeoff, out.TookOff)
	}
	// a 197 unit fall is right at the falling damage threshold (187.5 from
	// rest): at most a point of damage, judged at the frame boundaries
	if out.FallDamage > 2 {
		t.Errorf("fall damage %d", out.FallDamage)
	}
}
