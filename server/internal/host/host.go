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
	"runtime/debug"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"quake2web/server/internal/auth"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
)

// ErrNoInstance is returned for an unknown instance id.
var ErrNoInstance = errors.New("host: no such game instance")

// Per-connection datagram rate limit (token bucket): a Quake 2 client sends
// one packet per client frame (cl_maxfps), so these leave ample headroom
// while stopping a single connection from flooding the shared inbox.
const (
	connRate  = 500 // datagrams per second
	connBurst = 500
)

// ErrStopped is returned when the instance has ended.
var ErrStopped = errors.New("host: instance stopped")

// Host is the instance manager.
type Host struct {
	mu        sync.Mutex
	instances map[string]*Instance
	tickets   map[string]ticket

	// RequireTickets makes the WebSocket endpoint demand a valid join ticket
	// (?ticket=...). When false a client without a ticket may connect
	// anonymously (development); a ticket that is given must still be valid.
	RequireTickets bool
	// Tickets, when set, redeems the account join tickets issued by the API
	// (auth.TicketService); the redeemed account becomes the connection's
	// Player. Without it the host's own IssueTicket tickets are used.
	Tickets auth.TicketService
	// TickObserver, when set, receives the duration of every server frame.
	TickObserver func(time.Duration)
	// TicketTTL is how long an issued ticket stays valid (default 30 s).
	TicketTTL time.Duration
	// OriginPatterns are the extra Origin host patterns accepted by the
	// WebSocket endpoint (coder/websocket AcceptOptions.OriginPatterns); the
	// request's own host is always accepted.
	OriginPatterns []string
	// Logf receives host diagnostics (may be nil).
	Logf func(format string, args ...any)
	// RemoteIP, when set, returns the client IP of a WebSocket request
	// (e.g. from X-Forwarded-For behind a trusted proxy); default: the
	// host part of RemoteAddr.
	RemoteIP func(r *http.Request) string

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

// Player is the account behind a connection (from its join ticket).
type Player struct {
	UserID int64
	Name   string
}

// InstanceConfig describes a new game instance.
type InstanceConfig struct {
	ID string
	// Server is the configuration of the instance's server. Its Userinfo
	// and ClientCommand hooks are wrapped by the host.
	Server sv.Config
	// Commands are console commands executed at start (e.g. "map demo1").
	Commands []string
	// InboxSize bounds queued datagrams (default 1024); more are dropped.
	InboxSize int
	// ClientCommand, when set, is offered each client string command
	// (s.Cmd holds it) together with the connection's player (p is nil for
	// anonymous connections); returning true swallows the command. It runs
	// in the instance goroutine.
	ClientCommand func(inst *Instance, s *sv.Server, cl *sv.Client, p *Player) bool
	// BeforeDrop, when set, runs in the instance goroutine when the
	// transport of an in-game client closed, just before the host drops it.
	BeforeDrop func(inst *Instance, s *sv.Server, cl *sv.Client, p *Player)
	// ConnClosed, when set, runs (outside the instance goroutine) after a
	// connection has ended and its client was dropped.
	ConnClosed func(inst *Instance, p *Player)
}

// Stats is a snapshot of an instance, refreshed every server frame.
type Stats struct {
	Map        string
	Players    int
	MaxClients int
	// LastActive is the last time a client was connected (zero: never).
	LastActive time.Time
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
	cfg     InstanceConfig

	// pending are closures queued with Defer (instance goroutine only).
	pending []func(*sv.Server)

	connMu sync.Mutex
	conns  map[qnet.Addr]*Player

	stats atomic.Pointer[Stats]

	errMu sync.Mutex
	err   error
}

// Stats returns the latest snapshot of the instance.
func (i *Instance) Stats() Stats {
	if st := i.stats.Load(); st != nil {
		return *st
	}
	return Stats{}
}

// Player returns the player of the connection with the given address.
func (i *Instance) Player(addr qnet.Addr) *Player {
	i.connMu.Lock()
	defer i.connMu.Unlock()
	return i.conns[addr]
}

// Defer queues fn to run in the instance goroutine after the current event
// (packet, control closure or frame) has been handled. It must only be
// called from inside the instance goroutine (hooks, Do closures).
func (i *Instance) Defer(fn func(*sv.Server)) { i.pending = append(i.pending, fn) }

// SanitizeName makes an account display name usable as a Quake 2 player
// name: no userinfo/command separators or control characters, at most 31
// bytes.
func SanitizeName(n string) string {
	b := make([]byte, 0, len(n))
	for _, c := range []byte(n) {
		if c < 32 || c >= 127 || c == '\\' || c == '"' || c == ';' {
			continue
		}
		b = append(b, c)
	}
	if len(b) > 31 {
		b = b[:31]
	}
	if len(b) == 0 {
		return "player"
	}
	return string(b)
}

func (i *Instance) installHooks(cfg *sv.Config) {
	prevUI := cfg.Userinfo
	cfg.Userinfo = func(addr qnet.Addr, ui string) string {
		if p := i.Player(addr); p != nil {
			if nu, warn := shared.Info_SetValueForKey(ui, "name", SanitizeName(p.Name)); warn == "" {
				ui = nu
			}
		}
		if prevUI != nil {
			ui = prevUI(addr, ui)
		}
		return ui
	}
	prevCmd := cfg.ClientCommand
	cfg.ClientCommand = func(cl *sv.Client) bool {
		if i.cfg.ClientCommand != nil && i.cfg.ClientCommand(i, i.srv, cl, i.Player(cl.Netchan.RemoteAddress)) {
			return true
		}
		return prevCmd != nil && prevCmd(cl)
	}
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
		cfg:     cfg,
		conns:   map[qnet.Addr]*Player{},
	}
	h.instances[cfg.ID] = inst
	h.mu.Unlock()

	scfg := cfg.Server
	inst.installHooks(&scfg)
	inst.srv = sv.New(scfg)
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
		quit, err := i.step(s, timer, elapsed)
		if quit {
			if err != nil {
				i.errMu.Lock()
				i.err = err
				i.errMu.Unlock()
			}
			return
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

// step handles one event of the instance loop. A panic that is not a
// Com_Error (those are recovered by sv itself) ends this instance only:
// without the recover it would unwind the instance goroutine and take the
// whole process (every game and the HTTP API) down.
func (i *Instance) step(s *sv.Server, timer *time.Timer, elapsed func() int) (quit bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			i.host.logf("instance %s: panic: %v\n%s", i.id, r, debug.Stack())
			func() {
				defer func() { _ = recover() }()
				s.Shutdown("Server crashed.\n")
			}()
			quit, err = true, fmt.Errorf("host: instance %s panicked: %v", i.id, r)
		}
	}()
	select {
	case <-i.stop:
		s.Shutdown("Server was killed.\n")
		return true, nil
	case p := <-i.inbox:
		err = s.HandlePacket(elapsed(), p)
	case fn := <-i.control:
		fn(s)
		i.updateStats(s)
	case <-timer.C:
		var sleep int
		t0 := time.Now()
		sleep, err = s.Frame(elapsed())
		if obs := i.host.TickObserver; obs != nil && s.SVS.Initialized {
			obs(time.Since(t0))
		}
		if sleep < 1 {
			sleep = 1
		}
		timer.Reset(time.Duration(sleep) * time.Millisecond)
		i.updateStats(s)
	}
	for len(i.pending) > 0 {
		fn := i.pending[0]
		i.pending = i.pending[1:]
		fn(s)
	}
	return false, err
}

func (i *Instance) updateStats(s *sv.Server) {
	old := i.stats.Load()
	st := Stats{Map: s.SV.Name, MaxClients: len(s.SVS.Clients)}
	if old != nil {
		st.LastActive = old.LastActive
	}
	for k := range s.SVS.Clients {
		if s.SVS.Clients[k].InUse() {
			st.Players++
		}
	}
	if st.Players > 0 {
		st.LastActive = time.Now()
	}
	if old == nil || old.Map != st.Map || old.Players != st.Players || old.MaxClients != st.MaxClients ||
		(st.Players > 0 && st.LastActive.Sub(old.LastActive) > time.Second) {
		i.stats.Store(&st)
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
// (NET_CompareBaseAdr); each connection gets a unique base and port number.
func (i *Instance) ServeConn(ctx context.Context, conn qnet.Conn, base string) error {
	return i.ServeConnAs(ctx, conn, base, nil)
}

// ServeConnAs is ServeConn for an authenticated player (nil: anonymous).
// The player's display name overrides the userinfo name. When the
// connection ends its client, if still connected, is dropped at once
// (BeforeDrop runs first) instead of waiting for the netchan timeout.
func (i *Instance) ServeConnAs(ctx context.Context, conn qnet.Conn, base string, p *Player) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	n := int(i.host.nextConn.Add(1))
	if !qnet.IsLocalAddress(qnet.Addr{Base: base}) {
		// Every connection is its own "host" for NET_CompareBaseAdr: the
		// server routes sequenced packets by base address + qport and
		// reuses a slot on "connect" from the same base, so connections
		// sharing a remote IP (all browsers behind the reverse proxy or one
		// NAT) could otherwise inject commands into, or take over, each
		// other's clients by guessing a 16-bit qport. The IP stays in
		// front ("1.2.3.4#17") so the game's SV_FilterPacket still parses it.
		base = base + "#" + strconv.Itoa(n)
	}
	addr := qnet.Addr{Base: base, Port: n}
	i.connMu.Lock()
	i.conns[addr] = p
	i.connMu.Unlock()
	defer i.connClosed(addr, p)
	via := newAsyncSender(conn, ctx.Done())
	go func() {
		select {
		case <-i.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	tokens, last := float64(connBurst), time.Now()
	for {
		d, err := conn.Recv(ctx)
		if err != nil {
			return err
		}
		now := time.Now()
		tokens += now.Sub(last).Seconds() * connRate
		last = now
		if tokens > connBurst {
			tokens = connBurst
		}
		if tokens < 1 {
			continue // over the rate: dropped, like an overflowing socket buffer
		}
		tokens--
		i.Deliver(qnet.Packet{From: addr, Via: via, Data: d})
	}
}

func (i *Instance) connClosed(addr qnet.Addr, p *Player) {
	_ = i.Do(func(s *sv.Server) {
		for k := range s.SVS.Clients {
			cl := &s.SVS.Clients[k]
			if !cl.InUse() || cl.Netchan.RemoteAddress != addr {
				continue
			}
			if i.cfg.BeforeDrop != nil {
				i.cfg.BeforeDrop(i, s, cl, p)
			}
			s.DropClient(cl)
		}
		i.updateStats(s)
	})
	i.connMu.Lock()
	delete(i.conns, addr)
	i.connMu.Unlock()
	if i.cfg.ConnClosed != nil {
		i.cfg.ConnClosed(i, p)
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
	var player *Player
	switch tk := r.URL.Query().Get("ticket"); {
	case tk != "" && h.Tickets != nil:
		t, err := h.Tickets.Redeem(tk, id)
		if err != nil {
			http.Error(w, "invalid ticket", http.StatusForbidden)
			return
		}
		player = &Player{UserID: t.UserID, Name: t.DisplayName}
	case tk != "":
		if !h.redeemTicket(tk, id) {
			http.Error(w, "invalid ticket", http.StatusForbidden)
			return
		}
	case h.RequireTickets:
		http.Error(w, "ticket required", http.StatusForbidden)
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
	if h.RemoteIP != nil {
		if ip := h.RemoteIP(r); ip != "" {
			base = ip
		}
	}
	_ = inst.ServeConnAs(r.Context(), conn, base, player)
	_ = conn.Close()
}
