// Cinematic (.cin) decoding, a port of client/cl_cin.c (header, Huff1TableInit, Huff1Decompress,
// SCR_ReadNextFrame) working on an in-memory file. Playback timing is not handled here.
import { toU8 } from './bytes';
import { FormatError } from './errors';

export interface CinHeader {
  width: number;
  height: number;
  s_rate: number;
  s_width: number;
  s_channels: number;
}

export interface CinFrame {
  /** 0 = frame, 1 = frame with new palette */
  command: number;
  /** 768-byte palette when command == 1 */
  palette: Uint8Array | null;
  /** decompressed 8-bit picture (width*height bytes for well-formed files) */
  pic: Uint8Array;
  /** raw audio for this frame: count * s_width * s_channels bytes */
  samples: Uint8Array;
  /** number of audio sample frames */
  count: number;
}

/** Order-1 Huffman tables (cinematics_t hnodes1 / numhnodes1). */
export class Huff1Tables {
  /** [256][256][2]: for each previous byte, nodes 256..510 stored at (node-256)*2 */
  readonly hnodes1 = new Int32Array(256 * 256 * 2);
  readonly numhnodes1 = new Int32Array(256);
  private readonly h_used = new Int32Array(512);
  private readonly h_count = new Int32Array(512);

  // C: client/cl_cin.c:218 SmallestNode1
  private SmallestNode1(numhnodes: number): number {
    let best = 99999999;
    let bestnode = -1;
    for (let i = 0; i < numhnodes; i++) {
      if (this.h_used[i]) continue;
      if (!this.h_count[i]) continue;
      if (this.h_count[i]! < best) {
        best = this.h_count[i]!;
        bestnode = i;
      }
    }
    if (bestnode === -1) return -1;
    this.h_used[bestnode] = 1;
    return bestnode;
  }

  // C: client/cl_cin.c:252 Huff1TableInit -- `counts` is the 64 KiB table (256 rows of 256 counts)
  init(counts: Uint8Array): void {
    if (counts.length < 65536) throw new FormatError('cin: truncated huffman count table');
    this.hnodes1.fill(0);
    for (let prev = 0; prev < 256; prev++) {
      this.h_count.fill(0);
      this.h_used.fill(0);
      for (let j = 0; j < 256; j++) this.h_count[j] = counts[prev * 256 + j]!;
      let numhnodes = 256;
      const nodebase = prev * 256 * 2;
      while (numhnodes !== 511) {
        const node = nodebase + (numhnodes - 256) * 2;
        const n0 = this.SmallestNode1(numhnodes);
        this.hnodes1[node] = n0;
        if (n0 === -1) break;
        const n1 = this.SmallestNode1(numhnodes);
        this.hnodes1[node + 1] = n1;
        if (n1 === -1) break;
        this.h_count[numhnodes] = this.h_count[n0]! + this.h_count[n1]!;
        numhnodes++;
      }
      this.numhnodes1[prev] = numhnodes - 1;
    }
  }

  // C: client/cl_cin.c:302 Huff1Decompress
  // `input` is the compressed block (4-byte little-endian output count followed by the bit stream, LSB
  // first). The C decoder may read one byte past the block; bytes past the end read as 0 here (C reads
  // stale buffer contents). Returns the output and how many input bytes were consumed.
  decompress(input: Uint8Array): { data: Uint8Array; consumed: number } {
    if (input.length < 4) throw new FormatError('cin: compressed block too small');
    let count = (input[0]! + (input[1]! << 8) + (input[2]! << 16) + (input[3]! << 24)) | 0;
    if (count < 0 || count > 0x4000000) throw new FormatError('cin: bad decompressed size');
    const out = new Uint8Array(count);
    let o = 0;
    let ip = 4;
    const nodes = this.hnodes1;
    // hnodesbase = hnodes1 - 256*2: node n of table `prev` is at prev*512 + n*2 - 512
    let hbase = -512;
    let nodenum = this.numhnodes1[0]!;
    outer: while (count) {
      let inbyte = ip < input.length ? input[ip]! : 0;
      ip++;
      for (let bit = 0; bit < 8; bit++) {
        if (nodenum < 256) {
          hbase = -512 + (nodenum << 9);
          out[o++] = nodenum;
          if (!--count) break outer;
          nodenum = this.numhnodes1[nodenum]!;
        }
        const idx = hbase + nodenum * 2 + (inbyte & 1);
        nodenum = idx >= 0 && idx < nodes.length ? nodes[idx]! : 0;
        inbyte >>= 1;
      }
    }
    return { data: out.subarray(0, o), consumed: ip };
  }
}

/** Streaming reader over a whole .cin file (SCR_PlayCinematic header + SCR_ReadNextFrame). */
export class CinReader {
  readonly header: CinHeader;
  readonly huff = new Huff1Tables();
  /** cl.cinematicframe */
  frame = 0;
  private pos = 0;
  private readonly bytes: Uint8Array;
  private readonly dv: DataView;

  constructor(data: ArrayBuffer | Uint8Array) {
    this.bytes = toU8(data);
    this.dv = new DataView(this.bytes.buffer, this.bytes.byteOffset, this.bytes.byteLength);
    // C: client/cl_cin.c:612 SCR_PlayCinematic (header part)
    this.header = {
      width: this.readLong(),
      height: this.readLong(),
      s_rate: this.readLong(),
      s_width: this.readLong(),
      s_channels: this.readLong(),
    };
    this.huff.init(this.read(65536));
  }

  private read(n: number): Uint8Array {
    if (n < 0 || this.pos + n > this.bytes.length) throw new FormatError('cin: FS_Read past end of file');
    const r = this.bytes.subarray(this.pos, this.pos + n);
    this.pos += n;
    return r;
  }

  private readLong(): number {
    if (this.pos + 4 > this.bytes.length) throw new FormatError('cin: FS_Read past end of file');
    const v = this.dv.getInt32(this.pos, true);
    this.pos += 4;
    return v;
  }

  // C: client/cl_cin.c:421 SCR_ReadNextFrame. Returns null at end of file or the last-frame marker.
  readNextFrame(): CinFrame | null {
    if (this.pos + 4 > this.bytes.length) return null;
    const command = this.readLong();
    if (command === 2) return null; // last frame marker
    let palette: Uint8Array | null = null;
    if (command === 1) palette = this.read(768).slice();
    const size = this.readLong();
    if (size > 0x20000 || size < 1) throw new FormatError('Bad compressed frame size');
    const compressed = this.read(size);

    const { s_rate, s_width, s_channels } = this.header;
    const start = Math.trunc((this.frame * s_rate) / 14);
    const end = Math.trunc(((this.frame + 1) * s_rate) / 14);
    const count = end - start;
    const samples = this.read(count * s_width * s_channels).slice();

    const pic = this.huff.decompress(compressed).data;
    this.frame++;
    return { command, palette, pic, samples, count };
  }
}
