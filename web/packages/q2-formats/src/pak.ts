// PAK archive reader (qcommon/files.c FS_LoadPackFile) and a multi-pak search path.
import { IDPAKHEADER, readCString } from 'q2-shared';
import { FormatError } from './errors';

export interface PakEntry {
  /** name as stored (Latin-1, up to 56 chars) */
  name: string;
  filepos: number;
  filelen: number;
}

const MAX_FILES_IN_PACK = 4096;

export class Pak {
  readonly entries: PakEntry[];
  private readonly byName = new Map<string, PakEntry>();
  private readonly bytes: Uint8Array;

  constructor(
    data: ArrayBuffer | Uint8Array,
    readonly filename = 'pak',
  ) {
    this.bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
    const b = this.bytes;
    const dv = new DataView(b.buffer, b.byteOffset, b.byteLength);
    if (b.length < 12) throw new FormatError(`${filename} is not a packfile`);
    if (dv.getInt32(0, true) !== IDPAKHEADER) throw new FormatError(`${filename} is not a packfile`);
    const dirofs = dv.getInt32(4, true);
    const dirlen = dv.getInt32(8, true);
    const numpackfiles = Math.trunc(dirlen / 64);
    if (numpackfiles > MAX_FILES_IN_PACK) throw new FormatError(`${filename} has ${numpackfiles} files`);
    if (dirofs < 0 || dirlen < 0 || dirofs + numpackfiles * 64 > b.length) {
      throw new FormatError(`${filename}: directory out of bounds`);
    }
    this.entries = [];
    for (let i = 0; i < numpackfiles; i++) {
      const o = dirofs + i * 64;
      const e: PakEntry = {
        name: readCString(b, o, 56),
        filepos: dv.getInt32(o + 56, true),
        filelen: dv.getInt32(o + 60, true),
      };
      this.entries.push(e);
      // FS_FOpenFile scans the directory front to back with Q_strcasecmp: the first match wins.
      const key = e.name.toLowerCase();
      if (!this.byName.has(key)) this.byName.set(key, e);
    }
  }

  /** Case-insensitive lookup. */
  find(name: string): PakEntry | undefined {
    return this.byName.get(name.toLowerCase());
  }

  has(name: string): boolean {
    return this.find(name) !== undefined;
  }

  /** File contents (a view into the pak buffer), or undefined if absent. Throws if out of bounds. */
  read(name: string): Uint8Array | undefined {
    const e = this.find(name);
    if (!e) return undefined;
    return this.readEntry(e);
  }

  readEntry(e: PakEntry): Uint8Array {
    if (e.filepos < 0 || e.filelen < 0 || e.filepos + e.filelen > this.bytes.length) {
      throw new FormatError(`${this.filename}: entry ${e.name} out of bounds`);
    }
    return this.bytes.subarray(e.filepos, e.filepos + e.filelen);
  }

  list(): string[] {
    return this.entries.map((e) => e.name);
  }
}

/**
 * Ordered set of paks. Paks added later override earlier ones (like pak1.pak over pak0.pak: the
 * engine searches the most recently added pack first).
 */
export class SearchPath {
  private readonly paks: Pak[] = [];

  add(pak: Pak): void {
    this.paks.push(pak);
  }

  get packs(): readonly Pak[] {
    return this.paks;
  }

  find(name: string): { pak: Pak; entry: PakEntry } | undefined {
    for (let i = this.paks.length - 1; i >= 0; i--) {
      const pak = this.paks[i]!;
      const entry = pak.find(name);
      if (entry) return { pak, entry };
    }
    return undefined;
  }

  read(name: string): Uint8Array | undefined {
    const r = this.find(name);
    return r ? r.pak.readEntry(r.entry) : undefined;
  }

  has(name: string): boolean {
    return this.find(name) !== undefined;
  }

  /** Union of all file names (lower-cased), sorted. */
  list(): string[] {
    const s = new Set<string>();
    for (const p of this.paks) for (const e of p.entries) s.add(e.name.toLowerCase());
    return [...s].sort();
  }
}
