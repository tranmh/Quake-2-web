package control

// Weapon is what a player knows about one of the player's weapons: the
// numbers the reflex layer needs to aim, to gate the trigger and to time a
// weapon switch. They are the C game's (game/p_weapon.c, game/g_weapon.c),
// rounded.
type Weapon struct {
	// Pickup is the weapon's pickup name ("use <pickup>").
	Pickup string
	// Speed is the projectile speed (units/s); 0 for hitscan weapons.
	Speed float32
	// Spread is the half-angle (degrees) the shot pattern covers around the
	// aim: the pellets and bullets of fire_lead (atan(spread / 8192)),
	// halved for the shotguns so that most of the pattern hits.
	Spread float32
	// Splash is the radius of the radius damage of its projectile (0:
	// none).
	Splash float32
	// Cycle is the time (ms) from one shot to the next: the fire frames of
	// Weapon_Generic at 10 Hz.
	Cycle int
	// Continuous weapons fire every frame the trigger is held.
	Continuous bool
}

// Weapons by pickup name. Speeds: fire_blaster 1000, fire_grenade 600,
// fire_rocket 650, fire_bfg 400. Spreads: fire_shotgun with
// DEFAULT_SHOTGUN_HSPREAD 500 (shotgun) and 1000 (super shotgun, two
// barrels 5 degrees apart), fire_bullet DEFAULT_BULLET_HSPREAD 300. Splash:
// fire_grenade 160, fire_rocket 120, fire_bfg 1000 (its ball's radius
// damage; the laser part is ignored). Cycles: Weapon_Generic's
// FRAME_FIRE_LAST - FRAME_ACTIVATE_LAST frames.
func weaponTable() [11]Weapon {
	return [...]Weapon{
		{Pickup: "Blaster", Speed: 1000, Cycle: 500},
		{Pickup: "Shotgun", Spread: 1.75, Cycle: 1100},
		{Pickup: "Super Shotgun", Spread: 4, Cycle: 1100},
		{Pickup: "Machinegun", Spread: 2, Cycle: 100, Continuous: true},
		{Pickup: "Chaingun", Spread: 2, Cycle: 100, Continuous: true},
		{Pickup: "Grenades", Speed: 600, Splash: 160, Cycle: 1100},
		{Pickup: "Grenade Launcher", Speed: 600, Splash: 160, Cycle: 1100},
		{Pickup: "Rocket Launcher", Speed: 650, Splash: 120, Cycle: 800},
		{Pickup: "HyperBlaster", Speed: 1000, Cycle: 100, Continuous: true},
		{Pickup: "Railgun", Cycle: 1500},
		{Pickup: "BFG10K", Speed: 400, Splash: 1000, Cycle: 2500},
	}
}

// WeaponByPickup returns the weapon with pickup name name (ok false for an
// unknown name: then the zero Weapon, a hitscan weapon without spread).
func WeaponByPickup(name string) (Weapon, bool) {
	for _, w := range weaponTable() {
		if w.Pickup == name {
			return w, true
		}
	}
	return Weapon{Pickup: name}, false
}

// Hitscan reports whether the weapon hits the instant it fires.
func (w Weapon) Hitscan() bool { return w.Speed <= 0 }

// HasSplash reports whether its projectile explodes with radius damage.
func (w Weapon) HasSplash() bool { return w.Splash > 0 }

// Committed reports whether a weapon switch at now would cut into the
// fire cycle of a shot fired at lastFire (ms; 0: never fired): the slow
// single-shot weapons with splash or a long cycle (rocket launcher,
// railgun, grenade launcher, BFG) finish their cycle first. The game
// itself only switches at the end of a fire animation; a "use" sent
// earlier is a wasted, confusing command.
func (w Weapon) Committed(now, lastFire int64) bool {
	if lastFire <= 0 || w.Continuous || w.Cycle < 800 || !(w.HasSplash() || w.Cycle >= 1500) {
		return false
	}
	return now-lastFire < int64(w.Cycle)
}
