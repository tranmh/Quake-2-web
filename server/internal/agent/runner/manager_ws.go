package runner

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/spectate"
)

// feedWriteTimeout bounds one decision feed write: a viewer that takes
// longer is disconnected.
const feedWriteTimeout = 10 * time.Second

// DefaultFeedDepth is a decision feed's subscription (events; the oldest
// are dropped when the viewer falls behind, never blocking the bot).
const DefaultFeedDepth = 512

// Stream names (the per-address connection counts of a bot).
const (
	streamWatch     = "watch"
	streamDecisions = "decisions"
)

// WatchHandler serves GET /ws/v1/bots/{id}/watch: the bot's first-person
// view as a watch-only protocol 34 stream (spectate.Hub; one binary
// message per datagram, ADR-0001), redeeming the one-time tickets of
// Watch. Without RequireTickets a viewer without a ticket may watch a
// public bot (development). One address has at most MaxViewersPerIP
// connections to a bot's view (503).
func (m *Manager) WatchHandler() http.Handler {
	h := spectate.NewHandler(spectate.HandlerConfig{
		Hub: func(id string) (*spectate.Hub, bool) {
			if b := m.live(id); b != nil {
				return b.hub, true
			}
			return nil, false
		},
		Redeem: func(ticket, id string) error {
			return m.redeem(id, ticket, watchScope(id))
		},
		OriginPatterns: m.cfg.OriginPatterns,
		RemoteIP:       m.cfg.RemoteIP,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := m.live(r.PathValue("id"))
		if b == nil {
			h.ServeHTTP(w, r) // 404
			return
		}
		ip := m.remoteIP(r)
		if !b.acquireIP(streamWatch, ip, m.cfg.MaxViewersPerIP) {
			http.Error(w, "too many viewers from your address", http.StatusServiceUnavailable)
			return
		}
		defer b.releaseIP(streamWatch, ip)
		if !m.cfg.RequireTickets && r.URL.Query().Get("ticket") == "" && m.cfg.Tickets != nil && b.snapshot().Public {
			// development: the request gets a ticket of its own (bounded
			// like Watch's), revoked when the request ends
			if tks, err := m.issueTickets(b, api.BotUser{}, watchScope(b.id)); err == nil {
				tk := tks[0].Token
				defer func() { _ = m.redeem(b.id, tk, watchScope(b.id)) }()
				q := r.URL.Query()
				q.Set("ticket", tk)
				r2 := r.Clone(r.Context())
				r2.URL.RawQuery = q.Encode()
				r = r2
			}
		}
		h.ServeHTTP(w, r)
	})
}

type ticketError string

func (e ticketError) Error() string { return string(e) }

const errNoTickets = ticketError("runner: no ticket service")

// pendingTicket is a stream ticket of a bot issued and not redeemed yet.
type pendingTicket struct {
	token, scope string
	expires      time.Time
}

// maxPendingTickets bounds a bot's outstanding stream tickets: those of
// two watches per viewer slot.
func (m *Manager) maxPendingTickets() int { return 4 * m.cfg.MaxViewers }

// issueTickets issues u a ticket of each scope for b's streams. A bot has
// at most maxPendingTickets outstanding: past it the oldest are revoked.
// A viewer redeems its tickets at once, so that only takes abandoned
// ones, and bounds what a client asking for tickets in a loop leaves in
// the server's ticket service, without locking the bot's viewers out.
func (m *Manager) issueTickets(b *managedBot, u api.BotUser, scopes ...string) ([]auth.Ticket, error) {
	if m.cfg.Tickets == nil {
		return nil, errNoTickets
	}
	b.tmu.Lock()
	defer b.tmu.Unlock()
	now := m.cfg.Now()
	kept := b.tickets[:0]
	for _, t := range b.tickets {
		if t.expires.After(now) {
			kept = append(kept, t)
		}
	}
	clear(b.tickets[len(kept):])
	b.tickets = kept
	for n := len(b.tickets) + len(scopes) - m.maxPendingTickets(); n > 0 && len(b.tickets) > 0; n-- {
		old := b.tickets[0]
		b.tickets = b.tickets[1:]
		_, _ = m.cfg.Tickets.Redeem(old.token, old.scope) // revoked: redeeming consumes it
	}
	out := make([]auth.Ticket, 0, len(scopes))
	for _, scope := range scopes {
		tk, err := m.cfg.Tickets.Issue(u.ID, u.Name, scope)
		if err != nil {
			return nil, err
		}
		b.tickets = append(b.tickets, pendingTicket{token: tk.Token, scope: scope, expires: tk.ExpiresAt})
		out = append(out, tk)
	}
	return out, nil
}

// pendingTickets is the number of b's outstanding stream tickets.
func (b *managedBot) pendingTickets() int {
	b.tmu.Lock()
	defer b.tmu.Unlock()
	return len(b.tickets)
}

// forgetTicket drops token from b's outstanding tickets.
func (b *managedBot) forgetTicket(token string) {
	b.tmu.Lock()
	defer b.tmu.Unlock()
	for i, t := range b.tickets {
		if t.token == token {
			b.tickets = append(b.tickets[:i], b.tickets[i+1:]...)
			return
		}
	}
}

// redeem redeems a stream ticket of bot id (consuming it even when it is
// rejected).
func (m *Manager) redeem(id, token, scope string) error {
	if m.cfg.Tickets == nil {
		return errNoTickets
	}
	if b := m.live(id); b != nil {
		b.forgetTicket(token)
	}
	_, err := m.cfg.Tickets.Redeem(token, scope)
	return err
}

// remoteIP is the address of a stream's viewer.
func (m *Manager) remoteIP(r *http.Request) string {
	if m.cfg.RemoteIP != nil {
		if ip := m.cfg.RemoteIP(r); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// acquireIP takes one of ip's at most limit connections to b's stream.
func (b *managedBot) acquireIP(stream, ip string, limit int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := stream + " " + ip
	if b.ips[k] >= limit {
		return false
	}
	if b.ips == nil {
		b.ips = map[string]int{}
	}
	b.ips[k]++
	return true
}

func (b *managedBot) releaseIP(stream, ip string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := stream + " " + ip
	if b.ips[k]--; b.ips[k] <= 0 {
		delete(b.ips, k)
	}
}

// DecisionsHandler serves GET /ws/v1/bots/{id}/decisions: the bot's
// decision feed (DecisionsProtocol), redeeming the one-time tickets of
// Watch, with host.Host's ticket rules (RequireTickets; without it an
// anonymous viewer may follow a public bot). It answers 404 for an
// unknown bot, 410 for an ended one, 403 for a missing or rejected
// ticket and 503 when the bot, the server or the viewer's address has too
// many feeds.
func (m *Manager) DecisionsHandler() http.Handler {
	return http.HandlerFunc(m.serveDecisions)
}

func (m *Manager) serveDecisions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b := m.live(id)
	if b == nil {
		if _, ok := m.readRun(id); ok {
			http.Error(w, "bot no longer live", http.StatusGone)
		} else {
			http.Error(w, "no such bot", http.StatusNotFound)
		}
		return
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" && (m.cfg.RequireTickets || !b.snapshot().Public) {
		http.Error(w, "ticket required", http.StatusForbidden)
		return
	}
	// take the slots before the ticket: a full feed does not burn tickets
	if !m.acquireFeed(b) {
		http.Error(w, "too many viewers", http.StatusServiceUnavailable)
		return
	}
	defer m.releaseFeed(b)
	ip := m.remoteIP(r)
	if !b.acquireIP(streamDecisions, ip, m.cfg.MaxViewersPerIP) {
		http.Error(w, "too many viewers from your address", http.StatusServiceUnavailable)
		return
	}
	defer b.releaseIP(streamDecisions, ip)
	if ticket != "" {
		if err := m.redeem(id, ticket, decisionsScope(id)); err != nil {
			http.Error(w, "invalid ticket", http.StatusForbidden)
			return
		}
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: m.cfg.OriginPatterns,
		Subprotocols: []string{DecisionsProtocol}})
	if err != nil {
		return
	}
	defer conn.CloseNow() //nolint:errcheck
	m.feed(r.Context(), conn, b)
}

// acquireFeed takes a decision feed slot of b and of the server.
func (m *Manager) acquireFeed(b *managedBot) bool {
	if m.feeds.Add(1) > int64(m.cfg.MaxViewersTotal) {
		m.feeds.Add(-1)
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.feeds >= int64(m.cfg.MaxViewers) {
		m.feeds.Add(-1)
		return false
	}
	b.feeds++
	return true
}

func (m *Manager) releaseFeed(b *managedBot) {
	b.mu.Lock()
	b.feeds--
	b.mu.Unlock()
	m.feeds.Add(-1)
}

// feed streams b's decision feed on conn until the run ends (bye), the
// viewer leaves or falls too far behind.
func (m *Manager) feed(ctx context.Context, conn *websocket.Conn, b *managedBot) {
	sub := b.run.Bus().Subscribe(m.feedDepth)
	defer sub.Close()
	ctx = conn.CloseRead(ctx) // answers pings and closes; ends ctx when the viewer leaves
	send := func(v any) bool {
		data, err := marshalFeed(v)
		if err != nil {
			return false
		}
		wctx, cancel := context.WithTimeout(ctx, feedWriteTimeout)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, data) == nil
	}
	if m.runFeed(ctx, b, sub, send, time.Second) {
		_ = conn.Close(websocket.StatusNormalClosure, "the run ended")
	}
}

// runFeed sends b's feed messages: hello, then the translated events of
// sub, a gap whenever sub dropped events, stats every interval, and bye
// once the run's bus closed and its status is final. It returns true
// after bye, false when a send failed or ctx ended.
func (m *Manager) runFeed(ctx context.Context, b *managedBot, sub *trace.Subscription, send func(any) bool, interval time.Duration) bool {
	meta := b.snapshot()
	if !send(feedHello{T: "hello", Bot: meta.ID, Backend: meta.Backend, Model: meta.Model, Maps: meta.Maps}) {
		return false
	}
	tr := newFeedTranslator()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var dropped uint64
	gap := func() bool {
		if n := sub.Dropped(); n > dropped {
			m.metrics.traceDropped.WithLabelValues("decisions").Add(float64(n - dropped))
			msg := feedGap{T: "gap", Dropped: n - dropped}
			dropped = n
			return send(msg)
		}
		return true
	}
	last, lastAt := b.stats.counters(), time.Now()
	for {
		select {
		case e, ok := <-sub.C():
			if !ok {
				// the run's bus closed: say how it ended once that is final
				if !gap() {
					return false
				}
				t := time.NewTimer(m.cfg.StopWait)
				select {
				case <-b.done:
				case <-t.C:
				case <-ctx.Done():
					t.Stop()
					return false
				}
				t.Stop()
				return send(feedBye{T: "bye", Status: b.status()})
			}
			if !gap() {
				return false
			}
			for _, msg := range tr.translate(&e) {
				if !send(msg) {
					return false
				}
			}
		case now := <-tick.C:
			if !gap() {
				return false
			}
			c := b.stats.counters()
			rate := 0.0
			if dt := now.Sub(lastAt).Seconds(); dt > 0 {
				rate = float64(c.Decisions-last.Decisions) / dt
			}
			last, lastAt = c, now
			if !send(statsMessage(c, rate)) {
				return false
			}
		case <-ctx.Done():
			return false
		}
	}
}
