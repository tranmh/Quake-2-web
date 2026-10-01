package navsim

import (
	"math"

	"quake2web/server/internal/pmove"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Physics are the server settings pmove runs with. A bot game may not change
// them (session forbids the physics cvars), so the defaults are what the
// live server uses.
type Physics struct {
	// Gravity is sv_gravity (pm.s.gravity).
	Gravity int16
	// AirAccelerate is sv_airaccelerate (pm_airaccelerate): 0 in single
	// player.
	AirAccelerate float32
	// StepMsec is the usercmd length the executors are simulated with.
	StepMsec int
	// FrameMsec is the server frame: falling damage is judged once per
	// frame (P_FallingDamage in ClientEndServerFrame).
	FrameMsec int
}

// DefaultPhysics returns the single-player defaults with the lockstep
// session's 25 ms commands.
func DefaultPhysics() Physics {
	return Physics{Gravity: 800, AirAccelerate: 0, StepMsec: 25, FrameMsec: 100}
}

// Cmd is one usercmd as an executor wants it: movement plus absolute view
// angles. UserCmd turns it into the wire command for a client whose
// delta_angles are known.
type Cmd struct {
	Msec    uint8
	Buttons uint8
	Forward int16
	Side    int16
	Up      int16
	// Yaw and Pitch are the wanted view angles in degrees (pitch > 0 looks
	// down).
	Yaw, Pitch float32
}

// maxPitch matches control.MaxPitch (navsim cannot import control, which
// builds on nav; a test in control pins the two conversions together).
const maxPitch = 89

// UserCmd returns the usercmd that makes the server see c's view angles
// for a client with the given delta_angles: ANGLE2SHORT(want) - delta in
// short arithmetic, pitch clamped to ±89. It is control.CmdAngles.
func (c Cmd) UserCmd(delta [3]int16) shared.UserCmd {
	p := c.Pitch
	if p > maxPitch {
		p = maxPitch
	} else if p < -maxPitch {
		p = -maxPitch
	}
	u := shared.UserCmd{Msec: c.Msec, Buttons: c.Buttons, ForwardMove: c.Forward, SideMove: c.Side, UpMove: c.Up}
	u.Angles[q2const.PITCH] = int16(shared.ANGLE2SHORT(p) - int32(delta[q2const.PITCH]))
	u.Angles[q2const.YAW] = int16(shared.ANGLE2SHORT(c.Yaw) - int32(delta[q2const.YAW]))
	u.Angles[q2const.ROLL] = int16(-int32(delta[q2const.ROLL]))
	return u
}

// State is the player's movement state between commands: what the server
// keeps in client->ps.pmove plus the results of the last move.
type State struct {
	PM         shared.PmoveState
	Mins, Maxs Vec3
	ViewHeight float32
	// Ground is pm.groundentity: WorldEnt, a Solid ID, or -1 in the air.
	Ground     int
	WaterLevel int32
	WaterType  int32
	// Msec is the simulated time since Reset.
	Msec int
	// OldVelocity is client->oldvelocity as of the last frame boundary.
	OldVelocity Vec3
	// CmdAngles is client->resp.cmd_angles (SHORT2ANGLE of the last
	// command's angles; teleporters use it).
	CmdAngles Vec3
	// ViewYaw is the view yaw of the last command.
	ViewYaw float32
}

// Origin returns the player origin (pm.s.origin / 8).
func (s State) Origin() Vec3 {
	return Vec3{float32(float64(s.PM.Origin[0]) * 0.125), float32(float64(s.PM.Origin[1]) * 0.125), float32(float64(s.PM.Origin[2]) * 0.125)}
}

// Velocity returns the player velocity (pm.s.velocity / 8).
func (s State) Velocity() Vec3 {
	return Vec3{float32(float64(s.PM.Velocity[0]) * 0.125), float32(float64(s.PM.Velocity[1]) * 0.125), float32(float64(s.PM.Velocity[2]) * 0.125)}
}

// HSpeed is the horizontal speed.
func (s State) HSpeed() float32 {
	v := s.Velocity()
	return float32(math.Hypot(float64(v[0]), float64(v[1])))
}

// OnGround reports pm.groundentity != NULL.
func (s State) OnGround() bool { return s.Ground >= 0 }

// Ducked reports PMF_DUCKED.
func (s State) Ducked() bool { return s.PM.PmFlags&q2const.PMF_DUCKED != 0 }

// CanJump reports whether a jump pressed now starts: on ground, not in the
// landing pause, jump released.
func (s State) CanJump() bool {
	return s.OnGround() && s.PM.PmFlags&(q2const.PMF_TIME_LAND|q2const.PMF_JUMP_HELD) == 0 && s.WaterLevel < 2
}

// SnapOrigin quantizes a position to the pmove origin grid (1/8 unit,
// truncated toward zero like pm.s.origin = origin*8).
func SnapOrigin(p Vec3) [3]int16 {
	return [3]int16{int16(int32(p[0] * 8)), int16(int32(p[1] * 8)), int16(int32(p[2] * 8))}
}

// Volume is a box the runner reports the player as being inside: a trigger
// (absmin/absmax, already grown by 1 like SV_LinkEdict), an item, a door
// trigger. The player box is the link box of origin+mins..maxs grown by 1,
// as G_TouchTriggers / SV_AreaEdicts compare them.
type Volume struct {
	ID       int
	Min, Max Vec3
}

// Push is a trigger_push: entering it sets the velocity (movedir * speed *
// 10) and oldvelocity. Once removes it after the first push (PUSH_ONCE).
// C: game/g_trigger.c:394 trigger_push_touch
type Push struct {
	ID       int
	Min, Max Vec3
	Velocity Vec3
	Once     bool
}

// Teleport is a misc_teleporter trigger: entering it moves the player to
// Dest + 10 up, clears the velocity, holds the player for 160 ms and turns
// the view to Angles.
// C: game/g_misc.c:1780 teleporter_touch
type Teleport struct {
	ID       int
	Min, Max Vec3
	Dest     Vec3
	Angles   Vec3
}

// Inside is one volume the player was inside after a step.
type Inside struct {
	ID int
	// Yaw is the view yaw of the command of that step (directional
	// triggers test the player's facing).
	Yaw float32
}

// StepResult is what one Step reports. Its slices are reused by the next
// Step.
type StepResult struct {
	// Touched are the solids in the pmove touch list (deduplicated, world
	// excluded).
	Touched []int
	// Inside are the Volumes the player box overlaps after the move.
	Inside []Inside
	// Pushed / Teleported report a hook that fired after this step (its ID,
	// or 0).
	Pushed, Teleported int
	// FrameEnd is set when this step ended a server frame; FallDamage is
	// the falling damage judged then (0 if none).
	FrameEnd   bool
	FallDamage int
}

// Runner steps pmove for one simulated player in a World.
type Runner struct {
	World *World
	Phys  Physics
	// Volumes, Pushes and Teleports are tested after every step, like
	// G_TouchTriggers after each ClientThink.
	Volumes   []Volume
	Pushes    []Push
	Teleports []Teleport
	// Dead uses MASK_DEADSOLID and PM_DEAD.
	Dead bool

	mv       *pmove.Mover
	pm       pmove.PmoveT
	st       State
	snap     bool
	frameAcc int
	usedPush []bool
	res      StepResult
}

// NewRunner returns a runner with physics p in world w. Call Reset before
// stepping.
func NewRunner(w *World, p Physics) *Runner {
	r := &Runner{World: w, Phys: p, mv: pmove.NewMover()}
	r.mv.AirAccelerate = p.AirAccelerate
	r.pm.Trace = func(start, mins, maxs, end *Vec3) shared.Trace {
		mask := int32(q2const.MASK_PLAYERSOLID)
		if r.Dead {
			mask = q2const.MASK_DEADSOLID
		}
		return r.World.Trace(*start, *mins, *maxs, *end, mask)
	}
	r.pm.PointContents = func(p Vec3) int32 { return r.World.PointContents(p) }
	return r
}

// Reset places the player at origin (snapped to the 1/8 grid) at rest,
// standing (or ducked), as if it had just been put there: the next step
// runs PM_InitialSnapPosition like the server after an outside change.
func (r *Runner) Reset(origin Vec3, ducked bool) {
	r.st = State{Ground: -1}
	r.st.PM.Origin = SnapOrigin(origin)
	r.st.PM.Gravity = r.Phys.Gravity
	r.st.Mins, r.st.Maxs, r.st.ViewHeight = StandMins(), StandMaxs(), 22
	if ducked {
		r.st.PM.PmFlags = q2const.PMF_DUCKED | q2const.PMF_ON_GROUND
		r.st.Maxs, r.st.ViewHeight = DuckMaxs(), -2
	}
	r.snap = true
	r.frameAcc = 0
	r.usedPush = r.usedPush[:0]
}

// SetState replaces the movement state (for a follower that rebuilds it
// from the client's player state). snap requests the initial snap of the
// next move.
func (r *Runner) SetState(s State, snap bool) {
	r.st = s
	r.snap = snap
}

// State returns a copy of the current state.
func (r *Runner) State() State { return r.st }

// StatePtr returns the live state (read only; valid until the next Step).
func (r *Runner) StatePtr() *State { return &r.st }

// Step runs one command (Msec 0 means Phys.StepMsec) and the touch hooks.
// C: game/p_client.c:1565 ClientThink (the pmove part)
func (r *Runner) Step(c Cmd) *StepResult {
	if c.Msec == 0 {
		c.Msec = uint8(r.Phys.StepMsec)
	}
	st := &r.st
	ucmd := c.UserCmd(st.PM.DeltaAngles)
	pm := &r.pm
	pm.S = st.PM
	pm.S.Gravity = r.Phys.Gravity
	pm.S.PmType = q2const.PM_NORMAL
	if r.Dead {
		pm.S.PmType = q2const.PM_DEAD
	}
	pm.SnapInitial = r.snap
	r.snap = false
	pm.Cmd = ucmd
	r.mv.Pmove(pm)

	st.PM = pm.S
	st.Mins, st.Maxs, st.ViewHeight = pm.Mins, pm.Maxs, pm.ViewHeight
	st.Ground, st.WaterLevel, st.WaterType = pm.GroundEntity, pm.WaterLevel, pm.WaterType
	st.Msec += int(c.Msec)
	for i := 0; i < 3; i++ {
		st.CmdAngles[i] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[i])))
	}
	st.ViewYaw = c.Yaw

	res := &r.res
	res.Touched = res.Touched[:0]
	res.Inside = res.Inside[:0]
	res.Pushed, res.Teleported = 0, 0
	res.FrameEnd, res.FallDamage = false, 0
	for i := 0; i < pm.NumTouch; i++ {
		id := pm.TouchEnts[i]
		if id <= WorldEnt || containsInt(res.Touched, id) {
			continue
		}
		res.Touched = append(res.Touched, id)
	}
	r.touchTriggers(c.Yaw)

	r.frameAcc += int(c.Msec)
	if r.Phys.FrameMsec > 0 && r.frameAcc >= r.Phys.FrameMsec {
		r.frameAcc -= r.Phys.FrameMsec
		res.FrameEnd = true
		res.FallDamage = FallingDamage(st.Velocity(), st.OldVelocity, st.OnGround(), st.WaterLevel)
		st.OldVelocity = st.Velocity()
	}
	return res
}

// touchTriggers tests the player link box against the volumes and hooks.
// C: game/g_utils.c:439 G_TouchTriggers
func (r *Runner) touchTriggers(yaw float32) {
	st, res := &r.st, &r.res
	o := st.Origin()
	var pmin, pmax Vec3
	for i := 0; i < 3; i++ {
		pmin[i] = o[i] + st.Mins[i] - 1
		pmax[i] = o[i] + st.Maxs[i] + 1
	}
	for i := range r.Volumes {
		v := &r.Volumes[i]
		if boxesTouch(pmin, pmax, v.Min, v.Max) {
			res.Inside = append(res.Inside, Inside{ID: v.ID, Yaw: yaw})
		}
	}
	for i := range r.Pushes {
		p := &r.Pushes[i]
		if p.Once && r.pushUsed(i) {
			continue
		}
		if boxesTouch(pmin, pmax, p.Min, p.Max) {
			// the game stores the float velocity; the next ClientThink
			// converts it with pm.s.velocity = ent->velocity*8
			for k := 0; k < 3; k++ {
				st.PM.Velocity[k] = int16(int32(p.Velocity[k] * 8))
			}
			st.OldVelocity = p.Velocity
			res.Pushed = p.ID
			r.snap = true
			if p.Once {
				r.markPush(i)
			}
		}
	}
	for i := range r.Teleports {
		t := &r.Teleports[i]
		if !boxesTouch(pmin, pmax, t.Min, t.Max) {
			continue
		}
		dest := t.Dest
		dest[2] += 10
		st.PM.Origin = SnapOrigin(dest)
		st.PM.Velocity = [3]int16{}
		st.PM.PmTime = 160 >> 3
		st.PM.PmFlags |= q2const.PMF_TIME_TELEPORT
		for k := 0; k < 3; k++ {
			st.PM.DeltaAngles[k] = int16(shared.ANGLE2SHORT(t.Angles[k] - st.CmdAngles[k]))
		}
		res.Teleported = t.ID
		r.snap = true
		break // the player is somewhere else now
	}
}

func (r *Runner) pushUsed(i int) bool { return i < len(r.usedPush) && r.usedPush[i] }

func (r *Runner) markPush(i int) {
	for len(r.usedPush) <= i {
		r.usedPush = append(r.usedPush, false)
	}
	r.usedPush[i] = true
}

func boxesTouch(amin, amax, bmin, bmax Vec3) bool {
	return !(amin[0] > bmax[0] || amin[1] > bmax[1] || amin[2] > bmax[2] ||
		amax[0] < bmin[0] || amax[1] < bmin[1] || amax[2] < bmin[2])
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// FallingDamage is the damage P_FallingDamage deals at a frame boundary for
// the current velocity, the velocity at the previous boundary, whether the
// player stands on something and its water level (0 when it only plays a
// footstep or a short-fall sound). Armor and god mode are not applied.
// C: game/p_view.c:478 P_FallingDamage
func FallingDamage(vel, oldVel Vec3, onGround bool, waterLevel int32) int {
	var delta float32
	if oldVel[2] < 0 && vel[2] > oldVel[2] && !onGround {
		delta = oldVel[2]
	} else {
		if !onGround {
			return 0
		}
		delta = vel[2] - oldVel[2]
	}
	delta = float32(float64(delta*delta) * 0.0001)

	// never take falling damage if completely underwater
	switch waterLevel {
	case 3:
		return 0
	case 2:
		delta = float32(float64(delta) * 0.25)
	case 1:
		delta = float32(float64(delta) * 0.5)
	}
	// below 1 nothing, below 15 a footstep, up to 30 EV_FALLSHORT
	if delta <= 30 {
		return 0
	}
	damage := int32((delta - 30) / 2)
	if damage < 1 {
		damage = 1
	}
	return int(damage)
}

// FallDamageForDrop estimates the falling damage of a free fall of h units
// at rest (v² = 2gh, delta = v²/10000): above about 187.5 units it hurts.
func FallDamageForDrop(h float32, gravity int16) int {
	v := float32(math.Sqrt(2 * float64(gravity) * float64(h)))
	return FallingDamage(Vec3{}, Vec3{0, 0, -v}, true, 0)
}

// OnLadder repeats pmove's ladder test for a player in state s facing yaw:
// a hull trace 1 unit along the flat forward vector hits CONTENTS_LADDER.
// C: qcommon/pmove.c:831 PM_CheckSpecialMovement
func (w *World) OnLadder(s *State, yaw float32) bool {
	var fwd Vec3
	shared.AngleVectors(Vec3{0, yaw, 0}, &fwd, nil, nil)
	fwd[2] = 0
	shared.VectorNormalize(&fwd)
	o := s.Origin()
	spot := shared.VectorMA(o, 1, fwd)
	tr := w.Trace(o, s.Mins, s.Maxs, spot, q2const.MASK_PLAYERSOLID)
	return tr.Fraction < 1 && tr.Contents&q2const.CONTENTS_LADDER != 0
}

// Categorize fills s.Ground, WaterLevel and WaterType from the world the way
// PM_CatagorizePosition does (without changing any flags), for a follower
// that rebuilds the state from the client's player state.
// C: qcommon/pmove.c:671 PM_CatagorizePosition
func (w *World) Categorize(s *State) {
	o := s.Origin()
	point := o
	point[2] = float32(float64(o[2]) - 0.25)
	s.Ground = -1
	if s.Velocity()[2] <= 180 {
		tr := w.Trace(o, s.Mins, s.Maxs, point, q2const.MASK_PLAYERSOLID)
		if !(tr.Ent < 0 || (float64(tr.Plane.Normal[2]) < 0.7 && !tr.StartSolid)) {
			s.Ground = tr.Ent
		}
	}
	s.WaterLevel, s.WaterType = 0, 0
	sample2 := int32(s.ViewHeight - s.Mins[2])
	sample1 := sample2 / 2
	point[2] = o[2] + s.Mins[2] + 1
	cont := w.PointContents(point)
	if cont&q2const.MASK_WATER != 0 {
		s.WaterType = cont
		s.WaterLevel = 1
		point[2] = o[2] + s.Mins[2] + float32(sample1)
		if cont = w.PointContents(point); cont&q2const.MASK_WATER != 0 {
			s.WaterLevel = 2
			point[2] = o[2] + s.Mins[2] + float32(sample2)
			if cont = w.PointContents(point); cont&q2const.MASK_WATER != 0 {
				s.WaterLevel = 3
			}
		}
	}
}

// lookahead steps c n times, calling fn after each step, and then puts the
// runner back as it was (state, snap request, frame phase, used pushes),
// so a caller can see where the player would go without moving it. The
// StepResult of the last Step is not restored.
func (r *Runner) lookahead(c Cmd, n int, fn func(s *State)) {
	if n <= 0 {
		return
	}
	st, snap, acc := r.st, r.snap, r.frameAcc
	used := append([]bool(nil), r.usedPush...)
	for i := 0; i < n; i++ {
		r.Step(c)
		fn(&r.st)
	}
	r.st, r.snap, r.frameAcc = st, snap, acc
	r.usedPush = append(r.usedPush[:0], used...)
}
