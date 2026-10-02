package bot

import (
	"math"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/worldmodel"
)

// Shooter is the minimal shoot executor of phase 4: it picks the best
// weapon the bot owns with ammo for the range ("use <pickup name>",
// debounced and verified by the view weapon), aims at the target's box
// centre with a rate-capped slew (leading it for projectile weapons) and
// holds the trigger only while the target is in view with a line of fire
// and the aim is within tolerance. It reads only the belief. A Shooter is
// not safe for concurrent use.
type Shooter struct {
	Slew control.Slew

	yaw, pitch float32
	aimed      bool // yaw/pitch hold the last commanded aim

	// useAt is when "use" was last sent (sent: ever), useFor the weapon
	// being switched to and useFirst when its first "use" went out (the
	// switch is judged from then, whatever the resends).
	useAt    int64
	sent     bool
	useFor   decide.WeaponKey
	useFirst int64
	refused  map[decide.WeaponKey]int64
	switches int
	// lastFire is when the shooter's trigger was last held (ms; 0: never).
	lastFire int64
}

// NoteFire records a command fired at now (ms).
func (s *Shooter) NoteFire(now int64) { s.lastFire = now }

// Weapon switching timing (ms).
const (
	// useDebounce is the least time between two "use" commands.
	useDebounce = 1500
	// useVerify is how long a switch may take before the weapon counts
	// as unavailable for refuseFor (the inventory was stale, or it has
	// no ammo after all).
	useVerify = 3000
	refuseFor = 15000
)

// weaponSpeed returns the projectile speed of a weapon (units/s; 0 for
// hitscan): C game/p_weapon.c fire_blaster 1000, fire_rocket 650,
// fire_grenade 600, fire_bfg 400.
func weaponSpeed(k decide.WeaponKey) float32 {
	switch k {
	case decide.WeaponBlaster, decide.WeaponHyperBlaster:
		return 1000
	case decide.WeaponRocketLauncher:
		return 650
	case decide.WeaponGrenadeLauncher, decide.WeaponGrenades:
		return 600
	case decide.WeaponBFG:
		return 400
	}
	return 0
}

// weaponScore ranks a weapon for a target at distance d (0: do not use).
// Splash weapons are kept off targets close by, grenades (which arc) and
// the BFG are never picked.
func weaponScore(k decide.WeaponKey, d float32) float32 {
	switch k {
	case decide.WeaponChaingun:
		return 9
	case decide.WeaponHyperBlaster:
		return 8
	case decide.WeaponRailgun:
		return 7.5
	case decide.WeaponSuperShotgun:
		if d < 400 {
			return 8.5
		}
		return 5
	case decide.WeaponMachinegun:
		return 7
	case decide.WeaponRocketLauncher:
		if d > 250 {
			return 7.8
		}
		return 0
	case decide.WeaponShotgun:
		return 5
	case decide.WeaponBlaster:
		return 1
	}
	return 0
}

// usable reports whether the bot may fire weapon k by its belief: the
// blaster always, the current weapon while it has ammo (STAT_AMMO is live:
// it wins over an inventory listed before the ammo ran out), others when
// the inventory lists them with ammo for a shot.
func usable(b *worldmodel.Belief, k decide.WeaponKey) bool {
	if k == decide.WeaponBlaster {
		return true
	}
	if b.Self.Weapon == k.Pickup() {
		return b.Self.Ammo >= max(1, k.AmmoPerShot())
	}
	inv := &b.Inventory
	return inv.Known && inv.Count(k.Pickup()) > 0 && inv.Count(k.AmmoName()) >= max(1, k.AmmoPerShot())
}

// Choose returns the weapon to fight with at distance d: pref when the bot
// can use it, else the best usable one; the current weapon is kept unless
// another scores clearly better.
func (s *Shooter) Choose(now int64, b *worldmodel.Belief, d float32, pref decide.WeaponKey) decide.WeaponKey {
	ok := func(k decide.WeaponKey) bool { return !s.Refused(now, k) && usable(b, k) }
	if pref != decide.WeaponKeep && pref.Known() && ok(pref) {
		return pref
	}
	cur := decide.WeaponFromPickup(b.Self.Weapon)
	best, bs := decide.WeaponBlaster, float32(-1)
	for _, k := range decide.Weapons() {
		if sc := weaponScore(k, d); sc > 0 && sc > bs && ok(k) {
			best, bs = k, sc
		}
	}
	if cur != "" && ok(cur) && weaponScore(cur, d) > 0 && weaponScore(cur, d) >= bs-0.6 {
		return cur
	}
	return best
}

// Switch returns the "use <pickup>" command to send now for weapon k ("" if
// none): when the view weapon is another one, at most every useDebounce
// (call it every frame), and never in the middle of the fire cycle of a
// rocket, grenade, rail or BFG shot (control.Weapon.Committed, from the
// shooter's own last shot, NoteFire). A switch that did not show in the
// view weapon within useVerify of its first "use" makes k unavailable
// (Choose skips it, Switch sends nothing for it) for refuseFor.
func (s *Shooter) Switch(now int64, b *worldmodel.Belief, k decide.WeaponKey) string {
	if k == "" || b.Self.Weapon == k.Pickup() {
		s.useFor = ""
		return ""
	}
	if until, bad := s.refused[k]; bad && now < until {
		return ""
	}
	if w, _ := control.WeaponByPickup(b.Self.Weapon); w.Committed(now, s.lastFire) {
		return ""
	}
	if s.useFor == k && now-s.useFirst >= useVerify {
		if s.refused == nil {
			s.refused = map[decide.WeaponKey]int64{}
		}
		s.refused[k] = now + refuseFor
		s.useFor = ""
		return ""
	}
	if s.sent && now-s.useAt < useDebounce {
		return ""
	}
	if s.useFor != k {
		s.useFor, s.useFirst = k, now
	}
	s.useAt, s.sent = now, true
	s.switches++
	return "use " + k.Pickup()
}

// Observe notes the frame's view weapon (call it every frame, before
// Switch): a switch that shows in it is done, so that a later switch back
// to that weapon (after the game changed the weapon by itself: a pickup's
// auto-switch, an empty gun) is judged afresh.
func (s *Shooter) Observe(b *worldmodel.Belief) {
	if s.useFor != "" && b.Self.Weapon == s.useFor.Pickup() {
		s.useFor = ""
	}
}

// HoldOff reports whether the trigger must stay released for the switch
// in progress: the game drops a weapon only out of its firing state (C
// game/p_weapon.c Weapon_Generic), and a continuous weapon (machinegun,
// chaingun, hyperblaster) stays in it for as long as the trigger is held.
func (s *Shooter) HoldOff(b *worldmodel.Belief) bool {
	if s.useFor == "" || b.Self.Weapon == s.useFor.Pickup() {
		return false
	}
	w, ok := control.WeaponByPickup(b.Self.Weapon)
	return ok && w.Continuous
}

// ForgetSwitch forgets the switch in progress and the weapons refused (a
// level entry or a reload: the game restored another weapon and
// inventory).
func (s *Shooter) ForgetSwitch() { s.useFor, s.useFirst, s.refused = "", 0, nil }

// Refused reports whether weapon k is unavailable at now because a switch
// to it did not take.
func (s *Shooter) Refused(now int64, k decide.WeaponKey) bool {
	until, bad := s.refused[k]
	return bad && now < until
}

// Switches returns how many "use" commands were sent.
func (s *Shooter) Switches() int { return s.switches }

// Reset forgets the aim (a new level, a teleport, the view taken over).
func (s *Shooter) Reset() { s.aimed = false }

// SetView makes the shooter's aim the given view (when something else
// set the view this command).
func (s *Shooter) SetView(yaw, pitch float32) { s.yaw, s.pitch, s.aimed = yaw, pitch, true }

// AimTarget is what a command aims at.
type AimTarget struct {
	// Point is the aim point (the target's box centre, already led);
	// Radius its size for the aim tolerance.
	Point  Vec3
	Radius float32
	// Fire allows pulling the trigger once aligned (the target is in view
	// with a line of fire and the fire policy allows it).
	Fire bool
}

// Turn returns the view after one command of msec turning towards point p
// from eye (the rate-capped slew), starting at the view (yaw, pitch) when
// the shooter has no aim yet.
func (s *Shooter) Turn(eye Vec3, yaw, pitch float32, p Vec3, msec int) (float32, float32) {
	if !s.aimed {
		s.yaw, s.pitch, s.aimed = yaw, pitch, true
	}
	if wy, wp, ok := control.LookAt(eye, p); ok {
		s.yaw, s.pitch = s.Slew.Step(s.yaw, s.pitch, wy, wp, msec)
	}
	return s.yaw, s.pitch
}

// Aim returns the view for one command of msec towards t from eye, starting
// at the view (yaw, pitch) when the shooter has no aim yet, and whether to
// fire: the turned view is within the aim tolerance of t (the angle its
// radius covers, between 1.5 and 8 degrees).
func (s *Shooter) Aim(eye Vec3, yaw, pitch float32, t AimTarget, msec int) (float32, float32, bool) {
	if !s.aimed {
		s.yaw, s.pitch, s.aimed = yaw, pitch, true
	}
	wy, wp, ok := control.LookAt(eye, t.Point)
	if !ok {
		return s.yaw, s.pitch, false
	}
	s.yaw, s.pitch = s.Slew.Step(s.yaw, s.pitch, wy, wp, msec)
	d := dist3(eye, t.Point)
	tol := float32(8)
	if d > 1 {
		tol = float32(math.Atan2(float64(max(t.Radius, 8)), float64(d)) * 180 / math.Pi)
	}
	tol = min(max(tol, 1.5), 8)
	return s.yaw, s.pitch, t.Fire && control.AimError(s.yaw, s.pitch, eye, t.Point) <= tol
}

// aimFor returns the aim target for track tr with weapon k from eye.
func aimFor(eye Vec3, tr *worldmodel.Track, k decide.WeaponKey) (Vec3, float32) {
	c := Vec3{tr.Pos[0] + (tr.Mins[0]+tr.Maxs[0])/2, tr.Pos[1] + (tr.Mins[1]+tr.Maxs[1])/2, tr.Pos[2] + (tr.Mins[2]+tr.Maxs[2])/2}
	r := min(tr.Maxs[0]-tr.Mins[0], tr.Maxs[2]-tr.Mins[2]) / 2
	if r <= 0 {
		r = 16
	}
	return control.Lead(eye, c, tr.Vel, weaponSpeed(k)), r
}

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}
