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
- `web/apps/dev`: Vite renderer harness (`pnpm --filter dev dev`, then `?map=demo1`).
- `server/cmd/wsbridge`: WebSocket↔UDP relay so the browser client can play on the original C `q2ded`.
- `oracle/build/bin/q2ded`: the original dedicated server, built headless for x86_64.

## How quality is verified

| Area | Evidence |
|---|---|
| Collision, movement, protocol | 60k BSP traces, 47,840 pmove steps, delta entity/usercmd/playerstate encodings, CRC/MD4, glibc `rand` — bit-exact in Go **and** TypeScript |
| Game logic | 29 oracle scenarios (SP on demo1–3, coop, 4-player DM, 14 synthetic monster scenarios incl. all bosses, 8 CTF scenarios) — every edict, client, event and `rand()` call per frame bit-exact |
| Renderer | 7,905 surfaces, lightmaps, all texture uploads/mips, MD2 lerp/shading, sky, warp vs real `ref_gl` C |
| Sound, effects | Mixer frames, spatialization, WAV resampling, particle effects vs the original C |
| Netcode | Netchan datagrams byte-exact vs `net_chan.c`; TS client plays a full session against the real C `q2ded` with zero prediction error |
| Full stack | Go e2e (SP save/restore, DM, CTF over WebSocket, Postgres) and Playwright e2e in the browser |

## Legal

This repository contains a port of the GPL-licensed Quake II engine and game source
(© id Software, see `Quake-2/gnu.txt`). **It contains no game data.** Game data is copyrighted by
id Software. The Quake II demo (`q2-314-demo-x86.exe`) is freely redistributable under id's demo
license and is fetched by `make demo`. To play the full game you must own Quake II and upload your
own `pak0.pak`; uploaded retail data is private to your account. Do not obtain retail data from
unofficial sources.
