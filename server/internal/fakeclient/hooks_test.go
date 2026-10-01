package fakeclient

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// queueConn is a net.Conn whose datagrams are appended to a slice, so a test
// can drive client and scriptServer synchronously from one goroutine.
type queueConn struct{ out *[][]byte }

func (q queueConn) Send(d []byte) error {
	*q.out = append(*q.out, append([]byte(nil), d...))
	return nil
}

func (q queueConn) Recv(context.Context) ([]byte, error) {
	return nil, errors.New("queueConn: datagrams are delivered with Feed")
}

func (q queueConn) Close() error { return nil }

// syncRig connects a Client to a scriptServer without goroutines: every
// datagram is handed over by pump.
type syncRig struct {
	t        testing.TB
	s        *scriptServer
	c        *Client
	toServer [][]byte
	toClient [][]byte
	wire     [][]byte // every client datagram, in order
	now      int      // virtual clock (Options.Clock)
}

func newSyncRig(tb testing.TB, opt Options) *syncRig {
	r := &syncRig{t: tb}
	r.s = &scriptServer{conn: queueConn{out: &r.toClient}}
	if t, ok := tb.(*testing.T); ok {
		r.s.t = t
	}
	if opt.Qport == 0 {
		opt.Qport = 999
	}
	if opt.Clock == nil {
		opt.Clock = func() int { return r.now }
	}
	r.c = New(queueConn{out: &r.toServer}, opt)
	return r
}

// pump delivers queued datagrams both ways until both queues are empty and
// returns the first client error.
func (r *syncRig) pump() error {
	var first error
	for i := 0; i < 64 && (len(r.toServer) > 0 || len(r.toClient) > 0); i++ {
		up := r.toServer
		r.toServer = nil
		for _, d := range up {
			r.wire = append(r.wire, d)
			r.s.handle(d)
		}
		if r.s.chan_.Message.CurSize > 0 {
			r.s.chan_.Transmit(nil, 0) // flush reliable replies
		}
		down := r.toClient
		r.toClient = nil
		for _, d := range down {
			if err := r.c.Feed(d); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// handshake runs getchallenge .. begin with Tick/Feed and makes the client
// active with a first (uncompressed) frame.
func (r *syncRig) handshake() {
	r.t.Helper()
	r.c.BeginConnect()
	for i := 0; i < 20 && !r.s.begun; i++ {
		if err := r.c.Tick(); err != nil {
			r.t.Fatal(err)
		}
		if err := r.pump(); err != nil {
			r.t.Fatal(err)
		}
	}
	if !r.s.begun || !r.c.BeginSent {
		r.t.Fatalf("handshake did not reach begin: %q", r.s.strings)
	}
	r.s.ps.PMove.Origin = [3]int16{80, 160, 240}
	r.s.ents = []shared.EntityState{{Number: 1, ModelIndex: 255}, {Number: 5, ModelIndex: 3, Origin: shared.Vec3{10, 20, 30}}}
	r.frame(nil)
	if r.c.State != CaActive {
		r.t.Fatalf("not active: state %d", r.c.State)
	}
}

// move sends one usercmd, lets the server read it and answers with a frame.
func (r *syncRig) move(extra func(w *msg.SizeBuf)) {
	r.t.Helper()
	r.c.SendCmd(shared.UserCmd{Msec: 100, ForwardMove: 200})
	if err := r.pump(); err != nil {
		r.t.Fatal(err)
	}
	r.s.ps.PMove.Origin[0] += 8
	r.frame(extra)
}

func (r *syncRig) frame(extra func(w *msg.SizeBuf)) {
	r.t.Helper()
	r.s.sendFrame(extra)
	if err := r.pump(); err != nil {
		r.t.Fatal(err)
	}
}

func TestFeedTickHandshake(t *testing.T) {
	r := newSyncRig(t, Options{})
	r.handshake()
	for i := 0; i < 5; i++ {
		r.move(nil)
	}
	if !r.c.Frame.Valid || r.c.Frame.DeltaFrame <= 0 || r.c.Frame.ServerFrame != r.s.frame {
		t.Fatalf("frame %+v (server frame %d)", r.c.Frame, r.s.frame)
	}
	if got := r.c.Origin(); got != (shared.Vec3{10 + 5, 20, 30}) {
		t.Fatalf("origin %v", got)
	}
	if r.s.cmdErr != nil {
		t.Fatal(r.s.cmdErr)
	}
}

// TestHooksDoNotPerturbWire runs the same scripted session with and without
// the observation hooks: the client must send byte-identical datagrams and
// end in the same state.
func TestHooksDoNotPerturbWire(t *testing.T) {
	run := func(opt Options) *syncRig {
		r := newSyncRig(t, opt)
		r.handshake()
		for i := 0; i < 8; i++ {
			r.move(func(w *msg.SizeBuf) {
				w.MSG_WriteByte(q2const.Svc_print)
				w.MSG_WriteByte(q2const.PRINT_HIGH)
				w.MSG_WriteString("hello\n")
				w.MSG_WriteByte(q2const.Svc_muzzleflash)
				w.MSG_WriteShort(1)
				w.MSG_WriteByte(q2const.MZ_BLASTER)
			})
		}
		return r
	}
	plain := run(Options{})
	calls := 0
	hooked := run(Options{MaxHistory: 3, OnServerMessage: func(*Client, []byte, []Span) { calls++ }})
	if calls == 0 {
		t.Fatal("OnServerMessage never called")
	}
	if len(plain.wire) != len(hooked.wire) {
		t.Fatalf("%d datagrams vs %d", len(plain.wire), len(hooked.wire))
	}
	for i := range plain.wire {
		if !bytes.Equal(plain.wire[i], hooked.wire[i]) {
			t.Fatalf("datagram %d differs:\n%x\n%x", i, plain.wire[i], hooked.wire[i])
		}
	}
	if plain.c.Frame != hooked.c.Frame || plain.c.ConfigStrings != hooked.c.ConfigStrings {
		t.Fatal("parsed state differs")
	}
	if len(plain.c.Prints) != 8 || len(hooked.c.Prints) < 3 || len(hooked.c.Prints) > 6 ||
		plain.c.Counts != hooked.c.Counts {
		t.Fatalf("prints: %d unbounded, %d capped (counts %+v vs %+v)",
			len(plain.c.Prints), len(hooked.c.Prints), plain.c.Counts, hooked.c.Counts)
	}
}

// TestZeroOptionsDefaults pins the defaults of a zero Options: no spans are
// recorded, histories are unbounded, deltas are requested and the wall clock
// drives the timers.
func TestZeroOptionsDefaults(t *testing.T) {
	cc, _ := net.MemPipe(8)
	c := New(cc, Options{})
	if c.opt.Clock != nil || c.WaitingFullFrame() || c.LevelGen() != 0 {
		t.Fatal("unexpected defaults")
	}
	if c.Qport() == 0 {
		t.Fatal("qport not picked")
	}
	if d := c.curtime(); d < 0 || d > 5000 {
		t.Fatalf("wall curtime %d", d)
	}
	r := newSyncRig(t, Options{})
	r.c.opt.Clock = nil // the wall clock, as with a zero Options
	r.handshake()
	for i := 0; i < 300; i++ {
		r.c.TempEnts = appendHistory(r.c.TempEnts, int32(i), r.c.opt.MaxHistory, &r.c.Counts.TempEnts)
	}
	if len(r.c.TempEnts) != 300 || r.c.TempEnts[0] != 0 || r.c.Counts.TempEnts != 300 {
		t.Fatalf("history capped at %d (count %d) without MaxHistory", len(r.c.TempEnts), r.c.Counts.TempEnts)
	}
	if r.c.spans != nil || r.c.wantSpans {
		t.Fatal("spans recorded without OnServerMessage")
	}
	r.move(nil)
	if r.s.lastAck != r.s.frame-1 {
		t.Fatalf("lastframe %d, want the previous frame %d", r.s.lastAck, r.s.frame-1)
	}
}

func TestClockOption(t *testing.T) {
	r := newSyncRig(t, Options{})
	r.now = 50000
	r.c.BeginConnect()
	_ = r.c.Tick() // fires immediately
	if len(r.toServer) != 1 || !strings.Contains(string(r.toServer[0][4:]), "getchallenge") {
		t.Fatalf("no getchallenge: %q", r.toServer)
	}
	r.toServer = nil
	r.now += 2999
	_ = r.c.Tick()
	if len(r.toServer) != 0 {
		t.Fatal("resent before 3 s of virtual time")
	}
	r.now++
	_ = r.c.Tick()
	if len(r.toServer) != 1 {
		t.Fatal("not resent after 3 s of virtual time")
	}
	r.toServer = nil
	if err := r.pump(); err != nil { // nothing queued: the server ignored nothing
		t.Fatal(err)
	}

	// keepalive while connected (after begin, before the first frame)
	// follows the virtual clock too
	r = newSyncRig(t, Options{})
	r.c.BeginConnect()
	for i := 0; i < 20 && !r.s.begun; i++ {
		_ = r.c.Tick()
		if err := r.pump(); err != nil {
			t.Fatal(err)
		}
	}
	if r.c.State != CaConnected || !r.s.begun {
		t.Fatalf("state %d begun %v", r.c.State, r.s.begun)
	}
	last := r.c.Netchan.LastSent
	r.now = last + 1000
	if r.c.Tick(); len(r.toServer) != 0 {
		t.Fatal("keepalive before 1 s")
	}
	r.now = last + 1001
	if r.c.Tick(); len(r.toServer) != 1 || r.c.Netchan.LastSent != r.now {
		t.Fatalf("keepalive: %d datagrams, last sent %d", len(r.toServer), r.c.Netchan.LastSent)
	}
}

type message struct {
	payload []byte
	spans   []Span
	begun   bool // BeginSent when the callback ran
}

func TestOnServerMessagePayloadAndSpans(t *testing.T) {
	var got []message
	r := newSyncRig(t, Options{OnServerMessage: func(c *Client, payload []byte, spans []Span) {
		got = append(got, message{payload: payload, spans: spans, begun: c.BeginSent})
	}})
	r.handshake()
	for i := 0; i < 3; i++ {
		r.move(func(w *msg.SizeBuf) {
			w.MSG_WriteByte(q2const.Svc_centerprint)
			w.MSG_WriteString("center")
		})
	}
	if len(got) == 0 {
		t.Fatal("no messages reported")
	}
	sawPrecache := false
	for i, m := range got {
		// spans tile the payload exactly
		pos := 0
		for _, sp := range m.spans {
			if sp.Start != pos || sp.End <= sp.Start || sp.End > len(m.payload) {
				t.Fatalf("message %d: bad span %+v at %d (len %d)", i, sp, pos, len(m.payload))
			}
			if int32(m.payload[sp.Start]) != sp.Cmd {
				t.Fatalf("message %d: span cmd %d, byte %d", i, sp.Cmd, m.payload[sp.Start])
			}
			pos = sp.End
		}
		if pos != len(m.payload) {
			t.Fatalf("message %d: spans end at %d of %d", i, pos, len(m.payload))
		}
		if bytes.Contains(m.payload, []byte("precache 7")) {
			sawPrecache = true
			if m.begun {
				t.Fatal("stuffed text ran before OnServerMessage")
			}
		}
	}
	if !sawPrecache {
		t.Fatal("precache message not reported")
	}
	if !r.c.BeginSent {
		t.Fatal("stuffed precache did not run after the callback")
	}
	last := got[len(got)-1]
	kinds := []int32{}
	for _, sp := range last.spans {
		kinds = append(kinds, sp.Cmd)
	}
	if len(kinds) != 2 || kinds[0] != q2const.Svc_frame || kinds[1] != q2const.Svc_centerprint {
		t.Fatalf("last message commands %v", kinds)
	}

	// the payload is the datagram minus its 8 byte netchan header, and a
	// duplicate (rejected by the netchan) or OOB packet is not reported
	r.s.sendFrame(nil)
	d := r.toClient[0]
	r.toClient = nil
	n := len(got)
	if err := r.c.Feed(d); err != nil {
		t.Fatal(err)
	}
	if len(got) != n+1 || !bytes.Equal(got[n].payload, d[8:]) {
		t.Fatalf("payload is not datagram[8:]")
	}
	d[8] ^= 0xff // the reported copy must not alias the datagram
	if bytes.Equal(got[n].payload, d[8:]) {
		t.Fatal("payload aliases the datagram")
	}
	d[8] ^= 0xff
	if err := r.c.Feed(d); err != nil || len(got) != n+1 {
		t.Fatalf("duplicate reported (err %v)", err)
	}
	if err := r.c.Feed(append([]byte{0xff, 0xff, 0xff, 0xff}, "print\nx\n"...)); err != nil || len(got) != n+1 {
		t.Fatalf("OOB reported (err %v)", err)
	}
}

func TestLevelGenCountsServerdata(t *testing.T) {
	r := newSyncRig(t, Options{})
	if r.c.LevelGen() != 0 {
		t.Fatal("gen before connect")
	}
	r.handshake()
	if r.c.LevelGen() != 1 {
		t.Fatalf("gen %d after first serverdata", r.c.LevelGen())
	}
	// a level change: changing + reconnect resends "new", the next serverdata
	// starts generation 2 even though the servercount is unchanged
	r.s.chan_.Message.MSG_WriteByte(q2const.Svc_stufftext)
	r.s.chan_.Message.MSG_WriteString("changing\nreconnect\n")
	r.s.chan_.Transmit(nil, 0)
	r.s.begun = false
	if err := r.pump(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10 && !r.s.begun; i++ {
		_ = r.c.Tick()
		if err := r.pump(); err != nil {
			t.Fatal(err)
		}
	}
	if !r.s.begun || r.c.LevelGen() != 2 || r.c.ServerData.ServerCount != 7 {
		t.Fatalf("gen %d servercount %d begun %v", r.c.LevelGen(), r.c.ServerData.ServerCount, r.s.begun)
	}
}

func TestRequestFullFrame(t *testing.T) {
	r := newSyncRig(t, Options{})
	r.handshake()
	r.move(nil)
	r.move(nil)
	if r.c.Frame.DeltaFrame <= 0 {
		t.Fatal("not delta compressed")
	}
	r.c.RequestFullFrame()
	if !r.c.WaitingFullFrame() {
		t.Fatal("not waiting")
	}
	r.c.SendCmd(shared.UserCmd{Msec: 10})
	if err := r.pump(); err != nil {
		t.Fatal(err)
	}
	if r.s.lastAck != -1 {
		t.Fatalf("lastframe %d while waiting, want -1", r.s.lastAck)
	}
	r.frame(nil)
	if r.c.Frame.DeltaFrame != -1 || r.c.WaitingFullFrame() {
		t.Fatalf("full frame: delta %d waiting %v", r.c.Frame.DeltaFrame, r.c.WaitingFullFrame())
	}
	r.move(nil)
	if r.s.lastAck != r.s.frame-1 || r.c.Frame.DeltaFrame <= 0 {
		t.Fatalf("deltas did not resume: ack %d delta %d", r.s.lastAck, r.c.Frame.DeltaFrame)
	}
}

func TestMaxHistory(t *testing.T) {
	const limit = 2
	r := newSyncRig(t, Options{MaxHistory: limit})
	r.handshake()
	c := r.c
	var seenPrints, seenSounds uint64
	for i := 0; i < 9; i++ {
		i := i
		r.move(func(w *msg.SizeBuf) {
			w.MSG_WriteByte(q2const.Svc_print)
			w.MSG_WriteByte(q2const.PRINT_HIGH)
			w.MSG_WriteString(string(rune('a' + i)))
			w.MSG_WriteByte(q2const.Svc_centerprint)
			w.MSG_WriteString("c")
			w.MSG_WriteByte(q2const.Svc_layout)
			w.MSG_WriteString("l")
			for k := 0; k < 2; k++ { // two sounds per message
				w.MSG_WriteByte(q2const.Svc_sound)
				w.MSG_WriteByte(0)
				w.MSG_WriteByte(int32(2*i + k))
			}
			w.MSG_WriteByte(q2const.Svc_temp_entity)
			w.MSG_WriteByte(q2const.TE_EXPLOSION1)
			w.MSG_WritePos(shared.Vec3{float32(i), 0, 0})
			w.MSG_WriteByte(q2const.Svc_muzzleflash2)
			w.MSG_WriteShort(5)
			w.MSG_WriteByte(int32(i))
			w.MSG_WriteByte(q2const.Svc_download)
			w.MSG_WriteShort(-1)
			w.MSG_WriteByte(0)
		})
		// the events of each message are the tail of the history
		fresh, lost := NewSince(c.Prints, c.Counts.Prints, seenPrints)
		if lost != 0 || len(fresh) != 1 || fresh[0] != string(rune('a'+i)) {
			t.Fatalf("message %d: new prints %q lost %d", i, fresh, lost)
		}
		sounds, lost := NewSince(c.Sounds, c.Counts.Sounds, seenSounds)
		if lost != 0 || len(sounds) != 2 || sounds[0].SoundNum != int32(2*i) || sounds[1].SoundNum != int32(2*i+1) {
			t.Fatalf("message %d: new sounds %+v lost %d", i, sounds, lost)
		}
		seenPrints, seenSounds = c.Counts.Prints, c.Counts.Sounds
		if len(c.Prints) < min(i+1, limit) || len(c.Prints) > 2*limit || len(c.Sounds) > 2*limit {
			t.Fatalf("message %d: %d prints %d sounds, want %d..%d", i, len(c.Prints), len(c.Sounds), limit, 2*limit)
		}
	}
	if c.Counts.Prints != 9 || c.Counts.Sounds != 18 || c.Counts.CenterPrints != 9 || c.Counts.Layouts != 9 ||
		c.Counts.TempEnts != 9 || c.Counts.TempEntEvents != 9 || c.Counts.MuzzleFlashes != 9 || c.Counts.Downloads != 9 ||
		c.Counts.OOB < 2 || c.Counts.StuffTexts < 2 {
		t.Fatalf("counts %+v", c.Counts)
	}
	for name, n := range map[string]int{"prints": len(c.Prints), "center": len(c.CenterPrints), "layouts": len(c.Layouts),
		"downloads": len(c.Downloads), "tents": len(c.TempEnts), "events": len(c.TempEntEvents),
		"flashes": len(c.MuzzleFlashes), "stuff": len(c.StuffTexts), "oob": len(c.OOB), "sounds": len(c.Sounds)} {
		if n < limit || n > 2*limit {
			t.Errorf("%s: %d entries, want %d..%d", name, n, limit, 2*limit)
		}
	}
	// the newest entries are kept
	if c.Prints[len(c.Prints)-1] != "i" || c.Sounds[len(c.Sounds)-1].SoundNum != 17 ||
		c.MuzzleFlashes[len(c.MuzzleFlashes)-1].Weapon != 8 || c.TempEntEvents[len(c.TempEntEvents)-1].Pos[0] != 8 {
		t.Fatalf("not the newest entries: %q %+v %+v %+v", c.Prints, c.Sounds, c.MuzzleFlashes, c.TempEntEvents)
	}
	// a reader that fell behind by more than the history learns how much it missed
	if fresh, lost := NewSince(c.Sounds, c.Counts.Sounds, 0); lost != 18-uint64(len(c.Sounds)) || len(fresh) != len(c.Sounds) {
		t.Fatalf("NewSince from 0: %d fresh, %d lost", len(fresh), lost)
	}
	if cap(c.Prints) > 2*2*limit {
		t.Fatalf("capped history grew to cap %d", cap(c.Prints))
	}
}

func TestAppendHistoryBounds(t *testing.T) {
	for _, limit := range []int{1, 2, 3, 64} {
		var s []int
		var count uint64
		for i := 0; i < 10*limit+3; i++ {
			s = appendHistory(s, i, limit, &count)
			if len(s) > 2*limit || len(s) < min(i+1, limit) || s[len(s)-1] != i {
				t.Fatalf("limit %d after %d: %v", limit, i, s)
			}
			for k := 1; k < len(s); k++ {
				if s[k] != s[k-1]+1 {
					t.Fatalf("limit %d after %d: not the newest run %v", limit, i, s)
				}
			}
		}
		if count != uint64(10*limit+3) || cap(s) > 4*limit {
			t.Fatalf("limit %d: count %d cap %d", limit, count, cap(s))
		}
	}
	if fresh, lost := NewSince([]int{1, 2}, 5, 5); fresh != nil || lost != 0 {
		t.Fatal("NewSince without new events")
	}
}

func TestTempEntEventsAndMuzzleFlashes(t *testing.T) {
	r := newSyncRig(t, Options{})
	r.handshake()
	up := shared.Vec3{0, 0, 1}
	r.move(func(w *msg.SizeBuf) {
		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_BLASTER)
		w.MSG_WritePos(shared.Vec3{1, 2, 3})
		w.MSG_WriteDir(&up)

		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_SPLASH)
		w.MSG_WriteByte(8)
		w.MSG_WritePos(shared.Vec3{4, 5, 6})
		w.MSG_WriteDir(&up)
		w.MSG_WriteByte(2)

		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_RAILTRAIL)
		w.MSG_WritePos(shared.Vec3{-8, 0, 16})
		w.MSG_WritePos(shared.Vec3{512, 0, 16})

		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_LIGHTNING)
		w.MSG_WriteShort(7)
		w.MSG_WriteShort(9)
		w.MSG_WritePos(shared.Vec3{1, 1, 1})
		w.MSG_WritePos(shared.Vec3{2, 2, 2})

		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_GRAPPLE_CABLE)
		w.MSG_WriteShort(3)
		w.MSG_WritePos(shared.Vec3{1, 0, 0})
		w.MSG_WritePos(shared.Vec3{2, 0, 0})
		w.MSG_WritePos(shared.Vec3{3, 0, 0})

		w.MSG_WriteByte(q2const.Svc_temp_entity)
		w.MSG_WriteByte(q2const.TE_STEAM)
		w.MSG_WriteShort(-1)
		w.MSG_WriteByte(20)
		w.MSG_WritePos(shared.Vec3{0, 8, 0})
		w.MSG_WriteDir(&up)
		w.MSG_WriteByte(0xe0)
		w.MSG_WriteShort(60)

		w.MSG_WriteByte(q2const.Svc_muzzleflash)
		w.MSG_WriteShort(1)
		w.MSG_WriteByte(q2const.MZ_BLASTER | q2const.MZ_SILENCED)
		w.MSG_WriteByte(q2const.Svc_muzzleflash2)
		w.MSG_WriteShort(42)
		w.MSG_WriteByte(q2const.MZ2_SOLDIER_BLASTER_1)
	})
	ev := r.c.TempEntEvents
	want := []TempEnt{
		{Type: q2const.TE_BLASTER, Pos: shared.Vec3{1, 2, 3}, Dir: up},
		{Type: q2const.TE_SPLASH, Count: 8, Pos: shared.Vec3{4, 5, 6}, Dir: up, Color: 2},
		{Type: q2const.TE_RAILTRAIL, Pos: shared.Vec3{-8, 0, 16}, Pos2: shared.Vec3{512, 0, 16}},
		{Type: q2const.TE_LIGHTNING, Ent: 7, Ent2: 9, Pos: shared.Vec3{1, 1, 1}, Pos2: shared.Vec3{2, 2, 2}},
		{Type: q2const.TE_GRAPPLE_CABLE, Ent: 3, Pos: shared.Vec3{1, 0, 0}, Pos2: shared.Vec3{2, 0, 0}, Offset: shared.Vec3{3, 0, 0}},
		{Type: q2const.TE_STEAM, Ent: -1, Count: 20, Pos: shared.Vec3{0, 8, 0}, Dir: up, Color: 0xe0, Magnitude: 60},
	}
	if len(ev) != len(want) {
		t.Fatalf("%d events: %+v", len(ev), ev)
	}
	for i := range want {
		if ev[i] != want[i] {
			t.Errorf("event %d: %+v want %+v", i, ev[i], want[i])
		}
		if r.c.TempEnts[i] != want[i].Type {
			t.Errorf("TempEnts[%d] = %d", i, r.c.TempEnts[i])
		}
	}
	mf := r.c.MuzzleFlashes
	if len(mf) != 2 || mf[0] != (MuzzleFlash{Ent: 1, Weapon: q2const.MZ_BLASTER | q2const.MZ_SILENCED}) ||
		mf[1] != (MuzzleFlash{Ent: 42, Weapon: q2const.MZ2_SOLDIER_BLASTER_1, Monster: true}) {
		t.Fatalf("muzzle flashes %+v", mf)
	}
}

func TestPassiveFeedPayload(t *testing.T) {
	var payloads [][]byte
	r := newSyncRig(t, Options{OnServerMessage: func(_ *Client, p []byte, _ []Span) {
		payloads = append(payloads, p)
	}})
	r.handshake()
	for i := 0; i < 4; i++ {
		if i == 2 {
			r.s.ents = append(r.s.ents, shared.EntityState{Number: 300, ModelIndex: 2})
		}
		r.move(func(w *msg.SizeBuf) {
			w.MSG_WriteByte(q2const.Svc_download)
			w.MSG_WriteShort(1)
			w.MSG_WriteByte(50)
			w.MSG_WriteByte(0)
		})
	}

	var seen int
	p := NewPassive(Options{OnServerMessage: func(*Client, []byte, []Span) { seen++ }})
	for i, pl := range payloads {
		spans, err := p.FeedPayload(pl)
		if err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		if len(spans) == 0 {
			t.Fatalf("payload %d: no spans", i)
		}
	}
	if seen != len(payloads) {
		t.Fatalf("OnServerMessage called %d times for %d payloads", seen, len(payloads))
	}
	if p.State != CaActive || p.ServerData != r.c.ServerData || p.ConfigStrings != r.c.ConfigStrings ||
		p.Entities[5].Baseline != r.c.Entities[5].Baseline {
		t.Fatal("passive parse differs from the live client")
	}
	if p.Frame != r.c.Frame {
		t.Fatalf("frame %+v want %+v", p.Frame, r.c.Frame)
	}
	got, want := p.FrameEntities(&p.Frame), r.c.FrameEntities(&r.c.Frame)
	if len(got) != 3 || len(got) != len(want) {
		t.Fatalf("entities %d want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("entity %d: %+v want %+v", i, got[i], want[i])
		}
	}
	// stuffed text is recorded, never executed; downloads are not answered
	if len(p.StuffTexts) != len(r.c.StuffTexts) || p.BeginSent || p.Reliables != 0 ||
		p.Netchan.Message.CurSize != 0 {
		t.Fatalf("passive client acted: stuff %d begin %v reliables %d msg %d",
			len(p.StuffTexts), p.BeginSent, p.Reliables, p.Netchan.Message.CurSize)
	}
	if p.SendCmd(shared.UserCmd{Msec: 10}) != nil || p.Tick() != nil {
		t.Fatal("passive client sent")
	}
	if _, err := r.c.FeedPayload(payloads[0]); err == nil {
		t.Fatal("FeedPayload accepted on an active client")
	}
	// svc_disconnect ends the passive client like a received one
	if _, err := p.FeedPayload([]byte{q2const.Svc_disconnect}); !errors.Is(err, ErrDisconnected) || !p.Disconnected {
		t.Fatalf("disconnect: %v", err)
	}
}

// TestPassiveRefusesFeed: a passive client has no connection, so a datagram
// (which may be a connectionless packet the client would answer) is refused
// instead of being processed.
func TestPassiveRefusesFeed(t *testing.T) {
	p := NewPassive(Options{})
	for _, d := range [][]byte{
		[]byte("\xff\xff\xff\xffchallenge 123\n"),
		[]byte("\xff\xff\xff\xffping\n"),
		[]byte("\xff\xff\xff\xffecho hi\n"),
		[]byte("\xff\xff\xff\xffclient_connect\n"),
		make([]byte, 12),
	} {
		if err := p.Feed(d); !errors.Is(err, ErrPassive) {
			t.Fatalf("Feed(%q) = %v, want ErrPassive", d, err)
		}
	}
	if p.State != CaDisconnected || p.Challenge != 0 || len(p.OOB) != 0 || p.Disconnected {
		t.Fatalf("passive client processed a datagram: state %d challenge %d oob %q", p.State, p.Challenge, p.OOB)
	}
}

func TestMapName(t *testing.T) {
	c := NewPassive(Options{})
	if c.MapName() != "" {
		t.Fatal("map before serverdata")
	}
	c.ServerData.LevelName = "victory.pcx"
	if got := c.MapName(); got != "victory.pcx" {
		t.Fatalf("picture level %q", got)
	}
	c.ConfigStrings[q2const.CS_MODELS+1] = "maps/demo2.bsp"
	if got := c.MapName(); got != "demo2" {
		t.Fatalf("game level %q", got)
	}
}

// TestNegativeEntityNumberIsDrop: a 16 bit entity number with the sign bit set
// must end in ERR_DROP (an error), not a runtime panic indexing cl_entities.
func TestNegativeEntityNumberIsDrop(t *testing.T) {
	p := NewPassive(Options{})
	w := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	w.MSG_WriteByte(q2const.Svc_frame)
	w.MSG_WriteLong(1)
	w.MSG_WriteLong(-1)
	w.MSG_WriteByte(0)
	w.MSG_WriteByte(0)
	w.MSG_WriteByte(q2const.Svc_playerinfo)
	w.MSG_WriteShort(0)
	w.MSG_WriteLong(0)
	w.MSG_WriteByte(q2const.Svc_packetentities)
	w.MSG_WriteByte(q2const.U_MOREBITS1 | 2) // U_ORIGIN2 plus more bits
	w.MSG_WriteByte(q2const.U_NUMBER16 >> 8)
	w.MSG_WriteShort(-2)
	w.MSG_WriteShort(0)
	w.MSG_WriteShort(0)
	if _, err := p.FeedPayload(w.Bytes()); err == nil || errors.Is(err, ErrDisconnected) {
		t.Fatalf("got %v, want a drop error", err)
	}
}

// FuzzFeedPayload: arbitrary server messages fed to a passive client end in
// a Com_Error at worst, never in a runtime panic.
func FuzzFeedPayload(f *testing.F) {
	var payloads [][]byte
	r := newSyncRig(f, Options{OnServerMessage: func(_ *Client, p []byte, _ []Span) {
		payloads = append(payloads, p)
	}})
	r.handshake()
	r.move(nil)
	for _, p := range payloads {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := NewPassive(Options{})
		for _, pl := range payloads[:2] { // serverdata, configstrings, baselines
			_, _ = p.FeedPayload(pl)
		}
		_, _ = p.FeedPayload(data)
	})
}
