// Package ollama is the decision backend for a decision model served
// locally by Ollama (>= 0.40.4), which answers jev's own protocol at
// POST {base}/v1/systemone: the request is decide.RequestBody (the same
// state and questions jev gets), the response jev's answers with
// probabilities and a confidence, decoded by decide.DecodeResponse. A
// "decision" model such as nimble-v3 serves no /api/chat.
//
// The client is the jev client (backend/jev) configured for a local
// server:
//   - named "ollama" (Name, logs, errors);
//   - no key and no Authorization header (Ollama takes none, so a
//     TYPESAFE_API_KEY can never reach it), plain http allowed;
//   - every call priced at 0 (Usage still reports the tokens);
//   - one attempt per request, the same Timeout on both lanes and no
//     800 ms fast-lane cap, no rate limiter, no circuit breaker, no
//     cooldown and no locally refused question sets: on a CPU a call takes
//     tens of seconds to minutes, and in lockstep the run waits for it.
//
// Calls are sent Parallel at a time (1, as an Ollama server with
// OLLAMA_NUM_PARALLEL=1 answers them), and Timeout counts from a call's
// send, not from its wait for a turn (the caller's context bounds that),
// so a queue of slow calls does not time out the ones at its end.
package ollama

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/decide"
)

// Defaults and environment variables.
const (
	// DefaultBaseURL is a local Ollama server.
	DefaultBaseURL = "http://127.0.0.1:11434"
	// DefaultModel is the model asked when none is configured.
	DefaultModel = "nimble-v3"
	// Name is the backend's name.
	Name = "ollama"

	// EnvHost is Ollama's own address variable ("127.0.0.1:11434",
	// "http://host:port"); EnvModel names the model.
	EnvHost  = "OLLAMA_HOST"
	EnvModel = "Q2_OLLAMA_MODEL"

	// DefaultTimeout bounds one call once it is sent: a 9B model on a CPU
	// takes 30 s to 2 min (3.5 min cold) for one request.
	DefaultTimeout = 10 * time.Minute
	// DefaultParallel is the number of calls sent at once.
	DefaultParallel = 1

	defaultPort = "11434"
)

// ErrBaseURL is returned for a base URL the client cannot use.
var ErrBaseURL = errors.New("ollama: invalid base URL")

// Config configures a Client. Zero values take the defaults.
type Config struct {
	// BaseURL is the server's base (DefaultBaseURL); "host:port" without
	// a scheme is taken as http, as Ollama reads OLLAMA_HOST.
	BaseURL string
	// Model is the model name (DefaultModel).
	Model string
	// Timeout bounds one call once it is sent (DefaultTimeout).
	Timeout time.Duration
	// Parallel is the number of calls sent at once (DefaultParallel).
	Parallel int
	// HTTPClient sends the requests (nil: a default client).
	HTTPClient *http.Client
	// Logf logs rare events.
	Logf func(format string, args ...any)
}

// FromEnv returns a Config from the environment (getenv nil: os.Getenv):
// OLLAMA_HOST and Q2_OLLAMA_MODEL.
func FromEnv(getenv func(string) string) Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	return Config{BaseURL: strings.TrimSpace(getenv(EnvHost)), Model: strings.TrimSpace(getenv(EnvModel))}
}

// BaseURL normalizes a base URL ("": DefaultBaseURL) as Ollama reads
// OLLAMA_HOST: without a scheme it is http, and an http base without a
// port gets Ollama's 11434.
func BaseURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = DefaultBaseURL
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: %q", ErrBaseURL, base)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: credentials, query or fragment in %q", ErrBaseURL, u.Redacted())
	}
	switch u.Scheme {
	case "http":
		if u.Port() == "" {
			u.Host = net.JoinHostPort(u.Hostname(), defaultPort)
		}
	case "https":
	default:
		return "", fmt.Errorf("%w: scheme %q", ErrBaseURL, u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// Client is the Ollama backend. It is safe for concurrent use.
type Client struct {
	jc      *jev.Client
	timeout time.Duration
	slots   chan struct{}

	mu   sync.Mutex
	wait *decide.LatencyStats
}

// New returns a client. It does not contact the server.
func New(cfg Config) (*Client, error) {
	base, err := BaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.Model = strings.TrimSpace(cfg.Model); cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Parallel <= 0 {
		cfg.Parallel = DefaultParallel
	}
	jc, err := jev.New(jev.Config{
		Name: Name, NoAuth: true, Free: true,
		BaseURL: base, AllowCustomBase: true, Model: cfg.Model, AllowAlias: true,
		FastTimeout: cfg.Timeout, SlowTimeout: cfg.Timeout, UncapFast: true, FastRetries: 0, SlowRetries: -1,
		RatePerSec: -1, BreakerFailures: math.MaxInt32,
		DefaultCooldown: time.Nanosecond, MaxCooldown: time.Nanosecond, BadSetTTL: time.Nanosecond,
		HTTPClient: cfg.HTTPClient, Logf: cfg.Logf,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	return &Client{jc: jc, timeout: cfg.Timeout, slots: make(chan struct{}, cfg.Parallel), wait: decide.NewLatencyStats(4096)}, nil
}

// Name implements decide.DecisionBackend.
func (c *Client) Name() string { return Name }

// Model returns the configured model name.
func (c *Client) Model() string { return c.jc.Model() }

// Endpoint returns the request URL.
func (c *Client) Endpoint() string { return c.jc.Endpoint() }

// Timeout returns the per-call timeout in effect.
func (c *Client) Timeout() time.Duration { return c.timeout }

// Decide implements decide.DecisionBackend: it waits for a turn, then
// sends the request once.
func (c *Client) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	queued := time.Now()
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		class := jev.ClassCanceled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			class = jev.ClassTimeout
		}
		return nil, &jev.Error{Class: class, Backend: Name, Msg: "waiting for a turn: " + ctx.Err().Error()}
	}
	defer func() { <-c.slots }()
	c.mu.Lock()
	c.wait.Add(time.Since(queued))
	c.mu.Unlock()
	return c.jc.Decide(ctx, req)
}

// Stats are the client's counters: the jev client's (wall-clock latency
// from send to reply, tokens, errors by class) and the turn waits.
type Stats struct {
	jev.Stats
	WaitP95 time.Duration
}

// Stats returns the counters.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	w := c.wait.Quantile(0.95)
	c.mu.Unlock()
	return Stats{Stats: c.jc.Stats(), WaitP95: w}
}

// Summary is a one-line account of the stats, for a run's log: the
// wall-clock cost of the calls, which a lockstep run's run.json (its
// simulated latencies) does not show.
func (s Stats) Summary() string {
	ms := func(d time.Duration) int64 { return d.Milliseconds() }
	return fmt.Sprintf("ollama %s: %d calls (%d ok, %d failed, %d timeouts), wall latency p50/p95/p99 %d/%d/%d ms (turn wait p95 %d ms), "+
		"%d input / %d output tokens", s.Model, s.Requests, s.OK, s.Errors.Total(), s.Timeouts, ms(s.P50), ms(s.P95), ms(s.P99),
		ms(s.WaitP95), s.InputTokens, s.OutputTokens)
}
