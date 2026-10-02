// Package fairness holds the fairness differential test of the bot: a
// lockstep episode is recorded as the client saw it (a .dm2 file, the
// server messages verbatim: demo.Recorder) together with its trace (every
// decision tick: the lane-state digest, the intent with the provenance of
// each field, and the usercmds sent), and Rebuild plays the recording back
// into a fresh bot on a passive client (fakeclient.NewPassive and
// FeedPayload) with the recorded commands' netchan numbers. The rebuilt
// bot must make the same decisions, byte for byte: the same tick events
// and the same usercmds. A bot that peeked at anything the client does
// not receive (the server, the game, the session's truth) could not be
// rebuilt from the recording.
//
// The package does not link the server, the game, the collision world,
// the host or the session (internal/sv, internal/game, internal/world,
// internal/host, internal/agent/session): its tests check that with go
// list -deps, so its test binary cannot reach them either. The recording
// is made by another test binary (campaign.TestRecordFairness, run as a
// subprocess with Q2_FAIRNESS_OUT), or taken from Q2_FAIRNESS_DIR.
//
// Normalizations (what the rebuild does differently from the live run,
// and why that does not change what the bot sees):
//
//   - The recording starts at the level's first uncompressed frame (like
//     CL_Record_f, the recorder waits for one). The bot enters the level
//     after its client became active, which is that frame, so it sees
//     every frame it saw live: Rebuild enters the bot on the frame its
//     first recorded command was built on.
//   - A .dm2 header marks the stream an attract loop (CL_Record_f writes
//     attractloop 1), which makes a client freeze the player's pmove
//     (PM_FREEZE: demo playback). The live client had no attract loop, so
//     Rebuild clears ServerData.AttractLoop once the header is parsed.
//   - Commands and frames are aligned by server frame: every recorded
//     command carries the frame it was built on (trace.UserCmd.Frame), so
//     the rebuilt bot builds it after that frame and before the next
//     message. Each message after the header is one lockstep step's.
//   - The netchan sequence numbers are the bot's own client state, which a
//     passive client does not keep: Rebuild sets the outgoing sequence and
//     the incoming acknowledgement of each command from the recording
//     (trace.UserCmd.Seq and Ack) before building it.
//   - The session clock of a frame (the gms a tick is stamped with and the
//     bot's now) is the recorded tick's; lockstep advances it one frame
//     time per server frame, which Rebuild checks.
//   - The envelope stamps of an event that are the bus's and the
//     campaign's (run, episode, sequence number, level index, map) are
//     taken from the recorded event; the wall clock is ignored
//     (trace.Comparable). The body, gms and sf are compared.
//   - The side commands the bot queues on its client (inven, use) are
//     dropped: a passive client sends nothing. What they caused reaches
//     the bot only through later server messages, which the recording
//     holds.
//
// Rebuild covers one level attempt: a recording with a death (the reload
// is a new level generation and file) or an explore burst the campaign's
// watchdog ordered (an input from outside the bot) is refused.
package fairness
