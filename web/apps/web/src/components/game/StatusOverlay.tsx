'use client';
// Loading plaque / connection status / errors above the canvas. Hidden while playing.
import { useEffect } from 'react';
import { useGameStore } from '@/game/store';
import { Q2Pic, Q2Text } from '../Q2Text';
import styles from './Game.module.css';

const PHASE_TEXT: Record<string, string> = {
  idle: 'Starting…',
  index: 'Fetching game data index…',
  engine: 'Initializing renderer…',
  joining: 'Joining game…',
  connecting: 'Connecting…',
  loading: 'Loading…',
};

export function StatusOverlay({ onRetry, onLeave }: { onRetry: () => void; onLeave: () => void }) {
  const phase = useGameStore((s) => s.phase);
  const loading = useGameStore((s) => s.loading);
  const fatal = useGameStore((s) => s.fatal);
  const notice = useGameStore((s) => s.notice);
  const mapName = useGameStore((s) => s.mapName);
  const downloaded = useGameStore((s) => s.downloadedBytes);
  const pointerLocked = useGameStore((s) => s.pointerLocked);
  const overlay = useGameStore((s) => s.overlay);
  const set = useGameStore((s) => s.set);

  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => set({ notice: null }), 8000);
    return () => clearTimeout(t);
  }, [notice, set]);

  if (fatal) {
    return (
      <div className={styles.center}>
        <div className="panel" role="alert" style={{ maxWidth: 520 }}>
          <Q2Text text="ERROR" scale={3} alt />
          <p className="error">{fatal}</p>
          <div className="row">
            <button className="btn" onClick={onLeave}>
              Server browser
            </button>
            <button className="btn primary" onClick={onRetry}>
              Retry
            </button>
          </div>
        </div>
      </div>
    );
  }

  const busy = phase !== 'active' && phase !== 'disconnected' && phase !== 'error';
  const progress = loading?.progress;
  return (
    <>
      {busy && (
        <div className={styles.loading} data-testid="loading-overlay">
          <Q2Pic name="loading" fallback="LOADING" scale={2} />
          <div className={styles.loadingText}>
            <Q2Text text={(loading?.mapname || mapName || '').toUpperCase()} scale={2} alt />
            <p className="mono">
              {PHASE_TEXT[phase] ?? phase}
              {loading?.stage ? ` ${loading.stage}` : ''}
            </p>
            {progress !== undefined && (
              <div className="progress" style={{ width: 280 }}>
                <div style={{ width: `${Math.round(progress * 100)}%` }} />
              </div>
            )}
            {downloaded > 0 && <p className="muted mono">{(downloaded / (1 << 20)).toFixed(1)} MiB downloaded</p>}
          </div>
        </div>
      )}
      {phase === 'active' && !pointerLocked && !overlay && (
        <div className={styles.hint}>
          <Q2Text text="CLICK TO PLAY - ESC FOR MENU" scale={1} />
        </div>
      )}
      {notice && (
        <div className={styles.notice} role="status">
          {notice}
        </div>
      )}
    </>
  );
}
