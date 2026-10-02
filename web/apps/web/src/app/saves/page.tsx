'use client';
import { useRouter } from 'next/navigation';
import { useCallback, useEffect, useState } from 'react';
import { CreateGameDialog } from '@/components/CreateGameDialog';
import { Q2Pic } from '@/components/Q2Text';
import { RequireLogin } from '@/components/RequireLogin';
import { api, errorMessage, type GameMode, type SaveInfo } from '@/lib/api';

export default function SavesPage() {
  return (
    <main className="page">
      <Q2Pic name="m_banner_load_game" fallback="SAVED GAMES" />
      <h1 className="visually-hidden">Saved games</h1>
      <RequireLogin>
        <SaveList />
      </RequireLogin>
    </main>
  );
}

/** "base1$base2" style mapcmd → map name (strips spawnpoint and cinematic prefixes). */
function mapOf(mapcmd: string): string {
  let m = mapcmd.split('+').pop() ?? mapcmd;
  if (m.includes('$')) m = m.split('$')[0]!;
  return m.replace(/^\*/, '').replace(/\.(cin|pcx|dm2)$/, '') || 'demo1';
}

function SaveList() {
  const router = useRouter();
  const [saves, setSaves] = useState<SaveInfo[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState<SaveInfo | null>(null);

  const reload = useCallback(async () => {
    try {
      setSaves(await api.listSaves());
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);

  return (
    <section className="panel">
      <p className="muted">
        Saves are stored on the server per account. Save in game from the menu (Esc → Save) or with the
        console command <span className="mono">save &lt;slot&gt;</span>.
      </p>
      {error && <p className="error">{error}</p>}
      <table className="table">
        <thead>
          <tr>
            <th>Slot</th>
            <th>Description</th>
            <th>Map</th>
            <th>Saved</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {saves?.map((s) => (
            <tr key={s.slot}>
              <td className="mono">{s.slot}</td>
              <td>{s.comment || <span className="muted">(no description)</span>}</td>
              <td className="mono">{mapOf(s.mapcmd)}</td>
              <td className="muted">{new Date(s.updatedAt).toLocaleString()}</td>
              <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                <button className="btn small primary" onClick={() => setLoading(s)}>
                  Load
                </button>{' '}
                <a className="btn small" href={api.saveDataUrl(s.slot)} download>
                  Download
                </a>{' '}
                <button
                  className="btn small danger"
                  onClick={async () => {
                    if (!confirm(`Delete save "${s.slot}"?`)) return;
                    try {
                      await api.deleteSave(s.slot);
                      void reload();
                    } catch (e) {
                      setError(errorMessage(e));
                    }
                  }}
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
          {saves && saves.length === 0 && (
            <tr>
              <td colSpan={5} className="muted">
                No saved games.
              </td>
            </tr>
          )}
        </tbody>
      </table>
      {loading && (
        <CreateGameDialog
          defaults={{
            loadSlot: loading.slot,
            map: mapOf(loading.mapcmd),
            mode: (['sp', 'coop', 'dm', 'ctf'].includes(loading.mode) ? loading.mode : 'sp') as GameMode,
          }}
          onClose={() => setLoading(null)}
          onCreated={(g) => router.push(`/play/${encodeURIComponent(g.id)}`)}
        />
      )}
    </section>
  );
}
