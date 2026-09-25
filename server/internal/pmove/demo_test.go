package pmove

import (
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

func demoWorld(t testing.TB) *cmodel.State {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Skip("no demo1")
	}
	m, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return cmodel.NewState(m)
}

// demoStart returns the first info_player_start origin.
func demoStart(cm *cmodel.State) Vec3 {
	data := cm.Map().EntityString()
	var kv map[string]string
	for {
		tok, rest, more := shared.COM_Parse(data)
		data = rest
		if !more {
			return Vec3{}
		}
		switch tok {
		case "{":
			kv = map[string]string{}
		case "}":
			if kv["classname"] == "info_player_start" {
				var o Vec3
				for i, f := range strings.Fields(kv["origin"]) {
					if i < 3 {
						x, _ := strconv.ParseFloat(f, 32)
						o[i] = float32(x)
					}
				}
				return o
			}
		default:
			v, rest, _ := shared.COM_Parse(data)
			data = rest
			kv[tok] = v
		}
	}
}

func TestDemo1Start(t *testing.T) {
	cm := demoWorld(t)
	start := demoStart(cm)
	pm := newPM(cm, start)
	pm.SnapInitial = true
	mv := NewMover()
	for i := 0; i < 60; i++ {
		pm.Cmd = shared.UserCmd{Msec: 16}
		mv.Pmove(pm)
		pm.SnapInitial = false
	}
	if pm.GroundEntity == NoEnt || pm.S.PmFlags&q2const.PMF_ON_GROUND == 0 {
		t.Fatalf("player did not settle on the floor: %+v", pm.S)
	}
	for i := 0; i < 30; i++ {
		pm.Cmd = shared.UserCmd{Msec: 16, ForwardMove: 400, Angles: [3]int16{0, 4000, 0}}
		mv.Pmove(pm)
	}
	t.Logf("after walking: %+v", pm.S)
}
