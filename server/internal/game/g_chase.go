package game

// Port of game/g_chase.c: spectator chase camera.

import (
	"fmt"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_chase.c:22 UpdateChaseCam
func (g *Game) UpdateChaseCam(ent *Edict) {
	var o, ownerv, goal Vec3
	var forward, right Vec3
	var oldgoal Vec3
	var angles Vec3

	// is our chase target gone?
	if g.ctfmod {
		// C: ctf/g_chase.c:34 (older base, no spectator mode)
		if !ent.Client.ChaseTarget.InUse {
			ent.Client.ChaseTarget = nil
			return
		}
	} else if !ent.Client.ChaseTarget.InUse || ent.Client.ChaseTarget.Client.Resp.Spectator {
		old := ent.Client.ChaseTarget
		g.ChaseNext(ent)
		if ent.Client.ChaseTarget == old {
			ent.Client.ChaseTarget = nil
			ent.Client.PS.PMove.PmFlags &^= PMF_NO_PREDICTION
			return
		}
	}

	targ := ent.Client.ChaseTarget

	ownerv = targ.S.Origin
	oldgoal = ent.S.Origin
	_ = oldgoal

	ownerv[2] += float32(targ.Viewheight)

	angles = targ.Client.VAngle
	if angles[PITCH] > 56 {
		angles[PITCH] = 56
	}
	shared.AngleVectors(angles, &forward, &right, nil)
	shared.VectorNormalize(&forward)
	o = shared.VectorMA(ownerv, -30, forward)

	if o[2] < targ.S.Origin[2]+20 {
		o[2] = targ.S.Origin[2] + 20
	}

	// jump animation lifts
	if targ.Groundentity == nil {
		o[2] += 16
	}

	trace := g.gi.Trace(&ownerv, &shared.Vec3Origin, &shared.Vec3Origin, &o, targ, MASK_SOLID)

	goal = trace.EndPos

	goal = shared.VectorMA(goal, 2, forward)

	// pad for floors and ceilings
	o = goal
	o[2] += 6
	trace = g.gi.Trace(&goal, &shared.Vec3Origin, &shared.Vec3Origin, &o, targ, MASK_SOLID)
	if trace.Fraction < 1 {
		goal = trace.EndPos
		goal[2] -= 6
	}

	o = goal
	o[2] -= 6
	trace = g.gi.Trace(&goal, &shared.Vec3Origin, &shared.Vec3Origin, &o, targ, MASK_SOLID)
	if trace.Fraction < 1 {
		goal = trace.EndPos
		goal[2] += 6
	}

	if targ.Deadflag != 0 && !g.ctfmod {
		ent.Client.PS.PMove.PmType = PM_DEAD
	} else {
		ent.Client.PS.PMove.PmType = PM_FREEZE
	}

	ent.S.Origin = goal
	for i := 0; i < 3; i++ {
		ent.Client.PS.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(targ.Client.VAngle[i] - ent.Client.Resp.CmdAngles[i]))
	}

	if targ.Deadflag != 0 && !g.ctfmod {
		ent.Client.PS.ViewAngles[ROLL] = 40
		ent.Client.PS.ViewAngles[PITCH] = -15
		ent.Client.PS.ViewAngles[YAW] = targ.Client.KillerYaw
	} else {
		ent.Client.PS.ViewAngles = targ.Client.VAngle
		ent.Client.VAngle = targ.Client.VAngle
	}

	ent.Viewheight = 0
	ent.Client.PS.PMove.PmFlags |= PMF_NO_PREDICTION
	g.gi.LinkEntity(ent)

	//ZOID: C: ctf/g_chase.c:96
	if g.ctfmod && ((!ent.Client.Showscores && ent.Client.Menu == nil &&
		!ent.Client.Showinventory && !ent.Client.Showhelp &&
		g.level.Framenum&31 == 0) || ent.Client.UpdateChase) {
		ent.Client.UpdateChase = false
		s := fmt.Sprintf("xv 0 yb -68 string2 \"Chasing %s\"",
			targ.Client.Pers.Netname)
		g.gi.WriteByteC(svc_layout)
		g.gi.WriteString(s)
		g.gi.Unicast(ent, false)
	}
}

// C: game/g_chase.c:111 ChaseNext
func (g *Game) ChaseNext(ent *Edict) {
	var e *Edict

	if ent.Client.ChaseTarget == nil {
		return
	}

	i := ent.Client.ChaseTarget.Index
	for {
		i++
		if float32(i) > g.maxclients.Value {
			i = 1
		}
		e = &g.edicts[i]
		if e.InUse {
			if g.ctfmod { // ctf/g_chase.c:127
				if e.Solid != SOLID_NOT {
					break
				}
			} else if !e.Client.Resp.Spectator {
				break
			}
		}
		if e == ent.Client.ChaseTarget {
			break
		}
	}

	ent.Client.ChaseTarget = e
	ent.Client.UpdateChase = true
}

// C: game/g_chase.c:135 ChasePrev
func (g *Game) ChasePrev(ent *Edict) {
	var e *Edict

	if ent.Client.ChaseTarget == nil {
		return
	}

	i := ent.Client.ChaseTarget.Index
	for {
		i--
		if i < 1 {
			i = int(int32(g.maxclients.Value))
		}
		e = &g.edicts[i]
		if e.InUse {
			if g.ctfmod { // ctf/g_chase.c:127
				if e.Solid != SOLID_NOT {
					break
				}
			} else if !e.Client.Resp.Spectator {
				break
			}
		}
		if e == ent.Client.ChaseTarget {
			break
		}
	}

	ent.Client.ChaseTarget = e
	ent.Client.UpdateChase = true
}

// C: game/g_chase.c:159 GetChaseTarget
func (g *Game) GetChaseTarget(ent *Edict) {
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		other := &g.edicts[i]
		if other.InUse && !other.Client.Resp.Spectator {
			ent.Client.ChaseTarget = other
			ent.Client.UpdateChase = true
			g.UpdateChaseCam(ent)
			return
		}
	}
	g.gi.Centerprintf(ent, "No other players to chase.")
}
