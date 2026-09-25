// Messages and options shared by the worklet (worklet.ts) and its loader (web.ts).
import type { CoreSfx } from './snd_core';

export const PROCESSOR_NAME = 'q2-sound';

export interface ProcessorOptions {
  /** dma.speed */
  speed: number;
  /** SharedArrayBuffer of a SabRing (crossOriginIsolated pages) */
  ring?: SharedArrayBuffer;
  portableC8bit?: boolean;
}

export type ToWorklet =
  | { type: 'sfx'; seq: number; id: number; sfx: CoreSfx | null }
  | { type: 'batch'; seq: number; batch: Uint8Array };

export type FromWorklet = { type: 'print'; text: string; developer: boolean };
