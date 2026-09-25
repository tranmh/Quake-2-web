// Port of qcommon/net_chan.c.
//
// packet header
// -------------
// 31 sequence
// 1  does this message contain a reliable payload
// 31 acknowledge sequence
// 1  acknowledge receipt of even/odd message
// 16 qport (client -> server only)
//
// The C globals (showpackets/showdrop/qport cvars, curtime, NET_SendPacket) are gathered in a
// NetchanContext so there is no module-level mutable state.
import { MAX_MSGLEN, NS_CLIENT, NS_SERVER, cInt, latin1ToBytes, sprintf, type PrintfArg } from 'q2-shared';
import { MSG_BeginReading, MSG_ReadLong, MSG_ReadShort, MSG_WriteLong, MSG_WriteShort } from './msg';
import { SZ_Clear, SZ_Write, SizeBuf } from './sizebuf';

export type NetSrc = typeof NS_CLIENT | typeof NS_SERVER | number;

export interface NetchanContext<A> {
  /** NET_SendPacket(sock, length, data, to) */
  sendPacket(sock: NetSrc, data: Uint8Array, to: A): void;
  /** Sys_Milliseconds()-based `curtime` */
  curtime(): number;
  /** value of the `qport` cvar (written by clients in every packet) */
  qport: number;
  /** `showpackets` cvar */
  showpackets?: number;
  /** `showdrop` cvar */
  showdrop?: number;
  /** Com_Printf */
  print?: (msg: string) => void;
  /** NET_AdrToString */
  adrToString?: (a: A) => string;
}

// C: net_chan.c:106 Netchan_OutOfBand -- sends an out-of-band datagram (sequence -1)
export function Netchan_OutOfBand<A>(
  ctx: NetchanContext<A>,
  net_socket: NetSrc,
  adr: A,
  length: number,
  data: Uint8Array,
): void {
  const send = new SizeBuf(MAX_MSGLEN);
  MSG_WriteLong(send, -1); // -1 sequence means out of band
  SZ_Write(send, data, length);
  ctx.sendPacket(net_socket, send.data.slice(0, send.cursize), adr);
}

// C: net_chan.c:127 Netchan_OutOfBandPrint -- sends a text message in an out-of-band datagram
export function Netchan_OutOfBandPrint<A>(
  ctx: NetchanContext<A>,
  net_socket: NetSrc,
  adr: A,
  format: string,
  ...args: PrintfArg[]
): void {
  let s = sprintf(format, ...args);
  if (s.length > MAX_MSGLEN - 4 - 1) s = s.slice(0, MAX_MSGLEN - 4 - 1); // static buffer (C would overrun)
  const b = latin1ToBytes(s);
  Netchan_OutOfBand(ctx, net_socket, adr, b.length, b);
}

// C: qcommon.h netchan_t
export class NetChan<A> {
  fatal_error = false;
  sock: NetSrc = NS_CLIENT;
  /** between last packet and previous */
  dropped = 0;
  /** for timeouts */
  last_received = 0;
  /** for retransmits */
  last_sent = 0;
  remote_address!: A;
  /** qport value to write when transmitting */
  qport = 0;

  // sequencing variables
  incoming_sequence = 0;
  incoming_acknowledged = 0;
  /** single bit */
  incoming_reliable_acknowledged = 0;
  /** single bit, maintained local */
  incoming_reliable_sequence = 0;

  outgoing_sequence = 0;
  /** single bit */
  reliable_sequence = 0;
  /** sequence number of last send */
  last_reliable_sequence = 0;

  // reliable staging and holding areas
  /** writing buffer to send to server */
  readonly message = new SizeBuf(MAX_MSGLEN - 16);
  reliable_length = 0;
  /** unacked reliable message */
  readonly reliable_buf = new Uint8Array(MAX_MSGLEN - 16);

  private readonly send = new SizeBuf(MAX_MSGLEN);

  constructor(readonly ctx: NetchanContext<A>) {}

  private print(fmt: string, ...args: PrintfArg[]): void {
    this.ctx.print?.(sprintf(fmt, ...args));
  }

  private adr(): string {
    return this.ctx.adrToString ? this.ctx.adrToString(this.remote_address) : String(this.remote_address);
  }

  // C: net_chan.c:147 Netchan_Setup -- called to open a channel to a remote system
  setup(sock: NetSrc, adr: A, qport: number): void {
    this.fatal_error = false;
    this.dropped = 0;
    this.last_sent = 0;
    this.incoming_acknowledged = 0;
    this.incoming_reliable_acknowledged = 0;
    this.incoming_reliable_sequence = 0;
    this.reliable_sequence = 0;
    this.last_reliable_sequence = 0;
    this.reliable_length = 0;
    this.reliable_buf.fill(0);

    this.sock = sock;
    this.remote_address = adr;
    this.qport = qport;
    this.last_received = this.ctx.curtime();
    this.incoming_sequence = 0;
    this.outgoing_sequence = 1;

    this.message.data.fill(0);
    this.message.maxsize = this.message.data.length;
    this.message.cursize = 0;
    this.message.readcount = 0;
    this.message.overflowed = false;
    this.message.allowoverflow = true;
    this.message.onPrint = this.ctx.print ?? null;
  }

  // C: net_chan.c:171 Netchan_CanReliable -- true if the last reliable message has acked
  canReliable(): boolean {
    if (this.reliable_length) return false; // waiting for ack
    return true;
  }

  // C: net_chan.c:179 Netchan_NeedReliable
  needReliable(): boolean {
    let send_reliable = false;
    // if the remote side dropped the last reliable message, resend it
    if (
      this.incoming_acknowledged > this.last_reliable_sequence &&
      this.incoming_reliable_acknowledged !== this.reliable_sequence
    )
      send_reliable = true;
    // if the reliable transmit buffer is empty, copy the current message out
    if (!this.reliable_length && this.message.cursize) send_reliable = true;
    return send_reliable;
  }

  // C: net_chan.c:207 Netchan_Transmit
  // Tries to send an unreliable message to a connection, and handles the transmition / retransmition of
  // the reliable messages. A 0 length will still generate a packet and deal with the reliable messages.
  transmit(length: number, data: Uint8Array = new Uint8Array(0)): void {
    // check for message overflow
    if (this.message.overflowed) {
      this.fatal_error = true;
      this.print('%s:Outgoing message overflow\n', this.adr());
      return;
    }

    const send_reliable = this.needReliable();

    if (!this.reliable_length && this.message.cursize) {
      this.reliable_buf.set(this.message.data.subarray(0, this.message.cursize));
      this.reliable_length = this.message.cursize;
      this.message.cursize = 0;
      this.reliable_sequence ^= 1;
    }

    // write the packet header
    const send = this.send;
    send.allowoverflow = false;
    SZ_Clear(send);

    const w1 = ((this.outgoing_sequence & ~(1 << 31)) | ((send_reliable ? 1 : 0) << 31)) >>> 0;
    const w2 = ((this.incoming_sequence & ~(1 << 31)) | (this.incoming_reliable_sequence << 31)) >>> 0;

    this.outgoing_sequence++;
    this.last_sent = this.ctx.curtime();

    MSG_WriteLong(send, w1);
    MSG_WriteLong(send, w2);

    // send the qport if we are a client
    if (this.sock === NS_CLIENT) MSG_WriteShort(send, cInt(this.ctx.qport));

    // copy the reliable message to the packet first
    if (send_reliable) {
      SZ_Write(send, this.reliable_buf, this.reliable_length);
      this.last_reliable_sequence = this.outgoing_sequence;
    }

    // add the unreliable part if space is available
    if (send.maxsize - send.cursize >= length) SZ_Write(send, data, length);
    else this.print('Netchan_Transmit: dumped unreliable\n');

    // send the datagram
    this.ctx.sendPacket(this.sock, send.data.slice(0, send.cursize), this.remote_address);

    if (this.ctx.showpackets) {
      if (send_reliable)
        this.print(
          'send %4i : s=%i reliable=%i ack=%i rack=%i\n',
          send.cursize,
          this.outgoing_sequence - 1,
          this.reliable_sequence,
          this.incoming_sequence,
          this.incoming_reliable_sequence,
        );
      else
        this.print(
          'send %4i : s=%i ack=%i rack=%i\n',
          send.cursize,
          this.outgoing_sequence - 1,
          this.incoming_sequence,
          this.incoming_reliable_sequence,
        );
    }
  }

  // C: net_chan.c:298 Netchan_Process
  // Called when the current net_message is from remote_address; leaves msg.readcount at the payload.
  process(msg: SizeBuf): boolean {
    // get sequence numbers
    MSG_BeginReading(msg);
    let sequence = MSG_ReadLong(msg) >>> 0;
    let sequence_ack = MSG_ReadLong(msg) >>> 0;

    // read the qport if we are a server
    if (this.sock === NS_SERVER) MSG_ReadShort(msg);

    const reliable_message = sequence >>> 31;
    const reliable_ack = sequence_ack >>> 31;

    sequence = (sequence & ~(1 << 31)) >>> 0;
    sequence_ack = (sequence_ack & ~(1 << 31)) >>> 0;

    if (this.ctx.showpackets) {
      if (reliable_message)
        this.print(
          'recv %4i : s=%i reliable=%i ack=%i rack=%i\n',
          msg.cursize,
          sequence,
          this.incoming_reliable_sequence ^ 1,
          sequence_ack,
          reliable_ack,
        );
      else this.print('recv %4i : s=%i ack=%i rack=%i\n', msg.cursize, sequence, sequence_ack, reliable_ack);
    }

    //
    // discard stale or duplicated packets (unsigned comparison)
    //
    if (sequence <= this.incoming_sequence >>> 0) {
      if (this.ctx.showdrop)
        this.print('%s:Out of order packet %i at %i\n', this.adr(), sequence, this.incoming_sequence);
      return false;
    }

    //
    // dropped packets don't keep the message from being used
    //
    this.dropped = (sequence - (this.incoming_sequence + 1)) | 0;
    if (this.dropped > 0) {
      if (this.ctx.showdrop) this.print('%s:Dropped %i packets at %i\n', this.adr(), this.dropped, sequence);
    }

    //
    // if the current outgoing reliable message has been acknowledged
    // clear the buffer to make way for the next
    //
    if (reliable_ack === this.reliable_sequence) this.reliable_length = 0; // it has been received

    //
    // if this message contains a reliable message, bump incoming_reliable_sequence
    //
    this.incoming_sequence = sequence | 0;
    this.incoming_acknowledged = sequence_ack | 0;
    this.incoming_reliable_acknowledged = reliable_ack;
    if (reliable_message) this.incoming_reliable_sequence ^= 1;

    //
    // the message can now be read from the current message pointer
    //
    this.last_received = this.ctx.curtime();

    return true;
  }
}
