// Port of client/cl_newfx.c -- MORE entity effects parsing and management (Rogue mission pack effects).
// Only the RINGS variant of CL_Heatbeam is compiled in 3.19 (CORKSCREW / DOUBLE_SCREW / SPRAY are off).
import {
  AngleVectors,
  DotProduct,
  M_PI,
  PITCH,
  ROLL,
  VIDREF_GL,
  VectorAdd,
  VectorClear,
  VectorCopy,
  VectorMA,
  VectorNormalize,
  VectorScale,
  VectorSubtract,
  YAW,
  vec3_origin,
} from 'q2-shared';
import {
  CL_AllocDlight,
  MakeNormalVectors,
  INSTANT_PARTICLE,
  PARTICLE_GRAVITY,
  allocParticle,
  crand,
  frand,
  type FxState,
} from './cl_fx';
import type { CEntity } from './client';
import type { CSustain } from './cl_tent';

const fr = Math.fround;

// PGM: the renderer is always GL in the browser
const vidref_val: number = VIDREF_GL;
const VIDREF_SOFT = 1;

// scratch vectors (C locals)
const s_move = new Float32Array(3);
const s_vec = new Float32Array(3);
const s_right = new Float32Array(3);
const s_up = new Float32Array(3);
const s_forward = new Float32Array(3);
const s_dir = new Float32Array(3);
const s_end = new Float32Array(3);
const s_angle = new Float32Array(3);
const s_back = new Float32Array(3);

// C: cl_newfx.c:37 vectoangles2 -- this is duplicated in the game DLL, but I need it here.
export function vectoangles2(value1: ArrayLike<number>, angles: Float32Array): void {
  let yaw: number;
  let pitch: number;
  if (value1[1] === 0 && value1[0] === 0) {
    yaw = 0;
    if (value1[2]! > 0) pitch = 90;
    else pitch = 270;
  } else {
    // PMM - fixed to correct for pitch of 0
    if (value1[0]) yaw = fr((Math.atan2(value1[1]!, value1[0]!) * 180) / M_PI);
    else if (value1[1]! > 0) yaw = 90;
    else yaw = 270;

    if (yaw < 0) yaw = fr(yaw + 360);

    const forward = fr(Math.sqrt(fr(fr(value1[0]! * value1[0]!) + fr(value1[1]! * value1[1]!))));
    pitch = fr((Math.atan2(value1[2]!, forward) * 180) / M_PI);
    if (pitch < 0) pitch = fr(pitch + 360);
  }
  angles[PITCH] = -pitch;
  angles[YAW] = yaw;
  angles[ROLL] = 0;
}

// C: cl_newfx.c:76 CL_Flashlight
export function CL_Flashlight(fx: FxState, ent: number, pos: ArrayLike<number>): void {
  const dl = CL_AllocDlight(fx, ent);
  VectorCopy(pos, dl.origin);
  dl.radius = 400;
  dl.minlight = 250;
  dl.die = fr(fx.c.cl.time + 100);
  dl.color[0] = 1;
  dl.color[1] = 1;
  dl.color[2] = 1;
}

// C: cl_newfx.c:95 CL_ColorFlash -- flash of light
export function CL_ColorFlash(
  fx: FxState,
  pos: ArrayLike<number>,
  ent: number,
  intensity: number,
  r: number,
  g: number,
  b: number,
): void {
  if (vidref_val === VIDREF_SOFT && (r < 0 || g < 0 || b < 0)) {
    intensity = -intensity;
    r = -r;
    g = -g;
    b = -b;
  }
  const dl = CL_AllocDlight(fx, ent);
  VectorCopy(pos, dl.origin);
  dl.radius = intensity;
  dl.minlight = 250;
  dl.die = fr(fx.c.cl.time + 100);
  dl.color[0] = r;
  dl.color[1] = g;
  dl.color[2] = b;
}

// C: cl_newfx.c:123 CL_DebugTrail
export function CL_DebugTrail(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  MakeNormalVectors(vec, s_right, s_up);
  const dec = 3;
  VectorScale(vec, dec, vec);
  VectorCopy(start, move);

  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    VectorClear(p.accel);
    VectorClear(p.vel);
    p.alpha = 1.0;
    p.alphavel = fr(-0.1);
    p.color = 0x74 + (c.rand.rand() & 7);
    VectorCopy(move, p.org);
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:187 CL_SmokeTrail
export function CL_SmokeTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  colorStart: number,
  colorRun: number,
  spacing: number,
): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  VectorScale(vec, spacing, vec);

  // FIXME: this is a really silly way to have a loop
  while (len > 0) {
    len = fr(len - spacing);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.5));
    p.color = fr(colorStart + (c.rand.rand() % colorRun));
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 3);
      p.accel[j] = 0;
    }
    p.vel[2] = 20 + fr(crand(c) * 5);
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:230 CL_ForceWall
export function CL_ForceWall(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  color: number,
): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  VectorScale(vec, 4, vec);

  // FIXME: this is a really silly way to have a loop
  while (len > 0) {
    len = fr(len - 4);
    if (!fx.free_particles) return;
    if (frand(c) > 0.3) {
      const p = allocParticle(fx)!;
      VectorClear(p.accel);
      p.time = fr(c.cl.time);
      p.alpha = 1.0;
      p.alphavel = fr(-1.0 / (3.0 + frand(c) * 0.5));
      p.color = color;
      for (let j = 0; j < 3; j++) {
        p.org[j] = move[j]! + fr(crand(c) * 3);
        p.accel[j] = 0;
      }
      p.vel[0] = 0;
      p.vel[1] = 0;
      p.vel[2] = -40 - fr(crand(c) * 10);
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:279 CL_FlameEffects
export function CL_FlameEffects(fx: FxState, _ent: CEntity, origin: ArrayLike<number>): void {
  const c = fx.c;
  let count = c.rand.rand() & 0xf;
  for (let n = 0; n < count; n++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.2));
    p.color = 226 + (c.rand.rand() % 4);
    for (let j = 0; j < 3; j++) {
      p.org[j] = origin[j]! + fr(crand(c) * 5);
      p.vel[j] = crand(c) * 5;
    }
    p.vel[2] = crand(c) * -10;
    p.accel[2] = -PARTICLE_GRAVITY;
  }

  count = c.rand.rand() & 0x7;
  for (let n = 0; n < count; n++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.5));
    p.color = 0 + (c.rand.rand() % 4);
    for (let j = 0; j < 3; j++) p.org[j] = origin[j]! + fr(crand(c) * 3);
    p.vel[2] = 20 + fr(crand(c) * 5);
  }
}

// C: cl_newfx.c:344 CL_GenericParticleEffect
export function CL_GenericParticleEffect(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
  numcolors: number,
  dirspread: number,
  alphavel: number,
): void {
  const c = fx.c;
  alphavel = fr(alphavel);
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    if (numcolors > 1) p.color = color + (c.rand.rand() & numcolors);
    else p.color = color;

    const d = c.rand.rand() & dirspread;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((c.rand.rand() & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = crand(c) * 20;
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + fr(frand(c) * alphavel)));
  }
}

// C: cl_newfx.c:388 CL_BubbleTrail2 (lets you control the # of bubbles by setting the distance between the spawns)
export function CL_BubbleTrail2(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  dist: number,
): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  const len = VectorNormalize(vec);
  const dec = fr(dist);
  VectorScale(vec, dec, vec);

  for (let i = 0; i < len; i = Math.trunc(fr(i + dec))) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (1 + frand(c) * 0.1));
    p.color = 4 + (c.rand.rand() & 7);
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + fr(crand(c) * 2);
      p.vel[j] = crand(c) * 10;
    }
    p.org[2] = p.org[2]! - 4;
    p.vel[2] = p.vel[2]! + 20;
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:525 CL_Heatbeam (RINGS version)
export function CL_Heatbeam(fx: FxState, start: ArrayLike<number>, forward: ArrayLike<number>): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  const right = s_right;
  const up = s_up;
  const dir = s_dir;
  const end = s_end;
  const step = 32.0;

  VectorMA(start, 4096, forward, end);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  const len = VectorNormalize(vec);

  // FIXME - pmm - these might end up using old values?
  VectorCopy(c.cl.v_right, right);
  VectorCopy(c.cl.v_up, up);
  if (vidref_val === VIDREF_GL) {
    // GL mode
    VectorMA(move, -0.5, right, move);
    VectorMA(move, -0.5, up, move);
  }
  // otherwise assume SOFT

  const ltime = fr(fr(c.cl.time) / 1000.0);
  const start_pt = fr((ltime * 96.0) % step);
  VectorMA(move, start_pt, vec, move);
  VectorScale(vec, step, vec);

  const rstep = fr(M_PI / 10.0);
  for (let i = Math.trunc(start_pt); i < len; i = Math.trunc(i + step)) {
    if (i > step * 5) break; // don't bother after the 5th ring

    for (let rot = 0; rot < M_PI * 2; rot = fr(rot + rstep)) {
      const p = allocParticle(fx);
      if (!p) return;
      p.time = fr(c.cl.time);
      VectorClear(p.accel);
      const variance = 0.5;
      const cc = fr(Math.cos(rot) * variance);
      const s = fr(Math.sin(rot) * variance);

      // trim it so it looks like it's starting at the origin
      if (i < 10) {
        VectorScale(right, cc * (i / 10.0), dir);
        VectorMA(dir, s * (i / 10.0), up, dir);
      } else {
        VectorScale(right, cc, dir);
        VectorMA(dir, s, up, dir);
      }

      p.alpha = 0.5;
      p.alphavel = -1000.0;
      p.color = 223 - (c.rand.rand() & 7);
      for (let j = 0; j < 3; j++) {
        p.org[j] = move[j]! + fr(dir[j]! * 3);
        p.vel[j] = 0;
      }
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:749 CL_ParticleSteamEffect -- puffs with velocity along direction, with some randomness
export function CL_ParticleSteamEffect(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
  magnitude: number,
): void {
  const c = fx.c;
  const r = s_right;
  const u = s_up;
  MakeNormalVectors(dir, r, u);

  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = color + (c.rand.rand() & 7);
    for (let j = 0; j < 3; j++) p.org[j] = org[j]! + magnitude * 0.1 * crand(c);
    VectorScale(dir, magnitude, p.vel);
    let d = fr(fr(crand(c) * magnitude) / 3);
    VectorMA(p.vel, d, r, p.vel);
    d = fr(fr(crand(c) * magnitude) / 3);
    VectorMA(p.vel, d, u, p.vel);

    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY / 2;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_newfx.c:792 CL_ParticleSteamEffect2 (sustain think)
export function CL_ParticleSteamEffect2(fx: FxState, self: CSustain): void {
  const c = fx.c;
  const r = s_right;
  const u = s_up;
  const dir = s_dir;
  VectorCopy(self.dir, dir);
  MakeNormalVectors(dir, r, u);

  for (let i = 0; i < self.count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = self.color + (c.rand.rand() & 7);
    for (let j = 0; j < 3; j++) p.org[j] = self.org[j]! + self.magnitude * 0.1 * crand(c);
    VectorScale(dir, self.magnitude, p.vel);
    let d = fr(fr(crand(c) * self.magnitude) / 3);
    VectorMA(p.vel, d, r, p.vel);
    d = fr(fr(crand(c) * self.magnitude) / 3);
    VectorMA(p.vel, d, u, p.vel);

    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY / 2;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
  self.nextthink += self.thinkinterval;
}

// C: cl_newfx.c:844 CL_TrackerTrail
export function CL_TrackerTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  particleColor: number,
): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  const forward = s_forward;
  const right = s_right;
  const up = s_up;
  const angle_dir = s_angle;
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);

  VectorCopy(vec, forward);
  vectoangles2(forward, angle_dir);
  AngleVectors(angle_dir, forward, right, up);

  const dec = 3;
  VectorScale(vec, 3, vec);

  // FIXME: this is a really silly way to have a loop
  while (len > 0) {
    len = fr(len - dec);
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = -2.0;
    p.color = particleColor;
    const dist = DotProduct(move, forward);
    VectorMA(move, 8 * Math.cos(dist), up, p.org);
    for (let j = 0; j < 3; j++) {
      p.vel[j] = 0;
      p.accel[j] = 0;
    }
    p.vel[2] = 5;
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:898 CL_Tracker_Shell
export function CL_Tracker_Shell(fx: FxState, origin: ArrayLike<number>): void {
  const c = fx.c;
  const dir = s_dir;
  for (let i = 0; i < 300; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = INSTANT_PARTICLE;
    p.color = 0;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorMA(origin, 40, dir, p.org);
  }
}

// C: cl_newfx.c:929 CL_MonsterPlasma_Shell
export function CL_MonsterPlasma_Shell(fx: FxState, origin: ArrayLike<number>): void {
  const c = fx.c;
  const dir = s_dir;
  for (let i = 0; i < 40; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = INSTANT_PARTICLE;
    p.color = 0xe0;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorMA(origin, 10, dir, p.org);
  }
}

const widow_colortable = [2 * 8, 13 * 8, 21 * 8, 18 * 8] as const;

// C: cl_newfx.c:961 CL_Widowbeamout (sustain think; note: never advances nextthink)
export function CL_Widowbeamout(fx: FxState, self: CSustain): void {
  const c = fx.c;
  const dir = s_dir;
  const ratio = fr(1.0 - fr(fr(self.endtime) - fr(c.cl.time)) / 2100.0);
  for (let i = 0; i < 300; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = INSTANT_PARTICLE;
    p.color = widow_colortable[c.rand.rand() & 3]!;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorMA(self.org, 45.0 * ratio, dir, p.org);
  }
}

const nuke_colortable = [110, 112, 114, 116] as const;

// C: cl_newfx.c:997 CL_Nukeblast (sustain think; never advances nextthink)
export function CL_Nukeblast(fx: FxState, self: CSustain): void {
  const c = fx.c;
  const dir = s_dir;
  const ratio = fr(1.0 - fr(fr(self.endtime) - fr(c.cl.time)) / 1000.0);
  for (let i = 0; i < 700; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = INSTANT_PARTICLE;
    p.color = nuke_colortable[c.rand.rand() & 3]!;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorMA(self.org, 200.0 * ratio, dir, p.org);
  }
}

// C: cl_newfx.c:1033 CL_WidowSplash (accel[2] is left stale, like C)
export function CL_WidowSplash(fx: FxState, org: ArrayLike<number>): void {
  const c = fx.c;
  const dir = s_dir;
  for (let i = 0; i < 256; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = widow_colortable[c.rand.rand() & 3]!;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorMA(org, 45.0, dir, p.org);
    VectorMA(vec3_origin, 40.0, dir, p.vel);
    p.accel[0] = p.accel[1] = 0;
    p.alpha = 1.0;
    p.alphavel = fr(-0.8 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_newfx.c:1067 CL_Tracker_Explode (unused in 3.19: TE_TRACKER_EXPLOSION calls it commented out)
export function CL_Tracker_Explode(fx: FxState, origin: ArrayLike<number>): void {
  const c = fx.c;
  const dir = s_dir;
  const backdir = s_back;
  for (let i = 0; i < 300; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    VectorClear(p.accel);
    p.time = fr(c.cl.time);
    p.alpha = 1.0;
    p.alphavel = -1.0;
    p.color = 0;
    dir[0] = crand(c);
    dir[1] = crand(c);
    dir[2] = crand(c);
    VectorNormalize(dir);
    VectorScale(dir, -1, backdir);
    VectorMA(origin, 64, dir, p.org);
    VectorScale(backdir, 64, p.vel);
  }
}

// C: cl_newfx.c:1107 CL_TagTrail
export function CL_TagTrail(
  fx: FxState,
  start: ArrayLike<number>,
  end: ArrayLike<number>,
  color: number,
): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
  color = fr(color);
  VectorCopy(start, move);
  VectorSubtract(end, start, vec);
  let len = VectorNormalize(vec);
  const dec = 5;
  VectorScale(vec, 5, vec);

  while (len >= 0) {
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
      p.vel[j] = crand(c) * 5;
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}

// C: cl_newfx.c:1156 CL_ColorExplosionParticles
export function CL_ColorExplosionParticles(
  fx: FxState,
  org: ArrayLike<number>,
  color: number,
  run: number,
): void {
  const c = fx.c;
  for (let i = 0; i < 128; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = fr(color + (c.rand.rand() % run));
    for (let j = 0; j < 3; j++) {
      p.org[j] = org[j]! + ((c.rand.rand() % 32) - 16);
      p.vel[j] = (c.rand.rand() % 256) - 128;
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-0.4 / (0.6 + frand(c) * 0.2));
  }
}

// C: cl_newfx.c:1192 CL_ParticleSmokeEffect -- like the steam effect, but unaffected by gravity
export function CL_ParticleSmokeEffect(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
  count: number,
  magnitude: number,
): void {
  const c = fx.c;
  const r = s_right;
  const u = s_up;
  MakeNormalVectors(dir, r, u);

  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = color + (c.rand.rand() & 7);
    for (let j = 0; j < 3; j++) p.org[j] = org[j]! + magnitude * 0.1 * crand(c);
    VectorScale(dir, magnitude, p.vel);
    let d = fr(fr(crand(c) * magnitude) / 3);
    VectorMA(p.vel, d, r, p.vel);
    d = fr(fr(crand(c) * magnitude) / 3);
    VectorMA(p.vel, d, u, p.vel);

    p.accel[0] = p.accel[1] = p.accel[2] = 0;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_newfx.c:1238 CL_BlasterParticles2 -- wall impact puffs (green)
export function CL_BlasterParticles2(
  fx: FxState,
  org: ArrayLike<number>,
  dir: ArrayLike<number>,
  color: number,
): void {
  const c = fx.c;
  color = color >>> 0; // unsigned int
  const count = 40;
  for (let i = 0; i < count; i++) {
    const p = allocParticle(fx);
    if (!p) return;
    p.time = fr(c.cl.time);
    p.color = fr((color + (c.rand.rand() & 7)) >>> 0);

    const d = c.rand.rand() & 15;
    for (let j = 0; j < 3; j++) {
      p.org[j] = fr(org[j]! + ((c.rand.rand() & 7) - 4)) + fr(d * dir[j]!);
      p.vel[j] = fr(dir[j]! * 30) + fr(crand(c) * 40);
    }
    p.accel[0] = p.accel[1] = 0;
    p.accel[2] = -PARTICLE_GRAVITY;
    p.alpha = 1.0;
    p.alphavel = fr(-1.0 / (0.5 + frand(c) * 0.3));
  }
}

// C: cl_newfx.c:1280 CL_BlasterTrail2 -- green!
export function CL_BlasterTrail2(fx: FxState, start: ArrayLike<number>, end: ArrayLike<number>): void {
  const c = fx.c;
  const move = s_move;
  const vec = s_vec;
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
    p.color = 0xd0;
    for (let j = 0; j < 3; j++) {
      p.org[j] = move[j]! + crand(c);
      p.vel[j] = crand(c) * 5;
      p.accel[j] = 0;
    }
    VectorAdd(move, vec, move);
  }
}
