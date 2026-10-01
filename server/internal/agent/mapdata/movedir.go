package mapdata

import "quake2web/server/internal/qcommon/shared"

// G_SetMovedir is a copy of the game function (mapdata does not import
// package game): it sets movedir from the editor angles and, like the
// original, clears *angles. The special "angle" values -1 (VEC_UP) and -2
// (VEC_DOWN) mean straight up and straight down; anything else is the
// AngleVectors forward vector.
// C: game/g_utils.c:314 G_SetMovedir
func G_SetMovedir(angles *Vec3, movedir *Vec3) {
	if shared.VectorCompare(*angles, Vec3{0, -1, 0}) != 0 { // VEC_UP
		*movedir = Vec3{0, 0, 1} // MOVEDIR_UP
	} else if shared.VectorCompare(*angles, Vec3{0, -2, 0}) != 0 { // VEC_DOWN
		*movedir = Vec3{0, 0, -1} // MOVEDIR_DOWN
	} else {
		shared.AngleVectors(*angles, movedir, nil, nil)
	}

	*angles = Vec3{}
}

// Movedir returns the direction G_SetMovedir derives from angles, leaving
// the caller's copy untouched.
func Movedir(angles Vec3) Vec3 {
	var md Vec3
	G_SetMovedir(&angles, &md)
	return md
}
