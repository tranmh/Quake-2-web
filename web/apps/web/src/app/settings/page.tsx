'use client';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { Q2Pic } from '@/components/Q2Text';
import { SettingsEditor, type SettingsBackend } from '@/components/settings/SettingsEditor';
import { AssetLoader, fetchAssetIndex } from '@/lib/assets';
import {
  loadConfigText,
  normalizeKeyName,
  parseConfig,
  saveConfigText,
  serializeConfig,
  type ParsedConfig,
} from '@/lib/config';
import { DEFAULT_PAKSET } from '@/lib/env';
import { useSession } from '@/lib/session';

/** default.cfg of the demo pak (executed before config.cfg by the engine). */
async function loadDefaults(): Promise<string> {
  try {
    const index = await fetchAssetIndex(DEFAULT_PAKSET);
    const data = await new AssetLoader(index).loadFile('default.cfg');
    return data ? new TextDecoder('latin1').decode(data) : '';
  } catch {
    return '';
  }
}

function emptyConfig(): ParsedConfig {
  return { binds: new Map(), cvars: new Map(), unbindall: false, other: [] };
}

/** The effective configuration: default.cfg, then the user's config.cfg on top. */
function effective(defaults: string, user: string | undefined): ParsedConfig {
  const cfg = parseConfig(defaults, emptyConfig());
  // default.cfg's own "unbindall" is not something the user chose
  cfg.unbindall = false;
  cfg.other = cfg.other.filter((l) => !/^exec\b/i.test(l));
  const defaultOther = new Set(cfg.other);
  if (user) parseConfig(user, cfg);
  // aliases etc. that come from default.cfg are re-created by it on every start
  cfg.other = cfg.other.filter((l, i, a) => a.indexOf(l) === i && !defaultOther.has(l));
  return cfg;
}

export default function SettingsPage() {
  const user = useSession((s) => s.user);
  const [defaults, setDefaults] = useState<string | null>(null);
  const [cfg, setCfg] = useState<ParsedConfig | null>(null);
  const [dirty, setDirty] = useState(false);
  const [status, setStatus] = useState<string | null>(null);
  const [raw, setRaw] = useState('');
  const [showRaw, setShowRaw] = useState(false);

  const load = useCallback(async () => {
    const [d, text] = await Promise.all([loadDefaults(), loadConfigText()]);
    setDefaults(d);
    setCfg(effective(d, text));
    setDirty(false);
  }, []);

  useEffect(() => {
    void load();
  }, [load, user]);

  const backend = useMemo<SettingsBackend | null>(() => {
    if (!cfg) return null;
    return {
      getCvar: (n) => cfg.cvars.get(n),
      setCvar: (n, v) => cfg.cvars.set(n, v),
      binds: () => cfg.binds,
      bind: (k, c) => cfg.binds.set(normalizeKeyName(k), c),
      unbind: (k) => cfg.binds.delete(normalizeKeyName(k)),
    };
  }, [cfg]);

  const save = async () => {
    if (!cfg) return;
    // unbindall + the complete list makes removed default bindings stick (config.cfg runs after default.cfg)
    const text = serializeConfig({ ...cfg, unbindall: true });
    const remote = await saveConfigText(text);
    setDirty(false);
    setStatus(remote ? 'Saved to your account.' : user ? 'Saved locally (server unavailable).' : 'Saved in this browser (log in to sync).');
  };

  return (
    <main className="page">
      <Q2Pic name="m_banner_options" fallback="OPTIONS" />
      <h1 className="visually-hidden">Settings</h1>
      <section className="panel">
        {!backend ? (
          <p className="muted">Loading…</p>
        ) : (
          <SettingsEditor
            backend={backend}
            onChange={() => {
              setDirty(true);
              setStatus(null);
            }}
          />
        )}
        <div className="row" style={{ marginTop: 20 }}>
          <button className="btn primary" disabled={!dirty} onClick={save}>
            Save
          </button>
          <button
            className="btn"
            onClick={() => {
              if (defaults === null) return;
              setCfg(effective(defaults, undefined));
              setDirty(true);
            }}
          >
            Reset to defaults
          </button>
          <button
            className="btn"
            onClick={() => {
              if (!cfg) return;
              setRaw(serializeConfig({ ...cfg, unbindall: true }));
              setShowRaw((v) => !v);
            }}
          >
            {showRaw ? 'Hide' : 'Edit'} config.cfg
          </button>
          <span className="spacer" />
          {status && <span className="ok">{status}</span>}
          {dirty && <span className="muted">unsaved changes</span>}
        </div>
        {showRaw && (
          <div style={{ marginTop: 16, display: 'grid', gap: 8 }}>
            <textarea rows={18} value={raw} onChange={(e) => setRaw(e.target.value)} spellCheck={false} />
            <div className="row">
              <button
                className="btn"
                onClick={() => {
                  setCfg(effective(defaults ?? '', raw));
                  setDirty(true);
                  setShowRaw(false);
                }}
              >
                Apply text
              </button>
            </div>
          </div>
        )}
      </section>
    </main>
  );
}
