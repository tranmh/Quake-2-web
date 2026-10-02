'use client';
// The game page: one full-window canvas mounted once, the engine in a ref (GameSession), and React
// overlays driven by the small Zustand store (loading plaque, errors, in-game menu).
import { useRouter } from 'next/navigation';
import { useEffect, useRef } from 'react';
import { GameSession } from '@/game/session';
import { useGameStore } from '@/game/store';
import { useSession } from '@/lib/session';
import { GameOverlay } from './GameOverlay';
import { StatusOverlay } from './StatusOverlay';
import styles from './Game.module.css';

export function GameView({ gameId }: { gameId: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const sessionRef = useRef<GameSession | null>(null);
  const router = useRouter();
  const { user, loaded, refresh } = useSession();

  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);

  useEffect(() => {
    if (!loaded || !user) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    useGameStore.getState().reset();
    const s = new GameSession({
      canvas,
      source: { kind: 'play', gameId },
      onExit: () => router.push('/servers'),
    });
    sessionRef.current = s;
    void s.start();
    return () => {
      s.dispose();
      if (sessionRef.current === s) sessionRef.current = null;
    };
  }, [gameId, loaded, user, router]);

  return (
    <div className={styles.root}>
      <canvas
        ref={canvasRef}
        className={styles.canvas}
        tabIndex={0}
        aria-label="Quake II game view"
        data-testid="game-canvas"
      />
      {loaded && !user ? (
        <div className={styles.center}>
          <div className="panel">
            <p>Log in to join this game.</p>
            <button
              className="btn primary"
              onClick={() => router.push(`/login?next=/play/${encodeURIComponent(gameId)}`)}
            >
              Log in
            </button>
          </div>
        </div>
      ) : (
        <>
          <StatusOverlay
            mode="play"
            onRetry={() => location.reload()}
            onLeave={() => router.push('/servers')}
          />
          <GameOverlay session={sessionRef} />
        </>
      )}
    </div>
  );
}
