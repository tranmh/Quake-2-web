import { describe, expect, it } from 'vitest';
import { MemoryTransport, WebSocketTransport, type WebSocketLike } from '../src/transport';

class FakeWS implements WebSocketLike {
  binaryType = 'blob';
  readyState = 0;
  sent: Uint8Array[] = [];
  closed = false;
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  send(data: ArrayBufferLike | ArrayBufferView): void {
    this.sent.push(new Uint8Array(data as ArrayBuffer));
  }
  close(): void {
    this.closed = true;
  }
}

describe('WebSocketTransport', () => {
  it('uses arraybuffer frames, queues until open, one message per datagram', () => {
    const ws = new FakeWS();
    const t = new WebSocketTransport(ws);
    expect(ws.binaryType).toBe('arraybuffer');
    const got: number[][] = [];
    t.onMessage = (d) => got.push(Array.from(d));
    t.send(new Uint8Array([0xff, 0xff, 0xff, 0xff, 1]));
    expect(ws.sent.length).toBe(0);
    ws.readyState = 1;
    ws.onopen!({});
    expect(ws.sent.map((d) => Array.from(d))).toEqual([[0xff, 0xff, 0xff, 0xff, 1]]);
    t.send(new Uint8Array([1, 2, 3]));
    expect(ws.sent.length).toBe(2);
    ws.onmessage!({ data: new Uint8Array([9, 8]).buffer });
    expect(got).toEqual([[9, 8]]);
    let reason = '';
    t.onClose = (r) => (reason = r);
    ws.onclose!({ code: 1006, reason: '' });
    expect(reason).toContain('1006');
    t.send(new Uint8Array([4]));
    expect(ws.sent.length).toBe(2);
  });
});

describe('MemoryTransport', () => {
  it('delivers to the peer (sync and async) and signals close', async () => {
    const [a, b] = MemoryTransport.pair(true);
    const got: number[] = [];
    b.onMessage = (d) => got.push(d[0]!);
    a.send(new Uint8Array([5]));
    expect(got).toEqual([5]);
    const [x, y] = MemoryTransport.pair();
    const got2: number[] = [];
    y.onMessage = (d) => got2.push(d[0]!);
    x.send(new Uint8Array([7]));
    expect(got2).toEqual([]);
    await Promise.resolve();
    expect(got2).toEqual([7]);
    let closed = '';
    y.onClose = (r) => (closed = r);
    x.close();
    expect(closed).toBe('peer closed');
  });
});
