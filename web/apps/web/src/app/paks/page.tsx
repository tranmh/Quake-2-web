'use client';
import { useCallback, useEffect, useRef, useState } from 'react';
import { RequireLogin } from '@/components/RequireLogin';
import { Q2Pic } from '@/components/Q2Text';
import { api, errorMessage, uploadPak, type Pak, type Pakset } from '@/lib/api';
import { useSession } from '@/lib/session';

interface UploadItem {
  id: number;
  name: string;
  size: number;
  loaded: number;
  status: 'uploading' | 'ingesting' | 'done' | 'failed';
  message?: string;
  jobProgress?: number;
}

function fmtSize(n: number): string {
  if (n >= 1 << 30) return (n / (1 << 30)).toFixed(2) + ' GiB';
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MiB';
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(0) + ' KiB';
  return n + ' B';
}

function statusBadge(s: string) {
  const cls = s === 'ready' ? 'ok' : s === 'failed' ? 'bad' : 'warn';
  return <span className={`badge ${cls}`}>{s}</span>;
}

export default function PaksPage() {
  return (
    <main className="page">
      <Q2Pic name="m_banner_customize" fallback="GAME DATA" />
      <h1 className="visually-hidden">Paks</h1>
      <RequireLogin>
        <PaksManager />
      </RequireLogin>
    </main>
  );
}

function PaksManager() {
  const user = useSession((s) => s.user);
  const [paks, setPaks] = useState<Pak[]>([]);
  const [paksets, setPaksets] = useState<Pakset[]>([]);
  const [uploads, setUploads] = useState<UploadItem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [drag, setDrag] = useState(false);
  const nextId = useRef(1);
  const fileInput = useRef<HTMLInputElement>(null);

  const reload = useCallback(async () => {
    try {
      const [p, s] = await Promise.all([api.listPaks(), api.listPaksets()]);
      setPaks(p);
      setPaksets(s);
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  // poll while something is ingesting
  const pending = paks.some((p) => p.status === 'pending') || uploads.some((u) => u.status === 'ingesting');
  useEffect(() => {
    if (!pending) return;
    const t = setInterval(() => void reload(), 2000);
    return () => clearInterval(t);
  }, [pending, reload]);

  const patch = (id: number, p: Partial<UploadItem>) =>
    setUploads((list) => list.map((u) => (u.id === id ? { ...u, ...p } : u)));

  const upload = async (file: File) => {
    const id = nextId.current++;
    setUploads((l) => [{ id, name: file.name, size: file.size, loaded: 0, status: 'uploading' }, ...l]);
    try {
      const r = await uploadPak(file, (p) => patch(id, { loaded: p.loaded }));
      patch(id, { loaded: file.size });
      void reload();
      if (!r.job) {
        patch(id, { status: 'done', message: r.pak.status === 'ready' ? 'already known — added to your paks' : r.pak.status });
        return;
      }
      patch(id, { status: 'ingesting', jobProgress: 0 });
      for (;;) {
        await new Promise((res) => setTimeout(res, 1000));
        const job = await api.getJob(r.job.id);
        patch(id, { jobProgress: job.progress });
        if (job.status === 'done') {
          patch(id, { status: 'done', message: `${r.pak.numFiles} files ingested` });
          break;
        }
        if (job.status === 'failed') {
          patch(id, { status: 'failed', message: job.error || 'ingest failed' });
          break;
        }
      }
      void reload();
    } catch (e) {
      patch(id, { status: 'failed', message: errorMessage(e) });
    }
  };

  const onFiles = (files: FileList | null) => {
    if (!files) return;
    for (const f of Array.from(files)) void upload(f);
  };

  return (
    <>
      <section className="panel">
        <h2>Upload pak files</h2>
        <p className="muted">
          Upload <span className="mono">pak0.pak</span>, <span className="mono">pak1.pak</span>, … from your own Quake II
          installation (<span className="mono">baseq2/</span>). Files are validated, stored privately and only served to
          accounts that uploaded the same file.
        </p>
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDrag(true);
          }}
          onDragLeave={() => setDrag(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDrag(false);
            onFiles(e.dataTransfer.files);
          }}
          style={{
            border: `2px dashed ${drag ? 'var(--accent)' : 'var(--border-hi)'}`,
            padding: 28,
            textAlign: 'center',
            cursor: 'pointer',
          }}
          onClick={() => fileInput.current?.click()}
        >
          <p>Drop .pak files here or click to choose</p>
          <input
            ref={fileInput}
            type="file"
            accept=".pak"
            multiple
            hidden
            onChange={(e) => {
              onFiles(e.target.files);
              e.target.value = '';
            }}
          />
        </div>
        {uploads.length > 0 && (
          <table className="table" style={{ marginTop: 16 }}>
            <tbody>
              {uploads.map((u) => {
                const frac =
                  u.status === 'uploading' ? u.loaded / Math.max(1, u.size) : u.status === 'ingesting' ? (u.jobProgress ?? 0) : 1;
                return (
                  <tr key={u.id}>
                    <td className="mono">{u.name}</td>
                    <td className="muted">{fmtSize(u.size)}</td>
                    <td style={{ width: '40%' }}>
                      <div className="progress" aria-label={`${u.status} ${Math.round(frac * 100)}%`}>
                        <div style={{ width: `${Math.round(frac * 100)}%` }} />
                      </div>
                    </td>
                    <td>
                      <span className={u.status === 'failed' ? 'error' : u.status === 'done' ? 'ok' : 'muted'}>
                        {u.status}
                        {u.message ? ` — ${u.message}` : ''}
                      </span>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </section>

      {error && <p className="error">{error}</p>}

      <section className="panel">
        <h2>Paks</h2>
        <table className="table">
          <thead>
            <tr>
              <th>Id</th>
              <th>Name</th>
              <th>Files</th>
              <th>Size</th>
              <th>Status</th>
              <th>SHA-256</th>
            </tr>
          </thead>
          <tbody>
            {paks.map((p) => (
              <tr key={p.id}>
                <td className="mono">{p.id}</td>
                <td className="mono">
                  {p.name} {p.public && <span className="badge">public</span>}
                </td>
                <td>{p.numFiles}</td>
                <td>{fmtSize(p.size)}</td>
                <td>
                  {statusBadge(p.status)} {p.error && <span className="error">{p.error}</span>}
                </td>
                <td className="mono muted" title={p.sha256}>
                  {p.sha256.slice(0, 12)}…
                </td>
              </tr>
            ))}
            {paks.length === 0 && (
              <tr>
                <td colSpan={6} className="muted">
                  No paks yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      <PaksetEditor paks={paks.filter((p) => p.status === 'ready')} paksets={paksets} userId={user?.id ?? 0} onChange={reload} />
    </>
  );
}

function PaksetEditor({
  paks,
  paksets,
  userId,
  onChange,
}: {
  paks: Pak[];
  paksets: Pakset[];
  userId: number;
  onChange: () => void;
}) {
  const [name, setName] = useState('My game');
  const [order, setOrder] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const toggle = (id: number) => setOrder((o) => (o.includes(id) ? o.filter((x) => x !== id) : [...o, id]));
  const pakName = (id: number) => paks.find((p) => p.id === id)?.name ?? `#${id}`;

  const create = async () => {
    setError(null);
    try {
      await api.createPakset(name, order);
      setOrder([]);
      onChange();
    } catch (e) {
      setError(errorMessage(e));
    }
  };

  return (
    <section className="panel">
      <h2>Paksets</h2>
      <p className="muted">
        A pakset is an ordered list of paks, lowest priority first (like <span className="mono">pak0</span>,{' '}
        <span className="mono">pak1</span>, …). Games are started from a pakset.
      </p>
      <table className="table">
        <thead>
          <tr>
            <th>Id</th>
            <th>Name</th>
            <th>Paks</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {paksets.map((s) => (
            <tr key={s.id}>
              <td className="mono">{s.id}</td>
              <td>
                {s.name} {s.public && <span className="badge">public</span>}
              </td>
              <td className="mono">{s.pakIds.map(pakName).join(' → ')}</td>
              <td style={{ textAlign: 'right' }}>
                {s.ownerId === userId && !s.public && (
                  <button
                    className="btn small danger"
                    onClick={async () => {
                      if (!confirm(`Delete pakset "${s.name}"?`)) return;
                      try {
                        await api.deletePakset(s.id);
                        onChange();
                      } catch (e) {
                        setError(errorMessage(e));
                      }
                    }}
                  >
                    Delete
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <h3 style={{ marginTop: 20 }}>New pakset</h3>
      <div className="row" style={{ alignItems: 'flex-end' }}>
        <label className="field">
          Name
          <input type="text" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
        </label>
      </div>
      <div className="row" style={{ marginTop: 12 }}>
        {paks.map((p) => (
          <label key={p.id} className="check">
            <input type="checkbox" checked={order.includes(p.id)} onChange={() => toggle(p.id)} />
            <span className="mono">
              {p.name} <span className="muted">#{p.id}</span>
            </span>
          </label>
        ))}
      </div>
      {order.length > 0 && (
        <p className="mono muted">
          Order: {order.map(pakName).join(' → ')} <span>(last wins)</span>
        </p>
      )}
      {error && <p className="error">{error}</p>}
      <button className="btn primary" disabled={!order.length || !name.trim()} onClick={create}>
        Create pakset
      </button>
    </section>
  );
}
