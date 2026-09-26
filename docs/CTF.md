# CTF mode of the game package

The `ctf/` mod (ThreeWave CTF 1.09b, `Quake-2/ctf/*.c`) is ported as a **mode** of the single Go game package
`server/internal/game`, not as a second package. With the ctf module selected, the Go game reproduces the original
`ctf/game.so` bit-exactly (oracle: `oracle/build/bin/oracle_game_ctf`); without it the 3.19 `game/` behaviour is
unchanged (every baseq2 golden stays green).

## Selecting the module

| How | Where |
|---|---|
| `game` cvar = `ctf` at `Init` (the gamedir, exactly what makes the engine load `ctf/game.so`) | `game.New` (auto-detect in `InitGame`) |
| explicit | `game.NewModule(gi, rng, "ctf")` |
| query | `(*Game).IsCTF()` |

`Game.ctfmod` is the module switch. Inside the module, the C tests `ctf->value` are `g.ctfOn()` (module **and**
ctf cvar, which the ctf module registers with default `"1"`). Hunks that are unconditional in `ctf/*.c` are guarded
by `ctfmod` only (e.g. `CTFApplyStrength` in `T_Damage`, `CTFMatchSetup` in `Touch_Item`).

The golden driver (`gametest`) sets `game ctf` before the scenario cvars for `"module":"ctf"` scenarios, like
`oracle/src/game_main.c`, and registers `game` like `FS_InitFilesystem`.

## Files

| Go | C |
|---|---|
| `g_ctf.go` | `ctf/g_ctf.c`, `ctf/g_ctf.h` (teams, flags, grapple, techs, scoreboard, say_team macros, match/election/ghost/admin, warp/boot, CTF menus, `trigger_teleport`/`info_teleport_destination`, `misc_ctf_banner`/`misc_ctf_small_banner`, `info_player_team1/2`), `ctfItemlist`, `ctfSpawns`, the ctf `dm_statusbar` |
| `p_menu.go` | `ctf/p_menu.c`, `ctf/p_menu.h` |
| `g_ctf_test.go` | spawn table vs `ctf/g_spawn.c`, module selection, menu layout |
| `game_test.go` `TestCTFItemTableMatchesC` | `ctfItemlist` vs `ctf/g_items.c` field by field |

State: the g_ctf.c globals (`ctfgame`, the CTF cvars, `flag1_item`/`flag2_item`, the global menu tables that the C
code edits in place) live in `Game.ctfg`. New private fields: `client_respawn_t` (`CtfTeam`, `CtfState`,
`CtfLasthurtcarrier`, ..., `Ghost`, and the fork's `GameHelpchanged`/`Helpchanged`), `gclient_t` (`Inmenu`, `Menu`,
`CtfGrapple`, `CtfGrapplestate`, ..., `Menutime`, `Menudirty`), `level_locals_t.Forcemap`. Menus and ghost pointers
are transient (not saved), like the ctf fork's own savegames.

### Item table

The ctf module's `itemlist[]` order differs from 3.19: `weapon_grapple` is inserted before `weapon_blaster` and
`item_flag_team1/2`, `item_tech1..4` are appended. `Game.itemlist` points at `itemlist` (baseq2) or `ctfItemlist`
(ctf), so item indices, inventories, `CS_ITEMS` configstrings and stats match the running module exactly (this
replaces the plan's "CTF items appended in all modes").

### Spawns

`ctfSpawns` is the ctf `spawns[]`: 3.19's fixed table plus `info_player_team1/2`, `misc_ctf_banner`,
`misc_ctf_small_banner`, `trigger_teleport`, `info_teleport_destination`, minus the monster code that
`ctf/g_spawn.c` compiles out (`#if 0 // remove monster code`: all `monster_*`, `misc_actor`, `misc_insane`,
`target_actor`, `monster_commander_body`, `turret_*`). Those classnames print `"%s doesn't have a spawn function"`
and the entity stays in use (as in C); the monster files' `RegisterSpawn` table is not consulted in ctf mode.

## //ZOID hunks and base-vs-fork branches

`ctf/` forks an **older** game base than 3.19. Every place where `ctf/*.c` differs from `game/*.c` is applied inline
in the Go file of the same name, guarded by the module. "3.19" is the baseq2 behaviour (unchanged), "ctf" the ctf
module's. Hunks marked **fork** are base differences in non-CTF code (the older base), the others are `//ZOID`
CTF additions.

| File | Function | 3.19 | ctf |
|---|---|---|---|
| g_main.go | `InitGame` (C: g_save.c) | registers `maxspectators`, `spectator_password`, `filterban` | **fork**: not registered; forces `deathmatch 1` (dprintf "Forcing deathmatch.") and `coop 0`; registers `capturelimit`, `instantweap`; `CTFInit` (ctf cvars) |
| g_main.go | `EndDMLevel` | - | `level.forcemap` (warp) wins over the map list |
| g_main.go | `CheckDMRules` | - | `CTFCheckRules` (elections, match timer, capturelimit) ends the level; no time/frag limit in match mode |
| g_main.go | `ExitLevel` | `gamemap` cmd, clear, `ClientEndServerFrames` | clears intermission first, `CTFNextMap` (post-match back to setup) may return early, `changemap` cleared after |
| g_items.go | `itemlist` / `FindItem*` / `GetItemByIndex` / `InitItems` / `SetItemNames` | 3.19 table | `ctfItemlist` |
| g_items.go | `DoRespawn` | random team member | weapons-stay + ctf: only the team master |
| g_items.go | `Drop_Ammo` | "Can't drop current weapon" check for grenades | **fork**: no check |
| g_items.go | `MegaHealth_think`, `Pickup_Health` | - | regen tech disables the mega health decay/timer; >25 health items cap at 250 |
| g_items.go | `Touch_Item` | - | no pickups during match setup |
| g_items.go | `SpawnItem` | - | flags freed when the ctf cvar is 0; flags think `CTFFlagSetup` |
| g_combat.go | `CheckPowerArmor` | power shield 2 damage/cell | 1 damage/cell |
| g_combat.go | `M_ReactToDamage` | clears `AI_SOUND_TARGET`; "meant to shoot us" / "not us" cases | **fork**: older logic (no flag clear, one "help our buddy" branch, unconditional `FoundTarget`) |
| g_combat.go | `CheckTeamDamage` | false | same ctf team -> true |
| g_combat.go | `T_Damage` | - | strength tech, team armor protect (`DF_ARMOR_PROTECT`), resistance tech, `CTFCheckHurtCarrier`, no health loss/pain during match setup |
| g_cmds.go | `SelectNextItem`/`SelectPrevItem` | chase next/prev | menu cursor first |
| g_cmds.go | `Cmd_Drop_f` | - | `drop tech` |
| g_cmds.go | `Cmd_Inven_f` | - | closes a menu; opens the join menu without a team |
| g_cmds.go | `Cmd_InvUse_f` | - | menu select |
| g_cmds.go | `Cmd_Kill_f` | - | no suicide as observer (`SOLID_NOT`) |
| g_cmds.go | `Cmd_PutAway_f` | - | closes a menu, `update_chase` |
| g_cmds.go | `Cmd_Say_f` | inline flood check | same code as the fork's `CheckFlood` (shared, identical behaviour) |
| g_cmds.go | `ClientCommand` | `say_team` -> `Cmd_Say_f`, `playerlist` -> `Cmd_PlayerList_f` | `say_team`/`steam` -> `CTFSay_Team`; CTF commands `team id yes no ready notready ghost admin stats warp boot playerlist observer`; `Cmd_LastWeap_f` ported (unbound, as in C) |
| g_chase.go | `UpdateChaseCam` | chase target spectator check, `ChaseNext` on loss, `PM_DEAD`/death view for dead targets | **fork**: target lost -> stop chasing; always `PM_FREEZE`/target view; ZOID "Chasing %s" layout |
| g_chase.go | `ChaseNext`/`ChasePrev` | skip spectators | **fork**: skip `SOLID_NOT` |
| g_func.go | `train_next` | teleporting path_corner sets `EV_OTHER_TELEPORT` | **fork**: no event |
| g_misc.go | `ThrowClientHead` | body-queue heads clear think/nextthink | **fork**: not cleared |
| g_misc.go | `BecomeExplosion1` | - | flags reset to base ("The %s flag has returned!", always naming RED as in C), techs respawn |
| g_misc.go | `path_corner_touch` | teleport sets `EV_OTHER_TELEPORT` | **fork**: no event |
| g_misc.go | `TH_viewthing` | - | **fork**: `robotron[]` spawnflags cycling (model index 0) |
| g_misc.go | `teleporter_touch` | - | resets the grapple |
| g_phys.go | `SV_FlyMove` | skips identical planes (`VectorCompare`) | **fork**: no plane compare |
| g_phys.go | `SV_Physics_Step` | returns if the entity was freed by a trigger | **fork**: no inuse check |
| g_spawn.go | `ED_CallSpawn` | `spawns` + monster `RegisterSpawn` | `ctfSpawns` only |
| g_spawn.go | `SpawnEntities` | - | `CTFSpawn` (reset `ctfgame`, tech spawner, competition setup) |
| g_spawn.go | `SP_worldspawn` | `dm_statusbar` with spectator/chase blocks; sexed `#w_*.md2` model indices | ctf cvar: `ctf_statusbar` + flag image precaches, else the fork's shorter `dm_statusbar`; **fork**: no sexed model indices (`#if 0 //DISABLED`, so later model indices differ) |
| g_svcmds.go | `ServerCommand` | `addip removeip listip writeip` | **fork**: only `test` |
| g_weapon.go | `fire_blaster` | `SVF_DEADMONSTER` | `SVF_PROJECTILE` (8) |
| g_weapon.go | `bfg_think` | - | no laser on team mates |
| p_client.go | `IsFemale` / `IsNeutral` | `gender` userinfo; neutral messages | **fork**: first letter of `skin`; no neutral |
| p_client.go | `ClientObituary` | - | `MOD_GRAPPLE` message |
| p_client.go | `player_die` | coop key handling; clears `FL_POWER_ARMOR`; inventory cleared only on the first death call | `modelindex3` cleared; telefrag at start by a team mate; `CTFFragBonuses`; grapple/flag/tech drop; help only if scores not shown; **fork**: no `FL_POWER_ARMOR` clear, inventory `memset` on every call; gib sets death anim |
| p_client.go | `InitClientPersistant` | - | grapple in the inventory, `lastweapon = blaster` |
| p_client.go | `InitClientResp` | - | keeps `ctf_team`/`id_state`, `CTFAssignTeam` |
| p_client.go | `SaveClientData` / `FetchClientEntData` | `savedFlags` (godmode, notarget, power armor) | **fork**: `powerArmorActive` (power armor only) |
| p_client.go | `SelectSpawnPoint` | - | `SelectCTFSpawnPoint` |
| p_client.go | `PutClientInServer` | coop: helpchanged copy; spectator spawn | **fork**: coop key loop over the item table, no spectators; `CTFStartClient` (observer + join menu) |
| p_client.go | `ClientUserinfoChanged` | `spectator` userinfo | **fork**: none; `CTFAssignSkin` (team skins) |
| p_client.go | `ClientConnect` | IP filter, spectator password/limit | **fork**: password only; `ctf_team = -1` (force team join) |
| p_client.go | `ClientDisconnect` | - | drops flag/tech |
| p_client.go | `ClientThink` | spectator buttons/chase, chase update | chase target: return after the angles; grapple pull; no attack as observer; regen tech; chase cams; dirty menu update |
| p_client.go | `ClientBeginServerFrame` | spectator respawn; no weapon think for spectators | **fork**: no spectators; no weapon think for noclip; respawn in match mode |
| p_hud.go | `BeginIntermission` | - | `CTFCalcScores` |
| p_hud.go | `DeathmatchScoreboardMessage` | skips spectators | `CTFScoreboardMessage` (reproduces the C entry duplication) |
| p_hud.go | `Cmd_Score_f`, `Cmd_Help_f` | `pers.helpchanged` | closes menus; **fork**: `resp.game_helpchanged`/`resp.helpchanged` |
| p_hud.go | `G_SetStats` | `STAT_SPECTATOR = 0` (stat 17) | **fork**: `resp.helpchanged`; `SetCTFStats` (stats 17-28) |
| p_view.go | `P_FallingDamage` | - | none while/just after grappling |
| p_view.go | `G_SetClientEffects` | - | `CTFEffects` (flag effects/model); quad/pent shells blink every 8 frames |
| p_view.go | `G_SetClientSound` | `pers` help beeps | **fork**: `resp` |
| p_view.go | `G_SetClientFrame` | - | standing frame on the grapple |
| p_view.go | `ClientEndServerFrame` | spectator stats, `G_CheckChaseStats` | **fork**: stats unless chasing; chase follower stats copy (`STAT_LAYOUTS = 1`); menu refresh with the scoreboard |
| p_weapon.go | `Weapon_Generic` | one frame | `Weapon_Generic2` + haste/grapple second frame; `instantweap`; strength/haste fire sounds |
| p_weapon.go | `weapon_grenade_fire` | returns if `health <= 0` before the animation | **fork**: no check |

Unported C (debug only in both bases): none beyond what 3.19 already leaves out. `stricmp` in the fork is
`Q_stricmp` (Linux build).

## Not reproduced / not applicable

- Savegames: the ctf fork's `g_save.c` differs (no function/mmove relocation, `savefields`); the Go save format is
  shared by both modes. `ctfgame`, menus and ghost links are not saved (neither are they in C).
- C undefined behaviour is not emulated: `CTFSay_Team` with a trailing `%` stops there (C copies the terminator and
  reads past it); buffer overflows of the 1400/1024-byte C buffers are not modelled.

## Fixtures

`fixtures/scenarios/game/ctf_demo1.json` (random walks) and the synthetic `ctf_*.json` written by
`oracle/scripts/gen_ctf_scenarios.py` (demo1 start area, monsters stripped):

| Scenario | What |
|---|---|
| ctf_flags | take, capture (with assist bonuses), "Winners don't drop flags", carrier suicide (dead drop), return by touch, auto-return, give-all fights |
| ctf_capturelimit | capturelimit 1: capture ends the level, intermission, blinking headers, exit (`gamemap`) |
| ctf_grapple | grapple fire/pull/hang/release, weapon switch while grappled, grappling a player, hasted grapple |
| ctf_techs | give/pickup/one-tech rule/drop tech, tech effects in fights, say_team macros, id view, tech respawn |
| ctf_teams | `team` variants, join/chase/credits menus, observer, chase camera, say_team/steam, playerlist/stats, team change while alive |
| ctf_match | admin election, admin menu, match mode, ready/notready, match start with ghosts, timers, stats, boot, match end, `CTFNextMap`, settings menu |
| ctf_vote | warp list/unknown/elections voted no, election timeout, match request from the join menu, admin password, force start, warp as admin |

All run in `go test -tags golden ./internal/game/gametest/` (`mustPass`).
