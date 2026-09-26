package game

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"quake2web/server/internal/testutil"
)

// cSpawnNames returns the classnames of the spawns[] table of a C
// g_spawn.c, with "#if 0" blocks removed.
func cSpawnNames(t *testing.T, rel string) []string {
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Skip(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "Quake-2", rel))
	if err != nil {
		t.Skip(err)
	}
	src := strings.ReplaceAll(string(b), "\r", "")
	src = regexp.MustCompile(`(?s)\n#if 0.*?\n#endif`).ReplaceAllString(src, "")
	start := strings.Index(src, "spawn_t\tspawns[] = {")
	if start < 0 {
		t.Fatalf("spawns[] not found in %s", rel)
	}
	src = src[start:]
	src = src[:strings.Index(src, "{NULL, NULL}")]
	var names []string
	for _, m := range regexp.MustCompile(`\{"([a-z0-9_]+)",\s*SP_[A-Za-z0-9_]+\}`).FindAllStringSubmatch(src, -1) {
		names = append(names, m[1])
	}
	sort.Strings(names)
	return names
}

// TestCTFSpawnTableMatchesC: ctfSpawns has exactly the classnames of the
// spawns[] of ctf/g_spawn.c (CTF spawns added, monster code removed).
func TestCTFSpawnTableMatchesC(t *testing.T) {
	want := cSpawnNames(t, filepath.Join("ctf", "g_spawn.c"))
	var got []string
	for _, s := range ctfSpawns {
		got = append(got, s.name)
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ctfSpawns differ from ctf/g_spawn.c:\n go: %v\n  C: %v", got, want)
	}
}

// TestModuleSelection: the item table follows the module.
func TestModuleSelection(t *testing.T) {
	g := NewModule(nil, nil, "ctf")
	if !g.IsCTF() || len(g.itemlist) != len(ctfItemlist) {
		t.Fatalf("ctf module: ctfmod=%v items=%d", g.ctfmod, len(g.itemlist))
	}
	g = NewModule(nil, nil, "baseq2")
	if g.IsCTF() || len(g.itemlist) != len(itemlist) {
		t.Fatalf("baseq2 module: ctfmod=%v items=%d", g.ctfmod, len(g.itemlist))
	}
	g.game.NumItems = int32(len(g.itemlist) - 1)
	if g.FindItem("Grapple") != nil || g.CTFMatchSetup() || g.CTFInMatch() || g.CTFMatchOn() {
		t.Fatal("baseq2 module sees ctf state")
	}
	g = NewModule(nil, nil, "ctf")
	g.game.NumItems = int32(len(g.itemlist) - 1)
	gr := g.FindItem("Grapple")
	if gr == nil || ITEM_INDEX(gr) != ITEM_INDEX(g.FindItem("Blaster"))-1 {
		t.Fatal("ctf module: grapple must precede the blaster")
	}
}

// TestPMenuDoUpdateLayout checks the layout string of PMenu_Do_Update
// (alignment arithmetic, alt '*' entries, cursor marker).
func TestPMenuDoUpdateLayout(t *testing.T) {
	li := &layoutImport{}
	g := NewModule(li, nil, "ctf")
	g.edicts = make([]Edict, 2)
	g.game.Clients = make([]GClient, 1)
	ent := &g.edicts[1]
	ent.Client = &g.game.Clients[0]
	menu := []PMenu{
		{"*Title", PMENU_ALIGN_CENTER, nil},
		{"", PMENU_ALIGN_LEFT, nil},
		{"Pick", PMENU_ALIGN_LEFT, (*Game).CTFAdmin_Cancel},
		{"v1", PMENU_ALIGN_RIGHT, nil},
	}
	g.PMenu_Open(ent, menu, -1, int32(len(menu)), nil)
	want := "xv 32 yv 8 picn inventory yv 32 xv 142 string2 \"Title\" yv 48 xv 56 string2 \"\x0dPick\" yv 56 xv 244 string \"v1\" "
	if li.str != want {
		t.Errorf("layout\n got %q\nwant %q", li.str, want)
	}
	if !ent.Client.Showscores || ent.Client.Menu == nil || ent.Client.Menu.Cur != 2 {
		t.Errorf("menu state: showscores=%v cur=%v", ent.Client.Showscores, ent.Client.Menu)
	}
}

type layoutImport struct {
	Import
	str string
}

func (l *layoutImport) WriteByteC(int)              {}
func (l *layoutImport) WriteString(s string)        { l.str = s }
func (l *layoutImport) Unicast(*Edict, bool)        {}
func (l *layoutImport) Dprintf(string)              {}
func (l *layoutImport) Cprintf(*Edict, int, string) {}
