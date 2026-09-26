'use client';
import { useEffect, useState } from 'react';
import { fetchAssetIndex, type AssetIndex } from './assets';
import { DEFAULT_PAKSET } from './env';

/** The (public demo) asset index for shell decorations; null until loaded or when unavailable. */
export function useAssetIndex(pakset = DEFAULT_PAKSET): AssetIndex | null {
  const [index, setIndex] = useState<AssetIndex | null>(null);
  useEffect(() => {
    let live = true;
    fetchAssetIndex(pakset).then(
      (i) => live && setIndex(i),
      () => live && setIndex(null),
    );
    return () => {
      live = false;
    };
  }, [pakset]);
  return index;
}
