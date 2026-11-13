// Package cache is the decision cache: it answers a request with an answer
// the backend already gave to an equal (exact) or a similar (approx)
// request of the same run, instead of asking the backend again. It wraps
// any decide.DecisionBackend (jev, mock, ollama) and keeps its name; a
// reused answer is marked (decide.Response.Cache, trace decision "cache"),
// costs nothing, makes no api_call and is not charged by the budget gate,
// and its answers still count as the model's.
//
// # Modes
//
//   - off: no cache (the default).
//   - exact: the key is the lane and the request digest (decide.Request.
//     Digest: the encoded state and the questions, byte for byte).
//   - approx[:fine|coarse]: the key is built from the typed lane state
//     (Request.View) with its raw numbers bucketed or dropped, the map the
//     request was asked on, and every question's id and option keys in
//     order. Option descriptions are left out (the target and pickup ones
//     repeat raw units and bearings); since the option keys are in the
//     key, a reused answer always names options of the new request. "approx"
//     alone is approx:fine.
//
// # The approximate key, field by field
//
// Kept as they are (already buckets, enums or ids), at both granularities
// unless noted: lane; map; questions (ids, option keys, in order); mode;
// me.hp, me.weapon, me.ammo, me.in_water, me.moving, me.weapons, me.empty;
// for every enemy, in rank order: id, class, dist, visible, shootable, aim,
// state, threat, current, objective; incoming: kind, eta, dodge (in order);
// items, in rank order: id, class, gives; objective: kind, desc, stalled,
// exit.
//
// Bucketed or dropped (fine / coarse):
//
//	me.health                  dropped (me.hp is its bucket)        / dropped
//	me.armor                   none, low <50, mid <100, high        / none, some
//	me.on_ground               kept                                 / dropped
//	me.damage_last_1s          none, light <15, heavy <40, severe   / none, hit
//	me.hit_from_bearing        8 sectors of 45°                     / 4 sectors (ahead, left, right, behind)
//	me.target_since_s          none, <1 s, <3 s, <10 s, longer      / none, <2 s, longer
//	enemies[].bearing          8 sectors                            / 4 sectors
//	enemies[].elev             dropped                              / dropped
//	enemies[].units            dropped (dist is its bucket)         / dropped
//	enemies[].heard            kept (near, mid, far)                / heard or not
//	enemies[].wounded          kept                                 / dropped
//	incoming[].bearing         8 sectors                            / dropped
//	space.*                    kept (blocked, tight, open)          / open or not
//	items[].bearing            8 sectors                            / dropped
//	items[].path               distance bucket (close, mid, far)    / distance bucket
//	objective.bearing          8 sectors                            / 4 sectors
//	objective.path             unknown or distance bucket           / unknown or distance bucket
//	objective.next             8 sectors                            / dropped
//	level.kills, .secrets      dropped                              / dropped
//	level.deaths_here          0, 1, 2, 3+                          / 0, 1+
//	events                     in order, without ages and amounts   / the same texts as a sorted set
//
// Event texts lose their age ("1.5s ago") and the amount of damage ("took
// 12 damage from e3" is "took damage from e3"). The 8 sectors are 45°
// wide and centered on ahead, left (90), behind and right (-90) and the
// diagonals; the 4 are ahead (|b| < 45), behind (|b| > 135), left and right.
//
// # Determinism
//
// In a lockstep run the backend runs on goroutines that finish in wall
// clock order, so a cache filled when answers come back would make runs
// depend on the machine. The lookup is made when the request is submitted,
// through the scheduler's latency model (Backend.Latency), the one
// synchronous hook before the backend runs: a hit gets a zero latency (the
// scheduler collects it in the same tick) and is remembered by Seq, so
// Decide returns it without calling the backend. Answers are inserted when
// they are collected (Backend.Record, from the pipeline's OnRecord), in
// game-time order. A request submitted at a tick therefore sees exactly the
// answers collected at earlier ticks. The latency model then depends on the
// cache's state, not only on the request, but that state is itself built
// in game-time order, so the run still repeats. Realtime sessions have no
// submission hook: Decide looks up itself.
//
// # Lifetime and memory
//
// One Store serves a whole run (every episode). Entries are kept least
// recently used first out, at most Config.MaxEntries (DefaultMaxEntries).
// Config.TTL (game time; 0: none) bounds the age of a reused answer: the
// time from the snapshot of the request it answered to the new request's,
// within an episode (an answer from another episode, whose clock started
// over, is too old under any TTL). Only answers without an error that
// answer every question are kept; a reused answer is never stored again.
//
// # Replays
//
// A replay does not use the cache: the replay backend answers a recorded
// hit with the recorded answer and marker (backend/replay), so the replay
// repeats the run event for event.
package cache
