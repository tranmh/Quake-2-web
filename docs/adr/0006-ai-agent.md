# ADR 0006-ai-agent

Status: accepted

An AI agent plays the single-player demo campaign of this port (demo1 → demo2 → demo3 → demo2 again →
`victory.pcx`) as a headless client of the Go server. A pluggable decision backend makes its choices: Jev,
TypeSafe's System One model, is the primary one. The agent sees only what a player at the same screen and
speakers could perceive. It can be watched live in the browser, measured headlessly and replayed. The plan is
[`docs/plans/0002-ai-agent.md`](../plans/0002-ai-agent.md). How to run it is in [`docs/AGENT.md`](../AGENT.md),
and the measured results are in [`docs/AGENT-EVAL.md`](../AGENT-EVAL.md).

## Context

- Jev answers typed questions (`choice`, `noul`, `score`) about a JSON state at about 10 Hz, with 70–500 ms
  of latency. It is billed per input token.
- A bot that cheats (reads the server's entities, the game's edicts or the level counters) would prove nothing
  about the model. So would a bot whose wins come from code rather than from its decisions.
- The porting rules (`docs/PORTING.md`) forbid behaviour changes in the ported packages (`sv`, `game`, `net`,
  `qcommon`, `pmove`, `cmodel`, `world`, `bsp`, `web/packages/q2-*`).

## Decision

### Architecture

```
lockstep sv.Server (virtual clock)  ─┐                                    ┌─ jev (HTTP, pinned model)
  or host.Instance (realtime)        │ netchan over a pipe, "loopback"    │  mock (jevtest fake server)
                                     ▼                                    │  scripted · replay · constant · random
                 fakeclient.Client ──OnServerMessage──► spectate.Stream ──► demo.Recorder (.dm2 per level attempt)
                        │                                     └─► Mirror ─► Hub ─► WS /ws/v1/bots/{id}/watch
                        ▼
 perception (FOV+LOS, audibility) ─► worldmodel (belief) ─► decide (lane states, questions, Scheduler, Arbiter)
                        │                                                     ▲                   │
 mapdata ─► nav graph ─► navrt (A*, follower, MapState) ◄── route table ◄─ campaign               ▼
                        └────────────► control (aim, MoveIntent, reflexes, fire gate) ◄──── Intent
                                                │ usercmds every 25 ms + string commands
 trace.Bus ─► trace.jsonl.gz · run.json (metrics) · WS /ws/v1/bots/{id}/decisions · Prometheus q2bot_*
```

- **Client.** The bot is `server/internal/fakeclient`, a Go port of the client's network and parsing code.
  Its new hooks are opt-in. With zero `Options` it behaves exactly as before: `Feed`, `Tick`, `BeginConnect`,
  `Options.Clock`, `OnServerMessage`, `Passive`/`NewPassive`/`FeedPayload`, `MaxHistory`, `TempEntEvents`,
  `MuzzleFlashes` and `RequestFullFrame`. The [PARITY](../PARITY.md) TODO-IMPROVE log lists them.
- **Hybrid control.** The model chooses intents: the target, the fire policy, movement relative to the target,
  the mode (fight / objective / pickup / retreat / explore), the weapon, the item to pick up, and a danger
  score. Code carries them out every 25 ms:
  - aim slew and leading;
  - path following on the nav graph;
  - the reflexes (fire gate, no splash at close range, no fire with a neutral or a barrel in the line, dodge,
    weapon dry);
  - mover waits and directional triggers.
  Answers arrive late, so the model chooses latency-tolerant policies, not usercmds.
- **Two lanes.** The fast (combat) lane runs up to 10 Hz while enemies or projectiles are known. The slow
  (strategy) lane runs at 2 Hz and early on events. Each lane gets its own compact, pre-bucketed state of about
  1 KB. Requests never block the control loop.
- **Backends.** The `decide.DecisionBackend` implementations are:
  - `jev` (the real API);
  - `mock` (the real jev HTTP client against an in-process fake server with a `scripted` or `noisy` policy);
  - `scripted` (the deterministic baseline, also the arbiter's fallback);
  - `replay` (recorded answers);
  - `constant` and `random` (ablations).
  The arbiter checks answers and accumulates them per field as time-decayed (`ArbiterConfig.Tau`, by
  field), confidence-weighted evidence. The posterior decides a field while its newest answer is within its
  TTL, its weight is at least `MinConfidence` and, for a choice, its top option is at least `MinPosterior`.
  Otherwise the field falls back to the scripted policy (reason `weak` for too little or split evidence). A
  value held by hysteresis stays model-sourced while the posterior still backs it. The arbiter records the
  provenance of every field the bot acted on: model, scripted, stale, reflex or default.
- **Map knowledge.**
  - A nav graph is built from the BSP by simulating the bit-exact `pmove` for every candidate edge. The edges
    carry conditions (mover poses, removed walls) and effects (the triggers they pass through). It is cached
    in `assets/nav`.
  - Per-visit route tables are checked in (`fixtures/agent/routes`). `q2nav plan` derives and validates them
    against the entity lump. There is no general logic planner.
- **Death.** A death reloads the level-entry autosave `save0`, which every `gamemap` writes in single player.
  The death is counted, and a per-level cap ends the episode.
- **Recording.** One client-format `.dm2` is written per level attempt by `internal/demo`. That package ports
  `CL_Record_f`/`CL_WriteDemoMessage`/`CL_Stop_f` under the PORTING rules. The browser replays the files with
  the existing `CL_PlayDemo`.

### Fairness rules

Everything the bot knows comes from one of the following:

1. **What its client received:**
   - packet entities (already filtered by the server's PVS);
   - the player state and `STAT_*`;
   - configstrings;
   - prints, layouts, temp entities, muzzle flashes, sounds and the inventory.
2. **What a player would perceive of that:**
   - An entity enters the belief only when it is inside the field of view and has a line of sight from the
     eye. The field of view is `player_state_t.fov`, from the `fov` userinfo, at a 4:3 aspect.
   - The line of sight is a `MASK_OPAQUE` trace against the world and the frame's brush entities, on the
     bot's own `cmodel.State`. Glass blocks shots but not sight. Monster bodies do not occlude. An occluding
     brush is itself visible.
   - Sounds, looping sounds, muzzle flashes and explosions count only when the client mixer would play them
     above zero volume (`S_SpatializeOrigin`, attenuation, `SOUND_FULLVOLUME`).
   - A sound whose emitter is not in the packet has no position. It is accepted only if it is `ATTN_NONE`, or
     if the emitter's last perceived position is within earshot. It never moves a track.
   - Entity numbers identify an entity across frames, standing in for a player's visual re-identification.
3. **Its own state:**
   - Damage bearing comes from the change in `kick_angles` on frames where `STAT_FLASHES` pulses, after
     subtracting the predicted run, bob and fall terms.
   - The inventory and the help computer are read the way a player reads them: `inven` or `help`, each
     followed by `putaway`. At most one pair goes out every 2 s, and none in a fight, except the first
     inventory of a level.
4. **Static knowledge:**
   - the entity lump (movers, triggers, exits, lasers, item spawns), the nav graph and the route tables;
   - MD2 frame names for animation state;
   - class priors (health, damage per second, range) as player knowledge.
   Derived inferences a player could also make:
   - one-way drops come from nav region reachability;
   - a removable wall is assumed gone when the bot stands in a pocket that only it could open;
   - in a pit from which the route's goal cannot be reached, the bot types `kill` (a player command), which
     counts as a death;
   - when the bot has stood within 24 units of one spot for 30 s of game time while its navigator kept
     trying to move it (on at least 75 % of the frames) and reported itself stuck at least 8 times, it is
     wedged and types `kill` too (`bot.wedgeTick`, reflex `wedged_kill`). That also counts as a death and
     reloads `save0`, so it shows in every evaluation's death count.

The game's level counters (`session.Truth`: kills and secrets) feed only `level_end` and `run.json`, for
metrics. They never reach the bot. Without a collision map the perceiver fails closed: it sees nothing and
still hears.

### How the rules are enforced (the proofs)

- **Import guards.** Every agent package that the bot's decisions depend on has a `TestImports`. It parses
  the non-test files and checks their imports against an allowlist; most guards also check the transitive
  dependencies with `go list -deps`. The exact lists are in each `imports_test.go`. The bot's code therefore
  cannot link:
  - `internal/sv`, `internal/game`, `internal/world` or `internal/host`;
  - `internal/api` or `internal/agent/session`;
  - for `bot`, `routeexec` and `navrt`, `internal/agent/nav/navbuild` either.

  This covers `perception`, `worldmodel`, `decide`, `control`, `bot`, `routeexec`, `navrt`, `mapdata`,
  `budget` and every `backend/*`. Only `session`, `campaign` (through the narrow `Control` interface) and
  `runner` drive the server. `spectate` imports no server, game, host, API, agent or demo code.
- **Perturbation invariance** (`worldmodel.TestPerturbationInvariance`). A 60 s demo1 run is recorded and
  replayed through a passive client twice: once verbatim, and once with everything the percept did not admit
  rewritten at random:
  - hidden monsters' states, heard-only fields and brushes outside the frustum;
  - unseen `old_origin`s and packet membership;
  - injected unpositioned sounds from out of earshot.
  The two beliefs must have identical digests at every frame. A control run, in which the *admitted* monsters
  are moved, must change the belief, so the comparison is not blind.
- **`.dm2` differential** (`fairness.TestDifferential`). A lockstep episode is recorded the way its client saw
  it: the `.dm2` holds the server messages verbatim. The trace records every decision tick and usercmd.
  `fairness.Rebuild` then plays the recording into a fresh bot on a passive client. That test binary does not
  link `sv`, `game`, `world`, `host` or `session`, which `go list -deps` checks. The rebuilt bot must make the
  same decisions byte for byte: the same tick events and the same usercmds. The documented normalizations are
  in `server/internal/agent/fairness/doc.go`, for example the attract-loop flag of a `.dm2` header and the
  netchan sequence numbers of a passive client. A bot that peeked at anything outside the recording could not
  be rebuilt from it.

### Lockstep vs realtime

- **Lockstep** (`session.Lockstep`, used for CI, evaluation and `q2bot`):
  - The bot owns an `sv.Server` with a virtual clock and a seeded generator.
  - One `Step` is one 100 ms server frame with four 25 ms usercmds.
  - Backend latency is simulated: an answer is due at `t + SimLatency` of game time, and the simulation waits
    for it.
  - A run depends only on its configuration and seed with the scripted, replay, constant and random backends.
    The trace compares equal except for the `wall` stamps, and `q2bot replay -strict` checks this.
  - A lockstep mock run is also exact, unless a loopback round trip exceeds the client's 800 ms fast-lane
    attempt cap.
  - A full campaign takes about 30 s of wall time for 17 min of game time.
- **Realtime** (`session.InProc`, used for the server's bots):
  - The bot connects to a `host.Instance` over an in-memory `loopback` connection, and `Step` is paced at
    about 100 ms of wall time.
  - Bot games are single-player games owned by the server (OwnerID 0). They are never idle-reaped and are
    outside per-owner game limits. They end when the bot's run ends.
  - A real Jev client is never bit-deterministic: network, wall-clock timeouts, the breaker and cooldowns all
    vary.
- **Shared code.** The perception, decision, control and campaign code is the same in both modes. Only the
  session differs.

### Live relay

`spectate.Hub` is a minimal protocol-34 server per viewer over the ADR-0001 WebSocket transport. The full
design is in `server/internal/spectate/doc.go`.

- **Mirror.** A `Stream` is fed synchronously in the bot's goroutine with every message the bot accepted. A
  `Mirror` keeps a copy-on-write snapshot for each level generation: serverdata, configstrings, baselines and
  the latest frame. A bounded feed never blocks the bot.
- **Join and keyframe.** A viewer runs the server handshake (`SV_New_f`, `SV_Configstrings_f`,
  `SV_Baselines_f`, `SV_Begin_f`) from the snapshot, with `attractloop 1`. On `begin` it gets a **keyframe**:
  the bot's latest frame written as an uncompressed `svc_frame` with the same serverframe number and
  `deltaframe -1`. The msg quantizers are idempotent, so this frame is exact.
- **Forwarding.** From then on the bot's own payloads are forwarded verbatim as unreliable data, minus the
  commands that only make sense on the bot's connection (stufftext, disconnect, reconnect, serverdata,
  download). The bot's next delta applies because the viewer holds the same frame.
- **Reliable data.**
  - Relay-originated reliable data (handshake pages, configstring diffs, `changing`/`reconnect`) always travels
    in a datagram of its own.
  - A forward bigger than one datagram, such as a keyframe in place of a small delta, is split, so the netchan
    never drops it for lack of room.
  - While reliable data is in flight, forwarded configstrings join the reliable stream, so a late
    retransmission cannot overwrite a newer value.
  - Every new bot generation (level change or reload) sends viewers `changing` + `reconnect` on their existing
    netchan. Generations are counted per Stream, so a hub outlives a bot client.
- **Resync and slow viewers.**
  - A viewer is resynced (configstring diff plus keyframe) when the bot deltas from a frame the viewer did not
    get, when the viewer's queue dropped a datagram, when the feed overflowed, or when the viewer reports an
    invalid frame (at most 1/s).
  - The relay waits for the queue to drain, then resyncs. More than 5 drops a minute, or a 10 s stall,
    disconnects the viewer.
  - There are viewer caps per bot, per address and in total, and a per-connection packet rate. Viewer input is
    parsed minimally and fuzzed (`FuzzRelayViewerPacket`).
- **Isolation.** Viewers never perturb the bot. Nothing a viewer sends reaches the bot or the game, and the
  relay asks the bot's server for nothing.
- **Watch endpoint statuses.** 404 unknown bot, 403 ticket, 503 full, 410 hub closed.

Rejected alternatives:

- **An `ss_demo` server instance playing a growing `.dm2`.** All viewers share one read position, and the
  instance kills itself when it runs out of data. A live recording runs out of data all the time.
- **`RequestFullFrame` (`cls.demowaiting`) for keyframes.** It changes what the server sends the bot, which
  perturbs the bot's stream and breaks lockstep determinism. It stays available only as an off-by-default
  recorder option for mid-level attachment.
- **Live client-side demos in the browser.** Recording or streaming from the TS client would mean changing a
  ported `q2-client` file. The browser instead plays the Go-recorded files through the unchanged
  `CL_PlayDemo`.
- **A second player connection to the bot's game.** A single-player game has one client slot, and a spectator
  would be a game entity that monsters can see and target.

### Cost safety

- **CI never calls the paid API.**
  - Inside a test binary (`testing.Testing()`), the jev client refuses every host that is not loopback.
  - CI sets `TYPESAFE_API_KEY: ""`. The mock backend and `jevtest` serve on loopback.
  - The only live job (`jev-live` in `agent-nightly.yml`) is manual (`workflow_dispatch`). It runs behind the
    protected `jev-live` environment, which holds the key. A repo admin creates that environment before the
    first dispatch, with required reviewers and deployment branches limited to `main`: a dispatch runs the
    dispatching ref's copy of the workflow, so without the branch rule an approved dispatch from another
    branch would hand the key to that branch's workflow. GitHub creates an environment that does not exist on
    first reference, with no protection rules (the job then fails on the empty secret).
- **The key.**
  - It is read from the environment only (`TYPESAFE_API_KEY`); no flag takes it.
  - It lives in a `trace.Secret`, which is redacted from logs, errors, traces, the recorder's Authorization
    header and API responses.
  - It is server side only and never in a `NEXT_PUBLIC_*` variable.
  - It is sent only to `https://api.typesafe.ai` unless `JEV_ALLOW_CUSTOM_BASE=1`. A custom base must be
    https unless it is loopback, and redirects are never followed.
- **The model is pinned** (`jev-1.13.0`). Moving aliases such as `jev-latest` are refused.
- **Budgets.**
  - Each run has caps in USD and in queries, and a game-time query rate (`-budget-usd`, `-budget-queries`,
    `-max-qps`).
  - As the tightest budget drains, the fast lane degrades from 10 Hz to 5 Hz below half the budget and to
    2 Hz below a quarter. An exhausted budget means the scripted fallback only, and the run is flagged as not
    model-driven. With `-on-exhausted stop` the run ends instead.
  - In `q2server`, a per-run cap (`Q2_BOT_BUDGET_USD_PER_RUN`, default $1) and a per-UTC-day cap over every
    Jev bot apply. The daily cap (`Q2_BOT_BUDGET_USD_PER_DAY`, default $5) is persisted in the database.
  - A process-wide account limiter (`Q2_JEV_MAX_QPS`) bounds the request rate.
- **Failures and access.**
  - 401/403 disable the client. 429/529 start a cooldown that honours `Retry-After`. Five consecutive
    failures open a breaker.
  - Jev bots are for administrators only. Other accounts may start local-backend bots only with
    `Q2_BOTS_ALLOW_USERS`, and within caps on bots, starts and viewers.
- **Accounting.** Every request's tokens and cost go into the trace (`api_call`), into `run.json` (`api`,
  `budget`) and into `q2bot_api_cost_usd_total`.

## Consequences

- **Ported packages are unchanged** apart from these exceptions:
  - the fakeclient's opt-in hooks;
  - the opt-in `demo.Writer.AllBaselines` and the per-level Recorder (see PARITY, TODO-IMPROVE);
  - one genuine port bug fix found by the agent: `game` `cfmt` now formats C's `%3i` like `printf`, so
    HelpComputer's kill counters are no longer `%!i(...)`.
- **Bounded fairness claims.** The fairness claims are exactly the rules above, and the three tests check
  them. Their stated assumptions are part of the claim: the FOV, that bodies do not occlude, class priors,
  and entity numbers standing in for re-identification.
- **Bounded determinism claims.** Determinism is claimed for lockstep runs with local backends only, on one
  machine and Go version. Nav graphs are byte-stable on one machine and Go version.
  - `game` `func_clock` with spawnflags 0 reads the wall clock. It is absent from demo1–3, so other maps would
    not be deterministic.
- **Other maps need new data.** The route tables are specific to the demo campaign, so another map needs new
  tables (a general planner is out of scope). The nav builder and the runtime are map-agnostic.
- **Cost.** The order of magnitude is $0.53–0.83 per campaign at 212 ms simulated latency: 12.6–19.9 M input
  tokens, by the fake server's token counts (see AGENT-EVAL.md). Live numbers come from `q2bot jev-probe` and the
  `jev-live` job once a key exists.
