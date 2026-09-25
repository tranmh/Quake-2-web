import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  ANGLE2SHORT,
  AddPointToBounds,
  AngleVectors,
  BoxOnPlaneSide,
  BoxOnPlaneSide2,
  CPlane,
  ClearBounds,
  CrossProduct,
  DotProduct,
  LerpAngle,
  Q_log2,
  RotatePointAroundVector,
  SHORT2ANGLE,
  VectorLength,
  VectorMA,
  VectorNormalize,
  anglemod,
  cInt,
  cShort,
  floatBits,
  fr,
  vec3,
} from '../src';
import { cmpF32, cmpF32Vec } from '../src/testing';

const here = dirname(fileURLToPath(import.meta.url));

interface Rec {
  fr: number;
  a: number[];
  f: number[];
  r: number[];
  u: number[];
  n: number[];
  d: number[];
  len: number;
  l2: number;
  am: number;
  la: number;
  c: number[];
  p: number[];
  pd: number;
  mn: number[];
  mx: number[];
  bops: number;
}

const recs: Rec[] = readFileSync(join(here, 'data', 'q_shared_c.jsonl'), 'utf8')
  .split('\n')
  .filter(Boolean)
  .map((l) => JSON.parse(l) as Rec);

describe('q_shared math vs C (gcc, oracle flags)', () => {
  it('matches bit-exactly for AngleVectors, VectorNormalize, VectorLength, anglemod, LerpAngle, CrossProduct, RotatePointAroundVector, BoxOnPlaneSide', () => {
    const f = vec3(),
      r = vec3(),
      u = vec3(),
      d = vec3(),
      c = vec3(),
      p = vec3();
    let err: string | null = null;
    for (let i = 0; i < recs.length && !err; i++) {
      const x = recs[i]!;
      const a = Float32Array.from(x.a);
      const n = Float32Array.from(x.n);
      AngleVectors(a, f, r, u);
      d.set(n);
      const len = VectorNormalize(d);
      CrossProduct(f, n, c);
      RotatePointAroundVector(p, f, n, a[2]!);
      const pl = new CPlane();
      pl.normal.set(d);
      pl.dist = x.pd;
      pl.type = 3 + (i % 3);
      pl.signbits = (d[0]! < 0 ? 1 : 0) | (d[1]! < 0 ? 2 : 0) | (d[2]! < 0 ? 4 : 0);
      err =
        cmpF32Vec(`[${i}] forward`, f, x.f) ??
        cmpF32Vec(`[${i}] right`, r, x.r) ??
        cmpF32Vec(`[${i}] up`, u, x.u) ??
        cmpF32Vec(`[${i}] normalized`, d, x.d) ??
        cmpF32(`[${i}] len`, len, x.len) ??
        cmpF32(`[${i}] VectorLength`, VectorLength(n), x.l2) ??
        cmpF32(`[${i}] anglemod`, anglemod(a[0]!), x.am) ??
        cmpF32(`[${i}] LerpAngle`, LerpAngle(a[0]!, a[1]!, x.fr), x.la) ??
        cmpF32Vec(`[${i}] CrossProduct`, c, x.c) ??
        cmpF32Vec(`[${i}] RotatePointAroundVector`, p, x.p) ??
        (BoxOnPlaneSide(Float32Array.from(x.mn), Float32Array.from(x.mx), pl) === x.bops
          ? null
          : `[${i}] BoxOnPlaneSide`);
    }
    expect(err).toBeNull();
  });
});

describe('q_shared math', () => {
  it('DotProduct rounds every operation to float', () => {
    const a = Float32Array.from([0.1, 0.2, 0.3]);
    const b = Float32Array.from([3, 7, 11]);
    const exact = a[0]! * 3 + a[1]! * 7 + a[2]! * 11;
    expect(DotProduct(a, b)).toBe(fr(fr(fr(a[0]! * 3) + fr(a[1]! * 7)) + fr(a[2]! * 11)));
    expect(Math.abs(DotProduct(a, b) - exact)).toBeLessThan(1e-5);
  });

  it('VectorMA', () => {
    const out = vec3();
    VectorMA(vec3(1, 2, 3), 0.1, vec3(10, 20, 30), out);
    expect(Array.from(out)).toEqual([
      fr(1 + fr(fr(0.1) * 10)),
      fr(2 + fr(fr(0.1) * 20)),
      fr(3 + fr(fr(0.1) * 30)),
    ]);
  });

  it('VectorNormalize of zero vector returns 0 and leaves it', () => {
    const v = vec3();
    expect(VectorNormalize(v)).toBe(0);
    expect(Array.from(v)).toEqual([0, 0, 0]);
  });

  it('AngleVectors basic axes', () => {
    const f = vec3(),
      r = vec3(),
      u = vec3();
    AngleVectors(vec3(0, 0, 0), f, r, u);
    // C signed zeros: forward[2] = -sp = -0, right[2] = -1*sr*cp = -0
    expect(Array.from(f)).toEqual([1, 0, -0]);
    expect(Array.from(r)).toEqual([0, -1, -0]);
    expect(Array.from(u)).toEqual([0, 0, 1]);
    AngleVectors(vec3(0, 90, 0), f, null, null);
    expect(f[1]).toBe(1);
    expect(Math.abs(f[0]!)).toBeLessThan(1e-7);
  });

  it('anglemod / LerpAngle / ANGLE2SHORT / SHORT2ANGLE', () => {
    expect(anglemod(360)).toBe(0);
    expect(anglemod(-90)).toBe(270);
    expect(LerpAngle(350, 10, 0.5)).toBe(360);
    expect(LerpAngle(10, 350, 0.5)).toBe(0);
    expect(ANGLE2SHORT(90)).toBe(16384);
    expect(ANGLE2SHORT(-90)).toBe(49152);
    expect(fr(SHORT2ANGLE(16384))).toBe(90);
  });

  it('BoxOnPlaneSide fast axial and general agree with BoxOnPlaneSide2', () => {
    const p = new CPlane();
    p.normal.set([0, 0, 1]);
    p.dist = 10;
    p.type = 2;
    expect(BoxOnPlaneSide(vec3(0, 0, 11), vec3(1, 1, 12), p)).toBe(1);
    expect(BoxOnPlaneSide(vec3(0, 0, 0), vec3(1, 1, 5), p)).toBe(2);
    expect(BoxOnPlaneSide(vec3(0, 0, 0), vec3(1, 1, 50), p)).toBe(3);
    const q = new CPlane();
    q.normal.set([0.6, -0.8, 0]);
    q.dist = 3;
    q.type = 3;
    q.signbits = 2;
    for (const [mn, mx] of [
      [vec3(10, -10, 0), vec3(11, -9, 1)],
      [vec3(-10, 10, 0), vec3(-9, 11, 1)],
      [vec3(-10, -10, 0), vec3(10, 10, 1)],
    ] as const) {
      expect(BoxOnPlaneSide(mn, mx, q)).toBe(BoxOnPlaneSide2(mn, mx, q));
    }
  });

  it('bounds', () => {
    const mn = vec3(),
      mx = vec3();
    ClearBounds(mn, mx);
    AddPointToBounds(vec3(1, -2, 3), mn, mx);
    AddPointToBounds(vec3(-1, 2, 0), mn, mx);
    expect(Array.from(mn)).toEqual([-1, -2, 0]);
    expect(Array.from(mx)).toEqual([1, 2, 3]);
  });

  it('Q_log2 and C casts', () => {
    expect(Q_log2(1)).toBe(0);
    expect(Q_log2(256)).toBe(8);
    expect(Q_log2(257)).toBe(8);
    expect(cInt(-1.9)).toBe(-1);
    expect(cInt(3e10)).toBe(-2147483648);
    expect(cInt(NaN)).toBe(-2147483648);
    expect(cShort(40000)).toBe(-25536);
    expect(floatBits(-0)).toBe(0x80000000);
  });
});
