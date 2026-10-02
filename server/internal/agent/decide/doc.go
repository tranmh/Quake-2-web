// Package decide is the agent's decision layer: it projects the fair
// belief of package worldmodel into compact per-lane states, asks typed
// questions about them (Jev's noul / choice / score primitives), schedules
// the requests to a DecisionBackend without ever blocking the control loop,
// and arbitrates the answers into an Intent for the controller.
//
// There are two lanes:
//
//   - fast (combat): me, the top enemies, incoming projectiles and the free
//     space around the bot; questions target, fire_policy and movement; up to
//     FastHz (10 Hz) while enemies or projectiles exist.
//   - slow (strategy): the fast state plus items, the objective, level
//     stats and recent events; questions mode, weapon, pickup and danger;
//     every SlowInterval (500 ms, 2 Hz) and early on events (an event is
//     kept until an accepted slow request carries it, so one that comes
//     while the lane's slot is busy is asked as soon as it frees).
//
// The flow per tick is Pipeline.Tick: project → Scheduler.Want → NewRequest
// → Arbiter.Observe (the scripted fallback answers the same request at
// once) → Scheduler.Submit → Scheduler.Collect → Arbiter.Apply →
// Arbiter.Intent.
//
// The arbiter does not trust a model's answers blindly: per field it
// accumulates them as evidence, each weighted by its confidence and
// decayed with its age, and decides from the posterior (see
// ArbiterConfig.Tau); it falls back to the scripted policy only when the
// evidence is weak or every answer has expired. How much one answer
// counts it learns from the model: per field it estimates how often the
// model's confident answers are lone swaps, takes a model that does not
// blip at its newest answer (accumulation would only delay it) and makes
// a noisy one's change wait for confirming answers (ArbiterConfig.Confirm,
// TrustBelow).
//
// Fairness: everything here reads only the belief (worldmodel), the bot's
// own state inside it, and static map knowledge (the collision model for
// SpaceProbe, a path function over the nav graph). It never sees server
// state; TestImports enforces that the package does not link the server,
// the game or the session.
//
// Determinism: the projections, questions, arbiter and the lockstep
// scheduler depend only on their inputs (no map iteration order, no wall
// clock), so a lockstep run with a deterministic backend repeats exactly.
package decide
