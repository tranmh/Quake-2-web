// Helpers reproducing C numeric semantics (x86-64 SSE, -ffp-contract=off) in JavaScript.
// See docs/PORTING.md "Numeric rules".

/** Round a double to the nearest float32 (C store to a `float`). */
export const fr = Math.fround;

/**
 * C `(int)x` for a double/float operand as compiled for x86-64 (cvttsd2si): truncation toward zero;
 * NaN or out-of-range values produce INT_MIN (0x80000000).
 */
export function cInt(x: number): number {
  if (!(x > -2147483649 && x < 2147483648)) return -2147483648;
  return Math.trunc(x) | 0;
}

/** C `(short)` narrowing of an int (two's complement wrap). */
export function cShort(x: number): number {
  return (x << 16) >> 16;
}

/** C `(unsigned short)` narrowing. */
export function cUShort(x: number): number {
  return x & 0xffff;
}

/** C signed `char` narrowing. */
export function cChar(x: number): number {
  return (x << 24) >> 24;
}

/** C `(byte)` / unsigned char narrowing. */
export function cByte(x: number): number {
  return x & 255;
}

/** C unsigned 32-bit narrowing. */
export function cUInt(x: number): number {
  return x >>> 0;
}

const f32 = new Float32Array(1);
const u32 = new Uint32Array(f32.buffer);

/** Raw IEEE-754 bits of the float32 value nearest to x. */
export function floatBits(x: number): number {
  f32[0] = x;
  return u32[0]!;
}

/** Float32 value from raw bits. */
export function bitsToFloat(bits: number): number {
  u32[0] = bits >>> 0;
  return f32[0]!;
}

/** Bitwise equality of two values viewed as float32 (distinguishes -0 and +0, equal NaN payloads). */
export function floatBitsEqual(a: number, b: number): boolean {
  return floatBits(a) === floatBits(b);
}
