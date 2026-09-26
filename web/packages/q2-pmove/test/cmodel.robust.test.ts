// Hostile-map robustness (maps come from user-uploaded paks): CM_LoadMap must reject maps whose node
// graph would make the collision queries loop forever, and random in-range node rewiring must never hang
// or crash the queries.
import { describe, expect, it } from 'vitest';
import { LUMP_NODES, MASK_ALL, vec3 } from 'q2-shared';
import { FormatError } from 'q2-formats';
import { CMError, CollisionModel, CollisionWorld } from '../src';
import { demoPak } from './helpers';

const pak = demoPak();

function demo1(): Uint8Array {
  return pak!.read('maps/demo1.bsp')!.slice();
}

function nodesLump(b: Uint8Array): { ofs: number; count: number; dv: DataView } {
  const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  const ofs = dv.getInt32(8 + LUMP_NODES * 8, true);
  const len = dv.getInt32(12 + LUMP_NODES * 8, true);
  return { ofs, count: len / 28, dv };
}

describe.skipIf(!pak)('CM_LoadMap on hostile node graphs', () => {
  it('rejects a node that is its own child (C: CM_PointLeafnum_r never terminates)', () => {
    const b = demo1();
    const { ofs, dv } = nodesLump(b);
    dv.setInt32(ofs + 4, 0, true); // node 0 children[0] = node 0
    expect(() => CollisionModel.load('maps/evil.bsp', b)).toThrow(CMError);
  });

  it('rejects a longer cycle back to the head node', () => {
    const b = demo1();
    const { ofs, count, dv } = nodesLump(b);
    // walk front children from the root to the deepest node and point its front child back to 0
    let node = 0;
    for (;;) {
      const next = dv.getInt32(ofs + node * 28 + 4, true);
      if (next < 0) break;
      node = next;
    }
    expect(node).toBeGreaterThan(0);
    expect(node).toBeLessThan(count);
    dv.setInt32(ofs + node * 28 + 4, 0, true);
    expect(() => CollisionModel.load('maps/evil.bsp', b)).toThrow(CMError);
  });

  it('rejects shared subtrees (C: a chain of them makes CM_RecursiveHullCheck take 2^depth steps)', () => {
    const b = demo1();
    const { ofs, dv } = nodesLump(b);
    const front = dv.getInt32(ofs + 4, true);
    expect(front).toBeGreaterThan(0);
    dv.setInt32(ofs + 8, front, true); // node 0: both children -> the same subtree
    expect(() => CollisionModel.load('maps/evil.bsp', b)).toThrow(CMError);
  });

  it('still loads the unmodified map', () => {
    expect(() => CollisionModel.load('maps/demo1.bsp', demo1())).not.toThrow();
  });

  it('fuzz: random in-range node children never hang the queries', () => {
    let s = 12345;
    const r = () => ((s = (Math.imul(s, 1103515245) + 12345) >>> 0), s);
    const pts = [vec3(0, 0, 0), vec3(100, -200, 50), vec3(-1000, 300, 20), vec3(4000, 4000, 4000)];
    for (let it = 0; it < 60; it++) {
      const b = demo1();
      const { ofs, count, dv } = nodesLump(b);
      for (let k = 0; k < 1 + (r() % 4); k++) {
        const n = r() % count;
        dv.setInt32(ofs + n * 28 + 4 + (r() & 1) * 4, (r() % (2 * count)) - count, true);
      }
      let m: CollisionModel;
      try {
        m = CollisionModel.load('maps/fuzz.bsp', b);
      } catch (e) {
        if (e instanceof CMError || e instanceof FormatError) continue;
        throw e;
      }
      const w = new CollisionWorld(m);
      for (const p of pts) {
        w.pointContents(p, 0);
        w.boxTrace(
          p,
          vec3(p[0]! + 500, p[1]! - 300, p[2]! - 100),
          vec3(-16, -16, -24),
          vec3(16, 16, 32),
          0,
          MASK_ALL,
        );
      }
    }
  });
});
