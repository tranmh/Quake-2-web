// Port of client/ref.h: the interface between the client and the refresh (renderer).
// The client fills RefDef each frame; the renderer implements Refresh (refexport_t).

export const MAX_DLIGHTS = 32;
export const MAX_ENTITIES = 128;
export const MAX_PARTICLES = 4096;
export const MAX_LIGHTSTYLES = 256;
export const POWERSUIT_SCALE = 4.0;
export const SHELL_RED_COLOR = 0xf2;
export const SHELL_GREEN_COLOR = 0xd0;
export const SHELL_BLUE_COLOR = 0xf3;
export const SHELL_RG_COLOR = 0xdc;
export const SHELL_RB_COLOR = 0x68;
export const SHELL_BG_COLOR = 0x78;
export const SHELL_DOUBLE_COLOR = 0xdf;
export const SHELL_HALF_DAM_COLOR = 0x90;
export const SHELL_CYAN_COLOR = 0x72;
export const SHELL_WHITE_COLOR = 0xd7;
export const API_VERSION = 3;

/** Opaque handle to a renderer model (struct model_s). */
export interface ModelHandle {
  readonly __model: unique symbol;
}
/** Opaque handle to a renderer image (struct image_s). */
export interface ImageHandle {
  readonly __image: unique symbol;
}

/** C: client/ref.h entity_t. Float fields hold float32 values. */
export interface RefEntity {
  model: ModelHandle | null;
  angles: Float32Array; // [3]
  origin: Float32Array; // [3] also RF_BEAM "from"
  frame: number; // also RF_BEAM diameter
  oldorigin: Float32Array; // [3] also RF_BEAM "to"
  oldframe: number;
  backlerp: number; // 0.0 = current, 1.0 = old
  skinnum: number; // also RF_BEAM palette index
  lightstyle: number;
  alpha: number; // ignored unless RF_TRANSLUCENT
  skin: ImageHandle | null; // null for inline skin
  flags: number;
}

export function newRefEntity(): RefEntity {
  return {
    model: null,
    angles: new Float32Array(3),
    origin: new Float32Array(3),
    frame: 0,
    oldorigin: new Float32Array(3),
    oldframe: 0,
    backlerp: 0,
    skinnum: 0,
    lightstyle: 0,
    alpha: 0,
    skin: null,
    flags: 0,
  };
}

/** C: dlight_t */
export interface DLight {
  origin: Float32Array; // [3]
  color: Float32Array; // [3]
  intensity: number;
}

/** C: particle_t */
export interface Particle {
  origin: Float32Array; // [3]
  color: number; // palette index
  alpha: number;
}

/** C: lightstyle_t */
export interface LightStyle {
  rgb: Float32Array; // [3] 0.0 - 2.0
  white: number; // highest of rgb
}

/** C: refdef_t. Arrays are preallocated by the client and reused every frame. */
export interface RefDef {
  x: number;
  y: number;
  width: number;
  height: number;
  fov_x: number;
  fov_y: number;
  vieworg: Float32Array; // [3]
  viewangles: Float32Array; // [3]
  blend: Float32Array; // [4] rgba 0-1
  time: number; // seconds, used to auto animate
  rdflags: number; // RDF_*
  areabits: Uint8Array | null; // only areas with set bits are drawn
  lightstyles: LightStyle[]; // [MAX_LIGHTSTYLES]
  num_entities: number;
  entities: RefEntity[];
  num_dlights: number;
  dlights: DLight[];
  num_particles: number;
  particles: Particle[];
}

/**
 * C: refexport_t. Registration may be asynchronous in the browser (assets are
 * fetched over HTTP); the client awaits endRegistration's preceding promises
 * exactly where the C code would block on disk.
 */
export interface Refresh {
  init(): Promise<boolean>;
  shutdown(): void;

  beginRegistration(map: string): Promise<void>;
  registerModel(name: string): Promise<ModelHandle | null>;
  registerSkin(name: string): Promise<ImageHandle | null>;
  registerPic(name: string): Promise<ImageHandle | null>;
  setSky(name: string, rotate: number, axis: Float32Array): Promise<void>;
  endRegistration(): void;

  renderFrame(fd: RefDef): void;

  /** Returns [0,0] if not found (pic must have been registered before for exact sizes). */
  drawGetPicSize(name: string): [number, number];
  drawPic(x: number, y: number, name: string): void;
  drawStretchPic(x: number, y: number, w: number, h: number, name: string): void;
  drawChar(x: number, y: number, c: number): void;
  drawTileClear(x: number, y: number, w: number, h: number, name: string): void;
  drawFill(x: number, y: number, w: number, h: number, c: number): void;
  drawFadeScreen(): void;
  drawStretchRaw(x: number, y: number, w: number, h: number, cols: number, rows: number, data: Uint8Array): void;

  /** null = game palette. 768 bytes RGB. */
  cinematicSetPalette(palette: Uint8Array | null): void;
  beginFrame(cameraSeparation: number): void;
  endFrame(): void;
  appActivate(activate: boolean): void;
}

/**
 * C: refimport_t, minus the parts the browser replaces (window/video modes).
 * File loading is asynchronous; `loadFile` returns null when the file does not exist.
 */
export interface RefImport {
  loadFile(name: string): Promise<Uint8Array | null>;
  cvarGet(name: string, value: string, flags: number): { string: string; value: number; modified: boolean };
  cvarSet(name: string, value: string): void;
  conPrintf(printLevel: number, text: string): void;
  sysError(errLevel: number, text: string): never;
  addCommand(name: string, fn: (args: string[]) => void): void;
  removeCommand(name: string): void;
}
