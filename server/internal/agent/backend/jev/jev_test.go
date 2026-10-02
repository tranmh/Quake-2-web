package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

const testKey = "sk-test-SECRET-0123456789abcdef"

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_700_000_000, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func server(t *testing.T, opt jevtest.Options) *jevtest.Server {
	t.Helper()
	opt.APIKey = testKey
	s := jevtest.NewServer(opt)
	t.Cleanup(s.Close)
	return s
}

func client(t *testing.T, base string, edit func(*Config)) *Client {
	t.Helper()
	cfg := Config{BaseURL: base, APIKey: trace.NewSecret(testKey), AllowCustomBase: true}
	if edit != nil {
		edit(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func testState() *decide.State {
	return &decide.State{
		Mode: "fight",
		Me:   decide.Me{HP: "ok", Health: 70, Armor: 10, Weapon: "shotgun", Ammo: "ok", OnGround: true, Weapons: []string{"blaster", "shotgun", "machinegun"}},
		Enemies: []decide.Enemy{
			{ID: "e1", Class: "soldier", Bearing: 20, Elev: -2, Dist: "mid", Units: 420, Visible: true, Shootable: true, Aim: "near", State: "attacking", Threat: "med"},
			{ID: "e2", Class: "gunner", Bearing: -60, Elev: 1, Dist: "far", Units: 900, Visible: true, Shootable: true, Aim: "off", State: "alert", Threat: "high"},
		},
		Space: &decide.Space{Front: "open", Back: "tight", Left: "open", Right: "blocked"},
		Items: []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Bearing: 90, Path: 300}},
		Level: &decide.LevelInfo{Map: "demo1"},
	}
}

func request(t *testing.T, seq uint64, lane decide.Lane) *decide.Request {
	t.Helper()
	req, err := decide.NewRequest(seq, lane, 1234, testState(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func scriptedServer(t *testing.T, faults jevtest.Faults) *jevtest.Server {
	pol := jevtest.NewScripted(scripted.Config{Seed: 1})
	pol.Now = func(*jevtest.Call) int64 { return 1234 }
	return server(t, jevtest.Options{Policy: pol, Faults: faults})
}

func TestRoundTrip(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{})
	c := client(t, srv.URL(), nil)
	sb := scripted.New(scripted.Config{Seed: 1})
	for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
		req := request(t, 3, lane)
		resp, err := c.Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := sb.Decide(context.Background(), req)
		for id, a := range want.Answers {
			if got := resp.Answers[id]; got.Choice != a.Choice || got.Score != a.Score || got.Confidence != 1 {
				t.Errorf("%s %s: %+v, want %+v", lane, id, got, a)
			}
		}
		if resp.Model != jevtest.DefaultModel || resp.Status != 200 || resp.Seq != 3 || resp.Usage.InputTokens == 0 || resp.Unknown != 0 {
			t.Fatalf("response %+v", resp)
		}
		if math.Abs(resp.CostUSD-float64(resp.Usage.InputTokens)*0.042/1e6) > 1e-15 {
			t.Fatalf("cost %v for %d tokens", resp.CostUSD, resp.Usage.InputTokens)
		}
	}
	calls := srv.Calls()
	if len(calls) != 2 {
		t.Fatalf("%d calls", len(calls))
	}
	call := calls[0]
	if call.Header.Get("Authorization") != "Bearer "+testKey || call.Header.Get("Content-Type") != "application/json" || call.Model != DefaultModel {
		t.Fatalf("headers %v model %s", call.Header, call.Model)
	}
	want, _ := decide.RequestBody(DefaultModel, request(t, 3, decide.LaneFast))
	if !bytes.Equal(call.Body, want) {
		t.Fatalf("body\n%s\nwant\n%s", call.Body, want)
	}
	golden := filepath.Join("testdata", "request_fast.golden.json")
	if os.Getenv("Q2_UPDATE_FIXTURES") == "1" {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(golden, append(call.Body, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (regenerate with Q2_UPDATE_FIXTURES=1)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(g), call.Body) {
		t.Fatalf("body differs from %s (regenerate with Q2_UPDATE_FIXTURES=1)", golden)
	}
	st := c.Stats()
	if st.Requests != 2 || st.Attempts != 2 || st.OK != 2 || st.Errors.Total() != 0 || st.LastModel != jevtest.DefaultModel || st.InputTokens == 0 {
		t.Fatalf("stats %+v", st)
	}
}

// TestKeyNeverLeaks: the key reaches the server's Authorization header and
// nothing else: not the recorder, the log or any error.
func TestKeyNeverLeaks(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultNone, jevtest.FaultServer, jevtest.FaultUnauthorized}})
	var rec, logs bytes.Buffer
	var mu sync.Mutex
	c := client(t, srv.URL(), func(cfg *Config) {
		cfg.Recorder = &rec
		cfg.Logf = func(f string, a ...any) { mu.Lock(); fmt.Fprintf(&logs, f+"\n", a...); mu.Unlock() }
	})
	var errs []string
	for i := 1; i <= 4; i++ {
		if _, err := c.Decide(context.Background(), request(t, uint64(i), decide.LaneFast)); err != nil {
			errs = append(errs, err.Error(), fmt.Sprintf("%+v %#v", err, err))
		}
	}
	// a transport error that echoes the URL
	dead := client(t, "http://127.0.0.1:1", func(cfg *Config) { cfg.Recorder = &rec; cfg.Logf = c.cfg.Logf })
	if _, err := dead.Decide(context.Background(), request(t, 9, decide.LaneSlow)); err == nil || ClassOf(err) != ClassTransport {
		t.Fatalf("dead server: %v", err)
	} else {
		errs = append(errs, err.Error())
	}
	stats := fmt.Sprintf("%+v %#v %+v", c.Stats(), c.cfg, c)
	for name, text := range map[string]string{"recorder": rec.String(), "log": logs.String(), "errors": strings.Join(errs, "\n"), "stats/config": stats} {
		if strings.Contains(text, testKey) || strings.Contains(text, "SECRET") {
			t.Errorf("the key leaked into the %s:\n%s", name, text)
		}
	}
	if !strings.Contains(rec.String(), `"Authorization":["[REDACTED]"]`) {
		t.Errorf("recorder without a redacted Authorization header:\n%s", rec.String())
	}
	lines := strings.Split(strings.TrimSpace(rec.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("%d recorded attempts, want 5", len(lines))
	}
	for _, l := range lines {
		var x map[string]any
		if err := json.Unmarshal([]byte(l), &x); err != nil {
			t.Fatalf("recorder line: %v", err)
		}
	}
	if n := strings.Count(logs.String(), "disabled"); n != 1 {
		t.Errorf("disable logged %d times:\n%s", n, logs.String())
	}
}

// TestKeyRedactedBeforeCut: an error body that echoes the key across the
// 200-byte cut leaves no prefix of it in the error.
func TestKeyRedactedBeforeCut(t *testing.T) {
	for _, pad := range []int{140, 145, 150, 160, 169} {
		body := `{"error":{"message":"` + strings.Repeat("x", pad) + ` invalid api key: ` + testKey + `"}}`
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(body))
		}))
		c := client(t, srv.URL, nil)
		_, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast))
		srv.Close()
		if err == nil || ClassOf(err) != ClassAuth {
			t.Fatalf("pad %d: %v", pad, err)
		}
		for n := 6; n <= len(testKey); n++ {
			if strings.Contains(err.Error(), testKey[:n]) {
				t.Fatalf("pad %d: a %d-byte prefix of the key leaked: %s", pad, n, err)
			}
		}
	}
}

// TestErrorCutAtRune: the cut error text stays valid UTF-8.
func TestErrorCutAtRune(t *testing.T) {
	body := strings.Repeat("x", 199) + strings.Repeat("é", 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := client(t, srv.URL, nil)
	_, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast))
	var e *Error
	if !errors.As(err, &e) || !utf8.ValidString(e.Msg) || e.Msg != strings.Repeat("x", 199) {
		t.Fatalf("error %v (msg %q)", err, e.Msg)
	}
}

func TestDecodeVariantsAndUnknowns(t *testing.T) {
	srv := server(t, jevtest.Options{Policy: jevtest.PolicyFunc(func(c *jevtest.Call) (*jevtest.Reply, error) {
		return &jevtest.Reply{Raw: json.RawMessage(`{"model":"jev-1.13.0","request_id":"r1","answers":[` +
			`{"id":"target","choice":"e2","probabilities":[["e1",0.25],["e2",0.75]],"confidence":"0.5"},` +
			`{"id":"fire_policy","choice":"suppress","confidence":0.9,"probabilities":{"suppress":1}},` +
			`{"id":"movement","answer":"strafe_left","confidence":0.8,"note":"x"}],` +
			`"usage":{"prompt_tokens":100,"completion_tokens":9}}`)}, nil
	})})
	c := client(t, srv.URL(), nil)
	resp, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Answers["target"].Choice != "e2" || resp.Answers["movement"].Choice != "strafe_left" || resp.Usage != (decide.Usage{InputTokens: 100, OutputTokens: 9}) || resp.Unknown != 2 {
		t.Fatalf("response %+v", resp)
	}
	if c.Stats().Unknown != 2 {
		t.Fatalf("stats unknown %d", c.Stats().Unknown)
	}
}

func TestCostAndBudget(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{})
	var calls []float64
	c := client(t, srv.URL(), func(cfg *Config) {
		cfg.Pricing = Pricing{InputPerMTok: 1000, OutputPerMTok: 2000}
		cfg.MaxCostUSD = 0.0001
		cfg.OnCost = func(call, total float64) { calls = append(calls, call, total) }
	})
	resp, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast))
	if err != nil {
		t.Fatal(err)
	}
	want := float64(resp.Usage.InputTokens)*1000/1e6 + float64(resp.Usage.OutputTokens)*2000/1e6
	if math.Abs(resp.CostUSD-want) > 1e-12 || len(calls) != 2 || calls[1] != resp.CostUSD {
		t.Fatalf("cost %v want %v, hook %v", resp.CostUSD, want, calls)
	}
	if _, err := c.Decide(context.Background(), request(t, 2, decide.LaneFast)); ClassOf(err) != ClassBudget {
		t.Fatalf("over budget: %v", err)
	}
	if srv.Count() != 1 || c.Stats().CostUSD != resp.CostUSD {
		t.Fatalf("server calls %d, stats %+v", srv.Count(), c.Stats())
	}
}

func TestRetries(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultServer, jevtest.FaultNone, jevtest.FaultServer, jevtest.FaultServer}})
	c := client(t, srv.URL(), nil)
	resp, err := c.Decide(context.Background(), request(t, 1, decide.LaneSlow))
	if err != nil || resp.Retries != 1 || srv.Count() != 2 {
		t.Fatalf("slow lane retried once: %v, retries %v, %d calls", err, resp, srv.Count())
	}
	if calls := srv.Calls(); calls[1].Attempt != 1 || calls[1].Digest != calls[0].Digest ||
		calls[0].Header.Get("X-Retry-Count") != "0" || calls[1].Header.Get("X-Retry-Count") != "1" {
		t.Fatalf("retry is not the same request with its attempt number: %+v", calls[1])
	}
	// the fast lane never retries
	if _, err := c.Decide(context.Background(), request(t, 2, decide.LaneFast)); ClassOf(err) != ClassServer || srv.Count() != 3 {
		t.Fatalf("fast lane: %v, %d calls", err, srv.Count())
	}
	if resp, err := c.Decide(context.Background(), request(t, 3, decide.LaneSlow)); err != nil || resp.Retries != 1 || srv.Count() != 5 {
		t.Fatalf("slow lane retried after a 500: %v (%d calls)", err, srv.Count())
	}
	st := c.Stats()
	if st.Retries != 2 || st.Errors.Server != 3 || st.OK != 2 {
		t.Fatalf("stats %+v", st)
	}
	// retries can be turned off
	srv2 := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultServer}})
	c2 := client(t, srv2.URL(), func(cfg *Config) { cfg.SlowRetries = -1 })
	if _, err := c2.Decide(context.Background(), request(t, 1, decide.LaneSlow)); ClassOf(err) != ClassServer || srv2.Count() != 1 {
		t.Fatalf("no retries: %v, %d calls", err, srv2.Count())
	}
}

func TestTimeouts(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultHang, jevtest.FaultHang}})
	c := client(t, srv.URL(), func(cfg *Config) {
		cfg.FastTimeout = 50 * time.Millisecond
		cfg.SlowRetries = -1
		cfg.SlowTimeout = 10 * time.Second
	})
	start := time.Now()
	if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast)); ClassOf(err) != ClassTimeout || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("attempt timeout: %v", err)
	}
	// the caller's deadline bounds a slow attempt
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := c.Decide(ctx, request(t, 2, decide.LaneSlow)); ClassOf(err) != ClassTimeout {
		t.Fatalf("caller deadline: %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("timeouts took %v", d)
	}
	if st := c.Stats(); st.Timeouts != 2 || st.Errors.Timeout != 2 {
		t.Fatalf("stats %+v", st)
	}
	// the fast lane's attempt is capped at 800 ms
	big := client(t, srv.URL(), func(cfg *Config) { cfg.FastTimeout = time.Minute })
	if big.cfg.FastTimeout != MaxFastTimeout || big.cfg.SlowTimeout != DefaultSlowTimeout {
		t.Fatalf("timeouts %v %v", big.cfg.FastTimeout, big.cfg.SlowTimeout)
	}
	// unless a lockstep run against a loopback server lifts the cap
	if u := client(t, srv.URL(), func(cfg *Config) { cfg.FastTimeout, cfg.UncapFast = time.Minute, true }); u.cfg.FastTimeout != time.Minute {
		t.Fatalf("uncapped fast timeout %v", u.cfg.FastTimeout)
	}
	if err := (&Error{Class: ClassCanceled}); !errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("a canceled call is not context.Canceled")
	}
}

// TestSlowRetryAfterTimeout: a slow attempt that hangs leaves room for its
// retry within the scheduler's slow-lane deadline.
func TestSlowRetryAfterTimeout(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultHang, jevtest.FaultNone}})
	c := client(t, srv.URL(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), decide.DefaultSlowTimeout)
	defer cancel()
	start := time.Now()
	resp, err := c.Decide(ctx, request(t, 1, decide.LaneSlow))
	if err != nil || resp.Retries != 1 || srv.Count() != 2 {
		t.Fatalf("retry after a hung attempt: %v, %+v, %d calls", err, resp, srv.Count())
	}
	if d := time.Since(start); d >= decide.DefaultSlowTimeout {
		t.Fatalf("took %v", d)
	}
	if st := c.Stats(); st.Timeouts != 1 || st.Retries != 1 || st.OK != 1 {
		t.Fatalf("stats %+v", st)
	}

	// the shares: the lane's timeout without a deadline or a retry left; a
	// share of the time left after the pauses; the lane's timeout again
	// when the share would be too short to be useful
	bg := context.Background()
	if d := c.attemptTimeout(bg, decide.LaneSlow, 0, 1); d != DefaultSlowTimeout {
		t.Fatalf("no deadline: %v", d)
	}
	long, cancel2 := context.WithTimeout(bg, 2500*time.Millisecond)
	defer cancel2()
	if d := c.attemptTimeout(long, decide.LaneSlow, 0, 0); d != DefaultSlowTimeout {
		t.Fatalf("no retry left: %v", d)
	}
	if d := c.attemptTimeout(long, decide.LaneSlow, 0, 1); d > 1175*time.Millisecond || d < 1100*time.Millisecond {
		t.Fatalf("share of 2.5 s: %v", d)
	}
	short, cancel3 := context.WithTimeout(bg, 300*time.Millisecond)
	defer cancel3()
	if d := c.attemptTimeout(short, decide.LaneSlow, 0, 1); d != DefaultSlowTimeout {
		t.Fatalf("share too short: %v", d)
	}
}

// TestRecorderKeepsBody: the recorded request is the body that was sent,
// byte for byte (no HTML escaping of <, > and &).
func TestRecorderKeepsBody(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{})
	var rec bytes.Buffer
	c := client(t, srv.URL(), func(cfg *Config) { cfg.Recorder = &rec })
	st := testState()
	st.Objective = &decide.Objective{Kind: "touch", Desc: "door -> lift & <exit>", Path: 300}
	req, err := decide.NewRequest(1, decide.LaneSlow, 1234, st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var x struct {
		Request  json.RawMessage `json:"request"`
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(rec.Bytes(), &x); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(x.Request, srv.Calls()[0].Body) || !bytes.Contains(x.Request, []byte("door -> lift & <exit>")) {
		t.Fatalf("recorded request differs from the body sent:\n%s\n%s", x.Request, srv.Calls()[0].Body)
	}
	if !json.Valid(x.Response) {
		t.Fatal("no recorded response")
	}
}

func TestCooldown(t *testing.T) {
	for _, tc := range []struct {
		fault jevtest.Fault
		class ErrorClass
	}{{jevtest.FaultRateLimit, ClassRateLimit}, {jevtest.FaultOverload, ClassOverloaded}} {
		t.Run(string(tc.class), func(t *testing.T) {
			clk := newClock()
			srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{tc.fault}, RetryAfter: 2 * time.Second})
			c := client(t, srv.URL(), func(cfg *Config) { cfg.Now = clk.Now })
			_, err := c.Decide(context.Background(), request(t, 1, decide.LaneSlow))
			var e *Error
			if !errors.As(err, &e) || e.Class != tc.class || e.RetryAfter != 2*time.Second {
				t.Fatalf("first call: %v", err)
			}
			if _, err := c.Decide(context.Background(), request(t, 2, decide.LaneSlow)); ClassOf(err) != ClassCooldown || srv.Count() != 1 {
				t.Fatalf("during the cooldown: %v (%d calls)", err, srv.Count())
			}
			clk.Advance(2100 * time.Millisecond)
			if _, err := c.Decide(context.Background(), request(t, 3, decide.LaneSlow)); err != nil || srv.Count() != 2 {
				t.Fatalf("after the cooldown: %v (%d calls)", err, srv.Count())
			}
		})
	}

	// without Retry-After: 1 s, doubling while 429s repeat
	clk := newClock()
	var n atomic.Int32
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer hs.Close()
	c := client(t, hs.URL, func(cfg *Config) { cfg.Now = clk.Now })
	var e *Error
	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast)); !errors.As(err, &e) || e.RetryAfter != want {
			t.Fatalf("cooldown %v, want %v", err, want)
		}
		clk.Advance(want)
	}
	if n.Load() != 3 {
		t.Fatalf("%d calls", n.Load())
	}
	if d, ok := retryAfter(http.Header{"Retry-After": {clk.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)}}, clk.Now()); !ok || d != 3*time.Second {
		t.Fatalf("HTTP date Retry-After: %v %v", d, ok)
	}
}

func TestCircuitBreaker(t *testing.T) {
	clk := newClock()
	srv := scriptedServer(t, jevtest.Faults{Server: 1})
	var logs atomic.Int32
	c := client(t, srv.URL(), func(cfg *Config) {
		cfg.Now = clk.Now
		cfg.Logf = func(string, ...any) { logs.Add(1) }
	})
	for i := 1; i <= 5; i++ {
		if _, err := c.Decide(context.Background(), request(t, uint64(i), decide.LaneFast)); ClassOf(err) != ClassServer {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if _, err := c.Decide(context.Background(), request(t, 6, decide.LaneFast)); ClassOf(err) != ClassBreaker || srv.Count() != 5 {
		t.Fatalf("breaker open: %v (%d calls)", err, srv.Count())
	}
	clk.Advance(5 * time.Second)
	// half open: one request goes out; it fails and the breaker reopens
	if _, err := c.Decide(context.Background(), request(t, 7, decide.LaneFast)); ClassOf(err) != ClassServer || srv.Count() != 6 {
		t.Fatalf("after 5 s: %v (%d calls)", err, srv.Count())
	}
	if _, err := c.Decide(context.Background(), request(t, 8, decide.LaneFast)); ClassOf(err) != ClassBreaker {
		t.Fatalf("reopened: %v", err)
	}
	if logs.Load() != 2 || c.Stats().Errors.Breaker != 2 {
		t.Fatalf("logs %d stats %+v", logs.Load(), c.Stats().Errors)
	}
}

func TestAuthDisables(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{})
	wrong := client(t, srv.URL(), func(cfg *Config) { cfg.APIKey = trace.NewSecret("sk-wrong") })
	if _, err := wrong.Decide(context.Background(), request(t, 1, decide.LaneFast)); ClassOf(err) != ClassAuth {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := wrong.Decide(context.Background(), request(t, 2, decide.LaneFast)); ClassOf(err) != ClassDisabled || srv.Hits() != 1 {
		t.Fatalf("after 401: %v (%d requests)", err, srv.Hits())
	}
	if !wrong.Stats().Disabled {
		t.Fatal("not disabled")
	}
}

// TestBadRequestMarksQuestionSet: a 400 marks the shape of the question
// set, not its live text, for BadSetTTL; repeated 400s open the breaker.
func TestBadRequestMarksQuestionSet(t *testing.T) {
	clk := newClock()
	always := jevtest.Faults{Func: func(*jevtest.Call) (jevtest.Fault, bool) { return jevtest.FaultBadRequest, true }}
	rejecting := scriptedServer(t, always)
	var logs atomic.Int32
	rc := client(t, rejecting.URL(), func(cfg *Config) { cfg.Now, cfg.Logf = clk.Now, func(string, ...any) { logs.Add(1) } })
	fast := func(seq uint64, enemies, shift int) *decide.Request {
		st := testState()
		for len(st.Enemies) < enemies {
			e := st.Enemies[0]
			e.ID = fmt.Sprintf("e%d", len(st.Enemies)+1)
			st.Enemies = append(st.Enemies, e)
		}
		st.Enemies = st.Enemies[:enemies]
		for i := range st.Enemies { // live ranges and bearings: other option text
			st.Enemies[i].Units += 37 * shift
			st.Enemies[i].Bearing -= 11 * shift
		}
		req, err := decide.NewRequest(seq, decide.LaneFast, 1234, st, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	want := func(req *decide.Request, class ErrorClass, calls int) {
		t.Helper()
		if _, err := rc.Decide(context.Background(), req); ClassOf(err) != class || rejecting.Count() != calls {
			t.Fatalf("request %d: %v (%d calls), want %s (%d calls)", req.Seq, err, rejecting.Count(), class, calls)
		}
	}
	want(fast(1, 2, 0), ClassBadRequest, 1)
	if string(fast(1, 2, 0).QuestionsJSON()) == string(fast(2, 2, 1).QuestionsJSON()) {
		t.Fatal("the shifted requests should differ in their option text")
	}
	want(fast(2, 2, 1), ClassBadSet, 1) // same shape, other ranges: refused locally
	want(fast(3, 2, 2), ClassBadSet, 1)
	want(fast(4, 3, 0), ClassBadRequest, 2) // another shape (three enemies) is sent
	clk.Advance(DefaultBadSetTTL)
	want(fast(5, 2, 3), ClassBadRequest, 3) // the mark expired: one more try
	want(fast(6, 2, 4), ClassBadSet, 3)
	if st := rc.Stats(); st.BadSets != 2 || st.Errors.BadRequest != 3 || st.Errors.BadSet != 3 || logs.Load() != 3 {
		t.Fatalf("stats %+v, %d logs", st, logs.Load())
	}
	// five consecutive 400s (two more shapes) open the breaker
	want(fast(7, 1, 0), ClassBadRequest, 4)
	want(fast(8, 0, 0), ClassBadRequest, 5)
	want(request(t, 9, decide.LaneSlow), ClassBreaker, 5)

	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultBadRequest}})
	c := client(t, srv.URL(), nil)
	if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneSlow)); ClassOf(err) != ClassBadRequest {
		t.Fatalf("400: %v", err)
	}
	if srv.Count() != 1 {
		t.Fatal("a 400 was retried")
	}
	if _, err := c.Decide(context.Background(), request(t, 2, decide.LaneSlow)); ClassOf(err) != ClassBadSet || srv.Count() != 1 {
		t.Fatalf("same set again: %v (%d calls)", err, srv.Count())
	}
	if _, err := c.Decide(context.Background(), request(t, 3, decide.LaneFast)); err != nil || srv.Count() != 2 {
		t.Fatalf("another set: %v (%d calls)", err, srv.Count())
	}
	if c.Stats().BadSets != 1 {
		t.Fatal(c.Stats())
	}
	// the fake server's own validation: 422 for an invalid question
	req := request(t, 4, decide.LaneFast)
	bad := *req
	bad.Questions = append([]decide.Question(nil), req.Questions...)
	bad.Questions[0].Options = nil
	if _, err := c.Decide(context.Background(), &bad); ClassOf(err) != ClassBadRequest {
		t.Fatalf("422: %v", err)
	}
}

func TestSafetyRules(t *testing.T) {
	key := trace.NewSecret(testKey)
	for _, tc := range []struct {
		base   string
		custom bool
		want   error
	}{
		{"", false, ErrTestNetwork}, // the default host, refused inside a test binary
		{"https://api.typesafe.ai/", false, ErrTestNetwork},
		{"https://api.typesafe.ai:443", false, ErrTestNetwork},
		{"https://evil.example.com", false, ErrCustomBase},
		{"https://api.typesafe.ai/proxy", false, ErrCustomBase},
		{"https://api.typesafe.ai:8443", false, ErrCustomBase},
		{"https://evil.example.com", true, ErrTestNetwork},
		{"http://evil.example.com", true, ErrBaseURL},
		{"http://api.typesafe.ai", false, ErrBaseURL},
		{"http://127.0.0.1:9", false, ErrCustomBase},
		{"http://127.0.0.1:9", true, nil},
		{"http://localhost:9", true, nil},
		{"https://localhost:9/adapter", true, nil},
		{"http://[::1]:9", true, nil},
		{"ftp://127.0.0.1", true, ErrBaseURL},
		{"https://user:pw@127.0.0.1", true, ErrBaseURL},
		{"http://127.0.0.1:9?x=1", true, ErrBaseURL},
		{"127.0.0.1:80", true, ErrBaseURL},
		{"http://127.0.0.2.evil.example.com", true, ErrBaseURL},
	} {
		_, err := New(Config{BaseURL: tc.base, APIKey: key, AllowCustomBase: tc.custom})
		if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
			t.Errorf("%q custom=%v: %v, want %v", tc.base, tc.custom, err, tc.want)
		}
		if err != nil && strings.Contains(err.Error(), testKey) {
			t.Errorf("key in error %v", err)
		}
	}
	c, err := New(Config{BaseURL: "http://127.0.0.1:9/base/", APIKey: key, AllowCustomBase: true})
	if err != nil || c.Endpoint() != "http://127.0.0.1:9/base/v1/systemone" {
		t.Fatalf("endpoint %v %v", c, err)
	}
	if _, err := New(Config{BaseURL: "http://127.0.0.1:9", AllowCustomBase: true}); !errors.Is(err, ErrNoKey) {
		t.Errorf("no key: %v", err)
	}
	for model, want := range map[string]error{"jev-latest": ErrAlias, "jev-preview": ErrAlias, "jev": ErrAlias, "jev-1.13": nil, "jev-1.13.0": nil} {
		_, err := New(Config{BaseURL: "http://127.0.0.1:9", APIKey: key, AllowCustomBase: true, Model: model})
		if want == nil && err != nil || want != nil && !errors.Is(err, want) {
			t.Errorf("model %q: %v", model, err)
		}
	}
	if c, err := New(Config{BaseURL: "http://127.0.0.1:9", APIKey: key, AllowCustomBase: true, Model: "jev-latest", AllowAlias: true}); err != nil || c.Model() != "jev-latest" {
		t.Errorf("allowed alias: %v", err)
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{EnvAPIKey: " " + testKey + "\n", EnvBaseURL: "http://127.0.0.1:9", EnvAllowCustomBase: "1", EnvModel: "jev-1.13"}
	cfg := FromEnv(func(k string) string { return env[k] })
	if cfg.APIKey.Reveal() != testKey || cfg.BaseURL != "http://127.0.0.1:9" || !cfg.AllowCustomBase || cfg.Model != "jev-1.13" {
		t.Fatalf("%+v", cfg)
	}
	if s := fmt.Sprintf("%+v", cfg); strings.Contains(s, testKey) {
		t.Fatal("config prints the key")
	}
	if cfg := FromEnv(func(string) string { return "" }); cfg.APIKey.IsSet() || cfg.AllowCustomBase {
		t.Fatalf("empty env: %+v", cfg)
	}
}

// TestNoRedirects: a redirect is never followed, so the key cannot be
// bounced to another host.
func TestNoRedirects(t *testing.T) {
	var other atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1) }))
	defer target.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/systemone", http.StatusTemporaryRedirect)
	}))
	defer src.Close()
	c := client(t, src.URL, func(cfg *Config) { cfg.HTTPClient = &http.Client{} })
	if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast)); ClassOf(err) != ClassHTTP {
		t.Fatalf("redirect: %v", err)
	}
	if other.Load() != 0 {
		t.Fatal("the redirect was followed")
	}
}

type denyAll struct{ n atomic.Int32 }

func (d *denyAll) Allow(int) bool { d.n.Add(1); return false }

func TestRateLimit(t *testing.T) {
	clk := newClock()
	srv := scriptedServer(t, jevtest.Faults{})
	c := client(t, srv.URL(), func(cfg *Config) { cfg.Now, cfg.RatePerSec, cfg.Burst = clk.Now, 2, 1 })
	if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(context.Background(), request(t, 2, decide.LaneFast)); ClassOf(err) != ClassThrottled || srv.Count() != 1 {
		t.Fatalf("over the rate: %v (%d calls)", err, srv.Count())
	}
	clk.Advance(500 * time.Millisecond)
	if _, err := c.Decide(context.Background(), request(t, 3, decide.LaneFast)); err != nil {
		t.Fatalf("after 0.5 s: %v", err)
	}
	// a shared limiter is consulted too
	d := &denyAll{}
	c2 := client(t, srv.URL(), func(cfg *Config) { cfg.Limiter, cfg.RatePerSec = d, -1 })
	if _, err := c2.Decide(context.Background(), request(t, 1, decide.LaneFast)); ClassOf(err) != ClassThrottled || d.n.Load() != 1 {
		t.Fatalf("shared limiter: %v", err)
	}
	// its refusals do not spend the client's own tokens: with the clock
	// stopped, the bucket's one token is still there when it allows again
	tl := &toggle{}
	c3 := client(t, srv.URL(), func(cfg *Config) { cfg.Now, cfg.RatePerSec, cfg.Burst, cfg.Limiter = clk.Now, 2, 1, tl })
	for i := 1; i <= 3; i++ {
		if _, err := c3.Decide(context.Background(), request(t, uint64(i), decide.LaneFast)); ClassOf(err) != ClassThrottled {
			t.Fatalf("shared refusal %d: %v", i, err)
		}
	}
	if tl.asked.Load() != 3 {
		t.Fatalf("the shared limiter was asked %d times, want 3 (the bucket refused instead)", tl.asked.Load())
	}
	tl.allow.Store(true)
	if _, err := c3.Decide(context.Background(), request(t, 4, decide.LaneFast)); err != nil {
		t.Fatalf("after the shared limiter allows: %v", err)
	}
}

// toggle is a shared limiter that refuses until allow is set.
type toggle struct {
	allow atomic.Bool
	asked atomic.Int32
}

func (l *toggle) Allow(int) bool { l.asked.Add(1); return l.allow.Load() }

func TestMalformedAndMissing(t *testing.T) {
	srv := scriptedServer(t, jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultMalformed, jevtest.FaultMissing}})
	c := client(t, srv.URL(), nil)
	if _, err := c.Decide(context.Background(), request(t, 1, decide.LaneFast)); ClassOf(err) != ClassDecode {
		t.Fatalf("malformed: %v", err)
	}
	req := request(t, 2, decide.LaneFast)
	resp, err := c.Decide(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	last := req.Questions[len(req.Questions)-1].ID
	if _, ok := resp.Answers[last]; ok || len(resp.Answers) != len(req.Questions)-1 {
		t.Fatalf("missing answer: %+v", resp.Answers)
	}
}

// TestConcurrent: many calls at once against the noisy policy (run with
// -race).
func TestConcurrent(t *testing.T) {
	srv := server(t, jevtest.Options{Policy: &jevtest.Noisy{Base: jevtest.NewScripted(scripted.Config{}), Seed: 9},
		Faults: jevtest.Faults{Latency: []time.Duration{time.Millisecond, 3 * time.Millisecond}}})
	c := client(t, srv.URL(), func(cfg *Config) { cfg.RatePerSec = -1 })
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lane := decide.Lane(i % 2)
			if _, err := c.Decide(context.Background(), request(t, uint64(i), lane)); err != nil {
				fails.Add(1)
			}
		}(i)
	}
	wg.Wait()
	st := c.Stats()
	if fails.Load() != 0 || st.OK != 24 || st.P50 <= 0 || st.P95 < st.P50 || st.P99 < st.P95 {
		t.Fatalf("fails %d stats %+v", fails.Load(), st)
	}
}
