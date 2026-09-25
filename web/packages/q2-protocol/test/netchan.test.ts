import { describe, expect, it } from 'vitest';
import { NS_CLIENT, NS_SERVER } from 'q2-shared';
import { bytesToHex } from 'q2-shared/testing';
import {
  MSG_ReadByte,
  MSG_ReadLong,
  MSG_ReadShort,
  MSG_ReadString,
  MSG_WriteByte,
  MSG_WriteString,
} from '../src/msg';
import { NetChan, Netchan_OutOfBand, Netchan_OutOfBandPrint, type NetchanContext } from '../src/net_chan';
import { SizeBuf } from '../src/sizebuf';

type Adr = string;
interface Pkt {
  sock: number;
  data: Uint8Array;
  to: Adr;
}

function mkctx(out: Pkt[], qport = 0x1234, time = { t: 0 }): NetchanContext<Adr> {
  return {
    sendPacket: (sock, data, to) => out.push({ sock, data, to }),
    curtime: () => time.t,
    qport,
  };
}

function asMsg(p: Pkt): SizeBuf {
  const sb = new SizeBuf(p.data.slice());
  sb.cursize = p.data.length;
  return sb;
}

describe('Netchan', () => {
  it('OutOfBand prefixes -1', () => {
    const out: Pkt[] = [];
    const ctx = mkctx(out);
    Netchan_OutOfBandPrint(ctx, NS_SERVER, 'x', 'print\n%s %d', 'hi', 5);
    expect(bytesToHex(out[0]!.data)).toBe('ffffffff' + bytesToHex(new TextEncoder().encode('print\nhi 5')));
    Netchan_OutOfBand(ctx, NS_CLIENT, 'y', 2, new Uint8Array([1, 2, 3]));
    expect(bytesToHex(out[1]!.data)).toBe('ffffffff0102');
    expect(out[1]!.to).toBe('y');
  });

  it('header layout: client writes qport (10 bytes), server does not (8 bytes)', () => {
    const out: Pkt[] = [];
    const ctx = mkctx(out, 0xbeef);
    const cl = new NetChan(ctx);
    cl.setup(NS_CLIENT, 'server', 7);
    cl.transmit(0);
    expect(bytesToHex(out[0]!.data)).toBe('01000000' + '00000000' + 'efbe');
    const sv = new NetChan(ctx);
    sv.setup(NS_SERVER, 'client', 7);
    sv.transmit(1, new Uint8Array([9]));
    expect(bytesToHex(out[1]!.data)).toBe('01000000' + '00000000' + '09');
  });

  it('delivers reliable data, resends after drop, drops stale packets', () => {
    const toServer: Pkt[] = [];
    const toClient: Pkt[] = [];
    const time = { t: 1000 };
    const cl = new NetChan(mkctx(toServer, 55, time));
    const sv = new NetChan(mkctx(toClient, 0, time));
    cl.setup(NS_CLIENT, 'sv', 55);
    sv.setup(NS_SERVER, 'cl', 55);

    // client queues reliable "hello", sends; packet dropped
    MSG_WriteString(cl.message, 'hello');
    expect(cl.needReliable()).toBe(true);
    cl.transmit(0);
    expect(cl.canReliable()).toBe(false);
    expect(toServer.length).toBe(1);
    const lost = toServer.pop()!;
    expect(lost.data[3]! & 0x80).toBe(0x80); // reliable bit

    // client sends an unreliable packet; server acks it (without the reliable bit)
    cl.transmit(1, new Uint8Array([42]));
    let p = toServer.pop()!;
    expect(p.data[3]! & 0x80).toBe(0); // reliable not resent until an ack shows the drop
    let m = asMsg(p);
    expect(sv.process(m)).toBe(true);
    expect(sv.dropped).toBe(1);
    expect(MSG_ReadByte(m)).toBe(42);
    expect(m.readcount).toBe(11);
    sv.transmit(0);
    expect(cl.process(asMsg(toClient.pop()!))).toBe(true);

    // Quirk: last_reliable_sequence is recorded after outgoing_sequence++ (= 2), so an ack of 2 is not yet
    // "greater" -- one more round trip is needed before the drop is detected.
    expect(cl.last_reliable_sequence).toBe(2);
    expect(cl.needReliable()).toBe(false);
    cl.transmit(0);
    expect(sv.process(asMsg(toServer.pop()!))).toBe(true);
    sv.transmit(0);
    expect(cl.process(asMsg(toClient.pop()!))).toBe(true);

    // client notices the reliable was not acknowledged and resends it
    expect(cl.needReliable()).toBe(true);
    cl.transmit(0);
    p = toServer.pop()!;
    expect(p.data[3]! & 0x80).toBe(0x80);
    m = asMsg(p);
    expect(sv.process(m)).toBe(true);
    expect(MSG_ReadString(m)).toBe('hello');
    expect(sv.incoming_reliable_sequence).toBe(1);

    // server acks the reliable; client can send a new one
    sv.transmit(0);
    expect(cl.process(asMsg(toClient.pop()!))).toBe(true);
    expect(cl.canReliable()).toBe(true);

    // stale duplicate is rejected
    expect(sv.process(asMsg(p))).toBe(false);
    expect(time.t).toBe(1000);
  });

  it('reads sequence/ack/qport fields', () => {
    const out: Pkt[] = [];
    const cl = new NetChan(mkctx(out, 0x4321));
    cl.setup(NS_CLIENT, 'a', 0);
    MSG_WriteByte(cl.message, 1);
    cl.transmit(0);
    const m = asMsg(out[0]!);
    expect(MSG_ReadLong(m) >>> 0).toBe(0x80000001);
    expect(MSG_ReadLong(m)).toBe(0);
    expect(MSG_ReadShort(m)).toBe(0x4321);
  });

  it('outgoing message overflow sets fatal_error', () => {
    const out: Pkt[] = [];
    const msgs: string[] = [];
    const ctx = { ...mkctx(out), print: (s: string) => msgs.push(s), adrToString: (a: string) => a };
    const cl = new NetChan(ctx);
    cl.setup(NS_CLIENT, 'addr', 0);
    for (let i = 0; i < 1400; i++) MSG_WriteByte(cl.message, 1);
    expect(cl.message.overflowed).toBe(true);
    cl.transmit(0);
    expect(cl.fatal_error).toBe(true);
    expect(out.length).toBe(0);
    expect(msgs).toContain('addr:Outgoing message overflow\n');
  });
});
