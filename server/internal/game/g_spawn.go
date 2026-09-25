package game

// Port of game/g_spawn.c, plus the fields[] table of game/g_save.c used by
// ED_ParseField.

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// spawn_t
// C: game/g_spawn.c:22 spawn_t
type spawn_t struct {
	name  string
	spawn func(g *Game, ent *Edict)
}

// spawns is the fixed spawns[] table (entries whose spawn function lives in
// a monster file m_*.c are registered with RegisterSpawn by that file).
// Built in init() and read-only afterwards.
// C: game/g_spawn.c:146 spawns
var spawns []spawn_t

func init() {
	spawns = []spawn_t{
		{"item_health", (*Game).SP_item_health},
		{"item_health_small", (*Game).SP_item_health_small},
		{"item_health_large", (*Game).SP_item_health_large},
		{"item_health_mega", (*Game).SP_item_health_mega},
		{"info_player_start", (*Game).SP_info_player_start},
		{"info_player_deathmatch", (*Game).SP_info_player_deathmatch},
		{"info_player_coop", (*Game).SP_info_player_coop},
		{"info_player_intermission", (*Game).SP_info_player_intermission},
		{"func_plat", (*Game).SP_func_plat},
		{"func_button", (*Game).SP_func_button},
		{"func_door", (*Game).SP_func_door},
		{"func_door_secret", (*Game).SP_func_door_secret},
		{"func_door_rotating", (*Game).SP_func_door_rotating},
		{"func_rotating", (*Game).SP_func_rotating},
		{"func_train", (*Game).SP_func_train},
		{"func_water", (*Game).SP_func_water},
		{"func_conveyor", (*Game).SP_func_conveyor},
		{"func_areaportal", (*Game).SP_func_areaportal},
		{"func_clock", (*Game).SP_func_clock},
		{"func_wall", (*Game).SP_func_wall},
		{"func_object", (*Game).SP_func_object},
		{"func_timer", (*Game).SP_func_timer},
		{"func_explosive", (*Game).SP_func_explosive},
		{"func_killbox", (*Game).SP_func_killbox},
		{"trigger_always", (*Game).SP_trigger_always},
		{"trigger_once", (*Game).SP_trigger_once},
		{"trigger_multiple", (*Game).SP_trigger_multiple},
		{"trigger_relay", (*Game).SP_trigger_relay},
		{"trigger_push", (*Game).SP_trigger_push},
		{"trigger_hurt", (*Game).SP_trigger_hurt},
		{"trigger_key", (*Game).SP_trigger_key},
		{"trigger_counter", (*Game).SP_trigger_counter},
		{"trigger_elevator", (*Game).SP_trigger_elevator},
		{"trigger_gravity", (*Game).SP_trigger_gravity},
		{"trigger_monsterjump", (*Game).SP_trigger_monsterjump},
		{"target_temp_entity", (*Game).SP_target_temp_entity},
		{"target_speaker", (*Game).SP_target_speaker},
		{"target_explosion", (*Game).SP_target_explosion},
		{"target_changelevel", (*Game).SP_target_changelevel},
		{"target_secret", (*Game).SP_target_secret},
		{"target_goal", (*Game).SP_target_goal},
		{"target_splash", (*Game).SP_target_splash},
		{"target_spawner", (*Game).SP_target_spawner},
		{"target_blaster", (*Game).SP_target_blaster},
		{"target_crosslevel_trigger", (*Game).SP_target_crosslevel_trigger},
		{"target_crosslevel_target", (*Game).SP_target_crosslevel_target},
		{"target_laser", (*Game).SP_target_laser},
		{"target_help", (*Game).SP_target_help},
		// {"target_actor", SP_target_actor} is in m_actor.c: registered via RegisterSpawn
		{"target_lightramp", (*Game).SP_target_lightramp},
		{"target_earthquake", (*Game).SP_target_earthquake},
		{"target_character", (*Game).SP_target_character},
		{"target_string", (*Game).SP_target_string},
		{"worldspawn", (*Game).SP_worldspawn},
		{"viewthing", (*Game).SP_viewthing},
		{"light", (*Game).SP_light},
		{"light_mine1", (*Game).SP_light_mine1},
		{"light_mine2", (*Game).SP_light_mine2},
		{"info_null", (*Game).SP_info_null},
		{"func_group", (*Game).SP_info_null},
		{"info_notnull", (*Game).SP_info_notnull},
		{"path_corner", (*Game).SP_path_corner},
		{"point_combat", (*Game).SP_point_combat},
		{"misc_explobox", (*Game).SP_misc_explobox},
		{"misc_banner", (*Game).SP_misc_banner},
		{"misc_satellite_dish", (*Game).SP_misc_satellite_dish},
		// {"misc_actor", SP_misc_actor} is in m_actor.c: registered via RegisterSpawn
		{"misc_gib_arm", (*Game).SP_misc_gib_arm},
		{"misc_gib_leg", (*Game).SP_misc_gib_leg},
		{"misc_gib_head", (*Game).SP_misc_gib_head},
		// {"misc_insane", SP_misc_insane} is in m_insane.c: registered via RegisterSpawn
		{"misc_deadsoldier", (*Game).SP_misc_deadsoldier},
		{"misc_viper", (*Game).SP_misc_viper},
		{"misc_viper_bomb", (*Game).SP_misc_viper_bomb},
		{"misc_bigviper", (*Game).SP_misc_bigviper},
		{"misc_strogg_ship", (*Game).SP_misc_strogg_ship},
		{"misc_teleporter", (*Game).SP_misc_teleporter},
		{"misc_teleporter_dest", (*Game).SP_misc_teleporter_dest},
		{"misc_blackhole", (*Game).SP_misc_blackhole},
		{"misc_eastertank", (*Game).SP_misc_eastertank},
		{"misc_easterchick", (*Game).SP_misc_easterchick},
		{"misc_easterchick2", (*Game).SP_misc_easterchick2},
		// {"monster_berserk", SP_monster_berserk} is in m_berserk.c: registered via RegisterSpawn
		// {"monster_gladiator", SP_monster_gladiator} is in m_gladiator.c: registered via RegisterSpawn
		// {"monster_gunner", SP_monster_gunner} is in m_gunner.c: registered via RegisterSpawn
		// {"monster_infantry", SP_monster_infantry} is in m_infantry.c: registered via RegisterSpawn
		// {"monster_soldier_light", SP_monster_soldier_light} is in m_soldier.c: registered via RegisterSpawn
		// {"monster_soldier", SP_monster_soldier} is in m_soldier.c: registered via RegisterSpawn
		// {"monster_soldier_ss", SP_monster_soldier_ss} is in m_soldier.c: registered via RegisterSpawn
		// {"monster_tank", SP_monster_tank} is in m_tank.c: registered via RegisterSpawn
		// {"monster_tank_commander", SP_monster_tank} is in m_tank.c: registered via RegisterSpawn
		// {"monster_medic", SP_monster_medic} is in m_medic.c: registered via RegisterSpawn
		// {"monster_flipper", SP_monster_flipper} is in m_flipper.c: registered via RegisterSpawn
		// {"monster_chick", SP_monster_chick} is in m_chick.c: registered via RegisterSpawn
		// {"monster_parasite", SP_monster_parasite} is in m_parasite.c: registered via RegisterSpawn
		// {"monster_flyer", SP_monster_flyer} is in m_flyer.c: registered via RegisterSpawn
		// {"monster_brain", SP_monster_brain} is in m_brain.c: registered via RegisterSpawn
		// {"monster_floater", SP_monster_floater} is in m_float.c: registered via RegisterSpawn
		// {"monster_hover", SP_monster_hover} is in m_hover.c: registered via RegisterSpawn
		// {"monster_mutant", SP_monster_mutant} is in m_mutant.c: registered via RegisterSpawn
		// {"monster_supertank", SP_monster_supertank} is in m_supertank.c: registered via RegisterSpawn
		// {"monster_boss2", SP_monster_boss2} is in m_boss2.c: registered via RegisterSpawn
		// {"monster_boss3_stand", SP_monster_boss3_stand} is in m_boss3.c: registered via RegisterSpawn
		// {"monster_jorg", SP_monster_jorg} is in m_boss31.c: registered via RegisterSpawn
		{"monster_commander_body", (*Game).SP_monster_commander_body},
		{"turret_breach", (*Game).SP_turret_breach},
		{"turret_base", (*Game).SP_turret_base},
		{"turret_driver", (*Game).SP_turret_driver},
	}
}

// field_t: one entry of the fields[] table.
// C: game/g_local.h:588 field_t
type field_t struct {
	name  string
	type_ int32 // fieldtype_t
	flags int32
	// set stores the parsed spawn value (nil for types ED_ParseField ignores:
	// F_EDICT, F_ITEM, F_FUNCTION, F_MMOVE, F_IGNORE, ...).
	set func(g *Game, ent *Edict, value string)
}

// Setter helpers for the fields table.
func spawnLString(f func(g *Game, e *Edict) *string) func(*Game, *Edict, string) {
	return func(g *Game, e *Edict, v string) { *f(g, e) = ED_NewString(v) }
}

func spawnInt(f func(g *Game, e *Edict) *int32) func(*Game, *Edict, string) {
	return func(g *Game, e *Edict, v string) { *f(g, e) = shared.Atoi(v) }
}

func spawnFloat(f func(g *Game, e *Edict) *float32) func(*Game, *Edict, string) {
	return func(g *Game, e *Edict, v string) { *f(g, e) = float32(shared.Atof(v)) }
}

func spawnVector(f func(g *Game, e *Edict) *Vec3) func(*Game, *Edict, string) {
	return func(g *Game, e *Edict, v string) {
		// C: sscanf (value, "%f %f %f", &vec[0], &vec[1], &vec[2]) into an
		// uninitialized local, then all three components are copied.
		var vec Vec3
		spawnSscanfVec(v, &vec)
		*f(g, e) = vec
	}
}

// spawnSscanfVec emulates glibc sscanf(value, "%f %f %f", ...): each
// conversion skips white space and converts the longest valid strtof
// prefix; scanning stops at the first failed conversion (the remaining
// components are left untouched).
func spawnSscanfVec(s string, vec *Vec3) {
	for i := 0; i < 3; i++ {
		v, n, ok := spawnStrtof(s)
		if !ok {
			return
		}
		vec[i] = v
		s = s[n:]
	}
}

// spawnStrtof converts the longest valid prefix of s (after leading white
// space) to float like glibc strtof (rounded once to float).
func spawnStrtof(s string) (float32, int, bool) {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	d, n := shared.Strtod(s)
	if n == 0 {
		return 0, 0, false
	}
	j := 0
	for j < n && (s[j] == ' ' || (s[j] >= '\t' && s[j] <= '\r')) {
		j++
	}
	prefix := s[j:n]
	decimal := true
	for k := 0; k < len(prefix); k++ {
		c := prefix[k]
		if !(c >= '0' && c <= '9' || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E') {
			decimal = false
			break
		}
	}
	if decimal {
		if f, err := strconv.ParseFloat(prefix, 32); err == nil || math.IsInf(f, 0) || f == 0 {
			return float32(f), n, true
		}
	}
	return float32(d), n, true
}

// fields is the fields[] table of g_save.c (exact order and flags).
// C: game/g_save.c:26 fields
var fields = []field_t{
	{"classname", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Classname })},
	{"model", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Model })},
	{"spawnflags", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Spawnflags })},
	{"speed", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Speed })},
	{"accel", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Accel })},
	{"decel", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Decel })},
	{"target", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Target })},
	{"targetname", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Targetname })},
	{"pathtarget", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Pathtarget })},
	{"deathtarget", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Deathtarget })},
	{"killtarget", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Killtarget })},
	{"combattarget", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Combattarget })},
	{"message", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Message })},
	{"team", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Team })},
	{"wait", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Wait })},
	{"delay", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Delay })},
	{"random", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Random })},
	{"move_origin", F_VECTOR, 0, spawnVector(func(g *Game, e *Edict) *Vec3 { return &e.MoveOrigin })},
	{"move_angles", F_VECTOR, 0, spawnVector(func(g *Game, e *Edict) *Vec3 { return &e.MoveAngles })},
	{"style", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Style })},
	{"count", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Count })},
	{"health", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Health })},
	{"sounds", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Sounds })},
	{"light", F_IGNORE, 0, nil},
	{"dmg", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Dmg })},
	{"mass", F_INT, 0, spawnInt(func(g *Game, e *Edict) *int32 { return &e.Mass })},
	{"volume", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Volume })},
	{"attenuation", F_FLOAT, 0, spawnFloat(func(g *Game, e *Edict) *float32 { return &e.Attenuation })},
	{"map", F_LSTRING, 0, spawnLString(func(g *Game, e *Edict) *string { return &e.Map })},
	{"origin", F_VECTOR, 0, spawnVector(func(g *Game, e *Edict) *Vec3 { return &e.S.Origin })},
	{"angles", F_VECTOR, 0, spawnVector(func(g *Game, e *Edict) *Vec3 { return &e.S.Angles })},
	{"angle", F_ANGLEHACK, 0, func(g *Game, e *Edict, v string) {
		a := float32(shared.Atof(v))
		e.S.Angles = Vec3{0, a, 0}
	}},

	{"goalentity", F_EDICT, FFL_NOSPAWN, nil},
	{"movetarget", F_EDICT, FFL_NOSPAWN, nil},
	{"enemy", F_EDICT, FFL_NOSPAWN, nil},
	{"oldenemy", F_EDICT, FFL_NOSPAWN, nil},
	{"activator", F_EDICT, FFL_NOSPAWN, nil},
	{"groundentity", F_EDICT, FFL_NOSPAWN, nil},
	{"teamchain", F_EDICT, FFL_NOSPAWN, nil},
	{"teammaster", F_EDICT, FFL_NOSPAWN, nil},
	{"owner", F_EDICT, FFL_NOSPAWN, nil},
	{"mynoise", F_EDICT, FFL_NOSPAWN, nil},
	{"mynoise2", F_EDICT, FFL_NOSPAWN, nil},
	{"target_ent", F_EDICT, FFL_NOSPAWN, nil},
	{"chain", F_EDICT, FFL_NOSPAWN, nil},

	{"prethink", F_FUNCTION, FFL_NOSPAWN, nil},
	{"think", F_FUNCTION, FFL_NOSPAWN, nil},
	{"blocked", F_FUNCTION, FFL_NOSPAWN, nil},
	{"touch", F_FUNCTION, FFL_NOSPAWN, nil},
	{"use", F_FUNCTION, FFL_NOSPAWN, nil},
	{"pain", F_FUNCTION, FFL_NOSPAWN, nil},
	{"die", F_FUNCTION, FFL_NOSPAWN, nil},

	{"stand", F_FUNCTION, FFL_NOSPAWN, nil},
	{"idle", F_FUNCTION, FFL_NOSPAWN, nil},
	{"search", F_FUNCTION, FFL_NOSPAWN, nil},
	{"walk", F_FUNCTION, FFL_NOSPAWN, nil},
	{"run", F_FUNCTION, FFL_NOSPAWN, nil},
	{"dodge", F_FUNCTION, FFL_NOSPAWN, nil},
	{"attack", F_FUNCTION, FFL_NOSPAWN, nil},
	{"melee", F_FUNCTION, FFL_NOSPAWN, nil},
	{"sight", F_FUNCTION, FFL_NOSPAWN, nil},
	{"checkattack", F_FUNCTION, FFL_NOSPAWN, nil},
	{"currentmove", F_MMOVE, FFL_NOSPAWN, nil},

	{"endfunc", F_FUNCTION, FFL_NOSPAWN, nil},

	// temp spawn vars -- only valid when the spawn function is called
	{"lip", F_INT, FFL_SPAWNTEMP, spawnInt(func(g *Game, e *Edict) *int32 { return &g.st.Lip })},
	{"distance", F_INT, FFL_SPAWNTEMP, spawnInt(func(g *Game, e *Edict) *int32 { return &g.st.Distance })},
	{"height", F_INT, FFL_SPAWNTEMP, spawnInt(func(g *Game, e *Edict) *int32 { return &g.st.Height })},
	{"noise", F_LSTRING, FFL_SPAWNTEMP, spawnLString(func(g *Game, e *Edict) *string { return &g.st.Noise })},
	{"pausetime", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Pausetime })},
	{"item", F_LSTRING, FFL_SPAWNTEMP, spawnLString(func(g *Game, e *Edict) *string { return &g.st.Item })},

	//need for item field in edict struct, FFL_SPAWNTEMP item will be skipped on saves
	{"item", F_ITEM, 0, nil},

	{"gravity", F_LSTRING, FFL_SPAWNTEMP, spawnLString(func(g *Game, e *Edict) *string { return &g.st.Gravity })},
	{"sky", F_LSTRING, FFL_SPAWNTEMP, spawnLString(func(g *Game, e *Edict) *string { return &g.st.Sky })},
	{"skyrotate", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Skyrotate })},
	{"skyaxis", F_VECTOR, FFL_SPAWNTEMP, spawnVector(func(g *Game, e *Edict) *Vec3 { return &g.st.Skyaxis })},
	{"minyaw", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Minyaw })},
	{"maxyaw", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Maxyaw })},
	{"minpitch", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Minpitch })},
	{"maxpitch", F_FLOAT, FFL_SPAWNTEMP, spawnFloat(func(g *Game, e *Edict) *float32 { return &g.st.Maxpitch })},
	{"nextmap", F_LSTRING, FFL_SPAWNTEMP, spawnLString(func(g *Game, e *Edict) *string { return &g.st.Nextmap })},
}

// ED_CallSpawn finds the spawn function for the entity and calls it.
// C: game/g_spawn.c:278 ED_CallSpawn
func (g *Game) ED_CallSpawn(ent *Edict) {
	if ent.Classname == "" {
		g.gi.Dprintf("ED_CallSpawn: NULL classname\n")
		return
	}

	// check item spawn functions
	for i := 0; i < int(g.game.NumItems); i++ {
		item := &itemlist[i]
		if item.Classname == "" {
			continue
		}
		if item.Classname == ent.Classname { // found it
			g.SpawnItem(ent, item)
			return
		}
	}

	// check normal spawn functions
	for _, s := range spawns {
		if s.name == ent.Classname { // found it
			s.spawn(g, ent)
			return
		}
	}
	// spawn functions of the monster files (C: also in spawns[])
	if fn, ok := spawnRegs[ent.Classname]; ok {
		fn(g, ent)
		return
	}
	// Spawn functions of m_*.c files that are not ported yet: every one of
	// them (except SP_target_actor) starts with
	//	if (deathmatch->value) { G_FreeEdict (self); return; }
	// which is reproduced here so deathmatch entity numbering matches C.
	if unportedMonsterSpawns[ent.Classname] {
		if g.deathmatch.Value != 0 {
			g.G_FreeEdict(ent)
			return
		}
		g.dprintf("%s: spawn function not ported yet (m_*.c)\n", ent.Classname)
		return
	}
	g.dprintf("%s doesn't have a spawn function\n", ent.Classname)
}

// unportedMonsterSpawns lists the spawns[] classnames whose SP_ function
// lives in a monster file and begins with the deathmatch G_FreeEdict check.
// Entries are only used while no RegisterSpawn exists for the classname.
var unportedMonsterSpawns = map[string]bool{
	"misc_actor": true, "misc_insane": true,
	"monster_berserk": true, "monster_gladiator": true, "monster_gunner": true,
	"monster_infantry": true, "monster_soldier_light": true, "monster_soldier": true,
	"monster_soldier_ss": true, "monster_tank": true, "monster_tank_commander": true,
	"monster_medic": true, "monster_flipper": true, "monster_chick": true,
	"monster_parasite": true, "monster_flyer": true, "monster_brain": true,
	"monster_floater": true, "monster_hover": true, "monster_mutant": true,
	"monster_supertank": true, "monster_boss2": true, "monster_boss3_stand": true,
	"monster_jorg": true,
}

// ED_NewString: "\n" escapes become newlines, any other backslash pair a
// single backslash.
// C: game/g_spawn.c:319 ED_NewString
func ED_NewString(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	l := len(s) + 1 // includes the terminating NUL
	for i := 0; i < l; i++ {
		var c byte
		if i < len(s) {
			c = s[i]
		}
		if c == '\\' && i < l-1 {
			i++
			var n byte
			if i < len(s) {
				n = s[i]
			}
			if n == 'n' {
				b = append(b, '\n')
			} else {
				b = append(b, '\\')
			}
		} else if i < len(s) {
			b = append(b, c)
		}
	}
	return string(b)
}

// ED_ParseField takes a key/value pair and sets the binary values in an edict.
// C: game/g_spawn.c:358 ED_ParseField
func (g *Game) ED_ParseField(key, value string, ent *Edict) {
	for i := range fields {
		f := &fields[i]
		if f.flags&FFL_NOSPAWN == 0 && shared.Q_stricmp(f.name, key) == 0 { // found it
			if f.set != nil {
				f.set(g, ent, value)
			}
			return
		}
	}
	g.dprintf("%s is not a field\n", key)
}

// ED_ParseEdict parses an edict out of the given string, returning the new
// position. ed should be a properly initialized empty edict.
// more is false where C returns NULL (end of data).
// C: game/g_spawn.c:414 ED_ParseEdict
func (g *Game) ED_ParseEdict(data string, ent *Edict) (rest string, more bool) {
	var keyname string
	init := false
	g.st = SpawnTemp{}
	more = true

	// go through all the dictionary pairs
	for {
		// parse key
		var comToken string
		comToken, data, more = shared.COM_Parse(data)
		if len(comToken) > 0 && comToken[0] == '}' {
			break
		}
		if !more {
			g.gi.Error("ED_ParseEntity: EOF without closing brace")
		}

		keyname = comToken
		if len(keyname) > 255 {
			keyname = keyname[:255]
		}

		// parse value
		comToken, data, more = shared.COM_Parse(data)
		if !more {
			g.gi.Error("ED_ParseEntity: EOF without closing brace")
		}

		if len(comToken) > 0 && comToken[0] == '}' {
			g.gi.Error("ED_ParseEntity: closing brace without data")
		}

		init = true

		// keynames with a leading underscore are used for utility comments,
		// and are immediately discarded by quake
		if len(keyname) > 0 && keyname[0] == '_' {
			continue
		}

		g.ED_ParseField(keyname, comToken, ent)
	}

	if !init {
		clearEdict(ent)
	}

	return data, more
}

// G_FindTeams chains together all entities with a matching team field.
//
// All but the first will have the FL_TEAMSLAVE flag set.
// All but the last will have the teamchain field set to the next one
// C: game/g_spawn.c:470 G_FindTeams
func (g *Game) G_FindTeams() {
	c := 0
	c2 := 0
	for i := 1; i < int(g.num_edicts); i++ {
		e := &g.edicts[i]
		if !e.InUse {
			continue
		}
		if e.Team == "" {
			continue
		}
		if e.Flags&FL_TEAMSLAVE != 0 {
			continue
		}
		chain := e
		e.Teammaster = e
		c++
		c2++
		for j := i + 1; j < int(g.num_edicts); j++ {
			e2 := &g.edicts[j]
			if !e2.InUse {
				continue
			}
			if e2.Team == "" {
				continue
			}
			if e2.Flags&FL_TEAMSLAVE != 0 {
				continue
			}
			if e.Team == e2.Team {
				c2++
				chain.Teamchain = e2
				e2.Teammaster = e
				chain = e2
				e2.Flags |= FL_TEAMSLAVE
			}
		}
	}

	g.dprintf("%i teams with %i entities\n", c, c2)
}

// SpawnEntities creates a server's entity / program execution context by
// parsing textual entity definitions out of an ent file.
// C: game/g_spawn.c:520 SpawnEntities
func (g *Game) SpawnEntities(mapname, entities, spawnpoint string) {
	var ent *Edict

	skillLevel := math.Floor(float64(g.skill.Value))
	if skillLevel < 0 {
		skillLevel = 0
	}
	if skillLevel > 3 {
		skillLevel = 3
	}
	if float64(g.skill.Value) != skillLevel {
		g.gi.CvarForceSet("skill", fmt.Sprintf("%f", skillLevel))
	}

	g.SaveClientData()

	g.level = LevelLocals{}
	for i := range g.edicts {
		clearEdict(&g.edicts[i])
	}

	g.level.Mapname = spawnStrncpy(mapname, MAX_QPATH)
	g.game.Spawnpoint = spawnStrncpy(spawnpoint, 512)

	// set client fields on player ents
	for i := 0; i < int(g.game.Maxclients); i++ {
		g.edicts[i+1].Client = &g.game.Clients[i]
	}

	ent = nil
	inhibit := 0

	// parse ents
	for {
		// parse the opening brace
		var comToken string
		var more bool
		comToken, entities, more = shared.COM_Parse(entities)
		if !more {
			break
		}
		if len(comToken) == 0 || comToken[0] != '{' {
			g.error("ED_LoadFromFile: found %s when expecting {", comToken)
		}

		if ent == nil {
			ent = &g.edicts[0]
		} else {
			ent = g.G_Spawn()
		}
		entities, _ = g.ED_ParseEdict(entities, ent)

		// yet another map hack
		if shared.Q_stricmp(g.level.Mapname, "command") == 0 && shared.Q_stricmp(ent.Classname, "trigger_once") == 0 && shared.Q_stricmp(ent.Model, "*27") == 0 {
			ent.Spawnflags &^= SPAWNFLAG_NOT_HARD
		}

		// remove things (except the world) from different skill levels or deathmatch
		if ent != &g.edicts[0] {
			if g.deathmatch.Value != 0 {
				if ent.Spawnflags&SPAWNFLAG_NOT_DEATHMATCH != 0 {
					g.G_FreeEdict(ent)
					inhibit++
					continue
				}
			} else {
				if /* ((coop->value) && (ent->spawnflags & SPAWNFLAG_NOT_COOP)) || */
				(g.skill.Value == 0 && ent.Spawnflags&SPAWNFLAG_NOT_EASY != 0) ||
					(g.skill.Value == 1 && ent.Spawnflags&SPAWNFLAG_NOT_MEDIUM != 0) ||
					((g.skill.Value == 2 || g.skill.Value == 3) && ent.Spawnflags&SPAWNFLAG_NOT_HARD != 0) {
					g.G_FreeEdict(ent)
					inhibit++
					continue
				}
			}

			ent.Spawnflags &^= (SPAWNFLAG_NOT_EASY | SPAWNFLAG_NOT_MEDIUM | SPAWNFLAG_NOT_HARD | SPAWNFLAG_NOT_COOP | SPAWNFLAG_NOT_DEATHMATCH)
		}

		g.ED_CallSpawn(ent)
	}

	g.dprintf("%i entities inhibited\n", inhibit)

	g.G_FindTeams()

	g.PlayerTrail_Init()
}

// spawnStrncpy emulates strncpy(dst, src, size-1) into a zeroed char[size].
func spawnStrncpy(s string, size int) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	if len(s) > size-1 {
		return s[:size-1]
	}
	return s
}

//===================================================================

/*
	// cursor positioning
	xl <value>
	xr <value>
	yb <value>
	yt <value>
	xv <value>
	yv <value>

	// drawing
	statpic <name>
	pic <stat>
	num <fieldwidth> <stat>
	string <stat>

	// control
	if <stat>
	ifeq <stat> <value>
	ifbit <stat> <value>
	endif
*/

// C: game/g_spawn.c:648 single_statusbar
const single_statusbar = "yb	-24 " +

	// health
	"xv	0 " +
	"hnum " +
	"xv	50 " +
	"pic 0 " +

	// ammo
	"if 2 " +
	"	xv	100 " +
	"	anum " +
	"	xv	150 " +
	"	pic 2 " +
	"endif " +

	// armor
	"if 4 " +
	"	xv	200 " +
	"	rnum " +
	"	xv	250 " +
	"	pic 4 " +
	"endif " +

	// selected item
	"if 6 " +
	"	xv	296 " +
	"	pic 6 " +
	"endif " +

	"yb	-50 " +

	// picked up item
	"if 7 " +
	"	xv	0 " +
	"	pic 7 " +
	"	xv	26 " +
	"	yb	-42 " +
	"	stat_string 8 " +
	"	yb	-50 " +
	"endif " +

	// timer
	"if 9 " +
	"	xv	262 " +
	"	num	2	10 " +
	"	xv	296 " +
	"	pic	9 " +
	"endif " +

	//  help / weapon icon
	"if 11 " +
	"	xv	148 " +
	"	pic	11 " +
	"endif "

// C: game/g_spawn.c:706 dm_statusbar
const dm_statusbar = "yb	-24 " +

	// health
	"xv	0 " +
	"hnum " +
	"xv	50 " +
	"pic 0 " +

	// ammo
	"if 2 " +
	"	xv	100 " +
	"	anum " +
	"	xv	150 " +
	"	pic 2 " +
	"endif " +

	// armor
	"if 4 " +
	"	xv	200 " +
	"	rnum " +
	"	xv	250 " +
	"	pic 4 " +
	"endif " +

	// selected item
	"if 6 " +
	"	xv	296 " +
	"	pic 6 " +
	"endif " +

	"yb	-50 " +

	// picked up item
	"if 7 " +
	"	xv	0 " +
	"	pic 7 " +
	"	xv	26 " +
	"	yb	-42 " +
	"	stat_string 8 " +
	"	yb	-50 " +
	"endif " +

	// timer
	"if 9 " +
	"	xv	246 " +
	"	num	2	10 " +
	"	xv	296 " +
	"	pic	9 " +
	"endif " +

	//  help / weapon icon
	"if 11 " +
	"	xv	148 " +
	"	pic	11 " +
	"endif " +

	//  frags
	"xr	-50 " +
	"yt 2 " +
	"num 3 14 " +

	// spectator
	"if 17 " +
	"xv 0 " +
	"yb -58 " +
	"string2 \"SPECTATOR MODE\" " +
	"endif " +

	// chase camera
	"if 16 " +
	"xv 0 " +
	"yb -68 " +
	"string \"Chasing\" " +
	"xv 64 " +
	"stat_string 16 " +
	"endif "

/*QUAKED worldspawn (0 0 0) ?

Only used for the world.
"sky"	environment map name
"skyaxis"	vector axis for rotating sky
"skyrotate"	speed of rotation in degrees/second
"sounds"	music cd track number
"gravity"	800 is default gravity
"message"	text to print at user logon
*/
// C: game/g_spawn.c:796 SP_worldspawn
func (g *Game) SP_worldspawn(ent *Edict) {
	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_BSP
	ent.InUse = true     // since the world doesn't use G_Spawn()
	ent.S.ModelIndex = 1 // world model is always index 1

	//---------------

	// reserve some spots for dead player bodies for coop / deathmatch
	g.InitBodyQue()

	// set configstrings for items
	g.SetItemNames()

	if g.st.Nextmap != "" {
		g.level.Nextmap = g.st.Nextmap
	}

	// make some data visible to the server

	if ent.Message != "" {
		g.gi.Configstring(CS_NAME, ent.Message)
		// strncpy (level.level_name, ent->message, sizeof(level.level_name))
		g.level.LevelName = ent.Message
		if len(g.level.LevelName) > MAX_QPATH {
			g.level.LevelName = g.level.LevelName[:MAX_QPATH]
		}
	} else {
		g.level.LevelName = g.level.Mapname
	}

	if g.st.Sky != "" {
		g.gi.Configstring(CS_SKY, g.st.Sky)
	} else {
		g.gi.Configstring(CS_SKY, "unit1_")
	}

	g.gi.Configstring(CS_SKYROTATE, fmt.Sprintf("%f", float64(g.st.Skyrotate)))

	g.gi.Configstring(CS_SKYAXIS, fmt.Sprintf("%f %f %f",
		float64(g.st.Skyaxis[0]), float64(g.st.Skyaxis[1]), float64(g.st.Skyaxis[2])))

	g.gi.Configstring(CS_CDTRACK, fmt.Sprintf("%d", ent.Sounds))

	g.gi.Configstring(CS_MAXCLIENTS, fmt.Sprintf("%d", int32(g.maxclients.Value)))

	// status bar program
	if g.deathmatch.Value != 0 {
		g.gi.Configstring(CS_STATUSBAR, dm_statusbar)
	} else {
		g.gi.Configstring(CS_STATUSBAR, single_statusbar)
	}

	//---------------

	// help icon for statusbar
	g.gi.ImageIndex("i_help")
	g.level.PicHealth = int32(g.gi.ImageIndex("i_health"))
	g.gi.ImageIndex("help")
	g.gi.ImageIndex("field_3")

	if g.st.Gravity == "" {
		g.gi.CvarSet("sv_gravity", "800")
	} else {
		g.gi.CvarSet("sv_gravity", g.st.Gravity)
	}

	g.snd_fry = int32(g.gi.SoundIndex("player/fry.wav")) // standing in lava / slime

	g.PrecacheItem(g.FindItem("Blaster"))

	g.gi.SoundIndex("player/lava1.wav")
	g.gi.SoundIndex("player/lava2.wav")

	g.gi.SoundIndex("misc/pc_up.wav")
	g.gi.SoundIndex("misc/talk1.wav")

	g.gi.SoundIndex("misc/udeath.wav")

	// gibs
	g.gi.SoundIndex("items/respawn1.wav")

	// sexed sounds
	g.gi.SoundIndex("*death1.wav")
	g.gi.SoundIndex("*death2.wav")
	g.gi.SoundIndex("*death3.wav")
	g.gi.SoundIndex("*death4.wav")
	g.gi.SoundIndex("*fall1.wav")
	g.gi.SoundIndex("*fall2.wav")
	g.gi.SoundIndex("*gurp1.wav") // drowning damage
	g.gi.SoundIndex("*gurp2.wav")
	g.gi.SoundIndex("*jump1.wav") // player jump
	g.gi.SoundIndex("*pain25_1.wav")
	g.gi.SoundIndex("*pain25_2.wav")
	g.gi.SoundIndex("*pain50_1.wav")
	g.gi.SoundIndex("*pain50_2.wav")
	g.gi.SoundIndex("*pain75_1.wav")
	g.gi.SoundIndex("*pain75_2.wav")
	g.gi.SoundIndex("*pain100_1.wav")
	g.gi.SoundIndex("*pain100_2.wav")

	// sexed models
	// THIS ORDER MUST MATCH THE DEFINES IN g_local.h
	// you can add more, max 15
	g.gi.ModelIndex("#w_blaster.md2")
	g.gi.ModelIndex("#w_shotgun.md2")
	g.gi.ModelIndex("#w_sshotgun.md2")
	g.gi.ModelIndex("#w_machinegun.md2")
	g.gi.ModelIndex("#w_chaingun.md2")
	g.gi.ModelIndex("#a_grenades.md2")
	g.gi.ModelIndex("#w_glauncher.md2")
	g.gi.ModelIndex("#w_rlauncher.md2")
	g.gi.ModelIndex("#w_hyperblaster.md2")
	g.gi.ModelIndex("#w_railgun.md2")
	g.gi.ModelIndex("#w_bfg.md2")

	//-------------------

	g.gi.SoundIndex("player/gasp1.wav") // gasping for air
	g.gi.SoundIndex("player/gasp2.wav") // head breaking surface, not gasping

	g.gi.SoundIndex("player/watr_in.wav")  // feet hitting water
	g.gi.SoundIndex("player/watr_out.wav") // feet leaving water

	g.gi.SoundIndex("player/watr_un.wav") // head going underwater

	g.gi.SoundIndex("player/u_breath1.wav")
	g.gi.SoundIndex("player/u_breath2.wav")

	g.gi.SoundIndex("items/pkup.wav")   // bonus item pickup
	g.gi.SoundIndex("world/land.wav")   // landing thud
	g.gi.SoundIndex("misc/h2ohit1.wav") // landing splash

	g.gi.SoundIndex("items/damage.wav")
	g.gi.SoundIndex("items/protect.wav")
	g.gi.SoundIndex("items/protect4.wav")
	g.gi.SoundIndex("weapons/noammo.wav")

	g.gi.SoundIndex("infantry/inflies1.wav")

	g.sm_meat_index = int32(g.gi.ModelIndex("models/objects/gibs/sm_meat/tris.md2"))
	g.gi.ModelIndex("models/objects/gibs/arm/tris.md2")
	g.gi.ModelIndex("models/objects/gibs/bone/tris.md2")
	g.gi.ModelIndex("models/objects/gibs/bone2/tris.md2")
	g.gi.ModelIndex("models/objects/gibs/chest/tris.md2")
	g.gi.ModelIndex("models/objects/gibs/skull/tris.md2")
	g.gi.ModelIndex("models/objects/gibs/head2/tris.md2")

	//
	// Setup light animation tables. 'a' is total darkness, 'z' is doublebright.
	//

	// 0 normal
	g.gi.Configstring(CS_LIGHTS+0, "m")

	// 1 FLICKER (first variety)
	g.gi.Configstring(CS_LIGHTS+1, "mmnmmommommnonmmonqnmmo")

	// 2 SLOW STRONG PULSE
	g.gi.Configstring(CS_LIGHTS+2, "abcdefghijklmnopqrstuvwxyzyxwvutsrqponmlkjihgfedcba")

	// 3 CANDLE (first variety)
	g.gi.Configstring(CS_LIGHTS+3, "mmmmmaaaaammmmmaaaaaabcdefgabcdefg")

	// 4 FAST STROBE
	g.gi.Configstring(CS_LIGHTS+4, "mamamamamama")

	// 5 GENTLE PULSE 1
	g.gi.Configstring(CS_LIGHTS+5, "jklmnopqrstuvwxyzyxwvutsrqponmlkj")

	// 6 FLICKER (second variety)
	g.gi.Configstring(CS_LIGHTS+6, "nmonqnmomnmomomno")

	// 7 CANDLE (second variety)
	g.gi.Configstring(CS_LIGHTS+7, "mmmaaaabcdefgmmmmaaaammmaamm")

	// 8 CANDLE (third variety)
	g.gi.Configstring(CS_LIGHTS+8, "mmmaaammmaaammmabcdefaaaammmmabcdefmmmaaaa")

	// 9 SLOW STROBE (fourth variety)
	g.gi.Configstring(CS_LIGHTS+9, "aaaaaaaazzzzzzzz")

	// 10 FLUORESCENT FLICKER
	g.gi.Configstring(CS_LIGHTS+10, "mmamammmmammamamaaamammma")

	// 11 SLOW PULSE NOT FADE TO BLACK
	g.gi.Configstring(CS_LIGHTS+11, "abcdefghijklmnopqrrqponmlkjihgfedcba")

	// styles 32-62 are assigned by the light program for switchable lights

	// 63 testing
	g.gi.Configstring(CS_LIGHTS+63, "a")
}
