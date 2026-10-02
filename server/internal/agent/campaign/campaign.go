// Package campaign drives one episode of the single-player campaign
// (fixtures/agent/routes/campaign.json: demo1 → demo2 → demo3 → demo2
// again → victory.pcx) through a session (session.Lockstep or
// session.InProc) with the agent's bot (package bot).
//
// A new level is signalled by the arrival of svc_serverdata (the client's
// level generation changes, fakeclient.LevelGen); once the client is active
// on it, the campaign loads the map data (checked against CS_MAPCHECKSUM),
// the nav graph and the route table of the map's visit (visits are counted
// per map) and enters the bot. The bot's level memory is checkpointed at
// each entry under (map, visit) and restored on a reload. A death (the
// belief shows STAT_HEALTH <= 0, PM_DEAD or PM_GIB) is followed, after
// DeathWait of game time, by Control.Reload (the level-entry save "save0"),
// and counted. A cinematic (PlayerNum -1, a ".cin" level) is answered with
// "nextserver <spawncount>"; a picture (PlayerNum -1, ".pcx") ends the
// campaign: victory.
//
// Every arrival through an exit is checked against the exit the left
// level's route claims: the level (or picture, or cinematic) it names and,
// for a game level, its spawnpoint (the info_player_start the bot's first
// frame is at, by the map data). Any other arrival is an unplanned exit:
// the episode ends with ErrUnplannedExit (the next route table would be
// played from the wrong place).
//
// Watchdogs bound a level (LevelTimeout) and the episode (EpisodeTimeout)
// in game time, the deaths on one level (MaxDeaths), the time without a
// frame on the entered level (FrameTimeout), and progress: after
// ExploreAfter without route progress the bot explores for a while, after
// FailAfter the level fails as stalled.
//
// Every step is traced on an agent/trace Bus (episode_start, level_start,
// damage, kill, death, reload, stuck, level_end, error, episode_end, and
// the bot's decision events: one lane tick event per decision tick, plus
// the request events when Config.Bot.TraceRequests is set) and
// summarized by an agent/metrics Collector (EpisodeResult.Summary). The game's level
// counters (session.Truth) feed only level_end, for metrics; the bot never
// sees them.
package campaign

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// Control is what the campaign needs of the game beyond the client to
// recover from a death: reload the level-entry autosave ("load save0")
// and wait until the client is active again. Every session.Session
// implements it.
type Control interface {
	Reload(ctx context.Context) error
}

// Watchdog and timing defaults (game time).
const (
	DefaultLevelTimeout   = 20 * time.Minute
	DefaultEpisodeTimeout = 90 * time.Minute
	DefaultMaxDeaths      = 5
	DefaultExploreAfter   = 120 * time.Second
	DefaultFailAfter      = 300 * time.Second
	DefaultExploreFor     = 30 * time.Second
	// DefaultFrameTimeout bounds the game time the entered level may go
	// without a frame for the bot (a stalled or dropped client).
	DefaultFrameTimeout = 10 * time.Second
	// DefaultDeathWait is how long the bot lies dead before the reload
	// (the plan's 1-2 s).
	DefaultDeathWait = 1500 * time.Millisecond
	// DefaultStallExplore is how long the bot explores when the route
	// gave up on a step (then the step starts over).
	DefaultStallExplore = 20 * time.Second
	// activeWait bounds the frames the campaign waits for the client to
	// become active on a new level or a cinematic to end.
	activeWait = 600
)

// Config configures an episode. Campaign (or RoutesDir) and a Library (or
// the session's ReadFile, used by a private one) are needed; the rest has
// defaults.
type Config struct {
	// Campaign is the loaded campaign (nil: route.Load(RoutesDir)).
	Campaign *route.Campaign
	// RoutesDir is the routes directory ("": DefaultRoutesDir()).
	RoutesDir string
	// Library holds the maps' static data (nil: a private one reading
	// through the session at the campaign's skill).
	Library *Library
	// Control reloads the level-entry save after a death (nil: the
	// session's Reload).
	Control Control
	// Bot configures the bot (ReadFile defaults to the session's).
	Bot bot.Config
	// Bus receives the trace (nil: a private bus). Run, Episode and
	// Seed stamp the events.
	Bus     *trace.Bus
	Episode int
	Seed    uint64
	// EntryCommands are client commands sent at every level entry,
	// reloads included (test cheats such as "god" and "notarget"). The
	// cheats "god", "notarget" and "noclip" toggle, and single player
	// carries god and notarget over level changes and into saves (C
	// SaveClientData / FetchClientEntData: pers.savedFlags), so the
	// campaign makes them idempotent: when the game answers "<cheat> OFF"
	// it sends the command again.
	EntryCommands []string

	LevelTimeout   time.Duration
	EpisodeTimeout time.Duration
	// MaxDeaths is the most deaths one level may take (0: default; -1:
	// none allowed).
	MaxDeaths    int
	ExploreAfter time.Duration
	ExploreFor   time.Duration
	FailAfter    time.Duration
	DeathWait    time.Duration
	FrameTimeout time.Duration

	// Visits counts earlier visits per map for an episode that starts
	// mid-campaign (a test starting on the second visit of demo2 passes
	// {"demo2": 1}).
	Visits map[string]int
	// StopAfter ends the episode (completed, without victory) once this
	// many levels were left through an exit (0: play to the terminal).
	StopAfter int

	// OnFrame, when set, is called after every frame the campaign ran
	// with the bot on a level (tests and tools: injecting events,
	// sampling the bot).
	OnFrame func(f *Frame)
	// Logf, when set, receives progress lines (level changes, deaths,
	// the route's step events).
	Logf func(format string, args ...any)
	// Now is the wall clock (nil: time.Now), for the wall times reported.
	Now func() time.Time
}

func (c *Config) defaults() {
	if c.LevelTimeout <= 0 {
		c.LevelTimeout = DefaultLevelTimeout
	}
	if c.EpisodeTimeout <= 0 {
		c.EpisodeTimeout = DefaultEpisodeTimeout
	}
	switch {
	case c.MaxDeaths == 0:
		c.MaxDeaths = DefaultMaxDeaths
	case c.MaxDeaths < 0:
		c.MaxDeaths = 0
	}
	if c.ExploreAfter <= 0 {
		c.ExploreAfter = DefaultExploreAfter
	}
	if c.ExploreFor <= 0 {
		c.ExploreFor = DefaultExploreFor
	}
	if c.FailAfter <= 0 {
		c.FailAfter = DefaultFailAfter
	}
	if c.DeathWait <= 0 {
		c.DeathWait = DefaultDeathWait
	}
	if c.FrameTimeout <= 0 {
		c.FrameTimeout = DefaultFrameTimeout
	}
	if c.Now == nil {
		c.Now = time.Now
	}
}

// Frame is what OnFrame sees.
type Frame struct {
	Session session.Session
	Bot     *bot.Bot
	// Level is the index of the level in the episode, Map and Visit name
	// it.
	Level int
	Map   string
	Visit int
	// GameMs is the session clock; LevelMs the game time on the level.
	GameMs, LevelMs int64
}

// Episode outcomes (EpisodeResult.Outcome, trace EpisodeEnd.Outcome).
const (
	OutcomeCompleted = "completed"
	OutcomeFailed    = "failed"
	OutcomeAborted   = "aborted"
)

// LevelResult is one level of the episode: from its arrival to its exit
// (a death reload stays on the level).
type LevelResult struct {
	Map     string
	Visit   int
	Route   string // the route table's name
	Outcome string // trace.Outcome*
	Reason  string
	GameMs  int64
	WallMs  int64
	Deaths  int
	// Steps is the route's step count, StepsDone how many were done when
	// the level ended (of the last attempt; all of them on its exit: the
	// level change completes the exit step).
	Steps, StepsDone int
}

// EpisodeResult is the episode's outcome.
type EpisodeResult struct {
	Outcome string
	Reason  string
	// Victory: the campaign's terminal picture was reached.
	Victory bool
	Levels  []LevelResult
	Deaths  int
	GameMs  int64
	WallMs  int64
	// Summary is the metrics collector's view of the episode's trace.
	Summary metrics.EpisodeSummary
	// Diagnostics, for an episode that failed on a level, is the bot's
	// state then (bot.Bot.Describe: the route step, position, navigation
	// status and a summary of the belief).
	Diagnostics string
}

// ErrNoRoute is wrapped by the episode's error when a level has no route
// table for its visit.
var ErrNoRoute = errors.New("campaign: no route for the level")

// ErrUnplannedExit is wrapped by the episode's error when the bot left a
// level other than through the exit its route claims: it arrived on
// another level, picture or cinematic, or at another spawnpoint.
var ErrUnplannedExit = errors.New("campaign: unplanned exit")

// Run plays one episode on the started session s (when its client is nil,
// Run preloads the campaign's maps into the library and then starts it)
// and returns its result. The error is non-nil when the
// episode could not be played (session failures, missing data, an
// unplanned level); an episode that ran but failed (a watchdog) reports it
// in the result with a nil error. ctx cancellation aborts the episode.
func Run(ctx context.Context, s session.Session, cfg Config) (EpisodeResult, error) {
	cfg.defaults()
	r, err := newRunner(ctx, s, cfg)
	if err != nil {
		return EpisodeResult{Outcome: OutcomeFailed, Reason: err.Error()}, err
	}
	return r.run(ctx)
}

// runner is the state of one episode.
type runner struct {
	s    session.Session
	ctl  Control
	cfg  Config
	camp *route.Campaign
	lib  *Library
	bus  *trace.Bus
	col  *metrics.Collector
	bot  *bot.Bot

	startGMs  int64
	startWall time.Time

	gen    int            // the level generation handled last
	visits map[string]int // arrivals per map

	// the level being played (lvl < 0: none yet)
	lvl         int
	lv          *levelState
	results     []LevelResult
	deaths      int
	nextserver  int32 // the spawncount answered with nextserver (0: none)
	cinematicAt int64
	// pending: a game level arrived and is entered once the client is
	// active on it (since pendingAt)
	pending   bool
	pendingAt int64
	exits     int
	// leaving is the level left through an exit, ended once the next
	// level is known
	leaving *levelState
	// expect is where the next arrival must be: the exit the left level's
	// route claims, or what follows the cinematic it led to (nil: no
	// claim, the episode's first level)
	expect *mapdata.LevelString
}

type levelState struct {
	name     string
	visit    int
	table    *route.Table
	startGMs int64
	start    time.Time
	deaths   int
	entered  bool // the bot entered the current attempt
	deadAt   int64
	// watchdog: last progress and the last explore burst
	progressAt  int64
	exploreAt   int64
	stallSeen   bool
	stucks      int
	combatMs    int64
	lastFrameAt int64 // the last frame folded (0: none since the entry)
	frameAt     int64 // the entry or the last frame folded (the frame watchdog)
	leftAt      int64
	stepsDone   int
	// truth is the game's level counters at the last frame (METRICS
	// ONLY: they go to level_end, never to the bot)
	truth session.Truth
	// damageAt is the time of the last damage event traced; fought the
	// tracks the bot fought, in the order it first fought them (the kill
	// events of one frame come out in that order)
	damageAt int64
	fought   []foughtTrack
	// entry cheats: when they were sent, the prints seen since, the
	// toggles sent again
	enteredAt    int64
	prints       uint64
	cheatRetries int
}

// foughtTrack is a track the bot fought on a level.
type foughtTrack struct {
	id   string
	dead bool // traced as killed
}

// cheatReply returns the word the reply to a toggling cheat starts with
// ("" for other commands; C: game/g_cmds.c Cmd_God_f, Cmd_Notarget_f,
// Cmd_Noclip_f).
func cheatReply(cmd string) string {
	switch cmd {
	case "god":
		return "godmode"
	case "notarget":
		return "notarget"
	case "noclip":
		return "noclip"
	}
	return ""
}

// cheatWindow is how long (ms) after a level entry the campaign watches
// the replies to the entry cheats.
const cheatWindow = 3000

// fixCheats sends a toggling entry cheat again when the game turned it
// off (it was still on: carried over from the last level or the save).
func (r *runner) fixCheats(c *fakeclient.Client, now int64) {
	lv := r.lv
	if now-lv.enteredAt > cheatWindow || lv.cheatRetries >= 2*len(r.cfg.EntryCommands) {
		return
	}
	fresh, _ := fakeclient.NewSince(c.Prints, c.Counts.Prints, lv.prints)
	lv.prints = c.Counts.Prints
	for _, p := range fresh {
		for _, cmd := range r.cfg.EntryCommands {
			if w := cheatReply(cmd); w != "" && strings.TrimSpace(p) == w+" OFF" {
				r.logf("campaign: %s: %q was on already: sending it again", lv.name, cmd)
				c.StringCmd(cmd)
				lv.cheatRetries++
			}
		}
	}
}

func newRunner(ctx context.Context, s session.Session, cfg Config) (*runner, error) {
	camp := cfg.Campaign
	if camp == nil {
		dir := cfg.RoutesDir
		if dir == "" {
			dir = DefaultRoutesDir()
		}
		c, err := route.Load(dir)
		if err != nil {
			return nil, err
		}
		camp = c
	}
	lib := cfg.Library
	if lib == nil {
		lib = NewLibrary(LibraryConfig{ReadFile: s.ReadFile, Skill: camp.Skill, Logf: cfg.Logf})
	} else if lib.Skill() != camp.Skill {
		return nil, fmt.Errorf("campaign: library resolves skill %d, campaign %s is for skill %d", lib.Skill(), camp.Name, camp.Skill)
	}
	if s.Client() == nil {
		// no level entry may wait for a graph build once the client is
		// connected (Library.Preload)
		if err := lib.Preload(ctx, CampaignMaps(camp)...); err != nil {
			return nil, err
		}
		if err := s.Start(ctx); err != nil {
			return nil, err
		}
	}
	bus := cfg.Bus
	if bus == nil {
		bus = trace.NewBus("campaign", nil)
	}
	bcfg := cfg.Bot
	if bcfg.ReadFile == nil {
		bcfg.ReadFile = s.ReadFile
	}
	if bcfg.Logf == nil {
		bcfg.Logf = cfg.Logf
	}
	if bcfg.Seed == 0 {
		bcfg.Seed = int64(cfg.Seed)
	}
	ctl := cfg.Control
	if ctl == nil {
		ctl = s
	}
	r := &runner{s: s, ctl: ctl, cfg: cfg, camp: camp, lib: lib, bus: bus, col: metrics.NewCollector(),
		gen: -1, visits: map[string]int{}, lvl: -1}
	// the bot's decision events go on the bus, stamped like the others
	user := bcfg.OnDecision
	bcfg.OnDecision = func(d *trace.Decision) {
		r.publish(trace.TypeDecision, *d)
		if user != nil {
			user(d)
		}
	}
	r.bot = bot.New(bcfg)
	for m, n := range cfg.Visits {
		r.visits[m] = n
	}
	return r, nil
}

func (r *runner) logf(format string, args ...any) {
	if r.cfg.Logf != nil {
		r.cfg.Logf(format, args...)
	}
}

// publish stamps and emits an event of the level being played (or the
// session's current map) at the session clock, and accounts it.
func (r *runner) publish(typ string, body any) {
	m := r.s.MapName()
	if r.lv != nil {
		m = r.lv.name
	}
	r.publishAt(typ, body, m, r.s.GameTimeMs())
}

func (r *runner) publishAt(typ string, body any, mapName string, gms int64) {
	e := trace.Event{Type: typ, Ep: r.cfg.Episode, GMs: gms, Lvl: max(r.lvl, 0), Map: mapName, Body: body}
	if c := r.s.Client(); c != nil {
		e.SF = c.Frame.ServerFrame
	}
	e = r.bus.Publish(e)
	_ = r.col.Add(e)
}

func (r *runner) run(ctx context.Context) (EpisodeResult, error) {
	r.startGMs, r.startWall = r.s.GameTimeMs(), r.cfg.Now()
	r.publish(trace.TypeEpisodeStart, trace.EpisodeStart{Seed: r.cfg.Seed})
	for {
		if err := ctx.Err(); err != nil {
			return r.finish(OutcomeAborted, trace.OutcomeAborted, err.Error(), nil)
		}
		now := r.s.GameTimeMs()
		if now-r.startGMs > r.cfg.EpisodeTimeout.Milliseconds() {
			return r.finish(OutcomeFailed, trace.OutcomeTimeout, fmt.Sprintf("episode watchdog: %s of game time", r.cfg.EpisodeTimeout), nil)
		}
		c := r.s.Client()
		if g := c.LevelGen(); g != r.gen {
			done, res, err := r.arrive(c)
			if done || err != nil {
				return res, err
			}
		}
		if r.pending {
			switch {
			case c.State == fakeclient.CaActive:
				if done, res, err := r.begin(ctx, c); done {
					return res, err
				}
			case r.s.GameTimeMs()-r.pendingAt > activeWait*session.FrameMsec:
				err := fmt.Errorf("campaign: the client did not become active on %q within %d frames", c.MapName(), activeWait)
				return r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
			}
		}
		var cmd session.CmdFunc
		if r.lv != nil && r.lv.entered {
			cmd = r.bot.Cmd
		}
		if err := r.s.Step(ctx, cmd); err != nil {
			if ctx.Err() != nil {
				return r.finish(OutcomeAborted, trace.OutcomeAborted, ctx.Err().Error(), nil)
			}
			return r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		}
		if r.lv == nil || !r.lv.entered || c.LevelGen() != r.gen {
			r.waitCinematic(c)
			continue
		}
		if r.bot.Observe(c, r.s.GameTimeMs()) {
			if done, res, err := r.checkLevel(ctx); done || err != nil {
				return res, err
			}
		}
		if r.lv != nil {
			if done, res, err := r.watchLevel(); done {
				return res, err
			}
		}
	}
}

// watchLevel runs the level watchdogs that hold whether frames come or
// not (after every step on an entered level): the level's game time and
// the time without a frame for the bot.
func (r *runner) watchLevel() (bool, EpisodeResult, error) {
	lv := r.lv
	now := r.s.GameTimeMs()
	switch {
	case now-lv.startGMs > r.cfg.LevelTimeout.Milliseconds():
		res, err := r.fail(trace.OutcomeTimeout, fmt.Sprintf("level watchdog: %s of game time", r.cfg.LevelTimeout))
		return true, res, err
	case now-lv.frameAt > r.cfg.FrameTimeout.Milliseconds():
		res, err := r.fail(trace.OutcomeError, fmt.Sprintf("frame watchdog: no frame on %s for %s of game time", lv.name, r.cfg.FrameTimeout))
		return true, res, err
	}
	return false, EpisodeResult{}, nil
}

// arrive handles a new svc_serverdata: the end of the level being played
// and the start of the next one, a cinematic or the victory picture.
func (r *runner) arrive(c *fakeclient.Client) (bool, EpisodeResult, error) {
	r.gen = c.LevelGen()
	sd := c.ServerData
	if r.lv != nil {
		// left through an exit: the level ends once the next one is named
		r.leaving, r.lv = r.lv, nil
		r.leaving.leftAt = r.s.GameTimeMs()
		want := mapdata.ParseLevelString(r.leaving.table.Exit.Map)
		r.expect = &want
	}
	if sd.PlayerNum == -1 && strings.HasSuffix(strings.ToLower(sd.LevelName), ".pcx") {
		name := sd.LevelName
		if err := r.checkArrival(name, nil); err != nil {
			res, err := r.unplanned(err)
			return true, res, err
		}
		if lv := r.leaving; lv != nil {
			lv.stepsDone = len(lv.table.Steps)
		}
		outcome, levelOutcome, reason := OutcomeCompleted, trace.OutcomeVictory, "victory: "+name
		if t := r.camp.Terminal.Exit; t != "" && !strings.EqualFold(t, name) {
			outcome, levelOutcome, reason = OutcomeFailed, trace.OutcomeExit, fmt.Sprintf("ended on picture %s, not the terminal %s", name, t)
		}
		res, err := r.finish(outcome, levelOutcome, reason, nil)
		res.Victory = outcome == OutcomeCompleted
		return true, res, err
	}
	if sd.PlayerNum == -1 {
		if done, res, err := r.left(sd.LevelName, nil); done {
			return true, res, err
		}
		// a cinematic (or a demo): the client asks for the next server
		r.logf("campaign: %s: answering with nextserver %d", sd.LevelName, sd.ServerCount)
		c.StringCmd(fmt.Sprintf("nextserver %d", sd.ServerCount))
		r.nextserver, r.cinematicAt = sd.ServerCount, r.s.GameTimeMs()
		return false, EpisodeResult{}, nil
	}
	// a game level: it is named once its configstrings are in (active)
	r.nextserver = 0
	r.pending, r.pendingAt = true, r.s.GameTimeMs()
	return false, EpisodeResult{}, nil
}

// begin starts the level the client became active on: its visit, route
// table and the bot's entry.
func (r *runner) begin(ctx context.Context, c *fakeclient.Client) (bool, EpisodeResult, error) {
	r.pending = false
	name := c.MapName()
	if done, res, err := r.left(name, c); done {
		return true, res, err
	}
	visit := r.visits[name]
	r.visits[name]++
	table, err := r.camp.Select(name, visit)
	if err != nil {
		err = fmt.Errorf("%w: %s visit %d (%v)", ErrNoRoute, name, visit, err)
		res, _ := r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		return true, res, err
	}
	r.lvl++
	r.lv = &levelState{name: name, visit: visit, table: table, startGMs: r.s.GameTimeMs(), start: r.cfg.Now()}
	r.logf("campaign: level %d: %s visit %d (route %s)", r.lvl, name, visit, table.Name)
	if err := r.enter(ctx, c, false); err != nil {
		res, _ := r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		return true, res, err
	}
	return false, EpisodeResult{}, nil
}

// left checks an arrival on to (c: the client active on a game level, nil
// for a cinematic) against the exit claimed, ends the level the bot left
// through it (if any), and the episode when StopAfter levels are done or
// the exit was unplanned.
func (r *runner) left(to string, c *fakeclient.Client) (bool, EpisodeResult, error) {
	if err := r.checkArrival(to, c); err != nil {
		res, err := r.unplanned(err)
		return true, res, err
	}
	lv := r.leaving
	if lv == nil {
		return false, EpisodeResult{}, nil
	}
	r.leaving = nil
	if n := len(lv.table.Steps); lv.stepsDone < n-1 {
		// the right exit, early: the claimed destination is what the next
		// route needs (a ride's last frames into the exit trigger may not
		// have been seen)
		r.logf("campaign: %s left at step %d of %d", lv.name, lv.stepsDone, n)
	}
	lv.stepsDone = len(lv.table.Steps) // the level change did the exit step
	r.endLevel(lv, trace.OutcomeExit, "to "+to, lv.leftAt)
	if r.exits++; r.cfg.StopAfter > 0 && r.exits >= r.cfg.StopAfter {
		res, err := r.finish(OutcomeCompleted, "", fmt.Sprintf("stopped after %d levels, on to %s", r.exits, to), nil)
		return true, res, err
	}
	return false, EpisodeResult{}, nil
}

// spawnSlack is how far (units) from its info_player_start the bot's
// first frame on a level may be (it spawns 10 units up: SelectSpawnPoint
// and PutClientInServer, then it drops).
const spawnSlack = 64

// checkArrival checks an arrival on level to against the claimed exit
// (r.expect), and then claims what follows a cinematic: the level must be
// the one the exit names and, for a game level (c, active on it), the bot
// must be at the spawnpoint the exit names (the info_player_start
// SelectSpawnPoint picks, by the map data), not nearer another one. It
// returns an error wrapping ErrUnplannedExit (or the map data's).
func (r *runner) checkArrival(to string, c *fakeclient.Client) error {
	want := r.expect
	r.expect = nil
	if want == nil {
		return nil
	}
	if !strings.EqualFold(to, want.Map) {
		return fmt.Errorf("%w: arrived on %s, the route exits to %s", ErrUnplannedExit, to, want.Raw)
	}
	if c == nil {
		if want.Next != "" {
			next := mapdata.ParseLevelString(want.Next)
			r.expect = &next
		}
		return nil
	}
	md, err := r.lib.Map(to)
	if err != nil {
		return err
	}
	if why := wrongSpawn(md, want.Spawnpoint, c.Origin()); why != "" {
		return fmt.Errorf("%w: %s, the route exits to %s", ErrUnplannedExit, why, want.Raw)
	}
	return nil
}

// wrongSpawn says why origin o (the first frame on map md) is not the
// spawnpoint sp: farther than spawnSlack from it and nearer another
// info_player_start ("" when it is there, or md cannot tell).
func wrongSpawn(md *mapdata.Map, sp string, o Vec3) string {
	want, ok := md.SpawnPoint(sp)
	if !ok {
		return ""
	}
	dw := dist3(o, want.Origin)
	if dw <= spawnSlack {
		return ""
	}
	for i := range md.Spawns {
		s := &md.Spawns[i]
		if s.Entity != want.Entity && dist3(o, s.Origin) < dw {
			return fmt.Sprintf("arrived at %v, at spawnpoint %q (#%d), %.0f units from %q (#%d)", o, s.Targetname, s.Entity, dw, want.Targetname, want.Entity)
		}
	}
	return ""
}

// unplanned ends the episode on an unplanned exit: the level left (if
// any) ends with an error.
func (r *runner) unplanned(err error) (EpisodeResult, error) {
	if lv := r.leaving; lv != nil {
		r.leaving = nil
		r.endLevel(lv, trace.OutcomeError, fmt.Sprintf("%v (at step %d of %d)", err, lv.stepsDone, len(lv.table.Steps)), lv.leftAt)
	}
	r.logf("campaign: %v", err)
	return r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
}

// waitCinematic repeats the nextserver answer of a cinematic that does not
// end (the reliable command was lost to a reconnect).
func (r *runner) waitCinematic(c *fakeclient.Client) {
	if r.nextserver == 0 || r.s.GameTimeMs()-r.cinematicAt < 5000 {
		return
	}
	c.StringCmd(fmt.Sprintf("nextserver %d", r.nextserver))
	r.cinematicAt = r.s.GameTimeMs()
}

// enter loads the level's static data and enters the bot (an arrival, or
// the reload after a death when reload is set).
func (r *runner) enter(ctx context.Context, c *fakeclient.Client, reload bool) error {
	lv := r.lv
	md, err := r.lib.Map(lv.name)
	if err != nil {
		return err
	}
	cs := c.ConfigStrings[q2const.CS_MAPCHECKSUM]
	if n, err := strconv.ParseInt(strings.TrimSpace(cs), 10, 64); err != nil || uint32(n) != md.Checksum {
		return fmt.Errorf("campaign: %s: the server's map checksum %q is not the map data's %d (%#08x)", lv.name, cs, int32(md.Checksum), md.Checksum)
	}
	_, g, err := r.lib.Level(ctx, lv.name)
	if err != nil {
		return err
	}
	key := worldmodel.LevelKey{Map: lv.name, Visit: lv.visit}
	if err := r.bot.Enter(bot.Level{Key: key, Gen: c.LevelGen(), Map: md, Graph: g, Route: lv.table}); err != nil {
		return err
	}
	for _, cmd := range r.cfg.EntryCommands {
		c.StringCmd(cmd)
	}
	lv.prints = c.Counts.Prints
	lv.cheatRetries = 0
	now := r.s.GameTimeMs()
	lv.enteredAt = now
	lv.entered, lv.deadAt = true, 0
	lv.lastFrameAt, lv.frameAt = 0, now
	lv.damageAt, lv.fought = now, nil
	lv.progressAt, lv.exploreAt, lv.stallSeen, lv.stucks = now, 0, false, 0
	if !reload {
		r.publish(trace.TypeLevelStart, trace.LevelStart{Visit: lv.visit, Gen: c.LevelGen(), Checksum: cs})
	}
	return nil
}

// checkLevel runs the per-frame checks after the bot folded a frame:
// death and reload, the watchdogs, stuck events.
func (r *runner) checkLevel(ctx context.Context) (bool, EpisodeResult, error) {
	lv := r.lv
	now := r.s.GameTimeMs()
	bel := r.bot.Belief()
	if lv.lastFrameAt > 0 && (bel.Self.InCombat || r.bot.Target() != "") {
		lv.combatMs += now - lv.lastFrameAt
	}
	lv.lastFrameAt, lv.frameAt = now, now
	r.fixCheats(r.s.Client(), now)
	r.traceFight(lv, bel, now)
	if t, err := r.s.Truth(); err == nil {
		lv.truth = t
	}
	if x := r.bot.Route(); x != nil {
		lv.stepsDone = x.Index()
	}
	if r.cfg.OnFrame != nil {
		r.cfg.OnFrame(&Frame{Session: r.s, Bot: r.bot, Level: r.lvl, Map: lv.name, Visit: lv.visit, GameMs: now, LevelMs: now - lv.startGMs})
	}

	if bel.Self.Dead {
		if lv.deadAt == 0 {
			lv.deadAt = now
			cause := "unknown"
			if n := len(bel.Damage); n > 0 {
				cause = bel.Damage[n-1].Cause
			}
			r.logf("campaign: %s: died at %v (%s)", lv.name, bel.Self.Origin, cause)
			r.publish(trace.TypeDeath, trace.Death{Cause: cause, Health: bel.Self.Health})
			return false, EpisodeResult{}, nil
		}
		if now-lv.deadAt < r.cfg.DeathWait.Milliseconds() {
			return false, EpisodeResult{}, nil
		}
		return r.reload(ctx)
	}

	if x := r.bot.Route(); x != nil {
		if p := x.LastProgress(); p > lv.progressAt {
			lv.progressAt = p
		}
		switch {
		case now-lv.progressAt > r.cfg.FailAfter.Milliseconds():
			res, err := r.fail(trace.OutcomeStalled, fmt.Sprintf("no progress for %s at step %d (%s)", r.cfg.FailAfter, x.Index(), x.Current().Desc))
			return true, res, err
		case x.Stalled() && !lv.stallSeen:
			lv.stallSeen = true
			r.logf("campaign: %s: step %d stalled (%s): exploring", lv.name, x.Index(), x.Current().Reason)
			r.bot.Explore(now + DefaultStallExplore.Milliseconds())
			lv.exploreAt = now
		case now-lv.progressAt > r.cfg.ExploreAfter.Milliseconds() && now-lv.exploreAt > r.cfg.ExploreAfter.Milliseconds()/2:
			r.logf("campaign: %s: no progress for %s: exploring", lv.name, r.cfg.ExploreAfter)
			r.bot.Explore(now + r.cfg.ExploreFor.Milliseconds())
			lv.exploreAt = now
		}
		if !x.Stalled() {
			lv.stallSeen = false
		}
	}
	if n := r.bot.Navigator(); n != nil {
		st := n.Status()
		if st.Stucks < lv.stucks {
			lv.stucks = 0 // a new goal counts from 0
		}
		if st.Stucks > lv.stucks {
			lv.stucks = st.Stucks
			r.publish(trace.TypeStuck, trace.Stuck{Stage: stuckStage(st), Node: int(st.Node), Pos: bel.Self.Origin})
		}
	}
	return false, EpisodeResult{}, nil
}

// traceFight publishes the damage the bot took since the last frame and
// the kills of monsters it fought, as the bot perceived them (the belief:
// a track it targeted seen dead).
func (r *runner) traceFight(lv *levelState, bel *worldmodel.Belief, now int64) {
	for i := range bel.Damage {
		d := &bel.Damage[i]
		if d.At <= lv.damageAt {
			continue
		}
		body := trace.Damage{Amount: d.Health + d.Armor, Health: d.Health, Armor: d.Armor, Source: d.Source}
		if d.BearingKnown {
			b := float64(d.Relative)
			body.Bearing = &b
		}
		r.publish(trace.TypeDamage, body)
	}
	if n := len(bel.Damage); n > 0 {
		lv.damageAt = max(lv.damageAt, bel.Damage[n-1].At)
	}
	if id := r.bot.Target(); id != "" && !slices.ContainsFunc(lv.fought, func(f foughtTrack) bool { return f.id == id }) {
		lv.fought = append(lv.fought, foughtTrack{id: id})
	}
	for i := range lv.fought {
		f := &lv.fought[i]
		if f.dead {
			continue
		}
		if t := bel.Track(f.id); t != nil && t.Life != worldmodel.LifeAlive && t.Life != worldmodel.LifeDying {
			f.dead = true
			r.publish(trace.TypeKill, trace.Kill{Target: f.id, Class: t.Class, Weapon: bel.Self.Weapon})
		}
	}
}

// stuckStage names the navigator's recovery stage (see navrt's stuck
// escalation: strafe or jump, the other one, back off, repath, mark the
// edge blocked, report).
func stuckStage(st navrt.Status) string {
	monster := st.Cause == navrt.CauseEntity
	switch st.Level {
	case 1:
		if monster {
			return "strafe"
		}
		return "jump"
	case 2:
		if monster {
			return "jump"
		}
		return "strafe"
	case 3:
		return "backoff"
	case 4:
		return "repath"
	case 5:
		return "block"
	}
	return "report"
}

// reload loads the level-entry save after a death and enters the bot
// again (its level memory restored).
func (r *runner) reload(ctx context.Context) (bool, EpisodeResult, error) {
	lv := r.lv
	lv.deaths++
	r.deaths++
	if lv.deaths > r.cfg.MaxDeaths {
		res, err := r.fail(trace.OutcomeDeathLimit, fmt.Sprintf("%d deaths on %s", lv.deaths, lv.name))
		return true, res, err
	}
	gen := r.s.LevelGen()
	r.logf("campaign: %s: reloading save0 (death %d)", lv.name, lv.deaths)
	if err := r.ctl.Reload(ctx); err != nil {
		res, _ := r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		return true, res, err
	}
	c := r.s.Client()
	if c.LevelGen() == gen || c.MapName() != lv.name {
		err := fmt.Errorf("campaign: reload of %s landed on %q (generation %d)", lv.name, c.MapName(), c.LevelGen())
		res, _ := r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		return true, res, err
	}
	r.gen = c.LevelGen()
	r.publish(trace.TypeReload, trace.Reload{Slot: "save0", Deaths: lv.deaths})
	if err := r.enter(ctx, c, true); err != nil {
		res, _ := r.finish(OutcomeFailed, trace.OutcomeError, err.Error(), err)
		return true, res, err
	}
	return false, EpisodeResult{}, nil
}

// fail ends the level with outcome and the episode as failed.
func (r *runner) fail(outcome, reason string) (EpisodeResult, error) {
	diag := r.bot.Describe()
	r.logf("campaign: %s failed (%s): %s\n%s", r.levelName(), outcome, reason, diag)
	res, err := r.finish(OutcomeFailed, outcome, reason, nil)
	res.Diagnostics = diag
	return res, err
}

func (r *runner) levelName() string {
	if r.lv == nil {
		return "-"
	}
	return r.lv.name
}

// endLevel closes level lv (the one being played, or the one just left)
// at game time gms.
func (r *runner) endLevel(lv *levelState, outcome, reason string, gms int64) {
	end := trace.LevelEnd{Outcome: outcome, Reason: reason, CombatMs: lv.combatMs}
	if t := lv.truth; t.Map == lv.name {
		end.KilledMonsters, end.TotalMonsters = t.KilledMonsters, t.TotalMonsters
		end.FoundSecrets, end.TotalSecrets = t.FoundSecrets, t.TotalSecrets
	}
	r.publishAt(trace.TypeLevelEnd, end, lv.name, gms)
	res := LevelResult{Map: lv.name, Visit: lv.visit, Route: lv.table.Name, Outcome: outcome, Reason: reason,
		GameMs: gms - lv.startGMs, WallMs: r.cfg.Now().Sub(lv.start).Milliseconds(), Deaths: lv.deaths, Steps: len(lv.table.Steps), StepsDone: lv.stepsDone}
	r.results = append(r.results, res)
	r.logf("campaign: level %s visit %d: %s (%s) in %.1fs game, %.1fs wall, %d deaths", lv.name, lv.visit, outcome, reason,
		float64(res.GameMs)/1000, float64(res.WallMs)/1000, lv.deaths)
	if r.lv == lv {
		r.lv = nil
	}
}

// finish ends the level being played (with levelOutcome) and the
// episode.
func (r *runner) finish(outcome, levelOutcome, reason string, err error) (EpisodeResult, error) {
	now := r.s.GameTimeMs()
	if r.leaving != nil {
		lv := r.leaving
		r.leaving = nil
		lo := levelOutcome
		if lo == "" || lo == trace.OutcomeError || lo == trace.OutcomeAborted {
			// it was left through an exit before the episode ended
			lo = trace.OutcomeExit
			if levelOutcome == trace.OutcomeVictory {
				lo = trace.OutcomeVictory
			}
		}
		r.endLevel(lv, lo, reason, lv.leftAt)
	}
	if r.lv != nil {
		r.endLevel(r.lv, levelOutcome, reason, now)
	}
	diag := ""
	if err != nil {
		r.publish(trace.TypeError, trace.Error{Msg: err.Error(), Fatal: true})
		diag = r.bot.Describe()
	}
	r.publish(trace.TypeEpisodeEnd, trace.EpisodeEnd{Outcome: outcome, Reason: reason})
	res := EpisodeResult{Outcome: outcome, Reason: reason, Levels: r.results, Deaths: r.deaths,
		GameMs: r.s.GameTimeMs() - r.startGMs, WallMs: r.cfg.Now().Sub(r.startWall).Milliseconds(), Diagnostics: diag}
	for _, ep := range r.col.Summary().Episodes {
		if ep.Index == r.cfg.Episode {
			res.Summary = ep
		}
	}
	return res, err
}

// Vec3 is the game's vec3_t.
type Vec3 = mapdata.Vec3

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}
