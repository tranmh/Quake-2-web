package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"

	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/host"
)

// fakeRun is a bot's run for Manager tests: it enters demo1, writes a
// trace and two demos, then waits for its end (ctx, or finish with an
// outcome) and writes run.json like a real run.
type fakeRun struct {
	cfg     Config
	bus     *trace.Bus
	entered chan struct{}
	finish  chan string // an outcome ends the run
}

func (f *fakeRun) Bus() *trace.Bus { return f.bus }

func (f *fakeRun) Run(ctx context.Context) (*metrics.RunSummary, error) {
	defer f.bus.Close()
	dir := filepath.Join(f.cfg.OutDir, f.cfg.RunID)
	ep := filepath.Join(dir, EpisodeDir(0))
	_ = os.MkdirAll(filepath.Join(ep, DemoDir), 0o755)
	_ = os.WriteFile(filepath.Join(ep, TraceFile), []byte("partial"), 0o644)
	_ = os.WriteFile(filepath.Join(ep, DemoDir, "00-demo1.dm2"), []byte("demo one"), 0o644)
	_ = os.WriteFile(filepath.Join(ep, DemoDir, "01-demo1.dm2"), []byte("recording"), 0o644)
	f.bus.Publish(trace.Event{Type: trace.TypeLevelStart, Map: "demo1", SF: 3, Body: trace.LevelStart{Gen: 1}})
	close(f.entered)
	outcome, reason := OutcomeAborted, "context canceled"
	select {
	case <-ctx.Done():
		reason = ctx.Err().Error()
	case outcome = <-f.finish:
		reason = "played"
	}
	f.bus.Publish(trace.Event{Type: trace.TypeRunEnd, Body: trace.RunEnd{Outcome: outcome, Reason: reason}})
	s := &metrics.RunSummary{Schema: metrics.Schema, Run: f.cfg.RunID, Outcome: outcome, Reason: reason, Backend: f.cfg.Backend}
	_ = os.WriteFile(filepath.Join(ep, TraceFile), []byte("whole"), 0o644)
	if err := metrics.WriteJSON(filepath.Join(dir, RunFile), s); err != nil {
		return s, err
	}
	return s, nil
}

// fakeRuns makes fakeRuns and keeps their configs.
type fakeRuns struct {
	mu   sync.Mutex
	runs []*fakeRun
	err  error
}

func (fr *fakeRuns) newRun(cfg Config) (botRun, error) {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if fr.err != nil {
		return nil, fr.err
	}
	if err := os.MkdirAll(filepath.Join(cfg.OutDir, cfg.RunID), 0o755); err != nil {
		return nil, err
	}
	r := &fakeRun{cfg: cfg, bus: trace.NewBus(cfg.RunID, nil), entered: make(chan struct{}), finish: make(chan string, 1)}
	fr.runs = append(fr.runs, r)
	return r, nil
}

func (fr *fakeRuns) last(t *testing.T) *fakeRun {
	t.Helper()
	fr.mu.Lock()
	defer fr.mu.Unlock()
	if len(fr.runs) == 0 {
		t.Fatal("no run made")
	}
	return fr.runs[len(fr.runs)-1]
}

// nopGames is a GameRunner the fake runs never use.
type nopGames struct{}

func (nopGames) CreateWithID(context.Context, string, api.GameSpec) (string, error) {
	return "", errors.New("no games in this test")
}
func (nopGames) Stop(context.Context, string) error { return nil }
func (nopGames) Host() *host.Host                   { return host.New() }

type emptyFS struct{}

func (emptyFS) ReadFile(name string) ([]byte, error) { return nil, fs.ErrNotExist }

// logBuffer collects log output (safe for concurrent use).
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (l *logBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func newTestManager(t *testing.T, adjust func(*ManagerConfig)) (*Manager, *fakeRuns) {
	t.Helper()
	cfg := ManagerConfig{Games: nopGames{}, FS: func(context.Context) (FileSystem, error) { return emptyFS{}, nil },
		Tickets: auth.NewTickets(0), Dir: t.TempDir(), StopWait: 5 * time.Second, AllowUsers: true,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if adjust != nil {
		adjust(&cfg)
	}
	m, err := NewManager(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRuns{}
	m.newRun = fr.newRun
	t.Cleanup(m.Close)
	return m, fr
}

var (
	alice = api.BotUser{ID: 1, Name: "alice"}
	bob   = api.BotUser{ID: 2, Name: "bob"}
	admin = api.BotUser{ID: 3, Name: "root", Admin: true}
	anon  = api.BotUser{}
)

func startBot(t *testing.T, m *Manager, fr *fakeRuns, spec api.BotSpec, u api.BotUser) (api.BotInfo, *fakeRun) {
	t.Helper()
	b, err := m.Start(context.Background(), spec, u)
	if err != nil {
		t.Fatalf("start %+v as %s: %v", spec, u.Name, err)
	}
	r := fr.last(t)
	<-r.entered
	return b, r
}

func TestManagerPermissions(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxPerUser = 5; c.MaxBots = 5 })
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		u       api.BotUser
		backend string
		want    error
	}{
		{"anonymous", anon, "scripted", api.ErrBotForbidden},
		{"user jev", alice, "jev", api.ErrBotForbidden},
		{"admin jev without a key", admin, "jev", api.ErrBotsDisabled},
		{"replay", admin, "replay", api.ErrBotInvalid},
		{"unknown", admin, "llm", api.ErrBotInvalid},
	} {
		if _, err := m.Start(ctx, api.BotSpec{Backend: tc.backend}, tc.u); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	for _, be := range []string{"scripted", "mock", "constant", "random", ""} {
		b, r := startBot(t, m, fr, api.BotSpec{Backend: be, Maps: []string{"demo1"}}, alice)
		if be == "" {
			be = "scripted"
		}
		if b.Backend != be || r.cfg.Backend != be || b.OwnerID != alice.ID || b.Status != api.BotStarting {
			t.Errorf("%s bot %+v (run backend %s)", be, b, r.cfg.Backend)
		}
		if r.cfg.Session != SessionInProc || !r.cfg.Record || r.cfg.Trace != TraceFull || r.cfg.Episodes != 1 || r.cfg.NewInstance == nil {
			t.Errorf("%s run config %+v", be, r.cfg)
		}
		// mock answers are free: no account, but a per-run budget like jev
		if r.cfg.Account != nil || r.cfg.Jev.APIKey.IsSet() {
			t.Errorf("%s run has the account or key", be)
		}
		if (be == "mock") != r.cfg.Budget.Enabled() && m.cfg.BudgetUSDPerRun > 0 {
			t.Errorf("%s budget %+v", be, r.cfg.Budget)
		}
		// a mock answers about as late as the API unless told otherwise
		if want := be == "mock"; want != (r.cfg.SimLatency.Fixed == DefaultModelLatency) {
			t.Errorf("%s latency %v", be, r.cfg.SimLatency)
		}
	}

	// users need Q2_BOTS_ALLOW_USERS; admins do not
	m2, fr2 := newTestManager(t, func(c *ManagerConfig) { c.AllowUsers = false })
	if _, err := m2.Start(ctx, api.BotSpec{Backend: "scripted"}, alice); !errors.Is(err, api.ErrBotForbidden) {
		t.Errorf("user without AllowUsers: %v", err)
	}
	startBot(t, m2, fr2, api.BotSpec{Backend: "scripted"}, admin)

	// a jev bot with a key: the run gets the key, the model and the shared account
	m3, fr3 := newTestManager(t, func(c *ManagerConfig) {
		c.Jev.APIKey = trace.NewSecret("sk-test-key")
		c.BudgetUSDPerRun = 0.5
	})
	b, r := startBot(t, m3, fr3, api.BotSpec{Backend: "jev"}, admin)
	if r.cfg.Jev.APIKey.Reveal() != "sk-test-key" || r.cfg.Account != m3.Account() || r.cfg.Budget.USD != 0.5 ||
		b.Model == "" || r.cfg.Jev.Model != b.Model {
		t.Errorf("jev run %+v, bot %+v", r.cfg, b)
	}
	// the daily budget refuses new jev bots once spent
	m4, _ := newTestManager(t, func(c *ManagerConfig) {
		c.Jev.APIKey = trace.NewSecret("sk-test-key")
		c.DailyUSD = 1
	})
	m4.Account().Add(1.5)
	if _, err := m4.Start(ctx, api.BotSpec{Backend: "jev"}, admin); !errors.Is(err, api.ErrBotLimit) {
		t.Errorf("jev over the daily budget: %v", err)
	}
}

func TestManagerSpecValidation(t *testing.T) {
	m, fr := newTestManager(t, nil)
	ctx := context.Background()
	three, neg := 3, -1
	for name, spec := range map[string]api.BotSpec{
		"skill":        {Backend: "scripted", Skill: &neg},
		"not a prefix": {Backend: "scripted", Maps: []string{"demo2"}},
		"unknown map":  {Backend: "scripted", Maps: []string{"demo1", "base1"}},
		"latency":      {Backend: "mock", SimLatency: "fast"},
		"slow latency": {Backend: "mock", SimLatency: "100ms,9s"},
		"long name":    {Backend: "scripted", Name: strings.Repeat("x", 65)},
		"control name": {Backend: "scripted", Name: "a\nb"},
	} {
		if _, err := m.Start(ctx, spec, admin); !errors.Is(err, api.ErrBotInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	b, r := startBot(t, m, fr, api.BotSpec{Backend: "mock", Name: "  probe  ", Maps: []string{"demo1", "demo2"}, Skill: &three,
		SimLatency: "80ms,212ms", Public: true}, admin)
	if b.Name != "probe" || len(b.Maps) != 2 || b.Skill != 3 || !b.Public || *r.cfg.Skill != 3 ||
		r.cfg.SimLatency.String() != "80ms,212ms" || len(r.cfg.Maps) != 2 {
		t.Fatalf("bot %+v run %+v", b, r.cfg)
	}
	if !strings.Contains(r.cfg.Userinfo, `\name\probe`) || !strings.Contains(r.cfg.Userinfo, `\password\`) ||
		!strings.Contains(r.cfg.Userinfo, `\fov\90`) {
		t.Errorf("userinfo %q", r.cfg.Userinfo)
	}
	b, _ = startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	if b.Name != "alice's bot" || len(b.Maps) != 4 || b.Skill != DefaultSkill {
		t.Fatalf("default bot %+v", b)
	}
	// the runner's own refusals are the caller's mistake
	fr.mu.Lock()
	fr.err = fmt.Errorf("%w: bad", ErrConfig)
	fr.mu.Unlock()
	if _, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, admin); !errors.Is(err, api.ErrBotInvalid) {
		t.Errorf("runner config error: %v", err)
	}
}

func TestManagerCaps(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxBots = 3; c.MaxPerUser = 1 })
	ctx := context.Background()
	a, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	if _, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, alice); !errors.Is(err, api.ErrBotLimit) {
		t.Fatalf("second bot of alice: %v", err)
	}
	startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, bob)
	startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, admin) // admins: no per-user cap
	if _, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, admin); !errors.Is(err, api.ErrBotLimit) {
		t.Fatalf("fourth bot: %v", err)
	}
	if n := m.liveCount(); n != 3 {
		t.Fatalf("%d live", n)
	}
	// stopping frees the slot
	if err := m.Stop(ctx, a.ID, alice); err != nil {
		t.Fatal(err)
	}
	startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
}

func TestManagerVisibility(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxPerUser = 3 })
	ctx := context.Background()
	priv, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	pub, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted", Public: true}, alice)
	ids := func(u api.BotUser) string {
		l, err := m.List(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, b := range l {
			out = append(out, b.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(alice); !strings.Contains(got, priv.ID) || !strings.Contains(got, pub.ID) {
		t.Errorf("alice lists %s", got)
	}
	for _, u := range []api.BotUser{bob, anon} {
		if got := ids(u); got != pub.ID {
			t.Errorf("%q lists %s", u.Name, got)
		}
		if _, err := m.Get(ctx, priv.ID, u); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("%q gets the private bot: %v", u.Name, err)
		}
		if _, err := m.Watch(ctx, priv.ID, u); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("%q watches the private bot: %v", u.Name, err)
		}
		if err := m.Stop(ctx, priv.ID, u); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("%q stops the private bot: %v", u.Name, err)
		}
		if _, err := m.OpenArtifact(ctx, priv.ID, "ep-000/demos/00-demo1.dm2", u); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("%q reads the private bot: %v", u.Name, err)
		}
		// a public bot is watched by anyone, but stopped only by its owner
		if _, err := m.Watch(ctx, pub.ID, u); err != nil {
			t.Errorf("%q watches the public bot: %v", u.Name, err)
		}
		if err := m.Stop(ctx, pub.ID, u); !errors.Is(err, api.ErrBotForbidden) {
			t.Errorf("%q stops the public bot: %v", u.Name, err)
		}
	}
	if got := ids(admin); !strings.Contains(got, priv.ID) {
		t.Errorf("admin lists %s", got)
	}
	w, err := m.Watch(ctx, priv.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(w.WSURL, "/ws/v1/bots/"+priv.ID+"/watch?ticket="+w.Ticket) || w.Pakset != "demo" ||
		w.DecisionsURL != "/ws/v1/bots/"+priv.ID+"/decisions?ticket="+w.DecisionsTicket {
		t.Fatalf("watch %+v", w)
	}
	// the tickets are scoped to their stream and bot
	tk := m.cfg.Tickets
	if _, err := tk.Redeem(w.Ticket, decisionsScope(priv.ID)); err == nil {
		t.Error("watch ticket redeemed for the decision feed")
	}
	if tt, err := tk.Redeem(w.DecisionsTicket, decisionsScope(priv.ID)); err != nil || tt.UserID != alice.ID {
		t.Errorf("decisions ticket: %+v %v", tt, err)
	}
	// an administrator stops anyone's bot; then it is ended for everyone
	if err := m.Stop(ctx, priv.ID, admin); err != nil {
		t.Fatal(err)
	}
	b, err := m.Get(ctx, priv.ID, alice)
	if err != nil || b.Status != api.BotStopped || b.Reason != "stopped by an administrator" || b.EndedAt == nil {
		t.Fatalf("stopped %+v %v", b, err)
	}
	if _, err := m.Watch(ctx, priv.ID, alice); !errors.Is(err, api.ErrBotNotLive) {
		t.Errorf("watch ended: %v", err)
	}
	if err := m.Stop(ctx, priv.ID, alice); err != nil {
		t.Errorf("stop ended: %v", err)
	}
	if err := m.Stop(ctx, priv.ID, bob); !errors.Is(err, api.ErrBotNotFound) {
		t.Errorf("bob stops ended private: %v", err)
	}
	if _, err := m.Get(ctx, "nope", alice); !errors.Is(err, api.ErrBotNotFound) {
		t.Error(err)
	}
	if _, err := m.Get(ctx, "../etc", admin); !errors.Is(err, api.ErrBotNotFound) {
		t.Error(err)
	}
}

func TestManagerEnd(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxPerUser = 5 })
	ctx := context.Background()
	b, r := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	if got, _ := m.Get(ctx, b.ID, alice); got.Status != api.BotRunning || got.Level == nil || got.Level.Map != "demo1" {
		t.Fatalf("running %+v", got)
	}
	r.finish <- OutcomeCompleted
	waitEnded(t, m, b.ID)
	got, err := m.Get(ctx, b.ID, alice)
	if err != nil || got.Status != api.BotFinished || got.Reason != "played" || got.EndedAt == nil || len(got.Summary) == 0 || got.Live != nil {
		t.Fatalf("finished %+v %v", got, err)
	}
	var s metrics.RunSummary
	if err := json.Unmarshal(got.Summary, &s); err != nil || s.Run != b.ID {
		t.Fatalf("summary %s", got.Summary)
	}
	b2, r2 := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	r2.finish <- OutcomeFailed
	waitEnded(t, m, b2.ID)
	if got, _ := m.Get(ctx, b2.ID, alice); got.Status != api.BotFailed {
		t.Fatalf("failed %+v", got)
	}
	// the wall-clock limit stops a run
	m2, fr2 := newTestManager(t, func(c *ManagerConfig) { c.MaxRun = 50 * time.Millisecond })
	b3, _ := startBot(t, m2, fr2, api.BotSpec{Backend: "scripted"}, alice)
	waitEnded(t, m2, b3.ID)
	if got, _ := m2.Get(ctx, b3.ID, alice); got.Status != api.BotStopped || !strings.Contains(got.Reason, "wall-clock limit") {
		t.Fatalf("limited %+v", got)
	}
}

func waitEnded(t *testing.T, m *Manager, id string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if m.live(id) == nil {
			return
		}
	}
	t.Fatalf("bot %s still live", id)
}

func TestEndStatus(t *testing.T) {
	sum := func(outcome, reason string) *metrics.RunSummary {
		return &metrics.RunSummary{Outcome: outcome, Reason: reason}
	}
	for _, tc := range []struct {
		sum            *metrics.RunSummary
		err            error
		stop           string
		limit          bool
		status, reason string
	}{
		{sum(OutcomeCompleted, "1 of 1 episodes to victory.pcx"), nil, "", false, api.BotFinished, "1 of 1 episodes to victory.pcx"},
		{sum(OutcomeCompleted, "x"), ErrIncomplete, "", false, api.BotFinished, "x"},
		{sum(OutcomeCompleted, "x"), errors.New("trace: close /data/bots/x/ep-000/trace.jsonl.gz: disk full"), "", false, api.BotFailed, reasonInternal},
		{sum(OutcomeFailed, "episode 0: death_limit"), nil, "", false, api.BotFailed, "episode 0: death_limit"},
		{sum(OutcomeFailed, "episode 0: death_limit"), errors.Join(fmt.Errorf("%w: failed (episode 0: death_limit)", ErrIncomplete)), "", false,
			api.BotFailed, "episode 0: death_limit"},
		// a reason that is an error's text (a campaign error naming a path)
		{sum(OutcomeFailed, "episode 0: campaign: demo1 nav graph: open /srv/nav/demo1.nav: permission denied"),
			errors.Join(errors.New("episode 0: campaign: demo1 nav graph: open /srv/nav/demo1.nav: permission denied")), "", false,
			api.BotFailed, reasonInternal},
		{sum(OutcomeFailed, ""), errors.New("daily spend: dial tcp db.internal:5432: refused"), "", false, api.BotFailed, reasonInternal},
		{sum(OutcomeAborted, "budget: run budget spent"), nil, "", false, api.BotStopped, "budget: run budget spent"},
		{sum(OutcomeAborted, "context canceled"), nil, "stopped by its owner", false, api.BotStopped, "stopped by its owner"},
		{sum(OutcomeAborted, "context deadline exceeded"), nil, "", true, api.BotStopped, "its wall-clock limit (1m0s) was reached"},
		{nil, errors.New("runner: bot run panicked: open /data/bots/x/ep-000/demos: no space left\nmore"), "", false, api.BotFailed, reasonInternal},
		{nil, nil, "", false, api.BotFailed, reasonInternal},
	} {
		st, why := endStatus(tc.sum, tc.err, tc.stop, tc.limit, time.Minute)
		if st != tc.status || why != tc.reason {
			t.Errorf("%+v %v: %s %q, want %s %q", tc.sum, tc.err, st, why, tc.status, tc.reason)
		}
	}
}

func TestManagerArtifacts(t *testing.T) {
	m, fr := newTestManager(t, nil)
	ctx := context.Background()
	b, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	names := func(as []api.BotArtifactInfo) string {
		var out []string
		for _, a := range as {
			out = append(out, a.Kind+":"+a.Name)
		}
		return strings.Join(out, " ")
	}
	// live: no trace yet, and not the demo being recorded
	live, _ := m.Get(ctx, b.ID, alice)
	if got := names(live.Artifacts); got != "demo:ep-000/demos/00-demo1.dm2" {
		t.Fatalf("live artifacts %s", got)
	}
	for _, name := range []string{"ep-000/trace.jsonl.gz", "ep-000/demos/01-demo1.dm2"} {
		if _, err := m.OpenArtifact(ctx, b.ID, name, alice); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("live %s: %v", name, err)
		}
	}
	read := func(name string) (*api.BotArtifact, string) {
		t.Helper()
		a, err := m.OpenArtifact(ctx, b.ID, name, alice)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		defer a.Content.Close()
		data, _ := io.ReadAll(a.Content)
		return a, string(data)
	}
	if a, data := read("ep-000/demos/00-demo1.dm2"); data != "demo one" || a.ContentType != "application/octet-stream" || a.Kind != api.ArtifactDemo {
		t.Fatalf("live demo %+v %q", a, data)
	}
	if err := m.Stop(ctx, b.ID, alice); err != nil {
		t.Fatal(err)
	}
	// ended: everything allowlisted, and nothing else
	dir := m.runDir(b.ID)
	_ = os.WriteFile(filepath.Join(dir, "ep-000", LogFile), []byte("log"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644)
	outside := filepath.Join(t.TempDir(), "outside.dm2")
	_ = os.WriteFile(outside, []byte("outside"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "ep-000", DemoDir, "02-link.dm2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "ep-000"), filepath.Join(dir, "ep-001")); err != nil {
		t.Fatal(err)
	}
	ended, _ := m.Get(ctx, b.ID, alice)
	if got := names(ended.Artifacts); got != "run:run.json trace:ep-000/trace.jsonl.gz demo:ep-000/demos/00-demo1.dm2 demo:ep-000/demos/01-demo1.dm2" {
		t.Fatalf("ended artifacts %s", got)
	}
	if a, data := read("ep-000/trace.jsonl.gz"); data != "whole" || a.ContentType != "application/x-ndjson" || a.ContentEncoding != "gzip" {
		t.Fatalf("trace %+v", a)
	}
	if a, data := read("run.json"); !strings.Contains(data, b.ID) || a.ContentType != "application/json" || a.ContentEncoding != "" {
		t.Fatalf("run.json %+v", a)
	}
	for _, name := range []string{
		"", "bot.json", "secret.txt", "ep-000/log.txt", "ep-000", "ep-000/demos", "../" + b.ID + "/run.json",
		"ep-000/../run.json", "ep-000/demos/../../run.json", "/etc/passwd", "ep-000//trace.jsonl.gz", "./run.json",
		"ep-000/demos/02-link.dm2", "ep-001/trace.jsonl.gz", "ep-000/demos/.hidden.dm2", "ep-000/demos/00-x.dm2/",
		"ep-000/demos/00-demo1.DM2", "ep-0/episode.json", "RUN.JSON", "ep-000\\trace.jsonl.gz",
	} {
		if a, err := m.OpenArtifact(ctx, b.ID, name, alice); !errors.Is(err, api.ErrBotNotFound) {
			if a != nil {
				a.Content.Close()
			}
			t.Errorf("artifact %q: %v", name, err)
		}
	}
	if _, err := m.OpenArtifact(ctx, "../"+filepath.Base(m.cfg.Dir), "run.json", admin); !errors.Is(err, api.ErrBotNotFound) {
		t.Errorf("bot id traversal: %v", err)
	}
}

// writeRunDir writes an ended run directory: bot.json (with meta) or, for a
// command line run, run.json only.
func writeRunDir(t *testing.T, dir, id string, started time.Time, meta *botMeta) {
	t.Helper()
	d := filepath.Join(dir, id)
	if err := os.MkdirAll(filepath.Join(d, "ep-000"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := metrics.RunSummary{Schema: metrics.Schema, Run: id, Outcome: OutcomeCompleted, Backend: "scripted", Maps: []string{"demo1"},
		Skill: 1, Started: started.UTC().Format(time.RFC3339), WallMs: 60000}
	if err := metrics.WriteJSON(filepath.Join(d, RunFile), s); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		meta.Schema, meta.ID, meta.StartedAt = BotSchema, id, started.UTC()
		if err := metrics.WriteJSON(filepath.Join(d, BotFile), meta); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerRunsDirectory(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		writeRunDir(t, dir, fmt.Sprintf("old-%d", i), t0.Add(time.Duration(i)*time.Hour),
			&botMeta{Name: "b", OwnerID: alice.ID, Backend: "scripted", Status: api.BotFinished})
	}
	writeRunDir(t, dir, "cli-run", t0.Add(10*time.Hour), nil)
	// a bot a previous process left running
	writeRunDir(t, dir, "crashed", t0.Add(11*time.Hour), &botMeta{Name: "c", OwnerID: bob.ID, Backend: "scripted", Status: api.BotRunning})
	// not runs: left alone by the retention
	_ = os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644)

	m, fr := newTestManager(t, func(c *ManagerConfig) { c.Dir = dir; c.Keep = 3 })
	ctx := context.Background()
	// the newest three ended runs remain, oldest first gone
	for i, keep := range []bool{false, false, false, false, true} {
		_, err := os.Stat(filepath.Join(dir, fmt.Sprintf("old-%d", i)))
		if keep != (err == nil) {
			t.Errorf("old-%d kept %v", i, err == nil)
		}
	}
	for _, d := range []string{"cli-run", "crashed", "notes", "README"} {
		if _, err := os.Stat(filepath.Join(dir, d)); err != nil {
			t.Errorf("%s removed", d)
		}
	}
	got, err := m.Get(ctx, "crashed", bob)
	if err != nil || got.Status != api.BotFailed || !strings.Contains(got.Reason, "interrupted") {
		t.Fatalf("interrupted bot %+v %v", got, err)
	}
	// a command line run is listed as a public, ownerless ended bot
	cli, err := m.Get(ctx, "cli-run", anon)
	if err != nil || cli.Status != api.BotFinished || !cli.Public || cli.OwnerID != 0 || cli.Backend != "scripted" ||
		!cli.StartedAt.Equal(t0.Add(10*time.Hour)) || cli.EndedAt == nil || !cli.EndedAt.Equal(t0.Add(10*time.Hour+time.Minute)) {
		t.Fatalf("cli run %+v %v", cli, err)
	}
	l, _ := m.List(ctx, anon)
	if len(l) != 1 || l[0].ID != "cli-run" || l[0].Summary != nil {
		t.Fatalf("anonymous list %+v", l)
	}
	// a new bot's end applies the retention again, never to live bots
	b, r := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	r.finish <- OutcomeCompleted
	waitEnded(t, m, b.ID)
	all, _ := m.List(ctx, admin)
	var ids []string
	for _, x := range all {
		ids = append(ids, x.ID)
	}
	if strings.Join(ids, ",") != b.ID+",crashed,cli-run" {
		t.Fatalf("after the retention: %v", ids)
	}
}

// Close stops every bot (as stopped, with its reason) and waits for the
// runs to end; then no bot starts.
func TestManagerClose(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxPerUser = 5 })
	ctx := context.Background()
	var ids []string
	for i := 0; i < 3; i++ {
		b, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
		ids = append(ids, b.ID)
	}
	m.Close()
	if n := m.liveCount(); n != 0 {
		t.Fatalf("%d live after Close", n)
	}
	for _, id := range ids {
		var meta botMeta
		if err := readJSONFile(filepath.Join(m.runDir(id), BotFile), &meta); err != nil || meta.Status != api.BotStopped ||
			meta.Reason != "the server shut down" {
			t.Errorf("%s: %+v %v", id, meta, err)
		}
		if _, err := os.Stat(filepath.Join(m.runDir(id), RunFile)); err != nil {
			t.Errorf("%s: no run.json", id)
		}
	}
	if _, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, alice); !errors.Is(err, api.ErrBotsDisabled) {
		t.Errorf("start after close: %v", err)
	}
	m.Close() // idempotent
}

func TestManagerMetrics(t *testing.T) {
	m, fr := newTestManager(t, nil)
	reg := prometheus.NewRegistry()
	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	_, r := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	bus := r.bus
	bus.Publish(trace.Event{Type: trace.TypeDecision, Map: "demo1", Body: trace.Decision{Lane: "tick", Intent: &trace.Intent{Fields: []trace.Field{
		{Name: "mode", Source: "model"}, {Name: "target", Source: "scripted", Fallback: "low_confidence"},
		{Name: "pickup", Source: "default", Fallback: strings.Repeat("x", 40)}}}}})
	bus.Publish(trace.Event{Type: trace.TypeDecision, Body: trace.Decision{Lane: "fast", LatencyMs: 150, Response: json.RawMessage(`{}`)}})
	bus.Publish(trace.Event{Type: trace.TypeAPICall, Body: trace.APICall{Backend: "scripted", Status: 429, Err: "rate limited"}})
	bus.Publish(trace.Event{Type: trace.TypeAPICall, Body: trace.APICall{Backend: "scripted", InputTokens: 900, CostUSD: 0.001}})
	bus.Publish(trace.Event{Type: trace.TypeDeath, Map: "demo1", Body: trace.Death{}})
	bus.Publish(trace.Event{Type: trace.TypeKill, Map: "demo1", Body: trace.Kill{}})
	bus.Publish(trace.Event{Type: trace.TypeLevelEnd, Map: "demo1", Body: trace.LevelEnd{Outcome: trace.OutcomeExit}})
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range families {
		for _, mt := range f.Metric {
			var labels []string
			for _, l := range mt.Label {
				labels = append(labels, l.GetName()+"="+l.GetValue())
			}
			v := mt.GetCounter().GetValue() + mt.GetGauge().GetValue() + float64(mt.GetHistogram().GetSampleCount())
			got[f.GetName()+"{"+strings.Join(labels, ",")+"}"] = fmt.Sprint(v)
			for _, l := range mt.Label {
				if strings.Contains(l.GetValue(), "2026") {
					t.Errorf("run id in a label: %s", f.GetName())
				}
			}
		}
	}
	for k, v := range map[string]string{
		"q2bot_runs_active{}": "1", "q2bot_viewers{}": "0", "q2bot_kills_total{}": "1",
		"q2bot_deaths_total{map=demo1}": "1", "q2bot_levels_completed_total{map=demo1}": "1",
		"q2bot_decisions_total{backend=scripted,source=model}":            "1",
		"q2bot_decisions_total{backend=scripted,source=scripted}":         "1",
		"q2bot_fallback_total{reason=low_confidence}":                     "1",
		"q2bot_fallback_total{reason=other}":                              "1",
		"q2bot_decision_latency_seconds{backend=scripted}":                "1",
		"q2bot_api_requests_total{backend=scripted,outcome=rate_limited}": "1",
		"q2bot_api_requests_total{backend=scripted,outcome=ok}":           "1",
		"q2bot_api_tokens_total{backend=scripted,direction=input}":        "900",
		"q2bot_api_cost_usd_total{backend=scripted}":                      "0.001",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %s", k, got[k], v)
		}
	}
	var g labelGuard
	g.max = 2
	if g.value("a") != "a" || g.value("b") != "b" || g.value("c") != "other" || g.value("a") != "a" || g.value("Bad Value") != "other" || g.value("") != "none" {
		t.Error("label guard")
	}
}

// dialFeed opens a decision feed of the manager's handler.
func dialFeed(ctx context.Context, srv *httptest.Server, id, ticket string) (*websocket.Conn, int, error) {
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/bots/" + id + "/decisions"
	if ticket != "" {
		u += "?ticket=" + ticket
	}
	c, resp, err := websocket.Dial(ctx, u, nil)
	code := 0
	if resp != nil {
		code = resp.StatusCode
	}
	return c, code, err
}

func feedServer(m *Manager) *httptest.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /ws/v1/bots/{id}/decisions", m.DecisionsHandler())
	mux.Handle("GET /ws/v1/bots/{id}/watch", m.WatchHandler())
	return httptest.NewServer(mux)
}

func TestDecisionFeedEndpoint(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.RequireTickets = true; c.MaxViewers = 1 })
	srv := feedServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, r := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)

	for _, tc := range []struct {
		id, ticket string
		code       int
	}{
		{"unknown", "x", http.StatusNotFound},
		{b.ID, "", http.StatusForbidden},
		{b.ID, "forged", http.StatusForbidden},
	} {
		if c, code, err := dialFeed(ctx, srv, tc.id, tc.ticket); err == nil || code != tc.code {
			if c != nil {
				c.CloseNow() //nolint:errcheck
			}
			t.Errorf("%s ticket %q: %d %v, want %d", tc.id, tc.ticket, code, err, tc.code)
		}
	}
	w, err := m.Watch(ctx, b.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	// a watch ticket does not open the feed (and is used up trying)
	if c, code, err := dialFeed(ctx, srv, b.ID, w.Ticket); err == nil || code != http.StatusForbidden {
		if c != nil {
			c.CloseNow() //nolint:errcheck
		}
		t.Errorf("watch ticket on the feed: %d %v", code, err)
	}
	c, _, err := dialFeed(ctx, srv, b.ID, w.DecisionsTicket)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow() //nolint:errcheck
	if c.Subprotocol() != "" {
		t.Errorf("subprotocol %q without asking", c.Subprotocol())
	}
	next := func() map[string]any {
		t.Helper()
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatal(err)
		}
		return msg
	}
	if h := next(); h["t"] != "hello" || h["bot"] != b.ID || h["backend"] != "scripted" {
		t.Fatalf("hello %v", h)
	}
	// one feed per bot here: the next viewer is refused before its ticket is spent
	w2, _ := m.Watch(ctx, b.ID, alice)
	if c2, code, err := dialFeed(ctx, srv, b.ID, w2.DecisionsTicket); err == nil || code != http.StatusServiceUnavailable {
		if c2 != nil {
			c2.CloseNow() //nolint:errcheck
		}
		t.Errorf("second feed: %d %v", code, err)
	}
	r.bus.Publish(trace.Event{Type: trace.TypeKill, Map: "demo1", SF: 9, Body: trace.Kill{Target: "e1", Class: "soldier"}})
	// skip the stats of before the kill: they come every second
	until := func(ok func(map[string]any) bool) map[string]any {
		t.Helper()
		for {
			if msg := next(); ok(msg) {
				return msg
			} else if msg["t"] != "stats" {
				t.Fatalf("unexpected %v", msg)
			}
		}
	}
	if e := until(func(m map[string]any) bool { return m["t"] == "event" }); e["kind"] != "kill" || e["sf"] != 9.0 {
		t.Fatalf("event %v", e)
	}
	until(func(m map[string]any) bool { return m["t"] == "stats" && m["kills"] == 1.0 })
	if err := m.Stop(ctx, b.ID, alice); err != nil {
		t.Fatal(err)
	}
	for {
		msg := next()
		if msg["t"] == "bye" {
			if msg["status"] != api.BotStopped {
				t.Fatalf("bye %v", msg)
			}
			break
		}
	}
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("after bye: %v", err)
	}
	// an ended bot's feed is gone
	if c3, code, err := dialFeed(ctx, srv, b.ID, w2.DecisionsTicket); err == nil || code != http.StatusGone {
		if c3 != nil {
			c3.CloseNow() //nolint:errcheck
		}
		t.Errorf("ended feed: %d %v", code, err)
	}
}

// Without RequireTickets (-insecure-ws) a public bot's streams open
// without a ticket; a private bot's still need one, and a given ticket
// must be valid.
func TestInsecureStreams(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxPerUser = 2 })
	srv := feedServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pub, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted", Public: true}, alice)
	priv, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	c, _, err := dialFeed(ctx, srv, pub.ID, "")
	if err != nil {
		t.Fatalf("public feed without a ticket: %v", err)
	}
	c.CloseNow() //nolint:errcheck
	for _, tc := range []struct{ id, ticket string }{{priv.ID, ""}, {pub.ID, "forged"}} {
		if c, code, err := dialFeed(ctx, srv, tc.id, tc.ticket); err == nil || code != http.StatusForbidden {
			if c != nil {
				c.CloseNow() //nolint:errcheck
			}
			t.Errorf("%s %q: %d %v", tc.id, tc.ticket, code, err)
		}
	}
	watch := func(id string) int {
		resp, err := http.Get(srv.URL + "/ws/v1/bots/" + id + "/watch")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	// a plain GET: the public bot's ticket check passes (no WebSocket upgrade), the private one's does not
	if code := watch(pub.ID); code == http.StatusForbidden || code == http.StatusNotFound {
		t.Errorf("public watch without a ticket: %d", code)
	}
	if code := watch(priv.ID); code != http.StatusForbidden {
		t.Errorf("private watch without a ticket: %d", code)
	}
	if code := watch("nope"); code != http.StatusNotFound {
		t.Errorf("unknown watch: %d", code)
	}
}

// The feed reports what it dropped as a gap and ends with bye once the
// run's status is final.
func TestFeedGapAndBye(t *testing.T) {
	m, _ := newTestManager(t, nil)
	bus := trace.NewBus("r", nil)
	b := &managedBot{id: "r", stats: &liveStats{metrics: m.metrics}, done: make(chan struct{}),
		meta: botMeta{ID: "r", Backend: "mock", Model: "jev-1.13.0", Maps: []string{"demo1"}, Status: api.BotRunning}}
	sub := bus.Subscribe(2)
	for i := 0; i < 10; i++ {
		bus.Publish(trace.Event{Type: trace.TypeKill, SF: int32(i), Body: trace.Kill{Target: "e1"}})
	}
	bus.Close()
	b.meta.Status = api.BotFinished
	close(b.done)
	var got []string
	ok := m.runFeed(context.Background(), b, sub, func(v any) bool {
		data, _ := marshalFeed(v)
		got = append(got, string(data))
		return true
	}, time.Hour)
	want := []string{
		`{"t":"hello","bot":"r","backend":"mock","model":"jev-1.13.0","maps":["demo1"]}`,
		`{"t":"gap","dropped":8}`,
		`{"t":"event","kind":"kill","sf":8,"lvl":0,"map":"","data":{"class":"","target":"e1"}}`,
		`{"t":"event","kind":"kill","sf":9,"lvl":0,"map":"","data":{"class":"","target":"e1"}}`,
		`{"t":"bye","status":"finished"}`,
	}
	if !ok || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("feed %v:\n%s", ok, strings.Join(got, "\n"))
	}
}

// A bot prepared but never started (the server closed meanwhile) runs
// nothing, releases its run and leaves no directory behind.
func TestManagerDiscard(t *testing.T) {
	m, fr := newTestManager(t, nil)
	b, err := m.newBot(emptyFS{}, api.BotSpec{Backend: "scripted"}, alice, "x", []string{"demo1"}, 1, Latency{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(b.dir, BotFile)); err != nil {
		t.Fatal(err)
	}
	b.discard()
	if _, err := os.Stat(b.dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run directory left: %v", err)
	}
	select {
	case <-fr.last(t).entered: // the run ended at once on its cancelled context
	default:
		t.Error("the run was not released")
	}
	if l, _ := m.List(context.Background(), admin); len(l) != 0 {
		t.Errorf("listed %+v", l)
	}
}

// An account that is not an administrator starts bots at most
// StartLimiter's times per window; starts refused by the caps do not
// count, and nobody else is held back.
func TestManagerStartRate(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.StartLimiter = auth.NewLimiter(2, time.Hour) })
	ctx := context.Background()
	a, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	if _, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, alice); !errors.Is(err, api.ErrBotLimit) || strings.Contains(err.Error(), "too often") {
		t.Fatalf("second live bot of alice: %v", err)
	}
	if err := m.Stop(ctx, a.ID, alice); err != nil {
		t.Fatal(err)
	}
	a, _ = startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, alice)
	if err := m.Stop(ctx, a.ID, alice); err != nil {
		t.Fatal(err)
	}
	_, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, alice)
	if !errors.Is(err, api.ErrBotLimit) || !strings.Contains(err.Error(), "too often") {
		t.Fatalf("third start of alice: %v", err)
	}
	startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, bob)
	for i := 0; i < 3; i++ {
		startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, admin) // administrators: no start rate
	}
}

// Each account keeps at most KeepPerUser ended runs within Keep, so that
// a user starting bots in a loop does not evict everyone else's runs;
// administrators', the server's and command line runs are not capped.
func TestManagerRetentionPerUser(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	n := 0
	add := func(id string, meta *botMeta) {
		if meta != nil {
			meta.Name, meta.Backend, meta.Status = "b", "scripted", api.BotFinished
		}
		writeRunDir(t, dir, id, t0.Add(time.Duration(n)*time.Hour), meta)
		n++
	}
	for i := 0; i < 4; i++ {
		add(fmt.Sprintf("alice-%d", i), &botMeta{OwnerID: alice.ID})
	}
	add("bob-0", &botMeta{OwnerID: bob.ID})
	for i := 0; i < 3; i++ {
		add(fmt.Sprintf("admin-%d", i), &botMeta{OwnerID: admin.ID, OwnerAdmin: true})
	}
	add("server-0", &botMeta{})
	add("cli-0", nil)
	// newest first: cli-0 server-0 admin-2 admin-1 admin-0 bob-0 alice-3 alice-2 | alice-1 (Keep has room, alice's share not) alice-0
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.Dir = dir; c.Keep = 9; c.KeepPerUser = 2 })
	var left []string
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		left = append(left, e.Name())
	}
	if got := strings.Join(left, ","); got != "admin-0,admin-1,admin-2,alice-2,alice-3,bob-0,cli-0,server-0" {
		t.Fatalf("after the retention: %s", got)
	}
	// a bot records whether an administrator started it
	b, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted"}, admin)
	var meta botMeta
	if err := readJSONFile(filepath.Join(dir, b.ID, BotFile), &meta); err != nil || !meta.OwnerAdmin || meta.OwnerID != admin.ID {
		t.Fatalf("admin's bot.json %+v %v", meta, err)
	}
}

// Accounts that are not administrators cannot evict the runs of
// administrators, the server or the command line, however many of them
// start bots: their runs are kept in the room the others leave, and only
// newer privileged runs evict older ones.
func TestManagerRetentionPrivileged(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	n := 0
	add := func(id string, meta *botMeta) {
		if meta != nil {
			meta.Name, meta.Backend, meta.Status = "b", "scripted", api.BotFinished
		}
		writeRunDir(t, dir, id, t0.Add(time.Duration(n)*time.Minute), meta)
		n++
	}
	add("admin-0", &botMeta{OwnerID: admin.ID, OwnerAdmin: true})
	add("server-0", &botMeta{})
	add("cli-0", nil)
	for u := int64(1); u <= 5; u++ {
		for i := 0; i < 10; i++ {
			add(fmt.Sprintf("user%d-%d", u, i), &botMeta{OwnerID: 100 + u})
		}
	}
	newTestManager(t, func(c *ManagerConfig) { c.Dir = dir; c.Keep = 20; c.KeepPerUser = 10 })
	left := map[string]bool{}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		left[e.Name()] = true
	}
	for _, id := range []string{"admin-0", "server-0", "cli-0"} {
		if !left[id] {
			t.Errorf("%s evicted by the accounts' newer runs", id)
		}
	}
	// the accounts' 17 newest runs fill the rest, within their shares
	if len(left) != 20 || !left["user5-9"] || !left["user4-3"] || left["user4-2"] {
		t.Fatalf("%d runs left: %v", len(left), left)
	}

	// privileged runs are evicted by newer privileged runs, oldest first
	dir = t.TempDir()
	n = 0
	for i := 0; i < 4; i++ {
		add(fmt.Sprintf("admin-%d", i), &botMeta{OwnerID: admin.ID, OwnerAdmin: true})
	}
	add("alice-0", &botMeta{OwnerID: alice.ID})
	newTestManager(t, func(c *ManagerConfig) { c.Dir = dir; c.Keep = 3 })
	var names []string
	ents, _ = os.ReadDir(dir)
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if got := strings.Join(names, ","); got != "admin-1,admin-2,admin-3" {
		t.Fatalf("after the retention: %s", got)
	}
}

// A command line run still going (its run.json a progress snapshot,
// outcome incomplete) is listed as running with only its complete
// artifacts and is never pruned; once it stops writing it counts as
// failed.
func TestManagerCLIRunInProgress(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeRunDir(t, dir, "done", now.Add(-3*time.Hour), nil)
	for _, id := range []string{"dead", "going"} {
		started := now.Add(-2 * time.Hour)
		if id == "going" {
			started = now.Add(-time.Hour)
		}
		writeRunDir(t, dir, id, started, nil)
		d := filepath.Join(dir, id)
		s := metrics.RunSummary{Schema: metrics.Schema, Run: id, Outcome: metrics.OutcomeIncomplete, Backend: "scripted",
			Maps: []string{"demo1"}, Started: started.UTC().Format(time.RFC3339), WallMs: 1000}
		if err := metrics.WriteJSON(filepath.Join(d, RunFile), s); err != nil {
			t.Fatal(err)
		}
		for _, ep := range []string{"ep-000", "ep-001"} {
			_ = os.MkdirAll(filepath.Join(d, ep, DemoDir), 0o755)
			_ = os.WriteFile(filepath.Join(d, ep, TraceFile), []byte("trace"), 0o644)
			_ = os.WriteFile(filepath.Join(d, ep, DemoDir, "00-demo1.dm2"), []byte("demo"), 0o644)
		}
		_ = os.WriteFile(filepath.Join(d, "ep-000", EpisodeFile), []byte("{}"), 0o644)
		_ = os.WriteFile(filepath.Join(d, "ep-000", DemoDir, "01-demo1.dm2"), []byte("demo"), 0o644)
		if id == "dead" {
			ageRunDir(t, d, now.Add(-time.Hour))
		}
	}
	m, _ := newTestManager(t, func(c *ManagerConfig) { c.Dir = dir; c.Keep = 1 })
	ctx := context.Background()
	if _, err := os.Stat(filepath.Join(dir, "done")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the oldest ended run was kept: %v", err)
	}
	going, err := m.Get(ctx, "going", anon)
	if err != nil || going.Status != api.BotRunning || going.EndedAt != nil || going.Reason != "" || len(going.Summary) == 0 {
		t.Fatalf("running cli run %+v %v", going, err)
	}
	var names []string
	for _, a := range going.Artifacts {
		names = append(names, a.Name)
	}
	if got := strings.Join(names, " "); got != "run.json ep-000/episode.json ep-000/demos/00-demo1.dm2" {
		t.Fatalf("running cli run artifacts %s", got)
	}
	for _, name := range []string{"ep-000/trace.jsonl.gz", "ep-001/trace.jsonl.gz", "ep-001/demos/00-demo1.dm2"} {
		if _, err := m.OpenArtifact(ctx, "going", name, anon); !errors.Is(err, api.ErrBotNotFound) {
			t.Errorf("running cli run's %s: %v", name, err)
		}
	}
	if _, err := m.Watch(ctx, "going", anon); !errors.Is(err, api.ErrBotNotLive) {
		t.Errorf("watch a cli run: %v", err)
	}
	dead, err := m.Get(ctx, "dead", anon)
	if err != nil || dead.Status != api.BotFailed || dead.Reason != "the run did not end" || dead.EndedAt == nil {
		t.Fatalf("dead cli run %+v %v", dead, err)
	}
	// the retention ran with the run going: it stays, the other goes
	m.prune()
	if _, err := os.Stat(filepath.Join(dir, "going")); err != nil {
		t.Fatalf("running cli run pruned: %v", err)
	}
	// it stops writing: failed, and its trace is served
	ageRunDir(t, filepath.Join(dir, "going"), now.Add(-time.Hour))
	going, _ = m.Get(ctx, "going", anon)
	if going.Status != api.BotFailed || going.EndedAt == nil {
		t.Fatalf("stale cli run %+v", going)
	}
	a, err := m.OpenArtifact(ctx, "going", "ep-001/trace.jsonl.gz", anon)
	if err != nil {
		t.Fatal(err)
	}
	a.Content.Close()
}

// ageRunDir sets the times of every file of a run directory to at.
func ageRunDir(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// stubbornRun is a run that ignores its cancellation until released.
type stubbornRun struct {
	bus      *trace.Bus
	entered  chan struct{}
	release  chan struct{}
	released sync.Once
}

func (s *stubbornRun) Bus() *trace.Bus { return s.bus }

func (s *stubbornRun) Run(context.Context) (*metrics.RunSummary, error) {
	defer s.bus.Close()
	close(s.entered)
	<-s.release
	return &metrics.RunSummary{Schema: metrics.Schema, Outcome: OutcomeAborted, Reason: "context canceled"}, nil
}

func (s *stubbornRun) free() { s.released.Do(func() { close(s.release) }) }

// A bot taking longer than StopWait to end after its stop is still
// stopped: Stop is no error (DELETE answers 204, not 500).
func TestManagerStopSlow(t *testing.T) {
	m, _ := newTestManager(t, func(c *ManagerConfig) { c.StopWait = 50 * time.Millisecond })
	sr := &stubbornRun{bus: trace.NewBus("x", nil), entered: make(chan struct{}), release: make(chan struct{})}
	defer sr.free()
	m.newRun = func(Config) (botRun, error) { return sr, nil }
	ctx := context.Background()
	b, err := m.Start(ctx, api.BotSpec{Backend: "scripted"}, alice)
	if err != nil {
		t.Fatal(err)
	}
	<-sr.entered
	if err := m.Stop(ctx, b.ID, alice); err != nil {
		t.Fatalf("slow stop: %v", err)
	}
	if got, _ := m.Get(ctx, b.ID, alice); !api.BotLive(got.Status) {
		t.Fatalf("still ending %+v", got)
	}
	if err := m.Stop(ctx, b.ID, alice); err != nil {
		t.Fatalf("stop again: %v", err)
	}
	// a cancelled request does say so
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := m.Stop(cctx, b.ID, alice); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop with a cancelled request: %v", err)
	}
	sr.free()
	waitEnded(t, m, b.ID)
	if got, _ := m.Get(ctx, b.ID, alice); got.Status != api.BotStopped || got.Reason != "stopped by its owner" {
		t.Fatalf("stopped %+v", got)
	}
}

// Watch tickets are bounded per bot: past 4×MaxViewers outstanding the
// oldest are revoked (a viewer redeems its own at once); a redeemed
// ticket no longer counts, and the development path's own tickets do
// not outlive their request.
func TestManagerWatchTickets(t *testing.T) {
	tickets := auth.NewTickets(0)
	m, fr := newTestManager(t, func(c *ManagerConfig) { c.MaxViewers = 2; c.Tickets = tickets })
	srv := feedServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pub, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted", Public: true}, alice)
	b := m.live(pub.ID)
	var ws []api.BotWatch
	for i := 0; i < 10; i++ {
		w, err := m.Watch(ctx, pub.ID, anon)
		if err != nil {
			t.Fatal(err)
		}
		ws = append(ws, w)
	}
	if n, p := tickets.Len(), b.pendingTickets(); n != 8 || p != 8 {
		t.Fatalf("%d tickets outstanding (%d of the bot), want 8", n, p)
	}
	// the newest open the feed, and are forgotten then; the oldest were revoked
	c, _, err := dialFeed(ctx, srv, pub.ID, ws[9].DecisionsTicket)
	if err != nil {
		t.Fatal(err)
	}
	c.CloseNow() //nolint:errcheck
	if p := b.pendingTickets(); p != 7 {
		t.Errorf("%d tickets of the bot outstanding after a redeem, want 7", p)
	}
	if c, code, err := dialFeed(ctx, srv, pub.ID, ws[0].DecisionsTicket); err == nil || code != http.StatusForbidden {
		if c != nil {
			c.CloseNow() //nolint:errcheck
		}
		t.Errorf("revoked ticket: %d %v", code, err)
	}
	// without RequireTickets a public bot's view needs no ticket: the
	// one made for the request is gone with it, even when refused
	before := tickets.Len()
	for _, closed := range []bool{false, true} {
		if closed {
			_ = b.hub.Close() // the hub refuses viewers before redeeming
		}
		resp, err := http.Get(srv.URL + "/ws/v1/bots/" + pub.ID + "/watch")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if closed && resp.StatusCode != http.StatusGone {
			t.Errorf("closed hub: %d", resp.StatusCode)
		}
		if n := tickets.Len(); n > before {
			t.Errorf("hub closed %v: %d tickets after the request, %d before", closed, n, before)
		}
	}
}

// One address has at most MaxViewersPerIP connections to each stream of
// a bot; a refused one keeps its ticket.
func TestManagerStreamsPerIP(t *testing.T) {
	m, fr := newTestManager(t, func(c *ManagerConfig) {
		c.MaxViewersPerIP = 1
		c.RemoteIP = func(r *http.Request) string { return r.Header.Get("X-Test-IP") }
	})
	srv := feedServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pub, _ := startBot(t, m, fr, api.BotSpec{Backend: "scripted", Public: true}, alice)
	dial := func(ticket, ip string) (*websocket.Conn, int, error) {
		u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/v1/bots/" + pub.ID + "/decisions?ticket=" + ticket
		var opt websocket.DialOptions
		if ip != "" {
			opt.HTTPHeader = http.Header{"X-Test-IP": {ip}}
		}
		c, resp, err := websocket.Dial(ctx, u, &opt)
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		return c, code, err
	}
	watch := func() api.BotWatch {
		w, err := m.Watch(ctx, pub.ID, anon)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	c1, _, err := dial(watch().DecisionsTicket, "")
	if err != nil {
		t.Fatal(err)
	}
	w2 := watch()
	if c, code, err := dial(w2.DecisionsTicket, ""); err == nil || code != http.StatusServiceUnavailable {
		if c != nil {
			c.CloseNow() //nolint:errcheck
		}
		t.Fatalf("second feed of the address: %d %v", code, err)
	}
	// another address is welcome
	c3, _, err := dial(watch().DecisionsTicket, "192.0.2.7")
	if err != nil {
		t.Fatalf("feed of another address: %v", err)
	}
	c3.CloseNow() //nolint:errcheck
	// the refused ticket opens the feed once the first viewer has left
	c1.CloseNow() //nolint:errcheck
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		c, code, err := dial(w2.DecisionsTicket, "")
		if err == nil {
			c.CloseNow() //nolint:errcheck
			break
		}
		if code != http.StatusServiceUnavailable || time.Now().After(deadline) {
			t.Fatalf("feed after the first left: %d %v", code, err)
		}
	}
	// the view: the address's slot is checked before the ticket
	b := m.live(pub.ID)
	get := func() int {
		resp, err := http.Get(srv.URL + "/ws/v1/bots/" + pub.ID + "/watch?ticket=forged")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if !b.acquireIP(streamWatch, "127.0.0.1", 1) {
		t.Fatal("the view's slot is taken")
	}
	if code := get(); code != http.StatusServiceUnavailable {
		t.Errorf("view of a full address: %d", code)
	}
	b.releaseIP(streamWatch, "127.0.0.1")
	if code := get(); code != http.StatusForbidden {
		t.Errorf("view with a forged ticket: %d", code)
	}
}
