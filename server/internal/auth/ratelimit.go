package auth

import (
	"sync"
	"time"
)

// Limiter is a sliding-window rate limiter keyed by string (login attempts
// per IP and per email).
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
	sweeps int
}

// NewLimiter allows max events per window per key.
func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{max: max, window: window, now: time.Now, hits: map[string][]time.Time{}}
}

// SetClock replaces the time source (tests).
func (l *Limiter) SetClock(now func() time.Time) { l.mu.Lock(); l.now = now; l.mu.Unlock() }

// Allow records an event for key and reports whether it is within the limit.
// When denied it also returns how long until the next event is allowed.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cut := now.Add(-l.window)
	h := l.hits[key]
	i := 0
	for i < len(h) && !h[i].After(cut) {
		i++
	}
	h = h[i:]
	if len(h) >= l.max {
		l.hits[key] = h
		return false, h[0].Add(l.window).Sub(now)
	}
	l.hits[key] = append(h, now)
	l.sweeps++
	if l.sweeps >= 1024 {
		l.sweeps = 0
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cut) {
				delete(l.hits, k)
			}
		}
	}
	return true, 0
}

// Reset forgets a key (e.g. after a successful login).
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.hits, key)
	l.mu.Unlock()
}
