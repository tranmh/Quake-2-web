package bot

import (
	"bytes"
	"testing"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
)

// modeBot is a bot entered on demo map name (pak and nav cache needed)
// with route steps steps, its policy answering *intent, and a passive
// client to queue side commands on. Its decide is called directly on
// beliefs the tests make.
type modeBot struct {
	*Bot
	c      *fakeclient.Client
	intent *decide.Intent
	ticks  []trace.Decision
}

func newModeBot(t *testing.T, name string, steps ...route.Step) *modeBot {
	t.Helper()
	fs := sessiontest.DemoFS(t)
	md, g := demoLevel(t, fs, name)
	m := &modeBot{intent: &decide.Intent{Mode: decide.ModeObjective}}
	m.Bot = New(Config{Policy: policyFunc(func(int64, *worldmodel.Belief, *decide.ObjectiveView) decide.Intent { return *m.intent }),
		OnDecision: func(d *trace.Decision) { m.ticks = append(m.ticks, *d) }})
	var tab *route.Table
	if len(steps) > 0 {
		tab = &route.Table{Schema: route.SchemaVersion, Name: "t", Map: name, Exit: route.ExitRef{Map: "demo2$base1"}, Steps: steps}
	}
	if err := m.Enter(Level{Key: worldmodel.LevelKey{Map: name}, Map: md, Graph: g, Route: tab}); err != nil {
		t.Fatal(err)
	}
	if m.Route() != nil {
		m.Route().Start(0)
	}
	m.c = fakeclient.NewPassive(fakeclient.Options{})
	m.c.Netchan.Message.SZ_Init(make([]byte, 4096))
	return m
}

// at runs one decision tick at now on bel.
func (m *modeBot) at(now int64, bel *worldmodel.Belief) {
	m.now = now
	bel.Time = now
	m.decide(m.c, bel)
}

// sent reports whether a string command containing s went out.
func (m *modeBot) sent(s string) bool { return bytes.Contains(m.c.Netchan.Message.Bytes(), []byte(s)) }

func monster(id string, p Vec3) worldmodel.Track {
	return worldmodel.Track{ID: id, Kind: "monster", Class: "soldier", Life: worldmodel.LifeAlive, Pos: p, PosKnown: true,
		Visible: true, Shootable: true, Awareness: worldmodel.Attacking, Threat: 1}
}

func selfAt(p Vec3, health int) *worldmodel.Belief {
	b := &worldmodel.Belief{}
	b.Self.Origin, b.Self.Eye = p, Vec3{p[0], p[1], p[2] + 22}
	b.Self.Health, b.Self.Weapon, b.Self.OnGround = health, "Blaster", true
	return b
}

// TestModeTransitions: the bot's mode follows the intent's where it can
// act on it: fight a live target, retreat from a known threat, pick up a
// listed item; else the objective. The campaign's explore burst gives way
// to a fight; past the fight budget the bot disengages from a target.
func TestModeTransitions(t *testing.T) {
	m := newModeBot(t, "demo1", route.Step{Op: route.OpGoto, Pos: &route.Vec{128, -320, 24}})
	start := Vec3{0, 0, 24}
	bel := selfAt(start, 100)
	bel.Tracks = []worldmodel.Track{monster("e1", Vec3{300, 0, 24})}

	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveStrafeLeft}
	m.at(100, bel)
	if m.Mode() != ModeFight || m.Target() != "e1" {
		t.Fatalf("fight: mode %s target %q", m.Mode(), m.Target())
	}
	// a fight on a target the bot does not know: the objective
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e9"}
	m.at(200, bel)
	if m.Mode() != ModeObjective {
		t.Fatalf("fight on an unknown target: mode %s", m.Mode())
	}
	// retreat from the threat
	*m.intent = decide.Intent{Mode: decide.ModeRetreat, Target: "e1", FirePolicy: decide.FireWhenAligned}
	m.at(300, bel)
	if m.Mode() != ModeRetreat {
		t.Fatalf("retreat: mode %s", m.Mode())
	}
	// no threat known: nothing to retreat from
	bel.Tracks[0].Life = worldmodel.LifeDead
	m.at(400, bel)
	if m.Mode() != ModeObjective {
		t.Fatalf("retreat without a threat: mode %s", m.Mode())
	}
	bel.Tracks[0].Life = worldmodel.LifeAlive

	// the campaign's explore burst: the objective waits, a fight does not
	m.Explore(5000)
	*m.intent = decide.Intent{Mode: decide.ModeObjective}
	m.at(500, bel)
	if m.Mode() != ModeExplore {
		t.Fatalf("explore: mode %s", m.Mode())
	}
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned}
	m.at(600, bel)
	if m.Mode() != ModeFight {
		t.Fatalf("fight while exploring: mode %s", m.Mode())
	}
	*m.intent = decide.Intent{Mode: decide.ModeObjective}
	m.at(5000, bel) // the burst is over: the step starts over
	m.at(5100, bel)
	if m.Mode() != ModeObjective {
		t.Fatalf("after the explore burst: mode %s", m.Mode())
	}

	// past fightBudget on one target: disengaged for disengageFor
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveHold}
	now := int64(10000)
	for ; now <= 10000+fightBudget+200; now += 100 {
		m.at(now, bel)
	}
	if m.Mode() != ModeObjective || !m.disengaged("e1") {
		t.Fatalf("after %d ms on e1: mode %s, disengaged %v", fightBudget, m.Mode(), m.disengaged("e1"))
	}
	m.at(now+disengageFor, bel)
	if m.Mode() != ModeFight {
		t.Fatalf("after the disengagement: mode %s", m.Mode())
	}
}

// TestPickupGiveUp: an item the bot gets no closer to is given up on
// (badItemFor): the policy no longer gets a path to it and the bot goes
// back to the objective.
func TestPickupGiveUp(t *testing.T) {
	m := newModeBot(t, "demo1", route.Step{Op: route.OpGoto, Pos: &route.Vec{128, -320, 24}})
	md := m.Level().Map
	var it mapdata.Item
	for _, x := range md.Items {
		if x.Entity == 155 { // the item TestPolicyModes picks up
			it = x
		}
	}
	if it.Entity != 155 {
		t.Fatal("no item #155 on demo1")
	}
	bel := selfAt(Vec3{0, 0, 24}, 100)
	bel.Items = []worldmodel.Item{{ID: "i1", Lump: it.Entity, Class: it.Classname, Pos: it.Origin, Life: worldmodel.LifeAlive}}
	*m.intent = decide.Intent{Mode: decide.ModePickup, Pickup: "i1"}
	m.at(100, bel)
	if gl, has := m.Navigator().Goal(); m.Mode() != ModePickup || !has || gl.Kind != navrt.GoalItem {
		t.Fatalf("pickup: mode %s goal %v %v", m.Mode(), gl, has)
	}
	if _, ok := m.itemPath(bel.Self.Origin, it.Origin); !ok && m.Navigator().CanReturn(bel.Self.Origin, it.Origin) {
		t.Fatal("no path to the item before giving up")
	}
	now := int64(200)
	for ; now < 100+pickupStall+300; now += 100 { // the bot does not move
		m.at(now, bel)
	}
	if !m.badItem(it.Origin) {
		t.Fatalf("not given up after %d ms without progress", now-100)
	}
	m.at(now, bel)
	if m.Mode() != ModeObjective {
		t.Errorf("after giving up: mode %s", m.Mode())
	}
	if _, ok := m.itemPath(bel.Self.Origin, it.Origin); ok {
		t.Error("the policy still gets a path to an item given up on")
	}
	if !m.badItem(it.Origin) || m.now+badItemFor <= now {
		t.Error("the give-up does not last")
	}
}

// TestRouteKillForcesFight: once the route's kill step names its monster's
// track, the bot fights it even when the intent says objective, with the
// fire policy forced to fire_when_aligned; the tick event marks both as
// reflex overrides ("route_kill").
func TestRouteKillForcesFight(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	md, _ := demoLevel(t, fs, "demo1")
	if len(md.Monsters) == 0 {
		t.Skip("no monster on demo1")
	}
	mon := md.Monsters[0]
	lump := mon.Entity
	m := newModeBot(t, "demo1", route.Step{Op: route.OpKill, Class: mon.Classname, Pos: &route.Vec{mon.Origin[0], mon.Origin[1], mon.Origin[2]},
		Target: &route.Ref{Entity: &lump}})
	p := Vec3{mon.Origin[0] - 200, mon.Origin[1], mon.Origin[2]}
	bel := selfAt(p, 100)
	tr := monster("e3", mon.Origin)
	tr.Lump, tr.Class, tr.LastSeen = lump, "", 50
	bel.Tracks = []worldmodel.Track{tr}
	*m.intent = decide.Intent{Mode: decide.ModeObjective, FirePolicy: decide.FireHold}
	m.at(100, bel) // the executor names the track
	m.at(200, bel)
	if m.Mode() != ModeFight || m.Target() != "e3" {
		t.Fatalf("route kill: mode %s target %q", m.Mode(), m.Target())
	}
	if m.firePolicy != decide.FireWhenAligned || m.fireBy != "route_kill" || m.targetBy != "route_kill" {
		t.Errorf("fire policy %s (by %q), target by %q", m.firePolicy, m.fireBy, m.targetBy)
	}
	last := m.ticks[len(m.ticks)-1]
	if last.Lane != trace.LaneTick || last.Intent == nil || last.Tick == nil || last.Tick.Mode != string(ModeFight) {
		t.Fatalf("tick event %+v", last)
	}
	for _, f := range last.Intent.Fields {
		switch f.Name {
		case "target":
			if f.Value != "e3" || f.Source != trace.SourceReflex || f.Fallback != "route_kill" {
				t.Errorf("target field %+v", f)
			}
		case "fire_policy":
			if f.Value != string(decide.FireWhenAligned) || f.Source != trace.SourceReflex {
				t.Errorf("fire_policy field %+v", f)
			}
		case "mode":
			if f.Value != string(decide.ModeFight) || f.Source != trace.SourceReflex || f.Fallback != "route_kill" {
				t.Errorf("mode field %+v", f)
			}
		}
	}
	// the route kill fight is the step's own work: its clock runs on
	// (Engage, not Yield), as the executor sees once the monster is out of
	// view and the bot is back on the step
	for now := int64(300); now <= 1000; now += 100 {
		m.at(now, bel)
	}
	bel.Tracks[0].Visible, bel.Tracks[0].LastSeen = false, 1000
	m.at(1000+routeKillMemory+200, bel)
	if m.Mode() != ModeObjective {
		t.Fatalf("monster out of view: mode %s", m.Mode())
	}
	if y := m.Route().Yielded(); y != 0 {
		t.Errorf("the route kill fight paused the step for %d ms", y)
	}
}

// TestWeaponTick: the intent's weapon is switched to with a "use" (once
// per debounce); a rocket's cycle is never interrupted; a dry weapon and a
// splash weapon at a close target are reflexes that override the intent.
func TestWeaponTick(t *testing.T) {
	newBot := func() (*Bot, *fakeclient.Client) {
		b := New(Config{})
		c := fakeclient.NewPassive(fakeclient.Options{})
		c.Netchan.Message.SZ_Init(make([]byte, 4096))
		return b, c
	}
	inv := map[string]int{"Blaster": 1, "Shotgun": 1, "Shells": 20, "Machinegun": 1, "Bullets": 50, "Rocket Launcher": 1, "Rockets": 5}

	b, c := newBot()
	bel := belief("Blaster", 0, inv)
	b.intent = decide.Intent{Weapon: decide.WeaponShotgun}
	b.now = 1000
	b.weaponTick(c, bel)
	if b.switchCmd != "use Shotgun" || b.weaponBy != "" {
		t.Fatalf("intent shotgun: %q (by %q)", b.switchCmd, b.weaponBy)
	}
	b.switchCmd = ""
	b.now += 100
	b.weaponTick(c, bel)
	if b.switchCmd != "" {
		t.Errorf("resent within the debounce: %q", b.switchCmd)
	}

	// a rocket just fired: no switch until its cycle is over
	b, c = newBot()
	bel = belief("Rocket Launcher", 5, inv)
	b.intent = decide.Intent{Weapon: decide.WeaponMachinegun}
	b.now = 1000
	b.shoot.NoteFire(900)
	b.weaponTick(c, bel)
	if b.switchCmd != "" {
		t.Fatalf("switched in the rocket's cycle: %q", b.switchCmd)
	}
	b.now = 2000
	b.weaponTick(c, bel)
	if b.switchCmd != "use Machinegun" {
		t.Fatalf("after the cycle: %q", b.switchCmd)
	}

	// the weapon in hand ran dry: the best usable one (a reflex)
	b, c = newBot()
	bel = belief("Shotgun", 0, map[string]int{"Shotgun": 1, "Machinegun": 1, "Bullets": 50})
	b.intent = decide.Intent{Weapon: decide.WeaponKeep}
	b.now = 1000
	b.weaponTick(c, bel)
	if b.weaponBy != "dry" || b.switchCmd != "use Machinegun" {
		t.Fatalf("dry shotgun: %q (by %q)", b.switchCmd, b.weaponBy)
	}

	// rockets at a target 100 units away: switched off (a reflex), and an
	// intent asking for them there is not followed
	b, c = newBot()
	bel = belief("Rocket Launcher", 5, inv)
	bel.Self.Eye = Vec3{0, 0, 22}
	bel.Tracks = []worldmodel.Track{monster("e1", Vec3{100, 0, 22})}
	b.target = "e1"
	b.intent = decide.Intent{Weapon: decide.WeaponRocketLauncher}
	b.now = 1000
	b.weaponTick(c, bel)
	if b.weaponBy != "splash" || b.switchCmd == "" || b.switchCmd == "use Rocket Launcher" {
		t.Fatalf("rockets point blank: %q (by %q)", b.switchCmd, b.weaponBy)
	}
	b, c = newBot()
	bel.Self.Weapon, bel.Self.Ammo = "Machinegun", 50
	b.target = "e1"
	b.intent = decide.Intent{Weapon: decide.WeaponRocketLauncher}
	b.now = 1000
	b.weaponTick(c, bel)
	if b.switchCmd != "" || b.weaponBy != "splash" || b.weaponTo != decide.WeaponKeep {
		t.Errorf("switched to rockets at a close target: %q (by %q, to %q)", b.switchCmd, b.weaponBy, b.weaponTo)
	}
	if !bytes.Contains(c.Netchan.Message.Bytes(), []byte("use")) == (b.switchCmd != "") {
		t.Error("the client's outgoing commands disagree with the switch")
	}
}

// TestTrapKill: down a pit the route's objective cannot be reached from
// (demo3's pit under the ledge at 928,-896: 61 nav nodes, no way out), the
// bot types "kill" after trapFor, and again only after trapRetry; on the
// level's floor it never does.
func TestTrapKill(t *testing.T) {
	m := newModeBot(t, "demo3", route.Step{Op: route.OpGoto, Pos: &route.Vec{-536, -472, -272}})
	floor := selfAt(Vec3{1504, 1408, -808}, 100)
	for now := int64(100); now < 2*trapFor; now += 100 {
		m.at(now, floor)
	}
	if m.Stats().TrapKills != 0 || m.sent("kill") {
		t.Fatal("kill on the level's floor")
	}
	pit := selfAt(Vec3{632, -884, -688}, 100)
	start := int64(100000)
	now := start
	for ; now < start+trapFor; now += 100 {
		m.at(now, pit)
	}
	if m.Stats().TrapKills != 0 {
		t.Fatalf("kill before trapFor (%d ms)", now-start)
	}
	m.at(now, pit)
	if m.Stats().TrapKills != 1 || !m.sent("kill") {
		t.Fatalf("no kill after %d ms in the pit", now-start)
	}
	for now += 100; now < start+trapFor+trapRetry; now += 100 {
		m.at(now, pit)
	}
	if m.Stats().TrapKills != 1 {
		t.Fatalf("kill resent within trapRetry: %d", m.Stats().TrapKills)
	}
	m.at(now, pit)
	if m.Stats().TrapKills != 2 {
		t.Fatalf("no kill after trapRetry: %d", m.Stats().TrapKills)
	}
}

// TestWedgeKill: standing on one spot while the navigator keeps trying to
// move the bot and its stuck recovery keeps failing, it types "kill"
// after wedgeFor; standing still on purpose (the navigator idle, or under
// way without stuck reports: waiting at a mover), moving on, or trying on
// only half of the frames never does.
func TestWedgeKill(t *testing.T) {
	spot := Vec3{1504, 1408, -808}
	type navFn func(now int64) (bool, int)
	var off func(now int64) bool // off the graph (nil: never)
	run := func(nav navFn, at func(now int64) Vec3, until int64) *modeBot {
		m := newModeBot(t, "demo3", route.Step{Op: route.OpGoto, Pos: &route.Vec{-536, -472, -272}})
		var now int64
		m.testNav = func() navEffort {
			under, n := nav(now)
			return navEffort{underWay: under, recoveries: n, offGraph: off != nil && under && off(now)}
		}
		for now = 100; now <= until; now += 100 {
			m.at(now, selfAt(at(now), 100))
		}
		return m
	}
	still := func(int64) Vec3 { return spot }
	// stuck every 2 s; a new goal every 10 s (its count starts over)
	stuck := func(now int64) (bool, int) { return true, int(now%10000) / 2000 }
	m := run(stuck, still, wedgeFor)
	if m.Stats().TrapKills != 0 || m.sent("kill") {
		t.Fatalf("kill before wedgeFor: %d", m.Stats().TrapKills)
	}
	m = run(stuck, still, wedgeFor+300)
	if m.Stats().TrapKills != 1 || !m.sent("kill") {
		t.Fatalf("no kill after wedgeFor: %d", m.Stats().TrapKills)
	}
	if m := run(func(int64) (bool, int) { return false, 0 }, still, 2*wedgeFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill while standing still on purpose")
	}
	if m := run(func(int64) (bool, int) { return true, 0 }, still, 2*wedgeFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill while under way without a stuck report")
	}
	if m := run(func(now int64) (bool, int) { _, n := stuck(now); return now%200 == 0, n }, still, 2*wedgeFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill while under way on half of the frames")
	}
	// moving 32 units every 10 s: never wedged
	moving := func(now int64) Vec3 { return Vec3{spot[0] + float32(32*(now/10000)), spot[1], spot[2]} }
	if m := run(stuck, moving, 2*wedgeFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill while moving")
	}
	// off the graph with a goal, no recoveries: wedged after offGraphFor;
	// off it on half of the frames, or moving, never
	under := func(int64) (bool, int) { return true, 0 }
	off = func(int64) bool { return true }
	if m := run(under, still, offGraphFor-500); m.Stats().TrapKills != 0 {
		t.Fatal("kill off the graph before offGraphFor")
	}
	if m := run(under, still, offGraphFor+300); m.Stats().TrapKills != 1 {
		t.Fatal("no kill off the graph after offGraphFor")
	}
	if m := run(under, moving, 2*offGraphFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill off the graph while moving")
	}
	off = func(now int64) bool { return now%200 == 0 }
	if m := run(under, still, 2*offGraphFor); m.Stats().TrapKills != 0 {
		t.Fatal("kill off the graph on half of the frames")
	}
}

// field returns the named field of the last tick event's Intent.
func (m *modeBot) field(t *testing.T, name string) trace.Field {
	t.Helper()
	if len(m.ticks) == 0 {
		t.Fatal("no tick event")
	}
	d := m.ticks[len(m.ticks)-1]
	if d.Intent == nil {
		t.Fatalf("tick event without an intent: %+v", d)
	}
	for _, f := range d.Intent.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %s in %+v", name, d.Intent.Fields)
	return trace.Field{}
}

// wantField checks the named field of the last tick: value, and the
// reflex reason ("" for the intent's own source).
func (m *modeBot) wantField(t *testing.T, name, value, reflex string) {
	t.Helper()
	f := m.field(t, name)
	if f.Value != value || (reflex != "") != (f.Source == trace.SourceReflex) || reflex != "" && f.Fallback != reflex {
		t.Errorf("%s field %+v, want value %q reflex %q", name, f, value, reflex)
	}
}

// TestTickProvenance: a tick event's fields report the value the bot acted
// on, and every override of the intent as a reflex with its reason: the
// campaign's explore burst, a retreat without a threat, a fight on a dead
// target or one disengaged from, a pickup of an item not there, a retreat
// shooting back at another monster, a weapon the bot cannot use.
func TestTickProvenance(t *testing.T) {
	m := newModeBot(t, "demo1", route.Step{Op: route.OpGoto, Pos: &route.Vec{128, -320, 24}})
	bel := selfAt(Vec3{0, 0, 24}, 100)
	bel.Tracks = []worldmodel.Track{monster("e1", Vec3{300, 0, 24}), monster("e2", Vec3{0, 300, 24})}
	scripted := decide.FieldProvenance{Source: decide.SourceScripted}
	in := func(i decide.Intent) {
		p := &i.Provenance
		p.Mode, p.Target, p.FirePolicy, p.Movement, p.Weapon, p.Pickup, p.Danger = scripted, scripted, scripted, scripted, scripted, scripted, scripted
		*m.intent = i
	}

	// acted on as decided: the backend's
	in(decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveHold})
	m.at(100, bel)
	m.wantField(t, "mode", "fight", "")
	m.wantField(t, "target", "e1", "")
	m.wantField(t, "movement", "hold", "")

	// a retreat with no threat known runs the objective
	in(decide.Intent{Mode: decide.ModeRetreat, FirePolicy: decide.FireWhenAligned})
	dead := *bel
	dead.Tracks = []worldmodel.Track{bel.Tracks[0], bel.Tracks[1]}
	dead.Tracks[0].Life, dead.Tracks[1].Life = worldmodel.LifeDead, worldmodel.LifeDead
	m.at(200, &dead)
	m.wantField(t, "mode", "objective", "no_threat")

	// a fight on a dead target: the objective, no target
	in(decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned})
	m.at(300, &dead)
	m.wantField(t, "mode", "objective", "target_gone")
	m.wantField(t, "target", "none", "target_gone")

	// a pickup of an item the bot does not know of
	in(decide.Intent{Mode: decide.ModePickup, Pickup: "i9"})
	m.at(400, bel)
	m.wantField(t, "mode", "objective", "item_unavailable")

	// a retreat from e2 while the intent targets e1: the bot shoots back
	// at the threat
	bel.Tracks[0].Threat, bel.Tracks[1].Threat = 0, 5
	in(decide.Intent{Mode: decide.ModeRetreat, Target: "e1", FirePolicy: decide.FireWhenAligned})
	m.at(500, bel)
	m.wantField(t, "mode", "retreat", "")
	m.wantField(t, "target", "e2", "retreat_threat")

	// an intent weapon the bot does not hold: kept
	bel.Inventory.Known = true
	bel.Inventory.Items = []worldmodel.InvItem{{Index: 1, Name: "Blaster", Count: 1}}
	in(decide.Intent{Mode: decide.ModeObjective, Weapon: decide.WeaponRailgun})
	m.at(600, bel)
	m.wantField(t, "weapon", "keep", "unusable")
	m.wantField(t, "mode", "objective", "")

	// the campaign's explore burst: the objective waits
	in(decide.Intent{Mode: decide.ModeObjective})
	m.Explore(5000)
	m.at(700, bel)
	m.wantField(t, "mode", "explore", "explore_watchdog")
	m.at(5000, bel) // the burst is over
	m.at(5100, bel)
	m.wantField(t, "mode", "objective", "")

	// past the fight budget: disengaged
	in(decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveHold})
	now := int64(10000)
	for ; now <= 10000+fightBudget+200; now += 100 {
		m.at(now, bel)
	}
	m.wantField(t, "mode", "objective", "disengaged")
	m.wantField(t, "target", "e1", "") // still shot at on the move when in view
}

// TestStallKill: a level attempt with no progress (no route step done nor
// its path shortened, no monster the bot fought killed) for stallFor is
// given up with "kill"; a kill of a monster it fought restarts the clock.
func TestStallKill(t *testing.T) {
	run := func(kill int64, until int64) *modeBot {
		m := newModeBot(t, "demo3", route.Step{Op: route.OpGoto, Pos: &route.Vec{-536, -472, -272}})
		m.testNav = func() navEffort { return navEffort{} } // standing on purpose: never wedged
		for now := int64(100); now <= until; now += 100 {
			bel := selfAt(Vec3{1504, 1408, -808}, 100)
			e1 := monster("e1", Vec3{1700, 1408, -808})
			if kill > 0 && now >= kill {
				e1.Life, e1.Visible = worldmodel.LifeDead, false
			}
			bel.Tracks = []worldmodel.Track{e1}
			*m.intent = decide.Intent{Mode: decide.ModeObjective}
			if now < 10000 {
				*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned}
			}
			m.at(now, bel)
		}
		return m
	}
	if m := run(0, stallFor-1000); m.Stats().TrapKills != 0 || m.sent("kill") {
		t.Fatalf("kill before stallFor: %d", m.Stats().TrapKills)
	}
	m := run(0, stallFor+1000)
	if m.Stats().TrapKills != 1 || !m.sent("kill") || m.field(t, "mode").Value == "" {
		t.Fatalf("no kill after stallFor: %d", m.Stats().TrapKills)
	}
	// the monster fought until 10 s dies at 60 s: the clock starts over
	if m := run(60000, 60000+stallFor-1000); m.Stats().TrapKills != 0 {
		t.Fatal("kill within stallFor of a kill")
	}
	if m := run(60000, 60000+stallFor+1000); m.Stats().TrapKills != 1 {
		t.Fatal("no kill stallFor after the last kill")
	}
}
