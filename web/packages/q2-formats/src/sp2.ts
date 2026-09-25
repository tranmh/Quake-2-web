// SP2 sprite parser (qcommon/qfiles.h dsprite_t; checks from ref_gl/gl_model.c Mod_LoadSpriteModel).
import { IDSPRITEHEADER, MAX_MD2SKINS, MAX_SKINNAME, SPRITE_VERSION, readCString } from 'q2-shared';
import { LEView, toU8 } from './bytes';
import { FormatError } from './errors';

export interface Sp2Frame {
  width: number;
  height: number;
  origin_x: number;
  origin_y: number;
  name: string;
}

export interface Sp2Sprite {
  ident: number;
  version: number;
  frames: Sp2Frame[];
}

// C: ref_gl/gl_model.c:1061 Mod_LoadSpriteModel
export function parseSp2(data: ArrayBuffer | Uint8Array, name = 'sp2'): Sp2Sprite {
  const bytes = toU8(data);
  const r = new LEView(bytes, name);
  const ident = r.i32(0);
  const version = r.i32(4);
  const numframes = r.i32(8);
  if (ident !== IDSPRITEHEADER) throw new FormatError(`${name}: not an SP2 file`);
  if (version !== SPRITE_VERSION) {
    throw new FormatError(`${name} has wrong version number (${version} should be ${SPRITE_VERSION})`);
  }
  if (numframes > MAX_MD2SKINS) {
    throw new FormatError(`${name} has too many frames (${numframes} > ${MAX_MD2SKINS})`);
  }
  const frames: Sp2Frame[] = [];
  const fsize = 16 + MAX_SKINNAME;
  for (let i = 0; i < numframes; i++) {
    const o = 12 + i * fsize;
    r.check(o, fsize);
    frames.push({
      width: r.i32(o),
      height: r.i32(o + 4),
      origin_x: r.i32(o + 8),
      origin_y: r.i32(o + 12),
      name: readCString(bytes, o + 16, MAX_SKINNAME),
    });
  }
  return { ident, version, frames };
}
