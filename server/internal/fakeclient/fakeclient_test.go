package fakeclient

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cmd"
	"quake2web/server/internal/qcommon/crc"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// scriptServer is a minimal protocol-34 server used to exercise the client.
type scriptServer struct {
	t       *testing.T
	conn    net.Conn
	chan_   net.Netchan
	frame   int32
	moves   []shared.UserCmd
	lastAck int32
	strings []string
	begun   bool
	ents    []shared.EntityState
	ps      shared.PlayerState
	sent    map[int32]struct {
		ps   shared.PlayerState
		ents []shared.EntityState
	}
	cmdErr error
}

func (s *scriptServer) oob(text string) {
	net.OutOfBandPrint(net.ConnSender{C: s.conn}, net.Addr{}, text)
}

func (s *scriptServer) handle(data []byte) {
	m := msg.NewReader(data)
	if m.MSG_ReadLong() == -1 {
		line := m.MSG_ReadStringLine()
		tk := cmd.New(nil)
		tk.TokenizeString(line, false)
		switch tk.Argv(0) {
		case "getchallenge":
			s.oob("challenge 4321")
		case "connect":
			if tk.Argv(1) != "34" || tk.Argv(3) != "4321" || !strings.Contains(tk.Argv(4), `\name\`) {
				s.oob("print\nbad connect\n")
				return
			}
			s.chan_.Setup(q2const.NS_SERVER, net.Addr{}, net.ConnSender{C: s.conn}, 0, 0)
			s.oob("client_connect")
		}
		return
	}
	if !s.chan_.Process(m, 0) {
		return
	}
	for {
		c := m.MSG_ReadByte()
		switch c {
		case -1:
			return
		case q2const.Clc_nop:
		case q2const.Clc_stringcmd:
			str := m.MSG_ReadString()
			s.strings = append(s.strings, str)
			s.stringCmd(str)
		case q2const.Clc_userinfo:
			s.strings = append(s.strings, "userinfo:"+m.MSG_ReadString())
		case q2const.Clc_move:
			checksumIndex := m.ReadCount
			checksum := byte(m.MSG_ReadByte())
			lastframe := m.MSG_ReadLong()
			var null shared.UserCmd
			o := m.MSG_ReadDeltaUsercmd(&null)
			n1 := m.MSG_ReadDeltaUsercmd(&o)
			n2 := m.MSG_ReadDeltaUsercmd(&n1)
			calc := crc.COM_BlockSequenceCRCByte(m.Data[checksumIndex+1:m.ReadCount], int32(s.chan_.IncomingSequence))
			if calc != checksum {
				s.cmdErr = fmt.Errorf("checksum %d != %d", checksum, calc)
			}
			s.lastAck = lastframe
			s.moves = append(s.moves, n2)
		default:
			s.t.Errorf("bad clc %d", c)
			return
		}
	}
}

func (s *scriptServer) stringCmd(str string) {
	tk := cmd.New(nil)
	tk.TokenizeString(str, false)
	w := &s.chan_.Message
	switch tk.Argv(0) {
	case "new":
		w.MSG_WriteByte(q2const.Svc_serverdata)
		w.MSG_WriteLong(q2const.PROTOCOL_VERSION)
		w.MSG_WriteLong(7)
		w.MSG_WriteByte(0)
		w.MSG_WriteString("")
		w.MSG_WriteShort(0)
		w.MSG_WriteString("Test Level")
		w.MSG_WriteByte(q2const.Svc_stufftext)
		w.MSG_WriteString("cmd configstrings 7 0\n")
	case "configstrings":
		w.MSG_WriteByte(q2const.Svc_configstring)
		w.MSG_WriteShort(q2const.CS_MODELS + 1)
		w.MSG_WriteString("maps/test.bsp")
		var null shared.EntityState
		base := shared.EntityState{Number: 5, ModelIndex: 3, Origin: shared.Vec3{10, 20, 30}}
		w.MSG_WriteByte(q2const.Svc_spawnbaseline)
		w.MSG_WriteDeltaEntity(&null, &base, true, true)
		w.MSG_WriteByte(q2const.Svc_stufftext)
		w.MSG_WriteString("precache 7\n")
	case "begin":
		if tk.Argv(1) == "7" {
			s.begun = true
		}
	}
}

// sendFrame emits svc_frame deltaed against the client's last ack, like
// SV_WriteFrameToClient (simplified).
func (s *scriptServer) sendFrame(extra func(w *msg.SizeBuf)) {
	s.frame++
	w := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	var oldps *shared.PlayerState
	var oldents []shared.EntityState
	delta := int32(-1)
	if old, ok := s.sent[s.lastAck]; ok && s.lastAck > 0 {
		oldps, oldents, delta = &old.ps, old.ents, s.lastAck
	}
	w.MSG_WriteByte(q2const.Svc_frame)
	w.MSG_WriteLong(s.frame)
	w.MSG_WriteLong(delta)
	w.MSG_WriteByte(0)
	w.MSG_WriteByte(1)
	w.MSG_WriteByte(0xff)
	w.WriteDeltaPlayerstate(oldps, &s.ps)
	// packet entities (SV_EmitPacketEntities)
	w.MSG_WriteByte(q2const.Svc_packetentities)
	oi, ni := 0, 0
	for oi < len(oldents) || ni < len(s.ents) {
		oldnum, newnum := int32(9999), int32(9999)
		if oi < len(oldents) {
			oldnum = oldents[oi].Number
		}
		if ni < len(s.ents) {
			newnum = s.ents[ni].Number
		}
		switch {
		case newnum == oldnum:
			w.MSG_WriteDeltaEntity(&oldents[oi], &s.ents[ni], false, s.ents[ni].Number <= 1)
			oi++
			ni++
		case newnum < oldnum:
			base := shared.EntityState{}
			if newnum == 5 {
				base = shared.EntityState{Number: 5, ModelIndex: 3, Origin: shared.Vec3{10, 20, 30}}
			}
			w.MSG_WriteDeltaEntity(&base, &s.ents[ni], true, true)
			ni++
		default:
			bits := uint32(q2const.U_REMOVE)
			if oldnum >= 256 {
				bits |= q2const.U_NUMBER16 | q2const.U_MOREBITS1
			}
			w.MSG_WriteByte(int32(bits & 255))
			if bits&0x0000ff00 != 0 {
				w.MSG_WriteByte(int32((bits >> 8) & 255))
			}
			if bits&q2const.U_NUMBER16 != 0 {
				w.MSG_WriteShort(oldnum)
			} else {
				w.MSG_WriteByte(oldnum)
			}
			oi++
		}
	}
	w.MSG_WriteShort(0)
	if extra != nil {
		extra(w)
	}
	if s.sent == nil {
		s.sent = map[int32]struct {
			ps   shared.PlayerState
			ents []shared.EntityState
		}{}
	}
	s.sent[s.frame] = struct {
		ps   shared.PlayerState
		ents []shared.EntityState
	}{s.ps, append([]shared.EntityState(nil), s.ents...)}
	s.chan_.Transmit(w.Bytes(), 0)
}

func TestScriptedHandshakeAndFrames(t *testing.T) {
	cc, sc := net.MemPipe(64)
	s := &scriptServer{t: t, conn: sc}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srvDone := make(chan struct{})
	go func() {
		defer close(srvDone)
		for {
			d, err := sc.Recv(ctx)
			if err != nil {
				return
			}
			s.handle(d)
			// flush reliable replies
			if s.chan_.Message.CurSize > 0 && !s.begun {
				s.chan_.Transmit(nil, 0)
			}
		}
	}()
	// server goroutine only; the frame sending below runs from the test after
	// synchronising through the client state, so no data race on s.
	c := New(cc, Options{Qport: 999})
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	// run until "begin" was sent
	for !c.BeginSent {
		if err := c.Poll(ctx, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if c.ServerData.ServerCount != 7 || c.ServerData.LevelName != "Test Level" {
		t.Fatalf("serverdata %+v", c.ServerData)
	}
	if c.ConfigStrings[q2const.CS_MODELS+1] != "maps/test.bsp" || c.NumBaselines != 1 ||
		c.Entities[5].Baseline.ModelIndex != 3 {
		t.Fatal("configstrings / baselines not parsed")
	}
	time.Sleep(50 * time.Millisecond) // let the server read "begin"
	cancel()
	<-srvDone
	if !s.begun {
		t.Fatalf("server did not get begin: %q", s.strings)
	}

	// From here on drive the server synchronously.
	pump := func() {
		ctx2, c2 := context.WithTimeout(context.Background(), time.Second)
		defer c2()
		for {
			d, err := sc.Recv(ctx2)
			if err != nil {
				return
			}
			s.handle(d)
			ctx3, c3 := context.WithTimeout(context.Background(), 5*time.Millisecond)
			d2, err := sc.Recv(ctx3)
			c3()
			if err != nil {
				return
			}
			s.handle(d2)
		}
	}
	ctx4 := context.Background()
	s.ps.PMove.Origin = [3]int16{80, 160, 240}
	s.ents = []shared.EntityState{{Number: 1, ModelIndex: 255}, {Number: 5, ModelIndex: 3, Origin: shared.Vec3{10, 20, 30}}}
	s.sendFrame(nil)
	if err := c.WaitActive(ctx4); err != nil {
		t.Fatal(err)
	}
	if o := c.Origin(); o != (shared.Vec3{10, 20, 30}) {
		t.Fatalf("origin %v", o)
	}
	for f := 0; f < 10; f++ {
		c.SendCmd(shared.UserCmd{Msec: 100, ForwardMove: 200, Angles: [3]int16{0, int16(f * 100), 0}})
		pump()
		if s.cmdErr != nil {
			t.Fatal(s.cmdErr)
		}
		s.ps.PMove.Origin[0] += 8
		s.ents[1].Origin[0] += 1
		if f == 4 {
			s.ents = append(s.ents, shared.EntityState{Number: 300, ModelIndex: 2})
		}
		if f == 7 {
			s.ents = s.ents[:2] // remove 300
		}
		s.sendFrame(func(w *msg.SizeBuf) {
			w.MSG_WriteByte(q2const.Svc_temp_entity)
			w.MSG_WriteByte(q2const.TE_STEAM)
			w.MSG_WriteShort(3)
			w.MSG_WriteByte(1)
			w.MSG_WritePos(shared.Vec3{})
			dir := shared.Vec3{0, 0, 1}
			w.MSG_WriteDir(&dir)
			w.MSG_WriteByte(1)
			w.MSG_WriteShort(1)
			w.MSG_WriteLong(1)
			w.MSG_WriteByte(q2const.Svc_print)
			w.MSG_WriteByte(2)
			w.MSG_WriteString(fmt.Sprintf("frame %d\n", f))
		})
		if err := c.Poll(ctx4, 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if !c.Frame.Valid || c.Frame.ServerFrame != s.frame {
			t.Fatalf("frame %d: %+v", f, c.Frame)
		}
		ents := c.FrameEntities(&c.Frame)
		if len(ents) != len(s.ents) {
			t.Fatalf("frame %d: %d ents want %d", f, len(ents), len(s.ents))
		}
		for i := range ents {
			want := s.ents[i]
			want.OldOrigin = ents[i].OldOrigin // the client sets old_origin from the delta source
			if ents[i] != want {
				t.Fatalf("frame %d ent %d: %+v want %+v", f, i, ents[i], s.ents[i])
			}
		}
		if c.Frame.PlayerState.PMove.Origin != s.ps.PMove.Origin {
			t.Fatal("playerstate")
		}
	}
	if len(s.moves) < 10 || s.moves[len(s.moves)-1].ForwardMove != 200 || s.moves[len(s.moves)-1].Angles[1] != 900 {
		t.Fatalf("moves %+v", s.moves)
	}
	if s.lastAck <= 0 {
		t.Fatal("client never acked a frame for delta")
	}
	if c.Frame.DeltaFrame <= 0 {
		t.Fatal("server never deltaed")
	}
	if len(c.TempEnts) == 0 || len(c.Prints) == 0 {
		t.Fatal("tempents/prints not parsed")
	}

	// reconnect sequence: changing + reconnect -> "new" is resent
	s.chan_.Message.MSG_WriteByte(q2const.Svc_stufftext)
	s.chan_.Message.MSG_WriteString("changing\n")
	s.chan_.Message.MSG_WriteByte(q2const.Svc_stufftext)
	s.chan_.Message.MSG_WriteString("reconnect\n")
	s.chan_.Transmit(nil, 0)
	if err := c.Poll(ctx4, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if c.State != CaConnected {
		t.Fatalf("state %d", c.State)
	}
	c.SendCmd(shared.UserCmd{})
	pump()
	if s.strings[len(s.strings)-1] != "new" {
		t.Fatalf("expected new, got %q", s.strings)
	}

	// svc_disconnect
	s.chan_.Message.MSG_WriteByte(q2const.Svc_disconnect)
	s.chan_.Transmit(nil, 0)
	if err := c.Poll(ctx4, 20*time.Millisecond); err != ErrDisconnected || !c.Disconnected {
		t.Fatalf("disconnect: %v", err)
	}
}

func TestRejectedConnect(t *testing.T) {
	cc, sc := net.MemPipe(8)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		for {
			d, err := sc.Recv(ctx)
			if err != nil {
				return
			}
			if strings.Contains(string(d[4:]), "getchallenge") {
				net.OutOfBandPrint(net.ConnSender{C: sc}, net.Addr{}, "challenge 1")
			} else {
				net.OutOfBandPrint(net.ConnSender{C: sc}, net.Addr{}, "print\nServer is full.\n")
			}
		}
	}()
	c := New(cc, Options{})
	if err := c.Connect(ctx); err != ErrDisconnected || c.DisconnectMsg != "Server is full.\n" {
		t.Fatalf("got %v %q", err, c.DisconnectMsg)
	}
}

func TestSendCmdChecksumLayout(t *testing.T) {
	cc, sc := net.MemPipe(8)
	c := New(cc, Options{Qport: 0x1234})
	c.Netchan.Setup(q2const.NS_CLIENT, net.Addr{}, net.ConnSender{C: cc}, 0x1234, 0)
	c.State = CaActive
	c.Frame = Frame{Valid: true, ServerFrame: 42}
	payload := c.SendCmd(shared.UserCmd{Msec: 50, ForwardMove: 400, Buttons: 1})
	d, _ := sc.Recv(context.Background())
	if len(d) != 10+len(payload) || d[8] != 0x34 || d[9] != 0x12 {
		t.Fatalf("header %x", d)
	}
	m := msg.NewReader(payload)
	if m.MSG_ReadByte() != q2const.Clc_move {
		t.Fatal("clc_move")
	}
	ck := byte(m.MSG_ReadByte())
	if m.MSG_ReadLong() != 42 {
		t.Fatal("lastframe")
	}
	if crc.COM_BlockSequenceCRCByte(payload[2:], 1) != ck {
		t.Fatal("checksum")
	}
	var null shared.UserCmd
	a := m.MSG_ReadDeltaUsercmd(&null)
	b := m.MSG_ReadDeltaUsercmd(&a)
	cmd := m.MSG_ReadDeltaUsercmd(&b)
	if a != null || b != null || cmd.ForwardMove != 400 || cmd.Msec != 50 {
		t.Fatalf("cmds %+v %+v %+v", a, b, cmd)
	}
}
