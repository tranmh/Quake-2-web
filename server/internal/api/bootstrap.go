package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/config"
	"quake2web/server/internal/db"
)

// App bundles the router dependencies created by Bootstrap.
type App struct {
	Deps
	// Saves is the SaveStore the game server should use.
	Saves db.SaveStore
	stop  context.CancelFunc
	done  chan struct{}
}

// NewLogger builds the slog logger described by the config.
func NewLogger(cfg config.Config) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if strings.EqualFold(cfg.LogFormat, "text") {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

// Bootstrap opens the blob store and database (running migrations under an
// advisory lock), creates the auth, ticket and catalog services and ingests
// the demo pak. Without DATABASE_URL an in-memory repository is used.
//
// Typical use by cmd/q2server:
//
//	app, err := api.Bootstrap(ctx, cfg, log)
//	host := host.New(..., app.Tickets, app.Saves, app.Metrics)
//	app.Host = host
//	mux.Handle("/", api.NewRouter(app.Deps))
//	defer app.Close()
func Bootstrap(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	if log == nil {
		log = NewLogger(cfg)
	}
	store, err := blob.Open(cfg.BlobDir)
	if err != nil {
		return nil, fmt.Errorf("blob store: %w", err)
	}
	var repo db.Repo
	if cfg.DatabaseURL != "" {
		pg, err := db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("database: %w", err)
		}
		if cfg.MigrateOnStart {
			if err := pg.Migrate(ctx); err != nil {
				pg.Close()
				return nil, fmt.Errorf("migrations: %w", err)
			}
		}
		repo = pg
	} else {
		log.Warn("DATABASE_URL not set: using the in-memory repository (accounts and saves are lost on restart)")
		repo = db.NewMemory()
	}
	app := &App{
		Deps: Deps{
			Config:  cfg,
			Log:     log,
			Repo:    repo,
			Store:   store,
			Auth:    auth.NewService(repo, cfg.SessionTTL),
			Tickets: auth.NewTickets(auth.TicketTTL),
			Catalog: NewCatalog(repo, store, log, cfg.IngestWorkers, cfg.UploadDir),
			Metrics: NewRegistry(),
		},
		Saves: db.NewSaveStore(repo),
		done:  make(chan struct{}),
	}
	if cfg.DemoPak != "" {
		if _, err := os.Stat(cfg.DemoPak); err != nil {
			log.Warn("demo pak not found; the public \"demo\" pakset is unavailable", "path", cfg.DemoPak, "err", err)
		} else {
			t0 := time.Now()
			if _, err := app.Catalog.EnsureDemo(ctx, cfg.DemoPak); err != nil {
				app.Close()
				return nil, fmt.Errorf("demo pak: %w", err)
			}
			log.Info("demo pakset ready", "path", cfg.DemoPak, "took_ms", time.Since(t0).Milliseconds())
		}
	}
	sweepCtx, stop := context.WithCancel(context.Background())
	app.stop = stop
	go func() {
		defer close(app.done)
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case now := <-t.C:
				if n, err := repo.DeleteExpiredSessions(sweepCtx, now); err != nil && !errors.Is(err, context.Canceled) {
					log.Warn("session sweep failed", "err", err)
				} else if n > 0 {
					log.Info("expired sessions removed", "count", n)
				}
			}
		}
	}()
	return app, nil
}

// Close stops background work and closes the database.
func (a *App) Close() {
	if a.stop != nil {
		a.stop()
		<-a.done
		a.stop = nil
	}
	if a.Catalog != nil {
		a.Catalog.Close()
	}
	if a.Repo != nil {
		a.Repo.Close()
	}
}
