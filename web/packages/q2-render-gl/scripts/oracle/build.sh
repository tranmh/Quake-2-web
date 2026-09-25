#!/bin/sh
# Builds the ref_gl oracle (the real Quake-2/ref_gl C code with a no-op qgl) and regenerates the test
# vectors in ../../test/data/oracle.json.
#   usage: scripts/oracle/build.sh [outdir]     (default outdir: $TMPDIR/q2-render-gl-oracle)
# Uses the oracle compiler flags of oracle/Makefile (x86-64 SSE float, no FMA contraction).
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../../../.." && pwd)
q2="$root/Quake-2"
out=${1:-${TMPDIR:-/tmp}/q2-render-gl-oracle}
mkdir -p "$out"
CFLAGS="-m64 -O1 -g -std=gnu89 -fcommon -fno-strict-aliasing -fno-fast-math -ffp-contract=off -fexcess-precision=standard -Dstricmp=strcasecmp -w"
INC="-I$here/include -I$q2/ref_gl -I$q2/game"
python3 "$here/gen_qgl_stub.py" "$q2/ref_gl/qgl.h" > "$out/qgl_stub.c"
objs=""
for f in gl_draw gl_image gl_light gl_mesh gl_model gl_rmain gl_rmisc gl_rsurf gl_warp; do
  gcc $CFLAGS $INC -c "$q2/ref_gl/$f.c" -o "$out/$f.o"
  objs="$objs $out/$f.o"
done
gcc $CFLAGS $INC -c "$q2/game/q_shared.c" -o "$out/q_shared.o"
gcc $CFLAGS $INC -c "$out/qgl_stub.c" -o "$out/qgl_stub.o"
gcc $CFLAGS $INC -c "$here/oracle.c" -o "$out/oracle.o"
gcc -o "$out/ref_oracle" $objs "$out/q_shared.o" "$out/qgl_stub.o" "$out/oracle.o" -lm
"$out/ref_oracle" "$root/assets/demo/baseq2/pak0.pak" > "$here/../../test/data/oracle.json"
echo "wrote test/data/oracle.json"
