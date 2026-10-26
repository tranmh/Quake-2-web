// Shared test doubles and builders (aqaa). Not collected as a test file itself.
import { vi } from 'vitest';
import type { AssetIndex, IndexEntry, MapInfo } from '../../src/lib/assets';

/** Deterministic 64-hex sha256 derived from a seed character/number. */
export function fakeSha(seed: number | string = 0): string {
  const hex = typeof seed === 'number' ? seed.toString(16) : Buffer.from(seed).toString('hex');
  return hex.padStart(64, '0').slice(-64);
}

export function buildIndexEntry(over: Partial<IndexEntry> = {}): IndexEntry {
  return { path: 'pics/conback.pcx', sha256: fakeSha(1), size: 1024, kind: 'pcx', pak: 0, ...over };
}

export function buildMapInfo(over: Partial<MapInfo> = {}): MapInfo {
  return { name: 'base1', path: 'maps/base1.bsp', sha256: fakeSha(2), message: 'Outer Base', sky: 'unit1_', ...over };
}

export function buildAssetIndex(over: Partial<AssetIndex> = {}): AssetIndex {
  const entry = buildIndexEntry();
  return {
    schema: 1,
    pakset: 'baseq2',
    paks: [{ id: 0, name: 'pak0.pak', sha256: fakeSha(3), size: 4096, numFiles: 1 }],
    files: { [entry.path]: entry },
    maps: [buildMapInfo()],
    ...over,
  };
}

export interface FakeRoute {
  status?: number;
  body?: unknown;
}

/** Fake global fetch: routes keyed by URL (exact match); unknown URLs answer 404. Restore with vi.unstubAllGlobals(). */
export function stubFetch(routes: Record<string, FakeRoute | FakeRoute[]>) {
  const queues = new Map(Object.entries(routes).map(([k, v]) => [k, Array.isArray(v) ? [...v] : [v]]));
  const fn = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const q = queues.get(url);
    const r = q ? (q.length > 1 ? q.shift()! : q[0]) : { status: 404, body: { error: 'not_found' } };
    const body = r.body instanceof ArrayBuffer || typeof r.body === 'string' ? r.body : JSON.stringify(r.body ?? null);
    return new Response(body as BodyInit, { status: r.status ?? 200, headers: { 'content-type': 'application/json' } });
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}
