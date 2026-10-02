package budget

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// SpendStore persists the account's daily spend (the API server stores it
// in its database). Days are UTC dates "2006-01-02". Implementations must
// be safe for concurrent use.
type SpendStore interface {
	// LoadSpend returns the USD spent on day so far.
	LoadSpend(ctx context.Context, day string) (float64, error)
	// AddSpend adds usd to day's spend and returns the new total.
	AddSpend(ctx context.Context, day string, usd float64) (float64, error)
}

// MemStore is an in-memory SpendStore (a process-local daily budget).
type MemStore struct {
	mu sync.Mutex
	m  map[string]float64
}

// NewMemStore returns an empty store.
func NewMemStore() *MemStore { return &MemStore{m: map[string]float64{}} }

// LoadSpend implements SpendStore.
func (s *MemStore) LoadSpend(_ context.Context, day string) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[day], nil
}

// AddSpend implements SpendStore.
func (s *MemStore) AddSpend(_ context.Context, day string, usd float64) (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[day] += usd
	return s.m[day], nil
}

// AccountConfig configures an Account. Zero values disable the
// corresponding limit.
type AccountConfig struct {
	// QPS and Burst bound the requests per second of every bot together
	// (the API account's rate limit; Burst 0: max(1, ceil(QPS))).
	QPS   float64
	Burst int
	// TokensPerSec bounds the input tokens per second of every bot
	// together (burst: one second's worth).
	TokensPerSec float64
	// DailyUSD caps the account's spend per UTC day; Store persists it
	// (nil: a MemStore).
	DailyUSD float64
	Store    SpendStore
	// Now is the wall clock (nil: time.Now).
	Now func() time.Time
}

// AccountStats are the account limiter's counters.
type AccountStats struct {
	Allowed, Refused int64
}

// Account is the process-wide budget every bot shares: a wall-clock
// request and token rate limit (Allow implements the jev client's Limiter
// interface) and a daily spend cap whose spend is persisted through a
// SpendStore (Flush). It is safe for concurrent use.
type Account struct {
	cfg AccountConfig

	mu        sync.Mutex
	reqTokens float64
	tokTokens float64
	last      time.Time
	started   bool
	stats     AccountStats
	day       string
	daySpent  float64            // the store's total for day plus the unflushed spend
	unflushed map[string]float64 // spend not yet added to the store, by day
}

// NewAccount returns an account, loading today's spend from the store.
func NewAccount(ctx context.Context, cfg AccountConfig) (*Account, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Store == nil {
		cfg.Store = NewMemStore()
	}
	if cfg.QPS > 0 && cfg.Burst <= 0 {
		cfg.Burst = max(1, int(math.Ceil(cfg.QPS)))
	}
	a := &Account{cfg: cfg, unflushed: map[string]float64{}}
	a.day = dayOf(cfg.Now())
	spent, err := cfg.Store.LoadSpend(ctx, a.day)
	if err != nil {
		return nil, err
	}
	a.daySpent = spent
	return a, nil
}

func dayOf(t time.Time) string { return t.UTC().Format("2006-01-02") }

// Allow implements the jev Limiter: it takes a permit for one request of
// about tokens input tokens and reports whether it may be sent now.
func (a *Account) Allow(tokens int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.cfg.Now()
	if !a.started {
		a.started, a.last = true, now
		a.reqTokens, a.tokTokens = float64(a.cfg.Burst), a.cfg.TokensPerSec
	}
	if dt := now.Sub(a.last).Seconds(); dt > 0 {
		a.reqTokens = math.Min(float64(a.cfg.Burst), a.reqTokens+dt*a.cfg.QPS)
		a.tokTokens = math.Min(a.cfg.TokensPerSec, a.tokTokens+dt*a.cfg.TokensPerSec)
		a.last = now
	}
	t := float64(max(tokens, 0))
	switch {
	case a.cfg.QPS > 0 && a.reqTokens < 1:
	case a.cfg.TokensPerSec > 0 && a.tokTokens < math.Min(t, a.cfg.TokensPerSec):
	default:
		if a.cfg.QPS > 0 {
			a.reqTokens--
		}
		if a.cfg.TokensPerSec > 0 {
			a.tokTokens -= t
		}
		a.stats.Allowed++
		return true
	}
	a.stats.Refused++
	return false
}

// rollover starts a new day's spend when the clock passed midnight UTC.
func (a *Account) rollover() {
	if d := dayOf(a.cfg.Now()); d != a.day {
		a.day, a.daySpent = d, a.unflushed[d]
	}
}

// Add accounts spend (it is persisted by the next Flush).
func (a *Account) Add(usd float64) {
	if usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollover()
	a.unflushed[a.day] += usd
	a.daySpent += usd
}

// Daily returns today's spend (the store's total when last read plus the
// spend since) and the daily cap (0: none).
func (a *Account) Daily() (spent, limit float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rollover()
	return a.daySpent, a.cfg.DailyUSD
}

// Flush adds the unflushed spend to the store and refreshes today's total
// from it (which includes other processes' spend).
func (a *Account) Flush(ctx context.Context) error {
	a.mu.Lock()
	pending := a.unflushed
	a.unflushed = map[string]float64{}
	a.rollover()
	today := a.day
	a.mu.Unlock()

	var errs []error
	totals := map[string]float64{}
	for day, usd := range pending {
		total, err := a.cfg.Store.AddSpend(ctx, day, usd)
		if err != nil {
			errs = append(errs, err)
			a.mu.Lock()
			a.unflushed[day] += usd // try again next time
			a.mu.Unlock()
			continue
		}
		totals[day] = total
	}
	total, ok := totals[today]
	if !ok {
		var err error
		if total, err = a.cfg.Store.LoadSpend(ctx, today); err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	a.mu.Lock()
	if a.day == today {
		a.daySpent = total + a.unflushed[today]
	}
	a.mu.Unlock()
	return errors.Join(errs...)
}

// Stats returns the limiter's counters.
func (a *Account) Stats() AccountStats {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.stats
}
