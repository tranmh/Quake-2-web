package worldmodel

import (
	"errors"
	"sync"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// TestWorldsShareMapAndAnims: the bots of one process share the immutable
// collision map, the class table and one animation cache; each World runs
// in its own goroutine (run with -race), and both come to the same belief
// as a World running alone.
func TestWorldsShareMapAndAnims(t *testing.T) {
	cm := floorCM(t)
	anims := perception.NewAnimCache(func(string) ([]byte, error) { return nil, errors.New("no pak") })
	anims.Set("models/monsters/soldier/tris.md2", perception.NewModelAnims(testSoldierFrames))
	classes := perception.NewClassTable()
	sims := make([]*sim, 3)
	for i := range sims {
		s := newSim(t)
		// models whose animations the shared cache loads lazily
		s.cs[q2const.CS_MODELS+9] = "models/monsters/infantry/tris.md2"
		s.cs[q2const.CS_MODELS+10] = "models/monsters/gunner/tris.md2"
		cfg := Config{Anims: anims, Classes: classes}
		if i == 2 {
			cfg = Config{Anims: perception.NewAnimCache(nil), Classes: perception.NewClassTable()} // alone
			cfg.Anims.Set("models/monsters/soldier/tris.md2", perception.NewModelAnims(testSoldierFrames))
		}
		s.w = New(cfg)
		s.w.Reset(Level{Key: LevelKey{Map: "floor"}, CM: cm})
		sims[i] = s
	}
	run := func(s *sim) string {
		for f := 0; f < 100; f++ {
			x := float32(150 + f*3)
			s.ents = []shared.EntityState{soldierAt(20, Vec3{x, 0, 24}),
				{Number: 21, ModelIndex: 9, Origin: Vec3{x, 60, 24}, Solid: solidStd},
				{Number: 22, ModelIndex: 10, Origin: Vec3{x, -60, 24}, Solid: solidStd, Frame: int32(f % 20)}}
			s.step()
		}
		return s.w.Belief().Digest()
	}
	digests := make([]string, len(sims))
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			digests[i] = run(sims[i])
		}()
	}
	wg.Wait()
	digests[2] = run(sims[2])
	if digests[0] != digests[1] || digests[0] != digests[2] {
		t.Fatalf("digests %v", digests)
	}
	if n := len(sims[0].w.Belief().Tracks); n != 3 {
		t.Fatalf("%d tracks", n)
	}
}
