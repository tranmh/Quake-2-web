'use client';
// Bot browser: the runs this account can see (public ones, its own, all for an admin) with their live
// stats, and the form that starts a bot on the demo campaign (docs/plans/0002-ai-agent.md H.5/H.6).
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Q2Text } from '@/components/Q2Text';
import { ApiError, api, botIsLive, errorMessage, type BotBackend, type BotInfo, type User } from '@/lib/api';
import { CAMPAIGN, SKILL_NAMES, formatPercent, formatUsd } from '@/lib/bots';
import { useSession } from '@/lib/session';

export default function BotsPage() {
  return (
    <main className="page">
      <Q2Text text="BOTS" scale={3} alt />
      <h1 className="visually-hidden">Bots</h1>
      <BotBrowser />
    </main>
  );
}

const STATUS_BADGE: Record<string, string> = {
  starting: 'badge warn',
  running: 'badge ok',
  finished: 'badge',
  failed: 'badge bad',
  stopped: 'badge',
};

const BACKENDS: { id: BotBackend; label: string; admin?: boolean }[] = [
  { id: 'scripted', label: 'Scripted (baseline)' },
  { id: 'mock', label: 'Mock Jev (simulated model)' },
  { id: 'constant', label: 'Constant (ablation)' },
  { id: 'random', label: 'Random (ablation)' },
  { id: 'jev', label: 'Jev (paid API, admin only)', admin: true },
];

/** "stop after" choices: the campaign's prefixes; the last one is the whole campaign (maps omitted). */
const ROUTES = CAMPAIGN.map((_, i) => {
  const maps = CAMPAIGN.slice(0, i + 1);
  const label = maps.map((m, j) => (j === 3 ? `${m} (2nd visit)` : m)).join(' → ');
  return { value: String(i + 1), label: i === CAMPAIGN.length - 1 ? `Whole campaign: ${label}` : label };
});

function startedText(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '';
  return new Date(t).toLocaleString(undefined, { dateStyle: 'short', timeStyle: 'short' });
}

function BotBrowser() {
  const router = useRouter();
  const { user, loaded, refresh } = useSession();
  const [bots, setBots] = useState<BotInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [disabled, setDisabled] = useState(false);

  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);

  const reload = useCallback(async () => {
    try {
      setBots(await api.listBots());
      setError(null);
      setDisabled(false);
    } catch (e) {
      if (e instanceof ApiError && e.status === 503 && e.code === 'bots_disabled') setDisabled(true);
      else setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    if (!loaded) return;
    void reload();
    const t = setInterval(() => void reload(), 3000);
    return () => clearInterval(t);
  }, [loaded, reload, user]);

  if (disabled) {
    return (
      <section className="panel">
        <p>Bots are not enabled on this server (Q2_BOTS_ENABLED).</p>
      </section>
    );
  }

  const stop = async (b: BotInfo) => {
    try {
      await api.stopBot(b.id);
      void reload();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <>
      <section className="panel">
        <div className="row">
          <h2 style={{ margin: 0 }}>Runs</h2>
          <span className="spacer" />
          <button className="btn" onClick={() => void reload()}>
            Refresh
          </button>
        </div>
        {error && <p className="error">{error}</p>}
        <div style={{ overflowX: 'auto' }}>
          <table className="table" style={{ marginTop: 12 }} data-testid="bot-list">
            <thead>
              <tr>
                <th>Name</th>
                <th>Status</th>
                <th>Backend</th>
                <th>Level</th>
                <th>K / D</th>
                <th>Decisions</th>
                <th>Model</th>
                <th>Cost</th>
                <th>Viewers</th>
                <th>Started</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {bots?.map((b) => (
                <BotRow
                  key={b.id}
                  bot={b}
                  user={user}
                  onWatch={() => router.push(`/watch/${encodeURIComponent(b.id)}`)}
                  onStop={() => void stop(b)}
                />
              ))}
              {bots && bots.length === 0 && (
                <tr>
                  <td colSpan={11} className="muted">
                    No bots yet. Start one below.
                  </td>
                </tr>
              )}
              {!bots && !error && (
                <tr>
                  <td colSpan={11} className="muted">
                    Loading…
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </section>
      {user ? (
        <StartBotForm user={user} onStarted={(b) => router.push(`/watch/${encodeURIComponent(b.id)}`)} />
      ) : loaded ? (
        <section className="panel">
          <p>
            <Link href="/login?next=/bots">Log in</Link> to start a bot.
          </p>
        </section>
      ) : null}
    </>
  );
}

function BotRow({
  bot: b,
  user,
  onWatch,
  onStop,
}: {
  bot: BotInfo;
  user: User | null;
  onWatch: () => void;
  onStop: () => void;
}) {
  const live = botIsLive(b.status);
  const mine = !!user && (b.ownerId === user.id || user.isAdmin);
  return (
    <tr data-testid="bot-row" data-bot={b.id}>
      <td>
        <Link href={`/bots/${encodeURIComponent(b.id)}`}>{b.name || b.id}</Link>{' '}
        {!b.public && <span className="badge">private</span>}
      </td>
      <td>
        <span className={STATUS_BADGE[b.status] ?? 'badge'} title={b.reason}>
          {b.status}
        </span>
      </td>
      <td className="mono">
        {b.backend}
        {b.model ? <span className="muted"> {b.model}</span> : null}
      </td>
      <td className="mono">
        {b.level ? `${b.level.map}${b.level.visit > 0 ? ` (visit ${b.level.visit + 1})` : ''}` : '–'}
      </td>
      <td className="mono">{b.live ? `${b.live.kills} / ${b.live.deaths}` : '–'}</td>
      <td className="mono">{b.live ? b.live.decisions : '–'}</td>
      <td className="mono">{b.live ? formatPercent(b.live.modelShare) : '–'}</td>
      <td className="mono">{b.live ? formatUsd(b.live.costUsd) : '–'}</td>
      <td className="mono">{b.viewers}</td>
      <td className="muted">{startedText(b.startedAt)}</td>
      <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
        {live && (
          <button className="btn small primary" onClick={onWatch}>
            Watch
          </button>
        )}{' '}
        <Link className="btn small" href={`/bots/${encodeURIComponent(b.id)}`}>
          Summary
        </Link>{' '}
        {live && mine && (
          <button className="btn small danger" onClick={onStop}>
            Stop
          </button>
        )}
      </td>
    </tr>
  );
}

function StartBotForm({ user, onStarted }: { user: User; onStarted: (b: BotInfo) => void }) {
  const [backend, setBackend] = useState<BotBackend>('scripted');
  const [route, setRoute] = useState(String(CAMPAIGN.length));
  const [skill, setSkill] = useState(1);
  const [name, setName] = useState('');
  const [simLatency, setSimLatency] = useState('');
  const [isPublic, setPublic] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const modelBackend = backend === 'mock' || backend === 'jev';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const n = Number(route);
    try {
      const b = await api.createBot({
        backend,
        skill,
        ...(name.trim() ? { name: name.trim() } : {}),
        ...(n < CAMPAIGN.length ? { maps: CAMPAIGN.slice(0, n) } : {}),
        ...(modelBackend && simLatency.trim() ? { simLatency: simLatency.trim() } : {}),
        ...(isPublic ? { public: true } : {}),
      });
      onStarted(b);
    } catch (err) {
      setError(startError(err));
      setBusy(false);
    }
  };

  return (
    <form className="panel" onSubmit={submit} style={{ display: 'grid', gap: 14 }} aria-label="Start a bot">
      <h2 style={{ margin: 0 }}>Start a bot</h2>
      <p className="muted" style={{ margin: 0 }}>
        The bot plays the demo campaign single-player at the chosen skill; watch it live, then replay its
        recorded demos with the decision trace.
      </p>
      <div className="grid2">
        <label className="field">
          Backend
          <select name="backend" value={backend} onChange={(e) => setBackend(e.target.value as BotBackend)}>
            {BACKENDS.map((b) => (
              <option key={b.id} value={b.id} disabled={b.admin && !user.isAdmin}>
                {b.label}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Maps
          <select name="maps" value={route} onChange={(e) => setRoute(e.target.value)}>
            {ROUTES.map((r) => (
              <option key={r.value} value={r.value}>
                {r.label}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Skill
          <select name="skill" value={skill} onChange={(e) => setSkill(Number(e.target.value))}>
            {SKILL_NAMES.map((s, i) => (
              <option key={s} value={i}>
                {s}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Name
          <input
            type="text"
            name="name"
            value={name}
            maxLength={64}
            onChange={(e) => setName(e.target.value)}
            placeholder="(generated)"
          />
        </label>
        {modelBackend && (
          <label className="field">
            Simulated latency
            <input
              type="text"
              name="simLatency"
              value={simLatency}
              maxLength={64}
              onChange={(e) => setSimLatency(e.target.value)}
              placeholder="212ms"
            />
          </label>
        )}
      </div>
      <label className="check">
        <input type="checkbox" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} /> List
        publicly (anyone can watch)
      </label>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="row">
        <span className="spacer" />
        <button type="submit" className="btn primary" disabled={busy}>
          {busy ? 'Starting…' : 'Start bot'}
        </button>
      </div>
    </form>
  );
}

function startError(e: unknown): string {
  if (!(e instanceof ApiError)) return errorMessage(e);
  switch (e.status) {
    case 403:
      return `Not allowed: ${e.message}`;
    case 429:
      return `Too many bots running: ${e.message}`;
    case 503:
      return `Bots are unavailable: ${e.message}`;
    default:
      return e.message;
  }
}
