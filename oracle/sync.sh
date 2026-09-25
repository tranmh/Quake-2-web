#!/usr/bin/env bash
# Copy the needed subset of the untouched Quake-2 oracle into oracle/build/src and apply patches.
# Patches may only fix compilation, LP64 issues or headless operation — never behavior.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
src="$here/../Quake-2"
dst="$here/build/src"
rm -rf "$dst"; mkdir -p "$dst"
for d in qcommon server client game ctf null linux ref_gl; do cp -r "$src/$d" "$dst/$d"; done
# normalize CR-only / CRLF line endings (md4.c is CR-only) so patches apply
find "$dst" -type f \( -name "*.c" -o -name "*.h" \) -exec perl -pi -e 's/\r\n?/\n/g' {} +
for p in "$here"/patches/*.patch; do [[ -e "$p" ]] || continue; patch -s -p1 -d "$dst" < "$p"; done
echo "synced to $dst"
