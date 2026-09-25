// In-process backend: the SoundCore runs on the calling thread with a clock-driven fake DMA position.
// Used by createNullSound() (node, tests, headless clients) -- the mixer output is computed exactly as
// in the browser but not played.
import { CoreHost } from './host';
import type { CoreSfx, SoundCore, SoundCoreOptions } from './snd_core';
import type { SoundBackend } from './snd_dma';

export interface OfflineBackendOptions {
  /**
   * SNDDMA_GetDMAPos replacement in sample frames played since init (default: wall clock via
   * `milliseconds`). The DMA position is (frames * channels) & (dma.samples - 1).
   */
  framesPlayed?: () => number;
  /** clock in ms for the default framesPlayed (default performance.now / Date.now) */
  milliseconds?: () => number;
  core?: Omit<SoundCoreOptions, 'speed' | 'getDMAPos'>;
}

export class OfflineBackend implements SoundBackend {
  host: CoreHost | null = null;
  private t0 = 0;
  print: ((msg: string, developer: boolean) => void) | null = null;
  constructor(private readonly o: OfflineBackendOptions = {}) {}

  get core(): SoundCore | null {
    return this.host?.core ?? null;
  }

  get info():
    { channels: number; samples: number; samplebits: number; submission_chunk: number } | undefined {
    return this.host?.core.dma;
  }

  init(speed: number): boolean {
    const clock =
      this.o.milliseconds ?? (() => (typeof performance !== 'undefined' ? performance.now() : Date.now()));
    this.t0 = clock();
    const frames = this.o.framesPlayed ?? (() => Math.floor(((clock() - this.t0) * speed) / 1000));
    const host: CoreHost = new CoreHost({
      ...this.o.core,
      speed,
      print: (m) => this.print?.(m, false),
      dprint: (m) => this.print?.(m, true),
      getDMAPos: (): number => {
        const dma = host.core.dma;
        return (frames() * dma.channels) & (dma.samples - 1);
      },
    });
    this.host = host;
    return true;
  }

  shutdown(): void {
    this.host = null;
  }

  sfx(seq: number, id: number, sfx: CoreSfx | null): void {
    this.host?.onSfx(seq, id, sfx);
  }

  batch(seq: number, batch: Uint8Array): void {
    this.host?.onBatch(seq, batch);
  }
}
