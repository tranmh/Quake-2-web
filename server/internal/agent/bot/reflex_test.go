package bot

import (
	"math"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Spots on demo1 (nav nodes; skill 1): open floor all around, a wall 16
// units to the +y side (open to -y), and a corridor with walls 16 units
// away on both x sides (open along y).
var (
	openSpot     = Vec3{864, 64, -167.875}
	wallSpot     = Vec3{768, 160, -167.875}
	corridorSpot = Vec3{352, 1024, -231.875}
)

// cmdBot is a modeBot whose usercmds the test builds: the client's frame
// stands the player where the belief does, every intent the driver
// composes is recorded, and the line of fire is open (no occlusion).
type cmdBot struct {
	*modeBot
	bel     *worldmodel.Belief
	intents []control.MoveIntent
}

func newCmdBot(t *testing.T, at Vec3) *cmdBot {
	t.Helper()
	m := &cmdBot{modeBot: newModeBot(t, "demo1"), bel: selfAt(at, 100)}
	m.testVision = perception.NewVision(nil, perception.ViewOptions{NoOcclusion: true})
	return m
}

// frame folds a frame at now: the client's player state at the belief's
// origin and view, then the bot's decisions on the belief.
func (m *cmdBot) frame(now int64) {
	s := &m.bel.Self
	f := &m.c.Frame
	f.Valid = true
	f.ServerFrame++
	ps := &f.PlayerState
	ps.PMove.PmType = q2const.PM_NORMAL
	ps.PMove.PmFlags = q2const.PMF_ON_GROUND
	ps.PMove.Gravity = 800
	for i := 0; i < 3; i++ {
		ps.PMove.Origin[i] = int16(s.Origin[i] * 8)
	}
	ps.ViewOffset = Vec3{0, 0, 22}
	ps.ViewAngles = s.ViewAngles
	m.at(now, m.bel)
	m.drv.Observe(m.bel, now)
	m.drv.OnCmd = func(in control.MoveIntent, _ shared.UserCmd, _ *navsim.State) { m.intents = append(m.intents, in) }
}

// cmd builds one 25 ms command.
func (m *cmdBot) cmd() shared.UserCmd { return m.Cmd(m.c, navrt.CmdMsec) }

func (m *cmdBot) reflexed(name string) bool {
	for _, r := range m.fight.reflexes {
		if r == name {
			return true
		}
	}
	return false
}

func neutralAt(id string, p Vec3) worldmodel.Track {
	return worldmodel.Track{ID: id, Kind: "neutral", Class: "insane", Life: worldmodel.LifeAlive, Pos: p, PosKnown: true, Visible: true}
}

// at returns o moved d units along yaw (degrees) horizontally.
func along(o Vec3, yaw, d float32) Vec3 {
	s, c := math.Sincos(float64(yaw) * math.Pi / 180)
	return Vec3{o[0] + float32(c)*d, o[1] + float32(s)*d, o[2]}
}

// TestCmdFireGate: the bot's commands carry the fire gate's verdict. A
// target in view on the line fires; a neutral on the line holds the
// trigger (hold_fire:neutral); and so does a neutral off the line to the
// target but on the line of the shot actually taken, when suppressive fire
// goes out at the edge of its tolerance (the view still slewing onto the
// target).
func TestCmdFireGate(t *testing.T) {
	target := along(openSpot, 0, 300)
	type setup struct {
		name      string
		policy    decide.FirePolicy
		viewYaw   float32 // the shooter's view before the command
		neutral   *Vec3
		fire      bool
		reflex    string
		converged bool // several commands: the view settles on the target
	}
	// one command closes 1-exp(-25/60) of a 13 degree error: the shot goes
	// out 8.6 degrees off, within suppress's tolerance at 300 units
	offRay := along(Vec3{openSpot[0], openSpot[1], openSpot[2]}, 13*float32(math.Exp(-25.0/60)), 250)
	onLine := along(openSpot, 0, 150)
	for _, tc := range []setup{
		{name: "aligned, clear line", policy: decide.FireWhenAligned, fire: true, converged: true},
		{name: "aligned, neutral on the line", policy: decide.FireWhenAligned, neutral: &onLine, reflex: "hold_fire:neutral", converged: true},
		{name: "suppress at the tolerance's edge, clear", policy: decide.FireSuppress, viewYaw: 13, fire: true},
		{name: "suppress at the tolerance's edge, neutral on the shot's line", policy: decide.FireSuppress, viewYaw: 13, neutral: &offRay,
			reflex: "hold_fire:neutral"},
		{name: "hold", policy: decide.FireHold, converged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newCmdBot(t, openSpot)
			m.bel.Tracks = []worldmodel.Track{monster("e1", target)}
			if tc.neutral != nil {
				m.bel.Tracks = append(m.bel.Tracks, neutralAt("n1", *tc.neutral))
			}
			*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: tc.policy, Movement: decide.MoveHold}
			m.frame(100)
			if m.Mode() != ModeFight {
				t.Fatalf("mode %s", m.Mode())
			}
			eye := Vec3{openSpot[0], openSpot[1], openSpot[2] + 22}
			_, pitch, _ := control.LookAt(eye, Vec3{target[0], target[1], target[2] + 4})
			m.shoot.SetView(tc.viewYaw, pitch)
			n := 1
			if tc.converged {
				n = 4
			}
			var fired bool
			for i := 0; i < n; i++ {
				fired = m.cmd().Buttons&q2const.BUTTON_ATTACK != 0
			}
			if fired != tc.fire {
				t.Errorf("fired %v, want %v (reflexes %v)", fired, tc.fire, m.fight.reflexes)
			}
			if tc.reflex != "" && !m.reflexed(tc.reflex) {
				t.Errorf("reflexes %v, want %s", m.fight.reflexes, tc.reflex)
			}
		})
	}
}

// TestCmdDodge: a projectile that will pass 10 units from the bot in 0.2 s
// makes the next command run along its dodge direction (reflex dodge),
// whatever the fight's own movement.
func TestCmdDodge(t *testing.T) {
	m := newCmdBot(t, openSpot)
	m.bel.Tracks = []worldmodel.Track{monster("e1", along(openSpot, 0, 400))}
	m.bel.Projectiles = []worldmodel.Projectile{{ID: "p1", Class: "laser", Pos: along(openSpot, 0, 200), TCA: 0.2, Miss: 10,
		DodgeDir: Vec3{0, -1, 0}}}
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveHold}
	m.frame(100)
	m.cmd()
	in := m.intents[len(m.intents)-1]
	if in.WishDir != (Vec3{0, -1, 0}) || in.Speed <= 0 || !m.reflexed("dodge") {
		t.Fatalf("intent %+v, reflexes %v: no dodge", in, m.fight.reflexes)
	}
	// once the projectile is past: the fight's hold again
	m.bel.Projectiles = nil
	m.frame(100 + control.DodgeHold + 100)
	m.cmd()
	if in := m.intents[len(m.intents)-1]; in.Speed != 0 {
		t.Errorf("still running after the dodge: %+v", in)
	}
}

// TestCmdStrafe: a strafe towards a wall flips to the open side; with
// walls on both sides the bot holds and notes the block (the reposition
// watchdog's input).
func TestCmdStrafe(t *testing.T) {
	// the target to -x: strafe right runs +y (the wall), left -y
	m := newCmdBot(t, wallSpot)
	m.bel.Tracks = []worldmodel.Track{monster("e1", along(wallSpot, 180, 300))}
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireHold, Movement: decide.MoveStrafe}
	for _, now := range []int64{100, 200, 1500, 2600} {
		// whatever side the rhythm is on, the open side is taken
		m.frame(now)
		m.cmd()
		in := m.intents[len(m.intents)-1]
		if in.Speed <= 0 || in.WishDir[1] > -0.99 || m.fight.strafe.Current() != -1 {
			t.Fatalf("strafe at %d ms: intent %+v side %d, want the open side", now, in, m.fight.strafe.Current())
		}
	}

	// the target down the corridor (-y): both strafe sides are walls
	m = newCmdBot(t, corridorSpot)
	m.bel.Tracks = []worldmodel.Track{monster("e1", along(corridorSpot, 270, 300))}
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireHold, Movement: decide.MoveStrafe}
	m.frame(100)
	m.cmd()
	if in := m.intents[len(m.intents)-1]; in.Speed != 0 || m.fight.blockedAt == 0 {
		t.Fatalf("strafe between two walls: intent %+v, blocked at %d", in, m.fight.blockedAt)
	}
	// standing so for repositionAfter: the bot backs off, and keeps
	// backing off (the intent still says strafe) until it gets there
	start := -1
	for now := int64(200); now <= 200+repositionAfter+1000; now += 100 {
		m.frame(now)
		m.cmd()
		if start < 0 && m.ticks[len(m.ticks)-1].Tick != nil {
			for _, r := range m.ticks[len(m.ticks)-1].Tick.Reflexes {
				if r == "reposition" {
					start = len(m.ticks) - 1
				}
			}
		}
	}
	if start < 0 || m.fight.move != control.MoveRetreat || m.Movement() != control.MoveRetreat {
		t.Fatalf("no reposition after %d ms blocked: move %s", repositionAfter, m.fight.move)
	}
	for _, d := range m.ticks[start:] {
		f := d.Intent.Fields[3]
		if d.Tick.Move != "nav" || f.Name != "movement" || f.Value != "retreat" || f.Source != trace.SourceReflex || f.Fallback != "reposition" {
			t.Fatalf("tick %d of the reposition: move %q, movement field %+v", d.Tick.N, d.Tick.Move, f)
		}
	}
}

// TestCmdNavFireGate: the trigger the navigator pulls itself (a route
// shoot goal faces its button and fires) still goes through the fire
// gate's vetoes: rockets at a goal 64 units away never fire, and the
// weapon reflex switches away from them.
func TestCmdNavFireGate(t *testing.T) {
	m := newCmdBot(t, openSpot)
	goal := along(openSpot, 0, 64)
	if err := m.nav.SetGoal(navrt.ShootGoal(999, goal), 0); err != nil {
		t.Skipf("no shoot goal at %v: %v", goal, err)
	}
	m.bel.Self.Weapon, m.bel.Self.Ammo = "Rocket Launcher", 5
	m.bel.Inventory.Known = true
	m.bel.Inventory.Items = []worldmodel.InvItem{{Index: 1, Name: "Rocket Launcher", Count: 1}, {Index: 2, Name: "Rockets", Count: 5},
		{Index: 3, Name: "Blaster", Count: 1}}
	*m.intent = decide.Intent{Mode: decide.ModeObjective}
	m.frame(100)
	eye := Vec3{openSpot[0], openSpot[1], openSpot[2] + 22}
	y, p, _ := control.LookAt(eye, goal)
	m.drv.Move = func(control.MoveIntent, *navsim.State) control.MoveIntent {
		in := control.Idle(y, p)
		in.MustFace, in.Fire = true, true
		return in
	}
	if u := m.cmd(); u.Buttons&q2const.BUTTON_ATTACK != 0 || !m.reflexed("hold_fire:splash_close") {
		t.Fatalf("rockets at a shoot goal 64 units away: buttons %#x, reflexes %v", u.Buttons, m.fight.reflexes)
	}
	if m.weaponBy != "splash" || m.switchCmd != "use Blaster" {
		t.Errorf("weapon reflex %q, switch %q", m.weaponBy, m.switchCmd)
	}
	// with the blaster in hand the same shot goes out
	m.bel.Self.Weapon = "Blaster"
	m.frame(200)
	if u := m.cmd(); u.Buttons&q2const.BUTTON_ATTACK == 0 {
		t.Errorf("the blaster at the shoot goal did not fire (reflexes %v)", m.fight.reflexes)
	}
}

// TestKeepOff: in a fight the bot backs away at once from a drain or melee
// monster in view within its reach, whatever the intent's movement (reflex
// keep_off), and does not advance on an attacker in view under lowHealth
// (low_health); farther off, or healthier, the intent's movement stands.
func TestKeepOff(t *testing.T) {
	mon := func(id, class string, d float32) worldmodel.Track {
		tr := monster(id, along(openSpot, 0, d))
		tr.Class = class
		return tr
	}
	run := func(health int, intent decide.Movement, tracks ...worldmodel.Track) (*cmdBot, trace.Field) {
		m := newCmdBot(t, openSpot)
		m.bel.Self.Health = health
		m.bel.Tracks = tracks
		*m.intent = decide.Intent{Mode: decide.ModeFight, Target: tracks[0].ID, FirePolicy: decide.FireWhenAligned, Movement: intent}
		m.frame(100)
		m.cmd()
		return m, m.field(t, "movement")
	}
	for _, tc := range []struct {
		name   string
		health int
		intent decide.Movement
		tracks []worldmodel.Track
		move   control.Move
		reflex string
	}{
		{"soldier", 100, decide.MoveStrafe, []worldmodel.Track{mon("e1", "soldier", 200)}, control.MoveStrafe, ""},
		{"parasite near", 100, decide.MoveStrafe, []worldmodel.Track{mon("e1", "parasite", 200)}, control.MoveRetreat, "keep_off"},
		{"parasite far", 100, decide.MoveStrafe, []worldmodel.Track{mon("e1", "parasite", 400)}, control.MoveStrafe, ""},
		{"berserk near", 100, decide.MoveAdvance, []worldmodel.Track{mon("e1", "soldier", 500), mon("e2", "berserk", 120)}, control.MoveRetreat, "keep_off"},
		{"berserk far", 100, decide.MoveStrafe, []worldmodel.Track{mon("e1", "berserk", 250)}, control.MoveStrafe, ""},
		{"low health", 20, decide.MoveAdvance, []worldmodel.Track{mon("e1", "soldier", 500)}, control.MoveStrafe, "low_health"},
		{"healthy", 60, decide.MoveAdvance, []worldmodel.Track{mon("e1", "soldier", 500)}, control.MoveAdvance, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := run(tc.health, tc.intent, tc.tracks...)
			if move := m.fight.move; move != tc.move {
				t.Fatalf("move %s, want %s", m.fight.move, tc.move)
			}
			if tc.reflex != "" && (f.Source != trace.SourceReflex || f.Fallback != tc.reflex) || tc.reflex == "" && f.Source == trace.SourceReflex {
				t.Fatalf("movement field %+v, want reflex %q", f, tc.reflex)
			}
		})
	}
}
