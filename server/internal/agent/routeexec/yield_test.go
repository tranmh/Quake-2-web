package routeexec

import (
	"strings"
	"testing"

	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
)

// yieldFor is the bot busy elsewhere for n frames: it yields every frame
// and does not Update.
func (h *harness) yieldFor(n int) {
	for i := 0; i < n; i++ {
		h.now += 100
		h.x.Yield()
	}
}

// TestYieldPausesAttempt: the time the bot spends on something else
// (Yield) does not count against the step's attempt: a goto survives a
// yield longer than MoveTimeout, its deadline moves on by the time
// yielded, and the stall clock of the objective view stands meanwhile.
// Engage (a fight with the kill step's own monster) keeps the clock
// running.
func TestYieldPausesAttempt(t *testing.T) {
	lv := demo(t, "demo1")
	h := newHarness(t, lv, []route.Step{{Op: route.OpGoto, Pos: vec(128, -320, 24)}})
	h.tick(1)
	h.nav.fail(navrt.CauseNoPath, "no path") // not final: waits for the deadline
	h.tick(10)
	h.yieldFor(MoveTimeout/100 + 100) // 55 s fighting
	h.tick(1)
	if st := h.x.Current(); st.Attempts != 1 || st.Status != StepRunning {
		t.Fatalf("the attempt failed while the bot was busy elsewhere: %+v", st)
	}
	if y := h.x.Yielded(); y < MoveTimeout || y > MoveTimeout+11000 {
		t.Fatalf("yielded %d ms", y)
	}
	if ov := h.x.Objective(); ov == nil || ov.Stalled {
		t.Fatalf("objective %+v: stalled after the yield", ov)
	}
	// the attempt's own time still runs out
	h.tick(MoveTimeout/100 + 10)
	if st := h.x.Current(); st.Attempts != 2 || !strings.Contains(st.Reason, "timed out") || !strings.Contains(st.Reason, "yielded") {
		t.Fatalf("after the attempt's own time: %+v", st)
	}

	// a kill step's fight with its own monster is the step: Engage keeps
	// the clock running (and ends a pause a Yield began)
	h = newHarness(t, lv, []route.Step{{Op: route.OpGoto, Pos: vec(128, -320, 24)}})
	h.tick(1)
	h.nav.fail(navrt.CauseNoPath, "no path")
	h.yieldFor(20) // 2 s on a pickup: paused
	for i := 0; i < MoveTimeout/100+10; i++ {
		h.now += 100
		h.x.Engage(h.now)
	}
	h.tick(1)
	if st := h.x.Current(); st.Attempts != 2 || !strings.Contains(st.Reason, "timed out") {
		t.Fatalf("an engaged fight did not count against the attempt: %+v", st)
	}
	if y := h.x.Yielded(); y != 0 {
		t.Errorf("the new attempt starts with %d ms yielded", y)
	}
}
