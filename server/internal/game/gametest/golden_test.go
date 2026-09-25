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
// The other scenarios are run and their progress is logged (single-player
// scenarios diverge once monsters, not ported yet, act).
var mustPass = map[string]bool{
	"demo1_dm4": true,
}

// skipped scenarios (other game module).
var skipScenario = map[string]string{
	"ctf_demo1": "module ctf is not ported",
}

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
