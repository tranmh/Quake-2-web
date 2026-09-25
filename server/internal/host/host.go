// Package host manages game instances: one goroutine owns one *sv.Server and
// serializes everything that touches it (received datagrams, control
// closures, the frame timer). It also exposes the transports: the WebSocket
// endpoint /ws/v1/games/{id} (one binary message == one datagram, ADR-0001),
// raw UDP for the original tools, and in-memory connections for tests.
package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	stdnet "net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/sv"
)

// ErrNoInstance is returned for an unknown instance id.
var ErrNoInstance = errors.New("host: no such game instance")

// ErrStopped is returned when the instance has ended.
var ErrStopped = errors.New("host: instance stopped")

// Host is the instance manager.
type Host struct {
	mu        sync.Mutex
	instances map[string]*Instance
	tickets   map[string]ticket

	// RequireTickets makes the WebSocket endpoint demand a valid join ticket
	// (?ticket=...). When false any client may connect (development).
	RequireTickets bool
	// TicketTTL is how long an issued ticket stays valid (default 30 s).
	TicketTTL time.Duration
	// OriginPatterns are the extra Origin host patterns accepted by the
	// WebSocket endpoint (coder/websocket AcceptOptions.OriginPatterns); the
	// request's own host is always accepted.
	OriginPatterns []string
	// Logf receives host diagnostics (may be nil).
	Logf func(format string, args ...any)

	nextConn atomic.Int64
}

type ticket struct {
	instance string
	expires  time.Time
}

// New returns an empty host.
func New() *Host {
	return &Host{instances: map[string]*Instance{}, tickets: map[string]ticket{}}
}

func (h *Host) logf(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}

// InstanceConfig describes a new game instance.
type InstanceConfig struct {
	ID string
	// Server is the configuration of the instance's server.
	Server sv.Config
	// Commands are console commands executed at start (e.g. "map demo1").
	Commands []string
	// InboxSize bounds queued datagrams (default 1024); more are dropped.
	InboxSize int
}

// Instance is one running game.
type Instance struct {
	id      string
	host    *Host
	srv     *sv.Server
	inbox   chan qnet.Packet
	control chan func(*sv.Server)
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once

	errMu sync.Mutex
	err   error
}

// ID returns the instance id.
func (i *Instance) ID() string { return i.id }

// Create starts a new instance and runs its start commands (synchronously,
// inside the instance goroutine) before returning.
func (h *Host) Create(cfg InstanceConfig) (*Instance, error) {
	if cfg.ID == "" {
		cfg.ID = randomID()
	}
	h.mu.Lock()
	if _, ok := h.instances[cfg.ID]; ok {
		h.mu.Unlock()
		return nil, fmt.Errorf("host: instance %q exists", cfg.ID)
	}
	n := cfg.InboxSize
	if n <= 0 {
		n = 1024
	}
	inst := &Instance{
		id:      cfg.ID,
		host:    h,
		inbox:   make(chan qnet.Packet, n),
		control: make(chan func(*sv.Server)),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	h.instances[cfg.ID] = inst
	h.mu.Unlock()

	inst.srv = sv.New(cfg.Server)
	go inst.run()

	for _, c := range cfg.Commands {
		var err error
		if derr := inst.Do(func(s *sv.Server) { err = s.ExecuteText(c + "\n") }); derr != nil {
			return nil, derr
		}
		if err != nil {
			inst.Stop()
			return nil, err
		}
	}
	if len(cfg.Commands) > 0 {
		ok := false
		if err := inst.Do(func(s *sv.Server) { ok = s.SVS.Initialized }); err != nil {
			return nil, err
		}
		if !ok {
			inst.Stop()
			return nil, fmt.Errorf("host: instance %q did not start a server", cfg.ID)
		}
	}
	return inst, nil
}

// Get returns an instance by id.
func (h *Host) Get(id string) (*Instance, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	inst := h.instances[id]
	if inst == nil {
		return nil, ErrNoInstance
	}
	return inst, nil
}

// Instances returns the ids of the running instances.
func (h *Host) Instances() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for id := range h.instances {
		out = append(out, id)
	}
	return out
}

// Shutdown stops every instance.
func (h *Host) Shutdown() {
	h.mu.Lock()
	var all []*Instance
	for _, i := range h.instances {
		all = append(all, i)
	}
	h.mu.Unlock()
	for _, i := range all {
		i.Stop()
	}
}

func (h *Host) remove(inst *Instance) {
	h.mu.Lock()
	if h.instances[inst.id] == inst {
		delete(h.instances, inst.id)
	}
	h.mu.Unlock()
}

// IssueTicket returns a one-time join ticket for the instance.
func (h *Host) IssueTicket(instanceID string) (string, error) {
	if _, err := h.Get(instanceID); err != nil {
		return "", err
	}
	ttl := h.TicketTTL
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	t := randomID()
	h.mu.Lock()
	now := time.Now()
	for k, v := range h.tickets {
		if now.After(v.expires) {
			delete(h.tickets, k)
		}
	}
	h.tickets[t] = ticket{instance: instanceID, expires: now.Add(ttl)}
	h.mu.Unlock()
	return t, nil
}

// redeemTicket consumes a ticket for the instance.
func (h *Host) redeemTicket(t, instanceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.tickets[t]
	if !ok {
		return false
	}
	delete(h.tickets, t)
	return v.instance == instanceID && time.Now().Before(v.expires)
}

func randomID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Do runs fn inside the instance goroutine and waits for it.
func (i *Instance) Do(fn func(*sv.Server)) error {
	doneFn := make(chan struct{})
	select {
	case i.control <- func(s *sv.Server) { defer close(doneFn); fn(s) }:
	case <-i.done:
		return ErrStopped
	}
	select {
	case <-doneFn:
		return nil
	case <-i.done:
		return ErrStopped
	}
}

// Deliver queues a received datagram; it is dropped when the inbox is full
// (like a full socket buffer) or the instance has stopped.
func (i *Instance) Deliver(p qnet.Packet) {
	select {
	case <-i.done:
		return
	default:
	}
	select {
	case i.inbox <- p:
	default:
	}
}

// Done is closed when the instance goroutine has ended.
func (i *Instance) Done() <-chan struct{} { return i.done }

// Err returns why the instance ended (nil for a normal stop).
func (i *Instance) Err() error {
	i.errMu.Lock()
	defer i.errMu.Unlock()
	return i.err
}

// Stop shuts the server down (clients get a disconnect) and waits.
func (i *Instance) Stop() {
	i.once.Do(func() { close(i.stop) })
	<-i.done
}

// run is the instance loop: datagrams are executed as they arrive, control
// closures run in between, and the frame timer calls SV_Frame when the next
// 100 ms game frame is due (the sleep it returns is C's NET_Sleep).
func (i *Instance) run() {
	defer close(i.done)
	defer i.host.remove(i)

	s := i.srv
	last := time.Now()
	elapsed := func() int {
		now := time.Now()
		ms := int(now.Sub(last) / time.Millisecond)
		last = last.Add(time.Duration(ms) * time.Millisecond)
		return ms
	}
	timer := time.NewTimer(time.Millisecond)
	defer timer.Stop()
	wasInit := false

	for {
		var err error
		select {
		case <-i.stop:
			s.Shutdown("Server was killed.\n")
			return
		case p := <-i.inbox:
			err = s.HandlePacket(elapsed(), p)
		case fn := <-i.control:
			fn(s)
		case <-timer.C:
			var sleep int
			sleep, err = s.Frame(elapsed())
			if sleep < 1 {
				sleep = 1
			}
			timer.Reset(time.Duration(sleep) * time.Millisecond)
		}
		if err != nil {
			i.host.logf("instance %s: %v\n", i.id, err)
		}
		if s.SVS.Initialized {
			wasInit = true
		}
		if s.Killed() || (wasInit && !s.SVS.Initialized) {
			i.errMu.Lock()
			i.err = err
			i.errMu.Unlock()
			return
		}
	}
}

// asyncSender decouples the instance goroutine from a slow connection: sends
// are queued and dropped when the queue is full (as UDP would).
type asyncSender struct {
	q chan []byte
}

func newAsyncSender(c qnet.Conn, done <-chan struct{}) *asyncSender {
	a := &asyncSender{q: make(chan []byte, 256)}
	go func() {
		for {
			select {
			case d := <-a.q:
				if err := c.Send(d); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()
	return a
}

func (a *asyncSender) SendPacket(_ qnet.Addr, data []byte) error {
	select {
	case a.q <- data:
	default:
	}
	return nil
}

// ServeConn relays datagrams from a single-peer connection into the instance
// until the connection fails or ctx ends. base identifies the remote host
// (NET_CompareBaseAdr); each connection gets a unique port number.
func (i *Instance) ServeConn(ctx context.Context, conn qnet.Conn, base string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	addr := qnet.Addr{Base: base, Port: int(i.host.nextConn.Add(1))}
	via := newAsyncSender(conn, ctx.Done())
	go func() {
		select {
		case <-i.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		d, err := conn.Recv(ctx)
		if err != nil {
			return err
		}
		i.Deliver(qnet.Packet{From: addr, Via: via, Data: d})
	}
}

// ConnectMem returns the client end of an in-memory connection to the
// instance (for tests and bots).
func (i *Instance) ConnectMem(base string) qnet.Conn {
	cli, srvEnd := qnet.MemPipe(1024)
	go func() {
		_ = i.ServeConn(context.Background(), srvEnd, base)
		srvEnd.Close()
	}()
	return cli
}

// ServeUDP delivers every datagram of a UDP socket to the instance with the
// given id (looked up per packet, so the instance may be replaced).
func (h *Host) ServeUDP(l *qnet.UDPListener, instanceID string) error {
	return l.Serve(func(p qnet.Packet) {
		inst, err := h.Get(instanceID)
		if err != nil {
			return
		}
		inst.Deliver(p)
	})
}

// Handler returns the HTTP handler serving GET /ws/v1/games/{id}.
func (h *Host) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/v1/games/{id}", h.serveWS)
	return mux
}

func (h *Host) serveWS(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := h.Get(id)
	if err != nil {
		http.Error(w, "no such game", http.StatusNotFound)
		return
	}
	if h.RequireTickets && !h.redeemTicket(r.URL.Query().Get("ticket"), id) {
		http.Error(w, "invalid ticket", http.StatusForbidden)
		return
	}
	conn, err := qnet.AcceptWS(w, r, &websocket.AcceptOptions{OriginPatterns: h.OriginPatterns})
	if err != nil {
		return
	}
	base := r.RemoteAddr
	if hst, _, err := stdnet.SplitHostPort(r.RemoteAddr); err == nil {
		base = hst
	}
	_ = inst.ServeConn(r.Context(), conn, base)
	_ = conn.Close()
}
