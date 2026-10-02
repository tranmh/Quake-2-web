package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/runner"
)

func runReplay(e *env, args []string) error {
	fs := newFlags("replay", e)
	tracePath := fs.String("trace", "", "the recorded episode trace: runs/<id>/ep-NNN/trace.jsonl.gz (required)")
	mode := fs.String("mode", runner.ReplayActions, "actions: feed the recorded usercmds; responses: let the bot play on the recorded backend answers")
	strict := fs.Bool("strict", false, "stop at the first divergence")
	out := fs.String("out", "", "write the replay's own run directory here (its trace has full lane states)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	pakPath := fs.String("pak", defaultPak(e.getenv), "the game data the run played")
	navDir := fs.String("nav-dir", "", "the nav graph cache (default: assets/nav)")
	camp := fs.String("campaign", "", "the route tables' directory (default: fixtures/agent/routes)")
	verbose := fs.Bool("v", false, "log the replayed episode's lines to stderr")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if *tracePath == "" && len(pos) == 1 {
		*tracePath, pos = pos[0], nil
	}
	switch {
	case len(pos) > 0:
		return fmt.Errorf("%w: unexpected argument %q", errUsage, pos[0])
	case *tracePath == "":
		return fmt.Errorf("%w: -trace is required", errUsage)
	case *mode != runner.ReplayActions && *mode != runner.ReplayResponses:
		return fmt.Errorf("%w: -mode %q (actions|responses)", errUsage, *mode)
	}
	pk, err := openPak(*pakPath)
	if err != nil {
		return err
	}
	defer pk.Close()
	rc := runner.ReplayConfig{Trace: *tracePath, Mode: *mode, Strict: *strict, FS: pk, NavDir: *navDir, OutDir: *out}
	if *camp != "" {
		rc.RoutesDir = *camp
	}
	if *verbose {
		rc.Logf = func(format string, args ...any) { fmt.Fprintf(e.stderr, format+"\n", args...) }
	}
	ctx, stop := signalContext()
	defer stop()
	rep, err := runner.Replay(ctx, rc)
	if err != nil {
		return configErr(err)
	}
	if *asJSON {
		writeJSON(e.stdout, rep)
	} else {
		printReplay(e.stdout, rep)
	}
	if rep.Diverged() || rep.Err != "" {
		return errFailed
	}
	return nil
}

func printReplay(w io.Writer, r *runner.ReplayReport) {
	verdict := "no divergence"
	if r.Diverged() {
		verdict = "DIVERGED"
	}
	fmt.Fprintf(w, "replay %s (%s mode", r.Trace, r.Mode)
	if r.Strict {
		fmt.Fprint(w, ", strict")
	}
	fmt.Fprintf(w, "): %s\n", verdict)
	fmt.Fprintf(w, "  run %s episode %d, backend %s: %d of %d recorded events replayed equal (%d replayed, %d differ)\n",
		r.Run, r.Episode, r.Backend, r.Matched, r.Recorded, r.Replayed, r.Mismatches)
	if r.Mode == runner.ReplayActions {
		fmt.Fprintf(w, "  usercmds: %d fed, %d the bot computed differently\n", r.Cmds, r.CmdMismatches)
	}
	fmt.Fprintf(w, "  responses: %d matched, %d by seq despite a new digest, %d missing\n", r.Responses.Matched,
		r.Responses.SeqFallback, r.Responses.Missing)
	fmt.Fprintf(w, "  summary: equal %v (recorded %s, replayed %s)\n", r.SummaryEqual, r.RecordedSummary.Outcome, r.ReplayedSummary.Outcome)
	if d := r.Divergence; d != nil {
		switch d.Kind {
		case "cmd":
			fmt.Fprintf(w, "  first divergence: usercmd %d of step %d: %s\n", d.Cmd, d.Step, d.Note)
		default:
			fmt.Fprintf(w, "  first divergence: %s, event %d (recorded seq %d, %s at %d ms)", d.Kind, d.Index, d.Seq, d.Type, d.GMs)
			if d.Note != "" {
				fmt.Fprintf(w, ": %s", d.Note)
			}
			fmt.Fprintln(w)
		}
		if d.StateDiff {
			fmt.Fprintln(w, "  lane state diff (recorded -> replayed):")
		} else if len(d.Diff) > 0 {
			fmt.Fprintln(w, "  diff (recorded -> replayed):")
		}
		diff, _ := json.MarshalIndent(d.Diff, "    ", "  ")
		if len(d.Diff) > 0 {
			fmt.Fprintf(w, "    %s\n", diff)
		}
	}
	if d := r.ResponseDivergence; d != nil {
		fmt.Fprintf(w, "  first request off the recording: %v\n", d)
	}
	if r.Dir != "" {
		fmt.Fprintf(w, "  replay run directory: %s\n", r.Dir)
	}
	if r.Err != "" {
		fmt.Fprintf(w, "  error: %s\n", r.Err)
	}
}

// runSummarize recomputes run.json from the traces. Traces that do not
// read whole (a truncated file, an event the collector rejects) give a
// partial summary: it is printed and the exit status is 1, and it
// replaces run.json only when there is none (a crashed run) or with
// -force, so a good run.json is never overwritten by a partial one.
func runSummarize(e *env, args []string) error {
	fs := newFlags("summarize", e)
	dry := fs.Bool("dry-run", false, "print the summary, do not rewrite run.json")
	force := fs.Bool("force", false, "rewrite run.json even from traces that do not read whole")
	asJSON := fs.Bool("json", false, "print run.json")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: summarize takes one run directory", errUsage)
	}
	dir := pos[0]
	path := filepath.Join(dir, runner.RunFile)
	s, sumErr := runner.SummarizeDir(dir, runner.SummarizeOptions{})
	if sumErr != nil && s.Schema == "" {
		return sumErr
	}
	write := !*dry
	if sumErr != nil {
		fmt.Fprintf(e.stderr, "q2bot summarize: the traces do not read whole: %v\n", sumErr)
		if _, err := os.Stat(path); err == nil && !*force && write {
			fmt.Fprintf(e.stderr, "q2bot summarize: %s left as it is (-force rewrites it from the partial traces)\n", path)
			write = false
		}
	}
	if write {
		if err := metrics.WriteJSON(path, s); err != nil {
			return err
		}
	}
	if *asJSON {
		writeJSON(e.stdout, s)
	} else {
		printSummary(e.stdout, &s, dir)
	}
	if sumErr != nil {
		return errFailed
	}
	return nil
}

func runValidate(e *env, args []string) error {
	fs := newFlags("validate", e)
	minShare := fs.Float64("min-model-share", 0, "enforce the provenance gate with this model share (0: not enforced)")
	maxStale := fs.Float64("max-stale-rate", 0, "the gate's highest stale-answer rate (0: the run's, or 0.15)")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("%w: validate takes one run directory", errUsage)
	}
	if *minShare < 0 || *minShare > 1 || *maxStale < 0 || *maxStale > 1 {
		return fmt.Errorf("%w: shares are in 0..1", errUsage)
	}
	rep, err := runner.Validate(pos[0], runner.ValidateOptions{MinModelShare: *minShare, MaxStaleRate: *maxStale})
	if err != nil {
		return err
	}
	if *asJSON {
		writeJSON(e.stdout, rep)
	} else {
		printValidate(e.stdout, rep)
	}
	if !rep.OK() {
		return errFailed
	}
	return nil
}

func printValidate(w io.Writer, r *runner.ValidateReport) {
	verdict := "valid"
	if !r.OK() {
		verdict = fmt.Sprintf("INVALID (%d errors)", len(r.Errors))
	}
	fmt.Fprintf(w, "validate %s: %s\n", r.Dir, verdict)
	fmt.Fprintf(w, "  run %s: %d episodes, %d events, %d demos\n", r.Run, r.Episodes, r.Events, len(r.Demos))
	for _, d := range r.Demos {
		status := "ok"
		if d.Err != "" {
			status = d.Err
		}
		fmt.Fprintf(w, "    %s: %d frames, %d bytes, %s: %s\n", d.File, d.Stats.Frames, d.Stats.Bytes, d.Stats.Map, status)
	}
	if g := r.Gate; g != nil {
		v := "met"
		if !g.Passed {
			v = "NOT met: " + strings.Join(g.Reasons, "; ")
		}
		fmt.Fprintf(w, "  provenance gate (min share %.2f, max stale %.2f): %s\n", g.MinModelShare, g.MaxStaleRate, v)
	}
	for _, m := range r.Warnings {
		fmt.Fprintf(w, "  warning: %s\n", m)
	}
	for _, m := range r.Errors {
		fmt.Fprintf(w, "  error: %s\n", m)
	}
}
