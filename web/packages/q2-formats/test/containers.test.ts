import { describe, expect, it } from 'vitest';
import { FormatError, Pak, SearchPath, parseBsp } from '../src';
import { W } from './helpers';

function makePak(files: Record<string, number[]>): Uint8Array {
  const names = Object.keys(files);
  const w = new W().u8(0x50, 0x41, 0x43, 0x4b).i32(0, 0);
  const pos: number[] = [];
  for (const n of names) {
    pos.push(w.length);
    w.bytes(files[n]!);
  }
  const dirofs = w.length;
  names.forEach((n, i) => w.str(n, 56).i32(pos[i]!, files[n]!.length));
  w.set32(4, dirofs).set32(8, names.length * 64);
  return w.build();
}

describe('pak', () => {
  it('lists and reads case-insensitively', () => {
    const pak = new Pak(makePak({ 'maps/a.bsp': [1, 2, 3], 'Pics/B.pcx': [4] }));
    expect(pak.list()).toEqual(['maps/a.bsp', 'Pics/B.pcx']);
    expect(Array.from(pak.read('MAPS/A.BSP')!)).toEqual([1, 2, 3]);
    expect(Array.from(pak.read('pics/b.pcx')!)).toEqual([4]);
    expect(pak.read('nope')).toBeUndefined();
  });
  it('rejects bad data', () => {
    expect(() => new Pak(new Uint8Array(4))).toThrow(FormatError);
    const b = makePak({ a: [1] });
    b[0] = 0;
    expect(() => new Pak(b)).toThrow(FormatError);
    const c = makePak({ a: [1] });
    new DataView(c.buffer).setInt32(4, 100000, true);
    expect(() => new Pak(c)).toThrow(FormatError);
    const d = makePak({ a: [1] });
    new DataView(d.buffer).setInt32(12 + 1 + 56 + 4, 999, true); // filelen
    const pd = new Pak(d);
    expect(() => pd.read('a')).toThrow(FormatError);
  });
  it('search path: later paks override earlier ones', () => {
    const sp = new SearchPath();
    sp.add(new Pak(makePak({ 'x.txt': [1], 'y.txt': [2] })));
    sp.add(new Pak(makePak({ 'X.TXT': [9] })));
    expect(Array.from(sp.read('x.txt')!)).toEqual([9]);
    expect(Array.from(sp.read('y.txt')!)).toEqual([2]);
    expect(sp.list()).toEqual(['x.txt', 'y.txt']);
  });
});

function emptyBsp(): W {
  const w = new W().u8(0x49, 0x42, 0x53, 0x50).i32(38);
  const hdr = 8 + 19 * 8;
  for (let i = 0; i < 19; i++) w.i32(hdr, 0);
  return w;
}

describe('bsp', () => {
  it('parses an empty-lump file', () => {
    const b = parseBsp(emptyBsp().build());
    expect(b.version).toBe(38);
    expect(b.planes.count).toBe(0);
    expect(b.entityString).toBe('');
  });
  it('parses a plane and entity string', () => {
    const w = emptyBsp();
    const ofs = w.length;
    w.str('{ "classname" "worldspawn" }', 29); // incl. trailing NUL
    const pofs = w.length;
    w.f32(0, 0, 1, 64).i32(2);
    w.set32(8, ofs).set32(12, 29).set32(16, pofs).set32(20, 20);
    const b = parseBsp(w.build());
    expect(b.entityString).toBe('{ "classname" "worldspawn" }');
    expect(b.planes.count).toBe(1);
    expect(Array.from(b.planes.normal)).toEqual([0, 0, 1]);
    expect(b.planes.dist[0]).toBe(64);
    expect(b.planes.type[0]).toBe(2);
  });
  it('rejects malformed files', () => {
    expect(() => parseBsp(new Uint8Array(10))).toThrow(FormatError);
    const badVer = emptyBsp().set32(4, 37).build();
    expect(() => parseBsp(badVer)).toThrow(/wrong version/);
    const badIdent = emptyBsp().set32(0, 0).build();
    expect(() => parseBsp(badIdent)).toThrow(FormatError);
    const oob = emptyBsp().set32(16, 1000).set32(20, 20).build();
    expect(() => parseBsp(oob)).toThrow(/out of bounds/);
    const w = emptyBsp();
    const o = w.length;
    w.pad(21);
    expect(() => parseBsp(w.set32(16, o).set32(20, 21).build())).toThrow(/funny lump size/);
  });
});
