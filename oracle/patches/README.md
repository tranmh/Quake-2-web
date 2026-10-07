# Oracle patches

Applied by `sync.sh` to a copy of `../Quake-2` (never to the submodule). Allowed purposes only:
compilation on modern gcc, LP64 correctness, headless operation. Never gameplay behavior.

`0001-oracle-build.patch` (regenerate with `./mkpatch.sh` after editing `build/src`):
- `qcommon/md4.c`: `UINT4` was `unsigned long` (64-bit on LP64) → `unsigned int`; otherwise MD4 map checksums differ from the 32-bit engine.
- `qcommon/cmodel.c` CMod_LoadBrushSides: brush sides with texinfo -1 (qbsp bevels) pointed at `&map_surfaces[-1]`,
  i.e. whatever global the linker put before the array. With gcc 15 that is `trace_trace`, so traces reported
  garbage surface names and flags. They now point at an all-zero `oracle_badsurface`, the value earlier builds
  read and the Go port's `badsurface` reproduces.
- `server/sv_ents.c` SV_FatPVS: OR loop over `long*` (reads 2x too much on LP64) → `int*`.
- `server/sv_world.c` STRUCT_FROM_LINK: pointer offset cast through `int` → `size_t`.
- `linux/sys_linux.c`: x86_64 game module name `game.so` (was `#error Unknown arch`).
- `game/g_items.c`, `ctf/g_items.c` armor indices, `ctf/p_weapon.c` `is_quad`: `static` definitions conflicting
  with `extern` declarations in `g_local.h` (MSVC accepted it; gcc does not). Only used within their own file.
