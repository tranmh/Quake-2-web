// Datagram transports (ADR-0001): one message == one original UDP datagram, including the 10-byte
// netchan header or the -1 out-of-band marker.

export interface DatagramTransport {
  /** Send one datagram. Datagrams sent before the transport is open are queued. */
  send(data: Uint8Array): void;
  /** Receives one datagram per call. */
  onMessage: ((data: Uint8Array) => void) | null;
  /** Called when the underlying connection closed or failed (reason for the console). */
  onClose: ((reason: string) => void) | null;
  close(): void;
}

/** Creates a transport for the `connect <address>` argument. */
export type TransportFactory = (address: string) => DatagramTransport;

/** Minimal WebSocket shape (browser WebSocket or a compatible implementation). */
export interface WebSocketLike {
  binaryType: string;
  readonly readyState: number;
  send(data: ArrayBufferLike | ArrayBufferView): void;
  close(code?: number, reason?: string): void;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
}

const WS_OPEN = 1;

/** WebSocket transport (binaryType arraybuffer). */
export class WebSocketTransport implements DatagramTransport {
  onMessage: ((data: Uint8Array) => void) | null = null;
  onClose: ((reason: string) => void) | null = null;
  private readonly queue: Uint8Array[] = [];
  private closed = false;

  constructor(readonly ws: WebSocketLike) {
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      for (const d of this.queue) ws.send(d);
      this.queue.length = 0;
    };
    ws.onmessage = (ev) => {
      const d = ev.data;
      if (d instanceof ArrayBuffer) this.onMessage?.(new Uint8Array(d));
      else if (ArrayBuffer.isView(d)) this.onMessage?.(new Uint8Array(d.buffer, d.byteOffset, d.byteLength));
    };
    ws.onclose = (ev) => {
      this.closed = true;
      this.onClose?.(ev.reason || `websocket closed (${ev.code ?? 0})`);
    };
    ws.onerror = () => {};
  }

  /** Opens a WebSocket to `url` (uses the global WebSocket constructor). */
  static connect(url: string): WebSocketTransport {
    const Ctor = (globalThis as unknown as { WebSocket: new (u: string) => WebSocketLike }).WebSocket;
    return new WebSocketTransport(new Ctor(url));
  }

  send(data: Uint8Array): void {
    if (this.closed) return;
    const copy = data.slice();
    if (this.ws.readyState === WS_OPEN) this.ws.send(copy);
    else this.queue.push(copy);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.queue.length = 0;
    try {
      this.ws.close();
    } catch {
      // ignore
    }
  }
}

/**
 * In-memory transport pair for tests: `a.send()` is delivered to `b.onMessage` (asynchronously via a
 * microtask unless `sync` is set) and vice versa.
 */
export class MemoryTransport implements DatagramTransport {
  onMessage: ((data: Uint8Array) => void) | null = null;
  onClose: ((reason: string) => void) | null = null;
  peer: MemoryTransport | null = null;
  closed = false;
  /** every datagram sent through this end (copies) */
  readonly sent: Uint8Array[] = [];

  constructor(readonly sync = false) {}

  static pair(sync = false): [MemoryTransport, MemoryTransport] {
    const a = new MemoryTransport(sync);
    const b = new MemoryTransport(sync);
    a.peer = b;
    b.peer = a;
    return [a, b];
  }

  send(data: Uint8Array): void {
    if (this.closed) return;
    const copy = data.slice();
    this.sent.push(copy);
    const p = this.peer;
    if (!p || p.closed) return;
    if (this.sync) p.onMessage?.(copy);
    else queueMicrotask(() => p.onMessage?.(copy));
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    const p = this.peer;
    if (p && !p.closed) p.onClose?.('peer closed');
  }
}
