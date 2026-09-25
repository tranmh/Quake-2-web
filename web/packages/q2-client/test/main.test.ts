// cl_main.c / cl_input.c / engine tests over an in-memory transport with a scripted fake server.
import { describe, expect, it } from 'vitest';
import {
  BUTTON_ATTACK,
  NS_SERVER,
  UserCmd,
  clc_move,
  clc_stringcmd,
  clc_userinfo,
  latin1FromBytes,
  latin1ToBytes,
  svc_disconnect,
  svc_stufftext,
} from 'q2-shared';
import {
  COM_BlockSequenceCRCByte,
  MSG_ReadByte,
  MSG_ReadDeltaUsercmd,
  MSG_ReadLong,
  MSG_ReadShort,
  MSG_ReadString,
  MSG_WriteByte,
  MSG_WriteString,
  NetChan,
  SizeBuf,
} from 'q2-protocol';
import { createClientEngine, type ClientEngine } from '../src/engine';
import { ca_active, ca_connected, ca_connecting, ca_disconnected } from '../src/client';
import { MemoryTransport } from '../src/transport';
import { CL_KeyState } from '../src/cl_input';
import { createRecordingRefresh } from './helpers';

interface Harness {
  engine: ClientEngine;
  server: MemoryTransport;
  clock: { t: number };
  prints: string[];
  states: number[];
  errors: string[];
  /** advance the clock and run one frame */
  step(ms?: number): void;
  oob(): string[];
}

async function harness(extraCfg = ''): Promise<Harness> {
  const clock = { t: 100000 };
  let server: MemoryTransport | null = null;
  const prints: string[] = [];
  const states: number[] = [];
  const errors: string[] = [];
  const engine = await createClientEngine({
    refresh: createRecordingRefresh(),
    transport: () => {
      const [a, b] = MemoryTransport.pair(true);
      server = b;
      return a;
    },
    loadFile: async () => null,
    milliseconds: () => clock.t,
    host: {
      onPrint: (t) => prints.push(t),
      onState: (s) => states.push(s),
      onError: (m) => errors.push(m),
    },
    configText: extraCfg,
    args: ['+set', 'qport', '4242'],
  });
  const h: Harness = {
    engine,
    get server() {
      return server!;
    },
    clock,
    prints,
    states,
    errors,
    step(ms = 20) {
      clock.t += ms;
      engine.frame(ms);
    },
    oob() {
      return h.server.sent.length ? [] : [];
    },
  };
  return h;
}

/** Datagrams the client sent (the server end's peer = client end records them). */
function clientSent(h: Harness): Uint8Array[] {
  return h.server.peer!.sent;
}

function isOOB(d: Uint8Array): boolean {
  return d[0] === 0xff && d[1] === 0xff && d[2] === 0xff && d[3] === 0xff;
}

function oobText(d: Uint8Array): string {
  return latin1FromBytes(d, 4);
}

function serverOOB(h: Harness, text: string): void {
  const b = new Uint8Array(4 + text.length);
  b.fill(0xff, 0, 4);
  b.set(latin1ToBytes(text), 4);
  h.server.send(b);
}

/** A server-side netchan speaking to the client through the memory transport. */
function serverChannel(h: Harness): NetChan<string> {
  const ch = new NetChan<string>({
    sendPacket: (_s, data) => h.server.send(data),
    curtime: () => h.clock.t,
    qport: 0,
  });
  ch.setup(NS_SERVER, 'client', 4242);
  return ch;
}

async function connected(): Promise<{ h: Harness; sv: NetChan<string> }> {
  const h = await harness();
  h.engine.connect('memory');
  h.step();
  serverOOB(h, 'challenge 1234');
  h.step();
  serverOOB(h, 'client_connect');
  h.step();
  return { h, sv: serverChannel(h) };
}

describe('connection handshake (CL_CheckForResend / CL_ConnectionlessPacket)', () => {
  it('sends getchallenge, connect with userinfo, then "new" over the netchan', async () => {
    const h = await harness();
    h.engine.connect('memory');
    expect(h.engine.context.cls.state).toBe(ca_connecting);
    h.step();
    const sent = clientSent(h);
    expect(sent.length).toBe(1);
    expect(isOOB(sent[0]!)).toBe(true);
    expect(oobText(sent[0]!)).toBe('getchallenge\n');
    expect(h.prints.join('')).toContain('Connecting to memory...');

    // no reply: resend after 3 s only
    h.step(1000);
    expect(sent.length).toBe(1);
    h.step(2100);
    expect(sent.length).toBe(2);

    serverOOB(h, 'challenge 1234');
    h.step();
    const connect = oobText(sent[sent.length - 1]!);
    expect(connect).toMatch(/^connect 34 4242 1234 "/);
    expect(connect).toContain('\\name\\unnamed');
    expect(connect).toContain('\\skin\\male/grunt');
    expect(connect.endsWith('"\n')).toBe(true);

    serverOOB(h, 'client_connect');
    h.step();
    const c = h.engine.context;
    expect(c.cls.state).toBe(ca_connected);
    // "new" went out in the same frame (CL_SendCmd sees a pending reliable message)
    const n = sent.length;
    const pkt = sent[n - 1]!;
    // CL_Frame then waits 100 ms per frame while connected; keepalives only after 1 s of silence
    h.step(20);
    h.step(100);
    expect(sent.length).toBe(n);
    for (let i = 0; i < 9; i++) h.step(110);
    expect(sent.length).toBe(n + 1);
    const msg = new SizeBuf(pkt, pkt.length);
    msg.cursize = pkt.length;
    const seq = MSG_ReadLong(msg);
    expect(seq & 0x7fffffff).toBe(1);
    expect(seq >>> 31).toBe(1); // reliable
    MSG_ReadLong(msg);
    expect(MSG_ReadShort(msg)).toBe(4242); // qport
    expect(MSG_ReadByte(msg)).toBe(clc_stringcmd);
    expect(MSG_ReadString(msg)).toBe('new');
    expect(h.states).toEqual([ca_disconnected, ca_connecting, ca_connected]);
  });

  it('handles print / echo / unknown OOB commands and ignores remote "cmd"', async () => {
    const h = await harness();
    h.engine.connect('memory');
    h.step();
    serverOOB(h, 'print\nhello there\n');
    serverOOB(h, 'echo abc');
    serverOOB(h, 'cmd quit');
    serverOOB(h, 'bogus');
    h.step();
    const text = h.prints.join('');
    expect(text).toContain('hello there\n');
    expect(text).toContain('Command packet from remote host.  Ignored.');
    expect(text).toContain('Unknown command.');
    const sent = clientSent(h);
    expect(oobText(sent[sent.length - 1]!)).toBe('abc');
  });
});

describe('server messages through the netchan', () => {
  it('stufftext is executed and svc_disconnect drops without an error', async () => {
    const { h, sv } = await connected();
    const c = h.engine.context;
    const m = new SizeBuf(1400);
    MSG_WriteByte(m, svc_stufftext);
    MSG_WriteString(m, 'set foo bar\n');
    sv.transmit(m.cursize, m.data);
    h.step(120);
    h.step(20); // the stuffed text runs on the next Cbuf_Execute
    expect(c.cvars.variableString('foo')).toBe('bar');

    m.cursize = 0;
    MSG_WriteByte(m, svc_disconnect);
    sv.transmit(m.cursize, m.data);
    h.step(120);
    expect(c.cls.state).toBe(ca_disconnected);
    expect(h.errors).toEqual([]);
    // the three "disconnect" stringcmds were sent
    const last = clientSent(h).slice(-3);
    for (const d of last) expect(latin1FromBytes(d, 10)).toContain('disconnect');
  });

  it('times out after cl_timeout seconds of silence', async () => {
    const { h } = await connected();
    const c = h.engine.context;
    c.cvars.set('cl_timeout', '1');
    for (let i = 0; i < 20; i++) h.step(200);
    expect(c.cls.state).toBe(ca_disconnected);
    expect(h.prints.join('')).toContain('Server connection timed out.');
  });

  it('forwards unknown commands as clc_stringcmd only when connected', async () => {
    const h0 = await harness();
    h0.engine.exec('foobar 1');
    h0.engine.exec('kill');
    h0.step();
    expect(h0.prints.join('')).toContain('Unknown command "foobar"');
    // "kill" is registered with a NULL function: forwarded through "cmd", which needs a connection
    expect(h0.prints.join('')).toContain('Can\'t "cmd", not connected');

    const { h } = await connected();
    const c = h.engine.context;
    c.cls.state = ca_active;
    c.cls.netchan.message.cursize = 0;
    h.engine.exec('say hello world');
    c.cmd.cbufExecute();
    const msg = c.cls.netchan.message;
    const bytes = latin1FromBytes(msg.data, 0, msg.cursize);
    // "say" is registered with a NULL function -> Cmd_ExecuteString("cmd say hello world")
    expect(bytes).toBe(String.fromCharCode(clc_stringcmd) + 'say hello world\0');
  });
});

describe('CL_SendCmd (clc_move)', () => {
  it('writes the checksummed move with 3 delta usercmds and the last frame', async () => {
    const { h } = await connected();
    const c = h.engine.context;
    c.cls.state = ca_active;
    c.cl.frame.valid = true;
    c.cl.frame.serverframe = 77;
    c.cls.demowaiting = false;
    c.cvars.userinfoModified = false;
    c.cls.netchan.message.cursize = 0;

    // +forward pressed by key 119 at t, +attack by mouse1
    const t = h.clock.t;
    c.cmd.executeString(`+forward 119 ${t}`);
    c.cmd.executeString(`+attack 200 ${t}`);
    const before = clientSent(h).length;
    h.step(20);
    const pkt = clientSent(h)[before]!;
    const msg = new SizeBuf(pkt, pkt.length);
    msg.cursize = pkt.length;
    const seq = MSG_ReadLong(msg) & 0x7fffffff;
    MSG_ReadLong(msg);
    MSG_ReadShort(msg);
    expect(MSG_ReadByte(msg)).toBe(clc_move);
    const checksumIndex = msg.readcount;
    const checksum = MSG_ReadByte(msg);
    expect(MSG_ReadLong(msg)).toBe(77);
    const nullcmd = new UserCmd();
    const a = new UserCmd();
    const b = new UserCmd();
    const cc = new UserCmd();
    MSG_ReadDeltaUsercmd(msg, nullcmd, a);
    MSG_ReadDeltaUsercmd(msg, a, b);
    MSG_ReadDeltaUsercmd(msg, b, cc);
    expect(msg.readcount).toBe(msg.cursize);
    const crc = COM_BlockSequenceCRCByte(
      pkt.subarray(checksumIndex + 1),
      msg.cursize - checksumIndex - 1,
      seq,
    );
    expect(checksum).toBe(crc);
    expect(cc.forwardmove).toBe(200);
    expect(cc.buttons & BUTTON_ATTACK).toBe(BUTTON_ATTACK);
    expect(cc.msec).toBe(20);
  });

  it('sends clc_userinfo when a userinfo cvar changes', async () => {
    const { h } = await connected();
    const c = h.engine.context;
    c.cls.state = ca_active;
    c.cls.netchan.message.cursize = 0;
    c.cvars.set('name', 'player2');
    expect(c.cvars.userinfoModified).toBe(true);
    h.step(20);
    expect(c.cvars.userinfoModified).toBe(false);
    // queued on the reliable stream (the previous reliable "new" is still unacknowledged)
    const m = c.cls.netchan.message;
    const text = latin1FromBytes(m.data, 0, m.cursize);
    expect(text.charCodeAt(0)).toBe(clc_userinfo);
    expect(text).toContain('\\name\\player2');
  });
});

describe('kbutton state machine (KeyDown / KeyUp / CL_KeyState)', () => {
  it('accumulates held time as a fraction of the frame', async () => {
    const h = await harness();
    const c = h.engine.context;
    const inp = c.input;
    inp.sys_frame_time = 1000;
    inp.frame_msec = 50;
    c.cmd.executeString('+forward 17 980');
    expect(inp.in_forward.state).toBe(3);
    // still down: 1000 - 980 = 20 ms of 50
    expect(CL_KeyState(c, inp.in_forward)).toBeCloseTo(0.4, 6);
    inp.sys_frame_time = 1050;
    c.cmd.executeString('-forward 17 1030');
    // up at 1030: 30 ms held since downtime 1000 -> 0.6
    expect(CL_KeyState(c, inp.in_forward)).toBeCloseTo(0.6, 6);
    expect(inp.in_forward.state).toBe(0);
    // two keys: released only when both are up
    c.cmd.executeString('+back 1 1050');
    c.cmd.executeString('+back 2 1050');
    c.cmd.executeString('-back 1 1060');
    expect(inp.in_back.state & 1).toBe(1);
    c.cmd.executeString('-back 2 1070');
    expect(inp.in_back.state & 1).toBe(0);
    c.cmd.executeString('+back 1 1070');
    c.cmd.executeString('+back 2 1070');
    c.cmd.executeString('+back 3 1070');
    expect(h.prints.join('')).toContain('Three keys down for a button!');
    // typed at the console without a key number: unstick
    c.cmd.executeString('-back');
    expect(inp.in_back.state).toBe(4);
  });
});

describe('demo recording guard', () => {
  it('refuses to record outside a level', async () => {
    const h = await harness();
    h.engine.exec('record x');
    h.step();
    expect(h.prints.join('')).toContain('You must be in a level to record.');
    h.engine.exec('stop');
    h.step();
    expect(h.prints.join('')).toContain('Not recording a demo.');
  });
});

describe('CL_Frame pacing', () => {
  it('honours cl_maxfps by accumulating msec', async () => {
    const h = await harness('set cl_maxfps 10\n');
    const c = h.engine.context;
    const f0 = c.cls.framecount;
    for (let i = 0; i < 9; i++) h.step(10);
    expect(c.cls.framecount).toBe(f0);
    h.step(10);
    expect(c.cls.framecount).toBe(f0 + 1);
    expect(c.cls.frametime).toBeCloseTo(0.1, 6);
    expect(ca_active).toBe(4);
  });
});
