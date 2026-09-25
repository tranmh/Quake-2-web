// WebGL2 replacement for ref_gl/qgl.h (the OpenGL 1.1 binding layer).
//
// The ported ref_gl code keeps calling a fixed-function style API (matrix stacks, glBegin/glEnd, texture
// environment, alpha test, depth range ...). This class emulates exactly the subset ref_gl uses on top of
// WebGL2 shaders:
//  * glBegin/glEnd primitives are converted to indexed triangles (or lines) and appended to a preallocated
//    stream buffer; the batch is flushed lazily, i.e. only when a state change (texture, blend, matrices,
//    depth ...) or an explicit draw call requires it. Consecutive primitives with identical state become
//    one draw call.
//  * GL_REPLACE / GL_MODULATE texture environments, GL_ALPHA_TEST (GL_GREATER ref) and vertex colour
//    clamping are done in the fragment/vertex shader with the fixed-function semantics, including the
//    alpha source rule for RGB (gl_tex_solid_format) textures: REPLACE takes the fragment alpha.
//  * Brush surfaces use a dedicated path (drawWorld): a static vertex buffer (7 floats per vertex, the C
//    glpoly_t layout) with per-frame index ranges, one draw per (texture, lightmap page) batch, and the
//    lightmap combine of R_BlendLightmaps done in the same pass.
//  * Lightmaps live in one TEXTURE_2D_ARRAY (BLOCK_WIDTH x BLOCK_HEIGHT x pages), updated per dirty rect.
//  * Draw_StretchRaw uses an R8 index texture + 256x1 palette texture with bilinear filtering done in the
//    shader after the palette lookup (== GL_LINEAR on the expanded RGBA image C uploads).

// ---- GL 1.1 enums used by the port (values from GL/gl.h)
export const GL_POINTS = 0x0000;
export const GL_LINES = 0x0001;
export const GL_LINE_STRIP = 0x0003;
export const GL_TRIANGLES = 0x0004;
export const GL_TRIANGLE_STRIP = 0x0005;
export const GL_TRIANGLE_FAN = 0x0006;
export const GL_QUADS = 0x0007;
export const GL_POLYGON = 0x0009;

export const GL_NEVER = 0x0200;
export const GL_LESS = 0x0201;
export const GL_EQUAL = 0x0202;
export const GL_LEQUAL = 0x0203;
export const GL_GREATER = 0x0204;
export const GL_NOTEQUAL = 0x0205;
export const GL_GEQUAL = 0x0206;
export const GL_ALWAYS = 0x0207;

export const GL_ZERO = 0;
export const GL_ONE = 1;
export const GL_SRC_COLOR = 0x0300;
export const GL_ONE_MINUS_SRC_COLOR = 0x0301;
export const GL_SRC_ALPHA = 0x0302;
export const GL_ONE_MINUS_SRC_ALPHA = 0x0303;
export const GL_DST_ALPHA = 0x0304;
export const GL_ONE_MINUS_DST_ALPHA = 0x0305;
export const GL_DST_COLOR = 0x0306;

export const GL_FRONT = 0x0404;
export const GL_BACK = 0x0405;
export const GL_FRONT_AND_BACK = 0x0408;

export const GL_CULL_FACE = 0x0b44;
export const GL_DEPTH_TEST = 0x0b71;
export const GL_ALPHA_TEST = 0x0bc0;
export const GL_BLEND = 0x0be2;
export const GL_SCISSOR_TEST = 0x0c11;
export const GL_TEXTURE_2D = 0x0de1;

export const GL_MODELVIEW = 0x1700;
export const GL_PROJECTION = 0x1701;

export const GL_REPLACE = 0x1e01;
export const GL_MODULATE = 0x2100;
export const GL_DECAL = 0x2101;

export const GL_NEAREST = 0x2600;
export const GL_LINEAR = 0x2601;
export const GL_NEAREST_MIPMAP_NEAREST = 0x2700;
export const GL_LINEAR_MIPMAP_NEAREST = 0x2701;
export const GL_NEAREST_MIPMAP_LINEAR = 0x2702;
export const GL_LINEAR_MIPMAP_LINEAR = 0x2703;

export const GL_DEPTH_BUFFER_BIT = 0x00000100;
export const GL_COLOR_BUFFER_BIT = 0x00004000;

export const GL_RGB = 0x1907;
export const GL_RGBA = 0x1908;
export const GL_LUMINANCE8 = 0x8040;
export const GL_INTENSITY8 = 0x804b;
export const GL_R3_G3_B2 = 0x2a10;
export const GL_RGB4 = 0x804f;
export const GL_RGB5 = 0x8050;
export const GL_RGB8 = 0x8051;
export const GL_RGBA2 = 0x8055;
export const GL_RGBA4 = 0x8056;
export const GL_RGB5_A1 = 0x8057;
export const GL_RGBA8 = 0x8058;

/** Internal formats with no alpha channel (sampling them yields alpha 1, GL_REPLACE keeps the colour alpha). */
function isRGBFormat(f: number): boolean {
  return f === 3 || f === GL_RGB || f === GL_RGB8 || f === GL_RGB5 || f === GL_RGB4 || f === GL_R3_G3_B2;
}

/** Lightmap combine modes of drawWorld (see gl_rsurf.ts R_BlendLightmaps). */
export const LM_MODULATE = 0; // glBlendFunc(GL_ZERO, GL_SRC_COLOR)
export const LM_SATURATE = 1; // glBlendFunc(GL_ONE, GL_ONE)
export const LM_ALPHA = 2; // glBlendFunc(GL_SRC_ALPHA, GL_ONE_MINUS_SRC_ALPHA)
export const LM_REPLACE = 3; // gl_lightmap 1: lightmap drawn without blending
export const LM_NONE = 4; // no lightmap pass (r_fullbright, no light data, translucent bmodel)

/** How the lightmap internal format expands (GL_BeginBuildingLightmaps internal_format). */
export const LMSWZ_RGBA = 0;
export const LMSWZ_LUMINANCE = 1;
export const LMSWZ_INTENSITY = 2;
export const LMSWZ_RGB = 3; // gl_tex_solid_format: no alpha channel

const VFLOATS = 9; // x y z s t r g b a

const GENERIC_VS = `#version 300 es
layout(location=0) in vec3 a_pos;
layout(location=1) in vec2 a_st;
layout(location=2) in vec4 a_color;
uniform mat4 u_mvp;
out vec2 v_st;
out vec4 v_color;
void main() {
  v_st = a_st;
  v_color = clamp(a_color, 0.0, 1.0);
  gl_Position = u_mvp * vec4(a_pos, 1.0);
}`;

const GENERIC_FS = `#version 300 es
precision highp float;
uniform sampler2D u_tex;
uniform int u_env;
uniform bool u_texRGB;
uniform bool u_alphaTest;
uniform float u_alphaRef;
in vec2 v_st;
in vec4 v_color;
out vec4 o_color;
void main() {
  vec4 c;
  if (u_env == 0) {
    c = v_color;
  } else {
    vec4 t = texture(u_tex, v_st);
    if (u_texRGB) t.a = 1.0;
    if (u_env == 1) c = vec4(t.rgb, u_texRGB ? v_color.a : t.a);
    else c = t * v_color;
  }
  if (u_alphaTest && !(c.a > u_alphaRef)) discard;
  o_color = c;
}`;

const WORLD_VS = `#version 300 es
layout(location=0) in vec3 a_pos;
layout(location=1) in vec2 a_st;
layout(location=2) in vec2 a_lm;
uniform mat4 u_mvp;
uniform float u_scroll;
out vec2 v_st;
out vec2 v_lm;
void main() {
  v_st = vec2(a_st.x + u_scroll, a_st.y);
  v_lm = a_lm;
  gl_Position = u_mvp * vec4(a_pos, 1.0);
}`;

const WORLD_FS = `#version 300 es
precision highp float;
precision highp sampler2DArray;
uniform sampler2D u_tex;
uniform sampler2DArray u_lm;
uniform float u_layer;
uniform int u_mode;
uniform int u_swz;
uniform bool u_texRGB;
uniform float u_alpha;
in vec2 v_st;
in vec2 v_lm;
out vec4 o_color;
void main() {
  vec4 t = texture(u_tex, v_st);
  if (u_texRGB) t.a = 1.0;
  vec3 c = t.rgb;
  if (u_mode != 4) {
    vec4 l = texture(u_lm, vec3(v_lm, u_layer));
    if (u_swz == 1) l = vec4(l.rrr, 1.0);
    else if (u_swz == 2) l = l.rrrr;
    else if (u_swz == 3) l.a = 1.0;
    if (u_mode == 0) c = t.rgb * l.rgb;
    else if (u_mode == 1) c = min(t.rgb + l.rgb, vec3(1.0));
    else if (u_mode == 2) c = l.rgb * l.a + t.rgb * (1.0 - l.a);
    else c = l.rgb;
  }
  o_color = vec4(c, u_texRGB ? u_alpha : t.a);
}`;

const RAW_FS = `#version 300 es
precision highp float;
uniform highp usampler2D u_idx;
uniform sampler2D u_pal;
uniform bool u_alphaTest;
uniform float u_alphaRef;
in vec2 v_st;
in vec4 v_color;
out vec4 o_color;
vec4 texel(ivec2 p) {
  p = p & 255;
  uint i = texelFetch(u_idx, p, 0).r;
  return texelFetch(u_pal, ivec2(int(i), 0), 0);
}
void main() {
  vec2 p = v_st * 256.0 - 0.5;
  vec2 f = fract(p);
  ivec2 i0 = ivec2(floor(p));
  vec4 a = mix(texel(i0), texel(i0 + ivec2(1, 0)), f.x);
  vec4 b = mix(texel(i0 + ivec2(0, 1)), texel(i0 + ivec2(1, 1)), f.x);
  vec4 c = vec4(mix(a, b, f.y).rgb, v_color.a);
  if (u_alphaTest && !(c.a > u_alphaRef)) discard;
  o_color = c;
}`;

interface GenericProgram {
  prog: WebGLProgram;
  u_mvp: WebGLUniformLocation | null;
  u_tex: WebGLUniformLocation | null;
  u_env: WebGLUniformLocation | null;
  u_texRGB: WebGLUniformLocation | null;
  u_alphaTest: WebGLUniformLocation | null;
  u_alphaRef: WebGLUniformLocation | null;
}

interface TexInfo {
  tex: WebGLTexture;
  rgb: boolean;
  levels: number;
  minFilter: number;
  magFilter: number;
}

/** Static brush vertex buffer handle. */
export interface WorldBuffer {
  vao: WebGLVertexArrayObject;
  vbo: WebGLBuffer;
}

function mat4Identity(m: Float32Array): void {
  m.fill(0);
  m[0] = m[5] = m[10] = m[15] = 1;
}

/** m = m * b (column-major) */
function mat4MulRight(m: Float32Array, b: ArrayLike<number>, tmp: Float64Array): void {
  for (let c = 0; c < 4; c++) {
    for (let r = 0; r < 4; r++) {
      tmp[c * 4 + r] =
        m[r]! * b[c * 4]! +
        m[4 + r]! * b[c * 4 + 1]! +
        m[8 + r]! * b[c * 4 + 2]! +
        m[12 + r]! * b[c * 4 + 3]!;
    }
  }
  for (let i = 0; i < 16; i++) m[i] = tmp[i]!;
}

/** out = a * b (column-major) */
function mat4Mul(out: Float32Array, a: Float32Array, b: Float32Array): void {
  for (let c = 0; c < 4; c++) {
    for (let r = 0; r < 4; r++) {
      out[c * 4 + r] =
        a[r]! * b[c * 4]! +
        a[4 + r]! * b[c * 4 + 1]! +
        a[8 + r]! * b[c * 4 + 2]! +
        a[12 + r]! * b[c * 4 + 3]!;
    }
  }
}

export interface QGLOptions {
  preserveDrawingBuffer?: boolean;
}

export class QGL {
  readonly gl: WebGL2RenderingContext;
  readonly canvas: HTMLCanvasElement | OffscreenCanvas;

  // ---- programs
  private readonly generic: GenericProgram;
  private readonly world: {
    prog: WebGLProgram;
    u_mvp: WebGLUniformLocation | null;
    u_scroll: WebGLUniformLocation | null;
    u_tex: WebGLUniformLocation | null;
    u_lm: WebGLUniformLocation | null;
    u_layer: WebGLUniformLocation | null;
    u_mode: WebGLUniformLocation | null;
    u_swz: WebGLUniformLocation | null;
    u_texRGB: WebGLUniformLocation | null;
    u_alpha: WebGLUniformLocation | null;
  };
  private readonly raw: {
    prog: WebGLProgram;
    u_mvp: WebGLUniformLocation | null;
    u_idx: WebGLUniformLocation | null;
    u_pal: WebGLUniformLocation | null;
    u_alphaTest: WebGLUniformLocation | null;
    u_alphaRef: WebGLUniformLocation | null;
  };

  // ---- immediate-mode stream
  private vcap = 65536;
  private icap = 65536 * 3;
  private verts = new Float32Array(this.vcap * VFLOATS);
  private indices = new Uint32Array(this.icap);
  private nverts = 0;
  private nidx = 0;
  private batchLines = false;
  private inBegin = false;
  private prim = 0;
  private primStart = 0;
  private readonly streamVao: WebGLVertexArrayObject;
  private readonly streamVbo: WebGLBuffer;
  private readonly streamIbo: WebGLBuffer;
  private streamVboBytes = 0;
  private streamIboBytes = 0;

  // world index stream
  private readonly worldIbo: WebGLBuffer;
  private worldIboBytes = 0;

  // ---- current vertex attributes
  private cs = 0;
  private ct = 0;
  private cr = 1;
  private cg = 1;
  private cb = 1;
  private ca = 1;

  // ---- fixed function state (as set by the port)
  private matrixModeCur = GL_MODELVIEW;
  private readonly mvStack: Float32Array[] = [];
  private readonly projStack: Float32Array[] = [];
  private mvTop = 0;
  private projTop = 0;
  private readonly mtmp = new Float64Array(16);
  private readonly mtmp32 = new Float32Array(16);
  private readonly mvp = new Float32Array(16);
  private tex2D = true;
  private blend = false;
  private depthTest = false;
  private cull = false;
  private alphaTest = false;
  private scissorTest = false;
  private alphaRef = 0;
  private blendSrc = GL_ONE;
  private blendDst = GL_ZERO;
  private depthMaskV = true;
  private depthFuncV = GL_LESS;
  private depthNear = 0;
  private depthFar = 1;
  private cullFaceV = GL_BACK;
  private readonly vp = [0, 0, 0, 0];
  private readonly sc = [0, 0, 0, 0];
  private readonly clearCol = [0, 0, 0, 0];
  private boundTex = 0;
  private texEnvMode = GL_MODULATE;

  // ---- applied WebGL state (to skip redundant calls)
  private aProg: WebGLProgram | null = null;
  private aBlend = false;
  private aBlendSrc = -1;
  private aBlendDst = -1;
  private aDepthTest = false;
  private aDepthMask = true;
  private aDepthFunc = -1;
  private aDepthNear = -1;
  private aDepthFar = -1;
  private aCull = false;
  private aCullFace = -1;
  private aScissor = false;
  private readonly aVp = [-1, -1, -1, -1];
  private readonly aSc = [-1, -1, -1, -1];

  // ---- textures
  private readonly textures = new Map<number, TexInfo>();
  private lightmapTex: WebGLTexture | null = null;
  private lightmapPages = 0;
  private rawIdx: WebGLTexture | null = null;
  private rawPal: WebGLTexture | null = null;
  private readonly rawPalData = new Uint8Array(256 * 4);
  private rawPalDirty = true;

  /** Number of WebGL draw calls issued since the last resetStats(). */
  drawCalls = 0;

  constructor(canvas: HTMLCanvasElement | OffscreenCanvas, opts: QGLOptions = {}) {
    this.canvas = canvas;
    const gl = canvas.getContext('webgl2', {
      alpha: false,
      depth: true,
      stencil: false,
      antialias: false,
      premultipliedAlpha: false,
      preserveDrawingBuffer: opts.preserveDrawingBuffer ?? false,
    }) as WebGL2RenderingContext | null;
    if (!gl) throw new Error('WebGL2 is not available');
    this.gl = gl;
    for (let i = 0; i < 32; i++) {
      const a = new Float32Array(16);
      mat4Identity(a);
      this.mvStack.push(a);
      const b = new Float32Array(16);
      mat4Identity(b);
      this.projStack.push(b);
    }

    const g = this.link(GENERIC_VS, GENERIC_FS);
    this.generic = {
      prog: g,
      u_mvp: gl.getUniformLocation(g, 'u_mvp'),
      u_tex: gl.getUniformLocation(g, 'u_tex'),
      u_env: gl.getUniformLocation(g, 'u_env'),
      u_texRGB: gl.getUniformLocation(g, 'u_texRGB'),
      u_alphaTest: gl.getUniformLocation(g, 'u_alphaTest'),
      u_alphaRef: gl.getUniformLocation(g, 'u_alphaRef'),
    };
    const w = this.link(WORLD_VS, WORLD_FS);
    this.world = {
      prog: w,
      u_mvp: gl.getUniformLocation(w, 'u_mvp'),
      u_scroll: gl.getUniformLocation(w, 'u_scroll'),
      u_tex: gl.getUniformLocation(w, 'u_tex'),
      u_lm: gl.getUniformLocation(w, 'u_lm'),
      u_layer: gl.getUniformLocation(w, 'u_layer'),
      u_mode: gl.getUniformLocation(w, 'u_mode'),
      u_swz: gl.getUniformLocation(w, 'u_swz'),
      u_texRGB: gl.getUniformLocation(w, 'u_texRGB'),
      u_alpha: gl.getUniformLocation(w, 'u_alpha'),
    };
    const r = this.link(GENERIC_VS, RAW_FS);
    this.raw = {
      prog: r,
      u_mvp: gl.getUniformLocation(r, 'u_mvp'),
      u_idx: gl.getUniformLocation(r, 'u_idx'),
      u_pal: gl.getUniformLocation(r, 'u_pal'),
      u_alphaTest: gl.getUniformLocation(r, 'u_alphaTest'),
      u_alphaRef: gl.getUniformLocation(r, 'u_alphaRef'),
    };
    gl.useProgram(g);
    gl.uniform1i(this.generic.u_tex, 0);
    gl.useProgram(w);
    gl.uniform1i(this.world.u_tex, 0);
    gl.uniform1i(this.world.u_lm, 1);
    gl.useProgram(r);
    gl.uniform1i(this.raw.u_idx, 2);
    gl.uniform1i(this.raw.u_pal, 3);
    gl.useProgram(null);

    this.streamVao = gl.createVertexArray()!;
    this.streamVbo = gl.createBuffer()!;
    this.streamIbo = gl.createBuffer()!;
    gl.bindVertexArray(this.streamVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.streamVbo);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, VFLOATS * 4, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, VFLOATS * 4, 12);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 4, gl.FLOAT, false, VFLOATS * 4, 20);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.streamIbo);
    gl.bindVertexArray(null);
    this.worldIbo = gl.createBuffer()!;

    gl.pixelStorei(gl.UNPACK_ALIGNMENT, 1);
    gl.pixelStorei(gl.PACK_ALIGNMENT, 1);
    // GL 1.1 initial state
    gl.disable(gl.DEPTH_TEST);
    gl.disable(gl.BLEND);
    gl.disable(gl.CULL_FACE);
    gl.depthMask(true);
    gl.depthFunc(gl.LESS);
    gl.blendFunc(gl.ONE, gl.ZERO);
    gl.cullFace(gl.BACK);
    gl.frontFace(gl.CCW);
  }

  private link(vs: string, fs: string): WebGLProgram {
    const gl = this.gl;
    const mk = (type: number, src: string): WebGLShader => {
      const s = gl.createShader(type)!;
      gl.shaderSource(s, src);
      gl.compileShader(s);
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
        throw new Error(`shader compile failed: ${gl.getShaderInfoLog(s) ?? ''}`);
      }
      return s;
    };
    const p = gl.createProgram()!;
    gl.attachShader(p, mk(gl.VERTEX_SHADER, vs));
    gl.attachShader(p, mk(gl.FRAGMENT_SHADER, fs));
    gl.linkProgram(p);
    if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
      throw new Error(`program link failed: ${gl.getProgramInfoLog(p) ?? ''}`);
    }
    return p;
  }

  get drawingBufferWidth(): number {
    return this.gl.drawingBufferWidth;
  }
  get drawingBufferHeight(): number {
    return this.gl.drawingBufferHeight;
  }

  resetStats(): void {
    this.drawCalls = 0;
  }

  // ======================================================================== state
  private changed(): void {
    if (this.nidx > 0) this.flush();
  }

  enable(cap: number): void {
    this.setCap(cap, true);
  }
  disable(cap: number): void {
    this.setCap(cap, false);
  }
  private setCap(cap: number, v: boolean): void {
    switch (cap) {
      case GL_TEXTURE_2D:
        if (this.tex2D !== v) {
          this.changed();
          this.tex2D = v;
        }
        break;
      case GL_BLEND:
        if (this.blend !== v) {
          this.changed();
          this.blend = v;
        }
        break;
      case GL_DEPTH_TEST:
        if (this.depthTest !== v) {
          this.changed();
          this.depthTest = v;
        }
        break;
      case GL_CULL_FACE:
        if (this.cull !== v) {
          this.changed();
          this.cull = v;
        }
        break;
      case GL_ALPHA_TEST:
        if (this.alphaTest !== v) {
          this.changed();
          this.alphaTest = v;
        }
        break;
      case GL_SCISSOR_TEST:
        if (this.scissorTest !== v) {
          this.changed();
          this.scissorTest = v;
        }
        break;
      default:
        break; // GL_POINT_SMOOTH, GL_SHARED_TEXTURE_PALETTE_EXT ...: not emulated
    }
  }
  alphaFunc(_func: number, ref: number): void {
    // ref_gl only ever uses GL_GREATER
    if (this.alphaRef !== ref) {
      this.changed();
      this.alphaRef = ref;
    }
  }
  blendFunc(s: number, d: number): void {
    if (this.blendSrc !== s || this.blendDst !== d) {
      this.changed();
      this.blendSrc = s;
      this.blendDst = d;
    }
  }
  depthMask(v: boolean | number): void {
    const b = !!v;
    if (this.depthMaskV !== b) {
      this.changed();
      this.depthMaskV = b;
    }
  }
  depthFunc(f: number): void {
    if (this.depthFuncV !== f) {
      this.changed();
      this.depthFuncV = f;
    }
  }
  depthRange(n: number, f: number): void {
    if (this.depthNear !== n || this.depthFar !== f) {
      this.changed();
      this.depthNear = n;
      this.depthFar = f;
    }
  }
  cullFace(m: number): void {
    if (this.cullFaceV !== m) {
      this.changed();
      this.cullFaceV = m;
    }
  }
  viewport(x: number, y: number, w: number, h: number): void {
    const v = this.vp;
    if (v[0] !== x || v[1] !== y || v[2] !== w || v[3] !== h) {
      this.changed();
      v[0] = x;
      v[1] = y;
      v[2] = w;
      v[3] = h;
    }
  }
  scissor(x: number, y: number, w: number, h: number): void {
    const v = this.sc;
    if (v[0] !== x || v[1] !== y || v[2] !== w || v[3] !== h) {
      this.changed();
      v[0] = x;
      v[1] = y;
      v[2] = w;
      v[3] = h;
    }
  }
  clearColor(r: number, g: number, b: number, a: number): void {
    this.clearCol[0] = r;
    this.clearCol[1] = g;
    this.clearCol[2] = b;
    this.clearCol[3] = a;
  }
  clear(mask: number): void {
    this.changed();
    const gl = this.gl;
    this.applyCommon();
    let m = 0;
    if (mask & GL_COLOR_BUFFER_BIT) {
      m |= gl.COLOR_BUFFER_BIT;
      gl.clearColor(this.clearCol[0]!, this.clearCol[1]!, this.clearCol[2]!, this.clearCol[3]!);
    }
    if (mask & GL_DEPTH_BUFFER_BIT) m |= gl.DEPTH_BUFFER_BIT;
    gl.clear(m);
  }
  color4f(r: number, g: number, b: number, a: number): void {
    this.cr = r;
    this.cg = g;
    this.cb = b;
    this.ca = a;
  }
  color3f(r: number, g: number, b: number): void {
    this.cr = r;
    this.cg = g;
    this.cb = b;
    this.ca = 1;
  }
  /** glColor4ubv */
  color4ub(r: number, g: number, b: number, a: number): void {
    this.cr = r / 255;
    this.cg = g / 255;
    this.cb = b / 255;
    this.ca = a / 255;
  }
  texEnv(mode: number): void {
    if (this.texEnvMode !== mode) {
      this.changed();
      this.texEnvMode = mode;
    }
  }
  bindTexture(texnum: number): void {
    if (this.boundTex !== texnum) {
      this.changed();
      this.boundTex = texnum;
    }
  }

  // ======================================================================== matrices
  matrixMode(m: number): void {
    this.matrixModeCur = m;
  }
  private cur(): Float32Array {
    return this.matrixModeCur === GL_PROJECTION ? this.projStack[this.projTop]! : this.mvStack[this.mvTop]!;
  }
  loadIdentity(): void {
    this.changed();
    mat4Identity(this.cur());
  }
  loadMatrixf(m: ArrayLike<number>): void {
    this.changed();
    const c = this.cur();
    for (let i = 0; i < 16; i++) c[i] = m[i]!;
  }
  pushMatrix(): void {
    if (this.matrixModeCur === GL_PROJECTION) {
      this.projStack[this.projTop + 1]!.set(this.projStack[this.projTop]!);
      this.projTop++;
    } else {
      this.mvStack[this.mvTop + 1]!.set(this.mvStack[this.mvTop]!);
      this.mvTop++;
    }
  }
  popMatrix(): void {
    this.changed();
    if (this.matrixModeCur === GL_PROJECTION) this.projTop--;
    else this.mvTop--;
  }
  private multRight(b: ArrayLike<number>): void {
    this.changed();
    mat4MulRight(this.cur(), b, this.mtmp);
  }
  translatef(x: number, y: number, z: number): void {
    const m = this.mtmp32;
    mat4Identity(m);
    m[12] = x;
    m[13] = y;
    m[14] = z;
    this.multRight(m);
  }
  scalef(x: number, y: number, z: number): void {
    const m = this.mtmp32;
    mat4Identity(m);
    m[0] = x;
    m[5] = y;
    m[10] = z;
    this.multRight(m);
  }
  /** glRotatef (angle in degrees around axis; a (near) zero axis leaves the matrix unchanged like Mesa) */
  rotatef(angle: number, x: number, y: number, z: number): void {
    const mag = Math.sqrt(x * x + y * y + z * z);
    if (mag <= 1.0e-4) return;
    x /= mag;
    y /= mag;
    z /= mag;
    const a = (angle * Math.PI) / 180;
    const s = Math.sin(a);
    const c = Math.cos(a);
    const one_c = 1 - c;
    const m = this.mtmp32;
    m[0] = x * x * one_c + c;
    m[1] = y * x * one_c + z * s;
    m[2] = x * z * one_c - y * s;
    m[3] = 0;
    m[4] = x * y * one_c - z * s;
    m[5] = y * y * one_c + c;
    m[6] = y * z * one_c + x * s;
    m[7] = 0;
    m[8] = x * z * one_c + y * s;
    m[9] = y * z * one_c - x * s;
    m[10] = z * z * one_c + c;
    m[11] = 0;
    m[12] = m[13] = m[14] = 0;
    m[15] = 1;
    this.multRight(m);
  }
  frustum(l: number, r: number, b: number, t: number, n: number, f: number): void {
    const m = this.mtmp32;
    m.fill(0);
    m[0] = (2 * n) / (r - l);
    m[5] = (2 * n) / (t - b);
    m[8] = (r + l) / (r - l);
    m[9] = (t + b) / (t - b);
    m[10] = -(f + n) / (f - n);
    m[11] = -1;
    m[14] = -(2 * f * n) / (f - n);
    this.multRight(m);
  }
  ortho(l: number, r: number, b: number, t: number, n: number, f: number): void {
    const m = this.mtmp32;
    mat4Identity(m);
    m[0] = 2 / (r - l);
    m[5] = 2 / (t - b);
    m[10] = -2 / (f - n);
    m[12] = -(r + l) / (r - l);
    m[13] = -(t + b) / (t - b);
    m[14] = -(f + n) / (f - n);
    this.multRight(m);
  }
  /** glGetFloatv(GL_MODELVIEW_MATRIX) */
  getModelview(out: Float32Array): void {
    out.set(this.mvStack[this.mvTop]!);
  }

  // ======================================================================== immediate mode
  begin(prim: number): void {
    const lines = prim === GL_LINES || prim === GL_LINE_STRIP;
    if (this.nidx > 0 && lines !== this.batchLines) this.flush();
    this.batchLines = lines;
    this.inBegin = true;
    this.prim = prim;
    this.primStart = this.nverts;
  }
  texCoord2f(s: number, t: number): void {
    this.cs = s;
    this.ct = t;
  }
  vertex2f(x: number, y: number): void {
    this.vertex3f(x, y, 0);
  }
  vertex3fv(v: ArrayLike<number>, o = 0): void {
    this.vertex3f(v[o]!, v[o + 1]!, v[o + 2]!);
  }
  vertex3f(x: number, y: number, z: number): void {
    if (this.nverts >= this.vcap) this.growVerts();
    const o = this.nverts * VFLOATS;
    const v = this.verts;
    v[o] = x;
    v[o + 1] = y;
    v[o + 2] = z;
    v[o + 3] = this.cs;
    v[o + 4] = this.ct;
    v[o + 5] = this.cr;
    v[o + 6] = this.cg;
    v[o + 7] = this.cb;
    v[o + 8] = this.ca;
    this.nverts++;
  }
  private growVerts(): void {
    this.vcap *= 2;
    const nv = new Float32Array(this.vcap * VFLOATS);
    nv.set(this.verts);
    this.verts = nv;
  }
  private idx(i: number): void {
    if (this.nidx >= this.icap) {
      this.icap *= 2;
      const ni = new Uint32Array(this.icap);
      ni.set(this.indices);
      this.indices = ni;
    }
    this.indices[this.nidx++] = i;
  }
  end(): void {
    this.inBegin = false;
    const s = this.primStart;
    const n = this.nverts - s;
    switch (this.prim) {
      case GL_TRIANGLES:
        for (let i = 0; i + 2 < n; i += 3) {
          this.idx(s + i);
          this.idx(s + i + 1);
          this.idx(s + i + 2);
        }
        break;
      case GL_TRIANGLE_FAN:
      case GL_POLYGON:
        for (let i = 2; i < n; i++) {
          this.idx(s);
          this.idx(s + i - 1);
          this.idx(s + i);
        }
        break;
      case GL_TRIANGLE_STRIP:
        for (let i = 2; i < n; i++) {
          if (i & 1) {
            this.idx(s + i - 1);
            this.idx(s + i - 2);
          } else {
            this.idx(s + i - 2);
            this.idx(s + i - 1);
          }
          this.idx(s + i);
        }
        break;
      case GL_QUADS:
        for (let i = 0; i + 3 < n; i += 4) {
          this.idx(s + i);
          this.idx(s + i + 1);
          this.idx(s + i + 2);
          this.idx(s + i);
          this.idx(s + i + 2);
          this.idx(s + i + 3);
        }
        break;
      case GL_LINES:
        for (let i = 0; i + 1 < n; i += 2) {
          this.idx(s + i);
          this.idx(s + i + 1);
        }
        break;
      case GL_LINE_STRIP:
        for (let i = 1; i < n; i++) {
          this.idx(s + i - 1);
          this.idx(s + i);
        }
        break;
      default:
        break;
    }
    if (this.nidx === 0) this.nverts = 0;
  }

  /** Draw everything batched so far. */
  flush(): void {
    if (this.inBegin || this.nidx === 0) {
      if (!this.inBegin) this.nverts = 0;
      return;
    }
    const gl = this.gl;
    this.applyCommon();
    const p = this.generic;
    this.useProgram(p.prog);
    this.computeMvp();
    gl.uniformMatrix4fv(p.u_mvp, false, this.mvp);
    let env = 0;
    let rgb = false;
    if (this.tex2D) {
      const ti = this.textures.get(this.boundTex);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, ti ? ti.tex : null);
      rgb = ti ? ti.rgb : false;
      env = this.texEnvMode === GL_REPLACE ? 1 : 2;
    }
    gl.uniform1i(p.u_env, env);
    gl.uniform1i(p.u_texRGB, rgb ? 1 : 0);
    gl.uniform1i(p.u_alphaTest, this.alphaTest ? 1 : 0);
    gl.uniform1f(p.u_alphaRef, this.alphaRef);

    gl.bindVertexArray(this.streamVao);
    const vbytes = this.nverts * VFLOATS * 4;
    gl.bindBuffer(gl.ARRAY_BUFFER, this.streamVbo);
    if (vbytes > this.streamVboBytes) {
      this.streamVboBytes = Math.max(vbytes, this.vcap * VFLOATS * 4);
      gl.bufferData(gl.ARRAY_BUFFER, this.streamVboBytes, gl.STREAM_DRAW);
    }
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.verts, 0, this.nverts * VFLOATS);
    const ibytes = this.nidx * 4;
    if (ibytes > this.streamIboBytes) {
      this.streamIboBytes = Math.max(ibytes, this.icap * 4);
      gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, this.streamIboBytes, gl.STREAM_DRAW);
    }
    gl.bufferSubData(gl.ELEMENT_ARRAY_BUFFER, 0, this.indices, 0, this.nidx);
    gl.drawElements(this.batchLines ? gl.LINES : gl.TRIANGLES, this.nidx, gl.UNSIGNED_INT, 0);
    this.drawCalls++;
    gl.bindVertexArray(null);
    this.nverts = 0;
    this.nidx = 0;
  }

  private computeMvp(): void {
    mat4Mul(this.mvp, this.projStack[this.projTop]!, this.mvStack[this.mvTop]!);
  }

  private useProgram(p: WebGLProgram): void {
    if (this.aProg !== p) {
      this.gl.useProgram(p);
      this.aProg = p;
    }
  }

  private glBlendFactor(f: number): number {
    const gl = this.gl;
    switch (f) {
      case GL_ZERO:
        return gl.ZERO;
      case GL_ONE:
        return gl.ONE;
      case GL_SRC_COLOR:
        return gl.SRC_COLOR;
      case GL_ONE_MINUS_SRC_COLOR:
        return gl.ONE_MINUS_SRC_COLOR;
      case GL_SRC_ALPHA:
        return gl.SRC_ALPHA;
      case GL_ONE_MINUS_SRC_ALPHA:
        return gl.ONE_MINUS_SRC_ALPHA;
      case GL_DST_ALPHA:
        return gl.DST_ALPHA;
      case GL_ONE_MINUS_DST_ALPHA:
        return gl.ONE_MINUS_DST_ALPHA;
      case GL_DST_COLOR:
        return gl.DST_COLOR;
      default:
        return gl.ONE;
    }
  }

  /** Apply blend / depth / cull / viewport / scissor state to WebGL. */
  private applyCommon(): void {
    const gl = this.gl;
    if (this.aBlend !== this.blend) {
      if (this.blend) gl.enable(gl.BLEND);
      else gl.disable(gl.BLEND);
      this.aBlend = this.blend;
    }
    if (this.blend && (this.aBlendSrc !== this.blendSrc || this.aBlendDst !== this.blendDst)) {
      gl.blendFunc(this.glBlendFactor(this.blendSrc), this.glBlendFactor(this.blendDst));
      this.aBlendSrc = this.blendSrc;
      this.aBlendDst = this.blendDst;
    }
    if (this.aDepthTest !== this.depthTest) {
      if (this.depthTest) gl.enable(gl.DEPTH_TEST);
      else gl.disable(gl.DEPTH_TEST);
      this.aDepthTest = this.depthTest;
    }
    if (this.aDepthMask !== this.depthMaskV) {
      gl.depthMask(this.depthMaskV);
      this.aDepthMask = this.depthMaskV;
    }
    if (this.aDepthFunc !== this.depthFuncV) {
      gl.depthFunc(this.depthFuncV); // GL_NEVER..GL_ALWAYS share their values with WebGL
      this.aDepthFunc = this.depthFuncV;
    }
    if (this.aDepthNear !== this.depthNear || this.aDepthFar !== this.depthFar) {
      gl.depthRange(this.depthNear, this.depthFar);
      this.aDepthNear = this.depthNear;
      this.aDepthFar = this.depthFar;
    }
    if (this.aCull !== this.cull) {
      if (this.cull) gl.enable(gl.CULL_FACE);
      else gl.disable(gl.CULL_FACE);
      this.aCull = this.cull;
    }
    if (this.cull && this.aCullFace !== this.cullFaceV) {
      gl.cullFace(
        this.cullFaceV === GL_FRONT ? gl.FRONT : this.cullFaceV === GL_BACK ? gl.BACK : gl.FRONT_AND_BACK,
      );
      this.aCullFace = this.cullFaceV;
    }
    const v = this.vp;
    const av = this.aVp;
    if (av[0] !== v[0] || av[1] !== v[1] || av[2] !== v[2] || av[3] !== v[3]) {
      gl.viewport(v[0]!, v[1]!, v[2]!, v[3]!);
      av[0] = v[0]!;
      av[1] = v[1]!;
      av[2] = v[2]!;
      av[3] = v[3]!;
    }
    if (this.aScissor !== this.scissorTest) {
      if (this.scissorTest) gl.enable(gl.SCISSOR_TEST);
      else gl.disable(gl.SCISSOR_TEST);
      this.aScissor = this.scissorTest;
    }
    const s = this.sc;
    const as = this.aSc;
    if (this.scissorTest && (as[0] !== s[0] || as[1] !== s[1] || as[2] !== s[2] || as[3] !== s[3])) {
      gl.scissor(s[0]!, s[1]!, s[2]!, s[3]!);
      as[0] = s[0]!;
      as[1] = s[1]!;
      as[2] = s[2]!;
      as[3] = s[3]!;
    }
  }

  // ======================================================================== textures
  private glFilter(f: number): number {
    const gl = this.gl;
    switch (f) {
      case GL_NEAREST:
        return gl.NEAREST;
      case GL_LINEAR:
        return gl.LINEAR;
      case GL_NEAREST_MIPMAP_NEAREST:
        return gl.NEAREST_MIPMAP_NEAREST;
      case GL_LINEAR_MIPMAP_NEAREST:
        return gl.LINEAR_MIPMAP_NEAREST;
      case GL_NEAREST_MIPMAP_LINEAR:
        return gl.NEAREST_MIPMAP_LINEAR;
      case GL_LINEAR_MIPMAP_LINEAR:
        return gl.LINEAR_MIPMAP_LINEAR;
      default:
        return gl.LINEAR;
    }
  }

  private texInfo(texnum: number): TexInfo {
    let ti = this.textures.get(texnum);
    if (!ti) {
      const gl = this.gl;
      const tex = gl.createTexture()!;
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.REPEAT);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.REPEAT);
      // GL 1.1 defaults
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST_MIPMAP_LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      ti = { tex, rgb: false, levels: 0, minFilter: GL_NEAREST_MIPMAP_LINEAR, magFilter: GL_LINEAR };
      this.textures.set(texnum, ti);
    }
    return ti;
  }

  /** glTexImage2D(GL_TEXTURE_2D, level, internalFormat, w, h, 0, GL_RGBA, GL_UNSIGNED_BYTE, data) on the bound texture. */
  texImage2D(level: number, internalFormat: number, w: number, h: number, data: Uint8Array): void {
    this.changed();
    const gl = this.gl;
    const ti = this.texInfo(this.boundTex);
    gl.bindTexture(gl.TEXTURE_2D, ti.tex);
    gl.texImage2D(gl.TEXTURE_2D, level, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, data, 0);
    if (level === 0) {
      ti.rgb = isRGBFormat(internalFormat);
      ti.levels = 1;
    } else if (level + 1 > ti.levels) ti.levels = level + 1;
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAX_LEVEL, ti.levels - 1);
  }

  /** glTexParameterf(GL_TEXTURE_2D, GL_TEXTURE_MIN_FILTER / MAG_FILTER) on the bound texture. */
  texFilter(min: number, mag: number): void {
    this.changed();
    const gl = this.gl;
    const ti = this.texInfo(this.boundTex);
    gl.bindTexture(gl.TEXTURE_2D, ti.tex);
    ti.minFilter = min;
    ti.magFilter = mag;
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, this.glFilter(min));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, this.glFilter(mag));
  }

  deleteTexture(texnum: number): void {
    this.changed();
    const ti = this.textures.get(texnum);
    if (ti) {
      this.gl.deleteTexture(ti.tex);
      this.textures.delete(texnum);
    }
  }

  // ======================================================================== lightmaps
  /** (Re)allocate the lightmap array texture with `pages` layers. */
  lightmapInit(pages: number): void {
    this.changed();
    const gl = this.gl;
    if (this.lightmapTex && this.lightmapPages === pages) return;
    if (this.lightmapTex) gl.deleteTexture(this.lightmapTex);
    this.lightmapTex = gl.createTexture()!;
    this.lightmapPages = pages;
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D_ARRAY, this.lightmapTex);
    gl.texStorage3D(gl.TEXTURE_2D_ARRAY, 1, gl.RGBA8, 128, 128, pages);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D_ARRAY, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.activeTexture(gl.TEXTURE0);
  }

  /** Upload the sub rectangle (x, y, w, h) of a 128x128 RGBA page mirror into layer `page`. */
  lightmapSubImage(page: number, x: number, y: number, w: number, h: number, pageData: Uint8Array): void {
    if (!this.lightmapTex || page >= this.lightmapPages || w <= 0 || h <= 0) return;
    this.changed();
    const gl = this.gl;
    gl.activeTexture(gl.TEXTURE1);
    gl.bindTexture(gl.TEXTURE_2D_ARRAY, this.lightmapTex);
    gl.pixelStorei(gl.UNPACK_ROW_LENGTH, 128);
    gl.texSubImage3D(
      gl.TEXTURE_2D_ARRAY,
      0,
      x,
      y,
      page,
      w,
      h,
      1,
      gl.RGBA,
      gl.UNSIGNED_BYTE,
      pageData,
      (y * 128 + x) * 4,
    );
    gl.pixelStorei(gl.UNPACK_ROW_LENGTH, 0);
    gl.activeTexture(gl.TEXTURE0);
  }

  // ======================================================================== world geometry
  /** Upload a static vertex buffer of glpoly_t vertices (x y z s t ls lt). */
  createWorldBuffer(verts: Float32Array): WorldBuffer {
    this.changed();
    const gl = this.gl;
    const vao = gl.createVertexArray()!;
    const vbo = gl.createBuffer()!;
    gl.bindVertexArray(vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    gl.bufferData(gl.ARRAY_BUFFER, verts, gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 28, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, 28, 12);
    gl.enableVertexAttribArray(2);
    gl.vertexAttribPointer(2, 2, gl.FLOAT, false, 28, 20);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.worldIbo);
    gl.bindVertexArray(null);
    return { vao, vbo };
  }

  deleteWorldBuffer(b: WorldBuffer): void {
    this.gl.deleteVertexArray(b.vao);
    this.gl.deleteBuffer(b.vbo);
  }

  /** Upload the frame's world index list (once per brush model draw). */
  uploadWorldIndices(indices: Uint32Array, count: number): void {
    this.changed();
    const gl = this.gl;
    gl.bindVertexArray(null);
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, this.worldIbo);
    const bytes = count * 4;
    if (bytes > this.worldIboBytes) {
      this.worldIboBytes = Math.max(bytes, indices.byteLength);
      gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, this.worldIboBytes, gl.DYNAMIC_DRAW);
    }
    gl.bufferSubData(gl.ELEMENT_ARRAY_BUFFER, 0, indices, 0, count);
  }

  private worldSetup = false;
  /**
   * Draw `count` indices starting at `first` of the uploaded world index list with the current matrices,
   * blend/depth state, the given base texture and lightmap layer.
   */
  drawWorld(
    b: WorldBuffer,
    first: number,
    count: number,
    texnum: number,
    layer: number,
    scroll: number,
    mode: number,
    swizzle: number,
    alpha: number,
  ): void {
    this.changed();
    const gl = this.gl;
    const p = this.world;
    this.applyCommon();
    this.useProgram(p.prog);
    if (!this.worldSetup) {
      this.computeMvp();
      gl.uniformMatrix4fv(p.u_mvp, false, this.mvp);
      gl.bindVertexArray(b.vao);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D_ARRAY, this.lightmapTex);
      gl.activeTexture(gl.TEXTURE0);
      gl.uniform1i(p.u_mode, mode);
      gl.uniform1i(p.u_swz, swizzle);
      this.worldSetup = true;
    }
    const ti = this.textures.get(texnum);
    gl.bindTexture(gl.TEXTURE_2D, ti ? ti.tex : null);
    gl.uniform1i(p.u_texRGB, ti && ti.rgb ? 1 : 0);
    gl.uniform1f(p.u_alpha, alpha);
    gl.uniform1f(p.u_layer, layer);
    gl.uniform1f(p.u_scroll, scroll);
    gl.drawElements(gl.TRIANGLES, count, gl.UNSIGNED_INT, first * 4);
    this.drawCalls++;
  }
  /** End a sequence of drawWorld calls (they share matrices / mode). */
  endWorld(): void {
    if (this.worldSetup) {
      this.gl.bindVertexArray(null);
      this.worldSetup = false;
    }
  }

  // ======================================================================== raw (cinematic) images
  /** 256x256 8-bit image for Draw_StretchRaw. */
  rawImage(image8: Uint8Array): void {
    this.changed();
    const gl = this.gl;
    if (!this.rawIdx) {
      this.rawIdx = gl.createTexture()!;
      gl.activeTexture(gl.TEXTURE2);
      gl.bindTexture(gl.TEXTURE_2D, this.rawIdx);
      gl.texStorage2D(gl.TEXTURE_2D, 1, gl.R8UI, 256, 256);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    }
    gl.activeTexture(gl.TEXTURE2);
    gl.bindTexture(gl.TEXTURE_2D, this.rawIdx);
    gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, 256, 256, gl.RED_INTEGER, gl.UNSIGNED_BYTE, image8, 0);
    gl.activeTexture(gl.TEXTURE0);
  }

  /** r_rawpalette (256 little-endian RGBA words). */
  rawPalette(pal: Uint32Array): void {
    for (let i = 0; i < 256; i++) {
      const v = pal[i]!;
      this.rawPalData[i * 4] = v & 255;
      this.rawPalData[i * 4 + 1] = (v >>> 8) & 255;
      this.rawPalData[i * 4 + 2] = (v >>> 16) & 255;
      this.rawPalData[i * 4 + 3] = (v >>> 24) & 255;
    }
    this.rawPalDirty = true;
  }

  /** Draw a quad with the raw image (vertices x,y,s,t in 2D, current matrices). */
  drawRaw(x0: number, y0: number, x1: number, y1: number, t: number): void {
    this.changed();
    const gl = this.gl;
    if (!this.rawPal) {
      this.rawPal = gl.createTexture()!;
      gl.activeTexture(gl.TEXTURE3);
      gl.bindTexture(gl.TEXTURE_2D, this.rawPal);
      gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA8, 256, 1);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    }
    if (this.rawPalDirty) {
      gl.activeTexture(gl.TEXTURE3);
      gl.bindTexture(gl.TEXTURE_2D, this.rawPal);
      gl.texSubImage2D(gl.TEXTURE_2D, 0, 0, 0, 256, 1, gl.RGBA, gl.UNSIGNED_BYTE, this.rawPalData, 0);
      this.rawPalDirty = false;
    }
    gl.activeTexture(gl.TEXTURE2);
    gl.bindTexture(gl.TEXTURE_2D, this.rawIdx);
    gl.activeTexture(gl.TEXTURE3);
    gl.bindTexture(gl.TEXTURE_2D, this.rawPal);
    gl.activeTexture(gl.TEXTURE0);
    // build the quad in the stream buffer and draw it with the raw program
    this.begin(GL_QUADS);
    this.texCoord2f(0, 0);
    this.vertex2f(x0, y0);
    this.texCoord2f(1, 0);
    this.vertex2f(x1, y0);
    this.texCoord2f(1, t);
    this.vertex2f(x1, y1);
    this.texCoord2f(0, t);
    this.vertex2f(x0, y1);
    this.end();
    this.applyCommon();
    const p = this.raw;
    this.useProgram(p.prog);
    this.computeMvp();
    gl.uniformMatrix4fv(p.u_mvp, false, this.mvp);
    gl.uniform1i(p.u_alphaTest, this.alphaTest ? 1 : 0);
    gl.uniform1f(p.u_alphaRef, this.alphaRef);
    gl.bindVertexArray(this.streamVao);
    gl.bindBuffer(gl.ARRAY_BUFFER, this.streamVbo);
    if (this.nverts * VFLOATS * 4 > this.streamVboBytes) {
      this.streamVboBytes = this.vcap * VFLOATS * 4;
      gl.bufferData(gl.ARRAY_BUFFER, this.streamVboBytes, gl.STREAM_DRAW);
    }
    gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.verts, 0, this.nverts * VFLOATS);
    if (this.nidx * 4 > this.streamIboBytes) {
      this.streamIboBytes = this.icap * 4;
      gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, this.streamIboBytes, gl.STREAM_DRAW);
    }
    gl.bufferSubData(gl.ELEMENT_ARRAY_BUFFER, 0, this.indices, 0, this.nidx);
    gl.drawElements(gl.TRIANGLES, this.nidx, gl.UNSIGNED_INT, 0);
    this.drawCalls++;
    gl.bindVertexArray(null);
    this.nverts = 0;
    this.nidx = 0;
  }

  // ======================================================================== misc
  finish(): void {
    this.flush();
    this.gl.finish();
  }

  /** glReadPixels(0, 0, w, h, GL_RGBA) of the drawing buffer (bottom row first). */
  readPixels(out: Uint8Array, w: number, h: number): void {
    this.flush();
    this.gl.readPixels(0, 0, w, h, this.gl.RGBA, this.gl.UNSIGNED_BYTE, out);
  }

  getError(): number {
    return this.gl.getError();
  }
}
