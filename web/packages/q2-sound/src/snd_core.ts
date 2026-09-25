// The mixing half of client/snd_dma.c plus all of client/snd_mix.c: channels, the playsound queue,
// paintedtime/soundtime, spatialization, loop sounds, raw (cinematic) samples, the paint buffer and the
// transfer into the DMA ring buffer.
//
// SoundCore is pure and deterministic (no DOM / node APIs). It runs either inside the AudioWorklet
// (worklet.ts) or in-process (createNullSound / tests). Everything the C code read from client globals
// at mix time (cl.playernum, cls.state, cl_paused, cl.sound_prepped, cls.disable_screen, entity origins,
// cl.frame entities for S_AddLoopSounds, cvars) arrives through S_Update's FrameInfo argument; the sfx
// registry stays on the main thread and only sends the loaded sample data (sfx ids = known_sfx slots).
import { ATTN_STATIC, DotProduct, VectorNormalize, cInt, fr } from 'q2-shared';

// C: snd_dma.c
export const SOUND_FULLVOLUME = 80;
export const SOUND_LOOPATTENUATE = 0.003;
export const MAX_PLAYSOUNDS = 128;
// C: snd_loc.h
export const MAX_CHANNELS = 32;
export const MAX_RAW_SAMPLES = 8192;
// C: snd_mix.c
export const PAINTBUFFER_SIZE = 2048;

/** C: snd_loc.h sfxcache_t (the sample data as produced by q2-formats ResampleSfx). */
export interface CoreSfx {
  length: number;
  loopstart: number;
  speed: number;
  width: number;
  stereo: number;
  data: Int8Array | Int16Array;
  /** sfx->name, only for s_show output */
  name: string;
}

// C: snd_loc.h channel_t
export class Channel {
  /** sfx id, -1 == NULL */
  sfx = -1;
  leftvol = 0; // 0-255 volume
  rightvol = 0; // 0-255 volume
  end = 0; // end time in global paintsamples
  pos = 0; // sample position in sfx
  looping = 0; // where to loop, -1 = no looping OBSOLETE?
  entnum = 0; // to allow overriding a specific sound
  entchannel = 0;
  readonly origin = new Float32Array(3); // only use if fixed_origin is set
  /** vec_t (float) */
  dist_mult = 0; // distance multiplier (attenuation/clipK)
  master_vol = 0; // 0-255 master volume
  fixed_origin = false; // use origin instead of fetching entnum's origin
  autosound = false; // from an entity->sound, cleared each frame

  /** memset(ch, 0, sizeof(*ch)) */
  clear(): void {
    this.sfx = -1;
    this.leftvol = this.rightvol = this.end = this.pos = this.looping = 0;
    this.entnum = this.entchannel = 0;
    this.origin.fill(0);
    this.dist_mult = 0;
    this.master_vol = 0;
    this.fixed_origin = false;
    this.autosound = false;
  }
}

// C: snd_loc.h playsound_t
export class Playsound {
  prev: Playsound = this;
  next: Playsound = this;
  sfx = -1;
  /** float */
  volume = 0;
  /** float */
  attenuation = 0;
  entnum = 0;
  entchannel = 0;
  fixed_origin = false; // use origin field instead of entnum's origin
  readonly origin = new Float32Array(3);
  /** unsigned */
  begin = 0; // begin on this sample

  clear(): void {
    this.prev = this.next = this;
    this.sfx = -1;
    this.volume = this.attenuation = 0;
    this.entnum = this.entchannel = 0;
    this.fixed_origin = false;
    this.origin.fill(0);
    this.begin = 0;
  }
}

// C: snd_loc.h dma_t
export interface Dma {
  channels: number;
  samples: number; // mono samples in buffer
  submission_chunk: number; // don't mix less than this #
  samplepos: number; // in mono samples
  samplebits: number;
  speed: number;
  /** 16 bit interleaved stereo ring buffer (dma.samples entries) */
  buffer: Int16Array;
}

/** One entity of cl.frame for S_AddLoopSounds (in frame order). */
export interface LoopEnt {
  /** ent->sound (configstring index) */
  sound: number;
  /** cl.sound_precache[sound] as an sfx id, -1 = NULL */
  sfx: number;
  origin: ArrayLike<number>;
}

/** Client state S_Update (and the functions it calls) read from C globals. */
export interface FrameInfo {
  listenerOrigin: ArrayLike<number>;
  listenerForward: ArrayLike<number>;
  listenerRight: ArrayLike<number>;
  listenerUp: ArrayLike<number>;
  /** cl.playernum */
  playernum: number;
  /** cls.state == ca_active */
  active: boolean;
  /** cl_paused->value != 0 */
  paused: boolean;
  /** cl.sound_prepped */
  soundPrepped: boolean;
  /** cls.disable_screen != 0 */
  disableScreen: boolean;
  /** s_volume->value (float) and whether it was modified since the last S_InitScaletable */
  volume: number;
  volumeModified: boolean;
  /** s_mixahead->value */
  mixahead: number;
  /** s_testsound->value */
  testsound: number;
  /** s_show->value */
  show: number;
  /** cl.frame entities for S_AddLoopSounds */
  loops: LoopEnt[];
}

export interface SoundCoreOptions {
  /** dma.speed */
  speed: number;
  /** dma.samples (mono samples, power of two). Default 0x10000 bytes of 16 bit samples like win32 DirectSound. */
  samples?: number;
  channels?: number;
  samplebits?: number;
  submission_chunk?: number;
  /** s_volume->value at S_Init (default "0.7" as float) */
  volume?: number;
  /** Com_Printf */
  print?: (msg: string) => void;
  /** Com_DPrintf */
  dprint?: (msg: string) => void;
  /** SNDDMA_GetDMAPos (mono samples, 0..dma.samples-1) */
  getDMAPos?: () => number;
  /**
   * TODO-IMPROVE flag. snd_mix.c's portable C S_PaintChannelFrom8 indexes snd_scaletable with
   * `vol >> 11` (always row 0: 8 bit sounds are silent). The shipped x86 builds (win32 and linux/i386)
   * use the id386 assembly, which indexes with `vol >> 3`. false (default) = the assembly behaviour of
   * the retail binaries; true = the portable C code as compiled by the oracle on x86-64.
   */
  portableC8bit?: boolean;
}

/** SoundCore: the mixer state of snd_dma.c / snd_mix.c. */
export class SoundCore {
  readonly channels: Channel[] = Array.from({ length: MAX_CHANNELS }, () => new Channel());
  readonly dma: Dma;
  readonly listener_origin = new Float32Array(3);
  readonly listener_forward = new Float32Array(3);
  readonly listener_right = new Float32Array(3);
  readonly listener_up = new Float32Array(3);

  soundtime = 0; // sample PAIRS
  paintedtime = 0; // sample PAIRS

  readonly s_playsounds: Playsound[] = Array.from({ length: MAX_PLAYSOUNDS }, () => new Playsound());
  readonly s_freeplays = new Playsound();
  readonly s_pendingplays = new Playsound();
  s_beginofs = 0;

  s_rawend = 0;
  /** portable_samplepair_t s_rawsamples[MAX_RAW_SAMPLES] (left,right interleaved) */
  readonly s_rawsamples = new Int32Array(MAX_RAW_SAMPLES * 2);

  // snd_mix.c
  readonly paintbuffer = new Int32Array(PAINTBUFFER_SIZE * 2);
  /** int snd_scaletable[32][256] */
  readonly snd_scaletable = new Int32Array(32 * 256);
  snd_vol = 0;

  // GetSoundtime statics
  private buffers = 0;
  private oldsamplepos = 0;

  /** loaded sfx data by id (sfx_t.cache) */
  readonly sfx = new Map<number, CoreSfx>();
  /** last known entity origins (CL_GetEntitySoundOrigin) */
  readonly entityOrigins = new Map<number, Float32Array>();

  // client state of the current S_Update call (C globals)
  playernum = 0;
  active = false;
  volume = fr(0.7);
  testsound = 0;
  show = 0;
  /** s_volume->modified */
  volumeModified = false;

  readonly print: (msg: string) => void;
  readonly dprint: (msg: string) => void;
  getDMAPos: () => number;
  readonly portableC8bit: boolean;

  private readonly tmp = new Float32Array(3);

  constructor(o: SoundCoreOptions) {
    const samplebits = o.samplebits ?? 16;
    const samples = o.samples ?? 0x10000 / (samplebits / 8);
    this.dma = {
      channels: o.channels ?? 2,
      samples,
      submission_chunk: o.submission_chunk ?? 1,
      samplepos: 0,
      samplebits,
      speed: o.speed,
      buffer: new Int16Array(samples),
    };
    this.print = o.print ?? (() => {});
    this.dprint = o.dprint ?? (() => {});
    this.getDMAPos = o.getDMAPos ?? (() => 0);
    this.portableC8bit = o.portableC8bit ?? false;
    // C: S_Init (after SNDDMA_Init)
    this.S_InitScaletable(fr(o.volume ?? 0.7));
    this.soundtime = 0;
    this.paintedtime = 0;
    this.S_StopAllSounds();
  }

  // C: snd_mem.c S_LoadSound -- in the core only the "see if still in memory" part
  S_LoadSound(id: number): CoreSfx | null {
    if (id < 0) return null;
    return this.sfx.get(id) ?? null;
  }

  // C: snd_dma.c:398 S_PickChannel
  S_PickChannel(entnum: number, entchannel: number): Channel | null {
    if (entchannel < 0) throw new Error('S_PickChannel: entchannel<0'); // C: Com_Error (ERR_DROP)

    // Check for replacement sound, or find the best one to replace
    let first_to_die = -1;
    let life_left = 0x7fffffff;
    const channels = this.channels;
    for (let ch_idx = 0; ch_idx < MAX_CHANNELS; ch_idx++) {
      const ch = channels[ch_idx]!;
      if (
        entchannel !== 0 && // channel 0 never overrides
        ch.entnum === entnum &&
        ch.entchannel === entchannel
      ) {
        // always override sound from same entity
        first_to_die = ch_idx;
        break;
      }

      // don't let monster sounds override player sounds
      if (ch.entnum === this.playernum + 1 && entnum !== this.playernum + 1 && ch.sfx >= 0) continue;

      if (((ch.end - this.paintedtime) | 0) < life_left) {
        life_left = (ch.end - this.paintedtime) | 0;
        first_to_die = ch_idx;
      }
    }

    if (first_to_die === -1) return null;

    const ch = channels[first_to_die]!;
    ch.clear();
    return ch;
  }

  // C: snd_dma.c:447 S_SpatializeOrigin -- used for spatializing channels and autosounds.
  // Returns [left_vol, right_vol].
  S_SpatializeOrigin(origin: ArrayLike<number>, master_vol: number, dist_mult: number): [number, number] {
    if (!this.active) return [255, 255];

    // calculate stereo seperation and distance attenuation
    const source_vec = this.tmp;
    source_vec[0] = origin[0]! - this.listener_origin[0]!;
    source_vec[1] = origin[1]! - this.listener_origin[1]!;
    source_vec[2] = origin[2]! - this.listener_origin[2]!;

    let dist = VectorNormalize(source_vec);
    dist = fr(dist - SOUND_FULLVOLUME);
    if (dist < 0) dist = 0; // close enough to be at full volume
    dist = fr(dist * dist_mult); // different attenuation levels

    const dot = DotProduct(this.listener_right, source_vec);

    let rscale: number, lscale: number;
    if (this.dma.channels === 1 || !dist_mult) {
      // no attenuation = no spatialization
      rscale = 1.0;
      lscale = 1.0;
    } else {
      rscale = fr(0.5 * (1.0 + dot));
      lscale = fr(0.5 * (1.0 - dot));
    }

    // add in distance effect
    let scale = fr((1.0 - dist) * rscale);
    let right_vol = cInt(fr(master_vol * scale));
    if (right_vol < 0) right_vol = 0;

    scale = fr((1.0 - dist) * lscale);
    let left_vol = cInt(fr(master_vol * scale));
    if (left_vol < 0) left_vol = 0;
    return [left_vol, right_vol];
  }

  /** C: cl_ents.c CL_GetEntitySoundOrigin as forwarded by the main thread */
  private CL_GetEntitySoundOrigin(ent: number, org: Float32Array): void {
    const o = this.entityOrigins.get(ent);
    if (o) org.set(o);
    else org.fill(0);
  }

  private readonly spatOrigin = new Float32Array(3);

  // C: snd_dma.c:500 S_Spatialize
  S_Spatialize(ch: Channel): void {
    // anything coming from the view entity will always be full volume
    if (ch.entnum === this.playernum + 1) {
      ch.leftvol = ch.master_vol;
      ch.rightvol = ch.master_vol;
      return;
    }

    const origin = this.spatOrigin;
    if (ch.fixed_origin) origin.set(ch.origin);
    else this.CL_GetEntitySoundOrigin(ch.entnum, origin);

    const [l, r] = this.S_SpatializeOrigin(origin, ch.master_vol, ch.dist_mult);
    ch.leftvol = l;
    ch.rightvol = r;
  }

  // C: snd_dma.c:527 S_AllocPlaysound
  S_AllocPlaysound(): Playsound | null {
    const ps = this.s_freeplays.next;
    if (ps === this.s_freeplays) return null; // no free playsounds

    // unlink from freelist
    ps.prev.next = ps.next;
    ps.next.prev = ps.prev;
    return ps;
  }

  // C: snd_dma.c:547 S_FreePlaysound
  S_FreePlaysound(ps: Playsound): void {
    // unlink from channel
    ps.prev.next = ps.next;
    ps.next.prev = ps.prev;

    // add to free list
    ps.next = this.s_freeplays.next;
    this.s_freeplays.next.prev = ps;
    ps.prev = this.s_freeplays;
    this.s_freeplays.next = ps;
  }

  // C: snd_dma.c:571 S_IssuePlaysound -- take the next playsound and begin it on the channel
  S_IssuePlaysound(ps: Playsound): void {
    if (this.show) this.print(`Issue ${ps.begin}\n`);
    // pick a channel to play on
    const ch = this.S_PickChannel(ps.entnum, ps.entchannel);
    if (!ch) {
      this.S_FreePlaysound(ps);
      return;
    }

    // spatialize
    if (ps.attenuation === ATTN_STATIC) ch.dist_mult = fr(ps.attenuation * 0.001);
    else ch.dist_mult = fr(ps.attenuation * 0.0005);
    ch.master_vol = cInt(ps.volume);
    ch.entnum = ps.entnum;
    ch.entchannel = ps.entchannel;
    ch.sfx = ps.sfx;
    ch.origin.set(ps.origin);
    ch.fixed_origin = ps.fixed_origin;

    this.S_Spatialize(ch);

    ch.pos = 0;
    const sc = this.S_LoadSound(ch.sfx);
    // C dereferences sc unconditionally; it can only be NULL here if the sfx was freed after the start
    ch.end = (this.paintedtime + (sc ? sc.length : 0)) | 0;

    // free the playsound
    this.S_FreePlaysound(ps);
  }

  /**
   * C: snd_dma.c:668 S_StartSound, from "make sure the sound is loaded" on. The sexed-sound lookup and
   * loading happen on the main thread; `servertime` is cl.frame.servertime at the time of the call.
   * `fvol`, `attenuation`, `timeofs` are the float arguments.
   */
  S_StartSound(
    origin: ArrayLike<number> | null,
    entnum: number,
    entchannel: number,
    sfx: number,
    fvol: number,
    attenuation: number,
    timeofs: number,
    servertime: number,
  ): void {
    // make sure the sound is loaded
    const sc = this.S_LoadSound(sfx);
    if (!sc) return; // couldn't load the sound's data

    const vol = cInt(fr(fvol * 255));

    // make the playsound_t
    const ps = this.S_AllocPlaysound();
    if (!ps) return;

    if (origin) {
      ps.origin[0] = origin[0]!;
      ps.origin[1] = origin[1]!;
      ps.origin[2] = origin[2]!;
      ps.fixed_origin = true;
    } else ps.fixed_origin = false;

    ps.entnum = entnum;
    ps.entchannel = entchannel;
    ps.attenuation = fr(attenuation);
    ps.volume = fr(vol);
    ps.sfx = sfx;

    const speed = this.dma.speed;
    const paintedtime = this.paintedtime;
    // drift s_beginofs
    let start = cInt(servertime * 0.001 * speed + this.s_beginofs);
    if (start < paintedtime) {
      start = paintedtime;
      this.s_beginofs = cInt(start - servertime * 0.001 * speed);
    } else if (start > paintedtime + 0.3 * speed) {
      start = cInt(paintedtime + 0.1 * speed);
      this.s_beginofs = cInt(start - servertime * 0.001 * speed);
    } else {
      this.s_beginofs = (this.s_beginofs - 10) | 0;
    }

    if (!timeofs) ps.begin = paintedtime >>> 0;
    else ps.begin = cUnsigned(fr(fr(start) + fr(fr(timeofs) * fr(speed))));

    // sort into the pending sound list
    let sort = this.s_pendingplays.next;
    while (sort !== this.s_pendingplays && sort.begin < ps.begin) sort = sort.next;

    ps.next = sort;
    ps.prev = sort.prev;

    ps.next.prev = ps;
    ps.prev.next = ps;
  }

  // C: snd_dma.c:765 S_ClearBuffer
  S_ClearBuffer(): void {
    this.s_rawend = 0;
    // SNDDMA_BeginPainting / memset (16 bit: clear = 0) / SNDDMA_Submit
    this.dma.buffer.fill(0);
  }

  // C: snd_dma.c:790 S_StopAllSounds
  S_StopAllSounds(): void {
    // clear all the playsounds
    for (const ps of this.s_playsounds) ps.clear();
    const free = this.s_freeplays;
    const pend = this.s_pendingplays;
    free.next = free.prev = free;
    pend.next = pend.prev = pend;

    for (let i = 0; i < MAX_PLAYSOUNDS; i++) {
      const ps = this.s_playsounds[i]!;
      ps.prev = free;
      ps.next = free.next;
      ps.prev.next = ps;
      ps.next.prev = ps;
    }

    // clear all the channels
    for (const ch of this.channels) ch.clear();

    this.S_ClearBuffer();
  }

  // C: snd_dma.c:827 S_AddLoopSounds -- entities with a ->sound field will generated looped sounds
  S_AddLoopSounds(f: FrameInfo): void {
    if (f.paused) return;
    if (!f.active) return;
    if (!f.soundPrepped) return;

    const loops = f.loops;
    const n = loops.length;
    const sounds = new Int32Array(n);
    for (let i = 0; i < n; i++) sounds[i] = loops[i]!.sound;

    for (let i = 0; i < n; i++) {
      if (!sounds[i]) continue;

      const sfx = loops[i]!.sfx;
      if (sfx < 0) continue; // bad sound effect
      const sc = this.sfx.get(sfx);
      if (!sc) continue;

      // find the total contribution of all sounds of this type
      let [left_total, right_total] = this.S_SpatializeOrigin(
        loops[i]!.origin,
        255.0,
        fr(SOUND_LOOPATTENUATE),
      );
      for (let j = i + 1; j < n; j++) {
        if (sounds[j] !== sounds[i]) continue;
        sounds[j] = 0; // don't check this again later

        const [left, right] = this.S_SpatializeOrigin(loops[j]!.origin, 255.0, fr(SOUND_LOOPATTENUATE));
        left_total = (left_total + left) | 0;
        right_total = (right_total + right) | 0;
      }

      if (left_total === 0 && right_total === 0) continue; // not audible

      // allocate a channel
      const ch = this.S_PickChannel(0, 0);
      if (!ch) return;

      if (left_total > 255) left_total = 255;
      if (right_total > 255) right_total = 255;
      ch.leftvol = left_total;
      ch.rightvol = right_total;
      ch.autosound = true; // remove next frame
      ch.sfx = sfx;
      ch.pos = cMod(this.paintedtime, sc.length);
      ch.end = (this.paintedtime + sc.length - ch.pos) | 0;
    }
  }

  // C: snd_dma.c:916 S_RawSamples -- cinematic streaming and voice over network
  S_RawSamples(samples: number, rate: number, width: number, channels: number, data: Uint8Array): void {
    if (this.s_rawend < this.paintedtime) this.s_rawend = this.paintedtime;
    const scale = fr(fr(rate) / this.dma.speed);
    const raw = this.s_rawsamples;
    const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
    const short = (i: number): number => (2 * i + 1 < data.length ? dv.getInt16(i * 2, true) : 0);
    const sbyte = (i: number): number => (i < data.length ? (data[i]! << 24) >> 24 : 0);
    const ubyte = (i: number): number => (i < data.length ? data[i]! : 0);
    const put = (l: number, r: number): void => {
      const dst = this.s_rawend & (MAX_RAW_SAMPLES - 1);
      this.s_rawend = (this.s_rawend + 1) | 0;
      raw[dst * 2] = l;
      raw[dst * 2 + 1] = r;
    };

    if (channels === 2 && width === 2) {
      if (scale === 1.0) {
        // optimized case
        for (let i = 0; i < samples; i++) put(short(i * 2) << 8, short(i * 2 + 1) << 8);
      } else {
        for (let i = 0; ; i++) {
          const src = cInt(fr(i * scale));
          if (src >= samples) break;
          put(short(src * 2) << 8, short(src * 2 + 1) << 8);
        }
      }
    } else if (channels === 1 && width === 2) {
      for (let i = 0; ; i++) {
        const src = cInt(fr(i * scale));
        if (src >= samples) break;
        put(short(src) << 8, short(src) << 8);
      }
    } else if (channels === 2 && width === 1) {
      for (let i = 0; ; i++) {
        const src = cInt(fr(i * scale));
        if (src >= samples) break;
        put(sbyte(src * 2) << 16, sbyte(src * 2 + 1) << 16);
      }
    } else if (channels === 1 && width === 1) {
      for (let i = 0; ; i++) {
        const src = cInt(fr(i * scale));
        if (src >= samples) break;
        put((ubyte(src) - 128) << 16, (ubyte(src) - 128) << 16);
      }
    }
  }

  // C: snd_dma.c:1016 S_Update -- called once each time through the main loop
  S_Update(f: FrameInfo): void {
    this.playernum = f.playernum;
    this.active = f.active;
    this.volume = f.volume;
    this.testsound = f.testsound;
    this.show = f.show;
    // s_volume->modified stays set until S_InitScaletable runs (it is skipped while disable_screen)
    if (f.volumeModified) this.volumeModified = true;

    // if the laoding plaque is up, clear everything out to make sure we aren't looping a dirty dma
    // buffer while loading
    if (f.disableScreen) {
      this.S_ClearBuffer();
      return;
    }

    // rebuild scale tables if volume is modified
    if (this.volumeModified) this.S_InitScaletable(f.volume);

    copy3(this.listener_origin, f.listenerOrigin);
    copy3(this.listener_forward, f.listenerForward);
    copy3(this.listener_right, f.listenerRight);
    copy3(this.listener_up, f.listenerUp);

    // update spatialization for dynamic sounds
    for (const ch of this.channels) {
      if (ch.sfx < 0) continue;
      if (ch.autosound) {
        // autosounds are regenerated fresh each frame
        ch.clear();
        continue;
      }
      this.S_Spatialize(ch); // respatialize channel
      if (!ch.leftvol && !ch.rightvol) {
        ch.clear();
        continue;
      }
    }

    // add loopsounds
    this.S_AddLoopSounds(f);

    // debugging output
    if (f.show) {
      let total = 0;
      for (const ch of this.channels) {
        if (ch.sfx >= 0 && (ch.leftvol || ch.rightvol)) {
          const name = this.sfx.get(ch.sfx)?.name ?? '';
          this.print(`${pad3(ch.leftvol)} ${pad3(ch.rightvol)} ${name}\n`);
          total++;
        }
      }
      this.print(`----(${total})---- painted: ${this.paintedtime}\n`);
    }

    // mix some sound
    this.S_Update_(f.mixahead);
  }

  // C: snd_dma.c:1102 GetSoundtime
  GetSoundtime(): void {
    const dma = this.dma;
    const fullsamples = (dma.samples / dma.channels) | 0;

    // it is possible to miscount buffers if it has wrapped twice between calls to S_Update.  Oh well.
    const samplepos = this.getDMAPos();
    dma.samplepos = samplepos;

    if (samplepos < this.oldsamplepos) {
      this.buffers++; // buffer wrapped

      if (this.paintedtime > 0x40000000) {
        // time to chop things off to avoid 32 bit limits
        this.buffers = 0;
        this.paintedtime = fullsamples;
        this.S_StopAllSounds();
      }
    }
    this.oldsamplepos = samplepos;

    this.soundtime = (Math.imul(this.buffers, fullsamples) + ((samplepos / dma.channels) | 0)) | 0;
  }

  // C: snd_dma.c:1131 S_Update_
  S_Update_(mixahead: number): void {
    const dma = this.dma;
    // Updates DMA time
    this.GetSoundtime();

    // check to make sure that we haven't overshot
    if (this.paintedtime < this.soundtime) {
      this.dprint('S_Update_ : overflow\n');
      this.paintedtime = this.soundtime;
    }

    // mix ahead of current position
    let endtime = cUnsigned(fr(fr(this.soundtime) + fr(fr(mixahead) * fr(dma.speed))));

    // mix to an even submission block size
    endtime = ((endtime + dma.submission_chunk - 1) & ~(dma.submission_chunk - 1)) >>> 0;
    const samps = dma.samples >> (dma.channels - 1);
    if ((endtime - this.soundtime) >>> 0 > samps >>> 0) endtime = (this.soundtime + samps) >>> 0;

    this.S_PaintChannels(endtime | 0);
  }

  // ---------------------------------------------------------------------------------------------
  // snd_mix.c

  // C: snd_mix.c:35 S_WriteLinearBlastStereo16 / :100 S_TransferStereo16
  private S_TransferStereo16(endtime: number): void {
    const dma = this.dma;
    const out = dma.buffer;
    const pb = this.paintbuffer;
    let snd_p = 0;
    let lpaintedtime = this.paintedtime;

    while (lpaintedtime < endtime) {
      // handle recirculating buffer issues
      const lpos = lpaintedtime & ((dma.samples >> 1) - 1);
      const snd_out = lpos << 1;

      let snd_linear_count = (dma.samples >> 1) - lpos;
      if (lpaintedtime + snd_linear_count > endtime) snd_linear_count = endtime - lpaintedtime;

      snd_linear_count <<= 1;

      // write a linear blast of samples
      for (let i = 0; i < snd_linear_count; i += 2) {
        out[snd_out + i] = clamp16(pb[snd_p + i]! >> 8);
        out[snd_out + i + 1] = clamp16(pb[snd_p + i + 1]! >> 8);
      }

      snd_p += snd_linear_count;
      lpaintedtime += snd_linear_count >> 1;
    }
  }

  // C: snd_mix.c:132 S_TransferPaintBuffer
  S_TransferPaintBuffer(endtime: number): void {
    const dma = this.dma;
    const pb = this.paintbuffer;

    if (this.testsound) {
      // write a fixed sine wave
      const count = endtime - this.paintedtime;
      for (let i = 0; i < count; i++)
        pb[i * 2] = pb[i * 2 + 1] = cInt(Math.sin((this.paintedtime + i) * 0.1) * 20000 * 256);
    }

    if (dma.samplebits === 16 && dma.channels === 2) {
      // optimized case
      this.S_TransferStereo16(endtime);
    } else {
      // general case (only 16 bit output buffers are supported by this port)
      let p = 0;
      let count = (endtime - this.paintedtime) * dma.channels;
      const out_mask = dma.samples - 1;
      let out_idx = (this.paintedtime * dma.channels) & out_mask;
      const step = 3 - dma.channels;
      const out = dma.buffer;
      while (count--) {
        const val = clamp16(pb[p]! >> 8);
        p += step;
        out[out_idx] = val;
        out_idx = (out_idx + 1) & out_mask;
      }
    }
  }

  // C: snd_mix.c:222 S_PaintChannels
  S_PaintChannels(endtime: number): void {
    const pb = this.paintbuffer;
    this.snd_vol = cInt(fr(this.volume * 256));

    while (this.paintedtime < endtime) {
      const paintedtime = this.paintedtime;
      // if paintbuffer is smaller than DMA buffer
      let end = endtime;
      if (endtime - paintedtime > PAINTBUFFER_SIZE) end = paintedtime + PAINTBUFFER_SIZE;

      // start any playsounds
      while (1) {
        const ps = this.s_pendingplays.next;
        if (ps === this.s_pendingplays) break; // no more pending sounds
        if (ps.begin <= paintedtime >>> 0) {
          this.S_IssuePlaysound(ps);
          continue;
        }

        if (ps.begin < end >>> 0) end = ps.begin | 0; // stop here
        break;
      }

      // clear the paint buffer
      if (this.s_rawend < paintedtime) {
        pb.fill(0, 0, (end - paintedtime) * 2);
      } else {
        // copy from the streaming sound source
        const stop = end < this.s_rawend ? end : this.s_rawend;
        let i: number;
        for (i = paintedtime; i < stop; i++) {
          const s = i & (MAX_RAW_SAMPLES - 1);
          pb[(i - paintedtime) * 2] = this.s_rawsamples[s * 2]!;
          pb[(i - paintedtime) * 2 + 1] = this.s_rawsamples[s * 2 + 1]!;
        }
        for (; i < end; i++) {
          pb[(i - paintedtime) * 2] = pb[(i - paintedtime) * 2 + 1] = 0;
        }
      }

      // paint in the channels.
      for (const ch of this.channels) {
        let ltime = paintedtime;

        while (ltime < end) {
          if (ch.sfx < 0 || (!ch.leftvol && !ch.rightvol)) break;

          // max painting is to the end of the buffer
          let count = end - ltime;

          // might be stopped by running out of data
          if (ch.end - ltime < count) count = ch.end - ltime;

          const sc = this.S_LoadSound(ch.sfx);
          if (!sc) break;

          if (count > 0 && ch.sfx >= 0) {
            if (sc.width === 1) this.S_PaintChannelFrom8(ch, sc, count, ltime - paintedtime);
            else this.S_PaintChannelFrom16(ch, sc, count, ltime - paintedtime);

            ltime += count;
          }

          // if at end of loop, restart
          if (ltime >= ch.end) {
            if (ch.autosound) {
              // autolooping sounds always go back to start
              ch.pos = 0;
              ch.end = ltime + sc.length;
            } else if (sc.loopstart >= 0) {
              ch.pos = sc.loopstart;
              ch.end = ltime + sc.length - ch.pos;
            } else {
              // channel just stopped
              ch.sfx = -1;
            }
          }
        }
      }

      // transfer out according to DMA format
      this.S_TransferPaintBuffer(end);
      this.paintedtime = end;
    }
  }

  // C: snd_mix.c:349 S_InitScaletable
  S_InitScaletable(volume: number): void {
    this.volumeModified = false;
    this.volume = volume;
    for (let i = 0; i < 32; i++) {
      const scale = cInt(fr(i * 8 * 256 * volume));
      for (let j = 0; j < 256; j++) this.snd_scaletable[i * 256 + j] = Math.imul((j << 24) >> 24, scale);
    }
  }

  // C: snd_mix.c:366 S_PaintChannelFrom8
  S_PaintChannelFrom8(ch: Channel, sc: CoreSfx, count: number, offset: number): void {
    if (ch.leftvol > 255) ch.leftvol = 255;
    if (ch.rightvol > 255) ch.rightvol = 255;

    const shift = this.portableC8bit ? 11 : 3;
    const lscale = (ch.leftvol >> shift) * 256;
    const rscale = (ch.rightvol >> shift) * 256;
    const sfx = sc.data;
    const pos = ch.pos;
    const table = this.snd_scaletable;
    const pb = this.paintbuffer;

    for (let i = 0; i < count; i++) {
      const data = (sfx[pos + i] ?? 0) & 255; // unsigned char
      const o = (offset + i) * 2;
      pb[o] = (pb[o]! + table[lscale + data]!) | 0;
      pb[o + 1] = (pb[o + 1]! + table[rscale + data]!) | 0;
    }

    ch.pos += count;
  }

  // C: snd_mix.c:462 S_PaintChannelFrom16
  S_PaintChannelFrom16(ch: Channel, sc: CoreSfx, count: number, offset: number): void {
    const leftvol = Math.imul(ch.leftvol, this.snd_vol);
    const rightvol = Math.imul(ch.rightvol, this.snd_vol);
    const sfx = sc.data;
    const pos = ch.pos;
    const pb = this.paintbuffer;

    for (let i = 0; i < count; i++) {
      const data = sfx[pos + i] ?? 0;
      const o = (offset + i) * 2;
      pb[o] = (pb[o]! + (Math.imul(data, leftvol) >> 8)) | 0;
      pb[o + 1] = (pb[o + 1]! + (Math.imul(data, rightvol) >> 8)) | 0;
    }

    ch.pos += count;
  }
}

function copy3(dst: Float32Array, src: ArrayLike<number>): void {
  dst[0] = src[0]!;
  dst[1] = src[1]!;
  dst[2] = src[2]!;
}

function clamp16(val: number): number {
  if (val > 0x7fff) return 0x7fff;
  if (val < -0x8000) return -0x8000;
  return val;
}

/** C conversion of a float to `unsigned` (x86-64: via a 64 bit signed conversion, then truncated). */
function cUnsigned(x: number): number {
  if (!(x > -9.2e18 && x < 9.2e18)) return 0;
  const t = Math.trunc(x);
  return Number(BigInt.asUintN(32, BigInt(t)));
}

/** C `%` on ints (truncating); x % 0 is a SIGFPE in C, 0 here. */
function cMod(a: number, b: number): number {
  if (!b) return 0;
  return (a % b) | 0;
}

function pad3(n: number): string {
  const s = String(n);
  return s.length >= 3 ? s : ' '.repeat(3 - s.length) + s;
}
