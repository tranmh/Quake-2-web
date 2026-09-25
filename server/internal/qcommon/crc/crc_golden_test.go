//go:build golden

package crc

import (
	"encoding/json"
	"fmt"
	"testing"

	"quake2web/server/internal/testutil"
)

func TestGoldenCRC(t *testing.T) {
	path := testutil.Fixture(t, "core/msg/crc.jsonl")
	n := 0
	err := testutil.ReadJSONL(path, func(_ int, line []byte) error {
		var c struct {
			Base     *string `json:"base"`
			Sequence int32   `json:"sequence"`
			CRC      *int    `json:"crc"`
			Block    *string `json:"block"`
			CRC16    *int    `json:"crc16"`
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		n++
		switch {
		case c.Base != nil && c.CRC != nil:
			b := testutil.Hex(t, *c.Base)
			if got := COM_BlockSequenceCRCByte(b, c.Sequence); int(got) != *c.CRC {
				return fmt.Errorf("COM_BlockSequenceCRCByte(%s, %d) = %d, want %d", *c.Base, c.Sequence, got, *c.CRC)
			}
		case c.Block != nil && c.CRC16 != nil:
			b := testutil.Hex(t, *c.Block)
			if got := CRC_Block(b); int(got) != *c.CRC16 {
				return fmt.Errorf("CRC_Block(%s) = %d, want %d", *c.Block, got, *c.CRC16)
			}
		default:
			return fmt.Errorf("unrecognized line %s", line)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("first mismatch: %v", err)
	}
	t.Logf("%d cases OK", n)
}
