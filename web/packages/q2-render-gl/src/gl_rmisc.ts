// Port of ref_gl/gl_rmisc.c: particle texture, screenshots, default GL state.
import { PRINT_ALL, fr } from 'q2-shared';
import { GLState, it_sprite, it_wall } from './gl_local';
import { GL_LoadPic, GL_TexEnv, GL_TextureAlphaMode, GL_TextureMode, GL_TextureSolidMode } from './gl_image';
import {
  GL_ALPHA_TEST,
  GL_BLEND,
  GL_CULL_FACE,
  GL_DEPTH_TEST,
  GL_FRONT,
  GL_GREATER,
  GL_ONE_MINUS_SRC_ALPHA,
  GL_REPLACE,
  GL_SRC_ALPHA,
  GL_TEXTURE_2D,
} from './qgl';

// C: gl_rmisc.c:30 dottexture
const dottexture = [
  [0, 0, 0, 0, 0, 0, 0, 0],
  [0, 0, 1, 1, 0, 0, 0, 0],
  [0, 1, 1, 1, 1, 0, 0, 0],
  [0, 1, 1, 1, 1, 0, 0, 0],
  [0, 0, 1, 1, 0, 0, 0, 0],
  [0, 0, 0, 0, 0, 0, 0, 0],
  [0, 0, 0, 0, 0, 0, 0, 0],
  [0, 0, 0, 0, 0, 0, 0, 0],
];

// C: gl_rmisc.c:42 R_InitParticleTexture
export function R_InitParticleTexture(r: GLState): void {
  const data = new Uint8Array(8 * 8 * 4);
  // particle texture
  for (let x = 0; x < 8; x++) {
    for (let y = 0; y < 8; y++) {
      const o = (y * 8 + x) * 4;
      data[o] = 255;
      data[o + 1] = 255;
      data[o + 2] = 255;
      data[o + 3] = dottexture[x]![y]! * 255;
    }
  }
  r.r_particletexture = GL_LoadPic(r, '***particle***', data, 8, 8, it_sprite, 32);

  // also use this for bad textures, but without alpha
  const data2 = new Uint8Array(8 * 8 * 4);
  for (let x = 0; x < 8; x++) {
    for (let y = 0; y < 8; y++) {
      const o = (y * 8 + x) * 4;
      data2[o] = dottexture[x & 3]![y & 3]! * 255;
      data2[o + 1] = 0; // dottexture[x&3][y&3]*255;
      data2[o + 2] = 0; // dottexture[x&3][y&3]*255;
      data2[o + 3] = 255;
    }
  }
  r.r_notexture = GL_LoadPic(r, '***r_notexture***', data2, 8, 8, it_wall, 32);
}

/**
 * C: gl_rmisc.c:94 GL_ScreenShot_f. The browser has no game directory: the drawing buffer is encoded as a
 * PNG with canvas.toBlob and offered as a download named quakeNN.png (NN counting up per session).
 */
export function GL_ScreenShot_f(r: GLState, onBlob?: (name: string, blob: Blob) => void): void {
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.flush();
  const canvas = qgl.canvas as HTMLCanvasElement;
  if (typeof canvas.toBlob !== 'function') {
    r.ri.conPrintf(PRINT_ALL, "SCR_ScreenShot_f: Couldn't create a file\n");
    return;
  }
  const i = r.screenshotCount++;
  if (i > 99) {
    r.ri.conPrintf(PRINT_ALL, "SCR_ScreenShot_f: Couldn't create a file\n");
    return;
  }
  const picname = `quake${Math.trunc(i / 10)}${i % 10}.png`;
  canvas.toBlob((blob) => {
    if (!blob) {
      r.ri.conPrintf(PRINT_ALL, "SCR_ScreenShot_f: Couldn't create a file\n");
      return;
    }
    if (onBlob) onBlob(picname, blob);
    else if (typeof document !== 'undefined') {
      const a = document.createElement('a');
      a.href = URL.createObjectURL(blob);
      a.download = picname;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 10000);
    }
    r.ri.conPrintf(PRINT_ALL, `Wrote ${picname}\n`);
  }, 'image/png');
}

// C: gl_rmisc.c:163 GL_Strings_f
export function GL_Strings_f(r: GLState): void {
  r.ri.conPrintf(PRINT_ALL, `GL_VENDOR: ${r.gl_config.vendor_string}\n`);
  r.ri.conPrintf(PRINT_ALL, `GL_RENDERER: ${r.gl_config.renderer_string}\n`);
  r.ri.conPrintf(PRINT_ALL, `GL_VERSION: ${r.gl_config.version_string}\n`);
  r.ri.conPrintf(PRINT_ALL, `GL_EXTENSIONS: ${r.gl_config.extensions_string}\n`);
}

// C: gl_rmisc.c:175 GL_SetDefaultState
export function GL_SetDefaultState(r: GLState): void {
  const qgl = r.qgl;
  qgl?.clearColor(1, 0, 0.5, 0.5);
  qgl?.cullFace(GL_FRONT);
  qgl?.enable(GL_TEXTURE_2D);

  qgl?.enable(GL_ALPHA_TEST);
  qgl?.alphaFunc(GL_GREATER, fr(0.666));

  qgl?.disable(GL_DEPTH_TEST);
  qgl?.disable(GL_CULL_FACE);
  qgl?.disable(GL_BLEND);

  qgl?.color4f(1, 1, 1, 1);

  GL_TextureMode(r, r.cv.gl_texturemode.string);
  GL_TextureAlphaMode(r, r.cv.gl_texturealphamode.string);
  GL_TextureSolidMode(r, r.cv.gl_texturesolidmode.string);

  qgl?.blendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA);

  GL_TexEnv(r, GL_REPLACE);

  // qglPointParameterfEXT is NULL (no GL_EXT_point_parameters in WebGL)
  // qglColorTableEXT is NULL (no paletted textures in WebGL): GL_SetTexturePalette is not called

  GL_UpdateSwapInterval(r);
}

// C: gl_rmisc.c:232 GL_UpdateSwapInterval -- the browser owns vsync (requestAnimationFrame)
export function GL_UpdateSwapInterval(r: GLState): void {
  if (r.cv.gl_swapinterval.modified) r.cv.gl_swapinterval.modified = false;
}
