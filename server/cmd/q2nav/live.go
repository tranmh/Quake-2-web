package main

import (
	"context"
	"fmt"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// liveServer runs edges on a real lockstep server through the fakeclient:
// the player is put at an edge's start node by writing its edict (a tool
// only shortcut: Quake II has no setpos), left to settle, and then the
// recipe's commands are sent as usercmds through control.CmdAngles.
// Monsters are removed (freed without dying, so no death targets fire) and
// the player is in god and notarget mode, so only the movement is tested.
type liveServer struct {
	l      *session.Lockstep
	g      *game.Game
	player *game.Edict
	freed  int
	// barrels move when touched (barrel_touch); they are put back before
	// every sample so each edge runs in the level as it starts
	barrels []barrelAt
}

type barrelAt struct {
	e      *game.Edict
	origin shared.Vec3
}

// settleFrames is how long the player rests at a start node (300 ms).
const settleFrames = 3

func startLive(ctx context.Context, p *pak.Pak, name string, skill int, seed uint32) (*liveServer, error) {
	fsys := &pak.FS{}
	fsys.AddPak(p)
	l := session.NewLockstep(session.LockstepConfig{
		FS: fsys, Spec: session.Spec{Map: name, Skill: skill}, Seed: seed,
		Client: fakeclient.Options{MaxHistory: 64},
	})
	if err := l.Start(ctx); err != nil {
		return nil, err
	}
	g, ok := l.Server().Game().(*game.Game)
	if !ok {
		_ = l.Close()
		return nil, fmt.Errorf("live: no game module")
	}
	ls := &liveServer{l: l, g: g, player: &g.Edicts()[1]}
	c := l.Client()
	c.StringCmd("god")
	c.StringCmd("notarget")
	for i := 0; i < 2; i++ {
		if err := l.Step(ctx, nil); err != nil {
			_ = l.Close()
			return nil, err
		}
	}
	eds := g.Edicts()
	for i := range eds {
		e := &eds[i]
		if e.InUse && e.Client == nil && e.SVFlags&q2const.SVF_MONSTER != 0 && e.SVFlags&q2const.SVF_DEADMONSTER == 0 {
			g.G_FreeEdict(e)
			ls.freed++
		}
	}
	// two more frames: barrels drop to the floor two frames after spawning
	for i := 0; i < 2; i++ {
		if err := l.Step(ctx, nil); err != nil {
			_ = l.Close()
			return nil, err
		}
	}
	for i := range eds {
		if e := &eds[i]; e.InUse && e.Classname == "misc_explobox" {
			ls.barrels = append(ls.barrels, barrelAt{e, e.S.Origin})
		}
	}
	return ls, nil
}

// restore puts pushed barrels back.
func (ls *liveServer) restore() {
	for _, b := range ls.barrels {
		if b.e.InUse && b.e.S.Origin != b.origin {
			b.e.S.Origin = b.origin
			b.e.Velocity = shared.Vec3{}
			ls.l.Server().World.LinkEdict(b.e)
		}
	}
}

func (ls *liveServer) Close() error { return ls.l.Close() }

// place writes the player's edict to stand at node n at rest and lets it
// settle with the node's hold command; it returns the player state the
// client then sees.
func (ls *liveServer) place(ctx context.Context, n *nav.Node) (shared.PmoveState, error) {
	ls.restore()
	e := ls.player
	e.S.Origin, e.S.OldOrigin = n.Origin, n.Origin
	e.Velocity = shared.Vec3{}
	e.Groundentity = nil
	pm := &e.Client.PS.PMove
	pm.Velocity = [3]int16{}
	pm.PmTime = 0
	pm.PmFlags = q2const.PMF_ON_GROUND
	if n.Flags&nav.NodeCrouch != 0 {
		pm.PmFlags |= q2const.PMF_DUCKED
	}
	ls.l.Server().World.LinkEdict(e)
	hold := nav.HoldCmd(n)
	for i := 0; i < settleFrames; i++ {
		if err := ls.l.Step(ctx, cmdFunc(func() navsim.Cmd { return hold })); err != nil {
			return shared.PmoveState{}, err
		}
	}
	return ls.l.Client().Frame.PlayerState.PMove, nil
}

// cmdFunc turns navsim commands into usercmds for the session.
func cmdFunc(next func() navsim.Cmd) session.CmdFunc {
	return func(c *fakeclient.Client, _ int) shared.UserCmd {
		nc := next()
		u := shared.UserCmd{Buttons: nc.Buttons, ForwardMove: nc.Forward, SideMove: nc.Side, UpMove: nc.Up}
		u.Angles = control.CmdAngles(nc.Yaw, nc.Pitch, c.Frame.PlayerState.PMove.DeltaAngles)
		return u
	}
}

// run sends the commands (padded with hold to whole frames) and returns
// the player state after each frame.
func (ls *liveServer) run(ctx context.Context, cmds []navsim.Cmd, hold navsim.Cmd) ([]shared.PmoveState, error) {
	i := 0
	next := func() navsim.Cmd {
		c := hold
		if i < len(cmds) {
			c = cmds[i]
		}
		i++
		return c
	}
	var frames []shared.PmoveState
	for i < len(cmds) {
		if err := ls.l.Step(ctx, cmdFunc(next)); err != nil {
			return nil, err
		}
		frames = append(frames, ls.l.Client().Frame.PlayerState.PMove)
	}
	return frames, nil
}

// stateOf rebuilds a navsim state from the client's view of the player
// (the follower does the same), categorized in world w.
func stateOf(pm shared.PmoveState, w *navsim.World) navsim.State {
	st := navsim.State{PM: pm, Mins: navsim.StandMins(), Maxs: navsim.StandMaxs(), ViewHeight: 22}
	if pm.PmFlags&q2const.PMF_DUCKED != 0 {
		st.Maxs, st.ViewHeight = navsim.DuckMaxs(), -2
	}
	w.Categorize(&st)
	return st
}
