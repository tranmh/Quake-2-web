package sv

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crc"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// Fuzz targets for everything a remote peer can send: sequenced client
// messages (SV_ExecuteClientMessage and the game commands behind it) and
// connectionless packets. Runtime panics are bugs; Com_Error (a returned
// error) is the engine's own, C-faithful, handling.
//
//	go test ./internal/sv -run '^$' -fuzz FuzzClientMessage -fuzztime 3m
//	go test ./internal/sv -run '^$' -fuzz FuzzConnectionless -fuzztime 3m

func clcMove(lastframe int32, cmds [3]shared.UserCmd) []byte {
	b := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	b.MSG_WriteByte(q2const.Clc_move)
	b.MSG_WriteByte(0) // checksum (wrong: the commands are ignored)
	b.MSG_WriteLong(lastframe)
	var null shared.UserCmd
	b.MSG_WriteDeltaUsercmd(&null, &cmds[0])
	b.MSG_WriteDeltaUsercmd(&cmds[0], &cmds[1])
	b.MSG_WriteDeltaUsercmd(&cmds[1], &cmds[2])
	return b.Bytes()
}

func clcUserinfo(ui string) []byte {
	b := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	b.MSG_WriteByte(q2const.Clc_userinfo)
	b.MSG_WriteString(ui)
	return b.Bytes()
}

// fuzzPackets splits data into datagrams: [2-byte little-endian length % 1500][bytes]...
func fuzzPackets(data []byte) [][]byte {
	var out [][]byte
	for len(data) >= 2 && len(out) < 16 {
		n := int(binary.LittleEndian.Uint16(data)) % 1500
		data = data[2:]
		if n > len(data) {
			n = len(data)
		}
		out = append(out, data[:n])
		data = data[n:]
	}
	return out
}

func packPackets(pkts ...[]byte) []byte {
	var b []byte
	for _, p := range pkts {
		b = binary.LittleEndian.AppendUint16(b, uint16(len(p)))
		b = append(b, p...)
	}
	return b
}

func FuzzClientMessage(f *testing.F) {
	mv := [3]shared.UserCmd{{Msec: 50, ForwardMove: 200}, {Msec: 50, Angles: [3]int16{100, 2000, 0}}, {Msec: 100, Buttons: 1, UpMove: 200}}
	for _, mv := range [][]byte{
		{1, 0, 0, 0, 0xff, 0, 1, 0, 2, 0, 3, 200, 0, 0x38, 0xff, 0, 0, 1, 0, 100, 128, 0, 50, 0, 0, 100, 0},
		{0xff, 0xff, 0xff, 0x7f, 0x8f, 0xff, 0x7f, 0x00, 0x80, 0xff, 0x7f, 0xff, 0xff, 0x00, 0xff, 0x00, 0x80},
	} {
		f.Add(byte(4), packPackets(mv, mv, mv))
		f.Add(byte(5), packPackets(mv))
	}
	for _, mode := range []byte{0, 1, 2, 3} {
		f.Add(mode, packPackets(stringCmd("info")))
		f.Add(mode, packPackets(stringCmd("configstrings 0 0"), stringCmd("baselines 0 -5")))
		f.Add(mode, packPackets(stringCmd("download maps/synth.bsp"), stringCmd("nextdl")))
		f.Add(mode, packPackets(stringCmd("say hello $maxclients"), stringCmd("say_team \"x\"")))
		f.Add(mode, packPackets(stringCmd("give all"), stringCmd("god"), stringCmd("noclip"), stringCmd("kill")))
		f.Add(mode, packPackets(stringCmd("inven"), stringCmd("invnext"), stringCmd("invuse"), stringCmd("wave 3")))
		f.Add(mode, packPackets(stringCmd("use Blaster"), stringCmd("drop shells"), stringCmd("weapnext")))
		f.Add(mode, packPackets(stringCmd("players"), stringCmd("score"), stringCmd("help"), stringCmd("putaway")))
		f.Add(mode, packPackets(stringCmd("nextserver 0"), stringCmd("begin 0"), stringCmd("new")))
		f.Add(mode, packPackets(clcUserinfo("\\name\\zz\\skin\\female/athena\\rate\\1\\msg\\-5\\hand\\2\\fov\\0")))
		f.Add(mode, packPackets(clcMove(1, mv), clcMove(0x7fffffff, mv), clcMove(-1, mv)))
		f.Add(mode, packPackets(append(clcMove(3, mv), stringCmd("disconnect")...)))
		f.Add(mode, packPackets([]byte{0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0x80, 0x92, 0x10, 4, 'n', 'e', 'w', 0}))
	}
	strict := os.Getenv("Q2FUZZ_STRICT") == "1"
	f.Fuzz(func(t *testing.T, mode byte, data []byte) {
		s := newSynthServer(t, true)
		c := newTClient(t, s, "10.9.9.9", 7)
		c.connect(4242, "\\name\\fuzz\\skin\\male/grunt")
		if mode&1 == 0 {
			c.spawn()
		}
		for _, p := range fuzzPackets(data) {
			if mode&4 != 0 {
				// a clc_move built from the chunk with a valid checksum, so
				// the usercmds reach ClientThink / Pmove: lastframe(4 bytes)
				// then three raw MSG_ReadDeltaUsercmd bodies
				mv := []byte{q2const.Clc_move, 0}
				mv = append(mv, p...)
				mv[1] = crc.COM_BlockSequenceCRCByte(mv[2:], int32(c.ch.OutgoingSequence))
				p = mv
			}
			var err error
			if mode&2 != 0 && mode&4 == 0 {
				// the whole datagram, header included, from the client's address
				err = c.raw(p)
				c.oobReplies()
			} else {
				err = c.send(p)
			}
			if !s.SVS.Initialized {
				// a client of a running level should never be able to shut
				// the server down (Com_Error ERR_DROP); Q2FUZZ_STRICT=1
				// reports it
				if strict {
					t.Fatalf("client packet shut the server down: %v", err)
				}
				return
			}
			_, err = s.Frame(100)
			failOnInternal(t, err)
			if !s.SVS.Initialized && strict {
				t.Fatalf("frame after a client packet shut the server down: %v", err)
			}
			if err != nil || !s.SVS.Initialized {
				return
			}
			c.oobReplies()
		}
	})
}

func FuzzConnectionless(f *testing.F) {
	strict := os.Getenv("Q2FUZZ_STRICT") == "1"
	for _, q := range []string{
		"ping", "ack", "status", "info 34", "info", "getchallenge",
		"connect 34 1 0 \"\\name\\x\"", "connect 34 65535 0 \"\\name\\" + string(make([]byte, 600)) + "\"",
		"connect 34 1 0 \"\\\\\\\\\\ip\\1\"", "rcon  status", "rcon x map synth",
		"connect 34 -1 -1", "info -2147483648", "",
	} {
		f.Add([]byte(q), []byte(nil))
		f.Add([]byte(q), []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	}
	f.Fuzz(func(t *testing.T, oob []byte, after []byte) {
		s := newSynthServer(t, true)
		c := newTClient(t, s, "10.8.8.8", 3)
		// getchallenge first so "connect" gets past the challenge check
		_ = c.oob("getchallenge\n")
		var ch int
		for _, r := range c.oobReplies() {
			fmt.Sscanf(r, "challenge %d", &ch)
		}
		// half of the inputs become a "connect" with the valid challenge
		// (qport = first byte, rest = userinfo and trailing arguments)
		text := string(oob)
		if len(oob) > 0 && oob[0]&1 == 1 {
			text = fmt.Sprintf("connect %d %d %d %s", q2const.PROTOCOL_VERSION, int(oob[0]), ch, string(oob[1:]))
		}
		err := c.oob(text)
		c.oobReplies()
		if !s.SVS.Initialized {
			if strict {
				t.Fatalf("connectionless packet shut the server down: %v", err)
			}
			return
		}
		// then an arbitrary non-OOB datagram from the same address
		err = c.raw(after)
		c.oobReplies()
		if !s.SVS.Initialized {
			if strict {
				t.Fatalf("datagram shut the server down: %v", err)
			}
			return
		}
		_, err = s.Frame(100)
		failOnInternal(t, err)
		if !s.SVS.Initialized && strict {
			t.Fatalf("frame after connectionless packet shut the server down: %v", err)
		}
	})
}
