// PCX decoder, a port of ref_gl/gl_image.c LoadPCX (identical to client/cl_cin.c SCR_LoadPCX).
import { toU8 } from './bytes';
import { FormatError } from './errors';

export interface PcxImage {
  width: number;
  height: number;
  /** width*height 8-bit palette indices */
  pixels: Uint8Array;
  /** 768-byte RGB palette: always the last 768 bytes of the file */
  palette: Uint8Array;
}

export const PCX_HEADER_SIZE = 128;

// C: ref_gl/gl_image.c:421 LoadPCX
// The C function returns a NULL pic (after printing) when the header check fails or the RLE data
// overran the file; here both cases throw FormatError.
// Quirks reproduced: width/height come from xmax/ymax only (xmin/ymin ignored); a run crossing the end of
// a row spills into the next row (and is then overwritten by that row's own data); the palette is taken
// from the last 768 bytes of the file regardless of where the image data ends; reading exactly up to the
// end of the file is fine, reading past it marks the file malformed.
export function decodePcx(data: ArrayBuffer | Uint8Array, name = 'pcx'): PcxImage {
  const raw = toU8(data);
  const len = raw.length;
  if (len < PCX_HEADER_SIZE) throw new FormatError(`Bad pcx file ${name}`);
  const dv = new DataView(raw.buffer, raw.byteOffset, raw.byteLength);
  const manufacturer = (raw[0]! << 24) >> 24;
  const version = (raw[1]! << 24) >> 24;
  const encoding = (raw[2]! << 24) >> 24;
  const bits_per_pixel = (raw[3]! << 24) >> 24;
  const xmax = dv.getUint16(8, true);
  const ymax = dv.getUint16(10, true);
  if (
    manufacturer !== 0x0a ||
    version !== 5 ||
    encoding !== 1 ||
    bits_per_pixel !== 8 ||
    xmax >= 640 ||
    ymax >= 480
  ) {
    throw new FormatError(`Bad pcx file ${name}`);
  }
  if (len < 768) throw new FormatError(`Bad pcx file ${name}`);
  const width = xmax + 1;
  const height = ymax + 1;
  const out = new Uint8Array(width * height);
  const palette = raw.slice(len - 768, len);

  let p = PCX_HEADER_SIZE;
  let overread = false;
  const next = (): number => {
    if (p >= len) {
      overread = true;
      p++;
      return 0;
    }
    return raw[p++]!;
  };
  let pix = 0;
  for (let y = 0; y <= ymax; y++, pix += width) {
    for (let x = 0; x <= xmax;) {
      let dataByte = next();
      let runLength: number;
      if ((dataByte & 0xc0) === 0xc0) {
        runLength = dataByte & 0x3f;
        dataByte = next();
      } else runLength = 1;
      while (runLength-- > 0) {
        const o = pix + x++;
        if (o < out.length) out[o] = dataByte;
      }
      if (overread) throw new FormatError(`PCX file ${name} was malformed`);
    }
  }
  if (p > len) throw new FormatError(`PCX file ${name} was malformed`);
  return { width, height, pixels: out, palette };
}

/** Expand 8-bit pixels through a 768-byte palette into RGBA (alpha 255, index 255 transparent if asked). */
export function pcxToRgba(img: PcxImage, transparent255 = false): Uint8Array {
  const out = new Uint8Array(img.width * img.height * 4);
  for (let i = 0; i < img.pixels.length; i++) {
    const c = img.pixels[i]!;
    out[i * 4] = img.palette[c * 3]!;
    out[i * 4 + 1] = img.palette[c * 3 + 1]!;
    out[i * 4 + 2] = img.palette[c * 3 + 2]!;
    out[i * 4 + 3] = transparent255 && c === 255 ? 0 : 255;
  }
  return out;
}
