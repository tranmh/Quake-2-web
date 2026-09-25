// q2-render-gl dev harness: loads the demo pak, renders a map with a noclip fly camera, animated
// lightstyles, sample entities (MD2 models with frame lerp, a sprite, a beam, particles, a dlight) and a
// 2D overlay.
//
// Query parameters:
//   map=demo1              map name (maps/<map>.bsp)
//   pos=x,y,z  ang=p,y,r   initial camera (default: info_player_start)
//   time=<sec>             freeze the refresh time (deterministic frames for tests)
//   test=1                 preserveDrawingBuffer, publish window.__q2 status for Playwright
//   cvar_<name>=<value>    set a renderer cvar before init (e.g. cvar_gl_lightmap=1)
import { Pak, parseBsp } from 'q2-formats';
import { COM_Parse, RF_BEAM, RF_FRAMELERP, RF_GLOW, RF_TRANSLUCENT, atof, parseCursor } from 'q2-shared';
import {
  MAX_LIGHTSTYLES,
  newRefEntity,
  type DLight,
  type LightStyle,
  type ModelHandle,
  type Particle,
  type RefDef,
  type RefEntity,
  type RefImport,
} from 'q2-ref';
import { createGLRefresh } from 'q2-render-gl';

interface Q2Status {
  ready: boolean;
  frames: number;
  errors: string[];
  glErrors: number[];
  log: string[];
  drawCalls: number;
}

declare global {
  interface Window {
    __q2: Q2Status;
  }
}

const status: Q2Status = { ready: false, frames: 0, errors: [], glErrors: [], log: [], drawCalls: 0 };
window.__q2 = status;
window.addEventListener('error', (e) => status.errors.push(String(e.message)));
window.addEventListener('unhandledrejection', (e) => status.errors.push(String(e.reason)));

const params = new URLSearchParams(location.search);
const mapName = params.get('map') ?? 'demo1';
const testMode = params.get('test') === '1';
const fixedTime = params.has('time') ? Number(params.get('time')) : null;

const canvas = document.getElementById('c') as HTMLCanvasElement;
const hud = document.getElementById('hud') as HTMLDivElement;

// ---------------------------------------------------------------- cvars / commands (client side stubs)
interface CvarObj {
  name: string;
  string: string;
  value: number;
  modified: boolean;
}
const cvars = new Map<string, CvarObj>();
for (const [k, v] of params) {
  if (k.startsWith('cvar_')) {
    const name = k.slice(5);
    cvars.set(name, { name, string: v, value: atof(v), modified: true });
  }
}
const commands = new Map<string, (args: string[]) => void>();

const imports: RefImport = {
  loadFile: async (name) => pak.read(name) ?? null,
  cvarGet(name, value, _flags) {
    let c = cvars.get(name);
    if (!c) {
      c = { name, string: value, value: atof(value), modified: true };
      cvars.set(name, c);
    }
    return c;
  },
  cvarSet(name, value) {
    const c = imports.cvarGet(name, value, 0) as CvarObj;
    c.string = value;
    c.value = atof(value);
    c.modified = true;
  },
  conPrintf(level, text) {
    if (level !== 0) return; // developer prints
    status.log.push(text);
    console.log(text.replace(/\n$/, ''));
  },
  sysError(_lvl, text) {
    status.errors.push(text);
    throw new Error(text);
  },
  addCommand(name, fn) {
    commands.set(name, fn);
  },
  removeCommand(name) {
    commands.delete(name);
  },
};
(window as unknown as { q2cmd: (s: string) => void }).q2cmd = (s: string) => {
  const args = s.trim().split(/\s+/);
  const c = commands.get(args[0] ?? '');
  if (c) c(args);
  else if (args.length === 2 && cvars.has(args[0]!)) imports.cvarSet(args[0]!, args[1]!);
};

let pak: Pak;

// ---------------------------------------------------------------- lightstyles (g_spawn.c SP_worldspawn + cl_fx.c)
const STYLES: Record<number, string> = {
  0: 'm', // normal
  1: 'mmnmmommommnonmmonqnmmo', // flicker
  2: 'abcdefghijklmnopqrstuvwxyzyxwvutsrqponmlkjihgfedcba', // slow strong pulse
  3: 'mmmmmaaaaammmmmaaaaaabcdefgabcdefg', // candle
  4: 'mamamamamama', // fast strobe
  5: 'jklmnopqrstuvwxyzyxwvutsrqponmlkj', // gentle pulse
  6: 'nmonqnmomnmomomno', // other flicker
  7: 'mmmaaaabcdefgmmmmaaaammmaamm', // candle 2
  8: 'mmmaaammmaaammmabcdefaaaammmmabcdefmmmaaaa', // candle 3
  9: 'aaaaaaaazzzzzzzz', // slow strobe
  10: 'mmamammmmammamamaaamammma', // fluorescent flicker
  11: 'abcdefghijklmnopqrrqponmlkjihgfedcba', // slow pulse, not fading to black
  63: 'a', // styles 32-62 are assigned by the light program for switchable lights
};

function runLightStyles(timeMs: number, out: LightStyle[]): void {
  const ofs = Math.trunc(timeMs / 100);
  for (let i = 0; i < MAX_LIGHTSTYLES; i++) {
    const s = STYLES[i];
    let v = 1.0;
    if (s) {
      const k = s.length === 1 ? 0 : ofs % s.length;
      v = Math.fround((s.charCodeAt(k) - 97) / 12);
    }
    const ls = out[i]!;
    ls.rgb[0] = ls.rgb[1] = ls.rgb[2] = v;
    ls.white = Math.fround(v * 3);
  }
}

// ---------------------------------------------------------------- entity lump
type EntDict = Record<string, string>;
function parseEntities(text: string): EntDict[] {
  const out: EntDict[] = [];
  const p = parseCursor(text);
  for (;;) {
    const t = COM_Parse(p);
    if (p.pos < 0 || t !== '{') break;
    const e: EntDict = {};
    for (;;) {
      const k = COM_Parse(p);
      if (p.pos < 0 || k === '}') break;
      e[k] = COM_Parse(p);
    }
    out.push(e);
  }
  return out;
}

function vec(s: string | undefined, d: [number, number, number] = [0, 0, 0]): [number, number, number] {
  if (!s) return d;
  const a = s.trim().split(/\s+|,/).map(Number);
  return [a[0] ?? d[0], a[1] ?? d[1], a[2] ?? d[2]];
}

// ---------------------------------------------------------------- main
async function main(): Promise<void> {
  hud.textContent = 'fetching pak0.pak...';
  const resp = await fetch('/pak0.pak');
  if (!resp.ok) throw new Error(`pak0.pak: HTTP ${resp.status}`);
  pak = new Pak(await resp.arrayBuffer(), 'pak0.pak');

  const resize = (): void => {
    const w = Math.max(1, Math.floor(canvas.clientWidth));
    const h = Math.max(1, Math.floor(canvas.clientHeight));
    if (canvas.width !== w || canvas.height !== h) {
      canvas.width = w;
      canvas.height = h;
    }
  };
  resize();
  window.addEventListener('resize', resize);

  const re = createGLRefresh(canvas, imports, { preserveDrawingBuffer: testMode });
  if (!(await re.init())) throw new Error('renderer init failed');

  hud.textContent = `loading maps/${mapName}.bsp...`;
  const bspBytes = pak.read(`maps/${mapName}.bsp`);
  if (!bspBytes) throw new Error(`maps/${mapName}.bsp not in pak`);
  const ents = parseEntities(parseBsp(bspBytes).entityString);
  const world = ents[0] ?? {};

  await re.beginRegistration(mapName);
  await re.registerModel(`maps/${mapName}.bsp`);
  if (world['sky']) {
    const axis = new Float32Array(vec(world['skyaxis']));
    await re.setSky(world['sky'], atof(world['skyrotate'] ?? '0'), axis);
  }

  // brush entities (doors, platforms...) are drawn as entities with inline models
  const entities: RefEntity[] = [];
  for (const e of ents) {
    const m = e['model'];
    if (!m || m[0] !== '*') continue;
    // triggers are SVF_NOCLIENT in the game and never reach the client
    if ((e['classname'] ?? '').startsWith('trigger_')) continue;
    const model = await re.registerModel(m);
    if (!model) continue;
    const ent = newRefEntity();
    ent.model = model;
    const o = vec(e['origin']);
    ent.origin.set(o);
    ent.oldorigin.set(o);
    const ang = atof(e['angle'] ?? '0');
    if (e['classname'] === 'func_rotating') ent.angles[1] = ang;
    entities.push(ent);
  }

  // camera start
  const starts = ents.filter((e) => e['classname'] === 'info_player_start');
  const start = starts.find((e) => !e['targetname']) ?? starts[0] ?? {};
  const camPos = new Float32Array(vec(params.get('pos') ?? undefined, vec(start['origin'], [0, 0, 0])));
  if (!params.get('pos')) camPos[2] = camPos[2]! + 22;
  const camAng = new Float32Array(vec(params.get('ang') ?? undefined, [0, atof(start['angle'] ?? '0'), 0]));

  // sample entities in front of the start position
  const fwd = (d: number, side: number, up: number): [number, number, number] => {
    const y = (camAng[1]! * Math.PI) / 180;
    return [
      camPos[0]! + Math.cos(y) * d - Math.sin(y) * side,
      camPos[1]! + Math.sin(y) * d + Math.cos(y) * side,
      camPos[2]! + up,
    ];
  };
  const modelNames = [
    'models/monsters/soldier/tris.md2',
    'models/monsters/infantry/tris.md2',
    'models/items/healing/medium/tris.md2',
    'models/items/armor/body/tris.md2',
  ];
  const animated: { ent: RefEntity; frames: number }[] = [];
  let k = 0;
  for (const n of modelNames) {
    const model = (await re.registerModel(n)) as ModelHandle | null;
    if (!model) continue;
    const ent = newRefEntity();
    ent.model = model;
    const p = fwd(160, (k - 1.5) * 48, k < 2 ? -24 : -12);
    ent.origin.set(p);
    ent.oldorigin.set(p);
    ent.angles[1] = camAng[1]! + 180;
    ent.flags = k >= 2 ? RF_GLOW : 0;
    entities.push(ent);
    animated.push({ ent, frames: k < 2 ? 40 : 1 });
    k++;
  }
  const sprite = await re.registerModel('sprites/s_explod.sp2');
  let spriteEnt: RefEntity | null = null;
  if (sprite) {
    spriteEnt = newRefEntity();
    spriteEnt.model = sprite;
    spriteEnt.origin.set(fwd(220, 90, 30));
    spriteEnt.flags = RF_TRANSLUCENT;
    spriteEnt.alpha = 0.8;
    entities.push(spriteEnt);
  }
  const beam = newRefEntity();
  beam.flags = RF_BEAM | RF_TRANSLUCENT;
  beam.origin.set(fwd(120, -80, -20));
  beam.oldorigin.set(fwd(260, -40, 40));
  beam.frame = 6; // diameter
  beam.skinnum = 0xd0;
  beam.alpha = 0.3;
  entities.push(beam);

  await re.registerPic('backtile');
  // a small 8-bit image for Draw_StretchRaw (cinematic path) with the game palette
  const rawCols = 64;
  const rawRows = 48;
  const rawData = new Uint8Array(rawCols * rawRows);
  for (let y = 0; y < rawRows; y++)
    for (let x = 0; x < rawCols; x++) rawData[y * rawCols + x] = (x * 4 + (y >> 3) * 16) & 255;
  re.cinematicSetPalette(null);
  await re.registerPic('i_health');
  await re.registerPic('num_0');
  re.endRegistration();

  // refdef
  const lightstyles: LightStyle[] = [];
  for (let i = 0; i < MAX_LIGHTSTYLES; i++) lightstyles.push({ rgb: new Float32Array(3), white: 0 });
  const dlight: DLight = {
    origin: new Float32Array(3),
    color: new Float32Array([1, 0.6, 0.2]),
    intensity: 200,
  };
  const particles: Particle[] = [];
  for (let i = 0; i < 400; i++) particles.push({ origin: new Float32Array(3), color: 0, alpha: 1 });
  const fd: RefDef = {
    x: 0,
    y: 0,
    width: 0,
    height: 0,
    fov_x: 90,
    fov_y: 0,
    vieworg: camPos,
    viewangles: camAng,
    blend: new Float32Array(4),
    time: 0,
    rdflags: 0,
    areabits: null,
    lightstyles,
    num_entities: entities.length,
    entities,
    num_dlights: 1,
    dlights: [dlight],
    num_particles: particles.length,
    particles,
  };

  // ---------------------------------------------------------------- input: noclip fly camera
  const keys = new Set<string>();
  window.addEventListener('keydown', (e) => keys.add(e.code));
  window.addEventListener('keyup', (e) => keys.delete(e.code));
  canvas.addEventListener('click', () => {
    if (!testMode) void canvas.requestPointerLock();
  });
  document.addEventListener('mousemove', (e) => {
    if (document.pointerLockElement !== canvas) return;
    camAng[1] = camAng[1]! - e.movementX * 0.15;
    camAng[0] = Math.max(-89, Math.min(89, camAng[0]! + e.movementY * 0.15));
  });

  const t0 = performance.now();
  let last = t0;
  let fpsFrames = 0;
  let fpsTime = t0;
  let fps = 0;
  const glc = canvas.getContext('webgl2') as WebGL2RenderingContext;

  const frame = (now: number): void => {
    const dt = Math.min(0.1, (now - last) / 1000);
    last = now;
    resize();
    const time = fixedTime ?? (now - t0) / 1000;

    // move
    const speed = (keys.has('ShiftLeft') ? 800 : 300) * dt;
    const yaw = (camAng[1]! * Math.PI) / 180;
    const pitch = (camAng[0]! * Math.PI) / 180;
    const f = [Math.cos(yaw) * Math.cos(pitch), Math.sin(yaw) * Math.cos(pitch), -Math.sin(pitch)];
    const rgt = [Math.sin(yaw), -Math.cos(yaw), 0];
    const mv = (v: number[], s: number): void => {
      camPos[0] = camPos[0]! + v[0]! * s;
      camPos[1] = camPos[1]! + v[1]! * s;
      camPos[2] = camPos[2]! + v[2]! * s;
    };
    if (keys.has('KeyW')) mv(f, speed);
    if (keys.has('KeyS')) mv(f, -speed);
    if (keys.has('KeyD')) mv(rgt, speed);
    if (keys.has('KeyA')) mv(rgt, -speed);
    if (keys.has('Space')) camPos[2] = camPos[2]! + speed;
    if (keys.has('KeyC')) camPos[2] = camPos[2]! - speed;
    if (keys.has('ArrowLeft')) camAng[1] = camAng[1]! + 90 * dt;
    if (keys.has('ArrowRight')) camAng[1] = camAng[1]! - 90 * dt;

    // refdef
    fd.width = canvas.width;
    fd.height = canvas.height;
    fd.fov_x = 90;
    const x = fd.width / Math.tan((fd.fov_x / 360) * Math.PI);
    fd.fov_y = Math.fround((Math.atan(fd.height / x) * 360) / Math.PI);
    fd.time = Math.fround(time);
    runLightStyles(time * 1000, lightstyles);

    // animate entities (10 Hz frames with lerp, like the client)
    const ft = time * 10;
    for (const a of animated) {
      if (a.frames > 1) {
        a.ent.frame = Math.floor(ft) % a.frames;
        a.ent.oldframe = (a.ent.frame + a.frames - 1) % a.frames;
        a.ent.backlerp = Math.fround(1 - (ft - Math.floor(ft)));
        a.ent.flags |= RF_FRAMELERP;
      } else a.ent.angles[1] = (time * 90) % 360;
    }
    if (spriteEnt) spriteEnt.frame = Math.floor(ft);

    // dlight orbiting in front of the camera
    const dp = fwd(96, Math.sin(time * 2) * 64, 0);
    dlight.origin[0] = dp[0];
    dlight.origin[1] = dp[1];
    dlight.origin[2] = dp[2] + Math.cos(time * 3) * 24;

    // particle fountain
    const base = fwd(200, 0, -30);
    for (let i = 0; i < particles.length; i++) {
      const p = particles[i]!;
      const pt = (time * 0.7 + i / particles.length) % 1;
      const a = i * 2.39996;
      const rr = 40 * pt;
      p.origin[0] = base[0] + Math.cos(a) * rr;
      p.origin[1] = base[1] + Math.sin(a) * rr;
      p.origin[2] = base[2] + 160 * pt - 180 * pt * pt;
      p.color = 0xe0 + (i & 7);
      p.alpha = Math.fround(1 - pt);
    }

    re.beginFrame(0);
    re.renderFrame(fd);
    // 2D overlay
    const msg = 'q2-render-gl: ref_gl WebGL2 port';
    for (let i = 0; i < msg.length; i++) re.drawChar(8 + i * 8, 8, msg.charCodeAt(i));
    const alt = 'alt charset';
    for (let i = 0; i < alt.length; i++) re.drawChar(8 + i * 8, 20, alt.charCodeAt(i) | 128);
    re.drawPic(8, 36, 'i_health');
    re.drawPic(40, 36, 'num_0');
    re.drawFill(8, 72, 64, 8, 0xd0);
    re.drawFill(8, 82, 64, 8, 0x70);
    re.drawTileClear(8, 96, 64, 24, 'backtile');
    re.drawStretchPic(80, 36, 48, 24, 'i_health');
    re.drawStretchRaw(8, 124, 96, 72, rawCols, rawRows, rawData);
    re.endFrame();

    if (testMode) {
      const e = glc.getError();
      if (e) status.glErrors.push(e);
    }
    status.frames++;
    status.drawCalls = re.drawCalls();
    fpsFrames++;
    if (now - fpsTime > 500) {
      fps = (fpsFrames * 1000) / (now - fpsTime);
      fpsFrames = 0;
      fpsTime = now;
    }
    if (status.frames % 10 === 1 || !testMode) {
      hud.textContent =
        `${mapName}  ${fps.toFixed(0)} fps  ${status.drawCalls} draws\n` +
        `pos ${camPos[0]!.toFixed(0)} ${camPos[1]!.toFixed(0)} ${camPos[2]!.toFixed(0)}  ` +
        `ang ${camAng[0]!.toFixed(0)} ${camAng[1]!.toFixed(0)}\n` +
        `click: mouse look, WASD/space/c, shift: fast`;
    }
    if (status.frames >= 3) status.ready = true;
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
}

main().catch((e: unknown) => {
  status.errors.push(String(e instanceof Error ? (e.stack ?? e.message) : e));
  hud.textContent = `error: ${String(e)}`;
  console.error(e);
});
