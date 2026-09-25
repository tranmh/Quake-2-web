// Pure port of the HUD layout language interpreter: client/cl_scrn.c SCR_ExecuteLayoutString (and the
// helpers it calls: SCR_DrawField, DrawHUDString, DrawString, DrawAltString). It produces a list of draw
// operations; cl_scrn.ts executes them through the Refresh interface. Keeping it pure makes every token
// snapshot-testable.
import {
  CS_IMAGES,
  MAX_CLIENTS,
  MAX_CONFIGSTRINGS,
  MAX_IMAGES,
  STAT_AMMO,
  STAT_ARMOR,
  STAT_FLASHES,
  STAT_HEALTH,
  atoi,
  sprintf,
  COM_Parse,
  type ParseCursor,
} from 'q2-shared';

/** Draw operations produced by the layout interpreter. */
export type DrawOp =
  /** re.DrawPic(x, y, name) */
  | { op: 'pic'; x: number; y: number; name: string }
  /** re.DrawChar(x, y, num) (num already masked to 0..255) */
  | { op: 'char'; x: number; y: number; num: number }
  /** DrawString / DrawAltString: re.DrawChar per char, x += 8, char ^ xor */
  | { op: 'string'; x: number; y: number; text: string; xor: number }
  /** SCR_AddDirtyPoint(x, y) */
  | { op: 'dirty'; x: number; y: number }
  /** Com_Error(ERR_DROP, msg): execution stops here */
  | { op: 'error'; msg: string };

/** Everything SCR_ExecuteLayoutString reads from the client state. */
export interface LayoutEnv {
  /** viddef.width / viddef.height */
  width: number;
  height: number;
  /** cl.frame.playerstate.stats */
  stats: ArrayLike<number>;
  /** cl.configstrings */
  configstrings: readonly string[];
  /** cl.frame.serverframe (flashing digits) */
  serverframe: number;
  /** cl.playernum */
  playernum: number;
  /** cl.clientinfo[i].name */
  clientName(i: number): string;
  /** icon name drawn for a client block: cl.clientinfo[i].iconname, or cl.baseclientinfo.iconname if !icon */
  clientIcon(i: number): string;
}

// C: cl_scrn.c:779 sb_nums
export const STAT_MINUS = 10; // num frame for '-' stats digit
export const sb_nums: readonly (readonly string[])[] = [
  ['num_0', 'num_1', 'num_2', 'num_3', 'num_4', 'num_5', 'num_6', 'num_7', 'num_8', 'num_9', 'num_minus'],
  [
    'anum_0',
    'anum_1',
    'anum_2',
    'anum_3',
    'anum_4',
    'anum_5',
    'anum_6',
    'anum_7',
    'anum_8',
    'anum_9',
    'anum_minus',
  ],
];

export const ICON_WIDTH = 24;
export const ICON_HEIGHT = 24;
export const CHAR_WIDTH = 16;
export const ICON_SPACE = 8;

// C: console.c:35 DrawString
export function opDrawString(ops: DrawOp[], x: number, y: number, s: string): void {
  if (s.length) ops.push({ op: 'string', x, y, text: s, xor: 0 });
}

// C: console.c:45 DrawAltString
export function opDrawAltString(ops: DrawOp[], x: number, y: number, s: string): void {
  if (s.length) ops.push({ op: 'string', x, y, text: s, xor: 0x80 });
}

// C: cl_scrn.c:801 SizeHUDString -- allow embedded \n in the string
export function SizeHUDString(string: string): { w: number; h: number } {
  let lines = 1;
  let width = 0;
  let current = 0;
  for (let i = 0; i < string.length; i++) {
    if (string[i] === '\n') {
      lines++;
      current = 0;
    } else {
      current++;
      if (current > width) width = current;
    }
  }
  return { w: width * 8, h: lines * 8 };
}

// C: cl_scrn.c:829 DrawHUDString
export function opDrawHUDString(
  ops: DrawOp[],
  string: string,
  x: number,
  y: number,
  centerwidth: number,
  xor: number,
): void {
  const margin = x;
  let p = 0;
  while (p < string.length) {
    // scan out one line of text from the string
    let line = '';
    while (p < string.length && string[p] !== '\n') line += string[p++];
    const width = line.length;
    if (centerwidth) x = margin + Math.trunc((centerwidth - width * 8) / 2);
    else x = margin;
    if (width) ops.push({ op: 'string', x, y, text: line, xor });
    x += width * 8;
    if (p < string.length) {
      p++; // skip the \n
      x = margin;
      y += 8;
    }
  }
}

// C: cl_scrn.c:870 SCR_DrawField
export function opDrawField(
  ops: DrawOp[],
  x: number,
  y: number,
  color: number,
  width: number,
  value: number,
): void {
  if (width < 1) return;

  // draw number string
  if (width > 5) width = 5;

  ops.push({ op: 'dirty', x, y });
  ops.push({ op: 'dirty', x: x + width * CHAR_WIDTH + 2, y: y + 23 });

  const num = sprintf('%i', value);
  let l = num.length;
  if (l > width) l = width;
  x += 2 + CHAR_WIDTH * (width - l);

  let ptr = 0;
  while (ptr < num.length && l) {
    let frame: number;
    if (num[ptr] === '-') frame = STAT_MINUS;
    else frame = num.charCodeAt(ptr) - 48;

    ops.push({ op: 'pic', x, y, name: sb_nums[color]![frame]! });
    x += CHAR_WIDTH;
    ptr++;
    l--;
  }
}

function stat(env: LayoutEnv, i: number): number {
  return env.stats[i] ?? 0;
}

function cs(env: LayoutEnv, i: number): string {
  return env.configstrings[i] ?? '';
}

// C: cl_scrn.c:941 SCR_ExecuteLayoutString (the cls.state/refresh_prepped guard is done by the caller)
export function executeLayoutString(str: string, env: LayoutEnv): DrawOp[] {
  const ops: DrawOp[] = [];
  if (!str.length) return ops;

  let x = 0;
  let y = 0;
  let width = 3;
  let value: number;
  const s: ParseCursor = { data: str, pos: 0 };
  const halfW = Math.trunc(env.width / 2);
  const halfH = Math.trunc(env.height / 2);

  while (s.pos >= 0) {
    let token = COM_Parse(s);
    if (token === 'xl') {
      token = COM_Parse(s);
      x = atoi(token);
      continue;
    }
    if (token === 'xr') {
      token = COM_Parse(s);
      x = env.width + atoi(token);
      continue;
    }
    if (token === 'xv') {
      token = COM_Parse(s);
      x = halfW - 160 + atoi(token);
      continue;
    }

    if (token === 'yt') {
      token = COM_Parse(s);
      y = atoi(token);
      continue;
    }
    if (token === 'yb') {
      token = COM_Parse(s);
      y = env.height + atoi(token);
      continue;
    }
    if (token === 'yv') {
      token = COM_Parse(s);
      y = halfH - 120 + atoi(token);
      continue;
    }

    if (token === 'pic') {
      // draw a pic from a stat number
      token = COM_Parse(s);
      value = stat(env, atoi(token));
      if (value >= MAX_IMAGES) {
        ops.push({ op: 'error', msg: 'Pic >= MAX_IMAGES' });
        return ops;
      }
      // (sic) `if (cl.configstrings[CS_IMAGES+value])` tests an array address: always true
      ops.push({ op: 'dirty', x, y });
      ops.push({ op: 'dirty', x: x + 23, y: y + 23 });
      ops.push({ op: 'pic', x, y, name: cs(env, CS_IMAGES + value) });
      continue;
    }

    if (token === 'client') {
      // draw a deathmatch client block
      token = COM_Parse(s);
      x = halfW - 160 + atoi(token);
      token = COM_Parse(s);
      y = halfH - 120 + atoi(token);
      ops.push({ op: 'dirty', x, y });
      ops.push({ op: 'dirty', x: x + 159, y: y + 31 });

      token = COM_Parse(s);
      value = atoi(token);
      if (value >= MAX_CLIENTS || value < 0) {
        ops.push({ op: 'error', msg: 'client >= MAX_CLIENTS' });
        return ops;
      }

      token = COM_Parse(s);
      const score = atoi(token);
      token = COM_Parse(s);
      const ping = atoi(token);
      token = COM_Parse(s);
      const time = atoi(token);

      opDrawAltString(ops, x + 32, y, env.clientName(value));
      opDrawString(ops, x + 32, y + 8, 'Score: ');
      opDrawAltString(ops, x + 32 + 7 * 8, y + 8, sprintf('%i', score));
      opDrawString(ops, x + 32, y + 16, sprintf('Ping:  %i', ping));
      opDrawString(ops, x + 32, y + 24, sprintf('Time:  %i', time));

      ops.push({ op: 'pic', x, y, name: env.clientIcon(value) });
      continue;
    }

    if (token === 'ctf') {
      // draw a ctf client block
      token = COM_Parse(s);
      x = halfW - 160 + atoi(token);
      token = COM_Parse(s);
      y = halfH - 120 + atoi(token);
      ops.push({ op: 'dirty', x, y });
      ops.push({ op: 'dirty', x: x + 159, y: y + 31 });

      token = COM_Parse(s);
      value = atoi(token);
      if (value >= MAX_CLIENTS || value < 0) {
        ops.push({ op: 'error', msg: 'client >= MAX_CLIENTS' });
        return ops;
      }

      token = COM_Parse(s);
      const score = atoi(token);
      token = COM_Parse(s);
      let ping = atoi(token);
      if (ping > 999) ping = 999;

      const block = sprintf('%3d %3d %-12.12s', score, ping, env.clientName(value)).slice(0, 79);

      if (value === env.playernum) opDrawAltString(ops, x, y, block);
      else opDrawString(ops, x, y, block);
      continue;
    }

    if (token === 'picn') {
      // draw a pic from a name
      token = COM_Parse(s);
      ops.push({ op: 'dirty', x, y });
      ops.push({ op: 'dirty', x: x + 23, y: y + 23 });
      ops.push({ op: 'pic', x, y, name: token });
      continue;
    }

    if (token === 'num') {
      // draw a number
      token = COM_Parse(s);
      width = atoi(token);
      token = COM_Parse(s);
      value = stat(env, atoi(token));
      opDrawField(ops, x, y, 0, width, value);
      continue;
    }

    if (token === 'hnum') {
      // health number
      let color: number;
      width = 3;
      value = stat(env, STAT_HEALTH);
      if (value > 25)
        color = 0; // green
      else if (value > 0)
        color = (env.serverframe >> 2) & 1; // flash
      else color = 1;

      if (stat(env, STAT_FLASHES) & 1) ops.push({ op: 'pic', x, y, name: 'field_3' });

      opDrawField(ops, x, y, color, width, value);
      continue;
    }

    if (token === 'anum') {
      // ammo number
      let color: number;
      width = 3;
      value = stat(env, STAT_AMMO);
      if (value > 5)
        color = 0; // green
      else if (value >= 0)
        color = (env.serverframe >> 2) & 1; // flash
      else continue; // negative number = don't show

      if (stat(env, STAT_FLASHES) & 4) ops.push({ op: 'pic', x, y, name: 'field_3' });

      opDrawField(ops, x, y, color, width, value);
      continue;
    }

    if (token === 'rnum') {
      // armor number
      width = 3;
      value = stat(env, STAT_ARMOR);
      if (value < 1) continue;

      const color = 0; // green

      if (stat(env, STAT_FLASHES) & 2) ops.push({ op: 'pic', x, y, name: 'field_3' });

      opDrawField(ops, x, y, color, width, value);
      continue;
    }

    if (token === 'stat_string') {
      token = COM_Parse(s);
      let index = atoi(token);
      if (index < 0 || index >= MAX_CONFIGSTRINGS) {
        ops.push({ op: 'error', msg: 'Bad stat_string index' });
        return ops;
      }
      index = stat(env, index);
      if (index < 0 || index >= MAX_CONFIGSTRINGS) {
        ops.push({ op: 'error', msg: 'Bad stat_string index' });
        return ops;
      }
      opDrawString(ops, x, y, cs(env, index));
      continue;
    }

    if (token === 'cstring') {
      token = COM_Parse(s);
      opDrawHUDString(ops, token, x, y, 320, 0);
      continue;
    }

    if (token === 'string') {
      token = COM_Parse(s);
      opDrawString(ops, x, y, token);
      continue;
    }

    if (token === 'cstring2') {
      token = COM_Parse(s);
      opDrawHUDString(ops, token, x, y, 320, 0x80);
      continue;
    }

    if (token === 'string2') {
      token = COM_Parse(s);
      opDrawAltString(ops, x, y, token);
      continue;
    }

    if (token === 'if') {
      // draw a number
      token = COM_Parse(s);
      value = stat(env, atoi(token));
      if (!value) {
        // skip to endif
        while (s.pos >= 0 && token !== 'endif') token = COM_Parse(s);
      }
      continue;
    }
  }
  return ops;
}
