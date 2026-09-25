// Recording fake Refresh for cl_scrn / layout / cl_inv tests.
import type { ImageHandle, ModelHandle, RefDef, Refresh } from 'q2-ref';

export type RefCall = [string, ...unknown[]];

export function createFakeRefresh(
  picSizes: Record<string, [number, number]> = {},
): Refresh & { calls: RefCall[] } {
  const calls: RefCall[] = [];
  const h = (name: string) => ({ name }) as unknown as ImageHandle;
  return {
    calls,
    init: async () => true,
    shutdown: () => {},
    beginRegistration: async () => {},
    registerModel: async (name: string) => ({ name }) as unknown as ModelHandle,
    registerSkin: async (name: string) => h(name),
    registerPic: async (name: string) => {
      calls.push(['registerPic', name]);
      return h(name);
    },
    setSky: async (name: string, rotate: number, axis: Float32Array) => {
      calls.push(['setSky', name, rotate, Array.from(axis)]);
    },
    endRegistration: () => {},
    renderFrame: (_fd: RefDef) => {
      calls.push(['renderFrame']);
    },
    drawGetPicSize: (name: string) => picSizes[name] ?? [0, 0],
    drawPic: (x, y, name) => calls.push(['pic', x, y, name]),
    drawStretchPic: (x, y, w, hh, name) => calls.push(['stretchpic', x, y, w, hh, name]),
    drawChar: (x, y, c) => calls.push(['char', x, y, c]),
    drawTileClear: (x, y, w, hh, name) => calls.push(['tileclear', x, y, w, hh, name]),
    drawFill: (x, y, w, hh, c) => calls.push(['fill', x, y, w, hh, c]),
    drawFadeScreen: () => calls.push(['fade']),
    drawStretchRaw: () => calls.push(['raw']),
    cinematicSetPalette: () => calls.push(['cinpal']),
    beginFrame: () => calls.push(['begin']),
    endFrame: () => calls.push(['end']),
    appActivate: () => {},
  };
}

/** Collapses consecutive drawChar calls on one row into strings (high bit shown as {..}). */
export function charsToText(calls: RefCall[]): string[] {
  const out: string[] = [];
  let cur: { x: number; y: number; s: string } | null = null;
  for (const c of calls) {
    if (c[0] === 'char') {
      const [, x, y, n] = c as [string, number, number, number];
      const ch = n & 127;
      const txt = (n & 128 ? '~' : '') + String.fromCharCode(ch < 32 ? 63 : ch);
      if (cur && cur.y === y && x === cur.x + 8 * [...cur.s.replace(/~/g, '')].length) cur.s += txt;
      else {
        if (cur) out.push(`${cur.x},${cur.y}:${cur.s}`);
        cur = { x, y, s: txt };
      }
    } else {
      if (cur) out.push(`${cur.x},${cur.y}:${cur.s}`);
      cur = null;
      out.push(c.join(','));
    }
  }
  if (cur) out.push(`${cur.x},${cur.y}:${cur.s}`);
  return out;
}
