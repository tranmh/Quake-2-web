// Port of ref_gl/gl_image.c: texture management, image loading, upload preparation.
//
// Differences to the C file (see docs/PARITY.md for the renderer):
//  * No GL_EXT_paletted_texture / GL_EXT_shared_texture_palette and no GL_SGIS_multitexture exist in
//    WebGL, so qglColorTableEXT and qglSelectTextureSGIS are NULL exactly like on a modern OpenGL driver:
//    the paletted upload branches are never taken and GL_SelectTexture/GL_EnableMultitexture are no-ops.
//  * Image files are read from the prefetched file cache (GLState.files); the async registration entry
//    points (gl_rmain.ts) fetch them before calling the synchronous loaders.
//  * The GL_MipMap box filter is ported exactly and every mip level is uploaded like C (no generateMipmap).
import { COM_FileExtension, ERR_DROP, ERR_FATAL, PRINT_ALL, PRINT_DEVELOPER, Q_stricmp, fr } from 'q2-shared';
import { decodeTga, parseWal, PCX_HEADER_SIZE, decodePcx, FormatError } from 'q2-formats';
import {
  GL_RENDERER_VOODOO,
  GL_RENDERER_VOODOO2,
  GLState,
  Image,
  MAX_GLTEXTURES,
  TEXNUM_IMAGES,
  TEXNUM_SCRAPS,
  it_pic,
  it_skin,
  it_sky,
  it_sprite,
  it_wall,
} from './gl_local';
import {
  GL_LINEAR,
  GL_LINEAR_MIPMAP_LINEAR,
  GL_LINEAR_MIPMAP_NEAREST,
  GL_NEAREST,
  GL_NEAREST_MIPMAP_LINEAR,
  GL_NEAREST_MIPMAP_NEAREST,
  GL_R3_G3_B2,
  GL_RGB,
  GL_RGB4,
  GL_RGB5,
  GL_RGB5_A1,
  GL_RGB8,
  GL_RGBA,
  GL_RGBA2,
  GL_RGBA4,
  GL_RGBA8,
} from './qgl';

// C: gl_image.c:47 GL_SetTexturePalette -- qglColorTableEXT is NULL (no paletted textures in WebGL)
export function GL_SetTexturePalette(_r: GLState, _palette: Uint32Array): void {}

// C: gl_image.c:70 GL_EnableMultitexture -- qglSelectTextureSGIS is NULL: returns immediately
export function GL_EnableMultitexture(_r: GLState, _enable: boolean): void {}

// C: gl_image.c:91 GL_SelectTexture -- qglSelectTextureSGIS is NULL: returns immediately
export function GL_SelectTexture(_r: GLState, _texture: number): void {}

// C: gl_image.c:114 GL_TexEnv
export function GL_TexEnv(r: GLState, mode: number): void {
  const lastmodes = r.texEnvLastmodes;
  if (mode !== lastmodes[r.gl_state.currenttmu]) {
    r.qgl?.texEnv(mode);
    lastmodes[r.gl_state.currenttmu] = mode;
  }
}

// C: gl_image.c:125 GL_Bind
export function GL_Bind(r: GLState, texnum: number): void {
  if (r.cv.gl_nobind.value && r.draw_chars) texnum = r.draw_chars.texnum; // performance evaluation option
  if (r.gl_state.currenttextures[r.gl_state.currenttmu] === texnum) return;
  r.gl_state.currenttextures[r.gl_state.currenttmu] = texnum;
  r.qgl?.bindTexture(texnum);
}

// C: gl_image.c:137 GL_MBind
export function GL_MBind(r: GLState, target: number, texnum: number): void {
  GL_SelectTexture(r, target);
  if (r.gl_state.currenttextures[target === 0 ? 0 : 1] === texnum) return;
  GL_Bind(r, texnum);
}

interface GLMode {
  name: string;
  minimize: number;
  maximize: number;
}

// C: gl_image.c:159 modes
const modes: GLMode[] = [
  { name: 'GL_NEAREST', minimize: GL_NEAREST, maximize: GL_NEAREST },
  { name: 'GL_LINEAR', minimize: GL_LINEAR, maximize: GL_LINEAR },
  { name: 'GL_NEAREST_MIPMAP_NEAREST', minimize: GL_NEAREST_MIPMAP_NEAREST, maximize: GL_NEAREST },
  { name: 'GL_LINEAR_MIPMAP_NEAREST', minimize: GL_LINEAR_MIPMAP_NEAREST, maximize: GL_LINEAR },
  { name: 'GL_NEAREST_MIPMAP_LINEAR', minimize: GL_NEAREST_MIPMAP_LINEAR, maximize: GL_NEAREST },
  { name: 'GL_LINEAR_MIPMAP_LINEAR', minimize: GL_LINEAR_MIPMAP_LINEAR, maximize: GL_LINEAR },
];

// C: gl_image.c:176 gl_alpha_modes
const gl_alpha_modes: { name: string; mode: number }[] = [
  { name: 'default', mode: 4 },
  { name: 'GL_RGBA', mode: GL_RGBA },
  { name: 'GL_RGBA8', mode: GL_RGBA8 },
  { name: 'GL_RGB5_A1', mode: GL_RGB5_A1 },
  { name: 'GL_RGBA4', mode: GL_RGBA4 },
  { name: 'GL_RGBA2', mode: GL_RGBA2 },
];

// C: gl_image.c:187 gl_solid_modes
const gl_solid_modes: { name: string; mode: number }[] = [
  { name: 'default', mode: 3 },
  { name: 'GL_RGB', mode: GL_RGB },
  { name: 'GL_RGB8', mode: GL_RGB8 },
  { name: 'GL_RGB5', mode: GL_RGB5 },
  { name: 'GL_RGB4', mode: GL_RGB4 },
  { name: 'GL_R3_G3_B2', mode: GL_R3_G3_B2 },
];

// C: gl_image.c:206 GL_TextureMode
export function GL_TextureMode(r: GLState, string: string): void {
  let i;
  for (i = 0; i < modes.length; i++) {
    if (!Q_stricmp(modes[i]!.name, string)) break;
  }
  if (i === modes.length) {
    r.ri.conPrintf(PRINT_ALL, 'bad filter name\n');
    return;
  }
  r.gl_filter_min = modes[i]!.minimize;
  r.gl_filter_max = modes[i]!.maximize;

  // change all the existing mipmap texture objects
  for (i = 0; i < r.numgltextures; i++) {
    const glt = r.gltextures[i]!;
    if (glt.type !== it_pic && glt.type !== it_sky) {
      GL_Bind(r, glt.texnum);
      r.qgl?.texFilter(r.gl_filter_min, r.gl_filter_max);
    }
  }
}

// C: gl_image.c:243 GL_TextureAlphaMode
export function GL_TextureAlphaMode(r: GLState, string: string): void {
  let i;
  for (i = 0; i < gl_alpha_modes.length; i++) {
    if (!Q_stricmp(gl_alpha_modes[i]!.name, string)) break;
  }
  if (i === gl_alpha_modes.length) {
    r.ri.conPrintf(PRINT_ALL, 'bad alpha texture mode name\n');
    return;
  }
  r.gl_tex_alpha_format = gl_alpha_modes[i]!.mode;
}

// C: gl_image.c:267 GL_TextureSolidMode
export function GL_TextureSolidMode(r: GLState, string: string): void {
  let i;
  for (i = 0; i < gl_solid_modes.length; i++) {
    if (!Q_stricmp(gl_solid_modes[i]!.name, string)) break;
  }
  if (i === gl_solid_modes.length) {
    r.ri.conPrintf(PRINT_ALL, 'bad solid texture mode name\n');
    return;
  }
  r.gl_tex_solid_format = gl_solid_modes[i]!.mode;
}

function pad(s: string | number, n: number): string {
  const t = String(s);
  return t.length >= n ? t : ' '.repeat(n - t.length) + t;
}

// C: gl_image.c:291 GL_ImageList_f
export function GL_ImageList_f(r: GLState): void {
  const palstrings = ['RGB', 'PAL'];
  r.ri.conPrintf(PRINT_ALL, '------------------\n');
  let texels = 0;
  for (let i = 0; i < r.numgltextures; i++) {
    const image = r.gltextures[i]!;
    if (image.texnum <= 0) continue;
    texels += image.upload_width * image.upload_height;
    switch (image.type) {
      case it_skin:
        r.ri.conPrintf(PRINT_ALL, 'M');
        break;
      case it_sprite:
        r.ri.conPrintf(PRINT_ALL, 'S');
        break;
      case it_wall:
        r.ri.conPrintf(PRINT_ALL, 'W');
        break;
      case it_pic:
        r.ri.conPrintf(PRINT_ALL, 'P');
        break;
      default:
        r.ri.conPrintf(PRINT_ALL, ' ');
        break;
    }
    r.ri.conPrintf(
      PRINT_ALL,
      ` ${pad(image.upload_width, 3)} ${pad(image.upload_height, 3)} ${palstrings[image.paletted ? 1 : 0]}: ${image.name}\n`,
    );
  }
  r.ri.conPrintf(PRINT_ALL, `Total texel count (not counting mipmaps): ${texels}\n`);
}

// =============================================================================
// scrap allocation: all the little status bar objects go into a single texture

export const MAX_SCRAPS = 1;
export const SCRAP_BLOCK_WIDTH = 256;
export const SCRAP_BLOCK_HEIGHT = 256;

/**
 * C: gl_image.c:356 Scrap_AllocBlock. Returns the scrap texture number (or -1) and the position.
 * Quirk kept: the column scan stops at BLOCK_WIDTH-w, so the last column is never used.
 */
export function Scrap_AllocBlock(
  allocated: Int32Array[],
  w: number,
  h: number,
  out: { x: number; y: number },
): number {
  for (let texnum = 0; texnum < MAX_SCRAPS; texnum++) {
    const alloc = allocated[texnum]!;
    let best = SCRAP_BLOCK_HEIGHT;
    for (let i = 0; i < SCRAP_BLOCK_WIDTH - w; i++) {
      let best2 = 0;
      let j;
      for (j = 0; j < w; j++) {
        if (alloc[i + j]! >= best) break;
        if (alloc[i + j]! > best2) best2 = alloc[i + j]!;
      }
      if (j === w) {
        // this is a valid spot
        out.x = i;
        out.y = best = best2;
      }
    }
    if (best + h > SCRAP_BLOCK_HEIGHT) continue;
    for (let i = 0; i < w; i++) alloc[out.x + i] = best + h;
    return texnum;
  }
  return -1;
}

// C: gl_image.c:399 Scrap_Upload
export function Scrap_Upload(r: GLState): void {
  r.scrap_uploads++;
  GL_Bind(r, TEXNUM_SCRAPS);
  GL_Upload8(r, r.scrap_texels[0]!, SCRAP_BLOCK_WIDTH, SCRAP_BLOCK_HEIGHT, false, false);
  r.scrap_dirty = false;
}

// =============================================================================
// PCX / TGA loading

export interface LoadedPCX {
  pic: Uint8Array | null;
  palette: Uint8Array | null;
  width: number;
  height: number;
}

// C: gl_image.c:421 LoadPCX (decoding in q2-formats decodePcx). A malformed RLE stream frees the pic but
// still returns the palette, like C.
export function LoadPCX(r: GLState, filename: string): LoadedPCX {
  const raw = r.files.get(filename);
  if (!raw) {
    r.ri.conPrintf(PRINT_DEVELOPER, `Bad pcx file ${filename}\n`);
    return { pic: null, palette: null, width: 0, height: 0 };
  }
  const bad =
    raw.length < PCX_HEADER_SIZE ||
    raw[0] !== 0x0a ||
    raw[1] !== 5 ||
    raw[2] !== 1 ||
    raw[3] !== 8 ||
    (raw[8]! | (raw[9]! << 8)) >= 640 ||
    (raw[10]! | (raw[11]! << 8)) >= 480;
  if (bad) {
    r.ri.conPrintf(PRINT_ALL, `Bad pcx file ${filename}\n`);
    return { pic: null, palette: null, width: 0, height: 0 };
  }
  try {
    const img = decodePcx(raw, filename);
    return { pic: img.pixels, palette: img.palette, width: img.width, height: img.height };
  } catch (e) {
    if (!(e instanceof FormatError)) throw e;
    r.ri.conPrintf(PRINT_DEVELOPER, `PCX file ${filename} was malformed`);
    const palette = raw.length >= 768 ? raw.slice(raw.length - 768) : null;
    return {
      pic: null,
      palette,
      width: (raw[8]! | (raw[9]! << 8)) + 1,
      height: (raw[10]! | (raw[11]! << 8)) + 1,
    };
  }
}

// C: gl_image.c:539 LoadTGA (decoding in q2-formats decodeTga)
export function LoadTGA(r: GLState, name: string): { pic: Uint8Array | null; width: number; height: number } {
  const buffer = r.files.get(name);
  if (!buffer) {
    r.ri.conPrintf(PRINT_DEVELOPER, `Bad tga file ${name}\n`);
    return { pic: null, width: 0, height: 0 };
  }
  try {
    const t = decodeTga(buffer, name);
    return { pic: t.rgba, width: t.width, height: t.height };
  } catch (e) {
    if (!(e instanceof FormatError)) throw e;
    r.ri.sysError(ERR_DROP, e.message);
  }
}

// =============================================================================
// IMAGE FLOOD FILLING

const FLOODFILL_FIFO_SIZE = 0x1000;
const FLOODFILL_FIFO_MASK = FLOODFILL_FIFO_SIZE - 1;

/**
 * C: gl_image.c:761 R_FloodFillSkin -- fill background pixels so mipmapping doesn't have haloes.
 * Quirk kept: the "opaque black" search compares d_8to24table entries with 255 (0x000000ff), which never
 * matches a real palette, so the filled colour is always 0. The FIFO silently wraps like C.
 */
export function R_FloodFillSkin(
  skin: Uint8Array,
  skinwidth: number,
  skinheight: number,
  d_8to24table: Uint32Array,
): void {
  const fillcolor = skin[0]!; // assume this is the pixel to fill
  const fifoX = new Int16Array(FLOODFILL_FIFO_SIZE);
  const fifoY = new Int16Array(FLOODFILL_FIFO_SIZE);
  let inpt = 0;
  let outpt = 0;
  let filledcolor = -1;

  if (filledcolor === -1) {
    filledcolor = 0;
    // attempt to find opaque black
    for (let i = 0; i < 256; ++i) {
      if (d_8to24table[i] === 255 << 0) {
        filledcolor = i;
        break;
      }
    }
  }

  // can't fill to filled color or to transparent color (used as visited marker)
  if (fillcolor === filledcolor || fillcolor === 255) return;

  fifoX[inpt] = 0;
  fifoY[inpt] = 0;
  inpt = (inpt + 1) & FLOODFILL_FIFO_MASK;

  while (outpt !== inpt) {
    const x = fifoX[outpt]!;
    const y = fifoY[outpt]!;
    let fdc = filledcolor;
    const pos = x + skinwidth * y;
    outpt = (outpt + 1) & FLOODFILL_FIFO_MASK;

    const step = (off: number, dx: number, dy: number): void => {
      const p = skin[pos + off]!;
      if (p === fillcolor) {
        skin[pos + off] = 255;
        fifoX[inpt] = x + dx;
        fifoY[inpt] = y + dy;
        inpt = (inpt + 1) & FLOODFILL_FIFO_MASK;
      } else if (p !== 255) fdc = p;
    };
    if (x > 0) step(-1, -1, 0);
    if (x < skinwidth - 1) step(1, 1, 0);
    if (y > 0) step(-skinwidth, 0, -1);
    if (y < skinheight - 1) step(skinwidth, 0, 1);
    skin[pos] = fdc;
  }
}

// =======================================================

/**
 * C: gl_image.c:815 GL_ResampleTexture. `inp` / `out` are RGBA byte images (C: unsigned words).
 * fracstep uses 32-bit unsigned arithmetic, the row selection is computed in double like C.
 */
export function GL_ResampleTexture(
  inp: Uint8Array,
  inwidth: number,
  inheight: number,
  out: Uint8Array,
  outwidth: number,
  outheight: number,
): void {
  const p1 = new Uint32Array(1024);
  const p2 = new Uint32Array(1024);
  const fracstep = Math.floor(((inwidth * 0x10000) >>> 0) / outwidth) >>> 0;

  let frac = fracstep >>> 2;
  for (let i = 0; i < outwidth; i++) {
    p1[i] = 4 * (frac >>> 16);
    frac = (frac + fracstep) >>> 0;
  }
  frac = (3 * (fracstep >>> 2)) >>> 0;
  for (let i = 0; i < outwidth; i++) {
    p2[i] = 4 * (frac >>> 16);
    frac = (frac + fracstep) >>> 0;
  }

  let o = 0;
  for (let i = 0; i < outheight; i++, o += outwidth * 4) {
    const inrow = inwidth * Math.trunc(((i + 0.25) * inheight) / outheight) * 4;
    const inrow2 = inwidth * Math.trunc(((i + 0.75) * inheight) / outheight) * 4;
    for (let j = 0; j < outwidth; j++) {
      const pix1 = inrow + p1[j]!;
      const pix2 = inrow + p2[j]!;
      const pix3 = inrow2 + p1[j]!;
      const pix4 = inrow2 + p2[j]!;
      const d = o + j * 4;
      out[d] = (inp[pix1]! + inp[pix2]! + inp[pix3]! + inp[pix4]!) >> 2;
      out[d + 1] = (inp[pix1 + 1]! + inp[pix2 + 1]! + inp[pix3 + 1]! + inp[pix4 + 1]!) >> 2;
      out[d + 2] = (inp[pix1 + 2]! + inp[pix2 + 2]! + inp[pix3 + 2]! + inp[pix4 + 2]!) >> 2;
      out[d + 3] = (inp[pix1 + 3]! + inp[pix2 + 3]! + inp[pix3 + 3]! + inp[pix4 + 3]!) >> 2;
    }
  }
}

/** C: gl_image.c:865 GL_LightScaleTexture -- scale up the pixel values to increase the lighting range. */
export function GL_LightScaleTexture(
  data: Uint8Array,
  inwidth: number,
  inheight: number,
  only_gamma: boolean,
  gammatable: Uint8Array,
  intensitytable: Uint8Array,
): void {
  const c = inwidth * inheight;
  if (only_gamma) {
    for (let i = 0, p = 0; i < c; i++, p += 4) {
      data[p] = gammatable[data[p]!]!;
      data[p + 1] = gammatable[data[p + 1]!]!;
      data[p + 2] = gammatable[data[p + 2]!]!;
    }
  } else {
    for (let i = 0, p = 0; i < c; i++, p += 4) {
      data[p] = gammatable[intensitytable[data[p]!]!]!;
      data[p + 1] = gammatable[intensitytable[data[p + 1]!]!]!;
      data[p + 2] = gammatable[intensitytable[data[p + 2]!]!]!;
    }
  }
}

/** C: gl_image.c:906 GL_MipMap -- operates in place, quartering the size of the texture. */
export function GL_MipMap(data: Uint8Array, width: number, height: number): void {
  width <<= 2;
  height >>= 1;
  let out = 0;
  let inp = 0;
  for (let i = 0; i < height; i++, inp += width) {
    for (let j = 0; j < width; j += 8, out += 4, inp += 8) {
      data[out] = (data[inp]! + data[inp + 4]! + data[inp + width]! + data[inp + width + 4]!) >> 2;
      data[out + 1] =
        (data[inp + 1]! + data[inp + 5]! + data[inp + width + 1]! + data[inp + width + 5]!) >> 2;
      data[out + 2] =
        (data[inp + 2]! + data[inp + 6]! + data[inp + width + 2]! + data[inp + width + 6]!) >> 2;
      data[out + 3] =
        (data[inp + 3]! + data[inp + 7]! + data[inp + width + 3]! + data[inp + width + 7]!) >> 2;
    }
  }
}

/** Receives each qglTexImage2D call of GL_Upload32 (level, internal format, size, RGBA pixels). */
export type TexImageSink = (
  level: number,
  internalFormat: number,
  w: number,
  h: number,
  pixels: Uint8Array,
) => void;

export interface Upload32Params {
  gl_round_down: number;
  gl_picmip: number;
  gl_solid_format: number;
  gl_alpha_format: number;
  gl_tex_solid_format: number;
  gl_tex_alpha_format: number;
  gammatable: Uint8Array;
  intensitytable: Uint8Array;
}

export interface Upload32Result {
  has_alpha: boolean;
  upload_width: number;
  upload_height: number;
}

/**
 * C: gl_image.c:956 GL_Upload32 without the GL side effects: computes the power-of-two size (gl_round_down,
 * gl_picmip, 256 cap), resamples, light-scales and box-filters every mip level, handing each level to `sink`.
 * `data` (RGBA) is not modified when it can be uploaded directly, like C.
 */
export function GL_Upload32Levels(
  data: Uint8Array,
  width: number,
  height: number,
  mipmap: boolean,
  p: Upload32Params,
  sink: TexImageSink,
  sysError: (msg: string) => never,
): Upload32Result {
  let scaled_width;
  let scaled_height;
  for (scaled_width = 1; scaled_width < width; scaled_width <<= 1);
  if (p.gl_round_down && scaled_width > width && mipmap) scaled_width >>= 1;
  for (scaled_height = 1; scaled_height < height; scaled_height <<= 1);
  if (p.gl_round_down && scaled_height > height && mipmap) scaled_height >>= 1;

  // let people sample down the world textures for speed
  if (mipmap) {
    scaled_width >>= Math.trunc(p.gl_picmip);
    scaled_height >>= Math.trunc(p.gl_picmip);
  }

  // don't ever bother with >256 textures
  if (scaled_width > 256) scaled_width = 256;
  if (scaled_height > 256) scaled_height = 256;
  if (scaled_width < 1) scaled_width = 1;
  if (scaled_height < 1) scaled_height = 1;

  const upload_width = scaled_width;
  const upload_height = scaled_height;

  if (scaled_width * scaled_height > 256 * 256) sysError('GL_Upload32: too big');

  // scan the texture for any non-255 alpha
  const c = width * height;
  let samples = p.gl_solid_format;
  for (let i = 0, scan = 3; i < c; i++, scan += 4) {
    if (data[scan] !== 255) {
      samples = p.gl_alpha_format;
      break;
    }
  }

  let comp;
  if (samples === p.gl_solid_format) comp = p.gl_tex_solid_format;
  else if (samples === p.gl_alpha_format) comp = p.gl_tex_alpha_format;
  else comp = samples;

  const scaled = new Uint8Array(256 * 256 * 4);
  if (scaled_width === width && scaled_height === height) {
    if (!mipmap) {
      sink(0, comp, scaled_width, scaled_height, data);
      return { has_alpha: samples === p.gl_alpha_format, upload_width, upload_height };
    }
    scaled.set(data.subarray(0, width * height * 4));
  } else GL_ResampleTexture(data, width, height, scaled, scaled_width, scaled_height);

  GL_LightScaleTexture(scaled, scaled_width, scaled_height, !mipmap, p.gammatable, p.intensitytable);

  sink(0, comp, scaled_width, scaled_height, scaled.subarray(0, scaled_width * scaled_height * 4));

  if (mipmap) {
    let miplevel = 0;
    while (scaled_width > 1 || scaled_height > 1) {
      GL_MipMap(scaled, scaled_width, scaled_height);
      scaled_width >>= 1;
      scaled_height >>= 1;
      if (scaled_width < 1) scaled_width = 1;
      if (scaled_height < 1) scaled_height = 1;
      miplevel++;
      sink(miplevel, comp, scaled_width, scaled_height, scaled.subarray(0, scaled_width * scaled_height * 4));
    }
  }
  return { has_alpha: samples === p.gl_alpha_format, upload_width, upload_height };
}

// C: gl_image.c:956 GL_Upload32
export function GL_Upload32(
  r: GLState,
  data: Uint8Array,
  width: number,
  height: number,
  mipmap: boolean,
): boolean {
  r.uploaded_paletted = false;
  const qgl = r.qgl;
  const res = GL_Upload32Levels(
    data,
    width,
    height,
    mipmap,
    {
      gl_round_down: r.cv.gl_round_down.value,
      gl_picmip: r.cv.gl_picmip.value,
      gl_solid_format: r.gl_solid_format,
      gl_alpha_format: r.gl_alpha_format,
      gl_tex_solid_format: r.gl_tex_solid_format,
      gl_tex_alpha_format: r.gl_tex_alpha_format,
      gammatable: r.gammatable,
      intensitytable: r.intensitytable,
    },
    (level, comp, w, h, pixels) => qgl?.texImage2D(level, comp, w, h, pixels),
    (msg) => r.ri.sysError(ERR_DROP, msg),
  );
  r.upload_width = res.upload_width;
  r.upload_height = res.upload_height;

  if (mipmap) qgl?.texFilter(r.gl_filter_min, r.gl_filter_max);
  else qgl?.texFilter(r.gl_filter_max, r.gl_filter_max);
  return res.has_alpha;
}

/**
 * C: gl_image.c:1165 GL_Upload8, the palette expansion part: index 255 is transparent and takes the colour
 * of the first non-transparent neighbour (up, down, left, right) to avoid alpha fringes.
 */
export function GL_Expand8(
  data: Uint8Array,
  width: number,
  height: number,
  d_8to24table: Uint32Array,
  trans: Uint8Array,
): void {
  const s = width * height;
  for (let i = 0; i < s; i++) {
    let p = data[i]!;
    const v = d_8to24table[p]!;
    const o = i * 4;
    trans[o] = v & 255;
    trans[o + 1] = (v >>> 8) & 255;
    trans[o + 2] = (v >>> 16) & 255;
    trans[o + 3] = v >>> 24;
    if (p === 255) {
      // transparent, so scan around for another color to avoid alpha fringes
      if (i > width && data[i - width] !== 255) p = data[i - width]!;
      else if (i < s - width && data[i + width] !== 255) p = data[i + width]!;
      else if (i > 0 && data[i - 1] !== 255) p = data[i - 1]!;
      else if (i < s - 1 && data[i + 1] !== 255) p = data[i + 1]!;
      else p = 0;
      // copy rgb components
      const c = d_8to24table[p]!;
      trans[o] = c & 255;
      trans[o + 1] = (c >>> 8) & 255;
      trans[o + 2] = (c >>> 16) & 255;
    }
  }
}

// C: gl_image.c:1165 GL_Upload8 (the GL_COLOR_INDEX8_EXT sky branch needs qglColorTableEXT: never taken)
export function GL_Upload8(
  r: GLState,
  data: Uint8Array,
  width: number,
  height: number,
  mipmap: boolean,
  _is_sky: boolean,
): boolean {
  const s = width * height;
  if (s > 512 * 256) r.ri.sysError(ERR_DROP, 'GL_Upload8: too large');
  const trans = new Uint8Array(s * 4);
  GL_Expand8(data, width, height, r.d_8to24table, trans);
  return GL_Upload32(r, trans, width, height, mipmap);
}

const scrapPos = { x: 0, y: 0 };

// C: gl_image.c:1233 GL_LoadPic -- also used as an entry point for the generated r_notexture
export function GL_LoadPic(
  r: GLState,
  name: string,
  pic: Uint8Array,
  width: number,
  height: number,
  type: number,
  bits: number,
): Image {
  // find a free image_t
  let i;
  for (i = 0; i < r.numgltextures; i++) {
    if (!r.gltextures[i]!.texnum) break;
  }
  if (i === r.numgltextures) {
    if (r.numgltextures === MAX_GLTEXTURES) r.ri.sysError(ERR_DROP, 'MAX_GLTEXTURES');
    r.numgltextures++;
  }
  const image = r.gltextures[i]!;

  if (name.length >= 64) r.ri.sysError(ERR_DROP, `Draw_LoadPic: "${name}" is too long`);
  image.name = name;
  image.registration_sequence = r.registration_sequence;
  image.width = width;
  image.height = height;
  image.type = type;
  image.texturechain = null;

  if (type === it_skin && bits === 8) R_FloodFillSkin(pic, width, height, r.d_8to24table);

  // load little pics into the scrap
  let scrapped = false;
  if (image.type === it_pic && bits === 8 && image.width < 64 && image.height < 64) {
    const texnum = Scrap_AllocBlock(r.scrap_allocated, image.width, image.height, scrapPos);
    if (texnum !== -1) {
      r.scrap_dirty = true;
      const x = scrapPos.x;
      const y = scrapPos.y;
      // copy the texels into the scrap block
      let k = 0;
      const texels = r.scrap_texels[texnum]!;
      for (let ii = 0; ii < image.height; ii++) {
        for (let j = 0; j < image.width; j++, k++) texels[(y + ii) * SCRAP_BLOCK_WIDTH + x + j] = pic[k]!;
      }
      image.texnum = TEXNUM_SCRAPS + texnum;
      image.scrap = true;
      image.has_alpha = true;
      image.sl = fr((x + 0.01) / fr(SCRAP_BLOCK_WIDTH));
      image.sh = fr((x + image.width - 0.01) / fr(SCRAP_BLOCK_WIDTH));
      image.tl = fr((y + 0.01) / fr(SCRAP_BLOCK_WIDTH));
      image.th = fr((y + image.height - 0.01) / fr(SCRAP_BLOCK_WIDTH));
      scrapped = true;
    }
  }
  if (!scrapped) {
    // nonscrap:
    image.scrap = false;
    image.texnum = TEXNUM_IMAGES + i;
    GL_Bind(r, image.texnum);
    const mip = image.type !== it_pic && image.type !== it_sky;
    if (bits === 8) image.has_alpha = GL_Upload8(r, pic, width, height, mip, image.type === it_sky);
    else image.has_alpha = GL_Upload32(r, pic, width, height, mip);
    image.upload_width = r.upload_width; // after power of 2 and scales
    image.upload_height = r.upload_height;
    image.paletted = r.uploaded_paletted;
    image.sl = 0;
    image.sh = 1;
    image.tl = 0;
    image.th = 1;
  }
  return image;
}

// C: gl_image.c:1318 GL_LoadWal
export function GL_LoadWal(r: GLState, name: string): Image {
  const mt = r.files.get(name);
  if (!mt) {
    r.ri.conPrintf(PRINT_ALL, `GL_FindImage: can't load ${name}\n`);
    return r.r_notexture;
  }
  let wal;
  try {
    wal = parseWal(mt, name);
  } catch (e) {
    if (!(e instanceof FormatError)) throw e;
    r.ri.sysError(ERR_DROP, e.message);
  }
  return GL_LoadPic(r, name, wal.mips[0]!, wal.width, wal.height, it_wall, 8);
}

// C: gl_image.c:1349 GL_FindImage -- finds or loads the given image
export function GL_FindImage(r: GLState, name: string | null, type: number): Image | null {
  if (!name) return null;
  const len = name.length;
  if (len < 5) return null;

  // look for it
  for (let i = 0; i < r.numgltextures; i++) {
    const image = r.gltextures[i]!;
    if (name === image.name) {
      image.registration_sequence = r.registration_sequence;
      return image;
    }
  }

  // load the pic from disk
  const ext = name.slice(len - 4);
  let image: Image | null;
  if (ext === '.pcx') {
    const p = LoadPCX(r, name);
    if (!p.pic) return null;
    image = GL_LoadPic(r, name, p.pic, p.width, p.height, type, 8);
  } else if (ext === '.wal') {
    image = GL_LoadWal(r, name);
  } else if (ext === '.tga') {
    const t = LoadTGA(r, name);
    if (!t.pic) return null;
    image = GL_LoadPic(r, name, t.pic, t.width, t.height, type, 32);
  } else return null;
  return image;
}

/** File names GL_FindImage would read for `name` (for prefetching). */
export function imageFileFor(name: string | null): string | null {
  if (!name || name.length < 5) return null;
  const ext = COM_FileExtension(name);
  return ext === 'pcx' || ext === 'wal' || ext === 'tga' ? name : null;
}

// C: gl_image.c:1414 R_RegisterSkin
export function R_RegisterSkin(r: GLState, name: string): Image | null {
  return GL_FindImage(r, name, it_skin);
}

// C: gl_image.c:1428 GL_FreeUnusedImages -- any image not touched on this registration sequence is freed
export function GL_FreeUnusedImages(r: GLState): void {
  // never free r_notexture or particle texture
  r.r_notexture.registration_sequence = r.registration_sequence;
  r.r_particletexture.registration_sequence = r.registration_sequence;

  for (let i = 0; i < r.numgltextures; i++) {
    const image = r.gltextures[i]!;
    if (image.registration_sequence === r.registration_sequence) continue; // used this sequence
    if (!image.registration_sequence) continue; // free image_t slot
    if (image.type === it_pic) continue; // don't free pics
    // free it
    r.qgl?.deleteTexture(image.texnum);
    image.clear();
  }
}

// C: gl_image.c:1457 Draw_GetPalette
export function Draw_GetPalette(r: GLState): number {
  const p = LoadPCX(r, 'pics/colormap.pcx');
  if (!p.palette) r.ri.sysError(ERR_FATAL, "Couldn't load pics/colormap.pcx");
  const pal = p.palette;
  for (let i = 0; i < 256; i++) {
    const cr = pal[i * 3]!;
    const cg = pal[i * 3 + 1]!;
    const cb = pal[i * 3 + 2]!;
    r.d_8to24table[i] = ((255 << 24) >>> 0) + (cr << 0) + (cg << 8) + (cb << 16);
  }
  r.d_8to24table[255] = r.d_8to24table[255]! & 0xffffff; // 255 is transparent
  return 0;
}

/** C: gl_image.c:1524 gammatable build loop (float `inf`, pow in double). */
export function buildGammaTable(g: number, out: Uint8Array): void {
  for (let i = 0; i < 256; i++) {
    if (g === 1) out[i] = i;
    else {
      let inf = fr(255 * Math.pow((i + 0.5) / 255.5, g) + 0.5);
      if (inf < 0) inf = 0;
      if (inf > 255) inf = 255;
      out[i] = Math.trunc(inf);
    }
  }
}

/** C: gl_image.c:1543 intensitytable build loop (j = i*intensity->value, int truncation). */
export function buildIntensityTable(intensity: number, out: Uint8Array): void {
  for (let i = 0; i < 256; i++) {
    let j = Math.trunc(fr(i * intensity));
    if (j > 255) j = 255;
    out[i] = j;
  }
}

// C: gl_image.c:1495 GL_InitImages
export function GL_InitImages(r: GLState): void {
  let g = fr(r.cv.vid_gamma.value);
  r.registration_sequence = 1;

  // init intensity conversions
  r.cv.intensity = r.ri.cvarGet('intensity', '2', 0);
  if (r.cv.intensity.value <= 1) r.ri.cvarSet('intensity', '1');
  r.gl_state.inverse_intensity = fr(1 / r.cv.intensity.value);

  Draw_GetPalette(r);

  if (r.gl_config.renderer & (GL_RENDERER_VOODOO | GL_RENDERER_VOODOO2)) g = 1.0;

  buildGammaTable(g, r.gammatable);
  buildIntensityTable(r.cv.intensity.value, r.intensitytable);
}

// C: gl_image.c:1557 GL_ShutdownImages
export function GL_ShutdownImages(r: GLState): void {
  for (let i = 0; i < r.numgltextures; i++) {
    const image = r.gltextures[i]!;
    if (!image.registration_sequence) continue; // free image_t slot
    r.qgl?.deleteTexture(image.texnum);
    image.clear();
  }
}
