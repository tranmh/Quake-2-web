// Transport factory for watching a bot (WS /ws/v1/bots/{id}/watch, raw netchan datagrams like a game,
// served by the spectate relay). Like createJoinTransportFactory: the first connection uses the ticket of
// the page's watch request, every later one (CL_CheckForResend, or GameSession reconnecting after the
// socket dropped) asks POST /bots/{id}/watch for a fresh one-time ticket first.
import type { DatagramTransport, TransportFactory } from 'q2-client';
import { api, type BotWatchResponse } from '@/lib/api';
import { resolveWsUrl } from '@/lib/env';
import { TicketedTransport } from './transport';

export interface WatchTransportOptions {
  /**
   * The relay connection closed or could not be opened (not called when the engine closes it). The engine
   * itself only notices a dead connection when the netchan times out; the session uses this to tell a
   * finished run from a dropped socket.
   */
  onClose?: (reason: string) => void;
}

/** Forwards to the ticketed transport and reports its close to the session as well as the engine. */
class ObservedTransport implements DatagramTransport {
  onMessage: ((data: Uint8Array) => void) | null = null;
  onClose: ((reason: string) => void) | null = null;

  constructor(
    private readonly inner: DatagramTransport,
    onClosed: (reason: string) => void,
  ) {
    inner.onMessage = (d) => this.onMessage?.(d);
    inner.onClose = (r) => {
      this.onClose?.(r);
      onClosed(r);
    };
  }

  send(data: Uint8Array): void {
    this.inner.send(data);
  }

  close(): void {
    this.inner.close();
  }
}

export function createWatchTransportFactory(
  botId: string,
  first: BotWatchResponse | null,
  origin: string,
  opts: WatchTransportOptions = {},
): TransportFactory {
  let initial = first;
  return () => {
    const w = initial;
    initial = null;
    const res = w ? Promise.resolve(w) : api.watchBot(botId);
    const t = new TicketedTransport(
      res.then((r) => resolveWsUrl(r.wsUrl, r.ticket, origin)),
      'watch',
    );
    return opts.onClose ? new ObservedTransport(t, opts.onClose) : t;
  };
}
