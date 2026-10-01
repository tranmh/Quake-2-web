package worldmodel

import (
	"math/rand"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// TestRandomFramesDoNotBreak feeds frames of random entity states, events
// and player stats (what a hostile or broken server could send) through
// the perception and the world model: nothing panics and the belief stays
// encodable (no NaN) and bounded.
func TestRandomFramesDoNotBreak(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	s := newSim(t)
	coord := func() float32 { return float32(rng.Intn(8192)-4096) / 8 * float32(1+rng.Intn(8)) }
	vec := func() Vec3 { return Vec3{coord(), coord(), coord()} }
	frames := 1500
	if testing.Short() {
		frames = 500
	}
	for f := 0; f < frames; f++ {
		s.ents = s.ents[:0]
		for i, n := 0, rng.Intn(12); i < n; i++ {
			e := shared.EntityState{Number: int32(2 + rng.Intn(30)), ModelIndex: int32(rng.Intn(260)),
				Origin: vec(), OldOrigin: vec(), Angles: vec(), Frame: int32(rng.Intn(600) - 50),
				SkinNum: int32(rng.Intn(10)), Solid: int32(rng.Intn(70000)), Sound: int32(rng.Intn(300))}
			if rng.Intn(4) == 0 {
				e.RenderFX = q2const.RF_BEAM
			}
			if rng.Intn(4) == 0 {
				e.Effects = uint32(rng.Int63())
			}
			s.ents = append(s.ents, e)
		}
		for i, n := 0, rng.Intn(4); i < n; i++ {
			snd := fakeclient.Sound{SoundNum: int32(rng.Intn(300)), Ent: int32(rng.Intn(40)), Volume: rng.Float32(),
				Attenuation: float32(rng.Intn(5))}
			if rng.Intn(2) == 0 {
				p := vec()
				snd.Pos = &p
			}
			s.ev.Sounds = append(s.ev.Sounds, snd)
			s.ev.MuzzleFlashes = append(s.ev.MuzzleFlashes, fakeclient.MuzzleFlash{Ent: int32(rng.Intn(40)),
				Weapon: int32(rng.Intn(256)), Monster: rng.Intn(2) == 0})
			s.ev.TempEnts = append(s.ev.TempEnts, fakeclient.TempEnt{Type: int32(rng.Intn(60)), Pos: vec(), Pos2: vec()})
		}
		if rng.Intn(10) == 0 {
			s.ev.Layouts = []string{"xv 32 yv 8 picn help string2 \"x\" cstring2 \"" + string(rune(rng.Intn(200))) + "\""}
		}
		for i := range s.ps.Stats {
			if rng.Intn(5) == 0 {
				s.ps.Stats[i] = int16(rng.Intn(65536) - 32768)
			}
		}
		s.ps.GunIndex = int32(rng.Intn(300) - 20)
		s.ps.KickAngles = Vec3{float32(rng.Intn(256)-128) / 4, 0, float32(rng.Intn(256)-128) / 4}
		s.ps.ViewAngles = Vec3{float32(rng.Intn(180) - 90), float32(rng.Intn(360)), 0}
		s.ps.Fov = float32(rng.Intn(200) - 10)
		s.ps.PMove.PmType = int32(rng.Intn(6))
		for i := 0; i < 3; i++ {
			s.ps.PMove.Origin[i] = int16(rng.Intn(65536) - 32768)
			s.ps.PMove.Velocity[i] = int16(rng.Intn(65536) - 32768)
		}
		if rng.Intn(50) == 0 {
			s.invSeq++
			for i := range s.inv {
				s.inv[i] = int32(rng.Intn(3))
			}
		}
		b := s.step()
		_ = b.Digest() // panics on NaN
		if f%97 == 0 {
			_ = s.w.Snapshot()
			s.w.WantsInventoryRefresh()
			s.w.WantsHelpRefresh()
		}
	}
	if b := s.w.Belief(); len(b.Tracks) > maxActors+40 || len(b.Sounds) > 400 {
		t.Fatalf("belief grew: %d tracks, %d sounds", len(b.Tracks), len(b.Sounds))
	}
}
