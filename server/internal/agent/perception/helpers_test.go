package perception

import (
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

// demoFS opens the demo pak (skipping without it). Perception tests do not
// use the session helpers, so this test binary links no server code.
func demoFS(t testing.TB) *pak.FS {
	t.Helper()
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { _ = fs.Close() })
	return fs
}
