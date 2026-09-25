// Tests against the real demo pak0.pak; skipped when it is not present.
import { describe, expect, it } from 'vitest';
import { demoPakPath, fileExists, readArrayBuffer } from 'q2-shared/testing';
import {
  CinReader,
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

const pakFile = demoPakPath();
const have = fileExists(pakFile);
const pak = have ? new Pak(readArrayBuffer(pakFile)!, 'pak0.pak') : undefined;

describe.skipIf(!have)('demo pak0.pak', () => {
  const p = pak!;
  const byExt = (re: RegExp) => p.entries.filter((e) => re.test(e.name));

  it('lists 1106 files', () => {
    expect(p.entries.length).toBe(1106);
    expect(p.has('MAPS/DEMO1.BSP')).toBe(true);
  });

  it('parses maps/demo1.bsp', () => {
    const b = parseBsp(p.read('maps/demo1.bsp')!);
    expect(b.models.count).toBeGreaterThan(1);
    expect(b.planes.count).toBeGreaterThan(100);
    expect(b.nodes.count).toBeGreaterThan(100);
    expect(b.leafs.count).toBeGreaterThan(b.nodes.count / 2);
    expect(b.brushes.count).toBeGreaterThan(10);
    expect(b.areas.count).toBeGreaterThan(1);
    expect(b.entityString).toContain('info_player_start');
    expect(b.entityString.startsWith('{')).toBe(true);
    // every node references valid planes/children
    for (let i = 0; i < b.nodes.count; i++) {
      expect(b.nodes.planenum[i]!).toBeLessThan(b.planes.count);
      for (const c of [b.nodes.children[i * 2]!, b.nodes.children[i * 2 + 1]!]) {
        if (c >= 0) expect(c).toBeLessThan(b.nodes.count);
        else expect(-1 - c).toBeLessThan(b.leafs.count);
      }
    }
    const numclusters = new DataView(b.visibility.buffer, b.visibility.byteOffset).getInt32(0, true);
    expect(numclusters).toBeGreaterThan(10);
  });

  it('parses models/monsters/soldier/tris.md2', () => {
    const m = parseMd2(p.read('models/monsters/soldier/tris.md2')!);
    expect(m.header.num_frames).toBeGreaterThan(100);
    expect(m.skins.length).toBeGreaterThan(0);
    expect(m.skins[0]).toMatch(/^models\/monsters\/soldier\//);
    const cmds = decodeGlCmds(m.glcmds);
    let tris = 0;
    for (const c of cmds) {
      tris += c.indices.length - 2;
      for (const ix of c.indices) expect(ix).toBeLessThan(m.header.num_xyz);
    }
    expect(tris).toBe(m.header.num_tris);
    for (const f of m.frames)
      for (let i = 3; i < f.verts.length; i += 4) expect(f.verts[i]!).toBeLessThan(162);
  });

  it('parses pics/colormap.pcx and palette', () => {
    const img = decodePcx(p.read('pics/colormap.pcx')!);
    expect([img.width, img.height]).toEqual([256, 320]);
    expect(img.palette.length).toBe(768);
    // palette index 0 is black in the Quake 2 palette
    expect(Array.from(img.palette.subarray(0, 3))).toEqual([0, 0, 0]);
    const conchars = decodePcx(p.read('pics/conchars.pcx')!);
    expect([conchars.width, conchars.height]).toEqual([128, 128]);
  });

  it('parses a WAL and a WAV', () => {
    const wal = byExt(/\.wal$/i)[0]!;
    const t = parseWal(p.readEntry(wal));
    expect(t.width).toBeGreaterThan(0);
    expect(t.name.length).toBeGreaterThan(0);
    const info = GetWavinfo('x', p.read('sound/world/ten0.wav') ?? p.readEntry(byExt(/\.wav$/i)[0]!));
    expect(info.channels).toBe(1);
    expect(info.samples).toBeGreaterThan(0);
  });

  it('parses every bsp/md2/sp2/wal/pcx/tga/wav without throwing', () => {
    const counts: Record<string, number> = {};
    for (const e of p.entries) {
      const ext = e.name.slice(e.name.lastIndexOf('.') + 1).toLowerCase();
      const d = p.readEntry(e);
      try {
        switch (ext) {
          case 'bsp':
            parseBsp(d);
            break;
          case 'md2':
            decodeGlCmds(parseMd2(d, e.name).glcmds);
            break;
          case 'sp2':
            for (const f of parseSp2(d, e.name).frames) expect(p.has(f.name)).toBe(true);
            break;
          case 'wal':
            parseWal(d, e.name);
            break;
          case 'pcx':
            decodePcx(d, e.name);
            break;
          case 'tga':
            decodeTga(d, e.name);
            break;
          case 'wav': {
            const info = GetWavinfo(e.name, d);
            expect(info.error).toBeUndefined();
            S_LoadSound(e.name, d, 22050);
            break;
          }
          case 'cin':
            new CinReader(d).readNextFrame();
            break;
          default:
            continue;
        }
      } catch (err) {
        throw new Error(`${e.name}: ${(err as Error).message}`);
      }
      counts[ext] = (counts[ext] ?? 0) + 1;
    }
    expect(counts['bsp']).toBe(3);
    expect(counts['wav']).toBe(375);
    expect(counts['tga']).toBe(6);
  });
});
