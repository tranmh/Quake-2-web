import { bench, describe } from 'vitest';
import {
  buildNavOverlay,
  cullOverlay,
  parseNavDump,
  shortestPath,
  type NavDump,
  type RouteTable,
} from '../src/navOverlay';

// Deterministic grid graph of n*n nodes, 4-neighbour edges both ways, plus some volumes and solids.
function makeDump(n: number): Record<string, unknown> {
  const nodes: number[][] = [];
  const edges: number[][] = [];
  for (let y = 0; y < n; y++)
    for (let x = 0; x < n; x++) {
      const i = y * n + x;
      nodes.push([x * 32, y * 32, (i % 7) * 8, i % 5, 0]);
      if (x + 1 < n) edges.push([i, i + 1, i % 3, i % 11 === 0 ? 1 : 0, 0], [i + 1, i, i % 3, 0, 0]);
      if (y + 1 < n) edges.push([i, i + n, 0, 0, 0], [i + n, i, 0, 0, 0]);
    }
  const boxes = Math.max(1, n);
  const box = (k: number) => ({ min: [k * 16, k * 8, 0], max: [k * 16 + 32, k * 8 + 32, 64] });
  return {
    schema: 'q2nav.dump/2',
    map: 'bench',
    checksum: 1,
    skill: 1,
    edgeKinds: ['walk', 'jump', 'drop'],
    nodeFlags: ['spawn', 'end', 'mover', 'ladder', 'water'],
    edgeFlags: [],
    nodes,
    edges,
    volumes: Array.from({ length: boxes }, (_, k) => ({
      ...box(k),
      kind: 'trigger',
      entity: k + 1,
      model: `*${k + 1}`,
    })),
    movers: [],
    lasers: [],
    spawns: [{ entity: 0, origin: [0, 0, 0], node: 0 }],
    solids: Array.from({ length: boxes }, (_, k) => ({ ...box(k), entity: 1000 + k })),
    buttons: [1000],
  };
}

function makeRoute(n: number): RouteTable {
  const steps = [];
  for (let k = 1; k <= 8; k++) steps.push({ op: 'goto', pos: [((k * n) / 9) * 32, ((k * n) / 9) * 32, 0] });
  return { name: 'r', map: 'bench', steps };
}

const SIZES: [string, number][] = [
  ['small 10x10', 10],
  ['medium 40x40', 40],
  ['large 120x120', 120],
];
let sink: unknown;

describe('parseNavDump', () => {
  for (const [name, n] of SIZES) {
    const raw = makeDump(n);
    bench(name, () => {
      sink = parseNavDump(raw);
    });
  }
});

describe('buildNavOverlay (all layers, route)', () => {
  for (const [name, n] of SIZES) {
    const d = parseNavDump(makeDump(n));
    const route = makeRoute(n);
    bench(name, () => {
      sink = buildNavOverlay(d, { route });
    });
  }
});

describe('shortestPath corner to corner', () => {
  for (const [name, n] of SIZES) {
    const d: NavDump = parseNavDump(makeDump(n));
    bench(name, () => {
      sink = shortestPath(d, 0, n * n - 1);
    });
  }
});

describe('cullOverlay', () => {
  for (const [name, n] of SIZES) {
    const o = buildNavOverlay(parseNavDump(makeDump(n)), { route: makeRoute(n) });
    const eye: [number, number, number] = [n * 16, n * 16, 32];
    bench(name, () => {
      sink = cullOverlay(o, eye, { radius: 1024, maxPoints: 512, maxLines: 1024 });
    });
  }
});

export const keep = () => sink;
