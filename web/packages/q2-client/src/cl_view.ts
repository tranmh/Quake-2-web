// Port of client/cl_view.c -- player rendering positioning.
import {
  CS_IMAGES,
  CS_MODELS,
  CS_NAME,
  CS_PLAYERSKINS,
  CS_SKY,
  CS_SKYAXIS,
  CS_SKYROTATE,
  CVAR_ARCHIVE,
  ERR_DROP,
  MAX_CLIENTS,
  MAX_CLIENTWEAPONMODELS,
  MAX_IMAGES,
  MAX_MODELS,
  YAW,
  atof,
  cInt,
  fr,
} from 'q2-shared';
import {
  MAX_DLIGHTS,
  MAX_ENTITIES,
  MAX_LIGHTSTYLES,
  MAX_PARTICLES,
  newRefEntity,
  type DLight,
  type LightStyle,
  type ModelHandle,
  type Particle,
  type RefEntity,
} from 'q2-ref';
import { ca_active, Com_Error, Com_Printf, Sys_Milliseconds, type ClientContext } from './client';
import { CL_AddEntities } from './cl_ents';
import { CL_ConfigStringChanged, CL_LoadClientinfo } from './cl_parse';
import { SCR_AddDirtyPoint, SCR_TouchPics, SCR_UpdateScreen } from './cl_scrn';
import { Con_ClearNotify } from './console';
import { RegisterModel, RegisterPic } from './regcache';

const BLANK_LINE = '                                     \r';

/** C: cl_view.c globals */
export class ViewState {
  // development tools for weapons
  gun_frame = 0;
  gun_model: ModelHandle | null = null;
  /** sequence number of the last gun_model request (async registration) */
  gun_model_seq = 0;

  r_numdlights = 0;
  readonly r_dlights: DLight[] = Array.from({ length: MAX_DLIGHTS }, () => ({
    origin: new Float32Array(3),
    color: new Float32Array(3),
    intensity: 0,
  }));

  r_numentities = 0;
  readonly r_entities: RefEntity[] = Array.from({ length: MAX_ENTITIES }, () => newRefEntity());

  r_numparticles = 0;
  readonly r_particles: Particle[] = Array.from({ length: MAX_PARTICLES }, () => ({
    origin: new Float32Array(3),
    color: 0,
    alpha: 0,
  }));

  readonly r_lightstyles: LightStyle[] = Array.from({ length: MAX_LIGHTSTYLES }, () => ({
    rgb: new Float32Array(3),
    white: 0,
  }));

  readonly cl_weaponmodels: string[] = new Array<string>(MAX_CLIENTWEAPONMODELS).fill('');
  num_cl_weaponmodels = 0;

  /** in-flight CL_PrepRefresh */
  prepping: Promise<void> | null = null;
  /** c.clearGeneration the in-flight prep belongs to */
  prepping_generation = -1;

  /** identity ordinals standing in for pointer values in entitycmpfnc */
  readonly handleIds = new Map<object, number>();
  readonly sortScratch: RefEntity[] = [];
}

function copyRefEntity(dst: RefEntity, src: RefEntity): void {
  dst.model = src.model;
  dst.angles.set(src.angles);
  dst.origin.set(src.origin);
  dst.frame = src.frame;
  dst.oldorigin.set(src.oldorigin);
  dst.oldframe = src.oldframe;
  dst.backlerp = src.backlerp;
  dst.skinnum = src.skinnum;
  dst.lightstyle = src.lightstyle;
  dst.alpha = src.alpha;
  dst.skin = src.skin;
  dst.flags = src.flags;
}

function clearRefEntity(e: RefEntity): void {
  e.model = null;
  e.angles.fill(0);
  e.origin.fill(0);
  e.frame = 0;
  e.oldorigin.fill(0);
  e.oldframe = 0;
  e.backlerp = 0;
  e.skinnum = 0;
  e.lightstyle = 0;
  e.alpha = 0;
  e.skin = null;
  e.flags = 0;
}

// C: cl_view.c:63 V_ClearScene -- specifies the model that will be used as the world
export function V_ClearScene(c: ClientContext): void {
  const v = c.view;
  v.r_numdlights = 0;
  v.r_numentities = 0;
  v.r_numparticles = 0;
}

// C: cl_view.c:77 V_AddEntity -- the entity is copied (callers reuse a scratch entity)
export function V_AddEntity(c: ClientContext, ent: RefEntity): void {
  const v = c.view;
  if (v.r_numentities >= MAX_ENTITIES) return;
  copyRefEntity(v.r_entities[v.r_numentities++]!, ent);
}

// C: cl_view.c:91 V_AddParticle
export function V_AddParticle(c: ClientContext, org: ArrayLike<number>, color: number, alpha: number): void {
  const v = c.view;
  if (v.r_numparticles >= MAX_PARTICLES) return;
  const p = v.r_particles[v.r_numparticles++]!;
  p.origin[0] = org[0]!;
  p.origin[1] = org[1]!;
  p.origin[2] = org[2]!;
  p.color = color;
  p.alpha = fr(alpha);
}

// C: cl_view.c:109 V_AddLight
export function V_AddLight(
  c: ClientContext,
  org: ArrayLike<number>,
  intensity: number,
  r: number,
  g: number,
  b: number,
): void {
  const v = c.view;
  if (v.r_numdlights >= MAX_DLIGHTS) return;
  const dl = v.r_dlights[v.r_numdlights++]!;
  dl.origin[0] = org[0]!;
  dl.origin[1] = org[1]!;
  dl.origin[2] = org[2]!;
  dl.intensity = fr(intensity);
  dl.color[0] = r;
  dl.color[1] = g;
  dl.color[2] = b;
}

// C: cl_view.c:130 V_AddLightStyle
export function V_AddLightStyle(c: ClientContext, style: number, r: number, g: number, b: number): void {
  if (style < 0 || style > MAX_LIGHTSTYLES) Com_Error(c, ERR_DROP, 'Bad light style %i', style);
  // (sic) style == MAX_LIGHTSTYLES passes the check and writes past the array in C; ignored here
  const ls = c.view.r_lightstyles[style];
  if (!ls) return;
  r = fr(r);
  g = fr(g);
  b = fr(b);
  ls.white = fr(fr(r + g) + b);
  ls.rgb[0] = r;
  ls.rgb[1] = g;
  ls.rgb[2] = b;
}

// C: cl_view.c:151 V_TestParticles -- if cl_testparticles is set, create 4096 particles in the view
export function V_TestParticles(c: ClientContext): void {
  const v = c.view;
  const cl = c.cl;
  v.r_numparticles = MAX_PARTICLES;
  for (let i = 0; i < v.r_numparticles; i++) {
    const d = fr(i * 0.25);
    const r = fr(4 * ((i & 7) - 3.5));
    const u = fr(4 * (((i >> 3) & 7) - 3.5));
    const p = v.r_particles[i]!;

    for (let j = 0; j < 3; j++)
      p.origin[j] = fr(
        fr(fr(cl.refdef.vieworg[j]! + fr(cl.v_forward[j]! * d)) + fr(cl.v_right[j]! * r)) +
          fr(cl.v_up[j]! * u),
      );

    p.color = 8;
    p.alpha = c.cv.cl_testparticles.value;
  }
}

// C: cl_view.c:181 V_TestEntities -- if cl_testentities is set, create 32 player models
export function V_TestEntities(c: ClientContext): void {
  const v = c.view;
  const cl = c.cl;
  v.r_numentities = 32;
  for (const e of v.r_entities) clearRefEntity(e);

  for (let i = 0; i < v.r_numentities; i++) {
    const ent = v.r_entities[i]!;

    const r = fr(64 * ((i % 4) - 1.5));
    const f = fr(64 * ((i / 4) | 0) + 128);

    for (let j = 0; j < 3; j++)
      ent.origin[j] = fr(fr(cl.refdef.vieworg[j]! + fr(cl.v_forward[j]! * f)) + fr(cl.v_right[j]! * r));

    ent.model = cl.baseclientinfo.model;
    ent.skin = cl.baseclientinfo.skin;
  }
}

// C: cl_view.c:213 V_TestLights -- if cl_testlights is set, create 32 lights models
export function V_TestLights(c: ClientContext): void {
  const v = c.view;
  const cl = c.cl;
  v.r_numdlights = 32;
  for (const dl of v.r_dlights) {
    dl.origin.fill(0);
    dl.color.fill(0);
    dl.intensity = 0;
  }

  for (let i = 0; i < v.r_numdlights; i++) {
    const dl = v.r_dlights[i]!;

    const r = fr(64 * ((i % 4) - 1.5));
    const f = fr(64 * ((i / 4) | 0) + 128);

    for (let j = 0; j < 3; j++)
      dl.origin[j] = fr(fr(cl.refdef.vieworg[j]! + fr(cl.v_forward[j]! * f)) + fr(cl.v_right[j]! * r));
    dl.color[0] = ((i % 6) + 1) & 1;
    dl.color[1] = (((i % 6) + 1) & 2) >> 1;
    dl.color[2] = (((i % 6) + 1) & 4) >> 2;
    dl.intensity = 200;
  }
}

/** sscanf(s, "%f %f %f", ...) -- fields that fail to parse are left 0 (C leaves them uninitialized). */
function scanFloats3(s: string, out: Float32Array): void {
  out.fill(0);
  const re = /^\s*([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)/;
  let rest = s;
  for (let i = 0; i < 3; i++) {
    const m = re.exec(rest);
    if (!m) return;
    out[i] = atof(m[1]!);
    rest = rest.slice(m[0].length);
  }
}

// C: cl_view.c:248 CL_PrepRefresh -- call before entering a new level, or after changing dlls.
// Registration is asynchronous; every step is awaited in the original order. A concurrent call returns
// the in-flight promise. If the client state is cleared while loading (disconnect / new map), the
// sequence is abandoned without setting refresh_prepped.
export function CL_PrepRefresh(c: ClientContext): Promise<void> {
  // an in-flight prep of a previous level (cleared state) only returns early once it resumes: chain a
  // fresh prep after it instead of handing it out (the caller would send "begin" with nothing prepped)
  if (c.view.prepping && c.view.prepping_generation === c.clearGeneration) return c.view.prepping;
  const previous = c.view.prepping;
  const gen = c.clearGeneration;
  const start = (): Promise<void> => {
    if (c.clearGeneration !== gen) return Promise.resolve();
    if (!c.cl.configstrings[CS_MODELS + 1]![0]) return Promise.resolve(); // no map loaded
    return prepRefresh(c);
  };
  if (!previous && !c.cl.configstrings[CS_MODELS + 1]![0]) return Promise.resolve(); // no map loaded
  const p = (previous ? previous.catch(() => {}).then(start) : start()).finally(() => {
    if (c.view.prepping === p) c.view.prepping = null;
  });
  c.view.prepping = p;
  c.view.prepping_generation = gen;
  return p;
}

async function prepRefresh(c: ClientContext): Promise<void> {
  const cl = c.cl;
  const v = c.view;
  const gen = c.clearGeneration;
  const stale = (): boolean => c.clearGeneration !== gen;

  SCR_AddDirtyPoint(c, 0, 0);
  SCR_AddDirtyPoint(c, c.viddef.width - 1, c.viddef.height - 1);

  // let the render dll load the map
  let mapname = cl.configstrings[CS_MODELS + 1]!.slice(5); // skip "maps/"
  mapname = mapname.slice(0, Math.max(0, mapname.length - 4)); // cut off ".bsp"
  const loading = (stage: string, progress: number): void =>
    c.host.onLoading?.({ active: true, mapname, stage, progress });

  // register models, pics, and skins
  Com_Printf(c, 'Map: %s\r', mapname);
  loading('map', 0);
  SCR_UpdateScreen(c);
  await c.re.beginRegistration(mapname);
  if (stale()) return;
  c.regcache.clear();
  v.handleIds.clear(); // (sort ordinals of the previous level's handles; would pin them forever)
  Com_Printf(c, BLANK_LINE);

  // precache status bar pics
  Com_Printf(c, 'pics\r');
  loading('pics', 0.1);
  SCR_UpdateScreen(c);
  await SCR_TouchPics(c);
  if (stale()) return;
  Com_Printf(c, BLANK_LINE);

  await c.fx.registerTEntModels();
  if (stale()) return;

  v.num_cl_weaponmodels = 1;
  v.cl_weaponmodels[0] = 'weapon.md2';

  let nmodels = 1;
  while (nmodels < MAX_MODELS && cl.configstrings[CS_MODELS + nmodels]![0]) nmodels++;

  for (let i = 1; i < MAX_MODELS && cl.configstrings[CS_MODELS + i]![0]; i++) {
    const cs = cl.configstrings[CS_MODELS + i]!;
    const name = cs.slice(0, 37); // never go beyond one line
    if (name[0] !== '*') Com_Printf(c, '%s\r', name);
    loading('models', 0.2 + (0.5 * i) / nmodels);
    SCR_UpdateScreen(c);
    if (name[0] === '#') {
      // special player weapon model
      if (v.num_cl_weaponmodels < MAX_CLIENTWEAPONMODELS) {
        v.cl_weaponmodels[v.num_cl_weaponmodels] = cs.slice(1, 1 + 63);
        v.num_cl_weaponmodels++;
      }
    } else {
      const h = await RegisterModel(c, cs);
      if (stale()) return;
      cl.model_draw[i] = h;
      if (name[0] === '*') {
        if (!c.cmModel) cl.model_clip[i] = null;
        else {
          try {
            cl.model_clip[i] = c.cmModel.inlineModel(cs);
          } catch (e) {
            Com_Error(c, ERR_DROP, '%s', (e as Error).message);
          }
        }
      } else cl.model_clip[i] = null;
    }
    if (name[0] !== '*') Com_Printf(c, BLANK_LINE);
  }

  Com_Printf(c, 'images\r');
  loading('images', 0.7);
  SCR_UpdateScreen(c);
  for (let i = 1; i < MAX_IMAGES && cl.configstrings[CS_IMAGES + i]![0]; i++) {
    const h = await RegisterPic(c, cl.configstrings[CS_IMAGES + i]!);
    if (stale()) return;
    cl.image_precache[i] = h;
  }

  Com_Printf(c, BLANK_LINE);
  for (let i = 0; i < MAX_CLIENTS; i++) {
    if (!cl.configstrings[CS_PLAYERSKINS + i]![0]) continue;
    Com_Printf(c, 'client %i\r', i);
    loading('clients', 0.8);
    SCR_UpdateScreen(c);
    // CL_ParseClientinfo (i)
    await CL_LoadClientinfo(c, cl.clientinfo[i]!, cl.configstrings[CS_PLAYERSKINS + i]!);
    if (stale()) return;
    Com_Printf(c, BLANK_LINE);
  }

  await CL_LoadClientinfo(c, cl.baseclientinfo, 'unnamed\\male/grunt');
  if (stale()) return;

  // set sky textures and speed
  Com_Printf(c, 'sky\r');
  loading('sky', 0.9);
  SCR_UpdateScreen(c);
  const rotate = fr(atof(cl.configstrings[CS_SKYROTATE]!));
  const axis = new Float32Array(3);
  scanFloats3(cl.configstrings[CS_SKYAXIS]!, axis);
  await c.re.setSky(cl.configstrings[CS_SKY]!, rotate, axis);
  if (stale()) return;
  Com_Printf(c, BLANK_LINE);

  // the renderer can now free unneeded stuff
  c.re.endRegistration();

  // clear any lines of console text
  Con_ClearNotify(c);

  SCR_UpdateScreen(c);
  cl.refresh_prepped = true;
  cl.force_refdef = true; // make sure we have a valid refdef

  // configstrings that arrived while the asynchronous precache/prep ran (see dirty_configstrings)
  for (const i of cl.dirty_configstrings) CL_ConfigStringChanged(c, i, cl.configstrings[i]!);
  cl.dirty_configstrings.clear();

  // start the cd track: CDAudio_Play (atoi(cl.configstrings[CS_CDTRACK]), true) -- no CD audio (PARITY)
  loading('done', 1);
  c.host.onLevel?.({ mapname, levelname: cl.configstrings[CS_NAME]! });
}

// C: cl_view.c:363 CalcFov
export function CalcFov(c: ClientContext | null, fov_x: number, width: number, height: number): number {
  fov_x = fr(fov_x);
  width = fr(width);
  height = fr(height);
  if (fov_x < 1 || fov_x > 179) {
    if (c) Com_Error(c, ERR_DROP, 'Bad fov: %f', fov_x);
    throw new Error('Bad fov: ' + fov_x);
  }

  const x = fr(width / Math.tan(fr(fov_x / 360) * Math.PI));

  let a = fr(Math.atan(fr(height / x)));

  a = fr(fr(a * 360) / Math.PI);

  return a;
}

// C: cl_view.c:383 V_Gun_Next_f -- gun frame debugging functions
export function V_Gun_Next_f(c: ClientContext): void {
  c.view.gun_frame++;
  Com_Printf(c, 'frame %i\n', c.view.gun_frame);
}

// C: cl_view.c:389 V_Gun_Prev_f
export function V_Gun_Prev_f(c: ClientContext): void {
  c.view.gun_frame--;
  if (c.view.gun_frame < 0) c.view.gun_frame = 0;
  Com_Printf(c, 'frame %i\n', c.view.gun_frame);
}

// C: cl_view.c:397 V_Gun_Model_f (the model handle is assigned once registration resolves)
export function V_Gun_Model_f(c: ClientContext): void {
  const seq = ++c.view.gun_model_seq;
  if (c.cmd.argc() !== 2) {
    c.view.gun_model = null;
    return;
  }
  const name = ('models/' + c.cmd.argv(1) + '/tris.md2').slice(0, 63);
  void RegisterModel(c, name).then((h) => {
    if (c.view.gun_model_seq === seq) c.view.gun_model = h;
  });
}

// C: cl_view.c:418 SCR_DrawCrosshair
export function SCR_DrawCrosshair(c: ClientContext): void {
  const crosshair = c.cv.crosshair;
  if (!crosshair.value) return;

  if (crosshair.modified) {
    crosshair.modified = false;
    void SCR_TouchPics(c);
  }

  const scr = c.scr;
  if (!scr.crosshair_pic) return;

  const vr = scr.scr_vrect;
  c.re.drawPic(
    vr.x + ((vr.width - scr.crosshair_width) >> 1),
    vr.y + ((vr.height - scr.crosshair_height) >> 1),
    scr.crosshair_pic,
  );
}

function handleId(v: ViewState, h: object | null): number {
  if (!h) return 0;
  let id = v.handleIds.get(h);
  if (id === undefined) {
    id = v.handleIds.size + 1;
    v.handleIds.set(h, id);
  }
  return id;
}

// C: cl_scrn.c:608 entitycmpfnc -- sorted by model then skin. C compares pointer values (and qsort is
// unstable), so the exact order is implementation-defined; identity ordinals group equal models/skins.
function sortEntities(v: ViewState, n: number): void {
  const s = v.sortScratch;
  s.length = n;
  for (let i = 0; i < n; i++) s[i] = v.r_entities[i]!;
  s.sort((a, b) => {
    const am = handleId(v, a.model);
    const bm = handleId(v, b.model);
    if (am === bm) return handleId(v, a.skin) - handleId(v, b.skin);
    return am - bm;
  });
  for (let i = 0; i < n; i++) v.r_entities[i] = s[i]!;
}

// C: cl_view.c:442 V_RenderView
export function V_RenderView(c: ClientContext, stereo_separation: number): void {
  const cl = c.cl;
  const v = c.view;
  const cv = c.cv;

  if (c.cls.state !== ca_active) return;

  if (!cl.refresh_prepped) return; // still loading

  if (cv.cl_timedemo.value) {
    if (!cl.timedemo_start) cl.timedemo_start = Sys_Milliseconds(c);
    cl.timedemo_frames++;
  }

  // an invalid frame will just use the exact previous refdef
  // we can't use the old frame if the video mode has changed, though...
  if (cl.frame.valid && (cl.force_refdef || !cv.cl_paused.value)) {
    cl.force_refdef = false;

    V_ClearScene(c);

    // build a refresh entity list and calc cl.sim*
    // this also calls CL_CalcViewValues which loads
    // v_forward, etc.
    CL_AddEntities(c);

    if (cv.cl_testparticles.value) V_TestParticles(c);
    if (cv.cl_testentities.value) V_TestEntities(c);
    if (cv.cl_testlights.value) V_TestLights(c);
    if (cv.cl_testblend.value) {
      cl.refdef.blend[0] = 1;
      cl.refdef.blend[1] = 0.5;
      cl.refdef.blend[2] = 0.25;
      cl.refdef.blend[3] = 0.5;
    }

    const vo = cl.refdef.vieworg;
    // offset vieworg appropriately if we're doing stereo separation
    if (stereo_separation !== 0) {
      const s = fr(stereo_separation);
      vo[0] = fr(vo[0]! + fr(cl.v_right[0]! * s));
      vo[1] = fr(vo[1]! + fr(cl.v_right[1]! * s));
      vo[2] = fr(vo[2]! + fr(cl.v_right[2]! * s));
    }

    // never let it sit exactly on a node line, because a water plane can
    // dissapear when viewed with the eye exactly on it.
    // the server protocol only specifies to 1/8 pixel, so add 1/16 in each axis
    vo[0] = vo[0]! + 1.0 / 16;
    vo[1] = vo[1]! + 1.0 / 16;
    vo[2] = vo[2]! + 1.0 / 16;

    const vr = c.scr.scr_vrect;
    cl.refdef.x = vr.x;
    cl.refdef.y = vr.y;
    cl.refdef.width = vr.width;
    cl.refdef.height = vr.height;
    cl.refdef.fov_y = CalcFov(c, cl.refdef.fov_x, cl.refdef.width, cl.refdef.height);
    cl.refdef.time = fr(cl.time * 0.001);

    cl.refdef.areabits = cl.frame.areabits;

    if (!cv.cl_add_entities.value) v.r_numentities = 0;
    if (!cv.cl_add_particles.value) v.r_numparticles = 0;
    if (!cv.cl_add_lights.value) v.r_numdlights = 0;
    if (!cv.cl_add_blend.value) {
      // VectorClear: only rgb
      cl.refdef.blend[0] = cl.refdef.blend[1] = cl.refdef.blend[2] = 0;
    }

    cl.refdef.num_entities = v.r_numentities;
    cl.refdef.entities = v.r_entities;
    cl.refdef.num_particles = v.r_numparticles;
    cl.refdef.particles = v.r_particles;
    cl.refdef.num_dlights = v.r_numdlights;
    cl.refdef.dlights = v.r_dlights;
    cl.refdef.lightstyles = v.r_lightstyles;

    cl.refdef.rdflags = cl.frame.playerstate.rdflags;

    // sort entities for better cache locality
    sortEntities(v, cl.refdef.num_entities);
  }

  c.re.renderFrame(cl.refdef);
  if (cv.cl_stats.value)
    Com_Printf(c, 'ent:%i  lt:%i  part:%i\n', v.r_numentities, v.r_numdlights, v.r_numparticles);

  const vr = c.scr.scr_vrect;
  SCR_AddDirtyPoint(c, vr.x, vr.y);
  SCR_AddDirtyPoint(c, vr.x + vr.width - 1, vr.y + vr.height - 1);

  SCR_DrawCrosshair(c);
}

// C: cl_view.c:556 V_Viewpos_f
export function V_Viewpos_f(c: ClientContext): void {
  const r = c.cl.refdef;
  Com_Printf(
    c,
    '(%i %i %i) : %i\n',
    cInt(r.vieworg[0]!),
    cInt(r.vieworg[1]!),
    cInt(r.vieworg[2]!),
    cInt(r.viewangles[YAW]!),
  );
}

// C: cl_view.c:568 V_Init
export function V_Init(c: ClientContext): void {
  c.cmd.addCommand('gun_next', () => V_Gun_Next_f(c));
  c.cmd.addCommand('gun_prev', () => V_Gun_Prev_f(c));
  c.cmd.addCommand('gun_model', () => V_Gun_Model_f(c));

  c.cmd.addCommand('viewpos', () => V_Viewpos_f(c));

  c.cv.crosshair = c.cvars.get('crosshair', '0', CVAR_ARCHIVE);

  c.cv.cl_testblend = c.cvars.get('cl_testblend', '0', 0);
  c.cv.cl_testparticles = c.cvars.get('cl_testparticles', '0', 0);
  c.cv.cl_testentities = c.cvars.get('cl_testentities', '0', 0);
  c.cv.cl_testlights = c.cvars.get('cl_testlights', '0', 0);

  c.cv.cl_stats = c.cvars.get('cl_stats', '0', 0);

  // refdef arrays always reference the preallocated scene arrays
  const v = c.view;
  c.cl.refdef.entities = v.r_entities;
  c.cl.refdef.particles = v.r_particles;
  c.cl.refdef.dlights = v.r_dlights;
  c.cl.refdef.lightstyles = v.r_lightstyles;
}
