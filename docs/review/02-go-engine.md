# Review 02: Go engine (sv, net, world, cmodel, pmove, bsp, qcommon, fakeclient, wsbridge)

Scope: `server/internal/{sv,net,world,fakeclient,qcommon/*,cmodel,pmove,bsp,q2const,testutil}`,
`server/cmd/{wsbridge,genconst}`. The C tree in `Quake-2/` was used as the oracle. Behaviour that reproduces C
was only treated as a bug when a hostile peer could use it: a panic, a hang, unbounded work, an
out-of-range index, an auth/rcon bypass, or a remote shutdown. Remote peers include network clients and uploaded
maps (users can upload paks through `POST /api/v1/paks` and start games on them).

## Summary

| ID | Sev | Area | Title | Status |
|---|---|---|---|---|
| ENG-01 | critical | sv_user.go | A `clc_move` in the last byte of a full datagram panics and kills the whole process | fixed |
| ENG-02 | critical | cmodel.go | A node cycle in an uploaded map causes an endless loop or a fatal stack overflow that no recover can catch | fixed |
| ENG-03 | high | cmodel.go | Shared subtrees (a DAG) in an uploaded map make traces take 2^N steps, so the instance hangs forever | fixed |
| ENG-04 | high | cmodel.go | A leaf cluster < -1 indexes visbits with a negative index, panicking on every frame | fixed |
| ENG-05 | high | sv_user.go | `configstrings`/`baselines` with a negative start spin 2^31 iterations (about 10 s each, 70 s per packet) | fixed |
| ENG-06 | high | server.go | Runtime panics escape `recoverError`, so one bug kills every game in the process | fixed |
| ENG-07 | high | sv_user.go | `begin` on a pic/cinematic server makes the game abort the server (ERR_DROP) | fixed |
| ENG-08 | high | sv_user.go / cmd.go | `say $rcon_password`: client macro expansion discloses any server cvar | fixed |
| ENG-20 | high | sv_init.go | MapCache is keyed by 64-bit FNV-1a: a crafted colliding map poisons a shared map; the cache is unbounded | fixed |
| ENG-09 | medium | sv_ccmds.go | Client edicts dangle after `load`; a later `save` dereferences nil | fixed |
| ENG-10 | medium | sv_user.go | A clc_userinfo flood fills every other client's reliable buffer, so they are all dropped | open (C-faithful) |
| ENG-11 | medium | sv_main.go | One IP can hold every slot with idle connects (different qports, 125 s timeout) | open (C-faithful) |
| ENG-12 | medium | sv_main.go / net_chan.go | UDP only: spoofed packets can rebind a client's port or desync its sequence, and `status` gives about 100x reflection | open (C-faithful) |
| ENG-13 | low | sv_send.go | While `serverrecord` runs, more than 1400 bytes of multicast in one frame causes ERR_FATAL | open (C-faithful) |
| ENG-14 | low | sv_main.go | rcon: no rate limit, the password is logged in "Rcon from" lines, the compare is not constant time | open (C-faithful) |
| ENG-15 | low | sv_main.go | Log flooding and injection (2 KB of echoed OOB text, names containing newlines) | open (C-faithful) |
| ENG-16 | low | sv_main.go | The `ip` userinfo key is not forced when the userinfo is near 512 bytes | open (C-faithful) |
| ENG-17 | low | sv_ccmds.go | "Bad savedir." does not return (C quirk); relies on SaveStore/host slot validation | open (C-faithful) |
| ENG-18 | low | cmd/wsbridge | Any Origin is accepted and connections are unlimited (one UDP socket each); there is no SSRF because the target is fixed | open |
| ENG-19 | low | sv_main.go | A spoofed getchallenge flood evicts challenges, and each getchallenge draws from the game RNG | open (C-faithful) |

All critical and high findings were fixed with strict TDD (red, then green). ENG-09 was also fixed because the fix
is cheap and it crashed the process before ENG-06.

## Fixed findings

### ENG-01 (critical): clc_move checksum slice panic

- **Where:** `server/internal/sv/sv_user.go:466-478` (SV_ExecuteClientMessage, clc_move).
- **What:** the checksum range `nm.Data[checksumIndex+1:end]` had only `end` clamped to the buffer size. If a
  spawned client sends a full `MAX_MSGLEN` (1400 byte) datagram whose last byte is `clc_move`, then
  `checksumIndex+1 = 1401 > end = 1400` and Go raises a runtime panic. That is not a Com_Error, so `recoverError`
  re-panicked it and the host instance goroutine (which has no recover) took down the **whole process**, with every
  game on it.
- **Fix:** clamp the start to `end` as well. C reads stale bytes past `net_message_buffer` (UB); now the CRC runs over an
  empty range and the command is ignored as a checksum failure. Valid packets are unchanged.
- **Test:** `TestClcMoveAtEndOfFullPacketDoesNotPanic` (internal/sv/robust_test.go)
  - red: `clc_move at end of full packet panicked: runtime error: slice bounds out of range [1401:1400]`
  - green: `--- PASS: TestClcMoveAtEndOfFullPacketDoesNotPanic (0.00s)`

### ENG-02 (critical): BSP node cycle → endless loop / fatal stack overflow

- **Where:** `server/internal/cmodel/cmodel.go:500` (new `checkNodeCycles`, called from `validate`).
- **What:** `validate()` checked that node children were in range, but not that the graph is a tree. With a child
  pointing back at itself or an ancestor, `CM_PointLeafnum_r` loops forever, and `CM_RecursiveHullCheck`,
  `CM_BoxLeafnums_r` and `CM_HeadnodeVisible` recurse until `fatal error: stack overflow`. That error cannot be
  recovered. An authenticated user could upload a pak with such a map, start a game, and kill the server process.
- **Fix:** reject maps whose node graph has a cycle (iterative DFS). The check is combined with ENG-03.
- **Test:** `TestLoadMapRejectsNodeCycles` (internal/cmodel/robust_test.go): self loop, back edge, submodel loop.
  - red: `self: map with a node cycle was accepted` (also for "back edge" and "submodel loop"). Loading that map and
    tracing once gave `runtime: goroutine stack exceeds 1000000000-byte limit / fatal error: stack overflow`.
  - green: `ok quake2web/server/internal/cmodel`

### ENG-03 (high): shared subtrees → exponential traversal

- **Where:** `server/internal/cmodel/cmodel.go:500` (`checkNodeCycles`: in-degree check).
- **What:** a DAG is acyclic but still harmful. Take 64 nodes where both children of each node are the next node.
  Any walk that visits both sides then takes 2^64 steps: a trace straddling the planes, `CM_BoxLeafnums_r`, or
  `CM_HeadnodeVisible` in `SV_BuildClientFrame`. The instance goroutine spins forever and `Stop()` cannot end it.
- **Fix:** every node may have at most one parent. Compiled BSPs are trees: model headnodes are roots and every other
  node has one parent. The golden BSP and trace tests on demo1-3 still pass.
- **Test:** `TestLoadMapRejectsSharedSubtrees`
  - red: `map with a shared subtree was accepted`, `exponential DAG accepted`. Tracing that map gave
    `panic: test timed out after 15s`.
  - green: PASS.

### ENG-04 (high): negative leaf cluster in CM_HeadnodeVisible

- **Where:** `server/internal/cmodel/cmodel.go:1613`.
- **What:** leaf clusters come from the map as `int16`. Only `-1` was special-cased, so a cluster such as `-26`
  indexed `visbits[-4]`. This happens for every entity spanning more than 16 clusters or 128 leafs
  (`NumClusters == -1`), on every frame. It was found by the new structured map fuzzer. Before ENG-06 this killed the
  process; now it kills the instance.
- **Fix:** any negative cluster counts as not visible. This matches `maskBit` and the existing PARITY entry.
- **Tests:** `TestHeadnodeVisibleNegativeCluster`, plus the fuzz crasher kept as a seed at
  `internal/cmodel/testdata/fuzz/FuzzLoadMapStructured/768063283d3a1031`.
  - red: `HeadnodeVisible panicked: runtime error: index out of range [-4]` (fuzz: `index out of range [-26]`)
  - green: PASS.

### ENG-05 (high): negative configstrings/baselines start spins 2^31 times

- **Where:** `server/internal/sv/sv_user.go:115-122` (SV_Configstrings_f) and `:160-167` (SV_Baselines_f).
- **What:** an earlier memory-safety guard skipped negative slots one at a time (`if start >= 0`). So
  `configstrings <spawncount> -2147483648` looped about 2^31 times, and a connected client can send 7 such commands
  per datagram. It needs only the spawncount, which the server itself sends in the stufftext. The instance was frozen
  for 13.6 s (configstrings) or 67 s (baselines) per packet, and this repeats forever.
- **Fix:** a negative start begins at 0. C read before the arrays (UB), so this only affects invalid input.
- **Tests:** `TestConfigstringsNegativeStartIsBounded`, `TestBaselinesNegativeStartIsBounded`
  - red: `configstrings with negative start took 13.639277065s`,
    `baselines with negative start took 1m6.801564357s`
  - green: PASS (0.01 s, 0.00 s).

### ENG-06 (high): runtime panics escape instance recovery

- **Where:** `server/internal/sv/server.go:474-500` (`recoverError`) and the new `InternalError` type.
- **What:** `recoverError` recovered only `shared.ComError` and re-panicked everything else. The host instance loop
  (`internal/host/host.go` `run`) has no recover, so any Go bug under HandlePacket/Frame/ExecuteText (sv, game,
  cmodel, pmove) reached from a packet or a map killed **every** game on the process.
- **Fix:** a runtime panic now ends only that instance, like ERR_FATAL. It logs the value and stack, runs `svShutdown`
  under its own recover (state may be inconsistent), sets `killed`, and returns `*InternalError`. The fuzz targets use
  `failOnInternal` so they still report these as crashes.
- **Opt-in:** `Config.DropClientOnPanic` (suggested by the coordinator) contains a runtime panic raised while
  executing one client's message by dropping only that client. `HandlePacket` then returns an `*InternalError` with
  `ClientDropped` and the level keeps running. It is off by default because game state touched by the panicking
  code may be inconsistent, and because the host's `TestInstancePanicStopsOnlyThatInstance` expects the instance to
  end. The host can enable it and update that test.
- **Tests:** `TestRuntimePanicEndsOnlyTheInstance` (a game whose ClientCommand writes to a nil map),
  `TestRuntimePanicInFrameEndsOnlyTheInstance`, `TestRuntimePanicInClientMessageDropsOnlyThatClient`
  (DropClientOnPanic).
  - red: `game command panicked: assignment to entry in nil map`
  - green: `sv: internal error: assignment to entry in nil map`, PASS. The host test suite, including the network
    reviewer's `TestInstancePanicStopsOnlyThatInstance`, still passes.

### ENG-07 (high): `begin` on a non-game server aborts the instance

- **Where:** `server/internal/sv/sv_user.go:199-212` (SV_Begin_f).
- **What:** SV_Begin_f checks neither the client state nor `sv.state`. On a pic or cinematic server, such as the coop
  `victory.pcx` end screen, a client that echoes the spawncount makes the game spawn a player on the empty map. The
  game then calls `gi.error("Couldn't find spawn point")`, which is ERR_DROP, and the server shuts down for everyone.
  Real clients never send `begin` there (C: `playernum == -1` means a cinematic, and the client only sends
  `nextserver`).
- **Fix:** after the spawncount check (so C's "different level" → `SV_New_f` path is unchanged, including demo
  servers), `begin` is ignored unless `sv.state == ss_game`.
- **Test:** `TestBeginOnPicServerIgnored` (coop and deathmatch)
  - red: `[coop 1]: begin on a pic server shut it down: Game Error: Couldn't find spawn point`
  - green: PASS.

### ENG-08 (high): client `$cvar` expansion discloses server cvars

- **Where:** `server/internal/sv/sv_user.go:337-343` and `publicCvar`; `server/internal/qcommon/cmd/cmd.go`
  (`MacroAllow`, macroExpand).
- **What:** `SV_ExecuteUserCommand` tokenizes client text with macro expansion (`Cmd_TokenizeString(s, true)`). So
  `say $rcon_password` broadcast the value to every player (verified: `p: hunter2`). The same works for `password`,
  `spectator_password`, CTF `admin_password` and every other cvar. If rcon_password is set (rcon is still answered
  over UDP and WebSocket), any player gets full rcon, which would be critical. The deployment default leaves it
  unset and the API cannot set it, hence high.
- **Fix:** while a client command is tokenized, `$name` may only name CVAR_SERVERINFO cvars, which are already public
  through `status`/`info`. Any other name expands to `""`, as an unknown cvar does. Everything else is unchanged:
  expansion of public cvars, the quote handling, the stale-buffer quirk and the line-length checks.
- **Test:** `TestClientMacroCannotReadPrivateCvars`
  - red: `rcon_password leaked to a client through $ expansion`
  - green: PASS (and `say $hostname` still expands to `p: pubhost`).

### ENG-20 (high): MapCache FNV-64 collision poisoning; unbounded cache

- **Where:** `server/internal/sv/sv_init.go` (`MapCache`). This was reported by the network reviewer through the
  coordinator.
- **What:** the key was `name#len#fnv64a(raw)`, and FNV-1a is not collision resistant. A parallel rho search found a
  colliding pair of 8-byte suffixes for the synthetic map in **23 s** on 4 cores. A user map crafted to collide with a
  public map, and loaded first, would replace the collision model and entity string of every later game on that map.
  Every distinct map also stayed cached forever. The host now shares the cache only for the demo pakset, but the root
  cause was here.
- **Fix:** the key is the name plus SHA-256 of the bytes. The cache is bounded (`Max`, default
  `DefaultMapCacheEntries = 16`) and evicts the least recently used entry.
- **Tests:** `TestMapCacheCollisionDoesNotPoison` (a real FNV-1a-64 collision vector, checked in the test),
  `TestMapCacheIsBounded` (internal/sv/mapcache_test.go)
  - red: `cache returned the colliding (attacker's) map for different bytes`, `cache holds 48 maps (bound 16)`
  - green: PASS.

### ENG-09 (medium): client edicts dangle after `load`

- **Where:** `server/internal/sv/sv_ccmds.go:410-416` (SV_ReadServerFile).
- **What:** `SV_InitGame` points `svs.clients[i].edict` into the edict array, then `ge->ReadGame` reallocates it. In C
  the pointers dangle into freed memory. In Go they point at the old array, whose edicts have no client. Until the
  client reconnected, `save` (operator console) dereferenced nil (runtime panic, which killed the process before
  ENG-06). `gamemap` would also toggle `inuse` on the wrong array for free coop slots.
- **Fix:** re-point every slot at `EDICT_NUM(i+1)` after ReadGame.
- **Test:** `TestLoadgameRepointsClientEdicts`
  - red: `client 0 edict points at a stale edict array`, `save after load: sv: internal error: runtime error:
    invalid memory address or nil pointer dereference`
  - green: PASS.

## Open findings (not fixed: C-faithful protocol behaviour or low impact)

- **ENG-10 (medium) userinfo flood:** `sv_user.go:423` (`clc_userinfo`). The number of `clc_userinfo` per packet is not
  limited (only stringcmds are). Each one calls `ClientUserinfoChanged`, which sets `CS_PLAYERSKINS+n`, and that goes
  out as a reliable MULTICAST_ALL_R configstring. Measured: one datagram of 24 userinfos put 1195 of the 1384 bytes of
  reliable buffer into every other client. Two packets overflow them, and they are dropped ("overflowed"). The C
  behaviour is identical. Recommendation: time-based userinfo rate limiting (r1q2/q2pro style). This is a protocol
  deviation, so it needs a product decision.
- **ENG-11 (medium) slot exhaustion:** `sv_main.go:204-235`. A reconnect is only matched on the same base plus the same
  qport or port. One IP can therefore connect with different qports until the server is full, and each slot is held
  for `timeout` (125 s) without sending anything. Recommendation: a per-IP connection cap in the host (out of scope).
- **ENG-12 (medium, UDP transport only) spoofing:** `sv_main.go:454` ("fixing up a translated port") and
  `net_chan.go:228`. A spoofed datagram with the victim's IP and qport rebinds the victim's port, or jumps its
  sequence to 0x7fffffff so every real packet is later dropped as "out of order". `status` answers about 15 bytes
  with up to 1400 (reflection amplification). This does not apply to WebSocket, where the base is the real peer IP
  and each connection has its own port.
- **ENG-13 (low) serverrecord overflow:** `sv_send.go:125`. `svs.demo_multicast` has no allowoverflow, so more than
  1400 bytes of multicast in one frame while recording causes ERR_FATAL and the instance ends (as in C). This only
  happens while an operator runs `serverrecord`.
- **ENG-14 (low) rcon hygiene:** `sv_main.go:290-325`. There is no rate limit on password guesses and the compare is
  not constant time. `Rcon from %s:\n%s` and `Bad rcon from` log the whole packet, **including the password**, and
  rcon is accepted over WebSocket too. Recommendation: stop logging the argument, or disable OOB rcon entirely
  (PARITY: replaced by the admin API).
- **ENG-15 (low) logs:** `sv_main.go:365` echoes up to 2047 attacker-controlled bytes per packet into the server log,
  and names containing `\n` (from clc_userinfo) inject lines into `status` replies and logs.
- **ENG-16 (low) ip key:** `sv_main.go:169`. When the userinfo is close to 512 bytes, `Info_SetValueForKey("ip")` fails
  ("Info string length exceeded") after the client's own `ip` was removed, so the game sees no ip. Game-level IP
  filters are replaced by host bans (PARITY), so the impact is small.
- **ENG-17 (low) savedir:** `sv_ccmds.go:521,568` print "Bad savedir." and carry on, as C does. The host validates
  slots with `api.ValidSlot` before issuing save/load, and MemSaveStore is key-based.
- **ENG-18 (low) wsbridge:** `cmd/wsbridge/main.go:28` sets `InsecureSkipVerify: true`, so any web page a developer
  visits can use a running bridge. There is no connection cap, and each connection opens a UDP socket. There is no
  open relay or SSRF: the UDP target is fixed by the `-udp` flag and datagrams are only sent to it.
- **ENG-19 (low) challenges:** `sv_main.go:111`. Spoofed getchallenges from many addresses evict real challenges, and
  every getchallenge draws `rand()` from the instance RNG shared with the game (as C does). This only works over UDP.

## Areas reviewed without findings

- **net:** netchan (sequence, reliable and qport handling matches net_chan.c; runt datagrams read -1 as C does), mem,
  WS and UDP transports. The `wsConn.err`/close ordering is race-free, `Recv` timeouts keep the socket open, the
  read limit is 64 KB, and there are no goroutine leaks: the readers exit on close or ctx.
- **msg:** reads stay within `CurSize`, strings are capped at 2047 bytes, and the usercmd delta is exercised by the
  fuzzers.
- **world:** sv_world.c port; area lists are bounded by the caller's slice and cluster lists by MAX_ENT_CLUSTERS.
- **pmove:** every loop has a fixed bound (bumps, clip planes, snap offsets) and touch lists are capped. It was fuzzed
  through real ClientThink with valid-checksum usercmds.
- **qcommon/cmd:** tokenizer, macro loop limit (100) and alias loop limit.
- **qcommon/shared:** info-string helpers.
- **cvar, crc, md4, crand.**
- **bsp.Parse:** lump bounds and element maximums are checked.
- **Save/load via SaveStore, serverrecord, rate drop, kick/drop, level change with connected clients, reconnect:**
  reviewed and covered by the existing sv tests plus the new ones.
- **Not reviewed in depth:** `fakeclient` (test/bot client) and `genconst` (offline generator); neither is exposed to
  the internet.

## Fuzzing

New targets:

- `internal/sv/fuzz_test.go`:
  - `FuzzClientMessage`: a spawned or connecting client on the synthetic map with the **real game module**. Up to 16
    datagrams per input, either as netchan payloads or as raw datagrams with arbitrary headers, or as clc_move with a
    valid checksum so that ClientThink and Pmove run. A server frame follows every packet.
  - `FuzzConnectionless`: any OOB text (optionally a `connect` with a valid challenge), followed by an arbitrary
    sequenced or runt datagram.
  - With `Q2FUZZ_STRICT=1`, a server shutdown caused by a client (ERR_DROP) also counts as a failure.
- `internal/cmodel/robust_test.go`:
  - `FuzzLoadMapStructured`: patches the synthetic map's cross references, clusters, areas, portals and vis bytes,
    then runs every query the server makes.

Runs on 4 cores (the Go fuzzer's exec counter reports in bursts):

| Target | Time | Execs | Result |
|---|---|---|---|
| FuzzClientMessage | 4 min + 5 min (with checksum mode) + 4 min strict | ≈126k + 36k | no crash |
| FuzzConnectionless | 3 min + 3 min strict | ≈113k + 138k | no crash |
| FuzzLoadMap (existing, raw bytes) | 3 min | ≈197k | no crash |
| FuzzLoadMapStructured | 150 s | – | found ENG-04 |
| FuzzLoadMapStructured (after fix) | 200 s | 1.8M | no crash, no stall |

## Deviations introduced (all invalid or hostile input only; logged in docs/PARITY.md)

1. clc_move checksum start clamped to the buffer end (ENG-01).
2. Node graphs that are not a forest are rejected at map load (ENG-02/03).
3. Negative clusters other than -1 are not visible in CM_HeadnodeVisible (ENG-04).
4. A negative `configstrings`/`baselines` start begins at 0 (ENG-05).
5. A runtime panic ends only the instance and returns `*sv.InternalError` (ENG-06).
6. `begin` is ignored unless `sv.state == ss_game` (after the spawncount check) (ENG-07).
7. Client `$name` expansion only reads CVAR_SERVERINFO cvars (ENG-08). A client command containing `$private_cvar`
   now expands to "" instead of the value. No golden scenario uses `$`.
8. Client edicts are re-pointed after ReadGame (ENG-09).
9. The map cache is keyed by SHA-256 and LRU-bounded (ENG-20; no C counterpart).

## Out-of-scope findings (for the host/game/api owners)

- **host (high, since fixed by the network reviewer):** `Instance.run` had no recover, and the `control`/`pending`
  closures ran outside sv's recovery. `Instance.step` now recovers per instance. Optionally enable
  `sv.Config.DropClientOnPanic`.
- **host (medium):** there is no per-IP cap on WebSocket connections and slots (see ENG-11), and tickets are optional
  (`RequireTickets`).
- **host (info):** the `DemoCreate` implementation receives the raw `serverrecord` argument; it must sanitise the name
  (not reviewed).
- **game:** about 16 minutes of fuzzing ClientCommand, ClientUserinfoChanged and ClientThink found no crash and no
  ERR_DROP reachable from a client on a running level.

## Gates

- `cd server && go vet ./... && gofmt -l .`: clean.
- `go test -race ./internal/sv/... ./internal/net/... ./internal/world/... ./internal/qcommon/... ./internal/cmodel/...
  ./internal/pmove/... ./internal/bsp/... ./cmd/wsbridge/...` (plus fakeclient): all ok.
- `go test -tags golden ./...`: all ok (bit-exact goldens unchanged).
