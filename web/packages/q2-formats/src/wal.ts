// WAL texture parser (qcommon/qfiles.h miptex_t; ref_gl/gl_image.c GL_LoadWal).
import { MIPLEVELS, readCString } from 'q2-shared';
import { LEView, toU8 } from './bytes';
import { FormatError } from './errors';

export interface WalTexture {
  name: string;
  width: number;
  height: number;
  offsets: number[];
  animname: string;
  flags: number;
  contents: number;
  value: number;
  /** 8-bit paletted pixels of each mip level (level i is (width>>i) x (height>>i)) */
  mips: Uint8Array[];
}

export const MIPTEX_SIZE = 100;

// C: ref_gl/gl_image.c GL_LoadWal (header decode) + qfiles.h miptex_t
export function parseWal(data: ArrayBuffer | Uint8Array, name = 'wal'): WalTexture {
  const bytes = toU8(data);
  const r = new LEView(bytes, name);
  r.check(0, MIPTEX_SIZE);
  const width = r.u32(32);
  const height = r.u32(36);
  const offsets: number[] = [];
  for (let i = 0; i < MIPLEVELS; i++) offsets.push(r.u32(40 + i * 4));
  const mips: Uint8Array[] = [];
  for (let i = 0; i < MIPLEVELS; i++) {
    const w = width >>> i;
    const h = height >>> i;
    const len = w * h;
    const o = offsets[i]!;
    if (o + len > bytes.length) throw new FormatError(`${name}: mip ${i} out of bounds`);
    mips.push(bytes.slice(o, o + len));
  }
  return {
    name: readCString(bytes, 0, 32),
    width,
    height,
    offsets,
    animname: readCString(bytes, 56, 32),
    flags: r.i32(88),
    contents: r.i32(92),
    value: r.i32(96),
    mips,
  };
}
