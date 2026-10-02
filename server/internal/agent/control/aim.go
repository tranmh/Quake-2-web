package control

import (
	"math"

	"quake2web/server/internal/qcommon/shared"
)

// Aim slew defaults.
const (
	// DefaultSlewRate caps the turn rate (degrees per second): a quick
	// but human flick.
	DefaultSlewRate = 720
	// DefaultSlewTau is the time constant (seconds) of the exponential
	// approach: each command closes 1-exp(-dt/tau) of the error.
	DefaultSlewTau = 0.06
)

// Slew turns a view towards a wanted one the way a hand on a mouse does:
// each command closes a fraction of the remaining error (an exponential
// approach with time constant Tau), never faster than Rate. The zero value
// uses the defaults.
type Slew struct {
	Rate float32 // degrees per second (0: DefaultSlewRate)
	Tau  float32 // seconds (0: DefaultSlewTau)
}

// Step returns the view after msec of turning from yaw/pitch towards
// wantYaw/wantPitch (degrees; pitch > 0 looks down, clamped to
// ±MaxPitch). Yaw turns the short way round; the result is in [0, 360).
func (s Slew) Step(yaw, pitch, wantYaw, wantPitch float32, msec int) (float32, float32) {
	rate, tau := s.Rate, s.Tau
	if rate <= 0 {
		rate = DefaultSlewRate
	}
	if tau <= 0 {
		tau = DefaultSlewTau
	}
	dt := float64(max(msec, 0)) / 1000
	frac := 1 - math.Exp(-dt/float64(tau))
	maxStep := float64(rate) * dt
	dy := float64(AngleDelta(wantYaw, yaw))
	dp := float64(clampPitch(wantPitch) - clampPitch(pitch))
	sy, sp := dy*frac, dp*frac
	// the rate cap applies to the turn as a whole, keeping its direction
	if m := math.Hypot(sy, sp); m > maxStep && m > 0 {
		sy, sp = sy*maxStep/m, sp*maxStep/m
	}
	// close enough: snap (the exponential never quite gets there)
	if math.Abs(dy-sy) < 0.05 && math.Abs(dp-sp) < 0.05 {
		sy, sp = dy, dp
	}
	return normYaw(float64(yaw) + sy), clampPitch(float32(float64(clampPitch(pitch)) + sp))
}

func clampPitch(p float32) float32 { return min(max(p, -MaxPitch), MaxPitch) }

func normYaw(y float64) float32 {
	y = math.Mod(y, 360)
	if y < 0 {
		y += 360
	}
	return float32(y)
}

// LookAt returns the yaw and pitch (degrees, pitch > 0 down) that look
// from eye at p, and ok=false when p is (nearly) at the eye.
func LookAt(eye, p shared.Vec3) (yaw, pitch float32, ok bool) {
	dx, dy, dz := float64(p[0]-eye[0]), float64(p[1]-eye[1]), float64(p[2]-eye[2])
	h := math.Hypot(dx, dy)
	if h < 0.5 && math.Abs(dz) < 0.5 {
		return 0, 0, false
	}
	yaw = normYaw(math.Atan2(dy, dx) * 180 / math.Pi)
	pitch = float32(-math.Atan2(dz, h) * 180 / math.Pi)
	return yaw, pitch, true
}

// AimError returns the angle (degrees) between the view yaw/pitch and the
// direction from eye to p.
func AimError(yaw, pitch float32, eye, p shared.Vec3) float32 {
	var fwd shared.Vec3
	shared.AngleVectors(shared.Vec3{pitch, yaw, 0}, &fwd, nil, nil)
	d := [3]float64{float64(p[0] - eye[0]), float64(p[1] - eye[1]), float64(p[2] - eye[2])}
	l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
	if l < 1e-6 {
		return 0
	}
	c := (float64(fwd[0])*d[0] + float64(fwd[1])*d[1] + float64(fwd[2])*d[2]) / l
	return float32(math.Acos(math.Max(-1, math.Min(1, c))) * 180 / math.Pi)
}

// Lead returns where to aim from eye with a projectile of speed (units
// per second) to meet a target at p moving with velocity v: the point the
// target reaches when the projectile gets there (two fixed-point
// iterations of the flight time, at most 2 s ahead). A speed <= 0
// (hitscan) aims at p.
func Lead(eye, p, v shared.Vec3, speed float32) shared.Vec3 {
	if speed <= 0 {
		return p
	}
	at := p
	for k := 0; k < 2; k++ {
		d := math.Sqrt(float64((at[0]-eye[0])*(at[0]-eye[0]) + (at[1]-eye[1])*(at[1]-eye[1]) + (at[2]-eye[2])*(at[2]-eye[2])))
		t := float32(math.Min(d/float64(speed), 2))
		at = shared.Vec3{p[0] + v[0]*t, p[1] + v[1]*t, p[2] + v[2]*t}
	}
	return at
}
