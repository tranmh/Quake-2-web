// Headless helpers: a fake refimport_t, a recording QGL stub (mirrors scripts/oracle/oracle.c capture)
// and a demo1 renderer state loaded from the demo pak.
import { Pak } from 'q2-formats';
import type { RefImport } from 'q2-ref';
import { demoPakPath, fileExists, readArrayBuffer } from 'q2-shared/testing';
import { GLState, type QGL } from '../src/index';
import { R_Init } from '../src/gl_rmain';
import { R_BeginRegistration, R_RegisterModel } from '../src/gl_model';

/** FNV-1a 32 (same as oracle.c fnv) */
export function fnv(b: Uint8Array, h = 2166136261): number {
  for (let i = 0; i < b.length; i++) {
    h ^= b[i]!;
    h = Math.imul(h, 16777619) >>> 0;
  }
  return h >>> 0;
}
export function fnvFloats(f: ArrayLike<number>, h = 2166136261): number {
  return fnv(new Uint8Array(Float32Array.from(f).buffer), h);
}
export function fnvInts(v: number[], h = 2166136261): number {
  return fnv(new Uint8Array(Int32Array.from(v).buffer), h);
}

/** oracle.c lcg */
export class Lcg {
  constructor(public seed: number) {}
  next(): number {
    this.seed = (Math.imul(this.seed, 1103515245) + 12345) >>> 0;
    return (this.seed >>> 16) & 0x7fff;
  }
}

let pak: Pak | null | undefined;
export function demoPak(): Pak | null {
  if (pak !== undefined) return pak;
  const p = demoPakPath();
  pak = fileExists(p) ? new Pak(readArrayBuffer(p)!, 'pak0.pak') : null;
  return pak;
}

export interface FakeCvar {
  string: string;
  value: number;
  modified: boolean;
}

export function fakeImports(): { ri: RefImport; cvars: Map<string, FakeCvar>; log: string[] } {
  const cvars = new Map<string, FakeCvar>();
  const log: string[] = [];
  const ri: RefImport = {
    loadFile: async () => null,
    cvarGet(name, value) {
      let c = cvars.get(name);
      if (!c) {
        c = { string: value, value: parseFloat(value) || 0, modified: true };
        cvars.set(name, c);
      }
      return c;
    },
    cvarSet(name, value) {
      const c = ri.cvarGet(name, value, 0);
      c.string = value;
      c.value = parseFloat(value) || 0;
      c.modified = true;
    },
    conPrintf(_l, text) {
      log.push(text);
    },
    sysError(_l, text): never {
      throw new Error(`Sys_Error: ${text}`);
    },
    addCommand() {},
    removeCommand() {},
  };
  return { ri, cvars, log };
}

export interface Upload {
  texnum: number;
  level: number;
  comp: number;
  w: number;
  h: number;
  hash: number;
}

/** Records texture uploads and the immediate-mode vertex stream like oracle.c. */
export class RecordingQGL {
  uploads: Upload[] = [];
  boundTex = 0;
  rec = false;
  count = 0;
  prims = 0;
  hash = 2166136261;
  first: number[][] = [];
  private st = [0, 0];
  private col = [1, 1, 1, 1];
  drawingBufferWidth = 640;
  drawingBufferHeight = 480;
  drawCalls = 0;
  gl: unknown = null;

  start(): void {
    this.rec = true;
    this.st = [0, 0];
    this.col = [1, 1, 1, 1];
    this.count = this.prims = 0;
    this.hash = 2166136261;
    this.first = [];
  }
  stop(): { count: number; prims: number; hash: number; first: number[][] } {
    this.rec = false;
    return { count: this.count, prims: this.prims, hash: this.hash, first: this.first };
  }
  bindTexture(t: number): void {
    this.boundTex = t;
  }
  texImage2D(level: number, comp: number, w: number, h: number, data: Uint8Array): void {
    this.uploads.push({ texnum: this.boundTex, level, comp, w, h, hash: fnv(data.subarray(0, w * h * 4)) });
  }
  begin(mode: number): void {
    if (!this.rec) return;
    this.prims++;
    this.hash = fnv(new Uint8Array(Int32Array.of(mode).buffer), this.hash);
  }
  texCoord2f(s: number, t: number): void {
    this.st[0] = s;
    this.st[1] = t;
  }
  color4f(r: number, g: number, b: number, a: number): void {
    this.col = [r, g, b, a];
  }
  color3f(r: number, g: number, b: number): void {
    this.col = [r, g, b, 1];
  }
  color4ub(r: number, g: number, b: number, a: number): void {
    this.col = [r / 255, g / 255, b / 255, a / 255];
  }
  vertex3f(x: number, y: number, z: number): void {
    if (!this.rec) return;
    const v = [x, y, z, this.st[0]!, this.st[1]!, ...this.col];
    this.hash = fnvFloats(v, this.hash);
    if (this.count < 64) this.first.push(Array.from(Float32Array.from(v)));
    this.count++;
  }
  vertex3fv(v: ArrayLike<number>, o = 0): void {
    this.vertex3f(v[o]!, v[o + 1]!, v[o + 2]!);
  }
  vertex2f(x: number, y: number): void {
    this.vertex3f(x, y, 0);
  }
  getError(): number {
    return 0;
  }
  getModelview(out: Float32Array): void {
    out.fill(0);
  }
  createWorldBuffer(): unknown {
    return {};
  }
}

/** A RecordingQGL whose unknown methods are no-ops, typed as QGL. */
export function recordingQGL(): { rec: RecordingQGL; qgl: QGL } {
  const rec = new RecordingQGL();
  const qgl = new Proxy(rec, {
    get(target, prop, recv) {
      if (prop in target) return Reflect.get(target, prop, recv);
      return () => undefined;
    },
  }) as unknown as QGL;
  return { rec, qgl };
}

export interface Demo1 {
  r: GLState;
  rec: RecordingQGL;
  cvars: Map<string, FakeCvar>;
}

let demo1: Demo1 | undefined;

/** R_Init + R_BeginRegistration("demo1") + R_RegisterModel("maps/demo1.bsp") with a recording QGL. */
export function loadDemo1(): Demo1 {
  if (demo1) return demo1;
  const p = demoPak();
  if (!p) throw new Error('demo pak missing');
  const { ri, cvars } = fakeImports();
  const r = new GLState(ri);
  const { rec, qgl } = recordingQGL();
  r.qgl = qgl;
  r.files = { get: (name: string) => p.read(name) ?? null };
  R_Init(r);
  R_BeginRegistration(r, 'demo1');
  R_RegisterModel(r, 'maps/demo1.bsp');
  demo1 = { r, rec, cvars };
  return demo1;
}
