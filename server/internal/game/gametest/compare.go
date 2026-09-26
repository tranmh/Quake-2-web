package gametest

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"

	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// Options configures Run.
type Options struct {
	BaseDir     string      // contains baseq2/pak0.pak (default testutil.BaseDir())
	FixturesDir string      // fixtures/generated (default testutil.FixturesDir())
	MaxFrames   int         // stop after this frame (0 = all)
	NewGame     NewGameFunc // default game.New
	Log         func(string)
	MaxDiffs    int // diffs listed for the diverging frame (default 20)
	// MaskMonsters ignores monster edicts (fixture classname monster_*,
	// misc_insane, misc_actor, turret_driver, or SVF_MONSTER on either side),
	// the events whose "ent" is such an edict, and rand_calls. Used to measure
	// the non-monster progress of single-player scenarios while m_*.c files
	// are not ported.
	MaskMonsters bool
}

// Divergence describes the first frame that differs.
type Divergence struct {
	Frame   int
	Diffs   []string // "edict 12 .s.origin[0]: expected 1 actual 2", ...
	Context string   // short context (events of the frame, inputs)
}

// Result of a scenario run.
type Result struct {
	Scenario   string
	Frames     int // frames compared (including frame 0)
	Matched    int // frames that matched, counted from frame 0
	Divergence *Divergence
}

func (r *Result) String() string {
	if r.Divergence == nil {
		return fmt.Sprintf("%s: all %d frames match", r.Scenario, r.Frames)
	}
	d := r.Divergence
	var b strings.Builder
	fmt.Fprintf(&b, "%s: matched %d frames; first divergence at frame %d:\n", r.Scenario, r.Matched, d.Frame)
	for _, s := range d.Diffs {
		b.WriteString("  " + s + "\n")
	}
	if d.Context != "" {
		b.WriteString("  context: " + d.Context + "\n")
	}
	return b.String()
}

// Run executes scenario (a fixture base name, e.g. "demo1_dm4") and compares
// every frame with the fixture.
func Run(scenario string, opt Options) (res *Result, err error) {
	if opt.MaxDiffs == 0 {
		opt.MaxDiffs = 20
	}
	if opt.BaseDir == "" {
		opt.BaseDir = testutil.BaseDir()
	}
	if opt.FixturesDir == "" {
		opt.FixturesDir = testutil.FixturesDir()
	}
	res = &Result{Scenario: scenario}
	fr, err := OpenFixture(filepath.Join(opt.FixturesDir, "game", scenario+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer fr.Close()

	var sc *Scenario
	scPath := filepath.Join(opt.FixturesDir, "scenarios", "game", scenario+".json")
	if _, e := os.Stat(scPath); e == nil {
		sc, err = LoadScenario(scPath)
	} else {
		sc, err = ParseScenario(fr.RawHead)
	}
	if err != nil {
		return nil, err
	}
	if sc.Module != "baseq2" && sc.Module != "ctf" {
		return nil, fmt.Errorf("module %q not supported", sc.Module)
	}

	var s *Server
	frame := 0
	defer func() {
		if p := recover(); p != nil {
			msg := fmt.Sprint(p)
			if ce, ok := p.(shared.ComError); ok {
				msg = "Com_Error: " + ce.Msg
			}
			res.Divergence = &Divergence{Frame: frame, Diffs: []string{"panic: " + msg}, Context: string(debug.Stack())}
			err = nil
		}
	}()

	s, err = NewServer(sc, opt.BaseDir, opt.NewGame)
	if err != nil {
		return nil, err
	}
	s.E.Log = opt.Log
	if err := s.Start(); err != nil {
		return nil, err
	}
	for {
		exp, e := fr.Next()
		if e == io.EOF {
			return res, nil
		}
		if e != nil {
			return res, e
		}
		if exp.Frame != frame {
			return res, fmt.Errorf("fixture frame %d, expected %d", exp.Frame, frame)
		}
		if frame > 0 {
			if err := s.Frame(frame); err != nil {
				return res, err
			}
		}
		act := s.Dump(frame)
		res.Frames++
		if diffs := CompareFrameOpt(exp, act, opt.MaxDiffs, opt.MaskMonsters); len(diffs) > 0 {
			res.Divergence = &Divergence{Frame: frame, Diffs: diffs, Context: frameContext(exp, act)}
			return res, nil
		}
		res.Matched++
		s.EndFrame()
		frame++
		if opt.MaxFrames > 0 && frame > opt.MaxFrames {
			return res, nil
		}
	}
}

func frameContext(exp *FixtureFrame, act *FrameDump) string {
	e, _ := json.Marshal(exp.Events)
	a, _ := json.Marshal(act.Events)
	es, as := string(e), string(a)
	const lim = 3000
	if len(es) > lim {
		es = es[:lim] + "..."
	}
	if len(as) > lim {
		as = as[:lim] + "..."
	}
	return fmt.Sprintf("expected events %s\n  actual events %s", es, as)
}

// CompareFrame returns the differences (at most max) between a fixture frame
// and our dump, in the order edicts, clients, events, rand_calls, level, inputs.
func CompareFrame(exp *FixtureFrame, act *FrameDump, max int) []string {
	return CompareFrameOpt(exp, act, max, false)
}

// monsterClass reports whether a classname belongs to a monster file.
func monsterClass(c any) bool {
	s, _ := c.(string)
	return strings.HasPrefix(s, "monster_") || s == "misc_insane" || s == "misc_actor" || s == "turret_driver"
}

func svflagsMonster(r map[string]any) bool {
	switch v := r["svflags"].(type) {
	case int64:
		return v&4 != 0
	case json.Number:
		i, _ := v.Int64()
		return i&4 != 0
	}
	return false
}

// maskedEdicts returns the edict numbers ignored by the monster mask.
func maskedEdicts(exp *FixtureFrame, act *FrameDump) map[int]bool {
	m := map[int]bool{}
	for n, r := range exp.Edicts {
		if monsterClass(r["classname"]) || svflagsMonster(r) {
			m[n] = true
		}
	}
	for n, r := range act.Edicts {
		if monsterClass(r["classname"]) || svflagsMonster(r) {
			m[n] = true
		}
	}
	return m
}

func filterEvents(evs []any, masked map[int]bool) []any {
	out := make([]any, 0, len(evs))
	for _, ev := range evs {
		m, _ := ev.(map[string]any)
		if m != nil {
			var n int
			ok := false
			switch v := m["ent"].(type) {
			case int64:
				n, ok = int(v), true
			case json.Number:
				n, ok = numInt(v)
			}
			if ok && masked[n] {
				continue
			}
		}
		out = append(out, ev)
	}
	return out
}

// CompareFrameOpt is CompareFrame with the optional monster mask.
func CompareFrameOpt(exp *FixtureFrame, act *FrameDump, max int, maskMonsters bool) []string {
	var diffs []string
	var masked map[int]bool
	expEvents, actEvents := exp.Events, act.Events
	if maskMonsters {
		masked = maskedEdicts(exp, act)
		expEvents = filterEvents(exp.Events, masked)
		actEvents = filterEvents(act.Events, masked)
	}
	add := func(s string) bool {
		diffs = append(diffs, s)
		return len(diffs) >= max
	}

	// edicts
	ns := map[int]bool{}
	for n := range exp.Edicts {
		ns[n] = true
	}
	for n := range act.Edicts {
		ns[n] = true
	}
	order := make([]int, 0, len(ns))
	for n := range ns {
		order = append(order, n)
	}
	sort.Ints(order)
	for _, n := range order {
		if masked[n] {
			continue
		}
		er, eok := exp.Edicts[n]
		ar, aok := act.Edicts[n]
		switch {
		case !eok:
			if add(fmt.Sprintf("edict %d: unexpected inuse edict (classname %v)", n, ar["classname"])) {
				return diffs
			}
		case !aok:
			if add(fmt.Sprintf("edict %d: missing (expected classname %v)", n, er["classname"])) {
				return diffs
			}
		default:
			for _, d := range cmpVal(fmt.Sprintf("edict %d (%v)", n, er["classname"]), er, ar) {
				if add(d) {
					return diffs
				}
			}
		}
	}
	for _, d := range cmpVal("clients", exp.Clients, act.Clients) {
		if add(d) {
			return diffs
		}
	}
	for _, d := range cmpVal("events", expEvents, actEvents) {
		if add(d) {
			return diffs
		}
	}
	if !maskMonsters && exp.RandCalls != act.RandCalls {
		if add(fmt.Sprintf("rand_calls: expected %d actual %d", exp.RandCalls, act.RandCalls)) {
			return diffs
		}
	}
	expLevel, actLevel := exp.Level, act.Level
	if maskMonsters {
		expLevel, actLevel = dropKeys(expLevel), dropKeys(actLevel)
	}
	for _, d := range cmpVal("level", expLevel, actLevel) {
		if add(d) {
			return diffs
		}
	}
	for _, d := range cmpVal("inputs", exp.Inputs, act.Inputs) {
		if add(d) {
			return diffs
		}
	}
	return diffs
}

// latin1 turns a decoded JSON string back into the C byte string (the oracle
// escapes bytes >= 0x7f as \u00XX).
func latin1(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 256 {
			b = append(b, byte(r))
		} else {
			b = append(b, '?')
		}
	}
	return string(b)
}

func show(v any) string {
	switch x := v.(type) {
	case float32:
		return strconv.FormatFloat(float64(x), 'g', 9, 32) + fmt.Sprintf(" (0x%08x)", math.Float32bits(x))
	case string:
		return strconv.Quote(x)
	case nil:
		return "null"
	case json.Number:
		return string(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	s := string(b)
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// cmpVal compares an expected JSON value (json.Number, string, nil, []any,
// map[string]any) with our value (float32, int64, string, nil, []any, map).
func cmpVal(path string, exp, act any) []string {
	bad := func() []string {
		return []string{fmt.Sprintf("%s: expected %s actual %s", path, show(exp), show(act))}
	}
	switch a := act.(type) {
	case float32:
		n, ok := exp.(json.Number)
		if !ok {
			return bad()
		}
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil || math.Float32bits(float32(f)) != math.Float32bits(a) {
			return []string{fmt.Sprintf("%s: expected %s (0x%08x) actual %s", path, string(n),
				math.Float32bits(float32(f)), show(a))}
		}
		return nil
	case int64:
		n, ok := exp.(json.Number)
		if !ok {
			return bad()
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			f, ferr := n.Float64()
			if ferr != nil {
				return bad()
			}
			i = int64(f)
		}
		if i != a {
			return bad()
		}
		return nil
	case string:
		e, ok := exp.(string)
		if !ok || latin1(e) != a {
			return bad()
		}
		return nil
	case nil:
		if exp != nil {
			return bad()
		}
		return nil
	case []any:
		e, ok := exp.([]any)
		if !ok {
			return bad()
		}
		var out []string
		n := len(e)
		if len(a) > n {
			n = len(a)
		}
		for i := 0; i < n; i++ {
			p := fmt.Sprintf("%s[%d]", path, i)
			switch {
			case i >= len(e):
				out = append(out, fmt.Sprintf("%s: unexpected %s", p, show(a[i])))
			case i >= len(a):
				out = append(out, fmt.Sprintf("%s: missing, expected %s", p, show(e[i])))
			default:
				out = append(out, cmpVal(p, e[i], a[i])...)
			}
			if len(out) > 50 {
				break
			}
		}
		return out
	case map[string]any:
		e, ok := exp.(map[string]any)
		if !ok {
			return bad()
		}
		keys := map[string]bool{}
		for k := range e {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		ks := make([]string, 0, len(keys))
		for k := range keys {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		var out []string
		for _, k := range ks {
			p := path + "." + k
			ev, eok := e[k]
			av, aok := a[k]
			switch {
			case !eok:
				out = append(out, fmt.Sprintf("%s: unexpected key (actual %s)", p, show(av)))
			case !aok:
				out = append(out, fmt.Sprintf("%s: missing key (expected %s)", p, show(ev)))
			default:
				out = append(out, cmpVal(p, ev, av)...)
			}
		}
		return out
	}
	return []string{fmt.Sprintf("%s: unsupported actual type %T", path, act)}
}

// dropKeys removes the monster counters from a level record.
func dropKeys(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if k != "total_monsters" && k != "killed_monsters" {
			out[k] = v
		}
	}
	return out
}
