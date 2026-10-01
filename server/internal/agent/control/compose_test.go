package control_test

import (
	"math"
	"math/rand"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

func dirOf(yaw float64) shared.Vec3 {
	s, c := math.Sincos(yaw * math.Pi / 180)
	return shared.Vec3{float32(c), float32(s), 0}
}

// TestComposeQuadrants: the wish direction is projected onto the aim yaw,
// so moving ahead, left, back and right of the view gives the four pure
// moves (sidemove > 0 is right in Quake II).
func TestComposeQuadrants(t *testing.T) {
	for _, aim := range []float64{0, 37, 90, 181, 270, 359.5} {
		for _, c := range []struct {
			rel         float64
			fwd, side   int16
			description string
		}{
			{0, 400, 0, "ahead"},
			{90, 0, -400, "left"},
			{180, -400, 0, "behind"},
			{-90, 0, 400, "right"},
			{45, 283, -283, "ahead left"},
			{-135, -283, 283, "behind right"},
		} {
			in := control.MoveIntent{WishDir: dirOf(aim + c.rel), Speed: 400, FaceYaw: 12}
			u := control.Compose(in, float32(aim), 0, [3]int16{}, 25)
			if d := absDiff(u.ForwardMove, c.fwd) + absDiff(u.SideMove, c.side); d > 1 {
				t.Errorf("aim %v, wish %s: forward %d side %d, want %d %d", aim, c.description, u.ForwardMove, u.SideMove, c.fwd, c.side)
			}
			if y, _ := control.ViewAngles(u.Angles, [3]int16{}); math.Abs(float64(control.AngleDelta(y, float32(aim)))) > 0.01 {
				t.Errorf("aim %v: view yaw %v (MustFace is off, the aim decides)", aim, y)
			}
		}
	}
}

func absDiff(a, b int16) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func TestComposeMustFace(t *testing.T) {
	delta := [3]int16{100, -2000, 7}
	in := control.MoveIntent{WishDir: dirOf(90), Speed: 400, FaceYaw: 90, FacePitch: -20, MustFace: true}
	u := control.Compose(in, 250, 40, delta, 25)
	if u.Angles != control.CmdAngles(90, -20, delta) {
		t.Errorf("MustFace: angles %v, want the face angles %v", u.Angles, control.CmdAngles(90, -20, delta))
	}
	if u.ForwardMove != 400 || u.SideMove != 0 {
		t.Errorf("MustFace along the wish: forward %d side %d", u.ForwardMove, u.SideMove)
	}
	in.MustFace = false
	if u := control.Compose(in, 250, 40, delta, 25); u.Angles != control.CmdAngles(250, 40, delta) {
		t.Errorf("without MustFace the aim decides: %v", u.Angles)
	}
}

func TestComposeButtonsUpAndMsec(t *testing.T) {
	for _, c := range []struct {
		in   control.MoveIntent
		up   int16
		btn  uint8
		msec int
		want uint8
	}{
		{control.MoveIntent{Jump: true}, 400, 0, 25, 25},
		{control.MoveIntent{SwimUp: true}, 400, 0, 25, 25},
		{control.MoveIntent{Crouch: true}, -400, 0, 25, 25},
		{control.MoveIntent{Fire: true}, 0, q2const.BUTTON_ATTACK, 300, control.MaxCmdMsec},
		{control.MoveIntent{}, 0, 0, -5, 0},
	} {
		u := control.Compose(c.in, 0, 0, [3]int16{}, c.msec)
		if u.UpMove != c.up || u.Buttons != c.btn || u.Msec != c.want || u.ForwardMove != 0 || u.SideMove != 0 {
			t.Errorf("%+v: %+v", c.in, u)
		}
	}
}

// TestFromCmdRoundTrip: Compose of an executor command's intent, aimed
// where the command looks, is exactly the command the executor would have
// sent (so the follower runs the recipes the builder validated).
func TestFromCmdRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	ups := []int16{-400, 0, 400}
	for i := 0; i < 20000; i++ {
		swim := i%3 == 0
		c := navsim.Cmd{Msec: 25, Forward: int16(rng.Intn(801) - 400), Side: int16(rng.Intn(801) - 400), Up: ups[rng.Intn(3)],
			Yaw: float32(rng.Float64() * 360)}
		if i%2 == 0 {
			c.Pitch = float32(rng.Float64()*160 - 80)
		}
		if rng.Intn(4) == 0 {
			c.Buttons = q2const.BUTTON_ATTACK
		}
		if swim && c.Up != 0 && rng.Intn(2) == 0 {
			c.Forward, c.Side = 0, 0
		}
		var delta [3]int16
		for k := range delta {
			delta[k] = int16(rng.Intn(65536) - 32768)
		}
		in := control.FromCmd(c, swim)
		got := control.Compose(in, c.Yaw, c.Pitch, delta, 25)
		if want := c.UserCmd(delta); got != want {
			t.Fatalf("cmd %+v swim %v: composed %+v, the executor sends %+v (intent %+v)", c, swim, got, want, in)
		}
		// the same with MustFace and an unrelated aim
		in.MustFace = true
		if got := control.Compose(in, c.Yaw+77, -c.Pitch, delta, 25); got != c.UserCmd(delta) {
			t.Fatalf("MustFace cmd %+v: %+v", c, got)
		}
	}
}

func floor(t testing.TB) *cmodel.Map {
	t.Helper()
	m, err := cmodel.LoadMapBytes("synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestComposeMovesAlongWish runs the real pmove: whatever the aim (yaw
// and a moderate pitch, which pmove's pitched forward vector would shorten
// without the compensation), one command from rest accelerates the player
// along the wish direction, at the full ground acceleration.
func TestComposeMovesAlongWish(t *testing.T) {
	w := navsim.NewWorld(floor(t))
	r := navsim.NewRunner(w, navsim.DefaultPhysics())
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 300; i++ {
		if !r.Settle(navsim.Vec3{0, 0, 30}, false, 2000) {
			t.Fatal("no rest on the slab")
		}
		st := r.State()
		wish := rng.Float64() * 360
		aimYaw := float32(rng.Float64() * 360)
		aimPitch := float32(rng.Float64()*178 - 89)
		u := control.Compose(control.MoveIntent{WishDir: dirOf(wish), Speed: 400}, aimYaw, aimPitch, st.PM.DeltaAngles, 25)
		vy, vp := control.ViewAngles(u.Angles, st.PM.DeltaAngles)
		r.Step(navsim.Cmd{Msec: 25, Forward: u.ForwardMove, Side: u.SideMove, Up: u.UpMove, Yaw: vy, Pitch: vp})
		v := r.State().Velocity()
		got := math.Atan2(float64(v[1]), float64(v[0])) * 180 / math.Pi
		if d := math.Abs(float64(control.AngleDelta(float32(got), float32(wish)))); d > 1.5 {
			t.Fatalf("wish %.1f aim %.1f/%.1f: moved at %.1f (cmd %+v)", wish, aimYaw, aimPitch, got, u)
		}
		// pm_accelerate 10 * 300 * 0.025 = 75 units/s after one command
		// (velocities are 1/8 unit snapped)
		if sp := math.Hypot(float64(v[0]), float64(v[1])); sp < 73 || sp > 76 {
			t.Fatalf("wish %.1f aim %.1f/%.1f: speed %.2f after one command (cmd %+v)", wish, aimYaw, aimPitch, sp, u)
		}
	}
}

func TestMsecBudget(t *testing.T) {
	var b control.MsecBudget
	// on schedule: every command gets its time
	for i := 0; i < 40; i++ {
		if got := b.Allow(int64(i*25), 25); got != 25 {
			t.Fatalf("command %d: %d ms", i, got)
		}
	}
	// a burst ahead of the clock is cut at the slack (200 ms)
	now := int64(40 * 25)
	total := 0
	for i := 0; i < 20; i++ {
		total += b.Allow(now, 25)
	}
	if total != 200 { // the clock and the time sent are even: only the slack is left
		t.Errorf("burst got %d ms", total)
	}
	if got := b.Allow(now, 25); got != 0 {
		t.Errorf("over budget: %d", got)
	}
	if got := b.Allow(now+50, 300); got != 50 {
		t.Errorf("after 50 ms: %d", got)
	}
	b.Reset()
	if got := b.Allow(5000, 25); got != 25 {
		t.Errorf("after reset: %d", got)
	}
}
