package perception

import "testing"

func TestModelAnimsSequences(t *testing.T) {
	names := []string{"stand101", "stand102", "stand301", "walk01", "walk02", "run01", "runs01", "runs02",
		"attak101", "attak201", "pain101", "pain102", "duck01", "death101", "death102", "death103",
		"death201", "death202", "standb1", "standb2", "att_c1", "r_attb5", "slam2", "drain01", "fall3", "gun02",
		"defens01", "block01", "bankl01"}
	m := NewModelAnims(names)
	want := map[string]struct {
		seq   string
		state AnimState
	}{
		"stand101": {"stand1", AnimStand}, "stand301": {"stand3", AnimStand}, "walk02": {"walk", AnimWalk},
		"run01": {"run", AnimRun}, "runs02": {"runs", AnimAttack}, "attak201": {"attak2", AnimAttack},
		"pain102": {"pain1", AnimPain}, "duck01": {"duck", AnimDuck}, "death103": {"death1", AnimDeath},
		"death201": {"death2", AnimDeath}, "standb2": {"standb", AnimStand}, "att_c1": {"att_c", AnimAttack},
		"r_attb5": {"r_attb", AnimAttack}, "slam2": {"slam", AnimAttack}, "drain01": {"drain", AnimAttack},
		"fall3": {"fall", AnimMove}, "gun02": {"gun", AnimStand}, "defens01": {"defens", AnimDuck},
		"block01": {"block", AnimDuck}, "bankl01": {"bankl", AnimMove},
	}
	for i, n := range names {
		fa := m.At(int32(i))
		if w, ok := want[n]; ok && (fa.Sequence != w.seq || fa.State != w.state) {
			t.Errorf("%s: %s/%s, want %s/%s", n, fa.Sequence, fa.State, w.seq, w.state)
		}
		if fa.Name != n {
			t.Errorf("frame %d name %q", i, fa.Name)
		}
	}
	// sequence bounds: death1 is frames 13..15, death2 16..17
	if fa := m.At(14); fa.First != 13 || fa.Last != 15 {
		t.Errorf("death1 bounds %d..%d", fa.First, fa.Last)
	}
	if fa := m.At(16); fa.First != 16 || fa.Last != 17 {
		t.Errorf("death2 bounds %d..%d", fa.First, fa.Last)
	}
	if m.At(-1).State != AnimUnknown || m.At(int32(len(names))).State != AnimUnknown {
		t.Error("out of range frames must be unknown")
	}
	var nilm *ModelAnims
	if nilm.At(0).State != AnimUnknown {
		t.Error("nil model")
	}
}

// TestPakMonsterAnims maps the demo pak's monster MD2 frame names: every
// monster has standing, attack, pain and death frames, the soldier's frame
// table matches the game's frame numbers.
func TestPakMonsterAnims(t *testing.T) {
	fs := demoFS(t)
	cache := NewAnimCache(fs.ReadFile)
	for _, model := range []string{"models/monsters/soldier/tris.md2", "models/monsters/infantry/tris.md2",
		"models/monsters/gunner/tris.md2", "models/monsters/berserk/tris.md2", "models/monsters/parasite/tris.md2",
		"models/monsters/tank/tris.md2", "models/monsters/flyer/tris.md2"} {
		m := cache.Model(model)
		if m == nil {
			t.Fatalf("%s: no animations", model)
		}
		seen := map[AnimState]bool{}
		for _, f := range m.Frames {
			seen[f.State] = true
		}
		need := []AnimState{AnimStand, AnimAttack, AnimPain}
		if model != "models/monsters/flyer/tris.md2" { // flyers explode, no death frames
			need = append(need, AnimDeath)
		}
		for _, st := range need {
			if !seen[st] {
				t.Errorf("%s: no %s frames", model, st)
			}
		}
	}
	// soldier frame numbers (C: game/m_soldier.h FRAME_attak101 0,
	// FRAME_runs01 109, FRAME_stand101 146, FRAME_death101 272, FRAME_death136 307)
	s := cache.Model("models/monsters/soldier/tris.md2")
	for frame, want := range map[int32]AnimState{0: AnimAttack, 45: AnimDuck, 50: AnimPain, 97: AnimRun,
		109: AnimAttack, 146: AnimStand, 215: AnimWalk, 272: AnimDeath} {
		if got := s.At(frame).State; got != want {
			t.Errorf("soldier frame %d: %s, want %s", frame, got, want)
		}
	}
	if fa := s.At(280); fa.Sequence != "death1" || fa.First != 272 {
		t.Errorf("soldier frame 280: %+v", fa)
	}
	if cache.Model("models/monsters/medic/tris.md2") != nil { // not in the demo pak
		t.Error("missing model has animations")
	}
	if cache.Model("*3") != nil || cache.Model("sprites/s_bfg1.sp2") != nil {
		t.Error("non-MD2 has animations")
	}
}
