import { describe, expect, it } from 'vitest';
import {
  COM_DefaultExtension,
  COM_FileBase,
  COM_FileExtension,
  COM_FilePath,
  COM_Parse,
  COM_ParseAll,
  COM_SkipPath,
  COM_StripExtension,
  Info_RemoveKey,
  Info_SetValueForKey,
  Info_Validate,
  Info_ValueForKey,
  Q_strcasecmp,
  Q_stricmp,
  Q_strncasecmp,
  atof,
  atoi,
  parseCursor,
} from '../src';

describe('COM_Parse', () => {
  it('tokenizes words, quotes and comments', () => {
    expect(COM_ParseAll('{ "classname" "worldspawn"\n// comment "x"\n"message" "a b"\n}')).toEqual([
      '{',
      'classname',
      'worldspawn',
      'message',
      'a b',
      '}',
    ]);
  });

  it('returns NULL cursor at end', () => {
    const p = parseCursor('  abc   ');
    expect(COM_Parse(p)).toBe('abc');
    expect(p.pos).toBe(5);
    expect(COM_Parse(p)).toBe('');
    expect(p.pos).toBe(-1);
    expect(COM_Parse(p)).toBe('');
  });

  it('unterminated quote ends at NUL', () => {
    const p = parseCursor('"abc');
    expect(COM_Parse(p)).toBe('abc');
    expect(p.pos).toBe(5);
  });

  it('high-bit chars are whitespace outside quotes (signed char), kept inside quotes', () => {
    expect(COM_ParseAll('ab\xe9cd "x\xe9y"')).toEqual(['ab', 'cd', 'x\xe9y']);
  });

  it('a word runs into a quote char', () => {
    expect(COM_ParseAll('ab"cd"')).toEqual(['ab"cd"']);
  });

  it('discards unquoted tokens of MAX_TOKEN_CHARS, keeps long quoted ones truncated', () => {
    expect(COM_ParseAll('x'.repeat(128))).toEqual(['']);
    expect(COM_ParseAll('x'.repeat(127))).toEqual(['x'.repeat(127)]);
    expect(COM_ParseAll('x'.repeat(200))).toEqual(['']);
    expect(COM_ParseAll('"' + 'y'.repeat(200) + '"')).toEqual(['y'.repeat(128)]);
  });

  it('stops at embedded NUL', () => {
    expect(COM_ParseAll('a\0b')).toEqual(['a']);
  });
});

describe('path helpers', () => {
  it('COM_SkipPath / StripExtension / FileExtension', () => {
    expect(COM_SkipPath('maps/demo1.bsp')).toBe('demo1.bsp');
    expect(COM_StripExtension('players/male.x/tris.md2')).toBe('players/male');
    expect(COM_FileExtension('a.b.c')).toBe('b.c');
    expect(COM_FileExtension('file.verylongext')).toBe('verylon');
    expect(COM_FileExtension('noext')).toBe('');
  });

  it('COM_FileBase quirks', () => {
    expect(COM_FileBase('maps/demo1.bsp')).toBe('demo1');
    expect(COM_FileBase('demo1.bsp')).toBe('emo1');
    expect(COM_FileBase('noext')).toBe('');
    expect(COM_FileBase('a/b.c')).toBe('b');
    expect(COM_FileBase('a/.c')).toBe('');
  });

  it('COM_FilePath / COM_DefaultExtension', () => {
    expect(COM_FilePath('maps/demo1.bsp')).toBe('maps');
    expect(COM_FilePath('demo1.bsp')).toBe('');
    expect(COM_DefaultExtension('demo1', '.dm2')).toBe('demo1.dm2');
    expect(COM_DefaultExtension('demo1.dm2', '.dm2')).toBe('demo1.dm2');
    expect(COM_DefaultExtension('dir.x/demo1', '.dm2')).toBe('dir.x/demo1.dm2');
  });
});

describe('string compare', () => {
  it('case-insensitive compares', () => {
    expect(Q_strcasecmp('Hello', 'hELLO')).toBe(0);
    expect(Q_strcasecmp('a', 'b')).toBe(-1);
    expect(Q_strncasecmp('abcX', 'ABCy', 3)).toBe(0);
    expect(Q_stricmp('abc', 'ABD')).toBeLessThan(0);
    expect(Q_stricmp('b', 'A')).toBeGreaterThan(0);
    expect(Q_stricmp('same', 'SAME')).toBe(0);
  });

  it('atoi / atof', () => {
    expect(atoi('  42xyz')).toBe(42);
    expect(atoi('-17')).toBe(-17);
    expect(atoi('abc')).toBe(0);
    expect(atoi('4294967297')).toBe(1);
    expect(atoi('99999999999999999999')).toBe(-1);
    expect(atof(' 1.5e2x')).toBe(150);
    expect(atof('.5')).toBe(0.5);
    expect(atof('-inf')).toBe(-Infinity);
    expect(atof('0x10')).toBe(16);
    expect(atof('junk')).toBe(0);
  });
});

describe('info strings', () => {
  const s = '\\name\\player\\skin\\male/grunt\\hand\\0';
  it('Info_ValueForKey', () => {
    expect(Info_ValueForKey(s, 'skin')).toBe('male/grunt');
    expect(Info_ValueForKey(s, 'hand')).toBe('0');
    expect(Info_ValueForKey(s, 'nope')).toBe('');
    expect(Info_ValueForKey('name\\bob', 'name')).toBe('bob');
  });

  it('Info_RemoveKey', () => {
    expect(Info_RemoveKey(s, 'skin')).toBe('\\name\\player\\hand\\0');
    expect(Info_RemoveKey(s, 'name')).toBe('\\skin\\male/grunt\\hand\\0');
    expect(Info_RemoveKey(s, 'hand')).toBe('\\name\\player\\skin\\male/grunt');
    expect(Info_RemoveKey(s, 'x\\y')).toBe(s);
  });

  it('Info_SetValueForKey replaces, appends and strips high bits / control chars', () => {
    expect(Info_SetValueForKey(s, 'hand', '2')).toBe('\\name\\player\\skin\\male/grunt\\hand\\2');
    expect(Info_SetValueForKey('', 'name', 'b\xe9o\x01b')).toBe('\\name\\biob');
    expect(Info_SetValueForKey(s, 'skin', '')).toBe('\\name\\player\\hand\\0');
    const msgs: string[] = [];
    expect(Info_SetValueForKey(s, 'a;b', 'x', (m) => msgs.push(m))).toBe(s);
    expect(Info_SetValueForKey(s, 'k', 'x'.repeat(64), (m) => msgs.push(m))).toBe(s);
    // 508 + 4 == MAX_INFO_STRING is still accepted, 509 + 4 is not
    expect(Info_SetValueForKey('\\a\\' + 'x'.repeat(505), 'b', 'c').length).toBe(512);
    expect(Info_SetValueForKey('\\a\\' + 'x'.repeat(506), 'b', 'c', (m) => msgs.push(m))).toBe(
      '\\a\\' + 'x'.repeat(506),
    );
    expect(msgs.length).toBe(3);
  });

  it('Info_Validate', () => {
    expect(Info_Validate(s)).toBe(true);
    expect(Info_Validate('a"b')).toBe(false);
    expect(Info_Validate('a;b')).toBe(false);
  });
});
