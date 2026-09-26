// Port of qcommon/cvar.c -- dynamic variable tracking (client-side registry).
// The C linked list `cvar_vars` (new variables are linked at the head) is kept as an array iterated
// newest-first so that cvarlist / Cvar_BitInfo produce the same ordering as the original.
import {
  CVAR_ARCHIVE,
  CVAR_LATCH,
  CVAR_NOSET,
  CVAR_SERVERINFO,
  CVAR_USERINFO,
  Info_SetValueForKey,
  atof,
  fr,
  sprintf,
} from 'q2-shared';

// C: q_shared.h cvar_t
export class Cvar {
  latched_string: string | null = null; // for CVAR_LATCH vars
  modified = true; // set each time the cvar is changed
  /** float */
  value: number;
  constructor(
    readonly name: string,
    public string: string,
    public flags: number,
  ) {
    this.value = fr(atof(string));
  }
}

/** Command-argument accessor used by the cvar console commands (implemented by CmdSystem). */
export interface CmdArgs {
  argc(): number;
  argv(i: number): string;
}

export class CvarSystem {
  /** cvar_vars, oldest first (iterate backwards for the C order). */
  private readonly vars: Cvar[] = [];
  private readonly byName = new Map<string, Cvar>();

  /** C: cvar.c userinfo_modified */
  userinfoModified = false;

  /** Com_Printf */
  print: (msg: string) => void = () => {};
  /** Com_ServerState(): a browser client never runs a local server. */
  serverState: () => number = () => 0;
  /** FS_SetGamedir + FS_ExecAutoexec when "game" changes (hook). */
  onGameChanged: ((game: string) => void) | null = null;
  /** Notified after a variable's string changed (engine `config` events, archive persistence). */
  onChange: ((v: Cvar) => void) | null = null;

  private changed(v: Cvar): void {
    this.onChange?.(v);
  }

  // C: cvar.c:31 Cvar_InfoValidate
  static infoValidate(s: string): boolean {
    if (s.includes('\\')) return false;
    if (s.includes('"')) return false;
    if (s.includes(';')) return false;
    return true;
  }

  // C: cvar.c:47 Cvar_FindVar
  find(var_name: string): Cvar | null {
    return this.byName.get(var_name) ?? null;
  }

  /** All variables in C list order (newest first). */
  list(): Cvar[] {
    return this.vars.slice().reverse();
  }

  // C: cvar.c:64 Cvar_VariableValue
  variableValue(var_name: string): number {
    const v = this.find(var_name);
    if (!v) return 0;
    return fr(atof(v.string));
  }

  // C: cvar.c:80 Cvar_VariableString
  variableString(var_name: string): string {
    const v = this.find(var_name);
    if (!v) return '';
    return v.string;
  }

  // C: cvar.c:96 Cvar_CompleteVariable
  completeVariable(partial: string): string | null {
    const len = partial.length;
    if (!len) return null;
    const l = this.list();
    // check exact match
    for (const v of l) if (partial === v.name) return v.name;
    // check partial match
    for (const v of l) if (v.name.startsWith(partial)) return v.name;
    return null;
  }

  /**
   * C: cvar.c:126 Cvar_Get. Returns null exactly where C returns NULL (invalid info names/values, or a
   * missing variable with a null default).
   */
  getOrNull(var_name: string, var_value: string | null, flags: number): Cvar | null {
    if (flags & (CVAR_USERINFO | CVAR_SERVERINFO)) {
      if (!CvarSystem.infoValidate(var_name)) {
        this.print('invalid info cvar name\n');
        return null;
      }
    }

    let v = this.find(var_name);
    if (v) {
      v.flags |= flags;
      return v;
    }

    if (var_value === null) return null;

    if (flags & (CVAR_USERINFO | CVAR_SERVERINFO)) {
      if (!CvarSystem.infoValidate(var_value)) {
        this.print('invalid info cvar value\n');
        return null;
      }
    }

    v = new Cvar(var_name, var_value, flags);
    v.modified = true;
    // link the variable in
    this.vars.push(v);
    this.byName.set(var_name, v);
    return v;
  }

  /**
   * Cvar_Get for engine code that registers its own (always valid) variables. Where C would return NULL
   * a detached, unregistered cvar is returned instead of crashing later on the NULL dereference.
   */
  get(var_name: string, var_value: string, flags: number): Cvar {
    return this.getOrNull(var_name, var_value, flags) ?? new Cvar(var_name, var_value, flags);
  }

  // C: cvar.c:182 Cvar_Set2
  set2(var_name: string, value: string, force: boolean): Cvar | null {
    const v = this.find(var_name);
    if (!v) {
      // create it
      const nv = this.getOrNull(var_name, value, 0);
      if (nv) this.changed(nv);
      return nv;
    }

    if (v.flags & (CVAR_USERINFO | CVAR_SERVERINFO)) {
      if (!CvarSystem.infoValidate(value)) {
        this.print('invalid info cvar value\n');
        return v;
      }
    }

    if (!force) {
      if (v.flags & CVAR_NOSET) {
        this.print(sprintf('%s is write protected.\n', var_name));
        return v;
      }

      if (v.flags & CVAR_LATCH) {
        if (v.latched_string !== null) {
          if (value === v.latched_string) return v;
          v.latched_string = null;
        } else {
          if (value === v.string) return v;
        }

        if (this.serverState()) {
          this.print(sprintf('%s will be changed for next game.\n', var_name));
          v.latched_string = value;
        } else {
          v.string = value;
          v.value = fr(atof(v.string));
          if (v.name === 'game') this.onGameChanged?.(v.string);
          this.changed(v);
        }
        return v;
      }
    } else {
      if (v.latched_string !== null) v.latched_string = null;
    }

    if (value === v.string) return v; // not changed

    v.modified = true;

    if (v.flags & CVAR_USERINFO) this.userinfoModified = true; // transmit at next oportunity

    v.string = value;
    v.value = fr(atof(v.string));
    this.changed(v);
    return v;
  }

  // C: cvar.c:271 Cvar_ForceSet
  forceSet(var_name: string, value: string): Cvar | null {
    return this.set2(var_name, value, true);
  }

  // C: cvar.c:281 Cvar_Set
  set(var_name: string, value: string): Cvar | null {
    return this.set2(var_name, value, false);
  }

  // C: cvar.c:291 Cvar_FullSet
  fullSet(var_name: string, value: string, flags: number): Cvar | null {
    const v = this.find(var_name);
    if (!v) {
      // create it
      const nv = this.getOrNull(var_name, value, flags);
      if (nv) this.changed(nv);
      return nv;
    }

    v.modified = true;

    if (v.flags & CVAR_USERINFO) this.userinfoModified = true; // transmit at next oportunity

    v.string = value;
    v.value = fr(atof(v.string));
    v.flags = flags;
    this.changed(v);
    return v;
  }

  // C: cvar.c:321 Cvar_SetValue
  setValue(var_name: string, value: number): void {
    value = fr(value);
    let val: string;
    if (value === Math.trunc(value)) val = sprintf('%i', Math.trunc(value));
    else val = sprintf('%f', value);
    this.set(var_name, val.slice(0, 31));
  }

  // C: cvar.c:340 Cvar_GetLatchedVars
  getLatchedVars(): void {
    for (const v of this.list()) {
      if (v.latched_string === null) continue;
      v.string = v.latched_string;
      v.latched_string = null;
      v.value = fr(atof(v.string));
      if (v.name === 'game') this.onGameChanged?.(v.string);
      this.changed(v);
    }
  }

  // C: cvar.c:366 Cvar_Command -- handles variable inspection and changing from the console
  command(args: CmdArgs): boolean {
    // check variables
    const v = this.find(args.argv(0));
    if (!v) return false;

    // perform a variable print or set
    if (args.argc() === 1) {
      this.print(sprintf('"%s" is "%s"\n', v.name, v.string));
      return true;
    }

    this.set(v.name, args.argv(1));
    return true;
  }

  // C: cvar.c:393 Cvar_Set_f -- allows setting and defining of arbitrary cvars from console
  set_f(args: CmdArgs): void {
    const c = args.argc();
    if (c !== 3 && c !== 4) {
      this.print('usage: set <variable> <value> [u / s]\n');
      return;
    }

    if (c === 4) {
      let flags: number;
      if (args.argv(3) === 'u') flags = CVAR_USERINFO;
      else if (args.argv(3) === 's') flags = CVAR_SERVERINFO;
      else {
        this.print("flags can only be 'u' or 's'\n");
        return;
      }
      this.fullSet(args.argv(1), args.argv(2), flags);
    } else this.set(args.argv(1), args.argv(2));
  }

  /**
   * Addition (not in 3.19; later engines): `seta`/`setu`/`sets` = set and add CVAR_ARCHIVE/USERINFO/SERVERINFO.
   */
  setFlag_f(args: CmdArgs, flag: number, cmdName: string): void {
    if (args.argc() !== 3) {
      this.print(sprintf('usage: %s <variable> <value>\n', cmdName));
      return;
    }
    const v = this.find(args.argv(1));
    if (v) {
      this.set(args.argv(1), args.argv(2));
      v.flags |= flag;
      if (flag & CVAR_USERINFO) this.userinfoModified = true;
      this.changed(v);
    } else {
      this.fullSet(args.argv(1), args.argv(2), flag);
    }
  }

  /** Addition (not in 3.19): `toggle <cvar>` flips between 0 and 1. */
  toggle_f(args: CmdArgs): void {
    if (args.argc() !== 2) {
      this.print('usage: toggle <variable>\n');
      return;
    }
    const v = this.find(args.argv(1));
    if (!v) {
      this.print(sprintf('%s is not a variable\n', args.argv(1)));
      return;
    }
    this.set(v.name, v.value ? '0' : '1');
  }

  // C: cvar.c:432 Cvar_WriteVariables -- returns the text C appends to config.cfg
  // The text is saved to the player's account and executed at every start (deviation from C for
  // values/names that would not parse back): a '"' in a value is replaced by '\'' (C: FIXME, the rest of
  // the line would run as commands) and variables whose name contains a quote, ';' or whitespace are not
  // written.
  writeVariables(): string {
    let out = '';
    for (const v of this.list()) {
      if (!(v.flags & CVAR_ARCHIVE) || /[\x00-\x20";]/.test(v.name)) continue;
      out += sprintf('set %s "%s"\n', v.name, v.string.replace(/"/g, "'")).slice(0, 1023);
    }
    return out;
  }

  // C: cvar.c:455 Cvar_List_f
  list_f(): void {
    let i = 0;
    for (const v of this.list()) {
      let s = '';
      s += v.flags & CVAR_ARCHIVE ? '*' : ' ';
      s += v.flags & CVAR_USERINFO ? 'U' : ' ';
      s += v.flags & CVAR_SERVERINFO ? 'S' : ' ';
      if (v.flags & CVAR_NOSET) s += '-';
      else if (v.flags & CVAR_LATCH) s += 'L';
      else s += ' ';
      this.print(s);
      this.print(sprintf(' %s "%s"\n', v.name, v.string));
      i++;
    }
    this.print(sprintf('%i cvars\n', i));
  }

  // C: cvar.c:490 Cvar_BitInfo
  bitInfo(bit: number): string {
    let info = '';
    for (const v of this.list()) {
      if (v.flags & bit) info = Info_SetValueForKey(info, v.name, v.string, this.print);
    }
    return info;
  }

  // C: cvar.c:505 Cvar_Userinfo -- an info string containing all the CVAR_USERINFO cvars
  userinfo(): string {
    return this.bitInfo(CVAR_USERINFO);
  }

  // C: cvar.c:511 Cvar_Serverinfo
  serverinfo(): string {
    return this.bitInfo(CVAR_SERVERINFO);
  }
}
