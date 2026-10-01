package worldmodel_test

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

// TestImports keeps the world model on the client side of the wire: it
// sees the game only through package perception. Its non-test files import
// only the standard library and this allowlist, and nothing it links is
// server or game code.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/agent/perception": true,
		"quake2web/server/internal/agent/mapdata":    true,
		"quake2web/server/internal/cmodel":           true,
		"quake2web/server/internal/q2const":          true,
		"quake2web/server/internal/qcommon/shared":   true,
	}
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
		for _, f := range []string{"internal/sv", "internal/game", "internal/world", "internal/host", "internal/api", "internal/agent/session"} {
			if p := "quake2web/server/" + f; dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("worldmodel links %s", dep)
			}
		}
	}
}
