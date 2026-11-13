package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"time"

	"quake2web/server/internal/agent/backend/cache"
	"quake2web/server/internal/agent/runner"
)

// cacheSimModes are the cache modes cache-sim compares, cacheSimTTLs the
// lifetimes of its TTL table (0: none).
var (
	cacheSimModes = []string{"exact", "approx:fine", "approx:coarse"}
	cacheSimTTLs  = []time.Duration{0, 10 * time.Second, 2 * time.Second, 500 * time.Millisecond}
)

// cacheSimRun is one run's simulation.
type cacheSimRun struct {
	Dir      string `json:"dir"`
	Run      string `json:"run"`
	Requests int    `json:"requests"`
	// ExactRepeats counts the requests whose digest an earlier one had,
	// with no causal rule.
	ExactRepeats int                `json:"exact_repeats_no_causal_rule"`
	Modes        []*cache.SimResult `json:"modes"`
}

// cacheSimReport is cache-sim's report.
type cacheSimReport struct {
	Runs   []cacheSimRun      `json:"runs"`
	Totals []*cache.SimResult `json:"totals"`
	// TTL is the hit rate of every mode under each TTL (totals).
	TTL  []*cache.SimResult `json:"ttl"`
	Note string             `json:"note"`
}

const cacheSimNote = "an estimate: a live cache answers a hit at once, which changes what the bot does and so which requests follow; " +
	"the recordings are of runs without the cache"

// runCacheSim replays recorded runs' requests through the decision cache
// (no backend is asked) and reports hits, misses, savings and how often a
// reused answer equals the one the backend gave.
func runCacheSim(e *env, args []string) error {
	fs := newFlags("cache-sim", e)
	asJSON := fs.Bool("json", false, "print the report as JSON")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fmt.Errorf("%w: cache-sim takes one or more run directories", errUsage)
	}
	rep := &cacheSimReport{Note: cacheSimNote}
	var runs [][]cache.SimRequest
	for _, dir := range pos {
		evs, err := runner.RunEvents(dir)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		reqs, err := cache.SimRequests(evs)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		run := cacheSimRun{Dir: dir, Run: filepath.Base(filepath.Clean(dir)), ExactRepeats: cache.ExactRepeats(reqs)}
		if len(evs) > 0 && evs[0].Run != "" {
			run.Run = evs[0].Run
		}
		for i := range reqs {
			if !reqs[i].Refused {
				run.Requests++
			}
		}
		for mi, m := range cacheSimModes {
			cfg, _ := cache.Parse(m)
			res := cache.Simulate(reqs, cfg)
			run.Modes = append(run.Modes, res)
			if len(rep.Totals) <= mi {
				rep.Totals = append(rep.Totals, cache.Simulate(nil, cfg))
			}
			rep.Totals[mi].Merge(res)
		}
		rep.Runs = append(rep.Runs, run)
		runs = append(runs, reqs)
	}
	for _, m := range cacheSimModes {
		for _, ttl := range cacheSimTTLs {
			cfg, _ := cache.Parse(m)
			cfg.TTL = ttl
			// per run (a store per run, as live), merged
			tot := cache.Simulate(nil, cfg)
			for _, reqs := range runs {
				tot.Merge(cache.Simulate(reqs, cfg))
			}
			rep.TTL = append(rep.TTL, tot)
		}
	}
	if *asJSON {
		writeJSON(e.stdout, rep)
		return nil
	}
	printCacheSim(e.stdout, rep)
	return nil
}

func printCacheSim(w io.Writer, r *cacheSimReport) {
	fmt.Fprintf(w, "cache-sim: %d runs (%s)\n", len(r.Runs), r.Note)
	header := func() {
		fmt.Fprintf(w, "  %-14s %-5s %8s %6s %7s %7s %9s %12s %10s\n", "mode", "lane", "requests", "hits", "misses", "hit%",
			"in-flight", "saved tok", "saved $")
	}
	row := func(mode, lane string, l cache.SimLane) {
		fmt.Fprintf(w, "  %-14s %-5s %8d %6d %7d %6.1f%% %9d %12d %10.6f\n", mode, lane, l.Requests, l.Hits, l.Misses, 100*l.HitRate,
			l.InFlight, l.SavedTokens, l.SavedUSD)
	}
	table := func(results []*cache.SimResult) {
		header()
		for _, res := range results {
			row(res.Mode, "fast", res.Lanes["fast"])
			row(res.Mode, "slow", res.Lanes["slow"])
			row(res.Mode, "all", res.Total)
		}
	}
	for _, run := range r.Runs {
		fmt.Fprintf(w, "\nrun %s: %d requests; exact repeats with no causal rule: %d\n", run.Run, run.Requests, run.ExactRepeats)
		table(run.Modes)
		for _, res := range run.Modes {
			if res.Total.RecordedHits > 0 {
				fmt.Fprintf(w, "  %s: the recording marks %d hits, the simulation finds %d of them\n", res.Mode, res.Total.RecordedHits,
					res.Total.RecordedAgree)
			}
		}
	}
	fmt.Fprintf(w, "\nall runs:\n")
	table(r.Totals)

	fmt.Fprintf(w, "\nanswer fidelity on hits (the reused answer equals the backend's own; score: same level rounded):\n")
	ids := map[string]bool{}
	for _, res := range r.Totals {
		for id := range res.Fidelity {
			ids[id] = true
		}
	}
	order := []string{"target", "fire_policy", "movement", "mode", "weapon", "pickup", "danger"}
	for id := range ids {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	fmt.Fprintf(w, "  %-14s", "mode")
	for _, id := range order {
		if ids[id] {
			fmt.Fprintf(w, " %18s", id)
		}
	}
	fmt.Fprintf(w, " %18s\n", "all questions")
	for _, res := range r.Totals {
		fmt.Fprintf(w, "  %-14s", res.Mode)
		for _, id := range order {
			if !ids[id] {
				continue
			}
			a := res.Fidelity[id]
			fmt.Fprintf(w, " %18s", agreement(a))
		}
		fmt.Fprintf(w, " %18s\n", agreement(res.AllEqual))
	}

	fmt.Fprintf(w, "\nhit rate by TTL (game time; all runs, a store per run):\n")
	fmt.Fprintf(w, "  %-14s", "mode")
	for _, ttl := range cacheSimTTLs {
		fmt.Fprintf(w, " %22s", ttlName(ttl))
	}
	fmt.Fprintln(w)
	for mi, m := range cacheSimModes {
		fmt.Fprintf(w, "  %-14s", m)
		for ti := range cacheSimTTLs {
			res := r.TTL[mi*len(cacheSimTTLs)+ti]
			fmt.Fprintf(w, " %22s", fmt.Sprintf("%.1f%% (%d, all eq %.0f%%)", 100*res.Total.HitRate, res.Total.Hits,
				100*res.AllEqual.Rate))
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "\nage of the reused answers (no TTL; all runs):\n")
	fmt.Fprintf(w, "  %-14s %8s %8s %8s %8s   %7s %7s %7s %7s %7s\n", "mode", "p10 ms", "p50 ms", "p90 ms", "max ms", "<0.5s",
		"0.5-2s", "2-10s", ">=10s", "other ep")
	for _, res := range r.Totals {
		q := res.AgeQuantiles
		if len(q) < 4 {
			q = []int64{0, 0, 0, 0}
		}
		sh := res.AgeShares()
		fmt.Fprintf(w, "  %-14s %8d %8d %8d %8d   %6.1f%% %6.1f%% %6.1f%% %6.1f%% %6.1f%%\n", res.Mode, q[0], q[1], q[2], q[3],
			100*sh[0], 100*sh[1], 100*sh[2], 100*sh[3], 100*sh[4])
	}
}

func agreement(a cache.Agreement) string {
	if a.N == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%% (n=%d)", 100*a.Rate, a.N)
}

func ttlName(d time.Duration) string {
	if d == 0 {
		return "ttl none"
	}
	return "ttl " + d.String()
}
