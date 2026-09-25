// Package shared ports game/q_shared.h and game/q_shared.c: the math library,
// string helpers, info strings and the core types shared by the engine, the
// game module and the client.
package shared

import (
	"fmt"

	"quake2web/server/internal/q2const"
)

// Vec3 is C vec3_t (float[3]).
// C: game/q_shared.h:126 vec3_t
type Vec3 = [3]float32

// Vec3Origin is C vec3_origin. It is a value, never mutated.
// C: game/q_shared.c:24 vec3_origin
var Vec3Origin = Vec3{}

// MPI is C M_PI (a double literal).
// C: game/q_shared.h:134 M_PI
const MPI = 3.14159265358979323846

// CPlane is C cplane_t.
// C: game/q_shared.h:412 cplane_t
type CPlane struct {
	Normal   Vec3
	Dist     float32
	Type     uint8 // for fast side tests
	SignBits uint8 // signx + (signy<<1) + (signz<<2)
}

// CModel is C cmodel_t.
// C: game/q_shared.h:429 cmodel_t
type CModel struct {
	Mins, Maxs Vec3
	Origin     Vec3 // for sounds or lights
	Headnode   int32
}

// CSurface is C csurface_t. Name holds at most 15 bytes like char[16].
// C: game/q_shared.h:436 csurface_t
type CSurface struct {
	Name  string
	Flags int32
	Value int32
}

// MapSurface is C mapsurface_t (csurface_t plus the full 32 byte name).
// C: game/q_shared.h:442 mapsurface_t
type MapSurface struct {
	C     CSurface
	RName string
}

// Trace is C trace_t. Ent is an opaque entity id: -1 means NULL (CM_* never
// set it, so cmodel returns -1).
// C: game/q_shared.h:455 trace_t
type Trace struct {
	AllSolid   bool    // if true, plane is not valid
	StartSolid bool    // if true, the initial point was in a solid area
	Fraction   float32 // time completed, 1.0 = didn't hit anything
	EndPos     Vec3    // final position
	Plane      CPlane  // surface normal at impact
	Surface    *CSurface
	Contents   int32 // contents on other side of surface hit
	Ent        int   // not set by CM_*() functions; -1 = NULL
}

// PmoveState is C pmove_state_t; communicated bit-exact.
// C: game/q_shared.h:497 pmove_state_t
type PmoveState struct {
	PmType      int32 // pmtype_t
	Origin      [3]int16
	Velocity    [3]int16
	PmFlags     uint8
	PmTime      uint8
	Gravity     int16
	DeltaAngles [3]int16
}

// UserCmd is C usercmd_t.
// C: game/q_shared.h:517 usercmd_t
type UserCmd struct {
	Msec        uint8
	Buttons     uint8
	Angles      [3]int16
	ForwardMove int16
	SideMove    int16
	UpMove      int16
	Impulse     uint8
	LightLevel  uint8
}

// EntityState is C entity_state_t.
// C: game/q_shared.h:1157 entity_state_t
type EntityState struct {
	Number      int32
	Origin      Vec3
	Angles      Vec3
	OldOrigin   Vec3
	ModelIndex  int32
	ModelIndex2 int32
	ModelIndex3 int32
	ModelIndex4 int32
	Frame       int32
	SkinNum     int32
	Effects     uint32
	RenderFX    int32
	Solid       int32
	Sound       int32
	Event       int32
}

// PlayerState is C player_state_t.
// C: game/q_shared.h:1189 player_state_t
type PlayerState struct {
	PMove      PmoveState
	ViewAngles Vec3
	ViewOffset Vec3
	KickAngles Vec3
	GunAngles  Vec3
	GunOffset  Vec3
	GunIndex   int32
	GunFrame   int32
	Blend      [4]float32
	Fov        float32
	RDFlags    int32
	Stats      [q2const.MAX_STATS]int16
}

// ANGLE2SHORT is the C macro ((int)((x)*65536/360) & 65535), evaluated in float.
// C: game/q_shared.h:1084 ANGLE2SHORT
func ANGLE2SHORT(x float32) int32 {
	return int32(float32(x*65536)/360) & 65535
}

// SHORT2ANGLE is the C macro ((x)*(360.0/65536)); the result is a double.
// C: game/q_shared.h:1085 SHORT2ANGLE
func SHORT2ANGLE(x int32) float64 {
	return float64(x) * (360.0 / 65536)
}

// ComError is raised (via panic) where C calls Com_Error. Code is ERR_FATAL,
// ERR_DROP or ERR_DISCONNECT. Callers recover it at a frame boundary.
type ComError struct {
	Code int
	Msg  string
}

func (e ComError) Error() string { return e.Msg }

// Error panics with a ComError, like C Com_Error.
// C: qcommon/common.c:175 Com_Error
func Error(code int, format string, args ...any) {
	panic(ComError{Code: code, Msg: fmt.Sprintf(format, args...)})
}
