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

func TestShooterSwitch(t *testing.T) {
	var s Shooter
	b := belief("Blaster", 0, map[string]int{"Shotgun": 1, "Shells": 10})
	if cmd := s.Switch(1000, b, decide.WeaponShotgun); cmd != "use Shotgun" {
		t.Fatalf("switch: %q", cmd)
	}
	if cmd := s.Switch(1100, b, decide.WeaponShotgun); cmd != "" {
		t.Fatalf("not debounced: %q", cmd)
	}
	if cmd := s.Switch(1000+useDebounce, b, decide.WeaponShotgun); cmd != "use Shotgun" {
		t.Fatalf("second try: %q", cmd)
	}
	// the view weapon never changed: unavailable for a while
	if cmd := s.Switch(1000+useVerify+useDebounce, b, decide.WeaponShotgun); cmd != "" {
		t.Fatalf("after %d ms unverified: %q", useVerify, cmd)
	}
	if k := s.Choose(1000+useVerify+useDebounce, b, 300, decide.WeaponShotgun); k != decide.WeaponBlaster {
		t.Errorf("refused weapon chosen: %s", k)
	}
	if k := s.Choose(1000+useVerify+useDebounce+refuseFor+1, b, 300, decide.WeaponShotgun); k != decide.WeaponShotgun {
		t.Errorf("refusal did not expire: %s", k)
	}
	// verified: nothing more to send
	b.Self.Weapon = "Shotgun"
	if cmd := s.Switch(50000, b, decide.WeaponShotgun); cmd != "" {
		t.Errorf("switch to the weapon in hand: %q", cmd)
	}
	if s.Switches() != 2 {
		t.Errorf("%d switches sent", s.Switches())
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
