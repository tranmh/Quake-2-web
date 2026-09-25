import { describe, expect, it } from 'vitest';
import { CVAR_ARCHIVE, CVAR_LATCH, CVAR_NOSET, CVAR_SERVERINFO, CVAR_USERINFO } from 'q2-shared';
import { CvarSystem } from '../src/cvar';
import { CmdSystem } from '../src/cmd';

function setup() {
  const cvars = new CvarSystem();
  const out: string[] = [];
  cvars.print = (s) => out.push(s);
  const cmd = new CmdSystem(cvars);
  cmd.print = (s) => out.push(s);
  return { cvars, cmd, out };
}

describe('cvar.c', () => {
  it('Cvar_Get creates once, ORs flags, keeps value', () => {
    const { cvars } = setup();
    const v = cvars.get('foo', '1.5', 0);
    expect(v.value).toBe(1.5);
    expect(v.modified).toBe(true);
    const v2 = cvars.get('foo', '9', CVAR_ARCHIVE);
    expect(v2).toBe(v);
    expect(v.string).toBe('1.5');
    expect(v.flags).toBe(CVAR_ARCHIVE);
    expect(cvars.getOrNull('nope', null, 0)).toBeNull();
  });

  it('rejects invalid info names/values', () => {
    const { cvars, out } = setup();
    expect(cvars.getOrNull('a;b', 'x', CVAR_USERINFO)).toBeNull();
    expect(cvars.getOrNull('ab', 'x"', CVAR_USERINFO)).toBeNull();
    expect(out).toEqual(['invalid info cvar name\n', 'invalid info cvar value\n']);
  });

  it('value is float32 of atof', () => {
    const { cvars } = setup();
    expect(cvars.get('f', '0.1', 0).value).toBe(Math.fround(0.1));
    expect(cvars.get('g', '12abc', 0).value).toBe(12);
  });

  it('Cvar_Set: NOSET, userinfo_modified, unchanged', () => {
    const { cvars, out } = setup();
    cvars.get('ro', '1', CVAR_NOSET);
    cvars.set('ro', '2');
    expect(cvars.variableString('ro')).toBe('1');
    expect(out.pop()).toBe('ro is write protected.\n');
    cvars.forceSet('ro', '2');
    expect(cvars.variableString('ro')).toBe('2');

    const n = cvars.get('name', 'unnamed', CVAR_USERINFO);
    n.modified = false;
    cvars.set('name', 'unnamed');
    expect(cvars.userinfoModified).toBe(false);
    expect(n.modified).toBe(false);
    cvars.set('name', 'bob');
    expect(cvars.userinfoModified).toBe(true);
    expect(n.modified).toBe(true);
    cvars.set('name', 'a\\b');
    expect(n.string).toBe('bob');
  });

  it('Cvar_Set creates unknown variables with flags 0', () => {
    const { cvars } = setup();
    cvars.set('newvar', '5');
    expect(cvars.find('newvar')!.flags).toBe(0);
    expect(cvars.variableValue('newvar')).toBe(5);
    expect(cvars.variableValue('missing')).toBe(0);
    expect(cvars.variableString('missing')).toBe('');
  });

  it('latched vars', () => {
    const { cvars, out } = setup();
    const v = cvars.get('game', '', CVAR_LATCH | CVAR_SERVERINFO);
    const changed: string[] = [];
    cvars.onGameChanged = (g) => changed.push(g);
    cvars.set('game', 'ctf'); // no server running: applies immediately
    expect(v.string).toBe('ctf');
    expect(changed).toEqual(['ctf']);
    cvars.serverState = () => 1;
    cvars.set('game', 'rogue');
    expect(v.string).toBe('ctf');
    expect(v.latched_string).toBe('rogue');
    expect(out.pop()).toBe('game will be changed for next game.\n');
    cvars.getLatchedVars();
    expect(v.string).toBe('rogue');
    expect(v.latched_string).toBeNull();
    expect(changed).toEqual(['ctf', 'rogue']);
  });

  it('Cvar_FullSet replaces flags', () => {
    const { cvars } = setup();
    cvars.get('x', '1', CVAR_ARCHIVE);
    cvars.fullSet('x', '2', CVAR_USERINFO);
    expect(cvars.find('x')!.flags).toBe(CVAR_USERINFO);
    // C checks the *old* flags: x was not userinfo
    expect(cvars.userinfoModified).toBe(false);
    cvars.fullSet('x', '3', CVAR_USERINFO);
    expect(cvars.userinfoModified).toBe(true);
  });

  it('Cvar_SetValue formatting', () => {
    const { cvars } = setup();
    cvars.setValue('a', 3);
    expect(cvars.variableString('a')).toBe('3');
    cvars.setValue('a', 0.5);
    expect(cvars.variableString('a')).toBe('0.500000');
    cvars.setValue('a', -2);
    expect(cvars.variableString('a')).toBe('-2');
  });

  it('BitInfo / cvarlist order is newest first', () => {
    const { cvars, cmd, out } = setup();
    cvars.get('name', 'player', CVAR_USERINFO | CVAR_ARCHIVE);
    cvars.get('skin', 'male/grunt', CVAR_USERINFO);
    cvars.get('rate', '25000', CVAR_USERINFO);
    cvars.get('dm', '1', CVAR_SERVERINFO);
    expect(cvars.userinfo()).toBe('\\rate\\25000\\skin\\male/grunt\\name\\player');
    expect(cvars.serverinfo()).toBe('\\dm\\1');
    out.length = 0;
    cmd.executeString('cvarlist');
    const text = out.join('');
    expect(text.indexOf('dm')).toBeLessThan(text.indexOf('name'));
    expect(text).toContain('*U   name "player"\n');
    expect(text).toContain('  S  dm "1"\n');
    expect(text.endsWith('4 cvars\n')).toBe(true);
  });

  it('writeVariables lists archive cvars', () => {
    const { cvars } = setup();
    cvars.get('a', '1', CVAR_ARCHIVE);
    cvars.get('b', '2', 0);
    cvars.get('c', 'x y', CVAR_ARCHIVE);
    expect(cvars.writeVariables()).toBe('set c "x y"\nset a "1"\n');
  });

  it('console commands: set, set u/s, var print/set, seta/setu/sets, toggle', () => {
    const { cvars, cmd, out } = setup();
    cmd.executeString('set foo bar');
    expect(cvars.variableString('foo')).toBe('bar');
    cmd.executeString('set usr val u');
    expect(cvars.find('usr')!.flags).toBe(CVAR_USERINFO);
    cmd.executeString('set srv val s');
    expect(cvars.find('srv')!.flags).toBe(CVAR_SERVERINFO);
    out.length = 0;
    cmd.executeString('set x y z');
    expect(out).toEqual(["flags can only be 'u' or 's'\n"]);
    out.length = 0;
    cmd.executeString('set x');
    expect(out).toEqual(['usage: set <variable> <value> [u / s]\n']);
    out.length = 0;
    cmd.executeString('foo');
    expect(out).toEqual(['"foo" is "bar"\n']);
    cmd.executeString('foo baz');
    expect(cvars.variableString('foo')).toBe('baz');

    cmd.executeString('seta arch 1');
    expect(cvars.find('arch')!.flags & CVAR_ARCHIVE).toBeTruthy();
    cmd.executeString('seta foo 2');
    expect(cvars.find('foo')!.flags & CVAR_ARCHIVE).toBeTruthy();
    expect(cvars.variableString('foo')).toBe('2');
    cmd.executeString('setu uu 1');
    expect(cvars.find('uu')!.flags).toBe(CVAR_USERINFO);
    cmd.executeString('sets ss 1');
    expect(cvars.find('ss')!.flags).toBe(CVAR_SERVERINFO);

    cmd.executeString('toggle arch');
    expect(cvars.variableString('arch')).toBe('0');
    cmd.executeString('toggle arch');
    expect(cvars.variableString('arch')).toBe('1');
  });

  it('completeVariable exact then partial', () => {
    const { cvars } = setup();
    cvars.get('cl_run', '0', 0);
    cvars.get('cl_r', '0', 0);
    expect(cvars.completeVariable('cl_r')).toBe('cl_r');
    expect(cvars.completeVariable('cl_ru')).toBe('cl_run');
    expect(cvars.completeVariable('')).toBeNull();
  });
});
