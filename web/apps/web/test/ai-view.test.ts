// AI overlay view model: the edge cases next to ai-overlay.test.ts (unknown fields and sources, option
// trimming boundaries, empty targets / actions, stat fallbacks and every event kind).
import { describe, expect, it } from 'vitest';
import type { DecisionView } from '@/game/aiStore';
import {
  actionText,
  eventText,
  fallbackFields,
  fieldLabel,
  isModelBackend,
  provenanceBadges,
  questionRows,
  statCells,
  targetText,
} from '@/game/aiView';

const base: DecisionView = {
  sf: 1,
  lvl: 0,
  map: 'demo1',
  mode: 'explore',
  objective: '',
  target: null,
  questions: [],
  provenance: {},
  fallback: {},
  action: {},
  latencyMs: 0,
  costUsd: 0,
} as unknown as DecisionView;

describe('fieldLabel / isModelBackend', () => {
  it('labels known fields and spells out unknown ones', () => {
    expect(fieldLabel('fire_policy')).toBe('Fire');
    expect(fieldLabel('danger')).toBe('Danger');
    expect(fieldLabel('look_at_me')).toBe('look at me');
    expect(fieldLabel('')).toBe('');
  });

  it('treats only jev and mock as model backends', () => {
    expect(isModelBackend(undefined)).toBe(false);
    expect(isModelBackend('')).toBe(false);
    expect(isModelBackend('JEV')).toBe(false);
    expect(isModelBackend('ablation')).toBe(false);
  });
});

describe('provenanceBadges', () => {
  it('orders unknown fields alphabetically after the known ones and maps unknown sources to default', () => {
    const d = { ...base, provenance: { zeta: 'model', weapon: 'cached', alpha: 'stale', mode: 'default' } };
    expect(provenanceBadges(d)).toEqual([
      { field: 'mode', label: 'Mode', source: 'default', tone: 'default' },
      { field: 'weapon', label: 'Weapon', source: 'cached', tone: 'default' },
      { field: 'alpha', label: 'alpha', source: 'stale', tone: 'stale' },
      { field: 'zeta', label: 'zeta', source: 'model', tone: 'model' },
    ]);
  });

  it('is empty for a decision without provenance', () => {
    expect(provenanceBadges(base)).toEqual([]);
  });
});

describe('fallbackFields', () => {
  it('reports every model-failure reason and none of the others', () => {
    const failed = ['missing', 'invalid', 'unknown_option', 'weak', 'low_confidence', 'error', 'timeout'];
    const fine = ['not_asked', 'no_answer', 'ttl', ''];
    const provenance: Record<string, string> = {};
    const fallback: Record<string, string> = {};
    for (const r of [...failed, ...fine]) {
      provenance[`f_${r}`] = 'default';
      if (r) fallback[`f_${r}`] = r;
    }
    const out = fallbackFields({ ...base, provenance, fallback }, true);
    expect(out.map((f) => f.reason).sort()).toEqual([...failed].sort());
  });

  it('uses the provenance as the reason of a scripted field without one', () => {
    const d = { ...base, provenance: { weapon: 'scripted', mode: 'model' }, fallback: { mode: 'timeout' } };
    expect(fallbackFields(d, true)).toEqual([{ field: 'weapon', reason: 'scripted' }]);
    expect(fallbackFields(null, true)).toEqual([]);
  });
});

describe('questionRows', () => {
  const options = [
    { label: 'a', p: 0.1 },
    { label: 'b', p: 0.4 },
    { label: 'c', p: 0.2 },
    { label: 'd', p: 0.3 },
  ];

  it('keeps all options at exactly maxOptions', () => {
    const d = { ...base, questions: [{ id: 'mode', options, chosen: 'a' }] };
    expect(questionRows(d, 4)[0]!.options.map((o) => o.label)).toEqual(['a', 'b', 'c', 'd']);
  });

  it('trims to the most probable ones in original order when nothing matches the chosen label', () => {
    const d = { ...base, questions: [{ id: 'mode', options, chosen: 'zz', confidence: 0 }] };
    const row = questionRows(d, 2)[0]!;
    expect(row.options.map((o) => o.label)).toEqual(['b', 'd']);
    expect(row.options.every((o) => !o.chosen)).toBe(true);
    expect(row.confidence).toBe('conf 0%');
    expect(row).toMatchObject({ id: 'mode', label: 'Mode' });
  });

  it('formats a non-finite probability as a dash', () => {
    const d = { ...base, questions: [{ id: 'x_y', options: [{ label: 'q', p: Number.NaN }], chosen: 'q' }] };
    const row = questionRows(d)[0]!;
    expect(row.label).toBe('x y');
    expect(row.options[0]).toMatchObject({ pct: '–', chosen: true });
    expect(questionRows(null)).toEqual([]);
  });
});

describe('targetText / actionText', () => {
  it('omits an unknown class and distance', () => {
    expect(targetText({ id: 'e1', class: '', dist: 0 })).toBe('e1');
    expect(targetText({ id: 'e1', class: 'tank', dist: 0.4 })).toBe('e1 · tank · 0u');
    expect(targetText({ id: 'e1', class: '', dist: 99.5 })).toBe('e1 · 100u');
  });

  it('is empty for an idle action and leaves FIRE out when not firing', () => {
    expect(actionText({})).toBe('');
    expect(actionText({ weapon: 'blaster', fire: false })).toBe('blaster');
    expect(actionText({ fire: true })).toBe('FIRE');
  });
});

describe('statCells', () => {
  it('falls back to the decision cost without totals and formats seconds', () => {
    const cells = Object.fromEntries(
      statCells({ ...base, latencyMs: 1500, costUsd: 2 } as DecisionView, null).map((c) => [c.label, c.value]),
    );
    expect(cells).toEqual({ latency: '1.50 s', cost: '$2.00', rate: '–', model: '–', 'k / d': '–', p50: '–' });
  });

  it('lists the cells in footer order', () => {
    expect(statCells(null, null).map((c) => c.label)).toEqual(['latency', 'cost', 'rate', 'model', 'k / d', 'p50']);
  });
});

describe('eventText', () => {
  const ev = (kind: string, data: Record<string, unknown> = {}, map?: string) =>
    eventText({ kind, sf: 0, data, ...(map === undefined ? {} : { map }) });

  it.each([
    ['kill', {}, undefined, 'killed an enemy'],
    ['kill', { target: 'e4' }, undefined, 'killed e4'],
    ['kill', { class: 42, target: 'e4' }, undefined, 'killed e4'],
    ['death', {}, undefined, 'died'],
    ['death', { cause: 'lava' }, undefined, 'died (lava)'],
    ['reload', {}, undefined, 'reload save0'],
    ['damage', { amount: 25 }, undefined, 'took 25 damage'],
    ['damage', { amount: '25' }, undefined, 'took ? damage'],
    ['damage', { amount: 0 }, undefined, 'took 0 damage'],
    ['level_start', { map: 'demo2' }, 'demo1', 'entered demo1'],
    ['level_start', { map: 'demo2' }, undefined, 'entered demo2'],
    ['level_start', {}, undefined, 'entered level'],
    ['level_end', { outcome: 'exit' }, 'demo3', 'left demo3 (exit)'],
    ['level_end', {}, undefined, 'left level'],
    ['stuck', {}, undefined, 'stuck: recovering'],
    ['stuck', { stage: 'jump' }, undefined, 'stuck: jump'],
    ['budget', {}, undefined, 'budget change'],
    ['budget', { reason: 'daily cap' }, undefined, 'budget: daily cap'],
    ['weapon_pickup', {}, undefined, 'weapon pickup'],
  ])('%s %j %s → %s', (kind, data, map, text) => {
    expect(ev(kind, data, map)).toBe(text);
  });
});
