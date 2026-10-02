package metrics

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"time"

	"quake2web/server/internal/agent/trace"
)

// Collector consumes trace events (as a trace.Sink on the Bus, or replayed
// from a trace file) and produces the run summary. Events are attributed to
// their envelope's episode (ep) and level (lvl). It is safe for concurrent
// use: Summary may be called while events arrive.
type Collector struct {
	mu sync.Mutex

	s            RunSummary
	modelBackend bool
	scriptedOnly bool

	episodes map[int]*episodeAcc
	order    []int // episode numbers in first-seen order

	fields    map[string]*Provenance
	latencies []float64
	byStatus  map[string]int

	firstWall, lastWall int64
	firstGMs, lastGMs   int64
	seen                bool

	// ticks accumulates the provenance events by episode, tickEv the lane
	// tick events (which win where an episode has any); gate, when set,
	// is the provenance gate Summary evaluates.
	ticks  map[int]*tickAcc
	tickEv map[int]*tickAcc
	gate   *GateConfig
}

type episodeAcc struct {
	sum      EpisodeSummary
	ended    bool
	startGMs int64
	lastGMs  int64
	totals   Totals // event counts outside the level summaries' scope
	levels   map[int]int
}

// NewCollector returns an empty collector.
func NewCollector() *Collector {
	return &Collector{
		s:        RunSummary{Schema: Schema, Outcome: OutcomeIncomplete},
		episodes: map[int]*episodeAcc{},
		fields:   map[string]*Provenance{},
		byStatus: map[string]int{},
		ticks:    map[int]*tickAcc{},
		tickEv:   map[int]*tickAcc{},
	}
}

// SetGate makes Summary evaluate the provenance gate with cfg: the
// summary's Gate is set and ModelDriven becomes the gate's verdict.
func (c *Collector) SetGate(cfg GateConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g := cfg.withDefaults()
	c.gate = &g
}

var _ trace.Sink = (*Collector)(nil)

// Write implements trace.Sink.
func (c *Collector) Write(e trace.Event) error { return c.Add(e) }

// Add accounts one event. An event whose body does not decode is counted
// but otherwise ignored, and reported.
func (c *Collector) Add(e trace.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.s.Events++
	if c.s.Run == "" {
		c.s.Run = e.Run
	}
	if !c.seen || e.Wall < c.firstWall {
		c.firstWall = e.Wall
	}
	if !c.seen || e.GMs < c.firstGMs {
		c.firstGMs = e.GMs
	}
	c.seen = true
	if e.Wall > c.lastWall {
		c.lastWall = e.Wall
	}
	if e.GMs > c.lastGMs {
		c.lastGMs = e.GMs
	}
	if err := c.add(e); err != nil {
		return fmt.Errorf("metrics: %s event %d: %w", e.Type, e.Seq, err)
	}
	return nil
}

func (c *Collector) episode(n int, gms int64) *episodeAcc {
	ep := c.episodes[n]
	if ep == nil {
		ep = &episodeAcc{sum: EpisodeSummary{Index: n, Outcome: OutcomeIncomplete}, startGMs: gms, levels: map[int]int{}}
		c.episodes[n] = ep
		c.order = append(c.order, n)
	}
	if gms > ep.lastGMs {
		ep.lastGMs = gms
	}
	return ep
}

// level returns the level summary the event belongs to, or nil before its
// level_start.
func (ep *episodeAcc) level(lvl int) *LevelSummary {
	i, ok := ep.levels[lvl]
	if !ok {
		return nil
	}
	return &ep.sum.Levels[i]
}

// episodeScoped are the event types that belong to an episode (and create
// its summary); the others are accounted run-wide.
func episodeScoped(typ string) bool {
	switch typ {
	case trace.TypeEpisodeStart, trace.TypeLevelStart, trace.TypeLevelEnd, trace.TypeDamage,
		trace.TypeKill, trace.TypeDeath, trace.TypeReload, trace.TypeStuck, trace.TypeEpisodeEnd:
		return true
	}
	return false
}

func (c *Collector) add(e trace.Event) error {
	var ep *episodeAcc
	var lv *LevelSummary
	if episodeScoped(e.Type) {
		ep = c.episode(e.Ep, e.GMs)
		lv = ep.level(e.Lvl)
	} else if x := c.episodes[e.Ep]; x != nil && e.GMs > x.lastGMs {
		x.lastGMs = e.GMs
	}
	switch e.Type {
	case trace.TypeRunStart:
		var b trace.RunStart
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		c.s.Backend, c.s.Model, c.s.Session = b.Backend, b.Model, b.Session
		c.s.Maps, c.s.Skill, c.s.Seed = append([]string(nil), b.Maps...), b.Skill, b.Seed
		c.modelBackend = b.ModelBackend

	case trace.TypeEpisodeStart:
		var b trace.EpisodeStart
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		ep.sum.Seed = b.Seed
		ep.startGMs = e.GMs

	case trace.TypeLevelStart:
		var b trace.LevelStart
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		ep.levels[e.Lvl] = len(ep.sum.Levels)
		ep.sum.Levels = append(ep.sum.Levels, LevelSummary{Lvl: e.Lvl, Map: e.Map, Visit: b.Visit,
			Outcome: OutcomeIncomplete, StartGMs: e.GMs})

	case trace.TypeLevelEnd:
		var b trace.LevelEnd
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		if lv == nil {
			return fmt.Errorf("level_end for level %d without level_start", e.Lvl)
		}
		lv.Outcome, lv.Reason = b.Outcome, b.Reason
		lv.TimeMs = e.GMs - lv.StartGMs
		lv.CombatMs = b.CombatMs
		lv.Kills, lv.Monsters, lv.Secrets, lv.TotalSecrets = b.KilledMonsters, b.TotalMonsters, b.FoundSecrets, b.TotalSecrets

	case trace.TypeDamage:
		var b trace.Damage
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		ep.totals.DamageTaken += b.Amount
		if lv != nil {
			lv.DamageTaken += b.Amount
		}

	case trace.TypeKill:
		ep.totals.BotKills++
		if lv != nil {
			lv.BotKills++
		}

	case trace.TypeDeath:
		ep.totals.Deaths++
		if lv != nil {
			lv.Deaths++
		}

	case trace.TypeReload:
		ep.totals.Reloads++
		if lv != nil {
			lv.Reloads++
		}

	case trace.TypeStuck:
		ep.totals.Stuck++
		if lv != nil {
			lv.Stuck++
		}

	case trace.TypeDecision:
		var b trace.Decision
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		if b.Lane == trace.LaneTick {
			// a decision tick of the bot, not a request: its Intent says
			// what was acted on (the per-tick provenance)
			c.s.Decisions.Ticks++
			acc := c.tickEv[e.Ep]
			if acc == nil {
				acc = &tickAcc{}
				c.tickEv[e.Ep] = acc
			}
			acc.addTick(b.Intent)
			break
		}
		c.s.Decisions.Decisions++
		for _, f := range b.Fields {
			p := c.fields[f.Name]
			if p == nil {
				p = &Provenance{}
				c.fields[f.Name] = p
			}
			switch f.Source {
			case trace.SourceModel:
				p.Model++
				if f.Scripted != "" {
					c.s.Decisions.Comparisons++
					if f.Scripted != f.Value {
						c.s.Decisions.Disagreements++
					}
				}
			case trace.SourceScripted:
				p.Scripted++
			case trace.SourceReflex:
				p.Reflex++
			case trace.SourceStale:
				p.Stale++
			default:
				p.Other++
			}
		}

	case trace.TypeAPICall:
		var b trace.APICall
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		a := &c.s.API
		a.Calls++
		c.byStatus[strconv.Itoa(b.Status)]++
		ok := b.Err == "" && (b.Status == 0 || b.Status >= 200 && b.Status < 300)
		if ok {
			a.OK++
			c.latencies = append(c.latencies, b.LatencyMs)
			if b.Stale {
				a.Stale++
			}
			if b.Combat {
				a.CombatCalls++
			}
		} else {
			a.Errors++
		}
		if b.Retry > 0 {
			a.Retries++
		}
		a.InputTokens += b.InputTokens
		a.OutputTokens += b.OutputTokens
		a.CostUSD += b.CostUSD

	case trace.TypeBudget:
		var b trace.Budget
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		c.s.Budget = &BudgetState{SpentUSD: b.SpentUSD, LimitUSD: b.LimitUSD, RateHz: b.RateHz,
			ScriptedOnly: b.ScriptedOnly, Reason: b.Reason}
		if b.ScriptedOnly {
			c.scriptedOnly = true
		}

	case trace.TypeError:
		var b trace.Error
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		c.s.Errors++
		c.s.LastError = b.Msg

	case trace.TypeEpisodeEnd:
		var b trace.EpisodeEnd
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		ep.sum.Outcome, ep.sum.Reason = b.Outcome, b.Reason
		ep.sum.GameMs = e.GMs - ep.startGMs
		ep.ended = true

	case trace.TypeProvenance:
		var b trace.Provenance
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		acc := c.ticks[e.Ep]
		if acc == nil {
			acc = &tickAcc{}
			c.ticks[e.Ep] = acc
		}
		acc.add(b)

	case trace.TypeRunEnd:
		var b trace.RunEnd
		if err := e.DecodeBody(&b); err != nil {
			return err
		}
		c.s.Outcome, c.s.Reason = b.Outcome, b.Reason
	}
	return nil
}

// Summary returns the run summary of the events so far (derived rates and
// percentiles included). Levels and episodes without an end event are
// "incomplete" and measured up to the last event.
func (c *Collector) Summary() RunSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.s
	s.Maps = append([]string(nil), c.s.Maps...)
	s.ModelDriven = c.modelBackend && !c.scriptedOnly
	if c.seen {
		s.Started = time.UnixMilli(c.firstWall).UTC().Format(time.RFC3339Nano)
		s.WallMs = c.lastWall - c.firstWall
		s.GameMs = c.lastGMs - c.firstGMs // without episodes; else their sum below
	}
	if c.s.Budget != nil {
		b := *c.s.Budget
		s.Budget = &b
	}

	s.Episodes = []EpisodeSummary{}
	s.EpisodeSeeds = nil
	s.Totals = Totals{}
	order := append([]int(nil), c.order...)
	sort.Ints(order)
	var episodesMs int64
	for _, n := range order {
		ep := c.episodes[n]
		es := ep.sum
		es.Levels = append([]LevelSummary{}, ep.sum.Levels...)
		if !ep.ended {
			es.GameMs = ep.lastGMs - ep.startGMs
		}
		t := Totals{Deaths: ep.totals.Deaths, Reloads: ep.totals.Reloads, DamageTaken: ep.totals.DamageTaken,
			BotKills: ep.totals.BotKills, Stuck: ep.totals.Stuck}
		for i := range es.Levels {
			l := &es.Levels[i]
			if l.Outcome == OutcomeIncomplete {
				l.TimeMs = ep.lastGMs - l.StartGMs
			}
			t.Levels++
			if l.Outcome == trace.OutcomeExit || l.Outcome == trace.OutcomeVictory {
				t.LevelsCompleted++
			}
			t.Kills += l.Kills
			t.Monsters += l.Monsters
			t.Secrets += l.Secrets
			t.TotalSecrets += l.TotalSecrets
			t.CombatMs += l.CombatMs
		}
		es.Totals = t
		es.Ticks = c.episodeTicks(n).stats()
		s.Totals.add(t)
		s.Episodes = append(s.Episodes, es)
		s.EpisodeSeeds = append(s.EpisodeSeeds, es.Seed)
		episodesMs += es.GameMs
	}
	if len(order) > 0 {
		// every episode runs a fresh session whose game clock restarts, so
		// the run's game time is the sum, not the span of all gms
		s.GameMs = episodesMs
	}

	d := &s.Decisions
	d.Fields = map[string]Provenance{}
	d.All = Provenance{}
	var gate Provenance
	for name, p := range c.fields {
		q := *p
		q.finish()
		d.Fields[name] = q
		d.All.add(q)
		for _, g := range gateFields {
			if g == name {
				gate.add(q)
			}
		}
	}
	d.All.finish()
	gate.finish()
	d.GateModelShare = gate.ModelShare
	d.DisagreementRate = ratio(d.Disagreements, d.Comparisons)

	a := &s.API
	a.ByStatus = nil
	if len(c.byStatus) > 0 {
		a.ByStatus = map[string]int{}
		for k, v := range c.byStatus {
			a.ByStatus[k] = v
		}
	}
	a.StaleRate = ratio(a.Stale, a.OK)
	a.LatencyMs = percentiles(c.latencies)
	if s.Totals.CombatMs > 0 {
		a.CombatQPS = float64(a.CombatCalls) / (float64(s.Totals.CombatMs) / 1000)
	}

	if len(c.ticks) > 0 || len(c.tickEv) > 0 {
		var all tickAcc
		set := map[int]bool{}
		for n := range c.ticks {
			set[n] = true
		}
		for n := range c.tickEv {
			set[n] = true
		}
		eps := make([]int, 0, len(set))
		for n := range set {
			eps = append(eps, n)
		}
		sort.Ints(eps)
		for _, n := range eps {
			all.merge(c.episodeTicks(n))
		}
		s.Ticks = all.stats()
	}
	if c.gate != nil {
		g := evaluateGate(&s, *c.gate, c.modelBackend, c.scriptedOnly)
		s.Gate = &g
		s.ModelDriven = g.Passed
	}
	return s
}

// episodeTicks returns episode n's per-tick provenance: its lane tick
// events when there are any (they cover the episode as far as it got),
// else its provenance events (nil without either).
func (c *Collector) episodeTicks(n int) *tickAcc {
	if acc := c.tickEv[n]; acc != nil && acc.ticks > 0 {
		return acc
	}
	return c.ticks[n]
}

// percentiles returns nearest-rank percentiles of v.
func percentiles(v []float64) Percentiles {
	if len(v) == 0 {
		return Percentiles{}
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	rank := func(p float64) float64 {
		i := int(math.Ceil(p/100*float64(len(s)))) - 1
		if i < 0 {
			i = 0
		}
		return s[i]
	}
	return Percentiles{P50: rank(50), P95: rank(95), P99: rank(99), Max: s[len(s)-1]}
}
