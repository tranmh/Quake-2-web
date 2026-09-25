package game

import (
	"strings"
	"testing"
)

type printImport struct {
	Import
	out []string
}

func (p *printImport) Dprintf(text string) { p.out = append(p.out, text) }

// TestEDParseEdict checks ED_ParseEdict / ED_ParseField semantics of the
// fields[] table: spawntemp fields, FFL_NOSPAWN, F_ANGLEHACK, F_IGNORE,
// underscore keys, ED_NewString escapes and case-insensitive keys.
func TestEDParseEdict(t *testing.T) {
	pi := &printImport{}
	g := New(pi, nil)
	var ent Edict
	src := `{
"classname" "func_door"
"ORIGIN" "1.5 -2 3e1"
"angle" "90"
"spawnflags" "0x10"
"message" "line1\nline2\\x"
"_comment" "ignored"
"light" "300"
"item" "weapon_shotgun"
"sky" "unit2_"
"skyaxis" "1 2"
"lip" "8"
"think" "door_go_up"
"bogus" "1"
"wait" "-1"
}
{ "classname" "next" }`
	rest, more := g.ED_ParseEdict(strings.TrimPrefix(src, "{"), &ent)
	if !more || !strings.Contains(rest, "next") {
		t.Fatalf("rest=%q more=%v", rest, more)
	}
	if ent.Classname != "func_door" {
		t.Errorf("classname %q", ent.Classname)
	}
	if ent.S.Origin != (Vec3{1.5, -2, 30}) {
		t.Errorf("origin %v", ent.S.Origin)
	}
	if ent.S.Angles != (Vec3{0, 90, 0}) {
		t.Errorf("angle hack %v", ent.S.Angles)
	}
	if ent.Spawnflags != 0 { // atoi("0x10") == 0
		t.Errorf("spawnflags %d", ent.Spawnflags)
	}
	if ent.Message != "line1\nline2\\x" {
		t.Errorf("message %q", ent.Message)
	}
	if ent.Item != nil || g.st.Item != "weapon_shotgun" {
		t.Errorf("item: ent %v st %q", ent.Item, g.st.Item)
	}
	if g.st.Sky != "unit2_" || g.st.Skyaxis != (Vec3{1, 2, 0}) || g.st.Lip != 8 {
		t.Errorf("spawntemp %+v", g.st)
	}
	if ent.Think != nil {
		t.Errorf("FFL_NOSPAWN field was set")
	}
	if ent.Wait != -1 {
		t.Errorf("wait %v", ent.Wait)
	}
	want := []string{"think is not a field\n", "bogus is not a field\n"}
	if strings.Join(pi.out, "|") != strings.Join(want, "|") {
		t.Errorf("dprintf %q, want %q", pi.out, want)
	}
}

func TestSpawnSscanfVec(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Vec3
	}{
		{"1 2 3", Vec3{1, 2, 3}},
		{"  -0.1 16777217 1e-50", Vec3{-0.1, 16777216, 0}},
		{"5 x 7", Vec3{5, 0, 0}},
		{"0.30000001192092896", Vec3{0.3, 0, 0}},
	} {
		var v Vec3
		spawnSscanfVec(c.in, &v)
		if v != c.want {
			t.Errorf("sscanf(%q) = %v, want %v", c.in, v, c.want)
		}
	}
}
