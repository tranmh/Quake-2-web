# Review 05: TypeScript renderer, sound, web app and dev harness

Scope: `web/packages/q2-render-gl`, `web/packages/q2-sound`, `web/apps/web` (Next.js shell + game session),
`web/apps/dev`, plus a light pass over the helper C and scripts in `oracle/src`, `oracle/scripts` and `tools/`
(checked only for bugs that could produce wrong fixtures). `Quake-2/` was read as the reference and not changed.

Severity scale: **critical** (XSS, session theft, code-execution-like, persistent harm), **high** (a crafted
asset or server can crash or freeze the client, or a major functional bug or leak in the main flow),
**medium**, **low**.

Every critical and high finding was fixed test-first: a failing test was written and run to confirm it failed
for the right reason (red), then the smallest fix was applied and the test was run again (green). All renderer
and sound oracle tests (`oracle.test.ts`) are still bit-exact after the fixes.

## How the tests are run

| Package | Command | New tests |
|---|---|---|
| q2-render-gl | `pnpm --filter q2-render-gl test` | `test/robustness.test.ts` (17) |
| q2-sound | `pnpm --filter q2-sound test` | `test/robustness.test.ts` (6) |
| web (app) | `pnpm --filter web test` (new: vitest in a node environment with small DOM stubs, `apps/web/vitest.config.ts`) | `test/nav.test.ts`, `test/session-input.test.ts`, `test/transport.test.ts` |

About the "red" runs for endless loops: a synchronous endless loop cannot fail inside vitest, because the test
timeout cannot interrupt it. For those tests the red run was done with `timeout 60 npx vitest run -t …`, and the
evidence is that the run was killed (`exit=124`). Once fixed, each of these tests finishes in milliseconds. The
sound mixer tests avoid the problem with a guard that throws after 200 000 `S_LoadSound` calls inside
`S_PaintChannels`.

## Summary

| ID | Sev | Area | Title | Status |
|---|---|---|---|---|
| R-01 | high | render | Texture animation chain with a cycle that skips the start: endless loop at map load | fixed + test |
| R-02 | high | render | PVS decompression: cluster out of range or truncated row causes an endless loop every frame | fixed + tests |
| R-03 | high | render | MD2 glcmd counts not validated: billions of vertices per frame, OOM or freeze | fixed + tests |
| R-04 | high | render | SURF_WARP subdivision: crafted faces cut into millions of polygons; coordinates past ±9999 recurse forever | fixed + tests |
| R-05 | high | render | Node cycles and shared subtrees: stack overflow, or exponential tree walks that freeze | fixed + tests |
| R-06 | medium | render | Submodel / node face ranges and submodel count not validated (TypeError every frame, `mod_inline` overrun) | fixed + test |
| R-07 | medium | render | `RecursiveLightPoint` mid-point buffers limited to depth 1024 (TypeError) | fixed + test |
| R-08 | medium | render | `nexttexinfo` past the lump gives `undefined` instead of `null` | fixed + test |
| R-09 | low | render | `R_Shutdown` does not release QGL programs, buffers, lightmap array or raw textures | open (note) |
| R-10 | low | render | `screenshot` can be triggered 100 times by server `stufftext` (download prompts) | open (note) |
| S-01 | high | sound | Loop point at or past the end, or a zero-length autosound: `S_PaintChannels` spins forever on the audio thread | fixed + tests |
| S-02 | high | sound | WAV header with a tiny rate: a 2 KB file resampled into 22 M samples (main thread frozen for 2 minutes in the test) | fixed + tests |
| S-03 | low | sound | An exception in a batch kills the AudioWorkletProcessor permanently | fixed |
| S-04 | low | sound | `start()` racing `shutdown()` builds a worklet node on a closed context | fixed |
| S-05 | low | sound | SharedArrayBuffer ring never used: the app is not cross-origin isolated (no COOP/COEP) | open (note) |
| A-01 | high | app | Open redirect after login via `?next=//evil` or `/\evil` | fixed + test |
| A-02 | high | app | Keys and mouse buttons released while the menu is open or the pointer lock is lost are dropped, leaving `+forward` / `+attack` stuck | fixed + tests |
| A-03 | medium | app | WebGL context loss not handled (permanent black screen, GL error spam) | fixed + test |
| A-04 | medium | app | `new WebSocket()` throwing (mixed content, bad URL) leaves the client "connecting" forever | fixed + test |
| A-05 | medium | app | No anti-framing headers (clickjacking of delete / upload / pointer lock) | fixed |
| A-06 | medium | app | Pak upload job polling keeps running after leaving the page | fixed |
| A-07 | medium | app | Dispose during engine init leaves the renderer initialized (GPU objects leak) | fixed |
| A-08 | low | app | Local `config.cfg` mirror survives logout and is uploaded to the next account on a shared browser | fixed |
| A-09 | low | app | Asset hashes from the index used unvalidated in URLs and CSS `url()` | fixed |
| A-10 | low | app | `/play/%` crashes the server render (`decodeURIComponent` URIError) | fixed |
| A-11 | low | app | `window.__q2web` debug hook (with `exec`) is exposed in production builds | open (note) |
| A-12 | low | app | Private pak blobs stay in the Cache API after logout | open (note) |
| A-13 | info | app | Join ticket travels in the WebSocket URL query string | accepted (note) |
| T-01 | low | tooling | `oracle/src/snd_main.c` wav mode does not check `fopen` / `strchr` results | open (note) |

No XSS was found. The shell renders every server-provided string (player and display names, map names, pak
names and errors, save comments, error messages, prints) as React text children, and nothing uses
`dangerouslySetInnerHTML` or `innerHTML`. The dev harness only writes `textContent`. Next.js rewrites go only to
the configured `Q2_SERVER_URL`, and requests are the same-origin fetch/XHR calls with cookies that the Go API
expects.

---

## Renderer (q2-render-gl)

### R-01 high: endless loop counting the frames of a texture animation chain
- **Where:** `web/packages/q2-render-gl/src/gl_model.ts:299` (`Mod_LoadTexinfo`). C: `gl_model.c` `Mod_LoadTexinfo`, same loop.
- **Problem:** `for (step = o.next; step && step !== o; step = step.next) numframes++` only stops when the
  chain comes back to `o`. With `nexttexinfo` values like 0→1→2→1, the walk never returns to texinfo 0.
- **Scenario:** a user uploads a pak with such a map. Every client that loads it freezes during `R_BeginRegistration`.
- **Fix:** stop counting once `numframes` reaches the texinfo count. No real chain can be longer than that, so
  valid maps are unchanged.
- **Test:** `crafted BSP > texture animation chain with a cycle that does not return to the start terminates`.
  Red: `timeout 60 … -t "does not return to the start"` → `exit=124` (hang). Green: passes in about 2 s (the time
  is spent loading the map).

### R-02 high: endless loop in `Mod_DecompressVis` / `Mod_ClusterPVS`
- **Where:** `gl_model.ts:93` (`Mod_DecompressVis`) and `gl_model.ts:116` (`Mod_ClusterPVS`).
- **Problem 1:** a leaf `cluster` at or past `dvis_t.numclusters` reads `visBitofs[…]` as `undefined`. From then on
  `inp` is `NaN`, `out` never grows, and `do … while (out < row)` never ends.
- **Problem 2:** a row offset near the end of the lump, or a zero run whose count byte lies past the end, reads
  `undefined` forever. C reads past the lump and overruns its static row buffer.
- **Scenario:** a crafted map freezes the tab on the first `R_MarkLeaves`, and again every time the view cluster
  changes.
- **Fix:** an out-of-range cluster is treated like "no vis" (everything visible). Decompression stops at the end
  of the lump (the rest of the row is invisible), and the row is clamped to the buffer. Valid rows are unchanged.
- **Tests:** `PVS of a cluster number beyond the vis lump does not spin` and `a compressed PVS row that runs off
  the end of the vis lump terminates`. Red: both hang (`exit=124`). Green: both pass.

### R-03 high: MD2 glcmd strip or fan counts are trusted
- **Where:** `gl_model.ts:629` (`validateGlCmds`, called from `Mod_LoadAliasModel`). The loops that trust the
  counts are `gl_mesh.ts` `GL_DrawAliasFrameLerp` and `GL_DrawAliasShadow`.
- **Problem:** `parseMd2` returns the raw glcmd list, and the draw loops run `do { … } while (--count)` for
  whatever count is stored. With `0x7fffffff` or `-2^31`, the renderer pushes about 2^31 vertices into the
  immediate-mode buffers every frame, which grow until the tab runs out of memory.
- **Fix:** at load time, check that every strip or fan fits inside the list. Otherwise raise `ERR_DROP`. A list
  without the 0 terminator is still accepted, because the draw loop stops at the end.
- **Tests:** `crafted MD2 > a glcmd strip count larger than the command list is rejected with ERR_DROP` and `a
  negative (fan) count of -2^31 is rejected too`. Red: `AssertionError: expected [Function] to throw an error`
  (the model loaded). Green: both pass. `every model of the demo pak still loads` checks every md2 and sp2 in
  pak0.

### R-04 high: warp polygon subdivision on crafted faces
- **Where:** `gl_warp.ts:17` and `gl_warp.ts:49` (budget and depth limit), `gl_warp.ts:112`; the counter is reset
  per model in `Mod_LoadFaces`.
- **Problem 1:** every SURF_WARP face is cut into 64-unit pieces. A 19 200-unit face gives 90 k pieces, and a map
  with thousands of such faces allocates billions of `GLPoly` objects.
- **Problem 2:** `BoundPoly` starts its bounds at ±9999. When all of a piece's coordinates are beyond 9999, the
  split plane misses the polygon and C recurses forever. The port threw a raw `RangeError`.
- **Fix:** allow at most 2^18 warp polygons per model and a recursion depth of 64. Past either limit, raise
  `ERR_DROP`. A qbsp water face has a handful of pieces, and the recursion depth is about 2·log2(size/64).
- **Tests:** `many large SURF_WARP polygons are rejected …`, whose red run showed no throw after 24 s of
  subdividing, and `a warp polygon beyond +-9999 units ends in ERR_DROP, not a stack overflow`, whose red run got
  `'Maximum call stack size exceeded'`. Both pass now. `a normal water face is still subdivided like C` expects
  16 pieces for a 256×256 face.

### R-05 high: node cycles and shared subtrees
- **Where:** `gl_model.ts:412` (`Mod_SetParent`).
- **Problem:** node children are not checked for cycles. A node that is its own child made `Mod_SetParent`
  overflow the stack (a generic fatal error instead of `ERR_DROP`). A node whose two children are the same
  subtree makes every recursive walk (`Mod_SetParent`, `R_RecursiveWorldNode`, `R_MarkLights`) exponential, so a
  chain of about 60 such nodes freezes the tab.
- **Fix:** reject any internal node reached twice, and any depth above 1024, with `ERR_DROP`. Leaves may still be
  shared.
- **Tests:** `a node that is its own child is rejected with ERR_DROP instead of a stack overflow` (red: got
  `'Maximum call stack size exceeded'`) and `a node reachable twice (exponential DAG) is rejected with ERR_DROP`
  (red: `expected [Function] to throw an error`). Both pass. `the unmodified demo1/demo2/demo3 still loads` checks
  that real maps are unaffected.

### R-06 medium: face ranges and submodel count
- **Where:** `gl_model.ts:443` (node `firstface`/`numfaces`) and `gl_model.ts:564` (submodel count above
  `mod_inline.length`, submodel face range).
- **Problem:** `R_RecursiveWorldNode` and `R_DrawInlineBModel` index `surfaces[]` without bounds checks and throw
  a `TypeError` every frame, which stops the session with a fatal error. More than 512 submodels throws at load
  (C overruns `mod_inline[]`).
- **Fix:** `ERR_DROP` at load.
- **Test:** `a submodel with faces beyond the surface array is rejected with ERR_DROP`. Red: no throw. Green: passes.

### R-07 medium: `RecursiveLightPoint` deeper than 1024 levels
- **Where:** `gl_light.ts:136`. `rlpMid` was preallocated for 1024 levels, and `rlpMid[1024]` is `undefined`.
- **Fix:** allocate further levels on demand. (R-05 now limits trees to depth 1024 anyway.)
- **Test:** `deep BSP trees > RecursiveLightPoint deeper than 1024 nodes …`. Red: `TypeError: Cannot set
  properties of undefined (setting '0')`. Green: passes.

### R-08 medium: `nexttexinfo` past the lump
- **Where:** `gl_model.ts:284`. `out[next]!` gave `undefined`, not `null`, so any later code that compares with
  `null` behaved differently.
- **Test:** `nexttexinfo beyond the texinfo lump is treated as no animation`. Red: `expected undefined to be
  null`. Green: passes.

### R-09 low (open): `R_Shutdown` leaves QGL objects alive
`QGL` never deletes its programs, the stream VAO/VBO, the lightmap array texture or the raw
cinematic textures. They are released only when the canvas and its context are garbage collected. The game page
creates a new canvas for each mount, so the leak lasts at most until GC. A `QGL.dispose()` called from
`WebGLRefresh.shutdown` would make the release prompt. Losing the context outright is not an option, because the
same canvas may be reused when `gameId` changes.

### R-10 low (open): server-triggered screenshots
A malicious server can send `stufftext "screenshot"`, and `GL_ScreenShot_f` then offers a PNG download (at
most 100 per session). Consider ignoring `screenshot` unless it comes from local input (q2-client `stufftext`
filtering, out of scope).

### Renderer faithfulness (paths not covered by the oracle)
I checked `R_DrawAlphaSurfaces`, `R_DrawInlineBModel` (translucent path: GL_REPLACE on RGB textures takes alpha
from the vertex colour, as modelled by `flushSurfaces(…, 0.25)`), `R_DrawSpriteModel`, alias skin and frame
clamping, `Draw_StretchRaw` resampling, image upload limits (`GL_Upload8` 512×256, textures capped at 256²,
`MAX_GLTEXTURES`, `MAX_LIGHTMAPS` → `ERR_DROP`) and the sky clipping recursion (bounded by stage 6 and
`MAX_CLIP_VERTS`). All match `ref_gl`, apart from the documented WebGL deviations. GPU memory is bounded by the C
limits: at most 1024 textures of up to 256², at most 128 lightmap pages of 128², and a world VBO proportional to
the BSP size.

## Sound (q2-sound)

### S-01 high: mixer spins on malformed loops
- **Where:** `web/packages/q2-sound/src/snd_core.ts:850` (`S_PaintChannels`). C: `snd_mix.c` has the same loop.
- **Problem:** after a restart at the loop point, `ch.end = ltime + length - loopstart`. When the cue chunk puts
  `loopstart >= length`, or the sound is a zero-length autosound, `ch.end <= ltime`. `count` then stays at 0 and
  the `while (ltime < end)` loop never advances. In the browser this runs inside the AudioWorklet, so all audio
  stops and the audio thread burns a CPU core. With the offline backend it runs on the main thread. C spins too,
  or divides by zero (SIGFPE) for a zero-length autosound.
- **Fix:** a channel whose restart makes no progress is stopped. Valid loops always satisfy
  `loopstart < length`, so their output is unchanged, and the oracle vectors still match bit for bit.
- **Tests:** `loopstart == length does not spin S_PaintChannels`, `loopstart beyond the end …`, and `a zero length
  looping entity sound (autosound) does not spin`. Red: `Error: mixer spins: S_PaintChannels made no progress` for
  all three. Green: all pass. `valid loops keep looping` passes before and after.

### S-02 high: the resample size comes straight from the WAV header
- **Where:** `snd_dma.ts:341` (`MainSound.S_LoadSound`, before `ResampleSfx`).
- **Problem:** `outcount = samples / (rate / dma.speed)`. A 2 KB file that claims 1 Hz asks for 22 050 000
  samples. At rates below `speed/256` the source index never advances, so the allocation and the loop are
  bounded only by the header, not by the file size.
- **Scenario:** in the red test this single 2 KB WAV blocked the main thread for **128 s**.
- **Fix:** reject `rate <= 0` and any resampled sound larger than `MAX_SFX_BYTES` (16 MiB, several minutes of
  44 kHz 16-bit audio), with a console message. The sound counts as failed, as C does when it returns NULL.
- **Tests:** `a 2 KB WAV claiming 1 Hz is not resampled into tens of millions of samples`. Red: `expected {
  length: 22050000, … } to be null` after 127 785 ms. Green: passes in milliseconds. `a normal 11 kHz WAV still
  loads` passes, and so do the oracle `demo_wavs` vectors.

### S-03 low: an exception in `process()` kills the worklet
`host.ts` `pump`: an exception thrown while applying a batch propagated out of `AudioWorkletProcessor.process()`,
which stops the processor for good and leaves the game silent until reload. The batch is now dropped, counted,
and reported through `dprint`.

### S-04 low: start/shutdown race
`web.ts:88`: if `shutdown()` ran while `addModule` was pending, a node was still built on the closed context.
The code now returns early.

### S-05 low (open): the SAB ring is never used
The ring needs `crossOriginIsolated`, and the Next app sends neither COOP nor COEP, so batches always go through
`postMessage`. That works, with slightly more latency. Enabling it needs `Cross-Origin-Opener-Policy:
same-origin` and `Cross-Origin-Embedder-Policy: require-corp` (all assets are same-origin) in `next.config.ts`
and the Caddyfile, followed by an e2e run.

Also reviewed, no change needed: ring overflow (`push` returns false and the batch falls back to
`postMessage`; sequence numbers keep the order), `S_RawSamples` bounds, and `S_Update_` mixahead clamping.

## Web app (apps/web)

### A-01 high: open redirect after login
- **Where:** `web/apps/web/src/components/AuthForm.tsx:19` and the new `src/lib/nav.ts` (`safeNextPath`).
- **Problem:** the old check `next.startsWith('/')` accepts `//evil.example`, `/\evil.example` and
  `/<TAB>/evil.example`. The browser resolves these protocol-relative URLs to another host, and Next's
  `router.push` performs the external navigation.
- **Scenario:** a phishing link `https://q2.example/login?next=//evil.example/fake-login` goes to a look-alike page
  right after a real login.
- **Fix:** accept only paths that start with a single `/` and contain no control characters, and that still
  resolve to the same origin.
- **Test:** `test/nav.test.ts > never leaves the origin for …`. Red: 4 failures, `expected
  'https://evil.example' to be 'https://q2.example'`. Green: 7/7 pass.

### A-02 high: stuck movement or fire after Esc or losing the pointer lock
- **Where:** `src/game/session.ts:311` (key up) and `session.ts:338` (mouse up).
- **Problem:** key events were dropped while the React menu was open, and `mouseup` was ignored without the pointer
  lock. The common case: the player runs with W held, presses Esc (the browser releases the lock and the menu
  opens), releases W, clicks Resume, and keeps running. The same happens to `+attack` when the mouse button is
  released after the lock is lost. `keys.c` sends the `-command` for every key up whatever `key_dest` is.
- **Fix:** always forward key-up and mouse-up events to the engine. Key-down events are still filtered while the
  menu is open or the user is typing in a form.
- **Tests:** `test/session-input.test.ts > forwards a key release that happens while the in-game menu is open` and
  `forwards a mouse button release after the pointer lock was lost`. Red: `expected last "vi.fn()" call to have
  been called with [ 'KeyW', false, 'w' ]` and `[ +0, false ]`. Green: both pass. `does not forward key presses
  while the menu is open` and `removes every listener on dispose` guard the rest.

### A-03 medium: WebGL context loss
`session.ts:381`: nothing listened for `webglcontextlost`. After a GPU reset, or once the browser evicted the
context for having too many, the game kept running into a dead context, and on restore every texture was gone.
The session now calls `preventDefault()`, stops the loop, and shows a fatal error with Retry (reload).
Test: `reports a lost WebGL context instead of rendering into it forever`.

### A-04 medium: WebSocket constructor exceptions
`src/game/transport.ts:25`: `WebSocketTransport.connect` ran inside a `then` callback. A synchronous throw, such as
a `SecurityError` for `ws://` from an https page or a bad URL, became an unhandled rejection, and `onClose` never
fired. The client then stayed in "connecting" forever, resending into a queue. Test: `test/transport.test.ts >
closes with a reason when new WebSocket() throws`. Red: `transport never reported the failure` plus an unhandled
`SecurityError`. Green: passes.

### A-05 medium: clickjacking
`next.config.ts:37` now sends `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`,
`X-Content-Type-Options: nosniff` and `Referrer-Policy: strict-origin-when-cross-origin` on every page. The
Caddyfile sets only the last two. A full script CSP was left out because Next injects inline bootstrap scripts
and would need nonces.

### A-06 medium: pak upload polling outlives the page
`src/app/paks/page.tsx:56`: `for (;;)` job polling and running uploads went on after navigation, calling
`setState` on the unmounted page forever if a job never finished. An `AbortController` created on mount now
aborts the XHR and the polling loop when the page unmounts.

### A-07 medium: dispose during async init
`session.ts:216`: when `dispose()` ran while `createClientEngine` was pending, `refresh.shutdown()` had already
run before `re.init()` created the GL objects and textures, and the late `engine.shutdown()` did not shut the
renderer down again. The late path now shuts down both. The sound path was already safe, because `createWebSound`
creates no `AudioContext` before `S_Init` and `CL_Shutdown` runs `S_Shutdown`.

### A-08 low: local config mirror across accounts
`src/lib/session.ts:76` and `src/lib/config.ts`: `loadConfigText` falls back to the localStorage mirror when the
account has no server copy, and the next save uploads it. On a shared browser, user B therefore inherited user
A's binds, name and settings. Logout now clears the mirror. Logged-out users cannot play, so nothing depends on it.

### A-09 low: unvalidated asset hashes
`src/lib/assets.ts:52`: `assetUrl` now requires `^[0-9a-f]{64}$`. A malformed index entry can no longer build
`/assets/../api/…` fetches, or break out of the `url(…)` in `Q2Text`'s background image. `picUrl` returns
`null` instead, which shows the fallback text.

### A-10 low: `/play/%`
`src/app/play/[id]/page.tsx:10`: `decodeURIComponent` threw a `URIError` during the server render, giving a 500
page. The raw segment is now used.

### A-11 low (open): debug hook in production
`window.__q2web` exposes `exec(text)`, so any script on the origin can run console commands (bind, rcon if
configured). It is only reachable by same-origin script, which already has full control. Still, consider
publishing it only when `NODE_ENV !== 'production'` or behind a query flag. The Playwright e2e uses it against a
production build, so the e2e would need the flag.

### A-12 low (open): Cache API keeps private blobs
Blobs are cached forever in `q2-assets-v1`, including private pak contents served with `Cache-Control:
private`. After logout, another account on the same browser could read a cached blob, but only if it already
knows the hash. Clearing the cache on logout would also throw away the demo assets. One option is a separate
cache for non-public blobs that logout deletes.

### A-13 info: ticket in the WS URL
The one-time join ticket is sent as `?ticket=` because browsers cannot set WebSocket headers. The page URL never
contains it, so it does not leak through the referrer. It can appear in proxy or server access logs, so the Go
server should expire tickets quickly and keep query strings out of its logs. `resolveWsUrl` also trusts an
absolute `wsUrl` from the API, which is a server-trust decision.

Also reviewed, no finding: rAF cancelled and listeners removed on unmount (the session keeps a cleanup list), the
AudioContext closed through `CL_Shutdown` → `S_Shutdown`, the WebSocket closed through `disconnect` →
`NET_CloseTransport`, the ResizeObserver disconnected, stale-closure guards (`live` flags) in `useAssetIndex`
and `CreateGameDialog`, Cache API failures falling back to the network, and `localStorage` holding only
non-secret hints and config (the session cookie is HttpOnly). CSRF protection relies on the Go API (JSON
content type and SameSite cookies). The app never sends form posts.

## Dev harness (apps/dev)
Only `textContent` is written from URL parameters, and the pak is served only at `/pak0.pak` from a fixed path.
No findings.

## Tooling (oracle/src, oracle/scripts, tools/)
Light pass only. `tools/gen-fixtures.sh` runs under `set -euo pipefail`, and `xargs` returns 123 on any child
failure, so a broken generator aborts the run. `fetch-demo-pak.sh` checks the sha256 of the download.
- **T-01 low:** `oracle/src/snd_main.c` `wavmode` does not check the results of `strchr(argv[i], '=')` or
  `fopen` (a missing file gives a NULL dereference). A malformed invocation crashes; it cannot silently write a
  wrong fixture. `Cvar_Get` has a fixed table of 64 without a bounds check, but the sound code uses about 10.
  I found nothing that could make generated fixtures wrong.

## Gates
`cd web && pnpm -r typecheck && pnpm lint && pnpm --filter q2-render-gl --filter q2-sound test && pnpm --filter web build`.
Final run:
- `pnpm -r typecheck`: every package in this scope passes (q2-render-gl, q2-sound, apps/web, apps/dev) and so
  do the other packages, except `packages/q2-formats/test/fuzz.test.ts(58,9) TS2532`. That file is being edited
  by the reviewer who owns q2-formats and is not touched by this review.
- `pnpm lint`: passes.
- `pnpm --filter q2-render-gl --filter q2-sound test`: 34 and 20 tests pass, including the unchanged
  bit-exact oracle tests.
- `pnpm --filter web test`: 13 tests pass.
- `pnpm --filter web build`: passes.
