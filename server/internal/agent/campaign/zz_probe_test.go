package campaign

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/worldmodel"
)

func TestZZProbe(t *testing.T) {
	if os.Getenv("ZZ_PROBE") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	seed, _ := strconv.Atoi(os.Getenv("ZZ_SEED"))
	verbose := os.Getenv("ZZ_V") != ""
	last := int64(0)
	var pipe *decide.Pipeline
	stop, _ := strconv.Atoi(os.Getenv("ZZ_STOP"))
	res, _ := runEpisode(t, episodeSpec{lib: lib, seed: uint32(seed), stop: stop, start: os.Getenv("ZZ_START"), budget: 180 * time.Minute, onFrame: func(f *Frame) {
		if !verbose {
			return
		}
		b := f.Bot.Belief()
		s := &b.Self
		dmg := len(b.Damage) > 0 && b.Damage[len(b.Damage)-1].At == b.Time
		if f.GameMs-last < 500 && !dmg {
			return
		}
		last = f.GameMs
		in := f.Bot.Intent()
		line := fmt.Sprintf("%s t=%.1f pos=%.0f,%.0f,%.0f hp=%d ar=%d w=%q a=%d mode=%s tgt=%s mv=%s int=%s/%s/%s/%s/%s", f.Map, float64(f.LevelMs)/1000, s.Origin[0], s.Origin[1], s.Origin[2], s.Health, s.Armor, s.Weapon, s.Ammo, f.Bot.Mode(), f.Bot.Target(), f.Bot.Movement(), in.Mode, in.Target, in.FirePolicy, in.Movement, in.Pickup)
		for i := range b.Tracks {
			tr := &b.Tracks[i]
			if tr.Kind != "monster" || tr.Life != worldmodel.LifeAlive || b.Time-tr.LastUpdate > 3000 {
				continue
			}
			line += fmt.Sprintf(" | %s %s %.0f,%.0f,%.0f v=%v s=%v %s", tr.ID, tr.Class, tr.Pos[0], tr.Pos[1], tr.Pos[2], tr.Visible, tr.Shootable, tr.Awareness)
		}
		if pipe != nil && os.Getenv("ZZ_ST") != "" {
			ti := pipe.LastTick()
			for _, e := range ti.Fast.Enemies {
				line += fmt.Sprintf(" [%s u%d v%v s%v %s %s c%v]", e.ID, e.Units, e.Visible, e.Shootable, e.State, e.Threat, e.Current)
			}
		}
		if dmg {
			d := b.Damage[len(b.Damage)-1]
			line += fmt.Sprintf(" DMG %d/%d from %s %s", d.Health, d.Armor, d.Source, d.Cause)
		}
		t.Log(line)
	}, config: func(c *Config) {
		c.EntryCommands = nil
		c.MaxDeaths = 10
		if v, _ := strconv.Atoi(os.Getenv("ZZ_MAXD")); v > 0 {
			c.MaxDeaths = v
		}
		if v, _ := strconv.Atoi(os.Getenv("ZZ_LT")); v > 0 {
			c.LevelTimeout = time.Duration(v) * time.Minute
		}
		if v, _ := strconv.Atoi(os.Getenv("ZZ_FA")); v > 0 {
			c.FailAfter = time.Duration(v) * time.Minute
		}
		if verbose {
			c.Logf = t.Logf
		}
		p, err := bot.NewBrain(bot.BrainConfig{Seed: uint64(seed), Mode: decide.Lockstep})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		c.Bot.Policy = p
		pipe = p
	}})
	t.Logf("result %s %s victory %v deaths %d game %.0fs\n%s", res.Outcome, res.Reason, res.Victory, res.Deaths, float64(res.GameMs)/1000, res.Diagnostics)
}
