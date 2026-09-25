// AudioWorkletProcessor hosting the whole snd_dma.c/snd_mix.c mixer core (SoundCore). The main thread
// forwards the S_* calls as ordered messages (host.ts); S_Update paints into the core's DMA ring buffer
// and process() plays that buffer, advancing the DMA position (SNDDMA_GetDMAPos) by the frames played,
// like a sound card reading the DirectSound secondary buffer.
//
// Load with audioWorklet.addModule(<url of this module, bundled>) -- see web.ts.
import { CoreHost } from './host';
import { SabRing } from './ring';
import { PROCESSOR_NAME, type FromWorklet, type ProcessorOptions, type ToWorklet } from './worklet_types';

interface WorkletScope {
  sampleRate: number;
  registerProcessor(name: string, ctor: unknown): void;
  AudioWorkletProcessor: new () => { readonly port: MessagePort };
}

const scope = globalThis as unknown as Partial<WorkletScope>;

if (typeof scope.registerProcessor === 'function' && scope.AudioWorkletProcessor) {
  const Base = scope.AudioWorkletProcessor;
  class Q2SoundProcessor extends Base {
    private readonly host: CoreHost;
    private readonly ring: SabRing | null;
    /** DMA frames played (integer part of pos) */
    private frames = 0;
    /** fractional playback position when the context could not be created at dma.speed */
    private pos = 0;
    private readonly step: number;

    constructor(options: { processorOptions: ProcessorOptions }) {
      super();
      const o = options.processorOptions;
      this.ring = o.ring ? new SabRing(o.ring) : null;
      this.step = o.speed / (scope.sampleRate ?? o.speed);
      const post = (m: FromWorklet): void => this.port.postMessage(m);
      this.host = new CoreHost({
        speed: o.speed,
        portableC8bit: o.portableC8bit,
        print: (text) => post({ type: 'print', text, developer: false }),
        dprint: (text) => post({ type: 'print', text, developer: true }),
        getDMAPos: () => {
          const dma = this.host.core.dma;
          return (this.frames * dma.channels) & (dma.samples - 1);
        },
      });
      this.port.onmessage = (e: MessageEvent<ToWorklet>) => {
        const m = e.data;
        if (m.type === 'sfx') this.host.onSfx(m.seq, m.id, m.sfx);
        else this.host.onBatch(m.seq, m.batch);
      };
    }

    process(_inputs: Float32Array[][], outputs: Float32Array[][]): boolean {
      if (this.ring) this.host.drainRing(this.ring);
      const out = outputs[0];
      if (!out || !out[0]) return true;
      const L = out[0];
      const R = out[1] ?? null;
      const dma = this.host.core.dma;
      const buf = dma.buffer;
      const mask = (dma.samples >> 1) - 1;
      const n = L.length;
      if (this.step === 1) {
        for (let i = 0; i < n; i++) {
          const idx = ((this.frames + i) & mask) << 1;
          L[i] = buf[idx]! / 32768;
          if (R) R[i] = buf[idx + 1]! / 32768;
        }
        this.frames += n;
      } else {
        // TODO-IMPROVE: linear interpolation when the browser refused the requested sample rate
        for (let i = 0; i < n; i++) {
          const f = Math.floor(this.pos);
          const t = this.pos - f;
          const a = (f & mask) << 1;
          const b = ((f + 1) & mask) << 1;
          L[i] = (buf[a]! * (1 - t) + buf[b]! * t) / 32768;
          if (R) R[i] = (buf[a + 1]! * (1 - t) + buf[b + 1]! * t) / 32768;
          this.pos += this.step;
        }
        this.frames = Math.floor(this.pos);
      }
      return true;
    }
  }
  scope.registerProcessor(PROCESSOR_NAME, Q2SoundProcessor);
}
