package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"quake2web/server/internal/agent/runner"
	"quake2web/server/internal/api"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
	"quake2web/server/internal/db/dbtest"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/testutil"
)

// e2e is a complete q2server stack (API + game host with the real game
// module) behind an httptest server.
type e2e struct {
	t   *testing.T
	st  *stack
	srv *httptest.Server
}

func newE2E(t *testing.T, databaseURL string) *e2e {
	t.Helper()
	pak := testutil.RequireFile(t, testutil.DemoPakPath())
	cfg := config.Default()
	cfg.DatabaseURL = databaseURL
	cfg.BlobDir = t.TempDir()
	cfg.DemoPak = pak
	cfg.CookieSecure = false
	var out io.Writer = io.Discard
	if testing.Verbose() {
		out = &testWriter{t}
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := newStack(context.Background(), cfg, log, stackOptions{IdleTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	e := &e2e{t: t, st: st, srv: httptest.NewServer(st.Handler)}
	t.Cleanup(func() {
		e.srv.Close()
		st.Close()
	})
	return e
}

type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

type user struct {
	e    *e2e
	c    *http.Client
	name string
	id   int64
}

func (e *e2e) do(c *http.Client, method, path string, body any, want int, out any) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			e.t.Fatalf("%s %s: %v (%s)", method, path, err, b)
		}
	}
}

// signup registers an account, then logs in with a fresh cookie jar.
func (e *e2e) signup(name string) *user {
	e.t.Helper()
	email := name + "@example.com"
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	e.do(c, "POST", "/api/v1/auth/register", map[string]string{"email": email, "password": "correct horse battery", "displayName": name}, http.StatusCreated, nil)
	jar, _ = cookiejar.New(nil)
	c = &http.Client{Jar: jar}
	var me struct{ User db.User }
	e.do(c, "POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "correct horse battery"}, http.StatusOK, &me)
	if me.User.DisplayName != name {
		e.t.Fatalf("login: %+v", me.User)
	}
	return &user{e: e, c: c, name: name, id: me.User.ID}
}

func (u *user) createGame(path string, body any) api.GameInfo {
	u.e.t.Helper()
	var out struct{ Game api.GameInfo }
	u.e.do(u.c, "POST", path, body, http.StatusCreated, &out)
	return out.Game
}

// connect joins a game and completes the Quake 2 handshake over the
// WebSocket with the returned ticket.
func (u *user) connect(ctx context.Context, gameID string) *fakeclient.Client {
	u.e.t.Helper()
	var jr api.JoinResponse
	u.e.do(u.c, "POST", "/api/v1/games/"+gameID+"/join", nil, http.StatusOK, &jr)
	if jr.Ticket == "" || !strings.Contains(jr.WSURL, "/ws/v1/games/"+gameID) {
		u.e.t.Fatalf("join: %+v", jr)
	}
	url := "ws" + strings.TrimPrefix(u.e.srv.URL, "http") + jr.WSURL
	conn, err := qnet.DialWS(ctx, url)
	if err != nil {
		u.e.t.Fatal(err)
	}
	u.e.t.Cleanup(func() { conn.Close() })
	// a ticket is single use
	if c2, err := qnet.DialWS(ctx, url); err == nil {
		c2.Close()
		u.e.t.Fatal("ticket accepted twice")
	}
	c := fakeclient.New(conn, fakeclient.Options{Printf: func(string, ...any) {}})
	if err := c.Handshake(ctx); err != nil {
		u.e.t.Fatalf("%s: handshake: %v", u.name, err)
	}
	return c
}

// drive sends usercmds for d.
func drive(ctx context.Context, t *testing.T, c *fakeclient.Client, d time.Duration, cmd shared.UserCmd) {
	t.Helper()
	for end := time.Now().Add(d); time.Now().Before(end); {
		c.SendCmd(cmd)
		if err := c.Poll(ctx, 25*time.Millisecond); err != nil {
			t.Fatalf("poll: %v", err)
		}
	}
}

func playerName(c *fakeclient.Client, num int32) string {
	cs := c.ConfigStrings[q2const.CS_PLAYERSKINS+int(num)]
	name, _, _ := strings.Cut(cs, "\\")
	return name
}

func dist(a, b shared.Vec3) float32 { return shared.VectorLength(shared.VectorSubtract(a, b)) }

func (e *e2e) waitGone(u *user, id string) {
	e.t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, err := e.st.Games.Get(context.Background(), id); err != nil {
			e.do(u.c, "GET", "/api/v1/games/"+id, nil, http.StatusNotFound, nil)
			return
		}
	}
	e.t.Fatalf("game %s still running", id)
}

func testSinglePlayerSaveLoad(t *testing.T, databaseURL string) {
	e := newE2E(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	alice := e.signup("alice")

	g := alice.createGame("/api/v1/games", map[string]any{"mode": "sp", "map": "demo1", "cvars": map[string]string{"skill": "0"}})
	if g.Mode != "sp" || g.Map != "demo1" || g.OwnerID != alice.id || g.MaxPlayers != 1 || g.Pakset != "demo" {
		t.Fatalf("created %+v", g)
	}
	// the new game autosaved on entering the level
	e.do(alice.c, "GET", "/api/v1/saves/autosave", nil, http.StatusOK, nil)

	c := alice.connect(ctx, g.ID)
	if got := c.ConfigStrings[q2const.CS_MODELS+1]; got != "maps/demo1.bsp" {
		t.Fatalf("CS_MODELS+1 = %q", got)
	}
	if got := playerName(c, c.ServerData.PlayerNum); got != "alice" {
		t.Errorf("player name %q, want the account display name", got)
	}
	var info struct{ Game api.GameInfo }
	e.do(alice.c, "GET", "/api/v1/games/"+g.ID, nil, http.StatusOK, &info)
	if info.Game.Players != 1 {
		t.Errorf("players = %d", info.Game.Players)
	}

	// let the player drop to the floor, then walk
	drive(ctx, t, c, time.Second, shared.UserCmd{Msec: 25})
	start := c.Origin()
	drive(ctx, t, c, 2*time.Second, shared.UserCmd{Msec: 25, ForwardMove: 200})
	drive(ctx, t, c, 500*time.Millisecond, shared.UserCmd{Msec: 25})
	saved := c.Origin()
	if d := dist(start, saved); d < 64 {
		t.Fatalf("player did not walk: %v -> %v", start, saved)
	}
	health := c.Frame.PlayerState.Stats[q2const.STAT_HEALTH]
	t.Logf("walked from %v to %v (health %d)", start, saved, health)

	// "save quick" from the client console goes to the owner's account
	c.StringCmd("save quick")
	var saveMsg bool
	for i := 0; i < 40 && !saveMsg; i++ {
		drive(ctx, t, c, 50*time.Millisecond, shared.UserCmd{Msec: 25})
		for _, p := range c.Prints {
			if strings.Contains(p, "Saving game") || strings.Contains(p, "Done") {
				saveMsg = true
			}
		}
	}
	if !saveMsg {
		t.Errorf("no save confirmation printed: %q", c.Prints)
	}
	saved = c.Origin()
	var sv struct{ Save db.SaveInfo }
	e.do(alice.c, "GET", "/api/v1/saves/quick", nil, http.StatusOK, &sv)
	if sv.Save.Mode != "sp" || sv.Save.MapCmd != "demo1" || sv.Save.Size == 0 {
		t.Errorf("save info %+v", sv.Save)
	}
	var list struct{ Saves []db.SaveInfo }
	e.do(alice.c, "GET", "/api/v1/saves", nil, http.StatusOK, &list)
	slots := map[string]bool{}
	for _, s := range list.Saves {
		slots[s.Slot] = true
	}
	if !slots["quick"] || !slots["autosave"] || slots["current"] {
		t.Errorf("save slots %v", slots)
	}

	// walk further away, then leave: the single player game autosaves and ends
	drive(ctx, t, c, time.Second, shared.UserCmd{Msec: 25, ForwardMove: -200})
	c.Disconnect()
	e.waitGone(alice, g.ID)
	var auto struct{ Save db.SaveInfo }
	e.do(alice.c, "GET", "/api/v1/saves/autosave", nil, http.StatusOK, &auto)
	if strings.HasPrefix(auto.Save.Comment, "ENTERING") {
		t.Errorf("no autosave when leaving the game: %+v", auto.Save)
	}

	// another account cannot load alice's save
	bob := e.signup("bob")
	e.do(bob.c, "POST", "/api/v1/games/from-save", map[string]string{"slot": "quick"}, http.StatusNotFound, nil)

	g2 := alice.createGame("/api/v1/games/from-save", map[string]string{"slot": "quick"})
	if g2.Mode != "sp" || g2.Map != "demo1" || g2.ID == g.ID {
		t.Fatalf("from-save game %+v", g2)
	}
	c2 := alice.connect(ctx, g2.ID)
	drive(ctx, t, c2, 500*time.Millisecond, shared.UserCmd{Msec: 25})
	got := c2.Origin()
	t.Logf("restored at %v (saved at %v), health %d", got, saved, c2.Frame.PlayerState.Stats[q2const.STAT_HEALTH])
	if d := dist(got, saved); d > 16 {
		t.Errorf("position not restored: got %v, saved %v (%.1f units away)", got, saved, d)
	}
	if d := dist(got, start); d < 32 {
		t.Errorf("restored at the level start %v", got)
	}

	// DELETE stops the game
	e.do(alice.c, "DELETE", "/api/v1/games/"+g2.ID, nil, http.StatusNoContent, nil)
	e.waitGone(alice, g2.ID)
}

func testDeathmatch(t *testing.T, databaseURL string) {
	e := newE2E(t, databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	alice, bob := e.signup("alice"), e.signup("bob")

	g := alice.createGame("/api/v1/games", map[string]any{"mode": "dm", "map": "demo1", "public": true,
		"cvars": map[string]string{"fraglimit": "10", "timelimit": "5", "dmflags": "16"}})
	if g.Mode != "dm" || g.MaxPlayers != 8 {
		t.Fatalf("created %+v", g)
	}
	var games struct{ Games []api.GameInfo }
	e.do(bob.c, "GET", "/api/v1/games", nil, http.StatusOK, &games)
	if len(games.Games) != 1 || games.Games[0].ID != g.ID {
		t.Fatalf("bob sees %+v", games.Games)
	}

	a := alice.connect(ctx, g.ID)
	b := bob.connect(ctx, g.ID)
	seen := func(c, other *fakeclient.Client) bool {
		for _, s := range c.FrameEntities(&c.Frame) {
			if s.Number == other.ServerData.PlayerNum+1 {
				return true
			}
		}
		return false
	}
	// DM spawn points are random and may not be in each other's PVS: move
	// bob's player next to alice's (the next usercmd links him there).
	drive(ctx, t, a, 300*time.Millisecond, shared.UserCmd{Msec: 25})
	drive(ctx, t, b, 300*time.Millisecond, shared.UserCmd{Msec: 25})
	inst, err := e.st.Games.Host().Get(g.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = inst.Do(func(s *sv.Server) {
		ea := s.SVS.Clients[a.ServerData.PlayerNum].Edict
		eb := s.SVS.Clients[b.ServerData.PlayerNum].Edict
		eb.S.Origin = ea.S.Origin
		eb.Velocity = shared.Vec3{}
	})
	var aSeesB, bSeesA bool
	for i := 0; i < 80 && !(aSeesB && bSeesA); i++ {
		a.SendCmd(shared.UserCmd{Msec: 25})
		b.SendCmd(shared.UserCmd{Msec: 25})
		_ = a.Poll(ctx, 20*time.Millisecond)
		_ = b.Poll(ctx, 20*time.Millisecond)
		aSeesB = aSeesB || seen(a, b)
		bSeesA = bSeesA || seen(b, a)
	}
	if !aSeesB || !bSeesA {
		t.Logf("a=%d at %v ents %v; b=%d at %v ents %v; prints a %q b %q", a.ServerData.PlayerNum, a.Origin(), a.FrameEntities(&a.Frame),
			b.ServerData.PlayerNum, b.Origin(), b.FrameEntities(&b.Frame), a.Prints, b.Prints)
		t.Errorf("players do not see each other: a sees b %v, b sees a %v", aSeesB, bSeesA)
	}
	if got := playerName(a, b.ServerData.PlayerNum); got != "bob" {
		t.Errorf("alice sees player %d named %q", b.ServerData.PlayerNum, got)
	}
	if got := playerName(b, a.ServerData.PlayerNum); got != "alice" {
		t.Errorf("bob sees player %d named %q", a.ServerData.PlayerNum, got)
	}
	var info struct{ Game api.GameInfo }
	e.do(alice.c, "GET", "/api/v1/games/"+g.ID, nil, http.StatusOK, &info)
	if info.Game.Players != 2 {
		t.Errorf("players = %d", info.Game.Players)
	}

	// no saving in deathmatch
	a.StringCmd("save dm")
	drive(ctx, t, a, 300*time.Millisecond, shared.UserCmd{Msec: 25})
	if !strings.Contains(strings.Join(a.Prints, ""), "not available") {
		t.Errorf("save in deathmatch not refused: %q", a.Prints)
	}
	e.do(alice.c, "GET", "/api/v1/saves/dm", nil, http.StatusNotFound, nil)

	// bob leaves: the game keeps running with one player
	b.Disconnect()
	for i := 0; i < 40; i++ {
		drive(ctx, t, a, 50*time.Millisecond, shared.UserCmd{Msec: 25})
		if st, _ := e.st.Games.Get(ctx, g.ID); st.Players == 1 {
			break
		}
	}
	if st, err := e.st.Games.Get(ctx, g.ID); err != nil || st.Players != 1 {
		t.Errorf("after bob left: %+v %v", st, err)
	}
	// only the owner can stop it
	e.do(bob.c, "DELETE", "/api/v1/games/"+g.ID, nil, http.StatusForbidden, nil)
	e.do(alice.c, "DELETE", "/api/v1/games/"+g.ID, nil, http.StatusNoContent, nil)
	e.waitGone(alice, g.ID)

	// metrics are exported
	resp, err := http.Get(e.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, m := range []string{"q2_instances", "q2_players", "q2_tick_duration_seconds"} {
		if !strings.Contains(string(body), m) {
			t.Errorf("metric %s missing", m)
		}
	}
}

func TestE2ESinglePlayerSaveLoad(t *testing.T) { testSinglePlayerSaveLoad(t, "") }
func TestE2EDeathmatch(t *testing.T)           { testDeathmatch(t, "") }

func TestE2EPostgres(t *testing.T) {
	t.Run("sp", func(t *testing.T) { testSinglePlayerSaveLoad(t, dbtest.URL(t)) })
	t.Run("dm", func(t *testing.T) { testDeathmatch(t, dbtest.URL(t)) })
}

// TestE2ECTF checks that a "ctf" game runs the CTF module (ctf/game.so equivalent):
// the CTF item table is registered and the team menu / team command work.
func TestE2ECTF(t *testing.T) {
	e := newE2E(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	alice := e.signup("alice")
	g := alice.createGame("/api/v1/games", map[string]any{"mode": "ctf", "map": "demo1"})
	if g.Mode != "ctf" {
		t.Fatalf("created %+v", g)
	}
	a := alice.connect(ctx, g.ID)
	drive(ctx, t, a, 300*time.Millisecond, shared.UserCmd{Msec: 25})
	var grapple bool
	for i := q2const.CS_ITEMS; i < q2const.CS_ITEMS+q2const.MAX_ITEMS; i++ {
		if a.ConfigStrings[i] == "Grapple" {
			grapple = true
		}
	}
	if !grapple {
		t.Fatalf("CTF item table not active (no Grapple configstring)")
	}
	a.StringCmd("team red")
	drive(ctx, t, a, 500*time.Millisecond, shared.UserCmd{Msec: 25})
	if !strings.Contains(strings.ToLower(strings.Join(a.Prints, "")), "red team") {
		t.Errorf("team red not acknowledged: %q", a.Prints)
	}
}

// newE2EBots is newE2E with bots enabled for every account.
func newE2EBots(t *testing.T) *e2e {
	t.Helper()
	pak := testutil.RequireFile(t, testutil.DemoPakPath())
	cfg := config.Default()
	cfg.BlobDir = t.TempDir()
	cfg.DemoPak = pak
	cfg.CookieSecure = false
	cfg.Bots.Enabled = true
	cfg.Bots.AllowUsers = true
	cfg.Bots.RunsDir = t.TempDir()
	cfg.Bots.MaxRun = 2 * time.Minute
	var out io.Writer = io.Discard
	if testing.Verbose() {
		out = &testWriter{t}
	}
	log := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	st, err := newStack(context.Background(), cfg, log, stackOptions{IdleTimeout: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	e := &e2e{t: t, st: st, srv: httptest.NewServer(st.Handler)}
	t.Cleanup(func() {
		e.srv.Close()
		st.Close()
	})
	return e
}

func (e *e2e) wsURL(path string) string { return "ws" + strings.TrimPrefix(e.srv.URL, "http") + path }

// readFeed reads decision feed messages until done returns true.
func readFeed(ctx context.Context, t *testing.T, c *websocket.Conn, done func(msg map[string]any) bool) {
	t.Helper()
	for {
		typ, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("decision feed: %v", err)
		}
		if typ != websocket.MessageText {
			t.Fatalf("decision feed: %v message", typ)
		}
		var msg map[string]any
		if err := json.Unmarshal(b, &msg); err != nil {
			t.Fatalf("decision feed: %v (%s)", err, b)
		}
		if done(msg) {
			return
		}
	}
}

// TestE2EBotWatch drives a bot through the API: a user starts a scripted
// bot, watches its live view over the relay with a protocol 34 client and
// follows its decision feed, then stops it and fetches its artifacts.
func TestE2EBotWatch(t *testing.T) {
	e := newE2EBots(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	alice, bob := e.signup("alice"), e.signup("bob")

	var created struct{ Bot api.BotInfo }
	e.do(alice.c, "POST", "/api/v1/bots", map[string]any{"backend": "scripted", "maps": []string{"demo1"}}, http.StatusCreated, &created)
	b := created.Bot
	if b.ID == "" || b.OwnerID != alice.id || b.Status != api.BotStarting || b.Backend != "scripted" || b.Public ||
		len(b.Maps) != 1 || b.Maps[0] != "demo1" || b.Skill != 1 {
		t.Fatalf("created %+v", b)
	}
	// private: bob neither sees nor watches it
	var list struct{ Bots []api.BotInfo }
	e.do(bob.c, "GET", "/api/v1/bots", nil, http.StatusOK, &list)
	if len(list.Bots) != 0 {
		t.Fatalf("bob sees %+v", list.Bots)
	}
	e.do(bob.c, "GET", "/api/v1/bots/"+b.ID, nil, http.StatusNotFound, nil)
	e.do(bob.c, "POST", "/api/v1/bots/"+b.ID+"/watch", nil, http.StatusNotFound, nil)
	e.do(bob.c, "DELETE", "/api/v1/bots/"+b.ID, nil, http.StatusNotFound, nil)
	// a jev bot is for administrators
	e.do(alice.c, "POST", "/api/v1/bots", map[string]any{"backend": "jev", "maps": []string{"demo1"}}, http.StatusForbidden, nil)

	var w api.BotWatch
	e.do(alice.c, "POST", "/api/v1/bots/"+b.ID+"/watch", nil, http.StatusOK, &w)
	if w.Ticket == "" || w.DecisionsTicket == "" || w.Pakset != "demo" ||
		!strings.HasPrefix(w.WSURL, "/ws/v1/bots/"+b.ID+"/watch?ticket=") || !strings.HasPrefix(w.DecisionsURL, "/ws/v1/bots/"+b.ID+"/decisions?ticket=") {
		t.Fatalf("watch %+v", w)
	}

	// the decision feed: hello, then decisions with their provenance
	feed, _, err := websocket.Dial(ctx, e.wsURL(w.DecisionsURL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.CloseNow() //nolint:errcheck
	var hello map[string]any
	readFeed(ctx, t, feed, func(m map[string]any) bool { hello = m; return true })
	if hello["t"] != "hello" || hello["bot"] != b.ID || hello["backend"] != "scripted" {
		t.Fatalf("hello %v", hello)
	}

	// the live view: a viewer joins the relay as a protocol 34 client
	conn, err := qnet.DialWS(ctx, e.wsURL(w.WSURL))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	viewer := fakeclient.New(conn, fakeclient.Options{Printf: func(string, ...any) {}})
	if err := viewer.Handshake(ctx); err != nil {
		t.Fatalf("viewer handshake: %v", err)
	}
	if viewer.ServerData.AttractLoop != 1 || viewer.State != fakeclient.CaActive || viewer.MapName() != "demo1" {
		t.Fatalf("viewer: attractloop %d, state %v, map %q", viewer.ServerData.AttractLoop, viewer.State, viewer.MapName())
	}
	first := viewer.Frame.ServerFrame
	for end := time.Now().Add(1500 * time.Millisecond); time.Now().Before(end); {
		viewer.SendCmd(shared.UserCmd{Msec: 50})
		if err := viewer.Poll(ctx, 50*time.Millisecond); err != nil {
			t.Fatalf("viewer poll: %v", err)
		}
	}
	if got := viewer.Frame.ServerFrame; got < first+5 || !viewer.Frame.Valid {
		t.Fatalf("viewer frames: %d -> %d (valid %v)", first, got, viewer.Frame.Valid)
	}
	// a watch ticket is single use
	if c2, err := qnet.DialWS(ctx, e.wsURL(w.WSURL)); err == nil {
		c2.Close()
		t.Fatal("watch ticket accepted twice")
	}
	if c2, _, err := websocket.Dial(ctx, e.wsURL(w.DecisionsURL), nil); err == nil {
		c2.CloseNow() //nolint:errcheck
		t.Fatal("decisions ticket accepted twice")
	}
	// an expired ticket is refused
	var w2 api.BotWatch
	e.do(alice.c, "POST", "/api/v1/bots/"+b.ID+"/watch", nil, http.StatusOK, &w2)
	tickets := e.st.App.Tickets.(*auth.Tickets)
	tickets.SetClock(func() time.Time { return time.Now().Add(2 * auth.TicketTTL) })
	if c2, err := qnet.DialWS(ctx, e.wsURL(w2.WSURL)); err == nil {
		c2.Close()
		t.Fatal("expired watch ticket accepted")
	}
	if c2, _, err := websocket.Dial(ctx, e.wsURL(w2.DecisionsURL), nil); err == nil {
		c2.CloseNow() //nolint:errcheck
		t.Fatal("expired decisions ticket accepted")
	}
	tickets.SetClock(time.Now)
	// no ticket at all
	if c2, _, err := websocket.Dial(ctx, e.wsURL("/ws/v1/bots/"+b.ID+"/decisions"), nil); err == nil {
		c2.CloseNow() //nolint:errcheck
		t.Fatal("decision feed without a ticket")
	}

	var decision map[string]any
	readFeed(ctx, t, feed, func(m map[string]any) bool {
		if m["t"] != "decision" {
			return false
		}
		prov, _ := m["provenance"].(map[string]any)
		if len(prov) == 0 {
			t.Fatalf("decision without provenance: %v", m)
		}
		decision = m
		return true
	})
	if decision["map"] != "demo1" || decision["sf"].(float64) <= 0 {
		t.Fatalf("decision %v", decision)
	}
	// soon the backend's answers come with them: options, probabilities, the choice
	readFeed(ctx, t, feed, func(m map[string]any) bool {
		qs, _ := m["questions"].([]any)
		if m["t"] != "decision" || len(qs) == 0 {
			return false
		}
		decision = m
		return true
	})
	t.Logf("decision: %v", decision)
	for _, q := range decision["questions"].([]any) {
		q := q.(map[string]any)
		opts, _ := q["options"].([]any)
		if q["id"] == "" || q["chosen"] == "" || len(opts) == 0 {
			t.Fatalf("question %v", q)
		}
		sum := 0.0
		for _, o := range opts {
			sum += o.(map[string]any)["p"].(float64)
		}
		if sum < 0.98 || sum > 1.02 {
			t.Errorf("question %v: probabilities sum to %v", q["id"], sum)
		}
	}

	var got struct{ Bot api.BotInfo }
	e.do(alice.c, "GET", "/api/v1/bots/"+b.ID, nil, http.StatusOK, &got)
	if got.Bot.Status != api.BotRunning || got.Bot.Level == nil || got.Bot.Level.Map != "demo1" || got.Bot.Viewers != 1 || got.Bot.Live == nil {
		t.Fatalf("running bot %+v", got.Bot)
	}
	resp, err := http.Get(e.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metricsText, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, m := range []string{"q2bot_runs_active 1", "q2bot_viewers 1", "q2bot_decisions_total{backend=\"scripted\""} {
		if !strings.Contains(string(metricsText), m) {
			t.Errorf("metrics lack %s", m)
		}
	}

	// stop it: the feed says bye, the run directory has its artifacts
	e.do(alice.c, "DELETE", "/api/v1/bots/"+b.ID, nil, http.StatusNoContent, nil)
	readFeed(ctx, t, feed, func(m map[string]any) bool {
		if m["t"] != "bye" {
			return false
		}
		if m["status"] != api.BotStopped {
			t.Fatalf("bye %v", m)
		}
		return true
	})
	e.do(alice.c, "GET", "/api/v1/bots/"+b.ID, nil, http.StatusOK, &got)
	if got.Bot.Status != api.BotStopped || got.Bot.EndedAt == nil || len(got.Bot.Summary) == 0 {
		t.Fatalf("stopped bot %+v", got.Bot)
	}
	var sum struct {
		Schema, Run, Outcome string
	}
	if err := json.Unmarshal(got.Bot.Summary, &sum); err != nil || sum.Schema != "q2bot.run/1" || sum.Run != b.ID || sum.Outcome != "aborted" {
		t.Fatalf("summary %s: %v", got.Bot.Summary, err)
	}
	e.do(alice.c, "POST", "/api/v1/bots/"+b.ID+"/watch", nil, http.StatusConflict, nil)
	kinds := map[string]string{}
	for _, a := range got.Bot.Artifacts {
		kinds[a.Kind] = a.Name
	}
	if kinds[api.ArtifactRun] != "run.json" || kinds[api.ArtifactDemo] == "" || kinds[api.ArtifactTrace] == "" {
		t.Fatalf("artifacts %+v", got.Bot.Artifacts)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/bots/"+b.ID+"/artifacts/"+kinds[api.ArtifactDemo], nil)
	resp, err = alice.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	dm2, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("demo: %s %v", resp.Status, resp.Header)
	}
	ds, err := demo.Validate(dm2)
	if err != nil {
		t.Fatalf("demo %s: %v", kinds[api.ArtifactDemo], err)
	}
	if ds.Map != "maps/demo1.bsp" || ds.Frames < 10 || !ds.Terminated {
		t.Fatalf("demo stats %+v", ds)
	}
	t.Logf("demo %s: %d frames", kinds[api.ArtifactDemo], ds.Frames)
	// the trace is served as stored, gzip-encoded JSON lines
	req, _ = http.NewRequest("GET", e.srv.URL+"/api/v1/bots/"+b.ID+"/artifacts/"+kinds[api.ArtifactTrace], nil)
	resp, err = alice.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/x-ndjson" ||
		!strings.Contains(string(tr), `"type":"run_start"`) {
		t.Fatalf("trace: %s %v %.200s", resp.Status, resp.Header, tr)
	}
	// bob still sees nothing of it; nobody reads outside the allowlist
	e.do(bob.c, "GET", "/api/v1/bots/"+b.ID+"/artifacts/run.json", nil, http.StatusNotFound, nil)
	e.do(alice.c, "GET", "/api/v1/bots/"+b.ID+"/artifacts/bot.json", nil, http.StatusNotFound, nil)
	e.do(alice.c, "GET", "/api/v1/bots/"+b.ID+"/artifacts/ep-000/log.txt", nil, http.StatusNotFound, nil)
	e.do(alice.c, "GET", "/api/v1/bots/"+b.ID+"/artifacts/ep-000/..%2F..%2Fbot.json", nil, http.StatusNotFound, nil)
	// its game is gone
	if _, err := e.st.Games.Get(ctx, "bot-"+b.ID); err == nil {
		t.Error("the bot's game still runs")
	}
}

// waitBotStatus polls a bot until its status is want.
func (e *e2e) waitBotStatus(c *http.Client, id, want string) api.BotInfo {
	e.t.Helper()
	var got struct{ Bot api.BotInfo }
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		e.do(c, "GET", "/api/v1/bots/"+id, nil, http.StatusOK, &got)
		if got.Bot.Status == want {
			return got.Bot
		}
	}
	e.t.Fatalf("bot %s: status %s, want %s", id, got.Bot.Status, want)
	return got.Bot
}

// TestE2EBotShutdown: closing the server stops its bots before their
// games, so every run ends as stopped with its run directory complete
// (run.json, a terminated demo).
func TestE2EBotShutdown(t *testing.T) {
	e := newE2EBots(t)
	alice := e.signup("alice")
	var created struct{ Bot api.BotInfo }
	e.do(alice.c, "POST", "/api/v1/bots", map[string]any{"backend": "scripted", "maps": []string{"demo1"}}, http.StatusCreated, &created)
	id := created.Bot.ID
	e.waitBotStatus(alice.c, id, api.BotRunning)
	time.Sleep(500 * time.Millisecond) // some frames into the level
	dir := filepath.Join(e.st.Bots.Dir(), id)
	e.st.Close()

	var meta struct{ Status, Reason string }
	b, err := os.ReadFile(filepath.Join(dir, runner.BotFile))
	if err != nil || json.Unmarshal(b, &meta) != nil || meta.Status != api.BotStopped || meta.Reason != "the server shut down" {
		t.Fatalf("bot.json %s: %v", b, err)
	}
	var sum struct{ Outcome string }
	b, err = os.ReadFile(filepath.Join(dir, runner.RunFile))
	if err != nil || json.Unmarshal(b, &sum) != nil || sum.Outcome != "aborted" {
		t.Fatalf("run.json %s: %v", b, err)
	}
	demos, _ := filepath.Glob(filepath.Join(dir, "ep-000", runner.DemoDir, "*.dm2"))
	if len(demos) == 0 {
		t.Fatal("no demo")
	}
	for _, d := range demos {
		data, _ := os.ReadFile(d)
		if st, err := demo.Validate(data); err != nil || !st.Terminated {
			t.Errorf("%s: %+v %v", d, st, err)
		}
	}
}

// TestE2EDemoBot: the -bot bot is public and owned by the server, so
// anyone may follow it, and nobody but an administrator stops it.
func TestE2EDemoBot(t *testing.T) {
	e := newE2EBots(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	b, err := startDemoBot(ctx, e.st.Bots, " demo1, ", runner.BackendScripted)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Public || b.OwnerID != 0 || b.Name != "demo bot" || len(b.Maps) != 1 {
		t.Fatalf("demo bot %+v", b)
	}
	anon := &http.Client{}
	var list struct{ Bots []api.BotInfo }
	e.do(anon, "GET", "/api/v1/bots", nil, http.StatusOK, &list)
	if len(list.Bots) != 1 || list.Bots[0].ID != b.ID {
		t.Fatalf("anonymous list %+v", list.Bots)
	}
	var w api.BotWatch
	e.do(anon, "POST", "/api/v1/bots/"+b.ID+"/watch", nil, http.StatusOK, &w)
	feed, _, err := websocket.Dial(ctx, e.wsURL(w.DecisionsURL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.CloseNow() //nolint:errcheck
	readFeed(ctx, t, feed, func(m map[string]any) bool { return m["t"] == "hello" })
	e.do(anon, "DELETE", "/api/v1/bots/"+b.ID, nil, http.StatusUnauthorized, nil)
	alice := e.signup("alice")
	e.do(alice.c, "DELETE", "/api/v1/bots/"+b.ID, nil, http.StatusForbidden, nil)
	if _, err := startDemoBot(ctx, e.st.Bots, "demo2", runner.BackendScripted); err == nil {
		t.Error("a demo bot not starting on the campaign's first map")
	}
}
