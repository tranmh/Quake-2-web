package budget

import (
	"context"
	"fmt"
	"math"
	"sync"

	"quake2web/server/internal/agent/decide"
)

// planKeep is how long (ms of session time) a tick's plan is kept for
// the backend calls submitted at that tick (they start on their own
// goroutines right after the submit; a lockstep pipeline cannot run
// further ahead than its longest pending call, a few seconds).
const planKeep = 30000

// frameSlack tolerates a submit up to this much (ms) early against the
// capped interval (requests are asked on 100 ms frames).
const frameSlack = 50

// plan is the admission of the requests one tick submits.
type plan struct {
	allow [decide.NumLanes]bool
	why   [decide.NumLanes]string
}

// Gate applies a Budget to one pipeline (one episode): it plans which
// requests of each tick may be sent (Plan, on the control loop before the
// pipeline's Tick), refuses the others in the wrapped backend (Wrap), and
// charges the collected results (Charge). Plan, Submitted and Charge
// must be called from the control loop's goroutine; the wrapped backend
// is safe for concurrent use.
type Gate struct {
	b *Budget

	mu        sync.Mutex
	plans     map[int64]plan
	cur       State
	lastFast  int64
	fastSent  bool
	sent      []int64         // submit times of the requests let through (MaxQPS window)
	forwarded map[uint64]bool // requests the wrapped backend sent
	refused   [decide.NumLanes]int
	planned   bool
}

// Gate returns a new gate on b for one pipeline.
func (b *Budget) Gate() *Gate {
	return &Gate{b: b, plans: map[int64]plan{}, forwarded: map[uint64]bool{}}
}

// Budget returns the gate's budget.
func (g *Gate) Budget() *Budget { return g.b }

// Plan decides, at session time now and before the pipeline's tick, which
// lanes' requests of that tick may be sent: none when scripted only,
// the fast lane at most at the state's FastHz, both within MaxQPS. It
// returns the budget's state.
func (g *Gate) Plan(now int64) State {
	st := g.b.State()
	l := g.b.limits
	var p plan
	switch {
	case st.ScriptedOnly:
		p.why = [decide.NumLanes]string{st.Reason, st.Reason}
	default:
		p.allow = [decide.NumLanes]bool{true, true}
		if st.FastHz < l.FullHz && g.fastSent {
			if iv := int64(math.Round(1000 / st.FastHz)); now-g.lastFast < iv-frameSlack {
				p.allow[decide.LaneFast] = false
				p.why[decide.LaneFast] = fmt.Sprintf("fast lane capped at %g Hz (%s)", st.FastHz, st.Reason)
			}
		}
		if l.MaxQPS > 0 {
			g.trim(now)
			n := float64(len(g.sent))
			if p.allow[decide.LaneFast] && n+1 > l.MaxQPS+1e-9 {
				p.allow[decide.LaneFast] = false
				p.why[decide.LaneFast] = fmt.Sprintf("max %g queries per second", l.MaxQPS)
			}
			if p.allow[decide.LaneFast] {
				n++ // the fast lane goes first
			}
			if n+1 > l.MaxQPS+1e-9 {
				p.allow[decide.LaneSlow] = false
				p.why[decide.LaneSlow] = fmt.Sprintf("max %g queries per second", l.MaxQPS)
			}
		}
	}
	g.mu.Lock()
	g.plans[now] = p
	for t := range g.plans {
		if t < now-planKeep {
			delete(g.plans, t)
		}
	}
	g.cur, g.planned = st, true
	g.mu.Unlock()
	return st
}

// trim drops the MaxQPS window's entries older than a second before now.
func (g *Gate) trim(now int64) {
	i := 0
	for i < len(g.sent) && g.sent[i] <= now-1000 {
		i++
	}
	g.sent = append(g.sent[:0], g.sent[i:]...)
}

// Submitted notes, after the pipeline's tick at now, that the scheduler
// accepted a request of lane l (the rate caps count the requests let
// through, from their submit time).
func (g *Gate) Submitted(now int64, l decide.Lane) {
	g.mu.Lock()
	p, ok := g.plans[now]
	g.mu.Unlock()
	if !ok || !p.allow[l] {
		return
	}
	if l == decide.LaneFast {
		g.lastFast, g.fastSent = now, true
	}
	g.sent = append(g.sent, now)
}

// Charge accounts a collected result: a request the wrapped backend sent
// counts as a query, with its response's cost and input tokens (a refused
// one costs nothing).
func (g *Gate) Charge(r *decide.Record) {
	req := r.Result.Req
	if req == nil {
		return
	}
	g.mu.Lock()
	sent := g.forwarded[req.Seq]
	delete(g.forwarded, req.Seq)
	g.mu.Unlock()
	if !sent {
		return
	}
	var cost float64
	var tokens int64
	if resp := r.Result.Resp; resp != nil {
		cost, tokens = resp.CostUSD, resp.Usage.InputTokens
	}
	g.b.Charge(cost, tokens)
}

// Refused returns how many requests of each lane the gate refused.
func (g *Gate) Refused() [decide.NumLanes]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.refused
}

// admit decides a request in the wrapped backend.
func (g *Gate) admit(req *decide.Request) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	p, ok := g.plans[req.SnapTime]
	if !ok {
		// a tick that was not planned (a caller that skipped Plan): admit
		// by the last state
		if g.planned && g.cur.ScriptedOnly {
			g.refused[req.Lane]++
			return &RefusedError{Reason: g.cur.Reason}
		}
		g.forwarded[req.Seq] = true
		return nil
	}
	if !p.allow[req.Lane] {
		g.refused[req.Lane]++
		return &RefusedError{Reason: p.why[req.Lane]}
	}
	g.forwarded[req.Seq] = true
	return nil
}

// Wrap returns inner behind the gate: a request the plan refuses fails
// with a RefusedError without reaching inner. The wrapper keeps inner's
// name.
func (g *Gate) Wrap(inner decide.DecisionBackend) decide.DecisionBackend {
	return &gated{g: g, inner: inner}
}

type gated struct {
	g     *Gate
	inner decide.DecisionBackend
}

func (w *gated) Name() string { return w.inner.Name() }

func (w *gated) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if err := w.g.admit(req); err != nil {
		return nil, err
	}
	return w.inner.Decide(ctx, req)
}
