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
