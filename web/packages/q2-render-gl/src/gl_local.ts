// Port of ref_gl/gl_local.h. The C globals of every ref_gl file live as fields of one GLState instance
// (docs/PORTING.md: "C globals become fields of an instance struct"); each ported .c file exports
// functions taking that state as their first argument.
import { CPlane } from 'q2-shared';
import type { DLight, LightStyle, Particle, RefDef, RefEntity, RefImport } from 'q2-ref';
import { MAX_LIGHTSTYLES } from 'q2-ref';
import { MAXLIGHTMAPS, Model, type MSurface } from './gl_model_h';
import type { QGL } from './qgl';
import { R_TURBSIN_RAW } from './generated/tables';

export const REF_VERSION = 'GL 0.01';

export const PITCH = 0;
export const YAW = 1;
export const ROLL = 2;

/** C: cvar_t as seen through refimport_t (the object returned by ri.Cvar_Get is live). */
export type Cvar = ReturnType<RefImport['cvarGet']>;

// C: gl_local.h imagetype_t
export const it_skin = 0;
export const it_sprite = 1;
export const it_wall = 2;
export const it_pic = 3;
export const it_sky = 4;

// C: gl_local.h image_t
export class Image {
  name = ''; // game path, including extension
  type = it_skin;
  width = 0; // source image
  height = 0;
  upload_width = 0; // after power of two and picmip
  upload_height = 0;
  registration_sequence = 0; // 0 = free
  texturechain: MSurface | null = null; // for sort-by-texture world drawing
  texnum = 0; // gl texture binding
  sl = 0; // 0,0 - 1,1 unless part of the scrap
  tl = 0;
  sh = 0;
  th = 0;
  scrap = false;
  has_alpha = false;
  paletted = false;

  clear(): void {
    this.name = '';
    this.type = it_skin;
    this.width = this.height = this.upload_width = this.upload_height = 0;
    this.registration_sequence = 0;
    this.texturechain = null;
    this.texnum = 0;
    this.sl = this.tl = this.sh = this.th = 0;
    this.scrap = this.has_alpha = this.paletted = false;
  }
}

export const TEXNUM_LIGHTMAPS = 1024;
export const TEXNUM_SCRAPS = 1152;
export const TEXNUM_IMAGES = 1153;
export const MAX_GLTEXTURES = 1024;

export const MAX_LBM_HEIGHT = 480;
export const BACKFACE_EPSILON = 0.01;

// C: gl_rsurf.c lightmap block constants
export const BLOCK_WIDTH = 128;
export const BLOCK_HEIGHT = 128;
export const MAX_LIGHTMAPS = 128;
export const LIGHTMAP_BYTES = 4;

// C: gl_local.h GL_RENDERER_* (only the ones the port can distinguish)
export const GL_RENDERER_VOODOO = 0x00000001;
export const GL_RENDERER_VOODOO2 = 0x00000002;
export const GL_RENDERER_POWERVR = 0x00000070;
export const GL_RENDERER_PERMEDIA2 = 0x00000100;
export const GL_RENDERER_3DLABS = 0x00000f00;
export const GL_RENDERER_RENDITION = 0x001c0000;
export const GL_RENDERER_MCD = 0x01000000;
export const GL_RENDERER_OTHER = 0x80000000 | 0;

/** All cvars ref_gl registers (C: the cvar_t* globals of gl_rmain.c / gl_image.c). */
export interface GLCvars {
  r_lefthand: Cvar;
  r_norefresh: Cvar;
  r_fullbright: Cvar;
  r_drawentities: Cvar;
  r_drawworld: Cvar;
  r_novis: Cvar;
  r_nocull: Cvar;
  r_lerpmodels: Cvar;
  r_speeds: Cvar;
  r_lightlevel: Cvar;
  gl_nosubimage: Cvar;
  gl_allow_software: Cvar;
  gl_particle_min_size: Cvar;
  gl_particle_max_size: Cvar;
  gl_particle_size: Cvar;
  gl_particle_att_a: Cvar;
  gl_particle_att_b: Cvar;
  gl_particle_att_c: Cvar;
  gl_modulate: Cvar;
  gl_log: Cvar;
  gl_bitdepth: Cvar;
  gl_mode: Cvar;
  gl_lightmap: Cvar;
  gl_shadows: Cvar;
  gl_dynamic: Cvar;
  gl_nobind: Cvar;
  gl_round_down: Cvar;
  gl_picmip: Cvar;
  gl_skymip: Cvar;
  gl_showtris: Cvar;
  gl_ztrick: Cvar;
  gl_finish: Cvar;
  gl_clear: Cvar;
  gl_cull: Cvar;
  gl_polyblend: Cvar;
  gl_flashblend: Cvar;
  gl_playermip: Cvar;
  gl_monolightmap: Cvar;
  gl_driver: Cvar;
  gl_texturemode: Cvar;
  gl_texturealphamode: Cvar;
  gl_texturesolidmode: Cvar;
  gl_lockpvs: Cvar;
  gl_vertex_arrays: Cvar;
  gl_ext_swapinterval: Cvar;
  gl_ext_palettedtexture: Cvar;
  gl_ext_multitexture: Cvar;
  gl_ext_pointparameters: Cvar;
  gl_ext_compiled_vertex_array: Cvar;
  gl_drawbuffer: Cvar;
  gl_swapinterval: Cvar;
  gl_saturatelighting: Cvar;
  gl_3dlabs_broken: Cvar;
  vid_fullscreen: Cvar;
  vid_gamma: Cvar;
  vid_ref: Cvar;
  intensity: Cvar;
}

/** C: gllightmapstate_t (gl_rsurf.c) */
export class GLLightmapState {
  internal_format = 0;
  current_lightmap_texture = 0;
  readonly lightmap_surfaces: (MSurface | null)[] = new Array<MSurface | null>(MAX_LIGHTMAPS).fill(null);
  readonly allocated = new Int32Array(BLOCK_WIDTH);
  // the lightmap texture data needs to be kept in main memory so texsubimage can update properly
  readonly lightmap_buffer = new Uint8Array(4 * BLOCK_WIDTH * BLOCK_HEIGHT);
}

function newLightStyle(): LightStyle {
  return { rgb: new Float32Array(3), white: 0 };
}

/** Internal refdef copy (C: r_newrefdef = *fd). Array members reference the client's arrays. */
export function newRefDef(): RefDef {
  const ls: LightStyle[] = [];
  for (let i = 0; i < MAX_LIGHTSTYLES; i++) ls.push(newLightStyle());
  return {
    x: 0,
    y: 0,
    width: 0,
    height: 0,
    fov_x: 0,
    fov_y: 0,
    vieworg: new Float32Array(3),
    viewangles: new Float32Array(3),
    blend: new Float32Array(4),
    time: 0,
    rdflags: 0,
    areabits: null,
    lightstyles: ls,
    num_entities: 0,
    entities: [] as RefEntity[],
    num_dlights: 0,
    dlights: [] as DLight[],
    num_particles: 0,
    particles: [] as Particle[],
  };
}

/** Synchronous file access over files prefetched by the async registration entry points. */
export interface FileCache {
  /** undefined: never fetched; null: does not exist */
  get(name: string): Uint8Array | null | undefined;
}

export class GLState {
  readonly ri: RefImport;
  /** WebGL2 backend (null when running the CPU side headless, e.g. in unit tests). */
  qgl: QGL | null = null;
  files: FileCache = { get: () => undefined };
  cv: GLCvars = null as unknown as GLCvars;

  // ---- gl_rmain.c
  readonly vid = { width: 0, height: 0 };
  r_worldmodel: Model | null = null;
  gldepthmin = 0;
  gldepthmax = 1;
  readonly gl_config = {
    renderer: 0,
    renderer_string: '',
    vendor_string: '',
    version_string: '',
    extensions_string: '',
    allow_cds: true,
  };
  readonly gl_state = {
    inverse_intensity: 1,
    lightmap_textures: 0,
    currenttextures: [0, 0],
    currenttmu: 0,
    camera_separation: 0,
    stereo_enabled: false,
    prev_mode: 3,
  };
  r_notexture: Image = null as unknown as Image;
  r_particletexture: Image = null as unknown as Image;
  currententity: RefEntity = null as unknown as RefEntity;
  currentmodel: Model = null as unknown as Model;
  readonly frustum: CPlane[] = [new CPlane(), new CPlane(), new CPlane(), new CPlane()];
  r_visframecount = 0;
  r_framecount = 0;
  c_brush_polys = 0;
  c_alias_polys = 0;
  readonly v_blend = new Float32Array(4);
  readonly vup = new Float32Array(3);
  readonly vpn = new Float32Array(3);
  readonly vright = new Float32Array(3);
  readonly r_origin = new Float32Array(3);
  readonly r_world_matrix = new Float32Array(16);
  readonly r_newrefdef: RefDef = newRefDef();
  r_viewcluster = 0;
  r_viewcluster2 = 0;
  r_oldviewcluster = 0;
  r_oldviewcluster2 = 0;
  readonly r_rawpalette = new Uint32Array(256);
  trickframe = 0;
  screenshotCount = 0;
  /** Port: receives the PNG of the "screenshot" command instead of a download. */
  screenshotHook: ((name: string, blob: Blob) => void) | null = null;

  // ---- gl_image.c
  readonly gltextures: Image[] = [];
  numgltextures = 0;
  readonly intensitytable = new Uint8Array(256);
  readonly gammatable = new Uint8Array(256);
  readonly d_8to24table = new Uint32Array(256);
  gl_solid_format = 3;
  gl_alpha_format = 4;
  gl_tex_solid_format = 3;
  gl_tex_alpha_format = 4;
  gl_filter_min = 0x2701; // GL_LINEAR_MIPMAP_NEAREST
  gl_filter_max = 0x2601; // GL_LINEAR
  readonly scrap_allocated: Int32Array[] = [new Int32Array(256)];
  readonly scrap_texels: Uint8Array[] = [new Uint8Array(256 * 256)];
  scrap_dirty = false;
  scrap_uploads = 0;
  upload_width = 0;
  upload_height = 0;
  uploaded_paletted = false;
  readonly texEnvLastmodes = [-1, -1];

  // ---- gl_model.c
  loadmodel: Model = null as unknown as Model;
  modfilelen = 0;
  readonly mod_novis = new Uint8Array(65536 / 8);
  readonly mod_known: Model[] = [];
  mod_numknown = 0;
  readonly mod_inline: Model[] = [];
  registration_sequence = 0;
  readonly mod_decompressed = new Uint8Array(65536 / 8);

  // ---- gl_rsurf.c
  readonly modelorg = new Float32Array(3);
  r_alpha_surfaces: MSurface | null = null;
  c_visible_lightmaps = 0;
  c_visible_textures = 0;
  readonly gl_lms = new GLLightmapState();
  readonly fatvis = new Uint8Array(65536 / 8);
  /**
   * Port: CPU mirrors of the lightmap pages. lmCanon[i] is what C keeps in GL texture lightmap_textures+i;
   * lmDisp[i] is what the TEXTURE_2D_ARRAY layer i holds (it additionally contains the per-frame lightmaps
   * C would have drawn from the dynamic block 0, see gl_rsurf.ts). lmDirty holds x0,y0,x1,y1 per page.
   */
  readonly lmCanon: Uint8Array[] = [];
  readonly lmDisp: Uint8Array[] = [];
  readonly lmDirty = new Int32Array(MAX_LIGHTMAPS * 4);
  lmNumPages = 0;
  /** Port: lightmap swizzle of GL_BeginBuildingLightmaps' internal_format (LMSWZ_*). */
  lmSwizzle = 0;
  /** C: GL_BeginBuildingLightmaps static lightstyles (all 1.0, white 3). */
  readonly buildLightstyles: LightStyle[] = [];
  /** Port: surfaces handed to R_RenderBrushPoly this pass, flushed as (texture, lightmap page) batches. */
  numPending = 0;
  pendTex = new Int32Array(0);
  pendKey = new Int32Array(0);
  pendTex2 = new Int32Array(0);
  pendKey2 = new Int32Array(0);
  readonly sortCountsTex = new Int32Array(TEXNUM_IMAGES + MAX_GLTEXTURES + 1);
  /** Port: per-frame world index stream and batch lists (preallocated, see gl_rsurf.ts). */
  worldIndices = new Uint32Array(0);
  worldIndexCount = 0;
  readonly batchTex = new Int32Array(8192);
  readonly batchPage = new Int32Array(8192);
  readonly batchFlow = new Uint8Array(8192);
  readonly batchFirst = new Int32Array(8192);
  readonly batchCount = new Int32Array(8192);
  numBatches = 0;
  sortSurfs: MSurface[] = [];
  sortSurfs2: MSurface[] = [];
  readonly sortCounts = new Int32Array(MAX_LIGHTMAPS * 2 + 1);
  /** Port: LightMapContext bound to this state (created by gl_rsurf.ts). */
  lmctx: unknown = null;

  // ---- gl_light.c
  r_dlightframecount = 0;
  readonly pointcolor = new Float32Array(3);
  lightplane: CPlane | null = null;
  readonly lightspot = new Float32Array(3);
  readonly s_blocklights = new Float32Array(34 * 34 * 3);

  // ---- gl_warp.c
  skyname = '';
  skyrotate = 0;
  readonly skyaxis = new Float32Array(3);
  readonly sky_images: Image[] = [];
  warpface: MSurface = null as unknown as MSurface;
  c_sky = 0;
  /** skymins[2][6] row-major */
  readonly skymins = new Float32Array(12);
  readonly skymaxs = new Float32Array(12);
  sky_min = 0;
  sky_max = 0;
  /** r_turbsin (R_Init halves the table in place) */
  readonly r_turbsin = Float32Array.from(R_TURBSIN_RAW);

  // ---- gl_mesh.c
  /** s_lerped[MAX_VERTS][4] */
  readonly s_lerped = new Float32Array(2048 * 4);
  readonly shadevector = new Float32Array(3);
  readonly shadelight = new Float32Array(3);
  /** row of r_avertexnormal_dots selected by the entity yaw (offset in floats) */
  shadedots = 0;

  // ---- gl_draw.c
  draw_chars: Image | null = null;
  /** Draw_StretchRaw image8[256*256] */
  readonly rawImage8 = new Uint8Array(256 * 256);
  /** Port: asks the embedding Refresh to fetch a file that a synchronous lookup found missing. */
  requestFile: ((name: string) => void) | null = null;

  constructor(ri: RefImport) {
    this.ri = ri;
    for (let i = 0; i < MAX_GLTEXTURES; i++) this.gltextures.push(new Image());
    for (let i = 0; i < MAX_LIGHTSTYLES; i++) {
      const ls = newLightStyle();
      ls.rgb[0] = ls.rgb[1] = ls.rgb[2] = 1;
      ls.white = 3;
      this.buildLightstyles.push(ls);
    }
    for (let i = 0; i < 512; i++) {
      this.mod_known.push(new Model());
      this.mod_inline.push(new Model());
    }
  }
}

export { MAXLIGHTMAPS };
