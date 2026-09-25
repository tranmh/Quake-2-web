package sv

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// MapCache shares immutable collision maps between instances. It is safe for
// concurrent use.
type MapCache struct {
	mu   sync.Mutex
	maps map[string]*cmodel.Map
}

// NewMapCache returns an empty cache.
func NewMapCache() *MapCache { return &MapCache{maps: map[string]*cmodel.Map{}} }

// Load returns the map name loaded through fs, from the cache if possible.
// The key is the name plus the file checksum so different paks don't collide.
func (c *MapCache) Load(fs FileSystem, name string) (*cmodel.Map, error) {
	raw, err := fs.ReadFile(name)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return cmodel.LoadMapBytes(name, raw)
	}
	h := fnv.New64a()
	_, _ = h.Write(raw)
	key := fmt.Sprintf("%s#%d#%x", name, len(raw), h.Sum64())
	c.mu.Lock()
	m := c.maps[key]
	c.mu.Unlock()
	if m != nil {
		return m, nil
	}
	m, err = cmodel.LoadMapBytes(name, raw)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.maps[key] = m
	c.mu.Unlock()
	return m, nil
}

// cmLoadMap is CM_LoadMap for the server: a fresh collision state (portals
// closed, as CM_LoadMap leaves them even for a reloaded map).
// C: qcommon/cmodel.c:548 CM_LoadMap
func (s *Server) cmLoadMap(name string) (*shared.CModel, uint32) {
	var m *cmodel.Map
	if name == "" {
		m = cmodel.EmptyMap()
	} else {
		if s.cfg.FS == nil {
			shared.Error(q2const.ERR_DROP, "Couldn't load %s", name)
		}
		var err error
		m, err = s.cfg.Maps.Load(s.cfg.FS, name)
		if err != nil {
			shared.Error(q2const.ERR_DROP, "Couldn't load %s", name)
		}
	}
	s.CM = cmodel.NewState(m)
	s.World.CM = s.CM
	return m.WorldModel(), m.Checksum
}

// findIndex. C: server/sv_init.c:31 SV_FindIndex
func (s *Server) findIndex(name string, start, max int, create bool) int {
	if name == "" {
		return 0
	}

	i := 1
	for i = 1; i < max && !s.SV.ConfigStrings.Empty(start+i); i++ {
		if s.SV.ConfigStrings.Get(start+i) == name {
			return i
		}
	}

	if !create {
		return 0
	}

	if i == max {
		shared.Error(q2const.ERR_DROP, "*Index: overflow")
	}

	s.SV.ConfigStrings.SetN(start+i, name)

	if s.SV.State != ss_loading {
		// send the update to everyone
		s.SV.Multicast.SZ_Clear()
		s.SV.Multicast.MSG_WriteChar(q2const.Svc_configstring)
		s.SV.Multicast.MSG_WriteShort(int32(start + i))
		s.SV.Multicast.MSG_WriteString(name)
		s.Multicast(&shared.Vec3Origin, q2const.MULTICAST_ALL_R)
	}

	return i
}

// ModelIndex. C: server/sv_init.c:62 SV_ModelIndex
func (s *Server) ModelIndex(name string) int {
	return s.findIndex(name, q2const.CS_MODELS, q2const.MAX_MODELS, true)
}

// SoundIndex. C: server/sv_init.c:67 SV_SoundIndex
func (s *Server) SoundIndex(name string) int {
	return s.findIndex(name, q2const.CS_SOUNDS, q2const.MAX_SOUNDS, true)
}

// ImageIndex. C: server/sv_init.c:72 SV_ImageIndex
func (s *Server) ImageIndex(name string) int {
	return s.findIndex(name, q2const.CS_IMAGES, q2const.MAX_IMAGES, true)
}

// createBaseline takes the current entity states as baselines.
// C: server/sv_init.c:88 SV_CreateBaseline
func (s *Server) createBaseline() {
	n := s.ge.NumEdicts()
	for entnum := 1; entnum < n; entnum++ {
		svent := s.edictNum(entnum)
		if !svent.InUse {
			continue
		}
		if svent.S.ModelIndex == 0 && svent.S.Sound == 0 && svent.S.Effects == 0 {
			continue
		}
		svent.S.Number = int32(entnum)

		// take current state as baseline
		svent.S.OldOrigin = svent.S.Origin
		s.SV.Baselines[entnum] = svent.S
	}
}

// checkForSavegame. C: server/sv_init.c:117 SV_CheckForSavegame
func (s *Server) checkForSavegame() {
	if s.svNoreload.Value != 0 {
		return
	}

	if s.Cvars.VariableValue("deathmatch") != 0 {
		return
	}

	if !s.saveExists("current", s.SV.Name+".sav") {
		return // no savegame
	}

	s.World.ClearWorld()

	// get configstrings and areaportals
	s.readLevelFile()

	if !s.SV.LoadGame {
		// coming back to a level after being in a different
		// level, so run it for ten seconds

		// rlava2 was sending too many lightstyles, and overflowing the
		// reliable data. temporarily changing the server state to loading
		// prevents these from being passed down.
		previousState := s.SV.State
		s.SV.State = ss_loading
		for i := 0; i < 100; i++ {
			s.ge.RunFrame()
		}
		s.SV.State = previousState
	}
}

// cFormatG is C printf "%g" of a double.
func cFormatG(d float64) string {
	return strconv.FormatFloat(d, 'g', 6, 64)
}

// spawnServer changes the server to a new map, taking all connected clients
// along with it.
// C: server/sv_init.c:166 SV_SpawnServer
func (s *Server) spawnServer(server, spawnpoint string, serverstate int, attractloop, loadgame bool) {
	if attractloop {
		s.Cvars.Set("paused", "0")
	}

	s.Printf("------- Server Initialization -------\n")

	s.DPrintf("SpawnServer: %s\n", server)
	s.SV.DemoFile = nil

	s.SVS.SpawnCount++ // any partially connected client will be restarted
	s.SV.State = ss_dead

	// wipe the entire per-level structure
	s.SV = ServerT{}
	s.SVS.RealTime = 0
	s.SV.LoadGame = loadgame
	s.SV.AttractLoop = attractloop

	// save name for levels that don't set message
	s.SV.ConfigStrings.Set(q2const.CS_NAME, server)
	if s.Cvars.VariableValue("deathmatch") != 0 {
		s.SV.ConfigStrings.Set(q2const.CS_AIRACCEL, cFormatG(float64(s.svAirAccelerate.Value)))
		s.pmAirAccelerate = s.svAirAccelerate.Value
	} else {
		s.SV.ConfigStrings.Set(q2const.CS_AIRACCEL, "0")
		s.pmAirAccelerate = 0
	}

	s.SV.Multicast.SZ_Init(s.SV.multicastBuf[:])

	s.SV.Name = server

	// leave slots at start for clients only
	for i := 0; i < s.maxClients(); i++ {
		// needs to reconnect
		if s.SVS.Clients[i].State > cs_connected {
			s.SVS.Clients[i].State = cs_connected
		}
		s.SVS.Clients[i].LastFrame = -1
	}

	s.SV.Time = 1000

	s.SV.Name = server
	s.SV.ConfigStrings.Set(q2const.CS_NAME, server)

	var checksum uint32
	if serverstate != ss_game {
		s.SV.Models[1], checksum = s.cmLoadMap("") // no real map
	} else {
		s.SV.ConfigStrings.Sprintf(q2const.CS_MODELS+1, "maps/%s.bsp", server)
		s.SV.Models[1], checksum = s.cmLoadMap(s.SV.ConfigStrings.Get(q2const.CS_MODELS + 1))
	}
	s.SV.ConfigStrings.Sprintf(q2const.CS_MAPCHECKSUM, "%d", int32(checksum))

	// clear physics interaction links
	s.World.ClearWorld()

	m := s.CM.Map()
	for i := 1; i < m.NumInlineModels(); i++ {
		s.SV.ConfigStrings.Sprintf(q2const.CS_MODELS+1+i, "*%d", i)
		s.SV.Models[i+1] = m.InlineModel(s.SV.ConfigStrings.Get(q2const.CS_MODELS + 1 + i))
	}

	// spawn the rest of the entities on the map

	// precache and static commands can be issued during
	// map initialization
	s.SV.State = ss_loading

	// load and spawn all other entities
	s.ge.SpawnEntities(s.SV.Name, m.EntityString(), spawnpoint)

	// run two frames to allow everything to settle
	s.ge.RunFrame()
	s.ge.RunFrame()

	// all precaches are complete
	s.SV.State = serverstate

	// create a baseline for more efficient communications
	s.createBaseline()

	// check for a savegame
	s.checkForSavegame()

	// set serverinfo variable
	s.Cvars.FullSet("mapname", s.SV.Name, q2const.CVAR_SERVERINFO|q2const.CVAR_NOSET)

	s.Printf("-------------------------------------\n")
}

// initGame: a brand new game has been started.
// C: server/sv_init.c:285 SV_InitGame
func (s *Server) initGame() {
	if s.SVS.Initialized {
		// cause any connected clients to reconnect
		s.svShutdown("Server restarted\n", true)
	}
	// else: CL_Drop / SCR_BeginLoadingPlaque (no local client)

	// get any latched variable changes (maxclients, etc)
	s.Cvars.GetLatchedVars()

	s.SVS.Initialized = true

	if s.Cvars.VariableValue("coop") != 0 && s.Cvars.VariableValue("deathmatch") != 0 {
		s.Printf("Deathmatch and Coop both set, disabling Coop\n")
		s.Cvars.FullSet("coop", "0", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	}

	// dedicated servers are can't be single player and are usually DM
	// so unless they explicity set coop, force it to deathmatch
	if s.dedicated.Value != 0 {
		if s.Cvars.VariableValue("coop") == 0 {
			s.Cvars.FullSet("deathmatch", "1", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
		}
	}

	// init clients
	if s.Cvars.VariableValue("deathmatch") != 0 {
		if s.maxclients.Value <= 1 {
			s.Cvars.FullSet("maxclients", "8", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
		} else if s.maxclients.Value > q2const.MAX_CLIENTS {
			s.Cvars.FullSet("maxclients", fmt.Sprintf("%d", q2const.MAX_CLIENTS), q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
		}
	} else if s.Cvars.VariableValue("coop") != 0 {
		if s.maxclients.Value <= 1 || s.maxclients.Value > 4 {
			s.Cvars.FullSet("maxclients", "4", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
		}
	} else { // non-deathmatch, non-coop is one player
		s.Cvars.FullSet("maxclients", "1", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	}

	s.SVS.SpawnCount = int(s.Rand.Rand())
	s.SVS.Clients = make([]Client, s.maxClients())
	for i := range s.SVS.Clients {
		s.SVS.Clients[i].index = i
	}
	s.SVS.NumClientEntities = s.maxClients() * q2const.UPDATE_BACKUP * 64
	s.SVS.ClientEntities = make([]shared.EntityState, s.SVS.NumClientEntities)

	// init network stuff: NET_Config is the host's business

	// heartbeats will always be sent to the id master; masters are replaced
	// by the server browser API (master_adr[0] left empty)
	s.SVS.LastHeartbeat = -99999 // send immediately

	// init game
	s.initGameProgs()
	for i := 0; i < s.maxClients(); i++ {
		ent := s.edictNum(i + 1)
		ent.S.Number = int32(i + 1)
		s.SVS.Clients[i].Edict = ent
		s.SVS.Clients[i].LastCmd = shared.UserCmd{}
	}
}

// Map starts a map: the full syntax is map [*]<map>$<startspot>+<nextserver>.
// C: server/sv_init.c:386 SV_Map
func (s *Server) Map(attractloop bool, levelstring string, loadgame bool) {
	s.SV.LoadGame = loadgame
	s.SV.AttractLoop = attractloop

	if s.SV.State == ss_dead && !s.SV.LoadGame {
		s.initGame() // the game is just starting
	}

	level := levelstring

	// if there is a + in the map, set nextserver to the remainder
	if ch := strings.Index(level, "+"); ch >= 0 {
		s.Cvars.Set("nextserver", fmt.Sprintf("gamemap \"%s\"", level[ch+1:]))
		level = level[:ch]
	} else {
		s.Cvars.Set("nextserver", "")
	}

	//ZOID special hack for end game screen in coop mode
	if s.Cvars.VariableValue("coop") != 0 && shared.Q_stricmp(level, "victory.pcx") == 0 {
		s.Cvars.Set("nextserver", "gamemap \"*base1\"")
	}

	// if there is a $, use the remainder as a spawnpoint
	spawnpoint := ""
	if ch := strings.Index(level, "$"); ch >= 0 {
		spawnpoint = level[ch+1:]
		level = level[:ch]
	}

	// skip the end-of-unit flag if necessary
	if strings.HasPrefix(level, "*") {
		level = level[1:]
	}

	l := len(level)
	switch {
	case l > 4 && level[l-4:] == ".cin":
		s.BroadcastCommand("changing\n")
		s.spawnServer(level, spawnpoint, ss_cinematic, attractloop, loadgame)
	case l > 4 && level[l-4:] == ".dm2":
		s.BroadcastCommand("changing\n")
		s.spawnServer(level, spawnpoint, ss_demo, attractloop, loadgame)
	case l > 4 && level[l-4:] == ".pcx":
		s.BroadcastCommand("changing\n")
		s.spawnServer(level, spawnpoint, ss_pic, attractloop, loadgame)
	default:
		s.BroadcastCommand("changing\n")
		s.SendClientMessages()
		s.spawnServer(level, spawnpoint, ss_game, attractloop, loadgame)
		s.Cmd.Cbuf_CopyToDefer()
	}

	s.BroadcastCommand("reconnect\n")
}

// edicts helper used by the world (ge->edicts).
func (s *Server) worldEdict() *game.Edict {
	if s.ge == nil {
		return nil
	}
	return &s.ge.Edicts()[0]
}
