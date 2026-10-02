// GameSession input wiring (docs/review/05-ts-render-sound-app.md): releasing a key or mouse button while
// the React menu is open / after the pointer lock was lost must still reach the engine, otherwise +forward
// or +attack stays active after "Resume" (keys.c sends the -command for every key up, whatever key_dest).
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

type Listener = (e: Event) => void;

function stubTarget<T extends object>(extra: T): EventTarget & T {
  return Object.assign(new EventTarget(), extra);
}

function ev(type: string, props: Record<string, unknown>): Event {
  const e = new Event(type, { cancelable: true });
  for (const [k, v] of Object.entries(props)) Object.defineProperty(e, k, { value: v });
  return e;
}

const g = globalThis as Record<string, unknown>;
let saved: Record<string, unknown>;

beforeEach(() => {
  saved = {
    window: g['window'],
    document: g['document'],
    ResizeObserver: g['ResizeObserver'],
    cancelAnimationFrame: g['cancelAnimationFrame'],
  };
  g['cancelAnimationFrame'] = () => {};
  g['window'] = stubTarget({ innerWidth: 800, innerHeight: 600 });
  g['document'] = stubTarget({ pointerLockElement: null as unknown, exitPointerLock() {} });
  g['ResizeObserver'] = class {
    observe() {}
    disconnect() {}
  };
});

afterEach(() => {
  for (const [k, v] of Object.entries(saved)) {
    if (v === undefined) delete g[k];
    else g[k] = v;
  }
});

async function setup() {
  const { GameSession } = await import('@/game/session');
  const { useGameStore } = await import('@/game/store');
  const canvas = stubTarget({ clientWidth: 800, clientHeight: 600, width: 0, height: 0, focus() {} });
  const engine = {
    keyboardEvent: vi.fn((_code: string, _down: boolean, _key?: string) => true),
    mouseButton: vi.fn(),
    mouseMove: vi.fn(),
    wheel: vi.fn(),
    setPointerLocked: vi.fn(),
    setVidSize: vi.fn(),
    disconnect: vi.fn(),
    shutdown: vi.fn(),
    writeConfig: vi.fn(() => ''),
    context: { cls: { key_dest: 0, state: 0 }, keys: { keybindings: [] } },
  };
  const s = new GameSession({
    canvas: canvas as unknown as HTMLCanvasElement,
    source: { kind: 'play', gameId: 'g' },
    onExit: () => {},
  });
  const internals = s as unknown as { engine: unknown; installInput(): void };
  internals.engine = engine;
  internals.installInput();
  return { s, engine, canvas, store: useGameStore };
}

describe('GameSession input', () => {
  it('forwards a key release that happens while the in-game menu is open', async () => {
    const { s, engine, store } = await setup();
    const win = g['window'] as EventTarget;
    win.dispatchEvent(ev('keydown', { code: 'KeyW', key: 'w', repeat: false }));
    store.getState().set({ overlay: 'main' }); // Esc / lost pointer lock opened the menu
    win.dispatchEvent(ev('keyup', { code: 'KeyW', key: 'w', repeat: false }));
    expect(engine.keyboardEvent).toHaveBeenLastCalledWith('KeyW', false, 'w');
    s.dispose();
    store.getState().reset();
  });

  it('does not forward key presses while the menu is open', async () => {
    const { s, engine, store } = await setup();
    store.getState().set({ overlay: 'main' });
    (g['window'] as EventTarget).dispatchEvent(ev('keydown', { code: 'KeyW', key: 'w', repeat: false }));
    expect(engine.keyboardEvent).not.toHaveBeenCalled();
    s.dispose();
    store.getState().reset();
  });

  it('forwards a mouse button release after the pointer lock was lost', async () => {
    const { s, engine, canvas, store } = await setup();
    const doc = g['document'] as { pointerLockElement: unknown } & EventTarget;
    doc.pointerLockElement = canvas;
    canvas.dispatchEvent(ev('mousedown', { button: 0 }));
    expect(engine.mouseButton).toHaveBeenLastCalledWith(0, true);
    doc.pointerLockElement = null; // Esc released the lock while firing
    (g['window'] as EventTarget).dispatchEvent(ev('mouseup', { button: 0 }));
    expect(engine.mouseButton).toHaveBeenLastCalledWith(0, false);
    s.dispose();
    store.getState().reset();
  });

  it('reports a lost WebGL context instead of rendering into it forever', async () => {
    const { s, canvas, store } = await setup();
    const e = ev('webglcontextlost', {});
    canvas.dispatchEvent(e);
    expect(e.defaultPrevented).toBe(true);
    expect(store.getState().fatal).toMatch(/graphics context was lost/);
    s.dispose();
    store.getState().reset();
  });

  it('removes every listener on dispose', async () => {
    const { s, engine } = await setup();
    s.dispose();
    (g['window'] as EventTarget).dispatchEvent(ev('keydown', { code: 'KeyW', key: 'w', repeat: false }));
    expect(engine.keyboardEvent).not.toHaveBeenCalled();
  });
});

export type { Listener };
