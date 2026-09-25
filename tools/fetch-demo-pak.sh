#!/usr/bin/env bash
# Fetch the freely redistributable Quake 2 3.14 demo and extract baseq2/pak0.pak.
# The installer is a self-extracting ZIP. Retail data is never fetched.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
assets="${ASSETS_DIR:-$root/assets}"
exe="$assets/dl/q2-314-demo-x86.exe"
out="$assets/demo/baseq2"
mirrors=("${Q2DEMO_URL:-https://deponie.yamagi.org/quake2/idstuff/q2-314-demo-x86.exe}")
mkdir -p "$assets/dl" "$out"
if [[ -s "$out/pak0.pak" ]]; then echo "pak0.pak present"; exit 0; fi
if [[ ! -s "$exe" ]]; then
  for u in "${mirrors[@]}"; do curl -fL --retry 3 -o "$exe.part" "$u" && mv "$exe.part" "$exe" && break; done
fi
expected="$(cut -d' ' -f1 "$root/tools/demo.sha256")"
actual="$(sha256sum "$exe" | cut -d' ' -f1)"
[[ "$expected" == "$actual" ]] || { echo "sha256 mismatch: $actual" >&2; exit 1; }
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
unzip -q -o "$exe" 'Install/Data/baseq2/pak0.pak' -d "$tmp" || [[ $? -eq 1 ]]
cp "$tmp/Install/Data/baseq2/pak0.pak" "$out/pak0.pak"
echo "extracted $(stat -c %s "$out/pak0.pak") bytes -> $out/pak0.pak"
