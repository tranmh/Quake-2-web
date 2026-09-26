// Crafted-asset robustness (docs/review/05-ts-render-sound-app.md): malformed BSP / MD2 data from a
// user-uploaded pak must end in Sys_Error(ERR_DROP) or a harmless fallback, never in an endless loop or an
// unbounded allocation that freezes the tab. Valid data keeps the exact C behaviour (oracle.test.ts).
import { describe, expect, it } from 'vitest';
import { GLState } from '../src/index';
import { R_Init } from '../src/gl_rmain';
import { Mod_ClusterPVS, Mod_ForName, R_BeginRegistration, R_RegisterModel } from '../src/gl_model';
import { GL_SubdivideSurface } from '../src/gl_warp';
import { MSurface, MTexinfo, Model } from '../src/gl_model_h';
import { RecursiveLightPoint } from '../src/gl_light';
import { demoPak, fakeImports, recordingQGL } from './helpers';

const LUMP_TEXINFO = 5;

function lump(buf: Uint8Array, n: number): { ofs: number; len: number } {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);
  return { ofs: dv.getInt32(8 + n * 8, true), len: dv.getInt32(12 + n * 8, true) };
}

/** R_Init + registration of a (possibly modified) demo1.bsp and extra files. */
function loadWith(overrides: Record<string, Uint8Array>): GLState {
  const p = demoPak()!;
  const { ri } = fakeImports();
  const r = new GLState(ri);
  r.qgl = recordingQGL().qgl;
  r.files = { get: (name: string) => overrides[name] ?? p.read(name) ?? null };
  R_Init(r);
  R_BeginRegistration(r, 'demo1');
  R_RegisterModel(r, 'maps/demo1.bsp');
  return r;
}

function demo1Bsp(): Uint8Array {
  return Uint8Array.from(demoPak()!.read('maps/demo1.bsp')!);
}

describe.skipIf(demoPak() === null)('crafted BSP', () => {
  it.each(['demo1', 'demo2', 'demo3'])('the unmodified %s still loads', (m) => {
    const p = demoPak()!;
    const r = new GLState(fakeImports().ri);
    r.qgl = recordingQGL().qgl;
    r.files = { get: (name: string) => p.read(name) ?? null };
    R_Init(r);
    R_BeginRegistration(r, m);
    expect(r.r_worldmodel?.numnodes).toBeGreaterThan(0);
  });

  it('texture animation chain with a cycle that does not return to the start terminates', () => {
    const bsp = demo1Bsp();
    const { ofs, len } = lump(bsp, LUMP_TEXINFO);
    expect(len / 76).toBeGreaterThan(3);
    const dv = new DataView(bsp.buffer);
    // 0 -> 1 -> 2 -> 1 -> 2 ... (C: endless loop counting the frames of texinfo 0)
    dv.setInt32(ofs + 0 * 76 + 72, 1, true);
    dv.setInt32(ofs + 1 * 76 + 72, 2, true);
    dv.setInt32(ofs + 2 * 76 + 72, 1, true);
    const r = loadWith({ 'maps/demo1.bsp': bsp });
    const ti = r.r_worldmodel!.texinfo;
    expect(ti[0]!.numframes).toBeLessThanOrEqual(ti.length);
    expect(ti[1]!.numframes).toBe(2);
  });

  it('nexttexinfo beyond the texinfo lump is treated as no animation', () => {
    const bsp = demo1Bsp();
    const { ofs } = lump(bsp, LUMP_TEXINFO);
    new DataView(bsp.buffer).setInt32(ofs + 72, 0x7fff0000, true);
    const r = loadWith({ 'maps/demo1.bsp': bsp });
    const t0 = r.r_worldmodel!.texinfo[0]!;
    expect(t0.next).toBeNull();
    expect(t0.numframes).toBe(1);
  });

  it('PVS of a cluster number beyond the vis lump does not spin', () => {
    const r = loadWith({});
    const w = r.r_worldmodel!;
    expect(w.vis).not.toBeNull();
    const vis = Mod_ClusterPVS(r, w.visNumclusters + 1000, w);
    expect(vis.length).toBeGreaterThan(0);
  });

  it('a compressed PVS row that runs off the end of the vis lump terminates', () => {
    const r = loadWith({});
    const w = r.r_worldmodel!;
    const vis = w.vis!;
    // point cluster 0 at the last byte and make it the start of a zero run whose count lies past the end
    vis[vis.length - 1] = 0;
    w.visBitofs[0] = vis.length - 1;
    r.r_oldviewcluster = -2;
    const row = Mod_ClusterPVS(r, 0, w);
    expect(row.length).toBeGreaterThan(0);
  });

  it('a node that is its own child is rejected with ERR_DROP instead of a stack overflow', () => {
    const bsp = demo1Bsp();
    const { ofs } = lump(bsp, 4 /* LUMP_NODES */);
    new DataView(bsp.buffer).setInt32(ofs + 4, 0, true); // node 0 front child = node 0
    expect(() => loadWith({ 'maps/demo1.bsp': bsp })).toThrow(/Sys_Error/);
  });

  it('a node reachable twice (exponential DAG) is rejected with ERR_DROP', () => {
    const bsp = demo1Bsp();
    const { ofs } = lump(bsp, 4 /* LUMP_NODES */);
    const dv = new DataView(bsp.buffer);
    const front = dv.getInt32(ofs + 4, true);
    expect(front).toBeGreaterThan(0);
    dv.setInt32(ofs + 8, front, true); // both children of node 0 are the same subtree
    expect(() => loadWith({ 'maps/demo1.bsp': bsp })).toThrow(/Sys_Error/);
  });

  it('a submodel with faces beyond the surface array is rejected with ERR_DROP', () => {
    const bsp = demo1Bsp();
    const { ofs, len } = lump(bsp, 13 /* LUMP_MODELS */);
    expect(len / 48).toBeGreaterThan(1);
    new DataView(bsp.buffer).setInt32(ofs + 48 + 44, 0x100000, true); // model 1 numfaces
    expect(() => loadWith({ 'maps/demo1.bsp': bsp })).toThrow(/Sys_Error/);
  });
});

describe('crafted warp surface', () => {
  function warpQuad(r: GLState, S: number): MSurface {
    const lm = new Model();
    lm.vertexes = Float32Array.of(0, 0, 0, S, 0, 0, S, S, 0, 0, S, 0);
    lm.edges = Uint16Array.of(0, 0, 0, 1, 1, 2, 2, 3, 3, 0);
    lm.surfedges = Int32Array.of(1, 2, 3, 4);
    r.loadmodel = lm;
    const fa = new MSurface();
    fa.texinfo = new MTexinfo();
    fa.firstedge = 0;
    fa.numedges = 4;
    return fa;
  }

  it('many large SURF_WARP polygons are rejected instead of being cut into millions of 64 unit pieces', () => {
    const r = new GLState(fakeImports().ri);
    // 22500 pieces per face; a crafted map can have tens of thousands of such faces (C: hangs / OOM)
    expect(() => {
      for (let i = 0; i < 16; i++) GL_SubdivideSurface(r, warpQuad(r, 64 * 150));
    }).toThrow(/Sys_Error/);
  });

  it('a warp polygon beyond +-9999 units ends in ERR_DROP, not a stack overflow', () => {
    // BoundPoly starts from +-9999, so the split plane misses the polygon and C recurses forever
    const r = new GLState(fakeImports().ri);
    expect(() => GL_SubdivideSurface(r, warpQuad(r, 64 * 200))).toThrow(/Sys_Error/);
  });

  it('a normal water face is still subdivided like C', () => {
    const r = new GLState(fakeImports().ri);
    const fa = warpQuad(r, 256);
    GL_SubdivideSurface(r, fa);
    let n = 0;
    for (let p = fa.polys; p; p = p.next) n++;
    expect(n).toBe(16);
  });
});

describe('deep BSP trees', () => {
  it('RecursiveLightPoint deeper than 1024 nodes does not dereference a missing mid-point buffer', () => {
    const { ri } = fakeImports();
    const r = new GLState(ri);
    type N = import('../src/gl_model_h').MNode;
    // node at depth d splits at z = d; the segment 2000 -> -10 crosses every one of them (front first)
    const leaf = { contents: 0 } as unknown as N;
    let node = leaf;
    for (let d = 1499; d >= 0; d--) {
      node = {
        contents: -1,
        plane: { normal: Float32Array.of(0, 0, 1), dist: d, type: 2 },
        children: [node, leaf],
        firstsurface: 0,
        numsurfaces: 0,
      } as unknown as N;
    }
    r.r_worldmodel = { surfaces: [], lightdata: null } as unknown as Model;
    (r as unknown as { cv: unknown }).cv = { gl_modulate: { value: 1 } };
    expect(RecursiveLightPoint(r, node, Float32Array.of(0, 0, 2000), Float32Array.of(0, 0, -10))).toBe(-1);
  });
});

describe.skipIf(demoPak() === null)('crafted MD2', () => {
  const name = demoPak()?.list().find((n) => n.endsWith('.md2')) ?? '';

  it('a glcmd strip count larger than the command list is rejected with ERR_DROP', () => {
    const md2 = Uint8Array.from(demoPak()!.read(name)!);
    const dv = new DataView(md2.buffer);
    const ofsGlcmds = dv.getInt32(60, true);
    dv.setInt32(ofsGlcmds, 0x7fffffff, true); // C: draws 2^31 vertices from beyond the buffer every frame
    const r = loadWith({ [name]: md2 });
    expect(() => Mod_ForName(r, name, true)).toThrow(/Sys_Error/);
  });

  it('a negative (fan) count of -2^31 is rejected too', () => {
    const md2 = Uint8Array.from(demoPak()!.read(name)!);
    const dv = new DataView(md2.buffer);
    dv.setInt32(dv.getInt32(60, true), -0x80000000, true);
    const r = loadWith({ [name]: md2 });
    expect(() => Mod_ForName(r, name, true)).toThrow(/Sys_Error/);
  });

  it('every model of the demo pak still loads', () => {
    const r = loadWith({});
    const models = demoPak()!.list().filter((n) => n.endsWith('.md2') || n.endsWith('.sp2'));
    expect(models.length).toBeGreaterThan(10);
    for (const m of models) expect(Mod_ForName(r, m, true)).not.toBeNull();
  });
});
