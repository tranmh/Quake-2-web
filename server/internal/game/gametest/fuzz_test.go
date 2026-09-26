package gametest

// Native Go fuzz targets (go test -fuzz=FuzzX ./internal/game/gametest/).
// Without -fuzz they run their seed corpus as regular tests.

import (
	"math/rand"
	"strings"
	"testing"
)

// fuzzFail reports msg unless it is acceptable (see classify).
func fuzzFail(t *testing.T, what, msg string) {
	t.Helper()
	if site := classify(msg); site != "" {
		t.Fatalf("%s: %s: %s", what, site, msg)
	}
}

// FuzzClientCommand sends one fuzzed command line (optionally several,
// separated by newlines) from client 0, then two frames, in sp or ctf.
func FuzzClientCommand(f *testing.F) {
	needDemoPak(f)
	for _, c := range robustCommands {
		f.Add(false, c)
		f.Add(true, c)
		f.Add(false, c+" 1")
	}
	for _, s := range []string{"give all\ninvnextp\ninvprevp", "give health 2147483647", "wave -2147483648",
		"use \"Quad Damage\"", "drop Blaster", "say_team %l %a %h %t %w %n %", "team blue\nteam red\nobserver",
		"admin pw\nwarp demo2", "give all\ndrop tech\ndrop tech", "invnextw\ninvprevw\ninvuse\ninvdrop",
		"kill\nkill\nputaway\nhelp\nhelp\nscore", "ghost 12345", "boot 99999999999"} {
		f.Add(false, s)
		f.Add(true, s)
	}
	f.Fuzz(func(t *testing.T, ctf bool, cmds string) {
		mode := "sp"
		if ctf {
			mode = "ctf"
		}
		s := newRobustServer(t, robustModes[mode], "")
		for _, c := range strings.Split(cmds, "\n") {
			c := c
			fuzzFail(t, c, catchPanic(func() {
				s.inputCommand(0, c)
				s.step(nil)
			}))
		}
		fuzzFail(t, "frame", catchPanic(func() { s.step(nil) }))
	})
}

// FuzzUserinfo applies a fuzzed userinfo string to a connected client (both
// as the connect userinfo and as a later change) and plays a few frames.
func FuzzUserinfo(f *testing.F) {
	needDemoPak(f)
	f.Add(0, `\name\x\skin\male/grunt\hand\2\fov\999`)
	f.Add(1, `\name\x\spectator\1\skin\/`)
	f.Add(2, `\name\\\skin\`)
	f.Add(3, `\\\\`)
	f.Fuzz(func(t *testing.T, m int, ui string) {
		modes := []string{"sp", "coop", "dm", "ctf"}
		mode := modes[(m%4+4)%4]
		s := newRobustServer(t, robustModes[mode], "")
		fuzzFail(t, "reconnect", catchPanic(func() { s.reconnect(0, ui) }))
		fuzzFail(t, "change", catchPanic(func() {
			s.userinfoChanged(0, ui)
			for i := 0; i < 3; i++ {
				s.step(nil)
			}
		}))
	})
}

// FuzzEntities spawns demo1 (stripped) plus a fuzzed entity string and
// plays a few frames with a random walk.
func FuzzEntities(f *testing.F) {
	base, start := demoEntities(f)
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 20; i++ {
		f.Add(randomEntity(r, start) + randomEntity(r, start))
	}
	f.Add(`{ "classname" "target_string" "team" "a" "targetname" "t" "message" "1" } { "classname" "target_character" "team" "a" "count" "-3" "model" "*1" } { "classname" "trigger_always" "target" "t" }`)
	f.Fuzz(func(t *testing.T, extra string) {
		var s *Server
		msg := catchPanic(func() { s = newRobustServer(t, robustModes["sp"], base+extra) })
		if msg == "" {
			rr := rand.New(rand.NewSource(int64(len(extra))))
			msg = monkey(s, rr, 20, nil)
		}
		fuzzFail(t, "entities", msg)
	})
}

// FuzzReadSave loads fuzzed game/level blobs (raw bytes, so the zstd and
// JSON layers are fuzzed too; valid saves are in the seed corpus).
func FuzzReadSave(f *testing.F) {
	needDemoPak(f)
	a, gd, ld := saveFixture(f, "sp", 10, rand.New(rand.NewSource(1)))
	f.Add(gd, ld)
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 10; i++ {
		f.Add(mutateSave(f, r, gd, 2), mutateSave(f, r, ld, 2))
	}
	f.Add([]byte{}, []byte{0x28, 0xb5, 0x2f, 0xfd})
	f.Fuzz(func(t *testing.T, g, l []byte) {
		var b *Server
		var lerr error
		msg := catchPanic(func() { b, lerr = loadSave(t, a, g, l) })
		if msg == "" && lerr != nil && strings.Contains(lerr.Error(), "internal error") {
			msg = "comerror: Game Error: " + lerr.Error()
		}
		if msg == "" && lerr == nil {
			msg = monkey(b, rand.New(rand.NewSource(3)), 10, nil)
		}
		fuzzFail(t, "load", msg)
	})
}
