// Replay feed: the decision trace (q2bot.trace/1 JSON Lines, gzip on disk) indexed by level attempt and
// server frame, translated to the overlay's DecisionView, and shown for the frame a demo is at.
import { gzipSync } from 'node:zlib';
import { beforeEach, describe, expect, it } from 'vitest';
import { useAiStore } from '@/game/aiStore';
import {
  ReplayFeed,
  TraceIndexer,
  answerQuestion,
  attemptFor,
  lastAtOrBefore,
  readLines,
} from '@/game/replayFeed';

// event shapes as server/internal/agent/trace writes them (envelope first, body flattened)
let seq = 0;
const env = (type: string, gms: number, sf: number, lvl: number, map: string, body: object) =>
  JSON.stringify({ v: 1, type, run: 'r1', ep: 0, seq: ++seq, wall: 1, gms, lvl, map, sf, ...body });

const slow = (gms: number, sf: number, req: number, mode: [string, number][], desc: string) =>
  env('decision', gms, sf, 0, 'demo1', {
    lane: 'slow',
    req,
    snap_gms: gms,
    state: { mode: 'explore', objective: { kind: 'goto', desc }, enemies: [] },
    questions: {
      mode: { type: 'choice', criteria: { fight: '', objective: '', pickup: '', retreat: '', explore: '' } },
      danger: { type: 'score', criteria: ['safe: no threat', 'low: weak'] },
    },
    response: {
      model: 'jev-1.13.0',
      answers: {
        mode: {
          type: 'choice',
          choice: mode[0]![0],
          confidence: 0.75,
          probabilities: Object.fromEntries(mode),
        },
        danger: {
          type: 'score',
          score: 0.2,
          confidence: 0.8,
          legend: { '0': 'safe: no threat', '1': 'low: weak' },
          probabilities: { '0': 0.8, '1': 0.2 },
        },
      },
    },
    fields: [{ name: 'mode', value: mode[0]![0], source: 'model', confidence: 0.75 }],
    backend: 'jev',
    model: 'jev-1.13.0',
    latency_ms: 212,
    cost_usd: 0.00002,
  });

const fast = (gms: number, sf: number, req: number, stale = false) =>
  env('decision', gms, sf, 0, 'demo1', {
    lane: 'fast',
    req,
    snap_gms: gms,
    state: { enemies: [{ id: 'e1', class: 'infantry', units: 746 }] },
    questions: { target: { type: 'choice', criteria: { e1: 'infantry', none: 'no enemy' } } },
    response: {
      answers: {
        target: { type: 'choice', choice: 'e1', confidence: 0.77, probabilities: { none: 0.11, e1: 0.89 } },
      },
    },
    backend: 'jev',
    latency_ms: 180,
    cost_usd: 0.00001,
    ...(stale ? { stale: true } : {}),
  });

const tick = (gms: number, sf: number, lvl: number, map: string, mode: string, buttons = 0) =>
  env('decision', gms, sf, lvl, map, {
    lane: 'tick',
    req: 0,
    snap_gms: gms,
    backend: '',
    latency_ms: 0,
    intent: {
      mode,
      target: 'e1',
      fire_policy: 'fire_when_aligned',
      movement: 'strafe_left',
      danger: 0.2,
      fields: [
        { name: 'mode', value: mode, source: 'model', confidence: 0.75 },
        { name: 'target', value: 'e1', source: 'model' },
        { name: 'fire_policy', value: 'hold', source: 'scripted', fallback: 'low_confidence' },
        { name: 'movement', value: 'strafe_left', source: 'stale', fallback: 'ttl' },
        { name: 'weapon', value: 'blaster', source: 'default', fallback: 'not_asked' },
        { name: 'pickup', value: 'none', source: 'reflex', fallback: 'item_unavailable' },
      ],
    },
    tick: { n: 1, mode, step: 2, target: 'e1', move: 'strafe_left', weapon: 'Blaster', health: 100 },
    cmds: [{ msec: 25, buttons, angles: [0, 0, 0], forward: 0, side: 0, up: 0 }],
  });

function trace(): string[] {
  seq = 0;
  return [
    env('run_start', 0, 0, 0, '', {
      schema: 'q2bot.trace/1',
      backend: 'mock',
      model: 'jev-1.13.0',
      maps: ['demo1', 'demo2'],
    }),
    env('episode_start', 1900, 18, 0, 'demo1', { seed: 1 }),
    env('level_start', 1900, 18, 0, 'demo1', { visit: 0, gen: 1 }),
    env('cmds', 2000, 19, 0, 'demo1', { step: 0, cmds: [] }),
    tick(2000, 19, 0, 'demo1', 'objective'),
    slow(
      2300,
      22,
      1,
      [
        ['objective', 0.7],
        ['fight', 0.2],
        ['explore', 0.1],
      ],
      'go to func_door *32',
    ),
    env('api_call', 2300, 22, 0, 'demo1', {
      backend: 'jev',
      lane: 'slow',
      req: 1,
      status: 200,
      latency_ms: 212,
    }),
    tick(2400, 23, 0, 'demo1', 'objective'),
    fast(2500, 24, 2),
    tick(2500, 24, 0, 'demo1', 'fight', 1),
    env('kill', 2600, 25, 0, 'demo1', { target: 'e1', class: 'infantry', weapon: 'Blaster' }),
    fast(2700, 26, 3, true),
    env('death', 2800, 27, 0, 'demo1', { cause: 'hit', health: -5 }),
    env('reload', 3500, 18, 0, 'demo1', { slot: 'save0', deaths: 1 }),
    tick(3600, 19, 0, 'demo1', 'pickup'),
    '{"broken',
    env('level_end', 9000, 5, 0, 'demo1', { outcome: 'exit', reason: 'to demo2' }),
    env('level_start', 9100, 18, 1, 'demo2', { visit: 0, gen: 3 }),
    tick(9200, 20, 1, 'demo2', 'explore'),
    '',
  ];
}

function index() {
  const ix = new TraceIndexer();
  for (const l of trace()) ix.pushLine(l);
  return ix.finish();
}

describe('TraceIndexer', () => {
  it('splits the episode into level attempts like the recorder (level_start, reload)', () => {
    const r = index();
    expect(r.hello).toEqual({
      t: 'hello',
      bot: 'r1',
      backend: 'mock',
      model: 'jev-1.13.0',
      maps: ['demo1', 'demo2'],
    });
    expect(r.attempts.map((a) => `${a.index}:${a.map}:${a.lvl}:${a.decisions.length}`)).toEqual([
      '0:demo1:0:3',
      '1:demo1:0:1',
      '2:demo2:1:1',
    ]);
    expect(r.ticks).toBe(5);
    expect(r.bad).toBe(1);
    expect(r.attempts[0]!.events.map((e) => e.kind)).toEqual(['level_start', 'kill', 'death']);
    // level_end is published with the next level's frame: clamped after the attempt's last tick
    expect(r.attempts[1]!.events.map((e) => `${e.kind}@${e.sf}`)).toEqual(['reload@18', 'level_end@19']);
  });

  it('translates decision ticks to the live feed shape', () => {
    const a = index().attempts[0]!;
    const before = a.decisions[0]!;
    expect(before.questions).toEqual([]);
    expect(before.objective).toBe('route step 2');
    const d = a.decisions[2]!;
    expect(d).toMatchObject({
      sf: 24,
      lvl: 0,
      map: 'demo1',
      mode: 'fight',
      objective: 'go to func_door *32 · step 2',
      target: { id: 'e1', class: 'infantry', dist: 746 },
      provenance: {
        mode: 'model',
        target: 'model',
        fire_policy: 'scripted',
        movement: 'stale',
        weapon: 'default',
        pickup: 'reflex',
      },
      // the reasons as the trace has them (aiView tells fallbacks from overrides)
      fallback: {
        fire_policy: 'low_confidence',
        movement: 'ttl',
        weapon: 'not_asked',
        pickup: 'item_unavailable',
      },
      // the movement executed, the fire policy acted on, the weapon in hand, fired since the last tick
      action: { movement: 'strafe_left', firePolicy: 'hold', weapon: 'Blaster', fire: true },
      latencyMs: 180,
      // the requests answered since the previous tick
      costUsd: 0.00001,
    });
    // every answered question so far, in field order, options in the question's criteria order
    expect(d.questions.map((q) => q.id)).toEqual(['mode', 'target', 'danger']);
    expect(d.questions[0]).toEqual({
      id: 'mode',
      chosen: 'objective',
      confidence: 0.75,
      options: [
        { label: 'fight', p: 0.2 },
        { label: 'objective', p: 0.7 },
        { label: 'pickup', p: 0 },
        { label: 'retreat', p: 0 },
        { label: 'explore', p: 0.1 },
      ],
    });
    expect(d.questions[1]!.options.map((o) => o.label)).toEqual(['e1', 'none']);
    expect(d.questions[2]).toMatchObject({
      chosen: 'safe',
      options: [
        { label: 'safe', p: 0.8 },
        { label: 'low', p: 0.2 },
      ],
    });
  });

  it('keeps running totals per decision', () => {
    const r = index();
    const t = r.attempts[0]!.totals;
    expect(t[0]).toMatchObject({ decisions: 0, kills: 0, costUsd: 0 });
    expect(t[2]).toMatchObject({ decisions: 2, kills: 0, deaths: 0, staleRate: 0, apiP50Ms: 212 });
    expect(t[2]!.costUsd).toBeCloseTo(0.00003, 10);
    const after = r.attempts[1]!.totals[0]!;
    expect(after).toMatchObject({ decisions: 3, kills: 1, deaths: 1 });
    expect(after.staleRate).toBeCloseTo(1 / 3);
    // decided fields only (default excluded): model share of the acted-on values
    expect(after.modelShare).toBeCloseTo(8 / 20);
    expect(r.attempts[1]!.startTotals).toMatchObject({ kills: 1, deaths: 1 });
  });
});

describe('lookup helpers', () => {
  it('finds the last entry at or before a frame', () => {
    const xs = [{ sf: 10 }, { sf: 12 }, { sf: 12 }, { sf: 20 }];
    expect(lastAtOrBefore(xs, 9)).toBe(-1);
    expect(lastAtOrBefore(xs, 12)).toBe(2);
    expect(lastAtOrBefore(xs, 19)).toBe(2);
    expect(lastAtOrBefore(xs, 99)).toBe(3);
    expect(lastAtOrBefore([], 5)).toBe(-1);
  });

  it('maps a demo to its attempt, or the nearest one on the same map', () => {
    const r = index();
    expect(attemptFor(r, 1, 'demo1')?.index).toBe(1);
    expect(attemptFor(r, 1, 'demo2')?.index).toBe(2);
    expect(attemptFor(r, 7, 'demo1')?.index).toBe(1);
    expect(attemptFor(r, 0, 'demo3')).toBeNull();
  });

  it('labels a score answer by its legend and picks the most probable entry', () => {
    const q = answerQuestion(
      'danger',
      { type: 'score', legend: { '0': 'safe: x', '1': 'high: y' }, probabilities: { '0': 0.3, '1': 0.7 } },
      null,
    );
    expect(q).toEqual({
      id: 'danger',
      chosen: 'high',
      options: [
        { label: 'safe', p: 0.3 },
        { label: 'high', p: 0.7 },
      ],
    });
    expect(answerQuestion('x', 'nope', null)).toBeNull();
    // with a score, its rounded level is the chosen one (as the live feed does)
    const scored = answerQuestion(
      'danger',
      { type: 'score', score: 1.4, probabilities: { '0': 0.1, '1': 0.4, '2': 0.5 } },
      { type: 'score', criteria: ['safe: x', 'low: y', 'high: z'] },
    );
    expect(scored).toMatchObject({
      chosen: 'low',
      options: [{ label: 'safe' }, { label: 'low' }, { label: 'high' }],
    });
    expect(answerQuestion('door_open', { type: 'noul', noul: 0.8 }, null)).toEqual({
      id: 'door_open',
      chosen: 'true',
      confidence: expect.closeTo(0.6, 10),
      options: [
        { label: 'true', p: 0.8 },
        { label: 'false', p: expect.closeTo(0.2, 10) },
      ],
    });
  });
});

describe('readLines', () => {
  // JSON Lines: every line ends with a newline (no empty line after the last one)
  const text = trace().join('\n');
  const want = text.split('\n').slice(0, -1);

  it('reads a decoded body split anywhere', async () => {
    const bytes = new TextEncoder().encode(text);
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        for (let i = 0; i < bytes.length; i += 97) c.enqueue(bytes.subarray(i, i + 97));
        c.close();
      },
    });
    const lines: string[] = [];
    await readLines(new Response(body), (l) => lines.push(l));
    expect(lines).toEqual(want);
  });

  it('decompresses a body that is still gzipped', async () => {
    const lines: string[] = [];
    await readLines(new Response(gzipSync(text)), (l) => lines.push(l));
    expect(lines).toEqual(want);
    // a last line without its newline still counts
    const tail: string[] = [];
    await readLines(new Response('{"a":1}\n{"b":2}'), (l) => tail.push(l));
    expect(tail).toEqual(['{"a":1}', '{"b":2}']);
  });
});

describe('ReplayFeed', () => {
  beforeEach(() => useAiStore.getState().reset());

  it('shows the decision of the replayed frame', async () => {
    const feed = new ReplayFeed({
      botId: 'b1',
      episode: 0,
      fetchTrace: async () => new Response(gzipSync(trace().join('\n'))),
    });
    feed.show(0, 'demo1', 23); // before the trace is loaded: remembered
    await feed.start();
    let s = useAiStore.getState();
    expect(s.status).toBe('replay');
    expect(s.hello?.backend).toBe('mock');
    expect(s.latest?.sf).toBe(23);
    expect(s.events.map((e) => e.kind)).toEqual(['level_start']);

    feed.show(0, 'demo1', 30);
    s = useAiStore.getState();
    expect(s.latest).toMatchObject({ sf: 24, mode: 'fight' });
    // totals as of that decision tick (the kill and death came after it)
    expect(s.totals).toMatchObject({ decisions: 2, kills: 0 });
    expect(s.events.map((e) => e.kind)).toEqual(['death', 'kill', 'level_start']);

    // the reloaded attempt: its frames start over
    feed.show(1, 'demo1', 19);
    expect(useAiStore.getState().latest).toMatchObject({ sf: 19, mode: 'pickup', lvl: 1 });
    // before the attempt's first decision: nothing, totals as the attempt began
    feed.show(2, 'demo2', 18);
    s = useAiStore.getState();
    expect(s.latest).toBeNull();
    expect(s.totals).toMatchObject({ decisions: 3, kills: 1 });
    feed.close();
  });

  it('reports a missing trace', async () => {
    const feed = new ReplayFeed({
      botId: 'b1',
      episode: 0,
      fetchTrace: async () => new Response('', { status: 404 }),
    });
    await feed.start();
    expect(useAiStore.getState()).toMatchObject({ status: 'error', error: 'this run has no decision trace' });
  });
});
