import { describe, expect, it } from 'vitest';
import {
  CS_LIGHTS,
  CS_MODELS,
  CS_PLAYERSKINS,
  CS_SOUNDS,
  ERR_DISCONNECT,
  MAX_ITEMS,
  PRINT_CHAT,
  PRINT_HIGH,
  PROTOCOL_VERSION,
  SND_ENT,
  SND_POS,
  SND_VOLUME,
  TE_GUNSHOT,
  TE_STEAM,
  U_REMOVE,
  EntityState,
  PlayerState,
  svc_centerprint,
  svc_configstring,
  svc_disconnect,
  svc_frame,
  svc_inventory,
  svc_layout,
  svc_muzzleflash,
  svc_nop,
  svc_packetentities,
  svc_print,
  svc_serverdata,
  svc_sound,
  svc_spawnbaseline,
  svc_stufftext,
  svc_temp_entity,
} from 'q2-shared';
import {
  MSG_WriteByte,
  MSG_WriteDeltaEntity,
  MSG_WriteDir,
  MSG_WriteLong,
  MSG_WritePos,
  MSG_WriteShort,
  MSG_WriteString,
  SizeBuf,
  writePlayerstateToClient,
} from 'q2-protocol';
import type { ModelHandle } from 'q2-ref';
import { ClientContext, DropError, ca_active, ca_connected } from '../src/client';
import { NullEffects } from '../src/null_effects';
import { NullSound, type SfxHandle, type Sound } from '../src/sound';
import { CL_InitLocal } from '../src/cl_main';
import { SCR_Init } from '../src/cl_scrn';
import { V_Init } from '../src/cl_view';
import { NullCinematics } from '../src/cinematic';
import { MemoryTransport } from '../src/transport';
import { CL_ParseServerMessage } from '../src/cl_parse';
import { CL_AddPacketEntities } from '../src/cl_ents';
import { V_ClearScene } from '../src/cl_view';
import { createRecordingRefresh } from './helpers';

class RecSound extends NullSound {
  started: { origin: number[] | null; ent: number; chan: number; vol: number; att: number; ofs: number }[] =
    [];
  local: string[] = [];
  constructor() {
    super();
    const self = this as unknown as Sound;
    self.startSound = (origin, entnum, entchannel, _sfx, fvol, attenuation, timeofs) => {
      this.started.push({
        origin: origin ? Array.from(origin) : null,
        ent: entnum,
        chan: entchannel,
        vol: fvol,
        att: attenuation,
        ofs: timeofs,
      });
    };
    self.startLocalSound = (name) => {
      this.local.push(name);
    };
  }
}

class RecEffects extends NullEffects {
  lightstyles: number[] = [];
  events: number[] = [];
  override setLightstyle(i: number): void {
    this.lightstyles.push(i);
  }
  override entityEvent(ent: EntityState): void {
    this.events.push(ent.number);
  }
}

function setup() {
  const sound = new RecSound();
  const fx = new RecEffects();
  const c = new ClientContext({
    refresh: createRecordingRefresh(),
    transport: () => new MemoryTransport(),
    loadFile: async () => null,
    sound,
    effects: fx,
    cinematics: new NullCinematics(),
  });
  fx.attach(c);
  c.cin.attach(c);
  CL_InitLocal(c);
  SCR_Init(c);
  V_Init(c);
  const prints: string[] = [];
  c.main.printHook = (m) => prints.push(m);
  return { c, sound, fx, prints };
}

function run(c: ClientContext, build: (m: SizeBuf) => void): void {
  const m = new SizeBuf(1400);
  build(m);
  c.net_message.data.set(m.data.subarray(0, m.cursize));
  c.net_message.cursize = m.cursize;
  c.net_message.readcount = 0;
  CL_ParseServerMessage(c);
}

function serverdata(m: SizeBuf, proto = PROTOCOL_VERSION): void {
  MSG_WriteByte(m, svc_serverdata);
  MSG_WriteLong(m, proto);
  MSG_WriteLong(m, 7);
  MSG_WriteByte(m, 0);
  MSG_WriteString(m, 'baseq2');
  MSG_WriteShort(m, 0);
  MSG_WriteString(m, 'The Edge');
}

function cs(m: SizeBuf, i: number, s: string): void {
  MSG_WriteByte(m, svc_configstring);
  MSG_WriteShort(m, i);
  MSG_WriteString(m, s);
}

function ent(num: number, x: number, model = 1): EntityState {
  const e = new EntityState();
  e.number = num;
  e.modelindex = model;
  e.origin[0] = x;
  e.origin[1] = 16;
  e.origin[2] = -8;
  return e;
}

const nullstate = new EntityState();

describe('CL_ParseServerMessage', () => {
  it('parses serverdata and rejects other protocols', () => {
    const { c, prints } = setup();
    run(c, (m) => serverdata(m));
    expect(c.cls.state).toBe(ca_connected);
    expect(c.cl.servercount).toBe(7);
    expect(c.cl.playernum).toBe(0);
    expect(c.cl.gamedir).toBe('baseq2');
    expect(c.cl.refresh_prepped).toBe(false);
    expect(prints.join('')).toContain('\x02The Edge\n');

    const { c: c2 } = setup();
    expect(() => run(c2, (m) => serverdata(m, 33))).toThrowError(/Server returned version 33, not 34/);
  });

  it('parses configstrings, baselines and frames (non-delta, delta, removal)', () => {
    const { c, fx } = setup();
    const b1 = ent(1, 100);
    const b2 = ent(2, 200, 2);
    run(c, (m) => {
      serverdata(m);
      cs(m, CS_MODELS + 1, 'maps/demo1.bsp');
      cs(m, CS_SOUNDS + 1, 'misc/talk.wav');
      cs(m, CS_LIGHTS + 3, 'mmnmmommommnonmmonqnmmo');
      cs(m, CS_PLAYERSKINS + 0, 'Player\\male/grunt');
      MSG_WriteByte(m, svc_spawnbaseline);
      MSG_WriteDeltaEntity(nullstate, b1, m, true, true);
      MSG_WriteByte(m, svc_spawnbaseline);
      MSG_WriteDeltaEntity(nullstate, b2, m, true, true);
    });
    expect(c.cl.configstrings[CS_MODELS + 1]).toBe('maps/demo1.bsp');
    expect(c.cl.configstrings[CS_PLAYERSKINS]).toBe('Player\\male/grunt');
    expect(fx.lightstyles).toEqual([3]);
    expect(c.cl_entities[1]!.baseline.origin[0]).toBe(100);
    expect(c.cl_entities[2]!.baseline.modelindex).toBe(2);

    // frame 1: uncompressed
    const ps1 = new PlayerState();
    ps1.pmove.origin[0] = 800;
    ps1.pmove.origin[1] = -80;
    ps1.pmove.origin[2] = 200;
    ps1.fov = 90;
    ps1.stats[1] = 100;
    const f1e1 = ent(1, 104);
    f1e1.old_origin.set(b1.origin); // newentity: U_OLDORIGIN carries the previous origin
    const f1e2 = ent(2, 200, 2);
    f1e2.event = 2;
    run(c, (m) => {
      MSG_WriteByte(m, svc_frame);
      MSG_WriteLong(m, 1);
      MSG_WriteLong(m, -1);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 1);
      MSG_WriteByte(m, 0xff);
      writePlayerstateToClient(null, ps1, m);
      MSG_WriteByte(m, svc_packetentities);
      MSG_WriteDeltaEntity(b1, f1e1, m, false, true);
      MSG_WriteDeltaEntity(b2, f1e2, m, false, true);
      MSG_WriteShort(m, 0);
    });
    expect(c.cls.state).toBe(ca_active);
    expect(c.cl.frame.valid).toBe(true);
    expect(c.cl.frame.serverframe).toBe(1);
    expect(c.cl.frame.servertime).toBe(100);
    expect(c.cl.frame.num_entities).toBe(2);
    expect(c.cl.frame.areabits[0]).toBe(0xff);
    expect(Array.from(c.cl.frame.playerstate.pmove.origin)).toEqual([800, -80, 200]);
    expect(c.cl.frame.playerstate.stats[1]).toBe(100);
    expect(Array.from(c.cl.predicted_origin)).toEqual([100, -10, 25]);
    expect(c.cl_parse_entities[0]!.origin[0]).toBe(104);
    expect(c.cl_parse_entities[1]!.number).toBe(2);
    expect(fx.events).toEqual([2]);
    // first appearance: prev == current with origin from old_origin (the baseline origin)
    expect(c.cl_entities[1]!.prev.origin[0]).toBe(100);
    expect(c.cl_entities[1]!.current.origin[0]).toBe(104);
    expect(c.cl.frames[1]!.serverframe).toBe(1);

    // frame 2: delta from 1, entity 1 moves, entity 2 removed
    const ps2 = new PlayerState().copyFrom(ps1);
    ps2.pmove.origin[0] = 816;
    const f2e1 = ent(1, 120);
    run(c, (m) => {
      MSG_WriteByte(m, svc_frame);
      MSG_WriteLong(m, 2);
      MSG_WriteLong(m, 1);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 0);
      writePlayerstateToClient(ps1, ps2, m);
      MSG_WriteByte(m, svc_packetentities);
      MSG_WriteDeltaEntity(f1e1, f2e1, m, false, false);
      MSG_WriteByte(m, U_REMOVE);
      MSG_WriteByte(m, 2);
      MSG_WriteShort(m, 0);
    });
    expect(c.cl.frame.valid).toBe(true);
    expect(c.cl.frame.deltaframe).toBe(1);
    expect(c.cl.frame.num_entities).toBe(1);
    expect(c.cl.frame.playerstate.pmove.origin[0]).toBe(816);
    expect(c.cl.frame.playerstate.stats[1]).toBe(100);
    const ce = c.cl_entities[1]!;
    expect(ce.prev.origin[0]).toBe(104);
    expect(ce.current.origin[0]).toBe(120);
    expect(ce.serverframe).toBe(2);
    expect(c.cl_entities[2]!.serverframe).toBe(1);

    // frame 3: delta with no entity changes copies entity 1 unchanged
    run(c, (m) => {
      MSG_WriteByte(m, svc_frame);
      MSG_WriteLong(m, 3);
      MSG_WriteLong(m, 2);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 0);
      writePlayerstateToClient(ps2, ps2, m);
      MSG_WriteByte(m, svc_packetentities);
      MSG_WriteShort(m, 0);
    });
    expect(c.cl.frame.num_entities).toBe(1);
    expect(ce.prev.origin[0]).toBe(120);
    expect(ce.current.origin[0]).toBe(120);

    // interpolation: entity 1 between frame 2 (104 -> 120) at lerpfrac 0.5 after one more move
    const f4e1 = ent(1, 140);
    run(c, (m) => {
      MSG_WriteByte(m, svc_frame);
      MSG_WriteLong(m, 4);
      MSG_WriteLong(m, 3);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 0);
      writePlayerstateToClient(ps2, ps2, m);
      MSG_WriteByte(m, svc_packetentities);
      MSG_WriteDeltaEntity(f2e1, f4e1, m, false, false);
      MSG_WriteShort(m, 0);
    });
    c.cl.model_draw[1] = { name: 'm1' } as unknown as ModelHandle;
    c.cl.lerpfrac = 0.5;
    c.cl.playernum = 5; // not our entity
    V_ClearScene(c);
    CL_AddPacketEntities(c, c.cl.frame);
    expect(c.view.r_numentities).toBe(1);
    const re = c.view.r_entities[0]!;
    expect(re.origin[0]).toBe(130);
    expect(re.oldorigin[0]).toBe(130);
    expect(re.backlerp).toBe(0.5);
    expect(re.model).toBe(c.cl.model_draw[1]);
    expect(ce.lerp_origin[0]).toBe(130);
  });

  it('flags a delta from a frame that is too old as invalid', () => {
    const { c, prints } = setup();
    run(c, (m) => serverdata(m));
    const ps = new PlayerState();
    run(c, (m) => {
      MSG_WriteByte(m, svc_frame);
      MSG_WriteLong(m, 20);
      MSG_WriteLong(m, 3);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 0);
      writePlayerstateToClient(null, ps, m);
      MSG_WriteByte(m, svc_packetentities);
      MSG_WriteShort(m, 0);
    });
    expect(c.cl.frame.valid).toBe(false);
    expect(c.cls.state).toBe(ca_connected);
    expect(prints.join('')).toContain('Delta frame too old.');
  });

  it('handles print/centerprint/stufftext/layout/inventory/sound/temp entities/nop', () => {
    const { c, sound, prints } = setup();
    c.cl.sound_precache[3] = { name: 'x' } as unknown as SfxHandle;
    run(c, (m) => {
      MSG_WriteByte(m, svc_nop);
      MSG_WriteByte(m, svc_print);
      MSG_WriteByte(m, PRINT_HIGH);
      MSG_WriteString(m, 'hello\n');
      MSG_WriteByte(m, svc_print);
      MSG_WriteByte(m, PRINT_CHAT);
      MSG_WriteString(m, 'chat\n');
      MSG_WriteByte(m, svc_centerprint);
      MSG_WriteString(m, 'center');
      MSG_WriteByte(m, svc_stufftext);
      MSG_WriteString(m, 'echo hi\n');
      MSG_WriteByte(m, svc_layout);
      MSG_WriteString(m, 'xv 0 yv 0 string "x"');
      MSG_WriteByte(m, svc_inventory);
      for (let i = 0; i < MAX_ITEMS; i++) MSG_WriteShort(m, i === 7 ? 25 : 0);
      MSG_WriteByte(m, svc_sound);
      MSG_WriteByte(m, SND_VOLUME | SND_ENT | SND_POS);
      MSG_WriteByte(m, 3);
      MSG_WriteByte(m, 255);
      MSG_WriteShort(m, (12 << 3) | 2);
      MSG_WritePos(m, [8, 16, 24]);
      MSG_WriteByte(m, svc_sound);
      MSG_WriteByte(m, 0);
      MSG_WriteByte(m, 4); // not precached: ignored
      MSG_WriteByte(m, svc_temp_entity);
      MSG_WriteByte(m, TE_GUNSHOT);
      MSG_WritePos(m, [1, 2, 3]);
      MSG_WriteDir(m, [0, 0, 1]);
      MSG_WriteByte(m, svc_temp_entity);
      MSG_WriteByte(m, TE_STEAM);
      MSG_WriteShort(m, 5);
      MSG_WriteByte(m, 10);
      MSG_WritePos(m, [1, 2, 3]);
      MSG_WriteDir(m, [0, 0, 1]);
      MSG_WriteByte(m, 3);
      MSG_WriteShort(m, 40);
      MSG_WriteLong(m, 1000);
      MSG_WriteByte(m, svc_muzzleflash);
      MSG_WriteShort(m, 1);
      MSG_WriteByte(m, 2);
      MSG_WriteByte(m, svc_nop);
    });
    const out = prints.join('');
    expect(out).toContain('hello\n');
    expect(out).toContain('chat\n');
    expect(sound.local).toEqual(['misc/talk.wav']);
    expect(c.cmd.text).toBe('echo hi\n');
    expect(c.cl.layout).toBe('xv 0 yv 0 string "x"');
    expect(c.cl.inventory[7]).toBe(25);
    expect(sound.started).toEqual([{ origin: [8, 16, 24], ent: 12, chan: 2, vol: 1, att: 1, ofs: 0 }]);
    expect(c.net_message.readcount).toBe(c.net_message.cursize + 1);
  });

  it('drops on disconnect, bad temp entities and illegible commands', () => {
    const { c } = setup();
    try {
      run(c, (m) => MSG_WriteByte(m, svc_disconnect));
      expect.unreachable();
    } catch (e) {
      expect(e).toBeInstanceOf(DropError);
      expect((e as DropError).code).toBe(ERR_DISCONNECT);
    }
    expect(() =>
      run(c, (m) => {
        MSG_WriteByte(m, svc_temp_entity);
        MSG_WriteByte(m, 200);
      }),
    ).toThrowError(/CL_ParseTEnt: bad type/);
    expect(() => run(c, (m) => MSG_WriteByte(m, 77))).toThrowError(/Illegible server message/);
  });

  it('emulates the overlapping char[MAX_QPATH] configstring slots (CS_STATUSBAR)', () => {
    const { c } = setup();
    run(c, (m) => serverdata(m));
    const bar = 'x'.repeat(64) + 'y'.repeat(64) + 'tail';
    run(c, (m) => {
      cs(m, 5, bar);
      cs(m, 6, bar.slice(64)); // the server sends the overlapping tails too
      cs(m, 7, bar.slice(128));
    });
    expect(c.cl.configstrings[5]).toBe(bar);
    expect(c.cl.configstrings[6]).toBe(bar.slice(64));
    expect(c.cl.configstrings[7]).toBe('tail');
    run(c, (m) => cs(m, 7, 'TAIL2'));
    expect(c.cl.configstrings[5]).toBe(bar.slice(0, 128) + 'TAIL2');
    expect(c.cl.configstrings[6]).toBe('y'.repeat(64) + 'TAIL2');
  });
});
