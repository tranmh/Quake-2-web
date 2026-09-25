package fakeclient

import (
	"context"
	"testing"
	"time"

	"quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// TestLiveQ2Ded connects to the original C dedicated server over UDP, completes
// the handshake and walks forward for 3 seconds.
func TestLiveQ2Ded(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	addr := testutil.StartQ2Ded(t, "+set", "deathmatch", "1", "+map", "demo1")
	conn, err := net.DialUDP(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := New(conn, Options{Qport: 4242, Printf: func(f string, a ...any) {}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v (prints %q)", err, c.Prints)
	}
	if c.ServerData.Protocol != q2const.PROTOCOL_VERSION || c.ConfigStrings[q2const.CS_MODELS+1] != "maps/demo1.bsp" {
		t.Fatalf("serverdata %+v model1 %q", c.ServerData, c.ConfigStrings[q2const.CS_MODELS+1])
	}
	if c.NumBaselines == 0 {
		t.Fatal("no baselines")
	}
	start := c.Origin()
	var maxEnts int
	end := time.Now().Add(3 * time.Second)
	for time.Now().Before(end) {
		c.SendCmd(shared.UserCmd{Msec: 25, ForwardMove: 200})
		if err := c.Poll(ctx, 25*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if c.Frame.NumEntities > maxEnts {
			maxEnts = c.Frame.NumEntities
		}
	}
	now := c.Origin()
	moved := shared.VectorLength(shared.Vec3{now[0] - start[0], now[1] - start[1], now[2] - start[2]})
	t.Logf("start %v end %v moved %.1f frames %d valid %d ents %d baselines %d",
		start, now, moved, c.FramesParsed, c.ValidFrames, maxEnts, c.NumBaselines)
	if moved < 50 {
		t.Fatalf("player did not move: %v -> %v", start, now)
	}
	if maxEnts == 0 {
		t.Fatal("no packet entities parsed")
	}
	if c.Frame.DeltaFrame <= 0 {
		t.Fatal("server never sent a delta frame")
	}
	c.Disconnect()
}
