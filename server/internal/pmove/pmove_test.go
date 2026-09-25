package pmove

import (
	"testing"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

func floorWorld(t testing.TB) *cmodel.State {
	t.Helper()
	m, err := cmodel.LoadMapBytes("synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return cmodel.NewState(m)
}

func newPM(cm *cmodel.State, origin Vec3) *PmoveT {
	pm := &PmoveT{}
	pm.S.Gravity = 800
	for i := 0; i < 3; i++ {
		pm.S.Origin[i] = int16(origin[i] * 8)
	}
	pm.Trace = func(start, mins, maxs, end *Vec3) shared.Trace {
		t := cm.BoxTrace(*start, *end, *mins, *maxs, 0, q2const.MASK_PLAYERSOLID)
		if t.Fraction < 1 {
			t.Ent = 0
		}
		return t
	}
	pm.PointContents = func(p Vec3) int32 { return cm.PointContents(p, 0) }
	return pm
}

func run(mv *Mover, pm *PmoveT, cmd shared.UserCmd, n int) {
	for i := 0; i < n; i++ {
		pm.Cmd = cmd
		mv.Pmove(pm)
	}
}

func TestStandOnFloor(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{0, 0, 24.125})
	mv := NewMover()
	run(mv, pm, shared.UserCmd{Msec: 16}, 30)
	if pm.GroundEntity == NoEnt || pm.S.PmFlags&q2const.PMF_ON_GROUND == 0 {
		t.Fatalf("not on ground: %+v", pm.S)
	}
	// resting on the slab top (z=0) with mins.z = -24
	if pm.S.Origin[2] < 24*8 || pm.S.Origin[2] > 25*8 {
		t.Errorf("rest height %d", pm.S.Origin[2])
	}
	if pm.ViewHeight != 22 || pm.Mins != (Vec3{-16, -16, -24}) || pm.Maxs != (Vec3{16, 16, 32}) {
		t.Errorf("hull %v %v %v", pm.ViewHeight, pm.Mins, pm.Maxs)
	}
	if pm.S.Velocity != [3]int16{} {
		t.Errorf("velocity %v", pm.S.Velocity)
	}
}

func TestWalkAndJump(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{0, 0, 24.125})
	mv := NewMover()
	run(mv, pm, shared.UserCmd{Msec: 16}, 5)
	x0 := pm.S.Origin[0]
	run(mv, pm, shared.UserCmd{Msec: 16, ForwardMove: 200}, 10) // yaw 0 = +x
	if pm.S.Origin[0] <= x0 {
		t.Fatalf("did not walk forward: %d -> %d", x0, pm.S.Origin[0])
	}
	if pm.S.Velocity[0] <= 0 || pm.S.Velocity[0] > 300*8 {
		t.Errorf("walk velocity %v", pm.S.Velocity)
	}
	// stop, then jump in place
	run(mv, pm, shared.UserCmd{Msec: 16, ForwardMove: -200}, 3)
	run(mv, pm, shared.UserCmd{Msec: 16}, 60)
	z0 := pm.S.Origin[2]
	pm.Cmd = shared.UserCmd{Msec: 16, UpMove: 400}
	mv.Pmove(pm)
	if pm.S.Velocity[2] <= 0 || pm.S.PmFlags&q2const.PMF_JUMP_HELD == 0 || pm.GroundEntity != NoEnt {
		t.Fatalf("jump did not start: %+v ground %d", pm.S, pm.GroundEntity)
	}
	run(mv, pm, shared.UserCmd{Msec: 16, UpMove: 400}, 5)
	if pm.S.Origin[2] <= z0 {
		t.Errorf("did not rise: %d -> %d", z0, pm.S.Origin[2])
	}
	// land again
	run(mv, pm, shared.UserCmd{Msec: 16}, 100)
	if pm.GroundEntity == NoEnt {
		t.Error("did not land")
	}
}

func TestDuck(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{0, 0, 24.125})
	mv := NewMover()
	run(mv, pm, shared.UserCmd{Msec: 16}, 5)
	run(mv, pm, shared.UserCmd{Msec: 16, UpMove: -200}, 2)
	if pm.S.PmFlags&q2const.PMF_DUCKED == 0 || pm.Maxs[2] != 4 || pm.ViewHeight != -2 {
		t.Fatalf("not ducked: %+v %v %v", pm.S, pm.Maxs, pm.ViewHeight)
	}
	run(mv, pm, shared.UserCmd{Msec: 16}, 1)
	if pm.S.PmFlags&q2const.PMF_DUCKED != 0 {
		t.Error("did not stand up")
	}
}

func TestFallOffEdge(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{200, 0, 24.125})
	mv := NewMover()
	run(mv, pm, shared.UserCmd{Msec: 50}, 10)
	if pm.GroundEntity != NoEnt || pm.S.Velocity[2] >= 0 || pm.S.Origin[2] >= 24*8 {
		t.Errorf("should be falling: %+v", pm.S)
	}
}

func TestSpectatorAndFreeze(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{0, 0, -8}) // inside the slab: spectators fly through
	pm.S.PmType = q2const.PM_SPECTATOR
	mv := NewMover()
	run(mv, pm, shared.UserCmd{Msec: 50, UpMove: -400}, 5)
	if pm.S.Origin[2] >= -8*8 {
		t.Errorf("spectator did not move down: %d", pm.S.Origin[2])
	}
	pm.S.PmType = q2const.PM_FREEZE
	before := pm.S
	run(mv, pm, shared.UserCmd{Msec: 50, ForwardMove: 400}, 5)
	if pm.S != before {
		t.Error("freeze moved")
	}
}

func TestClampAngles(t *testing.T) {
	cm := floorWorld(t)
	pm := newPM(cm, Vec3{0, 0, 24.125})
	mv := NewMover()
	pm.Cmd = shared.UserCmd{Msec: 1, Angles: [3]int16{int16(shared.ANGLE2SHORT(120)), 0, 0}}
	mv.Pmove(pm)
	if pm.ViewAngles[q2const.PITCH] != 89 {
		t.Errorf("pitch clamp %v", pm.ViewAngles)
	}
	// SHORT2ANGLE of a signed short is in [-180,180): the >= 180 branch of
	// PM_ClampAngles is unreachable, so looking far down is not clamped.
	pm.Cmd = shared.UserCmd{Msec: 1, Angles: [3]int16{int16(shared.ANGLE2SHORT(200)), 0, 0}}
	mv.Pmove(pm)
	if pm.ViewAngles[q2const.PITCH] >= 0 || pm.ViewAngles[q2const.PITCH] < -161 {
		t.Errorf("pitch -160 %v", pm.ViewAngles)
	}
}

func TestSnapInitial(t *testing.T) {
	cm := floorWorld(t)
	// origin with the box poking 1/8 unit into the slab: initial snap moves it out
	pm := newPM(cm, Vec3{0, 0, 23.875})
	pm.SnapInitial = true
	mv := NewMover()
	pm.Cmd = shared.UserCmd{Msec: 1}
	mv.Pmove(pm)
	if pm.S.Origin[2] < 24*8 {
		t.Errorf("snap initial did not free the player: %d", pm.S.Origin[2])
	}
}

func BenchmarkPmove(b *testing.B) {
	cm := floorWorld(b)
	pm := newPM(cm, Vec3{0, 0, 24.125})
	mv := NewMover()
	cmds := []shared.UserCmd{
		{Msec: 16, ForwardMove: 400}, {Msec: 16, SideMove: 400, Angles: [3]int16{0, 8000, 0}},
		{Msec: 16, ForwardMove: -400}, {Msec: 16, SideMove: -400, Angles: [3]int16{0, -8000, 0}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pm.Cmd = cmds[(i/16)&3]
		mv.Pmove(pm)
	}
}

func BenchmarkPmoveDemo1(b *testing.B) {
	cm := demoWorld(b)
	if cm == nil {
		return
	}
	start := demoStart(cm)
	pm := newPM(cm, start)
	mv := NewMover()
	cmds := []shared.UserCmd{
		{Msec: 16, ForwardMove: 400}, {Msec: 16, SideMove: 400, Angles: [3]int16{0, 8000, 0}},
		{Msec: 16, ForwardMove: -400, UpMove: 400}, {Msec: 16, SideMove: -400, Angles: [3]int16{0, -8000, 0}},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2000 == 0 {
			for k := 0; k < 3; k++ {
				pm.S.Origin[k] = int16(start[k] * 8)
			}
			pm.S.Velocity = [3]int16{}
		}
		pm.Cmd = cmds[(i/16)&3]
		mv.Pmove(pm)
	}
}
