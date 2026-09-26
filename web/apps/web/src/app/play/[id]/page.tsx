import type { Metadata } from 'next';
import { GameView } from '@/components/game/GameView';

export const metadata: Metadata = { title: 'Play' };

export default async function PlayPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  let gameId = id;
  try {
    gameId = decodeURIComponent(id);
  } catch {
    // malformed escape (e.g. a lone "%"): use the raw segment instead of failing the page render
  }
  return <GameView gameId={gameId} />;
}
