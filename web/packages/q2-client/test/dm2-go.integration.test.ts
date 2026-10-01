// Plays the .dm2 recorded by the Go server's agent session (server/internal/demo Recorder: a seeded
// lockstep bot walking on demo1, fixtures/agent/demo1-walker-10s.dm2) with the unchanged client's demo
// playback, and checks CL_Record_f header parity with the Go port: recording right after the first
// frame must write exactly fixtures/agent/demo1-walker-10s.record-header.dm2, which the Go
// demo.Writer produced for the same client state (regenerate both with
// `Q2_UPDATE_FIXTURES=1 go test ./internal/demo -run TestFixtureDemo1Walker`).
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { Pak } from 'q2-formats';
import { demoPakPath, repoRoot } from 'q2-shared/testing';
import type { ImageHandle, ModelHandle, Refresh } from 'q2-ref';
import { createClientEngine, type ClientEngine } from '../src/engine';
import { ca_active } from '../src/client';

const PAK = demoPakPath();
const FIXTURE = join(repoRoot(), 'fixtures', 'agent', 'demo1-walker-10s.dm2');
const HEADER = join(repoRoot(), 'fixtures', 'agent', 'demo1-walker-10s.record-header.dm2');
const available = existsSync(PAK) && existsSync(FIXTURE) && existsSync(HEADER);

function stubRefresh(): Refresh {
  const h = (name: string) => ({ name });
  return {
    async init() {
      return true;
    },
    shutdown() {},
    async beginRegistration() {},
    async registerModel(name) {
      return h(name) as unknown as ModelHandle;
    },
    async registerSkin(name) {
      return h(name) as unknown as ImageHandle;
    },
    async registerPic(name) {
      return h(name) as unknown as ImageHandle;
    },
    async setSky() {},
    endRegistration() {},
    renderFrame() {},
    drawGetPicSize() {
      return [24, 24];
    },
    drawPic() {},
    drawStretchPic() {},
    drawChar() {},
    drawTileClear() {},
    drawFill() {},
    drawFadeScreen() {},
    drawStretchRaw() {},
    cinematicSetPalette() {},
    beginFrame() {},
    endFrame() {},
    appActivate() {},
  };
}

/** A player engine on a virtual clock: `step(ms)` advances it and runs one frame. */
async function newPlayer() {
  const pak = new Pak(new Uint8Array(readFileSync(PAK)));
  const errors: string[] = [];
  const demos: { name: string; data: Uint8Array }[] = [];
  let printed = '';
  let now = 1000;
  const engine: ClientEngine = await createClientEngine({
    refresh: stubRefresh(),
    transport: () => {
      throw new Error('no network during playback');
    },
    loadFile: async (name) => pak.read(name) ?? null,
    host: {
      onError: (m) => errors.push(m),
      onPrint: (t) => (printed += t),
      onDemoRecorded: (name, data) => demos.push({ name, data }),
    },
    configText: '',
    milliseconds: () => now,
  });
  const step = async (ms: number) => {
    now += ms;
    engine.frame(ms);
    // let the asynchronous precache (model / sound loads) make progress
    await new Promise((r) => setImmediate(r));
  };
  return { engine, errors, demos, printed: () => printed, step };
}

describe.skipIf(!available)('Go-recorded .dm2 in the TS client', () => {
  it('plays demo1-walker-10s.dm2 to the end', async () => {
    const data = new Uint8Array(readFileSync(FIXTURE));
    const p = await newPlayer();
    const c = p.engine.context;
    p.engine.playDemo('demo1-walker-10s', data);
    const frames: number[] = [];
    let origin0: number[] | undefined;
    let origin1: number[] = [];
    for (let i = 0; i < 4000 && c.main.demoplaying; i++) {
      await p.step(25);
      if (c.cls.state === ca_active && c.cl.frame.valid && c.cl.frame.serverframe !== frames.at(-1)) {
        frames.push(c.cl.frame.serverframe);
        expect(c.cl.attractloop).toBe(true);
        origin1 = Array.from(c.cl.frame.playerstate.pmove.origin);
        origin0 ??= origin1;
      }
    }
    expect(p.errors, p.printed()).toEqual([]);
    expect(c.main.demoplaying).toBe(false);
    expect(p.printed()).toContain('Demo finished.');
    // every frame of the recording was reconstructed, in order (no delta from a missing frame)
    expect(frames.length).toBeGreaterThanOrEqual(100);
    expect(frames).toEqual(frames.map((_, i) => frames[0]! + i));
    expect(p.printed()).not.toContain('Delta');
    // the walker moved
    const dist = Math.hypot(origin1[0]! - origin0![0]!, origin1[1]! - origin0![1]!) / 8;
    expect(dist).toBeGreaterThan(64);
  });

  it('CL_Record_f writes the same header bytes as the Go port', async () => {
    const data = new Uint8Array(readFileSync(FIXTURE));
    const want = new Uint8Array(readFileSync(HEADER));
    const p = await newPlayer();
    const c = p.engine.context;
    p.engine.playDemo('demo1-walker-10s', data);
    // run until the first frame was parsed (blocks arrive 100 ms apart, steps are 5 ms)
    for (let i = 0; i < 4000 && !(c.cls.state === ca_active && c.cl.frame.valid); i++) await p.step(5);
    expect(c.cls.state).toBe(ca_active);
    const first = c.cl.frame.serverframe;
    // record + stop before any further message is parsed (frame(0) runs the command buffer only)
    p.engine.exec('record parity');
    p.engine.frame(0);
    p.engine.exec('stop');
    p.engine.frame(0);
    expect(c.cl.frame.serverframe).toBe(first);
    expect(p.errors, p.printed()).toEqual([]);
    expect(p.demos.map((d) => d.name)).toEqual(['baseq2/demos/parity.dm2']);
    const got = p.demos[0]!.data;
    expect(got.length).toBe(want.length);
    expect(Buffer.from(got).equals(Buffer.from(want))).toBe(true);
  });
});
