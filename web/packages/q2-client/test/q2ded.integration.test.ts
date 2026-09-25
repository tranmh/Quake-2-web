// Full integration test against the REAL original C dedicated server (oracle/build/bin/q2ded):
// challenge -> connect -> new -> serverdata/configstrings/baselines -> precache -> begin -> ca_active,
// then movement commands for a few seconds while checking frame parsing and prediction.
import { spawn, type ChildProcess } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync, symlinkSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createSocket } from 'node:dgram';
import { afterAll, describe, expect, it } from 'vitest';
import { Pak } from 'q2-formats';
import { demoPakPath, repoRoot } from 'q2-shared/testing';
import type { ImageHandle, ModelHandle, Refresh } from 'q2-ref';
import { createClientEngine } from '../src/engine';
import { ca_active } from '../src/client';
import { UdpTransport } from './udp_transport';

const Q2DED = join(repoRoot(), 'oracle', 'build', 'bin', 'q2ded');
const GAME_SO = join(repoRoot(), 'oracle', 'build', 'bin', 'baseq2', 'game.so');
const PAK = demoPakPath();
const available = existsSync(Q2DED) && existsSync(GAME_SO) && existsSync(PAK);

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

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function waitForServer(port: number, timeoutMs: number): Promise<boolean> {
  const sock = createSocket('udp4');
  let ok = false;
  sock.on('message', () => {
    ok = true;
  });
  const end = Date.now() + timeoutMs;
  try {
    while (!ok && Date.now() < end) {
      sock.send(Buffer.from('\xff\xff\xff\xffinfo 34\n', 'latin1'), port, '127.0.0.1');
      await sleep(200);
    }
  } finally {
    sock.close();
  }
  return ok;
}

describe.skipIf(!available)('integration with the original q2ded', () => {
  let proc: ChildProcess | null = null;
  let dir = '';

  afterAll(() => {
    if (proc && proc.exitCode === null) proc.kill('SIGKILL');
    if (dir) rmSync(dir, { recursive: true, force: true });
  });

  it('connects, reaches ca_active, moves and predicts exactly', async () => {
    dir = mkdtempSync(join(tmpdir(), 'q2ded-'));
    mkdirSync(join(dir, 'baseq2'));
    symlinkSync(PAK, join(dir, 'baseq2', 'pak0.pak'));
    copyFileSync(GAME_SO, join(dir, 'baseq2', 'game.so'));
    const port = 28000 + Math.floor(Math.random() * 2000);
    let serverOut = '';
    proc = spawn(
      Q2DED,
      ['+set', 'dedicated', '1', '+set', 'deathmatch', '1', '+set', 'port', String(port), '+map', 'demo1'],
      { cwd: dir, stdio: ['ignore', 'pipe', 'pipe'] },
    );
    proc.stdout!.on('data', (d: Buffer) => (serverOut += d.toString('latin1')));
    proc.stderr!.on('data', (d: Buffer) => (serverOut += d.toString('latin1')));
    expect(await waitForServer(port, 10000), 'q2ded did not answer: ' + serverOut).toBe(true);

    const pak = new Pak(new Uint8Array(readFileSync(PAK)));
    let printed = '';
    const errors: string[] = [];
    const levels: string[] = [];
    const demos: { name: string; data: Uint8Array }[] = [];
    const tr: { t: UdpTransport | null } = { t: null };
    const engine = await createClientEngine({
      refresh: stubRefresh(),
      transport: (addr) => (tr.t = new UdpTransport(addr)),
      loadFile: async (name) => pak.read(name) ?? null,
      host: {
        onPrint: (t) => (printed += t),
        onError: (m) => errors.push(m),
        onLevel: (l) => levels.push(l.mapname),
        onDemoRecorded: (name, data) => demos.push({ name, data }),
      },
      configText: 'set cl_showmiss 1\nset name tsclient\n',
    });
    const c = engine.context;

    engine.connect(`127.0.0.1:${port}`);

    // run frames in real time until active
    const deadline = Date.now() + 30000;
    while (c.cls.state !== ca_active && Date.now() < deadline && !errors.length) {
      engine.frame();
      await sleep(5);
    }
    expect(errors, printed).toEqual([]);
    expect(c.cls.state, printed).toBe(ca_active);
    expect(levels).toEqual(['demo1']);
    expect(c.cl.refresh_prepped).toBe(true);
    expect(c.cl.configstrings[1 + 32]).toBe('maps/demo1.bsp'); // CS_MODELS+1

    // settle for a second (spawn / teleport effects), then count prediction misses from here on
    const settle = Date.now() + 1000;
    while (Date.now() < settle) {
      engine.frame();
      await sleep(5);
    }
    const origin0 = Array.from(c.cl.frame.playerstate.pmove.origin);
    const frame0 = c.cl.frame.serverframe;
    const missesBefore = (printed.match(/prediction miss/g) ?? []).length;

    // move: forward for 1.5 s, turn right while moving for 1 s, strafe + jump for 1 s
    let maxErr = 0;
    let validFrames = 0;
    let lastServerframe = -1;
    const recordedOrigins = new Map<number, string>();
    const run = async (ms: number) => {
      const end = Date.now() + ms;
      while (Date.now() < end) {
        engine.frame();
        const e = c.cl.prediction_error;
        maxErr = Math.max(maxErr, Math.abs(e[0]!), Math.abs(e[1]!), Math.abs(e[2]!));
        if (c.cl.frame.valid && c.cl.frame.serverframe !== lastServerframe) {
          validFrames++;
          lastServerframe = c.cl.frame.serverframe;
          if (c.cls.demorecording && !c.cls.demowaiting)
            recordedOrigins.set(lastServerframe, Array.from(c.cl.frame.playerstate.pmove.origin).join(','));
        }
        await sleep(5);
      }
    };
    engine.exec('record itest');
    engine.exec('+forward');
    await run(1500);
    engine.exec('+right');
    await run(1000);
    engine.exec('-right');
    engine.exec('+moveleft');
    engine.exec('+moveup');
    await run(1000);
    engine.exec('-moveup');
    engine.exec('-moveleft');
    engine.exec('-forward');
    await run(500);
    engine.exec('stop');
    engine.frame();

    const origin1 = Array.from(c.cl.frame.playerstate.pmove.origin);
    const dist = Math.hypot(origin1[0]! - origin0[0]!, origin1[1]! - origin0[1]!) / 8;
    const misses = (printed.match(/prediction miss[^\n]*/g) ?? []).slice(missesBefore);
    if (misses.length) console.log('prediction misses:\n' + misses.join('\n'));
    console.log(
      `q2ded integration: frames ${frame0}..${c.cl.frame.serverframe} (${validFrames} parsed while moving), ` +
        `moved ${dist.toFixed(1)} units, max prediction error ${maxErr}, misses ${misses.length}, ` +
        `datagrams sent ${tr.t?.sentCount} recv ${tr.t?.recvCount}`,
    );

    expect(errors, printed).toEqual([]);
    expect(c.cls.state).toBe(ca_active);
    expect(validFrames).toBeGreaterThan(25); // ~10 Hz for 4 s
    expect(dist).toBeGreaterThan(50);
    expect(misses.length).toBe(0);
    expect(maxErr).toBe(0);

    engine.disconnect();
    await sleep(200);
    expect(c.cls.state).toBe(1); // ca_disconnected

    // ---- play the recorded .dm2 back in a fresh client
    expect(demos.length).toBe(1);
    expect(demos[0]!.name).toBe('baseq2/demos/itest.dm2');
    const demo = demos[0]!.data;
    expect(new DataView(demo.buffer, demo.byteOffset + demo.length - 4).getInt32(0, true)).toBe(-1);
    const perrors: string[] = [];
    let pprinted = '';
    const player = await createClientEngine({
      refresh: stubRefresh(),
      transport: () => {
        throw new Error('no network during playback');
      },
      loadFile: async (name) => pak.read(name) ?? null,
      host: { onError: (m) => perrors.push(m), onPrint: (t) => (pprinted += t) },
      configText: '',
    });
    const p = player.context;
    player.playDemo('itest', demo);
    const matched: number[] = [];
    const mismatched: number[] = [];
    let lastPf = -1;
    const pend = Date.now() + 15000;
    while (p.main.demoplaying && Date.now() < pend) {
      player.frame();
      if (p.cls.state === ca_active && p.cl.frame.valid && p.cl.frame.serverframe !== lastPf) {
        lastPf = p.cl.frame.serverframe;
        const want = recordedOrigins.get(lastPf);
        if (want !== undefined) {
          expect(p.cl.attractloop).toBe(true);
          if (want === Array.from(p.cl.frame.playerstate.pmove.origin).join(',')) matched.push(lastPf);
          else mismatched.push(lastPf);
        }
      }
      await sleep(5);
    }
    console.log(
      `demo playback: ${demo.length} bytes, ${matched.length} frames matched, ${mismatched.length} mismatched`,
    );
    expect(perrors, pprinted).toEqual([]);
    expect(pprinted).toContain('Demo finished.');
    expect(mismatched).toEqual([]);
    expect(matched.length).toBeGreaterThan(20);
  }, 90000);
});
