package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quake2web/server/internal/agent/decide"
)

// Stats count one episode's lookups by lane, as its records came back:
// Hits are reused answers delivered; Misses requests looked up and not
// found (whatever became of them: sent, refused by the budget, failed);
// InFlight the misses whose key an earlier request was still waiting for.
type Stats struct {
	Hits, Misses, InFlight [decide.NumLanes]int
}

// Backend is one episode's view of a run's Store: it answers a request
// from the store when it can and asks the inner backend otherwise. It
// keeps the inner backend's name and is safe for concurrent use.
//
// In a lockstep run the scheduler's latency model must be Latency's (the
// lookup happens there) and every collected result must reach Record (the
// pipeline's OnRecord); in a realtime run Decide looks up itself and
// Record still keeps the answers.
type Backend struct {
	store    *Store
	inner    decide.DecisionBackend
	ep       int
	lockstep bool

	mu sync.Mutex
	// found: hits looked up at submission (lockstep), by Seq; asked: the
	// keys of the misses, by Seq, and whether each was an in-flight
	// duplicate
	found map[uint64]*decide.Response
	asked map[uint64]askedKey
	stats Stats
}

type askedKey struct {
	key      string
	inflight bool
}

// Episode returns episode ep's backend on the store, asking inner on a
// miss; lockstep selects the lookup at submission (Latency).
func (s *Store) Episode(inner decide.DecisionBackend, ep int, lockstep bool) *Backend {
	return &Backend{store: s, inner: inner, ep: ep, lockstep: lockstep, found: map[uint64]*decide.Response{},
		asked: map[uint64]askedKey{}}
}

// Name implements decide.DecisionBackend: the inner backend's.
func (b *Backend) Name() string { return b.inner.Name() }

// Decide implements decide.DecisionBackend: the answer found at
// submission (lockstep) or now (realtime), else the inner backend's.
func (b *Backend) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	b.mu.Lock()
	resp, ok := b.found[req.Seq]
	delete(b.found, req.Seq)
	b.mu.Unlock()
	if !ok && !b.lockstep {
		resp, ok = b.lookup(req), true
	}
	if ok && resp != nil {
		return resp, nil
	}
	return b.inner.Decide(ctx, req)
}

// Latency returns the lockstep scheduler's latency model: inner's latency
// for a miss, zero for a hit (looked up here, at submission, and then
// answered by Decide). It is called once per request the scheduler
// accepts, from the control loop.
func (b *Backend) Latency(inner decide.LatencyModel) decide.LatencyModel {
	return latency{b: b, inner: inner}
}

type latency struct {
	b     *Backend
	inner decide.LatencyModel
}

func (l latency) Latency(req *decide.Request) time.Duration {
	if resp := l.b.lookup(req); resp != nil {
		l.b.mu.Lock()
		l.b.found[req.Seq] = resp
		l.b.mu.Unlock()
		return 0
	}
	if l.inner == nil {
		return 0
	}
	return l.inner.Latency(req)
}

// lookup returns the reused answer to req (nil: a miss, noted as asked).
func (b *Backend) lookup(req *decide.Request) *decide.Response {
	cfg := b.store.Config()
	key := cfg.Key(req, MapOf(req))
	e, inflight := b.store.Lookup(key, b.ep, req.SnapTime)
	if e != nil {
		if resp, err := Reuse(cfg, e, req.Seq, b.ep, req.SnapTime, req.Questions); err == nil {
			return resp
		}
	}
	b.store.Asked(key)
	b.mu.Lock()
	b.asked[req.Seq] = askedKey{key: key, inflight: inflight}
	b.mu.Unlock()
	return nil
}

// Reuse returns entry e as the answer to request seq of episode ep asked
// at snap with questions qs (whose option keys are those of the request
// e answered): e's response body with a zero usage, decoded again for qs
// (a replay decodes the recorded body the same way), marked and carrying
// what it saves.
func Reuse(cfg Config, e *Entry, seq uint64, ep int, snap int64, qs []decide.Question) (*decide.Response, error) {
	raw, err := decide.WithUsage(e.Raw, decide.Usage{})
	if err != nil || len(e.Raw) == 0 {
		if raw, err = decide.MarshalResponse(e.Model, qs, e.Answers, decide.Usage{}); err != nil {
			return nil, err
		}
	}
	resp, err := decide.DecodeResponse(raw, qs)
	if err != nil {
		return nil, fmt.Errorf("cache: reused answer: %w", err)
	}
	resp.Seq, resp.Cache, resp.SavedTokens, resp.SavedUSD = seq, cfg.Marker(), e.InputTokens, e.CostUSD
	if e.Ep == ep {
		resp.CacheAgeMs = snap - e.SnapTime
	}
	return resp, nil
}

// Record takes a collected result (the pipeline's OnRecord, in game-time
// order): a miss's answer is kept for later requests.
func (b *Backend) Record(r *decide.Record) {
	req := r.Result.Req
	if req == nil {
		return
	}
	b.mu.Lock()
	delete(b.found, req.Seq) // a hit the budget refused never reached Decide
	ak, asked := b.asked[req.Seq]
	delete(b.asked, req.Seq)
	resp := r.Result.Resp
	switch {
	case resp != nil && resp.Cache != "":
		b.stats.Hits[req.Lane]++
	case asked:
		b.stats.Misses[req.Lane]++
		if ak.inflight {
			b.stats.InFlight[req.Lane]++
		}
	}
	b.mu.Unlock()
	if !asked {
		return
	}
	var e *Entry
	if r.Result.Err == nil {
		e = EntryOf(b.ep, req.SnapTime, req.Questions, resp)
	}
	b.store.Answered(ak.key, e)
}

// Stats returns the episode's counts.
func (b *Backend) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stats
}

// Summary describes the counts and the store, for a log line.
func (b *Backend) Summary() string {
	st := b.Stats()
	n, ev := b.store.Len()
	var parts string
	for l := decide.Lane(0); l < decide.NumLanes; l++ {
		h, m := st.Hits[l], st.Misses[l]
		rate := 0.0
		if h+m > 0 {
			rate = 100 * float64(h) / float64(h+m)
		}
		parts += fmt.Sprintf(" %s %d hits / %d misses (%.1f%%, %d in flight),", l, h, m, rate, st.InFlight[l])
	}
	return fmt.Sprintf("cache %s:%s %d entries kept, %d evicted", b.store.Config(), parts, n, ev)
}
