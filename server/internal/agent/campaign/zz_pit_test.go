package campaign

import (
	"context"
	"os"
	"testing"

	"quake2web/server/internal/agent/nav"
)

func TestZZPit(t *testing.T) {
	if os.Getenv("ZZ_PIT") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	_, g, err := lib.Level(context.Background(), "demo3")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []nav.Vec3{{632, -889, -688}, {655, -902, -688}, {927, -904, -472}, {1518, 1404, -808}} {
		id := g.Localize(p, 192)
		if id == nav.NoNode {
			t.Logf("%v: no node", p)
			continue
		}
		n := g.Nodes[id]
		t.Logf("%v: node %d at %v flags %v region %d", p, id, n.Origin, n.Flags, n.Region)
		for _, c := range g.Nearby(p, 0) {
			nd := g.Nodes[c.Node]
			outs := g.Out(c.Node)
			s := ""
			for _, e := range outs {
				s += " ->" + itoa(int(e.To)) + "(r" + itoa(int(g.Nodes[e.To].Region)) + " k" + itoa(int(e.Kind)) + ")"
			}
			t.Logf("   n%d %v r%d flags %v outs:%s", c.Node, nd.Origin, nd.Region, nd.Flags, s)
		}
	}
	start := g.Localize(nav.Vec3{1518, 1404, -808}, 192)
	ledge := g.Localize(nav.Vec3{927, -904, -472}, 192)
	reach := g.Reachable([]nav.NodeID{g.Localize(nav.Vec3{632, -889, -688}, 192)}, nil)
	n := 0
	for _, r := range reach {
		if r {
			n++
		}
	}
	t.Logf("pit reaches start: %v ledge %v; %d of %d nodes", reach[start], reach[ledge], n, len(reach))
	back := g.Reachable([]nav.NodeID{start}, nil)
	t.Logf("start reaches pit: %v", back[g.Localize(nav.Vec3{632, -889, -688}, 192)])
}

func itoa(i int) string {
	if i < 0 {
		return "-" + itoa(-i)
	}
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
