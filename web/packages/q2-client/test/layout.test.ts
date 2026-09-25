import { describe, expect, it } from 'vitest';
import {
  CS_IMAGES,
  CS_ITEMS,
  MAX_CONFIGSTRINGS,
  MAX_STATS,
  STAT_AMMO,
  STAT_AMMO_ICON,
  STAT_ARMOR,
  STAT_ARMOR_ICON,
  STAT_CHASE,
  STAT_FLASHES,
  STAT_FRAGS,
  STAT_HEALTH,
  STAT_HEALTH_ICON,
  STAT_HELPICON,
  STAT_PICKUP_ICON,
  STAT_PICKUP_STRING,
  STAT_SELECTED_ICON,
  STAT_SPECTATOR,
  STAT_TIMER,
  STAT_TIMER_ICON,
  sprintf,
} from 'q2-shared';
import {
  executeLayoutString,
  opDrawField,
  opDrawHUDString,
  type DrawOp,
  type LayoutEnv,
} from '../src/layout';

// game/g_spawn.c:648 single_statusbar (verbatim, comments removed)
const single_statusbar =
  'yb	-24 ' +
  'xv	0 ' +
  'hnum ' +
  'xv	50 ' +
  'pic 0 ' +
  'if 2 ' +
  '	xv	100 ' +
  '	anum ' +
  '	xv	150 ' +
  '	pic 2 ' +
  'endif ' +
  'if 4 ' +
  '	xv	200 ' +
  '	rnum ' +
  '	xv	250 ' +
  '	pic 4 ' +
  'endif ' +
  'if 6 ' +
  '	xv	296 ' +
  '	pic 6 ' +
  'endif ' +
  'yb	-50 ' +
  'if 7 ' +
  '	xv	0 ' +
  '	pic 7 ' +
  '	xv	26 ' +
  '	yb	-42 ' +
  '	stat_string 8 ' +
  '	yb	-50 ' +
  'endif ' +
  'if 9 ' +
  '	xv	262 ' +
  '	num	2	10 ' +
  '	xv	296 ' +
  '	pic	9 ' +
  'endif ' +
  'if 11 ' +
  '	xv	148 ' +
  '	pic	11 ' +
  'endif ';

// game/g_spawn.c:704 dm_statusbar (verbatim, comments removed)
const dm_statusbar =
  'yb	-24 ' +
  'xv	0 ' +
  'hnum ' +
  'xv	50 ' +
  'pic 0 ' +
  'if 2 ' +
  '	xv	100 ' +
  '	anum ' +
  '	xv	150 ' +
  '	pic 2 ' +
  'endif ' +
  'if 4 ' +
  '	xv	200 ' +
  '	rnum ' +
  '	xv	250 ' +
  '	pic 4 ' +
  'endif ' +
  'if 6 ' +
  '	xv	296 ' +
  '	pic 6 ' +
  'endif ' +
  'yb	-50 ' +
  'if 7 ' +
  '	xv	0 ' +
  '	pic 7 ' +
  '	xv	26 ' +
  '	yb	-42 ' +
  '	stat_string 8 ' +
  '	yb	-50 ' +
  'endif ' +
  'if 9 ' +
  '	xv	246 ' +
  '	num	2	10 ' +
  '	xv	296 ' +
  '	pic	9 ' +
  'endif ' +
  'if 11 ' +
  '	xv	148 ' +
  '	pic	11 ' +
  'endif ' +
  'xr	-50 ' +
  'yt 2 ' +
  'num 3 14 ' +
  'if 17 ' +
  'xv 0 ' +
  'yb -58 ' +
  'string2 "SPECTATOR MODE" ' +
  'endif ' +
  'if 16 ' +
  'xv 0 ' +
  'yb -68 ' +
  'string "Chasing" ' +
  'xv 64 ' +
  'stat_string 16 ' +
  'endif ';

function makeEnv(stats: Partial<Record<number, number>>, extra: Partial<LayoutEnv> = {}): LayoutEnv {
  const st = new Int16Array(MAX_STATS);
  for (const [k, v] of Object.entries(stats)) st[Number(k)] = v!;
  const cs = new Array<string>(MAX_CONFIGSTRINGS).fill('');
  cs[CS_IMAGES + 1] = 'i_health';
  cs[CS_IMAGES + 2] = 'a_bullets';
  cs[CS_IMAGES + 3] = 'i_jacketarmor';
  cs[CS_IMAGES + 4] = 'w_machinegun';
  cs[CS_IMAGES + 5] = 'p_quad';
  cs[CS_IMAGES + 6] = 'i_help';
  cs[CS_ITEMS + 10] = 'Bullets';
  cs[CS_ITEMS + 30] = 'Grunt';
  const names = ['Player', 'Bob', 'Alice'];
  return {
    width: 640,
    height: 480,
    stats: st,
    configstrings: cs,
    serverframe: 0,
    playernum: 0,
    clientName: (i) => names[i] ?? '',
    clientIcon: (i) => (i === 1 ? '/players/male/grunt_i' : '/players/male/grunt_i'),
    ...extra,
  };
}

/** compact textual form of ops (strings with high bit shown via ~ prefix) */
function fmt(ops: DrawOp[]): string[] {
  return ops.map((o) => {
    switch (o.op) {
      case 'pic':
        return `pic ${o.x},${o.y} ${o.name}`;
      case 'char':
        return `char ${o.x},${o.y} ${o.num}`;
      case 'string':
        return `${o.xor ? 'alt' : 'str'} ${o.x},${o.y} "${o.text}"`;
      case 'dirty':
        return `dirty ${o.x},${o.y}`;
      case 'error':
        return `error ${o.msg}`;
    }
  });
}

const baseStats = {
  [STAT_HEALTH_ICON]: 1,
  [STAT_HEALTH]: 100,
  [STAT_AMMO_ICON]: 2,
  [STAT_AMMO]: 50,
  [STAT_ARMOR_ICON]: 3,
  [STAT_ARMOR]: 25,
  [STAT_SELECTED_ICON]: 1,
};

describe('SCR_ExecuteLayoutString (pure)', () => {
  it('single_statusbar full HUD', () => {
    const ops = executeLayoutString(single_statusbar, makeEnv(baseStats));
    const f = fmt(ops);
    // health field at xv 0 yb -24 : x = 640/2-160 = 160, y = 456; 3 digits, "100" => x+2
    expect(f.slice(0, 5)).toEqual([
      'dirty 160,456',
      'dirty 210,479',
      'pic 162,456 num_1',
      'pic 178,456 num_0',
      'pic 194,456 num_0',
    ]);
    expect(f).toContain('pic 210,456 i_health');
    expect(f).toContain('pic 310,456 a_bullets');
    // armor 25 → two digits right aligned: x = 360 + 2 + 16
    expect(f).toContain('pic 378,456 num_2');
    expect(f).toContain('pic 410,456 i_jacketarmor');
    expect(f).toContain('pic 456,456 i_health');
    expect(f).toMatchSnapshot();
  });

  it('single_statusbar: low health flashes on serverframe bit 2, zero health red, flashes field_3', () => {
    const lowA = fmt(executeLayoutString(single_statusbar, makeEnv({ ...baseStats, [STAT_HEALTH]: 20 })));
    expect(lowA).toContain('pic 178,456 num_2');
    const lowB = fmt(
      executeLayoutString(single_statusbar, makeEnv({ ...baseStats, [STAT_HEALTH]: 20 }, { serverframe: 4 })),
    );
    expect(lowB).toContain('pic 178,456 anum_2');
    const dead = fmt(
      executeLayoutString(single_statusbar, makeEnv({ ...baseStats, [STAT_HEALTH]: -5, [STAT_FLASHES]: 1 })),
    );
    expect(dead).toContain('pic 160,456 field_3');
    expect(dead).toContain('pic 178,456 anum_minus');
    expect(dead).toContain('pic 194,456 anum_5');
  });

  it('single_statusbar: no ammo/armor sections when icons are 0, negative ammo hidden, armor < 1 hidden', () => {
    const f = fmt(
      executeLayoutString(single_statusbar, makeEnv({ [STAT_HEALTH_ICON]: 1, [STAT_HEALTH]: 55 })),
    );
    expect(f.some((s) => s.includes('a_bullets'))).toBe(false);
    const neg = fmt(
      executeLayoutString(
        single_statusbar,
        makeEnv({ ...baseStats, [STAT_AMMO]: -1, [STAT_ARMOR]: 0, [STAT_FLASHES]: 6 }),
      ),
    );
    expect(neg.filter((s) => s.includes('field_3'))).toEqual([]);
    expect(neg).toMatchSnapshot();
  });

  it('single_statusbar: pickup, timer, help icon', () => {
    const f = fmt(
      executeLayoutString(
        single_statusbar,
        makeEnv({
          ...baseStats,
          [STAT_PICKUP_ICON]: 5,
          [STAT_PICKUP_STRING]: CS_ITEMS + 10,
          [STAT_TIMER_ICON]: 5,
          [STAT_TIMER]: 7,
          [STAT_HELPICON]: 6,
        }),
      ),
    );
    expect(f).toContain('pic 160,430 p_quad');
    expect(f).toContain('str 186,438 "Bullets"');
    // timer: num 2 10 at xv 262 → x=422; one digit: x + 2 + 16
    expect(f).toContain('pic 440,430 num_7');
    expect(f).toContain('pic 308,430 i_help');
    expect(f).toMatchSnapshot();
  });

  it('dm_statusbar: frags, spectator, chase', () => {
    const f = fmt(
      executeLayoutString(
        dm_statusbar,
        makeEnv({
          ...baseStats,
          [STAT_FRAGS]: -12,
          [STAT_SPECTATOR]: 1,
          [STAT_CHASE]: CS_ITEMS + 30,
        }),
      ),
    );
    // frags: xr -50 yt 2 num 3 14 → x=590 "-12"
    expect(f).toContain('pic 592,2 num_minus');
    expect(f).toContain('pic 624,2 num_2');
    expect(f).toContain('alt 160,422 "SPECTATOR MODE"');
    expect(f).toContain('str 160,412 "Chasing"');
    expect(f).toContain('str 224,412 "Grunt"');
    expect(f).toMatchSnapshot();
  });

  it('deathmatch scoreboard (p_hud.c DeathmatchScoreboardMessage)', () => {
    let s = '';
    // two players, the first with the "tag1" (killer) picture
    s += sprintf('client %i %i %i %i %i %i ', 0, 32, 0, 15, 50, 3);
    s += sprintf('xv %i yv %i picn %s ', 0 + 32, 32, 'tag1');
    s += sprintf('client %i %i %i %i %i %i ', 160, 32, 1, -2, 999, 12);
    const f = fmt(executeLayoutString(s, makeEnv(baseStats)));
    expect(f.slice(0, 8)).toEqual([
      'dirty 160,152',
      'dirty 319,183',
      'alt 192,152 "Player"',
      'str 192,160 "Score: "',
      'alt 248,160 "15"',
      'str 192,168 "Ping:  50"',
      'str 192,176 "Time:  3"',
      'pic 160,152 /players/male/grunt_i',
    ]);
    expect(f).toMatchSnapshot();
  });

  it('help computer (p_hud.c HelpComputer)', () => {
    const s = sprintf(
      'xv 32 yv 8 picn help ' +
        'xv 202 yv 12 string2 "%s" ' +
        'xv 0 yv 24 cstring2 "%s" ' +
        'xv 0 yv 54 cstring2 "%s" ' +
        'xv 0 yv 110 cstring2 "%s" ' +
        'xv 50 yv 164 string2 " kills     goals    secrets" ' +
        'xv 50 yv 172 string2 "%3i/%3i     %i/%i       %i/%i" ',
      'medium',
      'Outer Base',
      'Locate the Security\nPass and escape',
      'No secondary objective',
      3,
      27,
      0,
      1,
      1,
      3,
    );
    const f = fmt(executeLayoutString(s, makeEnv(baseStats)));
    // cstring2 centers each line in 320 pixels: "Outer Base" (10 chars) → x = 160 + (320-80)/2
    expect(f).toContain('alt 280,144 "Outer Base"');
    expect(f).toContain('alt 244,174 "Locate the Security"');
    expect(f).toContain('alt 260,182 "Pass and escape"');
    expect(f).toMatchSnapshot();
  });

  it('CTF scoreboard (ctf/g_ctf.c CTFScoreboardMessage)', () => {
    let s = 'if 24 xv 8 yv 8 pic 24 endif xv 40 yv 28 string "4" ';
    s += sprintf('ctf %d %d %d %d %d ', 0, 42, 0, 7, 1500);
    s += 'xv 56 picn sbfctf2 ';
    s += sprintf('ctf %d %d %d %d %d ', 160, 42, 2, -3, 45);
    s += 'xv 0 yv 170 string2 "Spectators" ';
    const f = fmt(executeLayoutString(s, makeEnv(baseStats)));
    // own player (playernum 0) drawn alt; ping clamped to 999
    expect(f).toContain('alt 160,162 "  7 999 Player      "');
    expect(f).toContain('str 320,162 " -3  45 Alice       "');
    expect(f).toMatchSnapshot();
  });

  it('errors: bad pic / client / stat_string raise ERR_DROP ops', () => {
    expect(fmt(executeLayoutString('pic 0', makeEnv({ 0: 300 }))).pop()).toBe('error Pic >= MAX_IMAGES');
    expect(fmt(executeLayoutString('client 0 0 300 0 0 0', makeEnv({}))).pop()).toBe(
      'error client >= MAX_CLIENTS',
    );
    expect(fmt(executeLayoutString('stat_string 5000', makeEnv({}))).pop()).toBe(
      'error Bad stat_string index',
    );
  });

  it('if/endif skipping and unknown tokens', () => {
    expect(fmt(executeLayoutString('if 3 picn a endif picn b bogus picn c', makeEnv({})))).toEqual([
      'dirty 0,0',
      'dirty 23,23',
      'pic 0,0 b',
      'dirty 0,0',
      'dirty 23,23',
      'pic 0,0 c',
    ]);
    // unterminated if swallows the rest
    expect(executeLayoutString('if 3 picn a', makeEnv({}))).toEqual([]);
  });

  it('SCR_DrawField: width clamp, truncation of long values, alt colour', () => {
    const ops: DrawOp[] = [];
    opDrawField(ops, 10, 20, 1, 9, 1234567);
    expect(fmt(ops)).toEqual([
      'dirty 10,20',
      'dirty 92,43',
      'pic 12,20 anum_1',
      'pic 28,20 anum_2',
      'pic 44,20 anum_3',
      'pic 60,20 anum_4',
      'pic 76,20 anum_5',
    ]);
    const none: DrawOp[] = [];
    opDrawField(none, 0, 0, 0, 0, 5);
    expect(none).toEqual([]);
  });

  it('DrawHUDString: no centering keeps the margin', () => {
    const ops: DrawOp[] = [];
    opDrawHUDString(ops, 'ab\ncdef\n', 5, 6, 0, 0x80);
    expect(fmt(ops)).toEqual(['alt 5,6 "ab"', 'alt 5,14 "cdef"']);
  });
});
