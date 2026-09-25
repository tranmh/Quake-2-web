// Main-thread half of client/snd_dma.c (+ S_LoadSound of snd_mem.c): the sfx registry, sexed sounds,
// sample loading, cvars and console commands, and the S_* entry points of the q2-client Sound
// interface. The mixer state (channels, playsounds, paintedtime ...) lives in SoundCore, reached through a
// SoundBackend (AudioWorklet in the browser, in-process for node). Calls are forwarded as ordered
// messages; see host.ts for the ordering guarantee.
import {
  CS_PLAYERSKINS,
  MAX_EDICTS,
  MAX_PARSE_ENTITIES,
  MAX_QPATH,
  MAX_SOUNDS,
  CVAR_ARCHIVE,
} from 'q2-shared';
import { GetWavinfo, ResampleSfx } from 'q2-formats';
import {
  CL_GetEntitySoundOrigin,
  Com_DPrintf,
  Com_Printf,
  ca_active,
  type ClientContext,
  type Cvar,
  type SfxHandle,
  type Sound,
} from 'q2-client';
import type { CoreSfx, LoopEnt } from './snd_core';
import { BatchWriter, writeOrigins, writeRaw, writeStart, writeStopAll, writeUpdate } from './protocol';

// C: snd_dma.c
export const MAX_SFX = MAX_SOUNDS * 2;

/** The DMA device as seen from the main thread (SNDDMA_*). */
export interface SoundBackend {
  /** SNDDMA_Init at dma.speed; false = no sound device */
  init(speed: number): boolean;
  /** SNDDMA_Shutdown */
  shutdown(): void;
  /** Sample data for sfx slot `id` (null: slot freed). `seq` orders it against batches. */
  sfx(seq: number, id: number, sfx: CoreSfx | null): void;
  /** A command batch (see protocol.ts). */
  batch(seq: number, batch: Uint8Array): void;
  /** Com_Printf / Com_DPrintf for messages printed by the mixer (s_show, overflow); set by attach */
  print?: ((msg: string, developer: boolean) => void) | null;
  /** dma parameters for soundinfo */
  readonly info?: { channels: number; samples: number; samplebits: number; submission_chunk: number };
}

/** C: snd_loc.h sfx_t. The object identity of a slot never changes (C pointers into known_sfx). */
export class Sfx {
  name = '';
  registration_sequence = 0;
  cache: CoreSfx | null = null;
  truename: string | null = null;
  // port bookkeeping
  /** bumped on every memset: pending loads of an older generation are dropped */
  gen = 0;
  loading: Promise<CoreSfx | null> | null = null;
  /** the last load failed (C retries on every S_LoadSound; this port retries at the next registration) */
  failed = false;
  /** sample data was sent to the backend */
  uploaded = false;
  constructor(readonly index: number) {}
}

const asSfx = (h: SfxHandle | null): Sfx | null => h as unknown as Sfx | null;
const asHandle = (s: Sfx | null): SfxHandle | null => s as unknown as SfxHandle | null;

/** C: snd_win.c SNDDMA_InitDirect / SNDDMA_InitWav: s_khz 44 / 22 / anything else */
export function speedFromKhz(khz: number): number {
  if (khz === 44) return 44100;
  if (khz === 22) return 22050;
  return 11025;
}

export interface MainSoundOptions {
  backend: SoundBackend;
}

export class MainSound implements Sound {
  private c!: ClientContext;
  readonly backend: SoundBackend;

  s_registration_sequence = 0;
  sound_started = 0;
  s_registering = false;
  /** dma.speed */
  speed = 11025;
  readonly known_sfx: Sfx[] = Array.from({ length: MAX_SFX }, (_, i) => new Sfx(i));
  num_sfx = 0;

  s_volume: Cvar | null = null;
  s_testsound: Cvar | null = null;
  s_loadas8bit: Cvar | null = null;
  s_khz: Cvar | null = null;
  s_show: Cvar | null = null;
  s_mixahead: Cvar | null = null;
  s_primary: Cvar | null = null;

  private readonly w = new BatchWriter();
  private seq = 0;
  /** bumped by S_StopAllSounds: deferred starts issued before it are dropped */
  private stopGen = 0;
  /** entities that started dynamically sourced sounds (their origins are forwarded every frame) */
  private readonly dynEnts = new Set<number>();
  /** FS_FOpenFile existence results for sexed sounds */
  private readonly exists = new Map<string, boolean | Promise<boolean>>();
  private readonly tmpOrigin = new Float32Array(3);

  constructor(o: MainSoundOptions) {
    this.backend = o.backend;
  }

  attach(c: ClientContext): void {
    this.c = c;
    this.backend.print = (msg, developer) =>
      developer ? Com_DPrintf(c, '%s', msg) : Com_Printf(c, '%s', msg);
  }

  private nextSeq(): number {
    return ++this.seq;
  }

  /** Sends the pending commands (called before sfx uploads and at S_Update). */
  private flush(): void {
    if (!this.w.length) return;
    this.backend.batch(this.nextSeq(), this.w.take());
  }

  private upload(s: Sfx, data: CoreSfx | null): void {
    this.flush();
    this.backend.sfx(this.nextSeq(), s.index, data);
    s.uploaded = !!data;
  }

  /** memset(sfx, 0, sizeof(*sfx)) (+ free the sample data in the mixer) */
  private clearSfx(s: Sfx, notify: boolean): void {
    if (notify && s.uploaded) this.upload(s, null);
    s.name = '';
    s.registration_sequence = 0;
    s.cache = null;
    s.truename = null;
    s.gen++;
    s.loading = null;
    s.failed = false;
    s.uploaded = false;
  }

  // C: snd_dma.c:112 S_Init
  init(): void {
    const c = this.c;
    Com_Printf(c, '\n------- sound initialization -------\n');

    const cv = c.cvars.get('s_initsound', '1', 0);
    if (!cv.value) Com_Printf(c, 'not initializing.\n');
    else {
      this.s_volume = c.cvars.get('s_volume', '0.7', CVAR_ARCHIVE);
      this.s_khz = c.cvars.get('s_khz', '11', CVAR_ARCHIVE);
      this.s_loadas8bit = c.cvars.get('s_loadas8bit', '1', CVAR_ARCHIVE);
      this.s_mixahead = c.cvars.get('s_mixahead', '0.2', CVAR_ARCHIVE);
      this.s_show = c.cvars.get('s_show', '0', 0);
      this.s_testsound = c.cvars.get('s_testsound', '0', 0);
      this.s_primary = c.cvars.get('s_primary', '0', CVAR_ARCHIVE); // win32 specific

      c.cmd.addCommand('play', () => this.S_Play());
      c.cmd.addCommand('stopsound', () => this.stopAllSounds());
      c.cmd.addCommand('soundlist', () => this.S_SoundList());
      c.cmd.addCommand('soundinfo', () => this.S_SoundInfo_f());

      this.speed = speedFromKhz(this.s_khz.value);
      this.w.take();
      this.seq = 0;
      if (!this.backend.init(this.speed)) return;

      // S_InitScaletable runs in the core (it gets s_volume->modified with the first S_Update)
      this.sound_started = 1;
      this.num_sfx = 0;

      Com_Printf(c, 'sound sampling rate: %i\n', this.speed);

      this.stopAllSounds();
    }

    Com_Printf(c, '------------------------------------\n');
  }

  // C: snd_dma.c:163 S_Shutdown
  shutdown(): void {
    if (!this.sound_started) return;
    const c = this.c;

    this.backend.shutdown();
    this.sound_started = 0;

    c.cmd.removeCommand('play');
    c.cmd.removeCommand('stopsound');
    c.cmd.removeCommand('soundlist');
    c.cmd.removeCommand('soundinfo');

    // free all sounds
    for (let i = 0; i < this.num_sfx; i++) {
      const sfx = this.known_sfx[i]!;
      if (!sfx.name) continue;
      this.clearSfx(sfx, false);
    }
    this.num_sfx = 0;
    this.dynEnts.clear();
    this.w.take();
  }

  // C: snd_dma.c:204 S_FindName
  S_FindName(name: string, create: boolean): Sfx | null {
    if (!name) throw new Error('S_FindName: empty name\n'); // C: Com_Error (ERR_FATAL)
    if (name.length >= MAX_QPATH) throw new Error(`Sound name too long: ${name}`);

    // see if already loaded
    for (let i = 0; i < this.num_sfx; i++) if (this.known_sfx[i]!.name === name) return this.known_sfx[i]!;

    if (!create) return null;

    return this.allocSfx(name);
  }

  private allocSfx(name: string): Sfx {
    // find a free sfx
    let i: number;
    for (i = 0; i < this.num_sfx; i++) if (!this.known_sfx[i]!.name) break;

    if (i === this.num_sfx) {
      if (this.num_sfx === MAX_SFX) throw new Error('S_FindName: out of sfx_t');
      this.num_sfx++;
    }

    const sfx = this.known_sfx[i]!;
    this.clearSfx(sfx, true);
    sfx.name = name;
    sfx.registration_sequence = this.s_registration_sequence;
    return sfx;
  }

  // C: snd_dma.c:253 S_AliasName
  S_AliasName(aliasname: string, truename: string): Sfx {
    const sfx = this.allocSfx(aliasname);
    sfx.truename = truename;
    return sfx;
  }

  // C: snd_dma.c:287 S_BeginRegistration
  beginRegistration(): void {
    this.s_registration_sequence++;
    this.s_registering = true;
  }

  // C: snd_dma.c:298 S_RegisterSound
  registerSound(name: string): SfxHandle | null {
    return asHandle(this.S_RegisterSound(name));
  }

  S_RegisterSound(name: string): Sfx | null {
    if (!this.sound_started) return null;

    const sfx = this.S_FindName(name, true)!;
    sfx.registration_sequence = this.s_registration_sequence;

    if (!this.s_registering) void this.S_LoadSound(sfx);

    return sfx;
  }

  // C: snd_dma.c:321 S_EndRegistration
  async endRegistration(): Promise<void> {
    // free any sounds not from this registration sequence
    for (let i = 0; i < this.num_sfx; i++) {
      const sfx = this.known_sfx[i]!;
      if (!sfx.name) continue;
      if (sfx.registration_sequence !== this.s_registration_sequence) {
        // don't need this sound
        this.clearSfx(sfx, true);
      }
    }

    // load everything in
    const loads: Promise<unknown>[] = [];
    for (let i = 0; i < this.num_sfx; i++) {
      const sfx = this.known_sfx[i]!;
      if (!sfx.name) continue;
      sfx.failed = false;
      const r = this.S_LoadSound(sfx);
      if (r instanceof Promise) loads.push(r);
    }
    await Promise.all(loads);

    this.s_registering = false;
  }

  /**
   * C: snd_mem.c:95 S_LoadSound. FS_LoadFile is asynchronous here: returns the cache when resident,
   * null when the sound cannot be loaded, or a promise while the file is being fetched.
   */
  S_LoadSound(s: Sfx): CoreSfx | null | Promise<CoreSfx | null> {
    if (s.name[0] === '*') return null;

    // see if still in memory
    if (s.cache) return s.cache;
    if (s.failed) return null;
    if (s.loading) return s.loading;

    // load it in
    const name = s.truename ?? s.name;
    const namebuffer = name[0] === '#' ? name.slice(1) : truncQPath(`sound/${name}`);
    const gen = s.gen;
    const c = this.c;
    const p = (async (): Promise<CoreSfx | null> => {
      let data: Uint8Array | null = null;
      try {
        data = await c.loadFile(namebuffer);
      } catch {
        data = null;
      }
      if (s.gen !== gen) return null; // slot was freed / reused meanwhile
      s.loading = null;
      if (!data) {
        Com_DPrintf(c, "Couldn't load %s\n", namebuffer);
        s.failed = true;
        return null;
      }
      let sc: CoreSfx | null = null;
      try {
        const info = GetWavinfo(s.name, data);
        if (info.error) Com_Printf(c, '%s\n', info.error);
        if (info.channels !== 1) {
          Com_Printf(c, '%s is a stereo sample\n', s.name);
          s.failed = true;
          return null;
        }
        const r = ResampleSfx(
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
          this.speed,
          !!this.s_loadas8bit?.value,
        );
        sc = { ...r, name: s.name };
      } catch (e) {
        // C: Com_Error (ERR_DROP) for a bad loop length; cannot unwind the frame from here
        Com_Printf(c, '%s\n', e instanceof Error ? e.message : String(e));
        s.failed = true;
        return null;
      }
      s.cache = sc;
      if (this.sound_started) this.upload(s, sc);
      return sc;
    })();
    s.loading = p;
    return p;
  }

  // C: snd_dma.c:600 S_RegisterSexedSound. The existence test (FS_FOpenFile) is asynchronous: a promise
  // is returned the first time a model/sound pair is looked up.
  S_RegisterSexedSound(entnumber: number, base: string): Sfx | null | Promise<Sfx | null> {
    const c = this.c;
    // determine what model the client is using
    let model = '';
    const n = CS_PLAYERSKINS + entnumber - 1;
    const cs = c.cl.configstrings[n] ?? '';
    if (cs) {
      const p = cs.indexOf('\\');
      if (p >= 0) {
        model = cs.slice(p + 1);
        const q = model.indexOf('/');
        if (q >= 0) model = model.slice(0, q);
      }
    }
    // if we can't figure it out, they're male
    if (!model) model = 'male';

    // see if we already know of the model specific sound
    const sexedFilename = truncQPath(`#players/${model}/${base.slice(1)}`);
    const finish = (exists: boolean): Sfx | null => {
      let sfx = this.S_FindName(sexedFilename, false);
      if (sfx) return sfx;
      if (exists) {
        // yes, close the file and register it
        sfx = this.S_RegisterSound(sexedFilename);
      } else {
        // no, revert to the male sound in the pak0.pak
        const maleFilename = truncQPath(`player/${'male'}/${base.slice(1)}`);
        sfx = this.S_AliasName(sexedFilename, maleFilename);
      }
      return sfx;
    };
    if (this.S_FindName(sexedFilename, false)) return this.S_FindName(sexedFilename, false);

    // no, so see if it exists
    const path = sexedFilename.slice(1);
    const known = this.exists.get(path);
    if (typeof known === 'boolean') return finish(known);
    let pr = known;
    if (!pr) {
      pr = c
        .loadFile(path)
        .then((d) => !!d)
        .catch(() => false);
      this.exists.set(path, pr);
      void pr.then((v) => this.exists.set(path, v));
    }
    return pr.then((v) => (this.sound_started ? finish(v) : null));
  }

  // C: snd_dma.c:668 S_StartSound -- validates the parms and queues the sound up; if origin is NULL the
  // sound will be dynamically sourced from the entity. Entchannel 0 will never override a playing sound.
  startSound(
    origin: Float32Array | null,
    entnum: number,
    entchannel: number,
    handle: SfxHandle | null,
    fvol: number,
    attenuation: number,
    timeofs: number,
  ): void {
    if (!this.sound_started) return;
    let sfx = asSfx(handle);
    if (!sfx) return;
    const c = this.c;
    const servertime = c.cl.frame.servertime;
    const stopGen = this.stopGen;
    const org = origin ? Float32Array.from(origin) : null;

    const issue = (s: Sfx): void => {
      // make sure the sound is loaded
      const sc = this.S_LoadSound(s);
      if (sc instanceof Promise) {
        // not resident yet (C would block on the disk here): start it once the data arrives
        void sc.then((r) => {
          if (r && this.sound_started && this.stopGen === stopGen)
            this.emitStart(org, entnum, entchannel, s, fvol, attenuation, timeofs, servertime);
        });
        return;
      }
      if (!sc) return; // couldn't load the sound's data
      this.emitStart(org, entnum, entchannel, s, fvol, attenuation, timeofs, servertime);
    };

    if (sfx.name[0] === '*') {
      const ent = entnum >= 0 && entnum < MAX_EDICTS ? c.cl_entities[entnum]!.current.number : 0;
      const r = this.S_RegisterSexedSound(ent, sfx.name);
      if (r instanceof Promise) {
        void r.then((s) => {
          if (s && this.sound_started && this.stopGen === stopGen) issue(s);
        });
        return;
      }
      sfx = r;
      if (!sfx) return;
    }
    issue(sfx);
  }

  private emitStart(
    origin: Float32Array | null,
    entnum: number,
    entchannel: number,
    s: Sfx,
    fvol: number,
    attenuation: number,
    timeofs: number,
    servertime: number,
  ): void {
    if (!origin) this.dynEnts.add(entnum);
    writeStart(this.w, origin, entnum, entchannel, s.index, fvol, attenuation, timeofs, servertime);
  }

  // C: snd_dma.c:740 S_StartLocalSound
  startLocalSound(sound: string): void {
    if (!this.sound_started) return;

    const sfx = this.S_RegisterSound(sound);
    if (!sfx) {
      Com_Printf(this.c, "S_StartLocalSound: can't cache %s\n", sound);
      return;
    }
    this.startSound(null, this.c.cl.playernum + 1, 0, asHandle(sfx), 1, 1, 0);
  }

  // C: snd_dma.c:790 S_StopAllSounds
  stopAllSounds(): void {
    if (!this.sound_started) return;
    this.stopGen++;
    writeStopAll(this.w);
  }

  // C: snd_dma.c:916 S_RawSamples -- cinematic streaming
  rawSamples(samples: number, rate: number, width: number, channels: number, data: Uint8Array): void {
    if (!this.sound_started) return;
    const n = Math.max(0, Math.min(data.length, samples * width * channels));
    writeRaw(this.w, samples, rate, width, channels, data.subarray(0, n));
  }

  // C: snd_dma.c:1016 S_Update -- gathers the client state the mixer reads and hands the frame over
  update(origin: Float32Array, forward: Float32Array, right: Float32Array, up: Float32Array): void {
    if (!this.sound_started) return;
    const c = this.c;
    const cl = c.cl;

    // CL_GetEntitySoundOrigin for every entity that has started a dynamically sourced sound
    if (this.dynEnts.size) {
      const ents = Array.from(this.dynEnts);
      const org = new Float32Array(ents.length * 3);
      for (let i = 0; i < ents.length; i++) {
        CL_GetEntitySoundOrigin(c, ents[i]!, this.tmpOrigin);
        org.set(this.tmpOrigin, i * 3);
      }
      writeOrigins(this.w, ents, org);
    }

    // S_AddLoopSounds input: the sound field of every entity of cl.frame
    const loops: LoopEnt[] = [];
    const active = c.cls.state === ca_active;
    const paused = !!c.cv.cl_paused?.value;
    if (!paused && active && cl.sound_prepped) {
      for (let i = 0; i < cl.frame.num_entities; i++) {
        const num = (cl.frame.parse_entities + i) & (MAX_PARSE_ENTITIES - 1);
        const ent = c.cl_parse_entities[num]!;
        const sfx = ent.sound ? asSfx(cl.sound_precache[ent.sound] ?? null) : null;
        loops.push({ sound: ent.sound, sfx: sfx ? sfx.index : -1, origin: ent.origin });
      }
    }

    const vol = this.s_volume!;
    const volumeModified = vol.modified;
    vol.modified = false;
    writeUpdate(this.w, {
      listenerOrigin: origin,
      listenerForward: forward,
      listenerRight: right,
      listenerUp: up,
      playernum: cl.playernum,
      active,
      paused,
      soundPrepped: cl.sound_prepped,
      disableScreen: !!c.cls.disable_screen,
      volume: vol.value,
      volumeModified,
      mixahead: this.s_mixahead!.value,
      testsound: this.s_testsound!.value,
      show: this.s_show!.value,
      loops,
    });
    this.flush();
  }

  // ---- console functions

  // C: snd_dma.c:1170 S_Play
  S_Play(): void {
    const cmd = this.c.cmd;
    for (let i = 1; i < cmd.argc(); i++) {
      const a = cmd.argv(i);
      const name = a.lastIndexOf('.') < 0 ? a + '.wav' : a;
      const sfx = this.S_RegisterSound(name);
      this.startSound(null, this.c.cl.playernum + 1, 0, asHandle(sfx), 1.0, 1.0, 0);
    }
  }

  // C: snd_dma.c:1192 S_SoundList
  S_SoundList(): void {
    const c = this.c;
    let total = 0;
    for (let i = 0; i < this.num_sfx; i++) {
      const sfx = this.known_sfx[i]!;
      if (!sfx.registration_sequence) continue;
      const sc = sfx.cache;
      if (sc) {
        const size = sc.length * sc.width * (sc.stereo + 1);
        total += size;
        if (sc.loopstart >= 0) Com_Printf(c, 'L');
        else Com_Printf(c, ' ');
        Com_Printf(c, '(%2db) %6i : %s\n', sc.width * 8, size, sfx.name);
      } else {
        if (sfx.name[0] === '*') Com_Printf(c, '  placeholder : %s\n', sfx.name);
        else Com_Printf(c, '  not loaded  : %s\n', sfx.name);
      }
    }
    Com_Printf(c, 'Total resident: %i\n', total);
  }

  // C: snd_dma.c:89 S_SoundInfo_f
  S_SoundInfo_f(): void {
    const c = this.c;
    if (!this.sound_started) {
      Com_Printf(c, 'sound system not started\n');
      return;
    }
    const info = this.backend.info ?? { channels: 2, samples: 32768, samplebits: 16, submission_chunk: 1 };
    Com_Printf(c, '%5d stereo\n', info.channels - 1);
    Com_Printf(c, '%5d samples\n', info.samples);
    Com_Printf(c, '%5d samplepos\n', 0);
    Com_Printf(c, '%5d samplebits\n', info.samplebits);
    Com_Printf(c, '%5d submission_chunk\n', info.submission_chunk);
    Com_Printf(c, '%5d speed\n', this.speed);
    Com_Printf(c, '0x%x dma buffer\n', 0);
  }
}

/** Com_sprintf into a char[MAX_QPATH] (truncation) */
function truncQPath(s: string): string {
  return s.length >= MAX_QPATH ? s.slice(0, MAX_QPATH - 1) : s;
}
