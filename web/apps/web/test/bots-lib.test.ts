// Run artifact layout and summary helpers of the bot pages.
import { describe, expect, it } from 'vitest';
import type { BotArtifact } from '@/lib/api';
import {
  artifactEpisodes,
  demoArtifacts,
  fieldShares,
  formatGameTime,
  formatUsd,
  traceArtifactName,
  type TickStats,
} from '@/lib/bots';

const artifacts: BotArtifact[] = [
  { name: 'run.json', size: 9000, kind: 'run' },
  { name: 'ep-000/episode.json', size: 5000, kind: 'episode' },
  { name: 'ep-000/trace.jsonl.gz', size: 2_000_000, kind: 'trace' },
  { name: 'ep-000/demos/10-demo3.dm2', size: 3, kind: 'demo' },
  { name: 'ep-000/demos/02-demo2.dm2', size: 2, kind: 'demo' },
  { name: 'ep-000/demos/00-demo1.dm2', size: 1, kind: 'demo' },
  { name: 'ep-001/demos/00-demo1.dm2', size: 4, kind: 'demo' },
  { name: 'ep-000/demos/notes.txt', size: 1, kind: 'demo' },
];

describe('run artifacts', () => {
  it('lists an episode’s demos in attempt order', () => {
    expect(demoArtifacts(artifacts).map((d) => `${d.attempt}:${d.map}`)).toEqual([
      '0:demo1',
      '2:demo2',
      '10:demo3',
    ]);
    expect(demoArtifacts(artifacts, 1).map((d) => d.name)).toEqual(['ep-001/demos/00-demo1.dm2']);
    expect(demoArtifacts(artifacts, 2)).toEqual([]);
  });

  it('accepts every demo name the server serves', () => {
    const demo = (name: string): BotArtifact => ({ name, size: 1, kind: 'demo' });
    const names = [
      'ep-000/demos/03-base-1.dm2',
      'ep-000/demos/04-q2dm1.v2.dm2',
      'ep-000/demos/05-_x.dm2',
      'ep-000/demos/1000-demo1.dm2',
      // not served (runner artifactName): a name starting with '.', other characters, 5 digits
      'ep-000/demos/06-.hidden.dm2',
      'ep-000/demos/07-a b.dm2',
      'ep-000/demos/10000-demo1.dm2',
    ];
    expect(demoArtifacts(names.map(demo)).map((d) => `${d.attempt}:${d.map}`)).toEqual([
      '3:base-1',
      '4:q2dm1.v2',
      '5:_x',
      '1000:demo1',
    ]);
  });

  it('finds the episodes and names their traces', () => {
    expect(artifactEpisodes(artifacts)).toEqual([0, 1]);
    expect(traceArtifactName(0)).toBe('ep-000/trace.jsonl.gz');
    expect(traceArtifactName(12)).toBe('ep-012/trace.jsonl.gz');
  });
});

describe('fieldShares', () => {
  it('turns tick counts into shares in the trace field order', () => {
    const ticks: TickStats = {
      ticks: 10,
      gate_model_share: 0.5,
      fields: {
        weapon: {
          default: 0,
          model: 10,
          scripted: 0,
          stale: 0,
          reflex: 0,
          decided: 10,
          model_share: 1,
          stale_share: 0,
        },
        extra: {
          default: 10,
          model: 0,
          scripted: 0,
          stale: 0,
          reflex: 0,
          decided: 0,
          model_share: 0,
          stale_share: 0,
        },
        mode: {
          default: 0,
          model: 5,
          scripted: 2,
          stale: 1,
          reflex: 2,
          decided: 10,
          model_share: 0.5,
          stale_share: 0.1,
        },
      },
    };
    const s = fieldShares(ticks);
    expect(s.map((f) => f.name)).toEqual(['mode', 'weapon', 'extra']);
    expect(s[0]).toMatchObject({
      ticks: 10,
      model: 0.5,
      scripted: 0.2,
      stale: 0.1,
      reflex: 0.2,
      default: 0,
      modelShare: 0.5,
    });
    expect(fieldShares(undefined)).toEqual([]);
  });
});

describe('formatting', () => {
  it('formats game time and dollars', () => {
    expect(formatGameTime(80_300)).toBe('1:20.3');
    expect(formatGameTime(3_853_900)).toBe('1:04:13');
    expect(formatGameTime(-1)).toBe('–');
    expect(formatUsd(0)).toBe('$0');
    expect(formatUsd(0.016)).toBe('$0.016');
    expect(formatUsd(0.000021756)).toBe('$0.000022');
    expect(formatUsd(12.345)).toBe('$12.35');
  });
});
