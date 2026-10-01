package worldmodel

import (
	"errors"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// TestAutoEnterWithoutDataIsBlind: a map change the caller did not Reset
// for, with no way to load the new map, enters it blind: a soldier in plain
// view is not seen (the packet is the server's PVS, not the bot's view),
// its cry is still heard.
func TestAutoEnterWithoutDataIsBlind(t *testing.T) {
	s := newSim(t)
	s.step()
	s.level = &perception.LevelStatic{Gen: 2, MapName: "elsewhere"}
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24})}
	b := s.step()
	if s.w.Level().Key != (LevelKey{Map: "elsewhere"}) || s.w.Level().CM != nil {
		t.Fatalf("level %+v", s.w.Level())
	}
	if len(b.Tracks) != 0 || len(s.w.Percept().Seen) != 0 {
		t.Fatalf("blind level: tracks %+v", b.Tracks)
	}
	s.sound(sSight, 20)
	if b = s.step(); len(b.Tracks) != 1 || b.Tracks[0].Visible || !b.Tracks[0].Heard {
		t.Fatalf("heard on a blind level: %+v", b.Tracks)
	}
}

// demo1Wall finds, from the demo1 start, a view direction with a wall in
// front: a point short of it and one behind it.
func demo1Wall(t *testing.T, cm *cmodel.Map) (eye, near, behind Vec3, yaw float32) {
	t.Helper()
	st := cmodel.NewState(cm)
	eye = Vec3{128, -320, 64} // info_player_start + view height
	for yaw = 0; yaw < 360; yaw += 45 {
		var fwd Vec3
		shared.AngleVectors(Vec3{0, yaw, 0}, &fwd, nil, nil)
		tr := st.BoxTrace(eye, shared.VectorMA(eye, 4096, fwd), Vec3{}, Vec3{}, 0, q2const.MASK_OPAQUE)
		if d := tr.Fraction * 4096; tr.Fraction < 1 && d > 160 {
			return eye, shared.VectorMA(eye, d-64, fwd), shared.VectorMA(eye, d+96, fwd), yaw
		}
	}
	t.Fatal("no wall around the demo1 start")
	return
}

func demo1Frame(cs *perception.ConfigStrings, ls *perception.LevelStatic, frame int32, eye Vec3, yaw float32, ents ...shared.EntityState) perception.FrameInput {
	var ps shared.PlayerState
	origin := Vec3{eye[0], eye[1], eye[2] - 22}
	for i := 0; i < 3; i++ {
		ps.PMove.Origin[i] = int16(origin[i] * 8)
	}
	ps.ViewOffset = Vec3{0, 0, 22}
	ps.ViewAngles = Vec3{0, yaw, 0}
	ps.Fov = 90
	ps.Stats[q2const.STAT_HEALTH] = 100
	in := perception.FrameInput{Level: ls, CS: cs, ServerFrame: frame, ServerTime: frame * 100, PlayerState: ps,
		Entities: append([]shared.EntityState{{Number: 1, ModelIndex: 255, Origin: origin}}, ents...)}
	in.AreaBytes = len(in.AreaBits)
	for i := range in.AreaBits {
		in.AreaBits[i] = 0xff
	}
	return in
}

// TestAutoEnterLoadsTheMap: an unannounced map change loads the new map's
// collision model (with ReadFile, or the whole Level with LoadLevel): a
// soldier behind a wall in the packet stays out of the belief, one in view
// is tracked. Each new map entry is the next visit of that map; a reload
// keeps the visit.
func TestAutoEnterLoadsTheMap(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	lv := loadLevel(t, fs, "demo1", 0)
	eye, near, behind, yaw := demo1Wall(t, lv.Map.CM)
	var cs perception.ConfigStrings
	cs[q2const.CS_MODELS+1] = "maps/demo1.bsp"
	cs[q2const.CS_MODELS+3] = "models/monsters/soldier/tris.md2"
	hidden := soldierAt(20, Vec3{behind[0], behind[1], behind[2] - 20})
	seen := soldierAt(21, Vec3{near[0], near[1], near[2] - 20})

	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"ReadFile", Config{ReadFile: fs.ReadFile}},
		{"LoadLevel", Config{LoadLevel: func(name string) (Level, error) {
			if name != "demo1" {
				return Level{}, errors.New("unknown map")
			}
			return loadLevel(t, fs, name, 7), nil // its key is ignored
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := New(tc.cfg) // never Reset: the first frame enters the level
			ls := &perception.LevelStatic{Gen: 1, MapName: "demo1"}
			w.Update(demo1Frame(&cs, ls, 1, eye, yaw, hidden, seen), 100)
			b := w.Belief()
			if w.Level().CM == nil || w.Level().Key != (LevelKey{Map: "demo1"}) {
				t.Fatalf("level %+v", w.Level().Key)
			}
			if (tc.name == "LoadLevel") != (w.Level().Map != nil) {
				t.Fatalf("static data: %v", w.Level().Map != nil)
			}
			if len(b.Tracks) != 1 || b.Tracks[0].Num != 21 {
				t.Fatalf("tracks %+v (hidden #20 behind the wall, #21 in view)", b.Tracks)
			}

			// demo2 (ReadFile only knows it), back to demo1: a second
			// visit; a reload of it keeps the visit
			ls = &perception.LevelStatic{Gen: 2, MapName: "demo2"}
			w.Update(demo1Frame(&cs, ls, 1, eye, yaw), 200)
			if w.Level().Key != (LevelKey{Map: "demo2"}) || (tc.name == "ReadFile") != (w.Level().CM != nil) {
				t.Fatalf("demo2 entry %+v, cm %v", w.Level().Key, w.Level().CM != nil)
			}
			w.Update(demo1Frame(&cs, &perception.LevelStatic{Gen: 3, MapName: "demo1"}, 1, eye, yaw), 300)
			if k := w.Level().Key; k != (LevelKey{Map: "demo1", Visit: 1}) {
				t.Fatalf("second demo1 entry %+v", k)
			}
			w.Update(demo1Frame(&cs, &perception.LevelStatic{Gen: 4, MapName: "demo1"}, 1, eye, yaw), 400)
			if k := w.Level().Key; k != (LevelKey{Map: "demo1", Visit: 1}) || w.Memory().Entries != 2 {
				t.Fatalf("reload %+v, entries %d", k, w.Memory().Entries)
			}
			if m := w.MemoryFor(LevelKey{Map: "demo1"}); m == nil || m.Entries != 1 {
				t.Fatalf("first visit memory %+v", m)
			}
		})
	}
}
