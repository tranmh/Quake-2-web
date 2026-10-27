import { describe, expect, it } from 'vitest';
import * as M from '../src/msg';
import { SizeBuf } from '../src/sizebuf';

// Shared test doubles for q2-protocol tests: SizeBuf builders that stand in
// for the network boundary (a written packet turned into a readable one).

/** Builds a fresh write buffer, optionally capturing overflow prints. */
export function makeWriter(maxsize = 1400, allowoverflow = false): { sb: SizeBuf; prints: string[] } {
  const sb = new SizeBuf(maxsize);
  sb.allowoverflow = allowoverflow;
  const prints: string[] = [];
  sb.onPrint = (m) => prints.push(m);
  return { sb, prints };
}

/** Copies the written part of a buffer into a new one, as if received off the wire. */
export function toReader(sb: SizeBuf): SizeBuf {
  const r = new SizeBuf(sb.data.slice(0, sb.cursize));
  r.cursize = sb.cursize;
  return r;
}

/** Builds a buffer containing the given bytes. */
export function bufferOf(bytes: readonly number[]): SizeBuf {
  const r = new SizeBuf(Uint8Array.from(bytes));
  r.cursize = bytes.length;
  return r;
}

describe('q2-protocol test fixtures', () => {
  it('writer and reader round-trip the written bytes', () => {
    const { sb, prints } = makeWriter(8);
    M.MSG_WriteByte(sb, 0x7f);
    M.MSG_WriteShort(sb, 0x1234);
    const r = toReader(sb);
    expect(r.cursize).toBe(3);
    expect(Array.from(r.data.slice(0, 3))).toEqual([0x7f, 0x34, 0x12]);
    expect(prints).toEqual([]);
  });

  it('bufferOf exposes the bytes as a full buffer', () => {
    const r = bufferOf([1, 2, 3]);
    expect(r.cursize).toBe(3);
    expect(Array.from(r.data)).toEqual([1, 2, 3]);
  });
});
