package campaign

import (
	"context"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/game"
	"os"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
)

func TestZZDbg(t *testing.T) {
	if os.Getenv("ZZ_DBG") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	seed, _ := strconv.Atoi(os.Getenv("ZZ_SEED"))
	done := false
	runEpisode(t, episodeSpec{lib: lib, seed: uint32(seed), stop: 2, budget: 5 * time.Minute, onFrame: func(f *Frame) {
		if done || f.Map != "demo2" || f.LevelMs < 60000 {
			return
		}
		done = true
		b := f.Bot.Belief()
		t.Log(f.Bot.Describe())
		c := f.Session.Client()
		for _, e := range c.FrameEntities(&c.Frame) {
			d := dist3(e.Origin, b.Self.Origin)
			if d < 300 {
				t.Logf("ent %d model %d %q frame %d solid %d effects %#x renderfx %#x origin %v", e.Number, e.ModelIndex, c.ConfigStrings[32+int(e.ModelIndex)], e.Frame, e.Solid, e.Effects, e.RenderFX, e.Origin)
			}
		}
		for _, tr := range b.Tracks {
			if dist3(tr.Pos, b.Self.Origin) < 300 {
				t.Logf("track %+v", tr)
			}
		}
		t.Logf("fire cmds %d stats %+v", f.Bot.Stats().FireCmds, f.Bot.Stats())
		t.Logf("ps %+v", c.Frame.PlayerState)
		srv := f.Session.(*session.Lockstep).Server()
		ed := srv.Game().Edicts()
		for _, n := range []int{1, 259, 265} {
			e := &ed[n]
			t.Logf("edict %d inuse %v health %d dead %d nextthink %v think %v solid %d mins %v maxs %v frame %d svflags %#x enemy %v aiflags %#x", n, e.InUse, e.Health, e.Deadflag, e.Nextthink, e.Think, e.Solid, e.Mins, e.Maxs, e.S.Frame, e.SVFlags, e.Enemy != nil, e.Monsterinfo.Aiflags)
		}
		cl := ed[1].Client
		nw := ""
		if cl.Newweapon != nil {
			nw = cl.Newweapon.PickupName
		}
		pw := ""
		if cl.Pers.Weapon != nil {
			pw = cl.Pers.Weapon.PickupName
		}
		t.Logf("showinv %v showhelp %v showscores %v", cl.Showinventory, cl.Showhelp, cl.Showscores)
		t.Logf("client weapon %q newweapon %q state %d gunframe %d buttons %d latched %d thunk %v modelindex %d", pw, nw, cl.Weaponstate, cl.PS.GunFrame, cl.Buttons, cl.LatchedButtons, cl.WeaponThunk, ed[1].S.ModelIndex)
		if gg, ok := srv.Game().(interface{ Level() *game.LevelLocals }); ok {
			lv := gg.Level()
			t.Logf("game level time %v framenum %d intermission %v changemap %q", lv.Time, lv.Framenum, lv.Intermissiontime, lv.Changemap)
		}
		ls := f.Session.(*session.Lockstep)
		for k := 0; k < 3; k++ {
			if err := ls.Step(context.Background(), f.Bot.Cmd); err != nil {
				t.Fatal(err)
			}
			u := ls.Client().Frame
			_ = u
			t.Logf("after step %d: gunframe %d state %d monster frames %d %d health %d %d buttons %d lvl %v", k, ed[1].Client.PS.GunFrame, ed[1].Client.Weaponstate, ed[259].S.Frame, ed[265].S.Frame, ed[259].Health, ed[265].Health, ed[1].Client.Buttons, srv.Game().(interface{ Level() *game.LevelLocals }).Level().Time)
		}
		t.Logf("level time %v framenum %d paused %q", srv.SV.Time, srv.SV.FrameNum, srv.Cvars.VariableString("paused"))
	}, config: func(c *Config) {
		c.EntryCommands = nil
		p, _ := bot.NewBrain(bot.BrainConfig{Seed: uint64(seed), Mode: decide.Lockstep})
		c.Bot.Policy = p
	}})
}
