package decide

import (
	"context"
	"errors"
	"math"
	"sort"
	"sync"
	"time"
)

// RunMode selects how the scheduler relates backend calls to the game
// clock.
type RunMode uint8

// Run modes.
const (
	// Realtime runs each call on its own goroutine with a deadline and
	// collects whatever has finished (wall-clock latency).
	Realtime RunMode = iota
	// Lockstep makes a request submitted at virtual time t due at
	// t + SimLatency; Collect(now) waits for the backend only for requests
	// due by now, so runs with a deterministic backend repeat exactly.
	Lockstep
)

// String returns "realtime" or "lockstep".
func (m RunMode) String() string {
	if m == Lockstep {
		return "lockstep"
	}
	return "realtime"
}

// Scheduler defaults.
const (
	DefaultFastHz       = 10
	DefaultSlowInterval = 500 * time.Millisecond
	DefaultSlowMinGap   = 200 * time.Millisecond
	DefaultFastTimeout  = 800 * time.Millisecond
	DefaultSlowTimeout  = 2500 * time.Millisecond
	DefaultPriorP95     = 300 * time.Millisecond
	DefaultWallTimeout  = 60 * time.Second
	DefaultHardInFlight = 16
)

// SchedulerConfig configures a Scheduler. Zero values take the defaults.
type SchedulerConfig struct {
	Backend DecisionBackend
	Mode    RunMode
	// FastHz is the fast lane's rate while a visible enemy, incoming fire
	// or danger make it urgent (DefaultFastHz); ReducedHz its rate while
	// only remembered enemies exist (FastHz/2).
	FastHz, ReducedHz float64
	// SlowInterval is the slow lane's period (2 Hz); SlowMinGap the least
	// time between slow requests when events ask for an early one.
	SlowInterval, SlowMinGap time.Duration
	// FastTimeout and SlowTimeout bound one call (the context deadline in
	// realtime; in lockstep a SimLatency beyond it resolves as a timeout).
	FastTimeout, SlowTimeout time.Duration
	// MaxInFlightFast and MaxInFlightSlow cap the requests in flight per
	// lane; 0 means ceil(rate × p95), at least 1. HardMaxInFlight bounds
	// both (DefaultHardInFlight).
	MaxInFlightFast, MaxInFlightSlow, HardMaxInFlight int
	// PriorP95 stands for the latency p95 until one is measured.
	PriorP95 time.Duration
	// SimLatency is the lockstep latency model (nil: zero latency).
	SimLatency LatencyModel
	// WallTimeout bounds how long a lockstep Collect waits for a backend
	// (a hang guard; hitting it makes a run non-deterministic).
	WallTimeout time.Duration
	// StaleAfter is how old (since SnapTime) a result may arrive and still
	// be applied; later ones are marked Stale (nil: the lane's timeout).
	StaleAfter func(Lane) time.Duration
	// LatencyWindow is the number of latencies the p95 is taken over.
	LatencyWindow int
}

// Activity is what the caller sees this tick, for the lane cadence.
type Activity struct {
	// Combat: enemies or projectiles exist; the fast lane runs.
	Combat bool
	// Urgent: a visible enemy, incoming fire or danger; the fast lane runs
	// at FastHz (else ReducedHz).
	Urgent bool
	// Event: a new enemy, damage, an objective change or a pickup; the
	// slow lane asks early (after SlowMinGap).
	Event bool
}

// Result is a finished request.
type Result struct {
	Req  *Request
	Resp *Response // nil when Err is set
	Err  error
	// Latency is the wall-clock latency (realtime) or the simulated one
	// (lockstep; it may exceed the timeout when Timeout is set).
	Latency time.Duration
	// Arrived is when the result was collected (lockstep: its due time),
	// in the session clock.
	Arrived int64
	// Timeout: the call ran out of time.
	Timeout bool
	// Stale: it arrived later than StaleAfter; it must not be applied.
	Stale bool
}

// LaneStats counts one lane's requests.
type LaneStats struct {
	Submitted, Dropped, Completed, Errors, Timeouts, Stale int
}

// SchedulerStats are the scheduler's counters.
type SchedulerStats struct {
	Lanes    [NumLanes]LaneStats
	P50, P95 time.Duration
}

// ErrSchedulerClosed is returned for requests cut short by Close.
var ErrSchedulerClosed = errors.New("decide: scheduler closed")

type outcome struct {
	req     *Request
	resp    *Response
	err     error
	lat     time.Duration
	timeout bool
}

type pending struct {
	req     *Request
	lat     time.Duration
	due     int64
	timeout bool
	done    chan struct{}
	resp    *Response
	err     error
}

// Scheduler runs backend calls without blocking its caller: Submit starts
// a call and returns at once, Collect returns what has finished. All
// methods must be called from one goroutine (the control loop); the
// backend calls run on their own.
type Scheduler struct {
	cfg SchedulerConfig
	lat *LatencyStats

	last      [NumLanes]int64
	submitted [NumLanes]bool
	urgent    bool // the last Want's fast rate was FastHz
	stats     SchedulerStats

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	results  chan outcome // realtime
	inflight [NumLanes]int
	pending  []*pending // lockstep
	closed   bool
}

// NewScheduler returns a scheduler for cfg.Backend.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.FastHz <= 0 {
		cfg.FastHz = DefaultFastHz
	}
	if cfg.ReducedHz <= 0 {
		cfg.ReducedHz = cfg.FastHz / 2
	}
	defd := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	defd(&cfg.SlowInterval, DefaultSlowInterval)
	defd(&cfg.SlowMinGap, DefaultSlowMinGap)
	defd(&cfg.FastTimeout, DefaultFastTimeout)
	defd(&cfg.SlowTimeout, DefaultSlowTimeout)
	defd(&cfg.PriorP95, DefaultPriorP95)
	defd(&cfg.WallTimeout, DefaultWallTimeout)
	if cfg.HardMaxInFlight <= 0 {
		cfg.HardMaxInFlight = DefaultHardInFlight
	}
	if cfg.SimLatency == nil {
		cfg.SimLatency = FixedLatency(0)
	}
	s := &Scheduler{cfg: cfg, lat: NewLatencyStats(cfg.LatencyWindow)}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.results = make(chan outcome, NumLanes*cfg.HardMaxInFlight)
	return s
}

// Mode returns the run mode.
func (s *Scheduler) Mode() RunMode { return s.cfg.Mode }

// P95 is the latency p95 over the window (PriorP95 before any result).
func (s *Scheduler) P95() time.Duration {
	if s.lat.Count() == 0 {
		return s.cfg.PriorP95
	}
	return s.lat.Quantile(0.95)
}

// Timeout returns the per-call timeout of a lane.
func (s *Scheduler) Timeout(l Lane) time.Duration {
	if l == LaneSlow {
		return s.cfg.SlowTimeout
	}
	return s.cfg.FastTimeout
}

// MaxInFlight is the current in-flight cap of a lane.
func (s *Scheduler) MaxInFlight(l Lane) int {
	n, rate := s.cfg.MaxInFlightFast, s.cfg.FastHz
	if l == LaneSlow {
		n, rate = s.cfg.MaxInFlightSlow, float64(time.Second)/float64(s.cfg.SlowInterval)
	}
	if n <= 0 {
		n = int(math.Ceil(rate*s.P95().Seconds() - 1e-9))
	}
	return max(1, min(n, s.cfg.HardMaxInFlight))
}

func durMs(d time.Duration) int64 { return int64(math.Round(float64(d) / float64(time.Millisecond))) }

// Interval is a lane's current request interval: the fast lane's at the
// rate the last Want chose, the slow lane's SlowInterval.
func (s *Scheduler) Interval(l Lane) time.Duration {
	if l == LaneSlow {
		return s.cfg.SlowInterval
	}
	hz := s.cfg.FastHz
	if !s.urgent {
		hz = s.cfg.ReducedHz
	}
	return time.Duration(math.Round(float64(time.Second) / hz))
}

// Want reports which lanes are due at now (session ms). Submit records
// the requests; Want only notes the fast rate it chose (Interval).
func (s *Scheduler) Want(now int64, a Activity) (fast, slow bool) {
	if a.Combat {
		s.urgent = a.Urgent
		iv := durMs(s.Interval(LaneFast))
		fast = !s.submitted[LaneFast] || now-s.last[LaneFast] >= iv
	}
	since := now - s.last[LaneSlow]
	slow = !s.submitted[LaneSlow] || since >= durMs(s.cfg.SlowInterval) || a.Event && since >= durMs(s.cfg.SlowMinGap)
	return fast, slow
}

// InFlight is the number of requests of a lane not yet collected
// (lockstep: not yet due at the lane's last submit).
func (s *Scheduler) InFlight(l Lane) int {
	if s.cfg.Mode == Lockstep {
		n := 0
		for _, p := range s.pending {
			if p.req.Lane == l {
				n++
			}
		}
		return n
	}
	return s.inflight[l]
}

// Submit starts req (its SnapTime is the submit time) unless the lane is
// at its in-flight cap, in which case the request is dropped and Submit
// returns false. It never blocks.
func (s *Scheduler) Submit(req *Request) bool {
	if s.closed {
		return false
	}
	l := req.Lane
	s.last[l], s.submitted[l] = req.SnapTime, true
	ls := &s.stats.Lanes[l]
	ls.Submitted++
	if s.cfg.Mode == Lockstep {
		return s.submitLockstep(req, ls)
	}
	if s.inflight[l] >= s.MaxInFlight(l) {
		ls.Dropped++
		return false
	}
	s.inflight[l]++
	ctx, cancel := context.WithTimeout(s.ctx, s.Timeout(l))
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		start := time.Now()
		resp, err := s.cfg.Backend.Decide(ctx, req)
		lat := time.Since(start)
		to := err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded))
		// the channel holds every lane's cap: this send never blocks
		s.results <- outcome{req: req, resp: resp, err: err, lat: lat, timeout: to}
	}()
	return true
}

func (s *Scheduler) submitLockstep(req *Request, ls *LaneStats) bool {
	n := 0
	for _, p := range s.pending {
		if p.req.Lane == req.Lane && p.due > req.SnapTime {
			n++
		}
	}
	if n >= s.MaxInFlight(req.Lane) {
		ls.Dropped++
		return false
	}
	lat := max(s.cfg.SimLatency.Latency(req), 0)
	p := &pending{req: req, lat: lat, due: req.SnapTime + durMs(lat), done: make(chan struct{})}
	if to := s.Timeout(req.Lane); lat > to {
		// resolved in sim time without asking the backend
		p.timeout, p.due, p.err = true, req.SnapTime+durMs(to), context.DeadlineExceeded
		close(p.done)
	} else {
		ctx, cancel := context.WithTimeout(s.ctx, s.cfg.WallTimeout)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer cancel()
			defer close(p.done)
			p.resp, p.err = s.cfg.Backend.Decide(ctx, req)
			if p.err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				p.timeout = true
			}
		}()
	}
	s.pending = append(s.pending, p)
	return true
}

func (s *Scheduler) staleAfter(l Lane) time.Duration {
	if s.cfg.StaleAfter != nil {
		return s.cfg.StaleAfter(l)
	}
	return s.Timeout(l)
}

func (s *Scheduler) finish(r *Result) {
	ls := &s.stats.Lanes[r.Req.Lane]
	ls.Completed++
	switch {
	case r.Timeout:
		ls.Timeouts++
	case r.Err != nil:
		ls.Errors++
	}
	if r.Err == nil || r.Timeout {
		s.lat.Add(min(r.Latency, s.Timeout(r.Req.Lane)))
	}
	if r.Err == nil && r.Resp == nil {
		r.Err = errors.New("decide: backend returned no response")
		ls.Errors++
	}
	if r.Err == nil && time.Duration(r.Arrived-r.Req.SnapTime)*time.Millisecond > s.staleAfter(r.Req.Lane) {
		r.Stale = true
		ls.Stale++
	}
}

// Collect returns the requests finished by now (session ms). Realtime: the
// calls that have returned, without waiting. Lockstep: every request due
// by now, waiting for the backend if it has not returned yet, in (due,
// Seq) order. Stale results are returned marked, for the trace.
func (s *Scheduler) Collect(now int64) []Result {
	var out []Result
	if s.cfg.Mode == Lockstep {
		sort.SliceStable(s.pending, func(i, j int) bool {
			a, b := s.pending[i], s.pending[j]
			if a.due != b.due {
				return a.due < b.due
			}
			return a.req.Seq < b.req.Seq
		})
		keep := s.pending[:0]
		for _, p := range s.pending {
			if p.due > now {
				keep = append(keep, p)
				continue
			}
			<-p.done
			r := Result{Req: p.req, Resp: p.resp, Err: p.err, Latency: p.lat, Arrived: p.due, Timeout: p.timeout}
			s.finish(&r)
			out = append(out, r)
		}
		for i := len(keep); i < len(s.pending); i++ {
			s.pending[i] = nil
		}
		s.pending = keep
		return out
	}
	for {
		select {
		case o := <-s.results:
			s.inflight[o.req.Lane]--
			r := Result{Req: o.req, Resp: o.resp, Err: o.err, Latency: o.lat, Arrived: now, Timeout: o.timeout}
			s.finish(&r)
			out = append(out, r)
		default:
			sort.SliceStable(out, func(i, j int) bool { return out[i].Req.Seq < out[j].Req.Seq })
			return out
		}
	}
}

// Stats returns the counters.
func (s *Scheduler) Stats() SchedulerStats {
	st := s.stats
	if s.lat.Count() > 0 {
		st.P50, st.P95 = s.lat.Quantile(0.5), s.lat.Quantile(0.95)
	}
	return st
}

// Close cancels the calls in flight and waits for them to return. Later
// Submits are dropped.
func (s *Scheduler) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.cancel()
	s.wg.Wait()
	return nil
}
