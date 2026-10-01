package demo

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// Recorder records a client's levels as .dm2 files, one per level
// generation (fakeclient.LevelGen), named "NN-<map>.dm2" with NN counting
// the files from 00. Plug OnServerMessage into fakeclient.Options (or call
// it from a hook there).
//
// Each file starts like a CL_Record_f recording: at the first uncompressed
// frame of the generation (the first frame after a level's handshake always
// is) the header is captured from the client and that message and every
// later one are written verbatim. Deviation from a C recording, which keeps
// going across level changes: a file ends (CL_Stop_f) before the message
// that ends or leaves the level (svc_disconnect, svc_reconnect, a stuffed
// "changing" or "reconnect"), so every file plays to its end instead of
// making the player restart or drop. Levels without frames (cinematics,
// pictures) produce no file.
//
// A Recorder is not safe for concurrent use; it runs in the client's
// goroutine.
type Recorder struct {
	create func(name string) (io.WriteCloser, error)

	// AllBaselines is passed to each file's Writer. NewRecorder sets it, so
	// sound-only entities replay exactly too; clear it for headers exactly
	// like CL_Record_f's.
	AllBaselines bool

	// RequestFull, when set, asks the client for an uncompressed frame
	// whenever the recorder waits for one (RequestFullFrame), for a
	// recorder attached in the middle of a level. It changes what the
	// server sends, so lockstep determinism runs leave it off.
	RequestFull bool

	gen     int
	waiting bool
	f       io.WriteCloser
	w       *Writer
	files   []string
	err     error
}

// NewRecorder returns a recorder creating its files with create.
func NewRecorder(create func(name string) (io.WriteCloser, error)) *Recorder {
	return &Recorder{create: create, AllBaselines: true}
}

// DirCreator returns a create function writing files into dir (created if
// needed).
func DirCreator(dir string) func(name string) (io.WriteCloser, error) {
	return func(name string) (io.WriteCloser, error) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		return os.Create(filepath.Join(dir, name))
	}
}

// Files returns the names of the files started so far.
func (r *Recorder) Files() []string { return append([]string(nil), r.files...) }

// Recording reports whether a file is open.
func (r *Recorder) Recording() bool { return r.w != nil }

// Err returns the first error; recording stops at it.
func (r *Recorder) Err() error { return r.err }

// OnServerMessage is the fakeclient.Options.OnServerMessage hook.
func (r *Recorder) OnServerMessage(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
	if r.err != nil {
		return
	}
	if gen := c.LevelGen(); gen != r.gen {
		r.finish()
		r.gen = gen
		r.waiting = true
	}
	if endsLevel(payload, spans) != "" {
		r.finish()
		r.waiting = false // this level is done; wait for the next one
		return
	}
	if r.waiting {
		if !fullFrame(c, spans) {
			if r.RequestFull && c.State == fakeclient.CaActive {
				c.RequestFullFrame()
			}
			return
		}
		r.waiting = false
		if err := r.begin(c); err != nil {
			r.fail(err)
			return
		}
	}
	if r.w != nil {
		if err := r.w.Message(payload); err != nil {
			r.fail(err)
		}
	}
}

func (r *Recorder) begin(c *fakeclient.Client) error {
	name := fmt.Sprintf("%02d-%s.dm2", len(r.files), fileLevelName(c))
	f, err := r.create(name)
	if err != nil {
		return fmt.Errorf("demo: create %s: %w", name, err)
	}
	r.files = append(r.files, name)
	r.f, r.w = f, NewWriter(f)
	r.w.AllBaselines = r.AllBaselines
	return r.w.Begin(HeaderFromClient(c))
}

// finish ends and closes the open file.
func (r *Recorder) finish() {
	if r.w == nil {
		return
	}
	err := r.w.End()
	if cerr := r.f.Close(); err == nil {
		err = cerr
	}
	r.f, r.w = nil, nil
	if err != nil && r.err == nil {
		r.err = err
	}
}

func (r *Recorder) fail(err error) {
	if r.err == nil {
		r.err = err
	}
	if r.f != nil {
		_ = r.f.Close()
	}
	r.f, r.w = nil, nil
}

// Close ends the open file and returns the first error.
func (r *Recorder) Close() error {
	r.finish()
	return r.err
}

// fullFrame reports whether the message held an uncompressed frame (the
// point where C clears cls.demowaiting).
func fullFrame(c *fakeclient.Client, spans []fakeclient.Span) bool {
	for _, sp := range spans {
		if sp.Cmd == q2const.Svc_frame && c.Frame.Valid && c.Frame.DeltaFrame <= 0 {
			return true
		}
	}
	return false
}

// endsLevel returns why a message ends playback of the level it belongs to
// ("" if it does not): a disconnect or reconnect, or stuffed text running
// "changing" or "reconnect" (a level change on the same connection).
func endsLevel(payload []byte, spans []fakeclient.Span) string {
	for _, sp := range spans {
		switch sp.Cmd {
		case q2const.Svc_disconnect:
			return "svc_disconnect"
		case q2const.Svc_reconnect:
			return "svc_reconnect"
		case q2const.Svc_stufftext:
			if sp.Start+1 > sp.End || sp.End > len(payload) {
				continue
			}
			text := string(bytes.TrimRight(payload[sp.Start+1:sp.End], "\x00"))
			for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ';' }) {
				if f := strings.Fields(line); len(f) > 0 && (f[0] == "changing" || f[0] == "reconnect") {
					return "stuffed " + f[0]
				}
			}
		}
	}
	return ""
}

// fileLevelName is the client's level (fakeclient.Client.MapName) for a
// file name, reduced to [A-Za-z0-9._-].
func fileLevelName(c *fakeclient.Client) string {
	b := []byte(c.MapName())
	for i, ch := range b {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '_' || ch == '-') {
			b[i] = '_'
		}
	}
	if len(b) == 0 {
		return "level"
	}
	return string(b)
}

// MemFiles collects recorded files in memory (a Recorder create function
// is m.Create).
type MemFiles map[string]*bytes.Buffer

// Create implements a Recorder create function.
func (m MemFiles) Create(name string) (io.WriteCloser, error) {
	if _, ok := m[name]; ok {
		return nil, fmt.Errorf("demo: %s exists", name)
	}
	b := &bytes.Buffer{}
	m[name] = b
	return nopCloser{b}, nil
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }
