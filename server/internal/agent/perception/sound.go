package perception

import (
	"strconv"
	"strings"
)

// SoundKind is what a sound tells a listener.
type SoundKind uint8

// Sound kinds.
const (
	SoundOther     SoundKind = iota
	SoundDeath               // a monster's or player's death cry
	SoundGib                 // misc/udeath.wav: something was gibbed
	SoundSight               // a monster noticed someone
	SoundSearch              // a monster looks for someone
	SoundIdle                // idle noise
	SoundPain                // pain cry
	SoundAttack              // attack or melee swing
	SoundDoor                // door start / move / stop
	SoundPlat                // platform start / move / stop
	SoundButton              // switch pressed
	SoundWeapon              // weapon fire, reload, projectile flight or impact
	SoundExplosion           // explosion
	SoundPickup              // item taken
	SoundPlayer              // player noise (pain, jump, fall, breath)
	SoundAmbient             // world ambience
	SoundStep                // a monster's footstep: it moves, nothing more
)

// String returns the kind's name.
func (k SoundKind) String() string {
	switch k {
	case SoundOther:
		return "other"
	case SoundDeath:
		return "death"
	case SoundGib:
		return "gib"
	case SoundSight:
		return "sight"
	case SoundSearch:
		return "search"
	case SoundIdle:
		return "idle"
	case SoundPain:
		return "pain"
	case SoundAttack:
		return "attack"
	case SoundDoor:
		return "door"
	case SoundPlat:
		return "plat"
	case SoundButton:
		return "button"
	case SoundWeapon:
		return "weapon"
	case SoundExplosion:
		return "explosion"
	case SoundPickup:
		return "pickup"
	case SoundPlayer:
		return "player"
	case SoundAmbient:
		return "ambient"
	case SoundStep:
		return "step"
	}
	return "sound" + strconv.Itoa(int(k))
}

// soundFamily maps a monster sound directory to the monster family whose
// voice it is (C: game/m_*.c gi.soundindex paths).
func soundFamily(dir string) string {
	switch dir {
	case "soldier", "infantry", "gunner", "berserk", "flyer", "hover", "parasite",
		"medic", "tank", "mutant", "brain", "gladiator", "chick", "flipper", "insane":
		return dir
	case "floater":
		return "floater"
	case "bosstank":
		return "supertank"
	case "bosshovr":
		return "boss2"
	case "boss3":
		return "jorg"
	case "makron":
		return "makron"
	}
	return ""
}

// ClassifySound returns what the sound at a CS_SOUNDS path means and, for a
// monster voice, the monster family ("soldier" for every soldier class,
// "tank" for both tanks, "" otherwise). It reads the name the way a player
// recognizes a sound: by what it sounds like.
func ClassifySound(path string) (SoundKind, string) {
	p := strings.ToLower(path)
	if strings.HasPrefix(p, "*") {
		return SoundPlayer, ""
	}
	dir, file := "", p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		dir, file = p[:i], p[i+1:]
	}
	if p == "misc/udeath.wav" {
		return SoundGib, ""
	}
	switch {
	case strings.HasPrefix(dir, "doors"):
		return SoundDoor, ""
	case strings.HasPrefix(dir, "plats"):
		return SoundPlat, ""
	case strings.HasPrefix(dir, "switches"):
		return SoundButton, ""
	case dir == "items" || strings.Contains(file, "pkup"):
		return SoundPickup, ""
	case strings.HasPrefix(dir, "player"):
		return SoundPlayer, ""
	case strings.Contains(file, "explod") || strings.Contains(file, "rocklx") || strings.Contains(file, "grenlx") ||
		strings.Contains(file, "xpld") || strings.Contains(file, "bfg__x"):
		return SoundExplosion, ""
	case dir == "weapons":
		return SoundWeapon, ""
	case dir == "world":
		return SoundAmbient, ""
	}
	fam := soundFamily(dir)
	if fam == "" {
		return SoundOther, ""
	}
	switch file {
	case "inflies1.wav": // flies buzzing over a corpse (C: game/g_monster.c M_FliesOn)
		return SoundAmbient, ""
	case "infatck3.wav": // a soldier or enforcer cocking its gun (C: m_soldier.c soldier_cock)
		return SoundIdle, fam
	}
	switch p {
	case "boss3/d_hit.wav": // jorg's body hits the floor (C: m_boss31.c jorg_death_hit)
		return SoundDeath, fam
	case "makron/bhit.wav": // in makron's death frames (C: m_boss32.c makron_hit)
		return SoundDeath, fam
	case "boss3/w_loop.wav": // jorg's guns winding up (C: m_boss31.c jorg_attack)
		return SoundAttack, fam
	}
	// footsteps: tank_footstep, mutant_step, jorg_step_*, makron_step_*
	// (C: game/m_tank.c, m_mutant.c, m_boss31.c, m_boss32.c)
	if strings.HasPrefix(file, "step") {
		return SoundStep, fam
	}
	switch {
	case strings.Contains(file, "deth") || strings.Contains(file, "death") || strings.Contains(file, "dth") ||
		strings.Contains(file, "die"):
		return SoundDeath, fam
	case strings.Contains(file, "sght") || strings.Contains(file, "sight"):
		return SoundSight, fam
	case strings.Contains(file, "srch") || strings.Contains(file, "search") || strings.Contains(file, "unqv"):
		return SoundSearch, fam
	case strings.Contains(file, "pain"):
		return SoundPain, fam
	case strings.Contains(file, "idle") || strings.Contains(file, "lens"):
		return SoundIdle, fam
	case strings.Contains(file, "atck") || strings.Contains(file, "attack") || strings.Contains(file, "melee") ||
		strings.Contains(file, "swing") || strings.Contains(file, "hit") || strings.Contains(file, "punch") ||
		strings.Contains(file, "slash") || strings.Contains(file, "strike") || strings.Contains(file, "thud") ||
		strings.Contains(file, "slam") || strings.Contains(file, "fire") || strings.Contains(file, "railgun") ||
		strings.Contains(file, "rail_up"):
		return SoundAttack, fam
	}
	return SoundOther, fam
}
