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
	"syscall"
	"time"

	"quake2web/server/internal/api"
	"quake2web/server/internal/config"
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
	games.Close() // autosaves single player games, disconnects clients
	return nil
}

// stackOptions are the non-environment settings of a server stack.
type stackOptions struct {
	Game         host.GameModule // default host.RealGame
	IdleTimeout  time.Duration
	ReapInterval time.Duration
	InsecureWS   bool
}

// stack is a fully wired server: API app, game host and the HTTP handler
// serving both (the WebSocket endpoint and the API router).
type stack struct {
	App     *api.App
	Games   *host.Games
	Handler http.Handler
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

	mux := http.NewServeMux()
	mux.Handle("GET /ws/v1/games/{id}", games.Handler())
	mux.Handle("/", api.NewRouter(app.Deps))
	return &stack{App: app, Games: games, Handler: mux}, nil
}

// Close stops the games (autosaving single player ones) and the API app.
func (s *stack) Close() {
	s.Games.Close()
	s.App.Close()
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
