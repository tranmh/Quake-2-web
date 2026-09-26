// Port of client/client.h: client_state_t (cl), client_static_t (cls), frame_t, centity_t, clientinfo_t,
// plus the ClientContext that gathers every C global of the client (cl_main.c/cl_view.c/console.c/...)
// into one instance. Each client file exports functions with their C names taking the context first.
import {
  CMD_BACKUP,
  ERR_DISCONNECT,
  ERR_DROP,
  MAX_CLIENTS,
  MAX_CLIENTWEAPONMODELS,
  MAX_CONFIGSTRINGS,
  MAX_EDICTS,
  MAX_IMAGES,
  MAX_ITEMS,
  MAX_MAP_AREAS,
  MAX_MODELS,
  MAX_MSGLEN,
  MAX_PARSE_ENTITIES,
  MAX_SOUNDS,
  UPDATE_BACKUP,
  CModel,
  EntityState,
  PlayerState,
  UserCmd,
  sprintf,
  type PrintfArg,
} from 'q2-shared';
import { NetChan, SizeBuf, type NetchanContext } from 'q2-protocol';
import { Pmove, type CollisionModel, type CollisionWorld } from 'q2-pmove';
import { newRefEntity, type ImageHandle, type ModelHandle, type RefDef, type Refresh } from 'q2-ref';
import { CmdSystem } from './cmd';
import { CvarSystem, type Cvar } from './cvar';
import type { Effects } from './effects';
import type { Cinematics } from './cinematic';
import type { Sound, SfxHandle } from './sound';
import type { DatagramTransport, TransportFactory } from './transport';
import { ConsoleState, Con_Print } from './console';
import { KeysState } from './keys';
import { ScreenState } from './cl_scrn';
import { ViewState } from './cl_view';
import { InputState } from './cl_input';
import { MainState } from './cl_main';
import { PredState } from './cl_pred';
import { RegistrationCache } from './regcache';
import { EntsState } from './cl_ents';
import { QRand } from 'q2-shared';

// C: client.h connstate_t
export const ca_uninitialized = 0;
export const ca_disconnected = 1; // not talking to a server
export const ca_connecting = 2; // sending request packets to the server
export const ca_connected = 3; // netchan_t established, waiting for svc_serverdata
export const ca_active = 4; // game views should be displayed

// C: client.h dltype_t
export const dl_none = 0;
export const dl_model = 1;
export const dl_sound = 2;
export const dl_skin = 3;
export const dl_single = 4;

// C: client.h keydest_t
export const key_game = 0;
export const key_console = 1;
export const key_message = 2;
export const key_menu = 3;

// C: client.h frame_t
export class Frame {
  valid = false; // cleared if delta parsing was invalid
  serverframe = 0;
  servertime = 0; // server time the message is valid for (in msec)
  deltaframe = 0;
  readonly areabits = new Uint8Array(MAX_MAP_AREAS / 8); // portalarea visibility bits
  readonly playerstate = new PlayerState();
  num_entities = 0;
  parse_entities = 0; // non-masked index into cl_parse_entities array

  copyFrom(o: Frame): this {
    this.valid = o.valid;
    this.serverframe = o.serverframe;
    this.servertime = o.servertime;
    this.deltaframe = o.deltaframe;
    this.areabits.set(o.areabits);
    this.playerstate.copyFrom(o.playerstate);
    this.num_entities = o.num_entities;
    this.parse_entities = o.parse_entities;
    return this;
  }

  clear(): this {
    this.valid = false;
    this.serverframe = 0;
    this.servertime = 0;
    this.deltaframe = 0;
    this.areabits.fill(0);
    this.playerstate.clear();
    this.num_entities = 0;
    this.parse_entities = 0;
    return this;
  }
}

// C: client.h centity_t
export class CEntity {
  readonly baseline = new EntityState(); // delta from this if not from a previous frame
  readonly current = new EntityState();
  readonly prev = new EntityState(); // will always be valid, but might just be a copy of current
  serverframe = 0; // if not current, this ent isn't in the frame
  trailcount = 0; // for diminishing grenade trails
  readonly lerp_origin = new Float32Array(3); // for trails (variable hz)
  fly_stoptime = 0;

  clear(): void {
    this.baseline.clear();
    this.current.clear();
    this.prev.clear();
    this.serverframe = 0;
    this.trailcount = 0;
    this.lerp_origin.fill(0);
    this.fly_stoptime = 0;
  }
}

// C: client.h clientinfo_t
export class ClientInfo {
  name = '';
  cinfo = '';
  skin: ImageHandle | null = null;
  icon: ImageHandle | null = null;
  iconname = '';
  model: ModelHandle | null = null;
  readonly weaponmodel: (ModelHandle | null)[] = new Array<ModelHandle | null>(MAX_CLIENTWEAPONMODELS).fill(
    null,
  );

  copyFrom(o: ClientInfo): this {
    this.name = o.name;
    this.cinfo = o.cinfo;
    this.skin = o.skin;
    this.icon = o.icon;
    this.iconname = o.iconname;
    this.model = o.model;
    for (let i = 0; i < MAX_CLIENTWEAPONMODELS; i++) this.weaponmodel[i] = o.weaponmodel[i]!;
    return this;
  }

  clear(): void {
    this.name = '';
    this.cinfo = '';
    this.skin = null;
    this.icon = null;
    this.iconname = '';
    this.model = null;
    this.weaponmodel.fill(null);
  }
}

function newRefDef(): RefDef {
  return {
    x: 0,
    y: 0,
    width: 0,
    height: 0,
    fov_x: 0,
    fov_y: 0,
    vieworg: new Float32Array(3),
    viewangles: new Float32Array(3),
    blend: new Float32Array(4),
    time: 0,
    rdflags: 0,
    areabits: null,
    lightstyles: [],
    num_entities: 0,
    entities: [],
    num_dlights: 0,
    dlights: [],
    num_particles: 0,
    particles: [],
  };
}

/**
 * C: client.h client_state_t -- wiped completely at every server map change (see reset()).
 * Preallocated arrays are kept across resets and cleared in place.
 */
export class ClientState {
  timeoutcount = 0;

  timedemo_frames = 0;
  timedemo_start = 0;

  refresh_prepped = false; // false if on new level or new ref dll
  sound_prepped = false; // ambient sounds can start
  force_refdef = false; // vid has changed, so we can't use a paused refdef

  parse_entities = 0; // index (not anded off) into cl_parse_entities[]

  readonly cmd = new UserCmd();
  readonly cmds: UserCmd[] = Array.from({ length: CMD_BACKUP }, () => new UserCmd()); // each mesage will send several old cmds
  readonly cmd_time = new Int32Array(CMD_BACKUP); // time sent, for calculating pings
  /** short predicted_origins[CMD_BACKUP][3], flattened */
  readonly predicted_origins = new Int16Array(CMD_BACKUP * 3); // for debug comparing against server

  /** float */
  predicted_step = 0; // for stair up smoothing
  /** unsigned */
  predicted_step_time = 0;

  readonly predicted_origin = new Float32Array(3); // generated by CL_PredictMovement
  readonly predicted_angles = new Float32Array(3);
  readonly prediction_error = new Float32Array(3);

  readonly frame = new Frame(); // received from server
  surpressCount = 0; // number of messages rate supressed
  readonly frames: Frame[] = Array.from({ length: UPDATE_BACKUP }, () => new Frame());

  // the client maintains its own idea of view angles, which are
  // sent to the server each frame.  It is cleared to 0 upon entering each level.
  // the server sends a delta each frame which is added to the locally
  // tracked view angles to account for standing on rotating objects,
  // and teleport direction changes
  readonly viewangles = new Float32Array(3);

  time = 0; // this is the time value that the client is rendering at.  always <= cls.realtime
  /** float */
  lerpfrac = 0; // between oldframe and frame

  readonly refdef: RefDef = newRefDef();

  readonly v_forward = new Float32Array(3); // set when refdef.angles is set
  readonly v_right = new Float32Array(3);
  readonly v_up = new Float32Array(3);

  //
  // transient data from server
  //
  layout = ''; // general 2D overlay (char[1024])
  readonly inventory = new Int32Array(MAX_ITEMS);

  //
  // non-gameserver infornamtion (cinematics are handled by the cin module; fields kept for parity)
  cinematictime = 0; // cls.realtime for first cinematic frame
  cinematicframe = 0;
  readonly cinematicpalette = new Uint8Array(768);
  cinematicpalette_active = false;

  //
  // server state information
  //
  attractloop = false; // running the attract loop, any key will menu
  servercount = 0; // server identification for prespawns
  gamedir = '';
  playernum = 0;

  readonly configstrings: string[] = new Array<string>(MAX_CONFIGSTRINGS).fill('');

  //
  // locally derived information from server state
  //
  readonly model_draw: (ModelHandle | null)[] = new Array<ModelHandle | null>(MAX_MODELS).fill(null);
  readonly model_clip: (CModel | null)[] = new Array<CModel | null>(MAX_MODELS).fill(null);

  readonly sound_precache: (SfxHandle | null)[] = new Array<SfxHandle | null>(MAX_SOUNDS).fill(null);
  readonly image_precache: (ImageHandle | null)[] = new Array<ImageHandle | null>(MAX_IMAGES).fill(null);

  readonly clientinfo: ClientInfo[] = Array.from({ length: MAX_CLIENTS }, () => new ClientInfo());
  /**
   * Configstrings changed while the asynchronous precache / CL_PrepRefresh was running (C prepares the
   * refresh synchronously, so no message is parsed meanwhile); their side effects are applied once the
   * refresh is prepped.
   */
  readonly dirty_configstrings = new Set<number>();
  readonly baseclientinfo = new ClientInfo();

  /** memset(&cl, 0, sizeof(cl)) */
  reset(): void {
    this.timeoutcount = 0;
    this.timedemo_frames = 0;
    this.timedemo_start = 0;
    this.refresh_prepped = false;
    this.sound_prepped = false;
    this.force_refdef = false;
    this.parse_entities = 0;
    this.cmd.clear();
    for (const c of this.cmds) c.clear();
    this.cmd_time.fill(0);
    this.predicted_origins.fill(0);
    this.predicted_step = 0;
    this.predicted_step_time = 0;
    this.predicted_origin.fill(0);
    this.predicted_angles.fill(0);
    this.prediction_error.fill(0);
    this.frame.clear();
    this.surpressCount = 0;
    for (const f of this.frames) f.clear();
    this.viewangles.fill(0);
    this.time = 0;
    this.lerpfrac = 0;
    const r = this.refdef;
    r.x = r.y = r.width = r.height = 0;
    r.fov_x = r.fov_y = 0;
    r.vieworg.fill(0);
    r.viewangles.fill(0);
    r.blend.fill(0);
    r.time = 0;
    r.rdflags = 0;
    r.areabits = null;
    r.num_entities = r.num_dlights = r.num_particles = 0;
    this.v_forward.fill(0);
    this.v_right.fill(0);
    this.v_up.fill(0);
    this.layout = '';
    this.inventory.fill(0);
    this.cinematictime = 0;
    this.cinematicframe = 0;
    this.cinematicpalette.fill(0);
    this.cinematicpalette_active = false;
    this.attractloop = false;
    this.servercount = 0;
    this.gamedir = '';
    this.playernum = 0;
    this.configstrings.fill('');
    this.model_draw.fill(null);
    this.model_clip.fill(null);
    this.sound_precache.fill(null);
    this.image_precache.fill(null);
    for (const ci of this.clientinfo) ci.clear();
    this.baseclientinfo.clear();
    this.dirty_configstrings.clear();
  }
}

/** Network address: the browser client talks to exactly one server through its transport. */
export type NetAdr = string;

/**
 * C: client.h client_static_t -- persistant through an arbitrary number of server connections.
 */
export class ClientStatic {
  state = ca_uninitialized;
  key_dest = key_game;

  framecount = 0;
  realtime = 0; // always increasing, no clamping, etc
  /** float */
  frametime = 0; // seconds since last frame

  // screen rendering information
  /** float */
  disable_screen = 0; // showing loading plaque between levels
  // or changing rendering dlls
  // if time gets > 30 seconds ahead, break it
  disable_servercount = 0; // when we receive a frame and cl.servercount
  // > cls.disable_servercount, clear disable_screen

  // connection information
  servername = ''; // name of server from original connect
  /** float */
  connect_time = 0; // for connection retransmits

  quakePort = 0; // a 16 bit value that allows quake servers
  // to work around address translating routers
  netchan: NetChan<NetAdr>;
  serverProtocol = 0; // in case we are doing some kind of version hack

  challenge = 0; // from the server to use for connecting

  /** Downloads are not supported (assets come over HTTP); always false. */
  download = false;
  downloadtempname = '';
  downloadname = '';
  downloadnumber = 0;
  downloadtype = dl_none;
  downloadpercent = 0;

  // demo recording info must be here, so it isn't cleared on level change
  demorecording = false;
  demowaiting = false; // don't record until a non-delta message is received
  /** demo file bytes collected in memory (FILE *demofile) */
  demofile: Uint8Array[] | null = null;
  demoname = '';

  constructor(netctx: NetchanContext<NetAdr>) {
    this.netchan = new NetChan<NetAdr>(netctx);
  }
}

/** Thrown by Com_Error(ERR_DROP / ERR_DISCONNECT) and caught at the frame boundary (C: longjmp). */
export class DropError extends Error {
  constructor(
    message: string,
    readonly code: number = ERR_DROP,
  ) {
    super(message);
    this.name = 'DropError';
  }
}

/** Loading-plaque / precache progress reported to the host UI. */
export interface LoadingInfo {
  active: boolean;
  mapname?: string;
  /** Short description of the current step (e.g. "models", "sounds", "images", "clients", "sky"). */
  stage?: string;
  /** 0..1 */
  progress?: number;
}

/**
 * Host (React shell / tests) callbacks. All optional. `EngineHost` in the plan.
 */
export interface ClientHostEvents {
  /** connstate_t changed */
  onState?(state: number): void;
  /** keydest_t changed */
  onKeyDest?(dest: number): void;
  onLoading?(info: LoadingInfo): void;
  /** entered a level (after CL_PrepRefresh) */
  onLevel?(info: { mapname: string; levelname: string }): void;
  onIntermission?(active: boolean): void;
  /** Com_Error(ERR_DROP) text */
  onError?(message: string): void;
  /** every Com_Printf line fragment (Latin-1 byte string, high bit = alternate colour) */
  onPrint?(text: string): void;
  /** archived cvar or binding changed (for settings persistence) */
  onConfig?(kind: 'cvar' | 'bind', name: string, value: string): void;
  /** `quit` command */
  onQuit?(): void;
  /** menu.c replacement: the React UI owns menus. */
  onMenu?(action: 'main' | 'off' | 'key', key?: number): void;
  /** demo recording finished (`stop`): the .dm2 bytes */
  onDemoRecorded?(name: string, data: Uint8Array): void;
  /** clipboard for console paste (Sys_GetClipboardData) */
  getClipboardData?(): string | null;
}

/** The client cvars (C globals `cvar_t *...` of cl_main.c, cl_view.c, cl_scrn.c, console.c, cl_input.c ...). */
export interface ClientCvars {
  // cl_main.c
  freelook: Cvar;
  adr: Cvar[];
  cl_stereo_separation: Cvar;
  cl_stereo: Cvar;
  rcon_client_password: Cvar;
  rcon_address: Cvar;
  cl_noskins: Cvar;
  cl_autoskins: Cvar;
  cl_footsteps: Cvar;
  cl_timeout: Cvar;
  cl_predict: Cvar;
  cl_maxfps: Cvar;
  cl_gun: Cvar;
  cl_add_particles: Cvar;
  cl_add_lights: Cvar;
  cl_add_entities: Cvar;
  cl_add_blend: Cvar;
  cl_shownet: Cvar;
  cl_showmiss: Cvar;
  cl_showclamp: Cvar;
  cl_paused: Cvar;
  cl_timedemo: Cvar;
  lookspring: Cvar;
  lookstrafe: Cvar;
  sensitivity: Cvar;
  m_pitch: Cvar;
  m_yaw: Cvar;
  m_forward: Cvar;
  m_side: Cvar;
  cl_lightlevel: Cvar;
  info_password: Cvar;
  info_spectator: Cvar;
  name: Cvar;
  skin: Cvar;
  rate: Cvar;
  fov: Cvar;
  msg: Cvar;
  hand: Cvar;
  gender: Cvar;
  gender_auto: Cvar;
  cl_vwep: Cvar;
  cl_upspeed: Cvar;
  cl_forwardspeed: Cvar;
  cl_sidespeed: Cvar;
  cl_yawspeed: Cvar;
  cl_pitchspeed: Cvar;
  cl_run: Cvar;
  cl_anglespeedkey: Cvar;
  // cl_input.c
  cl_nodelta: Cvar;
  // in_*.c (IN_Init of the platform mouse code)
  in_mouse: Cvar;
  m_filter: Cvar;
  // common.c
  developer: Cvar;
  host_speeds: Cvar;
  showtrace: Cvar;
  timescale: Cvar;
  fixedtime: Cvar;
  // net_chan.c
  showpackets: Cvar;
  showdrop: Cvar;
  qport: Cvar;
  // cl_view.c
  crosshair: Cvar;
  cl_testblend: Cvar;
  cl_testparticles: Cvar;
  cl_testentities: Cvar;
  cl_testlights: Cvar;
  cl_stats: Cvar;
  // cl_scrn.c
  scr_viewsize: Cvar;
  scr_conspeed: Cvar;
  scr_showturtle: Cvar;
  scr_showpause: Cvar;
  scr_centertime: Cvar;
  scr_printspeed: Cvar;
  scr_netgraph: Cvar;
  scr_timegraph: Cvar;
  scr_debuggraph: Cvar;
  scr_graphheight: Cvar;
  scr_graphscale: Cvar;
  scr_graphshift: Cvar;
  scr_drawall: Cvar;
  // console.c
  con_notifytime: Cvar;
  // vid (vid_menu / vid_*.c)
  vid_fullscreen: Cvar;
  vid_gamma: Cvar;
}

/** Engine construction options (see engine.ts createClientEngine). */
export interface ClientOptions {
  refresh: Refresh;
  transport: TransportFactory;
  /** FS_LoadFile: async; null when the file does not exist */
  loadFile: (name: string) => Promise<Uint8Array | null>;
  sound?: Sound;
  effects?: Effects;
  cinematics?: Cinematics;
  host?: ClientHostEvents;
  /** Sys_Milliseconds (default: performance.now based) */
  milliseconds?: () => number;
  /** viddef_t (virtual screen size in pixels); updated with setVidSize() */
  width?: number;
  height?: number;
}

/**
 * All client globals of the original, gathered in one instance. Subsystem state that is private to one
 * C file lives in that file's state class (con, keys, scr, view, input, main).
 */
export class ClientContext {
  readonly cvars = new CvarSystem();
  readonly cmd = new CmdSystem(this.cvars);
  readonly cls: ClientStatic;
  readonly cl = new ClientState();
  readonly cv = {} as ClientCvars;

  // C: cl_main.c cl_entities / cl_parse_entities
  readonly cl_entities: CEntity[] = Array.from({ length: MAX_EDICTS }, () => new CEntity());
  readonly cl_parse_entities: EntityState[] = Array.from(
    { length: MAX_PARSE_ENTITIES },
    () => new EntityState(),
  );

  // C: net_message / net_from
  readonly net_message = new SizeBuf(MAX_MSGLEN);
  net_from: NetAdr = '';

  // subsystems (state of individual C files)
  readonly con = new ConsoleState();
  readonly keys = new KeysState();
  readonly scr = new ScreenState();
  readonly view = new ViewState();
  readonly input = new InputState();
  readonly main = new MainState();
  /** cl_pred.c scratch (preallocated pmove_t / traces) */
  readonly pred = new PredState();
  readonly ents = new EntsState();
  /** C: glibc rand() used by client code (cl_ents.c beams/trap, cl_fx.c, cl_tent.c ...); one shared stream */
  readonly rand = new QRand();

  /** registration handle cache (see regcache.ts) */
  readonly regcache = new RegistrationCache();
  /**
   * Bumped by CL_ClearState (every map change / disconnect). Async flows (precache, CL_PrepRefresh,
   * CL_LoadClientinfo) capture it before awaiting and abandon their work if it changed.
   */
  clearGeneration = 0;

  // C: viddef
  readonly viddef = { width: 640, height: 480 };

  // collision map (C: cmodel.c globals, loaded by CM_LoadMap in CL_RequestNextDownload / CL_Precache_f)
  cmModel: CollisionModel | null = null;
  cm: CollisionWorld | null = null;
  /** C: pmove.c globals for client prediction */
  readonly pmove = new Pmove();

  // interfaces
  readonly re: Refresh;
  readonly sound: Sound;
  readonly fx: Effects;
  readonly cin: Cinematics;
  readonly host: ClientHostEvents;
  readonly transportFactory: TransportFactory;
  transport: DatagramTransport | null = null;
  readonly loadFile: (name: string) => Promise<Uint8Array | null>;
  readonly milliseconds: () => number;
  /** C: common.c curtime (Sys_Milliseconds at the start of the frame) */
  curtime = 0;

  constructor(opts: ClientOptions & { sound: Sound; effects: Effects; cinematics: Cinematics }) {
    this.re = opts.refresh;
    this.sound = opts.sound;
    this.fx = opts.effects;
    this.cin = opts.cinematics;
    this.host = opts.host ?? {};
    this.transportFactory = opts.transport;
    this.loadFile = opts.loadFile;
    const t0 = typeof performance !== 'undefined' ? performance : Date;
    this.milliseconds = opts.milliseconds ?? (() => Math.floor(t0.now()));
    if (opts.width) this.viddef.width = opts.width;
    if (opts.height) this.viddef.height = opts.height;
    this.cls = new ClientStatic({
      sendPacket: (_sock, data) => this.transport?.send(data),
      curtime: () => this.curtime,
      qport: 0,
      print: (s) => Com_Printf(this, '%s', s),
      adrToString: (a) => a,
    });
    this.cmd.loadFile = this.loadFile;
    this.cmd.print = (s) => Com_Printf(this, '%s', s);
    this.cvars.print = (s) => Com_Printf(this, '%s', s);
  }
}

// ---------------------------------------------------------------------------------------------------
// qcommon/common.c pieces used by the client

// C: common.c:130 Com_Printf -- prints to the console (and the host UI)
export function Com_Printf(c: ClientContext, fmt: string, ...args: PrintfArg[]): void {
  const msg = sprintf(fmt, ...args);
  c.host.onPrint?.(msg);
  c.main.printHook?.(msg);
  Con_Print(c, msg);
}

// C: common.c:176 Com_DPrintf -- a Com_Printf that only shows up if the "developer" cvar is set
export function Com_DPrintf(c: ClientContext, fmt: string, ...args: PrintfArg[]): void {
  if (!c.cv.developer || !c.cv.developer.value) return; // don't confuse non-developers with techie stuff...
  Com_Printf(c, fmt, ...args);
}

// C: common.c:195 Com_Error -- ERR_DROP / ERR_DISCONNECT unwind to the frame boundary
export function Com_Error(_c: ClientContext, code: number, fmt: string, ...args: PrintfArg[]): never {
  const msg = sprintf(fmt, ...args);
  throw new DropError(msg, code === ERR_DISCONNECT ? ERR_DISCONNECT : ERR_DROP);
}

// C: sys_*.c Sys_Milliseconds
// (like the linux version it also updates the global `curtime` read by net_chan.c)
export function Sys_Milliseconds(c: ClientContext): number {
  c.curtime = c.milliseconds() | 0;
  return c.curtime;
}

/** Fresh refdef entity helper re-exported for convenience. */
export { newRefEntity };
