// Package runner runs the agent headlessly: Episodes episodes of the
// campaign (or its first visits, Config.Maps) on a session (lockstep for
// evaluation and CI, InProc for realtime watching), with one decision
// backend (scripted, jev, mock, replay, or the constant and random
// ablations), and writes the run directory:
//
//	<OutDir>/<run id>/
//	  run.json                 the run summary (metrics.RunSummary, schema q2bot.run/1)
//	  ep-NNN/episode.json      the episode summary (EpisodeReport)
//	  ep-NNN/trace.jsonl.gz    the episode's trace (agent/trace; it starts with the run's run_start)
//	  ep-NNN/demos/NN-map.dm2  one client demo per level attempt (Config.Record)
//	  ep-NNN/log.txt           the episode's log, stamped with game time
//
// The run id is generated (NewRunID: time and crypto/rand hex), outside
// every decision path.
//
// Per episode the runner builds the bot's Policy: a decide.Pipeline on
// the backend, with the scripted policy as the arbiter's fallback, the
// projector's space probe on the level's collision model, and, when a
// budget applies, the budget gate (package agent/budget) planned before
// every tick. Every collected request is published as a decision event
// (and an api_call event, unless the budget refused it); every session
// step's usercmds as a cmds event; the episode's per-tick provenance (the
// bot's tick reports, with the arbiter's counts beside them) as a
// provenance event. The campaign (package agent/campaign) publishes the
// rest, including the bot's lane tick events (what it acted on each
// decision tick) when it carries them. A metrics.Collector on the bus makes run.json, with the
// provenance gate (Config.MinModelShare, MaxStaleRate): the run is
// ModelDriven only when a model backend answered for the whole run, at
// least MinModelShare of the decided ticks of target, fire_policy and mode
// acted on the model's answer (a reflex or route override is not the
// model's), at most MaxStaleRate of the answers were stale, and none of
// those fields acted on a stale answer on more than MaxStaleRate of its
// decided ticks.
//
// Replay re-runs a recorded lockstep episode, feeding the recorded
// usercmds (actions mode) or letting the bot play on the recorded
// responses (responses mode), and compares it with the recording event by
// event; Summarize recomputes run.json from the traces; Validate checks a
// run directory (trace schema, demos, run.json, the gate).
//
// Determinism: a lockstep run with the scripted, replay, constant or
// random backend depends only on its configuration and seed (the trace
// compares equal but for the wall clock). So does a lockstep mock run,
// whose jev client is set to keep its wall-clock state (breaker,
// cooldowns, refused question sets) out of the answers and lifts the
// fast lane's attempt cap (jev.Config.UncapFast, 30 s attempts on both
// lanes): only a loopback hang longer than 30 s could make it depart from
// the seed's run.
// A real Jev client is not bit-deterministic (network, wall-clock
// timeouts, breaker, cooldown), nor is a run sharing a wall-clock
// budget.Account limiter.
//
// Fairness: the runner drives sessions, so it links the server; the bot it
// builds sees only the belief and static map knowledge (TestImports keeps
// the runner off the game's level counters and the server behind a
// session).
package runner
