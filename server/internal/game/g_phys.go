package game

// Port of game/g_phys.c.
//
// pushmove objects do not obey gravity, and do not interact with each other or
// trigger fields, but block normal movement and push normal objects when they
// move.
//
// onground is set for toss objects when they come to a complete rest.  it is
// set for steping or walking objects
//
// doors, plats, etc are SOLID_BSP, and MOVETYPE_PUSH
// bonus items are SOLID_TRIGGER touch, and MOVETYPE_TOSS
// corpses are SOLID_NOT and MOVETYPE_TOSS
// crates are SOLID_BBOX and MOVETYPE_TOSS
// walking monsters are SOLID_SLIDEBOX and MOVETYPE_STEP
// flying/floating monsters are SOLID_SLIDEBOX and MOVETYPE_FLY
//
// solid_edge items only clip against bsp models.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_phys.c:49 SV_TestEntityPosition
func (g *Game) SV_TestEntityPosition(ent *Edict) *Edict {
	var mask int32
	if ent.ClipMask != 0 {
		mask = ent.ClipMask
	} else {
		mask = MASK_SOLID
	}
	trace := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &ent.S.Origin, ent, mask)

	if trace.StartSolid {
		return &g.edicts[0]
	}

	return nil
}

// C: game/g_phys.c:72 SV_CheckVelocity
func (g *Game) SV_CheckVelocity(ent *Edict) {
	//
	// bound velocity
	//
	for i := 0; i < 3; i++ {
		if ent.Velocity[i] > g.sv_maxvelocity.Value {
			ent.Velocity[i] = g.sv_maxvelocity.Value
		} else if ent.Velocity[i] < -g.sv_maxvelocity.Value {
			ent.Velocity[i] = -g.sv_maxvelocity.Value
		}
	}
}

// SV_RunThink runs thinking code for this frame if necessary.
// C: game/g_phys.c:95 SV_RunThink
func (g *Game) SV_RunThink(ent *Edict) bool {
	thinktime := ent.Nextthink
	if thinktime <= 0 {
		return true
	}
	if float64(thinktime) > float64(g.level.Time)+0.001 {
		return true
	}

	ent.Nextthink = 0
	if ent.Think == nil {
		g.gi.Error("NULL ent->think")
	}
	ent.Think.fn(g, ent)

	return false
}

// SV_Impact: two entities have touched, so run their touch functions.
// C: game/g_phys.c:120 SV_Impact
func (g *Game) SV_Impact(e1 *Edict, trace *Trace) {
	e2 := trace.Ent

	if e1.Touch != nil && e1.Solid != SOLID_NOT {
		e1.Touch.fn(g, e1, e2, &trace.Plane, trace.Surface)
	}

	if e2.Touch != nil && e2.Solid != SOLID_NOT {
		e2.Touch.fn(g, e2, e1, nil, nil)
	}
}

// STOP_EPSILON is a double literal.
// C: game/g_phys.c:143 STOP_EPSILON
const STOP_EPSILON = 0.1

// ClipVelocity slides off of the impacting object.
// returns the blocked flags (1 = floor, 2 = step / wall)
// C: game/g_phys.c:145 ClipVelocity
func ClipVelocity(in, normal Vec3, out *Vec3, overbounce float32) int32 {
	var blocked int32
	if normal[2] > 0 {
		blocked |= 1 // floor
	}
	if normal[2] == 0 {
		blocked |= 2 // step
	}

	backoff := shared.DotProduct(in, normal) * overbounce

	for i := 0; i < 3; i++ {
		change := normal[i] * backoff
		out[i] = in[i] - change
		if float64(out[i]) > -STOP_EPSILON && float64(out[i]) < STOP_EPSILON {
			out[i] = 0
		}
	}

	return blocked
}

// C: game/g_phys.c:182 MAX_CLIP_PLANES
const MAX_CLIP_PLANES = 5

// SV_FlyMove is the basic solid body movement clip that slides along multiple planes.
// Returns the clipflags if the velocity was modified (hit something solid)
// 1 = floor
// 2 = wall / step
// 4 = dead stop
// C: game/g_phys.c:183 SV_FlyMove
func (g *Game) SV_FlyMove(ent *Edict, time float32, mask int32) int32 {
	var (
		dir                                              Vec3
		planes                                           [MAX_CLIP_PLANES]Vec3
		primal_velocity, original_velocity, new_velocity Vec3
		end                                              Vec3
		i, j                                             int
	)

	numbumps := 4

	var blocked int32
	original_velocity = ent.Velocity
	primal_velocity = ent.Velocity
	numplanes := 0

	time_left := time

	ent.Groundentity = nil
	for bumpcount := 0; bumpcount < numbumps; bumpcount++ {
		for i = 0; i < 3; i++ {
			end[i] = ent.S.Origin[i] + time_left*ent.Velocity[i]
		}

		trace := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &end, ent, mask)

		if trace.AllSolid { // entity is trapped in another solid
			ent.Velocity = shared.Vec3Origin
			return 3
		}

		if trace.Fraction > 0 { // actually covered some distance
			ent.S.Origin = trace.EndPos
			original_velocity = ent.Velocity
			numplanes = 0
		}

		if trace.Fraction == 1 {
			break // moved the entire distance
		}

		hit := trace.Ent

		if float64(trace.Plane.Normal[2]) > 0.7 {
			blocked |= 1 // floor
			if hit.Solid == SOLID_BSP {
				ent.Groundentity = hit
				ent.GroundentityLinkcount = hit.LinkCount
			}
		}
		if trace.Plane.Normal[2] == 0 {
			blocked |= 2 // step
		}

		//
		// run the impact function
		//
		g.SV_Impact(ent, &trace)
		if !ent.InUse {
			break // removed by the impact function
		}

		time_left -= time_left * trace.Fraction

		// cliped to another plane
		if numplanes >= MAX_CLIP_PLANES { // this shouldn't really happen
			ent.Velocity = shared.Vec3Origin
			return 3
		}

		planes[numplanes] = trace.Plane.Normal
		numplanes++

		//
		// modify original_velocity so it parallels all of the clip planes
		//
		for i = 0; i < numplanes; i++ {
			ClipVelocity(original_velocity, planes[i], &new_velocity, 1)

			for j = 0; j < numplanes; j++ {
				if (j != i) && shared.VectorCompare(planes[i], planes[j]) == 0 {
					if shared.DotProduct(new_velocity, planes[j]) < 0 {
						break // not ok
					}
				}
			}
			if j == numplanes {
				break
			}
		}

		if i != numplanes { // go along this plane
			ent.Velocity = new_velocity
		} else { // go along the crease
			if numplanes != 2 {
				//				gi.dprintf ("clip velocity, numplanes == %i\n",numplanes);
				ent.Velocity = shared.Vec3Origin
				return 7
			}
			dir = shared.CrossProduct(planes[0], planes[1])
			d := shared.DotProduct(dir, ent.Velocity)
			ent.Velocity = shared.VectorScale(dir, d)
		}

		//
		// if original velocity is against the original velocity, stop dead
		// to avoid tiny occilations in sloping corners
		//
		if shared.DotProduct(ent.Velocity, primal_velocity) <= 0 {
			ent.Velocity = shared.Vec3Origin
			return blocked
		}
	}

	return blocked
}

// C: game/g_phys.c:322 SV_AddGravity
func (g *Game) SV_AddGravity(ent *Edict) {
	ent.Velocity[2] = float32(float64(ent.Velocity[2]) - float64(ent.Gravity*g.sv_gravity.Value)*FRAMETIME)
}

// SV_PushEntity does not change the entities velocity at all.
// C: game/g_phys.c:342 SV_PushEntity
func (g *Game) SV_PushEntity(ent *Edict, push Vec3) Trace {
	var trace Trace
	var mask int32

	start := ent.S.Origin
	end := shared.VectorAdd(start, push)

retry:
	if ent.ClipMask != 0 {
		mask = ent.ClipMask
	} else {
		mask = MASK_SOLID
	}

	trace = g.gi.Trace(&start, &ent.Mins, &ent.Maxs, &end, ent, mask)

	ent.S.Origin = trace.EndPos
	g.gi.LinkEntity(ent)

	if trace.Fraction != 1.0 {
		g.SV_Impact(ent, &trace)

		// if the pushed entity went away and the pusher is still there
		if !trace.Ent.InUse && ent.InUse {
			// move the pusher back and try again
			ent.S.Origin = start
			g.gi.LinkEntity(ent)
			goto retry
		}
	}

	if ent.InUse {
		g.G_TouchTriggers(ent)
	}

	return trace
}

// pushed_t records an entity moved by a pusher.
// C: game/g_phys.c:384 pushed_t
type pushed_t struct {
	ent      *Edict
	origin   Vec3
	angles   Vec3
	deltayaw float32
}

// SV_Push: objects need to be moved back on a failed push,
// otherwise riders would continue to slide.
// C: game/g_phys.c:403 SV_Push
func (g *Game) SV_Push(pusher *Edict, move, amove *Vec3) bool {
	var mins, maxs, org, org2, move2, forward, right, up Vec3

	// clamp the move to 1/8 units, so the position will
	// be accurate for client side prediction
	for i := 0; i < 3; i++ {
		var temp float32
		temp = float32(float64(move[i]) * 8.0)
		if float64(temp) > 0.0 {
			temp = float32(float64(temp) + 0.5)
		} else {
			temp = float32(float64(temp) - 0.5)
		}
		move[i] = float32(0.125 * float64(int32(temp)))
	}

	// find the bounding box
	for i := 0; i < 3; i++ {
		mins[i] = pusher.AbsMin[i] + move[i]
		maxs[i] = pusher.AbsMax[i] + move[i]
	}

	// we need this for pushing things later
	org = shared.VectorSubtract(shared.Vec3Origin, *amove)
	shared.AngleVectors(org, &forward, &right, &up)

	// save the pusher's original position
	p := &g.pushed[g.pushed_p]
	p.ent = pusher
	p.origin = pusher.S.Origin
	p.angles = pusher.S.Angles
	if pusher.Client != nil {
		p.deltayaw = float32(pusher.Client.PS.PMove.DeltaAngles[YAW])
	}
	g.pushed_p++

	// move the pusher to it's final position
	pusher.S.Origin = shared.VectorAdd(pusher.S.Origin, *move)
	pusher.S.Angles = shared.VectorAdd(pusher.S.Angles, *amove)
	g.gi.LinkEntity(pusher)

	// see if any solid entities are inside the final position
	for e := 1; e < int(g.num_edicts); e++ {
		check := &g.edicts[e]
		if !check.InUse {
			continue
		}
		if check.Movetype == MOVETYPE_PUSH ||
			check.Movetype == MOVETYPE_STOP ||
			check.Movetype == MOVETYPE_NONE ||
			check.Movetype == MOVETYPE_NOCLIP {
			continue
		}

		if check.Area.Prev == nil {
			continue // not linked in anywhere
		}

		// if the entity is standing on the pusher, it will definitely be moved
		if check.Groundentity != pusher {
			// see if the ent needs to be tested
			if check.AbsMin[0] >= maxs[0] ||
				check.AbsMin[1] >= maxs[1] ||
				check.AbsMin[2] >= maxs[2] ||
				check.AbsMax[0] <= mins[0] ||
				check.AbsMax[1] <= mins[1] ||
				check.AbsMax[2] <= mins[2] {
				continue
			}

			// see if the ent's bbox is inside the pusher's final position
			if g.SV_TestEntityPosition(check) == nil {
				continue
			}
		}

		if (pusher.Movetype == MOVETYPE_PUSH) || (check.Groundentity == pusher) {
			// move this entity
			p := &g.pushed[g.pushed_p]
			p.ent = check
			p.origin = check.S.Origin
			p.angles = check.S.Angles
			g.pushed_p++

			// try moving the contacted entity
			check.S.Origin = shared.VectorAdd(check.S.Origin, *move)
			if check.Client != nil { // FIXME: doesn't rotate monsters?
				// short += float: computed in float, converted back to short
				check.Client.PS.PMove.DeltaAngles[YAW] = int16(int32(float32(check.Client.PS.PMove.DeltaAngles[YAW]) + amove[YAW]))
			}

			// figure movement due to the pusher's amove
			org = shared.VectorSubtract(check.S.Origin, pusher.S.Origin)
			org2[0] = shared.DotProduct(org, forward)
			org2[1] = -shared.DotProduct(org, right)
			org2[2] = shared.DotProduct(org, up)
			move2 = shared.VectorSubtract(org2, org)
			check.S.Origin = shared.VectorAdd(check.S.Origin, move2)

			// may have pushed them off an edge
			if check.Groundentity != pusher {
				check.Groundentity = nil
			}

			block := g.SV_TestEntityPosition(check)
			if block == nil { // pushed ok
				g.gi.LinkEntity(check)
				// impact?
				continue
			}

			// if it is ok to leave in the old position, do it
			// this is only relevent for riding entities, not pushed
			// FIXME: this doesn't acount for rotation
			check.S.Origin = shared.VectorSubtract(check.S.Origin, *move)
			block = g.SV_TestEntityPosition(check)
			if block == nil {
				g.pushed_p--
				continue
			}
		}

		// save off the obstacle so we can call the block function
		g.obstacle = check

		// move back any entities we already moved
		// go backwards, so if the same entity was pushed
		// twice, it goes back to the original position
		for pi := g.pushed_p - 1; pi >= 0; pi-- {
			p := &g.pushed[pi]
			p.ent.S.Origin = p.origin
			p.ent.S.Angles = p.angles
			if p.ent.Client != nil {
				p.ent.Client.PS.PMove.DeltaAngles[YAW] = int16(int32(p.deltayaw))
			}
			g.gi.LinkEntity(p.ent)
		}
		return false
	}

	//FIXME: is there a better way to handle this?
	// see if anything we moved has touched a trigger
	for pi := g.pushed_p - 1; pi >= 0; pi-- {
		g.G_TouchTriggers(g.pushed[pi].ent)
	}

	return true
}

// SV_Physics_Pusher: bmodel objects don't interact with each other, but
// push all box objects.
// C: game/g_phys.c:562 SV_Physics_Pusher
func (g *Game) SV_Physics_Pusher(ent *Edict) {
	var move, amove Vec3
	var part *Edict

	// if not a team captain, so movement will be handled elsewhere
	if ent.Flags&FL_TEAMSLAVE != 0 {
		return
	}

	// make sure all team slaves can move before commiting
	// any moves or calling any think functions
	// if the move is blocked, all moved objects will be backed out
	//retry:
	g.pushed_p = 0
	for part = ent; part != nil; part = part.Teamchain {
		if part.Velocity[0] != 0 || part.Velocity[1] != 0 || part.Velocity[2] != 0 ||
			part.Avelocity[0] != 0 || part.Avelocity[1] != 0 || part.Avelocity[2] != 0 {
			// object is moving
			// VectorScale is a function taking a float scale
			move = shared.VectorScale(part.Velocity, float32(FRAMETIME))
			amove = shared.VectorScale(part.Avelocity, float32(FRAMETIME))

			if !g.SV_Push(part, &move, &amove) {
				break // move was blocked
			}
		}
	}
	if g.pushed_p > MAX_EDICTS {
		g.gi.Error("pushed_p > &pushed[MAX_EDICTS], memory corrupted")
	}

	if part != nil {
		// the move failed, bump all nextthink times and back out moves
		for mv := ent; mv != nil; mv = mv.Teamchain {
			if mv.Nextthink > 0 {
				mv.Nextthink = float32(float64(mv.Nextthink) + FRAMETIME)
			}
		}

		// if the pusher has a "blocked" function, call it
		// otherwise, just stay in place until the obstacle is gone
		if part.Blocked != nil {
			part.Blocked.fn(g, part, g.obstacle)
		}
	} else {
		// the move succeeded, so call all think functions
		for part = ent; part != nil; part = part.Teamchain {
			g.SV_RunThink(part)
		}
	}
}

//==================================================================

// SV_Physics_None: non moving objects can only think.
// C: game/g_phys.c:630 SV_Physics_None
func (g *Game) SV_Physics_None(ent *Edict) {
	// regular thinking
	g.SV_RunThink(ent)
}

// SV_Physics_Noclip: a moving object that doesn't obey physics.
// C: game/g_phys.c:643 SV_Physics_Noclip
func (g *Game) SV_Physics_Noclip(ent *Edict) {
	// regular thinking
	if !g.SV_RunThink(ent) {
		return
	}

	ent.S.Angles = shared.VectorMA(ent.S.Angles, float32(FRAMETIME), ent.Avelocity)
	ent.S.Origin = shared.VectorMA(ent.S.Origin, float32(FRAMETIME), ent.Velocity)

	g.gi.LinkEntity(ent)
}

// SV_Physics_Toss: toss, bounce, and fly movement.  When onground, do nothing.
// C: game/g_phys.c:670 SV_Physics_Toss
func (g *Game) SV_Physics_Toss(ent *Edict) {
	var backoff float32

	// regular thinking
	g.SV_RunThink(ent)

	// if not a team captain, so movement will be handled elsewhere
	if ent.Flags&FL_TEAMSLAVE != 0 {
		return
	}

	if ent.Velocity[2] > 0 {
		ent.Groundentity = nil
	}

	// check for the groundentity going away
	if ent.Groundentity != nil {
		if !ent.Groundentity.InUse {
			ent.Groundentity = nil
		}
	}

	// if onground, return without moving
	if ent.Groundentity != nil {
		return
	}

	old_origin := ent.S.Origin

	g.SV_CheckVelocity(ent)

	// add gravity
	if ent.Movetype != MOVETYPE_FLY &&
		ent.Movetype != MOVETYPE_FLYMISSILE {
		g.SV_AddGravity(ent)
	}

	// move angles
	ent.S.Angles = shared.VectorMA(ent.S.Angles, float32(FRAMETIME), ent.Avelocity)

	// move origin
	move := shared.VectorScale(ent.Velocity, float32(FRAMETIME))
	trace := g.SV_PushEntity(ent, move)
	if !ent.InUse {
		return
	}

	if trace.Fraction < 1 {
		if ent.Movetype == MOVETYPE_BOUNCE {
			backoff = 1.5
		} else {
			backoff = 1
		}

		ClipVelocity(ent.Velocity, trace.Plane.Normal, &ent.Velocity, backoff)

		// stop if on ground
		if float64(trace.Plane.Normal[2]) > 0.7 {
			if ent.Velocity[2] < 60 || ent.Movetype != MOVETYPE_BOUNCE {
				ent.Groundentity = trace.Ent
				ent.GroundentityLinkcount = trace.Ent.LinkCount
				ent.Velocity = shared.Vec3Origin
				ent.Avelocity = shared.Vec3Origin
			}
		}

		//		if (ent->touch)
		//			ent->touch (ent, trace.ent, &trace.plane, trace.surface);
	}

	// check for water transition
	wasinwater := (ent.Watertype & MASK_WATER) != 0
	ent.Watertype = g.gi.PointContents(&ent.S.Origin)
	isinwater := ent.Watertype&MASK_WATER != 0

	if isinwater {
		ent.Waterlevel = 1
	} else {
		ent.Waterlevel = 0
	}

	if !wasinwater && isinwater {
		g.gi.PositionedSound(&old_origin, &g.edicts[0], CHAN_AUTO, g.gi.SoundIndex("misc/h2ohit1.wav"), 1, 1, 0)
	} else if wasinwater && !isinwater {
		g.gi.PositionedSound(&ent.S.Origin, &g.edicts[0], CHAN_AUTO, g.gi.SoundIndex("misc/h2ohit1.wav"), 1, 1, 0)
	}

	// move teamslaves
	for slave := ent.Teamchain; slave != nil; slave = slave.Teamchain {
		slave.S.Origin = ent.S.Origin
		g.gi.LinkEntity(slave)
	}
}

// FIXME: hacked in for E3 demo
// C: game/g_phys.c:787
const (
	sv_stopspeed     = 100
	sv_friction      = 6
	sv_waterfriction = 1
)

// C: game/g_phys.c:791 SV_AddRotationalFriction
func (g *Game) SV_AddRotationalFriction(ent *Edict) {
	ent.S.Angles = shared.VectorMA(ent.S.Angles, float32(FRAMETIME), ent.Avelocity)
	ft := float64(FRAMETIME) // non-constant: C evaluates in double step by step
	adjustment := float32(ft * sv_stopspeed * sv_friction)
	for n := 0; n < 3; n++ {
		if ent.Avelocity[n] > 0 {
			ent.Avelocity[n] -= adjustment
			if ent.Avelocity[n] < 0 {
				ent.Avelocity[n] = 0
			}
		} else {
			ent.Avelocity[n] += adjustment
			if ent.Avelocity[n] > 0 {
				ent.Avelocity[n] = 0
			}
		}
	}
}

// SV_Physics_Step: monsters freefall when they don't have a ground entity,
// otherwise all movement is done with discrete steps.
//
// This is also used for objects that have become still on the ground, but
// will fall if the floor is pulled out from under them.
// FIXME: is this true?
// C: game/g_phys.c:815 SV_Physics_Step
func (g *Game) SV_Physics_Step(ent *Edict) {
	var wasonground bool
	hitsound := false
	var speed, newspeed, control float32
	var friction float32
	var mask int32
	ft := float64(FRAMETIME)

	// airborn monsters should always check for ground
	if ent.Groundentity == nil {
		g.M_CheckGround(ent)
	}

	groundentity := ent.Groundentity

	g.SV_CheckVelocity(ent)

	if groundentity != nil {
		wasonground = true
	} else {
		wasonground = false
	}

	if ent.Avelocity[0] != 0 || ent.Avelocity[1] != 0 || ent.Avelocity[2] != 0 {
		g.SV_AddRotationalFriction(ent)
	}

	// add gravity except:
	//   flying monsters
	//   swimming monsters who are in the water
	if !wasonground {
		if ent.Flags&FL_FLY == 0 {
			if !((ent.Flags&FL_SWIM != 0) && (ent.Waterlevel > 2)) {
				if float64(ent.Velocity[2]) < float64(g.sv_gravity.Value)*-0.1 {
					hitsound = true
				}
				if ent.Waterlevel == 0 {
					g.SV_AddGravity(ent)
				}
			}
		}
	}

	// friction for flying monsters that have been given vertical velocity
	if (ent.Flags&FL_FLY != 0) && (ent.Velocity[2] != 0) {
		speed = float32(math.Abs(float64(ent.Velocity[2])))
		if speed < sv_stopspeed {
			control = sv_stopspeed
		} else {
			control = speed
		}
		friction = sv_friction / 3
		newspeed = float32(float64(speed) - (ft * float64(control) * float64(friction)))
		if newspeed < 0 {
			newspeed = 0
		}
		newspeed /= speed
		ent.Velocity[2] *= newspeed
	}

	// friction for flying monsters that have been given vertical velocity
	if (ent.Flags&FL_SWIM != 0) && (ent.Velocity[2] != 0) {
		speed = float32(math.Abs(float64(ent.Velocity[2])))
		if speed < sv_stopspeed {
			control = sv_stopspeed
		} else {
			control = speed
		}
		newspeed = float32(float64(speed) - (ft * float64(control) * sv_waterfriction * float64(ent.Waterlevel)))
		if newspeed < 0 {
			newspeed = 0
		}
		newspeed /= speed
		ent.Velocity[2] *= newspeed
	}

	if ent.Velocity[2] != 0 || ent.Velocity[1] != 0 || ent.Velocity[0] != 0 {
		// apply friction
		// let dead monsters who aren't completely onground slide
		if wasonground || (ent.Flags&(FL_SWIM|FL_FLY) != 0) {
			if !(ent.Health <= 0.0 && !g.M_CheckBottom(ent)) {
				vel := &ent.Velocity
				speed = float32(math.Sqrt(float64(vel[0]*vel[0] + vel[1]*vel[1])))
				if speed != 0 {
					friction = sv_friction

					if speed < sv_stopspeed {
						control = sv_stopspeed
					} else {
						control = speed
					}
					newspeed = float32(float64(speed) - ft*float64(control)*float64(friction))

					if newspeed < 0 {
						newspeed = 0
					}
					newspeed /= speed

					vel[0] *= newspeed
					vel[1] *= newspeed
				}
			}
		}

		if ent.SVFlags&SVF_MONSTER != 0 {
			mask = MASK_MONSTERSOLID
		} else {
			mask = MASK_SOLID
		}
		g.SV_FlyMove(ent, float32(FRAMETIME), mask)

		g.gi.LinkEntity(ent)
		g.G_TouchTriggers(ent)
		if !ent.InUse {
			return
		}

		if ent.Groundentity != nil {
			if !wasonground {
				if hitsound {
					g.gi.Sound(ent, 0, g.gi.SoundIndex("world/land.wav"), 1, 1, 0)
				}
			}
		}
	}

	// regular thinking
	g.SV_RunThink(ent)
}

//============================================================================

// C: game/g_phys.c:932 G_RunEntity
func (g *Game) G_RunEntity(ent *Edict) {
	if ent.Prethink != nil {
		ent.Prethink.fn(g, ent)
	}

	switch ent.Movetype {
	case MOVETYPE_PUSH, MOVETYPE_STOP:
		g.SV_Physics_Pusher(ent)
	case MOVETYPE_NONE:
		g.SV_Physics_None(ent)
	case MOVETYPE_NOCLIP:
		g.SV_Physics_Noclip(ent)
	case MOVETYPE_STEP:
		g.SV_Physics_Step(ent)
	case MOVETYPE_TOSS, MOVETYPE_BOUNCE, MOVETYPE_FLY, MOVETYPE_FLYMISSILE:
		g.SV_Physics_Toss(ent)
	default:
		g.error("SV_Physics: bad movetype %i", ent.Movetype)
	}
}
