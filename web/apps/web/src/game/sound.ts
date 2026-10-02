// Sound integration: q2-sound's AudioWorklet mixer when available. The worklet module is prebuilt to
// /q2-sound-worklet.js (scripts/build-worklet.mjs). Without it (or without Web Audio) the game runs
// silently with q2-client's NullSound. Audio starts on the first user gesture on the page.
import type { Sound } from 'q2-client';

export const WORKLET_URL = '/q2-sound-worklet.js';

export async function createSound(
  gestureTarget: EventTarget,
): Promise<{ sound: Sound | null; enabled: boolean }> {
  if (typeof AudioContext === 'undefined' || typeof AudioWorkletNode === 'undefined')
    return { sound: null, enabled: false };
  try {
    const head = await fetch(WORKLET_URL, { method: 'HEAD' });
    if (!head.ok) return { sound: null, enabled: false };
    const mod = await import('q2-sound');
    if (typeof mod.createWebSound !== 'function') return { sound: null, enabled: false };
    const sound = mod.createWebSound({
      workletUrl: WORKLET_URL,
      // the page (not only the canvas): clicking "Resume" or pressing a key also unlocks audio
      gestureTarget: typeof window !== 'undefined' ? window : gestureTarget,
      onError: (e) => console.warn('sound unavailable', e),
    });
    return { sound: sound as unknown as Sound, enabled: true };
  } catch (e) {
    console.warn('sound disabled', e);
    return { sound: null, enabled: false };
  }
}
