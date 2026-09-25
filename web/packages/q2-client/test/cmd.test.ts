import { describe, expect, it } from 'vitest';
import { latin1ToBytes } from 'q2-shared';
import { CvarSystem } from '../src/cvar';
import { CmdSystem } from '../src/cmd';

function setup() {
  const cvars = new CvarSystem();
  const out: string[] = [];
  cvars.print = (s) => out.push(s);
  const cmd = new CmdSystem(cvars);
  cmd.print = (s) => out.push(s);
  const log: string[] = [];
  cmd.addCommand('log', () => log.push([...Array(cmd.argc()).keys()].map((i) => cmd.argv(i)).join('|')));
  const fwd: string[] = [];
  cmd.forwardToServer = () => fwd.push(cmd.argv(0) + ':' + cmd.args());
  return { cvars, cmd, out, log, fwd };
}

describe('cmd.c', () => {
  it('Cbuf splits on ; and newline, not inside quotes', () => {
    const { cmd, log } = setup();
    cmd.cbufAddText('log a;log "b;c" d\nlog e');
    cmd.cbufExecute();
    expect(log).toEqual(['log|a', 'log|b;c|d', 'log|e']);
    expect(cmd.text).toBe('');
  });

  it('tokenizer: args, quotes, comments, high-bit bytes as whitespace', () => {
    const { cmd } = setup();
    cmd.tokenizeString('  say "hello world"  foo  ', false);
    expect(cmd.argc()).toBe(3);
    expect(cmd.argv(1)).toBe('hello world');
    expect(cmd.args()).toBe('"hello world"  foo');
    expect(cmd.argv(5)).toBe('');
    cmd.tokenizeString('a // comment', false);
    expect(cmd.argc()).toBe(1);
    cmd.tokenizeString('a\xa0b', false);
    expect(cmd.argc()).toBe(2);
  });

  it('wait defers the rest of the buffer to the next execute', () => {
    const { cmd, log } = setup();
    cmd.cbufAddText('log 1;wait;log 2\n');
    cmd.cbufExecute();
    expect(log).toEqual(['log|1']);
    cmd.cbufExecute();
    expect(log).toEqual(['log|1', 'log|2']);
  });

  it('InsertText runs before remaining text', () => {
    const { cmd, log } = setup();
    cmd.addCommand('ins', () => cmd.cbufInsertText('log inserted\n'));
    cmd.cbufAddText('ins;log after\n');
    cmd.cbufExecute();
    expect(log).toEqual(['log|inserted', 'log|after']);
  });

  it('overflow is rejected', () => {
    const { cmd, out } = setup();
    cmd.cbufAddText('x'.repeat(8191));
    cmd.cbufAddText('y');
    expect(out).toContain('Cbuf_AddText: overflow\n');
    expect(cmd.text.length).toBe(8191);
  });

  it('alias (list, reuse, loop count)', () => {
    const { cmd, log, out } = setup();
    cmd.executeString('alias go "log x ; log y"');
    expect(cmd.aliasValue('go')).toBe('log x ; log y\n');
    cmd.cbufAddText('go\n');
    cmd.cbufExecute();
    expect(log).toEqual(['log|x', 'log|y']);
    cmd.executeString('alias loop loop');
    cmd.cbufAddText('loop\n');
    cmd.cbufExecute();
    expect(out).toContain('ALIAS_LOOP_COUNT\n');
    out.length = 0;
    cmd.executeString('alias');
    expect(out[0]).toBe('Current alias commands:\n');
    expect(out).toContain('loop : loop\n\n');
    out.length = 0;
    cmd.executeString('alias ' + 'a'.repeat(32) + ' x');
    expect(out).toEqual(['Alias name is too long\n']);
  });

  it('macro expansion outside quotes only', () => {
    const { cmd, cvars, log, out } = setup();
    cvars.set('who', 'world');
    cmd.executeString('log $who "$who"');
    expect(log).toEqual(['log|world|$who']);
    cmd.executeString('log "unterminated');
    expect(out).toContain('Line has unmatched quote, discarded.\n');
    cvars.set('rec', '$rec');
    cmd.executeString('log $rec');
    expect(out).toContain('Macro expansion loop, discarded.\n');
  });

  it('echo', () => {
    const { cmd, out } = setup();
    cmd.executeString('echo hi there');
    expect(out.join('')).toBe('hi there \n');
  });

  it('unknown commands and NULL-function commands are forwarded', () => {
    const { cmd, fwd } = setup();
    cmd.executeString('god');
    expect(fwd).toEqual(['god:']);
    cmd.addCommand('kill', null);
    let got = '';
    cmd.addCommand('cmd', () => (got = cmd.args()));
    cmd.executeString('kill now');
    expect(got).toBe('kill now');
  });

  it('command names are case-insensitive; addCommand refuses dups and cvars', () => {
    const { cmd, cvars, log, out } = setup();
    cmd.executeString('LOG a');
    expect(log).toEqual(['LOG|a']);
    cmd.addCommand('log', () => {});
    expect(out).toContain('Cmd_AddCommand: log already defined\n');
    cvars.set('somevar', '1');
    cmd.addCommand('somevar', () => {});
    expect(out).toContain('Cmd_AddCommand: somevar already defined as a var\n');
    cmd.removeCommand('log');
    expect(cmd.exists('log')).toBe(false);
  });

  it('async exec preserves ordering', async () => {
    const { cmd, log, out } = setup();
    const files: Record<string, string> = {
      'a.cfg': 'log a1\nlog a2\n',
      'b.cfg': 'log b1\nexec a.cfg\nlog b2',
    };
    cmd.loadFile = async (n) => {
      await new Promise((r) => setTimeout(r, 1));
      return files[n] !== undefined ? latin1ToBytes(files[n]!) : null;
    };
    cmd.cbufAddText('log start;exec b.cfg;log end;exec missing.cfg\n');
    cmd.cbufExecute();
    expect(log).toEqual(['log|start']);
    await cmd.whenIdle();
    expect(log).toEqual(['log|start', 'log|b1', 'log|a1', 'log|a2', 'log|b2log|end']);
    // (sic) Cbuf_InsertText adds no newline: a file without a trailing newline runs into the next command
    expect(out).toContain('execing b.cfg\n');
    expect(out).toContain("couldn't exec missing.cfg\n");
  });

  it('completeCommand prefers exact, then commands, then aliases', () => {
    const { cmd } = setup();
    cmd.executeString('alias logalias x');
    expect(cmd.completeCommand('log')).toBe('log');
    expect(cmd.completeCommand('loga')).toBe('logalias');
    expect(cmd.completeCommand('ech')).toBe('echo');
  });

  it('AddEarly/LateCommands', () => {
    const { cmd } = setup();
    cmd.cbufAddEarlyCommands(['q2', '+set', 'a', '1', '+map', 'x']);
    expect(cmd.text).toBe('set a 1\n');
    cmd.cbufExecute();
    expect(cmd.cbufAddLateCommands(['q2', '+map', 'demo1', '+set', 'b', '2'])).toBe(true);
    expect(cmd.text).toBe('map demo1 \nset b 2\n');
  });
});
