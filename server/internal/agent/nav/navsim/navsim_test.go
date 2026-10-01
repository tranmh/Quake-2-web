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

// idleExec holds still.
type idleExec struct{}

func (idleExec) Next(_ *World, s *State) Cmd { return Cmd{Msec: 25} }

// TestFallDamageWorstPhase drops the player on the slab from several
// heights: Run must report the damage of the worst server frame phase,
// including the frame the landing happens in (judged after the run stopped
// at the landing, by looking ahead), and leave the runner at the landing.
func TestFallDamageWorstPhase(t *testing.T) {
	w := NewWorld(floorMap(t))
	for _, h := range []float32{230, 300, 420, 700} {
		start := Vec3{0, 0, 24.125 + h}
		// reference: step idle commands and judge every phase by hand
		ref := NewRunner(w, DefaultPhysics())
		ref.Reset(start, false)
		var vel []Vec3
		var ground []bool
		landed := -1
		for i := 0; landed < 0 || i < landed+4; i++ {
			ref.Step(Cmd{})
			vel = append(vel, ref.State().Velocity())
			ground = append(ground, ref.State().OnGround())
			if landed < 0 && ref.State().OnGround() {
				landed = i
			}
		}
		worst := 0
		for k := 0; k < 4; k++ {
			old, dmg := Vec3{}, 0
			for i := 0; i <= landed+3; i++ {
				if (i+1+k)%4 != 0 {
					continue
				}
				dmg += FallingDamage(vel[i], old, ground[i], 0)
				old = vel[i]
			}
			worst = max(worst, dmg)
		}

		r := NewRunner(w, DefaultPhysics())
		r.Reset(start, false)
		var out Outcome
		r.Run(idleExec{}, func(s *State, _ *StepResult) bool { return s.OnGround() }, 5000, &out)
		if !out.Done || len(out.Cmds) != landed+1 {
			t.Fatalf("%v: run stopped after %d steps, landing at %d", h, len(out.Cmds), landed+1)
		}
		if out.FallDamage != worst {
			t.Errorf("%vu: fall damage %d, worst phase %d", h, out.FallDamage, worst)
		}
		// about what a continuous free fall deals (the last boundary before
		// the landing sees up to a step's worth of speed less)
		if want := FallDamageForDrop(h, 800); out.FallDamage <= 0 || out.FallDamage < want-3 {
			t.Errorf("%vu: fall damage %d, a free fall deals %d", h, out.FallDamage, want)
		}
		// the look-ahead put the runner back at the landing
		if st := r.State(); st.Msec != 25*(landed+1) || !st.OnGround() || st.Velocity()[2] != vel[landed][2] {
			t.Errorf("%vu: runner after Run at %d ms, %v", h, st.Msec, st.Velocity())
		}
	}
}

func TestStopAt(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Settle(Vec3{-50, 0, 30}, false, 2000)
	for i := 0; i < 12; i++ { // full speed east
		r.Step(Cmd{Forward: 400})
	}
	if r.State().HSpeed() < 290 {
		t.Fatalf("speed %v", r.State().HSpeed())
	}
	p := r.State().Origin()
	p[0] -= 4 // behind the player: it has to stop and come back
	var out Outcome
	r.Run(StopAt(p, 25), func(s *State, _ *StepResult) bool { return Stopped(s, p) }, 2000, &out)
	if !out.Done || out.Msec > 1000 {
		t.Fatalf("not at rest at %v after %d ms: %v at %v", p, out.Msec, r.State().Origin(), r.State().HSpeed())
	}
	// at rest at the point: a no-op
	ex := StopAt(r.State().Origin(), 25)
	if c := ex.Next(w, r.StatePtr()); c.Forward != 0 || c.Side != 0 || c.Up != 0 {
		t.Errorf("command at rest %+v", c)
	}
	// the jump, drop and ladder executors stop first, so their commands
	// from rest at the start are what they always were
	o := r.State().Origin()
	for _, rec := range []Recipe{RecipeJump, RecipeDrop} {
		pl := Plan{Recipe: rec, From: o, Target: Vec3{o[0] + 64, o[1], o[2]}, Takeoff: o}
		if c := pl.Executor().Next(w, r.StatePtr()); c.Forward <= 0 || c.Yaw != 0 {
			t.Errorf("%v from rest at the start: %+v", rec, c)
		}
	}
	// ... and a running player is braked (thrust against the motion)
	for i := 0; i < 8; i++ {
		r.Step(Cmd{Forward: 400})
	}
	pl := Plan{Recipe: RecipeJump, From: r.State().Origin(), Target: Vec3{200, 0, 24}, Takeoff: r.State().Origin()}
	if c := pl.Executor().Next(w, r.StatePtr()); c.Up != 0 || AngleDiff(c.Yaw, 180) > 1 {
		t.Errorf("jump from a running entry did not brake first: %+v", c)
	}
}

// AngleDiff is the absolute difference of two yaws (degrees).
func AngleDiff(a, b float32) float32 {
	d := a - b
	for d > 180 {
		d -= 360
	}
	for d < -180 {
		d += 360
	}
	if d < 0 {
		d = -d
	}
	return d
}

func TestCoast(t *testing.T) {
	w := NewWorld(floorMap(t))
	r := NewRunner(w, DefaultPhysics())
	r.Settle(Vec3{-50, 0, 30}, false, 2000)
	stop := false
	ex := Coast(Plan{Recipe: RecipeCrouch, Target: Vec3{50, 0, 24}}.Executor(), &stop)
	if c := ex.Next(w, r.StatePtr()); c.Forward != 400 || c.Up != -400 {
		t.Fatalf("before the stop %+v", c)
	}
	stop = true
	if c := ex.Next(w, r.StatePtr()); c.Forward != 0 || c.Up != -400 || c.Yaw != 0 {
		t.Fatalf("coasting %+v", c)
	}
	if !AtRest(r.StatePtr()) {
		t.Error("a settled player is at rest")
	}
}
