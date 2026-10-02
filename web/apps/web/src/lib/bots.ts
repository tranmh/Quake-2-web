// Bot run data shared by the bot pages: the run.json schema (q2bot.run/1, server/internal/agent/metrics
// RunSummary; snake_case like the Go JSON tags), the run's artifact layout and display formatting.
import type { BotArtifact } from './api';

export const RUN_SCHEMA = 'q2bot.run/1';

/** The demo campaign's visits in order (runner: Maps is a prefix of these, nil = all of them). */
export const CAMPAIGN = ['demo1', 'demo2', 'demo3', 'demo2'] as const;

/** Decision fields in the order the trace lists them (trace.Intent.Fields). */
export const DECISION_FIELDS = [
  'mode',
  'target',
  'fire_policy',
  'movement',
  'weapon',
  'pickup',
  'danger',
] as const;

export interface RunTotals {
  levels: number;
  levels_completed: number;
  deaths: number;
  reloads: number;
  damage_taken: number;
  /** kills the bot perceived */
  bot_kills: number;
  stuck: number;
  /** the game's own counters (metrics only) */
  kills: number;
  monsters: number;
  secrets: number;
  total_secrets: number;
  combat_ms: number;
}

export interface LevelSummary {
  lvl: number;
  map: string;
  visit: number;
  /** exit | victory | death_limit | timeout | stalled | aborted | error | incomplete */
  outcome: string;
  reason?: string;
  start_gms: number;
  time_ms: number;
  combat_ms: number;
  deaths: number;
  reloads: number;
  damage_taken: number;
  bot_kills: number;
  stuck: number;
  kills: number;
  monsters: number;
  secrets: number;
  total_secrets: number;
}

/** Per-tick provenance of one decision field. */
export interface TickProvenance {
  default: number;
  model: number;
  scripted: number;
  stale: number;
  reflex: number;
  decided: number;
  model_share: number;
  stale_share: number;
}

export interface TickStats {
  ticks: number;
  fields: Record<string, TickProvenance>;
  gate_model_share: number;
  tick_events?: number;
}

/** Per-request provenance of one decision field. */
export interface RequestProvenance {
  model: number;
  scripted: number;
  reflex: number;
  stale: number;
  other?: number;
  model_share: number;
}

export interface DecisionStats {
  decisions: number;
  ticks?: number;
  fields: Record<string, RequestProvenance>;
  all: RequestProvenance;
  gate_model_share: number;
  comparisons: number;
  disagreements: number;
  disagreement_rate: number;
}

export interface Percentiles {
  p50: number;
  p95: number;
  p99: number;
  max: number;
}

export interface ApiStats {
  calls: number;
  ok: number;
  errors: number;
  retries: number;
  stale: number;
  stale_rate: number;
  by_status?: Record<string, number>;
  latency_ms: Percentiles;
  input_tokens: number;
  output_tokens: number;
  cost_usd: number;
  combat_calls: number;
  combat_qps: number;
}

export interface GateSummary {
  min_model_share: number;
  max_stale_rate: number;
  basis: string;
  model_shares: Record<string, number>;
  model_share: number;
  stale_rate: number;
  stale_shares?: Record<string, number>;
  tick_stale_share: number;
  model_backend: boolean;
  budget_exhausted: boolean;
  passed: boolean;
  reasons?: string[];
}

export interface BudgetSummary {
  spent_usd: number;
  limit_usd: number;
  rate_hz: number;
  scripted_only: boolean;
  reason?: string;
}

export interface EpisodeSummary {
  index: number;
  seed: number;
  /** completed | failed | aborted */
  outcome: string;
  reason?: string;
  game_ms: number;
  totals: RunTotals;
  levels: LevelSummary[] | null;
  ticks?: TickStats;
}

export interface RunSummary {
  schema: string;
  run: string;
  outcome: string;
  reason?: string;
  backend?: string;
  model?: string;
  session?: string;
  maps?: string[];
  skill: number;
  seed: number;
  episode_seeds?: number[];
  model_driven: boolean;
  gate?: GateSummary;
  started?: string;
  wall_ms: number;
  game_ms: number;
  events: number;
  totals: RunTotals;
  decisions: DecisionStats;
  ticks?: TickStats;
  api: ApiStats;
  budget?: BudgetSummary;
  errors: number;
  last_error?: string;
  episodes: EpisodeSummary[] | null;
}

// ---------------------------------------------------------------- artifacts

export interface DemoArtifact {
  name: string;
  size: number;
  episode: number;
  /** level attempt within the episode (the NN of NN-<map>.dm2) */
  attempt: number;
  map: string;
}

// the names the server serves (runner artifactName; demo.Recorder keeps '.', '_' and '-' of a map name)
const DEMO_RE = /^ep-(\d{3,4})\/demos\/(\d{2,4})-([A-Za-z0-9_-][A-Za-z0-9._-]{0,63})\.dm2$/;
const EPISODE_RE = /^ep-(\d{3,4})\//;

/** The episode's .dm2 artifacts in recording order. */
export function demoArtifacts(artifacts: readonly BotArtifact[], episode = 0): DemoArtifact[] {
  const out: DemoArtifact[] = [];
  for (const a of artifacts) {
    const m = DEMO_RE.exec(a.name);
    if (!m || Number(m[1]) !== episode) continue;
    out.push({ name: a.name, size: a.size, episode, attempt: Number(m[2]), map: m[3]! });
  }
  return out.sort((a, b) => a.attempt - b.attempt);
}

/** Episode indexes that have artifacts, ascending. */
export function artifactEpisodes(artifacts: readonly BotArtifact[]): number[] {
  const s = new Set<number>();
  for (const a of artifacts) {
    const m = EPISODE_RE.exec(a.name);
    if (m) s.add(Number(m[1]));
  }
  return [...s].sort((a, b) => a - b);
}

export function episodeDir(episode: number): string {
  return `ep-${String(episode).padStart(3, '0')}`;
}

export function traceArtifactName(episode: number): string {
  return `${episodeDir(episode)}/trace.jsonl.gz`;
}

// ---------------------------------------------------------------- provenance

export interface FieldShare {
  name: string;
  ticks: number;
  /** share of all ticks per source (sums to 1 when ticks > 0) */
  model: number;
  scripted: number;
  stale: number;
  reflex: number;
  default: number;
  /** share of decided ticks (not default) that came from the model */
  modelShare: number;
}

/** Tick provenance per field in the trace's field order (unknown fields after them, sorted). */
export function fieldShares(ticks: TickStats | undefined | null): FieldShare[] {
  if (!ticks?.fields) return [];
  const names = Object.keys(ticks.fields);
  const known = DECISION_FIELDS.filter((f) => names.includes(f)) as string[];
  const rest = names.filter((n) => !known.includes(n)).sort();
  return [...known, ...rest].map((name) => {
    const f = ticks.fields[name]!;
    const total = f.default + f.model + f.scripted + f.stale + (f.reflex ?? 0);
    const share = (n: number) => (total > 0 ? n / total : 0);
    return {
      name,
      ticks: total,
      model: share(f.model),
      scripted: share(f.scripted),
      stale: share(f.stale),
      reflex: share(f.reflex ?? 0),
      default: share(f.default),
      modelShare: f.model_share,
    };
  });
}

// ---------------------------------------------------------------- formatting

/** Game time as m:ss.t (h:mm:ss beyond an hour). */
export function formatGameTime(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '–';
  const tenths = Math.floor(ms / 100);
  const s = Math.floor(tenths / 10);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = String(s % 60).padStart(2, '0');
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${ss}`;
  return `${m}:${ss}.${tenths % 10}`;
}

export function formatPercent(x: number | undefined | null, digits = 0): string {
  if (x === undefined || x === null || !Number.isFinite(x)) return '–';
  return `${(x * 100).toFixed(digits)}%`;
}

/** Dollars with enough digits for per-token prices ($0.000021). */
export function formatUsd(x: number | undefined | null): string {
  if (x === undefined || x === null || !Number.isFinite(x)) return '–';
  if (x === 0) return '$0';
  if (Math.abs(x) >= 1) return `$${x.toFixed(2)}`;
  if (Math.abs(x) >= 0.01) return `$${x.toFixed(3)}`;
  return `$${x.toPrecision(2)}`;
}

export function formatMs(x: number | undefined | null): string {
  if (x === undefined || x === null || !Number.isFinite(x)) return '–';
  return x >= 1000 ? `${(x / 1000).toFixed(2)} s` : `${Math.round(x)} ms`;
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / (1 << 20)).toFixed(1)} MiB`;
}

export const SKILL_NAMES = ['Easy', 'Medium', 'Hard', 'Nightmare'];
