package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/testutil"
)

func routesDir(t *testing.T) string {
	t.Helper()
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "fixtures", "agent", "routes")
}

func runQ2nav(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestPlanValidatesCheckedInTables(t *testing.T) {
	pak := testutil.DemoPak(t)
	code, out, errOut := runQ2nav("plan", "-pak", pak, "-routes", routesDir(t))
	if code != 0 {
		t.Fatalf("plan exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	for _, want := range []string{
		// every exit with its activator chains
		"exit #418 target_changelevel (t37) -> demo2$base1",
		"<- carried in by #582 func_door *31 (t4)",
		"<- #591 func_button *34 [touch",
		"exit #441 target_changelevel (t8) -> demo1$base2",
		"<- carried in by #502 func_door *46 (t7)",
		"<- #495 func_button *43 [touch",
		"exit #669 target_changelevel (t86) -> victory.pcx [pic]",
		"<- #592 trigger_multiple *34 [touch facing",
		// the tables with what each step sets off
		// the car's 1.7 s ride down follows the button's 0.3 s travel
		"relay #419 trigger_multiple *27 +2.0s; exit #418 target_changelevel (t37) +2.0s",
		"demo3: demo3 visit 0",
		"toggle #176 target_laser (t145)",
		"move #398 func_door *19 (t134) +6.4s",
		"move #878 func_door *42 (t138) (needs key_blue_key)",
		"all 4 visit tables valid; campaign ends at victory.pcx (pic)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
}

func TestPlanFailsOnBrokenTable(t *testing.T) {
	pak := testutil.DemoPak(t)
	src := routesDir(t)
	dir := t.TempDir()
	files, err := filepath.Glob(filepath.Join(src, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no route files in %s: %v", src, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(f) == "demo2b.json" {
			// the victory button replaced by the exit-less button *36
			b = bytes.Replace(b, []byte(`"entity": 668, "model": "*59"`), []byte(`"entity": 408, "model": "*36"`), 1)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, errOut := runQ2nav("plan", "-pak", pak, "-routes", dir)
	if code != 1 {
		t.Fatalf("plan exit %d, want 1\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"demo2b: step 6 (press): #408 func_button *36 does not cause exit of #669 target_changelevel (t86)",
		`demo2b: step 6 (press): the last step does not lead to the exit "victory.pcx"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "route table problem(s)") {
		t.Errorf("stderr %q", errOut)
	}
}

func TestInfo(t *testing.T) {
	pak := testutil.DemoPak(t)
	code, out, errOut := runQ2nav("info", "-pak", pak, "-map", "demo3", "-skill", "1")
	if code != 0 {
		t.Fatalf("info exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		`map demo3 "Comm Center"  checksum -1346016603  skill 1`,
		"movers 20  triggers 44  exits 1  lasers 2",
		"#731   *40", "func_plat",
		"directional",
		"#591  t35         demo2$base3b",
		"#176   t145",
		"base2a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("info output lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"info", "-pak", "x.pak"},     // no -map
		{"info", "-map", "demo1"},     // no -pak
		{"info", "-nosuchflag"},       // bad flag
		{"plan", "-routes", "/", "x"}, // stray argument
	} {
		if code, _, errOut := runQ2nav(args...); code != 2 || errOut == "" {
			t.Errorf("q2nav %q: exit %d, stderr %q; want 2 with usage", args, code, errOut)
		}
	}
	if code, _, _ := runQ2nav("info", "-pak", filepath.Join(t.TempDir(), "missing.pak"), "-map", "demo1"); code != 1 {
		t.Errorf("missing pak: exit %d, want 1", code)
	}
}
