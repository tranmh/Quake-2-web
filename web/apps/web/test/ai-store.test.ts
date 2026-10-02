// aiStore: the overlay shows the decision the frame on screen was played with (newest with sf ≤ the
// video's server frame, within the current level attempt), keeps the latest answer of every question,
// and bounds its history.
import { beforeEach, describe, expect, it } from 'vitest';
import {
  HISTORY_LIMIT,
  mergeQuestions,
  useAiStore,
  visibleDecision,
  type DecisionView,
} from '@/game/aiStore';
import type { DecisionQuestion } from '@/lib/api';

function decision(sf: number, extra: Partial<DecisionView> = {}): DecisionView {
  return {
    sf,
    lvl: 0,
    map: 'demo1',
    mode: 'objective',
    objective: '',
    target: null,
    questions: [],
    provenance: {},
    fallback: {},
    action: {},
    latencyMs: null,
    costUsd: null,
    ...extra,
  };
}

const q = (id: string, chosen: string): DecisionQuestion => ({
  id,
  chosen,
  options: [{ label: chosen, p: 1 }],
});

beforeEach(() => useAiStore.getState().reset());

describe('visibleDecision', () => {
  it('picks the newest decision at or before the video frame', () => {
    const s = useAiStore.getState();
    for (const sf of [100, 103, 106, 109]) s.pushDecision(decision(sf, { mode: `m${sf}` }));
    const at = (sf: number) => {
      useAiStore.getState().setServerFrame(sf);
      return visibleDecision(useAiStore.getState())?.mode;
    };
    expect(at(106)).toBe('m106');
    expect(at(107)).toBe('m106');
    expect(at(105)).toBe('m103');
    expect(at(500)).toBe('m109');
    // the video is behind every decision kept: show the latest instead of nothing
    expect(at(50)).toBe('m109');
  });

  it('shows the latest decision before the first frame', () => {
    useAiStore.getState().pushDecision(decision(40, { mode: 'fight' }));
    expect(visibleDecision(useAiStore.getState())?.mode).toBe('fight');
  });

  it('never aligns with decisions of an earlier level attempt', () => {
    const s = useAiStore.getState();
    s.pushDecision(decision(900, { lvl: 0, mode: 'old' }));
    s.pushDecision(decision(20, { lvl: 1, mode: 'reloaded' }));
    s.pushDecision(decision(30, { lvl: 1, mode: 'newer' }));
    // the video still shows frame 25 of the new attempt
    useAiStore.getState().setServerFrame(25);
    expect(visibleDecision(useAiStore.getState())?.mode).toBe('reloaded');
    // frame 10 of the new attempt: no new-attempt decision yet; frame 900 of the old one must not show
    useAiStore.getState().setServerFrame(10);
    expect(visibleDecision(useAiStore.getState())?.mode).toBe('newer');
  });

  it('treats server frames that start over as a new attempt (a reload keeps the level)', () => {
    const s = useAiStore.getState();
    s.pushDecision(decision(12, { mode: 'old-early' }));
    s.pushDecision(decision(600, { mode: 'old-late' }));
    s.pushDecision(decision(20, { mode: 'reloaded' }));
    s.pushDecision(decision(30, { mode: 'newer' }));
    useAiStore.getState().setServerFrame(25);
    expect(visibleDecision(useAiStore.getState())?.mode).toBe('reloaded');
    // frame 15 of the reloaded attempt: the old attempt's frame 12 is another frame
    useAiStore.getState().setServerFrame(15);
    expect(visibleDecision(useAiStore.getState())?.mode).toBe('newer');
  });
});

describe('pushDecision', () => {
  it('bounds the history', () => {
    const s = useAiStore.getState();
    for (let i = 1; i <= HISTORY_LIMIT + 25; i++) s.pushDecision(decision(i));
    const h = useAiStore.getState().history;
    expect(h).toHaveLength(HISTORY_LIMIT);
    expect(h[0]!.sf).toBe(26);
    expect(useAiStore.getState().latest?.sf).toBe(HISTORY_LIMIT + 25);
  });

  it("keeps each question's latest answer within a level attempt", () => {
    const s = useAiStore.getState();
    s.pushDecision(decision(1, { questions: [q('mode', 'fight'), q('danger', 'low')] }));
    s.pushDecision(decision(2, { questions: [q('target', 'e7'), q('movement', 'hold')] }));
    s.pushDecision(decision(3, { questions: [q('mode', 'retreat')] }));
    expect(useAiStore.getState().latest!.questions.map((x) => `${x.id}=${x.chosen}`)).toEqual([
      'mode=retreat',
      'target=e7',
      'movement=hold',
      'danger=low',
    ]);
    // a new attempt forgets the previous level's answers
    s.pushDecision(decision(4, { lvl: 1, questions: [q('target', 'e1')] }));
    expect(useAiStore.getState().latest!.questions.map((x) => x.id)).toEqual(['target']);
  });
});

describe('mergeQuestions', () => {
  it('orders by the trace field order, unknown questions last', () => {
    const out = mergeQuestions(
      [q('zeta', 'a'), q('danger', 'x')],
      [q('weapon', 'shotgun'), q('mode', 'fight')],
    );
    expect(out.map((x) => x.id)).toEqual(['mode', 'weapon', 'danger', 'zeta']);
  });
});

describe('store actions', () => {
  it('counts gaps, records bye and caps events', () => {
    const s = useAiStore.getState();
    s.gap(3);
    s.gap(2);
    for (let i = 0; i < 30; i++) s.pushEvent({ kind: 'kill', sf: i, data: {} });
    s.bye('finished');
    const st = useAiStore.getState();
    expect(st.dropped).toBe(5);
    expect(st.events).toHaveLength(20);
    expect(st.events[0]!.sf).toBe(29);
    expect(st.ended).toBe('finished');
    expect(st.status).toBe('ended');
  });
});
