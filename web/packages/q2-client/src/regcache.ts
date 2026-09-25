// Browser adaptation: the C refresh registers models/skins/pics synchronously (re.RegisterModel returns
// a pointer immediately, loading from disk). Here registration is asynchronous, so the client keeps a
// cache. Code that the original runs inside a frame (CL_AddPacketEntities vwep models, configstring
// changes after the level is prepped, HUD pics) uses the *Sync variants: they return the cached handle,
// or start loading and return null until it resolves. CL_PrepRefresh awaits the async variants.
// The cache is flushed by beginRegistration (a new level).
import type { ImageHandle, ModelHandle } from 'q2-ref';
import type { ClientContext } from './client';

type Kind = 'model' | 'skin' | 'pic';

export class RegistrationCache {
  readonly handles = new Map<string, ModelHandle | ImageHandle | null>();
  readonly pending = new Map<string, Promise<ModelHandle | ImageHandle | null>>();
  /** bumped by clear(); results of loads started before a clear are discarded */
  generation = 0;

  clear(): void {
    this.handles.clear();
    this.pending.clear();
    this.generation++;
  }
}

function load(c: ClientContext, kind: Kind, name: string): Promise<ModelHandle | ImageHandle | null> {
  const rc = c.regcache;
  const key = kind + ':' + name;
  const p0 = rc.pending.get(key);
  if (p0) return p0;
  const gen = rc.generation;
  const call =
    kind === 'model'
      ? c.re.registerModel(name)
      : kind === 'skin'
        ? c.re.registerSkin(name)
        : c.re.registerPic(name);
  const p = call.then(
    (h) => {
      if (rc.generation === gen) {
        rc.handles.set(key, h);
        rc.pending.delete(key);
      }
      return h;
    },
    () => {
      if (rc.generation === gen) {
        rc.handles.set(key, null);
        rc.pending.delete(key);
      }
      return null;
    },
  );
  rc.pending.set(key, p);
  return p;
}

function sync(c: ClientContext, kind: Kind, name: string): ModelHandle | ImageHandle | null {
  const key = kind + ':' + name;
  const h = c.regcache.handles.get(key);
  if (h !== undefined) return h;
  void load(c, kind, name);
  return null;
}

/** re.RegisterModel (awaitable, cached) */
export function RegisterModel(c: ClientContext, name: string): Promise<ModelHandle | null> {
  const key = 'model:' + name;
  const h = c.regcache.handles.get(key);
  if (h !== undefined) return Promise.resolve(h as ModelHandle | null);
  return load(c, 'model', name) as Promise<ModelHandle | null>;
}
/** re.RegisterSkin (awaitable, cached) */
export function RegisterSkin(c: ClientContext, name: string): Promise<ImageHandle | null> {
  const key = 'skin:' + name;
  const h = c.regcache.handles.get(key);
  if (h !== undefined) return Promise.resolve(h as ImageHandle | null);
  return load(c, 'skin', name) as Promise<ImageHandle | null>;
}
/** re.RegisterPic (awaitable, cached) */
export function RegisterPic(c: ClientContext, name: string): Promise<ImageHandle | null> {
  const key = 'pic:' + name;
  const h = c.regcache.handles.get(key);
  if (h !== undefined) return Promise.resolve(h as ImageHandle | null);
  return load(c, 'pic', name) as Promise<ImageHandle | null>;
}

/** re.RegisterModel from inside a frame: cached handle or null while loading. */
export function RegisterModelSync(c: ClientContext, name: string): ModelHandle | null {
  return sync(c, 'model', name) as ModelHandle | null;
}
/** re.RegisterSkin from inside a frame: cached handle or null while loading. */
export function RegisterSkinSync(c: ClientContext, name: string): ImageHandle | null {
  return sync(c, 'skin', name) as ImageHandle | null;
}
/** re.RegisterPic from inside a frame: cached handle or null while loading. */
export function RegisterPicSync(c: ClientContext, name: string): ImageHandle | null {
  return sync(c, 'pic', name) as ImageHandle | null;
}
