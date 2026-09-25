// Browser backend: lazily creates the AudioContext at dma.speed (s_khz) on the first user gesture, loads
// the worklet (worklet.ts) and forwards sfx uploads and command batches to it. Batches go through a
// SharedArrayBuffer ring when the page is crossOriginIsolated (drained at the start of every audio
// quantum), otherwise through postMessage. Messages are renumbered here so the worklet sees a gapless
// sequence; everything issued before the worklet exists is dropped except the resident sample data,
// which is uploaded when the worklet starts.
import { SabRing } from './ring';
import type { CoreSfx } from './snd_core';
import type { SoundBackend } from './snd_dma';
import { PROCESSOR_NAME, type FromWorklet, type ProcessorOptions, type ToWorklet } from './worklet_types';

export interface WebBackendOptions {
  /**
   * URL of the bundled worklet module (src/worklet.ts). With Vite:
   * `import workletUrl from 'q2-sound/worklet?worker&url'`. Default: `new URL('./worklet.ts', import.meta.url)`
   * (works with dev servers that transform TypeScript on request).
   */
  workletUrl?: string | URL;
  /** Element/window whose first pointerdown/keydown/touchend starts audio (default: window). */
  gestureTarget?: EventTarget | null;
  /** Use a SharedArrayBuffer ring when crossOriginIsolated (default true). */
  useSharedArrayBuffer?: boolean;
  ringBytes?: number;
  /** see SoundCoreOptions.portableC8bit */
  portableC8bit?: boolean;
  /** Called when audio could not be started (e.g. no Web Audio). */
  onError?: (e: unknown) => void;
}

const GESTURES = ['pointerdown', 'keydown', 'touchend', 'mousedown'];

export class WebAudioBackend implements SoundBackend {
  ctx: AudioContext | null = null;
  node: AudioWorkletNode | null = null;
  private ring: SabRing | null = null;
  private speed = 11025;
  private ready = false;
  private starting: Promise<void> | null = null;
  private outSeq = 0;
  private readonly resident = new Map<number, CoreSfx>();
  private gestureTarget: EventTarget | null = null;
  private readonly onGesture = (): void => {
    void this.start();
  };
  print: ((msg: string, developer: boolean) => void) | null = null;
  readonly info = { channels: 2, samples: 0x10000 / 2, samplebits: 16, submission_chunk: 1 };

  constructor(private readonly o: WebBackendOptions = {}) {}

  init(speed: number): boolean {
    if (typeof AudioContext === 'undefined' || typeof AudioWorkletNode === 'undefined') return false;
    this.speed = speed;
    this.ready = false;
    this.outSeq = 0;
    this.resident.clear();
    const target =
      this.o.gestureTarget === undefined ? (globalThis as unknown as EventTarget) : this.o.gestureTarget;
    if (target && typeof target.addEventListener === 'function') {
      this.gestureTarget = target;
      for (const g of GESTURES) target.addEventListener(g, this.onGesture, { capture: true });
    }
    return true;
  }

  private removeGestures(): void {
    const t = this.gestureTarget;
    if (!t) return;
    for (const g of GESTURES) t.removeEventListener(g, this.onGesture, { capture: true });
    this.gestureTarget = null;
  }

  /** Creates the AudioContext + worklet (call from a user gesture; called automatically on the first one). */
  start(): Promise<void> {
    if (this.starting) return this.starting;
    this.removeGestures();
    const speed = this.speed;
    this.starting = (async () => {
      let ctx: AudioContext;
      try {
        ctx = new AudioContext({ sampleRate: speed, latencyHint: 'interactive' });
      } catch {
        ctx = new AudioContext({ latencyHint: 'interactive' }); // worklet resamples (TODO-IMPROVE)
      }
      this.ctx = ctx;
      try {
        const url = this.o.workletUrl ?? new URL('./worklet.ts', import.meta.url);
        await ctx.audioWorklet.addModule(url);
        const sab =
          (this.o.useSharedArrayBuffer ?? true) &&
          typeof SharedArrayBuffer !== 'undefined' &&
          (globalThis as { crossOriginIsolated?: boolean }).crossOriginIsolated === true;
        this.ring = sab ? SabRing.create(this.o.ringBytes ?? 1 << 20) : null;
        const processorOptions: ProcessorOptions = {
          speed,
          ...(this.ring ? { ring: this.ring.sab } : {}),
          ...(this.o.portableC8bit ? { portableC8bit: true } : {}),
        };
        const node = new AudioWorkletNode(ctx, PROCESSOR_NAME, {
          numberOfInputs: 0,
          numberOfOutputs: 1,
          outputChannelCount: [2],
          processorOptions,
        });
        node.port.onmessage = (e: MessageEvent<FromWorklet>) => {
          if (e.data.type === 'print') this.print?.(e.data.text, e.data.developer);
        };
        node.connect(ctx.destination);
        this.node = node;
        if (ctx.state === 'suspended') await ctx.resume();
        if (this.ctx !== ctx) return; // shut down meanwhile
        this.ready = true;
        for (const [id, sfx] of this.resident) this.post({ type: 'sfx', seq: ++this.outSeq, id, sfx });
      } catch (e) {
        this.o.onError?.(e);
      }
    })();
    return this.starting;
  }

  private post(m: ToWorklet): void {
    if (!this.node) return;
    if (m.type === 'batch') this.node.port.postMessage(m, [m.batch.buffer]);
    else this.node.port.postMessage(m);
  }

  shutdown(): void {
    this.removeGestures();
    this.ready = false;
    this.node?.disconnect();
    this.node = null;
    const ctx = this.ctx;
    this.ctx = null;
    this.starting = null;
    this.ring = null;
    this.resident.clear();
    void ctx?.close().catch(() => {});
  }

  sfx(_seq: number, id: number, sfx: CoreSfx | null): void {
    if (sfx) this.resident.set(id, sfx);
    else this.resident.delete(id);
    if (this.ready) this.post({ type: 'sfx', seq: ++this.outSeq, id, sfx });
  }

  batch(_seq: number, batch: Uint8Array): void {
    if (!this.ready) return;
    const seq = ++this.outSeq;
    if (this.ring && this.ring.push(seq, batch)) return;
    this.post({ type: 'batch', seq, batch });
  }
}
