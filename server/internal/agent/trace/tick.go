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
// missing, invalid, unknown_option, weak (the accumulated evidence is too
// weak or split; traces before evidence accumulation carry
// low_confidence instead), ttl, error, timeout, gone, held) or, for a
// reflex, which override:
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
//     target is dead or unknown: none), retarget (the decided target is
//     dead, unknown or none: the most dangerous awake monster in view with
//     a line of fire, fought in a fight, shot back at on the move when it
//     attacks), or the mode's override when it kept the bot from the fight
//     the intent asked for (disengaged, explore_watchdog: none).
//   - fire_policy: route_kill (a hold forced to fire_when_aligned at the
//     route's kill), retarget (likewise at a retarget's monster).
//   - movement, in a fight only: reposition (backed off after standing
//     still), in_range (an advance within the weapon's range strafes),
//     no_return (an advance down a one-way drop holds), no_cover (a
//     retreat with nowhere to go strafes), keep_off (a drain or melee
//     monster in view within its reach is backed away from at once; a
//     drain monster out of view too, while the bot takes hits without a
//     bearing), low_health (an advance on an attacker in view under 25
//     health sidesteps). The movement vocabulary is advance, retreat,
//     strafe, hold; traces before wave 8 also carry strafe_left and
//     strafe_right.
//   - weapon: dry (the weapon in hand ran dry), splash (a splash weapon
//     at a target or a route shoot goal too close), unusable (the decided
//     weapon is not held or has no ammo: keep).
//
// A decided value the bot had no use for on the tick is Source default
// with the reason, so that it is not credited to the decision: a target
// it neither fought nor shot back at (not_engaged: none; the intent's
// mode is another) and, outside a fight, the movement (not_fighting: nav,
// the navigator moves the bot). The weapon's Value is the weapon acted on
// even without an override (with no decision about it, Source default,
// the bot's choice for the range). Outside the modes that use them the
// pickup and danger fields keep the decided value.
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
	// too), reposition (backed off after standing still in a fight), scan
	// (a hit without a bearing and no target: the bot turns to the nearest
	// awake monster it knows of out of view, else behind it and to the
	// sides), quad (typed "use Quad Damage" for a picked-up quad in a fight
	// with an awake monster in view), disengage (the fight budget ran
	// out), trapped_kill (typed "kill" in a pit), wedged_kill (typed
	// "kill" wedged: on one spot for 120 s while the navigator kept
	// recovering, or for 60 s off the nav graph), stalled_kill (typed
	// "kill" after 250 s without progress).
	Reflexes []string `json:"reflexes,omitempty"`
	// Requests are the requests built this tick, by Req (submitted or
	// dropped at the in-flight cap).
	Requests []uint64 `json:"requests,omitempty"`
	// DroppedCmds counts the usercmds built since the previous tick event
	// that Cmds does not hold (over the bot's bound of 256: frames that
	// did not come).
	DroppedCmds int `json:"dropped_cmds,omitempty"`
}
