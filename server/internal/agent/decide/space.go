package decide

import (
	"math"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
)

// SpaceProbe measures the free room around the bot: the distances it can
// move from origin along the level view directions forward, back, left and
// right (in that order) at view yaw. It must use static map knowledge only.
type SpaceProbe interface {
	Clearance(origin Vec3, yaw float32) [4]float32
}

// TraceSpace is a SpaceProbe that sweeps the player's box through the
// world's collision model (static brushes only: doors and plats are not in
// it, and drops are not detected; the navigator handles both). It owns a
// cmodel.State and is not safe for concurrent use.
type TraceSpace struct {
	cs  *cmodel.State
	max float32
}

// NewTraceSpace returns a TraceSpace over cm measuring up to maxDist units
// (256 if <= 0).
func NewTraceSpace(cm *cmodel.Map, maxDist float32) *TraceSpace {
	if maxDist <= 0 {
		maxDist = 256
	}
	return &TraceSpace{cs: cmodel.NewState(cm), max: maxDist}
}

// Clearance implements SpaceProbe. The box is the standing player raised
// by a step (18 units), so stairs do not count as walls.
func (t *TraceSpace) Clearance(origin Vec3, yaw float32) [4]float32 {
	mins, maxs := Vec3{-16, -16, -24 + 18}, Vec3{16, 16, 32}
	s, c := math.Sincos(float64(yaw) * math.Pi / 180)
	fx, fy := float32(c), float32(s)
	dirs := [4][2]float32{{fx, fy}, {-fx, -fy}, {-fy, fx}, {fy, -fx}} // forward, back, left, right
	var out [4]float32
	for i, d := range dirs {
		end := Vec3{origin[0] + d[0]*t.max, origin[1] + d[1]*t.max, origin[2]}
		tr := t.cs.BoxTrace(origin, end, mins, maxs, 0, q2const.MASK_PLAYERSOLID)
		if tr.StartSolid || tr.AllSolid {
			continue
		}
		out[i] = tr.Fraction * t.max
	}
	return out
}
