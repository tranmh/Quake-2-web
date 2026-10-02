import { existsSync, readFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  BOX_COLORS,
  CONDITIONAL_COLOR,
  EDGE_COLORS,
  NODE_COLOR,
  PAL,
  ROUTE_COLOR,
  buildNavOverlay,
  cullOverlay,
  parseNavDump,
  parseRouteTable,
  resolveRef,
  routeForMap,
  routeWaypoints,
  shortestPath,
  type NavDump,
  type RouteTable,
  type Vec3,
} from '../src/navOverlay';

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '../../../..');

const EDGE_KINDS = [
  '',
  'walk',
  'crouch',
  'jump',
  'drop',
  'ladder',
  'swim',
  'waterjump',
  'ride',
  'teleport',
  'touch',
];
const NODE_FLAGS = ['crouch', 'water', 'breath', 'ladder', 'mover', 'ledge', 'spawn', 'end'];
const flag = (name: string) => 1 << NODE_FLAGS.indexOf(name);
const kind = (name: string) => EDGE_KINDS.indexOf(name);

/**
 * A corridor of nodes 0-1-2-3 along +x (64 u apart), a shortcut 0-3 that is conditional (a door), and an
 * island node 4. Node 0 is the spawn, node 2 is on a mover and also a ledge.
 */
function sample(): Record<string, unknown> {
  return {
    schema: 'q2nav.dump/2',
    map: 'test1',
    checksum: 1,
    skill: 1,
    edgeKinds: EDGE_KINDS,
    nodeFlags: NODE_FLAGS,
    edgeFlags: ['fast', 'step'],
    nodes: [
      [0, 0, 24, flag('spawn'), 0],
      [64, 0, 24, 0, 0],
      [128, 0, 24, flag('mover') | flag('ledge'), 0],
      [192, 0, 24, flag('ledge'), 0],
      [1000, 1000, 24, 0, 1],
    ],
    edges: [
      [0, 1, kind('walk'), 0, 0],
      [1, 0, kind('walk'), 0, 0], // the way back: one segment
      [1, 2, kind('jump'), 0, 0],
      [2, 3, kind('drop'), 0, 0],
      [3, 0, kind('walk'), 1, 0], // conditional one way only: the pair's segment is red
      [0, 3, kind('walk'), 0, 0],
      [2, 2, kind('touch'), 0, 0], // touch approach edge on one node: not drawn
    ],
    volumes: [
      {
        kind: 'trigger',
        entity: 10,
        class: 'trigger_multiple',
        model: '*1',
        pose: -1,
        min: [180, -16, 0],
        max: [204, 16, 64],
      },
      {
        kind: 'doortrigger',
        entity: 20,
        class: 'func_door',
        model: '*2',
        pose: -1,
        min: [90, -60, 0],
        max: [140, 60, 80],
      },
    ],
    movers: [
      {
        entity: 20,
        class: 'func_door',
        model: '*2',
        kind: 'door',
        spawn: 0,
        poses: [
          { name: 'pos1', min: [100, -32, 0], max: [130, 32, 64] },
          { name: 'pos2', min: [100, -32, 60], max: [130, 32, 124] },
        ],
      },
    ],
    lasers: [{ entity: 30, start: [50, -50, 30], end: [50, 50, 30], on: true }],
    spawns: [
      { entity: 1, origin: [0, 0, 32], node: 0 },
      { entity: 2, targetname: 'base2', origin: [192, 0, 32], node: 3 },
    ],
    solids: [
      { entity: 40, class: 'func_button', model: '*3', min: [60, 20, 20], max: [68, 28, 36] },
      { entity: 41, class: 'misc_explobox', box: true, min: [0, 40, 0], max: [32, 72, 40] },
    ],
    buttons: [40],
  };
}

const dump = (): NavDump => parseNavDump(sample());

const table: RouteTable = parseRouteTable({
  schema: 1,
  name: 'test1',
  map: 'test1',
  from: '',
  steps: [
    { op: 'press', target: { entity: 40, model: '*3', classname: 'func_button' } },
    { op: 'goto', target: { entity: 20, model: '*2', classname: 'func_door' } },
    { op: 'ride', target: { model: '*2' }, until: 'pos2' },
    { op: 'wait', seconds: 2 },
    { op: 'kill', class: 'monster_soldier', pos: [150, 0, 24], target: { entity: 99, targetname: 't1' } },
    { op: 'touch', target: { entity: 10, model: '*1', classname: 'trigger_multiple' } },
  ],
});

describe('parseNavDump', () => {
  it('accepts a q2nav dump and keeps every list', () => {
    const d = dump();
    expect(d.map).toBe('test1');
    expect(d.nodes).toHaveLength(5);
    expect(d.edges).toHaveLength(7);
    expect(d.volumes[0]).toMatchObject({ kind: 'trigger', entity: 10, model: '*1' });
    expect(d.movers[0]!.poses.map((p) => p.name)).toEqual(['pos1', 'pos2']);
    expect(d.spawns[1]).toMatchObject({ targetname: 'base2', node: 3 });
    expect(d.solids[1]!.box).toBe(true);
    expect(d.buttons).toEqual([40]);
  });

  it('rejects another schema, bad tuples and dangling node indexes', () => {
    expect(() => parseNavDump({ ...sample(), schema: 'q2nav.dump/1' })).toThrow(/schema/);
    expect(() => parseNavDump(null)).toThrow(/document/);
    expect(() => parseNavDump({ ...sample(), nodes: [[0, 0, 0]] })).toThrow(/nodes\[0\]/);
    expect(() => parseNavDump({ ...sample(), edges: [[0, 9, 1, 0, 0]] })).toThrow(
      /edges\[0\]: node out of range/,
    );
    expect(() => parseNavDump({ ...sample(), edges: [[0, 1.5, 1, 0, 0]] })).toThrow(/edges\[0\]/);
    expect(() => parseNavDump({ ...sample(), spawns: [{ entity: 1, origin: [0, 0, 0], node: 7 }] })).toThrow(
      /spawns\[0\]/,
    );
    expect(() =>
      parseNavDump({ ...sample(), volumes: [{ kind: 'trigger', entity: 1, min: [0, 0], max: [1, 1, 1] }] }),
    ).toThrow(/volumes\[0\]\.min/);
    expect(() => parseNavDump({ ...sample(), lasers: 'none' })).toThrow(/lasers: not an array/);
  });
});

describe('buildNavOverlay', () => {
  it('draws one point per node, coloured by its most significant flag', () => {
    const o = buildNavOverlay(dump(), { layers: ['nodes'] });
    expect(o.points.map((p) => p.color)).toEqual([PAL.white, NODE_COLOR, PAL.orange, PAL.yellow, NODE_COLOR]);
    expect(o.points[1]!.origin).toEqual([64, 0, 24]);
    expect(o.lines).toEqual([]);
  });

  it('draws one segment per node pair and kind; conditional pairs are red; self edges are skipped', () => {
    const o = buildNavOverlay(dump(), { layers: ['edges'] });
    const seg = (a: number, b: number) =>
      o.lines.filter(
        (l) => (l.from[0] === a * 64 && l.to[0] === b * 64) || (l.from[0] === b * 64 && l.to[0] === a * 64),
      );
    expect(o.lines).toHaveLength(4); // 0-1 walk, 1-2 jump, 2-3 drop, 0-3 walk (conditional)
    expect(seg(0, 1).map((l) => l.color)).toEqual([EDGE_COLORS['walk']]);
    expect(seg(1, 2).map((l) => l.color)).toEqual([EDGE_COLORS['jump']]);
    expect(seg(2, 3).map((l) => l.color)).toEqual([EDGE_COLORS['drop']]);
    expect(seg(0, 3).map((l) => l.color)).toEqual([CONDITIONAL_COLOR]);
    expect(o.lines.every((l) => l.width >= 2 && l.layer === 'edges')).toBe(true);
    expect(o.points).toEqual([]);
  });

  it('draws trigger, mover and solid boxes as 12 segments each, and lasers', () => {
    const o = buildNavOverlay(dump(), { layers: ['boxes'] });
    // 2 volumes + 2 mover poses + 2 solids, 12 segments each, plus one laser
    expect(o.lines).toHaveLength(6 * 12 + 1);
    const colors = o.lines.map((l) => l.color);
    const count = (c: number) => colors.filter((x) => x === c).length;
    expect(count(BOX_COLORS.trigger)).toBe(12);
    expect(count(BOX_COLORS.doortrigger)).toBe(12);
    expect(count(BOX_COLORS.mover)).toBe(12); // spawn pose
    expect(count(BOX_COLORS.moverPose)).toBe(12); // pos2
    expect(count(BOX_COLORS.button)).toBe(12);
    expect(count(BOX_COLORS.solid)).toBe(12);
    expect(count(BOX_COLORS.laserOn)).toBe(1);
    // the trigger box's corners
    const trig = o.lines.slice(0, 12);
    const xs = new Set(trig.flatMap((l) => [l.from[0], l.to[0]]));
    expect([...xs].sort((a, b) => a - b)).toEqual([180, 204]);
    for (const l of trig) {
      const axes = [0, 1, 2].filter((k) => l.from[k] !== l.to[k]);
      expect(axes).toHaveLength(1); // every box edge runs along one axis
    }
  });

  it('places the route and draws its graph path', () => {
    const o = buildNavOverlay(dump(), { route: table, layers: ['route'] });
    expect(o.waypoints.map((w) => w.label)).toEqual([
      'spawn',
      '0 press *3',
      '1 goto *2',
      '2 ride *2',
      '4 kill #99',
      '5 touch *1',
    ]);
    expect(o.unplaced).toEqual(['3 wait']);
    expect(o.waypoints[0]!.pos).toEqual([0, 0, 32]);
    expect(o.waypoints[1]!.pos).toEqual([64, 24, 28]); // the button's box centre
    expect(o.waypoints[2]!.pos).toEqual([115, 0, 32]); // the door at its spawn pose, not its trigger box
    expect(o.waypoints[3]!.pos).toEqual([115, 0, 92]); // ride until pos2
    expect(o.waypoints[4]!.pos).toEqual([150, 0, 24]); // a kill step's spawn origin
    expect(o.waypoints[5]!.pos).toEqual([192, 0, 32]); // the trigger volume
    expect(o.lines.every((l) => l.layer === 'route')).toBe(true);
    expect(o.lines.some((l) => l.color === ROUTE_COLOR && l.width === 6)).toBe(true);
    expect(o.routeNodes[0]).toBe(0);
    expect(o.routeNodes.at(-1)).toBe(3);
  });

  it('draws every layer by default and no route without a table', () => {
    const o = buildNavOverlay(dump());
    expect(o.points).toHaveLength(5);
    expect(o.lines).toHaveLength(4 + 6 * 12 + 1);
    expect(o.waypoints).toEqual([]);
  });
});

describe('route helpers', () => {
  it('resolves references like the route tables use them', () => {
    const d = dump();
    expect(resolveRef(d, { model: '*2' })).toMatchObject({ min: [100, -32, 0] }); // the mover, spawn pose
    expect(resolveRef(d, { model: '*2' }, 'pos2')).toMatchObject({ min: [100, -32, 60] });
    expect(resolveRef(d, { model: '*2' }, undefined, true)).toMatchObject({ min: [90, -60, 0] }); // its trigger
    expect(resolveRef(d, { entity: 40, classname: 'func_button' })).toMatchObject({ min: [60, 20, 20] });
    expect(resolveRef(d, { entity: 40, classname: 'func_door' })).toBeNull(); // every field must match
    expect(resolveRef(d, { targetname: 't1' })).toBeNull(); // not in the dump
    expect(resolveRef(d, { entity: 99 })).toBeNull();
  });

  it('starts at the arrival spawn point named by from', () => {
    const { waypoints } = routeWaypoints(dump(), { name: 'x', map: 'test1', from: 'base2', steps: [] });
    expect(waypoints).toEqual([{ step: -1, op: 'spawn', label: 'spawn base2', pos: [192, 0, 32], node: 3 }]);
  });

  it('finds the shortest path and nothing for an island', () => {
    const d = dump();
    expect(shortestPath(d, 0, 3)).toEqual([0, 3]); // the direct (conditional) edge is shorter
    expect(shortestPath(d, 1, 3)).toEqual([1, 2, 3]);
    expect(shortestPath(d, 3, 1)).toEqual([3, 0, 1]); // edges are directed: the drop 2-3 has no way back
    expect(shortestPath(d, 0, 4)).toBeNull();
    expect(shortestPath(d, 0, 0)).toEqual([0]);
    expect(shortestPath(d, -1, 2)).toBeNull();
  });

  it('picks the first campaign visit that plays the map', async () => {
    const files: Record<string, unknown> = {
      'campaign.json': { visits: ['a.json', 'b.json', 'c.json'] },
      'a.json': { name: 'a', map: 'demo1', steps: [] },
      'b.json': { name: 'b', map: 'demo2', steps: [{ op: 'goto', pos: [0, 0, 0] }] },
      'c.json': { name: 'c', map: 'demo2', steps: [] },
    };
    const load = async (f: string) => files[f];
    expect((await routeForMap('demo2', load))?.name).toBe('b');
    expect(await routeForMap('demo9', load)).toBeNull();
    await expect(routeForMap('demo2', async () => ({}))).rejects.toThrow(/visits/);
    expect(() => parseRouteTable({ name: 'x', map: 'y', steps: [{}] })).toThrow(/step 0 has no op/);
  });
});

describe('cullOverlay', () => {
  it('keeps the route, then the nearest lines and points within the radius, up to the caps', () => {
    const o = buildNavOverlay(dump(), { route: table });
    const eye: Vec3 = [0, 0, 24];
    const all = cullOverlay(o, eye, { radius: 1e6, maxPoints: 100, maxLines: 10_000 });
    expect(all.lines).toHaveLength(o.lines.length);
    expect(all.points).toHaveLength(5);

    const near = cullOverlay(o, eye, { radius: 70, maxPoints: 100, maxLines: 10_000 });
    expect(near.points.map((p) => p.origin[0])).toEqual([0, 64]); // nearest first
    const routeCount = o.lines.filter((l) => l.layer === 'route').length;
    expect(near.lines.filter((l) => l.layer === 'route')).toHaveLength(routeCount); // never culled by radius
    expect(near.lines.length).toBeLessThan(o.lines.length);

    const capped = cullOverlay(o, eye, { radius: 1e6, maxPoints: 2, maxLines: 3 });
    expect(capped.points).toHaveLength(2);
    expect(capped.lines).toHaveLength(3);
    expect(capped.lines.every((l) => l.layer === 'route')).toBe(true);
  });
});

// The real dumps (`make nav`) and the checked-in route tables: every route step with a place resolves.
const visits = [
  ['demo1', 'demo1.json'],
  ['demo2', 'demo2a.json'],
  ['demo3', 'demo3.json'],
  ['demo2', 'demo2b.json'],
] as const;
for (const [map, file] of visits) {
  const dumpFile = resolve(process.env['Q2_NAV_DIR'] ?? resolve(repo, 'assets/nav'), `${map}.viz.json`);
  const tableFile = resolve(repo, 'fixtures/agent/routes', file);
  describe.skipIf(!existsSync(dumpFile))(`real dump ${map} with ${file}`, () => {
    it('places every step that has a place and finds a graph path', () => {
      const d = parseNavDump(JSON.parse(readFileSync(dumpFile, 'utf8')));
      const t = parseRouteTable(JSON.parse(readFileSync(tableFile, 'utf8')));
      const o = buildNavOverlay(d, { route: t });
      expect(o.points).toHaveLength(d.nodes.length);
      expect(o.waypoints[0]!.op).toBe('spawn');
      // only steps without a place stay unplaced
      for (const u of o.unplaced) expect(u).toMatch(/^\d+ (wait|confirm|face)\b/);
      expect(o.waypoints.length).toBe(1 + t.steps.length - o.unplaced.length);
      expect(o.routeNodes.length).toBeGreaterThan(1);
      const c = cullOverlay(o, o.waypoints[0]!.pos, { radius: 1024, maxPoints: 3000, maxLines: 3000 });
      expect(c.lines.length).toBeLessThanOrEqual(3000);
      expect(c.points.length).toBeGreaterThan(0);
    });
  });
}
