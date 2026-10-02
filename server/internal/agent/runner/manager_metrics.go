package runner

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/spectate"
)

// ManagerMetrics are the Manager's Prometheus metrics (q2bot_*). Their
// labels are low-cardinality on purpose: backends, sources, outcomes and
// reasons from fixed vocabularies, maps of the campaign; never a run or
// bot id. Every label value passes a guard that folds unexpected or
// excess values into "other".
type ManagerMetrics struct {
	runsActive   prometheus.GaugeFunc
	runsTotal    *prometheus.CounterVec
	levels       *prometheus.CounterVec
	deaths       *prometheus.CounterVec
	kills        prometheus.Counter
	decisions    *prometheus.CounterVec
	latency      *prometheus.HistogramVec
	fallbacks    *prometheus.CounterVec
	apiRequests  *prometheus.CounterVec
	apiCost      *prometheus.CounterVec
	apiTokens    *prometheus.CounterVec
	viewers      prometheus.GaugeFunc
	resyncs      *prometheus.CounterVec
	traceDropped *prometheus.CounterVec
	labels       labelGuard
}

func newManagerMetrics(active, viewers func() float64) *ManagerMetrics {
	m := &ManagerMetrics{
		runsActive: prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "q2bot_runs_active",
			Help: "Bots running (starting or playing)."}, active),
		runsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_runs_total",
			Help: "Bot runs ended, by result (finished, failed, stopped)."}, []string{"result"}),
		levels: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_levels_completed_total",
			Help: "Levels bots left through an exit or won, by map."}, []string{"map"}),
		deaths: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_deaths_total",
			Help: "Bot deaths, by map."}, []string{"map"}),
		kills: prometheus.NewCounter(prometheus.CounterOpts{Name: "q2bot_kills_total",
			Help: "Monsters bots killed (as the bots perceived it)."}),
		decisions: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_decisions_total",
			Help: "Decision fields bots acted on, per decision tick, by backend and source (model, scripted, stale, reflex, default)."},
			[]string{"backend", "source"}),
		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "q2bot_decision_latency_seconds",
			Help:    "Latency of the decision requests bots got an answer to, by backend.",
			Buckets: []float64{0.005, 0.025, 0.05, 0.1, 0.15, 0.2, 0.3, 0.5, 0.8, 1.5, 3}}, []string{"backend"}),
		fallbacks: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_fallback_total",
			Help: "Decision fields bots acted on without the model's answer, by reason."}, []string{"reason"}),
		apiRequests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_api_requests_total",
			Help: "Decision backend requests, by backend and outcome (ok, stale, error, rate_limited, overloaded)."},
			[]string{"backend", "outcome"}),
		apiCost: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_api_cost_usd_total",
			Help: "Decision backend spend in USD, by backend."}, []string{"backend"}),
		apiTokens: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_api_tokens_total",
			Help: "Decision backend tokens, by backend and direction (input, output)."}, []string{"backend", "direction"}),
		viewers: prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "q2bot_viewers",
			Help: "Viewers watching bots live (video relay connections)."}, viewers),
		resyncs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_relay_resyncs_total",
			Help: "Keyframes the live relay sent viewers, by reason (join, delta, drop, feed, client)."}, []string{"reason"}),
		traceDropped: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "q2bot_trace_events_dropped_total",
			Help: "Trace events a lossy consumer dropped because it fell behind, by sink."}, []string{"sink"}),
		labels: labelGuard{max: 64},
	}
	return m
}

// collectors returns every metric.
func (m *ManagerMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.runsActive, m.runsTotal, m.levels, m.deaths, m.kills, m.decisions, m.latency, m.fallbacks,
		m.apiRequests, m.apiCost, m.apiTokens, m.viewers, m.resyncs, m.traceDropped}
}

// labelGuard bounds a label's values: a value that is not a short
// lowercase identifier, or comes after max distinct values, is "other".
type labelGuard struct {
	mu   sync.Mutex
	max  int
	seen map[string]bool
}

func (g *labelGuard) value(v string) string {
	if v == "" {
		return "none"
	}
	if len(v) > 32 {
		return "other"
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || c == ':') {
			return "other"
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = map[string]bool{}
	}
	if !g.seen[v] {
		if len(g.seen) >= g.max {
			return "other"
		}
		g.seen[v] = true
	}
	return v
}

// runEnded counts an ended run.
func (m *ManagerMetrics) runEnded(result string) {
	m.runsTotal.WithLabelValues(m.labels.value(result)).Inc()
}

// event accounts one trace event of a bot with backend.
func (m *ManagerMetrics) event(backend string, e *trace.Event, d *trace.Decision) {
	be := m.labels.value(backend)
	switch e.Type {
	case trace.TypeLevelEnd:
		var le trace.LevelEnd
		if decodeBody(e, &le) == nil && (le.Outcome == trace.OutcomeExit || le.Outcome == trace.OutcomeVictory) {
			m.levels.WithLabelValues(m.labels.value(e.Map)).Inc()
		}
	case trace.TypeDeath:
		m.deaths.WithLabelValues(m.labels.value(e.Map)).Inc()
	case trace.TypeKill:
		m.kills.Inc()
	case trace.TypeDecision:
		if d == nil {
			return
		}
		switch d.Lane {
		case trace.LaneTick:
			if d.Intent == nil {
				return
			}
			for _, f := range d.Intent.Fields {
				m.decisions.WithLabelValues(be, m.labels.value(f.Source)).Inc()
				if f.Fallback != "" && f.Source != trace.SourceModel && f.Source != trace.SourceReflex {
					m.fallbacks.WithLabelValues(m.labels.value(f.Fallback)).Inc()
				}
			}
		case trace.LaneFast, trace.LaneSlow:
			if d.Err == "" && len(d.Response) > 0 {
				m.latency.WithLabelValues(be).Observe(d.LatencyMs / 1000)
			}
		}
	case trace.TypeAPICall:
		var c trace.APICall
		if decodeBody(e, &c) != nil {
			return
		}
		m.apiRequests.WithLabelValues(be, apiOutcome(&c)).Inc()
		if c.CostUSD > 0 {
			m.apiCost.WithLabelValues(be).Add(c.CostUSD)
		}
		if c.InputTokens > 0 {
			m.apiTokens.WithLabelValues(be, "input").Add(float64(c.InputTokens))
		}
		if c.OutputTokens > 0 {
			m.apiTokens.WithLabelValues(be, "output").Add(float64(c.OutputTokens))
		}
	}
}

// apiOutcome classifies an api_call.
func apiOutcome(c *trace.APICall) string {
	switch {
	case c.Status == 429:
		return "rate_limited"
	case c.Status == 529 || c.Status == 503:
		return "overloaded"
	case c.Err != "":
		return "error"
	case c.Stale:
		return "stale"
	}
	return "ok"
}

// relayMetrics feeds the live relays' resyncs (spectate.Metrics; the
// other relay events are not exported).
type relayMetrics struct{ m *ManagerMetrics }

func (r relayMetrics) ViewerJoined()                   {}
func (r relayMetrics) ViewerLeft(spectate.LeaveReason) {}
func (r relayMetrics) DatagramIn()                     {}
func (r relayMetrics) DatagramOut()                    {}
func (r relayMetrics) Drop(spectate.DropReason, int)   {}
func (r relayMetrics) Resync(why spectate.ResyncReason) {
	r.m.resyncs.WithLabelValues(r.m.labels.value(string(why))).Inc()
}

var _ spectate.Metrics = relayMetrics{}
