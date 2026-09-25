import { describe, expect, it } from 'vitest';
import { latin1ToBytes } from 'q2-shared';
import { bytesToHex } from 'q2-shared/testing';
import { COM_BlockSequenceCRCByte, CRC_Block, CRC_Init, CRC_ProcessByte, CRC_Value } from '../src/crc';
import { Com_BlockChecksum, md4 } from '../src/md4';

describe('crc', () => {
  it('CRC_Block matches CCITT-FALSE check value', () => {
    expect(CRC_Block(latin1ToBytes('123456789'))).toBe(0x29b1);
    expect(CRC_Block(new Uint8Array(0))).toBe(0xffff);
  });
  it('incremental API agrees with CRC_Block', () => {
    const b = latin1ToBytes('hello quake');
    let c = CRC_Init();
    for (const x of b) c = CRC_ProcessByte(c, x);
    expect(CRC_Value(c)).toBe(CRC_Block(b));
  });
  it('COM_BlockSequenceCRCByte is a byte and deterministic; truncates at 60 bytes', () => {
    const b = new Uint8Array(100).map((_, i) => i * 7);
    const v = COM_BlockSequenceCRCByte(b, 100, 12345);
    expect(v).toBeGreaterThanOrEqual(0);
    expect(v).toBeLessThan(256);
    expect(COM_BlockSequenceCRCByte(b, 60, 12345)).toBe(v);
    // sequence wraps modulo 1020
    expect(COM_BlockSequenceCRCByte(b, 10, 5)).toBe(COM_BlockSequenceCRCByte(b, 10, 1025));
    expect(() => COM_BlockSequenceCRCByte(b, 10, -1)).toThrow();
  });
  it('COM_BlockSequenceCRCByte reference value (manual computation)', () => {
    // sequence 1019 reads chktbl[1019..1022], i.e. inside the zero tail (entries 960..1023 are 0)
    const base = new Uint8Array([1, 2, 3]);
    const chkb = new Uint8Array([1, 2, 3, 0, 0, 0, 0]);
    const crc = CRC_Block(chkb);
    expect(COM_BlockSequenceCRCByte(base, 3, 1019)).toBe((crc ^ 6) & 0xff);
  });
});

describe('md4', () => {
  it('RFC 1320 test vectors', () => {
    expect(bytesToHex(md4(new Uint8Array(0)))).toBe('31d6cfe0d16ae931b73c59d7e0c089c0');
    expect(bytesToHex(md4(latin1ToBytes('abc')))).toBe('a448017aaf21d8525fc10ae87aa6729d');
    expect(bytesToHex(md4(latin1ToBytes('message digest')))).toBe('d9130a8164549fe818874806e1c7014b');
    expect(
      bytesToHex(
        md4(
          latin1ToBytes('12345678901234567890123456789012345678901234567890123456789012345678901234567890'),
        ),
      ),
    ).toBe('e33b4ddc9c38f2199c3e7b164fcc0536');
  });
  it('Com_BlockChecksum xors digest words (unsigned)', () => {
    const d = md4(latin1ToBytes('abc'));
    const dv = new DataView(d.buffer);
    const x =
      (dv.getUint32(0, true) ^ dv.getUint32(4, true) ^ dv.getUint32(8, true) ^ dv.getUint32(12, true)) >>> 0;
    expect(Com_BlockChecksum(latin1ToBytes('abc'))).toBe(x);
    expect(Com_BlockChecksum(latin1ToBytes('abc'))).toBeGreaterThanOrEqual(0);
  });
  it('handles 55/56/63/64-byte boundaries', () => {
    for (const n of [55, 56, 63, 64, 65, 119, 120, 128]) {
      expect(md4(new Uint8Array(n)).length).toBe(16);
    }
    // known: md4 of 64 zero bytes? just stability between chunked lengths
    expect(bytesToHex(md4(new Uint8Array(56)))).not.toBe(bytesToHex(md4(new Uint8Array(55))));
  });
});
