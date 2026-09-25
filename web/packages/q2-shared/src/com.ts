// Port of game/q_shared.c string/path/info helpers. Strings are Latin-1 byte strings (see q2string.ts);
// C strings end at the first NUL, which callers are expected to have stripped.
import { MAX_INFO_KEY, MAX_INFO_STRING, MAX_TOKEN_CHARS } from './generated/const';

/** Signed char value of a byte string char (C `char` is signed on x86). */
function schar(s: string, i: number): number {
  if (i >= s.length) return 0;
  const c = s.charCodeAt(i) & 255;
  return c >= 128 ? c - 256 : c;
}

/**
 * Parse cursor for COM_Parse: `data` is the whole string, `pos` the current offset or -1 for the C NULL
 * pointer (end of data reached).
 */
export interface ParseCursor {
  data: string;
  pos: number;
}

export function parseCursor(data: string): ParseCursor {
  return { data, pos: 0 };
}

// C: q_shared.c:1062 COM_Parse
// Quirks reproduced: bytes >= 128 are negative `char`s and therefore count as whitespace outside quotes;
// an unquoted token of exactly MAX_TOKEN_CHARS chars is discarded (empty token); quoted tokens are not.
export function COM_Parse(p: ParseCursor): string {
  const s = p.data;
  let data = p.pos;
  let token = '';
  let len = 0;
  if (data < 0) {
    p.pos = -1;
    return '';
  }
  let c: number;
  // skip whitespace
  for (;;) {
    while ((c = schar(s, data)) <= 32) {
      if (c === 0) {
        p.pos = -1;
        return '';
      }
      data++;
    }
    // skip // comments
    if (c === 47 && schar(s, data + 1) === 47) {
      while (schar(s, data) && schar(s, data) !== 10) data++;
      continue;
    }
    break;
  }

  // handle quoted strings specially
  if (c === 34) {
    data++;
    for (;;) {
      c = schar(s, data++);
      if (c === 34 || !c) {
        p.pos = data;
        return token;
      }
      if (len < MAX_TOKEN_CHARS) {
        token += String.fromCharCode(c & 255);
        len++;
      }
    }
  }

  // parse a regular word
  do {
    if (len < MAX_TOKEN_CHARS) {
      token += String.fromCharCode(c & 255);
      len++;
    }
    data++;
    c = schar(s, data);
  } while (c > 32);

  if (len === MAX_TOKEN_CHARS) token = '';
  p.pos = data;
  return token;
}

/** Convenience: split a string into all COM_Parse tokens. */
export function COM_ParseAll(data: string): string[] {
  const p = parseCursor(data);
  const out: string[] = [];
  for (;;) {
    const t = COM_Parse(p);
    if (p.pos < 0) break;
    out.push(t);
  }
  return out;
}

// C: q_shared.c:845 COM_SkipPath
export function COM_SkipPath(pathname: string): string {
  let last = 0;
  for (let i = 0; i < pathname.length; i++) if (pathname.charCodeAt(i) === 47) last = i + 1;
  return pathname.slice(last);
}

// C: q_shared.c:864 COM_StripExtension (stops at the FIRST '.')
export function COM_StripExtension(inp: string): string {
  const i = inp.indexOf('.');
  return i < 0 ? inp : inp.slice(0, i);
}

// C: q_shared.c:876 COM_FileExtension (after the FIRST '.', at most 7 chars)
export function COM_FileExtension(inp: string): string {
  const i = inp.indexOf('.');
  if (i < 0) return '';
  return inp.slice(i + 1, i + 8);
}

// C: q_shared.c:896 COM_FileBase
// Quirk: when there is no '/', the first character is dropped ("demo1.bsp" -> "emo1").
export function COM_FileBase(inp: string): string {
  if (inp.length === 0) return '';
  let s = inp.length - 1;
  while (s !== 0 && inp.charCodeAt(s) !== 46) s--;
  let s2 = s;
  while (s2 !== 0 && inp.charCodeAt(s2) !== 47) s2--;
  if (s - s2 < 2) return '';
  s--;
  return inp.slice(s2 + 1, s2 + 1 + (s - s2));
}

// C: q_shared.c:925 COM_FilePath
export function COM_FilePath(inp: string): string {
  if (inp.length === 0) return '';
  let s = inp.length - 1;
  while (s !== 0 && inp.charCodeAt(s) !== 47) s--;
  return inp.slice(0, s);
}

// C: q_shared.c:945 COM_DefaultExtension (returns the new path)
export function COM_DefaultExtension(path: string, extension: string): string {
  let src = path.length - 1;
  while (src > 0 && path.charCodeAt(src) !== 47) {
    if (path.charCodeAt(src) === 46) return path;
    src--;
  }
  return path + extension;
}

// C: q_shared.c:1170 Q_strncasecmp
export function Q_strncasecmp(s1: string, s2: string, n: number): number {
  let i = 0;
  let c1: number, c2: number;
  do {
    c1 = schar(s1, i);
    c2 = schar(s2, i);
    i++;
    if (!n--) return 0;
    if (c1 !== c2) {
      if (c1 >= 97 && c1 <= 122) c1 -= 32;
      if (c2 >= 97 && c2 <= 122) c2 -= 32;
      if (c1 !== c2) return -1;
    }
  } while (c1);
  return 0;
}

// C: q_shared.c:1195 Q_strcasecmp
export function Q_strcasecmp(s1: string, s2: string): number {
  return Q_strncasecmp(s1, s2, 99999);
}

// C: q_shared.c:1160 Q_stricmp -> glibc strcasecmp (C locale): difference of ASCII-lowercased bytes.
export function Q_stricmp(s1: string, s2: string): number {
  for (let i = 0; ; i++) {
    let c1 = i < s1.length ? s1.charCodeAt(i) & 255 : 0;
    let c2 = i < s2.length ? s2.charCodeAt(i) & 255 : 0;
    if (c1 >= 65 && c1 <= 90) c1 += 32;
    if (c2 >= 65 && c2 <= 90) c2 += 32;
    if (c1 !== c2 || c1 === 0) return c1 - c2;
  }
}

// C: q_shared.c:1234 Info_ValueForKey
export function Info_ValueForKey(s: string, key: string): string {
  let i = 0;
  const at = (k: number) => (k < s.length ? s.charCodeAt(k) : 0);
  if (at(i) === 92) i++;
  for (;;) {
    let pkey = '';
    while (at(i) !== 92) {
      if (!at(i)) return '';
      pkey += s[i++];
    }
    i++;
    let value = '';
    while (at(i) !== 92 && at(i)) value += s[i++];
    if (key === pkey) return value;
    if (!at(i)) return '';
    i++;
  }
}

// C: q_shared.c:1277 Info_RemoveKey (returns the new string)
export function Info_RemoveKey(s: string, key: string): string {
  if (key.includes('\\')) return s;
  let i = 0;
  const at = (k: number) => (k < s.length ? s.charCodeAt(k) : 0);
  for (;;) {
    const start = i;
    if (at(i) === 92) i++;
    let pkey = '';
    while (at(i) !== 92) {
      if (!at(i)) return s;
      pkey += s[i++];
    }
    i++;
    while (at(i) !== 92 && at(i)) i++;
    if (key === pkey) return s.slice(0, start) + s.slice(i);
    if (!at(i)) return s;
  }
}

// C: q_shared.c:1329 Info_Validate
export function Info_Validate(s: string): boolean {
  if (s.includes('"')) return false;
  if (s.includes(';')) return false;
  return true;
}

// C: q_shared.c:1338 Info_SetValueForKey (returns the new string; messages go to `print`)
export function Info_SetValueForKey(
  s: string,
  key: string,
  value: string,
  print: (msg: string) => void = () => {},
): string {
  if (key.includes('\\') || value.includes('\\')) {
    print("Can't use keys or values with a \\\n");
    return s;
  }
  if (key.includes(';')) {
    print("Can't use keys or values with a semicolon\n");
    return s;
  }
  if (key.includes('"') || value.includes('"')) {
    print('Can\'t use keys or values with a "\n');
    return s;
  }
  if (key.length > MAX_INFO_KEY - 1 || value.length > MAX_INFO_KEY - 1) {
    print('Keys and values must be < 64 characters.\n');
    return s;
  }
  s = Info_RemoveKey(s, key);
  if (!value.length) return s;
  const newi = '\\' + key + '\\' + value;
  if (newi.length + s.length > MAX_INFO_STRING) {
    print('Info string length exceeded\n');
    return s;
  }
  // only copy ascii values
  let out = s;
  for (let i = 0; i < newi.length; i++) {
    const c = newi.charCodeAt(i) & 127;
    if (c >= 32 && c < 127) out += String.fromCharCode(c);
  }
  return out;
}

function isCSpace(c: number): boolean {
  return c === 32 || (c >= 9 && c <= 13);
}

/** glibc atoi: (int)strtol(s, NULL, 10). */
export function atoi(s: string): number {
  let i = 0;
  while (i < s.length && isCSpace(s.charCodeAt(i))) i++;
  let neg = false;
  if (s[i] === '+' || s[i] === '-') {
    neg = s[i] === '-';
    i++;
  }
  let v = 0n;
  const LMAX = (1n << 63n) - 1n;
  let overflow = false;
  while (i < s.length) {
    const c = s.charCodeAt(i);
    if (c < 48 || c > 57) break;
    v = v * 10n + BigInt(c - 48);
    if (v > LMAX + 1n) overflow = true;
    i++;
  }
  let r: bigint;
  if (overflow || (!neg && v > LMAX) || (neg && v > LMAX + 1n)) r = neg ? -(LMAX + 1n) : LMAX;
  else r = neg ? -v : v;
  return Number(BigInt.asIntN(32, r));
}

const FLOAT_RE = /^[+-]?(?:\d+\.?\d*(?:[eE][+-]?\d+)?|\.\d+(?:[eE][+-]?\d+)?)/;
const HEXFLOAT_RE = /^([+-]?)0[xX]([0-9a-fA-F]*)(?:\.([0-9a-fA-F]*))?(?:[pP]([+-]?\d+))?/;

/** glibc atof: strtod(s, NULL) (decimal, hex, inf, nan). Returns a double. */
export function atof(s: string): number {
  let i = 0;
  while (i < s.length && isCSpace(s.charCodeAt(i))) i++;
  const t = s.slice(i);
  const hx = HEXFLOAT_RE.exec(t);
  if (hx && (hx[2] || hx[3])) {
    const intp = hx[2] ?? '';
    const fracp = hx[3] ?? '';
    let v = 0;
    for (const ch of intp) v = v * 16 + parseInt(ch, 16);
    let scale = 1 / 16;
    for (const ch of fracp) {
      v += parseInt(ch, 16) * scale;
      scale /= 16;
    }
    const e = hx[4] ? parseInt(hx[4], 10) : 0;
    v *= Math.pow(2, e);
    return hx[1] === '-' ? -v : v;
  }
  const m = FLOAT_RE.exec(t);
  if (m) return Number(m[0]);
  const low = t.toLowerCase();
  const sign = low[0] === '-' ? -1 : 1;
  const body = low[0] === '-' || low[0] === '+' ? low.slice(1) : low;
  if (body.startsWith('inf')) return sign * Infinity;
  if (body.startsWith('nan')) return NaN;
  return 0;
}
