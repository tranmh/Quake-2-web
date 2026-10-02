// Typed fetch wrappers for the Go HTTP API (server/internal/api). All requests are same-origin
// (/api/v1/...; proxied in dev by next.config.ts rewrites) and carry the session cookie.
import type { RunSummary } from './bots';

export interface User {
  id: number;
  email: string;
  displayName: string;
  isAdmin: boolean;
  createdAt: string;
}

export interface Pak {
  id: number;
  sha256: string;
  name: string;
  size: number;
  checksum: number;
  numFiles: number;
  public: boolean;
  /** pending | ready | failed */
  status: string;
  error?: string;
  createdAt: string;
  ingestedAt?: string;
}

export interface Job {
  id: number;
  kind: string;
  /** queued | running | done | failed */
  status: string;
  pakId: number;
  progress: number;
  error?: string;
}

export interface Pakset {
  id: string;
  name: string;
  ownerId: number;
  public: boolean;
  pakIds: number[];
  createdAt: string;
  updatedAt: string;
}

export interface MapSummary {
  name: string;
  path: string;
  sha256: string;
  checksum: number;
  message: string;
  sky: string;
  pakId: number;
}

export type GameMode = 'sp' | 'coop' | 'dm' | 'ctf';

export interface GameSpec {
  name?: string;
  mode: GameMode;
  map: string;
  pakset?: string;
  maxPlayers?: number;
  public?: boolean;
  cvars?: Record<string, string>;
  loadSlot?: string;
}

export interface GameInfo {
  id: string;
  name: string;
  ownerId: number;
  mode: GameMode;
  map: string;
  pakset: string;
  public: boolean;
  players: number;
  maxPlayers: number;
  startedAt: string;
}

export interface JoinResponse {
  ticket: string;
  wsUrl: string;
}

export interface SaveInfo {
  slot: string;
  comment: string;
  mapcmd: string;
  mode: string;
  schema: number;
  size: number;
  createdAt: string;
  updatedAt: string;
}

export interface Settings {
  config: string;
  updatedAt?: string;
}

// ---------------------------------------------------------------- bots (docs/plans/0002-ai-agent.md H.5)

/** Decision backends a bot can run with (jev needs an admin). */
export type BotBackend = 'scripted' | 'jev' | 'mock' | 'constant' | 'random';

export type BotStatus = 'starting' | 'running' | 'finished' | 'failed' | 'stopped';

export type BotArtifactKind = 'run' | 'episode' | 'trace' | 'demo';

/** POST /api/v1/bots body. */
export interface BotSpec {
  name?: string;
  /** a prefix of the campaign's visits, starting with its first map (default: the whole campaign) */
  maps?: string[];
  /** 0..3 (default 1) */
  skill?: number;
  backend: BotBackend;
  /** simulated model latency of a lockstep run, e.g. "212ms" */
  simLatency?: string;
  public?: boolean;
}

export interface BotLiveStats {
  kills: number;
  deaths: number;
  decisions: number;
  costUsd: number;
  modelShare: number;
}

export interface BotArtifact {
  /** path inside the run directory: run.json, ep-NNN/episode.json, ep-NNN/trace.jsonl.gz, ep-NNN/demos/NN-<map>.dm2 */
  name: string;
  size: number;
  kind: BotArtifactKind;
}

export interface BotInfo {
  id: string;
  name: string;
  ownerId?: number;
  status: BotStatus;
  backend: string;
  model?: string;
  maps: string[];
  skill: number;
  public: boolean;
  /** RFC 3339 */
  startedAt: string;
  endedAt?: string;
  viewers: number;
  level?: { map: string; visit: number };
  live?: BotLiveStats;
  /** run.json (q2bot.run/1) once the run ended */
  summary?: RunSummary;
  artifacts: BotArtifact[];
}

/** POST /api/v1/bots/{id}/watch: one-time tickets for the video relay and the decision feed. */
export interface BotWatchResponse {
  ticket: string;
  wsUrl: string;
  decisionsTicket: string;
  decisionsUrl: string;
  pakset: string;
}

/** True while a bot's run can be watched live. */
export function botIsLive(status: string): boolean {
  return status === 'starting' || status === 'running';
}

// ---- decision feed messages (WS /ws/v1/bots/{id}/decisions, JSON text frames)

export const DECISIONS_PROTOCOL = 'q2bot.decisions/1';

/** Where the value a decision field acted on came from (trace.Source*). */
export type FieldSource = 'model' | 'scripted' | 'stale' | 'reflex' | 'default';

export interface DecisionOption {
  label: string;
  /** probability 0..1 */
  p: number;
}

export interface DecisionQuestion {
  /** question id: mode, target, fire_policy, movement, weapon, pickup, danger, ... */
  id: string;
  options: DecisionOption[];
  chosen: string;
  confidence?: number;
}

export interface DecisionTarget {
  id: string;
  class: string;
  /** map units ("" class / 0 dist when unknown) */
  dist: number;
}

export interface DecisionAction {
  movement?: string;
  firePolicy?: string;
  weapon?: string;
  fire?: boolean;
}

export interface FeedHello {
  t: 'hello';
  bot: string;
  backend: string;
  model?: string;
  maps: string[];
}

export interface FeedDecision {
  t: 'decision';
  /** the bot's server frame (aligns the decision with the video) */
  sf: number;
  /** level attempt */
  lvl: number;
  map: string;
  mode: string;
  objective?: string;
  target: DecisionTarget | null;
  questions: DecisionQuestion[];
  /** field → FieldSource */
  provenance: Record<string, string>;
  /** optional: field → why the model's answer was not used */
  fallback?: Record<string, string>;
  action: DecisionAction;
  latencyMs?: number;
  costUsd?: number;
}

export type FeedEventKind = 'level_start' | 'level_end' | 'death' | 'reload' | 'kill' | 'damage' | 'stuck' | 'budget';

export interface FeedEvent {
  t: 'event';
  kind: FeedEventKind;
  sf: number;
  data: Record<string, unknown>;
}

export interface FeedStats {
  t: 'stats';
  kills: number;
  deaths: number;
  decisions: number;
  /** decisions per second */
  decisionRate: number;
  modelShare: number;
  staleRate: number;
  apiP50Ms: number;
  costUsd: number;
}

export interface FeedGap {
  t: 'gap';
  dropped: number;
}

export interface FeedBye {
  t: 'bye';
  status: string;
}

export type FeedMessage = FeedHello | FeedDecision | FeedEvent | FeedStats | FeedGap | FeedBye;

/** Error envelope {"error":{"code","message","field"}} as an exception. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly field?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const init: RequestInit = { method, credentials: 'include', headers: { Accept: 'application/json' } };
  if (signal) init.signal = signal;
  if (body !== undefined) {
    init.body = JSON.stringify(body);
    (init.headers as Record<string, string>)['Content-Type'] = 'application/json';
  }
  let res: Response;
  try {
    res = await fetch(path, init);
  } catch (e) {
    if ((e as Error).name === 'AbortError') throw e;
    throw new ApiError(0, 'network', 'cannot reach the game server');
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let json: unknown = undefined;
  if (text) {
    try {
      json = JSON.parse(text);
    } catch {
      // not JSON (e.g. a proxy error page)
    }
  }
  if (!res.ok) {
    const err = (json as { error?: { code?: string; message?: string; field?: string } } | undefined)?.error;
    throw new ApiError(
      res.status,
      err?.code ?? (res.status >= 502 ? 'unavailable' : 'http_' + res.status),
      err?.message ?? (res.status >= 502 ? 'game server unavailable' : `${res.status} ${res.statusText}`),
      err?.field,
    );
  }
  return json as T;
}

const V1 = '/api/v1';

export const api = {
  // ---- auth
  register: (email: string, password: string, displayName: string) =>
    request<{ user: User }>('POST', `${V1}/auth/register`, { email, password, displayName }).then((r) => r.user),
  login: (email: string, password: string) =>
    request<{ user: User }>('POST', `${V1}/auth/login`, { email, password }).then((r) => r.user),
  logout: () => request<void>('POST', `${V1}/auth/logout`),
  /** null when not logged in */
  me: async (): Promise<User | null> => {
    try {
      return (await request<{ user: User }>('GET', `${V1}/auth/me`)).user;
    } catch (e) {
      if (e instanceof ApiError && e.status === 401) return null;
      throw e;
    }
  },

  // ---- paks / paksets
  listPaks: () => request<{ paks: Pak[] }>('GET', `${V1}/paks`).then((r) => r.paks),
  getPak: (id: number) => request<{ pak: Pak }>('GET', `${V1}/paks/${id}`).then((r) => r.pak),
  getJob: (id: number) => request<{ job: Job }>('GET', `${V1}/jobs/${id}`).then((r) => r.job),
  listPaksets: () => request<{ paksets: Pakset[] }>('GET', `${V1}/paksets`).then((r) => r.paksets),
  createPakset: (name: string, paks: number[]) =>
    request<{ pakset: Pakset }>('POST', `${V1}/paksets`, { name, paks }).then((r) => r.pakset),
  updatePakset: (id: string, name: string, paks: number[]) =>
    request<{ pakset: Pakset }>('PUT', `${V1}/paksets/${encodeURIComponent(id)}`, { name, paks }).then(
      (r) => r.pakset,
    ),
  deletePakset: (id: string) => request<void>('DELETE', `${V1}/paksets/${encodeURIComponent(id)}`),
  listMaps: (pakset = 'demo') =>
    request<{ pakset: string; maps: MapSummary[] }>('GET', `${V1}/maps?pakset=${encodeURIComponent(pakset)}`).then(
      (r) => r.maps,
    ),

  // ---- games
  listGames: () => request<{ games: GameInfo[] }>('GET', `${V1}/games`).then((r) => r.games),
  getGame: (id: string) => request<{ game: GameInfo }>('GET', `${V1}/games/${encodeURIComponent(id)}`).then((r) => r.game),
  createGame: (spec: GameSpec) => request<{ game: GameInfo }>('POST', `${V1}/games`, spec).then((r) => r.game),
  joinGame: (id: string) => request<JoinResponse>('POST', `${V1}/games/${encodeURIComponent(id)}/join`),
  deleteGame: (id: string) => request<void>('DELETE', `${V1}/games/${encodeURIComponent(id)}`),

  // ---- saves
  listSaves: () => request<{ saves: SaveInfo[] | null }>('GET', `${V1}/saves`).then((r) => r.saves ?? []),
  deleteSave: (slot: string) => request<void>('DELETE', `${V1}/saves/${encodeURIComponent(slot)}`),
  saveDataUrl: (slot: string) => `${V1}/saves/${encodeURIComponent(slot)}/data`,

  // ---- settings
  getSettings: () => request<Settings>('GET', `${V1}/settings`),
  putSettings: (config: string) => request<Settings>('PUT', `${V1}/settings`, { config }),

  // ---- bots
  listBots: () => request<{ bots: BotInfo[] | null }>('GET', `${V1}/bots`).then((r) => (r.bots ?? []).map(normalizeBot)),
  getBot: (id: string) =>
    request<{ bot: BotInfo }>('GET', `${V1}/bots/${encodeURIComponent(id)}`).then((r) => normalizeBot(r.bot)),
  createBot: (spec: BotSpec) => request<{ bot: BotInfo }>('POST', `${V1}/bots`, spec).then((r) => normalizeBot(r.bot)),
  /** stops a running bot (owner or admin) */
  stopBot: (id: string) => request<void>('DELETE', `${V1}/bots/${encodeURIComponent(id)}`),
  /** one-time tickets for both watch streams (409 when the bot is not live) */
  watchBot: (id: string) => request<BotWatchResponse>('POST', `${V1}/bots/${encodeURIComponent(id)}/watch`),
  artifactUrl: (id: string, name: string) => artifactUrl(id, name),
  /** GET an artifact; rejects with ApiError like the JSON endpoints */
  fetchArtifact: async (id: string, name: string, signal?: AbortSignal): Promise<Response> => {
    const init: RequestInit = { credentials: 'include' };
    if (signal) init.signal = signal;
    let res: Response;
    try {
      res = await fetch(artifactUrl(id, name), init);
    } catch (e) {
      if ((e as Error).name === 'AbortError') throw e;
      throw new ApiError(0, 'network', 'cannot reach the game server');
    }
    if (!res.ok) {
      let code = res.status >= 502 ? 'unavailable' : 'http_' + res.status;
      let message = `${name}: ${res.status} ${res.statusText}`.trim();
      try {
        const j = (await res.json()) as { error?: { code?: string; message?: string } };
        if (j.error?.code) code = j.error.code;
        if (j.error?.message) message = `${name}: ${j.error.message}`;
      } catch {
        // not JSON
      }
      throw new ApiError(res.status, code, message);
    }
    return res;
  },
};

/** The JSON arrays of a Go nil slice arrive as null. */
function normalizeBot(b: BotInfo): BotInfo {
  return { ...b, maps: b.maps ?? [], artifacts: b.artifacts ?? [] };
}

/**
 * URL of a run artifact (GET /api/v1/bots/{id}/artifacts/{name...}). Each path segment is escaped; empty,
 * "." and ".." segments are refused so a name can never resolve outside the artifacts route.
 */
export function artifactUrl(id: string, name: string): string {
  const parts = name.split('/');
  if (!name || parts.some((p) => p === '' || p === '.' || p === '..')) {
    throw new ApiError(0, 'artifact', `bad artifact name ${JSON.stringify(name).slice(0, 80)}`);
  }
  return `${V1}/bots/${encodeURIComponent(id)}/artifacts/${parts.map(encodeURIComponent).join('/')}`;
}

export interface UploadProgress {
  loaded: number;
  total: number;
}

/**
 * POST /api/v1/paks (multipart field "file") with upload progress. fetch() has no upload progress
 * events, so this uses XMLHttpRequest. Resolves with {pak, owned, job?}.
 */
export function uploadPak(
  file: File,
  onProgress: (p: UploadProgress) => void,
  signal?: AbortSignal,
): Promise<{ pak: Pak; owned: boolean; job?: Job }> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('POST', `${V1}/paks`);
    xhr.withCredentials = true;
    xhr.responseType = 'text';
    xhr.upload.onprogress = (e) => onProgress({ loaded: e.loaded, total: e.lengthComputable ? e.total : file.size });
    xhr.onerror = () => reject(new ApiError(0, 'network', 'upload failed (network error)'));
    xhr.onabort = () => reject(new DOMException('upload aborted', 'AbortError'));
    xhr.onload = () => {
      let json: unknown;
      try {
        json = JSON.parse(xhr.responseText);
      } catch {
        json = undefined;
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(json as { pak: Pak; owned: boolean; job?: Job });
      else {
        const err = (json as { error?: { code?: string; message?: string } } | undefined)?.error;
        reject(new ApiError(xhr.status, err?.code ?? 'http_' + xhr.status, err?.message ?? `upload failed (${xhr.status})`));
      }
    };
    signal?.addEventListener('abort', () => xhr.abort());
    const fd = new FormData();
    fd.append('file', file, file.name);
    xhr.send(fd);
  });
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message;
  if (e instanceof Error) return e.message;
  return String(e);
}
