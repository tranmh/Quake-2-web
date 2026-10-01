package navsim

import (
	"math"
)

// Recipe names an edge executor.
type Recipe uint8

const (
	RecipeNone Recipe = iota
	// RecipeWalk faces the target and runs (forwardmove 400; pmove caps
	// the speed at 300). It also walks off ledges and up stairs.
	RecipeWalk
	// RecipeCrouch is RecipeWalk ducked (upmove -400, speed 100).
	RecipeCrouch
	// RecipeJump runs towards the target (after an optional back-up for a
	// run-up) and presses jump for one command at the takeoff point. Like
	// RecipeDrop and RecipeLadder it first walks a player that is not at
	// rest at the start back there and stops it (see StopAt): they were
	// validated from rest.
	RecipeJump
	// RecipeDrop walks off a ledge slowly (forwardmove 200) so the fall is
	// steep.
	RecipeDrop
	// RecipeLadder climbs or descends a ladder facing it (forwardmove 200,
	// upmove ±400), then walks to the target.
	RecipeLadder
	// RecipeSwim swims towards the target, pitched at it.
	RecipeSwim
	// RecipeWaterJump swims at a ledge holding forward and up, which makes
	// pmove jump out of the water.
	RecipeWaterJump
	// RecipeRide stands still on a mover while it moves.
	RecipeRide
)

func recipeNames() [9]string {
	return [9]string{"none", "walk", "crouch", "jump", "drop", "ladder", "swim", "waterjump", "ride"}
}

// String returns the lower-case recipe name.
func (r Recipe) String() string {
	if names := recipeNames(); int(r) < len(names) {
		return names[r]
	}
	return "?"
}

// ParseRecipe is the inverse of String.
func ParseRecipe(s string) (Recipe, bool) {
	for i, n := range recipeNames() {
		if n == s {
			return Recipe(i), true
		}
	}
	return RecipeNone, false
}

// Plan describes how to execute one edge; it is stored with the edge so the
// follower runs exactly what the builder validated.
type Plan struct {
	Recipe Recipe
	// From is the edge start and Target where the executor heads (the edge
	// end, or the point to touch).
	From, Target Vec3
	// Takeoff is where RecipeJump presses jump.
	Takeoff Vec3
	// Forward overrides the forwardmove (0: 400 for walk and jump, 200 for
	// drop).
	Forward int16
	// BackupMsec makes RecipeJump back away from the target this long
	// before running (a run-up).
	BackupMsec int
	// Yaw is the ladder facing of RecipeLadder (towards the ladder).
	Yaw float32
	// StepMsec is the command length (0: 25).
	StepMsec int
}

// Executor produces the commands of one recipe, one step at a time, from
// the current state. It may probe the world but never changes it.
type Executor interface {
	Next(w *World, s *State) Cmd
}

// Executor returns a fresh executor for the plan.
func (p Plan) Executor() Executor {
	msec := p.StepMsec
	if msec <= 0 {
		msec = 25
	}
	fwd := p.Forward
	switch p.Recipe {
	case RecipeCrouch:
		if fwd == 0 {
			fwd = 400
		}
		return &walker{target: p.Target, fwd: fwd, up: -400, msec: msec}
	case RecipeDrop:
		if fwd == 0 {
			fwd = 200
		}
		return &walker{target: p.Target, fwd: fwd, msec: msec, stop: &stopper{target: p.From}}
	case RecipeJump:
		if fwd == 0 {
			fwd = 400
		}
		return newJumper(p, fwd, msec)
	case RecipeLadder:
		return &climber{target: p.Target, yaw: p.Yaw, msec: msec, stop: stopper{target: p.From}}
	case RecipeSwim:
		return &swimmer{target: p.Target, msec: msec}
	case RecipeWaterJump:
		return &swimmer{target: p.Target, jump: true, msec: msec}
	case RecipeRide:
		return &rider{target: p.Target, msec: msec}
	}
	if fwd == 0 {
		fwd = 400
	}
	return &walker{target: p.Target, fwd: fwd, msec: msec}
}

// YawTo returns the yaw (degrees, [0, 360)) from a to b in the horizontal
// plane, or ok=false when they are closer than half a unit.
func YawTo(a, b Vec3) (yaw float32, ok bool) {
	dx, dy := float64(b[0]-a[0]), float64(b[1]-a[1])
	if dx*dx+dy*dy < 0.25 {
		return 0, false
	}
	y := math.Atan2(dy, dx) * 180 / math.Pi
	if y < 0 {
		y += 360
	}
	return float32(y), true
}

// PitchTo returns the pitch (degrees, > 0 down) that looks from a to b.
func PitchTo(a, b Vec3) float32 {
	dx, dy, dz := float64(b[0]-a[0]), float64(b[1]-a[1]), float64(b[2]-a[2])
	return float32(-math.Atan2(dz, math.Hypot(dx, dy)) * 180 / math.Pi)
}

// heading keeps the last yaw while the target is right below/above.
type heading struct {
	yaw  float32
	have bool
}

func (h *heading) to(a, b Vec3) float32 {
	if y, ok := YawTo(a, b); ok || !h.have {
		h.yaw, h.have = y, true
	}
	return h.yaw
}

// Stopping. The jump, drop and ladder executors were validated from rest
// at the start node, but a follower switches to them at full speed. They
// therefore begin by steering the player back to the start and stopping
// it there (a no-op for a player already at rest there, so runs from rest
// are unchanged); StopAt offers the same to followers for other edges.
const (
	// StopRadius and StopSpeed: a player within StopRadius (horizontally)
	// of the stop point and slower than StopSpeed is at rest there.
	StopRadius = 1
	StopSpeed  = 10
	// stopTau is the time constant of the approach (the wanted velocity is
	// the remaining distance / stopTau), stopMaxMsec the time it gets.
	stopTau     = 0.1
	stopMaxMsec = 2000
)

// Stopped reports whether s is at rest at p (see StopRadius).
func Stopped(s *State, p Vec3) bool {
	o := s.Origin()
	return math.Hypot(float64(o[0]-p[0]), float64(o[1]-p[1])) <= StopRadius && s.HSpeed() <= StopSpeed
}

// StopAt returns an executor that walks the player to p and stops it there
// (Stopped), for a follower that must start an edge flagged from rest.
// After Stopped holds, or 2 s, or when the player starts off the ground or
// swims, it returns idle commands.
func StopAt(p Vec3, msec int) Executor {
	if msec <= 0 {
		msec = 25
	}
	return &stopExec{st: stopper{target: p}, msec: msec}
}

type stopExec struct {
	st   stopper
	msec int
}

func (e *stopExec) Next(_ *World, s *State) Cmd {
	if c, ok := e.st.next(s, e.msec); ok {
		return c
	}
	c := Cmd{Msec: uint8(e.msec), Yaw: s.ViewYaw}
	if s.Ducked() {
		c.Up = -400
	}
	return c
}

// stopper steers to target and stops there: each command asks pmove for
// the velocity that closes the remaining distance within stopTau, given
// what ground friction leaves of the current one (PM_Friction, then
// PM_Accelerate along the velocity error).
type stopper struct {
	target  Vec3
	elapsed int
	started bool
	done    bool
}

// next returns the command, or ok=false once the player is at rest at the
// target, swimming, or the time is up; from then on it always reports
// false. A player that is not on the ground at the first command (on a
// ladder, falling) is left alone; one that leaves the ground for a moment
// later on (a bump) gets idle commands until it lands.
func (e *stopper) next(s *State, msec int) (c Cmd, ok bool) {
	if e.done {
		return Cmd{}, false
	}
	if !e.started && !s.OnGround() {
		e.done = true
		return Cmd{}, false
	}
	e.started = true
	if Stopped(s, e.target) || s.WaterLevel >= 2 || e.elapsed >= stopMaxMsec {
		e.done = true
		return Cmd{}, false
	}
	e.elapsed += msec
	if !s.OnGround() {
		c = Cmd{Msec: uint8(msec), Yaw: s.ViewYaw}
		if s.Ducked() {
			c.Up = -400
		}
		return c, true
	}
	o, v := s.Origin(), s.Velocity()
	dt := float64(msec) / 1000
	// velocity after friction (pm_friction 6, pm_stopspeed 100)
	vx, vy := float64(v[0]), float64(v[1])
	if full := math.Sqrt(vx*vx + vy*vy + float64(v[2])*float64(v[2])); full >= 1 {
		f := math.Max(full-math.Max(full, 100)*6*dt, 0) / full
		vx, vy = vx*f, vy*f
	} else {
		vx, vy = 0, 0
	}
	maxSpeed := 300.0
	if s.Ducked() {
		maxSpeed = 100
	}
	wx, wy := float64(e.target[0]-o[0])/stopTau, float64(e.target[1]-o[1])/stopTau
	if l := math.Hypot(wx, wy); l > maxSpeed {
		wx, wy = wx*maxSpeed/l, wy*maxSpeed/l
	}
	ex, ey := wx-vx, wy-vy
	c = Cmd{Msec: uint8(msec), Yaw: s.ViewYaw}
	if s.Ducked() {
		c.Up = -400
	}
	if el := math.Hypot(ex, ey); el >= 0.5 {
		// pm_accelerate 10: one command adds 10*wishspeed*dt along wishdir
		c.Forward = int16(math.Min(el/(10*dt), maxSpeed) + 0.5)
		yaw := math.Atan2(ey, ex) * 180 / math.Pi
		if yaw < 0 {
			yaw += 360
		}
		c.Yaw = float32(yaw)
	}
	return c, true
}

type walker struct {
	target  Vec3
	fwd, up int16
	msec    int
	h       heading
	// stop: come to rest at the start first (drops)
	stop *stopper
}

func (e *walker) Next(_ *World, s *State) Cmd {
	if e.stop != nil {
		if c, ok := e.stop.next(s, e.msec); ok {
			return c
		}
	}
	return Cmd{Msec: uint8(e.msec), Forward: e.fwd, Up: e.up, Yaw: e.h.to(s.Origin(), e.target)}
}

type jumper struct {
	target, takeoff Vec3
	dir             [2]float32
	fwd             int16
	backup, msec    int
	elapsed, ground int
	jumped          bool
	h               heading
	stop            stopper
}

func newJumper(p Plan, fwd int16, msec int) *jumper {
	e := &jumper{target: p.Target, takeoff: p.Takeoff, fwd: fwd, backup: p.BackupMsec, msec: msec, stop: stopper{target: p.From}}
	dx, dy := float64(p.Target[0]-p.From[0]), float64(p.Target[1]-p.From[1])
	if l := math.Hypot(dx, dy); l > 0 {
		e.dir = [2]float32{float32(dx / l), float32(dy / l)}
	}
	return e
}

func (e *jumper) Next(_ *World, s *State) Cmd {
	if c, ok := e.stop.next(s, e.msec); ok {
		return c
	}
	o := s.Origin()
	c := Cmd{Msec: uint8(e.msec), Yaw: e.h.to(o, e.target)}
	if e.elapsed < e.backup {
		e.elapsed += e.msec
		c.Forward = -e.fwd
		return c
	}
	e.elapsed += e.msec
	c.Forward = e.fwd
	if !e.jumped && s.CanJump() {
		along := (o[0]-e.takeoff[0])*e.dir[0] + (o[1]-e.takeoff[1])*e.dir[1]
		lead := s.HSpeed() * float32(e.msec) / 1000
		// press at the step that reaches the takeoff point, or when stuck
		// against something (a ledge to jump onto)
		if along >= -lead || (e.ground >= 150 && s.HSpeed() < 20) {
			c.Up = 400 // for this command only: checkJump needs a release
			e.jumped = true
		}
	}
	if s.OnGround() {
		e.ground += e.msec
	}
	return c
}

type climber struct {
	target Vec3
	yaw    float32
	msec   int
	h      heading
	stop   stopper
}

func (e *climber) Next(w *World, s *State) Cmd {
	if c, ok := e.stop.next(s, e.msec); ok {
		return c
	}
	o := s.Origin()
	c := Cmd{Msec: uint8(e.msec), Yaw: e.yaw}
	dz := e.target[2] - o[2]
	on := w.OnLadder(s, e.yaw)
	switch {
	case dz > 4:
		// climb; at the top the forward push carries the player over the lip
		c.Forward, c.Up = 200, 400
	case dz < -4 && on:
		c.Forward = 200
		if !s.OnGround() {
			c.Up = -400
		}
	case dz < -4 && s.OnGround():
		// back off the top ledge facing the ladder
		c.Forward = -300
	case dz < -4:
		c.Forward = 200 // falling next to it: lean on the ladder
	default:
		c.Yaw = e.h.to(o, e.target)
		c.Forward = 400
		if on {
			c.Up = 400
		}
	}
	return c
}

type swimmer struct {
	target Vec3
	jump   bool
	msec   int
	h      heading
}

func (e *swimmer) Next(_ *World, s *State) Cmd {
	o := s.Origin()
	c := Cmd{Msec: uint8(e.msec), Forward: 400, Yaw: e.h.to(o, e.target)}
	if e.jump {
		c.Up = 400
		return c
	}
	c.Pitch = PitchTo(o, e.target)
	if s.WaterLevel < 2 && !s.OnGround() && e.target[2] < o[2] {
		c.Pitch = 0 // falling into the water: no swim control yet
	}
	return c
}

type rider struct {
	target Vec3
	msec   int
	h      heading
}

func (e *rider) Next(_ *World, s *State) Cmd {
	return Cmd{Msec: uint8(e.msec), Yaw: e.h.to(s.Origin(), e.target)}
}

// Coast returns an executor that runs ex until *stop is set (by the
// caller's done predicate, which Run calls after every step), then gives
// idle commands so the player coasts to rest: how a touch edge ends once
// its target is set off.
func Coast(ex Executor, stop *bool) Executor { return &coaster{ex: ex, stop: stop} }

type coaster struct {
	ex   Executor
	stop *bool
	last Cmd
}

func (e *coaster) Next(w *World, s *State) Cmd {
	if !*e.stop {
		e.last = e.ex.Next(w, s)
		return e.last
	}
	return idleCmd(e.last)
}

// AtRest reports whether the player stands (or floats) still: on the
// ground or in water, horizontally slower than StopSpeed.
func AtRest(s *State) bool {
	return (s.OnGround() || s.WaterLevel >= 1) && s.HSpeed() <= StopSpeed
}

// Arrival tolerances: an edge succeeds when the player is within ArriveXY
// horizontally and ArriveZ vertically of the edge end.
const (
	ArriveXY = 16
	ArriveZ  = 20
)

// ArriveMode is what the end of an edge requires besides the position.
type ArriveMode uint8

const (
	// ArriveGround: standing on something (or in water).
	ArriveGround ArriveMode = iota
	// ArriveWater: in water (water level >= 1).
	ArriveWater
	// ArriveAny: the position alone (ladder nodes).
	ArriveAny
)

// Near reports whether p is within the arrival tolerances of b.
func Near(p, b Vec3) bool {
	dx, dy, dz := float64(p[0]-b[0]), float64(p[1]-b[1]), math.Abs(float64(p[2]-b[2]))
	return dx*dx+dy*dy <= ArriveXY*ArriveXY && dz <= ArriveZ
}

// Arrived reports whether s counts as having arrived at b.
func Arrived(s *State, b Vec3, mode ArriveMode) bool {
	if !Near(s.Origin(), b) {
		return false
	}
	switch mode {
	case ArriveWater:
		return s.WaterLevel >= 1
	case ArriveAny:
		return true
	}
	return s.OnGround() || s.WaterLevel >= 1
}

// TimeLimitMsec is the validation timeout of an edge of length dist: dist /
// 150 + 1 seconds.
func TimeLimitMsec(dist float32) int { return int(dist/150*1000) + 1000 }

// Sample is the player after one step of a run.
type Sample struct {
	Origin     Vec3
	Ducked     bool
	OnGround   bool
	WaterLevel int8
	// Yaw is the view yaw of the command that led here.
	Yaw float32
}

// Mins returns the hull bottom of the sample.
func (s *Sample) Mins() Vec3 { return StandMins() }

// Maxs returns the hull top of the sample (ducked or standing).
func (s *Sample) Maxs() Vec3 {
	if s.Ducked {
		return DuckMaxs()
	}
	return StandMaxs()
}

// VolumeHit is the first step of a run at which the player was inside a
// volume.
type VolumeHit struct {
	ID   int
	Step int
	Yaw  float32
}

// Outcome is the result of Runner.Run. Its slices are reused when the same
// Outcome is passed to Run again.
type Outcome struct {
	// Done reports that the done predicate held before the time limit.
	Done bool
	Msec int
	// Samples[0] is the start, then one per step.
	Samples []Sample
	// Cmds are the commands that were run.
	Cmds []Cmd
	// TookOff reports that the player left the ground; Takeoff is the
	// origin of the last grounded step before that, TakeoffSpeed the
	// horizontal speed there.
	TookOff      bool
	Takeoff      Vec3
	TakeoffSpeed float32
	// FallDamage is the falling damage of the run, judged the way
	// P_FallingDamage does at server frame boundaries, at the worst of the
	// possible phases of those boundaries relative to the run's start (the
	// server's frame phase is not known in advance), and including the
	// frame the run ends in: when the done predicate stops a run, it looks
	// ahead with an idle command until every phase has judged that frame,
	// then restores the runner.
	FallDamage int
	// Touched are the solids touched, in order of first contact.
	Touched []Contact
	// Volumes are the volumes entered, in order of first entry.
	Volumes []VolumeHit
	// Pushed / Teleported: the last hook that fired (0 for none).
	Pushed, Teleported int
}

// Reset clears o for reuse.
func (o *Outcome) Reset() {
	*o = Outcome{Samples: o.Samples[:0], Cmds: o.Cmds[:0], Touched: o.Touched[:0], Volumes: o.Volumes[:0]}
}

// Run steps ex from the runner's current state until done returns true
// after a step or maxMsec of simulated time pass. out is reset and filled.
func (r *Runner) Run(ex Executor, done func(s *State, res *StepResult) bool, maxMsec int, out *Outcome) {
	out.Reset()
	st := &r.st
	out.Samples = append(out.Samples, sampleOf(st))
	start := st.Msec
	step := 0
	var fall fallJudge
	fall.reset(r.Phys, st.Velocity())
	for st.Msec-start < maxMsec {
		wasGround := st.OnGround()
		before := st.Origin()
		speed := st.HSpeed()
		c := ex.Next(r.World, st)
		res := r.Step(c)
		step++
		out.Cmds = append(out.Cmds, c)
		out.Samples = append(out.Samples, sampleOf(st))
		if wasGround && !st.OnGround() && !out.TookOff {
			out.TookOff, out.Takeoff, out.TakeoffSpeed = true, before, speed
		}
		fall.observe(st)
		for _, id := range res.Touched {
			if !out.HasTouched(id) {
				out.Touched = append(out.Touched, Contact{ID: id, Step: step})
			}
		}
		for _, in := range res.Inside {
			if !hasVolume(out.Volumes, in.ID) {
				out.Volumes = append(out.Volumes, VolumeHit{ID: in.ID, Step: step, Yaw: in.Yaw})
			}
		}
		if res.Pushed != 0 {
			out.Pushed = res.Pushed
		}
		if res.Teleported != 0 {
			out.Teleported = res.Teleported
		}
		if done != nil && done(st, res) {
			out.Done = true
			break
		}
	}
	out.Msec = st.Msec - start
	if out.Done && len(out.Cmds) > 0 {
		r.lookahead(idleCmd(out.Cmds[len(out.Cmds)-1]), fall.pending(), func(s *State) { fall.observe(s) })
	}
	out.FallDamage = fall.worst()
}

// idleCmd is the command that holds still after c: no movement, the same
// view, still ducked when c ducked.
func idleCmd(c Cmd) Cmd {
	h := Cmd{Msec: c.Msec, Yaw: c.Yaw, Pitch: c.Pitch}
	if c.Up < 0 {
		h.Up = c.Up
	}
	return h
}

// maxPhases bounds the frame phases fallJudge tracks (FrameMsec/StepMsec).
const maxPhases = 10

// fallJudge runs P_FallingDamage at every phase the server frame
// boundaries can have relative to a run: phase k judges after the steps s
// (counted from 1) with (s+k) % phases == 0, each with its own
// oldvelocity, and sums the damage.
type fallJudge struct {
	phases, step int
	old          [maxPhases]Vec3
	dmg          [maxPhases]int
}

func (f *fallJudge) reset(p Physics, vel Vec3) {
	*f = fallJudge{phases: 1}
	if p.StepMsec > 0 && p.FrameMsec > p.StepMsec {
		f.phases = min(p.FrameMsec/p.StepMsec, maxPhases)
	}
	for k := range f.old {
		f.old[k] = vel
	}
}

// observe judges the state after the next step at the phases whose frame
// ends there.
func (f *fallJudge) observe(s *State) {
	f.step++
	v := s.Velocity()
	for k := 0; k < f.phases; k++ {
		if (f.step+k)%f.phases != 0 {
			continue
		}
		f.dmg[k] += FallingDamage(v, f.old[k], s.OnGround(), s.WaterLevel)
		f.old[k] = v
	}
}

// pending is the number of further steps in which every phase that did
// not judge the last observed step judges once: the frame each of them is
// in when the run ends.
func (f *fallJudge) pending() int { return f.phases - 1 }

func (f *fallJudge) worst() int {
	w := 0
	for k := 0; k < f.phases; k++ {
		w = max(w, f.dmg[k])
	}
	return w
}

// Contact is the first step of a run at which the player touched a solid.
type Contact struct {
	ID   int
	Step int
}

// HasTouched reports whether the run touched solid id.
func (o *Outcome) HasTouched(id int) bool {
	for _, c := range o.Touched {
		if c.ID == id {
			return true
		}
	}
	return false
}

// HasVolume reports whether the run entered volume id.
func (o *Outcome) HasVolume(id int) bool { return hasVolume(o.Volumes, id) }

func hasVolume(v []VolumeHit, id int) bool {
	for i := range v {
		if v[i].ID == id {
			return true
		}
	}
	return false
}

func sampleOf(s *State) Sample {
	return Sample{Origin: s.Origin(), Ducked: s.Ducked(), OnGround: s.OnGround(), WaterLevel: int8(s.WaterLevel), Yaw: s.ViewYaw}
}

// Settle puts the player at origin and steps idle commands until it stands
// still on something (at most maxMsec). It returns whether it came to rest
// on the ground.
func (r *Runner) Settle(origin Vec3, ducked bool, maxMsec int) bool {
	r.Reset(origin, ducked)
	c := Cmd{}
	if ducked {
		c.Up = -400
	}
	for r.st.Msec < maxMsec {
		r.Step(c)
		if r.st.OnGround() && r.st.PM.Velocity == [3]int16{} && r.st.PM.PmTime == 0 {
			return true
		}
	}
	return false
}
