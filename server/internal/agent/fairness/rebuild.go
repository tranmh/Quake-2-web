package fairness

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Recording is a recorded level attempt: the .dm2 file of the level
// (demo.Recorder) and the episode's trace (its decision tick events are
// the ones of that level).
type Recording struct {
	Demo   []byte
	Events []trace.Event
}

// Config is the bot as the recorded episode configured it.
type Config struct {
	// Bot is the bot's configuration (ReadFile, Seed, ...); Rebuild sets
	// Policy (from NewPolicy) and OnDecision.
	Bot bot.Config
	// NewPolicy returns a fresh Policy configured like the episode's and
	// a function releasing it.
	NewPolicy func() (bot.Policy, func(), error)
	// Level is the level the bot entered (Gen is the passive client's,
	// filled in by Rebuild).
	Level bot.Level
	// Client holds the recording client's options that change what the
	// bot reads (MaxHistory); Passive is forced on.
	Client fakeclient.Options
	// CmdMsec is the length of a usercmd and FrameMsec the server frame
	// time (0: the lockstep session's, 25 and 100).
	CmdMsec, FrameMsec int
}

// Result counts what Rebuild compared.
type Result struct {
	Blocks int // .dm2 blocks fed
	Frames int // frames the bot folded
	Ticks  int // tick events compared
	Cmds   int // usercmds compared
}

// recTick is a recorded tick event and the commands it carries.
type recTick struct {
	ev  trace.Event
	dec trace.Decision
}

// Rebuild plays rec back into a fresh bot on a passive client and
// compares every decision with the recording: each usercmd the bot builds
// with the recorded command's (fields, netchan numbers and frame) and each
// tick event it emits with the recorded one (trace.Comparable, the bus's
// stamps taken from the recorded event). It returns the first difference
// as an error. See the package documentation for the normalizations.
func Rebuild(rec Recording, cfg Config) (Result, error) {
	var res Result
	if cfg.CmdMsec == 0 {
		cfg.CmdMsec = 25
	}
	if cfg.FrameMsec == 0 {
		cfg.FrameMsec = 100
	}
	ticks, err := recordedTicks(rec.Events, cfg.Level.Key.Map, cfg.FrameMsec)
	if err != nil {
		return res, err
	}
	var cmds []trace.UserCmd
	for _, t := range ticks {
		cmds = append(cmds, t.dec.Cmds...)
	}
	if len(cmds) == 0 {
		return res, errors.New("fairness: the recording has no commands")
	}
	blocks, err := readBlocks(rec.Demo)
	if err != nil {
		return res, err
	}
	frames, err := scanFrames(blocks, cfg.Client)
	if err != nil {
		return res, err
	}

	c := fakeclient.NewPassive(cfg.Client)
	c.Netchan.Message.SZ_Init(make([]byte, q2const.MAX_MSGLEN))
	bi := 0
	feed := func() error {
		spans, err := c.FeedPayload(blocks[bi])
		if err != nil {
			return fmt.Errorf("fairness: block %d: %w", bi, err)
		}
		for _, sp := range spans {
			if sp.Cmd == q2const.Svc_serverdata {
				// the header's attract loop is the recording's, not the
				// live client's (it would freeze the player's pmove)
				c.ServerData.AttractLoop = 0
			}
		}
		bi++
		res.Blocks++
		return nil
	}
	// up to the frame the bot's first command was built on: the bot
	// entered the level there
	for c.Frame.ServerFrame != cmds[0].Frame || !c.Frame.Valid {
		if bi == len(blocks) {
			return res, fmt.Errorf("fairness: the recording has no frame %d (the first command's)", cmds[0].Frame)
		}
		if err := feed(); err != nil {
			return res, err
		}
	}
	if c.State != fakeclient.CaActive {
		return res, fmt.Errorf("fairness: the passive client is not active at frame %d", c.Frame.ServerFrame)
	}

	pol, release, err := cfg.NewPolicy()
	if err != nil {
		return res, err
	}
	defer release()
	var got []trace.Decision
	bcfg := cfg.Bot
	bcfg.Policy = pol
	bcfg.OnDecision = func(d *trace.Decision) {
		if d.Lane == trace.LaneTick {
			got = append(got, *d)
		}
	}
	b := bot.New(bcfg)
	lv := cfg.Level
	lv.Gen = c.LevelGen()
	if err := b.Enter(lv); err != nil {
		return res, err
	}

	// the session clock: lockstep advances it one frame time per frame
	clock := ticks[0].ev.GMs - int64(ticks[0].ev.SF)*int64(cfg.FrameMsec)
	ci, ti := 0, 0
	for bi < len(blocks) && (ci < len(cmds) || ti < len(ticks)) {
		// the commands built on the current frame went out before the
		// next message
		for ci < len(cmds) && cmds[ci].Frame == c.Frame.ServerFrame {
			want := cmds[ci]
			c.Netchan.OutgoingSequence, c.Netchan.IncomingAcknowledged = want.Seq, want.Ack
			u := b.Cmd(c, cfg.CmdMsec)
			if have := traceCmd(u, want); have != want {
				return res, fmt.Errorf("fairness: command %d (frame %d) differs:\nrebuilt  %+v\nrecorded %+v", ci, want.Frame, have, want)
			}
			ci++
			res.Cmds++
		}
		hasFrame := frames[bi] >= 0
		if err := feed(); err != nil {
			return res, err
		}
		if !hasFrame {
			continue
		}
		got = got[:0]
		if b.Observe(c, clock+int64(c.Frame.ServerFrame)*int64(cfg.FrameMsec)) {
			res.Frames++
		}
		c.Netchan.Message.SZ_Clear() // the side commands: a passive client sends nothing
		for _, d := range got {
			if ti == len(ticks) {
				return res, fmt.Errorf("fairness: the rebuilt bot made a tick at frame %d the recording does not have", c.Frame.ServerFrame)
			}
			if err := sameTick(ticks[ti].ev, d, c.Frame.ServerFrame, clock+int64(c.Frame.ServerFrame)*int64(cfg.FrameMsec)); err != nil {
				return res, fmt.Errorf("fairness: tick %d: %w", ti, err)
			}
			ti++
			res.Ticks++
		}
	}
	switch {
	case ci < len(cmds):
		return res, fmt.Errorf("fairness: %d of %d recorded commands rebuilt (next built on frame %d, the recording ended at frame %d)",
			ci, len(cmds), cmds[ci].Frame, c.Frame.ServerFrame)
	case ti < len(ticks):
		return res, fmt.Errorf("fairness: %d of %d recorded ticks rebuilt (next at frame %d)", ti, len(ticks), ticks[ti].ev.SF)
	}
	return res, nil
}

// recordedTicks returns the tick events of level m in trace order and
// refuses a recording Rebuild cannot cover: more than one attempt at the
// level (a death and its reload), an explore burst the campaign ordered
// (an input from outside the bot), a clock that does not advance one
// frame time per frame.
func recordedTicks(events []trace.Event, m string, frameMsec int) ([]recTick, error) {
	var out []recTick
	for _, e := range events {
		switch e.Type {
		case trace.TypeDeath, trace.TypeReload:
			return nil, fmt.Errorf("fairness: the recording has a %s: Rebuild covers one level attempt", e.Type)
		case trace.TypeDecision:
		default:
			continue
		}
		if e.Map != m {
			continue
		}
		var d trace.Decision
		if err := e.DecodeBody(&d); err != nil {
			return nil, err
		}
		if d.Lane != trace.LaneTick {
			continue
		}
		if d.Tick != nil && d.Tick.Mode == string(bot.ModeExplore) {
			return nil, fmt.Errorf("fairness: the bot explored at %d ms (the campaign's watchdog: not part of a rebuild)", e.GMs)
		}
		out = append(out, recTick{ev: e, dec: d})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fairness: no tick events on %s", m)
	}
	t0 := out[0].ev
	for _, t := range out {
		if t.ev.GMs-t0.GMs != int64(t.ev.SF-t0.SF)*int64(frameMsec) {
			return nil, fmt.Errorf("fairness: tick at frame %d is stamped %d ms, not one frame time per frame after frame %d (%d ms)",
				t.ev.SF, t.ev.GMs, t0.SF, t0.GMs)
		}
	}
	return out, nil
}

// readBlocks returns the server messages of a .dm2 file.
func readBlocks(data []byte) ([][]byte, error) {
	r := demo.NewReader(bytes.NewReader(data))
	var out [][]byte
	for {
		b, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		out = append(out, append([]byte(nil), b...))
	}
	if !r.Terminated() {
		return nil, errors.New("fairness: the .dm2 file is not terminated")
	}
	return out, nil
}

// scanFrames parses blocks on a scratch passive client and returns, per
// block, the server frame it carries (-1 for none).
func scanFrames(blocks [][]byte, opt fakeclient.Options) ([]int32, error) {
	c := fakeclient.NewPassive(opt)
	out := make([]int32, len(blocks))
	for i, b := range blocks {
		spans, err := c.FeedPayload(b)
		if err != nil {
			return nil, fmt.Errorf("fairness: block %d: %w", i, err)
		}
		out[i] = -1
		for _, sp := range spans {
			if sp.Cmd == q2const.Svc_frame {
				out[i] = c.Frame.ServerFrame
			}
		}
	}
	return out, nil
}

// traceCmd is u as a tick event carries it, with the netchan numbers and
// frame of want (Rebuild sets the former and builds on the latter).
func traceCmd(u shared.UserCmd, want trace.UserCmd) trace.UserCmd {
	return trace.UserCmd{Msec: u.Msec, Buttons: u.Buttons, Angles: u.Angles, Forward: u.ForwardMove, Side: u.SideMove, Up: u.UpMove,
		Impulse: u.Impulse, Seq: want.Seq, Ack: want.Ack, Frame: want.Frame}
}

// sameTick compares a rebuilt tick event (made on server frame sf at
// clock gms) with the recorded one: the whole event, wall clock aside,
// with the bus's and the campaign's stamps (run, episode, sequence, level
// index, map, version) taken from the recorded event.
func sameTick(want trace.Event, d trace.Decision, sf int32, gms int64) error {
	have := trace.Event{V: want.V, Type: trace.TypeDecision, Run: want.Run, Ep: want.Ep, Seq: want.Seq, GMs: gms, Lvl: want.Lvl,
		Map: want.Map, SF: sf, Body: d}
	a, err := trace.Comparable(have)
	if err != nil {
		return err
	}
	w, err := trace.Comparable(want)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, w) {
		return fmt.Errorf("differs at frame %d:\nrebuilt  %s\nrecorded %s", sf, a, w)
	}
	return nil
}
