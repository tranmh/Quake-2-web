package game

// Port of game/m_move.c: monster movement.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/m_move.c:24 STEPSIZE
const STEPSIZE = 18

// M_CheckBottom returns false if any part of the bottom of the entity is off
// an edge that is not a staircase.
// C: game/m_move.c:37 M_CheckBottom
func (g *Game) M_CheckBottom(ent *Edict) bool {
	var start, stop Vec3
	var trace Trace
	var mid, bottom float32
	origin := shared.Vec3Origin

	mins := shared.VectorAdd(ent.S.Origin, ent.Mins)
	maxs := shared.VectorAdd(ent.S.Origin, ent.Maxs)

	// if all of the points under the corners are solid world, don't bother
	// with the tougher checks
	// the corners must be within 16 of the midpoint
	start[2] = mins[2] - 1
	for x := 0; x <= 1; x++ {
		for y := 0; y <= 1; y++ {
			if x != 0 {
				start[0] = maxs[0]
			} else {
				start[0] = mins[0]
			}
			if y != 0 {
				start[1] = maxs[1]
			} else {
				start[1] = mins[1]
			}
			if g.gi.PointContents(&start) != CONTENTS_SOLID {
				goto realcheck
			}
		}
	}

	g.c_yes++
	return true // we got out easy

realcheck:
	g.c_no++
	//
	// check it for real...
	//
	start[2] = mins[2]

	// the midpoint must be within 16 of the bottom
	start[0] = float32(float64(mins[0]+maxs[0]) * 0.5)
	stop[0] = start[0]
	start[1] = float32(float64(mins[1]+maxs[1]) * 0.5)
	stop[1] = start[1]
	stop[2] = start[2] - 2*STEPSIZE
	trace = g.gi.Trace(&start, &origin, &origin, &stop, ent, MASK_MONSTERSOLID)

	if trace.Fraction == 1.0 {
		return false
	}
	mid = trace.EndPos[2]
	bottom = mid

	// the corners must be within 16 of the midpoint
	for x := 0; x <= 1; x++ {
		for y := 0; y <= 1; y++ {
			if x != 0 {
				start[0] = maxs[0]
			} else {
				start[0] = mins[0]
			}
			stop[0] = start[0]
			if y != 0 {
				start[1] = maxs[1]
			} else {
				start[1] = mins[1]
			}
			stop[1] = start[1]

			trace = g.gi.Trace(&start, &origin, &origin, &stop, ent, MASK_MONSTERSOLID)

			if trace.Fraction != 1.0 && trace.EndPos[2] > bottom {
				bottom = trace.EndPos[2]
			}
			if trace.Fraction == 1.0 || mid-trace.EndPos[2] > STEPSIZE {
				return false
			}
		}
	}

	g.c_yes++
	return true
}

// SV_movestep is called by monster program code.
// The move will be adjusted for slopes and stairs, but if the move isn't
// possible, no move is done, false is returned, and
// pr_global_struct->trace_normal is set to the normal of the blocking wall
// C: game/m_move.c:112 SV_movestep
func (g *Game) SV_movestep(ent *Edict, move Vec3, relink bool) bool {
	var dz float32
	var neworg, end Vec3
	var trace Trace
	var stepsize float32
	var test Vec3
	var contents int32

	// try the move
	oldorg := ent.S.Origin
	neworg = shared.VectorAdd(ent.S.Origin, move)

	// flying monsters don't step up
	if ent.Flags&(FL_SWIM|FL_FLY) != 0 {
		// try one move with vertical motion, then one without
		for i := 0; i < 2; i++ {
			neworg = shared.VectorAdd(ent.S.Origin, move)
			if i == 0 && ent.Enemy != nil {
				if ent.Goalentity == nil {
					ent.Goalentity = ent.Enemy
				}
				dz = ent.S.Origin[2] - ent.Goalentity.S.Origin[2]
				if ent.Goalentity.Client != nil {
					if dz > 40 {
						neworg[2] -= 8
					}
					if !((ent.Flags&FL_SWIM != 0) && (ent.Waterlevel < 2)) {
						if dz < 30 {
							neworg[2] += 8
						}
					}
				} else {
					if dz > 8 {
						neworg[2] -= 8
					} else if dz > 0 {
						neworg[2] -= dz
					} else if dz < -8 {
						neworg[2] += 8
					} else {
						neworg[2] += dz
					}
				}
			}
			trace = g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &neworg, ent, MASK_MONSTERSOLID)

			// fly monsters don't enter water voluntarily
			if ent.Flags&FL_FLY != 0 {
				if ent.Waterlevel == 0 {
					test[0] = trace.EndPos[0]
					test[1] = trace.EndPos[1]
					test[2] = trace.EndPos[2] + ent.Mins[2] + 1
					contents = g.gi.PointContents(&test)
					if contents&MASK_WATER != 0 {
						return false
					}
				}
			}

			// swim monsters don't exit water voluntarily
			if ent.Flags&FL_SWIM != 0 {
				if ent.Waterlevel < 2 {
					test[0] = trace.EndPos[0]
					test[1] = trace.EndPos[1]
					test[2] = trace.EndPos[2] + ent.Mins[2] + 1
					contents = g.gi.PointContents(&test)
					if contents&MASK_WATER == 0 {
						return false
					}
				}
			}

			if trace.Fraction == 1 {
				ent.S.Origin = trace.EndPos
				if relink {
					g.gi.LinkEntity(ent)
					g.G_TouchTriggers(ent)
				}
				return true
			}

			if ent.Enemy == nil {
				break
			}
		}

		return false
	}

	// push down from a step height above the wished position
	if ent.Monsterinfo.Aiflags&AI_NOSTEP == 0 {
		stepsize = STEPSIZE
	} else {
		stepsize = 1
	}

	neworg[2] += stepsize
	end = neworg
	end[2] -= stepsize * 2

	trace = g.gi.Trace(&neworg, &ent.Mins, &ent.Maxs, &end, ent, MASK_MONSTERSOLID)

	if trace.AllSolid {
		return false
	}

	if trace.StartSolid {
		neworg[2] -= stepsize
		trace = g.gi.Trace(&neworg, &ent.Mins, &ent.Maxs, &end, ent, MASK_MONSTERSOLID)
		if trace.AllSolid || trace.StartSolid {
			return false
		}
	}

	// don't go in to water
	if ent.Waterlevel == 0 {
		test[0] = trace.EndPos[0]
		test[1] = trace.EndPos[1]
		test[2] = trace.EndPos[2] + ent.Mins[2] + 1
		contents = g.gi.PointContents(&test)

		if contents&MASK_WATER != 0 {
			return false
		}
	}

	if trace.Fraction == 1 {
		// if monster had the ground pulled out, go ahead and fall
		if ent.Flags&FL_PARTIALGROUND != 0 {
			ent.S.Origin = shared.VectorAdd(ent.S.Origin, move)
			if relink {
				g.gi.LinkEntity(ent)
				g.G_TouchTriggers(ent)
			}
			ent.Groundentity = nil
			return true
		}

		return false // walked off an edge
	}

	// check point traces down for dangling corners
	ent.S.Origin = trace.EndPos

	if !g.M_CheckBottom(ent) {
		if ent.Flags&FL_PARTIALGROUND != 0 {
			// entity had floor mostly pulled out from underneath it
			// and is trying to correct
			if relink {
				g.gi.LinkEntity(ent)
				g.G_TouchTriggers(ent)
			}
			return true
		}
		ent.S.Origin = oldorg
		return false
	}

	if ent.Flags&FL_PARTIALGROUND != 0 {
		ent.Flags &^= FL_PARTIALGROUND
	}
	ent.Groundentity = trace.Ent
	ent.GroundentityLinkcount = trace.Ent.LinkCount

	// the move is ok
	if relink {
		g.gi.LinkEntity(ent)
		g.G_TouchTriggers(ent)
	}
	return true
}

//============================================================================

// C: game/m_move.c:304 M_ChangeYaw
func (g *Game) M_ChangeYaw(ent *Edict) {
	current := shared.Anglemod(ent.S.Angles[YAW])
	ideal := ent.IdealYaw

	if current == ideal {
		return
	}

	move := ideal - current
	speed := ent.YawSpeed
	if ideal > current {
		if move >= 180 {
			move = move - 360
		}
	} else {
		if move <= -180 {
			move = move + 360
		}
	}
	if move > 0 {
		if move > speed {
			move = speed
		}
	} else {
		if move < -speed {
			move = -speed
		}
	}

	ent.S.Angles[YAW] = shared.Anglemod(current + move)
}

// SV_StepDirection turns to the movement direction, and walks the current
// distance if facing it.
// C: game/m_move.c:353 SV_StepDirection
func (g *Game) SV_StepDirection(ent *Edict, yaw, dist float32) bool {
	var move Vec3

	ent.IdealYaw = yaw
	g.M_ChangeYaw(ent)

	yaw = float32(float64(yaw) * shared.MPI * 2 / 360)
	move[0] = float32(math.Cos(float64(yaw)) * float64(dist))
	move[1] = float32(math.Sin(float64(yaw)) * float64(dist))
	move[2] = 0

	oldorigin := ent.S.Origin
	if g.SV_movestep(ent, move, false) {
		delta := ent.S.Angles[YAW] - ent.IdealYaw
		if delta > 45 && delta < 315 { // not turned far enough, so don't take the step
			ent.S.Origin = oldorigin
		}
		g.gi.LinkEntity(ent)
		g.G_TouchTriggers(ent)
		return true
	}
	g.gi.LinkEntity(ent)
	g.G_TouchTriggers(ent)
	return false
}

// C: game/m_move.c:389 SV_FixCheckBottom
func (g *Game) SV_FixCheckBottom(ent *Edict) {
	ent.Flags |= FL_PARTIALGROUND
}

// C: game/m_move.c:402 DI_NODIR
const DI_NODIR = -1

// mmoveAbs is C abs(int) applied to a float argument (implicit truncation).
func mmoveAbs(f float32) int32 {
	i := int32(f)
	if i < 0 {
		return -i
	}
	return i
}

// C: game/m_move.c:403 SV_NewChaseDir
func (g *Game) SV_NewChaseDir(actor, enemy *Edict, dist float32) {
	var deltax, deltay float32
	var d [3]float32
	var tdir, olddir, turnaround float32

	//FIXME: how did we get here with no enemy
	if enemy == nil {
		return
	}

	olddir = shared.Anglemod(float32(int32(actor.IdealYaw/45) * 45))
	turnaround = shared.Anglemod(olddir - 180)

	deltax = enemy.S.Origin[0] - actor.S.Origin[0]
	deltay = enemy.S.Origin[1] - actor.S.Origin[1]
	if deltax > 10 {
		d[1] = 0
	} else if deltax < -10 {
		d[1] = 180
	} else {
		d[1] = DI_NODIR
	}
	if deltay < -10 {
		d[2] = 270
	} else if deltay > 10 {
		d[2] = 90
	} else {
		d[2] = DI_NODIR
	}

	// try direct route
	if d[1] != DI_NODIR && d[2] != DI_NODIR {
		if d[1] == 0 {
			if d[2] == 90 {
				tdir = 45
			} else {
				tdir = 315
			}
		} else {
			if d[2] == 90 {
				tdir = 135
			} else {
				tdir = 215
			}
		}

		if tdir != turnaround && g.SV_StepDirection(actor, tdir, dist) {
			return
		}
	}

	// try other directions
	// abs() is the int abs: the float deltas are truncated
	if ((g.rng.Rand()&3)&1 != 0) || mmoveAbs(deltay) > mmoveAbs(deltax) {
		tdir = d[1]
		d[1] = d[2]
		d[2] = tdir
	}

	if d[1] != DI_NODIR && d[1] != turnaround &&
		g.SV_StepDirection(actor, d[1], dist) {
		return
	}

	if d[2] != DI_NODIR && d[2] != turnaround &&
		g.SV_StepDirection(actor, d[2], dist) {
		return
	}

	/* there is no direct path to the player, so pick another direction */

	if olddir != DI_NODIR && g.SV_StepDirection(actor, olddir, dist) {
		return
	}

	if g.rng.Rand()&1 != 0 { /*randomly determine direction of search*/
		for tdir = 0; tdir <= 315; tdir += 45 {
			if tdir != turnaround && g.SV_StepDirection(actor, tdir, dist) {
				return
			}
		}
	} else {
		for tdir = 315; tdir >= 0; tdir -= 45 {
			if tdir != turnaround && g.SV_StepDirection(actor, tdir, dist) {
				return
			}
		}
	}

	if turnaround != DI_NODIR && g.SV_StepDirection(actor, turnaround, dist) {
		return
	}

	actor.IdealYaw = olddir // can't move

	// if a bridge was pulled out from underneath a monster, it may not have
	// a valid standing position at all

	if !g.M_CheckBottom(actor) {
		g.SV_FixCheckBottom(actor)
	}
}

// C: game/m_move.c:495 SV_CloseEnough
func (g *Game) SV_CloseEnough(ent, goal *Edict, dist float32) bool {
	for i := 0; i < 3; i++ {
		if goal.AbsMin[i] > ent.AbsMax[i]+dist {
			return false
		}
		if goal.AbsMax[i] < ent.AbsMin[i]-dist {
			return false
		}
	}
	return true
}

// C: game/m_move.c:515 M_MoveToGoal
func (g *Game) M_MoveToGoal(ent *Edict, dist float32) {
	goal := ent.Goalentity

	if ent.Groundentity == nil && ent.Flags&(FL_FLY|FL_SWIM) == 0 {
		return
	}

	// if the next step hits the enemy, return immediately
	if ent.Enemy != nil && g.SV_CloseEnough(ent, ent.Enemy, dist) {
		return
	}

	// bump around...
	if (g.rng.Rand()&3) == 1 || !g.SV_StepDirection(ent, ent.IdealYaw, dist) {
		if ent.InUse {
			g.SV_NewChaseDir(ent, goal, dist)
		}
	}
}

// C: game/m_move.c:542 M_walkmove
func (g *Game) M_walkmove(ent *Edict, yaw, dist float32) bool {
	var move Vec3

	if ent.Groundentity == nil && ent.Flags&(FL_FLY|FL_SWIM) == 0 {
		return false
	}

	yaw = float32(float64(yaw) * shared.MPI * 2 / 360)

	move[0] = float32(math.Cos(float64(yaw)) * float64(dist))
	move[1] = float32(math.Sin(float64(yaw)) * float64(dist))
	move[2] = 0

	return g.SV_movestep(ent, move, true)
}
