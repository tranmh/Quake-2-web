package perception_test

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

// forbidden are the server-side packages a fair perception must never
// link: everything it knows has to come over the wire.
var forbiddenDeps = []string{
	"quake2web/server/internal/sv",
	"quake2web/server/internal/game",
	"quake2web/server/internal/world",
	"quake2web/server/internal/host",
	"quake2web/server/internal/api",
	"quake2web/server/internal/agent/session",
}

// TestImports keeps perception on the client side of the wire: its non-test
// files import only the standard library and this allowlist, and nothing it
// links (transitively) is server or game code.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/fakeclient":     true,
		"quake2web/server/internal/cmodel":         true,
		"quake2web/server/internal/bsp":            true,
		"quake2web/server/internal/pmove":          true,
		"quake2web/server/internal/assets/md2":     true,
		"quake2web/server/internal/assets/pak":     true,
		"quake2web/server/internal/q2const":        true,
		"quake2web/server/internal/qcommon/shared": true,
		"quake2web/server/internal/agent/mapdata":  true,
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
	checkDeps(t, ".")
}

// checkDeps fails when the package's transitive non-test dependencies
// include a forbidden package.
func checkDeps(t *testing.T, pkg string) {
	t.Helper()
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := exec.LookPath(gobin); err != nil {
		if gobin, err = exec.LookPath("go"); err != nil {
			t.Skip("go command not found; transitive check skipped")
		}
	}
	out, err := exec.Command(gobin, "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, f := range forbiddenDeps {
			if dep == f || strings.HasPrefix(dep, f+"/") {
				t.Errorf("perception links %s", dep)
			}
		}
	}
}
