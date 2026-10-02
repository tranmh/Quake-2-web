package control

import (
	"math"
	"testing"
)

func mustWeapon(t *testing.T, name string) Weapon {
	t.Helper()
	w, ok := WeaponByPickup(name)
	if !ok {
		t.Fatalf("no weapon %q", name)
	}
	return w
}

// aimed is a fire input looking straight at a visible, shootable target
// 400 units ahead along +x.
func aimed(t *testing.T, w string, mode FireMode) FireInput {
	return FireInput{Mode: mode, Weapon: mustWeapon(t, w), Eye: Vec3{0, 0, 0}, Yaw: 0, Pitch: 0,
		Aim: Vec3{400, 0, 0}, Radius: 16, Visible: true, Shootable: true}
}

// TestFireGate covers each gate of the fire reflex.
func TestFireGate(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(*FireInput)
		fire   bool
		reason string
	}{
		{"aligned on target", func(in *FireInput) {}, true, ""},
		{"hold never fires", func(in *FireInput) { in.Mode = FireHold }, false, NoFireHold},
		{"neutral in the line", func(in *FireInput) { in.Neutral = true }, false, NoFireNeutral},
		{"neutral beats suppress", func(in *FireInput) { in.Mode, in.Neutral = FireSuppress, true }, false, NoFireNeutral},
		{"a barrel close in the line", func(in *FireInput) { in.Barrel = true }, false, NoFireBarrel},
		{"not visible: aligned holds", func(in *FireInput) { in.Visible = false }, false, NoFireNotVisible},
		{"no line of fire", func(in *FireInput) { in.Shootable = false }, false, NoFireNoLine},
		{"suppress without a line", func(in *FireInput) { in.Mode, in.Visible, in.Shootable = FireSuppress, false, false }, false, NoFireNoLine},
		{"suppress at a remembered spot", func(in *FireInput) { in.Mode, in.Visible = FireSuppress, false }, true, ""},
		// 16 units at 400 is 2.3 degrees: 5 degrees off misses aligned...
		{"aim off", func(in *FireInput) { in.Yaw = 5 }, false, NoFireAim},
		// ... but suppress fires within 2*2.3+3
		{"suppress wider tolerance", func(in *FireInput) { in.Yaw, in.Mode = 5, FireSuppress }, true, ""},
		{"suppress still needs some aim", func(in *FireInput) { in.Yaw, in.Mode = 15, FireSuppress }, false, NoFireAim},
	}
	for _, tc := range cases {
		in := aimed(t, "Machinegun", FireAligned)
		tc.edit(&in)
		v := FireGate(in)
		if v.Fire != tc.fire || v.Reason != tc.reason {
			t.Errorf("%s: fire %v reason %q (err %.2f tol %.2f), want %v %q", tc.name, v.Fire, v.Reason, v.Err, v.Tol, tc.fire, tc.reason)
		}
	}
}

// TestFireGateSplash: a splash weapon never fires at a target or a wall
// closer than SplashSafe; hitscan weapons do.
func TestFireGateSplash(t *testing.T) {
	for _, name := range []string{"Rocket Launcher", "Grenade Launcher", "BFG10K"} {
		in := aimed(t, name, FireAligned)
		if v := FireGate(in); !v.Fire {
			t.Errorf("%s at 400: %+v", name, v)
		}
		in.Aim = Vec3{SplashSafe - 10, 0, 0}
		if v := FireGate(in); v.Fire || v.Reason != NoFireSplashClose || !v.Vetoed() {
			t.Errorf("%s at %d: %+v", name, SplashSafe-10, v)
		}
		// a crossing target 140 units away, the aim point led past
		// SplashSafe: the target's own distance holds the trigger
		in = aimed(t, name, FireAligned)
		in.Aim, in.TargetDist = Vec3{140, 70, 0}, 140
		if v := FireGate(in); v.Fire || v.Reason != NoFireSplashClose {
			t.Errorf("%s led to %.0f at a target at 140: %+v", name, dist3(in.Eye, in.Aim), v)
		}
		in.Aim, in.TargetDist = Vec3{400, 0, 0}, 400
		if v := FireGate(in); !v.Fire {
			t.Errorf("%s with the target distance at 400: %+v", name, v)
		}
		in = aimed(t, name, FireSuppress)
		in.WallClose = true
		if v := FireGate(in); v.Fire || v.Reason != NoFireSplashWall {
			t.Errorf("%s with a wall close: %+v", name, v)
		}
	}
	in := aimed(t, "Super Shotgun", FireAligned)
	in.Aim, in.WallClose = Vec3{80, 0, 0}, true
	if v := FireGate(in); !v.Fire {
		t.Errorf("a shotgun point blank by a wall: %+v", v)
	}
}

// TestAimTolerance: the angle the target covers plus the weapon's
// spread, bounded.
func TestAimTolerance(t *testing.T) {
	rail, ssg := mustWeapon(t, "Railgun"), mustWeapon(t, "Super Shotgun")
	want := float32(math.Atan2(16, 400) * 180 / math.Pi)
	if got := AimTolerance(rail, 16, 400); math.Abs(float64(got-want)) > 1e-4 {
		t.Errorf("railgun at 400: %v, want %v", got, want)
	}
	if got := AimTolerance(ssg, 16, 400); math.Abs(float64(got-want-ssg.Spread)) > 1e-4 {
		t.Errorf("super shotgun at 400: %v, want %v", got, want+ssg.Spread)
	}
	if got := AimTolerance(rail, 16, 100000); got != MinAimTol {
		t.Errorf("far away: %v, want the floor %v", got, float32(MinAimTol))
	}
	if got := AimTolerance(rail, 16, 0); got != MaxAimTol {
		t.Errorf("point blank: %v, want the cap %v", got, float32(MaxAimTol))
	}
}

func TestWeaponTable(t *testing.T) {
	for _, name := range []string{"Blaster", "Shotgun", "Super Shotgun", "Machinegun", "Chaingun", "Grenades", "Grenade Launcher",
		"Rocket Launcher", "HyperBlaster", "Railgun", "BFG10K"} {
		w := mustWeapon(t, name)
		if w.Cycle <= 0 {
			t.Errorf("%s: cycle %d", name, w.Cycle)
		}
		if w.HasSplash() && w.Hitscan() {
			t.Errorf("%s: hitscan with splash", name)
		}
	}
	if _, ok := WeaponByPickup("Bananas"); ok {
		t.Error("unknown weapon found")
	}
	if mustWeapon(t, "Railgun").Speed != 0 || mustWeapon(t, "Rocket Launcher").Speed != 650 {
		t.Error("speeds")
	}
}

// TestCommitted: no switch in the middle of a rocket or rail cycle; the
// continuous and quick weapons never hold one.
func TestCommitted(t *testing.T) {
	rl, rail, mg, sg := mustWeapon(t, "Rocket Launcher"), mustWeapon(t, "Railgun"), mustWeapon(t, "Machinegun"), mustWeapon(t, "Shotgun")
	if !rl.Committed(1500, 1000) || rl.Committed(1000+int64(rl.Cycle), 1000) {
		t.Error("rocket launcher cycle")
	}
	if !rail.Committed(2400, 1000) || rail.Committed(2500, 1000) {
		t.Error("railgun cycle")
	}
	if mg.Committed(1050, 1000) || sg.Committed(1100, 1000) {
		t.Error("a machinegun or shotgun holds the switch")
	}
	if rl.Committed(1000, 0) {
		t.Error("never fired, yet committed")
	}
}

// TestDodgeFor: only close, imminent projectiles make the bot sidestep,
// the most urgent first; splash widens the margin.
func TestDodgeFor(t *testing.T) {
	left, right := Vec3{0, 1, 0}, Vec3{0, -1, 0}
	ps := []Incoming{
		{TCA: 0.3, Miss: 40, Dir: left},              // too wide for a bolt
		{TCA: 0.5, Miss: 5, Dir: left},               // too far off
		{TCA: -0.1, Miss: 5, Dir: left},              // moving away
		{TCA: 0.2, Miss: 30, Dir: right},             // dodge
		{TCA: 0.1, Miss: 80, Dir: left, Splash: 120}, // a rocket 80 off: dodge, sooner
	}
	dir, tca, ok := DodgeFor(ps[:3])
	if ok {
		t.Fatalf("dodged %v (tca %v) with nothing close", dir, tca)
	}
	if dir, tca, ok = DodgeFor(ps[:4]); !ok || dir != right || tca != 0.2 {
		t.Fatalf("bolt: %v %v %v", dir, tca, ok)
	}
	if dir, _, ok = DodgeFor(ps); !ok || dir != left {
		t.Fatalf("rocket: %v %v", dir, ok)
	}
}

func TestGrenadeEscape(t *testing.T) {
	o := Vec3{0, 0, 0}
	if _, _, ok := GrenadeEscape(o, []Vec3{{300, 0, 0}}, true); ok {
		t.Error("ran from a far grenade")
	}
	dir, jump, ok := GrenadeEscape(o, []Vec3{{300, 0, 0}, {0, 100, 0}}, true)
	if !ok || !jump || math.Abs(float64(dir[1]+1)) > 1e-6 {
		t.Errorf("near grenade: dir %v jump %v ok %v", dir, jump, ok)
	}
	if _, jump, _ := GrenadeEscape(o, []Vec3{{0, 100, 0}}, false); jump {
		t.Error("jumped in the air")
	}
	if dir, jump, ok := GrenadeEscape(o, []Vec3{{150, 0, 0}}, true); !ok || jump || dir[0] >= 0 {
		t.Errorf("grenade at 150: %v %v %v", dir, jump, ok)
	}
}

func TestMoveDir(t *testing.T) {
	self, target := Vec3{0, 0, 0}, Vec3{100, 0, 50}
	cases := map[Move]Vec3{MoveAdvance: {1, 0, 0}, MoveRetreat: {-1, 0, 0}, MoveStrafeLeft: {0, 1, 0}, MoveStrafeRight: {0, -1, 0}, MoveHold: {}}
	for m, want := range cases {
		got := MoveDir(m, self, target)
		for k := 0; k < 3; k++ {
			if math.Abs(float64(got[k]-want[k])) > 1e-6 {
				t.Errorf("%s: %v, want %v", m, got, want)
				break
			}
		}
	}
	if d := MoveDir(MoveAdvance, self, Vec3{0, 0, 80}); d != (Vec3{}) {
		t.Errorf("target overhead: %v", d)
	}
	if SideDir(1, self, target) != MoveDir(MoveStrafeRight, self, target) || SideDir(0, self, target) != (Vec3{}) {
		t.Error("SideDir")
	}
	if MoveStrafeLeft.Side() != -1 || MoveStrafeRight.Side() != 1 || MoveAdvance.Side() != 0 {
		t.Error("Side")
	}
}

// TestStrafer: a side is kept StrafeHold, flips at once when blocked, and
// gives up when both sides are.
func TestStrafer(t *testing.T) {
	var s Strafer
	all := func(int) bool { return true }
	if got := s.Side(0, -1, all); got != -1 {
		t.Fatalf("start: %d", got)
	}
	if got := s.Side(StrafeHold-25, 1, all); got != -1 {
		t.Fatalf("flipped after %d ms: %d", StrafeHold-25, got)
	}
	if got := s.Side(StrafeHold, 1, all); got != 1 {
		t.Fatalf("not flipped after the hold: %d", got)
	}
	// right blocked: flip to the left at once, though just switched
	leftOnly := func(side int) bool { return side < 0 }
	if got := s.Side(StrafeHold+25, 1, leftOnly); got != -1 {
		t.Fatalf("blocked side kept: %d", got)
	}
	if got := s.Side(StrafeHold+50, 1, func(int) bool { return false }); got != 0 {
		t.Fatalf("both blocked: %d", got)
	}
	if got := s.Side(StrafeHold+75, 0, all); got != 0 || s.Current() != 0 {
		t.Fatalf("no strafe wanted: %d (held %d)", got, s.Current())
	}
}

func TestAimPoint(t *testing.T) {
	eye := Vec3{0, 0, 0}
	b := Body{Origin: Vec3{500, 0, 0}, Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}, Vel: Vec3{0, 300, 0}, OnGround: true}
	if p, feet := AimPoint(eye, b, mustWeapon(t, "Railgun")); p != b.Center() || feet {
		t.Errorf("hitscan aims at %v (feet %v), want the centre %v", p, feet, b.Center())
	}
	p, feet := AimPoint(eye, b, mustWeapon(t, "Blaster"))
	if feet || p[1] < 100 || p[2] != b.Center()[2] {
		t.Errorf("blaster lead: %v %v", p, feet)
	}
	p, feet = AimPoint(eye, b, mustWeapon(t, "Rocket Launcher"))
	if !feet || p[2] != -16 || p[1] < 150 {
		t.Errorf("rocket at the feet, led: %v %v", p, feet)
	}
	b.OnGround = false
	if _, feet := AimPoint(eye, b, mustWeapon(t, "Rocket Launcher")); feet {
		t.Error("rocket at the feet of a flyer")
	}
	if r := b.Radius(); r != 16 {
		t.Errorf("radius %v", r)
	}
}
