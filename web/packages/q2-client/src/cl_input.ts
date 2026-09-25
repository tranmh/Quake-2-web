// Port of client/cl_input.c -- builds an intended movement command to send to the server.
// IN_* mouse handling follows win32/in_win.c (IN_MouseMove / +mlook), fed by pointer-lock deltas.
import {
  BUTTON_ANY,
  BUTTON_ATTACK,
  BUTTON_USE,
  CMD_BACKUP,
  PITCH,
  YAW,
  ANGLE2SHORT,
  SHORT2ANGLE,
  UserCmd,
  atoi,
  cInt,
  cShort,
  clc_move,
  clc_userinfo,
  fr,
} from 'q2-shared';
import {
  COM_BlockSequenceCRCByte,
  MSG_WriteByte,
  MSG_WriteDeltaUsercmd,
  MSG_WriteLong,
  MSG_WriteString,
  SizeBuf,
} from 'q2-protocol';
import {
  ca_connected,
  ca_connecting,
  ca_disconnected,
  key_game,
  Com_Printf,
  type ClientContext,
} from './client';
import { SCR_FinishCinematic } from './cinematic';
import { CL_FixUpGender } from './cl_main';

/**
 * C: client.h kbutton_t.
 * state bit 0 is the current state of the key
 * state bit 1 is edge triggered on the up to down transition
 * state bit 2 is edge triggered on the down to up transition
 */
export class KButton {
  readonly down = new Int32Array(2); // key nums holding it down
  /** unsigned msec timestamp */
  downtime = 0;
  /** unsigned msec down this frame */
  msec = 0;
  state = 0;
}

export class InputState {
  readonly in_klook = new KButton();
  readonly in_left = new KButton();
  readonly in_right = new KButton();
  readonly in_forward = new KButton();
  readonly in_back = new KButton();
  readonly in_lookup = new KButton();
  readonly in_lookdown = new KButton();
  readonly in_moveleft = new KButton();
  readonly in_moveright = new KButton();
  readonly in_strafe = new KButton();
  readonly in_speed = new KButton();
  readonly in_use = new KButton();
  readonly in_attack = new KButton();
  readonly in_up = new KButton();
  readonly in_down = new KButton();
  in_impulse = 0;

  /** C: sys_frame_time (unsigned; set by Sys_SendKeyEvents) */
  sys_frame_time = 0;
  /** unsigned */
  frame_msec = 0;
  /** unsigned */
  old_sys_frame_time = 0;

  // in_win.c mouse state
  mlooking = false;
  /** pointer lock active (C: mouseactive) */
  mouseactive = false;
  mouse_x = 0;
  mouse_y = 0;
  old_mouse_x = 0;
  old_mouse_y = 0;
  /** accumulated pointer-lock deltas since the last IN_MouseMove (C: cursor offset from the window centre) */
  mx_accum = 0;
  my_accum = 0;

  readonly nullcmd = new UserCmd();
  readonly buf = new SizeBuf(128);
}

// C: cl_input.c:66 KeyDown
function KeyDown(c: ClientContext, b: KButton): void {
  const inp = c.input;
  let s = c.cmd.argv(1);
  let k: number;
  if (s[0]) k = atoi(s);
  else k = -1; // typed manually at the console for continuous down

  if (k === b.down[0] || k === b.down[1]) return; // repeating key

  if (!b.down[0]) b.down[0] = k;
  else if (!b.down[1]) b.down[1] = k;
  else {
    Com_Printf(c, 'Three keys down for a button!\n');
    return;
  }

  if (b.state & 1) return; // still down

  // save timestamp
  s = c.cmd.argv(2);
  b.downtime = atoi(s) >>> 0;
  if (!b.downtime) b.downtime = (inp.sys_frame_time - 100) >>> 0;

  b.state |= 1 + 2; // down + impulse down
}

// C: cl_input.c:102 KeyUp
function KeyUp(c: ClientContext, b: KButton): void {
  let s = c.cmd.argv(1);
  let k: number;
  if (s[0]) k = atoi(s);
  else {
    // typed manually at the console, assume for unsticking, so clear all
    b.down[0] = b.down[1] = 0;
    b.state = 4; // impulse up
    return;
  }

  if (b.down[0] === k) b.down[0] = 0;
  else if (b.down[1] === k) b.down[1] = 0;
  else return; // key up without coresponding down (menu pass through)
  if (b.down[0] || b.down[1]) return; // some other key is still holding it down

  if (!(b.state & 1)) return; // still up (this should not happen)

  // save timestamp
  s = c.cmd.argv(2);
  const uptime = atoi(s) >>> 0;
  if (uptime) b.msec = (b.msec + uptime - b.downtime) >>> 0;
  else b.msec = (b.msec + 10) >>> 0;

  b.state &= ~1; // now up
  b.state |= 4; // impulse up
}

// C: cl_input.c:185 CL_KeyState -- returns the fraction of the frame that the key was down
export function CL_KeyState(c: ClientContext, key: KButton): number {
  const inp = c.input;
  key.state &= 1; // clear impulses

  let msec = key.msec | 0;
  key.msec = 0;

  if (key.state) {
    // still down
    msec = (msec + ((inp.sys_frame_time - key.downtime) >>> 0)) | 0;
    key.downtime = inp.sys_frame_time;
  }

  let val = fr(fr(msec) / fr(inp.frame_msec));
  if (val < 0) val = 0;
  if (val > 1) val = 1;
  return val;
}

// C: cl_input.c:241 CL_AdjustAngles -- moves the local angle positions
export function CL_AdjustAngles(c: ClientContext): void {
  const inp = c.input;
  const cl = c.cl;
  const cv = c.cv;
  let speed: number;
  if (inp.in_speed.state & 1) speed = fr(c.cls.frametime * cv.cl_anglespeedkey.value);
  else speed = c.cls.frametime;

  const yawspeed = cv.cl_yawspeed.value;
  const pitchspeed = cv.cl_pitchspeed.value;
  if (!(inp.in_strafe.state & 1)) {
    cl.viewangles[YAW] = cl.viewangles[YAW]! - fr(fr(speed * yawspeed) * CL_KeyState(c, inp.in_right));
    cl.viewangles[YAW] = cl.viewangles[YAW]! + fr(fr(speed * yawspeed) * CL_KeyState(c, inp.in_left));
  }
  if (inp.in_klook.state & 1) {
    cl.viewangles[PITCH] =
      cl.viewangles[PITCH]! - fr(fr(speed * pitchspeed) * CL_KeyState(c, inp.in_forward));
    cl.viewangles[PITCH] = cl.viewangles[PITCH]! + fr(fr(speed * pitchspeed) * CL_KeyState(c, inp.in_back));
  }

  const up = CL_KeyState(c, inp.in_lookup);
  const down = CL_KeyState(c, inp.in_lookdown);

  cl.viewangles[PITCH] = cl.viewangles[PITCH]! - fr(fr(speed * pitchspeed) * up);
  cl.viewangles[PITCH] = cl.viewangles[PITCH]! + fr(fr(speed * pitchspeed) * down);
}

/** C `short += float`: (short)(int)((float)s + f) */
function addShort(s: number, f: number): number {
  return cShort(cInt(fr(s + f)));
}

// C: cl_input.c:276 CL_BaseMove -- send the intended movement message to the server
export function CL_BaseMove(c: ClientContext, cmd: UserCmd): void {
  const inp = c.input;
  const cv = c.cv;
  CL_AdjustAngles(c);

  cmd.clear();

  // VectorCopy (cl.viewangles, cmd->angles): float -> short conversion
  for (let i = 0; i < 3; i++) cmd.angles[i] = cShort(cInt(c.cl.viewangles[i]!));
  const side = cv.cl_sidespeed.value;
  if (inp.in_strafe.state & 1) {
    cmd.sidemove = addShort(cmd.sidemove, fr(side * CL_KeyState(c, inp.in_right)));
    cmd.sidemove = addShort(cmd.sidemove, -fr(side * CL_KeyState(c, inp.in_left)));
  }

  cmd.sidemove = addShort(cmd.sidemove, fr(side * CL_KeyState(c, inp.in_moveright)));
  cmd.sidemove = addShort(cmd.sidemove, -fr(side * CL_KeyState(c, inp.in_moveleft)));

  const upspeed = cv.cl_upspeed.value;
  cmd.upmove = addShort(cmd.upmove, fr(upspeed * CL_KeyState(c, inp.in_up)));
  cmd.upmove = addShort(cmd.upmove, -fr(upspeed * CL_KeyState(c, inp.in_down)));

  if (!(inp.in_klook.state & 1)) {
    const fwd = cv.cl_forwardspeed.value;
    cmd.forwardmove = addShort(cmd.forwardmove, fr(fwd * CL_KeyState(c, inp.in_forward)));
    cmd.forwardmove = addShort(cmd.forwardmove, -fr(fwd * CL_KeyState(c, inp.in_back)));
  }

  //
  // adjust for speed key / running
  //
  if ((inp.in_speed.state & 1) ^ cInt(cv.cl_run.value)) {
    cmd.forwardmove = cShort(cmd.forwardmove * 2);
    cmd.sidemove = cShort(cmd.sidemove * 2);
    cmd.upmove = cShort(cmd.upmove * 2);
  }
}

// C: cl_input.c:312 CL_ClampPitch
export function CL_ClampPitch(c: ClientContext): void {
  const cl = c.cl;
  let pitch = fr(SHORT2ANGLE(cl.frame.playerstate.pmove.delta_angles[PITCH]!));
  if (pitch > 180) pitch = fr(pitch - 360);

  if (fr(cl.viewangles[PITCH]! + pitch) > 89) cl.viewangles[PITCH] = 89 - pitch;
  if (fr(cl.viewangles[PITCH]! + pitch) < -89) cl.viewangles[PITCH] = -89 - pitch;
}

// C: cl_input.c:330 CL_FinishMove
export function CL_FinishMove(c: ClientContext, cmd: UserCmd): void {
  const inp = c.input;
  //
  // figure button bits
  //
  if (inp.in_attack.state & 3) cmd.buttons |= BUTTON_ATTACK;
  inp.in_attack.state &= ~2;

  if (inp.in_use.state & 3) cmd.buttons |= BUTTON_USE;
  inp.in_use.state &= ~2;

  if (c.keys.anykeydown && c.cls.key_dest === key_game) cmd.buttons |= BUTTON_ANY;

  // send milliseconds of time to apply the move
  let ms = cInt(fr(c.cls.frametime * 1000));
  if (ms > 250) ms = 100; // time was unreasonable
  cmd.msec = ms & 255;

  CL_ClampPitch(c);
  for (let i = 0; i < 3; i++) cmd.angles[i] = ANGLE2SHORT(c.cl.viewangles[i]!);

  cmd.impulse = inp.in_impulse & 255;
  inp.in_impulse = 0;

  // send the ambient light level at the player's current position
  cmd.lightlevel = cInt(c.cv.cl_lightlevel.value) & 255;
}

// C: win32/in_win.c:272 IN_MouseMove (pointer-lock deltas replace the cursor-to-centre offset)
export function IN_MouseMove(c: ClientContext, cmd: UserCmd): void {
  const inp = c.input;
  const cv = c.cv;
  const cl = c.cl;
  if (!inp.mouseactive) {
    inp.mx_accum = inp.my_accum = 0;
    return;
  }

  // find mouse movement
  const mx = inp.mx_accum | 0;
  const my = inp.my_accum | 0;
  inp.mx_accum -= mx;
  inp.my_accum -= my;

  if (cv.m_filter.value) {
    inp.mouse_x = cInt((mx + inp.old_mouse_x) * 0.5);
    inp.mouse_y = cInt((my + inp.old_mouse_y) * 0.5);
  } else {
    inp.mouse_x = mx;
    inp.mouse_y = my;
  }

  inp.old_mouse_x = mx;
  inp.old_mouse_y = my;

  inp.mouse_x = cInt(fr(fr(inp.mouse_x) * cv.sensitivity.value));
  inp.mouse_y = cInt(fr(fr(inp.mouse_y) * cv.sensitivity.value));

  // add mouse X/Y movement to cmd
  if (inp.in_strafe.state & 1 || (cv.lookstrafe.value && inp.mlooking))
    cmd.sidemove = addShort(cmd.sidemove, fr(cv.m_side.value * inp.mouse_x));
  else cl.viewangles[YAW] = cl.viewangles[YAW]! - fr(cv.m_yaw.value * inp.mouse_x);

  if ((inp.mlooking || cv.freelook.value) && !(inp.in_strafe.state & 1)) {
    cl.viewangles[PITCH] = cl.viewangles[PITCH]! + fr(cv.m_pitch.value * inp.mouse_y);
  } else {
    cmd.forwardmove = addShort(cmd.forwardmove, -fr(cv.m_forward.value * inp.mouse_y));
  }
}

// C: win32/in_win.c IN_Move
export function IN_Move(c: ClientContext, cmd: UserCmd): void {
  IN_MouseMove(c, cmd);
}

/** Browser entry point: pointer-lock movementX/movementY (accumulated until the next command). */
export function IN_MouseDelta(c: ClientContext, dx: number, dy: number): void {
  c.input.mx_accum += dx;
  c.input.my_accum += dy;
}

// C: cl_input.c:371 CL_CreateCmd
export function CL_CreateCmd(c: ClientContext, cmd: UserCmd): void {
  const inp = c.input;
  let fm = (inp.sys_frame_time - inp.old_sys_frame_time) | 0; // unsigned in C; negative wraps huge -> 200
  if (fm < 0) fm = 200;
  inp.frame_msec = fm;
  if (inp.frame_msec < 1) inp.frame_msec = 1;
  if (inp.frame_msec > 200) inp.frame_msec = 200;

  // get basic movement from keyboard
  CL_BaseMove(c, cmd);

  // allow mice or other external controllers to add to the move
  IN_Move(c, cmd);

  CL_FinishMove(c, cmd);

  inp.old_sys_frame_time = inp.sys_frame_time;
}

// C: cl_input.c:397 IN_CenterView
export function IN_CenterView(c: ClientContext): void {
  c.cl.viewangles[PITCH] = -SHORT2ANGLE(c.cl.frame.playerstate.pmove.delta_angles[PITCH]!);
}

// C: cl_input.c:407 CL_InitInput
export function CL_InitInput(c: ClientContext): void {
  const inp = c.input;
  const add = (name: string, fn: () => void) => c.cmd.addCommand(name, fn);
  add('centerview', () => IN_CenterView(c));

  const button = (name: string, b: KButton) => {
    add('+' + name, () => KeyDown(c, b));
    add('-' + name, () => KeyUp(c, b));
  };
  button('moveup', inp.in_up);
  button('movedown', inp.in_down);
  button('left', inp.in_left);
  button('right', inp.in_right);
  button('forward', inp.in_forward);
  button('back', inp.in_back);
  button('lookup', inp.in_lookup);
  button('lookdown', inp.in_lookdown);
  button('strafe', inp.in_strafe);
  button('moveleft', inp.in_moveleft);
  button('moveright', inp.in_moveright);
  button('speed', inp.in_speed);
  button('attack', inp.in_attack);
  button('use', inp.in_use);
  add('impulse', () => {
    inp.in_impulse = atoi(c.cmd.argv(1));
  });
  button('klook', inp.in_klook);

  c.cv.cl_nodelta = c.cvars.get('cl_nodelta', '0', 0);
}

// C: win32/in_win.c:342 IN_Init (mouse part)
export function IN_Init(c: ClientContext): void {
  // mouse variables
  c.cv.m_filter = c.cvars.get('m_filter', '0', 0);
  c.cv.in_mouse = c.cvars.get('in_mouse', '1', 1 /* CVAR_ARCHIVE */);
  c.cmd.addCommand('+mlook', () => {
    c.input.mlooking = true;
  });
  c.cmd.addCommand('-mlook', () => {
    c.input.mlooking = false;
    if (!c.cv.freelook.value && c.cv.lookspring.value) IN_CenterView(c);
  });
}

// C: cl_input.c:453 CL_SendCmd
export function CL_SendCmd(c: ClientContext): void {
  const cls = c.cls;
  const cl = c.cl;
  const inp = c.input;

  // build a command even if not connected

  // save this command off for prediction
  let i = cls.netchan.outgoing_sequence & (CMD_BACKUP - 1);
  let cmd = cl.cmds[i]!;
  cl.cmd_time[i] = cls.realtime; // for netgraph ping calculation

  CL_CreateCmd(c, cmd);

  cl.cmd.copyFrom(cmd);

  if (cls.state === ca_disconnected || cls.state === ca_connecting) return;

  if (c.main.demoplaying) return; // demo playback has no server to talk to

  if (cls.state === ca_connected) {
    if (cls.netchan.message.cursize || c.curtime - cls.netchan.last_sent > 1000) cls.netchan.transmit(0);
    return;
  }

  // send a userinfo update if needed
  if (c.cvars.userinfoModified) {
    CL_FixUpGender(c);
    c.cvars.userinfoModified = false;
    MSG_WriteByte(cls.netchan.message, clc_userinfo);
    MSG_WriteString(cls.netchan.message, c.cvars.userinfo());
  }

  const buf = inp.buf;
  buf.cursize = 0;
  buf.overflowed = false;

  if (cmd.buttons && cl.cinematictime > 0 && !cl.attractloop && cls.realtime - cl.cinematictime > 1000) {
    // skip the rest of the cinematic
    SCR_FinishCinematic(c);
  }

  // begin a client move command
  MSG_WriteByte(buf, clc_move);

  // save the position for a checksum byte
  const checksumIndex = buf.cursize;
  MSG_WriteByte(buf, 0);

  // let the server know what the last frame we
  // got was, so the next message can be delta compressed
  if (c.cv.cl_nodelta.value || !cl.frame.valid || cls.demowaiting)
    MSG_WriteLong(buf, -1); // no compression
  else MSG_WriteLong(buf, cl.frame.serverframe);

  // send this and the previous cmds in the message, so
  // if the last packet was dropped, it can be recovered
  i = (cls.netchan.outgoing_sequence - 2) & (CMD_BACKUP - 1);
  cmd = cl.cmds[i]!;
  inp.nullcmd.clear();
  MSG_WriteDeltaUsercmd(buf, inp.nullcmd, cmd);
  let oldcmd = cmd;

  i = (cls.netchan.outgoing_sequence - 1) & (CMD_BACKUP - 1);
  cmd = cl.cmds[i]!;
  MSG_WriteDeltaUsercmd(buf, oldcmd, cmd);
  oldcmd = cmd;

  i = cls.netchan.outgoing_sequence & (CMD_BACKUP - 1);
  cmd = cl.cmds[i]!;
  MSG_WriteDeltaUsercmd(buf, oldcmd, cmd);

  // calculate a checksum over the move commands
  buf.data[checksumIndex] = COM_BlockSequenceCRCByte(
    buf.data.subarray(checksumIndex + 1),
    buf.cursize - checksumIndex - 1,
    cls.netchan.outgoing_sequence,
  );

  //
  // deliver the message
  //
  cls.netchan.transmit(buf.cursize, buf.data);
}
