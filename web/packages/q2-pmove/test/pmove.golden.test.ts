// Golden tests against the C oracle (docs/FIXTURES.md core/pmove).
import { readdirSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { MASK_PLAYERSOLID, Trace, type PmoveState, type UserCmd, type Vec3 } from 'q2-shared';
import { cmpF32, cmpF32Vec, cmpInt, cmpIntVec, fileExists, fixturePath, readJsonl } from 'q2-shared/testing';
import { CollisionWorld, Pmove, PmoveT } from '../src';
import { demoPak, loadMap } from './helpers';

interface PMS {
  pm_type: number;
  origin: number[];
  velocity: number[];
  pm_flags: number;
  pm_time: number;
  gravity: number;
  delta_angles: number[];
}
interface UC {
  msec: number;
  buttons: number;
  angles: number[];
  forwardmove: number;
  sidemove: number;
  upmove: number;
  impulse: number;
  lightlevel: number;
}
interface Step {
  header?: { map: string; airaccelerate: number };
  cmd: UC;
  in?: PMS;
  out: {
    s: PMS;
    viewangles: number[];
    viewheight: number;
    mins: number[];
    maxs: number[];
    groundentity: number;
    watertype: number;
    waterlevel: number;
    numtouch: number;
  };
}

function setPms(s: PmoveState, p: PMS): void {
  s.pm_type = p.pm_type;
  s.origin.set(p.origin);
  s.velocity.set(p.velocity);
  s.pm_flags = p.pm_flags & 255;
  s.pm_time = p.pm_time & 255;
  s.gravity = (p.gravity << 16) >> 16;
  s.delta_angles.set(p.delta_angles);
}

function setCmd(c: UserCmd, u: UC): void {
  c.msec = u.msec & 255;
  c.buttons = u.buttons & 255;
  c.angles.set(u.angles);
  c.forwardmove = (u.forwardmove << 16) >> 16;
  c.sidemove = (u.sidemove << 16) >> 16;
  c.upmove = (u.upmove << 16) >> 16;
  c.impulse = u.impulse & 255;
  c.lightlevel = u.lightlevel & 255;
}

function cmpPms(path: string, s: PmoveState, p: PMS): string | null {
  return (
    cmpInt(`${path}.pm_type`, s.pm_type, p.pm_type) ??
    cmpIntVec(`${path}.origin`, s.origin, p.origin) ??
    cmpIntVec(`${path}.velocity`, s.velocity, p.velocity) ??
    cmpInt(`${path}.pm_flags`, s.pm_flags, p.pm_flags) ??
    cmpInt(`${path}.pm_time`, s.pm_time, p.pm_time) ??
    cmpInt(`${path}.gravity`, s.gravity, p.gravity) ??
    cmpIntVec(`${path}.delta_angles`, s.delta_angles, p.delta_angles)
  );
}

const dir = fixturePath('core', 'pmove');
const scenarios = demoPak() && fileExists(dir) ? readdirSync(dir).filter((f) => f.endsWith('.jsonl')) : [];

describe.skipIf(scenarios.length === 0)('golden core/pmove', () => {
  for (const file of scenarios) {
    it(file, () => {
      const lines = readJsonl<Step>(fixturePath('core', 'pmove', file));
      const header = lines[0]!.header!;
      const world = new CollisionWorld(loadMap(header.map));
      const tr = new Trace();
      const trace = (start: Vec3, mins: Vec3, maxs: Vec3, end: Vec3): Trace => {
        world.boxTrace(start, end, mins, maxs, 0, MASK_PLAYERSOLID, tr);
        // as CL_PMTrace: world hits carry a non-NULL entity
        tr.ent = tr.fraction < 1.0 ? 1 : null;
        return tr;
      };
      const pointcontents = (p: Vec3): number => world.pointContents(p, 0);
      const pmove = new Pmove();
      pmove.pm_airaccelerate = header.airaccelerate;
      const pm = new PmoveT(trace, pointcontents);
      let firstErr: string | null = null;
      let mismatches = 0;
      let steps = 0;
      for (let idx = 1; idx < lines.length; idx++) {
        const step = lines[idx]!;
        steps++;
        if (step.in) setPms(pm.s, step.in);
        setCmd(pm.cmd, step.cmd);
        pm.snapinitial = false;
        // the oracle memsets its pmove_t before every step
        pm.mins.fill(0);
        pm.maxs.fill(0);
        pmove.run(pm);
        const o = step.out;
        const err =
          cmpPms('s', pm.s, o.s) ??
          cmpF32Vec('viewangles', pm.viewangles, o.viewangles) ??
          cmpF32('viewheight', pm.viewheight, o.viewheight) ??
          cmpF32Vec('mins', pm.mins, o.mins) ??
          cmpF32Vec('maxs', pm.maxs, o.maxs) ??
          cmpInt('groundentity', pm.groundentity ? 1 : 0, o.groundentity) ??
          cmpInt('watertype', pm.watertype, o.watertype) ??
          cmpInt('waterlevel', pm.waterlevel, o.waterlevel) ??
          cmpInt('numtouch', pm.numtouch, o.numtouch) ??
          cmpInt('cmd.forwardmove', pm.cmd.forwardmove, step.cmd.forwardmove);
        if (err) {
          mismatches++;
          if (!firstErr) firstErr = `line ${idx + 1}: ${err}; cmd=${JSON.stringify(step.cmd)}`;
          // resynchronise on the oracle state so one divergence doesn't cascade
          setPms(pm.s, o.s);
        }
      }
      expect(steps).toBeGreaterThan(0);
      expect(firstErr ? `${mismatches}/${steps} mismatches; first: ${firstErr}` : null).toBeNull();
    });
  }
});
