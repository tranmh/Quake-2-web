package mapdata_test

import (
	"fmt"
	"strconv"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/game"
	"quake2web/server/internal/game/gametest"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

// readMap returns the bytes of maps/<name>.bsp from the demo pak, skipping
// the test when the pak is absent.
func readMap(t testing.TB, name string) []byte {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func loadMap(t testing.TB, name string, opt mapdata.Options) *mapdata.Map {
	t.Helper()
	m, err := mapdata.Load(name, readMap(t, name), opt)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// spawnGame spawns the map in the Go game module like the oracle driver
// does (SpawnEntities plus two settle frames).
func spawnGame(t *testing.T, name string, opt mapdata.Options) *gametest.Server {
	t.Helper()
	testutil.DemoPak(t)
	sc := &gametest.Scenario{Map: name, Cvars: []gametest.KV{{Key: "skill", Value: strconv.Itoa(opt.Skill)}}}
	if opt.Deathmatch {
		sc.Cvars = append(sc.Cvars, gametest.KV{Key: "deathmatch", Value: "1"})
	}
	if opt.Coop {
		sc.Cvars = append(sc.Cvars, gametest.KV{Key: "coop", Value: "1"})
	}
	s, err := gametest.NewServer(sc, testutil.BaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SpawnServer(name); err != nil {
		t.Fatal(err)
	}
	return s
}

// helperClass lists the classnames of the edicts the game creates itself
// (body queue, door/plat/teleporter triggers, delayed uses, player trail).
func helperClass(c string) bool {
	switch c {
	case "bodyque", "noclass", "DelayedUse", "player_trail":
		return true
	}
	return false
}

// gameClassname is the classname the spawn function leaves on the edict.
func gameClassname(c string) string {
	switch c {
	case "func_water", "func_door_secret":
		return "func_door"
	}
	return c
}

// matchEdicts pairs every present lump entity with its edict. Lump order is
// preserved in edict order (an inhibited or freed entity gives its slot to
// the next one), so walking the in-use edicts and skipping the game's own
// helper edicts must meet the present entities one by one; any surplus or
// missing edict means the inhibit/free model disagrees with the game.
func matchEdicts(t *testing.T, m *mapdata.Map, edicts []game.Edict) map[int]*game.Edict {
	t.Helper()
	var want []*mapdata.Entity
	for i := 1; i < len(m.Entities); i++ {
		if e := &m.Entities[i]; e.Present() {
			want = append(want, e)
		}
	}
	slot := map[int]*game.Edict{0: &edicts[0]}
	k := 0
	for n := 1; n < len(edicts); n++ {
		ed := &edicts[n]
		if !ed.InUse || helperClass(ed.Classname) {
			continue
		}
		if k >= len(want) {
			t.Fatalf("edict %d (%s %s %q) has no present lump entity left", n, ed.Classname, ed.Model, ed.Targetname)
		}
		e := want[k]
		// (health items set their own model; brush models never change)
		if ed.Classname != gameClassname(e.Classname) || (e.Brush() && ed.Model != e.Model) || ed.Targetname != e.Targetname {
			t.Fatalf("edict %d is %s %s %q, want lump %v (inhibit/free mismatch before it)",
				n, ed.Classname, ed.Model, ed.Targetname, e)
		}
		slot[e.Index] = ed
		k++
	}
	if k != len(want) {
		t.Fatalf("%d present lump entities have no edict, first %v", len(want)-k, want[k])
	}
	return slot
}

func vecEq(t *testing.T, what string, got, want mapdata.Vec3) {
	t.Helper()
	if !testutil.SameVec3(got, want) {
		t.Errorf("%s = %s, game %s", what, testutil.FmtVec3(got), testutil.FmtVec3(want))
	}
}

func f32Eq(t *testing.T, what string, got, want float32) {
	t.Helper()
	if !testutil.SameF32(got, want) {
		t.Errorf("%s = %s, game %s", what, testutil.FmtF32(got), testutil.FmtF32(want))
	}
}

// TestCrossCheckGametest compares the derived movers, triggers, exits,
// lasers and the inhibited set with a real spawn of the demo maps.
func TestCrossCheckGametest(t *testing.T) {
	cases := []struct {
		name string
		opt  mapdata.Options
	}{
		{"demo1", mapdata.Options{Skill: 1}}, {"demo2", mapdata.Options{Skill: 1}}, {"demo3", mapdata.Options{Skill: 1}},
		{"demo1", mapdata.Options{Skill: 0}}, {"demo2", mapdata.Options{Skill: 0}}, {"demo3", mapdata.Options{Skill: 0}},
		{"demo1", mapdata.Options{Skill: 2}}, {"demo2", mapdata.Options{Skill: 2}}, {"demo3", mapdata.Options{Skill: 3}},
		{"demo1", mapdata.Options{Skill: 1, Coop: true}},
		{"demo2", mapdata.Options{Skill: 1, Deathmatch: true}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%+v", tc.name, tc.opt), func(t *testing.T) {
			m := loadMap(t, tc.name, tc.opt)
			s := spawnGame(t, tc.name, tc.opt)
			if got, want := strconv.Itoa(int(int32(m.Checksum))), s.E.Configstrings[q2const.CS_MAPCHECKSUM]; got != want {
				t.Errorf("checksum %s, CS_MAPCHECKSUM %s", got, want)
			}
			edicts := s.E.Ge.Edicts()[:s.E.Ge.NumEdicts()]
			slot := matchEdicts(t, m, edicts)
			checkMovers(t, m, slot, edicts)
			checkTriggers(t, m, slot)
			for _, x := range m.Exits {
				if ed := slot[x.Entity]; ed.Map != x.Level.Raw {
					t.Errorf("exit #%d map %q, game %q", x.Entity, x.Level.Raw, ed.Map)
				}
			}

			// target_laser_start runs one second in; run past it
			for f := 1; f <= 10; f++ {
				if err := s.Frame(f); err != nil {
					t.Fatal(err)
				}
				s.EndFrame()
			}
			checkLasers(t, m, slot)
		})
	}
}

func checkMovers(t *testing.T, m *mapdata.Map, slot map[int]*game.Edict, edicts []game.Edict) {
	t.Helper()
	doorTriggers, platTriggers := map[*game.Edict]*game.Edict{}, map[*game.Edict]*game.Edict{}
	for n := range edicts {
		ed := &edicts[n]
		if !ed.InUse || ed.Touch == nil {
			continue
		}
		switch ed.Touch.Name() {
		case "Touch_DoorTrigger":
			doorTriggers[ed.Owner] = ed
		case "Touch_Plat_Center":
			platTriggers[ed.Enemy] = ed
		}
	}
	nTriggers := 0
	for k := range m.Movers {
		mv := &m.Movers[k]
		ed := slot[mv.Entity]
		what := func(f string) string { return fmt.Sprintf("%s #%d %s %s", mv.Classname, mv.Entity, mv.Model, f) }
		vecEq(t, what("pos1"), mv.Pos1, ed.Pos1)
		vecEq(t, what("pos2"), mv.Pos2, ed.Pos2)
		vecEq(t, what("movedir"), mv.Movedir, ed.Movedir)
		vecEq(t, what("mins"), mv.Mins, ed.Mins)
		vecEq(t, what("maxs"), mv.Maxs, ed.Maxs)
		vecEq(t, what("origin"), mv.Origin, ed.S.Origin)
		if mv.Kind != mapdata.MoverRotating {
			vecEq(t, what("angles"), mv.Angles, ed.S.Angles)
			vecEq(t, what("absmin"), mv.Box.Min, ed.AbsMin)
			vecEq(t, what("absmax"), mv.Box.Max, ed.AbsMax)
		}
		if got := ed.Solid == q2const.SOLID_BSP; got != mv.Solid {
			t.Errorf("%s = %v, game solid %d", what("solid"), mv.Solid, ed.Solid)
		}
		switch mv.Kind {
		case mapdata.MoverDoor, mapdata.MoverDoorRotating, mapdata.MoverButton, mapdata.MoverPlat,
			mapdata.MoverTrain, mapdata.MoverDoorSecret:
			f32Eq(t, what("speed"), mv.Speed, ed.Moveinfo.Speed)
			f32Eq(t, what("accel"), mv.Accel, ed.Moveinfo.Accel)
			f32Eq(t, what("decel"), mv.Decel, ed.Moveinfo.Decel)
		}
		switch mv.Kind {
		case mapdata.MoverDoor, mapdata.MoverDoorRotating:
			f32Eq(t, what("distance"), mv.Distance, ed.Moveinfo.Distance)
			f32Eq(t, what("wait"), mv.Wait, ed.Moveinfo.Wait)
			if ed.Teammaster != nil && ed.Teammaster != slot[mv.TeamMaster] {
				t.Errorf("%s = #%d, game edict %d", what("teammaster"), mv.TeamMaster, ed.Teammaster.Index)
			}
		case mapdata.MoverButton, mapdata.MoverPlat:
			f32Eq(t, what("wait"), mv.Wait, ed.Moveinfo.Wait)
		case mapdata.MoverDoorSecret:
			f32Eq(t, what("wait"), mv.Wait, ed.Wait)
		}
		if int32(mv.Spawnflags) != ed.Spawnflags && mv.Kind != mapdata.MoverTrain && mv.Kind != mapdata.MoverRotating {
			t.Errorf("%s = %d, game %d", what("spawnflags"), mv.Spawnflags, ed.Spawnflags)
		}

		var trig *game.Edict
		switch mv.Kind {
		case mapdata.MoverPlat:
			trig = platTriggers[ed]
		default:
			trig = doorTriggers[ed]
		}
		switch {
		case mv.Trigger == nil && trig != nil:
			t.Errorf("%s: game spawned a trigger %s-%s", what("trigger"),
				testutil.FmtVec3(trig.AbsMin), testutil.FmtVec3(trig.AbsMax))
		case mv.Trigger != nil && trig == nil:
			t.Errorf("%s: no trigger in game", what("trigger"))
		case mv.Trigger != nil:
			nTriggers++
			vecEq(t, what("trigger absmin"), mv.Trigger.Min, trig.AbsMin)
			vecEq(t, what("trigger absmax"), mv.Trigger.Max, trig.AbsMax)
		}
	}
	if nTriggers != len(doorTriggers)+len(platTriggers) {
		t.Errorf("%d mover triggers, game has %d door + %d plat triggers", nTriggers, len(doorTriggers), len(platTriggers))
	}
}

func checkTriggers(t *testing.T, m *mapdata.Map, slot map[int]*game.Edict) {
	t.Helper()
	for k := range m.Triggers {
		tr := &m.Triggers[k]
		ed := slot[tr.Entity]
		what := func(f string) string { return fmt.Sprintf("%s #%d %s %s", tr.Classname, tr.Entity, tr.Model, f) }
		if ed.Spawnflags != tr.Spawnflags {
			t.Errorf("%s = %d, game %d", what("spawnflags"), tr.Spawnflags, ed.Spawnflags)
		}
		f32Eq(t, what("wait"), tr.Wait, ed.Wait)
		f32Eq(t, what("delay"), tr.Delay, ed.Delay)
		if !tr.HasVolume {
			continue
		}
		vecEq(t, what("absmin"), tr.Box.Min, ed.AbsMin)
		vecEq(t, what("absmax"), tr.Box.Max, ed.AbsMax)
		vecEq(t, what("movedir"), tr.Movedir, ed.Movedir)
		switch tr.Classname {
		case "trigger_multiple", "trigger_once", "trigger_hurt":
			if got := ed.Solid == q2const.SOLID_TRIGGER; got != tr.StartsEnabled {
				t.Errorf("%s = %v, game solid %d", what("starts enabled"), tr.StartsEnabled, ed.Solid)
			}
		}
	}
}

func checkLasers(t *testing.T, m *mapdata.Map, slot map[int]*game.Edict) {
	t.Helper()
	for _, l := range m.Lasers {
		ed := slot[l.Entity]
		what := fmt.Sprintf("target_laser #%d", l.Entity)
		vecEq(t, what+" movedir", l.Movedir, ed.Movedir)
		if on := ed.Spawnflags&1 != 0; on != l.StartOn {
			t.Errorf("%s on = %v, game spawnflags %#x", what, l.StartOn, ed.Spawnflags)
		}
		if l.StartOn {
			vecEq(t, what+" end", l.End, ed.S.OldOrigin) // target_laser_think: old_origin = tr.endpos
		}
	}
}
