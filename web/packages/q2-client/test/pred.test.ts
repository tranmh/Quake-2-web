// Tests for cl_pred.ts (bit-exact prediction vs. q2-pmove) and cl_view.ts.
import { readFileSync } from 'node:fs';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import {
  COM_Parse,
  CONTENTS_SOLID,
  CS_IMAGES,
  CS_MODELS,
  CS_PLAYERSKINS,
  CS_SKY,
  CS_SKYAXIS,
  CS_SKYROTATE,
  MASK_PLAYERSOLID,
  PMF_ON_GROUND,
  PmoveState,
  UserCmd,
  atof,
  floatBits,
  type ParseCursor,
} from 'q2-shared';
import { demoPakPath, fileExists } from 'q2-shared/testing';
import { Pak } from 'q2-formats';
import { CollisionModel, CollisionWorld, Pmove, PmoveT } from 'q2-pmove';
import { MAX_ENTITIES, newRefEntity, type ImageHandle, type ModelHandle, type Refresh } from 'q2-ref';

const loadClientinfoCalls: string[] = [];

vi.mock('../src/cl_parse', async (orig) => ({
  ...(await orig<typeof import('../src/cl_parse')>()),
  CL_LoadClientinfo: async (_c: unknown, _ci: unknown, s: string) => {
    loadClientinfoCalls.push(s);
  },
}));
vi.mock('../src/console', async (orig) => ({
  ...(await orig<typeof import('../src/console')>()),
  Con_ClearNotify: () => {},
}));

import { ca_active, ClientContext } from '../src/client';
import { CL_CheckPredictionError, CL_PMTrace, CL_PMpointcontents, CL_PredictMovement } from '../src/cl_pred';
import { CalcFov, CL_PrepRefresh, V_AddEntity, V_Init } from '../src/cl_view';
import { NullSound } from '../src/sound';
import { NullCinematics } from '../src/cinematic';
import type { Effects } from '../src/effects';

const havePak = fileExists(demoPakPath());

function makeRefresh(log: string[]): Refresh {
  const h = (n: string) => ({ n }) as unknown;
  return {
    init: async () => true,
    shutdown: () => {},
    beginRegistration: async (m) => void log.push('begin ' + m),
    registerModel: async (n) => (log.push('model ' + n), h(n) as ModelHandle),
    registerSkin: async (n) => (log.push('skin ' + n), h(n) as ImageHandle),
    registerPic: async (n) => (log.push('pic ' + n), h(n) as ImageHandle),
    setSky: async (n, r, a) => void log.push(`sky ${n} ${r} ${a[0]} ${a[1]} ${a[2]}`),
    endRegistration: () => void log.push('end'),
    renderFrame: () => {},
    drawGetPicSize: () => [0, 0],
    drawPic: () => {},
    drawStretchPic: () => {},
    drawChar: () => {},
    drawTileClear: () => {},
    drawFill: () => {},
    drawFadeScreen: () => {},
    drawStretchRaw: () => {},
    cinematicSetPalette: () => {},
    beginFrame: () => {},
    endFrame: () => {},
    appActivate: () => {},
  };
}

function makeEffects(log: string[]): Effects {
  return new Proxy({} as Effects, {
    get: (_t, p) => {
      if (p === 'registerTEntModels') return async () => void log.push('tentmodels');
      if (p === 'cl_mod_powerscreen') return null;
      return () => {};
    },
  });
}

function makeContext(log: string[] = []): ClientContext {
  const c = new ClientContext({
    refresh: makeRefresh(log),
    transport: () => {
      throw new Error('no transport');
    },
    loadFile: async () => null,
    sound: new NullSound(),
    effects: makeEffects(log),
    cinematics: new NullCinematics(),
  });
  c.cv.cl_predict = c.cvars.get('cl_predict', '1', 0);
  c.cv.cl_showmiss = c.cvars.get('cl_showmiss', '0', 0);
  c.cv.cl_paused = c.cvars.get('paused', '0', 0);
  c.cv.developer = c.cvars.get('developer', '0', 0);
  return c;
}

let model: CollisionModel;
let spawn: [number, number, number] = [0, 0, 0];
const brushModels: { classname: string; model: string }[] = [];

function parseEntities(s: string): Record<string, string>[] {
  const out: Record<string, string>[] = [];
  const p: ParseCursor = { data: s, pos: 0 };
  for (;;) {
    const t = COM_Parse(p);
    if (p.pos < 0 || t !== '{') break;
    const e: Record<string, string> = {};
    for (;;) {
      const k = COM_Parse(p);
      if (p.pos < 0 || k === '}') break;
      e[k] = COM_Parse(p);
    }
    out.push(e);
  }
  return out;
}

beforeAll(() => {
  if (!havePak) return;
  const pak = new Pak(readFileSync(demoPakPath()));
  model = CollisionModel.load('maps/demo1.bsp', pak.read('maps/demo1.bsp')!);
  const ents = parseEntities(model.entityString);
  const sp =
    ents.find((e) => e['classname'] === 'info_player_deathmatch') ??
    ents.find((e) => e['classname'] === 'info_player_start');
  const o = sp!['origin']!.split(' ').map(Number);
  spawn = [o[0]!, o[1]!, o[2]! + 9];
  for (const e of ents)
    if (e['model']?.startsWith('*')) brushModels.push({ classname: e['classname']!, model: e['model'] });
});

function setupActive(c: ClientContext): void {
  c.cmModel = model;
  c.cm = new CollisionWorld(model);
  c.cls.state = ca_active;
  c.cl.configstrings[29] = '0'; // CS_AIRACCEL
  const f = c.cl.frame;
  f.valid = true;
  const pm = f.playerstate.pmove;
  pm.pm_type = 0;
  pm.gravity = 800;
  pm.origin[0] = Math.trunc(spawn[0] * 8);
  pm.origin[1] = Math.trunc(spawn[1] * 8);
  pm.origin[2] = Math.trunc(spawn[2] * 8);
}

function makeCmds(n: number): UserCmd[] {
  const out: UserCmd[] = [];
  for (let i = 0; i < n; i++) {
    const u = new UserCmd();
    u.msec = [16, 13, 25, 16, 33][i % 5]!;
    u.angles[1] = (i * 300) & 0xffff;
    u.angles[0] = i > 30 ? -600 : 0;
    if (i >= 8) u.forwardmove = 400;
    if (i >= 20 && i < 30) u.sidemove = -200;
    if (i === 25 || i === 40) u.upmove = 200;
    out.push(u);
  }
  return out;
}

/** Independent reference: q2-pmove with a world-only trace on a separate CollisionWorld. */
function referencePmove(start: PmoveState, cmds: UserCmd[], airaccel: number) {
  const world = new CollisionWorld(model);
  const pmove = new Pmove();
  pmove.pm_airaccelerate = airaccel;
  const pm = new PmoveT(
    (s, mi, ma, e) => {
      const t = world.boxTrace(s, e, mi, ma, 0, MASK_PLAYERSOLID);
      if (t.fraction < 1) t.ent = 1;
      return t;
    },
    (p) => world.pointContents(p, 0),
  );
  pm.s.copyFrom(start);
  const origins: Int16Array[] = [];
  for (const cmd of cmds) {
    pm.cmd.copyFrom(cmd);
    pmove.run(pm);
    origins.push(pm.s.origin.slice());
  }
  return { pm, origins };
}

describe.skipIf(!havePak)('CL_PredictMovement', () => {
  it('is bit-identical to running q2-pmove directly', () => {
    const c = makeContext();
    setupActive(c);
    const cmds = makeCmds(50);
    const ack = 100;
    const current = ack + 1 + cmds.length;
    c.cls.netchan.incoming_acknowledged = ack;
    c.cls.netchan.outgoing_sequence = current;
    for (let i = 0; i < cmds.length; i++) c.cl.cmds[(ack + 1 + i) & 63]!.copyFrom(cmds[i]!);

    CL_PredictMovement(c);
    const ref = referencePmove(c.cl.frame.playerstate.pmove, cmds, 0);

    expect(c.pred.lastFrames).toBe(cmds.length);
    for (let i = 0; i < cmds.length; i++) {
      const f = (ack + 1 + i) & 63;
      expect(Array.from(c.cl.predicted_origins.subarray(f * 3, f * 3 + 3))).toEqual(
        Array.from(ref.origins[i]!),
      );
    }
    for (let i = 0; i < 3; i++) {
      expect(floatBits(c.cl.predicted_origin[i]!)).toBe(floatBits(Math.fround(ref.pm.s.origin[i]! * 0.125)));
      expect(floatBits(c.cl.predicted_angles[i]!)).toBe(floatBits(ref.pm.viewangles[i]!));
    }
    // the player really moved
    const moved =
      Math.abs(ref.pm.s.origin[0]! - c.cl.frame.playerstate.pmove.origin[0]!) +
      Math.abs(ref.pm.s.origin[1]! - c.cl.frame.playerstate.pmove.origin[1]!);
    expect(moved).toBeGreaterThan(8 * 64);
  });

  it('only sets angles without prediction and freezes past CMD_BACKUP', () => {
    const c = makeContext();
    setupActive(c);
    c.cvars.set('cl_predict', '0');
    c.cl.viewangles[1] = 10;
    c.cl.frame.playerstate.pmove.delta_angles[1] = 16384;
    CL_PredictMovement(c);
    expect(c.cl.predicted_angles[1]).toBe(100);

    c.cvars.set('cl_predict', '1');
    c.cl.predicted_origin.fill(0);
    c.cls.netchan.incoming_acknowledged = 10;
    c.cls.netchan.outgoing_sequence = 10 + 64;
    CL_PredictMovement(c);
    expect(Array.from(c.cl.predicted_origin)).toEqual([0, 0, 0]);
  });

  it('detects stair steps', () => {
    const c = makeContext();
    setupActive(c);
    const pm = c.cl.frame.playerstate.pmove;
    pm.pm_type = 4; // PM_FREEZE: pmove leaves the state untouched
    pm.pm_flags = PMF_ON_GROUND;
    c.cls.netchan.incoming_acknowledged = 200;
    c.cls.netchan.outgoing_sequence = 201; // no commands to run
    c.cls.realtime = 5000;
    c.cls.frametime = Math.fround(0.016);
    c.cl.predicted_origins[((201 - 2) & 63) * 3 + 2] = pm.origin[2]! - 100;
    CL_PredictMovement(c);
    expect(c.cl.predicted_step).toBe(12.5);
    expect(c.cl.predicted_step_time).toBe(
      Math.trunc(Math.fround(5000 - Math.fround(Math.fround(0.016) * 500))),
    );
  });
});

describe.skipIf(!havePak)('CL_CheckPredictionError', () => {
  it('computes error, clears on teleports', () => {
    const c = makeContext();
    setupActive(c);
    c.cls.netchan.incoming_acknowledged = 70;
    const f = 70 & 63;
    const o = c.cl.frame.playerstate.pmove.origin;
    c.cl.predicted_origins.set(o, f * 3);
    CL_CheckPredictionError(c);
    expect(Array.from(c.cl.prediction_error)).toEqual([0, 0, 0]);

    c.cl.predicted_origins[f * 3] = o[0]! - 3;
    c.cl.predicted_origins[f * 3 + 2] = o[2]! + 5;
    CL_CheckPredictionError(c);
    expect(Array.from(c.cl.prediction_error)).toEqual([0.375, 0, -0.625]);
    expect(c.cl.predicted_origins[f * 3]).toBe(o[0]); // resynced

    c.cl.predicted_origins[f * 3] = o[0]! - 700;
    CL_CheckPredictionError(c);
    expect(Array.from(c.cl.prediction_error)).toEqual([0, 0, 0]);
    expect(c.cl.predicted_origins[f * 3]).toBe(o[0]! - 700); // not resynced on teleport
  });
});

describe.skipIf(!havePak)('CL_ClipMoveToEntities / CL_PMpointcontents', () => {
  it('clips against brush models and encoded boxes', () => {
    const c = makeContext();
    setupActive(c);
    const bm =
      brushModels.find((b) => b.classname === 'func_door' || b.classname === 'func_plat') ?? brushModels[0]!;
    const cmodel = model.inlineModel(bm.model);
    c.cl.model_clip[5] = cmodel;
    const ent = c.cl_parse_entities[0]!;
    ent.number = 50;
    ent.solid = 31;
    ent.modelindex = 5;
    c.cl.frame.parse_entities = 0;
    c.cl.frame.num_entities = 1;

    const center = new Float32Array(3);
    for (let i = 0; i < 3; i++) center[i] = (cmodel.mins[i]! + cmodel.maxs[i]!) * 0.5;
    expect(CL_PMpointcontents(c, center) & CONTENTS_SOLID).toBeTruthy();
    c.cl.frame.num_entities = 0;
    const worldOnly = CL_PMpointcontents(c, center);
    expect(worldOnly & CONTENTS_SOLID).toBe(0);

    // a point trace from the centre of the brush entity outwards starts solid only with the entity
    c.cl.frame.num_entities = 1;
    const zero = new Float32Array(3);
    const end = center.slice();
    end[2] = end[2]! + 1;
    const t = CL_PMTrace(c, center, zero, zero, end);
    expect(t.startsolid).toBe(true);
    expect(t.ent).toBe(ent);

    // encoded bbox (x=16, zd=24, zu=32) 100 units in front of the spawn point
    ent.solid = 2 | (3 << 5) | (8 << 10);
    ent.modelindex = 0;
    ent.origin[0] = spawn[0] + 100;
    ent.origin[1] = spawn[1];
    ent.origin[2] = spawn[2];
    const s = new Float32Array(spawn);
    const e = s.slice();
    e[0] = e[0]! + 100;
    const mins = new Float32Array([-16, -16, -24]);
    const maxs = new Float32Array([16, 16, 32]);
    const world = new CollisionWorld(model).boxTrace(s, e, mins, maxs, 0, MASK_PLAYERSOLID);
    const t2 = CL_PMTrace(c, s, mins, maxs, e);
    if (world.fraction === 1) {
      expect(t2.ent).toBe(ent);
      expect(t2.endpos[0]).toBeCloseTo(spawn[0] + 100 - 32, 1);
    } else expect(t2.fraction).toBeLessThanOrEqual(world.fraction);
  });
});

describe('cl_view', () => {
  it('V_AddEntity copies and limits', () => {
    const c = makeContext();
    const e = newRefEntity();
    e.origin[0] = 5;
    e.frame = 3;
    V_AddEntity(c, e);
    e.origin[0] = 6;
    expect(c.view.r_entities[0]!.origin[0]).toBe(5);
    expect(c.view.r_entities[0]!.frame).toBe(3);
    for (let i = 0; i < MAX_ENTITIES + 10; i++) V_AddEntity(c, e);
    expect(c.view.r_numentities).toBe(MAX_ENTITIES);
  });

  it('CalcFov matches the C float formula', () => {
    const f = Math.fround;
    const ref = (fx: number, w: number, h: number) => {
      const x = f(w / Math.tan(f(fx / 360) * Math.PI));
      let a = f(Math.atan(f(h / x)));
      a = f(f(a * 360) / Math.PI);
      return a;
    };
    expect(CalcFov(null, 90, 640, 480)).toBe(ref(90, 640, 480));
    expect(Math.abs(CalcFov(null, 90, 640, 480) - 73.74)).toBeLessThan(0.01);
    expect(CalcFov(null, 110, 1024, 768)).toBe(ref(110, 1024, 768));
    expect(() => CalcFov(null, 0.5, 640, 480)).toThrow();
  });

  it.skipIf(!havePak)('CL_PrepRefresh registers in the original order', async () => {
    const log: string[] = [];
    const c = makeContext(log);
    V_Init(c);
    c.cmModel = model;
    c.cm = new CollisionWorld(model);
    const cs = c.cl.configstrings;
    cs[CS_MODELS + 1] = 'maps/demo1.bsp';
    cs[CS_MODELS + 2] = '*1';
    cs[CS_MODELS + 3] = 'models/items/armor/tris.md2';
    cs[CS_MODELS + 4] = '#w_blaster.md2';
    cs[CS_IMAGES + 1] = 'i_health';
    cs[CS_PLAYERSKINS + 2] = 'bob\\female/athena';
    cs[CS_SKY] = 'unit1_';
    cs[CS_SKYROTATE] = '1.5';
    cs[CS_SKYAXIS] = '0 0.5 1';
    loadClientinfoCalls.length = 0;
    const levels: string[] = [];
    (c.host as { onLevel?: (i: { mapname: string }) => void }).onLevel = (i) => levels.push(i.mapname);
    const p1 = CL_PrepRefresh(c);
    expect(CL_PrepRefresh(c)).toBe(p1);
    await p1;
    // SCR_TouchPics (real) registers the HUD pics between beginRegistration and the tent models
    const ti = log.indexOf('tentmodels');
    expect(log.slice(1, ti).every((l) => l.startsWith('pic '))).toBe(true);
    expect([log[0], ...log.slice(ti)]).toEqual([
      'begin demo1',
      'tentmodels',
      'model maps/demo1.bsp',
      'model *1',
      'model models/items/armor/tris.md2',
      'pic i_health',
      `sky unit1_ 1.5 0 0.5 1`,
      'end',
    ]);
    expect(loadClientinfoCalls).toEqual(['bob\\female/athena', 'unnamed\\male/grunt']);
    expect(c.cl.model_clip[2]).toBe(model.inlineModel('*1'));
    expect(c.cl.model_clip[3]).toBeNull();
    expect(c.view.cl_weaponmodels.slice(0, c.view.num_cl_weaponmodels)).toEqual([
      'weapon.md2',
      'w_blaster.md2',
    ]);
    expect(c.cl.refresh_prepped).toBe(true);
    expect(c.cl.force_refdef).toBe(true);
    expect(levels).toEqual(['demo1']);
    expect(atof(cs[CS_SKYROTATE]!)).toBe(1.5);
  });

  it('CL_PrepRefresh abandons when the client state is cleared', async () => {
    const log: string[] = [];
    const c = makeContext(log);
    c.cl.configstrings[CS_MODELS + 1] = 'maps/demo1.bsp';
    const p = CL_PrepRefresh(c);
    c.clearGeneration++;
    await p;
    expect(c.cl.refresh_prepped).toBe(false);
    expect(log).toEqual(['begin demo1']);
  });
});
