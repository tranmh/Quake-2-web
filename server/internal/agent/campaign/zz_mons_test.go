package campaign

import (
	"os"
	"testing"
)

func TestZZMons(t *testing.T) {
	if os.Getenv("ZZ_MONS") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		md, err := lib.Map(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, mo := range md.Monsters {
			e := md.Entity(mo.Entity)
			t.Logf("%s #%d %s at %v ambush %v trig %v tn %q target %q", m, mo.Entity, mo.Classname, mo.Origin, mo.Ambush, mo.TriggerSpawn, mo.Targetname, mo.Target)
			_ = e
		}
		for _, it := range md.Items {
			t.Logf("%s item #%d %s at %v", m, it.Entity, it.Classname, it.Origin)
		}
	}
}
