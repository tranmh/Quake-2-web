// cl_cin.c port: synthetic .cin files encoded with a tiny order-1 Huffman encoder built on the same
// tree construction as Huff1TableInit, played through ClientCinematics.
import { describe, expect, it } from 'vitest';
import { Huff1Tables } from 'q2-formats';
import type { Refresh } from 'q2-ref';
import { ca_active, ca_connected, ClientContext, key_game, key_menu } from '../src/client';
import { NullSound, type Sound } from '../src/sound';
import { MemoryTransport } from '../src/transport';
import { createClientCinematics, SCR_LoadPCX, type ClientCinematics } from '../src/cl_cin';
import { capturePrints, createRecordingRefresh, createStubEffects } from './helpers';

// ---------------------------------------------------------------------------------------------------
// encoder

/** Per-context code tables derived from the decoder's own trees. */
function buildCodes(counts: Uint8Array): { tables: Huff1Tables; codes: number[][][] } {
  const tables = new Huff1Tables();
  tables.init(counts);
  const codes: number[][][] = [];
  for (let prev = 0; prev < 256; prev++) {
    const table: number[][] = new Array<number[]>(256);
    const walk = (node: number, path: number[]): void => {
      if (node < 256) {
        table[node] = path;
        return;
      }
      for (let bit = 0; bit < 2; bit++) {
        const child = tables.hnodes1[prev * 512 + (node - 256) * 2 + bit]!;
        walk(child, [...path, bit]);
      }
    };
    walk(tables.numhnodes1[prev]!, []);
    codes.push(table);
  }
  return { tables, codes };
}

/** Huff1 block: 4-byte LE count + bit stream (LSB first), context = previous output byte (starts at 0). */
function huffEncode(codes: number[][][], data: Uint8Array): Uint8Array {
  const bits: number[] = [];
  let prev = 0;
  for (const b of data) {
    const code = codes[prev]![b];
    if (!code) throw new Error(`no code for ${b} after ${prev}`);
    bits.push(...code);
    prev = b;
  }
  const out = new Uint8Array(4 + Math.ceil(bits.length / 8));
  new DataView(out.buffer).setInt32(0, data.length, true);
  bits.forEach((bit, i) => {
    if (bit) out[4 + (i >> 3)]! |= 1 << (i & 7);
  });
  return out;
}

interface SynthFrame {
  palette?: Uint8Array;
  pic: Uint8Array;
}

function le32(v: number): number[] {
  return [v & 255, (v >> 8) & 255, (v >> 16) & 255, (v >>> 24) & 255];
}

/** Builds a .cin file; returns it with the audio chunks written per frame. */
function buildCin(
  width: number,
  height: number,
  s_rate: number,
  s_width: number,
  s_channels: number,
  frames: SynthFrame[],
  endMarker = true,
): { file: Uint8Array; audio: Uint8Array[] } {
  // counts: every symbol present in every context (>= 2 leaves per tree), biased by real transitions
  const counts = new Uint8Array(65536).fill(1);
  for (const f of frames) {
    let prev = 0;
    for (const b of f.pic) {
      const k = prev * 256 + b;
      if (counts[k]! < 255) counts[k]!++;
      prev = b;
    }
  }
  const { codes } = buildCodes(counts);
  const bytes: number[] = [
    ...le32(width),
    ...le32(height),
    ...le32(s_rate),
    ...le32(s_width),
    ...le32(s_channels),
  ];
  bytes.push(...counts);
  const audio: Uint8Array[] = [];
  frames.forEach((f, i) => {
    bytes.push(...le32(f.palette ? 1 : 0));
    if (f.palette) bytes.push(...f.palette);
    const comp = huffEncode(codes, f.pic);
    bytes.push(...le32(comp.length), ...comp);
    const start = Math.trunc((i * s_rate) / 14);
    const end = Math.trunc(((i + 1) * s_rate) / 14);
    const a = new Uint8Array((end - start) * s_width * s_channels);
    for (let j = 0; j < a.length; j++) a[j] = (i * 31 + j * 7) & 255;
    audio.push(a);
    bytes.push(...a);
  });
  if (endMarker) bytes.push(...le32(2));
  return { file: new Uint8Array(bytes), audio };
}

function randomPic(w: number, h: number, seed: number): Uint8Array {
  const p = new Uint8Array(w * h);
  let s = seed;
  for (let i = 0; i < p.length; i++) {
    s = (s * 1103515245 + 12345) >>> 0;
    // a skewed distribution so the trees are uneven
    p[i] = (s >>> 16) % 7 === 0 ? (s >>> 8) & 255 : (s >>> 20) & 7;
  }
  return p;
}

function palette(seed: number): Uint8Array {
  const p = new Uint8Array(768);
  for (let i = 0; i < 768; i++) p[i] = (i * seed) & 255;
  return p;
}

// ---------------------------------------------------------------------------------------------------
// harness

interface Harness {
  c: ClientContext;
  cin: ClientCinematics;
  raws: { cols: number; rows: number; w: number; h: number; data: Uint8Array }[];
  palettes: (Uint8Array | null)[];
  rawSamples: { samples: number; rate: number; width: number; channels: number; data: Uint8Array }[];
  prints: string[];
  clock: { now: number };
  loads: string[];
}

function harness(files: Record<string, Uint8Array>): Harness {
  const raws: Harness['raws'] = [];
  const palettes: Harness['palettes'] = [];
  const rawSamples: Harness['rawSamples'] = [];
  const loads: string[] = [];
  const base = createRecordingRefresh();
  const refresh: Refresh = {
    ...base,
    drawStretchRaw(x, y, w, h, cols, rows, data) {
      raws.push({ cols, rows, w, h, data: data.slice() });
      base.drawStretchRaw(x, y, w, h, cols, rows, data);
    },
    cinematicSetPalette(p) {
      palettes.push(p ? p.slice() : null);
    },
  };
  const sound: Sound = new NullSound();
  sound.rawSamples = (samples, rate, width, channels, data) => {
    rawSamples.push({ samples, rate, width, channels, data: data.slice() });
  };
  const clock = { now: 5000 };
  const cin = createClientCinematics();
  const c = new ClientContext({
    refresh,
    transport: () => new MemoryTransport(),
    loadFile: async (name) => {
      loads.push(name);
      return files[name] ?? null;
    },
    sound,
    effects: createStubEffects(),
    cinematics: cin,
    milliseconds: () => clock.now,
  });
  cin.attach(c);
  c.cls.state = ca_connected;
  c.cls.key_dest = key_game;
  const prints = capturePrints(c);
  return { c, cin, raws, palettes, rawSamples, prints, clock, loads };
}

function netText(c: ClientContext): string {
  const m = c.cls.netchan.message;
  return String.fromCharCode(...m.data.subarray(0, m.cursize));
}

// ---------------------------------------------------------------------------------------------------

describe('order-1 huffman encoder', () => {
  it('round-trips through Huff1Decompress', () => {
    const counts = new Uint8Array(65536);
    for (let i = 0; i < counts.length; i++) counts[i] = 1 + (((i * 2654435761) >>> 24) % 200);
    const { tables, codes } = buildCodes(counts);
    for (let seed = 1; seed < 6; seed++) {
      const data = randomPic(37, 11, seed);
      const enc = huffEncode(codes, data);
      const { data: dec, consumed } = tables.decompress(enc);
      expect(Array.from(dec)).toEqual(Array.from(data));
      expect(consumed === enc.length || consumed === enc.length + 1).toBe(true);
    }
  });
});

describe('SCR_PlayCinematic / SCR_RunCinematic / SCR_DrawCinematic (.cin)', () => {
  const W = 16;
  const H = 10;
  const pal0 = palette(3);
  const pal2 = palette(5);
  const pics = [randomPic(W, H, 11), randomPic(W, H, 12), randomPic(W, H, 13), randomPic(W, H, 14)];
  const frames: SynthFrame[] = [
    { palette: pal0, pic: pics[0]! },
    { pic: pics[1]! },
    { palette: pal2, pic: pics[2]! },
    { pic: pics[3]! },
  ];

  it('decodes every frame, timing, palette, audio and finishes with nextserver', async () => {
    const { file, audio } = buildCin(W, H, 11025, 2, 1, frames);
    const h = harness({ 'video/test.cin': file });
    const { c, cin } = h;
    c.cvars.get('s_khz', '11', 0); // no sound restart
    c.cl.servercount = 77;

    cin.play('test.cin');
    expect(c.cl.cinematicframe).toBe(0);
    expect(c.cl.cinematictime).toBe(0); // async gap
    await cin.pending;
    expect(h.loads).toEqual(['video/test.cin']);
    expect(c.cls.state).toBe(ca_active);
    expect(c.cl.cinematictime).toBe(5000);
    expect(c.cl.cinematicframe).toBe(1);
    expect(cin.restart_sound).toBe(false);
    expect(Array.from(c.cl.cinematicpalette)).toEqual(Array.from(pal0));
    expect(c.cl.cinematicpalette_active).toBe(false);
    expect(h.rawSamples.length).toBe(1);
    expect(h.rawSamples[0]).toMatchObject({ samples: 787, rate: 11025, width: 2, channels: 1 });
    expect(Array.from(h.rawSamples[0]!.data)).toEqual(Array.from(audio[0]!));

    // draw frame 0
    c.viddef.width = 320;
    c.viddef.height = 240;
    expect(cin.draw()).toBe(true);
    expect(h.palettes).toEqual([pal0]);
    expect(c.cl.cinematicpalette_active).toBe(true);
    expect(h.raws.length).toBe(1);
    expect(h.raws[0]).toMatchObject({ cols: W, rows: H, w: 320, h: 240 });
    expect(Array.from(h.raws[0]!.data)).toEqual(Array.from(pics[0]!));

    // 14 fps: frame 1 is due at realtime - cinematictime >= 143 ms (72 ms gives frame (72*14/1000)|0 = 1)
    c.cls.realtime = 5000 + 72;
    cin.run();
    expect(c.cl.cinematicframe).toBe(1);
    c.cls.realtime = 5000 + 143;
    cin.run();
    expect(c.cl.cinematicframe).toBe(2);
    // (sic) cin.pic takes pic_pending, which is NULL after SCR_PlayCinematic: one blank frame
    expect(cin.pic).toBeNull();
    expect(cin.draw()).toBe(true);
    expect(h.raws.length).toBe(1);
    expect(h.rawSamples[1]).toMatchObject({ samples: 788 });
    expect(Array.from(h.rawSamples[1]!.data)).toEqual(Array.from(audio[1]!));

    c.cls.realtime = 5000 + 215;
    cin.run(); // frame 3: pic = frame 1, pending = frame 2 (new palette)
    expect(Array.from(cin.pic!)).toEqual(Array.from(pics[1]!));
    expect(c.cl.cinematicpalette_active).toBe(false); // "dubious" reset by the palette command
    expect(Array.from(c.cl.cinematicpalette)).toEqual(Array.from(pal2));
    cin.draw();
    expect(h.palettes.length).toBe(2);
    expect(Array.from(h.palettes[1]!)).toEqual(Array.from(pal2));
    expect(Array.from(h.raws[1]!.data)).toEqual(Array.from(pics[1]!));

    // menu up: blank + pause
    c.cls.key_dest = key_menu;
    expect(cin.draw()).toBe(true);
    expect(h.palettes[2]).toBeNull();
    c.cls.realtime = 9000;
    cin.run();
    expect(c.cl.cinematictime).toBe(9000 - Math.trunc((3 * 1000) / 14));
    c.cls.key_dest = key_game;

    // dropped frame: far in the future
    c.cls.realtime = c.cl.cinematictime + 1000;
    cin.run(); // frame 4: pic = frame 2, pending = frame 3
    expect(h.prints.join('')).toContain('Dropped frame: 14 > 4\n');
    expect(Array.from(cin.pic!)).toEqual(Array.from(pics[2]!));
    expect(c.cl.cinematictime).toBe(c.cls.realtime - Math.trunc((3 * 1000) / 14));
    expect(h.rawSamples.length).toBe(4);
    expect(Array.from(h.rawSamples[3]!.data)).toEqual(Array.from(audio[3]!));

    // next due frame hits the end marker: stop + nextserver + loading plaque
    c.cls.disable_screen = 1; // (keep SCR_BeginLoadingPlaque from drawing in this bare context)
    c.cls.realtime += 150;
    cin.run();
    expect(c.cl.cinematictime).toBe(0);
    expect(cin.pic).toBeNull();
    expect(netText(c)).toContain('nextserver 77\n');
    expect(cin.draw()).toBe(false);
  });

  it('switches s_khz and restarts sound when the rate differs', async () => {
    const { file } = buildCin(W, H, 22050, 1, 2, frames.slice(0, 2));
    const h = harness({ 'video/k.cin': file });
    const { c, cin } = h;
    c.cvars.get('s_khz', '11', 0);
    let restarts = 0;
    const s = c.sound as NullSound;
    s.init = () => void restarts++;
    cin.play('k.cin');
    await cin.pending;
    expect(cin.restart_sound).toBe(true);
    expect(restarts).toBe(1);
    expect(c.cvars.variableString('s_khz')).toBe('11');
    expect(h.rawSamples[0]).toMatchObject({ samples: 1575, rate: 22050, width: 1, channels: 2 });
    expect(h.rawSamples[0]!.data.length).toBe(1575 * 2);
    cin.stop();
    expect(restarts).toBe(2);
    expect(cin.restart_sound).toBe(false);
  });

  it('a file without the end marker stops at EOF', async () => {
    const { file } = buildCin(W, H, 11025, 1, 1, frames.slice(0, 1), false);
    const h = harness({ 'video/e.cin': file });
    const { c, cin } = h;
    c.cvars.get('s_khz', '11', 0);
    cin.play('e.cin');
    await cin.pending;
    c.cls.disable_screen = 1;
    c.cls.realtime = 5000 + 143;
    cin.run();
    expect(c.cl.cinematictime).toBe(0);
    expect(netText(c)).toContain('nextserver');
  });

  it('missing .cin: finish immediately', async () => {
    const h = harness({});
    h.c.cl.servercount = 3;
    h.cin.play('idlog.cin');
    await h.cin.pending;
    expect(h.loads).toEqual(['video/idlog.cin']);
    expect(h.c.cl.cinematictime).toBe(0);
    expect(h.c.cls.state).toBe(ca_connected);
    expect(netText(h.c)).toContain('nextserver 3\n');
  });

  it('a newer play() abandons a pending load', async () => {
    const { file } = buildCin(W, H, 11025, 1, 1, frames);
    const h = harness({ 'video/a.cin': file });
    h.cin.play('a.cin');
    h.cin.play('b.cin');
    await h.cin.pending;
    expect(h.c.cls.state).toBe(ca_connected); // a.cin was dropped; b.cin missing -> finish
    expect(netText(h.c)).toContain('nextserver');
  });
});

// ---------------------------------------------------------------------------------------------------

function buildPcx(w: number, h: number, pix: Uint8Array, pal: Uint8Array): Uint8Array {
  const out: number[] = new Array<number>(128).fill(0);
  out[0] = 0x0a;
  out[1] = 5;
  out[2] = 1;
  out[3] = 8;
  out[8] = (w - 1) & 255;
  out[9] = (w - 1) >> 8;
  out[10] = (h - 1) & 255;
  out[11] = (h - 1) >> 8;
  for (let y = 0; y < h; y++) {
    let x = 0;
    while (x < w) {
      const v = pix[y * w + x]!;
      let run = 1;
      while (x + run < w && run < 63 && pix[y * w + x + run] === v) run++;
      if (run > 1 || (v & 0xc0) === 0xc0) out.push(0xc0 | run, v);
      else out.push(v);
      x += run;
    }
  }
  out.push(0x0c, ...pal);
  return new Uint8Array(out);
}

describe('static pcx', () => {
  const w = 20;
  const h = 7;
  const pix = new Uint8Array(w * h);
  for (let i = 0; i < pix.length; i++) pix[i] = i % 5 === 0 ? 200 : (i >> 3) & 255;
  const pal = palette(7);

  it('SCR_LoadPCX decodes like the C loader', () => {
    const c = harness({}).c;
    const img = SCR_LoadPCX(c, 'pics/x.pcx', buildPcx(w, h, pix, pal))!;
    expect(img.width).toBe(w);
    expect(img.height).toBe(h);
    expect(Array.from(img.pic)).toEqual(Array.from(pix));
    expect(Array.from(img.palette)).toEqual(Array.from(pal));
  });

  it('rejects bad headers / sizes', () => {
    const hh = harness({});
    const bad = buildPcx(w, h, pix, pal);
    bad[1] = 4;
    expect(SCR_LoadPCX(hh.c, 'pics/b.pcx', bad)).toBeNull();
    expect(hh.prints.join('')).toContain('Bad pcx file pics/b.pcx\n');
    const big = buildPcx(w, h, pix, pal);
    big[8] = 640 & 255;
    big[9] = 640 >> 8;
    expect(SCR_LoadPCX(hh.c, 'pics/c.pcx', big)).toBeNull();
    const trunc = buildPcx(w, h, pix, pal).subarray(0, 140);
    expect(SCR_LoadPCX(hh.c, 'pics/t.pcx', trunc)).toBeNull();
    expect(hh.prints.join('')).toContain('PCX file pics/t.pcx was malformed');
  });

  it('plays end.pcx as a static image', async () => {
    const hh = harness({ 'pics/end.pcx': buildPcx(w, h, pix, pal) });
    const { c, cin } = hh;
    cin.play('end.pcx');
    await cin.pending;
    expect(hh.loads).toEqual(['pics/end.pcx']);
    expect(c.cl.cinematicframe).toBe(-1);
    expect(c.cl.cinematictime).toBe(1);
    expect(c.cls.state).toBe(ca_active);
    cin.run(); // static image: nothing happens
    expect(c.cl.cinematictime).toBe(1);
    expect(cin.draw()).toBe(true);
    expect(Array.from(hh.palettes[0]!)).toEqual(Array.from(pal));
    expect(Array.from(hh.raws[0]!.data)).toEqual(Array.from(pix));
    expect(hh.raws[0]).toMatchObject({ cols: w, rows: h, w: c.viddef.width, h: c.viddef.height });
    cin.stop();
    expect(c.cl.cinematictime).toBe(0);
    expect(hh.palettes[1]).toBeNull();
    expect(c.cl.cinematicpalette_active).toBe(false);
  });

  it('missing pcx prints not found', async () => {
    const hh = harness({});
    hh.cin.play('nope.pcx');
    await hh.cin.pending;
    expect(hh.prints.join('')).toContain('pics/nope.pcx not found.\n');
    expect(hh.c.cl.cinematictime).toBe(0);
    expect(hh.c.cl.cinematicframe).toBe(-1);
    expect(hh.c.cls.state).toBe(ca_active);
  });
});
