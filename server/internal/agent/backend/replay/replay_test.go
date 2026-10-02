package replay_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/replay"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

const frames = 60

// belief is a synthetic fight: a soldier circles the bot from 0.5 s to
// 4 s, a gunner joins from 1.5 s to 3 s, rockets fly now and then, and the
// bot loses health. perturb changes the soldier's position from that
// frame on (a different run).
func belief(i, perturb int) *worldmodel.Belief {
	now := int64(i * 100)
	b := &worldmodel.Belief{Level: worldmodel.LevelKey{Map: "demo1"}, Map: "demo1", Time: now, Frames: i + 1,
		Self: worldmodel.Self{Origin: worldmodel.Vec3{0, 0, 24}, Eye: worldmodel.Vec3{0, 0, 46}, ViewAngles: worldmodel.Vec3{0, float32(i * 7 % 360), 0},
			Health: 100 - i, Ammo: 20, Weapon: "Shotgun", OnGround: true}}
	mon := perception.KindMonster.String()
	if i >= 5 && i < 40 {
		a := float64(i) * 0.2
		r := 150 + 20*float64(i%10)
		if perturb > 0 && i >= perturb {
			r += 400
		}
		b.Tracks = append(b.Tracks, worldmodel.Track{ID: "e1", Class: "soldier", Kind: mon, PosKnown: true, Visible: true, Shootable: true,
			Pos: worldmodel.Vec3{float32(r * math.Cos(a)), float32(r * math.Sin(a)), 24}, Mins: worldmodel.Vec3{-16, -16, -24}, Maxs: worldmodel.Vec3{16, 16, 32},
			LastSeen: now, LastUpdate: now, FirstSeen: 500, Awareness: worldmodel.Attacking, Threat: 9, Confidence: 1})
	}
	if i >= 15 && i < 30 {
		b.Tracks = append(b.Tracks, worldmodel.Track{ID: "e2", Class: "gunner", Kind: mon, PosKnown: true, Visible: i%4 != 0, Shootable: true,
			Pos: worldmodel.Vec3{-500, 300, 24}, Mins: worldmodel.Vec3{-16, -16, -24}, Maxs: worldmodel.Vec3{16, 16, 32},
			LastSeen: now, LastUpdate: now, FirstSeen: 1500, Awareness: worldmodel.Alert, Threat: 14, Confidence: 1})
	}
	if i%10 == 3 {
		b.Projectiles = []worldmodel.Projectile{{ID: "p1", Class: "rocket", Pos: worldmodel.Vec3{0, 100, 40}, TCA: 0.3, Danger: true, DodgeSide: -1}}
	}
	return b
}

// run drives a lockstep pipeline over the synthetic fight, writing every
// record to a trace file; it returns the intents and the decision events.
func run(t *testing.T, backend decide.DecisionBackend, lat decide.LatencyModel, perturb int) ([]decide.Intent, []trace.Event) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "trace.jsonl.gz")
	sink, err := trace.CreateFile(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	bus := trace.NewBus("r1", func() time.Time { return time.Unix(0, 0) })
	bus.AddSink(sink)
	p, err := decide.NewPipeline(decide.PipelineConfig{
		Backend:   backend,
		Fallback:  scripted.New(scripted.Config{Seed: 1}),
		Scheduler: decide.SchedulerConfig{Mode: decide.Lockstep, SimLatency: lat},
		OnRecord: func(r *decide.Record) {
			bus.Publish(trace.Event{Type: trace.TypeDecision, GMs: r.Result.Arrived, Map: "demo1", Body: r.Decision(decide.RecordOptions{})})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var intents []decide.Intent
	for i := 0; i < frames; i++ {
		intents = append(intents, p.Tick(int64(i*100), belief(i, perturb), nil))
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	bus.Close()
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	events, err := trace.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return intents, events
}

// hangTimeout is the noisy client's attempt timeout: a hung first attempt
// becomes the client's own timeout (a fast-lane failure; the slow lane
// retries).
const hangTimeout = 100 * time.Millisecond

func noisyJev(t *testing.T) *jev.Client {
	t.Helper()
	base := jevtest.NewScripted(scripted.Config{Seed: 1})
	base.Now = func(*jevtest.Call) int64 { return 0 }
	hang := func(c *jevtest.Call) (jevtest.Fault, bool) {
		// the first attempts of the requests whose digest starts with "0"
		// (one in sixteen, chosen by content) hang
		return jevtest.FaultHang, c.Attempt == 0 && len(c.Digest) > 0 && c.Digest[0] == '0'
	}
	srv := jevtest.NewServer(jevtest.Options{APIKey: "k", Policy: &jevtest.Noisy{Base: base, Seed: 3},
		Faults: jevtest.Faults{Seed: 3, Server: 0.05, Missing: 0.05, Func: hang}})
	t.Cleanup(srv.Close)
	c, err := jev.New(jev.Config{BaseURL: srv.URL(), APIKey: trace.NewSecret("k"), AllowCustomBase: true, RatePerSec: -1,
		FastTimeout: hangTimeout, SlowTimeout: hangTimeout})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func comparable(t *testing.T, events []trace.Event) []string {
	t.Helper()
	var out []string
	for _, e := range events {
		var d trace.Decision
		if err := e.DecodeBody(&d); err != nil {
			t.Fatal(err)
		}
		d.Backend = "" // jev vs replay
		e.Body = d
		b, err := trace.Comparable(e)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(b))
	}
	return out
}

// TestRoundTrip records a noisy mock-Jev run and replays it from the trace
// file: the same intents and the same decision events.
func TestRoundTrip(t *testing.T) {
	lat := &decide.SampledLatency{Seed: 2, Samples: []time.Duration{90 * time.Millisecond, 212 * time.Millisecond, 350 * time.Millisecond, 900 * time.Millisecond}}
	want, events := run(t, noisyJev(t), lat, 0)
	errs, models, ownTimeouts := 0, 0, 0
	for _, e := range events {
		var d trace.Decision
		_ = e.DecodeBody(&d)
		if d.Err != "" {
			errs++
		}
		// the backend's own timeout (not a simulated latency beyond the
		// lane's timeout): it must replay as a timeout, not a plain error
		if d.Timeout && d.Err != "" && d.LatencyMs <= float64(decide.DefaultFastTimeout/time.Millisecond) {
			ownTimeouts++
		}
		for _, f := range d.Fields {
			if f.Source == trace.SourceModel {
				models++
			}
		}
	}
	if len(events) < 40 || errs == 0 || models == 0 || ownTimeouts == 0 {
		t.Fatalf("%d decision events, %d errors (%d client timeouts), %d model fields", len(events), errs, ownTimeouts, models)
	}
	rb, err := replay.New(events, replay.Options{Strict: true})
	if err != nil {
		t.Fatal(err)
	}
	got, again := run(t, rb, rb, 0)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatal("replayed intents differ")
	}
	a, b := comparable(t, events), comparable(t, again)
	if len(a) != len(b) {
		t.Fatalf("%d events, replay %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("event %d differs:\n%s\n%s", i, a[i], b[i])
		}
	}
	st := rb.Stats()
	if rb.Divergence() != nil || st.Matched != st.Calls || st.Failures == 0 {
		t.Fatalf("divergence %v stats %+v", rb.Divergence(), st)
	}
	t.Logf("%d events (%d failed requests, %d client timeouts) replayed, stats %+v", len(events), errs, ownTimeouts, st)
}

// TestStrictDivergence: a run that differs from frame 20 on is reported
// at its first changed request; non-strict replay answers by Seq.
func TestStrictDivergence(t *testing.T) {
	sb := scripted.New(scripted.Config{Seed: 1})
	_, events := run(t, sb, decide.FixedLatency(200*time.Millisecond), 0)
	strict, _ := replay.New(events, replay.Options{Strict: true})
	run(t, strict, decide.FixedLatency(200*time.Millisecond), 20)
	d := strict.Divergence()
	if d == nil || d.Reason != "digest" || !errors.Is(d, replay.ErrDivergence) {
		t.Fatalf("divergence %v", d)
	}
	// the first request built at frame 20 or later (fast every frame,
	// slow every 5 frames: seq 1 + 20 fast + 4-5 slow)
	var first uint64
	for _, e := range events {
		var dec trace.Decision
		_ = e.DecodeBody(&dec)
		if dec.SnapGMs >= 2000 && (first == 0 || dec.Req < first) && dec.Lane == "fast" {
			first = dec.Req
		}
	}
	if d.Seq > first || d.Seq == 0 {
		t.Fatalf("first divergence at %d, the run changes at request %d", d.Seq, first)
	}

	loose, _ := replay.New(events, replay.Options{})
	run(t, loose, decide.FixedLatency(200*time.Millisecond), 20)
	if st := loose.Stats(); st.SeqFallback == 0 || st.Matched == 0 || loose.Divergence() == nil {
		t.Fatalf("non-strict stats %+v", st)
	}
}

func TestRecordedFailuresAndMissing(t *testing.T) {
	q := []decide.Question{{ID: "mode", Type: decide.Choice, Instructions: "i", Options: []decide.Option{{Key: "fight"}, {Key: "explore"}}}}
	qs, _ := decide.MarshalQuestions(q)
	state := json.RawMessage(`{"me":{}}`)
	dig := decide.RequestDigest(state, qs)
	ev := func(seq uint64, body trace.Decision) trace.Event {
		body.Req, body.Lane = seq, "slow"
		return trace.Event{Type: trace.TypeDecision, Seq: seq, Body: body}
	}
	raw, _ := decide.MarshalResponse("m", q, map[string]decide.Answer{"mode": decide.OneHot(&q[0], "fight")}, decide.Usage{InputTokens: 9})
	events := []trace.Event{
		ev(1, trace.Decision{ReqDigest: dig, Response: raw, LatencyMs: 120, CostUSD: 0.5}),
		ev(2, trace.Decision{ReqDigest: dig, Err: "jev: server (status 500)"}),
		ev(4, trace.Decision{ReqDigest: dig, Err: "jev: timeout: context deadline exceeded", Timeout: true, LatencyMs: 300}),
		{Type: trace.TypeAPICall, Seq: 3},
	}
	b, err := replay.New(events, replay.Options{})
	if err != nil || b.Len() != 3 {
		t.Fatalf("%v %d", err, b.Len())
	}
	req := &decide.Request{Seq: 1, Lane: decide.LaneSlow, State: state, Questions: q}
	resp, err := b.Decide(context.Background(), req)
	if err != nil || resp.Answers["mode"].Choice != "fight" || resp.Latency != 120*time.Millisecond || resp.CostUSD != 0.5 || b.Latency(req) != 120*time.Millisecond {
		t.Fatalf("replayed %+v %v", resp, err)
	}
	var re *replay.RecordedError
	if _, err := b.Decide(context.Background(), &decide.Request{Seq: 2, Lane: decide.LaneSlow, State: state, Questions: q}); !errors.As(err, &re) ||
		errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("recorded failure: %v", err)
	}
	// a recorded timeout is a timeout again, with the recorded message
	if _, err := b.Decide(context.Background(), &decide.Request{Seq: 4, Lane: decide.LaneSlow, State: state, Questions: q}); !errors.Is(err, context.DeadlineExceeded) ||
		err.Error() != "jev: timeout: context deadline exceeded" {
		t.Fatalf("recorded timeout: %v", err)
	}
	if _, err := b.Decide(context.Background(), &decide.Request{Seq: 9, Lane: decide.LaneSlow, State: state, Questions: q}); !errors.Is(err, replay.ErrDivergence) {
		t.Fatalf("missing request: %v", err)
	}
	if d := b.Divergence(); d == nil || d.Seq != 9 || d.Reason != "missing" {
		t.Fatalf("divergence %v", d)
	}
	// non-strict: a lane mismatch still answers by Seq; the divergence of
	// the lowest Seq is the one reported
	if _, err := b.Decide(context.Background(), &decide.Request{Seq: 1, Lane: decide.LaneFast, State: state, Questions: q}); err != nil {
		t.Fatalf("non-strict lane mismatch: %v", err)
	}
	if d := b.Divergence(); d == nil || d.Seq != 1 || d.Reason != "lane" {
		t.Fatalf("lowest divergence %v", d)
	}
	if _, err := replay.New(append(events, ev(1, trace.Decision{})), replay.Options{}); err == nil {
		t.Fatal("duplicate request accepted")
	}
	// other episodes are ignored
	other := ev(5, trace.Decision{})
	other.Ep = 1
	if b, _ := replay.New([]trace.Event{other}, replay.Options{}); b.Len() != 0 {
		t.Fatal("episode filter")
	}
}
