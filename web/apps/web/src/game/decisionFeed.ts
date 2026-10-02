// Live decision feed of a watched bot: WS /ws/v1/bots/{id}/decisions (protocol q2bot.decisions/1, one
// JSON message per text frame) into the AI store. Tickets are one-time: the first connection uses the
// one of the page's watch request, every reconnect asks POST /bots/{id}/watch for a fresh one, with
// exponential backoff. The feed stops on `bye`, when the bot is not live any more (404/409/410) and when
// the account may not watch it (401/403).
import {
  ApiError,
  api,
  errorMessage,
  type DecisionAction,
  type DecisionOption,
  type DecisionQuestion,
  type DecisionTarget,
  type FeedDecision,
  type FeedEventKind,
  type FeedMessage,
} from '@/lib/api';
import { resolveWsUrl } from '@/lib/env';
import { useAiStore, type AiState, type DecisionView } from './aiStore';

// ---------------------------------------------------------------- parsing

type Obj = Record<string, unknown>;

function isObj(v: unknown): v is Obj {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function num(v: unknown, def = 0): number {
  return typeof v === 'number' && Number.isFinite(v) ? v : def;
}

function optNum(v: unknown): number | undefined {
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined;
}

function str(v: unknown, def = ''): string {
  return typeof v === 'string' ? v : def;
}

function strRecord(v: unknown): Record<string, string> {
  const out: Record<string, string> = {};
  if (!isObj(v)) return out;
  for (const [k, x] of Object.entries(v)) if (typeof x === 'string') out[k] = x;
  return out;
}

function clamp01(p: number): number {
  return p < 0 ? 0 : p > 1 ? 1 : p;
}

function parseOptions(v: unknown): DecisionOption[] {
  if (Array.isArray(v)) {
    return v.filter(isObj).map((o) => ({ label: str(o['label']), p: clamp01(num(o['p'])) }));
  }
  // tolerate the backend's {label: p} map
  if (isObj(v)) return Object.entries(v).map(([label, p]) => ({ label, p: clamp01(num(p)) }));
  return [];
}

function parseQuestion(v: unknown): DecisionQuestion | null {
  if (!isObj(v) || typeof v['id'] !== 'string') return null;
  const q: DecisionQuestion = { id: v['id'], options: parseOptions(v['options']), chosen: str(v['chosen']) };
  const c = optNum(v['confidence']);
  if (c !== undefined) q.confidence = clamp01(c);
  return q;
}

function parseTarget(v: unknown): DecisionTarget | null {
  if (!isObj(v) || typeof v['id'] !== 'string' || !v['id']) return null;
  return { id: v['id'], class: str(v['class']), dist: num(v['dist']) };
}

function parseAction(v: unknown): DecisionAction {
  const a: DecisionAction = {};
  if (!isObj(v)) return a;
  if (typeof v['movement'] === 'string') a.movement = v['movement'];
  if (typeof v['firePolicy'] === 'string') a.firePolicy = v['firePolicy'];
  if (typeof v['weapon'] === 'string') a.weapon = v['weapon'];
  if (typeof v['fire'] === 'boolean') a.fire = v['fire'];
  return a;
}

/** Parses one text frame; null for anything that is not a well-formed protocol message. */
export function parseFeedMessage(text: unknown): FeedMessage | null {
  if (typeof text !== 'string') return null;
  let o: unknown;
  try {
    o = JSON.parse(text);
  } catch {
    return null;
  }
  if (!isObj(o)) return null;
  switch (o['t']) {
    case 'hello': {
      const h: FeedMessage = {
        t: 'hello',
        bot: str(o['bot']),
        backend: str(o['backend']),
        maps: Array.isArray(o['maps']) ? o['maps'].filter((m): m is string => typeof m === 'string') : [],
      };
      if (typeof o['model'] === 'string' && o['model']) h.model = o['model'];
      return h;
    }
    case 'decision': {
      const d: FeedDecision = {
        t: 'decision',
        sf: num(o['sf']),
        lvl: num(o['lvl']),
        map: str(o['map']),
        mode: str(o['mode']),
        objective: str(o['objective']),
        target: parseTarget(o['target']),
        questions: Array.isArray(o['questions'])
          ? o['questions'].map(parseQuestion).filter((q): q is DecisionQuestion => q !== null)
          : [],
        provenance: strRecord(o['provenance']),
        fallback: strRecord(o['fallback']),
        action: parseAction(o['action']),
      };
      const lat = optNum(o['latencyMs']);
      if (lat !== undefined) d.latencyMs = lat;
      const cost = optNum(o['costUsd']);
      if (cost !== undefined) d.costUsd = cost;
      return d;
    }
    case 'event': {
      if (typeof o['kind'] !== 'string') return null;
      const ev: FeedMessage = {
        t: 'event',
        // unknown kinds pass through: the ticker shows them as they come
        kind: o['kind'] as FeedEventKind,
        sf: num(o['sf']),
        data: isObj(o['data']) ? o['data'] : {},
      };
      if (typeof o['lvl'] === 'number') ev.lvl = o['lvl'];
      if (typeof o['map'] === 'string') ev.map = o['map'];
      return ev;
    }
    case 'stats':
      return {
        t: 'stats',
        kills: num(o['kills']),
        deaths: num(o['deaths']),
        decisions: num(o['decisions']),
        decisionRate: num(o['decisionRate']),
        modelShare: num(o['modelShare']),
        staleRate: num(o['staleRate']),
        apiP50Ms: num(o['apiP50Ms']),
        costUsd: num(o['costUsd']),
      };
    case 'gap':
      return { t: 'gap', dropped: Math.max(0, num(o['dropped'])) };
    case 'bye':
      return { t: 'bye', status: str(o['status'], 'finished') };
    default:
      return null;
  }
}

/** A parsed `decision` message as the store keeps it. */
export function decisionView(m: FeedDecision): DecisionView {
  return {
    sf: m.sf,
    lvl: m.lvl,
    map: m.map,
    mode: m.mode,
    objective: m.objective ?? '',
    target: m.target,
    questions: m.questions,
    provenance: m.provenance,
    fallback: m.fallback ?? {},
    action: m.action,
    latencyMs: m.latencyMs ?? null,
    costUsd: m.costUsd ?? null,
  };
}

// ---------------------------------------------------------------- connection

/** Minimal WebSocket shape (the browser's, or a test double). */
export interface FeedSocket {
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code?: number; reason?: string }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  close(code?: number, reason?: string): void;
}

export type FeedSocketCtor = new (url: string) => FeedSocket;

export interface FeedTicket {
  url: string;
  ticket: string;
}

export interface DecisionFeedOptions {
  botId: string;
  /** page origin the relative decisionsUrl resolves against */
  origin: string;
  /** the decisions ticket of the page's watch request (used for the first connection only) */
  first?: FeedTicket | null;
  /** a fresh ticket (default: POST /bots/{id}/watch) */
  ticket?: () => Promise<FeedTicket>;
  store?: { getState(): AiState };
  WebSocket?: FeedSocketCtor;
  /** reconnect backoff: first delay, doubling up to maxBackoffMs, ±jitter */
  minBackoffMs?: number;
  maxBackoffMs?: number;
  jitter?: number;
  /** the run ended (bye, or the bot is not live any more) */
  onEnded?: (status: string) => void;
}

/** HTTP statuses of the watch request after which reconnecting is pointless. */
const GONE = new Set([404, 409, 410]);
const DENIED = new Set([401, 403]);

export class DecisionFeed {
  private ws: FeedSocket | null = null;
  private first: FeedTicket | null;
  private closed = false;
  private failures = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private readonly store: { getState(): AiState };

  constructor(private readonly o: DecisionFeedOptions) {
    this.first = o.first ?? null;
    this.store = o.store ?? useAiStore;
  }

  get active(): boolean {
    return !this.closed;
  }

  start(): void {
    if (this.closed || this.ws || this.timer) return;
    void this.connect();
  }

  /** Stops the feed for good (page left). */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.dropSocket();
  }

  private ticket(): Promise<FeedTicket> {
    if (this.o.ticket) return this.o.ticket();
    return api.watchBot(this.o.botId).then((r) => ({ url: r.decisionsUrl, ticket: r.decisionsTicket }));
  }

  private async connect(): Promise<void> {
    const s = this.store.getState();
    s.setStatus(this.failures === 0 && !s.hello ? 'connecting' : 'reconnecting');
    let t: FeedTicket;
    try {
      t = this.first ?? (await this.ticket());
      this.first = null;
    } catch (e) {
      if (this.closed) return;
      if (e instanceof ApiError && GONE.has(e.status)) return this.end('ended');
      if (e instanceof ApiError && DENIED.has(e.status)) {
        this.closed = true;
        this.store.getState().setStatus('error', errorMessage(e));
        return;
      }
      return this.retry(errorMessage(e));
    }
    if (this.closed) return;
    const Ctor = this.o.WebSocket ?? (globalThis as unknown as { WebSocket: FeedSocketCtor }).WebSocket;
    let ws: FeedSocket;
    try {
      ws = new Ctor(resolveWsUrl(t.url, t.ticket, this.o.origin));
    } catch (e) {
      // e.g. a SecurityError for ws:// from an https page: retrying cannot help
      this.closed = true;
      this.store.getState().setStatus('error', `cannot open the decision feed: ${errorMessage(e)}`);
      return;
    }
    this.ws = ws;
    ws.onopen = () => {
      if (this.ws === ws) this.store.getState().setStatus('live', null);
    };
    ws.onmessage = (ev) => {
      if (this.ws === ws) this.handle(ev.data);
    };
    ws.onerror = () => {};
    ws.onclose = (ev) => {
      if (this.ws !== ws) return;
      this.ws = null;
      if (!this.closed) this.retry(ev.reason || `decision feed closed (${ev.code ?? 0})`);
    };
  }

  private handle(data: unknown): void {
    const m = parseFeedMessage(data);
    if (!m) return;
    const s = this.store.getState();
    switch (m.t) {
      case 'hello':
        this.failures = 0; // a working connection: the next drop starts the backoff over
        s.setHello(m);
        s.setStatus('live', null);
        break;
      case 'decision':
        s.pushDecision(decisionView(m));
        break;
      case 'event': {
        const { t: _t, ...ev } = m;
        s.pushEvent(ev);
        break;
      }
      case 'stats': {
        const { t: _t, ...totals } = m;
        s.setTotals(totals);
        break;
      }
      case 'gap':
        s.gap(m.dropped);
        break;
      case 'bye':
        this.end(m.status);
        break;
    }
  }

  private end(status: string): void {
    if (this.closed) return;
    this.closed = true;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    this.dropSocket();
    this.store.getState().bye(status);
    this.o.onEnded?.(status);
  }

  private retry(reason: string): void {
    if (this.closed || this.timer) return;
    const min = this.o.minBackoffMs ?? 500;
    const max = this.o.maxBackoffMs ?? 15_000;
    const jitter = this.o.jitter ?? 0.2;
    const base = Math.min(max, min * 2 ** this.failures);
    const delay = Math.round(base * (1 + jitter * (2 * Math.random() - 1)));
    this.failures++;
    this.store.getState().setStatus('reconnecting', reason);
    this.timer = setTimeout(() => {
      this.timer = null;
      if (!this.closed) void this.connect();
    }, delay);
  }

  private dropSocket(): void {
    const ws = this.ws;
    this.ws = null;
    if (!ws) return;
    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
    try {
      ws.close(1000, 'done');
    } catch {
      // ignore
    }
  }
}
