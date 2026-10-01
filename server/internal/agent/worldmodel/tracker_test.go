package worldmodel

import (
	"math"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

func TestTrackFollowsAndDecays(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
	b := s.step()
	if len(b.Tracks) != 1 {
		t.Fatalf("tracks %+v", b.Tracks)
	}
	tr := s.track("e1")
	if tr.Num != 20 || tr.Class != "soldier_light" || !tr.Visible || tr.Life != LifeAlive || tr.Anim != "stand" ||
		tr.FirstSeen != 100 || tr.Confidence != 1 || tr.Threat <= 0 || tr.Lump != -1 {
		t.Fatalf("track %+v", tr)
	}
	s.ents[0].Origin = Vec3{210, 0, 24}
	s.step()
	if tr := s.track("e1"); tr.Pos != (Vec3{210, 0, 24}) || math.Abs(float64(tr.Vel[0]-50)) > 1e-3 {
		t.Fatalf("pos %v vel %v (EMA of 100 u/s from 0)", tr.Pos, tr.Vel)
	}

	// it walks out of the PVS: its spot is in view and empty
	s.ents = nil
	s.step()
	if tr := s.track("e1"); tr.Visible || !tr.Missing || tr.Life != LifeAlive || tr.Pos != (Vec3{210, 0, 24}) {
		t.Fatalf("after leaving %+v", tr)
	}
	for i := 0; i < 30; i++ {
		s.step()
	}
	tr = s.track("e1")
	if want := float32(math.Exp(-1)); math.Abs(float64(tr.Confidence-want)) > 0.02 {
		t.Fatalf("confidence %v after 3 s, want ~%v", tr.Confidence, want)
	}
	// it comes back on the same number: same track
	s.ents = []shared.EntityState{soldierAt(20, Vec3{300, 0, 24})}
	s.step()
	if b := s.w.Belief(); len(b.Tracks) != 1 || !b.Tracks[0].Visible || b.Tracks[0].Missing {
		t.Fatalf("return %+v", b.Tracks)
	}
	// the number is reused by a rocket: the soldier is gone, a projectile appears
	s.ents = []shared.EntityState{{Number: 20, ModelIndex: mRocket, Origin: Vec3{300, 0, 30}, OldOrigin: Vec3{365, 0, 30}, Solid: 4129}}
	b = s.step()
	if tr := s.track("e1"); tr.Life != LifeGone || tr.Visible {
		t.Fatalf("reused number: old track %+v", tr)
	}
	if len(b.Projectiles) != 1 || b.Projectiles[0].ID != "p1" || b.Projectiles[0].Class != "rocket" ||
		b.Projectiles[0].Vel != (Vec3{-650, 0, 0}) || b.Projectiles[0].Own {
		t.Fatalf("projectiles %+v", b.Projectiles)
	}
}

func TestNewTrackOnTeleport(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
	s.step()
	s.ents[0].Origin = Vec3{900, 0, 24} // > 512 units in one frame: a new entity
	s.step()
	b := s.w.Belief()
	if len(b.Tracks) != 2 || b.Tracks[0].Life != LifeGone || b.Tracks[1].ID != "e2" || b.Tracks[1].Pos[0] != 900 {
		t.Fatalf("tracks after a teleport-like jump %+v", b.Tracks)
	}
}

func TestDeathSignals(t *testing.T) {
	type step func(s *sim)
	cases := []struct {
		name  string
		steps []step
		want  []LifeState // after each step
	}{
		{"death animation", []step{
			func(s *sim) { s.ents[0].Frame = 6 },
			func(s *sim) { s.ents[0].Frame = 7 },
			func(s *sim) { s.ents[0].Frame = 8 },
		}, []LifeState{LifeDying, LifeDying, LifeDead}},
		{"death cry", []step{
			func(s *sim) { s.sound(sDeath, 20) },
		}, []LifeState{LifeDying}},
		{"not solid", []step{
			func(s *sim) { s.ents[0].Solid = 0 },
		}, []LifeState{LifeDead}},
		{"gib model", []step{
			func(s *sim) {
				s.ents[0] = shared.EntityState{Number: 20, ModelIndex: mGibHead, Origin: Vec3{205, 0, 10}, Effects: q2const.EF_GIB}
			},
		}, []LifeState{LifeGibbed}},
		{"gib sound", []step{
			func(s *sim) { s.sound(sGib, 20) },
		}, []LifeState{LifeGibbed}},
		{"vanished in an explosion", []step{
			func(s *sim) {
				s.ents = nil
				s.ev.TempEnts = []fakeclient.TempEnt{{Type: q2const.TE_ROCKET_EXPLOSION, Pos: Vec3{210, 0, 20}}}
			},
		}, []LifeState{LifeGibbed}},
		{"corpse vanished", []step{
			func(s *sim) { s.ents[0].Solid = 0 },
			func(s *sim) { s.ents = nil },
		}, []LifeState{LifeDead, LifeGone}},
		{"medic revive", []step{
			func(s *sim) { s.ents[0].Solid, s.ents[0].Frame = 0, 8 },
			func(s *sim) { s.ents[0].Solid, s.ents[0].Frame = solidStd, 0 },
		}, []LifeState{LifeDead, LifeAlive}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSim(t)
			s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
			s.step()
			for i, f := range tc.steps {
				f(s)
				s.step()
				if got := s.track("e1").Life; got != tc.want[i] {
					t.Fatalf("step %d: %s, want %s", i, got, tc.want[i])
				}
			}
			if tc.name == "medic revive" && s.track("e1").Revived != 1 {
				t.Fatal("revive not counted")
			}
			if len(s.w.Belief().Tracks) != 1 {
				t.Fatalf("death made new tracks: %+v", s.w.Belief().Tracks)
			}
		})
	}
}

func TestAwareness(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
	s.step()
	if tr := s.track("e1"); tr.Awareness != Idle {
		t.Fatalf("awareness %s", tr.Awareness)
	}
	s.sound(sSight, 20)
	s.step()
	if tr := s.track("e1"); tr.Awareness != Alert {
		t.Fatalf("after a sight cry %s", tr.Awareness)
	}
	s.ev.MuzzleFlashes = []fakeclient.MuzzleFlash{{Ent: 20, Weapon: q2const.MZ2_SOLDIER_BLASTER_1, Monster: true}}
	s.step()
	tr := s.track("e1")
	if tr.Awareness != Attacking || tr.Weapon != "blaster" || tr.LastAttack != s.now {
		t.Fatalf("after a muzzle flash %+v", tr)
	}
	if !s.w.Belief().Self.InCombat {
		t.Fatal("an attacking soldier in view is combat")
	}
	for i := 0; i < 31; i++ {
		s.step()
	}
	if tr := s.track("e1"); tr.Awareness != Alert {
		t.Fatalf("3 s after the last shot: %s", tr.Awareness)
	}
	// the attack animation counts as attacking
	s.ents[0].Frame = 4
	s.step()
	if tr := s.track("e1"); tr.Awareness != Attacking {
		t.Fatalf("attack frame: %s", tr.Awareness)
	}

	// a hidden monster's flash creates a track at its position
	s2 := newSim(t)
	s2.ents = []shared.EntityState{soldierAt(30, Vec3{-300, 0, 24})} // behind the player
	s2.ev.MuzzleFlashes = []fakeclient.MuzzleFlash{{Ent: 30, Weapon: q2const.MZ2_SOLDIER_MACHINEGUN_1, Monster: true}}
	b := s2.step()
	if len(b.Tracks) != 1 || b.Tracks[0].Visible || !b.Tracks[0].Heard || b.Tracks[0].Pos != (Vec3{-300, 0, 24}) ||
		b.Tracks[0].Class != "monster" || b.Tracks[0].Awareness != Attacking {
		t.Fatalf("heard flash: %+v", b.Tracks)
	}
	// later seen: the same track learns its class
	s2.ps.ViewAngles[q2const.YAW] = 180
	s2.step()
	if b := s2.w.Belief(); len(b.Tracks) != 1 || b.Tracks[0].Class != "soldier_light" || !b.Tracks[0].Visible {
		t.Fatalf("seen after heard: %+v", b.Tracks)
	}
}

func TestOccludedMonsterNotTracked(t *testing.T) {
	s := newSim(t)
	// under the slab (in the PVS, occluded) and behind the player
	s.ents = []shared.EntityState{soldierAt(20, Vec3{0, 0, -60}), soldierAt(21, Vec3{-200, 0, 24})}
	for i := 0; i < 5; i++ {
		s.step()
	}
	if b := s.w.Belief(); len(b.Tracks) != 0 {
		t.Fatalf("hidden monsters tracked: %+v", b.Tracks)
	}
	// turning around reveals 21
	s.ps.ViewAngles[q2const.YAW] = 180
	s.step()
	if b := s.w.Belief(); len(b.Tracks) != 1 || b.Tracks[0].Num != 21 {
		t.Fatalf("after turning: %+v", b.Tracks)
	}
}

func TestItemTakenAndGone(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{
		{Number: 40, ModelIndex: mStim, Origin: Vec3{100, 0, 16}},
		{Number: 41, ModelIndex: mStim, Origin: Vec3{300, 50, 16}},
	}
	b := s.step()
	if len(b.Items) != 2 || b.Items[0].ID != "i1" || b.Items[0].Pickup != "Health" || b.Items[0].Index != 1 ||
		b.Items[0].Life != LifeAlive || !b.Items[0].Visible {
		t.Fatalf("items %+v", b.Items)
	}
	// the player walks onto the first one: it disappears with a pickup message
	s.setOrigin(Vec3{100, 0, 24})
	s.ents = s.ents[1:]
	s.ps.Stats[q2const.STAT_PICKUP_STRING] = int16(q2const.CS_ITEMS + 1)
	b = s.step()
	if b.Items[0].Life != LifeTaken || b.Self.Pickup != "Health" || b.Self.PickupAt != s.now {
		t.Fatalf("after the pickup %+v self %+v", b.Items[0], b.Self)
	}
	// the second vanishes in view without a pickup
	s.setOrigin(Vec3{0, 0, 24})
	s.ps.Stats[q2const.STAT_PICKUP_STRING] = 0
	s.ents = nil
	for i := 0; i < 12; i++ {
		b = s.step()
	}
	if b.Items[1].Life != LifeGone {
		t.Fatalf("vanished item %+v", b.Items[1])
	}
	if !b.Inventory.Stale && b.Inventory.Known {
		t.Fatal("pickup must make a known inventory stale")
	}
}

func TestProjectileDodge(t *testing.T) {
	cases := []struct {
		y     float32
		solid int32
		vel   float32
		side  int
		dang  bool
		own   bool
	}{
		{30, 4129, -650, 1, true, false},   // passes on the left (+y): strafe right
		{-30, 4129, -650, -1, true, false}, // passes on the right: strafe left
		{30, 4129, 650, 0, false, false},   // flying away
		{30, 0, -650, 0, false, true},      // the bot's own rocket
		{400, 4129, -650, 0, false, false}, // far off the line
	}
	for _, tc := range cases {
		s := newSim(t)
		s.ents = []shared.EntityState{{Number: 50, ModelIndex: mRocket, Origin: Vec3{500, tc.y, 28},
			OldOrigin: Vec3{500 - tc.vel/10, tc.y, 28}, Solid: tc.solid}}
		b := s.step()
		if len(b.Projectiles) != 1 {
			t.Fatalf("y=%v: projectiles %+v", tc.y, b.Projectiles)
		}
		p := b.Projectiles[0]
		if p.Own != tc.own || p.Danger != tc.dang || (tc.dang && p.DodgeSide != tc.side) {
			t.Errorf("y=%v vel=%v: %+v, want side %d danger %v own %v", tc.y, tc.vel, p, tc.side, tc.dang, tc.own)
		}
		if tc.dang && (p.TCA < 0.6 || p.TCA > 0.9 || math.Abs(float64(p.Miss)-math.Abs(float64(tc.y))) > 1) {
			t.Errorf("y=%v: tca %v miss %v", tc.y, p.TCA, p.Miss)
		}
		if tc.dang && !b.Self.InCombat {
			t.Error("an incoming rocket is combat")
		}
	}
	// a projectile out of view is not in the belief
	s := newSim(t)
	s.ents = []shared.EntityState{{Number: 50, ModelIndex: mRocket, Origin: Vec3{-500, 0, 28}, Solid: 4129}}
	if b := s.step(); len(b.Projectiles) != 0 {
		t.Fatalf("hidden projectile %+v", b.Projectiles)
	}
}

func TestLaserOnOff(t *testing.T) {
	s := newSim(t)
	beam := shared.EntityState{Number: 60, ModelIndex: 1, RenderFX: q2const.RF_BEAM, Origin: Vec3{300, -100, 30}, OldOrigin: Vec3{300, 100, 30}}
	s.ents = []shared.EntityState{beam}
	b := s.step()
	if len(b.Lasers) != 1 || b.Lasers[0].State != LaserOn || b.Lasers[0].End != beam.OldOrigin {
		t.Fatalf("lasers %+v", b.Lasers)
	}
	s.ents = nil
	b = s.step()
	if b.Lasers[0].State != LaserOff || b.Lasers[0].LastUpdate != s.now {
		t.Fatalf("laser not confirmed off: %+v", b.Lasers)
	}
	// out of view the laser's state is not refreshed
	s.ps.ViewAngles[q2const.YAW] = 180
	s.ents = []shared.EntityState{beam}
	before := s.w.Belief().Lasers[0].LastUpdate
	b = s.step()
	if b.Lasers[0].State != LaserOff || b.Lasers[0].LastUpdate != before {
		t.Fatalf("hidden laser changed the belief: %+v", b.Lasers)
	}
}

func TestSnapshotIsDeep(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
	s.step()
	snap := s.w.Snapshot()
	d := snap.Digest()
	s.ents[0].Origin = Vec3{250, 0, 24}
	s.step()
	if snap.Tracks[0].Pos != (Vec3{200, 0, 24}) || snap.Digest() != d {
		t.Fatal("snapshot changed with the world")
	}
	if s.w.Belief().Digest() == d {
		t.Fatal("digest did not change with the belief")
	}
	again := snap.Clone()
	if again.Digest() != d {
		t.Fatal("clone digest differs")
	}
}

// TestActorsBounded: actors that vanished long ago and stand for no lump
// entity are forgotten once there are many; live bindings survive.
func TestActorsBounded(t *testing.T) {
	s := newSim(t)
	keepNum := int32(500)
	for i := 0; i < maxActors+20; i++ {
		num := int32(100 + i%300)
		s.ents = []shared.EntityState{soldierAt(keepNum, Vec3{150, 0, 24}), soldierAt(num, Vec3{200, float32(i % 50), 24})}
		s.step()
		s.ents = []shared.EntityState{soldierAt(keepNum, Vec3{150, 0, 24}),
			{Number: num, ModelIndex: mRocket, Origin: Vec3{300, 0, 30}, Solid: 4129}} // number reuse: the soldier is gone
		s.step()
	}
	for i := 0; i < 310; i++ {
		s.step()
	}
	b := s.w.Belief()
	if len(b.Tracks) > maxActors {
		t.Fatalf("%d tracks kept", len(b.Tracks))
	}
	var kept *Track
	for i := range b.Tracks {
		if b.Tracks[i].Num == keepNum && b.Tracks[i].Life == LifeAlive {
			kept = &b.Tracks[i]
		}
	}
	if kept == nil || kept.FirstSeen != 100 || !kept.Visible {
		t.Fatalf("the live track was lost: %+v", kept)
	}
	// its binding still works after the compaction
	id := kept.ID
	s.ents[0].Origin = Vec3{160, 0, 24}
	s.step()
	if tr := s.track(id); tr.Pos[0] != 160 {
		t.Fatalf("binding after compaction: %+v", tr)
	}
}
