package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/db"
)

// fakeBots is a BotHost recording its calls; err, when set, is returned
// by every call.
type fakeBots struct {
	mu    sync.Mutex
	err   error
	calls []string
	users []BotUser
	spec  BotSpec
	files map[string][]byte
}

func (f *fakeBots) record(call string, u BotUser) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
	f.users = append(f.users, u)
	return f.err
}

func (f *fakeBots) Start(_ context.Context, spec BotSpec, u BotUser) (BotInfo, error) {
	f.mu.Lock()
	f.spec = spec
	f.mu.Unlock()
	if err := f.record("start", u); err != nil {
		return BotInfo{}, err
	}
	return BotInfo{ID: "b1", Name: spec.Name, OwnerID: u.ID, Status: BotStarting, Backend: spec.Backend, Maps: spec.Maps,
		StartedAt: time.Unix(1700000000, 0).UTC()}, nil
}

func (f *fakeBots) List(_ context.Context, u BotUser) ([]BotInfo, error) {
	if err := f.record("list", u); err != nil {
		return nil, err
	}
	return nil, nil
}

func (f *fakeBots) Get(_ context.Context, id string, u BotUser) (BotInfo, error) {
	if err := f.record("get "+id, u); err != nil {
		return BotInfo{}, err
	}
	return BotInfo{ID: id, Status: BotFinished, Summary: []byte(`{"schema":"q2bot.run/1"}`)}, nil
}

func (f *fakeBots) Stop(_ context.Context, id string, u BotUser) error {
	return f.record("stop "+id, u)
}

func (f *fakeBots) Watch(_ context.Context, id string, u BotUser) (BotWatch, error) {
	if err := f.record("watch "+id, u); err != nil {
		return BotWatch{}, err
	}
	return BotWatch{Ticket: "t1", WSURL: "/ws/v1/bots/" + id + "/watch?ticket=t1", DecisionsTicket: "t2",
		DecisionsURL: "/ws/v1/bots/" + id + "/decisions?ticket=t2", Pakset: DemoPaksetID}, nil
}

type readSeekNopCloser struct{ *bytes.Reader }

func (readSeekNopCloser) Close() error { return nil }

func (f *fakeBots) OpenArtifact(_ context.Context, id, name string, u BotUser) (*BotArtifact, error) {
	if err := f.record("artifact "+id+" "+name, u); err != nil {
		return nil, err
	}
	b, ok := f.files[name]
	if !ok {
		return nil, ErrBotNotFound
	}
	a := &BotArtifact{Name: name, Size: int64(len(b)), ModTime: time.Unix(1700000000, 0), Content: readSeekNopCloser{bytes.NewReader(b)}}
	switch {
	case strings.HasSuffix(name, ".dm2"):
		a.Kind, a.ContentType = ArtifactDemo, "application/octet-stream"
	case strings.HasSuffix(name, ".jsonl.gz"):
		a.Kind, a.ContentType, a.ContentEncoding = ArtifactTrace, "application/x-ndjson", "gzip"
	default:
		a.Kind, a.ContentType = ArtifactRun, "application/json"
	}
	return a, nil
}

func (f *fakeBots) last() (string, BotUser) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return "", BotUser{}
	}
	return f.calls[len(f.calls)-1], f.users[len(f.users)-1]
}

func newBotsEnv(t *testing.T, bots BotHost) (*env, *client, db.User) {
	t.Helper()
	repo := db.NewMemory()
	e := newEnvWith(t, repo, func(d *Deps) { d.Bots = bots })
	c := e.client()
	u := c.register("alice@example.com")
	return e, c, u
}

// newEnvWith is newEnv with the router's dependencies adjusted first.
func newEnvWith(t *testing.T, repo db.Repo, adjust func(*Deps)) *env {
	t.Helper()
	e := newEnv(t, repo)
	e.srv.Close()
	adjust(&e.deps)
	e.srv = newTestServer(t, NewRouter(e.deps))
	return e
}

func TestBotsRoutes(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte(`{"v":1,"type":"run_start"}` + "\n")) //nolint:errcheck
	zw.Close()
	fb := &fakeBots{files: map[string][]byte{
		"run.json":                  []byte(`{"schema":"q2bot.run/1"}`),
		"ep-000/trace.jsonl.gz":     gz.Bytes(),
		"ep-000/demos/00-demo1.dm2": {1, 2, 3, 4},
	}}
	e, alice, u := newBotsEnv(t, fb)
	anon := e.client()

	// starting needs an account; the spec reaches the host as sent
	expect(t, anon.do("POST", "/api/v1/bots", map[string]any{"backend": "scripted"}), http.StatusUnauthorized)
	skill := 2
	b := decode[struct{ Bot BotInfo }](t, alice.do("POST", "/api/v1/bots", BotSpec{Name: "x", Maps: []string{"demo1"},
		Skill: &skill, Backend: "scripted", SimLatency: "212ms", Public: true}))
	if b.Bot.ID != "b1" || b.Bot.OwnerID != u.ID || b.Bot.Status != BotStarting {
		t.Fatalf("created %+v", b.Bot)
	}
	if call, bu := fb.last(); call != "start" || bu.ID != u.ID || bu.Name != "alice" || bu.Admin {
		t.Fatalf("start call %q as %+v", call, bu)
	}
	if fb.spec.Skill == nil || *fb.spec.Skill != 2 || fb.spec.SimLatency != "212ms" || !fb.spec.Public {
		t.Fatalf("spec %+v", fb.spec)
	}
	// unknown fields are refused like everywhere else
	expect(t, alice.do("POST", "/api/v1/bots", map[string]any{"backend": "scripted", "gravity": 100}), http.StatusBadRequest)

	// listing and getting work anonymously (the host filters)
	l := decode[map[string]any](t, anon.do("GET", "/api/v1/bots", nil))
	if bots, ok := l["bots"].([]any); !ok || len(bots) != 0 {
		t.Fatalf("list %v (want an empty array, not null)", l)
	}
	if _, bu := fb.last(); bu.ID != 0 {
		t.Fatalf("anonymous list as %+v", bu)
	}
	g := decode[struct{ Bot BotInfo }](t, anon.do("GET", "/api/v1/bots/b9", nil))
	if g.Bot.ID != "b9" || string(g.Bot.Summary) != `{"schema":"q2bot.run/1"}` {
		t.Fatalf("get %+v", g.Bot)
	}

	// stopping needs an account
	expect(t, anon.do("DELETE", "/api/v1/bots/b1", nil), http.StatusUnauthorized)
	expect(t, alice.do("DELETE", "/api/v1/bots/b1", nil), http.StatusNoContent)
	if call, _ := fb.last(); call != "stop b1" {
		t.Fatal(call)
	}

	w := decode[BotWatch](t, alice.do("POST", "/api/v1/bots/b1/watch", nil))
	if w.Ticket != "t1" || w.DecisionsTicket != "t2" || w.Pakset != "demo" || !strings.Contains(w.DecisionsURL, "/decisions?ticket=t2") {
		t.Fatalf("watch %+v", w)
	}

	// artifacts: content types; a trace goes out gzip-encoded as stored
	resp := anon.do("GET", "/api/v1/bots/b1/artifacts/ep-000/demos/00-demo1.dm2", nil)
	body := expect(t, resp, http.StatusOK)
	if !bytes.Equal(body, []byte{1, 2, 3, 4}) || resp.Header.Get("Content-Type") != "application/octet-stream" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `filename="00-demo1.dm2"`) ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("demo %v %q", resp.Header, body)
	}
	if call, _ := fb.last(); call != "artifact b1 ep-000/demos/00-demo1.dm2" {
		t.Fatal(call)
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/api/v1/bots/b1/artifacts/ep-000/trace.jsonl.gz", nil)
	req.Header.Set("Accept-Encoding", "gzip") // a client that decodes itself sees the stored bytes
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw := expect(t, resp, http.StatusOK)
	if !bytes.Equal(raw, gz.Bytes()) || resp.Header.Get("Content-Encoding") != "gzip" ||
		resp.Header.Get("Content-Type") != "application/x-ndjson" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), `filename="trace.jsonl"`) {
		t.Fatalf("trace %v", resp.Header)
	}
	resp = anon.do("GET", "/api/v1/bots/b1/artifacts/ep-000/trace.jsonl.gz", nil) // the default client decodes
	if got := expect(t, resp, http.StatusOK); !strings.Contains(string(got), `"run_start"`) {
		t.Fatalf("decoded trace %q", got)
	}
	resp = anon.do("GET", "/api/v1/bots/b1/artifacts/run.json", nil)
	if got := expect(t, resp, http.StatusOK); string(got) != `{"schema":"q2bot.run/1"}` || resp.Header.Get("Content-Disposition") != "" {
		t.Fatalf("run.json %q %v", got, resp.Header)
	}
	expect(t, anon.do("GET", "/api/v1/bots/b1/artifacts/nope.txt", nil), http.StatusNotFound)
}

func TestBotsErrorMapping(t *testing.T) {
	fb := &fakeBots{}
	_, alice, _ := newBotsEnv(t, fb)
	for _, tc := range []struct {
		err  error
		code int
		key  string
	}{
		{ErrBotNotFound, http.StatusNotFound, "not_found"},
		{fmt.Errorf("%w: jev needs an administrator", ErrBotForbidden), http.StatusForbidden, "forbidden"},
		{fmt.Errorf("%w: 4 bots run", ErrBotLimit), http.StatusTooManyRequests, "too_many_bots"},
		{fmt.Errorf("%w: no key", ErrBotsDisabled), http.StatusServiceUnavailable, "bots_unavailable"},
		{fmt.Errorf("%w: map xyz", ErrBotInvalid), http.StatusBadRequest, "invalid_bot"},
		{ErrBotNotLive, http.StatusConflict, "not_live"},
		{io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal"},
	} {
		fb.mu.Lock()
		fb.err = tc.err
		fb.mu.Unlock()
		for _, r := range []struct{ method, path string }{
			{"POST", "/api/v1/bots"}, {"GET", "/api/v1/bots"}, {"GET", "/api/v1/bots/x"}, {"DELETE", "/api/v1/bots/x"},
			{"POST", "/api/v1/bots/x/watch"}, {"GET", "/api/v1/bots/x/artifacts/run.json"},
		} {
			var body any
			if r.path == "/api/v1/bots" && r.method == "POST" {
				body = BotSpec{Backend: "scripted"}
			}
			b := expect(t, alice.do(r.method, r.path, body), tc.code)
			if !strings.Contains(string(b), `"code":"`+tc.key+`"`) {
				t.Errorf("%v %s %s: %s", tc.err, r.method, r.path, b)
			}
			if tc.code != http.StatusInternalServerError && tc.err != ErrBotNotFound && !strings.Contains(string(b), tc.err.Error()) {
				t.Errorf("%s %s: message %s lacks %q", r.method, r.path, b, tc.err)
			}
		}
	}
}

// Without a BotHost (Q2_BOTS_ENABLED off) every bots endpoint is 503.
func TestBotsDisabled(t *testing.T) {
	_, alice, _ := newBotsEnv(t, nil)
	for _, r := range []struct{ method, path string }{
		{"POST", "/api/v1/bots"}, {"GET", "/api/v1/bots"}, {"GET", "/api/v1/bots/x"}, {"DELETE", "/api/v1/bots/x"},
		{"POST", "/api/v1/bots/x/watch"}, {"GET", "/api/v1/bots/x/artifacts/run.json"},
	} {
		var body any
		if r.method == "POST" && r.path == "/api/v1/bots" {
			body = BotSpec{Backend: "scripted"}
		}
		if b := expect(t, alice.do(r.method, r.path, body), http.StatusServiceUnavailable); !strings.Contains(string(b), "bots_disabled") {
			t.Errorf("%s %s: %s", r.method, r.path, b)
		}
	}
}

// A banned account cannot start bots.
func TestBotsBanned(t *testing.T) {
	fb := &fakeBots{}
	e, alice, u := newBotsEnv(t, fb)
	if _, err := e.deps.Repo.CreateBan(context.Background(), db.Ban{UserID: u.ID, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	expect(t, alice.do("POST", "/api/v1/bots", BotSpec{Backend: "scripted"}), http.StatusForbidden)
	if call, _ := fb.last(); call != "" {
		t.Fatalf("host called: %s", call)
	}
}

// POST /bots/{id}/watch issues tickets to anyone for a public bot: it is
// rate-limited per address for anonymous callers (30/min), per account
// for others, and refused to banned accounts and addresses.
func TestBotsWatchLimits(t *testing.T) {
	fb := &fakeBots{}
	e, alice, u := newBotsEnv(t, fb)
	anon := e.client()
	for i := 0; i < 30; i++ {
		expect(t, anon.do("POST", "/api/v1/bots/b1/watch", nil), http.StatusOK)
	}
	resp := anon.do("POST", "/api/v1/bots/b1/watch", nil)
	if b := expect(t, resp, http.StatusTooManyRequests); resp.Header.Get("Retry-After") == "" || !strings.Contains(string(b), "rate_limited") {
		t.Fatalf("31st anonymous watch: %v %s", resp.Header, b)
	}
	fb.mu.Lock()
	watches := 0
	for _, c := range fb.calls {
		if c == "watch b1" {
			watches++
		}
	}
	fb.mu.Unlock()
	if watches != 30 {
		t.Errorf("host asked for %d watches, want 30", watches)
	}
	// an account has an allowance of its own
	expect(t, alice.do("POST", "/api/v1/bots/b1/watch", nil), http.StatusOK)
	if _, err := e.deps.Repo.CreateBan(context.Background(), db.Ban{UserID: u.ID, Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	expect(t, alice.do("POST", "/api/v1/bots/b1/watch", nil), http.StatusForbidden)

	// a banned address gets no tickets, logged in or not
	fb2 := &fakeBots{}
	e2, bob, _ := newBotsEnv(t, fb2)
	if _, err := e2.deps.Repo.CreateBan(context.Background(), db.Ban{IP: "127.0.0.1", Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	expect(t, e2.client().do("POST", "/api/v1/bots/b1/watch", nil), http.StatusForbidden)
	expect(t, bob.do("POST", "/api/v1/bots/b1/watch", nil), http.StatusForbidden)
	if call, _ := fb2.last(); call != "" {
		t.Fatalf("host called: %s", call)
	}
}

func newTestServer(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}
