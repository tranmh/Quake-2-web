package worldmodel

import (
	"math"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// voiceAt is an emitter the bot cannot see (no model) at bearing deg
// (world yaw from the player's origin) and dist, level with the player.
func voiceAt(num int32, deg, dist float32) shared.EntityState {
	s, c := math.Sincos(float64(deg) * math.Pi / 180)
	return shared.EntityState{Number: num, Origin: Vec3{dist * float32(c), dist * float32(s), 24}}
}

func TestArcMaskAndRuns(t *testing.T) {
	m := arcMask(350, 370) // wraps through 0
	if r := m.runs(); len(r) != 1 || absf(angleDiff(r[0].center(), 0)) > earBin || float32(r[0].n)*earBin > 25 {
		t.Fatalf("runs %+v", r)
	}
	m = cueMask(perception.PanCenter, 0) // ahead and behind
	if r := m.runs(); len(r) != 2 || absf(angleDiff(r[0].center(), 0)) > 1 && absf(angleDiff(r[0].center(), 180)) > 1 {
		t.Fatalf("center runs %+v", r)
	}
	if arcMask(0, 360) != ^yawMask(0) || len((^yawMask(0)).runs()) != 1 || yawMask(0).runs() != nil {
		t.Fatal("full and empty masks")
	}
}

// TestEarTellsFrontFromBackByTurning: a source ahead on the left and its
// mirror image behind sound alike, so the belief is the same; a turn
// between two sounds tells them apart, as a player's turn of the head.
func TestEarTellsFrontFromBackByTurning(t *testing.T) {
	front, back := newSim(t), newSim(t)
	front.ents = []shared.EntityState{voiceAt(30, 30, 300)}
	back.ents = []shared.EntityState{voiceAt(30, 150, 300)}
	for _, s := range []*sim{front, back} {
		s.sound(sSight, 30)
		s.step()
	}
	if df, db := front.w.Belief().Digest(), back.w.Belief().Digest(); df != db {
		t.Fatalf("mirror images make different beliefs:\n%+v\n%+v", front.track("e1"), back.track("e1"))
	}
	e := front.track("e1").Ear
	if e.Pan != perception.PanLeft || e.Loud != perception.LoudNear || absf(angleDiff(e.Yaw, 90)) > 1 || e.Spread < 60 {
		t.Fatalf("one sound on the left: %+v", e)
	}
	for _, s := range []*sim{front, back} {
		s.ps.ViewAngles[q2const.YAW] = 90 // turn to the left
		s.sound(sSight, 30)
		s.step()
	}
	for _, tc := range []struct {
		s     *sim
		truth float32
	}{{front, 30}, {back, 150}} {
		tr := tc.s.track("e1")
		if tr.Visible || tr.PosKnown || tr.Ear.Ambiguous || absf(angleDiff(tr.Ear.Yaw, tc.truth)) > tr.Ear.Spread ||
			tr.Ear.Spread > 45 || tr.Loc != tr.Ear.Est {
			t.Errorf("truth %v° after a turn: %+v", tc.truth, tr)
		}
	}
	if front.w.Belief().Digest() == back.w.Belief().Digest() {
		t.Fatal("the turn told nothing apart")
	}
}

// TestEarKeepsTheLastPositionSeen: a sound that agrees with where the
// monster was last seen keeps that position as its location; one from
// elsewhere moves the location to hearing's stand-in, never Pos or Vel.
func TestEarKeepsTheLastPositionSeen(t *testing.T) {
	s := newSim(t)
	seen := Vec3{300, 0, 24}
	s.ents = []shared.EntityState{soldierAt(20, seen)}
	s.step()
	s.ents = []shared.EntityState{{Number: 20, Origin: seen}} // out of view now (no model)
	s.sound(sSight, 20)
	s.step()
	tr := s.track("e1")
	if !tr.Ear.Agrees || !tr.LocSeen || tr.Loc != seen || tr.Pos != seen {
		t.Fatalf("a sound from where it was seen: %+v", tr)
	}
	vel := tr.Vel
	s.ents = []shared.EntityState{{Number: 20, Origin: Vec3{0, 900, 24}}} // far on the left
	s.sound(sSight, 20)
	s.step()
	tr = s.track("e1")
	if tr.Ear.Agrees || tr.LocSeen || tr.Loc != tr.Ear.Est || tr.Pos != seen || tr.Vel != vel ||
		tr.Ear.Pan != perception.PanHardLeft || tr.Ear.Loud != perception.LoudMid {
		t.Fatalf("a sound from elsewhere: %+v", tr)
	}
	if d := dist(tr.Loc, Vec3{0, 0, 24}); d < 700 || d > 1100 || absf(angleDiff(yawTo(Vec3{}, tr.Loc), 90)) > 30 {
		t.Fatalf("stand-in %v", tr.Loc)
	}
}

// TestHitNarrowsTheEar: a hit whose bearing the view kick gives, from an
// attacker placed by ear, narrows its arc to that bearing (the bot's own
// state: it feels where the hit came from).
func TestHitNarrowsTheEar(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{voiceAt(30, 150, 300)}
	s.sound(sSight, 30)
	s.step()
	tr := s.track("e1")
	if tr.Ear.Spread < 60 {
		t.Fatalf("one sound on the left: %+v", tr.Ear)
	}
	s.w.feelHit(tr.ID, 160, 0)
	var e Ear // the actor's: the belief is published at the end of a frame
	for _, a := range s.w.actors {
		if a.ID == "e1" {
			e = a.Ear
		}
	}
	if e.Spread > hitArc+earBin || absf(angleDiff(e.Yaw, 160)) > hitArc || absf(angleDiff(yawTo(Vec3{}, e.Est), e.Yaw)) > 1 {
		t.Fatalf("after a hit from 160°: %+v", e)
	}
}

// TestEarPrefersTheSpawnThatAgrees: a monster never seen, tied to a lump
// entity, is placed at that entity's spawn origin (static map knowledge)
// while what is heard agrees with it, and by the cue alone once it does not.
func TestEarPrefersTheSpawnThatAgrees(t *testing.T) {
	s := newSim(t)
	spawn := Vec3{0, 300, 24} // on the left
	s.ents = []shared.EntityState{voiceAt(30, 90, 320)}
	s.sound(sSight, 30)
	s.step()
	s.w.level.Map = &mapdata.Map{Entities: []mapdata.Entity{{Index: 0}, {Index: 1, Classname: "monster_soldier", Origin: spawn}}}
	for _, a := range s.w.actors {
		a.Lump = 1
	}
	s.sound(sSight, 30)
	s.step()
	if tr := s.track("e1"); !tr.Ear.AtSpawn || tr.Loc != spawn || tr.LocSeen || tr.PosKnown {
		t.Fatalf("heard where it spawns: %+v", tr)
	}
	s.ents = []shared.EntityState{voiceAt(30, -90, 320)} // now on the right
	s.sound(sSight, 30)
	s.step()
	if tr := s.track("e1"); tr.Ear.AtSpawn || tr.Loc == spawn || absf(angleDiff(tr.Ear.Yaw, -90)) > tr.Ear.Spread {
		t.Fatalf("heard elsewhere: %+v", tr)
	}
}
