package net

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func echoOnce(t *testing.T, a, b Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, p := range [][]byte{{0xff, 0xff, 0xff, 0xff, 'h', 'i'}, bytes.Repeat([]byte{1}, 1400)} {
		if err := a.Send(p); err != nil {
			t.Fatal(err)
		}
		got, err := b.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, p) {
			t.Fatalf("got %x want %x", got, p)
		}
	}
}

func TestMemPipe(t *testing.T) {
	a, b := MemPipe(4)
	echoOnce(t, a, b)
	echoOnce(t, b, a)
	// sender buffer is copied
	buf := []byte{1, 2, 3}
	_ = a.Send(buf)
	buf[0] = 9
	got, _ := b.Recv(context.Background())
	if got[0] != 1 {
		t.Fatal("not copied")
	}
	// overflow drops
	for i := 0; i < 10; i++ {
		_ = a.Send([]byte{byte(i)})
	}
	if len(b.(*memConn).in) != 4 {
		t.Fatal("expected drop at depth")
	}
	a.Close()
	if _, err := b.Recv(context.Background()); err != ErrClosed {
		t.Fatalf("recv after close: %v", err)
	}
	if err := b.Send([]byte{1}); err != ErrClosed {
		t.Fatalf("send after close: %v", err)
	}
}

func TestWS(t *testing.T) {
	srvConn := make(chan Conn, 1)
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := AcceptWS(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		srvConn <- c
		<-r.Context().Done()
	}))
	defer hs.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := DialWS(ctx, "ws"+strings.TrimPrefix(hs.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	sv := <-srvConn
	// a Recv timeout must not close the socket (coder/websocket closes on
	// a cancelled Read context)
	tctx, tcancel := context.WithTimeout(ctx, 10*time.Millisecond)
	if _, err := sv.Recv(tctx); err == nil {
		t.Fatal("expected timeout")
	}
	tcancel()
	echoOnce(t, cl, sv)
	echoOnce(t, sv, cl)
	go cl.Close()
	if _, err := sv.Recv(ctx); err == nil {
		t.Fatal("expected error after close")
	}
}

func TestUDP(t *testing.T) {
	l, err := ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan Packet, 4)
	go func() { _ = l.Serve(func(p Packet) { got <- p }) }()
	c, err := DialUDP(l.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	var p Packet
	select {
	case p = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	if string(p.Data) != "ping" || p.From.Base != "127.0.0.1" {
		t.Fatalf("bad packet %+v", p)
	}
	if err := p.Via.SendPacket(p.From, []byte("pong")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	d, err := c.Recv(ctx)
	if err != nil || string(d) != "pong" {
		t.Fatalf("recv %q %v", d, err)
	}
	// Recv honours context cancellation
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	if _, err := c.Recv(ctx2); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestAddr(t *testing.T) {
	a := Addr{Base: "1.2.3.4", Port: 5}
	b := Addr{Base: "1.2.3.4", Port: 6}
	if CompareAdr(a, b) || !CompareBaseAdr(a, b) || a.String() != "1.2.3.4:5" {
		t.Fatal("addr compare")
	}
	if !IsLocalAddress(Addr{Base: "loopback"}) || (Addr{Base: "loopback"}).String() != "loopback" {
		t.Fatal("loopback")
	}
}
