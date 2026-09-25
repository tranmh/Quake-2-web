import { describe, expect, it } from 'vitest';
import {
  COM_ParseAll,
  MASK_PLAYERSOLID,
  PMF_DUCKED,
  PMF_ON_GROUND,
  PM_FREEZE,
  PM_SPECTATOR,
  Trace,
  type Vec3,
} from 'q2-shared';
import { CollisionModel, CollisionWorld, Pmove, PmoveT } from '../src';
import { demoPak, loadMap } from './helpers';

function makePm(w: CollisionWorld): PmoveT {
  const tr = new Trace();
  return new PmoveT(
    (start: Vec3, mins: Vec3, maxs: Vec3, end: Vec3) => {
      w.boxTrace(start, end, mins, maxs, 0, MASK_PLAYERSOLID, tr);
      tr.ent = tr.fraction < 1 ? 1 : null;
      return tr;
    },
    (p: Vec3) => w.pointContents(p, 0),
  );
}

/** empty space: every trace completes (note: CM_BoxTrace without a map returns endpos 0,0,0 like C) */
function freeSpacePm(): PmoveT {
  const tr = new Trace();
  return new PmoveT(
    (_start: Vec3, _mins: Vec3, _maxs: Vec3, end: Vec3) => {
      tr.fraction = 1;
      tr.endpos.set(end);
      return tr;
    },
    () => 0,
  );
}

describe('Pmove without a map', () => {
  it('CM without a map returns endpos 0 (C behaviour), pulling the player to the origin', () => {
    const pm = makePm(new CollisionWorld(CollisionModel.empty()));
    pm.s.origin.set([80, 80, 800]);
    pm.cmd.msec = 10;
    new Pmove().run(pm);
    expect(Array.from(pm.s.origin)).toEqual([0, 0, 0]);
  });

  it('falls freely under gravity and snaps to 1/8 units', () => {
    const pm = freeSpacePm();
    const pmove = new Pmove();
    pm.s.gravity = 800;
    pm.s.origin.set([0, 0, 800]);
    pm.cmd.msec = 100;
    pmove.run(pm);
    expect(pm.s.velocity[2]).toBe(-640); // -80 units/s in 1/8 units
    expect(pm.s.origin[2]).toBe(800 - 64);
    expect(pm.groundentity).toBeNull();
    expect(pm.viewheight).toBe(22);
    expect(Array.from(pm.maxs)).toEqual([16, 16, 32]);
  });

  it('freeze does not move; spectator flies', () => {
    const pm = freeSpacePm();
    const pmove = new Pmove();
    pm.s.pm_type = PM_FREEZE;
    pm.s.origin.set([8, 8, 8]);
    pm.cmd.msec = 50;
    pm.cmd.forwardmove = 400;
    pmove.run(pm);
    expect(Array.from(pm.s.origin)).toEqual([8, 8, 8]);
    expect(pm.cmd.forwardmove).toBe(0);
    pm.s.pm_type = PM_SPECTATOR;
    pm.cmd.forwardmove = 400;
    pmove.run(pm);
    expect(pm.s.origin[0]).toBeGreaterThan(8);
  });
});

describe.skipIf(!demoPak())('Pmove on demo1', () => {
  it('lands at info_player_start, then walks forward and ducks', () => {
    const m = loadMap('demo1');
    const w = new CollisionWorld(m);
    const toks = COM_ParseAll(m.entityString);
    const i = toks.indexOf('info_player_start');
    const oi = toks.indexOf('origin', i);
    const o = toks[oi + 1]!.split(' ').map(Number);
    const pm = makePm(w);
    const pmove = new Pmove();
    pm.s.gravity = 800;
    pm.s.origin.set(o.map((v) => Math.round(v * 8)));
    pm.cmd.msec = 25;
    for (let k = 0; k < 80; k++) pmove.run(pm);
    expect(pm.s.pm_flags & PMF_ON_GROUND).toBe(PMF_ON_GROUND);
    expect(pm.groundentity).not.toBeNull();
    expect(pm.s.velocity[2]).toBe(0);
    const x0 = pm.s.origin[0]!;
    pm.cmd.forwardmove = 200;
    for (let k = 0; k < 10; k++) pmove.run(pm);
    expect(pm.s.origin[0]).not.toBe(x0);
    pm.cmd.forwardmove = 0;
    pm.cmd.upmove = -200;
    pmove.run(pm);
    expect(pm.s.pm_flags & PMF_DUCKED).toBe(PMF_DUCKED);
    expect(pm.viewheight).toBe(-2);
    pm.cmd.upmove = 0;
    pmove.run(pm);
    expect(pm.s.pm_flags & PMF_DUCKED).toBe(0);
  });
});
