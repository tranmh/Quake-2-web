// Golden tests against the C oracle (docs/FIXTURES.md core/bsp and core/trace).
import { createHash } from 'node:crypto';
import { readdirSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { Trace, latin1ToBytes } from 'q2-shared';
import {
  bytesToHex,
  cmpF32,
  cmpF32Vec,
  cmpInt,
  fileExists,
  fixturePath,
  readJson,
  readJsonl,
} from 'q2-shared/testing';
import { CollisionWorld } from '../src';
import { demoPak, loadMap } from './helpers';

interface BspFixture {
  map: string;
  checksum: number;
  numclusters: number;
  numareas: number;
  numleafs: number;
  numnodes: number;
  numplanes: number;
  numbrushes: number;
  numbrushsides: number;
  numleafbrushes: number;
  numtexinfo: number;
  numareaportals: number;
  numcmodels: number;
  entitystring_sha256: string;
  cmodels: { mins: number[]; maxs: number[]; origin: number[]; headnode: number }[];
  pvs: string[];
  phs: string[];
  leafs: { contents: number; cluster: number; area: number }[];
}

function listFixtures(dir: string, ext: string): string[] {
  const d = fixturePath(...dir.split('/'));
  if (!fileExists(d)) return [];
  return readdirSync(d)
    .filter((f) => f.endsWith(ext))
    .map((f) => f.slice(0, -ext.length));
}

const havePak = demoPak() !== null;
const bspMaps = havePak ? listFixtures('core/bsp', '.json') : [];
const traceMaps = havePak ? listFixtures('core/trace', '.jsonl') : [];

describe.skipIf(bspMaps.length === 0)('golden core/bsp', () => {
  for (const map of bspMaps) {
    it(map, () => {
      const fx = readJson<BspFixture>(fixturePath('core', 'bsp', `${map}.json`));
      const m = loadMap(fx.map);
      const w = new CollisionWorld(m);
      const errs: (string | null)[] = [
        cmpInt('checksum', m.checksum, fx.checksum),
        cmpInt('numclusters', m.numclusters, fx.numclusters),
        cmpInt('numareas', m.numareas, fx.numareas),
        cmpInt('numleafs', m.numleafs, fx.numleafs),
        cmpInt('numnodes', m.numnodes, fx.numnodes),
        cmpInt('numplanes', m.numplanes, fx.numplanes),
        cmpInt('numbrushes', m.numbrushes, fx.numbrushes),
        cmpInt('numbrushsides', m.numbrushsides, fx.numbrushsides),
        cmpInt('numleafbrushes', m.numleafbrushes, fx.numleafbrushes),
        cmpInt('numtexinfo', m.numtexinfo, fx.numtexinfo),
        cmpInt('numareaportals', m.numareaportals, fx.numareaportals),
        cmpInt('numcmodels', m.numcmodels, fx.numcmodels),
      ];
      const sha = createHash('sha256').update(latin1ToBytes(m.entityString)).digest('hex');
      if (sha !== fx.entitystring_sha256) errs.push(`entitystring sha ${sha} != ${fx.entitystring_sha256}`);
      fx.cmodels.forEach((c, i) => {
        const cm = m.cmodels[i]!;
        errs.push(
          cmpF32Vec(`cmodels[${i}].mins`, cm.mins, c.mins),
          cmpF32Vec(`cmodels[${i}].maxs`, cm.maxs, c.maxs),
          cmpF32Vec(`cmodels[${i}].origin`, cm.origin, c.origin),
          cmpInt(`cmodels[${i}].headnode`, cm.headnode, c.headnode),
        );
      });
      const rowlen = (m.numclusters + 7) >> 3;
      for (let c = 0; c < fx.pvs.length; c++) {
        const got = bytesToHex(w.clusterPVS(c).subarray(0, rowlen));
        if (got !== fx.pvs[c]) {
          errs.push(`pvs[${c}]: got ${got} want ${fx.pvs[c]}`);
          break;
        }
      }
      for (let c = 0; c < fx.phs.length; c++) {
        const got = bytesToHex(w.clusterPHS(c).subarray(0, rowlen));
        if (got !== fx.phs[c]) {
          errs.push(`phs[${c}]: got ${got} want ${fx.phs[c]}`);
          break;
        }
      }
      for (let i = 0; i < fx.leafs.length; i++) {
        const l = fx.leafs[i]!;
        const e =
          cmpInt(`leafs[${i}].contents`, m.leafContents[i]!, l.contents) ??
          cmpInt(`leafs[${i}].cluster`, m.leafCluster[i]!, l.cluster) ??
          cmpInt(`leafs[${i}].area`, m.leafArea[i]!, l.area);
        if (e) {
          errs.push(e);
          break;
        }
      }
      const first = errs.find((e) => e);
      expect(first ?? null).toBeNull();
    });
  }
});

interface TraceQuery {
  kind: 'box' | 'transformed' | 'point' | 'pointt' | 'leaf';
  start?: number[];
  end?: number[];
  mins?: number[];
  maxs?: number[];
  headnode?: number;
  mask?: number;
  origin?: number[];
  angles?: number[];
  p?: number[];
}

interface TraceResult {
  allsolid?: number;
  startsolid?: number;
  fraction?: number;
  endpos?: number[];
  plane?: { normal: number[]; dist: number; type: number; signbits: number };
  surface?: { name: string; flags: number; value: number };
  contents?: number;
  leaf?: number;
}

function f32(v: number[] | undefined): Float32Array {
  return Float32Array.from(v ?? [0, 0, 0]);
}

export function compareTrace(t: Trace, r: TraceResult): string | null {
  return (
    cmpInt('allsolid', t.allsolid ? 1 : 0, r.allsolid!) ??
    cmpInt('startsolid', t.startsolid ? 1 : 0, r.startsolid!) ??
    cmpF32('fraction', t.fraction, r.fraction!) ??
    cmpF32Vec('endpos', t.endpos, r.endpos!) ??
    cmpF32Vec('plane.normal', t.plane.normal, r.plane!.normal) ??
    cmpF32('plane.dist', t.plane.dist, r.plane!.dist) ??
    cmpInt('plane.type', t.plane.type, r.plane!.type) ??
    cmpInt('plane.signbits', t.plane.signbits, r.plane!.signbits) ??
    (t.surface?.name === r.surface!.name
      ? null
      : `surface.name: got ${t.surface?.name} want ${r.surface!.name}`) ??
    cmpInt('surface.flags', t.surface?.flags ?? -1, r.surface!.flags) ??
    cmpInt('surface.value', t.surface?.value ?? -1, r.surface!.value) ??
    cmpInt('contents', t.contents, r.contents!)
  );
}

describe.skipIf(traceMaps.length === 0)('golden core/trace', () => {
  for (const map of traceMaps) {
    it(map, () => {
      const lines = readJsonl<{ q: TraceQuery; r: TraceResult }>(
        fixturePath('core', 'trace', `${map}.jsonl`),
      );
      const w = new CollisionWorld(loadMap(map));
      const tr = new Trace();
      let firstErr: string | null = null;
      let mismatches = 0;
      lines.forEach(({ q, r }, idx) => {
        let err: string | null = null;
        switch (q.kind) {
          case 'box':
            w.boxTrace(f32(q.start), f32(q.end), f32(q.mins), f32(q.maxs), q.headnode!, q.mask!, tr);
            err = compareTrace(tr, r);
            break;
          case 'transformed':
            w.transformedBoxTrace(
              f32(q.start),
              f32(q.end),
              f32(q.mins),
              f32(q.maxs),
              q.headnode!,
              q.mask!,
              f32(q.origin),
              f32(q.angles),
              tr,
            );
            err = compareTrace(tr, r);
            break;
          case 'point':
            err = cmpInt('contents', w.pointContents(f32(q.p), q.headnode!), r.contents!);
            break;
          case 'pointt':
            err = cmpInt(
              'contents',
              w.transformedPointContents(f32(q.p), q.headnode!, f32(q.origin), f32(q.angles)),
              r.contents!,
            );
            break;
          case 'leaf':
            err = cmpInt('leaf', w.pointLeafnum(f32(q.p)), r.leaf!);
            break;
        }
        if (err) {
          mismatches++;
          if (!firstErr) firstErr = `line ${idx + 1} (${JSON.stringify(q)}): ${err}`;
        }
      });
      expect(firstErr ? `${mismatches}/${lines.length} mismatches; first: ${firstErr}` : null).toBeNull();
    });
  }
});
