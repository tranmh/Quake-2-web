package shared

import (
	"math"
	"strings"

	"quake2web/server/internal/q2const"
)

// Float evaluation follows docs/PORTING.md: float*float stays float32, any
// double literal or libm call promotes to float64 and is narrowed on store.
// Products that feed sums are wrapped in float32() so the compiler can never
// fuse them into an FMA.

// deg2Rad360 is the double constant (M_PI*2 / 360) exactly as C folds it.
const deg2Rad360 = 0x1.1df46a2529d39p-6

// DotProduct is the C macro (x[0]*y[0]+x[1]*y[1]+x[2]*y[2]) in float.
// C: game/q_shared.h:154 DotProduct
func DotProduct(x, y Vec3) float32 {
	return float32(x[0]*y[0]) + float32(x[1]*y[1]) + float32(x[2]*y[2])
}

// VectorSubtract is the C macro c = a - b.
// C: game/q_shared.h:155 VectorSubtract
func VectorSubtract(a, b Vec3) Vec3 {
	return Vec3{a[0] - b[0], a[1] - b[1], a[2] - b[2]}
}

// VectorAdd is the C macro c = a + b.
// C: game/q_shared.h:156 VectorAdd
func VectorAdd(a, b Vec3) Vec3 {
	return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]}
}

// VectorNegate is the C macro b = -a.
// C: game/q_shared.h:159 VectorNegate
func VectorNegate(a Vec3) Vec3 {
	return Vec3{-a[0], -a[1], -a[2]}
}

// RotatePointAroundVector rotates point around dir by degrees.
// C: game/q_shared.c:32 RotatePointAroundVector
func RotatePointAroundVector(dir, point Vec3, degrees float32) Vec3 {
	var m, im, zrot, tmpmat, rot [3][3]float32
	var vr, vup, vf Vec3

	vf[0] = dir[0]
	vf[1] = dir[1]
	vf[2] = dir[2]

	vr = PerpendicularVector(dir)
	vup = CrossProduct(vr, vf)

	m[0][0] = vr[0]
	m[1][0] = vr[1]
	m[2][0] = vr[2]

	m[0][1] = vup[0]
	m[1][1] = vup[1]
	m[2][1] = vup[2]

	m[0][2] = vf[0]
	m[1][2] = vf[1]
	m[2][2] = vf[2]

	im = m

	im[0][1] = m[1][0]
	im[0][2] = m[2][0]
	im[1][0] = m[0][1]
	im[1][2] = m[2][1]
	im[2][0] = m[0][2]
	im[2][1] = m[1][2]

	zrot[0][0], zrot[1][1], zrot[2][2] = 1.0, 1.0, 1.0

	// DEG2RAD( a ) ( a * M_PI ) / 180.0F, all in double
	rad := float64(degrees) * MPI / 180.0
	zrot[0][0] = float32(math.Cos(rad))
	zrot[0][1] = float32(math.Sin(rad))
	zrot[1][0] = float32(-math.Sin(rad))
	zrot[1][1] = float32(math.Cos(rad))

	R_ConcatRotations(&m, &zrot, &tmpmat)
	R_ConcatRotations(&tmpmat, &im, &rot)

	var dst Vec3
	for i := 0; i < 3; i++ {
		dst[i] = float32(rot[i][0]*point[0]) + float32(rot[i][1]*point[1]) + float32(rot[i][2]*point[2])
	}
	return dst
}

// AngleVectors computes the forward, right and up vectors for angles. Any of
// the outputs may be nil.
// C: game/q_shared.c:93 AngleVectors
func AngleVectors(angles Vec3, forward, right, up *Vec3) {
	var angle, sr, sp, sy, cr, cp, cy float32

	angle = float32(float64(angles[q2const.YAW]) * deg2Rad360)
	sy = float32(math.Sin(float64(angle)))
	cy = float32(math.Cos(float64(angle)))
	angle = float32(float64(angles[q2const.PITCH]) * deg2Rad360)
	sp = float32(math.Sin(float64(angle)))
	cp = float32(math.Cos(float64(angle)))
	angle = float32(float64(angles[q2const.ROLL]) * deg2Rad360)
	sr = float32(math.Sin(float64(angle)))
	cr = float32(math.Cos(float64(angle)))

	if forward != nil {
		forward[0] = cp * cy
		forward[1] = cp * sy
		forward[2] = -sp
	}
	if right != nil {
		// (-1*sr*sp*cy+-1*cr*-sy): -1*x is exactly -x in float
		right[0] = float32(float32((-sr)*sp)*cy) + float32((-cr)*(-sy))
		right[1] = float32(float32((-sr)*sp)*sy) + float32((-cr)*cy)
		right[2] = (-sr) * cp
	}
	if up != nil {
		up[0] = float32(float32(cr*sp)*cy) + float32((-sr)*(-sy))
		up[1] = float32(float32(cr*sp)*sy) + float32((-sr)*cy)
		up[2] = cr * cp
	}
}

// ProjectPointOnPlane projects p onto the plane through the origin with normal.
// C: game/q_shared.c:130 ProjectPointOnPlane
func ProjectPointOnPlane(p, normal Vec3) Vec3 {
	var n Vec3
	invDenom := 1.0 / DotProduct(normal, normal)

	d := DotProduct(normal, p) * invDenom

	n[0] = normal[0] * invDenom
	n[1] = normal[1] * invDenom
	n[2] = normal[2] * invDenom

	return Vec3{
		p[0] - float32(d*n[0]),
		p[1] - float32(d*n[1]),
		p[2] - float32(d*n[2]),
	}
}

// PerpendicularVector returns a unit vector perpendicular to src (assumed
// normalized).
// C: game/q_shared.c:152 PerpendicularVector
func PerpendicularVector(src Vec3) Vec3 {
	pos := 0
	var minelem float32 = 1.0
	var tempvec Vec3

	// find the smallest magnitude axially aligned vector
	for i := 0; i < 3; i++ {
		if math.Abs(float64(src[i])) < float64(minelem) {
			pos = i
			minelem = float32(math.Abs(float64(src[i])))
		}
	}
	tempvec[pos] = 1.0

	// project the point onto the plane defined by src
	dst := ProjectPointOnPlane(tempvec, src)

	// normalize the result
	VectorNormalize(&dst)
	return dst
}

// R_ConcatRotations multiplies two 3x3 rotation matrices.
// C: game/q_shared.c:191 R_ConcatRotations
func R_ConcatRotations(in1, in2, out *[3][3]float32) {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = float32(in1[i][0]*in2[0][j]) + float32(in1[i][1]*in2[1][j]) +
				float32(in1[i][2]*in2[2][j])
		}
	}
}

// R_ConcatTransforms multiplies two 3x4 transforms.
// C: game/q_shared.c:219 R_ConcatTransforms
func R_ConcatTransforms(in1, in2, out *[3][4]float32) {
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			out[i][j] = float32(in1[i][0]*in2[0][j]) + float32(in1[i][1]*in2[1][j]) +
				float32(in1[i][2]*in2[2][j])
		}
		out[i][3] = float32(in1[i][0]*in2[0][3]) + float32(in1[i][1]*in2[1][3]) +
			float32(in1[i][2]*in2[2][3]) + in1[i][3]
	}
}

// Q_fabs clears the sign bit.
// C: game/q_shared.c:251 Q_fabs
func Q_fabs(f float32) float32 {
	return math.Float32frombits(math.Float32bits(f) & 0x7FFFFFFF)
}

// LerpAngle interpolates between two angles taking the short way round.
// C: game/q_shared.c:283 LerpAngle
func LerpAngle(a2, a1, frac float32) float32 {
	if a1-a2 > 180 {
		a1 -= 360
	}
	if a1-a2 < -180 {
		a1 += 360
	}
	return a2 + float32(frac*(a1-a2))
}

// Anglemod is C anglemod: quantizes to 16 bits in double precision.
// C: game/q_shared.c:293 anglemod
func Anglemod(a float32) float32 {
	return float32((360.0 / 65536) * float64(int32(float64(a)*(65536/360.0))&65535))
}

// BoxOnPlaneSide2 is the slow general version.
// C: game/q_shared.c:310 BoxOnPlaneSide2
func BoxOnPlaneSide2(emins, emaxs Vec3, p *CPlane) int {
	var corners [2]Vec3
	for i := 0; i < 3; i++ {
		if p.Normal[i] < 0 {
			corners[0][i] = emins[i]
			corners[1][i] = emaxs[i]
		} else {
			corners[1][i] = emins[i]
			corners[0][i] = emaxs[i]
		}
	}
	dist1 := DotProduct(p.Normal, corners[0]) - p.Dist
	dist2 := DotProduct(p.Normal, corners[1]) - p.Dist
	sides := 0
	if dist1 >= 0 {
		sides = 1
	}
	if dist2 < 0 {
		sides |= 2
	}
	return sides
}

// BoxOnPlaneSide returns 1, 2, or 1 + 2 (the portable C version).
// C: game/q_shared.c:349 BoxOnPlaneSide
func BoxOnPlaneSide(emins, emaxs *Vec3, p *CPlane) int {
	var dist1, dist2 float32
	n := &p.Normal

	// fast axial cases
	if p.Type < 3 {
		if p.Dist <= emins[p.Type] {
			return 1
		}
		if p.Dist >= emaxs[p.Type] {
			return 2
		}
		return 3
	}

	dot := func(a, b, c *Vec3) float32 {
		return float32(n[0]*a[0]) + float32(n[1]*b[1]) + float32(n[2]*c[2])
	}
	// general case
	switch p.SignBits {
	case 0:
		dist1 = dot(emaxs, emaxs, emaxs)
		dist2 = dot(emins, emins, emins)
	case 1:
		dist1 = dot(emins, emaxs, emaxs)
		dist2 = dot(emaxs, emins, emins)
	case 2:
		dist1 = dot(emaxs, emins, emaxs)
		dist2 = dot(emins, emaxs, emins)
	case 3:
		dist1 = dot(emins, emins, emaxs)
		dist2 = dot(emaxs, emaxs, emins)
	case 4:
		dist1 = dot(emaxs, emaxs, emins)
		dist2 = dot(emins, emins, emaxs)
	case 5:
		dist1 = dot(emins, emaxs, emins)
		dist2 = dot(emaxs, emins, emaxs)
	case 6:
		dist1 = dot(emaxs, emins, emins)
		dist2 = dot(emins, emaxs, emaxs)
	case 7:
		dist1 = dot(emins, emins, emins)
		dist2 = dot(emaxs, emaxs, emaxs)
	default:
		dist1, dist2 = 0, 0 // shut up compiler
	}

	sides := 0
	if dist1 >= p.Dist {
		sides = 1
	}
	if dist2 < p.Dist {
		sides |= 2
	}
	return sides
}

// BOX_ON_PLANE_SIDE is the C macro with the axial fast path inlined.
// C: game/q_shared.h:189 BOX_ON_PLANE_SIDE
func BOX_ON_PLANE_SIDE(emins, emaxs *Vec3, p *CPlane) int {
	if p.Type < 3 {
		if p.Dist <= emins[p.Type] {
			return 1
		}
		if p.Dist >= emaxs[p.Type] {
			return 2
		}
		return 3
	}
	return BoxOnPlaneSide(emins, emaxs, p)
}

// SignbitsForPlane returns signx + (signy<<1) + (signz<<2), as computed in
// CMod_LoadPlanes and ref_gl's SignbitsForPlane.
// C: ref_gl/gl_rmain.c:547 SignbitsForPlane
func SignbitsForPlane(p *CPlane) uint8 {
	var bits uint8
	for j := 0; j < 3; j++ {
		if p.Normal[j] < 0 {
			bits |= 1 << j
		}
	}
	return bits
}

// ClearBounds resets mins/maxs to an inverted box.
// C: game/q_shared.c:650 ClearBounds
func ClearBounds(mins, maxs *Vec3) {
	mins[0], mins[1], mins[2] = 99999, 99999, 99999
	maxs[0], maxs[1], maxs[2] = -99999, -99999, -99999
}

// AddPointToBounds expands mins/maxs to include v.
// C: game/q_shared.c:656 AddPointToBounds
func AddPointToBounds(v Vec3, mins, maxs *Vec3) {
	for i := 0; i < 3; i++ {
		val := v[i]
		if val < mins[i] {
			mins[i] = val
		}
		if val > maxs[i] {
			maxs[i] = val
		}
	}
}

// VectorCompare returns 1 if the vectors are equal.
// C: game/q_shared.c:672 VectorCompare
func VectorCompare(v1, v2 Vec3) int {
	if v1[0] != v2[0] || v1[1] != v2[1] || v1[2] != v2[2] {
		return 0
	}
	return 1
}

// VectorNormalize normalizes v in place and returns its length.
// C: game/q_shared.c:681 VectorNormalize
func VectorNormalize(v *Vec3) float32 {
	length := float32(v[0]*v[0]) + float32(v[1]*v[1]) + float32(v[2]*v[2])
	length = float32(math.Sqrt(float64(length)))

	if length != 0 {
		ilength := 1 / length
		v[0] *= ilength
		v[1] *= ilength
		v[2] *= ilength
	}
	return length
}

// VectorNormalize2 writes the normalized v to out and returns the length.
// out is left untouched when the length is zero, like C.
// C: game/q_shared.c:700 VectorNormalize2
func VectorNormalize2(v Vec3, out *Vec3) float32 {
	length := float32(v[0]*v[0]) + float32(v[1]*v[1]) + float32(v[2]*v[2])
	length = float32(math.Sqrt(float64(length)))

	if length != 0 {
		ilength := 1 / length
		out[0] = v[0] * ilength
		out[1] = v[1] * ilength
		out[2] = v[2] * ilength
	}
	return length
}

// VectorMA returns veca + scale*vecb.
// C: game/q_shared.c:719 VectorMA
func VectorMA(veca Vec3, scale float32, vecb Vec3) Vec3 {
	return Vec3{
		veca[0] + float32(scale*vecb[0]),
		veca[1] + float32(scale*vecb[1]),
		veca[2] + float32(scale*vecb[2]),
	}
}

// CrossProduct returns v1 x v2.
// C: game/q_shared.c:753 CrossProduct
func CrossProduct(v1, v2 Vec3) Vec3 {
	return Vec3{
		float32(v1[1]*v2[2]) - float32(v1[2]*v2[1]),
		float32(v1[2]*v2[0]) - float32(v1[0]*v2[2]),
		float32(v1[0]*v2[1]) - float32(v1[1]*v2[0]),
	}
}

// VectorLength returns |v|.
// C: game/q_shared.c:762 VectorLength
func VectorLength(v Vec3) float32 {
	var length float32
	for i := 0; i < 3; i++ {
		length += float32(v[i] * v[i])
	}
	return float32(math.Sqrt(float64(length)))
}

// VectorInverse negates v in place.
// C: game/q_shared.c:775 VectorInverse
func VectorInverse(v *Vec3) {
	v[0] = -v[0]
	v[1] = -v[1]
	v[2] = -v[2]
}

// VectorScale returns in*scale.
// C: game/q_shared.c:782 VectorScale
func VectorScale(in Vec3, scale float32) Vec3 {
	return Vec3{in[0] * scale, in[1] * scale, in[2] * scale}
}

// Q_log2 returns floor(log2(val)) for val > 0.
// C: game/q_shared.c:790 Q_log2
func Q_log2(val int32) int32 {
	var answer int32
	for {
		val >>= 1
		if val == 0 {
			break
		}
		answer++
	}
	return answer
}

//====================================================================================

// COM_SkipPath returns the part after the last '/'.
// C: game/q_shared.c:807 COM_SkipPath
func COM_SkipPath(pathname string) string {
	if i := strings.LastIndexByte(pathname, '/'); i >= 0 {
		return pathname[i+1:]
	}
	return pathname
}

// COM_StripExtension copies up to the first '.' (anywhere in the path).
// C: game/q_shared.c:826 COM_StripExtension
func COM_StripExtension(in string) string {
	if i := strings.IndexByte(in, '.'); i >= 0 {
		return in[:i]
	}
	return in
}

// COM_FileExtension returns up to 7 chars after the first '.'.
// C: game/q_shared.c:838 COM_FileExtension
func COM_FileExtension(in string) string {
	i := strings.IndexByte(in, '.')
	if i < 0 {
		return ""
	}
	ext := in[i+1:]
	if len(ext) > 7 {
		ext = ext[:7]
	}
	return ext
}

// COM_FileBase returns the base name without extension. Quirk kept: without a
// '/', the first character is dropped ("demo1.bsp" -> "emo1").
// C: game/q_shared.c:859 COM_FileBase
func COM_FileBase(in string) string {
	if len(in) == 0 {
		return "" // C reads in[-1] here (undefined); treat as empty
	}
	s := len(in) - 1
	for s != 0 && in[s] != '.' {
		s--
	}
	s2 := s
	for s2 != 0 && in[s2] != '/' {
		s2--
	}
	if s-s2 < 2 {
		return ""
	}
	s--
	return in[s2+1 : s2+1+(s-s2)]
}

// COM_FilePath returns the path up to, but not including the last '/'.
// C: game/q_shared.c:888 COM_FilePath
func COM_FilePath(in string) string {
	if len(in) == 0 {
		return ""
	}
	s := len(in) - 1
	for s != 0 && in[s] != '/' {
		s--
	}
	return in[:s]
}

// COM_DefaultExtension appends extension if path has no .EXT after the last
// '/'. Like C, the first character of path is never examined.
// C: game/q_shared.c:907 COM_DefaultExtension
func COM_DefaultExtension(path, extension string) string {
	src := len(path) - 1
	for src > 0 && path[src] != '/' {
		if path[src] == '.' {
			return path // it has an extension
		}
		src--
	}
	return path + extension
}

// COM_Parse parses one token from data. It returns the token, the remaining
// data and more=false where C sets *data_p = NULL (end of data). Characters
// are signed like C char: bytes >= 0x80 count as whitespace / word breaks
// outside quotes.
// C: game/q_shared.c:1072 COM_Parse
func COM_Parse(data string) (token string, rest string, more bool) {
	var tok [q2const.MAX_TOKEN_CHARS]byte
	length := 0
	i := 0
	at := func(k int) int {
		if k >= len(data) {
			return 0
		}
		return int(int8(data[k]))
	}

	// skip whitespace
	var c int
skipwhite:
	for {
		c = at(i)
		if c > ' ' {
			break
		}
		if c == 0 {
			return "", "", false
		}
		i++
	}

	// skip // comments
	if c == '/' && at(i+1) == '/' {
		for at(i) != 0 && at(i) != '\n' {
			i++
		}
		goto skipwhite
	}

	// handle quoted strings specially
	if c == '"' {
		i++
		for {
			c = at(i)
			i++
			if c == '"' || c == 0 {
				if i > len(data) {
					i = len(data)
				}
				return string(tok[:length]), data[i:], true
			}
			if length < q2const.MAX_TOKEN_CHARS {
				tok[length] = byte(c)
				length++
			}
		}
	}

	// parse a regular word
	for {
		if length < q2const.MAX_TOKEN_CHARS {
			tok[length] = byte(c)
			length++
		}
		i++
		c = at(i)
		if c <= 32 {
			break
		}
	}

	if length == q2const.MAX_TOKEN_CHARS {
		length = 0
	}
	return string(tok[:length]), data[i:], true
}

//============================================================================

// Q_stricmp is strcasecmp (C locale): the difference of the first differing
// lowercased bytes.
// C: game/q_shared.c:1180 Q_stricmp
func Q_stricmp(s1, s2 string) int {
	for i := 0; ; i++ {
		var c1, c2 int
		if i < len(s1) {
			c1 = int(s1[i])
		}
		if i < len(s2) {
			c2 = int(s2[i])
		}
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 || c1 == 0 {
			return c1 - c2
		}
	}
}

// Q_strncasecmp compares at most n bytes case-insensitively; returns 0 or -1.
// C: game/q_shared.c:1190 Q_strncasecmp
func Q_strncasecmp(s1, s2 string, n int) int {
	i := 0
	for {
		var c1, c2 int
		if i < len(s1) {
			c1 = int(int8(s1[i]))
		}
		if i < len(s2) {
			c2 = int(int8(s2[i]))
		}
		i++

		if n == 0 {
			return 0 // strings are equal until end point
		}
		n--

		if c1 != c2 {
			if c1 >= 'a' && c1 <= 'z' {
				c1 -= 'a' - 'A'
			}
			if c2 >= 'a' && c2 <= 'z' {
				c2 -= 'a' - 'A'
			}
			if c1 != c2 {
				return -1 // strings not equal
			}
		}
		if c1 == 0 {
			return 0
		}
	}
}

// Q_strcasecmp is Q_strncasecmp with n = 99999.
// C: game/q_shared.c:1216 Q_strcasecmp
func Q_strcasecmp(s1, s2 string) int {
	return Q_strncasecmp(s1, s2, 99999)
}

// cstr truncates at the first NUL, like C string functions see it.
func cstr(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

/*
=====================================================================

  INFO STRINGS

=====================================================================
*/

// Info_ValueForKey searches the string for the given key and returns the
// associated value, or an empty string.
// C: game/q_shared.c:1253 Info_ValueForKey
func Info_ValueForKey(s, key string) string {
	s = cstr(s)
	i := 0
	if i < len(s) && s[i] == '\\' {
		i++
	}
	for {
		start := i
		for {
			if i >= len(s) {
				return ""
			}
			if s[i] == '\\' {
				break
			}
			i++
		}
		pkey := s[start:i]
		i++

		vstart := i
		for i < len(s) && s[i] != '\\' {
			i++
		}
		value := s[vstart:i]

		if key == pkey {
			return value
		}
		if i >= len(s) {
			return ""
		}
		i++
	}
}

// Info_RemoveKey returns s with key's pair removed.
// C: game/q_shared.c:1295 Info_RemoveKey
func Info_RemoveKey(s, key string) string {
	s = cstr(s)
	if strings.Contains(key, "\\") {
		return s
	}
	i := 0
	for {
		start := i
		if i < len(s) && s[i] == '\\' {
			i++
		}
		kstart := i
		for {
			if i >= len(s) {
				return s
			}
			if s[i] == '\\' {
				break
			}
			i++
		}
		pkey := s[kstart:i]
		i++

		for i < len(s) && s[i] != '\\' {
			i++
		}

		if key == pkey {
			return s[:start] + s[i:] // remove this part
		}
		if i >= len(s) {
			return s
		}
	}
}

// Info_Validate reports whether s is free of characters that break parsing.
// C: game/q_shared.c:1353 Info_Validate
func Info_Validate(s string) bool {
	s = cstr(s)
	if strings.Contains(s, "\"") {
		return false
	}
	if strings.Contains(s, ";") {
		return false
	}
	return true
}

// Info_SetValueForKey returns s with key set to value. warn carries the text
// C would Com_Printf (empty when none).
// C: game/q_shared.c:1362 Info_SetValueForKey
func Info_SetValueForKey(s, key, value string) (result string, warn string) {
	s, key, value = cstr(s), cstr(key), cstr(value)
	const maxsize = q2const.MAX_INFO_STRING

	if strings.Contains(key, "\\") || strings.Contains(value, "\\") {
		return s, "Can't use keys or values with a \\\n"
	}
	if strings.Contains(key, ";") {
		return s, "Can't use keys or values with a semicolon\n"
	}
	if strings.Contains(key, "\"") || strings.Contains(value, "\"") {
		return s, "Can't use keys or values with a \"\n"
	}
	if len(key) > q2const.MAX_INFO_KEY-1 || len(value) > q2const.MAX_INFO_KEY-1 {
		return s, "Keys and values must be < 64 characters.\n"
	}
	s = Info_RemoveKey(s, key)
	if len(value) == 0 {
		return s, ""
	}

	newi := "\\" + key + "\\" + value
	if len(newi) > q2const.MAX_INFO_STRING-1 { // Com_sprintf truncation
		newi = newi[:q2const.MAX_INFO_STRING-1]
	}

	if len(newi)+len(s) > maxsize {
		return s, "Info string length exceeded\n"
	}

	// only copy ascii values
	b := []byte(s)
	for k := 0; k < len(newi); k++ {
		c := newi[k] & 127 // strip high bits
		if c >= 32 && c < 127 {
			b = append(b, c)
		}
	}
	return string(b), ""
}
