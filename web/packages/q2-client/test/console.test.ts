import { describe, expect, it } from 'vitest';
import { ca_active, ca_connected, key_console, key_game, key_message } from '../src/client';
import {
  CON_TEXTSIZE,
  Con_CenteredPrint,
  Con_CheckResize,
  Con_Clear_f,
  Con_DrawConsole,
  Con_DrawNotify,
  Con_Init,
  Con_Print,
} from '../src/console';
import { Key_Init } from '../src/keys';
import { capturePrints, createRecordingRefresh, createTestContext, rowText } from './helpers';

function lineText(c: ReturnType<typeof createTestContext>, line: number, strip = false): string {
  const con = c.con;
  const start = (((line % con.totallines) + con.totallines) % con.totallines) * con.linewidth;
  let s = '';
  for (let i = 0; i < con.linewidth; i++)
    s += String.fromCharCode(strip ? con.text[start + i]! & 127 : con.text[start + i]!);
  return s.replace(/ +$/, '');
}

function setup(width = 320, height = 240) {
  const re = createRecordingRefresh();
  const c = createTestContext({ refresh: re });
  c.viddef.width = width;
  c.viddef.height = height;
  Con_Init(c);
  Key_Init(c);
  return { c, re };
}

describe('console.c', () => {
  it('Con_Init sizes the buffer from viddef and prints', () => {
    const { c } = setup(320);
    expect(c.con.linewidth).toBe(38);
    expect(c.con.totallines).toBe(Math.trunc(CON_TEXTSIZE / 38));
    expect(c.con.initialized).toBe(true);
    // "Console initialized." is printed before con.initialized is set -> dropped (C order)
    expect(lineText(c, c.con.current)).toBe('');
    expect(c.cv.con_notifytime.value).toBe(3);
  });

  it('Con_Print is a no-op before Con_Init', () => {
    const c = createTestContext();
    Con_Print(c, 'hello\n');
    expect(c.con.current).toBe(0);
  });

  it('prints, word-wraps at linewidth and handles CR', () => {
    const { c } = setup(320); // 38 columns
    Con_Print(c, 'hello world\n');
    expect(lineText(c, c.con.current)).toBe('hello world');
    const cur = c.con.current;
    Con_Print(c, 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbb\n'); // 30 + 1 + 12 -> wraps the word
    expect(lineText(c, cur + 1)).toBe('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
    expect(lineText(c, cur + 2)).toBe('bbbbbbbbbbbb');
    Con_Print(c, 'first\rsecond\n');
    expect(lineText(c, c.con.current)).toBe('second');
  });

  it('high-bit colour for text starting with \\x01 or \\x02', () => {
    const { c } = setup();
    Con_Print(c, '\x01green\n');
    expect(lineText(c, c.con.current, true)).toBe('green');
    const start = (c.con.current % c.con.totallines) * c.con.linewidth;
    expect(c.con.text[start]).toBe('g'.charCodeAt(0) | 128);
  });

  it('notify times track cls.realtime', () => {
    const { c } = setup();
    c.cls.realtime = 1234;
    Con_Print(c, 'x\n');
    expect(c.con.times[c.con.current % 4]).toBe(1234);
  });

  it('resize keeps the most recent lines', () => {
    const { c } = setup(320);
    Con_Print(c, 'line one\n');
    Con_Print(c, 'line two\n');
    c.viddef.width = 640;
    Con_CheckResize(c);
    expect(c.con.linewidth).toBe(78);
    // the linefeed after a trailing newline is lazy: current is still "line two"
    expect(lineText(c, c.con.current)).toBe('line two');
    expect(lineText(c, c.con.current - 1)).toBe('line one');
    expect(c.con.display).toBe(c.con.current);
  });

  it('Con_Clear_f and Con_CenteredPrint', () => {
    const { c } = setup(320);
    Con_Print(c, 'abc\n');
    Con_Clear_f(c);
    expect(lineText(c, c.con.current)).toBe('');
    Con_CenteredPrint(c, 'mid');
    expect(lineText(c, c.con.current)).toBe(' '.repeat(17) + 'mid');
  });

  it('condump returns the text', () => {
    const { c } = setup(320);
    const out = capturePrints(c);
    Con_Print(c, 'alpha\nbeta\n');
    c.cmd.executeString('condump test');
    expect(out).toContain('Dumped console text to baseq2/test.txt.\n');
    const dumps: string[] = [];
    c.host.onPrint = (s) => dumps.push(s);
    c.cmd.executeString('condump t2');
    expect(dumps[dumps.length - 1]!.startsWith('alpha\nbeta\n')).toBe(true);
  });

  it('Con_DrawConsole draws background, version, text and input line', () => {
    const { c, re } = setup(320, 240);
    c.cls.state = ca_connected;
    c.cls.key_dest = key_console;
    Con_Print(c, 'hello\n');
    re.calls.length = 0;
    c.cls.realtime = 0;
    Con_DrawConsole(c, 0.5);
    expect(re.calls[0]).toEqual({ op: 'stretchpic', x: 0, y: -120, w: 320, h: 240, name: 'conback' });
    expect(rowText(re.calls, 120 - 12, true)).toBe('v3.19');
    // rows are drawn from y = lines-30 upward; the linefeed for a trailing \n happens lazily, so current = 'hello'
    expect(rowText(re.calls, 120 - 30).trimEnd()).toBe('hello');
    // input line at vislines-22: "]" + cursor (char 10) + spaces
    const input = rowText(re.calls, 120 - 22);
    expect(input.charCodeAt(0)).toBe(93);
    expect(input.charCodeAt(1)).toBe(10);
    expect(c.con.vislines).toBe(120);
    // cursor removed from the edit line afterwards
    expect(c.keys.key_lines[0]![1]).toBe(0);
  });

  it('backscroll arrows', () => {
    const { c, re } = setup(320, 240);
    for (let i = 0; i < 20; i++) Con_Print(c, `l${i}\n`);
    c.con.display -= 2;
    re.calls.length = 0;
    Con_DrawConsole(c, 1);
    const arrows = rowText(re.calls, 240 - 30);
    expect(arrows).toBe('^'.repeat(10));
  });

  it('Con_DrawNotify draws recent lines and the chat prompt', () => {
    const { c, re } = setup(320, 240);
    c.cls.state = ca_active;
    c.cls.key_dest = key_game;
    c.cls.realtime = 1000;
    Con_Print(c, 'note\n');
    c.cls.realtime = 2000;
    re.calls.length = 0;
    Con_DrawNotify(c);
    // lines current-3..current with times: "note" line (time 1000) and the new empty line (time 0 until printed)
    expect(rowText(re.calls, 0).trimEnd()).toBe('note');
    c.cls.realtime = 1000 + 3001;
    re.calls.length = 0;
    Con_DrawNotify(c);
    expect(re.calls.length).toBe(0);

    c.cls.key_dest = key_message;
    c.keys.chat_team = true;
    c.keys.chat_buffer.set([104, 105, 0]);
    c.keys.chat_bufferlen = 2;
    re.calls.length = 0;
    Con_DrawNotify(c);
    const row = rowText(re.calls, 0);
    expect(row.slice(0, 9)).toBe('say_team:');
    expect(row.slice(9, 11)).toBe('hi');
  });
});
