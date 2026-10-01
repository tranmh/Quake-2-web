// Package control turns the agent's movement and aim decisions into the
// usercmds a real client sends.
//
// This file holds the angle conversion every command needs. The server
// computes the view angles of a usercmd as cmd.angles + delta_angles
// (PM_ClampAngles), where delta_angles is the server-side offset in
// ps.pmove that spawns, teleports and loads change. A bot that wants to look
// at an absolute yaw/pitch must therefore recompute the command angles from
// the latest delta_angles for every command.
package control

import (
	"math"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// MaxPitch is the largest pitch (degrees, positive looks down) CmdAngles
// sends; PM_ClampAngles clamps the view to 89 degrees as well.
const MaxPitch = 89

// CmdAngles returns the usercmd angles that make the server's view angles
// equal yaw and pitch (degrees; pitch > 0 looks down, clamped to ±MaxPitch,
// roll 0) for a client whose ps.pmove.delta_angles is delta:
// ANGLE2SHORT(want) - delta, wrapped to a short like the C usercmd field.
func CmdAngles(yaw, pitch float32, delta [3]int16) [3]int16 {
	if pitch > MaxPitch {
		pitch = MaxPitch
	} else if pitch < -MaxPitch {
		pitch = -MaxPitch
	}
	var out [3]int16
	out[q2const.PITCH] = int16(shared.ANGLE2SHORT(pitch) - int32(delta[q2const.PITCH]))
	out[q2const.YAW] = int16(shared.ANGLE2SHORT(yaw) - int32(delta[q2const.YAW]))
	out[q2const.ROLL] = int16(-int32(delta[q2const.ROLL]))
	return out
}

// ViewAngles is the inverse of CmdAngles: the view yaw and pitch the server
// derives from usercmd angles cmd and delta_angles delta, as PM_ClampAngles
// computes them (short sum, SHORT2ANGLE, pitch clamped to 89 when looking
// down). Yaw is normalized to [0, 360), pitch to (-180, 180].
// C: qcommon/pmove.c:1204 PM_ClampAngles
func ViewAngles(cmd, delta [3]int16) (yaw, pitch float32) {
	y := float32(shared.SHORT2ANGLE(int32(cmd[q2const.YAW] + delta[q2const.YAW])))
	p := float32(shared.SHORT2ANGLE(int32(cmd[q2const.PITCH] + delta[q2const.PITCH])))
	// the short sum is signed, so SHORT2ANGLE gives [-180, 180)
	if p > 89 && p < 180 {
		p = 89
	}
	if y < 0 {
		y += 360
	}
	return y, p
}

// AngleDelta returns the signed difference a-b of two angles in degrees,
// normalized to (-180, 180].
func AngleDelta(a, b float32) float32 {
	d := math.Mod(float64(a)-float64(b), 360)
	if d <= -180 {
		d += 360
	} else if d > 180 {
		d -= 360
	}
	return float32(d)
}
