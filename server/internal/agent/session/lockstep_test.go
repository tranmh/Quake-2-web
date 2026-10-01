package session_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/api"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
)

// startLockstep starts a lockstep session on the demo pak.
func startLockstep(t *testing.T, cfg session.LockstepConfig) *session.Lockstep {
	t.Helper()
	if cfg.FS == nil {
		cfg.FS = sessiontest.DemoFS(t)
	}
	l := session.NewLockstep(cfg)
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func steps(t *testing.T, s session.Session, n int, f session.CmdFunc) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		if err := s.Step(ctx, f); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
}

func TestLockstepHandshake(t *testing.T) {
	t0 := time.Now()
	l := startLockstep(t, session.LockstepConfig{Spec: session.Spec{Skill: 1}, Seed: 7})
	wall := time.Since(t0)
	c := l.Client()
	if c.State != fakeclient.CaActive || !c.Frame.Valid {
		t.Fatalf("not active: state %d", c.State)
	}
	if wall > 2*time.Second {
		t.Errorf("handshake took %v of wall time", wall)
	}
	if got := l.MapName(); got != "demo1" {
		t.Errorf("map %q", got)
	}
	if l.LevelGen() != 1 || c.ServerData.PlayerNum != 0 || c.ServerData.AttractLoop != 0 {
		t.Errorf("serverdata %+v gen %d", c.ServerData, l.LevelGen())
	}
	if c.ConfigStrings[q2const.CS_AIRACCEL] != "0" {
		t.Errorf("airaccel %q", c.ConfigStrings[q2const.CS_AIRACCEL])
	}
	if c.Frame.PlayerState.Stats[q2const.STAT_HEALTH] != 100 {
		t.Errorf("health %d", c.Frame.PlayerState.Stats[q2const.STAT_HEALTH])
	}
	// the handshake took a handful of server frames of virtual time
	if ms := l.GameTimeMs(); ms <= 0 || ms > 3000 {
		t.Errorf("handshake took %d ms of game time", ms)
	}
	tr, err := l.Truth()
	if err != nil || tr.Map != "demo1" || tr.TotalMonsters == 0 {
		t.Errorf("truth %+v %v", tr, err)
	}
	if b, err := l.ReadFile("maps/demo1.bsp"); err != nil || len(b) < 1000 {
		t.Errorf("ReadFile: %d bytes, %v", len(b), err)
	}
	// every Step advances the game by exactly one frame
	f0 := c.Frame.ServerFrame
	steps(t, l, 10, nil)
	if got := c.Frame.ServerFrame - f0; got != 10 {
		t.Errorf("10 steps advanced %d server frames", got)
	}
}

// walkDigest runs a walker for n frames and hashes every server message the
// client accepted (from the first one of the handshake on).
func walkDigest(t *testing.T, seed uint32, n int) (string, shared.Vec3, int) {
	t.Helper()
	h := sha256.New()
	msgs := 0
	l := startLockstep(t, session.LockstepConfig{
		Spec: session.Spec{Skill: 1}, Seed: seed,
		Client: fakeclient.Options{OnServerMessage: func(_ *fakeclient.Client, payload []byte, _ []fakeclient.Span) {
			fmt.Fprintf(h, "%d:", len(payload))
			h.Write(payload)
			msgs++
		}},
	})
	steps(t, l, n, sessiontest.Walker())
	return hex.EncodeToString(h.Sum(nil)), l.Client().Origin(), msgs
}

func TestLockstepDeterminism(t *testing.T) {
	const frames = 300 // 30 s of game time
	a, oa, na := walkDigest(t, 1234, frames)
	b, ob, nb := walkDigest(t, 1234, frames)
	t.Logf("digest %s over %d messages, end origin %v", a, na, oa)
	if a != b || oa != ob || na != nb {
		t.Fatalf("runs differ: %s (%d msgs, %v) vs %s (%d msgs, %v)", a, na, oa, b, nb, ob)
	}
	if na < frames {
		t.Fatalf("only %d messages for %d frames", na, frames)
	}
	// a different seed changes the game (monster AI draws random numbers)
	c, _, _ := walkDigest(t, 99, frames)
	if c == a {
		t.Fatal("seed has no effect")
	}
}

func TestLockstepSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	l := startLockstep(t, session.LockstepConfig{Spec: session.Spec{Skill: 1}, Seed: 3})
	const frames = 300
	t0 := time.Now()
	steps(t, l, frames, sessiontest.Walker())
	wall := time.Since(t0)
	speed := float64(frames*session.FrameMsec) / float64(wall.Milliseconds()+1)
	t.Logf("%d frames in %v: %.1fx realtime", frames, wall, speed)
	min := 10.0
	if raceEnabled {
		min = 5 // the race detector slows the game code down several times
	}
	if speed < min {
		t.Fatalf("lockstep ran at %.1fx realtime, want >= %.0fx", speed, min)
	}
}

func TestLockstepReload(t *testing.T) {
	l := startLockstep(t, session.LockstepConfig{Spec: session.Spec{Skill: 1}, Seed: 5})
	c := l.Client()
	steps(t, l, 60, sessiontest.Walker()) // Cmd_Kill_f refuses within 5 s of the spawn
	moved := c.Origin()
	c.StringCmd("kill")
	for i := 0; i < 30 && c.Frame.PlayerState.Stats[q2const.STAT_HEALTH] > 0; i++ {
		steps(t, l, 1, nil)
	}
	if hp := c.Frame.PlayerState.Stats[q2const.STAT_HEALTH]; hp > 0 {
		t.Fatalf("kill: health %d", hp)
	}
	steps(t, l, 15, nil) // lie dead for a while like the campaign does
	gen := l.LevelGen()
	if err := l.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l.LevelGen() <= gen || c.State != fakeclient.CaActive || l.MapName() != "demo1" {
		t.Fatalf("after reload: gen %d (was %d) state %d map %q", l.LevelGen(), gen, c.State, l.MapName())
	}
	if hp := c.Frame.PlayerState.Stats[q2const.STAT_HEALTH]; hp != 100 {
		t.Fatalf("health after reload %d", hp)
	}
	if c.Origin() == moved {
		t.Fatal("reload did not return to the level start")
	}
	steps(t, l, 10, sessiontest.Walker()) // and play on
}

func TestLockstepLevelChange(t *testing.T) {
	l := startLockstep(t, session.LockstepConfig{Spec: session.Spec{Skill: 1}, Seed: 9})
	steps(t, l, 5, nil)
	gen := l.LevelGen()
	if err := l.Exec("gamemap demo2"); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitLevel(context.Background(), gen, 0); err != nil {
		t.Fatal(err)
	}
	c := l.Client()
	if l.MapName() != "demo2" || l.LevelGen() != gen+1 || c.ConfigStrings[q2const.CS_MODELS+1] != "maps/demo2.bsp" {
		t.Fatalf("map %q gen %d", l.MapName(), l.LevelGen())
	}
	if tr, err := l.Truth(); err != nil || tr.Map != "demo2" {
		t.Fatalf("truth %+v %v", tr, err)
	}
	steps(t, l, 10, sessiontest.Walker())
	// the level entry wrote the autosave for demo2: a reload stays there
	if err := l.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l.MapName() != "demo2" {
		t.Fatalf("reload went to %q", l.MapName())
	}
}

func TestLockstepErrors(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	ctx := context.Background()
	if err := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "nosuchmap"}}).Start(ctx); err == nil {
		t.Error("missing map started")
	}
	err := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Cvars: map[string]string{"sv_gravity": "100"}}}).Start(ctx)
	if !errors.Is(err, api.ErrGameInvalid) || !strings.Contains(err.Error(), "physics") {
		t.Errorf("physics cvar: %v", err)
	}
	// static data needs no server; a session without a file system errors
	if b, err := session.NewLockstep(session.LockstepConfig{FS: fs}).ReadFile("maps/demo1.bsp"); err != nil || len(b) == 0 {
		t.Errorf("ReadFile before Start: %v", err)
	}
	if _, err := session.NewLockstep(session.LockstepConfig{}).ReadFile("maps/demo1.bsp"); err == nil {
		t.Error("ReadFile without a file system")
	}
	unstarted := session.NewLockstep(session.LockstepConfig{FS: fs})
	if _, err := unstarted.Truth(); err == nil || errors.Is(err, session.ErrClosed) {
		t.Errorf("truth before start: %v", err)
	}
	_ = unstarted.Close()
	if err := unstarted.Start(ctx); !errors.Is(err, session.ErrClosed) {
		t.Errorf("start after close: %v", err)
	}

	l := startLockstep(t, session.LockstepConfig{FS: fs})
	if err := l.Start(ctx); err == nil {
		t.Error("started twice")
	}
	_ = l.Close()
	if err := l.Step(ctx, nil); !errors.Is(err, session.ErrClosed) {
		t.Errorf("step after close: %v", err)
	}
	if err := l.Exec("echo hi"); !errors.Is(err, session.ErrClosed) {
		t.Errorf("exec after close: %v", err)
	}
	if err := l.Reload(ctx); !errors.Is(err, session.ErrClosed) {
		t.Errorf("reload after close: %v", err)
	}
	if err := l.WaitActive(ctx, 1); !errors.Is(err, session.ErrClosed) {
		t.Errorf("wait after close: %v", err)
	}
	if _, err := l.Truth(); !errors.Is(err, session.ErrClosed) {
		t.Errorf("truth after close: %v", err)
	}
}

func TestLockstepStepsThroughNonActiveStates(t *testing.T) {
	// a level change triggered inside a frame (the game's changelevel goes
	// through the command buffer) leaves the client connecting; Step keeps
	// going and WaitLevel finds the new level
	l := startLockstep(t, session.LockstepConfig{Spec: session.Spec{Skill: 1}})
	gen := l.LevelGen()
	l.Server().Cmd.Cbuf_AddText("gamemap demo3\n")
	steps(t, l, 1, sessiontest.Walker())
	if err := l.WaitLevel(context.Background(), gen, 0); err != nil {
		t.Fatal(err)
	}
	if l.MapName() != "demo3" {
		t.Fatalf("map %q", l.MapName())
	}
	var _ *sv.Server = l.Server()
}
