// Port of client/keys.c -- key bindings, key event routing (key_dest machine), console line editing,
// chat message typing. Key numbers are the K_* values of client/keys.h; printable keys are their
// (unshifted) ASCII codes.
//
// key up events are sent even if in console mode
import {
  ERR_FATAL,
  K_ALT,
  K_AUX1,
  K_AUX10,
  K_AUX11,
  K_AUX12,
  K_AUX13,
  K_AUX14,
  K_AUX15,
  K_AUX16,
  K_AUX17,
  K_AUX18,
  K_AUX19,
  K_AUX2,
  K_AUX20,
  K_AUX21,
  K_AUX22,
  K_AUX23,
  K_AUX24,
  K_AUX25,
  K_AUX26,
  K_AUX27,
  K_AUX28,
  K_AUX29,
  K_AUX3,
  K_AUX30,
  K_AUX31,
  K_AUX32,
  K_AUX4,
  K_AUX5,
  K_AUX6,
  K_AUX7,
  K_AUX8,
  K_AUX9,
  K_BACKSPACE,
  K_CTRL,
  K_DEL,
  K_DOWNARROW,
  K_END,
  K_ENTER,
  K_ESCAPE,
  K_F1,
  K_F10,
  K_F11,
  K_F12,
  K_F2,
  K_F3,
  K_F4,
  K_F5,
  K_F6,
  K_F7,
  K_F8,
  K_F9,
  K_HOME,
  K_INS,
  K_JOY1,
  K_JOY2,
  K_JOY3,
  K_JOY4,
  K_KP_5,
  K_KP_DEL,
  K_KP_DOWNARROW,
  K_KP_END,
  K_KP_ENTER,
  K_KP_HOME,
  K_KP_INS,
  K_KP_LEFTARROW,
  K_KP_MINUS,
  K_KP_PGDN,
  K_KP_PGUP,
  K_KP_PLUS,
  K_KP_RIGHTARROW,
  K_KP_SLASH,
  K_KP_UPARROW,
  K_LEFTARROW,
  K_MOUSE1,
  K_MOUSE2,
  K_MOUSE3,
  K_MWHEELDOWN,
  K_MWHEELUP,
  K_PAUSE,
  K_PGDN,
  K_PGUP,
  K_RIGHTARROW,
  K_SHIFT,
  K_SPACE,
  K_TAB,
  K_UPARROW,
  Q_strcasecmp,
  STAT_LAYOUTS,
  sprintf,
} from 'q2-shared';
import {
  ca_active,
  ca_disconnected,
  Com_Error,
  Com_Printf,
  key_console,
  key_game,
  key_menu,
  key_message,
  type ClientContext,
} from './client';
import { Con_ToggleConsole_f } from './console';
import { M_Keydown, M_Menu_Main_f } from './menu';
import { SCR_UpdateScreen } from './cl_scrn';

export const MAXCMDLINE = 256;

// C: keys.c globals
export class KeysState {
  /** char key_lines[32][MAXCMDLINE] (NUL-terminated byte strings) */
  readonly key_lines: Uint8Array[] = Array.from({ length: 32 }, () => new Uint8Array(MAXCMDLINE));
  key_linepos = 0;
  shift_down = false;
  anykeydown = 0;

  edit_line = 0;
  history_line = 0;

  key_waiting = 0;
  readonly keybindings: (string | null)[] = new Array<string | null>(256).fill(null);
  readonly consolekeys: boolean[] = new Array<boolean>(256).fill(false); // if true, can't be rebound while in console
  readonly menubound: boolean[] = new Array<boolean>(256).fill(false); // if true, can't be rebound while in menu
  readonly keyshift = new Int32Array(256); // key to map to if shift held down in console
  readonly key_repeats = new Int32Array(256); // if > 1, it is autorepeating
  readonly keydown: boolean[] = new Array<boolean>(256).fill(false);

  chat_team = false;
  readonly chat_buffer = new Uint8Array(MAXCMDLINE);
  chat_bufferlen = 0;

  /** Key_GetKey waiters (browser replacement for the blocking loop) */
  getKeyWaiters: ((key: number) => void)[] = [];
}

// C: keys.c:52 keynames
const keynames: readonly [string, number][] = [
  ['TAB', K_TAB],
  ['ENTER', K_ENTER],
  ['ESCAPE', K_ESCAPE],
  ['SPACE', K_SPACE],
  ['BACKSPACE', K_BACKSPACE],
  ['UPARROW', K_UPARROW],
  ['DOWNARROW', K_DOWNARROW],
  ['LEFTARROW', K_LEFTARROW],
  ['RIGHTARROW', K_RIGHTARROW],

  ['ALT', K_ALT],
  ['CTRL', K_CTRL],
  ['SHIFT', K_SHIFT],

  ['F1', K_F1],
  ['F2', K_F2],
  ['F3', K_F3],
  ['F4', K_F4],
  ['F5', K_F5],
  ['F6', K_F6],
  ['F7', K_F7],
  ['F8', K_F8],
  ['F9', K_F9],
  ['F10', K_F10],
  ['F11', K_F11],
  ['F12', K_F12],

  ['INS', K_INS],
  ['DEL', K_DEL],
  ['PGDN', K_PGDN],
  ['PGUP', K_PGUP],
  ['HOME', K_HOME],
  ['END', K_END],

  ['MOUSE1', K_MOUSE1],
  ['MOUSE2', K_MOUSE2],
  ['MOUSE3', K_MOUSE3],

  ['JOY1', K_JOY1],
  ['JOY2', K_JOY2],
  ['JOY3', K_JOY3],
  ['JOY4', K_JOY4],

  ['AUX1', K_AUX1],
  ['AUX2', K_AUX2],
  ['AUX3', K_AUX3],
  ['AUX4', K_AUX4],
  ['AUX5', K_AUX5],
  ['AUX6', K_AUX6],
  ['AUX7', K_AUX7],
  ['AUX8', K_AUX8],
  ['AUX9', K_AUX9],
  ['AUX10', K_AUX10],
  ['AUX11', K_AUX11],
  ['AUX12', K_AUX12],
  ['AUX13', K_AUX13],
  ['AUX14', K_AUX14],
  ['AUX15', K_AUX15],
  ['AUX16', K_AUX16],
  ['AUX17', K_AUX17],
  ['AUX18', K_AUX18],
  ['AUX19', K_AUX19],
  ['AUX20', K_AUX20],
  ['AUX21', K_AUX21],
  ['AUX22', K_AUX22],
  ['AUX23', K_AUX23],
  ['AUX24', K_AUX24],
  ['AUX25', K_AUX25],
  ['AUX26', K_AUX26],
  ['AUX27', K_AUX27],
  ['AUX28', K_AUX28],
  ['AUX29', K_AUX29],
  ['AUX30', K_AUX30],
  ['AUX31', K_AUX31],
  ['AUX32', K_AUX32],

  ['KP_HOME', K_KP_HOME],
  ['KP_UPARROW', K_KP_UPARROW],
  ['KP_PGUP', K_KP_PGUP],
  ['KP_LEFTARROW', K_KP_LEFTARROW],
  ['KP_5', K_KP_5],
  ['KP_RIGHTARROW', K_KP_RIGHTARROW],
  ['KP_END', K_KP_END],
  ['KP_DOWNARROW', K_KP_DOWNARROW],
  ['KP_PGDN', K_KP_PGDN],
  ['KP_ENTER', K_KP_ENTER],
  ['KP_INS', K_KP_INS],
  ['KP_DEL', K_KP_DEL],
  ['KP_SLASH', K_KP_SLASH],
  ['KP_MINUS', K_KP_MINUS],
  ['KP_PLUS', K_KP_PLUS],

  ['MWHEELUP', K_MWHEELUP],
  ['MWHEELDOWN', K_MWHEELDOWN],

  ['PAUSE', K_PAUSE],

  ['SEMICOLON', 59], // because a raw semicolon seperates commands
];

// ---- byte-string helpers for the fixed char arrays
function cstrlen(b: Uint8Array, from = 0): number {
  let i = from;
  while (i < b.length && b[i]) i++;
  return i - from;
}
function cstrAt(b: Uint8Array, from = 0): string {
  let s = '';
  for (let i = from; i < b.length && b[i]; i++) s += String.fromCharCode(b[i]!);
  return s;
}
/** strcpy(dst + at, s) (clamped to the array) */
function cstrcpy(dst: Uint8Array, at: number, s: string): void {
  let i = 0;
  for (; i < s.length && at + i < dst.length - 1; i++) dst[at + i] = s.charCodeAt(i) & 255;
  dst[at + i] = 0;
}

/** Current console edit line as a string (without the leading ']'), for UI/tests. */
export function Key_EditLine(c: ClientContext): string {
  return cstrAt(c.keys.key_lines[c.keys.edit_line]!, 0);
}

/*
==============================================================================

			LINE TYPING INTO THE CONSOLE

==============================================================================
*/

// C: keys.c:164 CompleteCommand
export function CompleteCommand(c: ClientContext): void {
  const k = c.keys;
  const line = k.key_lines[k.edit_line]!;
  let s = 1;
  if (line[s] === 92 || line[s] === 47) s++;

  const partial = cstrAt(line, s);
  let cmd = c.cmd.completeCommand(partial);
  if (!cmd) cmd = c.cvars.completeVariable(partial);
  if (cmd) {
    line[1] = 47; // '/'
    cstrcpy(line, 2, cmd);
    k.key_linepos = Math.min(cmd.length + 2, MAXCMDLINE - 2);
    line[k.key_linepos] = 32;
    k.key_linepos++;
    line[k.key_linepos] = 0;
    return;
  }
}

// C: keys.c:194 Key_Console -- interactive line editing and console scrollback
export function Key_Console(c: ClientContext, key: number): void {
  const k = c.keys;
  const con = c.con;
  switch (key) {
    case K_KP_SLASH:
      key = 47; // '/'
      break;
    case K_KP_MINUS:
      key = 45; // '-'
      break;
    case K_KP_PLUS:
      key = 43; // '+'
      break;
    case K_KP_HOME:
      key = 55; // '7'
      break;
    case K_KP_UPARROW:
      key = 56;
      break;
    case K_KP_PGUP:
      key = 57;
      break;
    case K_KP_LEFTARROW:
      key = 52;
      break;
    case K_KP_5:
      key = 53;
      break;
    case K_KP_RIGHTARROW:
      key = 54;
      break;
    case K_KP_END:
      key = 49;
      break;
    case K_KP_DOWNARROW:
      key = 50;
      break;
    case K_KP_PGDN:
      key = 51;
      break;
    case K_KP_INS:
      key = 48;
      break;
    case K_KP_DEL:
      key = 46; // '.'
      break;
  }

  const line = k.key_lines[k.edit_line]!;

  if (
    ((key === 86 || key === 118) && k.keydown[K_CTRL]) ||
    ((key === K_INS || key === K_KP_INS) && k.keydown[K_SHIFT])
  ) {
    const clip = c.host.getClipboardData?.() ?? null;
    if (clip !== null) {
      // strtok(cbd, "\n\r\b"): the token end becomes the string end
      let cbd = clip;
      const isDelim = (ch: string) => ch === '\n' || ch === '\r' || ch === '\b';
      let st = 0;
      while (st < cbd.length && isDelim(cbd[st]!)) st++;
      if (st < cbd.length) {
        let e = st;
        while (e < cbd.length && !isDelim(cbd[e]!)) e++;
        cbd = cbd.slice(0, e);
      }

      let i = cbd.length;
      if (i + k.key_linepos >= MAXCMDLINE) i = MAXCMDLINE - k.key_linepos;

      if (i > 0) {
        cbd = cbd.slice(0, i);
        // strcat(key_lines[edit_line], cbd)
        cstrcpy(line, cstrlen(line), cbd);
        k.key_linepos += i;
      }
    }
    return;
  }

  if (key === 108) {
    // 'l'
    if (k.keydown[K_CTRL]) {
      c.cmd.cbufAddText('clear\n');
      return;
    }
  }

  if (key === K_ENTER || key === K_KP_ENTER) {
    // backslash text are commands, else chat
    if (line[1] === 92 || line[1] === 47)
      c.cmd.cbufAddText(cstrAt(line, 2)); // skip the >
    else c.cmd.cbufAddText(cstrAt(line, 1)); // valid command

    c.cmd.cbufAddText('\n');
    Com_Printf(c, '%s\n', cstrAt(line, 0));
    k.edit_line = (k.edit_line + 1) & 31;
    k.history_line = k.edit_line;
    k.key_lines[k.edit_line]![0] = 93; // ']'
    k.key_linepos = 1;
    if (c.cls.state === ca_disconnected) SCR_UpdateScreen(c); // force an update, because the command may take some time
    return;
  }

  if (key === K_TAB) {
    // command completion
    CompleteCommand(c);
    return;
  }

  if (
    key === K_BACKSPACE ||
    key === K_LEFTARROW ||
    key === K_KP_LEFTARROW ||
    (key === 104 && k.keydown[K_CTRL])
  ) {
    if (k.key_linepos > 1) k.key_linepos--;
    return;
  }

  if (key === K_UPARROW || key === K_KP_UPARROW || (key === 112 && k.keydown[K_CTRL])) {
    do {
      k.history_line = (k.history_line - 1) & 31;
    } while (k.history_line !== k.edit_line && !k.key_lines[k.history_line]![1]);
    if (k.history_line === k.edit_line) k.history_line = (k.edit_line + 1) & 31;
    k.key_lines[k.edit_line]!.set(k.key_lines[k.history_line]!);
    k.key_linepos = cstrlen(k.key_lines[k.edit_line]!);
    return;
  }

  if (key === K_DOWNARROW || key === K_KP_DOWNARROW || (key === 110 && k.keydown[K_CTRL])) {
    if (k.history_line === k.edit_line) return;
    do {
      k.history_line = (k.history_line + 1) & 31;
    } while (k.history_line !== k.edit_line && !k.key_lines[k.history_line]![1]);
    if (k.history_line === k.edit_line) {
      k.key_lines[k.edit_line]![0] = 93;
      k.key_linepos = 1;
    } else {
      k.key_lines[k.edit_line]!.set(k.key_lines[k.history_line]!);
      k.key_linepos = cstrlen(k.key_lines[k.edit_line]!);
    }
    return;
  }

  if (key === K_PGUP || key === K_KP_PGUP) {
    con.display -= 2;
    return;
  }

  if (key === K_PGDN || key === K_KP_PGDN) {
    con.display += 2;
    if (con.display > con.current) con.display = con.current;
    return;
  }

  if (key === K_HOME || key === K_KP_HOME) {
    con.display = con.current - con.totallines + 10;
    return;
  }

  if (key === K_END || key === K_KP_END) {
    con.display = con.current;
    return;
  }

  if (key < 32 || key > 127) return; // non printable

  if (k.key_linepos < MAXCMDLINE - 1) {
    line[k.key_linepos] = key;
    k.key_linepos++;
    line[k.key_linepos] = 0;
  }
}

//============================================================================

// C: keys.c:393 Key_Message
export function Key_Message(c: ClientContext, key: number): void {
  const k = c.keys;
  if (key === K_ENTER || key === K_KP_ENTER) {
    if (k.chat_team) c.cmd.cbufAddText('say_team "');
    else c.cmd.cbufAddText('say "');
    c.cmd.cbufAddText(cstrAt(k.chat_buffer, 0));
    c.cmd.cbufAddText('"\n');

    c.cls.key_dest = key_game;
    k.chat_bufferlen = 0;
    k.chat_buffer[0] = 0;
    return;
  }

  if (key === K_ESCAPE) {
    c.cls.key_dest = key_game;
    k.chat_bufferlen = 0;
    k.chat_buffer[0] = 0;
    return;
  }

  if (key < 32 || key > 127) return; // non printable

  if (key === K_BACKSPACE) {
    if (k.chat_bufferlen) {
      k.chat_bufferlen--;
      k.chat_buffer[k.chat_bufferlen] = 0;
    }
    return;
  }

  if (k.chat_bufferlen === MAXCMDLINE - 1) return; // all full

  k.chat_buffer[k.chat_bufferlen++] = key;
  k.chat_buffer[k.chat_bufferlen] = 0;
}

/** Current chat buffer text (UI/tests). */
export function Key_ChatBuffer(c: ClientContext): string {
  return cstrAt(c.keys.chat_buffer, 0);
}

//============================================================================

// C: keys.c:451 Key_StringToKeynum -- single ascii characters return themselves, K_* names are matched
export function Key_StringToKeynum(str: string | null): number {
  if (!str || !str.length) return -1;
  if (str.length === 1) return str.charCodeAt(0) & 255;

  for (const [name, keynum] of keynames) {
    if (!Q_strcasecmp(str, name)) return keynum;
  }
  return -1;
}

// C: keys.c:477 Key_KeynumToString
export function Key_KeynumToString(keynum: number): string {
  if (keynum === -1) return '<KEY NOT FOUND>';
  if (keynum > 32 && keynum < 127) {
    // printable ascii
    return String.fromCharCode(keynum);
  }

  for (const [name, kn] of keynames) if (keynum === kn) return name;

  return '<UNKNOWN KEYNUM>';
}

// C: keys.c:504 Key_SetBinding
export function Key_SetBinding(c: ClientContext, keynum: number, binding: string): void {
  if (keynum === -1) return;
  if (keynum < 0 || keynum > 255) return; // (C would index out of bounds)

  c.keys.keybindings[keynum] = binding;
  c.host.onConfig?.('bind', Key_KeynumToString(keynum), binding);
}

// C: keys.c:532 Key_Unbind_f
export function Key_Unbind_f(c: ClientContext): void {
  if (c.cmd.argc() !== 2) {
    Com_Printf(c, 'unbind <key> : remove commands from a key\n');
    return;
  }

  const b = Key_StringToKeynum(c.cmd.argv(1));
  if (b === -1) {
    Com_Printf(c, '"%s" isn\'t a valid key\n', c.cmd.argv(1));
    return;
  }

  Key_SetBinding(c, b, '');
}

// C: keys.c:552 Key_Unbindall_f
export function Key_Unbindall_f(c: ClientContext): void {
  for (let i = 0; i < 256; i++) if (c.keys.keybindings[i] !== null) Key_SetBinding(c, i, '');
}

// C: keys.c:567 Key_Bind_f
export function Key_Bind_f(c: ClientContext): void {
  const argc = c.cmd.argc();

  if (argc < 2) {
    Com_Printf(c, 'bind <key> [command] : attach a command to a key\n');
    return;
  }
  const b = Key_StringToKeynum(c.cmd.argv(1));
  if (b === -1) {
    Com_Printf(c, '"%s" isn\'t a valid key\n', c.cmd.argv(1));
    return;
  }

  if (argc === 2) {
    const kb = c.keys.keybindings[b];
    if (kb !== null && kb !== undefined) Com_Printf(c, '"%s" = "%s"\n', c.cmd.argv(1), kb);
    else Com_Printf(c, '"%s" is not bound\n', c.cmd.argv(1));
    return;
  }

  // copy the rest of the command line
  let cmd = ''; // start out with a null string
  for (let i = 2; i < argc; i++) {
    cmd += c.cmd.argv(i);
    if (i !== argc - 1) cmd += ' ';
  }

  Key_SetBinding(c, b, cmd.slice(0, 1023));
}

// C: keys.c:614 Key_WriteBindings -- returns the "bind key value" lines written to config.cfg
export function Key_WriteBindings(c: ClientContext): string {
  let out = '';
  for (let i = 0; i < 256; i++) {
    const kb = c.keys.keybindings[i];
    if (kb) out += sprintf('bind %s "%s"\n', Key_KeynumToString(i), kb);
  }
  return out;
}

// C: keys.c:630 Key_Bindlist_f
export function Key_Bindlist_f(c: ClientContext): void {
  for (let i = 0; i < 256; i++) {
    const kb = c.keys.keybindings[i];
    if (kb) Com_Printf(c, '%s "%s"\n', Key_KeynumToString(i), kb);
  }
}

// C: keys.c:645 Key_Init
export function Key_Init(c: ClientContext): void {
  const k = c.keys;
  for (let i = 0; i < 32; i++) {
    k.key_lines[i]![0] = 93; // ']'
    k.key_lines[i]![1] = 0;
  }
  k.key_linepos = 1;

  //
  // init ascii characters in console mode
  //
  const ck = k.consolekeys;
  for (let i = 32; i < 128; i++) ck[i] = true;
  for (const key of [
    K_ENTER,
    K_KP_ENTER,
    K_TAB,
    K_LEFTARROW,
    K_KP_LEFTARROW,
    K_RIGHTARROW,
    K_KP_RIGHTARROW,
    K_UPARROW,
    K_KP_UPARROW,
    K_DOWNARROW,
    K_KP_DOWNARROW,
    K_BACKSPACE,
    K_HOME,
    K_KP_HOME,
    K_END,
    K_KP_END,
    K_PGUP,
    K_KP_PGUP,
    K_PGDN,
    K_KP_PGDN,
    K_SHIFT,
    K_INS,
    K_KP_INS,
    K_KP_DEL,
    K_KP_SLASH,
    K_KP_PLUS,
    K_KP_MINUS,
    K_KP_5,
  ])
    ck[key] = true;

  ck[96] = false; // '`'
  ck[126] = false; // '~'

  const ks = k.keyshift;
  for (let i = 0; i < 256; i++) ks[i] = i;
  for (let i = 97; i <= 122; i++) ks[i] = i - 97 + 65;
  const shifts = '1!2@3#4$5%6^7&8*9(0)-_=+,<.>/?;:\'"[{]}`~\\|';
  for (let i = 0; i < shifts.length; i += 2) ks[shifts.charCodeAt(i)] = shifts.charCodeAt(i + 1);

  k.menubound[K_ESCAPE] = true;
  for (let i = 0; i < 12; i++) k.menubound[K_F1 + i] = true;

  //
  // register our functions
  //
  c.cmd.addCommand('bind', () => Key_Bind_f(c));
  c.cmd.addCommand('unbind', () => Key_Unbind_f(c));
  c.cmd.addCommand('unbindall', () => Key_Unbindall_f(c));
  c.cmd.addCommand('bindlist', () => Key_Bindlist_f(c));
}

/**
 * C: keys.c:740 Key_Event -- called by the system between frames for both key up and key down events.
 * `time` is the unsigned Sys_Milliseconds timestamp of the event.
 */
export function Key_Event(c: ClientContext, key: number, down: boolean, time: number): void {
  const k = c.keys;
  if (key < 0 || key > 255) return; // (C would index out of bounds)
  time = time >>> 0;

  // hack for modal presses
  if (k.key_waiting === -1) {
    if (down) {
      k.key_waiting = key;
      const w = k.getKeyWaiters;
      k.getKeyWaiters = [];
      for (const f of w) f(key);
    }
    return;
  }

  // update auto-repeat status
  if (down) {
    k.key_repeats[key]!++;
    if (
      key !== K_BACKSPACE &&
      key !== K_PAUSE &&
      key !== K_PGUP &&
      key !== K_KP_PGUP &&
      key !== K_PGDN &&
      key !== K_KP_PGDN &&
      k.key_repeats[key]! > 1
    )
      return; // ignore most autorepeats

    if (key >= 200 && k.keybindings[key] === null)
      Com_Printf(c, '%s is unbound, hit F4 to set.\n', Key_KeynumToString(key));
  } else {
    k.key_repeats[key] = 0;
  }

  if (key === K_SHIFT) k.shift_down = down;

  // console key is hardcoded, so the user can never unbind it
  if (key === 96 || key === 126) {
    if (!down) return;
    Con_ToggleConsole_f(c);
    return;
  }

  // any key during the attract mode will bring up the menu
  if (c.cl.attractloop && c.cls.key_dest !== key_menu) key = K_ESCAPE;

  // menu key is hardcoded, so the user can never unbind it
  if (key === K_ESCAPE) {
    if (!down) return;

    if (c.cl.frame.playerstate.stats[STAT_LAYOUTS] && c.cls.key_dest === key_game) {
      // put away help computer / inventory
      c.cmd.cbufAddText('cmd putaway\n');
      return;
    }
    switch (c.cls.key_dest) {
      case key_message:
        Key_Message(c, key);
        break;
      case key_menu:
        M_Keydown(c, key);
        break;
      case key_game:
      case key_console:
        M_Menu_Main_f(c);
        break;
      default:
        Com_Error(c, ERR_FATAL, 'Bad cls.key_dest');
    }
    return;
  }

  // track if any key is down for BUTTON_ANY
  k.keydown[key] = down;
  if (down) {
    if (k.key_repeats[key] === 1) k.anykeydown++;
  } else {
    k.anykeydown--;
    if (k.anykeydown < 0) k.anykeydown = 0;
  }

  //
  // key up events only generate commands if the game key binding is
  // a button command (leading + sign).  These will occur even in console mode,
  // to keep the character from continuing an action started before a console
  // switch.  Button commands include the kenum as a parameter, so multiple
  // downs can be matched with ups
  //
  if (!down) {
    let kb = k.keybindings[key];
    if (kb && kb[0] === '+')
      c.cmd.cbufAddText(sprintf('-%s %i %i\n', kb.slice(1), key, time | 0).slice(0, 1023));
    if (k.keyshift[key] !== key) {
      kb = k.keybindings[k.keyshift[key]!];
      if (kb && kb[0] === '+')
        c.cmd.cbufAddText(sprintf('-%s %i %i\n', kb.slice(1), key, time | 0).slice(0, 1023));
    }
    return;
  }

  //
  // if not a consolekey, send to the interpreter no matter what mode is
  //
  const kd = c.cls.key_dest;
  if (
    (kd === key_menu && k.menubound[key]) ||
    (kd === key_console && !k.consolekeys[key]) ||
    (kd === key_game && (c.cls.state === ca_active || !k.consolekeys[key]))
  ) {
    const kb = k.keybindings[key];
    if (kb !== null && kb !== undefined) {
      if (kb[0] === '+') {
        // button commands add keynum and time as a parm
        c.cmd.cbufAddText(sprintf('%s %i %i\n', kb, key, time | 0).slice(0, 1023));
      } else {
        c.cmd.cbufAddText(kb);
        c.cmd.cbufAddText('\n');
      }
    }
    return;
  }

  if (!down) return; // other systems only care about key down events

  if (k.shift_down) key = k.keyshift[key]!;

  switch (c.cls.key_dest) {
    case key_message:
      Key_Message(c, key);
      break;
    case key_menu:
      M_Keydown(c, key);
      break;

    case key_game:
    case key_console:
      Key_Console(c, key);
      break;
    default:
      Com_Error(c, ERR_FATAL, 'Bad cls.key_dest');
  }
}

// C: keys.c:913 Key_ClearStates
export function Key_ClearStates(c: ClientContext): void {
  const k = c.keys;
  k.anykeydown = 0;

  for (let i = 0; i < 256; i++) {
    if (k.keydown[i] || k.key_repeats[i]) Key_Event(c, i, false, 0);
    k.keydown[i] = false;
    k.key_repeats[i] = 0;
  }
}

/** C: keys.c:934 Key_GetKey -- the blocking wait becomes a promise resolved by the next key down. */
export function Key_GetKey(c: ClientContext): Promise<number> {
  c.keys.key_waiting = -1;
  return new Promise((resolve) =>
    c.keys.getKeyWaiters.push((key) => {
      resolve(key);
    }),
  );
}
