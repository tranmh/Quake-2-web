package campaign

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImports keeps the campaign's view of the game narrow: it drives the
// session (and so links the server), but imports no server or game
// package itself, never reaches through the session to the server
// (Lockstep.Server, InProc.Instance), and reads the game's level counters
// (Session.Truth, metrics only) in one place, which caches them for
// level_end and nothing else.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/agent/bot":          true,
		"quake2web/server/internal/agent/mapdata":      true,
		"quake2web/server/internal/agent/metrics":      true,
		"quake2web/server/internal/agent/nav":          true,
		"quake2web/server/internal/agent/nav/navbuild": true,
		"quake2web/server/internal/agent/nav/navrt":    true,
		"quake2web/server/internal/agent/route":        true,
		"quake2web/server/internal/agent/session":      true,
		"quake2web/server/internal/agent/trace":        true,
		"quake2web/server/internal/agent/worldmodel":   true,
		"quake2web/server/internal/fakeclient":         true,
		"quake2web/server/internal/q2const":            true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	truthCalls := 0
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
			if strings.HasPrefix(path, "quake2web/") && !allowed[path] {
				t.Errorf("%s imports %s", f, path)
			}
		}
		for _, d := range af.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				switch sel.Sel.Name {
				case "Server", "Instance":
					t.Errorf("%s: %s calls %s()", fset.Position(call.Pos()), fn.Name.Name, sel.Sel.Name)
				case "Truth":
					truthCalls++
					if fn.Name.Name != "checkLevel" {
						t.Errorf("%s: %s reads Truth (metrics only, cached in checkLevel)", fset.Position(call.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
	if truthCalls != 1 {
		t.Errorf("%d Truth calls, want the one in checkLevel", truthCalls)
	}
}
