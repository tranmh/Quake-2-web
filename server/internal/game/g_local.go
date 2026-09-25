package game

// Port of game/g_local.h: local definitions for the game module.
//
// Porting conventions used by every file of this package (see docs/PORTING.md):
//
//   - Every C function is a method on *Game with its exact C name
//     (g.T_Damage, g.door_go_up, g.SP_func_door). Stateless helpers
//     (vtos, tv, vectoyaw, vectoangles, G_ProjectSource, ...) are package
//     functions with their exact C name. The single exception is range
//     (a Go keyword): range_.
//   - C function pointers stored in edicts/items/mmoves are registry handles
//     (registry.go): package-level variables named exactly like the C
//     function (door_go_up is the handle, g.door_go_up the method), bound in
//     an init() of the defining file. Calling a handle: ent.Think.fn(g, ent).
//   - mmove_t tables are package-level *MMove variables with the C variable
//     name (soldier_move_stand), created with defMMove.
//   - Private edict/client/level/... fields use the C name converted
//     mechanically to CamelCase: split at '_', capitalize each part, join
//     (touch_debounce_time -> TouchDebounceTime, nextthink -> Nextthink,
//     monsterinfo -> Monsterinfo, v_dmg_roll -> VDmgRoll).
//   - C globals are fields of Game with their exact C name (g.level,
//     g.game, g.st, g.deathmatch, g.meansOfDeath, g.enemy_vis, ...).
//   - Types: int -> int32, float -> float32, qboolean -> bool, char* -> string
//     ("" stands for NULL), edict_t* -> *Edict, gitem_t* -> *GItem,
//     gclient_t* -> *GClient, vec3_t parameters: input-only -> Vec3 (by
//     value), pure outputs -> returned, modified in place -> *Vec3, NULL-able
//     -> *Vec3.
//   - Float expressions follow C promotion (ADR-0003): FRAMETIME and every
//     literal with a '.' is a double: level.time + FRAMETIME is
//     float32(float64(g.level.Time) + FRAMETIME).
//   - random()/crandom() are g.random() (float32) and g.crandom() (float64);
//     rand() is g.rng.Rand(). The call order of the C code is kept.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// GAMEVERSION: the "gameversion" client command prints this plus compile date.
// C: game/g_local.h:31 GAMEVERSION
const GAMEVERSION = "baseq2"

// protocol bytes that can be directly added to messages
// C: game/g_local.h:34
const (
	svc_muzzleflash  = 1
	svc_muzzleflash2 = 2
	svc_temp_entity  = 3
	svc_layout       = 4
	svc_inventory    = 5
	svc_stufftext    = 11
)

// view pitching times (double literals)
// C: game/g_local.h:44
const (
	DAMAGE_TIME = 0.5
	FALL_TIME   = 0.3
)

// edict->spawnflags
// C: game/g_local.h:50
const (
	SPAWNFLAG_NOT_EASY       = 0x00000100
	SPAWNFLAG_NOT_MEDIUM     = 0x00000200
	SPAWNFLAG_NOT_HARD       = 0x00000400
	SPAWNFLAG_NOT_DEATHMATCH = 0x00000800
	SPAWNFLAG_NOT_COOP       = 0x00001000
)

// edict->flags
// C: game/g_local.h:57
const (
	FL_FLY           = 0x00000001
	FL_SWIM          = 0x00000002 // implied immunity to drowining
	FL_IMMUNE_LASER  = 0x00000004
	FL_INWATER       = 0x00000008
	FL_GODMODE       = 0x00000010
	FL_NOTARGET      = 0x00000020
	FL_IMMUNE_SLIME  = 0x00000040
	FL_IMMUNE_LAVA   = 0x00000080
	FL_PARTIALGROUND = 0x00000100 // not all corners are valid
	FL_WATERJUMP     = 0x00000200 // player jumping out of water
	FL_TEAMSLAVE     = 0x00000400 // not the first on the team
	FL_NO_KNOCKBACK  = 0x00000800
	FL_POWER_ARMOR   = 0x00001000 // power armor (if any) is active
	// FL_RESPAWN is 0x80000000 in C (an int, so negative).
	FL_RESPAWN = -0x80000000 // used for item respawning
)

// FRAMETIME is a double literal in C.
// C: game/g_local.h:74 FRAMETIME
const FRAMETIME = 0.1

// memory tags to allow dynamic memory to be cleaned up
// C: game/g_local.h:77
const (
	TAG_GAME  = 765 // clear when unloading the dll
	TAG_LEVEL = 766 // clear when loading a new level
)

// C: game/g_local.h:81
const (
	MELEE_DISTANCE  = 80
	BODY_QUEUE_SIZE = 8
)

// damage_t
// C: game/g_local.h:85
const (
	DAMAGE_NO  = 0
	DAMAGE_YES = 1 // will take damage if hit
	DAMAGE_AIM = 2 // auto targeting recognizes this
)

// weaponstate_t
// C: game/g_local.h:92
const (
	WEAPON_READY      = 0
	WEAPON_ACTIVATING = 1
	WEAPON_DROPPING   = 2
	WEAPON_FIRING     = 3
)

// ammo_t
// C: game/g_local.h:100
const (
	AMMO_BULLETS  = 0
	AMMO_SHELLS   = 1
	AMMO_ROCKETS  = 2
	AMMO_GRENADES = 3
	AMMO_CELLS    = 4
	AMMO_SLUGS    = 5
)

// deadflag
// C: game/g_local.h:111
const (
	DEAD_NO          = 0
	DEAD_DYING       = 1
	DEAD_DEAD        = 2
	DEAD_RESPAWNABLE = 3
)

// range
// C: game/g_local.h:117
const (
	RANGE_MELEE = 0
	RANGE_NEAR  = 1
	RANGE_MID   = 2
	RANGE_FAR   = 3
)

// gib types
// C: game/g_local.h:123
const (
	GIB_ORGANIC  = 0
	GIB_METALLIC = 1
)

// monster ai flags
// C: game/g_local.h:127
const (
	AI_STAND_GROUND      = 0x00000001
	AI_TEMP_STAND_GROUND = 0x00000002
	AI_SOUND_TARGET      = 0x00000004
	AI_LOST_SIGHT        = 0x00000008
	AI_PURSUIT_LAST_SEEN = 0x00000010
	AI_PURSUE_NEXT       = 0x00000020
	AI_PURSUE_TEMP       = 0x00000040
	AI_HOLD_FRAME        = 0x00000080
	AI_GOOD_GUY          = 0x00000100
	AI_BRUTAL            = 0x00000200
	AI_NOSTEP            = 0x00000400
	AI_DUCKED            = 0x00000800
	AI_COMBAT_POINT      = 0x00001000
	AI_MEDIC             = 0x00002000
	AI_RESURRECTING      = 0x00004000
)

// monster attack state
// C: game/g_local.h:144
const (
	AS_STRAIGHT = 1
	AS_SLIDING  = 2
	AS_MELEE    = 3
	AS_MISSILE  = 4
)

// armor types
// C: game/g_local.h:150
const (
	ARMOR_NONE   = 0
	ARMOR_JACKET = 1
	ARMOR_COMBAT = 2
	ARMOR_BODY   = 3
	ARMOR_SHARD  = 4
)

// power armor types
// C: game/g_local.h:157
const (
	POWER_ARMOR_NONE   = 0
	POWER_ARMOR_SCREEN = 1
	POWER_ARMOR_SHIELD = 2
)

// handedness values
// C: game/g_local.h:162
const (
	RIGHT_HANDED  = 0
	LEFT_HANDED   = 1
	CENTER_HANDED = 2
)

// game.serverflags values
// C: game/g_local.h:168
const (
	SFL_CROSS_TRIGGER_1    = 0x00000001
	SFL_CROSS_TRIGGER_2    = 0x00000002
	SFL_CROSS_TRIGGER_3    = 0x00000004
	SFL_CROSS_TRIGGER_4    = 0x00000008
	SFL_CROSS_TRIGGER_5    = 0x00000010
	SFL_CROSS_TRIGGER_6    = 0x00000020
	SFL_CROSS_TRIGGER_7    = 0x00000040
	SFL_CROSS_TRIGGER_8    = 0x00000080
	SFL_CROSS_TRIGGER_MASK = 0x000000ff
)

// noise types for PlayerNoise
// C: game/g_local.h:180
const (
	PNOISE_SELF   = 0
	PNOISE_WEAPON = 1
	PNOISE_IMPACT = 2
)

// movetype_t: edict->movetype values
// C: game/g_local.h:187
const (
	MOVETYPE_NONE       = 0 // never moves
	MOVETYPE_NOCLIP     = 1 // origin and angles change with no interaction
	MOVETYPE_PUSH       = 2 // no clip to world, push on box contact
	MOVETYPE_STOP       = 3 // no clip to world, stops on box contact
	MOVETYPE_WALK       = 4 // gravity
	MOVETYPE_STEP       = 5 // gravity, special edge handling
	MOVETYPE_FLY        = 6
	MOVETYPE_TOSS       = 7 // gravity
	MOVETYPE_FLYMISSILE = 8 // extra size to monsters
	MOVETYPE_BOUNCE     = 9
)

// GItemArmor is C gitem_armor_t.
// C: game/g_local.h:203 gitem_armor_t
type GItemArmor struct {
	BaseCount        int32
	MaxCount         int32
	NormalProtection float32
	EnergyProtection float32
	Armor            int32
}

// gitem_t->flags
// C: game/g_local.h:213
const (
	IT_WEAPON    = 1 // use makes active weapon
	IT_AMMO      = 2
	IT_ARMOR     = 4
	IT_STAY_COOP = 8
	IT_KEY       = 16
	IT_POWERUP   = 32
)

// gitem_t->weapmodel for weapons indicates model index
// C: game/g_local.h:221
const (
	WEAP_BLASTER         = 1
	WEAP_SHOTGUN         = 2
	WEAP_SUPERSHOTGUN    = 3
	WEAP_MACHINEGUN      = 4
	WEAP_CHAINGUN        = 5
	WEAP_GRENADES        = 6
	WEAP_GRENADELAUNCHER = 7
	WEAP_ROCKETLAUNCHER  = 8
	WEAP_HYPERBLASTER    = 9
	WEAP_RAILGUN         = 10
	WEAP_BFG             = 11
)

// GItem is C gitem_t. The item table (itemlist, g_items.go) is immutable
// package data; *GItem pointers are compared by identity.
// C: game/g_local.h:233 gitem_s
type GItem struct {
	Classname       string // spawning name
	Pickup          PickupFn
	Use             ItemFn
	Drop            ItemFn
	Weaponthink     ThinkFn
	PickupSound     string
	WorldModel      string
	WorldModelFlags int32
	ViewModel       string

	// client side info
	Icon       string
	PickupName string // for printing on pickup
	CountWidth int32  // number of digits to display by icon

	Quantity int32  // for ammo how much, for weapons how much is used per shot
	Ammo     string // for weapons
	Flags    int32  // IT_* flags

	Weapmodel int32 // weapon model index (for weapons)

	Info *GItemArmor
	Tag  int32

	Precaches string // string of all models, sounds, and images this item will use

	// index is ITEM_INDEX(item): the position in itemlist.
	index int32
}

// GameLocals is C game_locals_t: left intact through an entire game.
// C: game/g_local.h:268 game_locals_t
type GameLocals struct {
	Helpmessage1 string
	Helpmessage2 string
	Helpchanged  int32 // flash F1 icon if non 0, play sound and increment only if 1, 2, or 3

	// Clients is game.clients [maxclients]; saved separately by WriteGame.
	Clients []GClient `save:"-"`

	// can't store spawnpoint in level, because
	// it would get overwritten by the savegame restore
	Spawnpoint string // needed for coop respawns

	// store latched cvars here that we want to get at often
	Maxclients  int32
	Maxentities int32

	// cross level triggers
	Serverflags int32

	// items
	NumItems int32

	Autosaved bool
}

// LevelLocals is C level_locals_t: cleared as each map is entered.
// C: game/g_local.h:299 level_locals_t
type LevelLocals struct {
	Framenum int32
	Time     float32

	LevelName string // the descriptive name (Outer Base, etc)
	Mapname   string // the server name (base1, etc)
	Nextmap   string // go here when fraglimit is hit

	// intermission state
	Intermissiontime   float32 // time the intermission was started
	Changemap          string
	Exitintermission   int32
	IntermissionOrigin Vec3
	IntermissionAngle  Vec3

	SightClient *Edict // changed once each frame for coop games

	SightEntity          *Edict
	SightEntityFramenum  int32
	SoundEntity          *Edict
	SoundEntityFramenum  int32
	Sound2Entity         *Edict
	Sound2EntityFramenum int32

	PicHealth int32

	TotalSecrets int32
	FoundSecrets int32

	TotalGoals int32
	FoundGoals int32

	TotalMonsters  int32
	KilledMonsters int32

	CurrentEntity *Edict // entity running from G_RunFrame
	BodyQue       int32  // dead bodies

	PowerCubes int32 // ugly necessity for coop
}

// SpawnTemp is C spawn_temp_t: entity field values that can be set from the
// editor, but aren't actualy present in edict_t during gameplay.
// C: game/g_local.h:348 spawn_temp_t
type SpawnTemp struct {
	// world vars
	Sky       string
	Skyrotate float32
	Skyaxis   Vec3
	Nextmap   string

	Lip       int32
	Distance  int32
	Height    int32
	Noise     string
	Pausetime float32
	Item      string
	Gravity   string

	Minyaw   float32
	Maxyaw   float32
	Minpitch float32
	Maxpitch float32
}

// MoveInfo is C moveinfo_t.
// C: game/g_local.h:372 moveinfo_t
type MoveInfo struct {
	// fixed data
	StartOrigin Vec3
	StartAngles Vec3
	EndOrigin   Vec3
	EndAngles   Vec3

	SoundStart  int32
	SoundMiddle int32
	SoundEnd    int32

	Accel    float32
	Speed    float32
	Decel    float32
	Distance float32

	Wait float32

	// state data
	State             int32
	Dir               Vec3
	CurrentSpeed      float32
	MoveSpeed         float32
	NextSpeed         float32
	RemainingDistance float32
	DecelDistance     float32
	Endfunc           ThinkFn
}

// MFrame is C mframe_t.
// C: game/g_local.h:403 mframe_t
type MFrame struct {
	Aifunc    AIFn
	Dist      float32
	Thinkfunc ThinkFn
}

// MMove is C mmove_t. Tables are immutable package data registered by their
// C variable name (defMMove); monsterinfo.currentmove points at them.
// C: game/g_local.h:410 mmove_t
type MMove struct {
	Name       string
	Firstframe int32
	Lastframe  int32
	Frame      []MFrame
	Endfunc    ThinkFn
}

// MonsterInfo is C monsterinfo_t.
// C: game/g_local.h:418 monsterinfo_t
type MonsterInfo struct {
	Currentmove *MMove
	Aiflags     int32
	Nextframe   int32
	Scale       float32

	Stand       ThinkFn
	Idle        ThinkFn
	Search      ThinkFn
	Walk        ThinkFn
	Run         ThinkFn
	Dodge       DodgeFn
	Attack      ThinkFn
	Melee       ThinkFn
	Sight       BlockedFn
	Checkattack CheckAttackFn

	Pausetime      float32
	AttackFinished float32

	SavedGoal    Vec3
	SearchTime   float32
	TrailTime    float32
	LastSighting Vec3
	AttackState  int32
	Lefty        int32
	IdleTime     float32
	Linkcount    int32

	PowerArmorType  int32
	PowerArmorPower int32
}

// means of death
// C: game/g_local.h:468
const (
	MOD_UNKNOWN        = 0
	MOD_BLASTER        = 1
	MOD_SHOTGUN        = 2
	MOD_SSHOTGUN       = 3
	MOD_MACHINEGUN     = 4
	MOD_CHAINGUN       = 5
	MOD_GRENADE        = 6
	MOD_G_SPLASH       = 7
	MOD_ROCKET         = 8
	MOD_R_SPLASH       = 9
	MOD_HYPERBLASTER   = 10
	MOD_RAILGUN        = 11
	MOD_BFG_LASER      = 12
	MOD_BFG_BLAST      = 13
	MOD_BFG_EFFECT     = 14
	MOD_HANDGRENADE    = 15
	MOD_HG_SPLASH      = 16
	MOD_WATER          = 17
	MOD_SLIME          = 18
	MOD_LAVA           = 19
	MOD_CRUSH          = 20
	MOD_TELEFRAG       = 21
	MOD_FALLING        = 22
	MOD_SUICIDE        = 23
	MOD_HELD_GRENADE   = 24
	MOD_EXPLOSIVE      = 25
	MOD_BARREL         = 26
	MOD_BOMB           = 27
	MOD_EXIT           = 28
	MOD_SPLASH         = 29
	MOD_TARGET_LASER   = 30
	MOD_TRIGGER_HURT   = 31
	MOD_HIT            = 32
	MOD_TARGET_BLASTER = 33
	MOD_FRIENDLY_FIRE  = 0x8000000
)

// item spawnflags
// C: game/g_local.h:553
const (
	ITEM_TRIGGER_SPAWN = 0x00000001
	ITEM_NO_TOUCH      = 0x00000002
	// 6 bits reserved for editor flags
	// 8 bits used as power cube id bits for coop games
	DROPPED_ITEM        = 0x00010000
	DROPPED_PLAYER_ITEM = 0x00020000
	ITEM_TARGETS_USED   = 0x00040000
)

// fields are needed for spawning from the entity string
// C: game/g_local.h:567
const (
	FFL_SPAWNTEMP = 1
	FFL_NOSPAWN   = 2
)

// fieldtype_t
// C: game/g_local.h:570
const (
	F_INT       = 0
	F_FLOAT     = 1
	F_LSTRING   = 2 // string on disk, pointer in memory, TAG_LEVEL
	F_GSTRING   = 3 // string on disk, pointer in memory, TAG_GAME
	F_VECTOR    = 4
	F_ANGLEHACK = 5
	F_EDICT     = 6 // index on disk, pointer in memory
	F_ITEM      = 7 // index on disk, pointer in memory
	F_CLIENT    = 8 // index on disk, pointer in memory
	F_FUNCTION  = 9
	F_MMOVE     = 10
	F_IGNORE    = 11
)

// damage flags
// C: game/g_local.h:667
const (
	DAMAGE_RADIUS        = 0x00000001 // damage was indirect
	DAMAGE_NO_ARMOR      = 0x00000002 // armour does not protect from this damage
	DAMAGE_ENERGY        = 0x00000004 // damage is from an energy based weapon
	DAMAGE_NO_KNOCKBACK  = 0x00000008 // do not affect velocity, just view angles
	DAMAGE_BULLET        = 0x00000010 // damage is from a bullet (used for ricochets)
	DAMAGE_NO_PROTECTION = 0x00000020 // armor, shields, invulnerability, and godmode have no effect
)

// C: game/g_local.h:674
const (
	DEFAULT_BULLET_HSPREAD           = 300
	DEFAULT_BULLET_VSPREAD           = 500
	DEFAULT_SHOTGUN_HSPREAD          = 1000
	DEFAULT_SHOTGUN_VSPREAD          = 500
	DEFAULT_DEATHMATCH_SHOTGUN_COUNT = 12
	DEFAULT_SHOTGUN_COUNT            = 12
	DEFAULT_SSHOTGUN_COUNT           = 20
)

// client_t->anim_priority
// C: game/g_local.h:833
const (
	ANIM_BASIC   = 0 // stand / run
	ANIM_WAVE    = 1
	ANIM_JUMP    = 2
	ANIM_PAIN    = 3
	ANIM_ATTACK  = 4
	ANIM_DEATH   = 5
	ANIM_REVERSE = 6
)

// ClientPersistant is C client_persistant_t: client data that stays across
// multiple level loads.
// C: game/g_local.h:844 client_persistant_t
type ClientPersistant struct {
	Userinfo string
	Netname  string
	Hand     int32

	Connected bool // a loadgame will leave valid entities that just don't have a connection yet

	// values saved and restored from edicts when changing levels
	Health     int32
	MaxHealth  int32
	SavedFlags int32

	SelectedItem int32
	Inventory    [MAX_ITEMS]int32

	// ammo capacities
	MaxBullets  int32
	MaxShells   int32
	MaxRockets  int32
	MaxGrenades int32
	MaxCells    int32
	MaxSlugs    int32

	Weapon     *GItem
	Lastweapon *GItem

	PowerCubes int32 // used for tracking the cubes in coop games
	Score      int32 // for calculating total unit score in coop games

	GameHelpchanged int32
	Helpchanged     int32

	Spectator bool // client is a spectator
}

// ClientRespawn is C client_respawn_t: client data that stays across
// deathmatch respawns.
// C: game/g_local.h:880 client_respawn_t
type ClientRespawn struct {
	CoopRespawn ClientPersistant // what to set client->pers to on a respawn
	Enterframe  int32            // level.framenum the client entered the game
	Score       int32            // frags, etc
	CmdAngles   Vec3             // angles sent over in the last command

	Spectator bool // client is a spectator
}

// CPlane / CSurface as used by touch functions.
type (
	CPlane   = shared.CPlane
	CSurface = shared.CSurface
)
