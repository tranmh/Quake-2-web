package game

// Replacement for game/g_save.c (ADR-0004): versioned JSON saves compressed
// with zstd. Pointers are stored as indices (edicts, clients), items by
// classname, mmove tables and functions by their C names (registry.go).
//
// The structures are walked by reflection: every field of the saved structs
// is serialized unless it is tagged `save:"-"` (transient). saveable() is
// checked by a unit test for every saved type, so a new field can not be
// forgotten silently.
//
// Deliberate differences from the C raw-struct format (documented in
// docs/PARITY.md by the coordinator): the level file stores every edict below
// num_edicts (not only inuse ones) and restores linkcount after relinking, so
// that save -> load -> continue is identical to an uninterrupted run. The C
// load-time semantics that are part of the game logic are kept: clients are
// marked unconnected and target_crosslevel_target thinks are rescheduled.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"

	"github.com/klauspost/compress/zstd"

	. "quake2web/server/internal/q2const"
)

// SaveVersion is the version of the game/level save format.
const SaveVersion = 1

const (
	saveMagicGame  = "q2web-game"
	saveMagicLevel = "q2web-level"
)

// isHandle marks registry handles for the save walker.
func (f *Fn[F]) isHandle() {}

type handleI interface {
	Name() string
	isHandle()
}

var (
	typEdict   = reflect.TypeOf((*Edict)(nil))
	typGClient = reflect.TypeOf((*GClient)(nil))
	typGItem   = reflect.TypeOf((*GItem)(nil))
	typMMove   = reflect.TypeOf((*MMove)(nil))
	typLink    = reflect.TypeOf(Link{})
	typHandleI = reflect.TypeOf((*handleI)(nil)).Elem()
)

// saveCtx resolves pointers while encoding/decoding.
type saveCtx struct {
	g *Game
}

// ---------------------------------------------------------------- encoding

func (c *saveCtx) enc(v reflect.Value) (any, error) {
	t := v.Type()
	switch {
	case t == typEdict:
		if v.IsNil() {
			return nil, nil
		}
		return v.Interface().(*Edict).Index, nil
	case t == typGClient:
		if v.IsNil() {
			return nil, nil
		}
		return v.Interface().(*GClient).Index, nil
	case t == typGItem:
		if v.IsNil() {
			return nil, nil
		}
		it := v.Interface().(*GItem)
		if it.Classname == "" {
			return map[string]any{"index": it.index}, nil
		}
		return it.Classname, nil
	case t == typMMove:
		if v.IsNil() {
			return nil, nil
		}
		return v.Interface().(*MMove).Name, nil
	case t.Implements(typHandleI) && t.Kind() == reflect.Pointer:
		if v.IsNil() {
			return nil, nil
		}
		return v.Interface().(handleI).Name(), nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		bits := 64
		if t.Kind() == reflect.Float32 {
			bits = 32
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			if bits == 32 {
				return map[string]any{"f32bits": math.Float32bits(float32(f))}, nil
			}
			return map[string]any{"f64bits": math.Float64bits(f)}, nil
		}
		return json.Number(strconv.FormatFloat(f, 'g', -1, bits)), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Array, reflect.Slice:
		if t.Kind() == reflect.Slice && v.IsNil() {
			return nil, nil
		}
		out := make([]any, v.Len())
		for i := range out {
			e, err := c.enc(v.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	case reflect.Struct:
		out := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.Tag.Get("save") == "-" || sf.Type == typLink {
				continue
			}
			if !sf.IsExported() {
				return nil, fmt.Errorf("save: unexported field %s.%s", t.Name(), sf.Name)
			}
			e, err := c.enc(v.Field(i))
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", t.Name(), sf.Name, err)
			}
			out[sf.Name] = e
		}
		return out, nil
	}
	return nil, fmt.Errorf("save: unsupported type %s", t)
}

// ---------------------------------------------------------------- decoding

func (c *saveCtx) dec(j any, v reflect.Value) error {
	t := v.Type()
	switch {
	case t == typEdict:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		n, err := jsonInt(j)
		if err != nil {
			return err
		}
		if n < 0 || int(n) >= len(c.g.edicts) {
			return fmt.Errorf("save: edict index %d out of range", n)
		}
		v.Set(reflect.ValueOf(&c.g.edicts[n]))
		return nil
	case t == typGClient:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		n, err := jsonInt(j)
		if err != nil {
			return err
		}
		if n < 0 || int(n) >= len(c.g.game.Clients) {
			return fmt.Errorf("save: client index %d out of range", n)
		}
		v.Set(reflect.ValueOf(&c.g.game.Clients[n]))
		return nil
	case t == typGItem:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		if m, ok := j.(map[string]any); ok {
			n, err := jsonInt(m["index"])
			if err != nil || n < 0 || int(n) >= len(c.g.itemlist) {
				return fmt.Errorf("save: bad item index %v", m["index"])
			}
			v.Set(reflect.ValueOf(&c.g.itemlist[n]))
			return nil
		}
		s, ok := j.(string)
		if !ok {
			return fmt.Errorf("save: item is not a string: %v", j)
		}
		it := c.g.FindItemByClassname(s)
		if it == nil {
			return fmt.Errorf("save: unknown item %q", s)
		}
		v.Set(reflect.ValueOf(it))
		return nil
	case t == typMMove:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		s, ok := j.(string)
		if !ok {
			return fmt.Errorf("save: mmove is not a string: %v", j)
		}
		m := mmoves[s]
		if m == nil {
			return fmt.Errorf("save: unknown mmove %q", s)
		}
		v.Set(reflect.ValueOf(m))
		return nil
	case t.Implements(typHandleI) && t.Kind() == reflect.Pointer:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		s, ok := j.(string)
		if !ok {
			return fmt.Errorf("save: function is not a string: %v", j)
		}
		h, found := regByName[s]
		if !found {
			return fmt.Errorf("save: unknown function %q", s)
		}
		hv := reflect.ValueOf(h)
		if !hv.Type().AssignableTo(t) {
			return fmt.Errorf("save: function %q has kind %s, field wants %s", s, regKind[s], t)
		}
		v.Set(hv)
		return nil
	}
	switch t.Kind() {
	case reflect.Bool:
		b, ok := j.(bool)
		if !ok {
			return fmt.Errorf("save: not a bool: %v", j)
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := jsonInt(j)
		if err != nil {
			return err
		}
		if v.OverflowInt(n) {
			return fmt.Errorf("save: %d overflows %s", n, t)
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		num, ok := j.(json.Number)
		if !ok {
			return fmt.Errorf("save: not a number: %v", j)
		}
		n, err := strconv.ParseUint(string(num), 10, 64)
		if err != nil {
			return err
		}
		if v.OverflowUint(n) {
			return fmt.Errorf("save: %d overflows %s", n, t)
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if m, ok := j.(map[string]any); ok {
			if b, ok := m["f32bits"]; ok {
				n, err := jsonInt(b)
				if err != nil {
					return err
				}
				v.SetFloat(float64(math.Float32frombits(uint32(n))))
				return nil
			}
			n, err := strconv.ParseUint(fmt.Sprint(m["f64bits"]), 10, 64)
			if err != nil {
				return err
			}
			v.SetFloat(math.Float64frombits(n))
			return nil
		}
		num, ok := j.(json.Number)
		if !ok {
			return fmt.Errorf("save: not a number: %v", j)
		}
		bits := 64
		if t.Kind() == reflect.Float32 {
			bits = 32
		}
		f, err := strconv.ParseFloat(string(num), bits)
		if err != nil {
			return err
		}
		v.SetFloat(f)
	case reflect.String:
		s, ok := j.(string)
		if !ok {
			return fmt.Errorf("save: not a string: %v", j)
		}
		v.SetString(s)
	case reflect.Array:
		a, ok := j.([]any)
		if !ok || len(a) != v.Len() {
			return fmt.Errorf("save: bad array for %s", t)
		}
		for i := range a {
			if err := c.dec(a[i], v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		if j == nil {
			v.Set(reflect.Zero(t))
			return nil
		}
		a, ok := j.([]any)
		if !ok {
			return fmt.Errorf("save: bad slice for %s", t)
		}
		s := reflect.MakeSlice(t, len(a), len(a))
		for i := range a {
			if err := c.dec(a[i], s.Index(i)); err != nil {
				return err
			}
		}
		v.Set(s)
	case reflect.Struct:
		m, ok := j.(map[string]any)
		if !ok {
			return fmt.Errorf("save: bad object for %s", t)
		}
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.Tag.Get("save") == "-" || sf.Type == typLink {
				continue
			}
			fj, present := m[sf.Name]
			if !present {
				return fmt.Errorf("save: missing field %s.%s", t.Name(), sf.Name)
			}
			if err := c.dec(fj, v.Field(i)); err != nil {
				return fmt.Errorf("%s.%s: %w", t.Name(), sf.Name, err)
			}
		}
	default:
		return fmt.Errorf("save: unsupported type %s", t)
	}
	return nil
}

func jsonInt(j any) (int64, error) {
	num, ok := j.(json.Number)
	if !ok {
		return 0, fmt.Errorf("save: not a number: %v", j)
	}
	return strconv.ParseInt(string(num), 10, 64)
}

// saveable reports an error if t contains a field the walker can not
// serialize and that is not tagged `save:"-"`.
func saveable(t reflect.Type) error {
	return saveableRec(t, map[reflect.Type]bool{}, t.String())
}

func saveableRec(t reflect.Type, seen map[reflect.Type]bool, path string) error {
	if seen[t] {
		return nil
	}
	seen[t] = true
	switch {
	case t == typEdict, t == typGClient, t == typGItem, t == typMMove, t == typLink:
		return nil
	case t.Kind() == reflect.Pointer && t.Implements(typHandleI):
		return nil
	}
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64, reflect.String:
		return nil
	case reflect.Array, reflect.Slice:
		return saveableRec(t.Elem(), seen, path+"[]")
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			sf := t.Field(i)
			if sf.Tag.Get("save") == "-" {
				continue
			}
			if !sf.IsExported() {
				return fmt.Errorf("%s.%s: unexported field must be tagged save:\"-\"", path, sf.Name)
			}
			if err := saveableRec(sf.Type, seen, path+"."+sf.Name); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("%s: type %s is not serializable; tag the field save:\"-\" if it is transient", path, t)
}

// ---------------------------------------------------------------- files

// gameFile is the content of a game save (C: server.ssv game part).
type gameFile struct {
	Magic   string
	Version int
	Game    GameLocals
	Clients []GClient
}

// edictRecord is one edict of a level file.
type edictRecord struct {
	N int
	E Edict
}

// levelExtras are Game globals that live across frames and are rebuilt by
// SpawnEntities in C (precache indices, player trail, monster statics).
type levelExtras struct {
	SmMeatIndex         int32
	SndFry              int32
	Windsound           int32
	JacketArmorIndex    int32
	CombatArmorIndex    int32
	BodyArmorIndex      int32
	PowerScreenIndex    int32
	PowerShieldIndex    int32
	QuadDropTimeoutHack int32
	Trail               [TRAIL_LENGTH]*Edict
	TrailHead           int32
	TrailActive         bool
	PDamageFeedbackI    int32
	PlayerDieI          int32
}

// levelFile is the content of a level save (C: <map>.sav).
type levelFile struct {
	Magic     string
	Version   int
	Level     LevelLocals
	NumEdicts int32
	Edicts    []edictRecord
	Extras    levelExtras
	MStatics  map[string]any `save:"-"` // encoded separately (per monster file struct)
}

// Game fields that are saved (by WriteGame / WriteLevel) or deliberately
// transient. A unit test checks that every Game field is in exactly one set.
var (
	gameFieldsSaved = []string{
		"game", "level", "edicts", "num_edicts",
		"sm_meat_index", "snd_fry", "windsound",
		"jacket_armor_index", "combat_armor_index", "body_armor_index",
		"power_screen_index", "power_shield_index", "quad_drop_timeout_hack",
		"trail", "trail_head", "trail_active", "mstatics",
		"P_DamageFeedback_i", "player_die_i",
	}
	gameFieldsTransient = []string{
		"gi", "rng", // engine side
		"st",           // only valid while spawning
		"meansOfDeath", // set before every use
		// cvars: registered again by InitGame
		"deathmatch", "coop", "dmflags", "skill", "fraglimit", "timelimit", "password",
		"spectator_password", "maxclients", "maxspectators", "maxentities", "g_select_empty",
		"dedicated", "filterban", "sv_maxvelocity", "sv_gravity", "sv_rollspeed", "sv_rollangle",
		"gun_x", "gun_y", "gun_z", "run_pitch", "run_roll", "bob_up", "bob_pitch", "bob_roll",
		"sv_cheats", "flood_msgs", "flood_persecond", "flood_waitdelay", "sv_maplist",
		// per-call scratch
		"enemy_vis", "enemy_infront", "enemy_range", "enemy_yaw",
		"pm_passent", "c_yes", "c_no", "is_quad", "is_silenced", "obstacle", "pushed", "pushed_p",
		"current_player", "current_client", "forward", "right", "up", "xyspeed", "bobmove",
		"bobcycle", "bobfracsin",
		// server lifetime, not part of a savegame (C keeps them in the dll across loads)
		"ipfilters", "numipfilters",
		// recursion guard (enterCall), zero between frames
		"callDepth",
		// rebuilt by SpawnItem during SpawnEntities (C mutates the item table)
		"itemDropCleared",
		// ctf module: cvars, module identity (chosen at Init) and the
		// g_ctf.c globals (ctfgame, menus), which the ctf fork's savegames
		// do not store either
		"capturelimit", "instantweap", "ctfmod", "moduleForced", "itemlist", "ctfg",
	}
)

func zstdCompress(b []byte) ([]byte, error) {
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	return enc.EncodeAll(b, nil), nil
}

// maxSaveSize bounds the decompressed size of a save blob. A real level
// save is about 1 MB for 200 edicts (~5 MB at MAX_EDICTS); without a bound a
// tampered blob of a few KB expands to gigabytes and the process is
// OOM-killed (docs/review/03-go-game.md G-20).
const maxSaveSize = 64 << 20

func zstdDecompress(b []byte) ([]byte, error) {
	dec, err := zstd.NewReader(nil, zstd.WithDecoderMaxMemory(maxSaveSize), zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(b, nil)
}

func (g *Game) marshalSave(v any) ([]byte, error) {
	c := &saveCtx{g: g}
	j, err := c.enc(reflect.ValueOf(v).Elem())
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return zstdCompress(raw)
}

func (g *Game) unmarshalSave(data []byte) (map[string]any, error) {
	raw, err := zstdDecompress(data)
	if err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var j any
	if err := d.Decode(&j); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}
	m, ok := j.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("save: not an object")
	}
	return m, nil
}

func checkHeader(m map[string]any, magic string) error {
	if m["Magic"] != magic {
		return fmt.Errorf("save: not a %s file", magic)
	}
	v, err := jsonInt(m["Version"])
	if err != nil {
		return err
	}
	if v != SaveVersion {
		return fmt.Errorf("save: unsupported version %d (want %d)", v, SaveVersion)
	}
	return nil
}

// WriteGame is called whenever the game goes to a new level, and when the
// user explicitly saves the game.
//
// Game information include cross level data, like multi level
// triggers, help computer info, and all client states.
// C: game/g_save.c:460 WriteGame
func (g *Game) WriteGame(autosave bool) (data []byte, err error) {
	defer guardErr(&err)
	if !autosave {
		g.SaveClientData()
	}

	g.game.Autosaved = autosave
	f := &gameFile{Magic: saveMagicGame, Version: SaveVersion, Game: g.game, Clients: g.game.Clients}
	data, err = g.marshalSave(f)
	g.game.Autosaved = false
	return data, err
}

// ReadGame restores a game written by WriteGame.
// C: game/g_save.c:487 ReadGame
func (g *Game) ReadGame(data []byte) (err error) {
	defer guardErr(&err)
	m, err := g.unmarshalSave(data)
	if err != nil {
		return err
	}
	if err := checkHeader(m, saveMagicGame); err != nil {
		return err
	}

	g.edicts = make([]Edict, g.game.Maxentities)
	for i := range g.edicts {
		g.edicts[i].Index = i
	}

	c := &saveCtx{g: g}
	var gl GameLocals
	if err := c.dec(m["Game"], reflect.ValueOf(&gl).Elem()); err != nil {
		return err
	}
	// the counts below size arrays and bound loops: a tampered or foreign
	// save must match this game (C trusts the file; the engine restores the
	// latched maxclients/maxentities cvars before ReadGame, so a consistent
	// save always passes)
	if err := g.validateGameLocals(&gl); err != nil {
		return err
	}
	cl, ok := m["Clients"].([]any)
	if !ok || len(cl) != int(gl.Maxclients) {
		return fmt.Errorf("save: client count mismatch")
	}
	g.game = gl
	g.game.Clients = make([]GClient, g.game.Maxclients)
	for i := range g.game.Clients {
		g.game.Clients[i].Index = i
	}
	for i := range cl {
		if err := c.dec(cl[i], reflect.ValueOf(&g.game.Clients[i]).Elem()); err != nil {
			return err
		}
		g.game.Clients[i].Index = i
		if err := g.validateClient(&g.game.Clients[i]); err != nil {
			return fmt.Errorf("client %d: %w", i, err)
		}
	}
	return nil
}

// validateGameLocals checks the game_locals_t counts of a save against this
// game instance (memory-safety check for untrusted saves, docs/review/03-go-game.md G-04).
func (g *Game) validateGameLocals(gl *GameLocals) error {
	if gl.Maxclients < 1 || gl.Maxclients > MAX_CLIENTS || gl.Maxclients != int32(g.maxclients.Value) {
		return fmt.Errorf("save: maxclients %d does not match the server (%g)", gl.Maxclients, g.maxclients.Value)
	}
	if int(gl.Maxentities) != len(g.edicts) || int(gl.Maxclients) >= len(g.edicts) {
		return fmt.Errorf("save: maxentities %d does not match the server (%d)", gl.Maxentities, len(g.edicts))
	}
	if int(gl.NumItems) != len(g.itemlist)-1 {
		return fmt.Errorf("save: num_items %d does not match the item table (%d)", gl.NumItems, len(g.itemlist)-1)
	}
	return nil
}

// validateClient checks the client fields that are used as indices
// (docs/review/03-go-game.md G-03).
func (g *Game) validateClient(cl *GClient) error {
	if sel := cl.Pers.SelectedItem; sel < -1 || int(sel) >= len(g.itemlist) {
		return fmt.Errorf("save: selected_item %d out of range", sel)
	}
	for _, p := range []*ClientPersistant{&cl.Pers, &cl.Resp.CoopRespawn} {
		for i := len(g.itemlist); i < MAX_ITEMS; i++ {
			if p.Inventory[i] != 0 {
				return fmt.Errorf("save: inventory[%d] set for a nonexistent item", i)
			}
		}
	}
	return nil
}

// WriteLevel saves the level locals and the edicts.
// C: game/g_save.c:628 WriteLevel
func (g *Game) WriteLevel() (data []byte, err error) {
	defer guardErr(&err)
	f := &levelFile{Magic: saveMagicLevel, Version: SaveVersion, Level: g.level, NumEdicts: g.num_edicts}
	for i := 0; i < int(g.num_edicts); i++ {
		f.Edicts = append(f.Edicts, edictRecord{N: i, E: g.edicts[i]})
	}
	f.Extras = levelExtras{
		SmMeatIndex: g.sm_meat_index, SndFry: g.snd_fry, Windsound: g.windsound,
		JacketArmorIndex: g.jacket_armor_index, CombatArmorIndex: g.combat_armor_index,
		BodyArmorIndex: g.body_armor_index, PowerScreenIndex: g.power_screen_index,
		PowerShieldIndex: g.power_shield_index, QuadDropTimeoutHack: g.quad_drop_timeout_hack,
		Trail: g.trail, TrailHead: g.trail_head, TrailActive: g.trail_active,
		PDamageFeedbackI: g.P_DamageFeedback_i, PlayerDieI: g.player_die_i,
	}
	c := &saveCtx{g: g}
	j, err := c.enc(reflect.ValueOf(f).Elem())
	if err != nil {
		return nil, err
	}
	// monster statics: file name -> struct
	ms := map[string]any{}
	keys := make([]string, 0, len(g.mstatics))
	for k := range g.mstatics {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e, err := c.enc(reflect.ValueOf(g.mstatics[k]).Elem())
		if err != nil {
			return nil, fmt.Errorf("mstatics %s: %w", k, err)
		}
		ms[k] = e
	}
	j.(map[string]any)["MStatics"] = ms
	raw, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return zstdCompress(raw)
}

// ReadLevel restores a level written by WriteLevel.
//
// SpawnEntities will allready have been called on the
// level the same way it was when the level was saved.
//
// That is necessary to get the baselines
// set up identically.
//
// The server will have cleared all of the world links before
// calling ReadLevel.
//
// No clients are connected yet.
// C: game/g_save.c:682 ReadLevel
func (g *Game) ReadLevel(data []byte) (err error) {
	defer guardErr(&err)
	m, err := g.unmarshalSave(data)
	if err != nil {
		return err
	}
	if err := checkHeader(m, saveMagicLevel); err != nil {
		return err
	}

	// wipe all the entities
	for i := range g.edicts {
		clearEdict(&g.edicts[i])
	}
	g.num_edicts = int32(g.maxclients.Value + 1)

	c := &saveCtx{g: g}
	var lvl LevelLocals
	if err := c.dec(m["Level"], reflect.ValueOf(&lvl).Elem()); err != nil {
		return err
	}
	g.level = lvl

	var ex levelExtras
	if err := c.dec(m["Extras"], reflect.ValueOf(&ex).Elem()); err != nil {
		return err
	}

	recs, ok := m["Edicts"].([]any)
	if !ok {
		return fmt.Errorf("save: no edicts")
	}
	type linkFix struct {
		ent       *Edict
		linkcount int32
	}
	var relink []linkFix
	for _, r := range recs {
		rm, ok := r.(map[string]any)
		if !ok {
			return fmt.Errorf("save: bad edict record")
		}
		n, err := jsonInt(rm["N"])
		if err != nil {
			return err
		}
		if n < 0 || int(n) >= len(g.edicts) {
			return fmt.Errorf("save: edict %d out of range", n)
		}
		if int32(n) >= g.num_edicts {
			g.num_edicts = int32(n) + 1
		}
		ent := &g.edicts[n]
		if err := c.dec(rm["E"], reflect.ValueOf(ent).Elem()); err != nil {
			return fmt.Errorf("edict %d: %w", n, err)
		}
		ent.Index = int(n)
		// let the server rebuild world links for this ent
		ent.Area = Link{}
		if ent.InUse {
			relink = append(relink, linkFix{ent, ent.LinkCount})
		}
	}
	numEdicts, err := jsonInt(m["NumEdicts"])
	if err != nil || numEdicts < 0 || numEdicts > int64(len(g.edicts)) {
		return fmt.Errorf("save: num_edicts %v out of range", m["NumEdicts"])
	}
	if int32(numEdicts) > g.num_edicts {
		g.num_edicts = int32(numEdicts)
	}
	// indices into fixed arrays (docs/review/03-go-game.md G-05)
	if g.level.BodyQue < 0 || g.level.BodyQue >= BODY_QUEUE_SIZE {
		return fmt.Errorf("save: body_que %d out of range", g.level.BodyQue)
	}
	if ex.TrailHead < 0 || ex.TrailHead >= TRAIL_LENGTH {
		return fmt.Errorf("save: trail_head %d out of range", ex.TrailHead)
	}
	if len(g.game.Clients) < int(g.maxclients.Value) || int(g.maxclients.Value) >= len(g.edicts) {
		return fmt.Errorf("save: level for %g clients, game has %d", g.maxclients.Value, len(g.game.Clients))
	}
	for _, r := range relink {
		g.gi.LinkEntity(r.ent)
		r.ent.LinkCount = r.linkcount
	}

	g.sm_meat_index, g.snd_fry, g.windsound = ex.SmMeatIndex, ex.SndFry, ex.Windsound
	g.jacket_armor_index, g.combat_armor_index, g.body_armor_index = ex.JacketArmorIndex, ex.CombatArmorIndex, ex.BodyArmorIndex
	g.power_screen_index, g.power_shield_index = ex.PowerScreenIndex, ex.PowerShieldIndex
	g.quad_drop_timeout_hack = ex.QuadDropTimeoutHack
	g.trail, g.trail_head, g.trail_active = ex.Trail, ex.TrailHead, ex.TrailActive
	g.P_DamageFeedback_i, g.player_die_i = ex.PDamageFeedbackI, ex.PlayerDieI

	if ms, ok := m["MStatics"].(map[string]any); ok {
		for k, v := range ms {
			newFn := monsterStaticsNew[k]
			if newFn == nil {
				return fmt.Errorf("save: unknown monster statics %q", k)
			}
			p := newFn()
			if err := c.dec(v, reflect.ValueOf(p).Elem()); err != nil {
				return fmt.Errorf("mstatics %s: %w", k, err)
			}
			g.mstatics[k] = p
		}
	}

	// mark all clients as unconnected
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		ent := &g.edicts[i+1]
		ent.Client = &g.game.Clients[i]
		ent.Client.Pers.Connected = false
	}

	// do any load time things at this point
	for i := 0; i < int(g.num_edicts); i++ {
		ent := &g.edicts[i]

		if !ent.InUse {
			continue
		}

		// fire any cross-level triggers
		if ent.Classname == "target_crosslevel_target" {
			ent.Nextthink = g.level.Time + ent.Delay
		}
	}
	return nil
}
