# Quake 2 Web

A faithful port of id Software's Quake II (v3.19, protocol 34) to a modern stack:

- **Go** authoritative server (`server/`): engine server, all game logic (22 monsters, 11 weapons, every map
  entity), the CTF mod, asset ingest, REST API, Postgres accounts and saves.
- **TypeScript** browser client (`web/packages/*`): WebGL2 renderer (port of `ref_gl`), AudioWorklet mixer
  (port of `snd_dma`/`snd_mix`), client prediction, effects, HUD layouts, console, cinematics.
- **Next.js** shell (`web/apps/web`): accounts, pak upload, server browser, saves, settings, the game page.

The original C source is tracked as the `Quake-2/` git submodule and is used **only as a test oracle** —
it is never modified. It is compiled (`oracle/`) into harnesses that generate golden fixtures; the Go and
TypeScript ports are tested against them bit for bit. See
[`docs/plans/0001-port-plan.md`](docs/plans/0001-port-plan.md) for the plan,
[`docs/PORTING.md`](docs/PORTING.md) for the porting rules and [`docs/PARITY.md`](docs/PARITY.md) for the
disposition of every original file.

## Quick start

Requirements: Go 1.22+, Node 22 + pnpm, gcc (for the oracle), Docker (optional, for Postgres).

```sh
git submodule update --init
make demo                       # download the free Quake II demo, extract pak0.pak into assets/
make oracle fixtures            # build the C oracle and generate golden fixtures (~1 min)
make test golden                # Go + TypeScript unit and golden tests

# run the stack locally (in-memory DB; set DATABASE_URL for Postgres)
cd server && Q2_COOKIE_SECURE=false Q2_DEMO_PAK=../assets/demo/baseq2/pak0.pak \
  Q2_CORS_ORIGINS=http://localhost:3000 go run ./cmd/q2server
cd web && pnpm install && pnpm --filter web dev     # http://localhost:3000
```

Or everything in containers: `docker compose -f deploy/docker-compose.dev.yml up --build`
(API/WebSocket on :8080, web on :3000, Postgres on :5433).

Useful dev tools:
- `web/apps/dev`: Vite renderer harness (`pnpm --filter q2-dev dev`, then `?map=demo1`; `&nav=1` overlays the
  AI agent's nav graph and route).
- `server/cmd/wsbridge`: WebSocket↔UDP relay so the browser client can play on the original C `q2ded`.
- `oracle/build/bin/q2ded`: the original dedicated server, built headless for x86_64.

## AI agent

A headless bot plays the single-player demo campaign: demo1 → demo2 → demo3 → demo2 again → `victory.pcx`.
It perceives only what a player could (field of view, line of sight, audible sounds), and a pluggable
decision backend chooses what it does:
- [Jev](https://docs.typesafe.ai), TypeSafe's System One model, is the primary backend;
- `scripted` (the baseline), `mock` (the real Jev client against a fake server), `replay`, and the
  `constant` and `random` ablations are the others.

The model picks intents (target, fire policy, movement, mode, weapon, pickup, danger) about 10 times a
second. Code executes them every 25 ms: aiming, path following on a nav graph that is pmove-validated and
built from the BSP, and reflexes. Every run writes `run.json` (outcome, deaths, the provenance of every
decision, latency, tokens, cost), a decision trace and one `.dm2` per level. In `q2server` you can watch a
bot live in the browser at `/watch/<id>`, with an overlay of its decisions, and replay a finished run at
`/bots/<id>`.

```sh
make demo nav                                  # demo pak + nav graphs of demo1-3 (assets/nav)
cd server
go run ./cmd/q2bot run -backend scripted -require-complete            # the whole campaign, ~30 s
go run ./cmd/q2bot validate runs/<id>                                 # traces + demos
go run ./cmd/q2bot run -backend mock -maps demo1 -min-model-share 0.7 -require-complete   # noisy mock Jev, gated
go run ./cmd/q2nav plan -pak ../assets/demo/baseq2/pak0.pak           # validate the route tables
```

How to run, schemas and metrics: [`docs/AGENT.md`](docs/AGENT.md). Design and fairness rules:
[ADR-0006](docs/adr/0006-ai-agent.md). Evaluation: [`docs/AGENT-EVAL.md`](docs/AGENT-EVAL.md).

## How quality is verified

| Area | Evidence |
|---|---|
| Collision, movement, protocol | 60k BSP traces, 47,840 pmove steps, delta entity/usercmd/playerstate encodings, CRC/MD4, glibc `rand` — bit-exact in Go **and** TypeScript |
| Game logic | 29 oracle scenarios (SP on demo1–3, coop, 4-player DM, 14 synthetic monster scenarios incl. all bosses, 8 CTF scenarios) — every edict, client, event and `rand()` call per frame bit-exact |
| Renderer | 7,905 surfaces, lightmaps, all texture uploads/mips, MD2 lerp/shading, sky, warp vs real `ref_gl` C |
| Sound, effects | Mixer frames, spatialization, WAV resampling, particle effects vs the original C |
| Netcode | Netchan datagrams byte-exact vs `net_chan.c`; TS client plays a full session against the real C `q2ded` with zero prediction error |
| Full stack | Go e2e (SP save/restore, DM, CTF over WebSocket, Postgres) and Playwright e2e in the browser (play a game; start, watch and replay a bot) |
| AI agent: fairness | Import guards in every agent package the bot's decisions depend on (no `sv`/`game`/`host`/`session` linked); perturbation invariance (rewriting everything not perceived leaves the belief unchanged); a `.dm2` differential that rebuilds every decision byte for byte from the recording alone |
| AI agent: navigation | Nav graph edges validated by bit-exact pmove simulation; sampled edges re-run on a live server 100/100 bit-exact per map; route tables re-derived from the entity lump and validated (`q2nav plan`) |
| AI agent: play | Scripted lockstep campaign to `victory.pcx` without cheats (CI and nightly); lockstep runs replay with no divergence; a mock-Jev demo1 run against the provenance gate (≥ 70 % of target/fire_policy/mode decided by the model) in CI (`make agent-smoke`) and full mock-Jev campaigns against it nightly, next to ablations that must do measurably worse. Results: [`docs/AGENT-EVAL.md`](docs/AGENT-EVAL.md) |

## Legal

This repository contains a port of the GPL-licensed Quake II engine and game source
(© id Software, see `Quake-2/gnu.txt`). **It contains no game data.** Game data is copyrighted by
id Software. The Quake II demo (`q2-314-demo-x86.exe`) is freely redistributable under id's demo
license and is fetched by `make demo`. To play the full game you must own Quake II and upload your
own `pak0.pak`; uploaded retail data is private to your account. Do not obtain retail data from
unofficial sources.
