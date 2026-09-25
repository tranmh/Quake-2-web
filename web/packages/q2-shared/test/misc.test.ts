import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  Com_sprintf,
  EntityState,
  PlayerState,
  PmoveState,
  QRand,
  Q2String,
  Trace,
  UserCmd,
  latin1FromBytes,
  latin1ToBytes,
  readCString,
  sprintf,
} from '../src';
import { fileExists, fixturePath, readJson } from '../src/testing';

const here = dirname(fileURLToPath(import.meta.url));

describe('QRand (glibc TYPE_3)', () => {
  it('first values after srand(1)', () => {
    const r = new QRand(1);
    expect([r.rand(), r.rand(), r.rand(), r.rand(), r.rand()]).toEqual([
      1804289383, 846930886, 1681692777, 1714636915, 1957747793,
    ]);
  });

  it('srand(0) behaves like srand(1)', () => {
    const a = new QRand(0);
    expect(a.rand()).toBe(1804289383);
  });

  it('save/restore', () => {
    const a = new QRand(7);
    a.rand();
    const snap = a.save();
    const x = [a.rand(), a.rand()];
    a.restore(snap);
    expect([a.rand(), a.rand()]).toEqual(x);
  });

  for (const [file, seed] of [
    ['rand.json', 1],
    ['rand-12345.json', 12345],
  ] as const) {
    const p = fixturePath('core', file);
    it.skipIf(!fileExists(p))(`golden core/${file}`, () => {
      const fx = readJson<{ seed: number; values: number[] }>(p);
      expect(fx.seed).toBe(seed);
      const r = new QRand(fx.seed);
      let bad: string | null = null;
      for (let i = 0; i < fx.values.length; i++) {
        const v = r.rand();
        if (v !== fx.values[i]) {
          bad = `value ${i}: got ${v} want ${fx.values[i]}`;
          break;
        }
      }
      expect(bad).toBeNull();
      expect(fx.values.length).toBeGreaterThan(0);
    });
  }
});

describe('sprintf', () => {
  const cases = JSON.parse(readFileSync(join(here, 'data', 'printf_glibc.json'), 'utf8')) as [
    string,
    number | string,
    string,
  ][];
  it(`matches glibc output for ${cases.length} cases`, () => {
    const bad: string[] = [];
    for (const [fmt, v, want] of cases) {
      const got = sprintf(fmt, v);
      if (got !== want) bad.push(`${fmt} ${v}: got ${JSON.stringify(got)} want ${JSON.stringify(want)}`);
    }
    expect(bad.slice(0, 10)).toEqual([]);
  });

  it('multiple args, %%, * width, %c from number', () => {
    expect(sprintf('%s=%d%%', 'hp', 100)).toBe('hp=100%');
    expect(sprintf('%*d|%-*d|', 4, 7, 3, 1)).toBe('   7|1  |');
    expect(sprintf('%c%c', 72, 'i')).toBe('Hi');
    expect(sprintf('%-12.12s|', 'player')).toBe('player      |');
    expect(sprintf('%5.1f', 22.25)).toBe(' 22.2');
  });

  it('Com_sprintf truncates to size-1', () => {
    expect(Com_sprintf(4, '%s', 'abcdef')).toBe('abc');
    expect(Com_sprintf(10, '%d', 5)).toBe('5');
  });
});

describe('Q2String', () => {
  it('round-trips all byte values', () => {
    const b = new Uint8Array(256).map((_, i) => i);
    const s = latin1FromBytes(b);
    expect(s.length).toBe(256);
    expect(Array.from(latin1ToBytes(s))).toEqual(Array.from(b));
  });

  it('readCString stops at NUL / maxLen', () => {
    const b = Uint8Array.from([104, 105, 0, 120]);
    expect(readCString(b)).toBe('hi');
    expect(readCString(b, 0, 1)).toBe('h');
    expect(readCString(b, 3)).toBe('x');
  });

  it('high bit helpers', () => {
    expect(Q2String.setHighBit('ab')).toBe('\xe1\xe2');
    expect(Q2String.stripHighBit('\xe1\xe2c')).toBe('abc');
    expect(Q2String.isByteString('abc\xff')).toBe(true);
    expect(Q2String.isByteString('€')).toBe(false);
    expect(Q2String.toByteString('a€b')).toBe('a?b');
    expect(Q2String.cstr('ab\0cd')).toBe('ab');
  });
});

describe('types', () => {
  it('Int16Array / Float32Array fields wrap and round', () => {
    const s = new PmoveState();
    s.origin[0] = 40000;
    expect(s.origin[0]).toBe(-25536);
    const t = new PmoveState().copyFrom(s);
    expect(t.equals(s)).toBe(true);
    t.velocity[1] = 1;
    expect(t.equals(s)).toBe(false);
    const e = new EntityState();
    e.origin[0] = 0.1;
    expect(e.origin[0]).toBe(Math.fround(0.1));
    const e2 = new EntityState().copyFrom(e);
    expect(e2.origin[0]).toBe(e.origin[0]);
    e2.clear();
    expect(e2.origin[0]).toBe(0);
    const u = new UserCmd();
    u.angles[1] = 70000;
    expect(new UserCmd().copyFrom(u).angles[1]).toBe(4464);
    const ps = new PlayerState();
    ps.stats[31] = -5;
    ps.pmove.gravity = 800;
    const ps2 = new PlayerState().copyFrom(ps);
    expect(ps2.stats[31]).toBe(-5);
    expect(ps2.pmove.gravity).toBe(800);
    const tr = new Trace();
    tr.fraction = 0.5;
    tr.plane.normal[2] = 1;
    expect(new Trace().copyFrom(tr).plane.normal[2]).toBe(1);
  });
});
