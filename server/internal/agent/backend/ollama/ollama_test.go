package ollama

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
)

func testState() *decide.State {
	return &decide.State{
		Mode: "fight",
		Me:   decide.Me{HP: "ok", Health: 70, Armor: 10, Weapon: "shotgun", Ammo: "ok", OnGround: true, Weapons: []string{"blaster", "shotgun", "machinegun"}},
		Enemies: []decide.Enemy{
			{ID: "e1", Class: "soldier", Bearing: 20, Elev: -2, Dist: "mid", Units: 420, Visible: true, Shootable: true, Aim: "near", State: "attacking", Threat: "med"},
			{ID: "e2", Class: "gunner", Bearing: -60, Elev: 1, Dist: "far", Units: 900, Visible: true, Shootable: true, Aim: "off", State: "alert", Threat: "high"},
		},
		Items: []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Bearing: 90, Path: 300}},
		Level: &decide.LevelInfo{Map: "demo1"},
	}
}

func request(t *testing.T, seq uint64, lane decide.Lane) *decide.Request {
	t.Helper()
	req, err := decide.NewRequest(seq, lane, 1000, testState(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// fake is a fake Ollama /v1/systemone: it answers with the scripted
// policy (confidence 0.6, so a test can tell its probabilities from a
// one-hot answer), unless reply writes something else. It keeps every
// body and Authorization header.
type fake struct {
	srv    *httptest.Server
	calls  atomic.Int64
	active atomic.Int32
	peak   atomic.Int32
	mu     sync.Mutex
	bodies [][]byte
	auth   []string
	delay  time.Duration
	reply  func(w http.ResponseWriter, n int64) bool // true: replied
}

func newFake(t *testing.T) *fake {
	t.Helper()
	pol := jevtest.NewScripted(scripted.Config{Seed: 1})
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.calls.Add(1)
		a := f.active.Add(1)
		defer f.active.Add(-1)
		for p := f.peak.Load(); a > p && !f.peak.CompareAndSwap(p, a); p = f.peak.Load() {
		}
		data, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.bodies = append(f.bodies, data)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		if f.delay > 0 {
			select {
			case <-time.After(f.delay):
			case <-r.Context().Done():
				return
			}
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		if f.reply != nil && f.reply(w, n) {
			return
		}
		wr, err := decide.ParseRequestBody(data)
		if err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}
		rep, err := pol.Answer(&jevtest.Call{State: wr.State, Questions: wr.Questions})
		if err != nil {
			http.Error(w, `{"error":"policy"}`, http.StatusInternalServerError)
			return
		}
		for id, ans := range rep.Answers {
			if ans.Type != decide.Noul {
				ans.Confidence = 0.6
			}
			rep.Answers[id] = ans
		}
		raw, _ := decide.MarshalResponse(wr.Model, wr.Questions, rep.Answers, decide.Usage{InputTokens: 841, OutputTokens: 0})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func client(t *testing.T, base string, edit func(*Config)) *Client {
	t.Helper()
	cfg := Config{BaseURL: base, Model: "nimble-v3", Logf: t.Logf}
	if edit != nil {
		edit(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"":                           "http://127.0.0.1:11434",
		"127.0.0.1:11434":            "http://127.0.0.1:11434",
		"localhost":                  "http://localhost:11434",
		"http://gpu-box:8080/":       "http://gpu-box:8080",
		"http://h:1/proxy/ollama":    "http://h:1/proxy/ollama",
		"https://ollama.example.org": "https://ollama.example.org",
		" [::1]:11434 ":              "http://[::1]:11434",
	} {
		got, err := BaseURL(in)
		if err != nil || got != want {
			t.Errorf("BaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ftp://h", "http://user:pw@h", "http://h/?q=1", "http://"} {
		if _, err := BaseURL(bad); !errors.Is(err, ErrBaseURL) {
			t.Errorf("BaseURL(%q): %v", bad, err)
		}
	}
	c, err := New(Config{})
	if err != nil || c.Model() != DefaultModel || c.Name() != "ollama" || c.Timeout() != DefaultTimeout ||
		c.Endpoint() != "http://127.0.0.1:11434/v1/systemone" {
		t.Fatalf("defaults: %v", err)
	}
	// a test binary never leaves loopback (the jev client's rule)
	if _, err := New(Config{BaseURL: "http://10.1.2.3:11434"}); !errors.Is(err, jev.ErrTestNetwork) {
		t.Fatalf("non-loopback in a test: %v", err)
	}
	env := map[string]string{EnvHost: "10.0.0.2:11434", EnvModel: " qwen-test "}
	if cfg := FromEnv(func(k string) string { return env[k] }); cfg.BaseURL != "10.0.0.2:11434" || cfg.Model != "qwen-test" {
		t.Fatalf("FromEnv %+v", cfg)
	}
}

// A call: jev's request body to /v1/systemone without any key, jev's
// answers decoded with their probabilities and confidence (not one-hot),
// tokens counted, cost 0, under the name "ollama".
func TestDecide(t *testing.T) {
	t.Setenv(jev.EnvAPIKey, "sk-must-never-reach-ollama")
	f := newFake(t)
	c := client(t, f.srv.URL, nil)
	for i, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
		req := request(t, uint64(i+1), lane)
		resp, err := c.Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := decide.RequestBody("nimble-v3", req)
		if string(f.bodies[i]) != string(want) || f.auth[i] != "" {
			t.Fatalf("body %s (want %s), Authorization %q", f.bodies[i], want, f.auth[i])
		}
		if resp.Seq != req.Seq || resp.Model != "nimble-v3" || resp.CostUSD != 0 || resp.Usage.InputTokens != 841 || resp.Status != http.StatusOK ||
			resp.Retries != 0 || len(resp.Answers) != len(req.Questions) {
			t.Fatalf("response %+v", resp)
		}
		for id, a := range resp.Answers {
			if a.Type != decide.Noul && (!a.HasConfidence || a.Confidence != 0.6) {
				t.Fatalf("%s: %+v, want the server's confidence", id, a)
			}
		}
		dec, err := decide.DecodeResponse(resp.Raw, req.Questions)
		if err != nil || !reflect.DeepEqual(dec.Answers, resp.Answers) {
			t.Fatalf("raw %s: %v", resp.Raw, err)
		}
	}
	st := c.Stats()
	if st.Requests != 2 || st.OK != 2 || st.InputTokens != 2*841 || st.CostUSD != 0 || st.Model != "nimble-v3" {
		t.Fatalf("stats %+v", st)
	}
	t.Log(st.Summary())
}

// One attempt per request, and none of the jev client's wall-clock
// guards: failures repeat as the server answers them, never refused
// locally (no breaker, bad-set refusal or cooldown), and errors carry the
// backend's name.
func TestDecideNoGuards(t *testing.T) {
	for _, tc := range []struct {
		status int
		class  jev.ErrorClass
	}{
		{http.StatusInternalServerError, jev.ClassServer},
		{http.StatusBadRequest, jev.ClassBadRequest},
		{http.StatusTooManyRequests, jev.ClassRateLimit},
	} {
		f := newFake(t)
		f.reply = func(w http.ResponseWriter, _ int64) bool {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"nope"}`, tc.status)
			return true
		}
		c := client(t, f.srv.URL, nil)
		req := request(t, 1, decide.LaneSlow)
		for i := 0; i < 7; i++ {
			_, err := c.Decide(context.Background(), req)
			if jev.ClassOf(err) != tc.class || !strings.HasPrefix(err.Error(), "ollama: "+string(tc.class)) {
				t.Fatalf("status %d call %d: %v", tc.status, i, err)
			}
		}
		if n := f.calls.Load(); n != 7 {
			t.Fatalf("status %d: %d server calls for 7 requests (want one attempt each, none refused)", tc.status, n)
		}
	}
	// a 404 (model not pulled) is reported, not retried
	f := newFake(t)
	_, err := client(t, strings.TrimSuffix(f.srv.URL, "/")+"/nowhere", nil).Decide(context.Background(), request(t, 1, decide.LaneFast))
	var e *jev.Error
	if !errors.As(err, &e) || e.Status != http.StatusNotFound || e.HTTPStatus() != http.StatusNotFound || f.calls.Load() != 1 {
		t.Fatalf("404: %v", err)
	}
}

// No 800 ms fast-lane cap: a fast call of a second is answered. A call
// slower than Timeout is a timeout the scheduler recognizes; a caller's
// cancel a cancel.
func TestDecideTimeout(t *testing.T) {
	f := newFake(t)
	f.delay = jev.MaxFastTimeout + 200*time.Millisecond
	req := request(t, 1, decide.LaneFast)
	if _, err := client(t, f.srv.URL, func(c *Config) { c.Timeout = 5 * time.Second }).Decide(context.Background(), req); err != nil {
		t.Fatalf("a fast call of %v: %v", f.delay, err)
	}
	c := client(t, f.srv.URL, func(c *Config) { c.Timeout = 100 * time.Millisecond })
	_, err := c.Decide(context.Background(), req)
	if jev.ClassOf(err) != jev.ClassTimeout || !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "ollama: timeout") {
		t.Fatalf("timeout: %v", err)
	}
	if st := c.Stats(); st.Timeouts != 1 {
		t.Fatalf("stats %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	if _, err := client(t, f.srv.URL, nil).Decide(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	if _, err := client(t, url, nil).Decide(context.Background(), req); jev.ClassOf(err) != jev.ClassTransport {
		t.Fatalf("no server: %v", err)
	}
}

// Calls are sent one at a time, and Timeout counts from the send: three
// calls of 150 ms each all answer under a 300 ms timeout although the
// last one waits 300 ms for its turn; a caller's deadline still ends the
// wait.
func TestDecideQueueNotTimed(t *testing.T) {
	f := newFake(t)
	f.delay = 150 * time.Millisecond
	c := client(t, f.srv.URL, func(c *Config) { c.Timeout = 300 * time.Millisecond })
	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.Decide(context.Background(), request(t, uint64(i+1), decide.LaneFast))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if p := f.peak.Load(); p != 1 {
		t.Fatalf("%d calls at once, want 1", p)
	}
	if st := c.Stats(); st.OK != 3 || st.WaitP95 < 200*time.Millisecond {
		t.Fatalf("stats %+v", st)
	}
	// a turn that never comes within the caller's deadline
	f.delay = time.Second
	go func() { _, _ = c.Decide(context.Background(), request(t, 9, decide.LaneFast)) }()
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Decide(ctx, request(t, 10, decide.LaneFast)); jev.ClassOf(err) != jev.ClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting past the deadline: %v", err)
	}
}
