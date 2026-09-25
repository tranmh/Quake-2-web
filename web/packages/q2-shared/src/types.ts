// Structures from game/q_shared.h. Float vectors are Float32Array (stores round to float32);
// `short` arrays are Int16Array (stores wrap).
import { MAX_STATS } from './generated/const';

export type Vec3 = Float32Array;

export function vec3(x = 0, y = 0, z = 0): Vec3 {
  const v = new Float32Array(3);
  v[0] = x;
  v[1] = y;
  v[2] = z;
  return v;
}

// C: q_shared.h cplane_t
export class CPlane {
  readonly normal: Vec3 = new Float32Array(3);
  /** float */
  dist = 0;
  /** byte */
  type = 0;
  /** byte: signx + (signy<<1) + (signz<<2) */
  signbits = 0;

  copyFrom(o: CPlane): this {
    this.normal[0] = o.normal[0]!;
    this.normal[1] = o.normal[1]!;
    this.normal[2] = o.normal[2]!;
    this.dist = o.dist;
    this.type = o.type;
    this.signbits = o.signbits;
    return this;
  }

  clear(): this {
    this.normal.fill(0);
    this.dist = 0;
    this.type = 0;
    this.signbits = 0;
    return this;
  }
}

// C: q_shared.h csurface_t (name is a Latin-1 byte string, at most 15 chars)
export class CSurface {
  constructor(
    public name = '',
    public flags = 0,
    public value = 0,
  ) {}
}

// C: q_shared.h cmodel_t
export class CModel {
  readonly mins: Vec3 = new Float32Array(3);
  readonly maxs: Vec3 = new Float32Array(3);
  readonly origin: Vec3 = new Float32Array(3);
  headnode = 0;
}

// C: q_shared.h trace_t
export class Trace {
  allsolid = false;
  startsolid = false;
  /** float */
  fraction = 0;
  readonly endpos: Vec3 = new Float32Array(3);
  readonly plane = new CPlane();
  surface: CSurface | null = null;
  contents = 0;
  /** not set by CM_*() functions; opaque entity handle for callers */
  ent: unknown = null;

  copyFrom(o: Trace): this {
    this.allsolid = o.allsolid;
    this.startsolid = o.startsolid;
    this.fraction = o.fraction;
    this.endpos.set(o.endpos);
    this.plane.copyFrom(o.plane);
    this.surface = o.surface;
    this.contents = o.contents;
    this.ent = o.ent;
    return this;
  }
}

// C: q_shared.h pmove_state_t
export class PmoveState {
  pm_type = 0;
  /** short[3], 12.3 fixed */
  readonly origin = new Int16Array(3);
  /** short[3], 12.3 fixed */
  readonly velocity = new Int16Array(3);
  /** byte */
  pm_flags = 0;
  /** byte, each unit = 8 ms */
  pm_time = 0;
  /** short */
  gravity = 0;
  /** short[3] */
  readonly delta_angles = new Int16Array(3);

  copyFrom(o: PmoveState): this {
    this.pm_type = o.pm_type;
    this.origin.set(o.origin);
    this.velocity.set(o.velocity);
    this.pm_flags = o.pm_flags;
    this.pm_time = o.pm_time;
    this.gravity = o.gravity;
    this.delta_angles.set(o.delta_angles);
    return this;
  }

  clear(): this {
    this.pm_type = 0;
    this.origin.fill(0);
    this.velocity.fill(0);
    this.pm_flags = 0;
    this.pm_time = 0;
    this.gravity = 0;
    this.delta_angles.fill(0);
    return this;
  }

  /** memcmp-style equality */
  equals(o: PmoveState): boolean {
    for (let i = 0; i < 3; i++) {
      if (this.origin[i] !== o.origin[i] || this.velocity[i] !== o.velocity[i]) return false;
      if (this.delta_angles[i] !== o.delta_angles[i]) return false;
    }
    return (
      this.pm_type === o.pm_type &&
      this.pm_flags === o.pm_flags &&
      this.pm_time === o.pm_time &&
      this.gravity === o.gravity
    );
  }
}

// C: q_shared.h usercmd_t
export class UserCmd {
  /** byte */
  msec = 0;
  /** byte */
  buttons = 0;
  /** short[3] */
  readonly angles = new Int16Array(3);
  /** short */
  forwardmove = 0;
  /** short */
  sidemove = 0;
  /** short */
  upmove = 0;
  /** byte */
  impulse = 0;
  /** byte */
  lightlevel = 0;

  copyFrom(o: UserCmd): this {
    this.msec = o.msec;
    this.buttons = o.buttons;
    this.angles.set(o.angles);
    this.forwardmove = o.forwardmove;
    this.sidemove = o.sidemove;
    this.upmove = o.upmove;
    this.impulse = o.impulse;
    this.lightlevel = o.lightlevel;
    return this;
  }

  clear(): this {
    this.msec = 0;
    this.buttons = 0;
    this.angles.fill(0);
    this.forwardmove = 0;
    this.sidemove = 0;
    this.upmove = 0;
    this.impulse = 0;
    this.lightlevel = 0;
    return this;
  }
}

// C: q_shared.h entity_state_t
export class EntityState {
  number = 0;
  readonly origin: Vec3 = new Float32Array(3);
  readonly angles: Vec3 = new Float32Array(3);
  readonly old_origin: Vec3 = new Float32Array(3);
  modelindex = 0;
  modelindex2 = 0;
  modelindex3 = 0;
  modelindex4 = 0;
  frame = 0;
  skinnum = 0;
  /** unsigned int */
  effects = 0;
  renderfx = 0;
  solid = 0;
  sound = 0;
  event = 0;

  copyFrom(o: EntityState): this {
    this.number = o.number;
    this.origin.set(o.origin);
    this.angles.set(o.angles);
    this.old_origin.set(o.old_origin);
    this.modelindex = o.modelindex;
    this.modelindex2 = o.modelindex2;
    this.modelindex3 = o.modelindex3;
    this.modelindex4 = o.modelindex4;
    this.frame = o.frame;
    this.skinnum = o.skinnum;
    this.effects = o.effects;
    this.renderfx = o.renderfx;
    this.solid = o.solid;
    this.sound = o.sound;
    this.event = o.event;
    return this;
  }

  clear(): this {
    return this.copyFrom(EMPTY_ENTITY_STATE);
  }
}

const EMPTY_ENTITY_STATE = new EntityState();

// C: q_shared.h player_state_t
export class PlayerState {
  readonly pmove = new PmoveState();
  readonly viewangles: Vec3 = new Float32Array(3);
  readonly viewoffset: Vec3 = new Float32Array(3);
  readonly kick_angles: Vec3 = new Float32Array(3);
  readonly gunangles: Vec3 = new Float32Array(3);
  readonly gunoffset: Vec3 = new Float32Array(3);
  gunindex = 0;
  gunframe = 0;
  readonly blend = new Float32Array(4);
  /** float */
  fov = 0;
  rdflags = 0;
  readonly stats = new Int16Array(MAX_STATS);

  copyFrom(o: PlayerState): this {
    this.pmove.copyFrom(o.pmove);
    this.viewangles.set(o.viewangles);
    this.viewoffset.set(o.viewoffset);
    this.kick_angles.set(o.kick_angles);
    this.gunangles.set(o.gunangles);
    this.gunoffset.set(o.gunoffset);
    this.gunindex = o.gunindex;
    this.gunframe = o.gunframe;
    this.blend.set(o.blend);
    this.fov = o.fov;
    this.rdflags = o.rdflags;
    this.stats.set(o.stats);
    return this;
  }

  clear(): this {
    return this.copyFrom(EMPTY_PLAYER_STATE);
  }
}

const EMPTY_PLAYER_STATE = new PlayerState();
