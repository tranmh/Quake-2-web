package campaign

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
)

// TestZZDemo3 plays demo1 and demo2a, then demo3 on the same session (a
// second episode starting on the current level) with a verbose log.
func TestZZDemo3(t *testing.T) {
	if os.Getenv("ZZ_D3") == "" {
		t.Skip()
	}
	seed, _ := strconv.Atoi(os.Getenv("ZZ_SEED"))
	if seed == 0 {
		seed = 1
	}
	verbose := os.Getenv("ZZ_V") != ""
	fs := sessiontest.DemoFS(t)
	lib := demoLibrary(t)
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: uint32(seed), Client: fakeclient.Options{MaxHistory: 256}})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	brain := func() bot.Policy {
		p, err := bot.NewBrain(bot.BrainConfig{Seed: uint64(seed), Mode: decide.Lockstep})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		return p
	}
	cfg := Config{Campaign: demoCampaign(t), Library: lib, StopAfter: 2, Seed: uint64(seed), Bot: bot.Config{Policy: brain()}}
	res, err := Run(ctx, l, cfg)
	if err != nil || res.Outcome != OutcomeCompleted {
		t.Fatalf("to demo3: %v %+v", err, res)
	}
	t.Logf("reached %s at %.0fs", l.MapName(), float64(l.GameTimeMs())/1000)
	last := int64(0)
	cfg = Config{Campaign: demoCampaign(t), Library: lib, StopAfter: 1, Seed: uint64(seed), MaxDeaths: 8, Visits: map[string]int{"demo1": 1, "demo2": 1},
		Bot: bot.Config{Policy: brain()}, LevelTimeout: 15 * time.Minute, Bus: trace.NewBus("d3", nil)}
	if verbose {
		cfg.Logf = t.Logf
		cfg.OnFrame = func(f *Frame) {
			b := f.Bot.Belief()
			s := &b.Self
			dmg := len(b.Damage) > 0 && b.Damage[len(b.Damage)-1].At == b.Time
			if f.GameMs-last < 500 && !dmg {
				return
			}
			last = f.GameMs
			in := f.Bot.Intent()
			line := fmt.Sprintf("t=%.1f pos=%.0f,%.0f,%.0f hp=%d ar=%d w=%q a=%d mode=%s tgt=%s mv=%s int=%s/%s/%s/%s/%s", float64(f.LevelMs)/1000, s.Origin[0], s.Origin[1], s.Origin[2], s.Health, s.Armor, s.Weapon, s.Ammo, f.Bot.Mode(), f.Bot.Target(), f.Bot.Movement(), in.Mode, in.Target, in.FirePolicy, in.Movement, in.Pickup)
			for i := range b.Tracks {
				tr := &b.Tracks[i]
				if tr.Kind != "monster" || tr.Life != worldmodel.LifeAlive || b.Time-tr.LastUpdate > 3000 {
					continue
				}
				line += fmt.Sprintf(" | %s %s %.0f,%.0f,%.0f v=%v s=%v %s", tr.ID, tr.Class, tr.Pos[0], tr.Pos[1], tr.Pos[2], tr.Visible, tr.Shootable, tr.Awareness)
			}
			if dmg {
				d := b.Damage[len(b.Damage)-1]
				line += fmt.Sprintf(" DMG %d/%d from %s %s", d.Health, d.Armor, d.Source, d.Cause)
			}
			t.Log(line)
		}
	}
	res, err = Run(ctx, l, cfg)
	t.Logf("demo3: %v %s %s deaths %d game %.0fs\n%s", err, res.Outcome, res.Reason, res.Deaths, float64(res.GameMs)/1000, res.Diagnostics)
}
