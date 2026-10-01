package route_test

import (
	"strings"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/bsp"
)

// synthMap is a small level on the synthetic floor BSP with extra inline
// models. The boxes are the bounds the game sees (CMod_LoadSubmodels
// spreads the file bounds by 1, so the file gets them shrunk by 1).
//
//	#2 button A -> #3 lift (drops 100) carrying the rider into #4, a
//	   TRIGGERED trigger_once that fires the exit #5; the death of #6
//	   enables #4
//	#7 button B -> #8 train on c1 -> c2 (wait 2) -> c3 (wait -1) -> c1
//	#12 button C -> #13 trigger_once used by name -> #14 door D
func synthMap(t *testing.T) *mapdata.Map {
	t.Helper()
	const ents = `{ "classname" "worldspawn" }
{ "classname" "info_player_start" "origin" "0 300 24" }
{ "classname" "func_button" "model" "*1" "target" "lift" }
{ "classname" "func_door" "model" "*2" "targetname" "lift" "angle" "-2" "lip" "-84" }
{ "classname" "trigger_once" "model" "*3" "spawnflags" "4" "targetname" "en" "target" "x" }
{ "classname" "target_changelevel" "targetname" "x" "map" "next$s" }
{ "classname" "monster_soldier" "origin" "300 300 0" "target" "en" }
{ "classname" "func_button" "model" "*4" "target" "tr" }
{ "classname" "func_train" "model" "*6" "targetname" "tr" "target" "c1" }
{ "classname" "path_corner" "targetname" "c1" "target" "c2" "origin" "100 0 0" }
{ "classname" "path_corner" "targetname" "c2" "target" "c3" "origin" "100 200 0" "wait" "2" }
{ "classname" "path_corner" "targetname" "c3" "target" "c1" "origin" "200 200 0" "wait" "-1" }
{ "classname" "func_button" "model" "*8" "target" "once" }
{ "classname" "trigger_once" "model" "*5" "targetname" "once" "target" "d" }
{ "classname" "func_door" "model" "*7" "targetname" "d" }
`
	models := [][2]mapdata.Vec3{
		{{200, 0, 0}, {208, 8, 8}},   // *1 button A
		{{0, 0, 100}, {64, 64, 116}}, // *2 lift
		{{10, 10, 20}, {50, 50, 40}}, // *3 under the lift
		{{300, 0, 0}, {308, 8, 8}},   // *4 button B
		{{0, 400, 0}, {64, 416, 64}}, // *5
		{{-8, -8, 0}, {8, 8, 16}},    // *6 train
		{{0, 200, 0}, {64, 216, 64}}, // *7 door D
		{{400, 0, 0}, {408, 8, 8}},   // *8 button C
	}
	f := bsp.SyntheticFloorMap()
	for _, b := range models {
		var d bsp.DModel
		for k := 0; k < 3; k++ {
			d.Mins[k], d.Maxs[k] = b[0][k]+1, b[1][k]-1
		}
		f.Models = append(f.Models, d)
	}
	f.Entities = append([]byte(ents), 0)
	m, err := mapdata.Load("synth", bsp.Encode(f), mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestSynthChains checks the validator's reading of chains that need state
// from earlier steps: TRIGGERED triggers reached by a carrying mover, the
// poses a use sends a mover to, and chains through used-up entities.
func TestSynthChains(t *testing.T) {
	m := synthMap(t)
	for _, tc := range []struct {
		name  string
		steps string
		want  []string // nil: valid
		not   []string
	}{
		{"carry into a disabled TRIGGERED trigger", `
			{"op":"press","target":{"model":"*1"},"effects":[{"kind":"moverAt","target":{"model":"*2"}}]},
			{"op":"ride","target":{"model":"*2"},"until":"pos2"},
			{"op":"touch","target":{"model":"*3"},"effects":[{"kind":"exit","target":{"entity":5}}]}`,
			[]string{"step 2 (touch): #4 trigger_once *3 (en) is TRIGGERED and no earlier step enables it"}, nil},
		{"a carry does not enable", `
			{"op":"press","target":{"model":"*1"},"effects":[{"kind":"enable","target":{"entity":4}}]}`,
			[]string{"#2 func_button *1 does not cause enable of #4 trigger_once *3 (en)", "#4 trigger_once *3 (en) [disabled]"}, nil},
		{"carry into a trigger enabled earlier", `
			{"op":"kill","class":"monster_soldier","pos":[300,300,0],"effects":[{"kind":"enable","target":{"entity":4}}]},
			{"op":"press","target":{"model":"*1"},"effects":[{"kind":"moverAt","target":{"model":"*2"},"pose":"pos2"},{"kind":"exit","target":{"entity":5}}]}`,
			nil, nil},
		{"door pose a use does not reach", `
			{"op":"kill","class":"monster_soldier","pos":[300,300,0],"effects":[{"kind":"enable","target":{"entity":4}}]},
			{"op":"press","target":{"model":"*1"},"effects":[{"kind":"moverAt","target":{"model":"*2"},"pose":"pos1"},{"kind":"exit","target":{"entity":5}}]}`,
			[]string{`effect moverAt: #3 func_door *2 (lift) moves to pos2 when used, not "pos1"`}, nil},
		{"train corners", `
			{"op":"press","target":{"model":"*4"},"effects":[{"kind":"moverAt","target":{"model":"*6"},"pose":"c2"},{"kind":"moverAt","target":{"model":"*6"},"pose":"c3"}]},
			{"op":"press","target":{"model":"*4"},"effects":[{"kind":"moverAt","target":{"model":"*6"},"pose":"c1"}]}`,
			[]string{`step 1 (press): effect moverAt: #8 func_train *6 (tr) moves to c2, c3 when used, not "c1"`},
			[]string{`not "c2"`, `not "c3"`}},
		{"chain through a used-up trigger_once", `
			{"op":"press","target":{"model":"*8"},"effects":[{"kind":"doorOpen","target":{"model":"*7"}}]},
			{"op":"press","target":{"model":"*8"},"effects":[{"kind":"doorOpen","target":{"model":"*7"}}]}`,
			[]string{"step 1 (press): the chain to #14 func_door *7 (d) passes #13 trigger_once *5 (once), which was fired by p step 0"},
			[]string{"step 0 (press): the chain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb, err := route.ParseTable([]byte(`{"schema":1,"name":"p","map":"synth","from":"","exit":{"map":"next$s"},"steps":[` + tc.steps + `]}`))
			if err != nil {
				t.Fatal(err)
			}
			err = route.Validate(tb, m)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected problems:\n%v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Validate accepted the table")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("problems do not mention %q:\n%v", w, err)
				}
			}
			for _, w := range tc.not {
				if strings.Contains(err.Error(), w) {
					t.Errorf("problems mention %q:\n%v", w, err)
				}
			}
		})
	}
}
