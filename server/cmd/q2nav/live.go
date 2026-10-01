package main

import (
	"context"
	"fmt"
	"strings"

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
	// movers (MOVETYPE_PUSH/STOP edicts) at the start, and the number of
	// edicts in use: a sample that set something off (a button brushed
	// while coasting to a stop, a trigger_once that freed itself) changes
	// them, and the server is restarted
	movers []moverAt
	inUse  int
}

type moverAt struct {
	e              *game.Edict
	origin, angles shared.Vec3
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
	return ls, nil
}

// solidMover: a brush entity that moves and blocks (not a decoration
// like misc_strogg_ship, which flies around non-solid).
func solidMover(e *game.Edict) bool {
	return e.InUse && e.Solid != q2const.SOLID_NOT && (e.Movetype == game.MOVETYPE_PUSH || e.Movetype == game.MOVETYPE_STOP)
}

// moving reports whether a solid mover translates, or rotates without
// spinning for good (func_rotating keeps its avelocity).
func moving(e *game.Edict, angles0 shared.Vec3) bool {
	return e.Velocity != (shared.Vec3{}) || (e.Avelocity == (shared.Vec3{}) && e.S.Angles != angles0)
}

// quiesce parks the player at node n and steps until the level is as it
// starts: every mover of the graph back at its spawn pose and at rest
// (doors the monsters or the spawn spot opened close again; at most a
// minute of game time). Then it takes the snapshot changed compares with.
func (ls *liveServer) quiesce(ctx context.Context, g *nav.Graph, n *nav.Node) error {
	if _, err := ls.place(ctx, n); err != nil {
		return err
	}
	hold := nav.HoldCmd(n)
	eds := ls.g.Edicts()
	spawn := map[string]nav.BlockerPose{}
	for _, b := range g.Blockers {
		if b.Spawn >= 0 && b.Model != "" {
			spawn[b.Model] = b.Poses[b.Spawn]
		}
	}
	still := 0
	for f := 0; f < 600 && still < 10; f++ {
		if err := ls.l.Step(ctx, cmdFunc(func() navsim.Cmd { return hold })); err != nil {
			return err
		}
		home := true
		for i := range eds {
			e := &eds[i]
			if !solidMover(e) {
				continue
			}
			if e.Velocity != (shared.Vec3{}) {
				home = false
			}
			if p, ok := spawn[e.Model]; ok && (e.S.Origin != p.Origin || (e.Avelocity == (shared.Vec3{}) && e.S.Angles != p.Angles)) {
				home = false
			}
		}
		if home {
			still++
		} else {
			still = 0
		}
	}
	ls.barrels, ls.movers, ls.inUse = nil, nil, ls.counted()
	for i := range eds {
		e := &eds[i]
		if !e.InUse {
			continue
		}
		if e.Classname == "misc_explobox" {
			ls.barrels = append(ls.barrels, barrelAt{e, e.S.Origin})
		}
		if solidMover(e) {
			ls.movers = append(ls.movers, moverAt{e, e.S.Origin, e.S.Angles})
		}
	}
	return nil
}

// changed reports whether the level is no longer as it was at the
// snapshot: a solid mover moved or is moving, or edicts were freed or
// spawned. Spinning func_rotating movers are expected to turn.
func (ls *liveServer) changed() string {
	if n := ls.counted(); n != ls.inUse {
		return fmt.Sprintf("%d entities in use, %d at the start", n, ls.inUse)
	}
	for _, m := range ls.movers {
		if !m.e.InUse || m.e.S.Origin != m.origin || moving(m.e, m.angles) {
			return fmt.Sprintf("%s %s moved", m.e.Classname, m.e.Model)
		}
	}
	return ""
}

// counted is the number of edicts in use that matter for movement: all
// but the items (picked up on the way, which frees them), the player and
// the DelayedUse helpers func_timers keep spawning (G_UseTargets with a
// delay).
func (ls *liveServer) counted() int {
	n := 0
	eds := ls.g.Edicts()
	for i := range eds {
		e := &eds[i]
		if !e.InUse || e.Client != nil || isItem(e.Classname) || e.Classname == "DelayedUse" {
			continue
		}
		n++
	}
	return n
}

func isItem(c string) bool {
	for _, p := range []string{"item_", "weapon_", "ammo_", "key_"} {
		if strings.HasPrefix(c, p) {
			return true
		}
	}
	return false
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
