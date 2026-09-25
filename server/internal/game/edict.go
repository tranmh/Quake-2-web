package game

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the C vec3_t (float[3]).
type Vec3 = shared.Vec3

// Link is C link_t: the area-node link used by the server world (sv_world).
// C: game/game.h:61 link_t
type Link struct {
	Prev, Next *Link
	Ent        *Edict // owner of the link (replaces STRUCT_FROM_LINK); nil for area-node sentinels
}

// Edict is C edict_t. The first block of fields is the part visible to the
// server (game/game.h:86 struct edict_s, #ifndef GAME_INCLUDE); the server
// packages (internal/world, internal/sv) must only touch those fields.
// Game-private fields (game/g_local.h:965 struct edict_s) follow.
// C: game/game.h:86, game/g_local.h:965 edict_s
type Edict struct {
	// ---- server-visible (game.h) ----
	S         shared.EntityState
	Client    *GClient // nil if not a player
	InUse     bool
	LinkCount int32

	Area        Link  // linked to a division node or leaf
	NumClusters int32 // if -1, use headnode instead
	ClusterNums [q2const.MAX_ENT_CLUSTERS]int32
	HeadNode    int32 // unused if NumClusters != -1
	AreaNum     int32
	AreaNum2    int32

	SVFlags  int32 // SVF_NOCLIENT, SVF_DEADMONSTER, SVF_MONSTER, etc
	Mins     Vec3
	Maxs     Vec3
	AbsMin   Vec3
	AbsMax   Vec3
	Size     Vec3
	Solid    int32 // solid_t
	ClipMask int32
	Owner    *Edict

	// Index is the entity number (ent - g_edicts). Set once at allocation.
	Index int

	// ---- game private (g_local.h) ----
	EdictPrivate
}

// GClient is C gclient_t. PS and Ping are read by the server (game.h:78);
// the rest is game private (g_local.h:880).
// C: game/game.h:78, game/g_local.h:880 gclient_s
type GClient struct {
	PS   shared.PlayerState // communicated by server to clients
	Ping int32

	// Index is the client number (client - game.clients).
	Index int

	ClientPrivate
}
