package campaign

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
)

// Integration tests on the demo pak (they skip without it): lockstep
// episodes of the bot in god and notarget mode (client cheats, single
// player), so they test the route, the navigation and the campaign
// lifecycle, not combat.

// cheats are the entry commands of the god-mode tests.
var cheats = []string{"god", "notarget"}

// demoLibrary returns a library over the demo pak (opened for test t,
// closed when it ends); a test shares it between its subtests.
func demoLibrary(t testing.TB) *Library {
	t.Helper()
	fs := sessiontest.DemoFS(t)
	return NewLibrary(LibraryConfig{ReadFile: fs.ReadFile, Skill: 1})
}

func demoCampaign(t testing.TB) *route.Campaign {
	t.Helper()
	c, err := route.Load(DefaultRoutesDir())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// episodeSpec is one lockstep episode of a test.
type episodeSpec struct {
	lib     *Library // nil: a library of its own
	start   string   // the start command ("": map demo1)
	visits  map[string]int
	stop    int // StopAfter
	seed    uint32
	budget  time.Duration // game time (EpisodeTimeout)
	onFrame func(f *Frame)
	// prepare runs on the started session before the episode (a
	// gamemap to reach a revisit, say)
	prepare func(t testing.TB, l *session.Lockstep)
	config  func(c *Config)
	// client adjusts the client's options (a recorder's hook, say)
	client func(o *fakeclient.Options)
}

// runEpisode plays spec and returns the result and the trace events; it
// fails the test on an error.
func runEpisode(t testing.TB, spec episodeSpec) (EpisodeResult, []trace.Event) {
	t.Helper()
	fs := sessiontest.DemoFS(t)
	lib := spec.lib
	if lib == nil {
		lib = demoLibrary(t)
	}
	seed := spec.seed
	if seed == 0 {
		seed = 1
	}
	copt := fakeclient.Options{MaxHistory: 256}
	if spec.client != nil {
		spec.client(&copt)
	}
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: seed,
		StartCommand: spec.start, Client: copt})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if spec.prepare != nil {
		spec.prepare(t, l)
	}
	bus := trace.NewBus("test", nil)
	var mu sync.Mutex
	var events []trace.Event
	bus.AddSink(sinkFunc(func(e trace.Event) error {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
		return nil
	}))
	cfg := Config{Campaign: demoCampaign(t), Library: lib, Bus: bus, EntryCommands: cheats, Visits: spec.visits, StopAfter: spec.stop,
		EpisodeTimeout: spec.budget, OnFrame: spec.onFrame, Seed: uint64(seed)}
	if testing.Verbose() || os.Getenv("Q2_CAMPAIGN_LOG") != "" {
		cfg.Logf = t.Logf
	}
	if spec.config != nil {
		spec.config(&cfg)
	}
	wall := time.Now()
	res, err := Run(ctx, l, cfg)
	if err != nil {
		t.Fatalf("episode: %v (result %+v)", err, res)
	}
	t.Logf("episode: %s (%s) in %.1fs game time, %.1fs wall, %d deaths", res.Outcome, res.Reason, float64(res.GameMs)/1000,
		time.Since(wall).Seconds(), res.Deaths)
	for _, lr := range res.Levels {
		t.Logf("  %-6s visit %d (%s): %-8s %6.1fs game %6.1fs wall, %d deaths, steps %d/%d: %s", lr.Map, lr.Visit, lr.Route, lr.Outcome,
			float64(lr.GameMs)/1000, float64(lr.WallMs)/1000, lr.Deaths, lr.StepsDone, lr.Steps, lr.Reason)
	}
	return res, events
}

type sinkFunc func(e trace.Event) error

func (f sinkFunc) Write(e trace.Event) error { return f(e) }

func countEvents(events []trace.Event, typ string) int {
	n := 0
	for _, e := range events {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// TestLevelDemo1God plays demo1 from its start to the elevator exit into
// demo2.
func TestLevelDemo1God(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episode")
	}
	res, events := runEpisode(t, episodeSpec{stop: 1, budget: 4 * time.Minute})
	if res.Outcome != OutcomeCompleted || len(res.Levels) != 1 {
		t.Fatalf("outcome %s (%s), levels %+v\n%s", res.Outcome, res.Reason, res.Levels, res.Diagnostics)
	}
	lr := res.Levels[0]
	if lr.Map != "demo1" || lr.Outcome != trace.OutcomeExit || !strings.Contains(lr.Reason, "demo2") {
		t.Fatalf("demo1 ended %s (%s)", lr.Outcome, lr.Reason)
	}
	if lr.StepsDone != lr.Steps {
		t.Errorf("left demo1 with %d of %d steps done", lr.StepsDone, lr.Steps)
	}
	if n := countEvents(events, trace.TypeLevelStart); n != 1 {
		t.Errorf("%d level_start events, want 1", n)
	}
	if res.Summary.Totals.LevelsCompleted != 1 {
		t.Errorf("summary: %d levels completed, want 1", res.Summary.Totals.LevelsCompleted)
	}
}

// TestCampaignGod is the phase-4 gate: from "map demo1" to victory.pcx
// with god and notarget at skill 1, through every route table.
func TestCampaignGod(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep campaign")
	}
	res, events := runEpisode(t, episodeSpec{budget: 30 * time.Minute})
	if res.Outcome != OutcomeCompleted || !res.Victory {
		t.Fatalf("outcome %s (%s), victory %v\n%s", res.Outcome, res.Reason, res.Victory, res.Diagnostics)
	}
	want := []string{"demo1/0", "demo2/0", "demo3/0", "demo2/1"}
	if len(res.Levels) != len(want) {
		t.Fatalf("%d levels, want %d: %+v", len(res.Levels), len(want), res.Levels)
	}
	for i, lr := range res.Levels {
		if got := lr.Map + "/" + string(rune('0'+lr.Visit)); got != want[i] {
			t.Errorf("level %d is %s, want %s", i, got, want[i])
		}
	}
	if last := res.Levels[len(res.Levels)-1]; last.Outcome != trace.OutcomeVictory {
		t.Errorf("last level ended %s, want victory", last.Outcome)
	}
	// the route's two kills, as the bot perceived them
	if n := countEvents(events, trace.TypeKill); n < 2 || res.Summary.Totals.BotKills != n {
		t.Errorf("%d kill events (summary %d), want the two route kills at least", n, res.Summary.Totals.BotKills)
	}
	if n := countEvents(events, trace.TypeEpisodeEnd); n != 1 {
		t.Errorf("%d episode_end events", n)
	}
}

// TestVisits plays each later visit on its own from its arrival: demo2's
// first visit ("map demo2$base1"), demo3 ("map demo3$base2a") and demo2's
// second visit. The console expands "$name" as a cvar (C
// Cmd_MacroExpandString) unless it is quoted, and "map" refuses a quoted
// "demo2$base1" (no maps/demo2$base1.bsp), so the first two start at the
// map's default spawn, which is next to the arrival spot. demo2's second
// visit is reached the way the campaign reaches it without playing the
// levels in between: "map demo2$base1", gamemap "demo3$base2a" (which
// saves demo2) and gamemap "demo2$base3b" (which restores it, at base3b).
// The campaign's revisit restores demo2 as demo2a left it; demo2b's route
// does not depend on what demo2a changed.
func TestVisits(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episodes")
	}
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("about 17 s under -race, and TestCampaignGod plays the same levels: set Q2_AGENT_LONG=1")
	}
	lib := demoLibrary(t)
	t.Run("demo2a", func(t *testing.T) {
		res, _ := runEpisode(t, episodeSpec{lib: lib, start: "map demo2$base1", stop: 1, budget: 5 * time.Minute})
		wantExit(t, res, "demo2", "demo3")
	})
	t.Run("demo3", func(t *testing.T) {
		res, _ := runEpisode(t, episodeSpec{lib: lib, start: "map demo3$base2a", stop: 1, budget: 10 * time.Minute})
		wantExit(t, res, "demo3", "demo2")
	})
	t.Run("demo2b", func(t *testing.T) {
		res, _ := runEpisode(t, episodeSpec{lib: lib, start: "map demo2$base1", visits: map[string]int{"demo2": 1}, budget: 10 * time.Minute,
			prepare: func(t testing.TB, l *session.Lockstep) {
				for _, cmd := range []string{`gamemap "demo3$base2a"`, `gamemap "demo2$base3b"`} {
					gen := l.LevelGen()
					if err := l.Exec(cmd); err != nil {
						t.Fatal(err)
					}
					if err := l.WaitLevel(context.Background(), gen, 0); err != nil {
						t.Fatal(err)
					}
				}
			}})
		if res.Outcome != OutcomeCompleted || !res.Victory {
			t.Fatalf("outcome %s (%s), victory %v\n%s", res.Outcome, res.Reason, res.Victory, res.Diagnostics)
		}
	})
}

func wantExit(t *testing.T, res EpisodeResult, from, to string) {
	t.Helper()
	if res.Outcome != OutcomeCompleted || len(res.Levels) != 1 {
		t.Fatalf("outcome %s (%s), levels %+v\n%s", res.Outcome, res.Reason, res.Levels, res.Diagnostics)
	}
	if lr := res.Levels[0]; lr.Map != from || lr.Outcome != trace.OutcomeExit || lr.Reason != "to "+to {
		t.Fatalf("%s ended %s (%s), want exit to %s", lr.Map, lr.Outcome, lr.Reason, to)
	}
}

// TestCampaignDeathReload forces a death on demo1 ("kill" from the
// console; Cmd_Kill_f refuses within 5 s of a spawn, so it waits 8 s):
// the campaign waits, reloads save0, restores the level memory (the
// death spot is kept) and the bot still finishes demo1.
func TestCampaignDeathReload(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episode")
	}
	killed := false
	var b *bot.Bot
	res, events := runEpisode(t, episodeSpec{stop: 1, budget: 5 * time.Minute, onFrame: func(f *Frame) {
		b = f.Bot
		if !killed && f.LevelMs > 8000 {
			killed = true
			f.Session.Client().StringCmd("kill")
		}
	}})
	if !killed {
		t.Fatal("never sent kill")
	}
	if res.Outcome != OutcomeCompleted || len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeExit {
		t.Fatalf("outcome %s (%s), levels %+v", res.Outcome, res.Reason, res.Levels)
	}
	if res.Deaths != 1 || res.Levels[0].Deaths != 1 {
		t.Errorf("deaths %d (level %d), want 1", res.Deaths, res.Levels[0].Deaths)
	}
	if n, m := countEvents(events, trace.TypeDeath), countEvents(events, trace.TypeReload); n != 1 || m != 1 {
		t.Errorf("%d death and %d reload events, want 1 each", n, m)
	}
	if got := res.Summary.Totals; got.Deaths != 1 || got.Reloads != 1 {
		t.Errorf("summary deaths %d reloads %d", got.Deaths, got.Reloads)
	}
	mem := b.World().MemoryFor(worldmodel.LevelKey{Map: "demo1"})
	if mem == nil || mem.Entries != 2 || len(mem.DeathSpots) != 1 {
		t.Fatalf("level memory after the reload: %+v", mem)
	}
	// the reload restarted the route
	for _, e := range events {
		if e.Type == trace.TypeReload {
			var body trace.Reload
			if err := e.DecodeBody(&body); err != nil || body.Slot != "save0" || body.Deaths != 1 {
				t.Errorf("reload event %+v (%v)", body, err)
			}
		}
	}
}

// TestRunFailures: the episode ends with an error on a level without a
// route for its visit and on map data that is not the server's map, and
// fails (without an error) when a level takes more deaths than allowed.
func TestRunFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episodes")
	}
	t.Run("no route", func(t *testing.T) {
		fs := sessiontest.DemoFS(t)
		l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1})
		if err := l.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		res, err := Run(context.Background(), l, Config{Campaign: demoCampaign(t), Library: demoLibrary(t), Visits: map[string]int{"demo1": 1}})
		if !errors.Is(err, ErrNoRoute) || res.Outcome != OutcomeFailed {
			t.Fatalf("result %+v, %v", res, err)
		}
	})
	t.Run("checksum", func(t *testing.T) {
		fs := sessiontest.DemoFS(t)
		other := func(name string) ([]byte, error) { // demo2's BSP served as demo1's
			return fs.ReadFile(strings.Replace(name, "demo1", "demo2", 1))
		}
		l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1})
		if err := l.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		res, err := Run(context.Background(), l, Config{Campaign: demoCampaign(t), Library: NewLibrary(LibraryConfig{ReadFile: other, Skill: 1})})
		if err == nil || !strings.Contains(err.Error(), "checksum") || res.Outcome != OutcomeFailed {
			t.Fatalf("result %+v, %v", res, err)
		}
	})
	t.Run("death limit", func(t *testing.T) {
		killed := false
		res, events := runEpisode(t, episodeSpec{stop: 1, budget: time.Minute, onFrame: func(f *Frame) {
			if !killed && f.LevelMs > 8000 {
				killed = true
				f.Session.Client().StringCmd("kill")
			}
		}, config: func(c *Config) { c.MaxDeaths = -1 }})
		if res.Outcome != OutcomeFailed || len(res.Levels) != 1 || res.Levels[0].Outcome != trace.OutcomeDeathLimit {
			t.Fatalf("result %+v", res)
		}
		if countEvents(events, trace.TypeReload) != 0 {
			t.Error("reloaded past the death limit")
		}
	})
}

// TestDeterministic: two lockstep runs of demo1 with the same seed give
// the same trace (wall time aside) and the same belief, frame by frame.
func TestDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episodes")
	}
	lib := demoLibrary(t)
	run := func() ([][]byte, []string) {
		var digests []string
		n := 0
		_, events := runEpisode(t, episodeSpec{lib: lib, stop: 1, budget: 2 * time.Minute, onFrame: func(f *Frame) {
			if n++; n%10 == 0 {
				digests = append(digests, f.Bot.Belief().Digest())
			}
		}})
		var out [][]byte
		for _, e := range events {
			b, err := trace.Comparable(e)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, b)
		}
		return out, digests
	}
	e1, d1 := run()
	e2, d2 := run()
	if len(e1) != len(e2) || len(d1) != len(d2) || len(d1) == 0 {
		t.Fatalf("%d/%d events, %d/%d digests", len(e1), len(e2), len(d1), len(d2))
	}
	for i := range e1 {
		if string(e1[i]) != string(e2[i]) {
			t.Fatalf("event %d differs:\n%s\n%s", i, e1[i], e2[i])
		}
	}
	for i := range d1 {
		if d1[i] != d2[i] {
			t.Fatalf("belief differs at frame %d", (i+1)*10)
		}
	}
}

// TestInProcDemo1 plays demo1 on a realtime host instance (InProc): the
// same campaign code on wall time (about 25 s; Q2_AGENT_LONG=1).
func TestInProcDemo1(t *testing.T) {
	if os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("realtime: about 25 s of wall time (set Q2_AGENT_LONG=1)")
	}
	fs := sessiontest.DemoFS(t)
	inst, err := session.NewInstance(host.New(), session.InstanceConfig{ID: "bot-campaign", FS: fs, Spec: session.Spec{Skill: 1}, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Stop)
	p := session.NewInProc(inst, session.InProcConfig{FS: fs, Client: fakeclient.Options{MaxHistory: 256}})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cfg := Config{Campaign: demoCampaign(t), Library: demoLibrary(t), EntryCommands: cheats, StopAfter: 1, EpisodeTimeout: 90 * time.Second, Logf: t.Logf}
	res, err := Run(ctx, p, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("InProc demo1: %s (%s) in %.1fs", res.Outcome, res.Reason, float64(res.WallMs)/1000)
	wantExit(t, res, "demo1", "demo2")
}

// TestCampaignGodSeeds plays the god-mode campaign on more seeds (the
// monsters' wandering and the random spawns differ): Q2_AGENT_LONG=1.
func TestCampaignGodSeeds(t *testing.T) {
	if os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("set Q2_AGENT_LONG=1")
	}
	lib := demoLibrary(t)
	for seed := uint32(2); seed <= 9; seed++ {
		t.Run(strconv.Itoa(int(seed)), func(t *testing.T) {
			res, _ := runEpisode(t, episodeSpec{lib: lib, seed: seed, budget: 30 * time.Minute})
			if !res.Victory {
				t.Errorf("seed %d: %s (%s)\n%s", seed, res.Outcome, res.Reason, res.Diagnostics)
			}
		})
	}
}
