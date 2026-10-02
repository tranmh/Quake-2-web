// Package routeexec executes a route table (package route) on one level:
// the ordered steps of one visit (go somewhere, touch a trigger, press or
// shoot a button, ride a mover, wait, face, kill a monster, pick up an
// item, confirm an effect) are turned, one at a time, into goals for the
// navigation runtime (navrt.Navigator) and judged done from what the bot
// may know: the navigator's status, the world model's fair belief
// (worldmodel.Belief: mover poses, lasers, tracks, items, the inventory)
// and static map knowledge (mapdata, the nav graph).
//
// An Executor owns the navigator while the bot pursues the objective: it
// sets the step's goal and the route's avoid set (the table's avoid list
// plus every other exit's activators), retries a step whose navigation
// failed or timed out (MaxAttempts per step), re-attempts the step before a
// wait or confirm whose effects the belief contradicts, and reports a step
// it gave up on as stalled (the campaign's no-progress watchdog decides
// what follows). When the bot uses the navigator for something else
// (exploring, a fight the decision layer chose) it calls Yield; the next
// Update sets the step's goal again.
//
// A kill step needs a weapon: the Directive names the monster (its track
// once seen, its spawn origin until then) and the executor moves the bot to
// a firing position with a line of fire within range; the bot's shoot
// executor aims and fires until the belief shows the monster dead.
//
// Objective returns the current step as a decide.ObjectiveView for the
// decision layer: kind, description, bearing, path distance, the bearing of
// the next waypoint and whether the step has stalled.
//
// Fairness: nothing here reads server state (TestImports keeps the server,
// game and session packages out). An Executor serves one bot on one level
// and is not safe for concurrent use.
package routeexec
