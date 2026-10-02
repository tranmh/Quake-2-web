// What the bot decided, for the AI overlay. The live decision feed (decisionFeed.ts) or the replay feed
// (replayFeed.ts) writes it; GameSession publishes the server frame the video shows (serverFrame), and
// visibleDecision() picks the decision that frame was played with: decisions arrive ahead of the video
// (the relay buffers frames), so the overlay shows the newest decision made at or before the frame on
// screen instead of the newest one received.
import { create } from 'zustand';
import type { DecisionAction, DecisionQuestion, DecisionTarget, FeedEventKind, FeedHello } from '@/lib/api';
import { DECISION_FIELDS } from '@/lib/bots';

/** Decisions kept for alignment with the video (about 10 s at 10 decisions/s). */
export const HISTORY_LIMIT = 100;
/** Events kept for the overlay's ticker. */
export const EVENT_LIMIT = 20;

export type AiFeedStatus =
  | 'idle'
  | 'connecting' // first connection of the live feed
  | 'live'
  | 'reconnecting' // the live feed dropped; retrying with a fresh ticket
  | 'loading' // replay: fetching and indexing the trace
  | 'replay' // replay: the trace is indexed
  | 'ended' // the run ended (bye) or is not live any more
  | 'error';

/** One decision as the overlay shows it (a feed `decision` message, normalized). */
export interface DecisionView {
  sf: number;
  /** level attempt */
  lvl: number;
  map: string;
  mode: string;
  /** objective / route step ("" unknown) */
  objective: string;
  target: DecisionTarget | null;
  questions: DecisionQuestion[];
  /** field → source (model, scripted, stale, reflex, default) */
  provenance: Record<string, string>;
  /** field → why the model's answer was not used (only when the feed says so) */
  fallback: Record<string, string>;
  action: DecisionAction;
  latencyMs: number | null;
  costUsd: number | null;
}

/** Running totals (the feed's 1 Hz `stats` message, or computed from the trace in a replay). */
export interface AiTotals {
  kills: number;
  deaths: number;
  decisions: number;
  decisionRate: number;
  modelShare: number;
  staleRate: number;
  apiP50Ms: number;
  costUsd: number;
}

export interface AiEventView {
  kind: FeedEventKind | string;
  sf: number;
  lvl?: number;
  map?: string;
  data: Record<string, unknown>;
}

export interface AiState {
  status: AiFeedStatus;
  /** why the feed stopped (status error) or the last reconnect reason */
  error: string | null;
  hello: FeedHello | null;
  /** oldest first, at most HISTORY_LIMIT */
  history: DecisionView[];
  latest: DecisionView | null;
  totals: AiTotals | null;
  /** newest first, at most EVENT_LIMIT */
  events: AiEventView[];
  /** messages the server dropped for this viewer (gap) */
  dropped: number;
  /** the run's final status once the feed said bye */
  ended: string | null;
  /** the server frame the video shows (cl.frame.serverframe) */
  serverFrame: number;
  /** replay: the level attempt being played (the demo's NN); null while watching live */
  attempt: number | null;

  setStatus(status: AiFeedStatus, error?: string | null): void;
  setHello(h: FeedHello): void;
  pushDecision(d: DecisionView): void;
  pushEvent(e: AiEventView): void;
  setTotals(t: AiTotals): void;
  gap(dropped: number): void;
  bye(status: string): void;
  setServerFrame(sf: number): void;
  setAttempt(attempt: number | null): void;
  /** replay: the decision of the frame on screen and the totals up to it */
  showReplay(d: DecisionView | null, totals: AiTotals | null, events?: AiEventView[]): void;
  reset(): void;
}

const initial = {
  status: 'idle' as AiFeedStatus,
  error: null,
  hello: null,
  history: [] as DecisionView[],
  latest: null,
  totals: null,
  events: [] as AiEventView[],
  dropped: 0,
  ended: null,
  serverFrame: 0,
  attempt: null,
};

const FIELD_RANK = new Map<string, number>(DECISION_FIELDS.map((f, i) => [f, i]));

function rank(id: string): number {
  return FIELD_RANK.get(id) ?? DECISION_FIELDS.length;
}

/**
 * The questions of `next` plus the ones of `prev` it does not answer, in field order. A request asks a
 * lane's questions only (the fast lane target/fire/movement, the slow lane mode/pickup/danger), so the
 * overlay keeps each question's latest answer.
 */
export function mergeQuestions(
  prev: readonly DecisionQuestion[],
  next: readonly DecisionQuestion[],
): DecisionQuestion[] {
  const ids = new Set(next.map((q) => q.id));
  const out = [...next, ...prev.filter((q) => !ids.has(q.id))];
  return out
    .map((q, i) => ({ q, i }))
    .sort((a, b) => rank(a.q.id) - rank(b.q.id) || a.i - b.i)
    .map((x) => x.q);
}

/**
 * True when `next` starts a new level attempt after `prev`: another level or map, or server frames that
 * start over (a reload of the level-entry save spawns the server again; the feed's lvl is the level, not
 * the attempt).
 */
export function newAttempt(prev: DecisionView, next: DecisionView): boolean {
  return next.lvl !== prev.lvl || next.map !== prev.map || next.sf < prev.sf;
}

/**
 * The decision the frame on screen was played with: the newest decision of the current level attempt
 * (the newest decision's) whose server frame is at or before `serverFrame`; the latest one when there is
 * none (the video is ahead, or no frame yet).
 */
export function visibleDecision(s: Pick<AiState, 'history' | 'latest' | 'serverFrame'>): DecisionView | null {
  const h = s.history;
  if (!h.length || s.serverFrame <= 0) return s.latest;
  for (let i = h.length - 1; i >= 0; i--) {
    const d = h[i]!;
    if (d.sf <= s.serverFrame) return d;
    // never align with a decision of an earlier attempt (its frame numbers mean other frames)
    if (i > 0 && newAttempt(h[i - 1]!, d)) break;
  }
  return s.latest;
}

export const useAiStore = create<AiState>((set, get) => ({
  ...initial,
  setStatus: (status, error) => set(error === undefined ? { status } : { status, error }),
  setHello: (hello) => set({ hello }),
  pushDecision: (d) => {
    const { history, latest } = get();
    const view =
      latest && !newAttempt(latest, d)
        ? { ...d, questions: mergeQuestions(latest.questions, d.questions) }
        : d;
    const h =
      history.length >= HISTORY_LIMIT ? history.slice(history.length - HISTORY_LIMIT + 1) : history.slice();
    h.push(view);
    set({ history: h, latest: view });
  },
  pushEvent: (e) => set({ events: [e, ...get().events].slice(0, EVENT_LIMIT) }),
  setTotals: (totals) => set({ totals }),
  gap: (dropped) => set({ dropped: get().dropped + Math.max(0, dropped) }),
  bye: (status) => set({ ended: status, status: 'ended' }),
  setServerFrame: (serverFrame) => {
    if (get().serverFrame !== serverFrame) set({ serverFrame });
  },
  setAttempt: (attempt) => {
    if (get().attempt !== attempt) set({ attempt });
  },
  showReplay: (d, totals, events) => {
    const s = get();
    if (s.latest === d && s.totals === totals && (events === undefined || events === s.events)) return;
    const p: Partial<AiState> = { latest: d, history: d ? [d] : [], totals };
    if (events !== undefined) p.events = events;
    set(p);
  },
  reset: () => set(initial),
}));
