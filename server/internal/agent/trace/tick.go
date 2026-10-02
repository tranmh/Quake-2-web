package trace

// Decision lanes (Decision.Lane). A decision event is one of two kinds:
//
//   - lane fast or slow: one answered (or failed) request of a decision
//     lane: its state digest (and the full state when logged), the
//     questions, the raw response, how each answer was taken (Fields:
//     model, scripted fallback, default, with the reason), latency,
//     tokens and cost. Replay matches these by Req and ReqDigest.
//   - lane tick: one decision tick of the bot (a server frame it decided
//     on: alive and not in an intermission; the bot decides nothing
//     while dead or frozen, so those frames have no tick event): the
//     digest of the fast lane state projected that tick (StateDigest, the
//     full state in State when logged), the requests built that tick
//     (Tick.Requests; their answers arrive as lane fast or slow events),
//     the Intent acted on with the provenance of every field
//     (Intent.Fields), what the bot executed (Tick), and the usercmds the
//     bot built since the previous tick event (Cmds, with the netchan
//     numbers they were sent with). The idle commands of dead and
//     intermission frames are not traced (no tick event of the level
//     attempt follows them: the campaign reloads or changes level). Tick
//     events carry no Req and no top-level Fields: request-level
//     consumers (replay, the per-request provenance counts) skip them.
const (
	LaneFast = "fast"
	LaneSlow = "slow"
	LaneTick = "tick"
)

// Intent is the decision layer's Intent a tick acted on (the top-level
// values are the decided ones). Fields gives every field's provenance, in
// the order mode, target, fire_policy, movement, weapon, pickup, danger:
// Value is the value acted on (in the option vocabulary: target none,
// weapon keep), Source where it came from (Source*: model, scripted,
// stale, default, or reflex when the bot's reflexes, watchdogs or its
// route overrode the decided value), Confidence the answer's, and
// Fallback why the model's answer was not used (no_answer, not_asked,
// missing, invalid, unknown_option, low_confidence, ttl, error, timeout,
// gone, held) or, for a reflex, which override:
//
//   - mode: route_kill (the route's kill step makes the bot fight its
//     monster), explore_watchdog (the campaign's explore burst),
//     no_threat (a retreat with no threat known), target_gone (a fight on
//     a dead or unknown target), disengaged (a fight past the bot's fight
//     budget), item_unavailable (a pickup of an item not there or given
//     up on), route (an explore while the route has the bot); the value
//     is the mode executed (done: objective).
//   - target: route_kill (the route's kill monster), retreat_threat (the
//     main threat a retreat shoots back at), target_gone (the decided
//     target is dead or unknown: none).
//   - fire_policy: route_kill (a hold forced to fire_when_aligned at the
//     route's kill).
//   - movement, in a fight only: reposition (backed off after standing
//     still), in_range (an advance within the weapon's range strafes),
//     no_return (an advance down a one-way drop holds), no_cover (a
//     retreat with nowhere to go strafes).
//   - weapon: dry (the weapon in hand ran dry), splash (a splash weapon
//     at a target or a route shoot goal too close), unusable (the decided
//     weapon is not held or has no ammo: keep).
//
// The weapon's Value is the weapon acted on even without an override
// (with no decision about it, Source default, the bot's choice for the
// range). Outside the modes that use them the movement, pickup and danger
// fields keep the decided value.
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
	// explore, done (the route is done: waiting for the level to end).
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
	// barrel, splash_close, splash_wall; a route shoot goal's trigger held
	// too), reposition (backed off after standing still in a fight),
	// disengage (the fight budget ran out), trapped_kill (typed "kill" in
	// a pit).
	Reflexes []string `json:"reflexes,omitempty"`
	// Requests are the requests built this tick, by Req (submitted or
	// dropped at the in-flight cap).
	Requests []uint64 `json:"requests,omitempty"`
	// DroppedCmds counts the usercmds built since the previous tick event
	// that Cmds does not hold (over the bot's bound of 256: frames that
	// did not come).
	DroppedCmds int `json:"dropped_cmds,omitempty"`
}
