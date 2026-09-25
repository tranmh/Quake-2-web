// Port of ref_gl/gl_draw.c: 2D drawing.
// Pics are looked up by name synchronously like C; a pic whose file has not been fetched yet (the
// browser cannot block on I/O) triggers an asynchronous fetch and is skipped until it arrives.
import { ERR_FATAL, PRINT_ALL, fr } from 'q2-shared';
import { GLState, GL_RENDERER_MCD, GL_RENDERER_RENDITION, Image, it_pic } from './gl_local';
import { GL_Bind, GL_FindImage, Scrap_Upload } from './gl_image';
import { GL_ALPHA_TEST, GL_BLEND, GL_NEAREST, GL_QUADS, GL_TEXTURE_2D } from './qgl';

// C: gl_draw.c:36 Draw_InitLocal
export function Draw_InitLocal(r: GLState): void {
  // load console characters (don't bilerp characters)
  r.draw_chars = GL_FindImage(r, 'pics/conchars.pcx', it_pic);
  if (!r.draw_chars) r.ri.sysError(ERR_FATAL, "Couldn't load pics/conchars.pcx"); // port: C dereferences NULL
  GL_Bind(r, r.draw_chars.texnum);
  r.qgl?.texFilter(GL_NEAREST, GL_NEAREST);
}

// C: gl_draw.c:56 Draw_Char -- draws one 8*8 graphics character with 0 being transparent
export function Draw_Char(r: GLState, x: number, y: number, num: number): void {
  num &= 255;
  if ((num & 127) === 32) return; // space
  if (y <= -8) return; // totally off screen
  const qgl = r.qgl;
  if (!qgl || !r.draw_chars) return;

  const row = num >> 4;
  const col = num & 15;
  const frow = fr(row * 0.0625);
  const fcol = fr(col * 0.0625);
  const size = fr(0.0625);

  GL_Bind(r, r.draw_chars.texnum);
  qgl.begin(GL_QUADS);
  qgl.texCoord2f(fcol, frow);
  qgl.vertex2f(x, y);
  qgl.texCoord2f(fr(fcol + size), frow);
  qgl.vertex2f(x + 8, y);
  qgl.texCoord2f(fr(fcol + size), fr(frow + size));
  qgl.vertex2f(x + 8, y + 8);
  qgl.texCoord2f(fcol, fr(frow + size));
  qgl.vertex2f(x, y + 8);
  qgl.end();
}

/** File name Draw_FindPic resolves `name` to. */
export function picFileName(name: string): string {
  if (name[0] !== '/' && name[0] !== '\\') return `pics/${name}.pcx`.slice(0, 63);
  return name.slice(1);
}

/** Port: true while the pic's file is being fetched (it was never requested before: request it). */
export function picPending(r: GLState, name: string): boolean {
  const full = picFileName(name);
  for (let i = 0; i < r.numgltextures; i++) if (r.gltextures[i]!.name === full) return false;
  if (r.files.get(full) !== undefined) return false;
  r.requestFile?.(full);
  return true;
}

// C: gl_draw.c:95 Draw_FindPic
export function Draw_FindPic(r: GLState, name: string): Image | null {
  return GL_FindImage(r, picFileName(name), it_pic);
}

// C: gl_draw.c:116 Draw_GetPicSize. Port: [0, 0] while the file is still being fetched.
export function Draw_GetPicSize(r: GLState, pic: string): [number, number] {
  if (picPending(r, pic)) return [0, 0];
  const gl = Draw_FindPic(r, pic);
  if (!gl) return [-1, -1];
  return [gl.width, gl.height];
}

function alphaTestHack(r: GLState, gl: Image): boolean {
  return (
    (r.gl_config.renderer === GL_RENDERER_MCD || (r.gl_config.renderer & GL_RENDERER_RENDITION) !== 0) &&
    !gl.has_alpha
  );
}

// C: gl_draw.c:135 Draw_StretchPic
export function Draw_StretchPic(r: GLState, x: number, y: number, w: number, h: number, pic: string): void {
  if (picPending(r, pic)) return;
  const gl = Draw_FindPic(r, pic);
  if (!gl) {
    r.ri.conPrintf(PRINT_ALL, `Can't find pic: ${pic}\n`);
    return;
  }
  if (r.scrap_dirty) Scrap_Upload(r);
  const qgl = r.qgl;
  if (!qgl) return;
  const hack = alphaTestHack(r, gl);
  if (hack) qgl.disable(GL_ALPHA_TEST);
  GL_Bind(r, gl.texnum);
  qgl.begin(GL_QUADS);
  qgl.texCoord2f(gl.sl, gl.tl);
  qgl.vertex2f(x, y);
  qgl.texCoord2f(gl.sh, gl.tl);
  qgl.vertex2f(x + w, y);
  qgl.texCoord2f(gl.sh, gl.th);
  qgl.vertex2f(x + w, y + h);
  qgl.texCoord2f(gl.sl, gl.th);
  qgl.vertex2f(x, y + h);
  qgl.end();
  if (hack) qgl.enable(GL_ALPHA_TEST);
}

// C: gl_draw.c:174 Draw_Pic
export function Draw_Pic(r: GLState, x: number, y: number, pic: string): void {
  if (picPending(r, pic)) return;
  const gl = Draw_FindPic(r, pic);
  if (!gl) {
    r.ri.conPrintf(PRINT_ALL, `Can't find pic: ${pic}\n`);
    return;
  }
  if (r.scrap_dirty) Scrap_Upload(r);
  const qgl = r.qgl;
  if (!qgl) return;
  const hack = alphaTestHack(r, gl);
  if (hack) qgl.disable(GL_ALPHA_TEST);
  GL_Bind(r, gl.texnum);
  qgl.begin(GL_QUADS);
  qgl.texCoord2f(gl.sl, gl.tl);
  qgl.vertex2f(x, y);
  qgl.texCoord2f(gl.sh, gl.tl);
  qgl.vertex2f(x + gl.width, y);
  qgl.texCoord2f(gl.sh, gl.th);
  qgl.vertex2f(x + gl.width, y + gl.height);
  qgl.texCoord2f(gl.sl, gl.th);
  qgl.vertex2f(x, y + gl.height);
  qgl.end();
  if (hack) qgl.enable(GL_ALPHA_TEST);
}

// C: gl_draw.c:214 Draw_TileClear -- repeats a 64*64 tile graphic to fill the screen around a sized down refresh window
export function Draw_TileClear(r: GLState, x: number, y: number, w: number, h: number, pic: string): void {
  if (picPending(r, pic)) return;
  const image = Draw_FindPic(r, pic);
  if (!image) {
    r.ri.conPrintf(PRINT_ALL, `Can't find pic: ${pic}\n`);
    return;
  }
  const qgl = r.qgl;
  if (!qgl) return;
  const hack = alphaTestHack(r, image);
  if (hack) qgl.disable(GL_ALPHA_TEST);
  GL_Bind(r, image.texnum);
  qgl.begin(GL_QUADS);
  qgl.texCoord2f(x / 64.0, y / 64.0);
  qgl.vertex2f(x, y);
  qgl.texCoord2f((x + w) / 64.0, y / 64.0);
  qgl.vertex2f(x + w, y);
  qgl.texCoord2f((x + w) / 64.0, (y + h) / 64.0);
  qgl.vertex2f(x + w, y + h);
  qgl.texCoord2f(x / 64.0, (y + h) / 64.0);
  qgl.vertex2f(x, y + h);
  qgl.end();
  if (hack) qgl.enable(GL_ALPHA_TEST);
}

// C: gl_draw.c:252 Draw_Fill -- fills a box of pixels with a single color
export function Draw_Fill(r: GLState, x: number, y: number, w: number, h: number, c: number): void {
  if (c >>> 0 > 255) r.ri.sysError(ERR_FATAL, 'Draw_Fill: bad color');
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.disable(GL_TEXTURE_2D);
  const color = r.d_8to24table[c]!;
  qgl.color3f((color & 255) / 255.0, ((color >>> 8) & 255) / 255.0, ((color >>> 16) & 255) / 255.0);
  qgl.begin(GL_QUADS);
  qgl.vertex2f(x, y);
  qgl.vertex2f(x + w, y);
  qgl.vertex2f(x + w, y + h);
  qgl.vertex2f(x, y + h);
  qgl.end();
  qgl.color3f(1, 1, 1);
  qgl.enable(GL_TEXTURE_2D);
}

// C: gl_draw.c:290 Draw_FadeScreen
export function Draw_FadeScreen(r: GLState): void {
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.enable(GL_BLEND);
  qgl.disable(GL_TEXTURE_2D);
  qgl.color4f(0, 0, 0, 0.8);
  qgl.begin(GL_QUADS);
  qgl.vertex2f(0, 0);
  qgl.vertex2f(r.vid.width, 0);
  qgl.vertex2f(r.vid.width, r.vid.height);
  qgl.vertex2f(0, r.vid.height);
  qgl.end();
  qgl.color4f(1, 1, 1, 1);
  qgl.enable(GL_TEXTURE_2D);
  qgl.disable(GL_BLEND);
}

/**
 * C: gl_draw.c:319 Draw_StretchRaw, the resampling loop into the 256x256 8-bit image (the C paletted
 * branch; the RGBA branch uses the same sampling). Returns t, the bottom texture coordinate.
 */
export function stretchRawResample(cols: number, rows: number, data: Uint8Array, image8: Uint8Array): number {
  let hscale;
  let trows;
  if (rows <= 256) {
    hscale = 1;
    trows = rows;
  } else {
    hscale = fr(rows / 256.0);
    trows = 256;
  }
  const t = fr(fr(rows * hscale) / 256);

  const fracstep = Math.trunc((cols * 0x10000) / 256);
  for (let i = 0; i < trows; i++) {
    const row = Math.trunc(fr(i * hscale));
    if (row > rows) break;
    const source = cols * row;
    const dest = i * 256;
    let frac = fracstep >> 1;
    for (let j = 0; j < 256; j++) {
      image8[dest + j] = data[source + (frac >> 16)] ?? 0;
      frac += fracstep;
    }
  }
  return t;
}

// C: gl_draw.c:319 Draw_StretchRaw -- drawn with an R8 index texture and the r_rawpalette texture
export function Draw_StretchRaw(
  r: GLState,
  x: number,
  y: number,
  w: number,
  h: number,
  cols: number,
  rows: number,
  data: Uint8Array,
): void {
  GL_Bind(r, 0);
  const t = stretchRawResample(cols, rows, data, r.rawImage8);
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.rawImage(r.rawImage8);
  const hack =
    r.gl_config.renderer === GL_RENDERER_MCD || (r.gl_config.renderer & GL_RENDERER_RENDITION) !== 0;
  if (hack) qgl.disable(GL_ALPHA_TEST);
  qgl.drawRaw(x, y, x + w, y + h, t);
  if (hack) qgl.enable(GL_ALPHA_TEST);
}
