// Transport factory for `connect`: the first connection uses the ticket of the join request made by the
// page; any later reconnect (the WebSocket closed, CL_CheckForResend opens a new transport) asks the API
// for a fresh one-time ticket first. Datagrams sent while the ticket/socket is pending are queued.
import { WebSocketTransport, type DatagramTransport, type TransportFactory } from 'q2-client';
import { api, errorMessage, type JoinResponse } from '@/lib/api';
import { resolveWsUrl } from '@/lib/env';

class TicketedTransport implements DatagramTransport {
  onMessage: ((data: Uint8Array) => void) | null = null;
  onClose: ((reason: string) => void) | null = null;
  private inner: WebSocketTransport | null = null;
  private readonly queue: Uint8Array[] = [];
  private closed = false;

  constructor(url: Promise<string>) {
    url.then(
      (u) => {
        if (this.closed) return;
        let t: WebSocketTransport;
        try {
          t = WebSocketTransport.connect(u);
        } catch (e) {
          // e.g. SecurityError for ws:// from an https page: report it instead of "connecting" forever
          this.closed = true;
          this.onClose?.(`cannot open websocket: ${errorMessage(e)}`);
          return;
        }
        t.onMessage = (d) => this.onMessage?.(d);
        t.onClose = (r) => {
          if (this.closed) return;
          this.closed = true;
          this.onClose?.(r);
        };
        for (const d of this.queue) t.send(d);
        this.queue.length = 0;
        this.inner = t;
      },
      (e) => {
        if (this.closed) return;
        this.closed = true;
        this.onClose?.(`join failed: ${errorMessage(e)}`);
      },
    );
  }

  send(data: Uint8Array): void {
    if (this.closed) return;
    if (this.inner) this.inner.send(data);
    else this.queue.push(data.slice());
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.queue.length = 0;
    this.inner?.close();
  }
}

export function createJoinTransportFactory(gameId: string, first: JoinResponse, origin: string): TransportFactory {
  let initial: JoinResponse | null = first;
  return () => {
    const j = initial;
    initial = null;
    const url = j ? Promise.resolve(j) : api.joinGame(gameId);
    return new TicketedTransport(url.then((r) => resolveWsUrl(r.wsUrl, r.ticket, origin)));
  };
}
