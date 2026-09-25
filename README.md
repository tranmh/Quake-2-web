# Quake 2 Web

A faithful port of id Software's Quake II (v3.19, protocol 34) to a modern stack:

- **Go** authoritative server (`server/`): engine server, game logic, CTF, asset ingest, REST API, Postgres.
- **TypeScript** browser client (`web/`): WebGL2 renderer (port of `ref_gl`), AudioWorklet mixer, prediction, HUD, console.
- **Next.js** shell (`web/apps/web`): accounts, pak upload, lobby, server browser, saves, settings.

The original C source is tracked as the `Quake-2/` git submodule and is used **only as a test oracle** —
it is never modified. See [`docs/plans/0001-port-plan.md`](docs/plans/0001-port-plan.md) for the full plan
and [`docs/PORTING.md`](docs/PORTING.md) for porting rules.

## Quick start

```sh
git submodule update --init
make demo        # download the free Quake II demo and extract pak0.pak into assets/
make oracle      # build the C oracle harness (needs gcc)
make fixtures    # generate golden fixtures from the oracle
make test        # Go + TypeScript unit and golden tests
make dev         # docker compose: postgres + server + web
```

## Legal

This repository contains a port of the GPL-licensed Quake II engine and game source
(© id Software, see `Quake-2/gnu.txt`). **It contains no game data.** Game data is copyrighted by
id Software. The Quake II demo (`q2-314-demo-x86.exe`) is freely redistributable under id's demo
license and is fetched by `make demo`. To play the full game you must own Quake II and upload your
own `pak0.pak`; uploaded retail data is private to your account. Do not obtain retail data from
unofficial sources.
