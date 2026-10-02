package routeexec

import (
	"testing"

	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
)

// TestTargetAndExit: Target is where the step's objective is (a kill's
// monster where the kill order places it: its spawn, then its track), and
// the objective view's Exit says whether the steps left lead straight out
// of the level (no kill, pickup or confirmation before the exit).
func TestTargetAndExit(t *testing.T) {
	lv := demo(t, "demo3")
	exit := route.Step{Op: route.OpTouch, Target: modelRef("*34"), Yaw: yaw(0),
		Effects: []route.Effect{{Kind: route.EffExit, Target: *ref(591)}}}
	h := newHarness(t, lv, []route.Step{{Op: route.OpKill, Class: "monster_gunner", Pos: vec(-536, -472, -272), Target: ref(418)},
		{Op: route.OpGoto, Pos: vec(-416, -312, -320)}, exit})
	h.b.Self.Origin, h.b.Self.Eye = Vec3{-536, -100, -280}, Vec3{-536, -100, -258}
	h.tick(1)
	spawn := lv.md.Entity(418).Origin
	p, ok := h.x.Target()
	if !ok || dist3(p, spawn) > 40 {
		t.Fatalf("kill target %v (%v) before the monster is seen, want near its spawn %v", p, ok, spawn)
	}
	if ov := h.x.Objective(); ov == nil || ov.Exit {
		t.Fatalf("objective %+v: a kill is left before the exit", ov)
	}
	moved := Vec3{spawn[0] + 100, spawn[1], spawn[2]}
	h.b.Tracks = []worldmodel.Track{{ID: "e7", Class: "gunner", Lump: 418, Pos: moved, PosKnown: true, Mins: Vec3{-16, -16, -24},
		Maxs: Vec3{16, 16, 32}, Visible: true, Shootable: true, Life: worldmodel.LifeAlive}}
	h.tick(1)
	if p, ok := h.x.Target(); !ok || dist3(p, Vec3{moved[0], moved[1], moved[2] + 4}) > 1 {
		t.Fatalf("kill target %v (%v) once seen, want the track's center", p, ok)
	}
	// out of view and heard since from elsewhere: the kill follows
	// hearing's stand-in (the track's Pos stays where it was seen)
	standIn := Vec3{moved[0], moved[1] + 300, moved[2]}
	tr := &h.b.Tracks[0]
	tr.Visible, tr.Loc, tr.LocKnown = false, standIn, true
	h.tick(1)
	if p, ok := h.x.Target(); !ok || dist3(p, Vec3{standIn[0], standIn[1], standIn[2] + 4}) > 1 {
		t.Fatalf("kill target %v (%v) once heard elsewhere, want hearing's stand-in", p, ok)
	}
	tr.Visible, tr.Loc, tr.LocSeen = true, moved, true
	// the kill done: a goto and the exit are left
	h.b.Tracks[0].Life, h.b.Tracks[0].LifeAt = worldmodel.LifeDead, h.now
	h.tick(dyingConfirm/100 + 2)
	if h.x.Index() != 1 {
		t.Fatalf("at step %d, want 1", h.x.Index())
	}
	if p, ok := h.x.Target(); !ok || p != (Vec3{-416, -312, -320}) {
		t.Errorf("goto target %v (%v)", p, ok)
	}
	if ov := h.x.Objective(); ov == nil || !ov.Exit {
		t.Errorf("objective %+v: only a goto before the exit", ov)
	}
}
