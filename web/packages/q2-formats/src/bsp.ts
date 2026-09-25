// BSP v38 parser (qcommon/qfiles.h). All lumps are decoded into structure-of-arrays typed arrays.
// Malformed input raises FormatError instead of reading out of bounds.
import {
  BSPVERSION,
  HEADER_LUMPS,
  IDBSPHEADER,
  LUMP_AREAPORTALS,
  LUMP_AREAS,
  LUMP_BRUSHES,
  LUMP_BRUSHSIDES,
  LUMP_EDGES,
  LUMP_ENTITIES,
  LUMP_FACES,
  LUMP_LEAFBRUSHES,
  LUMP_LEAFFACES,
  LUMP_LEAFS,
  LUMP_LIGHTING,
  LUMP_MODELS,
  LUMP_NODES,
  LUMP_PLANES,
  LUMP_POP,
  LUMP_SURFEDGES,
  LUMP_TEXINFO,
  LUMP_VERTEXES,
  LUMP_VISIBILITY,
  readCString,
} from 'q2-shared';
import { FormatError } from './errors';

export interface BspLump {
  fileofs: number;
  filelen: number;
}

// dplane_t: 20 bytes
export interface BspPlanes {
  count: number;
  normal: Float32Array; // count*3
  dist: Float32Array;
  type: Int32Array;
}

// dnode_t: 28 bytes
export interface BspNodes {
  count: number;
  planenum: Int32Array;
  children: Int32Array; // count*2; negative = -(leaf+1)
  mins: Int16Array; // count*3
  maxs: Int16Array;
  firstface: Uint16Array;
  numfaces: Uint16Array;
}

// texinfo_t: 76 bytes
export interface BspTexinfo {
  count: number;
  vecs: Float32Array; // count*8: [s xyz offset, t xyz offset]
  flags: Int32Array;
  value: Int32Array;
  texture: string[]; // Latin-1, up to 32 chars
  nexttexinfo: Int32Array;
}

// dface_t: 20 bytes
export interface BspFaces {
  count: number;
  planenum: Uint16Array;
  side: Int16Array;
  firstedge: Int32Array;
  numedges: Int16Array;
  texinfo: Int16Array;
  styles: Uint8Array; // count*4
  lightofs: Int32Array;
}

// dleaf_t: 28 bytes
export interface BspLeafs {
  count: number;
  contents: Int32Array;
  cluster: Int16Array;
  area: Int16Array;
  mins: Int16Array; // count*3
  maxs: Int16Array;
  firstleafface: Uint16Array;
  numleaffaces: Uint16Array;
  firstleafbrush: Uint16Array;
  numleafbrushes: Uint16Array;
}

// dmodel_t: 48 bytes
export interface BspModels {
  count: number;
  mins: Float32Array; // count*3
  maxs: Float32Array;
  origin: Float32Array;
  headnode: Int32Array;
  firstface: Int32Array;
  numfaces: Int32Array;
}

// dbrush_t: 12 bytes
export interface BspBrushes {
  count: number;
  firstside: Int32Array;
  numsides: Int32Array;
  contents: Int32Array;
}

// dbrushside_t: 4 bytes
export interface BspBrushSides {
  count: number;
  planenum: Uint16Array;
  texinfo: Int16Array;
}

// darea_t: 8 bytes
export interface BspAreas {
  count: number;
  numareaportals: Int32Array;
  firstareaportal: Int32Array;
}

// dareaportal_t: 8 bytes
export interface BspAreaPortals {
  count: number;
  portalnum: Int32Array;
  otherarea: Int32Array;
}

export interface BspFile {
  /** The whole file (needed e.g. for the CM_LoadMap checksum). */
  bytes: Uint8Array;
  version: number;
  lumps: BspLump[];
  /** Raw entity lump bytes (may contain a trailing NUL). */
  entityBytes: Uint8Array;
  /** Entity string up to the first NUL, as a Latin-1 byte string. */
  entityString: string;
  planes: BspPlanes;
  vertexes: Float32Array; // count*3
  numvertexes: number;
  /** Raw visibility lump (dvis_t header + compressed rows). */
  visibility: Uint8Array;
  nodes: BspNodes;
  texinfo: BspTexinfo;
  faces: BspFaces;
  lighting: Uint8Array;
  leafs: BspLeafs;
  leaffaces: Uint16Array;
  leafbrushes: Uint16Array;
  edges: Uint16Array; // count*2
  numedges: number;
  surfedges: Int32Array;
  models: BspModels;
  brushes: BspBrushes;
  brushsides: BspBrushSides;
  pop: Uint8Array;
  areas: BspAreas;
  areaportals: BspAreaPortals;
}

export const LUMP_NAMES = [
  'entities',
  'planes',
  'vertexes',
  'visibility',
  'nodes',
  'texinfo',
  'faces',
  'lighting',
  'leafs',
  'leaffaces',
  'leafbrushes',
  'edges',
  'surfedges',
  'models',
  'brushes',
  'brushsides',
  'pop',
  'areas',
  'areaportals',
] as const;

function toU8(data: ArrayBuffer | Uint8Array): Uint8Array {
  return data instanceof Uint8Array ? data : new Uint8Array(data);
}

/** Parse a BSP v38 file. Throws FormatError on malformed data. */
export function parseBsp(data: ArrayBuffer | Uint8Array): BspFile {
  const bytes = toU8(data);
  const dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const headerSize = 8 + HEADER_LUMPS * 8;
  if (bytes.length < headerSize) throw new FormatError('bsp: file too small for header');
  const ident = dv.getInt32(0, true);
  if (ident !== IDBSPHEADER) throw new FormatError('bsp: bad ident');
  const version = dv.getInt32(4, true);
  if (version !== BSPVERSION) {
    throw new FormatError(`bsp: has wrong version number (${version} should be ${BSPVERSION})`);
  }
  const lumps: BspLump[] = [];
  for (let i = 0; i < HEADER_LUMPS; i++) {
    const fileofs = dv.getInt32(8 + i * 8, true);
    const filelen = dv.getInt32(12 + i * 8, true);
    if (fileofs < 0 || filelen < 0 || fileofs + filelen > bytes.length) {
      throw new FormatError(`bsp: lump ${LUMP_NAMES[i]} out of bounds`);
    }
    lumps.push({ fileofs, filelen });
  }

  const lump = (n: number, size: number): { ofs: number; count: number } => {
    const l = lumps[n]!;
    if (l.filelen % size) throw new FormatError(`bsp: funny lump size in ${LUMP_NAMES[n]}`);
    return { ofs: l.fileofs, count: l.filelen / size };
  };
  const raw = (n: number): Uint8Array => {
    const l = lumps[n]!;
    return bytes.slice(l.fileofs, l.fileofs + l.filelen);
  };
  const f32 = (o: number) => dv.getFloat32(o, true);
  const i32 = (o: number) => dv.getInt32(o, true);
  const i16 = (o: number) => dv.getInt16(o, true);
  const u16 = (o: number) => dv.getUint16(o, true);

  // entities
  const entityBytes = raw(LUMP_ENTITIES);
  const entityString = readCString(entityBytes, 0, entityBytes.length);

  // planes
  let { ofs, count } = lump(LUMP_PLANES, 20);
  const planes: BspPlanes = {
    count,
    normal: new Float32Array(count * 3),
    dist: new Float32Array(count),
    type: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 20) {
    planes.normal[i * 3] = f32(ofs);
    planes.normal[i * 3 + 1] = f32(ofs + 4);
    planes.normal[i * 3 + 2] = f32(ofs + 8);
    planes.dist[i] = f32(ofs + 12);
    planes.type[i] = i32(ofs + 16);
  }

  // vertexes
  ({ ofs, count } = lump(LUMP_VERTEXES, 12));
  const numvertexes = count;
  const vertexes = new Float32Array(count * 3);
  for (let i = 0; i < count * 3; i++) vertexes[i] = f32(ofs + i * 4);

  const visibility = raw(LUMP_VISIBILITY);

  // nodes
  ({ ofs, count } = lump(LUMP_NODES, 28));
  const nodes: BspNodes = {
    count,
    planenum: new Int32Array(count),
    children: new Int32Array(count * 2),
    mins: new Int16Array(count * 3),
    maxs: new Int16Array(count * 3),
    firstface: new Uint16Array(count),
    numfaces: new Uint16Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 28) {
    nodes.planenum[i] = i32(ofs);
    nodes.children[i * 2] = i32(ofs + 4);
    nodes.children[i * 2 + 1] = i32(ofs + 8);
    for (let j = 0; j < 3; j++) {
      nodes.mins[i * 3 + j] = i16(ofs + 12 + j * 2);
      nodes.maxs[i * 3 + j] = i16(ofs + 18 + j * 2);
    }
    nodes.firstface[i] = u16(ofs + 24);
    nodes.numfaces[i] = u16(ofs + 26);
  }

  // texinfo
  ({ ofs, count } = lump(LUMP_TEXINFO, 76));
  const texinfo: BspTexinfo = {
    count,
    vecs: new Float32Array(count * 8),
    flags: new Int32Array(count),
    value: new Int32Array(count),
    texture: new Array<string>(count),
    nexttexinfo: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 76) {
    for (let j = 0; j < 8; j++) texinfo.vecs[i * 8 + j] = f32(ofs + j * 4);
    texinfo.flags[i] = i32(ofs + 32);
    texinfo.value[i] = i32(ofs + 36);
    texinfo.texture[i] = readCString(bytes, ofs + 40, 32);
    texinfo.nexttexinfo[i] = i32(ofs + 72);
  }

  // faces
  ({ ofs, count } = lump(LUMP_FACES, 20));
  const faces: BspFaces = {
    count,
    planenum: new Uint16Array(count),
    side: new Int16Array(count),
    firstedge: new Int32Array(count),
    numedges: new Int16Array(count),
    texinfo: new Int16Array(count),
    styles: new Uint8Array(count * 4),
    lightofs: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 20) {
    faces.planenum[i] = u16(ofs);
    faces.side[i] = i16(ofs + 2);
    faces.firstedge[i] = i32(ofs + 4);
    faces.numedges[i] = i16(ofs + 8);
    faces.texinfo[i] = i16(ofs + 10);
    for (let j = 0; j < 4; j++) faces.styles[i * 4 + j] = bytes[ofs + 12 + j]!;
    faces.lightofs[i] = i32(ofs + 16);
  }

  const lighting = raw(LUMP_LIGHTING);

  // leafs
  ({ ofs, count } = lump(LUMP_LEAFS, 28));
  const leafs: BspLeafs = {
    count,
    contents: new Int32Array(count),
    cluster: new Int16Array(count),
    area: new Int16Array(count),
    mins: new Int16Array(count * 3),
    maxs: new Int16Array(count * 3),
    firstleafface: new Uint16Array(count),
    numleaffaces: new Uint16Array(count),
    firstleafbrush: new Uint16Array(count),
    numleafbrushes: new Uint16Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 28) {
    leafs.contents[i] = i32(ofs);
    leafs.cluster[i] = i16(ofs + 4);
    leafs.area[i] = i16(ofs + 6);
    for (let j = 0; j < 3; j++) {
      leafs.mins[i * 3 + j] = i16(ofs + 8 + j * 2);
      leafs.maxs[i * 3 + j] = i16(ofs + 14 + j * 2);
    }
    leafs.firstleafface[i] = u16(ofs + 20);
    leafs.numleaffaces[i] = u16(ofs + 22);
    leafs.firstleafbrush[i] = u16(ofs + 24);
    leafs.numleafbrushes[i] = u16(ofs + 26);
  }

  ({ ofs, count } = lump(LUMP_LEAFFACES, 2));
  const leaffaces = new Uint16Array(count);
  for (let i = 0; i < count; i++) leaffaces[i] = u16(ofs + i * 2);

  ({ ofs, count } = lump(LUMP_LEAFBRUSHES, 2));
  const leafbrushes = new Uint16Array(count);
  for (let i = 0; i < count; i++) leafbrushes[i] = u16(ofs + i * 2);

  ({ ofs, count } = lump(LUMP_EDGES, 4));
  const numedges = count;
  const edges = new Uint16Array(count * 2);
  for (let i = 0; i < count * 2; i++) edges[i] = u16(ofs + i * 2);

  ({ ofs, count } = lump(LUMP_SURFEDGES, 4));
  const surfedges = new Int32Array(count);
  for (let i = 0; i < count; i++) surfedges[i] = i32(ofs + i * 4);

  // models
  ({ ofs, count } = lump(LUMP_MODELS, 48));
  const models: BspModels = {
    count,
    mins: new Float32Array(count * 3),
    maxs: new Float32Array(count * 3),
    origin: new Float32Array(count * 3),
    headnode: new Int32Array(count),
    firstface: new Int32Array(count),
    numfaces: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 48) {
    for (let j = 0; j < 3; j++) {
      models.mins[i * 3 + j] = f32(ofs + j * 4);
      models.maxs[i * 3 + j] = f32(ofs + 12 + j * 4);
      models.origin[i * 3 + j] = f32(ofs + 24 + j * 4);
    }
    models.headnode[i] = i32(ofs + 36);
    models.firstface[i] = i32(ofs + 40);
    models.numfaces[i] = i32(ofs + 44);
  }

  ({ ofs, count } = lump(LUMP_BRUSHES, 12));
  const brushes: BspBrushes = {
    count,
    firstside: new Int32Array(count),
    numsides: new Int32Array(count),
    contents: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 12) {
    brushes.firstside[i] = i32(ofs);
    brushes.numsides[i] = i32(ofs + 4);
    brushes.contents[i] = i32(ofs + 8);
  }

  ({ ofs, count } = lump(LUMP_BRUSHSIDES, 4));
  const brushsides: BspBrushSides = {
    count,
    planenum: new Uint16Array(count),
    texinfo: new Int16Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 4) {
    brushsides.planenum[i] = u16(ofs);
    brushsides.texinfo[i] = i16(ofs + 2);
  }

  const pop = raw(LUMP_POP);

  ({ ofs, count } = lump(LUMP_AREAS, 8));
  const areas: BspAreas = {
    count,
    numareaportals: new Int32Array(count),
    firstareaportal: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 8) {
    areas.numareaportals[i] = i32(ofs);
    areas.firstareaportal[i] = i32(ofs + 4);
  }

  ({ ofs, count } = lump(LUMP_AREAPORTALS, 8));
  const areaportals: BspAreaPortals = {
    count,
    portalnum: new Int32Array(count),
    otherarea: new Int32Array(count),
  };
  for (let i = 0; i < count; i++, ofs += 8) {
    areaportals.portalnum[i] = i32(ofs);
    areaportals.otherarea[i] = i32(ofs + 4);
  }

  return {
    bytes,
    version,
    lumps,
    entityBytes,
    entityString,
    planes,
    vertexes,
    numvertexes,
    visibility,
    nodes,
    texinfo,
    faces,
    lighting,
    leafs,
    leaffaces,
    leafbrushes,
    edges,
    numedges,
    surfedges,
    models,
    brushes,
    brushsides,
    pop,
    areas,
    areaportals,
  };
}
