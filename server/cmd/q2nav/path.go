package main

import (
	"container/heap"
	"fmt"
	"io"
	"strconv"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
)

func runPath(args []string, stdout io.Writer) error {
	fs := newFlags("path")
	pakFile := fs.String("pak", "", "pak file holding the map")
	name := fs.String("map", "", "level name (e.g. demo1)")
	from := fs.String("from", "spawn", `start: "x,y,z", "spawn" or "spawn:<targetname>"`)
	to := fs.String("to", "", `goal: "x,y,z", or "ent:<lump index>" for an edge that sets that entity off`)
	dir := fs.String("nav", "", "nav cache directory (default <repo>/assets/nav)")
	skill := fs.Int("skill", 1, "skill level")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *name == "" || *to == "" {
		return fmt.Errorf("%w: -map and -to are required", errUsage)
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
	start, err := pathEnd(g, md, *from)
	if err != nil {
		return err
	}
	goal := func(e *nav.Edge) bool { return false }
	var goalNode nav.NodeID = nav.NoNode
	if ent, ok := strings.CutPrefix(*to, "ent:"); ok {
		n, err := strconv.Atoi(ent)
		if err != nil {
			return fmt.Errorf("%w: bad -to %q", errUsage, *to)
		}
		goal = func(e *nav.Edge) bool { return e.HasEffect(n) }
	} else {
		if goalNode, err = pathEnd(g, md, *to); err != nil {
			return err
		}
	}
	states := navbuild.RouteStates(g, md)
	edges, cost, ok := dijkstra(g, start, goalNode, goal, func(e *nav.Edge) bool { return nav.Holds(e, states) })
	if !ok {
		return fmt.Errorf("no path from node %d to %s", start, *to)
	}
	fmt.Fprintf(stdout, "path %d edges, %.2f s\n", len(edges), cost)
	for _, i := range edges {
		e := &g.Edges[i]
		fmt.Fprintf(stdout, "  %-9s %5d %v -> %5d %v  %.2fs", e.Kind, e.From, g.Nodes[e.From].Origin, e.To, g.Nodes[e.To].Origin, e.Cost)
		if len(e.Reqs) > 0 {
			fmt.Fprintf(stdout, "  needs")
			for _, r := range e.Reqs {
				b := &g.Blockers[r.Blocker]
				fmt.Fprintf(stdout, " %s#%d=%b", b.Model, b.Entity, r.States)
			}
		}
		for _, f := range e.Effects {
			fmt.Fprintf(stdout, "  %s#%d", f.Kind, f.Entity)
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

// pathEnd resolves "x,y,z" (nearest node) or "spawn[:targetname]".
func pathEnd(g *nav.Graph, md *mapdata.Map, s string) (nav.NodeID, error) {
	if sp, ok := strings.CutPrefix(s, "spawn"); ok {
		sp = strings.TrimPrefix(sp, ":")
		spot, found := md.SpawnPoint(sp)
		if !found {
			return nav.NoNode, fmt.Errorf("no spawn point %q", sp)
		}
		for _, x := range g.Spawns {
			if int(x.Entity) == spot.Entity && x.Node != nav.NoNode {
				return x.Node, nil
			}
		}
		return nav.NoNode, fmt.Errorf("spawn point #%d has no node", spot.Entity)
	}
	var v [3]float32
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return nav.NoNode, fmt.Errorf("%w: bad position %q", errUsage, s)
	}
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nav.NoNode, fmt.Errorf("%w: bad position %q", errUsage, s)
		}
		v[i] = float32(f)
	}
	n := g.Localize(v, 128)
	if n == nav.NoNode {
		return n, fmt.Errorf("no node within 128 units of %v", v)
	}
	return n, nil
}

type pqItem struct {
	n    nav.NodeID
	cost float32
}

type pq []pqItem

func (q pq) Len() int { return len(q) }
func (q pq) Less(i, j int) bool {
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return q[i].n < q[j].n
}
func (q pq) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *pq) Push(x any)   { *q = append(*q, x.(pqItem)) }
func (q *pq) Pop() any {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// dijkstra finds the cheapest path from start to goalNode, or to the first
// edge goal accepts, over the edges ok accepts. It returns edge indexes.
func dijkstra(g *nav.Graph, start, goalNode nav.NodeID, goal func(*nav.Edge) bool, ok func(*nav.Edge) bool) ([]int, float32, bool) {
	dist := make([]float32, len(g.Nodes))
	via := make([]int, len(g.Nodes))
	done := make([]bool, len(g.Nodes))
	for i := range dist {
		dist[i], via[i] = -1, -1
	}
	dist[start] = 0
	q := &pq{{start, 0}}
	finalEdge := -1
	for q.Len() > 0 {
		it := heap.Pop(q).(pqItem)
		if done[it.n] {
			continue
		}
		done[it.n] = true
		if it.n == goalNode {
			break
		}
		lo, hi := g.OutRange(it.n)
		found := false
		for i := lo; i < hi; i++ {
			e := &g.Edges[i]
			if !ok(e) {
				continue
			}
			if goal(e) {
				finalEdge, found = i, true
				break
			}
			c := it.cost + e.Cost
			if dist[e.To] < 0 || c < dist[e.To] {
				dist[e.To], via[e.To] = c, i
				heap.Push(q, pqItem{e.To, c})
			}
		}
		if found {
			break
		}
	}
	var path []int
	end := goalNode
	total := float32(0)
	if finalEdge >= 0 {
		path = append(path, finalEdge)
		end = g.Edges[finalEdge].From
		total = dist[end] + g.Edges[finalEdge].Cost
	} else {
		if goalNode == nav.NoNode || dist[goalNode] < 0 {
			return nil, 0, false
		}
		total = dist[goalNode]
	}
	for n := end; n != start; {
		i := via[n]
		if i < 0 {
			return nil, 0, false
		}
		path = append(path, i)
		n = g.Edges[i].From
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, total, true
}
