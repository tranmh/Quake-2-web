package worldmodel

import (
	"math"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/qcommon/shared"
)

// Projectile danger parameters.
const (
	// hitRadius is the bot's box half-size the closest approach is
	// compared with.
	hitRadius = 24
	// splashRadius is the radius damage of rockets and grenades
	// (C: game/g_weapon.c fire_rocket/fire_grenade damage_radius, rounded).
	splashRadius = 120
	// dangerHorizon: only approaches within this many seconds matter.
	dangerHorizon = 1.5
)

// approach computes the closest approach of projectile p to the box center
// c of a bot moving with velocity selfVel (view right vector right) and
// how to dodge it. Grenades are treated as moving in a straight line over
// the short horizon.
func approach(p *Projectile, c, selfVel, right Vec3, cls *perception.Class) {
	r := shared.VectorSubtract(p.Pos, c)
	v := shared.VectorSubtract(p.Vel, selfVel)
	vv := float64(shared.DotProduct(v, v))
	p.DodgeDir, p.DodgeSide, p.Danger = Vec3{}, 0, false
	if vv < 1 {
		p.TCA, p.Miss = -1, shared.VectorLength(r)
		return
	}
	t := -float64(shared.DotProduct(r, v)) / vv
	if t <= 0 {
		p.TCA, p.Miss = float32(t), shared.VectorLength(r)
		return
	}
	d := shared.VectorMA(r, float32(t), v) // projectile relative to the bot at closest approach
	p.TCA, p.Miss = float32(t), shared.VectorLength(d)
	radius := float32(hitRadius)
	if cls != nil && cls.Weapon.Splash() {
		radius += splashRadius
	}
	p.Danger = t <= dangerHorizon && p.Miss <= radius && !p.Own

	// move away from where it passes: opposite to the horizontal part of
	// d perpendicular to the flight direction (or sideways to a dead-on
	// shot)
	vh := Vec3{v[0], v[1], 0}
	if shared.VectorNormalize(&vh) == 0 {
		vh = Vec3{1, 0, 0} // falling straight down: any side
	}
	perp := Vec3{-vh[1], vh[0], 0} // left of the flight direction
	side := shared.DotProduct(Vec3{d[0], d[1], 0}, perp)
	var dir Vec3
	switch {
	case math.Abs(float64(side)) < 1:
		// dead on: the perpendicular closer to the view's right
		dir = perp
		if shared.DotProduct(perp, right) < 0 {
			dir = shared.VectorScale(perp, -1)
		}
	case side > 0: // it passes on the perp side of the bot
		dir = shared.VectorScale(perp, -1)
	default:
		dir = perp
	}
	p.DodgeDir = dir
	switch s := shared.DotProduct(dir, right); {
	case s > 0.1:
		p.DodgeSide = 1
	case s < -0.1:
		p.DodgeSide = -1
	}
}
