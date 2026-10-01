package control

import (
	"math"

	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// MaxMove is the largest forward, side or up move Compose sends: what a
// client running with the default cl_forwardspeed 200 doubled by cl_run
// sends (pmove caps the resulting speed at 300 on the ground).
const MaxMove = 400

// MaxCmdMsec is the longest usercmd a client sends (CL_FinishMove).
const MaxCmdMsec = 250

// MoveIntent is what the navigation layer wants the player's body to do
// during one usercmd, independently of where the bot looks: Compose turns
// it into the command for whatever aim the combat layer chose, so the bot
// can strafe-run towards its path while it aims elsewhere.
type MoveIntent struct {
	// WishDir is the world-space direction to move in (unit length; zero
	// to stand still). Only its horizontal part counts unless Swim is set.
	WishDir shared.Vec3
	// Speed is the move magnitude in usercmd units: MaxMove runs (pmove
	// caps the ground speed at 300), smaller values move slower.
	Speed float32
	// Swim makes WishDir three-dimensional: in water the view pitch and
	// upmove steer up and down.
	Swim bool
	// Jump presses jump for this command (upmove +MaxMove); pmove needs it
	// released in between to jump again.
	Jump bool
	// Crouch ducks (upmove -MaxMove); it also descends ladders and swims
	// down.
	Crouch bool
	// SwimUp swims or climbs up (upmove +MaxMove) without the meaning of a
	// jump.
	SwimUp bool
	// FaceYaw and FacePitch (degrees, pitch > 0 looks down) are where the
	// movement wants to look: the heading of the path, or what MustFace
	// requires.
	FaceYaw, FacePitch float32
	// MustFace makes the view FaceYaw/FacePitch whatever the aim: ladders,
	// jump takeoffs, swimming, directional triggers and buttons depend on
	// the view.
	MustFace bool
	// Fire holds the attack button (shooting a button).
	Fire bool
	// Stop marks a braking manoeuvre that brings the player to rest at
	// StopAt (an edge that must start from a standstill, a wait spot).
	Stop   bool
	StopAt shared.Vec3
}

// Idle returns the intent that stands still looking at yaw/pitch.
func Idle(yaw, pitch float32) MoveIntent { return MoveIntent{FaceYaw: yaw, FacePitch: pitch} }

// FromCmd returns the intent of an executor command (navsim recipes steer
// with absolute view angles): the world-space direction its forward and
// side moves push in under its view, its upmove as Jump/SwimUp or Crouch,
// and its view as the face angles. swim selects the three-dimensional
// water movement (the pitch steers). Compose(FromCmd(c, swim), c.Yaw,
// c.Pitch, delta, msec) reproduces c.UserCmd(delta) exactly.
func FromCmd(c navsim.Cmd, swim bool) MoveIntent {
	in := MoveIntent{FaceYaw: c.Yaw, FacePitch: c.Pitch, Swim: swim}
	// the wish velocity pmove computes from c (out of the water only its
	// horizontal part, which the pitch shortens: PM_AirMove's view vectors
	// use a third of the view pitch)
	pitch := c.Pitch
	if !swim {
		pitch /= 3
	}
	var fwd, right shared.Vec3
	shared.AngleVectors(shared.Vec3{pitch, c.Yaw, 0}, &fwd, &right, nil)
	var d [3]float64
	for k := 0; k < 3; k++ {
		d[k] = float64(fwd[k])*float64(c.Forward) + float64(right[k])*float64(c.Side)
	}
	if !swim {
		d[2] = 0
	}
	if l := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2]); l > 0 {
		in.Speed = float32(l)
		in.WishDir = shared.Vec3{float32(d[0] / l), float32(d[1] / l), float32(d[2] / l)}
	}
	switch {
	case c.Up > 0 && swim:
		in.SwimUp = true
	case c.Up > 0:
		in.Jump = true
	case c.Up < 0:
		in.Crouch = true
	}
	in.Fire = c.Buttons&q2const.BUTTON_ATTACK != 0
	return in
}

// Compose returns the usercmd that carries out intent in while the bot
// looks at aimYaw/aimPitch (overridden by the intent's face angles when it
// MustFace), for a client with delta_angles delta. The forward and side
// moves are the projection of the intent's wish direction onto the view
// the server will see (the quantized angles, as PM_ClampAngles computes
// them): out of the water the horizontal direction, with the forward move
// scaled up for the view pitch (PM_AirMove's forward vector is pitched by
// a third of it), in water (Swim) the full direction under the full pitch,
// the up move making up the vertical part. Moves
// are bounded by MaxMove keeping their direction, jump, crouch and swim-up
// add to the up move, Fire holds attack, and msec is clamped to
// [0, MaxCmdMsec].
func Compose(in MoveIntent, aimYaw, aimPitch float32, delta [3]int16, msec int) shared.UserCmd {
	yaw, pitch := aimYaw, aimPitch
	if in.MustFace {
		yaw, pitch = in.FaceYaw, in.FacePitch
	}
	u := shared.UserCmd{Msec: uint8(min(max(msec, 0), MaxCmdMsec))}
	u.Angles = CmdAngles(yaw, pitch, delta)
	vy, vp := ViewAngles(u.Angles, delta)
	if !in.Swim {
		vp /= 3 // PM_AirMove: forward and right of a third of the pitch
	}
	sy, cy := math.Sincos(float64(vy) * math.Pi / 180)
	sp, cp := math.Sincos(float64(vp) * math.Pi / 180)

	var f, s, up float64
	if in.Speed > 0 {
		w := [3]float64{float64(in.WishDir[0]), float64(in.WishDir[1]), float64(in.WishDir[2])}
		if !in.Swim {
			w[2] = 0
			if l := math.Hypot(w[0], w[1]); l > 0 {
				w[0], w[1] = w[0]/l, w[1]/l
			}
		}
		sp0 := float64(in.Speed)
		// pmove: wishvel = forward*fmove + right*smove (+ up*upmove in
		// water), forward = (cp*cy, cp*sy, -sp), right = (sy, -cy, 0)
		h := (w[0]*cy + w[1]*sy) * sp0
		if cp > 1e-3 {
			f = h / cp
		}
		s = (w[0]*sy - w[1]*cy) * sp0
		if in.Swim {
			up = w[2]*sp0 + f*sp
		}
		if m := math.Max(math.Abs(f), math.Max(math.Abs(s), math.Abs(up))); m > MaxMove {
			k := MaxMove / m
			f, s, up = f*k, s*k, up*k
		}
	}
	if in.Jump || in.SwimUp {
		up += MaxMove
	}
	if in.Crouch {
		up -= MaxMove
	}
	u.ForwardMove = moveShort(f)
	u.SideMove = moveShort(s)
	u.UpMove = moveShort(up)
	if in.Fire {
		u.Buttons |= q2const.BUTTON_ATTACK
	}
	return u
}

func moveShort(v float64) int16 {
	v = math.Round(v)
	return int16(math.Max(-MaxMove, math.Min(MaxMove, v)))
}

// MsecBudget keeps the msec a client sends within what the server grants:
// SV_ExecuteClientMessage drops commands once a client used more than its
// commandMsec allowance, which SV_GiveMsec resets to 1800 every 16 frames
// (1.6 s). A client that sends no more command time than game time passes,
// plus a little slack for jitter, is never cut off. The zero value is
// ready with the default slack.
type MsecBudget struct {
	// Slack is how far the sent time may run ahead of the clock (0: 200
	// ms, the server's allowance beyond 16 frames).
	Slack int64

	started bool
	start   int64
	sent    int64
}

// Allow returns the msec (at most want, at least 0) a command may claim at
// clock time now (ms, any monotonic clock), and counts it as sent.
func (b *MsecBudget) Allow(now int64, want int) int {
	if !b.started {
		b.started, b.start = true, now
	}
	slack := b.Slack
	if slack <= 0 {
		slack = 200
	}
	room := now - b.start + slack - b.sent
	n := int64(min(max(want, 0), MaxCmdMsec))
	if n > room {
		n = max(room, 0)
	}
	b.sent += n
	return int(n)
}

// Reset restarts the budget (a new connection: the server's allowance
// starts over).
func (b *MsecBudget) Reset() { *b = MsecBudget{Slack: b.Slack} }
