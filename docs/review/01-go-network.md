# Review 01: Go network, API, auth, persistence, assets, deploy

Scope: `server/internal/{api,auth,db,config,host}`, `server/internal/assets/*`,
`server/cmd/{q2server,q2ingest}`, `server/migrations`, `deploy/`.
Method: every request path read end to end (router, middleware, handlers, repo, blob store, catalog/ingest,
host/instance loop, WebSocket transport). Each candidate was confirmed by a test or an experiment,
plus a 15 s fuzz run of every asset parser and `go test -race` over the whole scope. The Postgres suites
(`internal/db`, `internal/api`, `cmd/q2server` e2e) were run against `postgres:16`
(`DATABASE_URL_TEST`).

Severity: **critical**: remote exploitation, auth bypass, data corruption, or a crash of the whole server from remote input.
**High**: DoS by one user, crash of an instance, a significant security weakness, or wrong behavior in a main flow.
**Medium** and **low** as usual.

Summary: 3 critical, 7 high, 12 medium, 12 low. All critical and high findings are fixed with red→green tests.
10 of the medium findings are fixed as well (trivial and safe); the other 2 are documented.

| id | sev | status |
|----|-----|--------|
| C1 | critical | fixed: cross-player netchan hijack (shared base address) |
| C2 | critical | fixed: crafted `.cin` panics an ingest worker and crashes the process |
| C3 | critical | fixed: crafted pak exhausts memory during ingest (TGA amplification, aliased entries) |
| H1 | high | fixed: a panic in one game instance crashes the whole server |
| H2 | high | fixed: unlimited game instances per account |
| H3 | high | fixed: pak stuck `pending`/`ingesting` forever after a restart or a full queue |
| H4 | high | fixed: unbounded uploads per account fill the disk |
| H5 | high | fixed: one WebSocket connection can flood the shared instance inbox |
| H6 | high | fixed: process-wide map cache keyed by FNV-1a is poisonable from a private pakset |
| H7 | high | fixed: `Games.Stop` not immediate (second DELETE answers 204; flaky test) |
| M1–M12 | medium | 10 fixed, 2 documented |
| L1–L12 | low | documented (L12 fixed) |

---

## Critical

### C1: a player can inject commands into, or take over, another player's client
- **File:** `server/internal/host/host.go:567` (`ServeConnAs`), with the routing logic in
  `internal/sv/sv_main.go` (`SV_ReadPackets` qport match + "fixing up a translated port", `SV_DirectConnect` "reconnect").
- **Description:** every WebSocket connection got `Addr{Base: <remote IP>, Port: n}`. The server treats
  `Base` as the host (`NET_CompareBaseAdr`) and routes sequenced packets by base + 16-bit qport. It also
  re-points a client's netchan at whichever connection sent the matching qport, before the sequence check.
  Behind the reverse proxy (the deployment) every browser has the proxy's IP, so all players share one base.
  Behind a NAT they share it anyway.
- **Failure scenario:** an attacker in a public game sends up to 65536 datagrams, one per qport. Each carries
  a large sequence number and `clc_stringcmd "disconnect"` (or any command, or usercmds). The matching victim is
  disconnected or remote-controlled, and the victim's outgoing stream is redirected to the attacker.
  `connect` with the victim's qport also takes over the victim's slot after `sv_reconnect_limit`.
- **Fix:** each connection gets its own base (`"<ip>#<n>"`), so connections never compare equal. The IP stays
  in front, so the game's `SV_FilterPacket` still parses `a.b.c.d`. Loopback is unchanged. UDP keeps the real
  C semantics.
- **Test:** `internal/host/security_test.go` `TestConnectionsFromSameIPAreIsolated`
  - red: `security_test.go:69: another connection from the same IP disconnected the victim (clients = 0)`
    (the log also showed `SV_ReadPackets: fixing up a translated port`)
  - green: `--- PASS: TestConnectionsFromSameIPAreIsolated`
- **Related:** `Host.RemoteIP` (host.go:71), wired in `cmd/q2server/main.go:190` when `Q2_TRUST_PROXY=true`.
  Game IP filters (`addip`/`filterban`) now see the real client IP instead of the proxy's.

### C2: crafted cinematic crashes the whole server during ingest
- **File:** `server/internal/assets/cin/cin.go:84`
- **Description:** `p += count * SampWidth * Channels` overflows int64 for large header values
  (e.g. rate 14·2²⁷, width 2³⁰, channels 2⁶ → 2⁶³). The read position turns negative, and the next
  `data[p:]` panics inside an ingest worker goroutine. Nothing recovers it there, so the process exits.
- **Failure scenario:** any registered user uploads a pak containing `video/x.cin` with such a header. The server
  (every game, the API) goes down, and on restart the pak is still "pending" (see H3).
- **Fix:** the audio span is bounds-checked with a division before it is added. Defense in depth: `ingestEntry`
  (ingest.go:342) now recovers a parser panic into an entry error.
- **Test:** `internal/assets/cin/cin_test.go` `TestParseSoundSizeOverflow`
  - red: `panic: runtime error: slice bounds out of range [-9223372036854710243:]`
  - green: `ok quake2web/server/internal/assets/cin`

### C3: crafted pak exhausts memory during ingest
- **Files:** `server/internal/assets/tga/tga.go:84`, `server/internal/assets/ingest/ingest.go:209`
- **Description:**
  1. `tga.Decode` bounded RLE images only by `pixels <= 128*len(data)` and then allocated `4*pixels`.
     A 2 MiB entry claiming 16384×16384 allocates 1 GiB, and a 34 MiB entry claiming 65535² allocates 16 GiB.
  2. Pak directory entries may alias the same bytes. A 1 GiB upload with 4096 entries each spanning ~1 GiB
     makes ingest read and hash ~4 TiB, with GOMAXPROCS entries held in memory at once.
- **Failure scenario:** a registered user uploads such a pak. The OOM killer ends the server, or at best the
  single ingest queue is blocked for hours.
- **Fix:** `tga.MaxDimension = 4096`, a memory-safety deviation (ref_gl resamples uploads to ≤256² anyway;
  nothing else in Go uses this decoder). Ingest also rejects paks whose entries cover more bytes than the file
  has, which bounds the total work and the concurrent memory by the upload size. Remaining amplification is
  bounded (PCX ≤ 640×480, WAL ≤ 4·len, MD2/SP2/WAV/CIN allocate proportionally to input).
- **Tests:**
  - `internal/assets/tga/tga_test.go` `TestRLEHugeDimensionsRejectedWithoutAllocation`
    - red: `tga_test.go:118: Decode allocated 1024 MiB for a 2148 KiB input`; green: `ok .../assets/tga`
  - `internal/assets/ingest/security_test.go` `TestIngestRejectsOverlappingEntries`
    - red: `security_test.go:47: ingest accepted a pak whose entries cover 64x its size`; green: `ok .../assets/ingest`

## High

### H1: a panic in one game instance crashes the whole server
- **File:** `server/internal/host/host.go:452` (`Instance.step`)
- **Description:** `sv.recoverError` only handles `Com_Error` panics and re-panics everything else
  (index out of range, nil map...). The instance goroutine had no recover, so any runtime panic reachable
  from client input (≈170k lines of ported game/server code) killed the process.
- **Fix:** each loop step recovers. The instance is shut down (a second recover guards the shutdown), `Err()`
  is set, and the stack is logged. Other instances and the API keep running.
- **Test:** `internal/host/security_test.go` `TestInstancePanicStopsOnlyThatInstance` (a client command
  hook writes to a nil map)
  - red: test binary aborted with `panic: assignment to entry in nil map [recovered]` from `sv.(*Server).recoverError`
  - green: `--- PASS: TestInstancePanicStopsOnlyThatInstance`

### H2: unlimited game instances per account
- **File:** `server/internal/host/games.go:439` (`reserve`), `internal/api/handlers_user.go` (`hostError`)
- **Description:** `POST /api/v1/games` started a new instance (a goroutine, a loaded map, a 10 Hz frame loop)
  every time, with no bound. One account could exhaust CPU and memory.
- **Fix:** `GamesConfig.MaxGamesPerOwner` (default 3) and `MaxGames` (default 64, all owned games).
  Slots are reserved under the lock, so concurrent creates cannot overshoot. The API answers
  `429 too_many_games` (`api.ErrGameLimit`). Server-created games are exempt.
- **Test:** `TestGamesPerOwnerLimit`
  - red: `security_test.go:158: one account started 50 games`
  - green: `--- PASS` (limit after 3, other owners unaffected, a stop frees a slot)

### H3: pak stuck `pending`/`ingesting` forever
- **Files:** `server/internal/api/handlers_assets.go:128`, `server/internal/api/catalog.go:167,176`
- **Description:** a re-upload only re-ingested `failed` paks. A restart during ingest, or `Enqueue`
  failing with "ingest queue full" (which left the row `pending`), made the content unusable forever for
  every user, because paks are unique by hash.
- **Fix:** the catalog tracks queued and running jobs per pak (`Busy`). A re-upload re-ingests a
  pending/ingesting pak that is not busy. A full queue marks the pak `failed`.
- **Test:** `internal/api/security_test.go` `TestReuploadRecoversStuckPak`
  - red: `security_test.go:47: POST /api/v1/paks: status 200, want 202: {... "status":"pending" ...}`
  - green: `ok quake2web/server/internal/api`

### H4: unbounded uploads per account (disk exhaustion)
- **File:** `server/internal/api/handlers_assets.go:159,180`, `internal/config/config.go`
- **Description:** any account could upload any number of 1 GiB paks, and derived blobs add more on top.
- **Fix:** a quota on the non-public paks an account owns: `Q2_MAX_USER_PAKS` (default 50) and
  `Q2_USER_QUOTA_BYTES` (default 4 GiB). Administrators are exempt. Re-uploading an owned or public pak is
  free. At most 2 concurrent uploads per account (each spools up to 1 GiB before it can be checked).
  Errors: `403 quota_exceeded`, `429 too_many_uploads`.
- **Test:** `TestUploadQuota`
  - red: `security_test.go:66: POST /api/v1/paks: status 202, want 403`
  - green: `ok`
- The new variables are not yet listed in `/.env.example` (outside this scope). Coordinator: please add them.

### H5: one WebSocket connection can flood the shared instance inbox
- **File:** `server/internal/host/host.go:582`
- **Description:** every datagram of every connection went into one 1024-slot inbox, which drops when full.
  A single client sending at wire speed starved every other player of the game and pinned the instance goroutine.
- **Fix:** a per-connection token bucket (500 datagrams/s, burst 500; a Q2 client sends one per client
  frame). Excess datagrams are dropped, like an overflowing socket buffer.
- **Test:** `TestConnectionFloodIsRateLimited`
  - red: `security_test.go:255: one connection queued 20000 datagrams in one burst (limit 500)`
  - green: `--- PASS`

### H6: process-wide map cache poisonable from a private pakset
- **File:** `server/internal/host/games.go:359` (`mapCacheFor`), with the root cause in `internal/sv/sv_init.go` `MapCache.Load`
- **Description:** every instance shared one `sv.MapCache` keyed by `name#len#fnv64a(raw)`. FNV-1a is not
  collision resistant. A user's pakset with a crafted `maps/demo1.bsp` of equal length and hash, loaded first,
  would become the collision model of every later public demo1 game. Every distinct map ever loaded also
  stayed cached forever (unbounded memory).
- **Fix (host side):** only games of the system `demo` pakset use the shared cache. Other games load their map
  privately. **Out of scope (sv owner):** key the cache by SHA-256 and bound it.
- **Test:** `TestPrivatePaksetsDoNotShareMapCache`
  - red: `security_test.go:271: games of a user pakset use the process-wide map cache`
  - green: `ok`

### H7: `Games.Stop` not immediate
- **File:** `server/internal/host/games.go:508` (`meta`), `List`
- **Description:** `Stop` waited for the instance, but the registry entry was removed later by a goroutine.
  `DELETE /games/{id}` twice answered 204 twice, and GET/list still showed a stopped game. The existing
  `TestGamesIdleReapAndSaves` failed intermittently under `-race` because of this.
- **Fix:** `meta`/`List`/`reserve` ignore instances whose goroutine has ended.
- **Test:** `TestGamesStopIsImmediate`
  - red: `security_test.go:199: second Stop: <nil>`
  - green: `--- PASS`

## Medium

| id | file | finding | status |
|----|------|---------|--------|
| M1 | `internal/config/config.go:141`, `api/middleware.go` | `Q2_CORS_ORIGINS=*` reflected every Origin with credentials and disabled the CSRF origin check | **fixed**: wildcards rejected at config load; `TestWildcardCORSOriginRejected` (red: `wildcard CORS origin accepted`) |
| M2 | `internal/auth/auth.go:48,52` | argon2 (19 MiB each) had unbounded concurrency; the per-IP login limiter does not stop many source IPs (IPv6 prefix) → OOM | **fixed**: hashing slots = GOMAXPROCS (≥2), waits honor the request context; `TestHashingSlotsBounded` |
| M3 | `internal/api/handlers_user.go:200`, `handlers_assets.go` | bans were only checked at login and join: an existing session of a banned user could still upload paks and start games | **fixed**: `rejectBanned` on upload and game creation; `TestBannedSessionCannotUploadOrCreateGames` |
| M4 | `internal/host/games.go:488` | unlimited save slots per account via the in-game `save <slot>` (each a DB row of up to MiBs) | **fixed**: `MaxSaveSlots = 32` (overwriting an existing slot is always allowed); `TestSaveSlotLimit` |
| M5 | `internal/api/catalog.go:366` | manifest and merged-index caches grew forever (one entry per pak or pakset, up to MiBs each) | **fixed**: capped at 256 entries (reset when full) |
| M6 | `internal/api/handlers_user.go` `readyz` | public `/readyz` returned raw error strings (DB host/user, file paths) | **fixed**: body says `"error"`, details are logged |
| M7 | `internal/host/host.go:71`, `cmd/q2server/main.go:190` | behind Caddy every WebSocket base was the proxy IP, so game IP bans (`addip`) hit everyone | **fixed**: `Host.RemoteIP` uses the last X-Forwarded-For hop when `Q2_TRUST_PROXY=true` |
| M8 | `deploy/Caddyfile` | no anti-framing headers (clickjacking of account actions; web reviewer) | **fixed**: `X-Frame-Options DENY`, `?Content-Security-Policy "frame-ancestors 'none'"` (keeps the asset sandbox CSP); validated with `caddy validate`. COOP/COEP not added: nothing needs SharedArrayBuffer and it would constrain asset/dev loading |
| M9 | `deploy/Caddyfile`, `api/middleware.go` | join tickets travel in the WS query string (web reviewer) | **verified / hardened**: tickets are 256-bit, one-time (consumed even on game mismatch), 60 s TTL, bound to one game. slog request logging records `URL.Path` only, and the WS route bypasses the API logger. Caddy has no access log; a commented `log` block that deletes `ticket` from logged URIs was added |
| M10 | `internal/assets/ingest/ingest.go:342` | a parser panic in an ingest worker crashes the process (general class of C2) | **fixed**: recover per entry, recorded as an entry error |
| M11 | `internal/db/slotfiles.go` | two concurrent games of the same owner saving to the same slot (e.g. both autosaving `save0` on a level change) interleave per-file read-modify-writes, mixing files of two saves in one bundle. The lock is per `SlotFiles`, not per user+slot | not fixed: needs a transactional "write slot" API from sv (write to `current`, then commit the whole bundle once). Rare after H2 (≤3 games) |
| M12 | `cmd/q2server/main.go` | `http.Server` has `ReadHeaderTimeout` but no body read/idle-write deadlines (slow-body uploads hold connections) | not fixed: a global `ReadTimeout` would break 1 GiB uploads and WebSockets. Suggested: per-request deadlines via `http.ResponseController` in `uploadPak` |

## Low

| id | finding | status |
|----|---------|--------|
| L1 | `POST /games/{id}/join` does not check that the joiner is entitled to the game's pakset (ADR-0005 "joining ... requires matching content hashes"). The joiner still cannot download private assets (`/assets` enforces entitlement), but learns map and model names | documented |
| L2 | upload answers 200 vs 202 depending on whether identical content exists (content-existence oracle); the first uploader's filename is shown to later owners | documented |
| L3 | `register` reveals whether an email exists (409) | by design |
| L4 | `/readyz` writes and deletes a probe blob on every unauthenticated call | documented |
| L5 | `/metrics` is served on the app port; Caddy hides it, but the dev compose publishes 8080 | documented |
| L6 | spool files (`upload-*.pak`, `put-*`) orphaned by a crash are never cleaned | documented |
| L7 | no per-account limit on paksets; `ListPaksets` does N+1 queries | documented |
| L8 | Postgres pool has no statement timeout | documented |
| L9 | unbounded WebSocket connections per ticket holder (each needs a fresh ticket, but tickets are unlimited) | documented; H5 bounds each connection's cost |
| L10 | GameSpec `name` may contain control characters; `hostname` cvar values may contain `\` (the serverinfo update is then ignored) | documented |
| L11 | no HSTS in Caddyfile (TLS optional there) | documented |
| L12 | `ingest` could hold a stale PNG pointer when a parser panicked | fixed with M10 |

## Verified as correct (no finding)
Sessions: 256-bit tokens, only the SHA-256 is stored, HttpOnly, SameSite=Lax, Secure by default, expiry checked
in SQL, logout deletes. CSRF: unsafe methods need an allowed Origin, JSON requires `application/json`, and the
multipart upload needs an Origin match or no cross-site Sec-Fetch-Site. IDOR: saves, settings and jobs are keyed
by the session user; paksets and paks are checked for visibility and ownership, including pak ids inside pakset
bodies; games check visibility and owner; SP join is owner-only. `/assets/{sha}`: hash validated,
`AssetAccess` requires a ready pak that is public or owned; private blobs get `Cache-Control: private` +
`Vary: Cookie`, nosniff and a sandbox CSP. Path traversal: blob paths come only from validated hex hashes; slot,
map and cvar names are validated by regex; pak entry names never reach the filesystem in production (IndexFS
resolves them through the index). SQL: parameterized throughout. WebSocket Origin: coder/websocket same-host
plus configured patterns. XFF: last hop only, only with `Q2_TRUST_PROXY`. Docker: distroless nonroot; Postgres
bound to 127.0.0.1 in dev.

## Out-of-scope findings (for the owning agents)
- **sv:** `MapCache` should key by SHA-256 and be bounded (root cause of H6). `recoverError` re-panics
  non-`ComError` panics (contained now by H1, but sv could convert them into a drop of the offending client).
  The shared-base reconnect/qport semantics (C1) are faithful to C and are now neutralized by the host.
- **sv/game save flow:** a transactional slot write would fix M11.
- **repo root `.env.example`:** add `Q2_MAX_USER_PAKS` and `Q2_USER_QUOTA_BYTES` (H4).

## Gates
- `cd server && go vet ./... && gofmt -l .`: clean.
- `go test -race ./internal/api/... ./internal/auth/... ./internal/db/... ./internal/host/... ./internal/assets/... ./internal/config/... ./cmd/...`: all ok.
- The same api/db/e2e suites against `postgres:16` (`DATABASE_URL_TEST`): all ok.
- `go test -tags golden ./internal/game/gametest` (the original golden/determinism tests): ok. See the report.
