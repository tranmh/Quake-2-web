//go:build golden

package pmove

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

type jPMS struct {
	PmType      int32   `json:"pm_type"`
	Origin      []int64 `json:"origin"`
	Velocity    []int64 `json:"velocity"`
	PmFlags     uint8   `json:"pm_flags"`
	PmTime      uint8   `json:"pm_time"`
	Gravity     int16   `json:"gravity"`
	DeltaAngles []int64 `json:"delta_angles"`
}

func (j *jPMS) state() shared.PmoveState {
	return shared.PmoveState{PmType: j.PmType, Origin: testutil.Short3(j.Origin), Velocity: testutil.Short3(j.Velocity),
		PmFlags: j.PmFlags, PmTime: j.PmTime, Gravity: j.Gravity, DeltaAngles: testutil.Short3(j.DeltaAngles)}
}

type jUC struct {
	Msec        uint8   `json:"msec"`
	Buttons     uint8   `json:"buttons"`
	Angles      []int64 `json:"angles"`
	ForwardMove int16   `json:"forwardmove"`
	SideMove    int16   `json:"sidemove"`
	UpMove      int16   `json:"upmove"`
	Impulse     uint8   `json:"impulse"`
	LightLevel  uint8   `json:"lightlevel"`
}

type jStep struct {
	Header *struct {
		Map           string  `json:"map"`
		AirAccelerate float64 `json:"airaccelerate"`
	} `json:"header"`
	Cmd *jUC  `json:"cmd"`
	In  *jPMS `json:"in"`
	Out struct {
		S            jPMS      `json:"s"`
		ViewAngles   []float64 `json:"viewangles"`
		ViewHeight   float64   `json:"viewheight"`
		Mins         []float64 `json:"mins"`
		Maxs         []float64 `json:"maxs"`
		GroundEntity int       `json:"groundentity"`
		WaterType    int32     `json:"watertype"`
		WaterLevel   int32     `json:"waterlevel"`
		NumTouch     int       `json:"numtouch"`
	} `json:"out"`
}

func loadWorld(t *testing.T, name string) *cmodel.State {
	t.Helper()
	testutil.DemoPak(t)
	var fs pak.FS
	if err := fs.AddGameDirectory(filepath.Join(testutil.BaseDir(), "baseq2")); err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Skipf("map %s not available: %v", name, err)
	}
	m, err := cmodel.LoadMapBytes("maps/"+name+".bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return cmodel.NewState(m)
}

// worldCallbacks wires pmove to the world like the oracle (CL_PMTrace without
// entities: world hits carry a non-NULL entity).
func worldCallbacks(cm *cmodel.State, pm *PmoveT) {
	pm.Trace = func(start, mins, maxs, end *Vec3) shared.Trace {
		t := cm.BoxTrace(*start, *end, *mins, *maxs, 0, q2const.MASK_PLAYERSOLID)
		if t.Fraction < 1.0 {
			t.Ent = 0 // world
		}
		return t
	}
	pm.PointContents = func(p Vec3) int32 { return cm.PointContents(p, 0) }
}

func fmtPM(pm *PmoveT) string {
	return fmt.Sprintf("s=%+v viewangles=%s viewheight=%s mins=%v maxs=%v ground=%d watertype=%d waterlevel=%d numtouch=%d",
		pm.S, testutil.FmtVec3(pm.ViewAngles), testutil.FmtF32(pm.ViewHeight), pm.Mins, pm.Maxs,
		pm.GroundEntity, pm.WaterType, pm.WaterLevel, pm.NumTouch)
}

func TestGoldenPmove(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join(testutil.FixturesDir(), "core", "pmove", "*.jsonl"))
	if len(files) == 0 {
		t.Skip("no core/pmove fixtures")
	}
	worlds := map[string]*cmodel.State{}
	for _, path := range files {
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.Run(name, func(t *testing.T) {
			var mv *Mover
			var cm *cmodel.State
			var st shared.PmoveState
			have := false
			n := 0
			err := testutil.ReadJSONL(path, func(lineNo int, line []byte) error {
				var g jStep
				if err := json.Unmarshal(line, &g); err != nil {
					return err
				}
				if g.Header != nil {
					if cm = worlds[g.Header.Map]; cm == nil {
						cm = loadWorld(t, g.Header.Map)
						worlds[g.Header.Map] = cm
					}
					mv = NewMover()
					mv.AirAccelerate = float32(g.Header.AirAccelerate)
					return nil
				}
				if mv == nil || g.Cmd == nil {
					return fmt.Errorf("step before header")
				}
				if g.In != nil {
					st = g.In.state()
					have = true
				}
				if !have {
					return fmt.Errorf("first step without in")
				}
				n++
				pm := &PmoveT{S: st}
				pm.Cmd = shared.UserCmd{Msec: g.Cmd.Msec, Buttons: g.Cmd.Buttons, Angles: testutil.Short3(g.Cmd.Angles),
					ForwardMove: g.Cmd.ForwardMove, SideMove: g.Cmd.SideMove, UpMove: g.Cmd.UpMove,
					Impulse: g.Cmd.Impulse, LightLevel: g.Cmd.LightLevel}
				worldCallbacks(cm, pm)
				in := pm.S
				mv.Pmove(pm)

				o := &g.Out
				ground := 0
				if pm.GroundEntity != NoEnt {
					ground = 1
				}
				ok := pm.S == o.S.state() && testutil.SameVec3(pm.ViewAngles, testutil.Vec3(o.ViewAngles)) &&
					testutil.SameF32(pm.ViewHeight, float32(o.ViewHeight)) &&
					testutil.SameVec3(pm.Mins, testutil.Vec3(o.Mins)) && testutil.SameVec3(pm.Maxs, testutil.Vec3(o.Maxs)) &&
					ground == o.GroundEntity && pm.WaterType == o.WaterType && pm.WaterLevel == o.WaterLevel &&
					pm.NumTouch == o.NumTouch
				if !ok {
					want := &PmoveT{S: o.S.state(), ViewAngles: testutil.Vec3(o.ViewAngles), ViewHeight: float32(o.ViewHeight),
						Mins: testutil.Vec3(o.Mins), Maxs: testutil.Vec3(o.Maxs), GroundEntity: o.GroundEntity - 1,
						WaterType: o.WaterType, WaterLevel: o.WaterLevel, NumTouch: o.NumTouch}
					return fmt.Errorf("step %d (line %d): in %+v cmd %+v\n got  %s\n want %s", n, lineNo, in, pm.Cmd, fmtPM(pm), fmtPM(want))
				}
				st = pm.S
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d steps bit-exact", name, n)
		})
	}
}
