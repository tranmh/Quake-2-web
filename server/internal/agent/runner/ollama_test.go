package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/ollama"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/session/sessiontest"
)

// fakeOllama is a fake Ollama /v1/systemone whose model is the scripted
// policy (jev's protocol, no key: a request with an Authorization header
// is refused). delay(n) delays the n-th call.
func fakeOllama(t *testing.T, delay func(n int64) time.Duration) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	pol := jevtest.NewScripted(scripted.Config{Seed: 1})
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if delay != nil {
			time.Sleep(delay(n))
		}
		if r.URL.Path != "/v1/systemone" || r.Header.Get("Authorization") != "" {
			http.Error(w, `{"error":"unexpected request"}`, http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(r.Body)
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
		raw, _ := decide.MarshalResponse(wr.Model, wr.Questions, rep.Answers, decide.Usage{InputTokens: 700})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// The ollama backend's configuration: a model backend (212 ms simulated
// latency, the provenance gate), checked URL and timeout, not for live
// bots.
func TestOllamaConfig(t *testing.T) {
	if !modelBackend(BackendOllama) || allowedBackend(BackendOllama) {
		t.Fatal("ollama: a model backend, not a live bot's")
	}
	known := false
	for _, b := range Backends() {
		known = known || b == BackendOllama
	}
	if !known {
		t.Fatalf("Backends() %v", Backends())
	}
	for _, c := range []Config{
		{FS: nopFS{}, Backend: BackendOllama, Ollama: ollama.Config{BaseURL: "ftp://h"}},
		{FS: nopFS{}, Backend: BackendOllama, Ollama: ollama.Config{Timeout: -time.Second}},
		{FS: nopFS{}, Backend: BackendOllama, HangGuard: -time.Second},
	} {
		if err := c.check(); !errors.Is(err, ErrConfig) {
			t.Errorf("%+v %v: %v", c.Ollama, c.HangGuard, err)
		}
	}
	c := Config{FS: nopFS{}, Backend: BackendOllama}
	if err := c.check(); err != nil {
		t.Fatal(err)
	}
	lat := func(c Config, recorded string) Latency {
		return (&Runner{cfg: c, be: &backends{recordedKind: recorded}}).latency()
	}
	if l := lat(c, ""); l.Fixed != DefaultModelLatency {
		t.Errorf("ollama latency %+v", l)
	}
	c.Backend = BackendReplay
	if l := lat(c, BackendOllama); l.Fixed != DefaultModelLatency {
		t.Errorf("the replay of an ollama run: latency %+v", l)
	}
}

// A lockstep ollama run waits for its model however long a call takes
// (up to the client's timeout): a call slower than the scheduler's
// fast-lane timeout (800 ms) is answered, not timed out or stale, and the
// scheduler's wall-clock hang guard is raised for this backend so that a
// call of minutes is not cut. The guard is what would cut it: here a
// small injected guard stands for the default 60 s against a slow reply.
// Other backends keep the scheduler's default guard.
func TestOllamaLockstepSlowCall(t *testing.T) {
	const reply = decide.DefaultFastTimeout + 200*time.Millisecond
	srv, _ := fakeOllama(t, func(int64) time.Duration { return reply })
	b, err := newBackends(&Config{Backend: BackendOllama, Session: SessionLockstep,
		Ollama: ollama.Config{BaseURL: srv.URL, Timeout: 2 * time.Second}}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer b.close()
	if b.model != ollama.DefaultModel {
		t.Fatalf("model %q", b.model)
	}
	be, err := b.episode(0, 1, false, decide.FixedLatency(DefaultModelLatency))
	if err != nil {
		t.Fatal(err)
	}
	if want := 33 * 2 * time.Second; be.hangGuard != want || be.source != decide.SourceModel || be.backend.Name() != BackendOllama {
		t.Fatalf("hang guard %v (want %v), source %v", be.hangGuard, want, be.source)
	}
	if g := ollamaHangGuard(ollama.DefaultTimeout); g <= decide.DefaultWallTimeout {
		t.Fatalf("default ollama hang guard %v", g)
	}
	for _, kind := range []string{BackendScripted, BackendConstant} {
		ob := &backends{kind: kind}
		if e, err := ob.episode(0, 1, false, nil); err != nil || e.hangGuard != 0 {
			t.Fatalf("%s: hang guard %v, %v", kind, e.hangGuard, err)
		}
	}
	st := &decide.State{Mode: "fight", Me: decide.Me{HP: "ok", Health: 70, Weapon: "shotgun", Ammo: "ok", Weapons: []string{"shotgun"}},
		Enemies: []decide.Enemy{{ID: "e1", Class: "soldier", Dist: "mid", Units: 420, Visible: true, Shootable: true, Threat: "med"}}}
	collect := func(guard time.Duration) decide.Result {
		sched := decide.NewScheduler(decide.SchedulerConfig{Backend: be.backend, Mode: decide.Lockstep,
			SimLatency: decide.FixedLatency(DefaultModelLatency), WallTimeout: guard})
		defer sched.Close()
		req, err := decide.NewRequest(1, decide.LaneFast, 1000, st, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !sched.Submit(req) {
			t.Fatal("submit refused")
		}
		res := sched.Collect(1000 + DefaultModelLatency.Milliseconds())
		if len(res) != 1 {
			t.Fatalf("%d results", len(res))
		}
		return res[0]
	}
	if r := collect(be.hangGuard); r.Err != nil || r.Timeout || r.Stale || r.Resp == nil || len(r.Resp.Answers) == 0 ||
		r.Latency != DefaultModelLatency || r.Resp.Latency < reply {
		t.Fatalf("under the ollama guard: %+v", r)
	}
	if r := collect(reply / 2); !r.Timeout || r.Resp != nil {
		t.Fatalf("under a guard shorter than the reply: %+v", r)
	}
}

// TestRunOllama runs demo1 on the ollama backend against a fake server
// whose model is the scripted policy: the run is model-driven, free, says
// which model played, and replays from its trace without the server.
func TestRunOllama(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	// a few calls slower than the fast lane's 800 ms: a lockstep run does
	// not drop them
	srv, calls := fakeOllama(t, func(n int64) time.Duration {
		if n == 3 || n == 4 {
			return decide.DefaultFastTimeout + 100*time.Millisecond
		}
		return 0
	})
	var logs strings.Builder
	cfg := Config{Backend: BackendOllama, Seed: 1, EpisodeTimeout: gameCap(0), Ollama: ollama.Config{BaseURL: srv.URL, Model: "nimble-v3"}}
	cfg.FS, cfg.Maps, cfg.OutDir = fs, []string{"demo1"}, t.TempDir()
	cfg.Logf = func(format string, args ...any) {
		logs.WriteString(fmt.Sprintf(format, args...) + "\n")
		t.Logf(format, args...)
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Run(context.Background())
	if err != nil || s == nil {
		t.Fatalf("run: %v", err)
	}
	mustValid(t, r.Dir(), ValidateOptions{})
	a := s.API
	if s.Backend != BackendOllama || s.Model != "nimble-v3" || !s.Config.ModelBackend || s.Gate == nil || !s.Gate.ModelBackend {
		t.Fatalf("summary backend %q model %q gate %+v", s.Backend, s.Model, s.Gate)
	}
	if a.Calls == 0 || a.OK != a.Calls || a.Errors != 0 || a.CostUSD != 0 || a.InputTokens != 700*int64(a.OK) ||
		a.LatencyMs.P50 != float64(DefaultModelLatency/time.Millisecond) || int64(a.Calls) > calls.Load() {
		t.Fatalf("api %+v (%d server calls)", a, calls.Load())
	}
	if !s.ModelDriven {
		t.Fatalf("not model-driven: %+v", s.Gate)
	}
	if !strings.Contains(logs.String(), "ollama nimble-v3") {
		t.Fatalf("no client stats logged:\n%s", logs.String())
	}
	t.Logf("ollama: %d calls, gate %+v", a.Calls, s.Gate)

	if testing.Short() {
		return
	}
	srv.Close()
	rep, err := Replay(context.Background(), ReplayConfig{Trace: filepath.Join(r.Dir(), "ep-000", TraceFile), Mode: ReplayResponses,
		Strict: true, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Diverged() || rep.Err != "" || rep.ResponseDivergence != nil || rep.Backend != BackendOllama {
		js, _ := json.MarshalIndent(rep, "", " ")
		t.Fatalf("ollama replay: %s", js)
	}
	t.Logf("ollama replay: %d events equal", rep.Matched)
}
