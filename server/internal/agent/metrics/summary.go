// Package metrics turns an agent trace into the run summary written as
// run.json (schema "q2bot.run/1") and episode.json: outcome, per-level time,
// deaths, kills and secrets, damage taken, decision provenance per field,
// stale-answer and disagreement rates, API latency percentiles, tokens,
// cost, the pinned model and the seeds.
//
// Kill and secret counts come from the game's level counters carried by
// level_end (metrics only: the bot never sees them).
package metrics

// Schema is the run summary schema ("schema" of run.json).
const Schema = "q2bot.run/1"

// Run outcomes (RunSummary.Outcome) besides what run_end reports.
const (
	OutcomeIncomplete = "incomplete" // no run_end (yet)
)

// gateFields are the decision fields of the model-share gate
// (DecisionStats.GateModelShare; phase 9 wants at least 70 % from the model).
var gateFields = [...]string{"target", "fire_policy", "mode"}

// RunSummary is run.json.
type RunSummary struct {
	Schema  string `json:"schema"`
	Run     string `json:"run"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`

	Backend string   `json:"backend,omitempty"`
	Model   string   `json:"model,omitempty"` // pinned model id
	Session string   `json:"session,omitempty"`
	Maps    []string `json:"maps,omitempty"`
	Skill   int      `json:"skill"`
	Seed    uint64   `json:"seed"`
	// EpisodeSeeds are the seeds of the episodes in order.
	EpisodeSeeds []uint64 `json:"episode_seeds,omitempty"`
	// Config is the run's configuration as its run_start recorded it
	// (Collector.SetConfig; nil in a run.json written before the section
	// existed, which still validates).
	Config *RunConfig `json:"config,omitempty"`
	// ModelDriven is false unless a model backend answered for the whole
	// run (a budget switching to scripted-only clears it). With a gate
	// (Collector.SetGate) it is the gate's verdict.
	ModelDriven bool `json:"model_driven"`
	// Gate is the provenance gate's verdict (Collector.SetGate; nil
	// without one).
	Gate *Gate `json:"gate,omitempty"`

	Started string `json:"started,omitempty"` // first event, RFC 3339 UTC
	WallMs  int64  `json:"wall_ms"`
	// GameMs is the episodes' game time summed (each episode has its own
	// session clock).
	GameMs int64 `json:"game_ms"`
	Events int   `json:"events"`

	Totals    Totals           `json:"totals"`
	Decisions DecisionStats    `json:"decisions"`
	Ticks     *TickStats       `json:"ticks,omitempty"` // per-tick provenance (tick or provenance events; nil without them)
	API       APIStats         `json:"api"`
	Budget    *BudgetState     `json:"budget,omitempty"`
	Errors    int              `json:"errors"`
	LastError string           `json:"last_error,omitempty"`
	Episodes  []EpisodeSummary `json:"episodes"`
}

// RunConfig is the configuration a run was played with (run.json
// "config"), as its run_start event recorded it: what tells two runs'
// numbers apart (a clean or a noisy mock, the simulated latency, the
// watchdogs, the budgets) and what a rerun needs. It holds no credential:
// the API key is never part of a run's configuration.
type RunConfig struct {
	Backend string `json:"backend"`
	// ModelBackend: the backend queries a model (jev, mock).
	ModelBackend bool   `json:"model_backend"`
	Model        string `json:"model,omitempty"` // pinned model id
	Session      string `json:"session"`         // lockstep | inproc
	// Campaign names the route tables; Maps are the visits played and
	// StopAfter how many (0: the whole campaign, to its terminal).
	Campaign  string   `json:"campaign,omitempty"`
	Maps      []string `json:"maps,omitempty"`
	StopAfter int      `json:"stop_after,omitempty"`
	Skill     int      `json:"skill"`
	// Seed is the run's seed: episode i plays with Seed+i.
	Seed     uint64 `json:"seed"`
	Episodes int    `json:"episodes"`
	// SimLatency is a lockstep run's simulated backend latency as
	// q2bot -sim-latency reads it ("212ms", "80ms,150ms", "0s" for none)
	// and SimLatencyMs its mean; both are empty in a realtime session.
	SimLatency   string  `json:"sim_latency,omitempty"`
	SimLatencyMs float64 `json:"sim_latency_ms,omitempty"`
	// Mock is the fake Jev server's policy and faults (backend mock).
	Mock *MockConfig `json:"mock,omitempty"`
	// Budget is the run's own budget (nil: none). A process-wide account
	// limit (q2bot -account-qps, q2server's Q2_JEV_MAX_QPS) is not
	// recorded.
	Budget *BudgetConfig `json:"budget,omitempty"`
	// MaxDeaths (per level; -1: no cap), LevelTimeout and EpisodeTimeout
	// are the campaign watchdogs the run set (empty: the campaign's
	// defaults).
	MaxDeaths      int    `json:"max_deaths,omitempty"`
	LevelTimeout   string `json:"level_timeout,omitempty"`
	EpisodeTimeout string `json:"episode_timeout,omitempty"`
	// MinModelShare and MaxStaleRate are the gate thresholds the run asked
	// for (0: the defaults; Gate holds the ones applied).
	MinModelShare float64 `json:"min_model_share,omitempty"`
	MaxStaleRate  float64 `json:"max_stale_rate,omitempty"`
	// EntryCommands are the client commands sent at every level entry
	// (cheats such as god: a test run, not a benchmark).
	EntryCommands []string `json:"entry_commands,omitempty"`
	// Trace is the decision events' state detail (full, digest,
	// every:N); Record whether a .dm2 was written per level attempt.
	Trace  string `json:"trace,omitempty"`
	Record bool   `json:"record"`
	// ReplayTrace is the recording a replay backend answered from.
	ReplayTrace string `json:"replay_trace,omitempty"`
}

// MockConfig is the configuration of a mock run's fake Jev server.
type MockConfig struct {
	// Policy is "noisy" or "scripted" (a clean model); empty in a trace
	// recorded before the policy was.
	Policy string `json:"policy,omitempty"`
	// Noise is the noisy policy's noise in effect (nil for the scripted
	// policy).
	Noise *MockNoise `json:"noise,omitempty"`
	// Faults are the server's injected faults as run_start records them
	// ("seed=5 server=0.02 missing=0.02 ...").
	Faults string `json:"faults,omitempty"`
}

// MockNoise is the noisy mock policy's noise in effect (q2bot -mock-noise,
// -mock-swap, -mock-lowconf; 0: that effect is off).
type MockNoise struct {
	// Noise is the largest share of probability mass spread at random.
	Noise float64 `json:"noise"`
	// Swap is the chance that the top two options swap.
	Swap float64 `json:"swap"`
	// LowConfidence is the chance of a confidence in [0.05, 0.3].
	LowConfidence float64 `json:"low_confidence"`
}

// BudgetConfig is a run's budget (q2bot -budget-usd, -budget-queries,
// -max-qps, -on-exhausted).
type BudgetConfig struct {
	USD         float64 `json:"usd,omitempty"`
	Queries     int     `json:"queries,omitempty"`
	MaxQPS      float64 `json:"max_qps,omitempty"`
	OnExhausted string  `json:"on_exhausted,omitempty"`
}

// clone returns a deep copy of c (nil for nil).
func (c *RunConfig) clone() *RunConfig {
	if c == nil {
		return nil
	}
	d := *c
	d.Maps = append([]string(nil), c.Maps...)
	d.EntryCommands = append([]string(nil), c.EntryCommands...)
	if c.Mock != nil {
		m := *c.Mock
		if c.Mock.Noise != nil {
			n := *c.Mock.Noise
			m.Noise = &n
		}
		d.Mock = &m
	}
	if c.Budget != nil {
		b := *c.Budget
		d.Budget = &b
	}
	return &d
}

// EpisodeSummary is one episode (episode.json).
type EpisodeSummary struct {
	Index   int            `json:"index"`
	Seed    uint64         `json:"seed"`
	Outcome string         `json:"outcome"`
	Reason  string         `json:"reason,omitempty"`
	GameMs  int64          `json:"game_ms"`
	Totals  Totals         `json:"totals"`
	Levels  []LevelSummary `json:"levels"`

	// Ticks is the episode's per-tick provenance (nil without lane tick
	// or provenance events).
	Ticks *TickStats `json:"ticks,omitempty"`
}

// LevelSummary is one level attempt sequence (from level_start to
// level_end; a death reload stays on the level).
type LevelSummary struct {
	Lvl         int    `json:"lvl"`
	Map         string `json:"map"`
	Visit       int    `json:"visit"`
	Outcome     string `json:"outcome"` // trace.Outcome*, or "incomplete"
	Reason      string `json:"reason,omitempty"`
	StartGMs    int64  `json:"start_gms"`
	TimeMs      int64  `json:"time_ms"`
	CombatMs    int64  `json:"combat_ms"`
	Deaths      int    `json:"deaths"`
	Reloads     int    `json:"reloads"`
	DamageTaken int    `json:"damage_taken"`
	BotKills    int    `json:"bot_kills"` // kills the bot perceived
	Stuck       int    `json:"stuck"`
	// level counters of the game (metrics only)
	Kills        int32 `json:"kills"`
	Monsters     int32 `json:"monsters"`
	Secrets      int32 `json:"secrets"`
	TotalSecrets int32 `json:"total_secrets"`
}

// Totals add up levels and events.
type Totals struct {
	Levels          int   `json:"levels"`
	LevelsCompleted int   `json:"levels_completed"` // exit or victory
	Deaths          int   `json:"deaths"`
	Reloads         int   `json:"reloads"`
	DamageTaken     int   `json:"damage_taken"`
	BotKills        int   `json:"bot_kills"`
	Stuck           int   `json:"stuck"`
	Kills           int32 `json:"kills"`
	Monsters        int32 `json:"monsters"`
	Secrets         int32 `json:"secrets"`
	TotalSecrets    int32 `json:"total_secrets"`
	CombatMs        int64 `json:"combat_ms"`
}

func (t *Totals) add(o Totals) {
	t.Levels += o.Levels
	t.LevelsCompleted += o.LevelsCompleted
	t.Deaths += o.Deaths
	t.Reloads += o.Reloads
	t.DamageTaken += o.DamageTaken
	t.BotKills += o.BotKills
	t.Stuck += o.Stuck
	t.Kills += o.Kills
	t.Monsters += o.Monsters
	t.Secrets += o.Secrets
	t.TotalSecrets += o.TotalSecrets
	t.CombatMs += o.CombatMs
}

// Provenance counts where the values of a decision field came from.
type Provenance struct {
	Model    int `json:"model"`
	Scripted int `json:"scripted"`
	Reflex   int `json:"reflex"`
	Stale    int `json:"stale"`
	Other    int `json:"other,omitempty"`
	// ModelShare is Model over all values.
	ModelShare float64 `json:"model_share"`
}

func (p *Provenance) total() int { return p.Model + p.Scripted + p.Reflex + p.Stale + p.Other }

func (p *Provenance) add(o Provenance) {
	p.Model += o.Model
	p.Scripted += o.Scripted
	p.Reflex += o.Reflex
	p.Stale += o.Stale
	p.Other += o.Other
}

func (p *Provenance) finish() {
	p.ModelShare = ratio(p.Model, p.total())
}

// DecisionStats summarizes the decision events.
type DecisionStats struct {
	// Decisions counts the request decision events (lanes fast and slow:
	// one answered or failed request each); Ticks the bot's lane tick
	// events, which are not requests (their provenance is in
	// RunSummary.Ticks).
	Decisions int                   `json:"decisions"`
	Ticks     int                   `json:"ticks,omitempty"`
	Fields    map[string]Provenance `json:"fields"`
	All       Provenance            `json:"all"`
	// GateModelShare is the model share over the gate fields target,
	// fire_policy and mode.
	GateModelShare float64 `json:"gate_model_share"`
	// Comparisons counts model answers that came with the scripted
	// policy's answer; Disagreements those that differed.
	Comparisons      int     `json:"comparisons"`
	Disagreements    int     `json:"disagreements"`
	DisagreementRate float64 `json:"disagreement_rate"`
}

// APIStats summarizes the backend requests (api_call).
type APIStats struct {
	Calls   int `json:"calls"`
	OK      int `json:"ok"`
	Errors  int `json:"errors"`
	Retries int `json:"retries"`
	// Stale answers arrived after their TTL; StaleRate is over OK answers.
	Stale     int            `json:"stale"`
	StaleRate float64        `json:"stale_rate"`
	ByStatus  map[string]int `json:"by_status,omitempty"`
	// LatencyMs are percentiles of the OK answers.
	LatencyMs    Percentiles `json:"latency_ms"`
	InputTokens  int64       `json:"input_tokens"`
	OutputTokens int64       `json:"output_tokens"`
	CostUSD      float64     `json:"cost_usd"`
	// CombatQPS is the OK answers during combat per second of combat.
	CombatCalls int     `json:"combat_calls"`
	CombatQPS   float64 `json:"combat_qps"`
}

// Percentiles are nearest-rank percentiles.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// BudgetState is the last budget event.
type BudgetState struct {
	SpentUSD     float64 `json:"spent_usd"`
	LimitUSD     float64 `json:"limit_usd"`
	RateHz       float64 `json:"rate_hz"`
	ScriptedOnly bool    `json:"scripted_only"`
	Reason       string  `json:"reason,omitempty"`
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
