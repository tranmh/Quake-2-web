#!/usr/bin/env bash
# Fails if the Quake-2 submodule has local modifications or is not at the pinned commit.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pinned=372afde46e7defc9dd2d719a1732b8ace1fa096e
actual="$(git -C "$root/Quake-2" rev-parse HEAD)"
[[ "$actual" == "$pinned" ]] || { echo "Quake-2 at $actual, expected $pinned" >&2; exit 1; }
[[ -z "$(git -C "$root/Quake-2" status --porcelain)" ]] || { echo "Quake-2 has local changes" >&2; exit 1; }
echo "oracle untouched"
