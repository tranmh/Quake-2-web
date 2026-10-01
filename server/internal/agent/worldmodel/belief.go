package worldmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"quake2web/server/internal/agent/perception"
)

// Vec3 is the game's vec3_t.
type Vec3 = perception.Vec3

// LifeState is what the bot believes about a track's existence. Items use
// LifeAlive for "present" and LifeTaken once picked up.
type LifeState uint8

// Life states, in the order a monster goes through them.
const (
	LifeAlive  LifeState = iota // alive, or (item) present
	LifeDying                   // death animation or death cry seen/heard
	LifeDead                    // a corpse
	LifeGibbed                  // blown to pieces
	LifeGone                    // vanished where it should be visible
	LifeTaken                   // an item that was picked up
)

// String returns the state's name.
func (l LifeState) String() string {
	switch l {
	case LifeAlive:
		return "alive"
	case LifeDying:
		return "dying"
	case LifeDead:
		return "dead"
	case LifeGibbed:
		return "gibbed"
	case LifeGone:
		return "gone"
	case LifeTaken:
		return "taken"
	}
	return "life" + strconv.Itoa(int(l))
}

// MarshalText encodes the state by name.
func (l LifeState) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// Dead reports a state a monster does not come back from by itself.
func (l LifeState) Dead() bool { return l != LifeAlive }

// Awareness is how much a monster seems to know about the bot.
type Awareness uint8

// Awareness levels.
const (
	Idle      Awareness = iota // standing or walking about
	Alert                      // has noticed someone (sight cry, running, pain)
	Attacking                  // firing or attacking (muzzle flash, attack animation)
)

// String returns the level's name.
func (a Awareness) String() string {
	switch a {
	case Idle:
		return "idle"
	case Alert:
		return "alert"
	case Attacking:
		return "attacking"
	}
	return "awareness" + strconv.Itoa(int(a))
}

// MarshalText encodes the level by name.
func (a Awareness) MarshalText() ([]byte, error) { return []byte(a.String()), nil }

// Track is a believed monster, player, neutral or barrel.
type Track struct {
	ID    string // "e3": stable for the level, independent of entity numbers
	Num   int32  // entity number of the last admitted observation
	Class string // perception class name, or a monster family when only heard
	Kind  string // perception kind name
	Lump  int    // matched entity lump index, -1 if unknown

	Pos      Vec3 // origin when last observed
	PosKnown bool // false: only heard, without a position
	Vel      Vec3 // velocity estimate (EMA of observed displacements)
	Yaw      float32
	Mins     Vec3
	Maxs     Vec3
	Dist     float32 // from the eye, at the last observation

	Visible   bool // in view this frame
	Heard     bool // heard this frame
	Shootable bool // a shot reached it when last seen
	// Missing: its last position is in view but it is not there.
	Missing    bool
	FirstSeen  int64
	LastSeen   int64 // last time in view (0: never seen)
	LastHeard  int64
	LastUpdate int64 // last admitted observation of any kind
	// Confidence decays with exp(-age/τ) after the last observation.
	Confidence float32

	Life      LifeState
	LifeAt    int64 // when Life last changed
	Revived   int   // medic revivals seen
	Awareness Awareness
	// LastAttack is the last muzzle flash or attack animation; Weapon the
	// attack's weapon kind.
	LastAttack int64
	Weapon     string
	Anim       string
	Wounded    bool
	// Threat estimates the damage per second it deals the bot now.
	Threat float32
}

// Item is a believed pickup.
type Item struct {
	ID       string // "i2"
	Num      int32
	Class    string
	Kind     string // perception item kind: health, armor, ammo, weapon, powerup, key
	Pickup   string // pickup name
	Index    int    // inventory index of the pickup name (CS_ITEMS), 0 if unknown
	Lump     int
	Pos      Vec3
	Visible  bool
	Value    float32
	Amount   int
	Life     LifeState // LifeAlive (present) or LifeTaken / LifeGone
	LifeAt   int64
	LastSeen int64
	// Remembered: known from an earlier attempt at this level (the level
	// was reloaded), not seen in this attempt yet.
	Remembered bool
}

// Projectile is a visible projectile.
type Projectile struct {
	ID     string // "p7"
	Num    int32
	Class  string
	Weapon string
	Pos    Vec3
	Vel    Vec3
	// Own: fired by the bot (the server clears solid for the shooter's own
	// missiles; or it appeared at the eye right after a shot).
	Own       bool
	FirstSeen int64
	LastSeen  int64
	// TCA is the time to the closest approach to the bot's box center in
	// seconds (negative: moving away) and Miss the distance then.
	TCA  float32
	Miss float32
	// Danger: it will pass within its damage radius soon.
	Danger bool
	// DodgeDir is the horizontal unit direction that increases the miss
	// distance most and DodgeSide its sign along the view's right vector
	// (+1 strafe right, -1 left, 0 none).
	DodgeDir  Vec3
	DodgeSide int
}

// Mover is a believed brush entity pose (door, plat, button, train).
type Mover struct {
	Model  string // "*N"
	Num    int32
	Lump   int
	Origin Vec3
	Angles Vec3
	// Visible / Heard this frame; LastUpdate the last admitted pose.
	Visible    bool
	Heard      bool
	LastUpdate int64
	Moving     bool // the pose changed between admitted observations
	// Moved: the pose differs from the spawn pose (a door that opened).
	Moved bool
	// Stale: no admitted pose for MoverTimeout.
	Stale bool
}

// LaserState is what the bot knows about a laser.
type LaserState uint8

// Laser states.
const (
	LaserUnknown LaserState = iota
	LaserOn
	LaserOff
)

// String returns the state's name.
func (s LaserState) String() string {
	switch s {
	case LaserOn:
		return "on"
	case LaserOff:
		return "off"
	}
	return "unknown"
}

// MarshalText encodes the state by name.
func (s LaserState) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Laser is a believed target_laser.
type Laser struct {
	Lump       int // -1 if not matched
	Num        int32
	Start, End Vec3
	State      LaserState
	LastUpdate int64
}

// EffectKind names an observed effect of the level's logic.
type EffectKind string

// Effects that confirm route prerequisites.
const (
	EffectLaserOff    EffectKind = "laser_off"
	EffectLaserOn     EffectKind = "laser_on"
	EffectMoverMoved  EffectKind = "mover_moved"
	EffectMonsterDead EffectKind = "monster_dead"
	EffectItemTaken   EffectKind = "item_taken"
)

// Effect is the first observation of an effect this attempt.
type Effect struct {
	Kind  EffectKind
	Lump  int    // lump index of the entity, -1 if unknown
	Ref   string // track/item ID or mover model
	At    int64
	Frame int32
}

// DamageEvent is damage the bot took in one frame.
type DamageEvent struct {
	At     int64
	Frame  int32
	Health int // health points lost
	Armor  int // armor points lost
	// Cause: "hit", "fall", "lava", "slime", "drown" or "unknown".
	Cause string
	// BearingKnown: the direction was recovered from the view kick.
	// Bearing is the world yaw (degrees) from the bot towards the source,
	// Relative the same relative to the view yaw (positive: to the left).
	BearingKnown bool
	Bearing      float32
	Relative     float32
	// Source is the track the damage is attributed to ("" if none).
	Source string
}

// SoundEvent is a recently heard sound.
type SoundEvent struct {
	At       int64
	Kind     string
	Path     string
	Family   string
	Num      int32
	Pos      Vec3
	PosKnown bool
	Track    string // the track it was attributed to
}

// InvItem is one inventory entry.
type InvItem struct {
	Index int
	Name  string
	Count int
}

// Inventory is the last inventory the server sent (svc_inventory).
type Inventory struct {
	Known bool
	Items []InvItem // non-zero counts by index
	Seq   uint64    // svc_inventory messages seen
	At    int64     // when it arrived
	// Stale: something was picked up or used since it arrived.
	Stale bool
}

// Count returns the count of a pickup name (0 if unknown).
func (inv *Inventory) Count(name string) int {
	for _, it := range inv.Items {
		if it.Name == name {
			return it.Count
		}
	}
	return 0
}

// Self is the bot's own state.
type Self struct {
	Origin     Vec3
	Velocity   Vec3
	Eye        Vec3
	ViewAngles Vec3
	Mins, Maxs Vec3
	OnGround   bool
	Ducked     bool
	PmType     int32
	Health     int
	Armor      int
	Ammo       int
	Weapon     string // pickup name of the current weapon ("" if none)
	Fov        float32
	// Waterlevel 0..3 like pmove; InLava/InSlime from the contents.
	Waterlevel   int
	InLava       bool
	InSlime      bool
	Dead         bool
	Intermission bool
	Layouts      int // STAT_LAYOUTS
	// LastFired is the last own muzzle flash.
	LastFired int64
	// Pickup is the last pickup message (STAT_PICKUP_STRING) and PickupAt
	// when it appeared.
	Pickup   string
	PickupAt int64
	// HelpBlink: the help icon blinks (new objectives in the help computer).
	HelpBlink  bool
	Timer      int
	TimerIcon  string
	LastDamage int64
	InCombat   bool
}

// Message is a text the server printed.
type Message struct {
	At     int64
	Center bool
	Text   string
}

// MemoryView is the static knowledge LevelMemory keeps for the level.
type MemoryView struct {
	Entries    int
	DeathSpots []Vec3
	Blocked    []string
}

// Belief is the world model's state: what the bot believes now. Slices are
// sorted deterministically (by ID, model, number or time).
type Belief struct {
	Level       LevelKey
	Map         string
	Time        int64 // the caller's clock (ms)
	ServerFrame int32
	Frames      int // frames processed since the level started
	Self        Self
	Tracks      []Track
	Items       []Item
	Projectiles []Projectile
	Movers      []Mover
	Lasers      []Laser
	Effects     []Effect
	Damage      []DamageEvent // the last DamageWindow, oldest first
	Sounds      []SoundEvent  // the last SoundWindow, oldest first
	Messages    []Message     // the last MessageLimit messages
	Inventory   Inventory
	Help        perception.Help
	HelpKnown   bool
	HelpAt      int64
	Memory      MemoryView
}

// Track returns the track with id (nil if none).
func (b *Belief) Track(id string) *Track {
	for i := range b.Tracks {
		if b.Tracks[i].ID == id {
			return &b.Tracks[i]
		}
	}
	return nil
}

// TrackByLump returns the track matched to lump entity i (nil if none).
func (b *Belief) TrackByLump(i int) *Track {
	for k := range b.Tracks {
		if b.Tracks[k].Lump == i {
			return &b.Tracks[k]
		}
	}
	return nil
}

// Laser returns the laser of lump entity i (nil if unknown).
func (b *Belief) Laser(i int) *Laser {
	for k := range b.Lasers {
		if b.Lasers[k].Lump == i {
			return &b.Lasers[k]
		}
	}
	return nil
}

// Mover returns the mover with model "*N" (nil if never observed).
func (b *Belief) Mover(model string) *Mover {
	for k := range b.Movers {
		if b.Movers[k].Model == model {
			return &b.Movers[k]
		}
	}
	return nil
}

// Effect returns the first effect of kind on lump entity i (nil if none).
func (b *Belief) Effect(kind EffectKind, lump int) *Effect {
	for k := range b.Effects {
		if b.Effects[k].Kind == kind && b.Effects[k].Lump == lump {
			return &b.Effects[k]
		}
	}
	return nil
}

// Clone returns a deep copy.
func (b *Belief) Clone() Belief {
	c := *b
	c.Tracks = append([]Track(nil), b.Tracks...)
	c.Items = append([]Item(nil), b.Items...)
	c.Projectiles = append([]Projectile(nil), b.Projectiles...)
	c.Movers = append([]Mover(nil), b.Movers...)
	c.Lasers = append([]Laser(nil), b.Lasers...)
	c.Effects = append([]Effect(nil), b.Effects...)
	c.Damage = append([]DamageEvent(nil), b.Damage...)
	c.Sounds = append([]SoundEvent(nil), b.Sounds...)
	c.Messages = append([]Message(nil), b.Messages...)
	c.Inventory.Items = append([]InvItem(nil), b.Inventory.Items...)
	c.Memory.DeathSpots = append([]Vec3(nil), b.Memory.DeathSpots...)
	c.Memory.Blocked = append([]string(nil), b.Memory.Blocked...)
	return c
}

// Digest returns a hash of the belief's JSON encoding (32 hex characters):
// equal beliefs have equal digests.
func (b *Belief) Digest() string {
	data, err := json.Marshal(b)
	if err != nil {
		panic("worldmodel: belief not encodable: " + err.Error())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}
