package mapdata_test

import (
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/bsp"
)

type box = [2]mapdata.Vec3

// synth builds a map from the synthetic floor BSP with extra inline models
// "*1", "*2", ... (sharing the world's collision tree) and the given entity
// string. The boxes are the bounds the game sees: CMod_LoadSubmodels spreads
// the file bounds by 1, so the file gets them shrunk by 1.
func synth(t *testing.T, opt mapdata.Options, ents string, models ...box) *mapdata.Map {
	t.Helper()
	m, err := synthErr(opt, ents, models...)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func synthErr(opt mapdata.Options, ents string, models ...box) (*mapdata.Map, error) {
	f := bsp.SyntheticFloorMap()
	for _, b := range models {
		var d bsp.DModel
		for k := 0; k < 3; k++ {
			d.Mins[k], d.Maxs[k] = b[0][k]+1, b[1][k]-1
		}
		f.Models = append(f.Models, d)
	}
	f.Entities = append([]byte(ents), 0)
	return mapdata.Load("synth", bsp.Encode(f), opt)
}

// ents joins entity bodies ("k" "v" pairs) into an entity string, with the
// worldspawn first.
func ents(bodies ...string) string {
	var b strings.Builder
	b.WriteString("{\n\"classname\" \"worldspawn\"\n\"message\" \"Synthetic\"\n\"nextmap\" \"next\"\n}\n")
	for _, body := range bodies {
		b.WriteString("{\n" + body + "\n}\n")
	}
	return b.String()
}

func TestParseFields(t *testing.T) {
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		// key case, a later "angle" overriding "angles", escapes, partial
		// vectors, '_' comment keys and an unknown key
		`"ClassName" "target_help" "MESSAGE" "line1\nline2 \q" "angles" "10 20 30" "angle" "90" "_color" "1 0 0" "foo" "bar"`,
		`"classname" "info_notnull" "origin" "1.5 -2" "targetname" "Spot"`,
		`"classname" "info_notnull" "origin" "8 x 9" "angle" "45" "angles" "1 2 3"`,
		`"classname" "func_timer" "wait" "2.5" "delay" ".25" "random" "1" "spawnflags" "0x10" "count" "7junk"`,
	))
	if m.Message != "Synthetic" || m.NextMap != "next" {
		t.Errorf("worldspawn message/nextmap = %q/%q", m.Message, m.NextMap)
	}
	help := m.Entity(1)
	if help.Classname != "target_help" || help.Message != "line1\nline2 \\" {
		t.Errorf("help classname/message = %q/%q", help.Classname, help.Message)
	}
	if help.Angles != (mapdata.Vec3{0, 90, 0}) {
		t.Errorf("later angle key: angles %v, want (0 90 0)", help.Angles)
	}
	if help.Keys["_color"] != "1 0 0" || help.Keys["message"] != `line1\nline2 \q` || help.Keys["foo"] != "bar" {
		t.Errorf("raw keys %v", help.Keys)
	}
	if e := m.Entity(2); e.Origin != (mapdata.Vec3{1.5, -2, 0}) || e.Targetname != "Spot" {
		t.Errorf("partial origin %v targetname %q", e.Origin, e.Targetname)
	}
	if e := m.Entity(3); e.Origin != (mapdata.Vec3{8, 0, 0}) || e.Angles != (mapdata.Vec3{1, 2, 3}) {
		t.Errorf("sscanf stop / later angles: origin %v angles %v", e.Origin, e.Angles)
	}
	// F_INT is atoi: "0x10" is 0, "7junk" is 7
	if e := m.Entity(4); e.Wait != 2.5 || e.Delay != 0.25 || e.Random != 1 || e.Spawnflags != 0 || e.Count != 7 {
		t.Errorf("numeric fields wait %g delay %g random %g spawnflags %d count %d", e.Wait, e.Delay, e.Random, e.Spawnflags, e.Count)
	}
	// G_Find matches targetnames case-insensitively
	if ts := m.Targets("SPOT"); len(ts) != 1 || ts[0].Index != 2 {
		t.Errorf("Targets(SPOT) = %v", ts)
	}
}

func TestParseErrors(t *testing.T) {
	for _, tc := range []struct{ ents, want string }{
		{`"classname" "worldspawn"`, "expecting {"},
		{"{\n\"classname\" \"worldspawn\"\n", "EOF without closing brace"},
		{"{\n\"classname\" }\n", "closing brace without data"},
		{"", "empty entity lump"},
	} {
		_, err := synthErr(mapdata.Options{}, tc.ents)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %v, want %q", tc.ents, err, tc.want)
		}
	}
	// a brush entity with a bad inline model drops the server (ERR_DROP)
	_, err := synthErr(mapdata.Options{}, ents(`"classname" "func_door" "model" "*9"`))
	if err == nil || !strings.Contains(err.Error(), "bad number") {
		t.Errorf("bad inline model: error %v", err)
	}
}

func TestInhibitFilter(t *testing.T) {
	const flags = `"classname" "info_notnull" "spawnflags" "%s"`
	src := ents(
		strings.Replace(flags, "%s", "256", 1),  // 1: NOT_EASY
		strings.Replace(flags, "%s", "512", 1),  // 2: NOT_MEDIUM
		strings.Replace(flags, "%s", "1024", 1), // 3: NOT_HARD
		strings.Replace(flags, "%s", "2048", 1), // 4: NOT_DEATHMATCH
		strings.Replace(flags, "%s", "4096", 1), // 5: NOT_COOP (never inhibits)
		strings.Replace(flags, "%s", "1793", 1), // 6: all skills + bit 1
	)
	for _, tc := range []struct {
		opt  mapdata.Options
		want []int // inhibited lump indexes
	}{
		{mapdata.Options{Skill: 0}, []int{1, 6}},
		{mapdata.Options{Skill: 1}, []int{2, 6}},
		{mapdata.Options{Skill: 2}, []int{3, 6}},
		{mapdata.Options{Skill: 3}, []int{3, 6}},
		{mapdata.Options{Skill: 9}, []int{3, 6}}, // clamped to 3
		{mapdata.Options{Skill: -4}, []int{1, 6}},
		{mapdata.Options{Skill: 1, Coop: true}, []int{2, 6}},
		{mapdata.Options{Skill: 0, Deathmatch: true}, []int{4}},
	} {
		m := synth(t, tc.opt, src)
		var got []int
		for i := range m.Entities {
			if m.Entities[i].Inhibited {
				got = append(got, i)
			}
		}
		if !equalInts(got, tc.want) {
			t.Errorf("%+v: inhibited %v, want %v", tc.opt, got, tc.want)
		}
		// the spawn function never sees the inhibit bits
		for i := range m.Entities {
			if e := &m.Entities[i]; !e.Inhibited && e.Spawnflags&^1 != 0 {
				t.Errorf("%+v: #%d spawnflags %#x keep inhibit bits", tc.opt, i, e.Spawnflags)
			}
		}
	}
}

func TestSpawnFrees(t *testing.T) {
	src := ents(
		`"classname" "light"`,                                        // 1: untargeted light
		`"classname" "light" "targetname" "l1"`,                      // 2
		`"classname" "path_corner"`,                                  // 3: untargeted corner
		`"classname" "info_player_coop"`,                             // 4
		`"classname" "info_player_deathmatch"`,                       // 5
		`"classname" "target_changelevel"`,                           // 6: no map
		`"classname" "func_group"`,                                   // 7
		`"classname" "monster_soldier"`,                              // 8: removed in deathmatch
		`"classname" "target_help" "message" "hi"`,                   // 9: removed in deathmatch
		`"classname" "target_lightramp" "message" "aa" "target" "x"`, // 10: bad ramp
		`"classname" "misc_actor" "target" "x"`,                      // 11: untargeted actor
		`"classname" "misc_actor" "targetname" "a"`,                  // 12: actor without a target
		`"classname" "misc_actor" "targetname" "a" "target" "x"`,     // 13: removed in deathmatch only
	)
	for _, tc := range []struct {
		opt  mapdata.Options
		want []int
	}{
		{mapdata.Options{Skill: 1}, []int{1, 3, 4, 5, 6, 7, 10, 11, 12}},
		{mapdata.Options{Skill: 1, Coop: true}, []int{1, 3, 5, 6, 7, 10, 11, 12}},
		{mapdata.Options{Deathmatch: true}, []int{1, 2, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13}},
		{mapdata.Options{Deathmatch: true, Coop: true}, []int{1, 2, 3, 4, 6, 7, 8, 9, 10, 11, 12, 13}}, // coop cleared by deathmatch
	} {
		m := synth(t, tc.opt, src)
		var got []int
		for i := range m.Entities {
			if m.Entities[i].Freed {
				got = append(got, i)
			}
		}
		if !equalInts(got, tc.want) {
			t.Errorf("%+v: freed %v, want %v", tc.opt, got, tc.want)
		}
	}
}

func TestSpawnPoint(t *testing.T) {
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "info_player_start" "targetname" "base1" "origin" "1 0 0"`,
		`"classname" "info_player_start" "origin" "2 0 0"`,
		`"classname" "info_player_start" "targetname" "Base2" "origin" "3 0 0"`,
	))
	for _, tc := range []struct {
		name string
		want float32
		ok   bool
	}{{"", 2, true}, {"base1", 1, true}, {"BASE2", 3, true}, {"nope", 0, false}} {
		s, ok := m.SpawnPoint(tc.name)
		if ok != tc.ok || ok && s.Origin[0] != tc.want {
			t.Errorf("SpawnPoint(%q) = %v, %v", tc.name, s, ok)
		}
	}
	// without an untargeted start, "" falls back to the first one
	m = synth(t, mapdata.Options{}, ents(`"classname" "info_player_start" "targetname" "a" "origin" "5 0 0"`))
	if s, ok := m.SpawnPoint(""); !ok || s.Origin[0] != 5 {
		t.Errorf("fallback SpawnPoint = %v, %v", s, ok)
	}
}

func TestParseLevelString(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want mapdata.LevelString
	}{
		{"demo2$base1", mapdata.LevelString{Map: "demo2", Spawnpoint: "base1"}},
		{"victory.pcx", mapdata.LevelString{Map: "victory.pcx", Kind: mapdata.ExitPic}},
		{"*boss1$start+next.cin", mapdata.LevelString{Map: "boss1", Spawnpoint: "start", Next: "next.cin", NewUnit: true}},
		{"*end.cin+victory.pcx", mapdata.LevelString{Map: "end.cin", Next: "victory.pcx", NewUnit: true, Kind: mapdata.ExitCinematic}},
		{"x.dm2", mapdata.LevelString{Map: "x.dm2", Kind: mapdata.ExitDemo}},
		{".pcx", mapdata.LevelString{Map: ".pcx"}}, // l > 4 is required
	} {
		tc.want.Raw = tc.in
		if got := mapdata.ParseLevelString(tc.in); got != tc.want {
			t.Errorf("ParseLevelString(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestMoverPoses(t *testing.T) {
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "func_door" "model" "*1" "angle" "-1"`,                                         // 1: up, lip 8
		`"classname" "func_door" "model" "*2" "angle" "0" "lip" "16" "spawnflags" "1"`,              // 2: +x, START_OPEN
		`"classname" "func_plat" "model" "*3"`,                                                      // 3: auto height
		`"classname" "func_plat" "model" "*3" "height" "40" "targetname" "p" "speed" "150"`,         // 4
		`"classname" "func_button" "model" "*4" "angle" "-2" "health" "5"`,                          // 5: shoot, lip 4
		`"classname" "func_door_rotating" "model" "*1" "spawnflags" "66" "distance" "-45"`,          // 6: X axis + reverse
		`"classname" "func_train" "model" "*4" "target" "c1" "targetname" "tr"`,                     // 7
		`"classname" "path_corner" "targetname" "c1" "target" "c2" "origin" "100 0 0"`,              // 8
		`"classname" "path_corner" "targetname" "c2" "target" "c1" "origin" "100 200 0" "wait" "2"`, // 9
		`"classname" "func_door" "model" "*5" "angle" "-2" "speed" "50"`,                            // 10: DM doubles speed
	),
		box{{0, 0, 0}, {64, 32, 100}},  // *1
		box{{0, 0, 0}, {80, 16, 64}},   // *2
		box{{0, 0, 0}, {128, 128, 72}}, // *3
		box{{-8, -8, 0}, {8, 8, 16}},   // *4
		box{{0, 0, 0}, {16, 16, 16}},   // *5
	)
	door := m.Mover(1)
	if door.Pos2 != (mapdata.Vec3{0, 0, 92}) || door.Activation != mapdata.ActTouch || door.Trigger == nil {
		t.Errorf("door up: pos2 %v act %v trigger %v", door.Pos2, door.Activation, door.Trigger)
	}
	// Think_SpawnDoorTrigger: abs box (+-1) grown by 60 in x/y, linked (+-1)
	if want := (mapdata.Box{Min: mapdata.Vec3{-62, -62, -2}, Max: mapdata.Vec3{126, 94, 102}}); *door.Trigger != want {
		t.Errorf("door trigger %v, want %v", *door.Trigger, want)
	}
	open := m.Mover(2)
	if !open.StartOpen || open.Pos1[0] != 64 || open.Pos2 != (mapdata.Vec3{}) || open.Origin != open.Pos1 {
		t.Errorf("START_OPEN door: pos1 %v pos2 %v origin %v", open.Pos1, open.Pos2, open.Origin)
	}
	plat := m.Mover(3)
	if plat.Pos1 != (mapdata.Vec3{}) || plat.Pos2 != (mapdata.Vec3{0, 0, -64}) || plat.Origin != plat.Pos2 ||
		plat.Speed != 20 || plat.Accel != 5 || plat.Decel != 5 {
		t.Errorf("plat: pos2 %v origin %v speed %g/%g/%g", plat.Pos2, plat.Origin, plat.Speed, plat.Accel, plat.Decel)
	}
	// plat_spawn_inside_trigger: x/y shrunk by 25, z from the bottom pose to 8 above the top
	if want := (mapdata.Box{Min: mapdata.Vec3{24, 24, 7}, Max: mapdata.Vec3{104, 104, 81}}); *plat.Trigger != want {
		t.Errorf("plat trigger %v, want %v", *plat.Trigger, want)
	}
	tp := m.Mover(4)
	// STATE_UP: Touch_Plat_Center ignores a targeted plat until a use sent it down
	if tp.Pos2 != (mapdata.Vec3{0, 0, -40}) || tp.Origin != tp.Pos1 || tp.Speed != 15 || tp.Activation != mapdata.ActUse {
		t.Errorf("targeted plat: pos2 %v origin %v speed %g act %v", tp.Pos2, tp.Origin, tp.Speed, tp.Activation)
	}
	btn := m.Mover(5)
	if btn.Pos2 != (mapdata.Vec3{0, 0, -12}) || btn.Activation != mapdata.ActShoot || btn.Wait != 3 {
		t.Errorf("button: pos2 %v act %v wait %g", btn.Pos2, btn.Activation, btn.Wait)
	}
	rot := m.Mover(6)
	if rot.Movedir != (mapdata.Vec3{0, 0, -1}) || rot.Pos2 != (mapdata.Vec3{0, 0, 45}) || rot.Distance != -45 {
		t.Errorf("rotating door: movedir %v pos2 %v distance %g", rot.Movedir, rot.Pos2, rot.Distance)
	}
	train := m.Mover(7)
	if len(train.Path) != 2 || train.Path[0].Origin != (mapdata.Vec3{108, 8, 0}) ||
		train.Path[1].Origin != (mapdata.Vec3{108, 208, 0}) || train.Path[1].Wait != 2 || train.Origin != train.Path[0].Origin {
		t.Errorf("train path %+v origin %v", train.Path, train.Origin)
	}
	if train.Activation != mapdata.ActUse {
		t.Errorf("targeted train activation %v", train.Activation)
	}
	if d := m.Mover(10); d.Speed != 50 {
		t.Errorf("single-player door speed %g", d.Speed)
	}
	dm := synth(t, mapdata.Options{Deathmatch: true}, ents(`"classname" "func_door" "model" "*1" "speed" "50"`), box{{0, 0, 0}, {64, 8, 8}})
	if d := dm.Mover(1); d.Speed != 100 {
		t.Errorf("deathmatch door speed %g, want doubled", d.Speed)
	}
}

func TestDoorTeam(t *testing.T) {
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "func_door" "model" "*1" "angle" "90" "team" "t"`,  // 1: master, moves 32-8
		`"classname" "func_door" "model" "*2" "angle" "270" "team" "t"`, // 2: slave, moves 64-8
	),
		box{{0, 0, 0}, {16, 32, 16}},
		box{{0, 100, 0}, {16, 164, 16}},
	)
	master, slave := m.Mover(1), m.Mover(2)
	if slave.TeamMaster != 1 || master.TeamMaster != 1 || len(m.Team(1)) != 2 {
		t.Fatalf("team: master %d/%d chain %v", master.TeamMaster, slave.TeamMaster, m.Team(1))
	}
	// Think_CalcMoveSpeed: both arrive together, the slave runs 56/0.24
	if master.Distance != 24 || slave.Distance != 56 || master.Speed != 100 ||
		math.Abs(float64(slave.Speed)-56/0.24) > 1e-3 || slave.TravelTime != master.TravelTime {
		t.Errorf("team distances %g %g speeds %g %g travel %g %g", master.Distance, slave.Distance,
			master.Speed, slave.Speed, master.TravelTime, slave.TravelTime)
	}
	if slave.Trigger != nil || slave.Activation != 0 || master.Trigger == nil {
		t.Errorf("only the master spawns a trigger: master %v slave %v/%v", master.Trigger, slave.Trigger, slave.Activation)
	}
	if want := (mapdata.Box{Min: mapdata.Vec3{-62, -62, -2}, Max: mapdata.Vec3{78, 226, 18}}); *master.Trigger != want {
		t.Errorf("team trigger %v, want %v", *master.Trigger, want)
	}
	if r := m.Response(2); r != mapdata.RespNone {
		t.Errorf("slave door response %v, want none (door_use ignores slaves)", r)
	}
}

func TestTriggers(t *testing.T) {
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "trigger_once" "model" "*1" "spawnflags" "1" "target" "a"`,      // 1: old TRIGGERED bit
		`"classname" "trigger_multiple" "model" "*1" "angle" "180" "spawnflags" "3"`, // 2: monster, not player, directional
		`"classname" "trigger_hurt" "model" "*1" "spawnflags" "3"`,                   // 3: start off, toggle
		`"classname" "trigger_always" "target" "a"`,                                  // 4: delay at least 0.2
		`"classname" "trigger_counter" "targetname" "c"`,                             // 5
		`"classname" "trigger_monsterjump" "model" "*1"`,                             // 6
	), box{{-16, -16, 0}, {16, 16, 32}})
	once := m.Trigger(1)
	if !once.Triggered || once.StartsEnabled || once.Spawnflags != 4 || once.Wait != -1 || once.Monster {
		t.Errorf("trigger_once fixup: %+v", once)
	}
	if once.Box != (mapdata.Box{Min: mapdata.Vec3{-17, -17, -1}, Max: mapdata.Vec3{17, 17, 33}}) {
		t.Errorf("trigger box %v", once.Box)
	}
	multi := m.Trigger(2)
	if !multi.Monster || !multi.NotPlayer || !multi.Directional() || multi.Movedir[0] != -1 || multi.Wait != 0.2 {
		t.Errorf("trigger_multiple: %+v", multi)
	}
	if hurt := m.Trigger(3); hurt.StartsEnabled || hurt.Dmg != 5 || m.Response(3) != mapdata.RespToggle {
		t.Errorf("trigger_hurt: %+v", hurt)
	}
	if always := m.Trigger(4); always.Delay != 0.2 || always.HasVolume {
		t.Errorf("trigger_always: %+v", always)
	}
	if c := m.Trigger(5); c.Count != 2 || c.Wait != -1 {
		t.Errorf("trigger_counter: %+v", c)
	}
	if j := m.Trigger(6); j.Movedir[2] != 200 || j.Speed != 200 || j.Movedir[0] != 1 {
		t.Errorf("trigger_monsterjump: %+v", j)
	}
}

func TestLaserBeam(t *testing.T) {
	// the beam starts above the slab (z in [-16,0], x,y in [-64,64]) and
	// points down: it stops on the slab top
	m := synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "target_laser" "origin" "0 0 100" "angle" "-2" "spawnflags" "1" "targetname" "l"`,
		`"classname" "target_laser" "origin" "200 0 100" "target" "spot"`,
		`"classname" "info_notnull" "targetname" "spot" "origin" "200 0 -100"`,
		`"classname" "target_laser" "origin" "-20 0 100" "angle" "-2" "target" "nothing"`,
	))
	l := m.Laser(1)
	// (the trace stops DIST_EPSILON short of the plane)
	if !l.StartOn || l.Dmg != 1 || l.Movedir != (mapdata.Vec3{0, 0, -1}) || l.End != (mapdata.Vec3{0, 0, 0.03125}) {
		t.Errorf("laser down onto the slab: %+v", l)
	}
	aimed := m.Laser(2)
	if aimed.StartOn || aimed.Movedir != (mapdata.Vec3{0, 0, -1}) || aimed.End != (mapdata.Vec3{200, 0, -1948}) {
		t.Errorf("aimed laser missing the slab: %+v", aimed)
	}
	// a dangling target is only a dprintf: no enemy, and the angles are not
	// used either, so the beam collapses onto its origin
	bad := m.Laser(4)
	if !bad.BadTarget || bad.Movedir != (mapdata.Vec3{}) || bad.End != bad.Start || aimed.BadTarget {
		t.Errorf("laser with a bad target: %+v", bad)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
