package perception

import (
	"fmt"
	"testing"
)

// helpLayout is HelpComputer's layout string (C %i is Go %d).
// C: game/p_hud.c:301 HelpComputer
func helpLayout(skill, level, h1, h2 string, k, km, g, gm, s, sm int) string {
	return fmt.Sprintf("xv 32 yv 8 picn help "+
		"xv 202 yv 12 string2 \"%s\" "+
		"xv 0 yv 24 cstring2 \"%s\" "+
		"xv 0 yv 54 cstring2 \"%s\" "+
		"xv 0 yv 110 cstring2 \"%s\" "+
		"xv 50 yv 164 string2 \" kills     goals    secrets\" "+
		"xv 50 yv 172 string2 \"%3d/%3d     %d/%d       %d/%d\" ",
		skill, level, h1, h2, k, km, g, gm, s, sm)
}

func TestParseHelp(t *testing.T) {
	l := helpLayout("medium", "Outer Base", "Find the elevator to the\nnext level.", "", 3, 12, 0, 1, 1, 2)
	h, ok := ParseHelp(l)
	want := Help{Skill: "medium", LevelName: "Outer Base", Help1: "Find the elevator to the\nnext level.",
		Kills: 3, KillsMax: 12, Goals: 0, GoalsMax: 1, Secrets: 1, SecretsMax: 2}
	if !ok || h != want {
		t.Fatalf("got %+v %v\nwant %+v", h, ok, want)
	}
	h, ok = ParseHelp(helpLayout("hard+", "Comm Center", "a", "b", 123, 456, 7, 8, 9, 10))
	if !ok || h.Kills != 123 || h.KillsMax != 456 || h.SecretsMax != 10 || h.Help2 != "b" {
		t.Fatalf("three digit counts: %+v %v", h, ok)
	}
	for _, bad := range []string{
		"",
		"xv 0 yv 0 string2 \"hello\"",       // not the help computer
		"xv 32 yv 8 picn help xv 202 yv 12", // cut off
		helpLayout("easy", "x", "", "", 1, 2, 3, 4, 5, 6)[:120],
		"picn help string2 a string2 b string2 c string2 d string2 kills string2 \"1/2 x/4 5/6\"",
		"client 0 0 1 2 3 4 picn",
	} {
		if h, ok := ParseHelp(bad); ok {
			t.Errorf("%q parsed as help: %+v", bad, h)
		}
	}
}

func FuzzParseHelp(f *testing.F) {
	f.Add(helpLayout("medium", "Outer Base", "x", "y", 1, 2, 3, 4, 5, 6))
	f.Add("if 1 picn help endif")
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = ParseHelp(s) // must not panic
	})
}
