package game

// Port of game/g_func.c: plats, doors, buttons, trains, rotating, timers,
// conveyors, secret doors, killboxes.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_func.c:56
const (
	PLAT_LOW_TRIGGER = 1

	STATE_TOP    = 0
	STATE_BOTTOM = 1
	STATE_UP     = 2
	STATE_DOWN   = 3

	DOOR_START_OPEN = 1
	DOOR_REVERSE    = 2
	DOOR_CRUSHER    = 4
	DOOR_NOMONSTER  = 8
	DOOR_TOGGLE     = 32
	DOOR_X_AXIS     = 64
	DOOR_Y_AXIS     = 128
)

// C: game/g_func.c:1445
const (
	TRAIN_START_ON    = 1
	TRAIN_TOGGLE      = 2
	TRAIN_BLOCK_STOPS = 4
)

// C: game/g_func.c:1878
const (
	SECRET_ALWAYS_SHOOT = 1
	SECRET_1ST_LEFT     = 2
	SECRET_1ST_DOWN     = 4
)

var (
	Move_Done              = defThink("Move_Done")
	Move_Final             = defThink("Move_Final")
	Move_Begin             = defThink("Move_Begin")
	AngleMove_Done         = defThink("AngleMove_Done")
	AngleMove_Final        = defThink("AngleMove_Final")
	AngleMove_Begin        = defThink("AngleMove_Begin")
	Think_AccelMove        = defThink("Think_AccelMove")
	plat_hit_top           = defThink("plat_hit_top")
	plat_hit_bottom        = defThink("plat_hit_bottom")
	plat_go_down           = defThink("plat_go_down")
	plat_go_up             = defThink("plat_go_up")
	plat_blocked           = defBlocked("plat_blocked")
	Use_Plat               = defUse("Use_Plat")
	Touch_Plat_Center      = defTouch("Touch_Plat_Center")
	rotating_blocked       = defBlocked("rotating_blocked")
	rotating_touch         = defTouch("rotating_touch")
	rotating_use           = defUse("rotating_use")
	button_done            = defThink("button_done")
	button_return          = defThink("button_return")
	button_wait            = defThink("button_wait")
	button_use             = defUse("button_use")
	button_touch           = defTouch("button_touch")
	button_killed          = defDie("button_killed")
	door_hit_top           = defThink("door_hit_top")
	door_hit_bottom        = defThink("door_hit_bottom")
	door_go_down           = defThink("door_go_down")
	door_use               = defUse("door_use")
	Touch_DoorTrigger      = defTouch("Touch_DoorTrigger")
	Think_CalcMoveSpeed    = defThink("Think_CalcMoveSpeed")
	Think_SpawnDoorTrigger = defThink("Think_SpawnDoorTrigger")
	door_blocked           = defBlocked("door_blocked")
	door_killed            = defDie("door_killed")
	door_touch             = defTouch("door_touch")
	train_blocked          = defBlocked("train_blocked")
	train_wait             = defThink("train_wait")
	train_next             = defThink("train_next")
	func_train_find        = defThink("func_train_find")
	train_use              = defUse("train_use")
	trigger_elevator_use   = defUse("trigger_elevator_use")
	trigger_elevator_init  = defThink("trigger_elevator_init")
	func_timer_think       = defThink("func_timer_think")
	func_timer_use         = defUse("func_timer_use")
	func_conveyor_use      = defUse("func_conveyor_use")
	door_secret_use        = defUse("door_secret_use")
	door_secret_move1      = defThink("door_secret_move1")
	door_secret_move2      = defThink("door_secret_move2")
	door_secret_move3      = defThink("door_secret_move3")
	door_secret_move4      = defThink("door_secret_move4")
	door_secret_move5      = defThink("door_secret_move5")
	door_secret_move6      = defThink("door_secret_move6")
	door_secret_done       = defThink("door_secret_done")
	door_secret_blocked    = defBlocked("door_secret_blocked")
	door_secret_die        = defDie("door_secret_die")
	use_killbox            = defUse("use_killbox")
)

func init() {
	Move_Done.bind((*Game).Move_Done)
	Move_Final.bind((*Game).Move_Final)
	Move_Begin.bind((*Game).Move_Begin)
	AngleMove_Done.bind((*Game).AngleMove_Done)
	AngleMove_Final.bind((*Game).AngleMove_Final)
	AngleMove_Begin.bind((*Game).AngleMove_Begin)
	Think_AccelMove.bind((*Game).Think_AccelMove)
	plat_hit_top.bind((*Game).plat_hit_top)
	plat_hit_bottom.bind((*Game).plat_hit_bottom)
	plat_go_down.bind((*Game).plat_go_down)
	plat_go_up.bind((*Game).plat_go_up)
	plat_blocked.bind((*Game).plat_blocked)
	Use_Plat.bind((*Game).Use_Plat)
	Touch_Plat_Center.bind((*Game).Touch_Plat_Center)
	rotating_blocked.bind((*Game).rotating_blocked)
	rotating_touch.bind((*Game).rotating_touch)
	rotating_use.bind((*Game).rotating_use)
	button_done.bind((*Game).button_done)
	button_return.bind((*Game).button_return)
	button_wait.bind((*Game).button_wait)
	button_use.bind((*Game).button_use)
	button_touch.bind((*Game).button_touch)
	button_killed.bind((*Game).button_killed)
	door_hit_top.bind((*Game).door_hit_top)
	door_hit_bottom.bind((*Game).door_hit_bottom)
	door_go_down.bind((*Game).door_go_down)
	door_use.bind((*Game).door_use)
	Touch_DoorTrigger.bind((*Game).Touch_DoorTrigger)
	Think_CalcMoveSpeed.bind((*Game).Think_CalcMoveSpeed)
	Think_SpawnDoorTrigger.bind((*Game).Think_SpawnDoorTrigger)
	door_blocked.bind((*Game).door_blocked)
	door_killed.bind((*Game).door_killed)
	door_touch.bind((*Game).door_touch)
	train_blocked.bind((*Game).train_blocked)
	train_wait.bind((*Game).train_wait)
	train_next.bind((*Game).train_next)
	func_train_find.bind((*Game).func_train_find)
	train_use.bind((*Game).train_use)
	trigger_elevator_use.bind((*Game).trigger_elevator_use)
	trigger_elevator_init.bind((*Game).trigger_elevator_init)
	func_timer_think.bind((*Game).func_timer_think)
	func_timer_use.bind((*Game).func_timer_use)
	func_conveyor_use.bind((*Game).func_conveyor_use)
	door_secret_use.bind((*Game).door_secret_use)
	door_secret_move1.bind((*Game).door_secret_move1)
	door_secret_move2.bind((*Game).door_secret_move2)
	door_secret_move3.bind((*Game).door_secret_move3)
	door_secret_move4.bind((*Game).door_secret_move4)
	door_secret_move5.bind((*Game).door_secret_move5)
	door_secret_move6.bind((*Game).door_secret_move6)
	door_secret_done.bind((*Game).door_secret_done)
	door_secret_blocked.bind((*Game).door_secret_blocked)
	door_secret_die.bind((*Game).door_secret_die)
	use_killbox.bind((*Game).use_killbox)
}

//
// Support routines for movement (changes in origin using velocity)
//

// C: game/g_func.c:76 Move_Done
func (g *Game) Move_Done(ent *Edict) {
	ent.Velocity = Vec3{}
	ent.Moveinfo.Endfunc.fn(g, ent)
}

// C: game/g_func.c:82 Move_Final
func (g *Game) Move_Final(ent *Edict) {
	if ent.Moveinfo.RemainingDistance == 0 {
		g.Move_Done(ent)
		return
	}

	ent.Velocity = shared.VectorScale(ent.Moveinfo.Dir, float32(float64(ent.Moveinfo.RemainingDistance)/FRAMETIME))

	ent.Think = Move_Done
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_func.c:96 Move_Begin
func (g *Game) Move_Begin(ent *Edict) {
	var frames float32

	if float64(ent.Moveinfo.Speed)*FRAMETIME >= float64(ent.Moveinfo.RemainingDistance) {
		g.Move_Final(ent)
		return
	}
	ent.Velocity = shared.VectorScale(ent.Moveinfo.Dir, ent.Moveinfo.Speed)
	frames = float32(math.Floor(float64(ent.Moveinfo.RemainingDistance/ent.Moveinfo.Speed) / FRAMETIME))
	ent.Moveinfo.RemainingDistance = float32(float64(ent.Moveinfo.RemainingDistance) - float64(frames*ent.Moveinfo.Speed)*FRAMETIME)
	ent.Nextthink = float32(float64(g.level.Time) + float64(frames)*FRAMETIME)
	ent.Think = Move_Final
}

// C: game/g_func.c:114 Move_Calc
func (g *Game) Move_Calc(ent *Edict, dest Vec3, fn ThinkFn) {
	ent.Velocity = Vec3{}
	ent.Moveinfo.Dir = shared.VectorSubtract(dest, ent.S.Origin)
	ent.Moveinfo.RemainingDistance = shared.VectorNormalize(&ent.Moveinfo.Dir)
	ent.Moveinfo.Endfunc = fn

	if ent.Moveinfo.Speed == ent.Moveinfo.Accel && ent.Moveinfo.Speed == ent.Moveinfo.Decel {
		var master *Edict
		if ent.Flags&FL_TEAMSLAVE != 0 {
			master = ent.Teammaster
		} else {
			master = ent
		}
		if g.level.CurrentEntity == master {
			g.Move_Begin(ent)
		} else {
			ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
			ent.Think = Move_Begin
		}
	} else {
		// accelerative
		ent.Moveinfo.CurrentSpeed = 0
		ent.Think = Think_AccelMove
		ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

//
// Support routines for angular movement (changes in angle using avelocity)
//

// C: game/g_func.c:147 AngleMove_Done
func (g *Game) AngleMove_Done(ent *Edict) {
	ent.Avelocity = Vec3{}
	ent.Moveinfo.Endfunc.fn(g, ent)
}

// C: game/g_func.c:153 AngleMove_Final
func (g *Game) AngleMove_Final(ent *Edict) {
	var move Vec3

	if ent.Moveinfo.State == STATE_UP {
		move = shared.VectorSubtract(ent.Moveinfo.EndAngles, ent.S.Angles)
	} else {
		move = shared.VectorSubtract(ent.Moveinfo.StartAngles, ent.S.Angles)
	}

	if shared.VectorCompare(move, shared.Vec3Origin) != 0 {
		g.AngleMove_Done(ent)
		return
	}

	ent.Avelocity = shared.VectorScale(move, float32(1.0/float64(FRAMETIME)))

	ent.Think = AngleMove_Done
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_func.c:174 AngleMove_Begin
func (g *Game) AngleMove_Begin(ent *Edict) {
	var destdelta Vec3
	var length, traveltime, frames float32

	// set destdelta to the vector needed to move
	if ent.Moveinfo.State == STATE_UP {
		destdelta = shared.VectorSubtract(ent.Moveinfo.EndAngles, ent.S.Angles)
	} else {
		destdelta = shared.VectorSubtract(ent.Moveinfo.StartAngles, ent.S.Angles)
	}

	// calculate length of vector
	length = shared.VectorLength(destdelta)

	// divide by speed to get time to reach dest
	traveltime = length / ent.Moveinfo.Speed

	if float64(traveltime) < FRAMETIME {
		g.AngleMove_Final(ent)
		return
	}

	frames = float32(math.Floor(float64(traveltime) / FRAMETIME))

	// scale the destdelta vector by the time spent traveling to get velocity
	ent.Avelocity = shared.VectorScale(destdelta, float32(1.0/float64(traveltime)))

	// set nextthink to trigger a think when dest is reached
	ent.Nextthink = float32(float64(g.level.Time) + float64(frames)*FRAMETIME)
	ent.Think = AngleMove_Final
}

// C: game/g_func.c:209 AngleMove_Calc
func (g *Game) AngleMove_Calc(ent *Edict, fn ThinkFn) {
	ent.Avelocity = Vec3{}
	ent.Moveinfo.Endfunc = fn
	var master *Edict
	if ent.Flags&FL_TEAMSLAVE != 0 {
		master = ent.Teammaster
	} else {
		master = ent
	}
	if g.level.CurrentEntity == master {
		g.AngleMove_Begin(ent)
	} else {
		ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		ent.Think = AngleMove_Begin
	}
}

// AccelerationDistance is the C macro (target * ((target / rate) + 1) / 2), all float.
// C: game/g_func.c:233 AccelerationDistance
func AccelerationDistance(target, rate float32) float32 {
	return target * ((target / rate) + 1) / 2
}

// C: game/g_func.c:235 plat_CalcAcceleratedMove
func (g *Game) plat_CalcAcceleratedMove(moveinfo *MoveInfo) {
	var accelDist, decelDist float32

	moveinfo.MoveSpeed = moveinfo.Speed

	if moveinfo.RemainingDistance < moveinfo.Accel {
		moveinfo.CurrentSpeed = moveinfo.RemainingDistance
		return
	}

	accelDist = AccelerationDistance(moveinfo.Speed, moveinfo.Accel)
	decelDist = AccelerationDistance(moveinfo.Speed, moveinfo.Decel)

	if (moveinfo.RemainingDistance - accelDist - decelDist) < 0 {
		var f float32

		f = (moveinfo.Accel + moveinfo.Decel) / (moveinfo.Accel * moveinfo.Decel)
		moveinfo.MoveSpeed = float32((-2 + math.Sqrt(float64(4-4*f*(-2*moveinfo.RemainingDistance)))) / float64(2*f))
		decelDist = AccelerationDistance(moveinfo.MoveSpeed, moveinfo.Decel)
	}

	moveinfo.DecelDistance = decelDist
}

// C: game/g_func.c:263 plat_Accelerate
func (g *Game) plat_Accelerate(moveinfo *MoveInfo) {
	// are we decelerating?
	if moveinfo.RemainingDistance <= moveinfo.DecelDistance {
		if moveinfo.RemainingDistance < moveinfo.DecelDistance {
			if moveinfo.NextSpeed != 0 {
				moveinfo.CurrentSpeed = moveinfo.NextSpeed
				moveinfo.NextSpeed = 0
				return
			}
			if moveinfo.CurrentSpeed > moveinfo.Decel {
				moveinfo.CurrentSpeed -= moveinfo.Decel
			}
		}
		return
	}

	// are we at full speed and need to start decelerating during this move?
	if moveinfo.CurrentSpeed == moveinfo.MoveSpeed {
		if (moveinfo.RemainingDistance - moveinfo.CurrentSpeed) < moveinfo.DecelDistance {
			var p1Distance, p2Distance, distance float32

			p1Distance = moveinfo.RemainingDistance - moveinfo.DecelDistance
			p2Distance = float32(float64(moveinfo.MoveSpeed) * (1.0 - float64(p1Distance/moveinfo.MoveSpeed)))
			distance = p1Distance + p2Distance
			moveinfo.CurrentSpeed = moveinfo.MoveSpeed
			moveinfo.NextSpeed = moveinfo.MoveSpeed - moveinfo.Decel*(p2Distance/distance)
			return
		}
	}

	// are we accelerating?
	if moveinfo.CurrentSpeed < moveinfo.Speed {
		var oldSpeed, p1Distance, p1Speed, p2Distance, distance float32

		oldSpeed = moveinfo.CurrentSpeed

		// figure simple acceleration up to move_speed
		moveinfo.CurrentSpeed += moveinfo.Accel
		if moveinfo.CurrentSpeed > moveinfo.Speed {
			moveinfo.CurrentSpeed = moveinfo.Speed
		}

		// are we accelerating throughout this entire move?
		if (moveinfo.RemainingDistance - moveinfo.CurrentSpeed) >= moveinfo.DecelDistance {
			return
		}

		// during this move we will accelrate from current_speed to move_speed
		// and cross over the decel_distance; figure the average speed for the
		// entire move
		p1Distance = moveinfo.RemainingDistance - moveinfo.DecelDistance
		p1Speed = float32(float64(oldSpeed+moveinfo.MoveSpeed) / 2.0)
		p2Distance = float32(float64(moveinfo.MoveSpeed) * (1.0 - float64(p1Distance/p1Speed)))
		distance = p1Distance + p2Distance
		moveinfo.CurrentSpeed = (p1Speed * (p1Distance / distance)) + (moveinfo.MoveSpeed * (p2Distance / distance))
		moveinfo.NextSpeed = moveinfo.MoveSpeed - moveinfo.Decel*(p2Distance/distance)
		return
	}

	// we are at constant velocity (move_speed)
}

// C: game/g_func.c:334 Think_AccelMove
//
// The team has completed a frame of movement, so
// change the speed for the next frame
func (g *Game) Think_AccelMove(ent *Edict) {
	ent.Moveinfo.RemainingDistance -= ent.Moveinfo.CurrentSpeed

	if ent.Moveinfo.CurrentSpeed == 0 { // starting or blocked
		g.plat_CalcAcceleratedMove(&ent.Moveinfo)
	}

	g.plat_Accelerate(&ent.Moveinfo)

	// will the entire move complete on next frame?
	if ent.Moveinfo.RemainingDistance <= ent.Moveinfo.CurrentSpeed {
		g.Move_Final(ent)
		return
	}

	ent.Velocity = shared.VectorScale(ent.Moveinfo.Dir, ent.Moveinfo.CurrentSpeed*10)
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	ent.Think = Think_AccelMove
}

// C: game/g_func.c:358 plat_hit_top
func (g *Game) plat_hit_top(ent *Edict) {
	if ent.Flags&FL_TEAMSLAVE == 0 {
		if ent.Moveinfo.SoundEnd != 0 {
			g.gi.Sound(ent, CHAN_NO_PHS_ADD+CHAN_VOICE, int(ent.Moveinfo.SoundEnd), 1, ATTN_STATIC, 0)
		}
		ent.S.Sound = 0
	}
	ent.Moveinfo.State = STATE_TOP

	ent.Think = plat_go_down
	ent.Nextthink = g.level.Time + 3
}

// C: game/g_func.c:372 plat_hit_bottom
func (g *Game) plat_hit_bottom(ent *Edict) {
	if ent.Flags&FL_TEAMSLAVE == 0 {
		if ent.Moveinfo.SoundEnd != 0 {
			g.gi.Sound(ent, CHAN_NO_PHS_ADD+CHAN_VOICE, int(ent.Moveinfo.SoundEnd), 1, ATTN_STATIC, 0)
		}
		ent.S.Sound = 0
	}
	ent.Moveinfo.State = STATE_BOTTOM
}

// C: game/g_func.c:383 plat_go_down
func (g *Game) plat_go_down(ent *Edict) {
	if ent.Flags&FL_TEAMSLAVE == 0 {
		if ent.Moveinfo.SoundStart != 0 {
			g.gi.Sound(ent, CHAN_NO_PHS_ADD+CHAN_VOICE, int(ent.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
		}
		ent.S.Sound = ent.Moveinfo.SoundMiddle
	}
	ent.Moveinfo.State = STATE_DOWN
	g.Move_Calc(ent, ent.Moveinfo.EndOrigin, plat_hit_bottom)
}

// C: game/g_func.c:395 plat_go_up
func (g *Game) plat_go_up(ent *Edict) {
	if ent.Flags&FL_TEAMSLAVE == 0 {
		if ent.Moveinfo.SoundStart != 0 {
			g.gi.Sound(ent, CHAN_NO_PHS_ADD+CHAN_VOICE, int(ent.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
		}
		ent.S.Sound = ent.Moveinfo.SoundMiddle
	}
	ent.Moveinfo.State = STATE_UP
	g.Move_Calc(ent, ent.Moveinfo.StartOrigin, plat_hit_top)
}

// C: game/g_func.c:407 plat_blocked
func (g *Game) plat_blocked(self, other *Edict) {
	if other.SVFlags&SVF_MONSTER == 0 && other.Client == nil {
		// give it a chance to go away on it's own terms (like gibs)
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, 100000, 1, 0, MOD_CRUSH)
		// if it's still there, nuke it
		if other != nil {
			g.BecomeExplosion1(other)
		}
		return
	}

	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)

	if self.Moveinfo.State == STATE_UP {
		g.plat_go_down(self)
	} else if self.Moveinfo.State == STATE_DOWN {
		g.plat_go_up(self)
	}
}

// C: game/g_func.c:428 Use_Plat
func (g *Game) Use_Plat(ent, other, activator *Edict) {
	if ent.Think != nil {
		return // already down
	}
	g.plat_go_down(ent)
}

// C: game/g_func.c:436 Touch_Plat_Center
func (g *Game) Touch_Plat_Center(ent, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client == nil {
		return
	}

	if other.Health <= 0 {
		return
	}

	ent = ent.Enemy // now point at the plat, not the trigger
	if ent.Moveinfo.State == STATE_BOTTOM {
		g.plat_go_up(ent)
	} else if ent.Moveinfo.State == STATE_TOP {
		ent.Nextthink = g.level.Time + 1 // the player is still on the plat, so delay going down
	}
}

// C: game/g_func.c:451 plat_spawn_inside_trigger
func (g *Game) plat_spawn_inside_trigger(ent *Edict) {
	var tmin, tmax Vec3

	//
	// middle trigger
	//
	trigger := g.G_Spawn()
	trigger.Touch = Touch_Plat_Center
	trigger.Movetype = MOVETYPE_NONE
	trigger.Solid = SOLID_TRIGGER
	trigger.Enemy = ent

	tmin[0] = ent.Mins[0] + 25
	tmin[1] = ent.Mins[1] + 25
	tmin[2] = ent.Mins[2]

	tmax[0] = ent.Maxs[0] - 25
	tmax[1] = ent.Maxs[1] - 25
	tmax[2] = ent.Maxs[2] + 8

	tmin[2] = tmax[2] - (ent.Pos1[2] - ent.Pos2[2] + float32(g.st.Lip))

	if ent.Spawnflags&PLAT_LOW_TRIGGER != 0 {
		tmax[2] = tmin[2] + 8
	}

	if tmax[0]-tmin[0] <= 0 {
		tmin[0] = float32(float64(ent.Mins[0]+ent.Maxs[0]) * 0.5)
		tmax[0] = tmin[0] + 1
	}
	if tmax[1]-tmin[1] <= 0 {
		tmin[1] = float32(float64(ent.Mins[1]+ent.Maxs[1]) * 0.5)
		tmax[1] = tmin[1] + 1
	}

	trigger.Mins = tmin
	trigger.Maxs = tmax

	g.gi.LinkEntity(trigger)
}

// C: game/g_func.c:513 SP_func_plat
func (g *Game) SP_func_plat(ent *Edict) {
	ent.S.Angles = Vec3{}
	ent.Solid = SOLID_BSP
	ent.Movetype = MOVETYPE_PUSH

	g.gi.SetModel(ent, ent.Model)

	ent.Blocked = plat_blocked

	if ent.Speed == 0 {
		ent.Speed = 20
	} else {
		ent.Speed = float32(float64(ent.Speed) * 0.1)
	}

	if ent.Accel == 0 {
		ent.Accel = 5
	} else {
		ent.Accel = float32(float64(ent.Accel) * 0.1)
	}

	if ent.Decel == 0 {
		ent.Decel = 5
	} else {
		ent.Decel = float32(float64(ent.Decel) * 0.1)
	}

	if ent.Dmg == 0 {
		ent.Dmg = 2
	}

	if g.st.Lip == 0 {
		g.st.Lip = 8
	}

	// pos1 is the top position, pos2 is the bottom
	ent.Pos1 = ent.S.Origin
	ent.Pos2 = ent.S.Origin
	if g.st.Height != 0 {
		ent.Pos2[2] -= float32(g.st.Height)
	} else {
		ent.Pos2[2] -= (ent.Maxs[2] - ent.Mins[2]) - float32(g.st.Lip)
	}

	ent.Use = Use_Plat

	g.plat_spawn_inside_trigger(ent) // the "start moving" trigger

	if ent.Targetname != "" {
		ent.Moveinfo.State = STATE_UP
	} else {
		ent.S.Origin = ent.Pos2
		g.gi.LinkEntity(ent)
		ent.Moveinfo.State = STATE_BOTTOM
	}

	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Accel = ent.Accel
	ent.Moveinfo.Decel = ent.Decel
	ent.Moveinfo.Wait = ent.Wait
	ent.Moveinfo.StartOrigin = ent.Pos1
	ent.Moveinfo.StartAngles = ent.S.Angles
	ent.Moveinfo.EndOrigin = ent.Pos2
	ent.Moveinfo.EndAngles = ent.S.Angles

	ent.Moveinfo.SoundStart = int32(g.gi.SoundIndex("plats/pt1_strt.wav"))
	ent.Moveinfo.SoundMiddle = int32(g.gi.SoundIndex("plats/pt1_mid.wav"))
	ent.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("plats/pt1_end.wav"))
}

//====================================================================

// C: game/g_func.c:595 rotating_blocked
func (g *Game) rotating_blocked(self, other *Edict) {
	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)
}

// C: game/g_func.c:600 rotating_touch
func (g *Game) rotating_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if self.Avelocity[0] != 0 || self.Avelocity[1] != 0 || self.Avelocity[2] != 0 {
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)
	}
}

// C: game/g_func.c:606 rotating_use
func (g *Game) rotating_use(self, other, activator *Edict) {
	if shared.VectorCompare(self.Avelocity, shared.Vec3Origin) == 0 {
		self.S.Sound = 0
		self.Avelocity = Vec3{}
		self.Touch = nil
	} else {
		self.S.Sound = self.Moveinfo.SoundMiddle
		self.Avelocity = shared.VectorScale(self.Movedir, self.Speed)
		if self.Spawnflags&16 != 0 {
			self.Touch = rotating_touch
		}
	}
}

// C: game/g_func.c:623 SP_func_rotating
func (g *Game) SP_func_rotating(ent *Edict) {
	ent.Solid = SOLID_BSP
	if ent.Spawnflags&32 != 0 {
		ent.Movetype = MOVETYPE_STOP
	} else {
		ent.Movetype = MOVETYPE_PUSH
	}

	// set the axis of rotation
	ent.Movedir = Vec3{}
	if ent.Spawnflags&4 != 0 {
		ent.Movedir[2] = 1.0
	} else if ent.Spawnflags&8 != 0 {
		ent.Movedir[0] = 1.0
	} else { // Z_AXIS
		ent.Movedir[1] = 1.0
	}

	// check for reverse rotation
	if ent.Spawnflags&2 != 0 {
		ent.Movedir = shared.VectorNegate(ent.Movedir)
	}

	if ent.Speed == 0 {
		ent.Speed = 100
	}
	if ent.Dmg == 0 {
		ent.Dmg = 2
	}

	//	ent->moveinfo.sound_middle = "doors/hydro1.wav";

	ent.Use = rotating_use
	if ent.Dmg != 0 {
		ent.Blocked = rotating_blocked
	}

	if ent.Spawnflags&1 != 0 {
		ent.Use.fn(g, ent, nil, nil)
	}

	if ent.Spawnflags&64 != 0 {
		ent.S.Effects |= EF_ANIM_ALL
	}
	if ent.Spawnflags&128 != 0 {
		ent.S.Effects |= EF_ANIM_ALLFAST
	}

	g.gi.SetModel(ent, ent.Model)
	g.gi.LinkEntity(ent)
}

/*
======================================================================

BUTTONS

======================================================================
*/

// C: game/g_func.c:692 button_done
func (g *Game) button_done(self *Edict) {
	self.Moveinfo.State = STATE_BOTTOM
	self.S.Effects &^= EF_ANIM23
	self.S.Effects |= EF_ANIM01
}

// C: game/g_func.c:699 button_return
func (g *Game) button_return(self *Edict) {
	self.Moveinfo.State = STATE_DOWN

	g.Move_Calc(self, self.Moveinfo.StartOrigin, button_done)

	self.S.Frame = 0

	if self.Health != 0 {
		self.Takedamage = DAMAGE_YES
	}
}

// C: game/g_func.c:711 button_wait
func (g *Game) button_wait(self *Edict) {
	self.Moveinfo.State = STATE_TOP
	self.S.Effects &^= EF_ANIM01
	self.S.Effects |= EF_ANIM23

	g.G_UseTargets(self, self.Activator)
	self.S.Frame = 1
	if self.Moveinfo.Wait >= 0 {
		self.Nextthink = g.level.Time + self.Moveinfo.Wait
		self.Think = button_return
	}
}

// C: game/g_func.c:726 button_fire
func (g *Game) button_fire(self *Edict) {
	if self.Moveinfo.State == STATE_UP || self.Moveinfo.State == STATE_TOP {
		return
	}

	self.Moveinfo.State = STATE_UP
	if self.Moveinfo.SoundStart != 0 && self.Flags&FL_TEAMSLAVE == 0 {
		g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
	}
	g.Move_Calc(self, self.Moveinfo.EndOrigin, button_wait)
}

// C: game/g_func.c:737 button_use
func (g *Game) button_use(self, other, activator *Edict) {
	self.Activator = activator
	g.button_fire(self)
}

// C: game/g_func.c:743 button_touch
func (g *Game) button_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client == nil {
		return
	}

	if other.Health <= 0 {
		return
	}

	self.Activator = other
	g.button_fire(self)
}

// C: game/g_func.c:755 button_killed
func (g *Game) button_killed(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	self.Activator = attacker
	self.Health = self.MaxHealth
	self.Takedamage = DAMAGE_NO
	g.button_fire(self)
}

// C: game/g_func.c:763 SP_func_button
func (g *Game) SP_func_button(ent *Edict) {
	var absMovedir Vec3
	var dist float32

	G_SetMovedir(&ent.S.Angles, &ent.Movedir)
	ent.Movetype = MOVETYPE_STOP
	ent.Solid = SOLID_BSP
	g.gi.SetModel(ent, ent.Model)

	if ent.Sounds != 1 {
		ent.Moveinfo.SoundStart = int32(g.gi.SoundIndex("switches/butn2.wav"))
	}

	if ent.Speed == 0 {
		ent.Speed = 40
	}
	if ent.Accel == 0 {
		ent.Accel = ent.Speed
	}
	if ent.Decel == 0 {
		ent.Decel = ent.Speed
	}

	if ent.Wait == 0 {
		ent.Wait = 3
	}
	if g.st.Lip == 0 {
		g.st.Lip = 4
	}

	ent.Pos1 = ent.S.Origin
	absMovedir[0] = float32(math.Abs(float64(ent.Movedir[0])))
	absMovedir[1] = float32(math.Abs(float64(ent.Movedir[1])))
	absMovedir[2] = float32(math.Abs(float64(ent.Movedir[2])))
	dist = absMovedir[0]*ent.Size[0] + absMovedir[1]*ent.Size[1] + absMovedir[2]*ent.Size[2] - float32(g.st.Lip)
	ent.Pos2 = shared.VectorMA(ent.Pos1, dist, ent.Movedir)

	ent.Use = button_use
	ent.S.Effects |= EF_ANIM01

	if ent.Health != 0 {
		ent.MaxHealth = ent.Health
		ent.Die = button_killed
		ent.Takedamage = DAMAGE_YES
	} else if ent.Targetname == "" {
		ent.Touch = button_touch
	}

	ent.Moveinfo.State = STATE_BOTTOM

	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Accel = ent.Accel
	ent.Moveinfo.Decel = ent.Decel
	ent.Moveinfo.Wait = ent.Wait
	ent.Moveinfo.StartOrigin = ent.Pos1
	ent.Moveinfo.StartAngles = ent.S.Angles
	ent.Moveinfo.EndOrigin = ent.Pos2
	ent.Moveinfo.EndAngles = ent.S.Angles

	g.gi.LinkEntity(ent)
}

/*
======================================================================

DOORS

  spawn a trigger surrounding the entire team unless it is
  already targeted by another

======================================================================
*/

// C: game/g_func.c:852 door_use_areaportals
func (g *Game) door_use_areaportals(self *Edict, open bool) {
	var t *Edict

	if self.Target == "" {
		return
	}

	for {
		t = g.G_Find(t, FOFS_targetname, self.Target)
		if t == nil {
			break
		}
		if shared.Q_stricmp(t.Classname, "func_areaportal") == 0 {
			g.gi.SetAreaPortalState(int(t.Style), open)
		}
	}
}

// C: game/g_func.c:870 door_hit_top
func (g *Game) door_hit_top(self *Edict) {
	if self.Flags&FL_TEAMSLAVE == 0 {
		if self.Moveinfo.SoundEnd != 0 {
			g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundEnd), 1, ATTN_STATIC, 0)
		}
		self.S.Sound = 0
	}
	self.Moveinfo.State = STATE_TOP
	if self.Spawnflags&DOOR_TOGGLE != 0 {
		return
	}
	if self.Moveinfo.Wait >= 0 {
		self.Think = door_go_down
		self.Nextthink = g.level.Time + self.Moveinfo.Wait
	}
}

// C: game/g_func.c:888 door_hit_bottom
func (g *Game) door_hit_bottom(self *Edict) {
	if self.Flags&FL_TEAMSLAVE == 0 {
		if self.Moveinfo.SoundEnd != 0 {
			g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundEnd), 1, ATTN_STATIC, 0)
		}
		self.S.Sound = 0
	}
	self.Moveinfo.State = STATE_BOTTOM
	g.door_use_areaportals(self, false)
}

// C: game/g_func.c:900 door_go_down
func (g *Game) door_go_down(self *Edict) {
	if self.Flags&FL_TEAMSLAVE == 0 {
		if self.Moveinfo.SoundStart != 0 {
			g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
		}
		self.S.Sound = self.Moveinfo.SoundMiddle
	}
	if self.MaxHealth != 0 {
		self.Takedamage = DAMAGE_YES
		self.Health = self.MaxHealth
	}

	self.Moveinfo.State = STATE_DOWN
	if self.Classname == "func_door" {
		g.Move_Calc(self, self.Moveinfo.StartOrigin, door_hit_bottom)
	} else if self.Classname == "func_door_rotating" {
		g.AngleMove_Calc(self, door_hit_bottom)
	}
}

// C: game/g_func.c:921 door_go_up
func (g *Game) door_go_up(self, activator *Edict) {
	if self.Moveinfo.State == STATE_UP {
		return // already going up
	}

	if self.Moveinfo.State == STATE_TOP { // reset top wait time
		if self.Moveinfo.Wait >= 0 {
			self.Nextthink = g.level.Time + self.Moveinfo.Wait
		}
		return
	}

	if self.Flags&FL_TEAMSLAVE == 0 {
		if self.Moveinfo.SoundStart != 0 {
			g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
		}
		self.S.Sound = self.Moveinfo.SoundMiddle
	}
	self.Moveinfo.State = STATE_UP
	if self.Classname == "func_door" {
		g.Move_Calc(self, self.Moveinfo.EndOrigin, door_hit_top)
	} else if self.Classname == "func_door_rotating" {
		g.AngleMove_Calc(self, door_hit_top)
	}

	g.G_UseTargets(self, activator)
	g.door_use_areaportals(self, true)
}

// C: game/g_func.c:949 door_use
func (g *Game) door_use(self, other, activator *Edict) {
	if self.Flags&FL_TEAMSLAVE != 0 {
		return
	}

	if self.Spawnflags&DOOR_TOGGLE != 0 {
		if self.Moveinfo.State == STATE_UP || self.Moveinfo.State == STATE_TOP {
			// trigger all paired doors
			for ent := self; ent != nil; ent = ent.Teamchain {
				ent.Message = ""
				ent.Touch = nil
				g.door_go_down(ent)
			}
			return
		}
	}

	// trigger all paired doors
	for ent := self; ent != nil; ent = ent.Teamchain {
		ent.Message = ""
		ent.Touch = nil
		g.door_go_up(ent, activator)
	}
}

// C: game/g_func.c:980 Touch_DoorTrigger
func (g *Game) Touch_DoorTrigger(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Health <= 0 {
		return
	}

	if other.SVFlags&SVF_MONSTER == 0 && other.Client == nil {
		return
	}

	if self.Owner.Spawnflags&DOOR_NOMONSTER != 0 && other.SVFlags&SVF_MONSTER != 0 {
		return
	}

	if g.level.Time < self.TouchDebounceTime {
		return
	}
	self.TouchDebounceTime = float32(float64(g.level.Time) + 1.0)

	g.door_use(self.Owner, other, other)
}

// C: game/g_func.c:998 Think_CalcMoveSpeed
func (g *Game) Think_CalcMoveSpeed(self *Edict) {
	var min, time, newspeed, ratio, dist float32

	if self.Flags&FL_TEAMSLAVE != 0 {
		return // only the team master does this
	}

	// find the smallest distance any member of the team will be moving
	min = float32(math.Abs(float64(self.Moveinfo.Distance)))
	for ent := self.Teamchain; ent != nil; ent = ent.Teamchain {
		dist = float32(math.Abs(float64(ent.Moveinfo.Distance)))
		if dist < min {
			min = dist
		}
	}

	time = min / self.Moveinfo.Speed

	// adjust speeds so they will all complete at the same time
	for ent := self; ent != nil; ent = ent.Teamchain {
		newspeed = float32(math.Abs(float64(ent.Moveinfo.Distance)) / float64(time))
		ratio = newspeed / ent.Moveinfo.Speed
		if ent.Moveinfo.Accel == ent.Moveinfo.Speed {
			ent.Moveinfo.Accel = newspeed
		} else {
			ent.Moveinfo.Accel *= ratio
		}
		if ent.Moveinfo.Decel == ent.Moveinfo.Speed {
			ent.Moveinfo.Decel = newspeed
		} else {
			ent.Moveinfo.Decel *= ratio
		}
		ent.Moveinfo.Speed = newspeed
	}
}

// C: game/g_func.c:1038 Think_SpawnDoorTrigger
func (g *Game) Think_SpawnDoorTrigger(ent *Edict) {
	var mins, maxs Vec3

	if ent.Flags&FL_TEAMSLAVE != 0 {
		return // only the team leader spawns a trigger
	}

	mins = ent.AbsMin
	maxs = ent.AbsMax

	for other := ent.Teamchain; other != nil; other = other.Teamchain {
		shared.AddPointToBounds(other.AbsMin, &mins, &maxs)
		shared.AddPointToBounds(other.AbsMax, &mins, &maxs)
	}

	// expand
	mins[0] -= 60
	mins[1] -= 60
	maxs[0] += 60
	maxs[1] += 60

	other := g.G_Spawn()
	other.Mins = mins
	other.Maxs = maxs
	other.Owner = ent
	other.Solid = SOLID_TRIGGER
	other.Movetype = MOVETYPE_NONE
	other.Touch = Touch_DoorTrigger
	g.gi.LinkEntity(other)

	if ent.Spawnflags&DOOR_START_OPEN != 0 {
		g.door_use_areaportals(ent, true)
	}

	g.Think_CalcMoveSpeed(ent)
}

// C: game/g_func.c:1076 door_blocked
func (g *Game) door_blocked(self, other *Edict) {
	if other.SVFlags&SVF_MONSTER == 0 && other.Client == nil {
		// give it a chance to go away on it's own terms (like gibs)
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, 100000, 1, 0, MOD_CRUSH)
		// if it's still there, nuke it
		if other != nil {
			g.BecomeExplosion1(other)
		}
		return
	}

	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)

	if self.Spawnflags&DOOR_CRUSHER != 0 {
		return
	}

	// if a door has a negative wait, it would never come back if blocked,
	// so let it just squash the object to death real fast
	if self.Moveinfo.Wait >= 0 {
		if self.Moveinfo.State == STATE_DOWN {
			for ent := self.Teammaster; ent != nil; ent = ent.Teamchain {
				g.door_go_up(ent, ent.Activator)
			}
		} else {
			for ent := self.Teammaster; ent != nil; ent = ent.Teamchain {
				g.door_go_down(ent)
			}
		}
	}
}

// C: game/g_func.c:1113 door_killed
func (g *Game) door_killed(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	for ent := self.Teammaster; ent != nil; ent = ent.Teamchain {
		ent.Health = ent.MaxHealth
		ent.Takedamage = DAMAGE_NO
	}
	g.door_use(self.Teammaster, attacker, attacker)
}

// C: game/g_func.c:1125 door_touch
func (g *Game) door_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client == nil {
		return
	}

	if g.level.Time < self.TouchDebounceTime {
		return
	}
	self.TouchDebounceTime = float32(float64(g.level.Time) + 5.0)

	g.gi.Centerprintf(other, self.Message)
	g.gi.Sound(other, CHAN_AUTO, g.gi.SoundIndex("misc/talk1.wav"), 1, ATTN_NORM, 0)
}

// C: game/g_func.c:1138 SP_func_door
func (g *Game) SP_func_door(ent *Edict) {
	var absMovedir Vec3

	if ent.Sounds != 1 {
		ent.Moveinfo.SoundStart = int32(g.gi.SoundIndex("doors/dr1_strt.wav"))
		ent.Moveinfo.SoundMiddle = int32(g.gi.SoundIndex("doors/dr1_mid.wav"))
		ent.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("doors/dr1_end.wav"))
	}

	G_SetMovedir(&ent.S.Angles, &ent.Movedir)
	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_BSP
	g.gi.SetModel(ent, ent.Model)

	ent.Blocked = door_blocked
	ent.Use = door_use

	if ent.Speed == 0 {
		ent.Speed = 100
	}
	if g.deathmatch.Value != 0 {
		ent.Speed *= 2
	}

	if ent.Accel == 0 {
		ent.Accel = ent.Speed
	}
	if ent.Decel == 0 {
		ent.Decel = ent.Speed
	}

	if ent.Wait == 0 {
		ent.Wait = 3
	}
	if g.st.Lip == 0 {
		g.st.Lip = 8
	}
	if ent.Dmg == 0 {
		ent.Dmg = 2
	}

	// calculate second position
	ent.Pos1 = ent.S.Origin
	absMovedir[0] = float32(math.Abs(float64(ent.Movedir[0])))
	absMovedir[1] = float32(math.Abs(float64(ent.Movedir[1])))
	absMovedir[2] = float32(math.Abs(float64(ent.Movedir[2])))
	ent.Moveinfo.Distance = absMovedir[0]*ent.Size[0] + absMovedir[1]*ent.Size[1] + absMovedir[2]*ent.Size[2] - float32(g.st.Lip)
	ent.Pos2 = shared.VectorMA(ent.Pos1, ent.Moveinfo.Distance, ent.Movedir)

	// if it starts open, switch the positions
	if ent.Spawnflags&DOOR_START_OPEN != 0 {
		ent.S.Origin = ent.Pos2
		ent.Pos2 = ent.Pos1
		ent.Pos1 = ent.S.Origin
	}

	ent.Moveinfo.State = STATE_BOTTOM

	if ent.Health != 0 {
		ent.Takedamage = DAMAGE_YES
		ent.Die = door_killed
		ent.MaxHealth = ent.Health
	} else if ent.Targetname != "" && ent.Message != "" {
		g.gi.SoundIndex("misc/talk.wav")
		ent.Touch = door_touch
	}

	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Accel = ent.Accel
	ent.Moveinfo.Decel = ent.Decel
	ent.Moveinfo.Wait = ent.Wait
	ent.Moveinfo.StartOrigin = ent.Pos1
	ent.Moveinfo.StartAngles = ent.S.Angles
	ent.Moveinfo.EndOrigin = ent.Pos2
	ent.Moveinfo.EndAngles = ent.S.Angles

	if ent.Spawnflags&16 != 0 {
		ent.S.Effects |= EF_ANIM_ALL
	}
	if ent.Spawnflags&64 != 0 {
		ent.S.Effects |= EF_ANIM_ALLFAST
	}

	// to simplify logic elsewhere, make non-teamed doors into a team of one
	if ent.Team == "" {
		ent.Teammaster = ent
	}

	g.gi.LinkEntity(ent)

	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	if ent.Health != 0 || ent.Targetname != "" {
		ent.Think = Think_CalcMoveSpeed
	} else {
		ent.Think = Think_SpawnDoorTrigger
	}
}

// C: game/g_func.c:1261 SP_func_door_rotating
func (g *Game) SP_func_door_rotating(ent *Edict) {
	ent.S.Angles = Vec3{}

	// set the axis of rotation
	ent.Movedir = Vec3{}
	if ent.Spawnflags&DOOR_X_AXIS != 0 {
		ent.Movedir[2] = 1.0
	} else if ent.Spawnflags&DOOR_Y_AXIS != 0 {
		ent.Movedir[0] = 1.0
	} else { // Z_AXIS
		ent.Movedir[1] = 1.0
	}

	// check for reverse rotation
	if ent.Spawnflags&DOOR_REVERSE != 0 {
		ent.Movedir = shared.VectorNegate(ent.Movedir)
	}

	if g.st.Distance == 0 {
		g.dprintf("%s at %s with no distance set\n", ent.Classname, vtos(ent.S.Origin))
		g.st.Distance = 90
	}

	ent.Pos1 = ent.S.Angles
	ent.Pos2 = shared.VectorMA(ent.S.Angles, float32(g.st.Distance), ent.Movedir)
	ent.Moveinfo.Distance = float32(g.st.Distance)

	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_BSP
	g.gi.SetModel(ent, ent.Model)

	ent.Blocked = door_blocked
	ent.Use = door_use

	if ent.Speed == 0 {
		ent.Speed = 100
	}
	if ent.Accel == 0 {
		ent.Accel = ent.Speed
	}
	if ent.Decel == 0 {
		ent.Decel = ent.Speed
	}

	if ent.Wait == 0 {
		ent.Wait = 3
	}
	if ent.Dmg == 0 {
		ent.Dmg = 2
	}

	if ent.Sounds != 1 {
		ent.Moveinfo.SoundStart = int32(g.gi.SoundIndex("doors/dr1_strt.wav"))
		ent.Moveinfo.SoundMiddle = int32(g.gi.SoundIndex("doors/dr1_mid.wav"))
		ent.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("doors/dr1_end.wav"))
	}

	// if it starts open, switch the positions
	if ent.Spawnflags&DOOR_START_OPEN != 0 {
		ent.S.Angles = ent.Pos2
		ent.Pos2 = ent.Pos1
		ent.Pos1 = ent.S.Angles
		ent.Movedir = shared.VectorNegate(ent.Movedir)
	}

	if ent.Health != 0 {
		ent.Takedamage = DAMAGE_YES
		ent.Die = door_killed
		ent.MaxHealth = ent.Health
	}

	if ent.Targetname != "" && ent.Message != "" {
		g.gi.SoundIndex("misc/talk.wav")
		ent.Touch = door_touch
	}

	ent.Moveinfo.State = STATE_BOTTOM
	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Accel = ent.Accel
	ent.Moveinfo.Decel = ent.Decel
	ent.Moveinfo.Wait = ent.Wait
	ent.Moveinfo.StartOrigin = ent.S.Origin
	ent.Moveinfo.StartAngles = ent.Pos1
	ent.Moveinfo.EndOrigin = ent.S.Origin
	ent.Moveinfo.EndAngles = ent.Pos2

	if ent.Spawnflags&16 != 0 {
		ent.S.Effects |= EF_ANIM_ALL
	}

	// to simplify logic elsewhere, make non-teamed doors into a team of one
	if ent.Team == "" {
		ent.Teammaster = ent
	}

	g.gi.LinkEntity(ent)

	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	if ent.Health != 0 || ent.Targetname != "" {
		ent.Think = Think_CalcMoveSpeed
	} else {
		ent.Think = Think_SpawnDoorTrigger
	}
}

// C: game/g_func.c:1378 SP_func_water
func (g *Game) SP_func_water(self *Edict) {
	var absMovedir Vec3

	G_SetMovedir(&self.S.Angles, &self.Movedir)
	self.Movetype = MOVETYPE_PUSH
	self.Solid = SOLID_BSP
	g.gi.SetModel(self, self.Model)

	switch self.Sounds {
	default:

	case 1: // water
		self.Moveinfo.SoundStart = int32(g.gi.SoundIndex("world/mov_watr.wav"))
		self.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("world/stp_watr.wav"))

	case 2: // lava
		self.Moveinfo.SoundStart = int32(g.gi.SoundIndex("world/mov_watr.wav"))
		self.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("world/stp_watr.wav"))
	}

	// calculate second position
	self.Pos1 = self.S.Origin
	absMovedir[0] = float32(math.Abs(float64(self.Movedir[0])))
	absMovedir[1] = float32(math.Abs(float64(self.Movedir[1])))
	absMovedir[2] = float32(math.Abs(float64(self.Movedir[2])))
	self.Moveinfo.Distance = absMovedir[0]*self.Size[0] + absMovedir[1]*self.Size[1] + absMovedir[2]*self.Size[2] - float32(g.st.Lip)
	self.Pos2 = shared.VectorMA(self.Pos1, self.Moveinfo.Distance, self.Movedir)

	// if it starts open, switch the positions
	if self.Spawnflags&DOOR_START_OPEN != 0 {
		self.S.Origin = self.Pos2
		self.Pos2 = self.Pos1
		self.Pos1 = self.S.Origin
	}

	self.Moveinfo.StartOrigin = self.Pos1
	self.Moveinfo.StartAngles = self.S.Angles
	self.Moveinfo.EndOrigin = self.Pos2
	self.Moveinfo.EndAngles = self.S.Angles

	self.Moveinfo.State = STATE_BOTTOM

	if self.Speed == 0 {
		self.Speed = 25
	}
	self.Moveinfo.Speed = self.Speed
	self.Moveinfo.Decel = self.Moveinfo.Speed
	self.Moveinfo.Accel = self.Moveinfo.Decel

	if self.Wait == 0 {
		self.Wait = -1
	}
	self.Moveinfo.Wait = self.Wait

	self.Use = door_use

	if self.Wait == -1 {
		self.Spawnflags |= DOOR_TOGGLE
	}

	self.Classname = "func_door"

	g.gi.LinkEntity(self)
}

// C: game/g_func.c:1461 train_blocked
func (g *Game) train_blocked(self, other *Edict) {
	if other.SVFlags&SVF_MONSTER == 0 && other.Client == nil {
		// give it a chance to go away on it's own terms (like gibs)
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, 100000, 1, 0, MOD_CRUSH)
		// if it's still there, nuke it
		if other != nil {
			g.BecomeExplosion1(other)
		}
		return
	}

	if g.level.Time < self.TouchDebounceTime {
		return
	}

	if self.Dmg == 0 {
		return
	}
	self.TouchDebounceTime = float32(float64(g.level.Time) + 0.5)
	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)
}

// C: game/g_func.c:1482 train_wait
func (g *Game) train_wait(self *Edict) {
	if self.TargetEnt.Pathtarget != "" {
		ent := self.TargetEnt
		savetarget := ent.Target
		ent.Target = ent.Pathtarget
		g.G_UseTargets(ent, self.Activator)
		ent.Target = savetarget

		// make sure we didn't get killed by a killtarget
		if !self.InUse {
			return
		}
	}

	if self.Moveinfo.Wait != 0 {
		if self.Moveinfo.Wait > 0 {
			self.Nextthink = g.level.Time + self.Moveinfo.Wait
			self.Think = train_next
		} else if self.Spawnflags&TRAIN_TOGGLE != 0 { // && wait < 0
			g.train_next(self)
			self.Spawnflags &^= TRAIN_START_ON
			self.Velocity = Vec3{}
			self.Nextthink = 0
		}

		if self.Flags&FL_TEAMSLAVE == 0 {
			if self.Moveinfo.SoundEnd != 0 {
				g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundEnd), 1, ATTN_STATIC, 0)
			}
			self.S.Sound = 0
		}
	} else {
		g.train_next(self)
	}
}

// C: game/g_func.c:1529 train_next
func (g *Game) train_next(self *Edict) {
	var ent *Edict
	var dest Vec3

	first := true
again:
	if self.Target == "" {
		//		gi.dprintf ("train_next: no next target\n");
		return
	}

	ent = g.G_PickTarget(self.Target)
	if ent == nil {
		g.dprintf("train_next: bad target %s\n", self.Target)
		return
	}

	self.Target = ent.Target

	// check for a teleport path_corner
	if ent.Spawnflags&1 != 0 {
		if !first {
			g.dprintf("connected teleport path_corners, see %s at %s\n", ent.Classname, vtos(ent.S.Origin))
			return
		}
		first = false
		self.S.Origin = shared.VectorSubtract(ent.S.Origin, self.Mins)
		self.S.OldOrigin = self.S.Origin
		self.S.Event = EV_OTHER_TELEPORT
		g.gi.LinkEntity(self)
		goto again
	}

	self.Moveinfo.Wait = ent.Wait
	self.TargetEnt = ent

	if self.Flags&FL_TEAMSLAVE == 0 {
		if self.Moveinfo.SoundStart != 0 {
			g.gi.Sound(self, CHAN_NO_PHS_ADD+CHAN_VOICE, int(self.Moveinfo.SoundStart), 1, ATTN_STATIC, 0)
		}
		self.S.Sound = self.Moveinfo.SoundMiddle
	}

	dest = shared.VectorSubtract(ent.S.Origin, self.Mins)
	self.Moveinfo.State = STATE_TOP
	self.Moveinfo.StartOrigin = self.S.Origin
	self.Moveinfo.EndOrigin = dest
	g.Move_Calc(self, dest, train_wait)
	self.Spawnflags |= TRAIN_START_ON
}

// C: game/g_func.c:1586 train_resume
func (g *Game) train_resume(self *Edict) {
	ent := self.TargetEnt

	dest := shared.VectorSubtract(ent.S.Origin, self.Mins)
	self.Moveinfo.State = STATE_TOP
	self.Moveinfo.StartOrigin = self.S.Origin
	self.Moveinfo.EndOrigin = dest
	g.Move_Calc(self, dest, train_wait)
	self.Spawnflags |= TRAIN_START_ON
}

// C: game/g_func.c:1601 func_train_find
func (g *Game) func_train_find(self *Edict) {
	if self.Target == "" {
		g.gi.Dprintf("train_find: no target\n")
		return
	}
	ent := g.G_PickTarget(self.Target)
	if ent == nil {
		g.dprintf("train_find: target %s not found\n", self.Target)
		return
	}
	self.Target = ent.Target

	self.S.Origin = shared.VectorSubtract(ent.S.Origin, self.Mins)
	g.gi.LinkEntity(self)

	// if not triggered, start immediately
	if self.Targetname == "" {
		self.Spawnflags |= TRAIN_START_ON
	}

	if self.Spawnflags&TRAIN_START_ON != 0 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		self.Think = train_next
		self.Activator = self
	}
}

// C: game/g_func.c:1633 train_use
func (g *Game) train_use(self, other, activator *Edict) {
	self.Activator = activator

	if self.Spawnflags&TRAIN_START_ON != 0 {
		if self.Spawnflags&TRAIN_TOGGLE == 0 {
			return
		}
		self.Spawnflags &^= TRAIN_START_ON
		self.Velocity = Vec3{}
		self.Nextthink = 0
	} else {
		if self.TargetEnt != nil {
			g.train_resume(self)
		} else {
			g.train_next(self)
		}
	}
}

// C: game/g_func.c:1654 SP_func_train
func (g *Game) SP_func_train(self *Edict) {
	self.Movetype = MOVETYPE_PUSH

	self.S.Angles = Vec3{}
	self.Blocked = train_blocked
	if self.Spawnflags&TRAIN_BLOCK_STOPS != 0 {
		self.Dmg = 0
	} else {
		if self.Dmg == 0 {
			self.Dmg = 100
		}
	}
	self.Solid = SOLID_BSP
	g.gi.SetModel(self, self.Model)

	if g.st.Noise != "" {
		self.Moveinfo.SoundMiddle = int32(g.gi.SoundIndex(g.st.Noise))
	}

	if self.Speed == 0 {
		self.Speed = 100
	}

	self.Moveinfo.Speed = self.Speed
	self.Moveinfo.Decel = self.Moveinfo.Speed
	self.Moveinfo.Accel = self.Moveinfo.Decel

	self.Use = train_use

	g.gi.LinkEntity(self)

	if self.Target != "" {
		// start trains on the second frame, to make sure their targets have had
		// a chance to spawn
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		self.Think = func_train_find
	} else {
		g.dprintf("func_train without a target at %s\n", vtos(self.AbsMin))
	}
}

// C: game/g_func.c:1699 trigger_elevator_use
func (g *Game) trigger_elevator_use(self, other, activator *Edict) {
	if self.Movetarget.Nextthink != 0 {
		//		gi.dprintf("elevator busy\n");
		return
	}

	if other.Pathtarget == "" {
		g.gi.Dprintf("elevator used with no pathtarget\n")
		return
	}

	target := g.G_PickTarget(other.Pathtarget)
	if target == nil {
		g.dprintf("elevator used with bad pathtarget: %s\n", other.Pathtarget)
		return
	}

	self.Movetarget.TargetEnt = target
	g.train_resume(self.Movetarget)
}

// C: game/g_func.c:1726 trigger_elevator_init
func (g *Game) trigger_elevator_init(self *Edict) {
	if self.Target == "" {
		g.gi.Dprintf("trigger_elevator has no target\n")
		return
	}
	self.Movetarget = g.G_PickTarget(self.Target)
	if self.Movetarget == nil {
		g.dprintf("trigger_elevator unable to find target %s\n", self.Target)
		return
	}
	if self.Movetarget.Classname != "func_train" {
		g.dprintf("trigger_elevator target %s is not a train\n", self.Target)
		return
	}

	self.Use = trigger_elevator_use
	self.SVFlags = SVF_NOCLIENT
}

// C: game/g_func.c:1750 SP_trigger_elevator
func (g *Game) SP_trigger_elevator(self *Edict) {
	self.Think = trigger_elevator_init
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_func.c:1771 func_timer_think
func (g *Game) func_timer_think(self *Edict) {
	g.G_UseTargets(self, self.Activator)
	self.Nextthink = float32(float64(g.level.Time+self.Wait) + g.crandom()*float64(self.Random))
}

// C: game/g_func.c:1777 func_timer_use
func (g *Game) func_timer_use(self, other, activator *Edict) {
	self.Activator = activator

	// if on, turn it off
	if self.Nextthink != 0 {
		self.Nextthink = 0
		return
	}

	// turn it on
	if self.Delay != 0 {
		self.Nextthink = g.level.Time + self.Delay
	} else {
		g.func_timer_think(self)
	}
}

// C: game/g_func.c:1795 SP_func_timer
func (g *Game) SP_func_timer(self *Edict) {
	if self.Wait == 0 {
		self.Wait = 1.0
	}

	self.Use = func_timer_use
	self.Think = func_timer_think

	if self.Random >= self.Wait {
		self.Random = float32(float64(self.Wait) - FRAMETIME)
		g.dprintf("func_timer at %s has random >= wait\n", vtos(self.S.Origin))
	}

	if self.Spawnflags&1 != 0 {
		self.Nextthink = float32(float64(g.level.Time) + 1.0 + float64(g.st.Pausetime) + float64(self.Delay) + float64(self.Wait) + g.crandom()*float64(self.Random))
		self.Activator = self
	}

	self.SVFlags = SVF_NOCLIENT
}

// C: game/g_func.c:1825 func_conveyor_use
func (g *Game) func_conveyor_use(self, other, activator *Edict) {
	if self.Spawnflags&1 != 0 {
		self.Speed = 0
		self.Spawnflags &^= 1
	} else {
		self.Speed = float32(self.Count)
		self.Spawnflags |= 1
	}

	if self.Spawnflags&2 == 0 {
		self.Count = 0
	}
}

// C: game/g_func.c:1842 SP_func_conveyor
func (g *Game) SP_func_conveyor(self *Edict) {
	if self.Speed == 0 {
		self.Speed = 100
	}

	if self.Spawnflags&1 == 0 {
		self.Count = int32(self.Speed)
		self.Speed = 0
	}

	self.Use = func_conveyor_use

	g.gi.SetModel(self, self.Model)
	self.Solid = SOLID_BSP
	g.gi.LinkEntity(self)
}

// C: game/g_func.c:1886 door_secret_use
func (g *Game) door_secret_use(self, other, activator *Edict) {
	// make sure we're not already moving
	if shared.VectorCompare(self.S.Origin, shared.Vec3Origin) == 0 {
		return
	}

	g.Move_Calc(self, self.Pos1, door_secret_move1)
	g.door_use_areaportals(self, true)
}

// C: game/g_func.c:1896 door_secret_move1
func (g *Game) door_secret_move1(self *Edict) {
	self.Nextthink = float32(float64(g.level.Time) + 1.0)
	self.Think = door_secret_move2
}

// C: game/g_func.c:1902 door_secret_move2
func (g *Game) door_secret_move2(self *Edict) {
	g.Move_Calc(self, self.Pos2, door_secret_move3)
}

// C: game/g_func.c:1907 door_secret_move3
func (g *Game) door_secret_move3(self *Edict) {
	if self.Wait == -1 {
		return
	}
	self.Nextthink = g.level.Time + self.Wait
	self.Think = door_secret_move4
}

// C: game/g_func.c:1915 door_secret_move4
func (g *Game) door_secret_move4(self *Edict) {
	g.Move_Calc(self, self.Pos1, door_secret_move5)
}

// C: game/g_func.c:1920 door_secret_move5
func (g *Game) door_secret_move5(self *Edict) {
	self.Nextthink = float32(float64(g.level.Time) + 1.0)
	self.Think = door_secret_move6
}

// C: game/g_func.c:1926 door_secret_move6
func (g *Game) door_secret_move6(self *Edict) {
	g.Move_Calc(self, shared.Vec3Origin, door_secret_done)
}

// C: game/g_func.c:1931 door_secret_done
func (g *Game) door_secret_done(self *Edict) {
	if self.Targetname == "" || self.Spawnflags&SECRET_ALWAYS_SHOOT != 0 {
		self.Health = 0
		self.Takedamage = DAMAGE_YES
	}
	g.door_use_areaportals(self, false)
}

// C: game/g_func.c:1941 door_secret_blocked
func (g *Game) door_secret_blocked(self, other *Edict) {
	if other.SVFlags&SVF_MONSTER == 0 && other.Client == nil {
		// give it a chance to go away on it's own terms (like gibs)
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, 100000, 1, 0, MOD_CRUSH)
		// if it's still there, nuke it
		if other != nil {
			g.BecomeExplosion1(other)
		}
		return
	}

	if g.level.Time < self.TouchDebounceTime {
		return
	}
	self.TouchDebounceTime = float32(float64(g.level.Time) + 0.5)

	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)
}

// C: game/g_func.c:1960 door_secret_die
func (g *Game) door_secret_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	self.Takedamage = DAMAGE_NO
	g.door_secret_use(self, attacker, attacker)
}

// C: game/g_func.c:1966 SP_func_door_secret
func (g *Game) SP_func_door_secret(ent *Edict) {
	var forward, right, up Vec3
	var side, width, length float32

	ent.Moveinfo.SoundStart = int32(g.gi.SoundIndex("doors/dr1_strt.wav"))
	ent.Moveinfo.SoundMiddle = int32(g.gi.SoundIndex("doors/dr1_mid.wav"))
	ent.Moveinfo.SoundEnd = int32(g.gi.SoundIndex("doors/dr1_end.wav"))

	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_BSP
	g.gi.SetModel(ent, ent.Model)

	ent.Blocked = door_secret_blocked
	ent.Use = door_secret_use

	if ent.Targetname == "" || ent.Spawnflags&SECRET_ALWAYS_SHOOT != 0 {
		ent.Health = 0
		ent.Takedamage = DAMAGE_YES
		ent.Die = door_secret_die
	}

	if ent.Dmg == 0 {
		ent.Dmg = 2
	}

	if ent.Wait == 0 {
		ent.Wait = 5
	}

	ent.Moveinfo.Speed = 50
	ent.Moveinfo.Decel = ent.Moveinfo.Speed
	ent.Moveinfo.Accel = ent.Moveinfo.Decel

	// calculate positions
	shared.AngleVectors(ent.S.Angles, &forward, &right, &up)
	ent.S.Angles = Vec3{}
	side = float32(1.0 - float64(ent.Spawnflags&SECRET_1ST_LEFT))
	if ent.Spawnflags&SECRET_1ST_DOWN != 0 {
		width = float32(math.Abs(float64(shared.DotProduct(up, ent.Size))))
	} else {
		width = float32(math.Abs(float64(shared.DotProduct(right, ent.Size))))
	}
	length = float32(math.Abs(float64(shared.DotProduct(forward, ent.Size))))
	if ent.Spawnflags&SECRET_1ST_DOWN != 0 {
		ent.Pos1 = shared.VectorMA(ent.S.Origin, -1*width, up)
	} else {
		ent.Pos1 = shared.VectorMA(ent.S.Origin, side*width, right)
	}
	ent.Pos2 = shared.VectorMA(ent.Pos1, length, forward)

	if ent.Health != 0 {
		ent.Takedamage = DAMAGE_YES
		ent.Die = door_killed
		ent.MaxHealth = ent.Health
	} else if ent.Targetname != "" && ent.Message != "" {
		g.gi.SoundIndex("misc/talk.wav")
		ent.Touch = door_touch
	}

	ent.Classname = "func_door"

	g.gi.LinkEntity(ent)
}

// C: game/g_func.c:2037 use_killbox
func (g *Game) use_killbox(self, other, activator *Edict) {
	g.KillBox(self)
}

// C: game/g_func.c:2042 SP_func_killbox
func (g *Game) SP_func_killbox(ent *Edict) {
	g.gi.SetModel(ent, ent.Model)
	ent.Use = use_killbox
	ent.SVFlags = SVF_NOCLIENT
}
