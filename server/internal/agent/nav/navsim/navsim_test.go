package navsim

import (
	"testing"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
)

func floorMap(t testing.TB) *cmodel.Map {
	t.Helper()
	m, err := cmodel.LoadMapBytes("synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestWorldTraceEntIds(t *testing.T) {
	w := NewWorld(floorMap(t))
	// a hit on the slab and a miss both report the world (SV_Trace sets
	// trace.ent = ge->edicts unconditionally), never cmodel's -1
	hit := w.Trace(Vec3{0, 0, 40}, StandMins(), StandMaxs(), Vec3{0, 0, -40}, q2const.MASK_PLAYERSOLID)
	if hit.Fraction >= 1 || hit.Ent != WorldEnt || hit.Plane.Normal[2] != 1 {
		t.Fatalf("floor hit %+v", hit)
	}
	miss := w.Trace(Vec3{0, 0, 100}, StandMins(), StandMaxs(), Vec3{0, 0, 200}, q2const.MASK_PLAYERSOLID)
	if miss.Fraction != 1 || miss.Ent != WorldEnt {
		t.Fatalf("miss %+v", miss)
	}
	// a box solid above the floor is hit first and reported by its id
	w.AddSolid(Solid{ID: 7, Box: true, Origin: Vec3{0, 0, 30}, Mins: Vec3{-8, -8, -4}, Maxs: Vec3{8, 8, 4}})
	tr := w.Trace(Vec3{0, 0, 100}, StandMins(), StandMaxs(), Vec3{0, 0, -40}, q2const.MASK_PLAYERSOLID)
	if tr.Ent != 7 || tr.EndPos[2] <= hit.EndPos[2] {
		t.Fatalf("box hit %+v (floor end %v)", tr, hit.EndPos)
	}
	if c := w.PointContents(Vec3{0, 0, 30}); c&q2const.CONTENTS_MONSTER == 0 {
		t.Errorf("box contents %#x", c)
	}
	if c := w.PointContents(Vec3{0, 0, -8}); c&q2const.CONTENTS_SOLID == 0 {
		t.Errorf("slab contents %#x", c)
	}
	if !w.Fits(Vec3{40, 0, 24.125}, StandMins(), StandMaxs()) || w.Fits(Vec3{40, 0, 10}, StandMins(), StandMaxs()) ||
		w.Fits(Vec3{0, 0, 24.125}, StandMins(), StandMaxs()) {
		t.Error("Fits: the hull fits on the slab, not in it nor in the box")
	}
}

func TestRunnerStandsAndWalks(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	if !r.Settle(Vec3{-40, 0, 30}, false, 2000) {
		t.Fatalf("did not settle: %+v", r.State())
	}
	st := r.State()
	if st.Ground != WorldEnt || st.PM.PmFlags&q2const.PMF_ON_GROUND == 0 {
		t.Fatalf("not on the world: ground %d flags %#x", st.Ground, st.PM.PmFlags)
	}
	if z := st.Origin()[2]; z < 24 || z > 25 {
		t.Fatalf("rest height %v", z)
	}
	var out Outcome
	target := Vec3{40, 0, 24}
	plan := Plan{Recipe: RecipeWalk, From: st.Origin(), Target: target}
	r.Run(plan.Executor(), func(s *State, _ *StepResult) bool { return Arrived(s, target, ArriveGround) }, TimeLimitMsec(80), &out)
	if !out.Done {
		t.Fatalf("walk did not arrive: end %v after %d ms", r.State().Origin(), out.Msec)
	}
	if out.TookOff || out.FallDamage != 0 || len(out.Touched) != 0 {
		t.Errorf("walk outcome %+v", out)
	}
	if len(out.Samples) != len(out.Cmds)+1 || out.Msec != 25*len(out.Cmds) {
		t.Errorf("samples %d cmds %d msec %d", len(out.Samples), len(out.Cmds), out.Msec)
	}
	// running at up to 300 u/s from rest covers 64 units in about 0.3 s
	if out.Msec < 200 || out.Msec > 600 {
		t.Errorf("walk took %d ms", out.Msec)
	}
}

func TestRunnerVolumesAndFall(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Volumes = []Volume{{ID: 3, Min: Vec3{20, -10, 0}, Max: Vec3{30, 10, 40}}}
	r.Settle(Vec3{-40, 0, 30}, false, 2000)
	var out Outcome
	// run off the +x edge of the slab (x = 64) and fall into the void
	target := Vec3{200, 0, 24}
	plan := Plan{Recipe: RecipeWalk, Target: target}
	r.Run(plan.Executor(), nil, 3000, &out)
	if len(out.Volumes) != 1 || out.Volumes[0].ID != 3 || out.Volumes[0].Yaw != 0 {
		t.Fatalf("volumes %+v", out.Volumes)
	}
	if !out.TookOff || out.Takeoff[0] < 40 || out.Takeoff[0] > 80 || out.TakeoffSpeed < 250 {
		t.Errorf("takeoff %v at %v (%v)", out.TookOff, out.Takeoff, out.TakeoffSpeed)
	}
	if r.State().OnGround() || r.State().Origin()[2] > -500 {
		t.Errorf("should be falling: %v", r.State().Origin())
	}
}

func TestFallingDamage(t *testing.T) {
	// 187.5 units is the threshold (delta = 0.16 h > 30)
	if d := FallDamageForDrop(180, 800); d != 0 {
		t.Errorf("180u: %d", d)
	}
	if d := FallDamageForDrop(200, 800); d < 1 || d > 2 {
		t.Errorf("200u: %d", d)
	}
	if d := FallDamageForDrop(1000, 800); d < 64 || d > 65 {
		t.Errorf("1000u: %d", d)
	}
	// in the air nothing happens; under water nothing either
	if FallingDamage(Vec3{0, 0, -600}, Vec3{0, 0, -500}, false, 0) != 0 {
		t.Error("airborne")
	}
	if FallingDamage(Vec3{}, Vec3{0, 0, -900}, true, 3) != 0 {
		t.Error("under water")
	}
	if a, b := FallingDamage(Vec3{}, Vec3{0, 0, -900}, true, 0), FallingDamage(Vec3{}, Vec3{0, 0, -900}, true, 1); b >= a {
		t.Errorf("waist deep water should halve the delta: %d vs %d", b, a)
	}
}

func TestRunnerJumpAndRelease(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Settle(Vec3{-40, 0, 30}, false, 2000)
	// a standing jump: takeoff at the start
	o := r.State().Origin()
	plan := Plan{Recipe: RecipeJump, From: o, Target: Vec3{40, 0, 24}, Takeoff: o}
	ex := plan.Executor()
	var ups []int16
	for i := 0; i < 40; i++ {
		c := ex.Next(w, r.StatePtr())
		ups = append(ups, c.Up)
		r.Step(c)
	}
	n := 0
	for _, u := range ups {
		if u != 0 {
			n++
		}
	}
	if ups[0] != 400 || n != 1 {
		t.Fatalf("jump must be pressed exactly once at takeoff: %v", ups)
	}
	if !r.State().OnGround() {
		t.Error("did not land")
	}
}

func TestRunnerHooks(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Pushes = []Push{{ID: 5, Min: Vec3{-10, -10, 0}, Max: Vec3{10, 10, 60}, Velocity: Vec3{0, 0, 500}, Once: true}}
	r.Settle(Vec3{-40, 0, 30}, false, 2000)
	var out Outcome
	plan := Plan{Recipe: RecipeWalk, Target: Vec3{60, 0, 24}}
	r.Run(plan.Executor(), func(s *State, res *StepResult) bool { return res.Pushed != 0 }, 2000, &out)
	if !out.Done || out.Pushed != 5 {
		t.Fatalf("no push: %+v", out)
	}
	if v := r.State().Velocity(); v[2] != 500 {
		t.Fatalf("push velocity %v", v)
	}
	r.Step(Cmd{})
	if r.State().OnGround() || r.State().Velocity()[2] <= 400 {
		t.Errorf("not pushed up: %+v", r.State())
	}

	// a teleporter moves the player and turns the view
	r.Pushes = nil
	r.Teleports = []Teleport{{ID: 9, Min: Vec3{-10, -10, 0}, Max: Vec3{10, 10, 60}, Dest: Vec3{-40, -40, 24}, Angles: Vec3{0, 90, 0}}}
	r.Settle(Vec3{-40, 0, 30}, false, 2000)
	r.Run(plan.Executor(), func(s *State, res *StepResult) bool { return res.Teleported != 0 }, 2000, &out)
	st := r.State()
	if !out.Done || st.Origin() != (Vec3{-40, -40, 34}) || st.PM.PmFlags&q2const.PMF_TIME_TELEPORT == 0 {
		t.Fatalf("teleport: %+v %v", out.Done, st)
	}
	r.Step(Cmd{Yaw: 0})
	// the teleport hold keeps the player in place and the view at 90
	if r.State().Origin()[0] != -40 {
		t.Errorf("moved during the teleport hold: %v", r.State().Origin())
	}
}

func TestCrouchExecutorDucks(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Settle(Vec3{-40, 0, 30}, false, 2000)
	plan := Plan{Recipe: RecipeCrouch, Target: Vec3{40, 0, 24}}
	var out Outcome
	r.Run(plan.Executor(), func(s *State, _ *StepResult) bool { return Arrived(s, plan.Target, ArriveGround) }, 3000, &out)
	if !out.Done || !r.State().Ducked() {
		t.Fatalf("crouch walk: done %v ducked %v", out.Done, r.State().Ducked())
	}
	// ducked speed is 100: 80 units take about a second
	if out.Msec < 500 {
		t.Errorf("crouch too fast: %d ms", out.Msec)
	}
}
