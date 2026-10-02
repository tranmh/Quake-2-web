package decide

import (
	"strings"
	"testing"
	"time"
)

// TestContinuityHints: the fast state tells the backend what the bot is
// doing (me.moving: the current movement) and how long its current target
// has been its target (me.target_since_s, only with a current target in
// the list).
func TestContinuityHints(t *testing.T) {
	p := testProjector()
	b := testBelief()
	st := p.Fast(b, Context{Target: "e1", TargetSince: fixtureNow - 2340, Moving: MoveStrafe})
	if st.Me.Moving != "strafe" || st.Me.TargetSinceS == nil || *st.Me.TargetSinceS != 2.3 {
		t.Fatalf("me %+v", st.Me)
	}
	if st := p.Fast(b, Context{TargetSince: fixtureNow - 1000}); st.Me.TargetSinceS != nil || st.Me.Moving != "" {
		t.Fatalf("no target: %+v", st.Me)
	}
	if st := p.Fast(b, Context{Target: "e9", TargetSince: fixtureNow - 1000}); st.Me.TargetSinceS != nil {
		t.Fatalf("target not in the list: %+v", st.Me)
	}
}

// TestPipelineTargetSince: the pipeline passes its last movement and the
// time its target last changed into the next fast state.
func TestPipelineTargetSince(t *testing.T) {
	model := constBackend(map[string]string{QTarget: "e1", QFirePolicy: "fire_when_aligned", QMovement: "strafe", QMode: "fight"})
	p, err := NewPipeline(PipelineConfig{Backend: model, Fallback: constBackend(fallbackKeys),
		Scheduler: SchedulerConfig{Mode: Lockstep, SimLatency: FixedLatency(212 * time.Millisecond)}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b := testBelief()
	var changed int64
	for i := 0; i < 20; i++ {
		now := int64(10000 + 100*i)
		b.Time, b.Frames = now, 100+i
		prev := p.Intent().Target
		in := p.Tick(now, b, testObjective())
		if in.Target != prev {
			changed = now
		}
		if i < 12 {
			continue
		}
		st := p.LastTick().Fast
		if st.Me.Moving != string(p.Intent().Movement) && st.Me.Moving != "strafe" {
			t.Fatalf("tick %d: moving %q", i, st.Me.Moving)
		}
		if want := float64(now-100-changed) / 1000; st.Me.TargetSinceS == nil || *st.Me.TargetSinceS < want-0.15 || *st.Me.TargetSinceS > want+0.15 {
			t.Fatalf("tick %d: target_since_s %v, want about %.1f (changed at %d)", i, st.Me.TargetSinceS, want, changed)
		}
	}
}

// TestObjectiveEnemy: the monster the route's kill step needs dead is
// listed (and marked) however long ago it was seen, kept when the list is
// full, and named in the target question.
func TestObjectiveEnemy(t *testing.T) {
	p := testProjector()
	b := testBelief()
	// e7, the tank last seen 9 s ago, is forgotten unless it is the
	// objective's
	if st := p.Fast(b, Context{}); hasEnemy(st, "e7") {
		t.Fatal("a long unseen monster listed")
	}
	st := p.Fast(b, Context{Objective: &ObjectiveView{Kind: "kill", Target: "e7"}})
	if !hasEnemy(st, "e7") || len(st.Enemies) != DefaultMaxEnemies {
		t.Fatalf("objective not listed: %+v", st.Enemies)
	}
	for _, e := range st.Enemies {
		if e.Objective != (e.ID == "e7") {
			t.Fatalf("objective mark on %+v", e)
		}
	}
	// with a current target out of the top too, both stay
	st = p.Fast(b, Context{Target: "e5", Objective: &ObjectiveView{Kind: "kill", Target: "e7"}})
	if !hasEnemy(st, "e5") || !hasEnemy(st, "e7") || len(st.Enemies) != DefaultMaxEnemies {
		t.Fatalf("current and objective: %+v", st.Enemies)
	}
	q, _ := targetQuestion(&st)
	if i := q.Index("e7"); i < 0 || !strings.Contains(q.Options[i].Desc, "objective") {
		t.Fatalf("target question: %+v", q.Options)
	}
	// trimming keeps it (and the current target) to the last
	fit, _, err := FitState(st, 700)
	if err != nil {
		t.Fatal(err)
	}
	if !hasEnemy(fit, "e7") {
		t.Fatalf("trimmed away: %+v", fit.Enemies)
	}
}

func hasEnemy(st State, id string) bool {
	for _, e := range st.Enemies {
		if e.ID == id {
			return true
		}
	}
	return false
}
