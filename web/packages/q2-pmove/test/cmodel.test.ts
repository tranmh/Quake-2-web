import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import { CONTENTS_SOLID, MASK_ALL, MASK_PLAYERSOLID, MASK_SOLID, Trace, COM_ParseAll, vec3 } from 'q2-shared';
import { cmpF32, cmpF32Vec, cmpInt } from 'q2-shared/testing';
import { CMError, CollisionModel, CollisionWorld } from '../src';
import { demoPak, loadMap } from './helpers';

const here = dirname(fileURLToPath(import.meta.url));

function entities(s: string): Record<string, string>[] {
  const toks = COM_ParseAll(s);
  const out: Record<string, string>[] = [];
  let cur: Record<string, string> | null = null;
  for (let i = 0; i < toks.length; i++) {
    const t = toks[i]!;
    if (t === '{') cur = {};
    else if (t === '}') {
      if (cur) out.push(cur);
      cur = null;
    } else if (cur) cur[t] = toks[++i]!;
  }
  return out;
}

describe('CollisionModel.empty', () => {
  it('behaves like no map loaded', () => {
    const w = new CollisionWorld(CollisionModel.empty());
    const t = w.boxTrace(vec3(0, 0, 0), vec3(100, 0, 0), vec3(), vec3(), 0, MASK_ALL);
    expect(t.fraction).toBe(1);
    expect(t.surface?.name).toBe('');
    expect(w.pointContents(vec3(1, 2, 3), 0)).toBe(0);
    expect(w.pointLeafnum(vec3(1, 2, 3))).toBe(0);
    expect(w.model.numclusters).toBe(1);
  });
});

describe('box hull vs C (CM_HeadnodeForBox + CM_TransformedBoxTrace on demo1)', () => {
  const lines = readFileSync(join(here, 'data', 'boxhull_c.jsonl'), 'utf8')
    .split('\n')
    .filter(Boolean);
  it.skipIf(!demoPak())(`matches ${lines.length} C results bit-exactly`, () => {
    const w = new CollisionWorld(loadMap('demo1'));
    const tr = new Trace();
    let err: string | null = null;
    for (let i = 0; i < lines.length && !err; i++) {
      const q = JSON.parse(lines[i]!) as Record<string, number[]> & {
        r: {
          allsolid: number;
          startsolid: number;
          fraction: number;
          endpos: number[];
          normal: number[];
          dist: number;
          type: number;
          contents: number;
          pc: number;
        };
      };
      const f = (k: string) => Float32Array.from(q[k]!);
      const hn = w.headnodeForBox(f('bmins'), f('bmaxs'));
      w.transformedBoxTrace(
        f('start'),
        f('end'),
        f('mins'),
        f('maxs'),
        hn,
        MASK_ALL,
        f('origin'),
        f('angles'),
        tr,
      );
      const r = q.r;
      err =
        cmpInt('allsolid', tr.allsolid ? 1 : 0, r.allsolid) ??
        cmpInt('startsolid', tr.startsolid ? 1 : 0, r.startsolid) ??
        cmpF32('fraction', tr.fraction, r.fraction) ??
        cmpF32Vec('endpos', tr.endpos, r.endpos) ??
        cmpF32Vec('normal', tr.plane.normal, r.normal) ??
        cmpF32('dist', tr.plane.dist, r.dist) ??
        cmpInt('type', tr.plane.type, r.type) ??
        cmpInt('contents', tr.contents, r.contents) ??
        cmpInt('pointcontents', w.transformedPointContents(f('start'), hn, f('origin'), f('angles')), r.pc);
      if (err) err = `line ${i + 1}: ${err}`;
    }
    expect(err).toBeNull();
  });
});

describe.skipIf(!demoPak())('demo1 collision sanity', () => {
  it('loads with plausible counts and checksum', () => {
    const m = loadMap('demo1');
    expect(m.numcmodels).toBeGreaterThan(1);
    expect(m.numclusters).toBeGreaterThan(100);
    expect(m.leafContents[0]).toBe(CONTENTS_SOLID);
    expect(m.checksum).toBe(3218560851);
    expect(m.inlineModel('*1').headnode).toBe(m.cmodels[1]!.headnode);
    expect(() => m.inlineModel('*0')).toThrow(CMError);
    expect(() => m.inlineModel('x')).toThrow(CMError);
  });

  it('info_player_start is not in solid and a trace down hits the floor', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const starts = entities(m.entityString).filter((e) => e['classname'] === 'info_player_start');
    expect(starts.length).toBeGreaterThan(0);
    for (const e of starts) {
      const o = Float32Array.from(e['origin']!.split(' ').map(Number));
      expect(w.pointContents(o, 0) & CONTENTS_SOLID).toBe(0);
      const mins = vec3(-16, -16, -24),
        maxs = vec3(16, 16, 32);
      const inside = w.boxTrace(o, o, mins, maxs, 0, MASK_PLAYERSOLID);
      expect(inside.startsolid).toBe(false);
      const end = vec3(o[0], o[1], o[2]! - 1000);
      const t = w.boxTrace(o, end, mins, maxs, 0, MASK_PLAYERSOLID);
      expect(t.fraction).toBeGreaterThan(0);
      expect(t.fraction).toBeLessThan(1);
      expect(t.plane.normal[2]).toBeGreaterThan(0.7);
      expect(t.endpos[2]).toBeLessThan(o[2]!);
      expect(t.surface).not.toBeNull();
      // the point trace agrees on hitting something
      const tp = w.boxTrace(o, end, vec3(), vec3(), 0, MASK_SOLID);
      expect(tp.fraction).toBeLessThan(1);
    }
  });

  it('PVS rows, PHS rows and headnode visibility', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const rowlen = (m.numclusters + 7) >> 3;
    const pvs = w.clusterPVS(0);
    // a cluster sees itself
    expect(pvs[0]! & 1).toBe(1);
    const phs = w.clusterPHS(0).slice(0, rowlen);
    // PHS is a superset of PVS
    for (let i = 0; i < rowlen; i++) expect(phs[i]! & w.clusterPVS(0)[i]!).toBe(w.clusterPVS(0)[i]!);
    expect(Array.from(w.clusterPVS(-1).subarray(0, rowlen)).every((b) => b === 0)).toBe(true);
    const all = new Uint8Array(rowlen).fill(255);
    expect(w.headnodeVisible(0, all)).toBe(true);
    expect(w.headnodeVisible(0, new Uint8Array(rowlen))).toBe(false);
  });

  it('box leafnums, topnode and leaf accessors', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const list = new Int32Array(64);
    const n = w.boxLeafnums(vec3(-2000, -700, -400), vec3(1400, 2000, 900), list, 64);
    expect(n).toBe(64); // clipped at listsize
    expect(w.lastTopnode).toBe(0);
    const l = w.pointLeafnum(vec3(-1768, 1536, 128));
    expect(m.leafClusterOf(l)).toBeGreaterThanOrEqual(0);
    expect(() => m.leafContentsOf(m.numleafs)).toThrow(CMError);
  });

  it('area portals flood and write area bits', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const buf = new Uint8Array(32);
    const bytes = w.writeAreaBits(buf, 0);
    expect(bytes).toBe((m.numareas + 7) >> 3);
    // area 0 => everything
    expect(buf[0]).toBe((1 << m.numareas) - 1);
    const before = w.areasConnected(1, 2);
    for (let p = 0; p <= m.numareaportals; p++) w.setAreaPortalState(p, true);
    expect(w.areasConnected(1, 2)).toBe(true);
    for (let p = 0; p <= m.numareaportals; p++) w.setAreaPortalState(p, false);
    expect(w.areasConnected(1, 2)).toBe(before);
    const saved = w.writePortalState();
    w.setAreaPortalState(1, true);
    w.readPortalState(saved);
    expect(w.portalopen[1]).toBe(0);
    w.noareas = 1;
    expect(w.areasConnected(1, 2)).toBe(true);
    expect(() => w.setAreaPortalState(m.numareaportals + 1, true)).toThrow(CMError);
  });

  it('transformed traces with zero angles equal offset box traces', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const cm = m.cmodels[1]!;
    const org = vec3(0, 0, 0);
    const a = w.transformedBoxTrace(
      vec3(cm.mins[0]! - 50, cm.mins[1]!, cm.mins[2]!),
      vec3(cm.maxs[0]! + 50, cm.maxs[1]!, cm.maxs[2]!),
      vec3(-4, -4, -4),
      vec3(4, 4, 4),
      cm.headnode,
      MASK_ALL,
      org,
      vec3(0, 0, 0),
    );
    const b = w.boxTrace(
      vec3(cm.mins[0]! - 50, cm.mins[1]!, cm.mins[2]!),
      vec3(cm.maxs[0]! + 50, cm.maxs[1]!, cm.maxs[2]!),
      vec3(-4, -4, -4),
      vec3(4, 4, 4),
      cm.headnode,
      MASK_ALL,
    );
    expect(a.fraction).toBe(b.fraction);
    expect(Array.from(a.endpos)).toEqual(Array.from(b.endpos));
  });
});
