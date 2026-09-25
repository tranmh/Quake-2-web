// cl_fx.c port: unit tests + a bit-exact comparison with the ORIGINAL cl_fx.c compiled into a scratch
// harness (test/fixtures/cl_fx_harness.c -> test/fixtures/cl_fx_vec.txt).
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  EF_GIB,
  EF_GREENGIB,
  EV_FOOTSTEP,
  EV_PLAYER_TELEPORT,
  EntityState,
  MZ2_GLADIATOR_RAILGUN_1,
  MZ2_INFANTRY_MACHINEGUN_1,
  MZ2_MAKRON_BLASTER_3,
  MZ2_TANK_MACHINEGUN_5,
  MZ2_WIDOW2_BEAMER_1,
  MZ_BLASTER,
  MZ_CHAINGUN2,
  MZ_CHAINGUN3,
  MZ_LOGIN,
  MZ_MACHINEGUN,
  MZ_NUKE1,
  MZ_RAILGUN,
  MZ_ROCKET,
  MZ_SHOTGUN,
  MZ_SILENCED,
  MZ_TRACKER,
  floatBits,
} from 'q2-shared';
import { SizeBuf } from 'q2-protocol';
import { newRefEntity } from 'q2-ref';
import { CEntity, ClientContext } from '../src/client';
import { NullCinematics } from '../src/cinematic';
import { MemoryTransport } from '../src/transport';
import { NullSound, type Sound } from '../src/sound';
import { V_ClearScene } from '../src/cl_view';
import {
  CL_AddDLights,
  CL_AddLightStyles,
  CL_AddParticles,
  CL_AllocDlight,
  CL_BFGExplosionParticles,
  CL_BfgParticles,
  CL_BigTeleportParticles,
  CL_BlasterParticles,
  CL_BlasterTrail,
  CL_BubbleTrail,
  CL_ClearEffects,
  CL_DiminishingTrail,
  CL_EntityEvent,
  CL_ExplosionParticles,
  CL_FlagTrail,
  CL_FlyEffect,
  CL_IonripperTrail,
  CL_ItemRespawnParticles,
  CL_LogoutEffect,
  CL_ParseMuzzleFlash,
  CL_ParseMuzzleFlash2,
  CL_ParticleEffect,
  CL_ParticleEffect2,
  CL_ParticleEffect3,
  CL_QuadTrail,
  CL_RailTrail,
  CL_RocketTrail,
  CL_RunDLights,
  CL_RunLightStyles,
  CL_SetLightstyle,
  CL_TeleportParticles,
  CL_TeleporterParticles,
  CL_TrapParticles,
  FxState,
  INSTANT_PARTICLE,
  allocParticle,
  monster_flash_offset,
} from '../src/cl_fx';
import { NullEffects } from '../src/null_effects';

class RecSound extends NullSound {
  log: string[] = [];
  constructor() {
    super();
    const h = (f: number) => floatBits(f).toString(16).padStart(8, '0');
    const start: Sound['startSound'] = (_o, ent, chan, sfx, vol, attn, ofs) => {
      const name = (sfx as unknown as { name: string } | null)?.name ?? 'null';
      this.log.push(`S ${ent} ${chan} ${name} ${h(vol)} ${h(attn)} ${h(ofs)}`);
    };
    (this as Sound).startSound = start;
  }
}

function setup(): { c: ClientContext; fx: FxState; snd: RecSound } {
  const snd = new RecSound();
  const c = new ClientContext({
    refresh: {} as never,
    transport: () => new MemoryTransport(),
    loadFile: async () => null,
    sound: snd,
    effects: new NullEffects(),
    cinematics: new NullCinematics(),
  });
  c.cv.cl_footsteps = c.cvars.get('cl_footsteps', '1', 0);
  const fx = new FxState();
  fx.c = c;
  CL_ClearEffects(fx);
  return { c, fx, snd };
}

const hex = (f: number) => floatBits(f).toString(16).padStart(8, '0');

describe('cl_fx.c vs the original C (scratch harness vector)', () => {
  it('reproduces particles, dlights, sounds and the rand() stream bit-exactly', () => {
    const want = readFileSync(join(__dirname, 'fixtures', 'cl_fx_vec.txt'), 'latin1')
      .trim()
      .split('\n');
    const { c, fx, snd } = setup();
    const out: string[] = [];
    for (let t = 0; t < 4; t++) fx.tent.cl_sfx_footsteps[t] = snd.registerSound(`step${t + 1}`);
    c.rand.srand(1);
    c.cl.time = 1000;
    c.cls.frametime = Math.fround(0.016);
    const v = (x: number, y: number, z: number) => new Float32Array([x, y, z]);
    const a = v(10.5, -20.25, 30);
    const b = v(200.75, -50, 10.125);
    const d = v(0.6, 0.8, 0);
    const g = v(-100, 33.3, -7.7);
    const flushSnd = () => {
      out.push(...snd.log);
      snd.log = [];
    };
    const AP = () => {
      V_ClearScene(c);
      CL_AddParticles(fx);
      const vw = c.view;
      let ph = 2166136261;
      const mix = (x: number) => {
        for (let k = 0; k < 4; k++) {
          ph ^= (x >>> (8 * k)) & 255;
          ph = Math.imul(ph, 16777619) >>> 0;
        }
      };
      for (let i = 0; i < vw.r_numparticles; i++) {
        const p = vw.r_particles[i]!;
        if (i < 2)
          out.push(
            `P ${hex(p.origin[0]!)} ${hex(p.origin[1]!)} ${hex(p.origin[2]!)} ${p.color} ${hex(p.alpha)}`,
          );
        mix(floatBits(p.origin[0]!));
        mix(floatBits(p.origin[1]!));
        mix(floatBits(p.origin[2]!));
        mix(p.color);
        mix(floatBits(p.alpha));
      }
      out.push(`A ${c.cl.time} ${vw.r_numparticles} ${ph.toString(16).padStart(8, '0')}`);
    };
    const AD = () => {
      flushSnd();
      V_ClearScene(c);
      CL_AddDLights(fx);
      for (let i = 0; i < c.view.r_numdlights; i++) {
        const l = c.view.r_dlights[i]!;
        out.push(
          'L ' +
            [l.origin[0]!, l.origin[1]!, l.origin[2]!, l.intensity, l.color[0]!, l.color[1]!, l.color[2]!]
              .map(hex)
              .join(' '),
        );
      }
    };

    CL_ParticleEffect(fx, a, d, 0xe0, 20);
    CL_ParticleEffect2(fx, b, d, 0x10, 10);
    CL_ParticleEffect3(fx, g, d, 0x20, 10);
    const es = new EntityState();
    es.origin.set(a);
    es.number = 5;
    CL_TeleporterParticles(fx, es);
    CL_LogoutEffect(fx, b, MZ_LOGIN);
    CL_ItemRespawnParticles(fx, g);
    CL_ExplosionParticles(fx, a);
    CL_BlasterParticles(fx, b, d);
    CL_BlasterTrail(fx, a, b);
    CL_QuadTrail(fx, b, g);
    CL_FlagTrail(fx, g, a, 242);
    const ce = new CEntity();
    ce.trailcount = 1024;
    CL_DiminishingTrail(fx, a, b, ce, EF_GIB);
    CL_DiminishingTrail(fx, b, g, ce, EF_GREENGIB);
    CL_RocketTrail(fx, g, a, ce);
    out.push(`TC ${ce.trailcount}`);
    CL_RailTrail(fx, a, b);
    CL_IonripperTrail(fx, b, a);
    CL_BubbleTrail(fx, a, g);
    ce.fly_stoptime = 0;
    CL_FlyEffect(fx, ce, a);
    out.push(`FS ${ce.fly_stoptime}`);
    const e = newRefEntity();
    e.origin.set(b);
    CL_BfgParticles(fx, e);
    CL_TrapParticles(fx, e);
    CL_BFGExplosionParticles(fx, g);
    CL_TeleportParticles(fx, a);
    c.cl.time = 1250;
    AP();
    c.cl.time = 1600;
    AP();
    CL_BigTeleportParticles(fx, b);
    c.cl.time = 1700;
    AP();
    c.cl.time = 5000;
    AP();
    c.cl.time = 12000;
    AP();

    const pl = c.cl_entities[3]!.current;
    pl.origin.set(a);
    pl.angles[1] = 37.5;
    pl.angles[0] = -12;
    const msg = (n: number) => {
      const m = new SizeBuf(16);
      m.data[0] = 3;
      m.data[1] = 0;
      m.data[2] = n;
      m.cursize = 3;
      return m;
    };
    for (const w of [
      MZ_BLASTER,
      MZ_MACHINEGUN,
      MZ_SHOTGUN,
      MZ_CHAINGUN2,
      MZ_CHAINGUN3,
      MZ_ROCKET,
      MZ_LOGIN,
      MZ_RAILGUN | MZ_SILENCED,
      MZ_TRACKER,
      MZ_NUKE1,
    ]) {
      const m = msg(w);
      CL_ParseMuzzleFlash(fx, m);
      expect(m.readcount).toBe(3);
      AD();
    }
    for (const w of [
      MZ2_INFANTRY_MACHINEGUN_1,
      MZ2_TANK_MACHINEGUN_5,
      MZ2_WIDOW2_BEAMER_1,
      MZ2_GLADIATOR_RAILGUN_1,
      MZ2_MAKRON_BLASTER_3,
    ]) {
      const m = msg(w);
      CL_ParseMuzzleFlash2(fx, m);
      expect(m.readcount).toBe(3);
      AD();
    }
    c.cl.time = 1800;
    CL_RunDLights(fx);
    AD();
    c.cl.time = 2100;
    AP();
    es.event = EV_FOOTSTEP;
    CL_EntityEvent(fx, es);
    es.event = EV_PLAYER_TELEPORT;
    CL_EntityEvent(fx, es);
    flushSnd();
    c.cl.time = 2200;
    AP();
    out.push(`R ${c.rand.rand()}`);

    expect(out).toEqual(want.filter((l) => l !== 'SMOKE'));
  });
});

describe('cl_fx.c units', () => {
  it('local monster_flash_offset has the 212 m_flash.c entries', () => {
    expect(monster_flash_offset.length).toBe(212 * 3);
    expect(monster_flash_offset[39 * 3]).toBe(Math.fround(10.6 * 1.2)); // MZ2_SOLDIER_BLASTER_1
  });

  it('lightstyles: a..z mapped to 0..2.08, 10 Hz stepping', () => {
    const { c, fx } = setup();
    c.cl.configstrings[32 + 5] = 'amz';
    CL_SetLightstyle(fx, 5);
    c.cl.time = 0;
    CL_RunLightStyles(fx);
    expect(fx.cl_lightstyle[5]!.value[0]).toBe(0);
    expect(fx.cl_lightstyle[0]!.value[0]).toBe(1); // empty style
    c.cl.time = 150;
    CL_RunLightStyles(fx);
    expect(fx.cl_lightstyle[5]!.value[1]).toBe(1);
    c.cl.time = 250;
    CL_RunLightStyles(fx);
    expect(fx.cl_lightstyle[5]!.value[2]).toBe(Math.fround(25 / 12));
    V_ClearScene(c);
    CL_AddLightStyles(fx);
    expect(c.view.r_lightstyles[5]!.rgb[0]).toBe(Math.fround(25 / 12));
    c.cl.configstrings[32 + 6] = 'a'.repeat(64);
    expect(() => CL_SetLightstyle(fx, 6)).toThrow(/svc_lightstyle length=64/);
  });

  it('dlights: key reuse, expiry and the CL_RunDLights early return', () => {
    const { c, fx } = setup();
    c.cl.time = 100;
    const d1 = CL_AllocDlight(fx, 7);
    d1.radius = 50;
    d1.die = 200;
    expect(CL_AllocDlight(fx, 7)).toBe(d1);
    d1.radius = 50;
    d1.die = 50; // already dead
    const d2 = fx.cl_dlights[1]!;
    d2.radius = 80;
    d2.die = 1000;
    d2.decay = 100;
    c.cls.frametime = 0.5;
    CL_RunDLights(fx);
    expect(d1.radius).toBe(0);
    expect(d2.radius).toBe(80); // not reached: C returns after the first expired light
    CL_RunDLights(fx);
    expect(d2.radius).toBe(30);
  });

  it('particle free list: exhausts at MAX_PARTICLES and recycles faded particles', () => {
    const { c, fx } = setup();
    c.cl.time = 0;
    CL_BigTeleportParticles(fx, [0, 0, 0]); // 4096 = all of them
    expect(fx.free_particles).toBeNull();
    const before = c.rand.save();
    CL_ParticleEffect(fx, [0, 0, 0], [0, 0, 1], 0, 10); // returns immediately, no rand() drawn
    expect(c.rand.save()).toEqual(before);
    c.cl.time = 10000;
    V_ClearScene(c);
    CL_AddParticles(fx);
    expect(fx.active_particles).toBeNull();
    let n = 0;
    for (let p = fx.free_particles; p; p = p.next) n++;
    expect(n).toBe(4096);
  });

  it('CL_AddParticles physics and INSTANT_PARTICLE', () => {
    const { c, fx } = setup();
    const p = allocParticle(fx)!;
    p.time = 1000;
    p.org.set([1, 2, 3]);
    p.vel.set([10, 0, 0]);
    p.accel.set([0, 0, -40]);
    p.alpha = 1;
    p.alphavel = -0.5;
    p.color = 0xe3;
    const q = allocParticle(fx)!; // added first (list head), instant
    q.time = 0;
    q.org.set([5, 5, 5]);
    q.vel.set([1, 1, 1]);
    q.accel.set([0, 0, 0]);
    q.alpha = 0.75;
    q.alphavel = INSTANT_PARTICLE;
    q.color = 1;
    c.cl.time = 1500;
    V_ClearScene(c);
    CL_AddParticles(fx);
    const v = c.view;
    expect(v.r_numparticles).toBe(2);
    // q first: time is still 0 (nothing computed yet)
    expect(Array.from(v.r_particles[0]!.origin)).toEqual([5, 5, 5]);
    expect(v.r_particles[0]!.alpha).toBe(0.75);
    // p: t = 0.5 s
    expect(Array.from(v.r_particles[1]!.origin)).toEqual([6, 2, 3 - 40 * 0.25]);
    expect(v.r_particles[1]!.alpha).toBe(0.75);
    expect(v.r_particles[1]!.color).toBe(0xe3);
    expect(q.alpha).toBe(0);
    expect(q.alphavel).toBe(0);
  });

  it('CL_ParticleEffect draws 1 + 1 + 3*2 + 1 rand() per particle', () => {
    const { c, fx } = setup();
    const r0 = c.rand.save();
    CL_ParticleEffect(fx, [0, 0, 0], [0, 0, 1], 0, 5);
    const r1 = c.rand.save();
    c.rand.restore(r0);
    for (let i = 0; i < 5 * 9; i++) c.rand.rand();
    expect(c.rand.save()).toEqual(r1);
  });
});
