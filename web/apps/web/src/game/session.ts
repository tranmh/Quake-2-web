// GameSession: everything the /play/[id] page runs outside React. Owns the canvas, the WebGL refresh,
// the client engine (q2-client), sound, input listeners and the requestAnimationFrame loop. React only
// creates/disposes it and reads the coarse UI state it publishes to the Zustand store (store.ts).
import {
  Com_DPrintf,
  Com_Error,
  Com_Printf,
  Key_ClearStates,
  Key_KeynumToString,
  M_ForceMenuOff,
  M_Menu_Main_f,
  NullSound,
  ca_active,
  ca_connected,
  ca_connecting,
  ca_disconnected,
  createClientEngine,
  key_menu,
  type ClientContext,
  type ClientEngine,
  type Sound,
} from 'q2-client';
import type { RefImport } from 'q2-ref';
import { createGLRefresh, type GLRefresh } from 'q2-render-gl';
import { ERR_DROP, PRINT_DEVELOPER } from 'q2-shared';
import { api, errorMessage, type GameInfo } from '@/lib/api';
import { AssetLoader, fetchAssetIndex } from '@/lib/assets';
import { loadConfigText, saveConfigText } from '@/lib/config';
import { DEFAULT_PAKSET } from '@/lib/env';
import type { SettingsBackend } from '@/components/settings/SettingsEditor';
import { useGameStore, type GameUiState } from './store';
import { createJoinTransportFactory } from './transport';
import { createSound } from './sound';

/** Test/debug hook published on window.__q2web (read by the Playwright e2e test). */
export interface Q2WebDebug {
  phase: string;
  connState: number;
  keyDest: number;
  frames: number;
  errors: string[];
  prints: string[];
  exec(text: string): void;
  key(code: string, down: boolean): void;
  /** predicted player origin (cl.predicted_origin) */
  origin(): number[];
}

declare global {
  interface Window {
    __q2web?: Q2WebDebug;
  }
}

const CONFIG_SAVE_DELAY = 1500;

export interface GameSessionOptions {
  canvas: HTMLCanvasElement;
  gameId: string;
  /** called when the user asked to leave (Disconnect / quit) */
  onExit: () => void;
}

export class GameSession {
  private engine: ClientEngine | null = null;
  private refresh: GLRefresh | null = null;
  private sound: Sound | null = null;
  private raf = 0;
  private disposed = false;
  private readonly cleanup: (() => void)[] = [];
  private configTimer: ReturnType<typeof setTimeout> | null = null;
  private configDirty = false;
  private everActive = false;
  private readonly debug: Q2WebDebug;
  game: GameInfo | null = null;

  constructor(private readonly o: GameSessionOptions) {
    this.debug = {
      phase: 'idle',
      connState: 0,
      keyDest: 0,
      frames: 0,
      errors: [],
      prints: [],
      exec: (t) => this.engine?.exec(t),
      key: (code, down) => this.engine?.keyboardEvent(code, down),
      origin: () => Array.from(this.engine?.context.cl.predicted_origin ?? []),
    };
    window.__q2web = this.debug;
  }

  private set(p: Partial<GameUiState>): void {
    if (this.disposed) return;
    useGameStore.getState().set(p);
    if (p.phase) this.debug.phase = p.phase;
  }

  /** Fetches everything, creates renderer + engine, joins the game and starts the frame loop. */
  async start(): Promise<void> {
    const { canvas, gameId } = this.o;
    try {
      this.set({ phase: 'index' });
      const game = await api.getGame(gameId);
      this.game = game;
      this.set({ mapName: game.map });
      const [index, configText, me] = await Promise.all([
        fetchAssetIndex(game.pakset || DEFAULT_PAKSET),
        loadConfigText(),
        api.me().catch(() => null),
      ]);
      if (this.disposed) return;

      const loader = new AssetLoader(index);
      loader.onDownload = (_p, bytes) =>
        this.set({ downloadedBytes: useGameStore.getState().downloadedBytes + bytes });

      this.set({ phase: 'engine' });
      this.resizeCanvas();

      // The renderer needs the engine's cvar/command systems (refimport_t); the ClientContext only
      // exists inside createClientEngine, which hands it over through onContext before re.init().
      let ctx: ClientContext | null = null;
      const need = (): ClientContext => {
        if (!ctx) throw new Error('refimport used before the client context exists');
        return ctx;
      };
      const imports: RefImport = {
        loadFile: loader.loadFile,
        cvarGet: (name, value, flags) => need().cvars.get(name, value, flags),
        cvarSet: (name, value) => {
          need().cvars.set(name, value);
        },
        conPrintf: (level, text) => {
          const c = need();
          if (level === PRINT_DEVELOPER) Com_DPrintf(c, '%s', text);
          else Com_Printf(c, '%s', text);
        },
        sysError: (level, text) => {
          if (level === ERR_DROP) Com_Error(need(), ERR_DROP, '%s', text);
          throw new Error(text);
        },
        addCommand: (name, fn) => {
          const c = need();
          c.cmd.addCommand(name, () => {
            const args: string[] = [];
            for (let i = 0; i < c.cmd.argc(); i++) args.push(c.cmd.argv(i));
            fn(args);
          });
        },
        removeCommand: (name) => need().cmd.removeCommand(name),
      };
      const refresh = createGLRefresh(canvas, imports);
      this.refresh = refresh;

      const { sound: realSound, enabled } = await createSound(canvas);
      const sound: Sound = realSound ?? new NullSound();
      this.sound = sound;
      this.set({ soundEnabled: enabled });

      const join = await api.joinGame(gameId);
      if (this.disposed) return;

      const engine = await createClientEngine({
        onContext: (c) => {
          ctx = c;
        },
        refresh,
        sound,
        transport: createJoinTransportFactory(gameId, join, location.origin),
        loadFile: loader.loadFile,
        width: canvas.width,
        height: canvas.height,
        ...(configText !== undefined ? { configText } : {}),
        host: {
          onState: (s) => this.onState(s),
          onKeyDest: (d) => {
            this.debug.keyDest = d;
            this.set({ keyDest: d });
          },
          onLoading: (info) => {
            if (info.active) {
              const l: GameUiState['loading'] = {};
              if (info.mapname !== undefined) l.mapname = info.mapname;
              if (info.stage !== undefined) l.stage = info.stage;
              if (info.progress !== undefined) l.progress = info.progress;
              this.set({ loading: l, phase: 'loading' });
            } else this.set({ loading: null });
          },
          onLevel: (info) => this.set({ levelName: info.levelname, mapName: info.mapname }),
          onError: (msg) => {
            this.debug.errors.push(msg);
            this.set({ notice: msg.trim() });
          },
          onPrint: (text) => {
            const p = this.debug.prints;
            p.push(text);
            if (p.length > 400) p.splice(0, p.length - 400);
          },
          onConfig: () => this.scheduleConfigSave(),
          onQuit: () => this.o.onExit(),
          onMenu: (action) => {
            if (action === 'main') {
              if (document.pointerLockElement) document.exitPointerLock();
              if (!useGameStore.getState().overlay) this.set({ overlay: 'main' });
            } else if (action === 'off') this.set({ overlay: null });
          },
          getClipboardData: () => null,
        },
      });
      if (this.disposed) {
        engine.shutdown();
        return;
      }
      this.engine = engine;
      engine.setVidSize(canvas.width, canvas.height);
      if (me && (engine.cvarGet('name') ?? 'unnamed') === 'unnamed' && me.displayName) {
        engine.cvarSet('name', me.displayName.slice(0, 15));
      }
      this.installInput();
      this.set({ phase: 'joining' });
      // the address is only a label for the transport factory (cls.servername is limited to 127
      // characters, a ws URL with a ticket may be longer)
      engine.connect(`game-${gameId}`);
      this.loop();
    } catch (e) {
      const msg = errorMessage(e);
      this.debug.errors.push(msg);
      this.set({ phase: 'error', fatal: msg });
    }
  }

  private onState(s: number): void {
    this.debug.connState = s;
    const patch: Partial<GameUiState> = { connState: s };
    if (s === ca_active) {
      patch.phase = 'active';
      patch.loading = null;
      this.everActive = true;
    } else if (s === ca_connected) patch.phase = 'loading';
    else if (s === ca_connecting) patch.phase = 'connecting';
    else if (s === ca_disconnected) patch.phase = this.everActive || this.engine ? 'disconnected' : 'joining';
    this.set(patch);
  }

  // ------------------------------------------------------------------ frame loop

  private loop = (): void => {
    if (this.disposed) return;
    this.raf = requestAnimationFrame(this.loop);
    const engine = this.engine;
    if (!engine) return;
    try {
      engine.frame();
      this.debug.frames++;
    } catch (e) {
      // Com_Error(ERR_FATAL) or a renderer failure: stop the loop, keep the page usable
      const msg = errorMessage(e);
      this.debug.errors.push(msg);
      console.error('engine frame failed', e);
      cancelAnimationFrame(this.raf);
      this.set({ phase: 'error', fatal: msg });
    }
  };

  // ------------------------------------------------------------------ canvas size

  private resizeCanvas(): boolean {
    const c = this.o.canvas;
    const w = Math.max(320, Math.floor(c.clientWidth || window.innerWidth));
    const h = Math.max(240, Math.floor(c.clientHeight || window.innerHeight));
    if (c.width === w && c.height === h) return false;
    c.width = w;
    c.height = h;
    return true;
  }

  // ------------------------------------------------------------------ input

  private listen<K extends keyof WindowEventMap>(
    target: Window | Document | HTMLElement,
    type: K | string,
    fn: (e: never) => void,
    opts?: AddEventListenerOptions,
  ): void {
    const l = fn as unknown as EventListener;
    target.addEventListener(type, l, opts);
    this.cleanup.push(() => target.removeEventListener(type, l, opts));
  }

  private overlayOpen(): boolean {
    return useGameStore.getState().overlay !== null;
  }

  private installInput(): void {
    const { canvas } = this.o;
    const engine = this.engine!;
    const typingInForm = (e: Event): boolean => {
      const t = e.target as HTMLElement | null;
      return !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable);
    };

    const onKey = (down: boolean) => (e: KeyboardEvent) => {
      if (this.overlayOpen() || typingInForm(e)) return;
      // leave browser shortcuts that the game does not use alone (reload, devtools, fullscreen)
      if (e.code === 'F5' || e.code === 'F11' || e.code === 'F12') return;
      if (down && e.repeat && engine.context.cls.key_dest === 0 /* key_game */) {
        e.preventDefault();
        return; // the original ignores autorepeat of game keys; Key_Event handles repeats for the console
      }
      if (engine.keyboardEvent(e.code, down, e.key)) e.preventDefault();
    };
    this.listen(window, 'keydown', onKey(true));
    this.listen(window, 'keyup', onKey(false));

    this.listen(canvas, 'click', () => {
      if (this.overlayOpen()) return;
      if (document.pointerLockElement !== canvas) void this.lockPointer();
    });
    this.listen(canvas, 'mousedown', (e: MouseEvent) => {
      if (document.pointerLockElement !== canvas) return;
      e.preventDefault();
      engine.mouseButton(e.button, true);
    });
    this.listen(window, 'mouseup', (e: MouseEvent) => {
      if (document.pointerLockElement !== canvas) return;
      engine.mouseButton(e.button, false);
    });
    this.listen(canvas, 'contextmenu', (e: MouseEvent) => e.preventDefault());
    this.listen(document, 'mousemove', (e: MouseEvent) => {
      if (document.pointerLockElement !== canvas) return;
      engine.mouseMove(e.movementX, e.movementY);
    });
    this.listen(
      canvas,
      'wheel',
      (e: WheelEvent) => {
        e.preventDefault();
        if (!this.overlayOpen()) engine.wheel(e.deltaY);
      },
      { passive: false },
    );
    this.listen(document, 'pointerlockchange', () => {
      const locked = document.pointerLockElement === canvas;
      engine.setPointerLocked(locked);
      this.set({ pointerLocked: locked });
      // losing the lock (Esc, alt-tab) while playing brings up the in-game menu like ESC in the original
      if (!locked && engine.context.cls.state === ca_active && !this.overlayOpen()) this.openMenu();
    });
    this.listen(window, 'blur', () => {
      // release held keys so movement does not stick (C: Key_ClearStates on app deactivation)
      Key_ClearStates(engine.context);
      this.refresh?.appActivate(false);
    });
    this.listen(window, 'focus', () => this.refresh?.appActivate(true));

    const onResize = () => {
      if (this.resizeCanvas()) engine.setVidSize(canvas.width, canvas.height);
    };
    this.listen(window, 'resize', onResize);
    const ro = new ResizeObserver(onResize);
    ro.observe(canvas);
    this.cleanup.push(() => ro.disconnect());

    this.listen(window, 'beforeunload', () => this.flushConfig());
  }

  async lockPointer(): Promise<void> {
    const { canvas } = this.o;
    try {
      await (canvas.requestPointerLock({ unadjustedMovement: true }) as unknown as Promise<void> | undefined);
    } catch {
      try {
        await (canvas.requestPointerLock() as unknown as Promise<void> | undefined);
      } catch {
        // denied (no user gesture / sandbox): play without lock
      }
    }
    canvas.focus();
  }

  // ------------------------------------------------------------------ menu (React overlay)

  /** ESC / lost pointer lock: M_Menu_Main_f (pauses single player) and show the overlay. */
  openMenu(): void {
    const e = this.engine;
    if (e && e.context.cls.key_dest !== key_menu) M_Menu_Main_f(e.context);
    if (document.pointerLockElement) document.exitPointerLock();
    this.set({ overlay: 'main' });
  }

  /** Resume: M_ForceMenuOff and re-lock (call from a click handler: pointer lock needs a gesture). */
  resume(): void {
    const e = this.engine;
    this.set({ overlay: null });
    if (e) M_ForceMenuOff(e.context);
    void this.lockPointer();
  }

  showOverlay(o: GameUiState['overlay']): void {
    this.set({ overlay: o });
  }

  exec(text: string): void {
    this.engine?.exec(text);
  }

  cvarGet(name: string): string | undefined {
    return this.engine?.cvarGet(name);
  }

  cvarSet(name: string, value: string): void {
    this.engine?.cvarSet(name, value);
  }

  bind(key: string, command: string): void {
    this.engine?.bind(key, command);
  }

  /** Live settings (in-game Options): edits the engine's cvars and bindings directly. */
  settingsBackend(): SettingsBackend {
    return {
      getCvar: (n) => this.engine?.cvarGet(n),
      setCvar: (n, v) => this.engine?.cvarSet(n, v),
      binds: () => {
        const m = new Map<string, string>();
        const kb = this.engine?.context.keys.keybindings;
        if (kb) for (let i = 0; i < 256; i++) if (kb[i]) m.set(Key_KeynumToString(i), kb[i]!);
        return m;
      },
      bind: (k, c) => this.engine?.bind(k, c),
      unbind: (k) => this.engine?.bind(k, ''),
    };
  }

  disconnect(): void {
    this.engine?.disconnect();
    this.o.onExit();
  }

  // ------------------------------------------------------------------ config persistence

  private scheduleConfigSave(): void {
    this.configDirty = true;
    if (this.configTimer) return;
    this.configTimer = setTimeout(() => {
      this.configTimer = null;
      this.flushConfig();
    }, CONFIG_SAVE_DELAY);
  }

  private flushConfig(): void {
    if (!this.configDirty || !this.engine) return;
    const text = this.engine.writeConfig();
    if (!text) return;
    this.configDirty = false;
    void saveConfigText(text);
  }

  // ------------------------------------------------------------------ teardown

  dispose(): void {
    if (this.disposed) return;
    if (this.configTimer) clearTimeout(this.configTimer);
    this.flushConfig();
    this.disposed = true;
    cancelAnimationFrame(this.raf);
    for (const fn of this.cleanup.splice(0)) fn();
    if (document.pointerLockElement) document.exitPointerLock();
    const engine = this.engine;
    this.engine = null;
    if (engine) {
      try {
        engine.disconnect();
        engine.shutdown();
        this.refresh?.shutdown();
      } catch (e) {
        console.warn('engine shutdown', e);
      }
    } else {
      try {
        this.sound?.shutdown();
        this.refresh?.shutdown();
      } catch {
        // ignore
      }
    }
    if (window.__q2web === this.debug) delete window.__q2web;
  }
}
