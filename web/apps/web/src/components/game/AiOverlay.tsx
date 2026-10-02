'use client';
// The AI overlay (docs/plans/0002-ai-agent.md H.6): what the bot decided for the frame on screen. Mode,
// objective / route step, target, one probability bar per option of every question (the chosen option
// highlighted), confidence, where each acted-on field came from (model / scripted / stale / reflex), a red
// FALLBACK badge when a field fell back from the model, latency, cost, decision rate and model share.
import { useEffect, useState } from 'react';
import {
  useAiStore,
  visibleDecision,
  type AiEventView,
  type AiFeedStatus,
  type AiTotals,
  type DecisionView,
} from '@/game/aiStore';
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
import type { FeedHello } from '@/lib/api';
import { Q2Text } from '../Q2Text';
import styles from './AiOverlay.module.css';

const OPEN_KEY = 'q2.aiOverlay';
/** options shown per question (the chosen one and the most probable others) */
const MAX_OPTIONS = 4;

const STATUS_TEXT: Partial<Record<AiFeedStatus, string>> = {
  idle: 'waiting for the decision feed…',
  connecting: 'connecting to the decision feed…',
  reconnecting: 'decision feed dropped: reconnecting…',
  loading: 'loading the decision trace…',
  ended: 'run ended',
};

export interface AiOverlayPanelProps {
  decision: DecisionView | null;
  hello: FeedHello | null;
  totals: AiTotals | null;
  status: AiFeedStatus;
  error?: string | null;
  events?: AiEventView[];
  dropped?: number;
  open: boolean;
  onToggle?: () => void;
}

/** The panel itself (pure: everything comes in through props). */
export function AiOverlayPanel({
  decision: d,
  hello,
  totals,
  status,
  error,
  events = [],
  dropped = 0,
  open,
  onToggle,
}: AiOverlayPanelProps) {
  const backend = hello ? [hello.backend, hello.model].filter(Boolean).join(' · ') : '';
  if (!open) {
    return (
      <aside
        className={`${styles.panel} ${styles.collapsed}`}
        data-testid="ai-overlay"
        aria-label="AI decisions"
      >
        <button type="button" className={styles.toggle} onClick={onToggle} aria-expanded={false}>
          AI ▸
        </button>
      </aside>
    );
  }
  const fallbacks = fallbackFields(d, isModelBackend(hello?.backend));
  const statusText = status === 'error' ? error || 'decision feed failed' : STATUS_TEXT[status];
  return (
    <aside className={styles.panel} data-testid="ai-overlay" aria-label="AI decisions">
      <div className={styles.header}>
        <span className={styles.title}>AI{backend ? ` · ${backend}` : ''}</span>
        {fallbacks.length > 0 && (
          <span
            className={`${styles.badge} ${styles.fallback}`}
            data-testid="ai-fallback"
            title={fallbacks.map((f) => `${fieldLabel(f.field)}: ${f.reason}`).join('\n')}
          >
            FALLBACK
          </span>
        )}
        <button type="button" className={styles.toggle} onClick={onToggle} aria-expanded={true}>
          hide
        </button>
      </div>

      {d ? (
        <>
          <div className={styles.mode} data-testid="ai-mode" data-mode={d.mode}>
            <Q2Text text={(d.mode || '?').toUpperCase()} scale={2} alt />
          </div>
          {d.objective && (
            <div className={styles.line}>
              objective <b>{d.objective}</b>
            </div>
          )}
          <div className={styles.line}>
            target <b data-testid="ai-target">{targetText(d.target)}</b>
          </div>
          {actionText(d.action) && (
            <div className={styles.line}>
              action <b>{actionText(d.action)}</b>
            </div>
          )}

          <div className={`${styles.section} ${styles.badges}`} data-testid="ai-provenance">
            {provenanceBadges(d).map((b) => (
              <span
                key={b.field}
                className={`${styles.badge} ${styles[b.tone] ?? ''}`}
                data-source={b.source}
                title={`${b.label}: ${b.source}${d.fallback[b.field] ? ` (${d.fallback[b.field]})` : ''}`}
              >
                {b.label} {b.source}
              </span>
            ))}
          </div>
        </>
      ) : (
        <div className={styles.line} style={{ marginTop: 8 }}>
          no decision yet
        </div>
      )}

      <div className={`${styles.section} ${styles.stats}`}>
        {statCells(d, totals).map((c) => (
          <div key={c.label} className={styles.stat}>
            <span>{c.label}</span>
            <span>{c.value}</span>
          </div>
        ))}
      </div>

      {d && d.questions.length > 0 && (
        <div className={`${styles.section} ${styles.questions}`}>
          {questionRows(d, MAX_OPTIONS).map((q) => (
            <div key={q.id} className={styles.question} data-testid="ai-question" data-question={q.id}>
              <div className={styles.qhead}>
                <span>{q.label}</span>
                <span>{q.confidence}</span>
              </div>
              {q.options.map((o) => (
                <div
                  key={o.label}
                  className={`${styles.option} ${o.chosen ? styles.chosen : ''}`}
                  data-testid="prob-bar"
                  data-chosen={o.chosen ? 'true' : undefined}
                  title={`${o.label}: ${o.pct}`}
                >
                  <div className={styles.fill} style={{ width: `${(o.p * 100).toFixed(1)}%` }} />
                  <span>{o.label}</span>
                  <span>{o.pct}</span>
                </div>
              ))}
            </div>
          ))}
        </div>
      )}

      {events.length > 0 && (
        <ul className={`${styles.section} ${styles.events}`} aria-label="Recent events">
          {events.slice(0, 3).map((e, i) => (
            <li key={`${e.kind}-${e.sf}-${i}`}>{eventText(e)}</li>
          ))}
        </ul>
      )}
      {statusText && status !== 'live' && status !== 'replay' && (
        <div className={status === 'error' ? styles.statusError : styles.status} role="status">
          {statusText}
        </div>
      )}
      {dropped > 0 && <div className={styles.status}>{dropped} messages dropped by the server</div>}
    </aside>
  );
}

/** The overlay bound to the AI store (aligned with the frame on screen); the toggle is remembered. */
export function AiOverlay() {
  const decision = useAiStore(visibleDecision);
  const hello = useAiStore((s) => s.hello);
  const totals = useAiStore((s) => s.totals);
  const status = useAiStore((s) => s.status);
  const error = useAiStore((s) => s.error);
  const events = useAiStore((s) => s.events);
  const dropped = useAiStore((s) => s.dropped);
  const [open, setOpen] = useState(true);

  useEffect(() => {
    try {
      if (localStorage.getItem(OPEN_KEY) === '0') setOpen(false);
    } catch {
      // storage unavailable
    }
  }, []);

  const toggle = () => {
    setOpen((o) => {
      try {
        localStorage.setItem(OPEN_KEY, o ? '0' : '1');
      } catch {
        // storage unavailable
      }
      return !o;
    });
  };

  return (
    <AiOverlayPanel
      decision={decision}
      hello={hello}
      totals={totals}
      status={status}
      error={error}
      events={events}
      dropped={dropped}
      open={open}
      onToggle={toggle}
    />
  );
}
