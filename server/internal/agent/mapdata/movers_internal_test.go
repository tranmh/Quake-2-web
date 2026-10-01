package mapdata

import (
	"math"
	"testing"
)

func TestMoveFramesConstant(t *testing.T) {
	for _, tc := range []struct {
		dist, speed float32
		want        int
	}{
		{100, 100, 11}, // 1 start frame + 10 full frames, no remainder
		{105, 100, 12}, // ... + a Move_Final frame for the last 5 units
		{5, 100, 2},    // shorter than one frame: Move_Final straight away
		{0, 100, 1},    // nothing to do: Move_Done on the first think
		{158, 100, 17}, // demo1's elevator car
	} {
		if got := moveFrames(tc.dist, tc.speed, tc.speed, tc.speed); got != tc.want {
			t.Errorf("moveFrames(%g at %g) = %d, want %d", tc.dist, tc.speed, got, tc.want)
		}
	}
	if got := moveFrames(10, 0, 0, 0); got != -1 {
		t.Errorf("zero speed: %d, want -1", got)
	}
}

// TestMoveFramesAccelerated checks the plat acceleration against simple
// kinematics: ramping up and down at accel units/frame^2 to the cruise
// speed, cruising in between.
func TestMoveFramesAccelerated(t *testing.T) {
	for _, tc := range []struct{ dist, speed, accel float32 }{
		{190, 20, 5}, {154, 20, 5}, {600, 30, 2}, {30, 20, 5},
	} {
		got := moveFrames(tc.dist, tc.speed, tc.accel, tc.accel)
		ramp := tc.speed / tc.accel                   // frames to reach cruise speed
		rampDist := tc.speed * (ramp + 1) / 2         // AccelerationDistance
		est := 2*ramp + (tc.dist-2*rampDist)/tc.speed // up + cruise + down
		if tc.dist < 2*rampDist {                     // never reaches cruise speed
			est = 2 * float32(math.Sqrt(float64(tc.dist/tc.accel)))
		}
		if d := float32(got) - (est + 1); d < -2 || d > 2 {
			t.Errorf("moveFrames(%g, %g, %g) = %d, kinematics say about %.1f", tc.dist, tc.speed, tc.accel, got, est+1)
		}
	}
}

func TestAngleMoveFrames(t *testing.T) {
	if got := angleMoveFrames(90, 35); got != 27 { // 2.57 s: 1 + floor(25.7) + 1
		t.Errorf("angleMoveFrames(90, 35) = %d, want 27", got)
	}
	if got := angleMoveFrames(2, 35); got != 2 {
		t.Errorf("angleMoveFrames(2, 35) = %d, want 2", got)
	}
}
