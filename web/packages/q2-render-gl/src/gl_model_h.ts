// Port of ref_gl/gl_model.h: in-memory model structures (brush, alias, sprite).
// Pointers become object references; `short` fields are wrapped explicitly where C would wrap.
import { CPlane } from 'q2-shared';
import type { Md2Model, Sp2Sprite } from 'q2-formats';
import type { Image } from './gl_local';

export const MAX_MD2SKINS = 32;
export const MAXLIGHTMAPS = 4;

// C: gl_model.h mmodel_t
export class MModel {
  readonly mins = new Float32Array(3);
  readonly maxs = new Float32Array(3);
  readonly origin = new Float32Array(3); // for sounds or lights
  radius = 0;
  headnode = 0;
  visleafs = 0; // not including the solid leaf 0
  firstface = 0;
  numfaces = 0;
}

export const SIDE_FRONT = 0;
export const SIDE_BACK = 1;
export const SIDE_ON = 2;

export const SURF_PLANEBACK = 2;
export const SURF_DRAWSKY = 4;
export const SURF_DRAWTURB = 0x10;
export const SURF_DRAWBACKGROUND = 0x40;
export const SURF_UNDERWATER = 0x80;

// C: gl_model.h mtexinfo_t
export class MTexinfo {
  /** vecs[2][4] row-major */
  readonly vecs = new Float32Array(8);
  flags = 0;
  numframes = 0;
  next: MTexinfo | null = null; // animation chain
  image: Image = null as unknown as Image;
}

export const VERTEXSIZE = 7;

// C: gl_model.h glpoly_t. verts holds numverts * VERTEXSIZE floats (xyz s1t1 s2t2).
export class GLPoly {
  next: GLPoly | null = null;
  chain: GLPoly | null = null;
  numverts = 0;
  flags = 0; // for SURF_UNDERWATER (not needed anymore?)
  verts: Float32Array;
  /** Port addition: first vertex of this poly in the world model's static vertex buffer (-1 = not in it). */
  firstVertex = -1;
  constructor(numverts: number, verts?: Float32Array) {
    this.numverts = numverts;
    this.verts = verts ?? new Float32Array(numverts * VERTEXSIZE);
  }
}

// C: gl_model.h msurface_t
export class MSurface {
  visframe = 0; // should be drawn when node is crossed
  plane: CPlane = null as unknown as CPlane;
  flags = 0;
  firstedge = 0; // look up in model->surfedges[], negative numbers
  numedges = 0; // are backwards edges
  /** short texturemins[2] */
  readonly texturemins = new Int16Array(2);
  /** short extents[2] */
  readonly extents = new Int16Array(2);
  light_s = 0; // gl lightmap coordinates
  light_t = 0;
  dlight_s = 0; // gl lightmap coordinates for dynamic lightmaps
  dlight_t = 0;
  polys: GLPoly | null = null; // multiple if warped
  texturechain: MSurface | null = null;
  lightmapchain: MSurface | null = null;
  texinfo: MTexinfo = null as unknown as MTexinfo;
  // lighting info
  dlightframe = 0;
  dlightbits = 0;
  lightmaptexturenum = 0;
  readonly styles = new Uint8Array(MAXLIGHTMAPS);
  readonly cached_light = new Float32Array(MAXLIGHTMAPS); // values currently used in lightmap
  /** [numstyles*surfsize] view into the model's light data, or null */
  samples: Uint8Array | null = null;
  /**
   * Port addition: the displayed lightmap page region currently holds a per-frame lightmap (C would have
   * drawn it from the dynamic lightmap block 0) and must be restored from the canonical page when the
   * surface stops being dynamic. See gl_rsurf.ts.
   */
  lmTemp = false;
}

// C: gl_model.h mnode_t and mleaf_t share one class here (contents == -1 for nodes).
export class MNode {
  // common with leaf
  contents = 0; // -1, to differentiate from leafs
  visframe = 0; // node needs to be traversed if current
  readonly minmaxs = new Float32Array(6); // for bounding box culling
  /** port: views of minmaxs[0..2] / minmaxs[3..5] (C passes node->minmaxs, node->minmaxs+3) */
  readonly mins = this.minmaxs.subarray(0, 3);
  readonly maxs = this.minmaxs.subarray(3, 6);
  parent: MNode | null = null;
  // node specific
  plane: CPlane = null as unknown as CPlane;
  readonly children: [MNode, MNode] = [null as unknown as MNode, null as unknown as MNode];
  firstsurface = 0; // unsigned short
  numsurfaces = 0; // unsigned short
  // leaf specific
  cluster = 0;
  area = 0;
  /** index into model.marksurfaces */
  firstmarksurface = 0;
  nummarksurfaces = 0;
}
export type MLeaf = MNode;

export const mod_bad = 0;
export const mod_brush = 1;
export const mod_sprite = 2;
export const mod_alias = 3;

/** Decoded MD2 (the C keeps the byte-swapped dmdl_t in extradata). */
export interface AliasData {
  md2: Md2Model;
  /** glcmds as raw int words (float s/t stored as bit patterns), shared view */
  glcmds: Int32Array;
  /** float view over the same glcmd words */
  glcmdsF: Float32Array;
}

// C: gl_model.h model_t
export class Model {
  name = '';
  registration_sequence = 0;
  type = mod_bad;
  numframes = 0;
  flags = 0;
  // volume occupied by the model graphics
  readonly mins = new Float32Array(3);
  readonly maxs = new Float32Array(3);
  radius = 0;
  // solid volume for clipping
  clipbox = false;
  readonly clipmins = new Float32Array(3);
  readonly clipmaxs = new Float32Array(3);
  // brush model
  firstmodelsurface = 0;
  nummodelsurfaces = 0;
  lightmap = 0; // only for submodels
  numsubmodels = 0;
  submodels: MModel[] = [];
  numplanes = 0;
  planes: CPlane[] = [];
  numleafs = 0; // number of visible leafs, not counting 0
  leafs: MNode[] = [];
  numvertexes = 0;
  /** mvertex_t position[3] per vertex */
  vertexes: Float32Array = new Float32Array(0);
  numedges = 0;
  /** medge_t v[2] per edge (unsigned short) */
  edges: Uint16Array = new Uint16Array(0);
  numnodes = 0;
  firstnode = 0;
  nodes: MNode[] = [];
  numtexinfo = 0;
  texinfo: MTexinfo[] = [];
  numsurfaces = 0;
  surfaces: MSurface[] = [];
  numsurfedges = 0;
  surfedges: Int32Array = new Int32Array(0);
  nummarksurfaces = 0;
  marksurfaces: MSurface[] = [];
  /** dvis_t: raw visibility lump (numclusters + bitofs + rows) or null */
  vis: Uint8Array | null = null;
  visNumclusters = 0;
  /** bitofs[cluster][2] */
  visBitofs: Int32Array = new Int32Array(0);
  lightdata: Uint8Array | null = null;
  // for alias models and skins
  skins: (Image | null)[] = new Array<Image | null>(MAX_MD2SKINS).fill(null);
  extradatasize = 0;
  /** mod_alias extradata */
  alias: AliasData | null = null;
  /** mod_sprite extradata */
  sprite: Sp2Sprite | null = null;
  /** Port addition: static vertex data of all GL_BuildPolygonFromSurface polys (world model only). */
  worldVerts: Float32Array | null = null;
  /** Port addition: backend handle of the uploaded static vertex buffer. */
  worldVbo: unknown = null;

  /** C struct assignment `*this = *o` (shallow: arrays and objects are shared like the C pointers). */
  assign(o: Model): this {
    Object.assign(this, o);
    // the fixed-size vectors are owned per model_t in C (copied by value)
    (this as { mins: Float32Array }).mins = Float32Array.from(o.mins);
    (this as { maxs: Float32Array }).maxs = Float32Array.from(o.maxs);
    (this as { clipmins: Float32Array }).clipmins = Float32Array.from(o.clipmins);
    (this as { clipmaxs: Float32Array }).clipmaxs = Float32Array.from(o.clipmaxs);
    this.skins = o.skins.slice();
    return this;
  }

  /** C memset(mod, 0, sizeof(*mod)) */
  clear(): void {
    this.assign(new Model());
  }
}
