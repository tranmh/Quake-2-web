package decide

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gate is a backend whose calls block until released (or their context
// ends).
type gate struct {
	release chan struct{}
	calls   atomic.Int32
}

func newGate() *gate { return &gate{release: make(chan struct{})} }

func (g *gate) Name() string { return "gate" }
func (g *gate) Decide(ctx context.Context, req *Request) (*Response, error) {
	g.calls.Add(1)
	select {
	case <-g.release:
		return &Response{Seq: req.Seq, Answers: map[string]Answer{}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// instant answers at once.
var instant = &funcBackend{name: "instant", fn: func(_ context.Context, req *Request) (*Response, error) {
	return &Response{Seq: req.Seq, Answers: map[string]Answer{}}, nil
}}

func bareReq(seq uint64, l Lane, snap int64) *Request {
	return &Request{Seq: seq, Lane: l, SnapTime: snap, State: []byte(`{}`)}
}

func TestSchedulerNeverBlocks(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, PriorP95: 450 * time.Millisecond})
	defer s.Close()
	start := time.Now()
	for i := 1; i <= 3; i++ {
		if !s.Submit(bareReq(uint64(i), LaneFast, int64(i*100))) {
			t.Fatalf("request %d dropped", i)
		}
		if got := s.Collect(int64(i * 100)); len(got) != 0 {
			t.Fatalf("results while the backend is blocked: %d", len(got))
		}
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("Submit/Collect took %v with a blocked backend", d)
	}
	if s.InFlight(LaneFast) != 3 {
		t.Fatalf("in flight %d", s.InFlight(LaneFast))
	}
	close(g.release)
	var got []Result
	deadline := time.Now().Add(5 * time.Second)
	for len(got) < 3 && time.Now().Before(deadline) {
		got = append(got, s.Collect(400)...)
		time.Sleep(time.Millisecond)
	}
	if len(got) != 3 || s.InFlight(LaneFast) != 0 {
		t.Fatalf("collected %d, in flight %d", len(got), s.InFlight(LaneFast))
	}
	for _, r := range got {
		if r.Err != nil || r.Resp == nil || r.Arrived != 400 || r.Latency <= 0 {
			t.Errorf("result %+v", r)
		}
	}
}

func TestSchedulerInFlightCap(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, PriorP95: 450 * time.Millisecond})
	// ceil(10 Hz x 0.45 s) = 5; slow: ceil(2 Hz x 0.45 s) = 1
	if s.MaxInFlight(LaneFast) != 5 || s.MaxInFlight(LaneSlow) != 1 {
		t.Fatalf("caps %d %d", s.MaxInFlight(LaneFast), s.MaxInFlight(LaneSlow))
	}
	accepted := 0
	for i := 1; i <= 7; i++ {
		if s.Submit(bareReq(uint64(i), LaneFast, int64(i))) {
			accepted++
		}
	}
	if !s.Submit(bareReq(8, LaneSlow, 8)) || s.Submit(bareReq(9, LaneSlow, 9)) {
		t.Fatal("slow lane cap")
	}
	if st := s.Stats(); accepted != 5 || st.Lanes[LaneFast].Dropped != 2 || st.Lanes[LaneSlow].Dropped != 1 {
		t.Fatalf("accepted %d, stats %+v", accepted, st.Lanes)
	}
	close(g.release)
	s.Close() // waits for the calls
	if n := len(s.Collect(10)); n != 6 {
		t.Fatalf("collected %d after Close", n)
	}
	if s.Submit(bareReq(10, LaneFast, 10)) {
		t.Fatal("Submit after Close accepted")
	}

	// explicit caps, and caps that follow the measured p95
	s = NewScheduler(SchedulerConfig{Backend: instant, Mode: Lockstep, MaxInFlightFast: 2, SimLatency: FixedLatency(250 * time.Millisecond)})
	defer s.Close()
	if s.MaxInFlight(LaneFast) != 2 {
		t.Fatalf("explicit cap %d", s.MaxInFlight(LaneFast))
	}
	s2 := NewScheduler(SchedulerConfig{Backend: instant, Mode: Lockstep, SimLatency: FixedLatency(250 * time.Millisecond)})
	defer s2.Close()
	for i := int64(0); i < 20; i++ {
		s2.Submit(bareReq(uint64(i+1), LaneFast, i*100))
		s2.Collect(i * 100)
	}
	if s2.P95() != 250*time.Millisecond || s2.MaxInFlight(LaneFast) != 3 || s2.Stats().Lanes[LaneFast].Dropped != 0 {
		t.Fatalf("p95 %v cap %d stats %+v", s2.P95(), s2.MaxInFlight(LaneFast), s2.Stats().Lanes)
	}
}

// TestSchedulerCadence drives Want/Submit for 10 s on a 25 ms loop.
func TestSchedulerCadence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		act        func(now int64) Activity
		fast, slow int
	}{
		{"out of combat", func(int64) Activity { return Activity{} }, 0, 20},
		{"urgent combat", func(int64) Activity { return Activity{Combat: true, Urgent: true} }, 100, 20},
		{"remembered enemies only", func(int64) Activity { return Activity{Combat: true} }, 50, 20},
		{"events every tick", func(int64) Activity { return Activity{Event: true} }, 0, 50},
		{"an event at 3.3 s", func(now int64) Activity { return Activity{Event: now == 3300} }, 0, 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScheduler(SchedulerConfig{Backend: instant, Mode: Lockstep})
			defer s.Close()
			var n [NumLanes]int
			seq := uint64(0)
			for now := int64(0); now < 10000; now += 25 {
				f, sl := s.Want(now, tc.act(now))
				for l, want := range []bool{f, sl} {
					if want {
						seq++
						s.Submit(bareReq(seq, Lane(l), now))
						n[l]++
					}
				}
				s.Collect(now)
			}
			if n[LaneFast] != tc.fast || n[LaneSlow] != tc.slow {
				t.Fatalf("fast %d slow %d, want %d %d", n[LaneFast], n[LaneSlow], tc.fast, tc.slow)
			}
		})
	}
}

// jitter answers after a short random wall delay: lockstep results must
// not depend on it.
type jitter struct{ calls atomic.Int64 }

func (j *jitter) Name() string { return "jitter" }
func (j *jitter) Decide(ctx context.Context, req *Request) (*Response, error) {
	n := j.calls.Add(1)
	time.Sleep(time.Duration(n%4) * time.Millisecond)
	if req.Seq%7 == 0 {
		return nil, fmt.Errorf("backend error for %d", req.Seq)
	}
	return &Response{Seq: req.Seq, Answers: map[string]Answer{}, Model: "jitter"}, nil
}

func TestSchedulerLockstepDeterministic(t *testing.T) {
	samples := []time.Duration{80 * time.Millisecond, 150 * time.Millisecond, 212 * time.Millisecond, 400 * time.Millisecond, 900 * time.Millisecond}
	run := func() ([]string, int64) {
		j := &jitter{}
		s := NewScheduler(SchedulerConfig{Backend: j, Mode: Lockstep, SimLatency: &SampledLatency{Seed: 3, Samples: samples}, HardMaxInFlight: 8})
		defer s.Close()
		var log []string
		seq := uint64(0)
		for now := int64(0); now <= 3000; now += 100 {
			if f, _ := s.Want(now, Activity{Combat: true, Urgent: true}); f {
				seq++
				s.Submit(bareReq(seq, LaneFast, now))
			}
			for _, r := range s.Collect(now) {
				due := r.Req.SnapTime + durMs(r.Latency)
				if r.Timeout {
					due = r.Req.SnapTime + durMs(s.Timeout(LaneFast))
				}
				if r.Arrived != due || r.Arrived > now || r.Arrived <= now-100 {
					t.Fatalf("request %d due %d arrived %d collected at %d", r.Req.Seq, due, r.Arrived, now)
				}
				log = append(log, fmt.Sprintf("%d@%d lat=%v to=%v err=%v", r.Req.Seq, now, r.Latency, r.Timeout, r.Err))
			}
		}
		for _, r := range s.Collect(5000) { // drain
			log = append(log, fmt.Sprintf("%d@end lat=%v to=%v err=%v", r.Req.Seq, r.Latency, r.Timeout, r.Err))
		}
		st := s.Stats().Lanes[LaneFast]
		if st.Submitted-st.Dropped != len(log) {
			t.Fatalf("submitted %d dropped %d, %d results", st.Submitted, st.Dropped, len(log))
		}
		return log, j.calls.Load()
	}
	a, callsA := run()
	b, callsB := run()
	if len(a) == 0 || fmt.Sprint(a) != fmt.Sprint(b) || callsA != callsB {
		t.Fatalf("runs differ:\n%v\n%v", a, b)
	}
	timeouts, errs := 0, 0
	for _, l := range a {
		if contains(l, "to=true") {
			timeouts++
		}
		if contains(l, "backend error") {
			errs++
		}
	}
	// 900 ms > the 800 ms timeout: resolved in sim time without a call
	if timeouts == 0 || errs == 0 || int(callsA) != len(a)-timeouts {
		t.Fatalf("%d results, %d timeouts, %d errors, %d calls", len(a), timeouts, errs, callsA)
	}
	t.Logf("%d results, %d timeouts, %d backend errors", len(a), timeouts, errs)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSchedulerLockstepWaitsOnlyForDue: Collect blocks for a due request
// whose backend has not returned, and never for one not yet due.
func TestSchedulerLockstepWaitsOnlyForDue(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, Mode: Lockstep, SimLatency: FixedLatency(200 * time.Millisecond)})
	defer s.Close()
	s.Submit(bareReq(1, LaneFast, 0))
	start := time.Now()
	if got := s.Collect(100); len(got) != 0 || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("Collect before due: %d results after %v", len(got), time.Since(start))
	}
	done := make(chan []Result)
	go func() { done <- s.Collect(200) }()
	select {
	case <-done:
		t.Fatal("Collect returned before the backend did")
	case <-time.After(50 * time.Millisecond):
	}
	close(g.release)
	select {
	case got := <-done:
		if len(got) != 1 || got[0].Arrived != 200 || got[0].Latency != 200*time.Millisecond {
			t.Fatalf("result %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Collect did not return")
	}
}

func TestSchedulerRealtimeTimeoutAndStale(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, FastTimeout: 30 * time.Millisecond})
	s.Submit(bareReq(1, LaneFast, 0))
	var got []Result
	deadline := time.Now().Add(5 * time.Second)
	for len(got) == 0 && time.Now().Before(deadline) {
		got = s.Collect(50)
		time.Sleep(2 * time.Millisecond)
	}
	if len(got) != 1 || !got[0].Timeout || got[0].Err == nil {
		t.Fatalf("result %+v", got)
	}
	if st := s.Stats(); st.Lanes[LaneFast].Timeouts != 1 {
		t.Fatalf("stats %+v", st.Lanes)
	}
	s.Close()

	// arriving later than StaleAfter (lockstep: latency 150 > 100 ms)
	ls := NewScheduler(SchedulerConfig{Backend: instant, Mode: Lockstep, SimLatency: FixedLatency(150 * time.Millisecond),
		StaleAfter: func(Lane) time.Duration { return 100 * time.Millisecond }})
	defer ls.Close()
	ls.Submit(bareReq(1, LaneFast, 0))
	if r := ls.Collect(200); len(r) != 1 || !r[0].Stale || ls.Stats().Lanes[LaneFast].Stale != 1 {
		t.Fatalf("stale %+v", r)
	}
}

// TestSchedulerCloseCancels: Close cancels calls in flight and waits.
func TestSchedulerCloseCancels(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, SlowTimeout: time.Hour})
	s.Submit(bareReq(1, LaneSlow, 0))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.Close() }()
	ch := make(chan struct{})
	go func() { wg.Wait(); close(ch) }()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the call")
	}
	if r := s.Collect(0); len(r) != 1 || r[0].Err == nil {
		t.Fatalf("cancelled call %+v", r)
	}
}

func TestLatencyStats(t *testing.T) {
	l := NewLatencyStats(4)
	if l.Quantile(0.95) != 0 {
		t.Fatal("empty")
	}
	for _, ms := range []int{10, 20, 30, 40, 50} { // the window keeps the last 4
		l.Add(time.Duration(ms) * time.Millisecond)
	}
	if l.Count() != 4 || l.Total() != 5 || l.Quantile(0) != 20*time.Millisecond || l.Quantile(0.5) != 30*time.Millisecond || l.Quantile(0.95) != 50*time.Millisecond {
		t.Fatalf("count %d q0 %v q50 %v q95 %v", l.Count(), l.Quantile(0), l.Quantile(0.5), l.Quantile(0.95))
	}
	sl := &SampledLatency{Seed: 1, Samples: []time.Duration{1, 2, 3}}
	seen := map[time.Duration]bool{}
	for i := uint64(0); i < 50; i++ {
		d := sl.Latency(&Request{Seq: i})
		if d != sl.Latency(&Request{Seq: i}) {
			t.Fatal("not a function of Seq")
		}
		seen[d] = true
	}
	if len(seen) != 3 {
		t.Fatalf("samples drawn %v", seen)
	}
}

// TestSchedulerRefusedLaneRetriesWhenFree: a lane refused at its cap is
// due again as soon as a slot frees, not a whole interval after the
// refusal.
func TestSchedulerRefusedLaneRetriesWhenFree(t *testing.T) {
	s := NewScheduler(SchedulerConfig{Backend: instant, Mode: Lockstep, MaxInFlightSlow: 1, SimLatency: FixedLatency(650 * time.Millisecond)})
	defer s.Close()
	var asked []int64
	seq := uint64(0)
	for now := int64(0); now <= 1500; now += 100 {
		if _, slow := s.Want(now, Activity{}); slow {
			seq++
			if s.Submit(bareReq(seq, LaneSlow, now)) {
				asked = append(asked, now)
			}
		}
		s.Collect(now)
	}
	// 0 (due 650); 500 refused; 700 once the slot is free (due 1350);
	// 1200 refused; 1400
	if fmt.Sprint(asked) != "[0 700 1400]" {
		t.Fatalf("accepted slow requests at %v", asked)
	}
	if st := s.Stats().Lanes[LaneSlow]; st.Dropped != 2 {
		t.Fatalf("stats %+v", st)
	}

	// realtime: a call that has returned frees its slot before Collect
	rt := NewScheduler(SchedulerConfig{Backend: instant, MaxInFlightFast: 1})
	defer rt.Close()
	if !rt.Submit(bareReq(1, LaneFast, 0)) {
		t.Fatal("first request dropped")
	}
	deadline := time.Now().Add(5 * time.Second)
	for rt.InFlight(LaneFast) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !rt.Submit(bareReq(2, LaneFast, 50)) {
		t.Fatal("the returned call still held the slot")
	}
	var got []Result
	for len(got) < 2 && time.Now().Before(deadline) {
		got = append(got, rt.Collect(100)...)
		time.Sleep(time.Millisecond)
	}
	if len(got) != 2 || got[0].Req.Seq != 1 || got[0].Arrived != 100 {
		t.Fatalf("results %+v", got)
	}
}

// TestSchedulerRealtimeFastCutAtStale: a realtime fast call ends at
// StaleAfter (its answer would be dropped anyway); the slow lane keeps its
// timeout.
func TestSchedulerRealtimeFastCutAtStale(t *testing.T) {
	g := newGate()
	s := NewScheduler(SchedulerConfig{Backend: g, FastTimeout: time.Hour, SlowTimeout: time.Hour,
		StaleAfter: func(Lane) time.Duration { return 40 * time.Millisecond }})
	defer s.Close()
	start := time.Now()
	s.Submit(bareReq(1, LaneFast, 0))
	s.Submit(bareReq(2, LaneSlow, 0))
	var got []Result
	for len(got) == 0 && time.Since(start) < 5*time.Second {
		got = s.Collect(int64(time.Since(start) / time.Millisecond))
		time.Sleep(time.Millisecond)
	}
	if len(got) != 1 || got[0].Req.Lane != LaneFast || !got[0].Timeout || got[0].Latency < 30*time.Millisecond {
		t.Fatalf("results %+v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if r := s.Collect(200); len(r) != 0 || s.InFlight(LaneSlow) != 1 {
		t.Fatalf("the slow call ended: %+v", r)
	}
	close(g.release)
}

// TestSchedulerLockstepBackendTimeout: a backend error that is a deadline
// (a timed-out attempt, a replayed timeout) is a timeout in lockstep too.
func TestSchedulerLockstepBackendTimeout(t *testing.T) {
	be := &funcBackend{name: "late", fn: func(_ context.Context, req *Request) (*Response, error) {
		if req.Seq == 1 {
			return nil, fmt.Errorf("attempt: %w", context.DeadlineExceeded)
		}
		return nil, fmt.Errorf("refused")
	}}
	s := NewScheduler(SchedulerConfig{Backend: be, Mode: Lockstep, SimLatency: FixedLatency(100 * time.Millisecond)})
	defer s.Close()
	s.Submit(bareReq(1, LaneFast, 0))
	s.Submit(bareReq(2, LaneSlow, 0))
	r := s.Collect(100)
	if len(r) != 2 || !r[0].Timeout || r[1].Timeout || r[1].Err == nil {
		t.Fatalf("results %+v", r)
	}
	if st := s.Stats(); st.Lanes[LaneFast].Timeouts != 1 || st.Lanes[LaneSlow].Errors != 1 {
		t.Fatalf("stats %+v", st.Lanes)
	}
}
