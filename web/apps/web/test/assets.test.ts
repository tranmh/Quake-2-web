// Asset index + content-addressed loading: index keys, hash URLs, the index fetch (retry, errors, cache)
// and the loader's memory LRU → Cache API → network chain, with fetch and caches faked.
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '@/lib/api';
import { AssetLoader, assetUrl, fetchAssetIndex, indexKey, picUrl, type AssetIndex } from '@/lib/assets';

const hash = (n: number): string => n.toString(16).padStart(64, '0');

function makeIndex(files: Record<string, { sha256: string; png?: string }> = {}): AssetIndex {
  return {
    schema: 1,
    pakset: 'base',
    paks: [],
    files: Object.fromEntries(
      Object.entries(files).map(([k, v]) => [
        k,
        {
          path: k,
          sha256: v.sha256,
          size: 0,
          kind: 'bin',
          pak: 0,
          ...(v.png
            ? { png: { sha256: v.png, size: 1, width: 1, height: 1, hasAlpha: false, type: 'image/png' } }
            : {}),
        },
      ]),
    ),
    maps: [],
  };
}

/** fetch fake serving blobs by URL; anything else is a 404. */
function blobServer(blobs: Record<string, Uint8Array>) {
  const fn = vi.fn(async (url: string) => {
    const b = blobs[url];
    if (!b) return new Response('nope', { status: 404 });
    return new Response(b.slice(), { status: 200, headers: { 'Content-Type': 'application/x-test' } });
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe('indexKey', () => {
  it('lowercases ASCII letters only', () => {
    expect(indexKey('PICS/Conchars.PCX')).toBe('pics/conchars.pcx');
    expect(indexKey('maps/base1.bsp')).toBe('maps/base1.bsp');
    // non-ASCII letters are left alone (Q_strcasecmp semantics)
    expect(indexKey('ÄÖÜ/É.wav')).toBe('ÄÖÜ/É.wav');
    expect(indexKey('')).toBe('');
  });
});

describe('assetUrl', () => {
  it('maps a sha256 to its asset path', () => {
    expect(assetUrl(hash(1))).toBe(`/assets/${hash(1)}`);
  });

  it.each([
    ['too short', 'a'.repeat(63)],
    ['too long', 'a'.repeat(65)],
    ['upper case', 'A'.repeat(64)],
    ['path traversal', '../'.repeat(21) + 'a'],
    ['empty', ''],
  ])('rejects a bad hash (%s)', (_, h) => {
    expect(() => assetUrl(h)).toThrow(ApiError);
    try {
      assetUrl(h);
    } catch (e) {
      expect((e as ApiError).code).toBe('asset');
      expect((e as ApiError).status).toBe(0);
    }
  });

  it('truncates a huge hash in the error message', () => {
    try {
      assetUrl('x'.repeat(500));
      expect.unreachable();
    } catch (e) {
      expect((e as Error).message.length).toBeLessThan(120);
    }
  });
});

describe('fetchAssetIndex', () => {
  it('fetches and caches the index per pakset', async () => {
    const idx = makeIndex();
    const fn = vi.fn(async () => Response.json(idx));
    vi.stubGlobal('fetch', fn);
    const a = await fetchAssetIndex('ok-1');
    const b = await fetchAssetIndex('ok-1');
    expect(a).toEqual(idx);
    expect(b).toBe(a);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn.mock.calls[0]![0]).toBe('/api/v1/paksets/ok-1/index');
    expect(fn.mock.calls[0]![1]).toMatchObject({ credentials: 'include' });
  });

  it('url-encodes the pakset id and forwards the abort signal', async () => {
    const fn = vi.fn(async () => Response.json(makeIndex()));
    vi.stubGlobal('fetch', fn);
    const ctl = new AbortController();
    await fetchAssetIndex('a b/c', ctl.signal);
    expect(fn.mock.calls[0]![0]).toBe('/api/v1/paksets/a%20b%2Fc/index');
    expect((fn.mock.calls[0]![1] as RequestInit).signal).toBe(ctl.signal);
  });

  it('turns an error envelope into an ApiError and forgets the failure', async () => {
    const fn = vi
      .fn()
      .mockResolvedValueOnce(
        Response.json({ error: { code: 'not_found', message: 'no such pakset' } }, { status: 404 }),
      )
      .mockResolvedValueOnce(Response.json(makeIndex()));
    vi.stubGlobal('fetch', fn);
    const err = await fetchAssetIndex('err-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 404, code: 'not_found', message: 'asset index for pakset "err-1": no such pakset' });
    // the rejected promise is not cached: a second call fetches again
    await expect(fetchAssetIndex('err-1')).resolves.toEqual(makeIndex());
    expect(fn).toHaveBeenCalledTimes(2);
  });

  it('falls back to the HTTP status when the error body is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('<html>oops</html>', { status: 500 })));
    await expect(fetchAssetIndex('err-2')).rejects.toMatchObject({
      status: 500,
      code: 'http_500',
      message: 'asset index for pakset "err-2": 500',
    });
  });

  it('retries while the pakset is still ingesting (409)', async () => {
    vi.useFakeTimers();
    const fn = vi
      .fn()
      .mockResolvedValueOnce(new Response('', { status: 409 }))
      .mockResolvedValueOnce(new Response('', { status: 409 }))
      .mockResolvedValueOnce(Response.json(makeIndex()));
    vi.stubGlobal('fetch', fn);
    const p = fetchAssetIndex('retry-1');
    await vi.advanceTimersByTimeAsync(2000);
    expect(fn).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(2000);
    await expect(p).resolves.toEqual(makeIndex());
    expect(fn).toHaveBeenCalledTimes(3);
  });

  it('gives up after 60 retries and reports the 409', async () => {
    vi.useFakeTimers();
    const fn = vi.fn(async () => Response.json({ error: { code: 'not_ready' } }, { status: 409 }));
    vi.stubGlobal('fetch', fn);
    const p = fetchAssetIndex('retry-2').catch((e: unknown) => e);
    await vi.advanceTimersByTimeAsync(60 * 2000);
    const err = await p;
    expect(err).toMatchObject({ status: 409, code: 'not_ready' });
    expect(fn).toHaveBeenCalledTimes(61);
  });
});

describe('AssetLoader', () => {
  const A = hash(0xa);
  const B = hash(0xb);

  it('loads a file by case-insensitive path from the network and counts it', async () => {
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([1, 2, 3]) });
    const loader = new AssetLoader(makeIndex({ 'pics/a.pcx': { sha256: A } }));
    const downloads: [string, number][] = [];
    loader.onDownload = (p, n) => downloads.push([p, n]);
    expect(loader.entry('PICS/A.PCX')?.sha256).toBe(A);
    const data = await loader.loadFile('PICS/A.pcx');
    expect(Array.from(data!)).toEqual([1, 2, 3]);
    expect(fn).toHaveBeenCalledWith(`/assets/${A}`, { credentials: 'include' });
    expect(downloads).toEqual([['PICS/A.pcx', 3]]);
    expect(loader.stats).toMatchObject({ requests: 1, network: 1, networkBytes: 3, missing: 0 });
  });

  it('returns null for a path no pak contains', async () => {
    const fn = blobServer({});
    const loader = new AssetLoader(makeIndex());
    await expect(loader.loadFile('maps/nope.bsp')).resolves.toBeNull();
    expect(loader.stats).toMatchObject({ requests: 1, missing: 1, network: 0 });
    expect(fn).not.toHaveBeenCalled();
  });

  it('serves repeated loads from memory and hands out independent copies', async () => {
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([9, 9]) });
    const loader = new AssetLoader(makeIndex({ a: { sha256: A } }));
    const first = (await loader.loadFile('a'))!;
    first[0] = 0;
    const second = (await loader.loadFile('a'))!;
    expect(Array.from(second)).toEqual([9, 9]);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(loader.stats.memoryHits).toBe(1);
  });

  it('shares one download between concurrent loads of the same blob', async () => {
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([5]) });
    const loader = new AssetLoader(makeIndex({ a: { sha256: A }, b: { sha256: A } }));
    const [x, y] = await Promise.all([loader.loadFile('a'), loader.loadFile('b')]);
    expect(Array.from(x!)).toEqual([5]);
    expect(Array.from(y!)).toEqual([5]);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('throws an ApiError on an HTTP error and retries on the next load', async () => {
    const fn = blobServer({});
    const loader = new AssetLoader(makeIndex({ a: { sha256: A } }));
    await expect(loader.loadFile('a')).rejects.toMatchObject({ status: 404, code: 'asset', message: 'asset a: HTTP 404' });
    await expect(loader.loadFile('a')).rejects.toBeInstanceOf(ApiError);
    expect(fn).toHaveBeenCalledTimes(2);
    expect(loader.stats.network).toBe(0);
  });

  it('rejects an index entry with a malformed hash without fetching', async () => {
    const fn = blobServer({});
    const loader = new AssetLoader(makeIndex({ a: { sha256: '../../etc/passwd' } }));
    await expect(loader.loadFile('a')).rejects.toBeInstanceOf(ApiError);
    expect(fn).not.toHaveBeenCalled();
  });

  it('evicts the least recently used blobs beyond the memory budget', async () => {
    const C = hash(0xc);
    const fn = blobServer({
      [`/assets/${A}`]: new Uint8Array(10),
      [`/assets/${B}`]: new Uint8Array(10),
      [`/assets/${C}`]: new Uint8Array(10),
    });
    // budget 40: single blobs up to 10 bytes are kept, at most 20 bytes stay
    const loader = new AssetLoader(makeIndex(), 20 * 2);
    await loader.blob(A);
    await loader.blob(B);
    await loader.blob(C);
    await loader.blob(A); // memory hit; A becomes most recent
    expect(loader.stats.memoryHits).toBe(1);
    const D = hash(0xd);
    const E = hash(0xe);
    fn.mockImplementation(async () => new Response(new Uint8Array(10)));
    await loader.blob(D); // 40 bytes: still fits
    await loader.blob(E); // 50 bytes: evicts B (least recent)
    fn.mockClear();
    await loader.blob(A);
    await loader.blob(C);
    expect(fn).not.toHaveBeenCalled();
    await loader.blob(B);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('does not keep a blob larger than a quarter of the budget in memory', async () => {
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array(11) });
    const loader = new AssetLoader(makeIndex(), 40);
    await loader.blob(A);
    await loader.blob(A);
    expect(fn).toHaveBeenCalledTimes(2);
    expect(loader.stats.memoryHits).toBe(0);
  });

  it('reads from the Cache API before the network and fills it after a download', async () => {
    const store = new Map<string, Response>();
    const cache = {
      match: vi.fn(async (url: string) => store.get(url)?.clone()),
      put: vi.fn(async (url: string, res: Response) => {
        store.set(url, res);
      }),
    };
    const open = vi.fn(async () => cache);
    vi.stubGlobal('caches', { open });
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([7, 8]) });
    store.set(`/assets/${B}`, new Response(new Uint8Array([4])));

    const loader = new AssetLoader(makeIndex({ a: { sha256: A }, b: { sha256: B } }));
    expect(Array.from((await loader.loadFile('b'))!)).toEqual([4]);
    expect(loader.stats.cacheHits).toBe(1);
    expect(fn).not.toHaveBeenCalled();

    expect(Array.from((await loader.loadFile('a'))!)).toEqual([7, 8]);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(cache.put).toHaveBeenCalledTimes(1);
    const put = store.get(`/assets/${A}`)!;
    expect(put.headers.get('Content-Type')).toBe('application/x-test');
    expect(Array.from(new Uint8Array(await put.clone().arrayBuffer()))).toEqual([7, 8]);
    expect(open).toHaveBeenCalledTimes(1);
    expect(open).toHaveBeenCalledWith('q2-assets-v1');

    // a fresh loader (new session) finds the downloaded blob in the cache
    const next = new AssetLoader(loader.index);
    await next.loadFile('a');
    expect(next.stats.cacheHits).toBe(1);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('falls through to the network when the Cache API fails', async () => {
    vi.stubGlobal('caches', {
      open: vi.fn(async () => ({
        match: vi.fn(async () => {
          throw new Error('quota');
        }),
        put: vi.fn(async () => {
          throw new Error('quota');
        }),
      })),
    });
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([1]) });
    const loader = new AssetLoader(makeIndex({ a: { sha256: A } }));
    expect(Array.from((await loader.loadFile('a'))!)).toEqual([1]);
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it('works without the Cache API when opening it fails', async () => {
    vi.stubGlobal('caches', { open: vi.fn(async () => Promise.reject(new Error('denied'))) });
    const fn = blobServer({ [`/assets/${A}`]: new Uint8Array([2]) });
    const loader = new AssetLoader(makeIndex({ a: { sha256: A } }));
    expect(Array.from((await loader.loadFile('a'))!)).toEqual([2]);
    expect(fn).toHaveBeenCalledTimes(1);
  });
});

describe('picUrl', () => {
  const P = hash(0x9);
  const idx = makeIndex({
    'pics/m_banner.pcx': { sha256: hash(1), png: P },
    'pics/raw.pcx': { sha256: hash(2) },
    'env/sky.pcx': { sha256: hash(3), png: P },
    'pics/bad.pcx': { sha256: hash(4), png: 'not-a-hash' },
  });

  it('resolves a short pic name and a full path to the PNG rendition', () => {
    expect(picUrl(idx, 'M_Banner')).toBe(`/assets/${P}`);
    expect(picUrl(idx, 'ENV/sky.pcx')).toBe(`/assets/${P}`);
  });

  it('returns null without an index, a PNG rendition, an entry or a valid hash', () => {
    expect(picUrl(null, 'm_banner')).toBeNull();
    expect(picUrl(idx, 'raw')).toBeNull();
    expect(picUrl(idx, 'missing')).toBeNull();
    expect(picUrl(idx, 'bad')).toBeNull();
  });
});
