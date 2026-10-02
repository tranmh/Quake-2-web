// Package bot is the agent's per-tick loop: it folds every new server
// frame into the world model (fair perception only), asks its Policy for
// an Intent with the route's objective, runs the route executor (package
// routeexec) or the mode the policy chose, and turns the navigator's
// movement plus the shoot executor's aim and trigger into the session's
// usercmds (four 25 ms commands per server frame). It also sends the side
// commands the world model asks for (the inventory and the help computer,
// at most one pair every 2 s and never in a fight) and leaves an
// intermission by pressing a button after 5.5 s of PM_FREEZE.
//
// A Bot serves one session. Call Enter at every level entry (including a
// reload of the entry save) with the level's static data, Observe after
// every session Step, and pass Cmd as the session.CmdFunc. Nothing here
// reads server state; TestImports keeps the server, game and session
// packages out. A Bot is not safe for concurrent use.
package bot

import (
	"errors"
	"fmt"
	"math/rand"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/routeexec"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// Timing of the bot loop (ms of game time).
const (
	// SideInterval is the least time between two side command pairs.
	SideInterval = 2000
	// IntermissionPress is how long the bot waits in PM_FREEZE before it
	// presses a button (the game accepts one after 5 s).
	IntermissionPress = 5500
	// exploreLeg bounds one explore leg before another spot is picked.
	exploreLeg = 20000
)

// Config configures a Bot. The zero value works (no animations, the
// default class table, ObjectivePolicy).
type Config struct {
	// ReadFile reads game data for the world model (MD2 animation names),
	// usually the session's ReadFile.
	ReadFile func(name string) ([]byte, error)
	// Perception configures the observation filter (field of view).
	Perception perception.Options
	// Classes is the class table (nil: perception.NewClassTable()).
	Classes *perception.ClassTable
	// Anims shares an animation cache between bots (nil: own cache).
	Anims *perception.AnimCache
	// Policy decides the intent each frame (nil: ObjectivePolicy).
	Policy Policy
	// Nav configures each level's navigator; the route executor sets its
	// avoid set, and OnBlocked is chained after the level memory's record.
	Nav navrt.Config
	// Route configures each level's route executor.
	Route routeexec.Config
	// Seed seeds the explore choices (deterministic per seed).
	Seed int64
	// Logf, when set, receives the route executor's step events.
	Logf func(format string, args ...any)
}

// Level is a level the bot enters: the visit key (a reload keeps it),
// the client's level generation, the static map knowledge and the visit's
// route (nil: no route, the bot idles or explores).
type Level struct {
	Key   worldmodel.LevelKey
	Gen   int
	Map   *mapdata.Map
	Graph *nav.Graph
	Route *route.Table
}

// Mode is what the bot does this frame.
type Mode string

// Modes.
const (
	ModeNone         Mode = "none" // no level entered
	ModeObjective    Mode = "objective"
	ModeFight        Mode = "fight"
	ModePickup       Mode = "pickup"
	ModeExplore      Mode = "explore"
	ModeDead         Mode = "dead"
	ModeIntermission Mode = "intermission"
	ModeDone         Mode = "done" // the route is done (waiting for the level to end)
)

// Bot is the per-tick loop (see the package documentation).
type Bot struct {
	cfg    Config
	world  *worldmodel.World
	reader *perception.Reader
	policy Policy
	shoot  Shooter
	rng    *rand.Rand

	lv      Level
	entered bool
	nav     *navrt.Navigator
	drv     *navrt.Driver
	exec    *routeexec.Executor
	started bool

	now     int64
	frames  int
	intent  decide.Intent
	dir     routeexec.Directive
	mode    Mode
	navMine bool // the bot (not the route executor) set the navigator's goal

	target     string // track fought this frame
	firePolicy decide.FirePolicy
	fire       bool // this command's trigger
	fired      int  // commands with the trigger held

	freezeSince int64
	pressed     bool
	sideAt      int64
	sideSent    int

	exploreUntil int64
	exploreAt    int64
	explores     int
}

// New returns a bot with no level: call Enter once the client is active on
// one.
func New(cfg Config) *Bot {
	b := &Bot{cfg: cfg, reader: perception.NewReader(), policy: cfg.Policy, mode: ModeNone, sideAt: -SideInterval}
	if b.policy == nil {
		b.policy = ObjectivePolicy{}
	}
	b.world = worldmodel.New(worldmodel.Config{ReadFile: cfg.ReadFile, Perception: cfg.Perception, Classes: cfg.Classes, Anims: cfg.Anims})
	b.rng = rand.New(rand.NewSource(cfg.Seed))
	return b
}

// ErrNoGraph is returned by Enter without a graph or map data.
var ErrNoGraph = errors.New("bot: a level needs its map data and nav graph")

// Enter starts a level: the world model resets to it (a key entered before
// restores its level memory), and the bot gets a new navigator, driver and
// route executor. Frames of other level generations are ignored from now
// on.
func (b *Bot) Enter(lv Level) error {
	if lv.Map == nil || lv.Graph == nil {
		return ErrNoGraph
	}
	b.world.Reset(worldmodel.Level{Key: lv.Key, Map: lv.Map})
	ncfg := b.cfg.Nav
	user := ncfg.OnBlocked
	ncfg.OnBlocked = func(edge int, key string, until int64) {
		b.world.MarkBlocked(key)
		if user != nil {
			user(edge, key, until)
		}
	}
	n := navrt.New(lv.Graph, lv.Map, ncfg)
	var x *routeexec.Executor
	if lv.Route != nil {
		rcfg := b.cfg.Route
		if rcfg.Classes == nil {
			rcfg.Classes = b.cfg.Classes
		}
		if rcfg.Logf == nil && b.cfg.Logf != nil {
			name := lv.Route.Name
			rcfg.Logf = func(format string, args ...any) { b.cfg.Logf(name+": "+format, args...) }
		}
		var err error
		if x, err = routeexec.New(lv.Route, lv.Map, lv.Graph, n, rcfg); err != nil {
			return err
		}
	}
	b.lv, b.entered = lv, true
	b.nav, b.exec = n, x
	b.drv = navrt.NewDriver(n)
	b.drv.Aim = b.aim
	b.started = false
	b.frames = 0
	b.intent = decide.Intent{}
	b.dir = routeexec.Directive{}
	b.mode = ModeObjective
	b.navMine = false
	b.target, b.fire = "", false
	b.freezeSince, b.pressed = 0, false
	b.exploreUntil, b.exploreAt = 0, 0
	b.shoot.Reset()
	return nil
}

// Observe folds the client's latest server frame into the world model
// (when it is a new one of the entered level), decides what to do and
// queues side commands on c. now is the session clock (ms). It reports
// whether a frame was folded.
func (b *Bot) Observe(c *fakeclient.Client, now int64) bool {
	in, ok := b.reader.Next(c)
	if !ok || !b.entered || in.Level == nil || in.Level.Gen != b.lv.Gen {
		return false
	}
	b.world.Update(in, now)
	b.now = now
	b.frames++
	bel := b.world.Belief()
	if !b.started && b.exec != nil {
		b.exec.Start(now)
	}
	b.started = true
	b.decide(c, bel)
	b.drv.Observe(bel, now)
	return true
}

// decide runs the frame's decisions: the mode, the route or the mode's
// navigation goal, the fight target and weapon, and the side commands.
func (b *Bot) decide(c *fakeclient.Client, bel *worldmodel.Belief) {
	s := &bel.Self
	b.target = ""
	switch {
	case s.PmType == q2const.PM_FREEZE:
		if b.freezeSince == 0 {
			b.freezeSince = b.now
		}
		b.mode = ModeIntermission
		return
	case s.Dead:
		b.freezeSince = 0
		b.mode = ModeDead
		return
	}
	b.freezeSince, b.pressed = 0, false

	var obj *decide.ObjectiveView
	if b.exec != nil {
		obj = b.exec.Objective()
	}
	b.intent = b.policy.Tick(b.now, bel, obj)
	b.firePolicy = b.intent.FirePolicy

	mode := ModeObjective
	switch {
	case b.exploreUntil > 0:
		mode = ModeExplore
	case b.intent.Mode == decide.ModeFight && b.liveTrack(bel, b.intent.Target) != nil:
		mode = ModeFight
	case b.intent.Mode == decide.ModePickup && b.pickupItem(bel, b.intent.Pickup) != nil:
		mode = ModePickup
	case b.intent.Mode == decide.ModeExplore && b.exec == nil:
		mode = ModeExplore
	}
	if mode != ModeObjective && b.exec != nil {
		b.exec.Yield()
	}
	b.dir = routeexec.Directive{Hold: true}
	switch mode {
	case ModeObjective:
		if b.navMine {
			b.nav.ClearGoal()
			b.navMine = false
		}
		if b.exec == nil {
			break
		}
		b.dir = b.exec.Update(b.now, bel)
		if b.dir.Done {
			mode = ModeDone
		}
		if k := b.dir.Kill; k != nil && k.Track != "" {
			b.target = k.Track
			if b.firePolicy == decide.FireHold || b.firePolicy == "" {
				b.firePolicy = decide.FireWhenAligned
			}
		}
	case ModeFight:
		// phase 4 has no fight movement: the bot stands and fights (the
		// decision layer's movement field is wave 5's)
		b.target = b.intent.Target
		b.takeNav()
		b.nav.ClearGoal()
	case ModePickup:
		b.pickup(bel)
	case ModeExplore:
		b.exploreTick(bel)
	}
	b.mode = mode

	// weapons: in a fight the best one for the range, else the one the
	// policy asks for
	want := b.intent.Weapon
	if tr := bel.Track(b.target); tr != nil {
		want = b.shoot.Choose(b.now, bel, dist3(s.Eye, tr.Pos), b.intent.Weapon)
	} else if want != decide.WeaponKeep && !usable(bel, want) {
		want = decide.WeaponKeep
	}
	if want != decide.WeaponKeep {
		if cmd := b.shoot.Switch(b.now, bel, want); cmd != "" {
			c.StringCmd(cmd)
		}
	}
	b.sideCommands(c, bel)
}

// takeNav makes the navigator the bot's (the route executor sets its goal
// again when the bot is back on the objective).
func (b *Bot) takeNav() {
	if b.exec != nil {
		b.exec.Yield()
	}
	b.navMine = true
}

func (b *Bot) liveTrack(bel *worldmodel.Belief, id string) *worldmodel.Track {
	if id == "" {
		return nil
	}
	if t := bel.Track(id); t != nil && t.Life == worldmodel.LifeAlive {
		return t
	}
	return nil
}

func (b *Bot) pickupItem(bel *worldmodel.Belief, id string) *worldmodel.Item {
	if id == "" {
		return nil
	}
	for i := range bel.Items {
		if it := &bel.Items[i]; it.ID == id && it.Life == worldmodel.LifeAlive {
			return it
		}
	}
	return nil
}

// pickup heads for the item the policy chose.
func (b *Bot) pickup(bel *worldmodel.Belief) {
	it := b.pickupItem(bel, b.intent.Pickup)
	if it == nil {
		return
	}
	goal := navrt.PointGoal(it.Pos, 16)
	if it.Lump >= 0 {
		goal = navrt.ItemGoal(int32(it.Lump))
	}
	if g, ok := b.nav.Goal(); !b.navMine || !ok || g.Kind != goal.Kind || g.Entity != goal.Entity || g.Point != goal.Point {
		b.takeNav()
		if err := b.nav.SetGoal(goal, b.now); err != nil {
			b.nav.ClearGoal()
		}
	}
}

// sideCommands sends the inventory or help refresh the world model asks
// for: one pair at most every SideInterval, never in a fight or while the
// bot is busy with an aim-sensitive step.
func (b *Bot) sideCommands(c *fakeclient.Client, bel *worldmodel.Belief) {
	if bel.Self.InCombat || b.target != "" || b.now-b.sideAt < SideInterval {
		return
	}
	switch {
	case b.world.WantsInventoryRefresh():
		c.StringCmd("inven")
		c.StringCmd("putaway")
		b.world.NoteInventoryRequested()
	case b.world.WantsHelpRefresh():
		c.StringCmd("help")
		c.StringCmd("putaway")
		b.world.NoteHelpRequested()
	default:
		return
	}
	b.sideAt = b.now
	b.sideSent++
}

// Explore makes the bot wander to spots of the level until the clock
// passes until (ms), then start the route's current step over (the
// campaign's no-progress watchdog).
func (b *Bot) Explore(until int64) {
	if b.exec != nil {
		b.exec.Yield()
	}
	b.exploreUntil, b.exploreAt = until, 0
	b.explores++
}

// exploreTick picks a spot 256 to 1024 units away and walks there,
// another one when it arrives, fails or takes too long.
func (b *Bot) exploreTick(bel *worldmodel.Belief) {
	if b.exploreUntil > 0 && b.now >= b.exploreUntil {
		b.exploreUntil = 0
		b.nav.ClearGoal()
		b.navMine = false
		if b.exec != nil {
			b.exec.Yield()
			b.exec.Retry()
		}
		return
	}
	st := b.nav.Status()
	_, has := b.nav.Goal()
	if b.navMine && has && st.Follow != navrt.Arrived && st.Follow != navrt.Failed && b.now-b.exploreAt < exploreLeg {
		return
	}
	g := b.lv.Graph
	cands := g.Nearby(bel.Self.Origin, 1024)
	var pick []nav.NodeID
	for _, c := range cands {
		nd := &g.Nodes[c.Node]
		if nd.Flags&(nav.NodeCrouch|nav.NodeLadder|nav.NodeWater|nav.NodeMover) != 0 {
			continue
		}
		if dist3(nd.Origin, bel.Self.Origin) >= 256 {
			pick = append(pick, c.Node)
		}
	}
	b.takeNav()
	b.exploreAt = b.now
	if len(pick) == 0 {
		b.nav.ClearGoal()
		return
	}
	if err := b.nav.SetGoal(navrt.NodeGoal(pick[b.rng.Intn(len(pick))]), b.now); err != nil {
		b.nav.ClearGoal()
	}
}

// Cmd is the session.CmdFunc: the usercmd for the next msec of client c.
func (b *Bot) Cmd(c *fakeclient.Client, msec int) shared.UserCmd {
	ps := &c.Frame.PlayerState
	if !b.entered || b.drv == nil || c.LevelGen() != b.lv.Gen {
		return idleCmd(ps, msec)
	}
	switch ps.PMove.PmType {
	case q2const.PM_FREEZE:
		u := idleCmd(ps, msec)
		if b.freezeSince > 0 && b.now-b.freezeSince >= IntermissionPress {
			u.Buttons |= q2const.BUTTON_ANY
			b.pressed = true
		}
		return u
	case q2const.PM_DEAD, q2const.PM_GIB:
		return idleCmd(ps, msec)
	}
	b.fire = false
	u := b.drv.Cmd(c, msec)
	if b.fire {
		u.Buttons |= q2const.BUTTON_ATTACK
	}
	if u.Buttons&q2const.BUTTON_ATTACK != 0 {
		b.fired++
	}
	return u
}

// idleCmd holds still with the current view.
func idleCmd(ps *shared.PlayerState, msec int) shared.UserCmd {
	yaw, pitch := ps.ViewAngles[q2const.YAW], ps.ViewAngles[q2const.PITCH]
	return control.Compose(control.Idle(yaw, pitch), yaw, pitch, ps.PMove.DeltaAngles, msec)
}

// aim is the driver's Aim: where each command looks. A view the movement
// depends on (MustFace) wins; then the fight target (slewed, with the
// trigger once aligned); then a facing the route step needs; then a point
// it wants watched while the bot stands; else the path heading.
func (b *Bot) aim(in control.MoveIntent, st *navsim.State) (float32, float32) {
	if in.MustFace {
		b.shoot.SetView(in.FaceYaw, in.FacePitch)
		return in.FaceYaw, in.FacePitch
	}
	eye := Vec3{st.Origin()[0], st.Origin()[1], st.Origin()[2] + st.ViewHeight}
	view := b.world.Belief().Self.ViewAngles
	if tr := b.liveTrack(b.world.Belief(), b.target); tr != nil {
		k := decide.WeaponFromPickup(b.world.Belief().Self.Weapon)
		p, r := aimFor(eye, tr, k)
		fire := b.firePolicy != decide.FireHold && tr.Visible && tr.Shootable
		y, pt, ok := b.shoot.Aim(eye, view[q2const.YAW], view[q2const.PITCH], AimTarget{Point: p, Radius: r, Fire: fire}, navrt.CmdMsec)
		b.fire = ok
		return y, pt
	}
	if b.dir.MustFace {
		b.shoot.SetView(b.dir.FaceYaw, 0)
		return b.dir.FaceYaw, 0
	}
	if b.dir.HasLook && b.standing() {
		y, p, _ := b.shoot.Aim(eye, view[q2const.YAW], view[q2const.PITCH], AimTarget{Point: b.dir.Look}, navrt.CmdMsec)
		return y, p
	}
	b.shoot.SetView(in.FaceYaw, in.FacePitch)
	return in.FaceYaw, in.FacePitch
}

// standing reports whether the bot is not walking a path (waiting,
// arrived, holding): it may look around.
func (b *Bot) standing() bool {
	if b.dir.Hold {
		return true
	}
	switch b.nav.Status().Follow {
	case navrt.Idle, navrt.Arrived, navrt.Waiting:
		return true
	}
	return false
}

// World returns the bot's world model.
func (b *Bot) World() *worldmodel.World { return b.world }

// Belief returns the live belief (see worldmodel.World.Belief).
func (b *Bot) Belief() *worldmodel.Belief { return b.world.Belief() }

// Navigator returns the level's navigator (nil before Enter).
func (b *Bot) Navigator() *navrt.Navigator { return b.nav }

// Route returns the level's route executor (nil without a route).
func (b *Bot) Route() *routeexec.Executor { return b.exec }

// Level returns the level entered last.
func (b *Bot) Level() Level { return b.lv }

// Intent returns the policy's last intent.
func (b *Bot) Intent() decide.Intent { return b.intent }

// Mode returns what the bot did in the last frame.
func (b *Bot) Mode() Mode { return b.mode }

// Target returns the track fought in the last frame ("" if none).
func (b *Bot) Target() string { return b.target }

// Stats counts what the bot did.
type Stats struct {
	Frames       int // frames folded on the current level
	FireCmds     int // commands with the trigger held
	SideCommands int // side command pairs sent
	Switches     int // "use" commands sent
	Explores     int // explore bursts started
	Pressed      bool
}

// Stats returns the counters.
func (b *Bot) Stats() Stats {
	return Stats{Frames: b.frames, FireCmds: b.fired, SideCommands: b.sideSent, Switches: b.shoot.Switches(), Explores: b.explores, Pressed: b.pressed}
}

// Describe returns a diagnostic dump: the step, the bot's position and
// state, the navigator's status and a summary of the belief.
func (b *Bot) Describe() string {
	if !b.entered {
		return "bot: no level entered"
	}
	bel := b.world.Belief()
	s := &bel.Self
	out := fmt.Sprintf("level %s visit %d gen %d, mode %s, frames %d\n", b.lv.Key.Map, b.lv.Key.Visit, b.lv.Gen, b.mode, b.frames)
	if b.exec != nil {
		cur := b.exec.Current()
		out += fmt.Sprintf("step %d/%d %s: %s, attempt %d, started %.1fs ago (%s)\n", b.exec.Index(), len(b.exec.Steps()), cur.Op, cur.Status, cur.Attempts,
			float64(b.now-cur.Started)/1000, cur.Reason)
		out += fmt.Sprintf("  avoid %v\n", b.exec.Avoid())
	}
	out += fmt.Sprintf("self origin %v vel %v view %v health %d armor %d weapon %q ammo %d pm %d ground %v dead %v\n",
		s.Origin, s.Velocity, s.ViewAngles, s.Health, s.Armor, s.Weapon, s.Ammo, s.PmType, s.OnGround, s.Dead)
	if b.nav != nil {
		st := b.nav.Status()
		g, has := b.nav.Goal()
		out += fmt.Sprintf("nav %s cause %s %q (last %s %q) node %d edge %d step %d remaining %.1fs repaths %d stucks %d goal %v (%v)\n",
			st.Follow, st.Cause, st.Reason, st.LastCause, st.LastReason, st.Node, st.Edge, st.Step, st.Remaining, st.Repaths, st.Stucks, g, has)
	}
	out += fmt.Sprintf("belief: frames %d tracks %d items %d movers %d lasers %d inventory known %v help %v\n",
		bel.Frames, len(bel.Tracks), len(bel.Items), len(bel.Movers), len(bel.Lasers), bel.Inventory.Known, bel.HelpKnown)
	for i := range bel.Tracks {
		t := &bel.Tracks[i]
		if t.Kind != perception.KindMonster.String() || dist3(t.Pos, s.Origin) > 1500 {
			continue
		}
		out += fmt.Sprintf("  track %s %s lump %d at %v %s vis %v shoot %v aware %s\n", t.ID, t.Class, t.Lump, t.Pos, t.Life, t.Visible, t.Shootable, t.Awareness)
	}
	for i := range bel.Lasers {
		l := &bel.Lasers[i]
		out += fmt.Sprintf("  laser #%d %s (updated %d)\n", l.Lump, l.State, l.LastUpdate)
	}
	for i := range bel.Messages {
		out += fmt.Sprintf("  msg %q\n", bel.Messages[i].Text)
	}
	return out
}
