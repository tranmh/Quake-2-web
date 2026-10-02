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
//     policy fires with a neutral (misc_insane, misc_actor) in the line of
//     fire, or a splash weapon at a target or a wall closer than
//     SplashSafe.
//   - DodgeFor sidesteps an incoming projectile that will pass within
//     DodgeMiss (plus half its splash radius) in less than DodgeETA.
//   - GrenadeEscape runs (and jumps) away from a live grenade within
//     GrenadeRadius.
//   - AimPoint and Slew (aim.go) aim: the box centre for hitscan weapons,
//     led for projectiles, the feet of a target on the ground for splash
//     weapons; a rate-capped exponential slew turns the view.
//   - MoveDir and Strafer turn a movement relative to the target (advance,
//     retreat, strafe left or right, hold) into a direction, keeping a
//     strafe side at least StrafeHold and flipping it when it is blocked.

// Reflex thresholds.
const (
	// SplashSafe is the least distance (units) to the target and to a wall
	// along the line of fire for a splash weapon (rocket, grenade, BFG).
	SplashSafe = 150
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
	// StrafeHold is the least time (ms) a strafe keeps its side.
	StrafeHold = 400
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
	// Visible: the target is in view now (else Aim is where it was last
	// known). Shootable: a shot from the eye reaches Aim (MASK_SHOT).
	Visible, Shootable bool
	// Neutral: a neutral body (misc_insane, misc_actor) is in the line of
	// fire.
	Neutral bool
	// WallClose: a solid lies within SplashSafe along the view.
	WallClose bool
}

// Reasons FireGate gives for not firing.
const (
	NoFireHold        = "hold"
	NoFireNeutral     = "neutral"
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
	case NoFireNeutral, NoFireSplashClose, NoFireSplashWall:
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
	case in.Weapon.HasSplash() && d < SplashSafe:
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
	MoveStrafeLeft
	MoveStrafeRight
)

// String returns the decide vocabulary name of the movement.
func (m Move) String() string {
	switch m {
	case MoveAdvance:
		return "advance"
	case MoveRetreat:
		return "retreat"
	case MoveStrafeLeft:
		return "strafe_left"
	case MoveStrafeRight:
		return "strafe_right"
	}
	return "hold"
}

// Side returns +1 for strafe_right, -1 for strafe_left, else 0.
func (m Move) Side() int {
	switch m {
	case MoveStrafeRight:
		return 1
	case MoveStrafeLeft:
		return -1
	}
	return 0
}

// MoveDir returns the horizontal unit direction of movement m for a bot at
// self engaging a target at target (zero for hold, or with the target on
// top of the bot): towards it, away from it, or perpendicular to the line
// to it, to the bot's left or right as it faces the target.
func MoveDir(m Move, self, target Vec3) Vec3 {
	to := Vec3{target[0] - self[0], target[1] - self[1], 0}
	if m == MoveHold || shared.VectorNormalize(&to) == 0 {
		return Vec3{}
	}
	switch m {
	case MoveAdvance:
		return to
	case MoveRetreat:
		return Vec3{-to[0], -to[1], 0}
	case MoveStrafeLeft:
		return Vec3{-to[1], to[0], 0}
	case MoveStrafeRight:
		return Vec3{to[1], -to[0], 0}
	}
	return Vec3{}
}

// SideDir returns the strafe direction of side (+1 right, -1 left) for a
// bot at self facing target.
func SideDir(side int, self, target Vec3) Vec3 {
	switch {
	case side > 0:
		return MoveDir(MoveStrafeRight, self, target)
	case side < 0:
		return MoveDir(MoveStrafeLeft, self, target)
	}
	return Vec3{}
}

// Strafer keeps a strafe side: a new side is taken only once the current
// one was held Hold ms (StrafeHold when 0), and a side whose way is
// blocked flips to the other at once. The zero value is ready.
type Strafer struct {
	Hold  int64
	side  int
	since int64
}

// Side returns the side (+1 right, -1 left, 0 none) to strafe to at now
// (ms) when the intent wants side want (0: no strafe), with open telling
// whether a side's way is clear. It returns 0 when both sides are blocked.
func (s *Strafer) Side(now int64, want int, open func(side int) bool) int {
	if want == 0 {
		s.side = 0
		return 0
	}
	hold := s.Hold
	if hold <= 0 {
		hold = StrafeHold
	}
	cand := s.side
	if cand == 0 || cand != want && now-s.since >= hold {
		cand = want
	}
	if !open(cand) {
		if !open(-cand) {
			return 0
		}
		cand = -cand
	}
	if cand != s.side {
		s.side, s.since = cand, now
	}
	return cand
}

// Current returns the side held (0: none).
func (s *Strafer) Current() int { return s.side }

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3
