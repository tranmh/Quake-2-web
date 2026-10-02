package budget

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

// TestImports keeps the budget on the decision side of the fairness line:
// its non-test files import only the decision layer, and nothing it links
// is server, game or session code.
func TestImports(t *testing.T) {
	allowed := map[string]bool{"quake2web/server/internal/agent/decide": true}
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
		for _, banned := range []string{"/internal/sv", "/internal/game", "/internal/host", "/internal/agent/session"} {
			if strings.HasSuffix(dep, banned) || strings.Contains(dep, banned+"/") {
				t.Errorf("links %s", dep)
			}
		}
	}
}
