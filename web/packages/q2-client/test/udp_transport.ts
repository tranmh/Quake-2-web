// Node-only UDP DatagramTransport for tests against the original C dedicated server (q2ded).
import { createSocket, type Socket } from 'node:dgram';
import type { DatagramTransport } from '../src/transport';

export class UdpTransport implements DatagramTransport {
  onMessage: ((data: Uint8Array) => void) | null = null;
  onClose: ((reason: string) => void) | null = null;
  private readonly sock: Socket;
  private closed = false;
  readonly host: string;
  readonly port: number;
  sentCount = 0;
  recvCount = 0;

  constructor(address: string) {
    const i = address.lastIndexOf(':');
    this.host = i >= 0 ? address.slice(0, i) : address;
    this.port = i >= 0 ? Number(address.slice(i + 1)) : 27910;
    if (!this.host || !Number.isFinite(this.port)) throw new Error('bad address ' + address);
    this.sock = createSocket('udp4');
    this.sock.on('message', (msg) => {
      this.recvCount++;
      this.onMessage?.(new Uint8Array(msg.buffer, msg.byteOffset, msg.byteLength).slice());
    });
    this.sock.on('error', (e) => this.onClose?.(String(e)));
  }

  send(data: Uint8Array): void {
    if (this.closed) return;
    this.sentCount++;
    this.sock.send(data, this.port, this.host);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    // let pending sends (the 3 "disconnect" datagrams) flush before closing
    setTimeout(() => this.sock.close(), 50);
  }
}
