#!/bin/sh
# Builds oracle/src/snd_main.c against the original client/snd_*.c (oracle CFLAGS) and regenerates the
# committed mixer vectors in test/vectors/. Variant "asm8" patches snd_mix.c's portable
# S_PaintChannelFrom8 row index (vol>>11) to the id386 assembly's (vol>>3).
set -e
ROOT=$(cd "$(dirname "$0")/../../../.." && pwd)
OUT="$ROOT/web/packages/q2-sound/test/vectors"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
CFLAGS="-m64 -O1 -g -std=gnu89 -fcommon -fno-strict-aliasing -fno-fast-math -ffp-contract=off -fexcess-precision=standard -Dstricmp=strcasecmp -DORACLE -w"
Q=$ROOT/Quake-2
sed 's/>> 11\]/>> 3]/' "$Q/client/snd_mix.c" > "$TMP/snd_mix_asm8.c"
grep -q '>> 3\]' "$TMP/snd_mix_asm8.c"
build() {
  gcc $CFLAGS -I"$Q/client" -o "$TMP/$1" "$ROOT/oracle/src/snd_main.c" "$Q/client/snd_dma.c" "$2" \
    "$Q/client/snd_mem.c" "$Q/game/q_shared.c" -lm
}
build snd_c "$Q/client/snd_mix.c"
build snd_asm8 "$TMP/snd_mix_asm8.c"
mkdir -p "$OUT"
"$TMP/snd_c" 0 | gzip -9n > "$OUT/snd16.jsonl.gz"
"$TMP/snd_c" 1 | gzip -9n > "$OUT/snd8_portable.jsonl.gz"
"$TMP/snd_asm8" 1 | gzip -9n > "$OUT/snd8_asm.jsonl.gz"
# S_LoadSound of every demo pak sound (sizes + hashes only, no audio data)
PAK="${Q2_BASEDIR:-$ROOT/assets/demo}/baseq2/pak0.pak"
if [ -f "$PAK" ]; then
  mkdir -p "$TMP/wav"
  ARGS=$(python3 - "$PAK" "$TMP/wav" <<'PY'
import struct, sys, os
f = open(sys.argv[1], 'rb').read()
_, ofs, ln = struct.unpack('<4sii', f[:12])
out = []
for i in range(ln // 64):
    e = f[ofs + i * 64: ofs + i * 64 + 64]
    name = e[:56].split(b'\0')[0].decode()
    fo, fl = struct.unpack('<ii', e[56:64])
    if name.startswith('sound/') and name.endswith('.wav'):
        p = os.path.join(sys.argv[2], 'w%d.wav' % i)
        open(p, 'wb').write(f[fo:fo + fl])
        out.append('%s=%s' % (name[6:], p))
print(' '.join(out))
PY
)
  { "$TMP/snd_c" wav 11025 1 $ARGS; "$TMP/snd_c" wav 22050 1 $ARGS; "$TMP/snd_c" wav 22050 0 $ARGS; "$TMP/snd_c" wav 44100 0 $ARGS; } \
    | gzip -9n > "$OUT/demo_wavs.jsonl.gz"
fi
ls -l "$OUT"
