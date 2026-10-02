// Replay feed: the AI overlay of a recorded run, rebuilt from the episode's decision trace
// (ep-NNN/trace.jsonl.gz, schema q2bot.trace/1, server/internal/agent/trace). The server serves the file
// with Content-Encoding: gzip, so the browser hands over the JSON Lines text (a body that still starts with
// the gzip magic is decompressed here). The trace is indexed by level attempt (a level_start or a reload
// starts the attempt the next NN-<map>.dm2 recorded) and server frame; GameSession tells the feed which
// demo and frame are on screen and the feed shows the decision tick made at or before it, in the same
// DecisionView shape as the live feed.
import { artifactUrl, type DecisionQuestion, type DecisionTarget, type FeedHello } from '@/lib/api';
import { traceArtifactName } from '@/lib/bots';
import {
  mergeQuestions,
  useAiStore,
  type AiEventView,
  type AiState,
  type AiTotals,
  type DecisionView,
} from './aiStore';

type Obj = Record<string, unknown>;

const isObj = (v: unknown): v is Obj => typeof v === 'object' && v !== null && !Array.isArray(v);
const num = (v: unknown, def = 0): number => (typeof v === 'number' && Number.isFinite(v) ? v : def);
const str = (v: unknown, def = ''): string => (typeof v === 'string' ? v : def);

/** BUTTON_ATTACK of usercmd_t buttons. */
const BUTTON_ATTACK = 1;
/** decisionRate window (game ms) */
const RATE_WINDOW_MS = 5000;
/** apiP50Ms: median of the last N request latencies */
const LATENCY_WINDOW = 31;
const EVENT_KINDS = new Set([
  'level_start',
  'level_end',
  'death',
  'reload',
  'kill',
  'damage',
  'stuck',
  'budget',
]);
/** envelope keys: not part of an event's data (as the live feed sends it) */
const ENVELOPE = new Set(['v', 'type', 'run', 'ep', 'seq', 'wall', 'gms', 'lvl', 'map', 'sf']);

/** The trace event types the index reads (cmds and api_call lines are skipped unparsed). */
const WANTED = new Set(['run_start', 'decision', ...EVENT_KINDS]);
const TYPE_RE = /^\{"v":\d+,"type":"([a-z_]+)"/;

export interface TraceAttempt {
  /** attempt index in the episode: the NN of the attempt's NN-<map>.dm2 */
  index: number;
  /** trace lvl: the level (level_start count) the attempt belongs to */
  lvl: number;
  map: string;
  visit: number;
  /** the attempt's decision ticks in server-frame order */
  decisions: DecisionView[];
  /** totals as of each decision (parallel to decisions) */
  totals: AiTotals[];
  /** events in trace order, sf clamped to be non-decreasing */
  events: AiEventView[];
  /** totals when the attempt began */
  startTotals: AiTotals;
}

export interface TraceIndex {
  hello: FeedHello | null;
  attempts: TraceAttempt[];
  /** decision ticks indexed */
  ticks: number;
  /** lines that were not valid JSON */
  bad: number;
}

const clamp01 = (p: number): number => (p < 0 ? 0 : p > 1 ? 1 : p);

/** A score level's short name: its description up to the first colon ("safe: no threat" → "safe"). */
function levelLabel(desc: string, key: string): string {
  const i = desc.indexOf(':');
  const name = i > 0 ? desc.slice(0, i).trim() : '';
  return name && name.length <= 24 ? name : key;
}

/**
 * One answered question of a backend response (the trace's raw response and questions, the Jev wire
 * format) as a DecisionQuestion, like the live feed builds it (runner/decisions.go questionView): a
 * choice's options in the question's criteria order with their probabilities and the backend's choice; a
 * score's levels with the level of the rounded score chosen; a noul as true / false.
 */
export function answerQuestion(id: string, answer: unknown, question: unknown): DecisionQuestion | null {
  if (!isObj(answer)) return null;
  const type = str(answer['type']) || (isObj(question) ? str(question['type']) : '');
  if (type === 'noul') {
    const p = clamp01(num(answer['noul']));
    return {
      id,
      options: [
        { label: 'true', p },
        { label: 'false', p: 1 - p },
      ],
      chosen: p >= 0.5 ? 'true' : 'false',
      // a noul has no confidence: its decisiveness
      confidence: Math.abs(2 * p - 1),
    };
  }
  const probs = isObj(answer['probabilities']) ? answer['probabilities'] : {};
  const criteria = isObj(question) ? question['criteria'] : undefined;
  let options: { label: string; p: number }[];
  let chosen: string;
  if (type === 'score') {
    const legend = isObj(answer['legend']) ? answer['legend'] : {};
    const levels = Array.isArray(criteria)
      ? criteria.map((c, i) => ({ key: String(i), label: levelLabel(str(c), String(i)) }))
      : [...new Set([...Object.keys(legend), ...Object.keys(probs)])]
          .sort((a, b) => Number(a) - Number(b))
          .map((k) => ({ key: k, label: levelLabel(str(legend[k]), k) }));
    options = levels.map((l) => ({ label: l.label, p: clamp01(num(probs[l.key])) }));
    const score = answer['score'];
    if (typeof score === 'number') chosen = levels[Math.round(score)]?.label ?? '';
    else {
      // no score: the most probable level
      let best = -1;
      chosen = '';
      for (const o of options) if (o.p > best) [best, chosen] = [o.p, o.label];
    }
  } else {
    const order = isObj(criteria) ? Object.keys(criteria) : [];
    const keys = [...order, ...Object.keys(probs).filter((k) => !order.includes(k))];
    options = keys.map((k) => ({ label: k, p: clamp01(num(probs[k])) }));
    chosen = str(answer['choice']);
  }
  const q: DecisionQuestion = { id, options, chosen };
  if (typeof answer['confidence'] === 'number') q.confidence = clamp01(answer['confidence']);
  return q;
}

/** Builds a TraceIndex from the events of one episode's trace, in order. */
export class TraceIndexer {
  private hello: FeedHello | null = null;
  private readonly attempts: TraceAttempt[] = [];
  private cur: TraceAttempt | null = null;
  private lastSf = 0;
  private lastGms = 0;
  private ticks = 0;
  bad = 0;

  // decision state carried between ticks
  private questions: DecisionQuestion[] = [];
  private objective = '';
  private enemies = new Map<string, { class: string; dist: number }>();
  private lastLatency: number | null = null;
  /** cost of the requests answered since the previous tick */
  private tickCost = 0;

  // running totals
  private kills = 0;
  private deaths = 0;
  private requests = 0;
  private stale = 0;
  private cost = 0;
  private decided = 0;
  private model = 0;
  private readonly reqTimes: number[] = [];
  private readonly latencies: number[] = [];

  /** Feeds one JSON line (blank lines and unneeded types are skipped). */
  pushLine(line: string): void {
    if (!line || line.charCodeAt(0) !== 123 /* { */) return;
    const m = TYPE_RE.exec(line);
    if (m && !WANTED.has(m[1]!)) return;
    let e: unknown;
    try {
      e = JSON.parse(line);
    } catch {
      this.bad++;
      return;
    }
    if (isObj(e)) this.push(e);
  }

  push(e: Obj): void {
    const type = str(e['type']);
    const sf = num(e['sf']);
    if (type !== 'run_start') this.lastGms = num(e['gms'], this.lastGms);
    switch (type) {
      case 'run_start': {
        const maps = Array.isArray(e['maps'])
          ? e['maps'].filter((x): x is string => typeof x === 'string')
          : [];
        this.hello = { t: 'hello', bot: str(e['run']), backend: str(e['backend']), maps };
        if (typeof e['model'] === 'string' && e['model']) this.hello.model = e['model'];
        return;
      }
      case 'level_start':
        this.begin(num(e['lvl']), str(e['map']), num(e['visit']));
        break;
      case 'reload': {
        const c = this.cur;
        this.begin(c?.lvl ?? num(e['lvl']), c?.map ?? str(e['map']), c?.visit ?? 0);
        break;
      }
      case 'kill':
        this.kills++;
        break;
      case 'death':
        this.deaths++;
        break;
      case 'decision':
        if (e['lane'] === 'tick') this.tick(e, sf);
        else this.request(e);
        return;
    }
    if (EVENT_KINDS.has(type)) this.event(type, sf, e);
  }

  finish(): TraceIndex {
    return { hello: this.hello, attempts: this.attempts, ticks: this.ticks, bad: this.bad };
  }

  private begin(lvl: number, map: string, visit: number): void {
    const a: TraceAttempt = {
      index: this.attempts.length,
      lvl,
      map,
      visit,
      decisions: [],
      totals: [],
      events: [],
      startTotals: this.totals(this.lastGms),
    };
    this.attempts.push(a);
    this.cur = a;
    this.lastSf = 0;
    // what the bot knew of the level is gone with it
    this.questions = [];
    this.objective = '';
    this.enemies = new Map();
  }

  private attempt(e: Obj): TraceAttempt {
    // decisions before any level_start (should not happen) open an attempt of their own
    if (!this.cur) this.begin(num(e['lvl']), str(e['map']), 0);
    return this.cur!;
  }

  private event(kind: string, sf: number, e: Obj): void {
    const a = this.attempt(e);
    const data: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(e)) if (!ENVELOPE.has(k)) data[k] = v;
    // a level_end is published with the next level's frames: keep the attempt's order searchable
    const at = Math.max(sf, this.lastSf);
    a.events.push({ kind, sf: at, lvl: num(e['lvl']), map: str(e['map']), data });
  }

  private request(e: Obj): void {
    this.requests++;
    const gms = num(e['gms']);
    this.reqTimes.push(gms);
    if (e['stale'] === true) this.stale++;
    const cost = num(e['cost_usd']);
    this.cost += cost;
    this.tickCost += cost;
    const resp = e['response'];
    const answered = (typeof e['err'] !== 'string' || !e['err']) && isObj(resp);
    if (answered) {
      this.lastLatency = num(e['latency_ms']);
      this.latencies.push(this.lastLatency);
      if (this.latencies.length > LATENCY_WINDOW) this.latencies.shift();
    }
    const state = e['state'];
    if (isObj(state)) {
      const obj = state['objective'];
      // the slow lane's state holds the route objective
      if (e['lane'] === 'slow') this.objective = isObj(obj) ? str(obj['desc']) : '';
      if (Array.isArray(state['enemies'])) {
        const m = new Map<string, { class: string; dist: number }>();
        for (const en of state['enemies']) {
          if (isObj(en) && typeof en['id'] === 'string')
            m.set(en['id'], { class: str(en['class']), dist: num(en['units']) });
        }
        this.enemies = m;
      }
    }
    // a stale answer was not applied
    if (!answered || !isObj(resp['answers']) || e['stale'] === true) return;
    const qs = isObj(e['questions']) ? e['questions'] : {};
    const latest: DecisionQuestion[] = [];
    for (const [id, ans] of Object.entries(resp['answers'])) {
      const q = answerQuestion(id, ans, qs[id]);
      if (q) latest.push(q);
    }
    if (latest.length) this.questions = mergeQuestions(this.questions, latest);
  }

  private tick(e: Obj, sf: number): void {
    const a = this.attempt(e);
    const intent = isObj(e['intent']) ? e['intent'] : {};
    const tick = isObj(e['tick']) ? e['tick'] : {};
    const provenance: Record<string, string> = {};
    const fallback: Record<string, string> = {};
    if (Array.isArray(intent['fields'])) {
      for (const f of intent['fields']) {
        if (!isObj(f) || typeof f['name'] !== 'string') continue;
        const source = str(f['source'], 'default');
        provenance[f['name']] = source;
        // the reason as the trace has it (a fallback's, or a reflex's override): aiView decides
        const reason = str(f['fallback']);
        if (reason) fallback[f['name']] = reason;
        if (source !== 'default') {
          this.decided++;
          if (source === 'model') this.model++;
        }
      }
    }
    // the track the bot fights (not a decided target it is not fighting)
    const targetId = str(tick['target']);
    let target: DecisionTarget | null = null;
    if (targetId) {
      const en = this.enemies.get(targetId);
      target = { id: targetId, class: en?.class ?? '', dist: en?.dist ?? 0 };
    }
    const step = num(tick['step'], -1);
    const objective =
      step >= 0
        ? this.objective
          ? `${this.objective} · step ${step}`
          : `route step ${step}`
        : this.objective;
    const cmds = Array.isArray(e['cmds']) ? e['cmds'] : [];
    const fields = Array.isArray(intent['fields']) ? intent['fields'] : [];
    const fire = fields.find((f) => isObj(f) && f['name'] === 'fire_policy') as Obj | undefined;
    const view: DecisionView = {
      sf,
      lvl: a.index,
      map: a.map || str(e['map']),
      mode: str(tick['mode']) || str(intent['mode']),
      objective,
      target,
      questions: this.questions,
      provenance,
      fallback,
      action: {
        // the movement executed ("nav": following the navigator), the fire policy acted on, the weapon in hand
        movement: str(tick['move']) || 'nav',
        firePolicy: (fire && str(fire['value'])) || str(intent['fire_policy']),
        weapon: str(tick['weapon']),
        // fired since the previous tick
        fire: cmds.some((c) => isObj(c) && (num(c['buttons']) & BUTTON_ATTACK) !== 0),
      },
      latencyMs: this.lastLatency,
      costUsd: this.tickCost,
    };
    this.tickCost = 0;
    // frames are non-decreasing within an attempt; a stray older frame keeps the order searchable
    view.sf = Math.max(sf, this.lastSf);
    this.lastSf = view.sf;
    a.decisions.push(view);
    a.totals.push(this.totals(num(e['gms'])));
    this.ticks++;
  }

  private totals(gms: number): AiTotals {
    const t = this.reqTimes;
    while (t.length && t[0]! < gms - RATE_WINDOW_MS) t.shift();
    let p50 = 0;
    if (this.latencies.length) {
      const s = [...this.latencies].sort((x, y) => x - y);
      p50 = s[s.length >> 1]!;
    }
    return {
      kills: this.kills,
      deaths: this.deaths,
      decisions: this.requests,
      decisionRate: t.length / (RATE_WINDOW_MS / 1000),
      modelShare: this.decided ? this.model / this.decided : 0,
      staleRate: this.requests ? this.stale / this.requests : 0,
      apiP50Ms: p50,
      costUsd: this.cost,
    };
  }
}

/** Index of the last element of `xs` (sorted by sf) with sf ≤ `sf`, or -1. */
export function lastAtOrBefore(xs: readonly { sf: number }[], sf: number): number {
  let lo = 0;
  let hi = xs.length - 1;
  let found = -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (xs[mid]!.sf <= sf) {
      found = mid;
      lo = mid + 1;
    } else hi = mid - 1;
  }
  return found;
}

/**
 * The trace attempt a demo recorded: normally the one with the demo's NN; when the counts disagree (a
 * trace that lost events) the nearest attempt on the same map.
 */
export function attemptFor(index: TraceIndex, nn: number, map: string): TraceAttempt | null {
  const a = index.attempts[nn];
  if (a && (!map || a.map === map)) return a;
  let best: TraceAttempt | null = null;
  for (const c of index.attempts) {
    if (c.map !== map) continue;
    if (!best || Math.abs(c.index - nn) < Math.abs(best.index - nn)) best = c;
  }
  return best;
}

/** Streams a JSON Lines body (decompressing a still gzipped one) line by line. */
export async function readLines(res: Response, onLine: (line: string) => void): Promise<void> {
  if (!res.body) {
    for (const l of (await res.text()).split('\n')) onLine(l);
    return;
  }
  const reader = res.body.getReader();
  const first = await reader.read();
  if (first.done) return;
  const head = first.value;
  const rest = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(head);
    },
    async pull(c) {
      const r = await reader.read();
      if (r.done) c.close();
      else c.enqueue(r.value);
    },
    cancel(reason) {
      return reader.cancel(reason);
    },
  });
  const gz = head.length >= 2 && head[0] === 0x1f && head[1] === 0x8b;
  const bytes = gz
    ? rest.pipeThrough(new DecompressionStream('gzip') as unknown as TransformStream<Uint8Array, Uint8Array>)
    : rest;
  const text = bytes
    .pipeThrough(new TextDecoderStream() as unknown as TransformStream<Uint8Array, string>)
    .getReader();
  let tail = '';
  for (;;) {
    const r = await text.read();
    if (r.done) break;
    const chunk = tail + r.value;
    let start = 0;
    for (let i = chunk.indexOf('\n'); i >= 0; i = chunk.indexOf('\n', start)) {
      onLine(chunk.slice(start, i));
      start = i + 1;
    }
    tail = chunk.slice(start);
  }
  if (tail) onLine(tail);
}

export interface ReplayFeedOptions {
  botId: string;
  episode: number;
  store?: { getState(): AiState };
  /** the trace's Response (default: GET the episode's trace artifact) */
  fetchTrace?: (signal: AbortSignal) => Promise<Response>;
}

/** Loads a run's trace and shows the decisions of the replayed frames in the AI store. */
export class ReplayFeed {
  private index: TraceIndex | null = null;
  private readonly abort = new AbortController();
  private readonly store: { getState(): AiState };
  private shown: { attempt: TraceAttempt | null; i: number; ev: number } = { attempt: null, i: -2, ev: -2 };
  private pending: { nn: number; map: string; sf: number } | null = null;

  constructor(private readonly o: ReplayFeedOptions) {
    this.store = o.store ?? useAiStore;
  }

  get loaded(): TraceIndex | null {
    return this.index;
  }

  async start(): Promise<void> {
    const s = this.store.getState();
    s.setStatus('loading');
    try {
      const res = this.o.fetchTrace
        ? await this.o.fetchTrace(this.abort.signal)
        : await fetch(artifactUrl(this.o.botId, traceArtifactName(this.o.episode)), {
            credentials: 'include',
            signal: this.abort.signal,
          });
      if (!res.ok)
        throw new Error(res.status === 404 ? 'this run has no decision trace' : `trace: ${res.status}`);
      const ix = new TraceIndexer();
      await readLines(res, (l) => ix.pushLine(l));
      if (this.abort.signal.aborted) return;
      this.index = ix.finish();
      if (this.index.hello) this.store.getState().setHello(this.index.hello);
      this.store.getState().setStatus('replay', null);
      if (this.pending) this.show(this.pending.nn, this.pending.map, this.pending.sf);
    } catch (e) {
      if (this.abort.signal.aborted) return;
      this.store.getState().setStatus('error', e instanceof Error ? e.message : String(e));
    }
  }

  /** The demo with attempt `nn` on `map` shows server frame `sf`. */
  show(nn: number, map: string, sf: number): void {
    this.pending = { nn, map, sf };
    const ix = this.index;
    if (!ix) return;
    const a = attemptFor(ix, nn, map);
    const i = a ? lastAtOrBefore(a.decisions, sf) : -1;
    const ev = a ? lastAtOrBefore(a.events, sf) : -1;
    if (this.shown.attempt === a && this.shown.i === i && this.shown.ev === ev) return;
    const evChanged = this.shown.attempt !== a || this.shown.ev !== ev;
    this.shown = { attempt: a, i, ev };
    const d = a && i >= 0 ? a.decisions[i]! : null;
    const totals = a ? (i >= 0 ? a.totals[i]! : a.startTotals) : null;
    const events = evChanged && a ? a.events.slice(Math.max(0, ev - 19), ev + 1).reverse() : undefined;
    this.store.getState().showReplay(d, totals, events ?? (a ? undefined : []));
  }

  close(): void {
    this.abort.abort();
  }
}
