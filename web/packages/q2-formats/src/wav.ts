// WAV loading and resampling, a port of client/snd_mem.c (GetWavinfo, ResampleSfx, S_LoadSound).
import { toU8 } from './bytes';
import { FormatError } from './errors';

// C: client/snd_loc.h wavinfo_t
export interface WavInfo {
  rate: number;
  width: number;
  channels: number;
  loopstart: number;
  samples: number;
  dataofs: number;
  /** message the C code would Com_Printf when it returns an incomplete info (not part of wavinfo_t) */
  error?: string;
}

// C: client/snd_loc.h sfxcache_t
export interface SfxCache {
  length: number;
  loopstart: number;
  speed: number;
  width: number;
  stereo: number;
  /** width 1: signed 8-bit samples; width 2: signed 16-bit samples */
  data: Int8Array | Int16Array;
}

/** The C parser state (data_p, iff_end, last_chunk, iff_data, iff_chunk_len) as an instance. */
class IffParser {
  data_p = -1; // -1 == NULL
  iff_end = 0;
  last_chunk = 0;
  iff_data = 0;
  iff_chunk_len = 0;
  constructor(readonly wav: Uint8Array) {}

  private byte(o: number): number {
    // Reads past the buffer are undefined in C; they only happen for truncated/corrupt files.
    if (o < 0 || o >= this.wav.length) throw new FormatError('wav: read out of bounds');
    return this.wav[o]!;
  }

  // C: client/snd_mem.c:178 GetLittleShort (result narrowed to short)
  GetLittleShort(): number {
    let val = this.byte(this.data_p);
    val = val + (this.byte(this.data_p + 1) << 8);
    this.data_p += 2;
    return (val << 16) >> 16;
  }

  // C: client/snd_mem.c:187 GetLittleLong
  GetLittleLong(): number {
    let val = this.byte(this.data_p);
    val = val + (this.byte(this.data_p + 1) << 8);
    val = val + (this.byte(this.data_p + 2) << 16);
    val = (val + (this.byte(this.data_p + 3) << 24)) | 0;
    this.data_p += 4;
    return val;
  }

  matches(o: number, name: string): boolean {
    // strncmp(data_p, name, 4) with a 4-char name: a plain byte comparison (a NUL mismatches)
    for (let i = 0; i < 4; i++) {
      if (o + i >= this.wav.length) return false;
      if (this.wav[o + i] !== name.charCodeAt(i)) return false;
    }
    return true;
  }

  // C: client/snd_mem.c:198 FindNextChunk
  FindNextChunk(name: string): void {
    for (;;) {
      this.data_p = this.last_chunk;
      if (this.data_p >= this.iff_end) {
        this.data_p = -1;
        return;
      }
      this.data_p += 4;
      if (this.data_p + 4 > this.wav.length) {
        // C would read past the end of the file buffer here; treat as "not found".
        this.data_p = -1;
        return;
      }
      this.iff_chunk_len = this.GetLittleLong();
      if (this.iff_chunk_len < 0) {
        this.data_p = -1;
        return;
      }
      this.data_p -= 8;
      this.last_chunk = this.data_p + 8 + ((this.iff_chunk_len + 1) & ~1);
      if (this.matches(this.data_p, name)) return;
    }
  }

  // C: client/snd_mem.c:226 FindChunk
  FindChunk(name: string): void {
    this.last_chunk = this.iff_data;
    this.FindNextChunk(name);
  }
}

// C: client/snd_mem.c:257 GetWavinfo
// Quirks reproduced: chunks are searched linearly from the start of the RIFF body; the loop length is read
// from a LIST chunk found anywhere after the cue chunk ("not a proper parse, but it works with cooledit"),
// at fixed offsets 24 ("mark" at 28); "bad loop length" is an error (Com_Error ERR_DROP).
export function GetWavinfo(name: string, data: ArrayBuffer | Uint8Array): WavInfo {
  const wav = toU8(data);
  const info: WavInfo = { rate: 0, width: 0, channels: 0, loopstart: 0, samples: 0, dataofs: 0 };
  const p = new IffParser(wav);
  p.iff_data = 0;
  p.iff_end = wav.length;

  // find "RIFF" chunk
  p.FindChunk('RIFF');
  if (!(p.data_p >= 0 && p.matches(p.data_p + 8, 'WAVE'))) {
    info.error = 'Missing RIFF/WAVE chunks';
    return info;
  }
  // get "fmt " chunk
  p.iff_data = p.data_p + 12;
  p.FindChunk('fmt ');
  if (p.data_p < 0) {
    info.error = 'Missing fmt chunk';
    return info;
  }
  p.data_p += 8;
  const format = p.GetLittleShort();
  if (format !== 1) {
    info.error = 'Microsoft PCM format only';
    return info;
  }
  info.channels = p.GetLittleShort();
  info.rate = p.GetLittleLong();
  p.data_p += 4 + 2;
  info.width = Math.trunc(p.GetLittleShort() / 8);

  // get cue chunk
  p.FindChunk('cue ');
  if (p.data_p >= 0) {
    p.data_p += 32;
    info.loopstart = p.GetLittleLong();
    // if the next chunk is a LIST chunk, look for a cue length marker
    p.FindNextChunk('LIST');
    if (p.data_p >= 0) {
      if (p.matches(p.data_p + 28, 'mark')) {
        p.data_p += 24;
        const i = p.GetLittleLong(); // samples in loop
        info.samples = (info.loopstart + i) | 0;
      }
    }
  } else info.loopstart = -1;

  // find data chunk
  p.FindChunk('data');
  if (p.data_p < 0) {
    info.error = 'Missing data chunk';
    return info;
  }
  p.data_p += 4;
  const len = p.GetLittleLong();
  if (info.width === 0) throw new FormatError(`Sound ${name} has a zero sample width`); // C: SIGFPE
  const samples = Math.trunc(len / info.width);
  if (info.samples) {
    if (samples < info.samples) throw new FormatError(`Sound ${name} has a bad loop length`);
  } else info.samples = samples;
  info.dataofs = p.data_p;
  return info;
}

// C: client/snd_mem.c:33 ResampleSfx
// `sc` holds length/loopstart as read from the file; it is updated in place and its data filled.
// stepscale is a float ((float)inrate / dma.speed); outcount/loopstart are float divisions truncated to int;
// the general case steps a 24.8 fixed-point source position by (int)(stepscale*256).
export function ResampleSfx(
  sc: Omit<SfxCache, 'data'> & { data?: Int8Array | Int16Array },
  inrate: number,
  inwidth: number,
  data: Uint8Array,
  dmaSpeed: number,
  loadas8bit: boolean,
): SfxCache {
  const stepscale = Math.fround(Math.fround(inrate) / dmaSpeed);
  const outcount = Math.trunc(Math.fround(sc.length / stepscale));
  sc.length = outcount;
  if (sc.loopstart !== -1) sc.loopstart = Math.trunc(Math.fround(sc.loopstart / stepscale));
  sc.speed = dmaSpeed;
  sc.width = loadas8bit ? 1 : inwidth;
  sc.stereo = 0;

  const rd = (o: number): number => {
    if (o < 0 || o >= data.length) throw new FormatError('ResampleSfx: sample data out of bounds');
    return data[o]!;
  };
  const out: Int8Array | Int16Array =
    sc.width === 2 ? new Int16Array(Math.max(0, outcount)) : new Int8Array(Math.max(0, outcount));
  if (stepscale === 1 && inwidth === 1 && sc.width === 1) {
    // fast special case
    for (let i = 0; i < outcount; i++) out[i] = rd(i) - 128;
  } else {
    // general case
    let samplefrac = 0;
    const fracstep = Math.trunc(Math.fround(stepscale * 256));
    for (let i = 0; i < outcount; i++) {
      const srcsample = samplefrac >> 8;
      samplefrac = (samplefrac + fracstep) | 0;
      let sample: number;
      if (inwidth === 2) {
        const lo = rd(srcsample * 2);
        const hi = rd(srcsample * 2 + 1);
        sample = ((lo | (hi << 8)) << 16) >> 16;
      } else sample = (rd(srcsample) - 128) << 8;
      if (sc.width === 2) out[i] = sample;
      else out[i] = sample >> 8;
    }
  }
  const res = sc as SfxCache;
  res.data = out;
  return res;
}

// C: client/snd_mem.c:95 S_LoadSound (file part: parse + resample). Returns null where C returns NULL.
export function S_LoadSound(
  name: string,
  data: ArrayBuffer | Uint8Array,
  dmaSpeed: number,
  loadas8bit = false,
): SfxCache | null {
  const bytes = toU8(data);
  const info = GetWavinfo(name, bytes);
  if (info.channels !== 1) return null; // "%s is a stereo sample" (also hit by broken files)
  const sc = {
    length: info.samples,
    loopstart: info.loopstart,
    speed: info.rate,
    width: info.width,
    stereo: info.channels,
  };
  return ResampleSfx(sc, sc.speed, sc.width, bytes.subarray(info.dataofs), dmaSpeed, loadas8bit);
}
