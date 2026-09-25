import { describe, expect, it } from 'vitest';
import { CS_ITEMS, MAX_ITEMS, STAT_SELECTED_ITEM, CS_STATUSBAR } from 'q2-shared';
import { MSG_WriteShort, SizeBuf } from 'q2-protocol';
import { ClientContext, ca_active } from '../src/client';
import { NullSound } from '../src/sound';
import { NullCinematics } from '../src/cinematic';
import type { Effects } from '../src/effects';
import { MemoryTransport } from '../src/transport';
import {
  SCR_CalcVrect,
  SCR_CenterPrint,
  SCR_CheckDrawCenterString,
  SCR_DrawField,
  SCR_DrawStats,
  SCR_Init,
  SCR_TileClear,
  SCR_TouchPics,
  CL_AddNetgraph,
} from '../src/cl_scrn';
import { CL_DrawInventory, CL_ParseInventory } from '../src/cl_inv';
import { charsToText, createFakeRefresh } from './layout_helpers';

function makeCtx(picSizes: Record<string, [number, number]> = {}) {
  const re = createFakeRefresh(picSizes);
  const c = new ClientContext({
    refresh: re,
    transport: () => new MemoryTransport(),
    loadFile: async () => null,
    sound: new NullSound(),
    effects: {} as Effects,
    cinematics: new NullCinematics(),
  });
  const printed: string[] = [];
  c.main.printHook = (m) => printed.push(m);
  SCR_Init(c);
  c.cv.crosshair = c.cvars.get('crosshair', '0', 1);
  return { c, re, printed };
}

describe('cl_scrn', () => {
  it('SCR_CalcVrect for viewsize values', () => {
    const { c } = makeCtx();
    const res: Record<string, number[]> = {};
    for (const vs of ['100', '90', '75', '40', '30', '150']) {
      c.cvars.set('viewsize', vs);
      SCR_CalcVrect(c);
      const r = c.scr.scr_vrect;
      res[vs] = [r.x, r.y, r.width, r.height];
    }
    expect(res).toEqual({
      '100': [0, 0, 640, 480],
      '90': [32, 24, 576, 432],
      '75': [80, 60, 480, 360],
      '40': [192, 144, 256, 192],
      '30': [192, 144, 256, 192], // clamped to 40
      '150': [0, 0, 640, 480], // clamped to 100
    });
    expect(c.cvars.variableString('viewsize')).toBe('100');
  });

  it('SCR_DrawField draws right-aligned digit pics', () => {
    const { c, re } = makeCtx();
    SCR_DrawField(c, 100, 200, 0, 3, -7);
    expect(re.calls).toEqual([
      ['pic', 118, 200, 'num_minus'],
      ['pic', 134, 200, 'num_7'],
    ]);
    expect(c.scr.scr_dirty.x2).toBe(150);
  });

  it('SCR_CenterPrint: console echo centring, line count and timing', () => {
    const { c, re, printed } = makeCtx();
    c.cl.time = 1000;
    SCR_CenterPrint(c, 'You found a secret area!\nsecond');
    expect(c.scr.scr_center_lines).toBe(2);
    expect(c.scr.scr_centertime_off).toBeCloseTo(2.5);
    expect(c.scr.scr_centertime_start).toBe(1000);
    const lines = printed.join('');
    expect(lines).toContain('\n\n\x1d' + '\x1e'.repeat(35) + '\x1f\n\n');
    expect(lines).toContain('        You found a secret area!\n');
    expect(lines).toContain('                 second\n');

    c.cls.frametime = 1.0;
    SCR_CheckDrawCenterString(c);
    // 480*0.35 = 168; line 1 is 24 chars → x = (640-192)/2
    expect(charsToText(re.calls)).toEqual(['224,168:You found a secret area!', '296,176:second']);
    re.calls.length = 0;
    c.cls.frametime = 2.0;
    SCR_CheckDrawCenterString(c);
    expect(re.calls).toEqual([]);
  });

  it('SCR_DrawStats runs the statusbar through the refresh only when active + prepped', () => {
    const { c, re } = makeCtx();
    c.cl.configstrings[CS_STATUSBAR] = 'xv 0 yb -24 string "hi"';
    SCR_DrawStats(c);
    expect(re.calls).toEqual([]);
    c.cls.state = ca_active;
    c.cl.refresh_prepped = true;
    SCR_DrawStats(c);
    expect(charsToText(re.calls)).toEqual(['160,456:hi']);
  });

  it('SCR_TileClear clears the border around a reduced view', () => {
    const { c, re } = makeCtx();
    c.cvars.set('viewsize', '50');
    SCR_CalcVrect(c);
    // first frame: dirty rect from a previous full-screen draw
    c.scr.scr_dirty.x1 = 0;
    c.scr.scr_dirty.y1 = 0;
    c.scr.scr_dirty.x2 = 639;
    c.scr.scr_dirty.y2 = 479;
    SCR_TileClear(c);
    expect(re.calls).toEqual([
      ['tileclear', 0, 0, 640, 120, 'backtile'],
      ['tileclear', 0, 360, 640, 120, 'backtile'],
      ['tileclear', 0, 120, 160, 240, 'backtile'],
      ['tileclear', 480, 120, 160, 240, 'backtile'],
    ]);
  });

  it('SCR_TouchPics registers sb_nums and the crosshair', async () => {
    const { c, re } = makeCtx({ ch2: [8, 8] });
    c.cvars.set('crosshair', '2');
    await SCR_TouchPics(c);
    expect(re.calls.filter((x) => x[0] === 'registerPic').length).toBe(23);
    expect(c.scr.crosshair_pic).toBe('ch2');
    expect(c.scr.crosshair_width).toBe(8);
    c.cvars.set('crosshair', '7');
    await SCR_TouchPics(c);
    expect(c.cv.crosshair.value).toBe(3);
    expect(c.scr.crosshair_pic).toBe(''); // ch3 has no size in the fake
  });

  it('CL_AddNetgraph adds dropped / suppressed / ping samples', () => {
    const { c } = makeCtx();
    c.cls.netchan.dropped = 2;
    c.cl.surpressCount = 1;
    c.cls.netchan.incoming_acknowledged = 65;
    c.cls.realtime = 1000;
    c.cl.cmd_time[1] = 100;
    CL_AddNetgraph(c);
    expect(c.scr.current).toBe(4);
    expect(c.scr.values.slice(0, 4).map((v) => [v.value, v.color])).toEqual([
      [30, 0x40],
      [30, 0x40],
      [30, 0xdf],
      [30, 0xd0],
    ]);
  });
});

describe('cl_inv', () => {
  it('CL_ParseInventory + CL_DrawInventory', () => {
    const { c, re } = makeCtx();
    const buf = new SizeBuf(2 * MAX_ITEMS);
    for (let i = 0; i < MAX_ITEMS; i++) MSG_WriteShort(buf, i === 7 ? 50 : i === 9 ? 3 : 0);
    c.net_message.data.set(buf.written());
    c.net_message.cursize = buf.cursize;
    c.net_message.readcount = 0;
    CL_ParseInventory(c);
    expect(c.cl.inventory[7]).toBe(50);
    expect(c.cl.inventory[9]).toBe(3);

    c.cl.configstrings[CS_ITEMS + 7] = 'Bullets';
    c.cl.configstrings[CS_ITEMS + 9] = 'Grenades';
    c.cl.frame.playerstate.stats[STAT_SELECTED_ITEM] = 9;
    c.keys.keybindings[103] = 'use grenades'; // 'g', case-insensitive match
    c.cls.realtime = 100; // (int)(1000) & 1 == 0 → no cursor
    CL_DrawInventory(c);
    expect(charsToText(re.calls)).toEqual([
      'pic,192,128,inventory',
      '216,144:hotkey ### item',
      '216,152:------ --- ----',
      '216,160:~ ~ ~ ~ ~ ~ ~ ~ ~5~0~ ~B~u~l~l~e~t~s',
      '216,168:     g   3 Grenades',
    ]);
  });
});
