// Golden: core/pak/pak0.json from the C oracle (docs/FIXTURES.md). Skips when absent.
import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { demoPakPath, fileExists, fixturePath, readArrayBuffer, readJson } from 'q2-shared/testing';
import { Pak } from '../src';

const fixture = fixturePath('core', 'pak', 'pak0.json');
const have = fileExists(fixture) && fileExists(demoPakPath());

interface PakFixture {
  files: { name: string; filepos: number; filelen: number; sha256: string }[];
}

describe.skipIf(!have)('golden core/pak/pak0.json', () => {
  it('matches the oracle directory and file hashes', () => {
    const want = readJson<PakFixture>(fixture);
    const pak = new Pak(readArrayBuffer(demoPakPath())!);
    expect(pak.entries.length).toBe(want.files.length);
    want.files.forEach((f, i) => {
      const e = pak.entries[i]!;
      const ctx = `entry ${i} (${f.name})`;
      if (e.name !== f.name || e.filepos !== f.filepos || e.filelen !== f.filelen) {
        throw new Error(`${ctx}: got ${JSON.stringify(e)} want ${JSON.stringify(f)}`);
      }
      const sha = createHash('sha256').update(pak.readEntry(e)).digest('hex');
      if (sha !== f.sha256) throw new Error(`${ctx}: sha256 ${sha} want ${f.sha256}`);
    });
  });
});
