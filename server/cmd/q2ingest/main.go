// Command q2ingest ingests pak files into a blob directory and prints (or
// writes) the merged asset index, without a database. Useful for inspecting
// what the server would serve:
//
//	q2ingest -blobs /tmp/blobs -o index.json assets/demo/baseq2/pak0.pak [pak1.pak ...]
//
// Paks are given lowest priority first (later paks override earlier ones).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/img"
	"quake2web/server/internal/assets/ingest"
	"quake2web/server/internal/assets/manifest"
)

func main() {
	blobs := flag.String("blobs", "./data/blobs", "blob store directory (or file:// URL)")
	out := flag.String("o", "", "write the merged index JSON to this file (default: summary only)")
	name := flag.String("pakset", "local", "pakset name recorded in the index")
	skipPNG := flag.Bool("nopng", false, "skip PNG derivation")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: q2ingest [-blobs dir] [-o index.json] pak0.pak [pak1.pak ...]")
		os.Exit(2)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), log, *blobs, *out, *name, *skipPNG, flag.Args()); err != nil {
		log.Error("q2ingest failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, blobDir, out, name string, skipPNG bool, paks []string) error {
	store, err := blob.Open(blobDir)
	if err != nil {
		return err
	}
	var pal *img.Palette
	var refs []manifest.PakRef
	var mans []*manifest.PakManifest
	for i, p := range paks {
		t0 := time.Now()
		res, err := ingest.PakFile(ctx, store, p, ingest.Options{Palette: pal, SkipPNG: skipPNG, Log: log})
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if res.Palette != nil {
			pal = res.Palette
		}
		m := res.Manifest
		refs = append(refs, manifest.PakRef{ID: int64(i + 1), Name: m.Name, SHA256: m.SHA256, Size: m.Size, Checksum: m.Checksum, NumFiles: m.NumFiles})
		mans = append(mans, m)
		log.Info("ingested", "pak", p, "sha256", m.SHA256, "files", m.NumFiles, "blobs", len(res.Blobs),
			"maps", len(res.Maps), "manifest", res.ManifestSHA, "took", time.Since(t0).Round(time.Millisecond))
		for _, mi := range res.Maps {
			log.Info("map", "name", mi.Name, "checksum", mi.Checksum, "message", mi.Message, "sky", mi.Sky, "textures", len(mi.Textures))
		}
	}
	idx := manifest.Merge(name, refs, mans)
	if out == "" {
		fmt.Printf("%d files, %d maps, %d blobs\n", len(idx.Files), len(idx.Maps), len(idx.SHAs()))
		return nil
	}
	b, err := json.MarshalIndent(idx, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, b, 0o644)
}
