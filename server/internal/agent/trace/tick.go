package trace

// Decision lanes (Decision.Lane). A decision event is one of two kinds:
//
//   - lane fast or slow: one answered (or failed) request of a decision
//     lane: its state digest (and the full state when logged), the
//     questions, the raw response, how each answer was taken (Fields:
//     model, scripted fallback, default, with the reason), latency,
//     tokens and cost. Replay matches these by Req and ReqDigest.
//   - lane tick: one decision tick of the bot (a server frame it decided
//     on): the digest of the fast lane state projected that tick
//     (StateDigest, the full state in State when logged), the requests
//     built that tick (Tick.Requests; their answers arrive as lane fast or
//     slow events), the Intent acted on with the provenance of every field
//     (Intent.Fields), what the bot executed (Tick), and the usercmds sent
//     since the previous tick (Cmds, with the netchan numbers they were
//     sent with). Tick events carry no Req and no top-level Fields:
//     request-level consumers (replay, the per-request provenance counts)
//     skip them.
const (
	LaneFast = "fast"
	LaneSlow = "slow"
	LaneTick = "tick"
)

// Intent is the decision layer's Intent a tick acted on. Fields gives
// every field's provenance, in the order mode, target, fire_policy,
// movement, weapon, pickup, danger: Value is the value acted on, Source
// where it came from (Source*: model, scripted, stale, default, or reflex
// when the bot's reflexes or its route overrode the decided value),
// Confidence the answer's, and Fallback why the model's answer was not
// used (no_answer, not_asked, missing, invalid, unknown_option,
// low_confidence, ttl, error, timeout, gone, held) or, for a reflex, which
// one (route_kill: the route's kill step; dry, splash: the weapon reflexes).
type Intent struct {
	Mode       string  `json:"mode"`
	Target     string  `json:"target,omitempty"`
	FirePolicy string  `json:"fire_policy"`
	Movement   string  `json:"movement"`
	Weapon     string  `json:"weapon,omitempty"` // "" keeps the current weapon
	Pickup     string  `json:"pickup,omitempty"`
	Danger     float64 `json:"danger"`
	Fields     []Field `json:"fields,omitempty"`
}

// Tick is what the bot did on a decision tick (Decision.Tick of a lane
// tick event).
type Tick struct {
	// N is the tick's number on the level attempt (from 0; a reload
	// starts over).
	N int `json:"n"`
	// Mode is what the bot executed: objective, fight, pickup, retreat,
	// explore, done, dead, intermission.
	Mode string `json:"mode"`
	// Step is the route step's index (-1 without a route).
	Step int `json:"step"`
	// Target is the track the bot fights ("" none), Move the movement it
	// executes relative to it (or "nav" when it follows the navigator).
	Target string `json:"target,omitempty"`
	Move   string `json:"move,omitempty"`
	// Weapon is the weapon in hand (pickup name); Switch the "use" command
	// sent this tick ("" none).
	Weapon string `json:"weapon,omitempty"`
	Switch string `json:"switch,omitempty"`
	// Health is the bot's health this tick.
	Health int `json:"health"`
	// Reflexes are the reflexes and watchdogs that acted this tick or in
	// Cmds, each once, in the order they first acted: dodge, grenade,
	// hold_fire:<reason> (the fire gate held the trigger: hold, neutral,
	// barrel, splash_close, splash_wall, not_visible, no_line, aim),
	// reposition (backed off after standing still in a fight), disengage
	// (the fight budget ran out), trapped_kill (typed "kill" in a pit).
	Reflexes []string `json:"reflexes,omitempty"`
	// Requests are the requests built this tick, by Req (submitted or
	// dropped at the in-flight cap).
	Requests []uint64 `json:"requests,omitempty"`
}
