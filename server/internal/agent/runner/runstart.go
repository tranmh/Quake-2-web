package runner

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/trace"
)

// Keys of run_start's config map: the settings a replay needs besides
// run_start's own fields (backend, session, maps, skill, seed).
const (
	keyCampaign          = "campaign"
	keyStopAfter         = "stop_after"
	keyTrace             = "trace"
	keyRecord            = "record"
	keySimLatency        = "sim_latency"
	keyEntryCommands     = "entry_commands"
	keyBudgetQueries     = "budget.queries"
	keyBudgetMaxQPS      = "budget.max_qps"
	keyBudgetOnExhausted = "budget.on_exhausted"
	keyMinModelShare     = "gate.min_model_share"
	keyMaxStaleRate      = "gate.max_stale_rate"
	keyReplayTrace       = "replay_trace"
	keyLevelTimeout      = "level_timeout"
	keyEpisodeTimeout    = "episode_timeout"
	keyMaxDeaths         = "max_deaths"
	keyMockFaults        = "mock.faults"
)

// recordedRun is what a trace says about the run that wrote it.
type recordedRun struct {
	start trace.RunStart
	run   string
	cfg   Config // the settings that shape an episode (no FS, no outputs)
}

// configFromRunStart rebuilds the episode-shaping part of a run's Config
// from its run_start event.
func configFromRunStart(e trace.Event) (*recordedRun, error) {
	var rs trace.RunStart
	if err := e.DecodeBody(&rs); err != nil {
		return nil, fmt.Errorf("runner: run_start: %w", err)
	}
	if rs.Schema != trace.Schema {
		return nil, fmt.Errorf("runner: run_start schema %q, want %q", rs.Schema, trace.Schema)
	}
	m := rs.Config
	c := Config{Backend: rs.Backend, Session: rs.Session, Maps: append([]string(nil), rs.Maps...), Seed: rs.Seed,
		Episodes: max(rs.Episodes, 1)}
	skill := rs.Skill
	c.Skill = &skill
	var err error
	if c.Trace, err = ParseTrace(m[keyTrace]); err != nil {
		return nil, err
	}
	if c.SimLatency, err = ParseLatency(m[keySimLatency]); err != nil {
		return nil, err
	}
	if !c.SimLatency.Set && rs.SimLatencyMs > 0 {
		c.SimLatency = Latency{Fixed: time.Duration(rs.SimLatencyMs * float64(time.Millisecond)), Set: true}
	}
	c.Record = m[keyRecord] == "true"
	if s := m[keyEntryCommands]; s != "" {
		c.EntryCommands = strings.Split(s, ";")
	}
	num := func(key string, f func(string) error) {
		if err == nil && m[key] != "" {
			if e := f(m[key]); e != nil {
				err = fmt.Errorf("runner: run_start %s %q: %w", key, m[key], e)
			}
		}
	}
	num(keyBudgetQueries, func(s string) (e error) { c.Budget.Queries, e = strconv.Atoi(s); return })
	num(keyBudgetMaxQPS, func(s string) (e error) { c.Budget.MaxQPS, e = strconv.ParseFloat(s, 64); return })
	num(keyBudgetOnExhausted, func(s string) (e error) { c.Budget.OnExhausted, e = budget.ParsePolicy(s); return })
	num(keyMinModelShare, func(s string) (e error) { c.MinModelShare, e = strconv.ParseFloat(s, 64); return })
	num(keyMaxStaleRate, func(s string) (e error) { c.MaxStaleRate, e = strconv.ParseFloat(s, 64); return })
	num(keyLevelTimeout, func(s string) (e error) { c.LevelTimeout, e = time.ParseDuration(s); return })
	num(keyEpisodeTimeout, func(s string) (e error) { c.EpisodeTimeout, e = time.ParseDuration(s); return })
	num(keyMaxDeaths, func(s string) (e error) { c.MaxDeaths, e = strconv.Atoi(s); return })
	if err != nil {
		return nil, err
	}
	c.Budget.USD = rs.BudgetUSD
	c.ReplayTrace = m[keyReplayTrace]
	return &recordedRun{start: rs, run: e.Run, cfg: c}, nil
}

// gateConfig returns the run's gate thresholds from its run_start.
func gateFromRunStart(rs trace.RunStart) (minShare, maxStale float64) {
	minShare, _ = strconv.ParseFloat(rs.Config[keyMinModelShare], 64)
	maxStale, _ = strconv.ParseFloat(rs.Config[keyMaxStaleRate], 64)
	return minShare, maxStale
}
