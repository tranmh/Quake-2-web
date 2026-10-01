package mapdata

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"quake2web/server/internal/qcommon/shared"
)

// Spawnflags removed by SpawnEntities before the spawn function runs.
// C: game/g_local.h:50 SPAWNFLAG_NOT_EASY
const (
	SpawnflagNotEasy       = 0x00000100
	SpawnflagNotMedium     = 0x00000200
	SpawnflagNotHard       = 0x00000400
	SpawnflagNotDeathmatch = 0x00000800
	SpawnflagNotCoop       = 0x00001000

	inhibitMask = SpawnflagNotEasy | SpawnflagNotMedium | SpawnflagNotHard | SpawnflagNotCoop | SpawnflagNotDeathmatch
)

// Entity is one entity of the lump with the spawn fields the game reads
// (fields[] of g_save.c plus the spawn_temp_t keys lip/distance/height/item),
// parsed with the same conversions as ED_ParseField. A string field is ""
// when the key is absent or empty: like the Go game port, an empty value
// counts as the C NULL in the spawn functions' checks.
type Entity struct {
	// Index is the position in the entity lump (0 is worldspawn). It is not
	// the edict number: inhibited entities give their slot to the next one.
	Index     int
	Classname string
	// Keys holds every key of the lump entity, lower-cased (ED_ParseField
	// matches case-insensitively), with its raw value; a repeated key keeps
	// the last value like the game does. Keys starting with '_' are kept here
	// although the game discards them.
	Keys map[string]string

	// Origin and Angles are s.origin / s.angles as parsed: "angle" sets
	// {0, angle, 0}, and of "angle" and "angles" the later key wins.
	Origin, Angles Vec3
	// Spawnflags is what the spawn function sees (the skill/deathmatch/coop
	// inhibit bits cleared); RawSpawnflags is the lump value.
	Spawnflags, RawSpawnflags int32

	Model                                              string
	Targetname, Target, Killtarget, Deathtarget        string
	Combattarget, Pathtarget, Team, Message, Map, Item string
	Health, Dmg, Count, Sounds, Style, Mass            int32
	Wait, Delay, Speed, Accel, Decel, Random           float32
	Lip, Distance, Height                              int32 // spawn_temp_t (st.lip, st.distance, st.height)
	hasGravityKey                                      bool  // st.gravity != NULL (trigger_gravity)

	// Inhibited is set when SpawnEntities frees the entity for the skill or
	// deathmatch setting, before any spawn function runs.
	Inhibited bool
	// Freed is set when the entity's own spawn function frees it at once
	// (info_null, untargeted lights and path_corners, a target_changelevel
	// without a map, deathmatch-only removals, ...). See spawnFrees.
	Freed bool
}

// Present reports whether the entity exists in the game after spawning.
func (e *Entity) Present() bool { return !e.Inhibited && !e.Freed }

// Brush reports whether the entity uses an inline BSP model ("*N").
func (e *Entity) Brush() bool { return len(e.Model) > 1 && e.Model[0] == '*' }

// String identifies the entity for messages: "#582 func_door *31 (t4)".
func (e *Entity) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s", e.Index, e.Classname)
	if e.Brush() {
		b.WriteString(" " + e.Model)
	}
	if e.Targetname != "" {
		b.WriteString(" (" + e.Targetname + ")")
	}
	return b.String()
}

// parseEntities splits the entity string into lump entities exactly like
// the SpawnEntities / ED_ParseEdict loop, returning an error where the game
// calls gi.error.
// C: game/g_spawn.c:520 SpawnEntities
func parseEntities(data string) ([]Entity, error) {
	var out []Entity
	for {
		// parse the opening brace
		tok, rest, more := shared.COM_Parse(data)
		if !more {
			break
		}
		if len(tok) == 0 || tok[0] != '{' {
			return nil, fmt.Errorf("ED_LoadFromFile: found %s when expecting {", tok)
		}
		e := Entity{Index: len(out)}
		var err error
		data, err = parseEdict(rest, &e)
		if err != nil {
			return nil, fmt.Errorf("entity %d: %w", e.Index, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// parseEdict parses the key/value pairs of one entity up to its closing
// brace and returns the remaining data. An entity without any pair is
// memset to zero by the game, which also clears inuse: it is marked Freed.
// C: game/g_spawn.c:414 ED_ParseEdict
func parseEdict(data string, e *Entity) (string, error) {
	e.Keys = map[string]string{}
	parsed := false
	defer func() {
		if !parsed {
			e.Freed = true
		}
	}()
	for {
		// parse key
		key, rest, more := shared.COM_Parse(data)
		data = rest
		if len(key) > 0 && key[0] == '}' {
			break
		}
		if !more {
			return data, fmt.Errorf("ED_ParseEntity: EOF without closing brace")
		}
		if len(key) > 255 {
			key = key[:255]
		}

		// parse value
		value, rest, more := shared.COM_Parse(data)
		data = rest
		if !more {
			return data, fmt.Errorf("ED_ParseEntity: EOF without closing brace")
		}
		if len(value) > 0 && value[0] == '}' {
			return data, fmt.Errorf("ED_ParseEntity: closing brace without data")
		}

		parsed = true
		e.Keys[asciiLower(key)] = value

		// keynames with a leading underscore are used for utility comments,
		// and are immediately discarded by quake
		if len(key) > 0 && key[0] == '_' {
			continue
		}
		e.parseField(key, value)
	}
	return data, nil
}

// parseField stores one spawn key like ED_ParseField with the fields[] and
// spawn_temp_t conversions (F_LSTRING through ED_NewString, F_INT atoi,
// F_FLOAT atof, F_VECTOR sscanf "%f %f %f", F_ANGLEHACK). Keys the game does
// not know are ignored.
// C: game/g_spawn.c:358 ED_ParseField
func (e *Entity) parseField(key, value string) {
	switch asciiLower(key) {
	case "classname":
		e.Classname = edNewString(value)
	case "model":
		e.Model = edNewString(value)
	case "spawnflags":
		e.Spawnflags = shared.Atoi(value)
	case "speed":
		e.Speed = float32(shared.Atof(value))
	case "accel":
		e.Accel = float32(shared.Atof(value))
	case "decel":
		e.Decel = float32(shared.Atof(value))
	case "target":
		e.Target = edNewString(value)
	case "targetname":
		e.Targetname = edNewString(value)
	case "pathtarget":
		e.Pathtarget = edNewString(value)
	case "deathtarget":
		e.Deathtarget = edNewString(value)
	case "killtarget":
		e.Killtarget = edNewString(value)
	case "combattarget":
		e.Combattarget = edNewString(value)
	case "message":
		e.Message = edNewString(value)
	case "team":
		e.Team = edNewString(value)
	case "wait":
		e.Wait = float32(shared.Atof(value))
	case "delay":
		e.Delay = float32(shared.Atof(value))
	case "random":
		e.Random = float32(shared.Atof(value))
	case "count":
		e.Count = shared.Atoi(value)
	case "health":
		e.Health = shared.Atoi(value)
	case "sounds":
		e.Sounds = shared.Atoi(value)
	case "style":
		e.Style = shared.Atoi(value)
	case "dmg":
		e.Dmg = shared.Atoi(value)
	case "mass":
		e.Mass = shared.Atoi(value)
	case "map":
		e.Map = edNewString(value)
	case "origin":
		e.Origin = sscanfVec(value)
	case "angles":
		e.Angles = sscanfVec(value)
	case "angle":
		e.Angles = Vec3{0, float32(shared.Atof(value)), 0}
	case "lip":
		e.Lip = shared.Atoi(value)
	case "distance":
		e.Distance = shared.Atoi(value)
	case "height":
		e.Height = shared.Atoi(value)
	case "item":
		e.Item = edNewString(value)
	case "gravity":
		e.hasGravityKey = edNewString(value) != ""
	}
}

// edNewString turns "\n" escapes into newlines and any other backslash pair
// into a single backslash.
// C: game/g_spawn.c:319 ED_NewString
func edNewString(s string) string {
	if strings.IndexByte(s, '\\') < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i < len(s)-1 {
			i++
			if s[i] == 'n' {
				b = append(b, '\n')
			} else {
				b = append(b, '\\')
			}
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

// sscanfVec emulates glibc sscanf(value, "%f %f %f", ...) into a zeroed
// vector: each conversion skips white space and takes the longest strtof
// prefix; scanning stops at the first failed conversion.
// C: game/g_spawn.c:379 ED_ParseField (F_VECTOR)
func sscanfVec(s string) Vec3 {
	var v Vec3
	for i := 0; i < 3; i++ {
		f, n, ok := strtof(s)
		if !ok {
			break
		}
		v[i] = f
		s = s[n:]
	}
	return v
}

// strtof converts the longest valid prefix of s (after leading white space)
// like glibc strtof, rounding once to float. It returns the number of bytes
// consumed.
func strtof(s string) (float32, int, bool) {
	d, n := shared.Strtod(s)
	if n == 0 {
		return 0, 0, false
	}
	j := 0
	for j < n && (s[j] == ' ' || (s[j] >= '\t' && s[j] <= '\r')) {
		j++
	}
	prefix := s[j:n]
	if strings.Trim(prefix, "0123456789.+-eE") == "" {
		// a plain decimal literal: parse it directly as float (one rounding)
		if f, err := strconv.ParseFloat(prefix, 32); err == nil || math.IsInf(f, 0) || f == 0 {
			return float32(f), n, true
		}
	}
	return float32(d), n, true
}

// asciiLower lower-cases A-Z only (C tolower in the C locale), leaving any
// other byte untouched.
func asciiLower(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 'A' && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if b[j] >= 'A' && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// inhibited applies the SpawnEntities skill / deathmatch filter. The coop
// test is commented out in the original, so coop never inhibits.
// C: game/g_spawn.c:576 SpawnEntities (inhibit)
func inhibited(spawnflags int32, skill int, deathmatch bool) bool {
	if deathmatch {
		return spawnflags&SpawnflagNotDeathmatch != 0
	}
	return (skill == 0 && spawnflags&SpawnflagNotEasy != 0) ||
		(skill == 1 && spawnflags&SpawnflagNotMedium != 0) ||
		((skill == 2 || skill == 3) && spawnflags&SpawnflagNotHard != 0)
}

// spawnFrees reports whether the spawn function of e frees the entity
// immediately (before the first frame). Item removals that depend on
// dmflags in deathmatch are not modelled.
func spawnFrees(e *Entity, deathmatch, coop bool) bool {
	c := e.Classname
	switch c {
	case "info_null", "func_group": // C: game/g_misc.c:519 SP_info_null
		return true
	case "info_player_coop": // C: game/p_client.c:137 SP_info_player_coop
		return !coop
	case "info_player_deathmatch": // C: game/p_client.c:123 SP_info_player_deathmatch
		return !deathmatch
	case "light": // C: game/g_misc.c:559 SP_light
		return e.Targetname == "" || deathmatch
	case "path_corner": // C: game/g_misc.c:399 SP_path_corner
		return e.Targetname == ""
	case "trigger_gravity": // C: game/g_trigger.c:537 SP_trigger_gravity
		return !e.hasGravityKey
	case "target_changelevel": // C: game/g_target.c:302 SP_target_changelevel
		return e.Map == ""
	case "target_help": // C: game/g_target.c:131 SP_target_help
		return deathmatch || e.Message == ""
	case "target_lightramp": // C: game/g_target.c:715 SP_target_lightramp
		m := e.Message
		if len(m) != 2 || m[0] < 'a' || m[0] > 'z' || m[1] < 'a' || m[1] > 'z' || m[0] == m[1] {
			return true
		}
		return deathmatch || e.Target == ""
	case "misc_viper", "misc_strogg_ship", "func_clock", "misc_teleporter":
		// C: game/g_misc.c:1830 SP_misc_teleporter (and the others: no target)
		return e.Target == ""
	case "misc_actor": // C: game/m_actor.c:423 SP_misc_actor
		return deathmatch || e.Targetname == "" || e.Target == ""
	case "target_secret", "target_goal", "func_explosive", "misc_explobox",
		"misc_deadsoldier", "point_combat", "turret_driver", "misc_insane":
		// C: game/g_misc.c:823 SP_func_explosive (auto-remove for deathmatch)
		return deathmatch
	}
	if strings.HasPrefix(c, "monster_") && c != "monster_commander_body" {
		// C: game/g_monster.c:534 monster_start (and every SP_monster_*)
		return deathmatch
	}
	return false
}
