package worldmodel

import (
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Model, sound and item indexes of the synthetic level.
const (
	mSoldier = 3
	mGibHead = 4
	mStim    = 5
	mRocket  = 6
	mBolt    = 7

	sDeath = 1
	sSight = 2
	sGib   = 3
	sDoor  = 4
	sLoop  = 5 // set by the tests that use it

	solidStd = 8290 // (-16 -16 -24) (16 16 32)
)

// soldier frames of the synthetic model: 0-3 stand, 4-5 attack, 6-8 death
var testSoldierFrames = []string{"stand101", "stand102", "stand103", "stand104", "attak101", "attak102",
	"death101", "death102", "death103"}

func floorCM(t testing.TB) *cmodel.Map {
	t.Helper()
	cm, err := cmodel.LoadMapBytes("maps/floor.bsp", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

// sim drives a World with hand-made frames on the synthetic floor map
// (a 128x128 slab at z -16..0 in empty space). The player stands at
// (0,0,24), eye (0,0,46), looking along +x.
type sim struct {
	t      *testing.T
	w      *World
	cs     *perception.ConfigStrings
	level  *perception.LevelStatic
	ps     shared.PlayerState
	ents   []shared.EntityState
	ev     perception.Events
	inv    [q2const.MAX_ITEMS]int32
	invSeq uint64
	// ownEvent is the own entity's event in the next frame
	ownEvent int32
	frame    int32
	now      int64
}

func newSim(t *testing.T) *sim {
	var cs perception.ConfigStrings
	for i, m := range []string{"", "maps/floor.bsp", "*1", "models/monsters/soldier/tris.md2",
		"models/objects/gibs/head2/tris.md2", "models/items/healing/stimpack/tris.md2",
		"models/objects/rocket/tris.md2", "models/objects/laser/tris.md2", "models/weapons/v_blast/tris.md2"} {
		cs[q2const.CS_MODELS+i] = m
	}
	for i, s := range []string{"", "soldier/soldeth1.wav", "soldier/solsght1.wav", "misc/udeath.wav", "doors/dr1_strt.wav"} {
		cs[q2const.CS_SOUNDS+i] = s
	}
	for i, n := range []string{"", "Health", "Blaster", "Shells"} {
		cs[q2const.CS_ITEMS+i] = n
	}
	for i, n := range []string{"", "i_help", "i_health"} {
		cs[q2const.CS_IMAGES+i] = n
	}
	anims := perception.NewAnimCache(nil)
	anims.Set("models/monsters/soldier/tris.md2", perception.NewModelAnims(testSoldierFrames))
	s := &sim{t: t, cs: &cs, level: &perception.LevelStatic{Gen: 1, MapName: "floor"}, frame: 0, now: 0}
	s.w = New(Config{Anims: anims})
	s.w.Reset(Level{Key: LevelKey{Map: "floor"}, CM: floorCM(t)})
	s.ps.PMove.Origin = [3]int16{0, 0, 24 * 8}
	s.ps.PMove.PmFlags = q2const.PMF_ON_GROUND
	s.ps.ViewOffset = Vec3{0, 0, 22}
	s.ps.Fov = 90
	s.ps.GunIndex = 8
	s.ps.Stats[q2const.STAT_HEALTH] = 100
	return s
}

func (s *sim) setOrigin(o Vec3) {
	for i := 0; i < 3; i++ {
		s.ps.PMove.Origin[i] = int16(o[i] * 8)
	}
}

func soldierAt(num int32, o Vec3) shared.EntityState {
	return shared.EntityState{Number: num, ModelIndex: mSoldier, Origin: o, Solid: solidStd}
}

// input is the frame step sends next (frame number and clock not yet
// advanced).
func (s *sim) input() perception.FrameInput {
	own := shared.EntityState{Number: 1, ModelIndex: 255, Solid: solidStd, Event: s.ownEvent,
		Origin: Vec3{float32(s.ps.PMove.Origin[0]) / 8, float32(s.ps.PMove.Origin[1]) / 8, float32(s.ps.PMove.Origin[2]) / 8}}
	in := perception.FrameInput{
		Level: s.level, CS: s.cs, ServerFrame: s.frame + 1, ServerTime: (s.frame + 1) * 100,
		PlayerState: s.ps, AreaBytes: 1, Entities: append([]shared.EntityState{own}, s.ents...),
		Events: s.ev, Inventory: s.inv, InventorySeq: s.invSeq,
	}
	in.AreaBits[0] = 0x02
	return in
}

// step sends one frame with the current entities and events (then clears
// the events) and advances 100 ms.
func (s *sim) step() *Belief {
	in := s.input()
	s.frame++
	s.now += 100
	s.ownEvent = 0
	s.w.Update(in, s.now)
	s.ev = perception.Events{}
	s.ps.Stats[q2const.STAT_FLASHES] = 0
	return s.w.Belief()
}

func (s *sim) sound(num, ent int32) {
	s.ev.Sounds = append(s.ev.Sounds, fakeclient.Sound{SoundNum: num, Ent: ent, Volume: 1, Attenuation: 1})
}

func (s *sim) track(id string) *Track {
	s.t.Helper()
	tr := s.w.Belief().Track(id)
	if tr == nil {
		s.t.Fatalf("no track %s in %+v", id, s.w.Belief().Tracks)
	}
	return tr
}
