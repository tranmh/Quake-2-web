package decide

import (
	"fmt"
	"strings"
)

// Preamble starts every question's instructions: the questions are
// answered in isolation, so each carries the context and the legend.
const Preamble = `You play Quake II single-player as "me". Bearings: degrees from my view, + left, - right, 0 ahead, 180 behind; units: map units.`

func instructions(s string) string { return Preamble + " " + s }

// Questions returns the lane's questions about st: fast asks target (when
// there are enemies), fire_policy and movement; slow asks mode, weapon
// (when another weapon is usable), pickup (when an item is useful) and
// danger. Dynamic options come from st, in its order.
func Questions(lane Lane, st *State) []Question {
	if lane == LaneSlow {
		qs := []Question{modeQuestion()}
		if q, ok := weaponQuestion(st); ok {
			qs = append(qs, q)
		}
		if q, ok := pickupQuestion(st); ok {
			qs = append(qs, q)
		}
		return append(qs, dangerQuestion())
	}
	var qs []Question
	if q, ok := targetQuestion(st); ok {
		qs = append(qs, q)
	}
	return append(qs, firePolicyQuestion(), movementQuestion())
}

func targetQuestion(st *State) (Question, bool) {
	if len(st.Enemies) == 0 {
		return Question{}, false
	}
	q := Question{ID: QTarget, Type: Choice,
		Instructions: instructions("Which enemy should I shoot now? Prefer visible, shootable, dangerous and close ones; keep my current target unless another is clearly better.")}
	for _, e := range st.Enemies {
		var b strings.Builder
		fmt.Fprintf(&b, "%s, %s %du, bearing %d, %s", e.Class, e.Dist, e.Units, e.Bearing, e.State)
		if e.Wounded {
			b.WriteString(", wounded")
		}
		if !e.Visible {
			b.WriteString(", not in view")
		}
		if e.Current {
			b.WriteString(", current target")
		}
		q.Options = append(q.Options, Option{Key: e.ID, Desc: b.String()})
	}
	q.Options = append(q.Options, Option{Key: OptNone, Desc: "no enemy is worth shooting"})
	return q, true
}

func firePolicyQuestion() Question {
	return Question{ID: QFirePolicy, Type: Choice,
		Instructions: instructions("When should I fire at my target? This is a standing rule: the aim, the view and the line of fire are checked again for every shot."),
		Options: []Option{
			{string(FireHold), "do not fire: the target is out of my weapon's range, or not worth the ammo"},
			{string(FireWhenAligned), "fire whenever it is in view, the shot is clear and the crosshair is on it"},
			{string(FireSuppress), "fire continuously, even slightly off target or where it was last seen: a close, dangerous enemy"},
		}}
}

func movementQuestion() Question {
	return Question{ID: QMovement, Type: Choice,
		Instructions: instructions("How should I move relative to my target or the incoming fire? Keep my weapon's best range and dodge projectiles. This is a standing rule, applied to where the target is at every moment."),
		Options: []Option{
			{string(MoveAdvance), "move toward the target"},
			{string(MoveRetreat), "back away from the target"},
			{string(MoveStrafe), "sidestep around it, alternating sides (the side is picked for me: away from walls and incoming projectiles)"},
			{string(MoveHold), "stay where I am"},
		}}
}

func modeQuestion() Question {
	return Question{ID: QMode, Type: Choice,
		Instructions: instructions("What should I focus on now?"),
		Options: []Option{
			{string(ModeFight), "fight the enemies"},
			{string(ModeObjective), "continue toward the level objective"},
			{string(ModePickup), "go get a useful item"},
			{string(ModeRetreat), "back off from danger to recover"},
			{string(ModeExplore), "look around for enemies or the way on"},
		}}
}

// weaponDesc is what a player knows about each weapon.
func weaponDesc(k WeaponKey) string {
	switch k {
	case WeaponBlaster:
		return "weak bolts, unlimited ammo"
	case WeaponShotgun:
		return "pellet spread, best within 300 units"
	case WeaponSuperShotgun:
		return "strong pellet spread, best within 200 units"
	case WeaponMachinegun:
		return "hitscan bullets, medium range"
	case WeaponChaingun:
		return "fast hitscan bullets, medium range"
	case WeaponGrenadeLauncher:
		return "bouncing splash grenades, risky up close"
	case WeaponRocketLauncher:
		return "strong splash rockets, stay 200 units away"
	case WeaponHyperBlaster:
		return "fast bolts, medium range"
	case WeaponRailgun:
		return "precise slug, best at long range"
	case WeaponBFG:
		return "huge splash, slow to fire"
	}
	return string(k)
}

func weaponQuestion(st *State) (Question, bool) {
	q := Question{ID: QWeapon, Type: Choice,
		Instructions: instructions("Which weapon should I hold for the current fight?"),
		Options:      []Option{{OptKeep, "keep the current weapon (" + st.Me.Weapon + ")"}}}
	for _, w := range st.Me.Weapons {
		if w == st.Me.Weapon {
			continue
		}
		q.Options = append(q.Options, Option{Key: w, Desc: weaponDesc(WeaponKey(w))})
	}
	return q, len(q.Options) > 1
}

func pickupQuestion(st *State) (Question, bool) {
	if len(st.Items) == 0 {
		return Question{}, false
	}
	q := Question{ID: QPickup, Type: Choice,
		Instructions: instructions("Which item should I get next, if any?")}
	for _, it := range st.Items {
		q.Options = append(q.Options, Option{Key: it.ID, Desc: fmt.Sprintf("%s (%s), path %du, bearing %d", it.Class, it.Gives, it.Path, it.Bearing)})
	}
	q.Options = append(q.Options, Option{Key: OptNone, Desc: "no item is worth the detour"})
	return q, true
}

func dangerQuestion() Question {
	return Question{ID: QDanger, Type: Score,
		Instructions: instructions("How dangerous is my situation right now?"),
		Options: []Option{
			{levelKey(DangerSafe), "safe: no threat"},
			{levelKey(DangerLow), "low: weak or distant threats"},
			{levelKey(DangerModerate), "moderate: under fire but healthy"},
			{levelKey(DangerHigh), "high: strong threats, or low health"},
			{levelKey(DangerCritical), "critical: about to die"},
		}}
}
