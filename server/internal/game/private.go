package game

import "quake2web/server/internal/qcommon/shared"

// EdictPrivate holds the game-private part of edict_t (game/g_local.h:996 onward).
// Field names are the C names in mechanical CamelCase (see g_local.go).
// C: game/g_local.h:996 edict_s (private part)
type EdictPrivate struct {
	Movetype int32
	Flags    int32

	Model    string
	Freetime float32 // sv.time when the object was freed

	//
	// only used locally in game, not by server
	//
	Message    string
	Classname  string
	Spawnflags int32

	Timestamp float32

	Angle        float32 // set in qe3, -1 = up, -2 = down
	Target       string
	Targetname   string
	Killtarget   string
	Team         string
	Pathtarget   string
	Deathtarget  string
	Combattarget string
	TargetEnt    *Edict

	Speed, Accel, Decel float32
	Movedir             Vec3
	Pos1, Pos2          Vec3

	Velocity    Vec3
	Avelocity   Vec3
	Mass        int32
	AirFinished float32
	Gravity     float32 // per entity gravity multiplier (1.0 is normal)

	Goalentity *Edict
	Movetarget *Edict
	YawSpeed   float32
	IdealYaw   float32

	Nextthink float32
	Prethink  ThinkFn
	Think     ThinkFn
	Blocked   BlockedFn // move to moveinfo?
	Touch     TouchFn
	Use       UseFn
	Pain      PainFn
	Die       DieFn

	TouchDebounceTime    float32 // are all these legit?  do we need more/less of them?
	PainDebounceTime     float32
	DamageDebounceTime   float32
	FlySoundDebounceTime float32 // move to clientinfo
	LastMoveTime         float32

	Health      int32
	MaxHealth   int32
	GibHealth   int32
	Deadflag    int32
	ShowHostile int32

	PowerarmorTime float32

	Map string // target_changelevel

	Viewheight int32 // height above origin where eyesight is determined
	Takedamage int32
	Dmg        int32
	RadiusDmg  int32
	DmgRadius  float32
	Sounds     int32 // make this a spawntemp var?
	Count      int32

	Chain                 *Edict
	Enemy                 *Edict
	Oldenemy              *Edict
	Activator             *Edict
	Groundentity          *Edict
	GroundentityLinkcount int32
	Teamchain             *Edict
	Teammaster            *Edict

	Mynoise  *Edict // can go in client only
	Mynoise2 *Edict

	NoiseIndex  int32
	NoiseIndex2 int32
	Volume      float32
	Attenuation float32

	// timing variables
	Wait   float32
	Delay  float32 // before firing targets
	Random float32

	TeleportTime float32

	Watertype  int32
	Waterlevel int32

	MoveOrigin Vec3
	MoveAngles Vec3

	// move this to clientinfo?
	LightLevel int32

	Style int32 // also used as areaportal number

	Item *GItem // for bonus items

	// common data blocks
	Moveinfo    MoveInfo
	Monsterinfo MonsterInfo
}

// ClientPrivate holds the game-private part of gclient_t (game/g_local.h:883 onward).
// This structure is cleared on each PutClientInServer(), except for 'client->pers'.
// C: game/g_local.h:883 gclient_s (private part)
type ClientPrivate struct {
	Pers     ClientPersistant
	Resp     ClientRespawn
	OldPmove shared.PmoveState // for detecting out-of-pmove changes

	Showscores    bool // set layout stat
	Showinventory bool // set layout stat
	Showhelp      bool
	Showhelpicon  bool

	AmmoIndex int32

	Buttons        int32
	Oldbuttons     int32
	LatchedButtons int32

	WeaponThunk bool

	Newweapon *GItem

	// sum up damage over an entire frame, so
	// shotgun blasts give a single big kick
	DamageArmor     int32 // damage absorbed by armor
	DamageParmor    int32 // damage absorbed by power armor
	DamageBlood     int32 // damage taken out of health
	DamageKnockback int32 // impact damage
	DamageFrom      Vec3  // origin for vector calculation

	KillerYaw float32 // when dead, look at killer

	Weaponstate                   int32 // weaponstate_t
	KickAngles                    Vec3  // weapon kicks
	KickOrigin                    Vec3
	VDmgRoll, VDmgPitch, VDmgTime float32 // damage kicks
	FallTime, FallValue           float32 // for view drop on fall
	DamageAlpha                   float32
	BonusAlpha                    float32
	DamageBlend                   Vec3
	VAngle                        Vec3    // aiming direction
	Bobtime                       float32 // so off-ground doesn't change it
	Oldviewangles                 Vec3
	Oldvelocity                   Vec3

	NextDrownTime float32
	OldWaterlevel int32
	BreatherSound int32

	MachinegunShots int32 // for weapon raising

	// animation vars
	AnimEnd      int32
	AnimPriority int32
	AnimDuck     bool
	AnimRun      bool

	// powerup timers
	QuadFramenum       float32
	InvincibleFramenum float32
	BreatherFramenum   float32
	EnviroFramenum     float32

	GrenadeBlewUp bool
	GrenadeTime   float32
	SilencerShots int32
	WeaponSound   int32

	PickupMsgTime float32

	FloodLocktill float32     // locked from talking
	FloodWhen     [10]float32 // when messages were said
	FloodWhenhead int32       // head pointer for when said

	RespawnTime float32 // can respawn when time > this

	ChaseTarget *Edict // player we are chasing
	UpdateChase bool   // need to update chase info?
}
