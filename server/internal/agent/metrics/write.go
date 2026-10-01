package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSON writes v (a RunSummary for run.json, an EpisodeSummary for
// episode.json) as indented JSON to path, atomically: a reader never sees a
// partial file.
func WriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // after a successful rename this is a no-op
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
