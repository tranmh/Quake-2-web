# Review 04: TypeScript engine (q2-shared, q2-protocol, q2-formats, q2-pmove, q2-client, q2-ref)

**Scope.** `web/packages/{q2-shared,q2-protocol,q2-formats,q2-pmove,q2-client,q2-ref}`, checked against the C
oracle in `Quake-2/`.

**Threat model.**
- The browser client connects to servers that users run themselves and plays user-supplied `.dm2` demos, so
  every server message is attacker-controlled.
- Every asset (BSP, MD2, SP2, PCX, WAL, TGA, WAV, CIN) can come from a user-uploaded pak.

**What counts as a bug.** C behaviour that is reproduced faithfully for valid input is not a bug. It only
counts when hostile input turns it into one of these:
- a hang, or a frozen or crashed tab;
- an unbounded allocation;
- persistent harm to the player's account settings;
- disclosure of a secret.

Fixes change behaviour for invalid or hostile input only, apart from the documented `stufftext` restriction.
All deviations are logged in `docs/PARITY.md` (TODO-IMPROVE, "review 04").

**Error handling baseline.** The engine frame loop (`engine.ts` `guard`) catches synchronous exceptions and
turns them into an ERR_DROP disconnect. So a stray TypeError is a *drop* (medium or low). An endless loop, or
growth that never stops, is a freeze (high).

**How the tests were run.**
- **Red run:** a test was run against the pre-fix source (`git show HEAD:<file>` swapped in temporarily).
- **Hanging red runs:** `timeout N npx vitest run -t <name>`. Exit status 124 means the test was still running
  when the timeout killed it.
- **Green:** every package gate passes (see the end of this file).

## Summary

| ID | Sev | Area | Title | Status |
|---|---|---|---|---|
| TSE-01 | critical | cmd.ts / cl_parse.ts | `svc_stufftext` can rewrite bindings and archived cvars (saved to the account), use or leak `rcon_password`, and trigger screenshot downloads | fixed |
| TSE-02 | critical | keys.ts / cvar.ts | Saved config text does not parse back: quotes and the `;` key inject commands that run at every start | fixed |
| TSE-03 | high | cl_scrn.ts | Netgraph loop over `netchan.dropped` (≈2^31 from one packet) freezes the tab for minutes | fixed |
| TSE-04 | high | cl_parse.ts | `svc_download` with a negative size re-parses itself forever | fixed |
| TSE-05 | high | q2-pmove cmodel.ts | A BSP whose node graph has a cycle or shared subtree: endless `CM_PointLeafnum_r`, exponential traces | fixed |
| TSE-06 | high | q2-formats md2.ts | Crafted MD2 counts: multi-GB typed-array allocations before bounds checks; zero-size frames give 2^31 frame objects | fixed |
| TSE-07 | high | q2-formats tga.ts | An 18-byte TGA can claim 65535×65535 pixels, allocating 17 GB before the data runs out | fixed |
| TSE-08 | high | q2-formats wav.ts | WAV rate ≤ 0 / tiny rate / loop marker: RangeError(Infinity) or multi-GB allocation plus ~2e9-iteration loop | fixed |
| TSE-09 | high | q2-formats cin.ts (via cl_cin.ts) | CIN frame with a degenerate Huffman table: 64 MiB allocation and 64 Mi iterations per frame (~17 s each) | fixed |
| TSE-10 | high | cl_parse.ts / cl_tent.ts | Sound on entity −1 / MAX_EDICTS makes every later frame drop; the console cannot even open | fixed (client side); see OOS-1 |
| TSE-11 | medium | cl_main.ts | `CM_LoadMap` race: an older map's collision model overwrites the new one | fixed |
| TSE-12 | medium | cl_view.ts | `CL_PrepRefresh` returns the previous level's in-flight promise, so "begin" is sent unprepped and the loading plaque sticks | fixed |
| TSE-13 | medium | cl_parse.ts / cl_view.ts | Configstring changes that arrive during the async precache/prep are lost for the rest of the level | fixed |
| TSE-14 | medium | cl_main.ts | The receive queue has no bound (background tab plus a flooding server leads to OOM) | fixed |
| TSE-15 | medium | cl_ents.ts | Repeated out-of-order entity numbers make every delta frame bigger than its base (unbounded per-frame work) | fixed |
| TSE-16 | medium | cl_main.ts | Recording while a demo plays strips 8 bytes of real data from each message (`len < 8` gives a RangeError) | fixed |
| TSE-17 | medium | cl_main.ts | `cl_maxfps 0` (also possible via stufftext) stops CL_Frame, freezing screen and console | open (C-faithful) |
| TSE-18 | low | cl_parse.ts | `svc_spawnbaseline` number ≥ MAX_EDICTS gives a TypeError instead of ERR_DROP | fixed |
| TSE-19 | low | cl_view.ts | `handleIds` (sort ordinals) is never cleared, so every model/skin handle stays pinned | fixed |
| TSE-20 | low | cl_main.ts | Demo pacing: messages that fall due during precache are all parsed in one frame afterwards | fixed |
| TSE-21 | low | keys.ts | Clipboard paste leaves `key_linepos` at MAXCMDLINE, past the stored text (C overflows by 1) | fixed |
| TSE-22 | low | console.ts | `condump` does not mask a 0x80 byte to NUL before the end-of-string test as C does | fixed |
| TSE-23 | low | cl_ents.ts | `skinName()` scans the whole regcache for each RF_USE_DISGUISE entity every frame | open |
| TSE-24 | low | cl_main.ts | `timedemo 1` still paces demo playback at 100 ms per message | open |
| TSE-25 | low | cmd.ts | stufftext `connect <addr>` / `quit` / `timescale` / `fixedtime` still allowed | open (by design) |
| TSE-26 | low | layout.ts | A negative `pic` stat looks up `pics/.pcx` every frame (console spam; C reads out of bounds) | open |
| TSE-27 | info | q2-formats wav.ts | `GetWavinfo` reports the header's data length unclamped | open (C-faithful, consumers guarded) |
| TSE-28 | info | q2-formats md2.ts | `parseMd2` does not range-check glcmd vertex indices | open (the renderer validates) |

### New tests

| File | What it covers |
|---|---|
| `web/packages/q2-formats/test/fuzz.test.ts` | Seeded mutation fuzzing of real demo-pak BSP/MD2/SP2/WAL/PCX/TGA/WAV files and a pak directory: anything other than FormatError fails the test. Plus crafted-header allocation tests. Iterations are set with `Q2_FUZZ_ITER`: 400 by default, and 5000 was run clean. |
| `web/packages/q2-pmove/test/cmodel.robust.test.ts` | Hostile node graphs, plus fuzzing of in-range node rewiring with point and box traces. |
| `web/packages/q2-client/test/hostile.test.ts` | Hostile server messages, stufftext restriction, config round trip, async load races. |

## Critical

### TSE-01: `svc_stufftext` is executed with console rights

**Where.** `q2-client/src/cl_parse.ts:598` (svc_stufftext) and `q2-client/src/cmd.ts`. C: `cl_parse.c:687`,
`cmd.c:772 Cmd_ExecuteString`.

**What.** As in C, stuffed text was appended to the command buffer and run like console input. In the browser:
- key bindings and CVAR_ARCHIVE cvars are pushed to the account settings within seconds of any change
  (`host.onConfig`, then `session.ts scheduleConfigSave`), and replayed at every start.

A hostile server could therefore:
- persistently wipe or replace bindings (`unbindall`, `bind w quit`);
- change archived settings (`name`, `sensitivity`, …), or create new archived cvars with `seta` (a TS
  addition);
- read the player's rcon password for other servers: `cmd say $rcon_password`, `rcon status`,
  `set rcon_password $x u`, which turns it into a userinfo cvar;
- spam PNG downloads with `screenshot`.

Aliases defined by the server and exec files it runs were a bypass: a user key bound to `+zoom` would run the
server's alias with full rights.

**Fix.**
- The command buffer records the origin of every character (`cmd_origin`). A line containing server text runs
  restricted.
- In restricted mode:
  - `bind unbind unbindall seta rcon screenshot` are refused with a console note;
  - `set/setu/sets/toggle` and `<cvar> <value>` are refused when the target is `rcon_password` or an existing
    CVAR_ARCHIVE cvar;
  - `$rcon_password` expands to "".
- Aliases defined by restricted text stay restricted when anyone runs them. Text inserted by an alias or `exec`
  inherits the restriction.
- Everything servers really send still works: `cmd …`, `precache`, `changing`, `reconnect`, `disconnect`,
  `spectator 0/1` (a userinfo cvar that is not archived), new non-archived cvars, and aliases.

**Test.** `q2-client/test/hostile.test.ts` › "svc_stufftext runs restricted", 4 tests.
- **Red:** `× cannot change key bindings or archived cvars (they persist in the account config)` —
  `AssertionError: expected '' to be '+forward'`. Also `× cannot read or use the rcon password` and
  `× aliases and exec files defined by the server stay restricted when the user runs them`.
- **Green:** 4/4 pass. The `screenshot` case is in "hostile messages (medium)" › `stufftext cannot trigger
  screenshot downloads`.

### TSE-02: saved config text injects commands at every start

**Where.** `q2-client/src/keys.ts:606` Key_WriteBindings and `q2-client/src/cvar.ts:329` writeVariables. C:
`keys.c:614`, `cvar.c:429` (the C code has a "FIXME: handle quote special" note here).

**What.** Values are written as `bind K "value"` / `set n "value"` with no escaping, and the text is executed at
startup (`engine.ts` configText).
- `bind k a";connect evil;"` is saved as `bind k "a";connect evil;""`, which connects to `evil` at every start.
- The C quirk of writing key 59 as `bind ; "quit"` splits into `bind ` and `"quit"` when executed, so it runs
  `quit` at startup and loses the binding.

With TSE-01 open, a server could plant either. Even without it, a user's own `;` binding broke their config.

**Fix.**
- Key 59 is written as its keyname `SEMICOLON`. Key 34 (`"`) has no name and is not written.
- `"` inside a value is written as `'`.
- Archived cvars whose name contains a quote, `;` or whitespace are skipped.

The existing `keys.test.ts` expectation (`bind ; "say hi"`) was updated to `bind SEMICOLON "say hi"`.

**Test.** `hostile.test.ts` › `bindings / cvar values with quotes and the semicolon key cannot inject commands`
(writes the config, replays it in a fresh client, checks that no connect or quit happened and that the bindings
round-trip).
- **Red:** `AssertionError: expected 2 to be +0`: `quit` ran twice when the saved config was replayed.
- **Green:** pass.

## High

### TSE-03: netgraph `dropped` loop freezes the tab

**Where.** `q2-client/src/cl_scrn.ts:133` CL_AddNetgraph, with `dropped` from `q2-protocol/src/net_chan.ts:273`.
C: `cl_scrn.c:102`.

**What.**
- A packet with sequence 0x7FFFFFFE sets `dropped ≈ 2^31`. `for (i < dropped) SCR_DebugGraph()` then runs after
  every parsed message, with the netgraph enabled by default.
- It takes about 60 s in node (C: a few seconds).
- Combined with stufftext `reconnect` it can be repeated forever.

**Fix.** Samples beyond the 1024-entry ring would be overwritten anyway, so `current` is advanced past them and
only the last 1024 are written. The resulting graph and `current` are identical.

**Test.** `hostile.test.ts` › `netgraph: a sequence jump of 2^31 packets does not freeze the frame`.
- **Red:** `timeout 30 … -t netgraph` → exit status 124 (hung).
- **Green:** pass in < 1 s, with an identical ring state.

### TSE-04: `svc_download` with a negative size loops forever

**Where.** `q2-client/src/cl_parse.ts:157` CL_ParseDownload. C: `cl_parse.c:200`, which has the same flaw.

**What.** `readcount += size` with size = −4 moves back onto the same `svc_download` byte. CL_ParseServerMessage
then parses it again, forever. This is reachable from any server or demo.

**Fix.** `size < 0` (other than the −1 "no file" marker) raises ERR_DROP.

**Test.** `hostile.test.ts` › `svc_download with a negative size drops instead of re-parsing itself forever`.
- **Red:** `timeout 60 … -t svc_download` → exit status 124 (hung).
- **Green:** DropError, pass.

### TSE-05: hostile BSP node graph (cycle or shared subtree)

**Where.** `q2-pmove/src/cmodel.ts:339` (new `checkNodeCycles`, called from loadNodes). C: `cmodel.c:205
CMod_LoadNodes`. This is the same issue as Go review 02 ENG-02/03.

**What.** Children were range-checked but the graph was not checked to be a tree.
- A child pointing to an ancestor makes the `while (num >= 0)` loop in `CM_PointLeafnum_r` endless: the tab
  freezes during the client's prediction, which calls pointcontents every frame. The same shape sends
  `CM_RecursiveHullCheck` into a stack overflow.
- A node with two parents makes traces exponential.

**Fix.** Reject maps where any node has more than one parent, or where a cycle is reachable from a model
headnode (iterative DFS). The demo maps and all golden tests are unchanged.

**Test.** `q2-pmove/test/cmodel.robust.test.ts`: `rejects a node that is its own child`, `rejects a longer cycle
back to the head node`, `rejects shared subtrees`, and a 60-iteration fuzz of random in-range node children
running pointcontents and box traces.
- **Red:** `AssertionError: expected function to throw an error, but it didn't` (×3). A scratch test calling
  `pointContents` on the self-looping demo1 hit `timeout 60` → exit status 124.
- **Green:** 5/5 pass.

### TSE-06: MD2 allocations sized by the attacker

**Where.** `q2-formats/src/md2.ts:95-125` parseMd2. C: `gl_model.c:929 Mod_LoadAliasModel` has no checks either.

**What.**
- `new Int16Array(num_st*2)` / `num_tris*3` were allocated before the bounds check, so num_st = 2^30 meant a
  4 GB allocation.
- `framesize = 0` with `num_frames = 2^31` passed every per-frame check, because the frames overlapped, and built
  billions of frame objects. The 5000-iteration fuzz run was killed by the node OOM handler.

**Fix.**
- The st and tris ranges are checked before allocating.
- `framesize >= 40 + num_xyz*4` is required (the tools always write exactly that), and the whole frame block must
  be inside the file.

**Test.** `fuzz.test.ts` › `md2 with huge num_st / num_tris is rejected before allocating` and `md2 with
overlapping zero-size frames …`.
- **Red:** `AssertionError: expected 4294754075 to be less than 16777216` (bytes allocated) and `expected function
  to throw an error, but it didn't`.
- **Green:** pass. Fuzz at 5000 iterations is clean.

### TSE-07: TGA claims 65535×65535 pixels

**Where.** `q2-formats/src/tga.ts:62-72` decodeTga. C: `gl_image.c:539 LoadTGA`.

**What.** `new Uint8Array(w*h*4)` was allocated from the header before any pixel data was read. An 18-byte file
reserved 12–17 GB. Node commits lazily; a browser throws, or the tab is killed.

**Fix.** Reject files shorter than the minimum the pixel count needs, before allocating: `w*h*bpp`
uncompressed, and `ceil(w*h/128)*(1+bpp)` for RLE. Such files threw "truncated" later anyway.

**Test.** `fuzz.test.ts` › `tga claiming 65535x65535 pixels in an 18-byte file is rejected before allocating`.
- **Red:** `AssertionError: expected 12884377536 to be less than 16777216`.
- **Green:** pass.

### TSE-08: WAV resampling with a hostile rate or length

**Where.** `q2-formats/src/wav.ts:171-200` ResampleSfx. C: `snd_mem.c:33`, which has the same flaw.

**What.**
- rate 0 gives `outcount = Infinity`, so `RangeError: Invalid typed array length: Infinity`. The fuzz run found
  this with seed 601.
- rate 1 with a large declared length gives a ~2e9-sample allocation. Because `fracstep = 0`, it is followed by
  ~2e9 loop iterations reading sample 0, which freezes the tab.

**Fix.** Before allocating, the resample is rejected in any of these cases:
- the rate is ≤ 0;
- the output exceeds what the file's sample data can produce;
- the last source sample the loop would read is outside the data;
- the output is more than 2^25 samples.

**Test.**
- `fuzz.test.ts` › `wav`:
  - **Red:** `sound/berserk/Berdeth2.wav: seed 601 iteration 307: RangeError: Invalid typed array length: Infinity`.
- `fuzz.test.ts` › `wav at 1 Hz with a data chunk claiming 1000 samples …`:
  - **Red:** `expected null to be an instance of FormatError` (it decoded 22 M samples).
- **Green:** both pass.

### TSE-09: CIN decoder runs away on a degenerate Huffman table

**Where.** `q2-formats/src/cin.ts:81` Huff1Tables.decompress, used by `q2-client/src/cl_cin.ts:175`. C:
`cl_cin.c:298`.

**What.** With a single symbol per row, the root is a leaf and symbols come out without consuming input. The
output count comes from the file, capped only at 64 MiB. Each 9-byte frame therefore cost a 64 MiB allocation
plus 64 Mi iterations (~17 s measured). C reads stack garbage here.

**Fix.** Decoding stops once the input is overread by more than the one byte C tolerates silently, and the
allocation is `min(count, 8*(len-2))`. Valid frames are bit-exact, since they never read past len+1.

**Test.** `fuzz.test.ts` › `cin frame claiming a 64 MiB picture from a 5-byte block decodes only what the input
can hold`.
- **Red:** `AssertionError: expected 67112013 to be less than 16777216`.
- **Green:** pass.

### TSE-10: entity sound on entity −1 / 1024 locks the client

**Where.**
- `q2-client/src/cl_parse.ts:499` (svc_sound; C checks only `ent > MAX_EDICTS`, `cl_parse.c:610`).
- `q2-client/src/cl_tent.ts:1004` (TE_LIGHTNING source entity, a signed short).

**What.**
- The sound is queued on an invalid entity.
- q2-sound keeps the entity in `dynEnts`, and `stopAllSounds` does not clear it (OOS-1).
- So every `update()` calls `CL_GetEntitySoundOrigin`, which raises ERR_DROP, on every frame, connected or not.
- The rest of CL_Frame, including SCR_RunConsole, never runs again.

C drops once and recovers.

**Fix (client side).** Out-of-range entity numbers raise ERR_DROP at parse time, before the sound is queued.

**Test.** `hostile.test.ts` › `svc_sound on entity -1 or MAX_EDICTS drops without queueing the sound` and
`TE_LIGHTNING from entity -1 drops without queueing an entity sound`.
- **Red:** both `×`: the sound was queued, with no DropError.
- **Green:** pass.

## Medium

### TSE-11: `CM_LoadMap` race

**Where.** `q2-client/src/cl_main.ts:905-913`.

**What.** `c.cmModel`/`c.cm` were assigned right after the fetch, before the caller's stale check. If the older
map A resolved after the newer map B, B was played with A's collision.

**Fix.** Skip the assignment when `clearGeneration` changed. Covered by the existing suite.

### TSE-12: stale `CL_PrepRefresh` promise

**Where.** `q2-client/src/cl_view.ts:264-284`.

**What.** The previous level's in-flight prep promise was handed out. It returned early (stale), `precache` then
sent `begin` with nothing prepped, and the loading plaque stayed until the 120 s timeout.

**Fix.** Record the generation. A new prep is chained after the old one.

**Test.** `hostile.test.ts` › `a prep of the previous level still in flight does not stand in for the new one`.
- **Red:** `expected Promise{…} not to be Promise{…}`.
- **Green:** pass.

### TSE-13: configstrings lost during the async prep

**Where.**
- `q2-client/src/cl_parse.ts:415-428` (new `CL_ConfigStringChanged`).
- `cl_view.ts:401`.
- `client.ts` `dirty_configstrings`.

**What.** C preps synchronously. Here packets keep being parsed during prep, and the `refresh_prepped` guards
dropped model, sound, image and skin updates for indices the prep had already passed. A player who joined
mid-load rendered as male/grunt.

**Fix.** Such indices are recorded and replayed once `refresh_prepped` is set.

**Test.** `hostile.test.ts` › `configstrings that change while the refresh is being prepared are registered
afterwards`.
- **Red:** `expected undefined to be 'models/late.md2'`.
- **Green:** pass.

### TSE-14: receive queue has no bound

**Where.** `q2-client/src/cl_main.ts:369,387`.

**What.** requestAnimationFrame stops in background tabs, so a flooding WebSocket server grew
`c.main.packets` until the tab ran out of memory.

**Fix.** At most 1024 datagrams are queued; the excess is dropped, like a full UDP receive buffer.

**Test.** `hostile.test.ts` › `the receive queue is bounded` (regression test).

### TSE-15: entity count growth

**Where.** `q2-client/src/cl_ents.ts:166` CL_DeltaEntity.

**What.** A server can repeat entity numbers out of order, since each goes to the baseline path. Every delta
frame then carries all of its base plus up to ~700 more, so per-frame work grows without bound and the parse
ring overwrites its own base.

**Fix.** More than MAX_PARSE_ENTITIES (== MAX_EDICTS) entities in one frame raises ERR_DROP. That is impossible
for valid frames.

**Test.** `hostile.test.ts` › `duplicate out-of-order entity numbers cannot grow a frame past
MAX_PARSE_ENTITIES` (regression test).

### TSE-16: demo recording during playback

**Where.** `q2-client/src/cl_main.ts:150-156` CL_WriteDemoMessage.

**What.** TS demo playback bypasses the netchan, so the message has no 8-byte header. It was stripped anyway,
corrupting the recording, and a message shorter than 8 bytes gave a RangeError.

**Fix.** The header is stripped only for network messages.

**Test.** `hostile.test.ts` › `recording while a demo plays keeps the whole message` (regression test).

### TSE-17: `cl_maxfps 0` (open)

**Where.** `cl_main.ts` CL_Frame: `extratime < 1000/cl_maxfps` is always true for 0.

**What.** CL_Frame stops. Screen, sound and console drawing freeze; commands typed blind still run, because
engine.frame runs Cbuf_Execute. A server can stuff it, since cl_maxfps is not archived.

**Status.** C-faithful and recovered by reload, so it was left as is. A clamp is a possible later improvement.

## Low / info

- **TSE-18** (fixed): baseline number range check, `cl_parse.ts:230`. Red: `expected [Function] to throw error
  matching /CL_ParseBaseline: bad number/ but got 'Cannot read properties of undefined (…'`; green:
  `hostile.test.ts` › `svc_spawnbaseline with an entity number >= MAX_EDICTS …`.
- **TSE-19** (fixed): `v.handleIds.clear()` at each BeginRegistration, `cl_view.ts:306`.
- **TSE-20** (fixed): the demo clock is held while precaching, `cl_main.ts:799`.
- **TSE-21** (fixed): paste clamps to `MAXCMDLINE - 1 - key_linepos`, `keys.ts:368`.
- **TSE-22** (fixed): condump masks before the NUL test, `console.ts:153`.
- **TSE-23** (open): `skinName()` is a linear regcache scan per RF_USE_DISGUISE entity per frame. The cost is
  bounded by entities × handles.
- **TSE-24** (open): `timedemo` ignores the per-frame demo pacing.
- **TSE-25** (open by design): stufftext `connect` (server redirects are a real feature; the new server receives
  the same userinfo), `quit` (it only leaves the session, which a server can force anyway by disconnecting), and
  `timescale`/`fixedtime` (session only).
- **TSE-26** (open): `pic` with a negative stat prints "Can't find pic" every frame. C reads out of bounds here.
- **TSE-27** (info): `GetWavinfo` returns the header's data length, as C does. Every consumer goes through the
  guarded `ResampleSfx` (TSE-08).
- **TSE-28** (info): glcmd vertex indices are validated by the renderer, not by `parseMd2`.

**Checked and fine:**
- `net_chan.ts`: sequence masks on large and negative values, reliable bits, `>>> 0` compares.
- `cl_pred.ts`: a hostile huge ack runs zero prediction steps, as in C.
- `cl_input.ts`: short wrap, checksum byte.
- `cl_ents.ts`: interpolation and no-lerp rules.
- `layout.ts` / `SCR_ExecuteLayoutString`: every index is checked or defaulted; `if`/`endif` and COM_Parse
  terminate.
- `cl_inv.ts`, centerprint, console ring.
- `cvar.ts`: latch, NOSET, info validation.
- `cl_tent.ts` / `cl_fx.ts` / `cl_newfx.ts`: every coordinate is a short/8, so trails are ≤ 14189 units; counts
  are bytes; all lists are fixed at 32; muzzle flash and entity numbers are checked.
- WAV chunk walking terminates. PCX is capped at 640×480. BSP lumps are bounded by the file size. Pak directory
  checks.

## Out of scope (for the owners of those packages)

- **OOS-1 (high, q2-sound):** `snd_dma.ts` `stopAllSounds` (~line 491) does not clear `dynEnts` (added at ~474).
  Any entity sound whose `CL_GetEntitySoundOrigin` throws is retried every frame, forever. TSE-10 closes the
  client paths that caused it. A defensive `entnum` range check in `startSound`, plus clearing `dynEnts`, is
  recommended.
- **OOS-2 (medium-low, q2-render-gl):** `gl_draw.ts:52-58` / `gl_rmain.ts:1005-1019` cache failed `picn` lookups as
  `null` with no eviction. A server that streams layouts with fresh names grows the map in proportion to its
  bandwidth. Recommend capping the negative cache or making it an LRU.
- **OOS-3 (info, apps/web):** `session.ts` persists the config on every `onConfig`, which is what made TSE-01/02
  persistent. `onQuit` maps to `onExit`, and `onDemoRecorded` is not wired.

## Final gates

`cd web && pnpm -r typecheck && pnpm lint && pnpm -r --filter q2-shared --filter q2-protocol --filter q2-formats --filter q2-pmove --filter q2-client test`

| Package | Test files | Tests |
|---|---|---|
| q2-shared | 3 | 37 |
| q2-protocol | 5 | 37 |
| q2-formats | 7 | 51 |
| q2-pmove | 5 | 27 |
| q2-client | 16 | 146 |

- All pass. This includes the golden bit-exact tests and the q2ded integration test (oracle binary present).
- `Q2_FUZZ_ITER=5000` for `fuzz.test.ts` is also clean.
- The q2-sound (20) and q2-render-gl (34) suites were also run against the changed q2-formats and still pass.
