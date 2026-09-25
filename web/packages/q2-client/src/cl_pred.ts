// Port of client/cl_pred.c -- client-side movement prediction with q2-pmove (bit exact with the server).
import {
  CMD_BACKUP,
  CS_AIRACCEL,
  MASK_PLAYERSOLID,
  MAX_PARSE_ENTITIES,
  PMF_NO_PREDICTION,
  PMF_ON_GROUND,
  SHORT2ANGLE,
  Trace,
  atof,
  cInt,
  fr,
  vec3_origin,
  type Vec3,
} from 'q2-shared';
import { PmoveT } from 'q2-pmove';
import { ca_active, Com_Printf, type ClientContext } from './client';

/** Opaque trace.ent value for the world (C: (struct edict_s *)1). */
export const PRED_WORLD_ENT = 1;

/** Per-context scratch for prediction (C: locals of cl_pred.c, preallocated). */
export class PredState {
  /** pmove_t pm (memset each CL_PredictMovement) -- created on first use (needs the context) */
  pm: PmoveT | null = null;
  /** result of CL_PMTrace (copied by pmove immediately) */
  readonly tr = new Trace();
  /** scratch trace for CL_ClipMoveToEntities */
  readonly trace = new Trace();
  readonly bmins = new Float32Array(3);
  readonly bmaxs = new Float32Array(3);
  /** last number of commands run by CL_PredictMovement (diagnostics) */
  lastFrames = 0;
}

function predState(c: ClientContext): PredState {
  return c.pred;
}

// C: cl_pred.c:29 CL_CheckPredictionError
export function CL_CheckPredictionError(c: ClientContext): void {
  const cl = c.cl;
  if (!c.cv.cl_predict.value || cl.frame.playerstate.pmove.pm_flags & PMF_NO_PREDICTION) return;

  // calculate the last usercmd_t we sent that the server has processed
  let frame = c.cls.netchan.incoming_acknowledged;
  frame &= CMD_BACKUP - 1;

  // compare what the server returned with what we had predicted it to be
  const origin = cl.frame.playerstate.pmove.origin;
  const po = cl.predicted_origins;
  const d0 = origin[0]! - po[frame * 3]!;
  const d1 = origin[1]! - po[frame * 3 + 1]!;
  const d2 = origin[2]! - po[frame * 3 + 2]!;

  // save the prediction error for interpolation
  const len = Math.abs(d0) + Math.abs(d1) + Math.abs(d2);
  if (len > 640) {
    // 80 world units
    // a teleport or something
    cl.prediction_error.fill(0);
  } else {
    if (c.cv.cl_showmiss.value && (d0 || d1 || d2))
      Com_Printf(c, 'prediction miss on %i: %i\n', cl.frame.serverframe, d0 + d1 + d2);

    po[frame * 3] = origin[0]!;
    po[frame * 3 + 1] = origin[1]!;
    po[frame * 3 + 2] = origin[2]!;

    // save for error itnerpolation
    cl.prediction_error[0] = d0 * 0.125;
    cl.prediction_error[1] = d1 * 0.125;
    cl.prediction_error[2] = d2 * 0.125;
  }
}

// C: cl_pred.c:73 CL_ClipMoveToEntities
export function CL_ClipMoveToEntities(
  c: ClientContext,
  start: Vec3,
  mins: Vec3,
  maxs: Vec3,
  end: Vec3,
  tr: Trace,
): void {
  const cl = c.cl;
  const cm = c.cm;
  if (!cm) return;
  const ps = predState(c);
  const bmins = ps.bmins;
  const bmaxs = ps.bmaxs;

  for (let i = 0; i < cl.frame.num_entities; i++) {
    const num = (cl.frame.parse_entities + i) & (MAX_PARSE_ENTITIES - 1);
    const ent = c.cl_parse_entities[num]!;

    if (!ent.solid) continue;

    if (ent.number === cl.playernum + 1) continue;

    let headnode: number;
    let angles: ArrayLike<number>;
    if (ent.solid === 31) {
      // special value for bmodel
      const cmodel = cl.model_clip[ent.modelindex];
      if (!cmodel) continue;
      headnode = cmodel.headnode;
      angles = ent.angles;
    } else {
      // encoded bbox
      const x = 8 * (ent.solid & 31);
      const zd = 8 * ((ent.solid >> 5) & 31);
      const zu = 8 * ((ent.solid >> 10) & 63) - 32;

      bmins[0] = bmins[1] = -x;
      bmaxs[0] = bmaxs[1] = x;
      bmins[2] = -zd;
      bmaxs[2] = zu;

      headnode = cm.headnodeForBox(bmins, bmaxs);
      angles = vec3_origin; // boxes don't rotate
    }

    if (tr.allsolid) return;

    const trace = cm.transformedBoxTrace(
      start,
      end,
      mins,
      maxs,
      headnode,
      MASK_PLAYERSOLID,
      ent.origin,
      angles,
      ps.trace,
    );

    if (trace.allsolid || trace.startsolid || trace.fraction < tr.fraction) {
      trace.ent = ent;
      if (tr.startsolid) {
        tr.copyFrom(trace);
        tr.startsolid = true;
      } else tr.copyFrom(trace);
      // (sic) unreachable in C too: startsolid is already covered by the first condition
      // eslint-disable-next-line no-dupe-else-if
    } else if (trace.startsolid) tr.startsolid = true;
  }
}

// C: cl_pred.c:148 CL_PMTrace -- the returned Trace is reused by the next call (pmove copies it)
export function CL_PMTrace(c: ClientContext, start: Vec3, mins: Vec3, maxs: Vec3, end: Vec3): Trace {
  const t = predState(c).tr;
  const cm = c.cm;

  // check against world
  if (cm) cm.boxTrace(start, end, mins, maxs, 0, MASK_PLAYERSOLID, t);
  else {
    // CM_BoxTrace with no map loaded: the default trace
    t.allsolid = false;
    t.startsolid = false;
    t.fraction = 1;
    t.endpos.fill(0);
    t.plane.clear();
    t.surface = null;
    t.contents = 0;
    t.ent = null;
  }
  if (t.fraction < 1.0) t.ent = PRED_WORLD_ENT;

  // check all other solid models
  CL_ClipMoveToEntities(c, start, mins, maxs, end, t);

  return t;
}

// C: cl_pred.c:163 CL_PMpointcontents
export function CL_PMpointcontents(c: ClientContext, point: Vec3): number {
  const cl = c.cl;
  const cm = c.cm;
  if (!cm) return 0;

  let contents = cm.pointContents(point, 0);

  for (let i = 0; i < cl.frame.num_entities; i++) {
    const num = (cl.frame.parse_entities + i) & (MAX_PARSE_ENTITIES - 1);
    const ent = c.cl_parse_entities[num]!;

    if (ent.solid !== 31) continue; // special value for bmodel

    const cmodel = cl.model_clip[ent.modelindex];
    if (!cmodel) continue;

    contents |= cm.transformedPointContents(point, cmodel.headnode, ent.origin, ent.angles);
  }

  return contents;
}

// C: cl_pred.c:199 CL_PredictMovement -- sets cl.predicted_origin and cl.predicted_angles
export function CL_PredictMovement(c: ClientContext): void {
  const cl = c.cl;
  const cls = c.cls;

  if (cls.state !== ca_active) return;

  if (c.cv.cl_paused.value) return;

  if (!c.cv.cl_predict.value || cl.frame.playerstate.pmove.pm_flags & PMF_NO_PREDICTION) {
    // just set angles
    for (let i = 0; i < 3; i++) {
      cl.predicted_angles[i] = cl.viewangles[i]! + SHORT2ANGLE(cl.frame.playerstate.pmove.delta_angles[i]!);
    }
    return;
  }

  let ack = cls.netchan.incoming_acknowledged;
  const current = cls.netchan.outgoing_sequence;

  // if we are too far out of date, just freeze
  if (current - ack >= CMD_BACKUP) {
    if (c.cv.cl_showmiss.value) Com_Printf(c, 'exceeded CMD_BACKUP\n');
    return;
  }

  // copy current state to pmove
  const ps = predState(c);
  let pm = ps.pm;
  if (!pm) {
    pm = ps.pm = new PmoveT(
      (start, mins, maxs, end) => CL_PMTrace(c, start, mins, maxs, end),
      (point) => CL_PMpointcontents(c, point),
    );
  }
  // memset (&pm, 0, sizeof(pm))
  pm.snapinitial = false;
  pm.cmd.clear();
  pm.mins.fill(0);
  pm.maxs.fill(0);

  c.pmove.pm_airaccelerate = fr(atof(cl.configstrings[CS_AIRACCEL]!));

  pm.s.copyFrom(cl.frame.playerstate.pmove);

  //	SCR_DebugGraph (current - ack - 1, 0);

  let frames = 0;
  // run frames
  while (++ack < current) {
    const frame = ack & (CMD_BACKUP - 1);
    const cmd = cl.cmds[frame]!;

    pm.cmd.copyFrom(cmd);
    c.pmove.run(pm);
    frames++;

    // save for debug checking
    cl.predicted_origins[frame * 3] = pm.s.origin[0]!;
    cl.predicted_origins[frame * 3 + 1] = pm.s.origin[1]!;
    cl.predicted_origins[frame * 3 + 2] = pm.s.origin[2]!;
  }
  ps.lastFrames = frames;

  const oldframe = (ack - 2) & (CMD_BACKUP - 1);
  const oldz = cl.predicted_origins[oldframe * 3 + 2]!;
  const step = pm.s.origin[2]! - oldz;
  if (step > 63 && step < 160 && pm.s.pm_flags & PMF_ON_GROUND) {
    cl.predicted_step = fr(step * 0.125);
    // unsigned = int - float*int (evaluated in float)
    cl.predicted_step_time = cInt(fr(cls.realtime - fr(cls.frametime * 500))) >>> 0;
  }

  // copy results out for rendering
  cl.predicted_origin[0] = pm.s.origin[0]! * 0.125;
  cl.predicted_origin[1] = pm.s.origin[1]! * 0.125;
  cl.predicted_origin[2] = pm.s.origin[2]! * 0.125;

  cl.predicted_angles.set(pm.viewangles);
}
