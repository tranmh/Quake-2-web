// Command q2bot runs the agent headlessly and inspects its runs:
//
//	q2bot run [-maps demo1,demo2,demo3 | -campaign fixtures/agent/routes] [-backend scripted|jev|mock|replay|constant|random]
//	          [-session lockstep|inproc] [-sim-latency 212ms | 80ms,150ms,300ms] [-episodes n] [-seed n] [-out runs]
//	          [-trace full|digest|every:N] [-record] [-budget-usd x] [-budget-queries n] [-max-qps x] [-on-exhausted fallback|stop]
//	          [-require-complete] [-min-model-share 0.7] [-json] [-pak pak0.pak] [-nav-dir assets/nav]
//	          [-mock-policy noisy|scripted] [-mock-noise 0.3] [-mock-swap 0.1] [-mock-lowconf 0.1]
//	q2bot replay -trace runs/<id>/ep-000/trace.jsonl.gz [-mode actions|responses] [-strict] [-out dir] [-json]
//	q2bot summarize runs/<id> [-dry-run] [-force] [-json]
//	q2bot validate runs/<id> [-min-model-share 0.7] [-json]
//	q2bot jev-probe [-lane fast|slow] [-n 1] [-out live-probe.json]
//
// run plays the campaign (package agent/runner) and writes the run
// directory runs/<id>/{run.json, ep-NNN/{episode.json, trace.jsonl.gz,
// demos/NN-<map>.dm2, log.txt}}; the run id is generated. replay re-runs a
// recorded lockstep episode and reports the first divergence. summarize
// recomputes run.json from the traces. validate checks the traces, every
// demo and, with -min-model-share, the provenance gate. jev-probe sends
// one real request to the Jev API and records the exchange.
//
// The Jev API key is read from the environment only (TYPESAFE_API_KEY,
// with JEV_BASE_URL, JEV_MODEL and JEV_ALLOW_CUSTOM_BASE); no flag takes
// it.
//
// Exit status: 0 ok; 1 the run failed, did not complete (-require-complete),
// missed the provenance gate (-min-model-share), diverged (replay), is
// invalid (validate) or its traces do not read whole (summarize, which
// then keeps an existing run.json unless -force); 2 a usage error (a
// configuration the runner rejects included, or jev-probe without a key).
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// command is one q2bot subcommand.
type command struct {
	name, usage string
	run         func(env *env, args []string) error
}

// env is a command's environment.
type env struct {
	stdout, stderr io.Writer
	getenv         func(string) string
}

func commands() []command {
	return []command{
		{"run", "run [-maps a,b | -campaign dir] [-backend b] [-session s] [-sim-latency d] [-episodes n] [-seed n] [-out dir] " +
			"[-trace m] [-record] [-budget-usd x] [-budget-queries n] [-max-qps x] [-on-exhausted p] [-require-complete] " +
			"[-min-model-share x] [-json] [-pak file] [-nav-dir dir] [-mock-policy noisy|scripted] [-mock-noise x] [-mock-swap x] " +
			"[-mock-lowconf x] (-h for all)", runRun},
		{"replay", "replay -trace runs/<id>/ep-NNN/trace.jsonl.gz [-mode actions|responses] [-strict] [-out dir] [-json] [-pak file]", runReplay},
		{"summarize", "summarize runs/<id> [-dry-run] [-force] [-json]", runSummarize},
		{"validate", "validate runs/<id> [-min-model-share x] [-max-stale-rate x] [-json]", runValidate},
		{"jev-probe", "jev-probe [-lane fast|slow] [-n 1] [-out file] [-state lane-state.json] (needs TYPESAFE_API_KEY)", runProbe},
	}
}

// errUsage marks a command-line error (exit status 2); errFailed a
// command that ran and failed its check (exit status 1, already
// reported); errNoKey jev-probe without an API key (exit status 2,
// already explained).
var (
	errUsage  = errors.New("usage")
	errFailed = errors.New("failed")
	errNoKey  = errors.New("no API key")
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// run executes one subcommand and returns the exit status.
func run(args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	e := &env{stdout: stdout, stderr: stderr, getenv: getenv}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		err := c.run(e, args[1:])
		switch {
		case err == nil:
			return 0
		case errors.Is(err, flag.ErrHelp):
			fmt.Fprintf(stderr, "usage: q2bot %s\n", c.usage)
			return 2
		case errors.Is(err, errUsage):
			fmt.Fprintf(stderr, "q2bot %s: %v\nusage: q2bot %s\n", c.name, err, c.usage)
			return 2
		case errors.Is(err, errFailed):
			return 1
		case errors.Is(err, errNoKey):
			return 2
		default:
			fmt.Fprintf(stderr, "q2bot %s: %v\n", c.name, err)
			return 1
		}
	}
	fmt.Fprintf(stderr, "q2bot: unknown command %q\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: q2bot <command> [flags]")
	for _, c := range commands() {
		fmt.Fprintf(w, "  q2bot %s\n", c.usage)
	}
}

// newFlags returns a flag set that reports errors instead of exiting.
func newFlags(name string, e *env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() {
		fmt.Fprintf(e.stderr, "flags of q2bot %s:\n", name)
		fs.PrintDefaults()
	}
	return fs
}

// parseFlags parses args, which may mix flags and positional arguments
// ("validate runs/x -json"), and returns the positional ones.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", errUsage, err)
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// repoRoot returns the repository root (the directory holding
// server/go.mod) above the working directory, "" if none.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil && !st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// defaultPak is the demo pak: $Q2_BASEDIR/baseq2/pak0.pak, else the
// repository's assets/demo/baseq2/pak0.pak.
func defaultPak(getenv func(string) string) string {
	if d := getenv("Q2_BASEDIR"); d != "" {
		return filepath.Join(d, "baseq2", "pak0.pak")
	}
	if root := repoRoot(); root != "" {
		return filepath.Join(root, "assets", "demo", "baseq2", "pak0.pak")
	}
	return filepath.Join("assets", "demo", "baseq2", "pak0.pak")
}

// splitList splits a comma separated list, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
