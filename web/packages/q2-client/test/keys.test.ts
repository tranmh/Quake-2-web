import { describe, expect, it } from 'vitest';
import {
  K_BACKSPACE,
  K_CTRL,
  K_DOWNARROW,
  K_ENTER,
  K_ESCAPE,
  K_F1,
  K_F12,
  K_INS,
  K_KP_5,
  K_MOUSE1,
  K_MWHEELUP,
  K_PGUP,
  K_SHIFT,
  K_TAB,
  K_UPARROW,
  STAT_LAYOUTS,
} from 'q2-shared';
import { ca_active, ca_connected, key_console, key_game, key_menu, key_message } from '../src/client';
import { Con_Init } from '../src/console';
import {
  Key_ChatBuffer,
  Key_ClearStates,
  Key_EditLine,
  Key_Event,
  Key_Init,
  Key_KeynumToString,
  Key_StringToKeynum,
  Key_WriteBindings,
} from '../src/keys';
import { capturePrints, createTestContext } from './helpers';

function setup() {
  const c = createTestContext();
  Con_Init(c);
  Key_Init(c);
  const out = capturePrints(c);
  return { c, out };
}

const ch = (s: string) => s.charCodeAt(0);
function typeText(c: ReturnType<typeof createTestContext>, s: string) {
  for (const x of s) {
    Key_Event(c, ch(x), true, 0);
    Key_Event(c, ch(x), false, 0);
  }
}

describe('keys.c', () => {
  it('keynum <-> string', () => {
    expect(Key_StringToKeynum('a')).toBe(97);
    expect(Key_StringToKeynum('SEMICOLON')).toBe(59);
    expect(Key_StringToKeynum('f12')).toBe(K_F12);
    expect(Key_StringToKeynum('mouse1')).toBe(K_MOUSE1);
    expect(Key_StringToKeynum('nosuchkey')).toBe(-1);
    expect(Key_StringToKeynum('')).toBe(-1);
    expect(Key_KeynumToString(59)).toBe(';');
    expect(Key_KeynumToString(32)).toBe('SPACE');
    expect(Key_KeynumToString(K_F1)).toBe('F1');
    expect(Key_KeynumToString(K_KP_5)).toBe('KP_5');
    expect(Key_KeynumToString(-1)).toBe('<KEY NOT FOUND>');
    expect(Key_KeynumToString(250)).toBe('<UNKNOWN KEYNUM>');
    for (const k of [K_F1, K_MWHEELUP, K_UPARROW, 97, 126])
      expect(Key_StringToKeynum(Key_KeynumToString(k))).toBe(k);
  });

  it('bind / unbind / unbindall / bindlist / writeBindings', () => {
    const { c, out } = setup();
    const cfg: string[] = [];
    c.host.onConfig = (kind, name, value) => cfg.push(`${kind}:${name}=${value}`);
    c.cmd.executeString('bind w +forward');
    c.cmd.executeString('bind SEMICOLON "say hi"');
    c.cmd.executeString('bind mouse1 +attack');
    expect(c.keys.keybindings[119]).toBe('+forward');
    expect(c.keys.keybindings[59]).toBe('say hi');
    expect(cfg).toContain('bind:w=+forward');
    out.length = 0;
    c.cmd.executeString('bind w');
    expect(out).toEqual(['"w" = "+forward"\n']);
    out.length = 0;
    c.cmd.executeString('bind x');
    expect(out).toEqual(['"x" is not bound\n']);
    out.length = 0;
    c.cmd.executeString('bind nokey foo');
    expect(out).toEqual(['"nokey" isn\'t a valid key\n']);
    // (deviation: C writes `bind ; "say hi"`, which does not parse back -- docs/review/04-ts-engine.md)
    expect(Key_WriteBindings(c)).toBe('bind SEMICOLON "say hi"\nbind w "+forward"\nbind MOUSE1 "+attack"\n');
    out.length = 0;
    c.cmd.executeString('bindlist');
    expect(out).toEqual(['; "say hi"\n', 'w "+forward"\n', 'MOUSE1 "+attack"\n']);
    c.cmd.executeString('unbind w');
    expect(c.keys.keybindings[119]).toBe('');
    c.cmd.executeString('unbindall');
    expect(Key_WriteBindings(c)).toBe('');
  });

  it('key_game: +commands carry keynum and time; key up sends -command', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    c.cls.key_dest = key_game;
    c.cmd.executeString('bind w +forward');
    c.cmd.executeString('bind e "use blaster"');
    Key_Event(c, 119, true, 1234);
    Key_Event(c, 119, true, 1250); // autorepeat ignored
    Key_Event(c, 119, false, 1300);
    Key_Event(c, 101, true, 1400);
    expect(c.cmd.text).toBe('+forward 119 1234\n-forward 119 1300\nuse blaster\n');
    expect(c.keys.anykeydown).toBe(1);
  });

  it('unbound mouse keys print a hint', () => {
    const { c, out } = setup();
    c.cls.state = ca_active;
    Key_Event(c, K_MOUSE1, true, 0);
    expect(out).toContain('MOUSE1 is unbound, hit F4 to set.\n');
  });

  it('key_console: typing, backspace (cursor), enter executes, history', () => {
    const { c, out } = setup();
    c.cls.state = ca_active;
    c.cls.key_dest = key_console;
    typeText(c, 'echo hi');
    expect(Key_EditLine(c)).toBe(']echo hi');
    Key_Event(c, K_ENTER, true, 0);
    Key_Event(c, K_ENTER, false, 0);
    expect(c.cmd.text).toBe('echo hi\n');
    expect(out).toContain(']echo hi\n');
    expect(c.keys.edit_line).toBe(1);
    expect(Key_EditLine(c)).toBe(']');
    // leading slash is stripped
    typeText(c, '/foo');
    Key_Event(c, K_ENTER, true, 0);
    expect(c.cmd.text).toBe('echo hi\nfoo\n');
    // history
    Key_Event(c, K_UPARROW, true, 0);
    Key_Event(c, K_UPARROW, false, 0);
    expect(Key_EditLine(c)).toBe(']/foo');
    Key_Event(c, K_UPARROW, true, 0);
    Key_Event(c, K_UPARROW, false, 0);
    expect(Key_EditLine(c)).toBe(']echo hi');
    Key_Event(c, K_DOWNARROW, true, 0);
    Key_Event(c, K_DOWNARROW, false, 0);
    expect(Key_EditLine(c)).toBe(']/foo');
    Key_Event(c, K_DOWNARROW, true, 0);
    Key_Event(c, K_DOWNARROW, false, 0);
    expect(c.keys.key_linepos).toBe(1);
    // backspace only moves the cursor (Con_DrawInput truncates the line at the cursor when drawn)
    typeText(c, 'ab');
    Key_Event(c, K_BACKSPACE, true, 0);
    expect(c.keys.key_linepos).toBe(2);
  });

  it('shift maps through keyshift in the console', () => {
    const { c } = setup();
    c.cls.state = ca_connected;
    c.cls.key_dest = key_console;
    Key_Event(c, K_SHIFT, true, 0);
    Key_Event(c, ch('a'), true, 0);
    Key_Event(c, ch('1'), true, 0);
    Key_Event(c, K_SHIFT, false, 0);
    expect(Key_EditLine(c)).toBe(']A!');
  });

  it('tab completion of commands and cvars', () => {
    const { c } = setup();
    c.cls.key_dest = key_console;
    c.cvars.get('cl_forwardspeed', '200', 0);
    typeText(c, 'bindl');
    Key_Event(c, K_TAB, true, 0);
    expect(Key_EditLine(c)).toBe(']/bindlist ');
    Key_Event(c, K_TAB, false, 0);
    c.keys.key_lines[c.keys.edit_line]![1] = 0;
    c.keys.key_linepos = 1;
    typeText(c, '\\cl_forw');
    Key_Event(c, K_TAB, true, 0);
    expect(Key_EditLine(c)).toBe(']/cl_forwardspeed ');
  });

  it('paste with ctrl-v and shift-ins', () => {
    const { c } = setup();
    c.cls.key_dest = key_console;
    c.host.getClipboardData = () => 'map demo1\nignored';
    Key_Event(c, K_CTRL, true, 0);
    Key_Event(c, ch('v'), true, 0);
    Key_Event(c, K_CTRL, false, 0);
    expect(Key_EditLine(c)).toBe(']map demo1');
    Key_Event(c, K_SHIFT, true, 0);
    Key_Event(c, K_INS, true, 0);
    expect(Key_EditLine(c)).toBe(']map demo1map demo1');
  });

  it('pgup scrolls the console', () => {
    const { c } = setup();
    c.cls.key_dest = key_console;
    const d = c.con.display;
    Key_Event(c, K_PGUP, true, 0);
    expect(c.con.display).toBe(d - 2);
  });

  it('key_message: chat to say / say_team; escape cancels', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    c.cmd.executeString('messagemode');
    expect(c.cls.key_dest).toBe(key_message);
    typeText(c, 'gg');
    expect(Key_ChatBuffer(c)).toBe('gg');
    Key_Event(c, K_ENTER, true, 0);
    Key_Event(c, K_ENTER, false, 0);
    expect(c.cmd.text).toBe('say "gg"\n');
    expect(c.cls.key_dest).toBe(key_game);
    c.cmd.executeString('messagemode2');
    typeText(c, 'x');
    Key_Event(c, K_ENTER, true, 0);
    expect(c.cmd.text).toBe('say "gg"\nsay_team "x"\n');
    c.cmd.executeString('messagemode');
    typeText(c, 'abc');
    Key_Event(c, K_ESCAPE, true, 0);
    expect(c.cls.key_dest).toBe(key_game);
    expect(Key_ChatBuffer(c)).toBe('');
  });

  it('escape: opens the menu, closes layouts, goes to the menu handler in key_menu', () => {
    const { c } = setup();
    const menu: string[] = [];
    c.host.onMenu = (a, k) => menu.push(a + (k !== undefined ? k : ''));
    c.cls.state = ca_active;
    c.cls.key_dest = key_game;
    Key_Event(c, K_ESCAPE, true, 0);
    expect(c.cls.key_dest).toBe(key_menu);
    expect(menu).toEqual(['main']);
    Key_Event(c, K_ESCAPE, false, 0);
    Key_Event(c, K_ESCAPE, true, 0);
    expect(menu).toEqual(['main', 'key27']);
    c.cls.key_dest = key_game;
    c.cl.frame.playerstate.stats[STAT_LAYOUTS] = 1;
    Key_Event(c, K_ESCAPE, false, 0);
    Key_Event(c, K_ESCAPE, true, 0);
    expect(c.cmd.text).toBe('cmd putaway\n');
  });

  it('console key toggles the console (and d1 when disconnected)', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    c.cls.key_dest = key_game;
    Key_Event(c, ch('`'), true, 0);
    expect(c.cls.key_dest).toBe(key_console);
    Key_Event(c, ch('`'), false, 0);
    Key_Event(c, ch('`'), true, 0);
    expect(c.cls.key_dest).toBe(key_game);
    c.cls.state = 1; // ca_disconnected
    Key_Event(c, ch('`'), false, 0);
    Key_Event(c, ch('`'), true, 0);
    expect(c.cmd.text).toBe('d1\n');
  });

  it('console keys are not bindings in key_console; F keys still are', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    c.cls.key_dest = key_console;
    c.cmd.executeString('bind w +forward');
    c.cmd.executeString('bind F1 help');
    Key_Event(c, ch('w'), true, 5);
    expect(c.cmd.text).toBe('');
    Key_Event(c, K_F1, true, 5);
    expect(c.cmd.text).toBe('help\n');
    // key up still sends the -command (keeps a button from sticking)
    Key_Event(c, ch('w'), false, 6);
    expect(c.cmd.text).toBe('help\n-forward 119 6\n');
  });

  it('Key_ClearStates releases held buttons', () => {
    const { c } = setup();
    c.cls.state = ca_active;
    c.cmd.executeString('bind w +forward');
    Key_Event(c, ch('w'), true, 10);
    Key_ClearStates(c);
    expect(c.cmd.text).toBe('+forward 119 10\n-forward 119 0\n');
    expect(c.keys.keydown[119]).toBe(false);
    expect(c.keys.anykeydown).toBe(0);
  });
});
