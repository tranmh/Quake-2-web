// Port of qcommon/cmd.c -- Quake script command processing module.
// Differences forced by the browser: `exec` loads its file through an asynchronous loader. While the
// load is pending, Cbuf_Execute stops (like `wait`) and resumes once the file text has been inserted at
// the head of the buffer, so command ordering is identical to the synchronous original.
import {
  CVAR_ARCHIVE,
  CVAR_SERVERINFO,
  CVAR_USERINFO,
  EXEC_APPEND,
  EXEC_INSERT,
  EXEC_NOW,
  MAX_STRING_CHARS,
  MAX_STRING_TOKENS,
  COM_Parse,
  Q_strcasecmp,
  cstr,
  latin1FromBytes,
  sprintf,
  type ParseCursor,
} from 'q2-shared';
import type { CmdArgs, CvarSystem } from './cvar';

const MAX_ALIAS_NAME = 32;
const ALIAS_LOOP_COUNT = 16;
const CMD_TEXT_SIZE = 8192;

export type XCommand = () => void;

interface CmdFunction {
  name: string;
  /** null = forward to server ("cmd <text>") */
  fn: XCommand | null;
}

interface CmdAlias {
  name: string;
  value: string;
  /** defined by server-originated text: its expansion stays restricted */
  restricted: boolean;
}

/**
 * Commands a server may not run through svc_stufftext (browser deviation, docs/review/04-ts-engine.md).
 * The original executed stuffed text like console input. In the browser client the key bindings and
 * archived cvars are saved to the player's account as soon as they change, so a hostile server could
 * otherwise plant bindings/settings that persist across sessions (and, via the saved config, run commands
 * at every start), send the rcon password of another server back to itself, or spam file downloads.
 */
// `screenshot` (registered by the renderer) hands a PNG download to the browser each time.
const SERVER_BLOCKED_COMMANDS = new Set(['bind', 'unbind', 'unbindall', 'seta', 'rcon', 'screenshot']);
/** cvar-setting commands whose target (argv 1) is checked with protectedFromServer */
const SERVER_CVAR_COMMANDS = new Set(['set', 'setu', 'sets', 'toggle']);
/** never readable ($macro) or writable from server text */
const SECRET_CVAR = 'rcon_password';

/** Signed-char test `c <= ' '` of the original (bytes >= 128 are negative chars). */
function isSpaceChar(c: number): boolean {
  return c <= 32 || c >= 128;
}

export class CmdSystem implements CmdArgs {
  // command buffer (C: cmd_text / cmd_text_buf / defer_text_buf)
  private cmd_text = '';
  private defer_text = '';
  /**
   * Origin of every character of cmd_text / defer_text: '1' = server text (svc_stufftext, or text an
   * alias/exec inserted while running server text), '0' = local. A line containing any server character
   * runs restricted.
   */
  private cmd_origin = '';
  private defer_origin = '';
  /** the command being executed came from the server (see SERVER_BLOCKED_COMMANDS) */
  private executingRestricted = false;
  /** C: cmd_wait */
  cmd_wait = false;
  /** C: alias_count -- for detecting runaway loops */
  private alias_count = 0;

  /** C: cmd_alias (newest first) */
  private readonly aliases: CmdAlias[] = [];
  /** C: cmd_functions (newest first) */
  private readonly functions: CmdFunction[] = [];

  private cmd_argc = 0;
  private readonly cmd_argv: string[] = [];
  private cmd_args = '';

  /** Pending asynchronous `exec` loads; Cbuf_Execute is blocked while non-zero. */
  private pendingExec = 0;
  private idleWaiters: (() => void)[] = [];

  /** Com_Printf */
  print: (msg: string) => void = () => {};
  /** FS_LoadFile (async; null = not found) */
  loadFile: (name: string) => Promise<Uint8Array | null> = async () => null;
  /** Cmd_ForwardToServer (cl_main.c) */
  forwardToServer: () => void = () => {};

  constructor(readonly cvars: CvarSystem) {
    this.init();
  }

  // C: cmd.c:879 Cmd_Init (+ cvar.c:521 Cvar_Init)
  private init(): void {
    this.addCommand('cmdlist', () => this.list_f());
    this.addCommand('exec', () => this.exec_f());
    this.addCommand('echo', () => this.echo_f());
    this.addCommand('alias', () => this.alias_f());
    this.addCommand('wait', () => this.wait_f());
    // Cvar_Init
    this.addCommand('set', () => this.cvars.set_f(this));
    this.addCommand('cvarlist', () => this.cvars.list_f());
    // additions (not in 3.19)
    this.addCommand('seta', () => this.cvars.setFlag_f(this, CVAR_ARCHIVE, 'seta'));
    this.addCommand('setu', () => this.cvars.setFlag_f(this, CVAR_USERINFO, 'setu'));
    this.addCommand('sets', () => this.cvars.setFlag_f(this, CVAR_SERVERINFO, 'sets'));
    this.addCommand('toggle', () => this.cvars.toggle_f(this));
  }

  // C: cmd.c:52 Cmd_Wait_f
  private wait_f(): void {
    this.cmd_wait = true;
  }

  /** Raw access to the command buffer (tests). */
  get text(): string {
    return this.cmd_text;
  }

  // C: cmd.c:87 Cbuf_AddText -- adds command text at the end of the buffer.
  // `fromServer`: svc_stufftext text, executed restricted (see SERVER_BLOCKED_COMMANDS).
  cbufAddText(text: string, fromServer = false): void {
    text = cstr(text);
    const l = text.length;
    if (this.cmd_text.length + l >= CMD_TEXT_SIZE) {
      this.print('Cbuf_AddText: overflow\n');
      return;
    }
    this.cmd_text += text;
    this.cmd_origin += (fromServer ? '1' : '0').repeat(l);
  }

  // C: cmd.c:110 Cbuf_InsertText -- adds command text immediately after the current command
  cbufInsertText(text: string, fromServer = false): void {
    // copy off any commands still remaining in the exec buffer
    const temp = this.cmd_text;
    const tempOrigin = this.cmd_origin;
    this.cmd_text = '';
    this.cmd_origin = '';
    // add the entire text of the file
    this.cbufAddText(text, fromServer);
    // add the copied off data
    if (temp.length) {
      // SZ_Write on a buffer without allowoverflow: Com_Error(ERR_FATAL) in C; clamp instead.
      this.cmd_text = (this.cmd_text + temp).slice(0, CMD_TEXT_SIZE);
      this.cmd_origin = (this.cmd_origin + tempOrigin).slice(0, CMD_TEXT_SIZE);
    }
  }

  /** Inserts text with per-character origins (defer buffer round trip). */
  private insertWithOrigin(text: string, origin: string): void {
    const temp = this.cmd_text;
    const tempOrigin = this.cmd_origin;
    this.cmd_text = (text + temp).slice(0, CMD_TEXT_SIZE);
    this.cmd_origin = (origin + tempOrigin).slice(0, CMD_TEXT_SIZE);
  }

  // C: cmd.c:143 Cbuf_CopyToDefer
  cbufCopyToDefer(): void {
    this.defer_text = this.cmd_text;
    this.defer_origin = this.cmd_origin;
    this.cmd_text = '';
    this.cmd_origin = '';
  }

  // C: cmd.c:155 Cbuf_InsertFromDefer
  cbufInsertFromDefer(): void {
    this.insertWithOrigin(this.defer_text, this.defer_origin);
    this.defer_text = '';
    this.defer_origin = '';
  }

  // C: cmd.c:167 Cbuf_ExecuteText
  cbufExecuteText(exec_when: number, text: string): void {
    switch (exec_when) {
      case EXEC_NOW:
        this.executeString(text);
        break;
      case EXEC_INSERT:
        this.cbufInsertText(text);
        break;
      case EXEC_APPEND:
        this.cbufAddText(text);
        break;
      default:
        throw new Error('Cbuf_ExecuteText: bad exec_when');
    }
  }

  // C: cmd.c:191 Cbuf_Execute
  cbufExecute(): void {
    this.alias_count = 0; // don't allow infinite alias loops

    while (this.cmd_text.length) {
      if (this.pendingExec) break; // waiting for an asynchronous exec (see header)
      // find a \n or ; line break
      const text = this.cmd_text;
      let quotes = 0;
      let i: number;
      for (i = 0; i < text.length; i++) {
        const ch = text.charCodeAt(i);
        if (ch === 34) quotes++;
        if (!(quotes & 1) && ch === 59) break; // don't break if inside a quoted string
        if (ch === 10) break;
      }

      // char line[1024]: the C memcpy would overflow; truncated here
      const line = text.slice(0, Math.min(i, 1023));
      const fromServer = this.cmd_origin.slice(0, i + 1).includes('1');

      // delete the text from the command buffer and move remaining commands down
      // this is necessary because commands (exec, alias) can insert data at the
      // beginning of the text buffer
      if (i === text.length) {
        this.cmd_text = '';
        this.cmd_origin = '';
      } else {
        this.cmd_text = text.slice(i + 1);
        this.cmd_origin = this.cmd_origin.slice(i + 1);
      }

      // execute the command line
      this.executeString(line, fromServer);

      if (this.cmd_wait) {
        // skip out while text still remains in buffer, leaving it for next frame
        this.cmd_wait = false;
        break;
      }
    }
    this.checkIdle();
  }

  // C: cmd.c:250 Cbuf_AddEarlyCommands -- `+set a b` pairs from a command line
  cbufAddEarlyCommands(argv: string[]): void {
    for (let i = 0; i < argv.length; i++) {
      if (argv[i] !== '+set') continue;
      this.cbufAddText(sprintf('set %s %s\n', argv[i + 1] ?? '', argv[i + 2] ?? ''));
      i += 2;
    }
  }

  // C: cmd.c:283 Cbuf_AddLateCommands (argv[0] is the program name as in C)
  cbufAddLateCommands(argv: string[]): boolean {
    let s = 0;
    for (let i = 1; i < argv.length; i++) s += argv[i]!.length + 1;
    if (!s) return false;
    const text = argv.slice(1).join(' ');
    let build = '';
    for (let i = 0; i < s - 1; i++) {
      if (text[i] === '+') {
        i++;
        let j = i;
        while (j < text.length && text[j] !== '+' && text[j] !== '-') j++;
        build += text.slice(i, j) + '\n';
        i = j - 1;
      }
    }
    const ret = build.length !== 0;
    if (ret) this.cbufAddText(build);
    return ret;
  }

  /** Resolves once the buffer is empty and no `exec` is loading (tests / engine startup). */
  whenIdle(): Promise<void> {
    if (!this.pendingExec && !this.cmd_text.length) return Promise.resolve();
    return new Promise((resolve) => this.idleWaiters.push(resolve));
  }

  private checkIdle(): void {
    if (this.pendingExec || this.cmd_text.length || !this.idleWaiters.length) return;
    const w = this.idleWaiters;
    this.idleWaiters = [];
    for (const f of w) f();
  }

  // C: cmd.c:354 Cmd_Exec_f
  private exec_f(): void {
    if (this.argc() !== 2) {
      this.print('exec <filename> : execute a script file\n');
      return;
    }
    const name = this.argv(1);
    const fromServer = this.executingRestricted; // the file runs with the rights of the exec line
    this.pendingExec++;
    const done = (text: string | null): void => {
      if (text === null) this.print(sprintf("couldn't exec %s\n", name));
      else {
        this.print(sprintf('execing %s\n', name));
        // the file doesn't have a trailing 0, so we need to copy it off
        this.cbufInsertText(text, fromServer);
      }
      this.pendingExec--;
      this.cbufExecute();
    };
    this.loadFile(name).then(
      (f) => done(f ? cstr(latin1FromBytes(f)) : null),
      () => done(null),
    );
  }

  // C: cmd.c:385 Cmd_Echo_f -- just prints the rest of the line to the console
  private echo_f(): void {
    for (let i = 1; i < this.argc(); i++) this.print(sprintf('%s ', this.argv(i)));
    this.print('\n');
  }

  // C: cmd.c:400 Cmd_Alias_f -- creates a new command that executes a command string
  private alias_f(): void {
    if (this.argc() === 1) {
      this.print('Current alias commands:\n');
      for (const a of this.aliases) this.print(sprintf('%s : %s\n', a.name, a.value));
      return;
    }

    const s = this.argv(1);
    if (s.length >= MAX_ALIAS_NAME) {
      this.print('Alias name is too long\n');
      return;
    }

    // if the alias already exists, reuse it
    let a = this.aliases.find((x) => x.name === s);
    if (!a) {
      a = { name: s, value: '', restricted: false };
      this.aliases.unshift(a);
    }
    a.restricted = this.executingRestricted;

    // copy the rest of the command line
    let cmd = ''; // start out with a null string
    const c = this.argc();
    for (let i = 2; i < c; i++) {
      cmd += this.argv(i);
      if (i !== c - 1) cmd += ' ';
    }
    cmd += '\n';
    a.value = cmd;
  }

  /** Alias value lookup (for tests/UI). */
  aliasValue(name: string): string | null {
    return this.aliases.find((x) => x.name === name)?.value ?? null;
  }

  // C: cmd.c:489 Cmd_Argc
  argc(): number {
    return this.cmd_argc;
  }

  // C: cmd.c:499 Cmd_Argv
  argv(arg: number): string {
    if (arg < 0 || arg >= this.cmd_argc) return '';
    return this.cmd_argv[arg]!;
  }

  // C: cmd.c:513 Cmd_Args -- a single string containing argv(1) to argv(argc()-1)
  args(): string {
    return this.cmd_args;
  }

  // C: cmd.c:524 Cmd_MacroExpandString (`fromServer`: $rcon_password expands to "")
  macroExpandString(text: string, fromServer = false): string | null {
    let inquote = false;
    let scan = text;
    let len = scan.length;
    if (len >= MAX_STRING_CHARS) {
      this.print(sprintf('Line exceeded %i chars, discarded.\n', MAX_STRING_CHARS));
      return null;
    }

    let count = 0;
    for (let i = 0; i < len; i++) {
      if (scan[i] === '"') inquote = !inquote;
      if (inquote) continue; // don't expand inside quotes
      if (scan[i] !== '$') continue;
      // scan out the complete macro
      const cur: ParseCursor = { data: scan, pos: i + 1 };
      let token = COM_Parse(cur);
      if (cur.pos < 0) continue;

      token = fromServer && token === SECRET_CVAR ? '' : this.cvars.variableString(token);

      const j = token.length;
      len += j;
      if (len >= MAX_STRING_CHARS) {
        this.print(sprintf('Expanded line exceeded %i chars, discarded.\n', MAX_STRING_CHARS));
        return null;
      }

      scan = scan.slice(0, i) + token + scan.slice(cur.pos);
      i--;

      if (++count === 100) {
        this.print('Macro expansion loop, discarded.\n');
        return null;
      }
    }

    if (inquote) {
      this.print('Line has unmatched quote, discarded.\n');
      return null;
    }

    return scan;
  }

  // C: cmd.c:593 Cmd_TokenizeString -- $Cvars will be expanded unless they are in a quoted token
  tokenizeString(input: string, macroExpand: boolean, fromServer = false): void {
    // clear the args from the last string
    this.cmd_argc = 0;
    this.cmd_argv.length = 0;
    this.cmd_args = '';

    // macro expand the text
    let text: string | null = cstr(input);
    if (macroExpand) text = this.macroExpandString(text, fromServer);
    if (text === null) return;

    const cur: ParseCursor = { data: text, pos: 0 };
    while (1) {
      // skip whitespace up to a /n
      while (cur.pos < text.length && isSpaceChar(text.charCodeAt(cur.pos)) && text[cur.pos] !== '\n')
        cur.pos++;

      if (text[cur.pos] === '\n') {
        // a newline seperates commands in the buffer
        cur.pos++;
        break;
      }

      if (cur.pos >= text.length) return;

      // set cmd_args to everything after the first arg
      if (this.cmd_argc === 1) {
        let a = text.slice(cur.pos);
        // strip off any trailing whitespace
        let l = a.length - 1;
        while (l >= 0 && isSpaceChar(a.charCodeAt(l))) l--;
        a = a.slice(0, l + 1);
        this.cmd_args = a.slice(0, MAX_STRING_CHARS - 1);
      }

      const com_token = COM_Parse(cur);
      if (cur.pos < 0) return;

      if (this.cmd_argc < MAX_STRING_TOKENS) {
        this.cmd_argv[this.cmd_argc] = com_token;
        this.cmd_argc++;
      }
    }
  }

  // C: cmd.c:656 Cmd_AddCommand (fn null = forward to server)
  addCommand(cmd_name: string, fn: XCommand | null): void {
    // fail if the command is a variable name
    if (this.cvars.variableString(cmd_name)[0]) {
      this.print(sprintf('Cmd_AddCommand: %s already defined as a var\n', cmd_name));
      return;
    }
    // fail if the command already exists
    if (this.functions.some((c) => c.name === cmd_name)) {
      this.print(sprintf('Cmd_AddCommand: %s already defined\n', cmd_name));
      return;
    }
    this.functions.unshift({ name: cmd_name, fn });
  }

  // C: cmd.c:688 Cmd_RemoveCommand
  removeCommand(cmd_name: string): void {
    const i = this.functions.findIndex((c) => c.name === cmd_name);
    if (i < 0) {
      this.print(sprintf('Cmd_RemoveCommand: %s not added\n', cmd_name));
      return;
    }
    this.functions.splice(i, 1);
  }

  // C: cmd.c:717 Cmd_Exists
  exists(cmd_name: string): boolean {
    return this.functions.some((c) => c.name === cmd_name);
  }

  /** Command names in C list order (newest first). */
  commandNames(): string[] {
    return this.functions.map((c) => c.name);
  }

  // C: cmd.c:737 Cmd_CompleteCommand
  completeCommand(partial: string): string | null {
    const len = partial.length;
    if (!len) return null;
    // check for exact match
    for (const c of this.functions) if (partial === c.name) return c.name;
    for (const a of this.aliases) if (partial === a.name) return a.name;
    // check for partial match
    for (const c of this.functions) if (c.name.startsWith(partial)) return c.name;
    for (const a of this.aliases) if (a.name.startsWith(partial)) return a.name;
    return null;
  }

  /** True if server text must not write cvar `name` (archived = saved to the account, or the secret). */
  private protectedFromServer(name: string): boolean {
    if (name === SECRET_CVAR) return true;
    const v = this.cvars.find(name);
    return !!v && (v.flags & CVAR_ARCHIVE) !== 0;
  }

  private refuseFromServer(): void {
    this.print(sprintf('Ignored server command "%s" (not allowed from a server)\n', this.cmd_argv[0]!));
  }

  // C: cmd.c:772 Cmd_ExecuteString -- a complete command line has been parsed, so try to execute it.
  // `fromServer`: the line came from svc_stufftext (see SERVER_BLOCKED_COMMANDS).
  executeString(text: string, fromServer = false): void {
    const prev = this.executingRestricted;
    this.executingRestricted = fromServer;
    try {
      this.executeLine(text, fromServer);
    } finally {
      this.executingRestricted = prev;
    }
  }

  private executeLine(text: string, fromServer: boolean): void {
    this.tokenizeString(text, true, fromServer);

    // execute the command line
    if (!this.argc()) return; // no tokens

    // check functions
    for (const cmd of this.functions) {
      if (!Q_strcasecmp(this.cmd_argv[0]!, cmd.name)) {
        if (fromServer) {
          const n = cmd.name.toLowerCase();
          if (
            SERVER_BLOCKED_COMMANDS.has(n) ||
            (SERVER_CVAR_COMMANDS.has(n) && this.protectedFromServer(this.argv(1)))
          ) {
            this.refuseFromServer();
            return;
          }
        }
        if (!cmd.fn) {
          // forward to server command
          this.executeString(sprintf('cmd %s', text), fromServer);
        } else cmd.fn();
        return;
      }
    }

    // check alias
    for (const a of this.aliases) {
      if (!Q_strcasecmp(this.cmd_argv[0]!, a.name)) {
        if (++this.alias_count === ALIAS_LOOP_COUNT) {
          this.print('ALIAS_LOOP_COUNT\n');
          return;
        }
        this.cbufInsertText(a.value, fromServer || a.restricted);
        return;
      }
    }

    // check cvars
    if (fromServer && this.argc() > 1 && this.protectedFromServer(this.cmd_argv[0]!)) {
      this.refuseFromServer();
      return;
    }
    if (this.cvars.command(this)) return;

    // send it as a server command if we are connected
    this.forwardToServer();
  }

  // C: cmd.c:830 Cmd_List_f
  private list_f(): void {
    let i = 0;
    for (const c of this.functions) {
      this.print(sprintf('%s\n', c.name));
      i++;
    }
    this.print(sprintf('%i commands\n', i));
  }
}
