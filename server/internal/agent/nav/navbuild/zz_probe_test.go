package navbuild

import (
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func TestZZProbeFall(t *testing.T) {
	dir := "/tmp/claude-0/-home-user-Quake-2-web/dabb3839-d966-559d-b3c7-ada2b561d25c/scratchpad/" + os.Getenv("ZZDIR")
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		files, _ := filepath.Glob(filepath.Join(dir, m+"-*.json.gz"))
		g, err := nav.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		hist := map[int]int{}
		for i := range g.Edges {
			e := &g.Edges[i]
			if e.FallDamage > 0 {
				hist[int(e.FallDamage)/10*10]++
			}
			a, b := g.Nodes[e.From].Origin, g.Nodes[e.To].Origin
			if m == "demo3" && abs32(a[0]-320) < 9 && abs32(a[1]-192) < 9 && abs32(a[2]+600) < 30 && b[2] < a[2]-150 {
				t.Logf("edge %d %v %v -> %v dmg %d", i, e.Kind, a, b, e.FallDamage)
			}
		}
		t.Logf("%s damage histogram %v", m, hist)
	}
}

func TestZZProbeEdge(t *testing.T) {
	dir := "/tmp/claude-0/-home-user-Quake-2-web/dabb3839-d966-559d-b3c7-ada2b561d25c/scratchpad/navfix"
	files, _ := filepath.Glob(filepath.Join(dir, "demo3-*.json.gz"))
	g0, _ := nav.ReadFile(files[0])
	g := g0.ForSkill(1)
	md := loadMD(t, "demo3")
	v := NewVerifier(g, md.CM)
	for i := range g.Edges {
		e := &g.Edges[i]
		a, b := g.Nodes[e.From].Origin, g.Nodes[e.To].Origin
		if a == (nav.Vec3{320, 192, -599.875}) && b == (nav.Vec3{320, 224, -807.875}) {
			r := v.Edge(i)
			t.Logf("edge %d %+v ok %v", i, *e, r.OK)
			for k, s := range v.out.Samples {
				t.Logf("  %d %v ground %v wl %d", k, s.Origin, s.OnGround, s.WaterLevel)
			}
		}
	}
}

func loadMD(t *testing.T, name string) *mapdata.Map {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	raw, _ := p.ReadFile("maps/" + name + ".bsp")
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	return md
}
