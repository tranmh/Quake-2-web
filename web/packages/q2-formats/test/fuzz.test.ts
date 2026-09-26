// Fuzz-style robustness tests: random mutations of real demo-pak files must either parse or raise
// FormatError -- never another exception (TypeError/RangeError), never hang, never allocate
// attacker-sized buffers. Assets come from user-uploaded paks, so every parser sees hostile input.
// The PRNG is seeded: failures are reproducible (the failing seed/iteration is in the message).
import { describe, expect, it } from 'vitest';
import { demoPakPath, fileExists, readArrayBuffer } from 'q2-shared/testing';
import {
  CinReader,
  FormatError,
  GetWavinfo,
  Pak,
  S_LoadSound,
  decodeGlCmds,
  decodePcx,
  decodeTga,
  parseBsp,
  parseMd2,
  parseSp2,
  parseWal,
} from '../src';
import { W } from './helpers';

const pakFile = demoPakPath();
const have = fileExists(pakFile);
const pak = have ? new Pak(readArrayBuffer(pakFile)!, 'pak0.pak') : undefined;

/** xorshift32 */
function rng(seed: number): () => number {
  let s = seed >>> 0 || 1;
  return () => {
    s ^= s << 13;
    s >>>= 0;
    s ^= s >>> 17;
    s ^= s << 5;
    s >>>= 0;
    return s;
  };
}

const EXTREMES = [0, 1, -1, 0x7fffffff, -0x80000000, 0xffff, 0x10000, 640, 65535 * 65535];

/** Copies `src` and applies 1..8 random mutations (header-biased). */
function mutate(src: Uint8Array, r: () => number): Uint8Array {
  let b = src.slice();
  const n = 1 + (r() % 8);
  for (let k = 0; k < n; k++) {
    const op = r() % 6;
    // half of the mutations hit the first 256 bytes (headers / directory fields)
    const span = r() & 1 ? Math.min(256, b.length) : b.length;
    if (!span) break;
    const at = r() % span;
    const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
    switch (op) {
      case 0:
        b[at] = r() & 255;
        break;
      case 1:
        b[at] = b[at]! ^ (1 << (r() % 8));
        break;
      case 2:
        if (at + 4 <= b.length) dv.setInt32(at & ~3, EXTREMES[r() % EXTREMES.length]! | 0, true);
        break;
      case 3:
        if (at + 2 <= b.length) dv.setUint16(at & ~1, r() & 0xffff, true);
        break;
      case 4:
        b = b.slice(0, at); // truncate
        break;
      default:
        if (at + 4 <= b.length) dv.setInt32(at & ~3, r() | 0, true);
        break;
    }
  }
  return b;
}

/** Runs `fn` on mutations of `src`; any exception other than FormatError fails the test. */
function fuzz(label: string, src: Uint8Array, iterations: number, seed: number, fn: (d: Uint8Array) => void) {
  const r = rng(seed);
  for (let i = 0; i < iterations; i++) {
    const d = mutate(src, r);
    try {
      fn(d);
    } catch (e) {
      if (e instanceof FormatError) continue;
      throw new Error(`${label}: seed ${seed} iteration ${i}: ${(e as Error).name}: ${(e as Error).message}`);
    }
  }
}

describe.skipIf(!have)('fuzz: mutated demo-pak files never escape FormatError', () => {
  const p = pak!;
  const first = (re: RegExp, n = 4) => p.entries.filter((e) => re.test(e.name)).slice(0, n);
  const ITER = Number(process.env['Q2_FUZZ_ITER'] ?? 400);

  it('bsp', () => {
    const d = p.read('maps/demo1.bsp')!;
    fuzz('bsp', d, Math.max(40, ITER / 10), 1, (x) => void parseBsp(x));
  });

  it('md2', () => {
    for (const [i, e] of first(/\.md2$/i).entries())
      fuzz(e.name, p.readEntry(e), ITER, 100 + i, (x) => decodeGlCmds(parseMd2(x, e.name).glcmds));
  });

  it('sp2', () => {
    for (const [i, e] of first(/\.sp2$/i).entries())
      fuzz(e.name, p.readEntry(e), ITER, 200 + i, (x) => void parseSp2(x, e.name));
  });

  it('wal', () => {
    for (const [i, e] of first(/\.wal$/i).entries())
      fuzz(e.name, p.readEntry(e), ITER, 300 + i, (x) => void parseWal(x, e.name));
  });

  it('pcx', () => {
    for (const [i, e] of first(/\.pcx$/i).entries())
      fuzz(e.name, p.readEntry(e), ITER, 400 + i, (x) => void decodePcx(x, e.name));
  });

  it('tga', () => {
    for (const [i, e] of first(/\.tga$/i, 2).entries())
      fuzz(e.name, p.readEntry(e), ITER / 4, 500 + i, (x) => void decodeTga(x, e.name));
  });

  it('wav', () => {
    for (const [i, e] of first(/\.wav$/i, 6).entries())
      fuzz(e.name, p.readEntry(e), ITER, 600 + i, (x) => {
        GetWavinfo(e.name, x);
        S_LoadSound(e.name, x, 22050);
      });
  });

  it('pak directory', () => {
    // the directory lives at the end of the file: fuzz a small pak built from three real entries
    const names = ['pics/conchars.pcx', 'sprites/s_bfg1.sp2', 'maps/demo1.bsp'].filter((n) => p.has(n));
    const files = names.map((n) => p.read(n)!.subarray(0, 64));
    const w = new W().i32(0x4b434150, 0, 0);
    const pos: number[] = [];
    for (const f of files) {
      pos.push(w.length);
      w.bytes(f);
    }
    const dirofs = w.length;
    names.forEach((n, i) => w.str(n, 56).i32(pos[i]!, files[i]!.length));
    const built = w
      .set32(4, dirofs)
      .set32(8, names.length * 64)
      .build();
    fuzz('pak', built, ITER, 700, (x) => {
      const pk = new Pak(x);
      for (const e of pk.entries) pk.readEntry(e);
    });
  });
});

/**
 * Runs `fn` (expected to throw FormatError) and returns how many ArrayBuffer bytes were allocated meanwhile.
 * Node commits zero-filled buffers lazily, so a 17 GB `new Uint8Array` "succeeds" here -- it has to be
 * measured; a browser tab would throw RangeError or be killed by the OOM handler instead.
 */
function allocatedWhileThrowing(fn: () => void): number {
  const before = process.memoryUsage().arrayBuffers;
  let err: unknown = null;
  try {
    fn();
  } catch (e) {
    err = e;
  }
  const delta = process.memoryUsage().arrayBuffers - before;
  expect(err).toBeInstanceOf(FormatError);
  return delta;
}

const MiB = 1 << 20;

describe('crafted headers: no attacker-sized allocations', () => {
  it('md2 with huge num_st / num_tris is rejected before allocating', () => {
    // ident, version 8, skin 64x64, framesize 40+4, 0 skins, 3 xyz, num_st=2^30, num_tris=2^30, 0 glcmds,
    // 1 frame, offsets all 68 (header end), ofs_end 68
    const d = new W()
      .i32(0x32504449, 8, 64, 64, 44, 0, 3, 0x40000000, 0x40000000, 0, 1, 68, 68, 68, 68, 68, 68)
      .build();
    expect(allocatedWhileThrowing(() => parseMd2(d, 'evil.md2'))).toBeLessThan(16 * MiB);
  });

  it('md2 with overlapping zero-size frames (num_frames = 200000, framesize = 0) is rejected', () => {
    // 1 xyz, 1 st, 1 tri, frame data at 68 (44 bytes), st at 112, tris at 116
    const d = new W()
      .i32(0x32504449, 8, 64, 64, 0, 0, 1, 1, 1, 0, 200000, 128, 112, 116, 68, 128, 128)
      .pad(44)
      .i16(0, 0)
      .i16(0, 0, 0, 0, 0, 0)
      .build();
    expect(() => parseMd2(d, 'evil.md2')).toThrow(FormatError);
  });

  it('tga claiming 65535x65535 pixels in an 18-byte file is rejected before allocating', () => {
    for (const type of [2, 10]) {
      const d = new W().u8(0, 0, type).i16(0, 0).u8(0).i16(0, 0, 65535, 65535).u8(32, 0).build();
      expect(allocatedWhileThrowing(() => decodeTga(d, 'evil.tga'))).toBeLessThan(16 * MiB);
    }
  });

  it('wav at 1 Hz with a data chunk claiming 1000 samples (1 present) is rejected before resampling', () => {
    // RIFF/WAVE, fmt: PCM mono rate 1, 8-bit; data len 1000 with one byte present
    const d = new W()
      .str('RIFF', 4)
      .i32(4 + 24 + 9)
      .str('WAVE', 4)
      .str('fmt ', 4)
      .i32(16)
      .i16(1, 1)
      .i32(1, 1)
      .i16(1, 8)
      .str('data', 4)
      .i32(1000)
      .u8(128)
      .build();
    expect(allocatedWhileThrowing(() => S_LoadSound('evil.wav', d, 22050))).toBeLessThan(16 * MiB);
  });

  it('cin frame claiming a 64 MiB picture from a 5-byte block decodes only what the input can hold', () => {
    // counts table with a single symbol per row: the order-1 decoder then emits bytes without consuming
    // input (C runs on through stack garbage; 64 Mi iterations and a 64 MiB buffer per frame)
    const w = new W().i32(320, 240, 22050, 2, 1);
    const counts = new Uint8Array(65536);
    for (let prev = 0; prev < 256; prev++) counts[prev * 256 + 7] = 1;
    w.bytes(counts);
    w.i32(0, 5).i32(0x3ffffff).u8(0); // command 0, size 5, count = 64 MiB - 1
    w.pad(Math.trunc(22050 / 14) * 2); // frame 0 audio
    const cin = new CinReader(w.build());
    const before = process.memoryUsage().arrayBuffers;
    const t0 = performance.now();
    const f = cin.readNextFrame()!;
    expect(process.memoryUsage().arrayBuffers - before).toBeLessThan(16 * MiB);
    expect(performance.now() - t0).toBeLessThan(1000);
    expect(f.pic.length).toBeLessThanOrEqual(8 * 3);
  });
});
