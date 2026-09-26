// Package api is the HTTP API (docs/ASSETS.md, docs/plans/0001-port-plan.md
// "API"): accounts, pak upload and ingest, paksets and their asset index,
// content-addressed asset serving, saves, settings, games, health and
// metrics. The game server binary mounts NewRouter next to its WebSocket
// handler.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
)

// GameSpec describes a game instance to create.
type GameSpec struct {
	OwnerID int64  `json:"-"`
	Name    string `json:"name"`
	// Mode is "sp", "coop", "dm" or "ctf".
	Mode string `json:"mode"`
	// Map is the map name without "maps/" and ".bsp" (e.g. "demo1").
	Map string `json:"map"`
	// Pakset is the pakset id supplying the game data (default "demo").
	Pakset     string `json:"pakset"`
	MaxPlayers int    `json:"maxPlayers"`
	Public     bool   `json:"public"`
	// Cvars are server cvars to set before the map starts
	// (e.g. {"skill":"1","dmflags":"16"}).
	Cvars map[string]string `json:"cvars,omitempty"`
	// LoadSlot optionally starts from a save slot of the owner.
	LoadSlot string `json:"loadSlot,omitempty"`
}

// GameInfo describes a running game instance.
type GameInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	OwnerID    int64     `json:"ownerId"`
	Mode       string    `json:"mode"`
	Map        string    `json:"map"`
	Pakset     string    `json:"pakset"`
	Public     bool      `json:"public"`
	Players    int       `json:"players"`
	MaxPlayers int       `json:"maxPlayers"`
	StartedAt  time.Time `json:"startedAt"`
}

// Player identifies the account joining a game.
type Player struct {
	UserID      int64
	DisplayName string
}

// ErrGameNotFound is returned by GameHost for unknown ids.
var ErrGameNotFound = errors.New("game not found")

// ErrGameFull is returned by GameHost.Join when no slot is free.
var ErrGameFull = errors.New("game full")

// ErrGameInvalid is wrapped by GameHost.Create errors caused by the spec
// (unknown mode, forbidden cvar, ...); the API answers 400 with the message.
var ErrGameInvalid = errors.New("invalid game settings")

// GameHost is implemented by the game server's instance manager
// (internal/host). The API only validates input, checks ownership and
// persists the registry row; everything else is the host's business.
type GameHost interface {
	// Create starts an instance and returns its id.
	Create(ctx context.Context, spec GameSpec) (string, error)
	// List returns all running instances.
	List(ctx context.Context) ([]GameInfo, error)
	// Get returns one instance or ErrGameNotFound.
	Get(ctx context.Context, id string) (GameInfo, error)
	// Join issues a one-time ticket (see auth.TicketService) and returns it
	// with the WebSocket URL the client must open
	// (e.g. "wss://host/ws/v1/games/{id}?ticket=..." or a relative
	// "/ws/v1/games/{id}"; the ticket is always returned separately too).
	Join(ctx context.Context, id string, p Player) (ticket, wsURL string, err error)
	// Stop shuts an instance down.
	Stop(ctx context.Context, id string) error
}

// Deps are the router's collaborators. Host may be nil (games endpoints
// then answer 503).
type Deps struct {
	Config  config.Config
	Log     *slog.Logger
	Repo    db.Repo
	Store   blob.Store
	Auth    *auth.Service
	Tickets auth.TicketService
	Catalog *Catalog
	Host    GameHost
	Metrics *prometheus.Registry
	// LoginLimiterIP / LoginLimiterEmail rate-limit POST /auth/login
	// (defaults: 20/min per IP, 10 per 10 min per email).
	LoginLimiterIP    *auth.Limiter
	LoginLimiterEmail *auth.Limiter
}

type server struct {
	Deps
	metrics *httpMetrics
}

// NewRouter returns the HTTP handler for every API route:
//
//	/healthz /readyz /metrics
//	/api/v1/auth/{register,login,logout,me}
//	/api/v1/paks /api/v1/paks/{id} /api/v1/jobs/{id}
//	/api/v1/paksets[/{id}[/index]]
//	/api/v1/maps /api/v1/maps/{name}/manifest
//	/api/v1/saves[/{slot}[/data]] /api/v1/settings
//	/api/v1/games[/{id}[/join]] /api/v1/games/from-save
//	/assets/{sha256}
func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Metrics == nil {
		d.Metrics = prometheus.NewRegistry()
	}
	if d.LoginLimiterIP == nil {
		d.LoginLimiterIP = auth.NewLimiter(20, time.Minute)
	}
	if d.LoginLimiterEmail == nil {
		d.LoginLimiterEmail = auth.NewLimiter(10, 10*time.Minute)
	}
	s := &server{Deps: d, metrics: newHTTPMetrics(d.Metrics)}
	mux := http.NewServeMux()
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, s.route(pattern, fn)) }

	h("GET /healthz", s.healthz)
	h("GET /readyz", s.readyz)
	mux.Handle("GET /metrics", s.route("GET /metrics", s.metricsHandler().ServeHTTP))

	h("POST /api/v1/auth/register", s.register)
	h("POST /api/v1/auth/login", s.login)
	h("POST /api/v1/auth/logout", s.logout)
	h("GET /api/v1/auth/me", s.me)

	h("POST /api/v1/paks", s.uploadPak)
	h("GET /api/v1/paks", s.listPaks)
	h("GET /api/v1/paks/{id}", s.getPak)
	h("GET /api/v1/jobs/{id}", s.getJob)

	h("GET /api/v1/paksets", s.listPaksets)
	h("POST /api/v1/paksets", s.createPakset)
	h("GET /api/v1/paksets/{id}", s.getPakset)
	h("PUT /api/v1/paksets/{id}", s.updatePakset)
	h("DELETE /api/v1/paksets/{id}", s.deletePakset)
	h("GET /api/v1/paksets/{id}/index", s.paksetIndex)

	h("GET /api/v1/maps", s.listMaps)
	h("GET /api/v1/maps/{name}/manifest", s.mapManifest)

	h("GET /assets/{sha256}", s.asset)

	h("GET /api/v1/saves", s.listSaves)
	h("GET /api/v1/saves/{slot}", s.getSave)
	h("GET /api/v1/saves/{slot}/data", s.getSaveData)
	h("DELETE /api/v1/saves/{slot}", s.deleteSave)

	h("GET /api/v1/settings", s.getSettings)
	h("PUT /api/v1/settings", s.putSettings)

	h("POST /api/v1/games", s.createGame)
	h("POST /api/v1/games/from-save", s.createGameFromSave)
	h("GET /api/v1/games", s.listGames)
	h("GET /api/v1/games/{id}", s.getGame)
	h("POST /api/v1/games/{id}/join", s.joinGame)
	h("DELETE /api/v1/games/{id}", s.deleteGame)

	h("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})

	var handler http.Handler = mux
	handler = s.withUser(handler)
	handler = s.csrf(handler)
	handler = s.cors(handler)
	handler = s.logRequests(handler)
	handler = s.recoverer(handler)
	handler = requestID(handler)
	return handler
}
