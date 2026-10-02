# The AI agent

A headless bot plays the single-player demo campaign: demo1 → demo2 → demo3 → demo2 again → `victory.pcx`.
It perceives only what a player could perceive, and a pluggable decision backend chooses what it does. Jev,
TypeSafe's System One model, is the primary backend; the others are scripted, mock, replay and two
ablations. You can run it from the command line (`q2bot`), watch it live in the browser through `q2server`,
replay its recordings and measure every decision.

- Design and fairness rules: [ADR-0006](adr/0006-ai-agent.md). Plan and phase gates:
  [plans/0002-ai-agent.md](plans/0002-ai-agent.md). Measured results: [AGENT-EVAL.md](AGENT-EVAL.md).
- Code: `server/internal/agent/*` (see `doc.go` of each package), `server/internal/demo`,
  `server/internal/spectate`, `server/cmd/q2bot`, `server/cmd/q2nav`; web: `web/apps/web/src/{app/bots,
  app/watch,components/bots,components/game/AiOverlay.tsx,game/*}`; dev overlay: `web/apps/dev`.

## Quick start

```sh
make demo                     # the free demo pak -> assets/demo/baseq2/pak0.pak
make nav                      # nav graphs of demo1-3 + overlay dumps -> assets/nav (optional: built on first use)
make agent-smoke              # scripted + noisy-mock demo1 runs (mock held to the 0.7 gate) + q2bot validate

cd server
go run ./cmd/q2bot run -backend scripted -require-complete -max-deaths 25 -level-timeout 60m   # the whole campaign, lockstep, ~40 s
go run ./cmd/q2bot validate runs/<id>                                 # traces + every .dm2
go run ./cmd/q2bot replay -trace runs/<id>/ep-000/trace.jsonl.gz -strict   # lockstep determinism
go run ./cmd/q2bot run -backend mock -mock-policy noisy -maps demo1 -min-model-share 0.7 -require-complete
```

The whole campaign runs with the limits of [AGENT-EVAL.md](AGENT-EVAL.md) (`-max-deaths 25 -level-timeout
60m`). With the defaults (5 deaths and 20 min of game time per level) it fails: every demo3 death reloads the
arrival save, and seed 1 dies there 6 times, in 18.5 min, before it gets through.

That command prints a summary like this one (scripted, seed 1; the numbers move with every change to the bot,
and the measured ones are in [AGENT-EVAL.md](AGENT-EVAL.md)):

```
run 20261002T162713Z-d134c59b: completed (1 of 1 episodes to victory.pcx)
  backend     scripted, lockstep session, skill 1, seed 1, maps demo1,demo2,demo3,demo2
  episodes    1: 4 of 4 levels done, 6 deaths, 160 kills seen, 1554.3 s game (876.3 s in combat), 41.2 s wall
  api         12660 calls (12660 ok, 0 errors, 0 stale), latency p50/p95/p99 0/0/0 ms, 13.7 QPS in combat, ...
  gate        not model-driven: backend "scripted" does not query a model; ...
```

To watch a bot live, start the server with bots enabled, then open the web app (see
[Bots in q2server](#bots-in-q2server)):

```sh
cd server && Q2_BOTS_ENABLED=true Q2_BOTS_ALLOW_USERS=true Q2_COOKIE_SECURE=false \
  Q2_DEMO_PAK=../assets/demo/baseq2/pak0.pak Q2_CORS_ORIGINS=http://localhost:3000 \
  go run ./cmd/q2server -bot demo1,demo2,demo3       # -bot also starts a public server-owned bot
cd web && pnpm --filter web dev                       # http://localhost:3000/bots
```

## Game data, nav cache and route tables

- **Game data.** Every tool reads the demo pak at `assets/demo/baseq2/pak0.pak` (`make demo`). Override it
  with `-pak` or `$Q2_BASEDIR` (`<dir>/baseq2/pak0.pak`). Tests skip without it.
- **Nav cache.** The graphs are gzipped JSON, named `assets/nav/<map>-<checksum hex>-v<format>-<physics
  hash>.json.gz`.
  - Override the directory with `$Q2_NAV_DIR` (or `q2bot -nav-dir`, `q2nav -nav`/`-out`).
  - The physics hash covers `nav.FormatVersion` (1), `nav.BuildVersion` (2), `navsim.Version` (2), the pmove
    version and the build parameters. Bump `BuildVersion` or `navsim.Version` whenever the builder's or the
    simulator's output changes. Map data changes are caught by the graph's recorded scene.
  - A missing or stale graph is rebuilt on first use: about 5–8 s per map, single-flight per map.
    `campaign.Run` preloads the campaign's maps before it connects, so no level entry waits for a build.
  - Under `-race` a build takes about 30 s per map, so the full-map nav tests skip under `-race` without a
    cache. CI runs `make nav` first.
  - Graphs are byte-stable on one machine and Go version.
- **Route tables.** These are `fixtures/agent/routes/{campaign.json,demo1.json,demo2a.json,demo3.json,
  demo2b.json}`; `$Q2_ROUTES_DIR` (or `q2bot -campaign`) overrides the directory. The schema and validation
  rules are in `fixtures/agent/routes/README.md`.
  - **Optional steps and detours.** A goto, press or wait step, or a pickup of an item that is not a key, may
    carry `"optional": true`: equipment the route does not need. An optional step may name its `"detour"`.
    Adjacent optional steps with the same name (or none) are one detour, so two detours next to each other
    need different names.
  - **Their validation.** The last step cannot be optional, only optional steps name a detour, and a named
    detour's steps are adjacent. The table must also validate without its optional steps, so no required step
    relies on one, and, when it has several detours, without any one of them, so every detour stands alone.
    `q2nav plan` marks these steps `(optional)` or `(optional, detour <name>)`.
  - **The demo detours.** demo1 `shotgun` (the shotgun and 2 boxes of shells near the start), demo2a
    `machinegun` (button `*36` opens the closet door `*35`: the machinegun and 2 boxes of bullets), and demo3
    `quad` (the quad damage) and `chaingun` (the chaingun and 2 boxes of bullets). Every demo3 attempt starts
    from the arrival save, so the bot tries the quad and the chaingun again on every attempt.

## Inside an episode

Each package's `doc.go` describes it in detail; this is the overview.

- **Campaign** (`campaign.Run`).
  - **Level detection.** The arrival of `svc_serverdata` (the client's `LevelGen`) marks a new level. Once the
    client is active, the campaign loads the map data (checked against `CS_MAPCHECKSUM`), the nav graph and
    the route table of the map's visit, then enters the bot. The level memory is checkpointed per (map,
    visit).
  - **Death.** A death (health ≤ 0, `PM_DEAD` or `PM_GIB`) is followed after 1.5 s of game time by
    `load save0`, and counted.
  - **Cinematics and victory.** A `.cin` gets `nextserver <spawncount>`. A `.pcx` with `PlayerNum -1` is the
    victory.
  - **Arrival check.** Every arrival is checked against the exit the left level's route claims (level and
    spawnpoint); any other arrival ends the episode with `ErrUnplannedExit`.
  - **Watchdogs** (game time): level 20 min, episode 90 min, 5 deaths per level, 10 s without a frame. After
    120 s without progress the bot explores for 30 s; after 300 s the level fails as `stalled`. Progress is a
    route step done, the current step's path getting shorter, or a monster the bot fought killed. The bot
    gives up a stalled attempt itself at 250 s (see the bot's `kill` recoveries below), before this watchdog
    would end the run.
  - **Mid-campaign starts.** `Config.EntryCommands` (the `-cheats`) are re-sent at every level entry and must
    be idempotent. `Config.Visits`/`StopAfter` start or stop mid-campaign.
- **Perception and belief** (`perception`, `worldmodel`; the rules are ADR-0006's).
  - **Sight.** An entity enters the belief when it is in the field of view with a line of sight from the eye.
    A track's position (`Pos`) and velocity (`Vel`) come from sightings only.
  - **Hearing.** A sound or muzzle flash out of view gives a cue, never a position: the stereo balance the
    client's mixer renders (`perception.Pan`: hard left, left, center, right, hard right; ahead and behind
    sound alike) and its loudness against the sound's own level (`perception.Loudness`: near, mid, far), with
    the sound's class and time. A heard explosion keeps its type and cue; a door's or plat's sound admits the
    brush's pose. A loop sound is the blend the mixer makes of every entity with that sound
    (`S_AddLoopSounds`): one hearing per sound, with the cue of the sum and no entity number.
  - **Placing a track by ear** (`worldmodel/ear.go`). Each track keeps the world yaws its recent cues allow and
    narrows them with each cue within 1.5 s: a turn of the bot between two sounds tells front from back.
    `Track.Ear` holds the last cue, the arc (`Yaw` ± `Spread`, `Ambiguous` while ahead and behind both fit)
    and a stand-in `Est`: the middle of the arc at the middle distance of the loudness step.
    `Track.Loc` is where the bot believes the track is: in view its position, out of view its last position
    seen while the sounds agree with it, otherwise `Est` (`LocSeen` false). For a monster never seen, the
    entity lump's spawns of the voice's family that fit the sound (bearing in the arc, the same loudness step,
    not in plain view, not the spawn of a monster seen) pick the arc, and when exactly one fits `Est` is that
    spawn origin (`Ear.AtSpawn`). A track is tied to its lump entity (`Track.Lump`) only once seen, never by
    the entity number of a sound, so a kill step for a monster never seen goes for its spawn origin, and
    takes a monster of its family that hearing places at that spawn (`Ear.AtSpawn`) for it. A hit
    whose bearing the view kick gives narrows the arc of the track it is attributed to. Aiming, turning,
    keeping away, the scan, retreats, damage attribution and a kill step's look and firing position use
    `Loc`; the navigator's obstacles and the line-of-fire checks use positions seen only.
- **Route executor** (`routeexec`).
  - Each step becomes a navigator goal. It is judged done from the belief: a mover pose, a laser gone, a
    track dead, an item taken, the inventory. A directional touch counts only while the bot faces along the
    trigger's movedir for a server frame.
  - Exit steps are held until the level changes. With no change within 3 s of reaching the goal, the exit is
    tried again.
  - A step is retried up to 3 times. A contradicting `wait`/`confirm` sends the bot back to the step before
    it.
  - The avoid set is the table's `avoid` plus the activators of every other exit.
  - When a later step has no path for 2 s, the executor assumes unseen claimed effects (a door opened
    elsewhere, a removed wall).
  - `Yield` pauses a step's clock while the bot fights or explores. `Engage` lets a kill step's own fight run
    on that step's clock.
  - An optional step gets one attempt and no retry. When it fails (no path, a timeout, its effects not seen),
    the executor skips it together with the rest of its detour: the pending optional steps right after it with
    the same `detour` name, such as the wait for a closet door and the pickups behind it. Detours are skipped
    one at a time, so a detour that follows under another name is still tried. An optional pickup of a weapon
    the bot already holds is skipped on its own, and the rest of its detour (the ammo) goes on. A skip counts
    as route progress.
- **Navigation runtime** (`nav/navrt`).
  - **Inputs.** The fair belief, the bot's own player state and the commands it sent, and static map
    knowledge.
  - **Planner.** A* with conditional edge costs. An edge whose condition only waiting meets costs the wait;
    an edge nothing would meet is left out.
  - **Execution.** The Navigator follows plain walks with a lookahead. Every other edge runs with the navsim
    executor it was validated with, which stops first when the edge needs to start from rest. The Navigator
    also waits for doors and plats, faces directional triggers, and presses or shoots buttons.
  - **Stuck recovery** escalates: jump, strafe (strafe before jump against monsters), back off, repath, then
    mark the edge blocked for 15 s × 2^k (capped at 120 s), then give up with a cause.
  - **Hazards.** Hazard edges (slime, lava) cost a 20 s penalty plus their damage.
  - **Debugging** in the live tests: `Q2_NAVRT_TRACE=1` (status changes), `Q2_NAVRT_TRACECMD=1` (every
    command), and `Q2_NAVRT_WALK=demo1:7:50` with `Q2_NAVRT_TRACE_AT=k` (`TestWalkTrace`: replay one random
    walk and trace target k).
- **Bot** (`bot`).
  - On every server frame it decides on (a decision tick), the bot's `Policy` gives it an Intent. The policy
    is a `decide.Pipeline` on the backend, with the scripted policy as the arbiter's fallback. Every 25 ms
    usercmd then executes that Intent.
  - The bot executes the Intent's mode (fight, objective, pickup, retreat, explore) with the navigator and the
    reflexes:
    - a fire gate on a shootable line and the aim tolerance;
    - no splash weapon closer than 150 units;
    - no shot with a neutral or a barrel in the line, including the spread cone;
    - dodging and repositioning;
    - weapon switches when dry;
    - in a fight, backing away at once from a drain monster (the parasite) within 304 units or a melee-only
      one (such as the berserker) within 160, whatever the Intent's movement (`keep_off`). The monster is in
      view, or, for a drain monster, was seen or heard awake in the last 2 s while hits arrive without a
      bearing (a drain does no knockback). One only heard has no distance: it counts when it sounded near,
      and the bot backs away from its stand-in;
    - under 25 health, no advance on an attacker in view within 600 units: it sidesteps instead
      (`low_health`);
    - when the Intent's fight target is dead, unknown or none (the next answer is a latency away), it fights
      the most dangerous awake monster in view with a line of fire within 1000 units. On the move with no
      live target in the Intent, it shoots back at such a monster when that monster attacks. When the Intent
      holds fire it fires when aligned (target and fire_policy `retarget`);
    - a hit with no bearing (a drain, a hit without knockback) while the bot has no target turns it towards
      the nearest awake monster out of view that it saw or heard within 5 s and 700 units (one only heard:
      towards the side it sounded from, at its stand-in). Without one, it
      turns behind itself, then to one side and the other on later hits. A scan under way is not restarted
      (tick reflex `scan`);
    - on the move with no target, it looks where an attacker only heard sounded from (its stand-in) while
      that attacker attacked within the last 1.5 s and sounded near or mid, so it sees the attacker as soon
      as it shows; the path's movement does not depend on the view (`heardAttacker`);
    - a picked-up quad damage is used (`use Quad Damage`) the first time the bot then fights an awake monster
      in view (tick reflex `quad`). In single player the game only stores a picked-up quad
      (`Pickup_Powerup`), and it lasts 30 s from its use.
  - It reads the inventory and the help computer as described in ADR-0006.
  - It types `kill` (a death, then `load save0`) when it cannot go on:
    - in a pit the route's goal cannot be reached from, for 12 s (`trapTick`, reflex `trapped_kill`);
    - wedged: within 24 units of one spot for 120 s while its navigator was under way on at least 75 % of the
      frames and recovered at least 8 times (stuck reports plus repaths), or for 60 s off the nav graph with a
      goal (`wedgeTick`, reflex `wedged_kill`);
    - after 250 s without progress as the campaign counts it, 50 s before the campaign's no-progress
      watchdog would end the run (`stallTick`, reflex `stalled_kill`).

    At most one such `kill` goes out every 6 s. Each one counts as a death in every evaluation.
- **Jev client** (`backend/jev`).
  - **Concurrency and retries.** The fast lane is never retried, and an attempt is capped at 800 ms (cut at
    the arbiter's TTL in realtime; a lockstep mock run lifts the cap: `jev.Config.UncapFast`, 30 s attempts on
    both lanes). A slow-lane attempt gets 1.2 s and one retry within the scheduler's 2.5 s deadline.
  - **Error handling.**
    - 400/422 refuse that question-set shape locally for 1 min and count toward the breaker.
    - 401/403 disable the client.
    - 429/529 back off, honouring `Retry-After`.
    - Five consecutive failures open the breaker for 5 s.
  - **Recording.** Timeouts are recorded as `decision.timeout`.
- **Budgets** (`budget`).
  - A budget refusal is recorded as a decision with `err: "budget: …"` and no `api_call`.
  - The query cap can be overshot only by the requests already in flight.
  - The fast lane degrades 10 → 5 → 2 Hz as the budget drains, then the bot runs scripted only.

## q2nav: map knowledge and nav graphs

Run from `server/`, with `-pak ../assets/demo/baseq2/pak0.pak`:

| Command | What it does |
|---|---|
| `q2nav info -map demo1 [-skill 1] [-deathmatch]` | Entity counts and the mover, trigger, exit and laser tables of one map. |
| `q2nav plan [-routes dir] [-skill 1]` | Every exit of every map with the logic chains that fire it. Each route table with what its steps set off. Validates the campaign. Exit 1 on an invalid table. It ends with `all 4 visit tables valid; campaign ends at victory.pcx (pic)`. |
| `q2nav build (-map demo1 \| -all) [-out dir] [-workers n] [-force]` | Builds the nav cache. Up-to-date graphs are kept (`demo1: up to date (3688 nodes, 29267 edges)`). |
| `q2nav verify -map demo1 [-sample 300] [-live 40] [-posed 20] [-seed 1]` | Re-simulates sampled edges with navsim. Runs a sample on a live lockstep server through the fakeclient (`-live`, the level as it starts), plus conditional and touch edges with their blockers posed (`-posed`). Checks that every route step is reachable. Exit 1 when a gate fails. |
| `q2nav dump -map demo1 [-o file.json]` | Compact JSON for the dev overlay (schema `q2nav.dump/2`): nodes `[x,y,z,flags,region]`, edges `[from,to,kind,conditional,flags]`, trigger/item volumes, mover poses, lasers, spawns, static solids, buttons. |
| `q2nav path -map demo1 [-from spawn[:name] \| x,y,z] -to x,y,z \| ent:N` | The cheapest path, with each edge's kind, cost, conditions and effects. `-to ent:419` reaches an edge that sets off lump entity 419 (demo1's exit trigger `*27`). |

`make nav` builds demo1–3 and writes the overlay dumps `assets/nav/<map>.viz.json`. `make nav-verify` runs
`verify -sample 300 -live 100 -posed 50` on each map. Measured: re-simulation 340/322/332 of 340/322/332
edges, live 100/100 bit-exact, posed 50/50, and all route steps covered.

### Dev overlay (`web/apps/dev`, `?nav=1`)

`pnpm --filter q2-dev dev`, then open `http://localhost:5199/?map=demo1&nav=1`.

- **Data.** The vite middleware serves `/nav/<map>.json` from `assets/nav/<map>.viz.json`. Without that file
  it runs `go run ./cmd/q2nav dump` on demand, which builds the graph if needed. It also serves
  `/routes/<file>.json` from the route tables.
- **What is drawn:**
  - nodes as particles, coloured by flag;
  - edges as beams, coloured by kind (walk grey, jump yellow, drop orange, ladder lime, swim blue, ride white,
    touch green); conditional edges are red;
  - trigger, mover and solid boxes, and lasers;
  - the route of the map's first campaign visit: the straight legs between its steps, and a shortest graph
    path between them as a thick white line.
- **Parameters.** `route=demo2b` picks a table (`route=none` disables it). `navlayers=nodes,edges,boxes,route`,
  `navradius=1024` (draw what is that near the camera) and `navmax=3000` (beams) control the rest.
- **Camera and tests.** WASD, space, `c` and the mouse fly the camera. The JSON→overlay conversion is
  `src/navOverlay.ts`, unit-tested in `test/navOverlay.test.ts`. The Playwright render test includes a
  `demo1-nav` view.

## q2bot: run, replay, summarize, validate, jev-probe

Run from `server/` (`go run ./cmd/q2bot …`) or from a built binary. Exit status: 0 ok; 1 the run failed, did
not complete (`-require-complete`), missed the provenance gate (`-min-model-share`), diverged (replay), is
invalid (validate) or has unreadable traces (summarize); 2 a usage error (including a configuration the
runner rejects, and `jev-probe` without a key).

### `q2bot run`

| Flag | Default | Meaning |
|---|---|---|
| `-maps demo1,demo2,demo3` | whole campaign | The campaign's first visits to play: `demo1` alone, or a prefix. |
| `-campaign dir` | `fixtures/agent/routes` | Route tables (`campaign.json` or its directory). |
| `-backend` | `scripted` | `scripted`, `jev`, `mock`, `replay`, `constant` (always the same option of each question), `random` (a seeded random option, answered one-hot with full confidence). |
| `-session` | `lockstep` | `lockstep` (virtual clock, deterministic) or `inproc` (realtime host instance). |
| `-sim-latency 212ms` / `80ms,150ms,300ms` | 212ms for jev/mock, none for local backends, recorded for replay | Simulated backend latency in lockstep: fixed, or drawn per request from the samples by a hash of the seed and the request number. With an inproc mock it delays the fake server's replies. |
| `-mock-policy` | `noisy` | Mock answers: `scripted` (the scripted policy through the real jev client: a clean model) or `noisy`. `-mock-noise 0.3`, `-mock-swap 0.1` and `-mock-lowconf 0.1` tune the noise (0 turns an effect off). |
| `-episodes n`, `-seed n` | 1, 1 | Episode i plays with seed+i. |
| `-skill n` | the campaign's (1) | 0..3. |
| `-out dir` | `runs` | Where the run directory `<id>` is made. |
| `-trace full\|digest\|every:N` | `full` | The lane state in decision events: full, digests only, or every Nth decision full (the others digests). |
| `-record` | true | One `.dm2` per level attempt. |
| `-budget-usd x`, `-budget-queries n`, `-max-qps x` | none | Run budget (USD, requests, requests per second of game time). |
| `-on-exhausted fallback\|stop` | `fallback` | At the end of the budget: scripted only (flagged not model-driven), or end the run. |
| `-account-qps x` | none | Wall-clock rate limit shared by all of the run's calls (an API account; breaks lockstep determinism). |
| `-require-complete` | off | Exit 1 unless every episode completes. |
| `-min-model-share 0.7`, `-max-stale-rate 0.15` | not enforced | The provenance gate: exit 1 unless model-driven. |
| `-max-deaths n`, `-level-timeout d`, `-episode-timeout d` | 5, 20m, 90m | Campaign watchdogs, in game time (`-1`: no death cap). |
| `-cheats god,notarget` | none | Client commands at every level entry: a test run, not a benchmark. |
| `-replay-trace file` | | The recording `-backend replay` answers from. |
| `-pak file`, `-nav-dir dir` | the demo pak, `assets/nav` | Game data and nav cache. |
| `-json`, `-v` | | Print run.json; copy the episode logs to stderr. |

`-backend jev` reads `TYPESAFE_API_KEY` from the environment (never a flag), along with `JEV_BASE_URL`,
`JEV_ALLOW_CUSTOM_BASE` and `JEV_MODEL` (default `jev-1.13.0`). Without a key it exits 2. The evaluation runs
of [AGENT-EVAL.md](AGENT-EVAL.md) use `-session lockstep -skill 1 -seed 1 -max-deaths 25 -level-timeout 60m`.

### `q2bot replay -trace runs/<id>/ep-NNN/trace.jsonl.gz [-mode actions|responses] [-strict] [-out dir] [-json]`

Re-runs a recorded lockstep episode and compares it with the recording event by event (`trace.Comparable`,
which ignores `wall`). It reports the first divergence and exits 1 on one.

- `actions` (default) feeds the recorded usercmds. It proves that the session, server and game repeat.
- `responses` lets the bot play on the recorded backend answers, matched by request number and digest. It
  proves that the whole bot repeats given the model's answers.

Requests still in flight at the end of the episode take the recorded run's simulated latency. `api_call`
status and retry are not compared. Output:
`… (actions mode, strict): no divergence`, then `2697 of 2697 recorded events replayed equal`.

### `q2bot summarize runs/<id> [-dry-run] [-force] [-json]`

Recomputes `run.json` from the traces. On a partial or unreadable trace it exits 1 and keeps the existing
`run.json`, unless `-force`. It also adds the `config` section to a `run.json` written before that section
existed.

### `q2bot validate runs/<id> [-min-model-share x] [-max-stale-rate x] [-json]`

Checks the trace schema and envelopes of every episode, every demo (`demo.Validate`: frame count, map) and
`run.json`, which must equal what the traces recompute. A `run.json` without a `config` section (written
before it existed) is compared without it and gets a warning. With `-min-model-share` it also checks the
provenance gate. It prints `valid` or `INVALID (n errors)` with one line per demo (`ep-000/demos/00-demo1.dm2: 791 frames, 121863 bytes,
maps/demo1.bsp: ok`).

### `q2bot jev-probe [-lane fast|slow] [-n 1] [-out file] [-state lane-state.json]`

Sends real requests to the Jev API with the decide package's golden lane states, or `-state`. It records the
exchange, with the key redacted, to `server/internal/agent/backend/jev/testdata/live-probe.json` (schema
`q2bot.jev-probe/1`): answers, latency, tokens and cost. One request is about a thousand input tokens, well
under a cent. Without `TYPESAFE_API_KEY` it explains itself and exits 2. Review the file before committing it
as a fixture.

## Bots in q2server

Bots run as realtime single-player games of the server's own host on the demo pakset, through
`agent/runner.Manager`.

| Variable | Default | Meaning |
|---|---|---|
| `Q2_BOTS_ENABLED` | false | Turns `/api/v1/bots` on (off: 503 `bots_disabled`). |
| `Q2_BOTS_MAX`, `Q2_BOTS_PER_USER` | 4, 1 | Live bots in total and per account (administrators exempt). |
| `Q2_BOTS_ALLOW_USERS` | false | Accounts that are not administrators may start local-backend bots (`scripted`, `mock`, `constant`, `random`). Jev bots are for administrators only. |
| `Q2_BOT_MAX_VIEWERS` | 8 | Viewers per bot and stream. There are also at most 4 connections per address, 64 video viewers in total, and watch tickets rate-limited to 30/min per account (per IP when anonymous). |
| `Q2_BOT_RUNS_DIR`, `Q2_BOT_RUNS_KEEP` | `./data/bots`, 50 | Run directories, and how many ended runs are kept. At most 10 are one account's; the runs of administrators, the server and the CLI count against the total only. |
| `Q2_BOT_MAX_RUN` | 30m | Wall-clock limit of one run. |
| `Q2_BOT_BUDGET_USD_PER_RUN`, `Q2_BOT_BUDGET_USD_PER_DAY` | 1, 5 | USD caps per model-backed run, and per UTC day over every Jev bot (persisted in the database). |
| `TYPESAFE_API_KEY` | empty (Jev off) | Server side only: never in a `NEXT_PUBLIC_*` variable. Redacted everywhere. |
| `JEV_BASE_URL`, `JEV_ALLOW_CUSTOM_BASE` | `https://api.typesafe.ai`, false | Another base needs the opt-in, and https unless loopback (for example a local system-one-adapter). |
| `Q2_JEV_MODEL`, `Q2_JEV_MAX_QPS` | `jev-1.13.0`, 20 | Pinned model (aliases refused); requests per second of every Jev bot together. |
| `Q2_NAV_DIR`, `Q2_ROUTES_DIR` | repository `assets/nav`, `fixtures/agent/routes` | Nav cache and route tables. |

`q2server -bot demo1,demo2,demo3 [-bot-backend scripted]` enables bots and starts a public bot owned by the
server. An account that is not an administrator starts at most 10 bots per 10 minutes. Bots stop before the
games close on shutdown. `deploy/docker-compose.dev.yml` enables bots for users and mounts `assets/nav` and
`fixtures/agent/routes`.

### HTTP and WebSocket API

| Endpoint | Answer |
|---|---|
| `POST /api/v1/bots` `{backend, maps?, skill?, name?, simLatency?, public?}` | 201 `{bot}`. Login required. 400 `invalid_bot`, 403, 429 `too_many_bots`, 503. |
| `GET /api/v1/bots` | `{bots: [...]}`: public bots, your own, or all for an administrator. Ended runs (including CLI runs in the runs directory) are listed too. |
| `GET /api/v1/bots/{id}` | `{bot}` with `summary` (`run.json`) of an ended run and its `artifacts`. |
| `DELETE /api/v1/bots/{id}` | 204: stops a live bot (owner or administrator). |
| `POST /api/v1/bots/{id}/watch` | `{ticket, wsUrl, decisionsTicket, decisionsUrl, pakset}`: one-time tickets. 409 when the bot is not live; 429 `rate_limited`. |
| `GET /api/v1/bots/{id}/artifacts/{name}` | An allowlisted file of the run: `run.json`, `ep-NNN/episode.json`, `ep-NNN/trace.jsonl.gz` (served with `Content-Encoding: gzip`), `ep-NNN/demos/NN-<map>.dm2`. A live run serves no trace and not its newest demo. |
| `GET /ws/v1/bots/{id}/watch?ticket=…` | The bot's first-person view: a watch-only protocol-34 stream (one binary message per datagram, ADR-0001). 404 unknown bot, 403 ticket, 503 full, 410 ended. |
| `GET /ws/v1/bots/{id}/decisions?ticket=…` | The decision feed (`q2bot.decisions/1`, below). |

- **Statuses.** `starting`, `running`, `finished`, `failed`, `stopped` (DELETE, the wall-clock limit, a
  budget, or shutdown). A CLI run whose `run.json` says `incomplete` and that was written in the last 5 min is
  listed as `running`.
- **Stop.** `DELETE` waits up to 15 s for the run to end. A run that is still ending after that is not an
  error.

### Web pages

- `/bots`: the bot list and a "Start a bot" form (backend, maps prefix, skill, name, the simulated latency of a
  mock bot, and whether anyone may watch it).
- `/watch/[id]`: the live view on the relay stream, with the AI overlay. The viewer is passive: no pointer
  lock, no menu, no config writes, `cl_predict 0`, and the transport takes a fresh ticket on reconnect.
  - The overlay shows the mode, the route objective and step, the target, probability bars per question with
    the chosen option and confidence, provenance badges per field, latency, cost, rate and model share.
  - With a model backend, a red FALLBACK badge marks the fields that fell back from the model: to the
    script, or to the default after a failed request.
  - Live decisions are aligned to the video by server frame.
- `/bots/[id]`: status, summary, provenance table, artifacts, a Stop button, and a replay player. The replay
  player plays a level's `.dm2` with the existing `CL_PlayDemo` and rebuilds the overlay from the episode
  trace. Demo `NN` maps to the trace's level attempts: each `level_start` or `reload` starts the next attempt.

## Run directory

```
runs/<run id>/                      id: <UTC time>-<8 hex>, generated outside every decision path
  run.json                          the run summary (schema q2bot.run/1)
  ep-NNN/episode.json               the episode report: outcome, levels, victory, routes, demos, decide stats
  ep-NNN/trace.jsonl.gz             the episode's trace (schema q2bot.trace/1), starting with run_start
  ep-NNN/demos/NN-<map>.dm2         one client demo per level attempt (a reload starts a new file)
  ep-NNN/log.txt                    the server and bot log, stamped with game time
```

### `run.json` (`q2bot.run/1`, `metrics.RunSummary`)

| Field | Meaning |
|---|---|
| `schema`, `run`, `outcome` (`completed`/`failed`/`aborted`/`incomplete`), `reason` | The verdict, e.g. `1 of 1 episodes to victory.pcx` or `episode 0: 26 deaths on demo3`. |
| `backend`, `model`, `session`, `maps`, `skill`, `seed`, `episode_seeds` | The configuration in short. |
| `config` | The configuration as `run_start` recorded it (`metrics.RunConfig`): `backend`, `model_backend`, `model`, `session`, `campaign`, `maps`, `stop_after`, `skill`, `seed`, `episodes`, `sim_latency` (as `-sim-latency` reads it; `0s`: none) and `sim_latency_ms`, `mock` (`policy` `noisy` or `scripted`, the noisy policy's `noise` in effect as `{noise, swap, low_confidence}`, and the server's `faults`), `budget` (`usd`, `queries`, `max_qps`, `on_exhausted`), the watchdogs `max_deaths`, `level_timeout` and `episode_timeout`, the gate thresholds asked for, `entry_commands` (cheats), `trace`, `record`, `replay_trace`. It never holds a key; a process-wide account limit (`-account-qps`, `Q2_JEV_MAX_QPS`) is not recorded. |
| `model_driven`, `gate` | The provenance gate (below): `min_model_share`, `max_stale_rate`, `basis` (`ticks`), `model_shares` and `stale_shares` per gate field, `tick_stale_share`, `stale_rate`, `model_backend`, `budget_exhausted`, `passed`, `reasons`. |
| `wall_ms`, `game_ms`, `events`, `errors`, `last_error` | Size and time. |
| `totals` | `levels`, `levels_completed`, `deaths`, `reloads`, `damage_taken`, `bot_kills` (what the bot perceived), `stuck`, `combat_ms`, and the game's `kills`/`monsters`/`secrets`/`total_secrets` (metrics only). |
| `decisions` | Request-level counts per field (`model`, `scripted`, `reflex`, `stale`, `model_share`). `decisions` counts requests and `ticks` counts tick events. `comparisons`, `disagreements` and `disagreement_rate` compare the model with the script. |
| `ticks` | Per-tick provenance of the acted-on values per field (`default`, `model`, `scripted`, `stale`, `reflex`), `gate_model_share`, `tick_events`. |
| `api` | `calls`, `ok`, `errors`, `retries`, `stale`, `stale_rate`, `by_status`, `latency_ms` (`p50`/`p95`/`p99`/`max`), `input_tokens`, `output_tokens`, `cost_usd`, `combat_calls`, `combat_qps`. |
| `budget` | `spent_usd`, `limit_usd`, `rate_hz`, `scripted_only`, `reason` (with a budget). |
| `episodes[]` | `index`, `seed`, `outcome`, `reason`, `game_ms`, `totals`, `ticks`, `levels[]` (`lvl`, `map`, `visit`, `outcome`, `time_ms`, `combat_ms`, `deaths`, `reloads`, `damage_taken`, `bot_kills`, `stuck`, game counters). |

**The provenance gate.** A run is model-driven only when all of these hold:

- a model backend (`jev`, `mock`) answered for the whole run, and the budget was not exhausted;
- `target`, `fire_policy` and `mode` each acted on the model's answer on at least `MinModelShare` (0.7) of
  their decided ticks;
- at most `MaxStaleRate` (0.15) of the answers were stale;
- none of these fields acted on a stale answer on more than 0.15 of its decided ticks.

A reflex or route override does not count as the model's. The gate's counts come from the bot's lane tick
events, which record what it acted on. The `provenance` event carries the arbiter's own counts for
`validate`'s cross-check.

### Trace (`q2bot.trace/1`, JSON Lines, gzip)

Every line is an envelope `{v, type, run, ep, seq, wall, gms, lvl, map, sf}`: version 1, the per-run
sequence from 1, wall clock in unix ms (excluded from comparisons), game time in ms, the level index in the
episode, the map, and the bot's latest server frame (`sf`, which aligns events with the video). The event's
own fields follow in the same object.

| type | Fields |
|---|---|
| `run_start` | `schema`, `backend`, `model_backend`, `model`, `session`, `maps`, `skill`, `seed`, `episodes`, `sim_latency_ms`, `budget_usd`, `config` (string map: `campaign`, `stop_after`, `trace`, `record`, `sim_latency`, `entry_commands`, `budget.*`, `gate.*`, `replay_trace`, `level_timeout`, `episode_timeout`, `max_deaths`, `mock.faults`, `mock.policy`; `run.json`'s `config` section is built from it) |
| `episode_start` | `seed` |
| `level_start` | `visit`, `gen` (the client's level generation), `checksum` (CS_MAPCHECKSUM) |
| `decision` | See below. |
| `api_call` | `backend`, `model`, `lane`, `req`, `status` (HTTP, 0 local), `latency_ms`, `input_tokens`, `output_tokens`, `cost_usd`, `err`, `stale`, `retry`, `combat` |
| `cmds` | `step`, `cmds[]` (one session step's usercmds: `msec`, `buttons`, `angles`, `forward`, `side`, `up`, `impulse`, `light`); the input of an actions replay |
| `damage` | `amount`, `health`, `armor`, `bearing` (degrees relative to the view, estimated), `source` (track id) |
| `kill` | `target` (track id), `class`, `weapon` |
| `death`, `reload` | `cause`, `health`; `slot` (`save0`), `deaths` |
| `stuck` | `stage` (jump, strafe, backoff, repath, block, report), `node`, `pos` |
| `budget` | `spent_usd`, `limit_usd`, `rate_hz`, `scripted_only`, `reason` |
| `level_end` | `outcome` (exit, victory, death_limit, timeout, stalled, aborted, error), `reason`, `combat_ms`, the game's counters (metrics only) |
| `provenance` | `ticks`, `fields[]` (`name`, `default`, `model`, `scripted`, `stale`, `reflex`), `arbiter_ticks`, `arbiter[]` |
| `error`, `episode_end`, `run_end` | `msg`, `fatal`; `outcome`, `reason` |

A **`decision`** event has one of three lanes:

- **`fast` or `slow`**: one answered or failed request. Fields:
  - `req`, `snap_gms`, `state` (when logged), `state_digest`, `req_digest` (replay matches by it),
    `questions`, `response` (the backend's raw answer);
  - `fields[]`: `name`, `value`, `source`, `confidence`, `scripted`, `fallback`;
  - `backend`, `model`, `latency_ms`, tokens, `cost_usd`, `combat`, `err`, `timeout`, `stale`.
- **`tick`**: one decision tick of the bot, which happens on each server frame it decided on, alive and
  outside an intermission. Fields:
  - `state_digest`;
  - `intent` (`mode`, `target`, `fire_policy`, `movement`, `weapon`, `pickup`, `danger`, and `fields[]` with
    the provenance of every acted-on value, in the order mode, target, fire_policy, movement, weapon, pickup,
    danger);
  - `tick` (`n`, the executed `mode`, route `step`, `target`, `move`, `weapon`, `switch`, `health`,
    `reflexes`, `requests`, `dropped_cmds`);
  - `cmds[]`: the usercmds since the previous tick, with `seq`, `ack` and `frame`.

  Request-level consumers (replay, per-request provenance) skip tick events. Dead and intermission frames have
  none.

**Sources and fallback reasons.**

- Sources: `model`, `scripted`, `stale` (an answer held past its TTL), `reflex` (a reflex, watchdog or the
  route overrode the decided value) and `default`.
- Fallback reasons: `no_answer`, `not_asked`, `missing`, `invalid`, `unknown_option`, `weak` (the accumulated
  evidence is too weak or split), `ttl`, `error`, `timeout`, `gone`, `held`. Traces from before the arbiter's
  evidence aggregation have `low_confidence` where `weak` is now.
- Reflex reasons per field are documented on `trace.Intent` (`server/internal/agent/trace/tick.go`).

### Decision feed (`q2bot.decisions/1`)

JSON text frames on `/ws/v1/bots/{id}/decisions`, distinguished by `"t"`:

```
{"t":"hello","bot":id,"backend":b,"model":m,"maps":[...]}                  first
{"t":"decision","sf":frame,"lvl":n,"map":m,"mode":..,"objective":..,"target":{"id","class","dist"}|null,
 "questions":[{"id","options":[{"label","p"}],"chosen","confidence"}],"provenance":{field:source},
 "fallback":{field:reason},"action":{"movement","firePolicy","weapon","fire"},"latencyMs":..,"costUsd":..}
                                                                            one per decision tick (~10 Hz)
{"t":"event","kind":k,"sf":..,"lvl":..,"map":..,"data":{...}}             level_start, level_end, death, reload,
                                                                            kill, damage, stuck, budget
{"t":"stats","kills","deaths","decisions","decisionRate","modelShare","staleRate","apiP50Ms","costUsd"}  ~1/s
{"t":"gap","dropped":n}                                                     n trace events lost (viewer behind)
{"t":"bye","status":s}                                                      last: the run ended
```

## Lane states and questions

The decide package projects the fair belief into one compact state per lane. The schema is `decide.State`
(`decide/state.go`), the questions and their options are in `decide/questions.go` and `decide/vocab.go`, and
the goldens are in `server/internal/agent/decide/testdata/*.golden.json`. The lists below are an overview;
the code is the reference.

- **Fast (combat) lane.** It runs up to 10 Hz while enemies or projectiles are known. The state holds `me`
  (hp bucket, health, armor, weapon, ammo bucket, on_ground, damage in the last second, the bearing of the
  last hit, ...), the top `enemies` (id, class, bearing, elev, dist bucket, units, visible, shootable, aim,
  state, wounded, threat, current, heard), the top `incoming` projectiles (kind, bearing, eta, dodge) and
  `space` (front, back, left, right). Questions: `target`, `fire_policy`, `movement`.
- **Slow (strategy) lane.** It runs at 2 Hz and early on events. The state adds the mode, the owned weapons,
  the top items, the objective (route step, path distance and bearing, stalled), level stats and recent
  events. Questions: `mode`, `weapon`, `pickup`, `danger`.
- **Legend.**
  - Bearings are degrees from the view: + left, − right, 0 ahead, 180 behind. `elev` is degrees above (+) or
    below (−) the eye. Units are map units.
  - `hp`: critical < 25, low < 50, ok < 100, full. `dist`: close < 250, mid < 700, far.
  - `space`: blocked < 48, tight < 128, open. `aim`: on (the view ray hits the box), near (within
    max(3 angular radii, 10°)), off.
  - `heard` (near, mid, far: how loud it sounded) marks an enemy out of view that only hearing places. Its
    bearing is then the side it sounded from in steps of 45° (ahead and behind alike until the bot turns),
    `elev` 0, `units` the middle distance of its loudness in steps of 50, `aim` off and `shootable` false.
    Its target option says "only heard".
- **Vocabulary.**

  | Question | Type | Options |
  |---|---|---|
  | `target` | choice | Enemy ids + `none` |
  | `fire_policy` | choice | `hold`, `fire_when_aligned`, `suppress` |
  | `movement` | choice | `advance`, `retreat`, `hold` and the strafe option(s) of `decide.Movement` |
  | `mode` | choice | `fight`, `objective`, `pickup`, `retreat`, `explore` |
  | `weapon` | choice | Owned weapons with ammo + `keep` |
  | `pickup` | choice | Item ids + `none` |
  | `danger` | score | 0..4: safe … critical |

**The arbiter** (`decide/arbiter.go`, `ArbiterConfig`; the design is in `decide/doc.go`) turns the answers
into the Intent, field by field.

- **Evidence.** Each accepted answer is evidence: its confidence (a noul's |2p−1|) × exp(−age/τ), with τ per
  field (`Tau`): 300 ms for target, fire_policy and movement, 1 s for mode, 500 ms for danger, 1.5 s for
  weapon and pickup. The posterior is the weighted mixture over the options still in the latest state (a dead
  target or a taken item is dropped). A weapon `keep` counts for the weapon in hand when it was asked: once the
  bot switched, an older `keep` backs the old weapon, not the new one. Danger is the weighted mean.
- **Decision.** The posterior decides while the newest answer is within its TTL, the evidence weighs at least
  `MinConfidence` (0.35; `MinNoulMargin` 0.2 for a noul) and, for a choice, the top option has at least
  `MinPosterior` (0.4). Otherwise the field falls back to the scripted policy: reason `weak` for too little or
  split evidence (or `ttl`, `error`, `timeout`, ...).
- **Trust and confirmation.** Per choice field the arbiter estimates the model's blip rate: how often a
  confident answer names a new top option that the next answer reverts (a lone swap). The estimate decays over
  the last few hundred answers and outlives a level change. A field under `TrustBelow` (4 %) takes the model at
  its word: one confident answer (top ≥ `ConfirmProb`, 0.6) naming a new option is a change point, the older
  evidence is dropped, and the field follows its newest answer. Otherwise a change needs `Confirm` (2)
  agreeing confident answers, so a lone swap never flips a field and a sustained change takes exactly 2
  answers at any rate.
- **Safety asymmetry.** Danger rises at once to a fresh answer's level and falls only with the evidence. At
  danger ≥ 3.5 (`RetreatDanger`) the newest answer's `retreat` is acted on at once, past the mode hysteresis.
- **Hysteresis and provenance.** Hysteresis applies to the posterior (mode 1.5 s and 0.2 more probability,
  target 0.15, fire_policy and movement 0.4 s, weapon and pickup 0.5 s). A held value stays `model` while its
  answer is within its TTL or the fresh posterior gives it at least 0.4; otherwise it is `stale`.
- **The scripted backend** (`AnswerSource` `scripted`) decides every field from its latest answer alone: its
  answers are exact, not evidence.

The reflexes in `control` and `bot` execute the Intent every 25 ms and always win. Goldens are regenerated
with `cd server && Q2_UPDATE_FIXTURES=1 go test ./internal/agent/decide ./internal/agent/backend/jev`.

## Metrics (Prometheus, `/metrics` of q2server)

| Metric | Labels | Meaning |
|---|---|---|
| `q2bot_runs_active` | | Bots starting or playing. |
| `q2bot_runs_total` | `result` (finished, failed, stopped) | Ended runs. |
| `q2bot_levels_completed_total`, `q2bot_deaths_total` | `map` | Levels left through an exit or won; deaths. |
| `q2bot_kills_total` | | Monsters killed, as the bots perceived it. |
| `q2bot_decisions_total` | `backend`, `source` | Decision fields acted on per tick, by source (model, scripted, stale, reflex, default). |
| `q2bot_decision_latency_seconds` | `backend` | Latency of answered requests (histogram). |
| `q2bot_fallback_total` | `reason` | Fields acted on without the model's answer. Model and reflex are excluded. |
| `q2bot_api_requests_total` | `backend`, `outcome` (ok, stale, error, rate_limited, overloaded) | Backend requests. |
| `q2bot_api_cost_usd_total`, `q2bot_api_tokens_total` | `backend`; `direction` | Spend and tokens. |
| `q2bot_viewers` | | Live video relay connections. |
| `q2bot_relay_resyncs_total` | `reason` (join, delta, drop, feed, client) | Keyframes sent to viewers. |
| `q2bot_trace_events_dropped_total` | `sink` | Trace events a lossy consumer (the decision feed) dropped. |

## Determinism scope

- **Exact repeats.** A lockstep run with the `scripted`, `replay`, `constant` or `random` backend depends
  only on its configuration and seed. The traces compare equal except for `wall` (`q2bot replay -strict`,
  `cmd/q2bot TestGate`). So do the `.dm2` files: `demo.TestFixtureDemo1Walker` regenerates the committed
  fixture byte for byte.
- **Mock runs.** A lockstep `mock` run is exact too: its jev client keeps the breaker, cooldowns and refused
  question sets, which run on the wall clock, out of the answers. It also lifts the fast-lane cap
  (`jev.Config.UncapFast`) and uses 30 s attempt timeouts on both lanes, so only a loopback hang longer than
  30 s could make it depart from the seed's run. Several mock runs may therefore share a machine (the nightly
  plays three seeds at a time).
- **Budgets.** A budgeted lockstep run repeats too, because the requests to refuse are planned from game time
  before each tick. The wall-clock `-account-qps` limiter is outside that guarantee.
- **Not deterministic.**
  - A real Jev client: network, wall-clock timeouts, breaker, cooldown.
  - `inproc` runs: realtime pacing.
  - Maps with `func_clock` spawnflags 0, which reads the wall clock. These are not in demo1–3.
- **Scope of the guarantees.** Nav graphs and recordings are byte-stable on one machine and Go version. Go's
  `math` gives the same float results on amd64 (CI pins `GOAMD64=v1`).

## Tests

- **Everything.** `make test` (or `cd server && go test ./...`) runs everything that does not need
  `Q2_AGENT_LONG`. Tests that need the demo pak skip without `make demo`; `make nav` makes the `-race` runs
  cover the full-map nav tests.
- **Main gates.** These run in CI:
  - `route.TestCheckedInTablesValid` and `q2nav TestPlanValidatesCheckedInTables`: the route tables against
    the pak.
  - `worldmodel.TestPerturbationInvariance` (beliefs, lane states and scripted answers unchanged when hidden
    state and the origins of heard-only emitters are rewritten within their cues; a redraw that keeps every
    cue must keep the whole percept) with `TestHeardOnlyOriginsInvariance` (the same for a synthetic fight
    full of heard monsters), `TestPerturberCatchesPerceptLeaks` (planted percept leaks are caught),
    `TestHeardNumbersDoNotPlace` (swapping two heard monsters' entity numbers changes nothing),
    `fairness.TestDifferential` and `TestImports` in every agent package.
  - `campaign.TestCampaignGod` (victory with god/notarget) and `campaign.TestCampaignScripted` (no cheats;
    under `-race` only with `Q2_AGENT_LONG=1`).
  - `cmd/q2bot` `TestGate` (run, validate, replay -strict, summarize on demo1) and `TestMockGate` (the gate's
    mechanism: a mock demo1 run held to `-min-model-share 0.99` must exit 1, and `validate` must pass it at
    0.01 and fail it at 0.99).
  - `make agent-smoke` (the go job): a scripted demo1 run, then a noisy mock demo1 run with
    `-require-complete -min-model-share 0.7`, both validated. This is the mock-Jev share that CI checks.
  - `spectate.TestRelayMatchesBot`, `cmd/q2server TestE2EBotWatch`, and `demo.TestFixtureDemo1Walker`.
- **Long tests.** `Q2_AGENT_LONG=1` adds:
  - the seed sweeps (`TestCampaignScriptedSeeds`, `TestCampaignGodSeeds`, `navrt TestWalkSeedsLong`);
  - the realtime InProc runs (`TestInProcDemo1`, `TestInProcDemo1Brain`);
  - a mock campaign (`runner TestRunMockCampaignReflex`, without `-race`);
  - under `-race`, the tests that otherwise skip there (`TestCampaignScripted`, `TestVisits`,
    `TestBuildDeterministic`, the replay and live nav gates).

  The nightly `long-tests` job runs the long suite in two steps (the CPU-bound tests with `-skip TestInProc`,
  then the realtime `TestInProc*` runs with `-p 1`, so wall-paced runs do not share the CPUs with the seed
  sweeps), plus a `-race` pass of the campaign and navbuild gates. Its timeouts are estimates until a first
  dispatched run is timed.
- **Web.**
  - `pnpm -r test`: vitest. It covers the watch transport, decision feed, AI store and overlay, and the replay
    feed in `apps/web`, and the nav overlay in `apps/dev`. The dev package also runs its Playwright render
    tests there.
  - `make e2e` runs Playwright: `play.spec.ts` and `watch.spec.ts`, which starts a scripted bot, watches it
    live, stops it and replays it. `Q2_E2E_REQUIRE=1` turns its self-skips into failures.
- **CI** (`.github/workflows/ci.yml`, a read-only `GITHUB_TOKEN`: `permissions: contents: read`). The go job
  sets `TYPESAFE_API_KEY: ""` and runs `make demo nav` and `make agent-smoke`, then `go test -race ./...`. The
  `e2e` job runs both Playwright suites with `Q2_E2E_REQUIRE=1`. The `nightly-scripts` job tests the nightly
  report's gates on synthetic artifacts (`.github/scripts/test_agent_nightly_report.py`).
- **Nightly** (`.github/workflows/agent-nightly.yml`, also read-only). Every run is a full lockstep campaign
  at skill 1 with `-max-deaths 25 -level-timeout 60m`, the protocol of AGENT-EVAL.md. The matrix job of each
  entry plays its seeds (`.github/scripts/agent_nightly_seeds.sh`, three at a time), checks each exit code and
  runs `q2bot validate` on every run; the `report` job (`.github/scripts/agent_nightly_report.py`) writes the
  table to the job summary and checks the gates across runs.

  | Entry | Seeds | Gate |
  |---|---|---|
  | `scripted` | 1 | `-require-complete`, `validate`, `replay -strict` |
  | `mock-clean` (`-mock-policy scripted`, 212 ms) | 1–6 | every run passes the provenance gate (`-min-model-share 0.7`, in the run and in `validate`); at least 5 of 6 reach `victory.pcx` |
  | `mock-noisy` (`-mock-policy noisy`, 212 ms) | 1–6 | every run passes the provenance gate; at least 5 of 6 reach `victory.pcx` |
  | `ablate-constant`, `ablate-random` | 1 | exit 1 (`-require-complete`), and measurably worse than scripted: fewer levels done, or as many with more deaths |
  | `latency-100ms`, `-400ms`, `-500ms` (clean mock) | 1 | none: data points (212 ms is `mock-clean`) |

  **Why several seeds, and why 5 of 6.**
  - A run is decided on demo3. With up to 26 attempts there, it wins with probability about 1 − (1 − s)^26
    for a per-attempt demo3 survival s. Which seeds win changes with any change to the bot, so the gate
    asks for a share of six seeds instead of a victory on each.
  - The thresholds come from AGENT-EVAL.md's wave-8 rates, pooled over the seeds of one build. Clean mock:
    80/199 (40 %, run-level 95 % interval 34–47 %) over seeds 1–80. Noisy mock: 50/171 (29 %, 24–37 %) over
    seeds 1–50. Every one of those 130 runs won, seeds 1–6 of both included.
  - The hearing hardening leaves them standing (AGENT-EVAL.md, "Fairness hardening: hearing"): all 60 mock
    runs of that build won, at demo3 survival 40/155 (26 %, clean, seeds 1–40) and 20/65 (31 %, noisy, seeds
    1–20). The pooled survival in the job summary should read about those rates now.
  - With no loss in 80 (clean) and 50 (noisy) runs, a per-run loss rate above 3.7 % and 5.8 % is ruled out at
    95 %. Even at those bounds, an unchanged bot misses 5 of 6 with probability 1.8 % (clean) and 4.3 %
    (noisy). At the survival rates alone the chance is under 10⁻⁵. 6 of 6 would catch more, but the runs
    cannot back it: at the same bounds an unchanged bot would miss it 20 % and 30 % of the time.
  - What 5 of 6 catches:
    - a fall back to the wave-7 rates (demo3 survival 6.4 % clean, 3.7 % noisy; win rates about 0.82 and
      0.62): 29 % (clean) and 73 % (noisy) of the time. The earlier thresholds (3 and 2 of 6, from those
      wave-7 rates) caught that 1 % and 3 % of the time;
    - a win rate of 0.5 (demo3 survival about 2.6 %): 89 %;
    - a win rate of 0.2 (survival under 1 %): over 99.8 %.
  - A victory gate only sees a collapse. A clean survival halved to 20 % still wins 99.7 % of runs. Smaller
    regressions show only in the pooled demo3 survival of the job summary (below).
  - The runs are deterministic, so a red gate repeats on the same commit: it is a change's draw, not a
    night's. When AGENT-EVAL.md's rates move, re-derive `min_wins` in the workflow's matrix.
  - The job summary also gives each entry's victories and pooled demo3 survival with its counts, which says
    more than either gate. Its attempts are a visit's deaths plus the attempt that exited or was under way
    when the level ended; a visit lost at the death cap holds 26 deaths and 26 attempts. AGENT-EVAL.md counts
    deaths + 1 for every visit, one attempt more per visit lost at the cap, so its denominators run slightly
    higher than the summary's for the same runs. Its wave-8 *after* rows have no such visit: every one of
    those runs won.

  **Also nightly.**
  - The `long-tests` job and a 60 s `FuzzRelayViewerPacket`.
  - Every run directory is uploaded, with each entry's `matrix.json` (its seeds and gates).
  - A manual `jev-live` job (`workflow_dispatch` with `jev_live`) runs `jev-probe`, then a budget-capped
    `-backend jev` campaign. It uses the `TYPESAFE_API_KEY` secret of the protected `jev-live` environment.
    A repo admin must create that environment before the first dispatch, with required reviewers and
    deployment branches limited to `main`. A dispatch runs the dispatching ref's copy of the workflow, so
    without the branch rule an approved dispatch from another branch hands the key to that branch's edited
    workflow. An environment that does not exist is created on first reference with no protection rules (the
    job then fails on the empty secret, but the environment stays unprotected).

## Troubleshooting

- **`game data: open …pak0.pak`**: run `make demo`, or pass `-pak` or `Q2_BASEDIR`.
- **The first run of a map pauses for several seconds.** The nav graph is being built. Run `make nav` once.
  Under `-race`, nav tests skip without the cache.
- **`q2nav plan` exits 1.** A route table no longer matches the pak or its rules. The message names the step;
  see `fixtures/agent/routes/README.md`.
- **`-backend jev needs TYPESAFE_API_KEY`** (exit 2): set the key in the environment, never as a flag. Inside
  `go test` the jev client refuses every host that is not loopback, by design.
- **`ErrCustomBase` / `invalid base URL`.** A `JEV_BASE_URL` other than the default needs
  `JEV_ALLOW_CUSTOM_BASE=1`, and https unless loopback.
- **`provenance gate not met`** (exit 1 with `-min-model-share`): see `run.json` `gate.reasons`. A field below
  the share, too many stale answers, or an exhausted budget each fail the gate.
- **`unplanned exit`.** The bot left through an exit its route does not claim, for example demo2's `*43` back
  to demo1. That is an episode failure; the trace and the level's `.dm2` show where.
- **The bots API answers 503.** Set `Q2_BOTS_ENABLED=true`. A 403 on start means users need
  `Q2_BOTS_ALLOW_USERS`, and Jev is for administrators only. A 429 means the bot, start or watch limits.
- **`/watch` stays black or reconnects.** The watch ticket is one-time and lives 60 s, and the page takes a
  new one on reconnect. Behind a proxy set `Q2_TRUST_PROXY=true`, so that per-address limits do not lump all
  viewers together.
  A 410 means the bot ended. Open `/bots/[id]` for the replay.
- **The replay has no overlay for a level.** The trace is not served while a run is live. Stop the run, or
  wait for it to end.
- **A web e2e test skips.** It printed why: no Chromium, no Go, no demo pak, or bots off. Run it with
  `Q2_E2E_REQUIRE=1` to fail instead. For a Chromium revision mismatch, run
  `npx playwright install chromium` in `web/apps/web`.
