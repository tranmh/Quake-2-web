// The bot endpoints of the API client: methods, paths, bodies, the nil-slice normalization, artifact URLs
// and the error envelope (403 / 429 / 503 / 409).
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, api, artifactUrl, botIsLive, type BotInfo } from '@/lib/api';

const g = globalThis as Record<string, unknown>;
const savedFetch = g['fetch'];
afterEach(() => {
  g['fetch'] = savedFetch;
});

interface Call {
  url: string;
  method: string;
  body: unknown;
}

function mockFetch(status: number, body: unknown): Call[] {
  const calls: Call[] = [];
  g['fetch'] = vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({
      url,
      method: init?.method ?? 'GET',
      body: init?.body ? JSON.parse(init.body as string) : undefined,
    });
    if (status === 204) return new Response(null, { status });
    return new Response(typeof body === 'string' ? body : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  return calls;
}

const bot: BotInfo = {
  id: 'b1',
  name: 'demo bot',
  ownerId: 7,
  status: 'running',
  backend: 'scripted',
  maps: ['demo1'],
  skill: 1,
  public: false,
  startedAt: '2026-10-02T05:33:02Z',
  viewers: 0,
  artifacts: [],
};

describe('bot API client', () => {
  it('starts a bot with the spec as the body', async () => {
    const calls = mockFetch(201, { bot });
    const b = await api.createBot({ backend: 'scripted', maps: ['demo1'], skill: 2, public: true });
    expect(b.id).toBe('b1');
    expect(calls).toEqual([
      {
        url: '/api/v1/bots',
        method: 'POST',
        body: { backend: 'scripted', maps: ['demo1'], skill: 2, public: true },
      },
    ]);
  });

  it('lists bots and turns Go nil slices into arrays', async () => {
    mockFetch(200, { bots: [{ ...bot, maps: null, artifacts: null }] });
    const list = await api.listBots();
    expect(list[0]!.maps).toEqual([]);
    expect(list[0]!.artifacts).toEqual([]);
    mockFetch(200, { bots: null });
    expect(await api.listBots()).toEqual([]);
  });

  it('gets, stops and watches a bot by its escaped id', async () => {
    let calls = mockFetch(200, { bot: { ...bot, id: 'a/b' } });
    await api.getBot('a/b');
    expect(calls[0]).toMatchObject({ url: '/api/v1/bots/a%2Fb', method: 'GET' });
    calls = mockFetch(204, null);
    await expect(api.stopBot('b1')).resolves.toBeUndefined();
    expect(calls[0]).toMatchObject({ url: '/api/v1/bots/b1', method: 'DELETE' });
    const w = {
      ticket: 't',
      wsUrl: '/ws/v1/bots/b1/watch',
      decisionsTicket: 'd',
      decisionsUrl: '/ws/v1/bots/b1/decisions',
      pakset: 'demo',
    };
    calls = mockFetch(200, w);
    expect(await api.watchBot('b1')).toEqual(w);
    expect(calls[0]).toMatchObject({ url: '/api/v1/bots/b1/watch', method: 'POST' });
  });

  it.each([
    [403, 'forbidden', 'jev bots need an admin'],
    [429, 'too_many_bots', 'limit of 2 bots reached'],
    [503, 'bots_disabled', 'bots are not enabled on this server'],
    [400, 'invalid_bot', 'skill must be 0..3'],
  ])('rejects with the error envelope (%i)', async (status, code, message) => {
    mockFetch(status, { error: { code, message } });
    const err = await api.createBot({ backend: 'jev' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status, code, message });
  });

  it('reports a watch of a bot that is not live as 409', async () => {
    mockFetch(409, { error: { code: 'not_live', message: 'bot is not running' } });
    await expect(api.watchBot('b1')).rejects.toMatchObject({ status: 409, code: 'not_live' });
  });

  it('builds artifact URLs segment by segment and refuses path tricks', () => {
    expect(artifactUrl('b1', 'run.json')).toBe('/api/v1/bots/b1/artifacts/run.json');
    expect(api.artifactUrl('b 1', 'ep-000/demos/00-demo1.dm2')).toBe(
      '/api/v1/bots/b%201/artifacts/ep-000/demos/00-demo1.dm2',
    );
    for (const bad of ['', '../games', 'ep-000/../../x', 'ep-000//trace.jsonl.gz', './run.json']) {
      expect(() => artifactUrl('b1', bad)).toThrow(ApiError);
    }
  });

  it('fetches an artifact and maps failures to ApiError', async () => {
    g['fetch'] = vi.fn(async () => new Response(new Uint8Array([1, 2, 3]), { status: 200 }));
    const res = await api.fetchArtifact('b1', 'ep-000/demos/00-demo1.dm2');
    expect(new Uint8Array(await res.arrayBuffer())).toEqual(new Uint8Array([1, 2, 3]));
    mockFetch(404, { error: { code: 'not_found', message: 'no such artifact' } });
    await expect(api.fetchArtifact('b1', 'run.json')).rejects.toMatchObject({
      status: 404,
      code: 'not_found',
    });
  });

  it('knows which statuses are live', () => {
    expect(['starting', 'running'].every(botIsLive)).toBe(true);
    expect(['finished', 'failed', 'stopped'].some(botIsLive)).toBe(false);
  });
});
