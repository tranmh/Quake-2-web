// Port of client/cl_cin.c -- cinematics (.cin playback) and static .pcx screens.
//
// The order-1 Huffman decoder (Huff1TableInit / Huff1Decompress / SmallestNode1) is the one in
// q2-formats (Huff1Tables); the frame reader is ported here so that client state (cl.cinematicpalette,
// cl.cinematicpalette_active, cl.cinematicframe, S_RawSamples) is touched exactly as in C.
//
// Deviation (async file system): C opens the file synchronously inside SCR_PlayCinematic. Here play()
// sets cl.cinematicframe = 0 synchronously, starts loadFile, and runs the rest of SCR_PlayCinematic
// (from SCR_LoadPCX / FS_FOpenFile on) when the bytes arrive -- unless a newer play(), CL_ClearState
// (c.clearGeneration) or a disconnect happened meanwhile. Until then cl.cinematictime stays 0, so
// SCR_RunCinematic/SCR_DrawCinematic behave like "no cinematic" (as in C before the call). The whole file
// is held in memory; FS_Read short reads (ERR_FATAL in C) become ERR_DROP.
import { ERR_DROP, sprintf } from 'q2-shared';
import { Huff1Tables } from 'q2-formats';
import {
  ca_active,
  ca_disconnected,
  Com_Error,
  Com_Printf,
  key_game,
  key_menu,
  Sys_Milliseconds,
  type ClientContext,
} from './client';
import { SCR_FinishCinematic, type Cinematics } from './cinematic';
import { SCR_BeginLoadingPlaque, SCR_EndLoadingPlaque } from './cl_scrn';
import { CL_Snd_Restart_f, Com_HandleError } from './cl_main';

/** In-memory replacement of the FILE *cl.cinematic_file. */
class CinFile {
  pos = 0;
  constructor(readonly data: Uint8Array) {}

  /** fread(buf, 4, 1, f): null when fewer than 4 bytes remain (r == 0). */
  freadLong(): number | null {
    if (this.pos + 4 > this.data.length) {
      this.pos = this.data.length;
      return null;
    }
    const d = this.data;
    const p = this.pos;
    this.pos += 4;
    return d[p]! | (d[p + 1]! << 8) | (d[p + 2]! << 16) | (d[p + 3]! << 24) | 0;
  }

  // C: files.c FS_Read -- a short read is ERR_FATAL ("FS_Read: 0 bytes read") in C
  read(c: ClientContext, n: number): Uint8Array {
    if (n <= 0) return this.data.subarray(this.pos, this.pos);
    if (this.pos + n > this.data.length) {
      this.pos = this.data.length;
      Com_Error(c, ERR_DROP, 'FS_Read: 0 bytes read');
    }
    const r = this.data.subarray(this.pos, this.pos + n);
    this.pos += n;
    return r;
  }

  readLong(c: ClientContext): number {
    const b = this.read(c, 4);
    return b[0]! | (b[1]! << 8) | (b[2]! << 16) | (b[3]! << 24) | 0;
  }
}

// C: cl_cin.c:62 SCR_LoadPCX -- returns null where C leaves *pic NULL
export function SCR_LoadPCX(
  c: ClientContext,
  filename: string,
  raw: Uint8Array | null,
): { pic: Uint8Array; palette: Uint8Array; width: number; height: number } | null {
  if (!raw) return null; // Com_Printf ("Bad pcx file %s\n", filename);
  const len = raw.length;
  const b = (o: number): number => (o < len ? raw[o]! : 0); // reads past the end are garbage in C
  const u16 = (o: number): number => b(o) | (b(o + 1) << 8);
  const manufacturer = b(0);
  const version = b(1);
  const encoding = b(2);
  const bits_per_pixel = b(3);
  const xmax = u16(8);
  const ymax = u16(10);
  if (
    manufacturer !== 0x0a ||
    version !== 5 ||
    encoding !== 1 ||
    bits_per_pixel !== 8 ||
    xmax >= 640 ||
    ymax >= 480
  ) {
    Com_Printf(c, 'Bad pcx file %s\n', filename);
    return null;
  }
  const w = xmax + 1;
  const h = ymax + 1;
  const out = new Uint8Array(h * w);
  const palette = new Uint8Array(768);
  for (let i = 0; i < 768; i++) palette[i] = len - 768 + i >= 0 ? b(len - 768 + i) : 0;

  let r = 128; // &pcx->data
  let pix = 0;
  for (let y = 0; y <= ymax; y++, pix += w) {
    for (let x = 0; x <= xmax;) {
      let dataByte = b(r++);
      let runLength: number;
      if ((dataByte & 0xc0) === 0xc0) {
        runLength = dataByte & 0x3f;
        dataByte = b(r++);
      } else runLength = 1;
      // (sic) a run may write past the row end in C (into the next row / past the buffer)
      while (runLength-- > 0) {
        const o = pix + x++;
        if (o < out.length) out[o] = dataByte;
      }
    }
  }
  if (r > len) {
    Com_Printf(c, 'PCX file %s was malformed', filename);
    return null;
  }
  return { pic: out, palette, width: w, height: h };
}

/** C: cl_cin.c cinematics_t cin + cl.cinematic_file */
export class ClientCinematics implements Cinematics {
  private c: ClientContext | null = null;
  restart_sound = false;
  s_rate = 0;
  s_width = 0;
  s_channels = 0;
  width = 0;
  height = 0;
  pic: Uint8Array | null = null;
  pic_pending: Uint8Array | null = null;
  private huff: Huff1Tables | null = null;
  private file: CinFile | null = null;
  /** bumped by every play(): stale loads are abandoned */
  private token = 0;
  /** resolves when the pending load of the last play() has been handled (tests) */
  pending: Promise<void> = Promise.resolve();

  attach(c: ClientContext): void {
    this.c = c;
  }

  // C: cl_cin.c:153 SCR_StopCinematic
  stop(): void {
    const c = this.c!;
    c.cl.cinematictime = 0; // done
    this.pic = null;
    this.pic_pending = null;
    if (c.cl.cinematicpalette_active) {
      c.re.cinematicSetPalette(null);
      c.cl.cinematicpalette_active = false;
    }
    this.file = null;
    this.huff = null;
    // switch back down to 11 khz sound if necessary
    if (this.restart_sound) {
      this.restart_sound = false;
      CL_Snd_Restart_f(c);
    }
  }

  // C: cl_cin.c:421 SCR_ReadNextFrame
  private readNextFrame(): Uint8Array | null {
    const c = this.c!;
    const cl = c.cl;
    const f = this.file;
    if (!f || !this.huff) return null;
    // read the next frame
    let command = f.freadLong();
    if (command === null) command = f.freadLong(); // we'll give it one more chance
    if (command === null) return null;
    if (command === 2) return null; // last frame marker

    if (command === 1) {
      // read palette
      cl.cinematicpalette.set(f.read(c, 768));
      cl.cinematicpalette_active = false; // dubious....  exposes an edge case
    }

    // decompress the next frame
    const size = f.readLong(c);
    if (size > 0x20000 || size < 1) Com_Error(c, ERR_DROP, 'Bad compressed frame size');
    const compressed = f.read(c, size);

    // read sound
    const start = Math.trunc((cl.cinematicframe * this.s_rate) / 14);
    const end = Math.trunc(((cl.cinematicframe + 1) * this.s_rate) / 14);
    const count = end - start;
    const samples = f.read(c, count * this.s_width * this.s_channels).slice();
    c.sound.rawSamples(count, this.s_rate, this.s_width, this.s_channels, samples);

    // C: Huff1Decompress
    const { data, consumed } = this.huff.decompress(compressed);
    if (consumed !== size && consumed !== size + 1)
      Com_Printf(c, 'Decompression overread by %i', consumed - size);
    cl.cinematicframe++;
    return data.slice();
  }

  // C: cl_cin.c:474 SCR_RunCinematic
  run(): void {
    const c = this.c!;
    const { cl, cls } = c;
    if (cl.cinematictime <= 0) {
      this.stop();
      return;
    }
    if (cl.cinematicframe === -1) return; // static image
    if (cls.key_dest !== key_game) {
      // pause if menu or console is up
      cl.cinematictime = cls.realtime - Math.trunc((cl.cinematicframe * 1000) / 14);
      return;
    }
    const frame = Math.trunc(((cls.realtime - cl.cinematictime) * 14.0) / 1000);
    if (frame <= cl.cinematicframe) return;
    if (frame > cl.cinematicframe + 1) {
      Com_Printf(c, 'Dropped frame: %i > %i\n', frame, cl.cinematicframe + 1);
      cl.cinematictime = cls.realtime - Math.trunc((cl.cinematicframe * 1000) / 14);
    }
    this.pic = this.pic_pending;
    this.pic_pending = null;
    this.pic_pending = this.readNextFrame();
    if (!this.pic_pending) {
      this.stop();
      SCR_FinishCinematic(c);
      cl.cinematictime = 1; // hack to get the black screen behind loading
      SCR_BeginLoadingPlaque(c);
      cl.cinematictime = 0;
      return;
    }
  }

  // C: cl_cin.c:531 SCR_DrawCinematic -- true if a cinematic is active (view rendering is skipped)
  draw(): boolean {
    const c = this.c!;
    const cl = c.cl;
    if (cl.cinematictime <= 0) return false;
    if (c.cls.key_dest === key_menu) {
      // blank screen and pause if menu is up
      c.re.cinematicSetPalette(null);
      cl.cinematicpalette_active = false;
      return true;
    }
    if (!cl.cinematicpalette_active) {
      c.re.cinematicSetPalette(cl.cinematicpalette);
      cl.cinematicpalette_active = true;
    }
    if (!this.pic) return true;
    c.re.drawStretchRaw(0, 0, c.viddef.width, c.viddef.height, this.width, this.height, this.pic);
    return true;
  }

  // C: cl_cin.c:567 SCR_PlayCinematic
  play(arg: string): void {
    const c = this.c!;
    // make sure CD isn't playing music (CDAudio_Stop: no CD audio)
    c.cl.cinematicframe = 0;
    const dot = arg.indexOf('.');
    const isPcx = dot >= 0 && arg.slice(dot) === '.pcx';
    const name = sprintf(isPcx ? 'pics/%s' : 'video/%s', arg).slice(0, 127); // MAX_OSPATH
    const token = ++this.token;
    const gen = c.clearGeneration;
    this.pending = c.loadFile(name).then(
      (data) => {
        if (token !== this.token || gen !== c.clearGeneration || c.cls.state === ca_disconnected) return;
        try {
          if (isPcx) this.playPcx(name, data);
          else this.playCin(name, data);
        } catch (e) {
          Com_HandleError(c, e);
        }
      },
      (e: unknown) => {
        if (token === this.token) Com_HandleError(c, e);
      },
    );
  }

  /** SCR_PlayCinematic, static pcx image branch (after FS_LoadFile) */
  private playPcx(name: string, data: Uint8Array | null): void {
    const c = this.c!;
    const { cl, cls } = c;
    const img = SCR_LoadPCX(c, name, data);
    this.pic = img ? img.pic : null;
    if (img) {
      this.width = img.width;
      this.height = img.height;
    }
    cl.cinematicframe = -1;
    cl.cinematictime = 1;
    SCR_EndLoadingPlaque(c);
    cls.state = ca_active;
    if (!img) {
      Com_Printf(c, '%s not found.\n', name);
      cl.cinematictime = 0;
    } else cl.cinematicpalette.set(img.palette);
  }

  /** SCR_PlayCinematic, .cin branch (after FS_FOpenFile) */
  private playCin(_name: string, data: Uint8Array | null): void {
    const c = this.c!;
    const { cl, cls } = c;
    if (!data) {
      // Com_Error (ERR_DROP, "Cinematic %s not found.\n", name);
      SCR_FinishCinematic(c);
      cl.cinematictime = 0; // done
      return;
    }
    const f = (this.file = new CinFile(data));
    SCR_EndLoadingPlaque(c);
    cls.state = ca_active;

    this.width = f.readLong(c);
    this.height = f.readLong(c);
    this.s_rate = f.readLong(c);
    this.s_width = f.readLong(c);
    this.s_channels = f.readLong(c);

    // C: Huff1TableInit (256 FS_Reads of 256 counts)
    this.huff = new Huff1Tables();
    this.huff.init(f.read(c, 65536));

    // switch up to 22 khz sound if necessary
    const old_khz = Math.trunc(c.cvars.variableValue('s_khz'));
    if (old_khz !== Math.trunc(this.s_rate / 1000)) {
      this.restart_sound = true;
      c.cvars.setValue('s_khz', Math.trunc(this.s_rate / 1000));
      CL_Snd_Restart_f(c);
      c.cvars.setValue('s_khz', old_khz);
    }

    cl.cinematicframe = 0;
    this.pic = this.readNextFrame();
    cl.cinematictime = Sys_Milliseconds(c);
  }
}

/** The ported cl_cin.c cinematics (default of createClientEngine). */
export function createClientCinematics(): ClientCinematics {
  return new ClientCinematics();
}
