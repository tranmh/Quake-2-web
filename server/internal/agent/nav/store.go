package nav

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"quake2web/server/internal/agent/mapdata"
)

// BuildFunc builds the skill-independent graph of a map (navbuild provides
// one), with the SceneDigest of each skill's map data in Graph.Scene. It
// must honor ctx.
type BuildFunc func(ctx context.Context, md *mapdata.Map, p Params) (*Graph, error)

// Store loads navigation graphs from a cache directory and builds the
// missing ones: at most one build per cache file runs at a time, however
// many callers ask (single flight), and a build stops when every caller
// waiting for it has given up. Built graphs are written atomically (a
// temporary file, then a rename) and kept in memory. A Store is safe for
// concurrent use.
type Store struct {
	dir   string
	build BuildFunc
	// Logf, when set, reports cache misses, corrupt files and write errors.
	Logf func(format string, args ...any)

	mu    sync.Mutex
	mem   map[string]*Graph
	calls map[string]*flight
}

type flight struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	g       *Graph
	err     error
	waiters int
}

// NewStore returns a store caching under dir ("" keeps graphs in memory
// only) that builds with build (nil: a missing graph is an error).
func NewStore(dir string, build BuildFunc) *Store {
	return &Store{dir: dir, build: build, mem: map[string]*Graph{}, calls: map[string]*flight{}}
}

// Dir returns the cache directory.
func (s *Store) Dir() string { return s.dir }

// Path returns the cache file of a map for params p.
func (s *Store) Path(md *mapdata.Map, p Params) string {
	return filepath.Join(s.dir, CacheName(md.Name, md.Checksum, p))
}

// ErrNoBuilder is returned when a graph is not cached and the store cannot
// build it.
var ErrNoBuilder = errors.New("nav: graph not cached and no builder")

// Load returns the graph of md for its skill (Graph.ForSkill(md.Skill)):
// from memory, from the cache file, or freshly built (and cached). A cache
// file that does not decode or does not match the map and params is
// rebuilt.
func (s *Store) Load(ctx context.Context, md *mapdata.Map, p Params) (*Graph, error) {
	if md == nil {
		return nil, errors.New("nav: nil map")
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	name := CacheName(md.Name, md.Checksum, p)
	s.mu.Lock()
	if g := s.mem[name]; g != nil {
		s.mu.Unlock()
		return s.resolved(name, g, md.Skill), nil
	}
	f := s.calls[name]
	if f == nil || f.ctx.Err() != nil {
		// no build running, or one abandoned by all its callers
		bctx, cancel := context.WithCancel(context.Background())
		f = &flight{ctx: bctx, cancel: cancel, done: make(chan struct{})}
		s.calls[name] = f
		go s.run(f, name, md, p)
	}
	f.waiters++
	s.mu.Unlock()

	select {
	case <-f.done:
		if f.err != nil {
			return nil, f.err
		}
		return s.resolved(name, f.g, md.Skill), nil
	case <-ctx.Done():
		s.mu.Lock()
		f.waiters--
		if f.waiters == 0 {
			f.cancel()
		}
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// resolved returns g resolved for skill, from memory when it was resolved
// before.
func (s *Store) resolved(name string, g *Graph, skill int) *Graph {
	key := fmt.Sprintf("%s#%d", name, skill)
	s.mu.Lock()
	r := s.mem[key]
	s.mu.Unlock()
	if r != nil {
		return r
	}
	r = g.ForSkill(skill)
	s.mu.Lock()
	if old := s.mem[key]; old != nil {
		r = old
	} else {
		s.mem[key] = r
	}
	s.mu.Unlock()
	return r
}

func (s *Store) run(f *flight, name string, md *mapdata.Map, p Params) {
	g, err := s.loadOrBuild(f.ctx, name, md, p)
	s.mu.Lock()
	f.g, f.err = g, err
	if s.calls[name] == f {
		delete(s.calls, name)
	}
	if err == nil {
		s.mem[name] = g
	}
	s.mu.Unlock()
	f.cancel()
	close(f.done)
}

func (s *Store) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Store) loadOrBuild(ctx context.Context, name string, md *mapdata.Map, p Params) (*Graph, error) {
	path := ""
	if s.dir != "" {
		path = filepath.Join(s.dir, name)
		g, err := ReadFile(path)
		switch {
		case err == nil:
			if err = g.Matches(md, p); err == nil {
				return g, nil
			}
			s.logf("nav: %s: %v; rebuilding", path, err)
		case errors.Is(err, fs.ErrNotExist):
			s.logf("nav: %s not cached; building", path)
		default:
			s.logf("nav: %s unreadable (%v); rebuilding", path, err)
		}
	}
	if s.build == nil {
		return nil, fmt.Errorf("%w: %s", ErrNoBuilder, name)
	}
	g, err := s.build(ctx, md, p)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	g.Format, g.Map, g.Checksum, g.PhysicsHash, g.Params, g.Skill = FormatVersion, md.Name, md.Checksum, p.PhysicsHash(), p, -1
	if path != "" {
		if err := g.WriteFile(path); err != nil {
			s.logf("nav: writing %s: %v", path, err)
		}
	}
	return g, nil
}

// Matches reports (nil) whether g was built from md's BSP with params p:
// the same map name and checksum, physics hash and params, and (for map
// data loaded with single-player options, the only ones the builder uses)
// the same scene digest at md's skill.
func (g *Graph) Matches(md *mapdata.Map, p Params) error {
	switch {
	case g.Map != md.Name:
		return fmt.Errorf("%w: map %q, want %q", ErrMismatch, g.Map, md.Name)
	case g.Checksum != md.Checksum:
		return fmt.Errorf("%w: checksum %08x, want %08x", ErrMismatch, g.Checksum, md.Checksum)
	case g.PhysicsHash != p.PhysicsHash() || g.Params != p:
		return fmt.Errorf("%w: physics %s, want %s", ErrMismatch, g.PhysicsHash, p.PhysicsHash())
	}
	if singlePlayer(md) {
		if d := SceneDigest(md); g.Scene[md.Skill&3] != d {
			return fmt.Errorf("%w: scene digest %q at skill %d, the map data gives %q", ErrMismatch, g.Scene[md.Skill&3], md.Skill, d)
		}
	}
	return nil
}

// DefaultDir returns the nav cache directory: $Q2_NAV_DIR when set, else
// <repo>/assets/nav for a working directory inside the repository (found by
// walking up to the directory holding server/go.mod), else "assets/nav".
func DefaultDir() string {
	if d := os.Getenv("Q2_NAV_DIR"); d != "" {
		return d
	}
	dir, err := os.Getwd()
	if err != nil {
		return filepath.Join("assets", "nav")
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil && !st.IsDir() {
			return filepath.Join(dir, "assets", "nav")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return filepath.Join("assets", "nav")
		}
		dir = parent
	}
}
