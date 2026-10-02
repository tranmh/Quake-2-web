// Package jevtest is a fake Jev server for tests: an httptest server that
// speaks the documented wire format (POST /v1/systemone, Bearer key,
// decide's request and response JSON) with pluggable answer policies and
// fault injection.
//
// Policies: Scripted answers like backend/scripted from the lane state
// JSON it receives (it parses the wire state, never a snapshot); Noisy
// perturbs another policy's answers the way a model disagrees (shifted
// probabilities, a second-best pick, low confidence); Recorded replays
// responses from a JSONL file (the jev client's recorder format).
//
// Randomness is seeded per request from the seed and the request's
// content digest (faults also from the client's attempt number), so the
// answers and the random faults do not depend on the order concurrent
// requests arrive in and a lockstep run against the server repeats
// exactly. Faults.Sequence is the exception: it is indexed by arrival, for
// serial tests; Faults.Func gives scripted faults keyed by request.
package jevtest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
)

// Path is the endpoint the server answers.
const Path = "/v1/systemone"

// DefaultModel is the model id the server reports.
const DefaultModel = "jev-1.13.0"

// Call is one request the server received.
type Call struct {
	N         int // arrival index, from 0
	Model     string
	State     json.RawMessage
	Questions []decide.Question
	// Digest is decide.RequestDigest of the state and questions (equal to
	// the client request's Digest). Attempt is the client's attempt number
	// (its X-Retry-Count header: 0 first, 1 for the first retry) or,
	// without the header, the number of earlier calls with the same digest.
	Digest  string
	Attempt int
	Body    []byte
	Header  http.Header
	// Fault is the fault injected into the reply.
	Fault Fault
}

// Reply is a policy's answer: Answers encoded in the documented format,
// or Raw sent verbatim.
type Reply struct {
	Answers map[string]decide.Answer
	Raw     json.RawMessage
}

// Policy answers calls. It must be safe for concurrent use.
type Policy interface {
	Answer(c *Call) (*Reply, error)
}

// PolicyFunc adapts a function to Policy.
type PolicyFunc func(c *Call) (*Reply, error)

// Answer implements Policy.
func (f PolicyFunc) Answer(c *Call) (*Reply, error) { return f(c) }

// Scripted answers with the scripted policy from the received lane state.
type Scripted struct {
	Policy *scripted.Policy
	// Now gives the policy's time for a call (it drives the strafe
	// rhythm); nil: a time drawn from the call's digest, so answers depend
	// on the content only (the strafe side then varies by request).
	Now func(c *Call) int64
}

// NewScripted returns a Scripted policy.
func NewScripted(cfg scripted.Config) *Scripted {
	return &Scripted{Policy: scripted.NewPolicy(cfg)}
}

// digestHash is the FNV-1a hash of a digest.
func digestHash(digest string) uint64 {
	h := uint64(14695981039346656037)
	for i := 0; i < len(digest); i++ {
		h = (h ^ uint64(digest[i])) * 1099511628211
	}
	return h
}

// Answer implements Policy.
func (s *Scripted) Answer(c *Call) (*Reply, error) {
	var st decide.State
	if err := json.Unmarshal(c.State, &st); err != nil {
		return nil, fmt.Errorf("jevtest: lane state: %w", err)
	}
	now := int64(decide.Mix64(digestHash(c.Digest)) % (1 << 31))
	if s.Now != nil {
		now = s.Now(c)
	}
	return &Reply{Answers: s.Policy.Answers(&st, c.Questions, now)}, nil
}

// Noisy perturbs Base's answers like a model that is mostly right: some
// probability mass moves to random options, now and then the second best
// option wins, and some answers come with a confidence under 0.35. The
// noise is a function of the seed and the request's content (like a model
// at temperature 0, the same request gets the same answer).
type Noisy struct {
	Base Policy
	Seed uint64
	// Noise is the largest share of probability mass spread at random
	// (0.3); SecondBest the chance the top two options swap (0.1);
	// LowConfidence the chance of a confidence in [0.05, 0.3] (0.1);
	// ScoreNoise the standard deviation of a score's shift in levels
	// (0.4). A negative value disables the effect.
	Noise, SecondBest, LowConfidence, ScoreNoise float64
}

func defNeg(v, d float64) float64 {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return d
	}
	return v
}

// rngFor seeds a generator from seed, a digest and an attempt.
func rngFor(seed uint64, digest string, attempt int) *rand.Rand {
	h := digestHash(digest)
	return rand.New(rand.NewPCG(decide.Mix64(seed^h), decide.Mix64(h+uint64(attempt))))
}

// Answer implements Policy.
func (n *Noisy) Answer(c *Call) (*Reply, error) {
	r, err := n.Base.Answer(c)
	if err != nil || r == nil || r.Raw != nil {
		return r, err
	}
	rng := rngFor(n.Seed, c.Digest, 0)
	out := map[string]decide.Answer{}
	for i := range c.Questions {
		q := &c.Questions[i]
		a, ok := r.Answers[q.ID]
		if !ok {
			continue
		}
		switch q.Type {
		case decide.Choice:
			a = n.choice(q, a, rng)
		case decide.Score:
			a = n.score(q, a, rng)
		case decide.Noul:
			a.Noul = math.Max(0, math.Min(1, a.Noul+rng.NormFloat64()*0.15))
		}
		out[q.ID] = a
	}
	return &Reply{Answers: out}, nil
}

func (n *Noisy) lowConfidence(rng *rand.Rand, conf float64) float64 {
	if rng.Float64() < defNeg(n.LowConfidence, 0.1) {
		return 0.05 + 0.25*rng.Float64()
	}
	return conf
}

func (n *Noisy) choice(q *decide.Question, a decide.Answer, rng *rand.Rand) decide.Answer {
	k := len(q.Options)
	p := make([]float64, k)
	sum := 0.0
	for i, o := range q.Options {
		p[i] = math.Max(0, a.Probabilities[o.Key])
		sum += p[i]
	}
	if sum <= 0 {
		if j := q.Index(a.Choice); j >= 0 {
			p[j], sum = 1, 1
		} else {
			return a
		}
	}
	eps := defNeg(n.Noise, 0.3) * rng.Float64()
	noise := make([]float64, k)
	ns := 0.0
	for i := range noise {
		noise[i] = rng.ExpFloat64()
		ns += noise[i]
	}
	for i := range p {
		p[i] = (1-eps)*p[i]/sum + eps*noise[i]/ns
	}
	order := make([]int, k)
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return p[order[i]] > p[order[j]] })
	if k >= 2 && rng.Float64() < defNeg(n.SecondBest, 0.1) {
		p[order[0]], p[order[1]] = p[order[1]], p[order[0]]
		order[0], order[1] = order[1], order[0]
	}
	out := decide.Answer{Type: decide.Choice, Choice: q.Options[order[0]].Key, Probabilities: map[string]float64{}, HasConfidence: true}
	for i, o := range q.Options {
		out.Probabilities[o.Key] = round4(p[i])
	}
	conf := p[order[0]]
	if k >= 2 {
		conf -= p[order[1]]
	}
	out.Confidence = round4(n.lowConfidence(rng, conf))
	return out
}

func (n *Noisy) score(q *decide.Question, a decide.Answer, rng *rand.Rand) decide.Answer {
	top := float64(len(q.Options) - 1)
	s := math.Max(0, math.Min(top, a.Score+rng.NormFloat64()*defNeg(n.ScoreNoise, 0.4)))
	out := decide.Answer{Type: decide.Score, Score: round4(s), Probabilities: map[string]float64{}, HasConfidence: true}
	best := 0.0
	for i, o := range q.Options {
		w := math.Max(0, 1-math.Abs(float64(i)-s))
		out.Probabilities[o.Key] = round4(w)
		best = math.Max(best, w)
	}
	out.Confidence = round4(n.lowConfidence(rng, best))
	return out
}

func round4(f float64) float64 { return math.Round(f*1e4) / 1e4 }

// ErrExhausted is returned by Recorded when no recorded response is left.
var ErrExhausted = errors.New("jevtest: recorded responses exhausted")

type recEntry struct {
	digest string
	raw    json.RawMessage
	used   bool
}

// Recorded replays recorded response bodies: the first unused one whose
// request has the call's digest, else the next unused one in file order.
type Recorded struct {
	mu      sync.Mutex
	entries []recEntry
}

// LoadRecorded reads a JSONL file of the jev client's recorder (lines with
// "request" and "response"; only 2xx responses are kept), or of lines
// {"response": {...}}, or of bare response objects.
func LoadRecorded(r io.Reader) (*Recorded, error) {
	rec := &Recorded{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var x struct {
			Request  json.RawMessage `json:"request"`
			Response json.RawMessage `json:"response"`
			Status   int             `json:"status"`
			Answers  json.RawMessage `json:"answers"`
		}
		if err := json.Unmarshal(b, &x); err != nil {
			return nil, fmt.Errorf("jevtest: recorded line %d: %w", line, err)
		}
		e := recEntry{}
		switch {
		case x.Response != nil:
			if x.Status != 0 && (x.Status < 200 || x.Status >= 300) {
				continue
			}
			e.raw = x.Response
			if x.Request != nil {
				if w, err := decide.ParseRequestBody(x.Request); err == nil {
					e.digest = w.Digest()
				}
			}
		case x.Answers != nil:
			e.raw = append(json.RawMessage(nil), b...)
		default:
			return nil, fmt.Errorf("jevtest: recorded line %d has no response", line)
		}
		rec.entries = append(rec.entries, e)
	}
	return rec, sc.Err()
}

// Len is the number of recorded responses.
func (r *Recorded) Len() int { return len(r.entries) }

// Answer implements Policy.
func (r *Recorded) Answer(c *Call) (*Reply, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pick := -1
	for i := range r.entries {
		if !r.entries[i].used && r.entries[i].digest != "" && r.entries[i].digest == c.Digest {
			pick = i
			break
		}
	}
	for i := 0; pick < 0 && i < len(r.entries); i++ {
		if !r.entries[i].used {
			pick = i
		}
	}
	if pick < 0 {
		return nil, ErrExhausted
	}
	r.entries[pick].used = true
	return &Reply{Raw: r.entries[pick].raw}, nil
}

// Fault is an injected failure.
type Fault uint8

// Faults.
const (
	FaultNone         Fault = iota
	FaultRateLimit          // 429 with Retry-After
	FaultOverload           // 529 with Retry-After
	FaultServer             // 500
	FaultMalformed          // 200 with a truncated JSON body
	FaultMissing            // 200 with the last question's answer left out
	FaultHang               // no answer until the client gives up
	FaultUnauthorized       // 401
	FaultBadRequest         // 400
)

// String names the fault.
func (f Fault) String() string {
	switch f {
	case FaultNone:
		return "none"
	case FaultRateLimit:
		return "rate_limit"
	case FaultOverload:
		return "overload"
	case FaultServer:
		return "server"
	case FaultMalformed:
		return "malformed"
	case FaultMissing:
		return "missing"
	case FaultHang:
		return "hang"
	case FaultUnauthorized:
		return "unauthorized"
	case FaultBadRequest:
		return "bad_request"
	}
	return "fault" + strconv.Itoa(int(f))
}

// Faults configures fault injection.
type Faults struct {
	Seed uint64
	// Latency, when set, delays each reply by one of its values (drawn per
	// call).
	Latency []time.Duration
	// Per-call probabilities of the random faults.
	RateLimit, Overload, Server, Malformed, Missing float64
	// RetryAfter is the 429/529 Retry-After (1 s), sent as seconds
	// (rounded up) and as Retry-After-Ms.
	RetryAfter time.Duration
	// Sequence gives the faults of the first calls in arrival order; the
	// random faults apply after it. Arrival order is only defined for
	// serial calls: with concurrent calls (lockstep fast and slow lanes in
	// flight together) which request gets which fault depends on timing,
	// so use Func there.
	Sequence []Fault
	// Func, when set, chooses each call's fault before Sequence and the
	// random faults; ok false leaves the call to them. Keyed on the call's
	// content (Digest, Attempt, State), it stays deterministic under
	// concurrency. It runs under the server's lock and must not call the
	// Server.
	Func func(c *Call) (f Fault, ok bool)
}

// Options configures a Server.
type Options struct {
	// Policy answers the calls (nil: NewScripted with seed 0).
	Policy Policy
	Faults Faults
	// APIKey is the Bearer key the server requires ("": any non-empty).
	APIKey string
	// Model is the model id the server reports (DefaultModel).
	Model string
	// MaxCalls bounds the calls the server keeps for Calls, the newest
	// first (0: all of them). A long-running server (a realtime mock bot
	// answering ten requests a second for an hour) sets it so the
	// recorded bodies do not grow without bound. With a bound, Attempt
	// falls back to counting only recent calls of a digest when the
	// client sends no X-Retry-Count (the jev client always does).
	MaxCalls int
}

// Server is the fake Jev server.
type Server struct {
	opt Options
	srv *httptest.Server

	hits atomic.Int64

	mu       sync.Mutex
	calls    []Call
	n        int // calls received
	attempts map[string]int
}

// NewServer starts a server on loopback.
func NewServer(opt Options) *Server {
	if opt.Policy == nil {
		opt.Policy = NewScripted(scripted.Config{})
	}
	if opt.Model == "" {
		opt.Model = DefaultModel
	}
	if opt.Faults.RetryAfter <= 0 {
		opt.Faults.RetryAfter = time.Second
	}
	s := &Server{opt: opt, attempts: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL is the server's base URL (http://127.0.0.1:port).
func (s *Server) URL() string { return s.srv.URL }

// Close stops the server.
func (s *Server) Close() { s.srv.Close() }

// Calls returns the calls received so far, in arrival order (with
// Options.MaxCalls, at least the newest MaxCalls of them).
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Hits is the number of HTTP requests received, including the ones
// refused before they became calls (wrong path, method, key or body).
func (s *Server) Hits() int { return int(s.hits.Load()) }

// Count is the number of calls received.
func (s *Server) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"type": typ, "message": msg}})
	_, _ = w.Write(b)
}

func (s *Server) fault(c *Call) (Fault, time.Duration) {
	f := s.opt.Faults
	rng := rngFor(f.Seed^0x5eed, c.Digest, c.Attempt)
	var lat time.Duration
	if len(f.Latency) > 0 {
		lat = f.Latency[rng.IntN(len(f.Latency))]
	}
	if f.Func != nil {
		if ft, ok := f.Func(c); ok {
			return ft, lat
		}
	}
	if c.N < len(f.Sequence) {
		return f.Sequence[c.N], lat
	}
	x := rng.Float64()
	for _, c := range []struct {
		p float64
		f Fault
	}{{f.RateLimit, FaultRateLimit}, {f.Overload, FaultOverload}, {f.Server, FaultServer}, {f.Malformed, FaultMalformed}, {f.Missing, FaultMissing}} {
		if x < c.p {
			return c.f, lat
		}
		x -= c.p
	}
	return FaultNone, lat
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	if r.URL.Path != Path {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
		return
	}
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token == "" || s.opt.APIKey != "" && token != s.opt.APIKey {
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "unreadable body")
		return
	}
	wr, err := decide.ParseRequestBody(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if wr.Model == "" {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "model is required")
		return
	}
	if len(wr.Questions) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "questions are required")
		return
	}
	for i := range wr.Questions {
		if err := wr.Questions[i].Validate(); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "validation_error", err.Error())
			return
		}
	}
	digest := wr.Digest()
	s.mu.Lock()
	c := Call{N: s.n, Model: wr.Model, State: wr.State, Questions: wr.Questions, Digest: digest,
		Attempt: s.attempts[digest], Body: body, Header: r.Header.Clone()}
	s.n++
	if m := s.opt.MaxCalls; m > 0 && len(s.attempts) >= 4*m {
		s.attempts = map[string]int{} // retries follow their first attempt within seconds
	}
	s.attempts[digest]++
	if v := r.Header.Get("X-Retry-Count"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			c.Attempt = n
		}
	}
	fault, lat := s.fault(&c)
	c.Fault = fault
	s.calls = append(s.calls, c)
	if m := s.opt.MaxCalls; m > 0 && len(s.calls) >= 2*m {
		s.calls = append([]Call(nil), s.calls[len(s.calls)-m:]...) // amortized O(1) per call
	}
	s.mu.Unlock()

	if lat > 0 {
		t := time.NewTimer(lat)
		select {
		case <-r.Context().Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
	retry := func(status int) {
		ra := s.opt.Faults.RetryAfter
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(ra.Seconds()))))
		w.Header().Set("Retry-After-Ms", strconv.FormatInt(ra.Milliseconds(), 10))
		typ := "rate_limit_error"
		if status == 529 {
			typ = "overloaded_error"
		}
		writeError(w, status, typ, "try again later")
	}
	switch fault {
	case FaultRateLimit:
		retry(http.StatusTooManyRequests)
		return
	case FaultOverload:
		retry(529)
		return
	case FaultServer:
		writeError(w, http.StatusInternalServerError, "server_error", "internal error")
		return
	case FaultUnauthorized:
		writeError(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
		return
	case FaultBadRequest:
		writeError(w, http.StatusBadRequest, "invalid_request", "bad request")
		return
	case FaultHang:
		<-r.Context().Done()
		return
	}
	reply, err := s.opt.Policy.Answer(&c)
	if err != nil || reply == nil {
		writeError(w, http.StatusInternalServerError, "server_error", fmt.Sprint("policy: ", err))
		return
	}
	raw := reply.Raw
	if raw == nil {
		ans := reply.Answers
		if fault == FaultMissing && len(wr.Questions) > 0 {
			cp := map[string]decide.Answer{}
			for k, v := range ans {
				cp[k] = v
			}
			delete(cp, wr.Questions[len(wr.Questions)-1].ID)
			ans = cp
		}
		usage := decide.Usage{InputTokens: int64((len(body) + 3) / 4), OutputTokens: int64(8 * len(ans))}
		raw, err = decide.MarshalResponse(s.opt.Model, wr.Questions, ans, usage)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", err.Error())
			return
		}
	}
	if fault == FaultMalformed {
		raw = raw[:len(raw)/2]
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Typesafe-Request-Id", "req_"+strconv.Itoa(c.N))
	_, _ = w.Write(raw)
}
