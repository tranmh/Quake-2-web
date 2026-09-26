// Hosts a SoundCore: applies sfx uploads and command batches in the exact order the main thread issued
// them. Every message (sfx upload / free, or command batch) carries a sequence number 1, 2, 3, ...; the
// host applies them strictly in sequence, so the two transports (postMessage for sample data, postMessage
// or a SharedArrayBuffer ring for batches) cannot reorder "load sfx N" against the commands using it.
// Only S_Update (inside a batch) paints; the audio callback just reads the DMA buffer, so splitting one
// client frame's commands over several batches does not change the output.
import { SoundCore, type CoreSfx, type SoundCoreOptions } from './snd_core';
import { applyBatch } from './protocol';
import type { SabRing } from './ring';

type Item = { kind: 'sfx'; id: number; sfx: CoreSfx | null } | { kind: 'batch'; batch: Uint8Array };

export class CoreHost {
  readonly core: SoundCore;
  private readonly pending = new Map<number, Item>();
  private next = 1;
  /** batches that threw (see pump) */
  errors = 0;

  constructor(opts: SoundCoreOptions) {
    this.core = new SoundCore(opts);
  }

  /** An sfx upload (sfx null = the slot was freed). */
  onSfx(seq: number, id: number, sfx: CoreSfx | null): void {
    this.pending.set(seq, { kind: 'sfx', id, sfx });
    this.pump();
  }

  onBatch(seq: number, batch: Uint8Array): void {
    this.pending.set(seq, { kind: 'batch', batch });
    this.pump();
  }

  private pump(): void {
    for (;;) {
      const it = this.pending.get(this.next);
      if (!it) return;
      this.pending.delete(this.next);
      this.next++;
      try {
        if (it.kind === 'sfx') {
          if (it.sfx) this.core.sfx.set(it.id, it.sfx);
          else this.core.sfx.delete(it.id);
        } else applyBatch(this.core, it.batch);
      } catch (e) {
        // an exception escaping AudioWorkletProcessor.process() kills the processor for good (silence
        // until reload): drop the rest of this batch and keep mixing
        this.errors++;
        if (this.errors <= 8)
          this.core.dprint(`q2-sound: batch failed: ${e instanceof Error ? e.message : String(e)}\n`);
      }
    }
  }

  /** Drains a SharedArrayBuffer ring of batches. */
  drainRing(ring: SabRing): void {
    for (;;) {
      const seq = ring.peekSeq();
      if (seq < 0) break;
      this.pending.set(seq, { kind: 'batch', batch: ring.pop()! });
    }
    this.pump();
  }
}
