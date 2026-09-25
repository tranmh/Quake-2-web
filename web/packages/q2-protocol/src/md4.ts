// Port of qcommon/md4.c (RSA MD4, UINT4 = 32-bit as patched for LP64 in the oracle) and Com_BlockChecksum.

function rotl(x: number, n: number): number {
  return (x << n) | (x >>> (32 - n));
}

// C: md4.c MD4Transform
function md4Transform(state: Uint32Array, block: Uint8Array, off: number, x: Uint32Array): void {
  for (let i = 0; i < 16; i++) {
    const j = off + i * 4;
    x[i] = block[j]! | (block[j + 1]! << 8) | (block[j + 2]! << 16) | (block[j + 3]! << 24);
  }
  let a = state[0]!,
    b = state[1]!,
    c = state[2]!,
    d = state[3]!;
  const F = (x: number, y: number, z: number) => (x & y) | (~x & z);
  const G = (x: number, y: number, z: number) => (x & y) | (x & z) | (y & z);
  const H = (x: number, y: number, z: number) => x ^ y ^ z;
  const r1 = [3, 7, 11, 19];
  for (let i = 0; i < 16; i++) {
    const t = rotl((a + F(b, c, d) + x[i]!) | 0, r1[i & 3]!);
    a = d;
    d = c;
    c = b;
    b = t;
  }
  const r2 = [3, 5, 9, 13];
  const o2 = [0, 4, 8, 12, 1, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15];
  for (let i = 0; i < 16; i++) {
    const t = rotl((a + G(b, c, d) + x[o2[i]!]! + 0x5a827999) | 0, r2[i & 3]!);
    a = d;
    d = c;
    c = b;
    b = t;
  }
  const r3 = [3, 9, 11, 15];
  const o3 = [0, 8, 4, 12, 2, 10, 6, 14, 1, 9, 5, 13, 3, 11, 7, 15];
  for (let i = 0; i < 16; i++) {
    const t = rotl((a + H(b, c, d) + x[o3[i]!]! + 0x6ed9eba1) | 0, r3[i & 3]!);
    a = d;
    d = c;
    c = b;
    b = t;
  }
  state[0] = state[0]! + a;
  state[1] = state[1]! + b;
  state[2] = state[2]! + c;
  state[3] = state[3]! + d;
}

/** MD4 digest (16 bytes) of `data`. */
export function md4(data: Uint8Array): Uint8Array {
  const state = new Uint32Array([0x67452301, 0xefcdab89, 0x98badcfe, 0x10325476]);
  const x = new Uint32Array(16);
  const len = data.length;
  let off = 0;
  for (; off + 64 <= len; off += 64) md4Transform(state, data, off, x);
  // padding: 0x80, zeros to 56 mod 64, 64-bit little-endian bit count
  const rem = len - off;
  const padLen = rem < 56 ? 64 : 128;
  const tail = new Uint8Array(padLen);
  tail.set(data.subarray(off));
  tail[rem] = 0x80;
  const bitsLo = (len << 3) >>> 0;
  const bitsHi = Math.floor(len / 0x20000000) >>> 0;
  const dv = new DataView(tail.buffer);
  dv.setUint32(padLen - 8, bitsLo, true);
  dv.setUint32(padLen - 4, bitsHi, true);
  for (let o = 0; o < padLen; o += 64) md4Transform(state, tail, o, x);
  const out = new Uint8Array(16);
  const odv = new DataView(out.buffer);
  for (let i = 0; i < 4; i++) odv.setUint32(i * 4, state[i]!, true);
  return out;
}

// C: md4.c:263 Com_BlockChecksum -- XOR of the four digest words, unsigned 32-bit.
export function Com_BlockChecksum(data: Uint8Array): number {
  const d = md4(data);
  const dv = new DataView(d.buffer);
  return (
    (dv.getUint32(0, true) ^ dv.getUint32(4, true) ^ dv.getUint32(8, true) ^ dv.getUint32(12, true)) >>> 0
  );
}
