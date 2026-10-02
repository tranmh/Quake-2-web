package runner

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImports keeps the runner's view of the game behind the session: it
// starts sessions (and so links the server and the host), but imports no
// game package, never reaches through a session to its server
// (Lockstep.Server, InProc.Instance), and never reads the game's level
// counters (Session.Truth: the campaign reads them, for metrics only).
// The bot's policy (policy.go) imports no session, server or host code at
// all.
func TestImports(t *testing.T) {
	banned := []string{"quake2web/server/internal/game", "quake2web/server/internal/game/"}
	policyAllowed := map[string]bool{
		"quake2web/server/internal/agent/budget":     true,
		"quake2web/server/internal/agent/campaign":   true,
		"quake2web/server/internal/agent/decide":     true,
		"quake2web/server/internal/agent/session":    true, // the publisher's clock and frame number only
		"quake2web/server/internal/agent/trace":      true,
		"quake2web/server/internal/agent/worldmodel": true,
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
		af, err := parser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			for _, b := range banned {
				if path == b || strings.HasSuffix(b, "/") && strings.HasPrefix(path, b) {
					t.Errorf("%s imports %s", f, path)
				}
			}
			if f == "policy.go" && strings.HasPrefix(path, "quake2web/") && !policyAllowed[path] {
				t.Errorf("the bot's policy imports %s", path)
			}
		}
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Truth", "Server", "Instance":
					t.Errorf("%s: calls %s()", fset.Position(call.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
}
