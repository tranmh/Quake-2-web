package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
)

// TestRunPanicReleases: a run whose bot loop panics (runSafely recovers
// it, as the Manager does) still stops its game, closes its connection and
// finishes its trace file and demo.
func TestRunPanicReleases(t *testing.T) {
	if testing.Short() {
		t.Skip("realtime: about a second of wall time")
	}
	fs := sessiontest.DemoFS(t)
	h := host.New()
	defer h.Shutdown()
	var stopped atomic.Bool
	frames := 0
	cfg := Config{FS: fs, Maps: []string{"demo1"}, Session: SessionInProc, Seed: 1, Record: true, OutDir: t.TempDir(),
		Logf: t.Logf,
		NewInstance: func(_ int, spec session.Spec, seed uint32) (*host.Instance, func(), error) {
			i, err := session.NewInstance(h, session.InstanceConfig{ID: "bot-panic-000", FS: fs, Spec: spec, Seed: seed})
			if err != nil {
				return nil, nil, err
			}
			return i, func() { stopped.Store(true); i.Stop() }, nil
		},
		// runs on the bot's goroutine, inside campaign.Run
		OnServerMessage: func(c *fakeclient.Client, _ []byte, _ []fakeclient.Span) {
			if c.Frame.Valid {
				if frames++; frames == 5 {
					panic("bot loop failure")
				}
			}
		},
	}
	r, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sum, err := runSafely(context.Background(), r)
	if sum != nil || err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("run: %v, %v", sum, err)
	}
	if !stopped.Load() {
		t.Fatal("the episode's game was not stopped")
	}
	if ids := h.Instances(); len(ids) != 0 {
		t.Fatalf("instances still running: %v", ids)
	}
	ep := filepath.Join(r.Dir(), EpisodeDir(0))
	evs, err := trace.ReadFile(filepath.Join(ep, TraceFile))
	if err != nil || len(evs) == 0 {
		t.Fatalf("trace file not finished: %d events, %v", len(evs), err)
	}
	demos, _ := filepath.Glob(filepath.Join(ep, DemoDir, "*.dm2"))
	if len(demos) == 0 {
		t.Fatal("no demo recorded before the panic")
	}
	for _, d := range demos {
		b, err := os.ReadFile(d)
		// a finished demo ends with the -1 block length
		if err != nil || len(b) < 4 || string(b[len(b)-4:]) != "\xff\xff\xff\xff" {
			t.Fatalf("demo %s not finished (%d bytes, %v)", d, len(b), err)
		}
	}
}

// deadlineStore records whether the account's flushes carry a deadline.
type deadlineStore struct {
	mu       sync.Mutex
	armed    bool
	calls    int
	unbound  int
	deadline time.Duration
}

func (s *deadlineStore) note(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.armed {
		return
	}
	s.calls++
	d, ok := ctx.Deadline()
	if !ok {
		s.unbound++
		return
	}
	s.deadline = time.Until(d)
}

func (s *deadlineStore) LoadSpend(ctx context.Context, _ string) (float64, error) {
	s.note(ctx)
	return 0, nil
}

func (s *deadlineStore) AddSpend(ctx context.Context, _ string, usd float64) (float64, error) {
	s.note(ctx)
	return usd, nil
}

// TestRunFlushBounded: the run's final flush of the account's spend has a
// deadline (a stalled store must not hold the run after its game ended),
// even when the run's own context is already done.
func TestRunFlushBounded(t *testing.T) {
	store := &deadlineStore{}
	acct, err := budget.NewAccount(context.Background(), budget.AccountConfig{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	store.armed = true
	r, err := New(Config{FS: sessiontest.DemoFS(t), Maps: []string{"demo1"}, Seed: 1, Account: acct, OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // no episode is played; the run ends and flushes
	if _, err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.calls == 0 || store.unbound != 0 || store.deadline <= 0 || store.deadline > accountFlushTimeout {
		t.Fatalf("%d store calls, %d without a deadline (last %v)", store.calls, store.unbound, store.deadline)
	}
}

// TestRunSetupFailureReason: an episode that cannot be set up fails with a
// reason naming the stage only (run.json is public for a public bot); the
// error, with the path, goes to the caller.
func TestRunSetupFailureReason(t *testing.T) {
	r, err := New(Config{FS: sessiontest.DemoFS(t), Maps: []string{"demo1"}, Seed: 1, OutDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(r.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	// a file where the episode's directory goes
	if err := os.WriteFile(filepath.Join(r.Dir(), EpisodeDir(0)), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := r.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), r.Dir()) || !strings.Contains(err.Error(), stageDir) {
		t.Fatalf("run error %v", err)
	}
	var run struct{ Outcome, Reason string }
	readJSON(t, filepath.Join(r.Dir(), RunFile), &run)
	for _, got := range []string{s.Reason, run.Reason} {
		if got != "episode 0: setup failed: episode directory" {
			t.Fatalf("reason %q", got)
		}
	}
	if run.Outcome != OutcomeFailed {
		t.Fatalf("outcome %s", run.Outcome)
	}
	st, why := endStatus(s, err, "", false, time.Minute)
	if st != api.BotFailed || strings.Contains(why, r.Dir()) {
		t.Fatalf("bot status %s %q", st, why)
	}
}
