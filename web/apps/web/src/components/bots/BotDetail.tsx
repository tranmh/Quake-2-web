'use client';
// A bot run: status while it runs, the summary from run.json once it ended (outcome, per-level times,
// deaths and kills, decision provenance per field, model share, API latency, cost), the artifacts and the
// replay player.
import Link from 'next/link';
import { useCallback, useEffect, useState } from 'react';
import { Q2Text } from '@/components/Q2Text';
import { ApiError, api, botIsLive, errorMessage, type BotInfo } from '@/lib/api';
import {
  RUN_SCHEMA,
  SKILL_NAMES,
  artifactEpisodes,
  demoArtifacts,
  fieldShares,
  formatBytes,
  formatGameTime,
  formatMs,
  formatPercent,
  formatUsd,
  type EpisodeSummary,
  type RunSummary,
} from '@/lib/bots';
import { useSession } from '@/lib/session';
import { ReplayPlayer } from './ReplayPlayer';
import styles from './Bots.module.css';

const STATUS_BADGE: Record<string, string> = {
  starting: 'badge warn',
  running: 'badge ok',
  finished: 'badge',
  failed: 'badge bad',
  stopped: 'badge',
};

const OUTCOME_BADGE: Record<string, string> = {
  completed: 'badge ok',
  exit: 'badge ok',
  victory: 'badge ok',
  failed: 'badge bad',
  death_limit: 'badge bad',
  timeout: 'badge bad',
  stalled: 'badge bad',
  error: 'badge bad',
  aborted: 'badge warn',
  incomplete: 'badge warn',
};

export function BotDetail({ botId }: { botId: string }) {
  const { user, loaded, refresh } = useSession();
  const [bot, setBot] = useState<BotInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [runJson, setRunJson] = useState<RunSummary | null>(null);
  const [episode, setEpisode] = useState(0);
  const [replayAt, setReplayAt] = useState<number | null>(null);

  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);

  const reload = useCallback(async () => {
    try {
      setBot(await api.getBot(botId));
      setError(null);
    } catch (e) {
      setError(
        e instanceof ApiError && e.status === 404
          ? 'No such bot (or it is not visible to you).'
          : errorMessage(e),
      );
    }
  }, [botId]);

  const live = bot ? botIsLive(bot.status) : false;
  const hasRunJson = !!bot?.artifacts.some((a) => a.name === 'run.json');

  useEffect(() => {
    if (!loaded) return;
    void reload();
  }, [loaded, reload]);

  // poll while the run is live, and until its run.json exists once it ended
  useEffect(() => {
    if (!bot || (!live && (hasRunJson || bot.summary))) return;
    const t = setInterval(() => void reload(), live ? 3000 : 5000);
    return () => clearInterval(t);
  }, [bot, live, hasRunJson, reload]);

  // older servers / a summary not inlined: read run.json itself
  useEffect(() => {
    if (!bot || bot.summary || !hasRunJson || runJson) return;
    let alive = true;
    api
      .fetchArtifact(bot.id, 'run.json')
      .then((r) => r.json() as Promise<RunSummary>)
      .then(
        (s) => alive && setRunJson(s),
        () => {},
      );
    return () => {
      alive = false;
    };
  }, [bot, hasRunJson, runJson]);

  if (error && !bot) {
    return (
      <section className="panel">
        <p className="error">{error}</p>
        <Link className="btn" href="/bots">
          All bots
        </Link>
      </section>
    );
  }
  if (!bot) return <p className="muted">Loading…</p>;

  const summary = bot.summary ?? runJson;
  const episodes = artifactEpisodes(bot.artifacts);
  const demos = demoArtifacts(bot.artifacts, episode);
  const mine = !!user && (bot.ownerId === user.id || user.isAdmin);

  return (
    <>
      <section className="panel">
        <div className={styles.header}>
          <Q2Text text={(bot.name || bot.id).toUpperCase()} scale={2} alt />
          <span className={STATUS_BADGE[bot.status] ?? 'badge'} data-testid="bot-status">
            {bot.status}
          </span>
          {!bot.public && <span className="badge">private</span>}
          <span className="spacer" />
          {live && (
            <Link className="btn primary" href={`/watch/${encodeURIComponent(bot.id)}`}>
              Watch live
            </Link>
          )}
          {live && mine && (
            <button
              className="btn danger"
              onClick={async () => {
                try {
                  await api.stopBot(bot.id);
                  void reload();
                } catch (e) {
                  setError(errorMessage(e));
                }
              }}
            >
              Stop
            </button>
          )}
          <Link className="btn" href="/bots">
            All bots
          </Link>
        </div>
        {bot.reason && <p className="muted">{bot.reason}</p>}
        {error && <p className="error">{error}</p>}
        <dl className={styles.facts}>
          <Fact label="Backend" value={bot.model ? `${bot.backend} · ${bot.model}` : bot.backend} />
          <Fact label="Maps" value={bot.maps.join(' → ') || 'campaign'} />
          <Fact label="Skill" value={SKILL_NAMES[bot.skill] ?? String(bot.skill)} />
          <Fact label="Started" value={new Date(bot.startedAt).toLocaleString()} />
          {bot.endedAt && <Fact label="Ended" value={new Date(bot.endedAt).toLocaleString()} />}
          {bot.level && (
            <Fact
              label="Level"
              value={`${bot.level.map}${bot.level.visit > 0 ? ` (visit ${bot.level.visit + 1})` : ''}`}
            />
          )}
          {bot.live && (
            <>
              <Fact label="Kills / deaths" value={`${bot.live.kills} / ${bot.live.deaths}`} />
              <Fact label="Decisions" value={String(bot.live.decisions)} />
              <Fact label="Model share" value={formatPercent(bot.live.modelShare)} />
              <Fact label="Cost" value={formatUsd(bot.live.costUsd)} />
            </>
          )}
          <Fact label="Viewers" value={String(bot.viewers)} />
        </dl>
      </section>

      {summary ? (
        <RunSummaryView summary={summary} />
      ) : (
        !live && (
          <section className="panel">
            <p className="muted">The run summary is not available yet.</p>
          </section>
        )
      )}

      {!live && (
        <section className="panel" aria-label="Replay">
          <div className="row">
            <h2 style={{ margin: 0 }}>Replay</h2>
            <span className="spacer" />
            {episodes.length > 1 && (
              <label className="check">
                Episode
                <select
                  name="episode"
                  value={episode}
                  onChange={(e) => {
                    setReplayAt(null);
                    setEpisode(Number(e.target.value));
                  }}
                >
                  {episodes.map((e) => (
                    <option key={e} value={e}>
                      {e}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>
          {demos.length === 0 ? (
            <p className="muted">This run recorded no demos.</p>
          ) : replayAt === null ? (
            <>
              <p className="muted">
                {demos.length} level attempt{demos.length === 1 ? '' : 's'} recorded. The AI overlay is
                rebuilt from the decision trace.
              </p>
              <div className={styles.levels}>
                <button className="btn primary" onClick={() => setReplayAt(0)}>
                  Play replay
                </button>
                {demos.map((d, i) => (
                  <button
                    key={d.name}
                    className="btn small"
                    onClick={() => setReplayAt(i)}
                    title={`${d.name} · ${formatBytes(d.size)}`}
                  >
                    {String(d.attempt).padStart(2, '0')} · {d.map}
                  </button>
                ))}
              </div>
            </>
          ) : (
            <ReplayPlayer
              key={`${episode}`}
              botId={bot.id}
              episode={episode}
              level={replayAt}
              onClose={() => setReplayAt(null)}
            />
          )}
        </section>
      )}

      <section className="panel">
        <h2>Artifacts</h2>
        {bot.artifacts.length === 0 ? (
          <p className="muted">No artifacts{live ? ' yet' : ''}.</p>
        ) : (
          <table className="table" data-testid="artifacts">
            <thead>
              <tr>
                <th>File</th>
                <th>Kind</th>
                <th>Size</th>
              </tr>
            </thead>
            <tbody>
              {bot.artifacts.map((a) => (
                <tr key={a.name}>
                  <td className="mono">
                    <a href={api.artifactUrl(bot.id, a.name)} download>
                      {a.name}
                    </a>
                  </td>
                  <td>{a.kind}</td>
                  <td className="mono">{formatBytes(a.size)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </section>
    </>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.fact}>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function RunSummaryView({ summary: s }: { summary: RunSummary }) {
  const t = s.totals;
  const episodes = s.episodes ?? [];
  const shares = fieldShares(s.ticks);
  return (
    <section className="panel" data-testid="run-summary">
      <div className="row">
        <h2 style={{ margin: 0 }}>Summary</h2>
        <span className={OUTCOME_BADGE[s.outcome] ?? 'badge'}>{s.outcome}</span>
        {s.reason && <span className="muted">{s.reason}</span>}
        {s.schema !== RUN_SCHEMA && <span className="badge warn">schema {s.schema}</span>}
      </div>
      <dl className={styles.facts}>
        <Fact label="Game time" value={formatGameTime(s.game_ms)} />
        <Fact label="Wall time" value={formatGameTime(s.wall_ms)} />
        <Fact label="Levels" value={`${t.levels_completed} / ${t.levels}`} />
        <Fact label="Deaths" value={String(t.deaths)} />
        <Fact label="Kills (bot)" value={String(t.bot_kills)} />
        <Fact label="Monsters" value={`${t.kills} / ${t.monsters}`} />
        <Fact label="Secrets" value={`${t.secrets} / ${t.total_secrets}`} />
        <Fact label="Damage taken" value={String(t.damage_taken)} />
        <Fact
          label="Model share"
          value={formatPercent(s.ticks?.gate_model_share ?? s.decisions.gate_model_share)}
        />
        <Fact label="Decisions" value={String(s.decisions.decisions)} />
        <Fact
          label="API p50 / p95"
          value={`${formatMs(s.api.latency_ms.p50)} / ${formatMs(s.api.latency_ms.p95)}`}
        />
        <Fact label="API errors" value={`${s.api.errors} · stale ${formatPercent(s.api.stale_rate, 1)}`} />
        <Fact label="Tokens in" value={String(s.api.input_tokens)} />
        <Fact label="Cost" value={formatUsd(s.api.cost_usd)} />
      </dl>
      {s.gate && (
        <div style={{ marginTop: 14 }}>
          <span className={s.gate.passed ? 'badge ok' : 'badge bad'}>
            provenance gate {s.gate.passed ? 'passed' : 'not met'}
          </span>{' '}
          <span className="muted mono">
            min model share {formatPercent(s.gate.min_model_share)} · max stale{' '}
            {formatPercent(s.gate.max_stale_rate)} · basis {s.gate.basis}
          </span>
          {s.gate.reasons && s.gate.reasons.length > 0 && (
            <ul className={styles.reasons}>
              {s.gate.reasons.map((r) => (
                <li key={r}>{r}</li>
              ))}
            </ul>
          )}
        </div>
      )}

      {episodes.map((ep) => (
        <EpisodeLevels key={ep.index} episode={ep} many={episodes.length > 1} />
      ))}

      {shares.length > 0 && (
        <>
          <h3 style={{ marginTop: 20 }}>Decision provenance (per tick)</h3>
          <table className="table" data-testid="provenance">
            <thead>
              <tr>
                <th>Field</th>
                <th>Sources</th>
                <th>Model</th>
                <th>Scripted</th>
                <th>Stale</th>
                <th>Reflex</th>
                <th>Default</th>
                <th>Model share</th>
              </tr>
            </thead>
            <tbody>
              {shares.map((f) => (
                <tr key={f.name}>
                  <td className="mono">{f.name}</td>
                  <td>
                    <div className={styles.shareBar} aria-hidden="true">
                      <span className={styles.sModel} style={{ width: `${f.model * 100}%` }} />
                      <span className={styles.sScripted} style={{ width: `${f.scripted * 100}%` }} />
                      <span className={styles.sStale} style={{ width: `${f.stale * 100}%` }} />
                      <span className={styles.sReflex} style={{ width: `${f.reflex * 100}%` }} />
                      <span className={styles.sDefault} style={{ width: `${f.default * 100}%` }} />
                    </div>
                  </td>
                  <td className="mono">{formatPercent(f.model)}</td>
                  <td className="mono">{formatPercent(f.scripted)}</td>
                  <td className="mono">{formatPercent(f.stale)}</td>
                  <td className="mono">{formatPercent(f.reflex)}</td>
                  <td className="mono">{formatPercent(f.default)}</td>
                  <td className="mono">{formatPercent(f.modelShare)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className={styles.legend}>
            <span>
              <i className={styles.sModel} />
              model
            </span>
            <span>
              <i className={styles.sScripted} />
              scripted
            </span>
            <span>
              <i className={styles.sStale} />
              stale
            </span>
            <span>
              <i className={styles.sReflex} />
              reflex
            </span>
            <span>
              <i className={styles.sDefault} />
              default
            </span>
          </div>
        </>
      )}
    </section>
  );
}

function EpisodeLevels({ episode: ep, many }: { episode: EpisodeSummary; many: boolean }) {
  const levels = ep.levels ?? [];
  return (
    <>
      <h3 style={{ marginTop: 20 }}>
        {many ? `Episode ${ep.index} · ` : ''}Levels{' '}
        <span className={OUTCOME_BADGE[ep.outcome] ?? 'badge'}>{ep.outcome}</span>
      </h3>
      <table className="table" data-testid="levels">
        <thead>
          <tr>
            <th>#</th>
            <th>Map</th>
            <th>Outcome</th>
            <th>Time</th>
            <th>Combat</th>
            <th>Deaths</th>
            <th>Kills</th>
            <th>Monsters</th>
            <th>Damage</th>
            <th>Stuck</th>
          </tr>
        </thead>
        <tbody>
          {levels.map((l) => (
            <tr key={`${l.lvl}-${l.map}`}>
              <td className="mono">{l.lvl}</td>
              <td className="mono">
                {l.map}
                {l.visit > 0 ? ` (visit ${l.visit + 1})` : ''}
              </td>
              <td>
                <span className={OUTCOME_BADGE[l.outcome] ?? 'badge'} title={l.reason}>
                  {l.outcome}
                </span>
              </td>
              <td className="mono">{formatGameTime(l.time_ms)}</td>
              <td className="mono">{formatGameTime(l.combat_ms)}</td>
              <td className="mono">{l.deaths}</td>
              <td className="mono">{l.bot_kills}</td>
              <td className="mono">
                {l.kills} / {l.monsters}
              </td>
              <td className="mono">{l.damage_taken}</td>
              <td className="mono">{l.stuck}</td>
            </tr>
          ))}
          {levels.length === 0 && (
            <tr>
              <td colSpan={10} className="muted">
                No level finished.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </>
  );
}
