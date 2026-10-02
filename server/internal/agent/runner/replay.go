package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"quake2web/server/internal/agent/backend/replay"
	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/qcommon/shared"
)

// Replay modes.
const (
	// ReplayActions feeds the recorded usercmds to the game (the bot still
	// computes its own, which are compared with them) and answers the
	// requests with the recorded responses: it checks that the game and
	// the bot's perception and decisions repeat on the recorded inputs.
	ReplayActions = "actions"
	// ReplayResponses lets the bot play (its own usercmds) on the
	// recorded backend responses: it checks that the whole agent repeats.
	ReplayResponses = "responses"
)

// ReplayConfig configures Replay.
type ReplayConfig struct {
	// Trace is the recorded episode trace: ep-NNN/trace.jsonl.gz of a
	// lockstep run.
	Trace string
	// Mode is ReplayActions ("") or ReplayResponses.
	Mode string
	// Strict stops at the first divergence.
	Strict bool
	// FS is the game data (the pak the run played).
	FS FileSystem
	// Campaign, RoutesDir, NavDir and Library locate the static data, as
	// for a run (the run's own are not recorded).
	Campaign  *route.Campaign
	RoutesDir string
	NavDir    string
	Library   *campaign.Library
	// OutDir, when set, receives the replay's own run directory (its
	// trace carries full lane states).
	OutDir string
	Logf   func(format string, args ...any)
	Now    func() time.Time
}

// Divergence is the first point where a replay differs from its
// recording.
type Divergence struct {
	// Kind: "event" (an event differs), "cmd" (actions mode: the bot
	// computed another usercmd than the recorded one), "missing" (the
	// replay ended before the recording) or "extra" (it went on past it).
	Kind string `json:"kind"`
	// Index is the event's position in the episode's events.
	Index int    `json:"index"`
	Seq   uint64 `json:"seq,omitempty"` // the recorded event's
	Type  string `json:"type,omitempty"`
	GMs   int64  `json:"gms"`
	// Step and Cmd locate a usercmd (kind cmd).
	Step int64 `json:"step,omitempty"`
	Cmd  int   `json:"cmd,omitempty"`
	// Want and Got are the recorded and the replayed event (normalized:
	// no wall clock, seq or run; decisions without their state, which
	// their digests cover) or usercmd.
	Want json.RawMessage `json:"want,omitempty"`
	Got  json.RawMessage `json:"got,omitempty"`
	// Diff lists what differs: of the lane states when the divergence is
	// a decision whose states are known (StateDiff), else of Want and Got.
	Diff      []DiffEntry `json:"diff,omitempty"`
	StateDiff bool        `json:"state_diff,omitempty"`
	Note      string      `json:"note,omitempty"`
}

// DiffEntry is one differing JSON value ("" Want or Got: absent).
type DiffEntry struct {
	Path string          `json:"path"`
	Want json.RawMessage `json:"want,omitempty"`
	Got  json.RawMessage `json:"got,omitempty"`
}

// ReplayReport is a replay's result.
type ReplayReport struct {
	Trace   string `json:"trace"`
	Mode    string `json:"mode"`
	Strict  bool   `json:"strict"`
	Run     string `json:"run"`
	Episode int    `json:"episode"`
	Backend string `json:"backend"`
	// Recorded and Replayed count the episode's events; Matched those
	// equal, Mismatches those not (after the first divergence the counts
	// of a strict replay stop).
	Recorded   int `json:"recorded"`
	Replayed   int `json:"replayed"`
	Matched    int `json:"matched"`
	Mismatches int `json:"mismatches"`
	// Cmds and CmdMismatches count the usercmds compared (actions mode).
	Cmds          int `json:"cmds"`
	CmdMismatches int `json:"cmd_mismatches"`
	// Divergence is the first one (nil: none).
	Divergence *Divergence `json:"divergence,omitempty"`
	// ResponseDivergence is the replay backend's first request that did
	// not match the recording (digest, lane or a missing request), except
	// requests after the last recorded one (in flight when the recorded
	// episode ended, never recorded); InFlight counts those.
	ResponseDivergence *replay.Divergence `json:"response_divergence,omitempty"`
	InFlight           int                `json:"in_flight_at_end,omitempty"`
	Responses          replay.Stats       `json:"responses"`
	// SummaryEqual: the replayed episode's summary equals the recorded
	// one.
	SummaryEqual    bool                   `json:"summary_equal"`
	RecordedSummary metrics.EpisodeSummary `json:"recorded_summary"`
	ReplayedSummary metrics.EpisodeSummary `json:"replayed_summary"`
	// Dir is the replay's run directory (OutDir set).
	Dir string `json:"dir,omitempty"`
	// Err is the replayed episode's error, if it could not be played.
	Err string `json:"error,omitempty"`
}

// Diverged reports whether the replay differs from the recording: an
// event (or, in actions mode, a usercmd) differs or the summaries do. A
// request the replay backend did not find (ResponseDivergence) matters
// only through what it changes in the events: the requests still in
// flight when the recorded episode ended were never recorded.
func (r *ReplayReport) Diverged() bool {
	return r.Divergence != nil || !r.SummaryEqual
}

// Replay re-runs a recorded lockstep episode and compares it with the
// recording, event by event (and, in actions mode, usercmd by usercmd).
// The error is for a replay that could not be set up or run; the
// differences are in the report.
func Replay(ctx context.Context, rc ReplayConfig) (*ReplayReport, error) {
	switch rc.Mode {
	case "":
		rc.Mode = ReplayActions
	case ReplayActions, ReplayResponses:
	default:
		return nil, fmt.Errorf("%w: replay mode %q (actions|responses)", ErrConfig, rc.Mode)
	}
	events, err := trace.ReadFile(rc.Trace)
	if err != nil {
		return nil, fmt.Errorf("runner: %s: %w", rc.Trace, err)
	}
	var rsEv *trace.Event
	ep, epSeed, haveEp := 0, uint64(0), false
	for i := range events {
		e := &events[i]
		switch {
		case e.Type == trace.TypeRunStart && rsEv == nil:
			rsEv = e
		case e.Type == trace.TypeEpisodeStart && !haveEp:
			var b trace.EpisodeStart
			if err := e.DecodeBody(&b); err != nil {
				return nil, err
			}
			ep, epSeed, haveEp = e.Ep, b.Seed, true
		}
	}
	if rsEv == nil || !haveEp {
		return nil, fmt.Errorf("runner: %s is not an episode trace (no run_start or episode_start)", rc.Trace)
	}
	rec, err := configFromRunStart(*rsEv)
	if err != nil {
		return nil, err
	}
	if rec.cfg.Session != SessionLockstep {
		return nil, fmt.Errorf("runner: %s was recorded in a %s session; only lockstep runs replay", rc.Trace, rec.cfg.Session)
	}
	var want []trace.Event
	cmds := map[int64][]trace.StepCmd{}
	for _, e := range events {
		if e.Ep != ep || e.Type == trace.TypeRunStart || e.Type == trace.TypeRunEnd {
			continue
		}
		want = append(want, e)
		if e.Type == trace.TypeCmds {
			var b trace.Cmds
			if err := e.DecodeBody(&b); err != nil {
				return nil, err
			}
			cmds[b.Step] = b.Cmds
		}
	}
	if rc.Mode == ReplayActions && len(cmds) == 0 {
		return nil, fmt.Errorf("runner: %s has no recorded usercmds (cmds events) for an actions replay", rc.Trace)
	}

	cfg := rec.cfg
	cfg.FS, cfg.Campaign, cfg.RoutesDir, cfg.NavDir, cfg.Library = rc.FS, rc.Campaign, rc.RoutesDir, rc.NavDir, rc.Library
	// the recorded latencies first, the recorded run's for the requests
	// still in flight when it ended (rec.cfg.SimLatency)
	cfg.Backend, cfg.ReplayTrace = BackendReplay, rc.Trace
	cfg.Seed = epSeed - uint64(ep)
	cfg.Episodes = ep + 1
	cfg.Trace, cfg.Record = TraceFull, false
	cfg.OutDir, cfg.RunID = rc.OutDir, rec.run+"-replay-"+rc.Mode
	cfg.Logf, cfg.Now = rc.Logf, rc.Now
	cfg.RequireComplete = false

	rep := &ReplayReport{Trace: rc.Trace, Mode: rc.Mode, Strict: rc.Strict, Run: rec.run, Episode: ep, Backend: rec.start.Backend,
		Recorded: len(want)}
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmp := &comparator{want: want, strict: rc.Strict, cancel: cancel}
	h := runnerHooks{epFrom: ep, strict: rc.Strict, noFiles: rc.OutDir == "", sinks: []trace.Sink{cmp}}
	if rc.Mode == ReplayActions {
		h.feed = cmp.feed(cmds)
	}
	r, err := newRunner(cfg, h)
	if err != nil {
		return nil, err
	}
	var played *episodeRun
	r.onEpisode = func(er *episodeRun) { played = er }
	_, runErr := r.Run(rctx)
	cmp.finish()

	cmp.report(rep)
	if !h.noFiles {
		rep.Dir = r.Dir()
	}
	if played != nil && played.be != nil && played.be.replay != nil {
		rep.Responses = played.be.replay.Stats()
		if d := played.be.replay.Divergence(); d != nil && !(d.Reason == "missing" && d.Seq > r.be.recMaxSeq[ep]) {
			rep.ResponseDivergence = d
		}
		rep.InFlight = rep.Responses.Missing
	}
	wantSum, _ := Summarize(events, SummarizeOptions{})
	gotSum := r.col.Summary()
	for _, es := range wantSum.Episodes {
		if es.Index == ep {
			rep.RecordedSummary = es
		}
	}
	for _, es := range gotSum.Episodes {
		if es.Index == ep {
			rep.ReplayedSummary = es
		}
	}
	a, _ := trace.Canonical(rep.RecordedSummary)
	b, _ := trace.Canonical(rep.ReplayedSummary)
	rep.SummaryEqual = bytes.Equal(a, b)
	if runErr != nil && !(rc.Strict && rep.Divergence != nil) {
		rep.Err = runErr.Error()
	}
	return rep, nil
}

// comparator compares the replayed events with the recorded ones as they
// are published (a bus sink) and, in actions mode, the bot's usercmds with
// the recorded ones (the stepper's feed).
type comparator struct {
	want   []trace.Event
	strict bool
	cancel func()

	mu      sync.Mutex
	n       int // replayed events compared
	matched int
	bad     int
	cmds    int
	badCmds int
	first   *Divergence
	done    bool // strict: stopped comparing
}

// Write implements trace.Sink (it runs under the bus lock: no bus calls).
func (c *comparator) Write(e trace.Event) error {
	if e.Type == trace.TypeRunStart || e.Type == trace.TypeRunEnd {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.n
	c.n++
	if c.done {
		return nil
	}
	got, err := normalized(e)
	if err != nil {
		return err
	}
	if i >= len(c.want) {
		c.bad++
		c.diverge(&Divergence{Kind: "extra", Index: i, Type: e.Type, GMs: e.GMs, Got: got,
			Note: fmt.Sprintf("the recording has %d events", len(c.want))})
		return nil
	}
	w := c.want[i]
	want, err := normalized(w)
	if err != nil {
		return err
	}
	if bytes.Equal(want, got) {
		c.matched++
		return nil
	}
	c.bad++
	if c.first == nil {
		d := &Divergence{Kind: "event", Index: i, Seq: w.Seq, Type: w.Type, GMs: w.GMs, Want: want, Got: got}
		explain(d, w, e)
		c.diverge(d)
	}
	return nil
}

func (c *comparator) diverge(d *Divergence) {
	if c.first == nil {
		c.first = d
	}
	if c.strict {
		c.done = true
		c.cancel()
	}
}

// feed returns the stepper's feed of an actions replay.
func (c *comparator) feed(rec map[int64][]trace.StepCmd) func(step int64, i int, bot shared.UserCmd) shared.UserCmd {
	return func(step int64, i int, bot shared.UserCmd) shared.UserCmd {
		cmds, ok := rec[step]
		c.mu.Lock()
		defer c.mu.Unlock()
		c.cmds++
		if !ok || i >= len(cmds) {
			c.badCmds++
			if !c.done {
				got, _ := json.Marshal(traceCmd(bot))
				c.diverge(&Divergence{Kind: "cmd", Step: step, Cmd: i, Got: got,
					Note: "the recording has no usercmd here (the client was not active at this step)"})
			}
			return bot
		}
		w := cmds[i]
		if g := traceCmd(bot); g != w {
			c.badCmds++
			if !c.done {
				want, _ := json.Marshal(w)
				got, _ := json.Marshal(g)
				d := &Divergence{Kind: "cmd", Step: step, Cmd: i, Want: want, Got: got,
					Note: "the bot computed another usercmd than the recorded one (the recorded one was sent)"}
				d.Diff = diffJSON(want, got, 50)
				c.diverge(d)
			}
		}
		return userCmd(w)
	}
}

// finish notes a replay that ended before the recording did.
func (c *comparator) finish() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.done || c.n >= len(c.want) {
		return
	}
	w := c.want[c.n]
	want, _ := normalized(w)
	c.bad += len(c.want) - c.n
	c.diverge(&Divergence{Kind: "missing", Index: c.n, Seq: w.Seq, Type: w.Type, GMs: w.GMs, Want: want,
		Note: fmt.Sprintf("the replay ended after %d of %d events", c.n, len(c.want))})
}

func (c *comparator) report(r *ReplayReport) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r.Replayed, r.Matched, r.Mismatches = c.n, c.matched, c.bad
	r.Cmds, r.CmdMismatches = c.cmds, c.badCmds
	r.Divergence = c.first
}

// normalized is an event as replays compare it: canonical JSON without
// the wall clock, seq and run id; decision events without their state
// and questions (their digests stay), api calls without the transport's
// status and retry count (a replayed response has neither).
func normalized(e trace.Event) (json.RawMessage, error) {
	e.Seq, e.Run, e.Wall = 0, "", 0
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	delete(m, "wall")
	delete(m, "seq")
	delete(m, "run")
	switch e.Type {
	case trace.TypeDecision:
		delete(m, "state")
		delete(m, "questions")
	case trace.TypeAPICall:
		delete(m, "status")
		delete(m, "retry")
	}
	return trace.Canonical(m)
}

// explain fills a divergence's diff: of the lane states for a decision
// whose recorded and replayed states are both known, else of the events.
func explain(d *Divergence, want, got trace.Event) {
	if want.Type == trace.TypeDecision && got.Type == trace.TypeDecision {
		var w, g struct {
			State json.RawMessage `json:"state"`
		}
		_ = want.DecodeBody(&w)
		_ = got.DecodeBody(&g)
		switch {
		case w.State != nil && g.State != nil:
			d.Diff, d.StateDiff = diffJSON(w.State, g.State, 50), true
			if len(d.Diff) == 0 {
				d.Note = "the lane states are equal; the decision differs elsewhere"
				d.Diff, d.StateDiff = diffJSON(d.Want, d.Got, 50), false
			}
			return
		case g.State != nil:
			d.Note = "the recording keeps only state digests (run with -trace full to diff the lane states); got state: " + string(g.State)
		}
	}
	d.Diff = diffJSON(d.Want, d.Got, 50)
}

// diffJSON lists the paths where two JSON documents differ (at most
// limit).
func diffJSON(want, got json.RawMessage, limit int) []DiffEntry {
	var w, g any
	dw := json.NewDecoder(bytes.NewReader(want))
	dw.UseNumber()
	dg := json.NewDecoder(bytes.NewReader(got))
	dg.UseNumber()
	if dw.Decode(&w) != nil || dg.Decode(&g) != nil {
		if !bytes.Equal(want, got) {
			return []DiffEntry{{Path: "$", Want: want, Got: got}}
		}
		return nil
	}
	var out []DiffEntry
	diffValue("$", w, g, &out, limit)
	return out
}

func diffValue(path string, w, g any, out *[]DiffEntry, limit int) {
	if len(*out) >= limit {
		return
	}
	switch wv := w.(type) {
	case map[string]any:
		if gv, ok := g.(map[string]any); ok {
			keys := map[string]bool{}
			for k := range wv {
				keys[k] = true
			}
			for k := range gv {
				keys[k] = true
			}
			names := make([]string, 0, len(keys))
			for k := range keys {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				a, inW := wv[k]
				b, inG := gv[k]
				p := path + "." + k
				switch {
				case !inW:
					*out = append(*out, DiffEntry{Path: p, Got: raw(b)})
				case !inG:
					*out = append(*out, DiffEntry{Path: p, Want: raw(a)})
				default:
					diffValue(p, a, b, out, limit)
				}
				if len(*out) >= limit {
					return
				}
			}
			return
		}
	case []any:
		if gv, ok := g.([]any); ok {
			for i := 0; i < max(len(wv), len(gv)); i++ {
				p := path + "[" + strconv.Itoa(i) + "]"
				switch {
				case i >= len(wv):
					*out = append(*out, DiffEntry{Path: p, Got: raw(gv[i])})
				case i >= len(gv):
					*out = append(*out, DiffEntry{Path: p, Want: raw(wv[i])})
				default:
					diffValue(p, wv[i], gv[i], out, limit)
				}
				if len(*out) >= limit {
					return
				}
			}
			return
		}
	}
	a, b := raw(w), raw(g)
	if !bytes.Equal(a, b) {
		*out = append(*out, DiffEntry{Path: path, Want: a, Got: b})
	}
}

func raw(v any) json.RawMessage {
	b, err := trace.Canonical(v)
	if err != nil {
		return json.RawMessage(strconv.Quote(fmt.Sprint(v)))
	}
	return b
}
