package spectate

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

// TestImports keeps the relay away from the server and the game: it sees
// only the bot client's received state (fakeclient), the protocol code and
// the transport. Nothing it links may be server, game, host, API or agent
// code, so a viewer can never reach the bot's game through it.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/fakeclient":     true,
		"quake2web/server/internal/net":            true,
		"quake2web/server/internal/q2const":        true,
		"quake2web/server/internal/qcommon/cmd":    true,
		"quake2web/server/internal/qcommon/cvar":   true,
		"quake2web/server/internal/qcommon/msg":    true,
		"quake2web/server/internal/qcommon/shared": true,
		"github.com/coder/websocket":               true, // AcceptOptions for the handler (already a dependency of net)
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
			if strings.Contains(strings.Split(path, "/")[0], ".") && !allowed[path] || strings.HasPrefix(path, "quake2web/") && !allowed[path] {
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
		for _, f := range []string{"internal/sv", "internal/game", "internal/world", "internal/host", "internal/api", "internal/agent", "internal/demo"} {
			if p := "quake2web/server/" + f; dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("links %s", dep)
			}
		}
	}
}
