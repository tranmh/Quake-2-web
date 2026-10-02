package runner

import (
	"context"
	"sync/atomic"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/qcommon/shared"
)

// stepper is the session the campaign drives: the run's session with
// every usercmd of a Step recorded (a cmds event per step) and, in an
// actions-mode replay, replaced by the recorded one. It also keeps the
// session clock in an atomic for the logs of other goroutines.
type stepper struct {
	session.Session
	steps int64
	gms   atomic.Int64
	// onStep receives the step's commands (as sent) after it.
	onStep func(step int64, cmds []trace.StepCmd)
	// feed, when set, returns the command to send instead of the bot's
	// (the i-th of step).
	feed func(step int64, i int, bot shared.UserCmd) shared.UserCmd
}

// Step runs one session step, recording (and in a replay, replacing) its
// commands. Like the sessions, it calls f only while the client is
// active; a nil f sends idle commands.
func (s *stepper) Step(ctx context.Context, f session.CmdFunc) error {
	step := s.steps
	s.steps++
	var cmds []trace.StepCmd
	g := func(c *fakeclient.Client, msec int) shared.UserCmd {
		var cmd shared.UserCmd
		if f != nil {
			cmd = f(c, msec)
		}
		cmd.Msec = uint8(msec)
		if s.feed != nil {
			cmd = s.feed(step, len(cmds), cmd)
			cmd.Msec = uint8(msec)
		}
		cmds = append(cmds, traceCmd(cmd))
		return cmd
	}
	err := s.Session.Step(ctx, g)
	s.gms.Store(s.Session.GameTimeMs())
	if len(cmds) > 0 && s.onStep != nil {
		s.onStep(step, cmds)
	}
	return err
}

// Reload reloads the level-entry save (the campaign's Control).
func (s *stepper) Reload(ctx context.Context) error {
	err := s.Session.Reload(ctx)
	s.gms.Store(s.Session.GameTimeMs())
	return err
}

// Start starts the session.
func (s *stepper) Start(ctx context.Context) error {
	err := s.Session.Start(ctx)
	s.gms.Store(s.Session.GameTimeMs())
	return err
}

func traceCmd(c shared.UserCmd) trace.StepCmd {
	return trace.StepCmd{UserCmd: trace.UserCmd{Msec: c.Msec, Buttons: c.Buttons, Angles: c.Angles, Forward: c.ForwardMove,
		Side: c.SideMove, Up: c.UpMove, Impulse: c.Impulse}, Light: c.LightLevel}
}

func userCmd(c trace.StepCmd) shared.UserCmd {
	return shared.UserCmd{Msec: c.Msec, Buttons: c.Buttons, Angles: c.Angles, ForwardMove: c.Forward, SideMove: c.Side,
		UpMove: c.Up, Impulse: c.Impulse, LightLevel: c.Light}
}
