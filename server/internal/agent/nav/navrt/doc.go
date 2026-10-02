// Package navrt is the navigation runtime: it moves the bot along the nav
// graph of package nav towards a goal, from what the bot may know.
//
//   - MapState is the belief about the level's dynamic state that edge
//     conditions depend on (doors, plats, trains, walls, lasers), fed only
//     from the world model's fair belief (worldmodel.Belief: mover poses
//     and lasers it saw or heard) and the static map data. Satisfied tells
//     whether a condition holds, or will by waiting and how long (an
//     unknown auto door opens as the bot walks up to it).
//   - Planner is A* over the graph with conditional costs: an edge whose
//     conditions the belief does not meet costs its waiting time when
//     waiting meets them and is left out when nothing would. A condition
//     on the mover an edge starts on counts as met (the bot on a plat's
//     top has the plat up under its feet), and an edge that starts where
//     its mover then stands (the floor of a plat's shaft, for the step onto
//     the plat once it is down) is left out. Goals (Goal) are nodes,
//     points, volumes, touching an entity (any edge whose effects set it
//     off satisfies it: a ride into a trigger, a drop through one), items
//     and shots at shootable entities.
//   - Navigator localizes the bot on the graph and follows the planned
//     path. It pursues plain walks with a lookahead and runs every other
//     edge with the navsim executor the builder validated it with (after a
//     stop at its start where the edge needs one), waits for doors and
//     plats away from their paths and triggers, faces directional
//     triggers, presses and shoots buttons, routes around and climbs over
//     monsters, detects when it is stuck and recovers (jump, strafe, back
//     off, repath, mark the edge blocked with backoff, give up with a
//     cause), and repaths on events and every RepathInterval. Before a jump
//     or drop it simulates the flight in its prediction world from the
//     bot's actual state (and onto a ledge, that the bot comes to rest
//     there, judged in the world the edge was validated in). Entities it
//     must not set off (Config.Avoid, SetAvoid) are left out of the plans
//     and kept clear of by its own steering too: the lookahead's corner
//     cuts, the recovery manoeuvres, the walk back to the graph. Hazard
//     edges (slime, lava) cost a penalty on top of their damage and run
//     with their validated executor. Its output is a control.MoveIntent per
//     usercmd and a Status.
//   - Predictor replays the commands the server has not acknowledged yet
//     with navsim (like CL_PredictMovement), so the follower decides on the
//     position each command will actually run from.
//   - Driver ties them together as a session command function.
//
// Fairness: nothing here reads server state. The inputs are the belief,
// the bot's own player state (and the commands it sent), and static map
// knowledge (mapdata, the nav graph). Tests may cheat; the code may not
// (TestImports keeps the server and game packages out).
//
// Nothing keeps package-level state. A Navigator, Planner, Predictor or
// Driver serves one bot and must not be used by two goroutines at once;
// the graph and map data are shared and immutable.
package navrt
