// Port of client/cl_inv.c -- client inventory screen
import { CS_ITEMS, MAX_ITEMS, Q_stricmp, STAT_SELECTED_ITEM, cInt, sprintf } from 'q2-shared';
import { MSG_ReadShort } from 'q2-protocol';
import type { ClientContext } from './client';
import { SCR_DirtyScreen } from './cl_scrn';
import { Key_KeynumToString } from './keys';

// C: cl_inv.c:29 CL_ParseInventory
export function CL_ParseInventory(c: ClientContext): void {
  for (let i = 0; i < MAX_ITEMS; i++) c.cl.inventory[i] = MSG_ReadShort(c.net_message);
}

// C: cl_inv.c:43 Inv_DrawString
export function Inv_DrawString(c: ClientContext, x: number, y: number, string: string): void {
  for (let i = 0; i < string.length; i++) {
    c.re.drawChar(x, y, string.charCodeAt(i) & 255);
    x += 8;
  }
}

// C: cl_inv.c:53 SetStringHighBit (returns the new string)
export function SetStringHighBit(s: string): string {
  let out = '';
  for (let i = 0; i < s.length; i++) out += String.fromCharCode((s.charCodeAt(i) | 128) & 255);
  return out;
}

const DISPLAY_ITEMS = 17;

// C: cl_inv.c:66 CL_DrawInventory
export function CL_DrawInventory(c: ClientContext): void {
  const { cl, cls } = c;
  const index = new Int32Array(MAX_ITEMS);

  const selected = cl.frame.playerstate.stats[STAT_SELECTED_ITEM]!;

  let num = 0;
  let selected_num = 0;
  for (let i = 0; i < MAX_ITEMS; i++) {
    if (i === selected) selected_num = num;
    if (cl.inventory[i]) {
      index[num] = i;
      num++;
    }
  }

  // determine scroll point
  let top = selected_num - Math.trunc(DISPLAY_ITEMS / 2);
  if (num - top < DISPLAY_ITEMS) top = num - DISPLAY_ITEMS;
  if (top < 0) top = 0;

  let x = Math.trunc((c.viddef.width - 256) / 2);
  let y = Math.trunc((c.viddef.height - 240) / 2);

  // repaint everything next frame
  SCR_DirtyScreen(c);

  c.re.drawPic(x, y + 8, 'inventory');

  y += 24;
  x += 24;
  Inv_DrawString(c, x, y, 'hotkey ### item');
  Inv_DrawString(c, x, y + 8, '------ --- ----');
  y += 16;
  const keybindings = c.keys.keybindings;
  for (let i = top; i < num && i < top + DISPLAY_ITEMS; i++) {
    const item = index[i]!;
    // search for a binding
    const binding = sprintf('use %s', cl.configstrings[CS_ITEMS + item]!);
    let bind = '';
    for (let j = 0; j < 256; j++) {
      const kb = keybindings[j];
      if (kb && !Q_stricmp(kb, binding)) {
        bind = Key_KeynumToString(j);
        break;
      }
    }

    let string = sprintf('%6s %3i %s', bind, cl.inventory[item]!, cl.configstrings[CS_ITEMS + item]!).slice(
      0,
      1023,
    );
    if (item !== selected) string = SetStringHighBit(string);
    // draw a blinky cursor by the selected item
    else if (cInt(cls.realtime * 10) & 1) c.re.drawChar(x - 8, y, 15);
    Inv_DrawString(c, x, y, string);
    y += 8;
  }
}
