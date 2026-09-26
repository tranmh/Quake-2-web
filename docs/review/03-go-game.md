# Review 03 — Go game module (`server/internal/game`)

Scope: `server/internal/game/*.go` (all C-mapped files, `m_*.go`, `g_ctf.go`, `p_menu.go`, `g_save.go`,
`registry.go`) and `gametest/`. The C tree `Quake-2/` is the oracle and was read, not changed.

Rule used throughout: behaviour that faithfully reproduces the C is not a bug. Deliberate deviations are
limited to inputs on which the C game crashes (NULL dereference, stack overflow, out-of-bounds access),
ends the server with `gi.error`, or does something unsafe (console injection). Each one is listed below
with its reason.

## Method

- **Line-by-line comparison** of the Go port against `game/*.c` and `ctf/*.c`, focusing on the paths
  the 29 golden scenarios do not cover:
  - entities: g_target, g_trigger, g_turret, g_utils, g_misc, g_func;
  - commands, HUD and play flow: g_cmds, p_hud, g_chase, p_client, g_items, p_weapon;
  - CTF: g_ctf and p_menu (admin, elections, match, ghosts, menus, say_team macros, techs, grapple).
- **Hostile-input harness** (`gametest/robust*_test.go`, `gametest/fuzz_test.go`). It drives the real game
  through the `gametest` engine on demo1 in sp, coop (3 clients), dm (3 clients, cheats) and ctf
  (3 clients, competition, admin password):
  - `TestRobustMonkey`: every frame gets random hostile usercmds, plus random client commands from a
    dictionary with crafted arguments (huge or negative numbers, item names, `%` macros, quotes, 300-byte
    strings). It also applies random userinfo (name, skin, hand, fov, spectator, gender, password, and
    malformed strings), disconnects, reconnects, and connect-without-begin (commands from `cs_connected`
    clients). Long run: 400 seeds × 4 modes × 800 frames.
  - `TestRobustEntities`: random entity strings for uploaded maps. It uses ~120 classnames and random
    keys cross-linked through a small targetname pool (target, killtarget, team, pathtarget, …), with
    hostile values and bad models. They are spawned next to the player and played with the monkey.
    Long run: 1500 seeds × 4 modes.
  - `TestRobustSave`: takes a valid game and level save of a running game, decompresses it, replaces
    JSON values with hostile ones (numbers, wrong types, registry names, deleted keys, truncated arrays),
    then loads it through the real flow (`ReadGame`, `SpawnServer`, `ReadLevel`, reconnect) and plays.
    Long run: 600 blobs × 4 modes.
  - Native fuzz targets `FuzzClientCommand`, `FuzzUserinfo`, `FuzzEntities` and `FuzzReadSave` each ran
    for 4–5 minutes. `FuzzReadSave` fuzzes the raw bytes, so the zstd and JSON layers are covered too.
    Without `-fuzz` they run their seed corpus as regular tests.
- **Concurrency.**
  - `TestRobustConcurrentInstances`: 6 concurrent instances in all modes, playing hostile input and
    saving.
  - `TestGoldenConcurrent` (`-tags golden`): all 29 golden scenarios in parallel in one process, each
    still compared with the oracle.
  - Both were run with `-race`. The package-level variables were also audited.

## Summary

| Severity | Count | Fixed |
|---|---|---|
| critical | 4 | 4 |
| high | 6 | 6 |
| medium | 3 | 1 (the other 2 are mitigated or documented) |
| low / info | 10 | 3 |

Race and determinism: no finding.
- All package-level state is written only during `init()`:
  - the registry (`regByName`, `mmoves`, `spawnRegs`, `monsterStaticsNew`);
  - the item, spawn and mmove tables;
  - the CTF template menus.
- Everything mutable is per `Game`:
  - the CTF menus are copied in `CTFInit` and again in `PMenu_Open`;
  - monster statics live in `g.mstatics`;
  - the coop `item->drop = NULL` is tracked in `itemDropCleared`.
- `go test -race` with 29 concurrent golden instances and 6 concurrent hostile instances is clean.
- The only wall-clock read is `func_clock`'s time-of-day style (`time.Now`, g_misc.go), which is C's
  `time()/localtime()` (faithful).
- Map iteration is either sorted (save writing) or order-independent.

## Findings

Status legend: **fixed** (TDD: red test, fix, green), **mitigated** (contained by G-02, behaviour otherwise
faithful), **open** (documented, not changed).

### G-01 — critical — `invprev*` indexes `itemlist[-1]` (process crash from a player command)

- **Location:** `g_cmds.go:111` `SelectPrevItem` (C: `game/g_cmds.c:98`, `ctf/g_cmds.c` same).
- **Problem:** With `pers.selected_item == -1` and no matching item, the last iteration computes
  `index = (-1 + 256 - 256) % 256 = -1`.
  - `cmdsInventory(-1)` correctly returns `selected_item` itself (-1, which is nonzero).
  - Then `&g.itemlist[-1]` panics.
- **C behaviour:** C reads `itemlist[-1]` out of bounds (UB, no crash). Whatever that entry holds, the
  result is `selected_item = -1`.
- **Failure scenario:** any player types `invnextp` then `invprevp` with no powerup (also
  `invnextw`/`invprevw` without weapons, or on a ctf menu-less client). The runtime panic reached
  `sv.recoverError`, which re-panics non-`ComError` values, so the whole server process died.
- **Fix:** skip the negative index (same result as C: falls through to `selected_item = -1`).
- **Test:** `gametest/review_test.go` `TestReviewInvPrevNoItem`.
  - red: `command "invprevp": panic: runtime error: index out of range [-1]` (`SelectPrevItem g_cmds.go:113`)
  - green: PASS (sp and ctf); `selected_item` stays -1.

### G-02 — critical — no panic containment at the game export boundary (process crash from maps/saves)

- **Location:** every `game_export_t` entry point: `Init`, `Shutdown`, `RunFrame`, `SpawnEntities`,
  `ClientConnect`, `ClientBegin`, `ClientUserinfoChanged`, `ClientDisconnect`, `ClientCommand`,
  `ClientThink`, `ServerCommand`, `Read/WriteGame`, `Read/WriteLevel`.
- **Problem:** The engine's frame recovery (`internal/sv/server.go` `recoverError`) handles only
  `shared.ComError` and re-panics anything else. Every Go runtime panic in the game therefore crashed
  the process, taking every hosted instance with it. That includes the many C NULL dereferences that a
  malformed user-uploaded map can reach (G-M01).
- **C behaviour:** C crashes too, but it crashes only its own server process.
- **Fix:** `game.go` `(*Game).guard` / `guardErr`, deferred by every entry point. A runtime panic
  becomes `gi.error("internal error: <panic> at <file>:<line> <func>")`, which is ERR_DROP of that
  instance only. The four entry points that return an error return it instead. `ComError` passes
  through unchanged. Valid input never reaches it, so goldens are unaffected.
- **Test:** `TestReviewGuardConvertsPanics` (turret_driver without target).
  - red (guard disabled): `want gi.error naming the panic site, got "panic: runtime error: invalid memory address or nil pointer dereference"`
  - green: `comerror: Game Error: internal error: runtime error: invalid memory address or nil pointer dereference at g_turret.go:366 (*Game).turret_driver_link`

### G-06 — critical — entity loops recurse until a fatal (unrecoverable) stack overflow

- **Location:** `g_utils.go:151` `G_UseTargets`, `g_func.go:1489` `train_next` (C: `game/g_utils.c:180`,
  `game/g_func.c:1439`).
- **Problem:**
  - A target loop recursed without limit through `G_UseTargets → trigger_relay_use → G_UseTargets …`.
    Examples: two `trigger_relay`s targeting each other, a `trigger_multiple` or `func_timer` targeting
    itself through a relay, or a `trigger_counter` cycle.
  - A `func_train` on zero-length, wait-0 `path_corner` loops recursed through
    `train_next → Move_Calc → Move_Begin → Move_Final → Move_Done → train_wait → train_next …`.
- **C behaviour:** C overflows its 8 MB stack (SIGSEGV).
- **Why it is worse in Go:** a goroutine stack overflow is `fatal error: stack overflow`. `recover()`
  can't catch it, so G-02 doesn't help: one uploaded map, or a mapper's mistake, killed the whole
  process. The long entity run hit it too (`ents1.log`, train loop).
- **Fix:** `game.go` `enterCall`/`leaveCall` (`maxCallDepth = 4096`, a transient `Game.callDepth`
  that is not saved). Past the limit it raises `gi.error("<fn>: entity loop, recursion deeper than 4096")`.
  A non-looping chain nests at most once per entity (MAX_EDICTS 1024), so any map the C game can run
  is unaffected.
- **Tests:** `TestReviewTargetLoop`, `TestReviewTrainLoop`.
  - red: `runtime: goroutine stack exceeds 1000000000-byte limit / fatal error: stack overflow` in
    `G_UseTargets → trigger_relay_use → G_UseTargets …` (and in `train_next → train_wait → …`); the
    test binary died.
  - green: `comerror: Game Error: G_UseTargets: entity loop, recursion deeper than 4096`, and
    `… train_next: entity loop …`.

### G-07 — critical — `target_string` with a negative `target_character` count (Go-only panic)

- **Location:** `g_misc.go:1602` `target_string_use` (C: `game/g_misc.c:1588`).
- **Problem:** `n = count-1 < 0` passes C's only check (`n > l`), and Go's `self.Message[n]` panicked.
- **C behaviour:** C reads `message[n]`, the heap byte before the string (UB, not a crash).
- **Failure scenario:** a map with a target_string / target_character team where one character has
  `"count" "-1"`, fired by a func_clock or any trigger. Before G-02 the process crashed.
- **Fix:** for `n < 0`, use the blank frame (12). It only deviates where C is undefined.
- **Test:** `TestReviewTargetStringNegativeCount`.
  - red: `internal error: runtime error: index out of range [-2] at g_misc.go:1603 (*Game).target_string_use`
  - green: PASS.

### G-03 — high — save: `selected_item` / inventory beyond the item table are trusted

- **Location:** `g_save.go:644` `validateClient`, called from `ReadGame` (C: `g_save.c:487`, which
  trusts the file).
- **Problem:** A tampered or corrupted `game.ssv` blob with `Pers.SelectedItem` outside -1..len(itemlist)-1,
  or a nonzero `inventory[i]` for a nonexistent item, made `SelectNextItem`/`SelectPrevItem`/`InvUse`
  index `itemlist` out of range.
- **Found by:** `TestRobustSave`: `internal error: index out of range at g_cmds.go:78 SelectNextItem`
  and `g_cmds.go:120 SelectPrevItem`.
- **Fix:** reject the save with a clear error. Both `pers` and `resp.coop_respawn` are checked.
- **Test:** `TestReviewSaveSelectedItem`.
  - red: `tampered save was accepted` (SelectedItem 256)
  - green: `rejected: client 0: save: selected_item 256 out of range` (also 1000, -2, -100); -1 still
    loads and plays.

### G-04 — high — save: game counts size arrays and loops without validation

- **Location:** `g_save.go:629` `validateGameLocals`, `ReadGame`.
- **Problem:** `game.maxclients`, `maxentities` and `num_items` are read from the blob.
  - `ReadGame` did `make([]GClient, Maxclients)` before any check.
    - 2e9 gave `makeslice: len out of range`.
    - Values around 1e7 allocate gigabytes. Zeroed pages are touched by the index loop, which risks an
      OOM kill that no `recover` can stop.
  - A smaller maxclients than the latched cvar gave `ReadLevel` index errors.
  - A different `num_items` made `ED_CallSpawn`/`give` loops run past the item table.
  - A different `maxentities` let `G_Spawn` run past `g.edicts`.
- **Fix:** before allocating, require all of the following:
  - `1 <= maxclients <= MAX_CLIENTS`, equal to the `maxclients` cvar (the engine restores the latched
    cvars from server.ssv before `ReadGame`, so consistent saves always pass);
  - `maxentities == len(edicts)`;
  - `num_items == len(itemlist)-1`;
  - the client array length equals maxclients.
- **Test:** `TestReviewSaveGameCounts`.
  - red:
    - `maxclients 1 (cvar 4)`: accepted;
    - `num_items 1000`: `index out of range [43] with length 43 at g_spawn.go:330 ED_CallSpawn`;
    - `maxclients huge`: `makeslice: len out of range at g_save.go:596 ReadGame`;
    - `maxentities 4096`: accepted.
  - green: all six cases are rejected with messages like `save: maxclients 1 does not match the
    server (4)`, and no large allocation happens.

### G-05 — high — save: level indices (`num_edicts`, `body_que`, trail head) unchecked

- **Location:** `g_save.go:776-793` `ReadLevel`.
- **Problem:**
  - `NumEdicts` > maxentities indexed `g.edicts` out of range.
  - `level.body_que` and `trail_head` outside their 8-entry arrays panic in `CopyToBodyQue` and
    `PlayerTrail_Add`.
  - `ReadLevel` also assumed `len(game.clients) >= maxclients`.
- **Fix:** range checks that return an error.
- **Test:** `TestReviewSaveLevelIndices`.
  - red:
    - `num_edicts 5000`: `index out of range [1024] with length 1024 at g_save.go:770 ReadLevel`;
    - `body_que 100/-1` and `trail_head 99`: accepted.
  - green: every case is rejected (`save: body_que 100 out of range`, …).
- **Note:** saves are read by `internal/sv/sv_ccmds.go` from the save store (DB). Any load error ends
  the load with ERR_DROP there.

### G-20 — high — save: zstd decompression without a size limit (decompression bomb, OOM kill)

- **Location:** `g_save.go` `zstdDecompress` / `maxSaveSize`.
- **Problem:** `zstd.NewReader(nil)` defaults to a 64 GiB decoded-size limit. A tampered save of a few KB
  expands to gigabytes.
  - `FuzzReadSave` found a 69 KB blob that allocated 3.8 GB, and the fuzz worker was killed
    ("fuzzing process hung or terminated unexpectedly").
  - An OOM kill is not recoverable, so it takes down the whole process.
- **Fix:** `zstd.WithDecoderMaxMemory(64 MiB)` (real level saves are about 1 MB) and decoder
  concurrency 1.
- **Test:** `TestReviewSaveDecompressionBomb`, a 114 KB frame of 1 GiB of zeros.
  - red: `loader allocated 1025 MB for a 114692-byte save`
  - green: `rejected: save: decompressed size exceeds configured limit`. The corpus entries in
    `gametest/testdata/fuzz/FuzzReadSave/` now replay with a max RSS of 96 MB (before: 3.9 GB).

### G-09 — high — a dead coop player can end the server (`SV_Physics: bad movetype 4`), C-faithful

- **Location:** `p_client.go:940` `CopyToBodyQue` (C: `game/p_client.c:971`).
- **Problem:** `noclip` needs no cheats outside deathmatch.
  - A dead player (movetype TOSS) who types `noclip` twice ends up with MOVETYPE_WALK.
  - `respawn` copies that movetype into a body-queue edict.
  - `G_RunEntity` has no case for WALK, so `gi.error("SV_Physics: bad movetype 4")` fires. That is
    ERR_DROP of the whole instance, and C behaves the same way.
- **Failure scenario:** any coop player can kill the game for everyone. So can any DM player when cheats
  are on. Found by the long monkey run (5 of 400 coop seeds).
- **Fix (documented deviation):** a body copied with MOVETYPE_WALK gets MOVETYPE_TOSS, the movetype a
  corpse always has. This changes only the case where C ends the server.
- **Test:** `TestReviewDeadNoclipBody`.
  - red: `respawn and play: comerror: Game Error: SV_Physics: bad movetype 4`
  - green: PASS.

### G-11 — high (security) — console command injection through `level.changemap` (C-faithful)

- **Location:** `g_main.go:509` `gamemapCommand`, used by `ExitLevel` and the ctf `ExitLevel`
  (C: `game/g_main.c:322`, `ctf/g_main.c:340`).
- **Problem:** `gamemap "<changemap>"\n` is built from map data (target_changelevel `map`, worldspawn
  `nextmap`) and passed to `gi.AddCommandString`. `ED_NewString` turns `\n` escapes into real newlines,
  so a user-uploaded map could run arbitrary console commands on its server when the level is exited.
  An example `map` value is `demo2\nquit\nset rcon_password x\n`.
- **Fix (documented security deviation):** cut the map name at the first `"`, CR or LF. Real map names
  never contain them. CTF warp names were already validated against `warp_list`.
- **Test:** `TestReviewChangemapInjection`.
  - red: `console command injection: "gamemap \"demo2\nquit\nset rcon_password x\n\"\n"`
  - green: `commands: ["gamemap \"demo2\"\n"]`.

### G-08 — medium — `flood_msgs` outside 1..11 indexes `flood_when[]` out of bounds

- **Location:** `g_cmds.go:739` `floodWhen`, used by `CheckFlood` (C: `game/g_cmds.c:838`,
  `ctf/g_cmds.c:863`).
- **Problem:** `i = flood_whenhead - flood_msgs + 1`, with one `+10` correction. For
  `flood_msgs >= 12` or `< 0`, `i` stays outside 0..9 and Go panicked on every chat line. That
  includes unknown commands, which are chat.
- **C behaviour:** C reads the neighbouring `gclient_t` fields.
- **Fix:** reproduce C's struct layout (identical in both modules) for the reads:
  - index -2 reads `pickup_msg_time`;
  - index -1 reads `flood_locktill`;
  - index 10 reads `flood_whenhead` as float bits;
  - index 11 reads `respawn_time`;
  - anything further out is UB, read as 0.
- **Trigger:** only the server admin can set `flood_msgs`, hence medium.
- **Test:** `TestReviewFloodMsgsRange` (values 12, 30, -1, -5, 0.5; baseq2 and ctf; say and say_team).
  - red: `flood_msgs 12 (baseq2), message 0: … index out of range [-1] at g_cmds.go:720 (*Game).CheckFlood`
  - green: PASS.

### G-12 — medium — an explicitly empty entity key (`"key" ""`) is treated like a missing key (open)

- **Problem:** Go stores unset strings as `""`, so every C `if (ent->x)` / `!ent->x` became `!= ""` /
  `== ""`. In C, `"key" ""` yields a non-NULL empty string. Affected places:
  - **g_utils:** `G_UseTargets` message `""`: C centerprints `""` and plays `misc/talk1.wav`; Go stays
    silent. `"target" ""`/`"killtarget" ""`: C fires or kills every entity with an empty targetname.
    `G_PickTarget("")`.
  - **g_target:**
    - `SP_target_help` message `""` (Go frees it; C keeps it);
    - `SP_target_changelevel` map `""`;
    - `SP_target_speaker` noise `""`;
    - `SP_target_secret`/`goal` noise `""` (Go uses the default sound; C is silent);
    - `SP_target_lightramp` message/target `""`;
    - `target_laser_start` target `""`.
  - **g_trigger:** `SP_trigger_key` item/target `""`, `SP_trigger_gravity` gravity `""`.
  - **g_turret:** `SP_turret_driver` item `""`, `turret_breach_finish_init` target `""`.
  - **p_client:** `SelectSpawnPoint` / `SP_FixCoopSpots` targetname `""`.
  - **g_misc / g_func / g_ctf:** `SP_func_clock`, `trigger_elevator`, and the ctf `SetLevelName` with
    worldspawn message `""`.
- **Scope:** only maps that write empty values explicitly, which the stock maps never do.
- **Status:** open. Fixing it means carrying "set but empty" for the spawn-time string fields through
  `ED_ParseField` and the save format.

### G-M01 — medium — C NULL dereferences reachable from malformed maps (mitigated by G-02)

These crash the C game as well (faithful). Before G-02 they crashed the whole Go process; now they end
only the instance with `gi.error("internal error …")`. They are listed in `knownCCrashes`
(`gametest/robust_test.go`) so the harness keeps running.

| Trigger | Go location | C location |
|---|---|---|
| turret_breach whose target is unresolvable, or with no team | g_turret.go:219/223 | g_turret.c:211/215 |
| turret_driver with no target | g_turret.go:366/367 | g_turret.c:363/364 |
| func_clock whose target has no `use` | g_misc.go:1715 | g_misc.c:1709 |
| `Killed` on an entity without `die`, e.g. monster_commander_body telefragged | g_combat.go:107 | g_combat.c:129 |

Also found by review, and faithful in both C and Go:
- misc_viper_bomb without a misc_viper (g_misc.go:1409);
- path_corner_touch / point_combat_touch on a monster without `stand`;
- intermission with no intermission, start or deathmatch spot (p_hud.go:144);
- `ClientObituary` in coop with a NULL attacker;
- `G_UseTargets` message with a NULL activator;
- `use_target_changelevel` with NULL `other` in dm.

**Status:** mitigated.

### G-10 — low — ctf `playerlist` / `stats` pad names by runes (`%-16.16s`)

- **Location:** `g_ctf.go:3896` `cLeftPrec`, used by `CTFStats` and `CTFPlayerList` (C:
  `ctf/g_ctf.c:3865`, `:3912`).
- **Problem:** Go's fmt counts width and precision in runes, C counts bytes. A UTF-8 netname
  (e.g. "Jürgen", which browser clients send easily) shifted the columns by one per extra byte.
- **Fix:** pad and truncate by bytes.
- **Test:** `TestReviewCTFPlayerListBytePadding`.
  - red: `"  1 Jürgen           00:00…"`, want `"  1 Jürgen          00:00"`
  - green: PASS.

### G-13 — low — `target_speaker` skips Com_sprintf's overflow message

- **Location:** `g_target.go:118` (C: `g_target.c:91`).
- **Problem:** For a noise of 60 or more characters without `.wav`, C prints
  `Com_sprintf: overflow of N in 64` through dprintf.
- **Fix:** use `hudComSprintf`, the existing Com_sprintf emulation. It is covered by the goldens'
  normal path and no dedicated test was added (low).
- **Status:** fixed.

### G-14 — low — `vtos` skips Com_sprintf's 32-byte truncation and message

- **Location:** `g_utils.go:237` (C: `g_utils.c:303`).
- **Problem:** It only matters for origins beyond ±1e8, and only in dprintf text.
- **Status:** open.

### G-15 — low — `func_explosive` with `mass <= -25`

- **Location:** `g_misc.go:859` (C: `g_misc.c:790`).
- **Problem:** C's `while(count--)` with a negative count throws debris until `G_Spawn` fails, which ends
  the server with `ED_Alloc: no free edicts`. Go's `for count > 0` throws none.
- **Status:** open. Go is more robust, and it deviates only where C errors out.

### G-16 — low — level name of 64 characters or more in the help computer

- **Location:** g_spawn.go:850, p_hud.go:303.
- **Problem:** C's unterminated `strncpy` makes HelpComputer print the map name right after the level
  name. Go prints only the 64 characters.
- **Status:** open.

### G-17 — info — C out-of-bounds or undefined behaviour that Go resolves cleanly

- HelpComputer text over 1023 bytes: C sends a non-terminated buffer.
- `BeginIntermission` coop key stripping: C loops over `MAX_ITEMS` past the end of `itemlist`.
- `CTFSay_Team` with a trailing `%`: already documented in docs/CTF.md.
- `func_areaportal` with a negative style: `cmodel` raises ERR_DROP, where C writes out of bounds
  (engine side).

**Status:** open, by design.

### G-18 — info — `players` sort stability

- **Location:** `Cmd_Players_f`.
- **Problem:** It uses `sort.SliceStable`, which matches glibc's merge-sort `qsort`. glibc releases that
  switched `qsort` to introsort would order players with equal frags differently.
- **Status:** open. Keep it pinned to the oracle's glibc.

### G-19 — info — `func_clock` time-of-day style reads the wall clock

- **Location:** `g_misc.go:1710`.
- **Problem:** `time.Now()` mirrors C `time()/localtime()` (faithful), so replays of such a map are not
  deterministic, exactly as in C.
- **Status:** open, by design.

## Out-of-scope findings (for the owners of the other packages)

- **internal/sv:**
  - `recoverError` re-panics every non-`ComError` panic. Any runtime panic in engine code during a frame
    (sv, world, cmodel, net) still kills the whole multi-instance process. The game side is now
    contained (G-02), but a per-instance recover that converts runtime errors into an instance drop
    would close this for the engine too.
  - `recoverError` covers ExecuteText and the frame, but check that `ge.ClientCommand` (from
    `sv_user.go`) and `ReadGame`/`ReadLevel` (`sv_ccmds.go`) always run under it.
- **internal/sv `readServerFile`:** latched cvars from `server.ssv` (maxclients, maxentities, deathmatch,
  …) are force-set without range checks. A tampered file can set `maxclients` above `MAX_CLIENTS` or
  `maxentities` above `MAX_EDICTS` before `initGame` allocates. G-04 rejects a game blob that doesn't
  match, but the engine should clamp these values first.
- **internal/sv `AddCommandString`:** commands issued by the game (`gamemap`, ctf `kick`, `menu_loadgame`)
  run on the instance console. G-11 sanitizes the game side. The engine could additionally restrict
  which commands game text may issue.
- **Configstring overflow:** a long `skin` userinfo reaches `CS_PLAYERSKINS`. It was not verified that
  `sv` reproduces C's 64-byte slot semantics without overflowing into the next configstring.
- **docs/PARITY.md:** the deliberate deviations G-06, G-09 and G-11 (plus the G-02 containment and the
  save validation G-03/04/05/20) should be added to its TODO-IMPROVE/deviation list by the coordinator.

## Tests added

| File | Content |
|---|---|
| `gametest/review_test.go` | The focused regression test for each fixed finding (`TestReview*`). |
| `gametest/robust_test.go` | Contains: <ul><li>the hostile-input harness (`monkey`, `classify`, `knownCCrashes`);</li><li>`TestRobustMonkey`;</li><li>`TestRobustConcurrentInstances`.</li></ul> |
| `gametest/robust_ents_test.go` | Random entity-string generator and `TestRobustEntities`. |
| `gametest/robust_save_test.go` | Save mutator, the real load flow, and `TestRobustSave`. |
| `gametest/fuzz_test.go` | `FuzzClientCommand`, `FuzzUserinfo`, `FuzzEntities`, `FuzzReadSave`. |
| `gametest/golden_race_test.go` (`-tags golden`) | `TestGoldenConcurrent`. |

**Knobs.** The default runs are short. Longer runs are set through environment variables:

| Test | Variables |
|---|---|
| `TestRobustMonkey` | `Q2_MONKEY_ITERS`, `Q2_MONKEY_FRAMES`, `Q2_MONKEY_SEED` |
| `TestRobustEntities` | `Q2_ENTS_ITERS`, `Q2_ENTS_FRAMES`, `Q2_ENTS_SEED` |
| `TestRobustSave` | `Q2_SAVE_ITERS`, `Q2_SAVE_SEED` |

**Other change:** `game_test.go` `TestSaveRoundTripSynthetic` now fills the validated index fields with
valid values. Before, it filled them with random ones.

## Gates

- `cd server && go vet ./... && gofmt -l .`: clean (see the final report).
- `go test -race ./internal/game/...`: ok.
- `go test -tags golden ./internal/game/...`: all 29 golden scenarios bit-exact.
- `go test -race -tags golden -run TestGoldenConcurrent`: ok (29 concurrent instances).
