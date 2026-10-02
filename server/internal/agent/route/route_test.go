package route_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func routesDir(t testing.TB) string {
	t.Helper()
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "fixtures", "agent", "routes")
}

func loadCampaign(t testing.TB) *route.Campaign {
	t.Helper()
	c, err := route.Load(routesDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// demoMaps loads map data from the demo pak at the given skill, caching
// per test binary run.
type demoMaps struct {
	mu    sync.Mutex
	p     *pak.Pak
	skill int
	maps  map[string]*mapdata.Map
}

func newDemoMaps(t testing.TB, skill int) *demoMaps {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return &demoMaps{p: p, skill: skill, maps: map[string]*mapdata.Map{}}
}

func (d *demoMaps) load(name string) (*mapdata.Map, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if m, ok := d.maps[name]; ok {
		return m, nil
	}
	raw, err := d.p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		return nil, err
	}
	m, err := mapdata.Load(name, raw, mapdata.Options{Skill: d.skill})
	if err != nil {
		return nil, err
	}
	d.maps[name] = m
	return m, nil
}

func (d *demoMaps) get(t testing.TB, name string) *mapdata.Map {
	t.Helper()
	m, err := d.load(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCheckedInTablesValid(t *testing.T) {
	c := loadCampaign(t)
	if len(c.Tables) != 4 || c.Terminal.Exit != "victory.pcx" {
		t.Fatalf("campaign has %d tables, terminal %q", len(c.Tables), c.Terminal.Exit)
	}
	maps := newDemoMaps(t, c.Skill)
	for _, tb := range c.Tables {
		if err := route.Validate(tb, maps.get(t, tb.Map)); err != nil {
			t.Errorf("%s:\n%v", tb.Name, err)
		}
	}
	if err := route.ValidateCampaign(c, maps.load); err != nil {
		t.Errorf("campaign:\n%v", err)
	}
}

func TestSelect(t *testing.T) {
	c := loadCampaign(t)
	for _, tc := range []struct {
		m     string
		visit int
		want  string
	}{{"demo1", 0, "demo1"}, {"demo2", 0, "demo2a"}, {"DEMO2", 1, "demo2b"}, {"demo3", 0, "demo3"}} {
		tb, err := c.Select(tc.m, tc.visit)
		if err != nil || tb.Name != tc.want {
			t.Errorf("Select(%s, %d) = %v, %v; want %s", tc.m, tc.visit, tb, err, tc.want)
			continue
		}
		if i := c.Index(tb); i < 0 || c.Tables[i] != tb {
			t.Errorf("Index(%s) = %d", tb.Name, i)
		}
	}
	if _, err := c.Select("demo2", 2); err == nil {
		t.Error("Select(demo2, 2) succeeded")
	}
}

// clone deep-copies a table through JSON so a test can break it.
func clone(t *testing.T, tb *route.Table) *route.Table {
	t.Helper()
	b, err := json.Marshal(tb)
	if err != nil {
		t.Fatal(err)
	}
	out, err := route.ParseTable(b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func intp(i int) *int             { return &i }
func f32p(f float32) *float32     { return &f }
func ref(model string) *route.Ref { return &route.Ref{Model: model} }

// TestBrokenTables breaks the checked-in tables one way at a time and
// checks the validator explains what is wrong.
func TestBrokenTables(t *testing.T) {
	c := loadCampaign(t)
	maps := newDemoMaps(t, c.Skill)
	table := func(name string) *route.Table {
		for _, tb := range c.Tables {
			if tb.Name == name {
				return clone(t, tb)
			}
		}
		t.Fatalf("no table %s", name)
		return nil
	}
	stepOp := func(tb *route.Table, op route.Op, model string) *route.Step {
		for i := range tb.Steps {
			s := &tb.Steps[i]
			if s.Op == op && (model == "" || s.Target != nil && s.Target.Model == model) {
				return s
			}
		}
		t.Fatalf("%s has no %s %s step", tb.Name, op, model)
		return nil
	}
	pickup := func(tb *route.Table, class string) *route.Step {
		for i := range tb.Steps {
			if s := &tb.Steps[i]; s.Op == route.OpPickup && s.Class == class {
				return s
			}
		}
		t.Fatalf("%s has no pickup of %s", tb.Name, class)
		return nil
	}
	remove := func(tb *route.Table, s *route.Step) {
		for i := range tb.Steps {
			if &tb.Steps[i] == s {
				tb.Steps = append(tb.Steps[:i], tb.Steps[i+1:]...)
				return
			}
		}
	}

	for _, tc := range []struct {
		name   string
		table  string
		break_ func(tb *route.Table)
		want   []string
	}{
		{"wrong classname", "demo1", func(tb *route.Table) {
			stepOp(tb, route.OpPress, "*34").Target.Classname = "func_door"
		}, []string{"step 5 (press)", "entity is #591 func_button *34"}},
		{"model and entity disagree", "demo1", func(tb *route.Table) {
			stepOp(tb, route.OpPress, "*34").Target.Entity = intp(590)
		}, []string{"entity is #590 func_door *33", `model "*33"`}},
		{"press a door", "demo1", func(tb *route.Table) {
			s := stepOp(tb, route.OpPress, "*34")
			s.Target = &route.Ref{Model: "*31"}
		}, []string{"#582 func_door *31 (t4) is not a touch-activated func_button (activation use)"}},
		{"press without effects", "demo1", func(tb *route.Table) {
			stepOp(tb, route.OpPress, "*34").Effects = nil
		}, []string{"press must claim at least one effect", "#582 func_door *31 (t4) [move]"}},
		{"claimed effect not reached", "demo2a", func(tb *route.Table) {
			s := stepOp(tb, route.OpPress, "*48")
			s.Effects = append(s.Effects, route.Effect{Kind: route.EffDoorOpen, Target: route.Ref{Model: "*46"}})
		}, []string{"#533 func_button *48 does not cause doorOpen of #502 func_door *46 (t7)"}},
		{"unknown exit", "demo1", func(tb *route.Table) {
			tb.Exit.Map = "demo2$base9"
		}, []string{`exit "demo2$base9" is no target_changelevel of demo1 (it has "demo2$base1")`}},
		{"last step short of the exit", "demo1", func(tb *route.Table) {
			tb.Steps = tb.Steps[:2]
		}, []string{`the last step does not lead to the exit "demo2$base1"`}},
		{"missing arrival spawnpoint", "demo3", func(tb *route.Table) {
			tb.From = "base9"
		}, []string{`arrival spawnpoint "base9"`}},
		{"key never picked up", "demo3", func(tb *route.Table) {
			remove(tb, pickup(tb, "key_blue_key"))
		}, []string{"step 8 (touch)", "trigger_key that needs key_blue_key, which no earlier step picks up"}},
		{"optional kill", "demo3", func(tb *route.Table) {
			stepOp(tb, route.OpKill, "").Optional = true
		}, []string{"step 4 (kill): a kill cannot be optional (only goto, press, wait, pickup)"}},
		{"optional key", "demo3", func(tb *route.Table) {
			pickup(tb, "key_blue_key").Optional = true
		}, []string{"step 8 (pickup): a key pickup cannot be optional"}},
		{"optional last step", "demo1", func(tb *route.Table) {
			tb.Steps[len(tb.Steps)-1].Optional = true
		}, []string{"step 7 (touch): the last step cannot be optional"}},
		{"required step relies on an optional one", "demo2a", func(tb *route.Table) {
			stepOp(tb, route.OpPress, "*48").Optional = true
		}, []string{"step 7 (wait) (without the optional steps): no earlier step causes doorOpen of #538 func_door_rotating *49 (t5)"}},
		{"directional exit without yaw", "demo3", func(tb *route.Table) {
			stepOp(tb, route.OpTouch, "*34").Yaw = nil
		}, []string{"#592 trigger_multiple *34 is directional", "give a yaw or face first"}},
		{"directional exit faced the wrong way", "demo3", func(tb *route.Table) {
			stepOp(tb, route.OpTouch, "*34").Yaw = f32p(180)
		}, []string{"yaw 180 faces away"}},
		{"face step satisfies the directional exit", "demo3", func(tb *route.Table) {
			s := stepOp(tb, route.OpTouch, "*34")
			s.Yaw = nil
			i := len(tb.Steps) - 1
			tb.Steps = append(tb.Steps[:i], route.Step{Op: route.OpFace, Yaw: f32p(10)}, tb.Steps[i])
		}, nil},
		{"inhibited monster", "demo3", func(tb *route.Table) {
			s := stepOp(tb, route.OpKill, "")
			s.Target = &route.Ref{Entity: intp(28)} // monster_gunner with NOT_EASY|NOT_MEDIUM
			s.Pos = &route.Vec{832, 80, -420}
		}, []string{"#28 monster_gunner (t108) is inhibited at skill 1 (spawnflags 0x302)"}},
		{"kill at the wrong spawn origin", "demo3", func(tb *route.Table) {
			s := stepOp(tb, route.OpKill, "")
			s.Target = nil
			s.Pos = &route.Vec{0, 0, 0}
		}, []string{"no present monster_gunner spawned at [0 0 0] (there are #32 at"}},
		{"TRIGGERED trigger touched before it is enabled", "demo2b", func(tb *route.Table) {
			remove(tb, stepOp(tb, route.OpKill, ""))
		}, []string{"#667 trigger_once *58 (t85) is TRIGGERED and no earlier step enables it"}},
		{"wait for an effect nothing caused", "demo2a", func(tb *route.Table) {
			remove(tb, stepOp(tb, route.OpPress, "*48"))
		}, []string{"step 6 (wait): no earlier step causes doorOpen of #538 func_door_rotating *49 (t5)"}},
		{"avoid entry that is no exit activator", "demo2a", func(tb *route.Table) {
			tb.Avoid = append(tb.Avoid, route.Avoid{Target: route.Ref{Model: "*36"}, Why: "x"})
		}, []string{"avoid 2: #408 func_button *36 does not lead to another exit"}},
		{"step uses an avoided activator", "demo2a", func(tb *route.Table) {
			i := len(tb.Steps) - 1
			tb.Steps = append(tb.Steps[:i], route.Step{Op: route.OpPress, Target: ref("*43"),
				Effects: []route.Effect{{Kind: route.EffMoverAt, Target: route.Ref{Model: "*46"}}}}, tb.Steps[i])
		}, []string{`step 8 (press): leads to the wrong exit "demo1$base2"`, "step 8 uses #495 func_button *43, which the table avoids"}},
		{"unknown op and kind", "demo1", func(tb *route.Table) {
			tb.Steps[0].Op = "teleport"
			s := stepOp(tb, route.OpPress, "*34")
			s.Effects[0].Kind = "explode"
		}, []string{`step 0 (teleport): unknown op "teleport"`, `unknown effect kind "explode"`}},
		{"bad ride pose", "demo3", func(tb *route.Table) {
			stepOp(tb, route.OpRide, "").Until = "middle"
		}, []string{`#731 func_plat *40 has no pose "middle" (poses: pos1/top, pos2/bottom)`}},
		{"mover pose the use does not send it to", "demo1", func(tb *route.Table) {
			stepOp(tb, route.OpPress, "*34").Effects[0].Pose = "pos1"
		}, []string{`step 5 (press): effect moverAt: #582 func_door *31 (t4) moves to pos2 when used, not "pos1"`}},
		{"START_OPEN door claimed to open", "demo2b", func(tb *route.Table) {
			s := stepOp(tb, route.OpTouch, "*22")
			s.Effects = append(s.Effects, route.Effect{Kind: route.EffDoorOpen, Target: route.Ref{Model: "*42"}})
		}, []string{"effect doorOpen: #487 func_door_rotating *42 (t6) starts open, so the use closes it (claim moverAt pos2)"}},
		{"kill target at the wrong spawn origin", "demo2b", func(tb *route.Table) {
			stepOp(tb, route.OpKill, "").Pos = &route.Vec{0, 0, 0}
		}, []string{"#29 monster_gunner (t88) is not a present monster_gunner spawned at [0 0 0]"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tb := table(tc.table)
			tc.break_(tb)
			err := route.Validate(tb, maps.get(t, tb.Map))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("unexpected problems:\n%v", err)
				}
				return
			}
			var verr *route.Error
			if !errors.As(err, &verr) {
				t.Fatalf("Validate = %v, want a *route.Error", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("problems do not mention %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestCampaignContinuity(t *testing.T) {
	maps := newDemoMaps(t, 1)
	for _, tc := range []struct {
		name   string
		break_ func(c *route.Campaign)
		want   []string
	}{
		{"arrival does not match the previous exit", func(c *route.Campaign) {
			c.Tables[3].From = "base1"
		}, []string{"previous visit demo3 exits to \"demo2$base3b\", but this visit arrives at demo2$base1"}},
		{"visit index", func(c *route.Campaign) {
			c.Tables[3].Visit = 0
		}, []string{"demo2b: table: visit 0, but it is visit 1 of demo2"}},
		{"first visit", func(c *route.Campaign) {
			c.Start = "demo2"
		}, []string{"the first visit must start demo2 without a spawnpoint"}},
		{"terminal", func(c *route.Campaign) {
			c.Terminal = route.Terminal{Exit: "end.cin", Kind: "cin"}
		}, []string{`the last visit exits to "victory.pcx", the campaign ends at "end.cin"`}},
		{"terminal kind", func(c *route.Campaign) {
			c.Terminal.Kind = "level"
		}, []string{`terminal exit "victory.pcx" is a pic, not a level`}},
		{"revisit relies on a used-up button", func(c *route.Campaign) {
			tb := c.Tables[3]
			i := len(tb.Steps) - 1
			tb.Steps = append(tb.Steps[:i], route.Step{Op: route.OpPress, Target: ref("*48"),
				Effects: []route.Effect{{Kind: route.EffDoorOpen, Target: route.Ref{Model: "*49"}}}}, tb.Steps[i])
		}, []string{
			"demo2b: step 6 (press): #533 func_button *48 was already pressed by demo2a step 6",
			"demo2b: step 6 (press): effect doorOpen: #538 func_door_rotating *49 (t5) was already moved by demo2a step 6 and does not move again",
		}},
		{"revisit refers to entities the first visit removed", func(c *route.Campaign) {
			// the first visit pulls the lever: t6 kills the 'block' walls
			a := c.Tables[1]
			i := len(a.Steps) - 1
			a.Steps = append(a.Steps[:i], route.Step{Op: route.OpTouch, Target: ref("*22"),
				Effects: []route.Effect{{Kind: route.EffRemove, Target: route.Ref{Model: "*17"}}}}, a.Steps[i])
			b := c.Tables[3]
			b.Steps = append([]route.Step{{Op: route.OpGoto, Target: ref("*17")}}, b.Steps...)
		}, []string{
			"demo2b: step 0 (goto): goto: #186 func_wall *17 (block) was already removed by demo2a step 8",
			"demo2b: step 2 (touch): #284 trigger_once *22 was already fired by demo2a step 8",
			"demo2b: step 2 (touch): effect remove: #186 func_wall *17 (block) was already removed by demo2a step 8",
			"demo2b: step 2 (touch): effect moverAt: #558 func_door_rotating *51 (t67) was already moved by demo2a step 8",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := loadCampaign(t)
			tc.break_(c)
			err := route.ValidateCampaign(c, maps.load)
			if err == nil {
				t.Fatal("ValidateCampaign accepted a broken campaign")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("problems do not mention %q:\n%v", w, err)
				}
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	if _, err := route.ParseTable([]byte(`{"schema":1,"name":"x","map":"demo1","steps":[{"op":"goto","targte":{"model":"*1"}}]}`)); err == nil ||
		!strings.Contains(err.Error(), "targte") {
		t.Errorf("unknown field accepted: %v", err)
	}
	if _, err := route.ParseTable([]byte(`{"schema":1} {}`)); err == nil {
		t.Error("trailing data accepted")
	}
	if _, err := route.ParseTable([]byte(`{"schema":2,"name":"x","map":"demo1"}`)); err == nil ||
		!strings.Contains(err.Error(), "schema 2, want 1") {
		t.Errorf("schema 2 accepted: %v", err)
	}
}

func TestResolve(t *testing.T) {
	m := newDemoMaps(t, 1).get(t, "demo2")
	for _, tc := range []struct {
		r    route.Ref
		want int
		err  string
	}{
		{route.Ref{Model: "*59"}, 668, ""},
		{route.Ref{Entity: intp(669), Classname: "target_changelevel", Targetname: "T86"}, 669, ""}, // G_Find is case-insensitive
		{route.Ref{Targetname: "t86"}, 669, ""},
		{route.Ref{Targetname: "t84"}, 0, "ambiguous, 2 entities match"},
		{route.Ref{Targetname: "t84", Classname: "func_door"}, 0, "ambiguous"},
		{route.Ref{Targetname: "nope"}, 0, "no present entity has this targetname"},
		{route.Ref{Model: "*999"}, 0, "no entity uses model *999"},
		{route.Ref{Entity: intp(5000)}, 0, "no entity #5000"},
		{route.Ref{Entity: intp(28)}, 0, "inhibited at skill 1"},
		{route.Ref{Model: "*7"}, 0, "inhibited at skill 1"}, // func_wall with NOT_EASY|NOT_MEDIUM|NOT_HARD
		{route.Ref{}, 0, "empty entity reference"},
	} {
		e, err := route.Resolve(tc.r, m)
		switch {
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("Resolve(%v) error %v, want %q", tc.r, err, tc.err)
		case tc.err == "" && (err != nil || e.Index != tc.want):
			t.Errorf("Resolve(%v) = %v, %v; want #%d", tc.r, e, err, tc.want)
		}
	}
}
