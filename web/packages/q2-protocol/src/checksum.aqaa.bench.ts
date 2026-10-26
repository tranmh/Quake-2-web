import { bench, describe } from 'vitest';
import { CRC_Block, COM_BlockSequenceCRCByte } from './crc';
import { md4, Com_BlockChecksum } from './md4';

// Deterministic input: a simple LCG fill, built once outside the measured part.
function makeBytes(n: number, seed = 12345): Uint8Array {
  const out = new Uint8Array(n);
  let s = seed >>> 0;
  for (let i = 0; i < n; i++) {
    s = (Math.imul(s, 1103515245) + 12345) >>> 0;
    out[i] = s >>> 24;
  }
  return out;
}

const SIZES = [64, 4096, 262144] as const;
const inputs = SIZES.map((n) => [n, makeBytes(n)] as const);

let sink = 0;

describe('CRC_Block', () => {
  for (const [n, data] of inputs) {
    bench(`${n} bytes`, () => {
      sink ^= CRC_Block(data);
    });
  }
});

describe('COM_BlockSequenceCRCByte', () => {
  // The protocol checksums at most a short prefix of each message.
  for (const n of [16, 30, 60]) {
    const data = makeBytes(n);
    bench(`${n} bytes`, () => {
      sink ^= COM_BlockSequenceCRCByte(data, n, 1234);
    });
  }
});

describe('md4', () => {
  for (const [n, data] of inputs) {
    bench(`${n} bytes`, () => {
      sink ^= md4(data)[0]!;
    });
  }
});

describe('Com_BlockChecksum', () => {
  for (const [n, data] of inputs) {
    bench(`${n} bytes`, () => {
      sink ^= Com_BlockChecksum(data);
    });
  }
});

export { sink };
