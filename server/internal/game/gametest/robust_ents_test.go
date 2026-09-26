package gametest

// Crafted entity strings (user-uploaded maps): random entities with random
// keys cross-linked through a small targetname pool, spawned next to the
// player on demo1, then played with random hostile input.

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
)

var robustClassnames = strings.Fields(`item_health item_health_small item_health_large item_health_mega
info_player_start info_player_deathmatch info_player_coop info_player_intermission func_plat func_button
func_door func_door_secret func_door_rotating func_rotating func_train func_water func_conveyor
func_areaportal func_clock func_wall func_object func_timer func_explosive func_killbox trigger_always
trigger_once trigger_multiple trigger_relay trigger_push trigger_hurt trigger_key trigger_counter
trigger_elevator trigger_gravity trigger_monsterjump target_temp_entity target_speaker target_explosion
target_changelevel target_secret target_goal target_splash target_spawner target_blaster
target_crosslevel_trigger target_crosslevel_target target_laser target_help target_actor
target_lightramp target_earthquake target_character target_string viewthing light light_mine1
light_mine2 info_null func_group info_notnull path_corner point_combat misc_explobox misc_banner
misc_satellite_dish misc_actor misc_gib_arm misc_gib_leg misc_gib_head misc_insane misc_deadsoldier
misc_viper misc_viper_bomb misc_bigviper misc_strogg_ship misc_teleporter misc_teleporter_dest
misc_blackhole misc_eastertank misc_easterchick misc_easterchick2 monster_berserk monster_gladiator
monster_gunner monster_infantry monster_soldier_light monster_soldier monster_soldier_ss monster_tank
monster_tank_commander monster_medic monster_flipper monster_chick monster_parasite monster_flyer
monster_brain monster_floater monster_hover monster_mutant monster_supertank monster_boss2
monster_boss3_stand monster_jorg monster_commander_body turret_breach turret_base turret_driver
weapon_shotgun weapon_railgun weapon_bfg ammo_cells ammo_shells item_quad item_invulnerability
item_power_shield key_blue_key key_data_cd item_armor_body info_player_team1 info_player_team2
item_flag_team1 item_flag_team2 misc_ctf_banner misc_ctf_small_banner trigger_teleport
info_teleport_destination item_tech1 worldspawn unknown_thing`)

var robustTargets = []string{"t1", "t2", "t3", "t4", "", "t1"}

var robustKeyVals = map[string][]string{
	"spawnflags":   {"0", "1", "2", "3", "4", "8", "16", "32", "64", "128", "256", "1024", "2048", "65535", "-1", "7"},
	"speed":        {"0", "-100", "1", "100", "10000", "1e30", "nan", "abc"},
	"accel":        {"0", "-5", "5", "1000"},
	"decel":        {"0", "-5", "5", "1000"},
	"wait":         {"0", "-1", "0.1", "2", "-5", "1e9"},
	"delay":        {"0", "0.1", "1", "-1"},
	"random":       {"0", "1", "5", "-1"},
	"count":        {"0", "1", "2", "-1", "100000", "2147483647"},
	"health":       {"0", "1", "10", "-5", "100000"},
	"dmg":          {"0", "1", "10", "1000", "-10"},
	"lip":          {"0", "8", "-100", "10000"},
	"distance":     {"0", "90", "-90", "360", "100000"},
	"height":       {"0", "8", "-64", "10000"},
	"angle":        {"0", "90", "-1", "-2", "45", "360", "1e9"},
	"angles":       {"0 0 0", "0 90 0", "90 0 0", "-90 0 0", "1 2", "x"},
	"style":        {"0", "1", "5", "31", "32", "-1", "255", "1000", "2048"},
	"sounds":       {"0", "1", "2", "3", "4", "5", "-1", "100"},
	"message":      {"", "hello", "%s%s%s%n", "abcdefghijklmnopqrstuvwxyz", "a\\nb", strings.Repeat("m", 600), "aa", "zz", "0", "15", "-1"},
	"map":          {"", "demo2", "demo1$start", "demo2$x", "*demo3", "base1", strings.Repeat("q", 100)},
	"item":         {"", "weapon_bfg", "item_quad", "ammo_cells", "key_blue_key", "nonexistent", "item_flag_team1"},
	"noise":        {"", "world/x.wav", "doors/dr1_strt.wav", strings.Repeat("s", 100)},
	"mass":         {"0", "100", "-1", "400"},
	"volume":       {"0", "1", "-1", "5"},
	"attenuation":  {"0", "1", "-1", "3"},
	"gravity":      {"0", "0.5", "-1", "abc"},
	"pausetime":    {"0", "1", "-1"},
	"move_origin":  {"0 0 0", "10 10 10"},
	"move_angles":  {"0 0 0", "0 90 0"},
	"light":        {"0", "300", "-1"},
	"minyaw":       {"0", "-30", "30"},
	"maxyaw":       {"0", "-30", "30", "360"},
	"minpitch":     {"0", "-30"},
	"maxpitch":     {"0", "30"},
	"target":       robustTargets,
	"targetname":   robustTargets,
	"killtarget":   robustTargets,
	"pathtarget":   robustTargets,
	"deathtarget":  robustTargets,
	"combattarget": robustTargets,
	"team":         {"", "a", "b", "a"},
	"sky":          {"", "unit1_"},
	"nextmap":      {"", "demo2"},
	"bogus":        {"1"},
	"_comment":     {"x"},
}

var robustKeys = func() []string {
	var ks []string
	for k := range robustKeyVals {
		ks = append(ks, k)
	}
	// deterministic order
	sortStrings(ks)
	return ks
}()

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

var (
	demoEntsOnce sync.Once
	demoEnts     string
	demoStart    [3]float64
	demoModels   int
)

// isBrushClass reports classnames whose spawn function sets a bsp model.
func isBrushClass(cn string) bool {
	return strings.HasPrefix(cn, "func_") || strings.HasPrefix(cn, "trigger_") || cn == "turret_breach" ||
		cn == "turret_base"
}

var robustBadModels = []string{"*0", "*999", "*-1", "*", "models/objects/barrels/tris.md2", "", "x"}

// demoEntities returns demo1's entity string with its monsters and the other
// non-essential entities stripped (worldspawn, player starts and lights kept),
// and the origin of its first info_player_start.
func demoEntities(t testing.TB) (string, [3]float64) {
	base := needDemoPak(t)
	demoEntsOnce.Do(func() {
		p, err := pak.Open(filepath.Join(base, "baseq2", "pak0.pak"))
		if err != nil {
			return
		}
		raw, err := p.ReadFile("maps/demo1.bsp")
		if err != nil {
			return
		}
		m, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
		if err != nil {
			return
		}
		demoModels = m.NumInlineModels()
		all := m.EntityString()
		blocks := regexp.MustCompile(`(?s)\{[^{}]*\}`).FindAllString(all, -1)
		var b strings.Builder
		for _, bl := range blocks {
			cn := regexp.MustCompile(`"classname"\s+"([^"]*)"`).FindStringSubmatch(bl)
			if cn == nil {
				continue
			}
			switch cn[1] {
			case "worldspawn", "info_player_start", "info_player_deathmatch", "info_player_coop",
				"info_player_intermission", "light":
				b.WriteString(bl + "\n")
			}
			if cn[1] == "info_player_start" && demoStart == [3]float64{} {
				o := regexp.MustCompile(`"origin"\s+"([^"]*)"`).FindStringSubmatch(bl)
				if o != nil {
					fmt.Sscan(o[1], &demoStart[0], &demoStart[1], &demoStart[2])
				}
			}
		}
		demoEnts = b.String()
	})
	if demoEnts == "" {
		t.Skip("demo1 entities not available")
	}
	return demoEnts, demoStart
}

func randomEntity(r *rand.Rand, start [3]float64) string {
	var b strings.Builder
	b.WriteString("{\n")
	cn := robustClassnames[r.Intn(len(robustClassnames))]
	for strings.HasPrefix(cn, "turret_") && r.Intn(10) != 0 {
		cn = robustClassnames[r.Intn(len(robustClassnames))]
	}
	fmt.Fprintf(&b, "\"classname\" \"%s\"\n", cn)
	switch {
	case r.Intn(100) == 0:
		fmt.Fprintf(&b, "\"model\" \"%s\"\n", robustBadModels[r.Intn(len(robustBadModels))])
	case isBrushClass(cn):
		fmt.Fprintf(&b, "\"model\" \"*%d\"\n", 1+r.Intn(demoModels-1))
	}
	fmt.Fprintf(&b, "\"origin\" \"%d %d %d\"\n", int(start[0])+r.Intn(512)-256, int(start[1])+r.Intn(512)-256,
		int(start[2])+r.Intn(96)-16)
	for n := r.Intn(8); n > 0; n-- {
		k := robustKeys[r.Intn(len(robustKeys))]
		vals := robustKeyVals[k]
		fmt.Fprintf(&b, "\"%s\" \"%s\"\n", k, vals[r.Intn(len(vals))])
	}
	b.WriteString("}\n")
	return b.String()
}

func randomEntString(r *rand.Rand, base string, start [3]float64) string {
	var b strings.Builder
	b.WriteString(base)
	for n := 1 + r.Intn(40); n > 0; n-- {
		b.WriteString(randomEntity(r, start))
	}
	return b.String()
}

// TestRobustEntities spawns random entity strings in every mode and plays
// them. Env Q2_ENTS_ITERS (default 6), Q2_ENTS_FRAMES (default 100),
// Q2_ENTS_SEED.
func TestRobustEntities(t *testing.T) {
	base, start := demoEntities(t)
	iters, frames := 6, 100
	if v := os.Getenv("Q2_ENTS_ITERS"); v != "" {
		fmt.Sscan(v, &iters)
	}
	if v := os.Getenv("Q2_ENTS_FRAMES"); v != "" {
		fmt.Sscan(v, &frames)
	}
	seed0 := int64(1)
	if v := os.Getenv("Q2_ENTS_SEED"); v != "" {
		fmt.Sscan(v, &seed0)
	}
	for _, mode := range []string{"sp", "coop", "dm", "ctf"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			found := map[string]string{}
			comerrs := map[string]int{}
			defer func() {
				for k, v := range comerrs {
					t.Logf("%4d x %s", v, k)
				}
			}()
			var order []string
			for it := 0; it < iters; it++ {
				seed := seed0 + int64(it)
				r := rand.New(rand.NewSource(seed))
				ents := randomEntString(r, base, start)
				var s *Server
				msg := catchPanic(func() { s = newRobustServer(t, robustModes[mode], ents) })
				if msg == "" {
					msg = monkey(s, r, frames, nil)
				}
				site := classify(msg)
				if site == "" {
					if msg != "" {
						comerrs[firstLine(msg[strings.Index(msg, "comerror"):])]++
					}
					continue
				}
				if _, dup := found[site]; !dup {
					order = append(order, site)
					found[site] = fmt.Sprintf("seed %d: %s\nentities:\n%s", seed, msg, ents[len(base):])
				}
			}
			for _, site := range order {
				t.Errorf("%s\n%s", site, found[site])
			}
		})
	}
}
