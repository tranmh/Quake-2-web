package gametest

import (
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/game"
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// fakeGame is a minimal game module used to test the engine side alone.
type fakeGame struct {
	gi     game.Import
	rng    *crand.Rand
	edicts []game.Edict
	num    int
	max    int

	spawn func(f *fakeGame)
	frame func(f *fakeGame)
	cmds  []string
}

func (f *fakeGame) Init() {
	f.max = 1024
	f.edicts = make([]game.Edict, f.max)
	for i := range f.edicts {
		f.edicts[i].Index = i
	}
	f.num = 1 + int(f.gi.Cvar("maxclients", "4", 0).Value)
}
func (f *fakeGame) Shutdown() {}
func (f *fakeGame) SpawnEntities(mapname, entstring, spawnpoint string) {
	f.edicts[0].InUse = true
	if f.spawn != nil {
		f.spawn(f)
	}
}
func (f *fakeGame) WriteGame(bool) ([]byte, error) { return nil, nil }
func (f *fakeGame) ReadGame([]byte) error          { return nil }
func (f *fakeGame) WriteLevel() ([]byte, error)    { return nil, nil }
func (f *fakeGame) ReadLevel([]byte) error         { return nil }
func (f *fakeGame) ClientConnect(ent *game.Edict, userinfo string) (bool, string) {
	return true, userinfo
}
func (f *fakeGame) ClientBegin(ent *game.Edict)                            { ent.InUse = true }
func (f *fakeGame) ClientUserinfoChanged(ent *game.Edict, userinfo string) {}
func (f *fakeGame) ClientDisconnect(ent *game.Edict)                       {}
func (f *fakeGame) ClientCommand(ent *game.Edict)                          { f.cmds = append(f.cmds, f.gi.Args()) }
func (f *fakeGame) ClientThink(ent *game.Edict, cmd *shared.UserCmd)       {}
func (f *fakeGame) RunFrame() {
	if f.frame != nil {
		f.frame(f)
	}
}
func (f *fakeGame) ServerCommand()       {}
func (f *fakeGame) Edicts() []game.Edict { return f.edicts }
func (f *fakeGame) NumEdicts() int       { return f.num }
func (f *fakeGame) MaxEdicts() int       { return f.max }
func (f *fakeGame) spawnEdict() *game.Edict {
	e := &f.edicts[f.num]
	f.num++
	e.InUse = true
	return e
}

func needPak(t *testing.T) string {
	base := testutil.BaseDir()
	if _, err := os.Stat(filepath.Join(base, "baseq2", "pak0.pak")); err != nil {
		t.Skip("demo pak not available")
	}
	return base
}

func TestEngineSpawnAndIndexes(t *testing.T) {
	base := needPak(t)
	sc, err := ParseScenario([]byte(`{"map":"demo1","seed":3,"cvars":{"deathmatch":"1","maxclients":"2"},"clients":[{"userinfo":"\\name\\a"}],"frames":2,"schedule":[{"frame":1,"client":0,"command":"say hi $maxclients"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	fg := &fakeGame{}
	var brush, box *game.Edict
	var firstModel, snd1, snd2 int
	fg.spawn = func(f *fakeGame) {
		gi := f.gi
		firstModel = gi.ModelIndex("models/test/tris.md2")
		snd1 = gi.SoundIndex("misc/a.wav")
		snd2 = gi.SoundIndex("misc/a.wav")
		gi.Configstring(CS_SKY, "unit1_") // loading: no multicast
		brush = f.spawnEdict()
		brush.Solid = SOLID_BSP
		gi.SetModel(brush, "*1")
		box = f.spawnEdict()
		box.Solid = SOLID_BBOX
		box.Mins = Vec3{-16, -16, -24}
		box.Maxs = Vec3{16, 16, 32}
		box.S.Origin = Vec3{-552, 232, -14}
		gi.LinkEntity(box)
	}
	frames := 0
	fg.frame = func(f *fakeGame) {
		frames++
		f.rng.Rand()
		f.rng.Rand()
	}
	s, err := NewServer(sc, base, func(gi game.Import, rng *crand.Rand) game.Export {
		fg.gi, fg.rng = gi, rng
		return fg
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	e := s.E
	nin := e.Map.NumInlineModels()
	if firstModel != nin+1 {
		t.Errorf("first model index %d, want %d", firstModel, nin+1)
	}
	if snd1 != 1 || snd2 != 1 {
		t.Errorf("sound indexes %d %d", snd1, snd2)
	}
	if e.Configstrings[CS_MODELS+1] != "maps/demo1.bsp" || e.Configstrings[CS_MODELS+2] != "*1" {
		t.Errorf("model configstrings %q %q", e.Configstrings[CS_MODELS+1], e.Configstrings[CS_MODELS+2])
	}
	if frames != 2 || s.RandCalls() != 4 {
		t.Errorf("frames %d rand calls %d", frames, s.RandCalls())
	}
	if brush.Area.Prev == nil || brush.Mins != e.Map.InlineModel("*1").Mins || brush.S.ModelIndex != 2 {
		t.Errorf("brush not set up: %+v %v", brush.Mins, brush.S.ModelIndex)
	}
	if box.AbsMin != (Vec3{-552 - 17, 232 - 17, -14 - 25}) || box.LinkCount != 1 {
		t.Errorf("box abs %v linkcount %d", box.AbsMin, box.LinkCount)
	}
	if box.S.Solid == 0 {
		t.Errorf("box s.solid not packed")
	}
	// baseline: s.number set for edicts with a model
	if brush.S.Number != int32(brush.Index) {
		t.Errorf("baseline number %d", brush.S.Number)
	}
	// events: the configstring during loading is recorded
	found := false
	for _, ev := range e.Events {
		if ev["t"] == "configstring" && ev["index"] == int64(CS_SKY) {
			found = true
		}
	}
	if !found {
		t.Errorf("configstring event missing: %v", e.Events)
	}

	// after spawn: new index writes a configstring multicast and clears it
	s.beginPeriod()
	if i := e.SoundIndex("misc/b.wav"); i != 2 || e.MC.CurSize != 0 {
		t.Errorf("post-spawn index %d cursize %d", i, e.MC.CurSize)
	}
	// written bytes + multicast are recorded
	e.WriteByteC(svcTempEntity)
	e.WriteByteC(7)
	e.Multicast(&box.S.Origin, MULTICAST_PVS)
	ev := e.Events[len(e.Events)-1]
	if ev["t"] != "multicast" || ev["bytes"] != "0307" || e.MC.CurSize != 0 {
		t.Errorf("multicast event %v", ev)
	}
	// sound: bytes go to the multicast buffer and are cleared
	e.Sound(box, CHAN_VOICE, 1, 1, ATTN_NORM, 0)
	if e.MC.CurSize != 0 || e.Events[len(e.Events)-1]["sound"] != "misc/a.wav" {
		t.Errorf("sound event %v", e.Events[len(e.Events)-1])
	}
	// unicast to a non-client does not clear the buffer
	e.WriteByteC(1)
	e.Unicast(brush, true)
	if e.MC.CurSize != 1 {
		t.Errorf("unicast to non-client cleared the buffer")
	}
	e.Unicast(&fg.edicts[1], false)
	if e.MC.CurSize != 0 {
		t.Errorf("unicast did not clear")
	}

	// traces: straight down from above the box hits it; the world blocks far below
	start := Vec3{-552, 232, 100}
	end := Vec3{-552, 232, -1000}
	tr := e.Trace(&start, nil, nil, &end, nil, MASK_SHOT)
	if tr.Ent != box || tr.Fraction >= 1 {
		t.Errorf("trace hit %v fraction %v", tr.Ent != nil, tr.Fraction)
	}
	tr = e.Trace(&start, nil, nil, &end, box, MASK_SHOT)
	if tr.Ent != &fg.edicts[0] || tr.Fraction >= 1 {
		t.Errorf("trace (pass box) hit %v fraction %v", tr.Ent != nil, tr.Fraction)
	}
	list := make([]*game.Edict, 16)
	if n := e.BoxEdicts(&box.AbsMin, &box.AbsMax, list, AREA_SOLID); n < 1 {
		t.Errorf("BoxEdicts found %d", n)
	}

	// client connected and began; the scheduled command expands macros
	if e.clients[0].state != cs_spawned {
		t.Errorf("client state %d", e.clients[0].state)
	}
	if err := s.Frame(1); err != nil {
		t.Fatal(err)
	}
	if len(fg.cmds) != 1 || fg.cmds[0] != "hi 2" {
		t.Errorf("client commands %q", fg.cmds)
	}
	if len(s.Inputs) != 1 {
		t.Errorf("inputs %v", s.Inputs)
	}
}

const svcTempEntity = 3

func TestRandomWalkMatchesC(t *testing.T) {
	// first command of a walk with seed 1000 (computed by hand from the
	// xorshift32 definition of docs/FIXTURES.md).
	w := &rwalk{}
	w.r.seed(0)
	if w.r.s != 0x9E3779B9 {
		t.Fatalf("seed 0 -> %#x", w.r.s)
	}
	w.r.seed(1)
	if x := w.r.next(); x != 270369 {
		t.Fatalf("xorshift32(1) = %d", x)
	}
}
