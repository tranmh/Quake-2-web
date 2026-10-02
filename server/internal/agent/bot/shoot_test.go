package bot

import (
	"testing"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/worldmodel"
)

// belief returns a belief holding weapon cur with ammo, and an inventory
// of the given pickup counts (nil: unknown).
func belief(cur string, ammo int, inv map[string]int) *worldmodel.Belief {
	b := &worldmodel.Belief{}
	b.Self.Weapon, b.Self.Ammo = cur, ammo
	if inv != nil {
		b.Inventory.Known = true
		i := 1
		for name, n := range inv {
			b.Inventory.Items = append(b.Inventory.Items, worldmodel.InvItem{Index: i, Name: name, Count: n})
			i++
		}
	}
	return b
}

func TestShooterChoose(t *testing.T) {
	var s Shooter
	// only the blaster known: the blaster
	if k := s.Choose(0, belief("Blaster", 0, nil), 300, decide.WeaponKeep); k != decide.WeaponBlaster {
		t.Errorf("unknown inventory: %s", k)
	}
	inv := map[string]int{"Blaster": 1, "Shotgun": 1, "Shells": 20, "Super Shotgun": 1, "Rocket Launcher": 1, "Rockets": 5}
	b := belief("Blaster", 0, inv)
	if k := s.Choose(0, b, 200, decide.WeaponKeep); k != decide.WeaponSuperShotgun {
		t.Errorf("close: %s, want the super shotgun", k)
	}
	if k := s.Choose(0, b, 900, decide.WeaponKeep); k != decide.WeaponRocketLauncher {
		t.Errorf("far: %s, want the rocket launcher", k)
	}
	// splash at close range is never chosen, even when preferred
	if k := s.Choose(0, belief("Blaster", 0, map[string]int{"Rocket Launcher": 1, "Rockets": 5}), 100, decide.WeaponKeep); k != decide.WeaponBlaster {
		t.Errorf("rockets point blank: %s", k)
	}
	// the policy's preference wins when usable
	if k := s.Choose(0, b, 200, decide.WeaponShotgun); k != decide.WeaponShotgun {
		t.Errorf("preferred shotgun: %s", k)
	}
	// no ammo: not usable
	if k := s.Choose(0, belief("Blaster", 0, map[string]int{"Shotgun": 1}), 200, decide.WeaponShotgun); k != decide.WeaponBlaster {
		t.Errorf("shotgun without shells: %s", k)
	}
	// the current weapon is kept unless another is clearly better
	cur := belief("Machinegun", 50, map[string]int{"Machinegun": 1, "Bullets": 50, "Super Shotgun": 1, "Shells": 10})
	if k := s.Choose(0, cur, 900, decide.WeaponKeep); k != decide.WeaponMachinegun {
		t.Errorf("keep the machinegun far away: %s", k)
	}
}

// TestShooterSwitch calls Switch every frame (100 ms), the way the bot
// loop does.
func TestShooterSwitch(t *testing.T) {
	var s Shooter
	b := belief("Blaster", 0, map[string]int{"Shotgun": 1, "Shells": 10})
	var sends []int64
	refusedAt := int64(-1)
	// the view weapon never changes: resent after useDebounce, refused
	// useVerify after the first "use" (not after the last one)
	for now := int64(1000); now <= 1000+useVerify+refuseFor-100; now += 100 {
		if cmd := s.Switch(now, b, decide.WeaponShotgun); cmd != "" {
			if cmd != "use Shotgun" {
				t.Fatalf("at %d: %q", now, cmd)
			}
			if refusedAt >= 0 {
				t.Fatalf("at %d: sent %q while refused (since %d)", now, cmd, refusedAt)
			}
			sends = append(sends, now)
		}
		if refusedAt < 0 && s.Refused(now, decide.WeaponShotgun) {
			refusedAt = now
		}
	}
	if len(sends) != 2 || sends[0] != 1000 || sends[1] != 1000+useDebounce {
		t.Fatalf("use commands at %v, want at 1000 and %d", sends, 1000+useDebounce)
	}
	if refusedAt != 1000+useVerify {
		t.Fatalf("refused at %d, want %d (useVerify after the first use)", refusedAt, 1000+useVerify)
	}
	if k := s.Choose(refusedAt+refuseFor-100, b, 300, decide.WeaponShotgun); k != decide.WeaponBlaster {
		t.Errorf("refused weapon chosen: %s", k)
	}
	// the refusal expires: chosen and tried again
	end := refusedAt + refuseFor
	if k := s.Choose(end, b, 300, decide.WeaponShotgun); k != decide.WeaponShotgun {
		t.Errorf("refusal did not expire: %s", k)
	}
	if cmd := s.Switch(end, b, decide.WeaponShotgun); cmd != "use Shotgun" {
		t.Fatalf("after the refusal: %q", cmd)
	}
	// it comes up: nothing more to send, and a later switch to it is a
	// new one (judged from its own first use)
	b.Self.Weapon = "Shotgun"
	for now := end + 100; now < end+useVerify+useDebounce; now += 100 {
		if cmd := s.Switch(now, b, decide.WeaponShotgun); cmd != "" {
			t.Fatalf("switch to the weapon in hand: %q", cmd)
		}
	}
	if s.Refused(end+useVerify+useDebounce, decide.WeaponShotgun) {
		t.Error("a verified switch was refused")
	}
	b.Self.Weapon = "Blaster"
	later := end + 60000
	if cmd := s.Switch(later, b, decide.WeaponShotgun); cmd != "use Shotgun" {
		t.Fatalf("a new switch: %q", cmd)
	}
	if cmd := s.Switch(later+useVerify-100, b, decide.WeaponShotgun); cmd != "use Shotgun" || s.Refused(later+useVerify-100, decide.WeaponShotgun) {
		t.Errorf("a new switch, resent before its own useVerify: %q, refused %v", cmd, s.Refused(later+useVerify-100, decide.WeaponShotgun))
	}
	if s.Switches() != 5 {
		t.Errorf("%d switches sent, want 5", s.Switches())
	}
}

// TestShooterSwitchBack: a switch that showed in the view weapon is done.
// When the game later changes the weapon by itself (a pickup's
// auto-switch), switching back sends a "use" judged afresh, not a refusal;
// and a level entry forgets a refusal.
func TestShooterSwitchBack(t *testing.T) {
	var s Shooter
	inv := map[string]int{"Shotgun": 1, "Shells": 20, "Machinegun": 1, "Bullets": 50}
	if cmd := s.Switch(1000, belief("Blaster", 0, inv), decide.WeaponShotgun); cmd != "use Shotgun" {
		t.Fatalf("switch: %q", cmd)
	}
	// it takes: the bot stops asking (weaponTick: the weapon wanted is in
	// hand) and observes every frame
	s.Observe(belief("Shotgun", 20, inv))
	mg := belief("Machinegun", 50, inv)
	s.Observe(mg)
	if cmd := s.Switch(20000, mg, decide.WeaponShotgun); cmd != "use Shotgun" || s.Refused(20000, decide.WeaponShotgun) {
		t.Fatalf("switch back after the game's own change: %q, refused %v", cmd, s.Refused(20000, decide.WeaponShotgun))
	}
	// this one never takes: refused useVerify after its first use
	for now := int64(20100); now <= 20000+useVerify; now += 100 {
		s.Observe(mg)
		s.Switch(now, mg, decide.WeaponShotgun)
	}
	if !s.Refused(20000+useVerify, decide.WeaponShotgun) {
		t.Fatal("a switch that never showed was not refused")
	}
	s.ForgetSwitch()
	if now := int64(20000 + useVerify + 100); s.Refused(now, decide.WeaponShotgun) || s.Switch(now, mg, decide.WeaponShotgun) != "use Shotgun" {
		t.Fatal("a refusal outlived the level entry")
	}
}

// TestShooterHoldOff: while a switch away from a continuous weapon is in
// progress the trigger stays released (the game drops a weapon only out of
// its firing state); not for a weapon that is not continuous, nor once the
// switch shows.
func TestShooterHoldOff(t *testing.T) {
	inv := map[string]int{"Machinegun": 1, "Bullets": 50, "Super Shotgun": 1, "Shotgun": 1, "Shells": 20}
	var s Shooter
	mg := belief("Machinegun", 50, inv)
	if s.HoldOff(mg) {
		t.Fatal("held off without a switch")
	}
	if cmd := s.Switch(1000, mg, decide.WeaponSuperShotgun); cmd != "use Super Shotgun" {
		t.Fatalf("switch: %q", cmd)
	}
	if !s.HoldOff(mg) {
		t.Fatal("the machinegun would fire on through the switch")
	}
	ssg := belief("Super Shotgun", 20, inv)
	s.Observe(ssg)
	if s.HoldOff(ssg) {
		t.Fatal("held off after the switch showed")
	}
	var s2 Shooter
	sg := belief("Shotgun", 20, inv)
	if cmd := s2.Switch(1000, sg, decide.WeaponSuperShotgun); cmd == "" || s2.HoldOff(sg) {
		t.Fatalf("switch %q from the shotgun held off %v", cmd, s2.HoldOff(sg))
	}
}

func TestShooterAim(t *testing.T) {
	var s Shooter
	eye := Vec3{0, 0, 0}
	target := AimTarget{Point: Vec3{400, 400, 0}, Radius: 16, Fire: true} // 45 degrees left, 566 units
	yaw, pitch, fire := s.Aim(eye, 300, 10, target, 25)
	if fire {
		t.Fatal("fired before turning")
	}
	steps := 0
	for !fire && steps < 80 {
		yaw, pitch, fire = s.Aim(eye, 0, 0, target, 25)
		steps++
	}
	if !fire {
		t.Fatalf("never aligned: view %v/%v", yaw, pitch)
	}
	// 105 degrees at 720 deg/s is at least 6 commands
	if steps < 5 {
		t.Errorf("aligned in %d commands: the turn is not rate capped", steps)
	}
	// held fire: aligned but not allowed
	target.Fire = false
	if _, _, fire := s.Aim(eye, 0, 0, target, 25); fire {
		t.Error("fired with Fire unset")
	}
	// a led projectile weapon aims ahead of a moving target
	tr := &worldmodel.Track{Pos: Vec3{1000, 0, 0}, Vel: Vec3{0, 200, 0}, Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}}
	p, r := aimFor(eye, tr, decide.WeaponBlaster)
	if p[1] < 150 || r != 16 {
		t.Errorf("blaster lead %v radius %v", p, r)
	}
	if p, _ := aimFor(eye, tr, decide.WeaponShotgun); p[1] != 0 || p[2] != 4 {
		t.Errorf("hitscan aim %v, want the box centre", p)
	}
}
