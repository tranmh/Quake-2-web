// Crafted-asset robustness (docs/review/05-ts-render-sound-app.md): malformed WAV data from a
// user-uploaded pak must not freeze the audio thread or the page. Valid data mixes exactly like C.
import { describe, expect, it } from 'vitest';
import { ATTN_NORM, CHAN_VOICE } from 'q2-shared';
import type { ImageHandle, ModelHandle, Refresh } from 'q2-ref';
import { ClientContext, NullCinematics, NullEffects } from 'q2-client';
import { SoundCore, type CoreSfx, type FrameInfo } from '../src/snd_core';
import { createNullSound } from '../src/index';

const fr = Math.fround;

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

/** Runs `fn`, failing (instead of hanging) when the mixer loop stops making progress. */
function guarded(core: SoundCore, fn: () => void): void {
  const orig = core.S_LoadSound.bind(core);
  let calls = 0;
  core.S_LoadSound = (id: number) => {
    if (++calls > 200_000) throw new Error('mixer spins: S_PaintChannels made no progress');
    return orig(id);
  };
  fn();
}

function sfx(length: number, loopstart: number): CoreSfx {
  return { length, loopstart, speed: 11025, width: 2, stereo: 0, data: new Int16Array(Math.max(0, length)).fill(1000), name: 'x' };
}

describe('mixer vs malformed loop points', () => {
  it('loopstart == length does not spin S_PaintChannels', () => {
    let pos = 0;
    const core = new SoundCore({ speed: 11025, getDMAPos: () => pos });
    core.sfx.set(0, sfx(100, 100));
    core.S_StartSound(null, 1, CHAN_VOICE, 0, 1, ATTN_NORM, 0, 0);
    guarded(core, () => {
      for (let f = 0; f < 5; f++) {
        pos = (pos + 2 * 1102) & (core.dma.samples - 1);
        core.S_Update(frameInfo());
      }
    });
    expect(core.paintedtime).toBeGreaterThan(0);
  });

  it('loopstart beyond the end does not spin S_PaintChannels', () => {
    let pos = 0;
    const core = new SoundCore({ speed: 11025, getDMAPos: () => pos });
    core.sfx.set(0, sfx(100, 5000));
    core.S_StartSound(null, 1, CHAN_VOICE, 0, 1, ATTN_NORM, 0, 0);
    guarded(core, () => {
      pos = (pos + 2 * 1102) & (core.dma.samples - 1);
      core.S_Update(frameInfo());
    });
  });

  it('a zero length looping entity sound (autosound) does not spin', () => {
    let pos = 0;
    const core = new SoundCore({ speed: 11025, getDMAPos: () => pos });
    core.sfx.set(3, sfx(0, 0));
    guarded(core, () => {
      pos = (pos + 2 * 1102) & (core.dma.samples - 1);
      core.S_Update(frameInfo({ loops: [{ sound: 1, sfx: 3, origin: Float32Array.of(10, 0, 0) }] }));
    });
  });

  it('valid loops keep looping', () => {
    let pos = 0;
    const core = new SoundCore({ speed: 11025, getDMAPos: () => pos });
    core.sfx.set(0, sfx(300, 100));
    core.S_StartSound(null, 1, CHAN_VOICE, 0, 1, ATTN_NORM, 0, 0);
    for (let f = 0; f < 5; f++) {
      pos = (pos + 2 * 1102) & (core.dma.samples - 1);
      core.S_Update(frameInfo());
    }
    expect(core.channels.some((c) => c.sfx === 0)).toBe(true);
  });
});

function wav(rate: number, bytes: number, width = 1): Uint8Array {
  const out = new Uint8Array(44 + bytes);
  const dv = new DataView(out.buffer);
  const s = (o: number, t: string) => [...t].forEach((ch, i) => (out[o + i] = ch.charCodeAt(0)));
  s(0, 'RIFF');
  dv.setUint32(4, 36 + bytes, true);
  s(8, 'WAVE');
  s(12, 'fmt ');
  dv.setUint32(16, 16, true);
  dv.setUint16(20, 1, true); // PCM
  dv.setUint16(22, 1, true); // mono
  dv.setUint32(24, rate, true);
  dv.setUint32(28, rate * width, true);
  dv.setUint16(32, width, true);
  dv.setUint16(34, width * 8, true);
  s(36, 'data');
  dv.setUint32(40, bytes, true);
  out.fill(128, 44);
  return out;
}

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

function mainSound(files: Record<string, Uint8Array>) {
  const sound = createNullSound({ framesPlayed: () => 0 });
  const c = new ClientContext({
    refresh: stubRefresh(),
    transport: () => {
      throw new Error('no network');
    },
    loadFile: async (name) => files[name] ?? null,
    sound,
    effects: new NullEffects(),
    cinematics: new NullCinematics(),
    host: {},
  });
  sound.attach(c);
  sound.init();
  return sound;
}

describe('S_LoadSound vs crafted WAV headers', () => {
  it('a 2 KB WAV claiming 1 Hz is not resampled into tens of millions of samples', async () => {
    // outcount = samples / (rate / dma.speed) = 2000 * 11025: C allocates and loops over 22M samples
    const sound = mainSound({ 'sound/evil.wav': wav(1, 2000) });
    const s = sound.S_FindName('evil.wav', true)!;
    expect(await sound.S_LoadSound(s)).toBeNull();
  });

  it('a normal 11 kHz WAV still loads', async () => {
    const sound = mainSound({ 'sound/ok.wav': wav(11025, 2000) });
    const s = sound.S_FindName('ok.wav', true)!;
    const sc = await sound.S_LoadSound(s);
    expect(sc?.length).toBe(2000);
  });
});
