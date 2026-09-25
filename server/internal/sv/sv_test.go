package sv

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv/stubgame"
	"quake2web/server/internal/testutil"
)

// harness runs a Server on its own goroutine with a fast fake clock: one 100 ms
// game frame every 10 ms of wall time. All access to the server goes through
// do().
type harness struct {
	t     *testing.T
	s     *Server
	mu    sync.Mutex
	clock int
	stop  chan struct{}
	done  chan struct{}
	pkts  chan qnet.Packet
	out   bytes.Buffer
	nconn int
}

func demoFS(t *testing.T) *pak.FS {
	t.Helper()
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { fs.Close() })
	return fs
}

func newHarness(t *testing.T, cfg Config, cmds ...string) *harness {
	t.Helper()
	h := &harness{t: t, stop: make(chan struct{}), done: make(chan struct{}), pkts: make(chan qnet.Packet, 1024)}
	if cfg.FS == nil {
		cfg.FS = demoFS(t)
	}
	if cfg.Game == nil {
		cfg.Game = stubgame.New()
	}
	cfg.Clock = func() int { return h.clock }
	cfg.Printf = func(f string, a ...any) {
		h.out.WriteString(fmt.Sprintf(f, a...))
	}
	h.s = New(cfg)
	for _, c := range cmds {
		if err := h.s.ExecuteText(c + "\n"); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
	}
	go h.run()
	t.Cleanup(h.close)
	return h
}

func (h *harness) run() {
	defer close(h.done)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-h.stop:
			return
		case p := <-h.pkts:
			h.mu.Lock()
			_ = h.s.HandlePacket(0, p)
			h.mu.Unlock()
		case <-tick.C:
			h.mu.Lock()
			h.clock += 100
			_, _ = h.s.Frame(100)
			h.mu.Unlock()
		}
	}
}

func (h *harness) close() {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
	<-h.done
}

func (h *harness) do(fn func(s *Server)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(h.s)
}

// connect returns a client connection whose datagrams reach the server from
// base:<n>.
func (h *harness) connect(base string) qnet.Conn {
	cli, srv := qnet.MemPipe(1024)
	h.nconn++
	addr := qnet.Addr{Base: base, Port: h.nconn}
	go func() {
		for {
			d, err := srv.Recv(context.Background())
			if err != nil {
				return
			}
			select {
			case h.pkts <- qnet.Packet{From: addr, Via: qnet.ConnSender{C: srv}, Data: d}:
			case <-h.stop:
				return
			}
		}
	}()
	h.t.Cleanup(func() { srv.Close() })
	return cli
}

func (h *harness) client(base string, opt fakeclient.Options) *fakeclient.Client {
	h.t.Helper()
	c := fakeclient.New(h.connect(base), opt)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.Handshake(ctx); err != nil {
		h.t.Fatalf("handshake: %v\nserver output:\n%s", err, h.output())
	}
	return c
}

func (h *harness) output() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.out.String()
}

func poll(c *fakeclient.Client, n int, cmd shared.UserCmd) {
	ctx := context.Background()
	for i := 0; i < n; i++ {
		c.SendCmd(cmd)
		_ = c.Poll(ctx, 5*time.Millisecond)
	}
}

func dm(extra ...[2]string) Config {
	return Config{Dedicated: true, Cvars: append([][2]string{{"deathmatch", "1"}}, extra...)}
}

func TestSpawnServerConfigstrings(t *testing.T) {
	h := newHarness(t, dm(), "map demo1")
	h.do(func(s *Server) {
		if s.SV.State != ss_game || s.SV.Name != "demo1" {
			t.Fatalf("state %d name %q", s.SV.State, s.SV.Name)
		}
		if got := s.SV.ConfigStrings.Get(q2const.CS_MODELS + 1); got != "maps/demo1.bsp" {
			t.Errorf("CS_MODELS+1 %q", got)
		}
		if got := s.SV.ConfigStrings.Get(q2const.CS_MODELS + 2); got != "*1" {
			t.Errorf("CS_MODELS+2 %q", got)
		}
		if got := s.SV.ConfigStrings.Get(q2const.CS_AIRACCEL); got != "0" {
			t.Errorf("CS_AIRACCEL %q", got)
		}
		if s.maxclients.Value != 8 {
			t.Errorf("dedicated DM maxclients %v, want 8", s.maxclients.Value)
		}
		if got := s.Cvars.VariableString("mapname"); got != "demo1" {
			t.Errorf("mapname %q", got)
		}
		// the statusbar spills into the following slots like C's strcpy
		if got := s.SV.ConfigStrings.Get(q2const.CS_STATUSBAR); got != stubgame.StatusBar {
			t.Errorf("statusbar not stored whole")
		}
		if got := s.SV.ConfigStrings.Get(q2const.CS_STATUSBAR + 1); got != stubgame.StatusBar[q2const.MAX_QPATH:] {
			t.Errorf("slot after statusbar %q", got)
		}
		// SV_FindIndex: existing names are found, new ones appended
		mi := s.ModelIndex("players/male/tris.md2")
		if s.SV.ConfigStrings.Get(q2const.CS_MODELS+mi) != "players/male/tris.md2" {
			t.Errorf("modelindex %d", mi)
		}
		if s.ModelIndex("players/male/tris.md2") != mi || s.ModelIndex("") != 0 {
			t.Errorf("FindIndex not stable")
		}
	})
}

func TestAirAccelerateConfigstring(t *testing.T) {
	h := newHarness(t, dm([2]string{"sv_airaccelerate", "0.5"}), "map demo1")
	h.do(func(s *Server) {
		if got := s.SV.ConfigStrings.Get(q2const.CS_AIRACCEL); got != "0.5" {
			t.Errorf("CS_AIRACCEL %q", got)
		}
		if s.pmAirAccelerate != 0.5 {
			t.Errorf("pm_airaccelerate %v", s.pmAirAccelerate)
		}
	})
}

func TestHandshakeAndDelta(t *testing.T) {
	h := newHarness(t, dm(), "map demo1")
	c := h.client("10.1.1.1", fakeclient.Options{})

	// the whole statusbar arrives in CS_STATUSBAR
	if c.ConfigStrings[q2const.CS_STATUSBAR] != stubgame.StatusBar {
		t.Errorf("client statusbar %q", c.ConfigStrings[q2const.CS_STATUSBAR])
	}
	// configstrings were chunked at MAX_MSGLEN/2
	chunks := 0
	for _, s := range c.StuffTexts {
		if strings.HasPrefix(s, "cmd configstrings ") {
			chunks++
		}
	}
	if chunks < 2 {
		t.Errorf("configstrings sent in %d chunks, want >= 2 (statusbar spans slots)", chunks)
	}
	poll(c, 20, shared.UserCmd{Msec: 50, ForwardMove: 200})
	if !c.Frame.Valid || c.Frame.DeltaFrame <= 0 {
		t.Errorf("expected delta frames: valid %v delta %d", c.Frame.Valid, c.Frame.DeltaFrame)
	}
	var ping int
	h.do(func(s *Server) { ping = s.SVS.Clients[0].Ping })
	t.Logf("ping %d", ping)

	// a client that never acknowledges gets full frames
	nd := h.client("10.1.1.2", fakeclient.Options{NoDelta: true})
	poll(nd, 10, shared.UserCmd{Msec: 50})
	if !nd.Frame.Valid || nd.Frame.DeltaFrame != -1 {
		t.Errorf("nodelta client: valid %v delta %d", nd.Frame.Valid, nd.Frame.DeltaFrame)
	}
}

func TestOldDeltaGetsFullFrame(t *testing.T) {
	h := newHarness(t, dm(), "map demo1")
	c := h.client("10.1.1.3", fakeclient.Options{})
	poll(c, 5, shared.UserCmd{Msec: 50})
	var frame, full int
	h.do(func(s *Server) {
		cl := &s.SVS.Clients[0]
		frame = s.SV.FrameNum
		// pretend the client acknowledged a frame 13 frames ago
		cl.LastFrame = s.SV.FrameNum - (q2const.UPDATE_BACKUP - 3) + 1
		m := msg.NewSizeBuf(q2const.MAX_MSGLEN)
		s.SV.FrameNum++
		s.buildClientFrame(cl)
		s.writeFrameToClient(cl, m)
		r := msg.NewReader(m.Bytes())
		r.MSG_ReadByte()
		r.MSG_ReadLong()
		full = int(r.MSG_ReadLong())
	})
	if full != -1 {
		t.Errorf("frame %d: lastframe %d too old, want full frame (-1)", frame, full)
	}
}

func TestRateDrop(t *testing.T) {
	h := newHarness(t, dm(), "map demo1")
	c := h.client("10.1.1.4", fakeclient.Options{Userinfo: "\\name\\slow\\rate\\100"})
	poll(c, 20, shared.UserCmd{Msec: 50})
	var rate, supp int
	h.do(func(s *Server) {
		rate = s.SVS.Clients[0].Rate
		supp = s.SVS.Clients[0].SurpressCount
	})
	if rate != 100 {
		t.Errorf("rate %d", rate)
	}
	sawSupp := false
	for i := range c.Frames {
		if c.Frames[i].SurpressCount > 0 {
			sawSupp = true
		}
	}
	if !sawSupp && supp == 0 {
		t.Errorf("no rate suppression at rate 100")
	}
}

func TestOOBCommands(t *testing.T) {
	h := newHarness(t, dm([2]string{"rcon_password", "secret"}, [2]string{"hostname", "gotest"}), "map demo1")
	conn := h.connect("10.2.2.2")
	ask := func(q string) string {
		t.Helper()
		_ = conn.Send(append([]byte{0xff, 0xff, 0xff, 0xff}, q...))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d, err := conn.Recv(ctx)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if binary.LittleEndian.Uint32(d) != 0xffffffff {
			t.Fatalf("%s: not OOB", q)
		}
		return string(d[4:])
	}
	if got := ask("ping"); got != "ack" {
		t.Errorf("ping -> %q", got)
	}
	if got := ask("info 34"); !strings.HasPrefix(got, "info\n          gotest    demo1  0/ 8\n") {
		t.Errorf("info -> %q", got)
	}
	if got := ask("status"); !strings.HasPrefix(got, "print\n\\") || !strings.Contains(got, "\\mapname\\demo1") {
		t.Errorf("status -> %q", got)
	}
	if got := ask("rcon wrong status"); got != "print\nBad rcon_password.\n" {
		t.Errorf("bad rcon -> %q", got)
	}
	if got := ask("rcon secret status"); !strings.HasPrefix(got, "print\nmap              : demo1\n") {
		t.Errorf("rcon status -> %q", got)
	}
	if got := ask("connect 33 1 1 \"\\name\\x\""); got != "print\nServer is version 3.19.\n" {
		t.Errorf("old protocol -> %q", got)
	}
	if got := ask("connect 34 1 1 \"\\name\\x\""); got != "print\nBad challenge.\n" &&
		got != "print\nNo challenge for address.\n" {
		t.Errorf("no challenge -> %q", got)
	}
}

type memDemo struct {
	bytes.Buffer
	closed bool
}

func (m *memDemo) Close() error { m.closed = true; return nil }

func TestServerRecord(t *testing.T) {
	demo := &memDemo{}
	cfg := dm()
	cfg.DemoCreate = func(name string) (io.WriteCloser, error) { return demo, nil }
	h := newHarness(t, cfg, "map demo1")
	c := h.client("10.3.3.3", fakeclient.Options{})
	h.do(func(s *Server) { _ = s.ExecuteText("serverrecord test\n") })
	poll(c, 10, shared.UserCmd{Msec: 50, ForwardMove: 100})
	h.do(func(s *Server) { _ = s.ExecuteText("serverstop\n") })
	if !demo.closed {
		t.Fatal("demo not closed")
	}
	data := demo.Bytes()
	blocks := 0
	for len(data) >= 4 {
		n := int(binary.LittleEndian.Uint32(data))
		if n <= 0 || n+4 > len(data) {
			t.Fatalf("bad block length %d", n)
		}
		b := data[4 : 4+n]
		r := msg.NewReader(b)
		cmd := r.MSG_ReadByte()
		if blocks == 0 {
			if cmd != q2const.Svc_serverdata || r.MSG_ReadLong() != q2const.PROTOCOL_VERSION {
				t.Fatalf("first block does not start with serverdata")
			}
			r.MSG_ReadLong()
			if r.MSG_ReadByte() != 2 {
				t.Fatalf("server demo attractloop != 2")
			}
			r.MSG_ReadString()
			if r.MSG_ReadShort() != -1 {
				t.Fatalf("playernum != -1")
			}
			if !bytes.Contains(b, []byte("maps/demo1.bsp")) {
				t.Fatalf("configstrings missing")
			}
		} else {
			if cmd != q2const.Svc_frame {
				t.Fatalf("block %d starts with %d", blocks, cmd)
			}
			r.MSG_ReadLong()
			if r.MSG_ReadByte() != q2const.Svc_packetentities {
				t.Fatalf("block %d: no packetentities", blocks)
			}
		}
		blocks++
		data = data[4+n:]
	}
	if blocks < 5 {
		t.Errorf("only %d demo blocks", blocks)
	}
}

func TestSaveLoad(t *testing.T) {
	saves := NewMemSaveStore()
	h := newHarness(t, Config{Saves: saves}, "map demo1")
	c := h.client("10.4.4.4", fakeclient.Options{})
	poll(c, 3, shared.UserCmd{Msec: 50})
	h.do(func(s *Server) {
		if s.maxclients.Value != 1 {
			t.Fatalf("single player maxclients %v", s.maxclients.Value)
		}
		s.SVS.Clients[0].Edict.Client.PS.Stats[q2const.STAT_HEALTH] = 100
		_ = s.ExecuteText("save s1\n")
	})
	names, _ := saves.List("s1")
	want := []string{"demo1.sav", "demo1.sv2", "game.ssv", "server.ssv"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("save slot contents %v\n%s", names, h.output())
	}
	// autosave slot from the initial "map" (not dedicated)
	if n, _ := saves.List("save0"); len(n) == 0 {
		t.Errorf("no autosave")
	}

	var spawn0 int
	h.do(func(s *Server) {
		spawn0 = s.SVS.SpawnCount
		_ = s.ExecuteText("load s1\n")
		if s.SV.State != ss_game || s.SV.Name != "demo1" || !s.SV.LoadGame {
			t.Errorf("after load: state %d name %q loadgame %v", s.SV.State, s.SV.Name, s.SV.LoadGame)
		}
		if s.SVS.SpawnCount == spawn0 {
			t.Errorf("spawncount unchanged")
		}
		if s.SV.ConfigStrings.Get(q2const.CS_MODELS+1) != "maps/demo1.bsp" {
			t.Errorf("configstrings not restored")
		}
	})

	// level change and back: the level is restored and run for 100 frames
	var before, after int
	h.do(func(s *Server) {
		_ = s.ExecuteText("gamemap demo2\n")
		if s.SV.Name != "demo2" {
			t.Errorf("gamemap: %q", s.SV.Name)
		}
		g := s.Game().(*stubgame.Game)
		before = g.FrameNum
		_ = s.ExecuteText("gamemap demo1\n")
		after = s.Game().(*stubgame.Game).FrameNum
	})
	if after-before != 2+100 {
		t.Errorf("re-entering a level ran %d frames, want 102 (2 settle + 100 SV_CheckForSavegame)", after-before)
	}
}

func TestKickAndDrop(t *testing.T) {
	h := newHarness(t, dm(), "map demo1")
	c := h.client("10.5.5.5", fakeclient.Options{Userinfo: "\\name\\victim\\rate\\25000"})
	h.do(func(s *Server) { _ = s.ExecuteText("kick victim\n") })
	poll(c, 10, shared.UserCmd{Msec: 50})
	if !c.Disconnected {
		t.Errorf("client not disconnected")
	}
	h.do(func(s *Server) {
		if s.SVS.Clients[0].State != cs_zombie && s.SVS.Clients[0].State != cs_free {
			t.Errorf("client state %d", s.SVS.Clients[0].State)
		}
	})
}

func TestComErrorDropKillsServer(t *testing.T) {
	s := New(Config{FS: demoFS(t), Game: stubgame.New(), Dedicated: true})
	if err := s.ExecuteText("map demo1\n"); err != nil {
		t.Fatal(err)
	}
	err := s.ExecuteText("sv x\n") // stub records it; fine
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer s.recoverError(&err)
		s.gi.Error("boom")
	}()
	if err == nil || !s.Killed() || s.SVS.Initialized {
		t.Fatalf("ERR_DROP: err %v killed %v", err, s.Killed())
	}
}
