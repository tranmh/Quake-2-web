package decide

import (
	"errors"
	"math"
	"testing"
	"time"

	"quake2web/server/internal/agent/worldmodel"
)

// fallbackKeys are the stand-in scripted policy's answers.
var fallbackKeys = map[string]string{QTarget: "e2", QFirePolicy: "hold", QMovement: "hold", QMode: "objective",
	QWeapon: "keep", QPickup: "none", QDanger: "1"}

func newTestArbiter(cfg ArbiterConfig) *Arbiter {
	if cfg.Fallback == nil {
		cfg.Fallback = constBackend(fallbackKeys)
	}
	return NewArbiter(cfg)
}

// laneReq builds a request of the fixture state at snap.
func laneReq(t *testing.T, seq uint64, l Lane, snap int64, b *worldmodel.Belief) *Request {
	t.Helper()
	st := testProjector().Project(l, b, Context{Objective: testObjective()})
	req, err := NewRequest(seq, l, snap, &st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func choice(key string, conf float64, probs map[string]float64) Answer {
	if probs == nil {
		probs = map[string]float64{key: 1}
	}
	return Answer{Type: Choice, Choice: key, Confidence: conf, HasConfidence: true, Probabilities: probs}
}

func score(s, conf float64) Answer {
	return Answer{Type: Score, Score: s, Confidence: conf, HasConfidence: true,
		Probabilities: map[string]float64{levelKey(int(math.Round(s))): 1}}
}

func result(req *Request, ans map[string]Answer) Result {
	return Result{Req: req, Resp: &Response{Seq: req.Seq, Answers: ans}, Arrived: req.SnapTime}
}

// step observes and applies one answered request.
func step(t *testing.T, a *Arbiter, req *Request, ans map[string]Answer) Applied {
	t.Helper()
	a.Observe(req)
	return a.Apply(result(req, ans))
}

func TestArbiterAcceptsAndCompares(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	req := laneReq(t, 1, LaneFast, 1000, b)
	ap := step(t, a, req, map[string]Answer{
		QTarget:     choice("e1", 0.9, map[string]float64{"e1": 0.9, "e2": 0.1}),
		QFirePolicy: choice("fire_when_aligned", 0.8, nil),
		QMovement:   choice("hold", 0.8, nil),
	})
	if ap.Dropped != "" || len(ap.Fields) != 3 {
		t.Fatalf("applied %+v", ap)
	}
	if f := ap.Fields[0]; f.Field != FieldTarget || f.Value != "e1" || f.Source != SourceModel || f.Scripted != "e2" || f.Reason != "" {
		t.Errorf("target outcome %+v", f)
	}
	if tf := ap.Fields[0].Trace(); tf.Name != "target" || tf.Source != "model" || tf.Scripted != "e2" {
		t.Errorf("trace field %+v", tf)
	}
	in := a.Intent(1100, b)
	if in.Target != "e1" || in.FirePolicy != FireWhenAligned || in.Movement != MoveHold {
		t.Fatalf("intent %+v", in)
	}
	if p := in.Provenance.Target; p.Source != SourceModel || p.Seq != 1 || p.Confidence != 0.9 {
		t.Errorf("target provenance %+v", p)
	}
	st := a.Stats()
	// target and fire_policy disagree with the fallback, movement agrees
	if st.Fields[FieldTarget].Comparisons != 1 || st.Fields[FieldTarget].Disagreements != 1 ||
		st.Fields[FieldMovement].Disagreements != 0 || st.Fields[FieldFirePolicy].Disagreements != 1 {
		t.Errorf("stats %+v", st.Fields)
	}
}

func TestArbiterValidation(t *testing.T) {
	b := testBelief()
	for _, tc := range []struct {
		name   string
		ans    map[string]Answer
		reason string
	}{
		{"unknown option", map[string]Answer{QTarget: choice("e9", 0.9, nil)}, "unknown_option"},
		{"low confidence", map[string]Answer{QTarget: choice("e1", 0.34, nil)}, "low_confidence"},
		{"missing", map[string]Answer{}, "missing"},
		{"type mismatch", map[string]Answer{QTarget: {Type: Score, Score: 1}}, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestArbiter(ArbiterConfig{})
			ap := step(t, a, laneReq(t, 1, LaneFast, 1000, b), tc.ans)
			o := ap.Fields[0]
			if o.Reason != tc.reason || o.Value != "e2" || o.Source != SourceScripted {
				t.Fatalf("outcome %+v", o)
			}
			in := a.Intent(1000, b)
			if in.Target != "e2" || in.Provenance.Target.Source != SourceScripted || in.Provenance.Target.Reason != tc.reason {
				t.Fatalf("intent target %q %+v", in.Target, in.Provenance.Target)
			}
		})
	}

	a := newTestArbiter(ArbiterConfig{})
	// exactly the threshold passes
	step(t, a, laneReq(t, 1, LaneFast, 1000, b), map[string]Answer{QTarget: choice("e1", 0.35, nil)})
	if in := a.Intent(1000, b); in.Target != "e1" || in.Provenance.Target.Source != SourceModel {
		t.Errorf("confidence 0.35: %+v", in.Provenance.Target)
	}

	q := &Question{ID: "x", Type: Choice, Options: []Option{{"a", ""}, {"b", ""}}}
	// renormalized over the options, unknown keys and bad values dropped
	c := a.validate(q, Answer{Type: Choice, Choice: "a", Probabilities: map[string]float64{"a": 2, "b": 2, "zz": 5}, Confidence: 1, HasConfidence: true}, true)
	if c.reason != "" || c.probs["a"] != 0.5 || c.probs["b"] != 0.5 || len(c.probs) != 2 {
		t.Errorf("renormalized %+v", c)
	}
	c = a.validate(q, Answer{Type: Choice, Choice: "b", Probabilities: map[string]float64{"a": math.NaN(), "b": -1}}, true)
	if c.probs["b"] != 1 || c.conf != 1 || c.reason != "" {
		t.Errorf("one-hot repair and confidence substitute %+v", c)
	}
	// noul: |2p-1| < 0.2 is gated
	nq := &Question{ID: "n", Type: Noul}
	for p, want := range map[float64]string{0.55: "low_confidence", 0.41: "low_confidence", 0.65: "", 0.05: "", 1.7: ""} {
		if c := a.validate(nq, Answer{Type: Noul, Noul: p}, true); c.reason != want {
			t.Errorf("noul %v: reason %q, want %q", p, c.reason, want)
		}
	}
	// score: clamped, NaN invalid, gated by confidence
	sq := dangerQuestion()
	if c := a.validate(&sq, score(5.2, 0.9), true); c.score != 4 || c.value != "4" || c.reason != "" {
		t.Errorf("score clamp %+v", c)
	}
	if c := a.validate(&sq, Answer{Type: Score, Score: math.NaN(), Confidence: 1, HasConfidence: true}, true); c.reason != "invalid" {
		t.Errorf("NaN score %+v", c)
	}
	if c := a.validate(&sq, score(2, 0.2), true); c.reason != "low_confidence" {
		t.Errorf("unconfident score %+v", c)
	}
}

func TestArbiterTTL(t *testing.T) {
	b := testBelief()
	ans := map[string]Answer{QTarget: choice("e1", 0.9, nil), QFirePolicy: choice("fire_when_aligned", 0.9, nil), QMovement: choice("hold", 0.9, nil)}
	a := newTestArbiter(ArbiterConfig{})
	step(t, a, laneReq(t, 1, LaneFast, 1000, b), ans)
	if a.TTL(LaneFast) != 300*time.Millisecond || a.TTL(LaneSlow) != 1500*time.Millisecond {
		t.Fatalf("TTL %v %v", a.TTL(LaneFast), a.TTL(LaneSlow))
	}
	if p := a.Intent(1300, b).Provenance.Target; p.Source != SourceModel {
		t.Errorf("at TTL: %+v", p)
	}
	// past the TTL (counted from SnapTime): the fallback of the same request
	if in := a.Intent(1301, b); in.Target != "e2" || in.Provenance.Target.Source != SourceScripted || in.Provenance.Target.Reason != "ttl" {
		t.Errorf("past TTL: %q %+v", in.Target, in.Provenance.Target)
	}
	// the lane stopped asking: the fallback expires too (500 ms); the
	// target has no hold time, fire_policy is held 0.4 s from 1.3 s
	in := a.Intent(1501, b)
	if in.Target != "" || in.Provenance.Target.Source != SourceDefault || in.Provenance.Target.Reason != "ttl" {
		t.Errorf("after the fallback TTL: %+v", in)
	}
	if in.FirePolicy != FireWhenAligned || in.Provenance.FirePolicy.Source != SourceStale || in.Provenance.FirePolicy.Reason != "held" {
		t.Errorf("fire_policy within its hold: %+v", in.Provenance.FirePolicy)
	}
	if in := a.Intent(1700, b); in.FirePolicy != FireHold || in.Provenance.FirePolicy.Source != SourceDefault {
		t.Errorf("fire_policy after its hold: %s %+v", in.FirePolicy, in.Provenance.FirePolicy)
	}

	// TTL >= p95 + 100 ms
	p95 := 450 * time.Millisecond
	a = newTestArbiter(ArbiterConfig{P95: func() time.Duration { return p95 }})
	if a.TTL(LaneFast) != 550*time.Millisecond {
		t.Fatalf("TTL with p95 450ms: %v", a.TTL(LaneFast))
	}
	step(t, a, laneReq(t, 1, LaneFast, 1000, b), ans)
	if p := a.Intent(1549, b).Provenance.Target; p.Source != SourceModel {
		t.Errorf("within p95+100: %+v", p)
	}
}

func TestArbiterModeHysteresis(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	seq := uint64(0)
	slow := func(now int64, mode map[string]float64, danger float64) Intent {
		t.Helper()
		seq++
		best, bp := "", -1.0
		for k, p := range mode {
			if p > bp || p == bp && k < best {
				best, bp = k, p
			}
		}
		step(t, a, laneReq(t, seq, LaneSlow, now, b), map[string]Answer{QMode: choice(best, 0.9, mode), QDanger: score(danger, 0.9)})
		return a.Intent(now, b)
	}
	if in := slow(0, map[string]float64{"fight": 0.6, "objective": 0.4}, 1); in.Mode != ModeFight {
		t.Fatalf("first answer: %s", in.Mode)
	}
	// a small margin never switches
	if in := slow(500, map[string]float64{"fight": 0.45, "objective": 0.55}, 1); in.Mode != ModeFight || in.Provenance.Mode.Reason != "held" {
		t.Fatalf("dp 0.1: %s %+v", in.Mode, in.Provenance.Mode)
	}
	// a clear margin waits until the mode was held 1.5 s
	if in := slow(1000, map[string]float64{"fight": 0.3, "objective": 0.7}, 1); in.Mode != ModeFight {
		t.Fatalf("held 1.0 s: %s", in.Mode)
	}
	if in := slow(1500, map[string]float64{"fight": 0.3, "objective": 0.7}, 1); in.Mode != ModeObjective || in.Provenance.Mode.Source != SourceModel {
		t.Fatalf("held 1.5 s: %s %+v", in.Mode, in.Provenance.Mode)
	}
	// retreat at danger >= 3.5 skips the hysteresis
	if in := slow(1600, map[string]float64{"objective": 0.45, "retreat": 0.5}, 3.6); in.Mode != ModeRetreat || in.Danger != 3.6 {
		t.Fatalf("retreat at danger 3.6: %s %v", in.Mode, in.Danger)
	}
	// but not below
	a = newTestArbiter(ArbiterConfig{})
	seq = 0
	slow(0, map[string]float64{"fight": 0.6, "objective": 0.4}, 1)
	if in := slow(100, map[string]float64{"fight": 0.45, "retreat": 0.55}, 3.4); in.Mode != ModeFight {
		t.Fatalf("retreat at danger 3.4: %s", in.Mode)
	}
}

func TestArbiterTargetHysteresis(t *testing.T) {
	fast := func(a *Arbiter, b *worldmodel.Belief, seq uint64, now int64, probs map[string]float64, pick string) Intent {
		t.Helper()
		step(t, a, laneReq(t, seq, LaneFast, now, b), map[string]Answer{QTarget: choice(pick, 0.9, probs)})
		return a.Intent(now, b)
	}
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	fast(a, b, 1, 1000, map[string]float64{"e1": 0.6, "e2": 0.4}, "e1")
	if in := fast(a, b, 2, 1100, map[string]float64{"e1": 0.45, "e2": 0.55}, "e2"); in.Target != "e1" {
		t.Fatalf("dp 0.1 switched to %s", in.Target)
	}
	if in := fast(a, b, 3, 1200, map[string]float64{"e1": 0.4, "e2": 0.6}, "e2"); in.Target != "e2" {
		t.Fatalf("dp 0.2 kept %s", in.Target)
	}

	// the current target died: any other answer switches at once
	b = testBelief()
	a = newTestArbiter(ArbiterConfig{})
	fast(a, b, 1, 1000, map[string]float64{"e1": 0.6, "e2": 0.4}, "e1")
	b.Track("e1").Life = worldmodel.LifeDead
	if in := fast(a, b, 2, 1100, map[string]float64{"e1": 0.48, "e2": 0.52}, "e2"); in.Target != "e2" {
		t.Fatalf("dead target kept: %s", in.Target)
	}

	// unseen for more than a second
	b = testBelief()
	a = newTestArbiter(ArbiterConfig{})
	fast(a, b, 1, 1000, map[string]float64{"e1": 0.6, "e2": 0.4}, "e1")
	e1 := b.Track("e1")
	e1.Visible, e1.LastSeen = false, 1000
	if in := fast(a, b, 2, 1900, map[string]float64{"e1": 0.48, "e2": 0.52}, "e2"); in.Target != "e1" {
		t.Fatalf("unseen 0.9 s switched: %s", in.Target)
	}
	if in := fast(a, b, 3, 2100, map[string]float64{"e1": 0.48, "e2": 0.52}, "e2"); in.Target != "e2" {
		t.Fatalf("unseen 1.1 s kept: %s", in.Target)
	}
	// a remembered (unseen) enemy is still a valid choice
	if in := fast(a, b, 4, 2150, map[string]float64{"e1": 0.9, "e2": 0.1}, "e1"); in.Target != "e1" || in.Provenance.Target.Source != SourceModel {
		t.Fatalf("remembered target refused: %q %+v", in.Target, in.Provenance.Target)
	}
	// a model answer naming a track that died since the request is not used
	step(t, a, laneReq(t, 5, LaneFast, 2200, b), map[string]Answer{QTarget: choice("e2", 0.9, map[string]float64{"e2": 0.9})})
	b.Track("e2").Life = worldmodel.LifeGibbed
	if in := a.Intent(2200, b); in.Target == "e2" || in.Provenance.Target.Reason != "gone" {
		t.Fatalf("dead model target: %q %+v", in.Target, in.Provenance.Target)
	}
}

func TestArbiterHoldsFastFields(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	ans := func(fire, move string) map[string]Answer {
		return map[string]Answer{QFirePolicy: choice(fire, 0.9, nil), QMovement: choice(move, 0.9, nil)}
	}
	step(t, a, laneReq(t, 1, LaneFast, 1000, b), ans("hold", "advance"))
	a.Intent(1000, b)
	step(t, a, laneReq(t, 2, LaneFast, 1100, b), ans("suppress", "retreat"))
	if in := a.Intent(1100, b); in.FirePolicy != FireHold || in.Movement != MoveAdvance || in.Provenance.Movement.Reason != "held" {
		t.Fatalf("held 0.1 s: %+v", in)
	}
	step(t, a, laneReq(t, 3, LaneFast, 1400, b), ans("suppress", "retreat"))
	if in := a.Intent(1400, b); in.FirePolicy != FireSuppress || in.Movement != MoveRetreat {
		t.Fatalf("held 0.4 s: %+v", in)
	}
}

func TestArbiterDropsResults(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	step(t, a, laneReq(t, 5, LaneFast, 1000, b), map[string]Answer{QTarget: choice("e1", 0.9, nil)})
	if ap := step(t, a, laneReq(t, 4, LaneFast, 900, b), map[string]Answer{QTarget: choice("e3", 0.9, nil)}); ap.Dropped != "out_of_order" {
		t.Fatalf("seq 4 after 5: %+v", ap)
	}
	if in := a.Intent(1000, b); in.Target != "e1" {
		t.Fatalf("out-of-order answer applied: %s", in.Target)
	}
	req := laneReq(t, 6, LaneFast, 1000, b)
	r := result(req, map[string]Answer{QTarget: choice("e3", 0.9, nil)})
	r.Stale = true
	if ap := a.Apply(r); ap.Dropped != "stale" {
		t.Fatalf("stale: %+v", ap)
	}
	if ap := a.Apply(Result{Req: laneReq(t, 7, LaneFast, 1050, b), Err: errors.New("boom")}); ap.Dropped != "error" {
		t.Fatalf("error: %+v", ap)
	}
	if ap := a.Apply(Result{Req: laneReq(t, 8, LaneFast, 1060, b), Err: errors.New("deadline"), Timeout: true}); ap.Dropped != "timeout" {
		t.Fatalf("timeout: %+v", ap)
	}
	// past the TTL the reason names the lane's last failure
	if p := a.Intent(1400, b).Provenance.Target; p.Reason != "timeout" {
		t.Fatalf("reason after a timeout: %+v", p)
	}
	st := a.Stats()
	if st.Applied != 1 || st.OutOfOrder != 1 || st.Stale != 1 || st.Errors != 2 {
		t.Fatalf("stats %+v", st)
	}
}

func TestArbiterStaleAndDefault(t *testing.T) {
	b := testBelief()
	// without a fallback an expired answer is kept as stale, then dropped
	a := NewArbiter(ArbiterConfig{})
	a.Apply(result(laneReq(t, 1, LaneFast, 0, b), map[string]Answer{QMovement: choice("advance", 0.9, nil)}))
	if in := a.Intent(100, b); in.Provenance.Movement.Source != SourceModel {
		t.Fatalf("fresh: %+v", in.Provenance.Movement)
	}
	if in := a.Intent(400, b); in.Movement != MoveAdvance || in.Provenance.Movement.Source != SourceStale || in.Provenance.Movement.Reason != "ttl" {
		t.Fatalf("expired: %s %+v", in.Movement, in.Provenance.Movement)
	}
	if in := a.Intent(2400, b); in.Movement != MoveHold || in.Provenance.Movement.Source != SourceDefault {
		t.Fatalf("past MaxStale: %s %+v", in.Movement, in.Provenance.Movement)
	}

	// with a fallback: a model value held by hysteresis past its TTL is stale
	a = newTestArbiter(ArbiterConfig{})
	step(t, a, laneReq(t, 1, LaneFast, 0, b), map[string]Answer{QMovement: choice("advance", 0.9, nil)})
	a.Intent(0, b)
	in := a.Intent(350, b)
	if in.Movement != MoveAdvance || in.Provenance.Movement.Source != SourceStale || in.Provenance.Movement.Reason != "held" {
		t.Fatalf("held past TTL: %s %+v", in.Movement, in.Provenance.Movement)
	}
	if in := a.Intent(400, b); in.Movement != MoveHold || in.Provenance.Movement.Source != SourceScripted {
		t.Fatalf("after the hold: %s %+v", in.Movement, in.Provenance.Movement)
	}

	// nothing yet: defaults
	a = newTestArbiter(ArbiterConfig{})
	in = a.Intent(0, b)
	if in.Mode != ModeExplore || in.Target != "" || in.Weapon != WeaponKeep || in.Provenance.Mode.Source != SourceDefault || in.Provenance.Mode.Reason != "no_answer" {
		t.Fatalf("no answers: %+v", in)
	}
	// the scripted backend's answers are labelled scripted
	a = NewArbiter(ArbiterConfig{AnswerSource: SourceScripted})
	a.Apply(result(laneReq(t, 1, LaneFast, 0, b), map[string]Answer{QMovement: choice("advance", 1, nil)}))
	if p := a.Intent(0, b).Provenance.Movement; p.Source != SourceScripted || p.Reason != "" {
		t.Fatalf("scripted backend: %+v", p)
	}

	// ticks are counted per field and source
	st := a.Stats()
	for f := Field(0); f < NumFields; f++ {
		n := 0
		for _, c := range st.Fields[f].Ticks {
			n += c
		}
		if n != 1 {
			t.Errorf("%s: %d ticks, want 1", f.ID(), n)
		}
	}
	if s := st.Fields[FieldMovement].Share(SourceScripted); s != 1 {
		t.Errorf("share %v", s)
	}
}

// TestArbiterNotAsked: a fast request without enemies has no target
// question; the target falls to its default at once.
func TestArbiterNotAsked(t *testing.T) {
	b := testBelief()
	a := newTestArbiter(ArbiterConfig{})
	step(t, a, laneReq(t, 1, LaneFast, 1000, b), map[string]Answer{QTarget: choice("e1", 0.9, nil)})
	if in := a.Intent(1000, b); in.Target != "e1" {
		t.Fatal(in.Target)
	}
	quiet := testBelief()
	quiet.Tracks = nil
	req := laneReq(t, 2, LaneFast, 1100, quiet)
	if req.Question(QTarget) != nil {
		t.Fatal("target asked without enemies")
	}
	step(t, a, req, map[string]Answer{QMovement: choice("strafe_left", 0.9, nil)})
	in := a.Intent(1100, quiet)
	if in.Target != "" || in.Provenance.Target.Source != SourceDefault || in.Provenance.Target.Reason != "not_asked" {
		t.Fatalf("not asked: %q %+v", in.Target, in.Provenance.Target)
	}
	a.Reset()
	if in := a.Intent(1200, b); in.Movement != MoveHold || in.Provenance.Movement.Reason != "no_answer" {
		t.Fatalf("after Reset: %+v", in)
	}
}
