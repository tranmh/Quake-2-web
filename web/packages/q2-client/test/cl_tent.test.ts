import { describe, expect, it } from 'vitest';
import {
  BYTE_DIRS,
  QRand,
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
  RF_TRANSLUCENT,
} from 'q2-shared';
import { MSG_WriteByte, MSG_WriteLong, MSG_WritePos, MSG_WriteShort, SizeBuf } from 'q2-protocol';
import { NullEffects } from '../src/null_effects';
import { FxState, CL_ClearEffects, crand, frand } from '../src/cl_fx';
import {
  CL_AddTEnts,
  CL_AllocExplosion,
  CL_ClearTEnts,
  CL_ParseTEnt,
  CL_RegisterTEntModels,
  CL_RegisterTEntSounds,
  MAX_EXPLOSIONS,
  MAX_SUSTAINS,
  ex_free,
  ex_poly,
} from '../src/cl_tent';
import { CL_BlasterParticles2, vectoangles2 } from '../src/cl_newfx';
import { V_ClearScene } from '../src/cl_view';
import type { ClientContext } from '../src/client';
import { createRecordingRefresh, createTestContext } from './helpers';

const fr = Math.fround;

type Started = {
  origin: number[] | null;
  ent: number;
  chan: number;
  sfx: string | null;
  vol: number;
  attn: number;
};

function setup(): { c: ClientContext; fx: FxState; started: Started[] } {
  const refresh = createRecordingRefresh();
  const c = createTestContext({ refresh });
  const fx = new FxState();
  fx.c = c;
  const started: Started[] = [];
  c.sound.startSound = (origin, ent, chan, sfx, vol, attn) => {
    started.push({
      origin: origin ? Array.from(origin) : null,
      ent,
      chan,
      sfx: sfx ? (sfx as unknown as { name: string }).name : null,
      vol,
      attn,
    });
  };
  CL_ClearEffects(fx);
  CL_ClearTEnts(fx);
  CL_RegisterTEntSounds(fx);
  c.cl.time = 1000;
  c.cl.frame.servertime = 1000;
  return { c, fx, started };
}

const P1 = [100, -200, 50.5];
const P2 = [300, 40, -12.25];
const P3 = [1, 2, 3];

/** Writes the payload of a svc_temp_entity (type byte + fields) the way the game (g_*.c / m_*.c) does. */
function writeTE(type: number, opts: { steamId?: number } = {}): SizeBuf {
  const sb = new SizeBuf(1400);
  MSG_WriteByte(sb, type);
  const pos = (p: number[]) => MSG_WritePos(sb, p);
  const dir = () => MSG_WriteByte(sb, 7); // a valid byte direction index
  switch (type) {
    case TE_BLOOD:
    case TE_GUNSHOT:
    case TE_SPARKS:
    case TE_BULLET_SPARKS:
    case TE_SCREEN_SPARKS:
    case TE_SHIELD_SPARKS:
    case TE_SHOTGUN:
    case TE_BLASTER:
    case TE_GREENBLOOD:
    case TE_BLASTER2:
    case TE_FLECHETTE:
    case TE_HEATBEAM_SPARKS:
    case TE_HEATBEAM_STEAM:
    case TE_MOREBLOOD:
    case TE_ELECTRIC_SPARKS:
      pos(P1);
      dir();
      break;
    case TE_SPLASH:
    case TE_LASER_SPARKS:
    case TE_WELDING_SPARKS:
    case TE_TUNNEL_SPARKS:
      MSG_WriteByte(sb, 12);
      pos(P1);
      dir();
      MSG_WriteByte(sb, type === TE_SPLASH ? SPLASH_SPARKS : 0xe0);
      break;
    case TE_BLUEHYPERBLASTER:
    case TE_RAILTRAIL:
    case TE_BUBBLETRAIL:
    case TE_DEBUGTRAIL:
    case TE_BUBBLETRAIL2:
    case TE_BFG_LASER:
      pos(P1);
      pos(P2);
      break;
    case TE_EXPLOSION2:
    case TE_GRENADE_EXPLOSION:
    case TE_GRENADE_EXPLOSION_WATER:
    case TE_PLASMA_EXPLOSION:
    case TE_EXPLOSION1:
    case TE_EXPLOSION1_BIG:
    case TE_ROCKET_EXPLOSION:
    case TE_ROCKET_EXPLOSION_WATER:
    case TE_EXPLOSION1_NP:
    case TE_BFG_EXPLOSION:
    case TE_BFG_BIGEXPLOSION:
    case TE_BOSSTPORT:
    case TE_PLAIN_EXPLOSION:
    case TE_CHAINFIST_SMOKE:
    case TE_TRACKER_EXPLOSION:
    case TE_TELEPORT_EFFECT:
    case TE_DBALL_GOAL:
    case TE_WIDOWSPLASH:
    case TE_NUKEBLAST:
      pos(P1);
      break;
    case TE_PARASITE_ATTACK:
    case TE_MEDIC_CABLE_ATTACK:
    case TE_HEATBEAM:
    case TE_MONSTER_HEATBEAM:
      MSG_WriteShort(sb, 5);
      pos(P1);
      pos(P2);
      break;
    case TE_GRAPPLE_CABLE:
      MSG_WriteShort(sb, 5);
      pos(P1);
      pos(P2);
      pos(P3);
      break;
    case TE_LIGHTNING:
      MSG_WriteShort(sb, 5);
      MSG_WriteShort(sb, 6);
      pos(P1);
      pos(P2);
      break;
    case TE_FLASHLIGHT:
      pos(P1);
      MSG_WriteShort(sb, 5);
      break;
    case TE_FORCEWALL:
      pos(P1);
      pos(P2);
      MSG_WriteByte(sb, 0xd0);
      break;
    case TE_STEAM: {
      const id = opts.steamId ?? -1;
      MSG_WriteShort(sb, id);
      MSG_WriteByte(sb, 10);
      pos(P1);
      dir();
      MSG_WriteByte(sb, 0xe0);
      MSG_WriteShort(sb, 50);
      if (id !== -1) MSG_WriteLong(sb, 1000);
      break;
    }
    case TE_WIDOWBEAMOUT:
      MSG_WriteShort(sb, 20001);
      pos(P1);
      break;
    default:
      throw new Error('unknown TE ' + type);
  }
  // trailing sentinel so over-reads would be detected as a readcount mismatch
  MSG_WriteByte(sb, 0x55);
  MSG_WriteByte(sb, 0xaa);
  return sb;
}

const ALL_TE = [
  TE_GUNSHOT,
  TE_BLOOD,
  TE_BLASTER,
  TE_RAILTRAIL,
  TE_SHOTGUN,
  TE_EXPLOSION1,
  TE_EXPLOSION2,
  TE_ROCKET_EXPLOSION,
  TE_GRENADE_EXPLOSION,
  TE_SPARKS,
  TE_SPLASH,
  TE_BUBBLETRAIL,
  TE_SCREEN_SPARKS,
  TE_SHIELD_SPARKS,
  TE_BULLET_SPARKS,
  TE_LASER_SPARKS,
  TE_PARASITE_ATTACK,
  TE_ROCKET_EXPLOSION_WATER,
  TE_GRENADE_EXPLOSION_WATER,
  TE_MEDIC_CABLE_ATTACK,
  TE_BFG_EXPLOSION,
  TE_BFG_BIGEXPLOSION,
  TE_BOSSTPORT,
  TE_BFG_LASER,
  TE_GRAPPLE_CABLE,
  TE_WELDING_SPARKS,
  TE_GREENBLOOD,
  TE_BLUEHYPERBLASTER,
  TE_PLASMA_EXPLOSION,
  TE_TUNNEL_SPARKS,
  TE_BLASTER2,
  TE_RAILTRAIL,
  TE_LIGHTNING,
  TE_DEBUGTRAIL,
  TE_PLAIN_EXPLOSION,
  TE_FLASHLIGHT,
  TE_FORCEWALL,
  TE_HEATBEAM,
  TE_MONSTER_HEATBEAM,
  TE_STEAM,
  TE_BUBBLETRAIL2,
  TE_MOREBLOOD,
  TE_HEATBEAM_SPARKS,
  TE_HEATBEAM_STEAM,
  TE_CHAINFIST_SMOKE,
  TE_ELECTRIC_SPARKS,
  TE_TRACKER_EXPLOSION,
  TE_TELEPORT_EFFECT,
  TE_DBALL_GOAL,
  TE_WIDOWBEAMOUT,
  TE_NUKEBLAST,
  TE_WIDOWSPLASH,
  TE_EXPLOSION1_BIG,
  TE_EXPLOSION1_NP,
  TE_FLECHETTE,
];

function nullConsumed(sb: SizeBuf): number {
  const ne = new NullEffects();
  ne.attach(createTestContext());
  sb.readcount = 0;
  ne.parseTEnt(sb);
  return sb.readcount;
}

function draws(before: QRand, after: QRand, max = 100000): number {
  const want = JSON.stringify(after.save());
  for (let n = 0; n <= max; n++) {
    if (JSON.stringify(before.save()) === want) return n;
    before.rand();
  }
  return -1;
}

function randCopy(c: ClientContext): QRand {
  const r = new QRand();
  r.restore(c.rand.save());
  return r;
}

describe('CL_ParseTEnt', () => {
  it('consumes exactly the bytes NullEffects consumes, for every TE_* type', async () => {
    for (const models of [false, true]) {
      const { c, fx } = setup();
      if (models) await CL_RegisterTEntModels(fx);
      for (const type of ALL_TE) {
        const sb = writeTE(type);
        const want = nullConsumed(sb);
        sb.readcount = 0;
        CL_ParseTEnt(fx, sb);
        expect(sb.readcount, `TE ${type} models=${models}`).toBe(want);
        expect(want).toBe(sb.cursize - 2);
      }
      // steam sustain path, and with every sustain in use
      for (let i = 0; i <= MAX_SUSTAINS; i++) {
        const sb = writeTE(TE_STEAM, { steamId: 100 + i });
        const want = nullConsumed(sb);
        sb.readcount = 0;
        CL_ParseTEnt(fx, sb);
        expect(sb.readcount).toBe(want);
      }
      expect(fx.tent.cl_sustains.every((s) => s.id !== 0)).toBe(true);
      for (const type of [TE_WIDOWBEAMOUT, TE_NUKEBLAST]) {
        const sb = writeTE(type);
        const want = nullConsumed(sb);
        sb.readcount = 0;
        CL_ParseTEnt(fx, sb);
        expect(sb.readcount).toBe(want);
      }
      // adding the tents (beams, explosions, lasers, sustains) runs without errors
      V_ClearScene(c);
      CL_AddTEnts(fx);
      expect(c.view.r_numentities).toBeGreaterThan(0);
    }
  });

  it('bad type is a drop error', () => {
    const { fx } = setup();
    const sb = new SizeBuf(16);
    MSG_WriteByte(sb, 200);
    expect(() => CL_ParseTEnt(fx, sb)).toThrow(/bad type/);
  });

  it('uses the exact number of rand() draws', () => {
    const cases: [number, number][] = [
      [TE_BLOOD, 60 * 9], // CL_ParticleEffect: color, d, 3*(rand&7, crand), frand
      [TE_SPARKS, 6 * 9],
      [TE_SHOTGUN, 20 * 9],
      [TE_BLASTER, 40 * 9], // CL_BlasterParticles
      [TE_BLASTER2, 40 * 9], // CL_BlasterParticles2
      [TE_EXPLOSION1_NP, 2], // rand()%360, frand()
      [TE_EXPLOSION1_BIG, 2],
      [TE_EXPLOSION1, 2 + 256 * 8], // + CL_ExplosionParticles
      [TE_GRENADE_EXPLOSION, 1 + 256 * 8],
      [TE_BFG_LASER, 1],
      [TE_BFG_EXPLOSION, 0],
      [TE_TRACKER_EXPLOSION, 128 * 8], // CL_ColorExplosionParticles
      [TE_WIDOWSPLASH, 256 * 5],
      [TE_HEATBEAM_STEAM, 20 * 7], // CL_ParticleSteamEffect (MakeNormalVectors draws nothing)
      [TE_CHAINFIST_SMOKE, 20 * 7],
    ];
    for (const [type, n] of cases) {
      const { c, fx } = setup();
      const before = randCopy(c);
      CL_ParseTEnt(fx, writeTE(type));
      expect(draws(before, c.rand), `TE ${type}`).toBe(n);
    }
    // TE_GUNSHOT: 40 particles + impact sound draw
    const { c, fx } = setup();
    const before = randCopy(c);
    CL_ParseTEnt(fx, writeTE(TE_GUNSHOT));
    expect(draws(before, c.rand)).toBe(40 * 9 + 1);
  });

  it('starts the original sounds', () => {
    const { fx, started } = setup();
    CL_ParseTEnt(fx, writeTE(TE_RAILTRAIL));
    expect(started).toEqual([{ origin: P2, ent: 0, chan: 0, sfx: 'weapons/railgf1a.wav', vol: 1, attn: 1 }]);
    started.length = 0;
    CL_ParseTEnt(fx, writeTE(TE_ROCKET_EXPLOSION_WATER));
    CL_ParseTEnt(fx, writeTE(TE_GRENADE_EXPLOSION));
    CL_ParseTEnt(fx, writeTE(TE_BOSSTPORT));
    CL_ParseTEnt(fx, writeTE(TE_LIGHTNING));
    CL_ParseTEnt(fx, writeTE(TE_TRACKER_EXPLOSION));
    expect(started.map((s) => [s.sfx, s.ent, s.chan, s.attn])).toEqual([
      ['weapons/xpld_wat.wav', 0, 0, 1],
      ['weapons/grenlx1a.wav', 0, 0, 1],
      ['misc/bigtele.wav', 0, 0, 0],
      ['weapons/tesla.wav', 5, 1, 1],
      ['weapons/disrupthit.wav', 0, 0, 1],
    ]);
    expect(started[3]!.origin).toBeNull();
  });

  it('registers the footstep sounds in order', () => {
    const { fx } = setup();
    expect(fx.tent.cl_sfx_footsteps.map((s) => (s as unknown as { name: string }).name)).toEqual([
      'player/step1.wav',
      'player/step2.wav',
      'player/step3.wav',
      'player/step4.wav',
    ]);
  });
});

describe('explosions / beams / sustains', () => {
  it('rocket explosion: poly explosion with light, frames and alpha as in CL_AddExplosions', async () => {
    const { c, fx } = setup();
    await CL_RegisterTEntModels(fx);
    CL_ParseTEnt(fx, writeTE(TE_EXPLOSION1_NP));
    const ex = fx.tent.cl_explosions[0]!;
    expect(ex.type).toBe(ex_poly);
    expect(ex.start).toBe(900);
    expect(ex.frames).toBe(15);
    expect((ex.ent.model as unknown as { name: string }).name).toBe('models/objects/r_explode/tris.md2');
    c.cl.time = 1250; // frac 3.5, f 3
    c.cl.lerpfrac = fr(0.25);
    V_ClearScene(c);
    CL_AddTEnts(fx);
    expect(c.view.r_numentities).toBe(1);
    const e = c.view.r_entities[0]!;
    expect(e.alpha).toBe(fr(13 / 16));
    expect(e.skinnum).toBe(1);
    expect(e.frame).toBe(ex.baseframe + 4);
    expect(e.oldframe).toBe(ex.baseframe + 3);
    expect(e.backlerp).toBe(fr(0.75));
    expect(c.view.r_numdlights).toBe(1);
    expect(c.view.r_dlights[0]!.intensity).toBe(fr(350 * fr(13 / 16)));
    // translucent from frame 10, freed at frames-1
    c.cl.time = 900 + 1100;
    V_ClearScene(c);
    CL_AddTEnts(fx);
    expect(c.view.r_entities[0]!.flags & RF_TRANSLUCENT).toBe(RF_TRANSLUCENT);
    c.cl.time = 900 + 1400;
    V_ClearScene(c);
    CL_AddTEnts(fx);
    expect(ex.type).toBe(ex_free);
    expect(c.view.r_numentities).toBe(0);
  });

  it('CL_AllocExplosion reuses the oldest slot when full', () => {
    const { c, fx } = setup();
    for (let i = 0; i < MAX_EXPLOSIONS; i++) {
      const ex = CL_AllocExplosion(fx);
      ex.type = ex_poly;
      ex.start = 500 + ((i * 7) % MAX_EXPLOSIONS);
    }
    c.cl.time = 2000;
    const oldest = fx.tent.cl_explosions.findIndex((e) => e.start === 500);
    expect(CL_AllocExplosion(fx)).toBe(fx.tent.cl_explosions[oldest]);
  });

  it('parasite beam: segments every 30 units along the beam', async () => {
    const { c, fx } = setup();
    await CL_RegisterTEntModels(fx);
    const sb = new SizeBuf(64);
    MSG_WriteByte(sb, TE_PARASITE_ATTACK);
    MSG_WriteShort(sb, 7);
    MSG_WritePos(sb, [0, 0, 0]);
    MSG_WritePos(sb, [100, 0, 0]);
    CL_ParseTEnt(fx, sb);
    V_ClearScene(c);
    CL_AddTEnts(fx);
    // d=100, steps=ceil(100/30)=4, len=(100-30)/3; while d>0: 100,70,40,10 -> 4 segments
    expect(c.view.r_numentities).toBe(4);
    const len = fr(70 / 3);
    expect(Array.from(c.view.r_entities[1]!.origin)).toEqual([len, 0, 0]);
    expect(c.view.r_entities[0]!.angles[0]).toBe(-0); // atan2(0,d) * -180 is -0 in C too
    expect(c.view.r_entities[0]!.angles[1]).toBe(0);
    // expired after 200 ms
    c.cl.time += 201;
    V_ClearScene(c);
    CL_AddTEnts(fx);
    expect(c.view.r_numentities).toBe(0);
  });

  it('player heatbeam adds the ring particles and beam segments', async () => {
    const { c, fx } = setup();
    await CL_RegisterTEntModels(fx);
    c.cl.playernum = 0;
    c.cl.v_forward.set([1, 0, 0]);
    c.cl.v_right.set([0, -1, 0]);
    c.cl.v_up.set([0, 0, 1]);
    const sb = new SizeBuf(64);
    MSG_WriteByte(sb, TE_HEATBEAM);
    MSG_WriteShort(sb, 1);
    MSG_WritePos(sb, [0, 0, 0]);
    MSG_WritePos(sb, [200, 0, 0]);
    CL_ParseTEnt(fx, sb);
    expect(Array.from(fx.tent.cl_playerbeams[0]!.offset)).toEqual([2, 7, -3]);
    V_ClearScene(c);
    CL_AddTEnts(fx);
    expect(c.view.r_numentities).toBeGreaterThan(0);
    expect(c.view.r_entities[0]!.frame).toBe(1);
    let n = 0;
    for (let p = fx.active_particles; p; p = p.next) n++;
    expect(n).toBeGreaterThan(0);
    expect(n % 20).toBe(0); // 20 particles per ring (rot 0..2pi step pi/10)
  });

  it('steam sustain thinks every 100 ms and expires', () => {
    const { c, fx } = setup();
    CL_ParseTEnt(fx, writeTE(TE_STEAM, { steamId: 3 }));
    const s = fx.tent.cl_sustains[0]!;
    expect(s.id).toBe(3);
    expect(s.endtime).toBe(2000);
    CL_AddTEnts(fx);
    expect(s.nextthink).toBe(1100);
    c.cl.time = 1050;
    CL_AddTEnts(fx);
    expect(s.nextthink).toBe(1100);
    c.cl.time = 2001;
    CL_AddTEnts(fx);
    expect(s.id).toBe(0);
  });
});

describe('cl_newfx', () => {
  it('CL_BlasterParticles2 first particle matches the C formulas', () => {
    const { c, fx } = setup();
    const ref = randCopy(c);
    const org = new Float32Array([10.5, -3, 7]);
    const dir = new Float32Array([BYTE_DIRS[21]!, BYTE_DIRS[22]!, BYTE_DIRS[23]!]);
    CL_BlasterParticles2(fx, org, dir, 0xd0);
    // after CL_ClearParticles the free list starts at particles[0]
    const last = fx.particles[0]!;
    const rc = { rand: ref } as unknown as ClientContext;
    const color = 0xd0 + (ref.rand() & 7);
    const d = ref.rand() & 15;
    const o = new Float32Array(3);
    const v = new Float32Array(3);
    for (let j = 0; j < 3; j++) {
      o[j] = fr(org[j]! + ((ref.rand() & 7) - 4)) + fr(d * dir[j]!);
      v[j] = fr(dir[j]! * 30) + fr(crand(rc) * 40);
    }
    const alphavel = fr(-1.0 / (0.5 + frand(rc) * 0.3));
    expect(last.color).toBe(color);
    expect(Array.from(last.org)).toEqual(Array.from(o));
    expect(Array.from(last.vel)).toEqual(Array.from(v));
    expect(last.alphavel).toBe(alphavel);
  });

  it('vectoangles2', () => {
    const a = new Float32Array(3);
    vectoangles2([0, 0, 5], a);
    expect(Array.from(a)).toEqual([-90, 0, 0]);
    vectoangles2([0, -1, 0], a);
    expect(Array.from(a)).toEqual([-0, 270, 0]);
    vectoangles2([1, 1, 0], a);
    expect(a[1]).toBe(fr((Math.atan2(1, 1) * 180) / Math.PI));
  });
});
