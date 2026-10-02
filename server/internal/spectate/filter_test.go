package spectate

import (
	"bytes"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// piece is one svc command written by the test, with its expected fate.
type piece struct {
	cmd   int32
	bytes []byte
}

// everyCommand returns a frame followed by one of every svc command a
// server sends, as separate pieces.
func everyCommand(t *testing.T, s *synth) []piece {
	t.Helper()
	var out []piece
	add := func(cmd int32, write func(m *msg.SizeBuf)) {
		m := msg.NewSizeBuf(1 << 14)
		m.MSG_WriteByte(cmd)
		write(m)
		out = append(out, piece{cmd, append([]byte(nil), m.Bytes()...)})
	}
	s.advance()
	frame := s.frameMsg(s.frame-1, nil)
	out = append(out, piece{q2const.Svc_frame, frame})
	add(q2const.Svc_nop, func(*msg.SizeBuf) {})
	add(q2const.Svc_print, func(m *msg.SizeBuf) { m.MSG_WriteByte(q2const.PRINT_HIGH); m.MSG_WriteString("hello\n") })
	add(q2const.Svc_centerprint, func(m *msg.SizeBuf) { m.MSG_WriteString("center") })
	add(q2const.Svc_stufftext, func(m *msg.SizeBuf) { m.MSG_WriteString("cmd putaway\n") })
	add(q2const.Svc_configstring, func(m *msg.SizeBuf) { m.MSG_WriteShort(q2const.CS_LIGHTS + 1); m.MSG_WriteString("abc") })
	add(q2const.Svc_sound, func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.SND_ENT | q2const.SND_POS)
		m.MSG_WriteByte(3)
		m.MSG_WriteShort(2<<3 | 1)
		m.MSG_WritePos(shared.Vec3{1, 2, 3})
	})
	add(q2const.Svc_temp_entity, func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.TE_GUNSHOT)
		m.MSG_WritePos(shared.Vec3{8, 8, 8})
		m.MSG_WriteDir(&shared.Vec3{0, 0, 1})
	})
	add(q2const.Svc_muzzleflash, func(m *msg.SizeBuf) { m.MSG_WriteShort(1); m.MSG_WriteByte(q2const.MZ_BLASTER) })
	add(q2const.Svc_muzzleflash2, func(m *msg.SizeBuf) { m.MSG_WriteShort(2); m.MSG_WriteByte(1) })
	add(q2const.Svc_layout, func(m *msg.SizeBuf) { m.MSG_WriteString("xv 0 yv 0 string hi") })
	add(q2const.Svc_inventory, func(m *msg.SizeBuf) {
		for i := 0; i < q2const.MAX_ITEMS; i++ {
			m.MSG_WriteShort(int32(i % 3))
		}
	})
	add(q2const.Svc_download, func(m *msg.SizeBuf) { m.MSG_WriteShort(-1); m.MSG_WriteByte(0) })
	add(q2const.Svc_spawnbaseline, func(m *msg.SizeBuf) {
		var null shared.EntityState
		m.MSG_WriteDeltaEntity(&null, &shared.EntityState{Number: 600, ModelIndex: 3}, true, true)
	})
	add(q2const.Svc_reconnect, func(*msg.SizeBuf) {})
	return out
}

func join(ps []piece) ([]byte, []fakeclient.Span) {
	var payload []byte
	var spans []fakeclient.Span
	for _, p := range ps {
		spans = append(spans, fakeclient.Span{Cmd: p.cmd, Start: len(payload), End: len(payload) + len(p.bytes)})
		payload = append(payload, p.bytes...)
	}
	return payload, spans
}

func TestFilterPayload(t *testing.T) {
	s := newSynth(42, "filter", 3)
	bot := fakeclient.NewPassive(fakeclient.Options{})
	for _, p := range s.handshake() {
		if _, err := bot.FeedPayload(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bot.FeedPayload(s.frameMsg(-1, nil)); err != nil {
		t.Fatal(err)
	}
	pieces := everyCommand(t, s)
	payload, spans := join(pieces)

	// the client finds the same spans
	got, err := bot.FeedPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(spans) {
		t.Fatalf("client spans %v, want %v", got, spans)
	}
	for i := range got {
		if got[i] != spans[i] {
			t.Fatalf("span %d: %v, want %v", i, got[i], spans[i])
		}
	}

	var want []byte
	kept := map[int32]bool{}
	for _, p := range pieces {
		if Forwarded(p.cmd) {
			want = append(want, p.bytes...)
			kept[p.cmd] = true
		}
	}
	for _, cmd := range []int32{q2const.Svc_frame, q2const.Svc_print, q2const.Svc_centerprint, q2const.Svc_configstring,
		q2const.Svc_sound, q2const.Svc_temp_entity, q2const.Svc_muzzleflash, q2const.Svc_muzzleflash2,
		q2const.Svc_layout, q2const.Svc_inventory} {
		if !kept[cmd] {
			t.Errorf("svc %d not forwarded", cmd)
		}
	}
	for _, cmd := range []int32{q2const.Svc_nop, q2const.Svc_stufftext, q2const.Svc_download, q2const.Svc_spawnbaseline,
		q2const.Svc_reconnect, q2const.Svc_disconnect, q2const.Svc_serverdata, q2const.Svc_bad,
		q2const.Svc_playerinfo, q2const.Svc_packetentities, q2const.Svc_deltapacketentities, 99} {
		if Forwarded(cmd) {
			t.Errorf("svc %d forwarded", cmd)
		}
	}
	if out := FilterPayload(payload, spans); !bytes.Equal(out, want) {
		t.Fatalf("filtered\n%x\nwant\n%x", out, want)
	}
	// spans outside the payload are skipped
	bad := append(append([]fakeclient.Span(nil), spans...), fakeclient.Span{Cmd: q2const.Svc_print, Start: len(payload) - 2, End: len(payload) + 5},
		fakeclient.Span{Cmd: q2const.Svc_print, Start: -1, End: 3}, fakeclient.Span{Cmd: q2const.Svc_print, Start: 4, End: 4})
	if out := FilterPayload(payload, bad); !bytes.Equal(out, want) {
		t.Fatal("bad spans not skipped")
	}
}

// TestRelayForwardsFilteredPayload checks the wire: a live viewer that needs
// no resync gets each bot message as exactly FilterPayload's bytes behind
// the netchan header.
func TestRelayForwardsFilteredPayload(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "wire", 4), 3)
	rv := rg.addViewer(64)
	rg.botFrame(nil)
	rg.botFrame(nil)
	pieces := everyCommand(t, rg.srv)
	pieces = pieces[:len(pieces)-1] // without svc_reconnect: the bot stays connected
	payload, spans := join(pieces)
	rg.feed(payload)
	select {
	case d := <-rg.sub.ch:
		rg.r.now = rg.now
		rg.r.bot(d)
	default:
		t.Fatal("no message on the feed")
	}
	if len(rv.q.items) != 1 {
		t.Fatalf("%d datagrams", len(rv.q.items))
	}
	if got, want := rv.q.items[0][8:], FilterPayload(payload, spans); !bytes.Equal(got, want) {
		t.Fatalf("forwarded\n%x\nwant\n%x", got, want)
	}
	rg.pump()
	rv.requireMatch(t)
	if rv.c.ConfigStrings[q2const.CS_LIGHTS+1] != "abc" || rv.c.Prints[len(rv.c.Prints)-1] != "hello\n" ||
		rv.c.CenterPrints[0] != "center" || rv.c.Layouts[0] != "xv 0 yv 0 string hi" || rv.c.Inventory[4] != 1 ||
		len(rv.c.Sounds) != 1 || len(rv.c.TempEntEvents) != 1 || len(rv.c.MuzzleFlashes) != 2 {
		t.Fatal("forwarded events not parsed by the viewer")
	}
	if len(rv.c.Downloads) != 0 || rv.c.NumBaselines != rg.bot.NumBaselines-1 {
		t.Fatal("bot-only commands reached the viewer")
	}
}
