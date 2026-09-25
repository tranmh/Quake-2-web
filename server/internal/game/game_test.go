package game

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/testutil"
)

// TestRegistryComplete: every registered function is bound exactly once and
// every mmove references registered handles.
func TestRegistryComplete(t *testing.T) {
	if u := registryUnbound(); len(u) > 0 {
		t.Fatalf("registered but unbound functions: %v", u)
	}
	for name, m := range mmoves {
		if m.Name != name {
			t.Errorf("mmove %s registered as %s", m.Name, name)
		}
		for i, f := range m.Frame {
			if f.Aifunc != nil && regByName[f.Aifunc.Name()] == nil {
				t.Errorf("mmove %s frame %d: unregistered aifunc", name, i)
			}
			if f.Thinkfunc != nil && regByName[f.Thinkfunc.Name()] == nil {
				t.Errorf("mmove %s frame %d: unregistered thinkfunc", name, i)
			}
		}
	}
	if len(RegistryNames()) < 100 {
		t.Fatalf("registry suspiciously small: %d", len(RegistryNames()))
	}
}

// TestRegistryNamesAreCFunctions: every registry name is a function defined
// in the C game sources (so saves and fixtures name callbacks exactly).
func TestRegistryNamesAreCFunctions(t *testing.T) {
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Skip(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "Quake-2", "game", "*.c"))
	if len(files) == 0 {
		t.Skip("C sources not available")
	}
	defs := map[string]bool{}
	re := regexp.MustCompile(`(?m)^[A-Za-z_][A-Za-z0-9_ \t\*]*?\b([A-Za-z_][A-Za-z0-9_]*)\s*\([^;{}\n]*\)\s*\{`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			defs[m[1]] = true
		}
	}
	for _, n := range RegistryNames() {
		if !defs[n] {
			t.Errorf("registry name %q is not a C function definition in Quake-2/game", n)
		}
	}
}

// TestItemTableMatchesC compares itemlist with the itemlist[] initializer of
// game/g_items.c, field by field, in order.
func TestItemTableMatchesC(t *testing.T) {
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Skip(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "Quake-2", "game", "g_items.c"))
	if err != nil {
		t.Skip(err)
	}
	src := string(b)
	start := strings.Index(src, "gitem_t\titemlist[] = ")
	if start < 0 {
		t.Fatal("itemlist not found in g_items.c")
	}
	src = src[start:]
	src = src[strings.Index(src, "{")+1:]
	// strip comments
	src = regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	src = regexp.MustCompile(`//[^\n]*`).ReplaceAllString(src, "")
	var entries [][]string
	depth := 0
	cur := ""
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == '"' {
			j := i + 1
			for src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			cur += src[i : j+1]
			i = j
			continue
		}
		if c == '{' {
			depth++
			if depth == 1 {
				cur = ""
				continue
			}
		}
		if c == '}' {
			depth--
			if depth == 0 {
				var fields []string
				for _, f := range splitTopLevel(cur) {
					if f = strings.TrimSpace(f); f != "" {
						fields = append(fields, f)
					}
				}
				entries = append(entries, fields)
				continue
			}
			if depth < 0 {
				break
			}
		}
		if depth >= 1 {
			cur += string(c)
		}
	}
	// itemlist keeps the final {NULL} terminator like C (num_items = len-1)
	if len(entries) != len(itemlist) {
		t.Fatalf("C has %d entries (incl. terminator), Go itemlist has %d", len(entries), len(itemlist))
	}
	consts := map[string]int64{
		"0": 0, "EF_ROTATE": q2const.EF_ROTATE, "EF_GIB": q2const.EF_GIB,
		"IT_WEAPON": IT_WEAPON, "IT_AMMO": IT_AMMO, "IT_ARMOR": IT_ARMOR, "IT_STAY_COOP": IT_STAY_COOP,
		"IT_KEY": IT_KEY, "IT_POWERUP": IT_POWERUP,
		"WEAP_BLASTER": WEAP_BLASTER, "WEAP_SHOTGUN": WEAP_SHOTGUN, "WEAP_SUPERSHOTGUN": WEAP_SUPERSHOTGUN,
		"WEAP_MACHINEGUN": WEAP_MACHINEGUN, "WEAP_CHAINGUN": WEAP_CHAINGUN, "WEAP_GRENADES": WEAP_GRENADES,
		"WEAP_GRENADELAUNCHER": WEAP_GRENADELAUNCHER, "WEAP_ROCKETLAUNCHER": WEAP_ROCKETLAUNCHER,
		"WEAP_HYPERBLASTER": WEAP_HYPERBLASTER, "WEAP_RAILGUN": WEAP_RAILGUN, "WEAP_BFG": WEAP_BFG,
		"ARMOR_BODY": ARMOR_BODY, "ARMOR_COMBAT": ARMOR_COMBAT, "ARMOR_JACKET": ARMOR_JACKET,
		"ARMOR_SHARD": ARMOR_SHARD, "ARMOR_NONE": ARMOR_NONE,
		"AMMO_BULLETS": AMMO_BULLETS, "AMMO_SHELLS": AMMO_SHELLS, "AMMO_ROCKETS": AMMO_ROCKETS,
		"AMMO_GRENADES": AMMO_GRENADES, "AMMO_CELLS": AMMO_CELLS, "AMMO_SLUGS": AMMO_SLUGS,
		"POWER_ARMOR_SCREEN": POWER_ARMOR_SCREEN, "POWER_ARMOR_SHIELD": POWER_ARMOR_SHIELD,
	}
	evalInt := func(s string) int64 {
		v := int64(0)
		for _, p := range strings.Split(s, "|") {
			p = strings.TrimSpace(p)
			if n, err := strconv.ParseInt(p, 0, 64); err == nil {
				v |= n
				continue
			}
			c, ok := consts[p]
			if !ok {
				t.Fatalf("unknown constant %q in itemlist", p)
			}
			v |= c
		}
		return v
	}
	str := func(s string) string {
		if s == "NULL" {
			return ""
		}
		u, err := strconv.Unquote(s)
		if err != nil {
			t.Fatalf("bad string %q", s)
		}
		return u
	}
	fn := func(s string) string {
		if s == "NULL" {
			return ""
		}
		return s
	}
	for i := range itemlist {
		c := entries[i]
		for len(c) < 19 {
			c = append(c, "NULL")
		}
		it := &itemlist[i]
		if it.index != int32(i) || ITEM_INDEX(it) != int32(i) {
			t.Errorf("item %d: index %d", i, it.index)
		}
		info := ""
		if it.Info != nil {
			switch it.Info {
			case &jacketarmor_info:
				info = "&jacketarmor_info"
			case &combatarmor_info:
				info = "&combatarmor_info"
			case &bodyarmor_info:
				info = "&bodyarmor_info"
			default:
				info = "?"
			}
		}
		if i == 0 {
			info = ""
		}
		got := []string{it.Classname, it.Pickup.Name(), it.Use.Name(), it.Drop.Name(), it.Weaponthink.Name(),
			it.PickupSound, it.WorldModel, strconv.Itoa(int(it.WorldModelFlags)), it.ViewModel, it.Icon,
			it.PickupName, strconv.Itoa(int(it.CountWidth)), strconv.Itoa(int(it.Quantity)), it.Ammo,
			strconv.Itoa(int(it.Flags)), strconv.Itoa(int(it.Weapmodel)), info, strconv.Itoa(int(it.Tag)), it.Precaches}
		want := []string{str(c[0]), fn(c[1]), fn(c[2]), fn(c[3]), fn(c[4]), str(c[5]), str(c[6]),
			strconv.Itoa(int(evalInt(zeroIfNull(c[7])))), str(c[8]), str(c[9]), str(c[10]),
			strconv.Itoa(int(evalInt(zeroIfNull(c[11])))), strconv.Itoa(int(evalInt(zeroIfNull(c[12])))), str(c[13]),
			strconv.Itoa(int(evalInt(zeroIfNull(c[14])))), strconv.Itoa(int(evalInt(zeroIfNull(c[15])))),
			strings.TrimSpace(strings.ReplaceAll(c[16], "NULL", "")), strconv.Itoa(int(evalInt(zeroIfNull(c[17])))),
			strPrecache(t, c[18])}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("item %d differs:\n go: %q\n  C: %q", i, got, want)
		}
	}
}

func zeroIfNull(s string) string {
	if s == "NULL" {
		return "0"
	}
	return s
}

// strPrecache handles adjacent string literal concatenation.
func strPrecache(t *testing.T, s string) string {
	if s == "NULL" {
		return ""
	}
	out := ""
	for _, m := range regexp.MustCompile(`"(?:[^"\\]|\\.)*"`).FindAllString(s, -1) {
		u, err := strconv.Unquote(m)
		if err != nil {
			t.Fatalf("bad string %q", m)
		}
		out += u
	}
	return out
}

func splitTopLevel(s string) []string {
	var out []string
	cur := ""
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			cur += string(c)
			if c == '\\' && i+1 < len(s) {
				i++
				cur += string(s[i])
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			cur += string(c)
			continue
		}
		if c == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(c)
	}
	out = append(out, cur)
	return out
}

// TestSaveableTypes: every saved type is fully serializable (or explicitly transient).
func TestSaveableTypes(t *testing.T) {
	for _, v := range []any{gameFile{}, levelFile{}, Edict{}, GClient{}, levelExtras{}} {
		if err := saveable(reflect.TypeOf(v)); err != nil {
			t.Error(err)
		}
	}
}

// TestGameFieldsDispositioned: every field of Game is either saved or
// explicitly transient.
func TestGameFieldsDispositioned(t *testing.T) {
	disp := map[string]string{}
	for _, n := range gameFieldsSaved {
		disp[n] = "saved"
	}
	for _, n := range gameFieldsTransient {
		if disp[n] != "" {
			t.Errorf("field %s both saved and transient", n)
		}
		disp[n] = "transient"
	}
	gt := reflect.TypeOf(Game{})
	seen := map[string]bool{}
	for i := 0; i < gt.NumField(); i++ {
		n := gt.Field(i).Name
		seen[n] = true
		if disp[n] == "" {
			t.Errorf("Game.%s is neither saved nor transient (update g_save.go)", n)
		}
	}
	for n := range disp {
		if !seen[n] {
			t.Errorf("g_save.go lists unknown Game field %s", n)
		}
	}
}

// fillRandom sets every saveable field of v to a random value; pointers are
// set to members of g (edicts, clients, items, mmoves, handles of the right kind).
func fillRandom(g *Game, v reflect.Value, r *rand.Rand) {
	t := v.Type()
	switch {
	case t == typEdict:
		if r.Intn(3) == 0 {
			v.Set(reflect.Zero(t))
		} else {
			v.Set(reflect.ValueOf(&g.edicts[r.Intn(len(g.edicts))]))
		}
		return
	case t == typGClient:
		v.Set(reflect.ValueOf(&g.game.Clients[r.Intn(len(g.game.Clients))]))
		return
	case t == typGItem:
		v.Set(reflect.ValueOf(&itemlist[1+r.Intn(len(itemlist)-1)]))
		return
	case t == typMMove:
		for _, m := range mmoves {
			v.Set(reflect.ValueOf(m))
			break
		}
		return
	case t == typLink:
		return
	case t.Kind() == reflect.Pointer && t.Implements(typHandleI):
		var cands []reflect.Value
		for _, h := range regByName {
			hv := reflect.ValueOf(h)
			if hv.Type().AssignableTo(t) {
				cands = append(cands, hv)
			}
		}
		if len(cands) > 0 && r.Intn(4) != 0 {
			v.Set(cands[r.Intn(len(cands))])
		}
		return
	}
	switch t.Kind() {
	case reflect.Bool:
		v.SetBool(r.Intn(2) == 1)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(int64(int8(r.Uint32())))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(uint64(uint8(r.Uint32())))
	case reflect.Float32:
		v.SetFloat(float64(float32(r.NormFloat64() * 1000)))
	case reflect.Float64:
		v.SetFloat(r.NormFloat64())
	case reflect.String:
		v.SetString(strconv.Itoa(r.Intn(1000)) + "\\n\"x")
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			fillRandom(g, v.Index(i), r)
		}
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).Tag.Get("save") == "-" || !t.Field(i).IsExported() {
				continue
			}
			fillRandom(g, v.Field(i), r)
		}
	}
}

type nullImport struct{ Import }

func (nullImport) LinkEntity(*Edict)   {}
func (nullImport) UnlinkEntity(*Edict) {}

// TestSaveRoundTripSynthetic fills game/level/edicts/clients with random
// values, saves, loads into a fresh Game and compares everything.
func TestSaveRoundTripSynthetic(t *testing.T) {
	mk := func() *Game {
		g := New(nullImport{}, nil)
		g.maxclients = &cvar.Cvar{Name: "maxclients", String: "4", Value: 4}
		g.game.Maxclients = 4
		g.game.Maxentities = 64
		g.edicts = make([]Edict, 64)
		for i := range g.edicts {
			g.edicts[i].Index = i
		}
		g.game.Clients = make([]GClient, 4)
		for i := range g.game.Clients {
			g.game.Clients[i].Index = i
		}
		return g
	}
	a := mk()
	r := rand.New(rand.NewSource(1))
	fillRandom(a, reflect.ValueOf(&a.game).Elem(), r)
	a.game.Maxclients, a.game.Maxentities = 4, 64
	fillRandom(a, reflect.ValueOf(&a.level).Elem(), r)
	for i := range a.game.Clients {
		fillRandom(a, reflect.ValueOf(&a.game.Clients[i]).Elem(), r)
		a.game.Clients[i].Index = i
	}
	a.num_edicts = 40
	for i := range a.edicts[:40] {
		fillRandom(a, reflect.ValueOf(&a.edicts[i]).Elem(), r)
		a.edicts[i].Index = i
		a.edicts[i].Area = Link{}
		if i >= 1 && i <= 4 {
			a.edicts[i].Client = &a.game.Clients[i-1]
			a.edicts[i].Client.Pers.Connected = false
		}
		if a.edicts[i].Classname == "target_crosslevel_target" {
			a.edicts[i].Classname = "x"
		}
	}
	a.trail_head = 3
	a.windsound = 7

	gd, err := a.WriteGame(true)
	if err != nil {
		t.Fatal(err)
	}
	ld, err := a.WriteLevel()
	if err != nil {
		t.Fatal(err)
	}
	b := mk()
	if err := b.ReadGame(gd); err != nil {
		t.Fatal(err)
	}
	if err := b.ReadLevel(ld); err != nil {
		t.Fatal(err)
	}
	c := &saveCtx{g: a}
	cb := &saveCtx{g: b}
	ja, _ := c.enc(reflect.ValueOf(&a.level).Elem())
	jb, _ := cb.enc(reflect.ValueOf(&b.level).Elem())
	if !reflect.DeepEqual(ja, jb) {
		t.Errorf("level differs after round trip")
	}
	if a.game.Autosaved || !b.game.Autosaved {
		t.Errorf("autosaved flag: a=%v b=%v", a.game.Autosaved, b.game.Autosaved)
	}
	b.game.Autosaved = false
	if !reflect.DeepEqual(mustEnc(t, c, &a.game), mustEnc(t, cb, &b.game)) {
		t.Errorf("game differs after round trip")
	}
	for i := range a.game.Clients {
		if !reflect.DeepEqual(mustEnc(t, c, &a.game.Clients[i]), mustEnc(t, cb, &b.game.Clients[i])) {
			t.Errorf("client %d differs after round trip", i)
		}
	}
	for i := 0; i < 40; i++ {
		ea, eb := mustEnc(t, c, &a.edicts[i]), mustEnc(t, cb, &b.edicts[i])
		if !reflect.DeepEqual(ea, eb) {
			t.Errorf("edict %d differs after round trip", i)
		}
	}
	if b.num_edicts != 40 || b.windsound != 7 || b.trail_head != 3 {
		t.Errorf("extras: num_edicts %d windsound %d trail_head %d", b.num_edicts, b.windsound, b.trail_head)
	}
}

func mustEnc(t *testing.T, c *saveCtx, p any) any {
	j, err := c.enc(reflect.ValueOf(p).Elem())
	if err != nil {
		t.Fatal(err)
	}
	return j
}
