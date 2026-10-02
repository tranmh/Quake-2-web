// GameSession watch and replay sources, started end to end with the renderer, sound, asset index and the
// client engine replaced by doubles: a viewer's session registers no key / mouse / wheel listeners, never
// asks for pointer lock or opens the menu (no single-player pause), never saves the player's config, sets
// cl_predict 0, connects to the bot (watch) or plays the run's demos in order without network (replay),
// and tells the AI overlay which server frame is on screen.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ClientEngineOptions } from 'q2-client';

const h = vi.hoisted(() => ({
  opts: null as unknown as ClientEngineOptions,
  engine: null as unknown as FakeEngine,
}));

type FakeEngine = ReturnType<typeof fakeEngine>;

vi.mock('q2-client', async (importOriginal) => {
  const real = await importOriginal<typeof import('q2-client')>();
  return {
    ...real,
    createClientEngine: vi.fn(async (opts: ClientEngineOptions) => {
      h.opts = opts;
      return h.engine;
    }),
  };
});
vi.mock('q2-render-gl', () => ({
  createGLRefresh: vi.fn(() => ({ shutdown: vi.fn(), appActivate: vi.fn() })),
}));
vi.mock('@/game/sound', () => ({ createSound: vi.fn(async () => ({ sound: null, enabled: false })) }));
vi.mock('@/lib/assets', async (importOriginal) => {
  const real = await importOriginal<typeof import('@/lib/assets')>();
  return {
    ...real,
    fetchAssetIndex: vi.fn(async () => ({ schema: 1, pakset: 'demo', paks: [], files: {}, maps: [] })),
    AssetLoader: class {
      onDownload: unknown = null;
      loadFile = async () => null;
    },
  };
});
vi.mock('@/lib/config', () => ({
  loadConfigText: vi.fn(async () => 'set sensitivity "5"'),
  readLocalConfig: vi.fn(() => 'set sensitivity "4"'),
  saveConfigText: vi.fn(async () => true),
  writeLocalConfig: vi.fn(),
  clearLocalConfig: vi.fn(),
}));

import { api, type BotInfo } from '@/lib/api';
import { saveConfigText } from '@/lib/config';
import { useAiStore } from '@/game/aiStore';
import { useGameStore } from '@/game/store';
import type { GameSource } from '@/game/session';

function fakeEngine() {
  const context = {
    cls: { state: 0, key_dest: 0 },
    cl: { frame: { valid: false, serverframe: 0 }, attractloop: false, predicted_origin: [0, 0, 0] },
    main: { demoplaying: false },
    keys: { keybindings: [] as string[] },
  };
  return {
    context,
    connect: vi.fn(),
    disconnect: vi.fn(),
    exec: vi.fn(),
    cvarGet: vi.fn((_n: string): string | undefined => undefined),
    cvarSet: vi.fn(),
    bind: vi.fn(),
    keyEvent: vi.fn(),
    keyboardEvent: vi.fn(() => true),
    mouseButton: vi.fn(),
    wheel: vi.fn(),
    mouseMove: vi.fn(),
    setPointerLocked: vi.fn(),
    setVidSize: vi.fn(),
    frame: vi.fn(),
    playDemo: vi.fn((_name: string, _data: Uint8Array) => {
      context.main.demoplaying = true;
    }),
    writeConfig: vi.fn(() => 'bind w "+forward"'),
    shutdown: vi.fn(() => ''),
  };
}

/** Event types a viewer's session must never listen to. */
const INPUT_EVENTS = [
  'keydown',
  'keyup',
  'keypress',
  'mousedown',
  'mouseup',
  'mousemove',
  'wheel',
  'click',
  'contextmenu',
  'pointerlockchange',
];

const g = globalThis as Record<string, unknown>;
let saved: Record<string, unknown>;
let listened: string[];
let rafs: (() => void)[];
let sockets: FakeSocket[];

class FakeSocket {
  binaryType = '';
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  constructor(readonly url: string) {
    sockets.push(this);
  }
  send() {}
  close() {}
}

function recordingTarget<T extends object>(extra: T): EventTarget & T {
  const t = Object.assign(new EventTarget(), extra);
  const add = t.addEventListener.bind(t);
  t.addEventListener = (
    type: string,
    l: EventListenerOrEventListenerObject | null,
    o?: AddEventListenerOptions | boolean,
  ) => {
    listened.push(type);
    add(type, l, o);
  };
  return t;
}

beforeEach(() => {
  saved = Object.fromEntries(
    [
      'window',
      'document',
      'location',
      'ResizeObserver',
      'requestAnimationFrame',
      'cancelAnimationFrame',
      'WebSocket',
      'fetch',
    ].map((k) => [k, g[k]]),
  );
  listened = [];
  rafs = [];
  sockets = [];
  g['window'] = recordingTarget({ innerWidth: 800, innerHeight: 600 });
  g['document'] = recordingTarget({ pointerLockElement: null as unknown, exitPointerLock: vi.fn() });
  g['location'] = { origin: 'http://q2.example', reload() {} };
  g['ResizeObserver'] = class {
    observe() {}
    disconnect() {}
  };
  g['requestAnimationFrame'] = (cb: () => void) => rafs.push(cb);
  g['cancelAnimationFrame'] = () => {};
  g['WebSocket'] = FakeSocket;
  h.engine = fakeEngine();
  h.opts = null as unknown as ClientEngineOptions;
  useGameStore.getState().reset();
  useAiStore.getState().reset();
  vi.mocked(saveConfigText).mockClear();
});

afterEach(() => {
  vi.restoreAllMocks();
  for (const [k, v] of Object.entries(saved)) {
    if (v === undefined) delete g[k];
    else g[k] = v;
  }
});

const bot = (status: BotInfo['status'], artifacts: BotInfo['artifacts'] = []): BotInfo => ({
  id: 'b1',
  name: 'bot one',
  status,
  backend: 'mock',
  maps: ['demo1', 'demo2'],
  skill: 1,
  public: true,
  startedAt: '2026-10-02T05:33:02Z',
  viewers: 0,
  level: { map: 'demo1', visit: 0 },
  artifacts,
});

const watchResponse = {
  ticket: 't1',
  wsUrl: '/ws/v1/bots/b1/watch',
  decisionsTicket: 'd1',
  decisionsUrl: '/ws/v1/bots/b1/decisions',
  pakset: 'demo',
};

/** A session with a recording canvas, not started yet. */
async function create(source: GameSource) {
  const { GameSession } = await import('@/game/session');
  const canvas = recordingTarget({
    clientWidth: 800,
    clientHeight: 600,
    width: 0,
    height: 0,
    focus() {},
    requestPointerLock: vi.fn(),
  });
  const s = new GameSession({ canvas: canvas as unknown as HTMLCanvasElement, source, onExit: () => {} });
  return { s, canvas, engine: h.engine };
}

async function start(source: GameSource) {
  const c = await create(source);
  await c.s.start();
  return c;
}

/** Runs the next requestAnimationFrame callback(s). */
function frames(n: number) {
  for (let i = 0; i < n; i++) rafs.at(-1)!();
}

function expectPassive(
  s: { lockPointer(): Promise<void>; openMenu(): void; resume(): void },
  canvas: { requestPointerLock: () => void },
) {
  expect(listened.filter((t) => INPUT_EVENTS.includes(t))).toEqual([]);
  expect(listened).toEqual(expect.arrayContaining(['resize', 'blur', 'focus', 'webglcontextlost']));
  void s.lockPointer();
  s.resume();
  s.openMenu();
  h.opts.host!.onMenu!('main');
  (g['document'] as EventTarget).dispatchEvent(new Event('pointerlockchange'));
  expect(canvas.requestPointerLock).not.toHaveBeenCalled();
  expect(useGameStore.getState().overlay).toBeNull();
}

describe('GameSession watch', () => {
  it('is passive, sets cl_predict 0 and connects to the bot with the watch tickets', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(bot('running'));
    const watch = vi.spyOn(api, 'watchBot').mockResolvedValue({ ...watchResponse });
    const { s, canvas, engine } = await start({ kind: 'watch', botId: 'b1' });

    expect(useGameStore.getState().fatal).toBeNull();
    expect(engine.connect).toHaveBeenCalledWith('bot-b1');
    expect(engine.cvarSet).toHaveBeenCalledWith('cl_predict', '0');
    expect(engine.cvarSet.mock.calls.map((c) => c[0])).not.toContain('name');
    // the player's settings are read (an anonymous viewer: the local copy), never written
    expect(h.opts.configText).toBe('set sensitivity "4"');
    expectPassive(s, canvas);

    // the decision feed uses its own ticket of the page's watch request
    expect(sockets.map((x) => x.url)).toEqual(['ws://q2.example/ws/v1/bots/b1/decisions?ticket=d1']);
    // the relay transport: the page ticket first, a fresh one for every later connection
    h.opts.transport('bot-b1');
    await vi.waitFor(() => expect(sockets).toHaveLength(2));
    expect(sockets[1]!.url).toBe('ws://q2.example/ws/v1/bots/b1/watch?ticket=t1');
    expect(watch).toHaveBeenCalledTimes(1);
    watch.mockResolvedValue({ ...watchResponse, ticket: 't2' });
    h.opts.transport('bot-b1');
    await vi.waitFor(() => expect(sockets).toHaveLength(3));
    expect(sockets[2]!.url).toContain('ticket=t2');
    expect(watch).toHaveBeenCalledTimes(2);

    const dbg = window.__q2web!;
    expect(dbg.mode).toBe('watch');
    expect(dbg.serverFrame()).toBe(0);
    engine.context.cls.state = 4;
    engine.context.cl.frame = { valid: true, serverframe: 321 };
    engine.context.cl.attractloop = true;
    expect(dbg.serverFrame()).toBe(321);
    expect(dbg.attractloop()).toBe(true);
    // the overlay follows the video every 6th frame
    frames(6);
    expect(useAiStore.getState().serverFrame).toBe(321);

    h.opts.host!.onConfig!('cvar', 'sensitivity', '9');
    s.dispose();
    expect(saveConfigText).not.toHaveBeenCalled();
    expect(engine.writeConfig).not.toHaveBeenCalled();
    expect(window.__q2web).toBeUndefined();
  });

  it('ends the view when the decision feed says bye', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(bot('running'));
    vi.spyOn(api, 'watchBot').mockResolvedValue({ ...watchResponse });
    const { s, engine } = await start({ kind: 'watch', botId: 'b1' });
    sockets[0]!.onmessage!({ data: '{"t":"bye","status":"finished"}' });
    expect(engine.disconnect).toHaveBeenCalled();
    expect(useGameStore.getState()).toMatchObject({ phase: 'ended', runStatus: 'finished' });
    s.dispose();
  });

  it('ends the view when the relay closes and the run is over', async () => {
    const getBot = vi.spyOn(api, 'getBot').mockResolvedValue(bot('running'));
    vi.spyOn(api, 'watchBot').mockResolvedValue({ ...watchResponse });
    const { s, engine } = await start({ kind: 'watch', botId: 'b1' });
    const t = h.opts.transport('bot-b1');
    t.onClose = () => {};
    await vi.waitFor(() => expect(sockets).toHaveLength(2));
    getBot.mockResolvedValue(bot('stopped'));
    sockets[1]!.onclose!({ code: 1000, reason: 'bot stopped' });
    await vi.waitFor(() => expect(useGameStore.getState().phase).toBe('ended'));
    expect(useGameStore.getState().runStatus).toBe('stopped');
    expect(engine.disconnect).toHaveBeenCalled();
    s.dispose();
  });

  it('reconnects with a fresh ticket when the relay drops a live run', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(bot('running'));
    vi.spyOn(api, 'watchBot').mockResolvedValue({ ...watchResponse });
    const { s, engine } = await start({ kind: 'watch', botId: 'b1' });
    vi.useFakeTimers();
    try {
      h.opts.host!.onState!(4);
      engine.context.cls.state = 1;
      h.opts.host!.onState!(1); // ca_disconnected: svc_disconnect from the relay, or a timeout
      await vi.advanceTimersByTimeAsync(999);
      expect(engine.connect).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(engine.connect).toHaveBeenCalledTimes(2);
      expect(engine.connect).toHaveBeenLastCalledWith('bot-b1');
      expect(useGameStore.getState().phase).not.toBe('ended');
    } finally {
      vi.useRealTimers();
    }
    s.dispose();
  });

  it('does not start an engine for a run that is over', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(bot('finished'));
    const watch = vi.spyOn(api, 'watchBot');
    const { s } = await start({ kind: 'watch', botId: 'b1' });
    expect(watch).not.toHaveBeenCalled();
    expect(h.opts).toBeNull();
    expect(useGameStore.getState()).toMatchObject({ phase: 'ended', runStatus: 'finished' });
    s.dispose();
  });
});

describe('GameSession replay', () => {
  const traceText = [
    { type: 'run_start', gms: 0, sf: 0, lvl: 0, map: '', backend: 'mock', maps: ['demo1', 'demo2'] },
    { type: 'level_start', gms: 1900, sf: 18, lvl: 0, map: 'demo1', visit: 0 },
    {
      type: 'decision',
      gms: 2400,
      sf: 24,
      lvl: 0,
      map: 'demo1',
      lane: 'tick',
      intent: { mode: 'fight', fields: [{ name: 'mode', value: 'fight', source: 'model' }] },
      tick: { mode: 'fight', step: 0 },
    },
    { type: 'level_start', gms: 9000, sf: 18, lvl: 1, map: 'demo2', visit: 0 },
  ]
    .map((e) => JSON.stringify({ v: 1, run: 'r1', ep: 0, seq: 1, wall: 0, ...e }))
    .join('\n');

  it('plays the demos in order without network, at the chosen speed, with the trace overlay', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(
      bot('finished', [
        { name: 'run.json', size: 10, kind: 'run' },
        { name: 'ep-000/demos/01-demo2.dm2', size: 2, kind: 'demo' },
        { name: 'ep-000/trace.jsonl.gz', size: 100, kind: 'trace' },
        { name: 'ep-000/demos/00-demo1.dm2', size: 1, kind: 'demo' },
      ]),
    );
    const demo = (name: string) => new Uint8Array(name.includes('00-demo1') ? [1] : [2, 2]);
    const fetchArtifact = vi
      .spyOn(api, 'fetchArtifact')
      .mockImplementation(async (_id, name) => new Response(demo(name)));
    g['fetch'] = vi.fn(async (url: string) => {
      expect(url).toBe('/api/v1/bots/b1/artifacts/ep-000/trace.jsonl.gz');
      return new Response(traceText);
    });
    const { s, canvas, engine } = await start({ kind: 'replay', botId: 'b1' });

    await vi.waitFor(() => expect(engine.playDemo).toHaveBeenCalledTimes(1));
    expect(engine.playDemo).toHaveBeenCalledWith('00-demo1', new Uint8Array([1]));
    expect(() => h.opts.transport('x')).toThrow(/no network/);
    expect(engine.connect).not.toHaveBeenCalled();
    expect(engine.cvarSet).toHaveBeenCalledWith('cl_predict', '0');
    expect(engine.cvarSet).toHaveBeenCalledWith('timescale', '1');
    expectPassive(s, canvas);
    const dbg = window.__q2web!;
    expect(dbg.mode).toBe('replay');
    expect(dbg.demoplaying()).toBe(true);
    expect(dbg.replayLevel()).toBe(0);
    expect(useGameStore.getState().replay).toMatchObject({ current: 0, speed: 1, finished: false });
    expect(useGameStore.getState().replay!.levels.map((l) => l.map)).toEqual(['demo1', 'demo2']);

    // the overlay shows the trace's decision of the frame on screen
    await vi.waitFor(() => expect(useAiStore.getState().status).toBe('replay'));
    engine.context.cls.state = 4;
    engine.context.cl.frame = { valid: true, serverframe: 30 };
    frames(6);
    expect(useAiStore.getState().latest).toMatchObject({
      sf: 24,
      mode: 'fight',
      provenance: { mode: 'model' },
    });

    s.setReplaySpeed(4);
    expect(engine.cvarSet).toHaveBeenLastCalledWith('timescale', '4');
    expect(useGameStore.getState().replay!.speed).toBe(4);
    s.setReplayPaused(true);
    const before = engine.frame.mock.calls.length;
    frames(3);
    expect(engine.frame.mock.calls.length).toBe(before);
    s.setReplayPaused(false);
    frames(1);
    expect(engine.frame).toHaveBeenLastCalledWith(16);

    // the demo finished: the next level attempt plays
    engine.context.main.demoplaying = false;
    frames(1);
    await vi.waitFor(() => expect(engine.playDemo).toHaveBeenCalledTimes(2));
    expect(engine.playDemo).toHaveBeenLastCalledWith('01-demo2', new Uint8Array([2, 2]));
    expect(useGameStore.getState().replay!.current).toBe(1);
    expect(useAiStore.getState().attempt).toBe(1);
    expect(fetchArtifact).toHaveBeenCalledTimes(2);

    // the last one finished: the replay is over
    engine.context.main.demoplaying = false;
    frames(1);
    expect(useGameStore.getState().phase).toBe('ended');
    expect(useGameStore.getState().replay!.finished).toBe(true);

    // picking a level again replays it (the bytes are cached)
    await s.playLevel(0);
    expect(engine.playDemo).toHaveBeenCalledTimes(3);
    expect(fetchArtifact).toHaveBeenCalledTimes(2);

    h.opts.host!.onConfig!('cvar', 'sensitivity', '9');
    s.dispose();
    expect(saveConfigText).not.toHaveBeenCalled();
  });

  it('starts nothing when disposed while fetching the run', async () => {
    let resolve!: (b: BotInfo) => void;
    vi.spyOn(api, 'getBot').mockReturnValue(new Promise<BotInfo>((r) => (resolve = r)));
    const fetchArtifact = vi.spyOn(api, 'fetchArtifact');
    const fetch = vi.fn(async () => new Response(traceText));
    g['fetch'] = fetch;
    const { s } = await create({ kind: 'replay', botId: 'b1' });
    const started = s.start();
    s.dispose(); // React strict mode: the effect's cleanup runs before the second mount
    resolve(
      bot('finished', [
        { name: 'ep-000/demos/00-demo1.dm2', size: 1, kind: 'demo' },
        { name: 'ep-000/trace.jsonl.gz', size: 100, kind: 'trace' },
      ]),
    );
    await started;
    await new Promise((r) => setTimeout(r, 0));
    expect(fetch).not.toHaveBeenCalled();
    expect(fetchArtifact).not.toHaveBeenCalled();
    expect(h.opts).toBeNull();
    expect(useAiStore.getState()).toMatchObject({ status: 'idle', attempt: null });
    expect(useGameStore.getState()).toMatchObject({ phase: 'index', mapName: '', replay: null });
  });

  it('retries a failed demo download when the level is picked again', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(
      bot('finished', [{ name: 'ep-000/demos/00-demo1.dm2', size: 1, kind: 'demo' }]),
    );
    const fetchArtifact = vi
      .spyOn(api, 'fetchArtifact')
      .mockRejectedValueOnce(new Error('HTTP 502'))
      .mockImplementation(async () => new Response(new Uint8Array([1])));
    g['fetch'] = vi.fn(async () => new Response(null, { status: 404 }));
    const { s, engine } = await start({ kind: 'replay', botId: 'b1' });

    await vi.waitFor(() =>
      expect(useGameStore.getState()).toMatchObject({
        phase: 'error',
        fatal: 'Cannot load ep-000/demos/00-demo1.dm2: HTTP 502',
      }),
    );
    expect(engine.playDemo).not.toHaveBeenCalled();

    await s.playLevel(0);
    expect(fetchArtifact).toHaveBeenCalledTimes(2);
    expect(engine.playDemo).toHaveBeenCalledWith('00-demo1', new Uint8Array([1]));
    expect(useGameStore.getState()).toMatchObject({ phase: 'loading', fatal: null });

    // a fatal error of the frame loop stays: nothing can be drawn any more
    engine.frame.mockImplementation(() => {
      throw new Error('renderer failed');
    });
    vi.spyOn(console, 'error').mockImplementation(() => {});
    frames(1);
    expect(useGameStore.getState()).toMatchObject({ phase: 'error', fatal: 'renderer failed' });
    await s.playLevel(0);
    expect(engine.playDemo).toHaveBeenCalledTimes(1);
    expect(useGameStore.getState()).toMatchObject({ phase: 'error', fatal: 'renderer failed' });
    s.dispose();
  });

  it('fails cleanly without demos', async () => {
    vi.spyOn(api, 'getBot').mockResolvedValue(bot('finished', [{ name: 'run.json', size: 1, kind: 'run' }]));
    const { s } = await start({ kind: 'replay', botId: 'b1' });
    expect(useGameStore.getState()).toMatchObject({
      phase: 'error',
      fatal: 'This run has no recorded demos to replay.',
    });
    s.dispose();
  });
});
