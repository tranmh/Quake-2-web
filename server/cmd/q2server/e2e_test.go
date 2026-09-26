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
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/api"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
	"quake2web/server/internal/db/dbtest"
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
