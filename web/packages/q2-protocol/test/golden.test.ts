// Golden tests against the C oracle fixtures (docs/FIXTURES.md, core/msg/*.jsonl).
// Skipped when $Q2_FIXTURES/core/msg is absent.
import { describe, expect, it } from 'vitest';
import { EntityState, PlayerState, UserCmd, latin1ToBytes } from 'q2-shared';
import { bytesToHex, fileExists, fixturePath, hexToBytes, readJsonl } from 'q2-shared/testing';
import { parseDelta, parseEntityBits, parsePlayerstate } from '../src/cl_ents';
import { COM_BlockSequenceCRCByte, CRC_Block } from '../src/crc';
import { Com_BlockChecksum } from '../src/md4';
import * as M from '../src/msg';
import { SizeBuf } from '../src/sizebuf';
import { writePlayerstateToClient } from '../src/sv_ents';

type Num3 = [number, number, number];
interface ES {
  number: number;
  origin: Num3;
  angles: Num3;
  old_origin: Num3;
  modelindex: number;
  modelindex2: number;
  modelindex3: number;
  modelindex4: number;
  frame: number;
  skinnum: number;
  effects: number;
  renderfx: number;
  solid: number;
  sound: number;
  event: number;
}
interface UC {
  msec: number;
  buttons: number;
  angles: Num3;
  forwardmove: number;
  sidemove: number;
  upmove: number;
  impulse: number;
  lightlevel: number;
}
interface PMS {
  pm_type: number;
  origin: Num3;
  velocity: Num3;
  pm_flags: number;
  pm_time: number;
  gravity: number;
  delta_angles: Num3;
}
interface PS {
  pmove: PMS;
  viewangles: Num3;
  viewoffset: Num3;
  kick_angles: Num3;
  gunangles: Num3;
  gunoffset: Num3;
  gunindex: number;
  gunframe: number;
  blend: number[];
  fov: number;
  rdflags: number;
  stats: number[];
}

const dir = fixturePath('core', 'msg');
const has = (f: string) => fileExists(`${dir}/${f}`);
const load = <T>(f: string) => readJsonl<T>(`${dir}/${f}`);

function toES(j: ES): EntityState {
  const e = new EntityState();
  e.number = j.number;
  e.origin.set(j.origin);
  e.angles.set(j.angles);
  e.old_origin.set(j.old_origin);
  e.modelindex = j.modelindex;
  e.modelindex2 = j.modelindex2;
  e.modelindex3 = j.modelindex3;
  e.modelindex4 = j.modelindex4;
  e.frame = j.frame;
  e.skinnum = j.skinnum;
  e.effects = j.effects >>> 0;
  e.renderfx = j.renderfx;
  e.solid = j.solid;
  e.sound = j.sound;
  e.event = j.event;
  return e;
}

function toUC(j: UC): UserCmd {
  const c = new UserCmd();
  c.msec = j.msec;
  c.buttons = j.buttons;
  c.angles.set(j.angles);
  c.forwardmove = j.forwardmove;
  c.sidemove = j.sidemove;
  c.upmove = j.upmove;
  c.impulse = j.impulse;
  c.lightlevel = j.lightlevel;
  return c;
}

function toPS(j: PS): PlayerState {
  const p = new PlayerState();
  p.pmove.pm_type = j.pmove.pm_type;
  p.pmove.origin.set(j.pmove.origin);
  p.pmove.velocity.set(j.pmove.velocity);
  p.pmove.pm_flags = j.pmove.pm_flags;
  p.pmove.pm_time = j.pmove.pm_time;
  p.pmove.gravity = j.pmove.gravity;
  p.pmove.delta_angles.set(j.pmove.delta_angles);
  p.viewangles.set(j.viewangles);
  p.viewoffset.set(j.viewoffset);
  p.kick_angles.set(j.kick_angles);
  p.gunangles.set(j.gunangles);
  p.gunoffset.set(j.gunoffset);
  p.gunindex = j.gunindex;
  p.gunframe = j.gunframe;
  p.blend.set(j.blend);
  p.fov = j.fov;
  p.rdflags = j.rdflags;
  p.stats.set(j.stats);
  return p;
}

function reader(bytes: Uint8Array): SizeBuf {
  const r = new SizeBuf(bytes);
  r.cursize = bytes.length;
  return r;
}

/** Collects the first mismatch; fails with the line number and details. */
function firstMismatch(file: string, n: number, check: (i: number) => string | null): void {
  for (let i = 0; i < n; i++) {
    const e = check(i);
    if (e) expect.fail(`${file} line ${i + 1}: ${e}`);
  }
}

describe.skipIf(!has('entity.jsonl'))('golden core/msg/entity', () => {
  it('MSG_WriteDeltaEntity bytes and CL_ParseDelta decode', () => {
    const rows = load<{ from: ES; to: ES; force: number; newentity: number; bytes: string }>('entity.jsonl');
    firstMismatch('entity.jsonl', rows.length, (i) => {
      const row = rows[i]!;
      const from = toES(row.from);
      const to = toES(row.to);
      const sb = new SizeBuf(4096);
      M.MSG_WriteDeltaEntity(from, to, sb, !!row.force, !!row.newentity);
      const got = bytesToHex(sb.written());
      if (got !== row.bytes) return `bytes got ${got} want ${row.bytes}`;
      if (!row.bytes) return null;
      // reader side: header + delta consumes exactly the bytes; integer fields decode back.
      const r = reader(hexToBytes(row.bytes));
      const eb = parseEntityBits(r);
      if (eb.number !== to.number) return `parsed number ${eb.number} want ${to.number}`;
      const out = new EntityState();
      parseDelta(from, out, eb.number, eb.bits, r);
      if (r.readcount !== r.cursize) return `parse consumed ${r.readcount} of ${r.cursize}`;
      if ((out.modelindex & 255) !== (to.modelindex & 255)) return 'modelindex';
      if (out.event !== (to.event & 255)) return `event ${out.event} want ${to.event}`;
      if (out.sound !== (to.sound & 255)) return 'sound';
      return null;
    });
  });
});

describe.skipIf(!has('usercmd.jsonl'))('golden core/msg/usercmd', () => {
  it('MSG_WriteDeltaUsercmd bytes and read back', () => {
    const rows = load<{ from: UC; to: UC; bytes: string }>('usercmd.jsonl');
    firstMismatch('usercmd.jsonl', rows.length, (i) => {
      const row = rows[i]!;
      const from = toUC(row.from);
      const to = toUC(row.to);
      const sb = new SizeBuf(64);
      M.MSG_WriteDeltaUsercmd(sb, from, to);
      const got = bytesToHex(sb.written());
      if (got !== row.bytes) return `bytes got ${got} want ${row.bytes}`;
      const out = new UserCmd();
      const r = reader(sb.written().slice());
      M.MSG_ReadDeltaUsercmd(r, from, out);
      if (JSON.stringify(out) !== JSON.stringify(to)) return `readback ${JSON.stringify(out)}`;
      return null;
    });
  });
});

describe.skipIf(!has('player.jsonl'))('golden core/msg/player', () => {
  it('SV_WritePlayerstateToClient bytes and CL_ParsePlayerstate decode', () => {
    const rows = load<{ from: PS | null; to: PS; bytes: string }>('player.jsonl');
    firstMismatch('player.jsonl', rows.length, (i) => {
      const row = rows[i]!;
      const from = row.from ? toPS(row.from) : null;
      const to = toPS(row.to);
      const sb = new SizeBuf(1400 * 4);
      writePlayerstateToClient(from, to, sb);
      const got = bytesToHex(sb.written());
      if (got !== row.bytes) return `bytes got ${got} want ${row.bytes}`;
      const r = reader(hexToBytes(row.bytes));
      r.readcount = 1; // svc_playerinfo
      const out = new PlayerState();
      parsePlayerstate(from, out, r);
      if (r.readcount !== r.cursize) return `parse consumed ${r.readcount} of ${r.cursize}`;
      if (!out.pmove.equals(to.pmove)) return 'pmove state readback';
      for (let k = 0; k < 32; k++) if (out.stats[k] !== to.stats[k]) return `stats[${k}] readback`;
      return null;
    });
  });
});

describe.skipIf(!has('scalar.jsonl'))('golden core/msg/scalar', () => {
  it('scalar writers', () => {
    const rows = load<{ op: string; v: unknown; bytes: string }>('scalar.jsonl');
    firstMismatch('scalar.jsonl', rows.length, (i) => {
      const { op, v, bytes } = rows[i]!;
      const sb = new SizeBuf(4096);
      switch (op) {
        case 'coord':
          M.MSG_WriteCoord(sb, v as number);
          break;
        case 'angle':
          M.MSG_WriteAngle(sb, v as number);
          break;
        case 'angle16':
          M.MSG_WriteAngle16(sb, v as number);
          break;
        case 'pos':
          M.MSG_WritePos(sb, v as number[]);
          break;
        case 'dir':
          M.MSG_WriteDir(sb, v === null ? null : new Float32Array(v as number[]));
          break;
        case 'float':
          M.MSG_WriteFloat(sb, v as number);
          break;
        case 'string':
          M.MSG_WriteString(sb, v as string);
          break;
        case 'long':
          M.MSG_WriteLong(sb, v as number);
          break;
        case 'short':
          M.MSG_WriteShort(sb, v as number);
          break;
        case 'char':
          M.MSG_WriteChar(sb, v as number);
          break;
        case 'byte':
          M.MSG_WriteByte(sb, v as number);
          break;
        default:
          return `unknown op ${op}`;
      }
      const got = bytesToHex(sb.written());
      if (got !== bytes) return `${op}(${JSON.stringify(v)}) got ${got} want ${bytes}`;
      if (op === 'string') {
        const r = reader(hexToBytes(bytes));
        const s = M.MSG_ReadString(r);
        if (bytesToHex(latin1ToBytes(s)) + '00' !== bytes) return `string readback ${JSON.stringify(s)}`;
      }
      return null;
    });
  });
});

describe.skipIf(!has('crc.jsonl'))('golden core/msg/crc', () => {
  it('CRC_Block and COM_BlockSequenceCRCByte', () => {
    const rows = load<{ base?: string; sequence?: number; crc?: number; block?: string; crc16?: number }>(
      'crc.jsonl',
    );
    firstMismatch('crc.jsonl', rows.length, (i) => {
      const row = rows[i]!;
      if (row.base !== undefined) {
        const b = hexToBytes(row.base);
        const got = COM_BlockSequenceCRCByte(b, b.length, row.sequence!);
        return got === row.crc ? null : `seqcrc(${row.base}, ${row.sequence}) got ${got} want ${row.crc}`;
      }
      const b = hexToBytes(row.block!);
      const got = CRC_Block(b, b.length);
      return got === row.crc16 ? null : `CRC_Block(${row.block}) got ${got} want ${row.crc16}`;
    });
  });
});

describe.skipIf(!has('md4.jsonl'))('golden core/msg/md4', () => {
  it('Com_BlockChecksum', () => {
    const rows = load<{ block: string; checksum: number }>('md4.jsonl');
    firstMismatch('md4.jsonl', rows.length, (i) => {
      const row = rows[i]!;
      const got = Com_BlockChecksum(hexToBytes(row.block));
      return got === row.checksum ? null : `len ${row.block.length / 2} got ${got} want ${row.checksum}`;
    });
  });
});
