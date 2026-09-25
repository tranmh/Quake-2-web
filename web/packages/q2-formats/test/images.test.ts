import { describe, expect, it } from 'vitest';
import { FormatError, decodePcx, decodeTga, pcxToRgba } from '../src';
import { W } from './helpers';

function pcxHeader(
  xmax: number,
  ymax: number,
  over: Partial<Record<'man' | 'ver' | 'enc' | 'bpp', number>> = {},
): W {
  const w = new W()
    .u8(over.man ?? 10, over.ver ?? 5, over.enc ?? 1, over.bpp ?? 8)
    .i16(0, 0, xmax, ymax, 72, 72);
  w.pad(48)
    .u8(0, 1)
    .i16(xmax + 1, 1)
    .pad(58);
  return w;
}

function palette(): number[] {
  const p: number[] = [];
  for (let i = 0; i < 256; i++) p.push(i, 255 - i, i >> 1);
  return p;
}

describe('pcx', () => {
  it('decodes RLE data and takes the palette from the file end', () => {
    // 4x2: row0 = run of 3 x 7 then literal 5; row1 = literal 0xC1 via run-of-1 escape, then run 3 x 2
    const w = pcxHeader(3, 1).u8(0xc3, 7, 5, 0xc1, 0xc1, 0xc3, 2).bytes(palette());
    const img = decodePcx(w.build());
    expect([img.width, img.height]).toEqual([4, 2]);
    expect(Array.from(img.pixels)).toEqual([7, 7, 7, 5, 0xc1, 2, 2, 2]);
    expect(img.palette.length).toBe(768);
    expect(Array.from(img.palette.subarray(3, 6))).toEqual([1, 254, 0]);
    const rgba = pcxToRgba(img);
    expect(Array.from(rgba.subarray(0, 4))).toEqual([7, 248, 3, 255]);
  });
  it('lets a run spill across a row end (next row overwrites)', () => {
    const w = pcxHeader(1, 1).u8(0xc3, 9, 0xc2, 4).bytes(palette());
    expect(Array.from(decodePcx(w.build()).pixels)).toEqual([9, 9, 4, 4]);
  });
  it('checks the header like LoadPCX', () => {
    expect(() => decodePcx(pcxHeader(3, 1, { ver: 3 }).bytes(palette()).build())).toThrow(/Bad pcx/);
    expect(() => decodePcx(pcxHeader(3, 1, { bpp: 4 }).bytes(palette()).build())).toThrow(/Bad pcx/);
    expect(() => decodePcx(pcxHeader(640, 1).bytes(palette()).build())).toThrow(/Bad pcx/);
    expect(() => decodePcx(pcxHeader(3, 480).bytes(palette()).build())).toThrow(/Bad pcx/);
  });
  it('flags overruns as malformed', () => {
    // no palette and not enough data: reading past the end
    const w = pcxHeader(3, 1).pad(700, 0xc1);
    // 700 bytes of 0xC1 decode as runs of 1 of value 0xC1; needs 16 bytes -> fine; make image huge instead
    expect(() => decodePcx(pcxHeader(639, 479).bytes(palette()).build())).toThrow(/malformed/);
    expect(decodePcx(w.build()).width).toBe(4);
  });
});

function tgaHeader(type: number, w: number, h: number, bpp: number, attr = 0, idlen = 0): W {
  return new W().u8(idlen, 0, type).i16(0, 0).u8(0).i16(0, 0, w, h).u8(bpp, attr);
}

describe('tga', () => {
  it('decodes type 2 (24 and 32 bit), bottom-up, ignoring the origin bit', () => {
    for (const attr of [0, 0x20]) {
      const t = tgaHeader(2, 2, 2, 24, attr, 3).u8(9, 9, 9);
      // bottom row first: (b,g,r)
      t.u8(1, 2, 3, 4, 5, 6).u8(7, 8, 9, 10, 11, 12);
      const img = decodeTga(t.build());
      expect(Array.from(img.rgba)).toEqual([9, 8, 7, 255, 12, 11, 10, 255, 3, 2, 1, 255, 6, 5, 4, 255]);
    }
    const t32 = tgaHeader(2, 1, 1, 32).u8(1, 2, 3, 4);
    expect(Array.from(decodeTga(t32.build()).rgba)).toEqual([3, 2, 1, 4]);
  });
  it('decodes type 10 RLE with packets spanning rows', () => {
    // 3x2, 32-bit: run of 4 (spans into the upper row), then raw packet of 2
    const t = tgaHeader(10, 3, 2, 32).u8(0x83, 10, 20, 30, 40).u8(0x01, 1, 2, 3, 4, 5, 6, 7, 8);
    const img = decodeTga(t.build());
    const px = (i: number) => Array.from(img.rgba.subarray(i * 4, i * 4 + 4));
    // bottom row (row 1) = first 3 pixels
    expect([px(3), px(4), px(5)]).toEqual([
      [30, 20, 10, 40],
      [30, 20, 10, 40],
      [30, 20, 10, 40],
    ]);
    expect([px(0), px(1), px(2)]).toEqual([
      [30, 20, 10, 40],
      [3, 2, 1, 4],
      [7, 6, 5, 8],
    ]);
    // 24-bit RLE: alpha 255; trailing packet data beyond the last pixel is ignored
    const t24 = tgaHeader(10, 1, 1, 24).u8(0x85, 1, 2, 3).u8(0xff);
    expect(Array.from(decodeTga(t24.build()).rgba)).toEqual([3, 2, 1, 255]);
  });
  it('rejects unsupported / truncated files', () => {
    expect(() => decodeTga(tgaHeader(1, 1, 1, 24).build())).toThrow(/type 2 and 10/);
    expect(() => decodeTga(tgaHeader(2, 1, 1, 16).build())).toThrow(/32 or 24/);
    expect(() => decodeTga(tgaHeader(2, 2, 2, 24).u8(1, 2).build())).toThrow(FormatError);
  });
});
