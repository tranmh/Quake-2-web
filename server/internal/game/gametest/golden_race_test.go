//go:build golden

package gametest

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"quake2web/server/internal/testutil"
)

// TestGoldenConcurrent runs every golden scenario at the same time in one
// process (the production server hosts many game instances): each must still
// match the oracle, and `go test -race -tags golden` reports any shared
// mutable state between instances. Env Q2_GAME_FRAMES limits the frames
// (default 120).
func TestGoldenConcurrent(t *testing.T) {
	if _, err := os.Stat(filepath.Join(testutil.BaseDir(), "baseq2", "pak0.pak")); err != nil {
		t.Skip("demo pak not available")
	}
	maxFrames := 120
	if v, err := strconv.Atoi(os.Getenv("Q2_GAME_FRAMES")); err == nil {
		maxFrames = v
	}
	for _, name := range fixtureScenarios(t) {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res, err := Run(name, Options{MaxFrames: maxFrames})
			if err != nil {
				t.Fatal(err)
			}
			if res.Divergence != nil && mustPass[name] {
				t.Errorf("%s", res)
			}
		})
	}
}
