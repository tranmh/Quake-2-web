#!/usr/bin/env bash
# Regenerate patches/0001-oracle-build.patch from edits made in build/src.
# build/orig is a pristine (line-ending-normalized) copy created by sync.sh.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/build"
diff -ru -a orig src > ../patches/0001-oracle-build.patch || true
echo "hunks: $(grep -c '^@@' ../patches/0001-oracle-build.patch)"
