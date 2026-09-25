import { describe, expect, it } from 'vitest';
import { FormatError, decodeGlCmds, parseMd2, parseSp2, parseWal } from '../src';
import { W } from './helpers';

function makeMd2(opts: { num_xyz?: number; version?: number; skinheight?: number } = {}): Uint8Array {
  const num_xyz = opts.num_xyz ?? 3;
  const num_st = 3,
    num_tris = 1,
    num_frames = 2,
    num_skins = 1;
  const framesize = 40 + num_xyz * 4;
  const glcmds = new W().i32(-3).f32(0.5, 0.25).i32(0).f32(1, 0).i32(1).f32(0, 1).i32(2).i32(0).build();
  const ofs_skins = 68;
  const ofs_st = ofs_skins + 64 * num_skins;
  const ofs_tris = ofs_st + num_st * 4;
  const ofs_frames = ofs_tris + num_tris * 12;
  const ofs_glcmds = ofs_frames + num_frames * framesize;
  const ofs_end = ofs_glcmds + glcmds.length;
  const w = new W()
    .u8(0x49, 0x44, 0x50, 0x32)
    .i32(
      opts.version ?? 8,
      64,
      opts.skinheight ?? 64,
      framesize,
      num_skins,
      num_xyz,
      num_st,
      num_tris,
      11,
      num_frames,
    )
    .i32(ofs_skins, ofs_st, ofs_tris, ofs_frames, ofs_glcmds, ofs_end)
    .str('models/test/skin.pcx', 64)
    .i16(0, 0, 63, 0, 0, -1)
    .i16(0, 1, 2, 0, 1, 2);
  for (let f = 0; f < num_frames; f++) {
    w.f32(1, 2, 3, -10, -20, f).str(`frame${f}`, 16);
    for (let v = 0; v < num_xyz; v++) w.u8(v, v + 1, v + 2, 100 + f);
  }
  w.bytes(glcmds);
  return w.build();
}

describe('md2', () => {
  it('parses all sections', () => {
    const m = parseMd2(makeMd2());
    expect(m.header.num_frames).toBe(2);
    expect(m.skins).toEqual(['models/test/skin.pcx']);
    expect(Array.from(m.st)).toEqual([0, 0, 63, 0, 0, -1]);
    expect(Array.from(m.trisXyz)).toEqual([0, 1, 2]);
    expect(Array.from(m.trisSt)).toEqual([0, 1, 2]);
    expect(m.frames[1]!.name).toBe('frame1');
    expect(Array.from(m.frames[1]!.scale)).toEqual([1, 2, 3]);
    expect(Array.from(m.frames[1]!.translate)).toEqual([-10, -20, 1]);
    expect(Array.from(m.frames[0]!.verts.subarray(4, 8))).toEqual([1, 2, 3, 100]);
    expect(m.glcmds.length).toBe(11);
    const cmds = decodeGlCmds(m.glcmds);
    expect(cmds).toHaveLength(1);
    expect(cmds[0]!.strip).toBe(false);
    expect(Array.from(cmds[0]!.indices)).toEqual([0, 1, 2]);
    expect(Array.from(cmds[0]!.st)).toEqual([0.5, 0.25, 1, 0, 0, 1]);
  });
  it('applies the Mod_LoadAliasModel checks', () => {
    expect(() => parseMd2(makeMd2({ version: 7 }))).toThrow(/wrong version/);
    expect(() => parseMd2(makeMd2({ num_xyz: 0 }))).toThrow(/no vertices/);
    expect(() => parseMd2(makeMd2({ num_xyz: 2049 }))).toThrow(FormatError);
    expect(() => parseMd2(makeMd2({ skinheight: 481 }))).toThrow(/taller/);
    expect(() => parseMd2(makeMd2().subarray(0, 200))).toThrow(FormatError);
  });
});

describe('sp2', () => {
  it('parses frames', () => {
    const w = new W().u8(0x49, 0x44, 0x53, 0x32).i32(2, 2);
    w.i32(32, 16, 16, 8).str('sprites/a_0.pcx', 64);
    w.i32(64, 64, 32, 32).str('sprites/a_1.pcx', 64);
    const s = parseSp2(w.build());
    expect(s.frames).toEqual([
      { width: 32, height: 16, origin_x: 16, origin_y: 8, name: 'sprites/a_0.pcx' },
      { width: 64, height: 64, origin_x: 32, origin_y: 32, name: 'sprites/a_1.pcx' },
    ]);
  });
  it('rejects bad sprites', () => {
    expect(() => parseSp2(new W().u8(0x49, 0x44, 0x53, 0x32).i32(1, 0).build())).toThrow(/wrong version/);
    expect(() => parseSp2(new W().u8(0x49, 0x44, 0x53, 0x32).i32(2, 33).build())).toThrow(/too many frames/);
    expect(() => parseSp2(new W().u8(0x49, 0x44, 0x53, 0x32).i32(2, 1).build())).toThrow(FormatError);
  });
});

describe('wal', () => {
  it('parses header and mips', () => {
    const w = new W().str('e1u1/floor1', 32).i32(8, 8);
    const o0 = 100;
    w.i32(o0, o0 + 64, o0 + 80, o0 + 84)
      .str('e1u1/floor2', 32)
      .i32(1, 2, 3);
    for (let i = 0; i < 64 + 16 + 4 + 1; i++) w.u8(i);
    const t = parseWal(w.build());
    expect(t.name).toBe('e1u1/floor1');
    expect(t.animname).toBe('e1u1/floor2');
    expect([t.width, t.height, t.flags, t.contents, t.value]).toEqual([8, 8, 1, 2, 3]);
    expect(t.mips.map((m) => m.length)).toEqual([64, 16, 4, 1]);
    expect(t.mips[1]![0]).toBe(64);
    expect(t.mips[3]![0]).toBe(84);
  });
  it('rejects truncated data', () => {
    expect(() => parseWal(new Uint8Array(50))).toThrow(FormatError);
    const w = new W().str('x', 32).i32(8, 4, 100, 132, 140, 142).str('', 32).i32(0, 0, 0);
    expect(() => parseWal(w.build())).toThrow(/out of bounds/);
  });
});
