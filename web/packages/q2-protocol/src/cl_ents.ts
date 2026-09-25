// Pure ports of the message-parsing parts of client/cl_ents.c.
import {
  MAX_STATS,
  PM_FREEZE,
  PS_BLEND,
  PS_FOV,
  PS_KICKANGLES,
  PS_M_DELTA_ANGLES,
  PS_M_FLAGS,
  PS_M_GRAVITY,
  PS_M_ORIGIN,
  PS_M_TIME,
  PS_M_TYPE,
  PS_M_VELOCITY,
  PS_RDFLAGS,
  PS_VIEWANGLES,
  PS_VIEWOFFSET,
  PS_WEAPONFRAME,
  PS_WEAPONINDEX,
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
  cShort,
  type EntityState,
  type PlayerState,
} from 'q2-shared';
import {
  MSG_ReadAngle,
  MSG_ReadAngle16,
  MSG_ReadByte,
  MSG_ReadChar,
  MSG_ReadCoord,
  MSG_ReadLong,
  MSG_ReadPos,
  MSG_ReadShort,
} from './msg';
import type { SizeBuf } from './sizebuf';

export interface EntityBits {
  number: number;
  /** unsigned header bits */
  bits: number;
}

// C: cl_ents.c:202 CL_ParseEntityBits -- returns the entity number and the header bits
export function parseEntityBits(msg: SizeBuf, out: EntityBits = { number: 0, bits: 0 }): EntityBits {
  let total = MSG_ReadByte(msg) >>> 0;
  if (total & U_MOREBITS1) total = (total | ((MSG_ReadByte(msg) >>> 0) << 8)) >>> 0;
  if (total & U_MOREBITS2) total = (total | ((MSG_ReadByte(msg) >>> 0) << 16)) >>> 0;
  if (total & U_MOREBITS3) total = (total | ((MSG_ReadByte(msg) >>> 0) << 24)) >>> 0;
  out.number = total & U_NUMBER16 ? MSG_ReadShort(msg) : MSG_ReadByte(msg);
  out.bits = total;
  return out;
}

// C: cl_ents.c:247 CL_ParseDelta -- can go from either a baseline or a previous packet_entity
export function parseDelta(
  from: EntityState,
  to: EntityState,
  number: number,
  bits: number,
  msg: SizeBuf,
): void {
  // set everything to the state we are delta'ing from
  to.copyFrom(from);
  to.old_origin.set(from.origin);
  to.number = number;

  if (bits & U_MODEL) to.modelindex = MSG_ReadByte(msg);
  if (bits & U_MODEL2) to.modelindex2 = MSG_ReadByte(msg);
  if (bits & U_MODEL3) to.modelindex3 = MSG_ReadByte(msg);
  if (bits & U_MODEL4) to.modelindex4 = MSG_ReadByte(msg);

  if (bits & U_FRAME8) to.frame = MSG_ReadByte(msg);
  if (bits & U_FRAME16) to.frame = MSG_ReadShort(msg);

  if (bits & U_SKIN8 && bits & U_SKIN16)
    // used for laser colors
    to.skinnum = MSG_ReadLong(msg);
  else if (bits & U_SKIN8) to.skinnum = MSG_ReadByte(msg);
  else if (bits & U_SKIN16) to.skinnum = MSG_ReadShort(msg);

  // effects is unsigned int in C
  if ((bits & (U_EFFECTS8 | U_EFFECTS16)) === (U_EFFECTS8 | U_EFFECTS16))
    to.effects = MSG_ReadLong(msg) >>> 0;
  else if (bits & U_EFFECTS8) to.effects = MSG_ReadByte(msg) >>> 0;
  else if (bits & U_EFFECTS16) to.effects = MSG_ReadShort(msg) >>> 0;

  if ((bits & (U_RENDERFX8 | U_RENDERFX16)) === (U_RENDERFX8 | U_RENDERFX16)) to.renderfx = MSG_ReadLong(msg);
  else if (bits & U_RENDERFX8) to.renderfx = MSG_ReadByte(msg);
  else if (bits & U_RENDERFX16) to.renderfx = MSG_ReadShort(msg);

  if (bits & U_ORIGIN1) to.origin[0] = MSG_ReadCoord(msg);
  if (bits & U_ORIGIN2) to.origin[1] = MSG_ReadCoord(msg);
  if (bits & U_ORIGIN3) to.origin[2] = MSG_ReadCoord(msg);

  if (bits & U_ANGLE1) to.angles[0] = MSG_ReadAngle(msg);
  if (bits & U_ANGLE2) to.angles[1] = MSG_ReadAngle(msg);
  if (bits & U_ANGLE3) to.angles[2] = MSG_ReadAngle(msg);

  if (bits & U_OLDORIGIN) MSG_ReadPos(msg, to.old_origin);

  if (bits & U_SOUND) to.sound = MSG_ReadByte(msg);

  if (bits & U_EVENT) to.event = MSG_ReadByte(msg);
  else to.event = 0;

  if (bits & U_SOLID) to.solid = MSG_ReadShort(msg);
}

// C: cl_ents.c:515 CL_ParsePlayerstate -- `from` is the old frame's playerstate or null.
// `attractloop` (demo playback) forces pm_type = PM_FREEZE like the original.
export function parsePlayerstate(
  from: PlayerState | null,
  state: PlayerState,
  msg: SizeBuf,
  attractloop = false,
): void {
  // clear to old value before delta parsing
  if (from) state.copyFrom(from);
  else state.clear();

  const flags = MSG_ReadShort(msg);

  //
  // parse the pmove_state_t
  //
  if (flags & PS_M_TYPE) state.pmove.pm_type = MSG_ReadByte(msg);

  if (flags & PS_M_ORIGIN) {
    state.pmove.origin[0] = MSG_ReadShort(msg);
    state.pmove.origin[1] = MSG_ReadShort(msg);
    state.pmove.origin[2] = MSG_ReadShort(msg);
  }

  if (flags & PS_M_VELOCITY) {
    state.pmove.velocity[0] = MSG_ReadShort(msg);
    state.pmove.velocity[1] = MSG_ReadShort(msg);
    state.pmove.velocity[2] = MSG_ReadShort(msg);
  }

  if (flags & PS_M_TIME) state.pmove.pm_time = MSG_ReadByte(msg) & 255;
  if (flags & PS_M_FLAGS) state.pmove.pm_flags = MSG_ReadByte(msg) & 255;
  if (flags & PS_M_GRAVITY) state.pmove.gravity = cShort(MSG_ReadShort(msg));

  if (flags & PS_M_DELTA_ANGLES) {
    state.pmove.delta_angles[0] = MSG_ReadShort(msg);
    state.pmove.delta_angles[1] = MSG_ReadShort(msg);
    state.pmove.delta_angles[2] = MSG_ReadShort(msg);
  }

  if (attractloop) state.pmove.pm_type = PM_FREEZE; // demo playback

  //
  // parse the rest of the player_state_t
  //
  if (flags & PS_VIEWOFFSET) {
    state.viewoffset[0] = MSG_ReadChar(msg) * 0.25;
    state.viewoffset[1] = MSG_ReadChar(msg) * 0.25;
    state.viewoffset[2] = MSG_ReadChar(msg) * 0.25;
  }

  if (flags & PS_VIEWANGLES) {
    state.viewangles[0] = MSG_ReadAngle16(msg);
    state.viewangles[1] = MSG_ReadAngle16(msg);
    state.viewangles[2] = MSG_ReadAngle16(msg);
  }

  if (flags & PS_KICKANGLES) {
    state.kick_angles[0] = MSG_ReadChar(msg) * 0.25;
    state.kick_angles[1] = MSG_ReadChar(msg) * 0.25;
    state.kick_angles[2] = MSG_ReadChar(msg) * 0.25;
  }

  if (flags & PS_WEAPONINDEX) state.gunindex = MSG_ReadByte(msg);

  if (flags & PS_WEAPONFRAME) {
    state.gunframe = MSG_ReadByte(msg);
    state.gunoffset[0] = MSG_ReadChar(msg) * 0.25;
    state.gunoffset[1] = MSG_ReadChar(msg) * 0.25;
    state.gunoffset[2] = MSG_ReadChar(msg) * 0.25;
    state.gunangles[0] = MSG_ReadChar(msg) * 0.25;
    state.gunangles[1] = MSG_ReadChar(msg) * 0.25;
    state.gunangles[2] = MSG_ReadChar(msg) * 0.25;
  }

  if (flags & PS_BLEND) {
    state.blend[0] = MSG_ReadByte(msg) / 255.0;
    state.blend[1] = MSG_ReadByte(msg) / 255.0;
    state.blend[2] = MSG_ReadByte(msg) / 255.0;
    state.blend[3] = MSG_ReadByte(msg) / 255.0;
  }

  if (flags & PS_FOV) state.fov = MSG_ReadByte(msg);
  if (flags & PS_RDFLAGS) state.rdflags = MSG_ReadByte(msg);

  // parse stats
  const statbits = MSG_ReadLong(msg);
  for (let i = 0; i < MAX_STATS; i++) if (statbits & (1 << i)) state.stats[i] = MSG_ReadShort(msg);
}
