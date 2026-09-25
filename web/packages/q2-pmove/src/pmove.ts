// Port of qcommon/pmove.c. Must be bit-identical to the C code: every float store is rounded with
// fround, double literals (0.125, 0.001, 0.25, STOP_EPSILON 0.1, MIN_STEP_NORMAL 0.7...) and libm calls
// are evaluated in double, `(int)` casts truncate, `short`/`byte` fields wrap.
import {
  AngleVectors,
  CONTENTS_CURRENT_0,
  CONTENTS_CURRENT_180,
  CONTENTS_CURRENT_270,
  CONTENTS_CURRENT_90,
  CONTENTS_CURRENT_DOWN,
  CONTENTS_CURRENT_UP,
  CONTENTS_LADDER,
  CONTENTS_SLIME,
  CONTENTS_SOLID,
  CONTENTS_WATER,
  CPlane,
  type CSurface,
  MASK_CURRENT,
  MASK_WATER,
  MAXTOUCH,
  PITCH,
  PMF_DUCKED,
  PMF_JUMP_HELD,
  PMF_ON_GROUND,
  PMF_TIME_LAND,
  PMF_TIME_TELEPORT,
  PMF_TIME_WATERJUMP,
  PM_DEAD,
  PM_FREEZE,
  PM_GIB,
  PM_SPECTATOR,
  PmoveState,
  ROLL,
  SURF_SLICK,
  Trace,
  UserCmd,
  VectorLength,
  VectorNormalize,
  YAW,
  cInt,
  fr,
  vec3_origin,
  type Vec3,
} from 'q2-shared';

const STEPSIZE = 18;
const STOP_EPSILON = 0.1;
const MIN_STEP_NORMAL = 0.7;
const MAX_CLIP_PLANES = 5;

/** pmove_t trace callback: C returns trace_t by value; the result is copied immediately. */
export type PmTraceFn = (start: Vec3, mins: Vec3, maxs: Vec3, end: Vec3) => Trace;
export type PmPointContentsFn = (point: Vec3) => number;

// C: q_shared.h pmove_t
export class PmoveT {
  // state (in / out)
  readonly s = new PmoveState();
  // command (in)
  readonly cmd = new UserCmd();
  /** if s has been changed outside pmove */
  snapinitial = false;
  // results (out)
  numtouch = 0;
  readonly touchents: unknown[] = new Array<unknown>(MAXTOUCH).fill(null);
  readonly viewangles: Vec3 = new Float32Array(3);
  /** float */
  viewheight = 0;
  readonly mins: Vec3 = new Float32Array(3);
  readonly maxs: Vec3 = new Float32Array(3);
  /** opaque entity handle, null = none */
  groundentity: unknown = null;
  watertype = 0;
  waterlevel = 0;
  // callbacks to test the world
  trace: PmTraceFn;
  pointcontents: PmPointContentsFn;

  constructor(trace: PmTraceFn, pointcontents: PmPointContentsFn) {
    this.trace = trace;
    this.pointcontents = pointcontents;
  }
}

function dot(a: ArrayLike<number>, b: ArrayLike<number>): number {
  return fr(fr(fr(a[0]! * b[0]!) + fr(a[1]! * b[1]!)) + fr(a[2]! * b[2]!));
}

/**
 * Pmove context: the C globals `pm`, `pml` and the movement tunables. One instance per simulation
 * (client prediction / server); not reentrant.
 */
export class Pmove {
  // movement parameters (C: float globals)
  pm_stopspeed = 100;
  pm_maxspeed = 300;
  pm_duckspeed = 100;
  pm_accelerate = 10;
  /** set from CS_AIRACCEL / sv_airaccelerate */
  pm_airaccelerate = 0;
  pm_wateraccelerate = 10;
  pm_friction = 6;
  pm_waterfriction = 1;
  pm_waterspeed = 400;

  private pm!: PmoveT;

  // pml_t
  private readonly origin = new Float32Array(3); // full float precision
  private readonly velocity = new Float32Array(3); // full float precision
  private readonly forward = new Float32Array(3);
  private readonly right = new Float32Array(3);
  private readonly up = new Float32Array(3);
  private frametime = 0;
  private groundsurface: CSurface | null = null;
  private readonly groundplane = new CPlane();
  private groundcontents = 0;
  private readonly previous_origin = new Float32Array(3);
  private ladder = false;

  // scratch (C locals)
  private readonly t_trace = new Trace();
  private readonly s_planes: Float32Array[] = Array.from(
    { length: MAX_CLIP_PLANES },
    () => new Float32Array(3),
  );
  private readonly s_primal = new Float32Array(3);
  private readonly s_dir = new Float32Array(3);
  private readonly s_end = new Float32Array(3);
  private readonly s_start_o = new Float32Array(3);
  private readonly s_start_v = new Float32Array(3);
  private readonly s_down_o = new Float32Array(3);
  private readonly s_down_v = new Float32Array(3);
  private readonly s_upv = new Float32Array(3);
  private readonly s_down = new Float32Array(3);
  private readonly s_v = new Float32Array(3);
  private readonly s_wishvel = new Float32Array(3);
  private readonly s_wishdir = new Float32Array(3);
  private readonly s_point = new Float32Array(3);
  private readonly s_spot = new Float32Array(3);
  private readonly s_flatforward = new Float32Array(3);
  private readonly s_gp_origin = new Float32Array(3);
  private readonly s_gp_end = new Float32Array(3);
  private readonly s_angles = new Float32Array(3);
  private readonly s_base = new Int16Array(3);
  private readonly s_sign = new Int32Array(3);

  /** call pm.trace and copy the by-value result into `out` */
  private doTrace(start: Vec3, mins: Vec3, maxs: Vec3, end: Vec3, out: Trace): Trace {
    const t = this.pm.trace(start, mins, maxs, end);
    if (t !== out) out.copyFrom(t);
    return out;
  }

  // C: pmove.c:79 PM_ClipVelocity
  private clipVelocity(inp: Vec3, normal: ArrayLike<number>, out: Vec3, overbounce: number): void {
    overbounce = fr(overbounce);
    const backoff = fr(dot(inp, normal) * overbounce);
    for (let i = 0; i < 3; i++) {
      const change = fr(normal[i]! * backoff);
      out[i] = inp[i]! - change;
      if (out[i]! > -STOP_EPSILON && out[i]! < STOP_EPSILON) out[i] = 0;
    }
  }

  // C: pmove.c:110 PM_StepSlideMove_
  private stepSlideMove_(): void {
    const pm = this.pm;
    const numbumps = 4;
    const planes = this.s_planes;
    const primal_velocity = this.s_primal;
    const dir = this.s_dir;
    const end = this.s_end;
    const vel = this.velocity;
    const org = this.origin;

    primal_velocity.set(vel);
    let numplanes = 0;
    let time_left = this.frametime;

    for (let bumpcount = 0; bumpcount < numbumps; bumpcount++) {
      for (let i = 0; i < 3; i++) end[i] = org[i]! + fr(time_left * vel[i]!);

      const trace = this.doTrace(org, pm.mins, pm.maxs, end, this.t_trace);

      if (trace.allsolid) {
        // entity is trapped in another solid
        vel[2] = 0; // don't build up falling damage
        return;
      }

      if (trace.fraction > 0) {
        // actually covered some distance
        org.set(trace.endpos);
        numplanes = 0;
      }

      if (trace.fraction === 1) break; // moved the entire distance

      // save entity for contact
      if (pm.numtouch < MAXTOUCH && trace.ent) {
        pm.touchents[pm.numtouch] = trace.ent;
        pm.numtouch++;
      }

      time_left = fr(time_left - fr(time_left * trace.fraction));

      // slide along this plane
      if (numplanes >= MAX_CLIP_PLANES) {
        // this shouldn't really happen
        vel.set(vec3_origin);
        break;
      }

      planes[numplanes]!.set(trace.plane.normal);
      numplanes++;

      // modify original_velocity so it parallels all of the clip planes
      let i: number;
      for (i = 0; i < numplanes; i++) {
        this.clipVelocity(vel, planes[i]!, vel, 1.01);
        let j: number;
        for (j = 0; j < numplanes; j++) {
          if (j !== i) {
            if (dot(vel, planes[j]!) < 0) break; // not ok
          }
        }
        if (j === numplanes) break;
      }

      if (i !== numplanes) {
        // go along this plane
      } else {
        // go along the crease
        if (numplanes !== 2) {
          vel.set(vec3_origin);
          break;
        }
        const p0 = planes[0]!,
          p1 = planes[1]!;
        dir[0] = fr(p0[1]! * p1[2]!) - fr(p0[2]! * p1[1]!);
        dir[1] = fr(p0[2]! * p1[0]!) - fr(p0[0]! * p1[2]!);
        dir[2] = fr(p0[0]! * p1[1]!) - fr(p0[1]! * p1[0]!);
        const d = dot(dir, vel);
        vel[0] = dir[0]! * d;
        vel[1] = dir[1]! * d;
        vel[2] = dir[2]! * d;
      }

      // if velocity is against the original velocity, stop dead
      // to avoid tiny occilations in sloping corners
      if (dot(vel, primal_velocity) <= 0) {
        vel.set(vec3_origin);
        break;
      }
    }

    if (pm.s.pm_time) vel.set(primal_velocity);
  }

  // C: pmove.c:258 PM_StepSlideMove
  private stepSlideMove(): void {
    const pm = this.pm;
    const start_o = this.s_start_o,
      start_v = this.s_start_v,
      down_o = this.s_down_o,
      down_v = this.s_down_v,
      up = this.s_upv,
      down = this.s_down;
    const org = this.origin,
      vel = this.velocity;

    start_o.set(org);
    start_v.set(vel);

    this.stepSlideMove_();

    down_o.set(org);
    down_v.set(vel);

    up.set(start_o);
    up[2] = up[2]! + STEPSIZE;

    let trace = this.doTrace(up, pm.mins, pm.maxs, up, this.t_trace);
    if (trace.allsolid) return; // can't step up

    // try sliding above
    org.set(up);
    vel.set(start_v);

    this.stepSlideMove_();

    // push down the final amount
    down.set(org);
    down[2] = down[2]! - STEPSIZE;
    trace = this.doTrace(org, pm.mins, pm.maxs, down, this.t_trace);
    if (!trace.allsolid) org.set(trace.endpos);

    up.set(org);

    // decide which one went farther
    const dx = fr(down_o[0]! - start_o[0]!);
    const dy = fr(down_o[1]! - start_o[1]!);
    const down_dist = fr(fr(dx * dx) + fr(dy * dy));
    const ux = fr(up[0]! - start_o[0]!);
    const uy = fr(up[1]! - start_o[1]!);
    const up_dist = fr(fr(ux * ux) + fr(uy * uy));

    if (down_dist > up_dist || trace.plane.normal[2]! < MIN_STEP_NORMAL) {
      org.set(down_o);
      vel.set(down_v);
      return;
    }
    //!! Special case
    // if we were walking along a plane, then we need to copy the Z over
    vel[2] = down_v[2]!;
  }

  // C: pmove.c:333 PM_Friction
  private friction(): void {
    const pm = this.pm;
    const vel = this.velocity;
    const speed = fr(Math.sqrt(dot(vel, vel)));
    if (speed < 1) {
      vel[0] = 0;
      vel[1] = 0;
      return;
    }

    let drop = 0;

    // apply ground friction
    if ((pm.groundentity && this.groundsurface && !(this.groundsurface.flags & SURF_SLICK)) || this.ladder) {
      const friction = fr(this.pm_friction);
      const control = speed < this.pm_stopspeed ? fr(this.pm_stopspeed) : speed;
      drop = fr(drop + fr(fr(control * friction) * this.frametime));
    }

    // apply water friction
    if (pm.waterlevel && !this.ladder) {
      drop = fr(drop + fr(fr(fr(speed * fr(this.pm_waterfriction)) * pm.waterlevel) * this.frametime));
    }

    // scale the velocity
    let newspeed = fr(speed - drop);
    if (newspeed < 0) newspeed = 0;
    newspeed = fr(newspeed / speed);

    vel[0] = vel[0]! * newspeed;
    vel[1] = vel[1]! * newspeed;
    vel[2] = vel[2]! * newspeed;
  }

  // C: pmove.c:380 PM_Accelerate
  private accelerate(wishdir: Vec3, wishspeed: number, accel: number): void {
    wishspeed = fr(wishspeed);
    accel = fr(accel);
    const vel = this.velocity;
    const currentspeed = dot(vel, wishdir);
    const addspeed = fr(wishspeed - currentspeed);
    if (addspeed <= 0) return;
    let accelspeed = fr(fr(accel * this.frametime) * wishspeed);
    if (accelspeed > addspeed) accelspeed = addspeed;
    for (let i = 0; i < 3; i++) vel[i] = vel[i]! + fr(accelspeed * wishdir[i]!);
  }

  // C: pmove.c:397 PM_AirAccelerate
  private airAccelerate(wishdir: Vec3, wishspeed: number, accel: number): void {
    wishspeed = fr(wishspeed);
    accel = fr(accel);
    const vel = this.velocity;
    let wishspd = wishspeed;
    if (wishspd > 30) wishspd = 30;
    const currentspeed = dot(vel, wishdir);
    const addspeed = fr(wishspd - currentspeed);
    if (addspeed <= 0) return;
    let accelspeed = fr(fr(accel * wishspeed) * this.frametime);
    if (accelspeed > addspeed) accelspeed = addspeed;
    for (let i = 0; i < 3; i++) vel[i] = vel[i]! + fr(accelspeed * wishdir[i]!);
  }

  // C: pmove.c:420 PM_AddCurrents
  private addCurrents(wishvel: Vec3): void {
    const pm = this.pm;
    const v = this.s_v;

    // account for ladders
    if (this.ladder && Math.abs(this.velocity[2]!) <= 200) {
      if (pm.viewangles[PITCH]! <= -15 && pm.cmd.forwardmove > 0) wishvel[2] = 200;
      else if (pm.viewangles[PITCH]! >= 15 && pm.cmd.forwardmove > 0) wishvel[2] = -200;
      else if (pm.cmd.upmove > 0) wishvel[2] = 200;
      else if (pm.cmd.upmove < 0) wishvel[2] = -200;
      else wishvel[2] = 0;

      // limit horizontal speed when on a ladder
      if (wishvel[0]! < -25) wishvel[0] = -25;
      else if (wishvel[0]! > 25) wishvel[0] = 25;

      if (wishvel[1]! < -25) wishvel[1] = -25;
      else if (wishvel[1]! > 25) wishvel[1] = 25;
    }

    // add water currents
    if (pm.watertype & MASK_CURRENT) {
      v[0] = v[1] = v[2] = 0;
      if (pm.watertype & CONTENTS_CURRENT_0) v[0] = v[0]! + 1;
      if (pm.watertype & CONTENTS_CURRENT_90) v[1] = v[1]! + 1;
      if (pm.watertype & CONTENTS_CURRENT_180) v[0] = v[0]! - 1;
      if (pm.watertype & CONTENTS_CURRENT_270) v[1] = v[1]! - 1;
      if (pm.watertype & CONTENTS_CURRENT_UP) v[2] = v[2]! + 1;
      if (pm.watertype & CONTENTS_CURRENT_DOWN) v[2] = v[2]! - 1;

      let s = fr(this.pm_waterspeed);
      if (pm.waterlevel === 1 && pm.groundentity) s = fr(s / 2);

      vectorMA(wishvel, s, v, wishvel);
    }

    // add conveyor belt velocities
    if (pm.groundentity) {
      v[0] = v[1] = v[2] = 0;
      const gc = this.groundcontents;
      if (gc & CONTENTS_CURRENT_0) v[0] = v[0]! + 1;
      if (gc & CONTENTS_CURRENT_90) v[1] = v[1]! + 1;
      if (gc & CONTENTS_CURRENT_180) v[0] = v[0]! - 1;
      if (gc & CONTENTS_CURRENT_270) v[1] = v[1]! - 1;
      if (gc & CONTENTS_CURRENT_UP) v[2] = v[2]! + 1;
      if (gc & CONTENTS_CURRENT_DOWN) v[2] = v[2]! - 1;

      vectorMA(wishvel, 100 /* pm->groundentity->speed */, v, wishvel);
    }
  }

  // C: pmove.c:531 PM_WaterMove
  private waterMove(): void {
    const pm = this.pm;
    const wishvel = this.s_wishvel,
      wishdir = this.s_wishdir;
    const fwd = this.forward,
      rgt = this.right;

    // user intentions
    for (let i = 0; i < 3; i++) {
      wishvel[i] = fr(fwd[i]! * pm.cmd.forwardmove) + fr(rgt[i]! * pm.cmd.sidemove);
    }

    if (!pm.cmd.forwardmove && !pm.cmd.sidemove && !pm.cmd.upmove) {
      wishvel[2] = wishvel[2]! - 60; // drift towards bottom
    } else {
      wishvel[2] = wishvel[2]! + pm.cmd.upmove;
    }

    this.addCurrents(wishvel);

    wishdir.set(wishvel);
    let wishspeed = VectorNormalize(wishdir);

    const maxspeed = fr(this.pm_maxspeed);
    if (wishspeed > maxspeed) {
      const sc = fr(maxspeed / wishspeed);
      wishvel[0] = wishvel[0]! * sc;
      wishvel[1] = wishvel[1]! * sc;
      wishvel[2] = wishvel[2]! * sc;
      wishspeed = maxspeed;
    }
    wishspeed = fr(wishspeed * 0.5);

    this.accelerate(wishdir, wishspeed, this.pm_wateraccelerate);

    this.stepSlideMove();
  }

  // C: pmove.c:575 PM_AirMove
  private airMove(): void {
    const pm = this.pm;
    const wishvel = this.s_wishvel,
      wishdir = this.s_wishdir;
    const vel = this.velocity;
    const fwd = this.forward,
      rgt = this.right;

    const fmove = pm.cmd.forwardmove;
    const smove = pm.cmd.sidemove;

    for (let i = 0; i < 2; i++) wishvel[i] = fr(fwd[i]! * fmove) + fr(rgt[i]! * smove);
    wishvel[2] = 0;

    this.addCurrents(wishvel);

    wishdir.set(wishvel);
    let wishspeed = VectorNormalize(wishdir);

    // clamp to server defined max speed
    const maxspeed = fr(pm.s.pm_flags & PMF_DUCKED ? this.pm_duckspeed : this.pm_maxspeed);

    if (wishspeed > maxspeed) {
      const sc = fr(maxspeed / wishspeed);
      wishvel[0] = wishvel[0]! * sc;
      wishvel[1] = wishvel[1]! * sc;
      wishvel[2] = wishvel[2]! * sc;
      wishspeed = maxspeed;
    }

    const gravdt = fr(pm.s.gravity * this.frametime);
    if (this.ladder) {
      this.accelerate(wishdir, wishspeed, this.pm_accelerate);
      if (!wishvel[2]) {
        if (vel[2]! > 0) {
          vel[2] = vel[2]! - gravdt;
          if (vel[2]! < 0) vel[2] = 0;
        } else {
          vel[2] = vel[2]! + gravdt;
          if (vel[2]! > 0) vel[2] = 0;
        }
      }
      this.stepSlideMove();
    } else if (pm.groundentity) {
      // walking on ground
      vel[2] = 0; //!!! this is before the accel
      this.accelerate(wishdir, wishspeed, this.pm_accelerate);

      // PGM	-- fix for negative trigger_gravity fields
      if (pm.s.gravity > 0) vel[2] = 0;
      else vel[2] = vel[2]! - gravdt;

      if (!vel[0] && !vel[1]) return;
      this.stepSlideMove();
    } else {
      // not on ground, so little effect on velocity
      if (fr(this.pm_airaccelerate)) this.airAccelerate(wishdir, wishspeed, this.pm_accelerate);
      else this.accelerate(wishdir, wishspeed, 1);
      // add gravity
      vel[2] = vel[2]! - gravdt;
      this.stepSlideMove();
    }
  }

  // C: pmove.c:668 PM_CatagorizePosition
  private catagorizePosition(): void {
    const pm = this.pm;
    const point = this.s_point;
    const org = this.origin;

    // if the player hull point one unit down is solid, the player is on ground
    // see if standing on something solid
    point[0] = org[0]!;
    point[1] = org[1]!;
    point[2] = org[2]! - 0.25;
    if (this.velocity[2]! > 180) {
      //!!ZOID changed from 100 to 180 (ramp accel)
      pm.s.pm_flags &= ~PMF_ON_GROUND & 255;
      pm.groundentity = null;
    } else {
      const trace = this.doTrace(org, pm.mins, pm.maxs, point, this.t_trace);
      this.groundplane.copyFrom(trace.plane);
      this.groundsurface = trace.surface;
      this.groundcontents = trace.contents;

      if (!trace.ent || (trace.plane.normal[2]! < 0.7 && !trace.startsolid)) {
        pm.groundentity = null;
        pm.s.pm_flags &= ~PMF_ON_GROUND & 255;
      } else {
        pm.groundentity = trace.ent;

        // hitting solid ground will end a waterjump
        if (pm.s.pm_flags & PMF_TIME_WATERJUMP) {
          pm.s.pm_flags &= ~(PMF_TIME_WATERJUMP | PMF_TIME_LAND | PMF_TIME_TELEPORT) & 255;
          pm.s.pm_time = 0;
        }

        if (!(pm.s.pm_flags & PMF_ON_GROUND)) {
          // just hit the ground
          pm.s.pm_flags |= PMF_ON_GROUND;
          // don't do landing time if we were just going down a slope
          if (this.velocity[2]! < -200) {
            pm.s.pm_flags |= PMF_TIME_LAND;
            // don't allow another jump for a little while
            if (this.velocity[2]! < -400) pm.s.pm_time = 25;
            else pm.s.pm_time = 18;
          }
        }
      }

      if (pm.numtouch < MAXTOUCH && trace.ent) {
        pm.touchents[pm.numtouch] = trace.ent;
        pm.numtouch++;
      }
    }

    // get waterlevel, accounting for ducking
    pm.waterlevel = 0;
    pm.watertype = 0;

    const sample2 = cInt(fr(pm.viewheight - pm.mins[2]!));
    const sample1 = (sample2 / 2) | 0;

    point[2] = fr(org[2]! + pm.mins[2]!) + 1;
    let cont = pm.pointcontents(point);

    if (cont & MASK_WATER) {
      pm.watertype = cont;
      pm.waterlevel = 1;
      point[2] = fr(org[2]! + pm.mins[2]!) + fr(sample1);
      cont = pm.pointcontents(point);
      if (cont & MASK_WATER) {
        pm.waterlevel = 2;
        point[2] = fr(org[2]! + pm.mins[2]!) + fr(sample2);
        cont = pm.pointcontents(point);
        if (cont & MASK_WATER) pm.waterlevel = 3;
      }
    }
  }

  // C: pmove.c:774 PM_CheckJump
  private checkJump(): void {
    const pm = this.pm;
    const vel = this.velocity;
    if (pm.s.pm_flags & PMF_TIME_LAND) {
      // hasn't been long enough since landing to jump again
      return;
    }

    if (pm.cmd.upmove < 10) {
      // not holding jump
      pm.s.pm_flags &= ~PMF_JUMP_HELD & 255;
      return;
    }

    // must wait for jump to be released
    if (pm.s.pm_flags & PMF_JUMP_HELD) return;

    if (pm.s.pm_type === PM_DEAD) return;

    if (pm.waterlevel >= 2) {
      // swimming, not jumping
      pm.groundentity = null;

      if (vel[2]! <= -300) return;

      if (pm.watertype === CONTENTS_WATER) vel[2] = 100;
      else if (pm.watertype === CONTENTS_SLIME) vel[2] = 80;
      else vel[2] = 50;
      return;
    }

    if (pm.groundentity === null) return; // in air, so no effect

    pm.s.pm_flags |= PMF_JUMP_HELD;

    pm.groundentity = null;
    vel[2] = vel[2]! + 270;
    if (vel[2]! < 270) vel[2] = 270;
  }

  // C: pmove.c:826 PM_CheckSpecialMovement
  private checkSpecialMovement(): void {
    const pm = this.pm;
    const spot = this.s_spot;
    const flatforward = this.s_flatforward;
    const org = this.origin;

    if (pm.s.pm_time) return;

    this.ladder = false;

    // check for ladder
    flatforward[0] = this.forward[0]!;
    flatforward[1] = this.forward[1]!;
    flatforward[2] = 0;
    VectorNormalize(flatforward);

    vectorMA(org, 1, flatforward, spot);
    const trace = this.doTrace(org, pm.mins, pm.maxs, spot, this.t_trace);
    if (trace.fraction < 1 && trace.contents & CONTENTS_LADDER) this.ladder = true;

    // check for water jump
    if (pm.waterlevel !== 2) return;

    vectorMA(org, 30, flatforward, spot);
    spot[2] = spot[2]! + 4;
    let cont = pm.pointcontents(spot);
    if (!(cont & CONTENTS_SOLID)) return;

    spot[2] = spot[2]! + 16;
    cont = pm.pointcontents(spot);
    if (cont) return;
    // jump out of water
    const vel = this.velocity;
    vel[0] = flatforward[0]! * 50;
    vel[1] = flatforward[1]! * 50;
    vel[2] = flatforward[2]! * 50;
    vel[2] = 350;

    pm.s.pm_flags |= PMF_TIME_WATERJUMP;
    pm.s.pm_time = 255;
  }

  // C: pmove.c:876 PM_FlyMove
  private flyMove(doclip: boolean): void {
    const pm = this.pm;
    const vel = this.velocity;
    const wishvel = this.s_wishvel,
      wishdir = this.s_wishdir,
      end = this.s_end;

    pm.viewheight = 22;

    // friction
    const speed = VectorLength(vel);
    if (speed < 1) {
      vel.set(vec3_origin);
    } else {
      let drop = 0;
      const friction = fr(fr(this.pm_friction) * 1.5); // extra friction
      const control = speed < this.pm_stopspeed ? fr(this.pm_stopspeed) : speed;
      drop = fr(drop + fr(fr(control * friction) * this.frametime));

      // scale the velocity
      let newspeed = fr(speed - drop);
      if (newspeed < 0) newspeed = 0;
      newspeed = fr(newspeed / speed);

      vel[0] = vel[0]! * newspeed;
      vel[1] = vel[1]! * newspeed;
      vel[2] = vel[2]! * newspeed;
    }

    // accelerate
    const fmove = pm.cmd.forwardmove;
    const smove = pm.cmd.sidemove;

    VectorNormalize(this.forward);
    VectorNormalize(this.right);

    for (let i = 0; i < 3; i++) wishvel[i] = fr(this.forward[i]! * fmove) + fr(this.right[i]! * smove);
    wishvel[2] = wishvel[2]! + pm.cmd.upmove;

    wishdir.set(wishvel);
    let wishspeed = VectorNormalize(wishdir);

    // clamp to server defined max speed
    const maxspeed = fr(this.pm_maxspeed);
    if (wishspeed > maxspeed) {
      const sc = fr(maxspeed / wishspeed);
      wishvel[0] = wishvel[0]! * sc;
      wishvel[1] = wishvel[1]! * sc;
      wishvel[2] = wishvel[2]! * sc;
      wishspeed = maxspeed;
    }

    const currentspeed = dot(vel, wishdir);
    const addspeed = fr(wishspeed - currentspeed);
    if (addspeed <= 0) return;
    let accelspeed = fr(fr(fr(this.pm_accelerate) * this.frametime) * wishspeed);
    if (accelspeed > addspeed) accelspeed = addspeed;

    for (let i = 0; i < 3; i++) vel[i] = vel[i]! + fr(accelspeed * wishdir[i]!);

    if (doclip) {
      for (let i = 0; i < 3; i++) end[i] = this.origin[i]! + fr(this.frametime * vel[i]!);
      const trace = this.doTrace(this.origin, pm.mins, pm.maxs, end, this.t_trace);
      this.origin.set(trace.endpos);
    } else {
      // move
      vectorMA(this.origin, this.frametime, vel, this.origin);
    }
  }

  // C: pmove.c:966 PM_CheckDuck
  private checkDuck(): void {
    const pm = this.pm;
    pm.mins[0] = -16;
    pm.mins[1] = -16;
    pm.maxs[0] = 16;
    pm.maxs[1] = 16;

    if (pm.s.pm_type === PM_GIB) {
      pm.mins[2] = 0;
      pm.maxs[2] = 16;
      pm.viewheight = 8;
      return;
    }

    pm.mins[2] = -24;

    if (pm.s.pm_type === PM_DEAD) {
      pm.s.pm_flags |= PMF_DUCKED;
    } else if (pm.cmd.upmove < 0 && pm.s.pm_flags & PMF_ON_GROUND) {
      // duck
      pm.s.pm_flags |= PMF_DUCKED;
    } else {
      // stand up if possible
      if (pm.s.pm_flags & PMF_DUCKED) {
        // try to stand up
        pm.maxs[2] = 32;
        const trace = this.doTrace(this.origin, pm.mins, pm.maxs, this.origin, this.t_trace);
        if (!trace.allsolid) pm.s.pm_flags &= ~PMF_DUCKED & 255;
      }
    }

    if (pm.s.pm_flags & PMF_DUCKED) {
      pm.maxs[2] = 4;
      pm.viewheight = -2;
    } else {
      pm.maxs[2] = 32;
      pm.viewheight = 22;
    }
  }

  // C: pmove.c:1025 PM_DeadMove
  private deadMove(): void {
    const pm = this.pm;
    const vel = this.velocity;
    if (!pm.groundentity) return;

    // extra friction
    let forward = VectorLength(vel);
    forward = fr(forward - 20);
    if (forward <= 0) {
      vel[0] = vel[1] = vel[2] = 0;
    } else {
      VectorNormalize(vel);
      vel[0] = vel[0]! * forward;
      vel[1] = vel[1]! * forward;
      vel[2] = vel[2]! * forward;
    }
  }

  // C: pmove.c:1047 PM_GoodPosition
  private goodPosition(): boolean {
    const pm = this.pm;
    if (pm.s.pm_type === PM_SPECTATOR) return true;
    const origin = this.s_gp_origin,
      end = this.s_gp_end;
    for (let i = 0; i < 3; i++) origin[i] = end[i] = pm.s.origin[i]! * 0.125;
    const trace = this.doTrace(origin, pm.mins, pm.maxs, end, this.t_trace);
    return !trace.allsolid;
  }

  // C: pmove.c:1072 PM_SnapPosition
  private snapPosition(): void {
    const pm = this.pm;
    const sign = this.s_sign;
    const base = this.s_base;
    const s = pm.s;

    // snap velocity to eigths
    for (let i = 0; i < 3; i++) s.velocity[i] = cInt(fr(this.velocity[i]! * 8));

    for (let i = 0; i < 3; i++) {
      if (this.origin[i]! >= 0) sign[i] = 1;
      else sign[i] = -1;
      s.origin[i] = cInt(fr(this.origin[i]! * 8));
      if (s.origin[i]! * 0.125 === this.origin[i]) sign[i] = 0;
    }
    base.set(s.origin);

    // try all combinations
    for (let j = 0; j < 8; j++) {
      const bits = JITTERBITS[j]!;
      s.origin.set(base);
      for (let i = 0; i < 3; i++) if (bits & (1 << i)) s.origin[i] = s.origin[i]! + sign[i]!;

      if (this.goodPosition()) return;
    }

    // go back to the last position
    for (let i = 0; i < 3; i++) s.origin[i] = cInt(this.previous_origin[i]!);
  }

  // C: pmove.c:1150 PM_InitialSnapPosition
  private initialSnapPosition(): void {
    const pm = this.pm;
    const base = this.s_base;
    const s = pm.s;
    base.set(s.origin);

    for (let z = 0; z < 3; z++) {
      s.origin[2] = base[2]! + SNAP_OFFSET[z]!;
      for (let y = 0; y < 3; y++) {
        s.origin[1] = base[1]! + SNAP_OFFSET[y]!;
        for (let x = 0; x < 3; x++) {
          s.origin[0] = base[0]! + SNAP_OFFSET[x]!;
          if (this.goodPosition()) {
            this.origin[0] = s.origin[0]! * 0.125;
            this.origin[1] = s.origin[1]! * 0.125;
            this.origin[2] = s.origin[2]! * 0.125;
            this.previous_origin[0] = s.origin[0]!;
            this.previous_origin[1] = s.origin[1]!;
            this.previous_origin[2] = s.origin[2]!;
            return;
          }
        }
      }
    }
    // Com_DPrintf ("Bad InitialSnapPosition\n");
  }

  // C: pmove.c:1190 PM_ClampAngles
  private clampAngles(): void {
    const pm = this.pm;
    const va = pm.viewangles;
    if (pm.s.pm_flags & PMF_TIME_TELEPORT) {
      // (sic) the int sum is not wrapped to short here
      va[YAW] = (pm.cmd.angles[YAW]! + pm.s.delta_angles[YAW]!) * (360.0 / 65536);
      va[PITCH] = 0;
      va[ROLL] = 0;
    } else {
      // circularly clamp the angles with deltas
      for (let i = 0; i < 3; i++) {
        const temp = ((pm.cmd.angles[i]! + pm.s.delta_angles[i]!) << 16) >> 16;
        va[i] = temp * (360.0 / 65536);
      }

      // don't let the player look up or down more than 90 degrees
      if (va[PITCH]! > 89 && va[PITCH]! < 180) va[PITCH] = 89;
      else if (va[PITCH]! < 271 && va[PITCH]! >= 180) va[PITCH] = 271;
    }
    AngleVectors(va, this.forward, this.right, this.up);
  }

  // C: pmove.c:1227 Pmove
  run(pmove: PmoveT): void {
    const pm = (this.pm = pmove);

    // clear results
    pm.numtouch = 0;
    pm.viewangles.fill(0);
    pm.viewheight = 0;
    pm.groundentity = null;
    pm.watertype = 0;
    pm.waterlevel = 0;

    // clear all pmove local vars
    this.origin.fill(0);
    this.velocity.fill(0);
    this.forward.fill(0);
    this.right.fill(0);
    this.up.fill(0);
    this.frametime = 0;
    this.groundsurface = null;
    this.groundplane.clear();
    this.groundcontents = 0;
    this.previous_origin.fill(0);
    this.ladder = false;

    // normalize the C field types (byte/short) in case the caller stored out-of-range values
    const s = pm.s;
    s.pm_flags &= 255;
    s.pm_time &= 255;
    s.gravity = (s.gravity << 16) >> 16;
    const cmd = pm.cmd;
    cmd.msec &= 255;
    cmd.forwardmove = (cmd.forwardmove << 16) >> 16;
    cmd.sidemove = (cmd.sidemove << 16) >> 16;
    cmd.upmove = (cmd.upmove << 16) >> 16;

    // convert origin and velocity to float values
    for (let i = 0; i < 3; i++) {
      this.origin[i] = s.origin[i]! * 0.125;
      this.velocity[i] = s.velocity[i]! * 0.125;
      // save old org in case we get stuck
      this.previous_origin[i] = s.origin[i]!;
    }

    this.frametime = fr(cmd.msec * 0.001);

    this.clampAngles();

    if (s.pm_type === PM_SPECTATOR) {
      this.flyMove(false);
      this.snapPosition();
      return;
    }

    if (s.pm_type >= PM_DEAD) {
      cmd.forwardmove = 0;
      cmd.sidemove = 0;
      cmd.upmove = 0;
    }

    if (s.pm_type === PM_FREEZE) return; // no movement at all

    // set mins, maxs, and viewheight
    this.checkDuck();

    if (pm.snapinitial) this.initialSnapPosition();

    // set groundentity, watertype, and waterlevel
    this.catagorizePosition();

    if (s.pm_type === PM_DEAD) this.deadMove();

    this.checkSpecialMovement();

    // drop timing counter
    if (s.pm_time) {
      let msec = cmd.msec >> 3;
      if (!msec) msec = 1;
      if (msec >= s.pm_time) {
        s.pm_flags &= ~(PMF_TIME_WATERJUMP | PMF_TIME_LAND | PMF_TIME_TELEPORT) & 255;
        s.pm_time = 0;
      } else {
        s.pm_time = (s.pm_time - msec) & 255;
      }
    }

    if (s.pm_flags & PMF_TIME_TELEPORT) {
      // teleport pause stays exactly in place
    } else if (s.pm_flags & PMF_TIME_WATERJUMP) {
      // waterjump has no control, but falls
      const vel = this.velocity;
      vel[2] = vel[2]! - fr(s.gravity * this.frametime);
      if (vel[2]! < 0) {
        // cancel as soon as we are falling down again
        s.pm_flags &= ~(PMF_TIME_WATERJUMP | PMF_TIME_LAND | PMF_TIME_TELEPORT) & 255;
        s.pm_time = 0;
      }
      this.stepSlideMove();
    } else {
      this.checkJump();
      this.friction();

      if (pm.waterlevel >= 2) {
        this.waterMove();
      } else {
        const angles = this.s_angles;
        angles.set(pm.viewangles);
        if (angles[PITCH]! > 180) angles[PITCH] = angles[PITCH]! - 360;
        angles[PITCH] = angles[PITCH]! / 3;

        AngleVectors(angles, this.forward, this.right, this.up);

        this.airMove();
      }
    }

    // set groundentity, watertype, and waterlevel for final spot
    this.catagorizePosition();

    this.snapPosition();
  }
}

const JITTERBITS = [0, 4, 1, 2, 3, 5, 6, 7] as const;
const SNAP_OFFSET = [0, -1, 1] as const;

// C: q_shared.c VectorMA (local copy to keep the hot path monomorphic)
function vectorMA(veca: Vec3, scale: number, vecb: Vec3, vecc: Vec3): void {
  scale = fr(scale);
  vecc[0] = veca[0]! + fr(scale * vecb[0]!);
  vecc[1] = veca[1]! + fr(scale * vecb[1]!);
  vecc[2] = veca[2]! + fr(scale * vecb[2]!);
}
