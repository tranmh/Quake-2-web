package trace

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// FileSink writes events as gzip-compressed JSON Lines. It flushes the
// compressor every flushEvery (so a crash loses at most that much) and on
// Close. It is safe for concurrent use and implements Sink.
type FileSink struct {
	mu     sync.Mutex
	w      io.WriteCloser
	gz     *gzip.Writer
	err    error
	events int
	closed bool

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// CreateFile creates path (truncating it) and returns a sink writing to it.
func CreateFile(path string, flushEvery time.Duration) (*FileSink, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return NewFileSink(f, flushEvery), nil
}

// NewFileSink returns a sink writing to w, which Close closes. flushEvery
// <= 0 disables the periodic flush.
func NewFileSink(w io.WriteCloser, flushEvery time.Duration) *FileSink {
	f := &FileSink{w: w, gz: gzip.NewWriter(w)}
	if flushEvery > 0 {
		f.stop, f.done = make(chan struct{}), make(chan struct{})
		go f.flusher(flushEvery)
	}
	return f
}

func (f *FileSink) flusher(every time.Duration) {
	defer close(f.done)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			_ = f.Flush()
		}
	}
}

// Write appends one event line.
func (f *FileSink) Write(e Event) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("trace: write to a closed sink")
	}
	if f.err != nil {
		return f.err
	}
	line = append(line, '\n')
	if _, err := f.gz.Write(line); err != nil {
		f.err = err
		return err
	}
	f.events++
	return nil
}

// Events returns the number of events written.
func (f *FileSink) Events() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.events
}

// Flush writes buffered events through to the file.
func (f *FileSink) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || f.err != nil {
		return f.err
	}
	if err := f.gz.Flush(); err != nil {
		f.err = err
	}
	return f.err
}

// Close finishes the gzip stream and closes the file.
func (f *FileSink) Close() error {
	if f.stop != nil {
		f.stopOnce.Do(func() { close(f.stop) })
		<-f.done
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return f.err
	}
	f.closed = true
	err := f.gz.Close()
	if cerr := f.w.Close(); err == nil {
		err = cerr
	}
	if f.err == nil {
		f.err = err
	}
	return f.err
}

// Reader reads events from a JSON Lines trace, gzip-compressed or plain.
type Reader struct {
	sc   *bufio.Scanner
	line int
	// unterminated is set when the scanner returned a last line without
	// its newline (the input ended inside it).
	unterminated bool
}

// NewReader returns a reader on r (gzip is detected by its magic bytes).
func NewReader(r io.Reader) (*Reader, error) {
	br := bufio.NewReader(r)
	var src io.Reader = br
	if magic, err := br.Peek(2); err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		src = gz
	}
	rd := &Reader{sc: bufio.NewScanner(src)}
	rd.sc.Buffer(make([]byte, 64<<10), 64<<20)
	rd.sc.Split(rd.scanLines)
	return rd, nil
}

// scanLines is bufio.ScanLines noting a final line without its newline.
func (r *Reader) scanLines(data []byte, atEOF bool) (int, []byte, error) {
	adv, tok, err := bufio.ScanLines(data, atEOF)
	if atEOF && tok != nil && adv == len(data) && data[len(data)-1] != '\n' {
		r.unterminated = true
	}
	return adv, tok, err
}

// Next returns the next event, io.EOF at the end. A trace cut short (a
// crash between flushes, a truncated copy) returns its complete events and
// then an error wrapping io.ErrUnexpectedEOF: the writer ends every line
// with a newline, so a last line without one that does not parse is a cut
// line, not a corrupt one.
func (r *Reader) Next() (Event, error) {
	for r.sc.Scan() {
		r.line++
		b := bytes.TrimSpace(r.sc.Bytes())
		if len(b) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			if r.unterminated {
				return Event{}, fmt.Errorf("trace: line %d is cut short: %w", r.line, io.ErrUnexpectedEOF)
			}
			return Event{}, fmt.Errorf("trace: line %d: %w", r.line, err)
		}
		return e, nil
	}
	if err := r.sc.Err(); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Event{}, fmt.Errorf("trace: truncated after line %d: %w", r.line, err)
		}
		return Event{}, err
	}
	return Event{}, io.EOF
}

// ReadFile reads every event of a trace file. On a truncated file it
// returns the complete events and an error wrapping io.ErrUnexpectedEOF.
func ReadFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r, err := NewReader(f)
	if err != nil {
		return nil, err
	}
	var out []Event
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
}
