package decide_test

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

type vec = worldmodel.Vec3

// route is a path through demo1 from the start that a free walker passed
// (the worldmodel tests' route): the start corridor, the first room, down
// the ramp to the soldiers.
var route = []vec{
	{153, -316, 0}, {207, -290, 0}, {236, -264, 0}, {236, -222, 0}, {240, -167, 0}, {240, -103, 0},
	{240, -32, 0}, {234, -27, 0}, {213, 27, 0}, {187, 82, 0}, {160, 112, 0}, {126, 112, 0}, {79, 120, 0},
	{17, 120, 0}, {-46, 120, 0}, {-49, 106, 0}, {-60, 88, 0}, {-81, 64, 0}, {-111, 32, 0}, {-156, 16, 0},
	{-208, 16, 0}, {-270, 2, 0}, {-327, -35, 0}, {-352, -92, 0}, {-358, -152, 0}, {-357, -176, 0},
	{-348, -191, 0}, {-324, -245, 0}, {-299, -301, 0}, {-296, -354, 0}, {-296, -394, 0}, {-296, -427, 0},
	{-286, -419, 0}, {-326, -416, 0}, {-371, -388, 0}, {-419, -352, 0}, {-448, -320, 0}, {-454, -285, 0},
	{-487, -254, 0}, {-546, -246, 0}, {-605, -239, 0},
}

// walker turns in place once, then runs along the route (jumping when it
// makes no progress), firing a short burst every 3 s to wake the monsters,
// and at the end turns slowly. It depends only on the client state and
// the command count: a deterministic turning walker.
func walker() session.CmdFunc {
	n, wp := 0, 0
	var last vec
	yaw := float32(135)
	const turn = 90
	return func(c *fakeclient.Client, msec int) shared.UserCmd {
		n++
		delta := c.Frame.PlayerState.PMove.DeltaAngles
		var cmd shared.UserCmd
		switch o := c.Origin(); {
		case n <= turn:
			yaw = 135 + float32(n)*360/turn
		case wp < len(route):
			for wp < len(route) && math.Hypot(float64(route[wp][0]-o[0]), float64(route[wp][1]-o[1])) < 24 {
				wp++
			}
			if wp == len(route) {
				break
			}
			yaw = float32(math.Atan2(float64(route[wp][1]-o[1]), float64(route[wp][0]-o[0])) * 180 / math.Pi)
			cmd.ForwardMove = 300
			if n%40 == 0 {
				if math.Hypot(float64(o[0]-last[0]), float64(o[1]-last[1])) < 8 {
					cmd.UpMove = 200
				}
				last = o
			}
		default:
			yaw++
		}
		if n > turn && n%120 < 4 {
			cmd.Buttons = q2const.BUTTON_ATTACK
		}
		cmd.Angles[q2const.YAW] = int16(shared.ANGLE2SHORT(yaw) - int32(delta[q2const.YAW]))
		cmd.Angles[q2const.PITCH] = int16(-int32(delta[q2const.PITCH]))
		return cmd
	}
}

// smokeFrames is the run length: the soldier down the ramp notices the
// walker after about 30 s and fights it.
const smokeFrames = 450

type smokeRun struct {
	intents  []decide.Intent
	combat   int // ticks whose fast state had enemies or projectiles
	stats    decide.PipelineStats
	records  []*decide.Record
	events   []string // comparable decision events
	badFast  int      // fast requests built without combat
	backend  string
	duration time.Duration
}

// runSmoke drives the lockstep demo1 session (seed 1, skill 1) with the
// walker, feeds the world model's beliefs to a pipeline on backend and
// returns what happened.
func runSmoke(t *testing.T, fs *pak.FS, backend decide.DecisionBackend, answerSource decide.Source, lat decide.LatencyModel) *smokeRun {
	t.Helper()
	start := time.Now()
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1,
		Client: fakeclient.Options{MaxHistory: 256}})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	raw, err := fs.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load("demo1", raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	w := worldmodel.New(worldmodel.Config{ReadFile: fs.ReadFile})
	w.Reset(worldmodel.Level{Key: worldmodel.LevelKey{Map: "demo1"}, Map: md})

	run := &smokeRun{backend: backend.Name()}
	pcfg := decide.ProjectorConfig{Space: decide.NewTraceSpace(md.CM, 256)}
	probe := decide.NewProjector(decide.ProjectorConfig{})
	p, err := decide.NewPipeline(decide.PipelineConfig{
		Backend:   backend,
		Fallback:  scripted.New(scripted.Config{Seed: 1}),
		Projector: pcfg,
		Scheduler: decide.SchedulerConfig{Mode: decide.Lockstep, SimLatency: lat},
		Arbiter:   decide.ArbiterConfig{AnswerSource: answerSource},
		OnRecord: func(r *decide.Record) {
			run.records = append(run.records, r)
			if r.Result.Req.Lane == decide.LaneFast && len(r.Result.Req.View.Enemies) == 0 && len(r.Result.Req.View.Incoming) == 0 {
				run.badFast++
			}
			e := trace.Event{Type: trace.TypeDecision, GMs: r.Result.Arrived, Body: r.Decision(decide.RecordOptions{State: true})}
			b, err := trace.Comparable(e)
			if err != nil {
				t.Fatal(err)
			}
			run.events = append(run.events, string(b))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	r := perception.NewReader()
	walk := walker()
	for i := 0; i <= smokeFrames; i++ {
		if i > 0 {
			if err := l.Step(ctx, walk); err != nil {
				t.Fatal(err)
			}
		}
		in, ok := r.Next(l.Client())
		if !ok {
			continue
		}
		now := l.GameTimeMs()
		w.Update(in, now)
		b := w.Belief()
		fast := probe.Fast(b, decide.Context{Target: p.Intent().Target})
		if len(fast.Enemies) > 0 || len(fast.Incoming) > 0 {
			run.combat++
		}
		run.intents = append(run.intents, p.Tick(now, b, nil))
	}
	run.stats = p.Stats()
	run.duration = time.Since(start)
	return run
}

func validIntent(in decide.Intent) error {
	ok := func(v string, allowed ...string) bool {
		for _, a := range allowed {
			if v == a {
				return true
			}
		}
		return false
	}
	switch {
	case !ok(string(in.Mode), "fight", "objective", "pickup", "retreat", "explore"):
		return fmt.Errorf("mode %q", in.Mode)
	case !ok(string(in.FirePolicy), "hold", "fire_when_aligned", "suppress"):
		return fmt.Errorf("fire_policy %q", in.FirePolicy)
	case !ok(string(in.Movement), "advance", "retreat", "strafe_left", "strafe_right", "hold"):
		return fmt.Errorf("movement %q", in.Movement)
	case in.Weapon != "" && !in.Weapon.Known():
		return fmt.Errorf("weapon %q", in.Weapon)
	case in.Danger < 0 || in.Danger > 4:
		return fmt.Errorf("danger %v", in.Danger)
	case in.Target != "" && !strings.HasPrefix(in.Target, "e"):
		return fmt.Errorf("target %q", in.Target)
	}
	return nil
}

// checkRun asserts an Intent every tick, the lane cadence and the
// provenance accounting.
func checkRun(t *testing.T, r *smokeRun) {
	t.Helper()
	n := len(r.intents)
	if n < smokeFrames-10 || r.stats.Ticks != n {
		t.Fatalf("%s: %d intents for %d ticks", r.backend, n, r.stats.Ticks)
	}
	for i, in := range r.intents {
		if err := validIntent(in); err != nil {
			t.Fatalf("%s tick %d: %v", r.backend, i, err)
		}
		if i > 0 && in.Time <= r.intents[i-1].Time {
			t.Fatalf("%s tick %d: time %d after %d", r.backend, i, in.Time, r.intents[i-1].Time)
		}
	}
	fast, slow := r.stats.Requests[decide.LaneFast], r.stats.Requests[decide.LaneSlow]
	// fast: only in combat, at 10 Hz (5 Hz while the enemies are only
	// remembered); slow: 2 Hz plus events
	if r.combat == 0 || fast == 0 || fast > r.combat || fast < r.combat/2 || r.badFast != 0 {
		t.Fatalf("%s: %d fast requests for %d combat ticks (%d without combat)", r.backend, fast, r.combat, r.badFast)
	}
	if slow < n/5 || slow > n/2 {
		t.Fatalf("%s: %d slow requests for %d ticks", r.backend, slow, n)
	}
	a := r.stats.Arbiter
	for f := decide.Field(0); f < decide.NumFields; f++ {
		sum := 0
		for _, c := range a.Fields[f].Ticks {
			sum += c
		}
		if sum != n {
			t.Fatalf("%s %s: provenance of %d ticks, want %d", r.backend, f.ID(), sum, n)
		}
	}
	sch := r.stats.Scheduler
	if got := sch.Lanes[decide.LaneFast].Completed + sch.Lanes[decide.LaneSlow].Completed; got != len(r.records) || r.stats.Records != got {
		t.Fatalf("%s: %d completed, %d records", r.backend, got, len(r.records))
	}
}

func share(a decide.ArbiterStats, f decide.Field, s decide.Source) float64 {
	return a.Fields[f].Share(s)
}

// decided is the share of src among the ticks a field was decided (not
// its default: for the fast fields, the combat ticks).
func decided(a decide.ArbiterStats, f decide.Field, src decide.Source) float64 {
	t := a.Fields[f].Ticks
	n := 0
	for s, c := range t {
		if decide.Source(s) != decide.SourceDefault {
			n += c
		}
	}
	if n == 0 {
		return 0
	}
	return float64(t[src]) / float64(n)
}

func TestPipelineDemo1Scripted(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	lat := &decide.SampledLatency{Seed: 1, Samples: []time.Duration{80 * time.Millisecond, 150 * time.Millisecond, 212 * time.Millisecond, 300 * time.Millisecond}}
	a := runSmoke(t, fs, scripted.New(scripted.Config{Seed: 1}), decide.SourceScripted, lat)
	checkRun(t, a)
	for f := decide.Field(0); f < decide.NumFields; f++ {
		if m := share(a.stats.Arbiter, f, decide.SourceModel); m != 0 {
			t.Errorf("scripted backend: %s has model provenance %v", f.ID(), m)
		}
	}
	if share(a.stats.Arbiter, decide.FieldTarget, decide.SourceScripted) == 0 || a.stats.Arbiter.Fields[decide.FieldTarget].Disagreements != 0 {
		t.Errorf("scripted target provenance %+v", a.stats.Arbiter.Fields[decide.FieldTarget])
	}
	t.Logf("scripted: %d ticks, %d combat, requests %v, %v", len(a.intents), a.combat, a.stats.Requests, a.duration)

	sameRuns(t, a, runSmoke(t, fs, scripted.New(scripted.Config{Seed: 1}), decide.SourceScripted, lat))
}

// mockJev returns a jev client on a fresh fake server with the noisy
// policy: 10% unconfident answers, some second-best picks, 2% server
// errors and 2% missing answers, all drawn from the request content.
func mockJev(t *testing.T) (*jev.Client, *jevtest.Server) {
	t.Helper()
	srv := jevtest.NewServer(jevtest.Options{APIKey: "k", Policy: &jevtest.Noisy{Base: jevtest.NewScripted(scripted.Config{Seed: 1}), Seed: 5},
		Faults: jevtest.Faults{Seed: 5, Server: 0.02, Missing: 0.02}})
	t.Cleanup(srv.Close)
	c, err := jev.New(jev.Config{BaseURL: srv.URL(), APIKey: trace.NewSecret("k"), AllowCustomBase: true, RatePerSec: -1})
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func sameRuns(t *testing.T, a, b *smokeRun) {
	t.Helper()
	if len(a.intents) != len(b.intents) || len(a.events) != len(b.events) {
		t.Fatalf("%s runs differ in length: %d/%d intents, %d/%d events", a.backend, len(a.intents), len(b.intents), len(a.events), len(b.events))
	}
	for i := range a.intents {
		if fmt.Sprint(a.intents[i]) != fmt.Sprint(b.intents[i]) {
			t.Fatalf("%s tick %d: %+v vs %+v", a.backend, i, a.intents[i], b.intents[i])
		}
	}
	for i := range a.events {
		if a.events[i] != b.events[i] {
			t.Fatalf("%s decision %d differs:\n%s\n%s", a.backend, i, a.events[i], b.events[i])
		}
	}
}

// TestPipelineDemo1MockJev runs the pipeline on the jev client against the
// noisy fake server (lockstep, SimLatency 212 ms): the plan's provenance
// gate holds and, since the fake answers depend on the content only, a
// second run repeats the first.
func TestPipelineDemo1MockJev(t *testing.T) {
	if testing.Short() && os.Getenv("Q2_AGENT_LONG") != "1" {
		t.Skip("lockstep mock-Jev smoke; runs without -short")
	}
	fs := sessiontest.DemoFS(t)
	c, srv := mockJev(t)
	r := runSmoke(t, fs, c, decide.SourceModel, decide.FixedLatency(212*time.Millisecond))
	checkRun(t, r)
	a := r.stats.Arbiter
	// the plan's provenance gate (phase 9: >= 70% of target, fire_policy
	// and mode from the model) with the noisy model
	for _, f := range []decide.Field{decide.FieldTarget, decide.FieldFirePolicy, decide.FieldMode} {
		if m := decided(a, f, decide.SourceModel); m < 0.7 {
			t.Errorf("%s: model share %.2f of decided ticks (%+v)", f.ID(), m, a.Fields[f])
		}
	}
	low, disagree := 0, 0
	for f := decide.Field(0); f < decide.NumFields; f++ {
		low += a.Fields[f].LowConfidence
		disagree += a.Fields[f].Disagreements
	}
	if low == 0 || disagree == 0 || srv.Count() == 0 {
		t.Errorf("no model-like noise: %d low-confidence answers, %d disagreements, %d calls", low, disagree, srv.Count())
	}
	st := c.Stats()
	if st.OK == 0 || st.InputTokens == 0 || st.CostUSD <= 0 {
		t.Errorf("client stats %+v", st)
	}
	t.Logf("mock-Jev: %d ticks, %d combat, requests %v, calls %d, model share target %.2f fire %.2f mode %.2f, %d low-confidence, %d disagreements, $%.6f, %v",
		len(r.intents), r.combat, r.stats.Requests, srv.Count(), decided(a, decide.FieldTarget, decide.SourceModel),
		decided(a, decide.FieldFirePolicy, decide.SourceModel), decided(a, decide.FieldMode, decide.SourceModel), low, disagree, st.CostUSD, r.duration)
	for f := decide.Field(0); f < decide.NumFields; f++ {
		t.Logf("  %-11s ticks default/model/scripted/stale %v, %d accepted, %d unconfident", f.ID(), a.Fields[f].Ticks, a.Fields[f].Accepted, a.Fields[f].LowConfidence)
	}

	c2, _ := mockJev(t)
	sameRuns(t, r, runSmoke(t, fs, c2, decide.SourceModel, decide.FixedLatency(212*time.Millisecond)))
}
