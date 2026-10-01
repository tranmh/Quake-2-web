package session_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
	"quake2web/server/internal/q2const"
)

func TestInProcSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	fs := sessiontest.DemoFS(t)
	h := host.New()
	inst, err := session.NewInstance(h, session.InstanceConfig{
		ID: "bot-test", FS: fs, Spec: session.Spec{Skill: 1}, Seed: 1,
		Printf: func(f string, a ...any) { t.Logf("sv: "+strings.TrimRight(f, "\n"), a...) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Stop)

	p := session.NewInProc(inst, session.InProcConfig{FS: fs})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c := p.Client()
	if c.State != fakeclient.CaActive || p.MapName() != "demo1" || p.LevelGen() != 1 {
		t.Fatalf("state %d map %q gen %d", c.State, p.MapName(), p.LevelGen())
	}
	start, f0 := c.Origin(), c.Frame.ServerFrame
	t0 := time.Now()
	walker := sessiontest.Walker()
	for i := 0; i < 15; i++ {
		if err := p.Step(ctx, walker); err != nil {
			t.Fatal(err)
		}
	}
	wall := time.Since(t0)
	frames := c.Frame.ServerFrame - f0
	t.Logf("15 steps in %v, %d server frames, moved %v -> %v", wall, frames, start, c.Origin())
	if wall < 1200*time.Millisecond || wall > 4*time.Second {
		t.Errorf("15 realtime steps took %v", wall)
	}
	if frames < 8 || frames > 25 {
		t.Errorf("%d server frames in 1.5 s", frames)
	}
	if c.Origin() == start {
		t.Error("did not move")
	}
	if ms := p.GameTimeMs(); ms < 1000 {
		t.Errorf("game time %d ms", ms)
	}
	if tr, err := p.Truth(); err != nil || tr.Map != "demo1" {
		t.Errorf("truth %+v %v", tr, err)
	}
	if b, err := p.ReadFile("maps/demo1.bsp"); err != nil || len(b) == 0 {
		t.Errorf("ReadFile %v", err)
	}

	// the autosave of the level entry reloads in realtime as well
	gen := p.LevelGen()
	if err := p.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if p.LevelGen() <= gen || c.State != fakeclient.CaActive || p.MapName() != "demo1" ||
		c.Frame.PlayerState.Stats[q2const.STAT_HEALTH] != 100 {
		t.Fatalf("after reload: gen %d state %d map %q", p.LevelGen(), c.State, p.MapName())
	}

	// closing the session drops the client (no disconnect is sent)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for inst.Stats().Players != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := inst.Stats().Players; n != 0 {
		t.Fatalf("%d players after close", n)
	}
	// a closed session refuses to drive or query the still running instance
	if err := p.Step(ctx, walker); !errors.Is(err, session.ErrClosed) {
		t.Errorf("step after close: %v", err)
	}
	if err := p.Exec("echo hi"); !errors.Is(err, session.ErrClosed) {
		t.Errorf("exec after close: %v", err)
	}
	if err := p.Reload(ctx); !errors.Is(err, session.ErrClosed) {
		t.Errorf("reload after close: %v", err)
	}
	if _, err := p.Truth(); !errors.Is(err, session.ErrClosed) {
		t.Errorf("truth after close: %v", err)
	}
	if err := p.WaitActive(ctx, 1); !errors.Is(err, session.ErrClosed) {
		t.Errorf("wait after close: %v", err)
	}
}
