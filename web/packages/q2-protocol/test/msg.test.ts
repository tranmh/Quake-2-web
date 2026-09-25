import { describe, expect, it } from 'vitest';
import { BYTE_DIRS, UserCmd } from 'q2-shared';
import { bytesToHex } from 'q2-shared/testing';
import * as M from '../src/msg';
import { ProtocolError, SZ_Clear, SZ_Init, SZ_Print, SZ_Write, SizeBuf } from '../src/sizebuf';

function reader(sb: SizeBuf): SizeBuf {
  const r = new SizeBuf(sb.data.slice(0, sb.cursize));
  r.cursize = sb.cursize;
  return r;
}

describe('SizeBuf', () => {
  it('overflow without allowoverflow throws', () => {
    const sb = new SizeBuf(4);
    M.MSG_WriteLong(sb, 1);
    expect(() => M.MSG_WriteByte(sb, 1)).toThrow(ProtocolError);
  });
  it('overflow with allowoverflow clears and flags', () => {
    const sb = new SizeBuf(4);
    sb.allowoverflow = true;
    const msgs: string[] = [];
    sb.onPrint = (m) => msgs.push(m);
    M.MSG_WriteShort(sb, 1);
    M.MSG_WriteShort(sb, 2);
    M.MSG_WriteByte(sb, 9);
    expect(sb.overflowed).toBe(true);
    expect(sb.cursize).toBe(1);
    expect(sb.data[0]).toBe(9);
    expect(msgs).toEqual(['SZ_GetSpace: overflow\n']);
    expect(() => SZ_Write(sb, new Uint8Array(5))).toThrow(/full buffer size/);
    SZ_Clear(sb);
    expect(sb.overflowed).toBe(false);
  });
  it('SZ_Print overwrites trailing NUL', () => {
    const sb = new SizeBuf(32);
    SZ_Print(sb, 'ab');
    SZ_Print(sb, 'cd');
    expect(bytesToHex(sb.written())).toBe('6162636400');
    M.MSG_WriteByte(sb, 1);
    SZ_Print(sb, 'e');
    expect(bytesToHex(sb.written())).toBe('616263640001' + '6500');
  });
  it('SZ_Init resets', () => {
    const sb = new SizeBuf(4);
    sb.cursize = 3;
    SZ_Init(sb, new Uint8Array(8));
    expect(sb.maxsize).toBe(8);
    expect(sb.cursize).toBe(0);
  });
});

describe('MSG scalar', () => {
  it('known encodings', () => {
    const sb = new SizeBuf(64);
    M.MSG_WriteChar(sb, -1);
    M.MSG_WriteByte(sb, 256 + 7);
    M.MSG_WriteShort(sb, -2);
    M.MSG_WriteLong(sb, 0x12345678);
    M.MSG_WriteFloat(sb, 1.5);
    M.MSG_WriteCoord(sb, 1.9); // (int)(15.2) = 15
    M.MSG_WriteCoord(sb, -1.9); // -15
    M.MSG_WriteAngle(sb, 90); // 64
    M.MSG_WriteAngle(sb, -90); // (int)-64 & 255 = 192
    M.MSG_WriteAngle16(sb, 90); // 16384
    expect(bytesToHex(sb.written())).toBe('ff07feff785634120000c03f0f00f1ff40c00040');
  });
  it('round trips and read-past-end = -1', () => {
    const sb = new SizeBuf(64);
    M.MSG_WriteString(sb, 'hi\x81there');
    M.MSG_WriteString(sb, null);
    M.MSG_WriteString(sb, 'line1\nline2');
    M.MSG_WriteShort(sb, 40000);
    M.MSG_WriteLong(sb, -5);
    M.MSG_WriteFloat(sb, 0.1);
    M.MSG_WritePos(sb, [1, 2.5, -3.125]);
    const r = reader(sb);
    expect(M.MSG_ReadString(r)).toBe('hi\x81there');
    expect(M.MSG_ReadString(r)).toBe('');
    expect(M.MSG_ReadStringLine(r)).toBe('line1');
    expect(M.MSG_ReadString(r)).toBe('line2');
    expect(M.MSG_ReadShort(r)).toBe(40000 - 65536);
    expect(M.MSG_ReadLong(r)).toBe(-5);
    expect(M.MSG_ReadFloat(r)).toBe(Math.fround(0.1));
    const p = new Float32Array(3);
    M.MSG_ReadPos(r, p);
    expect(Array.from(p)).toEqual([1, 2.5, -3.125]);
    expect(M.MSG_ReadByte(r)).toBe(-1);
    expect(M.MSG_ReadShort(r)).toBe(-1);
    expect(M.MSG_ReadLong(r)).toBe(-1);
    expect(M.MSG_ReadFloat(r)).toBe(-1);
    expect(M.MSG_ReadString(r)).toBe('');
    expect(r.readcount).toBe(r.cursize + 1 + 2 + 4 + 4 + 1);
  });
  it('ReadString stops at a 0xFF byte (signed char -1)', () => {
    const sb = new SizeBuf(8);
    SZ_Write(sb, new Uint8Array([0x61, 0xff, 0x62, 0]));
    const r = reader(sb);
    expect(M.MSG_ReadString(r)).toBe('a');
    expect(M.MSG_ReadString(r)).toBe('b');
  });
  it('ReadAngle / ReadAngle16', () => {
    const sb = new SizeBuf(8);
    M.MSG_WriteAngle(sb, 45);
    M.MSG_WriteAngle16(sb, 45);
    const r = reader(sb);
    expect(M.MSG_ReadAngle(r)).toBe(45);
    expect(M.MSG_ReadAngle16(r)).toBe(45);
  });
  it('WriteDir picks exact bytedir and ReadDir restores it', () => {
    for (let i = 0; i < 162; i++) {
      const d = BYTE_DIRS.subarray(i * 3, i * 3 + 3);
      const sb = new SizeBuf(4);
      M.MSG_WriteDir(sb, d);
      expect(sb.data[0]).toBe(i);
      const out = new Float32Array(3);
      M.MSG_ReadDir(reader(sb), out);
      expect(Array.from(out)).toEqual(Array.from(d));
    }
    const sb = new SizeBuf(4);
    M.MSG_WriteDir(sb, null);
    M.MSG_WriteDir(sb, [0, 0, 0]);
    expect(Array.from(sb.written())).toEqual([0, 0]);
    const bad = new SizeBuf(new Uint8Array([200]));
    bad.cursize = 1;
    expect(() => M.MSG_ReadDir(bad, new Float32Array(3))).toThrow(ProtocolError);
  });
});

describe('usercmd delta', () => {
  it('round trips with minimal bits', () => {
    const from = new UserCmd();
    const to = new UserCmd();
    to.msec = 16;
    to.angles[1] = -1234;
    to.forwardmove = 400;
    to.upmove = -200;
    to.buttons = 1;
    to.lightlevel = 77;
    const sb = new SizeBuf(64);
    M.MSG_WriteDeltaUsercmd(sb, from, to);
    expect(bytesToHex(sb.written())).toBe(
      // bits = ANGLE2|FORWARD|UP|BUTTONS = 2|8|32|64 = 0x6a
      '6a' + '2efb' + '9001' + '38ff' + '01' + '10' + '4d',
    );
    const out = new UserCmd();
    M.MSG_ReadDeltaUsercmd(reader(sb), from, out);
    expect(out).toEqual(to);
  });
  it('identical commands write 3 bytes', () => {
    const a = new UserCmd();
    a.msec = 5;
    const sb = new SizeBuf(8);
    M.MSG_WriteDeltaUsercmd(sb, a, a);
    expect(bytesToHex(sb.written())).toBe('000500');
  });
});
