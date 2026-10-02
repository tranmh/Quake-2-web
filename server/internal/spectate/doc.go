// Package spectate streams a headless bot's view to watchers: the live
// over-the-shoulder relay of docs/plans/0002-ai-agent.md (workstream H,
// item 3) and the fan-out that feeds the per-level .dm2 recorder.
//
// # Data flow
//
//	fakeclient.Options.OnServerMessage ─► Stream ─► Sinks (synchronous, e.g. *demo.Recorder)
//	                                         │
//	                                         └─► Mirror ─► bounded feed ─► Hub goroutine ─► viewers
//
// A Stream is fed synchronously in the bot's goroutine with every server
// message the bot accepted (payload and svc spans). It never blocks the bot:
// the synchronous sinks run inline, and each Hub gets the messages through a
// bounded channel. When that channel is full the message is dropped for the
// hub, and every live viewer is resynchronised from the mirrored state once
// the feed flows again.
//
// The Mirror keeps, per level generation (each svc_serverdata the bot
// parsed, counted across bot clients when a Stream outlives one), a
// LevelSnapshot (the svc_serverdata fields, the configstrings and the
// baselines the bot received) and the bot's latest valid frame (FrameSnapshot:
// serverframe, areabits, playerstate and the frame's entity states). It reads
// the bot client's own parsed state right after each message: a new
// generation is copied in full, later configstring and baseline changes are
// located by the message's spans, so nothing is parsed twice. Snapshots are
// immutable and copy-on-write, so the hub goroutine reads them without locks.
//
// # The relay
//
// A Hub is a minimal protocol-34 server per viewer over a datagram
// connection (net.Conn: the WebSocket transport of ADR-0001 or an in-memory
// pipe). It answers getchallenge and connect itself, runs a server-side
// netchan per viewer and mirrors the server's connection handshake
// (SV_New_f, SV_Configstrings_f, SV_Baselines_f, SV_Begin_f, with their
// paging) from the LevelSnapshot, with attractloop 1 so clients treat the
// stream as watch-only. A "new" that arrives before the bot's level is
// complete is answered once it is.
//
// On "begin" the viewer gets a keyframe at once: the mirrored latest frame of
// the bot written as an uncompressed svc_frame with the same serverframe
// number (deltaframe -1, the playerstate from zero, entities from their
// baselines; see AppendKeyframe). From then on the bot's own messages are
// forwarded verbatim as unreliable data, with the commands that only make
// sense on the bot's connection removed (see Forwarded): the bot's next delta
// frame applies on the viewer because the viewer holds the same frame.
// Viewers never perturb the bot: nothing a viewer sends reaches the bot or
// the game, and the relay asks the bot's server for nothing
// (fakeclient.RequestFullFrame is not used).
//
// A viewer is resynchronised with a new keyframe, preceded by a reliable diff
// of the configstrings it may lack, when the bot's frame deltas from a frame
// the viewer was not sent, when its send queue dropped a datagram, when the
// hub's feed overflowed, or (at most once a second) when the viewer reports
// an invalid frame (clc_move lastframe -1). A bot level change or reload (a
// new generation) sends every viewer "changing" and "reconnect", so it
// redoes the handshake on its netchan.
//
// Data the relay originates on the reliable stream (handshake pages,
// configstring diffs, "changing"/"reconnect") always travels in a datagram
// of its own, never together with a forwarded payload, so the netchan never
// has to dump an unreliable part for lack of room; a forward that exceeds
// one datagram (a keyframe in place of a small delta) is split over several.
// While the viewer's reliable stream has data in flight, forwarded
// configstrings join it instead of travelling unreliably, so a late
// retransmission can never overwrite a newer value.
//
// Slow viewers: each viewer has a bounded send queue drained by its own
// writer goroutine. A dropped datagram makes the relay wait for the queue to
// drain and then resynchronise the viewer; more than DropLimit drops within
// DropWindow, or a queue that does not drain for StallTimeout, disconnect
// it. Viewers that send nothing valid for Timeout are dropped; keepalives go
// out at least once a second. Per-hub and global viewer caps and a
// per-connection packet rate bound the cost of watching.
//
// # Fairness and isolation
//
// The package reads only the bot client's received state. Its non-test code
// imports no server, game, host, API, agent or demo code (TestImports).
// Viewer input is parsed minimally: clc_nop, clc_userinfo (ignored),
// clc_move (checksum and lastframe only) and clc_stringcmd limited to the
// handshake commands new, configstrings, baselines, begin and disconnect;
// any other string command is ignored and any other command drops the
// viewer.
package spectate
