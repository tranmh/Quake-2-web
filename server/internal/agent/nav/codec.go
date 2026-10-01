package nav

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"quake2web/server/internal/agent/nav/navsim"
)

// maxFileBytes bounds the decompressed size of a graph file.
const maxFileBytes = 1 << 30

// The file layout: compact keys, omitted zero fields, nodes and edges in
// graph order, so the same graph always encodes to the same bytes.
type fileGraph struct {
	Format   int           `json:"format"`
	Map      string        `json:"map"`
	Checksum uint32        `json:"checksum"`
	Physics  string        `json:"physics"`
	Params   Params        `json:"params"`
	Scene    [4]string     `json:"scene"`
	Blockers []fileBlocker `json:"blockers"`
	Ents     []fileEnt     `json:"ents"`
	Spawns   []fileSpawn   `json:"spawns"`
	Solids   []fileSolid   `json:"solids"`
	Volumes  []fileVolume  `json:"volumes"`
	Pushes   []filePush    `json:"pushes,omitempty"`
	Teles    []fileTele    `json:"teleports,omitempty"`
	Nodes    []fileNode    `json:"nodes"`
	Edges    []fileEdge    `json:"edges"`
}

type fileSolid struct {
	ID       int   `json:"id"`
	Headnode int32 `json:"headnode,omitempty"`
	Box      bool  `json:"box,omitempty"`
	Origin   Vec3  `json:"origin"`
	Angles   Vec3  `json:"angles"`
	Mins     Vec3  `json:"mins"`
	Maxs     Vec3  `json:"maxs"`
	Pushable bool  `json:"pushable,omitempty"`
}

type fileVolume struct {
	Kind    EffectKind `json:"kind"`
	Entity  int32      `json:"entity"`
	Pose    int8       `json:"pose"`
	Blocker int32      `json:"blocker"`
	Min     Vec3       `json:"min"`
	Max     Vec3       `json:"max"`
}

type filePush struct {
	ID       int  `json:"id"`
	Min      Vec3 `json:"min"`
	Max      Vec3 `json:"max"`
	Velocity Vec3 `json:"velocity"`
	Once     bool `json:"once,omitempty"`
}

type fileTele struct {
	ID     int  `json:"id"`
	Min    Vec3 `json:"min"`
	Max    Vec3 `json:"max"`
	Dest   Vec3 `json:"dest"`
	Angles Vec3 `json:"angles"`
}

type fileBlocker struct {
	Entity int32         `json:"entity"`
	Class  string        `json:"class"`
	Model  string        `json:"model,omitempty"`
	Kind   BlockerKind   `json:"kind"`
	Poses  []BlockerPose `json:"poses"`
	Gone   bool          `json:"gone,omitempty"`
	Spawn  int8          `json:"spawn"`
	Skills uint8         `json:"skills"`
	Start  *Vec3         `json:"start,omitempty"`
	End    *Vec3         `json:"end,omitempty"`
	Head   int32         `json:"headnode,omitempty"`
	Mins   Vec3          `json:"mins"`
	Maxs   Vec3          `json:"maxs"`
	Solid  bool          `json:"solid,omitempty"`
	Team   int32         `json:"team"`
}

type fileEnt struct {
	Entity   int32  `json:"entity"`
	Class    string `json:"class"`
	Model    string `json:"model,omitempty"`
	Skills   uint8  `json:"skills"`
	Disabled bool   `json:"disabled,omitempty"`
}

type fileSpawn struct {
	Entity     int32  `json:"entity"`
	Targetname string `json:"targetname,omitempty"`
	Origin     Vec3   `json:"origin"`
	Node       NodeID `json:"node"`
	Skills     uint8  `json:"skills"`
}

type fileNode struct {
	O Vec3      `json:"o"`
	F NodeFlags `json:"f,omitempty"`
	M int32     `json:"m,omitempty"` // blocker + 1
	P int8      `json:"p,omitempty"`
	R int32     `json:"r"`
	Y float32   `json:"y,omitempty"`
}

type fileEdge struct {
	F NodeID        `json:"f"`
	T NodeID        `json:"t"`
	K EdgeKind      `json:"k"`
	R navsim.Recipe `json:"r"`
	X EdgeFlags     `json:"x,omitempty"`
	C float32       `json:"c"`
	A *Vec3         `json:"a,omitempty"`
	O *Vec3         `json:"o,omitempty"`
	S float32       `json:"s,omitempty"`
	W int16         `json:"w,omitempty"`
	B int16         `json:"b,omitempty"`
	Y float32       `json:"y,omitempty"`
	D int16         `json:"d,omitempty"`
	H int16         `json:"h,omitempty"` // hazard damage
	G int32         `json:"g,omitempty"` // target entity
	Q [][2]uint32   `json:"q,omitempty"` // blocker, state mask
	E []fileEffect  `json:"e,omitempty"`
}

type fileEffect struct {
	K EffectKind `json:"k"`
	N int32      `json:"n"`
	Y float32    `json:"y,omitempty"`
	T float32    `json:"t,omitempty"`
	P int8       `json:"p,omitempty"` // pose + 1
	B int32      `json:"b,omitempty"` // blocker + 1
}

func vecPtr(v Vec3) *Vec3 {
	if v == (Vec3{}) {
		return nil
	}
	return &v
}

func (g *Graph) toFile() *fileGraph {
	f := &fileGraph{
		Format: g.Format, Map: g.Map, Checksum: g.Checksum, Physics: g.PhysicsHash, Params: g.Params, Scene: g.Scene,
		Blockers: make([]fileBlocker, len(g.Blockers)), Ents: make([]fileEnt, len(g.Ents)),
		Spawns: make([]fileSpawn, len(g.Spawns)), Nodes: make([]fileNode, len(g.Nodes)), Edges: make([]fileEdge, len(g.Edges)),
	}
	for i, b := range g.Blockers {
		f.Blockers[i] = fileBlocker{Entity: b.Entity, Class: b.Class, Model: b.Model, Kind: b.Kind, Poses: b.Poses,
			Gone: b.Gone, Spawn: b.Spawn, Skills: b.Skills, Start: vecPtr(b.Start), End: vecPtr(b.End),
			Head: b.Headnode, Mins: b.Mins, Maxs: b.Maxs, Solid: b.Solid, Team: b.Team}
	}
	f.Solids = make([]fileSolid, len(g.Solids))
	for i, s := range g.Solids {
		f.Solids[i] = fileSolid{ID: s.ID, Headnode: s.Headnode, Box: s.Box, Origin: s.Origin, Angles: s.Angles, Mins: s.Mins, Maxs: s.Maxs, Pushable: s.Pushable}
	}
	f.Volumes = make([]fileVolume, len(g.Volumes))
	for i, v := range g.Volumes {
		f.Volumes[i] = fileVolume(v)
	}
	for _, p := range g.Pushes {
		f.Pushes = append(f.Pushes, filePush(p))
	}
	for _, t := range g.Teleports {
		f.Teles = append(f.Teles, fileTele(t))
	}
	for i, e := range g.Ents {
		f.Ents[i] = fileEnt(e)
	}
	for i, s := range g.Spawns {
		f.Spawns[i] = fileSpawn(s)
	}
	for i := range g.Nodes {
		n := &g.Nodes[i]
		f.Nodes[i] = fileNode{O: n.Origin, F: n.Flags, M: n.Blocker + 1, P: n.Pose, R: n.Region, Y: n.Yaw}
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		fe := fileEdge{F: e.From, T: e.To, K: e.Kind, R: e.Recipe, X: e.Flags, C: e.Cost, A: vecPtr(e.Aim), O: vecPtr(e.Takeoff),
			S: e.TakeoffSpeed, W: e.Forward, B: e.BackupMsec, Y: e.Yaw, D: e.FallDamage, H: e.Damage, G: e.Target}
		for _, r := range e.Reqs {
			fe.Q = append(fe.Q, [2]uint32{uint32(r.Blocker), uint32(r.States)})
		}
		for _, x := range e.Effects {
			fe.E = append(fe.E, fileEffect{K: x.Kind, N: x.Entity, Y: x.Yaw, T: x.T, P: x.Pose + 1, B: x.Blocker + 1})
		}
		f.Edges[i] = fe
	}
	return f
}

func (f *fileGraph) toGraph() (*Graph, error) {
	if f.Format != FormatVersion {
		return nil, fmt.Errorf("nav: file format %d, want %d", f.Format, FormatVersion)
	}
	g := &Graph{Format: f.Format, Map: f.Map, Checksum: f.Checksum, PhysicsHash: f.Physics, Params: f.Params, Scene: f.Scene, Skill: -1,
		Blockers: make([]Blocker, len(f.Blockers)), Ents: make([]Ent, len(f.Ents)), Spawns: make([]Spawn, len(f.Spawns)),
		Nodes: make([]Node, len(f.Nodes)), Edges: make([]Edge, len(f.Edges))}
	for i, b := range f.Blockers {
		if b.Kind < BlockDoor || b.Kind > BlockWater {
			return nil, fmt.Errorf("nav: blocker %d: bad kind %d", i, b.Kind)
		}
		if int(b.Spawn) < -1 || int(b.Spawn) >= len(b.Poses) {
			return nil, fmt.Errorf("nav: blocker %d: bad spawn state %d", i, b.Spawn)
		}
		nb := Blocker{Entity: b.Entity, Class: b.Class, Model: b.Model, Kind: b.Kind, Poses: b.Poses, Gone: b.Gone, Spawn: b.Spawn, Skills: b.Skills,
			Headnode: b.Head, Mins: b.Mins, Maxs: b.Maxs, Solid: b.Solid, Team: b.Team}
		if b.Start != nil {
			nb.Start = *b.Start
		}
		if b.End != nil {
			nb.End = *b.End
		}
		g.Blockers[i] = nb
	}
	for i, e := range f.Ents {
		g.Ents[i] = Ent(e)
	}
	for i, s := range f.Spawns {
		g.Spawns[i] = Spawn(s)
	}
	for i, n := range f.Nodes {
		g.Nodes[i] = Node{Origin: n.O, Flags: n.F, Blocker: n.M - 1, Pose: n.P, Region: n.R, Yaw: n.Y}
	}
	for _, s := range f.Solids {
		if s.ID <= 0 {
			return nil, fmt.Errorf("nav: solid id %d", s.ID)
		}
		g.Solids = append(g.Solids, navsim.Solid{ID: s.ID, Headnode: s.Headnode, Box: s.Box, Origin: s.Origin, Angles: s.Angles, Mins: s.Mins, Maxs: s.Maxs, Pushable: s.Pushable})
	}
	for i, v := range f.Volumes {
		if v.Kind < EffTrigger || v.Kind > EffItem || v.Blocker < -1 || int(v.Blocker) >= len(g.Blockers) {
			return nil, fmt.Errorf("nav: volume %d: bad kind or blocker", i)
		}
		g.Volumes = append(g.Volumes, Volume(v))
	}
	for _, p := range f.Pushes {
		g.Pushes = append(g.Pushes, navsim.Push(p))
	}
	for _, t := range f.Teles {
		g.Teleports = append(g.Teleports, navsim.Teleport(t))
	}
	for i := range f.Edges {
		fe := &f.Edges[i]
		if fe.K < EdgeWalk || fe.K > EdgeTouch || fe.R == navsim.RecipeNone || fe.R > navsim.RecipeRide {
			return nil, fmt.Errorf("nav: edge %d: bad kind %d / recipe %d", i, fe.K, fe.R)
		}
		e := Edge{From: fe.F, To: fe.T, Kind: fe.K, Recipe: fe.R, Flags: fe.X, Cost: fe.C, TakeoffSpeed: fe.S,
			Forward: fe.W, BackupMsec: fe.B, Yaw: fe.Y, FallDamage: fe.D, Damage: fe.H, Target: fe.G}
		if fe.A != nil {
			e.Aim = *fe.A
		}
		if fe.O != nil {
			e.Takeoff = *fe.O
		}
		for _, q := range fe.Q {
			if q[0] >= uint32(len(g.Blockers)) {
				return nil, fmt.Errorf("nav: edge %d: bad blocker %d", i, q[0])
			}
			e.Reqs = append(e.Reqs, Req{Blocker: int32(q[0]), States: StateMask(q[1])})
		}
		for _, x := range fe.E {
			if x.K < EffTrigger || x.K > EffItem || x.B < 0 || int(x.B) > len(g.Blockers) {
				return nil, fmt.Errorf("nav: edge %d: bad effect kind %d", i, x.K)
			}
			e.Effects = append(e.Effects, Effect{Kind: x.K, Entity: x.N, Yaw: x.Y, T: x.T, Pose: x.P - 1, Blocker: x.B - 1})
		}
		g.Edges[i] = e
	}
	if err := g.Finish(); err != nil {
		return nil, err
	}
	return g, nil
}

// Encode writes the graph as gzipped JSON. The same graph always produces
// the same bytes.
func (g *Graph) Encode(w io.Writer) error {
	zw, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(zw, 1<<16)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(g.toFile()); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

// Decode reads a graph written by Encode and checks it.
func Decode(r io.Reader) (*Graph, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("nav: %w", err)
	}
	defer zr.Close()
	var f fileGraph
	dec := json.NewDecoder(bufio.NewReaderSize(io.LimitReader(zr, maxFileBytes), 1<<16))
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("nav: decode: %w", err)
	}
	return f.toGraph()
}

// ReadFile decodes a graph file.
func ReadFile(path string) (*Graph, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return Decode(fh)
}

// WriteFile encodes the graph to path atomically (a temporary file in the
// same directory, then a rename).
func (g *Graph) WriteFile(path string) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o644); err != nil { // a shared cache (CreateTemp makes it 0600)
		_ = tmp.Close()
		return err
	}
	bw := bufio.NewWriter(tmp)
	if err := g.Encode(bw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := bw.Flush(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ErrMismatch is returned (wrapped) when a cached file is for another map
// version or other build settings.
var ErrMismatch = errors.New("nav: cached graph does not match")
