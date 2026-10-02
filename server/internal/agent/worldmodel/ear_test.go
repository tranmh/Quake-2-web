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

// withLump gives the sim's level an entity lump (static map knowledge)
// and baselines (which tie lump entities to entity numbers): Reset before
// the first step.
func (s *sim) withLump(ents []mapdata.Entity, baselines []shared.EntityState) {
	for i := range ents {
		ents[i].Index = i
	}
	s.w.Reset(Level{Key: LevelKey{Map: "floor"}, CM: floorCM(s.t), Map: &mapdata.Map{Entities: ents}})
	s.level.Baselines = baselines
}

// TestEarPrefersTheSpawnThatAgrees: a monster never seen is placed at a
// spawn origin of its family (static map knowledge) when that is the only
// one what is heard agrees with, at hearing's stand-in when several or
// none agree, and never at the spawn of a monster seen elsewhere.
func TestEarPrefersTheSpawnThatAgrees(t *testing.T) {
	spawn := Vec3{0, 300, 24} // on the left
	lump := func(extra ...mapdata.Entity) []mapdata.Entity {
		return append([]mapdata.Entity{{Classname: "worldspawn"},
			{Classname: "monster_soldier", Origin: spawn},
			{Classname: "monster_gunner", Origin: Vec3{-20, 310, 24}},               // on the left too: another family
			{Classname: "monster_soldier_ss", Origin: Vec3{0, -300, 24}}}, extra...) // on the right
	}
	s := newSim(t)
	s.withLump(lump(), nil)
	s.ents = []shared.EntityState{voiceAt(30, 90, 320)}
	s.sound(sSight, 30)
	s.step()
	if tr := s.track("e1"); !tr.Ear.AtSpawn || tr.Loc != spawn || tr.LocSeen || tr.PosKnown || tr.Lump != -1 {
		t.Fatalf("heard where the only soldier spawn on the left is: %+v", tr)
	}
	s.ents = []shared.EntityState{voiceAt(30, -150, 320)} // now behind on the right: the soldier_ss spawn there
	s.sound(sSight, 30)
	s.step()
	if tr := s.track("e1"); !tr.Ear.AtSpawn || tr.Loc != (Vec3{0, -300, 24}) {
		t.Fatalf("heard where the only soldier spawn on the right is: %+v", tr)
	}
	s.ents = []shared.EntityState{voiceAt(30, -60, 1400)} // far on the right: no spawn sounds so far
	s.sound(sSight, 30)
	s.step()
	if tr := s.track("e1"); tr.Ear.AtSpawn || tr.Loc != tr.Ear.Est || tr.Ear.Loud == perception.LoudNear ||
		absf(angleDiff(tr.Ear.Yaw, -60)) > tr.Ear.Spread {
		t.Fatalf("heard where no spawn is: %+v", tr)
	}

	// two soldier spawns agree: the stand-in, in the arc they are in
	two := newSim(t)
	two.withLump(lump(mapdata.Entity{Classname: "monster_soldier_light", Origin: Vec3{-60, 330, 24}}), nil)
	two.ents = []shared.EntityState{voiceAt(30, 90, 320)}
	two.sound(sSight, 30)
	two.step()
	if tr := two.track("e1"); tr.Ear.AtSpawn || tr.Loc != tr.Ear.Est || absf(angleDiff(tr.Ear.Yaw, 90)) > tr.Ear.Spread {
		t.Fatalf("two spawns agree: %+v", tr)
	}

	// the soldier of that spawn was seen elsewhere: its spawn is no
	// candidate
	seen := newSim(t)
	seen.withLump(lump(), []shared.EntityState{{Number: 20, ModelIndex: mSoldier, SkinNum: 2, Origin: spawn}})
	seen.ents = []shared.EntityState{soldierAt(20, Vec3{300, 0, 24})}
	seen.ents[0].SkinNum = 2
	seen.step()
	if tr := seen.track("e1"); tr.Lump != 1 {
		t.Fatalf("the soldier seen is not tied to its spawn: %+v", tr)
	}
	seen.ents = []shared.EntityState{{Number: 20, Origin: Vec3{300, 0, 24}}, voiceAt(30, 90, 320)}
	seen.sound(sSight, 30)
	seen.step()
	if tr := seen.track("e2"); tr.Ear.AtSpawn || tr.Loc == spawn {
		t.Fatalf("placed at the spawn of a soldier seen elsewhere: %+v", tr)
	}
}

// TestHeardNumbersDoNotPlace: the entity number of a sound says nothing
// about which of a family's monsters it is. Two soldiers never seen, each
// heard near a spawn of theirs, are believed alike whether or not their
// entity numbers are the ones the baselines tie to those spawns: swapping
// the numbers leaves the belief the same but for the numbers.
func TestHeardNumbersDoNotPlace(t *testing.T) {
	spawnA, spawnB := Vec3{0, 400, 24}, Vec3{-200, -500, 24}
	ents := func() []mapdata.Entity {
		return []mapdata.Entity{{Classname: "worldspawn"},
			{Classname: "monster_soldier_light", Origin: spawnA},
			{Classname: "monster_soldier_light", Origin: spawnB}}
	}
	baselines := []shared.EntityState{{Number: 30, ModelIndex: mSoldier, Origin: spawnA}, {Number: 31, ModelIndex: mSoldier, Origin: spawnB}}
	a, b := newSim(t), newSim(t)
	a.withLump(ents(), baselines)
	b.withLump(ents(), baselines)
	at := []Vec3{{40, 380, 24}, {-180, -460, 24}} // X near A, Y near B
	numA := []int32{30, 31}                       // a: the numbers the baselines give
	numB := []int32{31, 30}                       // b: swapped
	strip := func(bel *Belief) string {
		c := bel.Clone()
		for i := range c.Tracks {
			c.Tracks[i].Num = 0
		}
		for i := range c.Sounds {
			c.Sounds[i].Num = 0
		}
		return c.Digest()
	}
	atSpawn := 0
	for frame := 0; frame < 40; frame++ {
		for _, x := range []struct {
			s    *sim
			nums []int32
		}{{a, numA}, {b, numB}} {
			x.s.ps.ViewAngles[q2const.YAW] = float32(frame * 9) // a turn: front and back resolve
			x.s.ents = nil
			for k, p := range at {
				x.s.ents = append(x.s.ents, shared.EntityState{Number: x.nums[k], Origin: p})
				if frame%2 == k%2 {
					x.s.sound(sSight, x.nums[k])
				}
			}
			x.s.step()
		}
		if len(a.w.match.numToLump) != 2 {
			t.Fatalf("the baselines tie %d numbers to the lump", len(a.w.match.numToLump))
		}
		if da, db := strip(a.w.Belief()), strip(b.w.Belief()); da != db {
			t.Fatalf("frame %d: swapping the entity numbers changed the belief\n%+v\n%+v", frame, a.w.Belief().Tracks, b.w.Belief().Tracks)
		}
		for _, tr := range a.w.Belief().Tracks {
			if tr.Lump != -1 {
				t.Fatalf("a track never seen is tied to lump entity %d: %+v", tr.Lump, tr)
			}
			if tr.Ear.AtSpawn {
				atSpawn++
			}
		}
	}
	if atSpawn == 0 {
		t.Fatal("no track was ever placed at a spawn: the test shows nothing")
	}
}
