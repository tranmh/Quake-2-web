package perception

import (
	"math"
	"strconv"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Default view parameters.
const (
	// DefaultFov is the client's default horizontal field of view (the
	// "fov" userinfo and player_state_t.fov).
	DefaultFov = 90
	// DefaultAspect is the width/height of the classic 640x480 view.
	DefaultAspect = 4.0 / 3.0
)

// ViewOptions configure the field of view.
//
// The horizontal field of view is the client's fov_x: player_state_t.fov,
// which the server copies from the "fov" userinfo (so the browser viewer of
// the bot sees what the bot knows). The vertical field of view follows from
// the viewer's aspect exactly like CalcFov in cl_view.c:
// fov_y = 2*atan(tan(fov_x/2) / aspect). The default aspect is 4:3, the
// original 640x480 view; a wide-screen viewer sees less vertically at the
// same fov_x, so 4:3 is the most permissive classic choice.
type ViewOptions struct {
	FovX   float32 // degrees; 0 uses the player state's fov (DefaultFov if 0)
	Aspect float32 // width/height; 0 means DefaultAspect
}

// BrushPose is the pose of a brush entity (inline model "*N") in a frame.
type BrushPose struct {
	Num    int32 // entity number
	Inline int   // N of "*N"
	Origin Vec3
	Angles Vec3
}

type brushOcc struct {
	num            int32
	headnode       int32
	origin, angles Vec3
	absmin, absmax Vec3
}

// Vision answers what the eye can see in one frame: field of view, line of
// sight against the world and the brush entities of the frame, and the
// server's PVS/PHS and area culling. It owns its collision state on the
// shared immutable cmodel.Map, so each bot has its own Vision. Begin sets
// the frame; the queries are valid until the next Begin. A nil map means
// no geometry: every line of sight is clear and everything is in the PVS.
type Vision struct {
	cm  *cmodel.Map
	st  *cmodel.State
	opt ViewOptions

	eye, angles    Vec3
	fwd, right, up Vec3
	tanX, tanY     float64
	fovX, fovY     float32
	brushes        []brushOcc
	areabits       [q2const.MAX_MAP_AREAS / 8]byte
	areaBytes      int
	clientArea     int32
	clientCluster  int32
	fatpvs         [q2const.MAX_MAP_LEAFS / 8]byte
	phs            [q2const.MAX_MAP_LEAFS / 8]byte
	leafs          [128]int32
}

// NewVision returns a Vision on map cm (nil: no geometry).
func NewVision(cm *cmodel.Map, opt ViewOptions) *Vision {
	v := &Vision{cm: cm, opt: opt}
	if cm != nil {
		v.st = cmodel.NewState(cm)
	}
	return v
}

// Map returns the collision map (nil if none).
func (v *Vision) Map() *cmodel.Map { return v.cm }

// Begin sets up the frame: the eye position, the view angles, the frame's
// fov (player_state_t.fov; overridden by ViewOptions.FovX), the area bits
// the server sent and the brush entities that occlude.
func (v *Vision) Begin(eye, angles Vec3, fov float32, areabits []byte, brushes []BrushPose) {
	v.eye, v.angles = eye, angles
	shared.AngleVectors(angles, &v.fwd, &v.right, &v.up)
	fx := v.opt.FovX
	if fx <= 0 {
		fx = fov
	}
	if fx < 1 || fx > 179 {
		fx = DefaultFov
	}
	aspect := float64(v.opt.Aspect)
	if aspect <= 0 {
		aspect = DefaultAspect
	}
	v.tanX = math.Tan(float64(fx) / 360 * math.Pi)
	v.tanY = v.tanX / aspect
	v.fovX = fx
	v.fovY = float32(math.Atan(v.tanY) * 360 / math.Pi)
	v.areabits = [q2const.MAX_MAP_AREAS / 8]byte{}
	v.areaBytes = copy(v.areabits[:], areabits)

	v.brushes = v.brushes[:0]
	if v.cm == nil {
		return
	}
	for _, b := range brushes {
		if b.Inline < 1 || b.Inline >= v.cm.NumInlineModels() {
			continue
		}
		mod := v.cm.InlineModel("*" + strconv.Itoa(b.Inline))
		occ := brushOcc{num: b.Num, headnode: mod.Headnode, origin: b.Origin, angles: b.Angles}
		occ.absmin, occ.absmax = brushBounds(mod.Mins, mod.Maxs, b.Origin, b.Angles)
		v.brushes = append(v.brushes, occ)
	}
	v.computePVS()
}

// brushBounds is the world box of an inline model at a pose, expanded for
// rotation like SV_LinkEdict.
func brushBounds(mins, maxs, origin, angles Vec3) (Vec3, Vec3) {
	var lo, hi Vec3
	if angles != (Vec3{}) {
		var r float32
		for i := 0; i < 3; i++ {
			r = max(r, float32(math.Abs(float64(mins[i]))), float32(math.Abs(float64(maxs[i]))))
		}
		for i := 0; i < 3; i++ {
			lo[i], hi[i] = origin[i]-r, origin[i]+r
		}
	} else {
		for i := 0; i < 3; i++ {
			lo[i], hi[i] = origin[i]+mins[i], origin[i]+maxs[i]
		}
	}
	return lo, hi
}

// BrushBounds returns the world box of inline model "*n" at a pose (ok
// false for a bad model number or without a map).
func (v *Vision) BrushBounds(n int, origin, angles Vec3) (absmin, absmax Vec3, ok bool) {
	if v.cm == nil || n < 1 || n >= v.cm.NumInlineModels() {
		return Vec3{}, Vec3{}, false
	}
	mod := v.cm.InlineModel("*" + strconv.Itoa(n))
	lo, hi := brushBounds(mod.Mins, mod.Maxs, origin, angles)
	return lo, hi, true
}

// computePVS mirrors SV_FatPVS and the client area/cluster lookup of
// SV_BuildClientFrame for the eye.
// C: server/sv_ents.c:478 SV_FatPVS, server/sv_ents.c:527 SV_BuildClientFrame
func (v *Vision) computePVS() {
	rowBytes := (v.cm.NumClusters() + 7) >> 3
	clear(v.fatpvs[:rowBytes])
	var lo, hi Vec3
	for i := 0; i < 3; i++ {
		lo[i], hi[i] = v.eye[i]-8, v.eye[i]+8
	}
	n := v.st.BoxLeafnums(lo, hi, v.leafs[:64], nil)
	var clusters [64]int32
	for i := 0; i < n; i++ {
		c := v.cm.LeafCluster(int(v.leafs[i]))
		dup := false
		for j := 0; j < i; j++ {
			if clusters[j] == c {
				dup = true
				break
			}
		}
		clusters[i] = c
		if dup {
			continue
		}
		row := v.st.ClusterPVS(int(c))
		for j := 0; j < rowBytes; j++ {
			v.fatpvs[j] |= row[j]
		}
	}
	leaf := v.st.PointLeafnum(v.eye)
	v.clientArea = v.cm.LeafArea(int(leaf))
	v.clientCluster = v.cm.LeafCluster(int(leaf))
	copy(v.phs[:rowBytes], v.st.ClusterPHS(int(v.clientCluster)))
}

// Eye returns the eye position of the frame.
func (v *Vision) Eye() Vec3 { return v.eye }

// Axes returns the view's forward, right and up vectors.
func (v *Vision) Axes() (fwd, right, up Vec3) { return v.fwd, v.right, v.up }

// Fov returns the horizontal and vertical field of view in degrees.
func (v *Vision) Fov() (x, y float32) { return v.fovX, v.fovY }

// InFOV reports whether p is inside the view frustum.
func (v *Vision) InFOV(p Vec3) bool {
	d := shared.VectorSubtract(p, v.eye)
	x := float64(shared.DotProduct(d, v.fwd))
	if x <= 0 {
		return false
	}
	return math.Abs(float64(shared.DotProduct(d, v.right))) <= x*v.tanX &&
		math.Abs(float64(shared.DotProduct(d, v.up))) <= x*v.tanY
}

// LOS reports whether nothing opaque (MASK_OPAQUE: solid, lava, slime of
// the world or a brush entity) lies between the eye and p. Brush entity
// ignore (its entity number; -1 for none) does not block, so a door can be
// tested against its own box.
func (v *Vision) LOS(p Vec3, ignore int32) bool {
	return v.clear(v.eye, p, q2const.MASK_OPAQUE, ignore)
}

// LOSBetween is LOS between two arbitrary points.
func (v *Vision) LOSBetween(a, b Vec3, ignore int32) bool {
	return v.clear(a, b, q2const.MASK_OPAQUE, ignore)
}

// Shootable reports whether a shot from the eye reaches p through the world
// and the brush entities (MASK_SHOT: glass stops shots but not sight).
// Bodies in the way are not considered.
func (v *Vision) Shootable(p Vec3, ignore int32) bool {
	return v.clear(v.eye, p, q2const.MASK_SHOT&^(q2const.CONTENTS_MONSTER|q2const.CONTENTS_DEADMONSTER), ignore)
}

// SeesPoint reports whether p is in the field of view with a line of sight.
func (v *Vision) SeesPoint(p Vec3) bool { return v.InFOV(p) && v.LOS(p, -1) }

// SeesBox reports whether some part of the world box [absmin, absmax] is in
// the field of view with a line of sight from the eye. It samples the
// center, near the top and bottom, and for large boxes the inset corners.
func (v *Vision) SeesBox(absmin, absmax Vec3, ignore int32) bool {
	inside := true
	for i := 0; i < 3; i++ {
		if v.eye[i] < absmin[i] || v.eye[i] > absmax[i] {
			inside = false
		}
	}
	if inside {
		return true
	}
	for _, p := range boxSamples(absmin, absmax) {
		if v.InFOV(p) && v.LOS(p, ignore) {
			return true
		}
	}
	return false
}

func boxSamples(lo, hi Vec3) []Vec3 {
	c := Vec3{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2}
	out := []Vec3{c}
	h := hi[2] - lo[2]
	if h > 24 {
		out = append(out, Vec3{c[0], c[1], hi[2] - 4}, Vec3{c[0], c[1], lo[2] + 6})
	}
	ext := max(hi[0]-lo[0], hi[1]-lo[1], h)
	if ext > 64 {
		const in = 4
		for _, x := range [2]float32{lo[0] + in, hi[0] - in} {
			for _, y := range [2]float32{lo[1] + in, hi[1] - in} {
				for _, z := range [2]float32{lo[2] + in, hi[2] - in} {
					out = append(out, Vec3{x, y, z})
				}
			}
		}
	}
	return out
}

// SeesSegment reports whether any of n+1 evenly spaced points from a to b
// is visible (beams, trails).
func (v *Vision) SeesSegment(a, b Vec3, n int, ignore int32) bool {
	if n < 1 {
		n = 1
	}
	for i := 0; i <= n; i++ {
		f := float32(i) / float32(n)
		p := Vec3{a[0] + (b[0]-a[0])*f, a[1] + (b[1]-a[1])*f, a[2] + (b[2]-a[2])*f}
		if v.InFOV(p) && v.LOS(p, ignore) {
			return true
		}
	}
	return false
}

// clear traces a point from a to b through the world and the frame's brush
// entities with mask; the last unit before b does not count, so a point on
// a surface is visible.
func (v *Vision) clear(a, b Vec3, mask int32, ignore int32) bool {
	if v.cm == nil {
		return true
	}
	length := shared.VectorLength(shared.VectorSubtract(b, a))
	if length < 1 {
		return true
	}
	blocked := func(tr shared.Trace) bool {
		if tr.AllSolid || tr.StartSolid {
			return tr.AllSolid || tr.Fraction < 1
		}
		return tr.Fraction < 1 && (1-tr.Fraction)*length > 1
	}
	var zero Vec3
	if blocked(v.st.BoxTrace(a, b, zero, zero, 0, mask)) {
		return false
	}
	for i := range v.brushes {
		bo := &v.brushes[i]
		if bo.num == ignore || !segmentHitsBox(a, b, bo.absmin, bo.absmax) {
			continue
		}
		if blocked(v.st.TransformedBoxTrace(a, b, zero, zero, bo.headnode, mask, bo.origin, bo.angles)) {
			return false
		}
	}
	return true
}

// segmentHitsBox is the slab test of segment a-b against a box grown by 1.
func segmentHitsBox(a, b, lo, hi Vec3) bool {
	t0, t1 := 0.0, 1.0
	for i := 0; i < 3; i++ {
		d := float64(b[i] - a[i])
		l, h := float64(lo[i]-1), float64(hi[i]+1)
		if math.Abs(d) < 1e-9 {
			if float64(a[i]) < l || float64(a[i]) > h {
				return false
			}
			continue
		}
		u, w := (l-float64(a[i]))/d, (h-float64(a[i]))/d
		if u > w {
			u, w = w, u
		}
		t0, t1 = math.Max(t0, u), math.Min(t1, w)
		if t0 > t1 {
			return false
		}
	}
	return true
}

func (v *Vision) bitSet(bits []byte, n int32) bool {
	return n >= 0 && int(n>>3) < len(bits) && bits[n>>3]&(1<<(n&7)) != 0
}

func (v *Vision) areaOpen(area int32) bool {
	if area < 0 || int(area>>3) >= v.areaBytes {
		return false
	}
	return v.areabits[area>>3]&(1<<(area&7)) != 0
}

// InPVS reports whether the server would send an entity with the world box
// [absmin, absmax]: some leaf of the box is in an area connected to the
// eye's (the frame's area bits) and some cluster of it in the eye's fat
// PVS, as SV_BuildClientFrame decides.
// C: server/sv_ents.c:527 SV_BuildClientFrame
func (v *Vision) InPVS(absmin, absmax Vec3) bool {
	if v.cm == nil {
		return true
	}
	for i := 0; i < 3; i++ {
		absmin[i]--
		absmax[i]++
	}
	n := v.st.BoxLeafnums(absmin, absmax, v.leafs[:], nil)
	var area1, area2 int32
	visible := false
	for i := 0; i < n; i++ {
		l := int(v.leafs[i])
		if a := v.cm.LeafArea(l); a != 0 {
			if area1 != 0 && area1 != a {
				area2 = a
			} else {
				area1 = a
			}
		}
		if c := v.cm.LeafCluster(l); c != -1 && v.bitSet(v.fatpvs[:], c) {
			visible = true
		}
	}
	if !v.areaOpen(area1) && (area2 == 0 || !v.areaOpen(area2)) {
		return false
	}
	return visible
}

// PointInPVS is InPVS for a point.
func (v *Vision) PointInPVS(p Vec3) bool {
	return v.InPVS(p, p)
}

// InPHS reports whether a beam whose first point is p would be sent: its
// cluster in the eye's PHS and its area connected (beams only check one
// point, against the PHS).
func (v *Vision) InPHS(p Vec3) bool {
	if v.cm == nil {
		return true
	}
	leaf := int(v.st.PointLeafnum(p))
	if !v.areaOpen(v.cm.LeafArea(leaf)) {
		return false
	}
	return v.bitSet(v.phs[:], v.cm.LeafCluster(leaf))
}

// PointContents returns the contents at p of the world and the frame's
// brush entities.
func (v *Vision) PointContents(p Vec3) int32 {
	if v.cm == nil {
		return 0
	}
	c := v.st.PointContents(p, 0)
	for i := range v.brushes {
		b := &v.brushes[i]
		if p[0] < b.absmin[0] || p[1] < b.absmin[1] || p[2] < b.absmin[2] ||
			p[0] > b.absmax[0] || p[1] > b.absmax[1] || p[2] > b.absmax[2] {
			continue
		}
		c |= v.st.TransformedPointContents(p, b.headnode, b.origin, b.angles)
	}
	return c
}
