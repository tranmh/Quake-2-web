package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
)

// dumpFile is the compact visualization JSON for a dev overlay: positions
// as [x, y, z] arrays, enums as indexes into the name tables.
type dumpFile struct {
	Schema    string       `json:"schema"`
	Map       string       `json:"map"`
	Checksum  uint32       `json:"checksum"`
	Skill     int          `json:"skill"`
	EdgeKinds []string     `json:"edgeKinds"`
	NodeFlags []string     `json:"nodeFlags"`
	// Nodes: [x, y, z, flags, region]
	Nodes [][5]float32 `json:"nodes"`
	// Edges: [from, to, kind index, conditional 0/1]
	Edges   [][4]int32   `json:"edges"`
	Volumes []dumpVolume `json:"volumes"`
	Movers  []dumpMover  `json:"movers"`
	Lasers  []dumpLaser  `json:"lasers"`
	Spawns  []dumpSpawn  `json:"spawns"`
}

type dumpVolume struct {
	Kind   string     `json:"kind"`
	Entity int32      `json:"entity"`
	Class  string     `json:"class,omitempty"`
	Model  string     `json:"model,omitempty"`
	Pose   int8       `json:"pose"`
	Min    [3]float32 `json:"min"`
	Max    [3]float32 `json:"max"`
}

type dumpPose struct {
	Name string     `json:"name"`
	Min  [3]float32 `json:"min"`
	Max  [3]float32 `json:"max"`
}

type dumpMover struct {
	Entity int32      `json:"entity"`
	Class  string     `json:"class"`
	Model  string     `json:"model"`
	Kind   string     `json:"kind"`
	Spawn  int8       `json:"spawn"`
	Gone   bool       `json:"gone,omitempty"`
	Poses  []dumpPose `json:"poses"`
}

type dumpLaser struct {
	Entity int32      `json:"entity"`
	Start  [3]float32 `json:"start"`
	End    [3]float32 `json:"end"`
	On     bool       `json:"on"`
}

type dumpSpawn struct {
	Entity     int32      `json:"entity"`
	Targetname string     `json:"targetname,omitempty"`
	Origin     [3]float32 `json:"origin"`
	Node       nav.NodeID `json:"node"`
}

func runDump(args []string, stdout io.Writer) error {
	fs := newFlags("dump")
	pakFile := fs.String("pak", "", "pak file holding the map")
	name := fs.String("map", "", "level name (e.g. demo1)")
	out := fs.String("o", "", "output JSON file (default stdout)")
	dir := fs.String("nav", "", "nav cache directory (default <repo>/assets/nav)")
	skill := fs.Int("skill", 1, "skill level the conditions are resolved for")
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
	g, _, err := loadGraph(src, *name, navDir(*dir))
	if err != nil {
		return err
	}
	d := buildDump(g)
	w := stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		bw := bufio.NewWriter(f)
		defer bw.Flush()
		w = bw
	}
	if err := json.NewEncoder(w).Encode(d); err != nil {
		return err
	}
	if *out != "" {
		fmt.Fprintf(stdout, "%s: %d nodes, %d edges, %d volumes, %d movers, %d lasers -> %s\n",
			g.Map, len(d.Nodes), len(d.Edges), len(d.Volumes), len(d.Movers), len(d.Lasers), *out)
	}
	return nil
}

func buildDump(g *nav.Graph) *dumpFile {
	d := &dumpFile{Schema: "q2nav.dump/1", Map: g.Map, Checksum: g.Checksum, Skill: g.Skill,
		EdgeKinds: []string{""}, NodeFlags: nil}
	for k := nav.EdgeWalk; k <= nav.EdgeTouch; k++ {
		d.EdgeKinds = append(d.EdgeKinds, k.String())
	}
	for f := nav.NodeFlags(1); f != 0 && f <= nav.NodeEnd; f <<= 1 {
		d.NodeFlags = append(d.NodeFlags, f.String())
	}
	ents := map[int32]nav.Ent{}
	for _, e := range g.Ents {
		ents[e.Entity] = e
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		d.Nodes = append(d.Nodes, [5]float32{n.Origin[0], n.Origin[1], n.Origin[2], float32(n.Flags), float32(n.Region)})
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		c := int32(0)
		if e.Conditional() {
			c = 1
		}
		d.Edges = append(d.Edges, [4]int32{int32(e.From), int32(e.To), int32(e.Kind), c})
	}
	for _, v := range g.Volumes {
		e := ents[v.Entity]
		d.Volumes = append(d.Volumes, dumpVolume{Kind: v.Kind.String(), Entity: v.Entity, Class: e.Class, Model: e.Model, Pose: v.Pose, Min: v.Min, Max: v.Max})
	}
	for _, b := range g.Blockers {
		if b.Kind == nav.BlockLaser {
			d.Lasers = append(d.Lasers, dumpLaser{Entity: b.Entity, Start: b.Start, End: b.End, On: b.Spawn == 0})
			continue
		}
		m := dumpMover{Entity: b.Entity, Class: b.Class, Model: b.Model, Kind: b.Kind.String(), Spawn: b.Spawn, Gone: b.Gone}
		for _, p := range b.Poses {
			mn, mx := navsim.LinkBox(true, p.Origin, p.Angles, b.Mins, b.Maxs)
			m.Poses = append(m.Poses, dumpPose{Name: p.Name, Min: mn, Max: mx})
		}
		d.Movers = append(d.Movers, m)
	}
	for _, s := range g.Spawns {
		d.Spawns = append(d.Spawns, dumpSpawn{Entity: s.Entity, Targetname: s.Targetname, Origin: s.Origin, Node: s.Node})
	}
	return d
}
