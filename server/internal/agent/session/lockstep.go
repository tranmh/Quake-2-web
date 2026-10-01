package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
)

// DefaultQport is the bot client's netchan qport when none is configured
// (fakeclient would otherwise seed it from the wall clock).
const DefaultQport = 1

// LockstepConfig configures a Lockstep session.
type LockstepConfig struct {
	// FS is the game data (required), e.g. the demo pak.
	FS sv.FileSystem
	// Spec is the single-player game to start.
	Spec Spec
	// Seed seeds the instance random generator (shared by engine and game).
	Seed uint32
	// StartCommand replaces the default start command "map <Spec.Map>"
	// (e.g. "demomap t.dm2" to play a recorded demo to the client).
	StartCommand string
	// Client configures the bot's client. Clock is always replaced by the
	// session clock and a zero Qport becomes DefaultQport. Long runs should
	// set MaxHistory (the client's event histories are unbounded by default)
	// and read new events with fakeclient.NewSince and Client.Counts.
	Client fakeclient.Options
	// Maps is an optional shared map cache.
	Maps *sv.MapCache
	// Saves stores the savegames (nil: a fresh in-memory store).
	Saves sv.SaveStore
	// Printf receives the server console output (nil discards it).
	Printf func(format string, args ...any)
	// Now is the wall time savegame comments report (default a fixed
	// instant, so saves are deterministic too).
	Now time.Time
	// MaxWaitFrames bounds the handshake of Start and Reload
	// (0: DefaultMaxWaitFrames).
	MaxWaitFrames int
}

// Lockstep runs an sv.Server and the bot's client in the calling goroutine
// on a virtual clock. Each Step is one server frame:
//
//  1. while the client is active, CmdsPerFrame usercmds of CmdMsec ms each
//     are produced by the CmdFunc and sent (SendCmd);
//  2. the client's datagrams are handed to the server (HandlePacket) and
//     connectionless replies back to the client, until both are drained;
//  3. the clock advances FrameMsec and the server runs SV_Frame;
//  4. the server's datagrams are fed to the client (Feed), the client runs
//     its connection timers (Tick) and the queues are drained again.
//
// The client's address is "loopback" with an explicit qport, so the run
// depends only on the seed, the spec and the commands. A Lockstep is not
// safe for concurrent use.
type Lockstep struct {
	cfg   LockstepConfig
	srv   *sv.Server
	cli   *fakeclient.Client
	addr  qnet.Addr
	clock int // virtual Sys_Milliseconds of server and client

	toServer [][]byte // client -> server datagrams
	toClient [][]byte // server -> client datagrams
	closed   bool
}

var _ Session = (*Lockstep)(nil)

// NewLockstep returns an unstarted Lockstep session.
func NewLockstep(cfg LockstepConfig) *Lockstep {
	return &Lockstep{cfg: cfg, addr: qnet.Addr{Base: "loopback", Port: 1}}
}

// lockConn is the client end of the in-process datagram loop.
type lockConn struct{ l *Lockstep }

func (c lockConn) Send(data []byte) error {
	c.l.toServer = append(c.l.toServer, append([]byte(nil), data...))
	return nil
}

func (c lockConn) Recv(context.Context) ([]byte, error) {
	return nil, errors.New("session: lockstep datagrams are delivered by Step (use Step/WaitActive, not Poll)")
}

func (c lockConn) Close() error { return nil }

// lockSender is the server end: datagrams the server sends to the client.
type lockSender struct{ l *Lockstep }

func (s lockSender) SendPacket(_ qnet.Addr, data []byte) error {
	s.l.toClient = append(s.l.toClient, append([]byte(nil), data...))
	return nil
}

func (l *Lockstep) now() int { return l.clock }

// Start creates the server, runs the start command and connects the client.
func (l *Lockstep) Start(ctx context.Context) error {
	if l.closed {
		return ErrClosed
	}
	if l.srv != nil {
		return errors.New("session: lockstep already started")
	}
	if l.cfg.FS == nil {
		return errors.New("session: lockstep needs a file system")
	}
	dedicated, cvars, err := l.cfg.Spec.ServerSettings()
	if err != nil {
		return err
	}
	stamp := l.cfg.Now
	if stamp.IsZero() {
		stamp = time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	}
	rng := crand.New(l.cfg.Seed)
	l.srv = sv.New(sv.Config{
		FS:        l.cfg.FS,
		Game:      host.RealGame(rng),
		Maps:      l.cfg.Maps,
		Saves:     l.cfg.Saves,
		Rand:      rng,
		Printf:    l.cfg.Printf,
		Clock:     l.now,
		Now:       func() time.Time { return stamp },
		Dedicated: dedicated,
		Cvars:     cvars,
	})
	start := l.cfg.StartCommand
	if start == "" {
		start = "map " + l.cfg.Spec.MapOrDefault()
	}
	if err := l.srv.ExecuteText(start + "\n"); err != nil {
		return fmt.Errorf("session: %s: %w", start, err)
	}
	if !l.srv.SVS.Initialized {
		return fmt.Errorf("session: %q did not start a server", start)
	}
	opt := l.cfg.Client
	opt.Clock = l.now
	opt.Passive = false
	if opt.Qport == 0 {
		opt.Qport = DefaultQport
	}
	l.cli = fakeclient.New(lockConn{l}, opt)
	l.cli.BeginConnect()
	return l.WaitActive(ctx, l.cfg.MaxWaitFrames)
}

func (l *Lockstep) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.closed {
		return ErrClosed
	}
	if l.srv == nil {
		return errors.New("session: lockstep not started")
	}
	if l.srv.Killed() {
		return errors.New("session: server stopped")
	}
	return nil
}

// Step advances one server frame (see Lockstep). f is called only while the
// client is active; nil sends idle usercmds.
func (l *Lockstep) Step(ctx context.Context, f CmdFunc) error {
	if err := l.check(ctx); err != nil {
		return err
	}
	c := l.cli
	if c.State == fakeclient.CaActive {
		for i := 0; i < CmdsPerFrame; i++ {
			var cmd shared.UserCmd
			if f != nil {
				cmd = f(c, CmdMsec)
			}
			cmd.Msec = CmdMsec
			c.SendCmd(cmd)
		}
	}
	if err := l.exchange(); err != nil {
		return err
	}
	if err := l.frame(); err != nil {
		return err
	}
	if err := l.exchange(); err != nil {
		return err
	}
	if err := c.Tick(); err != nil {
		return err
	}
	return l.exchange()
}

// exchange hands queued datagrams to the server and the client until both
// queues are empty (connectionless replies are immediate).
func (l *Lockstep) exchange() error {
	for round := 0; len(l.toServer) > 0 || len(l.toClient) > 0; round++ {
		if round == 64 {
			return errors.New("session: datagram ping-pong does not settle")
		}
		up := l.toServer
		l.toServer = nil
		for _, d := range up {
			if err := l.srv.HandlePacket(0, qnet.Packet{From: l.addr, Via: lockSender{l}, Data: d}); err != nil {
				return fmt.Errorf("session: server: %w", err)
			}
		}
		down := l.toClient
		l.toClient = nil
		for _, d := range down {
			if err := l.cli.Feed(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// frame runs one server frame. The first SV_Frame after a level spawn only
// lowclamps the server's realtime (sv.time starts at 1000) and runs no game
// frame; it is followed at once by the frame it scheduled, so every Step
// advances the game by one frame.
func (l *Lockstep) frame() error {
	spawn, num := l.srv.SVS.SpawnCount, l.srv.SV.FrameNum
	l.clock += FrameMsec
	sleep, err := l.srv.Frame(FrameMsec)
	if err == nil && l.srv.SVS.Initialized && sleep > 0 &&
		(l.srv.SVS.SpawnCount != spawn || l.srv.SV.FrameNum == num) {
		l.clock += sleep
		_, err = l.srv.Frame(sleep)
	}
	if err != nil {
		return fmt.Errorf("session: server frame: %w", err)
	}
	if l.srv.Killed() {
		return errors.New("session: server stopped")
	}
	return nil
}

// WaitActive steps until the client is active (at most maxFrames frames,
// 0: DefaultMaxWaitFrames).
func (l *Lockstep) WaitActive(ctx context.Context, maxFrames int) error {
	return l.WaitLevel(ctx, -1, maxFrames)
}

// WaitLevel steps until the client is active on a level generation after
// afterGen (at most maxFrames frames, 0: DefaultMaxWaitFrames): after a
// command that changes or reloads the level, pass the LevelGen from before.
func (l *Lockstep) WaitLevel(ctx context.Context, afterGen, maxFrames int) error {
	if err := l.check(ctx); err != nil {
		return err
	}
	return waitLevel(ctx, l, afterGen, maxFrames)
}

func waitLevel(ctx context.Context, s Session, afterGen, maxFrames int) error {
	if maxFrames <= 0 {
		maxFrames = DefaultMaxWaitFrames
	}
	c := s.Client()
	for i := 0; ; i++ {
		if c.State == fakeclient.CaActive && c.LevelGen() > afterGen {
			return nil
		}
		if i == maxFrames {
			return fmt.Errorf("%w after %d frames (state %d, level %d, %q)",
				ErrNotActive, maxFrames, c.State, c.LevelGen(), c.MapName())
		}
		if err := s.Step(ctx, nil); err != nil {
			return err
		}
	}
}

// Client returns the bot's client.
func (l *Lockstep) Client() *fakeclient.Client { return l.cli }

// Server returns the server, for tests and tools only: agent code must see
// the game through Client.
func (l *Lockstep) Server() *sv.Server { return l.srv }

// Reload loads "save0", the autosave gamemap writes on every level entry of
// a non-dedicated server, and waits until the client is active on the
// reloaded level. The load restarts the game (svc_reconnect), so the client
// runs a full new handshake.
func (l *Lockstep) Reload(ctx context.Context) error {
	if err := l.check(ctx); err != nil {
		return err
	}
	gen, spawn := l.cli.LevelGen(), l.srv.SVS.SpawnCount
	if err := l.srv.ExecuteText("load save0\n"); err != nil {
		return fmt.Errorf("session: load save0: %w", err)
	}
	if l.srv.SVS.SpawnCount == spawn {
		return errors.New("session: load save0 did not start a level")
	}
	return l.WaitLevel(ctx, gen, l.cfg.MaxWaitFrames)
}

// Exec runs server console text.
func (l *Lockstep) Exec(text string) error {
	if err := l.check(context.Background()); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := l.srv.ExecuteText(text); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// GameTimeMs returns the virtual clock (ms simulated since Start).
func (l *Lockstep) GameTimeMs() int64 { return int64(l.clock) }

// Truth returns the level counters of the running game. METRICS ONLY.
func (l *Lockstep) Truth() (Truth, error) {
	if err := l.check(context.Background()); err != nil {
		return Truth{}, err
	}
	return truthOf(l.srv.Game())
}

// ReadFile reads static game data from the configured file system. It
// works before Start and after Close: the data is static.
func (l *Lockstep) ReadFile(name string) ([]byte, error) {
	if l.cfg.FS == nil {
		return nil, errors.New("session: lockstep has no file system")
	}
	return l.cfg.FS.ReadFile(name)
}

// MapName returns the client's current level (fakeclient.Client.MapName).
func (l *Lockstep) MapName() string {
	if l.cli == nil {
		return ""
	}
	return l.cli.MapName()
}

// LevelGen returns the client's level generation.
func (l *Lockstep) LevelGen() int {
	if l.cli == nil {
		return 0
	}
	return l.cli.LevelGen()
}

// Close shuts the server down. The client sends no "disconnect".
func (l *Lockstep) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true
	if l.srv != nil && !l.srv.Killed() {
		l.srv.Shutdown("Server quit.\n")
	}
	l.toServer, l.toClient = nil, nil
	return nil
}
