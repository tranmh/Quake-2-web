// Shared test helpers for q2-client (additive: other test files may add helpers here).
import type { ImageHandle, ModelHandle, RefDef, Refresh } from 'q2-ref';
import { ClientContext } from '../src/client';
import type { Effects } from '../src/effects';
import { NullSound } from '../src/sound';
import { NullCinematics } from '../src/cinematic';
import { MemoryTransport } from '../src/transport';

export type DrawCall =
  | { op: 'char'; x: number; y: number; c: number }
  | { op: 'pic'; x: number; y: number; name: string }
  | { op: 'stretchpic'; x: number; y: number; w: number; h: number; name: string }
  | { op: 'tileclear'; x: number; y: number; w: number; h: number; name: string }
  | { op: 'fill'; x: number; y: number; w: number; h: number; c: number }
  | { op: 'fadescreen' }
  | { op: 'stretchraw'; x: number; y: number; w: number; h: number; cols: number; rows: number }
  | { op: 'renderframe'; fd: RefDef }
  | { op: 'beginframe' }
  | { op: 'endframe' };

export interface RecordingRefresh extends Refresh {
  calls: DrawCall[];
  registered: { kind: 'model' | 'skin' | 'pic' | 'sky' | 'map'; name: string }[];
  /** pic sizes returned by drawGetPicSize (default [0,0]) */
  picSizes: Map<string, [number, number]>;
}

/** A fake Refresh recording draw calls; register* resolve immediately to opaque `{ name }` handles. */
export function createRecordingRefresh(): RecordingRefresh {
  const calls: DrawCall[] = [];
  const registered: RecordingRefresh['registered'] = [];
  const picSizes = new Map<string, [number, number]>();
  return {
    calls,
    registered,
    picSizes,
    async init() {
      return true;
    },
    shutdown() {},
    async beginRegistration(map: string) {
      registered.push({ kind: 'map', name: map });
    },
    async registerModel(name: string) {
      registered.push({ kind: 'model', name });
      return { name } as unknown as ModelHandle;
    },
    async registerSkin(name: string) {
      registered.push({ kind: 'skin', name });
      return { name } as unknown as ImageHandle;
    },
    async registerPic(name: string) {
      registered.push({ kind: 'pic', name });
      return { name } as unknown as ImageHandle;
    },
    async setSky(name: string) {
      registered.push({ kind: 'sky', name });
    },
    endRegistration() {},
    renderFrame(fd: RefDef) {
      calls.push({ op: 'renderframe', fd });
    },
    drawGetPicSize(name: string): [number, number] {
      return picSizes.get(name) ?? [0, 0];
    },
    drawPic(x, y, name) {
      calls.push({ op: 'pic', x, y, name });
    },
    drawStretchPic(x, y, w, h, name) {
      calls.push({ op: 'stretchpic', x, y, w, h, name });
    },
    drawChar(x, y, c) {
      calls.push({ op: 'char', x, y, c });
    },
    drawTileClear(x, y, w, h, name) {
      calls.push({ op: 'tileclear', x, y, w, h, name });
    },
    drawFill(x, y, w, h, c) {
      calls.push({ op: 'fill', x, y, w, h, c });
    },
    drawFadeScreen() {
      calls.push({ op: 'fadescreen' });
    },
    drawStretchRaw(x, y, w, h, cols, rows) {
      calls.push({ op: 'stretchraw', x, y, w, h, cols, rows });
    },
    cinematicSetPalette() {},
    beginFrame() {
      calls.push({ op: 'beginframe' });
    },
    endFrame() {
      calls.push({ op: 'endframe' });
    },
    appActivate() {},
  };
}

/** Effects stub doing nothing (message parsing not supported; use NullEffects for that). */
export function createStubEffects(): Effects {
  const noop = () => {};
  return new Proxy({} as Effects, {
    get: (_t, p) => (p === 'registerTEntModels' ? async () => {} : p === 'cl_mod_powerscreen' ? null : noop),
  });
}

/** A bare ClientContext for unit tests (no init functions called). */
export function createTestContext(
  opts: { refresh?: Refresh; milliseconds?: () => number } = {},
): ClientContext {
  return new ClientContext({
    refresh: opts.refresh ?? createRecordingRefresh(),
    transport: () => new MemoryTransport(),
    loadFile: async () => null,
    sound: new NullSound(),
    effects: createStubEffects(),
    cinematics: new NullCinematics(),
    milliseconds: opts.milliseconds,
  });
}

/** Collects Com_Printf output of a context. */
export function capturePrints(c: ClientContext): string[] {
  const out: string[] = [];
  c.main.printHook = (m) => out.push(m);
  return out;
}

/** Text of the drawChar calls of one row (y), in x order, as a byte string (high bit stripped if asked). */
export function rowText(calls: DrawCall[], y: number, strip = false): string {
  return calls
    .filter((d): d is Extract<DrawCall, { op: 'char' }> => d.op === 'char' && d.y === y)
    .sort((a, b) => a.x - b.x)
    .map((d) => String.fromCharCode(strip ? d.c & 127 : d.c))
    .join('');
}
