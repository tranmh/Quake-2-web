// Watch transport: the first relay connection uses the page's one-time ticket; every reconnect asks
// POST /bots/{id}/watch for a fresh one. A refused ticket closes the transport with the reason, and the
// session hears about a closed relay connection (but not about the engine closing it).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { BotWatchResponse } from '@/lib/api';
import { createWatchTransportFactory } from '@/game/watch';

const g = globalThis as Record<string, unknown>;
let saved: Record<string, unknown>;
let sockets: FakeSocket[];

class FakeSocket {
  binaryType = '';
  readyState = 0;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  sent: Uint8Array[] = [];
  closed = false;
  constructor(readonly url: string) {
    sockets.push(this);
  }
  send(d: Uint8Array) {
    this.sent.push(d);
  }
  close() {
    this.closed = true;
  }
}

const first: BotWatchResponse = {
  ticket: 't1',
  wsUrl: '/ws/v1/bots/b1/watch',
  decisionsTicket: 'd1',
  decisionsUrl: '/ws/v1/bots/b1/decisions',
  pakset: 'demo',
};

beforeEach(() => {
  saved = { WebSocket: g['WebSocket'], fetch: g['fetch'] };
  sockets = [];
  g['WebSocket'] = FakeSocket;
});
afterEach(() => {
  for (const [k, v] of Object.entries(saved)) g[k] = v;
});

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('createWatchTransportFactory', () => {
  it('uses the page ticket first and a fresh ticket for every reconnect', async () => {
    let n = 1;
    const fetchMock = vi.fn(async () => {
      n++;
      return new Response(JSON.stringify({ ...first, ticket: `t${n}`, decisionsTicket: `d${n}` }), {
        status: 200,
      });
    });
    g['fetch'] = fetchMock;
    const factory = createWatchTransportFactory('b1', first, 'http://q2.example');

    const a = factory('bot-b1');
    a.send(new Uint8Array([0xff, 0xff, 0xff, 0xff])); // queued until the socket exists
    await tick();
    expect(fetchMock).not.toHaveBeenCalled();
    expect(sockets.map((s) => s.url)).toEqual(['ws://q2.example/ws/v1/bots/b1/watch?ticket=t1']);
    sockets[0]!.readyState = 1;
    sockets[0]!.onopen?.({});
    expect(sockets[0]!.sent).toEqual([new Uint8Array([0xff, 0xff, 0xff, 0xff])]);

    factory('bot-b1');
    await vi.waitFor(() => expect(sockets).toHaveLength(2));
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/v1/bots/b1/watch');
    expect(init.method).toBe('POST');
    expect(sockets[1]!.url).toBe('ws://q2.example/ws/v1/bots/b1/watch?ticket=t2');

    factory('bot-b1');
    await vi.waitFor(() => expect(sockets).toHaveLength(3));
    expect(sockets[2]!.url).toContain('ticket=t3');
  });

  it('closes with the reason when the bot is no longer watchable', async () => {
    g['fetch'] = vi.fn(
      async () =>
        new Response(JSON.stringify({ error: { code: 'not_live', message: 'bot is not running' } }), {
          status: 409,
        }),
    );
    const sessionHeard: string[] = [];
    const factory = createWatchTransportFactory('b1', null, 'http://q2.example', {
      onClose: (r) => sessionHeard.push(r),
    });
    const t = factory('bot-b1');
    const engineHeard = await new Promise<string>((resolve) => {
      t.onClose = resolve;
    });
    expect(engineHeard).toBe('watch failed: bot is not running');
    expect(sessionHeard).toEqual([engineHeard]);
    expect(sockets).toHaveLength(0);
  });

  it('reports a dropped relay socket to the session, not the engine closing it', async () => {
    const sessionHeard: string[] = [];
    const factory = createWatchTransportFactory('b1', first, 'http://q2.example', {
      onClose: (r) => sessionHeard.push(r),
    });
    const t = factory('bot-b1');
    const got: Uint8Array[] = [];
    t.onMessage = (d) => got.push(d);
    await tick();
    const ws = sockets[0]!;
    ws.onmessage?.({ data: new Uint8Array([1, 2, 3]).buffer });
    expect(got).toEqual([new Uint8Array([1, 2, 3])]);
    ws.onclose?.({ code: 1006 });
    expect(sessionHeard).toEqual(['websocket closed (1006)']);

    const t2 = createWatchTransportFactory('b1', first, 'http://q2.example', {
      onClose: (r) => sessionHeard.push(r),
    })('x');
    await tick();
    t2.close();
    expect(sockets[1]!.closed).toBe(true);
    expect(sessionHeard).toHaveLength(1);
  });
});
