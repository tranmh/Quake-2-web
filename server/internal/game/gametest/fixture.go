package gametest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// FixtureFrame is one decoded line of a game fixture with the full edict
// list reconstructed from the delta encoding.
type FixtureFrame struct {
	Frame     int
	Level     map[string]any
	RandCalls int
	Inputs    []any
	Freed     []int
	Listed    []int                  // edict numbers listed on this line
	Edicts    map[int]map[string]any // full inuse list (reconstructed)
	Clients   []any
	Events    []any
}

// FixtureReader streams a fixtures/generated/game/<scenario>.jsonl file.
type FixtureReader struct {
	f        *os.File
	r        *bufio.Reader
	Header   map[string]any
	RawHead  json.RawMessage // the header object (scenario echo + edict_size, edicts_encoding)
	Encoding string
	prev     map[int]map[string]any
}

func decodeNumber(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(v)
}

// OpenFixture opens a fixture and reads its header line.
func OpenFixture(path string) (*FixtureReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fr := &FixtureReader{f: f, r: bufio.NewReaderSize(f, 1<<20)}
	line, err := fr.line()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("fixture header: %w", err)
	}
	var h struct {
		Header json.RawMessage `json:"header"`
	}
	if err := json.Unmarshal(line, &h); err != nil || h.Header == nil {
		f.Close()
		return nil, fmt.Errorf("fixture header: %v", err)
	}
	fr.RawHead = h.Header
	if err := decodeNumber(h.Header, &fr.Header); err != nil {
		f.Close()
		return nil, err
	}
	fr.Encoding, _ = fr.Header["edicts_encoding"].(string)
	return fr, nil
}

// Close closes the file.
func (fr *FixtureReader) Close() error { return fr.f.Close() }

func (fr *FixtureReader) line() ([]byte, error) {
	for {
		b, err := fr.r.ReadBytes('\n')
		if len(bytes.TrimSpace(b)) > 0 {
			return b, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func numInt(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return int(f), true
	}
	return int(i), true
}

// Next returns the next frame, io.EOF at the end.
func (fr *FixtureReader) Next() (*FixtureFrame, error) {
	line, err := fr.line()
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := decodeNumber(line, &raw); err != nil {
		return nil, err
	}
	ff := &FixtureFrame{}
	ff.Frame, _ = numInt(raw["frame"])
	ff.Level, _ = raw["level"].(map[string]any)
	ff.RandCalls, _ = numInt(raw["rand_calls"])
	ff.Inputs, _ = raw["inputs"].([]any)
	ff.Clients, _ = raw["clients"].([]any)
	ff.Events, _ = raw["events"].([]any)
	if fl, ok := raw["freed"].([]any); ok {
		for _, v := range fl {
			n, _ := numInt(v)
			ff.Freed = append(ff.Freed, n)
		}
	}
	full := map[int]map[string]any{}
	if fr.prev != nil && fr.Encoding != "full" {
		for n, r := range fr.prev {
			full[n] = r
		}
	}
	for _, n := range ff.Freed {
		delete(full, n)
	}
	el, _ := raw["edicts"].([]any)
	for _, v := range el {
		r, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("frame %d: bad edict record", ff.Frame)
		}
		n, ok := numInt(r["n"])
		if !ok {
			return nil, fmt.Errorf("frame %d: edict without n", ff.Frame)
		}
		full[n] = r
		ff.Listed = append(ff.Listed, n)
	}
	ff.Edicts = full
	fr.prev = full
	return ff, nil
}

// ReadAll reads every remaining frame (for small tests).
func (fr *FixtureReader) ReadAll() ([]*FixtureFrame, error) {
	var out []*FixtureFrame
	for {
		f, err := fr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
}
