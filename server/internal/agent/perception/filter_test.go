package perception

import (
	"math"
	"reflect"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

const solidStd = 8290 // (-16 -16 -24) (16 16 32)

// floorInput is a frame on the floor map: the player stands at (0,0,24)
// looking along +x, eye (0,0,46).
func floorInput() FrameInput {
	cs := testCS()
	cs[q2const.CS_SOUNDS+1] = "soldier/solsght1.wav"
	cs[q2const.CS_SOUNDS+2] = "world/amb7.wav"
	cs[q2const.CS_SOUNDS+3] = "doors/dr1_strt.wav"
	var ps shared.PlayerState
	ps.PMove.Origin = [3]int16{0, 0, 24 * 8}
	ps.ViewOffset = Vec3{0, 0, 22}
	ps.Fov = 90
	in := FrameInput{
		Level:       &LevelStatic{Gen: 1, MapName: "floor", PlayerNum: 0},
		CS:          cs,
		ServerFrame: 10, ServerTime: 1000,
		PlayerState: ps,
		AreaBytes:   1,
	}
	in.AreaBits[0] = 0x02
	in.Entities = []shared.EntityState{
		{Number: 1, ModelIndex: 255, Origin: Vec3{0, 0, 24}, Solid: solidStd},
		{Number: 20, ModelIndex: 4, SkinNum: 2, Origin: Vec3{200, 0, 24}, Solid: solidStd, Frame: 146}, // in view
		{Number: 21, ModelIndex: 4, Origin: Vec3{0, 0, -60}, Solid: solidStd},                          // under the slab (and below the view)
		{Number: 22, ModelIndex: 4, Origin: Vec3{-200, 0, 24}, Solid: solidStd},                        // behind
		{Number: 23, ModelIndex: 4, Origin: Vec3{200, 230, 24}, Solid: solidStd},                       // outside the fov
		{Number: 24, ModelIndex: 7, Origin: Vec3{150, 40, 16}},                                         // item in view
		{Number: 25, Sound: 2, Origin: Vec3{-300, 0, 30}},                                              // speaker loop, audible
		{Number: 26, Sound: 2, Origin: Vec3{-600, 0, 30}},                                              // speaker loop, too far
		{Number: 27, ModelIndex: 2, Solid: 31, Sound: 0},                                               // brush *1
	}
	pos := Vec3{4000, 0, 0}
	in.Events.Sounds = []fakeclient.Sound{
		{SoundNum: 1, Ent: 21, Volume: 1, Attenuation: 1},               // the hidden soldier's sight cry: heard
		{SoundNum: 1, Ent: 99, Volume: 1, Attenuation: 1},               // from an entity not in the frame
		{SoundNum: 1, Ent: 22, Volume: 1, Attenuation: 1, Pos: &pos},    // positioned far away: inaudible
		{SoundNum: 3, Ent: 27, Volume: 1, Attenuation: 3, Pos: &Vec3{}}, // the door starts
		{SoundNum: 1, Ent: 1, Volume: 1, Attenuation: 1},                // own noise
	}
	in.Events.MuzzleFlashes = []fakeclient.MuzzleFlash{
		{Ent: 20, Weapon: q2const.MZ2_SOLDIER_SHOTGUN_1, Monster: true},
		{Ent: 1, Weapon: q2const.MZ_BLASTER},
		{Ent: 22, Weapon: q2const.MZ2_SOLDIER_BLASTER_1, Monster: true},
	}
	in.Events.TempEnts = []fakeclient.TempEnt{
		{Type: q2const.TE_ROCKET_EXPLOSION, Pos: Vec3{-300, 0, 30}}, // behind, heard
		{Type: q2const.TE_GUNSHOT, Pos: Vec3{-300, 0, 30}},          // behind, not seen
		{Type: q2const.TE_GUNSHOT, Pos: Vec3{300, 10, 30}},          // in view
		{Type: q2const.TE_EXPLOSION1, Pos: Vec3{-9000, 0, 30}},      // behind and too far to hear
	}
	return in
}

func newFloorPerceiver(t *testing.T) *Perceiver {
	return NewPerceiver(floorMap(t), NewClassifier(NewClassTable()), nil, Options{})
}

func nums[T any](s []T, f func(*T) int32) []int32 {
	var out []int32
	for i := range s {
		out = append(out, f(&s[i]))
	}
	return out
}

func TestObservationFilter(t *testing.T) {
	p := newFloorPerceiver(t)
	in := floorInput()
	pc := p.Perceive(&in)

	if !pc.HasOwn || pc.Own.Number != 1 || pc.OwnFlashes != 1 {
		t.Fatalf("own entity %v/%d, own flashes %d", pc.HasOwn, pc.Own.Number, pc.OwnFlashes)
	}
	if pc.Eye != (Vec3{0, 0, 46}) {
		t.Fatalf("eye %v", pc.Eye)
	}
	seen := nums(pc.Seen, func(s *Sighting) int32 { return s.Num })
	if !reflect.DeepEqual(seen, []int32{20, 24}) {
		t.Fatalf("seen %v, want [20 24] (21 under the slab, 22 behind, 23 outside the fov; "+
			"TestFilterOccludedInFOV covers occlusion inside the fov)", seen)
	}
	s := pc.Sighting(20)
	if s.Class.Name != "soldier" || !s.Shootable || s.Mins != (Vec3{-16, -16, -24}) || s.Dist < 190 || s.Dist > 210 {
		t.Fatalf("soldier sighting %+v", s)
	}
	if pc.Sighting(21) != nil {
		t.Fatal("occluded soldier in the percept")
	}
	if it := pc.Sighting(24); it.Class.Name != "item_health_small" || it.Mins != (Vec3{-15, -15, -15}) {
		t.Fatalf("item sighting %+v", it)
	}

	// sounds: the hidden soldier's cry gives a cue, not its origin; the
	// unseen entity's cry has no cue; the far one and the own one are
	// dropped; the door sound carries the door pose; the near speaker loop
	// is heard
	type hk struct {
		num    int32
		placed bool
		seen   bool
		cue    Cue
		mover  bool
		loop   bool
	}
	var heard []hk
	for _, h := range pc.Heard {
		heard = append(heard, hk{h.Num, h.Placed, h.Seen, h.Cue, h.Mover != nil, h.Loop})
	}
	below := Cue{PanCenter, LoudNear}
	want := []hk{{21, true, false, below, false, false}, {99, false, false, Cue{}, false, false},
		{27, true, false, below, true, false}, {25, true, false, Cue{PanCenter, LoudFar}, false, true}}
	if !reflect.DeepEqual(heard, want) {
		t.Fatalf("heard %+v\nwant  %+v", heard, want)
	}
	if h := pc.Heard[0]; h.Kind != SoundSight || h.Family != "soldier" {
		t.Fatalf("sight cry %+v", h)
	}
	// flashes: the visible soldier's (seen) and the one behind (heard: a
	// cue only)
	fl := nums(pc.Flashes, func(f *Flash) int32 { return f.Num })
	if !reflect.DeepEqual(fl, []int32{20, 22}) || pc.Flashes[0].Weapon != WeaponShotgun || !pc.Flashes[0].Seen ||
		!pc.Flashes[1].Placed || pc.Flashes[1].Seen || pc.Flashes[1].Cue != (Cue{PanCenter, LoudNear}) {
		t.Fatalf("flashes %+v", pc.Flashes)
	}
	// temp entities: the explosion behind is heard (its type and cue, no
	// position), the gunshot in view seen
	if len(pc.TempEnts) != 2 || !pc.TempEnts[0].Heard || pc.TempEnts[0].Seen ||
		pc.TempEnts[0].TempEnt != (fakeclient.TempEnt{Type: q2const.TE_ROCKET_EXPLOSION}) ||
		pc.TempEnts[0].Cue != (Cue{PanCenter, LoudNear}) ||
		pc.TempEnts[1].Type != q2const.TE_GUNSHOT || !pc.TempEnts[1].Seen || pc.TempEnts[1].Pos != (Vec3{300, 10, 30}) {
		t.Fatalf("temp ents %+v", pc.TempEnts)
	}
	// admitted: seen 20, 24; the door 27 by its sound. The cry of 21, the
	// speaker loop 25 and the flash of 22 admit only their cues
	if got := pc.Admitted(); !reflect.DeepEqual(got, []int32{20, 24, 27}) {
		t.Fatalf("admitted %v", got)
	}
}

// TestHearingCues: the cue of a sound is the mixer's stereo balance and
// loudness steps; a source ahead and its mirror image behind sound alike,
// and so do two sources at the same balance and loudness anywhere.
func TestHearingCues(t *testing.T) {
	p := newFloorPerceiver(t)
	at := func(pos Vec3, atten float32) Cue {
		in := floorInput()
		in.Entities = []shared.EntityState{{Number: 1, ModelIndex: 255, Origin: Vec3{0, 0, 24}, Solid: solidStd},
			{Number: 30, ModelIndex: 4, Origin: pos, Solid: solidStd}}
		in.Events = Events{Sounds: []fakeclient.Sound{{SoundNum: 1, Ent: 30, Volume: 1, Attenuation: atten}}}
		pc := p.Perceive(&in)
		if len(pc.Heard) != 1 {
			return Cue{}
		}
		return pc.Heard[0].Cue
	}
	eye := Vec3{0, 0, 46}
	polar := func(deg, dist float32) Vec3 {
		s, c := math.Sincos(float64(deg) * math.Pi / 180)
		return Vec3{eye[0] + dist*float32(c), eye[1] + dist*float32(s), eye[2]}
	}
	for _, tc := range []struct {
		deg, dist float32
		atten     float32
		want      Cue
	}{
		{0, 300, q2const.ATTN_NORM, Cue{PanCenter, LoudNear}},
		{180, 300, q2const.ATTN_NORM, Cue{PanCenter, LoudNear}},
		{90, 300, q2const.ATTN_NORM, Cue{PanHardLeft, LoudNear}},
		{45, 300, q2const.ATTN_NORM, Cue{PanLeft, LoudNear}},
		{135, 300, q2const.ATTN_NORM, Cue{PanLeft, LoudNear}},
		{-60, 900, q2const.ATTN_NORM, Cue{PanRight, LoudMid}},
		{-90, 1500, q2const.ATTN_NORM, Cue{PanHardRight, LoudFar}},
		{-90, 500, q2const.ATTN_IDLE, Cue{PanHardRight, LoudMid}},
		{30, 300, q2const.ATTN_NONE, Cue{}}, // not spatialized: no direction, no distance
	} {
		if got := at(polar(tc.deg, tc.dist), tc.atten); got != tc.want {
			t.Errorf("%v° at %v (atten %v): cue %+v, want %+v", tc.deg, tc.dist, tc.atten, got, tc.want)
		}
	}
	// every distance the loudness steps give for a sound lands in its step
	for _, atten := range []float32{q2const.ATTN_NORM, q2const.ATTN_IDLE, q2const.ATTN_STATIC} {
		for _, l := range []Loudness{LoudNear, LoudMid, LoudFar} {
			lo, mid, hi, ok := LoudnessRange(l, atten, false)
			if !ok || !(lo <= mid && mid <= hi) {
				t.Fatalf("range of %v at %v: %v %v %v %v", l, atten, lo, mid, hi, ok)
			}
			if got := at(polar(-90, mid), atten); got.Loud != l {
				t.Errorf("atten %v: the middle of %v (%v units) plays %v", atten, l, mid, got.Loud)
			}
		}
	}
}

// TestFilterIgnoresHiddenState: what is neither seen nor heard can change
// arbitrarily without changing the percept.
func TestFilterIgnoresHiddenState(t *testing.T) {
	a := floorInput()
	b := floorInput()
	b.Entities = append([]shared.EntityState(nil), b.Entities...)
	for i := range b.Entities {
		switch b.Entities[i].Number {
		case 23: // outside the fov
			b.Entities[i].Origin = Vec3{150, 300, 10}
			b.Entities[i].Frame, b.Entities[i].SkinNum = 99, 5
		case 26: // inaudible speaker
			b.Entities[i].Origin = Vec3{-700, 50, 0}
		}
	}
	b.Events.Sounds = append(b.Events.Sounds[:0:0], b.Events.Sounds...)
	pa := newFloorPerceiver(t).Perceive(&a)
	pb := newFloorPerceiver(t).Perceive(&b)
	if !reflect.DeepEqual(pa.Seen, pb.Seen) || !reflect.DeepEqual(pa.Heard, pb.Heard) ||
		!reflect.DeepEqual(pa.Flashes, pb.Flashes) || !reflect.DeepEqual(pa.TempEnts, pb.TempEnts) {
		t.Fatal("hidden entities changed the percept")
	}
}

// TestFilterFOVEdge: an entity enters the percept exactly when its box
// center crosses the frustum edge (boxes under 64 units sample only the
// center, top and bottom).
func TestFilterFOVEdge(t *testing.T) {
	p := newFloorPerceiver(t)
	for _, tc := range []struct {
		y    float32
		seen bool
	}{{170, true}, {200 - 1, true}, {200 + 1, false}, {260, false}} {
		in := floorInput()
		in.Entities = []shared.EntityState{{Number: 30, ModelIndex: 4, Origin: Vec3{200, tc.y, 46 - 4}, Solid: solidStd}}
		in.Events = Events{}
		pc := p.Perceive(&in)
		if got := len(pc.Seen) == 1; got != tc.seen {
			t.Errorf("y=%v: seen %v, want %v", tc.y, got, tc.seen)
		}
	}
}

// TestFilterOccludedInFOV: a monster inside the view frustum and in the
// PVS, hidden by the slab, is not admitted; without the slab (a map-less
// open vision) the same frame shows it.
func TestFilterOccludedInFOV(t *testing.T) {
	in := floorInput()
	in.Events = Events{}
	in.PlayerState.ViewAngles[q2const.PITCH] = 30 // looking down past the slab edge
	in.Entities = []shared.EntityState{{Number: 30, ModelIndex: 4, Origin: Vec3{100, 0, -100}, Solid: solidStd}}
	p := newFloorPerceiver(t)
	pc := p.Perceive(&in)
	v := pc.Vision()
	lo, hi := Vec3{84, -16, -124}, Vec3{116, 16, -68}
	if !v.InFOV(Vec3{100, 0, -96}) || !v.InPVS(lo, hi) {
		t.Fatal("the soldier must be in the fov and the PVS")
	}
	if pc.Sighting(30) != nil || len(pc.Admitted()) != 0 {
		t.Fatalf("occluded soldier admitted: %v", pc.Admitted())
	}
	open := NewPerceiver(nil, NewClassifier(NewClassTable()), nil, Options{View: ViewOptions{NoOcclusion: true}})
	if open.Perceive(&in).Sighting(30) == nil {
		t.Fatal("without the slab it is visible")
	}
}
