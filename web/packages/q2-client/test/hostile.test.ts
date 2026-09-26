// Hostile-server robustness (docs/review/04-ts-engine.md). The browser client connects to user-run
// servers and plays user-supplied demos, so every server message is attacker-controlled.
import { describe, expect, it } from 'vitest';
import {
  CS_MODELS,
  CVAR_ARCHIVE,
  MAX_EDICTS,
  PROTOCOL_VERSION,
  PlayerState,
  SND_ENT,
  TE_LIGHTNING,
  U_MODEL,
  U_MOREBITS1,
  U_NUMBER16,
  svc_configstring,
  svc_download,
  svc_frame,
  svc_packetentities,
  svc_serverdata,
  svc_sound,
  svc_spawnbaseline,
  svc_stufftext,
  svc_temp_entity,
} from 'q2-shared';
import {
  MSG_WriteByte,
  MSG_WriteLong,
  MSG_WritePos,
  MSG_WriteShort,
  MSG_WriteString,
  SizeBuf,
  writePlayerstateToClient,
} from 'q2-protocol';
import { ClientContext, DropError, ca_active } from '../src/client';
import { NullSound, type Sound } from '../src/sound';
import { NullCinematics } from '../src/cinematic';
import { MemoryTransport } from '../src/transport';
import {
  CL_InitLocal,
  CL_CheckForResend,
  CL_ClearState,
  CL_WriteDemoMessage,
  MAX_QUEUED_PACKETS,
} from '../src/cl_main';
import { CL_ParseServerMessage } from '../src/cl_parse';
import { CL_AddNetgraph, SCR_Init } from '../src/cl_scrn';
import { CL_PrepRefresh, V_Init } from '../src/cl_view';
import { Con_Init } from '../src/console';
import { Key_Init, Key_WriteBindings } from '../src/keys';
import { createClientEffects } from '../src/cl_effects';
import { NullEffects } from '../src/null_effects';
import type { Effects } from '../src/effects';
import { createRecordingRefresh } from './helpers';

class RecSound extends NullSound {
  started: { ent: number; chan: number }[] = [];
  constructor() {
    super();
    const self = this as unknown as Sound;
    self.startSound = (_origin, entnum, entchannel) => {
      this.started.push({ ent: entnum, chan: entchannel });
    };
    self.registerSound = (name: string) => ({ name }) as never;
  }
}

function setup(opts: { effects?: Effects; files?: Record<string, string> } = {}) {
  const sound = new RecSound();
  const fx = opts.effects ?? new NullEffects();
  const files = opts.files ?? {};
  const c = new ClientContext({
    refresh: createRecordingRefresh(),
    transport: () => new MemoryTransport(),
    loadFile: async (n) => (n in files ? Uint8Array.from(files[n]!, (ch) => ch.charCodeAt(0)) : null),
    sound,
    effects: fx,
    cinematics: new NullCinematics(),
  });
  fx.attach(c);
  c.cin.attach(c);
  Con_Init(c);
  Key_Init(c);
  CL_InitLocal(c);
  SCR_Init(c);
  V_Init(c);
  c.cmd.loadFile = async (n) => (n in files ? Uint8Array.from(files[n]!, (ch) => ch.charCodeAt(0)) : null);
  const prints: string[] = [];
  c.main.printHook = (m) => prints.push(m);
  const config: string[] = [];
  c.host.onConfig = (kind, name, value) => config.push(`${kind}:${name}=${value}`);
  let quits = 0;
  c.host.onQuit = () => quits++;
  return { c, sound, prints, config, quits: () => quits };
}

function run(c: ClientContext, build: (m: SizeBuf) => void): void {
  const m = new SizeBuf(1400);
  build(m);
  c.net_message.data.set(m.data.subarray(0, m.cursize));
  c.net_message.cursize = m.cursize;
  c.net_message.readcount = 0;
  CL_ParseServerMessage(c);
}

function stuff(c: ClientContext, text: string): void {
  run(c, (m) => {
    MSG_WriteByte(m, svc_stufftext);
    MSG_WriteString(m, text);
  });
  c.cmd.cbufExecute();
}

/** Bytes of the pending clc_stringcmd text as a Latin-1 string. */
function outgoing(c: ClientContext): string {
  return String.fromCharCode(...c.cls.netchan.message.data.subarray(0, c.cls.netchan.message.cursize));
}

describe('svc_stufftext runs restricted', () => {
  it('cannot change key bindings or archived cvars (they persist in the account config)', () => {
    const { c, config, quits } = setup();
    c.cmd.executeString('bind w +forward');
    c.cmd.executeString('set sensitivity 7');
    config.length = 0;
    stuff(
      c,
      'bind w quit\nunbind w\nunbindall\nseta evil 1\nset sensitivity 99\nsensitivity 50\n' +
        'setu sensitivity 1\ntoggle cl_run\nset rcon_password stolen u\nsetu rcon_password x\n',
    );
    expect(c.keys.keybindings['w'.charCodeAt(0)]).toBe('+forward');
    expect(c.cvars.find('evil')).toBeFalsy();
    expect(c.cvars.variableString('sensitivity')).toBe('7');
    expect(c.cvars.variableString('cl_run')).toBe('0');
    expect(c.cvars.find('rcon_password')!.flags).toBe(0);
    expect(c.cvars.variableString('rcon_password')).toBe('');
    expect(config).toEqual([]);
    expect(quits()).toBe(0);
  });

  it('cannot read or use the rcon password', () => {
    const { c } = setup();
    c.cvars.set('rcon_password', 'hunter2');
    c.cls.state = ca_active;
    stuff(c, 'cmd say $rcon_password\nrcon status\n');
    expect(outgoing(c)).not.toContain('hunter2');
    expect(outgoing(c)).toContain('say');
    // a local command still expands it
    c.cmd.executeString('cmd say $rcon_password');
    expect(outgoing(c)).toContain('hunter2');
  });

  it('aliases and exec files defined by the server stay restricted when the user runs them', async () => {
    const { c } = setup({ files: { 'evil.cfg': 'bind w quit\nset sensitivity 42\n' } });
    c.cmd.executeString('bind w +forward');
    stuff(c, 'alias +zoom "bind w quit"\nexec evil.cfg\n');
    await c.cmd.whenIdle();
    c.cmd.cbufAddText('+zoom\n'); // e.g. the user's own `bind z +zoom`
    c.cmd.cbufExecute();
    expect(c.keys.keybindings['w'.charCodeAt(0)]).toBe('+forward');
    expect(c.cvars.variableString('sensitivity')).toBe('3');
  });

  it('still runs everything the servers send legitimately', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    stuff(c, 'spectator 1\nset mymod_var 5\ncmd configstrings 1 0\n');
    expect(c.cvars.variableString('spectator')).toBe('1');
    expect(c.cvars.variableString('mymod_var')).toBe('5');
    expect(outgoing(c)).toContain('configstrings 1 0');
    // and local input keeps full rights
    c.cmd.executeString('bind w quit');
    expect(c.keys.keybindings['w'.charCodeAt(0)]).toBe('quit');
  });
});

describe('saved config text parses back to the same state (it is executed at every start)', () => {
  it('bindings / cvar values with quotes and the semicolon key cannot inject commands', async () => {
    const a = setup();
    a.c.cmd.executeString('bind k a";connect evil.example;"');
    a.c.cmd.executeString('bind SEMICOLON quit');
    a.c.cmd.executeString('set sensitivity x";quit;"'); // (userinfo cvars reject quotes already)
    const cfg = Key_WriteBindings(a.c) + a.c.cvars.writeVariables();

    const b = setup();
    b.c.cmd.cbufAddText(cfg);
    b.c.cmd.cbufExecute();
    await b.c.cmd.whenIdle();
    expect(b.quits()).toBe(0);
    expect(b.c.cls.servername).toBe('');
    expect(b.c.keys.keybindings[59]).toBe('quit');
    expect(b.c.keys.keybindings['k'.charCodeAt(0)]).toBe("a';connect evil.example;'");
    expect(b.c.cvars.variableString('sensitivity')).toBe("x';quit;'");
    expect(b.c.cvars.find('sensitivity')!.flags & CVAR_ARCHIVE).toBeTruthy();
  });
});

describe('hostile messages', () => {
  it('netgraph: a sequence jump of 2^31 packets does not freeze the frame', () => {
    const { c } = setup();
    const s = c.scr;
    s.current = 5;
    c.cls.netchan.dropped = 0x7ffffffe;
    const t0 = performance.now();
    CL_AddNetgraph(c);
    expect(performance.now() - t0).toBeLessThan(1000);
    expect(s.current).toBe(5 + 0x7ffffffe + 1);
    // the ring holds the last 1024 samples: 1023 drop marks and the ping sample
    const last = (s.current - 1) & 1023;
    for (let i = 0; i < 1024; i++) {
      if (i === last) continue;
      expect(s.values[i]!.color).toBe(0x40);
      expect(s.values[i]!.value).toBe(30);
    }
    expect(s.values[last]!.color).toBe(0xd0);
  });

  it('svc_download with a negative size drops instead of re-parsing itself forever', () => {
    const { c } = setup();
    expect(() =>
      run(c, (m) => {
        MSG_WriteByte(m, svc_download);
        MSG_WriteShort(m, -4); // readcount += -4 lands back on svc_download
        MSG_WriteByte(m, 0);
      }),
    ).toThrowError(DropError);
  });

  it('svc_sound on entity -1 or MAX_EDICTS drops without queueing the sound', () => {
    for (const ent of [-1, MAX_EDICTS]) {
      const { c, sound } = setup();
      c.cl.sound_precache[1] = { name: 'x' } as never;
      expect(() =>
        run(c, (m) => {
          MSG_WriteByte(m, svc_sound);
          MSG_WriteByte(m, SND_ENT);
          MSG_WriteByte(m, 1);
          MSG_WriteShort(m, (ent << 3) | 2);
        }),
      ).toThrowError(DropError);
      expect(sound.started).toEqual([]);
    }
  });

  it('TE_LIGHTNING from entity -1 drops without queueing an entity sound', () => {
    const { c, sound } = setup({ effects: createClientEffects() });
    expect(() =>
      run(c, (m) => {
        MSG_WriteByte(m, svc_temp_entity);
        MSG_WriteByte(m, TE_LIGHTNING);
        MSG_WriteShort(m, -1);
        MSG_WriteShort(m, 2);
        MSG_WritePos(m, [0, 0, 0]);
        MSG_WritePos(m, [64, 0, 0]);
      }),
    ).toThrowError(DropError);
    expect(sound.started.filter((s) => s.ent < 0 || s.ent >= MAX_EDICTS)).toEqual([]);
  });

  it('svc_spawnbaseline with an entity number >= MAX_EDICTS is an ERR_DROP, not a TypeError', () => {
    const { c } = setup();
    expect(() =>
      run(c, (m) => {
        MSG_WriteByte(m, svc_serverdata);
        MSG_WriteLong(m, PROTOCOL_VERSION);
        MSG_WriteLong(m, 1);
        MSG_WriteByte(m, 0);
        MSG_WriteString(m, 'baseq2');
        MSG_WriteShort(m, 0);
        MSG_WriteString(m, 'x');
        MSG_WriteByte(m, svc_spawnbaseline);
        MSG_WriteByte(m, U_MOREBITS1);
        MSG_WriteByte(m, (U_NUMBER16 | U_MODEL) >> 8);
        MSG_WriteShort(m, 2000);
        MSG_WriteByte(m, 1);
      }),
    ).toThrowError(/CL_ParseBaseline: bad number/);
  });
});

describe('hostile messages (medium)', () => {
  it('stufftext cannot trigger screenshot downloads', () => {
    const { c } = setup();
    let shots = 0;
    c.cmd.addCommand('screenshot', () => shots++);
    stuff(c, 'screenshot\nscreenshot\n');
    expect(shots).toBe(0);
    c.cmd.executeString('screenshot');
    expect(shots).toBe(1);
  });

  it('the receive queue is bounded (a flooding server in a background tab)', () => {
    const { c } = setup();
    c.cmd.executeString('connect flood.example');
    c.cls.realtime = 10000;
    CL_CheckForResend(c);
    const t = c.transport as MemoryTransport;
    expect(t).toBeTruthy();
    for (let i = 0; i < MAX_QUEUED_PACKETS * 3; i++) t.onMessage!(new Uint8Array(16));
    expect(c.main.packets.length).toBe(MAX_QUEUED_PACKETS);
  });

  it('duplicate out-of-order entity numbers cannot grow a frame past MAX_PARSE_ENTITIES', () => {
    const { c } = setup();
    const frame = (num: number, delta: number, entnum: number) =>
      run(c, (m) => {
        MSG_WriteByte(m, svc_frame);
        MSG_WriteLong(m, num);
        MSG_WriteLong(m, delta);
        MSG_WriteByte(m, 0);
        MSG_WriteByte(m, 0);
        writePlayerstateToClient(null, new PlayerState(), m);
        MSG_WriteByte(m, svc_packetentities);
        for (let i = 0; i < 650; i++) {
          MSG_WriteByte(m, 0); // no fields
          MSG_WriteByte(m, entnum);
        }
        MSG_WriteShort(m, 0);
      });
    frame(1, -1, 1); // 650 copies of entity 1 from the baseline
    expect(c.cl.frame.num_entities).toBe(650);
    // delta: the 650 old entries are carried over, then 650 more baseline entries of entity 2
    expect(() => frame(2, 1, 2)).toThrowError(/too many entities/);
  });

  it('recording while a demo plays keeps the whole message (no netchan header to strip)', () => {
    const { c } = setup();
    c.main.demoplaying = true;
    c.cls.demofile = [];
    c.net_message.data.set([1, 2, 3, 4, 5], 0);
    c.net_message.cursize = 5;
    CL_WriteDemoMessage(c);
    expect(Array.from(c.cls.demofile[0]!)).toEqual([5, 0, 0, 0, 1, 2, 3, 4, 5]);
  });
});

describe('asynchronous level loading races', () => {
  function gated() {
    const ref = createRecordingRefresh();
    let open!: () => void;
    let gate = new Promise<void>((r) => (open = r));
    const hold = { begin: false, sky: false };
    const baseBegin = ref.beginRegistration.bind(ref);
    const baseSky = ref.setSky.bind(ref);
    ref.beginRegistration = async (m: string) => {
      if (hold.begin) await gate;
      return baseBegin(m);
    };
    ref.setSky = async (n, r, a) => {
      if (hold.sky) await gate;
      return baseSky(n, r, a);
    };
    const release = () => {
      open();
      gate = new Promise<void>((r) => (open = r));
    };
    return { ref, hold, release };
  }

  function ctx(ref: ReturnType<typeof createRecordingRefresh>) {
    const c = new ClientContext({
      refresh: ref,
      transport: () => new MemoryTransport(),
      loadFile: async () => null,
      sound: new RecSound(),
      effects: new NullEffects(),
      cinematics: new NullCinematics(),
    });
    c.fx.attach(c);
    c.cin.attach(c);
    Con_Init(c);
    Key_Init(c);
    CL_InitLocal(c);
    SCR_Init(c);
    V_Init(c);
    c.main.printHook = () => {};
    return c;
  }

  it('a prep of the previous level still in flight does not stand in for the new one', async () => {
    const { ref, hold, release } = gated();
    const c = ctx(ref);
    hold.begin = true;
    c.cl.configstrings[CS_MODELS + 1] = 'maps/a.bsp';
    const p1 = CL_PrepRefresh(c);
    // new level: state cleared while level a is still registering
    CL_ClearState(c);
    c.cl.configstrings[CS_MODELS + 1] = 'maps/b.bsp';
    const p2 = CL_PrepRefresh(c);
    expect(p2).not.toBe(p1);
    hold.begin = false;
    release();
    await p2;
    expect(c.cl.refresh_prepped).toBe(true);
    expect(ref.registered.filter((r) => r.kind === 'map').map((r) => r.name)).toEqual(['a', 'b']);
  });

  it('configstrings that change while the refresh is being prepared are registered afterwards', async () => {
    const { ref, hold, release } = gated();
    const c = ctx(ref);
    hold.sky = true;
    c.cl.configstrings[CS_MODELS + 1] = 'maps/a.bsp';
    c.cl.configstrings[CS_MODELS + 2] = 'models/one.md2';
    const p = CL_PrepRefresh(c);
    await new Promise((r) => setTimeout(r, 0));
    // the model loop is done; a model configstring arrives while the sky is loading
    run(c, (m) => {
      MSG_WriteByte(m, svc_configstring);
      MSG_WriteShort(m, CS_MODELS + 3);
      MSG_WriteString(m, 'models/late.md2');
    });
    release();
    await p;
    await new Promise((r) => setTimeout(r, 0));
    expect(c.cl.refresh_prepped).toBe(true);
    expect((c.cl.model_draw[3] as unknown as { name: string } | null)?.name).toBe('models/late.md2');
  });
});
