package perception

import (
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// gameModelPaths returns every model path string literal of the game
// source (server/internal/game/*.go), including the space separated
// precache lists, without importing the package.
func gameModelPaths(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "game", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("game sources not found: %v", err)
	}
	re := regexp.MustCompile(`^(models|players|sprites)/[^ ]+\.(md2|sp2)$`)
	seen := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var s scanner.Scanner
		fset := token.NewFileSet()
		s.Init(fset.AddFile(f, -1, len(src)), src, nil, 0)
		for {
			_, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok != token.STRING {
				continue
			}
			v, err := strconv.Unquote(lit)
			if err != nil {
				continue
			}
			for _, w := range strings.Fields(v) {
				if re.MatchString(w) {
					seen[w] = true
				}
			}
		}
	}
	var out []string
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// TestEveryGameModelClassified: each model the game can send has an
// explicit class (not the directory fallback).
func TestEveryGameModelClassified(t *testing.T) {
	tab := NewClassTable()
	paths := gameModelPaths(t)
	if len(paths) < 100 {
		t.Fatalf("only %d model paths found in the game source", len(paths))
	}
	for _, p := range paths {
		c := tab.ByModel(p)
		if c == nil {
			t.Errorf("%s: no class", p)
			continue
		}
		if c.Kind == KindUnknown {
			t.Errorf("%s: class %s has kind unknown", p, c.Name)
		}
	}
	// every monster and item class has its priors
	for _, c := range tab.Classes() {
		switch c.Kind {
		case KindMonster:
			if c.Model != "" && (c.Health <= 0 || c.DPS <= 0 || c.Range <= 0 || c.Weapon == WeaponNone || c.Maxs == (Vec3{})) {
				t.Errorf("monster %s lacks priors: %+v", c.Name, c)
			}
		case KindItem:
			if c.Model != "" && (c.Pickup == "" || c.Item == ItemNone || c.Value <= 0 || len(c.Classnames) == 0) {
				t.Errorf("item %s lacks properties: %+v", c.Name, c)
			}
		}
	}
}

func testCS() *ConfigStrings {
	var cs ConfigStrings
	models := []string{"", "maps/demo1.bsp", "*1", "*2", modelSoldier, modelTank, modelLaser,
		"models/items/healing/stimpack/tris.md2", "models/objects/gibs/head2/tris.md2",
		"models/monsters/insane/tris.md2", "models/objects/rocket/tris.md2", "models/weird/thing.md2",
		"models/monsters/newmonster/tris.md2"}
	for i, m := range models {
		cs[q2const.CS_MODELS+i] = m
	}
	for i, n := range []string{"", "Body Armor", "Health", "Blue Key"} {
		cs[q2const.CS_ITEMS+i] = n
	}
	return &cs
}

func TestClassifySkinsAndKinds(t *testing.T) {
	c := NewClassifier(NewClassTable())
	c.Update(testCS())
	for _, tc := range []struct {
		s       shared.EntityState
		name    string
		kind    Kind
		wounded bool
	}{
		{shared.EntityState{ModelIndex: 4, SkinNum: 0}, "soldier_light", KindMonster, false},
		{shared.EntityState{ModelIndex: 4, SkinNum: 1}, "soldier_light", KindMonster, true},
		{shared.EntityState{ModelIndex: 4, SkinNum: 2}, "soldier", KindMonster, false},
		{shared.EntityState{ModelIndex: 4, SkinNum: 3}, "soldier", KindMonster, true},
		{shared.EntityState{ModelIndex: 4, SkinNum: 4}, "soldier_ss", KindMonster, false},
		{shared.EntityState{ModelIndex: 4, SkinNum: 5}, "soldier_ss", KindMonster, true},
		{shared.EntityState{ModelIndex: 5, SkinNum: 0}, "tank", KindMonster, false},
		{shared.EntityState{ModelIndex: 5, SkinNum: 1}, "tank", KindMonster, true},
		{shared.EntityState{ModelIndex: 5, SkinNum: 2}, "tank_commander", KindMonster, false},
		{shared.EntityState{ModelIndex: 5, SkinNum: 3}, "tank_commander", KindMonster, true},
		{shared.EntityState{ModelIndex: 6, Effects: q2const.EF_BLASTER}, "blaster_bolt", KindProjectile, false},
		{shared.EntityState{ModelIndex: 6, Effects: q2const.EF_HYPERBLASTER}, "hyperblaster_bolt", KindProjectile, false},
		{shared.EntityState{ModelIndex: 7}, "item_health_small", KindItem, false},
		{shared.EntityState{ModelIndex: 8, Effects: q2const.EF_GIB}, "gib_head2", KindGib, false},
		{shared.EntityState{ModelIndex: 9, SkinNum: 1}, "insane", KindNeutral, false}, // random skins
		{shared.EntityState{ModelIndex: 10}, "rocket", KindProjectile, false},
		{shared.EntityState{ModelIndex: 11}, "unknown", KindUnknown, false},
		{shared.EntityState{ModelIndex: 11, Effects: q2const.EF_ROCKET}, "projectile", KindProjectile, false},
		{shared.EntityState{ModelIndex: 12}, "monster", KindMonster, false},
		{shared.EntityState{ModelIndex: 2, Solid: 31}, "brush", KindBrush, false},
		{shared.EntityState{ModelIndex: 255}, "player", KindPlayer, false},
		{shared.EntityState{ModelIndex: 1, RenderFX: q2const.RF_BEAM}, "laser_beam", KindBeam, false},
		{shared.EntityState{Sound: 7}, "speaker", KindSpeaker, false},
	} {
		got := c.Classify(&tc.s)
		if got.Class.Name != tc.name || got.Class.Kind != tc.kind || got.Wounded != tc.wounded {
			t.Errorf("%+v: got %s/%s wounded=%v, want %s/%s %v", tc.s, got.Class.Name, got.Class.Kind, got.Wounded, tc.name, tc.kind, tc.wounded)
		}
	}
	if c.InlineModel(3) != 2 || c.InlineModel(4) != 0 || c.ModelPath(4) != modelSoldier || c.ModelPath(-1) != "" {
		t.Fatal("model index lookups")
	}
	if c.ItemIndex("Blue Key") != 3 || c.ItemIndex("Railgun") != 0 || c.ItemName(1) != "Body Armor" {
		t.Fatal("item index lookups")
	}
	// lump classnames reach the classes
	if cs := c.Table().ForClassname("monster_soldier_ss"); len(cs) != 1 || cs[0].Name != "soldier_ss" {
		t.Fatalf("ForClassname: %v", cs)
	}
}

// encodeSolid is SV_LinkEdict's packing of a SOLID_BBOX box.
// C: server/sv_world.c:165 SV_LinkEdict
func encodeSolid(mins, maxs Vec3) int32 {
	i := int32(maxs[0] / 8)
	i = min(max(i, 1), 31)
	j := int32(-mins[2] / 8)
	j = min(max(j, 1), 31)
	k := int32((maxs[2] + 32) / 8)
	k = min(max(k, 1), 63)
	return k<<10 | j<<5 | i
}

func TestDecodeSolid(t *testing.T) {
	for _, tc := range []struct {
		mins, maxs Vec3
	}{
		{Vec3{-16, -16, -24}, Vec3{16, 16, 32}}, // player, most monsters
		{Vec3{-32, -32, -16}, Vec3{32, 32, 72}}, // tank
		{Vec3{-24, -24, -24}, Vec3{24, 24, 32}}, // hover
		{Vec3{-64, -64, -24}, Vec3{64, 64, 112}},
	} {
		s := encodeSolid(tc.mins, tc.maxs)
		mins, maxs, ok := DecodeSolid(s)
		if !ok || mins != tc.mins || maxs != tc.maxs {
			t.Errorf("solid %d (%v %v): decoded %v %v %v", s, tc.mins, tc.maxs, mins, maxs, ok)
		}
	}
	// a ducked player: maxs z 4 packs to 0 (8 unit steps, truncated)
	if _, maxs, _ := DecodeSolid(encodeSolid(Vec3{-16, -16, -24}, Vec3{16, 16, 4})); maxs[2] != 0 {
		t.Errorf("ducked maxs %v", maxs)
	}
	// what the demo maps send: the player and a barrel (mins z 0 packs as -8)
	if mins, maxs, ok := DecodeSolid(8290); !ok || mins != (Vec3{-16, -16, -24}) || maxs != (Vec3{16, 16, 32}) {
		t.Errorf("8290: %v %v", mins, maxs)
	}
	if mins, maxs, ok := DecodeSolid(9250); !ok || mins != (Vec3{-16, -16, -8}) || maxs != (Vec3{16, 16, 40}) {
		t.Errorf("9250: %v %v", mins, maxs)
	}
	for _, s := range []int32{0, 31} {
		if _, _, ok := DecodeSolid(s); ok {
			t.Errorf("%d decoded as a box", s)
		}
	}
}

func TestClassifySound(t *testing.T) {
	for _, tc := range []struct {
		path string
		kind SoundKind
		fam  string
	}{
		{"soldier/soldeth1.wav", SoundDeath, "soldier"},
		{"soldier/solsght1.wav", SoundSight, "soldier"},
		{"soldier/solsrch1.wav", SoundSearch, "soldier"},
		{"soldier/solpain2.wav", SoundPain, "soldier"},
		{"soldier/solidle1.wav", SoundIdle, "soldier"},
		{"infantry/infatck3.wav", SoundIdle, "infantry"},
		{"infantry/inflies1.wav", SoundAmbient, ""},
		{"infantry/infatck1.wav", SoundAttack, "infantry"},
		{"gunner/death1.wav", SoundDeath, "gunner"},
		{"gunner/sight1.wav", SoundSight, "gunner"},
		{"berserk/berdeth2.wav", SoundDeath, "berserk"},
		{"tank/death.wav", SoundDeath, "tank"},
		{"bosstank/btkdeth1.wav", SoundDeath, "supertank"},
		{"flyer/flydeth1.wav", SoundDeath, "flyer"},
		{"parasite/pardeth1.wav", SoundDeath, "parasite"},
		{"misc/udeath.wav", SoundGib, ""},
		{"doors/dr1_strt.wav", SoundDoor, ""},
		{"plats/pt1_end.wav", SoundPlat, ""},
		{"switches/butn2.wav", SoundButton, ""},
		{"weapons/rocklx1a.wav", SoundExplosion, ""},
		{"world/explod2.wav", SoundExplosion, ""},
		{"weapons/blastf1a.wav", SoundWeapon, ""},
		{"items/pkup.wav", SoundPickup, ""},
		{"misc/ar2_pkup.wav", SoundPickup, ""},
		{"*pain50_1.wav", SoundPlayer, ""},
		{"world/amb7.wav", SoundAmbient, ""},
		{"", SoundOther, ""},
	} {
		k, f := ClassifySound(tc.path)
		if k != tc.kind || f != tc.fam {
			t.Errorf("%q: %s/%q, want %s/%q", tc.path, k, f, tc.kind, tc.fam)
		}
	}
}

func TestFlashWeapons(t *testing.T) {
	for _, tc := range []struct {
		mz2 int32
		w   Weapon
	}{
		{q2const.MZ2_SOLDIER_BLASTER_1, WeaponBlaster},
		{q2const.MZ2_SOLDIER_BLASTER_2, WeaponBlaster},
		{q2const.MZ2_SOLDIER_SHOTGUN_1, WeaponShotgun},
		{q2const.MZ2_SOLDIER_MACHINEGUN_2, WeaponMachinegun},
		{q2const.MZ2_SOLDIER_BLASTER_8, WeaponBlaster},
		{q2const.MZ2_SOLDIER_SHOTGUN_5, WeaponShotgun},
		{q2const.MZ2_SOLDIER_MACHINEGUN_7, WeaponMachinegun},
		{q2const.MZ2_GUNNER_MACHINEGUN_3, WeaponChaingun},
		{q2const.MZ2_GUNNER_GRENADE_2, WeaponGrenade},
		{q2const.MZ2_INFANTRY_MACHINEGUN_13, WeaponMachinegun},
		{q2const.MZ2_TANK_ROCKET_2, WeaponRocket},
		{q2const.MZ2_TANK_BLASTER_3, WeaponBlaster},
		{q2const.MZ2_FLYER_BLASTER_2, WeaponBlaster},
		{q2const.MZ2_GLADIATOR_RAILGUN_1, WeaponRailgun},
		{q2const.MZ2_JORG_BFG_1, WeaponBFG},
		{q2const.MZ2_WIDOW_RAIL, WeaponOther},
	} {
		if got := MonsterFlashWeapon(tc.mz2); got != tc.w {
			t.Errorf("MZ2 %d: %s, want %s", tc.mz2, got, tc.w)
		}
	}
	if PlayerFlashWeapon(q2const.MZ_ROCKET) != WeaponRocket || PlayerFlashWeapon(q2const.MZ_SSHOTGUN) != WeaponShotgun {
		t.Fatal("player flashes")
	}
}
