'use client';
// Options / video / controls / player menus of client/menu.c as React. Works against a SettingsBackend:
// the /settings page edits the saved config.cfg text, the in-game overlay edits the live engine.
import { Key_KeynumToString, keyFromKeyboardEvent, keyFromMouseButton, keyFromWheel } from 'q2-client';
import { useEffect, useState, type ReactNode } from 'react';
import styles from './Settings.module.css';

export interface SettingsBackend {
  getCvar(name: string): string | undefined;
  setCvar(name: string, value: string): void;
  /** key name → command */
  binds(): Map<string, string>;
  bind(key: string, command: string): void;
  unbind(key: string): void;
}

// menu.c bindnames
export const BIND_NAMES: [string, string][] = [
  ['+attack', 'attack'],
  ['weapnext', 'next weapon'],
  ['+forward', 'walk forward'],
  ['+back', 'backpedal'],
  ['+left', 'turn left'],
  ['+right', 'turn right'],
  ['+speed', 'run'],
  ['+moveleft', 'step left'],
  ['+moveright', 'step right'],
  ['+strafe', 'sidestep'],
  ['+lookup', 'look up'],
  ['+lookdown', 'look down'],
  ['centerview', 'center view'],
  ['+mlook', 'mouse look'],
  ['+klook', 'keyboard look'],
  ['+moveup', 'up / jump'],
  ['+movedown', 'down / crouch'],
  ['inven', 'inventory'],
  ['invuse', 'use item'],
  ['invdrop', 'drop item'],
  ['invprev', 'prev item'],
  ['invnext', 'next item'],
  ['cmd help', 'help computer'],
];

/** cvar defaults of the original (used when neither config nor engine has a value). */
export const CVAR_DEFAULTS: Record<string, string> = {
  name: 'unnamed',
  skin: 'male/grunt',
  hand: '0',
  fov: '90',
  rate: '25000',
  sensitivity: '3',
  m_pitch: '0.022',
  cl_run: '0',
  freelook: '0',
  lookspring: '0',
  lookstrafe: '0',
  crosshair: '0',
  s_volume: '0.7',
  s_khz: '11',
  s_loadas8bit: '1',
  vid_gamma: '1',
  gl_picmip: '0',
  gl_dynamic: '1',
  gl_shadows: '0',
  cl_maxfps: '90',
  cl_gun: '1',
  gl_modulate: '1',
};

export type SettingsTab = 'player' | 'controls' | 'keys' | 'video' | 'audio';

const TABS: { id: SettingsTab; label: string }[] = [
  { id: 'player', label: 'Player' },
  { id: 'controls', label: 'Mouse & movement' },
  { id: 'keys', label: 'Key bindings' },
  { id: 'video', label: 'Video' },
  { id: 'audio', label: 'Audio' },
];

export function SettingsEditor({
  backend,
  tabs = TABS.map((t) => t.id),
  onChange,
}: {
  backend: SettingsBackend;
  tabs?: SettingsTab[];
  onChange?: () => void;
}) {
  const [tab, setTab] = useState<SettingsTab>(tabs[0] ?? 'player');
  const [, setVersion] = useState(0);
  const changed = () => {
    setVersion((v) => v + 1);
    onChange?.();
  };
  const cv = (name: string) => backend.getCvar(name) ?? CVAR_DEFAULTS[name] ?? '';
  const set = (name: string, value: string) => {
    backend.setCvar(name, value);
    changed();
  };
  const num = (name: string) => Number(cv(name)) || 0;

  return (
    <div className={styles.editor}>
      <div className={styles.tabs} role="tablist">
        {TABS.filter((t) => tabs.includes(t.id)).map((t) => (
          <button
            key={t.id}
            role="tab"
            aria-selected={tab === t.id}
            className={tab === t.id ? styles.tabActive : styles.tab}
            onClick={() => setTab(t.id)}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === 'player' && (
        <div className={styles.fields}>
          <Row label="Name">
            <input
              type="text"
              maxLength={15}
              value={cv('name')}
              onChange={(e) => set('name', e.target.value)}
            />
          </Row>
          <Row label="Model / skin">
            <select value={cv('skin')} onChange={(e) => set('skin', e.target.value)}>
              {[
                'male/grunt',
                'male/cipher',
                'male/claymore',
                'male/flak',
                'male/howitzer',
                'male/major',
                'male/nightops',
                'male/pointman',
                'male/psycho',
                'male/rampage',
                'male/razor',
                'male/recon',
                'male/scout',
                'male/sniper',
                'male/viper',
                'female/athena',
                'female/brianna',
                'female/cobalt',
                'female/ensign',
                'female/jezebel',
                'female/jungle',
                'female/lotus',
                'female/stiletto',
                'female/venus',
                'female/voodoo',
                'cyborg/oni911',
                'cyborg/ps9000',
                'cyborg/tyr574',
              ]
                .concat(cv('skin') ? [cv('skin')] : [])
                .filter((v, i, a) => a.indexOf(v) === i)
                .map((s) => (
                  <option key={s} value={s}>
                    {s}
                  </option>
                ))}
            </select>
          </Row>
          <Row label="Handedness">
            <select value={cv('hand')} onChange={(e) => set('hand', e.target.value)}>
              <option value="0">right</option>
              <option value="1">left</option>
              <option value="2">center</option>
            </select>
          </Row>
          <Row label="Connection speed">
            <select value={cv('rate')} onChange={(e) => set('rate', e.target.value)}>
              {[
                ['2500', '28.8 modem'],
                ['3200', '33.6 modem'],
                ['5000', 'single ISDN'],
                ['10000', 'dual ISDN / cable'],
                ['25000', 'T1 / LAN'],
              ].map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
              {!['2500', '3200', '5000', '10000', '25000'].includes(cv('rate')) && (
                <option value={cv('rate')}>user defined</option>
              )}
            </select>
          </Row>
          <Slider
            label="Field of view"
            min={60}
            max={130}
            step={1}
            value={num('fov')}
            onChange={(v) => set('fov', String(v))}
          />
        </div>
      )}

      {tab === 'controls' && (
        <div className={styles.fields}>
          <Slider
            label="Mouse speed"
            min={1}
            max={11}
            step={0.5}
            value={num('sensitivity')}
            onChange={(v) => set('sensitivity', String(v))}
          />
          <Toggle
            label="Invert mouse"
            checked={num('m_pitch') < 0}
            onChange={(on) => set('m_pitch', String((on ? -1 : 1) * Math.abs(num('m_pitch') || 0.022)))}
          />
          <Toggle
            label="Always run"
            checked={num('cl_run') !== 0}
            onChange={(on) => set('cl_run', on ? '1' : '0')}
          />
          <Toggle
            label="Free look"
            checked={num('freelook') !== 0}
            onChange={(on) => set('freelook', on ? '1' : '0')}
          />
          <Toggle
            label="Lookspring"
            checked={num('lookspring') !== 0}
            onChange={(on) => set('lookspring', on ? '1' : '0')}
          />
          <Toggle
            label="Lookstrafe"
            checked={num('lookstrafe') !== 0}
            onChange={(on) => set('lookstrafe', on ? '1' : '0')}
          />
          <Row label="Crosshair">
            <select value={cv('crosshair')} onChange={(e) => set('crosshair', e.target.value)}>
              <option value="0">none</option>
              <option value="1">cross</option>
              <option value="2">dot</option>
              <option value="3">angle</option>
            </select>
          </Row>
        </div>
      )}

      {tab === 'keys' && <KeyBindings backend={backend} onChange={changed} />}

      {tab === 'video' && (
        <div className={styles.fields}>
          <Slider
            label="Brightness"
            min={0.5}
            max={1.3}
            step={0.05}
            value={1.8 - num('vid_gamma')}
            display={(v) => v.toFixed(2)}
            onChange={(v) => set('vid_gamma', (1.8 - v).toFixed(2))}
          />
          <Slider
            label="Texture quality"
            min={0}
            max={3}
            step={1}
            value={3 - num('gl_picmip')}
            display={(v) => ['lowest', 'low', 'medium', 'high'][v] ?? String(v)}
            onChange={(v) => set('gl_picmip', String(3 - v))}
          />
          <Slider
            label="Max frame rate"
            min={30}
            max={250}
            step={5}
            value={num('cl_maxfps')}
            onChange={(v) => set('cl_maxfps', String(v))}
          />
          <Toggle
            label="Dynamic lighting"
            checked={num('gl_dynamic') !== 0}
            onChange={(on) => set('gl_dynamic', on ? '1' : '0')}
          />
          <Toggle
            label="Shadows"
            checked={num('gl_shadows') !== 0}
            onChange={(on) => set('gl_shadows', on ? '1' : '0')}
          />
          <Toggle
            label="Show gun"
            checked={num('cl_gun') !== 0}
            onChange={(on) => set('cl_gun', on ? '1' : '0')}
          />
          <p className="muted">
            Texture quality and brightness take effect on the next map load (vid_restart).
          </p>
        </div>
      )}

      {tab === 'audio' && (
        <div className={styles.fields}>
          <Slider
            label="Effects volume"
            min={0}
            max={1}
            step={0.05}
            value={num('s_volume')}
            display={(v) => `${Math.round(v * 100)}%`}
            onChange={(v) => set('s_volume', v.toFixed(2))}
          />
          <Row label="Sound quality">
            <select
              value={cv('s_khz') === '22' || cv('s_khz') === '44' ? cv('s_khz') : '11'}
              onChange={(e) => set('s_khz', e.target.value)}
            >
              <option value="11">low (11 kHz)</option>
              <option value="22">high (22 kHz)</option>
              <option value="44">very high (44 kHz)</option>
            </select>
          </Row>
          <Toggle
            label="Load sounds as 8 bit"
            checked={num('s_loadas8bit') !== 0}
            onChange={(on) => set('s_loadas8bit', on ? '1' : '0')}
          />
          <p className="muted">Audio starts after your first click or key press on the game page.</p>
        </div>
      )}
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className={styles.row}>
      <span className={styles.label}>{label}</span>
      {children}
    </label>
  );
}

function Slider({
  label,
  min,
  max,
  step,
  value,
  onChange,
  display,
}: {
  label: string;
  min: number;
  max: number;
  step: number;
  value: number;
  onChange: (v: number) => void;
  display?: (v: number) => string;
}) {
  const v = Math.min(max, Math.max(min, value));
  return (
    <Row label={label}>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={v}
        onChange={(e) => onChange(Number(e.target.value))}
      />
      <span className="mono muted" style={{ minWidth: 56 }}>
        {display ? display(v) : v}
      </span>
    </Row>
  );
}

function Toggle({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (on: boolean) => void;
}) {
  return (
    <Row label={label}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
    </Row>
  );
}

// ------------------------------------------------------------------ key bindings (menu.c Keys menu)

function keysFor(binds: Map<string, string>, command: string): string[] {
  const out: string[] = [];
  for (const [k, v] of binds) if (v === command) out.push(k);
  return out;
}

function KeyBindings({ backend, onChange }: { backend: SettingsBackend; onChange: () => void }) {
  const [capturing, setCapturing] = useState<string | null>(null);
  const binds = backend.binds();

  // menu.c KeyBindingFunc / Keys_MenuKey: two keys per command; a third replaces both
  const assign = (command: string, key: string) => {
    const current = keysFor(backend.binds(), command);
    if (current.includes(key)) return;
    if (current.length >= 2) for (const k of current) backend.unbind(k);
    backend.bind(key, command);
    onChange();
  };
  const clear = (command: string) => {
    for (const k of keysFor(backend.binds(), command)) backend.unbind(k);
    onChange();
  };

  useEffect(() => {
    if (!capturing) return;
    const done = (keynum: number | null) => {
      if (keynum !== null) assign(capturing, Key_KeynumToString(keynum));
      setCapturing(null);
    };
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault();
      e.stopPropagation();
      if (e.code === 'Escape') return setCapturing(null); // ESC cancels, like the original
      done(keyFromKeyboardEvent(e.code, e.key));
    };
    const onMouse = (e: MouseEvent) => {
      e.preventDefault();
      e.stopPropagation();
      done(keyFromMouseButton(e.button));
    };
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      done(keyFromWheel(e.deltaY));
    };
    const onCtx = (e: Event) => e.preventDefault();
    // defer so the click that started capturing does not bind MOUSE1 immediately
    const t = setTimeout(() => {
      window.addEventListener('mousedown', onMouse, true);
      window.addEventListener('wheel', onWheel, { capture: true, passive: false });
    }, 0);
    window.addEventListener('keydown', onKey, true);
    window.addEventListener('contextmenu', onCtx, true);
    return () => {
      clearTimeout(t);
      window.removeEventListener('keydown', onKey, true);
      window.removeEventListener('mousedown', onMouse, true);
      window.removeEventListener('wheel', onWheel, true);
      window.removeEventListener('contextmenu', onCtx, true);
    };
  }, [capturing]);

  const known = new Set(BIND_NAMES.map(([c]) => c));
  const others = [...binds].filter(([, c]) => !known.has(c)).sort(([a], [b]) => a.localeCompare(b));

  return (
    <div>
      <p className="muted">
        Click a row and press a key, mouse button or wheel. <span className="mono">Esc</span> cancels; the
        clear button removes a binding. Two keys per action; a third replaces both.
      </p>
      <table className="table">
        <tbody>
          {BIND_NAMES.map(([command, label]) => {
            const keys = keysFor(binds, command);
            const active = capturing === command;
            return (
              <tr key={command} className={active ? styles.capturing : undefined}>
                <td style={{ width: '40%' }}>{label}</td>
                <td>
                  <button
                    className={styles.keyButton}
                    data-command={command}
                    onClick={() => setCapturing(command)}
                    aria-label={`Bind ${label}`}
                  >
                    {active ? 'press a key…' : keys.length ? keys.join(' or ') : '???'}
                  </button>
                </td>
                <td style={{ textAlign: 'right' }}>
                  {keys.length > 0 && (
                    <button
                      className="btn small"
                      onClick={() => clear(command)}
                      aria-label={`Clear ${label}`}
                    >
                      Clear
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {others.length > 0 && (
        <>
          <h3 style={{ marginTop: 20 }}>Other bindings</h3>
          <table className="table">
            <tbody>
              {others.map(([k, c]) => (
                <tr key={k}>
                  <td className="mono" style={{ width: '25%' }}>
                    {k}
                  </td>
                  <td className="mono">{c}</td>
                  <td style={{ textAlign: 'right' }}>
                    <button
                      className="btn small"
                      onClick={() => {
                        backend.unbind(k);
                        onChange();
                      }}
                    >
                      Unbind
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
