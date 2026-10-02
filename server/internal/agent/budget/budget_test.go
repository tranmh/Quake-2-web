package budget

import (
	"context"
	"errors"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/decide"
)

// The account is the jev client's shared limiter.
var _ jev.Limiter = (*Account)(nil)

func TestParsePolicy(t *testing.T) {
	for in, want := range map[string]Policy{"": Fallback, "fallback": Fallback, "stop": Stop} {
		if got, err := ParsePolicy(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := ParsePolicy("panic"); err == nil {
		t.Error("bad policy accepted")
	}
}

func TestLadder(t *testing.T) {
	b := New(Limits{USD: 1}, nil)
	steps := []struct {
		charge   float64
		hz       float64
		scripted bool
		reason   string
	}{
		{0, 10, false, ""},
		{0.4, 10, false, ""},
		{0.1, 5, false, "$0.500000 of $1.000000 spent: fast lane at 5 Hz"},
		{0.2, 5, false, "fast lane at 5 Hz"},
		{0.06, 2, false, "fast lane at 2 Hz"},
		{0.239, 2, false, "fast lane at 2 Hz"},
		{0.001, 0, true, "budget exhausted ($1.000000 of $1.000000 spent): scripted only"},
		{5, 0, true, "budget exhausted"},
	}
	for i, s := range steps {
		if s.charge > 0 {
			b.Charge(s.charge, 100)
		}
		st := b.State()
		if st.FastHz != s.hz || st.ScriptedOnly != s.scripted || st.Exhausted != s.scripted || !strings.Contains(st.Reason, s.reason) {
			t.Fatalf("step %d: %+v", i, st)
		}
		if st.Remaining < 0 || st.Remaining > 1 {
			t.Fatalf("step %d: remaining %v", i, st.Remaining)
		}
	}

	// queries are a limit of their own; the tighter one rules
	q := New(Limits{USD: 100, Queries: 4}, nil)
	for i := 0; i < 2; i++ {
		q.Charge(0.01, 10)
	}
	if st := q.State(); st.FastHz != 5 || st.Queries != 2 || !strings.Contains(st.Reason, "2 of 4 queries") {
		t.Fatalf("queries: %+v", st)
	}
	q.Charge(0, 0)
	q.Charge(0, 0)
	if st := q.State(); !st.Exhausted {
		t.Fatalf("queries exhausted: %+v", st)
	}

	// a custom ladder can end in scripted-only before the budget is spent
	c := New(Limits{USD: 1, Ladder: []Step{{Remaining: 0.1, FastHz: 0}, {Remaining: 0.9, FastHz: 8}}}, nil)
	c.Charge(0.2, 0)
	if st := c.State(); st.FastHz != 8 {
		t.Fatalf("custom ladder: %+v", st)
	}
	c.Charge(0.75, 0)
	if st := c.State(); !st.ScriptedOnly || st.Exhausted {
		t.Fatalf("custom ladder bottom: %+v", st)
	}

	if st := New(Limits{}, nil).State(); st.Remaining != 1 || st.FastHz != DefaultFullHz || st.ScriptedOnly || st.Reason != "" {
		t.Fatalf("no limits: %+v", st)
	}
	if (Limits{}).Enabled() || !(Limits{MaxQPS: 1}).Enabled() {
		t.Fatal("Enabled")
	}
}

// fakeBackend answers every request with a fixed cost.
type fakeBackend struct {
	calls atomic.Int64
	cost  float64
}

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) Decide(_ context.Context, req *decide.Request) (*decide.Response, error) {
	f.calls.Add(1)
	return &decide.Response{Seq: req.Seq, CostUSD: f.cost, Usage: decide.Usage{InputTokens: 1000}}, nil
}

// tick is what one tick of tickRun let through.
type tick struct {
	now        int64
	hz         float64 // the fast rate planned
	fast, slow bool    // the lane's request got through
	slowAsked  bool
}

// tickRun drives a gate like a pipeline: every tick (100 ms) a fast
// request, every fifth a slow one; each request's call runs on its own
// goroutine (in a shuffled order) and is charged after all of a tick's
// calls returned.
func tickRun(t *testing.T, g *Gate, inner decide.DecisionBackend, ticks int, seed int64) []tick {
	t.Helper()
	be := g.Wrap(inner)
	if be.Name() != inner.Name() {
		t.Fatalf("wrapper name %q", be.Name())
	}
	rng := rand.New(rand.NewSource(seed))
	var out []tick
	seq := uint64(0)
	for i := 0; i < ticks; i++ {
		now := int64(i) * 100
		st := g.Plan(now)
		tk := tick{now: now, hz: st.FastHz, slowAsked: i%5 == 0}
		var reqs []*decide.Request
		lanes := []decide.Lane{decide.LaneFast}
		if tk.slowAsked {
			lanes = append(lanes, decide.LaneSlow)
		}
		for _, l := range lanes {
			seq++
			reqs = append(reqs, &decide.Request{Seq: seq, Lane: l, SnapTime: now})
			g.Submitted(now, l)
		}
		res := make([]decide.Result, len(reqs))
		var wg sync.WaitGroup
		for _, j := range rng.Perm(len(reqs)) {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				resp, err := be.Decide(context.Background(), reqs[j])
				res[j] = decide.Result{Req: reqs[j], Resp: resp, Err: err}
			}(j)
		}
		wg.Wait()
		for j := range res {
			if res[j].Err != nil && !IsRefused(res[j].Err) {
				t.Fatal(res[j].Err)
			}
			if res[j].Req.Lane == decide.LaneFast {
				tk.fast = res[j].Err == nil
			} else {
				tk.slow = res[j].Err == nil
			}
			g.Charge(&decide.Record{Result: res[j]})
		}
		out = append(out, tk)
	}
	return out
}

func TestGateDegrades(t *testing.T) {
	inner := &fakeBackend{cost: 0.01}
	b := New(Limits{USD: 1}, nil)
	g := b.Gate()
	got := tickRun(t, g, inner, 400, 1)

	// the fast lane per phase of the budget: every tick at 10 Hz, every
	// other at 5 Hz, every fifth at 2 Hz, none once exhausted; the slow
	// lane until exhausted
	asked, sent := map[float64]int{}, map[float64]int{}
	total := 0
	for _, tk := range got {
		asked[tk.hz]++
		if tk.fast {
			sent[tk.hz]++
			total++
		}
		if tk.slow {
			total++
		}
		if tk.hz == 0 && (tk.fast || tk.slow) {
			t.Fatalf("a request at %d ms after exhaustion", tk.now)
		}
		if tk.slowAsked && tk.hz > 0 && !tk.slow {
			t.Fatalf("slow request refused at %d ms (%g Hz)", tk.now, tk.hz)
		}
	}
	for hz, frac := range map[float64]float64{10: 1, 5: 0.5, 2: 0.2} {
		if asked[hz] < 10 {
			t.Fatalf("phase %g Hz has %d ticks", hz, asked[hz])
		}
		if r := float64(sent[hz]) / float64(asked[hz]); r < frac-0.1 || r > frac+0.1 {
			t.Errorf("at %g Hz %d of %d fast requests went through", hz, sent[hz], asked[hz])
		}
	}
	if total != 100 || inner.calls.Load() != 100 {
		t.Fatalf("%d sent, %d calls, want 100 (the budget)", total, inner.calls.Load())
	}
	if st := b.State(); !st.Exhausted || st.Queries != 100 {
		t.Fatalf("final state %+v", st)
	}
	if r := g.Refused(); r[decide.LaneFast] == 0 || r[decide.LaneSlow] == 0 {
		t.Fatalf("refused %v", r)
	}

	// the same run repeats exactly, whatever order the calls ran in
	again := tickRun(t, New(Limits{USD: 1}, nil).Gate(), &fakeBackend{cost: 0.01}, 400, 99)
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("tick %d: %+v then %+v", i, got[i], again[i])
		}
	}
}

func TestGateMaxQPS(t *testing.T) {
	g := New(Limits{MaxQPS: 3}, nil).Gate()
	var times []int64
	for _, tk := range tickRun(t, g, &fakeBackend{}, 100, 3) {
		if tk.fast {
			times = append(times, tk.now)
		}
		if tk.slow {
			times = append(times, tk.now)
		}
	}
	for i := range times {
		n := 0
		for _, u := range times {
			if u <= times[i] && u > times[i]-1000 {
				n++
			}
		}
		if n > 3 {
			t.Fatalf("%d requests in the second up to %d ms", n, times[i])
		}
	}
	if len(times) < 28 || len(times) > 30 {
		t.Fatalf("%d requests in 10 s at 3 QPS", len(times))
	}
}

func TestGateChargesOnlySent(t *testing.T) {
	b := New(Limits{USD: 1}, nil)
	g := b.Gate()
	g.Plan(0)
	// a result for a request the wrapper never saw (a lockstep timeout
	// resolved without a call) costs nothing
	g.Charge(&decide.Record{Result: decide.Result{Req: &decide.Request{Seq: 7}, Err: context.DeadlineExceeded}})
	g.Charge(&decide.Record{})
	if st := b.State(); st.Queries != 0 {
		t.Fatalf("%+v", st)
	}
	// an unplanned tick is admitted (or refused once scripted only)
	resp, err := g.Wrap(&fakeBackend{cost: 2}).Decide(context.Background(), &decide.Request{Seq: 1, SnapTime: 55})
	if err != nil {
		t.Fatal(err)
	}
	g.Charge(&decide.Record{Result: decide.Result{Req: &decide.Request{Seq: 1}, Resp: resp}})
	g.Plan(100)
	_, err = g.Wrap(&fakeBackend{}).Decide(context.Background(), &decide.Request{Seq: 2, SnapTime: 155})
	var re *RefusedError
	if !errors.As(err, &re) || !errors.Is(err, ErrRefused) || !strings.HasPrefix(err.Error(), "budget: budget exhausted") {
		t.Fatalf("refusal %v", err)
	}
}

// clock is a settable wall clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func TestAccountLimiter(t *testing.T) {
	clk := &clock{t: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	a, err := NewAccount(context.Background(), AccountConfig{QPS: 5, Burst: 8, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	// a frozen clock: the burst, shared by every caller, and no more
	var ok atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if a.Allow(1000) {
					ok.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 8 || a.Stats().Allowed != 8 || a.Stats().Refused != 32 {
		t.Fatalf("%d allowed, stats %+v", ok.Load(), a.Stats())
	}
	clk.Add(time.Second) // refills 5
	n := 0
	for a.Allow(1) {
		n++
	}
	if n != 5 {
		t.Fatalf("%d after a second", n)
	}

	// the token rate: 3000 tokens a second
	tk, _ := NewAccount(context.Background(), AccountConfig{TokensPerSec: 3000, Now: clk.Now})
	if !tk.Allow(2000) || tk.Allow(2000) {
		t.Fatal("token bucket")
	}
	clk.Add(700 * time.Millisecond)
	if !tk.Allow(2000) {
		t.Fatal("token refill")
	}
	if !tk.Allow(0) {
		t.Fatal("a zero-token request")
	}
	// without limits everything passes
	free, _ := NewAccount(context.Background(), AccountConfig{Now: clk.Now})
	for i := 0; i < 100; i++ {
		if !free.Allow(1 << 20) {
			t.Fatal("unlimited account refused")
		}
	}
}

func TestAccountDaily(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 10, 2, 23, 59, 0, 0, time.UTC)}
	store := NewMemStore()
	if _, err := store.AddSpend(ctx, "2026-10-02", 0.5); err != nil {
		t.Fatal(err)
	}
	a, err := NewAccount(ctx, AccountConfig{DailyUSD: 1, Store: store, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	if s, l := a.Daily(); s != 0.5 || l != 1 {
		t.Fatalf("loaded %v of %v", s, l)
	}
	// a run's budget sees the account's daily remainder
	b := New(Limits{USD: 10}, a)
	b.Charge(0.3, 0)
	if st := b.State(); st.DailySpentUSD != 0.8 || st.FastHz != 2 || !strings.Contains(st.Reason, "daily") {
		t.Fatalf("daily state %+v", st)
	}
	// another process spent meanwhile: Flush persists ours and reads the total
	if _, err := store.AddSpend(ctx, "2026-10-02", 0.1); err != nil {
		t.Fatal(err)
	}
	if err := a.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.LoadSpend(ctx, "2026-10-02"); v < 0.9-1e-9 || v > 0.9+1e-9 {
		t.Fatalf("store %v", v)
	}
	if s, _ := a.Daily(); s < 0.9-1e-9 || s > 0.9+1e-9 {
		t.Fatalf("after flush %v", s)
	}
	b.Charge(0.2, 0)
	if !b.State().Exhausted {
		t.Fatalf("daily cap not exhausting: %+v", b.State())
	}
	// midnight: a new day starts from the store's (empty) total
	clk.Add(2 * time.Minute)
	if s, _ := a.Daily(); s != 0 {
		t.Fatalf("new day %v", s)
	}
	a.Add(0.25)
	if err := a.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := store.LoadSpend(ctx, "2026-10-03"); v != 0.25 {
		t.Fatalf("new day stored %v", v)
	}
	if v, _ := store.LoadSpend(ctx, "2026-10-02"); v < 1.1-1e-9 || v > 1.1+1e-9 {
		t.Fatalf("old day stored %v (the spend before midnight is flushed to its day)", v)
	}
	// a failing store keeps the spend for the next flush
	bad := &failStore{}
	f, err := NewAccount(ctx, AccountConfig{Store: bad, Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(1)
	bad.fail = true
	if err := f.Flush(ctx); err == nil {
		t.Fatal("flush error lost")
	}
	bad.fail = false
	if err := f.Flush(ctx); err != nil || bad.total != 1 {
		t.Fatalf("retry: %v, stored %v", err, bad.total)
	}
}

type failStore struct {
	fail  bool
	total float64
}

func (s *failStore) LoadSpend(context.Context, string) (float64, error) {
	if s.fail {
		return 0, errors.New("down")
	}
	return s.total, nil
}

func (s *failStore) AddSpend(_ context.Context, _ string, usd float64) (float64, error) {
	if s.fail {
		return 0, errors.New("down")
	}
	s.total += usd
	return s.total, nil
}

// gatedStore is a MemStore whose calls park until released: AddSpend
// commits when addRelease closes; LoadSpend reads the total on entry and
// returns it when loadRelease closes.
type gatedStore struct {
	MemStore
	addEntered, loadEntered chan struct{}
	addRelease, loadRelease chan struct{}
	addOnce, loadOnce       sync.Once
}

func (s *gatedStore) AddSpend(ctx context.Context, day string, usd float64) (float64, error) {
	s.addOnce.Do(func() { close(s.addEntered) })
	<-s.addRelease
	return s.MemStore.AddSpend(ctx, day, usd)
}

func (s *gatedStore) LoadSpend(ctx context.Context, day string) (float64, error) {
	v, err := s.MemStore.LoadSpend(ctx, day)
	s.loadOnce.Do(func() { close(s.loadEntered) })
	<-s.loadRelease
	return v, err
}

// TestAccountFlushOverlap: a Flush that overlaps another cannot overwrite
// today's spend with a total read before the other's AddSpend landed.
func TestAccountFlushOverlap(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	store := &gatedStore{MemStore: MemStore{m: map[string]float64{"2026-10-02": 1}},
		addEntered: make(chan struct{}), loadEntered: make(chan struct{}),
		addRelease: make(chan struct{}), loadRelease: make(chan struct{})}
	a := &Account{cfg: AccountConfig{Store: store, Now: clk.Now}, flushing: make(chan struct{}, 1),
		unflushed: map[string]float64{}, day: "2026-10-02", daySpent: 1}
	a.Add(5)
	errA, errB := make(chan error, 1), make(chan error, 1)
	go func() { errA <- a.Flush(ctx) }()
	<-store.addEntered // A holds its pending spend, the store has not added it
	go func() { errB <- a.Flush(ctx) }()
	select { // unserialized, B reads the stale total now
	case <-store.loadEntered:
	case <-time.After(100 * time.Millisecond):
	}
	close(store.addRelease)
	if err := <-errA; err != nil {
		t.Fatal(err)
	}
	close(store.loadRelease)
	if err := <-errB; err != nil {
		t.Fatal(err)
	}
	if s, _ := a.Daily(); s != 6 {
		t.Fatalf("daily spend %v after overlapping flushes, want the store's 6", s)
	}

	// a Flush waiting behind a stalled one gives up with its context and
	// keeps its spend for the next
	stall := &gatedStore{MemStore: MemStore{m: map[string]float64{}},
		addEntered: make(chan struct{}), loadEntered: make(chan struct{}),
		addRelease: make(chan struct{}), loadRelease: make(chan struct{})}
	b := &Account{cfg: AccountConfig{Store: stall, Now: clk.Now}, flushing: make(chan struct{}, 1),
		unflushed: map[string]float64{}, day: "2026-10-02"}
	b.Add(1)
	done := make(chan error, 1)
	go func() { done <- b.Flush(ctx) }()
	<-stall.addEntered
	b.Add(2)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if err := b.Flush(cctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting flush: %v", err)
	}
	close(stall.addRelease)
	close(stall.loadRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if v := stall.m["2026-10-02"]; v != 3 {
		t.Fatalf("stored %v, want 3", v)
	}
}
