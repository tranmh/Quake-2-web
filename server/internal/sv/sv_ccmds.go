package sv

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

/*
===============================================================================

OPERATOR CONSOLE ONLY COMMANDS

===============================================================================
*/

// setMaster_f specifies a list of master servers. Masters are replaced by the
// server browser API: addresses are recorded but no datagrams are sent.
// C: server/sv_ccmds.c:37 SV_SetMaster_f
func (s *Server) setMaster_f() {
	// only dedicated servers send heartbeats
	if s.dedicated.Value == 0 {
		s.Printf("Only dedicated servers use masters.\n")
		return
	}

	// make sure the server is listed public
	s.Cvars.Set("public", "1")

	for i := 1; i < MAX_MASTERS; i++ {
		s.masterAdr[i] = qnet.Addr{}
	}

	slot := 1 // slot 0 will always contain the id master
	for i := 1; i < s.Cmd.Argc(); i++ {
		if slot == MAX_MASTERS {
			break
		}
		a := s.Cmd.Argv(i)
		host, port := a, q2const.PORT_MASTER
		if j := strings.LastIndexByte(a, ':'); j >= 0 {
			host = a[:j]
			port = int(shared.Atoi(a[j+1:]))
		}
		if host == "" {
			s.Printf("Bad address: %s\n", a)
			continue
		}
		s.masterAdr[slot] = qnet.Addr{Base: host, Port: port}
		s.Printf("Master server at %s\n", s.masterAdr[slot])
		s.Printf("Sending a ping.\n")
		slot++
	}

	s.SVS.LastHeartbeat = -9999999
}

// setPlayer sets sv_client and sv_player to the player with idnum Cmd_Argv(1).
// C: server/sv_ccmds.c:88 SV_SetPlayer
func (s *Server) setPlayer() bool {
	if s.Cmd.Argc() < 2 {
		return false
	}

	str := s.Cmd.Argv(1)

	// numeric values are just slot numbers
	if str[0] >= '0' && str[0] <= '9' {
		idnum := int(shared.Atoi(s.Cmd.Argv(1)))
		if idnum < 0 || idnum >= s.maxClients() {
			s.Printf("Bad client slot: %d\n", idnum)
			return false
		}

		s.client = &s.SVS.Clients[idnum]
		s.player = s.client.Edict
		if s.client.State == cs_free {
			s.Printf("Client %d is not active\n", idnum)
			return false
		}
		return true
	}

	// check for a name match
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_free {
			continue
		}
		if cl.Name == str {
			s.client = cl
			s.player = s.client.Edict
			return true
		}
	}

	s.Printf("Userid %s is not on the server\n", str)
	return false
}

/*
===============================================================================

SAVEGAME FILES

The C code writes <gamedir>/save/<slot>/{server.ssv,game.ssv,<map>.sav,<map>.sv2}.
Here the same named blobs live in a SaveStore. server.ssv and <map>.sv2 use a
versioned JSON envelope; game.ssv and <map>.sav are the game's own bytes.

===============================================================================
*/

// ErrNoSave is returned by SaveStore.Read for a missing blob.
var ErrNoSave = errors.New("sv: savegame file not found")

// SaveStore stores savegame blobs by slot ("current", "save0", user names)
// and file name.
type SaveStore interface {
	Read(slot, name string) ([]byte, error) // ErrNoSave if missing
	Write(slot, name string, data []byte) error
	Remove(slot, name string) error
	List(slot string) ([]string, error) // file names in the slot, sorted
}

// MemSaveStore is an in-memory SaveStore, safe for concurrent use.
type MemSaveStore struct {
	mu    sync.Mutex
	slots map[string]map[string][]byte
}

// NewMemSaveStore returns an empty store.
func NewMemSaveStore() *MemSaveStore {
	return &MemSaveStore{slots: map[string]map[string][]byte{}}
}

func (m *MemSaveStore) Read(slot, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.slots[slot][name]
	if !ok {
		return nil, ErrNoSave
	}
	return append([]byte(nil), d...), nil
}

func (m *MemSaveStore) Write(slot, name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.slots[slot] == nil {
		m.slots[slot] = map[string][]byte{}
	}
	m.slots[slot][name] = append([]byte(nil), data...)
	return nil
}

func (m *MemSaveStore) Remove(slot, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.slots[slot], name)
	return nil
}

func (m *MemSaveStore) List(slot string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for n := range m.slots[slot] {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Server) saveExists(slot, name string) bool {
	_, err := s.cfg.Saves.Read(slot, name)
	return err == nil
}

// SaveVersion is the version of the server's JSON save envelopes.
const SaveVersion = 1

// serverFile is the content of server.ssv.
type serverFile struct {
	Version int         `json:"version"`
	Comment string      `json:"comment"` // char comment[32]
	MapCmd  string      `json:"mapcmd"`  // char mapcmd[MAX_TOKEN_CHARS]
	Latched [][2]string `json:"latched"` // CVAR_LATCH cvars in cvar_vars order
}

// levelFile is the content of <map>.sv2.
type levelFile struct {
	Version int `json:"version"`
	// ConfigStrings is the raw char configstrings[MAX_CONFIGSTRINGS][MAX_QPATH].
	ConfigStrings []byte `json:"configstrings"`
	// Portals is CM_WritePortalState (MAX_MAP_AREAPORTALS little-endian ints).
	Portals []byte `json:"portals"`
}

// wipeSavegame deletes save/<XXX>/.
// C: server/sv_ccmds.c:151 SV_WipeSavegame
func (s *Server) wipeSavegame(savename string) {
	s.DPrintf("SV_WipeSaveGame(%s)\n", savename)

	st := s.cfg.Saves
	_ = st.Remove(savename, "server.ssv")
	_ = st.Remove(savename, "game.ssv")

	names, _ := st.List(savename)
	for _, n := range names {
		if strings.HasSuffix(n, ".sav") {
			_ = st.Remove(savename, n)
		}
	}
	names, _ = st.List(savename)
	for _, n := range names {
		if strings.HasSuffix(n, ".sv2") {
			_ = st.Remove(savename, n)
		}
	}
}

// copyFile. C: server/sv_ccmds.c:186 CopyFile
func (s *Server) copyFile(srcSlot, srcName, dstSlot, dstName string) {
	s.DPrintf("CopyFile (%s/%s, %s/%s)\n", srcSlot, srcName, dstSlot, dstName)
	d, err := s.cfg.Saves.Read(srcSlot, srcName)
	if err != nil {
		return
	}
	_ = s.cfg.Saves.Write(dstSlot, dstName, d)
}

// copySaveGame. C: server/sv_ccmds.c:221 SV_CopySaveGame
func (s *Server) copySaveGame(src, dst string) {
	s.DPrintf("SV_CopySaveGame(%s, %s)\n", src, dst)

	s.wipeSavegame(dst)

	// copy the savegame over
	s.copyFile(src, "server.ssv", dst, "server.ssv")
	s.copyFile(src, "game.ssv", dst, "game.ssv")

	names, _ := s.cfg.Saves.List(src)
	for _, n := range names {
		if !strings.HasSuffix(n, ".sav") {
			continue
		}
		s.copyFile(src, n, dst, n)

		// change sav to sv2
		n2 := n[:len(n)-3] + "sv2"
		s.copyFile(src, n2, dst, n2)
	}
}

// writeLevelFile. C: server/sv_ccmds.c:265 SV_WriteLevelFile
func (s *Server) writeLevelFile() {
	s.DPrintf("SV_WriteLevelFile()\n")

	lf := levelFile{
		Version:       SaveVersion,
		ConfigStrings: append([]byte(nil), s.SV.ConfigStrings.Raw()...),
		Portals:       s.CM.WritePortalState(),
	}
	data, err := json.Marshal(&lf)
	if err == nil {
		err = s.cfg.Saves.Write("current", s.SV.Name+".sv2", data)
	}
	if err != nil {
		s.Printf("Failed to open %s\n", "save/current/"+s.SV.Name+".sv2")
		return
	}

	game, err := s.ge.WriteLevel()
	if err == nil {
		err = s.cfg.Saves.Write("current", s.SV.Name+".sav", game)
	}
	if err != nil {
		s.Printf("Couldn't write level: %v\n", err)
	}
}

// readLevelFile. C: server/sv_ccmds.c:292 SV_ReadLevelFile
func (s *Server) readLevelFile() {
	s.DPrintf("SV_ReadLevelFile()\n")

	data, err := s.cfg.Saves.Read("current", s.SV.Name+".sv2")
	var lf levelFile
	if err == nil {
		err = json.Unmarshal(data, &lf)
	}
	if err == nil && lf.Version != SaveVersion {
		err = fmt.Errorf("unsupported level file version %d", lf.Version)
	}
	if err != nil {
		s.Printf("Failed to open %s\n", "save/current/"+s.SV.Name+".sv2")
		return
	}
	s.SV.ConfigStrings = ConfigStrings{}
	copy(s.SV.ConfigStrings.Raw(), lf.ConfigStrings)
	s.CM.ReadPortalState(lf.Portals)

	game, err := s.cfg.Saves.Read("current", s.SV.Name+".sav")
	if err == nil {
		err = s.ge.ReadLevel(game)
	}
	if err != nil {
		shared.Error(q2const.ERR_DROP, "Couldn't read level %s: %v", s.SV.Name, err)
	}
}

// writeServerFile. C: server/sv_ccmds.c:319 SV_WriteServerFile
func (s *Server) writeServerFile(autosave bool) {
	s.DPrintf("SV_WriteServerFile(%s)\n", map[bool]string{true: "true", false: "false"}[autosave])

	var comment string
	if !autosave {
		now := s.now()
		comment = fmt.Sprintf("%2d:%d%d %2d/%2d  ", now.Hour(), now.Minute()/10, now.Minute()%10,
			int(now.Month()), now.Day())
		comment += s.SV.ConfigStrings.Get(q2const.CS_NAME)
	} else {
		// autosaved
		comment = fmt.Sprintf("ENTERING %s", s.SV.ConfigStrings.Get(q2const.CS_NAME))
	}
	if len(comment) > 31 {
		comment = comment[:31]
	}

	sf := serverFile{Version: SaveVersion, Comment: comment, MapCmd: s.SVS.MapCmd}

	// write all CVAR_LATCH cvars
	// these will be things like coop, skill, deathmatch, etc
	s.Cvars.Each(func(v *cvar.Cvar) {
		if v.Flags&q2const.CVAR_LATCH == 0 {
			return
		}
		if len(v.Name) >= q2const.MAX_OSPATH-1 || len(v.String) >= 128-1 {
			s.Printf("Cvar too long: %s = %s\n", v.Name, v.String)
			return
		}
		sf.Latched = append(sf.Latched, [2]string{v.Name, v.String})
	})

	data, err := json.Marshal(&sf)
	if err == nil {
		err = s.cfg.Saves.Write("current", "server.ssv", data)
	}
	if err != nil {
		s.Printf("Couldn't write %s\n", "save/current/server.ssv")
		return
	}

	// write game state
	game, err := s.ge.WriteGame(autosave)
	if err == nil {
		err = s.cfg.Saves.Write("current", "game.ssv", game)
	}
	if err != nil {
		s.Printf("Couldn't write game: %v\n", err)
	}
}

// readServerFile. C: server/sv_ccmds.c:393 SV_ReadServerFile
func (s *Server) readServerFile() {
	s.DPrintf("SV_ReadServerFile()\n")

	data, err := s.cfg.Saves.Read("current", "server.ssv")
	var sf serverFile
	if err == nil {
		err = json.Unmarshal(data, &sf)
	}
	if err == nil && sf.Version != SaveVersion {
		err = fmt.Errorf("unsupported server file version %d", sf.Version)
	}
	if err != nil {
		s.Printf("Couldn't read %s\n", "save/current/server.ssv")
		return
	}

	// read all CVAR_LATCH cvars
	// these will be things like coop, skill, deathmatch, etc
	for _, kv := range sf.Latched {
		s.DPrintf("Set %s = %s\n", kv[0], kv[1])
		s.Cvars.ForceSet(kv[0], kv[1])
	}

	// start a new game fresh with new cvars
	s.initGame()

	s.SVS.MapCmd = sf.MapCmd

	// read game state
	game, err := s.cfg.Saves.Read("current", "game.ssv")
	if err == nil {
		err = s.ge.ReadGame(game)
	}
	if err != nil {
		shared.Error(q2const.ERR_DROP, "Couldn't read game: %v", err)
	}

	// ReadGame reallocates the edicts: SV_InitGame's client->edict pointers
	// would dangle (C: freed memory; a later "save" dereferenced a stale
	// edict without a client). Memory-safety fix.
	for i := range s.SVS.Clients {
		s.SVS.Clients[i].Edict = s.edictNum(i + 1)
	}
}

// demoMap_f puts the server in demo mode on a specific map/cinematic.
// C: server/sv_ccmds.c:451 SV_DemoMap_f
func (s *Server) demoMap_f() {
	s.Map(true, s.Cmd.Argv(1), false)
}

// gameMap_f saves the state of the map just being exited and goes to a new
// map. A leading '*' starts a new unit and clears the archived maps.
// C: server/sv_ccmds.c:474 SV_GameMap_f
func (s *Server) gameMap_f() {
	if s.Cmd.Argc() != 2 {
		s.Printf("USAGE: gamemap <map>\n")
		return
	}

	s.DPrintf("SV_GameMap(%s)\n", s.Cmd.Argv(1))

	// check for clearing the current savegame
	mapname := s.Cmd.Argv(1)
	if strings.HasPrefix(mapname, "*") {
		// wipe all the *.sav files
		s.wipeSavegame("current")
	} else {
		// save the map just exited
		if s.SV.State == ss_game {
			// clear all the client inuse flags before saving so that
			// when the level is re-entered, the clients will spawn
			// at spawn points instead of occupying body shells
			savedInuse := make([]bool, s.maxClients())
			for i := 0; i < s.maxClients(); i++ {
				cl := &s.SVS.Clients[i]
				savedInuse[i] = cl.Edict.InUse
				cl.Edict.InUse = false
			}

			s.writeLevelFile()

			// we must restore these for clients to transfer over correctly
			for i := 0; i < s.maxClients(); i++ {
				s.SVS.Clients[i].Edict.InUse = savedInuse[i]
			}
		}
	}

	// start up the next map
	s.Map(false, mapname, false)

	// archive server state
	mc := mapname
	if len(mc) > MAX_TOKEN_CHARS-1 {
		mc = mc[:MAX_TOKEN_CHARS-1]
	}
	s.SVS.MapCmd = mc

	// copy off the level to the autosave slot
	if s.dedicated.Value == 0 {
		s.writeServerFile(true)
		s.copySaveGame("current", "save0")
	}
}

// map_f goes directly to a given map without any savegame archiving.
// C: server/sv_ccmds.c:540 SV_Map_f
func (s *Server) map_f() {
	// if not a pcx, demo, or cinematic, check to make sure the level exists
	mapname := s.Cmd.Argv(1)
	if !strings.Contains(mapname, ".") {
		expanded := fmt.Sprintf("maps/%s.bsp", mapname)
		ok := false
		if s.cfg.FS != nil {
			_, err := s.cfg.FS.ReadFile(expanded)
			ok = err == nil
		}
		if !ok {
			s.Printf("Can't find %s\n", expanded)
			return
		}
	}

	s.SV.State = ss_dead // don't save current level when changing
	s.wipeSavegame("current")
	s.gameMap_f()
}

/*
=====================================================================

  SAVEGAMES

=====================================================================
*/

// loadgame_f. C: server/sv_ccmds.c:576 SV_Loadgame_f
func (s *Server) loadgame_f() {
	if s.Cmd.Argc() != 2 {
		s.Printf("USAGE: loadgame <directory>\n")
		return
	}

	s.Printf("Loading game...\n")

	dir := s.Cmd.Argv(1)
	if strings.Contains(dir, "..") || strings.Contains(dir, "/") || strings.Contains(dir, "\\") {
		s.Printf("Bad savedir.\n")
	}

	// make sure the server.ssv file exists
	if !s.saveExists(dir, "server.ssv") {
		s.Printf("No such savegame: %s\n", "save/"+dir+"/server.ssv")
		return
	}

	s.copySaveGame(dir, "current")

	s.readServerFile()

	// go to the map
	s.SV.State = ss_dead // don't save current level when changing
	s.Map(false, s.SVS.MapCmd, true)
}

// savegame_f. C: server/sv_ccmds.c:618 SV_Savegame_f
func (s *Server) savegame_f() {
	if s.SV.State != ss_game {
		s.Printf("You must be in a game to save.\n")
		return
	}

	if s.Cmd.Argc() != 2 {
		s.Printf("USAGE: savegame <directory>\n")
		return
	}

	if s.Cvars.VariableValue("deathmatch") != 0 {
		s.Printf("Can't savegame in a deathmatch\n")
		return
	}

	if s.Cmd.Argv(1) == "current" {
		s.Printf("Can't save to 'current'\n")
		return
	}

	if s.maxclients.Value == 1 && s.SVS.Clients[0].Edict.Client.PS.Stats[q2const.STAT_HEALTH] <= 0 {
		s.Printf("\nCan't savegame while dead!\n")
		return
	}

	dir := s.Cmd.Argv(1)
	if strings.Contains(dir, "..") || strings.Contains(dir, "/") || strings.Contains(dir, "\\") {
		s.Printf("Bad savedir.\n")
	}

	s.Printf("Saving game...\n")

	// archive current level, including all client edicts.
	// when the level is reloaded, they will be shells awaiting
	// a connecting client
	s.writeLevelFile()

	// save server state
	s.writeServerFile(false)

	// copy it off
	s.copySaveGame("current", dir)

	s.Printf("Done.\n")
}

// kick_f kicks a user off of the server.
// C: server/sv_ccmds.c:676 SV_Kick_f
func (s *Server) kick_f() {
	if !s.SVS.Initialized {
		s.Printf("No server running.\n")
		return
	}

	if s.Cmd.Argc() != 2 {
		s.Printf("Usage: kick <userid>\n")
		return
	}

	if !s.setPlayer() {
		return
	}

	s.BroadcastPrintf(q2const.PRINT_HIGH, "%s was kicked\n", s.client.Name)
	// print directly, because the dropped client won't get the
	// SV_BroadcastPrintf message
	s.ClientPrintf(s.client, q2const.PRINT_HIGH, "You were kicked from the game\n")
	s.DropClient(s.client)
	s.client.LastMessage = s.SVS.RealTime // min case there is a funny zombie
}

// status_f. C: server/sv_ccmds.c:707 SV_Status_f
func (s *Server) status_f() {
	if s.SVS.Clients == nil {
		s.Printf("No server running.\n")
		return
	}
	s.Printf("map              : %s\n", s.SV.Name)

	s.Printf("num score ping name            lastmsg address               qport \n")
	s.Printf("--- ----- ---- --------------- ------- --------------------- ------\n")
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_free {
			continue
		}
		s.Printf("%3d ", i)
		s.Printf("%5d ", cl.Edict.Client.PS.Stats[q2const.STAT_FRAGS])

		if cl.State == cs_connected {
			s.Printf("CNCT ")
		} else if cl.State == cs_zombie {
			s.Printf("ZMBI ")
		} else {
			ping := cl.Ping
			if ping >= 9999 {
				ping = 9999
			}
			s.Printf("%4d ", ping)
		}

		s.Printf("%s", cl.Name)
		for j := 0; j < 16-len(cl.Name); j++ {
			s.Printf(" ")
		}

		s.Printf("%7d ", s.SVS.RealTime-cl.LastMessage)

		str := cl.Netchan.RemoteAddress.String()
		s.Printf("%s", str)
		for j := 0; j < 22-len(str); j++ {
			s.Printf(" ")
		}

		s.Printf("%5d", cl.Netchan.Qport)

		s.Printf("\n")
	}
	s.Printf("\n")
}

// conSay_f. C: server/sv_ccmds.c:760 SV_ConSay_f
func (s *Server) conSay_f() {
	if s.Cmd.Argc() < 2 {
		return
	}

	p := s.Cmd.Args()
	if strings.HasPrefix(p, "\"") {
		p = p[1:]
		if len(p) > 0 {
			p = p[:len(p)-1]
		}
	}
	text := "console: " + p

	for j := 0; j < s.maxClients(); j++ {
		client := &s.SVS.Clients[j]
		if client.State != cs_spawned {
			continue
		}
		s.ClientPrintf(client, q2const.PRINT_CHAT, "%s\n", text)
	}
}

// heartbeat_f. C: server/sv_ccmds.c:792 SV_Heartbeat_f
func (s *Server) heartbeat_f() {
	s.SVS.LastHeartbeat = -9999999
}

// serverinfo_f examines the serverinfo string.
// C: server/sv_ccmds.c:805 SV_Serverinfo_f
func (s *Server) serverinfo_f() {
	s.Printf("Server info settings:\n")
	s.infoPrint(s.Cvars.Serverinfo())
}

// dumpUser_f examines all a users info strings.
// C: server/sv_ccmds.c:819 SV_DumpUser_f
func (s *Server) dumpUser_f() {
	if s.Cmd.Argc() != 2 {
		s.Printf("Usage: info <userid>\n")
		return
	}

	if !s.setPlayer() {
		return
	}

	s.Printf("userinfo\n")
	s.Printf("--------\n")
	s.infoPrint(s.client.Userinfo)
}

// serverRecord_f begins server demo recording. Every entity and every
// message will be recorded, but no playerinfo will be stored.
// C: server/sv_ccmds.c:846 SV_ServerRecord_f
func (s *Server) serverRecord_f() {
	if s.Cmd.Argc() != 2 {
		s.Printf("serverrecord <demoname>\n")
		return
	}

	if s.SVS.DemoFile != nil {
		s.Printf("Already recording.\n")
		return
	}

	if s.SV.State != ss_game {
		s.Printf("You must be in a level to record.\n")
		return
	}

	// open the demo file
	name := fmt.Sprintf("demos/%s.dm2", s.Cmd.Argv(1))

	s.Printf("recording to %s.\n", name)
	if s.cfg.DemoCreate == nil {
		s.Printf("ERROR: couldn't open.\n")
		return
	}
	f, err := s.cfg.DemoCreate(s.Cmd.Argv(1))
	if err != nil || f == nil {
		s.Printf("ERROR: couldn't open.\n")
		return
	}
	s.SVS.DemoFile = f

	// setup a buffer to catch all multicasts
	s.SVS.DemoMulticast.SZ_Init(s.SVS.demoMulticastBuf[:])

	// write a single giant fake message with all the startup info
	buf := msg.NewSizeBuf(32768)

	// serverdata needs to go over for all types of servers
	// to make sure the protocol is right, and to set the gamedir
	buf.MSG_WriteByte(q2const.Svc_serverdata)
	buf.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	buf.MSG_WriteLong(int32(s.SVS.SpawnCount))
	// 2 means server demo
	buf.MSG_WriteByte(2) // demos are always attract loops
	buf.MSG_WriteString(s.Cvars.VariableString("gamedir"))
	buf.MSG_WriteShort(-1)
	// send full levelname
	buf.MSG_WriteString(s.SV.ConfigStrings.Get(q2const.CS_NAME))

	for i := 0; i < q2const.MAX_CONFIGSTRINGS; i++ {
		if !s.SV.ConfigStrings.Empty(i) {
			buf.MSG_WriteByte(q2const.Svc_configstring)
			buf.MSG_WriteShort(int32(i))
			buf.MSG_WriteString(s.SV.ConfigStrings.Get(i))
		}
	}

	// write it to the demo file
	s.DPrintf("signon message length: %d\n", buf.CurSize)
	s.writeDemoBlock(buf.Bytes())

	// the rest of the demo file will be individual frames
}

// serverStop_f ends server demo recording.
// C: server/sv_ccmds.c:929 SV_ServerStop_f
func (s *Server) serverStop_f() {
	if s.SVS.DemoFile == nil {
		s.Printf("Not doing a serverrecord.\n")
		return
	}
	_ = s.SVS.DemoFile.Close()
	s.SVS.DemoFile = nil
	s.Printf("Recording completed.\n")
}

// killServer_f kicks everyone off, possibly in preparation for a new game.
// C: server/sv_ccmds.c:949 SV_KillServer_f
func (s *Server) killServer_f() {
	if !s.SVS.Initialized {
		return
	}
	s.svShutdown("Server was killed.\n", false)
	// NET_Config (false): the host closes the transports of a dead instance
}

// serverCommand_f lets the game dll handle a command.
// C: server/sv_ccmds.c:963 SV_ServerCommand_f
func (s *Server) serverCommand_f() {
	if s.ge == nil {
		s.Printf("No game loaded.\n")
		return
	}
	s.ge.ServerCommand()
}

// initOperatorCommands. C: server/sv_ccmds.c:980 SV_InitOperatorCommands
func (s *Server) initOperatorCommands() {
	s.Cmd.AddCommand("heartbeat", s.heartbeat_f)
	s.Cmd.AddCommand("kick", s.kick_f)
	s.Cmd.AddCommand("status", s.status_f)
	s.Cmd.AddCommand("serverinfo", s.serverinfo_f)
	s.Cmd.AddCommand("dumpuser", s.dumpUser_f)

	s.Cmd.AddCommand("map", s.map_f)
	s.Cmd.AddCommand("demomap", s.demoMap_f)
	s.Cmd.AddCommand("gamemap", s.gameMap_f)
	s.Cmd.AddCommand("setmaster", s.setMaster_f)

	if s.dedicated.Value != 0 {
		s.Cmd.AddCommand("say", s.conSay_f)
	}

	s.Cmd.AddCommand("serverrecord", s.serverRecord_f)
	s.Cmd.AddCommand("serverstop", s.serverStop_f)

	s.Cmd.AddCommand("save", s.savegame_f)
	s.Cmd.AddCommand("load", s.loadgame_f)

	s.Cmd.AddCommand("killserver", s.killServer_f)

	s.Cmd.AddCommand("sv", s.serverCommand_f)
}
