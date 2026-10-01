package demo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// blocks splits a .dm2 stream into its blocks.
func blocks(t *testing.T, data []byte) [][]byte {
	t.Helper()
	var out [][]byte
	r := NewReader(bytes.NewReader(data))
	for {
		b, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
}

// bigHeader has enough configstrings and baselines to need several blocks.
func bigHeader() *Header {
	h := &Header{ServerCount: 0x10000 + 4242, AttractLoop: 1, GameDir: "", PlayerNum: 0, LevelName: "The Outer Base"}
	h.ConfigStrings[q2const.CS_NAME] = "The Outer Base"
	h.ConfigStrings[q2const.CS_MODELS+1] = "maps/demo1.bsp"
	for i := 0; i < 200; i++ {
		h.ConfigStrings[q2const.CS_SOUNDS+1+i] = fmt.Sprintf("world/sound%03d_%s.wav", i, strings.Repeat("x", i%40))
	}
	for i := 1; i < 300; i++ {
		h.Baselines[i] = shared.EntityState{Number: int32(i), ModelIndex: int32(1 + i%200), Frame: int32(i % 7),
			Origin: shared.Vec3{float32(i), float32(-i), 24}, Angles: shared.Vec3{0, float32(i%128) * 360 / 256, 0}}
		h.HasBaseline[i] = true
	}
	h.Baselines[400] = shared.EntityState{Number: 400, Origin: shared.Vec3{1, 2, 3}} // no model: not written
	return h
}

func TestWriterHeaderBlocks(t *testing.T) {
	h := bigHeader()
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Begin(h); err != nil {
		t.Fatal(err)
	}
	if err := w.End(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if got := int32(binary.LittleEndian.Uint32(data[len(data)-4:])); got != -1 {
		t.Fatalf("terminator %d", got)
	}
	bl := blocks(t, data)
	if len(bl) < 3 || w.Blocks() != len(bl) || w.Bytes() != int64(len(data)) {
		t.Fatalf("%d blocks (writer says %d), %d bytes (writer says %d)", len(bl), w.Blocks(), len(data), w.Bytes())
	}

	// the serverdata fields as CL_Record_f writes them
	r := msg.NewReader(bl[0])
	if r.MSG_ReadByte() != q2const.Svc_serverdata || r.MSG_ReadLong() != q2const.PROTOCOL_VERSION ||
		r.MSG_ReadLong() != 0x10000+4242 || r.MSG_ReadByte() != 1 || r.MSG_ReadString() != "" ||
		r.MSG_ReadShort() != 0 || r.MSG_ReadString() != "The Outer Base" {
		t.Fatal("serverdata fields")
	}

	// split rules: a block is written out only when the next command would
	// not fit (configstring: len+32, baseline: 64 bytes of room)
	for i, b := range bl {
		if len(b) > q2const.MAX_MSGLEN {
			t.Fatalf("block %d: %d bytes", i, len(b))
		}
		if i == len(bl)-1 {
			break
		}
		next := msg.NewReader(bl[i+1])
		switch cmd := next.MSG_ReadByte(); cmd {
		case q2const.Svc_configstring:
			next.MSG_ReadShort()
			cs := next.MSG_ReadString()
			if len(b)+len(cs)+32 <= q2const.MAX_MSGLEN {
				t.Errorf("block %d (%d bytes) split before a %d byte configstring that fit", i, len(b), len(cs))
			}
		case q2const.Svc_spawnbaseline:
			if len(b)+64 <= q2const.MAX_MSGLEN {
				t.Errorf("block %d (%d bytes) split before a baseline that fit", i, len(b))
			}
		default:
			t.Fatalf("block %d starts with %d", i+1, cmd)
		}
	}
	last := bl[len(bl)-1]
	if !bytes.HasSuffix(last, append([]byte{q2const.Svc_stufftext}, "precache\n\x00"...)) {
		t.Fatalf("header does not end with the precache stufftext: %x", last[len(last)-12:])
	}

	// a player parsing the header gets the same state back
	p := fakeclient.NewPassive(fakeclient.Options{})
	for i, b := range bl {
		if _, err := p.FeedPayload(b); err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
	}
	if p.ConfigStrings != h.ConfigStrings {
		t.Fatal("configstrings differ after parsing the header")
	}
	for i := range h.Baselines {
		want := h.Baselines[i]
		has := want.ModelIndex != 0
		if !has {
			want = shared.EntityState{}
		}
		got := p.Entities[i].Baseline
		got.OldOrigin = want.OldOrigin // CL_ParseDelta sets old_origin from the null state
		if p.Entities[i].HasBaseline != has || got != want {
			t.Fatalf("baseline %d: %+v (has %v) want %+v", i, p.Entities[i].Baseline, p.Entities[i].HasBaseline, want)
		}
	}
	if p.ServerData.AttractLoop != 1 || p.ServerData.ServerCount != 0x10000+4242 || len(p.StuffTexts) != 1 {
		t.Fatalf("serverdata %+v stuff %q", p.ServerData, p.StuffTexts)
	}
}

func TestWriterMessagesAndErrors(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	if err := w.Message([]byte{1}); err == nil {
		t.Fatal("Message before Begin")
	}
	h := &Header{AttractLoop: 1}
	if err := w.Begin(h); err != nil {
		t.Fatal(err)
	}
	if err := w.Begin(h); err == nil {
		t.Fatal("Begin twice")
	}
	payloads := [][]byte{{q2const.Svc_nop}, {}, bytes.Repeat([]byte{q2const.Svc_nop}, q2const.MAX_MSGLEN-8)}
	for _, p := range payloads {
		if err := w.Message(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.End(); err != nil {
		t.Fatal(err)
	}
	if err := w.Message([]byte{1}); err == nil {
		t.Fatal("Message after End")
	}
	bl := blocks(t, buf.Bytes())
	if len(bl) != 1+len(payloads) {
		t.Fatalf("%d blocks", len(bl))
	}
	for i, p := range payloads {
		if !bytes.Equal(bl[1+i], p) {
			t.Fatalf("message %d not written verbatim", i)
		}
	}

	// a configstring longer than a message cannot be written (C: ERR_FATAL)
	h2 := &Header{AttractLoop: 1}
	h2.ConfigStrings[q2const.CS_STATUSBAR] = strings.Repeat("x", q2const.MAX_MSGLEN)
	if err := NewWriter(io.Discard).Begin(h2); err == nil {
		t.Fatal("oversized configstring accepted")
	}

	// write errors are sticky
	fw := NewWriter(failWriter{})
	if err := fw.Begin(&Header{}); err == nil || fw.End() == nil {
		t.Fatal("write error not reported")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestReaderEnds(t *testing.T) {
	block := func(n int32, data ...byte) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(n))
		return append(b, data...)
	}
	cat := func(parts ...[]byte) []byte { return bytes.Join(parts, nil) }
	for name, tc := range map[string]struct {
		data       []byte
		blocks     int
		terminated bool
		err        string
	}{
		"terminated":      {cat(block(1, 7), block(0), block(-1)), 2, true, ""},
		"data after end":  {cat(block(1, 7), block(-1), block(1, 9)), 1, true, ""},
		"no terminator":   {cat(block(2, 7, 7)), 1, false, ""},
		"empty":           {nil, 0, false, ""},
		"truncated len":   {cat(block(1, 7), []byte{1, 0}), 1, false, "unexpected EOF"},
		"truncated block": {cat(block(5, 1, 2)), 0, false, "unexpected EOF"},
		"too long":        {cat(block(q2const.MAX_MSGLEN + 1)), 0, false, "MAX_MSGLEN"},
		"negative":        {cat(block(-2)), 0, false, "out of range"},
	} {
		r := NewReader(bytes.NewReader(tc.data))
		n := 0
		var err error
		for {
			if _, err = r.Next(); err != nil {
				break
			}
			n++
		}
		if errors.Is(err, io.EOF) {
			err = nil
		}
		if n != tc.blocks || r.Terminated() != tc.terminated ||
			(tc.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.err)) {
			t.Errorf("%s: %d blocks terminated %v err %v", name, n, r.Terminated(), err)
		}
		if _, err := r.Next(); !errors.Is(err, io.EOF) {
			t.Errorf("%s: Next after the end: %v", name, err)
		}
	}
}

// synthetic server messages for a passive client

func serverdata(w *msg.SizeBuf, count int32, level string) {
	w.MSG_WriteByte(q2const.Svc_serverdata)
	w.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	w.MSG_WriteLong(count)
	w.MSG_WriteByte(0)
	w.MSG_WriteString("")
	w.MSG_WriteShort(0)
	w.MSG_WriteString(level)
}

// frame writes a svc_frame with one entity, deltaed from delta (or from
// the baseline when delta <= 0).
func frame(w *msg.SizeBuf, num, delta int32, x float32) {
	w.MSG_WriteByte(q2const.Svc_frame)
	w.MSG_WriteLong(num)
	w.MSG_WriteLong(delta)
	w.MSG_WriteByte(0)
	w.MSG_WriteByte(0) // no areabits
	var null shared.PlayerState
	ps := shared.PlayerState{}
	ps.PMove.Origin = [3]int16{int16(x * 8), 0, 0}
	w.WriteDeltaPlayerstate(&null, &ps)
	w.MSG_WriteByte(q2const.Svc_packetentities)
	base := shared.EntityState{Number: 1, ModelIndex: 1}
	cur := shared.EntityState{Number: 1, ModelIndex: 1, Origin: shared.Vec3{x, 0, 0}}
	w.MSG_WriteDeltaEntity(&base, &cur, false, true)
	w.MSG_WriteShort(0)
}

type synth struct {
	t   *testing.T
	p   *fakeclient.Client
	all [][]byte
}

func (s *synth) feed(build func(w *msg.SizeBuf)) []byte {
	s.t.Helper()
	w := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	build(w)
	b := append([]byte(nil), w.Bytes()...)
	if _, err := s.p.FeedPayload(b); err != nil {
		s.t.Fatal(err)
	}
	s.all = append(s.all, b)
	return b
}

func (s *synth) level(count int32, mapname string) {
	s.feed(func(w *msg.SizeBuf) {
		serverdata(w, count, mapname+" level")
		w.MSG_WriteByte(q2const.Svc_configstring)
		w.MSG_WriteShort(q2const.CS_NAME)
		w.MSG_WriteString(mapname + " level")
		w.MSG_WriteByte(q2const.Svc_configstring)
		w.MSG_WriteShort(q2const.CS_MODELS + 1)
		w.MSG_WriteString("maps/" + mapname + ".bsp")
		w.MSG_WriteByte(q2const.Svc_spawnbaseline)
		var null shared.EntityState
		w.MSG_WriteDeltaEntity(&null, &shared.EntityState{Number: 1, ModelIndex: 1}, true, true)
	})
}

func stuff(text string) func(w *msg.SizeBuf) {
	return func(w *msg.SizeBuf) {
		w.MSG_WriteByte(q2const.Svc_stufftext)
		w.MSG_WriteString(text)
	}
}

func TestRecorderSplitsLevels(t *testing.T) {
	files := MemFiles{}
	rec := NewRecorder(files.Create)
	p := fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rec.OnServerMessage})
	s := &synth{t: t, p: p}

	s.level(7, "demo1")
	s.feed(stuff("precache 7\n"))
	if rec.Recording() || len(rec.Files()) != 0 {
		t.Fatal("recording before the first frame")
	}
	var want [][]byte
	want = append(want, s.feed(func(w *msg.SizeBuf) { frame(w, 10, -1, 1) }))
	for f := int32(11); f < 15; f++ {
		want = append(want, s.feed(func(w *msg.SizeBuf) { frame(w, f, f-1, float32(f)) }))
	}
	if !rec.Recording() {
		t.Fatal("not recording after a full frame")
	}
	// a level change on the connection: the file ends before "changing"
	s.feed(func(w *msg.SizeBuf) {
		frame(w, 15, 14, 15)
		stuff("changing\n")(w)
	})
	if rec.Recording() {
		t.Fatal("still recording after changing")
	}
	s.feed(stuff("reconnect\n"))

	// the next level waits for its first full frame again
	s.level(7, "demo2")
	s.feed(func(w *msg.SizeBuf) { frame(w, 3, 2, 1) }) // a delta (a lost full frame): not a start
	if rec.Recording() {
		t.Fatal("started at a delta frame")
	}
	want2 := [][]byte{s.feed(func(w *msg.SizeBuf) { frame(w, 4, -1, 2) })}
	want2 = append(want2, s.feed(func(w *msg.SizeBuf) { frame(w, 5, 4, 3) }))
	// a load (svc_reconnect) ends it as well
	s.feed(func(w *msg.SizeBuf) { w.MSG_WriteByte(q2const.Svc_reconnect) })
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	if got := rec.Files(); len(got) != 2 || got[0] != "00-demo1.dm2" || got[1] != "01-demo2.dm2" {
		t.Fatalf("files %v", got)
	}
	for name, msgs := range map[string][][]byte{"00-demo1.dm2": want, "01-demo2.dm2": want2} {
		data := files[name].Bytes()
		st, err := Validate(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		bl := blocks(t, data)
		got := bl[st.HeaderBlocks:]
		if len(got) != len(msgs) {
			t.Fatalf("%s: %d messages, want %d", name, len(got), len(msgs))
		}
		for i := range msgs {
			if !bytes.Equal(got[i], msgs[i]) {
				t.Fatalf("%s: message %d not verbatim", name, i)
			}
		}
	}
}

func TestValidateRejects(t *testing.T) {
	// a good synthetic demo
	files := MemFiles{}
	rec := NewRecorder(files.Create)
	s := &synth{t: t, p: fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rec.OnServerMessage})}
	s.level(3, "demo1")
	s.feed(func(w *msg.SizeBuf) { frame(w, 1, -1, 1) })
	s.feed(func(w *msg.SizeBuf) { frame(w, 2, 1, 2) })
	s.feed(func(w *msg.SizeBuf) { frame(w, 3, 2, 3) })
	_ = rec.Close()
	good := files["00-demo1.dm2"].Bytes()
	st, err := Validate(good)
	if err != nil {
		t.Fatal(err)
	}
	if st.Frames != 3 || st.FirstFrame != 1 || st.LastFrame != 3 || !st.Terminated ||
		st.ServerCount != 0x10000+3 || st.Map != "maps/demo1.bsp" || st.LevelName != "demo1 level" {
		t.Fatalf("stats %+v", st)
	}
	bl := blocks(t, good)
	build := func(parts ...[]byte) []byte {
		var b bytes.Buffer
		w := &Writer{w: &b, begun: true}
		for _, p := range parts {
			_ = w.writeBlock(p)
		}
		_ = w.End()
		return b.Bytes()
	}
	hdr := bl[:st.HeaderBlocks]
	frames := bl[st.HeaderBlocks:]
	withHdr := func(rest ...[]byte) []byte { return build(append(append([][]byte(nil), hdr...), rest...)...) }

	for name, tc := range map[string]struct {
		data []byte
		err  string
	}{
		"no terminator":    {good[:len(good)-4], "terminator"},
		"no serverdata":    {build(frames...), "serverdata"},
		"lost frame":       {withHdr(frames[0], frames[2]), "does not hold"},
		"disconnect":       {withHdr(frames[0], []byte{q2const.Svc_disconnect}), "disconnected"},
		"reconnect":        {withHdr(frames[0], []byte{q2const.Svc_reconnect}), "svc_reconnect"},
		"stuffed changing": {withHdr(frames[0], append([]byte{q2const.Svc_stufftext}, "cmd x;changing\n\x00"...)), "changing"},
		"no frames":        {withHdr(), "no frames"},
		"empty":            {build(), "empty"},
	} {
		if _, err := Validate(tc.data); err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// a server demo (serverrecord, attractloop 2) is not a client demo
	sd := append([]byte(nil), bl[0]...)
	sd[9] = 2
	if _, err := Validate(build(append([][]byte{sd}, bl[1:]...)...)); err == nil || !strings.Contains(err.Error(), "attract") {
		t.Errorf("server demo: %v", err)
	}
}

func TestRecorderErrors(t *testing.T) {
	rec := NewRecorder(func(string) (io.WriteCloser, error) { return nil, errors.New("no space") })
	s := &synth{t: t, p: fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rec.OnServerMessage})}
	s.level(3, "demo1")
	s.feed(func(w *msg.SizeBuf) { frame(w, 1, -1, 1) })
	if rec.Err() == nil || rec.Recording() {
		t.Fatal("create error not reported")
	}
	s.feed(func(w *msg.SizeBuf) { frame(w, 2, 1, 2) }) // ignored after the error
	if err := rec.Close(); err == nil || !strings.Contains(err.Error(), "no space") {
		t.Fatalf("close: %v", err)
	}

	// a level name that is not a file name
	files := MemFiles{}
	rec = NewRecorder(files.Create)
	s = &synth{t: t, p: fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rec.OnServerMessage})}
	s.feed(func(w *msg.SizeBuf) { serverdata(w, 1, "../x y.cin") })
	s.feed(func(w *msg.SizeBuf) { frame(w, 1, -1, 1) })
	_ = rec.Close()
	if got := rec.Files(); len(got) != 1 || got[0] != "00-.._x_y.cin.dm2" {
		t.Fatalf("files %v", got)
	}
}

func TestWriterAllBaselines(t *testing.T) {
	h := &Header{AttractLoop: 1}
	h.Baselines[3] = shared.EntityState{Number: 3, ModelIndex: 2}
	h.Baselines[7] = shared.EntityState{Number: 7, Sound: 12, Origin: shared.Vec3{64, 0, 8}} // a looping speaker
	h.HasBaseline[3], h.HasBaseline[7] = true, true
	h.Baselines[9] = shared.EntityState{Number: 9, Origin: shared.Vec3{1, 1, 1}} // never received
	parse := func(all bool) *fakeclient.Client {
		var buf bytes.Buffer
		w := NewWriter(&buf)
		w.AllBaselines = all
		if err := w.Begin(h); err != nil {
			t.Fatal(err)
		}
		p := fakeclient.NewPassive(fakeclient.Options{})
		for _, b := range blocks(t, buf.Bytes()) {
			if _, err := p.FeedPayload(b); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	c := parse(false) // CL_Record_f: only baselines with a model
	if !c.Entities[3].HasBaseline || c.Entities[7].HasBaseline || c.Entities[9].HasBaseline {
		t.Fatalf("C header baselines: %v %v %v", c.Entities[3].HasBaseline, c.Entities[7].HasBaseline, c.Entities[9].HasBaseline)
	}
	a := parse(true)
	if !a.Entities[7].HasBaseline || a.Entities[7].Baseline.Sound != 12 || a.Entities[7].Baseline.Origin != h.Baselines[7].Origin ||
		a.Entities[9].HasBaseline {
		t.Fatalf("AllBaselines: %+v (9: %v)", a.Entities[7], a.Entities[9].HasBaseline)
	}
}
