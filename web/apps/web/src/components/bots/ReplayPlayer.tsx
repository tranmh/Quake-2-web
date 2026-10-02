'use client';
// Replay of a recorded run: the episode's NN-<map>.dm2 demos played one after another by the unchanged
// client (engine.playDemo), with the AI overlay rebuilt from the decision trace. Level picker, pause and
// speed (timescale) below the view.
import { useEffect, useRef } from 'react';
import { AiOverlay } from '@/components/game/AiOverlay';
import { StatusOverlay } from '@/components/game/StatusOverlay';
import { useAiStore } from '@/game/aiStore';
import { GameSession } from '@/game/session';
import { useGameStore } from '@/game/store';
import styles from './Bots.module.css';

const SPEEDS = [0.5, 1, 2, 4];

export function ReplayPlayer({
  botId,
  episode,
  level,
  onClose,
}: {
  botId: string;
  episode: number;
  /** level attempt index to start at */
  level: number;
  onClose: () => void;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const sessionRef = useRef<GameSession | null>(null);
  const replay = useGameStore((s) => s.replay);
  const phase = useGameStore((s) => s.phase);
  const mapName = useGameStore((s) => s.mapName);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    useGameStore.getState().reset();
    useAiStore.getState().reset();
    const s = new GameSession({ canvas, source: { kind: 'replay', botId, episode, level }, onExit: onClose });
    sessionRef.current = s;
    void s.start();
    return () => {
      s.dispose();
      if (sessionRef.current === s) sessionRef.current = null;
    };
    // `level` and `onClose` only matter when the player mounts; later level picks go through playLevel
  }, [botId, episode]);

  const current = replay?.current ?? -1;
  const levels = replay?.levels ?? [];
  const play = (i: number) => void sessionRef.current?.playLevel(i);

  return (
    <div data-testid="replay-player">
      <div className={styles.player}>
        <canvas
          ref={canvasRef}
          className={styles.playerCanvas}
          tabIndex={-1}
          aria-label="Replay view"
          data-testid="game-canvas"
        />
        <StatusOverlay mode="replay" onRetry={() => location.reload()} onLeave={onClose} leaveLabel="Close" />
        <AiOverlay />
      </div>
      <div className={styles.controls}>
        <button
          className="btn small"
          onClick={() => play(current - 1)}
          disabled={current <= 0}
          aria-label="Previous level"
        >
          ◂ prev
        </button>
        <button
          className="btn small"
          onClick={() => sessionRef.current?.setReplayPaused(!replay?.paused)}
          disabled={!replay || replay.finished}
        >
          {replay?.paused ? 'Play' : 'Pause'}
        </button>
        <button
          className="btn small"
          onClick={() => play(current + 1)}
          disabled={current < 0 || current + 1 >= levels.length}
          aria-label="Next level"
        >
          next ▸
        </button>
        <label className="check">
          Level
          <select
            name="level"
            value={current >= 0 ? String(current) : ''}
            onChange={(e) => play(Number(e.target.value))}
            disabled={!levels.length}
          >
            {levels.map((l, i) => (
              <option key={l.name} value={i}>
                {String(l.attempt).padStart(2, '0')} · {l.map}
              </option>
            ))}
          </select>
        </label>
        <label className="check">
          Speed
          <select
            name="speed"
            value={String(replay?.speed ?? 1)}
            onChange={(e) => sessionRef.current?.setReplaySpeed(Number(e.target.value))}
          >
            {SPEEDS.map((x) => (
              <option key={x} value={x}>
                {x}×
              </option>
            ))}
          </select>
        </label>
        <span className="spacer" />
        <span className="muted mono">
          {current >= 0 ? `${current + 1}/${levels.length} · ${mapName}` : ''}
          {replay?.finished ? ' · replay finished' : phase === 'error' ? ' · error' : ''}
        </span>
        <button className="btn small" onClick={onClose}>
          Close
        </button>
      </div>
    </div>
  );
}
