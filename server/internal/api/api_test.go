package api

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
	"quake2web/server/internal/db/dbtest"
	"quake2web/server/internal/testutil"
)

// fakeHost is a GameHost for tests.
type fakeHost struct {
	mu      sync.Mutex
	tickets auth.TicketService
	games   map[string]GameInfo
	n       int
}

func (h *fakeHost) Create(_ context.Context, spec GameSpec) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	id := fmt.Sprintf("g%d", h.n)
	h.games[id] = GameInfo{ID: id, Name: spec.Name, OwnerID: spec.OwnerID, Mode: spec.Mode, Map: spec.Map,
		Pakset: spec.Pakset, Public: spec.Public, MaxPlayers: spec.MaxPlayers, StartedAt: time.Now()}
	return id, nil
}
func (h *fakeHost) List(context.Context) ([]GameInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []GameInfo
	for _, g := range h.games {
		out = append(out, g)
	}
	return out, nil
}
func (h *fakeHost) Get(_ context.Context, id string) (GameInfo, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g, ok := h.games[id]
	if !ok {
		return g, ErrGameNotFound
	}
	return g, nil
}
func (h *fakeHost) Join(_ context.Context, id string, p Player) (string, string, error) {
	if _, err := h.Get(context.Background(), id); err != nil {
		return "", "", err
	}
	tk, err := h.tickets.Issue(p.UserID, p.DisplayName, id)
	if err != nil {
		return "", "", err
	}
	return tk.Token, "/ws/v1/games/" + id + "?ticket=" + tk.Token, nil
}
func (h *fakeHost) Stop(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.games[id]; !ok {
		return ErrGameNotFound
	}
	delete(h.games, id)
	return nil
}

type env struct {
	t       *testing.T
	srv     *httptest.Server
	deps    Deps
	host    *fakeHost
	hasDemo bool
}

var fastParams = auth.Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func newEnv(t *testing.T, repo db.Repo) *env {
	t.Helper()
	store, err := blob.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.CookieSecure = false
	cfg.MaxUploadBytes = 1 << 20
	cfg.CORSOrigins = []string{"http://localhost:3000"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cat := NewCatalog(repo, store, log, 1, "")
	t.Cleanup(cat.Close)
	tickets := auth.NewTickets(0)
	host := &fakeHost{tickets: tickets, games: map[string]GameInfo{}}
	d := Deps{Config: cfg, Log: log, Repo: repo, Store: store, Auth: auth.NewServiceWithParams(repo, time.Hour, fastParams),
		Tickets: tickets, Catalog: cat, Host: host, Metrics: NewRegistry()}
	e := &env{t: t, deps: d, host: host}
	if path := testutil.DemoPakPath(); fileExists(path) {
		if _, err := cat.EnsureDemo(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		// idempotent
		if _, err := cat.EnsureDemo(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		e.hasDemo = true
	}
	e.srv = httptest.NewServer(NewRouter(d))
	t.Cleanup(e.srv.Close)
	return e
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

type client struct {
	e *env
	c *http.Client
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, c: &http.Client{Jar: jar}}
}

func (c *client) do(method, path string, body any, hdr ...string) *http.Response {
	c.e.t.Helper()
	var r io.Reader
	ct := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		j, _ := json.Marshal(b)
		r = bytes.NewReader(j)
		ct = "application/json"
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, r)
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.c.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	return resp
}

func decode[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var v T
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("decode %s: %v (%s)", resp.Request.URL.Path, err, b)
	}
	return v
}

func expect(t *testing.T, resp *http.Response, code int) []byte {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != code {
		t.Fatalf("%s %s: status %d, want %d: %s", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode, code, b)
	}
	return b
}

func (c *client) register(email string) db.User {
	c.e.t.Helper()
	resp := c.do("POST", "/api/v1/auth/register", map[string]string{"email": email, "password": "password123", "displayName": strings.Split(email, "@")[0]})
	if resp.StatusCode != http.StatusCreated {
		expect(c.e.t, resp, http.StatusCreated)
	}
	return decode[meResponse](c.e.t, resp).User
}

// buildPak assembles a pak file.
func buildPak(files map[string][]byte, order []string) []byte {
	data := make([]byte, 12)
	type ent struct {
		name     string
		pos, len int
	}
	var ents []ent
	for _, n := range order {
		ents = append(ents, ent{n, len(data), len(files[n])})
		data = append(data, files[n]...)
	}
	dirofs := len(data)
	for _, e := range ents {
		var d [64]byte
		copy(d[:56], e.name)
		binary.LittleEndian.PutUint32(d[56:], uint32(e.pos))
		binary.LittleEndian.PutUint32(d[60:], uint32(e.len))
		data = append(data, d[:]...)
	}
	copy(data, "PACK")
	binary.LittleEndian.PutUint32(data[4:], uint32(dirofs))
	binary.LittleEndian.PutUint32(data[8:], uint32(len(ents)*64))
	return data
}

func (c *client) upload(name string, data []byte) *http.Response {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("note", "ignored")
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	return c.do("POST", "/api/v1/paks", buf.Bytes(), "Content-Type", mw.FormDataContentType())
}

func runAPISuite(t *testing.T, e *env) {
	ctx := context.Background()
	anon := e.client()

	// health
	expect(t, anon.do("GET", "/healthz", nil), 200)
	expect(t, anon.do("GET", "/readyz", nil), 200)
	if b := expect(t, anon.do("GET", "/metrics", nil), 200); !bytes.Contains(b, []byte("q2_http_requests_total")) {
		t.Fatal("metrics missing")
	}
	expect(t, anon.do("GET", "/api/v1/nope", nil), 404)

	// auth
	expect(t, anon.do("GET", "/api/v1/auth/me", nil), 401)
	alice := e.client()
	u := alice.register("alice@example.com")
	if me := decode[meResponse](t, alice.do("GET", "/api/v1/auth/me", nil)); me.User.ID != u.ID || me.User.Email != "alice@example.com" {
		t.Fatalf("%+v", me)
	}
	expect(t, alice.do("POST", "/api/v1/auth/register", map[string]string{"email": "alice@example.com", "password": "password123"}), 409)
	expect(t, alice.do("POST", "/api/v1/auth/register", map[string]string{"email": "bad", "password": "password123"}), 400)
	expect(t, anon.do("POST", "/api/v1/auth/login", []byte(`{"email":"a","password":"b"}`), "Content-Type", "text/plain"), 415)
	expect(t, anon.do("POST", "/api/v1/auth/login", map[string]any{"email": "alice@example.com", "password": "password123", "x": 1}), 400)
	// CSRF: cross-origin state change rejected, dev origin allowed
	expect(t, anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "alice@example.com", "password": "password123"}, "Origin", "https://evil.example"), 403)
	resp := anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "alice@example.com", "password": "password123"}, "Origin", "http://localhost:3000")
	expect(t, resp, 200)
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" || resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("CORS headers missing")
	}
	var sc *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			sc = c
		}
	}
	if sc == nil || !sc.HttpOnly || sc.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie %+v", sc)
	}
	expect(t, anon.do("POST", "/api/v1/auth/logout", nil), 204)
	expect(t, anon.do("GET", "/api/v1/auth/me", nil), 401)
	expect(t, anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "alice@example.com", "password": "wrong"}), 401)
	// preflight
	resp = anon.do("OPTIONS", "/api/v1/auth/login", nil, "Origin", "http://localhost:3000", "Access-Control-Request-Method", "POST")
	expect(t, resp, 204)

	bob := e.client()
	bobUser := bob.register("bob@example.com")

	var demoIdx manifest.Index
	var demoPakID int64
	if e.hasDemo {
		resp := anon.do("GET", "/api/v1/paksets/demo/index", nil)
		etag := resp.Header.Get("ETag")
		if resp.Header.Get("Cache-Control") != "public, no-cache" || etag == "" {
			t.Fatal(resp.Header)
		}
		demoIdx = decode[manifest.Index](t, resp)
		if len(demoIdx.Files) != 1106 || len(demoIdx.Maps) != 3 || demoIdx.Palette == nil || demoIdx.Schema != manifest.Schema {
			t.Fatalf("index: %d files %d maps", len(demoIdx.Files), len(demoIdx.Maps))
		}
		demoPakID = demoIdx.Paks[0].ID
		expect(t, anon.do("GET", "/api/v1/paksets/demo/index", nil, "If-None-Match", etag), 304)

		maps := decode[struct {
			Maps []MapSummary `json:"maps"`
		}](t, anon.do("GET", "/api/v1/maps?pakset=demo", nil))
		want := map[string]uint32{"demo1": 3218560851, "demo2": 180827219, "demo3": 2948950693}
		if len(maps.Maps) != 3 {
			t.Fatal(maps)
		}
		for _, m := range maps.Maps {
			if m.Checksum != want[m.Name] {
				t.Errorf("%s: %d", m.Name, m.Checksum)
			}
		}
		mm := decode[MapManifest](t, anon.do("GET", "/api/v1/maps/demo1/manifest", nil))
		if mm.Map.Name != "demo1" || len(mm.Files) < 100 || mm.Files["maps/demo1.bsp"] == nil || mm.Files["env/unit1_rt.tga"] == nil {
			t.Fatalf("map manifest: %d files, missing %v", len(mm.Files), mm.Missing)
		}
		expect(t, anon.do("GET", "/api/v1/maps/nope/manifest", nil), 404)

		// asset serving
		cm := demoIdx.Files["pics/colormap.pcx"]
		resp = anon.do("GET", "/assets/"+cm.SHA256, nil)
		b := expect(t, resp, 200)
		if blob.Sum(b) != cm.SHA256 || resp.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
			resp.Header.Get("ETag") != `"`+cm.SHA256+`"` || resp.Header.Get("Accept-Ranges") != "bytes" {
			t.Fatalf("asset headers %v", resp.Header)
		}
		resp = anon.do("GET", "/assets/"+cm.SHA256, nil, "Range", "bytes=0-3")
		if b := expect(t, resp, 206); len(b) != 4 || b[0] != 0x0a || resp.Header.Get("Content-Range") == "" {
			t.Fatalf("range: %v", b)
		}
		expect(t, anon.do("GET", "/assets/"+cm.SHA256, nil, "If-None-Match", `"`+cm.SHA256+`"`), 304)
		png := expect(t, anon.do("GET", "/assets/"+cm.PNG.SHA256, nil), 200)
		if !bytes.HasPrefix(png, []byte("\x89PNG")) {
			t.Fatal("not a png")
		}
		pal := expect(t, anon.do("GET", "/assets/"+demoIdx.Palette.SHA256, nil), 200)
		if len(pal) != 768 {
			t.Fatal(len(pal))
		}
		wav := demoIdx.Files["sound/world/amb1.wav"]
		if resp := anon.do("HEAD", "/assets/"+wav.SHA256, nil); resp.Header.Get("Content-Type") != "audio/wav" {
			t.Fatal(resp.Header.Get("Content-Type"))
		}
	}
	expect(t, anon.do("GET", "/assets/"+strings.Repeat("0", 64), nil), 404)
	expect(t, anon.do("GET", "/assets/xyz", nil), 404)

	// upload a private pak
	secret := []byte("secret sound data " + time.Now().String())
	files := map[string][]byte{"sound/secret.wav": secret, "pics/colormap.pcx": []byte("broken pcx"), "sound/world/amb1.wav": []byte("override")}
	pk := buildPak(files, []string{"sound/secret.wav", "pics/colormap.pcx", "sound/world/amb1.wav"})
	expect(t, anon.upload("pak1.pak", pk), 401)
	expect(t, alice.upload("bad.pak", []byte("not a pak at all")), 400)
	expect(t, alice.upload("big.pak", make([]byte, 2<<20)), 413)
	expect(t, alice.do("POST", "/api/v1/paks", []byte(`{}`), "Content-Type", "application/json"), 415)
	resp = alice.upload("pak1.pak", pk)
	up := decode[pakResponse](t, resp)
	if resp.StatusCode != 202 || up.Job == nil || up.Pak.SHA256 != blob.Sum(pk) {
		t.Fatalf("upload: %d %+v", resp.StatusCode, up)
	}
	var job db.Job
	for i := 0; i < 200; i++ {
		job = decode[struct {
			Job db.Job `json:"job"`
		}](t, alice.do("GET", fmt.Sprintf("/api/v1/jobs/%d", up.Job.ID), nil)).Job
		if job.Status == db.JobDone || job.Status == db.JobFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != db.JobDone {
		t.Fatalf("job %+v", job)
	}
	expect(t, bob.do("GET", fmt.Sprintf("/api/v1/jobs/%d", up.Job.ID), nil), 404)
	pinfo := decode[struct {
		Pak     db.Pak        `json:"pak"`
		Owned   bool          `json:"owned"`
		Entries []db.PakEntry `json:"entries"`
	}](t, alice.do("GET", fmt.Sprintf("/api/v1/paks/%d?entries=1", up.Pak.ID), nil))
	if pinfo.Pak.Status != db.PakReady || !pinfo.Owned || len(pinfo.Entries) != 3 || pinfo.Entries[0].SHA256 != blob.Sum(secret) {
		t.Fatalf("%+v", pinfo)
	}
	expect(t, bob.do("GET", fmt.Sprintf("/api/v1/paks/%d", up.Pak.ID), nil), 404)
	secretSHA := blob.Sum(secret)
	expect(t, anon.do("GET", "/assets/"+secretSHA, nil), 401)
	expect(t, bob.do("GET", "/assets/"+secretSHA, nil), 403)
	resp = alice.do("GET", "/assets/"+secretSHA, nil)
	if b := expect(t, resp, 200); !bytes.Equal(b, secret) || !strings.HasPrefix(resp.Header.Get("Cache-Control"), "private,") {
		t.Fatalf("owner asset: %q %v", b, resp.Header)
	}
	if len(decode[struct {
		Paks []db.Pak `json:"paks"`
	}](t, bob.do("GET", "/api/v1/paks", nil)).Paks) != map[bool]int{true: 1, false: 0}[e.hasDemo] {
		t.Fatal("bob sees alice's pak")
	}

	// paksets
	ids := []int64{up.Pak.ID}
	if e.hasDemo {
		ids = []int64{demoPakID, up.Pak.ID}
	}
	expect(t, bob.do("POST", "/api/v1/paksets", map[string]any{"name": "x", "paks": []int64{up.Pak.ID}}), 400)
	expect(t, alice.do("POST", "/api/v1/paksets", map[string]any{"name": "x", "paks": ids, "public": true}), 400)
	ps := decode[struct {
		Pakset db.Pakset `json:"pakset"`
	}](t, alice.do("POST", "/api/v1/paksets", map[string]any{"name": "Mine", "paks": ids})).Pakset
	if ps.ID == "" || len(ps.PakIDs) != len(ids) || ps.OwnerID != u.ID {
		t.Fatalf("%+v", ps)
	}
	expect(t, bob.do("GET", "/api/v1/paksets/"+ps.ID, nil), 404)
	expect(t, bob.do("GET", "/api/v1/paksets/"+ps.ID+"/index", nil), 404)
	resp = alice.do("GET", "/api/v1/paksets/"+ps.ID+"/index", nil)
	if resp.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatal(resp.Header)
	}
	idx := decode[manifest.Index](t, resp)
	if e := idx.Lookup("sound/secret.wav"); e == nil || e.SHA256 != secretSHA {
		t.Fatal("secret missing from index")
	}
	if ov := idx.Lookup("sound/world/amb1.wav"); ov == nil || ov.SHA256 != blob.Sum([]byte("override")) || ov.Pak != len(ids)-1 {
		t.Fatalf("override not applied: %+v", ov)
	}
	if e.hasDemo {
		if len(idx.Files) != 1107 || len(idx.Maps) != 3 {
			t.Fatalf("merged index %d files", len(idx.Files))
		}
		// pak1's colormap is broken, so no palette entry comes from it: the
		// winning colormap entry has no palette → index palette absent
		if idx.Palette != nil {
			t.Fatal("palette from a pak without a valid colormap")
		}
		// the private pak's entries were converted with the demo palette
		// (fallback), but its colormap is not an image
		if cm := idx.Lookup("pics/colormap.pcx"); cm.Error == "" {
			t.Fatal("broken pcx not flagged")
		}
	}
	lists := decode[struct {
		Paksets []db.Pakset `json:"paksets"`
	}](t, alice.do("GET", "/api/v1/paksets", nil))
	if len(lists.Paksets) != map[bool]int{true: 2, false: 1}[e.hasDemo] {
		t.Fatalf("%+v", lists)
	}
	expect(t, bob.do("PUT", "/api/v1/paksets/"+ps.ID, map[string]any{"name": "hack", "paks": ids}), 404)
	ps2 := decode[struct {
		Pakset db.Pakset `json:"pakset"`
	}](t, alice.do("PUT", "/api/v1/paksets/"+ps.ID, map[string]any{"name": "Renamed", "paks": []int64{up.Pak.ID}})).Pakset
	if ps2.Name != "Renamed" || len(ps2.PakIDs) != 1 {
		t.Fatal(ps2)
	}
	if idx := decode[manifest.Index](t, alice.do("GET", "/api/v1/paksets/"+ps.ID+"/index", nil)); len(idx.Files) != 3 {
		t.Fatal("index cache not invalidated", len(idx.Files))
	}
	if e.hasDemo {
		expect(t, alice.do("DELETE", "/api/v1/paksets/demo", nil), 403)
	}

	// bob uploads the same pak: he now owns it too (same content hash)
	resp = bob.upload("copy.pak", pk)
	if b := expect(t, resp, 200); !bytes.Contains(b, []byte(`"owned":true`)) {
		t.Fatal(string(b))
	}
	expect(t, bob.do("GET", "/assets/"+secretSHA, nil), 200)
	expect(t, alice.do("DELETE", "/api/v1/paksets/"+ps.ID, nil), 204)
	expect(t, alice.do("GET", "/api/v1/paksets/"+ps.ID, nil), 404)

	// saves (written by the game server through SaveStore)
	ss := db.NewSaveStore(e.deps.Repo)
	if err := ss.Put(ctx, u.ID, "save0", []byte("SAVEDATA"), db.SaveMeta{Comment: "Outer Base", MapCmd: "demo1", Mode: "sp", Schema: 1}); err != nil {
		t.Fatal(err)
	}
	saves := decode[struct {
		Saves []db.SaveInfo `json:"saves"`
	}](t, alice.do("GET", "/api/v1/saves", nil)).Saves
	if len(saves) != 1 || saves[0].Comment != "Outer Base" || saves[0].Size != 8 {
		t.Fatalf("%+v", saves)
	}
	expect(t, anon.do("GET", "/api/v1/saves", nil), 401)
	if got := decode[struct {
		Saves []db.SaveInfo `json:"saves"`
	}](t, bob.do("GET", "/api/v1/saves", nil)).Saves; len(got) != 0 {
		t.Fatal("bob sees alice's saves")
	}
	if s := decode[struct {
		Save db.SaveInfo `json:"save"`
	}](t, alice.do("GET", "/api/v1/saves/save0", nil)).Save; s.MapCmd != "demo1" {
		t.Fatal(s)
	}
	if b := expect(t, alice.do("GET", "/api/v1/saves/save0/data", nil), 200); string(b) != "SAVEDATA" {
		t.Fatal(string(b))
	}
	expect(t, bob.do("GET", "/api/v1/saves/save0", nil), 404)
	expect(t, alice.do("GET", "/api/v1/saves/..%2fx", nil), 404)
	expect(t, bob.do("DELETE", "/api/v1/saves/save0", nil), 404)
	expect(t, alice.do("DELETE", "/api/v1/saves/save0", nil), 204)
	expect(t, alice.do("GET", "/api/v1/saves/save0", nil), 404)

	// settings
	if s := decode[settingsBody](t, alice.do("GET", "/api/v1/settings", nil)); s.Config != "" || s.UpdatedAt != nil {
		t.Fatal(s)
	}
	cfg := "bind w \"+forward\"\nset name alice\n"
	if s := decode[settingsBody](t, alice.do("PUT", "/api/v1/settings", map[string]string{"config": cfg})); s.Config != cfg || s.UpdatedAt == nil {
		t.Fatal(s)
	}
	expect(t, alice.do("PUT", "/api/v1/settings", map[string]string{"config": strings.Repeat("x", MaxConfigBytes+1)}), 400)
	if s := decode[settingsBody](t, bob.do("GET", "/api/v1/settings", nil)); s.Config != "" {
		t.Fatal("settings leaked")
	}

	// games
	if e.hasDemo {
		expect(t, anon.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "demo1"}), 401)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "race", "map": "demo1"}), 400)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "base1"}), 400)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "../x"}), 400)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "demo1", "cvars": map[string]string{"x": "a;quit"}}), 400)
		g := decode[struct {
			Game GameInfo `json:"game"`
		}](t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "demo1", "public": true, "maxPlayers": 8,
			"cvars": map[string]string{"fraglimit": "10"}})).Game
		if g.ID == "" || g.OwnerID != u.ID || g.Pakset != "demo" {
			t.Fatalf("%+v", g)
		}
		priv := decode[struct {
			Game GameInfo `json:"game"`
		}](t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "sp", "map": "demo2"})).Game
		if gl := decode[struct {
			Games []GameInfo `json:"games"`
		}](t, bob.do("GET", "/api/v1/games", nil)).Games; len(gl) != 1 || gl[0].ID != g.ID {
			t.Fatalf("bob sees %+v", gl)
		}
		expect(t, bob.do("GET", "/api/v1/games/"+priv.ID, nil), 404)
		expect(t, alice.do("GET", "/api/v1/games/"+priv.ID, nil), 200)
		expect(t, anon.do("POST", "/api/v1/games/"+g.ID+"/join", nil), 401)
		jr := decode[JoinResponse](t, bob.do("POST", "/api/v1/games/"+g.ID+"/join", nil))
		if jr.Ticket == "" || !strings.Contains(jr.WSURL, g.ID) {
			t.Fatal(jr)
		}
		tk, err := e.deps.Tickets.Redeem(jr.Ticket, g.ID)
		if err != nil || tk.UserID != bobUser.ID || tk.DisplayName != "bob" {
			t.Fatal(tk, err)
		}
		expect(t, bob.do("DELETE", "/api/v1/games/"+g.ID, nil), 403)
		expect(t, alice.do("DELETE", "/api/v1/games/"+g.ID, nil), 204)
		expect(t, alice.do("GET", "/api/v1/games/"+g.ID, nil), 404)
		if gs, _ := e.deps.Repo.ListGames(ctx, false); len(gs) != 2 {
			t.Fatalf("registry: %+v", gs)
		}
		if gs, _ := e.deps.Repo.ListGames(ctx, true); len(gs) != 1 {
			t.Fatalf("active registry: %+v", gs)
		}

		// start from a save
		bundle := db.EncodeBundle(map[string][]byte{"server.ssv": []byte(`{}`), "game.ssv": []byte("g"),
			SaveOriginFile: []byte(`{"pakset":"demo","mode":"sp"}`)})
		if err := e.deps.Repo.PutSave(ctx, u.ID, "s1", bundle, db.SaveMeta{Comment: "c", MapCmd: "*demo2$start", Mode: "sp"}); err != nil {
			t.Fatal(err)
		}
		expect(t, anon.do("POST", "/api/v1/games/from-save", map[string]string{"slot": "s1"}), 401)
		expect(t, alice.do("POST", "/api/v1/games/from-save", map[string]string{"slot": "../x"}), 400)
		expect(t, alice.do("POST", "/api/v1/games/from-save", map[string]string{"slot": "nope"}), 404)
		expect(t, bob.do("POST", "/api/v1/games/from-save", map[string]string{"slot": "s1"}), 404)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "demo1", "loadSlot": "s1"}), 400)
		expect(t, alice.do("POST", "/api/v1/games", map[string]any{"mode": "sp", "map": "demo1", "loadSlot": "nope"}), 404)
		fs := decode[struct {
			Game GameInfo `json:"game"`
		}](t, alice.do("POST", "/api/v1/games/from-save", map[string]string{"slot": "s1"})).Game
		if fs.Mode != "sp" || fs.Map != "demo2" || fs.Pakset != "demo" || fs.OwnerID != u.ID {
			t.Fatalf("from-save: %+v", fs)
		}
	}

	// login rate limit (per email)
	for i := 0; i < 10; i++ {
		anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "x"}).Body.Close()
	}
	resp = anon.do("POST", "/api/v1/auth/login", map[string]string{"email": "victim@example.com", "password": "x"})
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("rate limit: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestMapFromMapCmd(t *testing.T) {
	for in, want := range map[string]string{"demo1": "demo1", "*base2$spawn": "base2", "intro.cin+base1": "base1", "victory.pcx": ""} {
		if got := MapFromMapCmd(in); got != want {
			t.Errorf("MapFromMapCmd(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAPIMemory(t *testing.T) {
	runAPISuite(t, newEnv(t, db.NewMemory()))
}

func TestAPIPostgres(t *testing.T) {
	url := dbtest.URL(t)
	ctx := context.Background()
	pg, err := db.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.MigrateDownAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	runAPISuite(t, newEnv(t, pg))
}

func TestNoHost(t *testing.T) {
	e := newEnv(t, db.NewMemory())
	d := e.deps
	d.Host = nil
	srv := httptest.NewServer(NewRouter(d))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/v1/games")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatal(resp.StatusCode)
	}
}

func TestBootstrapMemory(t *testing.T) {
	cfg := config.Default()
	cfg.BlobDir = t.TempDir()
	cfg.DemoPak = testutil.DemoPakPath()
	if !fileExists(cfg.DemoPak) {
		cfg.DemoPak = ""
	}
	app, err := Bootstrap(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	srv := httptest.NewServer(NewRouter(app.Deps))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	if cfg.DemoPak != "" {
		resp, _ := http.Get(srv.URL + "/api/v1/paksets/demo/index")
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
	}
}

func TestRecoverer(t *testing.T) {
	s := &server{Deps: Deps{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	h := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatal(rec.Code)
	}
}
