package spectate

import (
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
)

// Forwarded reports whether a svc command of the bot's stream is forwarded
// to live viewers: the frame (with its playerinfo and packetentities),
// sounds, temp entities, muzzle flashes, prints, centerprints, layouts, the
// inventory and configstrings. Everything else belongs to the bot's own
// connection and is removed: stufftext (the server's commands to the bot),
// svc_disconnect and svc_reconnect (the relay manages the viewer's
// connection itself), svc_serverdata and svc_spawnbaseline (handshake data
// the relay sends from the mirror), svc_download, svc_nop and anything
// unknown.
func Forwarded(cmd int32) bool {
	switch cmd {
	case q2const.Svc_frame,
		q2const.Svc_sound,
		q2const.Svc_temp_entity,
		q2const.Svc_muzzleflash,
		q2const.Svc_muzzleflash2,
		q2const.Svc_print,
		q2const.Svc_centerprint,
		q2const.Svc_layout,
		q2const.Svc_inventory,
		q2const.Svc_configstring:
		return true
	}
	return false
}

// FilterPayload returns the forwarded commands of a bot payload (see
// Forwarded) in their original order and byte for byte: the payload a
// viewer that holds the bot's previous frame can parse exactly like the
// bot did. Spans that do not lie inside the payload are skipped.
func FilterPayload(payload []byte, spans []fakeclient.Span) []byte {
	out := make([]byte, 0, len(payload))
	for _, sp := range spans {
		if Forwarded(sp.Cmd) && spanOK(payload, sp) {
			out = append(out, payload[sp.Start:sp.End]...)
		}
	}
	return out
}

func spanOK(payload []byte, sp fakeclient.Span) bool {
	return sp.Start >= 0 && sp.Start < sp.End && sp.End <= len(payload)
}
