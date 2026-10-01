package mapdata_test

import (
	"testing"

	"quake2web/server/internal/agent/mapdata"
)

// logicMap is a synthetic map exercising every kind of chain link.
func logicMap(t *testing.T) *mapdata.Map {
	return synth(t, mapdata.Options{Skill: 1}, ents(
		`"classname" "func_button" "model" "*1" "target" "t1"`,                                   // 1
		`"classname" "trigger_relay" "targetname" "t1" "target" "t2" "delay" "1.5"`,              // 2
		`"classname" "func_door" "model" "*2" "targetname" "t2" "target" "t3" "team" "a"`,        // 3: master
		`"classname" "func_door" "model" "*3" "team" "a" "target" "t4"`,                          // 4: slave
		`"classname" "target_laser" "targetname" "t3" "spawnflags" "1" "origin" "0 0 100"`,       // 5
		`"classname" "trigger_relay" "targetname" "t4" "killtarget" "w" "target" "k"`,            // 6
		`"classname" "func_wall" "model" "*4" "targetname" "w"`,                                  // 7
		`"classname" "trigger_key" "targetname" "k" "item" "key_blue_key" "target" "c"`,          // 8
		`"classname" "trigger_counter" "targetname" "c" "count" "2" "target" "x"`,                // 9
		`"classname" "target_changelevel" "targetname" "x" "map" "next$s"`,                       // 10
		`"classname" "monster_soldier" "target" "e" "origin" "300 0 0"`,                          // 11
		`"classname" "trigger_once" "model" "*5" "targetname" "e" "spawnflags" "4" "target" "x"`, // 12
		`"classname" "func_door" "model" "*6" "targetname" "lift" "angle" "-2" "lip" "-84"`,      // 13
		`"classname" "trigger_multiple" "model" "*7" "target" "x"`,                               // 14
		`"classname" "func_button" "model" "*8" "target" "lift"`,                                 // 15
		`"classname" "monster_soldier" "target" "walk" "deathtarget" "x"`,                        // 16
		`"classname" "monster_soldier" "target" "walk"`,                                          // 17
		`"classname" "path_corner" "targetname" "walk" "pathtarget" "x"`,                         // 18
		`"classname" "func_door" "model" "*2" "target" "portal"`,                                 // 19
		`"classname" "func_areaportal" "targetname" "portal"`,                                    // 20
		`"classname" "func_button" "model" "*9" "target" "both" "killtarget" "both"`,             // 21
		`"classname" "trigger_relay" "targetname" "both" "target" "x"`,                           // 22
		`"classname" "func_water" "model" "*2" "target" "portal"`,                                // 23
		`"classname" "func_door" "model" "*2" "target" "portal" "delay" "1"`,                     // 24
		`"classname" "trigger_once" "model" "*7" "spawnflags" "4" "target" "y"`,                  // 25: under the lift
		`"classname" "target_changelevel" "targetname" "y" "map" "other$s"`,                      // 26
	),
		box{{200, 0, 0}, {208, 8, 8}},     // *1
		box{{0, 200, 0}, {64, 216, 64}},   // *2
		box{{64, 200, 0}, {128, 216, 64}}, // *3
		box{{0, 300, 0}, {64, 316, 64}},   // *4
		box{{0, 400, 0}, {64, 416, 64}},   // *5
		box{{0, 0, 100}, {64, 64, 116}},   // *6: a lift dropping 100
		box{{10, 10, 20}, {50, 50, 40}},   // *7: below the lift
		box{{300, 0, 0}, {308, 8, 8}},     // *8
		box{{400, 0, 0}, {408, 8, 8}},     // *9
	)
}

func reachedByEntity(rs []mapdata.Reached) map[int]mapdata.Reached {
	out := map[int]mapdata.Reached{}
	for _, r := range rs {
		out[r.Entity] = r
	}
	return out
}

func TestReachChain(t *testing.T) {
	m := logicMap(t)
	got := reachedByEntity(m.Reach(1))
	btn := m.Mover(1).TravelTime
	for _, tc := range []struct {
		ent   int
		resp  mapdata.Response
		via   mapdata.LinkKind
		delay float32
	}{
		{2, mapdata.RespRelay, mapdata.LinkTarget, btn},
		{3, mapdata.RespMove, mapdata.LinkTarget, btn + 1.5},
		{4, mapdata.RespMove, mapdata.LinkTeam, btn + 1.5}, // the slave opens with its master
		{5, mapdata.RespToggle, mapdata.LinkTarget, btn + 1.5},
		{6, mapdata.RespRelay, mapdata.LinkTarget, btn + 1.5}, // fired by the slave itself
		{7, mapdata.RespNone, mapdata.LinkKill, btn + 1.5},
		{8, mapdata.RespKey, mapdata.LinkTarget, btn + 1.5},
		{9, mapdata.RespCounter, mapdata.LinkTarget, btn + 1.5},
		{10, mapdata.RespExit, mapdata.LinkTarget, btn + 1.5},
	} {
		r, ok := got[tc.ent]
		if !ok {
			t.Errorf("#%d not reached from the button", tc.ent)
			continue
		}
		if r.Response != tc.resp || r.Via.Kind != tc.via || r.Delay != tc.delay {
			t.Errorf("#%d reached as %v via %v after %gs, want %v via %v after %gs",
				tc.ent, r.Response, r.Via.Kind, r.Delay, tc.resp, tc.via, tc.delay)
		}
	}
	if !got[7].Removed {
		t.Error("killtarget not marked removed")
	}
	exit := got[10]
	if len(exit.Requires) != 1 || exit.Requires[0] != "key_blue_key" || !exit.Counter {
		t.Errorf("exit requires %v counter %v, want the key and a counter", exit.Requires, exit.Counter)
	}
	if _, ok := got[12]; ok {
		t.Error("the TRIGGERED trigger is not on the button's chain")
	}

	// a monster's death enables the TRIGGERED trigger and stops there
	death := reachedByEntity(m.Reach(11))
	if r := death[12]; r.Response != mapdata.RespEnable || len(death) != 1 {
		t.Errorf("monster death reaches %v", death)
	}
	// the lift carries its rider into the trigger below, arriving after the
	// button and the lift have both travelled
	lift := reachedByEntity(m.Reach(15))
	arrive := m.Mover(15).TravelTime + m.Mover(13).TravelTime
	if r := lift[14]; r.Via.Kind != mapdata.LinkCarry || r.Via.From != 13 || r.Response != mapdata.RespRelay ||
		r.Disabled || m.Mover(13).TravelTime == 0 || r.Delay != arrive {
		t.Errorf("lift chain %+v, want a carry 13 -> 14 after %gs", lift[14], arrive)
	}
	if r := lift[10]; r.Response != mapdata.RespExit || r.Via.From != 14 || r.Delay != arrive {
		t.Errorf("lift chain does not reach the exit through the trigger: %+v", lift[10])
	}
	// the TRIGGERED trigger under the lift is touched, not enabled, and
	// ignores the rider while disabled
	if r := lift[25]; r.Via.Kind != mapdata.LinkCarry || r.Response != mapdata.RespRelay || !r.Disabled {
		t.Errorf("carry into the TRIGGERED trigger: %+v", r)
	}
	if _, ok := lift[26]; ok {
		t.Error("the chain went on through the disabled trigger")
	}
	// ... and fires once something enabled it
	enabled := reachedByEntity(m.ReachEnabled(15, func(i int) bool { return i == 25 }))
	if r := enabled[25]; r.Disabled {
		t.Errorf("enabled trigger still disabled: %+v", r)
	}
	if r, ok := enabled[26]; !ok || r.Response != mapdata.RespExit || r.Via.From != 25 {
		t.Errorf("the enabled trigger does not fire its exit: %+v", r)
	}

	// G_UseTargets frees the killtargets before it fires the targets: the
	// relay that is both is removed, never used
	both := reachedByEntity(m.Reach(21))
	if r := both[22]; !r.Removed || r.Response != mapdata.RespNone || len(both) != 1 {
		t.Errorf("killtarget and target at once: %v", both)
	}
}

func TestFiredByAndSources(t *testing.T) {
	m := logicMap(t)
	var from []int
	for _, l := range m.FiredBy(10) {
		from = append(from, l.From)
	}
	// target links (counter, triggered once, multiple, deathtarget, corner
	// pathtarget, a relay that its user kills first) in entity order
	if !equalInts(from, []int{9, 12, 14, 16, 18, 22}) {
		t.Errorf("FiredBy(exit) = %v", from)
	}
	for _, tc := range []struct {
		ent  int
		want []mapdata.Source
	}{
		{1, []mapdata.Source{mapdata.SrcTouch}},
		{11, []mapdata.Source{mapdata.SrcDeath}},
		{12, []mapdata.Source{mapdata.SrcTouch}},
		{18, []mapdata.Source{mapdata.SrcCorner}},
		{2, nil},
		{3, nil}, // a targeted door is only used
		{19, []mapdata.Source{mapdata.SrcTouch}},
	} {
		got := m.Sources(tc.ent)
		if len(got) != len(tc.want) || len(got) > 0 && got[0] != tc.want[0] {
			t.Errorf("Sources(#%d) = %v, want %v", tc.ent, got, tc.want)
		}
	}
	// monster_start_go clears a target naming a path_corner: #17 fires nothing
	if tn := m.FiredTargetname(17); tn != "" {
		t.Errorf("FiredTargetname(walking monster) = %q", tn)
	}
	if tn := m.FiredTargetname(16); tn != "x" {
		t.Errorf("FiredTargetname(deathtarget) = %q", tn)
	}
	// doors open area portals themselves, not through G_UseTargets; so
	// does func_water, which renames itself func_door. A delayed use is
	// fired by a DelayedUse entity, which the exception does not cover.
	for _, door := range []int{19, 23} {
		if ls := m.Fires(door); len(ls) != 0 {
			t.Errorf("#%d fires %v, want no area portal link", door, ls)
		}
	}
	if ls := m.Fires(24); len(ls) != 1 || ls[0].To != 20 || ls[0].Delay != 1 {
		t.Errorf("delayed door fires %v, want the area portal after 1s", ls)
	}
}
