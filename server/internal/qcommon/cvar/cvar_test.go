package cvar

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/q2const"
)

type argv []string

func (a argv) Argc() int { return len(a) }
func (a argv) Argv(i int) string {
	if i < 0 || i >= len(a) {
		return ""
	}
	return a[i]
}

func newLogged() (*Registry, *strings.Builder) {
	r := New()
	var log strings.Builder
	r.Printf = func(f string, a ...any) { fmt.Fprintf(&log, f, a...) }
	return r, &log
}

func TestGetCreatesOnceAndOrsFlags(t *testing.T) {
	r, _ := newLogged()
	v := r.Get("skill", "1", q2const.CVAR_LATCH)
	if v == nil || v.String != "1" || v.Value != 1 || !v.Modified || v.Flags != q2const.CVAR_LATCH {
		t.Fatalf("bad cvar %+v", v)
	}
	v.Modified = false
	v2 := r.Get("skill", "3", q2const.CVAR_ARCHIVE)
	if v2 != v || v.String != "1" || v.Flags != q2const.CVAR_LATCH|q2const.CVAR_ARCHIVE || v.Modified {
		t.Fatalf("existing cvar changed: %+v", v)
	}
	if r.GetNull("nope", 0) != nil {
		t.Fatal("GetNull created a cvar")
	}
	if r.GetNull("skill", q2const.CVAR_NOSET) != v || v.Flags&q2const.CVAR_NOSET == 0 {
		t.Fatal("GetNull did not or flags")
	}
}

func TestInfoValidation(t *testing.T) {
	r, log := newLogged()
	if r.Get("bad\\name", "x", q2const.CVAR_USERINFO) != nil {
		t.Fatal("accepted bad info name")
	}
	if r.Get("name", "a;b", q2const.CVAR_USERINFO) != nil {
		t.Fatal("accepted bad info value")
	}
	v := r.Get("name", "player", q2const.CVAR_USERINFO)
	r.Set("name", "evil\"quote")
	if v.String != "player" {
		t.Fatal("Set accepted invalid info value")
	}
	want := "invalid info cvar name\ninvalid info cvar value\ninvalid info cvar value\n"
	if log.String() != want {
		t.Fatalf("log %q", log.String())
	}
}

func TestSetNosetAndForce(t *testing.T) {
	r, log := newLogged()
	r.Get("basedir", ".", q2const.CVAR_NOSET)
	r.Set("basedir", "/tmp")
	if r.VariableString("basedir") != "." {
		t.Fatal("NOSET cvar changed")
	}
	if log.String() != "basedir is write protected.\n" {
		t.Fatalf("log %q", log.String())
	}
	r.ForceSet("basedir", "/tmp")
	if r.VariableString("basedir") != "/tmp" {
		t.Fatal("ForceSet failed")
	}
}

func TestLatch(t *testing.T) {
	r, log := newLogged()
	running := 0
	r.ServerState = func() int { return running }
	var gamedirs []string
	autoexec := 0
	r.SetGamedir = func(d string) { gamedirs = append(gamedirs, d) }
	r.ExecAutoexec = func() { autoexec++ }

	v := r.Get("maxclients", "1", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	v.Modified = false

	// no server running: applied immediately, Modified not set (C quirk)
	r.Set("maxclients", "4")
	if v.String != "4" || v.Value != 4 || v.LatchedString != nil || v.Modified {
		t.Fatalf("not applied directly: %+v", v)
	}

	running = 1
	r.Set("maxclients", "8")
	if v.String != "4" || v.LatchedString == nil || *v.LatchedString != "8" {
		t.Fatalf("not latched: %+v", v)
	}
	if !strings.Contains(log.String(), "maxclients will be changed for next game.\n") {
		t.Fatalf("log %q", log.String())
	}
	// same as latched: nothing
	log.Reset()
	r.Set("maxclients", "8")
	if log.Len() != 0 {
		t.Fatalf("unexpected log %q", log.String())
	}
	r.GetLatchedVars()
	if v.String != "8" || v.Value != 8 || v.LatchedString != nil {
		t.Fatalf("latched not applied: %+v", v)
	}

	// game cvar triggers FS hooks both directly and from latch
	g := r.Get("game", "", q2const.CVAR_LATCH|q2const.CVAR_SERVERINFO)
	running = 0
	r.Set("game", "ctf")
	running = 1
	r.Set("game", "rogue")
	r.GetLatchedVars()
	if g.String != "rogue" || len(gamedirs) != 2 || gamedirs[0] != "ctf" || gamedirs[1] != "rogue" || autoexec != 2 {
		t.Fatalf("game hooks: %v %d %q", gamedirs, autoexec, g.String)
	}

	// ForceSet drops a pending latch
	r.Set("maxclients", "16")
	r.ForceSet("maxclients", "2")
	if v.LatchedString != nil || v.String != "2" {
		t.Fatalf("ForceSet: %+v", v)
	}
}

func TestSetModifiedAndUserinfo(t *testing.T) {
	r := New()
	v := r.Get("rate", "25000", q2const.CVAR_USERINFO|q2const.CVAR_ARCHIVE)
	v.Modified = false
	r.Set("rate", "25000")
	if v.Modified || r.UserinfoModified {
		t.Fatal("unchanged set marked modified")
	}
	r.Set("rate", "8000")
	if !v.Modified || !r.UserinfoModified || v.Value != 8000 {
		t.Fatal("set did not mark modified")
	}
}

func TestSetValueFormat(t *testing.T) {
	cases := []struct {
		v    float32
		want string
	}{
		{0, "0"}, {1, "1"}, {-3, "-3"}, {0.5, "0.500000"}, {0.1, "0.100000"},
		{1e-7, "0.000000"}, {-2.75, "-2.750000"}, {16777216, "16777216"},
		{3e9, "3000000000.000000"}, // (int) overflows to INT_MIN -> %f
		{float32(math.Inf(1)), "inf"}, {float32(math.NaN()), "nan"},
		{1e30, "1000000015047466219876688855040"[:31]},
	}
	for _, c := range cases {
		if got := FormatValue(c.v); got != c.want {
			t.Errorf("FormatValue(%g) = %q, want %q", c.v, got, c.want)
		}
	}
	r := New()
	r.SetValue("x", 2)
	if r.VariableString("x") != "2" || r.VariableValue("x") != 2 {
		t.Fatal("SetValue")
	}
}

func TestInfoStringsAndOrder(t *testing.T) {
	r := New()
	r.Get("name", "unnamed", q2const.CVAR_USERINFO|q2const.CVAR_ARCHIVE)
	r.Get("skin", "male/grunt", q2const.CVAR_USERINFO|q2const.CVAR_ARCHIVE)
	r.Get("hostname", "noname", q2const.CVAR_SERVERINFO)
	r.Get("empty", "", q2const.CVAR_USERINFO)
	// newest first
	if got := r.Userinfo(); got != "\\skin\\male/grunt\\name\\unnamed" {
		t.Fatalf("userinfo %q", got)
	}
	if got := r.Serverinfo(); got != "\\hostname\\noname" {
		t.Fatalf("serverinfo %q", got)
	}
	if got := r.WriteVariables(); got != "set skin \"male/grunt\"\nset name \"unnamed\"\n" {
		t.Fatalf("write %q", got)
	}
	list := r.List_f()
	if !strings.HasPrefix(list, " U   empty \"\"\n  S  hostname \"noname\"\n*U   skin") || !strings.HasSuffix(list, "4 cvars\n") {
		t.Fatalf("list %q", list)
	}
}

func TestCommandAndSetF(t *testing.T) {
	r, log := newLogged()
	if r.Command(argv{"nope"}) {
		t.Fatal("unknown cvar handled")
	}
	r.Get("fov", "90", 0)
	if !r.Command(argv{"fov"}) || log.String() != "\"fov\" is \"90\"\n" {
		t.Fatalf("print: %q", log.String())
	}
	r.Command(argv{"fov", "110", "ignored"})
	if r.VariableValue("fov") != 110 {
		t.Fatal("Command set")
	}
	r.Set_f(argv{"set", "cl_x", "5", "u"})
	if v := r.FindVar("cl_x"); v == nil || v.Flags != q2const.CVAR_USERINFO || v.String != "5" {
		t.Fatalf("set u: %+v", v)
	}
	r.Set_f(argv{"set", "cl_x", "6", "s"}) // FullSet replaces flags
	if v := r.FindVar("cl_x"); v.Flags != q2const.CVAR_SERVERINFO || v.String != "6" {
		t.Fatalf("set s: %+v", v)
	}
	log.Reset()
	r.Set_f(argv{"set", "a", "b", "z"})
	r.Set_f(argv{"set", "a"})
	if log.String() != "flags can only be 'u' or 's'\nusage: set <variable> <value> [u / s]\n" {
		t.Fatalf("log %q", log.String())
	}
}

func TestCompleteVariable(t *testing.T) {
	r := New()
	r.Get("sv_gravity", "800", 0)
	r.Get("sv", "1", 0)
	r.Get("sv_maxvelocity", "2000", 0)
	if got := r.CompleteVariable("sv"); got != "sv" {
		t.Fatalf("exact %q", got)
	}
	if got := r.CompleteVariable("sv_"); got != "sv_maxvelocity" { // newest first
		t.Fatalf("partial %q", got)
	}
	if got := r.CompleteVariable(""); got != "" {
		t.Fatalf("empty %q", got)
	}
}
