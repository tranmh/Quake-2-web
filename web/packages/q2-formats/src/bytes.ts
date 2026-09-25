// Bounds-checked little-endian access helpers shared by the format parsers.
import { FormatError } from './errors';

export function toU8(data: ArrayBuffer | Uint8Array): Uint8Array {
  return data instanceof Uint8Array ? data : new Uint8Array(data);
}

export class LEView {
  readonly dv: DataView;
  constructor(
    readonly bytes: Uint8Array,
    readonly what: string,
  ) {
    this.dv = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  }
  check(ofs: number, len: number): void {
    if (ofs < 0 || len < 0 || ofs + len > this.bytes.length || !Number.isInteger(ofs)) {
      throw new FormatError(
        `${this.what}: read of ${len} bytes at ${ofs} out of bounds (${this.bytes.length})`,
      );
    }
  }
  i32(ofs: number): number {
    this.check(ofs, 4);
    return this.dv.getInt32(ofs, true);
  }
  u32(ofs: number): number {
    this.check(ofs, 4);
    return this.dv.getUint32(ofs, true);
  }
  i16(ofs: number): number {
    this.check(ofs, 2);
    return this.dv.getInt16(ofs, true);
  }
  u16(ofs: number): number {
    this.check(ofs, 2);
    return this.dv.getUint16(ofs, true);
  }
  u8(ofs: number): number {
    this.check(ofs, 1);
    return this.bytes[ofs]!;
  }
  i8(ofs: number): number {
    this.check(ofs, 1);
    return (this.bytes[ofs]! << 24) >> 24;
  }
  f32(ofs: number): number {
    this.check(ofs, 4);
    return this.dv.getFloat32(ofs, true);
  }
}
