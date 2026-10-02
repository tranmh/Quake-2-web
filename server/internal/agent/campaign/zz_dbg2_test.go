package campaign

import (
	"os"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/session"
)

func TestZZDbg2(t *testing.T) {
	if os.Getenv("ZZ_DBG2") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	seed, _ := strconv.Atoi(os.Getenv("ZZ_SEED"))
	var dec []string
	runEpisode(t, episodeSpec{lib: lib, seed: uint32(seed), stop: 2, budget: 135 * time.Second, onFrame: func(f *Frame) {
		if f.Map != "demo2" || f.LevelMs < 44000 || f.LevelMs > 56000 {
			return
		}
		b := f.Bot.Belief()
		srv := f.Session.(*session.Lockstep).Server()
		cl := srv.Game().Edicts()[1].Client
		t.Logf("t=%.1f w=%q a=%d hp=%d int=%s/%s/%s/%s tgt=%s mode=%s fired=%d sw=%d | game weapon %q state %d gf %d btn %d shells %d", float64(f.LevelMs)/1000, b.Self.Weapon, b.Self.Ammo, b.Self.Health,
			f.Bot.Intent().Mode, f.Bot.Intent().FirePolicy, f.Bot.Intent().Movement, f.Bot.Intent().Weapon, f.Bot.Target(), f.Bot.Mode(), f.Bot.Stats().FireCmds, f.Bot.Stats().Switches,
			cl.Pers.Weapon.PickupName, cl.Weaponstate, cl.PS.GunFrame, cl.Buttons, cl.Pers.Inventory[cl.AmmoIndex])
		_ = dec
	}, config: func(c *Config) {
		c.EntryCommands = nil
		p, _ := bot.NewBrain(bot.BrainConfig{Seed: uint64(seed), Mode: decide.Lockstep})
		c.Bot.Policy = p
	}})
}
