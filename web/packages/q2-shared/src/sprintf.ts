// A glibc-compatible subset of printf formatting, used by ported code (Com_sprintf, va, Cvar_SetValue...).
// Supported: %d %i %u %x %X %o %c %s %f %F %e %E %g %G %p(as hex) %% with flags - 0 + space #,
// width/precision (incl. *), and ignored length modifiers (h hh l ll L q j z t).
// Floating conversions round the exact binary value half-to-even like glibc.

export type PrintfArg = number | string | bigint | boolean | null | undefined;

const F64 = new Float64Array(1);
const U64 = new BigUint64Array(F64.buffer);

/** |x| = mant * 2^exp exactly (x finite, non-zero). */
function decompose(x: number): { mant: bigint; exp: number } {
  F64[0] = Math.abs(x);
  const bits = U64[0]!;
  const e = Number((bits >> 52n) & 0x7ffn);
  let mant = bits & 0xfffffffffffffn;
  let exp: number;
  if (e === 0) {
    exp = -1074;
  } else {
    mant |= 0x10000000000000n;
    exp = e - 1075;
  }
  return { mant, exp };
}

/** round_half_even(|x| * 10^p) as a BigInt. */
function roundScaled(x: number, p: number): bigint {
  if (x === 0) return 0n;
  const { mant, exp } = decompose(x);
  let num = mant;
  let den = 1n;
  if (exp >= 0) num <<= BigInt(exp);
  else den <<= BigInt(-exp);
  if (p >= 0) num *= 10n ** BigInt(p);
  else den *= 10n ** BigInt(-p);
  const q = num / den;
  const r = num % den;
  const twice = r * 2n;
  if (twice > den || (twice === den && (q & 1n) === 1n)) return q + 1n;
  return q;
}

function fixedDigits(x: number, prec: number): string {
  const d = roundScaled(x, prec).toString();
  if (prec === 0) return d;
  const padded = d.padStart(prec + 1, '0');
  return padded.slice(0, padded.length - prec) + '.' + padded.slice(padded.length - prec);
}

/** Returns significant digits (length n) and decimal exponent E so that |x| ~= d.ddd * 10^E. */
function sciDigits(x: number, n: number): { digits: string; e: number } {
  if (x === 0) return { digits: '0'.repeat(n), e: 0 };
  let e = Math.floor(Math.log10(Math.abs(x)));
  const lo = 10n ** BigInt(n - 1);
  const hi = 10n ** BigInt(n);
  for (let iter = 0; iter < 4; iter++) {
    const d = roundScaled(x, n - 1 - e);
    if (d >= hi) e++;
    else if (d < lo) e--;
    else return { digits: d.toString(), e };
  }
  const d = roundScaled(x, n - 1 - e);
  return { digits: d.toString().slice(0, n), e };
}

function expString(e: number, upper: boolean): string {
  const sign = e < 0 ? '-' : '+';
  const a = Math.abs(e);
  return (upper ? 'E' : 'e') + sign + (a < 10 ? '0' + a : String(a));
}

function formatFloat(x: number, conv: string, prec: number, alt: boolean): string {
  const upper = conv === 'F' || conv === 'E' || conv === 'G';
  if (!Number.isFinite(x)) {
    const s = Number.isNaN(x) ? 'nan' : 'inf';
    return upper ? s.toUpperCase() : s;
  }
  if (prec < 0) prec = 6;
  const lc = conv.toLowerCase();
  if (lc === 'f') {
    let s = fixedDigits(x, prec);
    if (alt && prec === 0) s += '.';
    return s;
  }
  if (lc === 'e') {
    const { digits, e } = sciDigits(x, prec + 1);
    let s = digits[0]!;
    if (prec > 0 || alt) s += '.';
    s += digits.slice(1);
    return s + expString(e, upper);
  }
  // %g
  const P = prec === 0 ? 1 : prec;
  const { e } = sciDigits(x, P);
  let s: string;
  if (P > e && e >= -4) {
    s = fixedDigits(x, P - 1 - e);
    if (!alt && s.includes('.')) s = s.replace(/\.?0+$/, '');
  } else {
    const r = sciDigits(x, P);
    let m = r.digits[0]!;
    if (P > 1) m += '.' + r.digits.slice(1);
    if (!alt && m.includes('.')) m = m.replace(/\.?0+$/, '');
    s = m + expString(r.e, upper);
  }
  return s;
}

function toNumber(a: PrintfArg): number {
  if (typeof a === 'number') return a;
  if (typeof a === 'bigint') return Number(a);
  if (typeof a === 'boolean') return a ? 1 : 0;
  if (typeof a === 'string') return a.length ? a.charCodeAt(0) : 0;
  return 0;
}

/** C-style sprintf. Integer conversions treat arguments as 32-bit ints (like `int` varargs). */
export function sprintf(fmt: string, ...args: PrintfArg[]): string {
  let out = '';
  let ai = 0;
  const next = (): PrintfArg => args[ai++];
  let i = 0;
  const n = fmt.length;
  while (i < n) {
    const ch = fmt[i]!;
    if (ch !== '%') {
      out += ch;
      i++;
      continue;
    }
    i++;
    if (fmt[i] === '%') {
      out += '%';
      i++;
      continue;
    }
    let left = false,
      zero = false,
      plus = false,
      space = false,
      alt = false;
    for (;;) {
      const f = fmt[i];
      if (f === '-') left = true;
      else if (f === '0') zero = true;
      else if (f === '+') plus = true;
      else if (f === ' ') space = true;
      else if (f === '#') alt = true;
      else break;
      i++;
    }
    let width = 0;
    if (fmt[i] === '*') {
      width = toNumber(next()) | 0;
      if (width < 0) {
        left = true;
        width = -width;
      }
      i++;
    } else {
      while (i < n && fmt[i]! >= '0' && fmt[i]! <= '9') width = width * 10 + (fmt.charCodeAt(i++) - 48);
    }
    let prec = -1;
    if (fmt[i] === '.') {
      i++;
      if (fmt[i] === '*') {
        prec = toNumber(next()) | 0;
        if (prec < 0) prec = -1;
        i++;
      } else {
        prec = 0;
        while (i < n && fmt[i]! >= '0' && fmt[i]! <= '9') prec = prec * 10 + (fmt.charCodeAt(i++) - 48);
      }
    }
    while (i < n && 'hlLqjzt'.includes(fmt[i]!)) i++;
    const conv = fmt[i++];
    if (conv === undefined) break;

    let body = '';
    let sign = '';
    let numeric = false;
    switch (conv) {
      case 'd':
      case 'i': {
        numeric = true;
        let v = Math.trunc(toNumber(next())) | 0;
        if (v < 0) {
          sign = '-';
          v = -v;
        } else if (plus) sign = '+';
        else if (space) sign = ' ';
        body = (v >>> 0).toString();
        if (prec >= 0) body = prec === 0 && v === 0 ? '' : body.padStart(prec, '0');
        break;
      }
      case 'u':
      case 'x':
      case 'X':
      case 'o':
      case 'p': {
        numeric = true;
        const v = Math.trunc(toNumber(next())) >>> 0;
        const radix = conv === 'u' ? 10 : conv === 'o' ? 8 : 16;
        body = v.toString(radix);
        if (conv === 'X') body = body.toUpperCase();
        if (prec >= 0) body = prec === 0 && v === 0 ? '' : body.padStart(prec, '0');
        if ((alt || conv === 'p') && v !== 0) {
          if (conv === 'o' && body[0] !== '0') body = '0' + body;
          else if (conv === 'x' || conv === 'p') sign = '0x';
          else if (conv === 'X') sign = '0X';
        }
        break;
      }
      case 'c': {
        const a = next();
        body = String.fromCharCode(toNumber(a) & 255);
        break;
      }
      case 's': {
        const a = next();
        let s = a === null || a === undefined ? '(null)' : String(a);
        if (prec >= 0) s = s.slice(0, prec);
        body = s;
        break;
      }
      case 'f':
      case 'F':
      case 'e':
      case 'E':
      case 'g':
      case 'G': {
        numeric = true;
        const v = toNumber(next());
        if (v < 0 || Object.is(v, -0)) sign = '-';
        else if (plus) sign = '+';
        else if (space) sign = ' ';
        body = formatFloat(v, conv, prec, alt);
        if (!Number.isFinite(v)) zero = false;
        // integer-style precision does not disable 0 padding for floats
        prec = -1;
        break;
      }
      default:
        body = '%' + conv;
        break;
    }
    const len = sign.length + body.length;
    if (len >= width) out += sign + body;
    else if (left) out += sign + body + ' '.repeat(width - len);
    else if (zero && numeric && prec < 0) out += sign + '0'.repeat(width - len) + body;
    else out += ' '.repeat(width - len) + sign + body;
  }
  return out;
}

// C: q_shared.c:1205 Com_sprintf -- output truncated to size-1 chars.
export function Com_sprintf(size: number, fmt: string, ...args: PrintfArg[]): string {
  const s = sprintf(fmt, ...args);
  return s.length >= size ? s.slice(0, size - 1) : s;
}
