// Bit-exact comparison against the original snd_dma.c/snd_mix.c/snd_mem.c compiled with the oracle
// flags (vectors from scripts/gen-vectors.sh / oracle/src/snd_main.c): WAV loading + resampling,
// S_SpatializeOrigin, and 90 scripted frames of S_StartSound / S_RawSamples / S_StopAllSounds /
// loop sounds / S_Update with the DMA buffer hash and every channel compared after each frame.
import { existsSync, readFileSync } from 'node:fs';
import { gunzipSync } from 'node:zlib';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { GetWavinfo, Pak, ResampleSfx } from 'q2-formats';
import { demoPakPath, hexToBytes } from 'q2-shared/testing';
import { SoundCore, type Channel, type FrameInfo, type LoopEnt } from '../src/snd_core';

const fr = Math.fround;
type Rec = Record<string, unknown> & { op: string };

function load(name: string): Rec[] {
  const text = gunzipSync(readFileSync(join(__dirname, 'vectors', name))).toString('utf8');
  return text
    .split('\n')
    .filter((l) => l.trim())
    .map((l) => JSON.parse(l) as Rec);
}

function fnv(b: Uint8Array): number {
  let h = 2166136261;
  for (let i = 0; i < b.length; i++) {
    h ^= b[i]!;
    h = Math.imul(h, 16777619) >>> 0;
  }
  return h >>> 0;
}

const v3 = (a: unknown): Float32Array => Float32Array.from(a as number[]);

function chanRow(c: Channel): number[] {
  return [
    c.sfx,
    c.leftvol,
    c.rightvol,
    c.end,
    c.pos,
    c.entnum,
    c.entchannel,
    c.master_vol,
    c.autosound ? 1 : 0,
  ];
}

function run(file: string, portableC8bit: boolean): void {
  const recs = load(file);
  const init = recs[0]!;
  expect(init.op).toBe('init');
  const speed = init.speed as number;
  const loadas8bit = !!init.loadas8bit;
  let dmapos = 0;
  const core = new SoundCore({
    speed,
    samples: init.samples as number,
    volume: fr(init.volume as number),
    getDMAPos: () => dmapos,
    portableC8bit,
  });
  let volume = fr(init.volume as number);
  let volumeModified = false;
  const wavs = new Map<string, Uint8Array>();
  let frames = 0;
  let spats = 0;
  let pendingFrame: Rec | null = null;

  for (const r of recs.slice(1)) {
    switch (r.op) {
      case 'wav':
        wavs.set(r.name as string, hexToBytes(r.hex as string));
        break;
      case 'sfx': {
        const data = wavs.get(r.name as string)!;
        const info = GetWavinfo(r.name as string, data);
        const sc = ResampleSfx(
          {
            length: info.samples,
            loopstart: info.loopstart,
            speed: info.rate,
            width: info.width,
            stereo: info.channels,
          },
          info.rate,
          info.width,
          data.subarray(info.dataofs),
          speed,
          loadas8bit,
        );
        expect([sc.length, sc.loopstart, sc.width]).toEqual([r.length, r.loopstart, r.width]);
        const bytes = new Uint8Array(sc.data.buffer, sc.data.byteOffset, sc.length * sc.width);
        expect(fnv(bytes), `sfx ${r.name as string}`).toBe(r.hash);
        core.sfx.set(r.index as number, { ...sc, name: r.name as string });
        break;
      }
      case 'spat': {
        core.active = r.state === 4;
        core.listener_origin.set(v3(r.lo));
        core.listener_right.set(v3(r.lr));
        const got = core.S_SpatializeOrigin(v3(r.o), fr(r.mv as number), fr(r.dm as number));
        expect(got, JSON.stringify(r)).toEqual([r.l, r.r]);
        spats++;
        break;
      }
      case 'frame': {
        core.active = true;
        for (const e of r.ents as number[][]) {
          core.entityOrigins.set(e[0]!, Float32Array.of(e[1]!, e[2]!, e[3]!));
        }
        for (const s of r.starts as Rec[]) {
          core.S_StartSound(
            s.fixed ? v3(s.o) : null,
            s.ent as number,
            s.chan as number,
            s.sfx as number,
            fr(s.vol as number),
            fr(s.att as number),
            fr(s.ofs as number),
            s.st as number,
          );
        }
        if (r.raw) {
          const raw = r.raw as Rec;
          core.S_RawSamples(
            raw.n as number,
            raw.rate as number,
            raw.w as number,
            raw.c as number,
            hexToBytes(raw.hex as string),
          );
        }
        if (r.stopall) core.S_StopAllSounds();
        if (r.volume !== undefined) {
          volume = fr(r.volume as number);
          volumeModified = true;
        }
        const loops: LoopEnt[] = (r.loops as number[][]).map((l) => ({
          sound: l[0]!,
          sfx: l[1]!,
          origin: Float32Array.of(l[2]!, l[3]!, l[4]!),
        }));
        dmapos = r.dmapos as number;
        const f: FrameInfo = {
          listenerOrigin: v3(r.lo),
          listenerForward: v3(r.lf),
          listenerRight: v3(r.lr),
          listenerUp: v3(r.lu),
          playernum: 0,
          active: true,
          paused: false,
          soundPrepped: true,
          disableScreen: !!r.disable,
          volume,
          volumeModified,
          mixahead: fr(init.mixahead as number),
          testsound: 0,
          show: 0,
          loops,
        };
        volumeModified = false;
        core.S_Update(f);
        pendingFrame = r;
        break;
      }
      case 'state': {
        const ctx = `frame ${pendingFrame?.n as number}`;
        expect([core.paintedtime, core.soundtime, core.s_beginofs, core.s_rawend], ctx).toEqual([
          r.paintedtime,
          r.soundtime,
          r.beginofs,
          r.rawend,
        ]);
        expect(core.channels.map(chanRow), ctx).toEqual(r.ch);
        const buf = core.dma.buffer;
        expect(fnv(new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)), ctx).toBe(r.hash);
        frames++;
        break;
      }
      default:
        throw new Error('unknown op ' + r.op);
    }
  }
  expect(spats).toBe(300);
  expect(frames).toBe(90);
}

describe('snd_dma.c / snd_mix.c oracle vectors', () => {
  it('s_loadas8bit 0 (8 bit WAVs stay 8 bit), portable C', () => run('snd16.jsonl.gz', true));
  it('8 bit samples, portable C S_PaintChannelFrom8 (vol>>11)', () => run('snd8_portable.jsonl.gz', true));
  it('8 bit samples, id386 S_PaintChannelFrom8 row (vol>>3)', () => run('snd8_asm.jsonl.gz', false));
});

describe('S_LoadSound of the demo pak sounds vs the C oracle', () => {
  const pakPath = demoPakPath();
  it.skipIf(!existsSync(pakPath))('every sound/*.wav at 11/22/44 kHz, 8 and 16 bit', () => {
    const pak = new Pak(new Uint8Array(readFileSync(pakPath)));
    const text = gunzipSync(readFileSync(join(__dirname, 'vectors', 'demo_wavs.jsonl.gz'))).toString('utf8');
    let n = 0;
    for (const line of text.split('\n')) {
      if (!line) continue;
      const r = JSON.parse(line) as {
        name: string;
        speed: number;
        loadas8bit: number;
        length: number | null;
        loopstart: number;
        width: number;
        hash: number;
      };
      const data = pak.read('sound/' + r.name)!;
      const info = GetWavinfo(r.name, data);
      expect(info.channels, r.name).toBe(1);
      const sc = ResampleSfx(
        {
          length: info.samples,
          loopstart: info.loopstart,
          speed: info.rate,
          width: info.width,
          stereo: info.channels,
        },
        info.rate,
        info.width,
        data.subarray(info.dataofs),
        r.speed,
        !!r.loadas8bit,
      );
      expect([sc.length, sc.loopstart, sc.width], r.name).toEqual([r.length, r.loopstart, r.width]);
      expect(fnv(new Uint8Array(sc.data.buffer, sc.data.byteOffset, sc.length * sc.width)), r.name).toBe(
        r.hash,
      );
      n++;
    }
    expect(n).toBe(1460);
  });
});
