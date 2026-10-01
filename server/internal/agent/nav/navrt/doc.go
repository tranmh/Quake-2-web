// Package navrt is the navigation runtime: it moves the bot along the nav
// graph of package nav towards a goal, from what the bot may know.
//
//   - MapState is the belief about the level's dynamic state that edge
//     conditions depend on (doors, plats, trains, walls, lasers), fed only
//     from the world model's fair belief (worldmodel.Belief: mover poses
//     and lasers it saw or heard) and the static map data.
//   - Planner is A* over the graph with conditional costs: an edge whose
//     conditions the belief does not meet costs its waiting time when
//     waiting meets them (an auto door opening as the bot walks up) and is
//     left out when nothing would.
//   - Navigator localizes the bot on the graph and follows the planned
//     path: it runs each edge with the navsim executor the builder
//     validated it with, waits for doors and plats, faces directional
//     triggers, presses buttons, detects when it is stuck and recovers,
//     and repaths on events and every RepathInterval. Its output is a
//     control.MoveIntent per usercmd.
//   - Predictor replays the commands the server has not acknowledged yet
//     with navsim (like CL_PredictMovement), so the follower decides on the
//     position each command will actually run from.
//   - Driver ties them together as a session command function.
//
// Fairness: nothing here reads server state. The inputs are the belief,
// the bot's own player state (and the commands it sent), and static map
// knowledge (mapdata, the nav graph). Tests may cheat; the code may not.
//
// Nothing keeps package-level state. A Navigator, Planner, Predictor or
// Driver serves one bot and must not be used by two goroutines at once;
// the graph and map data are shared and immutable.
package navrt
