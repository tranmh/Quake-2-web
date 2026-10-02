package campaign

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// The level lifecycle on a fake session: the runner's arrival, exit and
// reload handling driven directly (the integration tests play it on the
// real game).

// newFakeRunner returns a runner of the checked-in campaign over the fake
// session f and library lib (nil: one that loads nothing).
func newFakeRunner(t *testing.T, f *fakeSession, lib *Library, ctl Control) *runner {
	t.Helper()
	cfg, _ := fakeConfig(t, f)
	if lib != nil {
		cfg.Library = lib
	}
	cfg.Control = ctl
	cfg.EntryCommands = cheats
	cfg.defaults()
	r, err := newRunner(context.Background(), f, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// playing makes the runner play visit visit of map name (with its route
// table), entered, as if the client had been active on it.
func playing(t *testing.T, r *runner, name string, visit int) *levelState {
	t.Helper()
	tab, err := r.camp.Select(name, visit)
	if err != nil {
		t.Fatal(err)
	}
	r.gen = r.s.LevelGen()
	r.lvl++
	r.lv = &levelState{name: name, visit: visit, table: tab, startGMs: r.s.GameTimeMs(), start: r.cfg.Now(), entered: true}
	r.visits[name] = visit + 1
	return r.lv
}

// gameLevel feeds the fake client a new game level: its serverdata, the
// configstrings that name it (and its checksum) and a first frame at o,
// as an active client has them.
func (f *fakeSession) gameLevel(t testing.TB, count int32, name string, checksum uint32, o Vec3) {
	t.Helper()
	f.serverdata(t, count, 0, "base")
	f.c.ConfigStrings[q2const.CS_MODELS+1] = "maps/" + name + ".bsp"
	f.c.ConfigStrings[q2const.CS_MAPCHECKSUM] = strconv.Itoa(int(int32(checksum)))
	for k := 0; k < 3; k++ {
		f.c.Frame.PlayerState.PMove.Origin[k] = int16(o[k] * 8)
	}
	f.c.State = fakeclient.CaActive
}

// arriveOn feeds a new game level and runs the runner's arrival on it up
// to the bot's entry (begin).
func arriveOn(t *testing.T, r *runner, f *fakeSession, count int32, name string, checksum uint32, o Vec3) (bool, EpisodeResult, error) {
	t.Helper()
	f.gameLevel(t, count, name, checksum, o)
	if done, res, err := r.arrive(f.c); done || err != nil {
		return done, res, err
	}
	if !r.pending {
		t.Fatalf("arrival on %s left nothing pending", name)
	}
	return r.begin(context.Background(), f.c)
}

// TestUnplannedExitWrongLevel: an arrival on a level, picture or
// cinematic other than the one the left level's route exits to ends the
// episode with ErrUnplannedExit, the left level with an error.
func TestUnplannedExitWrongLevel(t *testing.T) {
	check := func(t *testing.T, r *runner, res EpisodeResult, err error, from string) {
		t.Helper()
		if !errors.Is(err, ErrUnplannedExit) || res.Outcome != OutcomeFailed || res.Victory {
			t.Fatalf("result %+v, %v", res, err)
		}
		if len(res.Levels) != 1 || res.Levels[0].Map != from || res.Levels[0].Outcome != trace.OutcomeError ||
			!strings.Contains(res.Levels[0].Reason, "unplanned exit") || !strings.Contains(res.Levels[0].Reason, "at step") {
			t.Fatalf("levels %+v", res.Levels)
		}
	}
	t.Run("level", func(t *testing.T) {
		f := newFakeSession()
		r := newFakeRunner(t, f, nil, nil)
		playing(t, r, "demo2", 0) // demo2a exits to demo3$base2a
		_, res, err := arriveOn(t, r, f, 5, "demo1", 0, Vec3{})
		check(t, r, res, err, "demo2")
	})
	t.Run("picture", func(t *testing.T) {
		f := newFakeSession()
		r := newFakeRunner(t, f, nil, nil)
		playing(t, r, "demo3", 0) // demo3 exits to demo2$base3b, not the end
		f.serverdata(t, 5, -1, "victory.pcx")
		done, res, err := r.arrive(f.c)
		if !done {
			t.Fatal("the episode went on")
		}
		check(t, r, res, err, "demo3")
	})
	t.Run("cinematic", func(t *testing.T) {
		f := newFakeSession()
		r := newFakeRunner(t, f, nil, nil)
		playing(t, r, "demo1", 0)
		f.serverdata(t, 5, -1, "intro.cin")
		done, res, err := r.arrive(f.c)
		if !done {
			t.Fatal("the episode went on")
		}
		check(t, r, res, err, "demo1")
		for _, s := range f.sent() {
			if strings.HasPrefix(s, "nextserver") {
				t.Errorf("answered an unplanned cinematic: %q", s)
			}
		}
	})
	t.Run("after a cinematic", func(t *testing.T) {
		// a route exit through a cinematic ("end.cin+demo3$base2a"): the
		// cinematic is planned, the level after it is checked against the
		// rest of the exit string
		f := newFakeSession()
		r := newFakeRunner(t, f, nil, nil)
		lv := playing(t, r, "demo2", 0)
		tab := *lv.table
		tab.Exit = route.ExitRef{Map: "end.cin+demo3$base2a"}
		lv.table = &tab
		f.serverdata(t, 5, -1, "end.cin")
		if done, res, err := r.arrive(f.c); done || err != nil {
			t.Fatalf("planned cinematic: %+v, %v", res, err)
		}
		if r.expect == nil || r.expect.Map != "demo3" || r.expect.Spawnpoint != "base2a" {
			t.Fatalf("expected after the cinematic: %+v", r.expect)
		}
		_, res, err := arriveOn(t, r, f, 6, "demo1", 0, Vec3{})
		if !errors.Is(err, ErrUnplannedExit) || res.Outcome != OutcomeFailed {
			t.Fatalf("result %+v, %v", res, err)
		}
		// the left level ended at the (planned) cinematic
		if len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeExit || res.Levels[0].Reason != "to end.cin" {
			t.Fatalf("levels %+v", res.Levels)
		}
	})
}

// TestLevelLifecycle walks the campaign's arrivals on the fake session
// with the real map data and graphs: visits are counted per map (demo2's
// second arrival gets demo2b's table), every exit is checked against the
// route's claimed exit and its spawnpoint, and an arrival at the wrong
// spawnpoint of the right level is an unplanned exit.
func TestLevelLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the demo maps' graphs")
	}
	lib := demoLibrary(t)
	f := newFakeSession()
	r := newFakeRunner(t, f, lib, nil)
	spawn := func(name, sp string) (uint32, Vec3) {
		t.Helper()
		md, err := lib.Map(name)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := md.SpawnPoint(sp)
		if !ok {
			t.Fatalf("%s has no spawnpoint %q", name, sp)
		}
		return md.Checksum, s.Origin
	}
	count := int32(1)
	arrive := func(name, sp string) (bool, EpisodeResult, error) {
		t.Helper()
		sum, o := spawn(name, sp)
		count++
		return arriveOn(t, r, f, count, name, sum, o)
	}
	for i, want := range []struct {
		name, sp, table string
		visit           int
	}{
		{"demo1", "", "demo1", 0}, {"demo2", "base1", "demo2a", 0}, {"demo3", "base2a", "demo3", 0}, {"demo2", "base3b", "demo2b", 1},
	} {
		if done, res, err := arrive(want.name, want.sp); done || err != nil {
			t.Fatalf("arrival %d on %s$%s: %+v, %v", i, want.name, want.sp, res, err)
		}
		lv := r.lv
		if lv == nil || !lv.entered || lv.name != want.name || lv.visit != want.visit || lv.table.Name != want.table || r.lvl != i {
			t.Fatalf("arrival %d: playing %+v (level %d), want %s visit %d with %s", i, lv, r.lvl, want.name, want.visit, want.table)
		}
		if got := r.bot.Level().Key; got.Map != want.name || got.Visit != want.visit {
			t.Fatalf("arrival %d: bot entered %+v", i, got)
		}
		if i > 0 {
			prev := r.results[i-1]
			if prev.Outcome != trace.OutcomeExit || prev.Reason != "to "+want.name || prev.StepsDone != prev.Steps {
				t.Fatalf("level %d ended %+v", i-1, prev)
			}
		}
	}
	if n := len(f.sent()); n == 0 {
		t.Error("no entry commands sent")
	}

	// demo2a's directional trigger *39 leads to demo3$base2b: the right
	// level at the wrong spawnpoint (demo3's table starts at base2a)
	f2 := newFakeSession()
	r2 := newFakeRunner(t, f2, lib, nil)
	playing(t, r2, "demo2", 0)
	sum, o := spawn("demo3", "base2b")
	_, res, err := arriveOn(t, r2, f2, 9, "demo3", sum, o)
	if !errors.Is(err, ErrUnplannedExit) || !strings.Contains(err.Error(), "base2b") || len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeError {
		t.Fatalf("arrival at base2b: %+v, %v", res, err)
	}
	if r2.lv != nil {
		t.Errorf("began %s after an unplanned exit", r2.lv.name)
	}
	// a first frame near its spawnpoint (it spawns 9 units up and drops)
	// is no mistake
	md, _ := lib.Map("demo3")
	sp, _ := md.SpawnPoint("base2a")
	if why := wrongSpawn(md, "base2a", Vec3{sp.Origin[0] + 20, sp.Origin[1], sp.Origin[2] + 9}); why != "" {
		t.Errorf("near base2a: %s", why)
	}
	if why := wrongSpawn(md, "base2a", o); why == "" {
		t.Error("base2b taken for base2a")
	}
}

// reloadControl is a Control whose Reload the test scripts.
type reloadControl struct {
	calls  int
	reload func() error
}

func (c *reloadControl) Reload(context.Context) error {
	c.calls++
	return c.reload()
}

// TestReloadBranches: a reload that fails, does not change the level or
// lands on another level ends the episode with an error; past the death
// limit there is no reload.
func TestReloadBranches(t *testing.T) {
	for _, c := range []struct {
		name   string
		reload func(f *fakeSession) error
		want   string
	}{
		{"error", func(*fakeSession) error { return errors.New("no save") }, "no save"},
		{"same level generation", func(*fakeSession) error { return nil }, "landed on"},
		{"another level", func(f *fakeSession) error {
			f.gameLevel(t, 9, "demo2", 0, Vec3{})
			return nil
		}, `landed on "demo2"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeSession()
			f.gameLevel(t, 3, "demo1", 0, Vec3{})
			ctl := &reloadControl{reload: func() error { return c.reload(f) }}
			r := newFakeRunner(t, f, nil, ctl)
			lv := playing(t, r, "demo1", 0)
			done, res, err := r.reload(context.Background())
			if !done || err == nil || !strings.Contains(err.Error(), c.want) || res.Outcome != OutcomeFailed || ctl.calls != 1 {
				t.Fatalf("reload: done %v, %+v, %v (%d calls)", done, res, err, ctl.calls)
			}
			if lv.deaths != 1 || res.Deaths != 1 || len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeError {
				t.Fatalf("deaths %d/%d, levels %+v", lv.deaths, res.Deaths, res.Levels)
			}
		})
	}
	t.Run("death limit", func(t *testing.T) {
		f := newFakeSession()
		f.gameLevel(t, 3, "demo1", 0, Vec3{})
		ctl := &reloadControl{reload: func() error { return nil }}
		r := newFakeRunner(t, f, nil, ctl)
		r.cfg.MaxDeaths = 2
		lv := playing(t, r, "demo1", 0)
		lv.deaths = 2
		done, res, err := r.reload(context.Background())
		if !done || err != nil || res.Outcome != OutcomeFailed || ctl.calls != 0 {
			t.Fatalf("reload: done %v, %+v, %v (%d calls)", done, res, err, ctl.calls)
		}
		if len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeDeathLimit || res.Levels[0].Deaths != 3 {
			t.Fatalf("levels %+v", res.Levels)
		}
	})
}

// TestFrameWatchdog: an entered level that gets no frame for the bot
// fails after FrameTimeout of game time, whatever the other watchdogs
// say.
func TestFrameWatchdog(t *testing.T) {
	f := newFakeSession()
	f.gameLevel(t, 3, "demo1", 0, Vec3{})
	r := newFakeRunner(t, f, nil, nil)
	lv := playing(t, r, "demo1", 0)
	lv.frameAt = r.s.GameTimeMs()
	for i := 0; i < 200; i++ {
		_ = f.Step(context.Background(), nil)
		if done, res, err := r.watchLevel(); done {
			if err != nil || res.Outcome != OutcomeFailed || !strings.Contains(res.Reason, "frame watchdog") || res.Diagnostics == "" {
				t.Fatalf("result %+v, %v", res, err)
			}
			if got := f.clock - lv.frameAt; got <= DefaultFrameTimeout.Milliseconds() || got > DefaultFrameTimeout.Milliseconds()+100 {
				t.Errorf("failed after %d ms without a frame", got)
			}
			return
		}
	}
	t.Fatal("no frame watchdog")
}

// TestKillEventsInFightOrder: kills seen on one frame are traced in the
// order the bot first fought the monsters (deterministic traces).
func TestKillEventsInFightOrder(t *testing.T) {
	for run := 0; run < 20; run++ {
		f := newFakeSession()
		f.gameLevel(t, 3, "demo1", 0, Vec3{})
		var events []trace.Event
		r := newFakeRunner(t, f, nil, nil)
		r.bus.AddSink(sinkFunc(func(e trace.Event) error { events = append(events, e); return nil }))
		lv := playing(t, r, "demo1", 0)
		ids := []string{"e9", "e2", "e5", "e7"}
		for _, id := range ids {
			lv.fought = append(lv.fought, foughtTrack{id: id})
		}
		bel := &worldmodel.Belief{}
		for _, id := range []string{"e2", "e5", "e7", "e9"} {
			bel.Tracks = append(bel.Tracks, worldmodel.Track{ID: id, Class: "soldier", Life: worldmodel.LifeDead})
		}
		r.traceFight(lv, bel, r.s.GameTimeMs())
		r.traceFight(lv, bel, r.s.GameTimeMs()) // traced once
		var got []string
		for _, e := range events {
			var k trace.Kill
			if e.Type != trace.TypeKill || e.DecodeBody(&k) != nil {
				continue
			}
			got = append(got, k.Target)
		}
		if strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Fatalf("run %d: kills traced %v, want %v", run, got, ids)
		}
	}
}
