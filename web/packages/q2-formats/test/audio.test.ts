import { describe, expect, it } from 'vitest';
import { CinReader, FormatError, GetWavinfo, Huff1Tables, ResampleSfx, S_LoadSound } from '../src';
import { W } from './helpers';

function chunk(w: W, id: string, body: Uint8Array): void {
  w.str(id, 4).i32(body.length).bytes(body);
  if (body.length & 1) w.u8(0);
}

function makeWav(o: {
  rate?: number;
  width?: number;
  channels?: number;
  samples: Uint8Array;
  cue?: number;
  mark?: number;
  format?: number;
  extra?: boolean;
}): Uint8Array {
  const body = new W().str('WAVE', 4);
  const width = o.width ?? 1;
  const ch = o.channels ?? 1;
  const rate = o.rate ?? 11025;
  if (o.extra) chunk(body, 'junk', new Uint8Array([1, 2, 3]));
  chunk(
    body,
    'fmt ',
    new W()
      .i16(o.format ?? 1, ch)
      .i32(rate, rate * width * ch)
      .i16(width * ch, width * 8)
      .build(),
  );
  if (o.cue !== undefined) {
    chunk(body, 'cue ', new W().i32(1).i32(1, 0).str('data', 4).i32(0, 0, o.cue).build());
    if (o.mark !== undefined) {
      chunk(
        body,
        'LIST',
        new W().str('adtl', 4).str('ltxt', 4).i32(20).i32(1, o.mark).str('mark', 4).pad(8).build(),
      );
    }
  }
  chunk(body, 'data', o.samples);
  const b = body.build();
  const w = new W().str('RIFF', 4).i32(b.length).bytes(b);
  return w.build();
}

describe('wav', () => {
  it('parses fmt/data, loopstart -1 without cue', () => {
    const info = GetWavinfo('t', makeWav({ samples: new Uint8Array([128, 129, 130]), extra: true }));
    expect(info).toMatchObject({ rate: 11025, width: 1, channels: 1, loopstart: -1, samples: 3 });
    expect(info.dataofs).toBe(12 + 12 + 8 + 16 + 8);
  });
  it('reads cue loopstart and LIST/mark loop length', () => {
    const s = new Uint8Array(100).fill(128);
    let info = GetWavinfo('t', makeWav({ samples: s, cue: 10 }));
    expect([info.loopstart, info.samples]).toEqual([10, 100]);
    info = GetWavinfo('t', makeWav({ samples: s, cue: 10, mark: 50 }));
    expect([info.loopstart, info.samples]).toEqual([10, 60]);
    expect(() => GetWavinfo('t', makeWav({ samples: s, cue: 10, mark: 95 }))).toThrow(/bad loop length/);
  });
  it('reports missing chunks like the C code', () => {
    expect(GetWavinfo('t', new Uint8Array(4)).error).toBe('Missing RIFF/WAVE chunks');
    expect(GetWavinfo('t', makeWav({ samples: new Uint8Array(2), format: 3 })).error).toBe(
      'Microsoft PCM format only',
    );
    const noData = new W().str('RIFF', 4).i32(4).str('WAVE', 4).build();
    expect(GetWavinfo('t', noData).error).toBe('Missing fmt chunk');
  });
  it('S_LoadSound rejects stereo', () => {
    expect(S_LoadSound('t', makeWav({ samples: new Uint8Array(4), channels: 2 }), 22050)).toBeNull();
  });
});

describe('ResampleSfx', () => {
  it('fast path converts unsigned 8-bit to signed', () => {
    const sc = S_LoadSound('t', makeWav({ samples: new Uint8Array([0, 128, 255]) }), 11025)!;
    expect(sc).toMatchObject({ length: 3, speed: 11025, width: 1, stereo: 0, loopstart: -1 });
    expect(Array.from(sc.data)).toEqual([-128, 0, 127]);
  });
  it('upsamples with 24.8 fixed-point stepping', () => {
    const sc = S_LoadSound('t', makeWav({ samples: new Uint8Array([0, 64, 128, 255]), cue: 1 }), 22050)!;
    expect(sc.length).toBe(8);
    expect(sc.loopstart).toBe(2);
    expect(Array.from(sc.data)).toEqual([-128, -128, -64, -64, 0, 0, 127, 127]);
  });
  it('downsamples 16-bit and can load as 8-bit', () => {
    const pcm = new W().i16(1000, -1000, 256, -256, 32767, -32768).build();
    const sc = S_LoadSound('t', makeWav({ samples: pcm, width: 2, rate: 22050 }), 11025)!;
    expect(sc.width).toBe(2);
    expect(Array.from(sc.data)).toEqual([1000, 256, 32767]);
    const sc8 = S_LoadSound('t', makeWav({ samples: pcm, width: 2, rate: 22050 }), 11025, true)!;
    expect(sc8.width).toBe(1);
    expect(Array.from(sc8.data)).toEqual([3, 1, 127]);
  });
  it('uses float stepscale and truncating conversions (44100 -> 22050 with odd length)', () => {
    const sc = ResampleSfx(
      { length: 5, loopstart: 3, speed: 44100, width: 1, stereo: 1 },
      44100,
      1,
      new Uint8Array([0, 10, 20, 30, 40]),
      22050,
      false,
    );
    expect(sc.length).toBe(2);
    expect(sc.loopstart).toBe(1);
    expect(Array.from(sc.data)).toEqual([-128, -108]);
  });
});

// Build a Huffman encoder matching Huff1TableInit's trees (for round-trip tests).
function encoder(t: Huff1Tables): (data: Uint8Array) => Uint8Array {
  const codes: number[][][] = [];
  for (let prev = 0; prev < 256; prev++) {
    const table: number[][] = [];
    const walk = (node: number, path: number[]) => {
      if (node < 256) {
        table[node] = path;
        return;
      }
      const base = prev * 512 + (node - 256) * 2;
      walk(t.hnodes1[base]!, [...path, 0]);
      walk(t.hnodes1[base + 1]!, [...path, 1]);
    };
    walk(t.numhnodes1[prev]!, []);
    codes.push(table);
  }
  return (data) => {
    const bits: number[] = [];
    let prev = 0;
    for (const b of data) {
      const c = codes[prev]![b];
      if (!c) throw new Error(`no code for ${b} after ${prev}`);
      bits.push(...c);
      prev = b;
    }
    const out = new W().i32(data.length);
    for (let i = 0; i < bits.length; i += 8) {
      let v = 0;
      for (let j = 0; j < 8 && i + j < bits.length; j++) v |= bits[i + j]! << j;
      out.u8(v);
    }
    return out.build();
  };
}

function countsTable(): Uint8Array {
  const counts = new Uint8Array(65536);
  for (let p = 0; p < 256; p++)
    for (let j = 0; j < 256; j++) counts[p * 256 + j] = ((p * 7 + j * 13) % 50) + 1;
  return counts;
}

describe('cin huffman', () => {
  it('round-trips through Huff1Decompress', () => {
    const t = new Huff1Tables();
    t.init(countsTable());
    for (let p = 0; p < 256; p++) expect(t.numhnodes1[p]).toBe(510);
    const enc = encoder(t);
    const data = new Uint8Array(1000);
    for (let i = 0; i < data.length; i++) data[i] = (i * 31 + (i >> 3)) & 255;
    const comp = enc(data);
    const { data: out, consumed } = t.decompress(comp);
    expect(Array.from(out)).toEqual(Array.from(data));
    expect(consumed === comp.length || consumed === comp.length + 1).toBe(true);
  });
  it('handles sparse count rows (early break in tree building)', () => {
    const counts = new Uint8Array(65536);
    for (let p = 0; p < 256; p++) {
      counts[p * 256 + 1] = 5;
      counts[p * 256 + 2] = 3;
      counts[p * 256 + 3] = 1;
    }
    const t = new Huff1Tables();
    t.init(counts);
    expect(t.numhnodes1[0]).toBe(257); // nodes 256, 257; root = numhnodes-1
    const counts2 = new Uint8Array(65536);
    counts2[5] = 1; // row 0: single symbol -> no internal node
    const t2 = new Huff1Tables();
    t2.init(counts2);
    expect(t2.numhnodes1[0]).toBe(255);
    const d = new Uint8Array([1, 2, 3, 3, 2, 1, 1, 1]);
    expect(Array.from(t.decompress(encoder(t)(d)).data)).toEqual(Array.from(d));
  });
});

describe('cin reader', () => {
  it('reads header, palette frame, audio and end marker', () => {
    const counts = countsTable();
    const w = new W().i32(4, 2, 22050, 2, 1).bytes(counts);
    const t = new Huff1Tables();
    t.init(counts);
    const enc = encoder(t);
    const pic = new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8]);
    const comp = enc(pic);
    const pal = new Uint8Array(768).map((_, i) => i & 255);
    // frame 0: 22050/14 = 1575 samples
    w.i32(1)
      .bytes(pal)
      .i32(comp.length)
      .bytes(comp)
      .pad(1575 * 2, 7);
    // frame 1: start 1575, end 3150
    w.i32(0)
      .i32(comp.length)
      .bytes(comp)
      .pad(1575 * 2, 9);
    w.i32(2);
    const r = new CinReader(w.build());
    expect(r.header).toEqual({ width: 4, height: 2, s_rate: 22050, s_width: 2, s_channels: 1 });
    const f0 = r.readNextFrame()!;
    expect(f0.command).toBe(1);
    expect(f0.palette!.length).toBe(768);
    expect(Array.from(f0.pic)).toEqual(Array.from(pic));
    expect(f0.count).toBe(1575);
    expect(f0.samples.length).toBe(3150);
    const f1 = r.readNextFrame()!;
    expect(f1.palette).toBeNull();
    expect(f1.samples[0]).toBe(9);
    expect(r.readNextFrame()).toBeNull();
  });
  it('rejects bad frame sizes', () => {
    const w = new W().i32(4, 2, 22050, 2, 1).bytes(countsTable()).i32(0).i32(0);
    expect(() => new CinReader(w.build()).readNextFrame()).toThrow(/Bad compressed frame size/);
    expect(() => new CinReader(new Uint8Array(100))).toThrow(FormatError);
  });
});
