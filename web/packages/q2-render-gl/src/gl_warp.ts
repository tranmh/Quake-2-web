// Port of ref_gl/gl_warp.c: sky and water polygons.
// Water warp is computed on the CPU per vertex exactly like EmitWaterPolys (same 64 unit subdivision,
// same turbsin table and double precision index computation) and streamed through the immediate path.
import { ERR_DROP, SURF_FLOWING, VectorClear, VectorCopy, fr } from 'q2-shared';
import { GLState, it_sky } from './gl_local';
import { GLPoly, MSurface, SIDE_BACK, SIDE_FRONT, SIDE_ON, VERTEXSIZE } from './gl_model_h';
import { GL_Bind, GL_FindImage } from './gl_image';
import { GL_QUADS, GL_TRIANGLE_FAN } from './qgl';

export const SUBDIVIDE_SIZE = 64;

/**
 * Port: memory safety limits for SubdividePolygon on crafted maps. qbsp never emits warp faces larger
 * than a few hundred units (a handful of 64 unit pieces each); C would recurse forever on coordinates
 * beyond BoundPoly's +-9999 start values and cut huge faces into millions of polygons.
 */
export const MAX_WARP_POLYS = 1 << 18;
const MAX_SUBDIVIDE_DEPTH = 64;

export interface SubdivideBudget {
  polys: number;
}

// C: gl_warp.c:36 BoundPoly
function BoundPoly(numverts: number, verts: Float32Array, mins: Float32Array, maxs: Float32Array): void {
  mins[0] = mins[1] = mins[2] = 9999;
  maxs[0] = maxs[1] = maxs[2] = -9999;
  let v = 0;
  for (let i = 0; i < numverts; i++) {
    for (let j = 0; j < 3; j++, v++) {
      if (verts[v]! < mins[j]!) mins[j] = verts[v]!;
      if (verts[v]! > maxs[j]!) maxs[j] = verts[v]!;
    }
  }
}

/**
 * C: gl_warp.c:54 SubdividePolygon. `verts` holds numverts xyz triples and has room for one more (the
 * wrap-around copy C writes past the end). New polys are prepended to warpface.polys.
 */
export function SubdividePolygon(
  warpface: MSurface,
  numverts: number,
  verts: Float32Array,
  sysError: (m: string) => never,
  budget: SubdivideBudget = { polys: 0 },
  depth = 0,
): void {
  if (depth > MAX_SUBDIVIDE_DEPTH) sysError('SubdividePolygon: bad polygon bounds'); // port
  const mins = new Float32Array(3);
  const maxs = new Float32Array(3);
  const front = new Float32Array(64 * 3);
  const back = new Float32Array(64 * 3);
  const dist = new Float32Array(65);

  if (numverts > 60) sysError(`numverts = ${numverts}`);

  BoundPoly(numverts, verts, mins, maxs);

  for (let i = 0; i < 3; i++) {
    let m = fr((mins[i]! + maxs[i]!) * 0.5);
    m = fr(SUBDIVIDE_SIZE * Math.floor(fr(m / SUBDIVIDE_SIZE) + 0.5));
    if (fr(maxs[i]! - m) < 8) continue;
    if (fr(m - mins[i]!) < 8) continue;

    // cut it
    let j;
    for (j = 0; j < numverts; j++) dist[j] = verts[j * 3 + i]! - m;

    // wrap cases
    dist[j] = dist[0]!;
    verts[numverts * 3] = verts[0]!;
    verts[numverts * 3 + 1] = verts[1]!;
    verts[numverts * 3 + 2] = verts[2]!;

    let f = 0;
    let b = 0;
    for (j = 0; j < numverts; j++) {
      const v = j * 3;
      if (dist[j]! >= 0) {
        front[f * 3] = verts[v]!;
        front[f * 3 + 1] = verts[v + 1]!;
        front[f * 3 + 2] = verts[v + 2]!;
        f++;
      }
      if (dist[j]! <= 0) {
        back[b * 3] = verts[v]!;
        back[b * 3 + 1] = verts[v + 1]!;
        back[b * 3 + 2] = verts[v + 2]!;
        b++;
      }
      if (dist[j] === 0 || dist[j + 1] === 0) continue;
      if (dist[j]! > 0 !== dist[j + 1]! > 0) {
        // clip point
        const frac = fr(dist[j]! / fr(dist[j]! - dist[j + 1]!));
        for (let k = 0; k < 3; k++) {
          front[f * 3 + k] = back[b * 3 + k] = fr(
            verts[v + k]! + fr(frac * fr(verts[v + 3 + k]! - verts[v + k]!)),
          );
        }
        f++;
        b++;
      }
    }

    SubdividePolygon(warpface, f, front, sysError, budget, depth + 1);
    SubdividePolygon(warpface, b, back, sysError, budget, depth + 1);
    return;
  }

  // add a point in the center to help keep warp valid
  if (++budget.polys > MAX_WARP_POLYS) sysError('SubdividePolygon: too many warp polygons'); // port
  const poly = new GLPoly(numverts + 2);
  poly.next = warpface.polys;
  warpface.polys = poly;
  const total = new Float32Array(3);
  VectorClear(total);
  let total_s = 0;
  let total_t = 0;
  const vecs = warpface.texinfo.vecs;
  const pv = poly.verts;
  let i;
  for (i = 0; i < numverts; i++) {
    const x = verts[i * 3]!;
    const y = verts[i * 3 + 1]!;
    const z = verts[i * 3 + 2]!;
    const o = (i + 1) * VERTEXSIZE;
    pv[o] = x;
    pv[o + 1] = y;
    pv[o + 2] = z;
    const s = fr(fr(fr(x * vecs[0]!) + fr(y * vecs[1]!)) + fr(z * vecs[2]!));
    const t = fr(fr(fr(x * vecs[4]!) + fr(y * vecs[5]!)) + fr(z * vecs[6]!));
    total_s = fr(total_s + s);
    total_t = fr(total_t + t);
    total[0] = total[0]! + x;
    total[1] = total[1]! + y;
    total[2] = total[2]! + z;
    pv[o + 3] = s;
    pv[o + 4] = t;
  }

  const scale = fr(1.0 / numverts);
  pv[0] = total[0]! * scale;
  pv[1] = total[1]! * scale;
  pv[2] = total[2]! * scale;
  pv[3] = fr(total_s / numverts);
  pv[4] = fr(total_t / numverts);

  // copy first vertex to last
  pv.copyWithin((i + 1) * VERTEXSIZE, VERTEXSIZE, 2 * VERTEXSIZE);
}

// C: gl_warp.c:164 GL_SubdivideSurface -- breaks a polygon up along axial 64 unit boundaries
export function GL_SubdivideSurface(r: GLState, fa: MSurface): void {
  const lm = r.loadmodel;
  const verts = new Float32Array(65 * 3);
  r.warpface = fa;

  // convert edges back to a normal polygon
  let numverts = 0;
  for (let i = 0; i < fa.numedges; i++) {
    const lindex = lm.surfedges[fa.firstedge + i]!;
    const vi = lindex > 0 ? lm.edges[lindex * 2]! : lm.edges[-lindex * 2 + 1]!;
    if (numverts >= 64) r.ri.sysError(ERR_DROP, `numverts = ${fa.numedges}`); // port: C overflows verts[64]
    verts[numverts * 3] = lm.vertexes[vi * 3]!;
    verts[numverts * 3 + 1] = lm.vertexes[vi * 3 + 1]!;
    verts[numverts * 3 + 2] = lm.vertexes[vi * 3 + 2]!;
    numverts++;
  }
  const budget = { polys: r.warpPolys };
  SubdividePolygon(fa, numverts, verts, (m) => r.ri.sysError(ERR_DROP, m), budget);
  r.warpPolys = budget.polys;
}

// =========================================================

/** C: gl_warp.c:202 TURBSCALE (256.0 / (2 * M_PI)) */
export const TURBSCALE = 256.0 / (2 * Math.PI);

/**
 * C: gl_warp.c:211 EmitWaterPolys, the texture coordinate part: warped (s, t) of a warp vertex.
 * Same expression as the C (!id386) build: the table index is computed in double and truncated.
 */
export function warpST(
  os: number,
  ot: number,
  time: number,
  scroll: number,
  turbsin: Float32Array,
  out: Float32Array,
): void {
  let s = fr(os + turbsin[Math.trunc((ot * 0.125 + time) * TURBSCALE) & 255]!);
  s = fr(s + scroll);
  s = fr(s * (1.0 / 64));
  let t = fr(ot + turbsin[Math.trunc((os * 0.125 + time) * TURBSCALE) & 255]!);
  t = fr(t * (1.0 / 64));
  out[0] = s;
  out[1] = t;
}

const warpTmp = new Float32Array(2);

// C: gl_warp.c:211 EmitWaterPolys -- does a water warp on the pre-fragmented glpoly_t chain
export function EmitWaterPolys(r: GLState, fa: MSurface): void {
  const qgl = r.qgl;
  const time = fr(r.r_newrefdef.time);
  let scroll = 0;
  if (fa.texinfo.flags & SURF_FLOWING) scroll = fr(-64 * (time * 0.5 - Math.trunc(time * 0.5)));
  if (!qgl) return;
  for (let bp = fa.polys; bp; bp = bp.next) {
    const p = bp;
    const v = p.verts;
    qgl.begin(GL_TRIANGLE_FAN);
    for (let i = 0, o = 0; i < p.numverts; i++, o += VERTEXSIZE) {
      warpST(v[o + 3]!, v[o + 4]!, time, scroll, r.r_turbsin, warpTmp);
      qgl.texCoord2f(warpTmp[0]!, warpTmp[1]!);
      qgl.vertex3f(v[o]!, v[o + 1]!, v[o + 2]!);
    }
    qgl.end();
  }
}

// ===================================================================

// C: gl_warp.c:260 skyclip
const skyclip = [
  new Float32Array([1, 1, 0]),
  new Float32Array([1, -1, 0]),
  new Float32Array([0, -1, 1]),
  new Float32Array([0, 1, 1]),
  new Float32Array([1, 0, 1]),
  new Float32Array([-1, 0, 1]),
];

// C: gl_warp.c:271 st_to_vec -- 1 = s, 2 = t, 3 = 2048
const st_to_vec = [
  [3, -1, 2],
  [-3, 1, 2],
  [1, 3, 2],
  [-1, -3, 2],
  [-2, -1, 3], // 0 degrees yaw, look straight up
  [2, -1, -3], // look straight down
];

// C: gl_warp.c:287 vec_to_st -- s = [0]/[2], t = [1]/[2]
const vec_to_st = [
  [-2, 3, 1],
  [2, 3, -1],
  [1, 3, 2],
  [-1, 3, -2],
  [-2, -1, 3],
  [-2, 1, -3],
];

/** Sky bounds accumulated by R_AddSkySurface: skymins/skymaxs[2][6] (row-major). */
export interface SkyBounds {
  skymins: Float32Array;
  skymaxs: Float32Array;
}

const dsV = new Float32Array(3);
const dsAv = new Float32Array(3);

// C: gl_warp.c:305 DrawSkyPolygon
function DrawSkyPolygon(sb: SkyBounds, nump: number, vecs: Float32Array): void {
  // decide which face it maps to
  const v = dsV;
  v[0] = v[1] = v[2] = 0;
  for (let i = 0; i < nump; i++) {
    v[0] = vecs[i * 3]! + v[0]!;
    v[1] = vecs[i * 3 + 1]! + v[1]!;
    v[2] = vecs[i * 3 + 2]! + v[2]!;
  }
  const av = dsAv;
  av[0] = Math.abs(v[0]!);
  av[1] = Math.abs(v[1]!);
  av[2] = Math.abs(v[2]!);
  let axis;
  if (av[0]! > av[1]! && av[0]! > av[2]!) axis = v[0]! < 0 ? 1 : 0;
  else if (av[1]! > av[2]! && av[1]! > av[0]!) axis = v[1]! < 0 ? 3 : 2;
  else axis = v[2]! < 0 ? 5 : 4;

  const vts = vec_to_st[axis]!;
  // project new texture coords
  for (let i = 0; i < nump; i++) {
    const o = i * 3;
    let j = vts[2]!;
    const dv = j > 0 ? vecs[o + j - 1]! : -vecs[o - j - 1]!;
    if (dv < 0.001) continue; // don't divide by zero
    j = vts[0]!;
    const s = j < 0 ? fr(-vecs[o - j - 1]! / dv) : fr(vecs[o + j - 1]! / dv);
    j = vts[1]!;
    const t = j < 0 ? fr(-vecs[o - j - 1]! / dv) : fr(vecs[o + j - 1]! / dv);

    if (s < sb.skymins[axis]!) sb.skymins[axis] = s;
    if (t < sb.skymins[6 + axis]!) sb.skymins[6 + axis] = t;
    if (s > sb.skymaxs[axis]!) sb.skymaxs[axis] = s;
    if (t > sb.skymaxs[6 + axis]!) sb.skymaxs[6 + axis] = t;
  }
}

const ON_EPSILON = 0.1; // point on plane side epsilon
const MAX_CLIP_VERTS = 64;

// preallocated per-recursion-stage buffers for ClipSkyPolygon (C: locals)
const clipDists: Float32Array[] = [];
const clipSides: Int32Array[] = [];
const clipNewv: Float32Array[][] = [];
for (let s = 0; s < 7; s++) {
  clipDists.push(new Float32Array(MAX_CLIP_VERTS));
  clipSides.push(new Int32Array(MAX_CLIP_VERTS));
  clipNewv.push([new Float32Array(MAX_CLIP_VERTS * 3), new Float32Array(MAX_CLIP_VERTS * 3)]);
}

/**
 * C: gl_warp.c:389 ClipSkyPolygon. `vecs` holds nump xyz triples relative to the view origin and must have
 * room for one extra vertex (C writes the wrap-around copy past the end).
 */
export function ClipSkyPolygon(
  sb: SkyBounds,
  nump: number,
  vecs: Float32Array,
  stage: number,
  sysError: (m: string) => never,
): void {
  if (nump > MAX_CLIP_VERTS - 2) sysError('ClipSkyPolygon: MAX_CLIP_VERTS');
  if (stage === 6) {
    // fully clipped, so draw it
    DrawSkyPolygon(sb, nump, vecs);
    return;
  }

  let front = false;
  let back = false;
  const norm = skyclip[stage]!;
  const dists = clipDists[stage]!;
  const sides = clipSides[stage]!;
  let i;
  for (i = 0; i < nump; i++) {
    const o = i * 3;
    const d = fr(fr(fr(vecs[o]! * norm[0]!) + fr(vecs[o + 1]! * norm[1]!)) + fr(vecs[o + 2]! * norm[2]!));
    if (d > ON_EPSILON) {
      front = true;
      sides[i] = SIDE_FRONT;
    } else if (d < -ON_EPSILON) {
      back = true;
      sides[i] = SIDE_BACK;
    } else sides[i] = SIDE_ON;
    dists[i] = d;
  }

  if (!front || !back) {
    // not clipped
    ClipSkyPolygon(sb, nump, vecs, stage + 1, sysError);
    return;
  }

  // clip it
  sides[i] = sides[0]!;
  dists[i] = dists[0]!;
  vecs[i * 3] = vecs[0]!;
  vecs[i * 3 + 1] = vecs[1]!;
  vecs[i * 3 + 2] = vecs[2]!;
  const nv0 = clipNewv[stage]![0]!;
  const nv1 = clipNewv[stage]![1]!;
  let newc0 = 0;
  let newc1 = 0;

  for (i = 0; i < nump; i++) {
    const o = i * 3;
    switch (sides[i]) {
      case SIDE_FRONT:
        nv0[newc0 * 3] = vecs[o]!;
        nv0[newc0 * 3 + 1] = vecs[o + 1]!;
        nv0[newc0 * 3 + 2] = vecs[o + 2]!;
        newc0++;
        break;
      case SIDE_BACK:
        nv1[newc1 * 3] = vecs[o]!;
        nv1[newc1 * 3 + 1] = vecs[o + 1]!;
        nv1[newc1 * 3 + 2] = vecs[o + 2]!;
        newc1++;
        break;
      case SIDE_ON:
        nv0[newc0 * 3] = vecs[o]!;
        nv0[newc0 * 3 + 1] = vecs[o + 1]!;
        nv0[newc0 * 3 + 2] = vecs[o + 2]!;
        newc0++;
        nv1[newc1 * 3] = vecs[o]!;
        nv1[newc1 * 3 + 1] = vecs[o + 1]!;
        nv1[newc1 * 3 + 2] = vecs[o + 2]!;
        newc1++;
        break;
    }

    if (sides[i] === SIDE_ON || sides[i + 1] === SIDE_ON || sides[i + 1] === sides[i]) continue;

    const d = fr(dists[i]! / fr(dists[i]! - dists[i + 1]!));
    for (let j = 0; j < 3; j++) {
      const e = fr(vecs[o + j]! + fr(d * fr(vecs[o + 3 + j]! - vecs[o + j]!)));
      nv0[newc0 * 3 + j] = e;
      nv1[newc1 * 3 + j] = e;
    }
    newc0++;
    newc1++;
  }

  // continue
  ClipSkyPolygon(sb, newc0, nv0, stage + 1, sysError);
  ClipSkyPolygon(sb, newc1, nv1, stage + 1, sysError);
}

const addSkyVerts = new Float32Array(MAX_CLIP_VERTS * 3);

// C: gl_warp.c:485 R_AddSkySurface
export function R_AddSkySurface(r: GLState, fa: MSurface): void {
  // calculate vertex values for sky box
  const o = r.r_origin;
  for (let p: GLPoly | null = fa.polys; p; p = p.next) {
    const pv = p.verts;
    if (p.numverts > MAX_CLIP_VERTS - 2) r.ri.sysError(ERR_DROP, 'ClipSkyPolygon: MAX_CLIP_VERTS');
    for (let i = 0; i < p.numverts; i++) {
      addSkyVerts[i * 3] = pv[i * VERTEXSIZE]! - o[0]!;
      addSkyVerts[i * 3 + 1] = pv[i * VERTEXSIZE + 1]! - o[1]!;
      addSkyVerts[i * 3 + 2] = pv[i * VERTEXSIZE + 2]! - o[2]!;
    }
    ClipSkyPolygon(r, p.numverts, addSkyVerts, 0, (m) => r.ri.sysError(ERR_DROP, m));
  }
}

// C: gl_warp.c:508 R_ClearSkyBox
export function R_ClearSkyBox(r: GLState): void {
  for (let i = 0; i < 6; i++) {
    r.skymins[i] = r.skymins[6 + i] = 9999;
    r.skymaxs[i] = r.skymaxs[6 + i] = -9999;
  }
}

const msV = new Float32Array(3);
const msB = new Float32Array(3);

/** C: gl_warp.c:520 MakeSkyVec, returning (x, y, z, s, t) in `out` instead of emitting GL calls. */
export function MakeSkyVecST(
  s: number,
  t: number,
  axis: number,
  sky_min: number,
  sky_max: number,
  out: Float32Array,
): void {
  const b = msB;
  const v = msV;
  b[0] = s * 2300;
  b[1] = t * 2300;
  b[2] = 2300;
  const stv = st_to_vec[axis]!;
  for (let j = 0; j < 3; j++) {
    const k = stv[j]!;
    v[j] = k < 0 ? -b[-k - 1]! : b[k - 1]!;
  }

  // avoid bilerp seam
  s = fr((s + 1) * 0.5);
  t = fr((t + 1) * 0.5);

  if (s < sky_min) s = sky_min;
  else if (s > sky_max) s = sky_max;
  if (t < sky_min) t = sky_min;
  else if (t > sky_max) t = sky_max;

  t = fr(1.0 - t);
  out[0] = v[0]!;
  out[1] = v[1]!;
  out[2] = v[2]!;
  out[3] = s;
  out[4] = t;
}

const skyTmp = new Float32Array(5);

// C: gl_warp.c:520 MakeSkyVec
function MakeSkyVec(r: GLState, s: number, t: number, axis: number): void {
  MakeSkyVecST(s, t, axis, r.sky_min, r.sky_max, skyTmp);
  const qgl = r.qgl!;
  qgl.texCoord2f(skyTmp[3]!, skyTmp[4]!);
  qgl.vertex3f(skyTmp[0]!, skyTmp[1]!, skyTmp[2]!);
}

// C: gl_warp.c:561 skytexorder
const skytexorder = [0, 2, 1, 3, 4, 5];

// C: gl_warp.c:562 R_DrawSkyBox
export function R_DrawSkyBox(r: GLState): void {
  const qgl = r.qgl;
  if (r.skyrotate) {
    // check for no sky at all
    let i;
    for (i = 0; i < 6; i++) {
      if (r.skymins[i]! < r.skymaxs[i]! && r.skymins[6 + i]! < r.skymaxs[6 + i]!) break;
    }
    if (i === 6) return; // nothing visible
  }
  if (!qgl) return;

  qgl.pushMatrix();
  qgl.translatef(r.r_origin[0]!, r.r_origin[1]!, r.r_origin[2]!);
  qgl.rotatef(fr(r.r_newrefdef.time * r.skyrotate), r.skyaxis[0]!, r.skyaxis[1]!, r.skyaxis[2]!);

  for (let i = 0; i < 6; i++) {
    if (r.skyrotate) {
      // hack, forces full sky to draw when rotating
      r.skymins[i] = -1;
      r.skymins[6 + i] = -1;
      r.skymaxs[i] = 1;
      r.skymaxs[6 + i] = 1;
    }
    if (r.skymins[i]! >= r.skymaxs[i]! || r.skymins[6 + i]! >= r.skymaxs[6 + i]!) continue;

    GL_Bind(r, r.sky_images[skytexorder[i]!]!.texnum);

    qgl.begin(GL_QUADS);
    MakeSkyVec(r, r.skymins[i]!, r.skymins[6 + i]!, i);
    MakeSkyVec(r, r.skymins[i]!, r.skymaxs[6 + i]!, i);
    MakeSkyVec(r, r.skymaxs[i]!, r.skymaxs[6 + i]!, i);
    MakeSkyVec(r, r.skymaxs[i]!, r.skymins[6 + i]!, i);
    qgl.end();
  }
  qgl.popMatrix();
}

// C: gl_warp.c:625 suf -- 3dstudio environment map names
export const SKY_SUFFIXES = ['rt', 'bk', 'lf', 'ft', 'up', 'dn'];

/** Files R_SetSky reads (qglColorTableEXT is NULL, so the .tga names). */
export function skyImageNames(name: string): string[] {
  return SKY_SUFFIXES.map((s) => `env/${name.slice(0, 63)}${s}.tga`.slice(0, 63));
}

// C: gl_warp.c:626 R_SetSky
export function R_SetSky(r: GLState, name: string, rotate: number, axis: ArrayLike<number>): void {
  r.skyname = name.slice(0, 63);
  r.skyrotate = fr(rotate);
  VectorCopy(axis, r.skyaxis);
  const names = skyImageNames(r.skyname);

  for (let i = 0; i < 6; i++) {
    // chop down rotating skies for less memory
    if (r.cv.gl_skymip.value || r.skyrotate) r.cv.gl_picmip.value++;

    r.sky_images[i] = GL_FindImage(r, names[i]!, it_sky) ?? r.r_notexture;

    if (r.cv.gl_skymip.value || r.skyrotate) {
      // take less memory
      r.cv.gl_picmip.value--;
      r.sky_min = fr(1.0 / 256);
      r.sky_max = fr(255.0 / 256);
    } else {
      r.sky_min = fr(1.0 / 512);
      r.sky_max = fr(511.0 / 512);
    }
  }
}
