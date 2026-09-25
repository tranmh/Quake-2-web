// Port of ref_gl/gl_rsurf.c: surface-related refresh code (world and brush models, lightmap allocation).
//
// Rendering follows the C non-multitexture path: GL_SGIS_multitexture does not exist in WebGL (as on
// every modern OpenGL driver, so qglMTexCoord2fSGIS is NULL in C too). The immediate-mode draws are
// replaced as follows:
//  * R_RenderBrushPoly keeps the per-surface logic (texture animation, lightmap modification check,
//    lightmap chains) but, instead of drawing the polygon, queues the surface. The queue is flushed as
//    (texture, lightmap page, SURF_FLOWING) batches over the static vertex buffer built from
//    GL_BuildPolygonFromSurface (7 floats per vertex as C) with a per-frame index list.
//  * R_BlendLightmaps' second pass (glBlendFunc(GL_ZERO, GL_SRC_COLOR) etc.) is folded into the same draw
//    by the world shader (qgl.ts LM_* modes), which is equivalent for opaque surfaces.
//  * Lightmap pages live in a TEXTURE_2D_ARRAY. C keeps the static pages in GL textures 1..N and draws
//    dynamic surfaces from the scratch block 0 (re-packed every frame by LM_AllocBlock). Here each page
//    has a canonical CPU mirror (== the C GL texture) and a displayed mirror (== the array layer):
//    surfaces C would draw from block 0 get their per-frame lightmap written into the displayed page at
//    their own location (surf.lmTemp), and are restored from the canonical page once they stop being
//    dynamic. The texels sampled for every surface are thus identical to C's.
import {
  CONTENTS_SOLID,
  PLANE_X,
  PLANE_Y,
  PLANE_Z,
  RF_TRANSLUCENT,
  SURF_FLOWING,
  SURF_SKY,
  SURF_TRANS33,
  SURF_TRANS66,
  SURF_WARP,
  AngleVectors,
  DotProduct,
  ERR_DROP,
  ERR_FATAL,
  VectorAdd,
  VectorCopy,
  VectorSubtract,
  fr,
} from 'q2-shared';
import type { RefEntity } from 'q2-ref';
import {
  BACKFACE_EPSILON,
  BLOCK_HEIGHT,
  BLOCK_WIDTH,
  GLState,
  Image,
  LIGHTMAP_BYTES,
  MAX_LIGHTMAPS,
  TEXNUM_LIGHTMAPS,
} from './gl_local';
import {
  GLPoly,
  MAXLIGHTMAPS,
  MNode,
  MSurface,
  MTexinfo,
  Model,
  SURF_DRAWTURB,
  SURF_PLANEBACK,
  VERTEXSIZE,
} from './gl_model_h';
import { GL_Bind, GL_EnableMultitexture, GL_SelectTexture, GL_TexEnv } from './gl_image';
import {
  R_BuildLightMap,
  R_MarkLights,
  R_SetCacheState,
  lightMapContext,
  type LightMapContext,
} from './gl_light';
import { EmitWaterPolys, R_AddSkySurface, R_ClearSkyBox, R_DrawSkyBox } from './gl_warp';
import { Mod_ClusterPVS } from './gl_model';
import { R_CullBox, R_RotateForEntity } from './gl_rmain';
import {
  GL_BLEND,
  GL_DEPTH_TEST,
  GL_LINE_STRIP,
  GL_MODULATE,
  GL_POLYGON,
  GL_REPLACE,
  GL_TEXTURE_2D,
  LMSWZ_INTENSITY,
  LMSWZ_LUMINANCE,
  LMSWZ_RGB,
  LMSWZ_RGBA,
  LM_ALPHA,
  LM_MODULATE,
  LM_NONE,
  LM_REPLACE,
  LM_SATURATE,
  type WorldBuffer,
} from './qgl';

const nullEntity: RefEntity = {
  model: null,
  angles: new Float32Array(3),
  origin: new Float32Array(3),
  frame: 0,
  oldorigin: new Float32Array(3),
  oldframe: 0,
  backlerp: 0,
  skinnum: 0,
  lightstyle: 0,
  alpha: 0,
  skin: null,
  flags: 0,
};

function lmctx(r: GLState): LightMapContext {
  if (!r.lmctx) r.lmctx = lightMapContext(r);
  return r.lmctx as LightMapContext;
}

// =============================================================
// BRUSH MODELS

// C: gl_rsurf.c:83 R_TextureAnimation -- returns the proper texture for a given time and base texture
export function R_TextureAnimation(r: GLState, tex: MTexinfo): Image {
  if (!tex.next) return tex.image;
  let c = r.currententity.frame % tex.numframes;
  let t: MTexinfo = tex;
  while (c) {
    t = t.next!;
    c--;
  }
  return t.image;
}

// C: gl_rsurf.c:180 DrawGLPoly
export function DrawGLPoly(r: GLState, p: GLPoly): void {
  const qgl = r.qgl;
  if (!qgl) return;
  const v = p.verts;
  qgl.begin(GL_POLYGON);
  for (let i = 0, o = 0; i < p.numverts; i++, o += VERTEXSIZE) {
    qgl.texCoord2f(v[o + 3]!, v[o + 4]!);
    qgl.vertex3f(v[o]!, v[o + 1]!, v[o + 2]!);
  }
  qgl.end();
}

/** C: the SURF_FLOWING scroll of DrawGLFlowingPoly / GL_RenderLightmappedPoly. */
export function flowingScroll(time: number): number {
  let scroll = fr(-64 * (time / 40.0 - Math.trunc(time / 40.0)));
  if (scroll === 0.0) scroll = -64.0;
  return scroll;
}

// C: gl_rsurf.c:202 DrawGLFlowingPoly -- version of DrawGLPoly that handles scrolling texture
export function DrawGLFlowingPoly(r: GLState, fa: MSurface): void {
  const qgl = r.qgl;
  const p = fa.polys;
  if (!qgl || !p) return;
  const scroll = flowingScroll(fr(r.r_newrefdef.time));
  const v = p.verts;
  qgl.begin(GL_POLYGON);
  for (let i = 0, o = 0; i < p.numverts; i++, o += VERTEXSIZE) {
    qgl.texCoord2f(fr(v[o + 3]! + scroll), v[o + 4]!);
    qgl.vertex3f(v[o]!, v[o + 1]!, v[o + 2]!);
  }
  qgl.end();
}

// C: gl_rsurf.c:230 R_DrawTriangleOutlines
export function R_DrawTriangleOutlines(r: GLState): void {
  if (!r.cv.gl_showtris.value) return;
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.disable(GL_TEXTURE_2D);
  qgl.disable(GL_DEPTH_TEST);
  qgl.color4f(1, 1, 1, 1);
  for (let i = 0; i < MAX_LIGHTMAPS; i++) {
    for (let surf = r.gl_lms.lightmap_surfaces[i] ?? null; surf; surf = surf.lightmapchain) {
      for (let p = surf.polys; p; p = p.chain) {
        const v = p.verts;
        for (let j = 2; j < p.numverts; j++) {
          qgl.begin(GL_LINE_STRIP);
          qgl.vertex3f(v[0]!, v[1]!, v[2]!);
          qgl.vertex3f(v[(j - 1) * VERTEXSIZE]!, v[(j - 1) * VERTEXSIZE + 1]!, v[(j - 1) * VERTEXSIZE + 2]!);
          qgl.vertex3f(v[j * VERTEXSIZE]!, v[j * VERTEXSIZE + 1]!, v[j * VERTEXSIZE + 2]!);
          qgl.vertex3f(v[0]!, v[1]!, v[2]!);
          qgl.end();
        }
      }
    }
  }
  qgl.enable(GL_DEPTH_TEST);
  qgl.enable(GL_TEXTURE_2D);
}

// ---------------------------------------------------------------- port: lightmap page mirrors

function markDirty(r: GLState, page: number, x: number, y: number, w: number, h: number): void {
  const d = r.lmDirty;
  const o = page * 4;
  if (d[o]! > x) d[o] = x;
  if (d[o + 1]! > y) d[o + 1] = y;
  if (d[o + 2]! < x + w) d[o + 2] = x + w;
  if (d[o + 3]! < y + h) d[o + 3] = y + h;
}

function clearDirty(r: GLState, page: number): void {
  const o = page * 4;
  r.lmDirty[o] = r.lmDirty[o + 1] = 0x7fffffff;
  r.lmDirty[o + 2] = r.lmDirty[o + 3] = -1;
}

/** Upload the dirty rectangles of the displayed pages (C: the qglTexSubImage2D calls). */
export function R_UploadDirtyLightmaps(r: GLState): void {
  const qgl = r.qgl;
  for (let page = 0; page < r.lmNumPages; page++) {
    const o = page * 4;
    const x0 = r.lmDirty[o]!;
    const x1 = r.lmDirty[o + 2]!;
    if (x1 <= x0) continue;
    const y0 = r.lmDirty[o + 1]!;
    const y1 = r.lmDirty[o + 3]!;
    qgl?.lightmapSubImage(page, x0, y0, x1 - x0, y1 - y0, r.lmDisp[page]!);
    clearDirty(r, page);
  }
}

function copyLightmapRegion(
  src: Uint8Array,
  dst: Uint8Array,
  s: number,
  t: number,
  smax: number,
  tmax: number,
): void {
  for (let y = 0; y < tmax; y++) {
    let o = ((t + y) * BLOCK_WIDTH + s) * LIGHTMAP_BYTES;
    const e = o + smax * LIGHTMAP_BYTES;
    for (; o < e; o++) dst[o] = src[o]!;
  }
}

/**
 * C: gl_rsurf.c:477 R_RenderBrushPoly, the "check for lightmap modification" part and the lightmap chain
 * insertion. Updates the page mirrors instead of calling qglTexSubImage2D / queueing on block 0.
 */
export function R_UpdateSurfaceLightmap(r: GLState, fa: MSurface): void {
  const lightstyles = r.r_newrefdef.lightstyles;
  let maps;
  let dyn = false;
  for (maps = 0; maps < MAXLIGHTMAPS && fa.styles[maps] !== 255; maps++) {
    if (fr(lightstyles[fa.styles[maps]!]!.white) !== fa.cached_light[maps]) {
      dyn = true; // goto dynamic
      break;
    }
  }
  // dynamic this frame or dynamic previously
  if (fa.dlightframe === r.r_framecount) dyn = true;
  let is_dynamic = false;
  if (dyn && r.cv.gl_dynamic.value) {
    if (!(fa.texinfo.flags & (SURF_SKY | SURF_TRANS33 | SURF_TRANS66 | SURF_WARP))) is_dynamic = true;
  }

  const lms = r.gl_lms;
  const page = fa.lightmaptexturenum;
  const smax = (fa.extents[0]! >> 4) + 1;
  const tmax = (fa.extents[1]! >> 4) + 1;
  if (is_dynamic) {
    // styles[maps] with maps == MAXLIGHTMAPS reads past the array in C; only reachable when
    // dlightframe == r_framecount, where the result does not matter.
    const style = maps < MAXLIGHTMAPS ? fa.styles[maps]! : 255;
    if ((style >= 32 || style === 0) && fa.dlightframe !== r.r_framecount) {
      const canon = r.lmCanon[page];
      if (canon) {
        R_BuildLightMap(
          lmctx(r),
          fa,
          canon,
          (fa.light_t * BLOCK_WIDTH + fa.light_s) * LIGHTMAP_BYTES,
          BLOCK_WIDTH * LIGHTMAP_BYTES,
        );
        copyLightmapRegion(canon, r.lmDisp[page]!, fa.light_s, fa.light_t, smax, tmax);
        markDirty(r, page, fa.light_s, fa.light_t, smax, tmax);
      }
      R_SetCacheState(lightstyles, fa);
      fa.lmTemp = false;
      fa.lightmapchain = lms.lightmap_surfaces[page] ?? null;
      lms.lightmap_surfaces[page] = fa;
    } else {
      const disp = r.lmDisp[page];
      if (disp) {
        R_BuildLightMap(
          lmctx(r),
          fa,
          disp,
          (fa.light_t * BLOCK_WIDTH + fa.light_s) * LIGHTMAP_BYTES,
          BLOCK_WIDTH * LIGHTMAP_BYTES,
        );
        markDirty(r, page, fa.light_s, fa.light_t, smax, tmax);
        fa.lmTemp = true;
      }
      fa.lightmapchain = lms.lightmap_surfaces[0] ?? null;
      lms.lightmap_surfaces[0] = fa;
    }
  } else {
    if (fa.lmTemp) {
      const canon = r.lmCanon[page];
      if (canon) {
        copyLightmapRegion(canon, r.lmDisp[page]!, fa.light_s, fa.light_t, smax, tmax);
        markDirty(r, page, fa.light_s, fa.light_t, smax, tmax);
      }
      fa.lmTemp = false;
    }
    fa.lightmapchain = lms.lightmap_surfaces[page] ?? null;
    lms.lightmap_surfaces[page] = fa;
  }
}

// C: gl_rsurf.c:477 R_RenderBrushPoly
export function R_RenderBrushPoly(r: GLState, fa: MSurface): void {
  r.c_brush_polys++;
  const image = R_TextureAnimation(r, fa.texinfo);

  if (fa.flags & SURF_DRAWTURB) {
    GL_Bind(r, image.texnum);
    // warp texture, no lightmaps
    GL_TexEnv(r, GL_MODULATE);
    const ii = r.gl_state.inverse_intensity;
    r.qgl?.color4f(ii, ii, ii, 1.0);
    EmitWaterPolys(r, fa);
    GL_TexEnv(r, GL_REPLACE);
    return;
  }
  GL_Bind(r, image.texnum);
  GL_TexEnv(r, GL_REPLACE);

  // PGM: DrawGLFlowingPoly / DrawGLPoly -> queued as a batch
  queueSurface(r, fa, image);

  R_UpdateSurfaceLightmap(r, fa);
}

function queueSurface(r: GLState, fa: MSurface, image: Image): void {
  if (!fa.polys || fa.polys.firstVertex < 0) return;
  const n = r.numPending;
  if (n >= r.sortSurfs.length) return;
  r.sortSurfs[n] = fa;
  r.pendTex[n] = image.texnum;
  r.pendKey[n] = fa.lightmaptexturenum * 2 + (fa.texinfo.flags & SURF_FLOWING ? 1 : 0);
  r.numPending = n + 1;
}

/**
 * Port: sort the queued surfaces by (texture, lightmap page, flowing) with two stable counting passes and
 * build the index list and batch table.
 */
function buildBatches(r: GLState): void {
  const n = r.numPending;
  r.numBatches = 0;
  r.worldIndexCount = 0;
  if (!n) return;
  const surfs = r.sortSurfs;
  const surfs2 = r.sortSurfs2;
  const tex = r.pendTex;
  const key = r.pendKey;
  const tex2 = r.pendTex2;
  const key2 = r.pendKey2;

  // pass 1: by key (page*2+flow) into *2
  const cnt = r.sortCounts;
  cnt.fill(0);
  for (let i = 0; i < n; i++) cnt[key[i]! + 1]!++;
  for (let i = 1; i < cnt.length; i++) cnt[i]! += cnt[i - 1]!;
  for (let i = 0; i < n; i++) {
    const d = cnt[key[i]!]!++;
    surfs2[d] = surfs[i]!;
    tex2[d] = tex[i]!;
    key2[d] = key[i]!;
  }
  // pass 2: by texnum back into the primary arrays
  const ct = r.sortCountsTex;
  ct.fill(0);
  for (let i = 0; i < n; i++) ct[tex2[i]! + 1]!++;
  for (let i = 1; i < ct.length; i++) ct[i]! += ct[i - 1]!;
  for (let i = 0; i < n; i++) {
    const d = ct[tex2[i]!]!++;
    surfs[d] = surfs2[i]!;
    tex[d] = tex2[i]!;
    key[d] = key2[i]!;
  }

  const idx = r.worldIndices;
  let ni = 0;
  let nb = 0;
  let curTex = -1;
  let curKey = -1;
  for (let i = 0; i < n; i++) {
    if (tex[i] !== curTex || key[i] !== curKey) {
      if (nb > 0) r.batchCount[nb - 1] = ni - r.batchFirst[nb - 1]!;
      if (nb >= r.batchTex.length) break;
      curTex = tex[i]!;
      curKey = key[i]!;
      r.batchTex[nb] = curTex;
      r.batchPage[nb] = curKey >> 1;
      r.batchFlow[nb] = curKey & 1;
      r.batchFirst[nb] = ni;
      nb++;
    }
    const p = surfs[i]!.polys!;
    const fv = p.firstVertex;
    for (let k = 2; k < p.numverts; k++) {
      idx[ni++] = fv;
      idx[ni++] = fv + k - 1;
      idx[ni++] = fv + k;
    }
  }
  if (nb > 0) r.batchCount[nb - 1] = ni - r.batchFirst[nb - 1]!;
  r.numBatches = nb;
  r.worldIndexCount = ni;
}

/** Lightmap combine mode R_BlendLightmaps would use for the second pass. */
function lightmapMode(r: GLState): number {
  // don't bother if we're set to fullbright
  if (r.cv.r_fullbright.value) return LM_NONE;
  if (!r.r_worldmodel?.lightdata) return LM_NONE;
  if (r.cv.gl_lightmap.value) return LM_REPLACE;
  if (r.cv.gl_saturatelighting.value) return LM_SATURATE;
  const m = r.cv.gl_monolightmap.string;
  if (m[0] !== '0') {
    switch ((m[0] ?? '').toUpperCase()) {
      case 'I':
      case 'L':
        return LM_MODULATE;
      default:
        return LM_ALPHA;
    }
  }
  return LM_MODULATE;
}

/**
 * Port: draw the queued surfaces (C: the DrawGLPoly calls of R_RenderBrushPoly followed by the
 * R_BlendLightmaps pass). `mode` is the lightmap mode, `alpha` the colour alpha used by GL_REPLACE on
 * RGB textures.
 */
function flushSurfaces(r: GLState, mode: number, alpha: number): void {
  buildBatches(r);
  r.numPending = 0;
  const qgl = r.qgl;
  R_UploadDirtyLightmaps(r);
  const wm = r.currentmodel;
  if (!qgl || !r.numBatches || !wm.worldVbo) return;
  qgl.uploadWorldIndices(r.worldIndices, r.worldIndexCount);
  const scroll = flowingScroll(fr(r.r_newrefdef.time));
  const swz = r.lmSwizzle;
  for (let b = 0; b < r.numBatches; b++) {
    qgl.drawWorld(
      wm.worldVbo as WorldBuffer,
      r.batchFirst[b]!,
      r.batchCount[b]!,
      r.batchTex[b]!,
      r.batchPage[b]!,
      r.batchFlow[b] ? scroll : 0,
      mode,
      swz,
      alpha,
    );
  }
  qgl.endWorld();
}

// C: gl_rsurf.c:315 R_BlendLightmaps -- here: draws the queued surfaces with the lightmap combine
export function R_BlendLightmaps(r: GLState): void {
  if (r.currentmodel === r.r_worldmodel) r.c_visible_lightmaps = 0;
  for (let i = 1; i < MAX_LIGHTMAPS; i++) {
    if (r.gl_lms.lightmap_surfaces[i] && r.currentmodel === r.r_worldmodel) r.c_visible_lightmaps++;
  }
  if (r.cv.gl_dynamic.value && r.currentmodel === r.r_worldmodel) r.c_visible_lightmaps++;
  flushSurfaces(r, lightmapMode(r), 1);
}

// C: gl_rsurf.c:587 R_DrawAlphaSurfaces -- draw water surfaces and windows back to front
export function R_DrawAlphaSurfaces(r: GLState): void {
  const qgl = r.qgl;
  // go back to the world matrix
  qgl?.loadMatrixf(r.r_world_matrix);
  qgl?.enable(GL_BLEND);
  GL_TexEnv(r, GL_MODULATE);

  // the textures are prescaled up for a better lighting range, so scale it back down
  const intens = r.gl_state.inverse_intensity;

  for (let s = r.r_alpha_surfaces; s; s = s.texturechain) {
    GL_Bind(r, s.texinfo.image.texnum);
    r.c_brush_polys++;
    if (s.texinfo.flags & SURF_TRANS33) qgl?.color4f(intens, intens, intens, fr(0.33));
    else if (s.texinfo.flags & SURF_TRANS66) qgl?.color4f(intens, intens, intens, fr(0.66));
    else qgl?.color4f(intens, intens, intens, 1);
    if (s.flags & SURF_DRAWTURB) EmitWaterPolys(r, s);
    else if (s.polys) DrawGLPoly(r, s.polys);
  }

  GL_TexEnv(r, GL_REPLACE);
  qgl?.color4f(1, 1, 1, 1);
  qgl?.disable(GL_BLEND);
  r.r_alpha_surfaces = null;
}

// C: gl_rsurf.c:632 DrawTextureChains (the !qglSelectTextureSGIS branch)
export function DrawTextureChains(r: GLState): void {
  r.c_visible_textures = 0;
  for (let i = 0; i < r.numgltextures; i++) {
    const image = r.gltextures[i]!;
    if (!image.registration_sequence) continue;
    let s = image.texturechain;
    if (!s) continue;
    r.c_visible_textures++;
    for (; s; s = s.texturechain) R_RenderBrushPoly(r, s);
    image.texturechain = null;
  }
  GL_TexEnv(r, GL_REPLACE);
}

// C: gl_rsurf.c:877 R_DrawInlineBModel
export function R_DrawInlineBModel(r: GLState): void {
  const qgl = r.qgl;
  const cm = r.currentmodel;
  const rd = r.r_newrefdef;
  // calculate dynamic lighting for bmodel
  if (!r.cv.gl_flashblend.value) {
    for (let k = 0; k < rd.num_dlights; k++) R_MarkLights(r, rd.dlights[k]!, 1 << k, cm.nodes[cm.firstnode]!);
  }

  const translucent = !!(r.currententity.flags & RF_TRANSLUCENT);
  if (translucent) {
    qgl?.enable(GL_BLEND);
    qgl?.color4f(1, 1, 1, 0.25);
    GL_TexEnv(r, GL_MODULATE);
  }

  // draw texture
  const mo = r.modelorg;
  for (let i = 0; i < cm.nummodelsurfaces; i++) {
    const psurf = cm.surfaces[cm.firstmodelsurface + i]!;
    // find which side of the node we are on
    const pplane = psurf.plane;
    const dot = fr(DotProduct(mo, pplane.normal) - pplane.dist);

    // draw the polygon
    if (
      (psurf.flags & SURF_PLANEBACK && dot < -BACKFACE_EPSILON) ||
      (!(psurf.flags & SURF_PLANEBACK) && dot > BACKFACE_EPSILON)
    ) {
      if (psurf.texinfo.flags & (SURF_TRANS33 | SURF_TRANS66)) {
        // add to the translucent chain
        psurf.texturechain = r.r_alpha_surfaces;
        r.r_alpha_surfaces = psurf;
      } else {
        GL_EnableMultitexture(r, false);
        R_RenderBrushPoly(r, psurf);
        GL_EnableMultitexture(r, true);
      }
    }
  }

  if (!translucent) {
    R_BlendLightmaps(r);
  } else {
    // C draws the textures with GL_REPLACE under the 0.25 colour and skips the lightmap pass
    flushSurfaces(r, LM_NONE, 0.25);
    qgl?.disable(GL_BLEND);
    qgl?.color4f(1, 1, 1, 1);
    GL_TexEnv(r, GL_REPLACE);
  }
}

const bmMins = new Float32Array(3);
const bmMaxs = new Float32Array(3);
const bmTemp = new Float32Array(3);
const bmForward = new Float32Array(3);
const bmRight = new Float32Array(3);
const bmUp = new Float32Array(3);

// C: gl_rsurf.c:954 R_DrawBrushModel
export function R_DrawBrushModel(r: GLState, e: RefEntity): void {
  const cm = r.currentmodel;
  if (cm.nummodelsurfaces === 0) return;
  const qgl = r.qgl;

  r.currententity = e;
  r.gl_state.currenttextures[0] = r.gl_state.currenttextures[1] = -1;

  let rotated;
  if (e.angles[0] || e.angles[1] || e.angles[2]) {
    rotated = true;
    for (let i = 0; i < 3; i++) {
      bmMins[i] = e.origin[i]! - cm.radius;
      bmMaxs[i] = e.origin[i]! + cm.radius;
    }
  } else {
    rotated = false;
    VectorAdd(e.origin, cm.mins, bmMins);
    VectorAdd(e.origin, cm.maxs, bmMaxs);
  }

  if (R_CullBox(r, bmMins, bmMaxs)) return;

  qgl?.color3f(1, 1, 1);
  r.gl_lms.lightmap_surfaces.fill(null);

  VectorSubtract(r.r_newrefdef.vieworg, e.origin, r.modelorg);
  if (rotated) {
    VectorCopy(r.modelorg, bmTemp);
    AngleVectors(e.angles, bmForward, bmRight, bmUp);
    r.modelorg[0] = DotProduct(bmTemp, bmForward);
    r.modelorg[1] = -DotProduct(bmTemp, bmRight);
    r.modelorg[2] = DotProduct(bmTemp, bmUp);
  }

  qgl?.pushMatrix();
  e.angles[0] = -e.angles[0]!; // stupid quake bug
  e.angles[2] = -e.angles[2]!; // stupid quake bug
  R_RotateForEntity(r, e);
  e.angles[0] = -e.angles[0]!; // stupid quake bug
  e.angles[2] = -e.angles[2]!; // stupid quake bug

  GL_EnableMultitexture(r, true);
  GL_SelectTexture(r, 0);
  GL_TexEnv(r, GL_REPLACE);
  GL_SelectTexture(r, 1);
  GL_TexEnv(r, GL_MODULATE);

  R_DrawInlineBModel(r);
  GL_EnableMultitexture(r, false);

  qgl?.popMatrix();
}

// =============================================================
// WORLD MODEL

// C: gl_rsurf.c:1033 R_RecursiveWorldNode
export function R_RecursiveWorldNode(r: GLState, node: MNode): void {
  if (node.contents === CONTENTS_SOLID) return; // solid
  if (node.visframe !== r.r_visframecount) return;
  if (R_CullBox(r, node.mins, node.maxs)) return;

  const rd = r.r_newrefdef;
  // if a leaf node, draw stuff
  if (node.contents !== -1) {
    // check for door connected areas
    if (rd.areabits) {
      if (!(rd.areabits[node.area >> 3]! & (1 << (node.area & 7)))) return; // not visible
    }
    const marks = r.r_worldmodel!.marksurfaces;
    let c = node.nummarksurfaces;
    let m = node.firstmarksurface;
    if (c) {
      do {
        marks[m]!.visframe = r.r_framecount;
        m++;
      } while (--c);
    }
    return;
  }

  // node is just a decision point, so go down the apropriate sides
  // find which side of the node we are on
  const plane = node.plane;
  const mo = r.modelorg;
  let dot;
  switch (plane.type) {
    case PLANE_X:
      dot = fr(mo[0]! - plane.dist);
      break;
    case PLANE_Y:
      dot = fr(mo[1]! - plane.dist);
      break;
    case PLANE_Z:
      dot = fr(mo[2]! - plane.dist);
      break;
    default:
      dot = fr(DotProduct(mo, plane.normal) - plane.dist);
      break;
  }
  let side, sidebit;
  if (dot >= 0) {
    side = 0;
    sidebit = 0;
  } else {
    side = 1;
    sidebit = SURF_PLANEBACK;
  }

  // recurse down the children, front side first
  R_RecursiveWorldNode(r, node.children[side]!);

  // draw stuff
  const surfaces = r.r_worldmodel!.surfaces;
  for (let c = node.numsurfaces, si = node.firstsurface; c; c--, si++) {
    const surf = surfaces[si]!;
    if (surf.visframe !== r.r_framecount) continue;
    if ((surf.flags & SURF_PLANEBACK) !== sidebit) continue; // wrong side

    if (surf.texinfo.flags & SURF_SKY) {
      // just adds to visible sky bounds
      R_AddSkySurface(r, surf);
    } else if (surf.texinfo.flags & (SURF_TRANS33 | SURF_TRANS66)) {
      // add to the translucent chain
      surf.texturechain = r.r_alpha_surfaces;
      r.r_alpha_surfaces = surf;
    } else {
      // the polygon is visible, so add it to the texture sorted chain
      // FIXME: this is a hack for animation
      const image = R_TextureAnimation(r, surf.texinfo);
      surf.texturechain = image.texturechain;
      image.texturechain = surf;
    }
  }

  // recurse down the back side
  R_RecursiveWorldNode(r, node.children[side ? 0 : 1]!);
}

// C: gl_rsurf.c:1194 R_DrawWorld
export function R_DrawWorld(r: GLState): void {
  if (!r.cv.r_drawworld.value) return;
  const rd = r.r_newrefdef;
  if (rd.rdflags & 2 /* RDF_NOWORLDMODEL */) return;

  r.currentmodel = r.r_worldmodel!;
  VectorCopy(rd.vieworg, r.modelorg);

  // auto cycle the world frame for texture animation
  nullEntity.frame = Math.trunc(fr(rd.time * 2));
  r.currententity = nullEntity;

  r.gl_state.currenttextures[0] = r.gl_state.currenttextures[1] = -1;

  r.qgl?.color3f(1, 1, 1);
  r.gl_lms.lightmap_surfaces.fill(null);
  R_ClearSkyBox(r);

  R_RecursiveWorldNode(r, r.r_worldmodel!.nodes[0]!);

  // theoretically nothing should happen in the next two functions if multitexture is enabled
  DrawTextureChains(r);
  R_BlendLightmaps(r);

  R_DrawSkyBox(r);

  R_DrawTriangleOutlines(r);
}

// C: gl_rsurf.c:1262 R_MarkLeaves -- mark the leaves and nodes that are in the PVS for the current cluster
export function R_MarkLeaves(r: GLState): void {
  const wm = r.r_worldmodel!;
  if (
    r.r_oldviewcluster === r.r_viewcluster &&
    r.r_oldviewcluster2 === r.r_viewcluster2 &&
    !r.cv.r_novis.value &&
    r.r_viewcluster !== -1
  ) {
    return;
  }

  // development aid to let you run around and see exactly where the pvs ends
  if (r.cv.gl_lockpvs.value) return;

  r.r_visframecount++;
  r.r_oldviewcluster = r.r_viewcluster;
  r.r_oldviewcluster2 = r.r_viewcluster2;

  if (r.cv.r_novis.value || r.r_viewcluster === -1 || !wm.vis) {
    // mark everything
    for (let i = 0; i < wm.numleafs; i++) wm.leafs[i]!.visframe = r.r_visframecount;
    for (let i = 0; i < wm.numnodes; i++) wm.nodes[i]!.visframe = r.r_visframecount;
    return;
  }

  let vis = Mod_ClusterPVS(r, r.r_viewcluster, wm);
  // may have to combine two clusters because of solid water boundaries
  if (r.r_viewcluster2 !== r.r_viewcluster) {
    const fatvis = r.fatvis;
    fatvis.set(vis.subarray(0, (wm.numleafs + 7) >> 3));
    vis = Mod_ClusterPVS(r, r.r_viewcluster2, wm);
    const c = ((wm.numleafs + 31) >> 5) * 4;
    for (let i = 0; i < c; i++) fatvis[i] = fatvis[i]! | vis[i]!;
    vis = fatvis;
  }

  for (let i = 0; i < wm.numleafs; i++) {
    const leaf = wm.leafs[i]!;
    const cluster = leaf.cluster;
    if (cluster === -1) continue;
    if (vis[cluster >> 3]! & (1 << (cluster & 7))) {
      let node: MNode | null = leaf;
      do {
        if (node.visframe === r.r_visframecount) break;
        node.visframe = r.r_visframecount;
        node = node.parent;
      } while (node);
    }
  }
}

// =============================================================================
// LIGHTMAP ALLOCATION

// C: gl_rsurf.c:1351 LM_InitBlock
export function LM_InitBlock(r: GLState): void {
  r.gl_lms.allocated.fill(0);
}

// C: gl_rsurf.c:1356 LM_UploadBlock. The dynamic block 0 is not used by the port (see file comment);
// a static upload stores the block into the page mirrors (uploaded to the GPU once the map is loaded).
export function LM_UploadBlock(r: GLState, dynamic: boolean): void {
  if (dynamic) return;
  const lms = r.gl_lms;
  const texture = lms.current_lightmap_texture;
  r.lmCanon[texture] = Uint8Array.from(lms.lightmap_buffer);
  r.lmDisp[texture] = Uint8Array.from(lms.lightmap_buffer);
  clearDirty(r, texture);
  if (++lms.current_lightmap_texture === MAX_LIGHTMAPS)
    r.ri.sysError(ERR_DROP, 'LM_UploadBlock() - MAX_LIGHTMAPS exceeded\n');
  r.lmNumPages = lms.current_lightmap_texture;
}

/**
 * C: gl_rsurf.c:1408 LM_AllocBlock -- returns false when the block is full, else the position in `out`.
 * Quirk kept: the column scan stops at BLOCK_WIDTH-w.
 */
export function LM_AllocBlock(
  allocated: Int32Array,
  w: number,
  h: number,
  out: { x: number; y: number },
): boolean {
  let best = BLOCK_HEIGHT;
  for (let i = 0; i < BLOCK_WIDTH - w; i++) {
    let best2 = 0;
    let j;
    for (j = 0; j < w; j++) {
      if (allocated[i + j]! >= best) break;
      if (allocated[i + j]! > best2) best2 = allocated[i + j]!;
    }
    if (j === w) {
      // this is a valid spot
      out.x = i;
      out.y = best = best2;
    }
  }
  if (best + h > BLOCK_HEIGHT) return false;
  for (let i = 0; i < w; i++) allocated[out.x + i] = best + h;
  return true;
}

/**
 * C: gl_rsurf.c:1447 GL_BuildPolygonFromSurface, as a pure function: the glpoly_t vertices
 * (x y z s t ls lt) of a non-warped surface.
 */
export function buildSurfacePolygon(
  fa: MSurface,
  surfedges: Int32Array,
  edges: Uint16Array,
  vertexes: Float32Array,
  imageWidth: number,
  imageHeight: number,
): GLPoly {
  const lnumverts = fa.numedges;
  const poly = new GLPoly(lnumverts);
  poly.flags = fa.flags;
  const vecs = fa.texinfo.vecs;
  const pv = poly.verts;
  for (let i = 0; i < lnumverts; i++) {
    const lindex = surfedges[fa.firstedge + i]!;
    const vi = lindex > 0 ? edges[lindex * 2]! : edges[-lindex * 2 + 1]!;
    const x = vertexes[vi * 3]!;
    const y = vertexes[vi * 3 + 1]!;
    const z = vertexes[vi * 3 + 2]!;
    const ds = fr(fr(fr(fr(x * vecs[0]!) + fr(y * vecs[1]!)) + fr(z * vecs[2]!)) + vecs[3]!);
    const dt = fr(fr(fr(fr(x * vecs[4]!) + fr(y * vecs[5]!)) + fr(z * vecs[6]!)) + vecs[7]!);
    const o = i * VERTEXSIZE;
    pv[o] = x;
    pv[o + 1] = y;
    pv[o + 2] = z;
    pv[o + 3] = fr(ds / imageWidth);
    pv[o + 4] = fr(dt / imageHeight);

    // lightmap texture coordinates
    let s = fr(ds - fa.texturemins[0]!);
    s = fr(s + fa.light_s * 16);
    s = fr(s + 8);
    s = fr(s / (BLOCK_WIDTH * 16));
    let t = fr(dt - fa.texturemins[1]!);
    t = fr(t + fa.light_t * 16);
    t = fr(t + 8);
    t = fr(t / (BLOCK_HEIGHT * 16));
    pv[o + 5] = s;
    pv[o + 6] = t;
  }
  return poly;
}

// C: gl_rsurf.c:1447 GL_BuildPolygonFromSurface
export function GL_BuildPolygonFromSurface(r: GLState, fa: MSurface): void {
  const cm = r.currentmodel;
  const poly = buildSurfacePolygon(
    fa,
    cm.surfedges,
    cm.edges,
    cm.vertexes,
    fa.texinfo.image.width,
    fa.texinfo.image.height,
  );
  poly.next = fa.polys;
  fa.polys = poly;
}

const lmPos = { x: 0, y: 0 };

// C: gl_rsurf.c:1525 GL_CreateSurfaceLightmap
export function GL_CreateSurfaceLightmap(r: GLState, surf: MSurface): void {
  if (surf.flags & (4 /* SURF_DRAWSKY */ | SURF_DRAWTURB)) return;
  const smax = (surf.extents[0]! >> 4) + 1;
  const tmax = (surf.extents[1]! >> 4) + 1;
  const lms = r.gl_lms;

  if (!LM_AllocBlock(lms.allocated, smax, tmax, lmPos)) {
    LM_UploadBlock(r, false);
    LM_InitBlock(r);
    if (!LM_AllocBlock(lms.allocated, smax, tmax, lmPos)) {
      r.ri.sysError(ERR_FATAL, `Consecutive calls to LM_AllocBlock(${smax},${tmax}) failed\n`);
    }
  }
  surf.light_s = lmPos.x;
  surf.light_t = lmPos.y;
  surf.lightmaptexturenum = lms.current_lightmap_texture;

  R_SetCacheState(r.r_newrefdef.lightstyles, surf);
  R_BuildLightMap(
    lmctx(r),
    surf,
    lms.lightmap_buffer,
    (surf.light_t * BLOCK_WIDTH + surf.light_s) * LIGHTMAP_BYTES,
    BLOCK_WIDTH * LIGHTMAP_BYTES,
  );
}

// C: gl_rsurf.c:1562 GL_BeginBuildingLightmaps
export function GL_BeginBuildingLightmaps(r: GLState, _m: Model): void {
  const lms = r.gl_lms;
  lms.allocated.fill(0);
  r.r_framecount = 1; // no dlightcache

  GL_EnableMultitexture(r, true);
  GL_SelectTexture(r, 1);

  // setup the base lightstyles so the lightmaps won't have to be regenerated the first time they're seen
  r.r_newrefdef.lightstyles = r.buildLightstyles;

  if (!r.gl_state.lightmap_textures) r.gl_state.lightmap_textures = TEXNUM_LIGHTMAPS;

  lms.current_lightmap_texture = 1;

  const m = (r.cv.gl_monolightmap.string[0] ?? '').toUpperCase();
  if (m === 'A' || m === 'C') {
    lms.internal_format = r.gl_tex_alpha_format;
    r.lmSwizzle = LMSWZ_RGBA;
  } else if (m === 'I') {
    lms.internal_format = 0x804b; // GL_INTENSITY8
    r.lmSwizzle = LMSWZ_INTENSITY;
  } else if (m === 'L') {
    lms.internal_format = 0x8040; // GL_LUMINANCE8
    r.lmSwizzle = LMSWZ_LUMINANCE;
  } else {
    lms.internal_format = r.gl_tex_solid_format;
    r.lmSwizzle = LMSWZ_RGB;
  }

  // initialize the dynamic lightmap texture (page 0)
  r.lmCanon.length = 0;
  r.lmDisp.length = 0;
  r.lmCanon[0] = new Uint8Array(BLOCK_WIDTH * BLOCK_HEIGHT * LIGHTMAP_BYTES);
  r.lmDisp[0] = new Uint8Array(BLOCK_WIDTH * BLOCK_HEIGHT * LIGHTMAP_BYTES);
  clearDirty(r, 0);
  r.lmNumPages = 1;
}

// C: gl_rsurf.c:1655 GL_EndBuildingLightmaps
export function GL_EndBuildingLightmaps(r: GLState): void {
  LM_UploadBlock(r, false);
  GL_EnableMultitexture(r, false);
}

/**
 * Port: pack every GL_BuildPolygonFromSurface poly of the world model into one static vertex array
 * (poly.verts become views into it), size the per-frame buffers and upload vertices and lightmap pages.
 */
export function GL_UploadWorldGeometry(r: GLState, mod: Model): void {
  let nverts = 0;
  let nidx = 0;
  for (const s of mod.surfaces) {
    if (s.texinfo.flags & SURF_WARP) continue;
    for (let p = s.polys; p; p = p.next) {
      nverts += p.numverts;
      nidx += Math.max(0, p.numverts - 2) * 3;
    }
  }
  const verts = new Float32Array(nverts * VERTEXSIZE);
  let v = 0;
  for (const s of mod.surfaces) {
    if (s.texinfo.flags & SURF_WARP) continue;
    for (let p = s.polys; p; p = p.next) {
      verts.set(p.verts.subarray(0, p.numverts * VERTEXSIZE), v * VERTEXSIZE);
      p.verts = verts.subarray(v * VERTEXSIZE, (v + p.numverts) * VERTEXSIZE);
      p.firstVertex = v;
      v += p.numverts;
    }
  }
  mod.worldVerts = verts;
  r.worldIndices = new Uint32Array(Math.max(nidx, 3));
  const ns = mod.numsurfaces;
  r.sortSurfs = new Array<MSurface>(ns);
  r.sortSurfs2 = new Array<MSurface>(ns);
  r.pendTex = new Int32Array(ns);
  r.pendKey = new Int32Array(ns);
  r.pendTex2 = new Int32Array(ns);
  r.pendKey2 = new Int32Array(ns);
  r.numPending = 0;

  const qgl = r.qgl;
  if (qgl) {
    mod.worldVbo = qgl.createWorldBuffer(verts);
    qgl.lightmapInit(Math.max(1, r.lmNumPages));
    for (let page = 0; page < r.lmNumPages; page++) {
      qgl.lightmapSubImage(page, 0, 0, BLOCK_WIDTH, BLOCK_HEIGHT, r.lmDisp[page]!);
      clearDirty(r, page);
    }
  }
}
