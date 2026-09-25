// Port of qcommon/common.c SZ_* (sizebuf_t).
import { latin1ToBytes } from 'q2-shared';

/** Com_Error(ERR_FATAL / ERR_DROP, ...) raised by protocol code. */
export class ProtocolError extends Error {
  constructor(
    message: string,
    readonly code: 'fatal' | 'drop' = 'fatal',
  ) {
    super(message);
    this.name = 'ProtocolError';
  }
}

// C: qcommon.h sizebuf_t
export class SizeBuf {
  allowoverflow = false;
  overflowed = false;
  data: Uint8Array;
  maxsize: number;
  cursize = 0;
  readcount = 0;
  /** Com_Printf sink for the "SZ_GetSpace: overflow" warning. */
  onPrint: ((msg: string) => void) | null = null;

  constructor(dataOrLength: Uint8Array | number = 0, length?: number) {
    this.data = typeof dataOrLength === 'number' ? new Uint8Array(dataOrLength) : dataOrLength;
    this.maxsize = length ?? this.data.length;
  }

  /** Bytes written so far (a view, not a copy). */
  written(): Uint8Array {
    return this.data.subarray(0, this.cursize);
  }
}

// C: common.c:900 SZ_Init
export function SZ_Init(buf: SizeBuf, data: Uint8Array, length = data.length): void {
  buf.allowoverflow = false;
  buf.overflowed = false;
  buf.data = data;
  buf.maxsize = length;
  buf.cursize = 0;
  buf.readcount = 0;
}

// C: common.c:907 SZ_Clear
export function SZ_Clear(buf: SizeBuf): void {
  buf.cursize = 0;
  buf.overflowed = false;
}

// C: common.c:913 SZ_GetSpace -- returns the offset of the reserved space in buf.data.
export function SZ_GetSpace(buf: SizeBuf, length: number): number {
  if (buf.cursize + length > buf.maxsize) {
    if (!buf.allowoverflow) throw new ProtocolError('SZ_GetSpace: overflow without allowoverflow set');
    if (length > buf.maxsize) throw new ProtocolError(`SZ_GetSpace: ${length} is > full buffer size`);
    buf.onPrint?.('SZ_GetSpace: overflow\n');
    SZ_Clear(buf);
    buf.overflowed = true;
  }
  const ofs = buf.cursize;
  buf.cursize += length;
  return ofs;
}

// C: common.c:935 SZ_Write
export function SZ_Write(buf: SizeBuf, data: Uint8Array, length = data.length): void {
  const ofs = SZ_GetSpace(buf, length);
  buf.data.set(data.subarray(0, length), ofs);
}

// C: common.c:940 SZ_Print -- appends a C string, overwriting a previous trailing NUL.
export function SZ_Print(buf: SizeBuf, text: string): void {
  const i = text.indexOf('\0');
  const s = i < 0 ? text : text.slice(0, i);
  const bytes = new Uint8Array(s.length + 1);
  bytes.set(latin1ToBytes(s));
  const len = bytes.length;
  if (buf.cursize) {
    if (buf.data[buf.cursize - 1]) {
      buf.data.set(bytes, SZ_GetSpace(buf, len)); // no trailing 0
    } else {
      // write over trailing 0 (C writes at data[-1] if SZ_GetSpace just cleared an overflow; clamped here)
      buf.data.set(bytes, Math.max(0, SZ_GetSpace(buf, len - 1) - 1));
    }
  } else {
    buf.data.set(bytes, SZ_GetSpace(buf, len));
  }
}
