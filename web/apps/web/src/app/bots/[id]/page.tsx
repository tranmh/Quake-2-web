import type { Metadata } from 'next';
import { BotDetail } from '@/components/bots/BotDetail';

export const metadata: Metadata = { title: 'Bot run' };

export default async function BotPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  let botId = id;
  try {
    botId = decodeURIComponent(id);
  } catch {
    // malformed escape (e.g. a lone "%"): use the raw segment instead of failing the page render
  }
  return (
    <main className="page">
      <BotDetail botId={botId} />
    </main>
  );
}
