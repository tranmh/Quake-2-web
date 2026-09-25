// Binary command batches from the main thread (snd_dma.c entry points) to the SoundCore host.
// All S_* calls made between two S_Update calls are appended to one batch; S_Update closes the batch.
// The host applies a whole batch atomically, so no mixing happens between the calls of one client
// frame, exactly like the single threaded original.
import type { FrameInfo, LoopEnt, SoundCore } from './snd_core';

export const CMD_START = 1;
export const CMD_STOPALL = 2;
export const CMD_RAW = 3;
export const CMD_UPDATE = 5;
export const CMD_ORIGINS = 6;

/** Growable little-endian byte writer. */
export class BatchWriter {
  private buf = new ArrayBuffer(4096);
  private dv = new DataView(this.buf);
  private u8 = new Uint8Array(this.buf);
  length = 0;

  private need(n: number): void {
    if (this.length + n <= this.buf.byteLength) return;
    let size = this.buf.byteLength * 2;
    while (size < this.length + n) size *= 2;
    const nb = new ArrayBuffer(size);
    new Uint8Array(nb).set(this.u8.subarray(0, this.length));
    this.buf = nb;
    this.dv = new DataView(nb);
    this.u8 = new Uint8Array(nb);
  }
  u8w(v: number): void {
    this.need(1);
    this.dv.setUint8(this.length, v);
    this.length += 1;
  }
  i32(v: number): void {
    this.need(4);
    this.dv.setInt32(this.length, v, true);
    this.length += 4;
  }
  f32(v: number): void {
    this.need(4);
    this.dv.setFloat32(this.length, v, true);
    this.length += 4;
  }
  bytes(b: Uint8Array): void {
    this.need(b.length);
    this.u8.set(b, this.length);
    this.length += b.length;
  }
  /** Returns a copy of the written bytes and resets the writer. */
  take(): Uint8Array {
    const out = this.u8.slice(0, this.length);
    this.length = 0;
    return out;
  }
}

class BatchReader {
  private readonly dv: DataView;
  pos = 0;
  constructor(readonly b: Uint8Array) {
    this.dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
  }
  get done(): boolean {
    return this.pos >= this.b.length;
  }
  u8(): number {
    return this.dv.getUint8(this.pos++);
  }
  i32(): number {
    const v = this.dv.getInt32(this.pos, true);
    this.pos += 4;
    return v;
  }
  f32(): number {
    const v = this.dv.getFloat32(this.pos, true);
    this.pos += 4;
    return v;
  }
  bytes(n: number): Uint8Array {
    const r = this.b.subarray(this.pos, this.pos + n);
    this.pos += n;
    return r;
  }
}

export function writeStart(
  w: BatchWriter,
  origin: ArrayLike<number> | null,
  entnum: number,
  entchannel: number,
  sfx: number,
  fvol: number,
  attenuation: number,
  timeofs: number,
  servertime: number,
): void {
  w.u8w(CMD_START);
  w.i32(sfx);
  w.u8w(origin ? 1 : 0);
  w.f32(origin ? origin[0]! : 0);
  w.f32(origin ? origin[1]! : 0);
  w.f32(origin ? origin[2]! : 0);
  w.i32(entnum);
  w.i32(entchannel);
  w.f32(fvol);
  w.f32(attenuation);
  w.f32(timeofs);
  w.i32(servertime);
}

export function writeStopAll(w: BatchWriter): void {
  w.u8w(CMD_STOPALL);
}

export function writeRaw(
  w: BatchWriter,
  samples: number,
  rate: number,
  width: number,
  channels: number,
  data: Uint8Array,
): void {
  w.u8w(CMD_RAW);
  w.i32(samples);
  w.i32(rate);
  w.i32(width);
  w.i32(channels);
  w.i32(data.length);
  w.bytes(data);
}

/** Entity origins (CL_GetEntitySoundOrigin results) for entities with dynamic sounds. */
export function writeOrigins(w: BatchWriter, ents: ArrayLike<number>, origins: ArrayLike<number>): void {
  w.u8w(CMD_ORIGINS);
  w.i32(ents.length);
  for (let i = 0; i < ents.length; i++) {
    w.i32(ents[i]!);
    w.f32(origins[i * 3]!);
    w.f32(origins[i * 3 + 1]!);
    w.f32(origins[i * 3 + 2]!);
  }
}

export function writeUpdate(w: BatchWriter, f: FrameInfo): void {
  w.u8w(CMD_UPDATE);
  for (const v of [f.listenerOrigin, f.listenerForward, f.listenerRight, f.listenerUp]) {
    w.f32(v[0]!);
    w.f32(v[1]!);
    w.f32(v[2]!);
  }
  w.i32(f.playernum);
  w.u8w(
    (f.active ? 1 : 0) |
      (f.paused ? 2 : 0) |
      (f.soundPrepped ? 4 : 0) |
      (f.disableScreen ? 8 : 0) |
      (f.volumeModified ? 16 : 0),
  );
  w.f32(f.volume);
  w.f32(f.mixahead);
  w.f32(f.testsound);
  w.f32(f.show);
  w.i32(f.loops.length);
  for (const l of f.loops) {
    w.i32(l.sound);
    w.i32(l.sfx);
    w.f32(l.origin[0]!);
    w.f32(l.origin[1]!);
    w.f32(l.origin[2]!);
  }
}

/** Applies one batch to a core. Sticky state (volume modified) is handled by the caller-provided core. */
export function applyBatch(core: SoundCore, batch: Uint8Array): void {
  const r = new BatchReader(batch);
  const o = new Float32Array(3);
  while (!r.done) {
    const cmd = r.u8();
    switch (cmd) {
      case CMD_START: {
        const sfx = r.i32();
        const fixed = r.u8();
        o[0] = r.f32();
        o[1] = r.f32();
        o[2] = r.f32();
        const entnum = r.i32();
        const entchannel = r.i32();
        const fvol = r.f32();
        const attenuation = r.f32();
        const timeofs = r.f32();
        const servertime = r.i32();
        core.S_StartSound(fixed ? o : null, entnum, entchannel, sfx, fvol, attenuation, timeofs, servertime);
        break;
      }
      case CMD_STOPALL:
        core.S_StopAllSounds();
        break;
      case CMD_RAW: {
        const samples = r.i32();
        const rate = r.i32();
        const width = r.i32();
        const channels = r.i32();
        const n = r.i32();
        core.S_RawSamples(samples, rate, width, channels, r.bytes(n));
        break;
      }
      case CMD_ORIGINS: {
        const n = r.i32();
        for (let i = 0; i < n; i++) {
          const ent = r.i32();
          let v = core.entityOrigins.get(ent);
          if (!v) core.entityOrigins.set(ent, (v = new Float32Array(3)));
          v[0] = r.f32();
          v[1] = r.f32();
          v[2] = r.f32();
        }
        break;
      }
      case CMD_UPDATE: {
        const vecs: Float32Array[] = [];
        for (let k = 0; k < 4; k++) vecs.push(Float32Array.of(r.f32(), r.f32(), r.f32()));
        const playernum = r.i32();
        const flags = r.u8();
        const volume = r.f32();
        const mixahead = r.f32();
        const testsound = r.f32();
        const show = r.f32();
        const n = r.i32();
        const loops: LoopEnt[] = [];
        for (let i = 0; i < n; i++) {
          const sound = r.i32();
          const sfx = r.i32();
          loops.push({ sound, sfx, origin: Float32Array.of(r.f32(), r.f32(), r.f32()) });
        }
        core.S_Update({
          listenerOrigin: vecs[0]!,
          listenerForward: vecs[1]!,
          listenerRight: vecs[2]!,
          listenerUp: vecs[3]!,
          playernum,
          active: !!(flags & 1),
          paused: !!(flags & 2),
          soundPrepped: !!(flags & 4),
          disableScreen: !!(flags & 8),
          volumeModified: !!(flags & 16),
          volume,
          mixahead,
          testsound,
          show,
          loops,
        });
        break;
      }
      default:
        throw new Error(`q2-sound: bad batch command ${cmd}`);
    }
  }
}
