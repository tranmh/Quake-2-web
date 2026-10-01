package jev

import (
	"math"
	"sync"
	"time"
)

// Limiter is a rate limit shared by clients (a per-account limiter in the
// bot manager, say). Allow takes a permit for one request of about tokens
// input tokens and reports whether it may be sent now; it must not block
// and must be safe for concurrent use.
type Limiter interface {
	Allow(tokens int) bool
}

// TokenBucket limits requests per second with a burst. It is a Limiter
// (the tokens argument is ignored: it counts requests).
type TokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
	now    func() time.Time
}

// NewTokenBucket returns a bucket of rate requests per second holding up
// to burst (at least 1); now is the clock (nil: time.Now).
func NewTokenBucket(rate float64, burst int, now func() time.Time) *TokenBucket {
	if now == nil {
		now = time.Now
	}
	b := float64(max(burst, 1))
	return &TokenBucket{rate: rate, burst: b, tokens: b, now: now}
}

// Allow implements Limiter.
func (b *TokenBucket) Allow(int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	if !b.last.IsZero() {
		b.tokens = math.Min(b.burst, b.tokens+t.Sub(b.last).Seconds()*b.rate)
	}
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
