// Port of ref_gl/gl_model.c: model loading and caching (brush, alias, sprite) and the registration API.
// Lump decoding uses q2-formats (parseBsp / parseMd2 / parseSp2); the in-memory structures and every
// check / quirk of the C loaders are reproduced here.
import {
  BSPVERSION,
  CPlane,
  ERR_DROP,
  IDALIASHEADER,
  IDBSPHEADER,
  IDSPRITEHEADER,
  MAX_MAP_SURFEDGES,
  MAX_SKINNAME,
  PRINT_ALL,
  SURF_SKY,
  SURF_TRANS33,
  SURF_TRANS66,
  SURF_WARP,
  VectorCopy,
  VectorLength,
  atoi,
  fr,
  readCString,
} from 'q2-shared';
import { FormatError, parseBsp, parseMd2, parseSp2, type BspFile } from 'q2-formats';
import { GLState, it_skin, it_sprite, it_wall } from './gl_local';
import {
  MAXLIGHTMAPS,
  MModel,
  MNode,
  MSurface,
  MTexinfo,
  Model,
  SURF_DRAWTURB,
  SURF_PLANEBACK,
  mod_alias,
  mod_brush,
  mod_sprite,
} from './gl_model_h';
import { GL_FindImage, GL_FreeUnusedImages } from './gl_image';
import {
  GL_BeginBuildingLightmaps,
  GL_BuildPolygonFromSurface,
  GL_CreateSurfaceLightmap,
  GL_EndBuildingLightmaps,
  GL_UploadWorldGeometry,
} from './gl_rsurf';
import { GL_SubdivideSurface } from './gl_warp';

export const MAX_MOD_KNOWN = 512;

function sysErr(r: GLState, e: unknown): never {
  if (e instanceof FormatError) r.ri.sysError(ERR_DROP, e.message);
  throw e;
}

// C: gl_model.c:48 Mod_PointInLeaf
export function Mod_PointInLeaf(r: GLState, p: ArrayLike<number>, model: Model | null): MNode {
  if (!model || !model.nodes.length) r.ri.sysError(ERR_DROP, 'Mod_PointInLeaf: bad model');
  let node = model.nodes[0]!;
  while (1) {
    if (node.contents !== -1) return node;
    const plane = node.plane;
    const d = fr(
      fr(fr(fr(p[0]! * plane.normal[0]!) + fr(p[1]! * plane.normal[1]!)) + fr(p[2]! * plane.normal[2]!)) -
        plane.dist,
    );
    if (d > 0) node = node.children[0];
    else node = node.children[1];
  }
  return node; // never reached
}

// C: gl_model.c:79 Mod_DecompressVis. `inofs` is the offset of the compressed row in model.vis, or -1 (NULL).
export function Mod_DecompressVis(r: GLState, inofs: number, model: Model): Uint8Array {
  const decompressed = r.mod_decompressed;
  let row = (model.visNumclusters + 7) >> 3;
  let out = 0;

  if (inofs < 0 || !model.vis) {
    // no vis info, so make all visible
    while (row) {
      decompressed[out++] = 0xff;
      row--;
    }
    return decompressed;
  }
  const vis = model.vis;
  // port: memory safety -- C reads past the vis lump (and overruns the static row) for a truncated or
  // malformed row; here the row ends at the lump end / buffer end and the rest stays invisible.
  if (row > decompressed.length) row = decompressed.length;
  let inp = inofs;
  do {
    if (inp >= vis.length) {
      decompressed.fill(0, out, row);
      break;
    }
    if (vis[inp]) {
      decompressed[out++] = vis[inp++]!;
      continue;
    }
    let c = vis[inp + 1] ?? 0;
    inp += 2;
    if (c > row - out) c = row - out;
    while (c) {
      decompressed[out++] = 0;
      c--;
    }
  } while (out < row);
  return decompressed;
}

// C: gl_model.c:124 Mod_ClusterPVS
export function Mod_ClusterPVS(r: GLState, cluster: number, model: Model): Uint8Array {
  if (cluster === -1 || !model.vis) return r.mod_novis;
  // port: memory safety -- a leaf cluster beyond dvis_t.numclusters reads garbage offsets in C
  if (cluster < 0 || cluster >= model.visNumclusters) return r.mod_novis;
  return Mod_DecompressVis(r, model.visBitofs[cluster * 2]!, model);
}

// C: gl_model.c:140 Mod_Modellist_f
export function Mod_Modellist_f(r: GLState): void {
  let total = 0;
  r.ri.conPrintf(PRINT_ALL, 'Loaded models:\n');
  for (let i = 0; i < r.mod_numknown; i++) {
    const mod = r.mod_known[i]!;
    if (!mod.name) continue;
    r.ri.conPrintf(PRINT_ALL, `${String(mod.extradatasize).padStart(8)} : ${mod.name}\n`);
    total += mod.extradatasize;
  }
  r.ri.conPrintf(PRINT_ALL, `Total resident: ${total}\n`);
}

// C: gl_model.c:163 Mod_Init
export function Mod_Init(r: GLState): void {
  r.mod_novis.fill(0xff);
}

// C: gl_model.c:177 Mod_ForName -- loads in a model for the given name (file must be prefetched)
export function Mod_ForName(r: GLState, name: string, crash: boolean): Model | null {
  if (!name) r.ri.sysError(ERR_DROP, 'Mod_ForName: NULL name');

  // inline models are grabbed only from worldmodel
  if (name[0] === '*') {
    const i = atoi(name.slice(1));
    if (i < 1 || !r.r_worldmodel || i >= r.r_worldmodel.numsubmodels)
      r.ri.sysError(ERR_DROP, 'bad inline model number');
    return r.mod_inline[i]!;
  }

  // search the currently loaded models
  let i;
  for (i = 0; i < r.mod_numknown; i++) {
    const mod = r.mod_known[i]!;
    if (!mod.name) continue;
    if (mod.name === name) return mod;
  }

  // find a free model slot spot
  for (i = 0; i < r.mod_numknown; i++) {
    if (!r.mod_known[i]!.name) break; // free spot
  }
  if (i === r.mod_numknown) {
    if (r.mod_numknown === MAX_MOD_KNOWN) r.ri.sysError(ERR_DROP, 'mod_numknown == MAX_MOD_KNOWN');
    r.mod_numknown++;
  }
  const mod = r.mod_known[i]!;
  mod.name = name.slice(0, 63);

  // load the file
  const buf = r.files.get(mod.name);
  if (!buf) {
    if (crash) r.ri.sysError(ERR_DROP, `Mod_NumForName: ${mod.name} not found`);
    mod.name = '';
    return null;
  }
  r.modfilelen = buf.length;
  r.loadmodel = mod;

  // call the apropriate loader
  const ident = buf.length >= 4 ? (buf[0]! | (buf[1]! << 8) | (buf[2]! << 16) | (buf[3]! << 24)) >>> 0 : 0;
  switch (ident) {
    case IDALIASHEADER:
      Mod_LoadAliasModel(r, mod, buf);
      break;
    case IDSPRITEHEADER:
      Mod_LoadSpriteModel(r, mod, buf);
      break;
    case IDBSPHEADER:
      Mod_LoadBrushModel(r, mod, buf);
      break;
    default:
      r.ri.sysError(ERR_DROP, `Mod_NumForName: unknown fileid for ${mod.name}`);
  }
  r.loadmodel.extradatasize = buf.length;
  return mod;
}

// ===============================================================================
// BRUSHMODEL LOADING

// C: gl_model.c:290 Mod_LoadLighting
function Mod_LoadLighting(r: GLState, bsp: BspFile): void {
  r.loadmodel.lightdata = bsp.lighting.length ? bsp.lighting : null;
}

// C: gl_model.c:307 Mod_LoadVisibility
function Mod_LoadVisibility(r: GLState, bsp: BspFile): void {
  const l = bsp.visibility;
  if (!l.length) {
    r.loadmodel.vis = null;
    return;
  }
  const m = r.loadmodel;
  m.vis = l;
  const dv = new DataView(l.buffer, l.byteOffset, l.byteLength);
  m.visNumclusters = dv.getInt32(0, true);
  m.visBitofs = new Int32Array(m.visNumclusters * 2);
  for (let i = 0; i < m.visNumclusters; i++) {
    m.visBitofs[i * 2] = dv.getInt32(4 + i * 8, true);
    m.visBitofs[i * 2 + 1] = dv.getInt32(8 + i * 8, true);
  }
}

// C: gl_model.c:333 Mod_LoadVertexes
function Mod_LoadVertexes(r: GLState, bsp: BspFile): void {
  r.loadmodel.vertexes = bsp.vertexes;
  r.loadmodel.numvertexes = bsp.numvertexes;
}

// C: gl_model.c:361 RadiusFromBounds
export function RadiusFromBounds(mins: ArrayLike<number>, maxs: ArrayLike<number>): number {
  const corner = new Float32Array(3);
  for (let i = 0; i < 3; i++) {
    corner[i] = Math.abs(mins[i]!) > Math.abs(maxs[i]!) ? Math.abs(mins[i]!) : Math.abs(maxs[i]!);
  }
  return VectorLength(corner);
}

// C: gl_model.c:380 Mod_LoadSubmodels
function Mod_LoadSubmodels(r: GLState, bsp: BspFile): void {
  const m = bsp.models;
  const out: MModel[] = [];
  for (let i = 0; i < m.count; i++) {
    const o = new MModel();
    for (let j = 0; j < 3; j++) {
      // spread the mins / maxs by a pixel
      o.mins[j] = m.mins[i * 3 + j]! - 1;
      o.maxs[j] = m.maxs[i * 3 + j]! + 1;
      o.origin[j] = m.origin[i * 3 + j]!;
    }
    o.radius = RadiusFromBounds(o.mins, o.maxs);
    o.headnode = m.headnode[i]!;
    o.firstface = m.firstface[i]!;
    o.numfaces = m.numfaces[i]!;
    out.push(o);
  }
  r.loadmodel.submodels = out;
  r.loadmodel.numsubmodels = m.count;
}

// C: gl_model.c:415 Mod_LoadEdges
function Mod_LoadEdges(r: GLState, bsp: BspFile): void {
  // count + 1 entries like C's Hunk_Alloc ((count + 1) * sizeof(*out))
  const e = new Uint16Array((bsp.numedges + 1) * 2);
  e.set(bsp.edges.subarray(0, bsp.numedges * 2));
  r.loadmodel.edges = e;
  r.loadmodel.numedges = bsp.numedges;
}

// C: gl_model.c:442 Mod_LoadTexinfo
function Mod_LoadTexinfo(r: GLState, bsp: BspFile): void {
  const t = bsp.texinfo;
  const count = t.count;
  const out: MTexinfo[] = [];
  for (let i = 0; i < count; i++) out.push(new MTexinfo());
  r.loadmodel.texinfo = out;
  r.loadmodel.numtexinfo = count;

  for (let i = 0; i < count; i++) {
    const o = out[i]!;
    for (let j = 0; j < 8; j++) o.vecs[j] = t.vecs[i * 8 + j]!;
    o.flags = t.flags[i]!;
    const next = t.nexttexinfo[i]!;
    o.next = next > 0 ? (out[next] ?? null) : null; // port: memory safety (C: no bounds check)
    const name = texinfoImageName(t.texture[i]!);
    const image = GL_FindImage(r, name, it_wall);
    if (!image) {
      r.ri.conPrintf(PRINT_ALL, `Couldn't load ${name}\n`);
      o.image = r.r_notexture;
    } else o.image = image;
  }

  // count animation frames
  for (let i = 0; i < count; i++) {
    const o = out[i]!;
    o.numframes = 1;
    // port: a chain that cycles without returning to `o` loops forever in C; no real chain is longer
    // than the texinfo count
    for (let step = o.next; step && step !== o && o.numframes < count; step = step.next) o.numframes++;
  }
}

/** C: Com_sprintf (name, sizeof(name), "textures/%s.wal", in->texture) */
export function texinfoImageName(texture: string): string {
  return `textures/${texture}.wal`.slice(0, 63);
}

/**
 * C: gl_model.c:497 CalcSurfaceExtents -- fills in s->texturemins[] and s->extents[].
 * Float expressions are evaluated in single precision per operation like the x86-64 build.
 */
export function CalcSurfaceExtents(
  s: MSurface,
  surfedges: Int32Array,
  edges: Uint16Array,
  vertexes: Float32Array,
): void {
  const mins0 = new Float32Array(2);
  const maxs0 = new Float32Array(2);
  mins0[0] = mins0[1] = 999999;
  maxs0[0] = maxs0[1] = -99999;
  const tex = s.texinfo.vecs;

  for (let i = 0; i < s.numedges; i++) {
    const e = surfedges[s.firstedge + i]!;
    const vi = e >= 0 ? edges[e * 2]! : edges[-e * 2 + 1]!;
    const x = vertexes[vi * 3]!;
    const y = vertexes[vi * 3 + 1]!;
    const z = vertexes[vi * 3 + 2]!;
    for (let j = 0; j < 2; j++) {
      const val = fr(
        fr(fr(fr(x * tex[j * 4]!) + fr(y * tex[j * 4 + 1]!)) + fr(z * tex[j * 4 + 2]!)) + tex[j * 4 + 3]!,
      );
      if (val < mins0[j]!) mins0[j] = val;
      if (val > maxs0[j]!) maxs0[j] = val;
    }
  }

  for (let i = 0; i < 2; i++) {
    const bmins = Math.floor(fr(mins0[i]! / 16));
    const bmaxs = Math.ceil(fr(maxs0[i]! / 16));
    s.texturemins[i] = bmins * 16;
    s.extents[i] = (bmaxs - bmins) * 16;
  }
}

// C: gl_model.c:555 Mod_LoadFaces
function Mod_LoadFaces(r: GLState, bsp: BspFile): void {
  const f = bsp.faces;
  const count = f.count;
  const m = r.loadmodel;
  const out: MSurface[] = [];
  for (let i = 0; i < count; i++) out.push(new MSurface());
  m.surfaces = out;
  r.warpPolys = 0;
  m.numsurfaces = count;

  r.currentmodel = m;

  GL_BeginBuildingLightmaps(r, m);

  for (let surfnum = 0; surfnum < count; surfnum++) {
    const o = out[surfnum]!;
    o.firstedge = f.firstedge[surfnum]!;
    o.numedges = f.numedges[surfnum]!;
    o.flags = 0;
    o.polys = null;

    const planenum = (f.planenum[surfnum]! << 16) >> 16; // LittleShort: signed
    const side = f.side[surfnum]!;
    if (side) o.flags |= SURF_PLANEBACK;
    if (planenum < 0 || planenum >= m.numplanes) r.ri.sysError(ERR_DROP, 'MOD_LoadBmodel: bad planenum'); // port: memory safety
    o.plane = m.planes[planenum]!;

    const ti = f.texinfo[surfnum]!;
    if (ti < 0 || ti >= m.numtexinfo) r.ri.sysError(ERR_DROP, 'MOD_LoadBmodel: bad texinfo number');
    o.texinfo = m.texinfo[ti]!;

    CalcSurfaceExtents(o, m.surfedges, m.edges, m.vertexes);

    // lighting info
    for (let i = 0; i < MAXLIGHTMAPS; i++) o.styles[i] = f.styles[surfnum * 4 + i]!;
    const lo = f.lightofs[surfnum]!;
    if (lo === -1 || !m.lightdata) o.samples = null;
    else o.samples = m.lightdata.subarray(lo);

    // set the drawing flags
    if (o.texinfo.flags & SURF_WARP) {
      o.flags |= SURF_DRAWTURB;
      for (let i = 0; i < 2; i++) {
        o.extents[i] = 16384;
        o.texturemins[i] = -8192;
      }
      GL_SubdivideSurface(r, o); // cut up polygon for warps
    }

    // create lightmaps and polygons
    if (!(o.texinfo.flags & (SURF_SKY | SURF_TRANS33 | SURF_TRANS66 | SURF_WARP)))
      GL_CreateSurfaceLightmap(r, o);

    if (!(o.texinfo.flags & SURF_WARP)) GL_BuildPolygonFromSurface(r, o);
  }

  GL_EndBuildingLightmaps(r);
}

/** port: deepest node tree accepted (qbsp trees are a few dozen levels; a cycle recurses forever in C) */
const MAX_NODE_DEPTH = 1024;

// C: gl_model.c:638 Mod_SetParent. Port: memory safety -- a node reached twice (a cycle, or a shared
// subtree that makes every recursive walk exponential) is rejected; C recurses forever / freezes.
function Mod_SetParent(r: GLState, node: MNode, parent: MNode | null, seen: Set<MNode>, depth = 0): void {
  if (depth > MAX_NODE_DEPTH) r.ri.sysError(ERR_DROP, 'Mod_SetParent: bad node tree');
  node.parent = parent;
  if (node.contents !== -1) return;
  if (seen.has(node)) r.ri.sysError(ERR_DROP, 'Mod_SetParent: bad node tree');
  seen.add(node);
  Mod_SetParent(r, node.children[0], node, seen, depth + 1);
  Mod_SetParent(r, node.children[1], node, seen, depth + 1);
}

// C: gl_model.c:652 Mod_LoadNodes
function Mod_LoadNodes(r: GLState, bsp: BspFile): void {
  const n = bsp.nodes;
  const m = r.loadmodel;
  const out: MNode[] = [];
  for (let i = 0; i < n.count; i++) out.push(new MNode());
  m.nodes = out;
  m.numnodes = n.count;

  for (let i = 0; i < n.count; i++) {
    const o = out[i]!;
    for (let j = 0; j < 3; j++) {
      o.minmaxs[j] = n.mins[i * 3 + j]!;
      o.minmaxs[3 + j] = n.maxs[i * 3 + j]!;
    }
    const p = n.planenum[i]!;
    if (p < 0 || p >= m.numplanes) r.ri.sysError(ERR_DROP, 'MOD_LoadBmodel: bad planenum'); // port: memory safety
    o.plane = m.planes[p]!;
    o.firstsurface = n.firstface[i]!;
    o.numsurfaces = n.numfaces[i]!;
    if (o.firstsurface + o.numsurfaces > m.numsurfaces)
      r.ri.sysError(ERR_DROP, 'MOD_LoadBmodel: bad node surfaces'); // port: memory safety
    o.contents = -1; // differentiate from leafs
    for (let j = 0; j < 2; j++) {
      const c = n.children[i * 2 + j]!;
      const child = c >= 0 ? out[c] : m.leafs[-1 - c];
      if (!child) r.ri.sysError(ERR_DROP, 'MOD_LoadBmodel: bad node child'); // port: memory safety
      o.children[j] = child;
    }
  }
  if (out.length) Mod_SetParent(r, out[0]!, null, new Set()); // sets nodes and leafs
}

// C: gl_model.c:700 Mod_LoadLeafs
function Mod_LoadLeafs(r: GLState, bsp: BspFile): void {
  const l = bsp.leafs;
  const m = r.loadmodel;
  const out: MNode[] = [];
  for (let i = 0; i < l.count; i++) {
    const o = new MNode();
    for (let j = 0; j < 3; j++) {
      o.minmaxs[j] = l.mins[i * 3 + j]!;
      o.minmaxs[3 + j] = l.maxs[i * 3 + j]!;
    }
    o.contents = l.contents[i]!;
    o.cluster = l.cluster[i]!;
    o.area = l.area[i]!;
    o.firstmarksurface = (l.firstleafface[i]! << 16) >> 16; // LittleShort: signed
    o.nummarksurfaces = (l.numleaffaces[i]! << 16) >> 16;
    if (o.firstmarksurface < 0 || o.firstmarksurface + Math.max(0, o.nummarksurfaces) > m.nummarksurfaces) {
      r.ri.sysError(ERR_DROP, 'Mod_LoadLeafs: bad marksurfaces'); // port: memory safety
    }
    out.push(o);
  }
  m.leafs = out;
  m.numleafs = l.count;
}

// C: gl_model.c:754 Mod_LoadMarksurfaces
function Mod_LoadMarksurfaces(r: GLState, bsp: BspFile): void {
  const lf = bsp.leaffaces;
  const m = r.loadmodel;
  const out: MSurface[] = [];
  for (let i = 0; i < lf.length; i++) {
    const j = (lf[i]! << 16) >> 16;
    if (j < 0 || j >= m.numsurfaces) r.ri.sysError(ERR_DROP, 'Mod_ParseMarksurfaces: bad surface number');
    out.push(m.surfaces[j]!);
  }
  m.marksurfaces = out;
  m.nummarksurfaces = lf.length;
}

// C: gl_model.c:783 Mod_LoadSurfedges
function Mod_LoadSurfedges(r: GLState, bsp: BspFile): void {
  const count = bsp.surfedges.length;
  if (count < 1 || count >= MAX_MAP_SURFEDGES) {
    r.ri.sysError(ERR_DROP, `MOD_LoadBmodel: bad surfedges count in ${r.loadmodel.name}: ${count}`);
  }
  r.loadmodel.surfedges = bsp.surfedges;
  r.loadmodel.numsurfedges = count;
}

// C: gl_model.c:811 Mod_LoadPlanes
function Mod_LoadPlanes(r: GLState, bsp: BspFile): void {
  const p = bsp.planes;
  const out: CPlane[] = [];
  for (let i = 0; i < p.count; i++) {
    const o = new CPlane();
    let bits = 0;
    for (let j = 0; j < 3; j++) {
      o.normal[j] = p.normal[i * 3 + j]!;
      if (o.normal[j]! < 0) bits |= 1 << j;
    }
    o.dist = p.dist[i]!;
    o.type = p.type[i]! & 255;
    o.signbits = bits;
    out.push(o);
  }
  r.loadmodel.planes = out;
  r.loadmodel.numplanes = p.count;
}

// C: gl_model.c:849 Mod_LoadBrushModel
export function Mod_LoadBrushModel(r: GLState, mod: Model, buffer: Uint8Array): void {
  r.loadmodel.type = mod_brush;
  if (r.loadmodel !== r.mod_known[0]) r.ri.sysError(ERR_DROP, 'Loaded a brush model after the world');

  const version =
    buffer.length >= 8 ? buffer[4]! | (buffer[5]! << 8) | (buffer[6]! << 16) | (buffer[7]! << 24) : 0;
  if (version !== BSPVERSION) {
    r.ri.sysError(
      ERR_DROP,
      `Mod_LoadBrushModel: ${mod.name} has wrong version number (${version} should be ${BSPVERSION})`,
    );
  }
  let bsp: BspFile;
  try {
    bsp = parseBsp(buffer);
  } catch (e) {
    sysErr(r, e);
  }

  // load into heap
  Mod_LoadVertexes(r, bsp);
  Mod_LoadEdges(r, bsp);
  Mod_LoadSurfedges(r, bsp);
  Mod_LoadLighting(r, bsp);
  Mod_LoadPlanes(r, bsp);
  Mod_LoadTexinfo(r, bsp);
  Mod_LoadFaces(r, bsp);
  Mod_LoadMarksurfaces(r, bsp);
  Mod_LoadVisibility(r, bsp);
  Mod_LoadLeafs(r, bsp);
  Mod_LoadNodes(r, bsp);
  Mod_LoadSubmodels(r, bsp);
  mod.numframes = 2; // regular and alternate animation

  // port: pack the polygons into the static vertex buffer
  GL_UploadWorldGeometry(r, mod);

  // port: memory safety -- C overruns mod_inline[] / reads past the surface array
  if (mod.numsubmodels > r.mod_inline.length)
    r.ri.sysError(ERR_DROP, `Mod_LoadBrushModel: ${mod.name} has too many submodels`);
  for (let i = 0; i < mod.numsubmodels; i++) {
    const bm = mod.submodels[i]!;
    if (bm.firstface < 0 || bm.numfaces < 0 || bm.firstface + bm.numfaces > mod.numsurfaces)
      r.ri.sysError(ERR_DROP, `Inline model ${i} has bad faces`);
  }

  // set up the submodels
  for (let i = 0; i < mod.numsubmodels; i++) {
    const bm = mod.submodels[i]!;
    const starmod = r.mod_inline[i]!;

    starmod.assign(r.loadmodel);

    starmod.firstmodelsurface = bm.firstface;
    starmod.nummodelsurfaces = bm.numfaces;
    starmod.firstnode = bm.headnode;
    if (starmod.firstnode >= r.loadmodel.numnodes)
      r.ri.sysError(ERR_DROP, `Inline model ${i} has bad firstnode`);

    VectorCopy(bm.maxs, starmod.maxs);
    VectorCopy(bm.mins, starmod.mins);
    starmod.radius = bm.radius;

    if (i === 0) r.loadmodel.assign(starmod);

    starmod.numleafs = bm.visleafs;
  }
}

// ==============================================================================
// ALIAS MODELS

// C: gl_model.c:929 Mod_LoadAliasModel (parsing and sanity checks in q2-formats parseMd2)
export function Mod_LoadAliasModel(r: GLState, mod: Model, buffer: Uint8Array): void {
  let md2;
  try {
    md2 = parseMd2(buffer, mod.name);
  } catch (e) {
    sysErr(r, e);
  }
  validateGlCmds(r, md2.glcmds, mod.name);
  mod.type = mod_alias;
  mod.alias = {
    md2,
    glcmds: md2.glcmds,
    glcmdsF: new Float32Array(md2.glcmds.buffer, md2.glcmds.byteOffset, md2.glcmds.length),
  };

  // register all skins
  for (let i = 0; i < md2.header.num_skins; i++) mod.skins[i] = GL_FindImage(r, md2.skins[i]!, it_skin);

  mod.mins[0] = -32;
  mod.mins[1] = -32;
  mod.mins[2] = -32;
  mod.maxs[0] = 32;
  mod.maxs[1] = 32;
  mod.maxs[2] = 32;
}

/**
 * Port: memory safety. GL_DrawAliasFrameLerp / GL_DrawAliasShadow walk the glcmd list trusting every
 * strip / fan count; a count beyond the list makes C read past the model (and a count near 2^31 draws
 * billions of vertices every frame). The list may end without the 0 terminator (the walk stops there).
 */
function validateGlCmds(r: GLState, glcmds: Int32Array, name: string): void {
  let p = 0;
  while (p < glcmds.length) {
    const count = glcmds[p++]!;
    if (!count) return;
    const n = Math.abs(count);
    if (n > (glcmds.length - p) / 3) r.ri.sysError(ERR_DROP, `Mod_LoadAliasModel: ${name} has a bad glcmd list`);
    p += n * 3;
  }
}

// ==============================================================================
// SPRITE MODELS

// C: gl_model.c:1061 Mod_LoadSpriteModel
export function Mod_LoadSpriteModel(r: GLState, mod: Model, buffer: Uint8Array): void {
  let spr;
  try {
    spr = parseSp2(buffer, mod.name);
  } catch (e) {
    sysErr(r, e);
  }
  mod.sprite = spr;
  for (let i = 0; i < spr.frames.length; i++) mod.skins[i] = GL_FindImage(r, spr.frames[i]!.name, it_sprite);
  mod.type = mod_sprite;
}

// =============================================================================

// C: gl_model.c:1105 R_BeginRegistration -- specifies the model that will be used as the world
export function R_BeginRegistration(r: GLState, model: string): void {
  r.registration_sequence++;
  r.r_oldviewcluster = -1; // force markleafs

  const fullname = `maps/${model}.bsp`.slice(0, 63);

  // explicitly free the old map if different; this guarantees that mod_known[0] is the world map
  const flushmap = r.ri.cvarGet('flushmap', '0', 0);
  if (r.mod_known[0]!.name !== fullname || flushmap.value) Mod_Free(r, r.mod_known[0]!);
  r.r_worldmodel = Mod_ForName(r, fullname, true);

  r.r_viewcluster = -1;
}

// C: gl_model.c:1132 R_RegisterModel
export function R_RegisterModel(r: GLState, name: string): Model | null {
  const mod = Mod_ForName(r, name, false);
  if (mod) {
    mod.registration_sequence = r.registration_sequence;

    // register any images used by the models
    if (mod.type === mod_sprite) {
      const spr = mod.sprite!;
      for (let i = 0; i < spr.frames.length; i++)
        mod.skins[i] = GL_FindImage(r, spr.frames[i]!.name, it_sprite);
    } else if (mod.type === mod_alias) {
      const md2 = mod.alias!.md2;
      for (let i = 0; i < md2.header.num_skins; i++) mod.skins[i] = GL_FindImage(r, md2.skins[i]!, it_skin);
      mod.numframes = md2.header.num_frames;
    } else if (mod.type === mod_brush) {
      for (let i = 0; i < mod.numtexinfo; i++)
        mod.texinfo[i]!.image.registration_sequence = r.registration_sequence;
    }
  }
  return mod;
}

// C: gl_model.c:1176 R_EndRegistration
export function R_EndRegistration(r: GLState): void {
  for (let i = 0; i < r.mod_numknown; i++) {
    const mod = r.mod_known[i]!;
    if (!mod.name) continue;
    if (mod.registration_sequence !== r.registration_sequence) Mod_Free(r, mod); // don't need this model
  }
  GL_FreeUnusedImages(r);
}

// C: gl_model.c:1203 Mod_Free
export function Mod_Free(r: GLState, mod: Model): void {
  if (mod.worldVbo && r.qgl)
    r.qgl.deleteWorldBuffer(mod.worldVbo as Parameters<typeof r.qgl.deleteWorldBuffer>[0]);
  mod.clear();
}

// C: gl_model.c:1214 Mod_FreeAll
export function Mod_FreeAll(r: GLState): void {
  for (let i = 0; i < r.mod_numknown; i++) {
    if (r.mod_known[i]!.extradatasize) Mod_Free(r, r.mod_known[i]!);
  }
}

// ---------------------------------------------------------------------------------------------
// Port: dependency discovery for the async prefetch done before the synchronous loaders run.

/** Image files a model file references (BSP texinfo textures, MD2 skins, SP2 frames). */
export function modelImageDependencies(buf: Uint8Array): string[] {
  if (buf.length < 12) return [];
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  const ident = dv.getUint32(0, true);
  const out: string[] = [];
  try {
    if (ident === IDBSPHEADER) {
      const ofs = dv.getInt32(8 + 5 * 8, true);
      const len = dv.getInt32(12 + 5 * 8, true);
      if (ofs < 0 || len < 0 || ofs + len > buf.length) return [];
      for (let o = ofs; o + 76 <= ofs + len; o += 76)
        out.push(texinfoImageName(readCString(buf, o + 40, 32)));
    } else if (ident === IDALIASHEADER) {
      const num_skins = dv.getInt32(20, true);
      const ofs_skins = dv.getInt32(44, true);
      for (let i = 0; i < num_skins && ofs_skins + (i + 1) * MAX_SKINNAME <= buf.length; i++) {
        out.push(readCString(buf, ofs_skins + i * MAX_SKINNAME, MAX_SKINNAME));
      }
    } else if (ident === IDSPRITEHEADER) {
      const n = dv.getInt32(8, true);
      for (let i = 0; i < n && 12 + (i + 1) * (16 + MAX_SKINNAME) <= buf.length; i++) {
        out.push(readCString(buf, 12 + i * (16 + MAX_SKINNAME) + 16, MAX_SKINNAME));
      }
    }
  } catch {
    return out;
  }
  return out;
}
