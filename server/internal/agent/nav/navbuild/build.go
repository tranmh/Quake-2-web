// Package navbuild builds the navigation graph of a level from its BSP:
//
//  1. nodes: ground samples on a world-aligned grid over the upward faces of
//     the solid brushes (settled with a hull trace and one pmove step that
//     must land on ground), crouch nodes where only the ducked hull fits,
//     swim nodes in water, ladder nodes along ladder brushes, nodes on top of
//     movers for each mover pose, and the resting spot of every spawn point;
//  2. candidate edges: grid neighbors, targets below or across from ledges,
//     ladder chains, water and shore links, button/trigger/item approaches,
//     rides between mover poses and teleporters;
//  3. validation: a straight-line fast path for flat and step edges, else
//     the navsim executors are simulated with the bit-exact pmove until the
//     player arrives;
//  4. conditions (which mover poses, walls, lasers an edge's swept hull is
//     compatible with) and effects (triggers, buttons, items it sets off);
//  5. a degree cap, then running entries: every simulated edge is replayed
//     right after walking into its start node at full speed, and flagged
//     nav.EdgeFromRest when that fails;
//  6. pruning to what is reachable from a spawn point, and regions.
//
// The result does not depend on the number of workers: every job is a pure
// function of the map and the params, and results are merged in a fixed
// order, so two builds produce byte-identical files.
package navbuild

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
)

// Config are the build settings.
type Config struct {
	// Params zero means nav.DefaultParams().
	Params nav.Params
	// Workers is the number of parallel workers (0: GOMAXPROCS).
	Workers int
	// Logf, when set, reports the stages.
	Logf func(format string, args ...any)
	// Clip, when set, builds only the nodes inside the box (Min, Max): a
	// part of a map, for tests and for debugging one area. The result is
	// still deterministic, but it is not a full graph (do not cache it).
	Clip *[2]nav.Vec3
}

// Stage is the timing of one build stage.
type Stage struct {
	Name  string
	Took  time.Duration
	Count int
}

// Report describes a build (timings are not part of the graph).
type Report struct {
	Map     string
	Workers int
	Stages  []Stage
	Total   time.Duration
	// Candidates is the number of candidate edges tried, Sims the executor
	// simulations run, Fast the edges accepted by the fast path.
	Candidates, Sims, Fast int64
	Nodes, Edges           int
	// FromRest and Fragile are the numbers of edges (before pruning)
	// flagged nav.EdgeFromRest and nav.EdgeFragile; StopFails the FromRest
	// edges that also fail when the player first stops at the start
	// (navsim.StopAt) after a running entry.
	FromRest, Fragile, StopFails int
}

func (r *Report) String() string {
	s := fmt.Sprintf("%s: %d nodes, %d edges in %v (%d workers; %d candidates, %d sims, %d fast; %d from rest (%d fail after stopping), %d fragile)\n",
		r.Map, r.Nodes, r.Edges, r.Total.Round(time.Millisecond), r.Workers, r.Candidates, r.Sims, r.Fast, r.FromRest, r.StopFails, r.Fragile)
	for _, st := range r.Stages {
		s += fmt.Sprintf("  %-10s %8v  %d\n", st.Name, st.Took.Round(time.Millisecond), st.Count)
	}
	return s
}

// fileGeo is the parsed BSP and its collision model.
type fileGeo struct {
	f  *bsp.File
	cm *cmodel.Map
}

// world returns a fresh World with the scene's static solids.
func (g *fileGeo) world(sc *scene) *navsim.World {
	w := navsim.NewWorld(g.cm)
	w.SetSolids(sc.statics)
	return w
}

type builder struct {
	ctx     context.Context
	cfg     Config
	p       nav.Params
	phys    navsim.Physics
	name    string
	geo     *fileGeo
	sc      *scene
	workers []*worker
	rep     *Report

	nodes      []bnode
	cells      map[uint64][]int32
	touchEdges []nav.Edge
	edges      []nav.Edge
	// final maps builder node indexes to graph node ids (-1: pruned)
	final []nav.NodeID

	sims, cands, fast atomic.Int64
}

// support is a blocker pose an edge or node stands on.
type support struct {
	b    int32
	pose int8
}

type worker struct {
	b      *builder
	w      *navsim.World
	r      *navsim.Runner
	out    navsim.Outcome
	solids []navsim.Solid
}

func (b *builder) newWorker() *worker {
	w := navsim.NewWorld(b.geo.cm)
	r := navsim.NewRunner(w, b.phys)
	r.Pushes = b.sc.pushes
	for _, t := range b.sc.teles {
		r.Teleports = append(r.Teleports, navsim.Teleport{ID: int(t.entity), Min: t.min, Max: t.max, Dest: t.dest, Angles: t.angles})
	}
	r.Volumes = make([]navsim.Volume, len(b.sc.vols))
	for i, v := range b.sc.vols {
		r.Volumes[i] = navsim.Volume{ID: i, Min: v.min, Max: v.max}
	}
	wk := &worker{b: b, w: w, r: r}
	wk.setWorld()
	return wk
}

// setWorld makes the worker's world the static solids plus the given
// blocker poses.
func (wk *worker) setWorld(sup ...support) {
	wk.solids = append(wk.solids[:0], wk.b.sc.statics...)
	for _, s := range sup {
		if s.b >= 0 && wk.b.sc.geo[s.b].solid {
			wk.solids = append(wk.solids, wk.b.sc.solidAt(s.b, int(s.pose)))
		}
	}
	wk.w.SetSolids(wk.solids)
}

// setSpawnWorld is setWorld with every other solid blocker in its spawn
// state (the level as it starts: doors closed, walls up).
func (wk *worker) setSpawnWorld(sup ...support) {
	wk.solids = append(wk.solids[:0], wk.b.sc.statics...)
	sc := wk.b.sc
	for i := range sc.blockers {
		bl, g := &sc.blockers[i], &sc.geo[i]
		if !g.solid || g.laser {
			continue
		}
		pose := int(bl.Spawn)
		for _, s := range sup {
			if int(s.b) == i {
				pose = int(s.pose)
			}
		}
		if pose < 0 {
			continue
		}
		wk.solids = append(wk.solids, sc.solidAt(int32(i), pose))
	}
	wk.w.SetSolids(wk.solids)
}

// Build builds the skill-independent graph of map name from its BSP bytes.
func Build(ctx context.Context, name string, raw []byte, cfg Config) (*nav.Graph, *Report, error) {
	t0 := time.Now()
	p := cfg.Params
	if p == (nav.Params{}) {
		p = nav.DefaultParams()
	}
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	nw := cfg.Workers
	if nw <= 0 {
		nw = runtime.GOMAXPROCS(0)
	}
	f, err := bsp.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("navbuild %s: %w", name, err)
	}
	var maps [4]*mapdata.Map
	for s := range maps {
		if maps[s], err = mapdata.Load(name, raw, mapdata.Options{Skill: s}); err != nil {
			return nil, nil, fmt.Errorf("navbuild: %w", err)
		}
	}
	b := &builder{ctx: ctx, cfg: cfg, p: p, phys: p.Physics(), name: name,
		geo: &fileGeo{f: f, cm: maps[1].CM}, rep: &Report{Map: name, Workers: nw}}
	if b.sc, err = newScene(maps, b.geo); err != nil {
		return nil, nil, err
	}
	for i := 0; i < nw; i++ {
		b.workers = append(b.workers, b.newWorker())
	}

	stages := []struct {
		name string
		fn   func() (int, error)
	}{
		{"nodes", b.buildNodes},
		{"touch", b.buildTouchEnds},
		{"ledges", b.findLedges},
		{"edges", b.buildEdges},
		{"entry", b.checkEntries},
		{"finish", b.finish},
	}
	for _, st := range stages {
		ts := time.Now()
		n, err := st.fn()
		if err != nil {
			return nil, nil, err
		}
		b.rep.Stages = append(b.rep.Stages, Stage{Name: st.name, Took: time.Since(ts), Count: n})
		b.logf("navbuild %s: %s: %d in %v", name, st.name, n, time.Since(ts).Round(time.Millisecond))
	}
	g, err := b.graph()
	if err != nil {
		return nil, nil, err
	}
	b.rep.Total = time.Since(t0)
	b.rep.Candidates, b.rep.Sims, b.rep.Fast = b.cands.Load(), b.sims.Load(), b.fast.Load()
	b.rep.Nodes, b.rep.Edges = len(g.Nodes), len(g.Edges)
	return g, b.rep, nil
}

// StoreBuilder returns a nav.BuildFunc that reads maps/<name>.bsp with read
// (for example a pak file system's ReadFile) and builds it.
func StoreBuilder(read func(name string) ([]byte, error), cfg Config) nav.BuildFunc {
	return func(ctx context.Context, md *mapdata.Map, p nav.Params) (*nav.Graph, error) {
		raw, err := read("maps/" + md.Name + ".bsp")
		if err != nil {
			return nil, err
		}
		c := cfg
		c.Params = p
		g, _, err := Build(ctx, md.Name, raw, c)
		if err != nil {
			return nil, err
		}
		if g.Checksum != md.Checksum {
			return nil, fmt.Errorf("navbuild: %s: BSP checksum %08x, map data %08x", md.Name, g.Checksum, md.Checksum)
		}
		return g, nil
	}
}

func (b *builder) logf(format string, args ...any) {
	if b.cfg.Logf != nil {
		b.cfg.Logf(format, args...)
	}
}

// parallel runs fn(wk, i) for i in [0, n) on the workers. Each call must
// only write its own result slot.
func (b *builder) parallel(n int, fn func(wk *worker, i int)) error {
	var next atomic.Int64
	var wg sync.WaitGroup
	var once sync.Once
	var perr error
	for _, wk := range b.workers {
		wg.Add(1)
		go func(wk *worker) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					once.Do(func() { perr = fmt.Errorf("navbuild %s: worker panic: %v", b.name, r) })
					next.Store(int64(n))
				}
			}()
			for {
				i := int(next.Add(1) - 1)
				if i >= n || b.ctx.Err() != nil {
					return
				}
				fn(wk, i)
			}
		}(wk)
	}
	wg.Wait()
	if perr != nil {
		return perr
	}
	return b.ctx.Err()
}
