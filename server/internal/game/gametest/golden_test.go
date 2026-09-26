//go:build golden

package gametest

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"quake2web/server/internal/testutil"
)

// mustPass lists the scenarios that must match the oracle on every frame.
// The other scenarios are run and their progress is logged.
var mustPass = map[string]bool{
	"demo1_dm4":       true,
	"demo1_coop2":     true,
	"demo1_sp_idle":   true,
	"demo1_sp_walk":   true,
	"demo1_sp_random": true,
	"demo2_sp_random": true,
	"demo3_sp_random": true,
	// synthetic monster scenarios (fixtures/scenarios/game/synth_*.json)
	"synth_actor":     true,
	"synth_boss2":     true,
	"synth_boss3":     true,
	"synth_brain":     true,
	"synth_chick":     true,
	"synth_flipper":   true,
	"synth_float":     true,
	"synth_gladiator": true,
	"synth_hover":     true,
	"synth_insane":    true,
	"synth_jorg":      true,
	"synth_medic":     true,
	"synth_mutant":    true,
	"synth_supertank": true,
	// ctf module scenarios (oracle_game_ctf; synthetic ones from
	// oracle/scripts/gen_ctf_scenarios.py)
	"ctf_demo1":        true,
	"ctf_flags":        true,
	"ctf_capturelimit": true,
	"ctf_grapple":      true,
	"ctf_techs":        true,
	"ctf_teams":        true,
	"ctf_match":        true,
	"ctf_vote":         true,
}

// skipped scenarios.
var skipScenario = map[string]string{}

// TestGoldenGame runs every game fixture. Env Q2_GAME_SCENARIO restricts the
// run to one scenario, Q2_GAME_FRAMES limits the frames.
func TestGoldenGame(t *testing.T) {
	if _, err := os.Stat(filepath.Join(testutil.BaseDir(), "baseq2", "pak0.pak")); err != nil {
		t.Skip("demo pak not available")
	}
	only := os.Getenv("Q2_GAME_SCENARIO")
	maxFrames, _ := strconv.Atoi(os.Getenv("Q2_GAME_FRAMES"))
	for _, name := range fixtureScenarios(t) {
		name := name
		if only != "" && only != name {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if why, ok := skipScenario[name]; ok {
				t.Skip(why)
			}
			res, err := Run(name, Options{MaxFrames: maxFrames})
			if err != nil {
				t.Fatal(err)
			}
			if res.Divergence == nil {
				t.Logf("%s", res)
				return
			}
			if mustPass[name] {
				t.Errorf("%s", res)
				return
			}
			t.Logf("strict: %s", res)
			masked, err := Run(name, Options{MaxFrames: maxFrames, MaskMonsters: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("masked (monster edicts/events, rand_calls ignored): %s", masked)
		})
	}
}
