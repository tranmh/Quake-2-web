package net

import (
	"context"
	"errors"
	stdnet "net"
	"strconv"
	"syscall"
	"time"
)

// UDPListener is a server socket (NET_Config with a UDP port) delivering
// datagrams from many peers. It implements Sender.
type UDPListener struct {
	pc *stdnet.UDPConn
}

// ListenUDP opens a UDP server socket.
func ListenUDP(addr string) (*UDPListener, error) {
	ua, err := stdnet.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	pc, err := stdnet.ListenUDP("udp", ua)
	if err != nil {
		return nil, err
	}
	return &UDPListener{pc: pc}, nil
}

// LocalAddr returns the bound address.
func (l *UDPListener) LocalAddr() *stdnet.UDPAddr { return l.pc.LocalAddr().(*stdnet.UDPAddr) }

// Serve reads datagrams until the socket is closed, calling fn for each.
func (l *UDPListener) Serve(fn func(Packet)) error {
	buf := make([]byte, 65536)
	for {
		n, from, err := l.pc.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		fn(Packet{From: udpAddr(from), Via: l, Data: append([]byte(nil), buf[:n]...)})
	}
}

func udpAddr(a *stdnet.UDPAddr) Addr {
	return Addr{Base: a.IP.String(), Port: a.Port}
}

// SendPacket implements Sender.
func (l *UDPListener) SendPacket(to Addr, data []byte) error {
	ua := &stdnet.UDPAddr{IP: stdnet.ParseIP(to.Base), Port: to.Port}
	_, err := l.pc.WriteToUDP(data, ua)
	return err
}

// Close closes the socket.
func (l *UDPListener) Close() error { return l.pc.Close() }

type udpConn struct {
	c *stdnet.UDPConn
}

// DialUDP opens a client UDP socket to a single peer ("host:port").
func DialUDP(addr string) (Conn, error) {
	ua, err := stdnet.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	c, err := stdnet.DialUDP("udp", nil, ua)
	if err != nil {
		return nil, err
	}
	return &udpConn{c: c}, nil
}

func (u *udpConn) Send(data []byte) error {
	_, err := u.c.Write(data)
	return err
}

func (u *udpConn) Recv(ctx context.Context) ([]byte, error) {
	buf := make([]byte, 65536)
	for {
		if dl, ok := ctx.Deadline(); ok {
			_ = u.c.SetReadDeadline(dl)
		} else {
			_ = u.c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		}
		n, err := u.c.Read(buf)
		if err != nil {
			if ne, ok := err.(stdnet.Error); ok && ne.Timeout() {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				continue
			}
			if errors.Is(err, syscall.ECONNREFUSED) {
				continue // ICMP port unreachable from an earlier send
			}
			return nil, err
		}
		return append([]byte(nil), buf[:n]...), nil
	}
}

func (u *udpConn) Close() error { return u.c.Close() }

// HostPort formats a host and port.
func HostPort(host string, port int) string {
	return stdnet.JoinHostPort(host, strconv.Itoa(port))
}
