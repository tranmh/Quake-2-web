// Engine facade: the boundary between the React shell / tests and the ported client (EngineCommands +
// EngineHost events of the port plan). Mirrors qcommon/common.c Qcommon_Init / Qcommon_Frame for a
// client-only program.
import { PM_FREEZE, fr } from 'q2-shared';
import {
  ClientContext,
  Sys_Milliseconds,
  ca_active,
  type ClientHostEvents,
  type ClientOptions,
} from './client';
import {
  CL_Frame,
  CL_Init,
  CL_PlayDemo,
  CL_Shutdown,
  CL_WriteConfiguration,
  Com_HandleError,
  Qcommon_InitCvars,
} from './cl_main';
import { IN_MouseDelta } from './cl_input';
import { Key_Event, Key_Init, Key_SetBinding, Key_StringToKeynum } from './keys';
import { keyFromKeyboardEvent, keyFromMouseButton, keyFromWheel } from './keymap';
import { Con_CheckResize } from './console';
import { NullSound } from './sound';
import { NullEffects } from './null_effects';
import { NullCinematics } from './cinematic';

export interface ClientEngineOptions extends ClientOptions {
  /**
   * Text of the user's config.cfg (archived cvars and bindings saved by the host). When omitted,
   * `exec config.cfg` goes through loadFile like the original.
   */
  configText?: string;
  /** Command line style arguments (`+set name value`, `+connect host`) */
  args?: string[];
  /** Skip `exec default.cfg` (tests) */
  skipDefaultCfg?: boolean;
}

/** EngineCommands: everything the host can ask the engine to do. */
export interface ClientEngine {
  readonly context: ClientContext;
  /** `connect <address>` */
  connect(address: string): void;
  /** `disconnect` */
  disconnect(): void;
  /** Cbuf_AddText(text + "\n") */
  exec(text: string): void;
  cvarGet(name: string): string | undefined;
  cvarSet(name: string, value: string): void;
  /** `bind <key> <command>` (key name as in keys.c, e.g. "MOUSE1", "w", "SPACE") */
  bind(key: string, command: string): void;
  /** Key_Event with a K_* / ASCII key number */
  keyEvent(key: number, down: boolean): void;
  /** Browser KeyboardEvent.code; returns true if the key was mapped (caller may preventDefault) */
  keyboardEvent(code: string, down: boolean, key?: string): boolean;
  mouseButton(button: number, down: boolean): void;
  wheel(deltaY: number): void;
  /** pointer-lock movementX/movementY */
  mouseMove(dx: number, dy: number): void;
  /** pointer lock state (C: mouseactive) */
  setPointerLocked(locked: boolean): void;
  /** virtual screen size (viddef) */
  setVidSize(width: number, height: number): void;
  /**
   * Runs one Qcommon_Frame. Drive it from requestAnimationFrame. `msec` defaults to the time since the
   * previous call measured with the engine clock.
   */
  frame(msec?: number): void;
  /** Play .dm2 bytes */
  playDemo(name: string, data: Uint8Array): void;
  /** CL_WriteConfiguration text (bindings + archived cvars) */
  writeConfig(): string;
  shutdown(): string;
}

/**
 * Creates and initialises the client (Qcommon_Init order: cmd/cvar, key bindings, default.cfg,
 * config.cfg, +set args, CL_Init, late args).
 */
export async function createClientEngine(opts: ClientEngineOptions): Promise<ClientEngine> {
  const c = new ClientContext({
    ...opts,
    sound: opts.sound ?? new NullSound(),
    effects: opts.effects ?? new NullEffects(),
    cinematics: opts.cinematics ?? new NullCinematics(),
  });
  const host: ClientHostEvents = c.host;
  c.cvars.onChange = (v) => {
    if (v.flags & 1 /* CVAR_ARCHIVE */) host.onConfig?.('cvar', v.name, v.string);
  };

  Sys_Milliseconds(c);
  Key_Init(c);

  // we need to add the early commands twice, because a basedir or cddir needs to be set before execing
  // config files, but we want other parms to override the settings of the config files
  if (opts.args) c.cmd.cbufAddEarlyCommands(opts.args);
  c.cmd.cbufExecute();
  await c.cmd.whenIdle();

  if (!opts.skipDefaultCfg) c.cmd.cbufAddText('exec default.cfg\n');
  if (opts.configText !== undefined) c.cmd.cbufAddText(opts.configText + '\n');
  else c.cmd.cbufAddText('exec config.cfg\n');
  if (opts.args) c.cmd.cbufAddEarlyCommands(opts.args);
  c.cmd.cbufExecute();
  await c.cmd.whenIdle();

  // init commands and vars (host_speeds, developer, ... Netchan_Init)
  Qcommon_InitCvars(c);

  c.sound.attach(c);
  c.fx.attach(c);
  c.cin.attach(c);
  await c.re.init();

  CL_Init(c);
  await c.cmd.whenIdle();
  if (opts.args) {
    c.cmd.cbufAddLateCommands([
      'q2',
      ...opts.args.filter((_, i, a) => !(a[i] === '+set' || a[i - 1] === '+set' || a[i - 2] === '+set')),
    ]);
  }

  let last = Sys_Milliseconds(c);

  const guard = (fn: () => void) => {
    try {
      fn();
    } catch (e) {
      Com_HandleError(c, e);
    }
    poll(c);
  };

  const engine: ClientEngine = {
    context: c,
    connect(address) {
      guard(() => c.cmd.executeString(`connect "${address}"`));
    },
    disconnect() {
      guard(() => c.cmd.executeString('disconnect'));
    },
    exec(text) {
      c.cmd.cbufAddText(text + '\n');
    },
    cvarGet(name) {
      return c.cvars.find(name)?.string;
    },
    cvarSet(name, value) {
      c.cvars.set(name, value);
    },
    bind(key, command) {
      const k = Key_StringToKeynum(key);
      if (k === -1) return;
      Key_SetBinding(c, k, command);
    },
    keyEvent(key, down) {
      guard(() => Key_Event(c, key, down, Sys_Milliseconds(c) >>> 0));
    },
    keyboardEvent(code, down, key) {
      const k = keyFromKeyboardEvent(code, key);
      if (k === null) return false;
      engine.keyEvent(k, down);
      return true;
    },
    mouseButton(button, down) {
      const k = keyFromMouseButton(button);
      if (k !== null) engine.keyEvent(k, down);
    },
    wheel(deltaY) {
      if (!deltaY) return;
      const k = keyFromWheel(deltaY);
      if (k === null) return;
      engine.keyEvent(k, true);
      engine.keyEvent(k, false);
    },
    mouseMove(dx, dy) {
      IN_MouseDelta(c, dx, dy);
    },
    setPointerLocked(locked) {
      c.input.mouseactive = locked;
      c.input.mx_accum = c.input.my_accum = 0;
    },
    setVidSize(width, height) {
      c.viddef.width = width;
      c.viddef.height = height;
      guard(() => Con_CheckResize(c));
    },
    frame(msec) {
      const now = Sys_Milliseconds(c);
      let m = msec ?? now - last;
      last = now;
      // C: common.c Qcommon_Frame
      if (c.cv.fixedtime.value) m = c.cv.fixedtime.value | 0;
      else if (c.cv.timescale.value) {
        m = Math.trunc(fr(m * c.cv.timescale.value));
        if (m < 1) m = 1;
      }
      guard(() => {
        c.cmd.cbufExecute();
        CL_Frame(c, m);
      });
    },
    playDemo(name, data) {
      guard(() => CL_PlayDemo(c, name, data));
    },
    writeConfig() {
      return CL_WriteConfiguration(c);
    },
    shutdown() {
      return CL_Shutdown(c);
    },
  };
  poll(c);
  return engine;
}

/** Emits host events for state that the original only kept in globals. */
function poll(c: ClientContext): void {
  const m = c.main;
  const h = c.host;
  if (m.reportedState !== c.cls.state) {
    m.reportedState = c.cls.state;
    h.onState?.(c.cls.state);
  }
  if (m.reportedKeyDest !== c.cls.key_dest) {
    m.reportedKeyDest = c.cls.key_dest;
    h.onKeyDest?.(c.cls.key_dest);
  }
  const inter =
    c.cls.state === ca_active && !c.cl.attractloop && c.cl.frame.playerstate.pmove.pm_type === PM_FREEZE;
  if (inter !== m.reportedIntermission) {
    m.reportedIntermission = inter;
    h.onIntermission?.(inter);
  }
}
