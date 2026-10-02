package campaign

import (
	"os"
	"testing"
)

func TestZZItems(t *testing.T) {
	if os.Getenv("ZZ_ITEMS") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	md, err := lib.Map(os.Getenv("ZZ_ITEMS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range md.Items {
		e := md.Entity(it.Entity)
		t.Logf("%-28s %6.0f %6.0f %6.0f sf %d", it.Classname, it.Origin[0], it.Origin[1], it.Origin[2], e.Spawnflags)
	}
	n := map[string]int{}
	for _, m := range md.Monsters {
		n[m.Classname]++
	}
	t.Logf("monsters %v", n)
}
