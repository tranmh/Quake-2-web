// Nav overlay for the dev harness (?nav=1): turns a `q2nav dump` file (schema q2nav.dump/2, see
// server/cmd/q2nav/dump.go) and optionally a route table (fixtures/agent/routes/*.json) into coloured
// points and line segments, which main.ts draws with the harness's particle and RF_BEAM paths.
//
// Pure data in, data out: no DOM and no renderer, so it is unit tested under vitest (test/).
// Colours are indexes into the Quake II palette (pics/colormap.pcx), as particles and beams take them.

export type Vec3 = [number, number, number];

export const DUMP_SCHEMA = 'q2nav.dump/2';

export interface DumpBox {
  min: Vec3;
  max: Vec3;
}
export interface DumpVolume extends DumpBox {
  kind: string;
  entity: number;
  class?: string;
  model?: string;
  pose: number;
}
export interface DumpPose extends DumpBox {
  name: string;
}
export interface DumpMover {
  entity: number;
  class: string;
  model: string;
  kind: string;
  spawn: number;
  gone?: boolean;
  poses: DumpPose[];
}
export interface DumpLaser {
  entity: number;
  start: Vec3;
  end: Vec3;
  on: boolean;
}
export interface DumpSpawn {
  entity: number;
  targetname?: string;
  origin: Vec3;
  node: number;
}
export interface DumpSolid extends DumpBox {
  entity: number;
  class?: string;
  model?: string;
  box?: boolean;
}

/** A `q2nav dump` file. Nodes are [x, y, z, flags, region]; edges [from, to, kind, conditional, flags]. */
export interface NavDump {
  schema: string;
  map: string;
  checksum: number;
  skill: number;
  edgeKinds: string[];
  nodeFlags: string[];
  edgeFlags: string[];
  nodes: [number, number, number, number, number][];
  edges: [number, number, number, number, number][];
  volumes: DumpVolume[];
  movers: DumpMover[];
  lasers: DumpLaser[];
  spawns: DumpSpawn[];
  solids: DumpSolid[];
  buttons: number[];
}

/** An entity reference of a route table: every given field must match. */
export interface RouteRef {
  entity?: number;
  model?: string;
  classname?: string;
  targetname?: string;
}
export interface RouteStep {
  op: string;
  target?: RouteRef;
  pos?: number[];
  class?: string;
  until?: string;
  yaw?: number;
  seconds?: number;
  note?: string;
}
/** The fields of a route table (fixtures/agent/routes/README.md) the overlay uses. */
export interface RouteTable {
  name: string;
  map: string;
  visit?: number;
  from?: string;
  steps: RouteStep[];
  avoid?: { target: RouteRef; why?: string }[];
}

// ---------------------------------------------------------------- parsing

function fail(msg: string): never {
  throw new Error(msg);
}

function isNum(v: unknown): v is number {
  return typeof v === 'number' && Number.isFinite(v);
}

function vec3(v: unknown, what: string): Vec3 {
  if (!Array.isArray(v) || v.length !== 3 || !v.every(isNum)) fail(`${what}: not an [x, y, z] array`);
  const [x, y, z] = v as Vec3;
  return [x, y, z];
}

function list<T>(o: Record<string, unknown>, key: string, item: (v: unknown, i: number) => T): T[] {
  const v = o[key];
  if (!Array.isArray(v)) fail(`${key}: not an array`);
  return v.map(item);
}

function obj(v: unknown, what: string): Record<string, unknown> {
  if (typeof v !== 'object' || v === null || Array.isArray(v)) fail(`${what}: not an object`);
  return v as Record<string, unknown>;
}

function str(v: unknown, what: string, optional = false): string {
  if (v === undefined && optional) return '';
  if (typeof v !== 'string') fail(`${what}: not a string`);
  return v;
}

function int(v: unknown, what: string): number {
  if (!isNum(v) || !Number.isInteger(v)) fail(`${what}: not an integer`);
  return v;
}

/**
 * Validates a parsed `q2nav dump` JSON document: the schema, every list and tuple shape, and that every
 * edge and spawn names an existing node. Throws a descriptive error on the first problem.
 */
export function parseNavDump(raw: unknown): NavDump {
  try {
    return parseDump(raw);
  } catch (e) {
    throw new Error(`nav dump: ${e instanceof Error ? e.message : String(e)}`);
  }
}

function parseDump(raw: unknown): NavDump {
  const o = obj(raw, 'document');
  if (o['schema'] !== DUMP_SCHEMA) fail(`schema ${JSON.stringify(o['schema'])}, want ${DUMP_SCHEMA}`);
  const names = (k: string) => list(o, k, (v, i) => str(v, `${k}[${i}]`));
  const nodes = list(o, 'nodes', (v, i) => {
    if (!Array.isArray(v) || v.length !== 5 || !v.every(isNum))
      fail(`nodes[${i}]: not [x, y, z, flags, region]`);
    return v as [number, number, number, number, number];
  });
  const edges = list(o, 'edges', (v, i) => {
    if (!Array.isArray(v) || v.length !== 5 || !v.every((x) => isNum(x) && Number.isInteger(x)))
      fail(`edges[${i}]: not [from, to, kind, conditional, flags]`);
    const e = v as [number, number, number, number, number];
    if (e[0] < 0 || e[0] >= nodes.length || e[1] < 0 || e[1] >= nodes.length)
      fail(`edges[${i}]: node out of range`);
    return e;
  });
  const box = (v: Record<string, unknown>, what: string): DumpBox => ({
    min: vec3(v['min'], `${what}.min`),
    max: vec3(v['max'], `${what}.max`),
  });
  const volumes = list(o, 'volumes', (v, i): DumpVolume => {
    const x = obj(v, `volumes[${i}]`);
    return {
      ...box(x, `volumes[${i}]`),
      kind: str(x['kind'], `volumes[${i}].kind`),
      entity: int(x['entity'], `volumes[${i}].entity`),
      class: str(x['class'], `volumes[${i}].class`, true),
      model: str(x['model'], `volumes[${i}].model`, true),
      pose: int(x['pose'] ?? -1, `volumes[${i}].pose`),
    };
  });
  const movers = list(o, 'movers', (v, i): DumpMover => {
    const x = obj(v, `movers[${i}]`);
    const poses = list(x, 'poses', (p, j): DumpPose => {
      const y = obj(p, `movers[${i}].poses[${j}]`);
      return { ...box(y, `movers[${i}].poses[${j}]`), name: str(y['name'], `movers[${i}].poses[${j}].name`) };
    });
    return {
      entity: int(x['entity'], `movers[${i}].entity`),
      class: str(x['class'], `movers[${i}].class`, true),
      model: str(x['model'], `movers[${i}].model`, true),
      kind: str(x['kind'], `movers[${i}].kind`, true),
      spawn: int(x['spawn'] ?? 0, `movers[${i}].spawn`),
      gone: x['gone'] === true,
      poses,
    };
  });
  const lasers = list(o, 'lasers', (v, i): DumpLaser => {
    const x = obj(v, `lasers[${i}]`);
    return {
      entity: int(x['entity'], `lasers[${i}].entity`),
      start: vec3(x['start'], `lasers[${i}].start`),
      end: vec3(x['end'], `lasers[${i}].end`),
      on: x['on'] === true,
    };
  });
  const spawns = list(o, 'spawns', (v, i): DumpSpawn => {
    const x = obj(v, `spawns[${i}]`);
    const node = int(x['node'], `spawns[${i}].node`);
    if (node >= nodes.length) fail(`spawns[${i}]: node out of range`);
    return {
      entity: int(x['entity'], `spawns[${i}].entity`),
      targetname: str(x['targetname'], `spawns[${i}].targetname`, true),
      origin: vec3(x['origin'], `spawns[${i}].origin`),
      node,
    };
  });
  const solids = list(o, 'solids', (v, i): DumpSolid => {
    const x = obj(v, `solids[${i}]`);
    return {
      ...box(x, `solids[${i}]`),
      entity: int(x['entity'], `solids[${i}].entity`),
      class: str(x['class'], `solids[${i}].class`, true),
      model: str(x['model'], `solids[${i}].model`, true),
      box: x['box'] === true,
    };
  });
  return {
    schema: DUMP_SCHEMA,
    map: str(o['map'], 'map'),
    checksum: int(o['checksum'], 'checksum'),
    skill: int(o['skill'], 'skill'),
    edgeKinds: names('edgeKinds'),
    nodeFlags: names('nodeFlags'),
    edgeFlags: names('edgeFlags'),
    nodes,
    edges,
    volumes,
    movers,
    lasers,
    spawns,
    solids,
    buttons: list(o, 'buttons', (v, i) => int(v, `buttons[${i}]`)),
  };
}

// ---------------------------------------------------------------- colours (palette indexes)

export const PAL = {
  red: 0xf2,
  green: 0xd0,
  blue: 0xf3,
  yellow: 0xdc,
  white: 0xd7,
  orange: 0xe2,
  lightblue: 0xb0,
  steel: 0x6f,
  pink: 0xf7,
  lime: 0xd3,
  gray: 0x09,
  darkgray: 0x05,
} as const;

/** Edges drawn in red whatever their kind: they hold only with a mover in a pose, or a blocker gone. */
export const CONDITIONAL_COLOR = PAL.red;

/** Edge colour by kind name (q2nav dump edgeKinds). */
export const EDGE_COLORS: Record<string, number> = {
  walk: PAL.gray,
  crouch: PAL.steel,
  jump: PAL.yellow,
  drop: PAL.orange,
  ladder: PAL.lime,
  swim: PAL.blue,
  waterjump: PAL.lightblue,
  ride: PAL.white,
  teleport: PAL.pink,
  touch: PAL.green,
};

/** Node colour by flag name, the first flag of this list a node has wins; plain floor nodes are green. */
export const NODE_FLAG_COLORS: [string, number][] = [
  ['spawn', PAL.white],
  ['end', PAL.pink],
  ['mover', PAL.orange],
  ['ladder', PAL.lime],
  ['water', PAL.blue],
  ['breath', PAL.lightblue],
  ['crouch', PAL.steel],
  ['ledge', PAL.yellow],
];
export const NODE_COLOR = PAL.green;

/** Box colours: trigger volumes by kind, movers (spawn pose, other poses, gone), static solids. */
export const BOX_COLORS = {
  trigger: PAL.yellow,
  doortrigger: PAL.steel,
  item: PAL.lightblue,
  volume: PAL.gray,
  mover: PAL.orange,
  moverPose: PAL.pink,
  solid: PAL.darkgray,
  button: PAL.white,
  laserOn: PAL.red,
  laserOff: PAL.darkgray,
};

/** The route's graph path (thick) and the straight legs between its waypoints (thin). */
export const ROUTE_COLOR = PAL.white;
export const ROUTE_LEG_COLOR = PAL.lightblue;

// ---------------------------------------------------------------- overlay

export type Layer = 'nodes' | 'edges' | 'boxes' | 'route';
export const LAYERS: readonly Layer[] = ['nodes', 'edges', 'boxes', 'route'];

export interface OverlayPoint {
  origin: Vec3;
  color: number;
}
export interface OverlayLine {
  from: Vec3;
  to: Vec3;
  color: number;
  /** RF_BEAM diameter (the beam's radius is trunc(width / 2); below 2 it would not draw). */
  width: number;
  layer: Layer;
}
/** A route step placed in the map. */
export interface Waypoint {
  step: number; // -1: the arrival spawn point
  op: string;
  label: string;
  pos: Vec3;
  node: number; // the nearest nav node, -1 if none
}
export interface NavOverlay {
  map: string;
  points: OverlayPoint[];
  lines: OverlayLine[];
  waypoints: Waypoint[];
  /** Route steps that could not be placed (wait, confirm, face, or an unresolved reference). */
  unplaced: string[];
  /** Graph path of the route (node indexes), when one was found between consecutive waypoints. */
  routeNodes: number[];
}

export interface OverlayOptions {
  layers?: readonly Layer[];
  route?: RouteTable | null;
}

const EDGE_WIDTH = 2;
const BOX_WIDTH = 2;
const ROUTE_WIDTH = 6;
const LEG_WIDTH = 2;

function nodePos(d: NavDump, i: number): Vec3 {
  const n = d.nodes[i]!;
  return [n[0], n[1], n[2]];
}

/** The colour of a node from its flags. */
export function nodeColor(d: NavDump, flags: number): number {
  for (const [name, color] of NODE_FLAG_COLORS) {
    const bit = d.nodeFlags.indexOf(name);
    if (bit >= 0 && flags & (1 << bit)) return color;
  }
  return NODE_COLOR;
}

/** The colour of an edge from its kind index and conditional flag. */
export function edgeColor(d: NavDump, kind: number, conditional: boolean): number {
  if (conditional) return CONDITIONAL_COLOR;
  return EDGE_COLORS[d.edgeKinds[kind] ?? ''] ?? PAL.gray;
}

/** The 12 edges of an axis aligned box. */
export function boxLines(b: DumpBox, color: number, width: number, layer: Layer = 'boxes'): OverlayLine[] {
  const [x0, y0, z0] = b.min;
  const [x1, y1, z1] = b.max;
  const c: Vec3[] = [
    [x0, y0, z0],
    [x1, y0, z0],
    [x1, y1, z0],
    [x0, y1, z0],
    [x0, y0, z1],
    [x1, y0, z1],
    [x1, y1, z1],
    [x0, y1, z1],
  ];
  const pairs = [
    [0, 1],
    [1, 2],
    [2, 3],
    [3, 0],
    [4, 5],
    [5, 6],
    [6, 7],
    [7, 4],
    [0, 4],
    [1, 5],
    [2, 6],
    [3, 7],
  ];
  return pairs.map(([a, b2]) => ({ from: c[a!]!, to: c[b2!]!, color, width, layer }));
}

function center(b: DumpBox): Vec3 {
  return [(b.min[0] + b.max[0]) / 2, (b.min[1] + b.max[1]) / 2, (b.min[2] + b.max[2]) / 2];
}

function refMatches(ref: RouteRef, ent: { entity: number; model?: string; class?: string }): boolean {
  if (ref.entity === undefined && ref.model === undefined) return false; // a targetname alone: not in the dump
  if (ref.entity !== undefined && ref.entity !== ent.entity) return false;
  if (ref.model !== undefined && ref.model !== (ent.model ?? '')) return false;
  if (ref.classname !== undefined && ent.class && ref.classname !== ent.class) return false;
  return true;
}

/**
 * The box a route reference names in the dump: a mover (its spawn pose, or the pose `pose` names, e.g.
 * a ride's `until`), a static solid such as a button, or a volume (trigger, a door's trigger box, an
 * item). With preferVolumes (touch steps) volumes are searched first. Null when the dump has no such
 * entity (monsters and point entities are not in it).
 */
export function resolveRef(d: NavDump, ref: RouteRef, pose?: string, preferVolumes = false): DumpBox | null {
  const volume = (): DumpBox | null => d.volumes.find((v) => refMatches(ref, v)) ?? null;
  if (preferVolumes) {
    const v = volume();
    if (v) return v;
  }
  for (const m of d.movers) {
    if (!refMatches(ref, m)) continue;
    const p = (pose && m.poses.find((x) => x.name === pose)) || m.poses[m.spawn] || m.poses[0];
    if (p) return p;
  }
  for (const s of d.solids) if (refMatches(ref, s)) return s;
  return volume();
}

/** The node nearest to p (squared distance), -1 for an empty graph. */
export function nearestNode(d: NavDump, p: Vec3): number {
  let best = -1;
  let bestD = Infinity;
  for (let i = 0; i < d.nodes.length; i++) {
    const n = d.nodes[i]!;
    const dx = n[0] - p[0];
    const dy = n[1] - p[1];
    const dz = n[2] - p[2];
    const dd = dx * dx + dy * dy + dz * dz;
    if (dd < bestD) {
      bestD = dd;
      best = i;
    }
  }
  return best;
}

/**
 * Places the route table's steps: the arrival spawn point (`from`), then every step with a position
 * (its `pos`, or the centre of the box its target names). Steps without a place are listed in unplaced.
 */
export function routeWaypoints(d: NavDump, table: RouteTable): { waypoints: Waypoint[]; unplaced: string[] } {
  const waypoints: Waypoint[] = [];
  const unplaced: string[] = [];
  const from = table.from ?? '';
  const spawn = d.spawns.find((s) => (s.targetname ?? '') === from);
  if (spawn) {
    waypoints.push({
      step: -1,
      op: 'spawn',
      label: from ? `spawn ${from}` : 'spawn',
      pos: spawn.origin,
      node: spawn.node,
    });
  }
  table.steps.forEach((s, i) => {
    const name =
      s.target?.model ?? (s.target?.entity !== undefined ? `#${s.target.entity}` : (s.class ?? ''));
    const label = `${i} ${s.op}${name ? ' ' + name : ''}`;
    let pos: Vec3 | null = null;
    if (s.pos && s.pos.length === 3 && s.pos.every(isNum)) pos = [s.pos[0]!, s.pos[1]!, s.pos[2]!];
    else if (s.target) {
      const b = resolveRef(d, s.target, s.op === 'ride' ? s.until : undefined, s.op === 'touch');
      if (b) pos = center(b);
    }
    if (!pos) {
      unplaced.push(label);
      return;
    }
    waypoints.push({ step: i, op: s.op, label, pos, node: nearestNode(d, pos) });
  });
  return { waypoints, unplaced };
}

/** Min-heap of [cost, node] for shortestPath. */
class Heap {
  private a: [number, number][] = [];
  get size(): number {
    return this.a.length;
  }
  push(x: [number, number]): void {
    const a = this.a;
    a.push(x);
    let i = a.length - 1;
    while (i > 0) {
      const p = (i - 1) >> 1;
      if (a[p]![0] <= a[i]![0]) break;
      [a[p], a[i]] = [a[i]!, a[p]!];
      i = p;
    }
  }
  pop(): [number, number] {
    const a = this.a;
    const top = a[0]!;
    const last = a.pop()!;
    if (a.length > 0) {
      a[0] = last;
      let i = 0;
      for (;;) {
        const l = 2 * i + 1;
        const r = l + 1;
        let m = i;
        if (l < a.length && a[l]![0] < a[m]![0]) m = l;
        if (r < a.length && a[r]![0] < a[m]![0]) m = r;
        if (m === i) break;
        [a[m], a[i]] = [a[i]!, a[m]!];
        i = m;
      }
    }
    return top;
  }
}

/**
 * The shortest path from node a to node b over all the dump's edges, conditional ones included (the route
 * opens them), with straight-line length as the cost. It is a visual aid: the agent's planner uses the
 * graph's real edge costs and conditions, which the dump does not carry. Null when b is unreachable.
 */
export function shortestPath(d: NavDump, a: number, b: number, adj?: number[][]): number[] | null {
  if (a < 0 || b < 0 || a >= d.nodes.length || b >= d.nodes.length) return null;
  const out = adj ?? adjacency(d);
  const dist = new Float64Array(d.nodes.length).fill(Infinity);
  const via = new Int32Array(d.nodes.length).fill(-1);
  dist[a] = 0;
  const q = new Heap();
  q.push([0, a]);
  while (q.size > 0) {
    const [c, n] = q.pop();
    if (c > dist[n]!) continue;
    if (n === b) break;
    const p = d.nodes[n]!;
    for (const m of out[n]!) {
      const t = d.nodes[m]!;
      const nc = c + Math.hypot(t[0] - p[0], t[1] - p[1], t[2] - p[2]) + 1;
      if (nc < dist[m]!) {
        dist[m] = nc;
        via[m] = n;
        q.push([nc, m]);
      }
    }
  }
  if (dist[b] === Infinity) return null;
  const path = [b];
  for (let n = b; n !== a;) {
    n = via[n]!;
    path.push(n);
  }
  return path.reverse();
}

/** Outgoing neighbours of every node. */
export function adjacency(d: NavDump): number[][] {
  const out: number[][] = d.nodes.map(() => []);
  for (const e of d.edges) if (e[0] !== e[1]) out[e[0]]!.push(e[1]);
  return out;
}

/** Builds the whole overlay: what main.ts draws after culling. */
export function buildNavOverlay(d: NavDump, opts: OverlayOptions = {}): NavOverlay {
  const layers = new Set(opts.layers ?? LAYERS);
  const points: OverlayPoint[] = [];
  const lines: OverlayLine[] = [];
  if (layers.has('nodes')) {
    for (const n of d.nodes) points.push({ origin: [n[0], n[1], n[2]], color: nodeColor(d, n[3]) });
  }
  if (layers.has('edges')) {
    // one segment per node pair and kind: a conditional edge wins over the plain one of the other way
    const seen = new Map<string, number>();
    for (const e of d.edges) {
      if (e[0] === e[1]) continue; // touch approach edges start and end on one node
      const key = `${Math.min(e[0], e[1])}:${Math.max(e[0], e[1])}:${e[2]}`;
      const cond = e[3] !== 0;
      const at = seen.get(key);
      if (at !== undefined) {
        if (cond) lines[at]!.color = CONDITIONAL_COLOR;
        continue;
      }
      seen.set(key, lines.length);
      lines.push({
        from: nodePos(d, e[0]),
        to: nodePos(d, e[1]),
        color: edgeColor(d, e[2], cond),
        width: EDGE_WIDTH,
        layer: 'edges',
      });
    }
  }
  if (layers.has('boxes')) {
    for (const v of d.volumes) {
      const color = (BOX_COLORS as Record<string, number>)[v.kind] ?? BOX_COLORS.volume;
      lines.push(...boxLines(v, color, BOX_WIDTH));
    }
    for (const m of d.movers) {
      m.poses.forEach((p, i) => {
        const color = m.gone || i !== m.spawn ? BOX_COLORS.moverPose : BOX_COLORS.mover;
        lines.push(...boxLines(p, color, BOX_WIDTH));
      });
    }
    const buttons = new Set(d.buttons);
    for (const s of d.solids)
      lines.push(...boxLines(s, buttons.has(s.entity) ? BOX_COLORS.button : BOX_COLORS.solid, BOX_WIDTH));
    for (const l of d.lasers)
      lines.push({
        from: l.start,
        to: l.end,
        color: l.on ? BOX_COLORS.laserOn : BOX_COLORS.laserOff,
        width: 4,
        layer: 'boxes',
      });
  }
  let waypoints: Waypoint[] = [];
  let unplaced: string[] = [];
  const routeNodes: number[] = [];
  if (opts.route && layers.has('route')) {
    ({ waypoints, unplaced } = routeWaypoints(d, opts.route));
    const adj = adjacency(d);
    for (let i = 0; i + 1 < waypoints.length; i++) {
      const a = waypoints[i]!;
      const b = waypoints[i + 1]!;
      lines.push({ from: a.pos, to: b.pos, color: ROUTE_LEG_COLOR, width: LEG_WIDTH, layer: 'route' });
      const path = shortestPath(d, a.node, b.node, adj);
      if (!path) continue;
      for (let k = 0; k + 1 < path.length; k++) {
        lines.push({
          from: nodePos(d, path[k]!),
          to: nodePos(d, path[k + 1]!),
          color: ROUTE_COLOR,
          width: ROUTE_WIDTH,
          layer: 'route',
        });
      }
      routeNodes.push(...(routeNodes.length ? path.slice(1) : path));
    }
    for (const w of waypoints) {
      lines.push({
        from: w.pos,
        to: [w.pos[0], w.pos[1], w.pos[2] + 64],
        color: ROUTE_COLOR,
        width: ROUTE_WIDTH,
        layer: 'route',
      });
    }
  }
  return { map: d.map, points, lines, waypoints, unplaced, routeNodes };
}

// ---------------------------------------------------------------- culling

export interface CullOptions {
  /** Only what lies within this distance of the eye is drawn. */
  radius: number;
  maxPoints: number;
  maxLines: number;
}

function segDist2(p: Vec3, a: Vec3, b: Vec3): number {
  const abx = b[0] - a[0];
  const aby = b[1] - a[1];
  const abz = b[2] - a[2];
  const len2 = abx * abx + aby * aby + abz * abz;
  let t = len2 > 0 ? ((p[0] - a[0]) * abx + (p[1] - a[1]) * aby + (p[2] - a[2]) * abz) / len2 : 0;
  t = Math.max(0, Math.min(1, t));
  const dx = a[0] + abx * t - p[0];
  const dy = a[1] + aby * t - p[1];
  const dz = a[2] + abz * t - p[2];
  return dx * dx + dy * dy + dz * dz;
}

/**
 * What to draw from eye: route lines first (always, nearest first), then the nearest other lines and
 * points within the radius, up to the caps. Ties keep the overlay's order, so the result is stable.
 */
export function cullOverlay(
  o: NavOverlay,
  eye: Vec3,
  opt: CullOptions,
): { points: OverlayPoint[]; lines: OverlayLine[] } {
  const r2 = opt.radius * opt.radius;
  const route: [number, OverlayLine][] = [];
  const near: [number, OverlayLine][] = [];
  for (const l of o.lines) {
    const dd = segDist2(eye, l.from, l.to);
    if (l.layer === 'route') route.push([dd, l]);
    else if (dd <= r2) near.push([dd, l]);
  }
  const byDist = <T>(a: [number, T], b: [number, T]) => a[0] - b[0];
  route.sort(byDist);
  near.sort(byDist);
  const lines = route.slice(0, opt.maxLines).map((x) => x[1]);
  for (const [, l] of near) {
    if (lines.length >= opt.maxLines) break;
    lines.push(l);
  }
  const pts: [number, OverlayPoint][] = [];
  for (const p of o.points) {
    const dx = p.origin[0] - eye[0];
    const dy = p.origin[1] - eye[1];
    const dz = p.origin[2] - eye[2];
    const dd = dx * dx + dy * dy + dz * dz;
    if (dd <= r2) pts.push([dd, p]);
  }
  pts.sort(byDist);
  return { points: pts.slice(0, opt.maxPoints).map((x) => x[1]), lines };
}

/** The first route table of a campaign's visits that plays map (null if none). */
export async function routeForMap(
  map: string,
  load: (file: string) => Promise<unknown>,
): Promise<RouteTable | null> {
  const camp = obj(await load('campaign.json'), 'campaign.json');
  const visits = camp['visits'];
  if (!Array.isArray(visits)) throw new Error('campaign.json: visits is not an array');
  for (const v of visits) {
    if (typeof v !== 'string') continue;
    const t = parseRouteTable(await load(v));
    if (t.map === map) return t;
  }
  return null;
}

/** Checks the fields of a route table the overlay uses. */
export function parseRouteTable(raw: unknown): RouteTable {
  const o = obj(raw, 'route table');
  const name = str(o['name'], 'route table name');
  const map = str(o['map'], 'route table map');
  const steps = o['steps'];
  if (!Array.isArray(steps)) throw new Error(`route table ${name}: steps is not an array`);
  for (const [i, s] of steps.entries()) {
    if (typeof s !== 'object' || s === null || typeof (s as RouteStep).op !== 'string')
      throw new Error(`route table ${name}: step ${i} has no op`);
  }
  return { ...(o as unknown as RouteTable), name, map, steps: steps as RouteStep[] };
}
