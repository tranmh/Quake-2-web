package fakeclient

import (
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
)

// TestInventoryCount: every svc_inventory replaces Inventory and bumps
// Counts.Inventory, so an unchanged inventory that was re-sent still counts
// as fresh.
func TestInventoryCount(t *testing.T) {
	p := NewPassive(Options{})
	inv := func(v int32) []byte {
		w := msg.NewSizeBuf(q2const.MAX_MSGLEN)
		w.MSG_WriteByte(q2const.Svc_inventory)
		for i := 0; i < q2const.MAX_ITEMS; i++ {
			w.MSG_WriteShort(v * int32(i%3))
		}
		return w.Bytes()
	}
	for n, v := range []int32{2, 2, 5} {
		if _, err := p.FeedPayload(inv(v)); err != nil {
			t.Fatal(err)
		}
		if p.Counts.Inventory != uint64(n+1) {
			t.Fatalf("after %d messages Counts.Inventory = %d", n+1, p.Counts.Inventory)
		}
		if p.Inventory[1] != v || p.Inventory[2] != 2*v || p.Inventory[3] != 0 {
			t.Fatalf("inventory %v", p.Inventory[:4])
		}
	}
}
