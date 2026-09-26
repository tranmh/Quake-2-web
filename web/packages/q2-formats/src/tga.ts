// TGA decoder, a port of ref_gl/gl_image.c LoadTGA.
import { toU8 } from './bytes';
import { FormatError } from './errors';

export interface TgaHeader {
  id_length: number;
  colormap_type: number;
  image_type: number;
  colormap_index: number;
  colormap_length: number;
  colormap_size: number;
  x_origin: number;
  y_origin: number;
  width: number;
  height: number;
  pixel_size: number;
  attributes: number;
}

export interface TgaImage {
  header: TgaHeader;
  width: number;
  height: number;
  /** width*height*4 RGBA, top row first */
  rgba: Uint8Array;
}

// C: ref_gl/gl_image.c:539 LoadTGA
// Quirks reproduced: the attributes byte (incl. the top-left origin bit 0x20) is ignored -- pixel rows are
// always treated as stored bottom-up; RLE packets may span rows, and data after the last pixel of the
// top row is ignored. Sys_Error cases throw FormatError; truncated data throws FormatError (C over-reads).
export function decodeTga(data: ArrayBuffer | Uint8Array, name = 'tga'): TgaImage {
  const b = toU8(data);
  let p = 0;
  const u8 = (): number => {
    if (p >= b.length) throw new FormatError(`${name}: truncated tga`);
    return b[p++]!;
  };
  const u16 = (): number => u8() | (u8() << 8);
  const header: TgaHeader = {
    id_length: u8(),
    colormap_type: u8(),
    image_type: u8(),
    colormap_index: u16(),
    colormap_length: u16(),
    colormap_size: u8(),
    x_origin: u16(),
    y_origin: u16(),
    width: u16(),
    height: u16(),
    pixel_size: u8(),
    attributes: u8(),
  };
  if (header.image_type !== 2 && header.image_type !== 10) {
    throw new FormatError('LoadTGA: Only type 2 and 10 targa RGB images supported');
  }
  if (header.colormap_type !== 0 || (header.pixel_size !== 32 && header.pixel_size !== 24)) {
    throw new FormatError('LoadTGA: Only 32 or 24 bit images supported (no colormaps)');
  }
  const columns = header.width;
  const rows = header.height;
  p += header.id_length;
  const bpp32 = header.pixel_size === 32;
  // Reject files too short for the pixel count before allocating (memory safety: an 18-byte file may
  // claim 65535x65535 pixels). Such files would throw "truncated" below anyway. Uncompressed data needs
  // bpp bytes per pixel; an RLE packet covers at most 128 pixels and costs at least 1 + bpp bytes.
  const bytesPerPixel = bpp32 ? 4 : 3;
  const pixels = columns * rows;
  const minBytes =
    header.image_type === 2 ? pixels * bytesPerPixel : Math.ceil(pixels / 128) * (1 + bytesPerPixel);
  if (b.length - p < minBytes) throw new FormatError(`${name}: truncated tga`);
  const rgba = new Uint8Array(pixels * 4);

  if (header.image_type === 2) {
    for (let row = rows - 1; row >= 0; row--) {
      let pix = row * columns * 4;
      for (let column = 0; column < columns; column++) {
        const blue = u8();
        const green = u8();
        const red = u8();
        const alpha = bpp32 ? u8() : 255;
        rgba[pix++] = red;
        rgba[pix++] = green;
        rgba[pix++] = blue;
        rgba[pix++] = alpha;
      }
    }
  } else {
    let red = 0,
      green = 0,
      blue = 0,
      alpha = 0;
    outer: for (let row = rows - 1; row >= 0; row--) {
      let pix = row * columns * 4;
      for (let column = 0; column < columns;) {
        const packetHeader = u8();
        const packetSize = 1 + (packetHeader & 0x7f);
        if (packetHeader & 0x80) {
          blue = u8();
          green = u8();
          red = u8();
          alpha = bpp32 ? u8() : 255;
          for (let j = 0; j < packetSize; j++) {
            rgba[pix++] = red;
            rgba[pix++] = green;
            rgba[pix++] = blue;
            rgba[pix++] = alpha;
            column++;
            if (column === columns) {
              column = 0;
              if (row > 0) row--;
              else break outer;
              pix = row * columns * 4;
            }
          }
        } else {
          for (let j = 0; j < packetSize; j++) {
            blue = u8();
            green = u8();
            red = u8();
            alpha = bpp32 ? u8() : 255;
            rgba[pix++] = red;
            rgba[pix++] = green;
            rgba[pix++] = blue;
            rgba[pix++] = alpha;
            column++;
            if (column === columns) {
              column = 0;
              if (row > 0) row--;
              else break outer;
              pix = row * columns * 4;
            }
          }
        }
      }
    }
  }
  return { header, width: columns, height: rows, rgba };
}
