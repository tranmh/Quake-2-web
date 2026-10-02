package spectate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	qnet "quake2web/server/internal/net"
)

// Hub defaults (HubConfig zero values).
const (
	DefaultMaxViewers   = 8
	DefaultQueueDepth   = 64  // datagrams: several seconds of a 10 Hz stream
	DefaultInboxDepth   = 256 // viewer datagrams waiting for the hub goroutine
	DefaultTimeout      = 30 * time.Second
	DefaultPacketRate   = 500 // datagrams per second and connection, like host's connRate
	DefaultDropLimit    = 5   // send queue drops per DropWindow before a viewer is disconnected
	DefaultDropWindow   = time.Minute
	DefaultStallTimeout = 10 * time.Second
	DefaultTickInterval = 100 * time.Millisecond

	keepalive    = time.Second // C: SV_SendClientMessages sends at least one datagram a second
	clientResync = time.Second // viewer-requested keyframes at most this often
	drainTimeout = 2 * time.Second
)

// Errors of ServeConn.
var (
	ErrFull   = errors.New("spectate: too many viewers")
	ErrClosed = errors.New("spectate: hub closed")
)

// HubConfig configures a Hub; zero values take the defaults.
type HubConfig struct {
	MaxViewers int          // viewers of this hub (DefaultMaxViewers; < 0: unlimited)
	Global     *ViewerLimit // optional cap shared by several hubs

	QueueDepth int // per-viewer send queue, in datagrams
	FeedDepth  int // bot messages buffered for the hub (DefaultFeedDepth)
	InboxDepth int // viewer datagrams buffered for the hub goroutine

	Timeout    time.Duration // drop a viewer silent this long
	PacketRate int           // datagrams per second a viewer may send (burst: one second's worth)
	DropLimit  int           // more send queue drops than this within DropWindow disconnect a viewer
	DropWindow time.Duration
	// StallTimeout disconnects a viewer whose send queue has not drained
	// this long after a drop (its link has stopped taking data).
	StallTimeout time.Duration

	TickInterval time.Duration // timer resolution (keepalives, timeouts)

	// Metrics, when set, receives the hub's events too (Stats always
	// works).
	Metrics Metrics
	// Logf receives diagnostics (may be nil).
	Logf func(format string, args ...any)
	// Now is the clock (default time.Now); tests may slow it down or speed
	// it up.
	Now func() time.Time
}

func (c HubConfig) withDefaults() HubConfig {
	if c.MaxViewers == 0 {
		c.MaxViewers = DefaultMaxViewers
	}
	if c.QueueDepth <= 0 {
		c.QueueDepth = DefaultQueueDepth
	}
	if c.FeedDepth <= 0 {
		c.FeedDepth = DefaultFeedDepth
	}
	if c.InboxDepth <= 0 {
		c.InboxDepth = DefaultInboxDepth
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.PacketRate <= 0 {
		c.PacketRate = DefaultPacketRate
	}
	if c.DropLimit <= 0 {
		c.DropLimit = DefaultDropLimit
	}
	if c.DropWindow <= 0 {
		c.DropWindow = DefaultDropWindow
	}
	if c.StallTimeout <= 0 {
		c.StallTimeout = DefaultStallTimeout
	}
	if c.TickInterval <= 0 {
		c.TickInterval = DefaultTickInterval
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Hub relays one bot's stream to its viewers. It owns a goroutine that runs
// the relay: the bot's messages (from its Stream), the viewers' datagrams
// and the timers are handled there one at a time, so no viewer can hold up
// the bot or another viewer. Each viewer connection has a reader (ServeConn)
// and a writer goroutine draining its send queue.
type Hub struct {
	cfg      HubConfig
	stream   *Stream
	sub      *subscription
	counters Counters
	metrics  Metrics

	inbox  chan inbound
	joins  chan *viewer
	leaves chan *viewer

	closing   chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	slots  atomic.Int64
	nextID atomic.Int64
}

type inbound struct {
	v    *viewer
	data []byte
}

// NewHub starts a hub relaying s. Close it when the bot's run ends.
func NewHub(s *Stream, cfg HubConfig) *Hub {
	cfg = cfg.withDefaults()
	h := &Hub{
		cfg:     cfg,
		stream:  s,
		inbox:   make(chan inbound, cfg.InboxDepth),
		joins:   make(chan *viewer),
		leaves:  make(chan *viewer),
		closing: make(chan struct{}),
		done:    make(chan struct{}),
	}
	h.metrics = &h.counters
	if cfg.Metrics != nil {
		h.metrics = multiMetrics{&h.counters, cfg.Metrics}
	}
	h.sub = s.subscribe(cfg.FeedDepth)
	r := newRelay(relayConfig{
		timeout:      cfg.Timeout,
		keepalive:    keepalive,
		dropLimit:    cfg.DropLimit,
		dropWindow:   cfg.DropWindow,
		stallTimeout: cfg.StallTimeout,
		clientResync: clientResync,
		logf:         cfg.Logf,
	}, h.metrics, cfg.Now())
	r.onRemove = func(v *viewer) { close(v.done) }
	go h.run(r)
	return h
}

// Stats returns the hub's counters.
func (h *Hub) Stats() Stats { return h.counters.Snapshot() }

// Viewers returns the number of viewer connections being served.
func (h *Hub) Viewers() int { return int(h.slots.Load()) }

// Done is closed once the hub has stopped.
func (h *Hub) Done() <-chan struct{} { return h.done }

// Close disconnects every viewer (svc_disconnect, three times) and stops
// the hub. It is idempotent and returns once the hub goroutine has ended.
func (h *Hub) Close() error {
	h.closeOnce.Do(func() { close(h.closing) })
	<-h.done
	return nil
}

func (h *Hub) run(r *relay) {
	defer close(h.done)
	tick := time.NewTicker(h.cfg.TickInterval)
	defer tick.Stop()
	for {
		select {
		case d := <-h.sub.ch:
			r.now = h.cfg.Now()
			r.bot(d)
		case p := <-h.inbox:
			r.now = h.cfg.Now()
			r.packet(p.v, p.data)
		case v := <-h.joins:
			r.now = h.cfg.Now()
			r.join(v)
		case v := <-h.leaves:
			r.remove(v, LeaveConn)
		case <-tick.C:
			r.now = h.cfg.Now()
			r.tick()
		case <-h.closing:
			r.now = h.cfg.Now()
			h.stream.unsubscribe(h.sub)
			r.closeAll()
			return
		}
	}
}

// reserve takes a viewer slot of the hub and the global limit.
func (h *Hub) reserve() bool {
	select {
	case <-h.closing:
		return false
	default:
	}
	if n := h.slots.Add(1); h.cfg.MaxViewers > 0 && n > int64(h.cfg.MaxViewers) {
		h.slots.Add(-1)
		return false
	}
	if !h.cfg.Global.acquire() {
		h.slots.Add(-1)
		return false
	}
	return true
}

func (h *Hub) release() {
	h.cfg.Global.release()
	h.slots.Add(-1)
}

// ServeConn serves one viewer on a datagram connection until it leaves,
// the hub drops it or closes, the connection fails or ctx ends. base names
// the remote host (logs and the netchan address). It returns ErrFull when
// the hub or the global limit has no room and ErrClosed when the hub is
// closed; nil when the viewer disconnected or the hub removed it. The
// caller closes conn afterwards (the final svc_disconnect is flushed
// first).
func (h *Hub) ServeConn(ctx context.Context, conn qnet.Conn, base string) error {
	if !h.reserve() {
		return ErrFull
	}
	defer h.release()
	return h.serve(ctx, conn, base)
}

func (h *Hub) serve(ctx context.Context, conn qnet.Conn, base string) error {
	id := int(h.nextID.Add(1))
	q := make(chanQueue, h.cfg.QueueDepth)
	v := &viewer{id: id, addr: qnet.Addr{Base: base, Port: id}, out: q, q: q, done: make(chan struct{})}
	select {
	case h.joins <- v:
	case <-h.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}

	wdone := make(chan struct{})
	go func() {
		defer close(wdone)
		writeLoop(conn, q, v.done)
	}()

	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-v.done:
			cancel()
		case <-rctx.Done():
		}
	}()
	err := h.readLoop(rctx, conn, v)

	byHub := false
	select {
	case <-v.done:
		byHub = true
	default:
		select {
		case h.leaves <- v:
		case <-v.done: // removed meanwhile
		case <-h.done:
		}
		select {
		case <-v.done:
		case <-h.done:
		}
	}
	t := time.NewTimer(drainTimeout)
	select {
	case <-wdone:
	case <-t.C:
	}
	t.Stop()
	if byHub || errors.Is(err, qnet.ErrClosed) {
		return nil
	}
	return err
}

// readLoop hands the viewer's datagrams to the hub goroutine, at most
// PacketRate a second (token bucket); excess datagrams and those that find
// the inbox full are dropped, like an overflowing socket buffer.
func (h *Hub) readLoop(ctx context.Context, conn qnet.Conn, v *viewer) error {
	rate := float64(h.cfg.PacketRate)
	tokens, last := rate, h.cfg.Now()
	for {
		d, err := conn.Recv(ctx)
		if err != nil {
			return err
		}
		now := h.cfg.Now()
		tokens += now.Sub(last).Seconds() * rate
		last = now
		if tokens > rate {
			tokens = rate
		}
		if tokens < 1 {
			h.metrics.Drop(DropRate, 1)
			continue
		}
		tokens--
		select {
		case h.inbox <- inbound{v: v, data: d}:
		default:
			h.metrics.Drop(DropInbox, 1)
		}
	}
}
