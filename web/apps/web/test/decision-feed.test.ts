// Decision feed (q2bot.decisions/1): message parsing, the store updates, gaps, and the reconnect policy
// (backoff, a fresh ticket per reconnect, stop on bye / not live / denied).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  DecisionFeed,
  decisionView,
  parseFeedMessage,
  type FeedSocket,
  type FeedTicket,
} from '@/game/decisionFeed';
import { useAiStore } from '@/game/aiStore';
import { ApiError, type FeedDecision } from '@/lib/api';

const DECISION = {
  t: 'decision',
  sf: 1234,
  lvl: 2,
  map: 'demo1',
  mode: 'fight',
  objective: 'press button *34',
  target: { id: 'e7', class: 'soldier_shotgun', dist: 410 },
  questions: [
    {
      id: 'target',
      options: [
        { label: 'e7', p: 0.62 },
        { label: 'none', p: 0.38 },
      ],
      chosen: 'e7',
      confidence: 0.71,
    },
  ],
  provenance: { mode: 'model', target: 'model', fire_policy: 'scripted' },
  action: { movement: 'strafe_left', firePolicy: 'fire_when_aligned', weapon: 'shotgun', fire: true },
  latencyMs: 183,
  costUsd: 0.00004,
};

describe('parseFeedMessage', () => {
  it('parses every message type of the protocol', () => {
    expect(
      parseFeedMessage('{"t":"hello","bot":"b1","backend":"jev","model":"jev-1.13","maps":["demo1",3]}'),
    ).toEqual({
      t: 'hello',
      bot: 'b1',
      backend: 'jev',
      model: 'jev-1.13',
      maps: ['demo1'],
    });
    const d = parseFeedMessage(JSON.stringify(DECISION)) as FeedDecision;
    expect(d).toMatchObject({ ...DECISION, fallback: {} });
    expect(parseFeedMessage('{"t":"event","kind":"death","sf":9,"data":{"cause":"hit"}}')).toEqual({
      t: 'event',
      kind: 'death',
      sf: 9,
      data: { cause: 'hit' },
    });
    expect(
      parseFeedMessage(
        '{"t":"stats","kills":3,"deaths":1,"decisions":40,"decisionRate":9.5,"modelShare":0.8,"staleRate":0.01,"apiP50Ms":190,"costUsd":0.002}',
      ),
    ).toEqual({
      t: 'stats',
      kills: 3,
      deaths: 1,
      decisions: 40,
      decisionRate: 9.5,
      modelShare: 0.8,
      staleRate: 0.01,
      apiP50Ms: 190,
      costUsd: 0.002,
    });
    expect(parseFeedMessage('{"t":"gap","dropped":12}')).toEqual({ t: 'gap', dropped: 12 });
    expect(parseFeedMessage('{"t":"bye","status":"stopped"}')).toEqual({ t: 'bye', status: 'stopped' });
  });

  it('normalizes partial or odd decisions instead of failing', () => {
    const d = parseFeedMessage(
      '{"t":"decision","sf":"x","questions":[{"id":"mode","options":{"fight":0.9,"objective":1.7},"chosen":"fight"},{"nope":1}],"target":{"id":""},"provenance":{"mode":"model","bad":3}}',
    ) as FeedDecision;
    expect(d.sf).toBe(0);
    expect(d.target).toBeNull();
    expect(d.questions).toEqual([
      {
        id: 'mode',
        chosen: 'fight',
        options: [
          { label: 'fight', p: 0.9 },
          { label: 'objective', p: 1 },
        ],
      },
    ]);
    expect(d.provenance).toEqual({ mode: 'model' });
    expect(decisionView(d)).toMatchObject({ objective: '', latencyMs: null, costUsd: null, action: {} });
  });

  it('rejects what is not a protocol message', () => {
    for (const bad of ['', 'not json', '[]', '{"t":"nope"}', '{"t":"event"}', '42'])
      expect(parseFeedMessage(bad)).toBeNull();
    expect(parseFeedMessage(new ArrayBuffer(4))).toBeNull();
  });
});

class FakeSocket implements FeedSocket {
  static all: FakeSocket[] = [];
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  closed = false;
  constructor(readonly url: string) {
    FakeSocket.all.push(this);
  }
  close() {
    this.closed = true;
  }
  open() {
    this.onopen?.({});
  }
  msg(m: unknown) {
    this.onmessage?.({ data: typeof m === 'string' ? m : JSON.stringify(m) });
  }
  drop(code = 1006) {
    this.onclose?.({ code });
  }
}

function feed(
  ticket: () => Promise<FeedTicket>,
  first: FeedTicket | null = { url: '/ws/v1/bots/b1/decisions', ticket: 'd1' },
) {
  const onEnded = vi.fn();
  const f = new DecisionFeed({
    botId: 'b1',
    origin: 'http://q2.example',
    first,
    ticket,
    WebSocket: FakeSocket,
    minBackoffMs: 100,
    maxBackoffMs: 1000,
    jitter: 0,
    onEnded,
  });
  return { f, onEnded };
}

const flush = async () => {
  for (let i = 0; i < 5; i++) await Promise.resolve();
};

describe('DecisionFeed', () => {
  beforeEach(() => {
    FakeSocket.all = [];
    useAiStore.getState().reset();
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('fills the store from the messages', async () => {
    const { f } = feed(async () => ({ url: '/ws/v1/bots/b1/decisions', ticket: 'unused' }));
    f.start();
    await flush();
    const ws = FakeSocket.all[0]!;
    expect(ws.url).toBe('ws://q2.example/ws/v1/bots/b1/decisions?ticket=d1');
    expect(useAiStore.getState().status).toBe('connecting');
    ws.open();
    ws.msg({ t: 'hello', bot: 'b1', backend: 'mock', maps: ['demo1'] });
    ws.msg(DECISION);
    ws.msg({ t: 'event', kind: 'kill', sf: 1240, data: { class: 'soldier' } });
    ws.msg({
      t: 'stats',
      kills: 1,
      deaths: 0,
      decisions: 12,
      decisionRate: 9,
      modelShare: 0.7,
      staleRate: 0,
      apiP50Ms: 200,
      costUsd: 0.001,
    });
    ws.msg({ t: 'gap', dropped: 4 });
    ws.msg('garbage');
    const s = useAiStore.getState();
    expect(s.status).toBe('live');
    expect(s.hello?.backend).toBe('mock');
    expect(s.latest).toMatchObject({ sf: 1234, lvl: 2, mode: 'fight', target: { id: 'e7' }, latencyMs: 183 });
    expect(s.events[0]).toEqual({ kind: 'kill', sf: 1240, data: { class: 'soldier' } });
    expect(s.totals).toMatchObject({ kills: 1, decisions: 12, modelShare: 0.7 });
    expect(s.dropped).toBe(4);
    f.close();
    expect(ws.closed).toBe(true);
  });

  it('reconnects with backoff and a fresh ticket each time', async () => {
    let n = 1;
    const ticket = vi.fn(async () => ({ url: '/ws/v1/bots/b1/decisions', ticket: `d${++n}` }));
    const { f } = feed(ticket);
    f.start();
    await flush();
    FakeSocket.all[0]!.drop();
    expect(useAiStore.getState().status).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(99);
    expect(ticket).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await flush();
    expect(ticket).toHaveBeenCalledTimes(1);
    expect(FakeSocket.all[1]!.url).toContain('ticket=d2');

    // the second failure waits twice as long
    FakeSocket.all[1]!.drop();
    await vi.advanceTimersByTimeAsync(199);
    expect(FakeSocket.all).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    await flush();
    expect(FakeSocket.all[2]!.url).toContain('ticket=d3');

    // a working connection (hello) resets the backoff
    FakeSocket.all[2]!.msg({ t: 'hello', bot: 'b1', backend: 'scripted', maps: [] });
    FakeSocket.all[2]!.drop();
    await vi.advanceTimersByTimeAsync(100);
    await flush();
    expect(FakeSocket.all).toHaveLength(4);
    f.close();
  });

  it('retries a failed ticket request, and stops when the bot is not live', async () => {
    const ticket = vi
      .fn<() => Promise<FeedTicket>>()
      .mockRejectedValueOnce(new ApiError(0, 'network', 'cannot reach the game server'))
      .mockRejectedValueOnce(new ApiError(409, 'not_live', 'bot is not running'));
    const { f, onEnded } = feed(ticket, null);
    f.start();
    await flush();
    expect(useAiStore.getState().status).toBe('reconnecting');
    await vi.advanceTimersByTimeAsync(100);
    await flush();
    expect(ticket).toHaveBeenCalledTimes(2);
    expect(onEnded).toHaveBeenCalledWith('ended');
    expect(useAiStore.getState().status).toBe('ended');
    await vi.advanceTimersByTimeAsync(5000);
    expect(ticket).toHaveBeenCalledTimes(2);
    expect(FakeSocket.all).toHaveLength(0);
    expect(f.active).toBe(false);
  });

  it('stops for good on bye', async () => {
    const ticket = vi.fn(async () => ({ url: '/x', ticket: 'x' }));
    const { f, onEnded } = feed(ticket);
    f.start();
    await flush();
    const ws = FakeSocket.all[0]!;
    ws.msg({ t: 'bye', status: 'finished' });
    expect(ws.closed).toBe(true);
    expect(onEnded).toHaveBeenCalledWith('finished');
    expect(useAiStore.getState()).toMatchObject({ status: 'ended', ended: 'finished' });
    ws.drop(1000);
    await vi.advanceTimersByTimeAsync(5000);
    expect(ticket).not.toHaveBeenCalled();
  });

  it('gives up when the account may not watch', async () => {
    const ticket = vi.fn(async (): Promise<FeedTicket> => {
      throw new ApiError(403, 'forbidden', 'not your bot');
    });
    const { f } = feed(ticket, null);
    f.start();
    await flush();
    expect(useAiStore.getState()).toMatchObject({ status: 'error', error: 'not your bot' });
    await vi.advanceTimersByTimeAsync(5000);
    expect(ticket).toHaveBeenCalledTimes(1);
  });
});
