// Asset index + content-addressed file loading (docs/ASSETS.md).
//
// loadFile(virtualPath) = index.files[asciiLower(path)] → GET /assets/{sha256}. Blobs are immutable by
// hash, so they are cached forever in the Cache API (never revalidated) and additionally kept in a
// small in-memory LRU for repeated loads within a session (e.g. re-registration on map change).
import { ApiError } from './api';

export interface IndexPng {
  sha256: string;
  size: number;
  width: number;
  height: number;
  hasAlpha: boolean;
  type: string;
}

export interface IndexEntry {
  path: string;
  sha256: string;
  size: number;
  kind: string;
  pak: number;
  image?: { width: number; height: number };
  png?: IndexPng;
  error?: string;
}

export interface MapInfo {
  name: string;
  path: string;
  sha256: string;
  message: string;
  sky: string;
}

export interface AssetIndex {
  schema: number;
  pakset: string;
  paks: { id: number; name: string; sha256: string; size: number; numFiles: number }[];
  palette?: { sha256: string; source: string };
  files: Record<string, IndexEntry>;
  maps: MapInfo[];
}

/** Q_strcasecmp key: ASCII A–Z → a–z only (docs/ASSETS.md). */
export function indexKey(path: string): string {
  return path.replace(/[A-Z]/g, (c) => String.fromCharCode(c.charCodeAt(0) + 32));
}

export function assetUrl(sha256: string): string {
  // the hash comes from the server's JSON index: never let it turn into another path or a CSS url() break-out
  if (!/^[0-9a-f]{64}$/.test(sha256)) throw new ApiError(0, 'asset', `bad asset hash ${JSON.stringify(sha256).slice(0, 80)}`);
  return `/assets/${sha256}`;
}

const indexCache = new Map<string, Promise<AssetIndex>>();

/** GET /api/v1/paksets/{id}/index (retries while a pak is still ingesting: 409 not_ready). */
export function fetchAssetIndex(pakset: string, signal?: AbortSignal): Promise<AssetIndex> {
  let p = indexCache.get(pakset);
  if (!p) {
    p = (async () => {
      for (let attempt = 0; ; attempt++) {
        const init: RequestInit = { credentials: 'include' };
        if (signal) init.signal = signal;
        const res = await fetch(`/api/v1/paksets/${encodeURIComponent(pakset)}/index`, init);
        if (res.status === 409 && attempt < 60) {
          await new Promise((r) => setTimeout(r, 2000));
          continue;
        }
        if (!res.ok) {
          let msg = `asset index for pakset "${pakset}": ${res.status}`;
          let code = 'http_' + res.status;
          try {
            const j = (await res.json()) as { error?: { code?: string; message?: string } };
            if (j.error?.message) msg = `asset index for pakset "${pakset}": ${j.error.message}`;
            if (j.error?.code) code = j.error.code;
          } catch {
            // ignore
          }
          throw new ApiError(res.status, code, msg);
        }
        return (await res.json()) as AssetIndex;
      }
    })();
    indexCache.set(pakset, p);
    p.catch(() => indexCache.delete(pakset));
  }
  return p;
}

const CACHE_NAME = 'q2-assets-v1';

/** Byte-budgeted LRU (Map iteration order = insertion order). */
class LRU {
  private readonly map = new Map<string, Uint8Array>();
  private bytes = 0;
  constructor(private readonly budget: number) {}
  get(k: string): Uint8Array | undefined {
    const v = this.map.get(k);
    if (v) {
      this.map.delete(k);
      this.map.set(k, v);
    }
    return v;
  }
  set(k: string, v: Uint8Array): void {
    if (v.byteLength > this.budget / 4) return; // do not let one huge file flush everything
    const old = this.map.get(k);
    if (old) {
      this.bytes -= old.byteLength;
      this.map.delete(k);
    }
    this.map.set(k, v);
    this.bytes += v.byteLength;
    for (const [key, val] of this.map) {
      if (this.bytes <= this.budget) break;
      this.map.delete(key);
      this.bytes -= val.byteLength;
    }
  }
}

export interface AssetLoaderStats {
  requests: number;
  network: number;
  networkBytes: number;
  cacheHits: number;
  memoryHits: number;
  missing: number;
}

export class AssetLoader {
  private readonly lru: LRU;
  private readonly inflight = new Map<string, Promise<Uint8Array>>();
  private cache: Promise<Cache | null> | null = null;
  readonly stats: AssetLoaderStats = { requests: 0, network: 0, networkBytes: 0, cacheHits: 0, memoryHits: 0, missing: 0 };
  /** Called after each network download (for loading progress UI). */
  onDownload: ((path: string, bytes: number) => void) | null = null;

  constructor(
    readonly index: AssetIndex,
    memoryBudget = 96 << 20,
  ) {
    this.lru = new LRU(memoryBudget);
  }

  entry(path: string): IndexEntry | undefined {
    return this.index.files[indexKey(path)];
  }

  /** FS_LoadFile: bytes of a virtual path, or null if no pak of the set contains it. */
  readonly loadFile = async (path: string): Promise<Uint8Array | null> => {
    this.stats.requests++;
    const e = this.entry(path);
    if (!e) {
      this.stats.missing++;
      return null;
    }
    const data = await this.blob(e.sha256, path);
    // hand out a copy: loaders may keep or mutate the buffer, the LRU keeps the original
    return data.slice();
  };

  /** Raw blob bytes by hash (memory LRU → Cache API → network). */
  async blob(sha256: string, label = sha256): Promise<Uint8Array> {
    const mem = this.lru.get(sha256);
    if (mem) {
      this.stats.memoryHits++;
      return mem;
    }
    let p = this.inflight.get(sha256);
    if (!p) {
      p = this.fetchBlob(sha256, label).finally(() => this.inflight.delete(sha256));
      this.inflight.set(sha256, p);
    }
    const data = await p;
    this.lru.set(sha256, data);
    return data;
  }

  private openCache(): Promise<Cache | null> {
    if (!this.cache) {
      this.cache =
        typeof caches === 'undefined' ? Promise.resolve(null) : caches.open(CACHE_NAME).catch(() => null);
    }
    return this.cache;
  }

  private async fetchBlob(sha256: string, label: string): Promise<Uint8Array> {
    const url = assetUrl(sha256);
    const cache = await this.openCache();
    if (cache) {
      try {
        const hit = await cache.match(url);
        if (hit) {
          this.stats.cacheHits++;
          return new Uint8Array(await hit.arrayBuffer());
        }
      } catch {
        // cache unavailable (private mode quota etc.): fall through to the network
      }
    }
    const res = await fetch(url, { credentials: 'include' });
    if (!res.ok) throw new ApiError(res.status, 'asset', `asset ${label}: HTTP ${res.status}`);
    const buf = await res.arrayBuffer();
    this.stats.network++;
    this.stats.networkBytes += buf.byteLength;
    this.onDownload?.(label, buf.byteLength);
    if (cache) {
      const headers = new Headers({ 'Content-Type': res.headers.get('Content-Type') ?? 'application/octet-stream' });
      cache.put(url, new Response(buf.slice(0), { headers })).catch(() => {});
    }
    return new Uint8Array(buf);
  }
}

/** PNG rendition URL of a pic (`pics/<name>.pcx`), for the React shell (menus, banners, fonts). */
export function picUrl(index: AssetIndex | null, name: string): string | null {
  if (!index) return null;
  const path = name.includes('/') ? name : `pics/${name}.pcx`;
  const e = index.files[indexKey(path)];
  if (!e?.png) return null;
  try {
    return assetUrl(e.png.sha256);
  } catch {
    return null;
  }
}
