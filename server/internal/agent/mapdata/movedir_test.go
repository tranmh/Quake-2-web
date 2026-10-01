package mapdata_test

import (
	"math/rand"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/game"
	"quake2web/server/internal/testutil"
)

// TestSetMovedirOracle checks the mapdata copy of G_SetMovedir against the
// ported game function: the same movedir bit for bit, and the angles
// argument cleared like the original does.
func TestSetMovedirOracle(t *testing.T) {
	angles := []mapdata.Vec3{
		{0, -1, 0}, {0, -2, 0}, // VEC_UP, VEC_DOWN
		{0, 0, 0}, {0, 90, 0}, {0, 180, 0}, {0, 270, 0}, {0, 360, 0}, {0, 45, 0},
		{0, -90, 0}, {0, -3, 0}, {-90, 0, 0}, {90, 0, 0}, {30, 135, 0}, {0, 0, 45},
		{1, -1, 0}, {0, -1, 1}, {-0.0001, 0, 0}, {12.5, 77.25, -3},
	}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		var a mapdata.Vec3
		for k := range a {
			// whole degrees (editor values) and arbitrary floats
			if i%2 == 0 {
				a[k] = float32(rng.Intn(721) - 360)
			} else {
				a[k] = float32(rng.Float64()*1440 - 720)
			}
		}
		if i%3 == 0 {
			a[0], a[2] = 0, 0 // the "angle" form
		}
		angles = append(angles, a)
	}
	for _, a := range angles {
		gotAngles, wantAngles := a, a
		var got, want mapdata.Vec3
		mapdata.G_SetMovedir(&gotAngles, &got)
		game.G_SetMovedir(&wantAngles, &want)
		if !testutil.SameVec3(got, want) {
			t.Fatalf("G_SetMovedir(%v) = %s, game %s", a, testutil.FmtVec3(got), testutil.FmtVec3(want))
		}
		if !testutil.SameVec3(gotAngles, wantAngles) || gotAngles != (mapdata.Vec3{}) {
			t.Fatalf("G_SetMovedir(%v) left angles %v, game %v", a, gotAngles, wantAngles)
		}
		if md := mapdata.Movedir(a); !testutil.SameVec3(md, want) {
			t.Fatalf("Movedir(%v) = %v, want %v", a, md, want)
		}
	}
}

// TestSetMovedirSpecialAngles pins the editor conventions the route tables
// rely on: -1 is straight up, -2 straight down, 360 (demo3's exit) +x.
func TestSetMovedirSpecialAngles(t *testing.T) {
	for _, tc := range []struct {
		yaw  float32
		want mapdata.Vec3
	}{
		{-1, mapdata.Vec3{0, 0, 1}},
		{-2, mapdata.Vec3{0, 0, -1}},
	} {
		if got := mapdata.Movedir(mapdata.Vec3{0, tc.yaw, 0}); got != tc.want {
			t.Errorf("angle %g: movedir %v, want %v", tc.yaw, got, tc.want)
		}
	}
	if md := mapdata.Movedir(mapdata.Vec3{0, 360, 0}); md[0] != 1 || abs(md[1]) > 1e-6 || md[2] != 0 {
		t.Errorf("angle 360: movedir %v, want +x", md)
	}
}

func abs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
