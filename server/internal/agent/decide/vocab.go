package decide

// Question ids. The option keys below are stable vocabulary: traces,
// fixtures and the scripted policy depend on them.
const (
	QTarget     = "target"
	QFirePolicy = "fire_policy"
	QMovement   = "movement"
	QMode       = "mode"
	QWeapon     = "weapon"
	QPickup     = "pickup"
	QDanger     = "danger"
)

// Option keys shared by several questions.
const (
	OptNone = "none" // target, pickup: nothing
	OptKeep = "keep" // weapon: keep the current one
)

// Mode is what the bot focuses on (question mode).
type Mode string

// Modes.
const (
	ModeFight     Mode = "fight"
	ModeObjective Mode = "objective"
	ModePickup    Mode = "pickup"
	ModeRetreat   Mode = "retreat"
	ModeExplore   Mode = "explore"
)

// FirePolicy is when the reflex layer pulls the trigger (question
// fire_policy).
type FirePolicy string

// Fire policies.
const (
	FireHold        FirePolicy = "hold"
	FireWhenAligned FirePolicy = "fire_when_aligned"
	FireSuppress    FirePolicy = "suppress"
)

// Movement is how the bot moves relative to its target or threat
// (question movement).
type Movement string

// Movements.
const (
	MoveAdvance     Movement = "advance"
	MoveRetreat     Movement = "retreat"
	MoveStrafeLeft  Movement = "strafe_left"
	MoveStrafeRight Movement = "strafe_right"
	MoveHold        Movement = "hold"
)

// Danger levels (question danger, a score over these, 0..4).
const (
	DangerSafe = iota
	DangerLow
	DangerModerate
	DangerHigh
	DangerCritical
)

// WeaponKey is a weapon's vocabulary key ("" keeps the current weapon).
type WeaponKey string

// Weapons, in inventory order.
const (
	WeaponKeep            WeaponKey = ""
	WeaponBlaster         WeaponKey = "blaster"
	WeaponShotgun         WeaponKey = "shotgun"
	WeaponSuperShotgun    WeaponKey = "super_shotgun"
	WeaponMachinegun      WeaponKey = "machinegun"
	WeaponChaingun        WeaponKey = "chaingun"
	WeaponGrenades        WeaponKey = "grenades"
	WeaponGrenadeLauncher WeaponKey = "grenade_launcher"
	WeaponRocketLauncher  WeaponKey = "rocket_launcher"
	WeaponHyperBlaster    WeaponKey = "hyperblaster"
	WeaponRailgun         WeaponKey = "railgun"
	WeaponBFG             WeaponKey = "bfg"
)

// weaponSpec is what a player knows about a weapon (C: game/g_items.c
// itemlist, game/p_weapon.c ammo use).
type weaponSpec struct {
	pickup string // pickup name ("use <pickup>")
	ammo   string // ammo pickup name ("" for the blaster)
	perUse int    // ammo per shot
	low    int    // below this much ammo is "low"
}

func weaponInfo(k WeaponKey) (weaponSpec, bool) {
	switch k {
	case WeaponBlaster:
		return weaponSpec{"Blaster", "", 0, 0}, true
	case WeaponShotgun:
		return weaponSpec{"Shotgun", "Shells", 1, 10}, true
	case WeaponSuperShotgun:
		return weaponSpec{"Super Shotgun", "Shells", 2, 10}, true
	case WeaponMachinegun:
		return weaponSpec{"Machinegun", "Bullets", 1, 40}, true
	case WeaponChaingun:
		return weaponSpec{"Chaingun", "Bullets", 1, 60}, true
	case WeaponGrenades:
		return weaponSpec{"Grenades", "Grenades", 1, 5}, true
	case WeaponGrenadeLauncher:
		return weaponSpec{"Grenade Launcher", "Grenades", 1, 5}, true
	case WeaponRocketLauncher:
		return weaponSpec{"Rocket Launcher", "Rockets", 1, 5}, true
	case WeaponHyperBlaster:
		return weaponSpec{"HyperBlaster", "Cells", 1, 40}, true
	case WeaponRailgun:
		return weaponSpec{"Railgun", "Slugs", 1, 5}, true
	case WeaponBFG:
		return weaponSpec{"BFG10K", "Cells", 50, 100}, true
	}
	return weaponSpec{}, false
}

// Weapons returns the weapon keys in inventory order (without keep).
func Weapons() []WeaponKey {
	return []WeaponKey{WeaponBlaster, WeaponShotgun, WeaponSuperShotgun, WeaponMachinegun, WeaponChaingun,
		WeaponGrenades, WeaponGrenadeLauncher, WeaponRocketLauncher, WeaponHyperBlaster, WeaponRailgun, WeaponBFG}
}

// Pickup returns the weapon's pickup name, for "use <pickup>" ("" for keep
// or an unknown key).
func (k WeaponKey) Pickup() string {
	s, _ := weaponInfo(k)
	return s.pickup
}

// AmmoName returns the pickup name of the weapon's ammo ("" for the
// blaster).
func (k WeaponKey) AmmoName() string {
	s, _ := weaponInfo(k)
	return s.ammo
}

// AmmoPerShot returns the ammo one shot uses.
func (k WeaponKey) AmmoPerShot() int {
	s, _ := weaponInfo(k)
	return s.perUse
}

// LowAmmo returns the ammo count below which the weapon's ammo is "low".
func (k WeaponKey) LowAmmo() int {
	s, _ := weaponInfo(k)
	return s.low
}

// Known reports whether k is a weapon key.
func (k WeaponKey) Known() bool {
	_, ok := weaponInfo(k)
	return ok
}

// WeaponFromPickup returns the key of a weapon pickup name ("Shotgun" →
// shotgun; "" if not a weapon).
func WeaponFromPickup(name string) WeaponKey {
	for _, k := range Weapons() {
		if k.Pickup() == name {
			return k
		}
	}
	return ""
}

// optionKey is the option key of a weapon (keep for "").
func (k WeaponKey) optionKey() string {
	if k == WeaponKeep {
		return OptKeep
	}
	return string(k)
}
