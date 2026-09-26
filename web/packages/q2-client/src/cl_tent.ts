// Port of client/cl_tent.c -- client side temporary entities (explosions, beams, lasers, sustains and
// the svc_temp_entity parser). All globals live in TEntState (fx.tent).
import {
  MAX_EDICTS,
  AngleVectors,
  ATTN_NONE,
  ATTN_NORM,
  ATTN_STATIC,
  CHAN_WEAPON,
  ERR_DROP,
  M_PI,
  RF_BEAM,
  RF_FULLBRIGHT,
  RF_TRANSLUCENT,
  SPLASH_SPARKS,
  TE_BFG_BIGEXPLOSION,
  TE_BFG_EXPLOSION,
  TE_BFG_LASER,
  TE_BLASTER,
  TE_BLASTER2,
  TE_BLOOD,
  TE_BLUEHYPERBLASTER,
  TE_BOSSTPORT,
  TE_BUBBLETRAIL,
  TE_BUBBLETRAIL2,
  TE_BULLET_SPARKS,
  TE_CHAINFIST_SMOKE,
  TE_DBALL_GOAL,
  TE_DEBUGTRAIL,
  TE_ELECTRIC_SPARKS,
  TE_EXPLOSION1,
  TE_EXPLOSION1_BIG,
  TE_EXPLOSION1_NP,
  TE_EXPLOSION2,
  TE_FLASHLIGHT,
  TE_FLECHETTE,
  TE_FORCEWALL,
  TE_GRAPPLE_CABLE,
  TE_GREENBLOOD,
  TE_GRENADE_EXPLOSION,
  TE_GRENADE_EXPLOSION_WATER,
  TE_GUNSHOT,
  TE_HEATBEAM,
  TE_HEATBEAM_SPARKS,
  TE_HEATBEAM_STEAM,
  TE_LASER_SPARKS,
  TE_LIGHTNING,
  TE_MEDIC_CABLE_ATTACK,
  TE_MONSTER_HEATBEAM,
  TE_MOREBLOOD,
  TE_NUKEBLAST,
  TE_PARASITE_ATTACK,
  TE_PLAIN_EXPLOSION,
  TE_PLASMA_EXPLOSION,
  TE_RAILTRAIL,
  TE_ROCKET_EXPLOSION,
  TE_ROCKET_EXPLOSION_WATER,
  TE_SCREEN_SPARKS,
  TE_SHIELD_SPARKS,
  TE_SHOTGUN,
  TE_SPARKS,
  TE_SPLASH,
  TE_STEAM,
  TE_TELEPORT_EFFECT,
  TE_TRACKER_EXPLOSION,
  TE_TUNNEL_SPARKS,
  TE_WELDING_SPARKS,
  TE_WIDOWBEAMOUT,
  TE_WIDOWSPLASH,
  UPDATE_MASK,
  VectorAdd,
  VectorClear,
  VectorCompare,
  VectorCopy,
  VectorLength,
  VectorMA,
  VectorNormalize,
  VectorScale,
  VectorSet,
  VectorSubtract,
  sprintf,
  vec3_origin,
} from 'q2-shared';
import {
  MSG_ReadByte,
  MSG_ReadDir,
  MSG_ReadLong,
  MSG_ReadPos,
  MSG_ReadShort,
  type SizeBuf,
} from 'q2-protocol';
import { newRefEntity, type ModelHandle, type RefEntity } from 'q2-ref';
import { Com_Error, Com_Printf } from './client';
import type { SfxHandle } from './sound';
import { V_AddEntity, V_AddLight } from './cl_view';
import { RegisterModel, RegisterPic } from './regcache';
import {
  CL_BFGExplosionParticles,
  CL_BigTeleportParticles,
  CL_BlasterParticles,
  CL_BubbleTrail,
  CL_ExplosionParticles,
  CL_ParticleEffect,
  CL_ParticleEffect2,
  CL_ParticleEffect3,
  CL_RailTrail,
  CL_TeleportParticles,
  frand,
  type FxState,
} from './cl_fx';
import {
  CL_BlasterParticles2,
  CL_BubbleTrail2,
  CL_ColorExplosionParticles,
  CL_ColorFlash,
  CL_DebugTrail,
  CL_Flashlight,
  CL_ForceWall,
  CL_Heatbeam,
  CL_MonsterPlasma_Shell,
  CL_Nukeblast,
  CL_ParticleSmokeEffect,
  CL_ParticleSteamEffect,
  CL_ParticleSteamEffect2,
  CL_WidowSplash,
  CL_Widowbeamout,
} from './cl_newfx';

const fr = Math.fround;

// C: cl_tent.c exptype_t
export const ex_free = 0;
export const ex_explosion = 1;
export const ex_misc = 2;
export const ex_flash = 3;
export const ex_mflash = 4;
export const ex_poly = 5;
export const ex_poly2 = 6;

export const MAX_EXPLOSIONS = 32;
export const MAX_BEAMS = 32;
export const MAX_LASERS = 32;
// C: client.h
export const MAX_SUSTAINS = 32;

/** memset(&ent, 0, sizeof(ent)) */
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

// C: cl_tent.c explosion_t
export class Explosion {
  type = ex_free;
  readonly ent: RefEntity = newRefEntity();
  frames = 0;
  light = 0;
  readonly lightcolor = new Float32Array(3);
  start = 0;
  baseframe = 0;

  clear(): void {
    this.type = ex_free;
    clearRefEntity(this.ent);
    this.frames = 0;
    this.light = 0;
    this.lightcolor.fill(0);
    this.start = 0;
    this.baseframe = 0;
  }
}

// C: cl_tent.c beam_t
export class Beam {
  entity = 0;
  dest_entity = 0;
  model: ModelHandle | null = null;
  endtime = 0;
  readonly offset = new Float32Array(3);
  readonly start = new Float32Array(3);
  readonly end = new Float32Array(3);

  clear(): void {
    this.entity = 0;
    this.dest_entity = 0;
    this.model = null;
    this.endtime = 0;
    this.offset.fill(0);
    this.start.fill(0);
    this.end.fill(0);
  }
}

// C: cl_tent.c laser_t
export class Laser {
  readonly ent: RefEntity = newRefEntity();
  endtime = 0;

  clear(): void {
    clearRefEntity(this.ent);
    this.endtime = 0;
  }
}

/** C: client.h cl_sustain_t. `think` is the C function pointer (CL_ParticleSteamEffect2 / CL_Widowbeamout / CL_Nukeblast). */
export class CSustain {
  id = 0;
  type = 0;
  endtime = 0;
  nextthink = 0;
  thinkinterval = 0;
  readonly org = new Float32Array(3);
  readonly dir = new Float32Array(3);
  color = 0;
  count = 0;
  magnitude = 0;
  think: ((fx: FxState, self: CSustain) => void) | null = null;

  clear(): void {
    this.id = 0;
    this.type = 0;
    this.endtime = 0;
    this.nextthink = 0;
    this.thinkinterval = 0;
    this.org.fill(0);
    this.dir.fill(0);
    this.color = 0;
    this.count = 0;
    this.magnitude = 0;
    this.think = null;
  }
}

/** Globals of cl_tent.c. */
export class TEntState {
  readonly cl_explosions: Explosion[] = Array.from({ length: MAX_EXPLOSIONS }, () => new Explosion());
  readonly cl_beams: Beam[] = Array.from({ length: MAX_BEAMS }, () => new Beam());
  // PMM - added this for player-linked beams.  Currently only used by the plasma beam
  readonly cl_playerbeams: Beam[] = Array.from({ length: MAX_BEAMS }, () => new Beam());
  readonly cl_lasers: Laser[] = Array.from({ length: MAX_LASERS }, () => new Laser());
  readonly cl_sustains: CSustain[] = Array.from({ length: MAX_SUSTAINS }, () => new CSustain());

  cl_sfx_ric1: SfxHandle | null = null;
  cl_sfx_ric2: SfxHandle | null = null;
  cl_sfx_ric3: SfxHandle | null = null;
  cl_sfx_lashit: SfxHandle | null = null;
  cl_sfx_spark5: SfxHandle | null = null;
  cl_sfx_spark6: SfxHandle | null = null;
  cl_sfx_spark7: SfxHandle | null = null;
  cl_sfx_railg: SfxHandle | null = null;
  cl_sfx_rockexp: SfxHandle | null = null;
  cl_sfx_grenexp: SfxHandle | null = null;
  cl_sfx_watrexp: SfxHandle | null = null;
  cl_sfx_plasexp: SfxHandle | null = null;
  readonly cl_sfx_footsteps: (SfxHandle | null)[] = [null, null, null, null];

  cl_mod_explode: ModelHandle | null = null;
  cl_mod_smoke: ModelHandle | null = null;
  cl_mod_flash: ModelHandle | null = null;
  cl_mod_parasite_segment: ModelHandle | null = null;
  cl_mod_grapple_cable: ModelHandle | null = null;
  cl_mod_parasite_tip: ModelHandle | null = null;
  cl_mod_explo4: ModelHandle | null = null;
  cl_mod_bfg_explo: ModelHandle | null = null;
  cl_mod_powerscreen: ModelHandle | null = null;
  cl_mod_plasmaexplo: ModelHandle | null = null;

  cl_sfx_lightning: SfxHandle | null = null;
  cl_sfx_disrexp: SfxHandle | null = null;
  cl_mod_lightning: ModelHandle | null = null;
  cl_mod_heatbeam: ModelHandle | null = null;
  cl_mod_monster_heatbeam: ModelHandle | null = null;
  cl_mod_explo4_big: ModelHandle | null = null;

  // scratch (C locals)
  readonly pos = new Float32Array(3);
  readonly pos2 = new Float32Array(3);
  readonly dir = new Float32Array(3);
  readonly offset = new Float32Array(3);
  readonly dist = new Float32Array(3);
  readonly org = new Float32Array(3);
  readonly f = new Float32Array(3);
  readonly r = new Float32Array(3);
  readonly u = new Float32Array(3);
  readonly ent: RefEntity = newRefEntity();
}

// C: cl_tent.c:125 CL_RegisterTEntSounds
export function CL_RegisterTEntSounds(fx: FxState): void {
  const t = fx.tent;
  const S_RegisterSound = (n: string): SfxHandle | null => fx.c.sound.registerSound(n);
  t.cl_sfx_ric1 = S_RegisterSound('world/ric1.wav');
  t.cl_sfx_ric2 = S_RegisterSound('world/ric2.wav');
  t.cl_sfx_ric3 = S_RegisterSound('world/ric3.wav');
  t.cl_sfx_lashit = S_RegisterSound('weapons/lashit.wav');
  t.cl_sfx_spark5 = S_RegisterSound('world/spark5.wav');
  t.cl_sfx_spark6 = S_RegisterSound('world/spark6.wav');
  t.cl_sfx_spark7 = S_RegisterSound('world/spark7.wav');
  t.cl_sfx_railg = S_RegisterSound('weapons/railgf1a.wav');
  t.cl_sfx_rockexp = S_RegisterSound('weapons/rocklx1a.wav');
  t.cl_sfx_grenexp = S_RegisterSound('weapons/grenlx1a.wav');
  t.cl_sfx_watrexp = S_RegisterSound('weapons/xpld_wat.wav');
  // RAFAEL
  // cl_sfx_plasexp = S_RegisterSound ("weapons/plasexpl.wav");
  S_RegisterSound('player/land1.wav');

  S_RegisterSound('player/fall2.wav');
  S_RegisterSound('player/fall1.wav');

  for (let i = 0; i < 4; i++) t.cl_sfx_footsteps[i] = S_RegisterSound(sprintf('player/step%i.wav', i + 1));

  // PGM
  t.cl_sfx_lightning = S_RegisterSound('weapons/tesla.wav');
  t.cl_sfx_disrexp = S_RegisterSound('weapons/disrupthit.wav');
  // (the ROGUE_VERSION_ID "weapons/sound%d.wav" name is built but never used)
}

// C: cl_tent.c:172 CL_RegisterTEntModels (awaited in C call order; handles cached by regcache)
export async function CL_RegisterTEntModels(fx: FxState): Promise<void> {
  const c = fx.c;
  const t = fx.tent;
  const M = (n: string): Promise<ModelHandle | null> => RegisterModel(c, n);
  t.cl_mod_explode = await M('models/objects/explode/tris.md2');
  t.cl_mod_smoke = await M('models/objects/smoke/tris.md2');
  t.cl_mod_flash = await M('models/objects/flash/tris.md2');
  t.cl_mod_parasite_segment = await M('models/monsters/parasite/segment/tris.md2');
  t.cl_mod_grapple_cable = await M('models/ctf/segment/tris.md2');
  t.cl_mod_parasite_tip = await M('models/monsters/parasite/tip/tris.md2');
  t.cl_mod_explo4 = await M('models/objects/r_explode/tris.md2');
  t.cl_mod_bfg_explo = await M('sprites/s_bfg2.sp2');
  t.cl_mod_powerscreen = await M('models/items/armor/effect/tris.md2');

  await M('models/objects/laser/tris.md2');
  await M('models/objects/grenade2/tris.md2');
  await M('models/weapons/v_machn/tris.md2');
  await M('models/weapons/v_handgr/tris.md2');
  await M('models/weapons/v_shotg2/tris.md2');
  await M('models/objects/gibs/bone/tris.md2');
  await M('models/objects/gibs/sm_meat/tris.md2');
  await M('models/objects/gibs/bone2/tris.md2');
  // RAFAEL
  // re.RegisterModel ("models/objects/blaser/tris.md2");

  await RegisterPic(c, 'w_machinegun');
  await RegisterPic(c, 'a_bullets');
  await RegisterPic(c, 'i_health');
  await RegisterPic(c, 'a_grenades');

  // ROGUE
  t.cl_mod_explo4_big = await M('models/objects/r_explode2/tris.md2');
  t.cl_mod_lightning = await M('models/proj/lightning/tris.md2');
  t.cl_mod_heatbeam = await M('models/proj/beam/tris.md2');
  t.cl_mod_monster_heatbeam = await M('models/proj/widowbeam/tris.md2');
}

// C: cl_tent.c:213 CL_ClearTEnts
export function CL_ClearTEnts(fx: FxState): void {
  const t = fx.tent;
  for (const b of t.cl_beams) b.clear();
  for (const e of t.cl_explosions) e.clear();
  for (const l of t.cl_lasers) l.clear();
  // ROGUE
  for (const b of t.cl_playerbeams) b.clear();
  for (const s of t.cl_sustains) s.clear();
}

// C: cl_tent.c:230 CL_AllocExplosion
export function CL_AllocExplosion(fx: FxState): Explosion {
  const ex = fx.tent.cl_explosions;
  for (let i = 0; i < MAX_EXPLOSIONS; i++) {
    if (ex[i]!.type === ex_free) {
      ex[i]!.clear();
      return ex[i]!;
    }
  }
  // find the oldest explosion
  let time = fx.c.cl.time; // int
  let index = 0;
  for (let i = 0; i < MAX_EXPLOSIONS; i++)
    if (ex[i]!.start < time) {
      time = Math.trunc(ex[i]!.start); // float -> int
      index = i;
    }
  ex[index]!.clear();
  return ex[index]!;
}

// C: cl_tent.c:263 CL_SmokeAndFlash
export function CL_SmokeAndFlash(fx: FxState, origin: ArrayLike<number>): void {
  const cl = fx.c.cl;
  let ex = CL_AllocExplosion(fx);
  VectorCopy(origin, ex.ent.origin);
  ex.type = ex_misc;
  ex.frames = 4;
  ex.ent.flags = RF_TRANSLUCENT;
  ex.start = fr(cl.frame.servertime - 100);
  ex.ent.model = fx.tent.cl_mod_smoke;

  ex = CL_AllocExplosion(fx);
  VectorCopy(origin, ex.ent.origin);
  ex.type = ex_flash;
  ex.ent.flags = RF_FULLBRIGHT;
  ex.frames = 2;
  ex.start = fr(cl.frame.servertime - 100);
  ex.ent.model = fx.tent.cl_mod_flash;
}

// C: cl_tent.c:289 CL_ParseParticles (unused by CL_ParseTEnt in 3.19)
export function CL_ParseParticles(fx: FxState, msg: SizeBuf): void {
  const t = fx.tent;
  MSG_ReadPos(msg, t.pos);
  MSG_ReadDir(msg, t.dir);
  const color = MSG_ReadByte(msg);
  const count = MSG_ReadByte(msg);
  CL_ParticleEffect(fx, t.pos, t.dir, color, count);
}

function setBeam(
  b: Beam,
  ent: number,
  model: ModelHandle | null,
  endtime: number,
  start: Float32Array,
  end: Float32Array,
  offset: Float32Array | null,
): void {
  b.entity = ent;
  b.model = model;
  b.endtime = endtime;
  VectorCopy(start, b.start);
  VectorCopy(end, b.end);
  if (offset) VectorCopy(offset, b.offset);
  else VectorClear(b.offset);
}

// C: cl_tent.c:309 CL_ParseBeam
export function CL_ParseBeam(fx: FxState, msg: SizeBuf, model: ModelHandle | null): number {
  const c = fx.c;
  const t = fx.tent;
  const start = t.pos;
  const end = t.pos2;
  const ent = MSG_ReadShort(msg);
  MSG_ReadPos(msg, start);
  MSG_ReadPos(msg, end);

  // override any beam with the same entity
  for (const b of t.cl_beams)
    if (b.entity === ent) {
      setBeam(b, ent, model, c.cl.time + 200, start, end, null);
      return ent;
    }

  // find a free beam
  for (const b of t.cl_beams) {
    if (!b.model || b.endtime < c.cl.time) {
      setBeam(b, ent, model, c.cl.time + 200, start, end, null);
      return ent;
    }
  }
  Com_Printf(c, 'beam list overflow!\n');
  return ent;
}

// C: cl_tent.c:357 CL_ParseBeam2
export function CL_ParseBeam2(fx: FxState, msg: SizeBuf, model: ModelHandle | null): number {
  const c = fx.c;
  const t = fx.tent;
  const start = t.pos;
  const end = t.pos2;
  const offset = t.offset;
  const ent = MSG_ReadShort(msg);
  MSG_ReadPos(msg, start);
  MSG_ReadPos(msg, end);
  MSG_ReadPos(msg, offset);

  // override any beam with the same entity
  for (const b of t.cl_beams)
    if (b.entity === ent) {
      setBeam(b, ent, model, c.cl.time + 200, start, end, offset);
      return ent;
    }

  // find a free beam
  for (const b of t.cl_beams) {
    if (!b.model || b.endtime < c.cl.time) {
      setBeam(b, ent, model, c.cl.time + 200, start, end, offset);
      return ent;
    }
  }
  Com_Printf(c, 'beam list overflow!\n');
  return ent;
}

// C: cl_tent.c:411 CL_ParsePlayerBeam -- adds to the cl_playerbeam array instead of the cl_beams array
export function CL_ParsePlayerBeam(fx: FxState, msg: SizeBuf, model: ModelHandle | null): number {
  const c = fx.c;
  const t = fx.tent;
  const start = t.pos;
  const end = t.pos2;
  const offset = t.offset;
  const ent = MSG_ReadShort(msg);
  MSG_ReadPos(msg, start);
  MSG_ReadPos(msg, end);
  // PMM - network optimization
  if (model === t.cl_mod_heatbeam) VectorSet(offset, 2, 7, -3);
  else if (model === t.cl_mod_monster_heatbeam) {
    model = t.cl_mod_heatbeam;
    VectorSet(offset, 0, 0, 0);
  } else MSG_ReadPos(msg, offset);

  // override any beam with the same entity
  // PMM - For player beams, we only want one per player (entity) so..
  for (const b of t.cl_playerbeams) {
    if (b.entity === ent) {
      setBeam(b, ent, model, c.cl.time + 200, start, end, offset);
      return ent;
    }
  }

  // find a free beam
  for (const b of t.cl_playerbeams) {
    if (!b.model || b.endtime < c.cl.time) {
      // PMM - this needs to be 100 to prevent multiple heatbeams
      setBeam(b, ent, model, c.cl.time + 100, start, end, offset);
      return ent;
    }
  }
  Com_Printf(c, 'beam list overflow!\n');
  return ent;
}

// C: cl_tent.c:475 CL_ParseLightning
export function CL_ParseLightning(fx: FxState, msg: SizeBuf, model: ModelHandle | null): number {
  const c = fx.c;
  const t = fx.tent;
  const start = t.pos;
  const end = t.pos2;
  const srcEnt = MSG_ReadShort(msg);
  const destEnt = MSG_ReadShort(msg);
  MSG_ReadPos(msg, start);
  MSG_ReadPos(msg, end);

  // override any beam with the same source AND destination entities
  for (const b of t.cl_beams)
    if (b.entity === srcEnt && b.dest_entity === destEnt) {
      setBeam(b, srcEnt, model, c.cl.time + 200, start, end, null);
      b.dest_entity = destEnt;
      return srcEnt;
    }

  // find a free beam
  for (const b of t.cl_beams) {
    if (!b.model || b.endtime < c.cl.time) {
      setBeam(b, srcEnt, model, c.cl.time + 200, start, end, null);
      b.dest_entity = destEnt;
      return srcEnt;
    }
  }
  Com_Printf(c, 'beam list overflow!\n');
  return srcEnt;
}

// C: cl_tent.c:528 CL_ParseLaser
export function CL_ParseLaser(fx: FxState, msg: SizeBuf, colors: number): void {
  const c = fx.c;
  const t = fx.tent;
  const start = t.pos;
  const end = t.pos2;
  MSG_ReadPos(msg, start);
  MSG_ReadPos(msg, end);

  for (const l of t.cl_lasers) {
    if (l.endtime < c.cl.time) {
      l.ent.flags = RF_TRANSLUCENT | RF_BEAM;
      VectorCopy(start, l.ent.origin);
      VectorCopy(end, l.ent.oldorigin);
      l.ent.alpha = fr(0.3);
      l.ent.skinnum = ((colors | 0) >> ((c.rand.rand() % 4) * 8)) & 0xff;
      l.ent.model = null;
      l.ent.frame = 4;
      l.endtime = c.cl.time + 100;
      return;
    }
  }
}

function findFreeSustain(fx: FxState): CSustain | null {
  for (const s of fx.tent.cl_sustains) if (s.id === 0) return s;
  return null;
}

// C: cl_tent.c:557 CL_ParseSteam
export function CL_ParseSteam(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const t = fx.tent;
  const id = MSG_ReadShort(msg); // an id of -1 is an instant effect
  if (id !== -1) {
    // sustains
    const s = findFreeSustain(fx);
    if (s) {
      s.id = id;
      s.count = MSG_ReadByte(msg);
      MSG_ReadPos(msg, s.org);
      MSG_ReadDir(msg, s.dir);
      const r = MSG_ReadByte(msg);
      s.color = r & 0xff;
      s.magnitude = MSG_ReadShort(msg);
      s.endtime = (c.cl.time + MSG_ReadLong(msg)) | 0;
      s.think = CL_ParticleSteamEffect2;
      s.thinkinterval = 100;
      s.nextthink = c.cl.time;
    } else {
      // FIXME - read the stuff anyway
      MSG_ReadByte(msg);
      MSG_ReadPos(msg, t.pos);
      MSG_ReadDir(msg, t.dir);
      MSG_ReadByte(msg);
      MSG_ReadShort(msg);
      MSG_ReadLong(msg); // really interval
    }
  } else {
    // instant
    const cnt = MSG_ReadByte(msg);
    MSG_ReadPos(msg, t.pos);
    MSG_ReadDir(msg, t.dir);
    const r = MSG_ReadByte(msg);
    const magnitude = MSG_ReadShort(msg);
    const color = r & 0xff;
    CL_ParticleSteamEffect(fx, t.pos, t.dir, color, cnt, magnitude);
  }
}

// C: cl_tent.c:619 CL_ParseWidow
export function CL_ParseWidow(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const id = MSG_ReadShort(msg);
  const s = findFreeSustain(fx);
  if (s) {
    s.id = id;
    MSG_ReadPos(msg, s.org);
    s.endtime = c.cl.time + 2100;
    s.think = CL_Widowbeamout;
    s.thinkinterval = 1;
    s.nextthink = c.cl.time;
  } else {
    // no free sustains -- FIXME - read the stuff anyway
    MSG_ReadPos(msg, fx.tent.pos);
  }
}

// C: cl_tent.c:652 CL_ParseNuke
export function CL_ParseNuke(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const s = findFreeSustain(fx);
  if (s) {
    s.id = 21000;
    MSG_ReadPos(msg, s.org);
    s.endtime = c.cl.time + 1000;
    s.think = CL_Nukeblast;
    s.thinkinterval = 1;
    s.nextthink = c.cl.time;
  } else {
    // no free sustains -- FIXME - read the stuff anyway
    MSG_ReadPos(msg, fx.tent.pos);
  }
}

const splash_color = [0x00, 0xe0, 0xb0, 0x50, 0xd0, 0xe0, 0xe8] as const;

/** ent.angles[0..1] from an impact direction (TE_BLASTER / TE_BLASTER2 / TE_FLECHETTE) */
function impactAngles(ex: Explosion, dir: Float32Array): void {
  ex.ent.angles[0] = (Math.acos(dir[2]!) / M_PI) * 180;
  // PMM - fixed to correct for pitch of 0
  if (dir[0]) ex.ent.angles[1] = (Math.atan2(dir[1]!, dir[0]!) / M_PI) * 180;
  else if (dir[1]! > 0) ex.ent.angles[1] = 90;
  else if (dir[1]! < 0) ex.ent.angles[1] = 270;
  else ex.ent.angles[1] = 0;
}

// C: cl_tent.c:694 CL_ParseTEnt
export function CL_ParseTEnt(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const cl = c.cl;
  const t = fx.tent;
  const pos = t.pos;
  const pos2 = t.pos2;
  const dir = t.dir;
  const snd = c.sound;
  let ex: Explosion;
  let cnt: number;
  let color: number;
  let r: number;
  let ent: number;

  const type = MSG_ReadByte(msg);

  switch (type) {
    case TE_BLOOD: // bullet hitting flesh
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_ParticleEffect(fx, pos, dir, 0xe8, 60);
      break;

    case TE_GUNSHOT: // bullet hitting wall
    case TE_SPARKS:
    case TE_BULLET_SPARKS:
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      if (type === TE_GUNSHOT) CL_ParticleEffect(fx, pos, dir, 0, 40);
      else CL_ParticleEffect(fx, pos, dir, 0xe0, 6);

      if (type !== TE_SPARKS) {
        CL_SmokeAndFlash(fx, pos);

        // impact sound
        cnt = c.rand.rand() & 15;
        if (cnt === 1) snd.startSound(pos, 0, 0, t.cl_sfx_ric1, 1, ATTN_NORM, 0);
        else if (cnt === 2) snd.startSound(pos, 0, 0, t.cl_sfx_ric2, 1, ATTN_NORM, 0);
        else if (cnt === 3) snd.startSound(pos, 0, 0, t.cl_sfx_ric3, 1, ATTN_NORM, 0);
      }
      break;

    case TE_SCREEN_SPARKS:
    case TE_SHIELD_SPARKS:
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      if (type === TE_SCREEN_SPARKS) CL_ParticleEffect(fx, pos, dir, 0xd0, 40);
      else CL_ParticleEffect(fx, pos, dir, 0xb0, 40);
      //FIXME : replace or remove this sound
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_SHOTGUN: // bullet hitting wall
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_ParticleEffect(fx, pos, dir, 0, 20);
      CL_SmokeAndFlash(fx, pos);
      break;

    case TE_SPLASH: // bullet hitting water
      cnt = MSG_ReadByte(msg);
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      r = MSG_ReadByte(msg);
      if (r > 6) color = 0x00;
      else color = splash_color[r]!;
      CL_ParticleEffect(fx, pos, dir, color, cnt);

      if (r === SPLASH_SPARKS) {
        r = c.rand.rand() & 3;
        if (r === 0) snd.startSound(pos, 0, 0, t.cl_sfx_spark5, 1, ATTN_STATIC, 0);
        else if (r === 1) snd.startSound(pos, 0, 0, t.cl_sfx_spark6, 1, ATTN_STATIC, 0);
        else snd.startSound(pos, 0, 0, t.cl_sfx_spark7, 1, ATTN_STATIC, 0);
      }
      break;

    case TE_LASER_SPARKS:
      cnt = MSG_ReadByte(msg);
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      color = MSG_ReadByte(msg);
      CL_ParticleEffect2(fx, pos, dir, color, cnt);
      break;

    // RAFAEL
    case TE_BLUEHYPERBLASTER:
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, dir);
      CL_BlasterParticles(fx, pos, dir);
      break;

    case TE_BLASTER: // blaster hitting wall
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_BlasterParticles(fx, pos, dir);

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      impactAngles(ex, dir);
      ex.type = ex_misc;
      ex.ent.flags = RF_FULLBRIGHT | RF_TRANSLUCENT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 150;
      ex.lightcolor[0] = 1;
      ex.lightcolor[1] = 1;
      ex.ent.model = t.cl_mod_explode;
      ex.frames = 4;
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_RAILTRAIL: // railgun effect
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, pos2);
      CL_RailTrail(fx, pos, pos2);
      snd.startSound(pos2, 0, 0, t.cl_sfx_railg, 1, ATTN_NORM, 0);
      break;

    case TE_EXPLOSION2:
    case TE_GRENADE_EXPLOSION:
    case TE_GRENADE_EXPLOSION_WATER:
      MSG_ReadPos(msg, pos);

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_poly;
      ex.ent.flags = RF_FULLBRIGHT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 350;
      ex.lightcolor[0] = 1.0;
      ex.lightcolor[1] = 0.5;
      ex.lightcolor[2] = 0.5;
      ex.ent.model = t.cl_mod_explo4;
      ex.frames = 19;
      ex.baseframe = 30;
      ex.ent.angles[1] = c.rand.rand() % 360;
      CL_ExplosionParticles(fx, pos);
      if (type === TE_GRENADE_EXPLOSION_WATER) snd.startSound(pos, 0, 0, t.cl_sfx_watrexp, 1, ATTN_NORM, 0);
      else snd.startSound(pos, 0, 0, t.cl_sfx_grenexp, 1, ATTN_NORM, 0);
      break;

    // RAFAEL
    case TE_PLASMA_EXPLOSION:
      MSG_ReadPos(msg, pos);
      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_poly;
      ex.ent.flags = RF_FULLBRIGHT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 350;
      ex.lightcolor[0] = 1.0;
      ex.lightcolor[1] = 0.5;
      ex.lightcolor[2] = 0.5;
      ex.ent.angles[1] = c.rand.rand() % 360;
      ex.ent.model = t.cl_mod_explo4;
      if (frand(c) < 0.5) ex.baseframe = 15;
      ex.frames = 15;
      CL_ExplosionParticles(fx, pos);
      snd.startSound(pos, 0, 0, t.cl_sfx_rockexp, 1, ATTN_NORM, 0);
      break;

    case TE_EXPLOSION1:
    case TE_EXPLOSION1_BIG: // PMM
    case TE_ROCKET_EXPLOSION:
    case TE_ROCKET_EXPLOSION_WATER:
    case TE_EXPLOSION1_NP: // PMM
      MSG_ReadPos(msg, pos);

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_poly;
      ex.ent.flags = RF_FULLBRIGHT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 350;
      ex.lightcolor[0] = 1.0;
      ex.lightcolor[1] = 0.5;
      ex.lightcolor[2] = 0.5;
      ex.ent.angles[1] = c.rand.rand() % 360;
      if (type !== TE_EXPLOSION1_BIG)
        ex.ent.model = t.cl_mod_explo4; // PMM
      else ex.ent.model = t.cl_mod_explo4_big;
      if (frand(c) < 0.5) ex.baseframe = 15;
      ex.frames = 15;
      if (type !== TE_EXPLOSION1_BIG && type !== TE_EXPLOSION1_NP) CL_ExplosionParticles(fx, pos); // PMM
      if (type === TE_ROCKET_EXPLOSION_WATER) snd.startSound(pos, 0, 0, t.cl_sfx_watrexp, 1, ATTN_NORM, 0);
      else snd.startSound(pos, 0, 0, t.cl_sfx_rockexp, 1, ATTN_NORM, 0);
      break;

    case TE_BFG_EXPLOSION:
      MSG_ReadPos(msg, pos);
      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_poly;
      ex.ent.flags = RF_FULLBRIGHT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 350;
      ex.lightcolor[0] = 0.0;
      ex.lightcolor[1] = 1.0;
      ex.lightcolor[2] = 0.0;
      ex.ent.model = t.cl_mod_bfg_explo;
      ex.ent.flags |= RF_TRANSLUCENT;
      ex.ent.alpha = fr(0.3);
      ex.frames = 4;
      break;

    case TE_BFG_BIGEXPLOSION:
      MSG_ReadPos(msg, pos);
      CL_BFGExplosionParticles(fx, pos);
      break;

    case TE_BFG_LASER:
      CL_ParseLaser(fx, msg, 0xd0d1d2d3 | 0);
      break;

    case TE_BUBBLETRAIL:
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, pos2);
      CL_BubbleTrail(fx, pos, pos2);
      break;

    case TE_PARASITE_ATTACK:
    case TE_MEDIC_CABLE_ATTACK:
      CL_ParseBeam(fx, msg, t.cl_mod_parasite_segment);
      break;

    case TE_BOSSTPORT: // boss teleporting to station
      MSG_ReadPos(msg, pos);
      CL_BigTeleportParticles(fx, pos);
      snd.startSound(pos, 0, 0, snd.registerSound('misc/bigtele.wav'), 1, ATTN_NONE, 0);
      break;

    case TE_GRAPPLE_CABLE:
      CL_ParseBeam2(fx, msg, t.cl_mod_grapple_cable);
      break;

    // RAFAEL
    case TE_WELDING_SPARKS:
      cnt = MSG_ReadByte(msg);
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      color = MSG_ReadByte(msg);
      CL_ParticleEffect2(fx, pos, dir, color, cnt);

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_flash;
      // note to self
      // we need a better no draw flag
      ex.ent.flags = RF_BEAM;
      ex.start = fr(cl.frame.servertime - 0.1);
      ex.light = 100 + (c.rand.rand() % 75);
      ex.lightcolor[0] = 1.0;
      ex.lightcolor[1] = 1.0;
      ex.lightcolor[2] = 0.3;
      ex.ent.model = t.cl_mod_flash;
      ex.frames = 2;
      break;

    case TE_GREENBLOOD:
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_ParticleEffect2(fx, pos, dir, 0xdf, 30);
      break;

    // RAFAEL
    case TE_TUNNEL_SPARKS:
      cnt = MSG_ReadByte(msg);
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      color = MSG_ReadByte(msg);
      CL_ParticleEffect3(fx, pos, dir, color, cnt);
      break;

    //=============
    //PGM
    // PMM -following code integrated for flechette (different color)
    case TE_BLASTER2: // green blaster hitting wall
    case TE_FLECHETTE: // flechette
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);

      // PMM
      if (type === TE_BLASTER2) CL_BlasterParticles2(fx, pos, dir, 0xd0);
      else CL_BlasterParticles2(fx, pos, dir, 0x6f); // 75

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      impactAngles(ex, dir);
      ex.type = ex_misc;
      ex.ent.flags = RF_FULLBRIGHT | RF_TRANSLUCENT;

      // PMM
      if (type === TE_BLASTER2) ex.ent.skinnum = 1;
      else ex.ent.skinnum = 2; // flechette

      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 150;
      // PMM
      if (type === TE_BLASTER2) ex.lightcolor[1] = 1;
      else {
        // flechette
        ex.lightcolor[0] = 0.19;
        ex.lightcolor[1] = 0.41;
        ex.lightcolor[2] = 0.75;
      }
      ex.ent.model = t.cl_mod_explode;
      ex.frames = 4;
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_LIGHTNING:
      ent = CL_ParseLightning(fx, msg, t.cl_mod_lightning);
      // the sound follows entity `ent` (a signed short from the server); C drops later, in
      // CL_GetEntitySoundOrigin, for numbers outside cl_entities -- drop before queueing the sound
      if (ent < 0 || ent >= MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_ParseTEnt: bad lightning entity %i', ent);
      snd.startSound(null, ent, CHAN_WEAPON, t.cl_sfx_lightning, 1, ATTN_NORM, 0);
      break;

    case TE_DEBUGTRAIL:
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, pos2);
      CL_DebugTrail(fx, pos, pos2);
      break;

    case TE_PLAIN_EXPLOSION:
      MSG_ReadPos(msg, pos);

      ex = CL_AllocExplosion(fx);
      VectorCopy(pos, ex.ent.origin);
      ex.type = ex_poly;
      ex.ent.flags = RF_FULLBRIGHT;
      ex.start = fr(cl.frame.servertime - 100);
      ex.light = 350;
      ex.lightcolor[0] = 1.0;
      ex.lightcolor[1] = 0.5;
      ex.lightcolor[2] = 0.5;
      ex.ent.angles[1] = c.rand.rand() % 360;
      ex.ent.model = t.cl_mod_explo4;
      if (frand(c) < 0.5) ex.baseframe = 15;
      ex.frames = 15;
      // (type can never be TE_ROCKET_EXPLOSION_WATER here)
      snd.startSound(pos, 0, 0, t.cl_sfx_rockexp, 1, ATTN_NORM, 0);
      break;

    case TE_FLASHLIGHT:
      MSG_ReadPos(msg, pos);
      ent = MSG_ReadShort(msg);
      CL_Flashlight(fx, ent, pos);
      break;

    case TE_FORCEWALL:
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, pos2);
      color = MSG_ReadByte(msg);
      CL_ForceWall(fx, pos, pos2, color);
      break;

    case TE_HEATBEAM:
      CL_ParsePlayerBeam(fx, msg, t.cl_mod_heatbeam);
      break;

    case TE_MONSTER_HEATBEAM:
      CL_ParsePlayerBeam(fx, msg, t.cl_mod_monster_heatbeam);
      break;

    case TE_HEATBEAM_SPARKS:
      cnt = 50;
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      r = 8;
      color = r & 0xff;
      CL_ParticleSteamEffect(fx, pos, dir, color, cnt, 60);
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_HEATBEAM_STEAM:
      cnt = 20;
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      color = 0xe0;
      CL_ParticleSteamEffect(fx, pos, dir, color, cnt, 60);
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_STEAM:
      CL_ParseSteam(fx, msg);
      break;

    case TE_BUBBLETRAIL2:
      cnt = 8;
      MSG_ReadPos(msg, pos);
      MSG_ReadPos(msg, pos2);
      CL_BubbleTrail2(fx, pos, pos2, cnt);
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_MOREBLOOD:
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_ParticleEffect(fx, pos, dir, 0xe8, 250);
      break;

    case TE_CHAINFIST_SMOKE:
      dir[0] = 0;
      dir[1] = 0;
      dir[2] = 1;
      MSG_ReadPos(msg, pos);
      CL_ParticleSmokeEffect(fx, pos, dir, 0, 20, 20);
      break;

    case TE_ELECTRIC_SPARKS:
      MSG_ReadPos(msg, pos);
      MSG_ReadDir(msg, dir);
      CL_ParticleEffect(fx, pos, dir, 0x75, 40);
      //FIXME : replace or remove this sound
      snd.startSound(pos, 0, 0, t.cl_sfx_lashit, 1, ATTN_NORM, 0);
      break;

    case TE_TRACKER_EXPLOSION:
      MSG_ReadPos(msg, pos);
      CL_ColorFlash(fx, pos, 0, 150, -1, -1, -1);
      CL_ColorExplosionParticles(fx, pos, 0, 1);
      snd.startSound(pos, 0, 0, t.cl_sfx_disrexp, 1, ATTN_NORM, 0);
      break;

    case TE_TELEPORT_EFFECT:
    case TE_DBALL_GOAL:
      MSG_ReadPos(msg, pos);
      CL_TeleportParticles(fx, pos);
      break;

    case TE_WIDOWBEAMOUT:
      CL_ParseWidow(fx, msg);
      break;

    case TE_NUKEBLAST:
      CL_ParseNuke(fx, msg);
      break;

    case TE_WIDOWSPLASH:
      MSG_ReadPos(msg, pos);
      CL_WidowSplash(fx, pos);
      break;
    //PGM
    //==============

    default:
      Com_Error(c, ERR_DROP, 'CL_ParseTEnt: bad type');
  }
}

/** yaw/pitch of a beam direction, as in CL_AddBeams / CL_AddPlayerBeams. Returns [yaw, pitch] (float). */
function beamAngles(dist: Float32Array): [number, number] {
  let yaw: number;
  let pitch: number;
  if (dist[1] === 0 && dist[0] === 0) {
    yaw = 0;
    if (dist[2]! > 0) pitch = 90;
    else pitch = 270;
  } else {
    // PMM - fixed to correct for pitch of 0
    if (dist[0]) yaw = fr((Math.atan2(dist[1]!, dist[0]!) * 180) / M_PI);
    else if (dist[1]! > 0) yaw = 90;
    else yaw = 270;
    if (yaw < 0) yaw = fr(yaw + 360);

    const forward = fr(Math.sqrt(fr(fr(dist[0]! * dist[0]!) + fr(dist[1]! * dist[1]!))));
    pitch = fr((Math.atan2(dist[2]!, forward) * -180.0) / M_PI);
    if (pitch < 0) pitch = fr(pitch + 360.0);
  }
  return [yaw, pitch];
}

// C: cl_tent.c:1206 CL_AddBeams
export function CL_AddBeams(fx: FxState): void {
  const c = fx.c;
  const cl = c.cl;
  const t = fx.tent;
  const dist = t.dist;
  const org = t.org;
  const ent = t.ent;

  // update beams
  for (const b of t.cl_beams) {
    if (!b.model || b.endtime < cl.time) continue;

    // if coming from the player, update the start position
    if (b.entity === cl.playernum + 1) {
      // entity 0 is the world
      VectorCopy(cl.refdef.vieworg, b.start);
      b.start[2] = b.start[2]! - 22; // adjust for view height
    }
    VectorAdd(b.start, b.offset, org);

    // calculate pitch and yaw
    VectorSubtract(b.end, org, dist);
    const [yaw, pitch] = beamAngles(dist);

    // add new entities for the beams
    let d = VectorNormalize(dist);

    clearRefEntity(ent);
    let model_length: number;
    if (b.model === t.cl_mod_lightning) {
      model_length = 35.0;
      d = fr(d - 20.0); // correction so it doesn't end in middle of tesla
    } else {
      model_length = 30.0;
    }
    const steps = fr(Math.ceil(fr(d / model_length)));
    const len = fr(fr(d - model_length) / fr(steps - 1));

    // PMM - special case for lightning model .. if the real length is shorter than the model,
    // flip it around & draw it from the end to the start.  This prevents the model from going
    // through the tesla mine (instead it goes through the target)
    if (b.model === t.cl_mod_lightning && d <= model_length) {
      VectorCopy(b.end, ent.origin);
      ent.model = b.model;
      ent.flags = RF_FULLBRIGHT;
      ent.angles[0] = pitch;
      ent.angles[1] = yaw;
      ent.angles[2] = c.rand.rand() % 360;
      V_AddEntity(c, ent);
      return; // (sic) stops processing the remaining beams
    }
    while (d > 0) {
      VectorCopy(org, ent.origin);
      ent.model = b.model;
      if (b.model === t.cl_mod_lightning) {
        ent.flags = RF_FULLBRIGHT;
        ent.angles[0] = -pitch;
        ent.angles[1] = yaw + 180.0;
        ent.angles[2] = c.rand.rand() % 360;
      } else {
        ent.angles[0] = pitch;
        ent.angles[1] = yaw;
        ent.angles[2] = c.rand.rand() % 360;
      }
      V_AddEntity(c, ent);

      for (let j = 0; j < 3; j++) org[j] = org[j]! + fr(dist[j]! * len);
      d = fr(d - model_length);
    }
  }
}

// C: cl_tent.c:1346 CL_AddPlayerBeams -- ROGUE - draw player locked beams
export function CL_AddPlayerBeams(fx: FxState): void {
  const c = fx.c;
  const cl = c.cl;
  const t = fx.tent;
  const dist = t.dist;
  const org = t.org;
  const ent = t.ent;
  const f = t.f;
  const r = t.r;
  const u = t.u;
  const hand = c.cv.hand;
  let framenum = 0;

  // PMM
  let hand_multiplier: number;
  if (hand) {
    if (hand.value === 2) hand_multiplier = 0;
    else if (hand.value === 1) hand_multiplier = -1;
    else hand_multiplier = 1;
  } else {
    hand_multiplier = 1;
  }

  // update beams
  for (const b of t.cl_playerbeams) {
    if (!b.model || b.endtime < cl.time) continue;

    const isHeatbeam = !!t.cl_mod_heatbeam && b.model === t.cl_mod_heatbeam;
    if (isHeatbeam) {
      // if coming from the player, update the start position
      if (b.entity === cl.playernum + 1) {
        // set up gun position
        // code straight out of CL_AddViewWeapon
        const ps = cl.frame.playerstate;
        let oldframe = cl.frames[(cl.frame.serverframe - 1) & UPDATE_MASK]!;
        if (oldframe.serverframe !== cl.frame.serverframe - 1 || !oldframe.valid) oldframe = cl.frame; // previous frame was dropped or involid
        const ops = oldframe.playerstate;
        for (let j = 0; j < 3; j++) {
          b.start[j] =
            fr(cl.refdef.vieworg[j]! + ops.gunoffset[j]!) +
            fr(cl.lerpfrac * fr(ps.gunoffset[j]! - ops.gunoffset[j]!));
        }
        VectorMA(b.start, hand_multiplier * b.offset[0]!, cl.v_right, org);
        VectorMA(org, b.offset[1]!, cl.v_forward, org);
        VectorMA(org, b.offset[2]!, cl.v_up, org);
        if (hand && hand.value === 2) VectorMA(org, -1, cl.v_up, org);
        // FIXME - take these out when final
        VectorCopy(cl.v_right, r);
        VectorCopy(cl.v_forward, f);
        VectorCopy(cl.v_up, u);
      } else VectorCopy(b.start, org);
    } else {
      // if coming from the player, update the start position
      if (b.entity === cl.playernum + 1) {
        // entity 0 is the world
        VectorCopy(cl.refdef.vieworg, b.start);
        b.start[2] = b.start[2]! - 22; // adjust for view height
      }
      VectorAdd(b.start, b.offset, org);
    }

    // calculate pitch and yaw
    VectorSubtract(b.end, org, dist);

    // PMM
    if (isHeatbeam && b.entity === cl.playernum + 1) {
      const len = VectorLength(dist);
      VectorScale(f, len, dist);
      VectorMA(dist, hand_multiplier * b.offset[0]!, r, dist);
      VectorMA(dist, b.offset[1]!, f, dist);
      VectorMA(dist, b.offset[2]!, u, dist);
      if (hand && hand.value === 2) VectorMA(org, -1, cl.v_up, org);
    }
    // PMM

    const [yaw, pitch] = beamAngles(dist);

    if (isHeatbeam) {
      if (b.entity !== cl.playernum + 1) {
        framenum = 2;
        ent.angles[0] = -pitch;
        ent.angles[1] = yaw + 180.0;
        ent.angles[2] = 0;
        AngleVectors(ent.angles, f, r, u);

        // if it's a non-origin offset, it's a player, so use the hardcoded player offset
        if (!VectorCompare(b.offset, vec3_origin)) {
          VectorMA(org, fr(-b.offset[0]! + 1), r, org);
          VectorMA(org, -b.offset[1]!, f, org);
          VectorMA(org, fr(-b.offset[2]! - 10), u, org);
        } else {
          // if it's a monster, do the particle effect
          CL_MonsterPlasma_Shell(fx, b.start);
        }
      } else {
        framenum = 1;
      }
    }

    // if it's the heatbeam, draw the particle effect
    if (isHeatbeam && b.entity === cl.playernum + 1) CL_Heatbeam(fx, org, dist);

    // add new entities for the beams
    let d = VectorNormalize(dist);

    clearRefEntity(ent);
    let model_length: number;
    if (b.model === t.cl_mod_heatbeam) {
      model_length = 32.0;
    } else if (b.model === t.cl_mod_lightning) {
      model_length = 35.0;
      d = fr(d - 20.0); // correction so it doesn't end in middle of tesla
    } else {
      model_length = 30.0;
    }
    const steps = fr(Math.ceil(fr(d / model_length)));
    const len = fr(fr(d - model_length) / fr(steps - 1));

    // PMM - special case for lightning model .. if the real length is shorter than the model,
    // flip it around & draw it from the end to the start.  This prevents the model from going
    // through the tesla mine (instead it goes through the target)
    if (b.model === t.cl_mod_lightning && d <= model_length) {
      VectorCopy(b.end, ent.origin);
      ent.model = b.model;
      ent.flags = RF_FULLBRIGHT;
      ent.angles[0] = pitch;
      ent.angles[1] = yaw;
      ent.angles[2] = c.rand.rand() % 360;
      V_AddEntity(c, ent);
      return; // (sic)
    }
    while (d > 0) {
      VectorCopy(org, ent.origin);
      ent.model = b.model;
      if (isHeatbeam) {
        ent.flags = RF_FULLBRIGHT;
        ent.angles[0] = -pitch;
        ent.angles[1] = yaw + 180.0;
        ent.angles[2] = cl.time % 360;
        ent.frame = framenum;
      } else if (b.model === t.cl_mod_lightning) {
        ent.flags = RF_FULLBRIGHT;
        ent.angles[0] = -pitch;
        ent.angles[1] = yaw + 180.0;
        ent.angles[2] = c.rand.rand() % 360;
      } else {
        ent.angles[0] = pitch;
        ent.angles[1] = yaw;
        ent.angles[2] = c.rand.rand() % 360;
      }
      V_AddEntity(c, ent);

      for (let j = 0; j < 3; j++) org[j] = org[j]! + fr(dist[j]! * len);
      d = fr(d - model_length);
    }
  }
}

// C: cl_tent.c:1596 CL_AddExplosions
export function CL_AddExplosions(fx: FxState): void {
  const c = fx.c;
  const cl = c.cl;
  for (const ex of fx.tent.cl_explosions) {
    if (ex.type === ex_free) continue;
    const frac = fr(fr(fr(cl.time) - ex.start) / 100.0);
    let f = Math.floor(frac);

    const ent = ex.ent;

    switch (ex.type) {
      case ex_mflash:
        if (f >= ex.frames - 1) ex.type = ex_free;
        break;
      case ex_misc:
        if (f >= ex.frames - 1) {
          ex.type = ex_free;
          break;
        }
        ent.alpha = fr(1.0 - fr(frac / (ex.frames - 1)));
        break;
      case ex_flash:
        if (f >= 1) {
          ex.type = ex_free;
          break;
        }
        ent.alpha = 1.0;
        break;
      case ex_poly:
        if (f >= ex.frames - 1) {
          ex.type = ex_free;
          break;
        }

        ent.alpha = fr((16.0 - f) / 16.0);

        if (f < 10) {
          ent.skinnum = f >> 1;
          if (ent.skinnum < 0) ent.skinnum = 0;
        } else {
          ent.flags |= RF_TRANSLUCENT;
          if (f < 13) ent.skinnum = 5;
          else ent.skinnum = 6;
        }
        break;
      case ex_poly2:
        if (f >= ex.frames - 1) {
          ex.type = ex_free;
          break;
        }

        ent.alpha = fr((5.0 - f) / 5.0);
        ent.skinnum = 0;
        ent.flags |= RF_TRANSLUCENT;
        break;
    }

    if (ex.type === ex_free) continue;
    if (ex.light) {
      V_AddLight(
        c,
        ent.origin,
        fr(ex.light * ent.alpha),
        ex.lightcolor[0]!,
        ex.lightcolor[1]!,
        ex.lightcolor[2]!,
      );
    }

    VectorCopy(ent.origin, ent.oldorigin);

    if (f < 0) f = 0;
    ent.frame = ex.baseframe + f + 1;
    ent.oldframe = ex.baseframe + f;
    ent.backlerp = fr(1.0 - cl.lerpfrac);

    V_AddEntity(c, ent);
  }
}

// C: cl_tent.c:1700 CL_AddLasers
export function CL_AddLasers(fx: FxState): void {
  const c = fx.c;
  for (const l of fx.tent.cl_lasers) {
    if (l.endtime >= c.cl.time) V_AddEntity(c, l.ent);
  }
}

// C: cl_tent.c:1713 CL_ProcessSustain -- PMM - CL_Sustains
export function CL_ProcessSustain(fx: FxState): void {
  const time = fx.c.cl.time;
  for (const s of fx.tent.cl_sustains) {
    if (s.id) {
      if (s.endtime >= time && time >= s.nextthink) s.think!(fx, s);
      else if (s.endtime < time) s.id = 0;
    }
  }
}

// C: cl_tent.c:1736 CL_AddTEnts
export function CL_AddTEnts(fx: FxState): void {
  CL_AddBeams(fx);
  // PMM - draw plasma beams
  CL_AddPlayerBeams(fx);
  CL_AddExplosions(fx);
  CL_AddLasers(fx);
  // PMM - set up sustain
  CL_ProcessSustain(fx);
}
