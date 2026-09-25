// Node-only helpers for tests (paths to fixtures / demo data, JSONL, hex, float32 comparison).
// Not exported from the package root so browser bundles never pull in node: modules.
import { existsSync, readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { floatBits } from '../cmath';

let cachedRoot: string | undefined;

/** Repository root: the first ancestor directory containing both `web/` and `docs/`. */
export function repoRoot(): string {
  if (cachedRoot) return cachedRoot;
  let dir = dirname(fileURLToPath(import.meta.url));
  for (;;) {
    if (existsSync(join(dir, 'web')) && existsSync(join(dir, 'docs'))) return (cachedRoot = dir);
    const up = dirname(dir);
    if (up === dir) throw new Error('repo root not found');
    dir = up;
  }
}

/** `$Q2_FIXTURES` or `<repo>/fixtures/generated`. */
export function fixturesDir(): string {
  const env = process.env['Q2_FIXTURES'];
  return env ? resolve(env) : join(repoRoot(), 'fixtures', 'generated');
}

/** `$Q2_BASEDIR` or `<repo>/assets/demo`. */
export function baseDir(): string {
  const env = process.env['Q2_BASEDIR'];
  return env ? resolve(env) : join(repoRoot(), 'assets', 'demo');
}

/** Path of the demo `baseq2/pak0.pak`. */
export function demoPakPath(): string {
  return join(baseDir(), 'baseq2', 'pak0.pak');
}

export function fixturePath(...parts: string[]): string {
  return join(fixturesDir(), ...parts);
}

export function fileExists(p: string): boolean {
  return existsSync(p);
}

/** Read a file into a standalone ArrayBuffer, or undefined when missing. */
export function readArrayBuffer(p: string): ArrayBuffer | undefined {
  if (!existsSync(p)) return undefined;
  const b = readFileSync(p);
  return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength) as ArrayBuffer;
}

export function readJson<T = unknown>(p: string): T {
  return JSON.parse(readFileSync(p, 'utf8')) as T;
}

export function readJsonl<T = unknown>(p: string): T[] {
  const out: T[] = [];
  for (const line of readFileSync(p, 'utf8').split('\n')) {
    if (line.trim()) out.push(JSON.parse(line) as T);
  }
  return out;
}

export function hexToBytes(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length >> 1);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.substr(i * 2, 2), 16);
  return out;
}

export function bytesToHex(b: Uint8Array): string {
  let s = '';
  for (let i = 0; i < b.length; i++) s += b[i]!.toString(16).padStart(2, '0');
  return s;
}

/** Float32 bit pattern as hex, for mismatch messages. */
export function f32hex(x: number): string {
  return '0x' + floatBits(x).toString(16).padStart(8, '0');
}

/** Describe a float for error messages: value and bits. */
export function f32desc(x: number): string {
  return `${Math.fround(x)} (${f32hex(x)})`;
}

/**
 * Compare `actual` against `expected` (decoded from fixture JSON) bitwise as float32.
 * Returns an error string or null.
 */
export function cmpF32(path: string, actual: number, expected: number): string | null {
  if (floatBits(actual) === floatBits(Math.fround(expected))) return null;
  return `${path}: got ${f32desc(actual)} want ${f32desc(expected)}`;
}

export function cmpF32Vec(
  path: string,
  actual: ArrayLike<number>,
  expected: ArrayLike<number>,
): string | null {
  for (let i = 0; i < expected.length; i++) {
    const e = cmpF32(`${path}[${i}]`, actual[i]!, expected[i]!);
    if (e) return e;
  }
  return null;
}

export function cmpInt(path: string, actual: number, expected: number): string | null {
  return actual === expected ? null : `${path}: got ${actual} want ${expected}`;
}

export function cmpIntVec(
  path: string,
  actual: ArrayLike<number>,
  expected: ArrayLike<number>,
): string | null {
  for (let i = 0; i < expected.length; i++) {
    if (actual[i] !== expected[i]) return `${path}[${i}]: got ${actual[i]} want ${expected[i]}`;
  }
  return null;
}
