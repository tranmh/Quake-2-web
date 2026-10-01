// Package demo reads and writes client .dm2 demos. cl_main.go ports the
// recording side of client/cl_main.c (CL_Record_f, CL_WriteDemoMessage,
// CL_Stop_f) byte for byte, so the files play in the C client, the TS
// client (q2-client CL_PlayDemo) and through the server's "demomap".
//
// A .dm2 file is a sequence of blocks, each a little-endian int32 length
// followed by one server message without its netchan header; a length of -1
// ends the file. The first blocks hold the startup information CL_Record_f
// writes (svc_serverdata with attractloop 1, every configstring, every
// baseline with a model and a "precache" stufftext); the rest are the
// client's received messages verbatim.
//
// CL_Record_f takes its state from the client globals (cl, cl_entities) and
// writes to cls.demofile; here HeaderFromClient captures that state and
// Writer writes to any io.Writer. The one deliberate deviation is opt-in:
// Writer.AllBaselines (see there).
package demo

import (
	"encoding/binary"
	"fmt"
	"io"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// Header is the startup state of a demo: the svc_serverdata fields as they
// are written to the file, the configstrings and the baselines.
type Header struct {
	ServerCount int32 // CL_Record_f writes 0x10000 + cl.servercount
	AttractLoop int32 // CL_Record_f writes 1: demos are always attract loops
	GameDir     string
	PlayerNum   int32
	LevelName   string // cl.configstrings[CS_NAME]

	ConfigStrings [q2const.MAX_CONFIGSTRINGS]string
	Baselines     [q2const.MAX_EDICTS]shared.EntityState
	// HasBaseline marks the baselines the client received. Begin writes
	// every baseline with a modelindex, like C, and the others only with
	// Writer.AllBaselines.
	HasBaseline [q2const.MAX_EDICTS]bool
}

// HeaderFromClient captures the client state CL_Record_f writes.
// C: client/cl_main.c:160 CL_Record_f (the state it reads)
func HeaderFromClient(c *fakeclient.Client) *Header {
	h := &Header{
		ServerCount:   0x10000 + c.ServerData.ServerCount,
		AttractLoop:   1, // demos are always attract loops
		GameDir:       c.ServerData.GameDir,
		PlayerNum:     c.ServerData.PlayerNum,
		LevelName:     c.ConfigStrings[q2const.CS_NAME],
		ConfigStrings: c.ConfigStrings,
	}
	for i := range c.Entities {
		h.Baselines[i] = c.Entities[i].Baseline
		h.HasBaseline[i] = c.Entities[i].HasBaseline
	}
	return h
}

// Writer writes a .dm2 stream: Begin once, then Message for every received
// server message, then End. It does not close the underlying writer. The
// first write error is sticky.
type Writer struct {
	// AllBaselines also writes the received baselines that have no model.
	// CL_Record_f skips them (if (!ent->modelindex) continue), which drops
	// the baselines of sound-only entities such as looping target_speakers:
	// on playback those entities delta from a null baseline and replay at
	// the world origin. false (the default) is the C behavior; recorders
	// that need an exact replay of every entity set it (TODO-IMPROVE in
	// docs/PARITY.md).
	AllBaselines bool

	w      io.Writer
	err    error
	blocks int
	bytes  int64
	begun  bool
	ended  bool
}

// NewWriter returns a Writer on w.
func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

// Blocks returns the number of blocks written (without the terminator).
func (w *Writer) Blocks() int { return w.blocks }

// Bytes returns the number of bytes written.
func (w *Writer) Bytes() int64 { return w.bytes }

// writeBlock writes one length-prefixed block (fwrite of len, then data).
func (w *Writer) writeBlock(data []byte) error {
	if w.err != nil {
		return w.err
	}
	var hdr [4]byte
	binary.LittleEndian.PutUint32(hdr[:], uint32(int32(len(data))))
	if _, err := w.w.Write(hdr[:]); err != nil {
		w.err = err
		return err
	}
	if _, err := w.w.Write(data); err != nil {
		w.err = err
		return err
	}
	w.blocks++
	w.bytes += int64(4 + len(data))
	return nil
}

// Begin writes the startup information: messages to hold the serverdata,
// the configstrings and the baselines, split into blocks where C splits
// them. The rest of the demo file will be individual frames (Message).
// C: client/cl_main.c:160 CL_Record_f
func (w *Writer) Begin(h *Header) (err error) {
	if w.begun {
		return fmt.Errorf("demo: Begin called twice")
	}
	w.begun = true
	defer func() {
		// a configstring longer than a message overflows the buffer
		// (SZ_GetSpace: ERR_FATAL in C)
		if r := recover(); r != nil {
			ce, ok := r.(shared.ComError)
			if !ok {
				panic(r)
			}
			err = fmt.Errorf("demo: %s", ce.Msg)
			w.err = err
		}
	}()

	//
	// write out messages to hold the startup information
	//
	buf := msg.NewSizeBuf(q2const.MAX_MSGLEN)

	// send the serverdata
	buf.MSG_WriteByte(q2const.Svc_serverdata)
	buf.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	buf.MSG_WriteLong(h.ServerCount)
	buf.MSG_WriteByte(h.AttractLoop)
	buf.MSG_WriteString(h.GameDir)
	buf.MSG_WriteShort(h.PlayerNum)

	buf.MSG_WriteString(h.LevelName)

	// configstrings
	for i := 0; i < q2const.MAX_CONFIGSTRINGS; i++ {
		cs := h.ConfigStrings[i]
		if cs != "" {
			if buf.CurSize+len(cs)+32 > buf.MaxSize { // write it out
				if err := w.writeBlock(buf.Bytes()); err != nil {
					return err
				}
				buf.CurSize = 0
			}

			buf.MSG_WriteByte(q2const.Svc_configstring)
			buf.MSG_WriteShort(int32(i))
			buf.MSG_WriteString(cs)
		}
	}

	// baselines
	var nullstate shared.EntityState
	for i := 0; i < q2const.MAX_EDICTS; i++ {
		ent := &h.Baselines[i]
		if ent.ModelIndex == 0 && !(w.AllBaselines && h.HasBaseline[i]) {
			continue
		}

		if buf.CurSize+64 > buf.MaxSize { // write it out
			if err := w.writeBlock(buf.Bytes()); err != nil {
				return err
			}
			buf.CurSize = 0
		}

		buf.MSG_WriteByte(q2const.Svc_spawnbaseline)
		buf.MSG_WriteDeltaEntity(&nullstate, ent, true, true)
	}

	buf.MSG_WriteByte(q2const.Svc_stufftext)
	buf.MSG_WriteString("precache\n")

	// write it to the demo file
	return w.writeBlock(buf.Bytes())
}

// Message dumps one received server message (the packet without its 8 byte
// netchan header, as fakeclient's OnServerMessage reports it), prefixed by
// its length.
// C: client/cl_main.c:113 CL_WriteDemoMessage
func (w *Writer) Message(payload []byte) error {
	if !w.begun || w.ended {
		return fmt.Errorf("demo: Message outside Begin/End")
	}
	return w.writeBlock(payload)
}

// End finishes the demo with the -1 length.
// C: client/cl_main.c:132 CL_Stop_f
func (w *Writer) End() error {
	if w.ended {
		return nil
	}
	w.ended = true
	if w.err != nil {
		return w.err
	}
	// finish up
	var end [4]byte
	binary.LittleEndian.PutUint32(end[:], 0xffffffff) // len = -1
	if _, err := w.w.Write(end[:]); err != nil {
		w.err = err
		return err
	}
	w.bytes += 4
	return nil
}
