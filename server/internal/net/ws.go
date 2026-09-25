package net

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/coder/websocket"
)

// wsConn is a WebSocket carrying one datagram per binary message (ADR-0001).
//
// coder/websocket closes the connection when a Read's context is cancelled,
// so a single background goroutine reads with the connection's own context
// and Recv waits on the channel it fills; Recv timeouts leave the socket open.
type wsConn struct {
	c    *websocket.Conn
	ctx  context.Context
	stop context.CancelFunc
	wmu  sync.Mutex
	in   chan []byte
	err  error // set before in is closed
}

func newWSConn(c *websocket.Conn) *wsConn {
	c.SetReadLimit(64 * 1024)
	ctx, stop := context.WithCancel(context.Background())
	w := &wsConn{c: c, ctx: ctx, stop: stop, in: make(chan []byte, 64)}
	go w.readLoop()
	return w
}

func (w *wsConn) readLoop() {
	defer close(w.in)
	for {
		typ, data, err := w.c.Read(w.ctx)
		if err != nil {
			var ce websocket.CloseError
			if errors.As(err, &ce) || w.ctx.Err() != nil {
				err = ErrClosed
			}
			w.err = err
			return
		}
		if typ != websocket.MessageBinary {
			continue // text messages are ignored
		}
		select {
		case w.in <- data:
		case <-w.ctx.Done():
			w.err = ErrClosed
			return
		}
	}
}

// AcceptWS upgrades an HTTP request to a datagram WebSocket.
func AcceptWS(w http.ResponseWriter, r *http.Request, opts *websocket.AcceptOptions) (Conn, error) {
	c, err := websocket.Accept(w, r, opts)
	if err != nil {
		return nil, err
	}
	return newWSConn(c), nil
}

// DialWS connects to a datagram WebSocket endpoint.
func DialWS(ctx context.Context, url string) (Conn, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	return newWSConn(c), nil
}

func (w *wsConn) Send(data []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	return w.c.Write(w.ctx, websocket.MessageBinary, data)
}

func (w *wsConn) Recv(ctx context.Context) ([]byte, error) {
	select {
	case d, ok := <-w.in:
		if !ok {
			return nil, w.err
		}
		return d, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (w *wsConn) Close() error {
	w.stop()
	return w.c.Close(websocket.StatusNormalClosure, "")
}
