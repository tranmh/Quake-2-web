'use client';
import { useRouter } from 'next/navigation';
import { useCallback, useEffect, useState } from 'react';
import { CreateGameDialog } from '@/components/CreateGameDialog';
import { Q2Pic } from '@/components/Q2Text';
import { RequireLogin } from '@/components/RequireLogin';
import { api, errorMessage, type GameInfo } from '@/lib/api';
import { useSession } from '@/lib/session';

const MODE_LABEL: Record<string, string> = { sp: 'Single', coop: 'Coop', dm: 'Deathmatch', ctf: 'CTF' };

export default function ServersPage() {
  return (
    <main className="page">
      <Q2Pic name="m_banner_join_server" fallback="SERVERS" />
      <h1 className="visually-hidden">Server browser</h1>
      <RequireLogin>
        <ServerBrowser />
      </RequireLogin>
    </main>
  );
}

function ServerBrowser() {
  const router = useRouter();
  const user = useSession((s) => s.user);
  const [games, setGames] = useState<GameInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const reload = useCallback(async () => {
    try {
      setGames(await api.listGames());
      setError(null);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void reload();
    const t = setInterval(() => void reload(), 5000);
    return () => clearInterval(t);
  }, [reload]);

  return (
    <>
      <section className="panel">
        <div className="row">
          <h2 style={{ margin: 0 }}>Games</h2>
          <span className="spacer" />
          <button className="btn" onClick={() => void reload()}>
            Refresh
          </button>
          <button className="btn primary" onClick={() => setCreating(true)}>
            New game
          </button>
        </div>
        {error && <p className="error">{error}</p>}
        <table className="table" style={{ marginTop: 12 }}>
          <thead>
            <tr>
              <th>Name</th>
              <th>Mode</th>
              <th>Map</th>
              <th>Players</th>
              <th>Pakset</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {games?.map((g) => (
              <tr key={g.id}>
                <td>
                  {g.name} {!g.public && <span className="badge">private</span>}
                </td>
                <td>{MODE_LABEL[g.mode] ?? g.mode}</td>
                <td className="mono">{g.map}</td>
                <td>
                  {g.players}/{g.maxPlayers || '∞'}
                </td>
                <td className="mono muted">{g.pakset}</td>
                <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                  <button className="btn small primary" onClick={() => router.push(`/play/${encodeURIComponent(g.id)}`)}>
                    Join
                  </button>{' '}
                  {user && (g.ownerId === user.id || user.isAdmin) && (
                    <button
                      className="btn small danger"
                      onClick={async () => {
                        try {
                          await api.deleteGame(g.id);
                          void reload();
                        } catch (e) {
                          setError(errorMessage(e));
                        }
                      }}
                    >
                      Stop
                    </button>
                  )}
                </td>
              </tr>
            ))}
            {games && games.length === 0 && (
              <tr>
                <td colSpan={6} className="muted">
                  No games running. Start one with &ldquo;New game&rdquo;.
                </td>
              </tr>
            )}
            {!games && !error && (
              <tr>
                <td colSpan={6} className="muted">
                  Loading…
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>
      {creating && (
        <CreateGameDialog
          onClose={() => setCreating(false)}
          onCreated={(g) => router.push(`/play/${encodeURIComponent(g.id)}`)}
        />
      )}
    </>
  );
}
