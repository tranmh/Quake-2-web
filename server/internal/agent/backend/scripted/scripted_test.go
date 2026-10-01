package scripted

import (
	"context"
	"encoding/json"
	"testing"

	"quake2web/server/internal/agent/decide"
)

func enemy(id, class string, units int, threat, state string) decide.Enemy {
	dist := "far"
	switch {
	case units < 250:
		dist = "close"
	case units < 700:
		dist = "mid"
	}
	return decide.Enemy{ID: id, Class: class, Units: units, Dist: dist, Visible: true, Shootable: true, Aim: "near", State: state, Threat: threat}
}

func baseState() *decide.State {
	return &decide.State{
		Me:      decide.Me{HP: "full", Health: 100, Weapon: "shotgun", Ammo: "ok", OnGround: true, Weapons: []string{"blaster", "shotgun"}},
		Enemies: []decide.Enemy{},
		Space:   &decide.Space{Front: "open", Back: "open", Left: "open", Right: "open"},
	}
}

func TestModeRules(t *testing.T) {
	p := NewPolicy(Config{})
	for _, tc := range []struct {
		name string
		edit func(s *decide.State)
		want decide.Mode
	}{
		{"nothing: explore", func(s *decide.State) {}, decide.ModeExplore},
		{"an objective", func(s *decide.State) { s.Objective = &decide.Objective{Kind: "press"} }, decide.ModeObjective},
		{"a visible awake enemy within 1200", func(s *decide.State) {
			s.Objective = &decide.Objective{Kind: "press"}
			s.Enemies = []decide.Enemy{enemy("e1", "soldier", 1100, "med", "alert")}
		}, decide.ModeFight},
		{"an idle enemy does not start a fight", func(s *decide.State) {
			s.Enemies = []decide.Enemy{enemy("e1", "soldier", 400, "low", "idle")}
		}, decide.ModeExplore},
		{"too far to fight", func(s *decide.State) {
			s.Enemies = []decide.Enemy{enemy("e1", "soldier", 1300, "med", "attacking")}
		}, decide.ModeExplore},
		{"hurt with health near", func(s *decide.State) {
			s.Me.Health, s.Me.HP = 35, "low"
			s.Objective = &decide.Objective{Kind: "press"}
			s.Items = []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Path: 400}}
		}, decide.ModePickup},
		{"health too far", func(s *decide.State) {
			s.Me.Health, s.Me.HP = 35, "low"
			s.Items = []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Path: 900}}
		}, decide.ModeExplore},
		{"an unowned weapon near", func(s *decide.State) {
			s.Items = []decide.ItemView{{ID: "i2", Class: "weapon_supershotgun", Gives: "weapon:super_shotgun", Path: 700}}
		}, decide.ModePickup},
		{"an owned weapon is no reason", func(s *decide.State) {
			s.Items = []decide.ItemView{{ID: "i2", Class: "weapon_shotgun", Gives: "weapon:shotgun", Path: 100}}
		}, decide.ModeExplore},
		{"low ammo with ammo near", func(s *decide.State) {
			s.Me.Ammo = "low"
			s.Items = []decide.ItemView{{ID: "i3", Class: "ammo_shells", Gives: "shells+10", Path: 300}}
		}, decide.ModePickup},
		{"critical health under fire: retreat", func(s *decide.State) {
			s.Me.Health, s.Me.HP, s.Me.DamageLast1s = 20, "critical", 25
			s.Enemies = []decide.Enemy{enemy("e1", "gunner", 400, "high", "attacking")}
		}, decide.ModeRetreat},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := baseState()
			tc.edit(s)
			if d := p.Decide(s, 0); d.Mode != tc.want {
				t.Fatalf("mode %s, want %s (danger %d)", d.Mode, tc.want, d.Danger)
			}
		})
	}
}

func TestTargetAndFire(t *testing.T) {
	p := NewPolicy(Config{})
	s := baseState()
	s.Enemies = []decide.Enemy{
		enemy("e1", "soldier", 400, "med", "attacking"),
		enemy("e2", "gunner", 500, "high", "attacking"),
		enemy("e3", "soldier_light", 200, "low", "idle"),
	}
	d := p.Decide(s, 0)
	if d.Target != "e2" {
		t.Fatalf("target %s, want e2 (threat over distance)", d.Target)
	}
	// stickiness: the current target stays unless another beats it by 15%
	s.Enemies[0].Current = true
	s.Enemies[0].Units, s.Enemies[0].Threat = 300, "high" // 3/300 vs e2 3/500
	s.Enemies[1].Units = 280                              // e2 now 3/280: 7% better
	if d := p.Decide(s, 0); d.Target != "e1" {
		t.Fatalf("sticky target %s, want e1", d.Target)
	}
	s.Enemies[1].Units = 200 // 50% better
	if d := p.Decide(s, 0); d.Target != "e2" {
		t.Fatalf("clearly better target %s, want e2", d.Target)
	}

	// fire policies
	for _, tc := range []struct {
		name string
		e    decide.Enemy
		ammo string
		want decide.FirePolicy
	}{
		{"shootable in range", enemy("e1", "soldier", 400, "med", "attacking"), "ok", decide.FireWhenAligned},
		{"out of shotgun range", enemy("e1", "soldier", 650, "med", "attacking"), "ok", decide.FireHold},
		{"close and dangerous", enemy("e1", "gunner", 200, "high", "attacking"), "ok", decide.FireSuppress},
		{"no ammo", enemy("e1", "soldier", 300, "med", "attacking"), "none", decide.FireHold},
		{"not shootable", func() decide.Enemy { e := enemy("e1", "soldier", 300, "med", "alert"); e.Shootable = false; return e }(), "ok", decide.FireHold},
		{"not in view", func() decide.Enemy { e := enemy("e1", "soldier", 300, "med", "alert"); e.Visible = false; return e }(), "ok", decide.FireHold},
	} {
		s := baseState()
		s.Me.Ammo = tc.ammo
		s.Enemies = []decide.Enemy{tc.e}
		if d := p.Decide(s, 0); d.FirePolicy != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, d.FirePolicy, tc.want)
		}
	}
	if d := p.Decide(baseState(), 0); d.Target != "" || d.FirePolicy != decide.FireHold || d.Movement != decide.MoveHold {
		t.Errorf("no enemies: %+v", d)
	}
}

func TestMovementRanges(t *testing.T) {
	p := NewPolicy(Config{})
	strafe := func(m decide.Movement) bool { return m == decide.MoveStrafeLeft || m == decide.MoveStrafeRight }
	for _, tc := range []struct {
		weapon, class string
		units         int
		want          string // advance, retreat, strafe
	}{
		{"shotgun", "soldier", 100, "retreat"},
		{"shotgun", "soldier", 200, "strafe"},
		{"shotgun", "soldier", 400, "advance"},
		{"super_shotgun", "soldier", 150, "strafe"},
		{"super_shotgun", "soldier", 260, "advance"},
		{"machinegun", "soldier", 200, "retreat"},
		{"chaingun", "soldier", 500, "strafe"},
		{"chaingun", "soldier", 700, "advance"},
		{"blaster", "soldier", 250, "retreat"},
		{"blaster", "soldier", 400, "strafe"},
		{"blaster", "soldier", 600, "advance"},
		{"rocket_launcher", "soldier", 150, "retreat"},
		{"rocket_launcher", "soldier", 250, "strafe"},
		{"rocket_launcher", "soldier", 900, "advance"},
		{"railgun", "soldier", 300, "retreat"},
		{"railgun", "soldier", 1500, "strafe"},
		{"super_shotgun", "berserk", 200, "retreat"}, // melee only: keep 250 away
		{"super_shotgun", "berserk", 260, "advance"},
		{"super_shotgun", "parasite", 100, "retreat"},
		{"super_shotgun", "infantry", 150, "strafe"}, // has a gun too
	} {
		s := baseState()
		s.Me.Weapon = tc.weapon
		s.Enemies = []decide.Enemy{enemy("e1", tc.class, tc.units, "med", "attacking")}
		m := p.Decide(s, 0).Movement
		ok := string(m) == tc.want || tc.want == "strafe" && strafe(m)
		if !ok {
			t.Errorf("%s vs %s at %d: %s, want %s", tc.weapon, tc.class, tc.units, m, tc.want)
		}
	}
}

func TestStrafeRhythm(t *testing.T) {
	for seed := uint64(0); seed < 5; seed++ {
		var runs []int64
		last, since := StrafeLeft(seed, 0), int64(0)
		if !last {
			t.Fatalf("seed %d: a window starts left", seed)
		}
		for now := int64(1); now <= 60000; now++ {
			if l := StrafeLeft(seed, now); l != last {
				runs = append(runs, now-since)
				last, since = l, now
			}
		}
		if len(runs) < 60 {
			t.Fatalf("seed %d: %d flips in 60 s", seed, len(runs))
		}
		for i, d := range runs {
			if d < 600 || d > 1200 {
				t.Fatalf("seed %d: segment %d lasts %d ms", seed, i, d)
			}
		}
	}
	// negative times fall into the window before 0, which ends right
	if StrafeLeft(1, -1) || !StrafeLeft(1, -1800) {
		t.Fatal("negative times")
	}

	// blocked sides flip the strafe; both blocked hold; dodges win
	p := NewPolicy(Config{Seed: 1})
	s := baseState()
	s.Enemies = []decide.Enemy{enemy("e1", "soldier", 200, "med", "attacking")}
	now := int64(0) // left at the start of a window
	s.Space.Left = "blocked"
	if m := p.Decide(s, now).Movement; m != decide.MoveStrafeRight {
		t.Errorf("left blocked: %s", m)
	}
	s.Space.Right = "blocked"
	if m := p.Decide(s, now).Movement; m != decide.MoveHold {
		t.Errorf("both blocked: %s", m)
	}
	s = baseState()
	s.Enemies = []decide.Enemy{enemy("e1", "soldier", 100, "med", "attacking")}
	s.Space.Back = "blocked"
	if m := p.Decide(s, now).Movement; m != decide.MoveStrafeLeft {
		t.Errorf("retreat blocked: %s", m)
	}
	s.Incoming = []decide.Incoming{{Kind: "rocket", ETA: "imminent", Dodge: "right"}}
	if m := p.Decide(s, now).Movement; m != decide.MoveStrafeRight {
		t.Errorf("dodge: %s", m)
	}
	s = baseState()
	s.Incoming = []decide.Incoming{{Kind: "blaster_bolt", ETA: "soon", Dodge: "left"}}
	if m := p.Decide(s, now).Movement; m != decide.MoveStrafeLeft {
		t.Errorf("dodge without a target: %s", m)
	}
}

func TestWeaponAndPickup(t *testing.T) {
	p := NewPolicy(Config{})
	s := baseState()
	s.Me.Weapons = []string{"blaster", "shotgun", "super_shotgun", "chaingun", "grenade_launcher", "rocket_launcher", "railgun"}
	for _, tc := range []struct {
		units int
		want  decide.WeaponKey
	}{{150, decide.WeaponSuperShotgun}, {500, decide.WeaponChaingun}, {1000, decide.WeaponRailgun}} {
		s.Enemies = []decide.Enemy{enemy("e1", "soldier", tc.units, "med", "attacking")}
		if d := p.Decide(s, 0); d.Weapon != tc.want {
			t.Errorf("at %d: %s, want %s", tc.units, d.Weapon, tc.want)
		}
	}
	// never grenades
	s.Me.Weapons, s.Me.Weapon = []string{"blaster", "grenade_launcher"}, "blaster"
	s.Enemies = []decide.Enemy{enemy("e1", "soldier", 500, "med", "attacking")}
	if d := p.Decide(s, 0); d.Weapon != decide.WeaponKeep {
		t.Errorf("grenades chosen: %s", d.Weapon)
	}
	// already the best: keep; no target: keep
	s.Me.Weapons, s.Me.Weapon = []string{"blaster", "chaingun"}, "chaingun"
	if d := p.Decide(s, 0); d.Weapon != decide.WeaponKeep {
		t.Errorf("best in hand: %s", d.Weapon)
	}
	s.Enemies = nil
	if d := p.Decide(s, 0); d.Weapon != decide.WeaponKeep {
		t.Errorf("no target: %s", d.Weapon)
	}
	// nearest needed item
	s.Items = []decide.ItemView{{ID: "i1", Path: 500}, {ID: "i2", Path: 200}, {ID: "i3", Path: 200}}
	if d := p.Decide(s, 0); d.Pickup != "i2" {
		t.Errorf("pickup %s", d.Pickup)
	}
}

func TestDanger(t *testing.T) {
	p := NewPolicy(Config{})
	s := baseState()
	if d := p.Decide(s, 0); d.Danger != decide.DangerSafe {
		t.Errorf("empty: %d", d.Danger)
	}
	s.Enemies = []decide.Enemy{enemy("e1", "soldier", 600, "med", "attacking")}
	low := p.Decide(s, 0).Danger
	s.Enemies = append(s.Enemies, enemy("e2", "gunner", 200, "high", "attacking"), enemy("e3", "gunner", 300, "high", "attacking"))
	s.Incoming = []decide.Incoming{{ETA: "imminent", Dodge: "left"}}
	s.Me.HP, s.Me.Health, s.Me.DamageLast1s = "critical", 15, 30
	if high := p.Decide(s, 0).Danger; high != decide.DangerCritical || low >= high {
		t.Errorf("danger %d -> %d", low, high)
	}
}

// TestBackendAnswers: one-hot answers with confidence 1 for exactly the
// questions asked, options respected, the wire format in Raw.
func levelOf(s float64) string { return string(rune('0' + int(s))) }

func TestBackendAnswers(t *testing.T) {
	b := New(Config{Seed: 7})
	s := baseState()
	s.Enemies = []decide.Enemy{enemy("e1", "soldier", 400, "med", "attacking")}
	s.Items = []decide.ItemView{{ID: "i1", Class: "ammo_shells", Gives: "shells+10", Path: 300}}
	for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
		req, err := decide.NewRequest(1, lane, 1234, s, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := b.Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Answers) != len(req.Questions) || resp.Model != Name || resp.Seq != 1 {
			t.Fatalf("%s: %d answers for %d questions", lane, len(resp.Answers), len(req.Questions))
		}
		for _, q := range req.Questions {
			a := resp.Answers[q.ID]
			key := a.Choice
			if q.Type == decide.Score {
				key = levelOf(a.Score)
			}
			if q.Index(key) < 0 || a.Confidence != 1 || a.Probabilities[key] != 1 {
				t.Errorf("%s: answer %+v not a confident option", q.ID, a)
			}
		}
		dec, err := decide.DecodeResponse(resp.Raw, req.Questions)
		if err != nil || len(dec.Answers) != len(resp.Answers) {
			t.Fatalf("raw does not decode: %v", err)
		}
		// without the typed view it decodes the state
		bare := *req
		bare.View = nil
		again, err := b.Decide(context.Background(), &bare)
		if err != nil || string(again.Raw) != string(resp.Raw) {
			t.Fatalf("from the encoded state: %v\n%s\n%s", err, again.Raw, resp.Raw)
		}
	}
	// unknown question ids are not answered; a cancelled context fails
	ans := b.Policy().Answers(s, []decide.Question{{ID: "dodge", Type: decide.Noul}}, 0)
	if len(ans) != 0 {
		t.Errorf("unknown question answered: %+v", ans)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Decide(ctx, &decide.Request{State: json.RawMessage(`{}`)}); err == nil {
		t.Error("cancelled context")
	}
	// determinism
	req, _ := decide.NewRequest(2, decide.LaneFast, 5000, s, nil, 0)
	first, _ := b.Decide(context.Background(), req)
	for i := 0; i < 50; i++ {
		r, _ := b.Decide(context.Background(), req)
		if string(r.Raw) != string(first.Raw) {
			t.Fatal("not deterministic")
		}
	}
}
