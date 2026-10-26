import { describe, expect, it } from 'vitest';
import { Com_sprintf, sprintf } from '../src';

describe('sprintf integer conversions', () => {
  it('sign flags and zero padding', () => {
    expect(sprintf('%+d', 5)).toBe('+5');
    expect(sprintf('% d', 5)).toBe(' 5');
    expect(sprintf('%+ d', 5)).toBe('+5');
    expect(sprintf('%+d', -5)).toBe('-5');
    expect(sprintf('%05d', -42)).toBe('-0042');
    expect(sprintf('%-05d|', -42)).toBe('-42  |');
    expect(sprintf('%2d', 12345)).toBe('12345');
  });

  it('precision pads with zeros and disables the 0 flag', () => {
    expect(sprintf('%.3d', 7)).toBe('007');
    expect(sprintf('%.3d', -7)).toBe('-007');
    expect(sprintf('%05.2d', 3)).toBe('   03');
    expect(sprintf('%.0d|', 0)).toBe('|');
    expect(sprintf('%3.0d|', 0)).toBe('   |');
    expect(sprintf('%.0x|', 0)).toBe('|');
  });

  it('treats arguments as 32-bit ints', () => {
    expect(sprintf('%d', -2147483648)).toBe('-2147483648');
    expect(sprintf('%d', 2147483647)).toBe('2147483647');
    expect(sprintf('%d', 2147483648)).toBe('-2147483648');
    expect(sprintf('%d', 2 ** 32 + 1)).toBe('1');
    expect(sprintf('%d', 3.9)).toBe('3');
    expect(sprintf('%d', -3.9)).toBe('-3');
    expect(sprintf('%u', -1)).toBe('4294967295');
    expect(sprintf('%x', -1)).toBe('ffffffff');
  });

  it('hex, octal and pointer forms', () => {
    expect(sprintf('%x', 255)).toBe('ff');
    expect(sprintf('%X', 255)).toBe('FF');
    expect(sprintf('%#x', 255)).toBe('0xff');
    expect(sprintf('%#X', 255)).toBe('0XFF');
    expect(sprintf('%#x', 0)).toBe('0');
    expect(sprintf('%08x', 255)).toBe('000000ff');
    expect(sprintf('%#010x', 255)).toBe('0x000000ff');
    expect(sprintf('%o', 8)).toBe('10');
    expect(sprintf('%#o', 8)).toBe('010');
    expect(sprintf('%#.3o', 8)).toBe('010');
    expect(sprintf('%p', 16)).toBe('0x10');
  });

  it('ignores length modifiers', () => {
    expect(sprintf('%ld %lld %hhd %hd %zu %jd %td %qd %Lf', 1, 2, 3, 4, 5, 6, 7, 8, 1.5)).toBe(
      '1 2 3 4 5 6 7 8 1.500000',
    );
  });

  it('coerces non-number arguments', () => {
    expect(sprintf('%d', 10n)).toBe('10');
    expect(sprintf('%d %d', true, false)).toBe('1 0');
    expect(sprintf('%d', 'A')).toBe('65');
    expect(sprintf('%d', '')).toBe('0');
    expect(sprintf('%d %d', null, undefined)).toBe('0 0');
    expect(sprintf('%d', Number.NaN)).toBe('0');
  });
});

describe('sprintf %c and %s', () => {
  it('%c masks to a byte', () => {
    expect(sprintf('%c', 0x141)).toBe('A');
    expect(sprintf('%3c|%-3c|', 66, 67)).toBe('  B|C  |');
  });

  it('%s width, precision and null', () => {
    expect(sprintf('%s', null)).toBe('(null)');
    expect(sprintf('%s', undefined)).toBe('(null)');
    expect(sprintf('%.2s', 'abcd')).toBe('ab');
    expect(sprintf('%.0s|', 'abcd')).toBe('|');
    expect(sprintf('%5s', 'ab')).toBe('   ab');
    expect(sprintf('%-5s|', 'ab')).toBe('ab   |');
    expect(sprintf('%.*s', 3, 'abcdef')).toBe('abc');
    expect(sprintf('%s', 42)).toBe('42');
  });
});

describe('sprintf floating conversions', () => {
  it('%f rounds the exact binary value half to even', () => {
    expect(sprintf('%f', 1.5)).toBe('1.500000');
    expect(sprintf('%.0f', 0.5)).toBe('0');
    expect(sprintf('%.0f', 1.5)).toBe('2');
    expect(sprintf('%.0f', 2.5)).toBe('2');
    expect(sprintf('%.2f', 0.125)).toBe('0.12');
    expect(sprintf('%.2f', 0.375)).toBe('0.38');
    // 1.0005 is stored slightly below the tie
    expect(sprintf('%.3f', 1.0005)).toBe('1.000');
    expect(sprintf('%.2f', 0.001)).toBe('0.00');
    expect(sprintf('%.1f', 0.05)).toBe('0.1');
  });

  it('%f flags', () => {
    expect(sprintf('%#.0f', 3)).toBe('3.');
    expect(sprintf('%+.1f', 2)).toBe('+2.0');
    expect(sprintf('% .1f', 2)).toBe(' 2.0');
    expect(sprintf('%08.2f', -1.5)).toBe('-0001.50');
    expect(sprintf('%-8.2f|', -1.5)).toBe('-1.50   |');
    expect(sprintf('%f', -0)).toBe('-0.000000');
    expect(sprintf('%.0f', -0.4)).toBe('-0');
  });

  it('non-finite values ignore zero padding', () => {
    expect(sprintf('%f', Infinity)).toBe('inf');
    expect(sprintf('%f', -Infinity)).toBe('-inf');
    expect(sprintf('%F', Number.NaN)).toBe('NAN');
    expect(sprintf('%E', Infinity)).toBe('INF');
    expect(sprintf('%05f', Infinity)).toBe('  inf');
    expect(sprintf('%-6f|', -Infinity)).toBe('-inf  |');
    expect(sprintf('%+g', Infinity)).toBe('+inf');
  });

  it('%e', () => {
    expect(sprintf('%e', 12345.678)).toBe('1.234568e+04');
    expect(sprintf('%E', 0)).toBe('0.000000E+00');
    expect(sprintf('%.0e', 5)).toBe('5e+00');
    expect(sprintf('%#.0e', 5)).toBe('5.e+00');
    expect(sprintf('%e', 1e-300)).toBe('1.000000e-300');
    expect(sprintf('%e', 9.9999999)).toBe('1.000000e+01');
    expect(sprintf('%.2e', -0.000123)).toBe('-1.23e-04');
  });

  it('%g picks fixed or exponent form and strips zeros', () => {
    expect(sprintf('%g', 0)).toBe('0');
    expect(sprintf('%g', 1.5)).toBe('1.5');
    expect(sprintf('%g', 0.0001)).toBe('0.0001');
    expect(sprintf('%g', 0.00001)).toBe('1e-05');
    expect(sprintf('%g', 100000)).toBe('100000');
    expect(sprintf('%g', 1000000)).toBe('1e+06');
    expect(sprintf('%G', 1e-10)).toBe('1E-10');
    expect(sprintf('%.0g', 123)).toBe('1e+02');
    expect(sprintf('%#g', 1.5)).toBe('1.50000');
    expect(sprintf('%#g', 1e6)).toBe('1.00000e+06');
    expect(sprintf('%.17g', 0.1)).toBe('0.10000000000000001');
    expect(sprintf('%g', Number.MIN_VALUE)).toBe('4.94066e-324');
    expect(sprintf('%g', 1.7976931348623157e308)).toBe('1.79769e+308');
  });
});

describe('sprintf format parsing', () => {
  it('negative * width left-justifies, negative * precision is ignored', () => {
    expect(sprintf('%*d|', -4, 7)).toBe('7   |');
    expect(sprintf('%.*f', -1, 1.5)).toBe('1.500000');
    expect(sprintf('%*.*f', 8, 2, 3.14159)).toBe('    3.14');
  });

  it('unknown conversions are echoed, a trailing % is dropped', () => {
    expect(sprintf('%y')).toBe('%y');
    expect(sprintf('abc%')).toBe('abc');
    expect(sprintf('100%%')).toBe('100%');
    expect(sprintf('')).toBe('');
    expect(sprintf('plain text')).toBe('plain text');
  });

  it('missing arguments format as zero / (null)', () => {
    expect(sprintf('%d %s %f')).toBe('0 (null) 0.000000');
  });

  it('extra arguments are ignored', () => {
    expect(sprintf('%d', 1, 2, 3)).toBe('1');
  });
});

describe('Com_sprintf', () => {
  it('keeps output shorter than size', () => {
    expect(Com_sprintf(4, '%s', 'abc')).toBe('abc');
    expect(Com_sprintf(3, '%s', 'abc')).toBe('ab');
    expect(Com_sprintf(1, '%s', 'abc')).toBe('');
    expect(Com_sprintf(1, '')).toBe('');
    expect(Com_sprintf(64, '%s: %d', 'frags', -3)).toBe('frags: -3');
  });
});
