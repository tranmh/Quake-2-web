//go:build golden

package crand

import (
	"testing"

	"quake2web/server/internal/testutil"
)

func TestGoldenRand(t *testing.T) {
	for _, rel := range []string{"core/rand.json", "core/rand-12345.json"} {
		t.Run(rel, func(t *testing.T) {
			path := testutil.Fixture(t, rel)
			var f struct {
				Seed   uint32  `json:"seed"`
				Values []int32 `json:"values"`
			}
			if err := testutil.ReadJSON(path, &f); err != nil {
				t.Fatal(err)
			}
			r := New(f.Seed)
			for i, want := range f.Values {
				if got := r.Rand(); got != want {
					t.Fatalf("seed %d: value #%d = %d, want %d", f.Seed, i, got, want)
				}
			}
			t.Logf("seed %d: %d values OK", f.Seed, len(f.Values))
		})
	}
}
