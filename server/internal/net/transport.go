// Package net ports qcommon/net_chan.c (the netchan) and provides the datagram
// transports the server and test clients use: an in-memory pair, WebSocket
// (one binary message == one datagram, ADR-0001) and raw UDP.
package net

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Addr is C netadr_t reduced to what the engine needs: Base identifies the
// remote host (NET_CompareBaseAdr) and Port the endpoint on it. For WebSocket
// and in-memory transports Port is a unique connection number, so the qport
// rebinding of SV_ReadPackets works across a reconnected socket.
type Addr struct {
	Base string
	Port int
}

// String is NET_AdrToString.
func (a Addr) String() string {
	if a.Base == "loopback" {
		return "loopback"
	}
	return fmt.Sprintf("%s:%d", a.Base, a.Port)
}

// CompareAdr is NET_CompareAdr.
func CompareAdr(a, b Addr) bool { return a == b }

// CompareBaseAdr is NET_CompareBaseAdr.
func CompareBaseAdr(a, b Addr) bool { return a.Base == b.Base }

// IsLocalAddress is NET_IsLocalAddress.
func IsLocalAddress(a Addr) bool { return a.Base == "loopback" }

// Sender delivers one datagram to an address (NET_SendPacket). The server keeps
// the Sender a packet arrived through next to the remote address.
type Sender interface {
	SendPacket(to Addr, data []byte) error
}

// Packet is one received datagram with its origin.
type Packet struct {
	From Addr
	Via  Sender
	Data []byte
}

// Conn is a datagram connection to a single peer.
type Conn interface {
	// Send transmits one datagram.
	Send(data []byte) error
	// Recv blocks for the next datagram.
	Recv(ctx context.Context) ([]byte, error)
	Close() error
}

// ErrClosed is returned by operations on a closed connection.
var ErrClosed = errors.New("net: connection closed")

// ConnSender adapts a single-peer Conn to Sender (the address is ignored).
type ConnSender struct{ C Conn }

// SendPacket implements Sender.
func (s ConnSender) SendPacket(_ Addr, data []byte) error { return s.C.Send(data) }

// ---------------------------------------------------------------------------
// In-memory pair

type memConn struct {
	in     chan []byte
	peer   *memConn
	once   *sync.Once
	closed chan struct{}
}

// MemPipe returns two connected in-memory datagram endpoints. Datagrams are
// copied; each direction buffers up to depth datagrams and drops the rest (like
// a full socket buffer).
func MemPipe(depth int) (Conn, Conn) {
	if depth <= 0 {
		depth = 256
	}
	closed := make(chan struct{})
	once := &sync.Once{}
	a := &memConn{in: make(chan []byte, depth), once: once, closed: closed}
	b := &memConn{in: make(chan []byte, depth), once: once, closed: closed}
	a.peer, b.peer = b, a
	return a, b
}

func (c *memConn) Send(data []byte) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	cp := append([]byte(nil), data...)
	select {
	case c.peer.in <- cp:
	default: // dropped
	}
	return nil
}

func (c *memConn) Recv(ctx context.Context) ([]byte, error) {
	// a closed pipe reports ErrClosed even with datagrams still queued
	select {
	case <-c.closed:
		return nil, ErrClosed
	default:
	}
	select {
	case d := <-c.in:
		return d, nil
	case <-c.closed:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *memConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
