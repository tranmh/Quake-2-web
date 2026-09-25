// Single-producer / single-consumer byte ring on a SharedArrayBuffer, used to hand command batches to
// the AudioWorklet without going through its event loop when the page is crossOriginIsolated.
// Record layout: u32 length, u32 sfx sequence, payload (length bytes), padded to 4 bytes.

const HEADER_BYTES = 16; // Int32 [write, read, unused, unused]

export class SabRing {
  readonly ctrl: Int32Array;
  readonly data: Uint8Array;
  readonly size: number;

  constructor(readonly sab: SharedArrayBuffer) {
    this.ctrl = new Int32Array(sab, 0, 4);
    this.data = new Uint8Array(sab, HEADER_BYTES);
    this.size = this.data.length;
    if (this.size & 3) throw new Error('SabRing: size must be a multiple of 4');
  }

  static create(bytes = 1 << 20): SabRing {
    return new SabRing(new SharedArrayBuffer(HEADER_BYTES + bytes));
  }

  private used(w: number, r: number): number {
    return (w - r + this.size) % this.size;
  }

  private copyIn(pos: number, src: Uint8Array): void {
    const first = Math.min(src.length, this.size - pos);
    this.data.set(src.subarray(0, first), pos);
    if (first < src.length) this.data.set(src.subarray(first), 0);
  }

  private copyOut(pos: number, n: number): Uint8Array {
    const out = new Uint8Array(n);
    const first = Math.min(n, this.size - pos);
    out.set(this.data.subarray(pos, pos + first));
    if (first < n) out.set(this.data.subarray(0, n - first), first);
    return out;
  }

  private u32At(pos: number): number {
    const b = this.copyOut(pos, 4);
    return (b[0]! | (b[1]! << 8) | (b[2]! << 16) | (b[3]! << 24)) >>> 0;
  }

  /** Producer: returns false (record dropped) when the ring is full. */
  push(seq: number, payload: Uint8Array): boolean {
    const w = Atomics.load(this.ctrl, 0);
    const r = Atomics.load(this.ctrl, 1);
    const padded = (payload.length + 3) & ~3;
    const need = 8 + padded;
    if (this.used(w, r) + need >= this.size) return false;
    const hdr = new Uint8Array(8);
    const dv = new DataView(hdr.buffer);
    dv.setUint32(0, payload.length, true);
    dv.setUint32(4, seq >>> 0, true);
    this.copyIn(w, hdr);
    this.copyIn((w + 8) % this.size, payload);
    Atomics.store(this.ctrl, 0, (w + need) % this.size);
    return true;
  }

  /** Consumer: the sequence number of the next record, or -1 if empty. */
  peekSeq(): number {
    const w = Atomics.load(this.ctrl, 0);
    const r = Atomics.load(this.ctrl, 1);
    if (w === r) return -1;
    return this.u32At((r + 4) % this.size);
  }

  /** Consumer: removes and returns the next record's payload, or null if empty. */
  pop(): Uint8Array | null {
    const w = Atomics.load(this.ctrl, 0);
    const r = Atomics.load(this.ctrl, 1);
    if (w === r) return null;
    const len = this.u32At(r);
    const payload = this.copyOut((r + 8) % this.size, len);
    Atomics.store(this.ctrl, 1, (r + 8 + ((len + 3) & ~3)) % this.size);
    return payload;
  }
}
