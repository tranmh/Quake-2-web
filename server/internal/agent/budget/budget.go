// Package budget bounds what a bot run spends on its decision backend:
// a per-run cap in USD and in queries, a per-run query rate (MaxQPS), and a
// process-wide Account shared by every bot (the API account's rate limit
// and a daily USD cap whose spend a SpendStore persists).
//
// As the tightest budget drains, the fast lane's request rate degrades
// along a ladder (DefaultLadder: the full 10 Hz, 5 Hz below half the budget,
// 2 Hz below a quarter) and an exhausted budget leaves the bot on its
// scripted fallback only. The decide scheduler has no rate hook, so the
// degradation is applied by a backend wrapper (Gate.Wrap) that refuses the
// requests beyond the allowed rate with a RefusedError; the arbiter then
// keeps the last model answer for its TTL and falls back to the scripted
// policy, exactly as for any failed request.
//
// Determinism: a lockstep run with a budget repeats exactly. Which
// requests are refused is planned on the control loop's goroutine before
// each tick (Gate.Plan) from the session clock and the spend of the
// results collected so far (Gate.Charge), never from the order in which
// concurrent backend calls finish. Only the Account (the API account's
// wall-clock limiter and the daily spend shared with other processes) is
// outside that guarantee.
package budget

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
)

// Policy is what a run does once its budget is exhausted.
type Policy string

// Exhaustion policies.
const (
	// Fallback keeps playing on the scripted fallback only (the run is
	// flagged as not model-driven).
	Fallback Policy = "fallback"
	// Stop ends the run.
	Stop Policy = "stop"
)

// ParsePolicy parses "fallback" or "stop" ("" is Fallback).
func ParsePolicy(s string) (Policy, error) {
	switch Policy(s) {
	case "", Fallback:
		return Fallback, nil
	case Stop:
		return Stop, nil
	}
	return "", fmt.Errorf("budget: unknown exhaustion policy %q (fallback|stop)", s)
}

// DefaultFullHz is the fast lane's undegraded rate (decide.DefaultFastHz).
const DefaultFullHz = 10

// Step is a rung of the degradation ladder: once the remaining share of
// the budget is at or below Remaining, the fast lane is capped at FastHz
// (0: no requests at all, scripted only).
type Step struct {
	Remaining float64
	FastHz    float64
}

// DefaultLadder is the plan's degradation 10 → 5 → 2 Hz → scripted only:
// the full rate down to half the budget, 5 Hz down to a quarter, then
// 2 Hz until the budget is exhausted.
func DefaultLadder() []Step { return []Step{{Remaining: 0.5, FastHz: 5}, {Remaining: 0.25, FastHz: 2}} }

// Limits are one run's budget. The zero value has no limits.
type Limits struct {
	// USD caps the run's spend (0: no cap).
	USD float64
	// Queries caps the requests sent to the backend (0: no cap).
	Queries int
	// MaxQPS caps the requests sent per second of session time, both
	// lanes together (0: no cap).
	MaxQPS float64
	// Ladder is the degradation (nil: DefaultLadder()).
	Ladder []Step
	// OnExhausted is what happens at the end of the budget ("":
	// Fallback).
	OnExhausted Policy
	// FullHz is the fast lane's undegraded rate (0: DefaultFullHz).
	FullHz float64
}

// Enabled reports whether any limit is set.
func (l Limits) Enabled() bool { return l.USD > 0 || l.Queries > 0 || l.MaxQPS > 0 }

func (l Limits) withDefaults() Limits {
	if l.Ladder == nil {
		l.Ladder = DefaultLadder()
	}
	l.Ladder = append([]Step(nil), l.Ladder...)
	sort.SliceStable(l.Ladder, func(i, j int) bool { return l.Ladder[i].Remaining > l.Ladder[j].Remaining })
	if l.OnExhausted == "" {
		l.OnExhausted = Fallback
	}
	if l.FullHz <= 0 {
		l.FullHz = DefaultFullHz
	}
	return l
}

// State is a budget's state at one moment.
type State struct {
	SpentUSD   float64
	LimitUSD   float64 // 0: none
	Queries    int
	QueryLimit int // 0: none
	// DailySpentUSD and DailyLimitUSD are the account's (0: none).
	DailySpentUSD, DailyLimitUSD float64
	// Remaining is the remaining share (0..1) of the tightest limit
	// (1 without limits).
	Remaining float64
	// FastHz is the fast lane's allowed rate (0 with ScriptedOnly).
	FastHz float64
	// ScriptedOnly: no request may be sent; Exhausted: because the budget
	// is spent (rather than a ladder step at 0 Hz).
	ScriptedOnly, Exhausted bool
	// Reason describes the state ("" while the budget is untouched).
	Reason string
}

// Level returns what changes the bot's behaviour: the allowed fast rate
// and whether it is scripted only (budget events are published when it
// changes).
func (s State) Level() (fastHz float64, scriptedOnly bool) { return s.FastHz, s.ScriptedOnly }

// ErrRefused is wrapped by every RefusedError.
var ErrRefused = errors.New("budget: request refused")

// RefusedError is a request the budget did not let through: the backend
// was not called.
type RefusedError struct {
	Reason string
}

func (e *RefusedError) Error() string { return "budget: " + e.Reason }

// Unwrap makes errors.Is(err, ErrRefused) hold.
func (e *RefusedError) Unwrap() error { return ErrRefused }

// IsRefused reports whether err is a budget refusal.
func IsRefused(err error) bool { return errors.Is(err, ErrRefused) }

// Budget is one run's ledger: the spend and the queries of the results
// collected so far, against the Limits (and the Account's daily cap). It
// is shared by the run's episodes and safe for concurrent use.
type Budget struct {
	limits Limits
	acct   *Account

	mu      sync.Mutex
	spent   float64
	queries int
	tokens  int64
}

// New returns a budget for one run; acct (nil: none) is the process-wide
// account it also charges.
func New(l Limits, acct *Account) *Budget {
	return &Budget{limits: l.withDefaults(), acct: acct}
}

// Limits returns the budget's limits (with defaults).
func (b *Budget) Limits() Limits { return b.limits }

// Charge accounts one query sent to the backend and its cost.
func (b *Budget) Charge(costUSD float64, tokens int64) {
	if costUSD < 0 || math.IsNaN(costUSD) || math.IsInf(costUSD, 0) {
		costUSD = 0
	}
	b.mu.Lock()
	b.spent += costUSD
	b.queries++
	b.tokens += tokens
	b.mu.Unlock()
	if b.acct != nil && costUSD > 0 {
		b.acct.Add(costUSD)
	}
}

// State returns the budget's state now.
func (b *Budget) State() State {
	b.mu.Lock()
	st := State{SpentUSD: b.spent, LimitUSD: b.limits.USD, Queries: b.queries, QueryLimit: b.limits.Queries}
	b.mu.Unlock()
	if b.acct != nil {
		st.DailySpentUSD, st.DailyLimitUSD = b.acct.Daily()
	}
	l := b.limits
	st.Remaining = 1
	why := ""
	share := func(used, limit float64, what string) {
		if limit <= 0 {
			return
		}
		r := 1 - used/limit
		if r < st.Remaining {
			st.Remaining, why = r, what
		}
	}
	share(st.SpentUSD, st.LimitUSD, fmt.Sprintf("$%.6f of $%.6f spent", st.SpentUSD, st.LimitUSD))
	share(float64(st.Queries), float64(st.QueryLimit), fmt.Sprintf("%d of %d queries", st.Queries, st.QueryLimit))
	share(st.DailySpentUSD, st.DailyLimitUSD, fmt.Sprintf("$%.6f of the daily $%.6f spent", st.DailySpentUSD, st.DailyLimitUSD))
	st.Remaining = math.Max(0, math.Min(1, st.Remaining))
	st.FastHz = l.FullHz
	switch {
	case st.Remaining <= 0:
		st.FastHz, st.ScriptedOnly, st.Exhausted = 0, true, true
		st.Reason = "budget exhausted (" + why + "): scripted only"
	default:
		for _, s := range l.Ladder {
			if st.Remaining <= s.Remaining && s.FastHz < st.FastHz {
				st.FastHz = s.FastHz
			}
		}
		switch {
		case st.FastHz <= 0:
			st.FastHz, st.ScriptedOnly = 0, true
			st.Reason = why + ": scripted only"
		case st.FastHz < l.FullHz:
			st.Reason = fmt.Sprintf("%s: fast lane at %g Hz", why, st.FastHz)
		}
	}
	return st
}
