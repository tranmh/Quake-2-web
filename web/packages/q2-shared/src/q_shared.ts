// Port of game/q_shared.c (math library part). Every float store is rounded with Math.fround;
// double-typed intermediates (M_PI, double literals, libm) are computed in double first.
import { cInt, fr } from './cmath';
import { PITCH, ROLL, YAW } from './generated/const';
import type { CPlane, Vec3 } from './types';

// C: q_shared.h M_PI 3.14159265358979323846 (the nearest double is Math.PI)
export const M_PI = Math.PI;

/** C: q_shared.c vec3_origin (treat as read-only) */
export const vec3_origin: Vec3 = new Float32Array(3);

// C: q_shared.h DotProduct macro (float arithmetic, each op rounded)
export function DotProduct(x: ArrayLike<number>, y: ArrayLike<number>): number {
  return fr(fr(fr(x[0]! * y[0]!) + fr(x[1]! * y[1]!)) + fr(x[2]! * y[2]!));
}

// C: q_shared.h VectorSubtract
export function VectorSubtract(a: ArrayLike<number>, b: ArrayLike<number>, c: Vec3): void {
  c[0] = a[0]! - b[0]!;
  c[1] = a[1]! - b[1]!;
  c[2] = a[2]! - b[2]!;
}

// C: q_shared.h VectorAdd
export function VectorAdd(a: ArrayLike<number>, b: ArrayLike<number>, c: Vec3): void {
  c[0] = a[0]! + b[0]!;
  c[1] = a[1]! + b[1]!;
  c[2] = a[2]! + b[2]!;
}

// C: q_shared.h VectorCopy
export function VectorCopy(a: ArrayLike<number>, b: Vec3): void {
  b[0] = a[0]!;
  b[1] = a[1]!;
  b[2] = a[2]!;
}

// C: q_shared.h VectorClear
export function VectorClear(a: Vec3): void {
  a[0] = a[1] = a[2] = 0;
}

// C: q_shared.h VectorNegate
export function VectorNegate(a: ArrayLike<number>, b: Vec3): void {
  b[0] = -a[0]!;
  b[1] = -a[1]!;
  b[2] = -a[2]!;
}

// C: q_shared.h VectorSet
export function VectorSet(v: Vec3, x: number, y: number, z: number): void {
  v[0] = x;
  v[1] = y;
  v[2] = z;
}

// C: q_shared.c:760 VectorMA
export function VectorMA(veca: ArrayLike<number>, scale: number, vecb: ArrayLike<number>, vecc: Vec3): void {
  scale = fr(scale);
  vecc[0] = veca[0]! + fr(scale * vecb[0]!);
  vecc[1] = veca[1]! + fr(scale * vecb[1]!);
  vecc[2] = veca[2]! + fr(scale * vecb[2]!);
}

// C: q_shared.c:792 CrossProduct
export function CrossProduct(v1: ArrayLike<number>, v2: ArrayLike<number>, cross: Vec3): void {
  cross[0] = fr(v1[1]! * v2[2]!) - fr(v1[2]! * v2[1]!);
  cross[1] = fr(v1[2]! * v2[0]!) - fr(v1[0]! * v2[2]!);
  cross[2] = fr(v1[0]! * v2[1]!) - fr(v1[1]! * v2[0]!);
}

// C: q_shared.c:801 VectorLength
export function VectorLength(v: ArrayLike<number>): number {
  let length = 0;
  for (let i = 0; i < 3; i++) length = fr(length + fr(v[i]! * v[i]!));
  return fr(Math.sqrt(length));
}

// C: q_shared.c:813 VectorInverse
export function VectorInverse(v: Vec3): void {
  v[0] = -v[0]!;
  v[1] = -v[1]!;
  v[2] = -v[2]!;
}

// C: q_shared.c:820 VectorScale
export function VectorScale(inp: ArrayLike<number>, scale: number, out: Vec3): void {
  scale = fr(scale);
  out[0] = inp[0]! * scale;
  out[1] = inp[1]! * scale;
  out[2] = inp[2]! * scale;
}

// C: q_shared.c:731 VectorCompare
export function VectorCompare(v1: ArrayLike<number>, v2: ArrayLike<number>): number {
  if (v1[0] !== v2[0] || v1[1] !== v2[1] || v1[2] !== v2[2]) return 0;
  return 1;
}

// C: q_shared.c:740 VectorNormalize
export function VectorNormalize(v: Vec3): number {
  let length = DotProduct(v, v);
  length = fr(Math.sqrt(length));
  if (length) {
    const ilength = fr(1 / length);
    v[0] = v[0]! * ilength;
    v[1] = v[1]! * ilength;
    v[2] = v[2]! * ilength;
  }
  return length;
}

// C: q_shared.c:700 VectorNormalize2
export function VectorNormalize2(v: ArrayLike<number>, out: Vec3): number {
  let length = DotProduct(v, v);
  length = fr(Math.sqrt(length));
  if (length) {
    const ilength = fr(1 / length);
    out[0] = v[0]! * ilength;
    out[1] = v[1]! * ilength;
    out[2] = v[2]! * ilength;
  }
  return length;
}

// C: q_shared.c:828 Q_log2
export function Q_log2(val: number): number {
  let answer = 0;
  while ((val >>= 1)) answer++;
  return answer;
}

// C: q_shared.c:88 AngleVectors
// angle = angles[i] * (M_PI*2/360) is computed in double and narrowed to float; sin/cos take the float
// promoted to double and are narrowed when stored in the float statics.
export function AngleVectors(
  angles: ArrayLike<number>,
  forward: Vec3 | null,
  right: Vec3 | null,
  up: Vec3 | null,
): void {
  let angle = fr(angles[YAW]! * ((M_PI * 2) / 360));
  const sy = fr(Math.sin(angle));
  const cy = fr(Math.cos(angle));
  angle = fr(angles[PITCH]! * ((M_PI * 2) / 360));
  const sp = fr(Math.sin(angle));
  const cp = fr(Math.cos(angle));
  angle = fr(angles[ROLL]! * ((M_PI * 2) / 360));
  const sr = fr(Math.sin(angle));
  const cr = fr(Math.cos(angle));

  if (forward) {
    forward[0] = cp * cy;
    forward[1] = cp * sy;
    forward[2] = -sp;
  }
  if (right) {
    // (-1*sr*sp*cy+-1*cr*-sy)
    right[0] = fr(fr(-sr * sp) * cy) + fr(-cr * -sy);
    right[1] = fr(fr(-sr * sp) * sy) + fr(-cr * cy);
    right[2] = -sr * cp;
  }
  if (up) {
    // (cr*sp*cy+-sr*-sy)
    up[0] = fr(fr(cr * sp) * cy) + fr(-sr * -sy);
    up[1] = fr(fr(cr * sp) * sy) + fr(-sr * cy);
    up[2] = cr * cp;
  }
}

// C: q_shared.c:121 ProjectPointOnPlane
export function ProjectPointOnPlane(dst: Vec3, p: ArrayLike<number>, normal: ArrayLike<number>): void {
  const inv_denom = fr(1.0 / DotProduct(normal, normal));
  const d = fr(DotProduct(normal, p) * inv_denom);
  const n0 = fr(normal[0]! * inv_denom);
  const n1 = fr(normal[1]! * inv_denom);
  const n2 = fr(normal[2]! * inv_denom);
  dst[0] = p[0]! - fr(d * n0);
  dst[1] = p[1]! - fr(d * n1);
  dst[2] = p[2]! - fr(d * n2);
}

const pv_tempvec = new Float32Array(3);

// C: q_shared.c:144 PerpendicularVector (assumes src is normalized)
export function PerpendicularVector(dst: Vec3, src: ArrayLike<number>): void {
  let pos = 0;
  let minelem = 1.0;
  for (let i = 0; i < 3; i++) {
    if (Math.abs(src[i]!) < minelem) {
      pos = i;
      minelem = fr(Math.abs(src[i]!));
    }
  }
  const tempvec = pv_tempvec;
  tempvec[0] = tempvec[1] = tempvec[2] = 0;
  tempvec[pos] = 1;
  ProjectPointOnPlane(dst, tempvec, src);
  VectorNormalize(dst);
}

/** 3x3 float matrix stored row-major in a Float32Array(9). */
export type Mat3 = Float32Array;

function m3(m: Mat3, r: number, c: number): number {
  return m[r * 3 + c]!;
}

// C: q_shared.c:183 R_ConcatRotations
export function R_ConcatRotations(in1: Mat3, in2: Mat3, out: Mat3): void {
  for (let r = 0; r < 3; r++) {
    for (let c = 0; c < 3; c++) {
      out[r * 3 + c] =
        fr(fr(m3(in1, r, 0) * m3(in2, 0, c)) + fr(m3(in1, r, 1) * m3(in2, 1, c))) +
        fr(m3(in1, r, 2) * m3(in2, 2, c));
    }
  }
}

/** 3x4 float matrix stored row-major in a Float32Array(12). */
export type Mat34 = Float32Array;

// C: q_shared.c:211 R_ConcatTransforms
export function R_ConcatTransforms(in1: Mat34, in2: Mat34, out: Mat34): void {
  const a = (r: number, c: number) => in1[r * 4 + c]!;
  const b = (r: number, c: number) => in2[r * 4 + c]!;
  for (let r = 0; r < 3; r++) {
    for (let c = 0; c < 4; c++) {
      let v = fr(fr(fr(a(r, 0) * b(0, c)) + fr(a(r, 1) * b(1, c))) + fr(a(r, 2) * b(2, c)));
      if (c === 3) v = fr(v + a(r, 3));
      out[r * 4 + c] = v;
    }
  }
}

const rp_m = new Float32Array(9);
const rp_im = new Float32Array(9);
const rp_zrot = new Float32Array(9);
const rp_tmpmat = new Float32Array(9);
const rp_rot = new Float32Array(9);
const rp_vr = new Float32Array(3);
const rp_vup = new Float32Array(3);
const rp_vf = new Float32Array(3);

// C: q_shared.c:31 RotatePointAroundVector
export function RotatePointAroundVector(
  dst: Vec3,
  dir: ArrayLike<number>,
  point: ArrayLike<number>,
  degrees: number,
): void {
  degrees = fr(degrees);
  const m = rp_m,
    im = rp_im,
    zrot = rp_zrot,
    vr = rp_vr,
    vup = rp_vup,
    vf = rp_vf;
  vf[0] = dir[0]!;
  vf[1] = dir[1]!;
  vf[2] = dir[2]!;

  PerpendicularVector(vr, dir);
  CrossProduct(vr, vf, vup);

  m[0] = vr[0]!;
  m[3] = vr[1]!;
  m[6] = vr[2]!;
  m[1] = vup[0]!;
  m[4] = vup[1]!;
  m[7] = vup[2]!;
  m[2] = vf[0]!;
  m[5] = vf[1]!;
  m[8] = vf[2]!;

  im.set(m);
  im[1] = m[3]!;
  im[2] = m[6]!;
  im[3] = m[1]!;
  im[5] = m[7]!;
  im[6] = m[2]!;
  im[7] = m[5]!;

  zrot.fill(0);
  zrot[0] = zrot[4] = zrot[8] = 1;
  // DEG2RAD(a) = (a * M_PI) / 180.0F, evaluated in double
  const rad = (degrees * M_PI) / 180.0;
  zrot[0] = Math.cos(rad);
  zrot[1] = Math.sin(rad);
  zrot[3] = -Math.sin(rad);
  zrot[4] = Math.cos(rad);

  R_ConcatRotations(m, zrot, rp_tmpmat);
  R_ConcatRotations(rp_tmpmat, im, rp_rot);

  const rot = rp_rot;
  for (let i = 0; i < 3; i++) {
    dst[i] =
      fr(fr(rot[i * 3]! * point[0]!) + fr(rot[i * 3 + 1]! * point[1]!)) + fr(rot[i * 3 + 2]! * point[2]!);
  }
}

// C: q_shared.c:288 Q_fabs
export function Q_fabs(f: number): number {
  return Math.abs(fr(f));
}

// C: q_shared.c:310 LerpAngle
export function LerpAngle(a2: number, a1: number, frac: number): number {
  a2 = fr(a2);
  a1 = fr(a1);
  frac = fr(frac);
  if (fr(a1 - a2) > 180) a1 = fr(a1 - 360);
  if (fr(a1 - a2) < -180) a1 = fr(a1 + 360);
  return fr(a2 + fr(frac * fr(a1 - a2)));
}

// C: q_shared.c:320 anglemod
export function anglemod(a: number): number {
  return fr((360.0 / 65536) * (cInt(fr(a) * (65536 / 360.0)) & 65535));
}

// C: q_shared.c:337 BoxOnPlaneSide2 (slow, general version)
export function BoxOnPlaneSide2(emins: ArrayLike<number>, emaxs: ArrayLike<number>, p: CPlane): number {
  const c0 = new Float32Array(3);
  const c1 = new Float32Array(3);
  for (let i = 0; i < 3; i++) {
    if (p.normal[i]! < 0) {
      c0[i] = emins[i]!;
      c1[i] = emaxs[i]!;
    } else {
      c1[i] = emins[i]!;
      c0[i] = emaxs[i]!;
    }
  }
  const dist1 = fr(DotProduct(p.normal, c0) - p.dist);
  const dist2 = fr(DotProduct(p.normal, c1) - p.dist);
  let sides = 0;
  if (dist1 >= 0) sides = 1;
  if (dist2 < 0) sides |= 2;
  return sides;
}

function dot3(n: Float32Array, a: ArrayLike<number>, b: ArrayLike<number>, c: ArrayLike<number>): number {
  return fr(fr(fr(n[0]! * a[0]!) + fr(n[1]! * b[1]!)) + fr(n[2]! * c[2]!));
}

// C: q_shared.c:374 BoxOnPlaneSide (the portable C version, used on linux)
export function BoxOnPlaneSide(emins: ArrayLike<number>, emaxs: ArrayLike<number>, p: CPlane): number {
  if (p.type < 3) {
    if (p.dist <= emins[p.type]!) return 1;
    if (p.dist >= emaxs[p.type]!) return 2;
    return 3;
  }
  let dist1: number, dist2: number;
  const n = p.normal;
  switch (p.signbits) {
    case 0:
      dist1 = dot3(n, emaxs, emaxs, emaxs);
      dist2 = dot3(n, emins, emins, emins);
      break;
    case 1:
      dist1 = dot3(n, emins, emaxs, emaxs);
      dist2 = dot3(n, emaxs, emins, emins);
      break;
    case 2:
      dist1 = dot3(n, emaxs, emins, emaxs);
      dist2 = dot3(n, emins, emaxs, emins);
      break;
    case 3:
      dist1 = dot3(n, emins, emins, emaxs);
      dist2 = dot3(n, emaxs, emaxs, emins);
      break;
    case 4:
      dist1 = dot3(n, emaxs, emaxs, emins);
      dist2 = dot3(n, emins, emins, emaxs);
      break;
    case 5:
      dist1 = dot3(n, emins, emaxs, emins);
      dist2 = dot3(n, emaxs, emins, emaxs);
      break;
    case 6:
      dist1 = dot3(n, emaxs, emins, emins);
      dist2 = dot3(n, emins, emaxs, emaxs);
      break;
    case 7:
      dist1 = dot3(n, emins, emins, emins);
      dist2 = dot3(n, emaxs, emaxs, emaxs);
      break;
    default:
      dist1 = dist2 = 0;
      break;
  }
  let sides = 0;
  if (dist1 >= p.dist) sides = 1;
  if (dist2 < p.dist) sides |= 2;
  return sides;
}

// C: q_shared.h BOX_ON_PLANE_SIDE macro
export function BOX_ON_PLANE_SIDE(emins: ArrayLike<number>, emaxs: ArrayLike<number>, p: CPlane): number {
  if (p.type < 3) {
    if (p.dist <= emins[p.type]!) return 1;
    if (p.dist >= emaxs[p.type]!) return 2;
    return 3;
  }
  return BoxOnPlaneSide(emins, emaxs, p);
}

// C: q_shared.c:716 ClearBounds
export function ClearBounds(mins: Vec3, maxs: Vec3): void {
  mins[0] = mins[1] = mins[2] = 99999;
  maxs[0] = maxs[1] = maxs[2] = -99999;
}

// C: q_shared.c:722 AddPointToBounds
export function AddPointToBounds(v: ArrayLike<number>, mins: Vec3, maxs: Vec3): void {
  for (let i = 0; i < 3; i++) {
    const val = v[i]!;
    if (val < mins[i]!) mins[i] = val;
    if (val > maxs[i]!) maxs[i] = val;
  }
}

// C: q_shared.h ANGLE2SHORT: ((int)((x)*65536/360) & 65535), x is float -> float arithmetic
export function ANGLE2SHORT(x: number): number {
  return cInt(fr(fr(fr(x) * 65536) / 360)) & 65535;
}

// C: q_shared.h SHORT2ANGLE: ((x)*(360.0/65536)) (double; narrowed by the caller's store)
export function SHORT2ANGLE(x: number): number {
  return x * (360.0 / 65536);
}
