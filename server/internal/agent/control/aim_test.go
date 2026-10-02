package control

import (
	"math"
	"testing"

	"quake2web/server/internal/qcommon/shared"
)

func TestSlewConvergesWithinRate(t *testing.T) {
	s := Slew{Rate: 400, Tau: 0.05}
	yaw, pitch := float32(350), float32(0)
	const want, wantP = 80, -20 // 90 degrees the short way (through 0)
	prev := yaw
	for k := 0; k < 80; k++ {
		yaw, pitch = s.Step(yaw, pitch, want, wantP, 25)
		step := math.Abs(float64(AngleDelta(yaw, prev)))
		if step > 400*0.025+1e-3 {
			t.Fatalf("command %d turned %.2f degrees, cap is %.2f", k, step, 400*0.025)
		}
		prev = yaw
	}
	if math.Abs(float64(AngleDelta(yaw, want))) > 0.01 || math.Abs(float64(pitch-wantP)) > 0.01 {
		t.Fatalf("after 2 s at %.2f/%.2f, want %d/%d", yaw, pitch, want, wantP)
	}
	if yaw < 0 || yaw >= 360 {
		t.Fatalf("yaw %v out of [0, 360)", yaw)
	}
}

func TestSlewTurnsShortWayAndClampsPitch(t *testing.T) {
	var s Slew
	y, p := s.Step(10, 0, 340, 120, 25)
	if d := AngleDelta(y, 10); d >= 0 {
		t.Fatalf("turned %+.2f from 10 towards 340: want negative (the short way)", d)
	}
	for k := 0; k < 200; k++ {
		y, p = s.Step(y, p, 340, 120, 25)
	}
	if p != MaxPitch {
		t.Fatalf("pitch %v, want clamped to %v", p, MaxPitch)
	}
	if y2, p2 := s.Step(y, p, y, p, 0); y2 != y || p2 != p {
		t.Fatalf("a zero-length step moved the view")
	}
}

func TestSlewExponentialApproach(t *testing.T) {
	// far from the rate cap the error shrinks by exp(-dt/tau) a command
	s := Slew{Rate: 100000, Tau: 0.1}
	y, _ := s.Step(0, 0, 10, 0, 25)
	want := 10 * (1 - math.Exp(-0.25))
	if math.Abs(float64(y)-want) > 1e-4 {
		t.Fatalf("yaw %.5f after one step, want %.5f", y, want)
	}
}

func TestLookAtAndAimError(t *testing.T) {
	eye := shared.Vec3{0, 0, 0}
	cases := []struct {
		p          shared.Vec3
		yaw, pitch float32
	}{
		{shared.Vec3{100, 0, 0}, 0, 0},
		{shared.Vec3{0, 100, 0}, 90, 0},
		{shared.Vec3{-100, 0, -100}, 180, 45}, // below: pitch > 0
		{shared.Vec3{0, -100, 100}, 270, -45},
	}
	for _, c := range cases {
		y, p, ok := LookAt(eye, c.p)
		if !ok || math.Abs(float64(AngleDelta(y, c.yaw))) > 1e-3 || math.Abs(float64(p-c.pitch)) > 1e-3 {
			t.Errorf("LookAt(%v) = %.3f %.3f %v, want %v %v", c.p, y, p, ok, c.yaw, c.pitch)
		}
		if e := AimError(y, p, eye, c.p); e > 0.05 { // float32 view vectors
			t.Errorf("AimError on target %v = %v", c.p, e)
		}
	}
	if e := AimError(0, 0, eye, shared.Vec3{0, 100, 0}); math.Abs(float64(e)-90) > 1e-3 {
		t.Errorf("AimError 90 degrees off = %v", e)
	}
	if _, _, ok := LookAt(eye, eye); ok {
		t.Error("LookAt at the eye itself must not be ok")
	}
}

func TestLead(t *testing.T) {
	eye := shared.Vec3{}
	p := shared.Vec3{1000, 0, 0}
	v := shared.Vec3{0, 100, 0}
	if got := Lead(eye, p, v, 0); got != p {
		t.Fatalf("hitscan lead moved the aim: %v", got)
	}
	at := Lead(eye, p, v, 1000)
	// about a second of flight: about 100 units ahead along y
	if at[0] != 1000 || at[1] < 95 || at[1] > 105 {
		t.Fatalf("lead %v, want about (1000, 100, 0)", at)
	}
	// a projectile that would take ages is capped at 2 s ahead
	far := Lead(eye, shared.Vec3{1e6, 0, 0}, v, 1)
	if far[1] != 200 {
		t.Fatalf("lead capped at %v, want y=200", far)
	}
}
