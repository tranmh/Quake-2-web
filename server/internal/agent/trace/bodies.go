package trace

import "encoding/json"

// Decision field sources (Field.Source): where the value that was acted on
// came from.
const (
	SourceModel    = "model"    // the backend's answer
	SourceScripted = "scripted" // the scripted policy (fallback or scripted backend)
	SourceReflex   = "reflex"   // a reflex overrode the answer
	SourceStale    = "stale"    // a previous answer kept past its TTL
	SourceDefault  = "default"  // no answer was available: the field's default
)

// RunStart is the body of run_start.
type RunStart struct {
	Schema  string `json:"schema"` // Schema
	Backend string `json:"backend"`
	// ModelBackend is true for backends that query a model (jev, mock),
	// false for scripted, replay and ablation backends.
	ModelBackend bool              `json:"model_backend"`
	Model        string            `json:"model,omitempty"` // pinned model id
	Session      string            `json:"session"`         // lockstep | inproc
	Maps         []string          `json:"maps,omitempty"`
	Skill        int               `json:"skill"`
	Seed         uint64            `json:"seed"`
	Episodes     int               `json:"episodes,omitempty"`
	SimLatencyMs float64           `json:"sim_latency_ms,omitempty"`
	BudgetUSD    float64           `json:"budget_usd,omitempty"`
	Config       map[string]string `json:"config,omitempty"`
}

// EpisodeStart is the body of episode_start.
type EpisodeStart struct {
	Seed uint64 `json:"seed"`
}

// LevelStart is the body of level_start (the envelope's lvl and map name
// the level).
type LevelStart struct {
	Visit    int    `json:"visit"`              // visit of this map in the episode, from 0
	Gen      int    `json:"gen"`                // the client's level generation
	Checksum string `json:"checksum,omitempty"` // CS_MAPCHECKSUM
}

// Level outcomes (LevelEnd.Outcome).
const (
	OutcomeExit       = "exit"        // left through an exit
	OutcomeVictory    = "victory"     // the campaign's end
	OutcomeDeathLimit = "death_limit" // too many deaths
	OutcomeTimeout    = "timeout"     // level watchdog
	OutcomeStalled    = "stalled"     // no progress
	OutcomeAborted    = "aborted"     // stopped from outside
	OutcomeError      = "error"
)

// LevelEnd is the body of level_end. The counters come from the game and
// are metrics only (session.Truth).
type LevelEnd struct {
	Outcome        string `json:"outcome"`
	Reason         string `json:"reason,omitempty"`
	CombatMs       int64  `json:"combat_ms"` // game time spent fighting
	KilledMonsters int32  `json:"killed_monsters"`
	TotalMonsters  int32  `json:"total_monsters"`
	FoundSecrets   int32  `json:"found_secrets"`
	TotalSecrets   int32  `json:"total_secrets"`
}

// UserCmd is the usercmd a decision produced.
type UserCmd struct {
	Msec    uint8    `json:"msec"`
	Buttons uint8    `json:"buttons"`
	Angles  [3]int16 `json:"angles"`
	Forward int16    `json:"forward"`
	Side    int16    `json:"side"`
	Up      int16    `json:"up"`
	Impulse uint8    `json:"impulse,omitempty"`
	// Seq is the netchan outgoing sequence the command was sent with and
	// Ack the incoming acknowledged one then (the bot's own client state:
	// its movement prediction replays the commands after Ack). Zero when
	// not recorded.
	Seq int `json:"seq,omitempty"`
	Ack int `json:"ack,omitempty"`
}

// Field is the outcome of one decision field (one question).
type Field struct {
	Name       string  `json:"name"`  // question id: target, fire_policy, mode, ...
	Value      string  `json:"value"` // the value acted on
	Source     string  `json:"source"`
	Confidence float64 `json:"confidence,omitempty"`
	// Scripted is the scripted policy's answer when it was computed for
	// comparison (model/script disagreement).
	Scripted string `json:"scripted,omitempty"`
	Fallback string `json:"fallback,omitempty"` // why the model's answer was not used
}

// Decision is the body of decision: one answered request of a lane (lane
// fast or slow), or one decision tick of the bot (lane tick: Intent, Tick
// and Cmds; see LaneTick).
type Decision struct {
	Lane         string          `json:"lane"` // fast | slow | tick
	Req          uint64          `json:"req"`  // request sequence
	SnapGMs      int64           `json:"snap_gms"`
	State        json.RawMessage `json:"state,omitempty"` // full state (when logged)
	StateDigest  string          `json:"state_digest,omitempty"`
	Questions    json.RawMessage `json:"questions,omitempty"`
	Response     json.RawMessage `json:"response,omitempty"` // the backend's raw answer
	Fields       []Field         `json:"fields,omitempty"`
	Action       json.RawMessage `json:"action,omitempty"`
	Cmd          *UserCmd        `json:"cmd,omitempty"`
	Backend      string          `json:"backend"`
	Model        string          `json:"model,omitempty"`
	LatencyMs    float64         `json:"latency_ms"`
	InputTokens  int64           `json:"input_tokens,omitempty"`
	OutputTokens int64           `json:"output_tokens,omitempty"`
	CostUSD      float64         `json:"cost_usd,omitempty"`
	Combat       bool            `json:"combat,omitempty"`
	// ReqDigest is the digest of the request's state and questions
	// (decide.Request.Digest): replay matches recorded responses by it.
	ReqDigest string `json:"req_digest,omitempty"`
	// Err is the backend's error when the request failed (no Response).
	Err string `json:"err,omitempty"`
	// Timeout: the failure was a timeout (the call's deadline, or a
	// simulated latency beyond it).
	Timeout bool `json:"timeout,omitempty"`
	// Stale: the answer arrived after its TTL and was not applied.
	Stale bool `json:"stale,omitempty"`
	// Intent, Tick and Cmds are set on lane tick events only: the Intent
	// acted on with its provenance, what the bot executed, and the
	// usercmds sent since the previous tick.
	Intent *Intent   `json:"intent,omitempty"`
	Tick   *Tick     `json:"tick,omitempty"`
	Cmds   []UserCmd `json:"cmds,omitempty"`
}

// APICall is the body of api_call: one backend request.
type APICall struct {
	Backend      string  `json:"backend"`
	Model        string  `json:"model,omitempty"`
	Lane         string  `json:"lane"`
	Req          uint64  `json:"req"`
	Status       int     `json:"status"` // HTTP status, 0 for a transport error or a local backend
	LatencyMs    float64 `json:"latency_ms"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Err          string  `json:"err,omitempty"`
	Stale        bool    `json:"stale,omitempty"` // answered after its TTL and dropped
	Retry        int     `json:"retry,omitempty"` // retry number (0: first attempt)
	Combat       bool    `json:"combat,omitempty"`
}

// Damage is the body of damage: health or armor the bot lost.
type Damage struct {
	Amount  int      `json:"amount"`
	Health  int      `json:"health"`
	Armor   int      `json:"armor"`
	Bearing *float64 `json:"bearing,omitempty"` // estimated direction, degrees relative to the view
	Source  string   `json:"source,omitempty"`  // perceived attacker track id
}

// Kill is the body of kill: an enemy the bot killed (as the bot perceived it).
type Kill struct {
	Target string `json:"target"` // track id
	Class  string `json:"class"`
	Weapon string `json:"weapon,omitempty"`
}

// Death is the body of death.
type Death struct {
	Cause  string `json:"cause,omitempty"`
	Health int    `json:"health"`
}

// Reload is the body of reload: the level-entry autosave was loaded.
type Reload struct {
	Slot   string `json:"slot"`   // save0
	Deaths int    `json:"deaths"` // deaths on this level so far
}

// Stuck is the body of stuck: a stuck-recovery stage.
type Stuck struct {
	Stage string     `json:"stage"` // jump | strafe | backoff | repath | block | report
	Node  int        `json:"node,omitempty"`
	Pos   [3]float32 `json:"pos"`
}

// Budget is the body of budget: the spend state and a rate change.
type Budget struct {
	SpentUSD     float64 `json:"spent_usd"`
	LimitUSD     float64 `json:"limit_usd"`
	RateHz       float64 `json:"rate_hz"`
	ScriptedOnly bool    `json:"scripted_only,omitempty"`
	Reason       string  `json:"reason,omitempty"`
}

// Error is the body of error.
type Error struct {
	Msg   string `json:"msg"`
	Fatal bool   `json:"fatal,omitempty"`
}

// EpisodeEnd is the body of episode_end.
type EpisodeEnd struct {
	Outcome string `json:"outcome"` // completed | failed | aborted
	Reason  string `json:"reason,omitempty"`
}

// RunEnd is the body of run_end.
type RunEnd struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}
