// Package session runs the agent's headless client against an in-process
// Quake 2 server. Two drivers share one method set:
//
//   - Lockstep owns an sv.Server with a virtual clock and drives it and the
//     fakeclient from the calling goroutine: one Step is one 100 ms server
//     frame with four 25 ms usercmds. Runs are deterministic for a seed (the
//     server, the game and the bot only see the virtual clock and the seeded
//     generator) and much faster than real time.
//   - InProc connects to a realtime host.Instance over an in-memory
//     "loopback" connection and paces Step at about 100 ms of wall time
//     (for watching a bot live).
//
// The bot's client is "loopback" in both: the server skips the challenge
// check for it and never rate-drops it. The session never sends "disconnect"
// (a single-player game ends when its player leaves).
//
// Everything the bot may perceive must come from Client(); Truth reads the
// game's level counters and exists for metrics only.
package session

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"quake2web/server/internal/api"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/game"
	"quake2web/server/internal/host"
	"quake2web/server/internal/qcommon/shared"
)

// Timing of one Step.
const (
	FrameMsec    = 100 // one server frame
	CmdMsec      = 25  // one client frame (usercmd)
	CmdsPerFrame = FrameMsec / CmdMsec
)

// DefaultMaxWaitFrames bounds WaitActive when the caller passes 0: 30 s of
// game time is far more than a handshake, load or level change needs.
const DefaultMaxWaitFrames = 300

// ErrNotActive is returned by WaitActive when the client did not become
// active within the frame budget.
var ErrNotActive = errors.New("session: client not active")

// ErrClosed is returned by operations on a closed session.
var ErrClosed = errors.New("session: closed")

// CmdFunc produces the usercmd of one 25 ms client frame. It is called
// CmdsPerFrame times per Step while the client is active, with the client
// state as of the latest server frame. The session sets the returned
// command's Msec to msec. Reliable commands (StringCmd) may be queued on c.
type CmdFunc func(c *fakeclient.Client, msec int) shared.UserCmd

// Session is what the agent drives: a connected client plus the few
// controls of the server it needs (reload the level-entry autosave, console
// commands for tests and tools).
type Session interface {
	// Start creates or joins the game and waits until the client is active.
	Start(ctx context.Context) error
	// Step advances one server frame (see CmdFunc; nil sends idle commands).
	Step(ctx context.Context, f CmdFunc) error
	// WaitActive steps with idle commands until the client is active on a
	// level, for at most maxFrames frames (0: DefaultMaxWaitFrames).
	WaitActive(ctx context.Context, maxFrames int) error
	// WaitLevel is WaitActive for a level generation after afterGen: after
	// a command that changes or reloads the level, pass the LevelGen from
	// before it (the client is still active on the old level at first).
	WaitLevel(ctx context.Context, afterGen, maxFrames int) error
	// Client returns the bot's client.
	Client() *fakeclient.Client
	// Reload loads the level-entry autosave "save0" (written by every
	// gamemap of a non-dedicated server) and waits until active again.
	Reload(ctx context.Context) error
	// Exec runs server console text (e.g. "gamemap demo2").
	Exec(text string) error
	// GameTimeMs is the session clock: simulated ms in Lockstep, wall ms
	// since Start in InProc.
	GameTimeMs() int64
	// Truth returns the game's level counters. METRICS ONLY: never feed it
	// to the bot.
	Truth() (Truth, error)
	// ReadFile reads static game data (BSP, MD2, ...) from the game's file
	// system.
	ReadFile(name string) ([]byte, error)
	// MapName is the current level as the client knows it
	// (fakeclient.Client.MapName).
	MapName() string
	// LevelGen is the client's level generation (fakeclient.LevelGen).
	LevelGen() int
	// Close ends the session (without sending "disconnect"). Later calls
	// that drive or query the game return ErrClosed.
	Close() error
}

// Truth is the in-process ground truth of the current level
// (game.LevelLocals counters). METRICS ONLY: the bot must never see it.
type Truth struct {
	Map            string
	FrameNum       int32
	KilledMonsters int32
	TotalMonsters  int32
	FoundSecrets   int32
	TotalSecrets   int32
	FoundGoals     int32
	TotalGoals     int32
}

func truthOf(ge game.Export) (Truth, error) {
	g, ok := ge.(*game.Game)
	if !ok || g == nil {
		return Truth{}, errors.New("session: no game module running")
	}
	l := g.Level()
	return Truth{
		Map: l.Mapname, FrameNum: l.Framenum,
		KilledMonsters: l.KilledMonsters, TotalMonsters: l.TotalMonsters,
		FoundSecrets: l.FoundSecrets, TotalSecrets: l.TotalSecrets,
		FoundGoals: l.FoundGoals, TotalGoals: l.TotalGoals,
	}, nil
}

// physicsCvars change movement, collision or view-kick physics that the
// agent's own simulation (nav graph, pmove rollouts, damage bearing) assumes
// to be the defaults, so bot games may not set them.
var physicsCvars = map[string]bool{
	"sv_gravity": true, "sv_airaccelerate": true, "sv_maxvelocity": true,
	"sv_rollspeed": true, "sv_rollangle": true,
	"run_pitch": true, "run_roll": true, "bob_up": true, "bob_pitch": true, "bob_roll": true,
	"gun_x": true, "gun_y": true, "gun_z": true,
}

// Spec describes a bot's single-player game.
type Spec struct {
	// Map is the start map (default "demo1").
	Map string
	// Skill is the skill cvar: 0 easy, 1 medium, 2 hard, 3 nightmare.
	Skill int
	// Cvars are further server cvars, limited to what an api.GameSpec may
	// set (host.ModeSettings) minus the physics cvars.
	Cvars map[string]string
}

// MapOrDefault returns Map or "demo1".
func (s Spec) MapOrDefault() string {
	if s.Map == "" {
		return "demo1"
	}
	return s.Map
}

// GameSpec validates the spec and returns the equivalent single-player
// api.GameSpec.
func (s Spec) GameSpec() (api.GameSpec, error) {
	if s.Skill < 0 || s.Skill > 3 {
		return api.GameSpec{}, fmt.Errorf("%w: skill %d not in 0..3", api.ErrGameInvalid, s.Skill)
	}
	if strings.ContainsAny(s.MapOrDefault(), " \t\n;\"$+*") {
		return api.GameSpec{}, fmt.Errorf("%w: bad map name %q", api.ErrGameInvalid, s.Map)
	}
	cv := map[string]string{"skill": strconv.Itoa(s.Skill)}
	keys := make([]string, 0, len(s.Cvars))
	for k := range s.Cvars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if physicsCvars[strings.ToLower(k)] {
			return api.GameSpec{}, fmt.Errorf("%w: physics cvar %q may not be set for a bot game", api.ErrGameInvalid, k)
		}
		if k == "skill" {
			return api.GameSpec{}, fmt.Errorf("%w: set Spec.Skill instead of the skill cvar", api.ErrGameInvalid)
		}
		cv[k] = s.Cvars[k]
	}
	return api.GameSpec{Mode: "sp", Map: s.MapOrDefault(), Cvars: cv}, nil
}

// ServerSettings returns the dedicated flag and the cvars of the spec's
// server (host.ModeSettings of its GameSpec).
func (s Spec) ServerSettings() (dedicated bool, cvars [][2]string, err error) {
	gs, err := s.GameSpec()
	if err != nil {
		return false, nil, err
	}
	return host.ModeSettings(gs)
}
