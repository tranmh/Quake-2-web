package campaign

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
)

// Integration tests of the brain on the demo pak (they skip without it):
// the bot's Policy is the full decision pipeline (decide.Pipeline: lane
// states, scheduler, arbiter) over the scripted backend, in lockstep, and
// the bot plays without god or notarget at skill 1: it fights, takes
// damage, dies and reloads.

// Gate limits: deaths are allowed (each one reloads the level-entry save),
// so the per-level death limit and the watchdogs are wider than the
// defaults, which are tuned for god mode.
const (
	gateMaxDeaths    = 25
	gateLevelTimeout = 60 * time.Minute
	gateFailAfter    = 10 * time.Minute
)

// brain makes cfg's bot play with the scripted decision pipeline in
// lockstep, without cheats.
func brain(t testing.TB, seed uint64) func(c *Config) {
	return func(c *Config) {
		c.EntryCommands = nil
		p, err := bot.NewBrain(bot.BrainConfig{Seed: seed, Mode: decide.Lockstep})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		c.Bot.Policy = p
	}
}

// tickEvents returns the trace's decision tick events.
func tickEvents(t testing.TB, events []trace.Event) []trace.Decision {
	t.Helper()
	var out []trace.Decision
	for _, e := range events {
		if e.Type != trace.TypeDecision {
			continue
		}
		var d trace.Decision
		if err := e.DecodeBody(&d); err != nil {
			t.Fatal(err)
		}
		if d.Lane == trace.LaneTick {
			out = append(out, d)
		}
	}
	return out
}

// TestLevelDemo1Scripted is the short gate: demo1 from its start to the
// exit into demo2 without god or notarget, the scripted pipeline deciding
// every tick. Every tick is traced with its intent, the provenance of
// each field and the commands sent since the last one.
func TestLevelDemo1Scripted(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episode")
	}
	res, events := runEpisode(t, episodeSpec{stop: 1, budget: 10 * time.Minute, config: func(c *Config) {
		brain(t, 1)(c)
		c.MaxDeaths = gateMaxDeaths
	}})
	if res.Outcome != OutcomeCompleted || len(res.Levels) != 1 {
		t.Fatalf("outcome %s (%s), levels %+v\n%s", res.Outcome, res.Reason, res.Levels, res.Diagnostics)
	}
	wantExit(t, res, "demo1", "demo2")
	ticks := tickEvents(t, events)
	if len(ticks) < 100 {
		t.Fatalf("%d tick events", len(ticks))
	}
	scripted, cmds := 0, 0
	for _, d := range ticks {
		if d.Intent == nil || d.Tick == nil || d.StateDigest == "" || len(d.Intent.Fields) != 7 {
			t.Fatalf("incomplete tick event %+v", d)
		}
		for _, f := range d.Intent.Fields {
			if f.Source == trace.SourceScripted {
				scripted++
			}
		}
		cmds += len(d.Cmds)
	}
	if scripted == 0 {
		t.Error("no intent field came from the scripted backend")
	}
	if cmds < 4*len(ticks)-8 {
		t.Errorf("%d commands traced over %d ticks", cmds, len(ticks))
	}
	t.Logf("demo1: %d deaths, %d ticks, %d commands", res.Levels[0].Deaths, len(ticks), cmds)
}

// modelStandIn is the scripted backend under another name, so the
// pipeline takes its answers as a model's: delayed by the simulated
// latency, with the scripted fallback deciding meanwhile.
type modelStandIn struct{ decide.DecisionBackend }

func (modelStandIn) Name() string { return "stand-in" }

// TestLevelDemo1Latency plays demo1 without cheats with the answers of a
// "model" arriving 150 to 300 ms after their snapshot (a sampled lockstep
// latency, as for the mock model): the fallback covers the wait, the
// answers land late, and the bot still finishes the level. Most fields of
// the ticks come from the model's answers.
func TestLevelDemo1Latency(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episode")
	}
	var samples []time.Duration
	for ms := 150; ms <= 300; ms += 10 {
		samples = append(samples, time.Duration(ms)*time.Millisecond)
	}
	res, events := runEpisode(t, episodeSpec{stop: 1, budget: 10 * time.Minute, config: func(c *Config) {
		c.EntryCommands = nil
		c.MaxDeaths = gateMaxDeaths
		p, err := bot.NewBrain(bot.BrainConfig{Seed: 1, Mode: decide.Lockstep, Backend: modelStandIn{scripted.New(scripted.Config{Seed: 1})},
			SimLatency: &decide.SampledLatency{Seed: 1, Samples: samples}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		c.Bot.Policy = p
	}})
	wantExit(t, res, "demo1", "demo2")
	model, other := 0, 0
	for _, d := range tickEvents(t, events) {
		for _, f := range d.Intent.Fields {
			if f.Source == trace.SourceModel {
				model++
			} else {
				other++
			}
		}
	}
	if model <= other {
		t.Errorf("%d fields from the model's answers, %d otherwise", model, other)
	}
	t.Logf("demo1 with 150-300 ms latency: %d deaths; %d fields from the model, %d otherwise", res.Deaths, model, other)
}

// TestCampaignScripted is the phase-5 gate: from "map demo1" to
// victory.pcx at skill 1 without god or notarget, the scripted backend
// deciding through the full pipeline. Deaths are allowed: each one
// reloads the level-entry save (gateMaxDeaths per level). Seed 1 takes
// about 15 s of wall time (other seeds up to a minute: more deaths on
// demo3); Q2_GATE_SEED picks another, and Q2_AGENT_LONG=1 runs it under
// -race too.
func TestCampaignScripted(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep campaign")
	}
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("minutes under -race: set Q2_AGENT_LONG=1")
	}
	seed := uint32(1)
	if s, err := strconv.Atoi(os.Getenv("Q2_GATE_SEED")); err == nil && s > 0 {
		seed = uint32(s)
	}
	wall := time.Now()
	res, events := runEpisode(t, episodeSpec{seed: seed, budget: 3 * time.Hour, config: func(c *Config) {
		brain(t, uint64(seed))(c)
		c.MaxDeaths, c.LevelTimeout, c.FailAfter = gateMaxDeaths, gateLevelTimeout, gateFailAfter
	}})
	var per []string
	for _, lr := range res.Levels {
		per = append(per, lr.Map+"/"+strconv.Itoa(lr.Visit)+": "+strconv.Itoa(lr.Deaths))
	}
	t.Logf("GATE seed %d: %s, victory %v, %.0fs game time, %.1fs wall, %d deaths (%s)", seed, res.Outcome, res.Victory,
		float64(res.GameMs)/1000, time.Since(wall).Seconds(), res.Deaths, strings.Join(per, ", "))
	if res.Outcome != OutcomeCompleted || !res.Victory {
		t.Fatalf("outcome %s (%s), victory %v\n%s", res.Outcome, res.Reason, res.Victory, res.Diagnostics)
	}
	want := []string{"demo1/0", "demo2/0", "demo3/0", "demo2/1"}
	if len(res.Levels) != len(want) {
		t.Fatalf("%d levels, want %d: %+v", len(res.Levels), len(want), res.Levels)
	}
	for i, lr := range res.Levels {
		if got := lr.Map + "/" + strconv.Itoa(lr.Visit); got != want[i] {
			t.Errorf("level %d is %s, want %s", i, got, want[i])
		}
	}
	if n := countEvents(events, trace.TypeDeath); n != res.Deaths || countEvents(events, trace.TypeReload) != res.Deaths {
		t.Errorf("%d death and %d reload events for %d deaths", n, countEvents(events, trace.TypeReload), res.Deaths)
	}
	if res.Summary.Totals.BotKills < 2 {
		t.Errorf("%d kills: the route's two at least", res.Summary.Totals.BotKills)
	}
}

// TestCampaignScriptedSeeds plays the gate on more seeds and reports each
// outcome (Q2_AGENT_LONG=1; several minutes). It fails when fewer than
// half of them reach the victory.
func TestCampaignScriptedSeeds(t *testing.T) {
	if os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("set Q2_AGENT_LONG=1")
	}
	lib := demoLibrary(t)
	wins, n := 0, 0
	for seed := uint32(2); seed <= 7; seed++ {
		n++
		res, _ := runEpisode(t, episodeSpec{lib: lib, seed: seed, budget: 3 * time.Hour, config: func(c *Config) {
			brain(t, uint64(seed))(c)
			c.MaxDeaths, c.LevelTimeout, c.FailAfter = gateMaxDeaths, gateLevelTimeout, gateFailAfter
		}})
		if res.Victory {
			wins++
		}
		t.Logf("seed %d: %s (%s), %.0fs game time, %d deaths", seed, res.Outcome, res.Reason, float64(res.GameMs)/1000, res.Deaths)
	}
	t.Logf("%d of %d seeds reached the victory", wins, n)
	if 2*wins < n {
		t.Errorf("%d of %d seeds reached the victory", wins, n)
	}
}

// TestDeterministicScripted: two lockstep runs of demo1 without god and
// with the same seed give the same trace, wall time aside: every decision
// tick (intents, provenance, lane-state digests, the commands sent) and
// every campaign event.
func TestDeterministicScripted(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep episodes")
	}
	lib := demoLibrary(t)
	run := func() [][]byte {
		_, events := runEpisode(t, episodeSpec{lib: lib, stop: 1, budget: 10 * time.Minute, config: func(c *Config) {
			brain(t, 1)(c)
			c.MaxDeaths = gateMaxDeaths
		}})
		var out [][]byte
		for _, e := range events {
			b, err := trace.Comparable(e)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, b)
		}
		if n := len(tickEvents(t, events)); n < 100 {
			t.Fatalf("%d tick events", n)
		}
		return out
	}
	e1, e2 := run(), run()
	if len(e1) != len(e2) {
		t.Fatalf("%d and %d events", len(e1), len(e2))
	}
	for i := range e1 {
		if string(e1[i]) != string(e2[i]) {
			t.Fatalf("event %d differs:\n%s\n%s", i, e1[i], e2[i])
		}
	}
}

// FairnessRecording describes a recording TestRecordFairness writes for
// the fairness differential test (agent/fairness): the episode's seed,
// map, visit and skill, and its files in the directory.
type FairnessRecording struct {
	Seed   uint64 `json:"seed"`
	Map    string `json:"map"`
	Visit  int    `json:"visit"`
	Skill  int    `json:"skill"`
	Demo   string `json:"demo"`  // the level's .dm2 file
	Trace  string `json:"trace"` // the episode's trace (gzipped JSON lines)
	Deaths int    `json:"deaths"`
	Ticks  int    `json:"ticks"`
}

// TestRecordFairness records the episode the fairness differential test
// rebuilds: demo1 played by the scripted pipeline in lockstep without
// cheats, the client's messages recorded as a .dm2 file (demo.Recorder)
// and the trace with every decision tick. It runs only when
// Q2_FAIRNESS_OUT names the directory to write into (the fairness test
// sets it; Q2_FAIRNESS_SEED picks the seed, 1 by default).
func TestRecordFairness(t *testing.T) {
	out := os.Getenv("Q2_FAIRNESS_OUT")
	if out == "" {
		t.Skip("the fairness test's recording: set Q2_FAIRNESS_OUT")
	}
	seed := uint64(1)
	if s, err := strconv.Atoi(os.Getenv("Q2_FAIRNESS_SEED")); err == nil && s > 0 {
		seed = uint64(s)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := demo.NewRecorder(demo.DirCreator(filepath.Join(out, "demo")))
	sink, err := trace.CreateFile(filepath.Join(out, "trace.jsonl.gz"), 0)
	if err != nil {
		t.Fatal(err)
	}
	res, events := runEpisode(t, episodeSpec{seed: uint32(seed), stop: 1, budget: 10 * time.Minute,
		client: func(o *fakeclient.Options) { o.OnServerMessage = rec.OnServerMessage },
		config: func(c *Config) {
			brain(t, seed)(c)
			c.MaxDeaths = gateMaxDeaths
		}})
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := sink.Write(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	wantExit(t, res, "demo1", "demo2")
	files := rec.Files()
	level := ""
	for _, f := range files {
		if strings.HasSuffix(f, "-demo1.dm2") {
			level = f // demo1's (the client arrives on demo2 before the episode stops)
			break
		}
	}
	if level == "" {
		t.Fatalf("no demo1 file recorded: %v", files)
	}
	meta := FairnessRecording{Seed: seed, Map: "demo1", Skill: 1, Demo: filepath.Join("demo", level), Trace: "trace.jsonl.gz",
		Deaths: res.Deaths, Ticks: len(tickEvents(t, events))}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "recording.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded %v (%d deaths, %d ticks) in %s", files, res.Deaths, meta.Ticks, out)
}

// TestInProcDemo1Brain plays demo1 without cheats on a realtime host
// instance with the pipeline in realtime mode (the backend's answers
// arrive while the game runs; a few minutes of wall time: Q2_AGENT_LONG=1).
func TestInProcDemo1Brain(t *testing.T) {
	if os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("realtime: minutes of wall time (set Q2_AGENT_LONG=1)")
	}
	fs := sessiontest.DemoFS(t)
	inst, err := session.NewInstance(host.New(), session.InstanceConfig{ID: "bot-brain", FS: fs, Spec: session.Spec{Skill: 1}, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Stop)
	s := session.NewInProc(inst, session.InProcConfig{FS: fs, Client: fakeclient.Options{MaxHistory: 256}})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := bot.NewBrain(bot.BrainConfig{Seed: 1, Mode: decide.Realtime})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	cfg := Config{Campaign: demoCampaign(t), Library: demoLibrary(t), StopAfter: 1, EpisodeTimeout: 10 * time.Minute, MaxDeaths: gateMaxDeaths,
		Bot: bot.Config{Policy: p}, Logf: t.Logf}
	res, err := Run(ctx, s, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("InProc demo1 (realtime brain): %s (%s) in %.1fs, %d deaths", res.Outcome, res.Reason, float64(res.WallMs)/1000, res.Deaths)
	wantExit(t, res, "demo1", "demo2")
}
