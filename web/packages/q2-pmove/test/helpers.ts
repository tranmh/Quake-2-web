import { Pak } from 'q2-formats';
import { demoPakPath, fileExists, readArrayBuffer } from 'q2-shared/testing';
import { CollisionModel } from '../src';

let pak: Pak | null | undefined;

/** The demo pak0.pak or null when absent. */
export function demoPak(): Pak | null {
  if (pak !== undefined) return pak;
  const p = demoPakPath();
  pak = fileExists(p) ? new Pak(readArrayBuffer(p)!, 'pak0.pak') : null;
  return pak;
}

const models = new Map<string, CollisionModel>();

export function loadMap(name: string): CollisionModel {
  let m = models.get(name);
  if (!m) {
    const p = demoPak();
    if (!p) throw new Error('demo pak missing');
    const data = p.read(`maps/${name}.bsp`);
    if (!data) throw new Error(`maps/${name}.bsp missing`);
    m = CollisionModel.load(`maps/${name}.bsp`, data);
    models.set(name, m);
  }
  return m;
}
