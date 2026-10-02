package spectate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/qcommon/shared"
)

// liveViewer is a fake client watching a hub from its own goroutine, like a
// browser: it handshakes, sends a move every 20 ms once active and records
// its valid frames.
type liveViewer struct {
	c    *fakeclient.Client
	conn qnet.Conn
	stop context.CancelFunc
	done chan struct{}

	mu      sync.Mutex
	frames  map[frameKey]frameRec
	order   []frameKey
	err     error
	first   map[int32]int32  // servercount -> first valid serverframe
	maps    map[int32]string // servercount -> level
	attract int32
	stuffed []string // every stufftext received
	seen    uint64
}

// watch connects a viewer to h over an in-memory pipe (hub side served by
// ServeConn, with conn wrapping it when set).
func watch(t testing.TB, h *Hub, qport int, wrap func(qnet.Conn) qnet.Conn) *liveViewer {
	t.Helper()
	cli, srv := qnet.MemPipe(4096)
	if wrap != nil {
		srv = wrap(srv)
	}
	lv := watchConn(t, cli, qport)
	go func() {
		_ = h.ServeConn(context.Background(), srv, "test")
		// The caller closes the connection, like the handler. A MemPipe
		// reports ErrClosed as soon as it is closed, even with datagrams
		// still queued (a WebSocket delivers them before its close frame),
		// so the final svc_disconnects are left to the viewer to read
		// first: close once it stopped, or after a while (a viewer the hub
		// dropped without a word, e.g. as too slow, notices then).
		t := time.NewTimer(viewerLinger)
		defer t.Stop()
		select {
		case <-lv.done:
		case <-t.C:
		}
		_ = srv.Close()
	}()
	return lv
}

// viewerLinger is how long watch keeps the hub side of a pipe open after
// ServeConn returned, for the viewer to read what is queued.
const viewerLinger = 5 * time.Second

func watchConn(t testing.TB, conn qnet.Conn, qport int) *liveViewer {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	lv := &liveViewer{conn: conn, stop: stop, done: make(chan struct{}), frames: map[frameKey]frameRec{},
		first: map[int32]int32{}, maps: map[int32]string{}}
	lv.c = fakeclient.New(conn, fakeclient.Options{
		Qport: qport,
		OnServerMessage: func(c *fakeclient.Client, _ []byte, spans []fakeclient.Span) {
			lv.mu.Lock()
			fresh, _ := fakeclient.NewSince(c.StuffTexts, c.Counts.StuffTexts, lv.seen)
			lv.stuffed = append(lv.stuffed, fresh...)
			lv.seen = c.Counts.StuffTexts
			lv.mu.Unlock()
			if !hasFrameSpan(spans) || !c.Frame.Valid {
				return
			}
			k, v := recordFrame(c)
			lv.mu.Lock()
			lv.frames[k] = v
			lv.order = append(lv.order, k)
			if _, ok := lv.first[k.sc]; !ok {
				lv.first[k.sc] = k.frame
			}
			lv.maps[k.sc] = c.MapName()
			lv.attract = c.ServerData.AttractLoop
			lv.mu.Unlock()
		},
	})
	go lv.run(ctx)
	t.Cleanup(func() {
		lv.stop()
		<-lv.done
		_ = conn.Close()
	})
	return lv
}

func (lv *liveViewer) run(ctx context.Context) {
	defer close(lv.done)
	lv.c.BeginConnect()
	var lastMove time.Time
	for {
		err := lv.c.Poll(ctx, 5*time.Millisecond)
		if err == nil && lv.c.State == fakeclient.CaActive && time.Since(lastMove) >= 20*time.Millisecond {
			lastMove = time.Now()
			lv.c.SendCmd(shared.UserCmd{Msec: 20})
		}
		if err == nil && lv.c.Disconnected {
			err = fakeclient.ErrDisconnected
		}
		if err != nil {
			if ctx.Err() == nil {
				lv.mu.Lock()
				lv.err = err
				lv.mu.Unlock()
			}
			return
		}
	}
}

func (lv *liveViewer) has(k frameKey) bool {
	lv.mu.Lock()
	defer lv.mu.Unlock()
	_, ok := lv.frames[k]
	return ok
}

func (lv *liveViewer) firstFrame(sc int32) (int32, bool) {
	lv.mu.Lock()
	defer lv.mu.Unlock()
	f, ok := lv.first[sc]
	return f, ok
}

// stuffTexts returns the stufftexts received so far.
func (lv *liveViewer) stuffTexts() []string {
	lv.mu.Lock()
	defer lv.mu.Unlock()
	return append([]string(nil), lv.stuffed...)
}

func (lv *liveViewer) error() error {
	lv.mu.Lock()
	defer lv.mu.Unlock()
	return lv.err
}

// match checks every frame the viewer holds against the bot's frames and
// returns how many it holds of level sc.
func (lv *liveViewer) match(t testing.TB, bot map[frameKey]frameRec, sc int32) int {
	t.Helper()
	lv.mu.Lock()
	defer lv.mu.Unlock()
	n := 0
	for k, v := range lv.frames {
		b, ok := bot[k]
		if !ok {
			t.Fatalf("viewer has frame %v the bot never had", k)
		}
		if !sameFrame(v, b) {
			t.Fatalf("frame %v differs:\nviewer %+v\n   bot %+v", k, v, b)
		}
		if k.sc == sc {
			n++
		}
	}
	return n
}

// eventually polls cond for up to d.
func eventually(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

// synthBot is a passive bot client fed by a synth, with a Stream.
type synthBot struct {
	t      testing.TB
	c      *fakeclient.Client
	stream *Stream
	srv    *synth
	frames map[frameKey]frameRec
}

func newSynthBot(t testing.TB) *synthBot {
	b := &synthBot{t: t, frames: map[frameKey]frameRec{}}
	b.stream = NewStream(SinkFunc(func(c *fakeclient.Client, _ []byte, spans []fakeclient.Span) {
		if hasFrameSpan(spans) && c.Frame.Valid {
			k, v := recordFrame(c)
			b.frames[k] = v
		}
	}))
	b.c = fakeclient.NewPassive(fakeclient.Options{OnServerMessage: b.stream.OnServerMessage})
	return b
}

func (b *synthBot) feed(p []byte) {
	b.t.Helper()
	if _, err := b.c.FeedPayload(p); err != nil {
		b.t.Fatalf("bot: %v", err)
	}
}

func (b *synthBot) start(s *synth) {
	b.srv = s
	for _, p := range s.handshake() {
		b.feed(p)
	}
	b.frame()
}

func (b *synthBot) frame() frameKey {
	s := b.srv
	delta := s.frame
	if _, ok := s.history[delta]; !ok {
		delta = -1
	}
	s.advance()
	b.feed(s.frameMsg(delta, nil))
	return frameKey{s.sc, s.frame}
}

func TestHubWatch(t *testing.T) {
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 6))
	h := NewHub(bot.stream, HubConfig{})
	defer h.Close()
	lv := watch(t, h, 7, nil)
	if !eventually(5*time.Second, func() bool { _, ok := lv.firstFrame(42); return ok }) {
		t.Fatalf("viewer never got a frame: %v", lv.error())
	}
	for i := 0; i < 100; i++ {
		k := bot.frame()
		if !eventually(2*time.Second, func() bool { return lv.has(k) }) {
			t.Fatalf("viewer lacks frame %v", k)
		}
	}
	if n := lv.match(t, bot.frames, 42); n != 101 {
		t.Fatalf("viewer holds %d frames", n)
	}
	if lv.mu.Lock(); lv.attract != 1 {
		t.Fatal("not an attract loop")
	}
	lv.mu.Unlock()
	h.Close()
	if !eventually(2*time.Second, func() bool { return errors.Is(lv.error(), fakeclient.ErrDisconnected) }) {
		t.Fatalf("viewer after Close: %v", lv.error())
	}
	st := h.Stats()
	if st.Viewers != 0 || st.Leaves[LeaveClosed] != 1 || st.Resyncs[ResyncJoin] != 1 || st.DatagramsIn == 0 || st.DatagramsOut < 100 {
		t.Fatalf("stats %+v", st)
	}
	if err := h.ServeConn(context.Background(), nil, "late"); !errors.Is(err, ErrClosed) {
		t.Fatalf("ServeConn after Close: %v", err)
	}
}

// stallConn blocks every Send while stalled (a viewer on a dead link).
type stallConn struct {
	qnet.Conn
	mu     sync.Mutex
	gate   chan struct{} // nil: not stalled; closed to release
	closed bool
}

func newStallConn(c qnet.Conn) *stallConn { return &stallConn{Conn: c} }

func (s *stallConn) stall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate == nil && !s.closed {
		s.gate = make(chan struct{})
	}
}

func (s *stallConn) unstall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil {
		close(s.gate)
		s.gate = nil
	}
}

func (s *stallConn) Send(d []byte) error {
	s.mu.Lock()
	g := s.gate
	s.mu.Unlock()
	if g != nil {
		<-g
	}
	return s.Conn.Send(d)
}

func (s *stallConn) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.unstall()
	return s.Conn.Close()
}

// TestPushNeverBlocksWithStalledViewer: one viewer stops reading for good.
// The bot's messages still pass through the stream at once, the other
// viewer keeps watching, and the stalled one is resynchronised, then
// disconnected as too slow.
func TestPushNeverBlocksWithStalledViewer(t *testing.T) {
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 6))
	h := NewHub(bot.stream, HubConfig{QueueDepth: 8, StallTimeout: 300 * time.Millisecond, TickInterval: 20 * time.Millisecond})
	defer h.Close()
	var stalled *stallConn
	slow := watch(t, h, 7, func(c qnet.Conn) qnet.Conn { stalled = newStallConn(c); return stalled })
	good := watch(t, h, 8, nil)
	for _, lv := range []*liveViewer{slow, good} {
		lv := lv
		if !eventually(5*time.Second, func() bool { _, ok := lv.firstFrame(42); return ok }) {
			t.Fatalf("viewer never got a frame: %v", lv.error())
		}
	}
	stalled.stall()
	var worst time.Duration
	for i := 0; i < 1000; i++ {
		t0 := time.Now()
		k := bot.frame()
		if d := time.Since(t0); d > worst {
			worst = d
		}
		// the good viewer keeps up (paced: its queue is small too)
		if !eventually(2*time.Second, func() bool { return good.has(k) }) {
			t.Fatalf("the good viewer lacks frame %v", k)
		}
	}
	if worst > 100*time.Millisecond {
		t.Errorf("a bot message took %v", worst)
	}
	if !eventually(2*time.Second, func() bool { return h.Stats().Leaves[LeaveSlow] == 1 }) {
		t.Fatalf("stalled viewer not dropped: %+v", h.Stats())
	}
	good.match(t, bot.frames, 42)
	if st := h.Stats(); st.Drops[DropQueue] == 0 || st.Viewers != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestHubViewerCaps(t *testing.T) {
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 2))
	global := NewViewerLimit(3)
	h1 := NewHub(bot.stream, HubConfig{MaxViewers: 2, Global: global})
	defer h1.Close()
	h2 := NewHub(bot.stream, HubConfig{MaxViewers: 5, Global: global})
	defer h2.Close()
	serve := func(h *Hub) (chan error, qnet.Conn) {
		cli, srv := qnet.MemPipe(64)
		errc := make(chan error, 1)
		go func() { errc <- h.ServeConn(context.Background(), srv, "cap") }()
		return errc, cli
	}
	var conns []qnet.Conn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		_, c := serve(h1)
		conns = append(conns, c)
	}
	if !eventually(2*time.Second, func() bool { return h1.Viewers() == 2 }) {
		t.Fatal("viewers not served")
	}
	if errc, _ := serve(h1); !errors.Is(<-errc, ErrFull) {
		t.Fatal("hub cap not enforced")
	}
	_, c := serve(h2)
	conns = append(conns, c)
	if !eventually(2*time.Second, func() bool { return global.InUse() == 3 }) {
		t.Fatal("global count")
	}
	if errc, _ := serve(h2); !errors.Is(<-errc, ErrFull) {
		t.Fatal("global cap not enforced")
	}
	conns[0].Close()
	if !eventually(2*time.Second, func() bool { return global.InUse() == 2 && h1.Viewers() == 1 }) {
		t.Fatalf("slot not released: %d %d", global.InUse(), h1.Viewers())
	}
}

func TestHubTimeoutAndRate(t *testing.T) {
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 2))
	h := NewHub(bot.stream, HubConfig{Timeout: 300 * time.Millisecond, TickInterval: 20 * time.Millisecond, PacketRate: 50})
	defer h.Close()
	cli, srv := qnet.MemPipe(1024)
	defer cli.Close()
	errc := make(chan error, 1)
	go func() { errc <- h.ServeConn(context.Background(), srv, "quiet") }()
	for i := 0; i < 500; i++ {
		_ = cli.Send([]byte("\xff\xff\xff\xffgetchallenge\n"))
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("ServeConn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("silent viewer not timed out")
	}
	st := h.Stats()
	if st.Leaves[LeaveTimeout] != 1 || st.Drops[DropRate] < 400 || st.DatagramsIn > 100 {
		t.Fatalf("stats %+v", st)
	}
}

func TestHubAttachedMidLevel(t *testing.T) {
	// a hub created while the bot is paused in the middle of a level
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 4))
	for i := 0; i < 20; i++ {
		bot.frame()
	}
	h := NewHub(bot.stream, HubConfig{})
	defer h.Close()
	lv := watch(t, h, 9, nil)
	if !eventually(5*time.Second, func() bool { return lv.has(frameKey{42, bot.srv.frame}) }) {
		t.Fatalf("no keyframe of the paused bot's frame: %v", lv.error())
	}
	lv.match(t, bot.frames, 42)
}

func TestHandler(t *testing.T) {
	bot := newSynthBot(t)
	bot.start(newSynth(42, "test", 4))
	h := NewHub(bot.stream, HubConfig{MaxViewers: 1})
	defer h.Close()
	tickets := map[string]string{"good": "b1", "other": "b2"}
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.Handle("GET /ws/v1/bots/{id}/watch", NewHandler(HandlerConfig{
		Hub: func(id string) (*Hub, bool) { return h, id == "b1" },
		Redeem: func(tk, id string) error {
			mu.Lock()
			defer mu.Unlock()
			if tickets[tk] != id {
				return errors.New("bad ticket")
			}
			delete(tickets, tk)
			return nil
		},
	}))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	get := func(path string) int {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for path, want := range map[string]int{
		"/ws/v1/bots/b9/watch?ticket=good":  http.StatusNotFound,
		"/ws/v1/bots/b1/watch":              http.StatusForbidden,
		"/ws/v1/bots/b1/watch?ticket=other": http.StatusForbidden,
		"/ws/v1/bots/b1/watch?ticket=nope":  http.StatusForbidden,
	} {
		if got := get(path); got != want {
			t.Errorf("%s: %d, want %d", path, got, want)
		}
	}
	ws := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/v1/bots/b1/watch?ticket=good"
	conn, err := qnet.DialWS(context.Background(), ws)
	if err != nil {
		t.Fatal(err)
	}
	lv := watchConn(t, conn, 5)
	if !eventually(5*time.Second, func() bool { _, ok := lv.firstFrame(42); return ok }) {
		t.Fatalf("no frame over the WebSocket: %v", lv.error())
	}
	for i := 0; i < 20; i++ {
		k := bot.frame()
		if !eventually(2*time.Second, func() bool { return lv.has(k) }) {
			t.Fatalf("viewer lacks frame %v", k)
		}
	}
	lv.match(t, bot.frames, 42)
	// the ticket was used up; the hub is full anyway
	if got := get("/ws/v1/bots/b1/watch?ticket=good"); got != http.StatusServiceUnavailable {
		t.Errorf("second viewer: %d", got)
	}
	h.Close()
	if !eventually(2*time.Second, func() bool { return errors.Is(lv.error(), fakeclient.ErrDisconnected) }) {
		t.Fatalf("viewer after Close: %v", lv.error())
	}
	// a closed hub the lookup still returns (the run just ended): no
	// point in retrying
	if got := get("/ws/v1/bots/b1/watch?ticket=other"); got != http.StatusGone {
		t.Errorf("closed hub: %d", got)
	}
}
