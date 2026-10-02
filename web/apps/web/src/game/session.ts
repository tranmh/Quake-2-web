// GameSession: everything the game pages run outside React. Owns the canvas, the WebGL refresh, the
// client engine (q2-client), sound, input listeners and the requestAnimationFrame loop. React only
// creates/disposes it and reads the coarse UI state it publishes to the Zustand store (store.ts).
//
// Sources:
//   play    /play/[id]: join a game and play it (keyboard/mouse, pointer lock, React menu, config saves).
//   watch   /watch/[id]: the live first-person view of a bot through the spectate relay (an attractloop
//           stream), with the decision feed for the AI overlay. Passive: no key/mouse/wheel listeners, no
//           pointer lock, no menu (so no single-player pause), the user's config is read but never
//           written, and cl_predict 0 so the view interpolates the bot's frames.
//   replay  /bots/[id]: the run's recorded NN-<map>.dm2 demos one after another (engine.playDemo, no
//           network), the AI overlay rebuilt from the decision trace, speed through timescale.
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
  type ClientHostEvents,
  type Sound,
  type TransportFactory,
} from 'q2-client';
import type { RefImport } from 'q2-ref';
import { createGLRefresh, type GLRefresh } from 'q2-render-gl';
import { ERR_DROP, PRINT_DEVELOPER } from 'q2-shared';
import {
  ApiError,
  api,
  botIsLive,
  errorMessage,
  type BotInfo,
  type BotWatchResponse,
  type GameInfo,
  type User,
} from '@/lib/api';
import { AssetLoader, fetchAssetIndex } from '@/lib/assets';
import { demoArtifacts, type DemoArtifact } from '@/lib/bots';
import { loadConfigText, readLocalConfig, saveConfigText } from '@/lib/config';
import { DEFAULT_PAKSET } from '@/lib/env';
import { useSession } from '@/lib/session';
import type { SettingsBackend } from '@/components/settings/SettingsEditor';
import { useAiStore } from './aiStore';
import { DecisionFeed } from './decisionFeed';
import { ReplayFeed } from './replayFeed';
import { useGameStore, type GameUiState, type ReplayUiState } from './store';
import { createJoinTransportFactory } from './transport';
import { createWatchTransportFactory } from './watch';
import { createSound } from './sound';

/** Test/debug hook published on window.__q2web (read by the Playwright e2e tests). */
export interface Q2WebDebug {
  mode: SessionMode;
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
  /** cl.frame.serverframe (0 before the first frame) */
  serverFrame(): number;
  /** cl.attractloop: the server stream is watch-only (relay, demo) */
  attractloop(): boolean;
  /** a .dm2 is playing */
  demoplaying(): boolean;
  /** replay: index of the level attempt playing (-1 otherwise) */
  replayLevel(): number;
}

declare global {
  interface Window {
    __q2web?: Q2WebDebug;
  }
}

const CONFIG_SAVE_DELAY = 1500;
/** watch tickets live 60 s (auth.TicketTTL): an older one is replaced before the first connect */
const WATCH_TICKET_REUSE_MS = 30_000;
/** the AI overlay follows the video at about 10 Hz (every 6th frame at 60 fps) */
const FRAME_PUBLISH_EVERY = 6;
/** watch: reconnects after the relay connection dropped while the run is still live */
const WATCH_RECONNECTS = 5;
const WATCH_RECONNECT_DELAY = 1000;

export type GameSource =
  | { kind: 'play'; gameId: string }
  | { kind: 'watch'; botId: string }
  | {
      kind: 'replay';
      botId: string;
      episode?: number;
      /** level attempt to start at (index, default 0) */ level?: number;
    };

export type SessionMode = GameSource['kind'];

export interface GameSessionOptions {
  canvas: HTMLCanvasElement;
  source: GameSource;
  /** called when the user asked to leave (Disconnect / quit) */
  onExit: () => void;
}

interface ReplayRun {
  botId: string;
  demos: DemoArtifact[];
  current: number;
  /** playDemo ran for `current` and its demo has not finished yet */
  playing: boolean;
  bytes: Map<number, Promise<Uint8Array>>;
  speed: number;
  paused: boolean;
  /** the next frame after a pause passes an explicit msec (the engine clock kept running) */
  resumed: boolean;
  /** the last level finished playing */
  finished: boolean;
  /** the fatal text of a failed demo download (picking a level again retries and clears it) */
  loadError: string | null;
}

export class GameSession {
  private engine: ClientEngine | null = null;
  private refresh: GLRefresh | null = null;
  private sound: Sound | null = null;
  private raf = 0;
  private disposed = false;
  /** the frame loop was stopped for good (WebGL context lost, a fatal frame error) */
  private disposedLoop = false;
  private readonly cleanup: (() => void)[] = [];
  private configTimer: ReturnType<typeof setTimeout> | null = null;
  private configDirty = false;
  private everActive = false;
  private readonly debug: Q2WebDebug;
  readonly mode: SessionMode;
  game: GameInfo | null = null;
  bot: BotInfo | null = null;
  // watch
  private decisions: DecisionFeed | null = null;
  private runEnded = false;
  private reconnects = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  // replay
  private replay: ReplayRun | null = null;
  private replayFeed: ReplayFeed | null = null;

  constructor(private readonly o: GameSessionOptions) {
    this.mode = o.source.kind;
    this.debug = {
      mode: this.mode,
      phase: 'idle',
      connState: 0,
      keyDest: 0,
      frames: 0,
      errors: [],
      prints: [],
      exec: (t) => this.engine?.exec(t),
      key: (code, down) => this.engine?.keyboardEvent(code, down),
      origin: () => Array.from(this.engine?.context.cl.predicted_origin ?? []),
      serverFrame: () => this.serverFrame(),
      attractloop: () => !!this.engine?.context.cl.attractloop,
      demoplaying: () => !!this.engine?.context.main.demoplaying,
      replayLevel: () => this.replay?.current ?? -1,
    };
    window.__q2web = this.debug;
  }

  private set(p: Partial<GameUiState>): void {
    if (this.disposed) return;
    useGameStore.getState().set(p);
    if (p.phase) this.debug.phase = p.phase;
  }

  /** Fetches everything, creates renderer + engine, connects (or starts the replay) and the frame loop. */
  async start(): Promise<void> {
    const { canvas, source } = this.o;
    try {
      this.set({ phase: 'index' });
      let pakset = DEFAULT_PAKSET;
      let watch: BotWatchResponse | null = null;
      let watchAt = 0;
      if (source.kind === 'play') {
        const game = await api.getGame(source.gameId);
        this.game = game;
        this.set({ mapName: game.map });
        pakset = game.pakset || DEFAULT_PAKSET;
      } else {
        const bot = await api.getBot(source.botId);
        // disposed meanwhile (React strict mode mounts twice, Close right after Play): start nothing, a
        // replay feed would download the whole trace into the next session's overlay
        if (this.disposed) return;
        this.bot = bot;
        this.set({ mapName: bot.level?.map ?? bot.maps[0] ?? '' });
        if (source.kind === 'watch') {
          if (!botIsLive(bot.status)) return this.endRun(bot.status);
          try {
            watch = await api.watchBot(source.botId);
          } catch (e) {
            if (e instanceof ApiError && (e.status === 409 || e.status === 410))
              return this.endRun('finished');
            throw e;
          }
          if (this.disposed) return;
          watchAt = Date.now();
          pakset = watch.pakset || DEFAULT_PAKSET;
          this.startDecisionFeed(source.botId, watch);
        } else {
          this.prepareReplay(source.botId, bot, source.episode ?? 0);
        }
      }
      const [index, configText, me] = await Promise.all([
        fetchAssetIndex(pakset),
        // watch / replay read the player's settings (video, sound) but never write them; anonymous
        // viewers of a public bot only have the local copy
        source.kind === 'play' || useSession.getState().user
          ? loadConfigText()
          : Promise.resolve(readLocalConfig() ?? undefined),
        source.kind === 'play' ? api.me().catch(() => null) : Promise.resolve(null),
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

      let transport: TransportFactory;
      if (source.kind === 'play') {
        const join = await api.joinGame(source.gameId);
        if (this.disposed) return;
        transport = createJoinTransportFactory(source.gameId, join, location.origin);
      } else if (source.kind === 'watch') {
        // the first connect happens below; an old ticket would only fail, so fetch a fresh one then
        const first = Date.now() - watchAt < WATCH_TICKET_REUSE_MS ? watch : null;
        transport = createWatchTransportFactory(source.botId, first, location.origin, {
          onClose: (reason) => this.onWatchClosed(reason),
        });
      } else {
        transport = () => {
          throw new Error('no network during a replay');
        };
      }

      const engine = await createClientEngine({
        onContext: (c) => {
          ctx = c;
        },
        refresh,
        sound,
        transport,
        loadFile: loader.loadFile,
        width: canvas.width,
        height: canvas.height,
        ...(configText !== undefined ? { configText } : {}),
        host: this.hostEvents(),
      });
      if (this.disposed) {
        // dispose() ran while the engine initialized (re.init / S_Init happened after its cleanup)
        try {
          engine.shutdown();
          refresh.shutdown();
        } catch {
          // ignore
        }
        return;
      }
      this.engine = engine;
      engine.setVidSize(canvas.width, canvas.height);
      this.configureEngine(engine, me);
      this.installInput();
      if (source.kind === 'play') {
        this.set({ phase: 'joining' });
        // the address is only a label for the transport factory (cls.servername is limited to 127
        // characters, a ws URL with a ticket may be longer)
        engine.connect(`game-${source.gameId}`);
      } else if (source.kind === 'watch') {
        this.set({ phase: 'joining' });
        engine.connect(`bot-${source.botId}`);
      } else {
        const r = this.replay!;
        const first = source.kind === 'replay' ? (source.level ?? 0) : 0;
        void this.playLevel(Math.min(Math.max(0, first), r.demos.length - 1));
      }
      this.loop();
    } catch (e) {
      const msg = errorMessage(e);
      this.debug.errors.push(msg);
      this.set({ phase: 'error', fatal: msg });
    }
  }

  /** The engine's host callbacks for this session's mode. */
  private hostEvents(): ClientHostEvents {
    const play = this.mode === 'play';
    return {
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
          // the precache's last report can come after the first frame made the client active (a
          // demo's frames follow its precache at once): that does not start another load
          const active = this.engine?.context.cls.state === ca_active;
          this.set(active ? { loading: l } : { loading: l, phase: 'loading' });
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
      // watching never writes the player's config (cl_predict 0 and timescale are not even archived)
      onConfig: () => {
        if (play) this.scheduleConfigSave();
      },
      onQuit: () => this.o.onExit(),
      onMenu: (action) => {
        if (!play) {
          // no menu while watching: the main menu would pause nothing but hide the view
          if (action === 'off') this.set({ overlay: null });
          return;
        }
        if (action === 'main') {
          if (document.pointerLockElement) document.exitPointerLock();
          if (!useGameStore.getState().overlay) this.set({ overlay: 'main' });
        } else if (action === 'off') this.set({ overlay: null });
      },
      getClipboardData: () => null,
    };
  }

  /** Mode-specific cvars once the engine (and the player's config) is up. */
  private configureEngine(engine: ClientEngine, me: User | null): void {
    if (this.mode === 'play') {
      if (me && (engine.cvarGet('name') ?? 'unnamed') === 'unnamed' && me.displayName) {
        engine.cvarSet('name', me.displayName.slice(0, 15));
      }
      return;
    }
    // the viewer sends no input that moves anything: show the recorded / relayed frames as they are,
    // interpolated, instead of predicting the viewer's own (idle) commands
    engine.cvarSet('cl_predict', '0');
    if (this.replay) engine.cvarSet('timescale', String(this.replay.speed));
  }

  private onState(s: number): void {
    this.debug.connState = s;
    const patch: Partial<GameUiState> = { connState: s };
    if (s === ca_active) {
      patch.phase = 'active';
      patch.loading = null;
      this.everActive = true;
      this.reconnects = 0;
    } else if (s === ca_connected) patch.phase = 'loading';
    else if (s === ca_connecting) patch.phase = 'connecting';
    else if (s === ca_disconnected) patch.phase = this.everActive || this.engine ? 'disconnected' : 'joining';
    if (this.runEnded || this.replay?.finished) delete patch.phase;
    this.set(patch);
    // the relay dropped the viewer (svc_disconnect, a timeout): over, or reconnect
    if (s === ca_disconnected && this.mode === 'watch' && this.everActive) this.onWatchClosed('disconnected');
  }

  private serverFrame(): number {
    const c = this.engine?.context;
    if (!c || c.cls.state !== ca_active || !c.cl.frame.valid) return 0;
    return c.cl.frame.serverframe;
  }

  // ------------------------------------------------------------------ watch

  private startDecisionFeed(botId: string, w: BotWatchResponse): void {
    const feed = new DecisionFeed({
      botId,
      origin: location.origin,
      first: w.decisionsTicket ? { url: w.decisionsUrl, ticket: w.decisionsTicket } : null,
      onEnded: (status) => this.endRun(status),
    });
    this.decisions = feed;
    feed.start();
  }

  /** The relay connection closed: a finished run ends the view, a dropped socket reconnects. */
  private onWatchClosed(reason: string): void {
    if (this.disposed || this.runEnded || this.mode !== 'watch') return;
    const botId = this.o.source.kind === 'watch' ? this.o.source.botId : '';
    void api.getBot(botId).then(
      (b) => {
        if (this.disposed || this.runEnded) return;
        this.bot = b;
        if (!botIsLive(b.status)) return this.endRun(b.status);
        const e = this.engine;
        // while connecting CL_CheckForResend opens a new transport (fresh ticket) every 3 s by itself;
        // after the handshake the engine would only notice at cl_timeout, and once dropped it stays so
        if (!e || e.context.cls.state === ca_connecting || this.reconnectTimer) return;
        if (this.reconnects >= WATCH_RECONNECTS) {
          this.set({ phase: 'error', fatal: `Lost the connection to the bot (${reason}).` });
          return;
        }
        this.reconnects++;
        this.reconnectTimer = setTimeout(() => {
          this.reconnectTimer = null;
          if (!this.disposed && !this.runEnded) this.engine?.connect(`bot-${botId}`);
        }, WATCH_RECONNECT_DELAY * this.reconnects);
      },
      (e) => {
        if (e instanceof ApiError && e.status === 404) this.endRun('gone');
      },
    );
  }

  /** The bot's run is over (bye, or it is not live any more): stop the view. */
  private endRun(status: string): void {
    if (this.disposed || this.runEnded) return;
    this.runEnded = true;
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
    // stop CL_CheckForResend from asking for tickets of a run that is gone
    this.engine?.disconnect();
    this.set({ phase: 'ended', runStatus: status, loading: null });
  }

  // ------------------------------------------------------------------ replay

  private prepareReplay(botId: string, bot: BotInfo, episode: number): void {
    const demos = demoArtifacts(bot.artifacts, episode);
    if (!demos.length) throw new Error('This run has no recorded demos to replay.');
    this.replay = {
      botId,
      demos,
      current: -1,
      playing: false,
      bytes: new Map(),
      speed: 1,
      paused: false,
      resumed: false,
      finished: false,
      loadError: null,
    };
    this.publishReplay();
    useAiStore.getState().setAttempt(demos[0]!.attempt);
    const feed = new ReplayFeed({ botId, episode });
    this.replayFeed = feed;
    void feed.start();
  }

  private publishReplay(): void {
    const r = this.replay;
    if (!r) return;
    const state: ReplayUiState = {
      levels: r.demos.map((d) => ({ name: d.name, map: d.map, attempt: d.attempt })),
      current: r.current,
      speed: r.speed,
      paused: r.paused,
      finished: r.finished,
    };
    this.set({ replay: state });
  }

  private demoBytes(i: number): Promise<Uint8Array> {
    const r = this.replay!;
    let p = r.bytes.get(i);
    if (!p) {
      const d = r.demos[i]!;
      p = api
        .fetchArtifact(r.botId, d.name)
        .then((res) => res.arrayBuffer())
        .then((b) => new Uint8Array(b));
      // a failed download may be retried by picking the level again
      p.catch(() => r.bytes.delete(i));
      r.bytes.set(i, p);
    }
    return p;
  }

  /** Plays the replay's level attempt `i` (0-based) from its start. */
  async playLevel(i: number): Promise<void> {
    const r = this.replay;
    const engine = this.engine;
    // nothing more can be drawn once the frame loop stopped (its fatal error stays on screen)
    if (!r || !engine || this.disposed || this.disposedLoop || i < 0 || i >= r.demos.length) return;
    const d = r.demos[i]!;
    r.current = i;
    r.playing = false;
    r.finished = false;
    this.publishReplay();
    const patch: Partial<GameUiState> = {
      phase: 'loading',
      mapName: d.map,
      loading: { mapname: d.map, stage: 'demo' },
    };
    // this is the retry of a failed download: its error panel would hide the demo
    if (r.loadError !== null && useGameStore.getState().fatal === r.loadError) patch.fatal = null;
    r.loadError = null;
    this.set(patch);
    const ai = useAiStore.getState();
    ai.setAttempt(d.attempt);
    ai.showReplay(null, null, []);
    let data: Uint8Array;
    try {
      data = await this.demoBytes(i);
    } catch (e) {
      if (this.disposed || r.current !== i) return;
      r.loadError = `Cannot load ${d.name}: ${errorMessage(e)}`;
      this.set({ phase: 'error', fatal: r.loadError });
      return;
    }
    // another level was picked meanwhile
    if (this.disposed || r.current !== i || this.engine !== engine) return;
    engine.playDemo(`${String(d.attempt).padStart(2, '0')}-${d.map}`, data);
    r.playing = true;
    if (i + 1 < r.demos.length) void this.demoBytes(i + 1).catch(() => {});
  }

  /** Replay speed (timescale; the cvar is not archived). */
  setReplaySpeed(speed: number): void {
    const r = this.replay;
    if (!r || !(speed > 0)) return;
    r.speed = speed;
    this.engine?.cvarSet('timescale', String(speed));
    this.publishReplay();
  }

  setReplayPaused(paused: boolean): void {
    const r = this.replay;
    if (!r || r.paused === paused) return;
    r.paused = paused;
    if (!paused) r.resumed = true;
    this.publishReplay();
  }

  /** The demo of the current level ended (Demo finished. / a drop): play the next one. */
  private onDemoEnded(): void {
    const r = this.replay!;
    if (r.current + 1 < r.demos.length) {
      void this.playLevel(r.current + 1);
      return;
    }
    r.finished = true;
    this.publishReplay();
    this.set({ phase: 'ended', loading: null });
  }

  // ------------------------------------------------------------------ frame loop

  private loop = (): void => {
    if (this.disposed || this.disposedLoop) return;
    this.raf = requestAnimationFrame(this.loop);
    const engine = this.engine;
    if (!engine) return;
    const r = this.replay;
    try {
      if (r?.paused) return;
      if (r?.resumed) {
        r.resumed = false;
        engine.frame(16); // not the whole pause at once
      } else engine.frame();
      this.debug.frames++;
    } catch (e) {
      // Com_Error(ERR_FATAL) or a renderer failure: stop the loop, keep the page usable
      const msg = errorMessage(e);
      this.debug.errors.push(msg);
      console.error('engine frame failed', e);
      cancelAnimationFrame(this.raf);
      this.disposedLoop = true;
      this.set({ phase: 'error', fatal: msg });
      return;
    }
    if (this.mode === 'play') return;
    if (r?.playing && !engine.context.main.demoplaying) {
      r.playing = false;
      this.onDemoEnded();
    }
    if (this.debug.frames % FRAME_PUBLISH_EVERY === 0) this.publishFrame();
  };

  /** Tells the AI overlay which server frame is on screen. */
  private publishFrame(): void {
    const sf = this.serverFrame();
    useAiStore.getState().setServerFrame(sf);
    const r = this.replay;
    const d = r && r.current >= 0 ? r.demos[r.current] : undefined;
    if (d && sf > 0) this.replayFeed?.show(d.attempt, d.map, sf);
  }

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
    // watch / replay: nothing the viewer does reaches the engine (no keys, mouse, wheel, pointer lock)
    if (this.mode === 'play') this.installPlayInput();
    this.installWindowListeners();
  }

  /** Keyboard, mouse, wheel and pointer lock of a played game. */
  private installPlayInput(): void {
    const { canvas } = this.o;
    const engine = this.engine!;
    const typingInForm = (e: Event): boolean => {
      const t = e.target as HTMLElement | null;
      return (
        !!t &&
        (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)
      );
    };

    const onKey = (down: boolean) => (e: KeyboardEvent) => {
      if (!down) {
        // keys.c turns every key up into its -command whatever key_dest is: a key released while the
        // React menu is open (Esc / lost pointer lock) must still stop +forward, +attack, ...
        if (engine.keyboardEvent(e.code, false, e.key) && !this.overlayOpen() && !typingInForm(e))
          e.preventDefault();
        return;
      }
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
      // also after the lock was lost while the button was held (key up events are always delivered)
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
    });
    this.listen(window, 'beforeunload', () => this.flushConfig());
  }

  /** Focus, canvas size and the WebGL context: every mode. */
  private installWindowListeners(): void {
    const { canvas } = this.o;
    const engine = this.engine!;
    this.listen(window, 'blur', () => this.refresh?.appActivate(false));
    this.listen(window, 'focus', () => this.refresh?.appActivate(true));

    const onResize = () => {
      if (this.resizeCanvas()) engine.setVidSize(canvas.width, canvas.height);
    };
    this.listen(window, 'resize', onResize);
    const ro = new ResizeObserver(onResize);
    ro.observe(canvas);
    this.cleanup.push(() => ro.disconnect());

    // The renderer keeps no copy of its GPU objects: after a lost context nothing could be drawn again.
    this.listen(canvas, 'webglcontextlost', (e: Event) => {
      e.preventDefault();
      cancelAnimationFrame(this.raf);
      this.disposedLoop = true;
      if (document.pointerLockElement) document.exitPointerLock();
      this.set({
        phase: 'error',
        fatal: 'The graphics context was lost (GPU reset or too many WebGL pages). Retry to reload the game.',
      });
    });
  }

  async lockPointer(): Promise<void> {
    const { canvas } = this.o;
    if (this.mode !== 'play') return;
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
    if (this.mode !== 'play') return;
    const e = this.engine;
    if (e && e.context.cls.key_dest !== key_menu) M_Menu_Main_f(e.context);
    if (document.pointerLockElement) document.exitPointerLock();
    this.set({ overlay: 'main' });
  }

  /** Resume: M_ForceMenuOff and re-lock (call from a click handler: pointer lock needs a gesture). */
  resume(): void {
    if (this.mode !== 'play') return;
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
    if (this.mode !== 'play') return; // a viewer's settings are never saved
    this.configDirty = true;
    if (this.configTimer) return;
    this.configTimer = setTimeout(() => {
      this.configTimer = null;
      this.flushConfig();
    }, CONFIG_SAVE_DELAY);
  }

  private flushConfig(): void {
    if (this.mode !== 'play' || !this.configDirty || !this.engine) return;
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
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.decisions?.close();
    this.replayFeed?.close();
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
