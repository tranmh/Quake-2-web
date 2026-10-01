package control_test

import (
	"math"
	"math/rand"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/pmove"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// serverView runs the real PM_ClampAngles (pmove with PM_FREEZE returns
// right after computing the view angles).
func serverView(cmd, delta [3]int16) shared.Vec3 {
	pm := &pmove.PmoveT{}
	pm.S.PmType = q2const.PM_FREEZE
	pm.S.DeltaAngles = delta
	pm.Cmd.Angles = cmd
	pm.Cmd.Msec = 25
	pmove.Pmove(pm, 0)
	return pm.ViewAngles
}

const shortStep = 360.0 / 65536

func TestCmdAnglesRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		yaw := float32(rng.Float64()*1440 - 720)
		pitch := float32(rng.Float64()*240 - 120)
		var delta [3]int16
		for k := range delta {
			delta[k] = int16(rng.Intn(65536) - 32768)
		}
		cmd := control.CmdAngles(yaw, pitch, delta)
		gy, gp := control.ViewAngles(cmd, delta)
		wantP := pitch
		if wantP > control.MaxPitch {
			wantP = control.MaxPitch
		} else if wantP < -control.MaxPitch {
			wantP = -control.MaxPitch
		}
		if d := math.Abs(float64(control.AngleDelta(gy, yaw))); d > 2*shortStep {
			t.Fatalf("yaw %v delta %v: got %v (off %v)", yaw, delta, gy, d)
		}
		if d := math.Abs(float64(gp - wantP)); d > 2*shortStep {
			t.Fatalf("pitch %v delta %v: got %v want %v", pitch, delta, gp, wantP)
		}
		if gy < 0 || gy >= 360 {
			t.Fatalf("yaw %v not normalized", gy)
		}
		// the server sees exactly what ViewAngles says
		v := serverView(cmd, delta)
		if control.AngleDelta(v[q2const.YAW], gy) != 0 || v[q2const.PITCH] != gp || v[q2const.ROLL] != 0 {
			t.Fatalf("server view %v, ViewAngles %v %v (cmd %v delta %v)", v, gy, gp, cmd, delta)
		}
	}
}

func TestCmdAnglesWrap(t *testing.T) {
	// a delta that pushes the sum across the short range must wrap, not
	// saturate
	delta := [3]int16{-32768, 32767, 12345}
	cmd := control.CmdAngles(180, 0, delta)
	if want := int16(int32(shared.ANGLE2SHORT(180)) - 32767); cmd[q2const.YAW] != want {
		t.Errorf("yaw short %d, want %d", cmd[q2const.YAW], want)
	}
	if cmd[q2const.ROLL] != -12345 {
		t.Errorf("roll short %d", cmd[q2const.ROLL])
	}
	y, _ := control.ViewAngles(cmd, delta)
	if y != 180 {
		t.Errorf("yaw %v", y)
	}
	// zero delta is the plain ANGLE2SHORT
	if c := control.CmdAngles(90, -30, [3]int16{}); c[q2const.YAW] != 16384 || c[q2const.PITCH] != int16(shared.ANGLE2SHORT(-30)) {
		t.Errorf("zero delta %v", c)
	}
}

func TestAngleDelta(t *testing.T) {
	for _, c := range []struct{ a, b, want float32 }{
		{10, 350, 20}, {350, 10, -20}, {180, 0, 180}, {0, 180, 180}, {-90, 90, 180}, {720, 0, 0}, {45, 45, 0},
	} {
		if got := control.AngleDelta(c.a, c.b); got != c.want {
			t.Errorf("AngleDelta(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestNavsimCmdMatches pins navsim's own conversion (navsim cannot import
// control) to CmdAngles, so an edge recipe executed online through
// CmdAngles sends exactly the angles the offline simulation used.
func TestNavsimCmdMatches(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 5000; i++ {
		c := navsim.Cmd{Yaw: float32(rng.Float64()*720 - 360), Pitch: float32(rng.Float64()*200 - 100), Forward: 400, Msec: 25}
		var delta [3]int16
		for k := range delta {
			delta[k] = int16(rng.Intn(65536) - 32768)
		}
		if got, want := c.UserCmd(delta).Angles, control.CmdAngles(c.Yaw, c.Pitch, delta); got != want {
			t.Fatalf("%+v delta %v: navsim %v, control %v", c, delta, got, want)
		}
	}
}
