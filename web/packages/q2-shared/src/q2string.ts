// Quake 2 strings are byte strings. Values >= 128 select the alternate (green/highlighted) charset of
// conchars; they must never be UTF-8 decoded. In TypeScript we represent them as "binary" JS strings in
// which every UTF-16 code unit is in 0..255 (Latin-1), which makes byte <-> char a 1:1 mapping.

const CHUNK = 0x2000;

/** Decode bytes [start, end) as a Latin-1 binary string (no NUL handling). */
export function latin1FromBytes(bytes: Uint8Array, start = 0, end = bytes.length): string {
  let s = '';
  for (let i = start; i < end; i += CHUNK) {
    const sub = bytes.subarray(i, Math.min(end, i + CHUNK));
    s += String.fromCharCode.apply(null, sub as unknown as number[]);
  }
  return s;
}

/** Encode a binary string to bytes (each char code is truncated to 8 bits, like a C char store). */
export function latin1ToBytes(s: string): Uint8Array {
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 255;
  return out;
}

/** Read a NUL-terminated C string starting at `offset`, reading at most `maxLen` bytes. */
export function readCString(bytes: Uint8Array, offset = 0, maxLen = bytes.length - offset): string {
  const lim = Math.min(bytes.length, offset + Math.max(0, maxLen));
  let end = offset;
  while (end < lim && bytes[end] !== 0) end++;
  return latin1FromBytes(bytes, offset, end);
}

/** Truncate a binary string at its first NUL (what C code sees). */
export function cstr(s: string): string {
  const i = s.indexOf('\0');
  return i < 0 ? s : s.slice(0, i);
}

/** Strip the high bit of every char (as the console does for plain display). */
export function stripHighBit(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) out += String.fromCharCode(s.charCodeAt(i) & 127);
  return out;
}

/** Set the high bit of every char (alternate charset / "green" text, e.g. `^` in menus / say_team). */
export function setHighBit(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) out += String.fromCharCode((s.charCodeAt(i) & 255) | 128);
  return out;
}

/** True if every char code is a byte value. */
export function isByteString(s: string): boolean {
  for (let i = 0; i < s.length; i++) if (s.charCodeAt(i) > 255) return false;
  return true;
}

/** Convert an arbitrary JS (UTF-16) string to a byte string by replacing non-Latin-1 chars with '?'. */
export function toByteString(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    out += c > 255 ? '?' : s[i];
  }
  return out;
}

/** Namespace-style access to the byte-string helpers. */
export const Q2String = {
  fromBytes: latin1FromBytes,
  toBytes: latin1ToBytes,
  readCString,
  cstr,
  stripHighBit,
  setHighBit,
  isByteString,
  toByteString,
} as const;
