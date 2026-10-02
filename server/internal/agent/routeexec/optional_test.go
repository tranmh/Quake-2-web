package routeexec

import (
	"strings"
	"testing"

	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
)

// TestOptionalSteps: an optional step gets one attempt; when it fails it
// is skipped together with the optional steps right after it in the same
// detour, and the route goes on (with the next detour, if one follows);
// an optional pickup of a weapon the bot already holds is skipped at
// once, alone (the ammo next to it is still picked up); a required step
// still retries.
func TestOptionalSteps(t *testing.T) {
	lv := demo(t, "demo1")
	shotgun := lv.md.Entity(373).Origin
	shells := lv.md.Entity(371).Origin
	steps := func() []route.Step {
		return []route.Step{
			{Op: route.OpPickup, Class: "weapon_shotgun", Pos: vec(shotgun[0], shotgun[1], shotgun[2]), Optional: true},
			{Op: route.OpPickup, Class: "ammo_shells", Pos: vec(shells[0], shells[1], shells[2]), Optional: true},
			{Op: route.OpGoto, Pos: vec(128, -320, 24), Radius: 40},
		}
	}

	t.Run("FailureSkipsTheDetour", func(t *testing.T) {
		h := newHarness(t, lv, steps())
		h.tick(1)
		h.wantStep(0, StepRunning)
		h.nav.fail(navrt.CauseWorld, "gave up")
		h.tick(1)
		h.wantStep(2, StepRunning)
		st := h.x.Steps()
		if st[0].Status != StepSkipped || !strings.Contains(st[0].Reason, "gave up") || st[1].Status != StepSkipped || st[0].Attempts != 1 {
			t.Fatalf("steps after the failure: %+v", st)
		}
		if g := h.nav.last(); g.Kind != navrt.GoalPoint {
			t.Fatalf("the required step's goal: %s", g)
		}
		// the required step retries as before
		h.nav.fail(navrt.CauseWorld, "gave up")
		h.tick(1)
		if c := h.x.Current(); c.Index != 2 || c.Attempts != 2 || c.Status != StepRunning {
			t.Fatalf("required step after a failure: %+v", c)
		}
	})

	t.Run("SecondOfTheDetourFails", func(t *testing.T) {
		h := newHarness(t, lv, steps())
		h.tick(1)
		h.nav.arrive()
		h.tick(1)
		h.wantStep(1, StepRunning)
		h.tick(1)
		h.nav.fail(navrt.CauseWorld, "gave up")
		h.tick(1)
		h.wantStep(2, StepRunning)
		if st := h.x.Steps(); st[0].Status != StepDone || st[1].Status != StepSkipped {
			t.Fatalf("steps: %+v", st)
		}
	})

	t.Run("OwnedWeaponSkipped", func(t *testing.T) {
		h := newHarness(t, lv, steps())
		h.b.Inventory = worldmodel.Inventory{Known: true, Items: []worldmodel.InvItem{{Name: "Shotgun", Count: 1}}}
		h.tick(1)
		// the detour goes on: the shells are still worth it
		h.wantStep(1, StepRunning)
		if st := h.x.Steps(); st[0].Status != StepSkipped || st[0].Reason != "already owned" {
			t.Fatalf("steps: %+v", st)
		}
	})

	t.Run("NextDetourStays", func(t *testing.T) {
		// two detours next to each other: a timeout in the first leaves
		// the second to run
		for _, fail := range []bool{false, true} {
			s := steps()
			s[0].Detour, s[1].Detour = "shotgun", "shells"
			h := newHarness(t, lv, s)
			h.tick(1)
			if fail {
				h.nav.fail(navrt.CauseWorld, "gave up")
				h.tick(1)
			} else {
				h.nav.fail(navrt.CauseNoPath, "no path")
				h.tick(MoveTimeout/100 + 10)
			}
			h.wantStep(1, StepRunning)
			if st := h.x.Steps(); st[0].Status != StepSkipped || st[1].Attempts != 1 {
				t.Fatalf("fail %v: steps: %+v", fail, st)
			}
			// the second detour is skipped on its own failure
			h.nav.fail(navrt.CauseWorld, "gave up")
			h.tick(1)
			h.wantStep(2, StepRunning)
			if st := h.x.Steps(); st[1].Status != StepSkipped || st[1].Reason == "skipped with step 0" {
				t.Fatalf("fail %v: steps after the second failure: %+v", fail, st)
			}
		}
	})

	t.Run("TimeoutSkips", func(t *testing.T) {
		h := newHarness(t, lv, steps())
		h.tick(1)
		h.nav.fail(navrt.CauseNoPath, "no path")
		h.tick(MoveTimeout/100 + 10)
		h.wantStep(2, StepRunning)
		if st := h.x.Steps(); st[0].Status != StepSkipped || !strings.Contains(st[0].Reason, "timed out") {
			t.Fatalf("steps: %+v", st)
		}
	})
}
