package spectate

import (
	"bytes"
	"context"
	"testing"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
)

// TestKeyframeMatchesServerEncoder compares the keyframe encoder with the
// real server encoder. The bot asks for uncompressed frames only
// (cl_nodelta), so every frame it gets is SV_WriteFrameToClient's output
// with no old frame. The keyframe of the bot's parsed frame must be the
// same bytes, up to fields the server flagged although their wire value
// equals the baseline (a float that moved by less than its quantum, e.g. a
// kick angle of 0.1): canonicalFrame removes exactly those from the server
// bytes, giving the server encoding of the state the client parsed.
// Frames whose server state is exactly representable must match as they
// are, and a client parsing a keyframe must end up with the bot's frame.
func TestKeyframeMatchesServerEncoder(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	stream := NewStream()
	frames, verbatim := 0, 0
	stream.AddSink(SinkFunc(func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
		for _, sp := range spans {
			if sp.Cmd != q2const.Svc_frame || !c.Frame.Valid {
				continue
			}
			if c.Frame.DeltaFrame > 0 {
				t.Fatalf("frame %d delta-coded from %d with cl_nodelta", c.Frame.ServerFrame, c.Frame.DeltaFrame)
			}
			l, f := stream.Level(), stream.Frame()
			got, err := AppendKeyframe(nil, f, l)
			if err != nil {
				t.Fatal(err)
			}
			frames++
			server := payload[sp.Start:sp.End]
			if bytes.Equal(got, server) {
				verbatim++
			}
			if want := canonicalFrame(t, server, l); !bytes.Equal(got, want) {
				t.Fatalf("frame %d: keyframe differs from the canonical server frame\n got %x\nwant %x\n raw %x",
					f.ServerFrame, got, want, server)
			}
			ps, ents := decodeKeyframe(t, l, got)
			if ps != f.PlayerState || !equalEntities(ents, f.Entities) {
				t.Fatalf("frame %d: the parsed keyframe differs from the bot's frame", f.ServerFrame)
			}
		}
	}))
	l := startBot(t, fs, 11, fakeclient.Options{NoDelta: true}, stream)
	walk := sessiontest.Walker()
	for i := 0; i < 300; i++ {
		if err := l.Step(context.Background(), walk); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d full frames: %d byte-identical as sent, all identical after canonicalisation", frames, verbatim)
	if frames < 300 || verbatim < frames/4 {
		t.Fatalf("%d frames, %d verbatim", frames, verbatim)
	}
}

// TestKeyframeReproducesDeltaFrames checks the property the relay relies on
// over a normal (delta-coded) bot run: a client parsing the keyframe of any
// frame the bot holds gets exactly the bot's frame, including state that
// only delta-coding leaves behind (stale gun offsets with gunframe 0).
func TestKeyframeReproducesDeltaFrames(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	stream := NewStream()
	frames := 0
	stream.AddSink(SinkFunc(func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
		f := stream.Frame()
		if f == nil || f.ServerFrame != c.Frame.ServerFrame || !c.Frame.Valid {
			return
		}
		frames++
		k, err := AppendKeyframe(nil, f, stream.Level())
		if err != nil {
			t.Fatal(err)
		}
		ps, ents := decodeKeyframe(t, stream.Level(), k)
		if ps != f.PlayerState || !equalEntities(ents, f.Entities) {
			t.Fatalf("frame %d: the parsed keyframe differs from the bot's frame\n%+v\n%+v", f.ServerFrame, ps, f.PlayerState)
		}
	}))
	l := startBot(t, fs, 12, fakeclient.Options{}, stream)
	walk := sessiontest.Walker()
	for i := 0; i < 200; i++ {
		if err := l.Step(context.Background(), walk); err != nil {
			t.Fatal(err)
		}
	}
	if frames < 200 {
		t.Fatalf("%d frames", frames)
	}
}

func TestKeyframeStaleGunOffsets(t *testing.T) {
	l := testLevel()
	f := &FrameSnapshot{ServerFrame: 7, DeltaFrame: 6, AreaBytes: 1, AreaBits: [32]byte{1}}
	f.PlayerState.GunIndex = 3
	f.PlayerState.GunOffset = shared.Vec3{0.25, -1, 2}
	f.PlayerState.GunAngles = shared.Vec3{0.5, 0, -0.75}
	f.Entities = []shared.EntityState{{Number: 1, ModelIndex: 255}, {Number: 5, ModelIndex: 2, Origin: shared.Vec3{8, 16, 24.125}}}
	k, err := AppendKeyframe(nil, f, l)
	if err != nil {
		t.Fatal(err)
	}
	ps, ents := decodeKeyframe(t, l, k)
	if ps != f.PlayerState || !equalEntities(ents, f.Entities) {
		t.Fatalf("got %+v\nwant %+v", ps, f.PlayerState)
	}
	// without the forced flag the server encoder would drop them
	plain := msg.NewSizeBuf(256)
	plain.WriteDeltaPlayerstate(nil, &f.PlayerState)
	if plain.Bytes()[1]&(q2const.PS_WEAPONFRAME>>8) != 0 {
		t.Fatal("WriteDeltaPlayerstate now sends the gun offsets with gunframe 0: writeKeyPlayerstate can go")
	}
}

func TestKeyframeRejectsBadEntities(t *testing.T) {
	l := testLevel()
	for _, ents := range [][]shared.EntityState{
		{{Number: 0}},
		{{Number: q2const.MAX_EDICTS}},
		{{Number: 3}, {Number: 2}},
		{{Number: 3}, {Number: 3}},
	} {
		if _, err := AppendKeyframe(nil, &FrameSnapshot{ServerFrame: 1, Entities: ents}, l); err == nil {
			t.Errorf("%v accepted", ents)
		}
	}
	// an oversized areabits length is clamped
	k, err := AppendKeyframe(nil, &FrameSnapshot{ServerFrame: 1, AreaBytes: 200}, l)
	if err != nil || k[10] != 32 {
		t.Fatalf("areabytes %d, %v", k[10], err)
	}
}

// decodeKeyframe parses a keyframe the way a client that holds l's
// baselines does (attractloop 0, so pm_type is kept).
func decodeKeyframe(t *testing.T, l *LevelSnapshot, frame []byte) (shared.PlayerState, []shared.EntityState) {
	t.Helper()
	c := fakeclient.NewPassive(fakeclient.Options{})
	start := msg.NewSizeBuf(1 << 20)
	writeServerData(start, l, 0)
	var null shared.EntityState
	for n := range l.base.Present {
		if l.base.Present[n] {
			b := l.base.State[n]
			b.Number = int32(n)
			start.MSG_WriteByte(q2const.Svc_spawnbaseline)
			start.MSG_WriteDeltaEntity(&null, &b, true, true)
		}
	}
	if _, err := c.FeedPayload(start.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FeedPayload(frame); err != nil {
		t.Fatalf("parse keyframe: %v", err)
	}
	if !c.Frame.Valid {
		t.Fatal("keyframe not valid")
	}
	return c.Frame.PlayerState, c.FrameEntities(&c.Frame)
}

func writeServerData(m *msg.SizeBuf, l *LevelSnapshot, attract int32) {
	m.MSG_WriteByte(q2const.Svc_serverdata)
	m.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	m.MSG_WriteLong(l.ServerCount)
	m.MSG_WriteByte(attract)
	m.MSG_WriteString(l.GameDir)
	m.MSG_WriteShort(l.PlayerNum)
	m.MSG_WriteString(l.LevelName)
}

func equalEntities(a, b []shared.EntityState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// canonicalFrame rewrites an uncompressed svc_frame without the fields the
// server flagged although their wire value equals the value delta-coded
// from (the baseline, or zero for the playerstate). Only quantized floats
// can do that: entity origins and angles, and the playerstate's view
// offset, view angles, kick angles, blend and fov.
func canonicalFrame(t *testing.T, b []byte, l *LevelSnapshot) []byte {
	t.Helper()
	m := msg.NewReader(b)
	out := msg.NewSizeBuf(maxKeyframe)
	copyN := func(n int) {
		if m.ReadCount+n > m.CurSize {
			t.Fatalf("truncated frame at %d", m.ReadCount)
		}
		out.SZ_Write(b[m.ReadCount : m.ReadCount+n])
		m.ReadCount += n
	}
	if m.MSG_ReadByte() != q2const.Svc_frame {
		t.Fatal("not a frame")
	}
	m.ReadCount = 0
	copyN(1 + 4 + 4 + 1) // svc_frame, serverframe, deltaframe, surpresscount
	copyN(1 + int(b[m.ReadCount]))
	if b[m.ReadCount] != q2const.Svc_playerinfo {
		t.Fatal("no playerinfo")
	}
	copyN(1)
	canonicalPlayerstate(t, m, out)
	if b[m.ReadCount] != q2const.Svc_packetentities {
		t.Fatal("no packetentities")
	}
	copyN(1)
	for {
		start := m.ReadCount
		num, bits := m.ParseEntityBits()
		if num == 0 {
			out.SZ_Write(b[start:m.ReadCount])
			break
		}
		fields := m.ReadCount
		base := l.base.State[num]
		to := m.ParseDelta(&base, num, bits)
		drop := uint32(0)
		for k, bit := range []uint32{q2const.U_ORIGIN1, q2const.U_ORIGIN2, q2const.U_ORIGIN3} {
			if bits&bit != 0 && to.Origin[k] == base.Origin[k] {
				drop |= bit
			}
		}
		for k, bit := range []uint32{q2const.U_ANGLE1, q2const.U_ANGLE2, q2const.U_ANGLE3} {
			if bits&bit != 0 && to.Angles[k] == base.Angles[k] {
				drop |= bit
			}
		}
		nb := bits &^ (drop | q2const.U_MOREBITS1 | q2const.U_MOREBITS2 | q2const.U_MOREBITS3)
		switch {
		case nb&0xff000000 != 0:
			nb |= q2const.U_MOREBITS3 | q2const.U_MOREBITS2 | q2const.U_MOREBITS1
		case nb&0x00ff0000 != 0:
			nb |= q2const.U_MOREBITS2 | q2const.U_MOREBITS1
		case nb&0x0000ff00 != 0:
			nb |= q2const.U_MOREBITS1
		}
		out.MSG_WriteByte(int32(nb & 255))
		switch {
		case nb&0xff000000 != 0:
			out.MSG_WriteByte(int32(nb >> 8 & 255))
			out.MSG_WriteByte(int32(nb >> 16 & 255))
			out.MSG_WriteByte(int32(nb >> 24 & 255))
		case nb&0x00ff0000 != 0:
			out.MSG_WriteByte(int32(nb >> 8 & 255))
			out.MSG_WriteByte(int32(nb >> 16 & 255))
		case nb&0x0000ff00 != 0:
			out.MSG_WriteByte(int32(nb >> 8 & 255))
		}
		if nb&q2const.U_NUMBER16 != 0 {
			out.MSG_WriteShort(num)
		} else {
			out.MSG_WriteByte(num)
		}
		// the field bytes in ParseDelta order, without the dropped ones
		p := fields
		take := func(bit uint32, n int) {
			if bits&bit == 0 {
				return
			}
			if drop&bit == 0 {
				out.SZ_Write(b[p : p+n])
			}
			p += n
		}
		pair := func(b8, b16 uint32) {
			switch {
			case bits&b8 != 0 && bits&b16 != 0:
				out.SZ_Write(b[p : p+4])
				p += 4
			case bits&b8 != 0:
				take(b8, 1)
			default:
				take(b16, 2)
			}
		}
		take(q2const.U_MODEL, 1)
		take(q2const.U_MODEL2, 1)
		take(q2const.U_MODEL3, 1)
		take(q2const.U_MODEL4, 1)
		take(q2const.U_FRAME8, 1)
		take(q2const.U_FRAME16, 2)
		pair(q2const.U_SKIN8, q2const.U_SKIN16)
		pair(q2const.U_EFFECTS8, q2const.U_EFFECTS16)
		pair(q2const.U_RENDERFX8, q2const.U_RENDERFX16)
		take(q2const.U_ORIGIN1, 2)
		take(q2const.U_ORIGIN2, 2)
		take(q2const.U_ORIGIN3, 2)
		take(q2const.U_ANGLE1, 1)
		take(q2const.U_ANGLE2, 1)
		take(q2const.U_ANGLE3, 1)
		take(q2const.U_OLDORIGIN, 6)
		take(q2const.U_SOUND, 1)
		take(q2const.U_EVENT, 1)
		take(q2const.U_SOLID, 2)
		if p != m.ReadCount {
			t.Fatalf("entity %d: walked %d field bytes, ParseDelta read %d", num, p-fields, m.ReadCount-fields)
		}
	}
	if m.ReadCount != len(b) {
		t.Fatalf("frame has %d trailing bytes", len(b)-m.ReadCount)
	}
	return out.Bytes()
}

// canonicalPlayerstate copies a playerstate delta from a zeroed state
// without the float fields whose wire value is zero.
func canonicalPlayerstate(t *testing.T, m *msg.SizeBuf, out *msg.SizeBuf) {
	t.Helper()
	b := m.Data[:m.CurSize]
	flags := m.MSG_ReadShort()
	type field struct {
		flag int32
		size int
		zero bool // a float field: dropped when all its bytes are zero
	}
	var kept bytes.Buffer
	newFlags := flags
	for _, f := range []field{
		{q2const.PS_M_TYPE, 1, false},
		{q2const.PS_M_ORIGIN, 6, false},
		{q2const.PS_M_VELOCITY, 6, false},
		{q2const.PS_M_TIME, 1, false},
		{q2const.PS_M_FLAGS, 1, false},
		{q2const.PS_M_GRAVITY, 2, false},
		{q2const.PS_M_DELTA_ANGLES, 6, false},
		{q2const.PS_VIEWOFFSET, 3, true},
		{q2const.PS_VIEWANGLES, 6, true},
		{q2const.PS_KICKANGLES, 3, true},
		{q2const.PS_WEAPONINDEX, 1, false},
		{q2const.PS_WEAPONFRAME, 7, false},
		{q2const.PS_BLEND, 4, true},
		{q2const.PS_FOV, 1, true},
		{q2const.PS_RDFLAGS, 1, false},
	} {
		if flags&f.flag == 0 {
			continue
		}
		v := b[m.ReadCount : m.ReadCount+f.size]
		m.ReadCount += f.size
		if f.zero && bytes.Count(v, []byte{0}) == len(v) {
			newFlags &^= f.flag
			continue
		}
		kept.Write(v)
	}
	statbits := uint32(m.MSG_ReadLong())
	n := 0
	for i := 0; i < q2const.MAX_STATS; i++ {
		if statbits&(1<<i) != 0 {
			n++
		}
	}
	kept.Write(b[m.ReadCount-4 : m.ReadCount+2*n])
	m.ReadCount += 2 * n
	out.MSG_WriteShort(newFlags)
	out.SZ_Write(kept.Bytes())
}

// startBot starts a lockstep bot on the demo pak with stream on its client.
func startBot(t testing.TB, fs sv.FileSystem, seed uint32, opt fakeclient.Options, stream *Stream) *session.Lockstep {
	t.Helper()
	opt.OnServerMessage = stream.OnServerMessage
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Skill: 1}, Seed: seed, Client: opt})
	if err := l.Start(context.Background()); err != nil {
		t.Fatalf("start bot: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// testLevel returns a small ready game level with two baselines.
func testLevel() *LevelSnapshot {
	l := &LevelSnapshot{Gen: 1, ServerCount: 42, GameDir: "baseq2", PlayerNum: 0, LevelName: "Test", Ready: true,
		cs: new([q2const.MAX_CONFIGSTRINGS]string), base: new(baselines)}
	l.cs[q2const.CS_NAME] = "Test"
	l.cs[q2const.CS_MODELS+1] = "maps/test.bsp"
	l.base.State[5] = shared.EntityState{Number: 5, ModelIndex: 2, Origin: shared.Vec3{8, 16, 24}}
	l.base.Present[5] = true
	l.base.State[7] = shared.EntityState{Number: 7, Sound: 3}
	l.base.Present[7] = true
	return l
}
