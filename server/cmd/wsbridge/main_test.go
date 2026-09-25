package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/fakeclient"
	q2net "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

func TestBridgeRelays(t *testing.T) {
	// UDP echo server that answers each datagram with "echo:" + payload.
	l, err := q2net.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		_ = l.Serve(func(p q2net.Packet) {
			_ = p.Via.SendPacket(p.From, append([]byte("echo:"), p.Data...))
		})
	}()

	hs := httptest.NewServer(&bridge{target: l.LocalAddr().String(), logf: t.Logf})
	defer hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := q2net.DialWS(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, p := range [][]byte{[]byte("\xff\xff\xff\xffgetchallenge\n"), bytes.Repeat([]byte{7}, 1400)} {
		if err := ws.Send(p); err != nil {
			t.Fatal(err)
		}
		got, err := ws.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, append([]byte("echo:"), p...)) {
			t.Fatalf("got %q", got)
		}
	}
}

// TestBridgeToQ2Ded runs the fake client over WebSocket through the bridge to
// the original C dedicated server (skipped without oracle/build/bin/q2ded).
func TestBridgeToQ2Ded(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	addr := testutil.StartQ2Ded(t, "+set", "deathmatch", "1", "+map", "demo1")
	hs := httptest.NewServer(&bridge{target: addr, logf: t.Logf})
	defer hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ws, err := q2net.DialWS(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := fakeclient.New(ws, fakeclient.Options{Qport: 77})
	if err := c.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	if c.ConfigStrings[q2const.CS_MODELS+1] != "maps/demo1.bsp" || !c.Frame.Valid {
		t.Fatal("handshake through bridge incomplete")
	}
	c.Disconnect()
}
