//go:build golden

package md4

import (
	"encoding/json"
	"fmt"
	"testing"

	"quake2web/server/internal/testutil"
)

func TestGoldenMD4(t *testing.T) {
	path := testutil.Fixture(t, "core/msg/md4.jsonl")
	n := 0
	err := testutil.ReadJSONL(path, func(_ int, line []byte) error {
		var c struct {
			Block    string `json:"block"`
			Checksum uint32 `json:"checksum"`
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		n++
		if got := Com_BlockChecksum(testutil.Hex(t, c.Block)); got != c.Checksum {
			return fmt.Errorf("Com_BlockChecksum(%d bytes %s) = %d, want %d", len(c.Block)/2, c.Block, got, c.Checksum)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("first mismatch: %v", err)
	}
	t.Logf("%d cases OK", n)
}
