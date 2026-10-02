// View model of the AI overlay: pure functions from a DecisionView (and the run's backend) to what the
// panel shows, so they can be tested without a DOM.
import type { DecisionAction, DecisionTarget } from '@/lib/api';
import { DECISION_FIELDS, formatMs, formatPercent, formatUsd } from '@/lib/bots';
import type { AiEventView, AiTotals, DecisionView } from './aiStore';

export const FIELD_LABELS: Record<string, string> = {
  mode: 'Mode',
  target: 'Target',
  fire_policy: 'Fire',
  movement: 'Move',
  weapon: 'Weapon',
  pickup: 'Pickup',
  danger: 'Danger',
};

export function fieldLabel(field: string): string {
  return FIELD_LABELS[field] ?? field.replace(/_/g, ' ');
}

/** Backends whose answers come from a model (the others are the model's stand-ins). */
export function isModelBackend(backend: string | undefined): boolean {
  return backend === 'jev' || backend === 'mock';
}

export type SourceTone = 'model' | 'scripted' | 'stale' | 'reflex' | 'default';

const TONES = new Set<SourceTone>(['model', 'scripted', 'stale', 'reflex', 'default']);

export interface ProvenanceBadge {
  field: string;
  label: string;
  source: string;
  tone: SourceTone;
}

function fieldOrder(names: string[]): string[] {
  const known = (DECISION_FIELDS as readonly string[]).filter((f) => names.includes(f));
  return [...known, ...names.filter((n) => !known.includes(n)).sort()];
}

/** One badge per decision field: where the value the bot acted on came from. */
export function provenanceBadges(d: DecisionView | null): ProvenanceBadge[] {
  if (!d) return [];
  return fieldOrder(Object.keys(d.provenance)).map((field) => {
    const source = d.provenance[field]!;
    return {
      field,
      label: fieldLabel(field),
      source,
      tone: TONES.has(source as SourceTone) ? (source as SourceTone) : 'default',
    };
  });
}

export interface Fallback {
  field: string;
  reason: string;
}

/**
 * Default-value reasons that mean the model answered (or was asked) and its answers were not usable. `weak`
 * is the arbiter's (its accumulated evidence is too weak or split); `low_confidence` is its name in traces
 * from before the evidence aggregation, kept for their replays.
 */
const MODEL_FAILED = new Set([
  'missing',
  'invalid',
  'unknown_option',
  'weak',
  'low_confidence',
  'error',
  'timeout',
]);

/**
 * Fields of a model backend's decision that fell back from the model: the value acted on is the scripted
 * policy's, or the field's default because the model's answers were unusable (missing, invalid, too weak
 * as evidence, an error or a timeout). A default for lack of a question or of a fresh answer (not_asked,
 * no_answer, ttl), a stale value (it has a badge of its own) and a reflex override are no fallback. A
 * scripted or ablation backend never "falls back": scripted is what it is.
 */
export function fallbackFields(d: DecisionView | null, modelBackend: boolean): Fallback[] {
  if (!d || !modelBackend) return [];
  return fieldOrder(Object.keys(d.provenance))
    .filter((f) => {
      const src = d.provenance[f];
      return src === 'scripted' || (src === 'default' && MODEL_FAILED.has(d.fallback[f] ?? ''));
    })
    .map((field) => ({ field, reason: d.fallback[field] ?? d.provenance[field]! }));
}

export interface OptionRow {
  label: string;
  p: number;
  /** "62%" */
  pct: string;
  chosen: boolean;
}

export interface QuestionRow {
  id: string;
  label: string;
  /** "conf 71%" or "" */
  confidence: string;
  options: OptionRow[];
}

/** The probability bars: one row per question, every option with the chosen one marked. */
export function questionRows(d: DecisionView | null, maxOptions = 6): QuestionRow[] {
  if (!d) return [];
  return d.questions.map((q) => {
    let options = q.options.map((o) => ({
      label: o.label,
      p: o.p,
      pct: formatPercent(o.p),
      chosen: o.label === q.chosen,
    }));
    if (options.length > maxOptions) {
      // keep the chosen option and the most probable ones, in their original order
      const keep = new Set(
        [...options]
          .sort((a, b) => Number(b.chosen) - Number(a.chosen) || b.p - a.p)
          .slice(0, maxOptions)
          .map((o) => o.label),
      );
      options = options.filter((o) => keep.has(o.label));
    }
    return {
      id: q.id,
      label: fieldLabel(q.id),
      confidence: q.confidence === undefined ? '' : `conf ${formatPercent(q.confidence)}`,
      options,
    };
  });
}

export function targetText(t: DecisionTarget | null): string {
  if (!t) return 'none';
  const parts = [t.id];
  if (t.class) parts.push(t.class);
  if (t.dist > 0) parts.push(`${Math.round(t.dist)}u`);
  return parts.join(' · ');
}

export function actionText(a: DecisionAction): string {
  const parts: string[] = [];
  if (a.movement) parts.push(a.movement);
  if (a.firePolicy) parts.push(a.firePolicy);
  if (a.weapon) parts.push(a.weapon);
  if (a.fire) parts.push('FIRE');
  return parts.join(' · ');
}

export interface StatCell {
  label: string;
  value: string;
}

/** The footer: last API latency, cost so far, decision rate, model share, kills / deaths. */
export function statCells(d: DecisionView | null, t: AiTotals | null): StatCell[] {
  return [
    { label: 'latency', value: formatMs(d?.latencyMs ?? null) },
    { label: 'cost', value: formatUsd(t?.costUsd ?? d?.costUsd ?? null) },
    { label: 'rate', value: t ? `${t.decisionRate.toFixed(1)}/s` : '–' },
    { label: 'model', value: formatPercent(t?.modelShare) },
    { label: 'k / d', value: t ? `${t.kills} / ${t.deaths}` : '–' },
    { label: 'p50', value: formatMs(t?.apiP50Ms ?? null) },
  ];
}

export function eventText(e: AiEventView): string {
  const d = e.data;
  const s = (k: string): string => (typeof d[k] === 'string' ? (d[k] as string) : '');
  const n = (k: string): number | undefined => (typeof d[k] === 'number' ? (d[k] as number) : undefined);
  switch (e.kind) {
    case 'kill':
      return `killed ${s('class') || s('target') || 'an enemy'}`;
    case 'death':
      return s('cause') ? `died (${s('cause')})` : 'died';
    case 'reload':
      return `reload ${s('slot') || 'save0'}`;
    case 'damage':
      return `took ${n('amount') ?? '?'} damage`;
    case 'level_start':
      return `entered ${e.map || s('map') || 'level'}`;
    case 'level_end':
      return `left ${e.map || s('map') || 'level'}${s('outcome') ? ` (${s('outcome')})` : ''}`;
    case 'stuck':
      return `stuck: ${s('stage') || 'recovering'}`;
    case 'budget':
      return s('reason') ? `budget: ${s('reason')}` : 'budget change';
    default:
      return e.kind.replace(/_/g, ' ');
  }
}
