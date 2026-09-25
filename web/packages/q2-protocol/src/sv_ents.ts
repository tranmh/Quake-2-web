// Port of the body of server/sv_ents.c SV_WritePlayerstateToClient (pure form).
import {
  MAX_STATS,
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
  PlayerState,
  cInt,
  fr,
  svc_playerinfo,
} from 'q2-shared';
import { MSG_WriteAngle16, MSG_WriteByte, MSG_WriteChar, MSG_WriteLong, MSG_WriteShort } from './msg';
import type { SizeBuf } from './sizebuf';

const dummy = new PlayerState();

function v3ne(a: Float32Array, b: Float32Array): boolean {
  return a[0] !== b[0] || a[1] !== b[1] || a[2] !== b[2];
}

/** C float-to-int conversion of `f*k` (float arithmetic) as passed to an `int` parameter. */
function fi(f: number, k: number): number {
  return cInt(fr(fr(f) * k));
}

// C: sv_ents.c:220 SV_WritePlayerstateToClient
export function writePlayerstateToClient(from: PlayerState | null, ps: PlayerState, msg: SizeBuf): void {
  const ops = from ?? dummy.clear();

  //
  // determine what needs to be sent
  //
  let pflags = 0;
  const pm = ps.pmove,
    opm = ops.pmove;

  if (pm.pm_type !== opm.pm_type) pflags |= PS_M_TYPE;
  if (pm.origin[0] !== opm.origin[0] || pm.origin[1] !== opm.origin[1] || pm.origin[2] !== opm.origin[2])
    pflags |= PS_M_ORIGIN;
  if (
    pm.velocity[0] !== opm.velocity[0] ||
    pm.velocity[1] !== opm.velocity[1] ||
    pm.velocity[2] !== opm.velocity[2]
  )
    pflags |= PS_M_VELOCITY;
  if (pm.pm_time !== opm.pm_time) pflags |= PS_M_TIME;
  if (pm.pm_flags !== opm.pm_flags) pflags |= PS_M_FLAGS;
  if (pm.gravity !== opm.gravity) pflags |= PS_M_GRAVITY;
  if (
    pm.delta_angles[0] !== opm.delta_angles[0] ||
    pm.delta_angles[1] !== opm.delta_angles[1] ||
    pm.delta_angles[2] !== opm.delta_angles[2]
  )
    pflags |= PS_M_DELTA_ANGLES;

  if (v3ne(ps.viewoffset, ops.viewoffset)) pflags |= PS_VIEWOFFSET;
  if (v3ne(ps.viewangles, ops.viewangles)) pflags |= PS_VIEWANGLES;
  if (v3ne(ps.kick_angles, ops.kick_angles)) pflags |= PS_KICKANGLES;
  if (
    ps.blend[0] !== ops.blend[0] ||
    ps.blend[1] !== ops.blend[1] ||
    ps.blend[2] !== ops.blend[2] ||
    ps.blend[3] !== ops.blend[3]
  )
    pflags |= PS_BLEND;
  if (ps.fov !== ops.fov) pflags |= PS_FOV;
  if (ps.rdflags !== ops.rdflags) pflags |= PS_RDFLAGS;
  if (ps.gunframe !== ops.gunframe) pflags |= PS_WEAPONFRAME;

  pflags |= PS_WEAPONINDEX;

  //
  // write it
  //
  MSG_WriteByte(msg, svc_playerinfo);
  MSG_WriteShort(msg, pflags);

  //
  // write the pmove_state_t
  //
  if (pflags & PS_M_TYPE) MSG_WriteByte(msg, pm.pm_type);

  if (pflags & PS_M_ORIGIN) {
    MSG_WriteShort(msg, pm.origin[0]!);
    MSG_WriteShort(msg, pm.origin[1]!);
    MSG_WriteShort(msg, pm.origin[2]!);
  }

  if (pflags & PS_M_VELOCITY) {
    MSG_WriteShort(msg, pm.velocity[0]!);
    MSG_WriteShort(msg, pm.velocity[1]!);
    MSG_WriteShort(msg, pm.velocity[2]!);
  }

  if (pflags & PS_M_TIME) MSG_WriteByte(msg, pm.pm_time);
  if (pflags & PS_M_FLAGS) MSG_WriteByte(msg, pm.pm_flags);
  if (pflags & PS_M_GRAVITY) MSG_WriteShort(msg, pm.gravity);

  if (pflags & PS_M_DELTA_ANGLES) {
    MSG_WriteShort(msg, pm.delta_angles[0]!);
    MSG_WriteShort(msg, pm.delta_angles[1]!);
    MSG_WriteShort(msg, pm.delta_angles[2]!);
  }

  //
  // write the rest of the player_state_t
  //
  if (pflags & PS_VIEWOFFSET) {
    MSG_WriteChar(msg, fi(ps.viewoffset[0]!, 4));
    MSG_WriteChar(msg, fi(ps.viewoffset[1]!, 4));
    MSG_WriteChar(msg, fi(ps.viewoffset[2]!, 4));
  }

  if (pflags & PS_VIEWANGLES) {
    MSG_WriteAngle16(msg, ps.viewangles[0]!);
    MSG_WriteAngle16(msg, ps.viewangles[1]!);
    MSG_WriteAngle16(msg, ps.viewangles[2]!);
  }

  if (pflags & PS_KICKANGLES) {
    MSG_WriteChar(msg, fi(ps.kick_angles[0]!, 4));
    MSG_WriteChar(msg, fi(ps.kick_angles[1]!, 4));
    MSG_WriteChar(msg, fi(ps.kick_angles[2]!, 4));
  }

  if (pflags & PS_WEAPONINDEX) MSG_WriteByte(msg, ps.gunindex);

  if (pflags & PS_WEAPONFRAME) {
    MSG_WriteByte(msg, ps.gunframe);
    MSG_WriteChar(msg, fi(ps.gunoffset[0]!, 4));
    MSG_WriteChar(msg, fi(ps.gunoffset[1]!, 4));
    MSG_WriteChar(msg, fi(ps.gunoffset[2]!, 4));
    MSG_WriteChar(msg, fi(ps.gunangles[0]!, 4));
    MSG_WriteChar(msg, fi(ps.gunangles[1]!, 4));
    MSG_WriteChar(msg, fi(ps.gunangles[2]!, 4));
  }

  if (pflags & PS_BLEND) {
    MSG_WriteByte(msg, fi(ps.blend[0]!, 255));
    MSG_WriteByte(msg, fi(ps.blend[1]!, 255));
    MSG_WriteByte(msg, fi(ps.blend[2]!, 255));
    MSG_WriteByte(msg, fi(ps.blend[3]!, 255));
  }
  if (pflags & PS_FOV) MSG_WriteByte(msg, cInt(ps.fov));
  if (pflags & PS_RDFLAGS) MSG_WriteByte(msg, ps.rdflags);

  // send stats
  let statbits = 0;
  for (let i = 0; i < MAX_STATS; i++) if (ps.stats[i] !== ops.stats[i]) statbits |= 1 << i;
  MSG_WriteLong(msg, statbits);
  for (let i = 0; i < MAX_STATS; i++) if (statbits & (1 << i)) MSG_WriteShort(msg, ps.stats[i]!);
}
