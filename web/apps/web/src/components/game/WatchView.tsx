'use client';
// The watch page: a bot's live first-person view (spectate relay) in one full-window canvas, the AI
// overlay fed by the decision side channel, a LIVE badge, and a "Run ended" panel once the run is over.
// Unlike GameView there is no menu: the viewer has no controls (GameSession source watch is passive).
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useEffect, useRef, useState } from 'react';
import { useAiStore } from '@/game/aiStore';
import { GameSession } from '@/game/session';
import { useGameStore } from '@/game/store';
import { api, type BotInfo } from '@/lib/api';
import { useSession } from '@/lib/session';
import { Q2Text } from '../Q2Text';
import { AiOverlay } from './AiOverlay';
import { StatusOverlay } from './StatusOverlay';
import styles from './Game.module.css';

const STATUS_TEXT: Record<string, string> = {
  finished: 'The run finished.',
  failed: 'The run failed.',
  stopped: 'The run was stopped.',
  gone: 'The bot is gone.',
};

export function WatchView({ botId }: { botId: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const router = useRouter();
  const { loaded, refresh } = useSession();
  const [bot, setBot] = useState<BotInfo | null>(null);
  const phase = useGameStore((s) => s.phase);
  const runStatus = useGameStore((s) => s.runStatus);
  const bye = useAiStore((s) => s.ended);
  const over = phase === 'ended' || bye !== null;

  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);

  // wait for the account state: an anonymous viewer of a public bot does not ask for server settings
  useEffect(() => {
    if (!loaded) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    useGameStore.getState().reset();
    useAiStore.getState().reset();
    const s = new GameSession({
      canvas,
      source: { kind: 'watch', botId },
      onExit: () => router.push('/bots'),
    });
    void s.start();
    return () => s.dispose();
  }, [botId, loaded, router]);

  // the bot's name for the badge, and its final status once the run is over
  useEffect(() => {
    let live = true;
    api.getBot(botId).then(
      (b) => live && setBot(b),
      () => {},
    );
    return () => {
      live = false;
    };
  }, [botId, over]);

  const status = runStatus ?? bye ?? bot?.status ?? '';
  return (
    <div className={styles.root}>
      <canvas
        ref={canvasRef}
        className={`${styles.canvas} ${styles.passive}`}
        tabIndex={-1}
        aria-label="Bot view"
        data-testid="game-canvas"
      />
      <StatusOverlay
        mode="watch"
        onRetry={() => location.reload()}
        onLeave={() => router.push('/bots')}
        leaveLabel="Bots"
      />
      <div className={styles.liveBadge} data-testid="live-badge">
        <span className={over ? styles.endedDot : styles.liveDot} aria-hidden="true" />
        {over ? 'ENDED' : 'LIVE'} · {bot?.name || botId}
      </div>
      <AiOverlay />
      {over && (
        <div className={styles.ended}>
          <div
            className="panel"
            role="dialog"
            aria-label="Run ended"
            data-testid="run-ended"
            style={{ maxWidth: 480 }}
          >
            <Q2Text text="RUN ENDED" scale={3} alt />
            <p>
              {STATUS_TEXT[status] ?? `The run ended${status ? ` (${status})` : ''}.`}
              {bot?.reason ? <span className="muted"> {bot.reason}</span> : null}
            </p>
            <div className="row">
              <Link className="btn" href="/bots">
                All bots
              </Link>
              <span className="spacer" />
              <Link className="btn primary" href={`/bots/${encodeURIComponent(botId)}`}>
                Summary &amp; replay
              </Link>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
