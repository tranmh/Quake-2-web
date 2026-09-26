// MD2 alias model parser (qcommon/qfiles.h dmdl_t; checks from ref_gl/gl_model.c Mod_LoadAliasModel).
import { ALIAS_VERSION, IDALIASHEADER, MAX_SKINNAME, MAX_VERTS, readCString } from 'q2-shared';
import { LEView, toU8 } from './bytes';
import { FormatError } from './errors';

/** ref_gl/gl_local.h MAX_LBM_HEIGHT */
export const MAX_LBM_HEIGHT = 480;

export interface Md2Header {
  ident: number;
  version: number;
  skinwidth: number;
  skinheight: number;
  framesize: number;
  num_skins: number;
  num_xyz: number;
  num_st: number;
  num_tris: number;
  num_glcmds: number;
  num_frames: number;
  ofs_skins: number;
  ofs_st: number;
  ofs_tris: number;
  ofs_frames: number;
  ofs_glcmds: number;
  ofs_end: number;
}

export interface Md2Frame {
  scale: Float32Array; // 3
  translate: Float32Array; // 3
  name: string;
  /** num_xyz * 4 bytes: v[0], v[1], v[2], lightnormalindex */
  verts: Uint8Array;
}

export interface Md2GlCmd {
  /** true: triangle strip, false: triangle fan */
  strip: boolean;
  /** per vertex: s, t (float) */
  st: Float32Array;
  /** per vertex: index into frame verts */
  indices: Int32Array;
}

export interface Md2Model {
  header: Md2Header;
  skins: string[];
  /** num_st * 2: s, t (short) */
  st: Int16Array;
  /** num_tris * 3 */
  trisXyz: Int16Array;
  /** num_tris * 3 */
  trisSt: Int16Array;
  frames: Md2Frame[];
  /** raw glcmd dwords (the float s/t words are stored as their int bit patterns) */
  glcmds: Int32Array;
}

const HEADER_FIELDS = [
  'ident',
  'version',
  'skinwidth',
  'skinheight',
  'framesize',
  'num_skins',
  'num_xyz',
  'num_st',
  'num_tris',
  'num_glcmds',
  'num_frames',
  'ofs_skins',
  'ofs_st',
  'ofs_tris',
  'ofs_frames',
  'ofs_glcmds',
  'ofs_end',
] as const;

// C: ref_gl/gl_model.c:929 Mod_LoadAliasModel (parsing and sanity checks only)
export function parseMd2(data: ArrayBuffer | Uint8Array, name = 'md2'): Md2Model {
  const bytes = toU8(data);
  const r = new LEView(bytes, name);
  const h = {} as Md2Header;
  HEADER_FIELDS.forEach((f, i) => (h[f] = r.i32(i * 4)));
  if (h.ident !== IDALIASHEADER) throw new FormatError(`${name}: not an MD2 file`);
  if (h.version !== ALIAS_VERSION) {
    throw new FormatError(`${name} has wrong version number (${h.version} should be ${ALIAS_VERSION})`);
  }
  if (h.skinheight > MAX_LBM_HEIGHT)
    throw new FormatError(`model ${name} has a skin taller than ${MAX_LBM_HEIGHT}`);
  if (h.num_xyz <= 0) throw new FormatError(`model ${name} has no vertices`);
  if (h.num_xyz > MAX_VERTS) throw new FormatError(`model ${name} has too many vertices`);
  if (h.num_st <= 0) throw new FormatError(`model ${name} has no st vertices`);
  if (h.num_tris <= 0) throw new FormatError(`model ${name} has no triangles`);
  if (h.num_frames <= 0) throw new FormatError(`model ${name} has no frames`);
  if (h.num_skins < 0 || h.num_glcmds < 0) throw new FormatError(`${name}: negative count`);

  // bounds are checked before allocating: the counts are attacker-controlled (a user-uploaded pak) and an
  // unchecked `new Int16Array(num_st * 2)` would reserve gigabytes before the read fails
  r.check(h.ofs_st, h.num_st * 4);
  r.check(h.ofs_tris, h.num_tris * 12);
  const st = new Int16Array(h.num_st * 2);
  for (let i = 0; i < h.num_st * 2; i++) st[i] = r.i16(h.ofs_st + i * 2);

  const trisXyz = new Int16Array(h.num_tris * 3);
  const trisSt = new Int16Array(h.num_tris * 3);
  for (let i = 0; i < h.num_tris; i++) {
    for (let j = 0; j < 3; j++) {
      trisXyz[i * 3 + j] = r.i16(h.ofs_tris + i * 12 + j * 2);
      trisSt[i * 3 + j] = r.i16(h.ofs_tris + i * 12 + 6 + j * 2);
    }
  }

  // Frames must not overlap (framesize >= daliasframe_t + verts; every file written by the tools has it
  // exactly) and must all lie inside the file -- checked up front so that a crafted num_frames with a
  // zero framesize cannot create billions of frame objects (memory safety, not checked by the C code).
  const minFrame = 40 + h.num_xyz * 4;
  if (h.framesize < minFrame) throw new FormatError(`model ${name} has a bad frame size`);
  r.check(h.ofs_frames, h.num_frames * h.framesize);
  const frames: Md2Frame[] = [];
  for (let i = 0; i < h.num_frames; i++) {
    const o = h.ofs_frames + i * h.framesize;
    r.check(o, 40 + h.num_xyz * 4);
    const scale = new Float32Array(3);
    const translate = new Float32Array(3);
    for (let j = 0; j < 3; j++) {
      scale[j] = r.f32(o + j * 4);
      translate[j] = r.f32(o + 12 + j * 4);
    }
    frames.push({
      scale,
      translate,
      name: readCString(bytes, o + 24, 16),
      verts: bytes.slice(o + 40, o + 40 + h.num_xyz * 4),
    });
  }

  r.check(h.ofs_glcmds, h.num_glcmds * 4);
  const glcmds = new Int32Array(h.num_glcmds);
  for (let i = 0; i < h.num_glcmds; i++) glcmds[i] = r.i32(h.ofs_glcmds + i * 4);

  r.check(h.ofs_skins, h.num_skins * MAX_SKINNAME);
  const skins: string[] = [];
  for (let i = 0; i < h.num_skins; i++)
    skins.push(readCString(bytes, h.ofs_skins + i * MAX_SKINNAME, MAX_SKINNAME));

  return { header: h, skins, st, trisXyz, trisSt, frames, glcmds };
}

/**
 * Decode the glcmd list as ref_gl/gl_mesh.c GL_DrawAliasFrameLerp walks it: a positive count starts a
 * strip, negative a fan, zero ends. Each vertex is (float s, float t, int index).
 */
export function decodeGlCmds(glcmds: Int32Array): Md2GlCmd[] {
  const out: Md2GlCmd[] = [];
  const f = new Float32Array(glcmds.buffer, glcmds.byteOffset, glcmds.length);
  let p = 0;
  for (;;) {
    if (p >= glcmds.length) throw new FormatError('md2: unterminated glcmd list');
    let count = glcmds[p++]!;
    if (!count) break;
    const strip = count > 0;
    if (count < 0) count = -count;
    if (p + count * 3 > glcmds.length) throw new FormatError('md2: glcmd list out of bounds');
    const st = new Float32Array(count * 2);
    const indices = new Int32Array(count);
    for (let i = 0; i < count; i++) {
      st[i * 2] = f[p]!;
      st[i * 2 + 1] = f[p + 1]!;
      indices[i] = glcmds[p + 2]!;
      p += 3;
    }
    out.push({ strip, st, indices });
  }
  return out;
}
