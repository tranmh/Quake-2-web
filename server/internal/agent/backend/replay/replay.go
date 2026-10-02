// Package replay is a decision backend that answers from a recorded trace
// (responses mode): each request gets the raw response recorded for it,
// decoded again, so a lockstep run can be repeated without the model.
//
// A request is matched by its Seq and the digest of its state and
// questions (trace.Decision.ReqDigest). When the digest differs, the
// recorded response of that Seq is used anyway (Seq order) and the
// divergence is noted; in strict mode the first divergence is an error
// instead. Recorded failures are replayed as errors, and the recorded
// latencies are offered as a decide.LatencyModel so the lockstep schedule
// repeats too. A recorded timeout replays as an error that is
// context.DeadlineExceeded, so the scheduler classifies it as a timeout
// again.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// Name is the backend's name.
const Name = "replay"

// Options configures a Backend.
type Options struct {
	// Strict makes a divergence (a digest mismatch or a missing Seq) an
	// error.
	Strict bool
	// Episode selects the episode of a multi-episode trace (Seq restarts
	// in every episode).
	Episode int
}

// ErrDivergence is wrapped by every *Divergence.
var ErrDivergence = errors.New("replay: divergence")

// Divergence is a request that does not match the recording.
type Divergence struct {
	Seq       uint64
	Lane      string
	Want, Got string // recorded and requested digests ("" when missing)
	Reason    string // "digest", "lane" or "missing"
}

func (d *Divergence) Error() string {
	return fmt.Sprintf("replay: divergence at request %d (%s lane): %s (recorded %q, got %q)", d.Seq, d.Lane, d.Reason, d.Want, d.Got)
}

// Unwrap makes errors.Is(err, ErrDivergence) hold.
func (d *Divergence) Unwrap() error { return ErrDivergence }

// RecordedError is a failure replayed from the trace. Its message is the
// recorded one, unchanged, so a replayed trace compares equal to the
// original. Timeout marks a recorded timeout.
type RecordedError struct {
	Msg     string
	Timeout bool
}

func (e *RecordedError) Error() string { return e.Msg }

// Is makes errors.Is(err, context.DeadlineExceeded) hold for a recorded
// timeout.
func (e *RecordedError) Is(target error) bool {
	return e.Timeout && target == context.DeadlineExceeded
}

// Stats are the backend's counters.
type Stats struct {
	Calls int
	// Matched by Seq and digest; SeqFallback answered by Seq despite a
	// different digest; Missing had no recording.
	Matched, SeqFallback, Missing int
	// Failures replayed recorded errors.
	Failures int
}

type entry struct {
	seq     uint64
	lane    string
	digest  string
	raw     json.RawMessage
	err     string
	timeout bool
	latency time.Duration
	cost    float64
}

// Backend replays recorded responses. It is safe for concurrent use.
type Backend struct {
	opt     Options
	entries map[uint64]*entry

	mu    sync.Mutex
	first *Divergence
	stats Stats
}

// New builds a backend from a trace's request decision events (lanes fast
// and slow; the bot's tick events are skipped).
func New(events []trace.Event, opt Options) (*Backend, error) {
	b := &Backend{opt: opt, entries: map[uint64]*entry{}}
	for i := range events {
		e := &events[i]
		if e.Type != trace.TypeDecision || e.Ep != opt.Episode {
			continue
		}
		var d trace.Decision
		if err := e.DecodeBody(&d); err != nil {
			return nil, fmt.Errorf("replay: event %d: %w", e.Seq, err)
		}
		if d.Lane == trace.LaneTick {
			continue // a decision tick of the bot, not a request
		}
		if _, dup := b.entries[d.Req]; dup {
			return nil, fmt.Errorf("replay: request %d recorded twice in episode %d", d.Req, opt.Episode)
		}
		en := &entry{seq: d.Req, lane: d.Lane, digest: d.ReqDigest, raw: d.Response, err: d.Err, timeout: d.Timeout,
			latency: time.Duration(d.LatencyMs * float64(time.Millisecond)), cost: d.CostUSD}
		if en.digest == "" && d.State != nil && d.Questions != nil {
			en.digest = decide.RequestDigest(d.State, d.Questions)
		}
		b.entries[d.Req] = en
	}
	return b, nil
}

// Load reads a trace file (trace.ReadFile) and builds a backend.
func Load(path string, opt Options) (*Backend, error) {
	events, err := trace.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return New(events, opt)
}

// Len is the number of recorded requests.
func (b *Backend) Len() int { return len(b.entries) }

// Name implements decide.DecisionBackend.
func (b *Backend) Name() string { return Name }

// Latency implements decide.LatencyModel with the recorded latency of the
// request's Seq (0 when not recorded).
func (b *Backend) Latency(req *decide.Request) time.Duration {
	if e, ok := b.entries[req.Seq]; ok {
		return e.latency
	}
	return 0
}

// Divergence returns the divergence of the lowest Seq so far (nil if
// none): the first request that differs from the recording.
func (b *Backend) Divergence() *Divergence {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.first
}

// Stats returns the counters.
func (b *Backend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

// Decide implements decide.DecisionBackend.
func (b *Backend) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	got := req.Digest()
	e := b.entries[req.Seq]
	b.mu.Lock()
	b.stats.Calls++
	var div *Divergence
	switch {
	case e == nil:
		div = &Divergence{Seq: req.Seq, Lane: req.Lane.String(), Got: got, Reason: "missing"}
	case e.lane != req.Lane.String():
		div = &Divergence{Seq: req.Seq, Lane: req.Lane.String(), Want: e.digest, Got: got, Reason: "lane"}
	case e.digest != "" && e.digest != got:
		div = &Divergence{Seq: req.Seq, Lane: req.Lane.String(), Want: e.digest, Got: got, Reason: "digest"}
	}
	if div != nil && (b.first == nil || div.Seq < b.first.Seq) {
		b.first = div // the earliest request, whatever order concurrent calls came in
	}
	switch {
	case e == nil:
		b.stats.Missing++
	case div != nil:
		b.stats.SeqFallback++
	default:
		b.stats.Matched++
	}
	if e != nil && e.err != "" && (div == nil || !b.opt.Strict) {
		b.stats.Failures++
	}
	b.mu.Unlock()
	if div != nil && (b.opt.Strict || e == nil) {
		return nil, div
	}
	if e.err != "" {
		return nil, &RecordedError{Msg: e.err, Timeout: e.timeout}
	}
	if e.raw == nil {
		return nil, &RecordedError{Msg: "replay: no response recorded"}
	}
	resp, err := decide.DecodeResponse(e.raw, req.Questions)
	if err != nil {
		return nil, fmt.Errorf("replay: request %d: %w", req.Seq, err)
	}
	resp.Seq, resp.Latency, resp.CostUSD = req.Seq, e.latency, e.cost
	return resp, nil
}
