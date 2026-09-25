# Porting conventions

The C tree in `Quake-2/` is the oracle. Ported code must reproduce its behavior exactly.

## Mapping
- One Go file / one TS file per C file, same base name (`g_ai.c` → `internal/game/g_ai.go`, `cl_ents.c` → `cl_ents.ts`).
- Functions keep their C names (Go: camelCase methods or exported CamelCase; TS: camelCase). Each ported
  function carries a comment `// C: <file>:<line> <Name>` pointing at the original.
- C globals become fields of an instance struct. No package-level mutable state in Go.

## No behavior changes
- Bugs and oddities are reproduced. Any deliberate deviation is behind a flag defaulting to the original
  behavior and is logged in `docs/PARITY.md` (TODO-IMPROVE), except memory-safety fixes for network input.
- cvar names, defaults and flags are copied exactly. Constants come from generated files.

## Numeric rules (ADR-0003)
1. C `float` → Go `float32`, TS number rounded with `Math.fround` on every store. C `double` → `float64`/number.
2. Evaluate in C's promoted type: `float * 0.1` (double literal) is computed in double then narrowed:
   Go `float32(float64(a)*0.1)`, TS `fround(a*0.1)`. `float*float` is single precision: Go `a*b`, TS `fround(a*b)`.
   Calls to `sqrt/sin/cos/atan2/fabs/floor` take and return double.
3. No FMA: wrap products feeding sums in explicit `float32(...)` in Go hot paths; build amd64 `GOAMD64=v1`.
4. `(int)f` truncates toward zero; `short` fields wrap (`int16`, `Int16Array`); `char` is signed.
5. RNG (ADR-0002): glibc `rand()` (TYPE_3) is ported exactly; every `rand()` call site is kept in the same order.

## Pointers
- `edict_t*` → `*Edict` with an `Index`; function pointers → named registry handles keyed by the C function name;
  `mmove_t*` → registered by C variable name; `gitem_t*` → item index / classname.

## Review checklist (per ported file)
- [ ] Every C function present or dispositioned in PARITY.md
- [ ] RNG calls in the same order, no extra draws
- [ ] Float expressions follow the promotion rules
- [ ] cvars identical (name, default, flags)
- [ ] An oracle fixture exercises the file, or a reason is recorded
- [ ] No package-level mutable state; vet/lint clean
