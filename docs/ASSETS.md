# Asset contract (server ⇄ TypeScript client)

This document is normative for the asset index JSON and the asset HTTP endpoints. The Go side lives in
`server/internal/assets/{ingest,manifest,blob,…}` and `server/internal/api`; the TypeScript renderer/sound code
consumes it through `RefImport.loadFile(virtualPath)`.

## Principles

1. **Raw bytes are primary.** Every file of every pak is stored verbatim in a content-addressed blob store and
   the index maps its Quake virtual path (`pics/colormap.pcx`, `textures/e1u1/floor1_3.wal`, `maps/demo1.bsp`,
   `sound/world/amb1.wav`, …) to the SHA-256 of the **original bytes**. The TS client is a faithful `ref_gl` /
   `snd_mem` / `cl_cin` port and decodes PCX, WAL, TGA, BSP, MD2, SP2, WAV and CIN itself, so
   `loadFile(path)` = `fetch("/assets/" + index.files[lower(path)].sha256)`.
2. **PNG renditions are optional extras** (`png` field) for the React shell (thumbnails, menus, pak browser).
   The in-game renderer must not depend on them.
3. **Parsed metadata** (image sizes, WAL flags, WAV loop info, MD2 skins, map checksum, …) is included so the shell
   and loaders can plan work without downloading files. It is informational; the client's own parsers remain the
   authority at run time.
4. **Content addressing** makes every `/assets/{sha256}` response immutable and cacheable forever.

## Paks and paksets

* A **pak** is an uploaded `.pak` file, identified by the SHA-256 of the whole file (the same upload by two
  accounts is one pak with two owners).
* A **pakset** is an ordered list of paks, **lowest priority first** (`[pak0, pak1, …]`). Name resolution follows
  `qcommon/files.c`:
  * a later pak overrides an earlier one (`FS_AddGameDirectory` pushes pak*N* ahead of pak*N-1*);
  * inside one pak the **first** directory entry with a given name wins (`FS_FOpenFile` scans front to back);
  * names compare **ASCII case-insensitively** (`Q_strcasecmp`): index keys are the path lowercased with
    `A–Z → a–z` only. Clients must lowercase lookups the same way. The original spelling is in `path`.
  * Loose files / game directories other than paks do not exist server-side.
* The server ingests `Q2_DEMO_PAK` (default `assets/demo/baseq2/pak0.pak`) at startup (idempotent) as the
  public pakset **`demo`**. Endpoints that take an optional pakset default to `demo`.

## Endpoints

| Method & path | Auth | Description |
|---|---|---|
| `GET /api/v1/paksets` | optional | `{paksets:[Pakset]}` — public paksets + the caller's own |
| `GET /api/v1/paksets/{id}` | optional | `{pakset}` (404 if neither public nor owned) |
| `GET /api/v1/paksets/{id}/index` | optional | the **asset index** (below). `ETag` = quoted SHA-256 of the body; send `If-None-Match` to get `304`. `Cache-Control: public, no-cache` (public pakset) or `private, no-cache`. `409 not_ready` while a pak of the set is still ingesting. |
| `GET /api/v1/maps?pakset=demo` | optional | `{pakset, maps:[{name,path,sha256,checksum,message,sky,pakId}]}` |
| `GET /api/v1/maps/{name}/manifest?pakset=demo` | optional | `{pakset, map:MapInfo, palette, files:{lowerPath: Entry}, missing:[path]}` — the BSP, every texinfo texture (+ WAL animation chains), the 6 sky images, entity models and sounds, resolved through the pakset. Use it to prefetch a level. |
| `GET|HEAD /assets/{sha256}` | see below | the blob. `ETag: "<sha256>"`, `Cache-Control: public, max-age=31536000, immutable` for public (demo-derived) blobs, `private, max-age=31536000, immutable` + `Vary: Cookie` otherwise; `Accept-Ranges: bytes` / `206` with `Range`; `304` for `If-None-Match`. `Content-Type`: `audio/wav` for WAV, `image/png` for PNG renditions, `application/json` for manifests, `application/x-quake2-palette` for the palette, `application/octet-stream` for everything else. |

Asset entitlement (ADR-0005): a blob is served if it belongs to a **public** pak (demo), or to any pak the caller
**owns** (uploading a pak with identical content grants ownership — "matching content hash"). Otherwise `401`
(anonymous) / `403` (logged in). Unknown hashes → `404`. Because private blobs are `Cache-Control: private`,
shared caches (Caddy, CDNs) only store public ones.

Browser notes: requests from the Next.js dev origin need `credentials: "include"` (CORS allows the configured
origins with credentials). Cache blobs in the Cache API keyed by SHA-256; never re-validate them.

## Asset index JSON (schema 1)

```jsonc
{
  "schema": 1,
  "pakset": "demo",
  "paks": [                       // lowest priority first; Entry.pak indexes this array
    {"id": 1, "name": "pak0.pak", "sha256": "cae25718…", "size": 49951322,
     "checksum": 2999375850,       // Com_BlockChecksum of the pak directory (FS_LoadPackFile)
     "numFiles": 1106}
  ],
  "palette": {"sha256": "…", "source": "pics/colormap.pcx"},   // 768 raw RGB bytes; absent if no valid colormap wins
  "files": {                      // key: ASCII-lowercased virtual path
    "pics/conchars.pcx": {
      "path": "pics/conchars.pcx",           // spelling in the pak directory
      "sha256": "3ef8e60a…", "size": 29768,  // ORIGINAL bytes — what loadFile() returns
      "kind": "pcx",                         // bsp|md2|sp2|wal|pcx|tga|wav|cin|other (lowercased extension)
      "pak": 0,
      "image": {"width": 128, "height": 128},
      "png": {"sha256": "eaff1874…", "size": 7833, "width": 128, "height": 128,
              "hasAlpha": true, "type": "pic"}     // optional rendition, see "PNG renditions"
    },
    "textures/e1u1/metal2_1.wal": {
      "path": "…", "sha256": "…", "size": 10980, "kind": "wal", "pak": 0,
      "image": {"width": 64, "height": 128},
      "wal": {"name": "e1u1/metal2_1", "width": 64, "height": 128,
              "flags": 0, "contents": 0, "value": 0,       // miptex_t fields (SURF_*, CONTENTS_*, light value)
              "animName": "",                              // miptex_t.animname
              "animNext": "textures/<animName>.wal"},      // only when animName is non-empty
      "png": {…, "type": "wall"}
    },
    "sound/world/amb1.wav": {
      "…": "…", "kind": "wav",
      "wav": {"rate": 22050, "width": 2, "channels": 1,
              "loopStart": 0,       // cue chunk sample offset, -1 without cue chunk
              "samples": 24840,     // loopStart + LIST/"mark" length, else data bytes / width
              "dataOfs": 44}        // byte offset of the PCM data
    },
    "models/items/ammo/bullets/medium/tris.md2": {
      "…": "…", "kind": "md2",
      "md2": {"skinWidth": 136, "skinHeight": 60, "numXyz": 8, "numSt": 14, "numTris": 12,
              "numGlCmds": 72, "numFrames": 1,
              "skins": ["models/items/ammo/bullets/medium/skin.pcx"], "frames": ["bullets"]}
    },
    "sprites/s_explod.sp2": {
      "…": "…", "kind": "sp2",
      "sp2": {"frames": [{"width": 56, "height": 56, "originX": 28, "originY": 28,
                          "name": "sprites/s_explod_0.pcx"}, …]}
    },
    "video/idlog.cin": {
      "…": "…", "kind": "cin",
      "cin": {"width": 320, "height": 240, "rate": 22050, "sampleWidth": 2, "channels": 1,
              "numFrames": 1024, "palettes": 1}   // illustrative (the demo pak has no .cin)
    },
    "maps/demo1.bsp": {
      "…": "…", "kind": "bsp",
      "map": { /* MapInfo, see below */ }
    },
    "demos/demo1.dm2": {"…": "…", "kind": "other"}
  },
  "maps": [ /* every MapInfo in the index, sorted by name */ ]
}
```

`error` (string) is present on an entry whose content failed validation (e.g. a PCX the C loader would reject,
an MD2 failing `Mod_LoadAliasModel` checks, a WAV without `data` chunk). The raw blob is still listed and served;
the metadata block may be missing.

### MapInfo

```jsonc
{
  "name": "demo1", "path": "maps/demo1.bsp", "sha256": "ee13d2e5…",
  "checksum": 3218560851,          // CM_LoadMap: Com_BlockChecksum (MD4 fold) of the whole file = CS_MAPCHECKSUM
  "message": "Outer Base",         // worldspawn "message"
  "sky": "unit1_",                 // worldspawn "sky", default "unit1_" (SP_worldspawn)
  "skyRotate": "…", "skyAxis": "…",// raw worldspawn strings when present
  "cdTrack": "9",                  // worldspawn "sounds"
  "skyImages": ["env/unit1_rt.tga", "env/unit1_bk.tga", "env/unit1_lf.tga",
                "env/unit1_ft.tga", "env/unit1_up.tga", "env/unit1_dn.tga"],   // R_SetSky order (suf[])
  "numInlineModels": 35, "numTexInfo": 631,
  "textures": ["textures/e1u1/metal2_1.wal", …],   // unique texinfo textures, first-use order ("textures/%s.wal")
  "models": [ … ],                 // entity "model" keys that are not inline ("*n")
  "sounds": ["sound/world/explod1.wav", …],        // target_speaker "noise" (+".wav" when missing, "sound/" prefix)
  "classnames": ["worldspawn", "monster_soldier_light", …],
  "error": "…"                     // only if cmodel loading failed
}
```

`models`, `sounds` and `classnames` are hints for prefetching; the authoritative list of what a level needs
is the configstrings the server sends (`CL_PrepRefresh`).

## PNG renditions (optional)

Produced for PCX, WAL and TGA when a palette is available (the pak's own `pics/colormap.pcx`, otherwise the
palette of the demo pak). They reproduce what `ref_gl` uploads **before** GL-side processing:

* **Palette** — `Draw_GetPalette`: RGB from `pics/colormap.pcx`; every index opaque except **255, which has
  alpha 0**.
* **8-bit images** (PCX pics/skins/sprites, WAL walls, and 8-bit sky PCX with the default
  `gl_ext_palettedtexture 0`) go through the exact `GL_Upload8` expansion: index 255 → alpha 0, and its RGB is
  copied from the first non-255 neighbour tested **up, down, left, right** with `GL_Upload8`'s literal bounds
  checks on the flattened buffer (`i > width`, `i < s-width`, `i > 0`, `i < s-1` — note the strict `>` on the
  first test), else palette index 0. PNGs are **non-premultiplied**; the RGB of transparent texels is
  meaningful (anti-fringe). Load with `premultiplyAlpha: "none"`, `colorSpaceConversion: "none"`.
* **Skins** (`type: "skin"`) additionally get `R_FloodFillSkin` on the indices first (4096-entry FIFO, fill
  colour = first palette entry equal to `0x000000FF`, i.e. index 0 for the Quake II palette).
* **TGA** (32/24-bit, `LoadTGA` types 2 and 10, always flipped to top-down) is stored as is.
* `type` records which `imagetype_t` was assumed. The renderer decides by call site; ingest approximates:
  `.wal` → `wall`; `env/*` → `sky`; PCX named as an MD2 skin in the same pak → `skin`; `pics/*` → `pic`;
  `sprites/*` → `sprite`; other `models/*`, `players/*` → `skin`; anything else → `pic`.
* **Not applied**: gamma/intensity (`GL_LightScaleTexture`), power-of-two resampling, `gl_picmip`,
  `gl_round_down`, mipmaps, and the scrap atlas (for pics < 64×64 GL fills transparent texels from neighbours
  inside the scrap block, which can differ at the edges from the standalone fill done here).
* `hasAlpha` is `GL_Upload32`'s test: any texel alpha ≠ 255.

## Palette export

`index.palette.sha256` → 768 bytes, `r,g,b` for indices 0..255 (straight from the PCX trailer; index 255's alpha
rule is applied by consumers). Needed for `DrawStretchRaw` (cinematics) and 8-bit decoding in the client.

## Uploads and ingest

* `POST /api/v1/paks` (multipart, field `file`, max `Q2_MAX_UPLOAD_BYTES`) streams to disk while hashing,
  validates the pak directory (`FS_LoadPackFile`), stores the pak blob, and queues an ingest job:
  `202 {pak, owned, job}`; a pak already known by hash returns `200 {pak, owned}` and just adds ownership.
  Poll `GET /api/v1/jobs/{id}` (`queued → running → done|failed`, `progress` 0..1) or `GET /api/v1/paks/{id}`.
* Ingest stores every entry verbatim, PNG renditions, the palette and a per-pak manifest JSON (same `Entry`
  shape, `entries` in directory order including duplicates). The pakset index is the files.c merge of the
  per-pak manifests and is cached until a pakset or pak changes.
* `POST /api/v1/paksets {name, paks:[id…]}` creates a private pakset from paks the caller owns or that are public
  (e.g. `[demoPakId, myPak1Id]`); `PUT`/`DELETE /api/v1/paksets/{id}` modify it.

## CLI

`go run ./cmd/q2ingest -blobs /tmp/blobs -o index.json pak0.pak [pak1.pak …]` ingests paks into a blob directory
without a database and writes the merged index — handy for inspecting exactly what the server will serve.
