package spectate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// ---------------------------------------------------------------------------
// synth: a scripted server stream (what a bot would receive), for tests
// that need no game data.

type synthFrame struct {
	ps   shared.PlayerState
	ents []shared.EntityState
}

type synth struct {
	sc      int32
	base    [q2const.MAX_EDICTS]shared.EntityState
	has     [q2const.MAX_EDICTS]bool
	cs      map[int]string
	frame   int32
	ps      shared.PlayerState
	ents    []shared.EntityState
	history map[int32]synthFrame
}

// newSynth returns a level with n moving entities (numbers 2..n+1) plus
// the player entity 1 and a sound-only baseline.
func newSynth(sc int32, mapName string, n int) *synth {
	s := &synth{sc: sc, history: map[int32]synthFrame{}, cs: map[int]string{}}
	s.cs[q2const.CS_NAME] = "Synthetic " + mapName
	s.cs[q2const.CS_MODELS+1] = "maps/" + mapName + ".bsp"
	s.cs[q2const.CS_MAPCHECKSUM] = "12345"
	s.cs[q2const.CS_STATUSBAR] = "yb -24 xv 0 hnum xv 50 pic 0"
	for i := 0; i < 40; i++ {
		s.cs[q2const.CS_SOUNDS+1+i] = fmt.Sprintf("world/sound%02d.wav", i)
	}
	s.ps.PMove.PmType = q2const.PM_NORMAL
	s.ps.Fov = 90
	s.ps.GunIndex = 2
	s.ps.Stats[q2const.STAT_HEALTH] = 100
	s.ents = append(s.ents, shared.EntityState{Number: 1, ModelIndex: 255})
	for i := 0; i < n; i++ {
		num := int32(2 + i)
		e := shared.EntityState{Number: num, ModelIndex: int32(2 + i%5), Origin: shared.Vec3{float32(64 * i), 32, 16}}
		s.base[num], s.has[num] = e, true
		s.ents = append(s.ents, e)
	}
	s.base[300] = shared.EntityState{Number: 300, Sound: 4, Origin: shared.Vec3{100, 200, 300}}
	s.has[300] = true
	return s
}

// handshake returns the messages that start the level on a client:
// serverdata and configstrings, then the baselines.
func (s *synth) handshake() [][]byte {
	m := msg.NewSizeBuf(1 << 16)
	m.MSG_WriteByte(q2const.Svc_serverdata)
	m.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	m.MSG_WriteLong(s.sc)
	m.MSG_WriteByte(0)
	m.MSG_WriteString("baseq2")
	m.MSG_WriteShort(0)
	m.MSG_WriteString(s.cs[q2const.CS_NAME])
	for i := 0; i < q2const.MAX_CONFIGSTRINGS; i++ {
		if v, ok := s.cs[i]; ok {
			m.MSG_WriteByte(q2const.Svc_configstring)
			m.MSG_WriteShort(int32(i))
			m.MSG_WriteString(v)
		}
	}
	first := append([]byte(nil), m.Bytes()...)
	m.SZ_Clear()
	var null shared.EntityState
	for i := range s.base {
		if s.has[i] {
			m.MSG_WriteByte(q2const.Svc_spawnbaseline)
			m.MSG_WriteDeltaEntity(&null, &s.base[i], true, true)
		}
	}
	m.MSG_WriteByte(q2const.Svc_stufftext)
	m.MSG_WriteString(fmt.Sprintf("precache %d\n", s.sc))
	return [][]byte{first, append([]byte(nil), m.Bytes()...)}
}

// cinematic returns the serverdata of a picture level.
func (s *synth) cinematic(name string) []byte {
	m := msg.NewSizeBuf(256)
	m.MSG_WriteByte(q2const.Svc_serverdata)
	m.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	m.MSG_WriteLong(s.sc)
	m.MSG_WriteByte(0)
	m.MSG_WriteString("baseq2")
	m.MSG_WriteShort(-1)
	m.MSG_WriteString(name)
	return m.Bytes()
}

// advance moves the world one frame (fractional moves, like a real game).
func (s *synth) advance() {
	s.frame++
	f := float32(s.frame)
	s.ps.PMove.Origin[0] += 13
	s.ps.PMove.Velocity[1] = int16(s.frame % 50)
	s.ps.ViewAngles[1] = f * 1.7
	s.ps.KickAngles[0] = float32(s.frame%3) * 0.1 // sub-quantum, like a real weapon kick
	s.ps.GunFrame = s.frame % 7
	s.ps.GunAngles[2] = f * 0.03
	s.ps.Stats[q2const.STAT_HEALTH] = int16(100 - s.frame%10)
	for i := 1; i < len(s.ents); i++ {
		e := &s.ents[i]
		e.OldOrigin = e.Origin
		e.Origin[0] += 0.3 * float32(i)
		e.Angles[1] = f * 2.1
		e.Frame = s.frame % 40
		e.Event = 0
		if s.frame%17 == int32(i) {
			e.Event = q2const.EV_FOOTSTEP
		}
	}
}

// frameMsg returns a message with the current frame delta-coded from
// frame delta (<= 0: uncompressed), followed by extra commands.
func (s *synth) frameMsg(delta int32, extra func(m *msg.SizeBuf)) []byte {
	m := msg.NewSizeBuf(1 << 16)
	var from *synthFrame
	if delta > 0 {
		f, ok := s.history[delta]
		if !ok {
			panic(fmt.Sprintf("synth: no frame %d", delta))
		}
		from = &f
	} else {
		delta = -1
	}
	m.MSG_WriteByte(q2const.Svc_frame)
	m.MSG_WriteLong(s.frame)
	m.MSG_WriteLong(delta)
	m.MSG_WriteByte(0)
	m.MSG_WriteByte(1)
	m.MSG_WriteByte(1)
	if from == nil {
		m.WriteDeltaPlayerstate(nil, &s.ps)
		emitEntities(m, nil, s.ents, &s.base)
	} else {
		m.WriteDeltaPlayerstate(&from.ps, &s.ps)
		emitEntities(m, from.ents, s.ents, &s.base)
	}
	s.history[s.frame] = synthFrame{ps: s.ps, ents: append([]shared.EntityState(nil), s.ents...)}
	if extra != nil {
		extra(m)
	}
	return append([]byte(nil), m.Bytes()...)
}

// emitEntities is SV_EmitPacketEntities over two sorted lists.
func emitEntities(m *msg.SizeBuf, from, to []shared.EntityState, base *[q2const.MAX_EDICTS]shared.EntityState) {
	m.MSG_WriteByte(q2const.Svc_packetentities)
	oi, ni := 0, 0
	for ni < len(to) || oi < len(from) {
		newnum, oldnum := 9999, 9999
		if ni < len(to) {
			newnum = int(to[ni].Number)
		}
		if oi < len(from) {
			oldnum = int(from[oi].Number)
		}
		switch {
		case newnum == oldnum:
			m.MSG_WriteDeltaEntity(&from[oi], &to[ni], false, newnum <= 1)
			oi++
			ni++
		case newnum < oldnum:
			m.MSG_WriteDeltaEntity(&base[newnum], &to[ni], true, true)
			ni++
		default:
			bits := uint32(q2const.U_REMOVE)
			if oldnum >= 256 {
				bits |= q2const.U_NUMBER16 | q2const.U_MOREBITS1
			}
			m.MSG_WriteByte(int32(bits & 255))
			if bits&0xff00 != 0 {
				m.MSG_WriteByte(int32(bits >> 8 & 255))
			}
			if bits&q2const.U_NUMBER16 != 0 {
				m.MSG_WriteShort(int32(oldnum))
			} else {
				m.MSG_WriteByte(int32(oldnum))
			}
			oi++
		}
	}
	m.MSG_WriteShort(0)
}

func writePrint(text string) func(m *msg.SizeBuf) {
	return func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.Svc_print)
		m.MSG_WriteByte(q2const.PRINT_HIGH)
		m.MSG_WriteString(text)
	}
}

func writeConfigString(i int, v string) func(m *msg.SizeBuf) {
	return func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.Svc_configstring)
		m.MSG_WriteShort(int32(i))
		m.MSG_WriteString(v)
	}
}

func writeStuff(text string) func(m *msg.SizeBuf) {
	return func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(text)
	}
}

func both(fs ...func(m *msg.SizeBuf)) func(m *msg.SizeBuf) {
	return func(m *msg.SizeBuf) {
		for _, f := range fs {
			f(m)
		}
	}
}

// ---------------------------------------------------------------------------
// frame records shared by the rig and the hub tests

type frameKey struct {
	sc, frame int32
}

type frameRec struct {
	ps   shared.PlayerState
	ents []shared.EntityState
}

// recordFrame returns the client's current frame for comparison: pm_type is
// normalized, as attract-loop clients force PM_FREEZE (CL_ParseFrame).
func recordFrame(c *fakeclient.Client) (frameKey, frameRec) {
	ps := c.Frame.PlayerState
	ps.PMove.PmType = q2const.PM_FREEZE
	return frameKey{c.ServerData.ServerCount, c.Frame.ServerFrame}, frameRec{ps: ps, ents: c.FrameEntities(&c.Frame)}
}

func sameFrame(a, b frameRec) bool { return a.ps == b.ps && equalEntities(a.ents, b.ents) }

// hasFrameSpan reports whether a message carried a frame.
func hasFrameSpan(spans []fakeclient.Span) bool {
	for _, sp := range spans {
		if sp.Cmd == q2const.Svc_frame {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// rig: a bot (passive client fed by a synth) and viewers (fake clients)
// around a relay, all driven synchronously by the test.

type rig struct {
	t      *testing.T
	stream *Stream
	sub    *subscription
	r      *relay
	m      *Counters
	start  time.Time
	now    time.Time
	bot    *fakeclient.Client
	srv    *synth

	botFrames map[frameKey]frameRec
	viewers   []*rigViewer
	removed   map[*viewer]bool
}

type rigViewer struct {
	rg      *rig
	v       *viewer
	q       *sliceQueue
	c       *fakeclient.Client
	up      [][]byte
	stalled bool
	err     error
	frames  map[frameKey]frameRec
	order   []frameKey
}

// sliceQueue is a test send queue of bounded capacity.
type sliceQueue struct {
	items [][]byte
	cap   int
}

func (q *sliceQueue) push(d []byte) bool {
	if len(q.items) >= q.cap {
		return false
	}
	q.items = append(q.items, d)
	return true
}

func (q *sliceQueue) backlog() (int, int) { return len(q.items), q.cap }

type upConn struct{ rv *rigViewer }

func (c upConn) Send(d []byte) error {
	c.rv.up = append(c.rv.up, append([]byte(nil), d...))
	return nil
}
func (c upConn) Recv(context.Context) ([]byte, error) { return nil, errors.New("rig: no Recv") }
func (c upConn) Close() error                         { return nil }

func newRig(t *testing.T, feedDepth int) *rig {
	t.Helper()
	rg := &rig{t: t, start: time.Unix(1000, 0), botFrames: map[frameKey]frameRec{}, removed: map[*viewer]bool{}, m: &Counters{}}
	rg.now = rg.start
	rg.stream = NewStream(SinkFunc(func(c *fakeclient.Client, _ []byte, spans []fakeclient.Span) {
		if hasFrameSpan(spans) && c.Frame.Valid {
			k, v := recordFrame(c)
			rg.botFrames[k] = v
		}
	}))
	rg.sub = rg.stream.subscribe(feedDepth)
	rg.r = newRelay(relayConfig{
		timeout:      DefaultTimeout,
		keepalive:    keepalive,
		dropLimit:    DefaultDropLimit,
		dropWindow:   DefaultDropWindow,
		stallTimeout: DefaultStallTimeout,
		clientResync: clientResync,
		challenge:    func() int { return 4321 },
	}, rg.m, rg.now)
	rg.r.onRemove = func(v *viewer) { rg.removed[v] = true }
	rg.bot = fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rg.stream.OnServerMessage})
	return rg
}

func (rg *rig) ms() int { return int(rg.now.Sub(rg.start) / time.Millisecond) }

// feed parses one server message on the bot, without letting the relay
// see it yet.
func (rg *rig) feed(payload []byte) {
	rg.t.Helper()
	if _, err := rg.bot.FeedPayload(payload); err != nil {
		rg.t.Fatalf("bot: %v", err)
	}
}

// deliver hands the hub feed to the relay and runs the datagram exchange.
func (rg *rig) deliver() {
	for {
		select {
		case d := <-rg.sub.ch:
			rg.r.now = rg.now
			rg.r.bot(d)
		default:
			rg.pump()
			return
		}
	}
}

// botMsg is feed + deliver.
func (rg *rig) botMsg(payload []byte) {
	rg.t.Helper()
	rg.feed(payload)
	rg.deliver()
}

// startLevel runs the synth's handshake and n frames on the bot.
func (rg *rig) startLevel(s *synth, frames int) {
	rg.t.Helper()
	rg.srv = s
	for _, p := range s.handshake() {
		rg.botMsg(p)
	}
	rg.botFrame(nil)
	for i := 1; i < frames; i++ {
		rg.botFrame(nil)
	}
}

// botFrame sends the bot the next frame delta-coded from its previous one
// (the first of a level uncompressed), then lets every live viewer send a
// move.
func (rg *rig) botFrame(extra func(m *msg.SizeBuf)) {
	rg.t.Helper()
	s := rg.srv
	delta := s.frame
	if _, ok := s.history[delta]; !ok {
		delta = -1
	}
	s.advance()
	rg.botMsg(s.frameMsg(delta, extra))
	rg.now = rg.now.Add(100 * time.Millisecond)
	rg.r.now = rg.now
	for _, rv := range rg.viewers {
		if rv.err == nil && rv.c.State == fakeclient.CaActive {
			rv.c.SendCmd(shared.UserCmd{Msec: 100})
		}
	}
	rg.pump()
}

// tick advances the clock and runs the relay's timers.
func (rg *rig) tick(d time.Duration) {
	rg.now = rg.now.Add(d)
	rg.r.now = rg.now
	rg.r.tick()
	rg.pump()
}

// addViewer connects a fake client viewer through the relay.
func (rg *rig) addViewer(queue int) *rigViewer {
	rg.t.Helper()
	rv := &rigViewer{rg: rg, q: &sliceQueue{cap: queue}, frames: map[frameKey]frameRec{}}
	rv.v = &viewer{id: len(rg.viewers) + 1, addr: qnet.Addr{Base: "test", Port: len(rg.viewers) + 1}, out: rv.q}
	rv.c = fakeclient.New(upConn{rv}, fakeclient.Options{
		Qport: 100 + len(rg.viewers),
		Clock: rg.ms,
		OnServerMessage: func(c *fakeclient.Client, _ []byte, spans []fakeclient.Span) {
			if hasFrameSpan(spans) && c.Frame.Valid {
				k, v := recordFrame(c)
				rv.frames[k] = v
				rv.order = append(rv.order, k)
			}
		},
	})
	rg.viewers = append(rg.viewers, rv)
	rg.r.now = rg.now
	rg.r.join(rv.v)
	rv.c.BeginConnect()
	rg.pump()
	return rv
}

// pump exchanges datagrams between the relay and the viewers until quiet.
// A viewer runs its connection timers (Tick) once, and again after each
// round it received something in, like a client frame following packets.
func (rg *rig) pump() {
	rg.t.Helper()
	for round := 0; ; round++ {
		if round == 500 {
			rg.t.Fatal("rig: datagrams do not settle")
		}
		moved := false
		for _, rv := range rg.viewers {
			if rv.err != nil {
				continue
			}
			got := round == 0
			if !rv.stalled {
				for len(rv.q.items) > 0 {
					d := rv.q.items[0]
					rv.q.items = rv.q.items[1:]
					moved, got = true, true
					if err := rv.c.Feed(d); err != nil {
						rv.err = err
						break
					}
				}
			}
			if rv.err == nil && got {
				if err := rv.c.Tick(); err != nil {
					rv.err = err
				}
			}
		}
		for _, rv := range rg.viewers {
			for len(rv.up) > 0 {
				d := rv.up[0]
				rv.up = rv.up[1:]
				moved = true
				rg.r.now = rg.now
				rg.r.packet(rv.v, d)
			}
		}
		if !moved {
			return
		}
	}
}

// requireMatch checks that every frame the viewer holds equals the bot's
// and returns how many it holds.
func (rv *rigViewer) requireMatch(t *testing.T) int {
	t.Helper()
	for k, v := range rv.frames {
		b, ok := rv.rg.botFrames[k]
		if !ok {
			t.Fatalf("viewer has frame %v the bot never had", k)
		}
		if !sameFrame(v, b) {
			t.Fatalf("frame %v differs:\nviewer %+v\n   bot %+v", k, v, b)
		}
	}
	return len(rv.frames)
}
