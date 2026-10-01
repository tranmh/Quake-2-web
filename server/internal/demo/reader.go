package demo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// Reader reads the blocks of a .dm2 stream with the checks of the players:
// the server's demo playback (SV_SendClientMessages) and the TS client's
// CL_ReadDemoPackets end a demo at the -1 length or at the end of the data,
// and refuse a block longer than MAX_MSGLEN.
type Reader struct {
	r          io.Reader
	done       bool
	terminated bool
	blocks     int
}

// NewReader returns a Reader on r.
func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

// Terminated reports whether the stream ended with the -1 length (as
// CL_Stop_f writes it) rather than just running out of data.
func (r *Reader) Terminated() bool { return r.terminated }

// Next returns the next block. It returns io.EOF at the end of the demo
// (the -1 length or the end of the data at a block boundary) and
// io.ErrUnexpectedEOF for a truncated block.
func (r *Reader) Next() ([]byte, error) {
	if r.done {
		return nil, io.EOF
	}
	var hdr [4]byte
	if _, err := io.ReadFull(r.r, hdr[:]); err != nil {
		r.done = true
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, err
	}
	n := int32(binary.LittleEndian.Uint32(hdr[:]))
	if n == -1 {
		r.done, r.terminated = true, true
		return nil, io.EOF
	}
	if n < 0 || n > q2const.MAX_MSGLEN {
		r.done = true
		return nil, fmt.Errorf("demo: block %d: length %d out of range (msglen > MAX_MSGLEN)", r.blocks, n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r.r, b); err != nil {
		r.done = true
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("demo: block %d: %w", r.blocks, err)
	}
	r.blocks++
	return b, nil
}

// Stats summarizes a validated demo.
type Stats struct {
	Bytes        int
	Blocks       int // without the terminator
	HeaderBlocks int // blocks before the first svc_frame
	Frames       int // svc_frame commands
	FirstFrame   int32
	LastFrame    int32
	Terminated   bool
	ServerCount  int32
	LevelName    string
	Map          string // configstring CS_MODELS+1
}

// Validate parses every block of a demo with a passive client, the way a
// player would, and checks that it plays to its end: it starts with a
// client demo's serverdata (protocol 34, attract loop), every frame can be
// reconstructed (no delta from a frame the demo does not hold), nothing in
// it ends or restarts playback early (svc_disconnect, svc_reconnect, a
// stuffed "changing" or "reconnect") and it ends with the -1 length.
func Validate(data []byte) (Stats, error) {
	st := Stats{Bytes: len(data)}
	r := NewReader(bytes.NewReader(data))
	p := fakeclient.NewPassive(fakeclient.Options{MaxHistory: 64})
	for {
		blk, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return st, err
		}
		spans, err := p.FeedPayload(blk)
		if err != nil {
			return st, fmt.Errorf("demo: block %d: %w", st.Blocks, err)
		}
		if st.Blocks == 0 {
			if len(spans) == 0 || spans[0].Cmd != q2const.Svc_serverdata {
				return st, errors.New("demo: the first block does not start with svc_serverdata")
			}
			if p.ServerData.AttractLoop != 1 {
				return st, fmt.Errorf("demo: attract loop %d, a client demo has 1", p.ServerData.AttractLoop)
			}
			st.ServerCount = p.ServerData.ServerCount
			st.LevelName = p.ServerData.LevelName
		} else if p.LevelGen() != 1 {
			return st, fmt.Errorf("demo: block %d: a second svc_serverdata", st.Blocks)
		}
		if why := endsLevel(blk, spans); why != "" {
			return st, fmt.Errorf("demo: block %d: %s would end or restart playback", st.Blocks, why)
		}
		for _, sp := range spans {
			if sp.Cmd != q2const.Svc_frame {
				continue
			}
			if st.Frames == 0 {
				st.HeaderBlocks = st.Blocks
				st.FirstFrame = p.Frame.ServerFrame
			}
			if !p.Frame.Valid {
				return st, fmt.Errorf("demo: block %d: frame %d deltas from frame %d, which the demo does not hold",
					st.Blocks, p.Frame.ServerFrame, p.Frame.DeltaFrame)
			}
			st.Frames++
			st.LastFrame = p.Frame.ServerFrame
		}
		st.Blocks++
	}
	st.Terminated = r.Terminated()
	st.Map = p.ConfigStrings[q2const.CS_MODELS+1]
	if st.Blocks == 0 {
		return st, errors.New("demo: empty")
	}
	if st.Frames == 0 {
		return st, errors.New("demo: no frames")
	}
	if !st.Terminated {
		return st, errors.New("demo: missing the -1 terminator (CL_Stop_f)")
	}
	return st, nil
}
