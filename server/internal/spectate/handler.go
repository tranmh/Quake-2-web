package spectate

import (
	stdnet "net"
	"net/http"
	"strconv"
	"sync/atomic"

	"github.com/coder/websocket"

	qnet "quake2web/server/internal/net"
)

// HandlerConfig configures NewHandler.
type HandlerConfig struct {
	// Hub returns the hub of a bot (false: no such bot, or not watchable).
	// Required.
	Hub func(botID string) (*Hub, bool)
	// Redeem consumes a one-time watch ticket for the bot and fails for an
	// unknown, expired, used or foreign one. Required: a request without
	// a ticket or with a rejected one is refused (403).
	Redeem func(ticket, botID string) error
	// BotID extracts the bot id from the request; the default is the
	// "{id}" path wildcard of the mux pattern the handler is mounted at
	// (e.g. "GET /ws/v1/bots/{id}/watch").
	BotID func(r *http.Request) string
	// OriginPatterns are the extra Origin host patterns accepted
	// (coder/websocket AcceptOptions.OriginPatterns); the request's own
	// host is always accepted.
	OriginPatterns []string
	// RemoteIP returns the client IP (e.g. from a trusted proxy header);
	// default: the host part of RemoteAddr. It only names the viewer.
	RemoteIP func(r *http.Request) string
}

// NewHandler returns the WebSocket endpoint a viewer watches a bot through
// (one binary message per datagram, ADR-0001): it looks the bot's hub up
// (404 for an unknown bot), requires a ticket (403), takes a viewer slot
// (503 when full, 410 when the hub is already closed), redeems the ticket
// (403), upgrades the connection and serves the viewer on the hub until it
// leaves.
func NewHandler(cfg HandlerConfig) http.Handler {
	return &handler{cfg: cfg}
}

type handler struct {
	cfg  HandlerConfig
	conn atomic.Int64
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if h.cfg.BotID != nil {
		id = h.cfg.BotID(r)
	}
	if h.cfg.Hub == nil {
		http.Error(w, "no such bot", http.StatusNotFound)
		return
	}
	hub, ok := h.cfg.Hub(id)
	if !ok || hub == nil {
		http.Error(w, "no such bot", http.StatusNotFound)
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" || h.cfg.Redeem == nil {
		http.Error(w, "ticket required", http.StatusForbidden)
		return
	}
	switch err := hub.reserve(); err {
	case nil:
	case ErrClosed: // the bot's run ended after the lookup
		http.Error(w, "bot no longer watchable", http.StatusGone)
		return
	default:
		http.Error(w, "too many viewers", http.StatusServiceUnavailable)
		return
	}
	defer hub.release()
	if err := h.cfg.Redeem(ticket, id); err != nil {
		http.Error(w, "invalid ticket", http.StatusForbidden)
		return
	}
	conn, err := qnet.AcceptWS(w, r, &websocket.AcceptOptions{OriginPatterns: h.cfg.OriginPatterns})
	if err != nil {
		return
	}
	defer conn.Close()
	base := r.RemoteAddr
	if host, _, err := stdnet.SplitHostPort(r.RemoteAddr); err == nil {
		base = host
	}
	if h.cfg.RemoteIP != nil {
		if ip := h.cfg.RemoteIP(r); ip != "" {
			base = ip
		}
	}
	base += "#" + strconv.FormatInt(h.conn.Add(1), 10)
	_ = hub.serve(r.Context(), conn, base)
}
