package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/trace"
)

// episodeDirPattern matches the episode directories of a run.
var episodeDirPattern = regexp.MustCompile(`^ep-(\d{3,})$`)

// EpisodeTrace is one episode's trace file in a run directory.
type EpisodeTrace struct {
	Episode int
	Path    string
}

// EpisodeTraces lists the episode traces of run directory dir, in episode
// order.
func EpisodeTraces(dir string) ([]EpisodeTrace, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []EpisodeTrace
	for _, e := range ents {
		m := episodeDirPattern.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		p := filepath.Join(dir, e.Name(), TraceFile)
		if _, err := os.Stat(p); err != nil {
			continue
		}
		out = append(out, EpisodeTrace{Episode: n, Path: p})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Episode < out[j].Episode })
	if len(out) == 0 {
		return nil, fmt.Errorf("runner: %s has no episode traces (%s/%s)", dir, "ep-NNN", TraceFile)
	}
	return out, nil
}

// RunEvents reads every episode trace of run directory dir and returns
// the run's events in order, each once (later episodes' traces repeat the
// run_start). A truncated trace (a crash) contributes its complete events
// and is reported in the error, which wraps io.ErrUnexpectedEOF; the
// events are returned anyway.
func RunEvents(dir string) ([]trace.Event, error) {
	files, err := EpisodeTraces(dir)
	if err != nil {
		return nil, err
	}
	var out []trace.Event
	var errs []error
	seen := map[uint64]bool{}
	for _, f := range files {
		evs, err := trace.ReadFile(f.Path)
		if err != nil {
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				return nil, fmt.Errorf("%s: %w", f.Path, err)
			}
			errs = append(errs, fmt.Errorf("%s: %w", f.Path, err))
		}
		for _, e := range evs {
			if seen[e.Seq] {
				continue
			}
			seen[e.Seq] = true
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, errors.Join(errs...)
}

// SummarizeOptions overrides what Summarize takes from the trace.
type SummarizeOptions struct {
	// MinModelShare and MaxStaleRate override the run's gate thresholds
	// (0: the run's own, from its run_start).
	MinModelShare, MaxStaleRate float64
}

// Summarize recomputes a run's summary (run.json) from its events, with
// the provenance gate of its run_start (or the options').
func Summarize(events []trace.Event, opt SummarizeOptions) (metrics.RunSummary, error) {
	minShare, maxStale := opt.MinModelShare, opt.MaxStaleRate
	for i := range events {
		if events[i].Type != trace.TypeRunStart {
			continue
		}
		var rs trace.RunStart
		if err := events[i].DecodeBody(&rs); err != nil {
			return metrics.RunSummary{}, err
		}
		m, s := gateFromRunStart(rs)
		if minShare == 0 {
			minShare = m
		}
		if maxStale == 0 {
			maxStale = s
		}
		break
	}
	c := metrics.NewCollector()
	c.SetGate(metrics.GateConfig{MinModelShare: minShare, MaxStaleRate: maxStale})
	var errs []error
	for _, e := range events {
		if err := c.Add(e); err != nil {
			errs = append(errs, err)
		}
	}
	return c.Summary(), errors.Join(errs...)
}

// SummarizeDir recomputes the summary of run directory dir from its
// traces (see RunEvents and Summarize).
func SummarizeDir(dir string, opt SummarizeOptions) (metrics.RunSummary, error) {
	evs, rerr := RunEvents(dir)
	if evs == nil && rerr != nil {
		return metrics.RunSummary{}, rerr
	}
	s, err := Summarize(evs, opt)
	return s, errors.Join(rerr, err)
}
