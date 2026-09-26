package host

import (
	"context"
	"encoding/binary"
	"errors"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/api"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/db"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
)

// inUse counts the connected clients of an instance.
func inUse(t *testing.T, inst *Instance) int {
	t.Helper()
	n := 0
	if err := inst.Do(func(s *sv.Server) {
		for i := range s.SVS.Clients {
			if s.SVS.Clients[i].InUse() {
				n++
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// Two WebSocket connections from the same remote IP (every browser behind
// the reverse proxy, or behind one NAT) must not be able to address each
// other's netchan: the server routes sequenced packets by base address +
// qport, so a shared base lets one player inject commands into (or steal the
// outgoing stream of) another player's client by guessing its 16-bit qport.
func TestConnectionsFromSameIPAreIsolated(t *testing.T) {
	h := New()
	inst := startDemo1(t, h, "iso")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	victim := fakeclient.New(inst.ConnectMem("10.0.0.7"), fakeclient.Options{Qport: 4242})
	if err := victim.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		victim.SendCmd(shared.UserCmd{Msec: 50})
		_ = victim.Poll(ctx, 25*time.Millisecond)
	}
	if n := inUse(t, inst); n != 1 {
		t.Fatalf("clients before attack = %d", n)
	}

	// attacker: another connection from the same IP sends a sequenced
	// packet carrying the victim's qport and "disconnect"
	atk := inst.ConnectMem("10.0.0.7")
	defer atk.Close()
	pkt := make([]byte, 10)
	binary.LittleEndian.PutUint32(pkt[0:], 1<<29) // far ahead of the victim's sequence
	binary.LittleEndian.PutUint32(pkt[4:], 0)
	binary.LittleEndian.PutUint16(pkt[8:], uint16(victim.Qport()))
	pkt = append(pkt, q2const.Clc_stringcmd)
	pkt = append(pkt, "disconnect\x00"...)
	if err := atk.Send(pkt); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := inUse(t, inst); n != 1 {
		t.Fatalf("another connection from the same IP disconnected the victim (clients = %d)", n)
	}
}

// A panic inside one instance (a bug in the game or server code reached by
// client input) must end that instance only, not the whole process with
// every other game and the HTTP API.
func TestInstancePanicStopsOnlyThatInstance(t *testing.T) {
	h := New()
	other := startDemo1(t, h, "bystander")
	inst, err := h.Create(InstanceConfig{
		ID: "crashy",
		Server: sv.Config{
			FS:        demoFS(t),
			Game:      stubgame.New(),
			Dedicated: true,
			Printf:    func(string, ...any) {},
		},
		Commands: []string{"map demo1"},
		ClientCommand: func(_ *Instance, s *sv.Server, _ *sv.Client, _ *Player) bool {
			if s.Cmd.Argv(0) == "crashme" {
				var m map[string]int
				m["boom"]++ // nil map write: a runtime panic, not a Com_Error
			}
			return false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := fakeclient.New(inst.ConnectMem("10.0.0.9"), fakeclient.Options{})
	if err := c.Handshake(ctx); err != nil {
		t.Fatal(err)
	}
	c.StringCmd("crashme")
	for i := 0; i < 5; i++ {
		c.SendCmd(shared.UserCmd{Msec: 50})
		_ = c.Poll(ctx, 25*time.Millisecond)
	}
	select {
	case <-inst.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("crashed instance still running")
	}
	if inst.Err() == nil {
		t.Error("Err() is nil after a panic")
	}
	if _, err := h.Get("crashy"); err == nil {
		t.Error("crashed instance still registered")
	}
	if n := inUse(t, other); n != 0 {
		t.Errorf("bystander clients = %d", n)
	}
}

func testGames(t *testing.T, cfg GamesConfig) *Games {
	t.Helper()
	idx, store := demoIndex(t)
	cfg.Indexes = func(context.Context, string) (*manifest.Index, error) { return idx, nil }
	cfg.Blobs = store
	cfg.Tickets = auth.NewTickets(0)
	cfg.Game = func(*crand.Rand) sv.GameFactory { return stubgame.New() }
	g := NewGames(cfg)
	t.Cleanup(g.Close)
	return g
}

// Every game instance is a goroutine with a loaded map ticking at 10 Hz;
// one account must not be able to start an unbounded number of them.
func TestGamesPerOwnerLimit(t *testing.T) {
	g := testGames(t, GamesConfig{})
	ctx := context.Background()
	var err error
	n := 0
	for ; n < 50; n++ {
		if _, err = g.Create(ctx, api.GameSpec{OwnerID: 7, Mode: "dm", Map: "demo1"}); err != nil {
			break
		}
	}
	if err == nil {
		t.Fatalf("one account started %d games", n)
	}
	if !errors.Is(err, api.ErrGameLimit) {
		t.Fatalf("error %v, want ErrGameLimit", err)
	}
	if n != DefaultMaxGamesPerOwner {
		t.Errorf("limit hit after %d games, want %d", n, DefaultMaxGamesPerOwner)
	}
	// other accounts and server games are not affected
	if _, err := g.Create(ctx, api.GameSpec{OwnerID: 8, Mode: "dm", Map: "demo1"}); err != nil {
		t.Errorf("other owner: %v", err)
	}
	// stopping one frees a slot
	list, _ := g.List(ctx)
	for _, gi := range list {
		if gi.OwnerID == 7 {
			if err := g.Stop(ctx, gi.ID); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if _, err := g.Create(ctx, api.GameSpec{OwnerID: 7, Mode: "dm", Map: "demo1"}); err != nil {
		t.Errorf("after stop: %v", err)
	}
}

// A stopped game must be gone at once (a second DELETE answers 404, the
// list no longer shows it), not after a background goroutine ran.
func TestGamesStopIsImmediate(t *testing.T) {
	g := testGames(t, GamesConfig{})
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		id, err := g.Create(ctx, api.GameSpec{Mode: "dm", Map: "demo1"})
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Stop(ctx, id); err != nil {
			t.Fatal(err)
		}
		if err := g.Stop(ctx, id); !errors.Is(err, api.ErrGameNotFound) {
			t.Fatalf("second Stop: %v", err)
		}
		if _, err := g.Get(ctx, id); !errors.Is(err, api.ErrGameNotFound) {
			t.Fatalf("Get after Stop: %v", err)
		}
	}
}

// floodConn yields n datagrams as fast as they are read, then blocks.
type floodConn struct {
	n    int
	sent chan struct{}
}

func (c *floodConn) Send([]byte) error { return nil }
func (c *floodConn) Close() error      { return nil }
func (c *floodConn) Recv(ctx context.Context) ([]byte, error) {
	if c.n > 0 {
		c.n--
		if c.n == 0 {
			close(c.sent)
		}
		return []byte{0xff, 0xff, 0xff, 0xff, 'x'}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// One connection flooding datagrams must not monopolize the instance inbox
// (shared by every player of the game, and dropped from when full) or its
// goroutine: each connection is rate limited.
func TestConnectionFloodIsRateLimited(t *testing.T) {
	inst := &Instance{
		id:      "flood",
		host:    New(),
		inbox:   make(chan qnet.Packet, 100000),
		control: make(chan func(*sv.Server)),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		conns:   map[qnet.Addr]*Player{},
	}
	fc := &floodConn{n: 20000, sent: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { _ = inst.ServeConn(ctx, fc, "10.0.0.5"); close(served) }()
	select {
	case <-fc.sent:
	case <-time.After(10 * time.Second):
		t.Fatal("flood not consumed")
	}
	got := len(inst.inbox)
	cancel()
	close(inst.done) // lets connClosed's Do return
	<-served
	if got > 2*connBurst {
		t.Fatalf("one connection queued %d datagrams in one burst (limit %d)", got, connBurst)
	}
}

// sv.MapCache keys maps by name, length and a 64-bit FNV-1a hash, which is
// not collision resistant: a map crafted to collide with a public map (e.g.
// demo1.bsp) and loaded first from a user's private pakset would replace
// the collision model of every later public game; and every distinct map
// ever loaded stays cached forever. Only the system demo pakset (whose
// content no user controls) may use the process-wide cache.
func TestPrivatePaksetsDoNotShareMapCache(t *testing.T) {
	g := testGames(t, GamesConfig{})
	if g.mapCacheFor(api.GameSpec{Pakset: api.DemoPaksetID}) == nil {
		t.Error("demo pakset games do not use the shared map cache")
	}
	if g.mapCacheFor(api.GameSpec{Pakset: "0123456789abcdef", OwnerID: 7}) != nil {
		t.Error("games of a user pakset use the process-wide map cache")
	}
}

// A player could create unlimited save slots ("save a1", "save a2", ...),
// each a database row of up to a few MiB.
func TestSaveSlotLimit(t *testing.T) {
	repo := db.NewMemory()
	g := testGames(t, GamesConfig{Saves: db.NewSaveStore(repo)})
	ctx := context.Background()
	u, err := repo.CreateUser(ctx, db.User{Email: "s@example.com", DisplayName: "s", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	m := &gameMeta{spec: api.GameSpec{OwnerID: u.ID}}
	for i := 0; i < MaxSaveSlots; i++ {
		if !g.saveSlotAvailable(m, "s"+strconv.Itoa(i)) {
			t.Fatalf("slot %d refused", i)
		}
		if err := repo.PutSave(ctx, u.ID, "s"+strconv.Itoa(i), []byte("x"), db.SaveMeta{}); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.PutSave(ctx, u.ID, AutosaveSlot, []byte("x"), db.SaveMeta{})
	if g.saveSlotAvailable(m, "one-more") {
		t.Error("slot beyond the limit accepted")
	}
	if !g.saveSlotAvailable(m, "s3") {
		t.Error("overwriting an existing slot refused")
	}
}
