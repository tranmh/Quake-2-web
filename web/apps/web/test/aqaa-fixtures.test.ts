import { afterEach, describe, expect, it, vi } from 'vitest';
import { assetUrl, fetchAssetIndex, indexKey } from '../src/lib/assets';
import { buildAssetIndex, fakeSha, stubFetch } from './__tests__/fixtures';

afterEach(() => vi.unstubAllGlobals());

describe('aqaa fixtures', () => {
  it('builds a valid asset index and serves it through the fake fetch', async () => {
    const index = buildAssetIndex({ pakset: 'fixture-set' });
    expect(assetUrl(fakeSha(1))).toBe(`/assets/${fakeSha(1)}`);
    expect(index.files[indexKey('PICS/Conback.pcx')]).toBeDefined();
    const fetchFn = stubFetch({ '/api/v1/paksets/fixture-set/index': { body: index } });
    await expect(fetchAssetIndex('fixture-set')).resolves.toEqual(index);
    expect(fetchFn).toHaveBeenCalledTimes(1);
  });
});
