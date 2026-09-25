#!/usr/bin/env bash
# Every file tracked in the Quake-2 oracle must have a row in docs/PARITY.md.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
missing=0
while read -r f; do
  grep -qF "| \`$f\` |" "$root/docs/PARITY.md" || { echo "missing from PARITY.md: $f"; missing=1; }
done < <(git -C "$root/Quake-2" ls-files)
[[ $missing -eq 0 ]] && echo "parity inventory complete"
exit $missing
