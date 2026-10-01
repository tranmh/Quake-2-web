package decide

import (
	"context"
	"math"
	"strconv"
	"time"

	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

// Source is where a decided value came from.
type Source uint8

// Sources.
const (
	SourceDefault  Source = iota // nothing usable: the field's default
	SourceModel                  // the backend's answer
	SourceScripted               // the scripted policy (fallback, or the scripted backend)
	SourceStale                  // a model answer kept past its TTL
	numSources
)

// String returns the trace name of the source (trace.Source*).
func (s Source) String() string {
	switch s {
	case SourceModel:
		return trace.SourceModel
	case SourceScripted:
		return trace.SourceScripted
	case SourceStale:
		return trace.SourceStale
	}
	return trace.SourceDefault
}

// MarshalText encodes the source by name.
func (s Source) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Field is a decision field: one question's outcome.
type Field uint8

// Fields.
const (
	FieldMode Field = iota
	FieldTarget
	FieldFirePolicy
	FieldMovement
	FieldWeapon
	FieldPickup
	FieldDanger
	NumFields
)

// ID returns the field's question id.
func (f Field) ID() string {
	switch f {
	case FieldMode:
		return QMode
	case FieldTarget:
		return QTarget
	case FieldFirePolicy:
		return QFirePolicy
	case FieldMovement:
		return QMovement
	case FieldWeapon:
		return QWeapon
	case FieldPickup:
		return QPickup
	case FieldDanger:
		return QDanger
	}
	return "field" + strconv.Itoa(int(f))
}

// Lane returns the lane that asks the field's question.
func (f Field) Lane() Lane {
	switch f {
	case FieldTarget, FieldFirePolicy, FieldMovement:
		return LaneFast
	}
	return LaneSlow
}

// FieldOf returns the field of a question id.
func FieldOf(id string) (Field, bool) {
	for f := Field(0); f < NumFields; f++ {
		if f.ID() == id {
			return f, true
		}
	}
	return 0, false
}

// defaultValue is a field's value when nothing is known (an option key).
func defaultValue(f Field) string {
	switch f {
	case FieldMode:
		return string(ModeExplore)
	case FieldTarget, FieldPickup:
		return OptNone
	case FieldFirePolicy:
		return string(FireHold)
	case FieldMovement:
		return string(MoveHold)
	case FieldWeapon:
		return OptKeep
	}
	return levelKey(DangerSafe)
}

// FieldProvenance is where one field of an Intent came from.
type FieldProvenance struct {
	Source Source
	// Reason says why the model's answer is not used ("" when it is):
	// no_answer, not_asked, missing, invalid, unknown_option,
	// low_confidence, ttl, error, timeout, gone (the target or item is no
	// longer there) or held (hysteresis kept the previous value).
	Reason     string
	Confidence float64
	Seq        uint64 // the request it answered (0: none)
}

// Provenance is the provenance of every field of an Intent.
type Provenance struct {
	Mode, Target, FirePolicy, Movement, Weapon, Pickup, Danger FieldProvenance
}

// Get returns a field's provenance.
func (p *Provenance) Get(f Field) FieldProvenance { return *p.ptr(f) }

func (p *Provenance) ptr(f Field) *FieldProvenance {
	switch f {
	case FieldMode:
		return &p.Mode
	case FieldTarget:
		return &p.Target
	case FieldFirePolicy:
		return &p.FirePolicy
	case FieldMovement:
		return &p.Movement
	case FieldWeapon:
		return &p.Weapon
	case FieldPickup:
		return &p.Pickup
	}
	return &p.Danger
}

// Intent is the decision layer's output for the controller: the policies
// it executes (with its reflexes) until the next Intent.
type Intent struct {
	Time       int64 // session ms it was made at
	Mode       Mode
	Target     string // track id ("" none)
	FirePolicy FirePolicy
	Movement   Movement
	Weapon     WeaponKey // "" keeps the current weapon
	Pickup     string    // item id ("" none)
	// Danger is 0 (safe) … 4 (critical), continuous.
	Danger     float64
	Provenance Provenance
}

// DefaultIntent is the Intent before any decision.
func DefaultIntent() Intent {
	return Intent{Mode: ModeExplore, FirePolicy: FireHold, Movement: MoveHold}
}

// Value returns a field's value as an option key (danger: the level with
// two decimals).
func (in *Intent) Value(f Field) string {
	switch f {
	case FieldMode:
		return string(in.Mode)
	case FieldTarget:
		return orNone(in.Target, OptNone)
	case FieldFirePolicy:
		return string(in.FirePolicy)
	case FieldMovement:
		return string(in.Movement)
	case FieldWeapon:
		return in.Weapon.optionKey()
	case FieldPickup:
		return orNone(in.Pickup, OptNone)
	}
	return strconv.FormatFloat(in.Danger, 'f', 2, 64)
}

func orNone(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// ArbiterConfig configures an Arbiter. Zero values take the defaults.
type ArbiterConfig struct {
	// Fallback answers every observed request at once (the scripted
	// policy): it stands in when the model's answer is missing, invalid,
	// unconfident or expired, and its answers are compared with the
	// model's (nil: no fallback; expired answers are kept as stale).
	Fallback DecisionBackend
	// AnswerSource labels accepted backend answers (SourceModel; set
	// SourceScripted when the backend itself is the scripted policy).
	AnswerSource Source

	// MinConfidence gates choice and score answers (0.35); MinNoulMargin
	// gates noul answers on |2p-1| (0.2).
	MinConfidence, MinNoulMargin float64

	// FastTTL and SlowTTL are the least lifetimes of an answer, counted
	// from its request's SnapTime (300 ms, 1500 ms); the actual TTL is at
	// least P95() plus the larger of TTLMargin (100 ms) and the lane's
	// current request interval, so answers overlap at any rate.
	FastTTL, SlowTTL, TTLMargin time.Duration
	// P95 is the backend's latency p95 (Scheduler.P95; nil: 0).
	P95 func() time.Duration
	// Interval is a lane's current request interval (Scheduler.Interval;
	// nil: 0).
	Interval func(Lane) time.Duration
	// FastFallbackTTL and SlowFallbackTTL are the lifetimes of a fallback
	// answer (500 ms, 2000 ms): a lane that stopped asking (the fight is
	// over) falls back to the defaults.
	FastFallbackTTL, SlowFallbackTTL time.Duration
	// MaxStale is how long past its TTL a model answer is kept as stale
	// when there is no fallback (2 s).
	MaxStale time.Duration

	// Hysteresis. A mode switch needs ModeDelta (0.2) more probability
	// for the new mode and the current one held ModeHold (1.5 s), except
	// a switch to retreat at danger >= RetreatDanger (3.5). A target
	// switch needs TargetDelta (0.15), or the current target dead or
	// unseen for TargetLost (1 s). fire_policy and movement are held
	// FastHold (0.4 s); weapon and pickup SlowHold (0.5 s).
	ModeDelta, RetreatDanger, TargetDelta    float64
	ModeHold, TargetLost, FastHold, SlowHold time.Duration
}

// FieldOutcome is how one answer of a response was taken.
type FieldOutcome struct {
	Field Field
	// Value is the accepted answer, or the fallback's when the answer was
	// not accepted ("" without one).
	Value  string
	Source Source
	// Confidence is the answer's (validated) confidence.
	Confidence float64
	// Scripted is the fallback's answer to the same request ("" without).
	Scripted string
	// Reason is why the answer was not accepted ("" when it was).
	Reason string
}

// Trace returns the outcome as a trace decision field. Scripted is set
// only for model answers (the metrics count those as comparisons).
func (o *FieldOutcome) Trace() trace.Field {
	f := trace.Field{Name: o.Field.ID(), Value: o.Value, Source: o.Source.String(), Confidence: o.Confidence, Fallback: o.Reason}
	if o.Source == SourceModel {
		f.Scripted = o.Scripted
	}
	return f
}

// Applied is how a Result was taken.
type Applied struct {
	Seq  uint64
	Lane Lane
	// Dropped is why the whole result was not applied: stale, error,
	// timeout or out_of_order ("" when applied).
	Dropped string
	Fields  []FieldOutcome
}

// FieldStats are a field's counters.
type FieldStats struct {
	// Answers by validation outcome.
	Accepted, LowConfidence, Invalid, Missing int
	// Comparisons of accepted model answers with the fallback's answer to
	// the same request, and how many disagreed.
	Comparisons, Disagreements int
	// Ticks counts the Intents by the field's Source.
	Ticks [numSources]int
}

// Share returns the share of Intents whose field came from src.
func (s *FieldStats) Share(src Source) float64 {
	n := 0
	for _, t := range s.Ticks {
		n += t
	}
	if n == 0 {
		return 0
	}
	return float64(s.Ticks[src]) / float64(n)
}

// ArbiterStats are the arbiter's counters.
type ArbiterStats struct {
	Applied, Stale, Errors, OutOfOrder, FallbackErrors int
	Fields                                             [NumFields]FieldStats
}

// cand is a candidate value for a field.
type cand struct {
	ok     bool // a value is present (reason may still gate it)
	value  string
	score  float64
	probs  map[string]float64
	conf   float64
	seq    uint64
	snap   int64
	src    Source
	reason string
}

type current struct {
	set        bool
	value      string
	origin     Source
	originSeq  uint64
	originSnap int64
	since      int64
}

type fieldState struct {
	model, fb cand
	cur       current
}

type fbEntry struct {
	seq  uint64
	vals [NumFields]cand
}

const fbHistory = 32

// Arbiter turns answers into Intents: it validates and gates them, keeps
// them for their TTL, falls back to the scripted policy, applies
// hysteresis and records provenance. It is not safe for concurrent use;
// it depends only on its inputs (deterministic).
type Arbiter struct {
	cfg     ArbiterConfig
	f       [NumFields]fieldState
	lastSeq [NumLanes]uint64
	lastErr [NumLanes]string
	fbHist  [NumLanes][]fbEntry
	stats   ArbiterStats
}

// NewArbiter returns an Arbiter.
func NewArbiter(cfg ArbiterConfig) *Arbiter {
	if cfg.AnswerSource == SourceDefault {
		cfg.AnswerSource = SourceModel
	}
	deff := func(v *float64, d float64) {
		if *v <= 0 {
			*v = d
		}
	}
	defd := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	deff(&cfg.MinConfidence, 0.35)
	deff(&cfg.MinNoulMargin, 0.2)
	deff(&cfg.ModeDelta, 0.2)
	deff(&cfg.RetreatDanger, 3.5)
	deff(&cfg.TargetDelta, 0.15)
	defd(&cfg.FastTTL, 300*time.Millisecond)
	defd(&cfg.SlowTTL, 1500*time.Millisecond)
	defd(&cfg.TTLMargin, 100*time.Millisecond)
	defd(&cfg.FastFallbackTTL, 500*time.Millisecond)
	defd(&cfg.SlowFallbackTTL, 2000*time.Millisecond)
	defd(&cfg.MaxStale, 2*time.Second)
	defd(&cfg.ModeHold, 1500*time.Millisecond)
	defd(&cfg.TargetLost, time.Second)
	defd(&cfg.FastHold, 400*time.Millisecond)
	defd(&cfg.SlowHold, 500*time.Millisecond)
	return &Arbiter{cfg: cfg}
}

// TTL returns the current lifetime of a lane's answers.
func (a *Arbiter) TTL(l Lane) time.Duration {
	ttl := a.cfg.FastTTL
	if l == LaneSlow {
		ttl = a.cfg.SlowTTL
	}
	margin := a.cfg.TTLMargin
	if a.cfg.Interval != nil {
		margin = max(margin, a.cfg.Interval(l))
	}
	var p95 time.Duration
	if a.cfg.P95 != nil {
		p95 = a.cfg.P95()
	}
	return max(ttl, p95+margin)
}

func ms(d time.Duration) int64 { return durMs(d) }

func (a *Arbiter) laneFields(l Lane) []Field {
	if l == LaneFast {
		return []Field{FieldTarget, FieldFirePolicy, FieldMovement}
	}
	return []Field{FieldMode, FieldWeapon, FieldPickup, FieldDanger}
}

// validate checks an answer to q and returns it as a candidate; reason is
// set when it must not be used.
func (a *Arbiter) validate(q *Question, ans Answer, present bool) cand {
	c := cand{}
	if !present {
		c.reason = "missing"
		return c
	}
	if ans.Type != "" && ans.Type != q.Type {
		c.reason = "invalid"
		return c
	}
	c.ok = true
	clean := func(p float64) float64 {
		if math.IsNaN(p) || math.IsInf(p, 0) || p < 0 {
			return 0
		}
		return p
	}
	normalize := func(onehot string) {
		c.probs = map[string]float64{}
		sum := 0.0
		for _, o := range q.Options {
			p := clean(ans.Probabilities[o.Key])
			c.probs[o.Key] = p
			sum += p
		}
		if sum <= 0 {
			for k := range c.probs {
				c.probs[k] = 0
			}
			if onehot != "" {
				c.probs[onehot] = 1
			}
			return
		}
		for k := range c.probs {
			c.probs[k] /= sum
		}
	}
	conf := func(p float64) {
		if ans.HasConfidence {
			c.conf = math.Max(0, math.Min(1, clean(ans.Confidence)))
		} else {
			c.conf = p
		}
		if c.conf < a.cfg.MinConfidence {
			c.reason = "low_confidence"
		}
	}
	switch q.Type {
	case Choice:
		if q.Index(ans.Choice) < 0 {
			c.reason = "unknown_option"
			return c
		}
		c.value = ans.Choice
		normalize(ans.Choice)
		conf(c.probs[c.value])
	case Score:
		s := ans.Score
		if math.IsNaN(s) || math.IsInf(s, 0) {
			c.reason = "invalid"
			return c
		}
		c.score = math.Max(0, math.Min(float64(len(q.Options)-1), s))
		c.value = levelKey(int(math.Round(c.score)))
		normalize(c.value)
		conf(c.probs[c.value])
	case Noul:
		p := math.Max(0, math.Min(1, clean(ans.Noul)))
		c.value = "false"
		if p >= 0.5 {
			c.value = "true"
		}
		c.score = p
		c.probs = map[string]float64{"true": p, "false": 1 - p}
		c.conf = math.Abs(2*p - 1)
		if c.conf < a.cfg.MinNoulMargin {
			c.reason = "low_confidence"
		}
	default:
		c.ok, c.reason = false, "invalid"
	}
	return c
}

func (a *Arbiter) countValidation(f Field, c *cand) {
	fs := &a.stats.Fields[f]
	switch c.reason {
	case "":
		fs.Accepted++
	case "low_confidence":
		fs.LowConfidence++
	case "missing":
		fs.Missing++
	default:
		fs.Invalid++
	}
}

// Observe gives the arbiter a request as it is built (submitted or not):
// the fallback answers it at once.
func (a *Arbiter) Observe(req *Request) {
	if a.cfg.Fallback == nil {
		return
	}
	resp, err := a.cfg.Fallback.Decide(context.Background(), req)
	if err != nil || resp == nil {
		a.stats.FallbackErrors++
		return
	}
	var e fbEntry
	e.seq = req.Seq
	for _, f := range a.laneFields(req.Lane) {
		q := req.Question(f.ID())
		var c cand
		if q == nil {
			c = cand{ok: true, value: defaultValue(f), src: SourceDefault, reason: "not_asked", conf: 1}
		} else {
			ans, present := resp.Answers[q.ID]
			c = a.validate(q, ans, present)
			c.src = SourceScripted
		}
		c.seq, c.snap = req.Seq, req.SnapTime
		e.vals[f] = c
		if c.ok && (c.reason == "" || c.reason == "not_asked") {
			a.f[f].fb = c
		}
	}
	h := append(a.fbHist[req.Lane], e)
	if len(h) > fbHistory {
		h = append(h[:0], h[len(h)-fbHistory:]...)
	}
	a.fbHist[req.Lane] = h
}

func (a *Arbiter) fallbackFor(l Lane, seq uint64) *fbEntry {
	h := a.fbHist[l]
	for i := len(h) - 1; i >= 0; i-- {
		if h[i].seq == seq {
			return &h[i]
		}
	}
	return nil
}

func displayValue(f Field, c *cand) string {
	if !c.ok {
		return ""
	}
	if f == FieldDanger {
		return strconv.FormatFloat(c.score, 'f', 2, 64)
	}
	return c.value
}

// Apply takes a collected result. Stale, failed and out-of-order results
// change nothing but the counters (an error marks the lane).
func (a *Arbiter) Apply(r Result) Applied {
	req := r.Req
	out := Applied{Seq: req.Seq, Lane: req.Lane}
	switch {
	case r.Stale:
		out.Dropped = "stale"
		a.stats.Stale++
		return out
	case r.Err != nil:
		out.Dropped = "error"
		if r.Timeout {
			out.Dropped = "timeout"
		}
		a.lastErr[req.Lane] = out.Dropped
		a.stats.Errors++
		return out
	case req.Seq <= a.lastSeq[req.Lane]:
		out.Dropped = "out_of_order"
		a.stats.OutOfOrder++
		return out
	}
	a.lastSeq[req.Lane], a.lastErr[req.Lane] = req.Seq, ""
	a.stats.Applied++
	fb := a.fallbackFor(req.Lane, req.Seq)
	for _, f := range a.laneFields(req.Lane) {
		fs := &a.f[f]
		q := req.Question(f.ID())
		if q == nil {
			fs.model = cand{reason: "not_asked", seq: req.Seq, snap: req.SnapTime}
			continue
		}
		ans, present := r.Resp.Answers[q.ID]
		c := a.validate(q, ans, present)
		c.src, c.seq, c.snap = a.cfg.AnswerSource, req.Seq, req.SnapTime
		fs.model = c
		a.countValidation(f, &c)
		o := FieldOutcome{Field: f, Value: displayValue(f, &c), Source: c.src, Confidence: c.conf, Reason: c.reason}
		if fb != nil {
			s := &fb.vals[f]
			o.Scripted = displayValue(f, s)
			if c.reason == "" && s.ok && s.reason == "" {
				st := &a.stats.Fields[f]
				st.Comparisons++
				if s.value != c.value {
					st.Disagreements++
				}
			}
			if c.reason != "" {
				o.Value, o.Source = o.Scripted, s.src
				if !s.ok {
					o.Source = SourceDefault
				}
			}
		} else if c.reason != "" {
			o.Value, o.Source = "", SourceDefault
		}
		out.Fields = append(out.Fields, o)
	}
	return out
}

// usable reports whether a target or pickup value still names something
// there in the belief: a live track, an item not taken (b nil: assume
// so). An enemy out of sight stays usable (it is remembered).
func (a *Arbiter) usable(f Field, value string, b *worldmodel.Belief) bool {
	if b == nil || value == OptNone || value == "" {
		return true
	}
	switch f {
	case FieldTarget:
		t := b.Track(value)
		return t != nil && t.Life == worldmodel.LifeAlive
	case FieldPickup:
		for i := range b.Items {
			if b.Items[i].ID == value {
				return b.Items[i].Life == worldmodel.LifeAlive
			}
		}
		return false
	}
	return true
}

// targetLost reports a current target that is dead, forgotten or unseen
// for more than TargetLost: switching away needs no margin then.
func (a *Arbiter) targetLost(value string, b *worldmodel.Belief, now int64) bool {
	if b == nil || value == OptNone || value == "" {
		return false
	}
	t := b.Track(value)
	if t == nil || t.Life != worldmodel.LifeAlive {
		return true
	}
	seen := t.LastSeen
	if seen == 0 {
		seen = t.LastUpdate
	}
	return !t.Visible && now-seen > ms(a.cfg.TargetLost)
}

func (a *Arbiter) fallbackTTL(l Lane) time.Duration {
	if l == LaneSlow {
		return a.cfg.SlowFallbackTTL
	}
	return a.cfg.FastFallbackTTL
}

func defaultCand(f Field) cand {
	v := defaultValue(f)
	return cand{ok: true, value: v, probs: map[string]float64{v: 1}, conf: 0, src: SourceDefault}
}

// candidate is the value a field would take now without hysteresis, and
// why the model's answer is not it.
func (a *Arbiter) candidate(f Field, now int64, b *worldmodel.Belief) (cand, string) {
	fs := &a.f[f]
	m := &fs.model
	l := f.Lane()
	ttl := ms(a.TTL(l))
	var reason string
	switch {
	case m.seq == 0:
		reason = "no_answer"
		if e := a.lastErr[l]; e != "" {
			reason = e
		}
	case m.reason != "":
		reason = m.reason
	case now-m.snap > ttl:
		reason = "ttl"
		if e := a.lastErr[l]; e != "" {
			reason = e
		}
	case !a.usable(f, m.value, b):
		reason = "gone"
	default:
		return *m, ""
	}
	if reason == "not_asked" {
		return defaultCand(f), reason
	}
	if fb := &fs.fb; fb.ok && fb.seq != 0 && now-fb.snap <= ms(a.fallbackTTL(l)) && a.usable(f, fb.value, b) {
		return *fb, reason
	}
	if a.cfg.Fallback == nil && m.seq != 0 && m.reason == "" && now-m.snap <= ttl+ms(a.cfg.MaxStale) && a.usable(f, m.value, b) {
		c := *m
		c.src = SourceStale
		return c, reason
	}
	return defaultCand(f), reason
}

// switchAllowed applies the field's hysteresis to a change from cur to c.
func (a *Arbiter) switchAllowed(f Field, cur *current, c *cand, now int64, b *worldmodel.Belief, danger float64) bool {
	held := now - cur.since
	dp := c.probs[c.value] - c.probs[cur.value]
	switch f {
	case FieldMode:
		if c.value == string(ModeRetreat) && danger >= a.cfg.RetreatDanger {
			return true
		}
		return dp >= a.cfg.ModeDelta-1e-9 && held >= ms(a.cfg.ModeHold)
	case FieldTarget:
		return a.targetLost(cur.value, b, now) || dp >= a.cfg.TargetDelta-1e-9
	case FieldFirePolicy, FieldMovement:
		return held >= ms(a.cfg.FastHold)
	case FieldWeapon:
		return held >= ms(a.cfg.SlowHold)
	case FieldPickup:
		return !a.usable(f, cur.value, b) || held >= ms(a.cfg.SlowHold)
	}
	return true
}

// resolve decides one field at now.
func (a *Arbiter) resolve(f Field, now int64, b *worldmodel.Belief, danger float64) (string, float64, FieldProvenance) {
	c, reason := a.candidate(f, now, b)
	fs := &a.f[f]
	cur := &fs.cur
	prov := FieldProvenance{Source: c.src, Reason: reason, Confidence: c.conf, Seq: c.seq}
	if f == FieldDanger {
		return c.value, c.score, prov
	}
	if !cur.set || c.value == cur.value || a.switchAllowed(f, cur, &c, now, b, danger) {
		if !cur.set || c.value != cur.value {
			cur.since = now
		}
		*cur = current{set: true, value: c.value, origin: c.src, originSeq: c.seq, originSnap: c.snap, since: cur.since}
		return c.value, c.score, prov
	}
	// held: the previous value stays, with the provenance of its origin
	src := cur.origin
	if src == SourceModel && now-cur.originSnap > ms(a.TTL(f.Lane())) {
		src = SourceStale
	}
	return cur.value, 0, FieldProvenance{Source: src, Reason: "held", Seq: cur.originSeq}
}

// Intent decides every field at now; b is the current belief (for the
// target and pickup checks; nil skips them).
func (a *Arbiter) Intent(now int64, b *worldmodel.Belief) Intent {
	in := Intent{Time: now}
	_, danger, dp := a.resolve(FieldDanger, now, b, 0)
	in.Danger, in.Provenance.Danger = danger, dp
	a.stats.Fields[FieldDanger].Ticks[dp.Source]++
	for _, f := range []Field{FieldMode, FieldTarget, FieldFirePolicy, FieldMovement, FieldWeapon, FieldPickup} {
		v, _, p := a.resolve(f, now, b, danger)
		*in.Provenance.ptr(f) = p
		a.stats.Fields[f].Ticks[p.Source]++
		switch f {
		case FieldMode:
			in.Mode = Mode(v)
		case FieldTarget:
			in.Target = noneToEmpty(v, OptNone)
		case FieldFirePolicy:
			in.FirePolicy = FirePolicy(v)
		case FieldMovement:
			in.Movement = Movement(v)
		case FieldWeapon:
			in.Weapon = WeaponKey(noneToEmpty(v, OptKeep))
		case FieldPickup:
			in.Pickup = noneToEmpty(v, OptNone)
		}
	}
	return in
}

func noneToEmpty(v, none string) string {
	if v == none {
		return ""
	}
	return v
}

// Stats returns the counters.
func (a *Arbiter) Stats() ArbiterStats { return a.stats }

// Reset forgets the fields' answers and hysteresis state (a new level or
// a reload: track and item ids start over). Counters are kept.
func (a *Arbiter) Reset() {
	a.f = [NumFields]fieldState{}
	a.lastErr = [NumLanes]string{}
	a.fbHist = [NumLanes][]fbEntry{}
}
