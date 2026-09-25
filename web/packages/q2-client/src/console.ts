// Port of client/console.c.
// The console text is a ring of `totallines` lines of `linewidth` bytes in a CON_TEXTSIZE byte buffer, as
// in the original. Bytes >= 128 select the alternate (green) charset of conchars.
import { cInt, fr, sprintf } from 'q2-shared';
import {
  ca_active,
  ca_disconnected,
  Com_Printf,
  key_console,
  key_game,
  key_menu,
  key_message,
  type ClientContext,
} from './client';
import { MAXCMDLINE } from './keys';
import { M_ForceMenuOff } from './menu';
import { SCR_AddDirtyPoint, SCR_EndLoadingPlaque } from './cl_scrn';

export const NUM_CON_TIMES = 4;
export const CON_TEXTSIZE = 32768;
/** C: qcommon.h VERSION */
const VERSION = 3.19;

// C: console.h console_t
export class ConsoleState {
  initialized = false;
  readonly text = new Uint8Array(CON_TEXTSIZE);
  current = 0; // line where next message will be printed
  x = 0; // offset in current line for next print
  display = 0; // bottom of console displays this line
  ormask = 0; // high bit mask for colored characters
  linewidth = 0; // characters across screen
  totallines = 0; // total lines in console scrollback
  /** float */
  cursorspeed = 0;
  vislines = 0;
  /** float times[NUM_CON_TIMES]: cls.realtime time the line was generated, for transparent notify lines */
  readonly times = new Float32Array(NUM_CON_TIMES);
  /** C: Con_Print static `cr` */
  cr = false;
}

// C: console.c:35 DrawString
export function DrawString(c: ClientContext, x: number, y: number, s: string): void {
  for (let i = 0; i < s.length; i++) {
    const ch = s.charCodeAt(i) & 255;
    if (!ch) break;
    c.re.drawChar(x, y, ch);
    x += 8;
  }
}

// C: console.c:45 DrawAltString
export function DrawAltString(c: ClientContext, x: number, y: number, s: string): void {
  for (let i = 0; i < s.length; i++) {
    const ch = s.charCodeAt(i) & 255;
    if (!ch) break;
    c.re.drawChar(x, y, ch ^ 0x80);
    x += 8;
  }
}

// C: console.c:56 Key_ClearTyping
export function Key_ClearTyping(c: ClientContext): void {
  const k = c.keys;
  k.key_lines[k.edit_line]![1] = 0; // clear any typing
  k.key_linepos = 1;
}

// C: console.c:67 Con_ToggleConsole_f
export function Con_ToggleConsole_f(c: ClientContext): void {
  SCR_EndLoadingPlaque(c); // get rid of loading plaque

  if (c.cl.attractloop) {
    c.cmd.cbufAddText('killserver\n');
    return;
  }

  if (c.cls.state === ca_disconnected) {
    // start the demo loop again
    c.cmd.cbufAddText('d1\n');
    return;
  }

  Key_ClearTyping(c);
  Con_ClearNotify(c);

  if (c.cls.key_dest === key_console) {
    M_ForceMenuOff(c);
    c.cvars.set('paused', '0');
  } else {
    M_ForceMenuOff(c);
    c.cls.key_dest = key_console;

    if (c.cvars.variableValue('maxclients') === 1 && c.cvars.serverState()) c.cvars.set('paused', '1');
  }
}

// C: console.c:107 Con_ToggleChat_f
export function Con_ToggleChat_f(c: ClientContext): void {
  Key_ClearTyping(c);

  if (c.cls.key_dest === key_console) {
    if (c.cls.state === ca_active) {
      M_ForceMenuOff(c);
      c.cls.key_dest = key_game;
    }
  } else c.cls.key_dest = key_console;

  Con_ClearNotify(c);
}

// C: console.c:130 Con_Clear_f
export function Con_Clear_f(c: ClientContext): void {
  c.con.text.fill(32);
}

/**
 * C: console.c:143 Con_Dump_f. There is no filesystem in the browser: the text that the original writes to
 * `<gamedir>/<name>.txt` is returned (and handed to the host as one print).
 */
export function Con_Dump_f(c: ClientContext): string | null {
  const con = c.con;
  if (c.cmd.argc() !== 2) {
    Com_Printf(c, 'usage: condump <filename>\n');
    return null;
  }
  const name = sprintf('%s/%s.txt', 'baseq2', c.cmd.argv(1));
  Com_Printf(c, 'Dumped console text to %s.\n', name);

  // skip empty lines
  let l: number;
  for (l = con.current - con.totallines + 1; l <= con.current; l++) {
    const line = ringLine(con, l) * con.linewidth;
    let x: number;
    for (x = 0; x < con.linewidth; x++) if (con.text[line + x] !== 32) break;
    if (x !== con.linewidth) break;
  }

  // write the remaining lines
  let out = '';
  for (; l <= con.current; l++) {
    const line = ringLine(con, l) * con.linewidth;
    const buffer = Array.from(con.text.subarray(line, line + con.linewidth));
    let end = con.linewidth;
    for (let x = con.linewidth - 1; x >= 0; x--) {
      if (buffer[x] === 32) end = x;
      else break;
    }
    let s = '';
    for (let x = 0; x < end; x++) {
      if (!buffer[x]) break;
      s += String.fromCharCode(buffer[x]! & 0x7f);
    }
    out += s + '\n';
  }
  c.host.onPrint?.(out);
  return out;
}

/** C `l % con.totallines` (C remainder keeps the sign; negative lines are never read in practice). */
function ringLine(con: ConsoleState, l: number): number {
  const r = l % con.totallines;
  return r < 0 ? r + con.totallines : r;
}

// C: console.c:207 Con_ClearNotify
export function Con_ClearNotify(c: ClientContext): void {
  for (let i = 0; i < NUM_CON_TIMES; i++) c.con.times[i] = 0;
}

// C: console.c:221 Con_MessageMode_f
export function Con_MessageMode_f(c: ClientContext): void {
  c.keys.chat_team = false;
  c.cls.key_dest = key_message;
}

// C: console.c:232 Con_MessageMode2_f
export function Con_MessageMode2_f(c: ClientContext): void {
  c.keys.chat_team = true;
  c.cls.key_dest = key_message;
}

// C: console.c:245 Con_CheckResize -- if the line width has changed, reformat the buffer
export function Con_CheckResize(c: ClientContext): void {
  const con = c.con;
  let width = (c.viddef.width >> 3) - 2;

  if (width === con.linewidth) return;

  if (width < 1) {
    // video hasn't been initialized yet
    width = 38;
    con.linewidth = width;
    con.totallines = Math.trunc(CON_TEXTSIZE / con.linewidth);
    con.text.fill(32);
  } else {
    const oldwidth = con.linewidth;
    con.linewidth = width;
    const oldtotallines = con.totallines;
    con.totallines = Math.trunc(CON_TEXTSIZE / con.linewidth);
    let numlines = oldtotallines;

    if (con.totallines < numlines) numlines = con.totallines;

    let numchars = oldwidth;

    if (con.linewidth < numchars) numchars = con.linewidth;

    const tbuf = con.text.slice();
    con.text.fill(32);

    for (let i = 0; i < numlines; i++) {
      for (let j = 0; j < numchars; j++) {
        con.text[(con.totallines - 1 - i) * con.linewidth + j] =
          tbuf[((con.current - i + oldtotallines) % oldtotallines) * oldwidth + j]!;
      }
    }

    Con_ClearNotify(c);
  }

  con.current = con.totallines - 1;
  con.display = con.current;
}

// C: console.c:304 Con_Init
export function Con_Init(c: ClientContext): void {
  c.con.linewidth = -1;

  Con_CheckResize(c);

  Com_Printf(c, 'Console initialized.\n');

  //
  // register our commands
  //
  c.cv.con_notifytime = c.cvars.get('con_notifytime', '3', 0);

  c.cmd.addCommand('toggleconsole', () => Con_ToggleConsole_f(c));
  c.cmd.addCommand('togglechat', () => Con_ToggleChat_f(c));
  c.cmd.addCommand('messagemode', () => Con_MessageMode_f(c));
  c.cmd.addCommand('messagemode2', () => Con_MessageMode2_f(c));
  c.cmd.addCommand('clear', () => Con_Clear_f(c));
  c.cmd.addCommand('condump', () => void Con_Dump_f(c));
  c.con.initialized = true;
}

// C: console.c:332 Con_Linefeed
export function Con_Linefeed(c: ClientContext): void {
  const con = c.con;
  con.x = 0;
  if (con.display === con.current) con.display++;
  con.current++;
  const start = ringLine(con, con.current) * con.linewidth;
  con.text.fill(32, start, start + con.linewidth);
}

/** signed-char `txt[l] <= ' '` (bytes >= 128 are negative), NUL past the end */
function isBreak(txt: string, i: number): boolean {
  if (i >= txt.length) return true;
  const ch = txt.charCodeAt(i) & 255;
  return ch <= 32 || ch >= 128;
}

// C: console.c:351 Con_Print -- handles cursor positioning, line wrapping, etc
export function Con_Print(c: ClientContext, txt: string): void {
  const con = c.con;
  if (!con.initialized) return;

  let p = 0;
  let mask: number;
  const c0 = txt.length ? txt.charCodeAt(0) : 0;
  if (c0 === 1 || c0 === 2) {
    mask = 128; // go to colored text
    p++;
  } else mask = 0;

  while (p < txt.length) {
    const ch = txt.charCodeAt(p) & 255;
    if (!ch) break;
    // count word length
    let l: number;
    for (l = 0; l < con.linewidth; l++) if (isBreak(txt, p + l)) break;

    // word wrap
    if (l !== con.linewidth && con.x + l > con.linewidth) con.x = 0;

    p++;

    if (con.cr) {
      con.current--;
      con.cr = false;
    }

    if (!con.x) {
      Con_Linefeed(c);
      // mark time for transparent overlay
      if (con.current >= 0) con.times[con.current % NUM_CON_TIMES] = c.cls.realtime;
    }

    switch (ch) {
      case 10:
        con.x = 0;
        break;

      case 13:
        con.x = 0;
        con.cr = true;
        break;

      default: {
        // display character and advance
        const y = ringLine(con, con.current);
        con.text[y * con.linewidth + con.x] = ch | mask | con.ormask;
        con.x++;
        if (con.x >= con.linewidth) con.x = 0;
        break;
      }
    }
  }
}

// C: console.c:427 Con_CenteredPrint
export function Con_CenteredPrint(c: ClientContext, text: string): void {
  let l = text.length;
  l = Math.trunc((c.con.linewidth - l) / 2);
  if (l < 0) l = 0;
  Con_Print(c, (' '.repeat(l) + text + '\n').slice(0, 1023));
}

/*
==============================================================================

DRAWING

==============================================================================
*/

// C: console.c:458 Con_DrawInput -- the input line scrolls horizontally if typing goes beyond the right edge
export function Con_DrawInput(c: ClientContext): void {
  const con = c.con;
  const k = c.keys;
  if (c.cls.key_dest === key_menu) return;
  if (c.cls.key_dest !== key_console && c.cls.state === ca_active) return; // don't draw anything (always draw if not active)

  const line = k.key_lines[k.edit_line]!;
  // the C code writes up to linewidth bytes into the 256-byte line; a scratch copy keeps wide screens safe
  const text = new Uint8Array(Math.max(MAXCMDLINE, k.key_linepos + con.linewidth + 2));
  text.set(line);

  // add the cursor frame
  text[k.key_linepos] = 10 + ((c.cls.realtime >> 8) & 1);

  // fill out remainder with spaces
  for (let i = k.key_linepos + 1; i < con.linewidth; i++) text[i] = 32;

  //	prestep if horizontally scrolling
  let ofs = 0;
  if (k.key_linepos >= con.linewidth) ofs = 1 + k.key_linepos - con.linewidth;

  // draw it
  for (let i = 0; i < con.linewidth; i++) c.re.drawChar((i + 1) << 3, con.vislines - 22, text[ofs + i]!);

  // the original's in-place writes (spaces after the cursor), then "remove cursor"
  line.set(text.subarray(0, MAXCMDLINE));
  line[k.key_linepos] = 0;
}

// C: console.c:500 Con_DrawNotify -- draws the last few lines of output transparently over the game top
export function Con_DrawNotify(c: ClientContext): void {
  const con = c.con;
  const k = c.keys;
  let v = 0;
  for (let i = con.current - NUM_CON_TIMES + 1; i <= con.current; i++) {
    if (i < 0) continue;
    let time = cInt(con.times[i % NUM_CON_TIMES]!);
    if (time === 0) continue;
    time = c.cls.realtime - time;
    if (time > fr(c.cv.con_notifytime.value * 1000)) continue;
    const text = (i % con.totallines) * con.linewidth;

    for (let x = 0; x < con.linewidth; x++) c.re.drawChar((x + 1) << 3, v, con.text[text + x]!);

    v += 8;
  }

  if (c.cls.key_dest === key_message) {
    let skip: number;
    if (k.chat_team) {
      DrawString(c, 8, v, 'say_team:');
      skip = 11;
    } else {
      DrawString(c, 8, v, 'say:');
      skip = 5;
    }

    let s = 0;
    const lim = (c.viddef.width >> 3) - (skip + 1);
    if (k.chat_bufferlen > lim) s += k.chat_bufferlen - lim;
    let x = 0;
    while (s + x < k.chat_bufferlen && k.chat_buffer[s + x]) {
      c.re.drawChar((x + skip) << 3, v, k.chat_buffer[s + x]!);
      x++;
    }
    c.re.drawChar((x + skip) << 3, v, 10 + ((c.cls.realtime >> 8) & 1));
    v += 8;
  }

  if (v) {
    SCR_AddDirtyPoint(c, 0, 0);
    SCR_AddDirtyPoint(c, c.viddef.width - 1, v);
  }
}

// C: console.c:569 Con_DrawConsole -- draws the console with the solid background
export function Con_DrawConsole(c: ClientContext, frac: number): void {
  const con = c.con;
  const vid = c.viddef;
  let lines = cInt(fr(vid.height * fr(frac)));
  if (lines <= 0) return;

  if (lines > vid.height) lines = vid.height;

  // draw the background
  c.re.drawStretchPic(0, -vid.height + lines, vid.width, vid.height, 'conback');
  SCR_AddDirtyPoint(c, 0, 0);
  SCR_AddDirtyPoint(c, vid.width - 1, lines - 1);

  const version = sprintf('v%4.2f', VERSION);
  for (let x = 0; x < 5; x++)
    c.re.drawChar(vid.width - 44 + x * 8, lines - 12, (128 + version.charCodeAt(x)) & 255);

  // draw the text
  con.vislines = lines;

  let rows = (lines - 22) >> 3; // rows of text to draw
  let y = lines - 30;

  // draw from the bottom up
  if (con.display !== con.current) {
    // draw arrows to show the buffer is backscrolled
    for (let x = 0; x < con.linewidth; x += 4) c.re.drawChar((x + 1) << 3, y, 94);

    y -= 8;
    rows--;
  }

  let row = con.display;
  for (let i = 0; i < rows; i++, y -= 8, row--) {
    if (row < 0) break;
    if (con.current - row >= con.totallines) break; // past scrollback wrap point

    const text = (row % con.totallines) * con.linewidth;

    for (let x = 0; x < con.linewidth; x++) c.re.drawChar((x + 1) << 3, y, con.text[text + x]!);
  }

  // (the download bar is never drawn: downloads are not supported, cls.download is always false)

  // draw the input prompt, user text, and cursor if desired
  Con_DrawInput(c);
}
