package runner

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/host"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv"
)

// testGames runs the bots' games on a plain host with the demo pak (the
// server's host.Games resolves paksets from its blob store instead).
type testGames struct {
	h    *host.Host
	fs   sv.FileSystem
	maps *sv.MapCache

	mu    sync.Mutex
	specs map[string]api.GameSpec
}

func newTestGames(fs sv.FileSystem) *testGames {
	return &testGames{h: host.New(), fs: fs, maps: sv.NewMapCache(), specs: map[string]api.GameSpec{}}
}

func (g *testGames) CreateWithID(_ context.Context, id string, spec api.GameSpec) (string, error) {
	dedicated, cvars, err := host.ModeSettings(spec)
	if err != nil {
		return "", err
	}
	rng := crand.New(1)
	if _, err := g.h.Create(host.InstanceConfig{ID: id, Server: sv.Config{FS: g.fs, Game: host.RealGame(rng), Maps: g.maps,
		Rand: rng, Dedicated: dedicated, Cvars: cvars}, Commands: []string{"map " + spec.Map}}); err != nil {
		return "", err
	}
	g.mu.Lock()
	g.specs[id] = spec
	g.mu.Unlock()
	return id, nil
}

func (g *testGames) Stop(_ context.Context, id string) error {
	inst, err := g.h.Get(id)
	if err != nil {
		return api.ErrGameNotFound
	}
	inst.Stop()
	return nil
}

func (g *testGames) Host() *host.Host { return g.h }

func (g *testGames) spec(id string) (api.GameSpec, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.specs[id]
	return s, ok
}

// TestManagerLiveJev runs a realtime jev bot (against the fake Jev server)
// through the Manager: it plays in a password-protected ownerless game,
// its decision feed shows the model's answers, its spend reaches the
// account's store, and the API key is nowhere: not in the run directory,
// the logs, BotInfo or the feed.
func TestManagerLiveJev(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	const key = "sk-q2bot-manager-5d1e0c93b7a24f68-secret"
	srv := jevtest.NewServer(jevtest.Options{APIKey: key, Policy: jevtest.NewScripted(scripted.Config{Seed: 1})})
	defer srv.Close()
	logs := &logBuffer{}
	games := newTestGames(fs)
	defer games.h.Shutdown()
	spend := budget.NewMemStore()
	tickets := auth.NewTickets(0)
	m, err := NewManager(context.Background(), ManagerConfig{
		Games: games, FS: func(context.Context) (FileSystem, error) { return fs, nil }, Tickets: tickets, Dir: t.TempDir(),
		Jev:             jevConfig(key, srv.URL()),
		BudgetUSDPerRun: 1, DailyUSD: 10, SpendStore: spend, JevMaxQPS: 50, RequireTickets: true,
		Log: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	b, err := m.Start(ctx, api.BotSpec{Backend: BackendJev, Maps: []string{"demo1"}}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if b.Model == "" || b.Backend != BackendJev || b.OwnerID != admin.ID {
		t.Fatalf("bot %+v", b)
	}
	feedSrv := feedServer(m)
	defer feedSrv.Close()
	w, err := m.Watch(ctx, b.ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	feed, _, err := dialFeed(ctx, feedSrv, b.ID, w.DecisionsTicket)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.CloseNow() //nolint:errcheck
	var feedText strings.Builder
	modelAnswered := false
	for !modelAnswered {
		_, data, err := feed.Read(ctx)
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
		feedText.Write(data)
		var msg struct {
			T          string
			Provenance map[string]string
			Questions  []json.RawMessage
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatal(err)
		}
		if msg.T == "decision" && len(msg.Questions) > 0 {
			for _, src := range msg.Provenance {
				modelAnswered = modelAnswered || src == trace.SourceModel
			}
		}
	}
	// the game: the server's own (no owner, private), password protected
	spec, ok := games.spec(gameID(b.ID, 0))
	if !ok || spec.OwnerID != 0 || spec.Public || spec.Mode != "sp" || spec.Map != "demo1" || len(spec.Cvars["password"]) < 16 ||
		spec.Cvars["skill"] != "1" {
		t.Fatalf("game spec %+v", spec)
	}
	info, err := m.Get(ctx, b.ID, admin)
	if err != nil || info.Status != api.BotRunning || info.Live == nil || info.Live.Decisions == 0 {
		t.Fatalf("live %+v %v", info, err)
	}
	if err := m.Stop(ctx, b.ID, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := games.h.Get(gameID(b.ID, 0)); err == nil {
		t.Error("the bot's game survived the bot")
	}
	info, err = m.Get(ctx, b.ID, admin)
	if err != nil || info.Status != api.BotStopped {
		t.Fatalf("stopped %+v %v", info, err)
	}
	var sum struct {
		API struct {
			OK      int     `json:"ok"`
			CostUSD float64 `json:"cost_usd"`
		} `json:"api"`
	}
	if err := json.Unmarshal(info.Summary, &sum); err != nil || sum.API.OK == 0 || sum.API.CostUSD <= 0 {
		t.Fatalf("summary %s: %v", info.Summary, err)
	}
	// the key went to the API, and nowhere else
	sent := false
	for _, c := range srv.Calls() {
		sent = sent || c.Header.Get("Authorization") == "Bearer "+key
	}
	if !sent {
		t.Error("the key never reached the API")
	}
	list, _ := m.List(ctx, admin)
	infoJSON, _ := json.Marshal([]any{info, list, w})
	needles := []string{key, key[len("sk-"):], "5d1e0c93b7a24f68"}
	check := func(what string, data []byte) {
		t.Helper()
		for _, n := range needles {
			if bytes.Contains(data, []byte(n)) {
				t.Errorf("%s holds the API key (%q)", what, n)
			}
		}
	}
	check("BotInfo", infoJSON)
	check("the decision feed", []byte(feedText.String()))
	m.Close() // flushes the spend
	check("the logs", []byte(logs.String()))
	files := 0
	err = filepath.WalkDir(m.runDir(b.ID), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if data, err = io.ReadAll(zr); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		files++
		check(path, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 4 {
		t.Fatalf("%d files in the run directory", files)
	}
	// the spend was persisted for the day
	day := time.Now().UTC().Format("2006-01-02")
	got, err := spend.LoadSpend(ctx, day)
	if err != nil || got <= 0 || got < sum.API.CostUSD*0.99 {
		t.Fatalf("persisted spend %v (run cost %v): %v", got, sum.API.CostUSD, err)
	}
	t.Logf("%d files, %d feed bytes, %d log bytes, $%.6f spent: no key", files, feedText.Len(), len(logs.String()), got)
}

func jevConfig(key, base string) jev.Config {
	return jev.Config{APIKey: trace.NewSecret(key), BaseURL: base, AllowCustomBase: true}
}
