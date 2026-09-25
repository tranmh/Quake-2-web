// Port of qcommon/common.c MSG_* reading/writing functions.
import {
  ANGLE2SHORT,
  BYTE_DIRS,
  CM_ANGLE1,
  CM_ANGLE2,
  CM_ANGLE3,
  CM_BUTTONS,
  CM_FORWARD,
  CM_IMPULSE,
  CM_SIDE,
  CM_UP,
  MAX_EDICTS,
  NUMVERTEXNORMALS,
  RF_BEAM,
  U_ANGLE1,
  U_ANGLE2,
  U_ANGLE3,
  U_EFFECTS16,
  U_EFFECTS8,
  U_EVENT,
  U_FRAME16,
  U_FRAME8,
  U_MODEL,
  U_MODEL2,
  U_MODEL3,
  U_MODEL4,
  U_MOREBITS1,
  U_MOREBITS2,
  U_MOREBITS3,
  U_NUMBER16,
  U_OLDORIGIN,
  U_ORIGIN1,
  U_ORIGIN2,
  U_ORIGIN3,
  U_RENDERFX16,
  U_RENDERFX8,
  U_SKIN16,
  U_SKIN8,
  U_SOLID,
  U_SOUND,
  cInt,
  cShort,
  fr,
  latin1ToBytes,
  type EntityState,
  type UserCmd,
  type Vec3,
} from 'q2-shared';
import { ProtocolError, SZ_GetSpace, SZ_Write, type SizeBuf } from './sizebuf';

const f32 = new Float32Array(1);
const u8f = new Uint8Array(f32.buffer);

//
// writing functions
//

// C: common.c:280 MSG_WriteChar
export function MSG_WriteChar(sb: SizeBuf, c: number): void {
  sb.data[SZ_GetSpace(sb, 1)] = c & 255;
}

// C: common.c:293 MSG_WriteByte
export function MSG_WriteByte(sb: SizeBuf, c: number): void {
  sb.data[SZ_GetSpace(sb, 1)] = c & 255;
}

// C: common.c:306 MSG_WriteShort
export function MSG_WriteShort(sb: SizeBuf, c: number): void {
  const o = SZ_GetSpace(sb, 2);
  sb.data[o] = c & 0xff;
  sb.data[o + 1] = (c >> 8) & 0xff;
}

// C: common.c:320 MSG_WriteLong
export function MSG_WriteLong(sb: SizeBuf, c: number): void {
  const o = SZ_GetSpace(sb, 4);
  sb.data[o] = c & 0xff;
  sb.data[o + 1] = (c >> 8) & 0xff;
  sb.data[o + 2] = (c >> 16) & 0xff;
  sb.data[o + 3] = (c >> 24) & 0xff;
}

// C: common.c:331 MSG_WriteFloat
export function MSG_WriteFloat(sb: SizeBuf, f: number): void {
  f32[0] = f;
  SZ_Write(sb, u8f, 4);
}

// C: common.c:346 MSG_WriteString -- `s` is a byte string; null writes "".
export function MSG_WriteString(sb: SizeBuf, s: string | null): void {
  if (s === null) {
    SZ_Write(sb, new Uint8Array(1), 1);
    return;
  }
  const i = s.indexOf('\0');
  if (i >= 0) s = s.slice(0, i);
  const b = new Uint8Array(s.length + 1);
  b.set(latin1ToBytes(s));
  SZ_Write(sb, b, b.length);
}

// C: common.c:354 MSG_WriteCoord -- (int)(f*8): float multiply, then truncation
export function MSG_WriteCoord(sb: SizeBuf, f: number): void {
  MSG_WriteShort(sb, cInt(fr(fr(f) * 8)));
}

// C: common.c:359 MSG_WritePos
export function MSG_WritePos(sb: SizeBuf, pos: ArrayLike<number>): void {
  MSG_WriteShort(sb, cInt(fr(fr(pos[0]!) * 8)));
  MSG_WriteShort(sb, cInt(fr(fr(pos[1]!) * 8)));
  MSG_WriteShort(sb, cInt(fr(fr(pos[2]!) * 8)));
}

// C: common.c:366 MSG_WriteAngle -- (int)(f*256/360) & 255 in float arithmetic
export function MSG_WriteAngle(sb: SizeBuf, f: number): void {
  MSG_WriteByte(sb, cInt(fr(fr(fr(f) * 256) / 360)) & 255);
}

// C: common.c:371 MSG_WriteAngle16
export function MSG_WriteAngle16(sb: SizeBuf, f: number): void {
  MSG_WriteShort(sb, ANGLE2SHORT(f));
}

// C: common.c:377 MSG_WriteDeltaUsercmd
export function MSG_WriteDeltaUsercmd(buf: SizeBuf, from: UserCmd, cmd: UserCmd): void {
  let bits = 0;
  if (cmd.angles[0] !== from.angles[0]) bits |= CM_ANGLE1;
  if (cmd.angles[1] !== from.angles[1]) bits |= CM_ANGLE2;
  if (cmd.angles[2] !== from.angles[2]) bits |= CM_ANGLE3;
  if (cmd.forwardmove !== from.forwardmove) bits |= CM_FORWARD;
  if (cmd.sidemove !== from.sidemove) bits |= CM_SIDE;
  if (cmd.upmove !== from.upmove) bits |= CM_UP;
  if (cmd.buttons !== from.buttons) bits |= CM_BUTTONS;
  if (cmd.impulse !== from.impulse) bits |= CM_IMPULSE;

  MSG_WriteByte(buf, bits);

  if (bits & CM_ANGLE1) MSG_WriteShort(buf, cmd.angles[0]!);
  if (bits & CM_ANGLE2) MSG_WriteShort(buf, cmd.angles[1]!);
  if (bits & CM_ANGLE3) MSG_WriteShort(buf, cmd.angles[2]!);

  if (bits & CM_FORWARD) MSG_WriteShort(buf, cmd.forwardmove);
  if (bits & CM_SIDE) MSG_WriteShort(buf, cmd.sidemove);
  if (bits & CM_UP) MSG_WriteShort(buf, cmd.upmove);

  if (bits & CM_BUTTONS) MSG_WriteByte(buf, cmd.buttons);
  if (bits & CM_IMPULSE) MSG_WriteByte(buf, cmd.impulse);

  MSG_WriteByte(buf, cmd.msec);
  MSG_WriteByte(buf, cmd.lightlevel);
}

// C: common.c:432 MSG_WriteDir -- nearest of the 162 bytedirs by float DotProduct (first max wins)
export function MSG_WriteDir(sb: SizeBuf, dir: ArrayLike<number> | null): void {
  if (!dir) {
    MSG_WriteByte(sb, 0);
    return;
  }
  const d0 = fr(dir[0]!),
    d1 = fr(dir[1]!),
    d2 = fr(dir[2]!);
  let bestd = 0;
  let best = 0;
  for (let i = 0; i < NUMVERTEXNORMALS; i++) {
    const d = fr(
      fr(fr(d0 * BYTE_DIRS[i * 3]!) + fr(d1 * BYTE_DIRS[i * 3 + 1]!)) + fr(d2 * BYTE_DIRS[i * 3 + 2]!),
    );
    if (d > bestd) {
      bestd = d;
      best = i;
    }
  }
  MSG_WriteByte(sb, best);
}

// C: common.c:456 MSG_ReadDir
export function MSG_ReadDir(sb: SizeBuf, dir: Vec3): void {
  const b = MSG_ReadByte(sb);
  if (b >= NUMVERTEXNORMALS) throw new ProtocolError('MSF_ReadDir: out of range', 'drop');
  // b == -1 (read past end) indexes bytedirs[-1] in C (out of bounds); we read zeros instead.
  if (b < 0) {
    dir[0] = dir[1] = dir[2] = 0;
    return;
  }
  dir[0] = BYTE_DIRS[b * 3]!;
  dir[1] = BYTE_DIRS[b * 3 + 1]!;
  dir[2] = BYTE_DIRS[b * 3 + 2]!;
}

// C: common.c:475 MSG_WriteDeltaEntity
// Writes part of a packetentities message. Can delta from either a baseline or a previous packet_entity.
export function MSG_WriteDeltaEntity(
  from: EntityState,
  to: EntityState,
  msg: SizeBuf,
  force: boolean,
  newentity: boolean,
): void {
  if (!to.number) throw new ProtocolError('Unset entity number');
  if (to.number >= MAX_EDICTS) throw new ProtocolError('Entity number >= MAX_EDICTS');

  let bits = 0;

  if (to.number >= 256) bits |= U_NUMBER16; // number8 is implicit otherwise

  if (to.origin[0] !== from.origin[0]) bits |= U_ORIGIN1;
  if (to.origin[1] !== from.origin[1]) bits |= U_ORIGIN2;
  if (to.origin[2] !== from.origin[2]) bits |= U_ORIGIN3;

  if (to.angles[0] !== from.angles[0]) bits |= U_ANGLE1;
  if (to.angles[1] !== from.angles[1]) bits |= U_ANGLE2;
  if (to.angles[2] !== from.angles[2]) bits |= U_ANGLE3;

  if ((to.skinnum | 0) !== (from.skinnum | 0)) {
    const s = to.skinnum >>> 0;
    if (s < 256) bits |= U_SKIN8;
    else if (s < 0x10000) bits |= U_SKIN16;
    else bits |= U_SKIN8 | U_SKIN16;
  }

  if ((to.frame | 0) !== (from.frame | 0)) {
    if ((to.frame | 0) < 256) bits |= U_FRAME8;
    else bits |= U_FRAME16;
  }

  if (to.effects >>> 0 !== from.effects >>> 0) {
    const e = to.effects >>> 0; // unsigned int
    if (e < 256) bits |= U_EFFECTS8;
    else if (e < 0x8000) bits |= U_EFFECTS16;
    else bits |= U_EFFECTS8 | U_EFFECTS16;
  }

  if ((to.renderfx | 0) !== (from.renderfx | 0)) {
    const r = to.renderfx | 0; // signed int
    if (r < 256) bits |= U_RENDERFX8;
    else if (r < 0x8000) bits |= U_RENDERFX16;
    else bits |= U_RENDERFX8 | U_RENDERFX16;
  }

  if ((to.solid | 0) !== (from.solid | 0)) bits |= U_SOLID;

  // event is not delta compressed, just 0 compressed
  if (to.event) bits |= U_EVENT;

  if ((to.modelindex | 0) !== (from.modelindex | 0)) bits |= U_MODEL;
  if ((to.modelindex2 | 0) !== (from.modelindex2 | 0)) bits |= U_MODEL2;
  if ((to.modelindex3 | 0) !== (from.modelindex3 | 0)) bits |= U_MODEL3;
  if ((to.modelindex4 | 0) !== (from.modelindex4 | 0)) bits |= U_MODEL4;

  if ((to.sound | 0) !== (from.sound | 0)) bits |= U_SOUND;

  if (newentity || to.renderfx & RF_BEAM) bits |= U_OLDORIGIN;

  //
  // write the message
  //
  if (!bits && !force) return; // nothing to send!

  if (bits & 0xff000000) bits |= U_MOREBITS3 | U_MOREBITS2 | U_MOREBITS1;
  else if (bits & 0x00ff0000) bits |= U_MOREBITS2 | U_MOREBITS1;
  else if (bits & 0x0000ff00) bits |= U_MOREBITS1;

  MSG_WriteByte(msg, bits & 255);

  if (bits & 0xff000000) {
    MSG_WriteByte(msg, (bits >> 8) & 255);
    MSG_WriteByte(msg, (bits >> 16) & 255);
    MSG_WriteByte(msg, (bits >> 24) & 255);
  } else if (bits & 0x00ff0000) {
    MSG_WriteByte(msg, (bits >> 8) & 255);
    MSG_WriteByte(msg, (bits >> 16) & 255);
  } else if (bits & 0x0000ff00) {
    MSG_WriteByte(msg, (bits >> 8) & 255);
  }

  if (bits & U_NUMBER16) MSG_WriteShort(msg, to.number);
  else MSG_WriteByte(msg, to.number);

  if (bits & U_MODEL) MSG_WriteByte(msg, to.modelindex);
  if (bits & U_MODEL2) MSG_WriteByte(msg, to.modelindex2);
  if (bits & U_MODEL3) MSG_WriteByte(msg, to.modelindex3);
  if (bits & U_MODEL4) MSG_WriteByte(msg, to.modelindex4);

  if (bits & U_FRAME8) MSG_WriteByte(msg, to.frame);
  if (bits & U_FRAME16) MSG_WriteShort(msg, to.frame);

  if (bits & U_SKIN8 && bits & U_SKIN16)
    // used for laser colors
    MSG_WriteLong(msg, to.skinnum);
  else if (bits & U_SKIN8) MSG_WriteByte(msg, to.skinnum);
  else if (bits & U_SKIN16) MSG_WriteShort(msg, to.skinnum);

  if ((bits & (U_EFFECTS8 | U_EFFECTS16)) === (U_EFFECTS8 | U_EFFECTS16)) MSG_WriteLong(msg, to.effects);
  else if (bits & U_EFFECTS8) MSG_WriteByte(msg, to.effects);
  else if (bits & U_EFFECTS16) MSG_WriteShort(msg, to.effects);

  if ((bits & (U_RENDERFX8 | U_RENDERFX16)) === (U_RENDERFX8 | U_RENDERFX16)) MSG_WriteLong(msg, to.renderfx);
  else if (bits & U_RENDERFX8) MSG_WriteByte(msg, to.renderfx);
  else if (bits & U_RENDERFX16) MSG_WriteShort(msg, to.renderfx);

  if (bits & U_ORIGIN1) MSG_WriteCoord(msg, to.origin[0]!);
  if (bits & U_ORIGIN2) MSG_WriteCoord(msg, to.origin[1]!);
  if (bits & U_ORIGIN3) MSG_WriteCoord(msg, to.origin[2]!);

  if (bits & U_ANGLE1) MSG_WriteAngle(msg, to.angles[0]!);
  if (bits & U_ANGLE2) MSG_WriteAngle(msg, to.angles[1]!);
  if (bits & U_ANGLE3) MSG_WriteAngle(msg, to.angles[2]!);

  if (bits & U_OLDORIGIN) {
    MSG_WriteCoord(msg, to.old_origin[0]!);
    MSG_WriteCoord(msg, to.old_origin[1]!);
    MSG_WriteCoord(msg, to.old_origin[2]!);
  }

  if (bits & U_SOUND) MSG_WriteByte(msg, to.sound);
  if (bits & U_EVENT) MSG_WriteByte(msg, to.event);
  if (bits & U_SOLID) MSG_WriteShort(msg, to.solid);
}

//
// reading functions
//

// C: common.c:681 MSG_BeginReading
export function MSG_BeginReading(msg: SizeBuf): void {
  msg.readcount = 0;
}

// C: common.c:687 MSG_ReadChar -- returns -1 if no more characters are available
export function MSG_ReadChar(msg: SizeBuf): number {
  let c: number;
  if (msg.readcount + 1 > msg.cursize) c = -1;
  else c = (msg.data[msg.readcount]! << 24) >> 24;
  msg.readcount++;
  return c;
}

// C: common.c:700 MSG_ReadByte
export function MSG_ReadByte(msg: SizeBuf): number {
  let c: number;
  if (msg.readcount + 1 > msg.cursize) c = -1;
  else c = msg.data[msg.readcount]!;
  msg.readcount++;
  return c;
}

// C: common.c:713 MSG_ReadShort
export function MSG_ReadShort(msg: SizeBuf): number {
  let c: number;
  if (msg.readcount + 2 > msg.cursize) c = -1;
  else c = cShort(msg.data[msg.readcount]! + (msg.data[msg.readcount + 1]! << 8));
  msg.readcount += 2;
  return c;
}

// C: common.c:727 MSG_ReadLong
export function MSG_ReadLong(msg: SizeBuf): number {
  let c: number;
  if (msg.readcount + 4 > msg.cursize) c = -1;
  else {
    const d = msg.data,
      r = msg.readcount;
    c = d[r]! | (d[r + 1]! << 8) | (d[r + 2]! << 16) | (d[r + 3]! << 24);
  }
  msg.readcount += 4;
  return c;
}

// C: common.c:743 MSG_ReadFloat
export function MSG_ReadFloat(msg: SizeBuf): number {
  let v: number;
  if (msg.readcount + 4 > msg.cursize) v = -1;
  else {
    for (let i = 0; i < 4; i++) u8f[i] = msg.data[msg.readcount + i]!;
    v = f32[0]!;
  }
  msg.readcount += 4;
  return v;
}

// C: common.c:768 MSG_ReadString -- returns a byte string. Quirk: a 0xFF byte reads as char -1 and
// terminates the string like end-of-message. At most 2047 chars.
export function MSG_ReadString(msg: SizeBuf): string {
  let s = '';
  let l = 0;
  do {
    const c = MSG_ReadChar(msg);
    if (c === -1 || c === 0) break;
    s += String.fromCharCode(c & 255);
    l++;
  } while (l < 2047);
  return s;
}

// C: common.c:789 MSG_ReadStringLine
export function MSG_ReadStringLine(msg: SizeBuf): string {
  let s = '';
  let l = 0;
  do {
    const c = MSG_ReadChar(msg);
    if (c === -1 || c === 0 || c === 10) break;
    s += String.fromCharCode(c & 255);
    l++;
  } while (l < 2047);
  return s;
}

// C: common.c:810 MSG_ReadCoord
export function MSG_ReadCoord(msg: SizeBuf): number {
  return fr(MSG_ReadShort(msg) * (1.0 / 8));
}

// C: common.c:815 MSG_ReadPos
export function MSG_ReadPos(msg: SizeBuf, pos: Vec3): void {
  pos[0] = MSG_ReadShort(msg) * (1.0 / 8);
  pos[1] = MSG_ReadShort(msg) * (1.0 / 8);
  pos[2] = MSG_ReadShort(msg) * (1.0 / 8);
}

// C: common.c:822 MSG_ReadAngle
export function MSG_ReadAngle(msg: SizeBuf): number {
  return fr(MSG_ReadChar(msg) * (360.0 / 256));
}

// C: common.c:827 MSG_ReadAngle16
export function MSG_ReadAngle16(msg: SizeBuf): number {
  return fr(MSG_ReadShort(msg) * (360.0 / 65536));
}

// C: common.c:832 MSG_ReadDeltaUsercmd
export function MSG_ReadDeltaUsercmd(msg: SizeBuf, from: UserCmd, move: UserCmd): void {
  move.copyFrom(from);
  const bits = MSG_ReadByte(msg);

  if (bits & CM_ANGLE1) move.angles[0] = MSG_ReadShort(msg);
  if (bits & CM_ANGLE2) move.angles[1] = MSG_ReadShort(msg);
  if (bits & CM_ANGLE3) move.angles[2] = MSG_ReadShort(msg);

  if (bits & CM_FORWARD) move.forwardmove = cShort(MSG_ReadShort(msg));
  if (bits & CM_SIDE) move.sidemove = cShort(MSG_ReadShort(msg));
  if (bits & CM_UP) move.upmove = cShort(MSG_ReadShort(msg));

  if (bits & CM_BUTTONS) move.buttons = MSG_ReadByte(msg) & 255;
  if (bits & CM_IMPULSE) move.impulse = MSG_ReadByte(msg) & 255;

  move.msec = MSG_ReadByte(msg) & 255;
  move.lightlevel = MSG_ReadByte(msg) & 255;
}

// C: common.c:873 MSG_ReadData
export function MSG_ReadData(msg: SizeBuf, data: Uint8Array, len: number): void {
  for (let i = 0; i < len; i++) data[i] = MSG_ReadByte(msg);
}
