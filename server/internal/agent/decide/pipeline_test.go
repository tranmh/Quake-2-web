package decide

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"quake2web/server/internal/agent/worldmodel"
)

// runPipeline ticks a lockstep pipeline over the fixture belief for n
// frames of 100 ms; the level changes at changeAt (0: never).
func runPipeline(t *testing.T, n, changeAt int) ([]Intent, []*Record, PipelineStats) {
	t.Helper()
	model := constBackend(map[string]string{QTarget: "e1", QFirePolicy: "fire_when_aligned", QMovement: "strafe_left",
		QMode: "fight", QWeapon: "machinegun", QPickup: "i1", QDanger: "2"})
	var recs []*Record
	p, err := NewPipeline(PipelineConfig{
		Backend:   model,
		Fallback:  constBackend(fallbackKeys),
		Projector: ProjectorConfig{Space: fixedSpace{300, 300, 300, 300}},
		Scheduler: SchedulerConfig{Mode: Lockstep, SimLatency: FixedLatency(212 * time.Millisecond)},
		OnRecord:  func(r *Record) { recs = append(recs, r) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var out []Intent
	b := testBelief()
	for i := 0; i < n; i++ {
		now := int64(10000 + 100*i)
		b.Time, b.Frames = now, 100+i
		if changeAt > 0 && i == changeAt {
			b.Level, b.Map = worldmodel.LevelKey{Map: "demo2"}, "demo2"
		}
		out = append(out, p.Tick(now, b, testObjective()))
	}
	return out, recs, p.Stats()
}

func TestPipelineLockstep(t *testing.T) {
	intents, recs, st := runPipeline(t, 30, 0)
	if len(intents) != 30 {
		t.Fatal(len(intents))
	}
	// before the first answer (due 212 ms later, collected at 300 ms) the
	// fallback decides; afterwards the model
	if p := intents[0].Provenance.Target; p.Source != SourceScripted || intents[0].Target != "e2" {
		t.Fatalf("tick 0: %q %+v", intents[0].Target, p)
	}
	// target has no hold time; movement switches once held 0.4 s
	for i := 3; i < 30; i++ {
		in := intents[i]
		if in.Provenance.Target.Source != SourceModel || in.Target != "e1" {
			t.Fatalf("tick %d: %+v", i, in)
		}
		if i >= 4 && (in.Provenance.Movement.Source != SourceModel || in.Movement != MoveStrafeLeft) {
			t.Fatalf("tick %d: movement %s %+v", i, in.Movement, in.Provenance.Movement)
		}
	}
	if p := intents[3].Provenance.Movement; p.Source != SourceScripted || p.Reason != "held" {
		t.Fatalf("tick 3 movement %+v", p)
	}
	// mode: the fallback said objective at 0 ms; the model's fight needs
	// the 1.5 s hold
	if intents[10].Mode != ModeObjective || intents[16].Mode != ModeFight {
		t.Fatalf("mode at 1.0 s %s, at 1.6 s %s", intents[10].Mode, intents[16].Mode)
	}
	// cadence: fast every frame, slow every 500 ms (+ the first-tick event)
	if st.Requests[LaneFast] != 30 || st.Requests[LaneSlow] < 6 || st.Requests[LaneSlow] > 8 {
		t.Fatalf("requests %v", st.Requests)
	}
	if len(recs) == 0 || st.Records != len(recs) {
		t.Fatalf("records %d, stats %d", len(recs), st.Records)
	}
	d := recs[0].Decision(RecordOptions{State: true, Questions: true})
	if d.Lane != "fast" || d.Req != 1 || d.Backend != "const" || d.LatencyMs != 212 || d.ReqDigest == "" || d.StateDigest == "" ||
		!json.Valid(d.State) || !json.Valid(d.Questions) || len(d.Fields) != 3 || !d.Combat {
		t.Fatalf("decision %+v", d)
	}
	if f := d.Fields[0]; f.Name != "target" || f.Value != "e1" || f.Source != "model" || f.Scripted != "e2" {
		t.Fatalf("field %+v", f)
	}
	if c := recs[0].APICall(); c.Lane != "fast" || c.Req != 1 || c.LatencyMs != 212 || c.Err != "" {
		t.Fatalf("api call %+v", c)
	}
	ast := st.Arbiter.Fields[FieldTarget]
	if ast.Comparisons == 0 || ast.Disagreements != ast.Comparisons || ast.Ticks[SourceModel] != 27 {
		t.Fatalf("target stats %+v", ast)
	}

	// the same run again: identical intents
	again, _, _ := runPipeline(t, 30, 0)
	if fmt.Sprint(again) != fmt.Sprint(intents) {
		t.Fatal("lockstep pipeline not deterministic")
	}
}

// TestPipelineLevelChange: a new level forgets the answers and drops the
// results of requests asked before it.
func TestPipelineLevelChange(t *testing.T) {
	intents, recs, st := runPipeline(t, 20, 10)
	if p := intents[10].Provenance.Target; p.Source != SourceScripted {
		t.Fatalf("first tick of the new level: %+v", p)
	}
	old := 0
	for _, r := range recs {
		if r.Applied.Dropped == "old_level" {
			old++
		}
	}
	if old == 0 || st.OldLevel != old {
		t.Fatalf("old-level results %d, stats %d", old, st.OldLevel)
	}
	if intents[19].Provenance.Target.Source != SourceModel {
		t.Fatalf("model answers resume: %+v", intents[19].Provenance.Target)
	}
}

func TestPipelineNeedsBackend(t *testing.T) {
	if _, err := NewPipeline(PipelineConfig{}); err != ErrNoBackend {
		t.Fatal(err)
	}
}

// TestPipelineRealtimeNeverBlocks: with a backend that never answers, Tick
// returns at once and the fallback decides.
func TestPipelineRealtimeNeverBlocks(t *testing.T) {
	g := newGate()
	p, err := NewPipeline(PipelineConfig{Backend: g, Fallback: constBackend(fallbackKeys),
		// no call ends before the end of the test (the fast lane's calls are
		// also cut at StaleAfter)
		Scheduler: SchedulerConfig{FastTimeout: time.Hour, SlowTimeout: time.Hour, StaleAfter: func(Lane) time.Duration { return time.Hour }}})
	if err != nil {
		t.Fatal(err)
	}
	b := testBelief()
	start := time.Now()
	var in Intent
	for i := 0; i < 40; i++ {
		now := int64(10000 + 25*i)
		b.Time, b.Frames = now, 100+i
		in = p.Tick(now, b, nil)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("40 ticks took %v with a blocked backend", d)
	}
	if in.Target != "e2" || in.Provenance.Target.Source != SourceScripted {
		t.Fatalf("intent %+v", in.Provenance.Target)
	}
	st := p.Stats()
	if st.Scheduler.Lanes[LaneFast].Dropped == 0 || p.Scheduler().InFlight(LaneFast) != p.Scheduler().MaxInFlight(LaneFast) {
		t.Fatalf("in-flight cap not reached: %+v", st.Scheduler.Lanes)
	}
	if p.Tick(20000, nil, nil) != p.Intent() {
		t.Fatal("nil belief")
	}
	close(g.release)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
}

type statusErr struct{ code int }

func (e statusErr) Error() string   { return "status" }
func (e statusErr) HTTPStatus() int { return e.code }

func TestRecordAPICallStatus(t *testing.T) {
	req := &Request{Seq: 4, Lane: LaneSlow, State: []byte(`{}`), View: &State{}}
	r := &Record{Backend: "jev", Result: Result{Req: req, Err: fmt.Errorf("wrapped: %w", statusErr{429}), Latency: 5 * time.Millisecond}}
	c := r.APICall()
	if c.Status != 429 || c.Err == "" || c.Lane != "slow" || c.LatencyMs != 5 || c.Combat {
		t.Fatalf("%+v", c)
	}
	r.Result = Result{Req: req, Resp: &Response{Model: "m", Status: 200, Retries: 1, Usage: Usage{InputTokens: 10}, CostUSD: 0.1}}
	if c := r.APICall(); c.Status != 200 || c.Retry != 1 || c.InputTokens != 10 || c.CostUSD != 0.1 || c.Model != "m" {
		t.Fatalf("%+v", c)
	}
	if d := r.Decision(RecordOptions{}); d.State != nil || d.Questions != nil || d.Model != "m" || d.ReqDigest != req.Digest() || d.Timeout {
		t.Fatalf("%+v", d)
	}
	r.Result = Result{Req: req, Err: context.DeadlineExceeded, Timeout: true, Latency: 300 * time.Millisecond}
	if d := r.Decision(RecordOptions{}); !d.Timeout || d.Err != context.DeadlineExceeded.Error() || d.LatencyMs != 300 {
		t.Fatalf("timeout decision %+v", d)
	}
}

// TestPipelineSlowEventWhileBusy: an event while the slow lane's one slot
// is busy is kept and asked as soon as the slot frees, and it does not
// push the next regular slow request later.
func TestPipelineSlowEventWhileBusy(t *testing.T) {
	var slow []int64
	p, err := NewPipeline(PipelineConfig{
		Backend:   constBackend(nil),
		Projector: ProjectorConfig{Space: fixedSpace{300, 300, 300, 300}},
		Scheduler: SchedulerConfig{Mode: Lockstep, SimLatency: FixedLatency(212 * time.Millisecond)},
		OnRecord: func(r *Record) {
			if r.Result.Req.Lane == LaneSlow {
				slow = append(slow, r.Result.Req.SnapTime)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b := testBelief()
	for i := 0; i <= 12; i++ {
		now := int64(fixtureNow + 100*i)
		b.Time, b.Frames = now, 100+i
		if i == 2 { // damage at +200 ms, while the first slow request (due +212) is in flight
			b.Damage = append(b.Damage, worldmodel.DamageEvent{At: now, Health: 6, Cause: "hit"})
		}
		p.Tick(now, b, testObjective())
	}
	// +0 (first tick), +300 (the event, once the slot is free), +800
	want := fmt.Sprint([]int64{fixtureNow, fixtureNow + 300, fixtureNow + 800})
	if got := fmt.Sprint(slow); got != want {
		t.Fatalf("slow requests at %s, want %s", got, want)
	}
	if st := p.Stats(); st.Scheduler.Lanes[LaneSlow].Dropped != 1 || st.Requests[LaneSlow] != 4 {
		t.Fatalf("stats %+v requests %v", st.Scheduler.Lanes[LaneSlow], st.Requests)
	}
}
