import type { Metadata } from 'next';
import { GameView } from '@/components/game/GameView';

export const metadata: Metadata = { title: 'Play' };

export default async function PlayPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <GameView gameId={decodeURIComponent(id)} />;
}
