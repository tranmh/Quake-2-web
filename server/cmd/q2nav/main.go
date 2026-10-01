// Command q2nav inspects the navigation knowledge the agent derives from a
// map and checks the checked-in route tables:
//
//	q2nav info -pak assets/demo/baseq2/pak0.pak -map demo1 [-skill 1]
//	q2nav plan -pak assets/demo/baseq2/pak0.pak [-routes fixtures/agent/routes] [-skill 1]
//
// info prints the entity counts and the mover, trigger, exit and laser
// tables of one map. plan prints, for every map of the pak, each exit with
// the logic chains that fire it, then validates every route table of the
// campaign against the pak and exits non-zero on any error.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/assets/pak"
)

// command is one q2nav subcommand.
type command struct {
	name, usage string
	run         func(args []string, stdout io.Writer) error
}

func commands() []command {
	return []command{
		{"info", "info -pak <pak> -map <name> [-skill n] [-deathmatch]", runInfo},
		{"plan", "plan -pak <pak> [-routes dir] [-skill n]", runPlan},
	}
}

// errUsage marks a command-line error (exit status 2).
var errUsage = errors.New("usage")

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one subcommand and returns the process exit status: 0 on
// success, 1 when the command fails (for plan: a route table is invalid),
// 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	for _, c := range commands() {
		if c.name != args[0] {
			continue
		}
		err := c.run(args[1:], stdout)
		switch {
		case err == nil:
			return 0
		case errors.Is(err, errUsage), errors.Is(err, flag.ErrHelp):
			if !errors.Is(err, flag.ErrHelp) {
				fmt.Fprintf(stderr, "q2nav %s: %v\n", c.name, err)
			}
			fmt.Fprintf(stderr, "usage: q2nav %s\n", c.usage)
			return 2
		default:
			fmt.Fprintf(stderr, "q2nav %s: %v\n", c.name, err)
			return 1
		}
	}
	fmt.Fprintf(stderr, "q2nav: unknown command %q\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: q2nav <command> [flags]")
	for _, c := range commands() {
		fmt.Fprintf(w, "  q2nav %s\n", c.usage)
	}
}

// newFlags returns a flag set that reports errors instead of exiting.
func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	return nil
}

// mapSource reads levels from a pak.
type mapSource struct {
	p   *pak.Pak
	opt mapdata.Options
}

func openPak(file string, opt mapdata.Options) (*mapSource, error) {
	if file == "" {
		return nil, fmt.Errorf("%w: -pak is required", errUsage)
	}
	p, err := pak.Open(file)
	if err != nil {
		return nil, err
	}
	return &mapSource{p: p, opt: opt}, nil
}

func (s *mapSource) Close() error { return s.p.Close() }

// load reads maps/<name>.bsp and derives its map data.
func (s *mapSource) load(name string) (*mapdata.Map, error) {
	raw, err := s.p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		return nil, fmt.Errorf("maps/%s.bsp: %w", name, err)
	}
	return mapdata.Load(name, raw, s.opt)
}

// maps lists the level names of the pak (maps/*.bsp), sorted.
func (s *mapSource) maps() []string {
	var out []string
	for _, f := range s.p.List() {
		name := strings.ToLower(f.Name)
		if strings.HasPrefix(name, "maps/") && strings.HasSuffix(name, ".bsp") && !strings.Contains(name[5:], "/") {
			out = append(out, strings.TrimSuffix(path.Base(name), ".bsp"))
		}
	}
	sort.Strings(out)
	return out
}
