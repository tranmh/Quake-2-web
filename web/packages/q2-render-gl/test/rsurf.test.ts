// Dynamic lightmap bookkeeping of the port (canonical / displayed page mirrors, see gl_rsurf.ts).
import { describe, expect, it } from 'vitest';
import type { LightStyle } from 'q2-ref';
import { R_UpdateSurfaceLightmap } from '../src/gl_rsurf';
import { R_BuildLightMap, lightMapContext } from '../src/gl_light';
import { BLOCK_WIDTH } from '../src/gl_local';
import { LM_AllocBlock } from '../src/gl_rsurf';
import { demoPak, loadDemo1 } from './helpers';

function region(page: Uint8Array, s: number, t: number, smax: number, tmax: number): Uint8Array {
  const out = new Uint8Array(smax * tmax * 4);
  for (let y = 0; y < tmax; y++)
    out.set(
      page.subarray(((t + y) * BLOCK_WIDTH + s) * 4, ((t + y) * BLOCK_WIDTH + s + smax) * 4),
      y * smax * 4,
    );
  return out;
}

describe('LM_AllocBlock', () => {
  it('packs like C (column scan stops at BLOCK_WIDTH-w)', () => {
    const alloc = new Int32Array(128);
    const p = { x: 0, y: 0 };
    expect(LM_AllocBlock(alloc, 10, 5, p)).toBe(true);
    expect(p).toEqual({ x: 0, y: 0 });
    expect(LM_AllocBlock(alloc, 10, 5, p)).toBe(true);
    expect(p).toEqual({ x: 10, y: 0 });
    expect(LM_AllocBlock(alloc, 128, 1, p)).toBe(false);
    expect(LM_AllocBlock(alloc, 127, 1, p)).toBe(true);
    expect(p).toEqual({ x: 0, y: 5 });
  });
});

describe.skipIf(demoPak() === null)('dynamic lightmaps', () => {
  it('styled surfaces are rebuilt into the displayed page and restored from the canonical page', () => {
    const { r } = loadDemo1();
    const w = r.r_worldmodel!;
    const styles: LightStyle[] = [];
    for (let i = 0; i < 256; i++) styles.push({ rgb: new Float32Array([1, 1, 1]), white: 3 });
    r.r_newrefdef.lightstyles = styles;
    r.r_newrefdef.num_dlights = 0;
    r.r_framecount = 1000;
    // a lit surface using a switchable/animated style < 32 (the "block 0" path in C)
    const s = w.surfaces.find(
      (x) => x.samples && x.styles[0] === 0 && x.styles[1]! > 0 && x.styles[1]! < 32 && x.polys,
    );
    expect(s).toBeDefined();
    if (!s) return;
    const smax = (s.extents[0]! >> 4) + 1;
    const tmax = (s.extents[1]! >> 4) + 1;
    const page = s.lightmaptexturenum;
    const canon0 = region(r.lmCanon[page]!, s.light_s, s.light_t, smax, tmax);
    styles[s.styles[1]!] = { rgb: new Float32Array([0.2, 0.2, 0.2]), white: 0.6 };
    R_UpdateSurfaceLightmap(r, s);
    expect(s.lmTemp).toBe(true);
    const expected = new Uint8Array(smax * tmax * 4);
    R_BuildLightMap(lightMapContext(r), s, expected, 0, smax * 4);
    expect(region(r.lmDisp[page]!, s.light_s, s.light_t, smax, tmax)).toEqual(expected);
    expect(region(r.lmCanon[page]!, s.light_s, s.light_t, smax, tmax)).toEqual(canon0);
    expect(r.gl_lms.lightmap_surfaces[0]).toBe(s);
    // back to normal: restored from the canonical page, chained on its own page
    styles[s.styles[1]!] = { rgb: new Float32Array([1, 1, 1]), white: 3 };
    R_UpdateSurfaceLightmap(r, s);
    expect(s.lmTemp).toBe(false);
    expect(region(r.lmDisp[page]!, s.light_s, s.light_t, smax, tmax)).toEqual(canon0);
    expect(r.gl_lms.lightmap_surfaces[page]).toBe(s);
  });
});
