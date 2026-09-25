// Command wsbridge relays datagrams between WebSocket clients and a UDP Quake 2
// server (ADR-0001: one binary WebSocket message == one UDP datagram). Each
// WebSocket connection gets its own UDP socket, so the server sees one client
// address per browser tab. It lets the browser client talk to the original C
// q2ded for differential testing:
//
//	wsbridge -listen :27911 -path /ws -udp 127.0.0.1:27910
package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/coder/websocket"

	q2net "quake2web/server/internal/net"
)

// bridge is the WebSocket handler relaying to one UDP target.
type bridge struct {
	target string
	logf   func(format string, args ...any)
}

func (b *bridge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := q2net.AcceptWS(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		b.logf("accept: %v", err)
		return
	}
	defer ws.Close()
	udp, err := q2net.DialUDP(b.target)
	if err != nil {
		b.logf("dial %s: %v", b.target, err)
		return
	}
	defer udp.Close()
	b.logf("%s connected -> %s", r.RemoteAddr, b.target)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() { // UDP -> WS
		defer cancel()
		for {
			d, err := udp.Recv(ctx)
			if err != nil {
				return
			}
			if err := ws.Send(d); err != nil {
				return
			}
		}
	}()
	for { // WS -> UDP
		d, err := ws.Recv(ctx)
		if err != nil {
			break
		}
		if err := udp.Send(d); err != nil {
			break
		}
	}
	b.logf("%s disconnected", r.RemoteAddr)
}

func main() {
	listen := flag.String("listen", ":27911", "HTTP listen address")
	path := flag.String("path", "/ws", "WebSocket path")
	target := flag.String("udp", "127.0.0.1:27910", "UDP server address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.Handle(*path, &bridge{target: *target, logf: log.Printf})
	log.Printf("wsbridge: ws://%s%s <-> udp://%s", *listen, *path, *target)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
