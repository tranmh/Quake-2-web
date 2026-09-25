// Package pmove ports qcommon/pmove.c, the player movement code shared by
// the server and client prediction. It must be bit-identical to the C
// oracle: every float expression follows the C promotion rules (see
// docs/PORTING.md).
package pmove

import (
	"math"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

type Vec3 = shared.Vec3

// STEPSIZE is the maximum step height.
// C: qcommon/pmove.c:25 STEPSIZE
const STEPSIZE = 18

// STOP_EPSILON is a double literal in C.
// C: qcommon/pmove.c:78 STOP_EPSILON
const STOP_EPSILON = 0.1

// MIN_STEP_NORMAL (double literal): can't step up onto very steep slopes.
// C: qcommon/pmove.c:111 MIN_STEP_NORMAL
const MIN_STEP_NORMAL = 0.7

// MAX_CLIP_PLANES bounds PM_StepSlideMove_.
// C: qcommon/pmove.c:112 MAX_CLIP_PLANES
const MAX_CLIP_PLANES = 5

// NoEnt is the entity id standing for a NULL edict_t pointer.
const NoEnt = -1

// PmoveT is C pmove_t. Entity pointers are opaque ids; NoEnt (-1) is NULL.
// C: game/q_shared.h:546 pmove_t
type PmoveT struct {
	// state (in / out)
	S shared.PmoveState

	// command (in)
	Cmd         shared.UserCmd
	SnapInitial bool // if s has been changed outside pmove

	// results (out)
	NumTouch  int
	TouchEnts [q2const.MAXTOUCH]int

	ViewAngles Vec3 // clamped
	ViewHeight float32

	Mins, Maxs Vec3 // bounding box size

	GroundEntity int
	WaterType    int32
	WaterLevel   int32

	// callbacks to test the world
	Trace         func(start, mins, maxs, end *Vec3) shared.Trace
	PointContents func(point Vec3) int32
}

// pml is C pml_t: all of the locals are zeroed before each pmove.
// C: qcommon/pmove.c:46 pml_t
type pml struct {
	origin   Vec3 // full float precision
	velocity Vec3 // full float precision

	forward, right, up Vec3
	frametime          float32

	groundsurface  *shared.CSurface
	groundplane    shared.CPlane
	groundcontents int32

	previousOrigin Vec3
	ladder         bool
}

// Mover holds the movement parameters (the pm_* globals) and the per-call
// scratch (pm, pml). One Mover must not be used concurrently.
type Mover struct {
	// movement parameters
	// C: qcommon/pmove.c:53 pm_stopspeed .. pm_waterspeed
	StopSpeed       float32
	MaxSpeed        float32
	DuckSpeed       float32
	Accelerate      float32
	AirAccelerate   float32
	WaterAccelerate float32
	Friction        float32
	WaterFriction   float32
	WaterSpeed      float32

	pm  *PmoveT
	pml pml
}

// NewMover returns a Mover with the C default parameters.
func NewMover() *Mover {
	return &Mover{
		StopSpeed:       100,
		MaxSpeed:        300,
		DuckSpeed:       100,
		Accelerate:      10,
		AirAccelerate:   0,
		WaterAccelerate: 10,
		Friction:        6,
		WaterFriction:   1,
		WaterSpeed:      400,
	}
}

// Pmove runs one move with the default parameters and the given
// pm_airaccelerate.
func Pmove(pm *PmoveT, airaccelerate float32) {
	mv := NewMover()
	mv.AirAccelerate = airaccelerate
	mv.Pmove(pm)
}

func entNull(e int) bool { return e < 0 }

// clipVelocity slides off of the impacting object.
// C: qcommon/pmove.c:80 PM_ClipVelocity
func clipVelocity(in, normal Vec3, out *Vec3, overbounce float32) {
	backoff := shared.DotProduct(in, normal) * overbounce

	for i := 0; i < 3; i++ {
		change := normal[i] * backoff
		out[i] = in[i] - change
		if float64(out[i]) > -STOP_EPSILON && float64(out[i]) < STOP_EPSILON {
			out[i] = 0
		}
	}
}

// stepSlideMove_ moves with sliding along clip planes.
// C: qcommon/pmove.c:113 PM_StepSlideMove_
func (mv *Mover) stepSlideMove_() {
	pm, pml := mv.pm, &mv.pml
	var planes [MAX_CLIP_PLANES]Vec3
	var end Vec3

	numbumps := 4

	primalVelocity := pml.velocity
	numplanes := 0

	timeLeft := pml.frametime

	for bumpcount := 0; bumpcount < numbumps; bumpcount++ {
		for i := 0; i < 3; i++ {
			end[i] = pml.origin[i] + float32(timeLeft*pml.velocity[i])
		}

		trace := pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &end)

		if trace.AllSolid { // entity is trapped in another solid
			pml.velocity[2] = 0 // don't build up falling damage
			return
		}

		if trace.Fraction > 0 { // actually covered some distance
			pml.origin = trace.EndPos
			numplanes = 0
		}

		if trace.Fraction == 1 {
			break // moved the entire distance
		}

		// save entity for contact
		if pm.NumTouch < q2const.MAXTOUCH && !entNull(trace.Ent) {
			pm.TouchEnts[pm.NumTouch] = trace.Ent
			pm.NumTouch++
		}

		timeLeft -= float32(timeLeft * trace.Fraction)

		// slide along this plane
		if numplanes >= MAX_CLIP_PLANES { // this shouldn't really happen
			pml.velocity = shared.Vec3Origin
			break
		}

		planes[numplanes] = trace.Plane.Normal
		numplanes++

		// modify original_velocity so it parallels all of the clip planes
		var i, j int
		for i = 0; i < numplanes; i++ {
			clipVelocity(pml.velocity, planes[i], &pml.velocity, 1.01) // (float)1.01, same as C double->float
			for j = 0; j < numplanes; j++ {
				if j != i {
					if shared.DotProduct(pml.velocity, planes[j]) < 0 {
						break // not ok
					}
				}
			}
			if j == numplanes {
				break
			}
		}

		if i != numplanes { // go along this plane
		} else { // go along the crease
			if numplanes != 2 {
				pml.velocity = shared.Vec3Origin
				break
			}
			dir := shared.CrossProduct(planes[0], planes[1])
			d := shared.DotProduct(dir, pml.velocity)
			pml.velocity = shared.VectorScale(dir, d)
		}

		// if velocity is against the original velocity, stop dead
		// to avoid tiny occilations in sloping corners
		if shared.DotProduct(pml.velocity, primalVelocity) <= 0 {
			pml.velocity = shared.Vec3Origin
			break
		}
	}

	if pm.S.PmTime != 0 {
		pml.velocity = primalVelocity
	}
}

// stepSlideMove tries a slide move both at the current height and stepped
// up, keeping the one that went farther.
// C: qcommon/pmove.c:271 PM_StepSlideMove
func (mv *Mover) stepSlideMove() {
	pm, pml := mv.pm, &mv.pml

	startO := pml.origin
	startV := pml.velocity

	mv.stepSlideMove_()

	downO := pml.origin
	downV := pml.velocity

	up := startO
	up[2] += STEPSIZE

	trace := pm.Trace(&up, &pm.Mins, &pm.Maxs, &up)
	if trace.AllSolid {
		return // can't step up
	}

	// try sliding above
	pml.origin = up
	pml.velocity = startV

	mv.stepSlideMove_()

	// push down the final amount
	down := pml.origin
	down[2] -= STEPSIZE
	trace = pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &down)
	if !trace.AllSolid {
		pml.origin = trace.EndPos
	}

	up = pml.origin

	// decide which one went farther
	downDist := float32((downO[0]-startO[0])*(downO[0]-startO[0])) +
		float32((downO[1]-startO[1])*(downO[1]-startO[1]))
	upDist := float32((up[0]-startO[0])*(up[0]-startO[0])) +
		float32((up[1]-startO[1])*(up[1]-startO[1]))

	if downDist > upDist || float64(trace.Plane.Normal[2]) < MIN_STEP_NORMAL {
		pml.origin = downO
		pml.velocity = downV
		return
	}
	//!! Special case
	// if we were walking along a plane, then we need to copy the Z over
	pml.velocity[2] = downV[2]
}

// friction handles both ground friction and water friction.
// C: qcommon/pmove.c:345 PM_Friction
func (mv *Mover) friction() {
	pm, pml := mv.pm, &mv.pml
	vel := &pml.velocity

	speed := float32(math.Sqrt(float64(float32(vel[0]*vel[0]) + float32(vel[1]*vel[1]) + float32(vel[2]*vel[2]))))
	if speed < 1 {
		vel[0] = 0
		vel[1] = 0
		return
	}

	var drop float32

	// apply ground friction
	if (!entNull(pm.GroundEntity) && pml.groundsurface != nil && pml.groundsurface.Flags&q2const.SURF_SLICK == 0) || pml.ladder {
		friction := mv.Friction
		control := speed
		if speed < mv.StopSpeed {
			control = mv.StopSpeed
		}
		drop += float32(float32(control*friction) * pml.frametime)
	}

	// apply water friction
	if pm.WaterLevel != 0 && !pml.ladder {
		drop += float32(float32(float32(speed*mv.WaterFriction)*float32(pm.WaterLevel)) * pml.frametime)
	}

	// scale the velocity
	newspeed := speed - drop
	if newspeed < 0 {
		newspeed = 0
	}
	newspeed /= speed

	vel[0] = vel[0] * newspeed
	vel[1] = vel[1] * newspeed
	vel[2] = vel[2] * newspeed
}

// accelerate handles user intended acceleration.
// C: qcommon/pmove.c:397 PM_Accelerate
func (mv *Mover) accelerate(wishdir Vec3, wishspeed, accel float32) {
	pml := &mv.pml
	currentspeed := shared.DotProduct(pml.velocity, wishdir)
	addspeed := wishspeed - currentspeed
	if addspeed <= 0 {
		return
	}
	accelspeed := float32(accel*pml.frametime) * wishspeed
	if accelspeed > addspeed {
		accelspeed = addspeed
	}
	for i := 0; i < 3; i++ {
		pml.velocity[i] += float32(accelspeed * wishdir[i])
	}
}

// airAccelerate is the pm_airaccelerate variant.
// C: qcommon/pmove.c:414 PM_AirAccelerate
func (mv *Mover) airAccelerate(wishdir Vec3, wishspeed, accel float32) {
	pml := &mv.pml
	wishspd := wishspeed
	if wishspd > 30 {
		wishspd = 30
	}
	currentspeed := shared.DotProduct(pml.velocity, wishdir)
	addspeed := wishspd - currentspeed
	if addspeed <= 0 {
		return
	}
	accelspeed := float32(accel*wishspeed) * pml.frametime
	if accelspeed > addspeed {
		accelspeed = addspeed
	}
	for i := 0; i < 3; i++ {
		pml.velocity[i] += float32(accelspeed * wishdir[i])
	}
}

// addCurrents accounts for ladders, water currents and conveyors.
// C: qcommon/pmove.c:438 PM_AddCurrents
func (mv *Mover) addCurrents(wishvel *Vec3) {
	pm, pml := mv.pm, &mv.pml
	var v Vec3

	// account for ladders
	if pml.ladder && math.Abs(float64(pml.velocity[2])) <= 200 {
		if pm.ViewAngles[q2const.PITCH] <= -15 && pm.Cmd.ForwardMove > 0 {
			wishvel[2] = 200
		} else if pm.ViewAngles[q2const.PITCH] >= 15 && pm.Cmd.ForwardMove > 0 {
			wishvel[2] = -200
		} else if pm.Cmd.UpMove > 0 {
			wishvel[2] = 200
		} else if pm.Cmd.UpMove < 0 {
			wishvel[2] = -200
		} else {
			wishvel[2] = 0
		}

		// limit horizontal speed when on a ladder
		if wishvel[0] < -25 {
			wishvel[0] = -25
		} else if wishvel[0] > 25 {
			wishvel[0] = 25
		}

		if wishvel[1] < -25 {
			wishvel[1] = -25
		} else if wishvel[1] > 25 {
			wishvel[1] = 25
		}
	}

	// add water currents
	if pm.WaterType&q2const.MASK_CURRENT != 0 {
		v = Vec3{}

		if pm.WaterType&q2const.CONTENTS_CURRENT_0 != 0 {
			v[0] += 1
		}
		if pm.WaterType&q2const.CONTENTS_CURRENT_90 != 0 {
			v[1] += 1
		}
		if pm.WaterType&q2const.CONTENTS_CURRENT_180 != 0 {
			v[0] -= 1
		}
		if pm.WaterType&q2const.CONTENTS_CURRENT_270 != 0 {
			v[1] -= 1
		}
		if pm.WaterType&q2const.CONTENTS_CURRENT_UP != 0 {
			v[2] += 1
		}
		if pm.WaterType&q2const.CONTENTS_CURRENT_DOWN != 0 {
			v[2] -= 1
		}

		s := mv.WaterSpeed
		if pm.WaterLevel == 1 && !entNull(pm.GroundEntity) {
			s /= 2
		}

		*wishvel = shared.VectorMA(*wishvel, s, v)
	}

	// add conveyor belt velocities
	if !entNull(pm.GroundEntity) {
		v = Vec3{}

		if pml.groundcontents&q2const.CONTENTS_CURRENT_0 != 0 {
			v[0] += 1
		}
		if pml.groundcontents&q2const.CONTENTS_CURRENT_90 != 0 {
			v[1] += 1
		}
		if pml.groundcontents&q2const.CONTENTS_CURRENT_180 != 0 {
			v[0] -= 1
		}
		if pml.groundcontents&q2const.CONTENTS_CURRENT_270 != 0 {
			v[1] -= 1
		}
		if pml.groundcontents&q2const.CONTENTS_CURRENT_UP != 0 {
			v[2] += 1
		}
		if pml.groundcontents&q2const.CONTENTS_CURRENT_DOWN != 0 {
			v[2] -= 1
		}

		*wishvel = shared.VectorMA(*wishvel, 100 /* pm->groundentity->speed */, v)
	}
}

// waterMove moves while swimming.
// C: qcommon/pmove.c:533 PM_WaterMove
func (mv *Mover) waterMove() {
	pm, pml := mv.pm, &mv.pml
	var wishvel Vec3

	// user intentions
	fm, sm := float32(pm.Cmd.ForwardMove), float32(pm.Cmd.SideMove)
	for i := 0; i < 3; i++ {
		wishvel[i] = float32(pml.forward[i]*fm) + float32(pml.right[i]*sm)
	}

	if pm.Cmd.ForwardMove == 0 && pm.Cmd.SideMove == 0 && pm.Cmd.UpMove == 0 {
		wishvel[2] -= 60 // drift towards bottom
	} else {
		wishvel[2] += float32(pm.Cmd.UpMove)
	}

	mv.addCurrents(&wishvel)

	wishdir := wishvel
	wishspeed := shared.VectorNormalize(&wishdir)

	if wishspeed > mv.MaxSpeed {
		wishvel = shared.VectorScale(wishvel, mv.MaxSpeed/wishspeed)
		wishspeed = mv.MaxSpeed
	}
	wishspeed = float32(float64(wishspeed) * 0.5)

	mv.accelerate(wishdir, wishspeed, mv.WaterAccelerate)

	mv.stepSlideMove()
}

// airMove moves on the ground, on ladders and in the air.
// C: qcommon/pmove.c:575 PM_AirMove
func (mv *Mover) airMove() {
	pm, pml := mv.pm, &mv.pml
	var wishvel Vec3

	fmove := float32(pm.Cmd.ForwardMove)
	smove := float32(pm.Cmd.SideMove)

	for i := 0; i < 2; i++ {
		wishvel[i] = float32(pml.forward[i]*fmove) + float32(pml.right[i]*smove)
	}
	wishvel[2] = 0

	mv.addCurrents(&wishvel)

	wishdir := wishvel
	wishspeed := shared.VectorNormalize(&wishdir)

	// clamp to server defined max speed
	maxspeed := mv.MaxSpeed
	if pm.S.PmFlags&q2const.PMF_DUCKED != 0 {
		maxspeed = mv.DuckSpeed
	}

	if wishspeed > maxspeed {
		wishvel = shared.VectorScale(wishvel, maxspeed/wishspeed)
		wishspeed = maxspeed
	}

	gravity := float32(pm.S.Gravity)
	if pml.ladder {
		mv.accelerate(wishdir, wishspeed, mv.Accelerate)
		if wishvel[2] == 0 {
			if pml.velocity[2] > 0 {
				pml.velocity[2] -= float32(gravity * pml.frametime)
				if pml.velocity[2] < 0 {
					pml.velocity[2] = 0
				}
			} else {
				pml.velocity[2] += float32(gravity * pml.frametime)
				if pml.velocity[2] > 0 {
					pml.velocity[2] = 0
				}
			}
		}
		mv.stepSlideMove()
	} else if !entNull(pm.GroundEntity) { // walking on ground
		pml.velocity[2] = 0 //!!! this is before the accel
		mv.accelerate(wishdir, wishspeed, mv.Accelerate)

		// PGM	-- fix for negative trigger_gravity fields
		if pm.S.Gravity > 0 {
			pml.velocity[2] = 0
		} else {
			pml.velocity[2] -= float32(gravity * pml.frametime)
		}

		if pml.velocity[0] == 0 && pml.velocity[1] == 0 {
			return
		}
		mv.stepSlideMove()
	} else { // not on ground, so little effect on velocity
		if mv.AirAccelerate != 0 {
			mv.airAccelerate(wishdir, wishspeed, mv.Accelerate)
		} else {
			mv.accelerate(wishdir, wishspeed, 1)
		}
		// add gravity
		pml.velocity[2] -= float32(gravity * pml.frametime)
		mv.stepSlideMove()
	}
}

// catagorizePosition sets groundentity, watertype and waterlevel.
// C: qcommon/pmove.c:671 PM_CatagorizePosition
func (mv *Mover) catagorizePosition() {
	pm, pml := mv.pm, &mv.pml
	var point Vec3

	// if the player hull point one unit down is solid, the player
	// is on ground

	// see if standing on something solid
	point[0] = pml.origin[0]
	point[1] = pml.origin[1]
	point[2] = float32(float64(pml.origin[2]) - 0.25)
	if pml.velocity[2] > 180 { //!!ZOID changed from 100 to 180 (ramp accel)
		pm.S.PmFlags &^= q2const.PMF_ON_GROUND
		pm.GroundEntity = NoEnt
	} else {
		trace := pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &point)
		pml.groundplane = trace.Plane
		pml.groundsurface = trace.Surface
		pml.groundcontents = trace.Contents

		if entNull(trace.Ent) || (float64(trace.Plane.Normal[2]) < 0.7 && !trace.StartSolid) {
			pm.GroundEntity = NoEnt
			pm.S.PmFlags &^= q2const.PMF_ON_GROUND
		} else {
			pm.GroundEntity = trace.Ent

			// hitting solid ground will end a waterjump
			if pm.S.PmFlags&q2const.PMF_TIME_WATERJUMP != 0 {
				pm.S.PmFlags &^= q2const.PMF_TIME_WATERJUMP | q2const.PMF_TIME_LAND | q2const.PMF_TIME_TELEPORT
				pm.S.PmTime = 0
			}

			if pm.S.PmFlags&q2const.PMF_ON_GROUND == 0 { // just hit the ground
				pm.S.PmFlags |= q2const.PMF_ON_GROUND
				// don't do landing time if we were just going down a slope
				if pml.velocity[2] < -200 {
					pm.S.PmFlags |= q2const.PMF_TIME_LAND
					// don't allow another jump for a little while
					if pml.velocity[2] < -400 {
						pm.S.PmTime = 25
					} else {
						pm.S.PmTime = 18
					}
				}
			}
		}

		if pm.NumTouch < q2const.MAXTOUCH && !entNull(trace.Ent) {
			pm.TouchEnts[pm.NumTouch] = trace.Ent
			pm.NumTouch++
		}
	}

	// get waterlevel, accounting for ducking
	pm.WaterLevel = 0
	pm.WaterType = 0

	sample2 := int32(pm.ViewHeight - pm.Mins[2])
	sample1 := sample2 / 2

	point[2] = pml.origin[2] + pm.Mins[2] + 1
	cont := pm.PointContents(point)

	if cont&q2const.MASK_WATER != 0 {
		pm.WaterType = cont
		pm.WaterLevel = 1
		point[2] = pml.origin[2] + pm.Mins[2] + float32(sample1)
		cont = pm.PointContents(point)
		if cont&q2const.MASK_WATER != 0 {
			pm.WaterLevel = 2
			point[2] = pml.origin[2] + pm.Mins[2] + float32(sample2)
			cont = pm.PointContents(point)
			if cont&q2const.MASK_WATER != 0 {
				pm.WaterLevel = 3
			}
		}
	}
}

// checkJump starts a jump or a swim-up.
// C: qcommon/pmove.c:778 PM_CheckJump
func (mv *Mover) checkJump() {
	pm, pml := mv.pm, &mv.pml
	if pm.S.PmFlags&q2const.PMF_TIME_LAND != 0 {
		// hasn't been long enough since landing to jump again
		return
	}

	if pm.Cmd.UpMove < 10 { // not holding jump
		pm.S.PmFlags &^= q2const.PMF_JUMP_HELD
		return
	}

	// must wait for jump to be released
	if pm.S.PmFlags&q2const.PMF_JUMP_HELD != 0 {
		return
	}

	if pm.S.PmType == q2const.PM_DEAD {
		return
	}

	if pm.WaterLevel >= 2 { // swimming, not jumping
		pm.GroundEntity = NoEnt

		if pml.velocity[2] <= -300 {
			return
		}

		if pm.WaterType == q2const.CONTENTS_WATER {
			pml.velocity[2] = 100
		} else if pm.WaterType == q2const.CONTENTS_SLIME {
			pml.velocity[2] = 80
		} else {
			pml.velocity[2] = 50
		}
		return
	}

	if entNull(pm.GroundEntity) {
		return // in air, so no effect
	}

	pm.S.PmFlags |= q2const.PMF_JUMP_HELD

	pm.GroundEntity = NoEnt
	pml.velocity[2] += 270
	if pml.velocity[2] < 270 {
		pml.velocity[2] = 270
	}
}

// checkSpecialMovement checks for ladders and water jumps.
// C: qcommon/pmove.c:831 PM_CheckSpecialMovement
func (mv *Mover) checkSpecialMovement() {
	pm, pml := mv.pm, &mv.pml
	var flatforward Vec3

	if pm.S.PmTime != 0 {
		return
	}

	pml.ladder = false

	// check for ladder
	flatforward[0] = pml.forward[0]
	flatforward[1] = pml.forward[1]
	flatforward[2] = 0
	shared.VectorNormalize(&flatforward)

	spot := shared.VectorMA(pml.origin, 1, flatforward)
	trace := pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &spot)
	if trace.Fraction < 1 && trace.Contents&q2const.CONTENTS_LADDER != 0 {
		pml.ladder = true
	}

	// check for water jump
	if pm.WaterLevel != 2 {
		return
	}

	spot = shared.VectorMA(pml.origin, 30, flatforward)
	spot[2] += 4
	cont := pm.PointContents(spot)
	if cont&q2const.CONTENTS_SOLID == 0 {
		return
	}

	spot[2] += 16
	cont = pm.PointContents(spot)
	if cont != 0 {
		return
	}
	// jump out of water
	pml.velocity = shared.VectorScale(flatforward, 50)
	pml.velocity[2] = 350

	pm.S.PmFlags |= q2const.PMF_TIME_WATERJUMP
	pm.S.PmTime = 255
}

// flyMove is spectator movement (doclip false) or noclip-free flying.
// C: qcommon/pmove.c:882 PM_FlyMove
func (mv *Mover) flyMove(doclip bool) {
	pm, pml := mv.pm, &mv.pml
	var wishvel, end Vec3

	pm.ViewHeight = 22

	// friction
	speed := shared.VectorLength(pml.velocity)
	if speed < 1 {
		pml.velocity = shared.Vec3Origin
	} else {
		var drop float32

		friction := float32(float64(mv.Friction) * 1.5) // extra friction
		control := speed
		if speed < mv.StopSpeed {
			control = mv.StopSpeed
		}
		drop += float32(float32(control*friction) * pml.frametime)

		// scale the velocity
		newspeed := speed - drop
		if newspeed < 0 {
			newspeed = 0
		}
		newspeed /= speed

		pml.velocity = shared.VectorScale(pml.velocity, newspeed)
	}

	// accelerate
	fmove := float32(pm.Cmd.ForwardMove)
	smove := float32(pm.Cmd.SideMove)

	shared.VectorNormalize(&pml.forward)
	shared.VectorNormalize(&pml.right)

	for i := 0; i < 3; i++ {
		wishvel[i] = float32(pml.forward[i]*fmove) + float32(pml.right[i]*smove)
	}
	wishvel[2] += float32(pm.Cmd.UpMove)

	wishdir := wishvel
	wishspeed := shared.VectorNormalize(&wishdir)

	// clamp to server defined max speed
	if wishspeed > mv.MaxSpeed {
		wishvel = shared.VectorScale(wishvel, mv.MaxSpeed/wishspeed)
		wishspeed = mv.MaxSpeed
	}

	currentspeed := shared.DotProduct(pml.velocity, wishdir)
	addspeed := wishspeed - currentspeed
	if addspeed <= 0 {
		return
	}
	accelspeed := float32(mv.Accelerate*pml.frametime) * wishspeed
	if accelspeed > addspeed {
		accelspeed = addspeed
	}

	for i := 0; i < 3; i++ {
		pml.velocity[i] += float32(accelspeed * wishdir[i])
	}

	if doclip {
		for i := 0; i < 3; i++ {
			end[i] = pml.origin[i] + float32(pml.frametime*pml.velocity[i])
		}

		trace := pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &end)

		pml.origin = trace.EndPos
	} else {
		// move
		pml.origin = shared.VectorMA(pml.origin, pml.frametime, pml.velocity)
	}
}

// checkDuck sets mins, maxs, and pm->viewheight.
// C: qcommon/pmove.c:976 PM_CheckDuck
func (mv *Mover) checkDuck() {
	pm, pml := mv.pm, &mv.pml

	pm.Mins[0] = -16
	pm.Mins[1] = -16

	pm.Maxs[0] = 16
	pm.Maxs[1] = 16

	if pm.S.PmType == q2const.PM_GIB {
		pm.Mins[2] = 0
		pm.Maxs[2] = 16
		pm.ViewHeight = 8
		return
	}

	pm.Mins[2] = -24

	if pm.S.PmType == q2const.PM_DEAD {
		pm.S.PmFlags |= q2const.PMF_DUCKED
	} else if pm.Cmd.UpMove < 0 && pm.S.PmFlags&q2const.PMF_ON_GROUND != 0 { // duck
		pm.S.PmFlags |= q2const.PMF_DUCKED
	} else { // stand up if possible
		if pm.S.PmFlags&q2const.PMF_DUCKED != 0 {
			// try to stand up
			pm.Maxs[2] = 32
			trace := pm.Trace(&pml.origin, &pm.Mins, &pm.Maxs, &pml.origin)
			if !trace.AllSolid {
				pm.S.PmFlags &^= q2const.PMF_DUCKED
			}
		}
	}

	if pm.S.PmFlags&q2const.PMF_DUCKED != 0 {
		pm.Maxs[2] = 4
		pm.ViewHeight = -2
	} else {
		pm.Maxs[2] = 32
		pm.ViewHeight = 22
	}
}

// deadMove applies extra friction to a dead player on the ground.
// C: qcommon/pmove.c:1034 PM_DeadMove
func (mv *Mover) deadMove() {
	pm, pml := mv.pm, &mv.pml

	if entNull(pm.GroundEntity) {
		return
	}

	// extra friction
	forward := shared.VectorLength(pml.velocity)
	forward -= 20
	if forward <= 0 {
		pml.velocity = Vec3{}
	} else {
		shared.VectorNormalize(&pml.velocity)
		pml.velocity = shared.VectorScale(pml.velocity, forward)
	}
}

// goodPosition tests the quantized origin.
// C: qcommon/pmove.c:1057 PM_GoodPosition
func (mv *Mover) goodPosition() bool {
	pm := mv.pm
	var origin, end Vec3

	if pm.S.PmType == q2const.PM_SPECTATOR {
		return true
	}

	for i := 0; i < 3; i++ {
		origin[i] = float32(float64(pm.S.Origin[i]) * 0.125)
		end[i] = origin[i]
	}
	trace := pm.Trace(&origin, &pm.Mins, &pm.Maxs, &end)

	return !trace.AllSolid
}

// jitterbits tries all single bits first.
// C: qcommon/pmove.c:1087 jitterbits
var jitterbits = [8]int{0, 4, 1, 2, 3, 5, 6, 7}

// snapPosition quantizes origin and velocity to the 0.125 network
// precision, jittering into a valid position.
// C: qcommon/pmove.c:1081 PM_SnapPosition
func (mv *Mover) snapPosition() {
	pm, pml := mv.pm, &mv.pml
	var sign [3]int32

	// snap velocity to eigths
	for i := 0; i < 3; i++ {
		pm.S.Velocity[i] = int16(int32(pml.velocity[i] * 8))
	}

	for i := 0; i < 3; i++ {
		if pml.origin[i] >= 0 {
			sign[i] = 1
		} else {
			sign[i] = -1
		}
		pm.S.Origin[i] = int16(int32(pml.origin[i] * 8))
		if float64(pm.S.Origin[i])*0.125 == float64(pml.origin[i]) {
			sign[i] = 0
		}
	}
	base := pm.S.Origin

	// try all combinations
	for j := 0; j < 8; j++ {
		bits := jitterbits[j]
		pm.S.Origin = base
		for i := 0; i < 3; i++ {
			if bits&(1<<i) != 0 {
				pm.S.Origin[i] = int16(int32(pm.S.Origin[i]) + sign[i])
			}
		}

		if mv.goodPosition() {
			return
		}
	}

	// go back to the last position
	for i := 0; i < 3; i++ {
		pm.S.Origin[i] = int16(int32(pml.previousOrigin[i]))
	}
}

// initialSnapOffset is the static offset table of PM_InitialSnapPosition.
// C: qcommon/pmove.c:1172 offset
var initialSnapOffset = [3]int32{0, -1, 1}

// initialSnapPosition finds a valid position near the given origin.
// C: qcommon/pmove.c:1168 PM_InitialSnapPosition
func (mv *Mover) initialSnapPosition() {
	pm, pml := mv.pm, &mv.pml
	base := pm.S.Origin

	for z := 0; z < 3; z++ {
		pm.S.Origin[2] = int16(int32(base[2]) + initialSnapOffset[z])
		for y := 0; y < 3; y++ {
			pm.S.Origin[1] = int16(int32(base[1]) + initialSnapOffset[y])
			for x := 0; x < 3; x++ {
				pm.S.Origin[0] = int16(int32(base[0]) + initialSnapOffset[x])
				if mv.goodPosition() {
					pml.origin[0] = float32(float64(pm.S.Origin[0]) * 0.125)
					pml.origin[1] = float32(float64(pm.S.Origin[1]) * 0.125)
					pml.origin[2] = float32(float64(pm.S.Origin[2]) * 0.125)
					for i := 0; i < 3; i++ {
						pml.previousOrigin[i] = float32(pm.S.Origin[i])
					}
					return
				}
			}
		}
	}
	// Com_DPrintf ("Bad InitialSnapPosition\n");
}

// clampAngles computes the view angles from the command and deltas.
// C: qcommon/pmove.c:1204 PM_ClampAngles
func (mv *Mover) clampAngles() {
	pm, pml := mv.pm, &mv.pml

	if pm.S.PmFlags&q2const.PMF_TIME_TELEPORT != 0 {
		// the sum is an int here, not wrapped to short
		pm.ViewAngles[q2const.YAW] = float32(shared.SHORT2ANGLE(int32(pm.Cmd.Angles[q2const.YAW]) + int32(pm.S.DeltaAngles[q2const.YAW])))
		pm.ViewAngles[q2const.PITCH] = 0
		pm.ViewAngles[q2const.ROLL] = 0
	} else {
		// circularly clamp the angles with deltas
		for i := 0; i < 3; i++ {
			temp := pm.Cmd.Angles[i] + pm.S.DeltaAngles[i] // short: wraps
			pm.ViewAngles[i] = float32(shared.SHORT2ANGLE(int32(temp)))
		}

		// don't let the player look up or down more than 90 degrees
		if pm.ViewAngles[q2const.PITCH] > 89 && pm.ViewAngles[q2const.PITCH] < 180 {
			pm.ViewAngles[q2const.PITCH] = 89
		} else if pm.ViewAngles[q2const.PITCH] < 271 && pm.ViewAngles[q2const.PITCH] >= 180 {
			pm.ViewAngles[q2const.PITCH] = 271
		}
	}
	shared.AngleVectors(pm.ViewAngles, &pml.forward, &pml.right, &pml.up)
}

// Pmove runs one player move. It can be called by either the server or
// the client.
// C: qcommon/pmove.c:1240 Pmove
func (mv *Mover) Pmove(pmove *PmoveT) {
	mv.pm = pmove
	defer func() { mv.pm = nil }()
	pm := pmove

	// clear results
	pm.NumTouch = 0
	pm.ViewAngles = Vec3{}
	pm.ViewHeight = 0
	pm.GroundEntity = NoEnt
	pm.WaterType = 0
	pm.WaterLevel = 0

	// clear all pmove local vars
	mv.pml = pml{}
	pml := &mv.pml

	// convert origin and velocity to float values
	pml.origin[0] = float32(float64(pm.S.Origin[0]) * 0.125)
	pml.origin[1] = float32(float64(pm.S.Origin[1]) * 0.125)
	pml.origin[2] = float32(float64(pm.S.Origin[2]) * 0.125)

	pml.velocity[0] = float32(float64(pm.S.Velocity[0]) * 0.125)
	pml.velocity[1] = float32(float64(pm.S.Velocity[1]) * 0.125)
	pml.velocity[2] = float32(float64(pm.S.Velocity[2]) * 0.125)

	// save old org in case we get stuck
	for i := 0; i < 3; i++ {
		pml.previousOrigin[i] = float32(pm.S.Origin[i])
	}

	pml.frametime = float32(float64(pm.Cmd.Msec) * 0.001)

	mv.clampAngles()

	if pm.S.PmType == q2const.PM_SPECTATOR {
		mv.flyMove(false)
		mv.snapPosition()
		return
	}

	if pm.S.PmType >= q2const.PM_DEAD {
		pm.Cmd.ForwardMove = 0
		pm.Cmd.SideMove = 0
		pm.Cmd.UpMove = 0
	}

	if pm.S.PmType == q2const.PM_FREEZE {
		return // no movement at all
	}

	// set mins, maxs, and viewheight
	mv.checkDuck()

	if pm.SnapInitial {
		mv.initialSnapPosition()
	}

	// set groundentity, watertype, and waterlevel
	mv.catagorizePosition()

	if pm.S.PmType == q2const.PM_DEAD {
		mv.deadMove()
	}

	mv.checkSpecialMovement()

	// drop timing counter
	if pm.S.PmTime != 0 {
		msec := int32(pm.Cmd.Msec >> 3)
		if msec == 0 {
			msec = 1
		}
		if msec >= int32(pm.S.PmTime) {
			pm.S.PmFlags &^= q2const.PMF_TIME_WATERJUMP | q2const.PMF_TIME_LAND | q2const.PMF_TIME_TELEPORT
			pm.S.PmTime = 0
		} else {
			pm.S.PmTime -= uint8(msec)
		}
	}

	if pm.S.PmFlags&q2const.PMF_TIME_TELEPORT != 0 {
		// teleport pause stays exactly in place
	} else if pm.S.PmFlags&q2const.PMF_TIME_WATERJUMP != 0 {
		// waterjump has no control, but falls
		pml.velocity[2] -= float32(float32(pm.S.Gravity) * pml.frametime)
		if pml.velocity[2] < 0 { // cancel as soon as we are falling down again
			pm.S.PmFlags &^= q2const.PMF_TIME_WATERJUMP | q2const.PMF_TIME_LAND | q2const.PMF_TIME_TELEPORT
			pm.S.PmTime = 0
		}

		mv.stepSlideMove()
	} else {
		mv.checkJump()

		mv.friction()

		if pm.WaterLevel >= 2 {
			mv.waterMove()
		} else {
			angles := pm.ViewAngles
			if angles[q2const.PITCH] > 180 {
				angles[q2const.PITCH] = angles[q2const.PITCH] - 360
			}
			angles[q2const.PITCH] /= 3

			shared.AngleVectors(angles, &pml.forward, &pml.right, &pml.up)

			mv.airMove()
		}
	}

	// set groundentity, watertype, and waterlevel for final spot
	mv.catagorizePosition()

	mv.snapPosition()
}
