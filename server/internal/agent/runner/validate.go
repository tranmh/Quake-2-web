package runner

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/demo"
)

// ValidateOptions configures Validate.
type ValidateOptions struct {
	// MinModelShare, when > 0, enforces the provenance gate with this
	// threshold (MaxStaleRate: the run's, or the default).
	MinModelShare, MaxStaleRate float64
}

// DemoCheck is one validated demo file.
type DemoCheck struct {
	File  string     `json:"file"`
	Stats demo.Stats `json:"stats"`
	Err   string     `json:"error,omitempty"`
}

// ValidateReport is what Validate found.
type ValidateReport struct {
	Dir      string             `json:"dir"`
	Run      string             `json:"run"`
	Episodes int                `json:"episodes"`
	Events   int                `json:"events"`
	Types    map[string]int     `json:"types"`
	Demos    []DemoCheck        `json:"demos"`
	Summary  metrics.RunSummary `json:"-"`
	Gate     *metrics.Gate      `json:"gate,omitempty"`
	Errors   []string           `json:"errors,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
}

// OK reports whether the run is valid.
func (r *ValidateReport) OK() bool { return len(r.Errors) == 0 }

func (r *ValidateReport) errorf(format string, args ...any) {
	if len(r.Errors) < 50 {
		r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
	} else if len(r.Errors) == 50 {
		r.Errors = append(r.Errors, "... (more errors)")
	}
}

func (r *ValidateReport) warnf(format string, args ...any) {
	if len(r.Warnings) < 50 {
		r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
	}
}

// knownTypes are the event types of the trace schema (q2bot.trace/1 and
// the runner's additions).
var knownTypes = map[string]bool{
	trace.TypeRunStart: true, trace.TypeEpisodeStart: true, trace.TypeLevelStart: true, trace.TypeLevelEnd: true,
	trace.TypeDecision: true, trace.TypeAPICall: true, trace.TypeDamage: true, trace.TypeKill: true, trace.TypeDeath: true,
	trace.TypeReload: true, trace.TypeStuck: true, trace.TypeBudget: true, trace.TypeError: true, trace.TypeEpisodeEnd: true,
	trace.TypeRunEnd: true, trace.TypeCmds: true, trace.TypeProvenance: true,
}

var levelOutcomes = map[string]bool{
	trace.OutcomeExit: true, trace.OutcomeVictory: true, trace.OutcomeDeathLimit: true, trace.OutcomeTimeout: true,
	trace.OutcomeStalled: true, trace.OutcomeAborted: true, trace.OutcomeError: true,
}

var episodeOutcomes = map[string]bool{campaign.OutcomeCompleted: true, campaign.OutcomeFailed: true, campaign.OutcomeAborted: true}

// Validate checks run directory dir: run.json parses and matches what its
// traces recompute to; every trace event has a known type, a body of its
// type's shape and the run's envelope (version, run id, episode, seq
// order), with each episode's start, levels and end in order; every
// recorded .dm2 plays to its end (demo.Validate); and, with
// opt.MinModelShare, the provenance gate holds. The error is for a
// directory that cannot be read at all; what is wrong with the run is in
// the report.
func Validate(dir string, opt ValidateOptions) (*ValidateReport, error) {
	rep := &ValidateReport{Dir: dir, Types: map[string]int{}}
	files, err := EpisodeTraces(dir)
	if err != nil {
		return nil, err
	}
	rep.Episodes = len(files)

	var runJSON *metrics.RunSummary
	if b, err := os.ReadFile(filepath.Join(dir, RunFile)); err != nil {
		rep.errorf("%s: %v", RunFile, err)
	} else {
		var s metrics.RunSummary
		if err := json.Unmarshal(b, &s); err != nil {
			rep.errorf("%s: %v", RunFile, err)
		} else {
			if s.Schema != metrics.Schema {
				rep.errorf("%s: schema %q, want %q", RunFile, s.Schema, metrics.Schema)
			}
			runJSON = &s
			rep.Run = s.Run
		}
	}

	record := false
	var all []trace.Event
	seen := map[uint64]bool{}
	lastSeq := uint64(0)
	for fi, f := range files {
		evs, err := trace.ReadFile(f.Path)
		if err != nil {
			rep.errorf("%s: %v", rel(dir, f.Path), err)
		}
		ev := &episodeValidator{rep: rep, ep: f.Episode, file: rel(dir, f.Path), openLvl: -1, lastLvl: -1}
		for i := range evs {
			e := &evs[i]
			if fi > 0 && i == 0 && e.Type == trace.TypeRunStart && seen[e.Seq] {
				continue // the run's run_start, repeated at the head of every episode
			}
			if e.Seq <= lastSeq {
				rep.errorf("%s: event %d (%s) has seq %d after %d", ev.file, i, e.Type, e.Seq, lastSeq)
			}
			lastSeq = max(lastSeq, e.Seq)
			if seen[e.Seq] {
				continue
			}
			seen[e.Seq] = true
			all = append(all, *e)
			rep.Events++
			rep.Types[e.Type]++
			if rep.Run == "" {
				rep.Run = e.Run
			}
			if e.Run != rep.Run {
				rep.errorf("%s: event %d (seq %d) of run %q, not %q", ev.file, i, e.Seq, e.Run, rep.Run)
			}
			if e.Type == trace.TypeRunStart {
				var rs trace.RunStart
				if e.DecodeBody(&rs) == nil && rs.Config[keyRecord] == "true" {
					record = true
				}
			}
			ev.check(e)
		}
		ev.finish(fi == len(files)-1)
		checkDemos(rep, dir, f.Episode, record && ev.levels > 0)
	}
	if len(all) > 0 && all[0].Type != trace.TypeRunStart {
		rep.errorf("the run's first event is %s, not run_start", all[0].Type)
	}

	s, err := Summarize(all, SummarizeOptions{})
	if err != nil {
		rep.errorf("summary: %v", err)
	}
	rep.Summary = s
	if runJSON != nil {
		if runJSON.Config == nil && s.Config != nil {
			// a run.json written before the config section existed: the
			// rest must still match
			rep.warnf("%s has no config section (written before it existed; q2bot summarize adds it)", RunFile)
			s.Config = nil
		}
		want, _ := trace.Canonical(s)
		got, _ := trace.Canonical(runJSON)
		if !bytes.Equal(want, got) {
			rep.errorf("%s does not match its traces (q2bot summarize rewrites it)", RunFile)
		}
	}
	if opt.MinModelShare > 0 {
		g, _ := Summarize(all, SummarizeOptions{MinModelShare: opt.MinModelShare, MaxStaleRate: opt.MaxStaleRate})
		rep.Gate = g.Gate
		if g.Gate != nil && !g.Gate.Passed {
			rep.errorf("provenance gate not met: %s", strings.Join(g.Gate.Reasons, "; "))
		}
	}
	return rep, nil
}

func rel(dir, p string) string {
	if r, err := filepath.Rel(dir, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

// checkDemos validates an episode's demo files.
func checkDemos(rep *ValidateReport, dir string, ep int, want bool) {
	paths, _ := filepath.Glob(filepath.Join(dir, EpisodeDir(ep), DemoDir, "*.dm2"))
	sort.Strings(paths)
	if want && len(paths) == 0 {
		rep.errorf("%s: the run records demos, but there are none", EpisodeDir(ep))
	}
	for _, p := range paths {
		dc := DemoCheck{File: rel(dir, p)}
		data, err := os.ReadFile(p)
		if err == nil {
			dc.Stats, err = demo.Validate(data)
		}
		if err != nil {
			dc.Err = err.Error()
			rep.errorf("%s: %v", dc.File, err)
		}
		rep.Demos = append(rep.Demos, dc)
	}
}

// episodeValidator checks the events of one episode's trace file.
type episodeValidator struct {
	rep     *ValidateReport
	ep      int
	file    string
	started bool
	ended   bool
	openLvl int // -1: no level open
	levels  int
	lastLvl int
	runEnd  bool
	// acted counts the lane tick events' fields; prov is the episode's
	// provenance event (cross-checked against them in finish)
	acted tickCounts
	prov  *trace.Provenance
}

func (v *episodeValidator) errorf(e *trace.Event, format string, args ...any) {
	v.rep.errorf("%s: seq %d %s: %s", v.file, e.Seq, e.Type, fmt.Sprintf(format, args...))
}

func (v *episodeValidator) check(e *trace.Event) {
	if e.V != trace.Version {
		v.errorf(e, "version %d, want %d", e.V, trace.Version)
	}
	if !knownTypes[e.Type] {
		v.rep.warnf("%s: seq %d: unknown event type %q", v.file, e.Seq, e.Type)
		return
	}
	runLevel := e.Type == trace.TypeRunStart || e.Type == trace.TypeRunEnd
	if !runLevel && e.Ep != v.ep {
		v.errorf(e, "episode %d in the trace of episode %d", e.Ep, v.ep)
	}
	if !runLevel && e.Type != trace.TypeEpisodeStart && !v.started {
		v.errorf(e, "before episode_start")
	}
	if v.ended && !runLevel && e.Type != trace.TypeProvenance && e.Type != trace.TypeBudget {
		v.errorf(e, "after episode_end")
	}
	if v.runEnd {
		v.errorf(e, "after run_end")
	}
	bad := func(err error) bool {
		if err != nil {
			v.errorf(e, "body: %v", err)
			return true
		}
		return false
	}
	switch e.Type {
	case trace.TypeRunStart:
		var b trace.RunStart
		if bad(e.DecodeBody(&b)) {
			return
		}
		if b.Schema != trace.Schema {
			v.errorf(e, "schema %q, want %q", b.Schema, trace.Schema)
		}
		if b.Backend == "" || (b.Session != SessionLockstep && b.Session != SessionInProc) {
			v.errorf(e, "backend %q, session %q", b.Backend, b.Session)
		}
	case trace.TypeEpisodeStart:
		if v.started {
			v.errorf(e, "a second episode_start")
		}
		v.started = true
	case trace.TypeLevelStart:
		var b trace.LevelStart
		if bad(e.DecodeBody(&b)) {
			return
		}
		if v.openLvl >= 0 {
			v.errorf(e, "level %d starts while level %d is open", e.Lvl, v.openLvl)
		}
		if e.Lvl != v.lastLvl+1 || e.Map == "" {
			v.errorf(e, "level %d (map %q) after level %d", e.Lvl, e.Map, v.lastLvl)
		}
		v.openLvl, v.lastLvl = e.Lvl, e.Lvl
		v.levels++
	case trace.TypeLevelEnd:
		var b trace.LevelEnd
		if bad(e.DecodeBody(&b)) {
			return
		}
		if e.Lvl != v.openLvl {
			v.errorf(e, "level %d ends while level %d is open", e.Lvl, v.openLvl)
		}
		if !levelOutcomes[b.Outcome] {
			v.errorf(e, "outcome %q", b.Outcome)
		}
		v.openLvl = -1
	case trace.TypeDecision:
		var b trace.Decision
		if bad(e.DecodeBody(&b)) {
			return
		}
		switch {
		case requestLane(b.Lane):
			if b.Req == 0 || b.Backend == "" || !isDigest(b.StateDigest) || !isDigest(b.ReqDigest) {
				v.errorf(e, "request %d of backend %q, digests %q %q", b.Req, b.Backend, b.StateDigest, b.ReqDigest)
			}
			if b.Response == nil && b.Err == "" {
				v.errorf(e, "request %d has neither a response nor an error", b.Req)
			}
			for _, f := range b.Fields {
				if _, ok := decide.FieldOf(f.Name); !ok || !knownSource(f.Source) {
					v.errorf(e, "field %q source %q", f.Name, f.Source)
				}
			}
		case b.Lane == trace.LaneTick:
			if b.Intent == nil {
				v.errorf(e, "a tick without its Intent")
				break
			}
			for _, f := range b.Intent.Fields {
				if _, ok := decide.FieldOf(f.Name); !ok || !knownSource(f.Source) {
					v.errorf(e, "tick field %q source %q", f.Name, f.Source)
				}
			}
			v.acted.add(b.Intent.Fields)
		default:
			v.errorf(e, "lane %q", b.Lane)
		}
		if b.LatencyMs < 0 {
			v.errorf(e, "latency %v", b.LatencyMs)
		}
	case trace.TypeAPICall:
		var b trace.APICall
		if bad(e.DecodeBody(&b)) {
			return
		}
		if !requestLane(b.Lane) || b.Req == 0 || b.LatencyMs < 0 || b.Backend == "" {
			v.errorf(e, "lane %q req %d latency %v backend %q", b.Lane, b.Req, b.LatencyMs, b.Backend)
		}
	case trace.TypeDamage:
		var b trace.Damage
		if !bad(e.DecodeBody(&b)) && b.Amount < 0 {
			v.errorf(e, "amount %d", b.Amount)
		}
	case trace.TypeKill:
		var b trace.Kill
		if !bad(e.DecodeBody(&b)) && b.Target == "" {
			v.errorf(e, "no target")
		}
	case trace.TypeDeath:
		bad(e.DecodeBody(&trace.Death{}))
	case trace.TypeReload:
		var b trace.Reload
		if !bad(e.DecodeBody(&b)) && b.Deaths < 1 {
			v.errorf(e, "reload after %d deaths", b.Deaths)
		}
	case trace.TypeStuck:
		var b trace.Stuck
		if !bad(e.DecodeBody(&b)) && b.Stage == "" {
			v.errorf(e, "no stage")
		}
	case trace.TypeBudget:
		var b trace.Budget
		if !bad(e.DecodeBody(&b)) && (b.SpentUSD < 0 || b.RateHz < 0) {
			v.errorf(e, "spent %v rate %v", b.SpentUSD, b.RateHz)
		}
	case trace.TypeError:
		var b trace.Error
		if !bad(e.DecodeBody(&b)) && b.Msg == "" {
			v.errorf(e, "no message")
		}
	case trace.TypeEpisodeEnd:
		var b trace.EpisodeEnd
		if bad(e.DecodeBody(&b)) {
			return
		}
		if !episodeOutcomes[b.Outcome] {
			v.errorf(e, "outcome %q", b.Outcome)
		}
		if v.openLvl >= 0 {
			v.errorf(e, "level %d is still open", v.openLvl)
		}
		v.ended = true
	case trace.TypeRunEnd:
		var b trace.RunEnd
		if !bad(e.DecodeBody(&b)) && !episodeOutcomes[b.Outcome] {
			v.errorf(e, "outcome %q", b.Outcome)
		}
		v.runEnd = true
	case trace.TypeCmds:
		var b trace.Cmds
		if bad(e.DecodeBody(&b)) {
			return
		}
		if len(b.Cmds) == 0 || len(b.Cmds) > 16 || b.Step < 0 {
			v.errorf(e, "step %d with %d commands", b.Step, len(b.Cmds))
		}
		for _, c := range b.Cmds {
			if c.Msec == 0 || c.Msec > 250 {
				v.errorf(e, "step %d: a command of %d ms", b.Step, c.Msec)
				break
			}
		}
	case trace.TypeProvenance:
		var b trace.Provenance
		if bad(e.DecodeBody(&b)) {
			return
		}
		if v.prov != nil {
			v.errorf(e, "a second provenance event")
		}
		v.prov = &b
		for _, f := range b.Fields {
			if _, ok := decide.FieldOf(f.Name); !ok {
				v.errorf(e, "field %q", f.Name)
			}
			if n := f.Total(); n != b.Ticks {
				v.errorf(e, "field %s counts %d ticks of %d", f.Name, n, b.Ticks)
			}
		}
		for _, f := range b.Arbiter {
			if _, ok := decide.FieldOf(f.Name); !ok || f.Reflex != 0 {
				v.errorf(e, "arbiter field %q (reflex %d)", f.Name, f.Reflex)
			}
			if n := f.Total(); n != b.ArbiterTicks {
				v.errorf(e, "arbiter field %s counts %d ticks of %d", f.Name, n, b.ArbiterTicks)
			}
		}
	}
}

// checkProvenance cross-checks the episode's provenance event: its
// acted-on counts are the lane tick events' (when the trace has them),
// and the arbiter decided at least as often from every source as the bot
// acted on it (the bot's overrides only turn values into reflex).
func (v *episodeValidator) checkProvenance() {
	p := v.prov
	if p == nil {
		return
	}
	if v.acted.ticks > 0 {
		if p.Ticks != v.acted.ticks {
			v.rep.errorf("%s: the provenance event counts %d ticks, the trace has %d tick events", v.file, p.Ticks, v.acted.ticks)
		}
		for _, f := range p.Fields {
			if got := v.acted.field(f.Name); got != f {
				v.rep.errorf("%s: provenance of %s %+v, the tick events count %+v", v.file, f.Name, f, got)
			}
		}
	}
	if len(p.Arbiter) == 0 {
		return
	}
	if p.ArbiterTicks != p.Ticks {
		v.rep.errorf("%s: the arbiter made %d intents for %d ticks acted on", v.file, p.ArbiterTicks, p.Ticks)
	}
	arb := map[string]trace.TickField{}
	for _, f := range p.Arbiter {
		arb[f.Name] = f
	}
	for _, f := range p.Fields {
		a, ok := arb[f.Name]
		if !ok {
			continue
		}
		if f.Default > a.Default || f.Model > a.Model || f.Scripted > a.Scripted || f.Stale > a.Stale {
			v.rep.errorf("%s: %s acted on %+v, more than the arbiter decided %+v", v.file, f.Name, f, a)
		}
	}
}

// finish checks the episode's end (last: the run's last trace, which must
// hold run_end).
func (v *episodeValidator) finish(last bool) {
	v.checkProvenance()
	switch {
	case !v.started:
		v.rep.errorf("%s: no episode_start", v.file)
	case !v.ended:
		v.rep.warnf("%s: no episode_end (the episode did not finish)", v.file)
	}
	if last && !v.runEnd {
		v.rep.warnf("%s: no run_end (the run did not finish)", v.file)
	}
}

func isDigest(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func knownSource(s string) bool {
	switch s {
	case trace.SourceModel, trace.SourceScripted, trace.SourceReflex, trace.SourceStale, trace.SourceDefault:
		return true
	}
	return false
}
