'use client';
// In-game menu (menu.c M_Menu_Main_f replacement): shown while key_dest == key_menu / the pointer is
// released. Resume re-locks the pointer (needs the click gesture), Options edits the live engine,
// Save/Load sends `save <slot>` / `load <slot>`, Disconnect leaves.
import { useEffect, useState, type RefObject } from 'react';
import type { GameSession } from '@/game/session';
import { useGameStore } from '@/game/store';
import { api, type SaveInfo } from '@/lib/api';
import { Q2Pic, Q2Text } from '../Q2Text';
import { SettingsEditor } from '../settings/SettingsEditor';
import styles from './Game.module.css';

const SLOTS = Array.from({ length: 15 }, (_, i) => `save${i + 1}`);

export function GameOverlay({ session }: { session: RefObject<GameSession | null> }) {
  const overlay = useGameStore((s) => s.overlay);
  const set = useGameStore((s) => s.set);

  useEffect(() => {
    if (!overlay) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.code !== 'Escape') return;
      e.preventDefault();
      if (overlay === 'main') session.current?.resume();
      else set({ overlay: 'main' });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [overlay, session, set]);

  if (!overlay) return null;
  const s = session.current;

  return (
    <div className={styles.menuBackdrop} data-testid="game-menu">
      {overlay === 'main' && (
        <nav className={styles.menu} aria-label="Game menu">
          <Q2Pic name="m_main_plaque" fallback="QUAKE II" />
          <div className={styles.menuItems}>
            <MenuItem label="Resume" onClick={() => s?.resume()} />
            <MenuItem pic="m_main_options" label="Options" onClick={() => set({ overlay: 'options' })} />
            <MenuItem pic="m_main_video" label="Video" onClick={() => set({ overlay: 'video' })} />
            <MenuItem label="Save / Load" onClick={() => set({ overlay: 'saveload' })} />
            <MenuItem label="Disconnect" onClick={() => s?.disconnect()} />
          </div>
        </nav>
      )}
      {(overlay === 'options' || overlay === 'video') && s && (
        <div className="panel dialog" style={{ width: 'min(760px, 100%)' }}>
          <Q2Pic name={overlay === 'video' ? 'm_banner_video' : 'm_banner_options'} fallback="OPTIONS" />
          <SettingsEditor
            backend={s.settingsBackend()}
            tabs={
              overlay === 'video'
                ? ['video', 'controls', 'keys', 'audio', 'player']
                : ['controls', 'keys', 'audio', 'video', 'player']
            }
          />
          <div className="row" style={{ marginTop: 16 }}>
            <button className="btn" onClick={() => set({ overlay: 'main' })}>
              Back
            </button>
            <span className="spacer" />
            <button className="btn primary" onClick={() => s.resume()}>
              Resume
            </button>
          </div>
        </div>
      )}
      {overlay === 'saveload' && s && <SaveLoad session={s} onBack={() => set({ overlay: 'main' })} />}
    </div>
  );
}

/** A main-menu entry: the m_main_* picture (its _sel variant while highlighted) or conchars text. */
function MenuItem({ label, onClick, pic }: { label: string; onClick: () => void; pic?: string }) {
  const [hot, setHot] = useState(false);
  return (
    <button
      className={styles.menuItem}
      onClick={onClick}
      onMouseEnter={() => setHot(true)}
      onMouseLeave={() => setHot(false)}
      onFocus={() => setHot(true)}
      onBlur={() => setHot(false)}
      aria-label={label}
    >
      {pic ? (
        <Q2Pic name={hot ? `${pic}_sel` : pic} fallback={label.toUpperCase()} />
      ) : (
        <Q2Text text={label.toUpperCase()} scale={3} alt={hot} />
      )}
    </button>
  );
}

function SaveLoad({ session, onBack }: { session: GameSession; onBack: () => void }) {
  const [saves, setSaves] = useState<SaveInfo[]>([]);
  const [slot, setSlot] = useState('save1');
  const [msg, setMsg] = useState<string | null>(null);
  const reload = () => api.listSaves().then(setSaves, () => setSaves([]));
  useEffect(() => {
    void reload();
  }, []);
  const bySlot = new Map(saves.map((x) => [x.slot, x]));

  return (
    <div className="panel dialog">
      <Q2Pic name="m_banner_save_game" fallback="SAVE / LOAD" />
      <table className="table">
        <tbody>
          {SLOTS.map((sl) => {
            const info = bySlot.get(sl);
            return (
              <tr key={sl} onClick={() => setSlot(sl)} style={{ cursor: 'pointer' }}>
                <td>
                  <input
                    type="radio"
                    name="slot"
                    checked={slot === sl}
                    onChange={() => setSlot(sl)}
                    aria-label={sl}
                  />
                </td>
                <td className="mono">{sl}</td>
                <td>{info ? info.comment || info.mapcmd : <span className="muted">&lt;EMPTY&gt;</span>}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {msg && <p className="muted">{msg}</p>}
      <div className="row" style={{ marginTop: 12 }}>
        <button className="btn" onClick={onBack}>
          Back
        </button>
        <span className="spacer" />
        <button
          className="btn"
          onClick={() => {
            session.exec(`save ${slot}`);
            setMsg(`Saving to ${slot}…`);
            setTimeout(() => void reload(), 1500);
          }}
        >
          Save
        </button>
        <button
          className="btn primary"
          disabled={!bySlot.has(slot)}
          onClick={() => {
            session.exec(`load ${slot}`);
            session.resume();
          }}
        >
          Load
        </button>
      </div>
    </div>
  );
}
