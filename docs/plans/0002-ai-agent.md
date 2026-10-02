# Plan: a Jev-driven AI agent that plays Quake 2 single-player (demo1 → … → victory)

## Status (2026-10-02)

Phases 0–8 are implemented. Phase 9 (live Jev) waits for an API key: the client, `jev-probe` and the manual
`jev-live` nightly job are ready, and only fixture-tested so far. How to run it: [`docs/AGENT.md`](../AGENT.md).
Design and fairness: [ADR-0006](../adr/0006-ai-agent.md). Measurements: [`docs/AGENT-EVAL.md`](../AGENT-EVAL.md).
The plan below is kept as written.

| # | Gate | Result |
|---|---|---|
| 0 | Deterministic seeded lockstep run; its `.dm2` plays in `sv demomap` and the TS client | **Met.** `demo.TestFixtureDemo1Walker` re-records the committed fixture byte for byte; `demo.TestRoundTripSvDemo` plays a recording through `sv demomap`; `q2-client` `dm2-go.integration.test.ts` plays it and matches the `CL_Record_f` header. |
| 1 | `q2nav plan` re-derives and validates all 4 visit tables | **Met.** "all 4 visit tables valid; campaign ends at victory.pcx". |
| 2 | demo1–3 build in under 60 s; sampled edges pass re-simulation and the live server | **Met.** 5–8 s per map. `make nav-verify`: re-simulation 340/340, 322/322, 332/332; live 100/100 bit-exact; posed (conditional and touch) 50/50 per map; every route step covered. |
| 3 | Walk to 50 random nodes on demo1 (god/notarget) | **Met.** demo1 50/50 (demo2 20/20, demo3 19/20); seed sweep 740/750. |
| 4 | Full campaign to `victory.pcx` with god/notarget | **Met.** `TestCampaignGod`: victory in 196.9 s of game time; seeds 1–9 all win. |
| 5 | Scripted bot finishes *without god*; fairness differential green | **Met.** Seed 1 wins (phase-5 record: 4 deaths in 1043 s of game time, 27 s wall; `-max-deaths 25 -level-timeout 60m`). Seeds 1–11 won 11 of 11 in the phase-5 record; the current build wins seeds 1–9 (AGENT-EVAL.md). `fairness.TestDifferential`, `worldmodel.TestPerturbationInvariance` and every `TestImports` pass. |
| 6 | CI mock-Jev demo1 green; nightly noisy-Jev campaign meets the provenance gate; ablations measurably worse | **Met for the provenance gate; completion is reported over seeds.** CI: the go job's `make agent-smoke` runs the noisy mock on demo1 with `-require-complete -min-model-share 0.7` and validates it; `TestMockGate` checks the gate's mechanism (a mock run held to 0.99 must exit 1). The **noisy** full campaign missed the gate before evidence aggregation (fire_policy 0.678 < 0.7). It now passes the provenance gate on every seed (0.82–0.95 of target/fire_policy/mode from the model, ≤ 5 % of ticks stale). Completion is a lottery on demo3: the round-2 build wins noisy seeds 1–5 4/5 and seeds 1–11 7/11 (demo3 survival 7/187), clean seeds 1–3 2/3 (seeds 1–9 7/9), against scripted 9/9 at 0 ms and at 212 ms. The nightly therefore plays six seeds of each mock and gates on the provenance gate of every run plus at least 2 (noisy) and 3 (clean) of 6 victories (AGENT.md, "Nightly"). Ablations are worse: constant fails on demo1 and random on demo3, while scripted wins. Per-seed numbers: AGENT-EVAL.md. |
| 7 | `TestRelayMatchesBot` and `TestE2EBotWatch` green | **Met.** All 8 relay sub-cases pass under `-race`. |
| 8 | `watch.spec.ts` green in CI | **Met locally.** `play.spec.ts` and `watch.spec.ts` pass with `Q2_E2E_REQUIRE=1`. The CI `e2e` job and the nightly workflow are in place and have not run on GitHub yet. |
| 9 | Jev completes the campaign with ≥ 70 % of target/fire_policy/mode from the model, stale ≤ 15 %, 8–10 QPS in combat, cost in `run.json` | **Pending a key.** At the mock models' demo3 survival, one live run's completion would also be a lottery: read AGENT-EVAL.md's [per-attempt survival table](../AGENT-EVAL.md#read-this-first-what-decides-a-run) first. |

Deviations from the plan as written:
- The live relay never resyncs a forward that overflows: relay reliable data never shares a datagram with a
  forward, oversize forwards are split, and only a keyframe that fits no datagram is retried.
- `.dm2` files end at each level transition (PARITY TODO-IMPROVE).
- Damage bearing uses the kick residual, corrected by cos(pitch), and is unknown beyond |pitch| 70°.
- "Vanished while in PVS" is implemented as "its last box is in view and it is not seen".
- Route facts confirmed on the pak:
  - demo1's car `*31` drops 158 units, and the exit fires about 2.0 s after `*34` is pressed;
  - demo2 has a 4th exit, the directional `*39` back to demo3, which is avoided like `*43`;
  - in demo3, `*44` opens the hatch `*20` after 3.4 s and raises the key pedestal `*19` after 6.4 s;
  - in demo2b, `*58` wakes an ambush tank 2.5 s after it fires.

## Context

TypeSafe's post "Introducing System One models and Jev" shows Jev playing Doom.
- **Input:** structured game state as JSON/text, not pixels, about 10 times a second, with 70–500 ms latency.
- **Output:** typed questions that are all answered in parallel.
  - `choice` returns `{choice, probabilities, confidence}`.
  - `noul` returns `{noul: p}`, with no confidence.
  - `score` returns a continuous value plus `{probabilities, legend, confidence}`.
- **API:** `POST https://api.typesafe.ai/v1/systemone {model, state, questions}` with a Bearer key. It is billed per input token ($0.042 per million), and output is free.

We want the same technique in this repo's Quake II port. A Jev-driven agent should play the single-player demo campaign end to end. It must be watchable live in the browser, measurable headlessly, and debuggable.

### Decisions made with the user
| Topic | Decision |
|---|---|
| Model | A pluggable `DecisionBackend` with **Jev as primary**, plus a **scripted** backend (baseline, fallback, CI) and a **replay** backend. There is no key yet, so the client is built against the documented schema and tested with fixtures. CI never calls the paid API. |
| Runtime | A Go headless bot on `server/internal/fakeclient`, in-process. Lockstep for CI and evaluation; host realtime for watching. |
| Goal and scope | A full single-player run of the demo campaign, fighting monsters along the way. |
| Actions | Hybrid. The model picks intents; code aims, follows paths, jumps and holds actions between answers. |
| Perception | **Fair**: only what a player could perceive (in view with line of sight, or heard). |
| Map knowledge | A nav graph precomputed from the BSP with pmove, plus **checked-in per-visit route tables** that `q2nav plan` derives and validates from the entity lump. There is no general logic planner. |
| Death | Reload the level-entry autosave (`save0`) and count the death. |
| Observability | A live over-the-shoulder browser stream, headless metrics, a per-tick JSONL decision trace, and a `.dm2` file per level. |

### Hard constraint: the porting rules (`docs/PORTING.md`)
- Ported packages (`sv`, `game`, `net`, `qcommon`, `pmove`, `cmodel`, and the TS `q2-*` packages) must keep C behavior bit for bit.
- All agent code goes in **new packages**. The only edits to existing code are opt-in, behavior-neutral hooks in `fakeclient`, plus extensions to infrastructure that isn't ported (`host`, `api`, `cmd`, `apps/web`).
- **`server/internal/demo` is itself a port** of `cl_main.c` `CL_Record_f`/`CL_WriteDemoMessage`/`CL_Stop_f`. It follows PORTING.md: the file is `demo/cl_main.go`, it carries `// C:` comments, and the PARITY row for `client/cl_main.c` gets a target update.

### Route facts (from the demo pak's entity lumps; `q2nav plan` re-derives them in phase 1)
Exits are `target_changelevel` entities; Quake 2 has no `trigger_changelevel`. The campaign is **demo1 → demo2 → demo3 → demo2 (second visit) → `victory.pcx`**:
- **demo1:** go through auto doors into the elevator car `*31` and touch button `*34`. The car drops into trigger `*27`, which leads to `demo2$base1`.
- **demo2, first visit:** **avoid** button `*43`, which rides you back to demo1. Touch button `*48` to open a hatch, then drop down the shaft through a mid-air trigger to `demo3$base2a`.
- **demo3:** kill gunner #418, whose death turns off two lethal lasers. Press `*44`, take the **blue key**, and touch the `trigger_key` to open `*42`. Ride plat `*40`, then pass the **directional** exit `*34` facing +x, which leads to `demo2$base3b`.
- **demo2, second visit** (restored from `current`): rotating door, then lever, then walls. Kill the gunner that guards trigger `*58`, then press `*59` to reach `victory.pcx`.

Victory is detected as `PlayerNum == -1` **and** a level name ending `.pcx`. A `.cin` is a cinematic, not the end: the bot answers it with `nextserver <spawncount>`.

---

## Architecture

```
lockstep sv.Server (virtual clock)  ─┐                                   ┌─ Jev HTTP  (pinned model, e.g. jev-1.13)
  or host.Instance (realtime)        │ netchan over MemPipe, "loopback"  │  scripted · replay · mock
                                     ▼                                   │
                 fakeclient.Client ──OnServerMessage──► spectate.Stream ─► Recorder (.dm2 per level)
                        │                                    └─► Hub (keyframes from Mirror) ─► WS /ws/v1/bots/{id}/watch
                        ▼
  perception ─► ObservationFilter (FOV+LOS | heard) ─► worldmodel ─► decide (lane state + questions) ─► Scheduler ─► Backend
                        │                                                     ▲                              │
  mapdata ─► nav (graph, A*, Belief, follower) ◄── route table (per visit) ◄─ campaign                        ▼
                        └──────────────► control (aim ⊕ MoveIntent, reflexes, fire_policy) ◄────────── Arbiter
                                                  │ UserCmd every 25 ms + StringCmds
  trace.Bus ─► trace.jsonl.gz · run.json · WS /ws/v1/bots/{id}/decisions (AiOverlay) · Prometheus (last)
```

## Package layout (new code; ported packages are used read-only)

```
server/internal/agent/
  perception/   classify, anim (MD2 frame names), vision (own cmodel.State), hud, events, ObservationFilter
  worldmodel/   tracker, threat, damage, projectile, inventory, LevelMemory
  mapdata/      entity lump → entities, movers+poses, triggers, exits, lasers; own copy of G_SetMovedir (+oracle test)
  nav/          graph, JSON+gzip cache Store, A*, Belief, Navigator, follower, stuck recovery
  nav/navsim/   collision World + pmove Runner + edge executors (shared by builder and runtime)
  nav/navbuild/ samples → nodes → candidate edges → pmove validation → conditions/effects → regions
  route/        per-visit subgoal tables loader/executor; fixtures/agent/routes/{demo1,demo2a,demo3,demo2b}.json
  decide/       types, lane state projections, questions, Scheduler, Arbiter
  backend/{jev,jevtest,scripted,replay}
  control/      CmdAngles, Compose(MoveIntent, aim), aim slew, weapons, side commands, reflexes
  session/      Lockstep, InProc(host)        (Remote WS: deferred, not needed for this goal)
  campaign/     level detection, nav/route load, death→Reload(), victory, intermission  (uses narrow Control iface)
  runner/       episodes, budgets, watchdogs, run dir, report; Manager implements api.BotHost
  trace/ metrics/
  fairness_test.go   # allowlist import guard over all agent/* except session, runner
server/internal/demo/      demo/cl_main.go: client-format .dm2 Writer/Reader/Validate (ported, PORTING rules apply)
server/internal/spectate/  Stream, Mirror(LevelSnapshot + latest frame), Recorder, Hub/relay, payload filter
server/cmd/q2bot/          run | replay | summarize | validate | jev-probe
server/cmd/q2nav/          build | info | plan | path | verify | dump -json
```

---

## Workstreams

### A. `fakeclient` hooks (opt-in; the zero-value `Options{}` and `Poll` stay byte-identical)
These live in `server/internal/fakeclient/{fakeclient.go,cl_parse.go,cl_ents.go,cl_input.go,cl_tent.go}`.
- **`Feed(datagram)`** is the post-`Recv` body of `readPacket` (the ComError recover, `processPacket`, `Cbuf_Execute`, the disconnect check). `readPacket` is refactored to call it.
- **`Tick()`** = `checkForResend` + `sendConnected`.
- **`Options.Clock`** replaces only `curtime()`, which already feeds Netchan Setup/Transmit/Process.
  - Lockstep must pass an explicit `Qport`, because `New` otherwise seeds it from `time.Now()`.
  - Lockstep runs the handshake itself with `Tick`/`Feed`. `Connect`/`WaitActive`/`Poll` block in `Recv` and cannot drive a single-goroutine server.
- **`Options.OnServerMessage(c, payload, spans)`** runs after `parseServerMessage` and before stuffed commands execute (the `CL_WriteDemoMessage` point), with a `Span{Cmd,Start,End}` per svc command.
- **`Passive` / `NewPassive().FeedPayload()`** parses `.dm2` blocks without executing stufftext.
- **`MaxHistory`** caps the event slices.
- **`TempEntEvents`** keeps temp-entity positions and direction, which `parseTEnt` already reads.
- **`MuzzleFlashes`** records muzzle flashes, which are currently discarded.
- **`RequestFullFrame()`** is the `demowaiting` equivalent, kept only as a relay fallback. It is off by default and never used in lockstep determinism runs.
- **Docs:** fakeclient has **no PARITY row**. Add a TODO-IMPROVE bullet listing these hooks, following the `DropClientOnPanic` precedent, and optionally "+ Go fakeclient" in the Target column of the `client/*.c` rows.
- **Hardening:** fakeclient now ships inside `q2server`, so it gets a hardening pass: bounds checks on server input and no unbounded growth.
- **Tests:** keep `sv_test.go` and `host/oracle_diff_test.go` green unchanged.

### B. Sessions (`agent/session`)
- **Lockstep:**
  - `sv.New(sv.Config{FS: pak, Game: host.RealGame(rng), Rand: crand.New(seed), Clock: vclock, Now: fixed, Dedicated: false, Cvars: ModeSettings(sp)+skill})`, then `ExecuteText("map demo1\n")`.
  - A loopConn carries datagrams. The client is `loopback`, which skips the challenge and is never rate-dropped.
  - Each 100 ms frame: 4 usercmds of 25 ms each → `HandlePacket` → `Frame(100)` → `Feed` the client's datagrams → `Tick`.
  - Bot GameSpecs **forbid physics cvars** such as gravity and airaccel.
- **InProc (realtime):**
  - `games.CreateWithID("bot-<id>", GameSpec{Mode:"sp", Map, Cvars:{skill, password}})` with **OwnerID 0**. Such games are never *idle*-reaped and are outside per-owner limits. They still end when the bot's connection closes or it sends `disconnect`, so the bot never does. Saves live in memory, and client `load` is refused.
  - The bot connects through `inst.ConnectMem("loopback")`.
  - Reload: `inst.Do(func(s *sv.Server){ _ = s.ExecuteText("load save0\n") })`.
- **Reload in both modes:**
  - `gameMap_f` writes `save0` on every level entry because SP is non-dedicated (verified in `host/games.go` `ModeSettings`).
  - `load` goes through `svc_reconnect`, which forces a **full re-handshake** (getchallenge/connect, new Netchan, `ServerCount = Rand()+1`). So `Tick` must keep calling `checkForResend`.
  - Level changes (`gamemap`) instead use `changing`/`reconnect` stufftext on the existing netchan.
- **Remote (WS)** is deferred. UDP and C `q2ded` are only for a protocol smoke test, because the C `q2ded` is dedicated and writes no `save0`.

### C. Fair perception and world model (`perception`, `worldmodel`)
- **Classification:**
  - `CS_MODELS+modelindex` paths map to a class table covering every demo monster, item, weapon, key and projectile. HP, DPS and range are player-knowledge *priors*.
  - Soldier subtype comes from `SkinNum` 0/2/4; an odd skin means "wounded".
  - Animation state (stand/run/attack/pain/death) comes from MD2 frame names via `assets/md2.Parse`.
- **Vision:**
  - The bot owns a `cmodel.State` on the shared immutable `*cmodel.Map`.
  - LOS is a `MASK_OPAQUE` trace, eye to target, against the world plus brush entities (`TransformedBoxTrace`). Shootable uses `MASK_SHOT`.
  - `InPVS` mirrors `SV_BuildClientFrame`, using cluster PVS and areabits.
- **ObservationFilter (the fairness core):**
  - An entity's fields enter the belief only when it is inside the FOV with LOS, or through a sound event (position as of the sound).
  - **Default FOV = the client `fov` userinfo (90°)**, so the browser viewer sees exactly what the bot knows.
  - In-PVS entities that are occluded produce only last-seen data with decay.
  - Mover and laser poses update only when visible or audible (door and plat sounds). Otherwise the belief uses the last known pose plus a timeout.
- **Tracks:**
  - Stable `e*`/`i*` IDs per level and a velocity EMA.
  - Death is inferred from any of: death animation, death sound, `solid==0`, gib, or vanishing while in PVS with an explosion. A medic revive is detected.
  - Awareness comes from muzzle flashes, attack animations and sight sounds.
- **Self state:**
  - Taken from `ps.pmove`, `STAT_*`, the weapon from `CS_MODELS+GunIndex`, and `pm_type`/`STAT_LAYOUTS` for dead and intermission.
  - **Damage bearing:**
    - It comes from the *change* in `ps.KickAngles` on frames where `STAT_FLASHES` pulses (health or armor dropped), with the bot's own predictable run, bob and weapon-kick terms subtracted.
    - It is skipped while firing, and impossible for knockback-free or power-armor-only hits.
    - It is fused with muzzle flashes and sounds.
- **Inventory and objective HUD:** sent as `inven`+`putaway` and `help`+`putaway` pairs, at most one pair every 2 s, never during combat.
- **`LevelMemory`:**
  - It is checkpointed at each level entry, keyed by map and visit index, matching `save0`. A reload restores that checkpoint and keeps only static learnings (blocked edges, death spots).
  - Lump monsters are matched to network entities **by class and spawn origin at level entry**, not by lump index.
  - Prerequisites are confirmed **by their effect** (the laser RF_BEAM is gone while in PVS, a door moved), not only by inferring a kill.

### D. Map knowledge and navigation (`mapdata`, `nav*`, `route`, `cmd/q2nav`)
- **`mapdata`:**
  - Parses the entity lump with `shared.COM_Parse` and applies the skill/deathmatch inhibit filter.
  - Derives mover poses exactly as the spawn functions do, using its **own copy of `G_SetMovedir`** (pure math with an oracle test, so mapdata doesn't import `game`; note the original mutates its argument).
  - Covers lip, START_OPEN, rotating axis, plat `pos2`, button lip 4, train corners, trigger boxes, directional `movedir`, exits and laser segments.
  - **Cross-check test:** `gametest.NewServer(&Scenario{Map, Cvars: skill})`, `SpawnServer`, then compare against `s.E.Ge.Edicts()[i]` (`AbsMin`/`AbsMax`/`Movedir`/`Pos1`/`Pos2`/`S.Origin`).
- **`navsim`:**
  - `Trace` returns **`Ent=0` for world hits** and a non-negative ID for movers, never cmodel's −1; otherwise `PMF_ON_GROUND` is never set.
  - Traces use `MASK_PLAYERSOLID`, and movers are clipped with `TransformedBoxTrace`/`HeadnodeForBox`.
  - `PointContents` ORs in the movers' contents.
  - Runs `pmove` with `AirAccelerate=0` (SP) and `Gravity=sv_gravity`. Touch hooks emulate push and teleport.
- **`navbuild`** (run by `q2nav build`; cached as JSON+gzip at `assets/nav/<map>-<checksum>-<fmtver>-<physicshash>.json.gz`, or built lazily with single-flight):
  - **Nodes:** ground samples on a 32u grid over upward brush-side windings with normal.z ≥ 0.7, settled with a hull trace and one zero pmove step that must land on ground. Plus crouch, water, ladder and mover-top nodes.
  - **Edges:**
    - Candidates come from grid neighbors and ledges. Each is validated by **simulating the bit-exact `pmove`** with the same executors the runtime uses: walk, crouch, jump (with release), drop, ladder, swim, waterjump, ride.
    - Each edge records **conditions** (required mover pose or "gone") and **effects** (triggers it passes through, plus the facing yaw for directional triggers).
  - The build is deterministic: results are merged in sorted order. Estimated size is about 6k nodes per map, built in about 5–15 s on 8 workers (calibrate with the benchmarks).
- **`nav` runtime:**
  - A* with conditional costs, and a `Belief` built from filtered observations.
  - `Tick() → MoveIntent{WishDir, Jump, Crouch, SwimUp, FaceYaw, MustFace}`. The **goal test also accepts an edge whose effects include the goal trigger**, which covers the riding and falling exits.
  - **Follower:** pursuit with a predicted rollout; takeoff, ladder, water and breath; plat and door waits; directional facing.
  - Buttons are pressed by touch or shot, because the game **never reads `BUTTON_USE`**.
  - Stuck recovery escalates: jump → strafe → back off → repath → mark the edge blocked with backoff → report.
- **`route`:**
  - Per-visit subgoal tables in `fixtures/agent/routes/*.json`, as ordered steps like `Touch{trigger *27}` or `Press{*34}`.
    - Movement and actions: `GoTo`, `Touch`, `Press`, `Shoot`, `Ride`, `Wait`, `Face`.
    - Prerequisites: `Kill{class, spawn origin}` and `Pickup{key_blue_key}`.
    - Forbidden zones: `Avoid{*43}`.
    - Guards: `ConfirmEffect{laser off | door open}`.
  - Steps execute through the navigator. Idle time between steps belongs to the brain's `mode` (fight, pickup, explore). A no-progress watchdog falls back to explore.
  - `q2nav plan` derives the tables from the entity-logic chains, prints them and **validates the checked-in tables** against the pak. A general planner for other maps is a possible later follow-up and is out of scope.
- **`q2nav`** also offers:
  - `verify`: re-simulates sampled edges **and executes a sample on the live lockstep server through fakeclient**.
  - `dump -json`: feeds a `?nav=1` overlay in `web/apps/dev` showing nodes, edges, trigger and mover boxes, and the route.

### E. Jev decision layer (`decide`, `backend/*`)
- **Lane-specific state projections.** The docs say irrelevant state hurts accuracy, so each lane gets its own state:
  - **The fast/combat state** holds `me`, enemies (top 4), incoming projectiles (top 2) and `space`.
  - **The slow state** adds the items (top 4), the objective (current route step, path distance and bearing, stalled flag), level stats and recent events.
  - Numbers are **pre-bucketed into categorical tokens** where a decision depends on them (e.g. `dist: close|mid|far`, `aim: on|near|off`, `eta: imminent|soon|none`, `hp: critical|low|ok|full`). Exact bearings stay in degrees, signed per a fixed legend.
  - Structs give a deterministic order. Golden tests check that output is byte-stable and under the cap: a fast state of about 0.5–1 KB, at most 3 KB.
- **Questions:** start with Doom-sized sets of **3–4 per lane**, and add more only when provenance or ablation shows a gain.

  | id | type | lane | options / meaning |
  |---|---|---|---|
  | `target` | choice | fast (≤10 Hz in combat) | dynamic enemy IDs + `none` |
  | `fire_policy` | choice | fast | `hold` · `fire_when_aligned` · `suppress`. **Latency-tolerant**: the reflex layer executes it every 25 ms. |
  | `movement` | choice | fast | `advance` · `retreat` · `strafe_left` · `strafe_right` · `hold` (applied by the controller with hysteresis) |
  | `mode` | choice | slow (2 Hz or on an event) | `fight` · `objective` · `pickup` · `retreat` · `explore` |
  | `weapon` | choice | slow | owned weapons with ammo + `keep` |
  | `pickup` | choice | slow | dynamic item IDs + `none` |
  | `danger` | score | slow | 5 levels, safe…critical. The continuous value drives the query rate and a retreat override. |

  Candidate extensions, added only if measured to help: `dodge` (noul) and speculative `engage_eN` (noul).
- **Decoding** follows the docs: noul → `noul` (no confidence; use |2p−1|), choice → `choice/probabilities/confidence`, score → continuous `score/legend/probabilities/confidence`. Choices are limited to 255 options. Decoding is tolerant, and fixtures are updated after `jev-probe`.
- **`DecisionBackend.Decide(ctx, *Request) (*Response, error)`:**
  - `Request` holds `Seq`, the snapshot time, the raw state JSON, the questions, an option index and an immutable snapshot.
  - `Response` holds the answers, the pinned model ID, latency and usage.
- **The Jev client (`backend/jev`):**
  - Uses `net/http` only, with no new deps. The model is **pinned** (e.g. `jev-1.13`) and recorded in config and the trace.
  - **Concurrency:** in-flight requests = ceil(target_hz × p95), about 4–5; stale answers are dropped. The fast lane gets **no retry**; the slow lane gets one.
  - **Rate limits:** a per-bot token bucket plus a **global account limiter** in the Manager. It honors 429/529 `Retry-After`, uses a circuit breaker, and a 401 disables the client.
  - **Cost** is accounted from `usage`.
  - **Safety:**
    - `JEV_BASE_URL` is env-only. It must be https unless loopback, and the key is never sent to a non-default host unless `JEV_ALLOW_CUSTOM_BASE=1`.
    - A **test-binary guard** refuses non-loopback hosts under `testing.Testing()`.
    - The recorder redacts the `Authorization` header.
- **Other backends:**
  - **Scripted** answers the same questions deterministically: baseline, fallback, CI.
  - **Replay** works in actions mode or responses mode.
  - **`jevtest.FakeServer`** is an httptest server with scriptable latency and faults, plus a **non-scripted "noisy" policy** (perturbed or recorded answers) so CI exercises real model-like disagreement.
  - **`q2bot jev-probe`** records a real exchange into `testdata` once a key exists.
  - For other models, point `JEV_BASE_URL` at TypeSafe's open-source `system-one-adapter`. No separate LLM backend is needed now.
- **Scheduler:**
  - Never blocks the 25 ms control loop. The fast lane runs while there are enemies or projectiles; otherwise only the slow lane runs, at 2 Hz.
  - In lockstep, a response is due at `t + SimLatency` (a fixed or sampled distribution) and the sim waits for it. Backend failures resolve through a sim-time policy and are recorded.
- **Arbiter:**
  - Validates and renormalizes answers, applies confidence gating per field (falling back to scripted) and hysteresis, and drops out-of-order answers.
  - **The TTL counts from snapshot time and is ≥ p95 + 100 ms for fast fields.**
  - **Reflexes always win:** shootable-line and aim-tolerance gate, no splash weapons under 150u, no fire with a neutral in the line, an auto-dodge faster than model latency, and a grenade jump.

### F. Control (`control`)
- **`CmdAngles`:** `ANGLE2SHORT(want) − DeltaAngles`, recomputed every cmd so it survives spawns, teleports and loads.
- **Aim:** exponential slew with a rate cap, aiming at the target's bbox center, leading targets for projectile weapons.
- **`Compose(MoveIntent, aim)`** projects the nav wish direction onto the aim yaw, which allows strafe-running. `MustFace` overrides aim on ladders, takeoffs, directional triggers and buttons.
- **Weapons:** `StringCmd("use <pickup name>")`, debounced and verified by `GunIndex`.
- **Intermission:** press `BUTTON_ANY` after 5.5 s of `PM_FREEZE`. A `.cin` gets `nextserver <sc>`.
- **Msec budget:** the total cmd `Msec` stays within `CommandMsec`.

### G. Campaign and runner (`campaign`, `runner`)
- **Level detection:** a new level is signaled by the **arrival of `svc_serverdata`** (fakeclient's `clearState`). It does not compare ServerCount values.
  - Once active, load mapdata, the nav graph and the route table for `(map, visit index)`, after checking the map against `CS_MAPCHECKSUM`.
- **Death:**
  1. Wait 1–2 s.
  2. Call `Control.Reload()`, a narrow interface implemented by the session (`load save0`).
  3. Increment deaths and restore the `LevelMemory` checkpoint.
- **Victory:** `.pcx` + `PlayerNum == -1`.
- **Watchdogs:** 20 min per level, 90 min per episode, a cap on deaths per level, and a no-progress check (explore after 120 s, fail after 300 s).
- **Budgets:** USD per run and per day (**daily spend persisted in the DB** when running in q2server), a query cap, and a QPS cap. As the budget runs down the rate degrades 10 → 5 → 2 Hz, then falls to scripted-only, and the run is **flagged as "not model-driven"**.
- **`run.json`:**
  - Outcome; per-level time, deaths, kills/secrets and damage taken.
  - **Decision provenance** per field (model / scripted-fallback / reflex / stale), the stale-answer rate, achieved QPS in combat, and the disagreement rate between model and script.
  - API p50/p95/p99, tokens, cost, pinned model, and seeds.

### H. Observability, viewing and API (built in this order)
1. **Trace** (`agent/trace`):
   - **Format:** JSONL.gz with the envelope `{v,type,run,ep,seq,wall,gms,lvl,map,sf}`. The `wall` field is excluded from replay comparisons.
   - **Event types:** `run_start`, `episode_start`, `level_start`, `level_end`, `decision`, `api_call`, `damage`, `kill`, `death`, `reload`, `stuck`, `budget`, `error`, `episode_end`, `run_end`.
   - **Decision events** record the state (full, digest, or every Nth), the questions, the **raw responses**, the chosen action, the usercmd, source and fallback reason, latency, tokens and cost.
   - **`Bus`:** fans events out to the file sink, the collector and the decision WS, using drop-oldest subscribers.
   - **Metrics only:** in-process kill/secret truth from `game.Level()` is used for metrics and is **never fed to the bot**.
2. **`.dm2` recording** (`internal/demo` + `spectate.Recorder`):
   - One file per level attempt, in the exact `CL_Record_f` client format. Header parity is checked against a header generated by the TS `CL_Record_f`.
   - The browser replays these files with the **existing** `engine.playDemo` (`q2-client/src/cl_main.ts` `CL_PlayDemo`), so `q2-client` doesn't change.
3. **Live relay** (`spectate.Hub`):
   - **Protocol:** a minimal protocol-34 server for each viewer, mounted next to `host.Handler`, with one-time ticket redemption like `host.serveWS`. It handles connectionless messages itself (`getchallenge` → `challenge`, `connect 34 …` → `client_connect`) and uses `Netchan.Setup(NS_SERVER, …)`, with keepalive and timeout.
   - **Joining:** a viewer gets `svc_serverdata` (attractloop=1), configstrings and baselines from the mirrored `LevelSnapshot`, then `precache <sc>`, then `begin`.
   - **Keyframe:** the hub then sends a **keyframe built from the Mirror's copy of the bot's latest frame**: the same serverframe number, `deltaframe −1`, written with the msg delta writers. The bot's next delta then applies on the viewer, so **viewers never perturb the bot's stream**.
   - **Forwarding:**
     - The bot's payloads go out as unreliable data, with stufftext, disconnect, reconnect, serverdata and download stripped.
     - Relay-originated reliable data (configstring diff, `changing`/`reconnect` on level change) goes in **separate packets**.
     - If a forward would overflow `MAX_MSGLEN`, the viewer is resynced instead of losing the frame silently.
   - **Limits:** a slow viewer is resynced. Viewer input is parsed minimally and fuzzed, and there are caps on viewers per bot and in total.
4. **Decision side channel:** `GET /ws/v1/bots/{id}/decisions`, **ticketed like watch**. JSON messages: `hello`, `decision` (aligned to the video by `sf`), `event`, `stats` (1 Hz), `gap`, `bye`.
5. **API** (`api/bots.go`, a `BotHost` interface implemented by `runner.Manager`, following the existing `api.GameHost` pattern; `Q2_BOTS_ENABLED`):
   - Endpoints: `POST/GET /api/v1/bots`, `GET/DELETE /api/v1/bots/{id}`, `POST …/watch` (tickets for both streams), `GET …/artifacts/{name}` (allowlist plus visibility check).
   - **Caps:** the Manager caps concurrent bots globally and per user. It also enforces wall-clock and episode limits, runs lockstep on a **bounded CPU worker pool**, and auto-stops and reaps finished runs. Run IDs are generated on the server.
   - **Access:** Jev bots require an admin. Users may start scripted bots on the demo pakset only when `Q2_BOTS_ALLOW_USERS` is set.
   - **Secrets:** `.env.example` gains `Q2_BOTS_*`, `TYPESAFE_API_KEY` and `JEV_*`. The key lives on the server only and is a redacted `Secret` type.
6. **Web** (`web/apps/web`; no `q2-*` package changes):
   - `GameSession` gets `source: play | watch | replay`. Watch mode uses passive input: no pointer lock, no menu or SP pause, no config writes, and `cl_predict 0`.
   - A watch-specific `TransportFactory` re-tickets on reconnect.
   - New pages `/bots`, `/watch/[id]` and `/bots/[id]` (summary and replay), plus `AiOverlay` (mode, route step, target, probability bars, confidence, latency, cost, provenance), `aiStore` and `DecisionFeed`.
7. **Prometheus** `q2bot_*` metrics on the existing `/metrics`, added last.
- **CLI** `q2bot run -maps demo1,demo2,demo3 -skill 1 -backend scripted|jev|replay|mock|constant|random -session lockstep|inproc -sim-latency 212ms -episodes N -seed -out runs/ -trace -record -budget-usd -max-qps -require-complete -min-model-share 0.7 -json`. Other subcommands: `replay -trace … -strict`, `summarize`, `validate`, `jev-probe`.
- **Run directory:** `runs/<runId>/{run.json, ep-000/{episode.json, trace.jsonl.gz, demos/NN-<map>.dm2, log.txt}}`.

### I. Tests and CI
- **Fairness proof (strongest test):** a differential test rebuilds perception → decide from the **recorded `.dm2`** through a `Passive` fakeclient. It runs in a test binary that does not link `sv`/`game`/`host`, and asserts that the decision trace matches the live lockstep run byte for byte.
- **Other fairness tests:**
  - A **perturbation-invariance** test: randomize non-visible entities in the recorded payloads, and the decisions stay identical.
  - An allowlist import guard over all `agent/*` except `session` and `runner`.
- **Unit tests:**
  - **Perception and tracking:** class coverage of every `models/…md2` literal in `game/*.go`, bbox decoding, MD2 animation mapping, help-layout parsing, vision on `bsp.SyntheticFloorMap()`, tracker and death signals, ObservationFilter, and kick-delta bearing.
  - **Decide and control:** golden lane states and request bodies, arbiter, TTL and reflexes, the scheduler never blocking, and the `CmdAngles` wrap round trip.
  - **Jev client:** fixtures using the documented field names, the test-binary guard, and base-URL safety.
  - **Nav and route:** `mapdata` `G_SetMovedir` against the oracle, the A* vs Dijkstra property, edge re-simulation, and the route table schema.
  - **Recording and relay:** `demo` round trip plus TS header parity, the relay filter and overflow resync, and `FuzzRelayViewerPacket`.
  - **Trace:** golden output and secret redaction.
- **Integration tests** need the demo pak, which CI already fetches with `make demo`. They go through `testutil` and skip without the pak.
  - **Lockstep basics:** handshake and determinism (scripted backend), the mapdata cross-check against `gametest`, and **`q2nav plan` vs the checked-in tables**.
  - **Nav:**
    - On demo1 with god/notarget: walk to 50 random nodes.
    - Edge recipes executed on the live server.
  - **Recording and relay:**
    - `TestRelayMatchesBot`: viewer frames equal bot frames, covering late join, level change, death reload, slow viewer, and an oversized frame with a pending configstring.
    - `.dm2` round trip through `sv demomap` (`OverlayFS`).
  - **Runner:** scripted demo1 with combat; death reload; level change; replay determinism; mock-Jev with injected timeouts and 429s; `TestE2EBotWatch` (`cmd/q2server/e2e_test.go`).
- **Nightly workflow** (`Q2_AGENT_LONG=1`, uploads `run.json`, `.dm2` files and the trace):
  - The full campaign to `victory.pcx` with the scripted backend.
  - The **noisy mock-Jev** campaign with the provenance gate.
  - A SimLatency sweep (100/212/400/500 ms).
- **Web:**
  - vitest for the watch transport, `DecisionFeed`, `aiStore` alignment, and checks that watch and replay sessions register no input listeners.
  - Playwright `watch.spec.ts`.
  - The **`make e2e` recipe** `cd web && pnpm --filter web e2e`, and a new CI e2e job (setup-go + pnpm, `make demo`, `playwright install --with-deps chromium`) with `Q2_E2E_REQUIRE=1`, which makes self-skips fatal.
- **Unchanged:** `make parity`, `check-oracle-untouched`, and the golden and oracle tests stay green. CI has an empty `TYPESAFE_API_KEY`. Live Jev runs are manual (`-tags jevlive` or `workflow_dispatch` with a protected secret).

### J. Docs
- `docs/adr/0006-ai-agent.md`: architecture, fairness rules and their proofs, lockstep vs realtime, relay design and rejected alternatives (an `ss_demo` instance has a shared read position and kills itself when it runs out of data; live client-side demos would mean changing a ported TS file).
- `docs/AGENT.md`: how to run, the schemas, metrics, and the determinism scope.
- A README section, the PARITY TODO-IMPROVE bullet plus the `cl_main.c` target, a `LEGAL.md` note on demos and traces, and `fixtures/agent` in `FIXTURES.md`.

---

## Phased delivery (each phase ends at a gate)

| # | Phase | Gate |
|---|---|---|
| 0 | fakeclient hooks, `session.Lockstep`, `demo` writer, `trace` + `run.json` skeleton | Seeded lockstep run on demo1 is deterministic; its `.dm2` plays through `sv demomap` and in the TS client |
| 1 | `mapdata` (+ own `G_SetMovedir`, gametest cross-check), `q2nav plan`, checked-in route tables | `q2nav plan` re-derives and validates all 4 visit tables on the real pak |
| 2 | `navsim`, `navbuild`, nav cache, `q2nav build/verify/dump`, dev overlay | demo1–3 build in under 60 s; sampled edges pass on re-simulation **and** on the live lockstep server |
| 3 | Navigator runtime, follower, stuck recovery, **minimal control** (`CmdAngles`, `Compose`, `MustFace`) | Walk to 50 random nodes on demo1 (god/notarget) |
| 4 | Movers, conditions and effects, the `route` executor, campaign lifecycle, a **minimal shoot-target executor** for `Kill`/`Shoot` steps | Full campaign to `victory.pcx` with god/notarget |
| 5 | Perception, ObservationFilter, worldmodel, decide (lanes, questions), scripted backend, full control, arbiter, death → reload | Scripted bot finishes the campaign *without god*; fairness differential test green |
| 6 | Jev client, jevtest (noisy policy), scheduler + SimLatency, replay, budgets, the `q2bot` CLI, constant/random ablation backends | CI: mock-Jev demo1 green. Nightly: the noisy-Jev campaign meets the provenance gate, and the ablations are measurably worse |
| 7 | `spectate` relay, decision WS, `BotHost` API, Manager caps | `TestRelayMatchesBot` and `TestE2EBotWatch` green |
| 8 | Web watch and replay, AiOverlay, Playwright, CI e2e job, nightly workflow, docs, Prometheus | `watch.spec.ts` green in CI |
| 9 | **Live Jev** (once a key exists): `jev-probe` → fixtures → tune lanes and questions | **Jev completes the campaign** with ≥ 70 % of target/fire_policy/mode decisions from the model, a stale rate ≤ 15 %, 8–10 QPS in combat, and a cost per run in `run.json` |

## Verification (end to end)
1. `make demo && make test && make parity`: everything green, with the oracle and golden tests untouched.
2. `cd server && go run ./cmd/q2nav plan -pak ../assets/demo/baseq2/pak0.pak` validates the route tables. `q2nav build && q2nav verify` builds the graph and checks it.
3. `go run ./cmd/q2bot run -backend scripted -session lockstep -maps demo1,demo2,demo3 -require-complete -out runs/` should end with `result: completed` (victory), a `.dm2` per level and the trace. Check it with `q2bot validate runs/<id>`.
4. `q2bot replay -trace runs/<id>/ep-000/trace.jsonl.gz -strict` must report no divergence. Determinism is claimed only for the scripted and replay backends.
5. `q2bot run -backend mock` (the noisy policy) together with `-min-model-share 0.7 -require-complete` checks the provenance gate. `-backend constant` and `-backend random` must do measurably worse.
6. With a key:
   - `TYPESAFE_API_KEY=… q2bot jev-probe` updates the fixtures.
   - Then run `q2bot run -backend jev -session lockstep -sim-latency 212ms -budget-usd 2 -min-model-share 0.7 -require-complete`.
7. Run `q2server` with `Q2_BOTS_ENABLED=true` and the web app. On `/bots`, start a bot, then open `/watch/<id>`: it should show the bot's live first-person view with the AiOverlay probabilities. `/bots/<id>` replays a level's `.dm2`.
8. `make e2e`.

## Risks and open items
- **Jev specifics are unverified until a key exists:** rate limits (429/529 are handled, but the docs give no numbers), the token count per request, and whether `criteria` order is kept.
  - **Cost:** about $0.042 per million input tokens. At about 1–1.5k tokens per request and 10 q/s that is about **$1.5–2.5/h**. The blog's "$7/h" implies about 4.6k tokens per request. `jev-probe` measures the real number.
- **Latency:** 70–500 ms. The model therefore picks latency-tolerant *policies* (target, fire_policy, movement), and reflexes and the aim controller execute them every 25 ms. The SimLatency sweep measures how much latency hurts.
- **Navigation is the main risk to finishing.** Phase 4 proves the route without real combat before the brain lands. Watchdogs and the explore fallback provide further protection.
- **Hazards:** kill-gated steps, lethal lasers, fall damage above 187u, directional triggers, and the wrong-exit button `*43`.
- **Fairness assumptions** (stated in ADR-0006):
  - The FOV equals the client fov.
  - MD2 animation names and class priors count as "player knowledge".
  - Monster bodies don't occlude line of sight.
  - In-process level stats are used for metrics only.