package game

// Named function registry (replaces the function pointer tables of
// game/g_save.c, ADR-0004).
//
// Every C function that is stored in a function pointer (edict think/touch/
// use/..., monsterinfo callbacks, moveinfo.endfunc, mframe_t callbacks,
// gitem_t callbacks) is represented by a handle: a pointer to an immutable
// Fn, registered under the exact C function name. Handles are comparable
// with ==, nil is C NULL, and saves store the name.
//
// Definition pattern (the handle variable has the C name, the method too):
//
//	var door_go_up = defThink("door_go_up")
//	func init() { door_go_up.bind((*Game).door_go_up) }
//
// Binding happens in init() so that method bodies may refer to handles
// without creating a Go initialization cycle. The registry is filled during
// package initialization and is read-only afterwards.

import (
	"fmt"
	"sort"

	"quake2web/server/internal/qcommon/shared"
)

// Fn is one registered C function of signature F.
type Fn[F any] struct {
	name string
	kind string
	fn   F
}

// Name returns the exact C function name.
func (f *Fn[F]) Name() string {
	if f == nil {
		return ""
	}
	return f.name
}

// bind attaches the Go implementation. It must be called exactly once, from init().
func (f *Fn[F]) bind(fn F) {
	if regBound[f.name] {
		panic("game: function bound twice: " + f.name)
	}
	regBound[f.name] = true
	f.fn = fn
}

// Handle types, one per C function pointer signature.
type (
	// ThinkFn: void (*)(edict_t *self) -- think, prethink, monsterinfo
	// stand/idle/search/walk/run/attack/melee, moveinfo.endfunc,
	// mmove_t endfunc, mframe_t thinkfunc, gitem_t weaponthink.
	ThinkFn = *Fn[func(g *Game, self *Edict)]
	// BlockedFn: void (*)(edict_t *self, edict_t *other) -- blocked, monsterinfo.sight.
	BlockedFn = *Fn[func(g *Game, self, other *Edict)]
	// TouchFn: void (*)(edict_t *self, edict_t *other, cplane_t *plane, csurface_t *surf).
	TouchFn = *Fn[func(g *Game, self, other *Edict, plane *CPlane, surf *CSurface)]
	// UseFn: void (*)(edict_t *self, edict_t *other, edict_t *activator).
	UseFn = *Fn[func(g *Game, self, other, activator *Edict)]
	// PainFn: void (*)(edict_t *self, edict_t *other, float kick, int damage).
	PainFn = *Fn[func(g *Game, self, other *Edict, kick float32, damage int32)]
	// DieFn: void (*)(edict_t *self, edict_t *inflictor, edict_t *attacker, int damage, vec3_t point).
	DieFn = *Fn[func(g *Game, self, inflictor, attacker *Edict, damage int32, point Vec3)]
	// AIFn: void (*)(edict_t *self, float dist) -- mframe_t aifunc.
	AIFn = *Fn[func(g *Game, self *Edict, dist float32)]
	// DodgeFn: void (*)(edict_t *self, edict_t *other, float eta).
	DodgeFn = *Fn[func(g *Game, self, other *Edict, eta float32)]
	// CheckAttackFn: qboolean (*)(edict_t *self).
	CheckAttackFn = *Fn[func(g *Game, self *Edict) bool]
	// PickupFn: qboolean (*)(edict_t *ent, edict_t *other).
	PickupFn = *Fn[func(g *Game, ent, other *Edict) bool]
	// ItemFn: void (*)(edict_t *ent, gitem_t *item) -- gitem_t use and drop.
	ItemFn = *Fn[func(g *Game, ent *Edict, item *GItem)]
)

// Registry kinds (names of the handle types).
const (
	kindThink       = "think"
	kindBlocked     = "blocked"
	kindTouch       = "touch"
	kindUse         = "use"
	kindPain        = "pain"
	kindDie         = "die"
	kindAI          = "ai"
	kindDodge       = "dodge"
	kindCheckAttack = "checkattack"
	kindPickup      = "pickup"
	kindItem        = "item"
)

// Package-level registries, written only during package initialization.
var (
	regByName = map[string]any{}    // C name -> handle (any *Fn[...])
	regKind   = map[string]string{} // C name -> kind
	regBound  = map[string]bool{}
	mmoves    = map[string]*MMove{}
	spawnRegs = map[string]func(g *Game, ent *Edict){}
)

func def[F any](kind, name string) *Fn[F] {
	if _, dup := regByName[name]; dup {
		panic("game: duplicate registry name: " + name)
	}
	f := &Fn[F]{name: name, kind: kind}
	regByName[name] = f
	regKind[name] = kind
	return f
}

func defThink(name string) ThinkFn     { return def[func(*Game, *Edict)](kindThink, name) }
func defBlocked(name string) BlockedFn { return def[func(*Game, *Edict, *Edict)](kindBlocked, name) }
func defUse(name string) UseFn         { return def[func(*Game, *Edict, *Edict, *Edict)](kindUse, name) }
func defAI(name string) AIFn           { return def[func(*Game, *Edict, float32)](kindAI, name) }
func defDodge(name string) DodgeFn     { return def[func(*Game, *Edict, *Edict, float32)](kindDodge, name) }
func defCheckAttack(name string) CheckAttackFn {
	return def[func(*Game, *Edict) bool](kindCheckAttack, name)
}
func defPickup(name string) PickupFn { return def[func(*Game, *Edict, *Edict) bool](kindPickup, name) }
func defItem(name string) ItemFn     { return def[func(*Game, *Edict, *GItem)](kindItem, name) }
func defTouch(name string) TouchFn {
	return def[func(*Game, *Edict, *Edict, *shared.CPlane, *shared.CSurface)](kindTouch, name)
}
func defPain(name string) PainFn {
	return def[func(*Game, *Edict, *Edict, float32, int32)](kindPain, name)
}
func defDie(name string) DieFn {
	return def[func(*Game, *Edict, *Edict, *Edict, int32, Vec3)](kindDie, name)
}

// lookupFn returns the handle registered under name if it has kind F, else nil.
// Used for references into files that may not be ported yet (g_turret.c uses
// infantry_die / infantry_stand from m_infantry.c) and by the save loader.
func lookupFn[F any](name string) *Fn[F] {
	h, ok := regByName[name].(*Fn[F])
	if !ok {
		return nil
	}
	return h
}

func thinkByName(name string) ThinkFn { return lookupFn[func(*Game, *Edict)](name) }
func dieByName(name string) DieFn {
	return lookupFn[func(*Game, *Edict, *Edict, *Edict, int32, Vec3)](name)
}

// defMMove registers an mmove_t table under its C variable name.
func defMMove(name string, firstframe, lastframe int32, frames []MFrame, endfunc ThinkFn) *MMove {
	if _, dup := mmoves[name]; dup {
		panic("game: duplicate mmove: " + name)
	}
	if int(lastframe-firstframe+1) != len(frames) {
		panic(fmt.Sprintf("game: mmove %s: %d frames for %d..%d", name, len(frames), firstframe, lastframe))
	}
	m := &MMove{Name: name, Firstframe: firstframe, Lastframe: lastframe, Frame: frames, Endfunc: endfunc}
	mmoves[name] = m
	return m
}

// MMoveByName returns the registered mmove table (nil if unknown).
func MMoveByName(name string) *MMove { return mmoves[name] }

// RegisterSpawn registers a spawn function for a classname that is not in
// the fixed spawns[] table of g_spawn.go -- monster files (m_*.go) use it
// from init() so they can be added independently:
//
//	func init() { RegisterSpawn("monster_soldier", (*Game).SP_monster_soldier) }
func RegisterSpawn(classname string, fn func(g *Game, ent *Edict)) {
	if _, dup := spawnRegs[classname]; dup {
		panic("game: duplicate spawn registration: " + classname)
	}
	spawnRegs[classname] = fn
}

// RegistryNames returns every registered function name, sorted.
func RegistryNames() []string {
	names := make([]string, 0, len(regByName))
	for n := range regByName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// RegistryKind returns the handle kind of a registered function name ("" if unknown).
func RegistryKind(name string) string { return regKind[name] }

// registryUnbound lists registered names without an implementation.
func registryUnbound() []string {
	var out []string
	for n := range regByName {
		if !regBound[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
