'use client';
// Start-server dialog (menu.c "start server" / "game" menus → POST /api/v1/games).
import { useEffect, useState, type FormEvent } from 'react';
import { api, errorMessage, type GameInfo, type GameMode, type MapSummary, type Pakset } from '@/lib/api';
import { DEFAULT_PAKSET } from '@/lib/env';
import { Q2Pic } from './Q2Text';

const MODES: { id: GameMode; label: string }[] = [
  { id: 'sp', label: 'Single player' },
  { id: 'coop', label: 'Cooperative' },
  { id: 'dm', label: 'Deathmatch' },
  { id: 'ctf', label: 'Capture the flag' },
];

// menu.c skill names
const SKILLS = ['Easy', 'Medium', 'Hard', 'Nightmare'];

export interface CreateGameDefaults {
  mode?: GameMode;
  map?: string;
  loadSlot?: string;
  pakset?: string;
}

export function CreateGameDialog({
  onClose,
  onCreated,
  defaults,
}: {
  onClose: () => void;
  onCreated: (g: GameInfo) => void;
  defaults?: CreateGameDefaults;
}) {
  const [paksets, setPaksets] = useState<Pakset[]>([]);
  const [pakset, setPakset] = useState(defaults?.pakset ?? DEFAULT_PAKSET);
  const [maps, setMaps] = useState<MapSummary[]>([]);
  const [mode, setMode] = useState<GameMode>(defaults?.mode ?? 'sp');
  const [map, setMap] = useState(defaults?.map ?? 'demo1');
  const [name, setName] = useState('');
  const [skill, setSkill] = useState(1);
  const [fraglimit, setFraglimit] = useState(0);
  const [timelimit, setTimelimit] = useState(0);
  const [maxPlayers, setMaxPlayers] = useState(8);
  const [password, setPassword] = useState('');
  const [isPublic, setPublic] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.listPaksets().then(setPaksets, () => {});
  }, []);
  useEffect(() => {
    let live = true;
    api.listMaps(pakset).then(
      (m) => {
        if (!live) return;
        setMaps(m);
        if (m.length && !m.some((x) => x.name === map)) setMap(m[0]!.name);
      },
      (e) => live && setError(errorMessage(e)),
    );
    return () => {
      live = false;
    };
  }, [pakset]);

  const multiplayer = mode === 'dm' || mode === 'ctf' || mode === 'coop';
  const deathmatch = mode === 'dm' || mode === 'ctf';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    const cvars: Record<string, string> = { skill: String(skill) };
    if (deathmatch) {
      cvars.fraglimit = String(fraglimit);
      cvars.timelimit = String(timelimit);
    }
    if (password) cvars.password = password;
    try {
      const g = await api.createGame({
        mode,
        map,
        pakset,
        ...(name.trim() ? { name: name.trim() } : {}),
        maxPlayers: multiplayer ? maxPlayers : 1,
        public: multiplayer && isPublic,
        cvars,
        ...(defaults?.loadSlot ? { loadSlot: defaults.loadSlot } : {}),
      });
      onCreated(g);
    } catch (err) {
      setError(errorMessage(err));
      setBusy(false);
    }
  };

  return (
    <div className="backdrop" role="dialog" aria-modal="true" aria-label="Start a game" onClick={onClose}>
      <form className="panel dialog" onSubmit={submit} onClick={(e) => e.stopPropagation()} style={{ display: 'grid', gap: 14 }}>
        <Q2Pic name="m_banner_start_server" fallback="START SERVER" />
        {defaults?.loadSlot && (
          <p className="muted">
            Loading save <span className="mono">{defaults.loadSlot}</span>
          </p>
        )}
        <div className="grid2">
          <label className="field">
            Mode
            <select name="mode" value={mode} onChange={(e) => setMode(e.target.value as GameMode)}>
              {MODES.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            Pakset
            <select name="pakset" value={pakset} onChange={(e) => setPakset(e.target.value)}>
              {!paksets.some((p) => p.id === pakset) && <option value={pakset}>{pakset}</option>}
              {paksets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            Map
            <select name="map" value={map} onChange={(e) => setMap(e.target.value)}>
              {maps.length === 0 && <option value={map}>{map}</option>}
              {maps.map((m) => (
                <option key={m.name} value={m.name}>
                  {m.name}
                  {m.message ? ` — ${m.message}` : ''}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            Skill
            <select name="skill" value={skill} onChange={(e) => setSkill(Number(e.target.value))}>
              {SKILLS.map((s, i) => (
                <option key={s} value={i}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          {multiplayer && (
            <label className="field">
              Max players
              <input type="number" min={2} max={64} value={maxPlayers} onChange={(e) => setMaxPlayers(Number(e.target.value))} />
            </label>
          )}
          {deathmatch && (
            <>
              <label className="field">
                Frag limit
                <input type="number" min={0} max={999} value={fraglimit} onChange={(e) => setFraglimit(Number(e.target.value))} />
              </label>
              <label className="field">
                Time limit (min)
                <input type="number" min={0} max={999} value={timelimit} onChange={(e) => setTimelimit(Number(e.target.value))} />
              </label>
            </>
          )}
          {multiplayer && (
            <label className="field">
              Password
              <input type="text" value={password} maxLength={64} onChange={(e) => setPassword(e.target.value)} placeholder="none" />
            </label>
          )}
          <label className="field">
            Server name
            <input type="text" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} placeholder="(my game)" />
          </label>
        </div>
        {multiplayer && (
          <label className="check">
            <input type="checkbox" checked={isPublic} onChange={(e) => setPublic(e.target.checked)} /> List publicly in the server
            browser
          </label>
        )}
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <div className="row">
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
          <span className="spacer" />
          <button type="submit" className="btn primary" disabled={busy || !map}>
            {busy ? 'Starting…' : 'Begin'}
          </button>
        </div>
      </form>
    </div>
  );
}
