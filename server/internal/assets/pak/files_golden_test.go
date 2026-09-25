//go:build golden

package pak

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/testutil"
)

func TestGoldenPak(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.FixturesDir(), "core", "pak", "*.json"))
	if len(files) == 0 {
		t.Skip("no core/pak fixtures")
	}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".json")
		t.Run(name, func(t *testing.T) {
			pakPath := testutil.RequireFile(t, filepath.Join(testutil.BaseDir(), "baseq2", name+".pak"))
			var g struct {
				Files []struct {
					Name    string `json:"name"`
					FilePos int32  `json:"filepos"`
					FileLen int32  `json:"filelen"`
					SHA256  string `json:"sha256"`
				} `json:"files"`
			}
			if err := testutil.ReadJSON(path, &g); err != nil {
				t.Fatal(err)
			}
			p, err := Open(pakPath)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			if len(p.List()) != len(g.Files) {
				t.Fatalf("%d files, want %d", len(p.List()), len(g.Files))
			}
			for i, want := range g.Files {
				got := p.List()[i]
				if got.Name != testutil.Latin1(want.Name) || got.FilePos != want.FilePos || got.FileLen != want.FileLen {
					t.Fatalf("entry %d: got %+v want %+v", i, got, want)
				}
				b, err := p.ReadEntry(i)
				if err != nil {
					t.Fatalf("entry %d (%s): %v", i, got.Name, err)
				}
				sum := sha256.Sum256(b)
				if hex.EncodeToString(sum[:]) != want.SHA256 {
					t.Fatalf("entry %d (%s): sha256 mismatch", i, got.Name)
				}
			}
			t.Logf("%s: %d entries match", name, len(g.Files))
		})
	}
}
