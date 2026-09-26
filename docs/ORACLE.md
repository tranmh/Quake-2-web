# C oracle harness

The oracle is the original Quake 2 v3.19 C code compiled natively and driven by small headless programs that dump
its behaviour as golden fixtures (formats: [FIXTURES.md](FIXTURES.md)). Go and TS golden tests compare against them.

## Build

```
oracle/sync.sh            # copy Quake-2/{qcommon,server,client,game,ctf,null,linux,ref_gl} -> oracle/build/src,
                          # normalize line endings, keep a pristine copy in build/orig, apply patches/*.patch
make -C oracle -j         # everything below, into oracle/build/bin
tools/gen-fixtures.sh     # build if needed, regenerate fixtures/generated/**, write fixtures/MANIFEST.sha256
tools/gen-fixtures.sh --check   # regenerate and compare with the committed manifest (drift / nondeterminism)
```

`Quake-2/` is never modified. The only source edits are in `oracle/patches/0001-oracle-build.patch` (compilation /
LP64 fixes, documented in `oracle/patches/README.md`); the drivers added for the oracle need **no** extra patch.
All oracle code uses the Makefile flags: `-m64 -O1 -std=gnu89 -fcommon -fno-strict-aliasing -fno-fast-math
-ffp-contract=off -fexcess-precision=standard` (x86-64 SSE math, no FMA contraction).

| Output | Built from |
|---|---|
| `q2ded` | core + `server/*.c` + `linux/sys_linux.c` + `null/cl_null.c`, `null/cd_null.c` — live dedicated server (`cd oracle/build/bin && ./q2ded +set dedicated 1 +map demo1`) |
| `baseq2/game.so`, `ctf/game.so` | `game/*.c`, `ctf/*.c` with `-fPIC`, not stripped |
| `oracle_core` | core (minus `net_udp.c`) + `server/*.c` + null client + `oracle/src/{sys_oracle,ojson,ostate,core_main}.c` |
| `oracle_game` | same engine objects + `oracle/src/{game_main,elfsym}.c` + `game_dump.c` compiled against `game/g_local.h`; dlopens `baseq2/game.so` |
| `oracle_game_ctf` | same, with `game_dump.c` compiled against `ctf/g_local.h`; dlopens `ctf/game.so` |

"core" = `qcommon/{cmd,cmodel,common,crc,cvar,files,md4,net_chan,pmove}.c`, `game/q_shared.c`,
`linux/{q_shlinux,glob}.c`.

### Driver sources (`oracle/src/`)

- `sys_oracle.c` — replaces `sys_linux.c` and `net_udp.c`: `Sys_*` (console → stderr, silent unless
  `ORACLE_VERBOSE=1`; `Sys_Error` exits 3), inert `NET_*` (no sockets are ever opened; `NET_Config` is a no-op),
  `Sys_GetGameAPI` that dlopens an explicit path and lets the driver wrap the `game_import_t` table before
  `GetGameAPI`, and `oracle_init()` which runs the **real `Qcommon_Init`** with `+set basedir <dir>` and the scenario
  cvars as `+set k v` early commands (exactly like `quake2 +set k v`) plus a harmless late `+echo` command so the
  default `d1`/`dedicated_start` action is not run. `default.cfg` from the pak is executed as usual (binds only).
- `core_main.c` — `oracle_core` subcommands (below). Contains a verbatim copy of the body of
  `SV_WritePlayerstateToClient` working on two `player_state_t`.
- `game_main.c` — `oracle_game` driver (engine side, includes `server/server.h`).
- `game_dump.c` — per-frame dump of game-private state (`edict_t`, `gclient_t`, `level_locals_t`); compiled once
  per game module so private layouts are exact. Edict records are delta-encoded (see FIXTURES.md).
- `elfsym.c` — exact address → symbol name from the module's `.symtab` (dladdr alone would silently name the nearest
  exported symbol for static functions).
- `ojson.c` / `ostate.c` — JSON writer/reader, SHA-256, xorshift32 PRNG, encoders for shared structs.
- `oracle/scripts/gen_pmove_scenarios.py` — builds pmove scenarios from the map (spawn points from the entity string;
  floor, water and ladder spots found by querying the C collision model via `oracle_core trace`).
- `oracle/scripts/gen_game_scenarios.py` — writes the committed `fixtures/scenarios/game/*.json`.
- `oracle/scripts/prep_game_scenario.py` — materializes `entstring_append` into `entstring_override`.

## oracle_core

```
oracle_core [--basedir DIR] [-o OUT] <cmd>      (DIR defaults to $Q2_BASEDIR, then assets/demo)
  bsp <map>                                   CM_LoadMap counts/checksum, cmodels, decompressed PVS/PHS rows, leafs
  trace <map> --random N --seed S             ~N CM_BoxTrace / CM_TransformedBoxTrace / CM_PointContents /
  trace <map> <queries.jsonl>                 CM_TransformedPointContents / CM_PointLeafnum queries and results
  pmove <map> <scenario.json>                 Pmove per usercmd, CL_PMTrace-style world-only trace callback
  msg entity|usercmd|player|scalar|crc|md4 [--seed S]
  rand [--seed S] [--count N]                 glibc rand() after srand(S)
  pak <pakfile>                               directory + sha256 of every entry (own reader)
```
Each map is loaded in a fresh process. Query/value generation uses the oracle's own xorshift32, never `rand()`.
Random trace mix: long random box traces, short traces from empty space (incl. vertical), axial, zero-length,
untransformed and transformed traces against inline models (random origin/angles), point contents (world and
inline), transformed point contents, leaf lookups; hulls: point, player (-16,-16,-24/16,16,32), ducked
(…/16,16,4), large, random asymmetric; masks: PLAYERSOLID, SOLID, SHOT, OPAQUE, WATER, ALL, MONSTERSOLID, DEADSOLID,
CURRENT, SOLID, SOLID|WINDOW. Hand-written cases first: zero-length and ±axis (long and 1/8 unit) traces for four
hulls at 16 empty points, starts outside the world / fully in solid, and point/leaf queries at every inline model.
msg generators keep float inputs inside the range where C's float→int conversions are defined (no UB).

pmove scenarios (per map): walking and running in 8 directions, strafing with turning and mixed msec, jump
hold/release, crouch/crouch-walk, a 3000-cmd random walk with yaw/pitch drift and random msec in 1..250, random
walks from 12 floor spots (ledges/stairs/slopes), falls from mid-air with various velocities, swimming in water spots
(sink, pitch up/down, swim up = waterjump attempts) and water entry from above, ladders (demo1 has some), PM_SPECTATOR
(incl. noclip outside the world), PM_DEAD, PM_GIB, PM_FREEZE, PMF_TIME_TELEPORT/LAND/WATERJUMP timers, non-zero
delta_angles, gravity 400/0/1600, msec 0/1/255. `demo1_airaccel` is the same generator with airaccelerate 10.

## oracle_game / oracle_game_ctf

```
oracle_game [--basedir DIR] [--game-so PATH] [-o OUT] [--full] scenario.json
```
The game module defaults to `<dir of the executable>/<module>/game.so`. Sequence:

1. `oracle_init` (real `Qcommon_Init`, scenario cvars as `+set`; the ctf binary also sets `game ctf`).
2. Real `SV_InitGame` → `SV_InitGameProgs` → our `Sys_GetGameAPI` wraps the import table → `ge->Init`.
   `dedicated` is 0, so SV_InitGame's maxclients rules for SP/coop/DM apply as in a listen server.
3. `srand(seed)`, rand counter reset. **Bypassed engine rand:** SV_InitGame's `svs.spawncount = rand()` happens
   before this reset and is not part of the stream; `SV_Frame`'s per-frame `rand()` ("keep the random time
   dependent") is never called because the driver does not call `SV_Frame`. No other engine code calls `rand()`.
4. Real `SV_SpawnServer(map, "", ss_game, false, false)`; `ge->SpawnEntities` is wrapped only to substitute
   `entstring_override` when given.
5. Clients: the tail of `SVC_DirectConnect` (slot init, `ge->ClientConnect` with `\ip\loopback` added to the userinfo,
   `SV_UserinfoChanged`, `Netchan_Setup`, `cs_connected`), then `SV_ExecuteUserCommand("new")` and
   `("begin <spawncount>")` → real `SV_New_f` / `SV_Begin_f` → `ge->ClientBegin`.
6. Dump frame 0, `SV_PrepWorldFrame`.
7. Per frame: clear client message buffers (nothing is ever sent), inputs (`SV_ClientThink` / `SV_ExecuteUserCommand`),
   `sv.framenum++; sv.time = sv.framenum*100; ge->RunFrame()`, dump, `SV_PrepWorldFrame`.

Capture: `sound`, `positioned_sound`, `configstring`, `bprintf`, `cprintf`, `centerprintf`, `dprintf`, `multicast`,
`unicast` (bytes = `sv.multicast` at the call) and `AddCommandString` are wrapped and then forwarded to the real
`PF_*`/`SV_*` functions (printf-style calls are forwarded pre-formatted with `"%s"`), except `AddCommandString`,
which is recorded only (the command buffer is never executed: no `gamemap`, `menu_loadgame`, ...). `gi.error`
prints and exits 4. `rand` is defined in the executable and exported (`-Wl,--export-dynamic-symbol=rand`), so both
the engine and `game.so` (via its PLT) call the counting wrapper, which forwards to glibc's `rand` (`RTLD_NEXT`).
`level` is read through `dlsym(game, "level")`, edicts through `ge->edicts`.

Committed scenarios (`fixtures/scenarios/game/`, regenerated by `gen_game_scenarios.py`):

| Scenario | Binary | What |
|---|---|---|
| demo1_sp_idle | oracle_game | SP skill 1, player idles 300 frames |
| demo1_sp_walk | oracle_game | scripted: walk/turn, `give all`, switch to and fire all 10 weapons, inventory cmds, run/jump/crouch; 600 frames |
| demo1_sp_random | oracle_game | SP random walk 1200 frames, no god mode (the player dies; SP respawn is a `menu_loadgame` cmd event, so it stays dead) |
| demo2_sp_random | oracle_game | SP skill 2 random walk, 600 frames |
| demo3_sp_random | oracle_game | SP skill 0 random walk (female, left hand), 600 frames |
| demo1_dm4 | oracle_game | deathmatch 1, 4 clients: `give all`, random walks with shooting, weapon switches, `kill`, `say`, `score` |
| demo1_coop2 | oracle_game | coop 1, 2 clients random walking, `give shotgun` |
| ctf_demo1 | oracle_game_ctf | demo1 + appended info_player_team1/2 and item_flag_team1/2 (placed on DM spots), 4 clients `team red/blue`, random walks, team change, `kill` |
| `ctf_<topic>` | oracle_game_ctf | synthetic CTF scenarios (`oracle/scripts/gen_ctf_scenarios.py`, demo1 start area, monsters stripped): flags (take/capture/drop/return/auto-return), capturelimit, grapple, techs, teams/menus/chase, match mode (ready, ghosts, admin menu), votes/elections/warp; see docs/CTF.md |
| `synth_<monster>` | oracle_game | demo1 with its monsters stripped (`entstring_strip`) and synthetic monsters appended near the SP start; player `give all`, `god`, random walk shooting (rockets/BFG/machinegun, `give cells` refills); some add `func_timer`→`target_explosion` to force pain/death: gladiator, supertank, medic (revives a chick killed by an explosion), chick, mutant, brain, flipper (in the water basin, own `info_player_start`), float, hover, boss2, jorg (dies → MakronToss → Makron fights and dies), boss3 (`monster_boss3_stand` used by `trigger_always`), insane (spawnflag variants 0/4/8/16/32/20), actor (`misc_actor` → `target_actor` message → `path_corner` → jump `target_actor` → attack `target_actor` kills a `misc_insane`) |

## Known limitations

- No networking at all: no client frames/deltas are built, reliable/unreliable buffers are cleared every frame,
  pings stay 0, `sv_enforcetime` is off (default), usercmds are applied directly with `SV_ClientThink`.
- Clients connect only at frame 0; there is no disconnect/reconnect scripting (a `disconnect` command would work
  through `SV_ExecuteUserCommand` but is not used).
- The command buffer is not executed, so level changes (`gamemap`), SP respawn (`menu_loadgame`) and server console
  commands issued by the game do not happen; they appear as `cmd` events.
- `SV_CheckForSavegame` is live: a `save/current/<map>.sav` in the base dir would change results (none exists in the
  demo layout).
- Everything is 64-bit (LP64 with the documented patches); a `-m32` cross-check is not done.
- Game-state dumps cover the fields listed in FIXTURES.md, not every `edict_t` field.
- Pmove scenarios depend on the demo maps (spots are sampled from the collision model); they are regenerated on every
  run, deterministically.
