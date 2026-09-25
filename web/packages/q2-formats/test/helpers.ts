/** Little-endian byte builder for synthetic test files. */
export class W {
  private a: number[] = [];
  get length(): number {
    return this.a.length;
  }
  u8(...v: number[]): this {
    for (const x of v) this.a.push(x & 255);
    return this;
  }
  i16(...v: number[]): this {
    for (const x of v) this.u8(x, x >> 8);
    return this;
  }
  i32(...v: number[]): this {
    for (const x of v) this.u8(x, x >> 8, x >> 16, x >> 24);
    return this;
  }
  f32(...v: number[]): this {
    const b = new DataView(new ArrayBuffer(4));
    for (const x of v) {
      b.setFloat32(0, x, true);
      this.u8(b.getUint8(0), b.getUint8(1), b.getUint8(2), b.getUint8(3));
    }
    return this;
  }
  str(s: string, size: number): this {
    for (let i = 0; i < size; i++) this.u8(i < s.length ? s.charCodeAt(i) : 0);
    return this;
  }
  bytes(b: ArrayLike<number>): this {
    for (let i = 0; i < b.length; i++) this.u8(b[i]!);
    return this;
  }
  pad(n: number, v = 0): this {
    for (let i = 0; i < n; i++) this.u8(v);
    return this;
  }
  /** overwrite a little-endian int32 at offset */
  set32(ofs: number, v: number): this {
    for (let i = 0; i < 4; i++) this.a[ofs + i] = (v >> (8 * i)) & 255;
    return this;
  }
  build(): Uint8Array {
    return Uint8Array.from(this.a);
  }
}
