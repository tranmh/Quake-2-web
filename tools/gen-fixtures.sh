#!/usr/bin/env bash
# Regenerate fixtures/generated/** with the C oracle and write fixtures/MANIFEST.sha256.
# Formats: docs/FIXTURES.md. Harness: docs/ORACLE.md.
#
#   tools/gen-fixtures.sh            build oracle if needed, generate everything, write manifest
#   tools/gen-fixtures.sh --check    same, but compare against the committed manifest instead of rewriting it
#
# Env: Q2_BASEDIR (default <repo>/assets/demo), Q2_FIXTURES (default <repo>/fixtures/generated), JOBS (default nproc).
set -euo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
basedir="$(cd "${Q2_BASEDIR:-$repo/assets/demo}" && pwd)"
gen="${Q2_FIXTURES:-$repo/fixtures/generated}"
scen="$repo/fixtures/scenarios"
bin="$repo/oracle/build/bin"
jobs="${JOBS:-$(nproc)}"
check=0
[[ "${1:-}" == "--check" ]] && check=1

[[ -f "$basedir/baseq2/pak0.pak" ]] || { echo "missing $basedir/baseq2/pak0.pak (run tools/fetch-demo-pak.sh)" >&2; exit 1; }

# ---- build ----
[[ -d "$repo/oracle/build/src/qcommon" ]] || "$repo/oracle/sync.sh"
make -s -C "$repo/oracle" -j"$jobs" >/dev/null

core() { "$bin/oracle_core" --basedir "$basedir" "$@"; }
maps=(demo1 demo2 demo3)

rm -rf "$gen/core" "$gen/game" "$gen/scenarios"
mkdir -p "$gen"/core/{bsp,trace,pmove,msg,pak} "$gen"/game "$gen"/scenarios/{pmove,game}

# ---- core ----
echo "core: bsp/trace" >&2
for m in "${maps[@]}"; do
  core -o "$gen/core/bsp/$m.json" bsp "$m"
  core -o "$gen/core/trace/$m.jsonl" trace "$m" --random 20000 --seed "$(( ${m#demo} * 1000 + 17 ))"
done
echo "core: msg/rand/pak" >&2
for k in entity usercmd player scalar crc md4; do core -o "$gen/core/msg/$k.jsonl" msg "$k" --seed 34; done
core -o "$gen/core/rand.json" rand --seed 1 --count 100000
core -o "$gen/core/rand-12345.json" rand --seed 12345 --count 100000
core -o "$gen/core/pak/pak0.json" pak "$basedir/baseq2/pak0.pak"

echo "core: pmove scenarios" >&2
for m in "${maps[@]}"; do
  python3 "$repo/oracle/scripts/gen_pmove_scenarios.py" "$bin/oracle_core" "$basedir" "$m" "$gen/scenarios/pmove/$m.json" 2>/dev/null
done
python3 "$repo/oracle/scripts/gen_pmove_scenarios.py" "$bin/oracle_core" "$basedir" demo1 \
  "$gen/scenarios/pmove/demo1_airaccel.json" 10 2>/dev/null
for s in "$gen"/scenarios/pmove/*.json; do
  n="$(basename "$s" .json)"
  m="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["map"])' "$s")"
  core -o "$gen/core/pmove/$n.jsonl" pmove "$m" "$s"
done

# ---- game ----
echo "game: scenarios" >&2
for s in "$scen"/game/*.json; do
  python3 "$repo/oracle/scripts/prep_game_scenario.py" "$basedir" "$s" "$gen/scenarios/game/$(basename "$s")"
done
run_game() {
  local s="$1" n mod exe
  n="$(basename "$s" .json)"
  mod="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("module","baseq2"))' "$s")"
  exe="$bin/oracle_game"; [[ "$mod" == ctf ]] && exe="$bin/oracle_game_ctf"
  "$exe" --basedir "$basedir" -o "$gen/game/$n.jsonl" "$s"
}
export -f run_game; export bin basedir gen
ls "$gen"/scenarios/game/*.json | xargs -P "$jobs" -I{} bash -c 'run_game "$1"' _ {}

# ---- manifest ----
manifest="$(cd "$gen" && find . -type f | LC_ALL=C sort | sed 's|^\./||' | xargs sha256sum)"
if (( check )); then
  if diff <(echo "$manifest") "$repo/fixtures/MANIFEST.sha256" >/dev/null; then echo "fixtures match MANIFEST.sha256" >&2
  else echo "fixtures DIFFER from MANIFEST.sha256:" >&2; diff <(echo "$manifest") "$repo/fixtures/MANIFEST.sha256" | head -20 >&2; exit 1; fi
else
  echo "$manifest" > "$repo/fixtures/MANIFEST.sha256"
  echo "wrote $(wc -l < "$repo/fixtures/MANIFEST.sha256") entries to fixtures/MANIFEST.sha256 ($(du -sh "$gen" | cut -f1))" >&2
fi
