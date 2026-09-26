// Port of qcommon/cmodel.c. `CollisionModel` holds the immutable map data (shared read-only);
// `CollisionWorld` holds the mutable per-instance state the C code kept in globals (box hull plane
// distances, brush checkcounts, area portal state/floods, trace scratch).
// Bit-exact float semantics: every C float store is rounded with fround; double literals
// (DIST_EPSILON, 1.0/...) and fabs() are evaluated in double as C does.
import {
  AngleVectors,
  BoxOnPlaneSide,
  CModel,
  CONTENTS_MONSTER,
  CONTENTS_SOLID,
  CPlane,
  CSurface,
  DVIS_PHS,
  DVIS_PVS,
  MAX_MAP_AREAPORTALS,
  MAX_MAP_AREAS,
  MAX_MAP_BRUSHES,
  MAX_MAP_BRUSHSIDES,
  MAX_MAP_ENTSTRING,
  MAX_MAP_LEAFBRUSHES,
  MAX_MAP_LEAFS,
  MAX_MAP_MODELS,
  MAX_MAP_NODES,
  MAX_MAP_PLANES,
  MAX_MAP_TEXINFO,
  MAX_MAP_VISIBILITY,
  Trace,
  atoi,
  cShort,
  fr,
  type Vec3,
} from 'q2-shared';
import type { BspFile } from 'q2-formats';
import { parseBsp } from 'q2-formats';
import { Com_BlockChecksum } from 'q2-protocol';

/** Com_Error(ERR_DROP, ...) raised by the collision code. */
export class CMError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'CMError';
  }
}

/** 1/32 epsilon to keep floating point happy (double literal in C). */
const DIST_EPSILON = 0.03125;

/**
 * Immutable collision data for one map (C: the map_* globals filled by CM_LoadMap + CM_InitBoxHull).
 * Arrays include the extra box hull slots at the end.
 */
export class CollisionModel {
  name = '';
  /** unsigned 32-bit Com_BlockChecksum of the file */
  checksum = 0;

  numplanes = 0;
  planeNormal = new Float32Array(0);
  /** initial plane distances (box hull slots are 0); CollisionWorld copies this */
  planeDist = new Float32Array(0);
  planeType = new Uint8Array(0);
  planeSignbits = new Uint8Array(0);

  numnodes = 0;
  nodePlane = new Int32Array(0);
  nodeChildren = new Int32Array(0);

  numleafs = 1;
  leafContents = new Int32Array(1);
  leafCluster = new Int32Array(1);
  leafArea = new Int32Array(1);
  leafFirstLeafBrush = new Uint16Array(1);
  leafNumLeafBrushes = new Uint16Array(1);
  emptyleaf = 0;
  solidleaf = 0;

  numleafbrushes = 0;
  leafbrushes = new Uint16Array(0);

  numbrushes = 0;
  brushContents = new Int32Array(0);
  brushNumSides = new Int32Array(0);
  brushFirstSide = new Int32Array(0);

  numbrushsides = 0;
  sidePlane = new Int32Array(0);
  /** index into `surfaces`; -1 = nullsurface */
  sideSurface = new Int32Array(0);

  numtexinfo = 0;
  surfaces: CSurface[] = [];
  /** mapsurface_t.rname (31 chars) */
  surfaceRName: string[] = [];
  readonly nullsurface = new CSurface('', 0, 0);

  numcmodels = 0;
  cmodels: CModel[] = [new CModel()];

  numvisibility = 0;
  visibility = new Uint8Array(0);
  numclusters = 1;

  numentitychars = 0;
  entityString = '';

  numareas = 1;
  areaNumPortals = new Int32Array(MAX_MAP_AREAS);
  areaFirstPortal = new Int32Array(MAX_MAP_AREAS);

  numareaportals = 0;
  areaportalPortalnum = new Int32Array(0);
  areaportalOtherarea = new Int32Array(0);

  box_headnode = 0;
  /** index of the first box plane */
  box_planes = 0;
  box_brush = 0;
  box_leaf = 0;

  /** C: CM_LoadMap with an empty name (cinematic servers): nothing loaded. */
  static empty(): CollisionModel {
    return new CollisionModel();
  }

  /** C: cmodel.c:548 CM_LoadMap (file bytes; the checksum is computed over them). */
  static load(name: string, data: ArrayBuffer | Uint8Array): CollisionModel {
    const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
    return CollisionModel.fromBsp(name, parseBsp(bytes));
  }

  /** Build from an already parsed BSP (bsp.bytes is used for the checksum). */
  static fromBsp(name: string, bsp: BspFile): CollisionModel {
    const m = new CollisionModel();
    m.name = name;
    m.checksum = Com_BlockChecksum(bsp.bytes) >>> 0;
    m.loadSurfaces(bsp);
    m.loadLeafs(bsp);
    m.loadLeafBrushes(bsp);
    m.loadPlanes(bsp);
    m.loadBrushes(bsp);
    m.loadBrushSides(bsp);
    m.loadSubmodels(bsp);
    m.loadNodes(bsp);
    m.loadAreas(bsp);
    m.loadAreaPortals(bsp);
    m.loadVisibility(bsp);
    m.loadEntityString(bsp);
    m.initBoxHull();
    return m;
  }

  // C: cmodel.c:172 CMod_LoadSurfaces
  private loadSurfaces(bsp: BspFile): void {
    const count = bsp.texinfo.count;
    if (count < 1) throw new CMError('Map with no surfaces');
    if (count > MAX_MAP_TEXINFO) throw new CMError('Map has too many surfaces');
    this.numtexinfo = count;
    this.surfaces = [];
    this.surfaceRName = [];
    for (let i = 0; i < count; i++) {
      const tex = bsp.texinfo.texture[i]!;
      this.surfaces.push(new CSurface(tex.slice(0, 15), bsp.texinfo.flags[i]!, bsp.texinfo.value[i]!));
      this.surfaceRName.push(tex.slice(0, 31));
    }
  }

  // C: cmodel.c:274 CMod_LoadLeafs
  private loadLeafs(bsp: BspFile): void {
    const L = bsp.leafs;
    const count = L.count;
    if (count < 1) throw new CMError('Map with no leafs');
    // need to save space for box planes
    if (count > MAX_MAP_PLANES) throw new CMError('Map has too many planes');
    const n = count + 1; // + box leaf
    this.leafContents = new Int32Array(n);
    this.leafCluster = new Int32Array(n);
    this.leafArea = new Int32Array(n);
    this.leafFirstLeafBrush = new Uint16Array(n);
    this.leafNumLeafBrushes = new Uint16Array(n);
    this.numleafs = count;
    this.numclusters = 0;
    for (let i = 0; i < count; i++) {
      this.leafContents[i] = L.contents[i]!;
      this.leafCluster[i] = L.cluster[i]!;
      this.leafArea[i] = L.area[i]!;
      this.leafFirstLeafBrush[i] = L.firstleafbrush[i]!;
      this.leafNumLeafBrushes[i] = L.numleafbrushes[i]!;
      if (this.leafCluster[i]! >= this.numclusters) this.numclusters = this.leafCluster[i]! + 1;
    }
    if (this.leafContents[0] !== CONTENTS_SOLID) throw new CMError('Map leaf 0 is not CONTENTS_SOLID');
    this.solidleaf = 0;
    this.emptyleaf = -1;
    for (let i = 1; i < count; i++) {
      if (!this.leafContents[i]) {
        this.emptyleaf = i;
        break;
      }
    }
    if (this.emptyleaf === -1) throw new CMError('Map does not have an empty leaf');
  }

  // C: cmodel.c:365 CMod_LoadLeafBrushes
  private loadLeafBrushes(bsp: BspFile): void {
    const count = bsp.leafbrushes.length;
    if (count < 1) throw new CMError('Map with no planes');
    if (count > MAX_MAP_LEAFBRUSHES) throw new CMError('Map has too many leafbrushes');
    this.numleafbrushes = count;
    this.leafbrushes = new Uint16Array(count + 1);
    this.leafbrushes.set(bsp.leafbrushes);
  }

  // C: cmodel.c:325 CMod_LoadPlanes
  private loadPlanes(bsp: BspFile): void {
    const P = bsp.planes;
    const count = P.count;
    if (count < 1) throw new CMError('Map with no planes');
    if (count > MAX_MAP_PLANES) throw new CMError('Map has too many planes');
    const n = count + 12; // box planes
    this.numplanes = count;
    this.planeNormal = new Float32Array(n * 3);
    this.planeDist = new Float32Array(n);
    this.planeType = new Uint8Array(n);
    this.planeSignbits = new Uint8Array(n);
    for (let i = 0; i < count; i++) {
      let bits = 0;
      for (let j = 0; j < 3; j++) {
        const v = P.normal[i * 3 + j]!;
        this.planeNormal[i * 3 + j] = v;
        if (v < 0) bits |= 1 << j;
      }
      this.planeDist[i] = P.dist[i]!;
      this.planeType[i] = P.type[i]! & 255;
      this.planeSignbits[i] = bits;
    }
  }

  // C: cmodel.c:240 CMod_LoadBrushes
  private loadBrushes(bsp: BspFile): void {
    const B = bsp.brushes;
    const count = B.count;
    if (count > MAX_MAP_BRUSHES) throw new CMError('Map has too many brushes');
    this.numbrushes = count;
    this.brushContents = new Int32Array(count + 1);
    this.brushNumSides = new Int32Array(count + 1);
    this.brushFirstSide = new Int32Array(count + 1);
    for (let i = 0; i < count; i++) {
      this.brushFirstSide[i] = B.firstside[i]!;
      this.brushNumSides[i] = B.numsides[i]!;
      this.brushContents[i] = B.contents[i]!;
    }
  }

  // C: cmodel.c:396 CMod_LoadBrushSides
  private loadBrushSides(bsp: BspFile): void {
    const S = bsp.brushsides;
    const count = S.count;
    if (count > MAX_MAP_BRUSHSIDES) throw new CMError('Map has too many planes');
    this.numbrushsides = count;
    this.sidePlane = new Int32Array(count + 6);
    this.sideSurface = new Int32Array(count + 6);
    for (let i = 0; i < count; i++) {
      // LittleShort() sign-extends the unsigned short planenum
      const num = cShort(S.planenum[i]!);
      if (num < 0 || num >= this.numplanes) throw new CMError('Bad brushside planenum');
      this.sidePlane[i] = num;
      const j = S.texinfo[i]!;
      if (j >= this.numtexinfo) throw new CMError('Bad brushside texinfo');
      // j < 0 would index before map_surfaces in C (undefined); use the null surface
      this.sideSurface[i] = j < 0 ? -1 : j;
    }
    // validate brushes now that the side count is known (memory safety; C does not check)
    for (let i = 0; i < this.numbrushes; i++) {
      const f = this.brushFirstSide[i]!;
      const ns = this.brushNumSides[i]!;
      if (ns < 0 || f < 0 || f + ns > count) throw new CMError('Bad brush sides');
    }
    for (let i = 0; i < this.numleafbrushes; i++) {
      if (this.leafbrushes[i]! >= this.numbrushes) throw new CMError('Bad leafbrush');
    }
    for (let i = 0; i < this.numleafs; i++) {
      if (this.leafFirstLeafBrush[i]! + this.leafNumLeafBrushes[i]! > this.numleafbrushes) {
        throw new CMError('Bad leaf brushes');
      }
    }
  }

  // C: cmodel.c:133 CMod_LoadSubmodels
  private loadSubmodels(bsp: BspFile): void {
    const M = bsp.models;
    const count = M.count;
    if (count < 1) throw new CMError('Map with no models');
    if (count > MAX_MAP_MODELS) throw new CMError('Map has too many models');
    this.numcmodels = count;
    this.cmodels = [];
    for (let i = 0; i < count; i++) {
      const out = new CModel();
      for (let j = 0; j < 3; j++) {
        // spread the mins / maxs by a pixel
        out.mins[j] = M.mins[i * 3 + j]! - 1;
        out.maxs[j] = M.maxs[i * 3 + j]! + 1;
        out.origin[j] = M.origin[i * 3 + j]!;
      }
      out.headnode = M.headnode[i]!;
      this.cmodels.push(out);
    }
  }

  // C: cmodel.c:205 CMod_LoadNodes
  private loadNodes(bsp: BspFile): void {
    const N = bsp.nodes;
    const count = N.count;
    if (count < 1) throw new CMError('Map has no nodes');
    if (count > MAX_MAP_NODES) throw new CMError('Map has too many nodes');
    this.numnodes = count;
    this.nodePlane = new Int32Array(count + 6);
    this.nodeChildren = new Int32Array((count + 6) * 2);
    for (let i = 0; i < count; i++) {
      const pn = N.planenum[i]!;
      if (pn < 0 || pn >= this.numplanes) throw new CMError('Bad node planenum');
      this.nodePlane[i] = pn;
      for (let j = 0; j < 2; j++) {
        const child = N.children[i * 2 + j]!;
        if (child >= count || -1 - child >= this.numleafs) throw new CMError('Bad node child');
        this.nodeChildren[i * 2 + j] = child;
      }
    }
    for (const cm of this.cmodels) {
      if (cm.headnode >= count || -1 - cm.headnode >= this.numleafs) throw new CMError('Bad model headnode');
    }
    this.checkNodeCycles();
  }

  /**
   * Memory/liveness safety for hostile maps (not in C): the node graph reachable from any model headnode
   * must be a forest: a cycle makes CM_PointLeafnum_r / CM_BoxLeafnums_r loop forever (freezing the tab) and
   * CM_RecursiveHullCheck recurse until the stack overflows; shared subtrees make traces exponential.
   * BSP compilers always emit one tree per model (same check as the Go server, review 02 ENG-02/03).
   */
  private checkNodeCycles(): void {
    const n = this.numnodes;
    const children = this.nodeChildren;
    // shared subtrees (a node with two parents) make CM_RecursiveHullCheck, which descends both sides of
    // a split, take 2^depth steps on a chain of them -- an effectively endless trace
    const parents = new Uint8Array(n);
    for (let i = 0; i < n * 2; i++) {
      const child = children[i]!;
      if (child < 0) continue;
      if (parents[child]) throw new CMError('Map node has more than one parent');
      parents[child] = 1;
    }
    const state = new Uint8Array(n); // 0 = unvisited, 1 = on the DFS path, 2 = done
    const stack = new Int32Array(n);
    const side = new Uint8Array(n);
    for (const cm of this.cmodels) {
      const root = cm.headnode;
      if (root < 0 || state[root] === 2) continue;
      let sp = 0;
      stack[sp++] = root;
      state[root] = 1;
      side[root] = 0;
      while (sp) {
        const node = stack[sp - 1]!;
        if (side[node] === 2) {
          state[node] = 2;
          sp--;
          continue;
        }
        const child = children[node * 2 + side[node]!]!;
        side[node] = side[node]! + 1;
        if (child < 0 || state[child] === 2) continue;
        if (state[child] === 1) throw new CMError('Map node graph has a cycle');
        state[child] = 1;
        side[child] = 0;
        stack[sp++] = child;
      }
    }
  }

  // C: cmodel.c:442 CMod_LoadAreas
  private loadAreas(bsp: BspFile): void {
    const A = bsp.areas;
    const count = A.count;
    if (count > MAX_MAP_AREAS) throw new CMError('Map has too many areas');
    this.numareas = count;
    this.areaNumPortals = new Int32Array(MAX_MAP_AREAS);
    this.areaFirstPortal = new Int32Array(MAX_MAP_AREAS);
    for (let i = 0; i < count; i++) {
      this.areaNumPortals[i] = A.numareaportals[i]!;
      this.areaFirstPortal[i] = A.firstareaportal[i]!;
    }
  }

  // C: cmodel.c:471 CMod_LoadAreaPortals
  private loadAreaPortals(bsp: BspFile): void {
    const P = bsp.areaportals;
    const count = P.count;
    // (sic) C checks against MAX_MAP_AREAS
    if (count > MAX_MAP_AREAS) throw new CMError('Map has too many areas');
    this.numareaportals = count;
    this.areaportalPortalnum = new Int32Array(count);
    this.areaportalOtherarea = new Int32Array(count);
    for (let i = 0; i < count; i++) {
      const pn = P.portalnum[i]!;
      const oa = P.otherarea[i]!;
      if (pn < 0 || pn >= MAX_MAP_AREAPORTALS || oa < 0 || oa >= MAX_MAP_AREAS) {
        throw new CMError('Bad areaportal');
      }
      this.areaportalPortalnum[i] = pn;
      this.areaportalOtherarea[i] = oa;
    }
    for (let i = 0; i < this.numareas; i++) {
      const f = this.areaFirstPortal[i]!;
      const n = this.areaNumPortals[i]!;
      if (n > 0 && (f < 0 || f + n > count)) throw new CMError('Bad area portals');
    }
  }

  // C: cmodel.c:499 CMod_LoadVisibility
  private loadVisibility(bsp: BspFile): void {
    const len = bsp.visibility.length;
    if (len > MAX_MAP_VISIBILITY) throw new CMError('Map has too large visibility lump');
    this.numvisibility = len;
    this.visibility = new Uint8Array(len);
    this.visibility.set(bsp.visibility);
  }

  // C: cmodel.c:523 CMod_LoadEntityString
  private loadEntityString(bsp: BspFile): void {
    const len = bsp.entityBytes.length;
    if (len > MAX_MAP_ENTSTRING) throw new CMError('Map has too large entity lump');
    this.numentitychars = len;
    this.entityString = bsp.entityString;
  }

  // C: cmodel.c:710 CM_InitBoxHull
  private initBoxHull(): void {
    const numplanes = this.numplanes;
    const numnodes = this.numnodes;
    this.box_headnode = numnodes;
    this.box_planes = numplanes;
    if (
      numnodes + 6 > MAX_MAP_NODES ||
      this.numbrushes + 1 > MAX_MAP_BRUSHES ||
      this.numleafbrushes + 1 > MAX_MAP_LEAFBRUSHES ||
      this.numbrushsides + 6 > MAX_MAP_BRUSHSIDES ||
      numplanes + 12 > MAX_MAP_PLANES
    ) {
      throw new CMError('Not enough room for box tree');
    }
    const bb = (this.box_brush = this.numbrushes);
    this.brushNumSides[bb] = 6;
    this.brushFirstSide[bb] = this.numbrushsides;
    this.brushContents[bb] = CONTENTS_MONSTER;

    const bl = (this.box_leaf = this.numleafs);
    this.leafContents[bl] = CONTENTS_MONSTER;
    this.leafFirstLeafBrush[bl] = this.numleafbrushes;
    this.leafNumLeafBrushes[bl] = 1;
    this.leafbrushes[this.numleafbrushes] = this.numbrushes;

    for (let i = 0; i < 6; i++) {
      const side = i & 1;
      // brush sides
      this.sidePlane[this.numbrushsides + i] = numplanes + i * 2 + side;
      this.sideSurface[this.numbrushsides + i] = -1;
      // nodes
      const c = this.box_headnode + i;
      this.nodePlane[c] = numplanes + i * 2;
      this.nodeChildren[c * 2 + side] = -1 - this.emptyleaf;
      if (i !== 5) this.nodeChildren[c * 2 + (side ^ 1)] = this.box_headnode + i + 1;
      else this.nodeChildren[c * 2 + (side ^ 1)] = -1 - this.numleafs;
      // planes
      let p = numplanes + i * 2;
      this.planeType[p] = i >> 1;
      this.planeSignbits[p] = 0;
      this.planeNormal.fill(0, p * 3, p * 3 + 3);
      this.planeNormal[p * 3 + (i >> 1)] = 1;

      p = numplanes + i * 2 + 1;
      this.planeType[p] = 3 + (i >> 1);
      this.planeSignbits[p] = 0;
      this.planeNormal.fill(0, p * 3, p * 3 + 3);
      this.planeNormal[p * 3 + (i >> 1)] = -1;
    }
  }

  /** Surface for a brush side (nullsurface for -1). */
  sideSurfaceObj(side: number): CSurface {
    const s = this.sideSurface[side]!;
    return s < 0 ? this.nullsurface : this.surfaces[s]!;
  }

  // C: cmodel.c:647 CM_InlineModel
  inlineModel(name: string): CModel {
    if (!name || name[0] !== '*') throw new CMError('CM_InlineModel: bad name');
    const num = atoi(name.slice(1));
    if (num < 1 || num >= this.numcmodels) throw new CMError('CM_InlineModel: bad number');
    return this.cmodels[num]!;
  }

  // C: cmodel.c:660 CM_NumClusters
  numClusters(): number {
    return this.numclusters;
  }

  // C: cmodel.c:665 CM_NumInlineModels
  numInlineModels(): number {
    return this.numcmodels;
  }

  // C: cmodel.c:670 CM_EntityString
  entityStringValue(): string {
    return this.entityString;
  }

  // C: cmodel.c:675 CM_LeafContents
  leafContentsOf(leafnum: number): number {
    if (leafnum < 0 || leafnum >= this.numleafs) throw new CMError('CM_LeafContents: bad number');
    return this.leafContents[leafnum]!;
  }

  // C: cmodel.c:682 CM_LeafCluster
  leafClusterOf(leafnum: number): number {
    if (leafnum < 0 || leafnum >= this.numleafs) throw new CMError('CM_LeafCluster: bad number');
    return this.leafCluster[leafnum]!;
  }

  // C: cmodel.c:689 CM_LeafArea
  leafAreaOf(leafnum: number): number {
    if (leafnum < 0 || leafnum >= this.numleafs) throw new CMError('CM_LeafArea: bad number');
    return this.leafArea[leafnum]!;
  }
}

/**
 * Mutable collision state for one simulation instance (server or client) using a CollisionModel.
 * All CM_* query functions live here.
 */
export class CollisionWorld {
  readonly planeDist: Float32Array;
  private readonly brushCheckcount: Int32Array;
  private checkcount = 0;

  readonly portalopen = new Uint8Array(MAX_MAP_AREAPORTALS);
  readonly areaFloodnum = new Int32Array(MAX_MAP_AREAS);
  readonly areaFloodvalid = new Int32Array(MAX_MAP_AREAS);
  private floodvalid = 0;
  /** cvar map_noareas */
  noareas = 0;

  // statistics
  c_pointcontents = 0;
  c_traces = 0;
  c_brush_traces = 0;

  // trace state (C: trace_* globals)
  private readonly trace_start = new Float32Array(3);
  private readonly trace_end = new Float32Array(3);
  private readonly trace_mins = new Float32Array(3);
  private readonly trace_maxs = new Float32Array(3);
  private readonly trace_extents = new Float32Array(3);
  private readonly trace_trace = new Trace();
  private trace_contents = 0;
  private trace_ispoint = false;
  private readonly midPool: Float32Array[] = [];

  // box leafnums state
  private leaf_count = 0;
  private leaf_maxcount = 0;
  private leaf_list: Int32Array = new Int32Array(0);
  private leaf_mins: ArrayLike<number> = new Float32Array(3);
  private leaf_maxs: ArrayLike<number> = new Float32Array(3);
  private leaf_topnode = -1;
  /** topnode result of the last boxLeafnums call */
  lastTopnode = -1;

  // scratch
  private readonly s_leafs = new Int32Array(1024);
  private readonly s_c1 = new Float32Array(3);
  private readonly s_c2 = new Float32Array(3);
  private readonly s_ofs = new Float32Array(3);
  private readonly s_start_l = new Float32Array(3);
  private readonly s_end_l = new Float32Array(3);
  private readonly s_temp = new Float32Array(3);
  private readonly s_a = new Float32Array(3);
  private readonly s_forward = new Float32Array(3);
  private readonly s_right = new Float32Array(3);
  private readonly s_up = new Float32Array(3);
  private readonly s_p_l = new Float32Array(3);
  private readonly s_p = new Float32Array(3);

  readonly pvsrow = new Uint8Array(MAX_MAP_LEAFS / 8);
  readonly phsrow = new Uint8Array(MAX_MAP_LEAFS / 8);

  constructor(readonly model: CollisionModel) {
    this.planeDist = model.planeDist.slice();
    this.brushCheckcount = new Int32Array(model.numbrushes + 1);
    this.resetPortals();
  }

  /** C: CM_LoadMap tail: memset(portalopen, 0) + FloodAreaConnections (also the "same map" path). */
  resetPortals(): void {
    this.portalopen.fill(0);
    this.floodAreaConnections();
  }

  // C: cmodel.c:782 CM_HeadnodeForBox
  headnodeForBox(mins: ArrayLike<number>, maxs: ArrayLike<number>): number {
    const d = this.planeDist;
    const b = this.model.box_planes;
    d[b + 0] = maxs[0]!;
    d[b + 1] = -maxs[0]!;
    d[b + 2] = mins[0]!;
    d[b + 3] = -mins[0]!;
    d[b + 4] = maxs[1]!;
    d[b + 5] = -maxs[1]!;
    d[b + 6] = mins[1]!;
    d[b + 7] = -mins[1]!;
    d[b + 8] = maxs[2]!;
    d[b + 9] = -maxs[2]!;
    d[b + 10] = mins[2]!;
    d[b + 11] = -mins[2]!;
    return this.model.box_headnode;
  }

  // C: cmodel.c:808 CM_PointLeafnum_r
  private pointLeafnum_r(p: ArrayLike<number>, num: number): number {
    const m = this.model;
    const normal = m.planeNormal;
    const dist = this.planeDist;
    const ptype = m.planeType;
    const nodePlane = m.nodePlane;
    const children = m.nodeChildren;
    while (num >= 0) {
      const pl = nodePlane[num]!;
      const t = ptype[pl]!;
      let d: number;
      if (t < 3) d = fr(p[t]! - dist[pl]!);
      else {
        const o = pl * 3;
        d = fr(
          fr(fr(fr(normal[o]! * p[0]!) + fr(normal[o + 1]! * p[1]!)) + fr(normal[o + 2]! * p[2]!)) -
            dist[pl]!,
        );
      }
      if (d < 0) num = children[num * 2 + 1]!;
      else num = children[num * 2]!;
    }
    this.c_pointcontents++;
    return -1 - num;
  }

  // C: cmodel.c:835 CM_PointLeafnum
  pointLeafnum(p: ArrayLike<number>): number {
    if (!this.model.numplanes) return 0;
    const pp = this.s_p;
    pp[0] = p[0]!;
    pp[1] = p[1]!;
    pp[2] = p[2]!;
    return this.pointLeafnum_r(pp, 0);
  }

  // C: cmodel.c:856 CM_BoxLeafnums_r
  private boxLeafnums_r(nodenum: number): void {
    const m = this.model;
    for (;;) {
      if (nodenum < 0) {
        if (this.leaf_count >= this.leaf_maxcount) return;
        this.leaf_list[this.leaf_count++] = -1 - nodenum;
        return;
      }
      const s = this.boxOnPlaneSide(this.leaf_mins, this.leaf_maxs, m.nodePlane[nodenum]!);
      if (s === 1) nodenum = m.nodeChildren[nodenum * 2]!;
      else if (s === 2) nodenum = m.nodeChildren[nodenum * 2 + 1]!;
      else {
        // go down both
        if (this.leaf_topnode === -1) this.leaf_topnode = nodenum;
        this.boxLeafnums_r(m.nodeChildren[nodenum * 2]!);
        nodenum = m.nodeChildren[nodenum * 2 + 1]!;
      }
    }
  }

  private readonly bopsPlane = new CPlane();

  /** BOX_ON_PLANE_SIDE on plane index pl. */
  private boxOnPlaneSide(emins: ArrayLike<number>, emaxs: ArrayLike<number>, pl: number): number {
    const m = this.model;
    const type = m.planeType[pl]!;
    const dist = this.planeDist[pl]!;
    if (type < 3) {
      if (dist <= emins[type]!) return 1;
      if (dist >= emaxs[type]!) return 2;
      return 3;
    }
    const bp = this.bopsPlane;
    bp.normal[0] = m.planeNormal[pl * 3]!;
    bp.normal[1] = m.planeNormal[pl * 3 + 1]!;
    bp.normal[2] = m.planeNormal[pl * 3 + 2]!;
    bp.dist = dist;
    bp.type = type;
    bp.signbits = m.planeSignbits[pl]!;
    return BoxOnPlaneSide(emins, emaxs, bp);
  }

  // C: cmodel.c:888 CM_BoxLeafnums_headnode (topnode result in lastTopnode)
  boxLeafnumsHeadnode(
    mins: ArrayLike<number>,
    maxs: ArrayLike<number>,
    list: Int32Array,
    listsize: number,
    headnode: number,
  ): number {
    this.leaf_list = list;
    this.leaf_count = 0;
    this.leaf_maxcount = listsize;
    this.leaf_mins = mins;
    this.leaf_maxs = maxs;
    this.leaf_topnode = -1;
    this.boxLeafnums_r(headnode);
    this.lastTopnode = this.leaf_topnode;
    return this.leaf_count;
  }

  // C: cmodel.c:903 CM_BoxLeafnums
  boxLeafnums(mins: ArrayLike<number>, maxs: ArrayLike<number>, list: Int32Array, listsize: number): number {
    return this.boxLeafnumsHeadnode(mins, maxs, list, listsize, this.model.cmodels[0]!.headnode);
  }

  // C: cmodel.c:917 CM_PointContents
  pointContents(p: ArrayLike<number>, headnode: number): number {
    if (!this.model.numnodes) return 0;
    const pp = this.s_p;
    pp[0] = p[0]!;
    pp[1] = p[1]!;
    pp[2] = p[2]!;
    const l = this.pointLeafnum_r(pp, headnode);
    return this.model.leafContents[l]!;
  }

  // C: cmodel.c:937 CM_TransformedPointContents
  transformedPointContents(
    p: ArrayLike<number>,
    headnode: number,
    origin: ArrayLike<number>,
    angles: ArrayLike<number>,
  ): number {
    const p_l = this.s_p_l;
    const temp = this.s_temp;
    // subtract origin offset
    p_l[0] = p[0]! - origin[0]!;
    p_l[1] = p[1]! - origin[1]!;
    p_l[2] = p[2]! - origin[2]!;
    // rotate start and end into the models frame of reference
    if (headnode !== this.model.box_headnode && (angles[0] || angles[1] || angles[2])) {
      const forward = this.s_forward,
        right = this.s_right,
        up = this.s_up;
      AngleVectors(angles, forward, right, up);
      temp.set(p_l);
      p_l[0] = dot(temp, forward);
      p_l[1] = -dot(temp, right);
      p_l[2] = dot(temp, up);
    }
    const l = this.pointLeafnum_r(p_l, headnode);
    return this.model.leafContents[l]!;
  }

  // C: cmodel.c:993 CM_ClipBoxToBrush
  private clipBoxToBrush(
    mins: Float32Array,
    maxs: Float32Array,
    p1: Float32Array,
    p2: Float32Array,
    trace: Trace,
    brush: number,
  ): void {
    const m = this.model;
    const numsides = m.brushNumSides[brush]!;
    if (!numsides) return;
    this.c_brush_traces++;

    let enterfrac = -1;
    let leavefrac = 1;
    let clipplane = -1;
    let leadside = -1;
    let getout = false;
    let startout = false;
    const normal = m.planeNormal;
    const pdist = this.planeDist;
    const ofs = this.s_ofs;
    const first = m.brushFirstSide[brush]!;

    for (let i = 0; i < numsides; i++) {
      const side = first + i;
      const pl = m.sidePlane[side]!;
      const o = pl * 3;
      const n0 = normal[o]!,
        n1 = normal[o + 1]!,
        n2 = normal[o + 2]!;
      let dist: number;
      if (!this.trace_ispoint) {
        // general box case: push the plane out apropriately for mins/maxs
        ofs[0] = n0 < 0 ? maxs[0]! : mins[0]!;
        ofs[1] = n1 < 0 ? maxs[1]! : mins[1]!;
        ofs[2] = n2 < 0 ? maxs[2]! : mins[2]!;
        dist = fr(fr(fr(ofs[0]! * n0) + fr(ofs[1]! * n1)) + fr(ofs[2]! * n2));
        dist = fr(pdist[pl]! - dist);
      } else {
        // special point case
        dist = pdist[pl]!;
      }

      const d1 = fr(fr(fr(fr(p1[0]! * n0) + fr(p1[1]! * n1)) + fr(p1[2]! * n2)) - dist);
      const d2 = fr(fr(fr(fr(p2[0]! * n0) + fr(p2[1]! * n1)) + fr(p2[2]! * n2)) - dist);

      if (d2 > 0) getout = true; // endpoint is not in solid
      if (d1 > 0) startout = true;

      // if completely in front of face, no intersection
      if (d1 > 0 && d2 >= d1) return;
      if (d1 <= 0 && d2 <= 0) continue;

      // crosses face
      if (d1 > d2) {
        // enter
        const f = fr((d1 - DIST_EPSILON) / fr(d1 - d2));
        if (f > enterfrac) {
          enterfrac = f;
          clipplane = pl;
          leadside = side;
        }
      } else {
        // leave
        const f = fr((d1 + DIST_EPSILON) / fr(d1 - d2));
        if (f < leavefrac) leavefrac = f;
      }
    }

    if (!startout) {
      // original point was inside brush
      trace.startsolid = true;
      if (!getout) trace.allsolid = true;
      return;
    }
    if (enterfrac < leavefrac) {
      if (enterfrac > -1 && enterfrac < trace.fraction) {
        if (enterfrac < 0) enterfrac = 0;
        trace.fraction = enterfrac;
        const tp = trace.plane;
        tp.normal[0] = normal[clipplane * 3]!;
        tp.normal[1] = normal[clipplane * 3 + 1]!;
        tp.normal[2] = normal[clipplane * 3 + 2]!;
        tp.dist = pdist[clipplane]!;
        tp.type = m.planeType[clipplane]!;
        tp.signbits = m.planeSignbits[clipplane]!;
        trace.surface = m.sideSurfaceObj(leadside);
        trace.contents = m.brushContents[brush]!;
      }
    }
  }

  // C: cmodel.c:1111 CM_TestBoxInBrush
  private testBoxInBrush(
    mins: Float32Array,
    maxs: Float32Array,
    p1: Float32Array,
    trace: Trace,
    brush: number,
  ): void {
    const m = this.model;
    const numsides = m.brushNumSides[brush]!;
    if (!numsides) return;
    const normal = m.planeNormal;
    const pdist = this.planeDist;
    const ofs = this.s_ofs;
    const first = m.brushFirstSide[brush]!;
    for (let i = 0; i < numsides; i++) {
      const pl = m.sidePlane[first + i]!;
      const o = pl * 3;
      const n0 = normal[o]!,
        n1 = normal[o + 1]!,
        n2 = normal[o + 2]!;
      ofs[0] = n0 < 0 ? maxs[0]! : mins[0]!;
      ofs[1] = n1 < 0 ? maxs[1]! : mins[1]!;
      ofs[2] = n2 < 0 ? maxs[2]! : mins[2]!;
      let dist = fr(fr(fr(ofs[0]! * n0) + fr(ofs[1]! * n1)) + fr(ofs[2]! * n2));
      dist = fr(pdist[pl]! - dist);
      const d1 = fr(fr(fr(fr(p1[0]! * n0) + fr(p1[1]! * n1)) + fr(p1[2]! * n2)) - dist);
      // if completely in front of face, no intersection
      if (d1 > 0) return;
    }
    // inside this brush
    trace.startsolid = trace.allsolid = true;
    trace.fraction = 0;
    trace.contents = m.brushContents[brush]!;
  }

  // C: cmodel.c:1162 CM_TraceToLeaf
  private traceToLeaf(leafnum: number): void {
    const m = this.model;
    if (!(m.leafContents[leafnum]! & this.trace_contents)) return;
    const first = m.leafFirstLeafBrush[leafnum]!;
    const num = m.leafNumLeafBrushes[leafnum]!;
    // trace line against all brushes in the leaf
    for (let k = 0; k < num; k++) {
      const b = m.leafbrushes[first + k]!;
      if (this.brushCheckcount[b] === this.checkcount) continue; // already checked
      this.brushCheckcount[b] = this.checkcount;
      if (!(m.brushContents[b]! & this.trace_contents)) continue;
      this.clipBoxToBrush(
        this.trace_mins,
        this.trace_maxs,
        this.trace_start,
        this.trace_end,
        this.trace_trace,
        b,
      );
      if (!this.trace_trace.fraction) return;
    }
  }

  // C: cmodel.c:1195 CM_TestInLeaf
  private testInLeaf(leafnum: number): void {
    const m = this.model;
    if (!(m.leafContents[leafnum]! & this.trace_contents)) return;
    const first = m.leafFirstLeafBrush[leafnum]!;
    const num = m.leafNumLeafBrushes[leafnum]!;
    for (let k = 0; k < num; k++) {
      const b = m.leafbrushes[first + k]!;
      if (this.brushCheckcount[b] === this.checkcount) continue;
      this.brushCheckcount[b] = this.checkcount;
      if (!(m.brushContents[b]! & this.trace_contents)) continue;
      this.testBoxInBrush(this.trace_mins, this.trace_maxs, this.trace_start, this.trace_trace, b);
      if (!this.trace_trace.fraction) return;
    }
  }

  // C: cmodel.c:1229 CM_RecursiveHullCheck
  private recursiveHullCheck(
    num: number,
    p1f: number,
    p2f: number,
    p1: Float32Array,
    p2: Float32Array,
    depth: number,
  ): void {
    if (this.trace_trace.fraction <= p1f) return; // already hit something nearer

    // if < 0, we are in a leaf node
    if (num < 0) {
      this.traceToLeaf(-1 - num);
      return;
    }

    // find the point distances to the seperating plane and the offset for the size of the box
    const m = this.model;
    const pl = m.nodePlane[num]!;
    const type = m.planeType[pl]!;
    const pdist = this.planeDist[pl]!;
    let t1: number, t2: number, offset: number;
    if (type < 3) {
      t1 = fr(p1[type]! - pdist);
      t2 = fr(p2[type]! - pdist);
      offset = this.trace_extents[type]!;
    } else {
      const normal = m.planeNormal;
      const o = pl * 3;
      const n0 = normal[o]!,
        n1 = normal[o + 1]!,
        n2 = normal[o + 2]!;
      t1 = fr(fr(fr(fr(n0 * p1[0]!) + fr(n1 * p1[1]!)) + fr(n2 * p1[2]!)) - pdist);
      t2 = fr(fr(fr(fr(n0 * p2[0]!) + fr(n1 * p2[1]!)) + fr(n2 * p2[2]!)) - pdist);
      if (this.trace_ispoint) offset = 0;
      else {
        // fabs() returns double: the three terms are summed in double, then narrowed
        const e = this.trace_extents;
        offset = fr(Math.abs(fr(e[0]! * n0)) + Math.abs(fr(e[1]! * n1)) + Math.abs(fr(e[2]! * n2)));
      }
    }

    const children = m.nodeChildren;
    // see which sides we need to consider
    if (t1 >= offset && t2 >= offset) {
      this.recursiveHullCheck(children[num * 2]!, p1f, p2f, p1, p2, depth);
      return;
    }
    if (t1 < -offset && t2 < -offset) {
      this.recursiveHullCheck(children[num * 2 + 1]!, p1f, p2f, p1, p2, depth);
      return;
    }

    // put the crosspoint DIST_EPSILON pixels on the near side
    let side: number, frac: number, frac2: number;
    if (t1 < t2) {
      const idist = fr(1.0 / fr(t1 - t2));
      side = 1;
      frac2 = fr((fr(t1 + offset) + DIST_EPSILON) * idist);
      frac = fr((fr(t1 - offset) + DIST_EPSILON) * idist);
    } else if (t1 > t2) {
      const idist = fr(1.0 / fr(t1 - t2));
      side = 0;
      frac2 = fr((fr(t1 - offset) - DIST_EPSILON) * idist);
      frac = fr((fr(t1 + offset) + DIST_EPSILON) * idist);
    } else {
      side = 0;
      frac = 1;
      frac2 = 0;
    }

    let mid = this.midPool[depth];
    if (!mid) {
      mid = new Float32Array(3);
      this.midPool[depth] = mid;
    }

    // move up to the node
    if (frac < 0) frac = 0;
    if (frac > 1) frac = 1;
    let midf = fr(p1f + fr(fr(p2f - p1f) * frac));
    for (let i = 0; i < 3; i++) mid[i] = p1[i]! + fr(frac * fr(p2[i]! - p1[i]!));

    this.recursiveHullCheck(children[num * 2 + side]!, p1f, midf, p1, mid, depth + 1);

    // go past the node
    if (frac2 < 0) frac2 = 0;
    if (frac2 > 1) frac2 = 1;
    midf = fr(p1f + fr(fr(p2f - p1f) * frac2));
    for (let i = 0; i < 3; i++) mid[i] = p1[i]! + fr(frac2 * fr(p2[i]! - p1[i]!));

    this.recursiveHullCheck(children[num * 2 + (side ^ 1)]!, midf, p2f, mid, p2, depth + 1);
  }

  /**
   * C: cmodel.c:1344 CM_BoxTrace. The result is copied into `out` (a fresh Trace by default), mirroring
   * C's return by value.
   */
  boxTrace(
    start: ArrayLike<number>,
    end: ArrayLike<number>,
    mins: ArrayLike<number>,
    maxs: ArrayLike<number>,
    headnode: number,
    brushmask: number,
    out: Trace = new Trace(),
  ): Trace {
    this.checkcount++; // for multi-check avoidance
    this.c_traces++;

    // fill in a default trace
    const tt = this.trace_trace;
    tt.allsolid = false;
    tt.startsolid = false;
    tt.fraction = 1;
    tt.endpos.fill(0);
    tt.plane.clear();
    tt.surface = this.model.nullsurface;
    tt.contents = 0;
    tt.ent = null;

    if (!this.model.numnodes) return out.copyFrom(tt); // map not loaded

    this.trace_contents = brushmask;
    const ts = this.trace_start,
      te = this.trace_end,
      tmins = this.trace_mins,
      tmaxs = this.trace_maxs;
    ts[0] = start[0]!;
    ts[1] = start[1]!;
    ts[2] = start[2]!;
    te[0] = end[0]!;
    te[1] = end[1]!;
    te[2] = end[2]!;
    tmins[0] = mins[0]!;
    tmins[1] = mins[1]!;
    tmins[2] = mins[2]!;
    tmaxs[0] = maxs[0]!;
    tmaxs[1] = maxs[1]!;
    tmaxs[2] = maxs[2]!;

    // check for position test special case
    if (ts[0] === te[0] && ts[1] === te[1] && ts[2] === te[2]) {
      const c1 = this.s_c1,
        c2 = this.s_c2;
      for (let i = 0; i < 3; i++) {
        c1[i] = ts[i]! + tmins[i]!;
        c2[i] = ts[i]! + tmaxs[i]!;
      }
      for (let i = 0; i < 3; i++) {
        c1[i] = c1[i]! - 1;
        c2[i] = c2[i]! + 1;
      }
      const leafs = this.s_leafs;
      const numleafs = this.boxLeafnumsHeadnode(c1, c2, leafs, 1024, headnode);
      for (let i = 0; i < numleafs; i++) {
        this.testInLeaf(leafs[i]!);
        if (tt.allsolid) break;
      }
      tt.endpos.set(ts);
      return out.copyFrom(tt);
    }

    // check for point special case
    if (
      tmins[0] === 0 &&
      tmins[1] === 0 &&
      tmins[2] === 0 &&
      tmaxs[0] === 0 &&
      tmaxs[1] === 0 &&
      tmaxs[2] === 0
    ) {
      this.trace_ispoint = true;
      this.trace_extents.fill(0);
    } else {
      this.trace_ispoint = false;
      const ex = this.trace_extents;
      ex[0] = -tmins[0]! > tmaxs[0]! ? -tmins[0]! : tmaxs[0]!;
      ex[1] = -tmins[1]! > tmaxs[1]! ? -tmins[1]! : tmaxs[1]!;
      ex[2] = -tmins[2]! > tmaxs[2]! ? -tmins[2]! : tmaxs[2]!;
    }

    // general sweeping through world
    this.recursiveHullCheck(headnode, 0, 1, ts, te, 0);

    if (tt.fraction === 1) {
      tt.endpos.set(te);
    } else {
      for (let i = 0; i < 3; i++) tt.endpos[i] = ts[i]! + fr(tt.fraction * fr(te[i]! - ts[i]!));
    }
    return out.copyFrom(tt);
  }

  // C: cmodel.c:1445 CM_TransformedBoxTrace
  transformedBoxTrace(
    start: ArrayLike<number>,
    end: ArrayLike<number>,
    mins: ArrayLike<number>,
    maxs: ArrayLike<number>,
    headnode: number,
    brushmask: number,
    origin: ArrayLike<number>,
    angles: ArrayLike<number>,
    out: Trace = new Trace(),
  ): Trace {
    const start_l = this.s_start_l,
      end_l = this.s_end_l,
      temp = this.s_temp;
    const forward = this.s_forward,
      right = this.s_right,
      up = this.s_up;
    // subtract origin offset
    for (let i = 0; i < 3; i++) {
      start_l[i] = start[i]! - origin[i]!;
      end_l[i] = end[i]! - origin[i]!;
    }
    // rotate start and end into the models frame of reference
    const rotated = headnode !== this.model.box_headnode && !!(angles[0] || angles[1] || angles[2]);
    if (rotated) {
      AngleVectors(angles, forward, right, up);
      temp.set(start_l);
      start_l[0] = dot(temp, forward);
      start_l[1] = -dot(temp, right);
      start_l[2] = dot(temp, up);
      temp.set(end_l);
      end_l[0] = dot(temp, forward);
      end_l[1] = -dot(temp, right);
      end_l[2] = dot(temp, up);
    }

    // sweep the box through the model
    const trace = this.boxTrace(start_l, end_l, mins, maxs, headnode, brushmask, out);

    if (rotated && trace.fraction !== 1.0) {
      const a = this.s_a;
      a[0] = -angles[0]!;
      a[1] = -angles[1]!;
      a[2] = -angles[2]!;
      AngleVectors(a, forward, right, up);
      temp.set(trace.plane.normal);
      trace.plane.normal[0] = dot(temp, forward);
      trace.plane.normal[1] = -dot(temp, right);
      trace.plane.normal[2] = dot(temp, up);
    }

    const f = trace.fraction;
    trace.endpos[0] = fr(start[0]!) + fr(f * fr(fr(end[0]!) - fr(start[0]!)));
    trace.endpos[1] = fr(start[1]!) + fr(f * fr(fr(end[1]!) - fr(start[1]!)));
    trace.endpos[2] = fr(start[2]!) + fr(f * fr(fr(end[2]!) - fr(start[2]!)));
    return trace;
  }

  // C: cmodel.c:1535 CM_DecompressVis
  decompressVis(inOfs: number, out: Uint8Array): void {
    const m = this.model;
    let row = (m.numclusters + 7) >> 3;
    let out_p = 0;
    if (inOfs < 0 || !m.numvisibility) {
      // no vis info, so make all visible
      while (row) {
        out[out_p++] = 0xff;
        row--;
      }
      return;
    }
    const vis = m.visibility;
    // bytes past the lump read as 0 (C: static zero-filled buffer of MAX_MAP_VISIBILITY)
    const at = (i: number): number => {
      if (i >= MAX_MAP_VISIBILITY) throw new CMError('CM_DecompressVis: read past visibility');
      return i < vis.length ? vis[i]! : 0;
    };
    let inp = inOfs;
    do {
      const b = at(inp);
      if (b) {
        out[out_p++] = b;
        inp++;
        continue;
      }
      let c = at(inp + 1);
      inp += 2;
      if (out_p + c > row) {
        c = row - out_p;
      }
      while (c) {
        out[out_p++] = 0;
        c--;
      }
    } while (out_p < row);
  }

  private visOffset(cluster: number, which: number): number {
    const m = this.model;
    if (!m.numvisibility) return -1;
    const o = 4 + cluster * 8 + which * 4;
    if (cluster < 0 || o + 4 > m.visibility.length) throw new CMError('CM_ClusterPVS: bad cluster');
    const v = m.visibility;
    return v[o]! | (v[o + 1]! << 8) | (v[o + 2]! << 16) | (v[o + 3]! << 24) | 0;
  }

  // C: cmodel.c:1578 CM_ClusterPVS (returns the shared pvsrow buffer)
  clusterPVS(cluster: number): Uint8Array {
    if (cluster === -1) this.pvsrow.fill(0, 0, (this.model.numclusters + 7) >> 3);
    else this.decompressVis(this.visOffset(cluster, DVIS_PVS), this.pvsrow);
    return this.pvsrow;
  }

  // C: cmodel.c:1587 CM_ClusterPHS (returns the shared phsrow buffer)
  clusterPHS(cluster: number): Uint8Array {
    if (cluster === -1) this.phsrow.fill(0, 0, (this.model.numclusters + 7) >> 3);
    else this.decompressVis(this.visOffset(cluster, DVIS_PHS), this.phsrow);
    return this.phsrow;
  }

  // C: cmodel.c:1607 FloodArea_r
  private floodArea_r(area: number, floodnum: number): void {
    const m = this.model;
    if (this.areaFloodvalid[area] === this.floodvalid) {
      if (this.areaFloodnum[area] === floodnum) return;
      throw new CMError('FloodArea_r: reflooded');
    }
    this.areaFloodnum[area] = floodnum;
    this.areaFloodvalid[area] = this.floodvalid;
    const first = m.areaFirstPortal[area]!;
    const n = m.areaNumPortals[area]!;
    for (let i = 0; i < n; i++) {
      const p = first + i;
      if (this.portalopen[m.areaportalPortalnum[p]!]) this.floodArea_r(m.areaportalOtherarea[p]!, floodnum);
    }
  }

  // C: cmodel.c:1633 FloodAreaConnections
  floodAreaConnections(): void {
    // all current floods are now invalid
    this.floodvalid++;
    let floodnum = 0;
    // area 0 is not used
    for (let i = 1; i < this.model.numareas; i++) {
      if (this.areaFloodvalid[i] === this.floodvalid) continue; // already flooded into
      floodnum++;
      this.floodArea_r(i, floodnum);
    }
  }

  // C: cmodel.c:1654 CM_SetAreaPortalState
  setAreaPortalState(portalnum: number, open: boolean): void {
    if (portalnum > this.model.numareaportals) throw new CMError('areaportal > numareaportals');
    if (portalnum < 0 || portalnum >= MAX_MAP_AREAPORTALS) throw new CMError('areaportal out of range');
    this.portalopen[portalnum] = open ? 1 : 0;
    this.floodAreaConnections();
  }

  // C: cmodel.c:1663 CM_AreasConnected
  areasConnected(area1: number, area2: number): boolean {
    if (this.noareas) return true;
    if (area1 > this.model.numareas || area2 > this.model.numareas) throw new CMError('area > numareas');
    if (area1 < 0 || area2 < 0) throw new CMError('area < 0');
    return this.areaFloodnum[area1] === this.areaFloodnum[area2];
  }

  // C: cmodel.c:1686 CM_WriteAreaBits
  writeAreaBits(buffer: Uint8Array, area: number): number {
    const numareas = this.model.numareas;
    const bytes = (numareas + 7) >> 3;
    if (this.noareas) {
      // for debugging, send everything
      buffer.fill(255, 0, bytes);
    } else {
      buffer.fill(0, 0, bytes);
      const floodnum = this.areaFloodnum[area]!;
      for (let i = 0; i < numareas; i++) {
        if (this.areaFloodnum[i] === floodnum || !area) buffer[i >> 3]! |= 1 << (i & 7);
      }
    }
    return bytes;
  }

  // C: cmodel.c:1718 CM_WritePortalState
  writePortalState(): Uint8Array {
    // C writes the qboolean (int) array
    const out = new Uint8Array(MAX_MAP_AREAPORTALS * 4);
    for (let i = 0; i < MAX_MAP_AREAPORTALS; i++) out[i * 4] = this.portalopen[i]!;
    return out;
  }

  // C: cmodel.c:1730 CM_ReadPortalState
  readPortalState(data: Uint8Array): void {
    for (let i = 0; i < MAX_MAP_AREAPORTALS; i++) {
      const o = i * 4;
      this.portalopen[i] = data[o]! | data[o + 1]! | data[o + 2]! | data[o + 3]! ? 1 : 0;
    }
    this.floodAreaConnections();
  }

  // C: cmodel.c:1744 CM_HeadnodeVisible
  headnodeVisible(nodenum: number, visbits: Uint8Array): boolean {
    const m = this.model;
    if (nodenum < 0) {
      const leafnum = -1 - nodenum;
      const cluster = m.leafCluster[leafnum]!;
      if (cluster === -1) return false;
      if (visbits[cluster >> 3]! & (1 << (cluster & 7))) return true;
      return false;
    }
    if (this.headnodeVisible(m.nodeChildren[nodenum * 2]!, visbits)) return true;
    return this.headnodeVisible(m.nodeChildren[nodenum * 2 + 1]!, visbits);
  }
}

function dot(a: Vec3, b: Vec3): number {
  return fr(fr(fr(a[0]! * b[0]!) + fr(a[1]! * b[1]!)) + fr(a[2]! * b[2]!));
}
