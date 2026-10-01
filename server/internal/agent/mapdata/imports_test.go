package mapdata_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestImports keeps mapdata off package game (and everything above the
// collision layer): its non-test files may only import the standard
// library and these ported low-level packages.
func TestImports(t *testing.T) {
	allowed := map[string]bool{
		"quake2web/server/internal/bsp":            true,
		"quake2web/server/internal/cmodel":         true,
		"quake2web/server/internal/q2const":        true,
		"quake2web/server/internal/qcommon/shared": true,
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
}
