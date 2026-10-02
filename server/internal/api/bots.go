package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// Bot statuses (BotInfo.Status).
const (
	BotStarting = "starting" // created; the bot has not entered its first level yet
	BotRunning  = "running"
	BotFinished = "finished" // the run completed (see BotInfo.Summary)
	BotFailed   = "failed"   // the run ended without completing, or could not be played
	BotStopped  = "stopped"  // stopped: DELETE, its wall-clock limit, a budget, or server shutdown
)

// BotLive reports whether a bot with status st can be watched.
func BotLive(st string) bool { return st == BotStarting || st == BotRunning }

// Bot artifact kinds (BotArtifactInfo.Kind).
const (
	ArtifactRun     = "run"     // run.json
	ArtifactEpisode = "episode" // ep-NNN/episode.json
	ArtifactTrace   = "trace"   // ep-NNN/trace.jsonl.gz
	ArtifactDemo    = "demo"    // ep-NNN/demos/NN-<map>.dm2
)

// BotSpec is the body of POST /api/v1/bots.
type BotSpec struct {
	Name string `json:"name,omitempty"`
	// Maps is a prefix of the campaign's visits starting with its first
	// map (default: the whole campaign).
	Maps []string `json:"maps,omitempty"`
	// Skill is 0..3 (default 1).
	Skill *int `json:"skill,omitempty"`
	// Backend is scripted, jev, mock, constant or random.
	Backend string `json:"backend"`
	// SimLatency is the model latency to simulate ("212ms" or a list of
	// samples "80ms,212ms"); in the server's realtime games it delays
	// the mock backend's answers (default 212ms) and is ignored by the
	// other backends.
	SimLatency string `json:"simLatency,omitempty"`
	Public     bool   `json:"public,omitempty"`
}

// BotUser is the account acting on bots (ID 0: anonymous).
type BotUser struct {
	ID    int64
	Name  string
	Admin bool
}

// BotLevel is the level a live bot plays.
type BotLevel struct {
	Map   string `json:"map"`
	Visit int    `json:"visit"`
}

// BotLiveStats are a bot's running counters.
type BotLiveStats struct {
	Kills      int     `json:"kills"`
	Deaths     int     `json:"deaths"`
	Decisions  int     `json:"decisions"`
	CostUSD    float64 `json:"costUsd"`
	ModelShare float64 `json:"modelShare"`
}

// BotArtifactInfo names a file of a bot's run directory.
type BotArtifactInfo struct {
	// Name is the path inside the run directory (run.json,
	// ep-NNN/episode.json, ep-NNN/trace.jsonl.gz, ep-NNN/demos/NN-map.dm2).
	Name string `json:"name"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
}

// BotInfo describes a bot: a live one, or an ended run (including runs
// the q2bot command line wrote into the server's runs directory).
type BotInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	OwnerID   int64      `json:"ownerId,omitempty"`
	Status    string     `json:"status"`
	Reason    string     `json:"reason,omitempty"` // why it failed or stopped
	Backend   string     `json:"backend"`
	Model     string     `json:"model,omitempty"`
	Maps      []string   `json:"maps"`
	Skill     int        `json:"skill"`
	Public    bool       `json:"public"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	// Viewers are the live watch connections.
	Viewers int           `json:"viewers"`
	Level   *BotLevel     `json:"level,omitempty"`
	Live    *BotLiveStats `json:"live,omitempty"`
	// Summary is run.json (schema q2bot.run/1) of an ended run (GET of
	// one bot only).
	Summary   json.RawMessage   `json:"summary,omitempty"`
	Artifacts []BotArtifactInfo `json:"artifacts"`
}

// BotWatch is the answer of POST /api/v1/bots/{id}/watch: one-time
// tickets for the live video relay (raw netchan datagrams, ADR-0001) and
// the decision feed (JSON text frames, protocol q2bot.decisions/1).
type BotWatch struct {
	Ticket          string `json:"ticket"`
	WSURL           string `json:"wsUrl"`
	DecisionsTicket string `json:"decisionsTicket"`
	DecisionsURL    string `json:"decisionsUrl"`
	// Pakset is the pakset whose assets the viewer loads.
	Pakset string `json:"pakset"`
}

// BotArtifact is an open artifact of a run.
type BotArtifact struct {
	Name        string
	Kind        string
	ContentType string
	// ContentEncoding is the encoding the file is stored in ("gzip" for
	// a trace, served as is).
	ContentEncoding string
	Size            int64
	ModTime         time.Time
	Content         io.ReadSeekCloser
}

// Errors of BotHost (the API answers with the status in parentheses).
var (
	ErrBotNotFound  = errors.New("bot not found")                  // 404 (also for bots the caller may not see)
	ErrBotForbidden = errors.New("not allowed")                    // 403
	ErrBotLimit     = errors.New("too many bots")                  // 429
	ErrBotsDisabled = errors.New("bots are unavailable")           // 503
	ErrBotInvalid   = errors.New("invalid bot settings")           // 400
	ErrBotNotLive   = errors.New("the bot is not running anymore") // 409
)

// BotHost runs the server's AI bots (agent/runner.Manager). It enforces
// who may start which bot, the caps, and visibility: a bot is visible to
// everyone when public, else to its owner and administrators; an
// invisible bot is ErrBotNotFound. Error messages wrapping the errors
// above are shown to the caller.
type BotHost interface {
	// Start starts a bot owned by u.
	Start(ctx context.Context, spec BotSpec, u BotUser) (BotInfo, error)
	// List returns the bots u may see, live ones first, then ended runs,
	// newest first (without summaries).
	List(ctx context.Context, u BotUser) ([]BotInfo, error)
	// Get returns one bot with its summary.
	Get(ctx context.Context, id string, u BotUser) (BotInfo, error)
	// Stop stops a live bot of u (or any, for an administrator) and waits
	// a while for its run to end (a run still ending after that is no
	// error); stopping an ended bot does nothing.
	Stop(ctx context.Context, id string, u BotUser) error
	// Watch issues one-time tickets for both live streams of a live bot
	// (ErrBotNotLive once it ended).
	Watch(ctx context.Context, id string, u BotUser) (BotWatch, error)
	// OpenArtifact opens an allowlisted file of a bot's run directory.
	OpenArtifact(ctx context.Context, id, name string, u BotUser) (*BotArtifact, error)
}
