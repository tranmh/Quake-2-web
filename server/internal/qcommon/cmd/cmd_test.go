package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

func newCmd() (*Cmd, *strings.Builder) {
	r := cvar.New()
	c := New(r)
	var log strings.Builder
	c.Printf = func(f string, a ...any) { fmt.Fprintf(&log, f, a...) }
	r.Printf = c.Printf
	c.Init()
	r.Init(c)
	return c, &log
}

func (c *Cmd) argvSlice() []string { return append([]string{}, c.argv...) }

func TestParseMatchesCOMParse(t *testing.T) {
	inputs := []string{
		"", "   ", "abc def", "  \"quoted string\" tail", "// comment\nnext", "a//b c",
		"\"unterminated", "x\xffy z", "\"hi\xff\" q", "word\n", strings.Repeat("w", 127) + " x",
		strings.Repeat("w", 128) + " x", strings.Repeat("w", 200), "/ / x", "\t\r\nfoo",
	}
	for _, in := range inputs {
		data := in
		pos := 0
		for step := 0; step < 10; step++ {
			wantTok, rest, more := shared.COM_Parse(data)
			tok, next, ok := parse(mem(in), pos)
			if ok != more || tok != wantTok {
				t.Fatalf("%q step %d: parse=(%q,%v) COM_Parse=(%q,%v)", in, step, tok, ok, wantTok, more)
			}
			if !ok {
				break
			}
			if next > len(in) {
				next = len(in)
			}
			if in[next:] != rest {
				t.Fatalf("%q step %d: rest %q vs %q", in, step, in[next:], rest)
			}
			data, pos = rest, next
		}
	}
}

func TestTokenize(t *testing.T) {
	c, _ := newCmd()
	c.Cvars.Get("name", "bob", 0)
	cases := []struct {
		in     string
		expand bool
		argv   []string
		args   string
	}{
		{"", true, nil, ""},
		{"map base1", true, []string{"map", "base1"}, "base1"},
		{"  say \"hello world\"  x  ", true, []string{"say", "hello world", "x"}, "\"hello world\"  x"},
		{"echo a // comment", true, []string{"echo", "a"}, "a // comment"},
		{"echo // c\nb", false, []string{"echo", "b"}, "// c\nb"}, // COM_Parse skips the \n after a comment
		{"first\nsecond", true, []string{"first"}, ""},
		{"say $name", true, []string{"say", "bob"}, "bob"},
		{"say \"$name\"", true, []string{"say", "$name"}, "\"$name\""},
		{"say $name", false, []string{"say", "$name"}, "$name"},
		{"a\xffb", true, []string{"a", "b"}, "b"}, // high bytes break words and are whitespace
		{"echo \"odd", true, nil, ""},             // unmatched quote discarded
	}
	for _, tc := range cases {
		c.TokenizeString(tc.in, tc.expand)
		got := c.argvSlice()
		if len(got) == 0 {
			got = nil
		}
		if !reflect.DeepEqual(got, tc.argv) || c.Args() != tc.args {
			t.Errorf("Tokenize(%q): argv=%q args=%q, want %q %q", tc.in, got, c.Args(), tc.argv, tc.args)
		}
	}
	// token limit
	c.TokenizeString(strings.Repeat("t ", 100), false)
	if c.Argc() != q2const.MAX_STRING_TOKENS {
		t.Fatalf("argc %d", c.Argc())
	}
	if c.Argv(-1) != "" || c.Argv(1000) != "" {
		t.Fatal("Argv out of range")
	}
}

func TestMacroExpandMatchesC(t *testing.T) {
	// testdata/macro_c.txt was produced by testdata/macro_c.c.txt: the real
	// COM_Parse and Cmd_MacroExpandString with stub cvars, fed a deterministic
	// sequence of inputs through one static expanded[] buffer. It pins the
	// stale-tail quirk (len overestimation reads past the NUL).
	f, err := os.Open("testdata/macro_c.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := cvar.New()
	for k, v := range map[string]string{"a": "x", "b": "$a", "loop": "$loop", "q": "\"hi there\"",
		"l": "a b", "z": "$", "long": "0123456789012345678901234567890123456789"} {
		r.Get(k, v, 0)
	}
	c := New(r)
	lcg := uint32(12345)
	nxt := func() uint32 { lcg = lcg*1103515245 + 12345; return (lcg >> 16) & 0x7fff }
	const alpha = "$$$ab\"x  zq/l\t"
	sc := bufio.NewScanner(f)
	for n := 0; n < 3000; n++ {
		l := int(nxt() % 40)
		b := make([]byte, l)
		for i := range b {
			b[i] = alpha[nxt()%14]
		}
		if !sc.Scan() {
			t.Fatal("short testdata")
		}
		want := sc.Text()
		got, ok := c.MacroExpandString(string(b))
		if !ok {
			got = "<NULL>"
		}
		if got != want {
			t.Fatalf("input %d %q: got %q want %q", n, b, got, want)
		}
	}
}

func TestMacroExpandMessages(t *testing.T) {
	c, log := newCmd()
	c.Cvars.Get("loop", "$loop", 0)
	c.Cvars.Get("big", strings.Repeat("y", 1000), 0)
	if _, ok := c.MacroExpandString("echo $loop"); ok || log.String() != "Macro expansion loop, discarded.\n" {
		t.Fatalf("loop: %q", log.String())
	}
	log.Reset()
	if _, ok := c.MacroExpandString("echo $big $big"); ok || log.String() != "Expanded line exceeded 1024 chars, discarded.\n" {
		t.Fatalf("big: %q", log.String())
	}
	log.Reset()
	if _, ok := c.MacroExpandString(strings.Repeat("z", 1024)); ok || log.String() != "Line exceeded 1024 chars, discarded.\n" {
		t.Fatalf("line: %q", log.String())
	}
}

func TestCbufExecuteSplitting(t *testing.T) {
	c, _ := newCmd()
	var got [][]string
	c.AddCommand("t", func() { got = append(got, c.argvSlice()) })
	c.Cbuf_AddText("t 1;t 2\nt \"3;4\" 5;t \"6\n t 7\n")
	c.Cbuf_Execute()
	// `t "6` is split at the newline and discarded (unmatched quote)
	want := [][]string{{"t", "1"}, {"t", "2"}, {"t", "3;4", "5"}, {"t", "7"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestWait(t *testing.T) {
	c, _ := newCmd()
	var got []string
	c.AddCommand("t", func() { got = append(got, c.Argv(1)) })
	c.Cbuf_AddText("t 1;wait;t 2;wait;wait;t 3\n")
	c.Cbuf_Execute()
	if !reflect.DeepEqual(got, []string{"1"}) {
		t.Fatalf("frame1 %q", got)
	}
	c.Cbuf_Execute()
	if !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Fatalf("frame2 %q", got)
	}
	c.Cbuf_Execute() // second wait
	c.Cbuf_Execute()
	if !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Fatalf("frame4 %q", got)
	}
}

func TestAliasAndLoop(t *testing.T) {
	c, log := newCmd()
	var got []string
	c.AddCommand("t", func() { got = append(got, c.Args()) })
	c.Cbuf_AddText("alias go \"t a;t b\"\nGO\nt c\n")
	c.Cbuf_Execute()
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("alias %q", got)
	}
	c.Cbuf_AddText("alias self self\nself\n")
	c.Cbuf_Execute()
	if !strings.Contains(log.String(), "ALIAS_LOOP_COUNT\n") {
		t.Fatalf("log %q", log.String())
	}
	log.Reset()
	c.ExecuteString("alias " + strings.Repeat("n", 32) + " x")
	if log.String() != "Alias name is too long\n" {
		t.Fatalf("log %q", log.String())
	}
	if c.CompleteCommand("se") != "set" || c.CompleteCommand("sel") != "self" || c.CompleteCommand("") != "" {
		t.Fatal("complete")
	}
}

func TestExec(t *testing.T) {
	c, log := newCmd()
	files := map[string]string{"autoexec.cfg": "set a 1\nset b \"2 3\"\nfoo\n\x00ignored"}
	c.LoadFile = func(n string) ([]byte, error) {
		if s, ok := files[n]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("not found")
	}
	var foo int
	c.AddCommand("foo", func() { foo++ })
	c.Cbuf_AddText("exec autoexec.cfg;set c 4\nexec missing.cfg\n")
	c.Cbuf_Execute()
	if c.Cvars.VariableString("a") != "1" || c.Cvars.VariableString("b") != "2 3" || c.Cvars.VariableString("c") != "4" || foo != 1 {
		t.Fatalf("exec failed: %q", log.String())
	}
	if !strings.Contains(log.String(), "execing autoexec.cfg\n") || !strings.Contains(log.String(), "couldn't exec missing.cfg\n") {
		t.Fatalf("log %q", log.String())
	}
}

func TestExecuteStringOrder(t *testing.T) {
	c, log := newCmd()
	var forwarded []string
	c.ForwardToServer = func() { forwarded = append(forwarded, c.Argv(0)) }
	var fwdCmd []string
	c.AddCommand("cmd", func() { fwdCmd = append(fwdCmd, c.Args()) })
	c.AddCommand("god", nil) // nil function: forwarded as "cmd god"
	c.Cvars.Get("fov", "90", 0)
	c.AddCommand("fov", func() {}) // rejected: already a var
	if log.String() != "Cmd_AddCommand: fov already defined as a var\n" {
		t.Fatalf("log %q", log.String())
	}
	c.ExecuteString("GOD 1")
	c.ExecuteString("fov 100")
	c.ExecuteString("kill")
	if !reflect.DeepEqual(fwdCmd, []string{"GOD 1"}) || c.Cvars.VariableString("fov") != "100" || !reflect.DeepEqual(forwarded, []string{"kill"}) {
		t.Fatalf("fwdCmd=%q fov=%q forwarded=%q", fwdCmd, c.Cvars.VariableString("fov"), forwarded)
	}
	c.ForwardToServer = nil
	log.Reset()
	c.ExecuteString("bogus")
	if log.String() != "Unknown command \"bogus\"\n" {
		t.Fatalf("log %q", log.String())
	}
	c.RemoveCommand("god")
	if c.Exists("god") {
		t.Fatal("RemoveCommand")
	}
}

func TestCbufBufferOps(t *testing.T) {
	c, log := newCmd()
	c.Cbuf_AddText(strings.Repeat("x", 8191))
	c.Cbuf_AddText("y")
	if log.String() != "Cbuf_AddText: overflow\n" || len(c.text) != 8191 {
		t.Fatalf("overflow check: %q %d", log.String(), len(c.text))
	}
	c.Cbuf_Init()
	c.Cbuf_AddText("b\n")
	c.Cbuf_InsertText("a\n")
	if string(c.text) != "a\nb\n" {
		t.Fatalf("insert %q", c.text)
	}
	c.Cbuf_CopyToDefer()
	if len(c.text) != 0 {
		t.Fatal("defer did not clear")
	}
	c.Cbuf_AddText("c\n")
	c.Cbuf_InsertFromDefer()
	if string(c.text) != "a\nb\nc\n" || len(c.deferText) != 0 {
		t.Fatalf("from defer %q", c.text)
	}
}

func TestCommandLine(t *testing.T) {
	c, _ := newCmd()
	argv := []string{"q2", "+set", "dedicated", "1", "+map", "base1", "+set", "x", "-1"}
	c.Cbuf_AddEarlyCommands(argv, true)
	if string(c.text) != "set dedicated 1\nset x -1\n" {
		t.Fatalf("early %q", c.text)
	}
	if argv[1] != "" || argv[3] != "" || argv[4] != "+map" {
		t.Fatalf("clear %q", argv)
	}
	c.Cbuf_Init()
	if !c.Cbuf_AddLateCommands([]string{"q2", "+map", "base1", "+set", "x", "-1", "+echo", "hi"}) {
		t.Fatal("late returned false")
	}
	// '-' ends a command (quirk): "+set x -1" becomes "set x "
	if string(c.text) != "map base1 \nset x \necho hi\n" {
		t.Fatalf("late %q", c.text)
	}
	c.Cbuf_Init()
	if c.Cbuf_AddLateCommands([]string{"q2"}) || c.Cbuf_AddLateCommands([]string{"q2", "nothing"}) {
		t.Fatal("late without +")
	}
}

func TestCvarInitCommands(t *testing.T) {
	c, log := newCmd()
	c.ExecuteString("set sv_x 5 s")
	if v := c.Cvars.FindVar("sv_x"); v == nil || v.Flags != q2const.CVAR_SERVERINFO {
		t.Fatal("set command")
	}
	log.Reset()
	c.ExecuteString("cvarlist")
	if !strings.HasSuffix(log.String(), "1 cvars\n") {
		t.Fatalf("cvarlist %q", log.String())
	}
	log.Reset()
	c.ExecuteString("echo a  b")
	if log.String() != "a b \n" {
		t.Fatalf("echo %q", log.String())
	}
}

// Cbuf_InsertText does not add a newline (despite its comment), so a script
// without a trailing newline runs into the following buffered command.
func TestExecNoTrailingNewlineMerges(t *testing.T) {
	c, log := newCmd()
	c.LoadFile = func(string) ([]byte, error) { return []byte("echo a"), nil }
	c.Cbuf_AddText("exec x.cfg\necho b\n")
	c.Cbuf_Execute()
	if log.String() != "execing x.cfg\naecho b \n" {
		t.Fatalf("log %q", log.String())
	}
}
