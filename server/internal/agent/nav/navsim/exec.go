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
	// run-up) and presses jump for one command at the takeoff point.
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

var recipeNames = [...]string{"none", "walk", "crouch", "jump", "drop", "ladder", "swim", "waterjump", "ride"}

// String returns the lower-case recipe name.
func (r Recipe) String() string {
	if int(r) < len(recipeNames) {
		return recipeNames[r]
	}
	return "?"
}

// ParseRecipe is the inverse of String.
func ParseRecipe(s string) (Recipe, bool) {
	for i, n := range recipeNames {
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
		return &walker{target: p.Target, fwd: fwd, msec: msec}
	case RecipeJump:
		if fwd == 0 {
			fwd = 400
		}
		return newJumper(p, fwd, msec)
	case RecipeLadder:
		return &climber{target: p.Target, yaw: p.Yaw, msec: msec}
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

type walker struct {
	target  Vec3
	fwd, up int16
	msec    int
	h       heading
}

func (e *walker) Next(_ *World, s *State) Cmd {
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
}

func newJumper(p Plan, fwd int16, msec int) *jumper {
	e := &jumper{target: p.Target, takeoff: p.Takeoff, fwd: fwd, backup: p.BackupMsec, msec: msec}
	dx, dy := float64(p.Target[0]-p.From[0]), float64(p.Target[1]-p.From[1])
	if l := math.Hypot(dx, dy); l > 0 {
		e.dir = [2]float32{float32(dx / l), float32(dy / l)}
	}
	return e
}

func (e *jumper) Next(_ *World, s *State) Cmd {
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
}

func (e *climber) Next(w *World, s *State) Cmd {
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
	// FallDamage is the falling damage summed over the run.
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
		out.FallDamage += res.FallDamage
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
