package bot

import (
	"math"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
)

// TestDrainKeepOff: a parasite out of view that was awake moments ago and
// is within its reach is backed away from while the bot takes hits it
// cannot place (no bearing: the drain does no knockback), not otherwise.
func TestDrainKeepOff(t *testing.T) {
	soldier := monster("e1", along(openSpot, 0, 400))
	parasite := monster("e2", along(openSpot, 180, 200))
	parasite.Class, parasite.Visible = "parasite", false
	for _, tc := range []struct {
		name    string
		hit     bool // a hit without a bearing 100 ms ago
		bearing bool // ... with a bearing instead
		age     int64
		want    control.Move
	}{
		{"drained", true, false, 100, control.MoveRetreat},
		{"no hit", false, false, 100, control.MoveStrafe},
		{"hit with a bearing", true, true, 100, control.MoveStrafe},
		{"parasite long unseen", true, false, drainMemory + 500, control.MoveStrafe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newCmdBot(t, openSpot)
			p := parasite
			p.LastUpdate = 1000 - tc.age
			m.bel.Tracks = []worldmodel.Track{soldier, p}
			m.bel.Time = 1000
			if tc.hit {
				m.bel.Damage = []worldmodel.DamageEvent{{At: 900, Health: 5, Cause: "hit", BearingKnown: tc.bearing}}
			}
			*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveStrafe}
			m.frame(1000)
			m.cmd()
			if m.fight.move != tc.want {
				t.Fatalf("move %s, want %s", m.fight.move, tc.want)
			}
			if f := m.field(t, "movement"); (tc.want == control.MoveRetreat) != (f.Source == trace.SourceReflex && f.Fallback == "keep_off") {
				t.Fatalf("movement field %+v", f)
			}
		})
	}
}

// TestScan: with no target, a hit without a bearing turns the bot towards
// the nearest awake monster it knows of out of view (reflex scan), else
// behind it, then to the sides; a scan under way is not restarted by the
// next hit.
func TestScan(t *testing.T) {
	m := newCmdBot(t, openSpot)
	m.bel.Time = 1000
	m.bel.Damage = []worldmodel.DamageEvent{{At: 1000, Health: 2, Cause: "hit"}}
	m.frame(1000)
	scanned := func() bool {
		for _, r := range m.ticks[len(m.ticks)-1].Tick.Reflexes {
			if r == "scan" {
				return true
			}
		}
		return false
	}
	if !scanned() || math.Abs(float64(angleDiff(m.searchYaw, 180))) > 1 || m.searchUntil != 1000+searchFor {
		t.Fatalf("no suspect: scan %v yaw %v until %d", scanned(), m.searchYaw, m.searchUntil)
	}
	m.cmd()
	if y := m.shoot.yaw; math.Abs(float64(angleDiff(y, 0))) < 1 {
		t.Fatalf("the view did not turn: %v", y)
	}
	// the next hit while scanning: the scan goes on
	m.bel.Time = 1100
	m.bel.Damage = append(m.bel.Damage, worldmodel.DamageEvent{At: 1100, Health: 2, Cause: "hit"})
	m.frame(1100)
	if m.searchUntil != 1000+searchFor {
		t.Fatalf("scan restarted: until %d", m.searchUntil)
	}
	// after it, a suspect heard to the left
	sus := monster("e3", along(openSpot, 90, 300))
	sus.Visible, sus.LastUpdate = false, 2000
	m.bel.Tracks = []worldmodel.Track{sus}
	m.bel.Time = 2400
	m.bel.Damage = append(m.bel.Damage, worldmodel.DamageEvent{At: 2400, Health: 2, Cause: "hit"})
	m.frame(2400)
	if math.Abs(float64(angleDiff(m.searchYaw, 90))) > 1 {
		t.Fatalf("suspect: yaw %v, want 90", m.searchYaw)
	}
}

// TestListen: with no target, on the move, the bot looks where an attacker
// only heard sounded from (its stand-in) when it attacked moments ago and
// sounded near or mid; not for one that sounded far or attacked long ago.
func TestListen(t *testing.T) {
	for _, tc := range []struct {
		name string
		loud perception.Loudness
		age  int64
		want bool
	}{
		{"mid", perception.LoudMid, 200, true},
		{"near", perception.LoudNear, 200, true},
		{"far", perception.LoudFar, 200, false},
		{"long ago", perception.LoudMid, listenFor + 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newCmdBot(t, openSpot)
			a := monster("e3", along(openSpot, 90, 600))
			a.Visible, a.Shootable, a.LocSeen, a.LastAttack = false, false, false, 1000-tc.age
			a.Ear.Loud = tc.loud
			m.bel.Tracks = []worldmodel.Track{a}
			m.bel.Time = 1000
			m.frame(1000)
			m.cmd()
			if got := math.Abs(float64(angleDiff(m.shoot.yaw, 0))) > 1; got != tc.want {
				t.Fatalf("view yaw %v: turned %v, want %v", m.shoot.yaw, got, tc.want)
			}
		})
	}
}

func angleDiff(a, b float32) float32 {
	d := math.Mod(float64(a-b), 360)
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	return float32(d)
}

// TestStrafeDodgeSide: a strafe takes the dodge side of a dangerous
// projectile passing soon, whatever side the rhythm is on.
func TestStrafeDodgeSide(t *testing.T) {
	target := along(openSpot, 0, 300) // +x: right is -y
	for _, side := range []int{1, -1} {
		m := newCmdBot(t, openSpot)
		m.bel.Tracks = []worldmodel.Track{monster("e1", target)}
		m.bel.Projectiles = []worldmodel.Projectile{{ID: "p1", Class: "rocket", Danger: true, TCA: 0.8, Miss: 10,
			DodgeDir: control.SideDir(side, openSpot, target)}}
		*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireHold, Movement: decide.MoveStrafe}
		m.frame(100)
		m.cmd()
		in := m.intents[len(m.intents)-1]
		if want := control.SideDir(side, openSpot, target); in.Speed <= 0 || in.WishDir[1]*want[1] < 0.9 || m.fight.strafe.Current() != side {
			t.Fatalf("dodge side %d: intent %+v side %d", side, in, m.fight.strafe.Current())
		}
	}
}

// TestRetargetOnTheMove: on the move without a live target, the bot
// shoots back at a monster in view that attacks it (reflex retarget), but
// not at an alert one.
func TestRetargetOnTheMove(t *testing.T) {
	m := newCmdBot(t, openSpot)
	a := monster("e1", along(openSpot, 0, 300))
	m.bel.Tracks = []worldmodel.Track{a}
	*m.intent = decide.Intent{Mode: decide.ModeObjective, FirePolicy: decide.FireHold}
	m.frame(100)
	if m.Target() != "e1" || m.targetBy != "retarget" || m.firePolicy != decide.FireWhenAligned {
		t.Fatalf("target %q by %q fire %s", m.Target(), m.targetBy, m.firePolicy)
	}
	if u := m.cmd(); u.Buttons&q2const.BUTTON_ATTACK == 0 && m.Mode() != ModeObjective {
		t.Fatalf("mode %s", m.Mode())
	}
	m.bel.Tracks[0].Awareness = worldmodel.Alert
	m.frame(200)
	if m.Target() != "" {
		t.Fatalf("shot at an alert monster on the move: %q", m.Target())
	}
}

// TestQuad: a quad damage picked up is used once the bot fights an awake
// monster in view (reflex quad), not before, and only once.
func TestQuad(t *testing.T) {
	m := newCmdBot(t, openSpot)
	m.bel.Self.Pickup, m.bel.Self.PickupAt = quadName, 50
	*m.intent = decide.Intent{Mode: decide.ModeObjective}
	m.frame(100)
	if !m.quadHeld {
		t.Fatal("the pickup was not noted")
	}
	m.bel.Tracks = []worldmodel.Track{monster("e1", along(openSpot, 0, 300))}
	*m.intent = decide.Intent{Mode: decide.ModeFight, Target: "e1", FirePolicy: decide.FireWhenAligned, Movement: decide.MoveStrafe}
	m.frame(200)
	if m.quadHeld {
		t.Fatal("not used in the fight")
	}
	used := false
	for _, r := range m.ticks[len(m.ticks)-1].Tick.Reflexes {
		used = used || r == "quad"
	}
	if !used {
		t.Fatal("no quad reflex in the tick")
	}
	m.frame(300)
	if m.quadHeld {
		t.Fatal("used twice")
	}
}
