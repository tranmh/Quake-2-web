package scripted

import (
	"testing"

	"quake2web/server/internal/agent/decide"
)

// TestKeepOff: the bot backs away from a drain monster inside 320 units
// (a parasite's drain reaches 256) and from a melee one inside 250, and
// strafes a gunman at the same range.
func TestKeepOff(t *testing.T) {
	p := NewPolicy(Config{Seed: 1})
	for _, tc := range []struct {
		class string
		units int
		want  decide.Movement
	}{
		{"parasite", 300, decide.MoveRetreat},
		{"parasite", 340, ""},
		{"berserk", 240, decide.MoveRetreat},
		{"berserk", 300, ""},
		{"soldier", 240, ""},
	} {
		st := baseState()
		st.Enemies = []decide.Enemy{enemy("e1", tc.class, tc.units, "high", "attacking")}
		st.Enemies[0].Current = true
		got := p.Decide(st, 1000).Movement
		switch {
		case tc.want != "" && got != tc.want:
			t.Errorf("%s at %d: %s, want %s", tc.class, tc.units, got, tc.want)
		case tc.want == "" && got == decide.MoveRetreat:
			t.Errorf("%s at %d: retreats", tc.class, tc.units)
		}
	}
}

// TestUnseenTarget: a target out of sight and out of the line of fire
// ranks below a visible attacker; while hit by something unseen the bot
// strafes instead of waiting.
func TestUnseenTarget(t *testing.T) {
	p := NewPolicy(Config{Seed: 1})
	st := baseState()
	hidden := enemy("e1", "parasite", 200, "med", "idle")
	hidden.Visible, hidden.Shootable, hidden.Current = false, false, true
	attacker := enemy("e2", "parasite", 280, "low", "attacking")
	attacker.Visible = false
	st.Enemies = []decide.Enemy{hidden, attacker}
	if d := p.Decide(st, 1000); d.Target != "e2" {
		t.Errorf("target %s, want the attacker in the line of fire", d.Target)
	}
	hidden = enemy("e1", "soldier", 400, "med", "alert")
	hidden.Visible, hidden.Shootable, hidden.Current = false, false, true
	st.Enemies = []decide.Enemy{hidden}
	if m := p.Decide(st, 1000).Movement; m != decide.MoveHold {
		t.Errorf("waiting for an unseen target: %s", m)
	}
	st.Me.DamageLast1s = 5
	if m := p.Decide(st, 1000).Movement; m != decide.MoveStrafeLeft && m != decide.MoveStrafeRight {
		t.Errorf("hit by something unseen: %s, want a strafe", m)
	}
}

// TestPickupReach: a gun better than every owned one is worth a long
// detour, a lesser unowned one a shorter one; nearly dead, health is
// worth a long way too.
func TestPickupReach(t *testing.T) {
	p := NewPolicy(Config{Seed: 1})
	st := baseState()
	st.Objective = &decide.Objective{Kind: "kill"}
	st.Items = []decide.ItemView{{ID: "i1", Class: "weapon_rocketlauncher", Gives: "weapon:rocket_launcher", Path: 2500}}
	if got := p.Decide(st, 1000).Pickup; got != "i1" || p.Decide(st, 1000).Mode != decide.ModePickup {
		t.Errorf("a better gun 2500 away: pickup %q mode %s", got, p.Decide(st, 1000).Mode)
	}
	st.Me.Weapons = []string{"blaster", "shotgun", "railgun"}
	if m := p.Decide(st, 1000).Mode; m == decide.ModePickup {
		t.Error("a lesser gun 2500 away is worth the detour")
	}
	if !outguns("rocket_launcher", []string{"blaster", "shotgun"}) || outguns("grenades", []string{"blaster"}) || outguns("shotgun", []string{"machinegun"}) {
		t.Error("outguns ranks wrong")
	}
	st = baseState()
	st.Objective = &decide.Objective{Kind: "kill"}
	st.Items = []decide.ItemView{{ID: "h1", Class: "item_health", Gives: "health+10", Path: 2500}}
	st.Me.Health, st.Me.HP = 30, "low"
	if m := p.Decide(st, 1000).Mode; m == decide.ModePickup {
		t.Error("health 2500 away at 30 hp")
	}
	st.Me.Health, st.Me.HP = 20, "critical"
	if d := p.Decide(st, 1000); d.Mode != decide.ModePickup || d.Pickup != "h1" {
		t.Errorf("health 2500 away at 20 hp: mode %s pickup %q", d.Mode, d.Pickup)
	}
}
