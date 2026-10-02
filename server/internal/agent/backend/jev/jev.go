// Package jev is the decision backend for TypeSafe's Jev System One model
// (POST {base}/v1/systemone with a Bearer key; docs.typesafe.ai). It uses
// net/http only and the wire format of package decide.
//
// Safety rules, enforced by New:
//   - the base URL must be https, except for a loopback host;
//   - the key is sent only to the default host unless AllowCustomBase is
//     set (JEV_ALLOW_CUSTOM_BASE=1), and redirects are never followed;
//   - inside a test binary (testing.Testing) only loopback hosts are
//     allowed, so tests can never reach the paid API;
//   - the key lives in a trace.Secret: it never reaches a log, an error or
//     the recorder (whose Authorization header is redacted);
//   - the model must be pinned to a version unless AllowAlias is set.
//
// Failures are classified (ErrorClass): 401/403 disable the client for
// good (logged once); 400/422 mark the question set's shape bad for a
// while (BadSetTTL); 429/529 start a cooldown that honors Retry-After; 5xx
// and timeouts are retried when the lane allows it (slow: once, fast:
// never) within the caller's deadline; five consecutive failures open a
// circuit breaker for five seconds.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// Defaults and environment variables.
const (
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel is the pinned model. The docs list "jev-1.13.0" as the
	// versioned id ("jev-latest" and "jev-preview" are moving aliases).
	DefaultModel = "jev-1.13.0"
	// Path is the endpoint below the base URL.
	Path = "/v1/systemone"

	EnvAPIKey          = "TYPESAFE_API_KEY"
	EnvBaseURL         = "JEV_BASE_URL"
	EnvAllowCustomBase = "JEV_ALLOW_CUSTOM_BASE"
	EnvModel           = "JEV_MODEL"

	// MaxFastTimeout caps one fast-lane attempt.
	MaxFastTimeout = 800 * time.Millisecond
	// DefaultSlowTimeout bounds one slow-lane attempt: an attempt, the
	// pause and one retry fit in the scheduler's 2.5 s slow-lane deadline.
	DefaultSlowTimeout = 1200 * time.Millisecond
	// DefaultBadSetTTL is how long a question set refused with 400/422
	// stays refused locally.
	DefaultBadSetTTL = time.Minute

	// retryPause is the pause before the first retry (doubling after).
	retryPause = 150 * time.Millisecond
	// minAttempt is the least time an attempt is given when the caller's
	// deadline is shared out among the attempts.
	minAttempt = 200 * time.Millisecond

	defaultHost = "api.typesafe.ai"
	maxBody     = 1 << 20
)

// Errors of New.
var (
	ErrNoKey       = errors.New("jev: no API key (" + EnvAPIKey + ")")
	ErrBaseURL     = errors.New("jev: invalid base URL")
	ErrCustomBase  = errors.New("jev: a non-default base URL needs " + EnvAllowCustomBase + "=1")
	ErrTestNetwork = errors.New("jev: a test binary may only reach loopback hosts")
	ErrAlias       = errors.New("jev: the model must be pinned to a version (aliases need AllowAlias)")
)

// Pricing is the price per million tokens in USD.
type Pricing struct {
	InputPerMTok, OutputPerMTok float64
}

// DefaultPricing is Jev 1.13's price: $0.042 per million input tokens,
// output free.
func DefaultPricing() Pricing { return Pricing{InputPerMTok: 0.042} }

// Cost returns the price of a usage.
func (p Pricing) Cost(u decide.Usage) float64 {
	return float64(u.InputTokens)*p.InputPerMTok/1e6 + float64(u.OutputTokens)*p.OutputPerMTok/1e6
}

// Config configures a Client. Zero values take the defaults.
type Config struct {
	// BaseURL is the API's base (DefaultBaseURL). In production it comes
	// from JEV_BASE_URL only (FromEnv).
	BaseURL string
	// APIKey is the Bearer key (TYPESAFE_API_KEY).
	APIKey trace.Secret
	// Model is the pinned model id (DefaultModel); AllowAlias permits a
	// moving alias such as "jev-latest".
	Model      string
	AllowAlias bool
	// AllowCustomBase permits a base URL other than the default
	// (JEV_ALLOW_CUSTOM_BASE=1).
	AllowCustomBase bool

	// FastTimeout and SlowTimeout bound one attempt of a lane's request
	// (fast: MaxFastTimeout, at most; slow: DefaultSlowTimeout). When the
	// caller's context has a deadline, an attempt that may still be
	// retried gets at most its share of the time left, so the retry fits.
	FastTimeout, SlowTimeout time.Duration
	// FastRetries and SlowRetries are the retries after a retryable
	// failure. FastRetries defaults to 0; SlowRetries 0 means the default
	// (1) and a negative value none.
	FastRetries, SlowRetries int

	// RatePerSec and Burst size the client's own token bucket (15/s, 5);
	// a negative RatePerSec disables it. Limiter is an optional shared
	// limiter consulted as well.
	RatePerSec float64
	Burst      int
	Limiter    Limiter

	// BreakerFailures consecutive failures (5) open the breaker for
	// BreakerOpen (5 s).
	BreakerFailures int
	BreakerOpen     time.Duration
	// DefaultCooldown is the cooldown after a 429/529 without Retry-After
	// (1 s, doubling while they repeat); MaxCooldown caps any cooldown
	// (30 s).
	DefaultCooldown, MaxCooldown time.Duration
	// BadSetTTL is how long a question set refused with 400/422 is refused
	// locally (DefaultBadSetTTL); then one request may try it again. Sets
	// are told apart by their shape (questionShape), not their live text.
	BadSetTTL time.Duration

	// Pricing prices the usage (DefaultPricing). MaxCostUSD, when > 0,
	// refuses requests once spent (calls already in flight may overshoot
	// it by their cost); OnCost sees each priced response.
	Pricing    Pricing
	MaxCostUSD float64
	OnCost     func(callUSD, totalUSD float64)

	// Recorder, when set, receives a JSON line per attempt: the request
	// (Authorization redacted) and the response.
	Recorder io.Writer
	// HTTPClient sends the requests (a copy is made that never follows
	// redirects; nil: a default client).
	HTTPClient *http.Client
	// Logf logs rare events (disable, breaker, bad question sets).
	Logf func(format string, args ...any)
	// Now is the clock (nil: time.Now).
	Now func() time.Time
}

// FromEnv returns a Config from the environment (getenv nil: os.Getenv):
// TYPESAFE_API_KEY, JEV_BASE_URL, JEV_MODEL and JEV_ALLOW_CUSTOM_BASE.
func FromEnv(getenv func(string) string) Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	return Config{
		BaseURL:         getenv(EnvBaseURL),
		APIKey:          trace.NewSecret(strings.TrimSpace(getenv(EnvAPIKey))),
		Model:           getenv(EnvModel),
		AllowCustomBase: getenv(EnvAllowCustomBase) == "1",
	}
}

// Stats are the client's counters.
type Stats struct {
	Model string // the pinned model
	// Requests are Decide calls, Attempts the HTTP requests sent, OK the
	// successful calls and Retries the extra attempts.
	Requests, Attempts, OK, Retries int64
	// Errors counts failed attempts and local refusals by class.
	Errors   ErrorCounts
	Timeouts int64
	// Latency quantiles of the attempts that got a response.
	P50, P95, P99 time.Duration
	// Usage and cost of the successful calls.
	InputTokens, OutputTokens int64
	CostUSD                   float64
	// Unknown counts response fields the decoder did not recognize.
	Unknown int64
	// LastModel is the model id the last response reported.
	LastModel string
	Disabled  bool
	// BadSets is the number of question-set shapes ever refused with
	// 400/422.
	BadSets int
}

// Client is the Jev backend. It is safe for concurrent use.
type Client struct {
	cfg      Config
	endpoint string
	hc       *http.Client
	bucket   *TokenBucket
	now      func() time.Time

	mu             sync.Mutex
	disabled       bool
	bad            map[string]time.Time // question-set shape -> refused until
	failures       int
	openUntil      time.Time
	cooldownUntil  time.Time
	cooldownStreak int
	spent          float64
	lat            *decide.LatencyStats
	stats          Stats

	recMu sync.Mutex
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkBase validates the base URL against the safety rules and returns
// the endpoint.
func checkBase(raw string, allowCustom bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", fmt.Errorf("%w: %q", ErrBaseURL, raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: credentials, query or fragment in %q", ErrBaseURL, u.Redacted())
	}
	host := u.Hostname()
	loop := isLoopback(host)
	switch u.Scheme {
	case "https":
	case "http":
		if !loop {
			return "", fmt.Errorf("%w: %s must use https (http only for loopback)", ErrBaseURL, u.Redacted())
		}
	default:
		return "", fmt.Errorf("%w: scheme %q", ErrBaseURL, u.Scheme)
	}
	path := strings.TrimRight(u.Path, "/")
	isDefault := u.Scheme == "https" && strings.EqualFold(host, defaultHost) && (u.Port() == "" || u.Port() == "443") && path == ""
	if !isDefault && !allowCustom {
		return "", fmt.Errorf("%w (base %s)", ErrCustomBase, u.Redacted())
	}
	if testing.Testing() && !loop {
		return "", fmt.Errorf("%w (base %s)", ErrTestNetwork, u.Redacted())
	}
	u.Path = path + Path
	return u.String(), nil
}

// isAlias reports a model name that is not pinned to a version.
func isAlias(m string) bool {
	for _, s := range []string{"-latest", "-preview", "-beta", "-nightly", "-next"} {
		if strings.HasSuffix(m, s) {
			return true
		}
	}
	return !strings.ContainsAny(m, "0123456789")
}

// New returns a client after checking the safety rules.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if isAlias(cfg.Model) && !cfg.AllowAlias {
		return nil, fmt.Errorf("%w: %q", ErrAlias, cfg.Model)
	}
	endpoint, err := checkBase(cfg.BaseURL, cfg.AllowCustomBase)
	if err != nil {
		return nil, err
	}
	if !cfg.APIKey.IsSet() {
		return nil, ErrNoKey
	}
	defd := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	defd(&cfg.FastTimeout, MaxFastTimeout)
	cfg.FastTimeout = min(cfg.FastTimeout, MaxFastTimeout)
	defd(&cfg.SlowTimeout, DefaultSlowTimeout)
	defd(&cfg.BadSetTTL, DefaultBadSetTTL)
	defd(&cfg.BreakerOpen, 5*time.Second)
	defd(&cfg.DefaultCooldown, time.Second)
	defd(&cfg.MaxCooldown, 30*time.Second)
	if cfg.FastRetries < 0 {
		cfg.FastRetries = 0
	}
	switch {
	case cfg.SlowRetries == 0:
		cfg.SlowRetries = 1
	case cfg.SlowRetries < 0:
		cfg.SlowRetries = 0
	}
	if cfg.BreakerFailures <= 0 {
		cfg.BreakerFailures = 5
	}
	if cfg.Pricing == (Pricing{}) {
		cfg.Pricing = DefaultPricing()
	}
	if cfg.RatePerSec == 0 {
		cfg.RatePerSec = 15
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 5
	}
	c := &Client{cfg: cfg, endpoint: endpoint, now: cfg.Now, bad: map[string]time.Time{}, lat: decide.NewLatencyStats(1024)}
	if c.now == nil {
		c.now = time.Now
	}
	if cfg.RatePerSec > 0 {
		c.bucket = NewTokenBucket(cfg.RatePerSec, cfg.Burst, c.now)
	}
	hc := &http.Client{}
	if cfg.HTTPClient != nil {
		cp := *cfg.HTTPClient
		hc = &cp
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c.hc = hc
	c.stats.Model = cfg.Model
	return c, nil
}

// Name implements decide.DecisionBackend.
func (c *Client) Name() string { return "jev" }

// Model returns the pinned model id.
func (c *Client) Model() string { return c.cfg.Model }

// Endpoint returns the request URL.
func (c *Client) Endpoint() string { return c.endpoint }

func (c *Client) logf(format string, args ...any) {
	if c.cfg.Logf != nil {
		c.cfg.Logf(format, args...)
	}
}

func (c *Client) retries(l decide.Lane) int {
	if l == decide.LaneSlow {
		return c.cfg.SlowRetries
	}
	return c.cfg.FastRetries
}

func (c *Client) timeout(l decide.Lane) time.Duration {
	if l == decide.LaneSlow {
		return c.cfg.SlowTimeout
	}
	return c.cfg.FastTimeout
}

// attemptTimeout bounds attempt n of a request that may be retried
// retriesLeft more times: the lane's timeout or, under a caller deadline,
// the attempt's share of the time left after the retry pauses (at least
// minAttempt; below that the attempt keeps the lane's timeout and the
// deadline alone bounds it).
func (c *Client) attemptTimeout(ctx context.Context, l decide.Lane, n, retriesLeft int) time.Duration {
	d := c.timeout(l)
	dl, ok := ctx.Deadline()
	if !ok || retriesLeft <= 0 {
		return d
	}
	left := time.Until(dl)
	for k := 0; k < retriesLeft; k++ {
		left -= retryPause << (n + k)
	}
	if share := left / time.Duration(retriesLeft+1); share >= minAttempt {
		d = min(d, share)
	}
	return d
}

// questionShape digests the shape of a question set: each question's id,
// type, instructions and number of options. Option keys and descriptions
// are left out: target and pickup options carry live ids, ranges and
// bearings, so two requests of the same schema would almost never share
// them, and a schema the API rejects would be sent again every time.
func questionShape(qs []decide.Question) string {
	type shape struct {
		ID           string `json:"id"`
		Type         string `json:"type"`
		Instructions string `json:"instructions"`
		Options      int    `json:"options"`
	}
	s := make([]shape, len(qs))
	for i := range qs {
		s[i] = shape{ID: qs[i].ID, Type: string(qs[i].Type), Instructions: qs[i].Instructions, Options: len(qs[i].Options)}
	}
	d, _ := trace.Digest(s)
	return d
}

// count counts a failed attempt or a local refusal.
func (c *Client) count(class ErrorClass) {
	c.mu.Lock()
	c.stats.Errors.add(class)
	if class == ClassTimeout {
		c.stats.Timeouts++
	}
	c.mu.Unlock()
}

// fail counts a failed call and returns it.
func (c *Client) fail(e *Error) error {
	c.count(e.Class)
	return e
}

// admit refuses locally what must not be sent now.
func (c *Client) admit(setDigest string) *Error {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	switch {
	case c.disabled:
		return &Error{Class: ClassDisabled, Msg: "disabled after an authentication failure"}
	case now.Before(c.bad[setDigest]):
		return &Error{Class: ClassBadSet, Msg: "the question set was refused before"}
	case now.Before(c.openUntil):
		return &Error{Class: ClassBreaker, Msg: "circuit breaker open"}
	case now.Before(c.cooldownUntil):
		return &Error{Class: ClassCooldown, RetryAfter: c.cooldownUntil.Sub(now), Msg: "rate limit cooldown"}
	case c.cfg.MaxCostUSD > 0 && c.spent >= c.cfg.MaxCostUSD:
		return &Error{Class: ClassBudget, Msg: fmt.Sprintf("budget of $%.4f spent", c.cfg.MaxCostUSD)}
	}
	return nil
}

// take asks the rate limiters for a permit: the client's own bucket
// first (a local refusal costs the shared limiter nothing), then the
// shared one; when the shared limiter refuses, the bucket's token is given
// back.
func (c *Client) take(tokens int) bool {
	if c.bucket != nil && !c.bucket.Allow(tokens) {
		return false
	}
	if c.cfg.Limiter != nil && !c.cfg.Limiter.Allow(tokens) {
		if c.bucket != nil {
			c.bucket.refund()
		}
		return false
	}
	return true
}

// Decide implements decide.DecisionBackend: it sends the request (with the
// lane's retries) and decodes the answers.
func (c *Client) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	c.mu.Lock()
	c.stats.Requests++
	c.mu.Unlock()
	setDigest := questionShape(req.Questions)
	body, err := decide.RequestBody(c.cfg.Model, req)
	if err != nil {
		return nil, c.fail(&Error{Class: ClassBadRequest, Msg: err.Error()})
	}
	retries := c.retries(req.Lane)
	for attempt := 0; ; attempt++ {
		if e := c.admit(setDigest); e != nil {
			return nil, c.fail(e)
		}
		if !c.take(len(body) / 4) {
			return nil, c.fail(&Error{Class: ClassThrottled, Msg: "rate limiter"})
		}
		resp, e := c.attempt(ctx, req, body, setDigest, attempt, c.attemptTimeout(ctx, req.Lane, attempt, retries-attempt))
		if e == nil {
			resp.Retries = attempt
			return resp, nil
		}
		if !e.Class.Retryable() || attempt >= retries || ctx.Err() != nil {
			return nil, c.fail(e)
		}
		c.count(e.Class)
		c.mu.Lock()
		c.stats.Retries++
		c.mu.Unlock()
		// a short pause before the retry, within the caller's deadline
		t := time.NewTimer(retryPause << attempt)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, c.fail(&Error{Class: classOfCtx(ctx), Msg: ctx.Err().Error()})
		case <-t.C:
		}
	}
}

func classOfCtx(ctx context.Context) ErrorClass {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ClassTimeout
	}
	return ClassCanceled
}

// exchange is one recorded attempt (a JSON line of the Recorder).
type exchange struct {
	Seq             uint64          `json:"seq"`
	Lane            string          `json:"lane"`
	Attempt         int             `json:"attempt"`
	Method          string          `json:"method"`
	URL             string          `json:"url"`
	RequestHeaders  http.Header     `json:"request_headers"`
	Request         json.RawMessage `json:"request"`
	Status          int             `json:"status,omitempty"`
	ResponseHeaders http.Header     `json:"response_headers,omitempty"`
	Response        json.RawMessage `json:"response,omitempty"`
	ResponseText    string          `json:"response_text,omitempty"`
	LatencyMs       float64         `json:"latency_ms"`
	Err             string          `json:"err,omitempty"`
}

func (c *Client) record(x *exchange) {
	if c.cfg.Recorder == nil {
		return
	}
	// no HTML escaping: the recorded request is the body that was sent,
	// byte for byte
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(x); err != nil {
		return
	}
	// the key never reaches the recorder, even if a server echoed it
	line := []byte(trace.Redact(string(bytes.TrimSuffix(buf.Bytes(), []byte{'\n'})), c.cfg.APIKey))
	c.recMu.Lock()
	defer c.recMu.Unlock()
	_, _ = c.cfg.Recorder.Write(append(line, '\n'))
}

// attempt sends one HTTP request, bounded by timeout.
func (c *Client) attempt(ctx context.Context, req *decide.Request, body []byte, setDigest string, n int, timeout time.Duration) (*decide.Response, *Error) {
	actx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(actx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &Error{Class: ClassTransport, Msg: err.Error()}
	}
	hreq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey.Reveal())
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("User-Agent", "quake2web-q2bot/1")
	// the attempt number, as the official SDKs send it (lets a test server
	// tell a retry from a new identical request)
	hreq.Header.Set("X-Retry-Count", strconv.Itoa(n))
	x := &exchange{Seq: req.Seq, Lane: req.Lane.String(), Attempt: n, Method: hreq.Method, URL: c.endpoint,
		RequestHeaders: trace.RedactHeader(hreq.Header), Request: body}
	c.mu.Lock()
	c.stats.Attempts++
	c.mu.Unlock()
	start := c.now()
	hresp, err := c.hc.Do(hreq)
	if err != nil {
		lat := c.now().Sub(start)
		x.LatencyMs = ms(lat)
		e := &Error{Class: ClassTransport, Msg: trace.Redact(err.Error(), c.cfg.APIKey)}
		switch {
		case ctx.Err() != nil:
			e.Class = classOfCtx(ctx)
		case errors.Is(actx.Err(), context.DeadlineExceeded):
			e.Class = ClassTimeout
		}
		x.Err = e.Error()
		c.record(x)
		c.noteFailure(e.Class)
		return nil, e
	}
	data, rerr := io.ReadAll(io.LimitReader(hresp.Body, maxBody+1))
	_ = hresp.Body.Close()
	lat := c.now().Sub(start)
	x.Status, x.ResponseHeaders, x.LatencyMs = hresp.StatusCode, trace.RedactHeader(hresp.Header), ms(lat)
	if json.Valid(data) {
		x.Response = data
	} else {
		x.ResponseText = string(data)
	}
	if rerr != nil {
		e := &Error{Class: ClassTransport, Status: hresp.StatusCode, Msg: trace.Redact(rerr.Error(), c.cfg.APIKey)}
		if errors.Is(actx.Err(), context.DeadlineExceeded) {
			e.Class = ClassTimeout
		}
		x.Err = e.Error()
		c.record(x)
		c.noteFailure(e.Class)
		return nil, e
	}
	c.mu.Lock()
	c.lat.Add(lat)
	c.mu.Unlock()
	e := c.classify(hresp, data, setDigest)
	if e == nil && len(data) > maxBody {
		e = &Error{Class: ClassDecode, Status: hresp.StatusCode, Msg: "response too large"}
	}
	var resp *decide.Response
	if e == nil {
		resp, err = decide.DecodeResponse(data, req.Questions)
		if err != nil {
			e = &Error{Class: ClassDecode, Status: hresp.StatusCode, Msg: err.Error()}
		}
	}
	if e != nil {
		x.Err = e.Error()
		c.record(x)
		c.noteFailure(e.Class)
		return nil, e
	}
	c.record(x)
	resp.Seq, resp.Latency, resp.Status = req.Seq, lat, hresp.StatusCode
	resp.CostUSD = c.cfg.Pricing.Cost(resp.Usage)
	c.noteSuccess(resp)
	return resp, nil
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// classify maps a non-2xx status to an error and applies its effect.
func (c *Client) classify(hresp *http.Response, data []byte, setDigest string) *Error {
	st := hresp.StatusCode
	if st >= 200 && st < 300 {
		return nil
	}
	msg := strings.TrimSpace(string(data))
	if len(msg) > 200 {
		msg = msg[:200]
	}
	e := &Error{Class: ClassHTTP, Status: st, Msg: trace.Redact(msg, c.cfg.APIKey)}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	switch {
	case st == http.StatusUnauthorized || st == http.StatusForbidden:
		e.Class = ClassAuth
		if !c.disabled {
			c.disabled = true
			c.logf("jev: authentication failed (status %d): client disabled", st)
		}
	case st == http.StatusBadRequest || st == http.StatusUnprocessableEntity:
		e.Class = ClassBadRequest
		if !now.Before(c.bad[setDigest]) {
			c.logf("jev: status %d for question set %s: refused locally for %v", st, setDigest, c.cfg.BadSetTTL)
		}
		c.bad[setDigest] = now.Add(c.cfg.BadSetTTL)
	case st == http.StatusTooManyRequests || st == 529:
		e.Class = ClassRateLimit
		if st == 529 {
			e.Class = ClassOverloaded
		}
		d, ok := retryAfter(hresp.Header, now)
		if !ok {
			d = c.cfg.DefaultCooldown << min(c.cooldownStreak, 5)
		}
		d = min(d, c.cfg.MaxCooldown)
		e.RetryAfter = d
		c.cooldownStreak++
		if until := now.Add(d); until.After(c.cooldownUntil) {
			c.cooldownUntil = until
		}
	case st == http.StatusRequestTimeout || st >= 500:
		e.Class = ClassServer
	}
	return e
}

// retryAfter parses retry-after-ms or Retry-After (seconds or an HTTP
// date).
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if v := strings.TrimSpace(h.Get("Retry-After-Ms")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return time.Duration(f * float64(time.Millisecond)), true
		}
	}
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
		return time.Duration(f * float64(time.Second)), true
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0), true
	}
	return 0, false
}

func (c *Client) noteFailure(class ErrorClass) {
	if !class.failure() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures++
	if c.failures >= c.cfg.BreakerFailures {
		now := c.now()
		if !now.Before(c.openUntil) {
			c.logf("jev: %d consecutive failures: circuit breaker open for %v", c.failures, c.cfg.BreakerOpen)
		}
		c.openUntil = now.Add(c.cfg.BreakerOpen)
	}
}

func (c *Client) noteSuccess(r *decide.Response) {
	c.mu.Lock()
	c.failures, c.cooldownStreak = 0, 0
	c.spent += r.CostUSD
	c.stats.OK++
	c.stats.InputTokens += r.Usage.InputTokens
	c.stats.OutputTokens += r.Usage.OutputTokens
	c.stats.CostUSD = c.spent
	c.stats.Unknown += int64(r.Unknown)
	c.stats.LastModel = r.Model
	spent, hook := c.spent, c.cfg.OnCost
	c.mu.Unlock()
	if hook != nil {
		hook(r.CostUSD, spent)
	}
}

// Stats returns the counters.
func (c *Client) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.Disabled, s.BadSets = c.disabled, len(c.bad)
	s.P50, s.P95, s.P99 = c.lat.Quantile(0.5), c.lat.Quantile(0.95), c.lat.Quantile(0.99)
	return s
}
