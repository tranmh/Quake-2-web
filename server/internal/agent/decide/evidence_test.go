package decide

import (
	"math"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"

	"quake2web/server/internal/agent/worldmodel"
)

// noisyChoice is a model-like answer: most of the mass on key, the rest
// spread over the other options of q.
func noisyChoice(q *Question, key string, top, conf float64) Answer {
	probs := map[string]float64{}
	rest := (1 - top) / float64(max(len(q.Options)-1, 1))
	for _, o := range q.Options {
		probs[o.Key] = rest
	}
	probs[key] = top
	return choice(key, conf, probs)
}

// feeder answers lane requests of one belief at a fixed period.
type feeder struct {
	t   *testing.T
	a   *Arbiter
	b   *worldmodel.Belief
	seq uint64
}

// answer observes and applies a request at now answered with key for
// every question id in keys (top probability top, confidence conf) and
// returns the Intent at now.
func (fd *feeder) answer(l Lane, now int64, keys map[string]string, top, conf float64) Intent {
	fd.t.Helper()
	fd.seq++
	req := laneReq(fd.t, fd.seq, l, now, fd.b)
	ans := map[string]Answer{}
	for id, k := range keys {
		q := req.Question(id)
		if q == nil {
			fd.t.Fatalf("question %s not asked", id)
		}
		ans[id] = noisyChoice(q, k, top, conf)
	}
	step(fd.t, fd.a, req, ans)
	return fd.a.Intent(now, fd.b)
}

// A single swapped answer among consistent ones does not flip the
// decision (it does when each answer is taken alone).
func TestEvidenceSwapDoesNotFlip(t *testing.T) {
	good := map[string]string{QTarget: "e1", QFirePolicy: "fire_when_aligned", QMovement: "advance"}
	swap := map[string]string{QTarget: "e2", QFirePolicy: "hold", QMovement: "retreat"}
	for _, accumulate := range []bool{true, false} {
		cfg := ArbiterConfig{}
		if !accumulate {
			cfg.Tau = latestOnly()
		}
		fd := &feeder{t: t, a: newTestArbiter(cfg), b: testBelief()}
		now := int64(1000)
		for i := 0; i < 6; i++ {
			fd.answer(LaneFast, now, good, 0.85, 0.7)
			now += 100
		}
		in := fd.answer(LaneFast, now, swap, 0.85, 0.7)
		flipped := in.Target != "e1" || in.FirePolicy != FireWhenAligned || in.Movement != MoveAdvance
		if accumulate {
			if flipped {
				t.Fatalf("one swapped answer flipped the accumulated decision: %+v", in)
			}
			for _, f := range []Field{FieldTarget, FieldFirePolicy, FieldMovement} {
				if p := in.Provenance.Get(f); p.Source != SourceModel || p.Reason != "" {
					t.Errorf("%s after the swap: %+v", f.ID(), p)
				}
			}
			if c := in.Provenance.FirePolicy.Confidence; c < 0.5 || c > 0.85 {
				t.Errorf("the swap should lower the posterior, not decide: %v", c)
			}
		} else if !flipped {
			t.Fatalf("without accumulation the swap should flip: %+v", in)
		}
		now += 100
		if in := fd.answer(LaneFast, now, good, 0.85, 0.7); accumulate && (in.FirePolicy != FireWhenAligned || in.Target != "e1") {
			t.Fatalf("after the swap: %+v", in)
		}
	}

	// the slow lane's mode too
	fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
	now := int64(0)
	for i := 0; i < 5; i++ {
		fd.answer(LaneSlow, now, map[string]string{QMode: "fight"}, 0.85, 0.7)
		now += 500
	}
	if in := fd.answer(LaneSlow, now, map[string]string{QMode: "explore"}, 0.85, 0.7); in.Mode != ModeFight ||
		in.Provenance.Mode.Source != SourceModel {
		t.Fatalf("one swapped mode answer: %s %+v", in.Mode, in.Provenance.Mode)
	}
}

// A sustained change flips the decision within a bounded number of
// answers: three for the fast fields at 10 Hz, three for the mode at 2 Hz.
func TestEvidenceSustainedChangeFlips(t *testing.T) {
	type tc struct {
		lane     Lane
		period   int64
		from, to map[string]string
		check    func(Intent) bool
	}
	for name, c := range map[string]tc{
		"fire_policy": {LaneFast, 100, map[string]string{QFirePolicy: "hold"}, map[string]string{QFirePolicy: "fire_when_aligned"},
			func(in Intent) bool { return in.FirePolicy == FireWhenAligned }},
		"target": {LaneFast, 100, map[string]string{QTarget: "e1"}, map[string]string{QTarget: "e3"},
			func(in Intent) bool { return in.Target == "e3" }},
		"movement": {LaneFast, 100, map[string]string{QMovement: "hold"}, map[string]string{QMovement: "strafe_left"},
			func(in Intent) bool { return in.Movement == MoveStrafeLeft }},
		"mode": {LaneSlow, 500, map[string]string{QMode: "objective"}, map[string]string{QMode: "fight"},
			func(in Intent) bool { return in.Mode == ModeFight }},
	} {
		t.Run(name, func(t *testing.T) {
			fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
			now := int64(1000)
			for i := 0; i < 10; i++ {
				if in := fd.answer(c.lane, now, c.from, 0.85, 0.7); c.check(in) {
					t.Fatalf("decided the new value before the change: %+v", in)
				}
				now += c.period
			}
			for k := 1; ; k++ {
				in := fd.answer(c.lane, now, c.to, 0.85, 0.7)
				if c.check(in) {
					if k < 2 || k > 3 {
						t.Fatalf("flipped after %d answers, want 2..3", k)
					}
					var p FieldProvenance
					switch name {
					case "fire_policy":
						p = in.Provenance.FirePolicy
					case "target":
						p = in.Provenance.Target
					case "movement":
						p = in.Provenance.Movement
					default:
						p = in.Provenance.Mode
					}
					if p.Source != SourceModel || p.Reason != "" || p.Seq != fd.seq {
						t.Fatalf("provenance of the flip %+v (seq %d)", p, fd.seq)
					}
					return
				}
				if k == 3 {
					t.Fatalf("no flip after %d answers: %+v", k, in)
				}
				now += c.period
			}
		})
	}
}

// Low-confidence answers still count: alone one is too weak, but several
// that agree decide, and one among confident answers moves the posterior.
func TestEvidenceLowConfidenceCounts(t *testing.T) {
	fire := map[string]string{QFirePolicy: "suppress"}
	fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
	fd.a.Intent(0, fd.b) // the defaults, held since long before
	// weights 0.15 x (1, e^-1/3, e^-2/3, e^-1): 0.15, 0.26, 0.33, 0.39
	for k, now := 1, int64(1000); k <= 4; k, now = k+1, now+100 {
		in := fd.answer(LaneFast, now, fire, 1, 0.15)
		p := in.Provenance.FirePolicy
		if k < 4 && (p.Source != SourceScripted || p.Reason != "weak" || in.FirePolicy != FireHold) {
			t.Fatalf("%d low-confidence answers: %s %+v", k, in.FirePolicy, p)
		}
		if k == 4 && (p.Source != SourceModel || in.FirePolicy != FireSuppress) {
			t.Fatalf("4 low-confidence answers: %s %+v", in.FirePolicy, p)
		}
	}
	if st := fd.a.Stats().Fields[FieldFirePolicy]; st.Accepted != 4 || st.LowConfidence != 4 {
		t.Errorf("stats %+v", st)
	}

	// confident answers, then one low-confidence disagreement: the
	// decision holds, its posterior drops a little (less than a confident
	// disagreement would drop it)
	post := func(conf float64) float64 {
		fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
		now := int64(1000)
		for i := 0; i < 4; i++ {
			fd.answer(LaneFast, now, fire, 0.9, 0.8)
			now += 100
		}
		in := fd.answer(LaneFast, now, map[string]string{QFirePolicy: "hold"}, 0.9, conf)
		if in.FirePolicy != FireSuppress || in.Provenance.FirePolicy.Source != SourceModel {
			t.Fatalf("conf %v: %s %+v", conf, in.FirePolicy, in.Provenance.FirePolicy)
		}
		return in.Provenance.FirePolicy.Confidence
	}
	none, low, high := post(0), post(0.15), post(0.8)
	if !(none > low && low > high) {
		t.Fatalf("posterior of suppress: no weight %v, low %v, confident %v", none, low, high)
	}
}

// Options that leave the state are dropped: an enemy gone from the lane
// state, or dead, no longer gets the accumulated mass.
func TestEvidenceDropsVanishedOptions(t *testing.T) {
	for _, how := range []string{"gone", "dead"} {
		t.Run(how, func(t *testing.T) {
			b := testBelief()
			fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: b}
			now := int64(1000)
			for i := 0; i < 3; i++ {
				fd.seq++
				req := laneReq(t, fd.seq, LaneFast, now, b)
				q := req.Question(QTarget)
				probs := map[string]float64{}
				for _, o := range q.Options {
					probs[o.Key] = 0
				}
				probs["e1"], probs["e3"] = 0.6, 0.4
				step(t, fd.a, req, map[string]Answer{QTarget: choice("e1", 0.8, probs)})
				now += 100
			}
			if in := fd.a.Intent(now-100, b); in.Target != "e1" {
				t.Fatalf("before: %s", in.Target)
			}
			b2 := testBelief()
			switch how {
			case "gone":
				b2.Tracks = b2.Tracks[1:] // e1 is no longer known
				fd.seq++
				req := laneReq(t, fd.seq, LaneFast, now, b2)
				if req.OptionIndex.Has(QTarget, "e1") {
					t.Fatal("e1 still an option")
				}
				// an unconfident answer split between e3 and none: the
				// earlier answers' e1 mass is dropped, their e3 mass decides
				fd.a.Observe(req)
				if in := fd.a.Intent(now, b); in.Target != "e1" {
					t.Fatalf("a request in flight dropped e1: %q", in.Target)
				}
				step(t, fd.a, req, map[string]Answer{QTarget: choice("e3", 0.3, map[string]float64{"e3": 0.5, OptNone: 0.5})})
			case "dead":
				b2.Track("e1").Life = worldmodel.LifeDead
			}
			in := fd.a.Intent(now, b2)
			if in.Target != "e3" || in.Provenance.Target.Source != SourceModel || in.Provenance.Target.Confidence < 0.8 {
				t.Fatalf("after e1 left: %q %+v", in.Target, in.Provenance.Target)
			}
		})
	}
}

// All contributing answers expired: the scripted fallback, then the
// default; a lane that stops asking a question drops the field at once.
func TestEvidenceStale(t *testing.T) {
	fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
	for i, now := 0, int64(1000); i < 3; i, now = i+1, now+100 {
		fd.answer(LaneFast, now, map[string]string{QTarget: "e1"}, 0.85, 0.8)
	}
	// the newest answer's snapshot is 1200; the TTL is 300 ms
	if p := fd.a.Intent(1500, fd.b).Provenance.Target; p.Source != SourceModel {
		t.Fatalf("within the TTL: %+v", p)
	}
	if in := fd.a.Intent(1501, fd.b); in.Target != "e2" || in.Provenance.Target.Source != SourceScripted || in.Provenance.Target.Reason != "ttl" {
		t.Fatalf("past the TTL: %q %+v", in.Target, in.Provenance.Target)
	}
	if in := fd.a.Intent(1701, fd.b); in.Target != "" || in.Provenance.Target.Source != SourceDefault {
		t.Fatalf("past the fallback's TTL: %q %+v", in.Target, in.Provenance.Target)
	}
}

// A score's value is the weighted mean of the answers' levels.
func TestEvidenceScore(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	step(t, a, laneReq(t, 1, LaneSlow, 0, b), map[string]Answer{QDanger: score(1, 0.9)})
	step(t, a, laneReq(t, 2, LaneSlow, 500, b), map[string]Answer{QDanger: score(3, 0.9)})
	in := a.Intent(500, b)
	w := math.Exp(-1) // danger's tau is 500 ms
	want := (w*1 + 3) / (w + 1)
	if math.Abs(in.Danger-want) > 1e-9 || in.Provenance.Danger.Source != SourceModel || in.Provenance.Danger.Seq != 2 {
		t.Fatalf("danger %v (want %v) %+v", in.Danger, want, in.Provenance.Danger)
	}
}

// The scripted backend's answers are exact: decided from the latest one.
func TestEvidenceScriptedLatestOnly(t *testing.T) {
	a := NewArbiter(ArbiterConfig{AnswerSource: SourceScripted})
	for f := Field(0); f < NumFields; f++ {
		if a.Tau(f) >= 0 {
			t.Fatalf("%s accumulates (tau %v)", f.ID(), a.Tau(f))
		}
	}
	if d := NewArbiter(ArbiterConfig{}); d.Tau(FieldTarget) != 300*time.Millisecond || d.Tau(FieldMode) != time.Second {
		t.Fatalf("model defaults %v %v", d.Tau(FieldTarget), d.Tau(FieldMode))
	}
	fd := &feeder{t: t, a: a, b: testBelief()}
	fd.answer(LaneFast, 1000, map[string]string{QTarget: "e1"}, 1, 1)
	if in := fd.answer(LaneFast, 1100, map[string]string{QTarget: "e3"}, 1, 1); in.Target != "e3" || in.Provenance.Target.Source != SourceScripted {
		t.Fatalf("latest answer: %q %+v", in.Target, in.Provenance.Target)
	}
}

// While its accumulated answers decide the field, a value hysteresis
// keeps is the model's (not stale), even past the TTL of the answer that
// set it.
func TestEvidenceHeldIsModel(t *testing.T) {
	fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
	probs := func(e1 float64) map[string]float64 { return map[string]float64{"e1": e1, "e3": 1 - e1} }
	now := int64(1000)
	fd.seq++
	step(t, fd.a, laneReq(t, fd.seq, LaneFast, now, fd.b), map[string]Answer{QTarget: choice("e1", 0.9, probs(0.8))})
	fd.a.Intent(now, fd.b)
	// answers that prefer e3 by less than TargetDelta keep e1: first the
	// posterior still prefers e1, then hysteresis holds it, the model's
	// all along (past the TTL of the answer that last chose e1)
	var in Intent
	for i := 0; i < 10; i++ {
		now += 100
		fd.seq++
		step(t, fd.a, laneReq(t, fd.seq, LaneFast, now, fd.b), map[string]Answer{QTarget: choice("e3", 0.9, probs(0.45))})
		in = fd.a.Intent(now, fd.b)
		if in.Target != "e1" || in.Provenance.Target.Source != SourceModel {
			t.Fatalf("at %d: %q %+v", now, in.Target, in.Provenance.Target)
		}
	}
	if p := in.Provenance.Target; p.Reason != "held" || now-int64(1000+100*(p.Seq-1)) <= 300 {
		t.Fatalf("at the end: %+v", p)
	}
}

// The arbiter is deterministic: the same answers give the same Intents
// and counters, bit for bit.
func TestEvidenceDeterminism(t *testing.T) {
	run := func() ([]Intent, ArbiterStats) {
		rng := rand.New(rand.NewPCG(7, 11))
		b := testBelief()
		a := newTestArbiter(ArbiterConfig{})
		var out []Intent
		for i := 0; i < 200; i++ {
			l := LaneFast
			if i%5 == 4 {
				l = LaneSlow
			}
			now := int64(1000 + 100*i)
			req := laneReq(t, uint64(i+1), l, now, b)
			ans := map[string]Answer{}
			for qi := range req.Questions {
				q := &req.Questions[qi]
				switch q.Type {
				case Choice:
					pick := q.Options[rng.IntN(len(q.Options))].Key
					ans[q.ID] = noisyChoice(q, pick, 0.5+0.5*rng.Float64(), 0.1+0.9*rng.Float64())
				case Score:
					ans[q.ID] = score(4*rng.Float64(), rng.Float64())
				}
			}
			a.Observe(req)
			if rng.IntN(10) > 0 { // some requests never come back
				a.Apply(Result{Req: req, Resp: &Response{Seq: req.Seq, Answers: ans}, Arrived: now + 212})
			}
			out = append(out, a.Intent(now+212, b))
		}
		return out, a.Stats()
	}
	i1, s1 := run()
	i2, s2 := run()
	if !reflect.DeepEqual(i1, i2) || !reflect.DeepEqual(s1, s2) {
		t.Fatal("two identical runs differ")
	}
	model := 0
	for _, in := range i1 {
		if in.Provenance.Target.Source == SourceModel {
			model++
		}
	}
	if model == 0 {
		t.Fatal("random answers never decided the target")
	}
}

// A value kept only by its dwell time, while fresh answers clearly prefer
// another, is stale once the answer that set it expired (not the model's).
func TestEvidenceDwellHeldIsStale(t *testing.T) {
	fd := &feeder{t: t, a: newTestArbiter(ArbiterConfig{}), b: testBelief()}
	fd.answer(LaneFast, 0, map[string]string{QFirePolicy: "hold"}, 1, 0.9)
	for now := int64(100); now <= 300; now += 100 {
		in := fd.answer(LaneFast, now, map[string]string{QFirePolicy: "suppress"}, 1, 0.9)
		if p := in.Provenance.FirePolicy; in.FirePolicy != FireHold || p.Reason != "held" || p.Source != SourceModel {
			t.Fatalf("at %d (within the TTL of the answer that set it): %s %+v", now, in.FirePolicy, p)
		}
	}
	if in := fd.a.Intent(350, fd.b); in.FirePolicy != FireHold || in.Provenance.FirePolicy.Source != SourceStale {
		t.Fatalf("held by its dwell past the TTL: %s %+v", in.FirePolicy, in.Provenance.FirePolicy)
	}
	if in := fd.a.Intent(400, fd.b); in.FirePolicy != FireSuppress || in.Provenance.FirePolicy.Source != SourceModel {
		t.Fatalf("after the dwell: %s %+v", in.FirePolicy, in.Provenance.FirePolicy)
	}
}
