package routeexec

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
)

// Navigator is what the executor needs of the navigation runtime
// (*navrt.Navigator implements it).
type Navigator interface {
	SetGoal(goal navrt.Goal, now int64) error
	ClearGoal()
	Status() navrt.Status
	Retry()
	SetAvoid(ents []int32)
	Path() (navrt.Path, int)
	// Assume makes the planner count on blocker b at pose (gone when
	// pose < 0) without having seen it (navrt.Navigator.Assume).
	Assume(b int32, pose int, now int64) bool
	// MapState may return nil (tests): mover effects are then judged from
	// the belief's movers alone.
	MapState() *navrt.MapState
}

// Config configures an Executor. The zero value uses the defaults.
type Config struct {
	// MaxAttempts bounds the attempts at one step before it counts as
	// stalled (0: DefaultMaxAttempts).
	MaxAttempts int
	// FireRange bounds the firing positions of a kill step (units; 0:
	// DefaultFireRange).
	FireRange float32
	// Classes is the class table (pickup names; nil: the default table).
	Classes *perception.ClassTable
	// Logf, when set, receives a line per step event (start, done,
	// retry, stall).
	Logf func(format string, args ...any)
}

// Defaults of Config and step timing.
const (
	DefaultMaxAttempts = 3
	DefaultFireRange   = 640
	// MoveTimeout is the least time an attempt at a movement step (goto,
	// touch, press, shoot, ride, pickup) gets (ms); a planned path
	// extends it to 2.5 x its cost + 20 s.
	MoveTimeout = 45000
	// KillTimeout bounds an attempt at a kill (ms).
	KillTimeout = 120000
	// ConfirmTimeout bounds a confirm, and a wait for effects without a
	// duration (ms).
	ConfirmTimeout = 20000
	// FaceTimeout bounds a face step (ms).
	FaceTimeout = 5000
	// RetryPause is how long the bot holds still before the next attempt
	// at a step (ms).
	RetryPause = 1000
	// StalledAfter is how long without progress makes the objective
	// "stalled" for the decision layer (ms).
	StalledAfter = 15000
	// ExitGrace is how long the bot stays at the goal of a step that
	// claims the level's exit, waiting for the level change, before the
	// attempt counts as failed (the exit did not fire) (ms).
	ExitGrace = 3000
)

// Directional triggers (Touch_Multi fires only for a player whose facing,
// as of the last server frame, is within 90 degrees of its movedir).
const (
	// directionalTol is the facing error a directional touch accepts
	// (degrees).
	directionalTol = 60
	// faceSettle is how long the facing must have held before the touch
	// counts (ms): one server frame, so that the server saw it.
	faceSettle = 100
)

// Status is the state of a step.
type Status uint8

const (
	StepPending Status = iota
	StepRunning
	StepDone
	// StepStalled: the step failed MaxAttempts times; the executor holds
	// until Retry.
	StepStalled
)

// String returns the status name.
func (s Status) String() string {
	switch s {
	case StepRunning:
		return "running"
	case StepDone:
		return "done"
	case StepStalled:
		return "stalled"
	}
	return "pending"
}

// StepState is the progress of one step.
type StepState struct {
	Index    int
	Op       route.Op
	Desc     string
	Status   Status
	Attempts int   // attempts started (the first counts)
	Started  int64 // ms, the current attempt
	Done     int64 // ms, when it was done (0: not yet)
	// Reason says why the last attempt ended (or the step is stalled).
	Reason string
}

// KillOrder is the monster a kill step needs dead.
type KillOrder struct {
	Lump  int
	Class string
	// Track is the monster's track once the bot has seen it ("" until
	// then); Pos its last known position (its spawn origin before).
	Track string
	Pos   Vec3
	// Visible: the track is in view with a line of fire (shootable).
	Visible bool
}

// Directive is what the route wants from the bot this frame, besides the
// navigator goal the executor set.
type Directive struct {
	// Hold: no navigation (wait, face, or nothing to do).
	Hold bool
	// Look is a point the bot should look at when nothing else needs its
	// view (an effect to observe, a monster's spot).
	Look    Vec3
	HasLook bool
	// FaceYaw with MustFace: a facing the step requires (a face step, a
	// directional trigger it is about to touch).
	FaceYaw  float32
	MustFace bool
	// Kill names the monster to fight (a kill step).
	Kill *KillOrder
	// Done: every step is done.
	Done bool
}

// ErrNoSteps is returned by New for an empty table.
var ErrNoSteps = errors.New("routeexec: route has no steps")

// Executor runs the steps of one route table on one level (see the
// package documentation).
type Executor struct {
	t   *route.Table
	md  *mapdata.Map
	g   *nav.Graph
	nav Navigator
	cfg Config

	plans []plan
	steps []StepState
	cur   int
	avoid []int32

	now    int64
	belief *worldmodel.Belief

	// issued: the current attempt's goal is set on the navigator;
	// pauseUntil holds before an attempt; deadline ends it.
	issued     bool
	pauseUntil int64
	deadline   int64
	extended   bool
	// causeAt is when the last action step (not a wait or confirm) was
	// done: the effects waited for were caused then.
	causeAt int64
	// progressAt is the last progress (a step done, or the path left
	// shrinking by progressMin); bestRemain the shortest path left of
	// the current step.
	progressAt int64
	bestRemain float32
	// yielding: the bot holds the navigator for something else since
	// yieldAt (Yield; the attempt's clock stands meanwhile); yielded is
	// the time (ms) the current attempt stood that way, yieldTotal the
	// route's and progressYield yieldTotal at the last progress (the stall
	// clock of Objective counts the time the bot pursued the objective)
	yielding                           bool
	yieldAt                            int64
	yielded, yieldTotal, progressYield int64
	// kill state: the firing goal's anchor point and when it was set.
	killAnchor Vec3
	killGoalAt int64
	killSeen   int64
	// lookGoalAt is when a confirm set a goal to see its effects from
	lookGoalAt int64
	// facedAt is since when the bot faces along the movedir of the
	// directional trigger it touches (0: it does not); exitAt is since when
	// an exit step's goal is reached (0: not yet).
	facedAt int64
	exitAt  int64
	world   *navsim.World
	classes *perception.ClassTable
	// claims are the effects the steps done so far claim, for the
	// planner to assume when it finds no path without them; nopathAt is
	// since when the current attempt has no path, assumed whether it
	// assumed them already.
	claims   []claim
	nopathAt int64
	assumed  bool
}

// claim is an effect a done step caused at.
type claim struct {
	f  *effect
	at int64
}

// New returns an executor for table t on the level of map data md and
// graph g (resolved for the level's skill), moving the bot with nav. It
// resolves every step's entities and poses and sets the avoid set: the
// table's avoid list plus every other activator of an exit that does not
// lead to the table's exit (navrt.ExitTriggers).
func New(t *route.Table, md *mapdata.Map, g *nav.Graph, n Navigator, cfg Config) (*Executor, error) {
	if t == nil || len(t.Steps) == 0 {
		return nil, ErrNoSteps
	}
	if md == nil || g == nil || n == nil {
		return nil, errors.New("routeexec: map data, graph and navigator are required")
	}
	if t.Map != md.Name {
		return nil, fmt.Errorf("routeexec: table %s is for map %s, not %s", t.Name, t.Map, md.Name)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}
	if cfg.FireRange <= 0 {
		cfg.FireRange = DefaultFireRange
	}
	plans, err := resolvePlans(t, md, g)
	if err != nil {
		return nil, err
	}
	avoid, err := avoidSet(t, md, g, plans)
	if err != nil {
		return nil, err
	}
	x := &Executor{t: t, md: md, g: g, nav: n, cfg: cfg, plans: plans, avoid: avoid,
		steps: make([]StepState, len(plans)), classes: cfg.Classes}
	if x.classes == nil {
		x.classes = perception.NewClassTable()
	}
	if md.CM != nil {
		x.world = navsim.NewWorld(md.CM)
	}
	for i := range plans {
		x.steps[i] = StepState{Index: i, Op: plans[i].op, Desc: plans[i].desc}
	}
	n.SetAvoid(avoid)
	return x, nil
}

// avoidSet returns the lump entities the route must never set off.
func avoidSet(t *route.Table, md *mapdata.Map, g *nav.Graph, plans []plan) ([]int32, error) {
	used := map[int32]bool{}
	for _, p := range plans {
		if p.ent >= 0 {
			used[int32(p.ent)] = true
		}
	}
	set := map[int32]bool{}
	for k, a := range t.Avoid {
		e, err := route.Resolve(a.Target, md)
		if err != nil {
			return nil, fmt.Errorf("routeexec: %s avoid %d: %w", t.Name, k, err)
		}
		set[int32(e.Index)] = true
	}
	for _, a := range navrt.ExitTriggers(g, md) {
		if !used[a] && !leadsTo(md, int(a), t.Exit.Map) {
			set[a] = true
		}
	}
	out := make([]int32, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// leadsTo reports whether setting off entity ent ends the level through
// the exit with map string exit.
func leadsTo(md *mapdata.Map, ent int, exit string) bool {
	for _, r := range md.Reach(ent) {
		if r.Response != mapdata.RespExit || r.Disabled {
			continue
		}
		if ex := md.Exit(r.Entity); ex != nil && ex.Level.Raw == exit {
			return true
		}
	}
	return false
}

// Table returns the route table.
func (x *Executor) Table() *route.Table { return x.t }

// Avoid returns the avoid set (sorted lump indexes).
func (x *Executor) Avoid() []int32 { return append([]int32(nil), x.avoid...) }

// Steps returns the state of every step.
func (x *Executor) Steps() []StepState { return append([]StepState(nil), x.steps...) }

// Current returns the state of the current step (the last one once all
// are done).
func (x *Executor) Current() StepState { return x.steps[min(x.cur, len(x.steps)-1)] }

// Index returns the index of the current step (len(steps) once all are
// done).
func (x *Executor) Index() int { return x.cur }

// Done reports whether every step is done.
func (x *Executor) Done() bool { return x.cur >= len(x.plans) }

// Stalled reports whether the current step gave up (see Retry).
func (x *Executor) Stalled() bool { return !x.Done() && x.steps[x.cur].Status == StepStalled }

// LastProgress returns the clock (ms) of the last progress: a step done,
// or the current step's path left shrinking.
func (x *Executor) LastProgress() int64 { return x.progressAt }

// Yield tells the executor that the bot used the navigator for something
// else (a fight the decision layer chose, a pickup, a retreat, exploring):
// the next Update sets the current step's goal again. The current
// attempt's clock stands from the last Update to the next one: its
// deadline and the stall clock of Objective move on by the time yielded,
// so a step does not time out while the bot is busy elsewhere. Yield may
// be called any number of times in between.
func (x *Executor) Yield() {
	x.issued = false
	if !x.yielding {
		x.yielding, x.yieldAt = true, x.now
	}
}

// Engage is Yield for a frame at now (ms) in which the bot does the
// current step's work itself: it fights the kill step's monster the
// Directive named. The next Update sets the step's goal again, but the
// attempt's clock runs on (a pause Yield began ends at now).
func (x *Executor) Engage(now int64) {
	x.issued = false
	x.resume(now)
}

// resume ends a pause Yield began, at now: the current attempt's deadline
// moves on by the time yielded.
func (x *Executor) resume(now int64) {
	if !x.yielding {
		return
	}
	x.yielding = false
	paused := now - x.yieldAt
	if paused <= 0 || x.Done() {
		return
	}
	x.deadline += paused
	x.yielded += paused
	x.yieldTotal += paused
}

// Yielded returns how long (ms) the current attempt stood while the bot
// used the navigator for something else (Yield), up to the last Update.
func (x *Executor) Yielded() int64 { return x.yielded }

// noteProgress records progress at the current clock.
func (x *Executor) noteProgress() {
	x.progressAt, x.progressYield = x.now, x.yieldTotal
}

// Retry starts the current step over with fresh attempts (after the
// campaign's watchdog let the bot explore, say).
func (x *Executor) Retry() {
	if x.Done() {
		return
	}
	st := &x.steps[x.cur]
	st.Attempts, st.Status = 0, StepPending
	x.issued = false
	x.pauseUntil = 0
	x.nav.Retry()
}

// Start begins the route at now (ms): call it once the level's first
// belief arrived.
func (x *Executor) Start(now int64) {
	x.now, x.causeAt = now, now
	x.yielding, x.yieldTotal = false, 0
	x.noteProgress()
	x.cur = 0
	x.claims = x.claims[:0]
	for i := range x.steps {
		x.steps[i] = StepState{Index: i, Op: x.plans[i].op, Desc: x.plans[i].desc}
	}
	x.issued = false
	x.begin()
}

func (x *Executor) logf(format string, args ...any) {
	if x.cfg.Logf != nil {
		x.cfg.Logf(format, args...)
	}
}

func (x *Executor) ms() *navrt.MapState { return x.nav.MapState() }

// begin starts an attempt at the current step.
func (x *Executor) begin() {
	if x.Done() {
		return
	}
	st := &x.steps[x.cur]
	st.Status = StepRunning
	st.Attempts++
	st.Started = x.now
	x.issued = false
	x.extended = false
	x.bestRemain = math.MaxFloat32
	x.killGoalAt, x.killSeen, x.lookGoalAt = 0, 0, 0
	x.facedAt, x.exitAt = 0, 0
	x.nopathAt, x.assumed = 0, false
	x.yielded = 0
	x.deadline = x.now + x.timeout(&x.plans[x.cur])
	x.logf("step %d %s: attempt %d", x.cur, st.Desc, st.Attempts)
}

func (x *Executor) timeout(p *plan) int64 {
	switch p.op {
	case route.OpKill:
		return KillTimeout
	case route.OpWait:
		if p.seconds > 0 {
			return p.seconds + p.seconds/2 + 5000
		}
		return ConfirmTimeout
	case route.OpConfirm:
		return ConfirmTimeout
	case route.OpFace:
		return FaceTimeout
	}
	return MoveTimeout
}

// complete marks the current step done and starts the next one.
func (x *Executor) complete(why string) {
	st := &x.steps[x.cur]
	st.Status, st.Done, st.Reason = StepDone, x.now, why
	x.logf("step %d %s: done (%s) after %.1fs", x.cur, st.Desc, why, float64(x.now-st.Started)/1000)
	if op := x.plans[x.cur].op; op != route.OpWait && op != route.OpConfirm && op != route.OpFace {
		x.causeAt = x.now
		p := &x.plans[x.cur]
		for k := range p.effects {
			x.claims = append(x.claims, claim{f: &p.effects[k], at: x.now})
		}
	}
	x.noteProgress()
	x.nav.ClearGoal()
	x.cur++
	x.begin()
}

// fail ends the current attempt: the next one starts after RetryPause,
// unless the step used up its attempts (stalled).
func (x *Executor) fail(why string) {
	st := &x.steps[x.cur]
	st.Reason = why
	x.nav.ClearGoal()
	x.issued = false
	if st.Attempts >= x.cfg.MaxAttempts {
		st.Status = StepStalled
		x.logf("step %d %s: stalled after %d attempts: %s", x.cur, st.Desc, st.Attempts, why)
		return
	}
	x.logf("step %d %s: attempt %d failed: %s", x.cur, st.Desc, st.Attempts, why)
	x.pauseUntil = x.now + RetryPause
	x.begin()
}

// back re-attempts the action step before the current wait or confirm
// (its effects did not happen).
func (x *Executor) back(why string) {
	st := &x.steps[x.cur]
	st.Reason = why
	if st.Attempts >= x.cfg.MaxAttempts {
		st.Status = StepStalled
		x.nav.ClearGoal()
		x.issued = false
		x.logf("step %d %s: stalled after %d attempts: %s", x.cur, st.Desc, st.Attempts, why)
		return
	}
	prev := x.cur - 1
	for prev >= 0 && (x.plans[prev].op == route.OpWait || x.plans[prev].op == route.OpConfirm || x.plans[prev].op == route.OpFace) {
		prev--
	}
	if prev < 0 {
		x.fail(why)
		return
	}
	x.logf("step %d %s: %s; back to step %d", x.cur, st.Desc, why, prev)
	// the guard keeps its attempt count: the next time it fails it stalls
	attempts := st.Attempts
	for i := prev; i <= x.cur; i++ {
		x.steps[i].Status = StepPending
	}
	x.steps[x.cur].Attempts = attempts
	x.nav.ClearGoal()
	x.cur = prev
	x.pauseUntil = x.now + RetryPause
	x.begin()
}

// Update advances the route on the belief of a new frame (now is the
// belief's clock, ms) and returns the directive for the bot. It sets the
// navigator's goal for the current step unless the bot holds it (Yield).
func (x *Executor) Update(now int64, b *worldmodel.Belief) Directive {
	x.resume(now)
	x.now, x.belief = now, b
	for guard := 0; guard < len(x.plans)+1; guard++ {
		if x.Done() {
			return Directive{Hold: true, Done: true}
		}
		st := &x.steps[x.cur]
		if st.Status == StepStalled {
			return Directive{Hold: true}
		}
		if st.Status == StepPending {
			x.begin()
		}
		if now < x.pauseUntil {
			return Directive{Hold: true}
		}
		before := x.cur
		d := x.run(&x.plans[x.cur])
		if x.cur == before || x.Done() || x.steps[x.cur].Status == StepStalled {
			if x.Done() {
				return Directive{Hold: true, Done: true}
			}
			return d
		}
		// a step finished: the next one may be done already (a ride that
		// carried the bot into the next trigger)
	}
	return Directive{Hold: true}
}

// run runs one frame of step p.
func (x *Executor) run(p *plan) Directive {
	switch p.op {
	case route.OpGoto, route.OpTouch, route.OpPress, route.OpShoot, route.OpRide, route.OpPickup:
		return x.runMove(p)
	case route.OpWait, route.OpConfirm:
		return x.runWait(p)
	case route.OpFace:
		return x.runFace(p)
	case route.OpKill:
		return x.runKill(p)
	}
	x.fail("unknown op")
	return Directive{Hold: true}
}

// issue sets goal on the navigator for the current attempt (once).
func (x *Executor) issue(goal navrt.Goal) bool {
	if x.issued {
		return true
	}
	if err := x.nav.SetGoal(goal, x.now); err != nil {
		x.fail(err.Error())
		return false
	}
	x.issued = true
	return true
}

// navCheck judges the navigator's progress on the current attempt: it
// tracks progress and extends the deadline once a path is planned, and
// fails the attempt on a final navigation failure or the deadline. It
// reports whether the attempt goes on.
func (x *Executor) navCheck() bool {
	st := x.nav.Status()
	if !x.extended && st.Follow == navrt.Following && st.Remaining > 0 {
		x.extended = true
		x.deadline = max(x.deadline, x.steps[x.cur].Started+x.yielded+int64(st.Remaining*2500)+20000)
	}
	if r := x.remaining(); r >= 0 && r < x.bestRemain-progressMin {
		if x.bestRemain != math.MaxFloat32 {
			x.noteProgress()
		}
		x.bestRemain = r
	}
	if st.Follow == navrt.Failed && st.Cause == navrt.CauseNoPath {
		if x.nopathAt == 0 {
			x.nopathAt = x.now
		}
		if !x.assumed && x.now-x.nopathAt >= assumeAfter {
			x.assumed = true
			x.assumeClaims()
		}
	} else {
		x.nopathAt = 0
	}
	switch {
	case st.Follow == navrt.Failed && st.Cause != navrt.CauseNoPath:
		x.fail(fmt.Sprintf("navigation failed: %s %s", st.Cause, st.Reason))
		return false
	case x.now > x.deadline:
		x.fail(fmt.Sprintf("timed out after %.0fs (%.0fs yielded; %s %s %s)", float64(x.now-x.steps[x.cur].Started)/1000, float64(x.yielded)/1000,
			st.Follow, st.Cause, st.Reason))
		return false
	}
	return true
}

// progressMin is the path shortening that counts as progress (units).
const progressMin = 64

// assumeAfter is how long (ms) an attempt has no path before the
// executor assumes the unseen effects the route's done steps claim.
const assumeAfter = 2000

// assumeClaims makes the navigator count on the claimed effects of the
// done steps that the belief has not shown (nor contradicted): doors
// opened and walls removed (static map knowledge: the route was derived
// from the level's entity logic). Movers' poses are left to observation
// (the follower must see a lift or plat where it rides it), and so are
// lasers: a wrong guess about one is lethal, and the tables guard them
// with a confirm step.
func (x *Executor) assumeClaims() {
	for _, c := range x.claims {
		f := c.f
		if f.blocker < 0 {
			continue
		}
		bl := &x.g.Blockers[f.blocker]
		pose := 0
		switch f.kind {
		case route.EffDoorOpen:
			switch {
			case len(bl.Poses) == 2 && bl.Spawn >= 0 && bl.Spawn <= 1:
				pose = 1 - int(bl.Spawn)
			case len(bl.Poses) > 2:
				pose = len(bl.Poses) - 1
			default:
				continue
			}
		case route.EffRemove:
			pose = -1
		default:
			// lasers (lethal) and movers' poses are left to observation
			continue
		}
		if v := x.judge(f, x.belief, c.at); v != unseen {
			continue
		}
		if x.nav.Assume(f.blocker, pose, x.now) {
			x.logf("step %d: no path: assuming %s", x.cur, f.desc)
		}
	}
}

// runMove runs a movement step. It is done when moveDone says so, except
// that a directional touch also needs the facing the trigger accepts (held
// for faceSettle while the bot is in it), and a step that claims the
// level's exit is never done here: the level change ends it, and when
// none came ExitGrace after its goal was reached the attempt fails.
func (x *Executor) runMove(p *plan) Directive {
	d := Directive{}
	if p.hasPoint {
		d.Look, d.HasLook = p.point, true
	}
	done, why := x.moveDone(p)
	if done && p.directional && !x.facing(p) {
		done = false
	}
	if done && !p.exit {
		x.complete(why)
		return d
	}
	goal, err := x.moveGoal(p)
	if err != nil {
		x.fail(err.Error())
		return Directive{Hold: true}
	}
	if !x.issue(goal) || !x.navCheck() {
		return Directive{Hold: true}
	}
	if p.directional && p.trigger != nil && x.belief != nil {
		// a directional trigger fires only for a player facing along its
		// movedir (the navigator faces it on the edges that enter it;
		// this covers a goal node already inside, and the bot standing in
		// it facing elsewhere)
		lo, hi := add(p.trigger.Min, Vec3{-96, -96, -96}), add(p.trigger.Max, Vec3{96, 96, 96})
		if boxTouch(x.belief.Self.Origin, x.belief.Self.Ducked, lo, hi) {
			d.FaceYaw, d.MustFace = p.yaw, true
		}
	}
	if !done {
		x.exitAt = 0
		return d
	}
	// an exit reached: hold there until the level changes
	if x.exitAt == 0 {
		x.exitAt = x.now
		x.logf("step %d %s: %s: waiting for the level change", x.cur, x.steps[x.cur].Desc, why)
	}
	if x.now-x.exitAt >= ExitGrace {
		x.fail(fmt.Sprintf("the exit did not fire within %.0fs of reaching it (%s)", float64(ExitGrace)/1000, why))
		return Directive{Hold: true}
	}
	return d
}

// facing reports whether a directional touch has the facing its trigger
// needs: the bot's view yaw within directionalTol of the movedir for
// faceSettle while its box is in the trigger. A bot no longer in it (it
// passed through, a drop or a ride) cannot be helped by turning now: that
// counts as facing (the navigator faced the trigger on the way in).
func (x *Executor) facing(p *plan) bool {
	b := x.belief
	if p.trigger == nil || b == nil || !boxTouch(b.Self.Origin, b.Self.Ducked, p.trigger.Min, p.trigger.Max) {
		x.facedAt = 0
		return true
	}
	if math.Abs(float64(angleDelta(b.Self.ViewAngles[1], p.yaw))) >= directionalTol {
		x.facedAt = 0
		return false
	}
	if x.facedAt == 0 {
		x.facedAt = x.now
	}
	return x.now-x.facedAt >= faceSettle
}

// moveDone reports whether a movement step is done.
func (x *Executor) moveDone(p *plan) (bool, string) {
	b := x.belief
	switch p.op {
	case route.OpPickup:
		if b != nil {
			if f := b.Effect(worldmodel.EffectItemTaken, p.ent); f != nil && f.At >= x.steps[x.cur].Started {
				return true, "seen taken"
			}
			if name := x.pickupName(p.class); name != "" && b.Inventory.Known && b.Inventory.Count(name) > 0 && strings.HasPrefix(p.class, "key_") {
				return true, "in the inventory"
			}
		}
	case route.OpRide:
		if x.issued && x.nav.Status().Follow == navrt.Arrived {
			return true, "arrived"
		}
		if ms := x.ms(); ms != nil && b != nil {
			bb := ms.Belief(p.blocker)
			if bb.Status == navrt.BlockerAt && bb.Pose == p.pose && x.onMover(p.blocker) {
				return true, "at " + x.g.Blockers[p.blocker].Poses[p.pose].Name
			}
		}
		// carried into the next step's trigger
		if n := x.cur + 1; n < len(x.plans) && x.plans[n].trigger != nil && b != nil &&
			boxTouch(b.Self.Origin, b.Self.Ducked, x.plans[n].trigger.Min, x.plans[n].trigger.Max) {
			return true, "carried into the next trigger"
		}
		return false, ""
	}
	if x.issued && x.nav.Status().Follow == navrt.Arrived {
		return true, "arrived"
	}
	return false, ""
}

// onMover reports whether the bot stands on blocker b (a node on it is
// near).
func (x *Executor) onMover(b int32) bool {
	if x.belief == nil {
		return false
	}
	o := x.belief.Self.Origin
	for _, c := range x.g.Nearby(o, 48) {
		if x.g.Nodes[c.Node].Blocker == b {
			return true
		}
	}
	return false
}

func (x *Executor) pickupName(class string) string {
	for _, c := range x.classes.ForClassname(class) {
		if c.Pickup != "" {
			return c.Pickup
		}
	}
	return ""
}

// moveGoal is the navigator goal of a movement step.
func (x *Executor) moveGoal(p *plan) (navrt.Goal, error) {
	switch p.op {
	case route.OpTouch, route.OpPress:
		return navrt.TouchGoal(int32(p.ent)), nil
	case route.OpShoot:
		return navrt.ShootGoal(int32(p.ent), p.point), nil
	case route.OpPickup:
		return navrt.ItemGoal(int32(p.ent)), nil
	case route.OpRide:
		nodes := x.g.MoverNodes(p.blocker, p.pose)
		if len(nodes) == 0 {
			return navrt.Goal{}, fmt.Errorf("no node on %s at %s", x.g.Blockers[p.blocker].Model, x.g.Blockers[p.blocker].Poses[p.pose].Name)
		}
		return navrt.NodeGoal(nodes...), nil
	}
	// goto
	if p.ent < 0 {
		return navrt.PointGoal(p.point, p.radius), nil
	}
	if mv := x.md.Mover(p.ent); mv != nil {
		master := x.md.Mover(x.md.TeamMaster(p.ent))
		if master != nil && master.Trigger != nil {
			// an auto door: walk into its trigger (it opens)
			return x.volumeGoal(master.Trigger.Min, master.Trigger.Max), nil
		}
		if p.blocker >= 0 {
			pose := -1
			if ms := x.ms(); ms != nil {
				if bb := ms.Belief(p.blocker); bb.Status != navrt.BlockerMoving && bb.Pose >= 0 {
					pose = bb.Pose
				}
			}
			if nodes := x.g.MoverNodes(p.blocker, pose); len(nodes) > 0 {
				return navrt.NodeGoal(nodes...), nil
			}
		}
		r := p.radius + max(mv.Size[0], mv.Size[1])/2
		return navrt.PointGoal(mv.Box.Center(), r), nil
	}
	if tr := x.md.Trigger(p.ent); tr != nil && tr.HasVolume {
		return x.volumeGoal(tr.Box.Min, tr.Box.Max), nil
	}
	return navrt.PointGoal(p.point, p.radius), nil
}

// volumeGoal is a goal of standing in the box lo..hi: the nodes whose
// origin lies in it (arriving at one, within the navigator's arrival
// tolerance, the player box surely touches the box; a volume goal's nodes
// may only graze it, and the bot can stop just short), else the volume.
func (x *Executor) volumeGoal(lo, hi Vec3) navrt.Goal {
	c := mid(lo, hi)
	r := dist3(lo, hi) / 2
	var nodes []nav.NodeID
	for _, cand := range x.g.Nearby(c, r+32) {
		o := x.g.Nodes[cand.Node].Origin
		if o[0] >= lo[0] && o[0] <= hi[0] && o[1] >= lo[1] && o[1] <= hi[1] && o[2] >= lo[2]-24 && o[2] <= hi[2]+24 {
			nodes = append(nodes, cand.Node)
		}
	}
	if len(nodes) == 0 {
		return navrt.VolumeGoal(lo, hi)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })
	return navrt.NodeGoal(nodes...)
}

// runWait holds (looking at the first effect not seen yet) until the
// step's effects are seen or, without effects, its time is up. A wait or
// confirm whose effects the belief contradicts goes back to the step that
// should have caused them. A wait whose effects stay unseen goes on when
// its time is up (the belief cannot tell); a confirm is a guard, so it
// walks to where it can see an effect it has not seen after lookAround,
// and goes back to the step before when its time is up without seeing
// them.
func (x *Executor) runWait(p *plan) Directive {
	d := Directive{Hold: true}
	st := &x.steps[x.cur]
	if !x.issued {
		x.nav.ClearGoal()
		x.issued = true
	}
	all, anyContra, anyPending := true, false, false
	var contra string
	var unseenAt *effect
	for k := range p.effects {
		f := &p.effects[k]
		if !observable(f.kind) {
			continue
		}
		switch v := x.judge(f, x.belief, x.causeAt); v {
		case seen:
		case contradicted:
			all, anyContra = false, true
			if contra == "" {
				contra = f.desc + " did not happen"
			}
		default:
			if v == pending {
				anyPending = true
			}
			if all {
				d.Look, d.HasLook = f.point, true
				unseenAt = f
			}
			all = false
		}
	}
	elapsed := x.now - st.Started
	observableEffects := false
	for _, f := range p.effects {
		observableEffects = observableEffects || observable(f.kind)
	}
	switch {
	case observableEffects && all:
		x.complete("effects seen")
	case !observableEffects && p.op == route.OpWait && elapsed >= p.seconds:
		x.complete("waited")
	case !observableEffects && p.op == route.OpConfirm && elapsed >= 1000:
		x.complete("nothing to observe")
	case anyContra && (p.op == route.OpConfirm || elapsed >= p.seconds) && !anyPending:
		x.back(contra)
	case x.now > x.deadline:
		switch {
		case anyPending:
			x.fail("effects still under way")
		case p.op == route.OpConfirm:
			x.back("effects not seen")
		default:
			x.complete("effects not observed (assumed)")
		}
	case p.op == route.OpConfirm && unseenAt != nil && elapsed >= lookAround:
		// walk to where the effect is in view (it may be out of sight
		// from here); the planner keeps clear of what it guards
		if x.lookGoalAt == 0 || x.nav.Status().Follow == navrt.Failed && x.now-x.lookGoalAt > lookAround {
			nodes := x.firingNodes(unseenAt.point, x.lookGoalAt != 0)
			if len(nodes) > 0 && x.nav.SetGoal(navrt.NodeGoal(nodes...), x.now) == nil {
				x.lookGoalAt = x.now
			}
		}
		if x.lookGoalAt != 0 && x.nav.Status().Follow != navrt.Arrived {
			d.Hold = false
		}
	}
	return d
}

func (x *Executor) runFace(p *plan) Directive {
	d := Directive{Hold: true, FaceYaw: p.yaw, MustFace: true}
	if !x.issued {
		x.nav.ClearGoal()
		x.issued = true
	}
	if x.belief != nil && math.Abs(float64(angleDelta(x.belief.Self.ViewAngles[1], p.yaw))) < 2 {
		x.complete("facing")
		return d
	}
	if x.now > x.deadline {
		x.fail("could not turn")
	}
	return d
}

// angleDelta returns a-b in (-180, 180].
func angleDelta(a, b float32) float32 {
	d := math.Mod(float64(a)-float64(b), 360)
	if d <= -180 {
		d += 360
	} else if d > 180 {
		d -= 360
	}
	return float32(d)
}

// boxTouch reports whether the player box at origin o touches lo..hi.
func boxTouch(o Vec3, ducked bool, lo, hi Vec3) bool {
	mins, maxs := navsim.StandMins(), navsim.StandMaxs()
	if ducked {
		maxs = navsim.DuckMaxs()
	}
	for c := 0; c < 3; c++ {
		if o[c]+mins[c]-1 > hi[c] || o[c]+maxs[c]+1 < lo[c] {
			return false
		}
	}
	return true
}

// remaining returns the path length (units) left to the navigator's goal:
// to the end of the edge being run, then the rest of the path; 0 when
// arrived, -1 without a path.
func (x *Executor) remaining() float32 {
	st := x.nav.Status()
	if st.Follow == navrt.Arrived {
		return 0
	}
	path, cur := x.nav.Path()
	if x.belief == nil || cur >= len(path.Edges) || len(path.Edges) == 0 {
		return -1
	}
	o := x.belief.Self.Origin
	e := &x.g.Edges[path.Edges[cur]]
	r := dist3(o, x.g.Nodes[e.To].Origin)
	for k := cur + 1; k < len(path.Edges); k++ {
		f := &x.g.Edges[path.Edges[k]]
		r += dist3(x.g.Nodes[f.From].Origin, x.g.Nodes[f.To].Origin)
	}
	return r
}

// nextWaypoint returns the end of the edge being run.
func (x *Executor) nextWaypoint() (Vec3, bool) {
	path, cur := x.nav.Path()
	if cur >= len(path.Edges) {
		return Vec3{}, false
	}
	return x.g.Nodes[x.g.Edges[path.Edges[cur]].To].Origin, true
}

// Objective returns the current step for the decision layer (nil once
// every step is done): bearings are degrees from the view yaw, + left.
func (x *Executor) Objective() *decide.ObjectiveView {
	if x.Done() {
		return nil
	}
	p := &x.plans[x.cur]
	st := &x.steps[x.cur]
	ov := &decide.ObjectiveView{Kind: string(p.op), Desc: p.desc, PathDist: -1}
	b := x.belief
	if b == nil {
		return ov
	}
	eye, view := b.Self.Eye, b.Self.ViewAngles[1]
	bearing := func(to Vec3) float32 {
		y, ok := navsim.YawTo(eye, to)
		if !ok {
			return 0
		}
		return angleDelta(y, view)
	}
	at := p.point
	if p.op == route.OpKill {
		if k := x.killOrder(p); k != nil {
			at = k.Pos
		}
	}
	if p.hasPoint || p.op == route.OpKill {
		ov.Bearing = bearing(at)
	}
	if r := x.remaining(); r >= 0 && x.issued {
		ov.PathDist = r
	}
	if w, ok := x.nextWaypoint(); ok && x.issued {
		ov.NextWaypointBearing = bearing(w)
	} else {
		ov.NextWaypointBearing = ov.Bearing
	}
	ov.Stalled = st.Status == StepStalled || st.Attempts > 1 || x.now-x.progressAt-(x.yieldTotal-x.progressYield) > StalledAfter
	ov.Exit = x.exitAhead()
	return ov
}

// Target returns where the current step's objective is (for a kill, the
// monster as the kill order places it), and false when the step has no
// point or every step is done.
func (x *Executor) Target() (Vec3, bool) {
	if x.Done() {
		return Vec3{}, false
	}
	p := &x.plans[x.cur]
	if p.op == route.OpKill {
		if k := x.killOrder(p); k != nil {
			return k.Pos, true
		}
	}
	return p.point, p.hasPoint
}

// exitAhead reports whether the steps left lead straight to the level's
// exit: one of them claims it and none before it is a kill, a pickup or a
// confirmation.
func (x *Executor) exitAhead() bool {
	for i := x.cur; i < len(x.plans); i++ {
		p := &x.plans[i]
		switch p.op {
		case route.OpKill, route.OpPickup, route.OpConfirm:
			return false
		}
		if p.exit {
			return true
		}
	}
	return false
}
