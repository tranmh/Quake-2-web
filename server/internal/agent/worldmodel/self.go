package worldmodel

import (
	"math"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// View kick cvar defaults (session.Spec forbids changing them for bot
// games). C: game/g_main.c InitGame run_pitch, run_roll, bob_pitch, bob_roll
const (
	runPitch = float32(0.002)
	runRoll  = float32(0.005)
	bobPitch = float32(0.002)
	bobRoll  = float32(0.002)
)

// Damage bearing parameters.
const (
	// kickQuantum is the resolution of player_state_t.kick_angles on the
	// wire (a char of angle*4, truncated).
	kickQuantum = 0.25
	// minKick: a damage kick residual below this is indistinguishable from
	// quantization noise.
	minKick = 0.5
	// firedQuiet: weapon kicks pollute the view kick this long after the
	// bot fired.
	firedQuiet = 300
	// fuseAngle: a damage bearing within this many degrees of a recent
	// attacker is attributed to it.
	fuseAngle = 30
	// fuseMemory: how recent that attacker's flash or sound must be.
	fuseMemory = 1000
	// maxBearingPitch: looking further up or down than this, the forward
	// part of the damage kick is too small to recover the bearing.
	maxBearingPitch = 70
)

// kickModel predicts the parts of player_state_t.kick_angles the bot can
// compute itself: run pitch/roll from its velocity, view bob and the fall
// kick, exactly as SV_CalcViewOffset adds them. What is left on a frame
// with STAT_FLASHES set is the damage kick of P_DamageFeedback.
// C: game/p_view.c:958 ClientEndServerFrame, :222 SV_CalcViewOffset,
// :456 P_FallingDamage
type kickModel struct {
	bobtime   float32 // client->bobtime
	bobmove   float32 // the game's bobmove (kept across frames)
	synced    bool    // bobtime follows the server's
	oldVel    Vec3    // client->oldvelocity
	fallValue float32
	fallFrame int32 // server frame the fall kick started
	hasFall   bool
	lastFrame int32
	started   bool
}

// predict advances the model by one server frame and returns the
// predictable pitch and roll kick for it.
func (k *kickModel) predict(ps *shared.PlayerState, waterlevel int, frame int32) (pitch, roll float32) {
	vel := Vec3{float32(ps.PMove.Velocity[0]) * 0.125, float32(ps.PMove.Velocity[1]) * 0.125, float32(ps.PMove.Velocity[2]) * 0.125}
	onGround := ps.PMove.PmFlags&q2const.PMF_ON_GROUND != 0
	ducked := ps.PMove.PmFlags&q2const.PMF_DUCKED != 0
	if !k.started {
		k.started, k.synced = true, true // a spawn starts with bobtime 0
		k.oldVel = vel
	} else if frame != k.lastFrame+1 {
		k.synced = false // frames were missed: bobtime unknown until a stop
	}
	k.lastFrame = frame

	var fwd, right Vec3
	shared.AngleVectors(ps.ViewAngles, &fwd, &right, nil)

	xyspeed := float32(math.Sqrt(float64(vel[0]*vel[0] + vel[1]*vel[1])))
	if xyspeed < 5 {
		k.bobmove, k.bobtime, k.synced = 0, 0, true
	} else if onGround {
		switch {
		case xyspeed > 210:
			k.bobmove = 0.25
		case xyspeed > 100:
			k.bobmove = 0.125
		default:
			k.bobmove = 0.0625
		}
	}
	k.bobtime += k.bobmove
	bt := k.bobtime
	if ducked {
		bt *= 4
	}
	bobcycle := int32(bt)
	bobfracsin := float32(math.Abs(math.Sin(float64(bt) * math.Pi)))

	// P_FallingDamage: the fall kick
	var delta float32
	switch {
	case k.oldVel[2] < 0 && vel[2] > k.oldVel[2] && !onGround:
		delta = k.oldVel[2]
	case onGround:
		delta = vel[2] - k.oldVel[2]
	}
	delta = float32(float64(delta*delta) * 0.0001)
	switch waterlevel {
	case 3:
		delta = 0
	case 2:
		delta = float32(float64(delta) * 0.25)
	case 1:
		delta = float32(float64(delta) * 0.5)
	}
	if delta >= 15 {
		k.fallValue = min(float32(float64(delta)*0.5), 40)
		k.fallFrame, k.hasFall = frame, true
	}
	k.oldVel = vel

	if ps.PMove.PmType == q2const.PM_DEAD || ps.PMove.PmType == q2const.PM_GIB {
		return 0, 0
	}
	if k.hasFall {
		// ratio = (falltime - level.time) / FALL_TIME, FALL_TIME 0.3 s
		ratio := 1 - float32(frame-k.fallFrame)/3
		if ratio > 0 {
			pitch += ratio * k.fallValue
		} else {
			k.hasFall = false
		}
	}
	pitch += shared.DotProduct(vel, fwd) * runPitch
	roll += shared.DotProduct(vel, right) * runRoll
	if k.synced {
		d := bobfracsin * bobPitch * xyspeed
		if ducked {
			d *= 6
		}
		pitch += d
		d = bobfracsin * bobRoll * xyspeed
		if ducked {
			d *= 6
		}
		if bobcycle&1 != 0 {
			d = -d
		}
		roll += d
	}
	return pitch, roll
}

// lastKick is the damage kick of the last hit, which decays over
// DAMAGE_TIME (0.5 s = 5 frames) unless a new hit with knockback replaces
// it (C: game/p_view.c SV_CalcViewOffset v_dmg_*).
type lastKick struct {
	ok          bool
	frame       int32
	pitch, roll float32
}

func (l *lastKick) at(frame int32) (pitch, roll float32) {
	if !l.ok {
		return 0, 0
	}
	ratio := 1 - float32(frame-l.frame)/5
	if ratio <= 0 {
		l.ok = false
		return 0, 0
	}
	return ratio * l.pitch, ratio * l.roll
}

// selfTracker keeps the bot's own state and turns health and armor drops
// into damage events.
type selfTracker struct {
	kick       kickModel
	hit        lastKick
	prevHealth int
	prevArmor  int
	prevStats  bool
	wasDead    bool
	pickupStat int16
}

func waterlevel(v *perception.Vision, origin Vec3, ducked bool) (level int, contents int32) {
	// C: game/../pmove.c PM_CatagorizePosition (mins z -24, viewheight 22 / -2)
	const minsZ = -24
	viewheight := float32(22)
	if ducked {
		viewheight = -2
	}
	sample2 := viewheight - minsZ
	sample1 := sample2 / 2
	p := origin
	p[2] = origin[2] + minsZ + 1
	c := v.PointContents(p)
	if c&q2const.MASK_WATER == 0 {
		return 0, c
	}
	level, contents = 1, c
	p[2] = origin[2] + minsZ + sample1
	if c2 := v.PointContents(p); c2&q2const.MASK_WATER != 0 {
		level, contents = 2, contents|c2
		p[2] = origin[2] + minsZ + sample2
		if c3 := v.PointContents(p); c3&q2const.MASK_WATER != 0 {
			level, contents = 3, contents|c3
		}
	}
	return level, contents
}

func (st *selfTracker) update(w *World, pc *perception.Percept) {
	ps := &pc.PS
	s := &w.b.Self
	s.Origin = Vec3{float32(ps.PMove.Origin[0]) * 0.125, float32(ps.PMove.Origin[1]) * 0.125, float32(ps.PMove.Origin[2]) * 0.125}
	s.Velocity = Vec3{float32(ps.PMove.Velocity[0]) * 0.125, float32(ps.PMove.Velocity[1]) * 0.125, float32(ps.PMove.Velocity[2]) * 0.125}
	s.Eye, s.ViewAngles = pc.Eye, ps.ViewAngles
	s.OnGround = ps.PMove.PmFlags&q2const.PMF_ON_GROUND != 0
	s.Ducked = ps.PMove.PmFlags&q2const.PMF_DUCKED != 0
	s.Mins, s.Maxs = Vec3{-16, -16, -24}, Vec3{16, 16, 32}
	if s.Ducked {
		s.Maxs[2] = 4
	}
	s.PmType = ps.PMove.PmType
	s.Health = int(ps.Stats[q2const.STAT_HEALTH])
	s.Armor = int(ps.Stats[q2const.STAT_ARMOR])
	s.Ammo = int(ps.Stats[q2const.STAT_AMMO])
	s.Layouts = int(ps.Stats[q2const.STAT_LAYOUTS])
	s.Fov = ps.Fov
	s.Weapon = ""
	if c := w.classes.ByModel(pc.Classifier().ModelPath(ps.GunIndex)); c != nil && c.Kind == perception.KindViewModel {
		s.Weapon = c.Pickup
	}
	s.Dead = ps.PMove.PmType == q2const.PM_DEAD || ps.PMove.PmType == q2const.PM_GIB || s.Health <= 0
	s.Intermission = ps.PMove.PmType == q2const.PM_FREEZE && s.Layouts&1 != 0
	var contents int32
	s.Waterlevel, contents = waterlevel(pc.Vision(), s.Origin, s.Ducked)
	s.InLava = contents&q2const.CONTENTS_LAVA != 0
	s.InSlime = contents&q2const.CONTENTS_SLIME != 0
	s.HelpBlink = imageIs(pc, int(ps.Stats[q2const.STAT_HELPICON]), "i_help")
	s.Timer = int(ps.Stats[q2const.STAT_TIMER])
	s.TimerIcon = imageName(pc, int(ps.Stats[q2const.STAT_TIMER_ICON]))
	if pc.OwnFlashes > 0 {
		s.LastFired = w.now
	}

	// a new pickup message: something was taken
	if ps2 := ps.Stats[q2const.STAT_PICKUP_STRING]; ps2 != st.pickupStat {
		st.pickupStat = ps2
		if ps2 != 0 {
			s.Pickup = configString(pc, int(ps2))
			s.PickupAt = w.now
			w.pickedUp(s.Pickup)
			w.refresh.inventoryDirty = true
		}
	}

	if !w.mem.Entry.Set && w.mem.Key.Map != "" && !s.Dead {
		w.mem.Entry = EntryState{Set: true, Origin: s.Origin, Health: s.Health, Armor: s.Armor, Weapon: s.Weapon}
	}
	if s.Dead && !st.wasDead {
		w.mem.addDeath(s.Origin)
	}
	st.wasDead = s.Dead

	predPitch, predRoll := st.kick.predict(ps, s.Waterlevel, pc.ServerFrame)
	flashes := ps.Stats[q2const.STAT_FLASHES]
	if st.prevStats && flashes != 0 {
		st.damage(w, pc, predPitch, predRoll)
	}
	st.prevHealth, st.prevArmor, st.prevStats = s.Health, s.Armor, true
}

// damage records the damage event of a frame whose STAT_FLASHES is set.
func (st *selfTracker) damage(w *World, pc *perception.Percept, predPitch, predRoll float32) {
	ps := &pc.PS
	s := &w.b.Self
	ev := DamageEvent{At: w.now, Frame: pc.ServerFrame, Health: max(st.prevHealth-s.Health, 0), Armor: max(st.prevArmor-s.Armor, 0), Cause: "hit"}
	s.LastDamage = w.now
	w.refresh.inventoryDirty = w.refresh.inventoryDirty || ev.Armor > 0
	switch {
	case pc.HasOwn && (pc.Own.Event == q2const.EV_FALL || pc.Own.Event == q2const.EV_FALLFAR):
		ev.Cause = "fall"
	case s.InLava:
		ev.Cause = "lava"
	case s.InSlime:
		ev.Cause = "slime"
	}

	// the damage kick: kick_angles minus what the bot predicts itself
	// (C: game/p_view.c P_DamageFeedback v_dmg_roll = kick*side*0.3 with
	// side = dot(dir, right), v_dmg_pitch = kick*-dot(dir, forward)*0.3)
	resP := ps.KickAngles[q2const.PITCH] - predPitch
	resR := ps.KickAngles[q2const.ROLL] - predRoll
	oldP, oldR := st.hit.at(pc.ServerFrame)
	fired := s.LastFired > 0 && w.now-s.LastFired <= firedQuiet
	newKick := math.Hypot(float64(resP-oldP), float64(resR-oldR)) > minKick &&
		math.Hypot(float64(resP), float64(resR)) > minKick
	if ev.Cause == "hit" && !fired && !s.Dead && newKick {
		// the new kick decays from here (a later knockback-free hit must
		// not take it for its own), whether or not it gives a bearing
		st.hit = lastKick{ok: true, frame: pc.ServerFrame, pitch: resP, roll: resR}
		ev.Bearing, ev.BearingKnown = kickBearing(ps.ViewAngles, resP, resR)
		if ev.BearingKnown {
			ev.Relative = angleDiff(ev.Bearing, ps.ViewAngles[q2const.YAW])
		}
	} else if ev.Cause == "hit" && s.Waterlevel == 3 && !newKick {
		ev.Cause = "drown"
	}
	ev.Source = w.attribute(&ev)
	w.b.Damage = append(w.b.Damage, ev)
}

// kickBearing recovers the world yaw a hit came from out of its damage kick
// (pitch, roll) seen at view angles view. P_DamageFeedback dots the
// direction with the pitched forward vector: for a horizontal direction
// that is cos(pitch) times its level forward part, while the right vector
// is level at roll 0. The direction runs from the origin to the impact
// point, and assuming it horizontal is the usual case; looking further up
// or down than maxBearingPitch the forward part is too small to use.
func kickBearing(view Vec3, pitch, roll float32) (bearing float32, ok bool) {
	p := float64(angleDiff(view[q2const.PITCH], 0))
	if math.Abs(p) > maxBearingPitch {
		return 0, false
	}
	var fwd, right Vec3
	shared.AngleVectors(Vec3{0, view[q2const.YAW], 0}, &fwd, &right, nil)
	f := -pitch / float32(math.Cos(p*math.Pi/180)) // dot(dir, level forward) up to the kick scale
	r := roll                                      // dot(dir, right)
	dir := Vec3{f*fwd[0] + r*right[0], f*fwd[1] + r*right[1], 0}
	if dir[0] == 0 && dir[1] == 0 {
		return 0, false
	}
	return float32(math.Atan2(float64(dir[1]), float64(dir[0])) * 180 / math.Pi), true
}

// attribute names the track a hit most likely came from: a recent
// attacker near the recovered bearing, or the only recent attacker.
func (w *World) attribute(ev *DamageEvent) string {
	if ev.Cause != "hit" {
		return ""
	}
	best, bestDiff, recent, only := "", float32(fuseAngle), 0, ""
	eye := w.b.Self.Eye
	for _, a := range w.actors {
		if a.Life != LifeAlive || !a.PosKnown {
			continue
		}
		if !(w.now-a.LastAttack <= fuseMemory || w.now-a.LastHeard <= fuseMemory || a.Visible && a.Awareness == Attacking) {
			continue
		}
		recent++
		only = a.ID
		if !ev.BearingKnown {
			continue
		}
		yaw := float32(math.Atan2(float64(a.Pos[1]-eye[1]), float64(a.Pos[0]-eye[0])) * 180 / math.Pi)
		if d := float32(math.Abs(float64(angleDiff(yaw, ev.Bearing)))); d <= bestDiff {
			best, bestDiff = a.ID, d
		}
	}
	if best != "" {
		return best
	}
	if recent == 1 && !ev.BearingKnown {
		return only
	}
	return ""
}

// angleDiff returns a-b normalized to (-180, 180].
func angleDiff(a, b float32) float32 {
	d := math.Mod(float64(a-b), 360)
	if d > 180 {
		d -= 360
	} else if d <= -180 {
		d += 360
	}
	return float32(d)
}

func configString(pc *perception.Percept, i int) string {
	if pc.CS == nil || i < 0 || i >= len(pc.CS) {
		return ""
	}
	return pc.CS[i]
}

func imageName(pc *perception.Percept, index int) string {
	if index <= 0 || index >= q2const.MAX_IMAGES {
		return ""
	}
	return configString(pc, q2const.CS_IMAGES+index)
}

func imageIs(pc *perception.Percept, index int, name string) bool {
	return index > 0 && imageName(pc, index) == name
}
