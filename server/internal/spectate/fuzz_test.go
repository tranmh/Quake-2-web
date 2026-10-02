package spectate

import (
	"testing"

	"quake2web/server/internal/q2const"
)

// FuzzRelayViewerPacket feeds arbitrary viewer datagrams to a relay serving
// a live viewer, a stalled viewer and a fresh connection, as connectionless
// data, as raw sequenced data and as the payload of the live viewer's
// netchan. The relay must never panic, the bot's stream must never block
// (one of its feeds is never drained, one viewer's queue stays full), and a
// viewer that is still connected afterwards keeps getting exactly the
// bot's frames.
func FuzzRelayViewerPacket(f *testing.F) {
	f.Add([]byte("\xff\xff\xff\xffgetchallenge\n"))
	f.Add([]byte("\xff\xff\xff\xffconnect 34 101 4321 \"\\name\\x\"\n"))
	f.Add([]byte("\xff\xff\xff\xffconnect 34 101 4321\n"))
	f.Add([]byte("\xff\xff\xff\xffconnect 99999999999 -1 4321 \"\"\n"))
	f.Add([]byte("\xff\xff\xff\xffrcon secret killserver\n"))
	f.Add([]byte{q2const.Clc_stringcmd, 'k', 'i', 'l', 'l', 's', 'e', 'r', 'v', 'e', 'r', 0})
	f.Add([]byte{q2const.Clc_stringcmd, 'b', 'e', 'g', 'i', 'n', ' ', '4', '2', 0})
	f.Add([]byte{q2const.Clc_stringcmd, 'n', 'e', 'w', 0, q2const.Clc_stringcmd, 'd', 'i', 's', 'c', 'o', 'n', 'n', 'e', 'c', 't', 0})
	f.Add([]byte("\x04configstrings 42 -2147483648\x00\x04baselines 42 999999\x00"))
	f.Add([]byte{q2const.Clc_move, 0, 0xff, 0xff, 0xff, 0xff, 1, 2, 3})
	f.Add([]byte{q2const.Clc_move, 0, 0xff})
	f.Add([]byte{q2const.Clc_userinfo, '\\', 'n', 0, q2const.Clc_nop, 200})
	f.Add([]byte{1, 0, 0, 0, 0, 0, 0, 0, 101, 0, q2const.Clc_nop})
	f.Add([]byte{0xff, 0xff, 0xff, 0x7f, 0xff, 0xff, 0xff, 0xff, 101, 0, q2const.Clc_stringcmd, 'n', 'e', 'w', 0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		rg := newRig(t, 0)
		rg.startLevel(newSynth(42, "fuzz", 3), 2)
		live := rg.addViewer(64)
		stalled := rg.addViewer(4)
		fresh := &rigViewer{rg: rg, q: &sliceQueue{cap: 4}, frames: map[frameKey]frameRec{}}
		fresh.v = &viewer{id: 99, out: fresh.q}
		rg.r.join(fresh.v)
		blocked := rg.stream.subscribe(1) // a hub that never reads its feed
		defer rg.stream.unsubscribe(blocked)
		stalled.stalled = true
		for len(stalled.q.items) < stalled.q.cap {
			rg.botFrame(nil)
		}

		rg.r.now = rg.now
		rg.r.packet(fresh.v, data)
		rg.r.packet(stalled.v, data)
		rg.r.packet(live.v, data)
		rg.pump()
		if len(data) <= q2const.MAX_MSGLEN-16 && live.err == nil && !live.v.removed {
			live.c.Netchan.Transmit(data, rg.ms())
			rg.pump()
		}
		if live.err == nil && !live.v.removed && len(data) >= 2 {
			// the same bytes as reliable data (a stringcmd stream)
			live.c.Netchan.Message.SZ_Write(data[:min(len(data), 200)])
			live.c.Netchan.Transmit(nil, rg.ms())
			rg.pump()
		}

		for i := 0; i < 5; i++ {
			rg.botFrame(nil)
		}
		rg.tick(0)
		if live.err == nil && !live.v.removed && live.v.state == vsSpawned {
			live.requireMatch(t)
			// it still gets the bot's frames (after a resync if it asked)
			rg.botFrame(nil)
			rg.botFrame(nil)
			live.requireMatch(t)
			if _, ok := live.frames[frameKey{42, rg.srv.frame}]; !ok && live.v.resync == "" && !live.v.waitDrain {
				t.Fatalf("live viewer stopped getting frames (client state %d)", live.c.State)
			}
		}
	})
}
