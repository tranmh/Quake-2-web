// AI overlay: the view model (provenance badges, fallbacks, probability rows, stats) and the rendered
// panel (server-rendered markup: the app's tests run without a DOM).
import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import type { AiTotals, DecisionView } from '@/game/aiStore';
import {
  actionText,
  eventText,
  fallbackFields,
  isModelBackend,
  provenanceBadges,
  questionRows,
  statCells,
  targetText,
} from '@/game/aiView';
import { AiOverlayPanel } from '@/components/game/AiOverlay';

const decision: DecisionView = {
  sf: 144,
  lvl: 0,
  map: 'demo1',
  mode: 'fight',
  objective: 'go to func_door *32 · step 0',
  target: { id: 'e7', class: 'soldier_shotgun', dist: 410.4 },
  questions: [
    {
      id: 'target',
      options: [
        { label: 'e7', p: 0.62 },
        { label: 'e3', p: 0.3 },
        { label: 'none', p: 0.08 },
      ],
      chosen: 'e7',
      confidence: 0.71,
    },
    {
      id: 'fire_policy',
      options: [
        { label: 'hold', p: 0.2 },
        { label: 'fire_when_aligned', p: 0.8 },
      ],
      chosen: 'fire_when_aligned',
    },
  ],
  provenance: {
    target: 'model',
    mode: 'model',
    fire_policy: 'scripted',
    movement: 'stale',
    weapon: 'reflex',
  },
  fallback: {},
  action: { movement: 'strafe_left', firePolicy: 'fire_when_aligned', weapon: 'shotgun', fire: true },
  latencyMs: 183,
  costUsd: 0.00004,
};

const totals: AiTotals = {
  kills: 3,
  deaths: 1,
  decisions: 420,
  decisionRate: 9.87,
  modelShare: 0.83,
  staleRate: 0.02,
  apiP50Ms: 190,
  costUsd: 0.0123,
};

describe('aiView', () => {
  it('lists provenance in the trace field order', () => {
    expect(provenanceBadges(decision).map((b) => `${b.field}:${b.tone}`)).toEqual([
      'mode:model',
      'target:model',
      'fire_policy:scripted',
      'movement:stale',
      'weapon:reflex',
    ]);
    expect(provenanceBadges(null)).toEqual([]);
  });

  it('names the fields that fell back from the model', () => {
    // a feed without reasons: the scripted / default values of a model backend
    expect(fallbackFields(decision, true)).toEqual([{ field: 'fire_policy', reason: 'scripted' }]);
    // a scripted backend is scripted by design, not a fallback
    expect(fallbackFields(decision, false)).toEqual([]);
    const withReasons: DecisionView = {
      ...decision,
      provenance: {
        mode: 'default', // no fresh answer: not a fallback
        target: 'default', // the model's evidence is too weak or split: a fallback
        fire_policy: 'scripted',
        movement: 'stale',
        weapon: 'reflex', // an override: not a fallback
        pickup: 'default', // never asked: not a fallback
        danger: 'default', // the same as target, as traces from before the evidence aggregation name it
      },
      // as the trace has them
      fallback: {
        mode: 'ttl',
        target: 'weak',
        fire_policy: 'timeout',
        movement: 'held',
        weapon: 'dry',
        pickup: 'not_asked',
        danger: 'low_confidence',
      },
    };
    expect(fallbackFields(withReasons, true)).toEqual([
      { field: 'target', reason: 'weak' },
      { field: 'fire_policy', reason: 'timeout' },
      { field: 'danger', reason: 'low_confidence' },
    ]);
    expect(fallbackFields(withReasons, false)).toEqual([]);
    expect(isModelBackend('jev')).toBe(true);
    expect(isModelBackend('mock')).toBe(true);
    expect(isModelBackend('scripted')).toBe(false);
  });

  it('builds one probability row per option with the chosen one marked', () => {
    const rows = questionRows(decision);
    expect(rows.map((r) => r.label)).toEqual(['Target', 'Fire']);
    expect(rows[0]!.confidence).toBe('conf 71%');
    expect(rows[0]!.options).toEqual([
      { label: 'e7', p: 0.62, pct: '62%', chosen: true },
      { label: 'e3', p: 0.3, pct: '30%', chosen: false },
      { label: 'none', p: 0.08, pct: '8%', chosen: false },
    ]);
    expect(rows[1]!.confidence).toBe('');
  });

  it('trims long option lists but keeps the chosen option', () => {
    const options = Array.from({ length: 10 }, (_, i) => ({ label: `i${i}`, p: i / 100 }));
    const d = { ...decision, questions: [{ id: 'pickup', options, chosen: 'i0' }] };
    const row = questionRows(d, 4)[0]!;
    expect(row.options.map((o) => o.label)).toEqual(['i0', 'i7', 'i8', 'i9']);
  });

  it('formats target, action, stats and events', () => {
    expect(targetText(decision.target)).toBe('e7 · soldier_shotgun · 410u');
    expect(targetText(null)).toBe('none');
    expect(actionText(decision.action)).toBe('strafe_left · fire_when_aligned · shotgun · FIRE');
    const cells = Object.fromEntries(statCells(decision, totals).map((c) => [c.label, c.value]));
    expect(cells).toMatchObject({
      latency: '183 ms',
      cost: '$0.012',
      rate: '9.9/s',
      model: '83%',
      'k / d': '3 / 1',
    });
    expect(statCells(null, null).every((c) => c.value === '–')).toBe(true);
    expect(eventText({ kind: 'kill', sf: 1, data: { class: 'soldier' } })).toBe('killed soldier');
    expect(eventText({ kind: 'reload', sf: 1, data: { slot: 'save0' } })).toBe('reload save0');
  });
});

describe('AiOverlayPanel', () => {
  const render = (props: Partial<Parameters<typeof AiOverlayPanel>[0]> = {}) =>
    renderToStaticMarkup(
      createElement(AiOverlayPanel, {
        decision,
        hello: { t: 'hello', bot: 'b1', backend: 'mock', model: 'jev-1.13.0', maps: ['demo1'] },
        totals,
        status: 'live',
        open: true,
        ...props,
      }),
    );

  it('renders mode, a bar per option, the chosen options, provenance and the fallback badge', () => {
    const html = render();
    expect(html).toContain('data-testid="ai-overlay"');
    expect(html).toContain('data-mode="fight"');
    expect(html.match(/data-testid="prob-bar"/g)).toHaveLength(5);
    expect(html.match(/data-chosen="true"/g)).toHaveLength(2);
    expect(html).toContain('width:62.0%');
    expect(html).toContain('data-source="stale"');
    expect(html).toContain('data-source="reflex"');
    expect(html).toContain('data-testid="ai-fallback"');
    expect(html).toContain('FALLBACK');
    expect(html).toContain('mock · jev-1.13.0');
    expect(html).toContain('183 ms');
    expect(html).toContain('83%');
  });

  it('shows no fallback badge for a scripted run', () => {
    const html = render({ hello: { t: 'hello', bot: 'b1', backend: 'scripted', maps: [] } });
    expect(html).not.toContain('data-testid="ai-fallback"');
  });

  it('collapses to the toggle and reports the feed state', () => {
    const closed = render({ open: false });
    expect(closed).toContain('data-testid="ai-overlay"');
    expect(closed).not.toContain('prob-bar');
    const waiting = render({ decision: null, status: 'reconnecting' });
    expect(waiting).toContain('no decision yet');
    expect(waiting).toContain('reconnecting');
    expect(render({ status: 'error', error: 'denied' })).toContain('denied');
    expect(render({ dropped: 7 })).toContain('7 messages dropped');
  });
});
