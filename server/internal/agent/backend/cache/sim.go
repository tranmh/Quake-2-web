package cache

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// The offline simulator (q2bot cache-sim): a recorded run's requests go
// through a Store under the live cache's rules, without any backend, and
// every hit's reused answer is compared with the answer the backend
// actually gave to that request.
//
// The rules are the live ones: a request looks up when it was submitted
// (its snap_gms) and a miss's answer is kept when it was collected (its
// decision event's gms); within a tick the lookups come first (a lockstep
// pipeline submits, then collects), so an answer is reusable from the
// tick after it was collected. Only answers without an error that answer
// every question are kept, never a reused one; the TTL, the entry limit
// and the keys are the Store's and Config's own. A request's map is that
// of the last level_start at or before its submission.
//
// It is an estimate: with a live cache a hit is answered at once instead
// of a latency later, which changes what the bot does and so which
// requests come after; the recording has the requests of a run without
// the cache.

// SimRequest is a recorded request decision.
type SimRequest struct {
	Ep      int
	Lane    decide.Lane
	Seq     uint64 // the request's
	EvSeq   uint64 // the decision event's
	Snap    int64  // submitted (snap_gms)
	Arrived int64  // collected (the event's gms)
	Map     string
	Digest  string
	State   *decide.State // nil when not recorded
	Qs      []decide.Question
	// Resp is the recorded answer (nil: none: an error, or not decodable);
	// Err the recorded error; Refused a budget refusal (it never reached
	// the backend); Cache the recorded marker of a reused answer.
	Resp    *decide.Response
	Err     string
	Refused bool
	Cache   string
}

// SimRequests extracts the request decisions of a run's events (the
// decision events of lanes fast and slow), in event order, with the map
// each was asked on.
func SimRequests(events []trace.Event) ([]SimRequest, error) {
	type lvl struct {
		gms int64
		m   string
	}
	levels := map[int][]lvl{}
	var out []SimRequest
	for i := range events {
		e := &events[i]
		switch e.Type {
		case trace.TypeLevelStart:
			levels[e.Ep] = append(levels[e.Ep], lvl{e.GMs, e.Map})
			continue
		case trace.TypeDecision:
		default:
			continue
		}
		var d trace.Decision
		if err := e.DecodeBody(&d); err != nil {
			return nil, fmt.Errorf("cache: event %d: %w", e.Seq, err)
		}
		var lane decide.Lane
		if d.Lane != "fast" && d.Lane != "slow" || lane.UnmarshalText([]byte(d.Lane)) != nil {
			continue
		}
		r := SimRequest{Ep: e.Ep, Lane: lane, Seq: d.Req, EvSeq: e.Seq, Snap: d.SnapGMs, Arrived: e.GMs, Digest: d.ReqDigest,
			Err: d.Err, Refused: strings.HasPrefix(d.Err, "budget: "), Cache: d.Cache}
		for _, l := range levels[e.Ep] {
			if l.gms <= d.SnapGMs {
				r.Map = l.m
			}
		}
		if len(d.Questions) > 0 {
			qs, err := decide.ParseQuestions(d.Questions)
			if err != nil {
				return nil, fmt.Errorf("cache: event %d questions: %w", e.Seq, err)
			}
			r.Qs = qs
		}
		if len(d.State) > 0 {
			var st decide.State
			if err := json.Unmarshal(d.State, &st); err != nil {
				return nil, fmt.Errorf("cache: event %d state: %w", e.Seq, err)
			}
			r.State = &st
		}
		if r.Digest == "" && d.State != nil && d.Questions != nil {
			r.Digest = decide.RequestDigest(d.State, d.Questions)
		}
		if d.Err == "" && len(d.Response) > 0 && r.Qs != nil {
			if resp, err := decide.DecodeResponse(d.Response, r.Qs); err == nil {
				resp.Usage.InputTokens, resp.CostUSD = d.InputTokens, d.CostUSD
				r.Resp = resp
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// SimLane counts one lane's simulated lookups: Requests the requests that
// reached the cache (budget refusals excluded), InFlight the misses whose
// key an earlier request was still waiting for, SavedTokens and SavedUSD
// what the hits' answers had cost.
type SimLane struct {
	Requests    int     `json:"requests"`
	Hits        int     `json:"hits"`
	Misses      int     `json:"misses"`
	HitRate     float64 `json:"hit_rate"`
	InFlight    int     `json:"in_flight"`
	SavedTokens int64   `json:"saved_input_tokens"`
	SavedUSD    float64 `json:"saved_usd"`
	// RecordedHits counts the requests the recording marks as reused and
	// RecordedAgree those the simulation found too (a run recorded with
	// the same cache must agree on every one).
	RecordedHits  int `json:"recorded_hits,omitempty"`
	RecordedAgree int `json:"recorded_agree,omitempty"`
}

// Agreement counts compared answers: N compared, Equal the same.
type Agreement struct {
	N     int     `json:"n"`
	Equal int     `json:"equal"`
	Rate  float64 `json:"rate"`
}

func (a *Agreement) add(eq bool) {
	a.N++
	if eq {
		a.Equal++
	}
}

// SimResult is a simulation of one cache configuration.
type SimResult struct {
	Mode  string             `json:"mode"`
	TTL   string             `json:"ttl"`
	Lanes map[string]SimLane `json:"lanes"`
	Total SimLane            `json:"total"`
	// Fidelity compares, on every hit whose request has a recorded answer,
	// the reused answer with the recorded one, by question id (choice: the
	// same option; score: the same level, rounded); AllEqual compares the
	// whole answer (every question the same).
	Fidelity map[string]Agreement `json:"fidelity"`
	AllEqual Agreement            `json:"all_equal"`
	// Ages are the hits' ages (ms from the reused answer's request to the
	// hit's, same episode); CrossEpisode counts hits on another episode's
	// answer.
	Ages         []int64 `json:"-"`
	AgeQuantiles []int64 `json:"age_ms_p10_p50_p90_max,omitempty"`
	CrossEpisode int     `json:"cross_episode,omitempty"`
	// NoKey counts requests the mode could not key (approx without a
	// recorded state), counted as misses.
	NoKey int `json:"no_key,omitempty"`
}

// Same reports whether two answers to question q agree: the same option
// (choice), the same level rounded (score), the same side (noul).
func Same(q *decide.Question, a, b decide.Answer) bool {
	switch q.Type {
	case decide.Choice:
		return a.Choice == b.Choice
	case decide.Score:
		return math.Round(a.Score) == math.Round(b.Score)
	case decide.Noul:
		return math.Round(a.Noul) == math.Round(b.Noul)
	}
	return false
}

// Simulate runs reqs (SimRequests of one run) through a new Store of cfg.
func Simulate(reqs []SimRequest, cfg Config) *SimResult {
	res := &SimResult{Mode: cfg.String(), TTL: "none", Lanes: map[string]SimLane{}, Fidelity: map[string]Agreement{}}
	if cfg.TTL > 0 {
		res.TTL = cfg.TTL.String()
	}
	type op struct {
		t     int64
		phase int // 0: lookup (submission), 1: answer (collection)
		order uint64
		i     int
	}
	// episodes in order of appearance, each on its own clock
	var eps []int
	byEp := map[int][]int{}
	for i := range reqs {
		if _, ok := byEp[reqs[i].Ep]; !ok {
			eps = append(eps, reqs[i].Ep)
		}
		byEp[reqs[i].Ep] = append(byEp[reqs[i].Ep], i)
	}
	store := NewStore(cfg)
	lanes := [decide.NumLanes]SimLane{}
	for _, ep := range eps {
		var ops []op
		for _, i := range byEp[ep] {
			ops = append(ops, op{reqs[i].Snap, 0, reqs[i].Seq, i}, op{reqs[i].Arrived, 1, reqs[i].EvSeq, i})
		}
		sort.SliceStable(ops, func(a, b int) bool {
			x, y := ops[a], ops[b]
			if x.t != y.t {
				return x.t < y.t
			}
			if x.phase != y.phase {
				return x.phase < y.phase
			}
			return x.order < y.order
		})
		keys := map[int]string{}
		hit := map[int]bool{}
		for _, o := range ops {
			r := &reqs[o.i]
			ln := &lanes[r.Lane]
			if o.phase == 1 {
				if hit[o.i] {
					continue
				}
				var e *Entry
				if r.Err == "" && r.Cache == "" && r.Resp != nil {
					e = EntryOf(r.Ep, r.Snap, r.Qs, r.Resp)
				}
				store.Answered(keys[o.i], e)
				continue
			}
			key := cfg.KeyOf(r.Lane, r.Map, r.State, r.Qs, r.Digest)
			e, inflight := store.Lookup(key, r.Ep, r.Snap)
			if r.Refused {
				// looked up at submission, refused by the budget after: no
				// answer either way
				if e == nil {
					store.Asked(key)
					keys[o.i] = key
				} else {
					hit[o.i] = true
				}
				continue
			}
			ln.Requests++
			if r.Cache != "" {
				ln.RecordedHits++
			}
			if e == nil {
				if key == "" {
					res.NoKey++
				}
				ln.Misses++
				if inflight {
					ln.InFlight++
				}
				store.Asked(key)
				keys[o.i] = key
				continue
			}
			hit[o.i] = true
			ln.Hits++
			if r.Cache != "" {
				ln.RecordedAgree++
			}
			ln.SavedTokens += e.InputTokens
			ln.SavedUSD += e.CostUSD
			if e.Ep == r.Ep {
				res.Ages = append(res.Ages, r.Snap-e.SnapTime)
			} else {
				res.CrossEpisode++
			}
			if r.Resp == nil || r.Cache != "" {
				continue // no answer of the backend's own to compare with
			}
			all, complete := true, true
			for qi := range r.Qs {
				q := &r.Qs[qi]
				a, okA := e.Answers[q.ID]
				b, okB := r.Resp.Answers[q.ID]
				if !okA || !okB {
					complete = false
					continue
				}
				eq := Same(q, a, b)
				ag := res.Fidelity[q.ID]
				ag.add(eq)
				res.Fidelity[q.ID] = ag
				all = all && eq
			}
			if complete {
				res.AllEqual.add(all)
			}
		}
	}
	for l := decide.Lane(0); l < decide.NumLanes; l++ {
		ln := lanes[l]
		ln.HitRate = rate(ln.Hits, ln.Requests)
		res.Lanes[l.String()] = ln
		res.Total.add(ln)
	}
	res.Total.HitRate = rate(res.Total.Hits, res.Total.Requests)
	res.finish()
	return res
}

func (a *SimLane) add(b SimLane) {
	a.Requests += b.Requests
	a.Hits += b.Hits
	a.Misses += b.Misses
	a.InFlight += b.InFlight
	a.SavedTokens += b.SavedTokens
	a.SavedUSD += b.SavedUSD
	a.RecordedHits += b.RecordedHits
	a.RecordedAgree += b.RecordedAgree
}

// Merge adds o (another run under the same configuration) to r.
func (r *SimResult) Merge(o *SimResult) {
	for k, v := range o.Lanes {
		l := r.Lanes[k]
		l.add(v)
		l.HitRate = rate(l.Hits, l.Requests)
		r.Lanes[k] = l
	}
	r.Total.add(o.Total)
	r.Total.HitRate = rate(r.Total.Hits, r.Total.Requests)
	for k, v := range o.Fidelity {
		a := r.Fidelity[k]
		a.N += v.N
		a.Equal += v.Equal
		r.Fidelity[k] = a
	}
	r.AllEqual.N += o.AllEqual.N
	r.AllEqual.Equal += o.AllEqual.Equal
	r.Ages = append(r.Ages, o.Ages...)
	r.CrossEpisode += o.CrossEpisode
	r.NoKey += o.NoKey
	r.finish()
}

func (r *SimResult) finish() {
	for k, v := range r.Fidelity {
		v.Rate = rate(v.Equal, v.N)
		r.Fidelity[k] = v
	}
	r.AllEqual.Rate = rate(r.AllEqual.Equal, r.AllEqual.N)
	r.AgeQuantiles = nil
	if len(r.Ages) > 0 {
		s := append([]int64(nil), r.Ages...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		q := func(p float64) int64 {
			i := int(math.Ceil(p*float64(len(s)))) - 1
			return s[max(i, 0)]
		}
		r.AgeQuantiles = []int64{q(0.1), q(0.5), q(0.9), s[len(s)-1]}
	}
}

// AgeShares are the shares of the hits by age: under 0.5 s, 0.5-2 s, 2-10
// s, 10 s and older, from another episode.
func (r *SimResult) AgeShares() [5]float64 {
	var n [5]int
	for _, a := range r.Ages {
		switch {
		case a < 500:
			n[0]++
		case a < 2000:
			n[1]++
		case a < 10000:
			n[2]++
		default:
			n[3]++
		}
	}
	n[4] = r.CrossEpisode
	total := len(r.Ages) + r.CrossEpisode
	var out [5]float64
	for i := range n {
		out[i] = rate(n[i], total)
	}
	return out
}

// ExactRepeats counts the requests (budget refusals excluded) whose
// digest an earlier request of the run already had, in event order and
// with no causal rule: an upper bound of exact hits.
func ExactRepeats(reqs []SimRequest) int {
	seen := map[string]bool{}
	n := 0
	for i := range reqs {
		r := &reqs[i]
		if r.Refused || r.Digest == "" {
			continue
		}
		if seen[r.Lane.String()+r.Digest] {
			n++
		}
		seen[r.Lane.String()+r.Digest] = true
	}
	return n
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
