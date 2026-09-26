// Port of client/cl_fx.c -- entity effects parsing and management (dlights, lightstyles, muzzle flashes,
// particles). All C globals of cl_fx.c / cl_tent.c / cl_newfx.c live in one FxState instance; the ported
// functions keep their C names and take the state first. The Effects interface (effects.ts) is
// implemented by ClientEffects (cl_effects.ts) on top of these functions.
import {
  AngleVectors,
  ATTN_IDLE,
  ATTN_NONE,
  ATTN_NORM,
  BYTE_DIRS,
  cChar,
  CHAN_AUTO,
  CHAN_BODY,
  CHAN_WEAPON,
  CrossProduct,
  DotProduct,
  EF_GIB,
  EF_GREENGIB,
  EF_ROCKET,
  ERR_DROP,
  EV_FALL,
  EV_FALLFAR,
  EV_FALLSHORT,
  EV_FOOTSTEP,
  EV_ITEM_RESPAWN,
  EV_PLAYER_TELEPORT,
  M_PI,
  MAX_EDICTS,
  MAX_QPATH,
  MZ2_ACTOR_MACHINEGUN_1,
  MZ2_BOSS2_MACHINEGUN_L1,
  MZ2_BOSS2_MACHINEGUN_L2,
  MZ2_BOSS2_MACHINEGUN_L3,
  MZ2_BOSS2_MACHINEGUN_L4,
  MZ2_BOSS2_MACHINEGUN_L5,
  MZ2_BOSS2_MACHINEGUN_R1,
  MZ2_BOSS2_MACHINEGUN_R2,
  MZ2_BOSS2_MACHINEGUN_R3,
  MZ2_BOSS2_MACHINEGUN_R4,
  MZ2_BOSS2_MACHINEGUN_R5,
  MZ2_BOSS2_ROCKET_1,
  MZ2_BOSS2_ROCKET_2,
  MZ2_BOSS2_ROCKET_3,
  MZ2_BOSS2_ROCKET_4,
  MZ2_CARRIER_MACHINEGUN_L1,
  MZ2_CARRIER_MACHINEGUN_L2,
  MZ2_CARRIER_MACHINEGUN_R1,
  MZ2_CARRIER_MACHINEGUN_R2,
  MZ2_CARRIER_RAILGUN,
  MZ2_CARRIER_ROCKET_1,
  MZ2_CHICK_ROCKET_1,
  MZ2_DAEDALUS_BLASTER,
  MZ2_FLOAT_BLASTER_1,
  MZ2_FLYER_BLASTER_1,
  MZ2_FLYER_BLASTER_2,
  MZ2_GLADIATOR_RAILGUN_1,
  MZ2_GUNNER_GRENADE_1,
  MZ2_GUNNER_GRENADE_2,
  MZ2_GUNNER_GRENADE_3,
  MZ2_GUNNER_GRENADE_4,
  MZ2_GUNNER_MACHINEGUN_1,
  MZ2_GUNNER_MACHINEGUN_2,
  MZ2_GUNNER_MACHINEGUN_3,
  MZ2_GUNNER_MACHINEGUN_4,
  MZ2_GUNNER_MACHINEGUN_5,
  MZ2_GUNNER_MACHINEGUN_6,
  MZ2_GUNNER_MACHINEGUN_7,
  MZ2_GUNNER_MACHINEGUN_8,
  MZ2_HOVER_BLASTER_1,
  MZ2_INFANTRY_MACHINEGUN_1,
  MZ2_INFANTRY_MACHINEGUN_10,
  MZ2_INFANTRY_MACHINEGUN_11,
  MZ2_INFANTRY_MACHINEGUN_12,
  MZ2_INFANTRY_MACHINEGUN_13,
  MZ2_INFANTRY_MACHINEGUN_2,
  MZ2_INFANTRY_MACHINEGUN_3,
  MZ2_INFANTRY_MACHINEGUN_4,
  MZ2_INFANTRY_MACHINEGUN_5,
  MZ2_INFANTRY_MACHINEGUN_6,
  MZ2_INFANTRY_MACHINEGUN_7,
  MZ2_INFANTRY_MACHINEGUN_8,
  MZ2_INFANTRY_MACHINEGUN_9,
  MZ2_JORG_BFG_1,
  MZ2_JORG_MACHINEGUN_L1,
  MZ2_JORG_MACHINEGUN_L2,
  MZ2_JORG_MACHINEGUN_L3,
  MZ2_JORG_MACHINEGUN_L4,
  MZ2_JORG_MACHINEGUN_L5,
  MZ2_JORG_MACHINEGUN_L6,
  MZ2_JORG_MACHINEGUN_R1,
  MZ2_JORG_MACHINEGUN_R2,
  MZ2_JORG_MACHINEGUN_R3,
  MZ2_JORG_MACHINEGUN_R4,
  MZ2_JORG_MACHINEGUN_R5,
  MZ2_JORG_MACHINEGUN_R6,
  MZ2_MAKRON_BFG,
  MZ2_MAKRON_BLASTER_1,
  MZ2_MAKRON_BLASTER_10,
  MZ2_MAKRON_BLASTER_11,
  MZ2_MAKRON_BLASTER_12,
  MZ2_MAKRON_BLASTER_13,
  MZ2_MAKRON_BLASTER_14,
  MZ2_MAKRON_BLASTER_15,
  MZ2_MAKRON_BLASTER_16,
  MZ2_MAKRON_BLASTER_17,
  MZ2_MAKRON_BLASTER_2,
  MZ2_MAKRON_BLASTER_3,
  MZ2_MAKRON_BLASTER_4,
  MZ2_MAKRON_BLASTER_5,
  MZ2_MAKRON_BLASTER_6,
  MZ2_MAKRON_BLASTER_7,
  MZ2_MAKRON_BLASTER_8,
  MZ2_MAKRON_BLASTER_9,
  MZ2_MEDIC_BLASTER_1,
  MZ2_MEDIC_BLASTER_2,
  MZ2_SOLDIER_BLASTER_1,
  MZ2_SOLDIER_BLASTER_2,
  MZ2_SOLDIER_BLASTER_3,
  MZ2_SOLDIER_BLASTER_4,
  MZ2_SOLDIER_BLASTER_5,
  MZ2_SOLDIER_BLASTER_6,
  MZ2_SOLDIER_BLASTER_7,
  MZ2_SOLDIER_BLASTER_8,
  MZ2_SOLDIER_MACHINEGUN_1,
  MZ2_SOLDIER_MACHINEGUN_2,
  MZ2_SOLDIER_MACHINEGUN_3,
  MZ2_SOLDIER_MACHINEGUN_4,
  MZ2_SOLDIER_MACHINEGUN_5,
  MZ2_SOLDIER_MACHINEGUN_6,
  MZ2_SOLDIER_MACHINEGUN_7,
  MZ2_SOLDIER_MACHINEGUN_8,
  MZ2_SOLDIER_SHOTGUN_1,
  MZ2_SOLDIER_SHOTGUN_2,
  MZ2_SOLDIER_SHOTGUN_3,
  MZ2_SOLDIER_SHOTGUN_4,
  MZ2_SOLDIER_SHOTGUN_5,
  MZ2_SOLDIER_SHOTGUN_6,
  MZ2_SOLDIER_SHOTGUN_7,
  MZ2_SOLDIER_SHOTGUN_8,
  MZ2_STALKER_BLASTER,
  MZ2_SUPERTANK_MACHINEGUN_1,
  MZ2_SUPERTANK_MACHINEGUN_2,
  MZ2_SUPERTANK_MACHINEGUN_3,
  MZ2_SUPERTANK_MACHINEGUN_4,
  MZ2_SUPERTANK_MACHINEGUN_5,
  MZ2_SUPERTANK_MACHINEGUN_6,
  MZ2_SUPERTANK_ROCKET_1,
  MZ2_SUPERTANK_ROCKET_2,
  MZ2_SUPERTANK_ROCKET_3,
  MZ2_TANK_BLASTER_1,
  MZ2_TANK_BLASTER_2,
  MZ2_TANK_BLASTER_3,
  MZ2_TANK_MACHINEGUN_1,
  MZ2_TANK_MACHINEGUN_10,
  MZ2_TANK_MACHINEGUN_11,
  MZ2_TANK_MACHINEGUN_12,
  MZ2_TANK_MACHINEGUN_13,
  MZ2_TANK_MACHINEGUN_14,
  MZ2_TANK_MACHINEGUN_15,
  MZ2_TANK_MACHINEGUN_16,
  MZ2_TANK_MACHINEGUN_17,
  MZ2_TANK_MACHINEGUN_18,
  MZ2_TANK_MACHINEGUN_19,
  MZ2_TANK_MACHINEGUN_2,
  MZ2_TANK_MACHINEGUN_3,
  MZ2_TANK_MACHINEGUN_4,
  MZ2_TANK_MACHINEGUN_5,
  MZ2_TANK_MACHINEGUN_6,
  MZ2_TANK_MACHINEGUN_7,
  MZ2_TANK_MACHINEGUN_8,
  MZ2_TANK_MACHINEGUN_9,
  MZ2_TANK_ROCKET_1,
  MZ2_TANK_ROCKET_2,
  MZ2_TANK_ROCKET_3,
  MZ2_TURRET_BLASTER,
  MZ2_TURRET_MACHINEGUN,
  MZ2_TURRET_ROCKET,
  MZ2_WIDOW2_BEAM_SWEEP_1,
  MZ2_WIDOW2_BEAM_SWEEP_10,
  MZ2_WIDOW2_BEAM_SWEEP_11,
  MZ2_WIDOW2_BEAM_SWEEP_2,
  MZ2_WIDOW2_BEAM_SWEEP_3,
  MZ2_WIDOW2_BEAM_SWEEP_4,
  MZ2_WIDOW2_BEAM_SWEEP_5,
  MZ2_WIDOW2_BEAM_SWEEP_6,
  MZ2_WIDOW2_BEAM_SWEEP_7,
  MZ2_WIDOW2_BEAM_SWEEP_8,
  MZ2_WIDOW2_BEAM_SWEEP_9,
  MZ2_WIDOW2_BEAMER_1,
  MZ2_WIDOW2_BEAMER_2,
  MZ2_WIDOW2_BEAMER_3,
  MZ2_WIDOW2_BEAMER_4,
  MZ2_WIDOW2_BEAMER_5,
  MZ2_WIDOW_BLASTER,
  MZ2_WIDOW_BLASTER_0,
  MZ2_WIDOW_BLASTER_10,
  MZ2_WIDOW_BLASTER_100,
  MZ2_WIDOW_BLASTER_10L,
  MZ2_WIDOW_BLASTER_20,
  MZ2_WIDOW_BLASTER_20L,
  MZ2_WIDOW_BLASTER_30,
  MZ2_WIDOW_BLASTER_30L,
  MZ2_WIDOW_BLASTER_40,
  MZ2_WIDOW_BLASTER_40L,
  MZ2_WIDOW_BLASTER_50,
  MZ2_WIDOW_BLASTER_50L,
  MZ2_WIDOW_BLASTER_60,
  MZ2_WIDOW_BLASTER_60L,
  MZ2_WIDOW_BLASTER_70,
  MZ2_WIDOW_BLASTER_70L,
  MZ2_WIDOW_BLASTER_80,
  MZ2_WIDOW_BLASTER_90,
  MZ2_WIDOW_BLASTER_SWEEP1,
  MZ2_WIDOW_BLASTER_SWEEP2,
  MZ2_WIDOW_BLASTER_SWEEP3,
  MZ2_WIDOW_BLASTER_SWEEP4,
  MZ2_WIDOW_BLASTER_SWEEP5,
  MZ2_WIDOW_BLASTER_SWEEP6,
  MZ2_WIDOW_BLASTER_SWEEP7,
  MZ2_WIDOW_BLASTER_SWEEP8,
  MZ2_WIDOW_BLASTER_SWEEP9,
  MZ2_WIDOW_DISRUPTOR,
  MZ2_WIDOW_PLASMABEAM,
  MZ2_WIDOW_RAIL,
  MZ2_WIDOW_RUN_1,
  MZ2_WIDOW_RUN_2,
  MZ2_WIDOW_RUN_3,
  MZ2_WIDOW_RUN_4,
  MZ2_WIDOW_RUN_5,
  MZ2_WIDOW_RUN_6,
  MZ2_WIDOW_RUN_7,
  MZ2_WIDOW_RUN_8,
  MZ_BFG,
  MZ_BLASTER,
  MZ_BLASTER2,
  MZ_BLUEHYPERBLASTER,
  MZ_CHAINGUN1,
  MZ_CHAINGUN2,
  MZ_CHAINGUN3,
  MZ_ETF_RIFLE,
  MZ_GRENADE,
  MZ_HEATBEAM,
  MZ_HYPERBLASTER,
  MZ_IONRIPPER,
  MZ_LOGIN,
  MZ_LOGOUT,
  MZ_MACHINEGUN,
  MZ_NUKE1,
  MZ_NUKE2,
  MZ_NUKE4,
  MZ_NUKE8,
  MZ_PHALANX,
  MZ_RAILGUN,
  MZ_RESPAWN,
  MZ_ROCKET,
  MZ_SHOTGUN,
  MZ_SHOTGUN2,
  MZ_SILENCED,
  MZ_SSHOTGUN,
  MZ_TRACKER,
  NUMVERTEXNORMALS,
  sprintf,
  vec3_origin,
  VectorAdd,
  VectorClear,
  VectorCopy,
  VectorLength,
  VectorMA,
  VectorNormalize,
  VectorScale,
  VectorSubtract,
  type EntityState,
  MONSTER_FLASH_OFFSET,
} from 'q2-shared';
import { MAX_DLIGHTS, MAX_LIGHTSTYLES, MAX_PARTICLES, type RefEntity } from 'q2-ref';
import { Com_Error, type CEntity, type ClientContext } from './client';
import { MSG_ReadByte, MSG_ReadShort, type SizeBuf } from 'q2-protocol';
import { V_AddLight, V_AddLightStyle, V_AddParticle } from './cl_view';
import { CL_SmokeAndFlash, TEntState } from './cl_tent';

const fr = Math.fround;

// C: client.h
export const PARTICLE_GRAVITY = 40;
export const BLASTER_PARTICLE_COLOR = 0xe0;
export const INSTANT_PARTICLE = -10000.0;

// C: client.h cparticle_t (float fields hold float32 values)
export class CParticle {
  next: CParticle | null = null;
  time = 0;
  readonly org = new Float32Array(3);
  readonly vel = new Float32Array(3);
  readonly accel = new Float32Array(3);
  color = 0;
  colorvel = 0;
  alpha = 0;
  alphavel = 0;
}

// C: client.h cdlight_t
export class CDLight {
  key = 0; // so entities can reuse same entry
  readonly color = new Float32Array(3);
  readonly origin = new Float32Array(3);
  radius = 0;
  die = 0; // stop lighting after this time
  decay = 0; // drop this each second
  minlight = 0; // don't add when contributing less

  /** memset(dl, 0, sizeof(*dl)) */
  clear(): void {
    this.key = 0;
    this.color.fill(0);
    this.origin.fill(0);
    this.radius = 0;
    this.die = 0;
    this.decay = 0;
    this.minlight = 0;
  }
}

// C: cl_fx.c clightstyle_t
export class CLightStyle {
  length = 0;
  readonly value = new Float32Array(3);
  readonly map = new Float32Array(MAX_QPATH);
}

/** Globals of cl_fx.c (+ cl_tent.c / cl_newfx.c in `tent`). */
export class FxState {
  c!: ClientContext;

  // cl_fx.c
  /** static vec3_t avelocities[NUMVERTEXNORMALS] (flattened) */
  readonly avelocities = new Float32Array(NUMVERTEXNORMALS * 3);
  readonly cl_lightstyle: CLightStyle[] = Array.from({ length: MAX_LIGHTSTYLES }, () => new CLightStyle());
  lastofs = -1;
  readonly cl_dlights: CDLight[] = Array.from({ length: MAX_DLIGHTS }, () => new CDLight());
  active_particles: CParticle | null = null;
  free_particles: CParticle | null = null;
  readonly particles: CParticle[] = Array.from({ length: MAX_PARTICLES }, () => new CParticle());
  cl_numparticles = MAX_PARTICLES;
  /** CL_AddParticles scratch origin */
  readonly addOrg = new Float32Array(3);

  // cl_tent.c / cl_newfx.c
  readonly tent = new TEntState();
}

// C: common.c:1366 frand -- (rand()&32767)*(1.0/32767) computed in double, returned as float
export function frand(c: ClientContext): number {
  return fr((c.rand.rand() & 32767) * (1.0 / 32767));
}

// C: common.c:1371 crand
export function crand(c: ClientContext): number {
  return fr((c.rand.rand() & 32767) * (2.0 / 32767) - 1);
}

/**
 * The free-list pop used by every particle effect:
 *   if (!free_particles) return; p = free_particles; free_particles = p->next;
 *   p->next = active_particles; active_particles = p;
 * Returns null when the list is empty (callers `return` exactly where C does).
 * Note: the popped particle keeps its stale field values, like C.
 */
export function allocParticle(fx: FxState): CParticle | null {
  const p = fx.free_particles;
  if (!p) return null;
  fx.free_particles = p.next;
  p.next = fx.active_particles;
  fx.active_particles = p;
  return p;
}

/** C: game/m_flash.c monster_flash_offset[] (x,y,z interleaved), generated by genconst. */
export const monster_flash_offset: Float32Array = MONSTER_FLASH_OFFSET;

const rand = (fx: FxState): number => fx.c.rand.rand();

/*
==============================================================

LIGHT STYLE MANAGEMENT

==============================================================
*/

// C: cl_fx.c:55 CL_ClearLightStyles
export function CL_ClearLightStyles(fx: FxState): void {
  for (const ls of fx.cl_lightstyle) {
    ls.length = 0;
    ls.value.fill(0);
    ls.map.fill(0);
  }
  fx.lastofs = -1;
}

// C: cl_fx.c:66 CL_RunLightStyles
export function CL_RunLightStyles(fx: FxState): void {
  const ofs = Math.trunc(fx.c.cl.time / 100);
  if (ofs === fx.lastofs) return;
  fx.lastofs = ofs;

  for (let i = 0; i < MAX_LIGHTSTYLES; i++) {
    const ls = fx.cl_lightstyle[i]!;
    if (!ls.length) {
      ls.value[0] = ls.value[1] = ls.value[2] = 1.0;
      continue;
    }
    if (ls.length === 1) ls.value[0] = ls.value[1] = ls.value[2] = ls.map[0]!;
    else ls.value[0] = ls.value[1] = ls.value[2] = ls.map[ofs % ls.length]!;
  }
}

// C: cl_fx.c:92 CL_SetLightstyle
export function CL_SetLightstyle(fx: FxState, i: number): void {
  const s = fx.c.cl.configstrings[i + 32 /* CS_LIGHTS */]!;
  const j = s.length;
  if (j >= MAX_QPATH) Com_Error(fx.c, ERR_DROP, 'svc_lightstyle length=%i', j);
  const ls = fx.cl_lightstyle[i]!;
  ls.length = j;
  for (let k = 0; k < j; k++) ls.map[k] = fr((cChar(s.charCodeAt(k)) - 97) / 12); // (float)(s[k]-'a')/(float)('m'-'a')
}

// C: cl_fx.c:114 CL_AddLightStyles
export function CL_AddLightStyles(fx: FxState): void {
  for (let i = 0; i < MAX_LIGHTSTYLES; i++) {
    const ls = fx.cl_lightstyle[i]!;
    V_AddLightStyle(fx.c, i, ls.value[0]!, ls.value[1]!, ls.value[2]!);
  }
}

/*
==============================================================

DLIGHT MANAGEMENT

==============================================================
*/

// C: cl_fx.c:138 CL_ClearDlights
export function CL_ClearDlights(fx: FxState): void {
  for (const dl of fx.cl_dlights) dl.clear();
}

// C: cl_fx.c:149 CL_AllocDlight
export function CL_AllocDlight(fx: FxState, key: number): CDLight {
  // first look for an exact key match
  if (key) {
    for (let i = 0; i < MAX_DLIGHTS; i++) {
      const dl = fx.cl_dlights[i]!;
      if (dl.key === key) {
        dl.clear();
        dl.key = key;
        return dl;
      }
    }
  }
  // then look for anything else
  const time = fr(fx.c.cl.time);
  for (let i = 0; i < MAX_DLIGHTS; i++) {
    const dl = fx.cl_dlights[i]!;
    if (dl.die < time) {
      dl.clear();
      dl.key = key;
      return dl;
    }
  }
  const dl = fx.cl_dlights[0]!;
  dl.clear();
  dl.key = key;
  return dl;
}

// C: cl_fx.c:192 CL_NewDlight
export function CL_NewDlight(
  fx: FxState,
  key: number,
  x: number,
  y: number,
  z: number,
  radius: number,
  time: number,
): void {
  const dl = CL_AllocDlight(fx, key);
  dl.origin[0] = x;
  dl.origin[1] = y;
  dl.origin[2] = z;
  dl.radius = fr(radius);
  dl.die = fr(fr(fx.c.cl.time) + fr(time));
}

// C: cl_fx.c:211 CL_RunDLights
export function CL_RunDLights(fx: FxState): void {
  const time = fr(fx.c.cl.time);
  for (let i = 0; i < MAX_DLIGHTS; i++) {
    const dl = fx.cl_dlights[i]!;
    if (!dl.radius) continue;
    if (dl.die < time) {
      dl.radius = 0;
      return; // (sic) stops processing the remaining lights this frame
    }
    dl.radius = fr(dl.radius - fr(fx.c.cls.frametime * dl.decay));
    if (dl.radius < 0) dl.radius = 0;
  }
}

// C: cl_fx.c:812 CL_AddDLights -- the browser renderer is GL (vidref_val == VIDREF_GL); the software
// branch (negative lights turned into black) is not reachable.
export function CL_AddDLights(fx: FxState): void {
  for (let i = 0; i < MAX_DLIGHTS; i++) {
    const dl = fx.cl_dlights[i]!;
    if (!dl.radius) continue;
    V_AddLight(fx.c, dl.origin, dl.radius, dl.color[0]!, dl.color[1]!, dl.color[2]!);
  }
}

/*
==============================================================

PARTICLE MANAGEMENT

==============================================================
*/

// C: cl_fx.c:896 CL_ClearParticles
export function CL_ClearParticles(fx: FxState): void {
  fx.free_particles = fx.particles[0]!;
  fx.active_particles = null;
  for (let i = 0; i < fx.cl_numparticles; i++) fx.particles[i]!.next = fx.particles[i + 1] ?? null;
  fx.particles[fx.cl_numparticles - 1]!.next = null;
}

// C: cl_fx.c:916 CL_ParticleEffect -- wall impact puffs
export function CL_ParticleEffect(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
): void {
  const c = fx.c;
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = fr(color + (rand(fx) & 7));
    const d = rand(fx) & 31;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((rand(fx) & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = fr(crand(c) * 20);
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:955 CL_ParticleEffect2
export function CL_ParticleEffect2(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
): void {
  const c = fx.c;
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = color;
    const d = rand(fx) & 7;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((rand(fx) & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = fr(crand(c) * 20);
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:995 CL_ParticleEffect3 (RAFAEL)
export function CL_ParticleEffect3(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
): void {
  const c = fx.c;
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = color;
    const d = rand(fx) & 7;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((rand(fx) & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = fr(crand(c) * 20);
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:1033 CL_TeleporterParticles
export function CL_TeleporterParticles(fx: FxState, ent: EntityState): void {
  const c = fx.c;
  for (let i = 0; i < 8; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = 0xdb;
    for (let j = 0; j < 2; j++) {
      p.org[j] = fr(ent.origin[j]! - 16) + (rand(fx) & 31);
      p.vel[j] = fr(crand(c) * 14);
    }
    p.org[2] = fr(ent.origin[2]! - 8) + (rand(fx) & 7);
    p.vel[2] = 80 + (rand(fx) & 7);
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = -0.5;
  }
}

// C: cl_fx.c:1074 CL_LogoutEffect
export function CL_LogoutEffect(fx: FxState, org: ArrayLike<number>, type: number): void {
  const c = fx.c;
  for (let i = 0; i < 500; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    if (type === MZ_LOGIN)
      p.color = 0xd0 + (rand(fx) & 7); // green
    else if (type === MZ_LOGOUT)
      p.color = 0x40 + (rand(fx) & 7); // red
    else p.color = 0xe0 + (rand(fx) & 7); // yellow

    p.org[0] = fr(org[0]! - 16) + fr(frand(c) * 32);
    p.org[1] = fr(org[1]! - 16) + fr(frand(c) * 32);
    p.org[2] = fr(org[2]! - 24) + fr(frand(c) * 56);

    for (let j = 0; j < 3; j++) p.vel[j] = fr(crand(c) * 20);

    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1.0 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:1119 CL_ItemRespawnParticles
export function CL_ItemRespawnParticles(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  for (let i = 0; i < 64; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = 0xd4 + (rand(fx) & 3); // green
    p.org[0] = org[0]! + fr(crand(c) * 8);
    p.org[1] = org[1]! + fr(crand(c) * 8);
    p.org[2] = org[2]! + fr(crand(c) * 8);
    for (let j = 0; j < 3; j++) p.vel[j] = fr(crand(c) * 8);
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY * 0.2;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1.0 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:1158 CL_ExplosionParticles
export function CL_ExplosionParticles(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  for (let i = 0; i < 256; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = 0xe0 + (rand(fx) & 7);
    for (let j = 0; j < 3; j++) {
      p.org[j] = org[j]! + ((rand(fx) % 32) - 16);
      p.vel[j] = (rand(fx) % 384) - 192;
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-0.8 / (0.5 + frand(c) * 0.3));
  }
}

const colortable = [2 * 8, 13 * 8, 21 * 8, 18 * 8];

// C: cl_fx.c:1195 CL_BigTeleportParticles
export function CL_BigTeleportParticles(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  for (let i = 0; i < 4096; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = colortable[rand(fx) & 3]!;

    const angle = fr((M_PI * 2 * (rand(fx) & 1023)) / 1023.0);
    const dist = rand(fx) & 31;
    p.org[0] = org[0]! + Math.cos(angle) * dist;
    p.vel[0] = Math.cos(angle) * (70 + (rand(fx) & 63));
    p.accel[0] = -Math.cos(angle) * 100;

    p.org[1] = org[1]! + Math.sin(angle) * dist;
    p.vel[1] = Math.sin(angle) * (70 + (rand(fx) & 63));
    p.accel[1] = -Math.sin(angle) * 100;

    p.org[2] = fr(org[2]! + 8) + (rand(fx) % 90);
    p.vel[2] = -100 + (rand(fx) & 31);
    p.accel[2] = PARTICLE_GRAVITY * 4;
    p.alpha = 1.0;

    p.alphavel = fr(-0.3 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:1242 CL_BlasterParticles -- wall impact puffs
export function CL_BlasterParticles(fx: FxState, org: ArrayLike<number>, dir: ArrayLike<number>): void {
  const c = fx.c;
  const count = 40;
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = 0xe0 + (rand(fx) & 7);
    const d = rand(fx) & 15;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((rand(fx) & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = fr(dir[j]! * 30) + fr(crand(c) * 40);
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:1284 CL_BlasterTrail
export function CL_BlasterTrail(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  // FIXME: this is a really silly way to have a loop
  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.3 + frand(c) * 0.2));
    p.color = 0xe0;
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + crand(c);
      p.vel[j] = fr(crand(c) * 5);
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1335 CL_QuadTrail
export function CL_QuadTrail(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.8 + frand(c) * 0.2));
    p.color = 115;
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 16);
      p.vel[j] = fr(crand(c) * 5);
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1385 CL_FlagTrail
export function CL_FlagTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  color: number,
): void {
  const c = fx.c;
  color = fr(color);
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.8 + frand(c) * 0.2));
    p.color = color;
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 16);
      p.vel[j] = fr(crand(c) * 5);
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1435 CL_DiminishingTrail
export function CL_DiminishingTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  old: CEntity,
  flags: number,
): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);

  const dec = 0.5;
  VectorScale(vec, dec, vec);

  let orgscale: number;
  let velscale: number;
  if (old.trailcount > 900) {
    orgscale = 4;
    velscale = 15;
  } else if (old.trailcount > 800) {
    orgscale = 2;
    velscale = 10;
  } else {
    orgscale = 1;
    velscale = 5;
  }

  while (len > 0) {
    len = fr(len - dec);

    if (!fx.free_particles) return;

    // drop less particles as it flies
    if ((rand(fx) & 1023) < old.trailcount) {
      const p = allocParticle(fx)!;
      VectorClear(p.accel);
      p.time = fr(c.cl.time);

      if (flags & EF_GIB) {
        p.alpha = 1.0;
        p.alphavel = fr(-1.0 / (1 + frand(c) * 0.4));
        p.color = 0xe8 + (rand(fx) & 7);
        for (let j = 0; j < 3; j++) {
          p.org[j] = move[j]! + fr(crand(c) * orgscale);
          p.vel[j] = fr(crand(c) * velscale);
          p.accel[j] = 0;
        }
        p.vel[2] = p.vel[2]! - PARTICLE_GRAVITY;
      } else if (flags & EF_GREENGIB) {
        p.alpha = 1.0;
        p.alphavel = fr(-1.0 / (1 + frand(c) * 0.4));
        p.color = 0xdb + (rand(fx) & 7);
        for (let j = 0; j < 3; j++) {
          p.org[j] = move[j]! + fr(crand(c) * orgscale);
          p.vel[j] = fr(crand(c) * velscale);
          p.accel[j] = 0;
        }
        p.vel[2] = p.vel[2]! - PARTICLE_GRAVITY;
      } else {
        p.alpha = 1.0;
        p.alphavel = fr(-1.0 / (1 + frand(c) * 0.2));
        p.color = 4 + (rand(fx) & 7);
        for (let j = 0; j < 3; j++) {
          p.org[j] = move[j]! + fr(crand(c) * orgscale);
          p.vel[j] = fr(crand(c) * velscale);
        }
        p.accel[2] = 20;
      }
    }

    old.trailcount -= 5;
    if (old.trailcount < 100) old.trailcount = 100;
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1534 MakeNormalVectors
export function MakeNormalVectors(forward: ArrayLike<number>, right: Float32Array, up: Float32Array): void {
  // this rotate and negat guarantees a vector not colinear with the original
  right[1] = -forward[0]!;
  right[2] = forward[1]!;
  right[0] = forward[2]!;

  const d = DotProduct(right, forward);
  VectorMA(right, -d, forward, right);
  VectorNormalize(right);
  CrossProduct(right, forward, up);
}

// C: cl_fx.c:1556 CL_RocketTrail
export function CL_RocketTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  old: CEntity,
): void {
  const c = fx.c;
  // smoke
  CL_DiminishingTrail(fx, start, end, old, EF_ROCKET);

  // fire
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 1;
  VectorScale(vec, dec, vec);

  while (len > 0) {
    len = fr(len - dec);
    if (!fx.free_particles) return;
    if ((rand(fx) & 7) === 0) {
      const p = allocParticle(fx)!;
      VectorClear(p.accel);
      p.time = fr(c.cl.time);
      p.alpha = 1.0;
      p.alphavel = fr(-1.0 / (1 + frand(c) * 0.2));
      p.color = 0xdc + (rand(fx) & 3);
      for (let j = 0; j < 3; j++) {
        p.org[j] = move[j]! + fr(crand(c) * 5);
        p.vel[j] = fr(crand(c) * 20);
      }
      p.accel[2] = -PARTICLE_GRAVITY;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1613 CL_RailTrail
export function CL_RailTrail(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  const right = new Float32Array(3);
  const up = new Float32Array(3);
  const dir = new Float32Array(3);
  const clr = 0x74;

  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);

  MakeNormalVectors(vec, right, up);

  for (let i = 0; i < len; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    VectorClear(p.accel);

    const d = fr(i * 0.1);
    const cs = fr(Math.cos(d));
    const s = fr(Math.sin(d));

    VectorScale(right, cs, dir);
    VectorMA(dir, s, up, dir);

    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.2));
    p.color = clr + (rand(fx) & 7);
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(dir[j]! * 3);
      p.vel[j] = fr(dir[j]! * 6);
    }
    VectorAdd(move, vec, move);
  }

  const dec = 0.75;
  VectorScale(vec, dec, vec);
  VectorCopy(start, move);

  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    VectorClear(p.accel);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.6 + frand(c) * 0.2));
    p.color = (0x0 + rand(fx)) & 15; // (sic) precedence: (0x0 + rand()) & 15
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 3);
      p.vel[j] = fr(crand(c) * 3);
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1704 CL_IonripperTrail (RAFAEL)
export function CL_IonripperTrail(fx: FxState, start: ArrayLike<number>, ent: ArrayLike<number>): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  let left = 0;
  VectorCopy(start, move);
  VectorSubtract(ent, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 0.5;
    p.alphavel = fr(-1.0 / (0.3 + frand(c) * 0.2));
    p.color = 0xe4 + (rand(fx) & 3);
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]!;
      p.accel[j] = 0;
    }
    if (left) {
      left = 0;
      p.vel[0] = 10;
    } else {
      left = 1;
      p.vel[0] = -10;
    }
    p.vel[1] = 0;
    p.vel[2] = 0;
    VectorAdd(move, vec, move);
  }
}

// C: cl_fx.c:1768 CL_BubbleTrail
export function CL_BubbleTrail(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  const len = VectorNormalize(vec);
  const dec = 32;
  VectorScale(vec, dec, vec);

  for (let i = 0; i < len; i = Math.trunc(fr(i + dec))) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.2));
    p.color = 4 + (rand(fx) & 7);
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 2);
      p.vel[j] = fr(crand(c) * 5);
    }
    p.vel[2] = p.vel[2]! + 6;
    VectorAdd(move, vec, move);
  }
}

const BEAMLENGTH = 16;

/** The shared `if (!avelocities[0][0]) ...` initialisation of CL_FlyParticles / CL_BfgParticles. */
function initAvelocities(fx: FxState): void {
  if (!fx.avelocities[0]) {
    for (let i = 0; i < NUMVERTEXNORMALS * 3; i++) fx.avelocities[i] = (rand(fx) & 255) * 0.01;
  }
}

// C: cl_fx.c:1819 CL_FlyParticles
export function CL_FlyParticles(fx: FxState, origin: ArrayLike<number>, count: number): void {
  const c = fx.c;
  const forward = new Float32Array(3);
  const av = fx.avelocities;
  if (count > NUMVERTEXNORMALS) count = NUMVERTEXNORMALS;

  initAvelocities(fx);

  const ltime = fr(fr(c.cl.time) / 1000.0);
  for (let i = 0; i < count; i += 2) {
    let angle = fr(ltime * av[i * 3]!);
    const sy = fr(Math.sin(angle));
    const cy = fr(Math.cos(angle));
    angle = fr(ltime * av[i * 3 + 1]!);
    const sp = fr(Math.sin(angle));
    const cp = fr(Math.cos(angle));
    // (sr/cr are computed from avelocities[i][2] in C but never used)

    forward[0] = cp * cy;
    forward[1] = cp * sy;
    forward[2] = -sp;

    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);

    const dist = fr(Math.sin(fr(ltime + i)) * 64);
    for (let k = 0; k < 3; k++)
      p.org[k] = fr(origin[k]! + fr(BYTE_DIRS[i * 3 + k]! * dist)) + fr(forward[k]! * BEAMLENGTH);

    VectorClear(p.vel);
    VectorClear(p.accel);
    p.color = 0;
    p.colorvel = 0;
    p.alpha = 1;
    p.alphavel = -100;
  }
}

// C: cl_fx.c:1882 CL_FlyEffect
export function CL_FlyEffect(fx: FxState, ent: CEntity, origin: ArrayLike<number>): void {
  const time = fx.c.cl.time;
  let starttime: number;
  let count: number;
  if (ent.fly_stoptime < time) {
    starttime = time;
    ent.fly_stoptime = time + 60000;
  } else starttime = ent.fly_stoptime - 60000;

  let n = time - starttime;
  if (n < 20000) count = Math.trunc((n * 162) / 20000.0);
  else {
    n = ent.fly_stoptime - time;
    if (n < 20000) count = Math.trunc((n * 162) / 20000.0);
    else count = 162;
  }
  CL_FlyParticles(fx, origin, count);
}

// C: cl_fx.c:1921 CL_BfgParticles
export function CL_BfgParticles(fx: FxState, ent: RefEntity): void {
  const c = fx.c;
  const forward = new Float32Array(3);
  const v = new Float32Array(3);
  const av = fx.avelocities;

  initAvelocities(fx);

  const ltime = fr(fr(c.cl.time) / 1000.0);
  for (let i = 0; i < NUMVERTEXNORMALS; i++) {
    let angle = fr(ltime * av[i * 3]!);
    const sy = fr(Math.sin(angle));
    const cy = fr(Math.cos(angle));
    angle = fr(ltime * av[i * 3 + 1]!);
    const sp = fr(Math.sin(angle));
    const cp = fr(Math.cos(angle));

    forward[0] = cp * cy;
    forward[1] = cp * sy;
    forward[2] = -sp;

    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);

    let dist = fr(Math.sin(fr(ltime + i)) * 64);
    for (let k = 0; k < 3; k++)
      p.org[k] = fr(ent.origin[k]! + fr(BYTE_DIRS[i * 3 + k]! * dist)) + fr(forward[k]! * BEAMLENGTH);

    VectorClear(p.vel);
    VectorClear(p.accel);

    VectorSubtract(p.org, ent.origin, v);
    dist = fr(VectorLength(v) / 90.0);
    p.color = Math.floor(fr(0xd0 + fr(dist * 7)));
    p.colorvel = 0;
    p.alpha = fr(1.0 - dist);
    p.alphavel = -100;
  }
}

// C: cl_fx.c:1990 CL_TrapParticles (RAFAEL). Modifies ent.origin[2] temporarily like C.
export function CL_TrapParticles(fx: FxState, ent: RefEntity): void {
  const c = fx.c;
  const move = new Float32Array(3);
  const vec = new Float32Array(3);
  const start = new Float32Array(3);
  const end = new Float32Array(3);

  ent.origin[2] = ent.origin[2]! - 14;
  VectorCopy(ent.origin, start);
  VectorCopy(ent.origin, end);
  end[2] = end[2]! + 64;

  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  // FIXME: this is a really silly way to have a loop
  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.3 + frand(c) * 0.2));
    p.color = 0xe0;
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + crand(c);
      p.vel[j] = fr(crand(c) * 15);
      p.accel[j] = 0;
    }
    p.accel[2] = PARTICLE_GRAVITY;
    VectorAdd(move, vec, move);
  }

  const dir = new Float32Array(3);
  const org = new Float32Array(3);
  ent.origin[2] = ent.origin[2]! + 14;
  VectorCopy(ent.origin, org);

  for (let i = -2; i <= 2; i += 4)
    for (let j = -2; j <= 2; j += 4)
      for (let k = -2; k <= 4; k += 4) {
        const p = allocParticle(fx);
        if (!p) return;
        p.time = fr(c.cl.time);
        p.color = 0xe0 + (rand(fx) & 3);
        p.alpha = 1.0;
        p.alphavel = fr(-1.0 / (0.3 + (rand(fx) & 7) * 0.02));

        // gcc evaluates (rand()&23) before crand() here (checked with the oracle CFLAGS)
        let r = rand(fx) & 23;
        p.org[0] = fr(org[0]! + i) + fr(r * crand(c));
        r = rand(fx) & 23;
        p.org[1] = fr(org[1]! + j) + fr(r * crand(c));
        r = rand(fx) & 23;
        p.org[2] = fr(org[2]! + k) + fr(r * crand(c));

        dir[0] = j * 8;
        dir[1] = i * 8;
        dir[2] = k * 8;

        VectorNormalize(dir);
        const vel = (50 + rand(fx)) & 63; // (sic) precedence
        VectorScale(dir, vel, p.vel);

        p.accel[0] = p.accel[1] = 0;
        p.accel[2] = -PARTICLE_GRAVITY;
      }
}

// C: cl_fx.c:2097 CL_BFGExplosionParticles
export function CL_BFGExplosionParticles(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  for (let i = 0; i < 256; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = 0xd0 + (rand(fx) & 7);
    for (let j = 0; j < 3; j++) {
      p.org[j] = org[j]! + ((rand(fx) % 32) - 16);
      p.vel[j] = (rand(fx) % 384) - 192;
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-0.8 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_fx.c:2135 CL_TeleportParticles
export function CL_TeleportParticles(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  const dir = new Float32Array(3);
  for (let i = -16; i <= 16; i += 4)
    for (let j = -16; j <= 16; j += 4)
      for (let k = -16; k <= 32; k += 4) {
        const p = allocParticle(fx);
        if (!p) return;
        p.time = fr(c.cl.time);
        p.color = 7 + (rand(fx) & 7);
        p.alpha = 1.0;
        p.alphavel = fr(-1.0 / (0.3 + (rand(fx) & 7) * 0.02));

        p.org[0] = fr(org[0]! + i) + (rand(fx) & 3);
        p.org[1] = fr(org[1]! + j) + (rand(fx) & 3);
        p.org[2] = fr(org[2]! + k) + (rand(fx) & 3);

        dir[0] = j * 8;
        dir[1] = i * 8;
        dir[2] = k * 8;

        VectorNormalize(dir);
        const vel = 50 + (rand(fx) & 63);
        VectorScale(dir, vel, p.vel);

        p.accel[0] = p.accel[1] = 0;
        p.accel[2] = -PARTICLE_GRAVITY;
      }
}

// C: cl_fx.c:2182 CL_AddParticles
export function CL_AddParticles(fx: FxState): void {
  const c = fx.c;
  const org = fx.addOrg;
  let active: CParticle | null = null;
  let tail: CParticle | null = null;
  // `time` is not reset per particle: an INSTANT_PARTICLE uses the value left by the previous
  // particle (uninitialised in C for the first one; 0 here).
  let time = 0;
  let alpha: number;
  const cltime = fr(c.cl.time);

  let next: CParticle | null;
  for (let p = fx.active_particles; p; p = next) {
    next = p.next;

    // PMM - added INSTANT_PARTICLE handling for heat beam
    if (p.alphavel !== INSTANT_PARTICLE) {
      time = fr(fr(cltime - p.time) * 0.001);
      alpha = fr(p.alpha + fr(time * p.alphavel));
      if (alpha <= 0) {
        // faded out
        p.next = fx.free_particles;
        fx.free_particles = p;
        continue;
      }
    } else alpha = p.alpha;

    p.next = null;
    if (!tail) active = tail = p;
    else {
      tail.next = p;
      tail = p;
    }

    if (alpha > 1.0) alpha = 1;
    const color = Math.trunc(p.color);

    const time2 = fr(time * time);

    org[0] = fr(p.org[0]! + fr(p.vel[0]! * time)) + fr(p.accel[0]! * time2);
    org[1] = fr(p.org[1]! + fr(p.vel[1]! * time)) + fr(p.accel[1]! * time2);
    org[2] = fr(p.org[2]! + fr(p.vel[2]! * time)) + fr(p.accel[2]! * time2);

    V_AddParticle(c, org, color, alpha);
    // PMM
    if (p.alphavel === INSTANT_PARTICLE) {
      p.alphavel = 0.0;
      p.alpha = 0.0;
    }
  }
  fx.active_particles = active;
}

// C: cl_fx.c:238 CL_ParseMuzzleFlash
export function CL_ParseMuzzleFlash(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const cl = c.cl;
  const fv = new Float32Array(3);
  const rv = new Float32Array(3);
  let soundname: string;

  const i = MSG_ReadShort(msg);
  if (i < 1 || i >= MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_ParseMuzzleFlash: bad entity');

  let weapon = MSG_ReadByte(msg);
  const silenced = weapon & MZ_SILENCED;
  weapon &= ~MZ_SILENCED;

  const pl = c.cl_entities[i]!;

  const dl = CL_AllocDlight(fx, i);
  VectorCopy(pl.current.origin, dl.origin);
  AngleVectors(pl.current.angles, fv, rv, null);
  VectorMA(dl.origin, 18, fv, dl.origin);
  VectorMA(dl.origin, 16, rv, dl.origin);
  if (silenced) dl.radius = 100 + (rand(fx) & 31);
  else dl.radius = 200 + (rand(fx) & 31);
  dl.minlight = 32;
  dl.die = fr(cl.time); // + 0.1;

  const volume = silenced ? fr(0.2) : 1;

  const snd = (chan: number, name: string, vol: number, attn: number, ofs: number): void =>
    c.sound.startSound(null, i, chan, c.sound.registerSound(name), vol, attn, fr(ofs));

  switch (weapon) {
    case MZ_BLASTER:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/blastf1a.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_BLUEHYPERBLASTER:
      dl.color[0] = 0;
      dl.color[1] = 0;
      dl.color[2] = 1;
      snd(CHAN_WEAPON, 'weapons/hyprbf1a.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_HYPERBLASTER:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/hyprbf1a.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_MACHINEGUN:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0);
      break;
    case MZ_SHOTGUN:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/shotgf1b.wav', volume, ATTN_NORM, 0);
      snd(CHAN_AUTO, 'weapons/shotgr1b.wav', volume, ATTN_NORM, 0.1);
      break;
    case MZ_SSHOTGUN:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/sshotf1b.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_CHAINGUN1:
      dl.radius = 200 + (rand(fx) & 31);
      dl.color[0] = 1;
      dl.color[1] = 0.25;
      dl.color[2] = 0;
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0);
      break;
    case MZ_CHAINGUN2:
      dl.radius = 225 + (rand(fx) & 31);
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 0.1);
      // long delay
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0);
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0.05);
      break;
    case MZ_CHAINGUN3:
      dl.radius = 250 + (rand(fx) & 31);
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 0.1);
      // long delay
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0);
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0.033);
      soundname = sprintf('weapons/machgf%ib.wav', (rand(fx) % 5) + 1);
      snd(CHAN_WEAPON, soundname, volume, ATTN_NORM, 0.066);
      break;
    case MZ_RAILGUN:
      dl.color[0] = 0.5;
      dl.color[1] = 0.5;
      dl.color[2] = 1.0;
      snd(CHAN_WEAPON, 'weapons/railgf1a.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_ROCKET:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.2;
      snd(CHAN_WEAPON, 'weapons/rocklf1a.wav', volume, ATTN_NORM, 0);
      snd(CHAN_AUTO, 'weapons/rocklr1b.wav', volume, ATTN_NORM, 0.1);
      break;
    case MZ_GRENADE:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/grenlf1a.wav', volume, ATTN_NORM, 0);
      snd(CHAN_AUTO, 'weapons/grenlr1b.wav', volume, ATTN_NORM, 0.1);
      break;
    case MZ_BFG:
      dl.color[0] = 0;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/bfg__f1y.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_LOGIN:
      dl.color[0] = 0;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 1.0);
      snd(CHAN_WEAPON, 'weapons/grenlf1a.wav', 1, ATTN_NORM, 0);
      CL_LogoutEffect(fx, pl.current.origin, weapon);
      break;
    case MZ_LOGOUT:
      dl.color[0] = 1;
      dl.color[1] = 0;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 1.0);
      snd(CHAN_WEAPON, 'weapons/grenlf1a.wav', 1, ATTN_NORM, 0);
      CL_LogoutEffect(fx, pl.current.origin, weapon);
      break;
    case MZ_RESPAWN:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 1.0);
      snd(CHAN_WEAPON, 'weapons/grenlf1a.wav', 1, ATTN_NORM, 0);
      CL_LogoutEffect(fx, pl.current.origin, weapon);
      break;
    // RAFAEL
    case MZ_PHALANX:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.5;
      snd(CHAN_WEAPON, 'weapons/plasshot.wav', volume, ATTN_NORM, 0);
      break;
    // RAFAEL
    case MZ_IONRIPPER:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.5;
      snd(CHAN_WEAPON, 'weapons/rippfire.wav', volume, ATTN_NORM, 0);
      break;
    // ======================
    // PGM
    case MZ_ETF_RIFLE:
      dl.color[0] = 0.9;
      dl.color[1] = 0.7;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/nail1.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_SHOTGUN2:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'weapons/shotg2.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_HEATBEAM:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 100);
      //		S_StartSound (NULL, i, CHAN_WEAPON, S_RegisterSound("weapons/bfg__l1a.wav"), volume, ATTN_NORM, 0);
      break;
    case MZ_BLASTER2:
      dl.color[0] = 0;
      dl.color[1] = 1;
      dl.color[2] = 0;
      // FIXME - different sound for blaster2 ??
      snd(CHAN_WEAPON, 'weapons/blastf1a.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_TRACKER:
      // negative flashes handled the same in gl/soft until CL_AddDLights
      dl.color[0] = -1;
      dl.color[1] = -1;
      dl.color[2] = -1;
      snd(CHAN_WEAPON, 'weapons/disint2.wav', volume, ATTN_NORM, 0);
      break;
    case MZ_NUKE1:
      dl.color[0] = 1;
      dl.color[1] = 0;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 100);
      break;
    case MZ_NUKE2:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 100);
      break;
    case MZ_NUKE4:
      dl.color[0] = 0;
      dl.color[1] = 0;
      dl.color[2] = 1;
      dl.die = fr(cl.time + 100);
      break;
    case MZ_NUKE8:
      dl.color[0] = 0;
      dl.color[1] = 1;
      dl.color[2] = 1;
      dl.die = fr(cl.time + 100);
      break;
    // PGM
    // ======================
  }
}

// C: cl_fx.c:429 CL_ParseMuzzleFlash2
export function CL_ParseMuzzleFlash2(fx: FxState, msg: SizeBuf): void {
  const c = fx.c;
  const cl = c.cl;
  const origin = new Float32Array(3);
  const forward = new Float32Array(3);
  const right = new Float32Array(3);
  let soundname: string;

  const ent = MSG_ReadShort(msg);
  if (ent < 1 || ent >= MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_ParseMuzzleFlash2: bad entity');

  const flash_number = MSG_ReadByte(msg);

  // locate the origin
  const ce = c.cl_entities[ent]!.current;
  AngleVectors(ce.angles, forward, right, null);
  const o = flash_number * 3;
  const f0 = monster_flash_offset[o] ?? 0;
  const f1 = monster_flash_offset[o + 1] ?? 0;
  const f2 = monster_flash_offset[o + 2] ?? 0;
  origin[0] = fr(ce.origin[0]! + fr(forward[0]! * f0)) + fr(right[0]! * f1);
  origin[1] = fr(ce.origin[1]! + fr(forward[1]! * f0)) + fr(right[1]! * f1);
  origin[2] = fr(fr(ce.origin[2]! + fr(forward[2]! * f0)) + fr(right[2]! * f1)) + f2;

  const dl = CL_AllocDlight(fx, ent);
  VectorCopy(origin, dl.origin);
  dl.radius = 200 + (rand(fx) & 31);
  dl.minlight = 32;
  dl.die = fr(cl.time); // + 0.1;

  const snd = (chan: number, name: string, vol: number, attn: number, ofs: number): void =>
    c.sound.startSound(null, ent, chan, c.sound.registerSound(name), vol, attn, fr(ofs));

  switch (flash_number) {
    case MZ2_INFANTRY_MACHINEGUN_1:
    case MZ2_INFANTRY_MACHINEGUN_2:
    case MZ2_INFANTRY_MACHINEGUN_3:
    case MZ2_INFANTRY_MACHINEGUN_4:
    case MZ2_INFANTRY_MACHINEGUN_5:
    case MZ2_INFANTRY_MACHINEGUN_6:
    case MZ2_INFANTRY_MACHINEGUN_7:
    case MZ2_INFANTRY_MACHINEGUN_8:
    case MZ2_INFANTRY_MACHINEGUN_9:
    case MZ2_INFANTRY_MACHINEGUN_10:
    case MZ2_INFANTRY_MACHINEGUN_11:
    case MZ2_INFANTRY_MACHINEGUN_12:
    case MZ2_INFANTRY_MACHINEGUN_13:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'infantry/infatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_SOLDIER_MACHINEGUN_1:
    case MZ2_SOLDIER_MACHINEGUN_2:
    case MZ2_SOLDIER_MACHINEGUN_3:
    case MZ2_SOLDIER_MACHINEGUN_4:
    case MZ2_SOLDIER_MACHINEGUN_5:
    case MZ2_SOLDIER_MACHINEGUN_6:
    case MZ2_SOLDIER_MACHINEGUN_7:
    case MZ2_SOLDIER_MACHINEGUN_8:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'soldier/solatck3.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_GUNNER_MACHINEGUN_1:
    case MZ2_GUNNER_MACHINEGUN_2:
    case MZ2_GUNNER_MACHINEGUN_3:
    case MZ2_GUNNER_MACHINEGUN_4:
    case MZ2_GUNNER_MACHINEGUN_5:
    case MZ2_GUNNER_MACHINEGUN_6:
    case MZ2_GUNNER_MACHINEGUN_7:
    case MZ2_GUNNER_MACHINEGUN_8:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'gunner/gunatck2.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_ACTOR_MACHINEGUN_1:
    case MZ2_SUPERTANK_MACHINEGUN_1:
    case MZ2_SUPERTANK_MACHINEGUN_2:
    case MZ2_SUPERTANK_MACHINEGUN_3:
    case MZ2_SUPERTANK_MACHINEGUN_4:
    case MZ2_SUPERTANK_MACHINEGUN_5:
    case MZ2_SUPERTANK_MACHINEGUN_6:
    case MZ2_TURRET_MACHINEGUN: // PGM
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'infantry/infatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_BOSS2_MACHINEGUN_L1:
    case MZ2_BOSS2_MACHINEGUN_L2:
    case MZ2_BOSS2_MACHINEGUN_L3:
    case MZ2_BOSS2_MACHINEGUN_L4:
    case MZ2_BOSS2_MACHINEGUN_L5:
    case MZ2_CARRIER_MACHINEGUN_L1: // PMM
    case MZ2_CARRIER_MACHINEGUN_L2: // PMM
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'infantry/infatck1.wav', 1, ATTN_NONE, 0);
      break;
    case MZ2_SOLDIER_BLASTER_1:
    case MZ2_SOLDIER_BLASTER_2:
    case MZ2_SOLDIER_BLASTER_3:
    case MZ2_SOLDIER_BLASTER_4:
    case MZ2_SOLDIER_BLASTER_5:
    case MZ2_SOLDIER_BLASTER_6:
    case MZ2_SOLDIER_BLASTER_7:
    case MZ2_SOLDIER_BLASTER_8:
    case MZ2_TURRET_BLASTER: // PGM
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'soldier/solatck2.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_FLYER_BLASTER_1:
    case MZ2_FLYER_BLASTER_2:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'flyer/flyatck3.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_MEDIC_BLASTER_1:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'medic/medatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_HOVER_BLASTER_1:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'hover/hovatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_FLOAT_BLASTER_1:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'floater/fltatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_SOLDIER_SHOTGUN_1:
    case MZ2_SOLDIER_SHOTGUN_2:
    case MZ2_SOLDIER_SHOTGUN_3:
    case MZ2_SOLDIER_SHOTGUN_4:
    case MZ2_SOLDIER_SHOTGUN_5:
    case MZ2_SOLDIER_SHOTGUN_6:
    case MZ2_SOLDIER_SHOTGUN_7:
    case MZ2_SOLDIER_SHOTGUN_8:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'soldier/solatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_TANK_BLASTER_1:
    case MZ2_TANK_BLASTER_2:
    case MZ2_TANK_BLASTER_3:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'tank/tnkatck3.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_TANK_MACHINEGUN_1:
    case MZ2_TANK_MACHINEGUN_2:
    case MZ2_TANK_MACHINEGUN_3:
    case MZ2_TANK_MACHINEGUN_4:
    case MZ2_TANK_MACHINEGUN_5:
    case MZ2_TANK_MACHINEGUN_6:
    case MZ2_TANK_MACHINEGUN_7:
    case MZ2_TANK_MACHINEGUN_8:
    case MZ2_TANK_MACHINEGUN_9:
    case MZ2_TANK_MACHINEGUN_10:
    case MZ2_TANK_MACHINEGUN_11:
    case MZ2_TANK_MACHINEGUN_12:
    case MZ2_TANK_MACHINEGUN_13:
    case MZ2_TANK_MACHINEGUN_14:
    case MZ2_TANK_MACHINEGUN_15:
    case MZ2_TANK_MACHINEGUN_16:
    case MZ2_TANK_MACHINEGUN_17:
    case MZ2_TANK_MACHINEGUN_18:
    case MZ2_TANK_MACHINEGUN_19:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      soundname = sprintf('tank/tnkatk2%c.wav', 97 + (rand(fx) % 5));
      snd(CHAN_WEAPON, soundname, 1, ATTN_NORM, 0);
      break;
    case MZ2_CHICK_ROCKET_1:
    case MZ2_TURRET_ROCKET: // PGM
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.2;
      snd(CHAN_WEAPON, 'chick/chkatck2.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_TANK_ROCKET_1:
    case MZ2_TANK_ROCKET_2:
    case MZ2_TANK_ROCKET_3:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.2;
      snd(CHAN_WEAPON, 'tank/tnkatck1.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_SUPERTANK_ROCKET_1:
    case MZ2_SUPERTANK_ROCKET_2:
    case MZ2_SUPERTANK_ROCKET_3:
    case MZ2_BOSS2_ROCKET_1:
    case MZ2_BOSS2_ROCKET_2:
    case MZ2_BOSS2_ROCKET_3:
    case MZ2_BOSS2_ROCKET_4:
    case MZ2_CARRIER_ROCKET_1:
      //	case MZ2_CARRIER_ROCKET_2:
      //	case MZ2_CARRIER_ROCKET_3:
      //	case MZ2_CARRIER_ROCKET_4:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0.2;
      snd(CHAN_WEAPON, 'tank/rocket.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_GUNNER_GRENADE_1:
    case MZ2_GUNNER_GRENADE_2:
    case MZ2_GUNNER_GRENADE_3:
    case MZ2_GUNNER_GRENADE_4:
      dl.color[0] = 1;
      dl.color[1] = 0.5;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'gunner/gunatck3.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_GLADIATOR_RAILGUN_1:
    case MZ2_CARRIER_RAILGUN: // PMM
    case MZ2_WIDOW_RAIL: // pmm
      dl.color[0] = 0.5;
      dl.color[1] = 0.5;
      dl.color[2] = 1.0;
      break;
    // --- Xian's shit starts ---
    case MZ2_MAKRON_BFG:
      dl.color[0] = 0.5;
      dl.color[1] = 1;
      dl.color[2] = 0.5;
      //S_StartSound (NULL, ent, CHAN_WEAPON, S_RegisterSound("makron/bfg_fire.wav"), 1, ATTN_NORM, 0);
      break;
    case MZ2_MAKRON_BLASTER_1:
    case MZ2_MAKRON_BLASTER_2:
    case MZ2_MAKRON_BLASTER_3:
    case MZ2_MAKRON_BLASTER_4:
    case MZ2_MAKRON_BLASTER_5:
    case MZ2_MAKRON_BLASTER_6:
    case MZ2_MAKRON_BLASTER_7:
    case MZ2_MAKRON_BLASTER_8:
    case MZ2_MAKRON_BLASTER_9:
    case MZ2_MAKRON_BLASTER_10:
    case MZ2_MAKRON_BLASTER_11:
    case MZ2_MAKRON_BLASTER_12:
    case MZ2_MAKRON_BLASTER_13:
    case MZ2_MAKRON_BLASTER_14:
    case MZ2_MAKRON_BLASTER_15:
    case MZ2_MAKRON_BLASTER_16:
    case MZ2_MAKRON_BLASTER_17:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'makron/blaster.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_JORG_MACHINEGUN_L1:
    case MZ2_JORG_MACHINEGUN_L2:
    case MZ2_JORG_MACHINEGUN_L3:
    case MZ2_JORG_MACHINEGUN_L4:
    case MZ2_JORG_MACHINEGUN_L5:
    case MZ2_JORG_MACHINEGUN_L6:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      snd(CHAN_WEAPON, 'boss3/xfire.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_JORG_MACHINEGUN_R1:
    case MZ2_JORG_MACHINEGUN_R2:
    case MZ2_JORG_MACHINEGUN_R3:
    case MZ2_JORG_MACHINEGUN_R4:
    case MZ2_JORG_MACHINEGUN_R5:
    case MZ2_JORG_MACHINEGUN_R6:
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      break;
    case MZ2_JORG_BFG_1:
      dl.color[0] = 0.5;
      dl.color[1] = 1;
      dl.color[2] = 0.5;
      break;
    case MZ2_BOSS2_MACHINEGUN_R1:
    case MZ2_BOSS2_MACHINEGUN_R2:
    case MZ2_BOSS2_MACHINEGUN_R3:
    case MZ2_BOSS2_MACHINEGUN_R4:
    case MZ2_BOSS2_MACHINEGUN_R5:
    case MZ2_CARRIER_MACHINEGUN_R1: // PMM
    case MZ2_CARRIER_MACHINEGUN_R2: // PMM
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      CL_ParticleEffect(fx, origin, vec3_origin, 0, 40);
      CL_SmokeAndFlash(fx, origin);
      break;
    // ======
    // ROGUE
    case MZ2_STALKER_BLASTER:
    case MZ2_DAEDALUS_BLASTER:
    case MZ2_MEDIC_BLASTER_2:
    case MZ2_WIDOW_BLASTER:
    case MZ2_WIDOW_BLASTER_SWEEP1:
    case MZ2_WIDOW_BLASTER_SWEEP2:
    case MZ2_WIDOW_BLASTER_SWEEP3:
    case MZ2_WIDOW_BLASTER_SWEEP4:
    case MZ2_WIDOW_BLASTER_SWEEP5:
    case MZ2_WIDOW_BLASTER_SWEEP6:
    case MZ2_WIDOW_BLASTER_SWEEP7:
    case MZ2_WIDOW_BLASTER_SWEEP8:
    case MZ2_WIDOW_BLASTER_SWEEP9:
    case MZ2_WIDOW_BLASTER_100:
    case MZ2_WIDOW_BLASTER_90:
    case MZ2_WIDOW_BLASTER_80:
    case MZ2_WIDOW_BLASTER_70:
    case MZ2_WIDOW_BLASTER_60:
    case MZ2_WIDOW_BLASTER_50:
    case MZ2_WIDOW_BLASTER_40:
    case MZ2_WIDOW_BLASTER_30:
    case MZ2_WIDOW_BLASTER_20:
    case MZ2_WIDOW_BLASTER_10:
    case MZ2_WIDOW_BLASTER_0:
    case MZ2_WIDOW_BLASTER_10L:
    case MZ2_WIDOW_BLASTER_20L:
    case MZ2_WIDOW_BLASTER_30L:
    case MZ2_WIDOW_BLASTER_40L:
    case MZ2_WIDOW_BLASTER_50L:
    case MZ2_WIDOW_BLASTER_60L:
    case MZ2_WIDOW_BLASTER_70L:
    case MZ2_WIDOW_RUN_1:
    case MZ2_WIDOW_RUN_2:
    case MZ2_WIDOW_RUN_3:
    case MZ2_WIDOW_RUN_4:
    case MZ2_WIDOW_RUN_5:
    case MZ2_WIDOW_RUN_6:
    case MZ2_WIDOW_RUN_7:
    case MZ2_WIDOW_RUN_8:
      dl.color[0] = 0;
      dl.color[1] = 1;
      dl.color[2] = 0;
      snd(CHAN_WEAPON, 'tank/tnkatck3.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_WIDOW_DISRUPTOR:
      dl.color[0] = -1;
      dl.color[1] = -1;
      dl.color[2] = -1;
      snd(CHAN_WEAPON, 'weapons/disint2.wav', 1, ATTN_NORM, 0);
      break;
    case MZ2_WIDOW_PLASMABEAM:
    case MZ2_WIDOW2_BEAMER_1:
    case MZ2_WIDOW2_BEAMER_2:
    case MZ2_WIDOW2_BEAMER_3:
    case MZ2_WIDOW2_BEAMER_4:
    case MZ2_WIDOW2_BEAMER_5:
    case MZ2_WIDOW2_BEAM_SWEEP_1:
    case MZ2_WIDOW2_BEAM_SWEEP_2:
    case MZ2_WIDOW2_BEAM_SWEEP_3:
    case MZ2_WIDOW2_BEAM_SWEEP_4:
    case MZ2_WIDOW2_BEAM_SWEEP_5:
    case MZ2_WIDOW2_BEAM_SWEEP_6:
    case MZ2_WIDOW2_BEAM_SWEEP_7:
    case MZ2_WIDOW2_BEAM_SWEEP_8:
    case MZ2_WIDOW2_BEAM_SWEEP_9:
    case MZ2_WIDOW2_BEAM_SWEEP_10:
    case MZ2_WIDOW2_BEAM_SWEEP_11:
      dl.radius = 300 + (rand(fx) & 100);
      dl.color[0] = 1;
      dl.color[1] = 1;
      dl.color[2] = 0;
      dl.die = fr(cl.time + 200);
      break;
    // ROGUE
    // ======
    // --- Xian's shit ends ---
  }
}

// C: cl_fx.c:2258 CL_EntityEvent -- an entity has just been parsed that has an event value.
// The female events are there for backwards compatability.
export function CL_EntityEvent(fx: FxState, ent: EntityState): void {
  const c = fx.c;
  const s = c.sound;
  switch (ent.event) {
    case EV_ITEM_RESPAWN:
      s.startSound(null, ent.number, CHAN_WEAPON, s.registerSound('items/respawn1.wav'), 1, ATTN_IDLE, 0);
      CL_ItemRespawnParticles(fx, ent.origin);
      break;
    case EV_PLAYER_TELEPORT:
      s.startSound(null, ent.number, CHAN_WEAPON, s.registerSound('misc/tele1.wav'), 1, ATTN_IDLE, 0);
      CL_TeleportParticles(fx, ent.origin);
      break;
    case EV_FOOTSTEP:
      if (c.cv.cl_footsteps.value)
        s.startSound(null, ent.number, CHAN_BODY, fx.tent.cl_sfx_footsteps[rand(fx) & 3]!, 1, ATTN_NORM, 0);
      break;
    case EV_FALLSHORT:
      s.startSound(null, ent.number, CHAN_AUTO, s.registerSound('player/land1.wav'), 1, ATTN_NORM, 0);
      break;
    case EV_FALL:
      s.startSound(null, ent.number, CHAN_AUTO, s.registerSound('*fall2.wav'), 1, ATTN_NORM, 0);
      break;
    case EV_FALLFAR:
      s.startSound(null, ent.number, CHAN_AUTO, s.registerSound('*fall1.wav'), 1, ATTN_NORM, 0);
      break;
  }
}

// C: cl_fx.c:2293 CL_ClearEffects
export function CL_ClearEffects(fx: FxState): void {
  CL_ClearParticles(fx);
  CL_ClearDlights(fx);
  CL_ClearLightStyles(fx);
}
