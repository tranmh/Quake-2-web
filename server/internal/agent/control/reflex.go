package control

import (
	"math"

	"quake2web/server/internal/qcommon/shared"
)

// The reflex layer: what the controller does every usercmd (25 ms) on its
// own, faster than any decision backend answers, and whatever the decided
// intent says (reflexes always win). The functions here are pure: the
// bot gathers their inputs from its fair belief, its own player state and
// static map knowledge, and applies their verdicts.
//
//   - FireGate executes a fire policy: hold never fires; fire-when-aligned
//     fires only at a target in view with a clear line of fire (MASK_SHOT)
//     and the aim within the weapon's tolerance (AimTolerance:
//     atan(radius/dist) plus the weapon's spread); suppress also fires at a
//     remembered position with a clear line, within a wider tolerance. No
//     policy fires with a neutral (misc_insane, misc_actor) or a near
//     explosive barrel in the line of fire, or a splash weapon at a target
//     or a wall closer than SplashSafe.
//   - DodgeFor sidesteps an incoming projectile that will pass within
//     DodgeMiss (plus half its splash radius) in less than DodgeETA.
//   - GrenadeEscape runs (and jumps) away from a live grenade within
//     GrenadeRadius.
//   - AimPoint and Slew (aim.go) aim: the box centre for hitscan weapons,
//     led for projectiles, the feet of a target on the ground for splash
//     weapons; a rate-capped exponential slew turns the view.
//   - MoveDir, SideDir and Strafer turn a movement relative to the target
//     (advance, retreat, strafe, hold) into a direction. The decision layer
//     only says strafe; the Strafer picks the side every command: a seeded
//     rhythm of StrafeMin to StrafeMax ms segments that alternate, a side
//     whose way is blocked flipping at once, a dodge side taken at once.

// Reflex thresholds.
const (
	// SplashSafe is the least distance (units) to the target and to a wall
	// along the line of fire for a splash weapon (rocket, grenade, BFG).
	SplashSafe = 150
	// BarrelSafe is the distance (units) within which an explosive barrel
	// in the line of fire holds it (misc_explobox: 150 damage over a
	// radius of 190).
	BarrelSafe = 240
	// DodgeMiss is the closest approach (units) of a projectile that makes
	// the bot sidestep; splash projectiles add half their radius.
	DodgeMiss = 36
	// DodgeETA is the longest time to the closest approach (seconds) that
	// makes the bot sidestep: below any model's latency.
	DodgeETA = 0.35
	// DodgeHold is how long (ms) a dodge is held at least.
	DodgeHold = 300
	// GrenadeRadius is the distance (units) within which the bot runs from
	// a live grenade; GrenadeJump the one within which it also jumps.
	GrenadeRadius = 192
	GrenadeJump   = 112
	// StrafeMin and StrafeMax bound a segment of the strafe rhythm (ms):
	// the Strafer keeps a side that long, then takes the other.
	StrafeMin = 600
	StrafeMax = 1200
	// MinAimTol and MaxAimTol bound the aim tolerance (degrees).
	MinAimTol = 0.75
	MaxAimTol = 10
	// SuppressSlack widens the aim tolerance of suppressive fire (degrees,
	// on top of twice the aligned tolerance).
	SuppressSlack = 3
)

// FireMode is the fire policy the reflex layer executes.
type FireMode uint8

// Fire policies.
const (
	FireHold     FireMode = iota // never fire
	FireAligned                  // only at a visible target with the aim on it
	FireSuppress                 // also at a remembered position, with a wider tolerance
)

// String returns the decide vocabulary name of the policy.
func (m FireMode) String() string {
	switch m {
	case FireAligned:
		return "fire_when_aligned"
	case FireSuppress:
		return "suppress"
	}
	return "hold"
}

// FireInput is what FireGate judges one command on.
type FireInput struct {
	Mode   FireMode
	Weapon Weapon
	// Eye is the eye of the command, Yaw and Pitch its view (degrees,
	// pitch > 0 down).
	Eye        Vec3
	Yaw, Pitch float32
	// Aim is the point aimed at (AimPoint), Radius the target's half size
	// (units) for the aim tolerance.
	Aim    Vec3
	Radius float32
	// TargetDist is the distance (units) from the eye to the target itself
	// (its box centre; 0: unknown, the distance to Aim stands in). A led or
	// feet aim point can lie farther than the target: the splash gate
	// judges the nearer of the two.
	TargetDist float32
	// Visible: the target is in view now (else Aim is where it was last
	// known). Shootable: a shot from the eye reaches Aim (MASK_SHOT).
	Visible, Shootable bool
	// Neutral: a neutral body (misc_insane, misc_actor) is in the line of
	// fire.
	Neutral bool
	// Barrel: an explosive barrel (misc_explobox) within BarrelSafe of the
	// eye is in the line of fire (its blast would catch the bot).
	Barrel bool
	// WallClose: a solid lies within SplashSafe along the view.
	WallClose bool
}

// Reasons FireGate gives for not firing.
const (
	NoFireHold        = "hold"
	NoFireNeutral     = "neutral"
	NoFireBarrel      = "barrel"
	NoFireSplashClose = "splash_close"
	NoFireSplashWall  = "splash_wall"
	NoFireNotVisible  = "not_visible"
	NoFireNoLine      = "no_line"
	NoFireAim         = "aim"
)

// FireVerdict is FireGate's answer.
type FireVerdict struct {
	Fire bool
	// Reason says why not ("" when firing): one of the NoFire constants.
	Reason string
	// Err is the aim error and Tol the tolerance it was judged against
	// (degrees).
	Err, Tol float32
}

// Vetoed reports a verdict that overrides the policy whatever the aim: a
// neutral in the line or a splash weapon too close (the reflexes the trace
// reports as overriding the decision).
func (v FireVerdict) Vetoed() bool {
	switch v.Reason {
	case NoFireNeutral, NoFireBarrel, NoFireSplashClose, NoFireSplashWall:
		return true
	}
	return false
}

// AimTolerance is the largest aim error (degrees) at which weapon w still
// hits a target of half size radius at distance dist: the angle the target
// covers plus the weapon's spread, within [MinAimTol, MaxAimTol].
func AimTolerance(w Weapon, radius, dist float32) float32 {
	tol := float32(MaxAimTol)
	if dist > 1 {
		tol = float32(math.Atan2(float64(max(radius, 1)), float64(dist))*180/math.Pi) + w.Spread
	}
	return min(max(tol, MinAimTol), MaxAimTol)
}

// FireGate decides whether one command pulls the trigger.
func FireGate(in FireInput) FireVerdict {
	d := dist3(in.Eye, in.Aim)
	v := FireVerdict{Tol: AimTolerance(in.Weapon, in.Radius, d), Err: AimError(in.Yaw, in.Pitch, in.Eye, in.Aim)}
	switch {
	case in.Mode == FireHold:
		v.Reason = NoFireHold
	case in.Neutral:
		v.Reason = NoFireNeutral
	case in.Barrel:
		v.Reason = NoFireBarrel
	case in.Weapon.HasSplash() && splashDist(d, in.TargetDist) < SplashSafe:
		v.Reason = NoFireSplashClose
	case in.Weapon.HasSplash() && in.WallClose:
		v.Reason = NoFireSplashWall
	case in.Mode == FireAligned && !in.Visible:
		v.Reason = NoFireNotVisible
	case !in.Shootable:
		v.Reason = NoFireNoLine
	}
	if v.Reason != "" {
		return v
	}
	if in.Mode == FireSuppress {
		v.Tol = min(2*v.Tol+SuppressSlack, 2*MaxAimTol)
	}
	if v.Err > v.Tol {
		v.Reason = NoFireAim
		return v
	}
	v.Fire = true
	return v
}

// splashDist is the distance the splash gate judges: the nearer of the aim
// point's (d) and the target's (target; 0: unknown).
func splashDist(d, target float32) float32 {
	if target > 0 {
		return min(d, target)
	}
	return d
}

// Body is a target's box as the bot believes it.
type Body struct {
	Origin, Mins, Maxs Vec3
	// Vel is its velocity estimate.
	Vel Vec3
	// OnGround: it stands (a splash weapon aims at its feet).
	OnGround bool
}

// Center returns the middle of the body's box.
func (b *Body) Center() Vec3 {
	return Vec3{b.Origin[0] + (b.Mins[0]+b.Maxs[0])/2, b.Origin[1] + (b.Mins[1]+b.Maxs[1])/2, b.Origin[2] + (b.Mins[2]+b.Maxs[2])/2}
}

// Radius returns the half size of the body for the aim tolerance (the
// smaller of its half width and half height; 16 for an empty box).
func (b *Body) Radius() float32 {
	r := min(b.Maxs[0]-b.Mins[0], b.Maxs[2]-b.Mins[2]) / 2
	if r <= 0 {
		return 16
	}
	return r
}

// AimPoint returns where to aim weapon w from eye at body t: its box
// centre for a hitscan weapon, the point it reaches when the projectile
// gets there for a projectile weapon (Lead), and for a splash weapon at a
// body on the ground its feet (the blast catches a target the projectile
// misses), led too. feet reports the feet point: the caller falls back to
// AimPoint with OnGround false when no shot reaches it.
func AimPoint(eye Vec3, t Body, w Weapon) (p Vec3, feet bool) {
	c := t.Center()
	if w.Hitscan() {
		return c, false
	}
	if w.HasSplash() && t.OnGround {
		c[2] = t.Origin[2] + t.Mins[2] + 8
		feet = true
	}
	return Lead(eye, c, t.Vel, w.Speed), feet
}

// Incoming is a projectile flying at the bot (the belief's prediction).
type Incoming struct {
	// TCA is the time (s) to its closest approach to the bot (<= 0: moving
	// away) and Miss the distance (units) then.
	TCA, Miss float32
	// Splash is the radius of its radius damage (0: none).
	Splash float32
	// Dir is the horizontal unit direction that increases the miss most.
	Dir Vec3
}

// DodgeFor returns the sidestep direction for the most urgent of the
// incoming projectiles that will pass within DodgeMiss (plus half its
// splash radius) of the bot in less than DodgeETA, its time to the closest
// approach, and ok false when none is that close.
func DodgeFor(ps []Incoming) (dir Vec3, tca float32, ok bool) {
	best := float32(math.MaxFloat32)
	for i := range ps {
		p := &ps[i]
		if p.TCA <= 0 || p.TCA > DodgeETA || p.Miss >= DodgeMiss+p.Splash/2 {
			continue
		}
		if p.Dir[0] == 0 && p.Dir[1] == 0 {
			continue
		}
		if p.TCA < best {
			best, dir, ok = p.TCA, p.Dir, true
		}
	}
	return dir, best, ok
}

// GrenadeEscape returns the direction (horizontal, unit) straight away from
// the nearest of the live grenades gs within GrenadeRadius of origin, and
// jump when it is within GrenadeJump and the bot stands (a jump carries it
// further before the blast and lifts its centre off the floor the grenade
// lies on). ok is false when no grenade is that close.
func GrenadeEscape(origin Vec3, gs []Vec3, onGround bool) (dir Vec3, jump, ok bool) {
	bd := float32(GrenadeRadius)
	var near Vec3
	for _, g := range gs {
		if d := dist3(origin, g); d < bd {
			bd, near, ok = d, g, true
		}
	}
	if !ok {
		return Vec3{}, false, false
	}
	dir = Vec3{origin[0] - near[0], origin[1] - near[1], 0}
	if shared.VectorNormalize(&dir) == 0 {
		dir = Vec3{1, 0, 0}
	}
	return dir, onGround && bd < GrenadeJump, true
}

// Move is a movement relative to the target (decide's movement field).
type Move uint8

// Movements.
const (
	MoveHold Move = iota
	MoveAdvance
	MoveRetreat
	MoveStrafe // sidestep; the Strafer picks the side
)

// String returns the decide vocabulary name of the movement.
func (m Move) String() string {
	switch m {
	case MoveAdvance:
		return "advance"
	case MoveRetreat:
		return "retreat"
	case MoveStrafe:
		return "strafe"
	}
	return "hold"
}

// MoveDir returns the horizontal unit direction of movement m for a bot at
// self engaging a target at target (zero for hold or a strafe, which needs
// a side: SideDir, or with the target on top of the bot): towards it or
// away from it.
func MoveDir(m Move, self, target Vec3) Vec3 {
	to := Vec3{target[0] - self[0], target[1] - self[1], 0}
	if shared.VectorNormalize(&to) == 0 {
		return Vec3{}
	}
	switch m {
	case MoveAdvance:
		return to
	case MoveRetreat:
		return Vec3{-to[0], -to[1], 0}
	}
	return Vec3{}
}

// SideDir returns the strafe direction of side (+1 right, -1 left) for a
// bot at self facing target: perpendicular to the line to it (zero for
// side 0 or with the target on top of the bot).
func SideDir(side int, self, target Vec3) Vec3 {
	to := Vec3{target[0] - self[0], target[1] - self[1], 0}
	if side == 0 || shared.VectorNormalize(&to) == 0 {
		return Vec3{}
	}
	if side > 0 {
		return Vec3{to[1], -to[0], 0}
	}
	return Vec3{-to[1], to[0], 0}
}

// SideOf returns the strafe side (+1 right, -1 left, 0 neither) whose
// direction for a bot at self facing target points along dir (a dodge
// direction).
func SideOf(dir, self, target Vec3) int {
	r := SideDir(1, self, target)
	switch d := dir[0]*r[0] + dir[1]*r[1]; {
	case d > 0.1:
		return 1
	case d < -0.1:
		return -1
	}
	return 0
}

// Strafer keeps the strafe rhythm. It strafes to one side for a segment
// of StrafeMin to StrafeMax ms, then to the other; each segment's length
// (and the first side) is drawn from Seed and the segment's number, so a
// run repeats exactly. A side whose way is blocked flips to the other at
// once (a new segment), and a preferred side (the dodge side of an
// incoming projectile) is taken at once, for a new segment. The rhythm runs on across pauses: a strafe resumed
// after a hold or an advance continues the current segment, or starts the
// next one when it is over. The zero value is ready (seed 0).
type Strafer struct {
	Seed  uint64
	side  int
	until int64  // the current segment's end
	n     uint64 // segments drawn
}

// mix is the splitmix64 finalizer (decide.Mix64).
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// segment starts a new segment of side at now.
func (s *Strafer) segment(side int, now int64) {
	h := mix(s.Seed ^ mix(s.n))
	s.n++
	s.side, s.until = side, now+StrafeMin+int64(h%uint64(StrafeMax-StrafeMin+1))
}

// Side returns the side (+1 right, -1 left, 0 both blocked) to strafe to
// at now (ms): the rhythm's, or prefer when it is not 0, with open telling
// whether a side's way is clear.
func (s *Strafer) Side(now int64, prefer int, open func(side int) bool) int {
	switch {
	case s.side == 0:
		first := 1
		if mix(s.Seed^0x5bd1e995)&1 == 0 {
			first = -1
		}
		if prefer != 0 {
			first = prefer
		}
		s.segment(first, now)
	case prefer != 0 && prefer != s.side:
		s.segment(prefer, now)
	case now >= s.until:
		s.segment(-s.side, now)
	}
	if !open(s.side) {
		if !open(-s.side) {
			return 0
		}
		s.segment(-s.side, now)
	}
	return s.side
}

// Current returns the side held (0: none yet).
func (s *Strafer) Current() int { return s.side }

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3
