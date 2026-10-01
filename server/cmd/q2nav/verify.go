package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/route"
)

// Gates of verify: every re-simulated edge must arrive, and at least
// liveGate of the live-server samples must reproduce the simulation.
const liveGate = 0.95

func runVerify(args []string, stdout io.Writer) error {
	fs := newFlags("verify")
	pakFile := fs.String("pak", "", "pak file holding the map")
	name := fs.String("map", "", "level name (e.g. demo1)")
	sample := fs.Int("sample", 300, "edges to re-simulate with navsim (0: all)")
	liveN := fs.Int("live", 40, "edges to execute on the live lockstep server (0: none)")
	seed := fs.Int64("seed", 1, "sampling and server seed")
	dir := fs.String("nav", "", "nav cache directory (default <repo>/assets/nav)")
	routes := fs.String("routes", "", "route table directory for the coverage check (default fixtures/agent/routes, skipped when absent)")
	skill := fs.Int("skill", 1, "skill level")
	verbose := fs.Bool("v", false, "list every failure")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("%w: -map is required", errUsage)
	}
	src, err := openPak(*pakFile, mapdata.Options{Skill: *skill})
	if err != nil {
		return err
	}
	defer src.Close()
	g, md, err := loadGraph(src, *name, navDir(*dir))
	if err != nil {
		return err
	}
	s := g.Stats()
	fmt.Fprintf(stdout, "%s: %d nodes, %d edges (%d conditional, %d fast) skill %d\n", g.Map, s.Nodes, s.Edges, s.Conditional, s.Fast, g.Skill)
	var failed []string

	// 1. re-simulation
	idx := sampleEdges(g, *sample, *seed, func(e *nav.Edge) bool { return e.Kind != nav.EdgeRide })
	v := navbuild.NewVerifier(g, md.CM)
	byKind := map[string][2]int{}
	pass := 0
	for _, i := range idx {
		r := v.Edge(i)
		k := kindKey(&g.Edges[i])
		c := byKind[k]
		c[1]++
		if r.OK {
			c[0]++
			pass++
		} else if *verbose || len(failed) < 10 {
			fmt.Fprintf(stdout, "  resim FAIL edge %d: %s\n", i, r.Reason)
		}
		byKind[k] = c
	}
	fmt.Fprintf(stdout, "resim: %d/%d edges arrive (%s)\n", pass, len(idx), kindSummary(byKind))
	if pass < len(idx) {
		failed = append(failed, fmt.Sprintf("resim %d/%d", pass, len(idx)))
	}

	// 2. live server
	if *liveN > 0 {
		res, err := verifyLive(stdout, src, g, md, *liveN, *seed, *skill, *verbose)
		if err != nil {
			return err
		}
		if res.total > 0 && float64(res.pass) < liveGate*float64(res.total) {
			failed = append(failed, fmt.Sprintf("live %d/%d", res.pass, res.total))
		}
	}

	// 3. route coverage
	if rd := findRoutes(*routes); rd != "" {
		c, err := route.Load(rd)
		if err != nil {
			return err
		}
		for _, t := range c.Tables {
			if !strings.EqualFold(t.Map, g.Map) {
				continue
			}
			probs, err := navbuild.CheckRoute(g, md, t)
			if err != nil {
				return err
			}
			for _, p := range probs {
				fmt.Fprintf(stdout, "  route: %v\n", p)
			}
			fmt.Fprintf(stdout, "route %s: %d of %d steps covered\n", t.Name, len(t.Steps)-len(probs), len(t.Steps))
			if len(probs) > 0 {
				failed = append(failed, fmt.Sprintf("route %s", t.Name))
			}
		}
	}
	if len(failed) > 0 {
		return errors.New("verify failed: " + strings.Join(failed, ", "))
	}
	fmt.Fprintln(stdout, "verify: ok")
	return nil
}

func findRoutes(flag string) string {
	if flag != "" {
		return flag
	}
	for _, d := range defaultRoutes() {
		if _, err := os.Stat(filepath.Join(d, route.CampaignFile)); err == nil {
			return d
		}
	}
	return ""
}

func kindKey(e *nav.Edge) string {
	k := e.Kind.String()
	if e.Flags&nav.EdgeFast != 0 {
		k += "/fast"
	}
	return k
}

func kindSummary(m map[string][2]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d/%d", k, m[k][0], m[k][1]))
	}
	return strings.Join(parts, ", ")
}

// sampleEdges picks n edges that ok accepts (all when n <= 0 or there are
// fewer), deterministically for a seed, spread over the edge kinds: every
// kind gets at least min(10, its count) samples, the rest proportionally.
func sampleEdges(g *nav.Graph, n int, seed int64, ok func(*nav.Edge) bool) []int {
	byKind := map[string][]int{}
	total := 0
	for i := range g.Edges {
		if ok(&g.Edges[i]) {
			k := kindKey(&g.Edges[i])
			byKind[k] = append(byKind[k], i)
			total++
		}
	}
	keys := make([]string, 0, len(byKind))
	for k := range byKind {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rng := rand.New(rand.NewSource(seed))
	var out []int
	for _, k := range keys {
		list := byKind[k]
		want := len(list)
		if n > 0 && total > n {
			want = int(math.Ceil(float64(n) * float64(len(list)) / float64(total)))
			if m := min(10, len(list)); want < m {
				want = m
			}
			if want > len(list) {
				want = len(list)
			}
		}
		perm := rng.Perm(len(list))
		for _, p := range perm[:want] {
			out = append(out, list[p])
		}
	}
	sort.Ints(out)
	return out
}

type liveResult struct{ pass, exact, total int }

// liveEligible: edges whose conditions hold in the level as it starts and
// that set nothing off but item pickups (a door opening or a trigger firing
// would change the world for the next sample).
func liveEligible(g *nav.Graph, spawn []nav.StateMask) func(*nav.Edge) bool {
	return func(e *nav.Edge) bool {
		switch e.Kind {
		case nav.EdgeRide, nav.EdgeTouch, nav.EdgeTeleport:
			return false
		}
		if e.Flags&nav.EdgePushes != 0 {
			return false // the barrel it brushes moves on the server
		}
		if !nav.Holds(e, spawn) {
			return false
		}
		for _, f := range e.Effects {
			if f.Kind != nav.EffItem {
				return false
			}
		}
		return true
	}
}

// verifyLive executes a sample of edges on a live lockstep server. For each
// edge the player is put at the start node; the edge is re-simulated
// offline from the state the server reports, and the same commands are
// sent to the server. An edge passes when the server ends where the
// simulation does (within one unit; usually bit for bit).
func verifyLive(stdout io.Writer, src *mapSource, g *nav.Graph, md *mapdata.Map, n int, seed int64, skill int, verbose bool) (liveResult, error) {
	ctx := context.Background()
	idx := sampleEdges(g, n, seed, liveEligible(g, g.SpawnStates()))
	if len(idx) > n {
		rng := rand.New(rand.NewSource(seed + 1))
		rng.Shuffle(len(idx), func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
		idx = idx[:n]
		sort.Ints(idx)
	}
	if len(idx) == 0 {
		fmt.Fprintln(stdout, "live: no eligible edges")
		return liveResult{}, nil
	}
	park := parkNode(g, idx)
	start := func() (*liveServer, error) {
		ls, err := startLive(ctx, src.p, g.Map, skill, uint32(seed))
		if err != nil {
			return nil, err
		}
		if err := ls.quiesce(ctx, g, park); err != nil {
			ls.Close()
			return nil, err
		}
		return ls, nil
	}
	ls, err := start()
	if err != nil {
		return liveResult{}, err
	}
	defer func() { ls.Close() }()
	fmt.Fprintf(stdout, "live: %s on a lockstep server, %d monsters removed, god+notarget\n", g.Map, ls.freed)
	v := navbuild.NewVerifier(g, md.CM)
	var res liveResult
	var reasons []string
	restarts := 0
	prev := idx[0]
	for _, i := range idx {
		if why := ls.changed(); why != "" {
			// the last sample set something off: start over from the level
			// as it starts
			if verbose {
				fmt.Fprintf(stdout, "  live restart: %s (after edge %d %s %v -> %v)\n", why, prev, kindKey(&g.Edges[prev]), g.Nodes[g.Edges[prev].From].Origin, g.Nodes[g.Edges[prev].To].Origin)
			}
			ls.Close()
			if ls, err = start(); err != nil {
				return res, err
			}
			restarts++
		}
		e := &g.Edges[i]
		from := g.Node(e.From)
		start, err := ls.place(ctx, from)
		if err != nil {
			return res, err
		}
		// simulate in the level as the server has it: the edge's conditions
		// hold there, and the padding after arrival may run into a blocker
		// that the edge's own (optimistic) world leaves out
		poses := g.SpawnPoses()
		g.SetWorld(v.World(), poses)
		st := stateOf(start, v.World())
		sim := v.EdgeIn(i, st, poses)
		res.total++
		why := ""
		switch {
		case !sim.OK:
			why = "offline re-simulation from the server state fails: " + sim.Reason
		default:
			hold := nav.HoldCmd(g.Node(e.To))
			frames, err := ls.run(ctx, sim.Cmds, hold)
			if err != nil {
				return res, err
			}
			// continue the simulation with the same padding
			r := v.Runner()
			for k := len(sim.Cmds); k%4 != 0; k++ {
				r.Step(hold)
			}
			off := r.State().PM.Origin
			on := frames[len(frames)-1].Origin
			if os.Getenv("Q2NAV_TRACE") != "" {
				fmt.Fprintf(stdout, "    edge %d %s start %v (node %v) -> server %v sim %v target %v, %d cmds, %d frames\n",
					i, kindKey(e), shortVec(start.Origin), from.Origin, shortVec(on), shortVec(off), g.Nodes[e.To].Origin, len(sim.Cmds), len(frames))
			}
			d := shortDist(off, on)
			switch {
			case d == 0:
				res.pass++
				res.exact++
			case d <= 1:
				res.pass++
			default:
				why = fmt.Sprintf("server ended at %v, simulation at %v (%.1f units apart)", shortVec(on), shortVec(off), d)
			}
		}
		prev = i
		if why != "" {
			msg := fmt.Sprintf("edge %d %s %v -> %v: %s", i, kindKey(e), from.Origin, g.Nodes[e.To].Origin, why)
			if verbose || len(reasons) < 10 {
				fmt.Fprintf(stdout, "  live FAIL %s\n", msg)
			}
			reasons = append(reasons, msg)
		}
	}
	fmt.Fprintf(stdout, "live: %d/%d edges reproduce on the server (%d bit-exact, %d server restarts)\n", res.pass, res.total, res.exact, restarts)
	return res, nil
}

// parkNode returns a start node of the sample that touches no volume (a
// player resting in a door's trigger box keeps the door open, and the
// level never comes to rest).
func parkNode(g *nav.Graph, idx []int) *nav.Node {
	for _, i := range idx {
		n := g.Node(g.Edges[i].From)
		free := true
		for _, v := range g.Volumes {
			lo := navsim.Vec3{n.Origin[0] - 17, n.Origin[1] - 17, n.Origin[2] - 25}
			hi := navsim.Vec3{n.Origin[0] + 17, n.Origin[1] + 17, n.Origin[2] + 33}
			if !(lo[0] > v.Max[0] || lo[1] > v.Max[1] || lo[2] > v.Max[2] || hi[0] < v.Min[0] || hi[1] < v.Min[1] || hi[2] < v.Min[2]) {
				free = false
				break
			}
		}
		if free {
			return n
		}
	}
	return g.Node(g.Edges[idx[0]].From)
}

func shortVec(o [3]int16) navsim.Vec3 {
	return navsim.Vec3{float32(o[0]) / 8, float32(o[1]) / 8, float32(o[2]) / 8}
}

func shortDist(a, b [3]int16) float64 {
	var d float64
	for k := 0; k < 3; k++ {
		x := float64(int32(a[k])-int32(b[k])) / 8
		d += x * x
	}
	return math.Sqrt(d)
}
