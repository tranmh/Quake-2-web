package perception

import (
	"math"
	"strconv"
	"testing"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// floorMap is bsp.SyntheticFloorMap: a 128x128 slab (x, y in [-64, 64], z
// in [-16, 0]) in empty space, one cluster, area 1.
func floorMap(t testing.TB) *cmodel.Map {
	t.Helper()
	cm, err := cmodel.LoadMapBytes("maps/floor.bsp", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

var area1 = []byte{0x02}

func TestVisionFloorLOS(t *testing.T) {
	v := NewVision(floorMap(t), ViewOptions{})
	v.Begin(Vec3{0, 0, 40}, Vec3{}, 90, area1, nil)
	for _, tc := range []struct {
		p   Vec3
		los bool
	}{
		{Vec3{0, 0, -40}, false},   // straight through the slab
		{Vec3{100, 0, -40}, false}, // crosses the slab top at x=50
		{Vec3{200, 0, -40}, true},  // passes beside it
		{Vec3{100, 30, 10}, true},  // above it
		{Vec3{0, 0, 0.5}, true},    // on the surface counts as visible
		{Vec3{30, 0, -8}, false},   // inside the slab
	} {
		if got := v.LOS(tc.p, -1); got != tc.los {
			t.Errorf("LOS to %v = %v, want %v", tc.p, got, tc.los)
		}
	}
	if !v.Shootable(Vec3{200, 0, -40}, -1) || v.Shootable(Vec3{0, 0, -40}, -1) {
		t.Error("shootable lines differ from sight lines on solid")
	}
	if c := v.PointContents(Vec3{0, 0, -8}); c&q2const.CONTENTS_SOLID == 0 {
		t.Errorf("slab contents %#x", c)
	}
	if c := v.PointContents(Vec3{0, 0, 8}); c != 0 {
		t.Errorf("air contents %#x", c)
	}
	// a box partly hidden by the slab is seen by its top
	if !v.SeesBox(Vec3{90, -16, -60}, Vec3{122, 16, 10}, -1) {
		t.Error("box with a visible top not seen")
	}
	if v.SeesBox(Vec3{-16, -16, -80}, Vec3{16, 16, -40}, -1) {
		t.Error("box under the slab seen")
	}
}

func TestVisionFOV(t *testing.T) {
	v := NewVision(nil, ViewOptions{})
	v.Begin(Vec3{0, 0, 0}, Vec3{0, 0, 0}, 90, nil, nil) // looking along +x
	x, y := v.Fov()
	wantY := float32(2 * math.Atan(1/DefaultAspect) * 180 / math.Pi) // CalcFov at 4:3: 73.74
	if x != 90 || math.Abs(float64(y-wantY)) > 1e-3 {
		t.Fatalf("fov %v x %v, want 90 x %v", x, y, wantY)
	}
	for _, tc := range []struct {
		p  Vec3
		in bool
	}{
		{Vec3{100, 0, 0}, true},
		{Vec3{100, 99, 0}, true},
		{Vec3{100, 101, 0}, false},
		{Vec3{100, -99, 0}, true},
		{Vec3{100, 0, 74}, true}, // tan(fov_y/2) = 0.75
		{Vec3{100, 0, 76}, false},
		{Vec3{100, 0, -76}, false},
		{Vec3{-100, 0, 0}, false},
		{Vec3{0, 100, 0}, false},
	} {
		if got := v.InFOV(tc.p); got != tc.in {
			t.Errorf("InFOV(%v) = %v, want %v", tc.p, got, tc.in)
		}
	}
	// the frustum turns with the view: yaw 90 looks along +y
	v.Begin(Vec3{}, Vec3{0, 90, 0}, 90, nil, nil)
	if !v.InFOV(Vec3{0, 100, 0}) || v.InFOV(Vec3{100, 0, 0}) {
		t.Error("yaw 90 frustum")
	}
	// pitch down 45 (positive pitch looks down)
	v.Begin(Vec3{}, Vec3{45, 0, 0}, 90, nil, nil)
	if !v.InFOV(Vec3{100, 0, -100}) || v.InFOV(Vec3{100, 0, 60}) {
		t.Error("pitched frustum")
	}
	// a configured fov overrides the player state's; wide screen narrows y
	v = NewVision(nil, ViewOptions{FovX: 60, Aspect: 16.0 / 9})
	v.Begin(Vec3{}, Vec3{}, 90, nil, nil)
	if x, _ := v.Fov(); x != 60 || v.InFOV(Vec3{100, 60, 0}) || !v.InFOV(Vec3{100, 57, 0}) {
		t.Errorf("fov override: %v", x)
	}
	if v.InFOV(Vec3{100, 0, 33}) || !v.InFOV(Vec3{100, 0, 32}) { // tan(30)/(16/9) = 0.3248
		t.Error("16:9 vertical fov")
	}
	// a bad fov falls back to the default
	v.Begin(Vec3{}, Vec3{}, 0, nil, nil)
	if x, _ := v.Fov(); x != 60 {
		t.Errorf("override lost: %v", x)
	}
	v = NewVision(nil, ViewOptions{})
	v.Begin(Vec3{}, Vec3{}, 500, nil, nil)
	if x, _ := v.Fov(); x != DefaultFov {
		t.Errorf("bad fov: %v", x)
	}
}

func TestVisionPVSAreas(t *testing.T) {
	v := NewVision(floorMap(t), ViewOptions{})
	v.Begin(Vec3{0, 0, 40}, Vec3{}, 90, area1, nil)
	if !v.InPVS(Vec3{100, 0, 0}, Vec3{132, 32, 56}) || !v.PointInPVS(Vec3{0, 0, -60}) {
		t.Error("the only cluster is not in the PVS")
	}
	if !v.InPHS(Vec3{0, 0, 60}) {
		t.Error("the only cluster is not in the PHS")
	}
	// area 1 not connected: nothing is sent
	v.Begin(Vec3{0, 0, 40}, Vec3{}, 90, []byte{0}, nil)
	if v.InPVS(Vec3{100, 0, 0}, Vec3{132, 32, 56}) || v.InPHS(Vec3{0, 0, 60}) {
		t.Error("closed area still in PVS")
	}
}

// TestVisionDemo1 checks lines of sight on the real demo1 map from the
// player start: points short of the first wall along each direction are
// visible, points behind it are not, and a far away room is outside the PVS.
func TestVisionDemo1(t *testing.T) {
	fs := demoFS(t)
	raw, err := fs.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	cm, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	st := cmodel.NewState(cm)
	eye := Vec3{128, -320, 64} // info_player_start + view height
	leaf := st.PointLeafnum(eye)
	var bits [q2const.MAX_MAP_AREAS / 8]byte
	n := st.WriteAreaBits(bits[:], int(cm.LeafArea(int(leaf))))
	v := NewVision(cm, ViewOptions{})
	walls := 0
	for yaw := float32(0); yaw < 360; yaw += 45 {
		v.Begin(eye, Vec3{0, yaw, 0}, 90, bits[:n], nil)
		var fwd Vec3
		shared.AngleVectors(Vec3{0, yaw, 0}, &fwd, nil, nil)
		far := shared.VectorMA(eye, 4096, fwd)
		tr := st.BoxTrace(eye, far, Vec3{}, Vec3{}, 0, q2const.MASK_OPAQUE)
		d := tr.Fraction * 4096
		if d < 48 || tr.Fraction == 1 {
			continue
		}
		near := shared.VectorMA(eye, d-16, fwd)
		behind := shared.VectorMA(eye, d+64, fwd)
		if !v.SeesPoint(near) {
			t.Errorf("yaw %v: point %v short of the wall at %.0f not seen", yaw, near, d)
		}
		if v.LOS(behind, -1) {
			t.Errorf("yaw %v: point %v behind the wall at %.0f has a line of sight", yaw, behind, d)
		}
		walls++
	}
	if walls < 4 {
		t.Fatalf("only %d walls found around the start", walls)
	}
	v.Begin(eye, Vec3{}, 90, bits[:n], nil)
	if !v.PointInPVS(eye) {
		t.Error("the eye is not in its own PVS")
	}
	if v.PointInPVS(Vec3{-1960, 1469, 120}) { // the far infantry room
		t.Error("a room across the map is in the start's PVS")
	}
}

// TestVisionBrushOccludes: a demo1 door (an inline model) blocks the line
// of sight through its doorway at its closed pose and not when it has moved
// away; the world alone does not block that line.
func TestVisionBrushOccludes(t *testing.T) {
	fs := demoFS(t)
	raw, err := fs.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	cm, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	st := cmodel.NewState(cm)
	v := NewVision(cm, ViewOptions{})
	tested := 0
	for n := 1; n < cm.NumInlineModels() && tested < 3; n++ {
		mod := cm.InlineModel("*" + itoaTest(n))
		size := shared.VectorSubtract(mod.Maxs, mod.Mins)
		axis := 0
		if size[1] < size[0] {
			axis = 1
		}
		if size[axis] > 32 || size[1-axis] < 48 || size[2] < 48 {
			continue // not door shaped
		}
		c := Vec3{(mod.Mins[0] + mod.Maxs[0]) / 2, (mod.Mins[1] + mod.Maxs[1]) / 2, (mod.Mins[2] + mod.Maxs[2]) / 2}
		a, b := c, c
		a[axis] -= size[axis]/2 + 24
		b[axis] += size[axis]/2 + 24
		if st.PointContents(a, 0)&q2const.MASK_OPAQUE != 0 || st.PointContents(b, 0)&q2const.MASK_OPAQUE != 0 {
			continue
		}
		if tr := st.BoxTrace(a, b, Vec3{}, Vec3{}, 0, q2const.MASK_OPAQUE); tr.Fraction < 1 {
			continue // the world blocks too: not a doorway
		}
		if c := st.TransformedPointContents(c, mod.Headnode, Vec3{}, Vec3{}); c&q2const.MASK_OPAQUE == 0 {
			continue // not an opaque brush
		}
		v.Begin(a, Vec3{}, 90, nil, []BrushPose{{Num: 50, Inline: n}})
		if v.LOSBetween(a, b, -1) {
			t.Errorf("*%d at its spawn pose does not block %v -> %v", n, a, b)
		}
		if !v.LOSBetween(a, b, 50) {
			t.Errorf("*%d: ignoring the door must clear the line", n)
		}
		v.Begin(a, Vec3{}, 90, nil, []BrushPose{{Num: 50, Inline: n, Origin: Vec3{0, 0, size[2] + 64}}})
		if !v.LOSBetween(a, b, -1) {
			t.Errorf("*%d moved away still blocks", n)
		}
		tested++
	}
	if tested == 0 {
		t.Fatal("no door-shaped inline model found in demo1")
	}
}

func itoaTest(n int) string { return strconv.Itoa(n) }

// TestVisionWithoutMap: a Vision without a collision map fails closed (it
// sees nothing; a level loaded without its map must not hand the bot the
// server's PVS), unless NoOcclusion asks for the test-only open world.
func TestVisionWithoutMap(t *testing.T) {
	p := Vec3{100, 0, 0}
	lo, hi := Vec3{90, -10, -10}, Vec3{110, 10, 10}
	blind := NewVision(nil, ViewOptions{})
	blind.Begin(Vec3{}, Vec3{}, 90, area1, nil)
	if !blind.InFOV(p) {
		t.Fatal("the field of view does not need a map")
	}
	if blind.LOS(p, -1) || blind.Shootable(p, -1) || blind.SeesPoint(p) || blind.SeesBox(lo, hi, -1) ||
		blind.InPVS(lo, hi) || blind.PointInPVS(p) || blind.InPHS(p) || blind.SeesSegment(p, Vec3{100, 50, 0}, 4, -1) {
		t.Fatal("a Vision without a map sees")
	}
	if _, ok := blind.VisiblePoint(Vec3{-1, -1, -1}, Vec3{1, 1, 1}, -1); ok {
		t.Fatal("blind but inside a box")
	}
	open := NewVision(nil, ViewOptions{NoOcclusion: true})
	open.Begin(Vec3{}, Vec3{}, 90, area1, nil)
	if !open.LOS(p, -1) || !open.SeesBox(lo, hi, -1) || !open.InPVS(lo, hi) || !open.InPHS(p) {
		t.Fatal("NoOcclusion without a map must see everything in the fov")
	}
	if open.SeesPoint(Vec3{-100, 0, 0}) {
		t.Fatal("NoOcclusion ignores the fov")
	}
	// the perceiver: an in-fov soldier is not seen without a map, its cry
	// is still heard
	in := floorInput()
	pc := NewPerceiver(nil, NewClassifier(NewClassTable()), nil, Options{}).Perceive(&in)
	if len(pc.Seen) != 0 || len(pc.Heard) == 0 {
		t.Fatalf("blind perceiver: seen %d heard %d", len(pc.Seen), len(pc.Heard))
	}
	pc = NewPerceiver(nil, NewClassifier(NewClassTable()), nil, Options{View: ViewOptions{NoOcclusion: true}}).Perceive(&in)
	if pc.Sighting(20) == nil || pc.Sighting(23) != nil {
		t.Fatal("open perceiver: the in-fov soldier must be seen, the one outside the fov not")
	}
}

// TestSightingAim: Shootable is traced to the visible part of the box, so
// the answer never depends on geometry outside the view.
func TestSightingAim(t *testing.T) {
	p := newFloorPerceiver(t)
	in := floorInput()
	in.Events = Events{}
	// a soldier standing below the slab edge: only its top shows
	in.Entities = []shared.EntityState{{Number: 30, ModelIndex: 4, Origin: Vec3{100, 0, -30}, Solid: solidStd}}
	pc := p.Perceive(&in)
	s := pc.Sighting(30)
	if s == nil {
		t.Fatal("the soldier's top is visible")
	}
	if s.Aim == s.Center() || !pc.Vision().SeesPoint(s.Aim) || !s.Shootable {
		t.Fatalf("aim %v center %v shootable %v", s.Aim, s.Center(), s.Shootable)
	}
	// in the open the center is the aim point
	in.Entities[0].Origin = Vec3{200, 0, 24}
	if s := p.Perceive(&in).Sighting(30); s == nil || s.Aim != s.Center() {
		t.Fatalf("open sighting %+v", s)
	}
}
