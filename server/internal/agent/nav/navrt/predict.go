package navrt

import (
	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// CmdBackup is the number of sent commands the predictor keeps (the
// client's CMD_BACKUP).
const CmdBackup = 64

// StateOf rebuilds the navsim movement state of a player state: the pmove
// state, the hull and view height of its duck flag, the view yaw, and the
// ground entity and water level categorized in world w (what pmove
// computes again at the start of the next move).
func StateOf(ps *shared.PlayerState, w *navsim.World) navsim.State {
	pm := ps.PMove
	st := navsim.State{PM: pm, Mins: navsim.StandMins(), Maxs: navsim.StandMaxs(), ViewHeight: 22, ViewYaw: ps.ViewAngles[q2const.YAW]}
	if pm.PmFlags&q2const.PMF_DUCKED != 0 {
		st.Maxs, st.ViewHeight = navsim.DuckMaxs(), -2
	}
	if w != nil {
		w.Categorize(&st)
	}
	return st
}

type sentCmd struct {
	seq   int
	cmd   shared.UserCmd
	valid bool
}

// Predictor predicts the bot's own movement between server frames the way
// CL_PredictMovement does: from the last player state the server sent, it
// replays with navsim's pmove the commands the server had not processed
// when it built that frame (sequence numbers after the netchan's incoming
// acknowledged one), so the follower decides on the position its next
// command will run from. It also simulates one command ahead (Step), which
// tells when a jump takes off or a button is touched.
//
// The prediction world is the follower's (the graph's static solids, the
// blockers as believed, visible monsters); it is wrong where the server's
// world moves (a plat under the player), like the client's prediction, and
// each frame starts over from the server's state. A Predictor is not safe
// for concurrent use.
type Predictor struct {
	r    *navsim.Runner
	ring [CmdBackup]sentCmd
	res  navsim.StepResult
}

// NewPredictor returns a predictor stepping runner r (its world is the
// prediction world; nav.Graph.NewRunner gives it the graph's volumes and
// hooks, so Step reports triggers entered).
func NewPredictor(r *navsim.Runner) *Predictor { return &Predictor{r: r} }

// Runner returns the predictor's runner.
func (p *Predictor) Runner() *navsim.Runner { return p.r }

// Reset forgets the sent commands (a new connection restarts the netchan
// sequence numbers).
func (p *Predictor) Reset() { p.ring = [CmdBackup]sentCmd{} }

// Sent records the command sent with netchan sequence seq (the client's
// outgoing sequence when it was sent).
func (p *Predictor) Sent(seq int, cmd shared.UserCmd) {
	p.ring[seq&(CmdBackup-1)] = sentCmd{seq: seq, cmd: cmd, valid: true}
}

// Predict returns the predicted state for the next command: ps (the last
// frame's player state) with the commands of sequences ack+1 .. current-1
// replayed, where ack is the netchan's incoming acknowledged sequence and
// current its outgoing sequence (the next command's). A player state that
// is not PM_NORMAL is returned as it is (pmove does not move the player,
// or not the way navsim simulates).
func (p *Predictor) Predict(ps *shared.PlayerState, ack, current int) navsim.State {
	st := StateOf(ps, p.r.World)
	if ps.PMove.PmType != q2const.PM_NORMAL || current-ack > CmdBackup {
		return st
	}
	p.r.SetState(st, false)
	for seq := ack + 1; seq < current; seq++ {
		s := &p.ring[seq&(CmdBackup-1)]
		if !s.valid || s.seq != seq {
			break
		}
		if s.cmd.Msec == 0 {
			continue // pmove does not move (navsim would take a 0 as the default length)
		}
		p.r.Step(cmdOf(s.cmd, p.r.State().PM.DeltaAngles))
	}
	return p.r.State()
}

// Step simulates command u from state st and returns the state after it
// and what it touched (the result is valid until the next Step or
// Predict).
func (p *Predictor) Step(st navsim.State, u shared.UserCmd) (navsim.State, *navsim.StepResult) {
	p.r.SetState(st, false)
	res := p.r.Step(cmdOf(u, st.PM.DeltaAngles))
	return p.r.State(), res
}

// cmdOf turns a usercmd back into the navsim command that sends it: the
// view angles the server derives from it (navsim.Cmd.UserCmd computes the
// same short angles again).
func cmdOf(u shared.UserCmd, delta [3]int16) navsim.Cmd {
	yaw, pitch := control.ViewAngles(u.Angles, delta)
	return navsim.Cmd{Msec: u.Msec, Buttons: u.Buttons, Forward: u.ForwardMove, Side: u.SideMove, Up: u.UpMove, Yaw: yaw, Pitch: pitch}
}
