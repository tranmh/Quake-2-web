// Package scripted is the agent's rule-based decision backend: a
// deterministic policy that answers the decide lanes' questions from the
// lane state alone. It is the baseline, the arbiter's fallback and the CI
// backend. Because it reads only the lane state (the same struct the wire
// carries), jevtest's fake server can run the very same rules on the JSON
// it receives.
//
// The policy is stateless and seeded: its only time dependence is the
// strafe rhythm, a pure function of the seed and the snapshot time, so it
// is safe for concurrent use and repeats exactly.
package scripted

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/perception"
)

// Name is the backend's name and the model id it reports.
const Name = "scripted"

// Config configures the policy.
type Config struct {
	// Seed drives the strafe rhythm.
	Seed uint64
	// Classes is the class table for monster priors (nil: the default).
	Classes *perception.ClassTable
	// Stickiness is how much better (relative) another target must score
	// to replace the current one (0.15).
	Stickiness float64
}

// Policy is the rule policy.
type Policy struct {
	seed    uint64
	classes *perception.ClassTable
	stick   float64
}

// NewPolicy returns the policy.
func NewPolicy(cfg Config) *Policy {
	p := &Policy{seed: cfg.Seed, classes: cfg.Classes, stick: cfg.Stickiness}
	if p.classes == nil {
		p.classes = perception.NewClassTable()
	}
	if p.stick <= 0 {
		p.stick = 0.15
	}
	return p
}

// Decision is everything the policy decides about a state.
type Decision struct {
	Target     string // enemy id ("" none)
	FirePolicy decide.FirePolicy
	Movement   decide.Movement
	Mode       decide.Mode
	Weapon     decide.WeaponKey // "" keep
	Pickup     string           // item id ("" none)
	Danger     int              // level 0..4
}

// Rule thresholds.
const (
	fightRange   = 1200 // units: a visible awake enemy this close means fight
	meleeKeepOff = 250  // units: stay this far from melee-only monsters
	retreatHP    = 25
	pickupHP     = 40
	healthNear   = 600 // path units
	weaponNear   = 800
	ammoNear     = 600
	strafeWindow = 1800 // ms: one left and one right segment
	strafeMin    = 600  // ms: shortest segment
)

// Decide applies the rules to st at now (the snapshot time, ms).
func (p *Policy) Decide(st *decide.State, now int64) Decision {
	d := Decision{Danger: p.danger(st)}
	t := p.target(st)
	if t != nil {
		d.Target = t.ID
	}
	d.FirePolicy = p.firePolicy(st, t)
	d.Movement = p.movement(st, t, now)
	d.Weapon = p.weapon(st, t)
	d.Pickup = p.pickup(st)
	d.Mode = p.mode(st, d.Danger)
	return d
}

func threatLevel(s string) float64 {
	switch s {
	case "high":
		return 2
	case "med":
		return 1
	}
	return 0
}

// targetScore ranks enemies by threat over distance.
func targetScore(e *decide.Enemy) float64 {
	s := (threatLevel(e.Threat) + 1) / math.Max(float64(e.Units), 64)
	switch {
	case e.Visible && e.Shootable:
	case e.Visible:
		s *= 0.8
	default:
		s *= 0.4
	}
	switch e.State {
	case "attacking":
	case "alert":
		s *= 0.8
	default:
		s *= 0.5
	}
	return s
}

// target picks the highest score, keeping the current target unless
// another beats it by the stickiness.
func (p *Policy) target(st *decide.State) *decide.Enemy {
	var best, cur *decide.Enemy
	bs := -1.0
	for i := range st.Enemies {
		e := &st.Enemies[i]
		if s := targetScore(e); s > bs {
			best, bs = e, s
		}
		if e.Current {
			cur = e
		}
	}
	if cur != nil && best != cur && bs <= targetScore(cur)*(1+p.stick) {
		return cur
	}
	return best
}

// weaponRange is how far a weapon is worth firing (units).
func weaponRange(w string) int {
	switch decide.WeaponKey(w) {
	case decide.WeaponShotgun:
		return 500
	case decide.WeaponSuperShotgun:
		return 300
	case decide.WeaponMachinegun, decide.WeaponChaingun, decide.WeaponHyperBlaster:
		return 900
	case decide.WeaponGrenadeLauncher:
		return 600
	case decide.WeaponRocketLauncher:
		return 1200
	case decide.WeaponRailgun:
		return 2500
	}
	return 1000 // blaster, bfg
}

func (p *Policy) firePolicy(st *decide.State, t *decide.Enemy) decide.FirePolicy {
	if t == nil || !t.Visible || st.Me.Ammo == "none" {
		return decide.FireHold
	}
	switch {
	case t.Dist == "close" && t.Threat == "high" && t.Shootable:
		return decide.FireSuppress
	case t.Shootable && t.Units <= weaponRange(st.Me.Weapon):
		return decide.FireWhenAligned
	}
	return decide.FireHold
}

// preferredRange is the distance band a weapon fights best at, and the
// distance under which the bot backs off.
func preferredRange(w string) (lo, hi, backoff int) {
	switch decide.WeaponKey(w) {
	case decide.WeaponShotgun:
		return 150, 300, 150
	case decide.WeaponSuperShotgun:
		return 0, 200, 0
	case decide.WeaponMachinegun, decide.WeaponChaingun, decide.WeaponHyperBlaster, decide.WeaponGrenadeLauncher:
		return 250, 600, 250
	case decide.WeaponRocketLauncher:
		return 300, 800, 200
	case decide.WeaponRailgun:
		return 400, 3000, 400
	case decide.WeaponBFG:
		return 400, 1000, 400
	}
	return 300, 500, 300 // blaster
}

// meleeOnly reports a monster that only fights in melee (or with a
// drain) by its class prior.
func (p *Policy) meleeOnly(class string) bool {
	c := p.classes.ByName(class)
	return c != nil && (c.Weapon == perception.WeaponMelee || c.Weapon == perception.WeaponDrain)
}

// StrafeLeft is the strafe rhythm: alternating left and right segments of
// 0.6 to 1.2 s, a pure function of the seed and the time.
func StrafeLeft(seed uint64, now int64) bool {
	w := now / strafeWindow
	if now < 0 && now%strafeWindow != 0 {
		w--
	}
	off := now - w*strafeWindow
	split := strafeMin + int64(decide.Mix64(seed^decide.Mix64(uint64(w)))%uint64(strafeWindow-2*strafeMin+1))
	return off < split
}

func blocked(s string) bool { return s == "blocked" }

func (p *Policy) movement(st *decide.State, t *decide.Enemy, now int64) decide.Movement {
	sp := st.Space
	open := func(side decide.Movement) bool {
		if sp == nil {
			return true
		}
		switch side {
		case decide.MoveStrafeLeft:
			return !blocked(sp.Left)
		case decide.MoveStrafeRight:
			return !blocked(sp.Right)
		case decide.MoveAdvance:
			return !blocked(sp.Front)
		case decide.MoveRetreat:
			return !blocked(sp.Back)
		}
		return true
	}
	strafe := func(prefer decide.Movement) decide.Movement {
		other := decide.MoveStrafeRight
		if prefer == decide.MoveStrafeRight {
			other = decide.MoveStrafeLeft
		}
		switch {
		case open(prefer):
			return prefer
		case open(other):
			return other
		}
		return decide.MoveHold
	}
	dodge := func(eta ...string) (decide.Movement, bool) {
		for _, in := range st.Incoming {
			for _, e := range eta {
				if in.ETA == e && in.Dodge != "none" {
					side := decide.MoveStrafeLeft
					if in.Dodge == "right" {
						side = decide.MoveStrafeRight
					}
					return strafe(side), true
				}
			}
		}
		return "", false
	}
	if m, ok := dodge("imminent"); ok {
		return m
	}
	if t == nil {
		if m, ok := dodge("soon"); ok {
			return m
		}
		return decide.MoveHold
	}
	rhythm := decide.MoveStrafeRight
	if StrafeLeft(p.seed, now) {
		rhythm = decide.MoveStrafeLeft
	}
	var want decide.Movement
	_, hi, backoff := preferredRange(st.Me.Weapon)
	switch {
	case p.meleeOnly(t.Class) && t.Units < meleeKeepOff:
		want = decide.MoveRetreat
	case t.Units < backoff:
		want = decide.MoveRetreat
	case t.Units > hi:
		want = decide.MoveAdvance
	default:
		return strafe(rhythm)
	}
	if !open(want) {
		return strafe(rhythm)
	}
	return want
}

// weaponOrder ranks the weapons for a distance band (never grenades).
func weaponOrder(band string) []decide.WeaponKey {
	switch band {
	case "close":
		return []decide.WeaponKey{decide.WeaponSuperShotgun, decide.WeaponShotgun, decide.WeaponChaingun, decide.WeaponHyperBlaster,
			decide.WeaponMachinegun, decide.WeaponBlaster, decide.WeaponRailgun, decide.WeaponRocketLauncher}
	case "far":
		return []decide.WeaponKey{decide.WeaponRailgun, decide.WeaponRocketLauncher, decide.WeaponChaingun, decide.WeaponHyperBlaster,
			decide.WeaponMachinegun, decide.WeaponBFG, decide.WeaponBlaster, decide.WeaponShotgun, decide.WeaponSuperShotgun}
	}
	return []decide.WeaponKey{decide.WeaponChaingun, decide.WeaponRocketLauncher, decide.WeaponHyperBlaster, decide.WeaponBFG,
		decide.WeaponMachinegun, decide.WeaponSuperShotgun, decide.WeaponRailgun, decide.WeaponShotgun, decide.WeaponBlaster}
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func (p *Policy) weapon(st *decide.State, t *decide.Enemy) decide.WeaponKey {
	band := ""
	switch {
	case t != nil:
		band = t.Dist
	case st.Me.Ammo == "none":
		band = "mid"
	default:
		return decide.WeaponKeep
	}
	for _, k := range weaponOrder(band) {
		if has(st.Me.Weapons, string(k)) {
			if string(k) == st.Me.Weapon {
				return decide.WeaponKeep
			}
			return k
		}
	}
	return decide.WeaponKeep
}

// pickup is the nearest item (the projector lists only useful ones).
func (p *Policy) pickup(st *decide.State) string {
	best, bp := "", math.MaxInt
	for _, it := range st.Items {
		if it.Path < bp {
			best, bp = it.ID, it.Path
		}
	}
	return best
}

// pickupNeeded: low health with health near, an unowned weapon near, or
// low ammo with ammo for an owned weapon near.
func (p *Policy) pickupNeeded(st *decide.State) bool {
	me := &st.Me
	for _, it := range st.Items {
		switch {
		case strings.HasPrefix(it.Gives, "health+") && me.Health < pickupHP && it.Path <= healthNear:
			return true
		case strings.HasPrefix(it.Gives, "weapon:") && !has(me.Weapons, strings.TrimPrefix(it.Gives, "weapon:")) && it.Path <= weaponNear:
			return true
		case (me.Ammo == "low" || me.Ammo == "none") && it.Path <= ammoNear:
			ammo := strings.ToLower(decide.WeaponKey(me.Weapon).AmmoName())
			if ammo != "" && strings.HasPrefix(it.Gives, ammo+"+") {
				return true
			}
		}
	}
	return false
}

func (p *Policy) mode(st *decide.State, danger int) decide.Mode {
	if st.Me.Health < retreatHP && danger >= decide.DangerHigh {
		return decide.ModeRetreat
	}
	for i := range st.Enemies {
		e := &st.Enemies[i]
		if e.Visible && e.State != "idle" && e.Units <= fightRange {
			return decide.ModeFight
		}
	}
	switch {
	case p.pickupNeeded(st):
		return decide.ModePickup
	case st.Objective != nil:
		return decide.ModeObjective
	}
	return decide.ModeExplore
}

// danger sums the visible threats, incoming fire and the bot's condition
// into a level 0..4.
func (p *Policy) danger(st *decide.State) int {
	d := 0.0
	for i := range st.Enemies {
		e := &st.Enemies[i]
		if e.State == "idle" && !e.Visible {
			continue
		}
		w := []float64{0.4, 0.8, 1.3}[int(threatLevel(e.Threat))]
		switch e.State {
		case "alert":
			w *= 0.7
		case "idle":
			w *= 0.4
		}
		switch e.Dist {
		case "close":
			w *= 1.3
		case "far":
			w *= 0.6
		}
		if !e.Visible {
			w *= 0.5
		}
		d += w
	}
	for _, in := range st.Incoming {
		switch in.ETA {
		case "imminent":
			d += 1
		case "soon":
			d += 0.4
		}
	}
	switch st.Me.HP {
	case "critical":
		d += 1.5
	case "low":
		d += 0.75
	}
	switch {
	case st.Me.DamageLast1s >= 20:
		d += 0.75
	case st.Me.DamageLast1s > 0:
		d += 0.25
	}
	return int(math.Round(math.Max(0, math.Min(decide.DangerCritical, d))))
}

// optionOr returns key if q offers it, else fallback if offered, else the
// first option.
func optionOr(q *decide.Question, key, fallback string) string {
	switch {
	case q.Index(key) >= 0:
		return key
	case q.Index(fallback) >= 0:
		return fallback
	case len(q.Options) > 0:
		return q.Options[0].Key
	}
	return key
}

// Answers answers the questions it knows (one-hot, confidence 1) for st
// at now; unknown question ids are left unanswered.
func (p *Policy) Answers(st *decide.State, qs []decide.Question, now int64) map[string]decide.Answer {
	d := p.Decide(st, now)
	out := map[string]decide.Answer{}
	for i := range qs {
		q := &qs[i]
		var key string
		switch q.ID {
		case decide.QTarget:
			key = optionOr(q, orNone(d.Target), decide.OptNone)
		case decide.QFirePolicy:
			key = optionOr(q, string(d.FirePolicy), string(decide.FireHold))
		case decide.QMovement:
			key = optionOr(q, string(d.Movement), string(decide.MoveHold))
		case decide.QMode:
			key = optionOr(q, string(d.Mode), string(decide.ModeExplore))
		case decide.QWeapon:
			w := decide.OptKeep
			if d.Weapon != decide.WeaponKeep {
				w = string(d.Weapon)
			}
			key = optionOr(q, w, decide.OptKeep)
		case decide.QPickup:
			key = optionOr(q, orNone(d.Pickup), decide.OptNone)
		case decide.QDanger:
			if q.Type != decide.Score || len(q.Options) == 0 {
				continue
			}
			key = q.Options[min(d.Danger, len(q.Options)-1)].Key
		default:
			continue
		}
		if q.Index(key) < 0 {
			continue
		}
		out[q.ID] = decide.OneHot(q, key)
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return decide.OptNone
	}
	return s
}

// Backend is the scripted DecisionBackend.
type Backend struct{ p *Policy }

// New returns the scripted backend.
func New(cfg Config) *Backend { return &Backend{p: NewPolicy(cfg)} }

// Policy returns the backend's policy.
func (b *Backend) Policy() *Policy { return b.p }

// Name implements decide.DecisionBackend.
func (b *Backend) Name() string { return Name }

// Decide answers req from its lane state (req.View, or the decoded
// req.State) at req.SnapTime. The response's Raw is the documented wire
// format, so traces of scripted runs replay like model runs.
func (b *Backend) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	st := req.View
	if st == nil {
		st = &decide.State{}
		if err := json.Unmarshal(req.State, st); err != nil {
			return nil, fmt.Errorf("scripted: state: %w", err)
		}
	}
	ans := b.p.Answers(st, req.Questions, req.SnapTime)
	raw, err := decide.MarshalResponse(Name, req.Questions, ans, decide.Usage{})
	if err != nil {
		return nil, err
	}
	return &decide.Response{Seq: req.Seq, Answers: ans, Model: Name, Raw: raw}, nil
}
