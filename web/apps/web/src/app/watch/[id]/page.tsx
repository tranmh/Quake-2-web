import type { Metadata } from 'next';
import { WatchView } from '@/components/game/WatchView';

export const metadata: Metadata = { title: 'Watch' };

export default async function WatchPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  let botId = id;
  try {
    botId = decodeURIComponent(id);
  } catch {
    // malformed escape (e.g. a lone "%"): use the raw segment instead of failing the page render
  }
  return <WatchView botId={botId} />;
}
