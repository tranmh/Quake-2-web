// Package cmd ports qcommon/cmd.c: the command buffer, tokenizer, macro
// expansion, aliases and the command table. All C globals live on a
// per-instance Cmd.
package cmd

import (
	"fmt"
	"strings"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

// C: qcommon/cmd.c:26 MAX_ALIAS_NAME
const maxAliasName = 32

// C: qcommon/cmd.c:39 ALIAS_LOOP_COUNT
const aliasLoopCount = 16

// cmdTextSize is sizeof(cmd_text_buf) / sizeof(defer_text_buf).
// C: qcommon/cmd.c:69 cmd_text_buf
const cmdTextSize = 8192

// XCommand is C xcommand_t. A command reads its arguments through the Cmd
// instance (Argc/Argv/Args), like the C code. A nil function forwards the
// command line to the server ("cmd ...").
type XCommand = func()

// C: qcommon/cmd.c:28 cmdalias_t
type cmdAlias struct {
	next  *cmdAlias
	name  string
	value string
}

// C: qcommon/cmd.c:487 cmd_function_t
type cmdFunction struct {
	next     *cmdFunction
	name     string
	function XCommand
}

// Cmd holds the state of qcommon/cmd.c for one engine instance.
type Cmd struct {
	// Cvars is used for $macro expansion, Cmd_AddCommand's var check and
	// Cvar_Command. May be nil (no cvars).
	Cvars *cvar.Registry
	// Printf receives Com_Printf output; nil discards it.
	Printf func(format string, args ...any)
	// LoadFile is FS_LoadFile for "exec"; a nil or error result means the
	// file was not found.
	LoadFile func(name string) ([]byte, error)
	// ForwardToServer is Cmd_ForwardToServer. nil uses the dedicated server
	// version from null/cl_null.c (prints "Unknown command").
	ForwardToServer func()
	// MacroAllow, when set, restricts $name expansion to the cvars it
	// accepts; any other name expands to "" (like an unknown cvar). The
	// server sets it while tokenizing client commands. Not in C.
	MacroAllow func(name string) bool

	text      []byte // cmd_text (cursize = len)
	deferText []byte // defer_text_buf (C string)
	wait      bool   // cmd_wait
	aliasCnt  int    // alias_count

	aliases   *cmdAlias    // cmd_alias
	functions *cmdFunction // cmd_functions

	argv []string // cmd_argv[0..cmd_argc)
	args string   // cmd_args

	// expanded is the static buffer of Cmd_MacroExpandString. Its stale tail
	// is observable (see MacroExpandString), so it persists per instance.
	expanded [q2const.MAX_STRING_CHARS]byte
}

// New returns a Cmd with an initialized command buffer (Cbuf_Init).
func New(cvars *cvar.Registry) *Cmd {
	return &Cmd{Cvars: cvars, text: make([]byte, 0, cmdTextSize)}
}

func (c *Cmd) printf(format string, args ...any) {
	if c.Printf != nil {
		c.Printf(format, args...)
	}
}

func (c *Cmd) cvarString(name string) string {
	if c.Cvars == nil {
		return ""
	}
	return c.Cvars.VariableString(name)
}

// cstr truncates at the first NUL, like C string functions see it.
func cstr(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

//=============================================================================

// Wait_f causes execution of the remainder of the command buffer to be
// delayed until next frame.
// C: qcommon/cmd.c:54 Cmd_Wait_f
func (c *Cmd) Wait_f() {
	c.wait = true
}

/*
=============================================================================

						COMMAND BUFFER

=============================================================================
*/

// Cbuf_Init initializes the command buffer.
// C: qcommon/cmd.c:78 Cbuf_Init
func (c *Cmd) Cbuf_Init() {
	c.text = make([]byte, 0, cmdTextSize)
}

// Cbuf_AddText adds command text at the end of the buffer.
// C: qcommon/cmd.c:90 Cbuf_AddText
func (c *Cmd) Cbuf_AddText(text string) {
	text = cstr(text)
	l := len(text)
	if len(c.text)+l >= cmdTextSize {
		c.printf("Cbuf_AddText: overflow\n")
		return
	}
	c.text = append(c.text, text...)
}

// Cbuf_InsertText adds command text immediately after the current command.
// C: qcommon/cmd.c:114 Cbuf_InsertText
func (c *Cmd) Cbuf_InsertText(text string) {
	// copy off any commands still remaining in the exec buffer
	temp := append([]byte(nil), c.text...)
	c.text = c.text[:0]

	// add the entire text of the file
	c.Cbuf_AddText(text)

	// add the copied off data (always fits: it fit before)
	c.text = append(c.text, temp...)
}

// Cbuf_CopyToDefer moves the command buffer to the defer buffer.
// C: qcommon/cmd.c:147 Cbuf_CopyToDefer
func (c *Cmd) Cbuf_CopyToDefer() {
	c.deferText = append(c.deferText[:0], c.text...)
	c.text = c.text[:0]
}

// Cbuf_InsertFromDefer inserts the defer buffer and clears it.
// C: qcommon/cmd.c:159 Cbuf_InsertFromDefer
func (c *Cmd) Cbuf_InsertFromDefer() {
	c.Cbuf_InsertText(string(c.deferText))
	c.deferText = c.deferText[:0]
}

// Cbuf_ExecuteText executes, inserts or appends text.
// C: qcommon/cmd.c:171 Cbuf_ExecuteText
func (c *Cmd) Cbuf_ExecuteText(execWhen int, text string) {
	switch execWhen {
	case q2const.EXEC_NOW:
		c.ExecuteString(text)
	case q2const.EXEC_INSERT:
		c.Cbuf_InsertText(text)
	case q2const.EXEC_APPEND:
		c.Cbuf_AddText(text)
	default:
		shared.Error(q2const.ERR_FATAL, "Cbuf_ExecuteText: bad exec_when")
	}
}

// Cbuf_Execute runs buffered commands until the buffer is empty or a
// command calls "wait".
// C: qcommon/cmd.c:194 Cbuf_Execute
func (c *Cmd) Cbuf_Execute() {
	c.aliasCnt = 0 // don't allow infinite alias loops

	for len(c.text) > 0 {
		// find a \n or ; line break
		text := c.text
		quotes := 0
		i := 0
		for i = 0; i < len(text); i++ {
			if text[i] == '"' {
				quotes++
			}
			if quotes&1 == 0 && text[i] == ';' {
				break // don't break if inside a quoted string
			}
			if text[i] == '\n' {
				break
			}
		}

		// C copies into line[1024] (overflowing for longer lines; such lines
		// are then discarded by Cmd_MacroExpandString's length check).
		line := string(text[:i])

		// delete the text from the command buffer and move remaining
		// commands down
		if i == len(c.text) {
			c.text = c.text[:0]
		} else {
			i++
			n := copy(c.text, c.text[i:])
			c.text = c.text[:n]
		}

		// execute the command line
		c.ExecuteString(line)

		if c.wait {
			// skip out while text still remains in buffer, leaving it
			// for next frame
			c.wait = false
			break
		}
	}
}

func argvAt(argv []string, i int) string {
	if i < 0 || i >= len(argv) {
		return ""
	}
	return argv[i]
}

// Cbuf_AddEarlyCommands adds "+set a b" command line parameters. With clear
// the used argv entries are set to "" (COM_ClearArgv).
// C: qcommon/cmd.c:263 Cbuf_AddEarlyCommands
func (c *Cmd) Cbuf_AddEarlyCommands(argv []string, clear bool) {
	for i := 0; i < len(argv); i++ {
		if argv[i] != "+set" {
			continue
		}
		c.Cbuf_AddText(fmt.Sprintf("set %s %s\n", argvAt(argv, i+1), argvAt(argv, i+2)))
		if clear {
			for k := i; k <= i+2; k++ {
				if k < len(argv) {
					argv[k] = ""
				}
			}
		}
		i += 2
	}
}

// Cbuf_AddLateCommands adds "+cmd args" command line parameters (argv[0] is
// the program name). Commands end at the next '+' or '-'. Returns true if
// any command was added.
// C: qcommon/cmd.c:296 Cbuf_AddLateCommands
func (c *Cmd) Cbuf_AddLateCommands(argv []string) bool {
	// build the combined string to parse from
	s := 0
	for i := 1; i < len(argv); i++ {
		s += len(argv[i]) + 1
	}
	if s == 0 {
		return false
	}

	var tb strings.Builder
	for i := 1; i < len(argv); i++ {
		tb.WriteString(argv[i])
		if i != len(argv)-1 {
			tb.WriteString(" ")
		}
	}
	text := cstr(tb.String())
	at := func(k int) byte {
		if k < len(text) {
			return text[k]
		}
		return 0
	}

	// pull out the commands
	var build strings.Builder
	for i := 0; i < s-1; i++ {
		if at(i) == '+' {
			i++
			j := i
			for at(j) != '+' && at(j) != '-' && at(j) != 0 {
				j++
			}
			if i < j {
				build.WriteString(text[i:j])
			}
			build.WriteString("\n")
			i = j - 1
		}
	}

	ret := build.Len() != 0
	if ret {
		c.Cbuf_AddText(build.String())
	}
	return ret
}

/*
==============================================================================

						SCRIPT COMMANDS

==============================================================================
*/

// Exec_f executes a script file through LoadFile.
// C: qcommon/cmd.c:371 Cmd_Exec_f
func (c *Cmd) Exec_f() {
	if c.Argc() != 2 {
		c.printf("exec <filename> : execute a script file\n")
		return
	}

	var f []byte
	var err error
	if c.LoadFile != nil {
		f, err = c.LoadFile(c.Argv(1))
	}
	if c.LoadFile == nil || err != nil || f == nil {
		c.printf("couldn't exec %s\n", c.Argv(1))
		return
	}
	c.printf("execing %s\n", c.Argv(1))

	// the file doesn't have a trailing 0, so we need to copy it off
	c.Cbuf_InsertText(string(f))
}

// Echo_f prints the rest of the line to the console.
// C: qcommon/cmd.c:409 Cmd_Echo_f
func (c *Cmd) Echo_f() {
	for i := 1; i < c.Argc(); i++ {
		c.printf("%s ", c.Argv(i))
	}
	c.printf("\n")
}

// Alias_f creates a new command that executes a command string (possibly ;
// separated).
// C: qcommon/cmd.c:425 Cmd_Alias_f
func (c *Cmd) Alias_f() {
	if c.Argc() == 1 {
		c.printf("Current alias commands:\n")
		for a := c.aliases; a != nil; a = a.next {
			c.printf("%s : %s\n", a.name, a.value)
		}
		return
	}

	s := c.Argv(1)
	if len(s) >= maxAliasName {
		c.printf("Alias name is too long\n")
		return
	}

	// if the alias already exists, reuse it
	var a *cmdAlias
	for a = c.aliases; a != nil; a = a.next {
		if s == a.name {
			break
		}
	}

	if a == nil {
		a = &cmdAlias{next: c.aliases}
		c.aliases = a
	}
	a.name = s

	// copy the rest of the command line
	var b strings.Builder
	n := c.Argc()
	for i := 2; i < n; i++ {
		b.WriteString(c.Argv(i))
		if i != n-1 {
			b.WriteString(" ")
		}
	}
	b.WriteString("\n")
	a.value = b.String()
}

/*
=============================================================================

					COMMAND EXECUTION

=============================================================================
*/

// Argc returns the number of tokens of the last tokenized line.
// C: qcommon/cmd.c:507 Cmd_Argc
func (c *Cmd) Argc() int {
	return len(c.argv)
}

// Argv returns token arg, or "" when out of range.
// C: qcommon/cmd.c:517 Cmd_Argv
func (c *Cmd) Argv(arg int) string {
	if arg < 0 || arg >= len(c.argv) {
		return ""
	}
	return c.argv[arg]
}

// Args returns a single string containing argv(1) to argv(argc()-1).
// C: qcommon/cmd.c:531 Cmd_Args
func (c *Cmd) Args() string {
	return c.args
}

// mem is a view of C memory: reads past the end yield NUL.
type mem []byte

func (m mem) at(k int) byte {
	if k < 0 || k >= len(m) {
		return 0
	}
	return m[k]
}

// cstrAt returns the C string starting at k.
func (m mem) cstrAt(k int) string {
	e := k
	for m.at(e) != 0 {
		e++
	}
	if k >= e {
		return ""
	}
	return string(m[k:e])
}

// parse is shared.COM_Parse operating on a memory view with offsets, so the
// position after a token may lie past a NUL exactly like C's data pointer
// (e.g. after an unterminated quoted string). ok=false is *data_p = NULL.
// C: game/q_shared.c:1072 COM_Parse
func parse(m mem, pos int) (token string, next int, ok bool) {
	var tok [q2const.MAX_TOKEN_CHARS]byte
	length := 0
	i := pos
	sc := func(k int) int { return int(int8(m.at(k))) }

	var ch int
skipwhite:
	for {
		ch = sc(i)
		if ch > ' ' {
			break
		}
		if ch == 0 {
			return "", 0, false
		}
		i++
	}

	// skip // comments
	if ch == '/' && sc(i+1) == '/' {
		for sc(i) != 0 && sc(i) != '\n' {
			i++
		}
		goto skipwhite
	}

	// handle quoted strings specially
	if ch == '"' {
		i++
		for {
			ch = sc(i)
			i++
			if ch == '"' || ch == 0 {
				return string(tok[:length]), i, true
			}
			if length < q2const.MAX_TOKEN_CHARS {
				tok[length] = byte(ch)
				length++
			}
		}
	}

	// parse a regular word
	for {
		if length < q2const.MAX_TOKEN_CHARS {
			tok[length] = byte(ch)
			length++
		}
		i++
		ch = sc(i)
		if ch <= 32 {
			break
		}
	}
	if length == q2const.MAX_TOKEN_CHARS {
		length = 0
	}
	return string(tok[:length]), i, true
}

// macroExpand returns the memory holding the expanded line (either the
// input text or the persistent expanded buffer), or ok=false when C returns
// NULL.
//
// Quirk kept: len is increased by the value length but never decreased by
// the "$name" length, so the scan continues past the terminating NUL into the
// stale tail of the static expanded buffer. A stale '$' there makes the loop
// repeat until "Macro expansion loop" (or the length check) discards the line.
// C: qcommon/cmd.c:542 Cmd_MacroExpandString
func (c *Cmd) macroExpand(text string) (mem, bool) {
	inquote := false
	scan := mem(text)

	length := len(cstr(text))
	if length >= q2const.MAX_STRING_CHARS {
		c.printf("Line exceeded %d chars, discarded.\n", q2const.MAX_STRING_CHARS)
		return nil, false
	}

	count := 0
	for i := 0; i < length; i++ {
		if scan.at(i) == '"' {
			inquote = !inquote
		}
		if inquote {
			continue // don't expand inside quotes
		}
		if scan.at(i) != '$' {
			continue
		}
		// scan out the complete macro
		token, start, ok := parse(scan, i+1)
		if !ok {
			continue
		}

		if c.MacroAllow != nil && !c.MacroAllow(token) {
			token = ""
		} else {
			token = c.cvarString(token)
		}

		j := len(token)
		length += j
		if length >= q2const.MAX_STRING_CHARS {
			c.printf("Expanded line exceeded %d chars, discarded.\n", q2const.MAX_STRING_CHARS)
			return nil, false
		}

		// strncpy (temporary, scan, i): stops at NUL and zero pads to i
		temporary := make([]byte, i, i+j+len(scan))
		for k := 0; k < i; k++ {
			b := scan.at(k)
			if b == 0 {
				break
			}
			temporary[k] = b
		}
		temporary = append(temporary, token...)              // strcpy (temporary+i, token)
		temporary = append(temporary, scan.cstrAt(start)...) // strcpy (temporary+i+j, start)

		// strcpy (expanded, temporary): up to the first NUL
		n := len(temporary)
		for k, b := range temporary {
			if b == 0 {
				n = k
				break
			}
		}
		if n > len(c.expanded)-1 {
			n = len(c.expanded) - 1 // C would overflow; memory-safety clamp
		}
		copy(c.expanded[:], temporary[:n])
		c.expanded[n] = 0
		scan = mem(c.expanded[:])
		i--

		count++
		if count == 100 {
			c.printf("Macro expansion loop, discarded.\n")
			return nil, false
		}
	}

	if inquote {
		c.printf("Line has unmatched quote, discarded.\n")
		return nil, false
	}
	return scan, true
}

// MacroExpandString expands $cvar references outside quotes. ok=false where
// C returns NULL (the line is discarded).
// C: qcommon/cmd.c:542 Cmd_MacroExpandString
func (c *Cmd) MacroExpandString(text string) (string, bool) {
	m, ok := c.macroExpand(text)
	if !ok {
		return "", false
	}
	return m.cstrAt(0), true
}

// TokenizeString parses the given string into command line tokens. $Cvars
// will be expanded unless they are in a quoted token.
// C: qcommon/cmd.c:620 Cmd_TokenizeString
func (c *Cmd) TokenizeString(text string, macroExpand bool) {
	// clear the args from the last string
	c.argv = c.argv[:0]
	c.args = ""

	var m mem
	if macroExpand {
		var ok bool
		m, ok = c.macroExpand(text)
		if !ok {
			return
		}
	} else {
		m = mem(text)
	}

	p := 0
	for {
		// skip whitespace up to a /n (char is signed: bytes >= 0x80 count)
		for m.at(p) != 0 && int8(m.at(p)) <= ' ' && m.at(p) != '\n' {
			p++
		}

		if m.at(p) == '\n' { // a newline seperates commands in the buffer
			break
		}

		if m.at(p) == 0 {
			return
		}

		// set cmd_args to everything after the first arg
		if len(c.argv) == 1 {
			args := []byte(m.cstrAt(p))
			// strip off any trailing whitespace
			l := len(args) - 1
			for ; l >= 0; l-- {
				if int8(args[l]) <= ' ' {
					args = args[:l]
				} else {
					break
				}
			}
			c.args = string(args)
		}

		token, next, ok := parse(m, p)
		if !ok {
			return
		}
		p = next

		if len(c.argv) < q2const.MAX_STRING_TOKENS {
			c.argv = append(c.argv, token)
		}
	}
}

// AddCommand registers a command. It fails if a cvar or command with that
// name already exists.
// C: qcommon/cmd.c:691 Cmd_AddCommand
func (c *Cmd) AddCommand(name string, function XCommand) {
	// fail if the command is a variable name
	if c.cvarString(name) != "" {
		c.printf("Cmd_AddCommand: %s already defined as a var\n", name)
		return
	}

	// fail if the command already exists
	for cmd := c.functions; cmd != nil; cmd = cmd.next {
		if name == cmd.name {
			c.printf("Cmd_AddCommand: %s already defined\n", name)
			return
		}
	}

	c.functions = &cmdFunction{name: name, function: function, next: c.functions}
}

// RemoveCommand unregisters a command.
// C: qcommon/cmd.c:724 Cmd_RemoveCommand
func (c *Cmd) RemoveCommand(name string) {
	back := &c.functions
	for {
		cmd := *back
		if cmd == nil {
			c.printf("Cmd_RemoveCommand: %s not added\n", name)
			return
		}
		if name == cmd.name {
			*back = cmd.next
			return
		}
		back = &cmd.next
	}
}

// Exists reports whether a command is registered (case-sensitive).
// C: qcommon/cmd.c:752 Cmd_Exists
func (c *Cmd) Exists(name string) bool {
	for cmd := c.functions; cmd != nil; cmd = cmd.next {
		if name == cmd.name {
			return true
		}
	}
	return false
}

// CompleteCommand returns the first exact, then prefix, command or alias
// match ("" = NULL).
// C: qcommon/cmd.c:772 Cmd_CompleteCommand
func (c *Cmd) CompleteCommand(partial string) string {
	partial = cstr(partial)
	if len(partial) == 0 {
		return ""
	}
	// check for exact match
	for cmd := c.functions; cmd != nil; cmd = cmd.next {
		if partial == cmd.name {
			return cmd.name
		}
	}
	for a := c.aliases; a != nil; a = a.next {
		if partial == a.name {
			return a.name
		}
	}
	// check for partial match
	for cmd := c.functions; cmd != nil; cmd = cmd.next {
		if strings.HasPrefix(cmd.name, partial) {
			return cmd.name
		}
	}
	for a := c.aliases; a != nil; a = a.next {
		if strings.HasPrefix(a.name, partial) {
			return a.name
		}
	}
	return ""
}

// asciiEqualFold is Q_strcasecmp(a, b) == 0: it compares case-insensitively for ASCII letters only, like
// Q_strncasecmp (bytes >= 0x80 must match exactly).
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'a' && x <= 'z' {
			x -= 'a' - 'A'
		}
		if y >= 'a' && y <= 'z' {
			y -= 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// ExecuteString tokenizes and executes one command line. Lookup order:
// commands, aliases, cvars, then ForwardToServer.
// C: qcommon/cmd.c:811 Cmd_ExecuteString
func (c *Cmd) ExecuteString(text string) {
	c.TokenizeString(text, true)

	// execute the command line
	if c.Argc() == 0 {
		return // no tokens
	}

	// check functions
	for cmd := c.functions; cmd != nil; cmd = cmd.next {
		if asciiEqualFold(c.argv[0], cmd.name) {
			if cmd.function == nil { // forward to server command
				c.ExecuteString(fmt.Sprintf("cmd %s", text))
			} else {
				cmd.function()
			}
			return
		}
	}

	// check alias
	for a := c.aliases; a != nil; a = a.next {
		if asciiEqualFold(c.argv[0], a.name) {
			c.aliasCnt++
			if c.aliasCnt == aliasLoopCount {
				c.printf("ALIAS_LOOP_COUNT\n")
				return
			}
			c.Cbuf_InsertText(a.value)
			return
		}
	}

	// check cvars
	if c.Cvars != nil && c.Cvars.Command(c) {
		return
	}

	// send it as a server command if we are connected
	if c.ForwardToServer != nil {
		c.ForwardToServer()
	} else {
		// C: null/cl_null.c:31 Cmd_ForwardToServer
		c.printf("Unknown command \"%s\"\n", c.Argv(0))
	}
}

// List_f prints the command list.
// C: qcommon/cmd.c:865 Cmd_List_f
func (c *Cmd) List_f() {
	i := 0
	for cmd := c.functions; cmd != nil; cmd, i = cmd.next, i+1 {
		c.printf("%s\n", cmd.name)
	}
	c.printf("%d commands\n", i)
}

// Init registers cmdlist, exec, echo, alias and wait.
// C: qcommon/cmd.c:881 Cmd_Init
func (c *Cmd) Init() {
	c.AddCommand("cmdlist", c.List_f)
	c.AddCommand("exec", c.Exec_f)
	c.AddCommand("echo", c.Echo_f)
	c.AddCommand("alias", c.Alias_f)
	c.AddCommand("wait", c.Wait_f)
}
