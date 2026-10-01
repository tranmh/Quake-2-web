package perception

import (
	"strconv"
	"strings"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Kind is the broad category of an entity class.
type Kind uint8

// Entity kinds.
const (
	KindUnknown    Kind = iota
	KindMonster         // hostile monster
	KindNeutral         // misc_insane, misc_actor: people that do not attack first
	KindPlayer          // a player entity (model index 255)
	KindItem            // something to pick up
	KindProjectile      // blaster bolt, rocket, grenade, BFG ball, bomb
	KindGib             // gibs, severed heads, debris
	KindBarrel          // misc_explobox: explodes when shot
	KindDecor           // props: corpses, banners, ships, lights, effects
	KindBrush           // inline BSP model "*N": doors, plats, buttons, walls
	KindBeam            // RF_BEAM entity (target_laser)
	KindSpeaker         // no model: a sound or effect source
	KindViewModel       // first-person weapon model (never a world entity)
)

// String returns the kind's name.
func (k Kind) String() string {
	switch k {
	case KindUnknown:
		return "unknown"
	case KindMonster:
		return "monster"
	case KindNeutral:
		return "neutral"
	case KindPlayer:
		return "player"
	case KindItem:
		return "item"
	case KindProjectile:
		return "projectile"
	case KindGib:
		return "gib"
	case KindBarrel:
		return "barrel"
	case KindDecor:
		return "decor"
	case KindBrush:
		return "brush"
	case KindBeam:
		return "beam"
	case KindSpeaker:
		return "speaker"
	case KindViewModel:
		return "viewmodel"
	}
	return "kind" + strconv.Itoa(int(k))
}

// ItemKind is the category of an item class.
type ItemKind uint8

// Item kinds.
const (
	ItemNone ItemKind = iota
	ItemHealth
	ItemArmor
	ItemAmmo
	ItemWeapon
	ItemPowerup
	ItemKey
)

// String returns the item kind's name ("" for none).
func (i ItemKind) String() string {
	switch i {
	case ItemNone:
		return ""
	case ItemHealth:
		return "health"
	case ItemArmor:
		return "armor"
	case ItemAmmo:
		return "ammo"
	case ItemWeapon:
		return "weapon"
	case ItemPowerup:
		return "powerup"
	case ItemKey:
		return "key"
	}
	return "item" + strconv.Itoa(int(i))
}

// Weapon is the kind of an attack (a monster's or a projectile's).
type Weapon uint8

// Weapons.
const (
	WeaponNone Weapon = iota
	WeaponBlaster
	WeaponShotgun
	WeaponMachinegun
	WeaponChaingun
	WeaponGrenade
	WeaponRocket
	WeaponHyperblaster
	WeaponRailgun
	WeaponBFG
	WeaponMelee
	WeaponDrain // parasite tongue, medic cable
	WeaponOther
)

// String returns the weapon's name ("" for none).
func (w Weapon) String() string {
	switch w {
	case WeaponNone:
		return ""
	case WeaponBlaster:
		return "blaster"
	case WeaponShotgun:
		return "shotgun"
	case WeaponMachinegun:
		return "machinegun"
	case WeaponChaingun:
		return "chaingun"
	case WeaponGrenade:
		return "grenade"
	case WeaponRocket:
		return "rocket"
	case WeaponHyperblaster:
		return "hyperblaster"
	case WeaponRailgun:
		return "railgun"
	case WeaponBFG:
		return "bfg"
	case WeaponMelee:
		return "melee"
	case WeaponDrain:
		return "drain"
	case WeaponOther:
		return "other"
	}
	return "weapon" + strconv.Itoa(int(w))
}

// Hitscan reports whether the weapon hits the instant it fires.
func (w Weapon) Hitscan() bool {
	switch w {
	case WeaponShotgun, WeaponMachinegun, WeaponChaingun, WeaponRailgun, WeaponMelee, WeaponDrain:
		return true
	}
	return false
}

// Splash reports whether the weapon's projectile explodes with radius damage.
func (w Weapon) Splash() bool {
	return w == WeaponGrenade || w == WeaponRocket || w == WeaponBFG
}

// Class describes one kind of entity a player can recognize. All numbers are
// priors a player knows from playing the game (the game's spawn values at
// skill 1, rounded), never live game state.
type Class struct {
	Name  string // stable identifier, e.g. "soldier_ss", "item_health_large"
	Kind  Kind
	Model string // model path the class is recognized by ("" if none)
	// Classnames are the lump classnames this class can stand for (used to
	// tie lump entities to entity numbers).
	Classnames []string

	// Monster priors.
	Health   int     // spawn health
	DPS      float32 // sustained damage per second against the player
	Range    float32 // distance it attacks from
	Weapon   Weapon  // main attack
	Melee    bool    // also hits in melee
	Flying   bool
	Swimming bool
	Mins     Vec3 // standing bounding box
	Maxs     Vec3
	// NoWoundSkin: the skin number does not mean "wounded" (misc_insane
	// picks a random skin).
	NoWoundSkin bool

	// Item properties.
	Item   ItemKind
	Pickup string  // pickup name, as CS_ITEMS lists it
	Amount int     // health or armor points, ammo or weapon count
	Value  float32 // pickup value prior in 0..1
}

// Hostile reports whether the class attacks the player on sight.
func (c *Class) Hostile() bool { return c != nil && c.Kind == KindMonster }

// Classification is a Class applied to one entity state.
type Classification struct {
	Class *Class
	// Wounded is the pain skin (an odd skin number for most monsters):
	// below half health.
	Wounded bool
	// Model is the CS_MODELS path of the entity's model index.
	Model string
}

// ClassTable is the immutable set of known classes. It is safe for
// concurrent use; the *Class values it hands out are shared and must not be
// modified.
type ClassTable struct {
	classes []*Class
	byName  map[string]*Class
	byModel map[string]*Class // model path -> default class of that model
	// skin variants: model path -> classes by (skin &^ 1) / 2
	skinVariants map[string][]*Class
}

// NewClassTable builds the class table.
func NewClassTable() *ClassTable {
	t := &ClassTable{byName: map[string]*Class{}, byModel: map[string]*Class{}, skinVariants: map[string][]*Class{}}
	for _, c := range classDefs() {
		c := c
		t.classes = append(t.classes, &c)
		if t.byName[c.Name] != nil {
			panic("perception: duplicate class " + c.Name)
		}
		t.byName[c.Name] = &c
	}
	for _, c := range t.classes {
		if c.Model == "" {
			continue
		}
		if _, dup := t.byModel[c.Model]; !dup {
			t.byModel[c.Model] = c
		}
	}
	// soldier: skin 0 light (blaster), 2 shotgun, 4 machinegun (+1 wounded)
	// C: game/m_soldier.c SP_monster_soldier_light/_soldier/_ss
	t.skinVariants[modelSoldier] = []*Class{t.byName["soldier_light"], t.byName["soldier"], t.byName["soldier_ss"]}
	// tank: skin 2 is the commander (+1 wounded)
	// C: game/m_tank.c SP_monster_tank
	t.skinVariants[modelTank] = []*Class{t.byName["tank"], t.byName["tank_commander"]}
	return t
}

// ByName returns the class with the given name (nil if none).
func (t *ClassTable) ByName(name string) *Class { return t.byName[name] }

// ByModel returns the default class of a model path (nil if unknown).
func (t *ClassTable) ByModel(path string) *Class { return t.byModel[path] }

// Classes returns all classes in table order.
func (t *ClassTable) Classes() []*Class { return append([]*Class(nil), t.classes...) }

// ForClassname returns the classes that can stand for a lump classname.
func (t *ClassTable) ForClassname(classname string) []*Class {
	var out []*Class
	for _, c := range t.classes {
		for _, cn := range c.Classnames {
			if cn == classname {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// lookup classifies a model path with the entity's skin and effects.
func (t *ClassTable) lookup(path string, skin int32, effects uint32) (*Class, bool) {
	if v, ok := t.skinVariants[path]; ok {
		i := int((skin &^ 1) / 2)
		if i < 0 || i >= len(v) {
			i = 0
		}
		return v[i], skin&1 != 0
	}
	c := t.byModel[path]
	if c == nil {
		return t.fallback(path, effects), false
	}
	if path == modelLaser && effects&q2const.EF_HYPERBLASTER != 0 {
		c = t.byName["hyperblaster_bolt"]
	}
	wounded := (c.Kind == KindMonster || c.Kind == KindNeutral) && !c.NoWoundSkin && skin&1 != 0
	return c, wounded
}

// fallback names models the table does not list by their directory.
func (t *ClassTable) fallback(path string, effects uint32) *Class {
	switch {
	case effects&q2const.EF_GIB != 0 || strings.HasPrefix(path, "models/objects/gibs/"):
		return t.byName["gib"]
	case effects&(q2const.EF_ROCKET|q2const.EF_BLASTER|q2const.EF_HYPERBLASTER|q2const.EF_GRENADE|q2const.EF_BFG) != 0:
		return t.byName["projectile"]
	case strings.HasPrefix(path, "models/monsters/"):
		return t.byName["monster"]
	case strings.HasPrefix(path, "models/items/"):
		return t.byName["item"]
	}
	return t.byName["unknown"]
}

// Classifier classifies the entities of one level: it resolves model
// indexes through the level's configstrings (CS_MODELS) and pickup names
// through CS_ITEMS. It is not safe for concurrent use.
type Classifier struct {
	t      *ClassTable
	cs     *ConfigStrings
	models [q2const.MAX_MODELS]modelEntry
	items  map[string]int
}

type modelEntry struct {
	path  string
	brush int // inline model number for "*N", else 0
}

// NewClassifier returns a classifier using table t. Call Update with the
// level's configstrings before Classify.
func NewClassifier(t *ClassTable) *Classifier { return &Classifier{t: t} }

// Table returns the classifier's class table.
func (c *Classifier) Table() *ClassTable { return c.t }

// Update re-resolves the model and item names when cs is a new copy.
func (c *Classifier) Update(cs *ConfigStrings) {
	if cs == nil || cs == c.cs {
		return
	}
	c.cs = cs
	for i := range c.models {
		p := cs[q2const.CS_MODELS+i]
		c.models[i] = modelEntry{path: p, brush: inlineNumber(p)}
	}
	c.items = map[string]int{}
	for i := 1; i < q2const.MAX_ITEMS; i++ {
		if n := cs[q2const.CS_ITEMS+i]; n != "" {
			if _, dup := c.items[n]; !dup {
				c.items[n] = i
			}
		}
	}
}

// inlineNumber returns N of a "*N" model name (0 if not one).
func inlineNumber(p string) int {
	if len(p) < 2 || p[0] != '*' {
		return 0
	}
	n, err := strconv.Atoi(p[1:])
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// ModelPath returns the CS_MODELS path of a model index ("" if unset).
func (c *Classifier) ModelPath(index int32) string {
	if index < 0 || int(index) >= len(c.models) {
		return ""
	}
	return c.models[index].path
}

// InlineModel returns N when model index is the brush model "*N".
func (c *Classifier) InlineModel(index int32) int {
	if index < 0 || int(index) >= len(c.models) {
		return 0
	}
	return c.models[index].brush
}

// ItemIndex returns the inventory index of a pickup name from CS_ITEMS (0 if
// the level does not list it).
func (c *Classifier) ItemIndex(pickup string) int { return c.items[pickup] }

// ItemName returns the pickup name of inventory index i ("" if unset).
func (c *Classifier) ItemName(i int) string {
	if c.cs == nil || i <= 0 || i >= q2const.MAX_ITEMS {
		return ""
	}
	return c.cs[q2const.CS_ITEMS+i]
}

// Classify names the entity s.
func (c *Classifier) Classify(s *shared.EntityState) Classification {
	switch {
	case s.RenderFX&q2const.RF_BEAM != 0:
		return Classification{Class: c.t.byName["laser_beam"]}
	case s.ModelIndex == 255:
		return Classification{Class: c.t.byName["player"]}
	case s.ModelIndex == 0:
		if s.Sound != 0 {
			return Classification{Class: c.t.byName["speaker"]}
		}
		return Classification{Class: c.t.byName["effect"]}
	}
	path := c.ModelPath(s.ModelIndex)
	if c.InlineModel(s.ModelIndex) != 0 {
		return Classification{Class: c.t.byName["brush"], Model: path}
	}
	cl, wounded := c.t.lookup(path, s.SkinNum, s.Effects)
	return Classification{Class: cl, Wounded: wounded, Model: path}
}

// DecodeSolid returns the bounding box the server packed into
// entity_state_t.solid for a SOLID_BBOX entity (the client's prediction
// decoding, CL_ClipMoveToEntities): x and y are symmetric. ok is false for
// 0 (not solid, or a dead monster) and 31 (a brush model).
// C: client/cl_pred.c:121 CL_ClipMoveToEntities, server/sv_world.c:165
func DecodeSolid(solid int32) (mins, maxs Vec3, ok bool) {
	if solid == 0 || solid == 31 {
		return Vec3{}, Vec3{}, false
	}
	x := float32(8 * (solid & 31))
	zd := float32(8 * ((solid >> 5) & 31))
	zu := float32(8*((solid>>10)&63) - 32)
	return Vec3{-x, -x, -zd}, Vec3{x, x, zu}, true
}
