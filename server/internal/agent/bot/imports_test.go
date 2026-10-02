package bot

import (
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestImports keeps the bot loop fair: its non-test files import only
// the standard library and this allowlist (its own client, the fair
// perception and belief, navigation, control, decisions and static map
// knowledge), and nothing it links is server, game or session code (its
// Cmd satisfies session.CmdFunc without importing it).
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/agent/control":    true,
		"quake2web/server/internal/agent/decide":     true,
		"quake2web/server/internal/agent/mapdata":    true,
		"quake2web/server/internal/agent/nav":        true,
		"quake2web/server/internal/agent/nav/navrt":  true,
		"quake2web/server/internal/agent/nav/navsim": true,
		"quake2web/server/internal/agent/perception": true,
		"quake2web/server/internal/agent/route":      true,
		"quake2web/server/internal/agent/routeexec":  true,
		"quake2web/server/internal/agent/worldmodel": true,
		"quake2web/server/internal/fakeclient":       true,
		"quake2web/server/internal/q2const":          true,
		"quake2web/server/internal/qcommon/shared":   true,
	}
	checkImports(t, "bot", allowed)
}

func checkImports(t *testing.T, pkg string, allowed map[string]bool) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, "quake2web/") && !allowed[path] {
				t.Errorf("%s imports %s", f, path)
			}
		}
	}
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := exec.LookPath(gobin); err != nil {
		if gobin, err = exec.LookPath("go"); err != nil {
			t.Skip("go command not found; transitive check skipped")
		}
	}
	out, err := exec.Command(gobin, "list", "-deps", "-f", "{{.ImportPath}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, f := range []string{"internal/sv", "internal/game", "internal/world", "internal/host", "internal/api", "internal/agent/session", "internal/agent/nav/navbuild"} {
			if p := "quake2web/server/" + f; dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("%s links %s", pkg, dep)
			}
		}
	}
}
