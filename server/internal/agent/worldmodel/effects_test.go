package worldmodel

import (
	"math"
	"strconv"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// staticFrame builds a frame of a real level with the player's eye at eye
// looking at target, the area bits of the eye's area (all portals closed)
// and the given entities.
func staticFrame(cm *cmodel.Map, ls *perception.LevelStatic, cs *perception.ConfigStrings, frame int32, eye, target Vec3, ents ...shared.EntityState) perception.FrameInput {
	st := cmodel.NewState(cm)
	var ps shared.PlayerState
	origin := Vec3{eye[0], eye[1], eye[2] - 22}
	for i := 0; i < 3; i++ {
		ps.PMove.Origin[i] = int16(origin[i] * 8)
	}
	ps.ViewOffset = shared.VectorSubtract(eye, Vec3{float32(ps.PMove.Origin[0]) / 8, float32(ps.PMove.Origin[1]) / 8, float32(ps.PMove.Origin[2]) / 8})
	d := shared.VectorSubtract(target, eye)
	ps.ViewAngles = Vec3{float32(-math.Atan2(float64(d[2]), math.Hypot(float64(d[0]), float64(d[1]))) * 180 / math.Pi),
		float32(math.Atan2(float64(d[1]), float64(d[0])) * 180 / math.Pi), 0}
	ps.Fov = 90
	ps.Stats[q2const.STAT_HEALTH] = 100
	in := perception.FrameInput{Level: ls, CS: cs, ServerFrame: frame, ServerTime: frame * 100, PlayerState: ps,
		Entities: append([]shared.EntityState{{Number: 1, ModelIndex: 255, Origin: origin}}, ents...)}
	leaf := st.PointLeafnum(eye)
	in.AreaBytes = st.WriteAreaBits(in.AreaBits[:], int(cm.LeafArea(int(leaf))))
	return in
}

// TestLaserOffConfirmedByEffect: demo3's lasers (lump 176/177, killed with
// gunner #418) are tied to their entity by the baseline; the beam seen is
// "on", the segment in view without the beam is "off", and the effect
// names the lump entity.
func TestLaserOffConfirmedByEffect(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	lv := loadLevel(t, fs, "demo3", 0)
	cm := lv.Map.CM
	las := lv.Map.Lasers[0]
	beam := shared.EntityState{Number: 50, ModelIndex: 1, RenderFX: q2const.RF_BEAM | q2const.RF_TRANSLUCENT,
		Origin: las.Start, OldOrigin: las.End, Frame: 4}
	var cs perception.ConfigStrings
	cs[q2const.CS_MODELS+1] = "maps/demo3.bsp"
	ls := &perception.LevelStatic{Gen: 1, MapName: "demo3", Baselines: []shared.EntityState{beam}}

	// a viewpoint beside the beam that sees it and hears its start
	st := cmodel.NewState(cm)
	var eye Vec3
	found := false
	for _, k := range []float32{96, 192, 288} {
		for _, off := range []Vec3{{0, -40, 24}, {0, 40, 24}, {0, -40, -24}, {0, 40, -24}, {0, -96, 0}, {0, 96, 0}} {
			e := shared.VectorAdd(shared.VectorMA(las.Start, k, las.Movedir), off)
			if found || st.PointContents(e, 0) != 0 {
				continue
			}
			in := staticFrame(cm, ls, &cs, 1, e, las.End)
			v := perception.NewVision(cm, perception.ViewOptions{})
			v.Begin(e, in.PlayerState.ViewAngles, 90, in.AreaBits[:in.AreaBytes], nil)
			if v.InPHS(las.Start) && v.SeesSegment(las.Start, las.End, 8, -1) {
				eye, found = e, true
			}
		}
	}
	if !found {
		t.Fatal("no viewpoint of the laser found")
	}
	w := New(Config{})
	w.Reset(lv)
	w.Update(staticFrame(cm, ls, &cs, 1, eye, las.End, beam), 100)
	b := w.Belief()
	l := b.Laser(las.Entity)
	if l == nil || l.Num != 50 || l.State != LaserOn {
		t.Fatalf("laser %+v (lasers %+v)", l, b.Lasers)
	}
	if b.Effect(EffectLaserOff, las.Entity) != nil {
		t.Fatal("off before it went off")
	}
	// the gunner died: the beam is gone while its segment is in view
	w.Update(staticFrame(cm, ls, &cs, 2, eye, las.End), 200)
	b = w.Belief()
	if l := b.Laser(las.Entity); l.State != LaserOff {
		t.Fatalf("laser %+v", l)
	}
	if e := b.Effect(EffectLaserOff, las.Entity); e == nil || e.At != 200 || e.Frame != 2 {
		t.Fatalf("effects %+v", b.Effects)
	}
	// looking up, a laser that went off is not noticed
	w2 := New(Config{})
	w2.Reset(lv)
	away := shared.VectorAdd(eye, Vec3{0, 0, 100})
	w2.Update(staticFrame(cm, ls, &cs, 1, eye, away), 100)
	if l := w2.Belief().Laser(las.Entity); l == nil || l.State != LaserUnknown || w2.Belief().Effect(EffectLaserOff, las.Entity) != nil {
		t.Fatalf("laser out of view %+v", l)
	}
}

// TestMoverMovedConfirmedByEffect: a demo1 door seen at its spawn pose and
// then raised gives the "mover moved" effect for its lump entity.
func TestMoverMovedConfirmedByEffect(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	lv := loadLevel(t, fs, "demo1", 0)
	cm := lv.Map.CM
	var door *struct {
		lump  int
		model string
	}
	for _, m := range lv.Map.Movers {
		if m.Classname == "func_door" && m.Pos2 != m.Pos1 {
			door = &struct {
				lump  int
				model string
			}{m.Entity, m.Model}
			break
		}
	}
	if door == nil {
		t.Fatal("no door in demo1")
	}
	n, _ := strconv.Atoi(door.model[1:])
	mod := cm.InlineModel(door.model)
	center := shared.VectorScale(shared.VectorAdd(mod.Mins, mod.Maxs), 0.5)
	var cs perception.ConfigStrings
	cs[q2const.CS_MODELS+1] = "maps/demo1.bsp"
	cs[q2const.CS_MODELS+2] = door.model
	ent := shared.EntityState{Number: 60, ModelIndex: 2, Solid: 31}
	ls := &perception.LevelStatic{Gen: 1, MapName: "demo1", Baselines: []shared.EntityState{ent}}
	st := cmodel.NewState(cm)
	var eye Vec3
	found := false
	for _, dir := range []Vec3{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}} {
		for _, r := range []float32{64, 96, 128} {
			e := shared.VectorMA(center, r, dir)
			if st.PointContents(e, 0) != 0 {
				continue
			}
			v := perception.NewVision(cm, perception.ViewOptions{})
			in := staticFrame(cm, ls, &cs, 1, e, center, ent)
			v.Begin(e, in.PlayerState.ViewAngles, 90, in.AreaBits[:in.AreaBytes], []perception.BrushPose{{Num: 60, Inline: n}})
			lo, hi, _ := v.BrushBounds(n, Vec3{}, Vec3{})
			if v.SeesBox(lo, hi, 60) {
				eye, found = e, true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("no viewpoint of %s", door.model)
	}
	w := New(Config{})
	w.Reset(lv)
	w.Update(staticFrame(cm, ls, &cs, 1, eye, center, ent), 100)
	m := w.Belief().Mover(door.model)
	if m == nil || !m.Visible || m.Moved || m.Lump != door.lump {
		t.Fatalf("mover %+v", m)
	}
	ent.Origin = Vec3{0, 0, 60}
	w.Update(staticFrame(cm, ls, &cs, 2, eye, center, ent), 200)
	b := w.Belief()
	if m := b.Mover(door.model); !m.Moved || !m.Moving || m.Origin != ent.Origin {
		t.Fatalf("moved mover %+v", m)
	}
	if e := b.Effect(EffectMoverMoved, door.lump); e == nil || e.Ref != door.model {
		t.Fatalf("effects %+v", b.Effects)
	}
	// out of sight and silent: the pose is kept and goes stale
	for i := int32(3); i < 60; i++ {
		w.Update(staticFrame(cm, ls, &cs, i, eye, shared.VectorMA(eye, -100, shared.VectorSubtract(center, eye))), int64(i)*100)
	}
	if m := w.Belief().Mover(door.model); m.Visible || !m.Stale || m.Origin != ent.Origin {
		t.Fatalf("hidden mover %+v", m)
	}
}
