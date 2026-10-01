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

// InProcConfig configures an InProc session.
type InProcConfig struct {
	// FS is the instance's file system (for ReadFile).
	FS sv.FileSystem
	// Client configures the bot's client (a zero Qport becomes
	// DefaultQport; the wall clock is used). Long runs should set
	// MaxHistory (see LockstepConfig.Client).
	Client fakeclient.Options
	// Timeout bounds the handshake of Start (default 20 s).
	Timeout time.Duration
}

// InProc is a realtime session on a host.Instance: the bot connects as
// "loopback" over an in-memory connection and Step paces the four usercmds
// of a frame 25 ms of wall time apart. Closing the session closes the
// connection (the host drops the client); it never sends "disconnect".
// An InProc is not safe for concurrent use.
type InProc struct {
	inst   *host.Instance
	cfg    InProcConfig
	conn   qnet.Conn
	cli    *fakeclient.Client
	start  time.Time
	next   time.Time // when the next client frame is due
	closed bool
}

var _ Session = (*InProc)(nil)

// NewInProc returns an unstarted session on inst.
func NewInProc(inst *host.Instance, cfg InProcConfig) *InProc {
	return &InProc{inst: inst, cfg: cfg}
}

// InstanceConfig describes a realtime single-player instance for InProc.
type InstanceConfig struct {
	ID     string // instance id ("" picks a random one)
	FS     sv.FileSystem
	Spec   Spec
	Seed   uint32
	Maps   *sv.MapCache
	Printf func(format string, args ...any)
}

// NewInstance creates a single-player instance on h running the real game
// on cfg.Spec.Map (the realtime counterpart of a Lockstep server).
func NewInstance(h *host.Host, cfg InstanceConfig) (*host.Instance, error) {
	dedicated, cvars, err := cfg.Spec.ServerSettings()
	if err != nil {
		return nil, err
	}
	rng := crand.New(cfg.Seed)
	return h.Create(host.InstanceConfig{
		ID: cfg.ID,
		Server: sv.Config{
			FS:        cfg.FS,
			Game:      host.RealGame(rng),
			Maps:      cfg.Maps,
			Rand:      rng,
			Dedicated: dedicated,
			Cvars:     cvars,
			Printf:    cfg.Printf,
		},
		Commands: []string{"map " + cfg.Spec.MapOrDefault()},
	})
}

// Start connects to the instance and waits until the client is active.
func (p *InProc) Start(ctx context.Context) error {
	if p.closed {
		return ErrClosed
	}
	if p.cli != nil {
		return errors.New("session: inproc already started")
	}
	opt := p.cfg.Client
	opt.Clock = nil
	opt.Passive = false
	if opt.Qport == 0 {
		opt.Qport = DefaultQport
	}
	p.conn = p.inst.ConnectMem("loopback")
	p.cli = fakeclient.New(p.conn, opt)
	timeout := p.cfg.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := p.cli.Handshake(hctx); err != nil {
		return fmt.Errorf("session: handshake: %w", err)
	}
	p.start = time.Now()
	p.next = p.start
	return nil
}

// check returns why the session cannot be driven: ctx is done, the session
// was closed (or its instance stopped) or it was never started.
func (p *InProc) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed {
		return ErrClosed
	}
	if p.cli == nil {
		return errors.New("session: inproc not started")
	}
	select {
	case <-p.inst.Done():
		return ErrClosed
	default:
	}
	return nil
}

// Step runs CmdsPerFrame client frames of CmdMsec ms wall time each: while
// active, a usercmd from f (nil: idle) is sent, then datagrams are received
// until the next client frame is due. A Step that started late does not
// try to catch up.
func (p *InProc) Step(ctx context.Context, f CmdFunc) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	c := p.cli
	for i := 0; i < CmdsPerFrame; i++ {
		if c.State == fakeclient.CaActive {
			var cmd shared.UserCmd
			if f != nil {
				cmd = f(c, CmdMsec)
			}
			cmd.Msec = CmdMsec
			c.SendCmd(cmd)
		}
		now := time.Now()
		p.next = p.next.Add(CmdMsec * time.Millisecond)
		if p.next.Before(now) {
			p.next = now.Add(time.Millisecond) // late: resynchronize
		}
		if err := c.Poll(ctx, time.Until(p.next)); err != nil {
			return err
		}
	}
	return nil
}

// WaitActive steps until the client is active (at most maxFrames frames).
func (p *InProc) WaitActive(ctx context.Context, maxFrames int) error {
	return p.WaitLevel(ctx, -1, maxFrames)
}

// WaitLevel steps until the client is active on a level generation after
// afterGen (at most maxFrames frames).
func (p *InProc) WaitLevel(ctx context.Context, afterGen, maxFrames int) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	return waitLevel(ctx, p, afterGen, maxFrames)
}

// Client returns the bot's client.
func (p *InProc) Client() *fakeclient.Client { return p.cli }

// Instance returns the host instance.
func (p *InProc) Instance() *host.Instance { return p.inst }

// Reload loads "save0" inside the instance goroutine and waits until the
// client is active on the reloaded level.
func (p *InProc) Reload(ctx context.Context) error {
	if err := p.check(ctx); err != nil {
		return err
	}
	gen := p.cli.LevelGen()
	var err error
	started := false
	if derr := p.inst.Do(func(s *sv.Server) {
		spawn := s.SVS.SpawnCount
		err = s.ExecuteText("load save0\n")
		started = s.SVS.SpawnCount != spawn
	}); derr != nil {
		return derr
	}
	if err != nil {
		return fmt.Errorf("session: load save0: %w", err)
	}
	if !started {
		return errors.New("session: load save0 did not start a level")
	}
	return p.WaitLevel(ctx, gen, 0)
}

// Exec runs server console text inside the instance goroutine.
func (p *InProc) Exec(text string) error {
	if err := p.check(context.Background()); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	var err error
	if derr := p.inst.Do(func(s *sv.Server) { err = s.ExecuteText(text) }); derr != nil {
		return derr
	}
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// GameTimeMs returns the wall ms since Start.
func (p *InProc) GameTimeMs() int64 {
	if p.start.IsZero() {
		return 0
	}
	return time.Since(p.start).Milliseconds()
}

// Truth returns the level counters of the running game. METRICS ONLY.
func (p *InProc) Truth() (Truth, error) {
	if err := p.check(context.Background()); err != nil {
		return Truth{}, err
	}
	var t Truth
	var err error
	if derr := p.inst.Do(func(s *sv.Server) { t, err = truthOf(s.Game()) }); derr != nil {
		return Truth{}, derr
	}
	return t, err
}

// ReadFile reads static game data from the configured file system.
func (p *InProc) ReadFile(name string) ([]byte, error) {
	if p.cfg.FS == nil {
		return nil, errors.New("session: inproc has no file system")
	}
	return p.cfg.FS.ReadFile(name)
}

// MapName returns the client's current level (fakeclient.Client.MapName).
func (p *InProc) MapName() string {
	if p.cli == nil {
		return ""
	}
	return p.cli.MapName()
}

// LevelGen returns the client's level generation.
func (p *InProc) LevelGen() int {
	if p.cli == nil {
		return 0
	}
	return p.cli.LevelGen()
}

// Close closes the connection; the host drops the client. The instance
// itself is left running (its owner stops it). Step, Reload, Exec and
// Truth return ErrClosed afterwards.
func (p *InProc) Close() error {
	p.closed = true
	if p.conn == nil {
		return nil
	}
	err := p.conn.Close()
	p.conn = nil
	return err
}
