package bot

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// demoLevel loads demo map name (skill 1) with its nav graph from the
// shared cache (built when missing; under -race it skips without the
// cache unless Q2_AGENT_LONG is set).
func demoLevel(t testing.TB, fs *pak.FS, name string) (*mapdata.Map, *nav.Graph) {
	t.Helper()
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	params, dir := nav.DefaultParams(), nav.DefaultDir()
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		g, err := nav.ReadFile(filepath.Join(dir, nav.CacheName(md.Name, md.Checksum, params)))
		if err != nil || g.Matches(md, params) != nil {
			t.Skipf("%s: no cached nav graph in %s (run q2nav build -all, or set Q2_AGENT_LONG=1)", name, dir)
		}
	}
	g, err := nav.NewStore(dir, navbuild.StoreBuilder(fs.ReadFile, navbuild.Config{})).Load(context.Background(), md, params)
	if err != nil {
		t.Fatal(err)
	}
	return md, g
}

// TestIntermissionAndDeath: in PM_FREEZE the bot holds still and presses
// a button once it has been frozen IntermissionPress; dead it sends idle
// commands; before Enter, or for another level generation, too.
func TestIntermissionAndDeath(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	md, g := demoLevel(t, fs, "demo1")
	c := fakeclient.NewPassive(fakeclient.Options{})
	b := New(Config{})
	if u := b.Cmd(c, 25); u.Buttons != 0 || u.ForwardMove != 0 {
		t.Fatalf("command before Enter: %+v", u)
	}
	if err := b.Enter(Level{Key: worldmodel.LevelKey{Map: "demo1"}, Gen: c.LevelGen(), Map: md, Graph: g}); err != nil {
		t.Fatal(err)
	}
	bel := &worldmodel.Belief{}
	bel.Self.PmType = q2const.PM_FREEZE
	c.Frame.PlayerState.PMove.PmType = q2const.PM_FREEZE
	c.Frame.PlayerState.ViewAngles = Vec3{0, 90, 0}
	b.now = 10000
	b.decide(c, bel)
	if b.Mode() != ModeIntermission {
		t.Fatalf("mode %s", b.Mode())
	}
	for _, at := range []int64{10000, 10000 + IntermissionPress - 100} {
		b.now = at
		b.decide(c, bel)
		if u := b.Cmd(c, 25); u.Buttons != 0 {
			t.Fatalf("pressed %d ms into the intermission", at-10000)
		}
	}
	b.now = 10000 + IntermissionPress
	b.decide(c, bel)
	u := b.Cmd(c, 25)
	if u.Buttons&q2const.BUTTON_ANY == 0 || u.ForwardMove != 0 || u.SideMove != 0 {
		t.Fatalf("after %d ms frozen: %+v", IntermissionPress, u)
	}
	if !b.Stats().Pressed {
		t.Error("Stats.Pressed not set")
	}
	// dead: no buttons (attack would ask the game to respawn)
	bel.Self.PmType, bel.Self.Dead = q2const.PM_DEAD, true
	c.Frame.PlayerState.PMove.PmType = q2const.PM_DEAD
	b.decide(c, bel)
	if u := b.Cmd(c, 25); u.Buttons != 0 || b.Mode() != ModeDead {
		t.Fatalf("dead: %+v mode %s", u, b.Mode())
	}
}

// TestLiveKill drives the bot on demo1 (lockstep, god and notarget) with
// a one-step route: kill the soldier that guards the first corridor. The
// route executor finds it by its lump entity, takes a firing position and
// the shoot executor kills it with what the bot carries. Along the way the
// bot asks for its inventory and the help computer through side commands
// (never in the fight, at most a pair every 2 s).
func TestLiveKill(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep run")
	}
	fs := sessiontest.DemoFS(t)
	md, g := demoLevel(t, fs, "demo1")
	const soldier = 433
	m := md.Entity(soldier)
	if m == nil || m.Classname != "monster_soldier" {
		t.Fatalf("#%d is not a soldier", soldier)
	}
	ent := soldier
	tab := &route.Table{Schema: route.SchemaVersion, Name: "kill", Map: "demo1", Exit: route.ExitRef{Map: "demo2$base1"},
		Steps: []route.Step{{Op: route.OpKill, Class: "monster_soldier", Pos: &route.Vec{m.Origin[0], m.Origin[1], m.Origin[2]}, Target: &route.Ref{Entity: &ent}}}}
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1, Client: fakeclient.Options{MaxHistory: 256}})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := l.Client()
	c.StringCmd("god")
	c.StringCmd("notarget")
	b := New(Config{ReadFile: fs.ReadFile, Logf: t.Logf})
	if err := b.Enter(Level{Key: worldmodel.LevelKey{Map: "demo1"}, Gen: c.LevelGen(), Map: md, Graph: g, Route: tab}); err != nil {
		t.Fatal(err)
	}
	maxSide, fought := 0, false
	lastSide, sideFrames := 0, 0
	for frame := 0; frame < 900 && !b.Route().Done(); frame++ {
		if err := l.Step(ctx, b.Cmd); err != nil {
			t.Fatal(err)
		}
		b.Observe(c, l.GameTimeMs())
		if b.Target() != "" {
			fought = true
			if b.Stats().SideCommands != lastSide {
				t.Errorf("frame %d: a side command pair in a fight", frame)
			}
		}
		if n := b.Stats().SideCommands; n != lastSide {
			if sideFrames > 0 && frame-sideFrames < SideInterval/session.FrameMsec {
				t.Errorf("side commands %d frames apart", frame-sideFrames)
			}
			lastSide, sideFrames = n, frame
			maxSide = n
		}
	}
	// a few seconds more for the replies to the side commands
	for frame := 0; frame < 50 && !(b.Belief().Inventory.Known && c.Counts.Layouts > 0); frame++ {
		if err := l.Step(ctx, b.Cmd); err != nil {
			t.Fatal(err)
		}
		b.Observe(c, l.GameTimeMs())
		maxSide = b.Stats().SideCommands
	}
	st := b.Route().Current()
	if !b.Route().Done() {
		t.Fatalf("not done after %.0fs: %+v\n%s", float64(l.GameTimeMs())/1000, st, b.Describe())
	}
	t.Logf("killed in %.1fs game time (%s), %d fire commands, %d side command pairs, %d weapon switches",
		float64(st.Done-st.Started)/1000, st.Reason, b.Stats().FireCmds, maxSide, b.Stats().Switches)
	if !fought || b.Stats().FireCmds == 0 {
		t.Errorf("fought %v, fire commands %d", fought, b.Stats().FireCmds)
	}
	// the help computer's layout arrives and parses (its "%3i" counters
	// are formatted as in C)
	bel := b.Belief()
	if !bel.Inventory.Known || c.Counts.Layouts == 0 || maxSide < 2 {
		t.Errorf("inventory known %v, %d layouts, %d side pairs", bel.Inventory.Known, c.Counts.Layouts, maxSide)
	}
	if !bel.HelpKnown || bel.Help.KillsMax == 0 {
		t.Errorf("help computer known %v, %+v", bel.HelpKnown, bel.Help)
	}
	if tr := bel.TrackByLump(soldier); tr == nil || tr.Life == worldmodel.LifeAlive {
		t.Errorf("the soldier's track: %+v", tr)
	}
	if b.Intent().Mode != decide.ModeObjective {
		t.Errorf("phase-4 policy mode %s", b.Intent().Mode)
	}
}

// *decide.Pipeline is the wave-5 policy.
var _ Policy = (*decide.Pipeline)(nil)

type policyFunc func(now int64, b *worldmodel.Belief, obj *decide.ObjectiveView) decide.Intent

func (f policyFunc) Tick(now int64, b *worldmodel.Belief, obj *decide.ObjectiveView) decide.Intent {
	return f(now, b, obj)
}

// TestPolicyModes: the policy's mode decides what the bot does: fight a
// live target (the route waits), pick up an item, else the objective;
// the route's objective is what the policy sees.
func TestPolicyModes(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	md, g := demoLevel(t, fs, "demo1")
	intent := decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned}
	var seen *decide.ObjectiveView
	b := New(Config{Policy: policyFunc(func(now int64, _ *worldmodel.Belief, obj *decide.ObjectiveView) decide.Intent {
		seen = obj
		return intent
	})})
	tab := &route.Table{Schema: route.SchemaVersion, Name: "t", Map: "demo1", Exit: route.ExitRef{Map: "demo2$base1"},
		Steps: []route.Step{{Op: route.OpGoto, Pos: &route.Vec{128, -320, 24}}}}
	c := fakeclient.NewPassive(fakeclient.Options{})
	if err := b.Enter(Level{Key: worldmodel.LevelKey{Map: "demo1"}, Map: md, Graph: g, Route: tab}); err != nil {
		t.Fatal(err)
	}
	b.Route().Start(0)
	bel := &worldmodel.Belief{Tracks: []worldmodel.Track{{ID: "e1", Life: worldmodel.LifeAlive, Pos: Vec3{0, 0, 24}, PosKnown: true,
		Loc: Vec3{0, 0, 24}, LocKnown: true, LocSeen: true}},
		Items: []worldmodel.Item{{ID: "i1", Lump: 155, Pos: md.Entity(155).Origin, Life: worldmodel.LifeAlive}}}
	bel.Self.Health, bel.Self.Weapon = 100, "Blaster"
	c.Netchan.Message.SZ_Init(make([]byte, 4096)) // a passive client has no outgoing buffer
	b.now = 100
	b.decide(c, bel)
	if b.Mode() != ModeFight || b.Target() != "e1" {
		t.Fatalf("fight: mode %s target %q", b.Mode(), b.Target())
	}
	if seen == nil || seen.Kind != "goto" {
		t.Errorf("the policy saw objective %+v", seen)
	}
	if _, has := b.Navigator().Goal(); has {
		t.Error("the navigator has a goal while fighting")
	}
	// the target is dead: back to the objective, whose goal is set again
	bel.Tracks[0].Life = worldmodel.LifeDead
	b.decide(c, bel)
	if gl, has := b.Navigator().Goal(); b.Mode() != ModeObjective || !has || gl.Kind != navrt.GoalPoint {
		t.Fatalf("objective: mode %s goal %v %v", b.Mode(), gl, has)
	}
	// pick up an item the policy names
	intent = decide.Intent{Mode: decide.ModePickup, Pickup: "i1"}
	b.decide(c, bel)
	if gl, has := b.Navigator().Goal(); b.Mode() != ModePickup || !has || gl.Kind != navrt.GoalItem || gl.Entity != 155 {
		t.Fatalf("pickup: mode %s goal %v %v", b.Mode(), gl, has)
	}
}
