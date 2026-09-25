package gametest

import (
	"fmt"
	"path/filepath"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/world"
)

// NewGameFunc creates the game module (game.New by default).
type NewGameFunc func(gi game.Import, rng *crand.Rand) game.Export

// Server drives one scenario exactly like oracle/src/game_main.c.
type Server struct {
	E   *Engine
	Rng *crand.Rand
	Sc  *Scenario

	pak        *pak.Pak
	spawncount int
	framenum   int
	sched      *Schedule

	// Inputs echoed during the current frame (game_main.c input_cmd/input_command).
	Inputs []any
	// randStart is the rand() state at the start of the current dump period.
	randStart crand.Rand
}

// NewServer loads the pak and prepares the engine (Qcommon_Init + SV_Init
// cvars, scenario cvars as "+set").
func NewServer(sc *Scenario, basedir string, newGame NewGameFunc) (*Server, error) {
	p, err := pak.Open(filepath.Join(basedir, "baseq2", "pak0.pak"))
	if err != nil {
		return nil, err
	}
	s := &Server{E: newEngine(), Sc: sc, pak: p, sched: NewSchedule(sc)}
	e := s.E
	cv := e.Cvars

	// oracle_init: "+set k v" early commands (Cvar_Set creates with flags 0)
	for _, kv := range sc.Cvars {
		cv.Set(kv.Key, kv.Value)
	}
	// Qcommon_Init / SV_Init registrations that matter for the game
	cv.Get("dedicated", "0", CVAR_NOSET)
	cv.Get("rcon_password", "", 0)
	cv.Get("skill", "1", 0)
	cv.Get("deathmatch", "0", CVAR_LATCH)
	cv.Get("coop", "0", CVAR_LATCH)
	cv.Get("dmflags", fmt.Sprintf("%d", DF_INSTANT_ITEMS), CVAR_SERVERINFO)
	cv.Get("fraglimit", "0", CVAR_SERVERINFO)
	cv.Get("timelimit", "0", CVAR_SERVERINFO)
	cv.Get("cheats", "0", CVAR_SERVERINFO|CVAR_LATCH)
	cv.Get("protocol", fmt.Sprintf("%d", PROTOCOL_VERSION), CVAR_SERVERINFO|CVAR_NOSET)
	e.maxclients = cv.Get("maxclients", "1", CVAR_SERVERINFO|CVAR_LATCH)
	cv.Get("hostname", "noname", CVAR_SERVERINFO|CVAR_ARCHIVE)
	cv.Get("sv_enforcetime", "0", 0)
	cv.Get("sv_airaccelerate", "0", CVAR_LATCH)

	s.Rng = crand.New(1)

	// ---- SV_InitGame ----
	cv.GetLatchedVars()
	if cv.VariableValue("coop") != 0 && cv.VariableValue("deathmatch") != 0 {
		cv.FullSet("coop", "0", CVAR_SERVERINFO|CVAR_LATCH)
	}
	if cv.VariableValue("deathmatch") != 0 {
		if e.maxclients.Value <= 1 {
			cv.FullSet("maxclients", "8", CVAR_SERVERINFO|CVAR_LATCH)
		} else if e.maxclients.Value > MAX_CLIENTS {
			cv.FullSet("maxclients", fmt.Sprintf("%d", MAX_CLIENTS), CVAR_SERVERINFO|CVAR_LATCH)
		}
	} else if cv.VariableValue("coop") != 0 {
		if e.maxclients.Value <= 1 || e.maxclients.Value > 4 {
			cv.FullSet("maxclients", "4", CVAR_SERVERINFO|CVAR_LATCH)
		}
	} else { // non-deathmatch, non-coop is one player
		cv.FullSet("maxclients", "1", CVAR_SERVERINFO|CVAR_LATCH)
	}
	s.spawncount = 0x1234 // svs.spawncount = rand() happens before srand(seed)
	e.clients = make([]client, e.maxClients())

	if newGame == nil {
		newGame = defaultNewGame
	}
	e.Ge = newGame(e, s.Rng)
	e.Ge.Init()
	edicts := e.Ge.Edicts()
	for i := 0; i < e.maxClients(); i++ {
		ent := &edicts[i+1]
		ent.S.Number = int32(i + 1)
		e.clients[i].edict = ent
	}

	if len(sc.Clients) > e.maxClients() {
		return nil, fmt.Errorf("%d clients > maxclients %g", len(sc.Clients), e.maxclients.Value)
	}
	return s, nil
}

// SpawnServer is SV_SpawnServer(map, "", ss_game, false, false).
// C: server/sv_init.c:169 SV_SpawnServer
func (s *Server) SpawnServer(mapname string) error {
	e := s.E
	s.spawncount++
	e.State = ss_dead
	e.Configstrings = [MAX_CONFIGSTRINGS]string{}
	e.models = [MAX_MODELS]*shared.CModel{}
	e.MC.SZ_Clear()
	s.framenum = 0

	e.Configstrings[CS_NAME] = mapname
	if e.Cvars.VariableValue("deathmatch") != 0 {
		aa := e.Cvars.FindVar("sv_airaccelerate").Value
		e.Configstrings[CS_AIRACCEL] = cFormatG(float64(aa))
		e.airaccelerate = aa
	} else {
		e.Configstrings[CS_AIRACCEL] = "0"
		e.airaccelerate = 0
	}

	for i := range e.clients {
		if e.clients[i].state > cs_connected {
			e.clients[i].state = cs_connected
		}
	}

	e.Configstrings[CS_MODELS+1] = fmt.Sprintf("maps/%s.bsp", mapname)
	raw, err := s.pak.ReadFile(e.Configstrings[CS_MODELS+1])
	if err != nil {
		return err
	}
	m, err := cmodel.LoadMapBytes(e.Configstrings[CS_MODELS+1], raw)
	if err != nil {
		return err
	}
	e.Map = m
	e.CM = cmodel.NewState(m)
	e.models[1] = m.WorldModel()
	e.Configstrings[CS_MAPCHECKSUM] = fmt.Sprintf("%d", int32(m.Checksum))

	e.World = &world.World{
		CM:         e.CM,
		Models:     &e.models,
		WorldEdict: func() *game.Edict { return &e.Ge.Edicts()[0] },
		Loading:    func() bool { return e.State == ss_loading },
		DPrintf:    func(format string, args ...any) { e.logf(format, args...) },
		Printf:     func(format string, args ...any) { e.logf(format, args...) },
	}
	e.World.ClearWorld()

	for i := 1; i < m.NumInlineModels(); i++ {
		name := fmt.Sprintf("*%d", i)
		e.Configstrings[CS_MODELS+1+i] = name
		e.models[i+1] = m.InlineModel(name)
	}

	e.State = ss_loading
	ents := m.EntityString()
	if s.Sc.EntstringOverride != nil {
		ents = *s.Sc.EntstringOverride
	}
	e.Ge.SpawnEntities(mapname, ents, "")

	// run two frames to allow everything to settle
	e.Ge.RunFrame()
	e.Ge.RunFrame()

	e.State = ss_game

	s.createBaseline()

	e.Cvars.FullSet("mapname", mapname, CVAR_SERVERINFO|CVAR_NOSET)
	return nil
}

// createBaseline C: server/sv_init.c:88 SV_CreateBaseline
func (s *Server) createBaseline() {
	edicts := s.E.Ge.Edicts()
	for entnum := 1; entnum < s.E.Ge.NumEdicts(); entnum++ {
		svent := &edicts[entnum]
		if !svent.InUse {
			continue
		}
		if svent.S.ModelIndex == 0 && svent.S.Sound == 0 && svent.S.Effects == 0 {
			continue
		}
		svent.S.Number = int32(entnum)
		svent.S.OldOrigin = svent.S.Origin
	}
}

// ConnectClients is the client loop of game_main.c (SVC_DirectConnect tail,
// SV_UserinfoChanged, "new", "begin").
func (s *Server) ConnectClients() error {
	e := s.E
	edicts := e.Ge.Edicts()
	for i, sc := range s.Sc.Clients {
		cl := &e.clients[i]
		userinfo := sc.Userinfo
		if len(userinfo) > MAX_INFO_STRING-1 {
			userinfo = userinfo[:MAX_INFO_STRING-1]
		}
		userinfo, _ = shared.Info_SetValueForKey(userinfo, "ip", "loopback")
		*cl = client{}
		ent := &edicts[i+1]
		cl.edict = ent
		ok, newinfo := e.Ge.ClientConnect(ent, userinfo)
		if !ok {
			e.logf("client %d rejected: %s\n", i, shared.Info_ValueForKey(newinfo, "rejmsg"))
			continue
		}
		if len(newinfo) > MAX_INFO_STRING-1 {
			newinfo = newinfo[:MAX_INFO_STRING-1]
		}
		cl.userinfo = newinfo
		e.Ge.ClientUserinfoChanged(cl.edict, cl.userinfo) // SV_UserinfoChanged
		cl.state = cs_connected
		s.executeUserCommand(i, "new")
		s.executeUserCommand(i, fmt.Sprintf("begin %d", s.spawncount))
	}
	return nil
}

// executeUserCommand C: server/sv_user.c:477 SV_ExecuteUserCommand
func (s *Server) executeUserCommand(clientNum int, text string) {
	e := s.E
	if len(text) > 1023 {
		text = text[:1023]
	}
	e.Cmd.TokenizeString(text, true)
	cl := &e.clients[clientNum]
	player := cl.edict

	switch e.Cmd.Argv(0) {
	case "new":
		s.svNew(clientNum)
	case "begin":
		s.svBegin(clientNum)
	case "disconnect":
		// SV_DropClient
		if cl.state == cs_spawned {
			e.Ge.ClientDisconnect(cl.edict)
		}
		cl.state = cs_zombie
	case "configstrings", "baselines", "nextserver", "info", "download", "nextdl":
		// only touch client message buffers (never sent by the oracle)
	default:
		if e.State == ss_game {
			e.Ge.ClientCommand(player)
		}
	}
}

// svNew C: server/sv_user.c:58 SV_New_f
func (s *Server) svNew(clientNum int) {
	e := s.E
	cl := &e.clients[clientNum]
	if cl.state != cs_connected {
		e.logf("New not valid -- already spawned\n")
		return
	}
	if e.State == ss_game {
		ent := &e.Ge.Edicts()[clientNum+1]
		ent.S.Number = int32(clientNum + 1)
		cl.edict = ent
	}
}

// svBegin C: server/sv_user.c:230 SV_Begin_f
func (s *Server) svBegin(clientNum int) {
	e := s.E
	cl := &e.clients[clientNum]
	if int(shared.Atoi(e.Cmd.Argv(1))) != s.spawncount {
		e.logf("SV_Begin_f from %d for a different level\n", clientNum)
		s.svNew(clientNum)
		return
	}
	cl.state = cs_spawned
	e.Ge.ClientBegin(cl.edict)
}

// prepWorldFrame C: server/sv_main.c:703 SV_PrepWorldFrame
func (s *Server) prepWorldFrame() {
	edicts := s.E.Ge.Edicts()
	for i := 0; i < s.E.Ge.NumEdicts(); i++ {
		edicts[i].S.Event = 0
	}
}

func ucRecord(c *shared.UserCmd) map[string]any {
	return map[string]any{
		"msec": int64(c.Msec), "buttons": int64(c.Buttons),
		"angles":      []any{int64(c.Angles[0]), int64(c.Angles[1]), int64(c.Angles[2])},
		"forwardmove": int64(c.ForwardMove), "sidemove": int64(c.SideMove), "upmove": int64(c.UpMove),
		"impulse": int64(c.Impulse), "lightlevel": int64(c.LightLevel),
	}
}

// inputCmd is game_main.c input_cmd.
func (s *Server) inputCmd(clientNum int, c shared.UserCmd) {
	s.Inputs = append(s.Inputs, map[string]any{"client": int64(clientNum), "cmd": ucRecord(&c)})
	cl := &s.E.clients[clientNum]
	if cl.state == cs_spawned {
		s.E.Ge.ClientThink(cl.edict, &c)
	}
}

// inputCommand is game_main.c input_command.
func (s *Server) inputCommand(clientNum int, text string) {
	s.Inputs = append(s.Inputs, map[string]any{"client": int64(clientNum), "command": text})
	s.executeUserCommand(clientNum, text)
}

// beginPeriod starts a dump period (game_main.c begin_frame).
func (s *Server) beginPeriod() {
	s.E.Events = nil
	s.Inputs = nil
	s.randStart = *s.Rng
}

// RandCalls returns the rand() calls made since beginPeriod.
func (s *Server) RandCalls() int {
	c := s.randStart
	n := 0
	for c != *s.Rng {
		c.Rand()
		n++
		if n > 100000000 {
			return -1
		}
	}
	return n
}

// Start runs everything up to frame 0 (init, srand, spawn, connect).
func (s *Server) Start() error {
	s.E.Events = nil // drop Init-time events
	s.Rng.Srand(s.Sc.seed())
	s.beginPeriod()
	if err := s.SpawnServer(s.Sc.Map); err != nil {
		return err
	}
	return s.ConnectClients()
}

// EndFrame performs SV_PrepWorldFrame after a dump.
func (s *Server) EndFrame() { s.prepWorldFrame() }

// Input is one scheduled input of a frame (game_main.c input_cmd / input_command).
type Input struct {
	Client  int
	Cmd     *shared.UserCmd // nil for a command
	Command string
}

// Record returns the fixture "inputs" form of the input.
func (in Input) Record() map[string]any {
	if in.Cmd != nil {
		return map[string]any{"client": int64(in.Client), "cmd": ucRecord(in.Cmd)}
	}
	return map[string]any{"client": int64(in.Client), "command": in.Command}
}

// Schedule is the input generator of game_main.c (schedule entries in file
// order, then every active random walk in start order).
type Schedule struct {
	sc    *Scenario
	walks []*rwalk
}

// NewSchedule creates the input generator of a scenario.
func NewSchedule(sc *Scenario) *Schedule { return &Schedule{sc: sc} }

// Inputs returns the inputs of frame f (f >= 1, called once per frame in order).
func (sch *Schedule) Inputs(f int) ([]Input, error) {
	var out []Input
	nclients := len(sch.sc.Clients)
	for _, ent := range sch.sc.Schedule {
		if ent.Frame != f {
			continue
		}
		cn := ent.Client
		if cn < 0 || cn >= nclients {
			continue
		}
		switch {
		case ent.Cmd != nil:
			c, err := parseUC(*ent.Cmd)
			if err != nil {
				return nil, err
			}
			out = append(out, Input{Client: cn, Cmd: &c})
		case ent.Command != nil:
			out = append(out, Input{Client: cn, Command: *ent.Command})
		case ent.Walk != nil && len(sch.walks) < 64:
			w := &rwalk{active: 1, client: cn, start: f, until: *sch.sc.Frames + 1}
			if ent.Walk.Until != nil {
				w.until = *ent.Walk.Until
			}
			var seed uint32 = 1
			if ent.Walk.Seed != nil {
				fv, _ := ent.Walk.Seed.Float64()
				seed = uint32(int64(fv))
			}
			w.r.seed(seed)
			sch.walks = append(sch.walks, w)
		}
	}
	for _, w := range sch.walks {
		if w.active == 0 || f >= w.until {
			continue
		}
		c := w.cmd()
		out = append(out, Input{Client: w.client, Cmd: &c})
	}
	return out, nil
}

// Frame runs server frame f >= 1 (inputs, framenum++, RunFrame). The caller
// dumps and then calls EndFrame.
//
// Schedule entries are applied in file order, interleaved exactly like
// game_main.c: a walk starting at frame f contributes its first command after
// all schedule entries of that frame.
func (s *Server) Frame(f int) error {
	s.beginPeriod()
	ins, err := s.sched.Inputs(f)
	if err != nil {
		return err
	}
	for _, in := range ins {
		if in.Cmd != nil {
			s.inputCmd(in.Client, *in.Cmd)
		} else {
			s.inputCommand(in.Client, in.Command)
		}
	}
	s.framenum++
	s.E.Ge.RunFrame()
	return nil
}
