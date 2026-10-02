// Package bot is the agent's per-tick loop: it folds every new server
// frame into the world model (fair perception only), asks its Policy for
// an Intent with the route's objective, runs the route executor (package
// routeexec) or the mode the policy chose, and turns the navigator's
// movement, the combat layer's own movement and reflexes and the aim and
// fire gate into the session's usercmds (four 25 ms commands per server
// frame). It also sends the side commands the world model asks for (the
// inventory and the help computer, at most one pair every 2 s and never
// in a fight) and leaves an intermission by pressing a button after 5.5 s
// of PM_FREEZE.
//
// The modes:
//
//   - objective: the route executor's current step (a route kill step
//     makes its monster the target once seen: the bot fights it);
//   - fight: engage the intent's target (or the route's kill): aim and fire
//     with the fire gate, and move relative to it as the intent says
//     (advance and retreat through the navigator, strafes and holds by the
//     controller where the way is safe: floor, avoided entities, hazards);
//   - retreat: back off from the main threat, to a health item when one is
//     known nearby, else to a spot it cannot see, else along the trail;
//   - pickup: the navigator to the intent's item, then back to the route;
//   - explore: frontier regions of the nav graph (the campaign's watchdog,
//     or the intent without a route).
//
// Reflexes always win, every command: the fire gate (control.FireGate),
// the dodge of an incoming projectile, the escape from a grenade. Between
// the modes run the watchdogs of the bot's own: a pickup (or a heal in a
// retreat) that makes no progress is given up for a while, a fight that
// lasts too long is broken off, and down a pit the route's objective
// cannot be reached from the bot types "kill" (the campaign reloads).
//
// A Policy that is a *decide.Pipeline (NewBrain) gets the level's static
// probes at every entry (SetProbes: the space around the bot, path
// distances over the nav graph). With Config.OnDecision set the bot emits
// one trace decision event per decision tick (lane tick, see
// trace.LaneTick): the fast lane state's digest, the Intent with the
// provenance of every field, what the bot executed and the usercmds sent
// since the previous tick.
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
	"quake2web/server/internal/agent/trace"
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
	// maxPendingCmds bounds the usercmds kept for the next tick event (a
	// tick comes every frame the bot is alive: 4 commands); the ones over
	// it are counted (trace.Tick.DroppedCmds).
	maxPendingCmds = 256
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
	// Policy decides the intent each frame (nil: ObjectivePolicy). A
	// *decide.Pipeline (NewBrain) is the decision layer.
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

	// OnDecision, when set, receives the bot's decision trace events: one
	// lane tick event per decision tick, preceded (TraceRequests) by the
	// lane fast and slow events of the requests the Policy collected that
	// tick. The event is the caller's to keep.
	OnDecision func(d *trace.Decision)
	// TraceRequests also emits the request events of a Policy that reports
	// them (decide.Pipeline). Leave it off when the pipeline's OnRecord
	// publishes them already: a request must be traced once.
	TraceRequests bool
	// TraceState puts the full fast lane state into every tick event and
	// the state and questions into the request events (default: digests
	// only).
	TraceState bool
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
	ModeRetreat      Mode = "retreat"
	ModeExplore      Mode = "explore"
	ModeDead         Mode = "dead"
	ModeIntermission Mode = "intermission"
	ModeDone         Mode = "done" // the route is done (waiting for the level to end)
)

// Bot is the per-tick loop (see the package documentation).
type Bot struct {
	cfg     Config
	classes *perception.ClassTable
	world   *worldmodel.World
	reader  *perception.Reader
	policy  Policy
	shoot   Shooter
	rng     *rand.Rand

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
	cmdSub     int  // commands built since the last frame folded

	// overrides of the intent this tick, for the trace: why the mode, the
	// target, the fire policy, the movement and the weapon acted on are not
	// the intent's ("" when they are; see trace.Intent)
	modeBy, targetBy, fireBy, moveBy, weaponBy string
	weaponTo                                   decide.WeaponKey // the weapon acted on
	// routeKill is the track of the route's kill step once the executor
	// named it; killTarget: the target this tick is that monster (the
	// route's), killFight: the bot fights it (the step is being done)
	routeKill             string
	killTarget, killFight bool
	fight                 fight
	// search: the bot looks towards searchYaw until searchUntil (a hit it
	// did not see coming, at searchAt); scans counts the scans without a
	// suspect (they turn behind, then to the sides)
	searchAt, searchUntil int64
	searchYaw             float32
	scans                 int

	// quad: a quad damage picked up (at quadAt) and not used yet
	quadHeld bool
	quadAt   int64

	freezeSince int64
	pressed     bool
	sideAt      int64
	sideSent    int
	switchCmd   string // the "use" sent this tick

	exploreUntil int64
	exploreAt    int64
	explores     int
	regions      map[int32]bool // nav regions visited on the level

	// the pickup under way (its progress) and the spots given up on (kept
	// per level visit across reloads: static learning)
	pickID   string
	pickBest float32
	pickAt   int64
	badItems []badSpot
	badByKey map[worldmodel.LevelKey][]badSpot

	ticks   int // decision ticks on the level attempt
	pending []trace.UserCmd
	dropped int // commands over maxPendingCmds since the last tick event

	// bel is the belief the frame was decided on (the commands of the
	// frame aim and move by it; nil before the level's first frame: the
	// world model's)
	bel *worldmodel.Belief
	// navVeto: the fire gate held the trigger the navigator pulled (a
	// shoot goal) in the command being built
	navVeto bool
	// testVision, when set, stands in for the percept's line of fire
	// (tests)
	testVision *perception.Vision

	// trapped: since when the bot stands where the route's objective is
	// out of reach for good (a pit: trapTick), when it last sent "kill",
	// how many times
	trappedSince, killAt int64
	kills                int
	// wedged: where and since when the bot has stood (wedgeTick), the
	// frames since then, how many of them its navigator was under way,
	// the recoveries (stuck reports, repaths) it made meanwhile and its
	// last count
	wedgeAt                               Vec3
	wedgeSince                            int64
	wedgeFrames, wedgeNav, wedgeRecovered int
	wedgeOff                              int // frames off the nav graph
	navRecoveries                         int
	// stalled: when the level attempt started (its first decision), the
	// monsters the bot fought on it (dead: seen killed) and when it last
	// saw one of them killed (stallTick)
	attemptAt, foughtKillAt int64
	fought                  []foughtTrack
	// testNav, when set, stands in for the navigator's status (tests)
	testNav func() navEffort
}

// New returns a bot with no level: call Enter once the client is active on
// one.
func New(cfg Config) *Bot {
	b := &Bot{cfg: cfg, classes: cfg.Classes, reader: perception.NewReader(), policy: cfg.Policy, mode: ModeNone, sideAt: -SideInterval}
	if b.policy == nil {
		b.policy = ObjectivePolicy{}
	}
	if b.classes == nil {
		b.classes = perception.NewClassTable()
	}
	b.world = worldmodel.New(worldmodel.Config{ReadFile: cfg.ReadFile, Perception: cfg.Perception, Classes: cfg.Classes, Anims: cfg.Anims})
	b.rng = rand.New(rand.NewSource(cfg.Seed))
	b.fight.strafe.Seed = uint64(cfg.Seed)
	return b
}

// ErrNoGraph is returned by Enter without a graph or map data.
var ErrNoGraph = errors.New("bot: a level needs its map data and nav graph")

// Enter starts a level: the world model resets to it (a key entered before
// restores its level memory), and the bot gets a new navigator, driver and
// route executor. Frames of other level generations are ignored from now
// on. A Policy that takes level probes (decide.Pipeline) gets the level's.
func (b *Bot) Enter(lv Level) error {
	if lv.Map == nil || lv.Graph == nil {
		return ErrNoGraph
	}
	if b.badByKey == nil {
		b.badByKey = map[worldmodel.LevelKey][]badSpot{}
	}
	if b.entered {
		b.badByKey[b.lv.Key] = b.badItems
	}
	b.world.Reset(worldmodel.Level{Key: lv.Key, Map: lv.Map})
	b.seedItems(lv.Map)
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
	if p, ok := b.policy.(levelProber); ok {
		var space decide.SpaceProbe
		if lv.Map.CM != nil {
			space = decide.NewTraceSpace(lv.Map.CM, 256)
		}
		p.SetProbes(space, b.itemPath)
	}
	b.lv, b.entered = lv, true
	b.nav, b.exec = n, x
	b.drv = navrt.NewDriver(n)
	b.drv.Aim = b.aim
	b.drv.Move = b.move
	b.started = false
	b.frames = 0
	b.intent = decide.Intent{}
	b.dir = routeexec.Directive{}
	b.mode = ModeObjective
	b.navMine = false
	b.target, b.fire, b.cmdSub = "", false, 0
	b.modeBy, b.targetBy, b.fireBy, b.moveBy, b.weaponBy, b.weaponTo = "", "", "", "", "", decide.WeaponKeep
	b.routeKill, b.killTarget, b.killFight = "", false, false
	b.bel, b.navVeto = nil, false
	b.fight.reset()
	b.searchAt, b.searchUntil, b.scans = 0, 0, 0
	b.quadHeld, b.quadAt = false, b.now // a pickup message from before the entry does not count
	b.freezeSince, b.pressed = 0, false
	b.exploreUntil, b.exploreAt = 0, 0
	b.regions = map[int32]bool{}
	b.pickID, b.badItems = "", b.badByKey[lv.Key]
	b.ticks, b.pending, b.dropped = 0, nil, 0
	b.trappedSince, b.killAt = 0, 0
	b.wedgeSince, b.navRecoveries = 0, 0
	b.attemptAt, b.foughtKillAt, b.fought = 0, 0, b.fought[:0]
	b.shoot.Reset()
	b.shoot.ForgetSwitch()
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
	b.cmdSub = 0
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
	b.bel = bel
	b.target, b.switchCmd = "", ""
	switch {
	case s.PmType == q2const.PM_FREEZE:
		if b.freezeSince == 0 {
			b.freezeSince = b.now
		}
		b.mode = ModeIntermission
		b.wedgeSince = 0
		return
	case s.Dead:
		b.freezeSince = 0
		b.mode = ModeDead
		b.wedgeSince = 0
		return
	}
	b.freezeSince, b.pressed = 0, false
	b.trailUpdate(s.Origin)
	b.noteRegion(s.Origin)
	b.reflexTick(bel)
	b.trapTick(c, bel)
	b.wedgeTick(c, bel)
	b.stallTick(c, bel)

	var obj *decide.ObjectiveView
	if b.exec != nil {
		obj = b.exec.Objective()
	}
	b.intent = b.policy.Tick(b.now, bel, obj)
	b.firePolicy = b.intent.FirePolicy
	b.modeBy, b.targetBy, b.fireBy, b.moveBy = "", "", "", ""
	b.killTarget, b.killFight = false, false

	mode := ModeObjective
	var foe, threat, re *worldmodel.Track
	rk := b.routeKillTrack(bel)
	gone := b.liveTrack(bel, b.intent.Target) == nil
	switch {
	case b.intent.Mode == decide.ModeRetreat && b.threatOrNil(bel, &threat):
		mode = ModeRetreat
	case b.intent.Mode == decide.ModeFight && !gone && !b.disengaged(b.intent.Target):
		mode, foe = ModeFight, b.liveTrack(bel, b.intent.Target)
	case b.intent.Mode == decide.ModeFight && gone && b.retargetOrNil(bel, false, &re):
		// the fight's target is dead or not known yet: fight the most
		// dangerous awake monster in view until the decision layer names
		// one (a latency away)
		mode, foe = ModeFight, re
	case rk != nil:
		mode, foe = ModeFight, rk
		b.killTarget, b.killFight = true, true
	case b.exploreUntil > 0:
		mode = ModeExplore
	case b.intent.Mode == decide.ModePickup && b.pickupItem(bel, b.intent.Pickup) != nil:
		mode = ModePickup
	case b.intent.Mode == decide.ModeExplore && b.exec == nil:
		mode = ModeExplore
	}
	b.modeBy = b.modeOverride(bel, mode)
	if mode != ModeObjective {
		b.yieldRoute()
	}
	if mode != ModeFight && mode != ModeRetreat && b.fight.goal != goalNone {
		b.fight.goal = goalNone
	}
	b.dir = routeexec.Directive{Hold: true}
	b.mode = mode
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
			b.mode = ModeDone
		}
		if k := b.dir.Kill; k != nil && k.Track != "" {
			// the route's kill: fight it from here on once seen (the
			// executor brought the bot to a firing position)
			b.routeKill = k.Track
			b.target = k.Track
			b.killTarget = true
		}
	case ModeFight:
		b.target = foe.ID
		if foe == re {
			b.retargeted()
		}
		b.fightTick(bel, foe)
		b.fightClock(foe.ID)
	case ModeRetreat:
		b.retreatTick(bel, threat)
	case ModePickup:
		b.pickup(bel)
	case ModeExplore:
		b.exploreTick(bel)
	}
	if b.killTarget {
		if b.target != b.intent.Target {
			b.targetBy = "route_kill"
		}
		if b.firePolicy == decide.FireHold || b.firePolicy == "" {
			b.firePolicy, b.fireBy = decide.FireWhenAligned, "route_kill"
		}
	}
	if b.target == "" && b.mode != ModeFight && b.firePolicy != decide.FireHold && b.firePolicy != "" {
		// on the move (the route, a pickup, a retreat): shoot back at the
		// intent's target while it is in view with a line of fire
		if t := b.liveTrack(bel, b.intent.Target); t != nil && t.Visible && t.Shootable {
			b.target = t.ID
		}
	}
	if b.target == "" && b.mode != ModeFight && gone && b.retargetOrNil(bel, true, &re) {
		// on the move without a live target: shoot back at a monster in
		// view attacking the bot
		b.target = re.ID
		b.retargeted()
	}
	if b.target == "" && b.targetBy == "" && b.intent.Target != "" && b.liveTrack(bel, b.intent.Target) == nil {
		b.targetBy = "target_gone" // dead, or a track the bot does not know
	}
	b.searchTick(bel)
	b.noteFought(b.target)

	b.weaponTick(c, bel)
	b.powerupTick(c, bel)
	b.sideCommands(c, bel)
	b.traceTick(bel)
}

// quadName is quad damage's pickup name (C: game/g_items.c itemlist).
const quadName = "Quad Damage"

// powerupTick uses a quad damage the bot picked up once it fights a
// monster in view that is awake (reflex quad): in single player the game
// only stores a picked-up quad (Pickup_Powerup), the player types "use
// Quad Damage" when it pays, and it lasts 30 s from then.
func (b *Bot) powerupTick(c *fakeclient.Client, bel *worldmodel.Belief) {
	if s := &bel.Self; s.Pickup == quadName && s.PickupAt > b.quadAt {
		b.quadHeld, b.quadAt = true, s.PickupAt
	}
	if !b.quadHeld || b.mode != ModeFight {
		return
	}
	if tr := b.liveTrack(bel, b.target); tr == nil || !tr.Visible || tr.Awareness == worldmodel.Idle {
		return
	}
	c.StringCmd("use " + quadName)
	b.quadHeld = false
	b.fight.noteReflex("quad")
}

// retargetOrNil sets *t to the retarget reflex's pick and reports whether
// there is one (see retarget).
func (b *Bot) retargetOrNil(bel *worldmodel.Belief, attacking bool, t **worldmodel.Track) bool {
	*t = b.retarget(bel, attacking)
	return *t != nil
}

// retargeted marks the target as the retarget reflex's: the target and,
// when the intent holds fire, the fire policy (fire when aligned).
func (b *Bot) retargeted() {
	b.targetBy = "retarget"
	if b.firePolicy == decide.FireHold || b.firePolicy == "" {
		b.firePolicy, b.fireBy = decide.FireWhenAligned, "retarget"
	}
}

// actedMode is mode m in the decision layer's vocabulary (done waits on
// the objective).
func actedMode(m Mode) decide.Mode {
	if m == ModeDone {
		return decide.ModeObjective
	}
	return decide.Mode(m)
}

// modeOverride returns why the bot executes mode instead of the intent's
// ("" when it does not): the route's kill (route_kill), the campaign's
// explore burst (explore_watchdog), a retreat with no threat known
// (no_threat), a fight on a target gone (target_gone) or given up on
// (disengaged), a pickup of an item not there or given up on
// (item_unavailable), an explore while the route has the bot (route).
func (b *Bot) modeOverride(bel *worldmodel.Belief, mode Mode) string {
	want := b.intent.Mode
	if want == "" {
		want = decide.ModeObjective
	}
	switch {
	case actedMode(mode) == want:
		return ""
	case b.killFight:
		return "route_kill"
	case mode == ModeExplore && b.exploreUntil > 0:
		return "explore_watchdog"
	}
	switch want {
	case decide.ModeRetreat:
		return "no_threat"
	case decide.ModeFight:
		if b.liveTrack(bel, b.intent.Target) == nil {
			return "target_gone"
		}
		return "disengaged"
	case decide.ModePickup:
		return "item_unavailable"
	case decide.ModeExplore:
		return "route"
	}
	return "unknown_mode"
}

// yieldRoute takes the navigator from the route executor for the frame:
// a fight with the route's kill is the step's own work (Engage: its clock
// runs on), anything else is not (Yield: its clock stands).
func (b *Bot) yieldRoute() {
	switch {
	case b.exec == nil:
	case b.killFight:
		b.exec.Engage(b.now)
	default:
		b.exec.Yield()
	}
}

// weaponTick switches to the weapon the intent asks for (the decision
// layer chooses weapons), unless a reflex overrides it: the weapon in hand
// is dry (dry), or a splash weapon would fire at a target or a route
// shoot goal too close (splash; the best usable weapon for the range
// then, Shooter.Choose), or the intent's weapon cannot be used (unusable:
// not held or without ammo) or is a splash weapon at such a target
// (splash): the bot keeps its weapon. Without any decision about the
// weapon (no answer, or a policy that does not choose one) the bot fights
// with the best weapon for the range. weaponTo is the weapon acted on.
func (b *Bot) weaponTick(c *fakeclient.Client, bel *worldmodel.Belief) {
	s := &bel.Self
	b.shoot.Observe(bel)
	cur := decide.WeaponFromPickup(s.Weapon)
	want := b.intent.Weapon
	tr := b.liveTrack(bel, b.target)
	d := float32(-1)
	if tr != nil {
		d = dist3(s.Eye, tr.Loc)
	} else if p, ok := b.shootGoal(); ok {
		d = dist3(s.Eye, p)
	}
	splashClose := func(k decide.WeaponKey) bool {
		w, _ := control.WeaponByPickup(k.Pickup())
		return d >= 0 && w.HasSplash() && d < 1.5*control.SplashSafe
	}
	b.weaponBy = ""
	switch {
	case cur != "" && !usable(bel, cur):
		want, b.weaponBy = b.shoot.Choose(b.now, bel, max(d, 0), decide.WeaponKeep), "dry"
	case splashClose(cur):
		want, b.weaponBy = b.shoot.Choose(b.now, bel, d, decide.WeaponKeep), "splash"
	case want != decide.WeaponKeep && !usable(bel, want):
		want, b.weaponBy = decide.WeaponKeep, "unusable"
	case want != decide.WeaponKeep && splashClose(want):
		want, b.weaponBy = decide.WeaponKeep, "splash"
	case want == decide.WeaponKeep && tr != nil && b.intent.Provenance.Weapon.Source == decide.SourceDefault:
		want = b.shoot.Choose(b.now, bel, d, decide.WeaponKeep)
	}
	b.weaponTo = want
	if want == decide.WeaponKeep || want == cur {
		return
	}
	if cmd := b.shoot.Switch(b.now, bel, want); cmd != "" {
		c.StringCmd(cmd)
		b.switchCmd = cmd
	}
}

// fightClock times the fight with track id: past fightBudget the bot
// disengages from it (disengaged) for disengageFor.
func (b *Bot) fightClock(id string) {
	f := &b.fight
	if f.foe != id || b.now-f.foeLast > 3000 {
		f.foe, f.foeSince = id, b.now
	}
	f.foeLast = b.now
	if b.now-f.foeSince > fightBudget {
		f.offFoe, f.offUntil = id, b.now+disengageFor
		f.foe = ""
		f.noteReflex("disengage")
	}
}

// disengaged reports a target the bot gave up fighting for now.
func (b *Bot) disengaged(id string) bool {
	return id != "" && id == b.fight.offFoe && b.now < b.fight.offUntil
}

// threatOrNil sets *t to the main threat and reports whether there is one.
func (b *Bot) threatOrNil(bel *worldmodel.Belief, t **worldmodel.Track) bool {
	*t = b.threat(bel)
	return *t != nil
}

// routeKillTrack returns the route kill step's monster while it is the
// step's (the executor named it) and alive, seen within routeKillMemory
// and within the executor's firing range; nil otherwise (the executor
// moves the bot to a firing position).
func (b *Bot) routeKillTrack(bel *worldmodel.Belief) *worldmodel.Track {
	if b.routeKill == "" {
		return nil
	}
	if b.exec == nil || b.exec.Done() || b.exec.Current().Op != route.OpKill {
		b.routeKill = ""
		return nil
	}
	t := b.liveTrack(bel, b.routeKill)
	if t == nil {
		b.routeKill = ""
		return nil
	}
	fireRange := b.cfg.Route.FireRange
	if fireRange <= 0 {
		fireRange = routeexec.DefaultFireRange
	}
	seen := t.Visible || t.LastSeen > 0 && bel.Time-t.LastSeen <= routeKillMemory
	if !seen || dist3(bel.Self.Eye, t.Pos) > fireRange*1.25 {
		return nil
	}
	return t
}

// takeNav makes the navigator the bot's (the route executor sets its goal
// again when the bot is back on the objective).
func (b *Bot) takeNav() {
	b.yieldRoute()
	b.navMine = true
}

// shootGoal returns the point of the route's shoot goal while the route
// executor has the navigator on one.
func (b *Bot) shootGoal() (Vec3, bool) {
	if b.nav == nil || b.navMine || b.mode != ModeObjective {
		return Vec3{}, false
	}
	if g, ok := b.nav.Goal(); ok && g.Kind == navrt.GoalShoot {
		return g.Point, true
	}
	return Vec3{}, false
}

// liveTrack returns the track id names while it is alive and located (seen,
// or heard with a direction: Loc), else nil.
func (b *Bot) liveTrack(bel *worldmodel.Belief, id string) *worldmodel.Track {
	if id == "" {
		return nil
	}
	if t := bel.Track(id); t != nil && t.Life == worldmodel.LifeAlive && t.LocKnown {
		return t
	}
	return nil
}

// pickupItem returns the item id names when it is there to pick up: alive
// and not given up on (pickupWatch).
func (b *Bot) pickupItem(bel *worldmodel.Belief, id string) *worldmodel.Item {
	if id == "" {
		return nil
	}
	for i := range bel.Items {
		if it := &bel.Items[i]; it.ID == id && it.Life == worldmodel.LifeAlive {
			if b.badItem(it.Pos) {
				return nil
			}
			return it
		}
	}
	return nil
}

// Pickup watchdog (ms, units).
const (
	// pickupStall is how long a pickup may go without getting pickupGain
	// closer to the item.
	pickupStall = 4000
	pickupGain  = 24
	// pickupStay is how long the bot may stand at the item (within
	// pickupReach) without taking it (it does not need it, or it lies out
	// of reach).
	pickupStay  = 1500
	pickupReach = 40
	// badItemFor is how long an item given up on stays out of the
	// decision layer's lists and the pickups.
	badItemFor = 60000
)

// badSpot is an item spot the bot gave up on, until when.
type badSpot struct {
	pos   Vec3
	until int64
}

// badItem reports an item spot given up on (until its time is over).
func (b *Bot) badItem(p Vec3) bool {
	for _, s := range b.badItems {
		if b.now < s.until && dist3(s.pos, p) < 8 {
			return true
		}
	}
	return false
}

// itemPath is the decision layer's PathFunc: the navigator's path
// distance; none to an item spot given up on, or one the bot could not
// come back from (down a one-way drop: navrt.Navigator.CanReturn), so the
// policy does not choose it.
func (b *Bot) itemPath(from, to Vec3) (float32, bool) {
	if b.badItem(to) || !b.nav.CanReturn(from, to) {
		return 0, false
	}
	return b.nav.PathDistance(from, to)
}

// giveUpItem runs the watchdog of the item the bot is heading for (a
// pickup, or health in a retreat) from o: it gives up on the item (for
// badItemFor) when the bot gets no closer, the navigator fails, or the bot
// stands at it without taking it (an item the belief still remembers
// there that is gone, or one it does not need). It reports a give-up.
func (b *Bot) giveUpItem(it *worldmodel.Item, o Vec3) bool {
	d := dist3(o, it.Pos)
	if it.ID != b.pickID {
		b.pickID, b.pickBest, b.pickAt = it.ID, d, b.now
	}
	if d < b.pickBest-pickupGain {
		b.pickBest, b.pickAt = d, b.now
	}
	if b.now-b.pickAt > pickupStall || d < pickupReach && b.now-b.pickAt > pickupStay || b.navFailed() && b.now-b.pickAt > 500 {
		b.badItems = append(b.badItems, badSpot{pos: it.Pos, until: b.now + badItemFor})
		b.pickID = ""
		return true
	}
	return false
}

// pickup heads for the item the policy chose, and gives up on it (for
// badItemFor) when the bot gets no closer, cannot get there, or stands at
// it without taking it.
func (b *Bot) pickup(bel *worldmodel.Belief) {
	it := b.pickupItem(bel, b.intent.Pickup)
	if it == nil {
		return
	}
	if b.giveUpItem(it, bel.Self.Origin) {
		b.nav.ClearGoal()
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
// for: one pair at most every SideInterval, never in a fight (except the
// first inventory of a level).
func (b *Bot) sideCommands(c *fakeclient.Client, bel *worldmodel.Belief) {
	if b.now-b.sideAt < SideInterval {
		return
	}
	// the inventory is learned again at every level entry: without it the
	// bot does not know its weapons, so the first one goes out even in a
	// fight
	if (bel.Self.InCombat || b.target != "") && bel.Inventory.Known {
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

// Trap recovery (ms, node share).
const (
	// trapShare: the bot is in a pit when the nav graph's nodes its spot
	// reaches (any edge, conditions ignored) are fewer than one in
	// trapShare of the level's and the route's objective is not among
	// them.
	trapShare = 20
	// trapFor is how long the bot stays in a pit before it gives up the
	// attempt ("kill", which the campaign answers like any death: the
	// level-entry save is loaded); trapRetry spaces the commands (the
	// game refuses a kill within 5 s of a spawn).
	trapFor   = 12000
	trapRetry = 6000
)

// trapTick gives up a level attempt the bot cannot finish from where it
// stands: down a one-way drop into a pit (a knockback off a ledge, a
// missed jump) with the route's objective out of reach. A player stuck
// there types "kill" or loads the last save; the bot types "kill", and
// the death and reload are counted as any other.
func (b *Bot) trapTick(c *fakeclient.Client, bel *worldmodel.Belief) {
	if b.exec == nil || b.exec.Done() {
		b.trappedSince = 0
		return
	}
	o := bel.Self.Origin
	reach, total := b.nav.ReachSize(o)
	trapped := reach*trapShare < total
	if trapped {
		if p, ok := b.exec.Target(); ok && b.nav.CanReturn(p, o) {
			trapped = false // the objective is down here too
		}
	}
	if !trapped {
		b.trappedSince = 0
		return
	}
	if b.trappedSince == 0 {
		b.trappedSince = b.now
	}
	if b.now-b.trappedSince < trapFor || b.now-b.killAt < trapRetry && b.killAt > 0 {
		return
	}
	b.killAt = b.now
	b.kills++
	b.fight.noteReflex("trapped_kill")
	if b.cfg.Logf != nil {
		b.cfg.Logf("bot: trapped at %v (%d of %d nav nodes reachable) for %.0fs: kill", o, reach, total, float64(b.now-b.trappedSince)/1000)
	}
	c.StringCmd("kill")
}

// Wedge recovery (units, ms, share, recoveries).
const (
	// wedgeMove: the bot is wedged while its origin stays within
	// wedgeMove of a spot for wedgeFor, its navigator under way (a path to
	// follow, or stuck recovery) on at least wedgeShare of the frames and
	// recovering (a stuck report or a repath) at least wedgeRecover times
	// meanwhile: it tries to move, its manoeuvres fail, and it does not
	// move. Standing still on purpose (waiting for a mover, holding in a
	// fight, a route step with no path in the believed state) is not; a
	// bot that frees itself within wedgeFor (two minutes: some spots take
	// a while) never trips it, and the campaign's stall watchdog (five
	// minutes, fatal to the run) comes later.
	wedgeMove    = 24
	wedgeFor     = 120000
	wedgeShare   = 0.75
	wedgeRecover = 8
	// offGraphFor: off the nav graph (no node within reach: wedged in
	// the geometry, say on a ledge's edge), its navigator with a goal on
	// at least wedgeShare of the frames, the bot that has not moved for
	// offGraphFor is wedged too: the navigator cannot plan a way out.
	offGraphFor = 60000
)

// navEffort is what the navigator does for the bot this frame: under way
// (it has a goal and follows a path to it or recovers from being stuck),
// and its recovery count for the goal (stuck reports plus repaths).
type navEffort struct {
	underWay   bool
	recoveries int
	// offGraph: under way, but no node within reach of the bot
	offGraph bool
}

// navTrying returns the navigator's effort.
func (b *Bot) navTrying() navEffort {
	if b.testNav != nil {
		return b.testNav()
	}
	if b.nav == nil {
		return navEffort{}
	}
	st := b.nav.Status()
	e := navEffort{recoveries: st.Stucks + st.Repaths}
	if _, has := b.nav.Goal(); !has {
		return e
	}
	switch st.Follow {
	case navrt.Following, navrt.Stuck, navrt.OffGraph:
		e.underWay = true
	}
	e.offGraph = st.Follow == navrt.OffGraph
	return e
}

// wedgeTick gives up a level attempt the bot cannot move on from: it has
// stood on one spot for wedgeFor while its navigator kept trying to move
// it, its stuck recovery (jump, strafe, back off, repath) included: say,
// wedged on a monster below its view; or for offGraphFor off the nav
// graph with a goal (wedged in the geometry, where the navigator has no
// node to plan from). Like a player stuck in a pit
// (trapTick), the bot types "kill"; the campaign counts the death and
// loads the level-entry save.
func (b *Bot) wedgeTick(c *fakeclient.Client, bel *worldmodel.Belief) {
	o := bel.Self.Origin
	eff := b.navTrying()
	if eff.recoveries < b.navRecoveries {
		b.navRecoveries = 0 // a new goal counts from 0
	}
	recovered := eff.recoveries - b.navRecoveries
	b.navRecoveries = eff.recoveries
	if b.wedgeSince == 0 || dist3(o, b.wedgeAt) > wedgeMove {
		b.wedgeAt, b.wedgeSince, b.wedgeFrames, b.wedgeNav, b.wedgeRecovered, b.wedgeOff = o, b.now, 0, 0, 0, 0
		return
	}
	b.wedgeFrames++
	b.wedgeRecovered += recovered
	if eff.underWay {
		b.wedgeNav++
	}
	if eff.offGraph {
		b.wedgeOff++
	}
	share := func(n int) bool { return float64(n) >= wedgeShare*float64(b.wedgeFrames) }
	recovering := b.now-b.wedgeSince >= wedgeFor && share(b.wedgeNav) && b.wedgeRecovered >= wedgeRecover
	offGraph := b.now-b.wedgeSince >= offGraphFor && share(b.wedgeOff)
	if !recovering && !offGraph || b.killAt > 0 && b.now-b.killAt < trapRetry {
		return
	}
	b.killAt = b.now
	b.kills++
	b.fight.noteReflex("wedged_kill")
	if b.cfg.Logf != nil {
		b.cfg.Logf("bot: wedged at %v for %.0fs (of %d frames the navigator was under way on %d, off the graph on %d, %d recoveries): kill", o,
			float64(b.now-b.wedgeSince)/1000, b.wedgeFrames, b.wedgeNav, b.wedgeOff, b.wedgeRecovered)
	}
	b.wedgeSince = 0
	c.StringCmd("kill")
}

// Stall recovery (ms).
const (
	// stallFor: a level attempt that made no progress for stallFor (no
	// route step done nor its path shortened, no monster the bot fought
	// killed) is given up. The campaign's no-progress watchdog (five
	// minutes by default) would end the run there; a fresh attempt may
	// not get stuck where this one did.
	stallFor = 250000
)

// foughtTrack is a monster the bot fought on the level attempt.
type foughtTrack struct {
	id   string
	dead bool
}

// noteFought records the track fought this frame.
func (b *Bot) noteFought(id string) {
	if id == "" {
		return
	}
	for i := range b.fought {
		if b.fought[i].id == id {
			return
		}
	}
	b.fought = append(b.fought, foughtTrack{id: id})
}

// stallTick gives up a level attempt that has made no progress for
// stallFor, as the campaign counts progress: a route step done (or its
// path left shortened), a monster the bot fought killed. Such an attempt
// is lost either way: the campaign's no-progress watchdog would end the
// run, while the level-entry save gives a fresh attempt that may not get
// stuck where this one did (a door shut behind the bot, a spot off the
// nav graph, a route monster that never comes). Like a player who
// cannot get on, the bot types "kill"; the campaign counts the death and
// reloads (trapTick).
func (b *Bot) stallTick(c *fakeclient.Client, bel *worldmodel.Belief) {
	for i := range b.fought {
		f := &b.fought[i]
		if f.dead {
			continue
		}
		if t := bel.Track(f.id); t != nil && t.Life != worldmodel.LifeAlive && t.Life != worldmodel.LifeDying {
			f.dead, b.foughtKillAt = true, b.now
		}
	}
	if b.attemptAt == 0 {
		b.attemptAt = b.now
	}
	if b.exec == nil || b.exec.Done() {
		return
	}
	since := max(b.attemptAt, b.exec.LastProgress(), b.foughtKillAt)
	if b.now-since < stallFor || b.killAt > 0 && b.now-b.killAt < trapRetry {
		return
	}
	b.killAt = b.now
	b.kills++
	b.fight.noteReflex("stalled_kill")
	if b.cfg.Logf != nil {
		b.cfg.Logf("bot: no progress for %.0fs at %v (step %d): kill", float64(b.now-since)/1000, bel.Self.Origin, b.exec.Index())
	}
	c.StringCmd("kill")
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

// noteRegion marks the nav region the bot stands in as visited.
func (b *Bot) noteRegion(o Vec3) {
	if b.lv.Graph == nil {
		return
	}
	if id := b.lv.Graph.Localize(o, 64); id != nav.NoNode {
		b.regions[b.lv.Graph.Nodes[id].Region] = true
	}
}

// exploreTick walks to the frontier: the nearest plain node, 256 to 1500
// units away, of a nav region the bot has not stood in; once there is none
// near, a random spot 256 to 1024 units away. Another spot is picked when
// the bot arrives, fails or takes too long. At the end of an explore burst
// the route's step starts over.
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
	o := bel.Self.Origin
	var frontier nav.NodeID = nav.NoNode
	var pick []nav.NodeID
	for _, c := range g.Nearby(o, 1500) {
		nd := &g.Nodes[c.Node]
		if nd.Flags&(nav.NodeCrouch|nav.NodeLadder|nav.NodeWater|nav.NodeMover) != 0 {
			continue
		}
		d := dist3(nd.Origin, o)
		if d < 256 || !b.nav.CanReturn(o, nd.Origin) {
			continue
		}
		if frontier == nav.NoNode && !b.regions[nd.Region] {
			frontier = c.Node
		}
		if d <= 1024 {
			pick = append(pick, c.Node)
		}
	}
	b.takeNav()
	b.exploreAt = b.now
	target := frontier
	if target == nav.NoNode {
		if len(pick) == 0 {
			b.nav.ClearGoal()
			return
		}
		target = pick[b.rng.Intn(len(pick))]
	}
	if err := b.nav.SetGoal(navrt.NodeGoal(target), b.now); err != nil {
		b.nav.ClearGoal()
		if target == frontier {
			b.regions[g.Nodes[target].Region] = true // unreachable: not a frontier
		}
	}
}

// Cmd is the session.CmdFunc: the usercmd for the next msec of client c.
func (b *Bot) Cmd(c *fakeclient.Client, msec int) shared.UserCmd {
	ps := &c.Frame.PlayerState
	if !b.entered || b.drv == nil || c.LevelGen() != b.lv.Gen {
		return idleCmd(ps, msec)
	}
	seq, ack := c.Netchan.OutgoingSequence, c.Netchan.IncomingAcknowledged
	var u shared.UserCmd
	switch ps.PMove.PmType {
	case q2const.PM_FREEZE:
		u = idleCmd(ps, msec)
		if b.freezeSince > 0 && b.now-b.freezeSince >= IntermissionPress {
			u.Buttons |= q2const.BUTTON_ANY
			b.pressed = true
		}
	case q2const.PM_DEAD, q2const.PM_GIB:
		u = idleCmd(ps, msec)
	default:
		b.fire, b.navVeto = false, false
		u = b.drv.Cmd(c, msec)
		if b.navVeto {
			// the navigator's shoot goal pulled the trigger where the fire
			// gate holds it (aim)
			u.Buttons &^= q2const.BUTTON_ATTACK
		}
		if b.fire {
			u.Buttons |= q2const.BUTTON_ATTACK
		}
		if b.bel != nil && b.shoot.HoldOff(b.bel) {
			// a continuous weapon fires on while the trigger is held, and
			// the game switches only once it stops: let go for the switch
			u.Buttons &^= q2const.BUTTON_ATTACK
		}
		if u.Buttons&q2const.BUTTON_ATTACK != 0 {
			b.fired++
			b.shoot.NoteFire(b.cmdNow())
		}
		b.cmdSub++
		// traced with the next tick event; the idle commands of dead and
		// intermission frames (no decision tick follows on the level
		// attempt) are not
		b.recordCmd(u, seq, ack, c.Frame.ServerFrame)
	}
	return u
}

// recordCmd keeps a sent command for the next tick event (the ones over
// maxPendingCmds are counted, not kept).
func (b *Bot) recordCmd(u shared.UserCmd, seq, ack int, frame int32) {
	if b.cfg.OnDecision == nil {
		return
	}
	if len(b.pending) >= maxPendingCmds {
		b.dropped++
		return
	}
	b.pending = append(b.pending, trace.UserCmd{Msec: u.Msec, Buttons: u.Buttons, Angles: u.Angles, Forward: u.ForwardMove,
		Side: u.SideMove, Up: u.UpMove, Impulse: u.Impulse, Seq: seq, Ack: ack, Frame: frame})
}

// belief is the belief the commands of the current frame are built on.
func (b *Bot) belief() *worldmodel.Belief {
	if b.bel != nil {
		return b.bel
	}
	return b.world.Belief()
}

// vision is the line-of-fire oracle of the current frame (the percept's;
// nil before the first frame).
func (b *Bot) vision() *perception.Vision {
	if b.testVision != nil {
		return b.testVision
	}
	if pc := b.world.Percept(); pc != nil {
		return pc.Vision()
	}
	return nil
}

// idleCmd holds still with the current view.
func idleCmd(ps *shared.PlayerState, msec int) shared.UserCmd {
	yaw, pitch := ps.ViewAngles[q2const.YAW], ps.ViewAngles[q2const.PITCH]
	return control.Compose(control.Idle(yaw, pitch), yaw, pitch, ps.PMove.DeltaAngles, msec)
}

// aim is the driver's Aim: where each command looks. A view the movement
// depends on (MustFace) wins; then a facing the route step needs; then
// the target (slewed, led, with the fire gate deciding the trigger); then
// a hit the bot did not see coming; then a point the step wants watched
// while the bot stands; then the side an attacker only heard sounded from
// (heardAttacker); else the path heading.
func (b *Bot) aim(in control.MoveIntent, st *navsim.State) (float32, float32) {
	eye := Vec3{st.Origin()[0], st.Origin()[1], st.Origin()[2] + st.ViewHeight}
	if in.MustFace {
		b.shoot.SetView(in.FaceYaw, in.FacePitch)
		if in.Fire {
			b.navVeto = b.vetoNavFire(eye, in.FaceYaw, in.FacePitch)
		}
		return in.FaceYaw, in.FacePitch
	}
	bel := b.belief()
	view := bel.Self.ViewAngles
	if b.dir.MustFace {
		// the route step needs the view (a directional trigger)
		b.shoot.SetView(b.dir.FaceYaw, 0)
		return b.dir.FaceYaw, 0
	}
	if tr := b.liveTrack(bel, b.target); tr != nil {
		return b.aimAt(eye, view, tr, fireModeOf(b.firePolicy))
	}
	if b.cmdNow() < b.searchUntil {
		sy, cy := sincos(b.searchYaw)
		p := Vec3{eye[0] + cy*256, eye[1] + sy*256, eye[2]}
		return b.shoot.Turn(eye, view[q2const.YAW], view[q2const.PITCH], p, navrt.CmdMsec)
	}
	if b.dir.HasLook && b.standing() {
		y, p, _ := b.shoot.Aim(eye, view[q2const.YAW], view[q2const.PITCH], AimTarget{Point: b.dir.Look}, navrt.CmdMsec)
		return y, p
	}
	if t := heardAttacker(bel); t != nil {
		// the path's movement does not depend on the view: look where the
		// attacker sounded from while walking on
		return b.shoot.Turn(eye, view[q2const.YAW], view[q2const.PITCH], Vec3{t.Loc[0], t.Loc[1], eye[2]}, navrt.CmdMsec)
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

// Movement returns the movement the combat layer executes relative to the
// target in the last frame (hold outside a fight).
func (b *Bot) Movement() control.Move {
	if b.mode != ModeFight {
		return control.MoveHold
	}
	return b.fight.move
}

// Stats counts what the bot did.
type Stats struct {
	Frames       int // frames folded on the current level
	FireCmds     int // commands with the trigger held
	SideCommands int // side command pairs sent
	Switches     int // "use" commands sent
	Explores     int // explore bursts started
	Pressed      bool
	Ticks        int // decision ticks on the level attempt
	TrapKills    int // "kill" commands sent from a pit (trapTick), wedged (wedgeTick) or stalled (stallTick)
}

// Stats returns the counters.
func (b *Bot) Stats() Stats {
	return Stats{Frames: b.frames, FireCmds: b.fired, SideCommands: b.sideSent, Switches: b.shoot.Switches(), Explores: b.explores,
		Pressed: b.pressed, Ticks: b.ticks, TrapKills: b.kills}
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
	out += fmt.Sprintf("intent %s target %q fire %s move %s weapon %q pickup %q danger %.2f\n", b.intent.Mode, b.intent.Target,
		b.intent.FirePolicy, b.intent.Movement, b.intent.Weapon, b.intent.Pickup, b.intent.Danger)
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
		if t.Kind != perception.KindMonster.String() || dist3(t.Loc, s.Origin) > 1500 {
			continue
		}
		out += fmt.Sprintf("  track %s %s lump %d at %v (seen %v) %s vis %v shoot %v aware %s\n", t.ID, t.Class, t.Lump, t.Loc, t.LocSeen,
			t.Life, t.Visible, t.Shootable, t.Awareness)
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
