package auth

import (
	"errors"
	"sync"
	"time"
)

// TicketTTL is the lifetime of a WebSocket join ticket.
const TicketTTL = 60 * time.Second

// ErrTicketInvalid is returned for unknown, expired, reused or
// wrong-game tickets.
var ErrTicketInvalid = errors.New("auth: invalid join ticket")

// Ticket is a one-time authorization to open the game WebSocket
// (GET /ws/v1/games/{id}?ticket=...).
type Ticket struct {
	Token       string    `json:"ticket"`
	UserID      int64     `json:"userId"`
	DisplayName string    `json:"displayName"`
	GameID      string    `json:"gameId"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// TicketService issues and redeems join tickets. The API's GameHost.Join
// implementation calls Issue; the WebSocket handler calls Redeem.
type TicketService interface {
	Issue(userID int64, displayName, gameID string) (Ticket, error)
	Redeem(token, gameID string) (Ticket, error)
}

// Tickets is the in-memory TicketService (single process; tickets do not
// survive restarts, which is fine for a 60 s TTL).
type Tickets struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	m   map[string]Ticket
}

// NewTickets returns a ticket store with the given TTL (0 → TicketTTL).
func NewTickets(ttl time.Duration) *Tickets {
	if ttl <= 0 {
		ttl = TicketTTL
	}
	return &Tickets{ttl: ttl, now: time.Now, m: map[string]Ticket{}}
}

// SetClock replaces the time source (tests).
func (t *Tickets) SetClock(now func() time.Time) { t.mu.Lock(); t.now = now; t.mu.Unlock() }

// Issue implements TicketService.
func (t *Tickets) Issue(userID int64, displayName, gameID string) (Ticket, error) {
	tok, err := NewToken()
	if err != nil {
		return Ticket{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	for k, v := range t.m { // sweep
		if !v.ExpiresAt.After(now) {
			delete(t.m, k)
		}
	}
	tk := Ticket{Token: tok, UserID: userID, DisplayName: displayName, GameID: gameID, ExpiresAt: now.Add(t.ttl)}
	t.m[tok] = tk
	return tk, nil
}

// Redeem implements TicketService: the ticket is consumed even when it
// does not match gameID.
func (t *Tickets) Redeem(token, gameID string) (Ticket, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tk, ok := t.m[token]
	if !ok {
		return Ticket{}, ErrTicketInvalid
	}
	delete(t.m, token)
	if !tk.ExpiresAt.After(t.now()) || tk.GameID != gameID {
		return Ticket{}, ErrTicketInvalid
	}
	return tk, nil
}

// Len returns the number of outstanding tickets.
func (t *Tickets) Len() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.m) }

var _ TicketService = (*Tickets)(nil)
