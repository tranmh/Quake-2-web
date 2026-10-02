package bot

import (
	"math"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
)

// The combat layer. Once per frame (fightTick, retreatTick, reflexTick)
// it turns the intent's movement relative to the target into either a
// navigator goal (advance: towards the target; retreat: to cover, or back
// along the bot's own trail) or its own per-command movement (strafes,
// hold), and arms the reflexes (dodge, grenade escape). Per command the
// driver's Move hook (move) carries those out where the navigator's
// safety check allows (SafeDir: floor, avoided entities, hazards), and the
// Aim hook (aim) aims and runs the fire gate (control.FireGate).

// Combat timing and distances.
const (
	// advanceRepath is how long (ms) an advance goal stands before it is
	// set again; advanceMove how far (units) the target may move first.
	advanceRepath = 1500
	advanceMove   = 128
	// retreatRepath is how long a retreat goal stands at most.
	retreatRepath = 2500
	// strafeLook is the look-ahead (ms) of a strafe's safety check;
	// dodgeLook a dodge's. strafeRoom is the room (units) a strafe side
	// needs before a wall or a body: less, and the side counts as blocked.
	strafeLook = 300
	dodgeLook  = 250
	strafeRoom = 32
	// searchFor is how long (ms) the bot looks towards a hit whose source
	// it did not see.
	searchFor = 1200
	// trailStep is the distance (units) between breadcrumbs of the trail
	// the bot walked; trailMax bounds it.
	trailStep = 64
	trailMax  = 48
	// coverMin and coverMax bound the distance of a cover spot, coverTries
	// the candidates checked for a line of sight.
	coverMin   = 192
	coverMax   = 900
	coverTries = 40
	// threatMemory is how long (ms) an unseen monster still counts as a
	// threat to retreat from.
	threatMemory = 5000
	// routeKillMemory is how long (ms) the monster of a route kill step
	// stays the target after it went out of view.
	routeKillMemory = 2500
	// repositionAfter is how long (ms) the bot may stand still in a fight
	// with a visible target within repositionFar before it backs off to
	// another spot (a corner, a gun blocked by the wall next to it).
	repositionAfter = 1500
	repositionFar   = 900
	// fightBudget is how long (ms) the bot fights one target before it
	// disengages for disengageFor (the target out of reach, a stalemate):
	// it goes on with its objective, shooting on the move.
	fightBudget  = 30000
	disengageFor = 20000
)

// fightGoal is what the navigator does for the fight.
type fightGoal uint8

const (
	goalNone    fightGoal = iota
	goalAdvance           // towards the target
	goalRetreat           // to cover or back along the trail
	goalHeal              // to a health item, out of the fight
)

// fight is the combat layer's state.
type fight struct {
	strafe control.Strafer
	// move is the movement executed in the fight (relative to the target)
	move control.Move
	// goal is the navigator goal the fight set, when, and the point it was
	// set for (the target's position, or the threat fled)
	goal    fightGoal
	goalAt  int64
	goalFor Vec3
	// reflexes armed by the frame: until when, and where to
	dodgeUntil   int64
	dodgeDir     Vec3
	grenadeUntil int64
	grenadeDir   Vec3
	grenadeJump  bool
	// reflexes that acted in the commands since the last tick (trace)
	reflexes []string
	// stuckSince is when the bot last started to stand in a fight without
	// moving (a hold, or both strafe sides blocked; 0: it moves),
	// blockedAt when the strafer last found both sides blocked;
	// repositioning: the retreat goal is the reposition's (it runs to its
	// spot whatever the intent's movement)
	stuckSince, blockedAt int64
	repositioning         bool
	// the fight clock: the target fought since foeSince (last at
	// foeLast), and the one disengaged from until offUntil
	foe               string
	foeSince, foeLast int64
	offFoe            string
	offUntil          int64
	// trail is the bot's own way so far (breadcrumbs, newest last)
	trail []Vec3
}

func (f *fight) reset() {
	*f = fight{trail: f.trail[:0]}
}

// noteReflex records a reflex that acted (each once per tick).
func (f *fight) noteReflex(name string) {
	for _, r := range f.reflexes {
		if r == name {
			return
		}
	}
	f.reflexes = append(f.reflexes, name)
}

// moveOf maps the intent's movement to the controller's.
func moveOf(m decide.Movement) control.Move {
	switch m {
	case decide.MoveAdvance:
		return control.MoveAdvance
	case decide.MoveRetreat:
		return control.MoveRetreat
	case decide.MoveStrafeLeft:
		return control.MoveStrafeLeft
	case decide.MoveStrafeRight:
		return control.MoveStrafeRight
	}
	return control.MoveHold
}

// fireModeOf maps the intent's fire policy to the controller's.
func fireModeOf(p decide.FirePolicy) control.FireMode {
	switch p {
	case decide.FireWhenAligned:
		return control.FireAligned
	case decide.FireSuppress:
		return control.FireSuppress
	}
	return control.FireHold
}

// trailUpdate drops a breadcrumb where the bot stands once it is
// trailStep from the last one.
func (b *Bot) trailUpdate(o Vec3) {
	t := b.fight.trail
	if len(t) > 0 && dist3(t[len(t)-1], o) < trailStep {
		return
	}
	if len(t) == trailMax {
		copy(t, t[1:])
		t = t[:len(t)-1]
	}
	b.fight.trail = append(t, o)
}

// reflexTick arms the reflexes from the frame's projectiles: a dodge of the
// most urgent projectile that will pass close soon (control.DodgeFor), an
// escape from a live grenade near the bot (control.GrenadeEscape).
func (b *Bot) reflexTick(bel *worldmodel.Belief) {
	var in []control.Incoming
	var gs []Vec3
	for i := range bel.Projectiles {
		p := &bel.Projectiles[i]
		if p.Own {
			continue
		}
		var splash float32
		if c := b.classes.ByName(p.Class); c != nil {
			switch c.Weapon {
			case perception.WeaponRocket:
				splash = 120
			case perception.WeaponGrenade:
				splash = 160
				gs = append(gs, p.Pos)
			case perception.WeaponBFG:
				splash = 1000
			}
		}
		in = append(in, control.Incoming{TCA: p.TCA, Miss: p.Miss, Splash: splash, Dir: p.DodgeDir})
	}
	if dir, tca, ok := control.DodgeFor(in); ok {
		b.fight.dodgeUntil = b.now + max(control.DodgeHold, int64(tca*1000)+100)
		b.fight.dodgeDir = dir
	}
	s := &bel.Self
	if dir, jump, ok := control.GrenadeEscape(s.Origin, gs, s.OnGround); ok {
		b.fight.grenadeUntil, b.fight.grenadeDir, b.fight.grenadeJump = b.now+control.DodgeHold, dir, jump
	}
}

// fightTick sets up the frame's movement against target tr: an advance
// goal towards it (or holding once close), a retreat goal, or the
// controller's own strafe or hold.
func (b *Bot) fightTick(bel *worldmodel.Belief, tr *worldmodel.Track) {
	f := &b.fight
	m := moveOf(b.intent.Movement)
	b.takeNav()
	// standing still in a close fight (a hold, or cornered between
	// blocked strafes) for repositionAfter: back off to another spot, all
	// the way there (or for retreatRepath)
	still := m == control.MoveHold || m.Side() != 0 && b.now-f.blockedAt < 300
	switch {
	case f.repositioning && f.goal == goalRetreat && !b.navDone() && b.now-f.goalAt <= retreatRepath:
		m = control.MoveRetreat
		b.moveBy = "reposition"
	case !still || f.goal != goalNone:
		f.stuckSince = 0
	case f.stuckSince == 0:
		f.stuckSince = b.now
	case b.now-f.stuckSince > repositionAfter && tr.Visible && dist3(bel.Self.Origin, tr.Pos) < repositionFar:
		m = control.MoveRetreat
		b.moveBy = "reposition"
		f.repositioning = true
		f.noteReflex("reposition")
	}
	switch m {
	case control.MoveAdvance:
		if tr.Visible && tr.Shootable && dist3(bel.Self.Eye, tr.Pos) < advanceStop(bel.Self.Weapon) {
			// close enough: no further, but not standing still either
			m = control.MoveStrafeRight
			if f.strafe.Current() < 0 {
				m = control.MoveStrafeLeft
			}
			b.moveBy = "in_range"
			break
		}
		if !b.nav.CanReturn(bel.Self.Origin, tr.Pos) {
			m = control.MoveHold // down a drop the bot could not climb back from: wait for it
			b.moveBy = "no_return"
			break
		}
		if f.goal != goalAdvance || b.now-f.goalAt > advanceRepath || dist3(f.goalFor, tr.Pos) > advanceMove || b.navFailed() {
			b.setFightGoal(goalAdvance, navrt.PointGoal(tr.Pos, 96), tr.Pos)
		}
	case control.MoveRetreat:
		if f.goal != goalRetreat || b.now-f.goalAt > retreatRepath || b.navDone() {
			if !b.retreatGoal(bel, tr.Pos) {
				m = control.MoveStrafeRight // nowhere to go: sidestep instead
				if f.strafe.Current() < 0 {
					m = control.MoveStrafeLeft
				}
				b.moveBy = "no_cover"
			}
		}
	}
	if m != control.MoveAdvance && m != control.MoveRetreat {
		// the controller moves the bot itself: no path (the route's goal,
		// an earlier advance or retreat) to follow meanwhile
		if _, has := b.nav.Goal(); has {
			b.nav.ClearGoal()
		}
		f.goal = goalNone
	}
	if m != control.MoveRetreat || b.moveBy != "reposition" {
		f.repositioning = false
	}
	f.move = m
}

// advanceStop is the distance from the target under which an advance stops
// (the weapon's effective range).
func advanceStop(weapon string) float32 {
	switch decide.WeaponFromPickup(weapon) {
	case decide.WeaponSuperShotgun:
		return 160
	case decide.WeaponShotgun:
		return 260
	case decide.WeaponRocketLauncher, decide.WeaponGrenadeLauncher:
		return 400
	case decide.WeaponRailgun:
		return 900
	}
	return 400
}

// setFightGoal makes goal the navigator's for the fight.
func (b *Bot) setFightGoal(kind fightGoal, goal navrt.Goal, at Vec3) bool {
	f := &b.fight
	f.goal, f.goalAt, f.goalFor = kind, b.now, at
	if err := b.nav.SetGoal(goal, b.now); err != nil {
		b.nav.ClearGoal()
		f.goal = goalNone
		return false
	}
	return true
}

// navFailed reports a navigator that gave up on its goal.
func (b *Bot) navFailed() bool { return b.nav.Status().Follow == navrt.Failed }

// navDone reports a navigator that reached or gave up on its goal.
func (b *Bot) navDone() bool {
	switch b.nav.Status().Follow {
	case navrt.Arrived, navrt.Failed, navrt.Idle:
		return true
	}
	return false
}

// threat returns the most dangerous monster the bot knows of now (alive,
// positioned, seen or heard within threatMemory), nil when none.
func (b *Bot) threat(bel *worldmodel.Belief) *worldmodel.Track {
	var best *worldmodel.Track
	bs := float32(-1)
	for i := range bel.Tracks {
		t := &bel.Tracks[i]
		if t.Kind != perception.KindMonster.String() || t.Life != worldmodel.LifeAlive || !t.PosKnown || bel.Time-t.LastUpdate > threatMemory {
			continue
		}
		s := (t.Threat + 1) / max(dist3(bel.Self.Origin, t.Pos), 64)
		if t.Awareness == worldmodel.Idle && !t.Visible {
			s *= 0.25
		}
		if s > bs {
			best, bs = t, s
		}
	}
	return best
}

// retreatGoal sets the navigator to back off from a threat at from: to a
// cover spot (coverSpot), else to a breadcrumb of the trail away from the
// threat. It reports whether a goal was set.
func (b *Bot) retreatGoal(bel *worldmodel.Belief, from Vec3) bool {
	if p, ok := b.coverSpot(bel, from); ok {
		return b.setFightGoal(goalRetreat, navrt.PointGoal(p, 32), from)
	}
	o := bel.Self.Origin
	d0 := dist3(o, from)
	t := b.fight.trail
	for i := len(t) - 1; i >= 0; i-- {
		if dist3(t[i], o) >= coverMin && dist3(t[i], from) > d0+64 && b.nav.CanReturn(o, t[i]) {
			return b.setFightGoal(goalRetreat, navrt.PointGoal(t[i], 32), from)
		}
	}
	return false
}

// coverSpot picks a node to back off to from a threat at from: between
// coverMin and coverMax away, farther from the threat than the bot by a
// margin, on plain floor, one the bot can come back from (not down a
// one-way drop), preferring one the threat cannot see (a line of sight
// from its eye to the node's) and then the nearest.
func (b *Bot) coverSpot(bel *worldmodel.Belief, from Vec3) (Vec3, bool) {
	g := b.lv.Graph
	o := bel.Self.Origin
	d0 := dist3(o, from)
	vis := b.vision()
	threatEye := Vec3{from[0], from[1], from[2] + 24}
	var best Vec3
	found, bestHidden := false, false
	tries := 0
	for _, c := range g.Nearby(o, coverMax) {
		nd := &g.Nodes[c.Node]
		if nd.Flags&(nav.NodeCrouch|nav.NodeLadder|nav.NodeWater|nav.NodeMover|nav.NodeLedge) != 0 {
			continue
		}
		p := nd.Origin
		if dist3(p, o) < coverMin || dist3(p, from) < d0+128 || !b.nav.CanReturn(o, p) {
			continue
		}
		if tries++; tries > coverTries {
			break
		}
		hidden := vis != nil && !vis.LOSBetween(threatEye, Vec3{p[0], p[1], p[2] + 22}, -1)
		if !found || hidden && !bestHidden {
			best, found, bestHidden = p, true, hidden
			if hidden {
				break // the nearest hidden spot
			}
		}
	}
	return best, found
}

// retreatTick runs retreat mode: back off from the main threat to a
// health item when one is known nearby (or the one the policy would pick
// up), else to cover; fight back at whatever is in view meanwhile.
func (b *Bot) retreatTick(bel *worldmodel.Belief, th *worldmodel.Track) {
	f := &b.fight
	b.takeNav()
	if th != nil && th.Visible {
		b.target = th.ID
		if th.ID != b.intent.Target {
			b.targetBy = "retreat_threat"
		}
	}
	if f.goal == goalHeal && !b.navDone() && b.now-f.goalAt < 3*retreatRepath {
		if it := b.healItem(bel); it == nil || !b.giveUpItem(it, bel.Self.Origin) {
			return
		}
		f.goal = goalNone
	}
	it := b.nearestHealth(bel)
	if it == nil {
		// none near: the health the policy would pick up, wherever it is
		if p := b.pickupItem(bel, b.intent.Pickup); p != nil && p.Kind == "health" {
			it = p
		}
	}
	if it != nil && !b.giveUpItem(it, bel.Self.Origin) {
		goal := navrt.PointGoal(it.Pos, 16)
		if it.Lump >= 0 {
			goal = navrt.ItemGoal(int32(it.Lump))
		}
		if b.setFightGoal(goalHeal, goal, it.Pos) {
			return
		}
	}
	if th == nil {
		return
	}
	if f.goal != goalRetreat || b.now-f.goalAt > retreatRepath || b.navDone() {
		b.retreatGoal(bel, th.Pos)
	}
}

// healItem is the item of the heal goal under way (nil when gone).
func (b *Bot) healItem(bel *worldmodel.Belief) *worldmodel.Item {
	for i := range bel.Items {
		it := &bel.Items[i]
		if it.Life == worldmodel.LifeAlive && dist3(it.Pos, b.fight.goalFor) < 1 {
			return it
		}
	}
	return nil
}

// nearestHealth is the nearest health item the bot knows of within
// coverMax*1.5 (straight line), nil when none.
func (b *Bot) nearestHealth(bel *worldmodel.Belief) *worldmodel.Item {
	var best *worldmodel.Item
	bd := float32(coverMax * 1.5)
	for i := range bel.Items {
		it := &bel.Items[i]
		if it.Kind != "health" || it.Life != worldmodel.LifeAlive || b.badItem(it.Pos) || !b.nav.CanReturn(bel.Self.Origin, it.Pos) {
			continue
		}
		if d := dist3(it.Pos, bel.Self.Origin); d < bd {
			best, bd = it, d
		}
	}
	return best
}

// cmdNow is the clock of the command being built (ms).
func (b *Bot) cmdNow() int64 { return b.now + int64(b.cmdSub*navrt.CmdMsec) }

// move is the driver's Move hook: the reflexes and the fight's own
// movement replace the navigator's intent where the way is safe; edges
// that need their exact input (jumps, ducks, swims, a required view) are
// left alone. A strafe side is blocked when its way is unsafe (SafeDir) or
// a wall or a body leaves it less than strafeRoom; the strafer then flips
// to the other side, and holds with both blocked.
func (b *Bot) move(in control.MoveIntent, st *navsim.State) control.MoveIntent {
	if in.MustFace || in.Jump || in.Crouch || in.Swim || in.SwimUp {
		return in
	}
	f := &b.fight
	now := b.cmdNow()
	run := func(dir Vec3, jump bool) control.MoveIntent {
		return control.MoveIntent{WishDir: dir, Speed: control.MaxMove, Jump: jump, FaceYaw: in.FaceYaw, FacePitch: in.FacePitch}
	}
	if now < f.grenadeUntil && b.nav.SafeDir(f.grenadeDir, dodgeLook) {
		f.noteReflex("grenade")
		return run(f.grenadeDir, f.grenadeJump && st.OnGround())
	}
	if now < f.dodgeUntil && b.nav.SafeDir(f.dodgeDir, dodgeLook) {
		f.noteReflex("dodge")
		return run(f.dodgeDir, false)
	}
	if b.mode != ModeFight || f.goal != goalNone {
		return in
	}
	tr := b.liveTrack(b.belief(), b.target)
	if tr == nil {
		return in
	}
	o := st.Origin()
	switch f.move {
	case control.MoveStrafeLeft, control.MoveStrafeRight:
		side := f.strafe.Side(now, f.move.Side(), func(side int) bool {
			dir := control.SideDir(side, o, tr.Pos)
			return b.nav.SafeDir(dir, strafeLook) && b.nav.Clearance(dir, strafeRoom) >= strafeRoom
		})
		if side != 0 {
			return run(control.SideDir(side, o, tr.Pos), false)
		}
		f.blockedAt = now
	}
	return control.MoveIntent{FaceYaw: in.FaceYaw, FacePitch: in.FacePitch}
}

// aimAt aims one command of msec from eye at track tr and runs the fire
// gate with the fire policy mode; it returns the view.
func (b *Bot) aimAt(eye Vec3, view Vec3, tr *worldmodel.Track, mode control.FireMode) (float32, float32) {
	bel := b.belief()
	w, _ := control.WeaponByPickup(bel.Self.Weapon)
	body := control.Body{Origin: tr.Pos, Mins: tr.Mins, Maxs: tr.Maxs, Vel: tr.Vel, OnGround: true}
	if c := b.classes.ByName(tr.Class); c != nil && (c.Flying || c.Swimming) {
		body.OnGround = false
	}
	if body.Maxs == body.Mins {
		body.Mins, body.Maxs = Vec3{-16, -16, -24}, Vec3{16, 16, 32}
	}
	vis := b.vision()
	shootable := func(p Vec3) bool { return vis != nil && vis.Shootable(p, -1) }
	p, feet := control.AimPoint(eye, body, w)
	line := shootable(p)
	if feet && !line {
		body.OnGround = false
		p, _ = control.AimPoint(eye, body, w)
		line = shootable(p)
	}
	if !line && tr.Visible {
		// the middle hidden (a ledge, a crate): the head may show
		head := Vec3{p[0], p[1], p[2] + (body.Maxs[2]-body.Mins[2])/2 - 8}
		if shootable(head) {
			p, line = head, true
		}
	}
	y, pt := b.shoot.Turn(eye, view[q2const.YAW], view[q2const.PITCH], p, navrt.CmdMsec)
	c := body.Center()
	in := control.FireInput{Mode: mode, Weapon: w, Eye: eye, Yaw: y, Pitch: pt, Aim: p, Radius: body.Radius(), TargetDist: dist3(eye, c),
		Visible: tr.Visible, Shootable: line}
	reach := shotReach(w, dist3(eye, p), body.Radius())
	in.Neutral, in.Barrel = b.lineOfFire(bel, vis, w, eye, y, pt, p, reach)
	if w.HasSplash() && vis != nil {
		in.WallClose = !shootable(viewPoint(eye, y, pt, control.SplashSafe))
	}
	v := control.FireGate(in)
	b.fire = v.Fire
	if v.Vetoed() {
		b.fight.noteReflex("hold_fire:" + v.Reason)
	}
	return y, pt
}

// shotReach is how far (units) the line of fire of weapon w runs for an
// aim point d away at a target of half size radius: through the target
// for a single projectile or bolt (a shot within the aim tolerance stops
// in it), on to the far wall for shots that go on past it (the railgun's
// slug pierces bodies; pellets and bullets of a spread pattern miss it).
func shotReach(w control.Weapon, d, radius float32) float32 {
	if w.Pickup == "Railgun" || w.Spread > 0 {
		return maxShot
	}
	return d + 2*radius
}

// maxShot is the range of a hitscan shot (fire_lead, fire_rail: 8192).
const maxShot = 8192

// lineOfFire reports a neutral, and an explosive barrel within
// control.BarrelSafe of the eye, in the line of fire of a command of
// weapon w from eye with view yaw/pitch at aim point p: along the view the
// shot actually takes and along the line to p (the view slews onto it),
// each reach units long, widened by the weapon's spread at the body's
// distance.
func (b *Bot) lineOfFire(bel *worldmodel.Belief, vis *perception.Vision, w control.Weapon, eye Vec3, yaw, pitch float32, p Vec3, reach float32) (neutral, barrel bool) {
	shot := viewPoint(eye, yaw, pitch, reach)
	aim := p
	if d := dist3(eye, p); d > 1 {
		f := reach / d
		aim = Vec3{eye[0] + (p[0]-eye[0])*f, eye[1] + (p[1]-eye[1])*f, eye[2] + (p[2]-eye[2])*f}
	}
	in := func(k perception.Kind, within float32) bool {
		return b.inLine(bel, vis, eye, shot, k, within, w.Spread) || b.inLine(bel, vis, eye, aim, k, within, w.Spread)
	}
	return in(perception.KindNeutral, 0), in(perception.KindBarrel, control.BarrelSafe)
}

// vetoNavFire runs the fire gate on a command in which the navigator pulls
// the trigger itself (a route shoot goal: a shootable button) from eye
// with view yaw/pitch, and reports a veto: a neutral or a near barrel in
// the line of fire, a splash weapon at the goal or a wall too close. The
// navigator judges the aim; the goal is a brush, visible and shootable by
// its own account.
func (b *Bot) vetoNavFire(eye Vec3, yaw, pitch float32) bool {
	g, ok := b.nav.Goal()
	if !ok || g.Kind != navrt.GoalShoot {
		return false
	}
	bel := b.belief()
	w, _ := control.WeaponByPickup(bel.Self.Weapon)
	vis := b.vision()
	d := dist3(eye, g.Point)
	in := control.FireInput{Mode: control.FireAligned, Weapon: w, Eye: eye, Yaw: yaw, Pitch: pitch, Aim: g.Point, Radius: 8, TargetDist: d,
		Visible: true, Shootable: true}
	in.Neutral, in.Barrel = b.lineOfFire(bel, vis, w, eye, yaw, pitch, g.Point, max(shotReach(w, d, 8), d))
	if w.HasSplash() && vis != nil && d > control.SplashSafe {
		in.WallClose = !vis.Shootable(viewPoint(eye, yaw, pitch, control.SplashSafe), -1)
	}
	v := control.FireGate(in)
	if v.Vetoed() {
		b.fight.noteReflex("hold_fire:" + v.Reason)
		return true
	}
	return false
}

// sincos returns the sine and cosine of an angle in degrees.
func sincos(deg float32) (float32, float32) {
	s, c := math.Sincos(float64(deg) * math.Pi / 180)
	return float32(s), float32(c)
}

// viewPoint is the point d units from eye along the view yaw/pitch.
func viewPoint(eye Vec3, yaw, pitch, d float32) Vec3 {
	sy, cy := math.Sincos(float64(yaw) * math.Pi / 180)
	sp, cp := math.Sincos(float64(pitch) * math.Pi / 180)
	return Vec3{eye[0] + float32(cp*cy)*d, eye[1] + float32(cp*sy)*d, eye[2] - float32(sp)*d}
}

// inLine reports a track of kind k the bot knows of (alive, positioned)
// in the line of fire from eye to p: a neutral body (misc_insane,
// misc_actor), or an explosive barrel within within units of the eye
// (within 0: any distance). The body's box is grown by 4 units plus the
// spread cone (spread degrees) at its distance; a body behind a wall or
// glass (no shot from the eye reaches its middle or its top: vis, when
// known) is not in the line.
func (b *Bot) inLine(bel *worldmodel.Belief, vis *perception.Vision, eye, p Vec3, k perception.Kind, within, spread float32) bool {
	kind := k.String()
	tan := float32(math.Tan(float64(spread) * math.Pi / 180))
	for i := range bel.Tracks {
		t := &bel.Tracks[i]
		dt := dist3(eye, t.Pos)
		if t.Kind != kind || t.Life != worldmodel.LifeAlive || !t.PosKnown || within > 0 && dt > within {
			continue
		}
		mins, maxs := t.Mins, t.Maxs
		if mins == maxs {
			mins, maxs = Vec3{-16, -16, -24}, Vec3{16, 16, 32}
		}
		pad := 4 + dt*tan
		lo := Vec3{t.Pos[0] + mins[0] - pad, t.Pos[1] + mins[1] - pad, t.Pos[2] + mins[2] - pad}
		hi := Vec3{t.Pos[0] + maxs[0] + pad, t.Pos[1] + maxs[1] + pad, t.Pos[2] + maxs[2] + pad}
		if !segmentHitsBox(eye, p, lo, hi) {
			continue
		}
		if vis != nil {
			mid := Vec3{t.Pos[0] + (mins[0]+maxs[0])/2, t.Pos[1] + (mins[1]+maxs[1])/2, t.Pos[2] + (mins[2]+maxs[2])/2}
			top := Vec3{mid[0], mid[1], t.Pos[2] + maxs[2] - 4}
			if !vis.Shootable(mid, -1) && !vis.Shootable(top, -1) {
				continue // behind a wall
			}
		}
		return true
	}
	return false
}

// segmentHitsBox is the slab test of segment a-b against box [lo, hi].
func segmentHitsBox(a, b, lo, hi Vec3) bool {
	t0, t1 := 0.0, 1.0
	for i := 0; i < 3; i++ {
		d := float64(b[i] - a[i])
		if math.Abs(d) < 1e-9 {
			if a[i] < lo[i] || a[i] > hi[i] {
				return false
			}
			continue
		}
		u, w := (float64(lo[i])-float64(a[i]))/d, (float64(hi[i])-float64(a[i]))/d
		if u > w {
			u, w = w, u
		}
		t0, t1 = math.Max(t0, u), math.Min(t1, w)
		if t0 > t1 {
			return false
		}
	}
	return true
}

// searchTick turns the bot towards the last hit whose source it does not
// see (a damage bearing without a target): for searchFor after the hit.
func (b *Bot) searchTick(bel *worldmodel.Belief) {
	n := len(bel.Damage)
	if n == 0 || b.target != "" {
		return
	}
	d := &bel.Damage[n-1]
	if !d.BearingKnown || d.At <= b.searchAt || bel.Time-d.At > 300 {
		return
	}
	b.searchAt, b.searchUntil, b.searchYaw = d.At, d.At+searchFor, d.Bearing
}
