// Port of ref_gl/gl_light.c: dynamic light marking, point light sampling, lightmap building.
// All float expressions follow the x86-64 single precision evaluation (Math.fround per operation);
// Q_ftol is the C cast (truncation) used by the non-x86-asm build.
import { ERR_DROP, SURF_SKY, SURF_TRANS33, SURF_TRANS66, SURF_WARP, VectorLength, fr } from 'q2-shared';
import type { DLight, LightStyle } from 'q2-ref';
import { GLState } from './gl_local';
import { MAXLIGHTMAPS, MNode, MSurface, SURF_DRAWSKY, SURF_DRAWTURB } from './gl_model_h';
import {
  GL_BLEND,
  GL_ONE,
  GL_ONE_MINUS_SRC_ALPHA,
  GL_SRC_ALPHA,
  GL_TEXTURE_2D,
  GL_TRIANGLE_FAN,
} from './qgl';

export const DLIGHT_CUTOFF = 64;

const rdV = new Float32Array(3);

// C: gl_light.c:36 R_RenderDlight
function R_RenderDlight(r: GLState, light: DLight): void {
  const qgl = r.qgl!;
  const rad = fr(light.intensity * 0.35);
  const v = rdV;

  qgl.begin(GL_TRIANGLE_FAN);
  qgl.color3f(fr(light.color[0]! * 0.2), fr(light.color[1]! * 0.2), fr(light.color[2]! * 0.2));
  for (let i = 0; i < 3; i++) v[i] = light.origin[i]! - fr(r.vpn[i]! * rad);
  qgl.vertex3f(v[0]!, v[1]!, v[2]!);
  qgl.color3f(0, 0, 0);
  for (let i = 16; i >= 0; i--) {
    const a = fr((i / 16.0) * Math.PI * 2);
    for (let j = 0; j < 3; j++) {
      v[j] = light.origin[j]! + r.vright[j]! * Math.cos(a) * rad + r.vup[j]! * Math.sin(a) * rad;
    }
    qgl.vertex3f(v[0]!, v[1]!, v[2]!);
  }
  qgl.end();
}

// C: gl_light.c:77 R_RenderDlights
export function R_RenderDlights(r: GLState): void {
  if (!r.cv.gl_flashblend.value) return;
  const qgl = r.qgl;
  r.r_dlightframecount = r.r_framecount + 1; // because the count hasn't advanced yet for this frame
  if (!qgl) return;
  qgl.depthMask(0);
  qgl.disable(GL_TEXTURE_2D);
  qgl.enable(GL_BLEND);
  qgl.blendFunc(GL_ONE, GL_ONE);

  const rd = r.r_newrefdef;
  for (let i = 0; i < rd.num_dlights; i++) R_RenderDlight(r, rd.dlights[i]!);

  qgl.color3f(1, 1, 1);
  qgl.disable(GL_BLEND);
  qgl.enable(GL_TEXTURE_2D);
  qgl.blendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);
  qgl.depthMask(1);
}

// =============================================================================
// DYNAMIC LIGHTS

// C: gl_light.c:118 R_MarkLights
export function R_MarkLights(r: GLState, light: DLight, bit: number, node: MNode): void {
  if (node.contents !== -1) return;
  const splitplane = node.plane;
  const o = light.origin;
  const n = splitplane.normal;
  const dist = fr(fr(fr(fr(o[0]! * n[0]!) + fr(o[1]! * n[1]!)) + fr(o[2]! * n[2]!)) - splitplane.dist);

  if (dist > fr(light.intensity - DLIGHT_CUTOFF)) {
    R_MarkLights(r, light, bit, node.children[0]);
    return;
  }
  if (dist < fr(-light.intensity + DLIGHT_CUTOFF)) {
    R_MarkLights(r, light, bit, node.children[1]);
    return;
  }

  // mark the polygons
  const surfaces = r.r_worldmodel!.surfaces;
  for (let i = 0; i < node.numsurfaces; i++) {
    const surf = surfaces[node.firstsurface + i]!;
    if (surf.dlightframe !== r.r_dlightframecount) {
      surf.dlightbits = 0;
      surf.dlightframe = r.r_dlightframecount;
    }
    surf.dlightbits |= bit;
  }

  R_MarkLights(r, light, bit, node.children[0]);
  R_MarkLights(r, light, bit, node.children[1]);
}

// C: gl_light.c:164 R_PushDlights
export function R_PushDlights(r: GLState): void {
  if (r.cv.gl_flashblend.value) return;
  r.r_dlightframecount = r.r_framecount + 1; // because the count hasn't advanced yet for this frame
  const rd = r.r_newrefdef;
  for (let i = 0; i < rd.num_dlights; i++) R_MarkLights(r, rd.dlights[i]!, 1 << i, r.r_worldmodel!.nodes[0]!);
}

// =============================================================================
// LIGHT SAMPLING

/** C: RecursiveLightPoint mid points (one per recursion depth, the C locals). */
const rlpMid: Float32Array[] = [];
for (let i = 0; i < 1024; i++) rlpMid.push(new Float32Array(3));
const rlpScale = new Float32Array(3);

// C: gl_light.c:192 RecursiveLightPoint
export function RecursiveLightPoint(
  r: GLState,
  node: MNode,
  start: Float32Array,
  end: Float32Array,
  depth = 0,
): number {
  if (node.contents !== -1) return -1; // didn't hit anything

  // calculate mid point
  const plane = node.plane;
  const n = plane.normal;
  const front = fr(
    fr(fr(fr(start[0]! * n[0]!) + fr(start[1]! * n[1]!)) + fr(start[2]! * n[2]!)) - plane.dist,
  );
  const back = fr(fr(fr(fr(end[0]! * n[0]!) + fr(end[1]! * n[1]!)) + fr(end[2]! * n[2]!)) - plane.dist);
  const side = front < 0 ? 1 : 0;

  if ((back < 0 ? 1 : 0) === side) return RecursiveLightPoint(r, node.children[side]!, start, end, depth + 1);

  const frac = fr(front / fr(front - back));
  const mid = rlpMid[depth]!;
  mid[0] = fr(start[0]! + fr(fr(end[0]! - start[0]!) * frac));
  mid[1] = fr(start[1]! + fr(fr(end[1]! - start[1]!) * frac));
  mid[2] = fr(start[2]! + fr(fr(end[2]! - start[2]!) * frac));

  // go down front side
  const rr = RecursiveLightPoint(r, node.children[side]!, start, mid, depth + 1);
  if (rr >= 0) return rr; // hit something

  if ((back < 0 ? 1 : 0) === side) return -1; // didn't hit anuthing

  // check for impact on this node
  r.lightspot[0] = mid[0]!;
  r.lightspot[1] = mid[1]!;
  r.lightspot[2] = mid[2]!;
  r.lightplane = plane;

  const surfaces = r.r_worldmodel!.surfaces;
  const modulate = fr(r.cv.gl_modulate.value);
  const lightstyles = r.r_newrefdef.lightstyles;
  for (let i = 0; i < node.numsurfaces; i++) {
    const surf = surfaces[node.firstsurface + i]!;
    if (surf.flags & (SURF_DRAWTURB | SURF_DRAWSKY)) continue; // no lightmaps

    const tex = surf.texinfo.vecs;
    const s = Math.trunc(
      fr(fr(fr(fr(mid[0]! * tex[0]!) + fr(mid[1]! * tex[1]!)) + fr(mid[2]! * tex[2]!)) + tex[3]!),
    );
    const t = Math.trunc(
      fr(fr(fr(fr(mid[0]! * tex[4]!) + fr(mid[1]! * tex[5]!)) + fr(mid[2]! * tex[6]!)) + tex[7]!),
    );

    if (s < surf.texturemins[0]! || t < surf.texturemins[1]!) continue;

    let ds = s - surf.texturemins[0]!;
    let dt = t - surf.texturemins[1]!;

    if (ds > surf.extents[0]! || dt > surf.extents[1]!) continue;

    if (!surf.samples) return 0;

    ds >>= 4;
    dt >>= 4;

    const lightmap = surf.samples;
    const pc = r.pointcolor;
    pc[0] = pc[1] = pc[2] = 0;
    let lo = 3 * (dt * ((surf.extents[0]! >> 4) + 1) + ds);
    const scale = rlpScale;
    for (let maps = 0; maps < MAXLIGHTMAPS && surf.styles[maps] !== 255; maps++) {
      const rgb = lightstyles[surf.styles[maps]!]!.rgb;
      for (let k = 0; k < 3; k++) scale[k] = fr(modulate * rgb[k]!);
      pc[0] = pc[0]! + fr(lightmap[lo]! * scale[0]!) * (1.0 / 255);
      pc[1] = pc[1]! + fr(lightmap[lo + 1]! * scale[1]!) * (1.0 / 255);
      pc[2] = pc[2]! + fr(lightmap[lo + 2]! * scale[2]!) * (1.0 / 255);
      lo += 3 * ((surf.extents[0]! >> 4) + 1) * ((surf.extents[1]! >> 4) + 1);
    }
    return 1;
  }

  // go down back side
  return RecursiveLightPoint(r, node.children[side ? 0 : 1]!, mid, end, depth + 1);
}

const lpEnd = new Float32Array(3);
const lpStart = new Float32Array(3);
const lpDist = new Float32Array(3);

// C: gl_light.c:298 R_LightPoint
export function R_LightPoint(r: GLState, p: ArrayLike<number>, color: Float32Array): void {
  const wm = r.r_worldmodel!;
  if (!wm.lightdata) {
    color[0] = color[1] = color[2] = 1.0;
    return;
  }
  lpStart[0] = p[0]!;
  lpStart[1] = p[1]!;
  lpStart[2] = p[2]!;
  lpEnd[0] = p[0]!;
  lpEnd[1] = p[1]!;
  lpEnd[2] = p[2]! - 2048;

  const rr = RecursiveLightPoint(r, wm.nodes[0]!, lpStart, lpEnd);

  if (rr === -1) {
    color[0] = color[1] = color[2] = 0;
  } else {
    color[0] = r.pointcolor[0]!;
    color[1] = r.pointcolor[1]!;
    color[2] = r.pointcolor[2]!;
  }

  // add dynamic lights
  const rd = r.r_newrefdef;
  const eo = r.currententity.origin;
  for (let lnum = 0; lnum < rd.num_dlights; lnum++) {
    const dl = rd.dlights[lnum]!;
    lpDist[0] = eo[0]! - dl.origin[0]!;
    lpDist[1] = eo[1]! - dl.origin[1]!;
    lpDist[2] = eo[2]! - dl.origin[2]!;
    let add = fr(dl.intensity - VectorLength(lpDist));
    add = fr(add * (1.0 / 256));
    if (add > 0) {
      color[0] = color[0]! + fr(add * dl.color[0]!);
      color[1] = color[1]! + fr(add * dl.color[1]!);
      color[2] = color[2]! + fr(add * dl.color[2]!);
    }
  }

  const m = fr(r.cv.gl_modulate.value);
  color[0] = color[0]! * m;
  color[1] = color[1]! * m;
  color[2] = color[2]! * m;
}

// ===================================================================

/** Inputs of R_BuildLightMap / R_AddDynamicLights beyond the surface (C: globals). */
export interface LightMapContext {
  lightstyles: LightStyle[];
  dlights: DLight[];
  num_dlights: number;
  r_framecount: number;
  gl_modulate: number;
  /** gl_monolightmap->string[0] as a char code */
  monolightmap: number;
  /** float s_blocklights[34*34*3] */
  s_blocklights: Float32Array;
  sysError: (errLevel: number, msg: string) => never;
}

const adlImpact = new Float32Array(3);
const adlLocal = new Float32Array(2);

// C: gl_light.c:359 R_AddDynamicLights
export function R_AddDynamicLights(ctx: LightMapContext, surf: MSurface): void {
  const smax = (surf.extents[0]! >> 4) + 1;
  const tmax = (surf.extents[1]! >> 4) + 1;
  const tex = surf.texinfo.vecs;
  const bl = ctx.s_blocklights;
  const normal = surf.plane.normal;

  for (let lnum = 0; lnum < ctx.num_dlights; lnum++) {
    if (!(surf.dlightbits & (1 << lnum))) continue; // not lit by this light

    const dl = ctx.dlights[lnum]!;
    let frad = fr(dl.intensity);
    let fdist = fr(
      fr(
        fr(fr(dl.origin[0]! * normal[0]!) + fr(dl.origin[1]! * normal[1]!)) + fr(dl.origin[2]! * normal[2]!),
      ) - surf.plane.dist,
    );
    frad = fr(frad - Math.abs(fdist));
    // rad is now the highest intensity on the plane

    let fminlight = DLIGHT_CUTOFF; // FIXME: make configurable?
    if (frad < fminlight) continue;
    fminlight = fr(frad - fminlight);

    const impact = adlImpact;
    for (let i = 0; i < 3; i++) impact[i] = fr(dl.origin[i]! - fr(normal[i]! * fdist));

    const local = adlLocal;
    local[0] = fr(
      fr(fr(fr(fr(impact[0]! * tex[0]!) + fr(impact[1]! * tex[1]!)) + fr(impact[2]! * tex[2]!)) + tex[3]!) -
        surf.texturemins[0]!,
    );
    local[1] = fr(
      fr(fr(fr(fr(impact[0]! * tex[4]!) + fr(impact[1]! * tex[5]!)) + fr(impact[2]! * tex[6]!)) + tex[7]!) -
        surf.texturemins[1]!,
    );

    let pfBL = 0;
    let ftacc = 0;
    for (let t = 0; t < tmax; t++, ftacc = fr(ftacc + 16)) {
      let td = Math.trunc(fr(local[1]! - ftacc));
      if (td < 0) td = -td;
      let fsacc = 0;
      for (let s = 0; s < smax; s++, fsacc = fr(fsacc + 16), pfBL += 3) {
        let sd = Math.trunc(fr(local[0]! - fsacc));
        if (sd < 0) sd = -sd;
        if (sd > td) fdist = sd + (td >> 1);
        else fdist = td + (sd >> 1);
        if (fdist < fminlight) {
          const k = fr(frad - fdist);
          bl[pfBL] = bl[pfBL]! + fr(k * dl.color[0]!);
          bl[pfBL + 1] = bl[pfBL + 1]! + fr(k * dl.color[1]!);
          bl[pfBL + 2] = bl[pfBL + 2]! + fr(k * dl.color[2]!);
        }
      }
    }
  }
}

// C: gl_light.c:437 R_SetCacheState
export function R_SetCacheState(lightstyles: LightStyle[], surf: MSurface): void {
  for (let maps = 0; maps < MAXLIGHTMAPS && surf.styles[maps] !== 255; maps++) {
    surf.cached_light[maps] = lightstyles[surf.styles[maps]!]!.white;
  }
}

const blScale = new Float32Array(4);
const CH_0 = 0x30;
const CH_L = 0x4c;
const CH_I = 0x49;
const CH_C = 0x43;

/**
 * C: gl_light.c:455 R_BuildLightMap -- combine and scale multiple lightmaps into the floating format in
 * blocklights, add dynamic lights, then store as RGBA bytes at dest[destOfs] with `stride` bytes per row.
 */
export function R_BuildLightMap(
  ctx: LightMapContext,
  surf: MSurface,
  dest: Uint8Array,
  destOfs: number,
  stride: number,
): void {
  if (surf.texinfo.flags & (SURF_SKY | SURF_TRANS33 | SURF_TRANS66 | SURF_WARP)) {
    ctx.sysError(ERR_DROP, 'R_BuildLightMap called for non-lit surface');
  }
  const smax = (surf.extents[0]! >> 4) + 1;
  const tmax = (surf.extents[1]! >> 4) + 1;
  const size = smax * tmax;
  const s_blocklights = ctx.s_blocklights;
  if (size > (34 * 34 * 3 * 4) >> 4) ctx.sysError(ERR_DROP, 'Bad s_blocklights size');

  const scale = blScale;
  // set to full bright if no light data
  if (!surf.samples) {
    for (let i = 0; i < size * 3; i++) s_blocklights[i] = 255;
  } else {
    // count the # of maps
    let nummaps;
    for (nummaps = 0; nummaps < MAXLIGHTMAPS && surf.styles[nummaps] !== 255; nummaps++);

    const lightmap = surf.samples;
    let lo = 0;
    const modulate = fr(ctx.gl_modulate);

    // add all the lightmaps
    if (nummaps === 1) {
      for (let maps = 0; maps < MAXLIGHTMAPS && surf.styles[maps] !== 255; maps++) {
        const rgb = ctx.lightstyles[surf.styles[maps]!]!.rgb;
        for (let i = 0; i < 3; i++) scale[i] = fr(modulate * rgb[i]!);
        if (scale[0] === 1.0 && scale[1] === 1.0 && scale[2] === 1.0) {
          for (let i = 0, bl = 0; i < size; i++, bl += 3) {
            s_blocklights[bl] = lightmap[lo + i * 3]!;
            s_blocklights[bl + 1] = lightmap[lo + i * 3 + 1]!;
            s_blocklights[bl + 2] = lightmap[lo + i * 3 + 2]!;
          }
        } else {
          for (let i = 0, bl = 0; i < size; i++, bl += 3) {
            s_blocklights[bl] = lightmap[lo + i * 3]! * scale[0]!;
            s_blocklights[bl + 1] = lightmap[lo + i * 3 + 1]! * scale[1]!;
            s_blocklights[bl + 2] = lightmap[lo + i * 3 + 2]! * scale[2]!;
          }
        }
        lo += size * 3; // skip to next lightmap
      }
    } else {
      s_blocklights.fill(0, 0, size * 3);
      for (let maps = 0; maps < MAXLIGHTMAPS && surf.styles[maps] !== 255; maps++) {
        const rgb = ctx.lightstyles[surf.styles[maps]!]!.rgb;
        for (let i = 0; i < 3; i++) scale[i] = fr(modulate * rgb[i]!);
        if (scale[0] === 1.0 && scale[1] === 1.0 && scale[2] === 1.0) {
          for (let i = 0, bl = 0; i < size; i++, bl += 3) {
            s_blocklights[bl] = s_blocklights[bl]! + lightmap[lo + i * 3]!;
            s_blocklights[bl + 1] = s_blocklights[bl + 1]! + lightmap[lo + i * 3 + 1]!;
            s_blocklights[bl + 2] = s_blocklights[bl + 2]! + lightmap[lo + i * 3 + 2]!;
          }
        } else {
          for (let i = 0, bl = 0; i < size; i++, bl += 3) {
            s_blocklights[bl] = s_blocklights[bl]! + fr(lightmap[lo + i * 3]! * scale[0]!);
            s_blocklights[bl + 1] = s_blocklights[bl + 1]! + fr(lightmap[lo + i * 3 + 1]! * scale[1]!);
            s_blocklights[bl + 2] = s_blocklights[bl + 2]! + fr(lightmap[lo + i * 3 + 2]! * scale[2]!);
          }
        }
        lo += size * 3; // skip to next lightmap
      }
    }

    // add all the dynamic lights
    if (surf.dlightframe === ctx.r_framecount) R_AddDynamicLights(ctx, surf);
  }

  // put into texture format (store:)
  stride -= smax << 2;
  let bl = 0;
  let d = destOfs;
  const monolightmap = ctx.monolightmap;

  for (let i = 0; i < tmax; i++, d += stride) {
    for (let j = 0; j < smax; j++) {
      let r = Math.trunc(s_blocklights[bl]!);
      let g = Math.trunc(s_blocklights[bl + 1]!);
      let b = Math.trunc(s_blocklights[bl + 2]!);

      // catch negative lights
      if (r < 0) r = 0;
      if (g < 0) g = 0;
      if (b < 0) b = 0;

      // determine the brightest of the three color components
      let max = r > g ? r : g;
      if (b > max) max = b;

      // alpha is ONLY used for the mono lightmap case
      let a = max;

      // rescale all the color components if the intensity of the greatest channel exceeds 1.0
      if (max > 255) {
        const t = fr(255.0 / max);
        r = Math.trunc(fr(r * t));
        g = Math.trunc(fr(g * t));
        b = Math.trunc(fr(b * t));
        a = Math.trunc(fr(a * t));
      }

      if (monolightmap !== CH_0) {
        switch (monolightmap) {
          case CH_L:
          case CH_I:
            r = a;
            g = b = 0;
            break;
          case CH_C:
            // try faking colored lighting
            a = 255 - Math.trunc((r + g + b) / 3);
            r = Math.trunc(r * (a / 255.0));
            g = Math.trunc(g * (a / 255.0));
            b = Math.trunc(b * (a / 255.0));
            break;
          default:
            r = g = b = 0;
            a = 255 - a;
            break;
        }
      }

      dest[d] = r;
      dest[d + 1] = g;
      dest[d + 2] = b;
      dest[d + 3] = a;
      bl += 3;
      d += 4;
    }
  }
}

/** The LightMapContext of the renderer state (fields read at call time). */
export function lightMapContext(r: GLState): LightMapContext {
  const rd = r.r_newrefdef;
  return {
    get lightstyles() {
      return rd.lightstyles;
    },
    get dlights() {
      return rd.dlights;
    },
    get num_dlights() {
      return rd.num_dlights;
    },
    get r_framecount() {
      return r.r_framecount;
    },
    get gl_modulate() {
      return r.cv.gl_modulate.value;
    },
    get monolightmap() {
      const s = r.cv.gl_monolightmap.string;
      return s.length ? s.charCodeAt(0) : 0;
    },
    s_blocklights: r.s_blocklights,
    sysError: (lvl, msg) => r.ri.sysError(lvl, msg),
  };
}
