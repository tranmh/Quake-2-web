import { describe, expect, it } from 'vitest';
import {
  EntityState,
  PlayerState,
  RF_BEAM,
  U_EFFECTS16,
  U_EFFECTS8,
  U_MOREBITS1,
  U_MOREBITS2,
  U_MOREBITS3,
  U_NUMBER16,
  U_OLDORIGIN,
  U_RENDERFX8,
  U_SKIN16,
  U_SKIN8,
  svc_playerinfo,
} from 'q2-shared';
import { bytesToHex } from 'q2-shared/testing';
import { parseDelta, parseEntityBits, parsePlayerstate } from '../src/cl_ents';
import { MSG_WriteDeltaEntity } from '../src/msg';
import { ProtocolError, SizeBuf } from '../src/sizebuf';
import { writePlayerstateToClient } from '../src/sv_ents';

function reader(sb: SizeBuf): SizeBuf {
  const r = new SizeBuf(sb.data.slice(0, sb.cursize));
  r.cursize = sb.cursize;
  return r;
}

function ent(n: number): EntityState {
  const e = new EntityState();
  e.number = n;
  return e;
}

describe('MSG_WriteDeltaEntity', () => {
  it('writes nothing when unchanged and not forced', () => {
    const a = ent(5);
    const sb = new SizeBuf(64);
    MSG_WriteDeltaEntity(a, a, sb, false, false);
    expect(sb.cursize).toBe(0);
    MSG_WriteDeltaEntity(a, a, sb, true, false);
    expect(bytesToHex(sb.written())).toBe('0005');
  });
  it('rejects bad numbers', () => {
    const sb = new SizeBuf(64);
    expect(() => MSG_WriteDeltaEntity(ent(0), ent(0), sb, true, false)).toThrow(ProtocolError);
    expect(() => MSG_WriteDeltaEntity(ent(1024), ent(1024), sb, true, false)).toThrow(ProtocolError);
  });
  it('selects bits exactly and round trips', () => {
    const from = ent(300);
    const to = ent(300);
    to.origin.set([10.5, -20.25, 30]);
    to.angles.set([0, 90, 180]);
    to.modelindex = 3;
    to.modelindex4 = 9;
    to.frame = 300;
    to.skinnum = 0x12345;
    to.effects = 0x80000000;
    to.renderfx = -1;
    to.solid = 0x1234;
    to.sound = 7;
    to.event = 2;
    const sb = new SizeBuf(128);
    MSG_WriteDeltaEntity(from, to, sb, false, true);
    const r = reader(sb);
    const eb = parseEntityBits(r);
    expect(eb.number).toBe(300);
    const b = eb.bits;
    expect(b & (U_MOREBITS1 | U_MOREBITS2 | U_MOREBITS3)).toBe(U_MOREBITS1 | U_MOREBITS2 | U_MOREBITS3);
    expect(b & U_NUMBER16).toBeTruthy();
    expect(b & (U_SKIN8 | U_SKIN16)).toBe(U_SKIN8 | U_SKIN16);
    expect(b & (U_EFFECTS8 | U_EFFECTS16)).toBe(U_EFFECTS8 | U_EFFECTS16);
    expect(b & U_RENDERFX8).toBeTruthy(); // negative renderfx < 256 (signed)
    expect(b & U_OLDORIGIN).toBeTruthy();
    const out = new EntityState();
    parseDelta(from, out, eb.number, b, r);
    expect(r.readcount).toBe(r.cursize);
    expect(Array.from(out.origin)).toEqual([10.5, -20.25, 30]);
    expect(Array.from(out.angles)).toEqual([0, 90, -180]);
    expect(out.modelindex).toBe(3);
    expect(out.modelindex4).toBe(9);
    expect(out.frame).toBe(300);
    expect(out.skinnum).toBe(0x12345);
    expect(out.effects).toBe(0x80000000);
    expect(out.renderfx).toBe(255); // written as a byte
    expect(out.solid).toBe(0x1234);
    expect(out.sound).toBe(7);
    expect(out.event).toBe(2);
    expect(Array.from(out.old_origin)).toEqual([0, 0, 0]);
  });
  it('RF_BEAM forces old_origin; event cleared when absent', () => {
    const from = ent(2);
    from.event = 5;
    const to = ent(2);
    to.renderfx = RF_BEAM;
    to.old_origin.set([1, 2, 3]);
    const sb = new SizeBuf(64);
    MSG_WriteDeltaEntity(from, to, sb, false, false);
    const r = reader(sb);
    const eb = parseEntityBits(r);
    expect(eb.bits & U_OLDORIGIN).toBeTruthy();
    const out = new EntityState();
    parseDelta(from, out, eb.number, eb.bits, r);
    expect(Array.from(out.old_origin)).toEqual([1, 2, 3]);
    expect(out.event).toBe(0);
  });
  it('effects 16-bit branch and coord truncation', () => {
    const from = ent(1);
    const to = ent(1);
    to.effects = 0x1000;
    to.origin[0] = 0.124; // (int)(0.992) = 0 -> still flagged since floats differ
    const sb = new SizeBuf(64);
    MSG_WriteDeltaEntity(from, to, sb, false, false);
    // bits: ORIGIN1 (1) | EFFECTS16 (0x80000) -> MOREBITS2|MOREBITS1
    expect(bytesToHex(sb.written())).toBe('81' + '80' + '08' + '01' + '0010' + '0000');
  });
});

describe('player state delta', () => {
  it('writes svc_playerinfo and round trips', () => {
    const to = new PlayerState();
    to.pmove.pm_type = 2;
    to.pmove.origin.set([100, -200, 300]);
    to.pmove.velocity.set([1, 2, 3]);
    to.pmove.pm_flags = 4;
    to.pmove.pm_time = 9;
    to.pmove.gravity = 800;
    to.pmove.delta_angles.set([0, 16384, 0]);
    to.viewoffset.set([0, 0, 22]);
    to.viewangles.set([10, 20, 30]);
    to.kick_angles.set([-1.25, 0, 1]);
    to.gunindex = 5;
    to.gunframe = 7;
    to.gunoffset.set([0.5, 0, 0]);
    to.gunangles.set([0, 0.25, 0]);
    to.blend.set([1, 0.5, 0, 0.2]);
    to.fov = 90;
    to.rdflags = 1;
    to.stats[1] = 100;
    to.stats[31] = -3;
    const sb = new SizeBuf(256);
    writePlayerstateToClient(null, to, sb);
    expect(sb.data[0]).toBe(svc_playerinfo);
    const r = reader(sb);
    r.readcount = 1;
    const out = new PlayerState();
    parsePlayerstate(null, out, r);
    expect(r.readcount).toBe(r.cursize);
    expect(out.pmove.equals(to.pmove)).toBe(true);
    expect(Array.from(out.viewoffset)).toEqual([0, 0, 22]);
    expect(out.viewangles[1]).toBeCloseTo(20, 2);
    expect(Array.from(out.kick_angles)).toEqual([-1.25, 0, 1]);
    expect(out.gunindex).toBe(5);
    expect(out.gunframe).toBe(7);
    expect(Array.from(out.gunoffset)).toEqual([0.5, 0, 0]);
    expect(Array.from(out.gunangles)).toEqual([0, 0.25, 0]);
    expect(out.blend[0]).toBe(1);
    expect(out.blend[1]).toBe(Math.fround(127 / 255));
    expect(out.fov).toBe(90);
    expect(out.rdflags).toBe(1);
    expect(out.stats[1]).toBe(100);
    expect(out.stats[31]).toBe(-3);
  });
  it('unchanged state writes only flags, gunindex and statbits', () => {
    const a = new PlayerState();
    a.gunindex = 3;
    const sb = new SizeBuf(64);
    writePlayerstateToClient(a, a, sb);
    expect(bytesToHex(sb.written())).toBe('11' + '0010' + '03' + '00000000');
  });
  it('attractloop forces PM_FREEZE', () => {
    const sb = new SizeBuf(64);
    writePlayerstateToClient(null, new PlayerState(), sb);
    const r = reader(sb);
    r.readcount = 1;
    const out = new PlayerState();
    parsePlayerstate(null, out, r, true);
    expect(out.pmove.pm_type).toBe(4);
  });
});
