// Package config loads server configuration from environment variables
// (see .env.example at the repository root).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the process configuration.
type Config struct {
	// HTTPAddr is the listen address (Q2_HTTP_ADDR, default ":8080").
	HTTPAddr string
	// DatabaseURL is the Postgres URL (DATABASE_URL). Empty: in-memory repo.
	DatabaseURL string
	// BlobDir is the blob store location, a directory or file:// URL
	// (Q2_BLOB_DIR, default "./data/blobs").
	BlobDir string
	// UploadDir holds in-flight uploads (Q2_UPLOAD_DIR, default BlobDir/tmp).
	UploadDir string
	// DemoPak is ingested as the public pakset "demo" on startup
	// (Q2_DEMO_PAK, default assets/demo/baseq2/pak0.pak; "" or "off" disables).
	DemoPak string
	// MaxUploadBytes caps pak uploads (Q2_MAX_UPLOAD_BYTES, default 1 GiB).
	MaxUploadBytes int64
	// CookieSecure sets the Secure flag on the session cookie
	// (Q2_COOKIE_SECURE, default true).
	CookieSecure bool
	// SessionTTL is the session lifetime (Q2_SESSION_TTL, default 720h).
	SessionTTL time.Duration
	// CORSOrigins are extra allowed browser origins, comma separated
	// (Q2_CORS_ORIGINS, default "http://localhost:3000").
	CORSOrigins []string
	// PublicWSURL is the base URL clients use for game WebSockets
	// (Q2_PUBLIC_WS_URL, e.g. "wss://q2.example.com"; empty: same origin).
	PublicWSURL string
	// LogLevel is debug|info|warn|error (Q2_LOG_LEVEL, default info).
	LogLevel string
	// LogFormat is json|text (Q2_LOG_FORMAT, default json).
	LogFormat string
	// IngestWorkers bounds concurrent ingest jobs (Q2_INGEST_WORKERS, default 1).
	IngestWorkers int
	// TrustProxy takes the client IP from the last X-Forwarded-For hop
	// (Q2_TRUST_PROXY, default false; enable behind Caddy).
	TrustProxy bool
	// MigrateOnStart runs goose migrations at startup (Q2_MIGRATE, default true).
	MigrateOnStart bool
}

// Default returns the defaults.
func Default() Config {
	return Config{
		HTTPAddr:       ":8080",
		BlobDir:        "./data/blobs",
		DemoPak:        "assets/demo/baseq2/pak0.pak",
		MaxUploadBytes: 1 << 30,
		CookieSecure:   true,
		SessionTTL:     30 * 24 * time.Hour,
		CORSOrigins:    []string{"http://localhost:3000"},
		LogLevel:       "info",
		LogFormat:      "json",
		IngestWorkers:  1,
		MigrateOnStart: true,
	}
}

// Load reads the process environment.
func Load() (Config, error) { return FromLookup(os.LookupEnv) }

// FromMap reads configuration from a map (tests).
func FromMap(env map[string]string) (Config, error) {
	return FromLookup(func(k string) (string, bool) { v, ok := env[k]; return v, ok })
}

// FromLookup reads configuration through a lookup function.
func FromLookup(lookup func(string) (string, bool)) (Config, error) {
	c := Default()
	var errs []string
	str := func(key string, dst *string) {
		if v, ok := lookup(key); ok {
			*dst = strings.TrimSpace(v)
		}
	}
	boolean := func(key string, dst *bool) {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			b, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", key, err))
				return
			}
			*dst = b
		}
	}
	integer := func(key string, dst *int64) {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil || n <= 0 {
				errs = append(errs, fmt.Sprintf("%s: must be a positive integer", key))
				return
			}
			*dst = n
		}
	}
	str("Q2_HTTP_ADDR", &c.HTTPAddr)
	str("DATABASE_URL", &c.DatabaseURL)
	str("Q2_BLOB_DIR", &c.BlobDir)
	str("Q2_UPLOAD_DIR", &c.UploadDir)
	str("Q2_DEMO_PAK", &c.DemoPak)
	if strings.EqualFold(c.DemoPak, "off") {
		c.DemoPak = ""
	}
	integer("Q2_MAX_UPLOAD_BYTES", &c.MaxUploadBytes)
	boolean("Q2_COOKIE_SECURE", &c.CookieSecure)
	if v, ok := lookup("Q2_SESSION_TTL"); ok && strings.TrimSpace(v) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil || d <= 0 {
			errs = append(errs, "Q2_SESSION_TTL: must be a positive duration")
		} else {
			c.SessionTTL = d
		}
	}
	if v, ok := lookup("Q2_CORS_ORIGINS"); ok {
		c.CORSOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
				c.CORSOrigins = append(c.CORSOrigins, o)
			}
		}
	}
	str("Q2_PUBLIC_WS_URL", &c.PublicWSURL)
	str("Q2_LOG_LEVEL", &c.LogLevel)
	str("Q2_LOG_FORMAT", &c.LogFormat)
	var workers int64 = int64(c.IngestWorkers)
	integer("Q2_INGEST_WORKERS", &workers)
	c.IngestWorkers = int(workers)
	boolean("Q2_MIGRATE", &c.MigrateOnStart)
	boolean("Q2_TRUST_PROXY", &c.TrustProxy)
	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, "Q2_LOG_LEVEL: must be debug|info|warn|error")
	}
	if c.BlobDir == "" {
		errs = append(errs, "Q2_BLOB_DIR: must not be empty")
	}
	if len(errs) > 0 {
		return c, fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return c, nil
}
