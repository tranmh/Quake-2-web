package navrt

import (
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/worldmodel"
)

func TestMapStateAssume(t *testing.T) {
	_, ms := blockerGraph(t)
	// the laser (#13, blocker 3) seen on, then assumed off: older
	// observations no longer count, a newer one does
	b := &worldmodel.Belief{Lasers: []worldmodel.Laser{{Lump: 13, State: worldmodel.LaserOn, LastUpdate: 1000}}}
	ms.Update(b, 1000)
	sat(t, ms, 3, nav.StateGone, 1000, false, 0)
	rev := ms.Revision(3)
	if !ms.Assume(3, -1, 2000) {
		t.Fatal("assuming the laser off changed nothing")
	}
	if bb := ms.Belief(3); bb.Status != BlockerGone || !bb.Assumed || ms.Revision(3) == rev {
		t.Fatalf("assumed off: %+v (revision %d)", bb, ms.Revision(3))
	}
	ms.Update(b, 2100)
	sat(t, ms, 3, nav.StateGone, 2100, true, 0)
	if ms.Assume(3, -1, 2200) {
		t.Error("assuming the same state again reported a change")
	}
	b.Lasers[0].LastUpdate = 3000
	ms.Update(b, 3000)
	if bb := ms.Belief(3); bb.Status != BlockerAt || bb.Assumed {
		t.Errorf("a newer observation must win: %+v", bb)
	}

	// the use door *2 (blocker 1) assumed open: passable, and back at its
	// spawn pose after its wait (3 s) and travel (1 s) like one seen open
	if !ms.Assume(1, 1, 5000) {
		t.Fatal("assuming the door open changed nothing")
	}
	sat(t, ms, 1, nav.Pose(1), 5000, true, 0)
	ms.Update(b, 5000+4000+returnSlack+1)
	if bb := ms.Belief(1); bb.Status != BlockerUnknown || bb.Pose != 0 {
		t.Errorf("the assumed open door did not go back: %+v", bb)
	}
	// the prediction world puts an assumed pose's solid where it is
	ms.Assume(1, 1, 20000)
	for _, s := range ms.Solids(nil) {
		if s.ID == 11 && s.Origin != (Vec3{100, 0, 88}) {
			t.Errorf("assumed open door solid at %v", s.Origin)
		}
	}

	// refused: gone for a blocker that cannot go, a pose it does not have
	if ms.Assume(0, -1, 0) || ms.Assume(0, 7, 0) || ms.Assume(99, 0, 0) {
		t.Error("an impossible assumption was taken")
	}
}

func TestMapStateHoldOpen(t *testing.T) {
	_, ms := blockerGraph(t)
	// the auto door *1 (blocker 0, travel 1.4 s, wait 3 s) with its
	// trigger in front of it
	ms.info[0].trigger = &mapdata.Box{Min: Vec3{40, -64, 0}, Max: Vec3{160, 64, 96}}
	inside := func(at int64, in bool) *worldmodel.Belief {
		b := &worldmodel.Belief{Time: at}
		b.Self.Origin = Vec3{0, 0, 24}
		if in {
			b.Self.Origin = Vec3{60, 0, 24}
		}
		return b
	}
	// unseen: open once the bot stood in the trigger for the travel time
	ms.Update(inside(1000, true), 1000)
	if bb := ms.Belief(0); bb.Status != BlockerUnknown {
		t.Fatalf("held for no time: %+v", bb)
	}
	ms.Update(inside(2400, true), 2400)
	if bb := ms.Belief(0); bb.Status != BlockerAt || bb.Pose != 1 || !bb.Assumed {
		t.Fatalf("held for its travel: %+v", bb)
	}
	sat(t, ms, 0, nav.Pose(1), 2400, true, 0)
	// it stays open while the bot stays (well past wait + travel)
	ms.Update(inside(20000, true), 20000)
	if bb := ms.Belief(0); bb.Status != BlockerAt || bb.Pose != 1 {
		t.Fatalf("held 20 s: %+v", bb)
	}
	// out of the trigger it closes after its wait and travel
	ms.Update(inside(21000, false), 21000)
	if bb := ms.Belief(0); bb.Status != BlockerAt || bb.Pose != 1 {
		t.Fatalf("just left: %+v", bb)
	}
	ms.Update(inside(20000+4400+returnSlack+1, false), 20000+4400+returnSlack+1)
	if bb := ms.Belief(0); bb.Status != BlockerUnknown || bb.Pose != 0 {
		t.Fatalf("left long ago: %+v", bb)
	}
	// what the bot sees right now wins: seen closed (a locked door, say)
	b := inside(40000, true)
	b.Movers = []worldmodel.Mover{{Model: "*1", Origin: Vec3{100, 0, 0}, LastUpdate: 40000, Visible: true}}
	ms.Update(b, 40000)
	b.Time = 42000
	b.Movers[0].LastUpdate = 42000
	ms.Update(b, 42000)
	if bb := ms.Belief(0); bb.Status != BlockerAt || bb.Pose != 0 || bb.Assumed {
		t.Errorf("seen closed while held: %+v", bb)
	}
	// a use door is never held open
	ms.info[1].trigger = ms.info[0].trigger
	ms.Update(inside(50000, true), 50000)
	ms.Update(inside(60000, true), 60000)
	if bb := ms.Belief(1); bb.Status != BlockerUnknown {
		t.Errorf("use door held open: %+v", bb)
	}
}
