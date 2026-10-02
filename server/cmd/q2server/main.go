// Command q2server is the production Quake 2 web server: the HTTP API
// (accounts, paks, paksets, assets, saves, settings, games; internal/api),
// the game instance host reachable over WebSocket (/ws/v1/games/{id}, one
// binary message == one netchan datagram, ADR-0001), Prometheus metrics and
// optionally raw UDP for the original C client and tools.
//
// Configuration comes from the environment (internal/config, see
// .env.example); flags only cover development extras:
//
//	q2server                                   # API + games, env config
//	q2server -map demo1 -mode dm -udp :27910   # plus a default DM game
//	q2server -gamemodule stub                  # the minimal stub game
//	q2server -bot demo1,demo2,demo3            # plus a public demo bot (enables bots)
//
// With Q2_BOTS_ENABLED the server also runs AI bots (agent/runner.Manager):
// /api/v1/bots, their live view at /ws/v1/bots/{id}/watch and their
// decision feed at /ws/v1/bots/{id}/decisions.
//
// Game data is read from ingested paksets (blob store + asset index), never
// from a local directory: the demo pak (Q2_DEMO_PAK) is ingested on start.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/runner"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
	"quake2web/server/internal/host"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
)

type cvarFlags map[string]string

func (c cvarFlags) String() string { return fmt.Sprint(map[string]string(c)) }
func (c cvarFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("want name=value, got %q", v)
	}
	c[k] = val
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "q2server:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", "", "HTTP listen address (default $Q2_HTTP_ADDR or :8080)")
	udp := flag.String("udp", "", "optional UDP listen address (e.g. :27910) routed to the default game (-map)")
	module := flag.String("gamemodule", "game", "game module: \"game\" (the internal/game port) or \"stub\" (internal/sv/stubgame)")
	mapName := flag.String("map", "", "start a default public game on this map (e.g. demo1)")
	mode := flag.String("mode", "dm", "mode of the default game: sp, coop, dm or ctf")
	pakset := flag.String("pakset", api.DemoPaksetID, "pakset of the default game")
	id := flag.String("id", "default", "id of the default game")
	idle := flag.Duration("idle", 5*time.Minute, "stop games that have had no players for this long")
	insecureWS := flag.Bool("insecure-ws", false, "allow WebSocket connections without a join ticket (development)")
	healthcheck := flag.Bool("healthcheck", false, "probe /healthz of the local server and exit 0 when healthy (container health checks)")
	cvars := cvarFlags{}
	flag.Var(cvars, "set", "cvar name=value for the default game (repeatable)")
	botMaps := flag.String("bot", "", "start a public server-owned bot on these maps (a prefix of the campaign, e.g. demo1,demo2,demo3); enables bots")
	botBackend := flag.String("bot-backend", runner.BackendScripted, "backend of the -bot bot: scripted, mock, constant, random or jev")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *listen != "" {
		cfg.HTTPAddr = *listen
	}
	if *healthcheck {
		return probe(cfg.HTTPAddr)
	}
	log := api.NewLogger(cfg)
	slog.SetDefault(log)
	if *botMaps != "" && !cfg.Bots.Enabled {
		log.Info("-bot: enabling bots")
		cfg.Bots.Enabled = true
	}

	var gm host.GameModule
	switch *module {
	case "game":
		gm = host.RealGame
	case "stub":
		gm = func(*crand.Rand) sv.GameFactory { return stubgame.New() }
	default:
		return fmt.Errorf("unknown -gamemodule %q", *module)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := newStack(ctx, cfg, log, stackOptions{Game: gm, IdleTimeout: *idle, InsecureWS: *insecureWS})
	if err != nil {
		return err
	}
	defer st.Close()
	games, h := st.Games, st.Games.Host()

	if *mapName != "" {
		gid, err := games.CreateWithID(ctx, *id, api.GameSpec{Name: "default", Mode: *mode, Map: *mapName,
			Pakset: *pakset, Public: true, Cvars: cvars})
		if err != nil {
			return fmt.Errorf("default game: %w", err)
		}
		log.Info("default game running", "id", gid, "map", *mapName, "mode", *mode)
	}

	if *botMaps != "" {
		b, err := startDemoBot(ctx, st.Bots, *botMaps, *botBackend)
		if err != nil {
			return fmt.Errorf("-bot: %w", err)
		}
		log.Info("demo bot running", "bot", b.ID, "maps", strings.Join(b.Maps, ","), "backend", b.Backend)
	}

	if *udp != "" {
		l, err := qnet.ListenUDP(*udp)
		if err != nil {
			return fmt.Errorf("udp: %w", err)
		}
		defer l.Close()
		log.Info("UDP listening", "addr", l.LocalAddr().String(), "game", *id)
		go func() { _ = h.ServeUDP(l, *id) }()
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           st.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "gamemodule", *module)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()

	select {
	case <-ctx.Done():
	case err := <-errc:
		if err != nil {
			return err
		}
	}
	log.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		log.Warn("http shutdown", "err", err)
	}
	if st.Bots != nil {
		st.Bots.Close() // the bots end their runs while their games still run
	}
	games.Close() // autosaves single player games, disconnects clients
	return nil
}

// stackOptions are the non-environment settings of a server stack.
type stackOptions struct {
	Game         host.GameModule // default host.RealGame
	IdleTimeout  time.Duration
	ReapInterval time.Duration
	InsecureWS   bool
	// RoutesDir and NavDir locate the bots' route tables and nav cache
	// ("": $Q2_ROUTES_DIR / $Q2_NAV_DIR, else the repository's).
	RoutesDir, NavDir string
}

// stack is a fully wired server: API app, game host, the bots (when
// enabled) and the HTTP handler serving them (the WebSocket endpoints and
// the API router).
type stack struct {
	App     *api.App
	Games   *host.Games
	Bots    *runner.Manager // nil unless cfg.Bots.Enabled
	Handler http.Handler

	closeOnce sync.Once
}

// newStack bootstraps the API (database, blob store, demo pakset) and the
// game host on top of it.
func newStack(ctx context.Context, cfg config.Config, log *slog.Logger, opt stackOptions) (*stack, error) {
	app, err := api.Bootstrap(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	h := host.New()
	h.RequireTickets = !opt.InsecureWS
	h.Logf = func(format string, args ...any) { log.Warn(strings.TrimRight(fmt.Sprintf(format, args...), "\n")) }
	h.OriginPatterns = originPatterns(cfg.CORSOrigins)
	if cfg.TrustProxy {
		// the proxy's own address would make every player the same IP for
		// the game's IP filters (addip/filterban); use the last X-Forwarded-For hop
		h.RemoteIP = func(r *http.Request) string {
			xff := r.Header.Get("X-Forwarded-For")
			if xff == "" {
				return ""
			}
			parts := strings.Split(xff, ",")
			ip := strings.TrimSpace(parts[len(parts)-1])
			if net.ParseIP(ip) == nil {
				return ""
			}
			return ip
		}
	}

	repo := app.Repo
	games := host.NewGames(host.GamesConfig{
		Host:         h,
		Indexes:      app.Catalog.PaksetIndex,
		Blobs:        app.Store,
		Saves:        app.Saves,
		Tickets:      app.Tickets,
		PublicWSURL:  cfg.PublicWSURL,
		Game:         opt.Game,
		IdleTimeout:  opt.IdleTimeout,
		ReapInterval: opt.ReapInterval,
		Log:          log,
		OnEnd: func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = repo.EndGame(ctx, id, time.Now())
		},
	})
	if err := games.RegisterMetrics(app.Metrics); err != nil {
		games.Close()
		app.Close()
		return nil, err
	}
	app.Host = games
	st := &stack{App: app, Games: games}

	mux := http.NewServeMux()
	mux.Handle("GET /ws/v1/games/{id}", games.Handler())
	if cfg.Bots.Enabled {
		bots, err := newBots(ctx, cfg, log, app, games, h, opt)
		if err != nil {
			games.Close()
			app.Close()
			return nil, err
		}
		st.Bots = bots
		app.Bots = bots
		mux.Handle("GET /ws/v1/bots/{id}/watch", bots.WatchHandler())
		mux.Handle("GET /ws/v1/bots/{id}/decisions", bots.DecisionsHandler())
	}
	mux.Handle("/", api.NewRouter(app.Deps))
	st.Handler = mux
	return st, nil
}

// newBots makes the bot manager: bots play realtime single-player games
// of the server's own host on the demo pakset, their daily spend is kept
// in the database and their streams follow the game WebSocket's rules.
func newBots(ctx context.Context, cfg config.Config, log *slog.Logger, app *api.App, games *host.Games, h *host.Host,
	opt stackOptions) (*runner.Manager, error) {
	bc := cfg.Bots
	fs := func(ctx context.Context) (runner.FileSystem, error) {
		idx, err := app.Catalog.PaksetIndex(ctx, api.DemoPaksetID)
		if err != nil {
			return nil, fmt.Errorf("pakset %s: %w", api.DemoPaksetID, err)
		}
		return host.NewIndexFS(idx, app.Store), nil
	}
	m, err := runner.NewManager(ctx, runner.ManagerConfig{
		Games: games, FS: fs, Pakset: api.DemoPaksetID, Tickets: app.Tickets, PublicWSURL: cfg.PublicWSURL,
		Dir: bc.RunsDir, Keep: bc.RunsKeep, MaxBots: bc.Max, MaxPerUser: bc.PerUser, AllowUsers: bc.AllowUsers,
		MaxViewers: bc.MaxViewers, MaxRun: bc.MaxRun,
		Jev: jev.Config{APIKey: trace.NewSecret(bc.APIKey.Reveal()), BaseURL: bc.JevBaseURL, AllowCustomBase: bc.JevAllowCustomBase,
			Model: bc.JevModel},
		BudgetUSDPerRun: bc.BudgetUSDPerRun, DailyUSD: bc.BudgetUSDPerDay, SpendStore: db.BotSpendStore(app.Repo),
		JevMaxQPS:      bc.JevMaxQPS,
		RoutesDir:      opt.RoutesDir,
		NavDir:         opt.NavDir,
		RequireTickets: h.RequireTickets, OriginPatterns: h.OriginPatterns, RemoteIP: h.RemoteIP,
		Log: log.With("component", "bots"),
	})
	if err != nil {
		return nil, fmt.Errorf("bots: %w", err)
	}
	if err := m.RegisterMetrics(app.Metrics); err != nil {
		m.Close()
		return nil, err
	}
	jevState := "off (no TYPESAFE_API_KEY)"
	if bc.APIKey.IsSet() {
		jevState = "on, model " + bc.JevModel
	}
	log.Info("bots enabled", "runs", bc.RunsDir, "max", bc.Max, "per_user", bc.PerUser, "allow_users", bc.AllowUsers, "jev", jevState)
	return m, nil
}

// startDemoBot starts the -bot bot: public and owned by the server (no
// account), on the comma separated maps.
func startDemoBot(ctx context.Context, bots *runner.Manager, maps, backend string) (api.BotInfo, error) {
	var list []string
	for _, m := range strings.Split(maps, ",") {
		if m = strings.TrimSpace(m); m != "" {
			list = append(list, m)
		}
	}
	return bots.Start(ctx, api.BotSpec{Name: "demo bot", Maps: list, Backend: backend, Public: true},
		api.BotUser{Name: "server", Admin: true})
}

// Close stops the bots (their runs end while their games still run), then
// the games (autosaving single player ones) and the API app. It is
// idempotent.
func (s *stack) Close() {
	s.closeOnce.Do(func() {
		if s.Bots != nil {
			s.Bots.Close()
		}
		s.Games.Close()
		s.App.Close()
	})
}

// probe GETs http://127.0.0.1:<port>/healthz.
func probe(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz: %s", resp.Status)
	}
	return nil
}

// originPatterns turns the allowed CORS origins into coder/websocket Origin
// host patterns.
func originPatterns(origins []string) []string {
	var out []string
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			out = append(out, u.Host)
		}
	}
	return out
}
