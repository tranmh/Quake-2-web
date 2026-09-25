package host

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
	"quake2web/server/internal/testutil"
)

func demoFS(t *testing.T) *pak.FS {
	t.Helper()
	p := testutil.DemoPak(t)
	pk, err := pak.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { fs.Close() })
	return fs
}

func startDemo1(t *testing.T, h *Host, id string) *Instance {
	t.Helper()
	inst, err := h.Create(InstanceConfig{
		ID: id,
		Server: sv.Config{
			FS:        demoFS(t),
			Game:      stubgame.New(),
			Dedicated: true,
			Printf:    func(f string, a ...any) { t.Logf("sv: "+strings.TrimRight(f, "\n"), a...) },
		},
		Commands: []string{"map demo1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Stop)
	return inst
}

// walk runs the full handshake and walks forward for 5 seconds of frames.
func walk(t *testing.T, conn qnet.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c := fakeclient.New(conn, fakeclient.Options{Printf: func(f string, a ...any) {}})
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if c.ServerData.Protocol != q2const.PROTOCOL_VERSION {
		t.Fatalf("protocol %d", c.ServerData.Protocol)
	}
	if got := c.ConfigStrings[q2const.CS_MODELS+1]; got != "maps/demo1.bsp" {
		t.Fatalf("CS_MODELS+1 = %q", got)
	}
	if c.NumBaselines == 0 {
		t.Fatalf("no baselines")
	}
	start := c.Origin()
	firstFrame := c.Frame.ServerFrame

	var maxEnts int
	lastLogged := int32(-1)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.SendCmd(shared.UserCmd{Msec: 25, ForwardMove: 200})
		if err := c.Poll(ctx, 25*time.Millisecond); err != nil {
			t.Fatalf("poll: %v", err)
		}
		if testing.Verbose() && c.Frame.ServerFrame%10 == 0 && c.Frame.ServerFrame != lastLogged {
			lastLogged = c.Frame.ServerFrame
			t.Logf("frame %d origin %v vel %v", c.Frame.ServerFrame, c.Origin(), c.Frame.PlayerState.PMove.Velocity)
		}
		if n := len(c.FrameEntities(&c.Frame)); n > maxEnts {
			maxEnts = n
		}
	}
	end := c.Origin()
	moved := shared.VectorLength(shared.VectorSubtract(end, start))
	frames := c.Frame.ServerFrame - firstFrame
	t.Logf("moved %.1f units from %v to %v in %d server frames, %d valid frames, max %d entities",
		moved, start, end, frames, c.ValidFrames, maxEnts)
	if moved < 100 {
		t.Errorf("player did not move: %v -> %v", start, end)
	}
	if frames < 40 || frames > 60 {
		t.Errorf("unexpected number of server frames in 5 s: %d", frames)
	}
	if maxEnts < 1 {
		t.Errorf("no packet entities parsed")
	}
	// the player's own entity must be in the frame
	found := false
	for _, e := range c.FrameEntities(&c.Frame) {
		if e.Number == c.ServerData.PlayerNum+1 {
			found = true
		}
	}
	if !found {
		t.Errorf("player entity %d not in frame", c.ServerData.PlayerNum+1)
	}
	if !c.Frame.Valid || c.Frame.DeltaFrame <= 0 {
		t.Errorf("expected valid delta frames, got valid=%v delta=%d", c.Frame.Valid, c.Frame.DeltaFrame)
	}
	c.Disconnect()
}

func TestWalkInMemory(t *testing.T) {
	h := New()
	inst := startDemo1(t, h, "mem")
	walk(t, inst.ConnectMem("10.0.0.1"))
}

func TestWalkWebSocket(t *testing.T) {
	h := New()
	h.RequireTickets = true
	startDemo1(t, h, "g1")
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	ctx := context.Background()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/games/g1"
	if c, err := qnet.DialWS(ctx, url+"?ticket=bogus"); err == nil {
		c.Close()
		t.Fatal("connected without a valid ticket")
	}
	tk, err := h.IssueTicket("g1")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := qnet.DialWS(ctx, url+"?ticket="+tk)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	walk(t, conn)
}

func TestTwoClientsSeeEachOther(t *testing.T) {
	h := New()
	inst := startDemo1(t, h, "two")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := fakeclient.New(inst.ConnectMem("10.0.0.1"), fakeclient.Options{})
	b := fakeclient.New(inst.ConnectMem("10.0.0.2"), fakeclient.Options{})
	if err := a.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	if err := b.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		a.SendCmd(shared.UserCmd{Msec: 50})
		b.SendCmd(shared.UserCmd{Msec: 50})
		_ = a.Poll(ctx, 25*time.Millisecond)
		_ = b.Poll(ctx, 25*time.Millisecond)
	}
	seen := false
	for _, e := range a.FrameEntities(&a.Frame) {
		if e.Number == b.ServerData.PlayerNum+1 {
			seen = true
		}
	}
	if !seen {
		t.Errorf("client A does not see client B (entity %d)", b.ServerData.PlayerNum+1)
	}
	var status string
	_ = inst.Do(func(s *sv.Server) {
		n := 0
		for i := range s.SVS.Clients {
			if s.SVS.Clients[i].State != 0 {
				n++
			}
		}
		status = strings.Repeat("x", n)
	})
	if len(status) != 2 {
		t.Errorf("expected 2 clients, got %d", len(status))
	}
}

func TestBadMapFails(t *testing.T) {
	h := New()
	_, err := h.Create(InstanceConfig{
		ID:       "bad",
		Server:   sv.Config{FS: demoFS(t), Game: stubgame.New(), Dedicated: true},
		Commands: []string{"map nosuchmap"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := h.Get("bad"); err == nil {
		t.Fatal("instance still registered")
	}
}

func TestHandshakeUDP(t *testing.T) {
	h := New()
	startDemo1(t, h, "udp")
	l, err := qnet.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() { _ = h.ServeUDP(l, "udp") }()
	conn, err := qnet.DialUDP(l.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := fakeclient.New(conn, fakeclient.Options{})
	if err := c.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		c.SendCmd(shared.UserCmd{Msec: 50, ForwardMove: 200})
		_ = c.Poll(ctx, 50*time.Millisecond)
	}
	if !c.Frame.Valid || c.Frame.DeltaFrame <= 0 {
		t.Errorf("valid %v delta %d", c.Frame.Valid, c.Frame.DeltaFrame)
	}
	c.Disconnect()
}
