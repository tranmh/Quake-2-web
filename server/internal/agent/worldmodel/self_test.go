package worldmodel

import (
	"math"
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// quantKick is the wire encoding of a kick angle: a char of angle*4,
// truncated towards zero (C: server/sv_ents.c SV_WritePlayerstateToClient).
func quantKick(a float64) float32 { return float32(int8(int32(a*4))) * 0.25 }

// damageKick returns P_DamageFeedback's view kick (pitch, roll) for a hit
// from world yaw theta (degrees) with kick strength k, seen by a player
// looking at yaw viewYaw (pitch 0).
// C: game/p_view.c:71 P_DamageFeedback
func damageKick(theta, viewYaw, k float64) (pitch, roll float32) {
	th, y := theta*math.Pi/180, viewYaw*math.Pi/180
	v := [2]float64{math.Cos(th), math.Sin(th)}
	fwd := [2]float64{math.Cos(y), math.Sin(y)}
	right := [2]float64{math.Sin(y), -math.Cos(y)}
	side := v[0]*right[0] + v[1]*right[1]
	front := v[0]*fwd[0] + v[1]*fwd[1]
	return quantKick(k * -front * 0.3), quantKick(k * side * 0.3)
}

func (s *sim) hit(health int16, pitch, roll float32) *Belief {
	s.ps.Stats[q2const.STAT_HEALTH] = health
	s.ps.Stats[q2const.STAT_FLASHES] = 1
	s.ps.KickAngles = Vec3{pitch, 0, roll}
	b := s.step()
	s.ps.KickAngles = Vec3{}
	return b
}

func TestDamageBearingFromKick(t *testing.T) {
	for _, viewYaw := range []float64{0, 90, 217} {
		for theta := -180.0; theta < 180; theta += 30 {
			for _, k := range []float64{10, 30, 50} {
				s := newSim(t)
				s.ps.ViewAngles[q2const.YAW] = float32(viewYaw)
				s.step()
				p, r := damageKick(theta, viewYaw, k)
				b := s.hit(90, p, r)
				if len(b.Damage) != 1 {
					t.Fatalf("damage events %+v", b.Damage)
				}
				d := b.Damage[0]
				tol := 3.0
				if k < 20 {
					tol = 6
				}
				if !d.BearingKnown || math.Abs(float64(angleDiff(d.Bearing, float32(theta)))) > tol {
					t.Errorf("view %v hit from %v kick %v: bearing %v known %v", viewYaw, theta, k, d.Bearing, d.BearingKnown)
					continue
				}
				if math.Abs(float64(angleDiff(d.Relative, angleDiff(float32(theta), float32(viewYaw))))) > tol {
					t.Errorf("relative %v for hit from %v at view %v", d.Relative, theta, viewYaw)
				}
				if d.Health != 10 || d.Cause != "hit" || d.Frame != s.frame {
					t.Errorf("event %+v", d)
				}
			}
		}
	}
}

func TestDamageBearingUnknown(t *testing.T) {
	// no knockback: no kick at all
	s := newSim(t)
	s.step()
	if d := s.hit(95, 0, 0).Damage[0]; d.BearingKnown || d.Cause != "hit" || d.Health != 5 {
		t.Fatalf("knockback-free hit %+v", d)
	}
	// the bot just fired: weapon kick pollutes the view kick
	s = newSim(t)
	s.step()
	s.ev.MuzzleFlashes = []fakeclient.MuzzleFlash{{Ent: 1, Weapon: q2const.MZ_BLASTER}}
	p, r := damageKick(90, 0, 30)
	if d := s.hit(90, p, r).Damage[0]; d.BearingKnown {
		t.Fatalf("bearing while firing %+v", d)
	}
	// a second, knockback-free hit while the first kick decays
	s = newSim(t)
	s.step()
	p, r = damageKick(90, 0, 30)
	if d := s.hit(90, p, r).Damage[0]; !d.BearingKnown {
		t.Fatal("first hit")
	}
	b := s.hit(85, quantKick(float64(p)*0.8), quantKick(float64(r)*0.8)) // ratio 0.8 one frame later
	if d := b.Damage[1]; d.BearingKnown {
		t.Fatalf("decaying kick taken for a new hit: %+v", d)
	}
	// falling damage: the own entity carries EV_FALL
	s = newSim(t)
	s.step()
	s.ownEvent = q2const.EV_FALL
	if d := s.hit(90, 10, 0).Damage[0]; d.Cause != "fall" || d.BearingKnown {
		t.Fatalf("fall %+v", d)
	}
}

func TestDamageAttribution(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{300, 300, 24}), soldierAt(21, Vec3{300, -300, 24})}
	s.ev.MuzzleFlashes = []fakeclient.MuzzleFlash{
		{Ent: 20, Weapon: q2const.MZ2_SOLDIER_SHOTGUN_1, Monster: true},
		{Ent: 21, Weapon: q2const.MZ2_SOLDIER_SHOTGUN_1, Monster: true},
	}
	s.step()
	p, r := damageKick(-45, 0, 30) // from 21's side
	b := s.hit(80, p, r)
	if d := b.Damage[0]; d.Source != s.track("e2").ID || !d.BearingKnown {
		t.Fatalf("attributed to %q (%+v)", d.Source, d)
	}
	if !b.Self.InCombat {
		t.Fatal("damage is combat")
	}
}
