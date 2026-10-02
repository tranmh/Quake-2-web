// Package config loads server configuration from environment variables
// (see .env.example at the repository root).
package config

import (
	"fmt"
	"math"
	"net/url"
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
	// MaxUserPaks and MaxUserStorageBytes bound the non-public paks one
	// account may own (Q2_MAX_USER_PAKS, default 50; Q2_USER_QUOTA_BYTES,
	// default 4 GiB). Administrators are exempt.
	MaxUserPaks         int64
	MaxUserStorageBytes int64
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
	// Bots configures the AI bots the server can run and stream
	// (agent/runner.Manager).
	Bots Bots
}

// Bots configures the server's AI bots: headless agents playing the
// single player campaign in realtime games of their own, watchable live
// (Q2_BOTS_*, Q2_BOT_*, TYPESAFE_API_KEY, JEV_*, Q2_JEV_*).
type Bots struct {
	// Enabled turns the bots API on (Q2_BOTS_ENABLED, default false).
	Enabled bool
	// Max bounds the bots running at once, all owners together
	// (Q2_BOTS_MAX, default 4); PerUser those of one account
	// (Q2_BOTS_PER_USER, default 1; administrators are exempt).
	Max     int
	PerUser int
	// AllowUsers lets accounts that are not administrators start bots
	// with a local backend (Q2_BOTS_ALLOW_USERS, default false). The Jev
	// backend, which spends money, is for administrators only.
	AllowUsers bool
	// MaxViewers bounds the live viewers of one bot (Q2_BOT_MAX_VIEWERS,
	// default 8).
	MaxViewers int
	// RunsDir holds the run directories (Q2_BOT_RUNS_DIR, default
	// "./data/bots"); RunsKeep is how many ended runs are kept, the newest
	// first (Q2_BOT_RUNS_KEEP, default 50; 0 keeps all).
	RunsDir  string
	RunsKeep int
	// MaxRun is a bot's wall-clock limit (Q2_BOT_MAX_RUN, default 30m).
	MaxRun time.Duration
	// BudgetUSDPerRun caps one model-backed run's spend and
	// BudgetUSDPerDay the spend of every Jev bot per UTC day, persisted in
	// the database (Q2_BOT_BUDGET_USD_PER_RUN, default 1;
	// Q2_BOT_BUDGET_USD_PER_DAY, default 5; 0 disables either cap).
	BudgetUSDPerRun float64
	BudgetUSDPerDay float64
	// APIKey is the Jev API key (TYPESAFE_API_KEY): server side only,
	// never logged or returned.
	APIKey Secret
	// JevBaseURL overrides the Jev API base (JEV_BASE_URL; https unless
	// loopback). The key is only sent to a base other than the default
	// with JevAllowCustomBase (JEV_ALLOW_CUSTOM_BASE).
	JevBaseURL         string
	JevAllowCustomBase bool
	// JevModel is the pinned Jev model (Q2_JEV_MODEL, default
	// DefaultJevModel).
	JevModel string
	// JevMaxQPS bounds the requests per second of every Jev bot together
	// (Q2_JEV_MAX_QPS, default 20; 0: no account limit).
	JevMaxQPS float64
}

// DefaultJevModel is the pinned Jev model id (the jev client's
// DefaultModel).
const DefaultJevModel = "jev-1.13.0"

// Default returns the defaults.
func Default() Config {
	return Config{
		HTTPAddr:            ":8080",
		BlobDir:             "./data/blobs",
		DemoPak:             "assets/demo/baseq2/pak0.pak",
		MaxUploadBytes:      1 << 30,
		MaxUserPaks:         50,
		MaxUserStorageBytes: 4 << 30,
		CookieSecure:        true,
		SessionTTL:          30 * 24 * time.Hour,
		CORSOrigins:         []string{"http://localhost:3000"},
		LogLevel:            "info",
		LogFormat:           "json",
		IngestWorkers:       1,
		MigrateOnStart:      true,
		Bots: Bots{
			Max:             4,
			PerUser:         1,
			MaxViewers:      8,
			RunsDir:         "./data/bots",
			RunsKeep:        50,
			MaxRun:          30 * time.Minute,
			BudgetUSDPerRun: 1,
			BudgetUSDPerDay: 5,
			JevModel:        DefaultJevModel,
			JevMaxQPS:       20,
		},
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
	integer("Q2_MAX_USER_PAKS", &c.MaxUserPaks)
	integer("Q2_USER_QUOTA_BYTES", &c.MaxUserStorageBytes)
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
				if strings.Contains(o, "*") {
					// credentials are allowed for these origins and they pass
					// the CSRF check: a wildcard would open both to any site
					errs = append(errs, "Q2_CORS_ORIGINS: wildcards are not allowed (list explicit origins)")
					continue
				}
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

	// bots
	count := func(key string, dst *int, allowZero bool) {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 || (n == 0 && !allowZero) {
				what := "a positive integer"
				if allowZero {
					what = "a non-negative integer"
				}
				errs = append(errs, fmt.Sprintf("%s: must be %s", key, what))
				return
			}
			*dst = n
		}
	}
	usd := func(key string, dst *float64) {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || f < 0 || math.IsInf(f, 0) || math.IsNaN(f) {
				errs = append(errs, fmt.Sprintf("%s: must be a non-negative number", key))
				return
			}
			*dst = f
		}
	}
	b := &c.Bots
	boolean("Q2_BOTS_ENABLED", &b.Enabled)
	count("Q2_BOTS_MAX", &b.Max, false)
	count("Q2_BOTS_PER_USER", &b.PerUser, false)
	boolean("Q2_BOTS_ALLOW_USERS", &b.AllowUsers)
	count("Q2_BOT_MAX_VIEWERS", &b.MaxViewers, false)
	str("Q2_BOT_RUNS_DIR", &b.RunsDir)
	count("Q2_BOT_RUNS_KEEP", &b.RunsKeep, true)
	if v, ok := lookup("Q2_BOT_MAX_RUN"); ok && strings.TrimSpace(v) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil || d <= 0 {
			errs = append(errs, "Q2_BOT_MAX_RUN: must be a positive duration")
		} else {
			b.MaxRun = d
		}
	}
	usd("Q2_BOT_BUDGET_USD_PER_RUN", &b.BudgetUSDPerRun)
	usd("Q2_BOT_BUDGET_USD_PER_DAY", &b.BudgetUSDPerDay)
	if v, ok := lookup("TYPESAFE_API_KEY"); ok {
		b.APIKey = NewSecret(strings.TrimSpace(v))
	}
	str("JEV_BASE_URL", &b.JevBaseURL)
	boolean("JEV_ALLOW_CUSTOM_BASE", &b.JevAllowCustomBase)
	str("Q2_JEV_MODEL", &b.JevModel)
	if b.JevModel == "" {
		b.JevModel = DefaultJevModel
	}
	usd("Q2_JEV_MAX_QPS", &b.JevMaxQPS)
	if b.JevBaseURL != "" {
		if u, err := url.Parse(b.JevBaseURL); err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			errs = append(errs, "JEV_BASE_URL: must be an http(s) URL")
		}
	}
	if b.Enabled && b.RunsDir == "" {
		errs = append(errs, "Q2_BOT_RUNS_DIR: must not be empty")
	}
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
