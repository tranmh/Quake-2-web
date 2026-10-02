package spectate_test

import (
	"errors"
	"net/http"

	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/spectate"
)

// Wiring a bot for watching: the Stream goes on the bot client's
// OnServerMessage hook (session.LockstepConfig.Client or
// InProcConfig.Client), the existing .dm2 recorder is one of its sinks, a
// Hub relays it, and the handler is mounted next to the game endpoints.
func Example() {
	rec := demo.NewRecorder(demo.DirCreator("runs/run-1/ep-000/demos"))
	stream := spectate.NewStream(rec)
	hub := spectate.NewHub(stream, spectate.HubConfig{MaxViewers: 4, Global: spectate.NewViewerLimit(64)})
	defer hub.Close()

	opt := fakeclient.Options{OnServerMessage: stream.OnServerMessage}
	_ = opt // the bot's session client options

	mux := http.NewServeMux()
	mux.Handle("GET /ws/v1/bots/{id}/watch", spectate.NewHandler(spectate.HandlerConfig{
		Hub: func(id string) (*spectate.Hub, bool) { return hub, id == "run-1" },
		Redeem: func(ticket, id string) error {
			return errors.New("redeem the API's one-time watch ticket here")
		},
	}))
}
