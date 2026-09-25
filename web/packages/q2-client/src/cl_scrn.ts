// Port of client/cl_scrn.c -- master for refresh, status bar, console, chat, notify, etc.
//
//   full screen console
//   put up loading plaque
//   blanked background with loading plaque
//   blanked background with menu
//   cinematics
//   full screen image for quit and victory
//   end of unit intermissions
//
// The layout language interpreter itself lives in layout.ts (pure, produces draw ops).
import {
  CMD_BACKUP,
  CS_MODELS,
  CS_STATUSBAR,
  CVAR_ARCHIVE,
  ERR_DROP,
  STAT_LAYOUTS,
  atof,
  cInt,
  fr,
} from 'q2-shared';
import {
  ca_active,
  ca_connecting,
  ca_disconnected,
  key_console,
  key_game,
  key_menu,
  key_message,
  Com_Error,
  Com_Printf,
  Sys_Milliseconds,
  type ClientContext,
} from './client';
import { Con_CheckResize, Con_ClearNotify, Con_DrawConsole, Con_DrawNotify } from './console';
import { SCR_DrawCinematic } from './cinematic';
import { M_Draw } from './menu';
import { V_RenderView } from './cl_view';
import { CL_DrawInventory } from './cl_inv';
import { RegisterPic } from './regcache';
import {
  CHAR_WIDTH,
  STAT_MINUS,
  SizeHUDString as SizeHUDStringPure,
  executeLayoutString,
  opDrawField,
  opDrawHUDString,
  sb_nums,
  type DrawOp,
  type LayoutEnv,
} from './layout';

export { sb_nums, STAT_MINUS, CHAR_WIDTH };

// C: cl_scrn.c:62 dirty_t
export interface Dirty {
  x1: number;
  y1: number;
  x2: number;
  y2: number;
}

// C: vid.h vrect_t
export interface VRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

// C: cl_scrn.c:103 graphsamp_t
interface GraphSamp {
  /** float */
  value: number;
  color: number;
}

/** C: cl_scrn.c file globals. */
export class ScreenState {
  /** float: aproaches scr_conlines at scr_conspeed */
  scr_con_current = 0;
  /** float: 0.0 to 1.0 lines of console to display */
  scr_conlines = 0;

  scr_initialized = false; // ready to draw

  scr_draw_loading = 0;

  readonly scr_vrect: VRect = { x: 0, y: 0, width: 0, height: 0 }; // position of render window on screen

  readonly scr_dirty: Dirty = { x1: 0, y1: 0, x2: 0, y2: 0 };
  readonly scr_old_dirty: Dirty[] = [
    { x1: 0, y1: 0, x2: 0, y2: 0 },
    { x1: 0, y1: 0, x2: 0, y2: 0 },
  ];

  crosshair_pic = '';
  crosshair_width = 0;
  crosshair_height = 0;

  // bar graphs
  current = 0;
  readonly values: GraphSamp[] = Array.from({ length: 1024 }, () => ({ value: 0, color: 0 }));

  // center printing
  scr_centerstring = '';
  /** float: for slow victory printing */
  scr_centertime_start = 0;
  /** float */
  scr_centertime_off = 0;
  scr_center_lines = 0;
  scr_erase_center = 0;
}

/*
===============================================================================

BAR GRAPHS

===============================================================================
*/

// C: cl_scrn.c:91 CL_AddNetgraph -- a new packet was just parsed
export function CL_AddNetgraph(c: ClientContext): void {
  const { cls, cl, cv } = c;
  // if using the debuggraph for something else, don't add the net lines
  if (cv.scr_debuggraph.value || cv.scr_timegraph.value) return;

  for (let i = 0; i < cls.netchan.dropped; i++) SCR_DebugGraph(c, 30, 0x40);

  for (let i = 0; i < cl.surpressCount; i++) SCR_DebugGraph(c, 30, 0xdf);

  // see what the latency was on this packet
  const inn = cls.netchan.incoming_acknowledged & (CMD_BACKUP - 1);
  let ping = cls.realtime - cl.cmd_time[inn]!;
  ping = Math.trunc(ping / 30);
  if (ping > 30) ping = 30;
  SCR_DebugGraph(c, ping, 0xd0);
}

// C: cl_scrn.c:132 SCR_DebugGraph
export function SCR_DebugGraph(c: ClientContext, value: number, color: number): void {
  const s = c.scr;
  const v = s.values[s.current & 1023]!;
  v.value = fr(value);
  v.color = color;
  s.current++;
}

// C: cl_scrn.c:144 SCR_DrawDebugGraph
export function SCR_DrawDebugGraph(c: ClientContext): void {
  const s = c.scr;
  const gh = c.cv.scr_graphheight.value;
  //
  // draw the graph
  //
  const w = s.scr_vrect.width;

  const x = s.scr_vrect.x;
  const y = s.scr_vrect.y + s.scr_vrect.height;
  c.re.drawFill(x, cInt(fr(y - gh)), w, cInt(gh), 8);

  for (let a = 0; a < w; a++) {
    const i = (s.current - 1 - a + 1024) & 1023;
    let v = s.values[i]!.value;
    const color = s.values[i]!.color;
    v = fr(fr(v * c.cv.scr_graphscale.value) + c.cv.scr_graphshift.value);

    if (v < 0) v = fr(v + fr(gh * (1 + cInt(fr(-v / gh)))));
    const div = cInt(gh);
    // C integer division by zero would trap; draw nothing instead
    const h = div ? cInt(v) % div : 0;
    c.re.drawFill(x + w - 1 - a, y - h, 1, h, color);
  }
}

/*
===============================================================================

CENTER PRINTING

===============================================================================
*/

const CENTER_RULE =
  '\n\n\x1d\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1f\n\n';

// C: cl_scrn.c:196 SCR_CenterPrint
// Called for important messages that should stay in the center of the screen for a few moments
export function SCR_CenterPrint(c: ClientContext, str: string): void {
  const s = c.scr;
  s.scr_centerstring = str.slice(0, 1023);
  s.scr_centertime_off = c.cv.scr_centertime.value;
  s.scr_centertime_start = fr(c.cl.time);

  // count the number of lines for centering
  s.scr_center_lines = 1;
  for (let i = 0; i < str.length; i++) if (str[i] === '\n') s.scr_center_lines++;

  // echo it to the console
  Com_Printf(c, '%s', CENTER_RULE);

  let p = 0;
  do {
    // scan the width of the line
    let l: number;
    for (l = 0; l < 40; l++) if (p + l >= str.length || str[p + l] === '\n') break;
    let line = ' '.repeat(Math.trunc((40 - l) / 2));
    line += str.slice(p, p + l);
    line += '\n';

    Com_Printf(c, '%s', line);

    while (p < str.length && str[p] !== '\n') p++;

    if (p >= str.length) break;
    p++; // skip the \n
  } while (1);
  Com_Printf(c, '%s', CENTER_RULE);
  Con_ClearNotify(c);
}

// C: cl_scrn.c:251 SCR_DrawCenterString
export function SCR_DrawCenterString(c: ClientContext): void {
  const s = c.scr;
  const str = s.scr_centerstring;
  // the finale prints the characters one at a time
  let remaining = 9999;

  s.scr_erase_center = 0;
  let start = 0;

  let y: number;
  if (s.scr_center_lines <= 4) y = cInt(c.viddef.height * 0.35);
  else y = 48;

  do {
    // scan the width of the line
    let l: number;
    for (l = 0; l < 40; l++) if (start + l >= str.length || str[start + l] === '\n') break;
    let x = Math.trunc((c.viddef.width - l * 8) / 2);
    SCR_AddDirtyPoint(c, x, y);
    for (let j = 0; j < l; j++, x += 8) {
      c.re.drawChar(x, y, str.charCodeAt(start + j) & 255);
      if (!remaining--) return;
    }
    SCR_AddDirtyPoint(c, x, y + 8);

    y += 8;

    while (start < str.length && str[start] !== '\n') start++;

    if (start >= str.length) break;
    start++; // skip the \n
  } while (1);
}

// C: cl_scrn.c:297 SCR_CheckDrawCenterString
export function SCR_CheckDrawCenterString(c: ClientContext): void {
  const s = c.scr;
  s.scr_centertime_off = fr(s.scr_centertime_off - c.cls.frametime);

  if (s.scr_centertime_off <= 0) return;

  SCR_DrawCenterString(c);
}

//=============================================================================

// C: cl_scrn.c:316 SCR_CalcVrect -- sets scr_vrect, the coordinates of the rendered window
export function SCR_CalcVrect(c: ClientContext): void {
  const vs = c.cv.scr_viewsize;
  const vr = c.scr.scr_vrect;
  // bound viewsize
  if (vs.value < 40) c.cvars.set('viewsize', '40');
  if (vs.value > 100) c.cvars.set('viewsize', '100');

  const size = cInt(vs.value);

  vr.width = Math.trunc((c.viddef.width * size) / 100);
  vr.width &= ~7;

  vr.height = Math.trunc((c.viddef.height * size) / 100);
  vr.height &= ~1;

  vr.x = Math.trunc((c.viddef.width - vr.width) / 2);
  vr.y = Math.trunc((c.viddef.height - vr.height) / 2);
}

// C: cl_scrn.c:346 SCR_SizeUp_f -- keybinding command
export function SCR_SizeUp_f(c: ClientContext): void {
  c.cvars.setValue('viewsize', fr(c.cv.scr_viewsize.value + 10));
}

// C: cl_scrn.c:359 SCR_SizeDown_f -- keybinding command
export function SCR_SizeDown_f(c: ClientContext): void {
  c.cvars.setValue('viewsize', fr(c.cv.scr_viewsize.value - 10));
}

// C: cl_scrn.c:371 SCR_Sky_f -- set a specific sky and rotation speed
export function SCR_Sky_f(c: ClientContext): void {
  const cmd = c.cmd;
  let rotate: number;
  const axis = new Float32Array(3);

  if (cmd.argc() < 2) {
    Com_Printf(c, 'Usage: sky <basename> <rotate> <axis x y z>\n');
    return;
  }
  if (cmd.argc() > 2) rotate = fr(atof(cmd.argv(2)));
  else rotate = 0;
  if (cmd.argc() === 6) {
    axis[0] = atof(cmd.argv(3));
    axis[1] = atof(cmd.argv(4));
    axis[2] = atof(cmd.argv(5));
  } else {
    axis[0] = 0;
    axis[1] = 0;
    axis[2] = 1;
  }

  void c.re.setSky(cmd.argv(1), rotate, axis);
}

//============================================================================

// C: cl_scrn.c:408 SCR_Init
export function SCR_Init(c: ClientContext): void {
  const cv = c.cv;
  const cvars = c.cvars;
  cv.scr_viewsize = cvars.get('viewsize', '100', CVAR_ARCHIVE);
  cv.scr_conspeed = cvars.get('scr_conspeed', '3', 0);
  cv.scr_showturtle = cvars.get('scr_showturtle', '0', 0);
  cv.scr_showpause = cvars.get('scr_showpause', '1', 0);
  cv.scr_centertime = cvars.get('scr_centertime', '2.5', 0);
  cv.scr_printspeed = cvars.get('scr_printspeed', '8', 0);
  cv.scr_netgraph = cvars.get('netgraph', '0', 0);
  cv.scr_timegraph = cvars.get('timegraph', '0', 0);
  cv.scr_debuggraph = cvars.get('debuggraph', '0', 0);
  cv.scr_graphheight = cvars.get('graphheight', '32', 0);
  cv.scr_graphscale = cvars.get('graphscale', '1', 0);
  cv.scr_graphshift = cvars.get('graphshift', '0', 0);
  cv.scr_drawall = cvars.get('scr_drawall', '0', 0);

  //
  // register our commands
  //
  c.cmd.addCommand('timerefresh', () => SCR_TimeRefresh_f(c));
  c.cmd.addCommand('loading', () => SCR_Loading_f(c));
  c.cmd.addCommand('sizeup', () => SCR_SizeUp_f(c));
  c.cmd.addCommand('sizedown', () => SCR_SizeDown_f(c));
  c.cmd.addCommand('sky', () => SCR_Sky_f(c));

  c.scr.scr_initialized = true;
}

// C: cl_scrn.c:442 SCR_DrawNet
export function SCR_DrawNet(c: ClientContext): void {
  const nc = c.cls.netchan;
  if (nc.outgoing_sequence - nc.incoming_acknowledged < CMD_BACKUP - 1) return;

  c.re.drawPic(c.scr.scr_vrect.x + 64, c.scr.scr_vrect.y, 'net');
}

// C: cl_scrn.c:456 SCR_DrawPause
export function SCR_DrawPause(c: ClientContext): void {
  if (!c.cv.scr_showpause.value) return; // turn off for screenshots

  if (!c.cv.cl_paused.value) return;

  const [w] = c.re.drawGetPicSize('pause');
  c.re.drawPic(Math.trunc((c.viddef.width - w) / 2), Math.trunc(c.viddef.height / 2) + 8, 'pause');
}

// C: cl_scrn.c:475 SCR_DrawLoading
export function SCR_DrawLoading(c: ClientContext): void {
  if (!c.scr.scr_draw_loading) return;

  c.scr.scr_draw_loading = 0;
  const [w, h] = c.re.drawGetPicSize('loading');
  c.re.drawPic(Math.trunc((c.viddef.width - w) / 2), Math.trunc((c.viddef.height - h) / 2), 'loading');
}

//=============================================================================

// C: cl_scrn.c:496 SCR_RunConsole -- scroll it up or down
export function SCR_RunConsole(c: ClientContext): void {
  const s = c.scr;
  // decide on the height of the console
  if (c.cls.key_dest === key_console)
    s.scr_conlines = 0.5; // half screen
  else s.scr_conlines = 0; // none visible

  const step = fr(c.cv.scr_conspeed.value * c.cls.frametime);
  if (s.scr_conlines < s.scr_con_current) {
    s.scr_con_current = fr(s.scr_con_current - step);
    if (s.scr_conlines > s.scr_con_current) s.scr_con_current = s.scr_conlines;
  } else if (s.scr_conlines > s.scr_con_current) {
    s.scr_con_current = fr(s.scr_con_current + step);
    if (s.scr_conlines < s.scr_con_current) s.scr_con_current = s.scr_conlines;
  }
}

// C: cl_scrn.c:525 SCR_DrawConsole
export function SCR_DrawConsole(c: ClientContext): void {
  const { cls, cl } = c;
  Con_CheckResize(c);

  if (cls.state === ca_disconnected || cls.state === ca_connecting) {
    // forced full screen console
    Con_DrawConsole(c, 1.0);
    return;
  }

  if (cls.state !== ca_active || !cl.refresh_prepped) {
    // connected, but can't render
    Con_DrawConsole(c, 0.5);
    c.re.drawFill(0, Math.trunc(c.viddef.height / 2), c.viddef.width, Math.trunc(c.viddef.height / 2), 0);
    return;
  }

  if (c.scr.scr_con_current) {
    Con_DrawConsole(c, c.scr.scr_con_current);
  } else {
    if (cls.key_dest === key_game || cls.key_dest === key_message) Con_DrawNotify(c); // only draw notify in game
  }
}

//=============================================================================

// C: cl_scrn.c:560 SCR_BeginLoadingPlaque
export function SCR_BeginLoadingPlaque(c: ClientContext): void {
  const { cls, cl } = c;
  c.sound.stopAllSounds();
  cl.sound_prepped = false; // don't play ambients
  // CDAudio_Stop: no CD audio in the browser
  if (cls.disable_screen) return;
  if (c.cv.developer && c.cv.developer.value) return;
  if (cls.state === ca_disconnected) return; // if at console, don't bring up the plaque
  if (cls.key_dest === key_console) return;
  if (cl.cinematictime > 0)
    c.scr.scr_draw_loading = 2; // clear to balack first
  else c.scr.scr_draw_loading = 1;
  // (C draws the frame once here; the plaque stays up until SCR_EndLoadingPlaque)
  SCR_UpdateScreen(c);
  cls.disable_screen = Sys_Milliseconds(c);
  cls.disable_servercount = cl.servercount;
  const mapname = cl.configstrings[CS_MODELS + 1];
  c.host.onLoading?.(mapname ? { active: true, mapname } : { active: true });
}

// C: cl_scrn.c:587 SCR_EndLoadingPlaque
export function SCR_EndLoadingPlaque(c: ClientContext): void {
  c.cls.disable_screen = 0;
  Con_ClearNotify(c);
  c.host.onLoading?.({ active: false });
}

// C: cl_scrn.c:598 SCR_Loading_f
export function SCR_Loading_f(c: ClientContext): void {
  SCR_BeginLoadingPlaque(c);
}

// C: cl_scrn.c:623 SCR_TimeRefresh_f
export function SCR_TimeRefresh_f(c: ClientContext): void {
  const { cls, cl } = c;
  if (cls.state !== ca_active) return;

  const start = Sys_Milliseconds(c);

  if (c.cmd.argc() === 2) {
    // run without page flipping
    c.re.beginFrame(0);
    for (let i = 0; i < 128; i++) {
      cl.refdef.viewangles[1] = (i / 128.0) * 360.0;
      c.re.renderFrame(cl.refdef);
    }
    c.re.endFrame();
  } else {
    for (let i = 0; i < 128; i++) {
      cl.refdef.viewangles[1] = (i / 128.0) * 360.0;

      c.re.beginFrame(0);
      c.re.renderFrame(cl.refdef);
      c.re.endFrame();
    }
  }

  const stop = Sys_Milliseconds(c);
  const time = fr((stop - start) / 1000.0);
  Com_Printf(c, '%f seconds (%f fps)\n', time, 128 / time);
}

// C: cl_scrn.c:666 SCR_AddDirtyPoint
export function SCR_AddDirtyPoint(c: ClientContext, x: number, y: number): void {
  const d = c.scr.scr_dirty;
  if (x < d.x1) d.x1 = x;
  if (x > d.x2) d.x2 = x;
  if (y < d.y1) d.y1 = y;
  if (y > d.y2) d.y2 = y;
}

// C: cl_scrn.c:678 SCR_DirtyScreen
export function SCR_DirtyScreen(c: ClientContext): void {
  SCR_AddDirtyPoint(c, 0, 0);
  SCR_AddDirtyPoint(c, c.viddef.width - 1, c.viddef.height - 1);
}

// C: cl_scrn.c:691 SCR_TileClear -- clear any parts of the tiled background that were drawn on last frame
export function SCR_TileClear(c: ClientContext): void {
  const s = c.scr;
  if (c.cv.scr_drawall.value) SCR_DirtyScreen(c); // for power vr or broken page flippers...

  if (s.scr_con_current === 1.0) return; // full screen console
  if (c.cv.scr_viewsize.value === 100) return; // full screen rendering
  if (c.cl.cinematictime > 0) return; // full screen cinematic

  // erase rect will be the union of the past three frames
  // so tripple buffering works properly
  const clear: Dirty = { ...s.scr_dirty };
  for (let i = 0; i < 2; i++) {
    const o = s.scr_old_dirty[i]!;
    if (o.x1 < clear.x1) clear.x1 = o.x1;
    if (o.x2 > clear.x2) clear.x2 = o.x2;
    if (o.y1 < clear.y1) clear.y1 = o.y1;
    if (o.y2 > clear.y2) clear.y2 = o.y2;
  }

  Object.assign(s.scr_old_dirty[1]!, s.scr_old_dirty[0]!);
  Object.assign(s.scr_old_dirty[0]!, s.scr_dirty);

  s.scr_dirty.x1 = 9999;
  s.scr_dirty.x2 = -9999;
  s.scr_dirty.y1 = 9999;
  s.scr_dirty.y2 = -9999;

  // don't bother with anything convered by the console)
  let top = cInt(fr(s.scr_con_current * c.viddef.height));
  if (top >= clear.y1) clear.y1 = top;

  if (clear.y2 <= clear.y1) return; // nothing disturbed

  const vr = s.scr_vrect;
  top = vr.y;
  const bottom = top + vr.height - 1;
  const left = vr.x;
  const right = left + vr.width - 1;

  let i: number;
  if (clear.y1 < top) {
    // clear above view screen
    i = clear.y2 < top - 1 ? clear.y2 : top - 1;
    c.re.drawTileClear(clear.x1, clear.y1, clear.x2 - clear.x1 + 1, i - clear.y1 + 1, 'backtile');
    clear.y1 = top;
  }
  if (clear.y2 > bottom) {
    // clear below view screen
    i = clear.y1 > bottom + 1 ? clear.y1 : bottom + 1;
    c.re.drawTileClear(clear.x1, i, clear.x2 - clear.x1 + 1, clear.y2 - i + 1, 'backtile');
    clear.y2 = bottom;
  }
  if (clear.x1 < left) {
    // clear left of view screen
    i = clear.x2 < left - 1 ? clear.x2 : left - 1;
    c.re.drawTileClear(clear.x1, clear.y1, i - clear.x1 + 1, clear.y2 - clear.y1 + 1, 'backtile');
    clear.x1 = left;
  }
  if (clear.x2 > right) {
    // clear left of view screen
    i = clear.x1 > right + 1 ? clear.x1 : right + 1;
    c.re.drawTileClear(i, clear.y1, clear.x2 - i + 1, clear.y2 - clear.y1 + 1, 'backtile');
    clear.x2 = right;
  }
}

//===============================================================

// C: cl_scrn.c:801 SizeHUDString -- allow embedded \n in the string
export function SizeHUDString(string: string): { w: number; h: number } {
  return SizeHUDStringPure(string);
}

/** Executes layout draw ops through the refresh (SCR_AddDirtyPoint / re.DrawPic / re.DrawChar). */
export function SCR_RunDrawOps(c: ClientContext, ops: readonly DrawOp[]): void {
  for (const op of ops) {
    switch (op.op) {
      case 'pic':
        c.re.drawPic(op.x, op.y, op.name);
        break;
      case 'char':
        c.re.drawChar(op.x, op.y, op.num);
        break;
      case 'string': {
        let x = op.x;
        for (let i = 0; i < op.text.length; i++) {
          c.re.drawChar(x, op.y, (op.text.charCodeAt(i) ^ op.xor) & 255);
          x += 8;
        }
        break;
      }
      case 'dirty':
        SCR_AddDirtyPoint(c, op.x, op.y);
        break;
      case 'error':
        Com_Error(c, ERR_DROP, '%s', op.msg);
    }
  }
}

// C: cl_scrn.c:829 DrawHUDString
export function DrawHUDString(
  c: ClientContext,
  string: string,
  x: number,
  y: number,
  centerwidth: number,
  xor: number,
): void {
  const ops: DrawOp[] = [];
  opDrawHUDString(ops, string, x, y, centerwidth, xor);
  SCR_RunDrawOps(c, ops);
}

// C: cl_scrn.c:870 SCR_DrawField
export function SCR_DrawField(
  c: ClientContext,
  x: number,
  y: number,
  color: number,
  width: number,
  value: number,
): void {
  const ops: DrawOp[] = [];
  opDrawField(ops, x, y, color, width, value);
  SCR_RunDrawOps(c, ops);
}

// C: cl_scrn.c:915 SCR_TouchPics -- allows rendering code to cache all needed sbar graphics.
// Async: the crosshair pic is registered before its size is queried (the C GL refresh loads it on
// demand inside Draw_GetPicSize).
export async function SCR_TouchPics(c: ClientContext): Promise<void> {
  const loads: Promise<unknown>[] = [];
  for (let i = 0; i < 2; i++) for (let j = 0; j < 11; j++) loads.push(RegisterPic(c, sb_nums[i]![j]!));
  await Promise.all(loads);

  const crosshair = c.cv.crosshair;
  if (crosshair && crosshair.value) {
    // (sic) the value is clamped without touching the cvar string
    if (crosshair.value > 3 || crosshair.value < 0) crosshair.value = 3;

    const s = c.scr;
    s.crosshair_pic = `ch${cInt(crosshair.value)}`;
    await RegisterPic(c, s.crosshair_pic);
    const [w, h] = c.re.drawGetPicSize(s.crosshair_pic);
    s.crosshair_width = w;
    s.crosshair_height = h;
    if (!s.crosshair_width) s.crosshair_pic = '';
  }
}

/** Builds the pure layout environment from the client state. */
export function SCR_LayoutEnv(c: ClientContext): LayoutEnv {
  const cl = c.cl;
  return {
    width: c.viddef.width,
    height: c.viddef.height,
    stats: cl.frame.playerstate.stats,
    configstrings: cl.configstrings,
    serverframe: cl.frame.serverframe,
    playernum: cl.playernum,
    clientName: (i) => cl.clientinfo[i]!.name,
    clientIcon: (i) => {
      const ci = cl.clientinfo[i]!;
      return ci.icon ? ci.iconname : cl.baseclientinfo.iconname;
    },
  };
}

// C: cl_scrn.c:941 SCR_ExecuteLayoutString
export function SCR_ExecuteLayoutString(c: ClientContext, s: string): void {
  if (c.cls.state !== ca_active || !c.cl.refresh_prepped) return;

  if (!s.length) return;

  SCR_RunDrawOps(c, executeLayoutString(s, SCR_LayoutEnv(c)));
}

// C: cl_scrn.c:1236 SCR_DrawStats -- the status bar is a small layout program that is based on the stats array
// (the statusbar configstring may be longer than MAX_QPATH: it spans CS_STATUSBAR..CS_AIRACCEL-1 in C)
export function SCR_DrawStats(c: ClientContext): void {
  SCR_ExecuteLayoutString(c, c.cl.configstrings[CS_STATUSBAR]!);
}

// C: cl_scrn.c:1250 SCR_DrawLayout
export function SCR_DrawLayout(c: ClientContext): void {
  if (!c.cl.frame.playerstate.stats[STAT_LAYOUTS]) return;
  SCR_ExecuteLayoutString(c, c.cl.layout);
}

//=======================================================

// C: cl_scrn.c:1267 SCR_UpdateScreen
// This is called every frame, and can also be called explicitly to flush text to the screen.
export function SCR_UpdateScreen(c: ClientContext): void {
  const { cls, cl, cv, re } = c;
  const s = c.scr;
  const separation = [0, 0];
  let numframes: number;

  // if the screen is disabled (loading plaque is up, or vid mode changing)
  // do nothing at all
  if (cls.disable_screen) {
    if (Sys_Milliseconds(c) - cls.disable_screen > 120000) {
      cls.disable_screen = 0;
      Com_Printf(c, 'Loading plaque timed out.\n');
    }
    return;
  }

  if (!s.scr_initialized || !c.con.initialized) return; // not initialized yet

  // range check cl_camera_separation so we don't inadvertently fry someone's brain
  if (cv.cl_stereo_separation.value > 1.0) c.cvars.setValue('cl_stereo_separation', 1.0);
  else if (cv.cl_stereo_separation.value < 0) c.cvars.setValue('cl_stereo_separation', 0.0);

  if (cv.cl_stereo.value) {
    numframes = 2;
    separation[0] = fr(-cv.cl_stereo_separation.value / 2);
    separation[1] = fr(cv.cl_stereo_separation.value / 2);
  } else {
    separation[0] = 0;
    separation[1] = 0;
    numframes = 1;
  }

  for (let i = 0; i < numframes; i++) {
    re.beginFrame(separation[i]!);

    if (s.scr_draw_loading === 2) {
      //  loading plaque over black screen
      re.cinematicSetPalette(null);
      s.scr_draw_loading = 0;
      const [w, h] = re.drawGetPicSize('loading');
      re.drawPic(Math.trunc((c.viddef.width - w) / 2), Math.trunc((c.viddef.height - h) / 2), 'loading');
    }
    // if a cinematic is supposed to be running, handle menus
    // and console specially
    else if (cl.cinematictime > 0) {
      if (cls.key_dest === key_menu) {
        if (cl.cinematicpalette_active) {
          re.cinematicSetPalette(null);
          cl.cinematicpalette_active = false;
        }
        M_Draw(c);
      } else if (cls.key_dest === key_console) {
        if (cl.cinematicpalette_active) {
          re.cinematicSetPalette(null);
          cl.cinematicpalette_active = false;
        }
        SCR_DrawConsole(c);
      } else {
        SCR_DrawCinematic(c);
      }
    } else {
      // make sure the game palette is active
      if (cl.cinematicpalette_active) {
        re.cinematicSetPalette(null);
        cl.cinematicpalette_active = false;
      }

      // do 3D refresh drawing, and then update the screen
      SCR_CalcVrect(c);

      // clear any dirty part of the background
      SCR_TileClear(c);

      V_RenderView(c, separation[i]!);

      SCR_DrawStats(c);
      if (cl.frame.playerstate.stats[STAT_LAYOUTS]! & 1) SCR_DrawLayout(c);
      if (cl.frame.playerstate.stats[STAT_LAYOUTS]! & 2) CL_DrawInventory(c);

      SCR_DrawNet(c);
      SCR_CheckDrawCenterString(c);

      if (cv.scr_timegraph.value) SCR_DebugGraph(c, fr(cls.frametime * 300), 0);

      if (cv.scr_debuggraph.value || cv.scr_timegraph.value || cv.scr_netgraph.value) SCR_DrawDebugGraph(c);

      SCR_DrawPause(c);

      SCR_DrawConsole(c);

      M_Draw(c);

      SCR_DrawLoading(c);
    }
  }
  re.endFrame();
}
