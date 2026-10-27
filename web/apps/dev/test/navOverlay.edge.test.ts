import { describe, expect, it } from 'vitest';
import {
  BOX_COLORS,
  CONDITIONAL_COLOR,
  NODE_COLOR,
  PAL,
  adjacency,
  boxLines,
  buildNavOverlay,
  cullOverlay,
  edgeColor,
  nearestNode,
  nodeColor,
  parseNavDump,
  parseRouteTable,
  resolveRef,
  routeForMap,
  routeWaypoints,
  shortestPath,
  type NavDump,
  type NavOverlay,
  type OverlayLine,
} from '../src/navOverlay';

const EDGE_KINDS = ['', 'walk', 'jump'];
const NODE_FLAGS = ['crouch', 'water', 'spawn'];

/** A minimal valid dump: two nodes joined by a walk edge, one spawn on node 0, nothing else. */
function minimal(): Record<string, unknown> {
  return {
    schema: 'q2nav.dump/2',
    map: 'mini',
    checksum: 7,
    skill: 2,
    edgeKinds: EDGE_KINDS,
    nodeFlags: NODE_FLAGS,
    edgeFlags: [],
    nodes: [
      [0, 0, 0, 0, 0],
      [100, 0, 0, 0, 0],
    ],
    edges: [[0, 1, 1, 0, 0]],
    volumes: [],
    movers: [],
    lasers: [],
    spawns: [{ entity: 1, origin: [0, 0, 8], node: 0 }],
    solids: [],
    buttons: [],
  };
}

const parse = (over: Record<string, unknown> = {}): NavDump => parseNavDump({ ...minimal(), ...over });

describe('parseNavDump: defaults and invalid input', () => {
  it('fills the optional fields with their defaults', () => {
    const d = parse({
      volumes: [{ kind: 'item', entity: 3, min: [0, 0, 0], max: [1, 1, 1] }],
      movers: [{ entity: 4, poses: [{ name: 'p', min: [0, 0, 0], max: [1, 1, 1] }] }],
      lasers: [{ entity: 5, start: [0, 0, 0], end: [1, 0, 0] }],
      solids: [{ entity: 6, min: [0, 0, 0], max: [1, 1, 1] }],
    });
    expect(d.volumes[0]).toEqual({
      kind: 'item',
      entity: 3,
      class: '',
      model: '',
      pose: -1,
      min: [0, 0, 0],
      max: [1, 1, 1],
    });
    expect(d.movers[0]).toMatchObject({ class: '', model: '', kind: '', spawn: 0, gone: false });
    expect(d.lasers[0]!.on).toBe(false);
    expect(d.solids[0]!.box).toBe(false);
    expect(d.spawns[0]!.targetname).toBe('');
    expect(d).toMatchObject({ schema: 'q2nav.dump/2', map: 'mini', checksum: 7, skill: 2 });
  });

  it('accepts an empty graph', () => {
    const d = parse({ nodes: [], edges: [], spawns: [] });
    expect(d.nodes).toEqual([]);
    expect(nearestNode(d, [0, 0, 0])).toBe(-1);
  });

  it('prefixes every error with "nav dump:"', () => {
    expect(() => parseNavDump([])).toThrow(/^nav dump: document: not an object$/);
    expect(() => parseNavDump('x')).toThrow(/^nav dump: document/);
  });

  it.each([
    ['missing schema', { schema: undefined }, /schema undefined, want q2nav\.dump\/2/],
    ['map not a string', { map: 3 }, /map: not a string/],
    ['fractional checksum', { checksum: 1.5 }, /checksum: not an integer/],
    ['skill missing', { skill: undefined }, /skill: not an integer/],
    ['edgeKinds not strings', { edgeKinds: ['walk', 2] }, /edgeKinds\[1\]: not a string/],
    ['nodeFlags missing', { nodeFlags: undefined }, /nodeFlags: not an array/],
    ['node with a non-finite coordinate', { nodes: [[0, Infinity, 0, 0, 0]] }, /nodes\[0\]/],
    ['node with six fields', { nodes: [[0, 0, 0, 0, 0, 0]] }, /nodes\[0\]/],
    ['negative edge endpoint', { edges: [[-1, 1, 1, 0, 0]] }, /edges\[0\]: node out of range/],
    ['edge endpoint == node count', { edges: [[0, 2, 1, 0, 0]] }, /edges\[0\]: node out of range/],
    ['edge with a string', { edges: [[0, '1', 1, 0, 0]] }, /edges\[0\]: not \[from/],
    ['volume not an object', { volumes: [[1]] }, /volumes\[0\]: not an object/],
    [
      'volume without kind',
      { volumes: [{ entity: 1, min: [0, 0, 0], max: [1, 1, 1] }] },
      /volumes\[0\]\.kind/,
    ],
    [
      'volume with fractional pose',
      { volumes: [{ kind: 'x', entity: 1, pose: 0.5, min: [0, 0, 0], max: [1, 1, 1] }] },
      /volumes\[0\]\.pose/,
    ],
    ['mover without poses', { movers: [{ entity: 1 }] }, /poses: not an array/],
    [
      'mover pose without name',
      { movers: [{ entity: 1, poses: [{ min: [0, 0, 0], max: [1, 1, 1] }] }] },
      /movers\[0\]\.poses\[0\]\.name/,
    ],
    ['laser with a bad end', { lasers: [{ entity: 1, start: [0, 0, 0], end: [0, 0] }] }, /lasers\[0\]\.end/],
    [
      'spawn node == node count',
      { spawns: [{ entity: 1, origin: [0, 0, 0], node: 2 }] },
      /spawns\[0\]: node out of range/,
    ],
    ['solid with a string max', { solids: [{ entity: 1, min: [0, 0, 0], max: 'x' }] }, /solids\[0\]\.max/],
    ['button not an integer', { buttons: [1.25] }, /buttons\[0\]: not an integer/],
  ])('rejects %s', (_name, over, re) => {
    expect(() => parse(over)).toThrow(re);
  });

  it('accepts the last node as an edge endpoint and spawn node (upper boundary)', () => {
    const d = parse({ edges: [[1, 0, 1, 0, 0]], spawns: [{ entity: 1, origin: [0, 0, 0], node: 1 }] });
    expect(d.edges[0]).toEqual([1, 0, 1, 0, 0]);
    expect(d.spawns[0]!.node).toBe(1);
  });

  // A spawn node of -1 is nav.NoNode: the Go graph validator accepts it (spawn point without a node) and
  // `q2nav dump` writes it through, so the parser must accept it too.
  it('accepts a spawn without a node (node -1, nav.NoNode)', () => {
    const d = parse({ spawns: [{ entity: 1, origin: [0, 0, 0], node: -1 }] });
    expect(d.spawns[0]!.node).toBe(-1);
  });
});

describe('colour helpers', () => {
  const d = parse();

  it('colours a node by the first flag of the priority list, green without one', () => {
    expect(nodeColor(d, 0)).toBe(NODE_COLOR);
    expect(nodeColor(d, 1 << 1)).toBe(PAL.blue); // water
    expect(nodeColor(d, 1 << 0)).toBe(PAL.steel); // crouch
    expect(nodeColor(d, (1 << 0) | (1 << 1) | (1 << 2))).toBe(PAL.white); // spawn beats water and crouch
    expect(nodeColor(d, 1 << 7)).toBe(NODE_COLOR); // a bit the dump does not name
  });

  it('colours an edge by kind, red when conditional, gray for an unknown kind', () => {
    expect(edgeColor(d, 2, false)).toBe(PAL.yellow);
    expect(edgeColor(d, 2, true)).toBe(CONDITIONAL_COLOR);
    expect(edgeColor(d, 0, false)).toBe(PAL.gray); // the '' kind
    expect(edgeColor(d, 99, false)).toBe(PAL.gray);
  });

  it('builds the 12 axis-aligned edges of a box with the given style', () => {
    const lines = boxLines({ min: [0, 0, 0], max: [1, 2, 3] }, 5, 4, 'route');
    expect(lines).toHaveLength(12);
    expect(lines.every((l) => l.color === 5 && l.width === 4 && l.layer === 'route')).toBe(true);
    const corners = new Set(lines.flatMap((l) => [l.from.join(), l.to.join()]));
    expect(corners.size).toBe(8);
    const len = (l: OverlayLine) => Math.hypot(l.to[0] - l.from[0], l.to[1] - l.from[1], l.to[2] - l.from[2]);
    expect(lines.map(len).sort()).toEqual([1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3]);
    expect(boxLines({ min: [0, 0, 0], max: [1, 1, 1] }, 1, 2)[0]!.layer).toBe('boxes');
  });
});

describe('buildNavOverlay boxes', () => {
  it('colours unknown volume kinds as volumes, gone movers as poses only, and lasers that are off', () => {
    const d = parse({
      volumes: [{ kind: 'weird', entity: 1, min: [0, 0, 0], max: [1, 1, 1] }],
      movers: [
        {
          entity: 2,
          gone: true,
          spawn: 0,
          poses: [{ name: 'a', min: [0, 0, 0], max: [1, 1, 1] }],
        },
      ],
      lasers: [{ entity: 3, start: [0, 0, 0], end: [0, 0, 9], on: false }],
    });
    const o = buildNavOverlay(d, { layers: ['boxes'] });
    expect(o.lines).toHaveLength(12 + 12 + 1);
    expect(o.lines.slice(0, 12).every((l) => l.color === BOX_COLORS.volume)).toBe(true);
    expect(o.lines.slice(12, 24).every((l) => l.color === BOX_COLORS.moverPose)).toBe(true);
    expect(o.lines[24]).toEqual({
      from: [0, 0, 0],
      to: [0, 0, 9],
      color: BOX_COLORS.laserOff,
      width: 4,
      layer: 'boxes',
    });
  });

  it('draws nothing with no layers and keeps the map name', () => {
    const o = buildNavOverlay(parse(), { layers: [], route: { name: 'r', map: 'mini', steps: [] } });
    expect(o).toEqual({ map: 'mini', points: [], lines: [], waypoints: [], unplaced: [], routeNodes: [] });
  });

  it('turns a plain segment red when a later conditional edge of the same pair and kind follows', () => {
    const d = parse({
      edges: [
        [0, 1, 1, 0, 0],
        [1, 0, 1, 1, 0],
        [0, 1, 2, 0, 0],
      ],
    });
    const o = buildNavOverlay(d, { layers: ['edges'] });
    expect(o.lines.map((l) => l.color)).toEqual([CONDITIONAL_COLOR, PAL.yellow]);
  });
});

describe('route placement', () => {
  const d = parse({
    movers: [
      {
        entity: 9,
        model: '*9',
        spawn: 5, // out of range: falls back to the first pose
        poses: [{ name: 'p0', min: [0, 0, 0], max: [10, 10, 10] }],
      },
    ],
    solids: [{ entity: 8, model: '*8', min: [90, 0, 0], max: [110, 0, 0] }],
  });

  it('falls back to the first pose and ignores classname when the entity has no class', () => {
    expect(resolveRef(d, { model: '*9' }, 'nope')).toMatchObject({ name: 'p0' });
    expect(resolveRef(d, { entity: 8, classname: 'anything' })).toMatchObject({ min: [90, 0, 0] });
    expect(resolveRef(d, { classname: 'func_door' })).toBeNull(); // no entity or model: no match
  });

  it('labels steps by model, entity, class, places pos first and lists unresolved steps', () => {
    const { waypoints, unplaced } = routeWaypoints(d, {
      name: 'r',
      map: 'mini',
      from: 'nowhere', // no such spawn: no arrival waypoint
      steps: [
        { op: 'goto', pos: [99, 0, 0], target: { model: '*9' } },
        { op: 'press', target: { entity: 8 } },
        { op: 'kill', class: 'monster_tank' },
        { op: 'goto', pos: [1, 2] },
        { op: 'face', yaw: 90 },
      ],
    });
    expect(waypoints).toEqual([
      { step: 0, op: 'goto', label: '0 goto *9', pos: [99, 0, 0], node: 1 },
      { step: 1, op: 'press', label: '1 press #8', pos: [100, 0, 0], node: 1 },
    ]);
    expect(unplaced).toEqual(['2 kill monster_tank', '3 goto', '4 face']);
  });

  it('chains the graph paths of consecutive waypoints without repeating the shared node', () => {
    const d3 = parse({
      nodes: [
        [0, 0, 0, 0, 0],
        [100, 0, 0, 0, 0],
        [200, 0, 0, 0, 0],
      ],
      edges: [
        [0, 1, 1, 0, 0],
        [1, 2, 1, 0, 0],
      ],
    });
    const o = buildNavOverlay(d3, {
      layers: ['route'],
      route: {
        name: 'r',
        map: 'mini',
        steps: [
          { op: 'goto', pos: [100, 0, 0] },
          { op: 'goto', pos: [200, 0, 0] },
        ],
      },
    });
    expect(o.waypoints.map((w) => w.node)).toEqual([0, 1, 2]);
    expect(o.routeNodes).toEqual([0, 1, 2]);
    // 2 legs + 2 path segments + 3 waypoint markers, the markers 64 u tall
    expect(o.lines).toHaveLength(7);
    expect(o.lines.at(-1)).toMatchObject({ from: [200, 0, 0], to: [200, 0, 64] });
  });
});

describe('graph helpers', () => {
  it('skips self edges in the adjacency and treats out-of-range nodes as unreachable', () => {
    const d = parse({
      edges: [
        [0, 0, 1, 0, 0],
        [0, 1, 1, 0, 0],
        [0, 1, 2, 0, 0],
      ],
    });
    expect(adjacency(d)).toEqual([[1, 1], []]);
    expect(shortestPath(d, 0, 2)).toBeNull();
    expect(shortestPath(d, 1, 0)).toBeNull();
    expect(shortestPath(d, 1, 1)).toEqual([1]);
  });

  it('prefers the cheaper of two routes by straight-line length', () => {
    const d = parse({
      nodes: [
        [0, 0, 0, 0, 0],
        [500, 500, 0, 0, 0],
        [50, 0, 0, 0, 0],
        [100, 0, 0, 0, 0],
      ],
      edges: [
        [0, 1, 1, 0, 0],
        [1, 3, 1, 0, 0],
        [0, 2, 1, 0, 0],
        [2, 3, 1, 0, 0],
      ],
    });
    expect(shortestPath(d, 0, 3)).toEqual([0, 2, 3]);
  });

  it('picks the first of equally near nodes', () => {
    expect(nearestNode(parse(), [50, 0, 0])).toBe(0);
    expect(nearestNode(parse(), [51, 0, 0])).toBe(1);
  });
});

describe('cullOverlay boundaries', () => {
  const line = (x: number, layer: OverlayLine['layer'] = 'edges'): OverlayLine => ({
    from: [x, -10, 0],
    to: [x, 10, 0],
    color: 1,
    width: 2,
    layer,
  });
  const overlay = (lines: OverlayLine[], xs: number[] = []): NavOverlay => ({
    map: 'm',
    points: xs.map((x) => ({ origin: [x, 0, 0], color: 1 })),
    lines,
    waypoints: [],
    unplaced: [],
    routeNodes: [],
  });

  it('includes what lies exactly on the radius and excludes what lies beyond', () => {
    const c = cullOverlay(overlay([line(10), line(10.5)], [10, 10.5]), [0, 0, 0], {
      radius: 10,
      maxPoints: 10,
      maxLines: 10,
    });
    expect(c.lines.map((l) => l.from[0])).toEqual([10]);
    expect(c.points.map((p) => p.origin[0])).toEqual([10]);
  });

  it('measures the distance to a segment, not to its ends, and handles zero-length segments', () => {
    const dot: OverlayLine = { from: [3, 4, 0], to: [3, 4, 0], color: 1, width: 2, layer: 'boxes' };
    const c = cullOverlay(overlay([line(5), dot]), [0, 0, 0], { radius: 5, maxPoints: 0, maxLines: 10 });
    expect(c.lines).toHaveLength(2); // the segment passes 5 u away; the dot is at 5 u
    expect(c.points).toEqual([]);
  });

  it('caps the route lines too and keeps the overlay order on ties', () => {
    const a = line(1, 'route');
    const b = line(-1, 'route');
    const c = line(-1);
    const d = line(1);
    const o = overlay([a, b, c, d]);
    expect(cullOverlay(o, [0, 0, 0], { radius: 100, maxPoints: 0, maxLines: 1 }).lines).toEqual([a]);
    expect(cullOverlay(o, [0, 0, 0], { radius: 100, maxPoints: 0, maxLines: 4 }).lines).toEqual([a, b, c, d]);
    expect(cullOverlay(o, [1e6, 0, 0], { radius: 1, maxPoints: 0, maxLines: 0 }).lines).toEqual([]);
  });
});

describe('route table loading', () => {
  it('rejects malformed route tables and campaigns', async () => {
    expect(() => parseRouteTable(null)).toThrow(/route table: not an object/);
    expect(() => parseRouteTable({ map: 'm', steps: [] })).toThrow(/route table name/);
    expect(() => parseRouteTable({ name: 'n', steps: [] })).toThrow(/route table map/);
    expect(() => parseRouteTable({ name: 'n', map: 'm', steps: {} })).toThrow(
      /route table n: steps is not an array/,
    );
    expect(() => parseRouteTable({ name: 'n', map: 'm', steps: [{ op: 'goto' }, null] })).toThrow(
      /step 1 has no op/,
    );
    await expect(routeForMap('m', async () => [])).rejects.toThrow(/campaign\.json: not an object/);
  });

  it('keeps extra fields and skips visits that are not file names', async () => {
    const t = parseRouteTable({ name: 'n', map: 'm', visit: 2, from: 'x', steps: [] });
    expect(t).toMatchObject({ visit: 2, from: 'x' });
    const loaded: string[] = [];
    const files: Record<string, unknown> = {
      'campaign.json': { visits: [3, null, 'a.json'] },
      'a.json': { name: 'a', map: 'm', steps: [] },
    };
    const r = await routeForMap('m', async (f) => {
      loaded.push(f);
      return files[f];
    });
    expect(r?.name).toBe('a');
    expect(loaded).toEqual(['campaign.json', 'a.json']);
  });

  it('propagates a broken route table of a visit', async () => {
    const files: Record<string, unknown> = {
      'campaign.json': { visits: ['a.json'] },
      'a.json': { name: 'a' },
    };
    await expect(routeForMap('m', async (f) => files[f])).rejects.toThrow(/route table map/);
  });
});
