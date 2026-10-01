package navrt

import (
	"sort"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// FrameMsec is the server frame the driver's clock advances by when it is
// given no belief clock.
const FrameMsec = 100

// Driver turns the navigator into a session command function: for every
// usercmd it predicts where the command runs (Predictor), asks the
// navigator for the intent, and composes the command (control.Compose)
// with the aim, which is where the intent faces unless Aim says
// otherwise. Call Observe after every server frame with the world
// model's belief, and use Cmd as the session.CmdFunc. One Driver serves
// one level of one session (a level change needs a new navigator); it is
// not safe for concurrent use.
type Driver struct {
	Nav  *Navigator
	Pred *Predictor
	// Aim, when set, chooses the view of each command (the combat layer):
	// the intent's wish direction is projected onto it, and MustFace
	// intents override it.
	Aim func(in control.MoveIntent, s *navsim.State) (yaw, pitch float32)
	// OnCmd, when set, sees each command with the intent and the predicted
	// state it was decided on (for traces and tests).
	OnCmd func(in control.MoveIntent, u shared.UserCmd, s *navsim.State)

	belief   *worldmodel.Belief
	clock    int64
	observed bool
	frame    int32
	sub      int
	gen      int
	last     navsim.StepResult
	haveLast bool
}

// NewDriver returns a driver for navigator n (with a predictor in the
// navigator's prediction world).
func NewDriver(n *Navigator) *Driver {
	return &Driver{Nav: n, Pred: NewPredictor(n.NewRunner()), gen: -1}
}

// Observe hands the driver the belief after a server frame and the
// belief's clock (ms, e.g. session.GameTimeMs): the commands of the next
// frame run from now on, CmdMsec apart.
func (d *Driver) Observe(b *worldmodel.Belief, now int64) {
	d.belief, d.clock, d.observed = b, now, true
}

// Cmd is the session.CmdFunc: the command for the next msec of the client
// c, as of c's latest server frame.
func (d *Driver) Cmd(c *fakeclient.Client, msec int) shared.UserCmd {
	if g := c.LevelGen(); g != d.gen {
		d.gen = g
		d.Pred.Reset()
		d.haveLast = false
	}
	if c.Frame.ServerFrame != d.frame {
		d.frame, d.sub = c.Frame.ServerFrame, 0
	}
	ps := &c.Frame.PlayerState
	cur := c.Netchan.OutgoingSequence
	st := d.Pred.Predict(ps, c.Netchan.IncomingAcknowledged, cur)
	base := d.clock
	if !d.observed {
		base = int64(c.Frame.ServerFrame) * FrameMsec
	}
	in := TickInput{Self: st, Belief: d.belief, Now: base + int64(d.sub*msec)}
	if d.haveLast {
		in.Last = &d.last
	}
	intent := d.Nav.Tick(in)
	yaw, pitch := intent.FaceYaw, intent.FacePitch
	if d.Aim != nil {
		yaw, pitch = d.Aim(intent, &st)
	}
	u := control.Compose(intent, yaw, pitch, st.PM.DeltaAngles, msec)
	d.Pred.Sent(cur, u)
	d.haveLast = false
	if ps.PMove.PmType == q2const.PM_NORMAL && u.Msec > 0 {
		_, res := d.Pred.Step(st, u)
		d.last.Touched = append(d.last.Touched[:0], res.Touched...)
		d.last.Inside = append(d.last.Inside[:0], res.Inside...)
		d.last.Pushed, d.last.Teleported = res.Pushed, res.Teleported
		d.last.FrameEnd, d.last.FallDamage = res.FrameEnd, res.FallDamage
		d.haveLast = true
	}
	if d.OnCmd != nil {
		d.OnCmd(intent, u, &st)
	}
	d.sub++
	return u
}

// ExitTriggers returns the lump entities the graph's edges can set off
// whose chain of uses ends the level (reaches a target_changelevel):
// triggers, buttons, items. Entities that start disabled are left out
// (only something else enabling them makes them fire, and that is listed
// itself). A navigator that must stay on the level avoids them
// (Config.Avoid).
func ExitTriggers(g *nav.Graph, md *mapdata.Map) []int32 {
	var out []int32
	for _, en := range g.Ents {
		if en.Disabled {
			continue
		}
		for _, r := range md.Reach(int(en.Entity)) {
			if r.Response == mapdata.RespExit && !r.Disabled {
				out = append(out, en.Entity)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
