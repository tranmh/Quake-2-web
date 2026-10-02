package spectate

import (
	"errors"
	"time"

	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
)

// viewerState is the relay's view of a viewer connection (C client_state_t
// reduced to what a watch-only server needs).
type viewerState int

const (
	vsNew       viewerState = iota // transport only: no netchan yet
	vsConnected                    // netchan set up, in the handshake (cs_connected)
	vsSpawned                      // "begin" accepted: frames are sent (cs_spawned)
)

// outQueue is a viewer's send queue: push never blocks.
type outQueue interface {
	push(d []byte) bool         // queue one datagram; false when full
	backlog() (n, capacity int) // queued datagrams and the capacity
}

// chanQueue is the Hub's send queue, drained by the connection's writer.
type chanQueue chan []byte

func (q chanQueue) push(d []byte) bool {
	select {
	case q <- d:
		return true
	default:
		return false
	}
}

func (q chanQueue) backlog() (int, int) { return len(q), cap(q) }

// viewer is one watching connection. Everything but out, q and done is
// owned by the relay (the hub goroutine).
type viewer struct {
	id   int
	addr qnet.Addr
	out  outQueue
	q    chanQueue     // Hub only: the queue behind out
	done chan struct{} // Hub only: closed when the relay removed the viewer

	removed bool
	state   viewerState
	ch      qnet.Netchan

	challenge int
	// gen and sc identify the level whose serverdata the viewer was sent
	// (gen 0: none since the last connect or level change).
	gen        int
	sc         int32
	pendingNew bool // "new" arrived while the bot's level was not ready

	// known holds the configstrings the viewer was sent (or will receive
	// on its reliable stream) since its last serverdata.
	known [q2const.MAX_CONFIGSTRINGS]string
	// frames holds the serverframe numbers sent to the viewer, by
	// UPDATE_BACKUP slot (-1: none), mirroring the client's frame ring.
	frames [q2const.UPDATE_BACKUP]int32
	live   bool // a frame was sent since "begin"

	resync     ResyncReason // a keyframe is due ("" none)
	waitDrain  bool         // a datagram was dropped: wait for the queue to drain
	drainSince time.Time    // since when

	lastValid        time.Time   // last valid datagram (timeout)
	lastClientResync time.Time   // last resync the viewer asked for
	drops            []time.Time // send queue drops within the drop window
	sendFailed       bool        // set by the sender during one transmit
}

func (v *viewer) clearFrames() {
	for i := range v.frames {
		v.frames[i] = -1
	}
}

func (v *viewer) hasFrame(n int32) bool {
	return n > 0 && v.frames[n&q2const.UPDATE_MASK] == n
}

// resetLevel forgets everything about the viewer's level (after a new
// netchan or a level change).
func (v *viewer) resetLevel() {
	v.gen, v.sc = 0, 0
	v.pendingNew = false
	v.known = [q2const.MAX_CONFIGSTRINGS]string{}
	v.clearFrames()
	v.live = false
	v.resync = ""
}

var errQueueFull = errors.New("spectate: viewer send queue full")

// viewerSender is the qnet.Sender of a viewer's netchan and connectionless
// replies: it queues without blocking and records a drop.
type viewerSender struct {
	r *relay
	v *viewer
}

func (s viewerSender) SendPacket(_ qnet.Addr, data []byte) error {
	if s.v.out.push(data) {
		s.r.metrics.DatagramOut()
		return nil
	}
	s.v.sendFailed = true
	s.r.metrics.Drop(DropQueue, 1)
	return errQueueFull
}

// writeLoop sends a viewer's queued datagrams until done is closed, then
// flushes what is left (the final svc_disconnect) and returns. A send error
// ends it early.
func writeLoop(conn qnet.Conn, q chanQueue, done <-chan struct{}) {
	for {
		select {
		case d := <-q:
			if conn.Send(d) != nil {
				return
			}
		case <-done:
			for {
				select {
				case d := <-q:
					if conn.Send(d) != nil {
						return
					}
				default:
					return
				}
			}
		}
	}
}
