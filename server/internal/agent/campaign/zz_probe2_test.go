package campaign

import (
	"fmt"
	"os"
	"testing"
	"time"

	"quake2web/server/internal/agent/worldmodel"
)

func TestZZProbe2(t *testing.T) {
	if os.Getenv("ZZ_PROBE2") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	last := int64(0)
	res, _ := runEpisode(t, episodeSpec{lib: lib, start: os.Getenv("ZZ_START"), stop: 1, budget: 4 * time.Minute, onFrame: func(f *Frame) {
		b := f.Bot.Belief()
		s := &b.Self
		if f.GameMs-last < 500 && !(len(b.Damage) > 0 && b.Damage[len(b.Damage)-1].At == b.Time) {
			return
		}
		last = f.GameMs
		line := fmt.Sprintf("t=%.1f pos=%.0f,%.0f,%.0f hp=%d ar=%d w=%q ammo=%d mode=%s tgt=%s int=%s/%s/%s", float64(f.LevelMs)/1000, s.Origin[0], s.Origin[1], s.Origin[2], s.Health, s.Armor, s.Weapon, s.Ammo, f.Bot.Mode(), f.Bot.Target(), f.Bot.Intent().Mode, f.Bot.Intent().FirePolicy, f.Bot.Intent().Movement)
		for i := range b.Tracks {
			tr := &b.Tracks[i]
			if tr.Kind != "monster" || tr.Life != worldmodel.LifeAlive || b.Time-tr.LastUpdate > 3000 {
				continue
			}
			line += fmt.Sprintf(" | %s %s %.0f,%.0f,%.0f vis=%v sh=%v %s", tr.ID, tr.Class, tr.Pos[0], tr.Pos[1], tr.Pos[2], tr.Visible, tr.Shootable, tr.Awareness)
		}
		if n := len(b.Damage); n > 0 && b.Damage[n-1].At == b.Time {
			d := b.Damage[n-1]
			line += fmt.Sprintf(" DMG %d/%d from %s cause %s", d.Health, d.Armor, d.Source, d.Cause)
		}
		t.Log(line)
	}, config: func(c *Config) {
		c.EntryCommands = nil
		c.MaxDeaths = 1
	}})
	t.Logf("result %s %s deaths %d", res.Outcome, res.Reason, res.Deaths)
}
