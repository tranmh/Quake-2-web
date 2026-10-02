package decide

import (
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
