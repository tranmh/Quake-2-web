package navbuild

import (
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/nav"
)

func TestZZProbeEntry(t *testing.T) {
	dir := "/tmp/claude-0/-home-user-Quake-2-web/dabb3839-d966-559d-b3c7-ada2b561d25c/scratchpad/" + os.Getenv("ZZDIR")
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		files, _ := filepath.Glob(filepath.Join(dir, m+"-*.json.gz"))
		g, err := nav.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		tot, fr := map[string]int{}, map[string]int{}
		for i := range g.Edges {
			e := &g.Edges[i]
			if e.Flags&nav.EdgeFast != 0 {
				continue
			}
			k := e.Kind.String()
			tot[k]++
			if e.Flags&nav.EdgeFromRest != 0 {
				fr[k]++
			}
		}
		t.Logf("%s fromrest %v of simulated %v", m, fr, tot)
	}
}
