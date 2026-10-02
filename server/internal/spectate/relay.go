package spectate

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cmd"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// Netchan and protocol sizes of the relay.
const (
	// maxDatagram is the payload room of a server-to-client datagram: the
	// NS_SERVER netchan header is 8 bytes (no qport).
	maxDatagram = q2const.MAX_MSGLEN - 8
	// maxStringCmds is MAX_STRINGCMDS: string commands executed per packet.
	maxStringCmds = 8 // C: server/sv_user.c:511 MAX_STRINGCMDS
)

// relayConfig holds the relay's resolved settings.
type relayConfig struct {
	timeout      time.Duration // drop a viewer that sent nothing valid this long
	keepalive    time.Duration // send at least one datagram this often
	dropLimit    int           // more send queue drops than this within dropWindow: disconnect
	dropWindow   time.Duration
	stallTimeout time.Duration // a send queue that does not drain this long: disconnect
	clientResync time.Duration // minimum interval between resyncs a viewer asks for
	challenge    func() int    // challenge numbers (default 1..0x7fff at random)
	logf         func(format string, args ...any)
}

// relay is the protocol core of a Hub: a synchronous state machine that
// owns every viewer's netchan and handshake state. The Hub goroutine calls
// it for each bot message, viewer datagram and timer tick, after setting
// now; tests drive it directly.
type relay struct {
	cfg     relayConfig
	metrics Metrics
	start   time.Time
	now     time.Time

	level *LevelSnapshot // the bot's level as of the last message
	frame *FrameSnapshot // its latest valid frame

	viewers []*viewer

	cmd    *cmd.Cmd // Cmd_TokenizeString for viewer text
	key    msg.SizeBuf
	pkt    []byte
	chunks []chunk
	saved  []savedCS

	// onRemove is called once for each viewer the relay removes.
	onRemove func(v *viewer)
}

// chunk is one command of an outgoing forward.
type chunk struct {
	b     []byte
	frame bool
}

// savedCS remembers a known configstring replaced by an unreliable forward,
// to restore it when the datagram is dropped.
type savedCS struct {
	i   int
	old string
}

func newRelay(cfg relayConfig, metrics Metrics, now time.Time) *relay {
	if cfg.challenge == nil {
		cfg.challenge = func() int { return 1 + rand.IntN(0x7fff) }
	}
	r := &relay{cfg: cfg, metrics: metrics, start: now, now: now, cmd: cmd.New(cvar.New())}
	r.key.SZ_Init(make([]byte, maxKeyframe))
	return r
}

func (r *relay) logf(format string, args ...any) {
	if r.cfg.logf != nil {
		r.cfg.logf(format, args...)
	}
}

// ms is the relay's Sys_Milliseconds for the netchans.
func (r *relay) ms() int { return int(r.now.Sub(r.start) / time.Millisecond) }

// snapshot returns a copy of the viewers to iterate over while some may be
// removed.
func (r *relay) snapshot() []*viewer { return slices.Clone(r.viewers) }

// ---------------------------------------------------------------------------
// Viewers

// join adds a viewer connection (it then has to getchallenge / connect).
func (r *relay) join(v *viewer) {
	v.state = vsNew
	v.lastValid = r.now
	v.resetLevel()
	r.viewers = append(r.viewers, v)
	r.metrics.ViewerJoined()
}

// remove forgets a viewer.
func (r *relay) remove(v *viewer, why LeaveReason) {
	if v.removed {
		return
	}
	v.removed = true
	for i, x := range r.viewers {
		if x == v {
			r.viewers = append(r.viewers[:i], r.viewers[i+1:]...)
			break
		}
	}
	r.metrics.ViewerLeft(why)
	r.logf("spectate: viewer %d left (%s)\n", v.id, why)
	if r.onRemove != nil {
		r.onRemove(v)
	}
}

// kick sends svc_disconnect times times (SV_FinalMessage style: unreliable
// data, after whatever reliable data is due) and removes the viewer.
func (r *relay) kick(v *viewer, why LeaveReason, times int) {
	if v.removed {
		return
	}
	if v.state >= vsConnected && !v.ch.Message.Overflowed {
		for i := 0; i < times; i++ {
			v.ch.Transmit([]byte{q2const.Svc_disconnect}, r.ms())
		}
	}
	r.remove(v, why)
}

// closeAll disconnects every viewer (Hub.Close).
func (r *relay) closeAll() {
	for _, v := range r.snapshot() {
		r.kick(v, LeaveClosed, 3)
	}
}

// tick runs the timers: timeouts, keepalives and reliable data waiting for
// an acknowledgement.
func (r *relay) tick() {
	for _, v := range r.snapshot() {
		if v.removed {
			continue
		}
		if r.now.Sub(v.lastValid) > r.cfg.timeout {
			r.remove(v, LeaveTimeout)
			continue
		}
		if v.waitDrain && r.now.Sub(v.drainSince) > r.cfg.stallTimeout {
			r.remove(v, LeaveSlow) // its connection does not take data any more
			continue
		}
		if v.state == vsNew || !r.canSend(v) {
			continue
		}
		if v.ch.NeedReliable() || r.ms()-v.ch.LastSent > int(r.cfg.keepalive/time.Millisecond) {
			r.transmit(v, nil)
		}
	}
}

// canSend reports whether datagrams may be queued for v: after a drop the
// relay waits until the queue has drained to half its capacity.
func (r *relay) canSend(v *viewer) bool {
	if !v.waitDrain {
		return true
	}
	if n, c := v.out.backlog(); n > c/2 {
		return false
	}
	v.waitDrain = false
	return true
}

// transmit sends one netchan datagram (reliable data due first, then data)
// and reports whether it was queued.
func (r *relay) transmit(v *viewer, data []byte) bool {
	v.sendFailed = false
	v.ch.Transmit(data, r.ms())
	if v.ch.FatalError {
		r.remove(v, LeaveOverflow)
		return false
	}
	if v.sendFailed {
		r.queueFull(v)
		return false
	}
	return true
}

// queueFull handles a dropped datagram: the viewer gets a keyframe once its
// queue drained; too many drops a minute disconnect it.
func (r *relay) queueFull(v *viewer) {
	if v.resync == "" {
		v.resync = ResyncDrop
	}
	if !v.waitDrain {
		v.waitDrain, v.drainSince = true, r.now
	}
	cut := r.now.Add(-r.cfg.dropWindow)
	n := 0
	for _, t := range v.drops {
		if t.After(cut) {
			v.drops[n] = t
			n++
		}
	}
	v.drops = append(v.drops[:n], r.now)
	if len(v.drops) > r.cfg.dropLimit {
		r.remove(v, LeaveSlow)
	}
}

// flushReliable sends reliable data that is due in a datagram of its own.
func (r *relay) flushReliable(v *viewer) {
	if !v.removed && v.state >= vsConnected && v.ch.NeedReliable() && r.canSend(v) {
		r.transmit(v, nil)
	}
}

// reliable appends to the viewer's reliable stream; an overflow (the
// client's backlog of unacknowledged reliable data is too large) drops the
// viewer like SV_SendClientMessages drops an overflowed client.
func (r *relay) reliable(v *viewer, write func(m *msg.SizeBuf)) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			if _, ce := p.(shared.ComError); !ce {
				panic(p)
			}
			ok = false
		}
		if !ok || v.ch.Message.Overflowed {
			ok = false
			r.remove(v, LeaveOverflow)
		}
	}()
	write(&v.ch.Message)
	return true
}

// ---------------------------------------------------------------------------
// Viewer datagrams

// packet handles one datagram from a viewer.
// C: server/sv_main.c:595 SV_ReadPackets (body for one packet)
func (r *relay) packet(v *viewer, data []byte) {
	if v.removed {
		return
	}
	r.metrics.DatagramIn()
	if len(data) > q2const.MAX_MSGLEN {
		return // NET_GetPacket: oversize packet
	}
	// check for connectionless packet (0xffffffff) first
	if len(data) >= 4 && binary.LittleEndian.Uint32(data) == 0xffffffff {
		r.connectionless(v, data)
		return
	}
	if v.state == vsNew || len(data) < 10 {
		return // not connected yet, or a runt
	}
	// the server routes sequenced packets by qport
	if int(binary.LittleEndian.Uint16(data[8:])) != v.ch.Qport {
		return
	}
	m := msg.NewReader(data)
	if !v.ch.Process(m, r.ms()) {
		return
	}
	v.lastValid = r.now
	r.clientMessage(v, m)
	r.flushReliable(v)
}

// connectionless handles getchallenge and connect; anything else is
// ignored without a reply.
// C: server/sv_main.c:476 SV_ConnectionlessPacket
func (r *relay) connectionless(v *viewer, data []byte) {
	m := msg.NewReader(data)
	m.MSG_ReadLong() // skip the -1 marker
	r.cmd.TokenizeString(m.MSG_ReadStringLine(), false)
	send := viewerSender{r, v}
	switch r.cmd.Argv(0) {
	case "getchallenge":
		// C: server/sv_main.c:222 SVC_GetChallenge (one address per connection)
		if v.challenge == 0 {
			v.challenge = r.cfg.challenge()
		}
		v.lastValid = r.now
		qnet.OutOfBandPrint(send, v.addr, fmt.Sprintf("challenge %d", v.challenge))
	case "connect":
		r.directConnect(v)
	}
}

// directConnect accepts a connect request.
// C: server/sv_main.c:262 SVC_DirectConnect
func (r *relay) directConnect(v *viewer) {
	send := viewerSender{r, v}
	if version := shared.Atoi(r.cmd.Argv(1)); version != q2const.PROTOCOL_VERSION {
		qnet.OutOfBandPrint(send, v.addr, fmt.Sprintf("print\nServer is version %4.2f.\n", q2const.VERSION))
		return
	}
	qport := int(shared.Atoi(r.cmd.Argv(2)))
	challenge := int(shared.Atoi(r.cmd.Argv(3)))
	// the userinfo (argv 4) is not used: viewers are anonymous
	if v.challenge == 0 {
		qnet.OutOfBandPrint(send, v.addr, "print\nNo challenge for address.\n")
		return
	}
	if challenge != v.challenge {
		qnet.OutOfBandPrint(send, v.addr, "print\nBad challenge.\n")
		return
	}
	v.lastValid = r.now
	qnet.OutOfBandPrint(send, v.addr, "client_connect")
	v.ch.Setup(q2const.NS_SERVER, v.addr, send, qport, r.ms())
	v.state = vsConnected
	v.resetLevel()
	v.waitDrain = false
}

// clientMessage parses a sequenced viewer packet minimally. Nothing in it
// ever reaches the bot or the game.
// C: server/sv_user.c:519 SV_ExecuteClientMessage
func (r *relay) clientMessage(v *viewer, m *msg.SizeBuf) {
	stringCmds := 0
	for !v.removed {
		if m.ReadCount > m.CurSize {
			r.kick(v, LeaveProtocol, 1) // SV_ReadClientMessage: badread
			return
		}
		c := m.MSG_ReadByte()
		if c == -1 {
			return
		}
		switch c {
		case q2const.Clc_nop:
		case q2const.Clc_userinfo:
			m.MSG_ReadString() // ignored: viewers have no player
		case q2const.Clc_move:
			m.MSG_ReadByte() // checksum: the usercmds are never executed
			r.lastFrame(v, m.MSG_ReadLong())
			return // the usercmds follow; a client sends nothing after them
		case q2const.Clc_stringcmd:
			s := m.MSG_ReadString()
			// malicious users may try using too many string commands
			stringCmds++
			if stringCmds < maxStringCmds {
				r.stringCmd(v, s)
			}
		default:
			r.kick(v, LeaveProtocol, 1) // SV_ReadClientMessage: unknown command char
			return
		}
	}
}

// lastFrame handles the lastframe of a clc_move: -1 while live means the
// viewer has no valid frame (a datagram was lost on the way), so it gets a
// keyframe, at most once per clientResync.
func (r *relay) lastFrame(v *viewer, lastframe int32) {
	if lastframe != -1 || v.state != vsSpawned || !v.live || v.resync != "" {
		return
	}
	if !v.lastClientResync.IsZero() && r.now.Sub(v.lastClientResync) < r.cfg.clientResync {
		return
	}
	v.lastClientResync = r.now
	v.resync = ResyncClient
	v.clearFrames() // what was sent did not all arrive
}

// stringCmd runs the allowlisted handshake commands; everything else (say,
// kill, killserver, download, nextserver, ...) is ignored.
// C: server/sv_user.c:462 SV_ExecuteUserCommand
func (r *relay) stringCmd(v *viewer, s string) {
	r.cmd.TokenizeString(s, false)
	switch r.cmd.Argv(0) {
	case "new":
		r.newViewer(v)
	case "configstrings":
		r.configstrings(v)
	case "baselines":
		r.baselines(v)
	case "begin":
		r.begin(v)
	case "disconnect":
		r.remove(v, LeaveDisconnect)
	}
}

// ---------------------------------------------------------------------------
// Handshake

// newViewer sends the serverdata of the bot's level (attractloop 1). It
// waits for the level to be ready.
// C: server/sv_user.c:55 SV_New_f
func (r *relay) newViewer(v *viewer) {
	if v.state != vsConnected {
		return // New not valid -- already spawned
	}
	l := r.level
	if l == nil || !l.Ready {
		v.pendingNew = true
		return
	}
	v.pendingNew = false
	if !r.reliable(v, func(m *msg.SizeBuf) {
		// send the serverdata
		m.MSG_WriteByte(q2const.Svc_serverdata)
		m.MSG_WriteLong(q2const.PROTOCOL_VERSION)
		m.MSG_WriteLong(l.ServerCount)
		m.MSG_WriteByte(1) // attract loop: the client only watches
		m.MSG_WriteString(l.GameDir)
		m.MSG_WriteShort(l.PlayerNum)
		m.MSG_WriteString(l.LevelName)
		if l.Game() {
			// begin fetching configstrings
			m.MSG_WriteByte(q2const.Svc_stufftext)
			m.MSG_WriteString(fmt.Sprintf("cmd configstrings %d 0\n", l.ServerCount))
		}
	}) {
		return
	}
	v.resetLevel()
	v.gen, v.sc = l.Gen, l.ServerCount
}

// sameLevel reports whether the handshake command's spawncount argument and
// the viewer's serverdata belong to the bot's current level.
func (r *relay) sameLevel(v *viewer) bool {
	l := r.level
	return l != nil && v.gen == l.Gen && shared.Atoi(r.cmd.Argv(1)) == l.ServerCount
}

// configstrings sends a packet full of configstrings.
// C: server/sv_user.c:125 SV_Configstrings_f
func (r *relay) configstrings(v *viewer) {
	if v.state != vsConnected {
		return // configstrings not valid -- already spawned
	}
	// handle the case of a level changing while a client was connecting
	if !r.sameLevel(v) {
		r.newViewer(v)
		return
	}
	l := r.level
	start := int(shared.Atoi(r.cmd.Argv(2)))
	if start < 0 {
		start = 0
	}
	r.reliable(v, func(m *msg.SizeBuf) {
		// write a packet full of data
		for m.CurSize < q2const.MAX_MSGLEN/2 && start < q2const.MAX_CONFIGSTRINGS {
			if cs := l.cs[start]; cs != "" {
				m.MSG_WriteByte(q2const.Svc_configstring)
				m.MSG_WriteShort(int32(start))
				m.MSG_WriteString(cs)
				v.known[start] = cs
			}
			start++
		}
		// send next command
		m.MSG_WriteByte(q2const.Svc_stufftext)
		if start == q2const.MAX_CONFIGSTRINGS {
			m.MSG_WriteString(fmt.Sprintf("cmd baselines %d 0\n", l.ServerCount))
		} else {
			m.MSG_WriteString(fmt.Sprintf("cmd configstrings %d %d\n", l.ServerCount, start))
		}
	})
}

// baselines sends a packet full of baselines, each delta-coded from a null
// state.
// C: server/sv_user.c:182 SV_Baselines_f
func (r *relay) baselines(v *viewer) {
	if v.state != vsConnected {
		return // baselines not valid -- already spawned
	}
	if !r.sameLevel(v) {
		r.newViewer(v)
		return
	}
	l := r.level
	start := int(shared.Atoi(r.cmd.Argv(2)))
	if start < 0 {
		start = 0
	}
	r.reliable(v, func(m *msg.SizeBuf) {
		var nullstate shared.EntityState
		for m.CurSize < q2const.MAX_MSGLEN/2 && start < q2const.MAX_EDICTS {
			// the bot holds exactly the baselines the server sent
			// (modelindex, sound or effects set)
			if l.base.Present[start] {
				base := l.base.State[start]
				base.Number = int32(start)
				m.MSG_WriteByte(q2const.Svc_spawnbaseline)
				m.MSG_WriteDeltaEntity(&nullstate, &base, true, true)
			}
			start++
		}
		m.MSG_WriteByte(q2const.Svc_stufftext)
		if start == q2const.MAX_EDICTS {
			m.MSG_WriteString(fmt.Sprintf("precache %d\n", l.ServerCount))
		} else {
			m.MSG_WriteString(fmt.Sprintf("cmd baselines %d %d\n", l.ServerCount, start))
		}
	})
}

// begin puts the viewer in the game: it gets the configstrings that changed
// during its handshake and a keyframe of the bot's latest frame.
// C: server/sv_user.c:238 SV_Begin_f
func (r *relay) begin(v *viewer) {
	if !r.sameLevel(v) {
		r.newViewer(v)
		return
	}
	if !r.level.Game() {
		return // nothing to spawn into on a cinematic or picture level
	}
	v.state = vsSpawned
	v.live = false
	v.resync = ResyncJoin
	if !r.sendConfigDiff(v) {
		return
	}
	if f := r.frame; f != nil && f.Gen == r.level.Gen {
		if r.canSend(v) {
			r.sendFrame(v, f)
		}
		return
	}
	// else: the keyframe goes out with the bot's first frame
	r.flushReliable(v)
}

// rehandshake makes the viewer start its handshake over on the same netchan
// ("changing" + "reconnect", what the server stuffs on a level change): the
// client goes back to connected and sends "new".
func (r *relay) rehandshake(v *viewer) bool {
	if !r.reliable(v, func(m *msg.SizeBuf) {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString("changing\n")
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString("reconnect\n")
	}) {
		return false
	}
	v.state = vsConnected
	v.resetLevel()
	v.waitDrain = false
	return true
}

// sendConfigDiff queues on the reliable stream every configstring the
// viewer may lack. A diff that does not fit restarts its handshake instead.
// It reports whether the viewer is still spawned.
func (r *relay) sendConfigDiff(v *viewer) bool {
	l := r.level
	need := 0
	for i := range l.cs {
		if v.known[i] != l.cs[i] {
			need += 1 + 2 + len(l.cs[i]) + 1
		}
	}
	if need == 0 {
		return true
	}
	if v.ch.Message.CurSize+need > v.ch.Message.MaxSize {
		r.rehandshake(v)
		return false
	}
	return r.reliable(v, func(m *msg.SizeBuf) {
		for i := range l.cs {
			if v.known[i] != l.cs[i] {
				m.MSG_WriteByte(q2const.Svc_configstring)
				m.MSG_WriteShort(int32(i))
				m.MSG_WriteString(l.cs[i])
				v.known[i] = l.cs[i]
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Bot messages

// bot handles one message of the bot (with the messages lost before it).
func (r *relay) bot(d delivery) {
	m := d.m
	if d.lost > 0 {
		r.metrics.Drop(DropFeed, int(min(d.lost, uint64(1<<31-1))))
		for _, v := range r.viewers {
			if v.state == vsSpawned && v.resync == "" {
				v.resync = ResyncFeed
			}
		}
	}
	l := m.Level
	if l == nil {
		return
	}
	newGen := r.level == nil || r.level.Gen != l.Gen
	r.level, r.frame = l, m.Frame
	if newGen {
		// a level change or reload: everyone does the handshake again
		for _, v := range r.snapshot() {
			if v.state == vsSpawned || (v.state == vsConnected && v.gen != 0) {
				if r.rehandshake(v) {
					r.flushReliable(v)
				}
			}
		}
	}
	if l.Ready {
		for _, v := range r.snapshot() {
			if !v.removed && v.state == vsConnected && v.pendingNew {
				r.newViewer(v)
				r.flushReliable(v)
			}
		}
	}
	if newGen || m.Payload == nil {
		return
	}
	for _, v := range r.snapshot() {
		if !v.removed && v.state == vsSpawned {
			r.forward(v, m)
		}
	}
}

// forward sends one bot message to a live viewer: the forwarded commands
// verbatim, with the frame replaced by a keyframe when the viewer needs one.
// Configstrings go on the reliable stream while that has data in flight, so
// a late retransmission can never overwrite a newer value.
func (r *relay) forward(v *viewer, m *Message) {
	if !r.canSend(v) {
		return
	}
	frameSpan := -1
	if m.FrameSpan >= 0 && m.FrameValid && m.Frame != nil {
		frameSpan = m.FrameSpan
		if f := m.Frame; v.resync == "" && f.DeltaFrame > 0 && !v.hasFrame(f.DeltaFrame) {
			v.resync = ResyncDelta
		}
		if v.resync != "" && !r.sendConfigDiff(v) {
			return
		}
	}
	// a frame for a viewer due for a keyframe resynchronises it: as a
	// keyframe, or verbatim when the bot's frame is uncompressed anyway
	resync := frameSpan >= 0 && v.resync != ""
	key := resync && m.Frame.DeltaFrame > 0
	busy := v.ch.ReliableLength > 0 || v.ch.Message.CurSize > 0
	chunks, saved := r.chunks[:0], r.saved[:0]
	for i, sp := range m.Spans {
		if !spanOK(m.Payload, sp) {
			continue
		}
		b := m.Payload[sp.Start:sp.End]
		switch {
		case sp.Cmd == q2const.Svc_frame:
			if i != frameSpan {
				continue // an invalid frame, or not the last of the message
			}
			if key {
				r.key.SZ_Clear()
				if err := writeKeyframe(&r.key, m.Frame, m.Level); err != nil {
					r.logf("spectate: %v\n", err)
					continue
				}
				b = r.key.Bytes()
			}
			chunks = append(chunks, chunk{b: b, frame: true})
		case sp.Cmd == q2const.Svc_configstring:
			idx, ok := configStringIndex(m.Payload, sp)
			if !ok {
				continue
			}
			if busy {
				if !r.reliable(v, func(rm *msg.SizeBuf) { rm.SZ_Write(b) }) {
					return
				}
			} else {
				saved = append(saved, savedCS{idx, v.known[idx]})
				chunks = append(chunks, chunk{b: b})
			}
			v.known[idx] = m.Level.cs[idx]
		case Forwarded(sp.Cmd):
			chunks = append(chunks, chunk{b: b})
		}
	}
	frameSent, ok := r.sendChunks(v, chunks)
	r.chunks, r.saved = chunks[:0], saved[:0]
	if v.removed {
		return
	}
	if !ok {
		// the dropped datagram may have carried any of them
		for i := len(saved) - 1; i >= 0; i-- {
			v.known[saved[i].i] = saved[i].old
		}
	}
	if frameSent {
		r.frameSent(v, m.Frame, resync)
	}
}

// sendFrame sends a standalone keyframe of f.
func (r *relay) sendFrame(v *viewer, f *FrameSnapshot) {
	r.key.SZ_Clear()
	if err := writeKeyframe(&r.key, f, r.level); err != nil {
		r.logf("spectate: %v\n", err)
		return
	}
	if sent, _ := r.sendChunks(v, []chunk{{b: r.key.Bytes(), frame: true}}); sent && !v.removed {
		r.frameSent(v, f, true)
	}
}

// frameSent records that frame f reached the viewer's send queue (resync:
// it resynchronised the viewer).
func (r *relay) frameSent(v *viewer, f *FrameSnapshot, resync bool) {
	v.frames[f.ServerFrame&q2const.UPDATE_MASK] = f.ServerFrame
	v.live = true
	if resync {
		r.metrics.Resync(v.resync)
		v.resync = ""
	}
}

// sendChunks sends chunks in order as unreliable data, packed into as few
// datagrams as fit. Reliable data that is due goes first, alone. It
// reports whether the frame chunk was queued and whether every datagram
// was.
func (r *relay) sendChunks(v *viewer, chunks []chunk) (frameSent, ok bool) {
	if v.ch.NeedReliable() && !r.transmit(v, nil) {
		return false, false
	}
	pkt, hasFrame := r.pkt[:0], false
	defer func() { r.pkt = pkt[:0] }()
	flush := func() bool {
		if len(pkt) == 0 {
			return true
		}
		if !r.transmit(v, pkt) {
			return false
		}
		frameSent = frameSent || hasFrame
		pkt, hasFrame = pkt[:0], false
		return true
	}
	for _, c := range chunks {
		if len(c.b) > maxDatagram {
			// a keyframe too large for any datagram (the viewer stays
			// due for one); a forwarded command never is
			r.metrics.Drop(DropSpan, 1)
			continue
		}
		if len(pkt)+len(c.b) > maxDatagram && !flush() {
			return frameSent, false
		}
		pkt = append(pkt, c.b...)
		hasFrame = hasFrame || c.frame
	}
	return frameSent, flush()
}
