package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
)

// navDir returns -nav / -out or the default cache directory.
func navDir(flag string) string {
	if flag != "" {
		return flag
	}
	return nav.DefaultDir()
}

func runBuild(args []string, stdout io.Writer) error {
	fs := newFlags("build")
	pakFile := fs.String("pak", "", "pak file holding the maps")
	name := fs.String("map", "", "level to build (e.g. demo1)")
	all := fs.Bool("all", false, "build every map of the pak")
	out := fs.String("out", "", "cache directory (default <repo>/assets/nav)")
	workers := fs.Int("workers", 0, "parallel workers (default GOMAXPROCS)")
	force := fs.Bool("force", false, "rebuild even when the cached graph is up to date")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if (*name == "") == !*all {
		return fmt.Errorf("%w: give -map or -all", errUsage)
	}
	src, err := openPak(*pakFile, mapdata.Options{Skill: 1})
	if err != nil {
		return err
	}
	defer src.Close()
	names := []string{*name}
	if *all {
		names = src.maps()
	}
	dir := navDir(*out)
	p := nav.DefaultParams()
	for _, m := range names {
		raw, err := src.p.ReadFile("maps/" + m + ".bsp")
		if err != nil {
			return fmt.Errorf("maps/%s.bsp: %w", m, err)
		}
		md, err := mapdata.Load(m, raw, src.opt)
		if err != nil {
			return err
		}
		path := filepath.Join(dir, nav.CacheName(m, md.Checksum, p))
		if !*force {
			if g, err := nav.ReadFile(path); err == nil && g.Matches(md, p) == nil {
				fmt.Fprintf(stdout, "%s: up to date (%d nodes, %d edges) %s\n", m, len(g.Nodes), len(g.Edges), path)
				continue
			}
		}
		g, rep, err := navbuild.Build(context.Background(), m, raw, navbuild.Config{Params: p, Workers: *workers})
		if err != nil {
			return err
		}
		t0 := time.Now()
		if err := g.WriteFile(path); err != nil {
			return err
		}
		st, _ := os.Stat(path)
		size := int64(0)
		if st != nil {
			size = st.Size()
		}
		s := g.Stats()
		fmt.Fprint(stdout, rep)
		fmt.Fprintf(stdout, "  kinds %v, %d conditional, %d fast\n", s.ByKind, s.Conditional, s.Fast)
		fmt.Fprintf(stdout, "  wrote %s (%d bytes, %v)\n", path, size, time.Since(t0).Round(time.Millisecond))
	}
	return nil
}

// loadGraph returns the graph of map name for the source's skill from the
// cache under dir, building (and caching) it when needed.
func loadGraph(src *mapSource, name, dir string) (*nav.Graph, *mapdata.Map, error) {
	md, err := src.load(name)
	if err != nil {
		return nil, nil, err
	}
	store := nav.NewStore(dir, navbuild.StoreBuilder(src.p.ReadFile, navbuild.Config{}))
	g, err := store.Load(context.Background(), md, nav.DefaultParams())
	if err != nil {
		return nil, nil, err
	}
	return g, md, nil
}
