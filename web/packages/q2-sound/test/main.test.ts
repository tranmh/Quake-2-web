// MainSound (main-thread snd_dma.c) driving the in-process mixer (createNullSound): registration,
// loading from the demo pak, sexed sounds, S_StartSound -> channels, loop sounds, raw samples, stop,
// hand-computed spatialization, batch protocol and the worklet processor (with a SharedArrayBuffer ring).
import { existsSync, readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { Pak } from 'q2-formats';
import { ATTN_NORM, ATTN_STATIC, CHAN_VOICE, CS_PLAYERSKINS, CS_SOUNDS, MAX_PARSE_ENTITIES } from 'q2-shared';
import { demoPakPath } from 'q2-shared/testing';
import type { ImageHandle, ModelHandle, Refresh } from 'q2-ref';
import { ClientContext, NullCinematics, NullEffects, ca_active } from 'q2-client';
import { createNullSound } from '../src/index';
import { SoundCore, type FrameInfo } from '../src/snd_core';
import { BatchWriter, applyBatch, writeStart, writeUpdate } from '../src/protocol';
import { SabRing } from '../src/ring';
import { CoreHost } from '../src/host';

const fr = Math.fround;
const PAK = demoPakPath();

function stubRefresh(): Refresh {
  const h = (name: string) => ({ name });
  return {
    async init() {
      return true;
    },
    shutdown() {},
    async beginRegistration() {},
    async registerModel(n) {
      return h(n) as unknown as ModelHandle;
    },
    async registerSkin(n) {
      return h(n) as unknown as ImageHandle;
    },
    async registerPic(n) {
      return h(n) as unknown as ImageHandle;
    },
    async setSky() {},
    endRegistration() {},
    renderFrame() {},
    drawGetPicSize: () => [0, 0],
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

function frameInfo(over: Partial<FrameInfo> = {}): FrameInfo {
  return {
    listenerOrigin: [0, 0, 0],
    listenerForward: [1, 0, 0],
    listenerRight: [0, -1, 0],
    listenerUp: [0, 0, 1],
    playernum: 0,
    active: true,
    paused: false,
    soundPrepped: true,
    disableScreen: false,
    volume: fr(0.7),
    volumeModified: false,
    mixahead: fr(0.2),
    testsound: 0,
    show: 0,
    loops: [],
    ...over,
  };
}

describe('S_SpatializeOrigin (hand-computed from the C formula)', () => {
  const core = new SoundCore({ speed: 22050 });
  core.active = true;
  core.listener_right.set([0, 1, 0]);
  it('source 180 units to the right, ATTN_NORM', () => {
    // dist = 180 - 80 = 100; *0.0005f = 0.05f; dot = 1 -> rscale 1, lscale 0
    // right = (int)(255 * (float)((1.0 - 0.05f) * 1)) = (int)242.25 = 242
    const dm = fr(ATTN_NORM * 0.0005);
    expect(core.S_SpatializeOrigin([0, 180, 0], 255, dm)).toEqual([0, 242]);
  });
  it('source 180 units ahead: 50/50 pan', () => {
    // dot = 0 -> 0.5 each; scale = 0.95*0.5 = 0.475 -> (int)(255*0.475f) = 121
    expect(core.S_SpatializeOrigin([180, 0, 0], 255, fr(0.0005))).toEqual([121, 121]);
  });
  it('inside SOUND_FULLVOLUME, behind-left, static attenuation', () => {
    // dist 50 -> 0; dot = -1 -> rscale 0, lscale 1
    expect(core.S_SpatializeOrigin([0, -50, 0], 200, fr(ATTN_STATIC * 0.001))).toEqual([200, 0]);
  });
  it('dist_mult 0 means no spatialization; not active means 255/255', () => {
    expect(core.S_SpatializeOrigin([0, -5000, 0], 100, 0)).toEqual([100, 100]);
    core.active = false;
    expect(core.S_SpatializeOrigin([0, -5000, 0], 100, fr(0.0005))).toEqual([255, 255]);
    core.active = true;
  });
  it('far away: clamped at 0', () => {
    expect(core.S_SpatializeOrigin([0, 5000, 0], 255, fr(0.001))).toEqual([0, 0]);
  });
});

describe('mixer determinism', () => {
  it('scripted channels produce identical DMA output across runs', () => {
    const mk = () => {
      let pos = 0;
      const core = new SoundCore({ speed: 11025, getDMAPos: () => pos });
      const data = new Int16Array(3000).map((_, i) => ((i * 37) % 2000) - 1000);
      core.sfx.set(0, { length: 3000, loopstart: 1000, speed: 11025, width: 2, stereo: 0, data, name: 'x' });
      core.entityOrigins.set(3, Float32Array.of(300, 0, 0));
      core.S_StartSound(null, 3, CHAN_VOICE, 0, 1, ATTN_NORM, 0, 0);
      core.S_StartSound(Float32Array.of(0, 400, 0), 5, 0, 0, fr(0.5), ATTN_NORM, fr(0.05), 0);
      for (let f = 0; f < 20; f++) {
        pos = (pos + 2 * 1102) & (core.dma.samples - 1);
        core.S_Update(frameInfo());
      }
      return core;
    };
    const a = mk();
    const b = mk();
    expect(a.paintedtime).toBeGreaterThan(20000);
    expect(Array.from(a.dma.buffer)).toEqual(Array.from(b.dma.buffer));
    expect(a.dma.buffer.some((v) => v !== 0)).toBe(true);
    // the looped sound keeps playing
    expect(a.channels.filter((c) => c.sfx === 0).length).toBe(2);
  });

  it('command batches replay the direct calls exactly', () => {
    const run = (viaBatch: boolean) => {
      let pos = 0;
      const core = new SoundCore({ speed: 22050, getDMAPos: () => pos });
      const data = new Int8Array(5000).map((_, i) => (i * 13) % 200) as Int8Array;
      core.sfx.set(7, { length: 5000, loopstart: -1, speed: 22050, width: 1, stereo: 0, data, name: 'y' });
      for (let f = 0; f < 10; f++) {
        pos = (pos + 4410) & (core.dma.samples - 1);
        const fi = frameInfo({ loops: [{ sound: 1, sfx: 7, origin: Float32Array.of(100, 100, 0) }] });
        if (viaBatch) {
          const w = new BatchWriter();
          writeStart(w, Float32Array.of(10, 20, 30), 2, 1, 7, fr(0.8), 1, 0, f * 100);
          writeUpdate(w, fi);
          applyBatch(core, w.take());
        } else {
          core.S_StartSound(Float32Array.of(10, 20, 30), 2, 1, 7, fr(0.8), 1, 0, f * 100);
          core.S_Update(fi);
        }
      }
      return Array.from(core.dma.buffer);
    };
    expect(run(true)).toEqual(run(false));
  });
});

describe('SabRing / CoreHost ordering', () => {
  it('applies uploads and batches strictly in sequence', () => {
    const host = new CoreHost({ speed: 11025 });
    const ring = SabRing.create(256);
    const w = new BatchWriter();
    writeStart(w, Float32Array.of(0, 0, 0), 1, 1, 3, 1, 1, 0, 0);
    ring.push(2, w.take()); // batch 2 needs upload 1
    host.drainRing(ring);
    expect(host.core.s_pendingplays.next).toBe(host.core.s_pendingplays); // not applied yet
    host.onSfx(1, 3, {
      length: 10,
      loopstart: -1,
      speed: 11025,
      width: 2,
      stereo: 0,
      data: new Int16Array(10),
      name: 'z',
    });
    expect(host.core.s_pendingplays.next.sfx).toBe(3);
    // wrap-around of the ring
    for (let i = 3; i < 40; i++) {
      expect(ring.push(i, new Uint8Array([2]))).toBe(true); // CMD_STOPALL
      host.drainRing(ring);
    }
    expect(ring.peekSeq()).toBe(-1);
    expect(host.core.s_pendingplays.next).toBe(host.core.s_pendingplays);
  });
});

describe('worklet processor', () => {
  it('registers, drains the ring and plays the DMA buffer', async () => {
    const g = globalThis as Record<string, unknown>;
    let Proc:
      | (new (o: unknown) => {
          port: { onmessage: ((e: { data: unknown }) => void) | null };
          process(i: unknown, o: Float32Array[][]): boolean;
        })
      | null = null;
    class FakeBase {
      readonly port = { onmessage: null as ((e: { data: unknown }) => void) | null, postMessage() {} };
    }
    g['AudioWorkletProcessor'] = FakeBase;
    g['sampleRate'] = 22050;
    g['registerProcessor'] = (name: string, ctor: typeof Proc) => {
      expect(name).toBe('q2-sound');
      Proc = ctor;
    };
    try {
      await import('../src/worklet');
      expect(Proc).not.toBeNull();
      const ring = SabRing.create(4096);
      const p = new Proc!({ processorOptions: { speed: 22050, ring: ring.sab } });
      const data = new Int16Array(20000).fill(12000);
      p.port.onmessage!({
        data: {
          type: 'sfx',
          seq: 1,
          id: 0,
          sfx: { length: 20000, loopstart: -1, speed: 22050, width: 2, stereo: 0, data, name: 'w' },
        },
      });
      const w = new BatchWriter();
      writeStart(w, null, 1, 0, 0, 1, 0, 0, 0); // entnum = playernum+1: full volume
      writeUpdate(w, frameInfo());
      ring.push(2, w.take());
      const L = new Float32Array(128);
      const R = new Float32Array(128);
      expect(p.process([], [[L, R]])).toBe(true);
      // 12000 * (255*179) >> 8 >> 8 = 8364 (snd_vol = (int)(0.7f*256) = 179)
      expect(Math.round(L[0]! * 32768)).toBe(((12000 * 255 * 179) >> 8) >> 8);
      expect(R[5]).toBe(L[5]);
    } finally {
      delete g['AudioWorkletProcessor'];
      delete g['registerProcessor'];
      delete g['sampleRate'];
    }
  });
});

describe.skipIf(!existsSync(PAK))('MainSound + in-process mixer with the demo pak', () => {
  it('registers, loads, starts, loops, streams raw samples and stops', async () => {
    const pak = new Pak(new Uint8Array(readFileSync(PAK)));
    let frames = 0;
    const sound = createNullSound({ framesPlayed: () => frames });
    let printed = '';
    const loaded: string[] = [];
    const c = new ClientContext({
      refresh: stubRefresh(),
      transport: () => {
        throw new Error('no network');
      },
      loadFile: async (name) => {
        loaded.push(name);
        return pak.read(name) ?? null;
      },
      sound,
      effects: new NullEffects(),
      cinematics: new NullCinematics(),
      host: { onPrint: (t) => (printed += t) },
    });
    sound.attach(c);
    sound.init();
    expect(printed).toContain('sound sampling rate: 11025');
    const core = sound.backend.core!;
    expect(core.dma.speed).toBe(11025);

    // registration (CL_RegisterSounds)
    sound.beginRegistration();
    const hit = sound.registerSound('weapons/blastf1a.wav');
    const loop = sound.registerSound('world/amb10.wav');
    const gone = sound.registerSound('misc/tele1.wav');
    expect(hit).not.toBeNull();
    await sound.endRegistration();
    const ids = [hit, loop, gone].map((h) => (h as unknown as { index: number }).index);
    c.cls.state = ca_active;
    c.cl.sound_prepped = true;
    c.cl.playernum = 0;
    const org = new Float32Array(3);
    const fwd = Float32Array.of(1, 0, 0);
    const right = Float32Array.of(0, -1, 0);
    const up = Float32Array.of(0, 0, 1);
    sound.update(org, fwd, right, up);
    for (const id of ids) expect(core.sfx.get(id)?.width).toBe(1); // s_loadas8bit 1

    // a player sound (full volume) and a positioned one
    sound.startSound(null, 1, CHAN_VOICE, hit, 1, ATTN_NORM, 0);
    sound.startSound(Float32Array.of(0, 300, 0), 7, 2, hit, 1, ATTN_NORM, 0);
    // loop sound from a frame entity
    c.cl.sound_precache[5] = loop;
    c.cl.configstrings[CS_SOUNDS + 5] = 'world/amb10.wav';
    c.cl.frame.parse_entities = 0;
    c.cl.frame.num_entities = 1;
    const ent = c.cl_parse_entities[0 & (MAX_PARSE_ENTITIES - 1)]!;
    ent.sound = 5;
    ent.origin.set([100, 0, 0]);
    frames += 1102;
    sound.update(org, fwd, right, up);
    const active = core.channels.filter((ch) => ch.sfx >= 0);
    const player = active.find((ch) => ch.entnum === 1)!;
    expect([player.leftvol, player.rightvol]).toEqual([255, 255]);
    const pos = active.find((ch) => ch.entnum === 7)!;
    expect(pos.leftvol).toBeGreaterThan(pos.rightvol); // right vector points to -y
    const auto = active.find((ch) => ch.autosound)!;
    expect(auto.sfx).toBe(ids[1]);
    expect(core.dma.buffer.some((v) => v !== 0)).toBe(true);

    // raw samples (cinematic audio)
    sound.rawSamples(100, 11025, 2, 2, new Uint8Array(400).fill(1));
    frames += 1102;
    sound.update(org, fwd, right, up);
    expect(core.s_rawend).toBeGreaterThan(0);

    // sexed sound: players/female/ is not in the demo pak -> alias to player/male/
    c.cl.configstrings[CS_PLAYERSKINS + 0] = 'tsclient\\female/athena';
    c.cl_entities[1]!.current.number = 1;
    const sexed = sound.registerSound('*pain25_1.wav');
    sound.startSound(null, 1, CHAN_VOICE, sexed, 1, ATTN_NORM, 0);
    await new Promise((r) => setTimeout(r, 20));
    expect(loaded).toContain('players/female/pain25_1.wav');
    expect(loaded).toContain('sound/player/male/pain25_1.wav');
    const alias = sound.S_FindName('#players/female/pain25_1.wav', false)!;
    expect(alias.truename).toBe('player/male/pain25_1.wav');
    frames += 1102;
    sound.update(org, fwd, right, up);
    expect(core.channels.some((ch) => ch.sfx === alias.index)).toBe(true);

    // S_StartLocalSound + s_show
    c.cvars.set('s_show', '1');
    sound.startLocalSound('weapons/blastf1a.wav');
    frames += 1102;
    sound.update(org, fwd, right, up);
    expect(printed).toMatch(/Issue \d+/);
    expect(printed).toMatch(/----\(\d+\)---- painted: \d+/);
    c.cvars.set('s_show', '0');

    // soundlist
    c.cmd.executeString('soundlist');
    expect(printed).toMatch(/Total resident: \d+/);

    // stop everything
    sound.stopAllSounds();
    frames += 1102;
    sound.update(org, fwd, right, up);
    expect(core.channels.filter((ch) => ch.sfx >= 0 && !ch.autosound)).toEqual([]);

    // next registration frees the unused sound in the mixer
    sound.beginRegistration();
    sound.registerSound('weapons/blastf1a.wav');
    await sound.endRegistration();
    sound.update(org, fwd, right, up);
    expect(core.sfx.has(ids[0]!)).toBe(true);
    expect(core.sfx.has(ids[2]!)).toBe(false);

    sound.shutdown();
    expect(sound.registerSound('x.wav')).toBeNull();
  });
});
