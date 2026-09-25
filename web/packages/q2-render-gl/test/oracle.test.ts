// Golden tests against the C ref_gl oracle (scripts/oracle/oracle.c -> test/data/oracle.json).
// The oracle links the real Quake-2/ref_gl code with a no-op GL; every value compared here must match
// bit for bit (float32 values are compared exactly, buffers by FNV-1a hash).
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  RF_GLOW,
  RF_MINLIGHT,
  RF_SHELL_HALF_DAM,
  RF_SHELL_RED,
  RF_TRANSLUCENT,
  SURF_FLOWING,
  fr,
} from 'q2-shared';
import { newRefEntity, type DLight, type LightStyle } from 'q2-ref';
import type { GLPoly, MSurface } from '../src/gl_model_h';
import { SURF_DRAWTURB, VERTEXSIZE } from '../src/gl_model_h';
import { R_BuildLightMap, R_LightPoint, lightMapContext } from '../src/gl_light';
import { R_ClearSkyBox, R_AddSkySurface, R_DrawSkyBox, R_SetSky, EmitWaterPolys } from '../src/gl_warp';
import { R_DrawAliasModel } from '../src/gl_mesh';
import { R_RegisterModel } from '../src/gl_model';
import { Draw_Pic, stretchRawResample } from '../src/gl_draw';
import { R_SetPalette } from '../src/gl_rmain';
import {
  GL_InitImages,
  GL_LightScaleTexture,
  GL_MipMap,
  GL_ResampleTexture,
  R_FloodFillSkin,
} from '../src/gl_image';
import type { Model } from '../src/gl_model_h';
import { Lcg, demoPak, fnv, fnvInts, loadDemo1 } from './helpers';

const here = dirname(fileURLToPath(import.meta.url));
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const O: any = JSON.parse(readFileSync(join(here, 'data', 'oracle.json'), 'utf8'));
const f32 = (a: number[]): number[] => Array.from(Float32Array.from(a));

function polyHash(h: number, p: GLPoly | null): number {
  for (; p; p = p.next) {
    h = fnvInts([p.numverts], h);
    h = fnv(new Uint8Array(Float32Array.from(p.verts.subarray(0, p.numverts * VERTEXSIZE)).buffer), h);
  }
  return h;
}

const hasPak = demoPak() !== null;

describe.skipIf(!hasPak)('ref_gl oracle (demo1)', () => {
  it('world counts, all surface fields and all polygons', () => {
    const { r } = loadDemo1();
    const w = r.r_worldmodel!;
    expect([w.numsurfaces, w.numnodes, w.numleafs, w.numtexinfo, w.numsubmodels]).toEqual([
      O.world.numsurfaces,
      O.world.numnodes,
      O.world.numleafs,
      O.world.numtexinfo,
      O.world.numsubmodels,
    ]);
    let h = 2166136261;
    for (const s of w.surfaces) h = polyHash(h, s.polys);
    expect(h).toBe(O.world.allPolysHash);
    h = 2166136261;
    for (const s of w.surfaces) {
      h = fnvInts(
        [
          s.texturemins[0]!,
          s.texturemins[1]!,
          s.extents[0]!,
          s.extents[1]!,
          s.light_s,
          s.light_t,
          s.lightmaptexturenum,
          s.flags,
        ],
        h,
      );
    }
    expect(h).toBe(O.world.allSurfHash);
  });

  it('sampled surfaces', () => {
    const { r } = loadDemo1();
    const w = r.r_worldmodel!;
    for (const e of O.surfaces) {
      const s = w.surfaces[e.i]!;
      let np = 0;
      for (let p = s.polys; p; p = p.next) np++;
      expect({
        i: e.i,
        flags: s.flags,
        texturemins: Array.from(s.texturemins),
        extents: Array.from(s.extents),
        light: [s.light_s, s.light_t],
        lmtex: s.lightmaptexturenum,
        image: s.texinfo.image.name,
        npolys: np,
        polyHash: polyHash(2166136261, s.polys),
      }).toEqual({ ...e, verts: undefined });
      if (s.polys)
        expect(Array.from(s.polys.verts.subarray(0, s.polys.numverts * VERTEXSIZE))).toEqual(f32(e.verts));
    }
  });

  it('lightmap pages (GL_CreateSurfaceLightmap / LM_UploadBlock)', () => {
    const { r } = loadDemo1();
    expect(r.lmNumPages - 1).toBe(O.lightmaps.length);
    for (const e of O.lightmaps) expect({ page: e.page, hash: fnv(r.lmCanon[e.page]!) }).toEqual(e);
  });

  it('image uploads (GL_LoadPic / GL_Upload8 / GL_Upload32 / GL_MipMap / light scale)', () => {
    const { r, rec } = loadDemo1();
    for (const e of O.images) {
      const im = r.gltextures.find((g) => g.name === e.name && g.texnum)!;
      expect(im, e.name).toBeDefined();
      const levels = rec.uploads
        .filter((u) => u.texnum === im.texnum)
        .map((u) => [u.level, u.comp, u.w, u.h, u.hash]);
      expect({
        name: im.name,
        type: im.type,
        w: im.width,
        h: im.height,
        uw: im.upload_width,
        uh: im.upload_height,
        alpha: im.has_alpha ? 1 : 0,
        levels,
      }).toEqual(e);
    }
  });

  it('R_BuildLightMap with lightstyles, mono modes, dynamic lights and gl_modulate', () => {
    const { r, cvars } = loadDemo1();
    const w = r.r_worldmodel!;
    const styles: LightStyle[] = [];
    for (let i = 0; i < 256; i++) {
      const rgb = new Float32Array(3);
      rgb[0] = fr(0.5 + fr((i % 7) * fr(0.2)));
      rgb[1] = fr(fr(1.7) - fr((i % 5) * fr(0.3)));
      rgb[2] = 1;
      styles.push({ rgb, white: fr(fr(rgb[0]! + rgb[1]!) + rgb[2]!) });
    }
    r.r_newrefdef.lightstyles = styles;
    const dlights: DLight[] = [0, 1].map(() => ({
      origin: new Float32Array(3),
      color: new Float32Array(3),
      intensity: 0,
    }));
    r.r_newrefdef.dlights = dlights;
    const ctx = lightMapContext(r);
    const mono = cvars.get('gl_monolightmap')!;
    const modulate = cvars.get('gl_modulate')!;
    const buf = new Uint8Array(34 * 34 * 4);
    let n = 0;
    for (const e of O.buildLightmap) {
      const s: MSurface = w.surfaces[e.i]!;
      const smax = (s.extents[0]! >> 4) + 1;
      const tmax = (s.extents[1]! >> 4) + 1;
      const h: number[] = [];
      for (const m of ['0', 'C', 'A', 'L']) {
        mono.string = m;
        r.r_framecount = 100;
        s.dlightframe = 0;
        buf.fill(0);
        R_BuildLightMap(ctx, s, buf, 0, smax * 4);
        h.push(fnv(buf.subarray(0, smax * tmax * 4)));
      }
      mono.string = '0';
      if (s.polys) {
        const v = s.polys.verts;
        const sign = s.flags & 2 ? -1 : 1;
        for (let k = 0; k < 2; k++) {
          const d = dlights[k]!;
          const n3 = s.plane.normal;
          d.origin[0] = fr(fr(v[0]! + fr(fr(fr(n3[0]! * sign) * (20 + 30 * k)))) + 8 * k);
          d.origin[1] = fr(fr(v[1]! + fr(fr(fr(n3[1]! * sign) * (20 + 30 * k)))) - 5 * k);
          d.origin[2] = fr(v[2]! + fr(fr(n3[2]! * sign) * (20 + 30 * k)));
          d.intensity = 200 + 100 * k;
          d.color[0] = 1;
          d.color[1] = fr(0.5 + fr(0.25 * k));
          d.color[2] = 0.25;
        }
        r.r_newrefdef.num_dlights = 2;
        s.dlightframe = r.r_framecount;
        s.dlightbits = 3;
        buf.fill(0);
        R_BuildLightMap(ctx, s, buf, 0, smax * 4);
        h.push(fnv(buf.subarray(0, smax * tmax * 4)));
        modulate.value = 1.5;
        R_BuildLightMap(ctx, s, buf, 0, smax * 4);
        h.push(fnv(buf.subarray(0, smax * tmax * 4)));
        modulate.value = 1;
        s.dlightframe = 0;
        r.r_newrefdef.num_dlights = 0;
      }
      expect({ i: e.i, size: [smax, tmax], h }).toEqual(e);
      n++;
    }
    expect(n).toBeGreaterThan(100);
  });

  it('R_LightPoint', () => {
    const { r } = loadDemo1();
    const w = r.r_worldmodel!;
    const ent = newRefEntity();
    r.currententity = ent;
    const dl = r.r_newrefdef.dlights[0]!; // left over from the previous test like the oracle's static array
    const p = new Float32Array(3);
    const c = new Float32Array(3);
    let idx = 0;
    for (let i = 0; i < w.numsurfaces; i += 97) {
      const s = w.surfaces[i]!;
      if (!s.polys) continue;
      const sign = s.flags & 2 ? -1 : 1;
      for (let k = 0; k < 3; k++) {
        p[k] = fr(s.polys.verts[k]! + fr(fr(s.plane.normal[k]! * sign) * 24));
        ent.origin[k] = p[k]!;
      }
      r.r_newrefdef.num_dlights = Math.trunc(i / 97) & 1;
      dl.origin[0] = fr(p[0]! + 10);
      dl.origin[1] = p[1]!;
      dl.origin[2] = fr(p[2]! + 5);
      R_LightPoint(r, p, c);
      const e = O.lightPoint[idx++];
      expect({
        p: Array.from(p),
        dl: r.r_newrefdef.num_dlights,
        c: Array.from(c),
        spot: Array.from(r.lightspot),
      }).toEqual({
        p: f32(e.p),
        dl: e.dl,
        c: f32(e.c),
        spot: f32(e.spot),
      });
    }
    expect(idx).toBe(O.lightPoint.length);
    r.r_newrefdef.num_dlights = 0;
  });

  it('R_SetSky images, R_AddSkySurface bounds and R_DrawSkyBox vertices', () => {
    const { r, rec } = loadDemo1();
    const w = r.r_worldmodel!;
    R_SetSky(r, 'unit1_', 0, new Float32Array([0, 0, 1]));
    for (let i = 0; i < 6; i++) {
      const im = r.sky_images[i]!;
      const levels = rec.uploads
        .filter((u) => u.texnum === im.texnum)
        .map((u) => [u.level, u.comp, u.w, u.h, u.hash]);
      expect({ name: im.name, levels }).toEqual(O.skyImages[i]);
    }
    for (const e of O.sky) {
      r.r_origin.set(e.origin);
      R_ClearSkyBox(r);
      for (const s of w.surfaces) if (s.texinfo.flags & 4) R_AddSkySurface(r, s);
      expect(Array.from(r.skymins)).toEqual(f32(e.mins));
      expect(Array.from(r.skymaxs)).toEqual(f32(e.maxs));
      rec.start();
      R_DrawSkyBox(r);
      const d = rec.stop();
      expect({ count: d.count, prims: d.prims, hash: d.hash, first: d.first.slice(0, 24) }).toEqual({
        ...e.draw,
        first: e.draw.first.map(f32),
      });
    }
  });

  it('EmitWaterPolys warp texture coordinates', () => {
    const { r, rec } = loadDemo1();
    const w = r.r_worldmodel!;
    const ws = w.surfaces.find((s) => s.flags & SURF_DRAWTURB)!;
    const wf = w.surfaces.find((s) => s.flags & SURF_DRAWTURB && s.texinfo.flags & SURF_FLOWING);
    let idx = 0;
    for (const t of [1.5, 13.37, 97.25]) {
      r.r_newrefdef.time = fr(t);
      for (const s of wf ? [ws, wf] : [ws]) {
        rec.start();
        EmitWaterPolys(r, s);
        const d = rec.stop();
        const e = O.warp[idx++];
        expect({
          surf: w.surfaces.indexOf(s),
          time: fr(t),
          draw: { ...d, first: d.first.slice(0, 12) },
        }).toEqual({
          ...e,
          time: fr(e.time),
          draw: { ...e.draw, first: e.draw.first.map(f32) },
        });
      }
    }
    expect(idx).toBe(O.warp.length);
  });

  it('R_DrawAliasModel (GL_LerpVerts, shadedots, shells, glow, minlight)', () => {
    const { r, rec } = loadDemo1();
    const mod = R_RegisterModel(r, 'models/monsters/soldier/tris.md2') as Model;
    expect(mod).toBeTruthy();
    const flags = [0, RF_SHELL_RED, RF_TRANSLUCENT | RF_GLOW, RF_SHELL_HALF_DAM | RF_MINLIGHT];
    r.r_newrefdef.time = 3.25;
    r.r_newrefdef.num_dlights = 1;
    const dl = r.r_newrefdef.dlights[0]!;
    dl.origin.set([100, 20, 40]);
    dl.intensity = 300;
    for (let k = 0; k < 4; k++) {
      const ent = newRefEntity();
      ent.model = mod as never;
      ent.frame = 5 + k;
      ent.oldframe = 4 + k;
      ent.backlerp = fr(0.3);
      ent.origin.set([120.5, -40.25, 30]);
      ent.oldorigin.set([118, -41, 29.5]);
      ent.angles.set([10, 37.5 + 90 * k, -5]);
      ent.alpha = fr(0.6);
      ent.flags = flags[k]!;
      r.currententity = ent;
      r.currentmodel = mod;
      rec.start();
      R_DrawAliasModel(r, ent);
      const d = rec.stop();
      const e = O.alias[k];
      expect({ flags: ent.flags, frame: ent.frame, draw: { ...d, first: d.first.slice(0, 6) } }).toEqual({
        ...e,
        draw: { ...e.draw, first: e.draw.first.map(f32) },
      });
    }
    r.r_newrefdef.num_dlights = 0;
  });

  it('scrap allocation and upload (Draw_Pic of small pics)', () => {
    const { r, rec } = loadDemo1();
    const n0 = rec.uploads.length;
    Draw_Pic(r, 0, 0, 'i_health');
    Draw_Pic(r, 0, 0, 'a_bullets');
    Draw_Pic(r, 10, 0, 'i_health');
    expect(rec.uploads.slice(n0).map((u) => [u.texnum, u.comp, u.w, u.h, u.hash])).toEqual(O.scrap);
  });

  it('Draw_StretchRaw resampling with the game palette', () => {
    const { r } = loadDemo1();
    R_SetPalette(r, null);
    const data = new Uint8Array(320 * 300);
    const img = new Uint8Array(256 * 256 * 4);
    for (const e of O.stretchRaw) {
      const lcg = new Lcg(e.seed);
      for (let i = 0; i < e.cols * e.rows; i++) data[i] = lcg.next() & 255;
      stretchRawResample(e.cols, e.rows, data, r.rawImage8);
      const trows = e.rows > 256 ? 256 : e.rows;
      for (let i = 0; i < 256 * trows; i++) {
        const c = r.r_rawpalette[r.rawImage8[i]!]!;
        img[i * 4] = c & 255;
        img[i * 4 + 1] = (c >>> 8) & 255;
        img[i * 4 + 2] = (c >>> 16) & 255;
        img[i * 4 + 3] = c >>> 24;
      }
      expect(fnv(img.subarray(0, 256 * trows * 4))).toBe(e.hash);
    }
  });

  it('gamma and intensity tables (GL_InitImages / GL_LightScaleTexture)', () => {
    const { r, cvars } = loadDemo1();
    for (const e of O.gamma) {
      r.ri.cvarSet('vid_gamma', String(e.gamma));
      r.ri.cvarSet('intensity', String(e.intensity));
      r.cv.vid_gamma = cvars.get('vid_gamma')!;
      GL_InitImages(r);
      const ramp = new Uint8Array(256 * 4);
      const fill = (): void => {
        for (let i = 0; i < 256; i++) {
          ramp[i * 4] = ramp[i * 4 + 1] = ramp[i * 4 + 2] = i;
          ramp[i * 4 + 3] = 255;
        }
      };
      fill();
      GL_LightScaleTexture(ramp, 256, 1, true, r.gammatable, r.intensitytable);
      const g = Array.from({ length: 256 }, (_, i) => ramp[i * 4]!);
      fill();
      GL_LightScaleTexture(ramp, 256, 1, false, r.gammatable, r.intensitytable);
      const gi = Array.from({ length: 256 }, (_, i) => ramp[i * 4]!);
      expect({ gamma: e.gamma, intensity: e.intensity, g, gi }).toEqual(e);
    }
  });
});

describe('ref_gl oracle (synthetic image functions)', () => {
  const words = (lcg: Lcg, n: number): Uint8Array => {
    const u = new Uint32Array(n);
    for (let i = 0; i < n; i++) {
      const a = lcg.next();
      const b = lcg.next();
      u[i] = (a | (b << 16)) >>> 0;
    }
    return new Uint8Array(u.buffer);
  };

  it('GL_ResampleTexture', () => {
    for (const e of O.resample) {
      const [iw, ih, ow, oh] = e.size;
      const inp = words(new Lcg(e.seed), iw * ih);
      const out = new Uint8Array(ow * oh * 4);
      GL_ResampleTexture(inp, iw, ih, out, ow, oh);
      expect(fnv(out)).toBe(e.hash);
    }
  });

  it('GL_MipMap', () => {
    for (const e of O.mipmap) {
      const [w, h] = e.size;
      const data = words(new Lcg(e.seed), w * h);
      GL_MipMap(data, w, h);
      expect(fnv(data)).toBe(e.hash);
    }
  });

  it.skipIf(!hasPak)('R_FloodFillSkin', () => {
    const { r } = loadDemo1();
    for (let k = 0; k < 3; k++) {
      const e = O.floodfill[k];
      const lcg = new Lcg(e.seed);
      const skin = new Uint8Array(256);
      for (let i = 0; i < 256; i++) skin[i] = lcg.next() % 5 === 0 ? lcg.next() & 255 : k === 2 ? 255 : 17;
      skin[0] = k === 2 ? 255 : 17;
      R_FloodFillSkin(skin, 16, 16, r.d_8to24table);
      expect(fnv(skin)).toBe(e.hash);
    }
  });
});
