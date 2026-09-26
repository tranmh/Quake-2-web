package gametest

// Robustness harness for docs/review/03-go-game.md: drives the real game
// module with hostile player input (client commands with crafted arguments,
// userinfo, usercmds, reconnects) and crafted entity strings, and fails on
// any Go runtime panic. A shared.ComError panic is gi.error / Com_Error,
// which the C game raises too (ERR_DROP), and is reported separately.

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

func needDemoPak(t testing.TB) string {
	t.Helper()
	base := testutil.BaseDir()
	if _, err := os.Stat(filepath.Join(base, "baseq2", "pak0.pak")); err != nil {
		t.Skip("demo pak not available")
	}
	return base
}

// robustModes are the game configurations the harness drives.
var robustModes = map[string]string{
	"sp":   `{"map":"demo1","module":"baseq2","seed":3,"cvars":{"skill":"1","deathmatch":"0","coop":"0","maxclients":"1"},"clients":[{"userinfo":"\\name\\p0\\skin\\male/grunt\\hand\\0\\fov\\90"}]}`,
	"coop": `{"map":"demo1","module":"baseq2","seed":4,"cvars":{"skill":"3","deathmatch":"0","coop":"1","maxclients":"4"},"clients":[{"userinfo":"\\name\\p0\\skin\\male/grunt\\hand\\0\\fov\\90"},{"userinfo":"\\name\\p1\\skin\\female/athena\\hand\\1\\fov\\90"},{"userinfo":"\\name\\p2\\skin\\male/cipher\\hand\\2\\fov\\90"}]}`,
	"dm":   `{"map":"demo1","module":"baseq2","seed":5,"cvars":{"skill":"1","deathmatch":"1","coop":"0","maxclients":"4","cheats":"1","dmflags":"0"},"clients":[{"userinfo":"\\name\\p0\\skin\\male/grunt\\hand\\0\\fov\\90"},{"userinfo":"\\name\\p1\\skin\\female/athena\\hand\\1\\fov\\90\\spectator\\1"},{"userinfo":"\\name\\p2\\skin\\male/cipher\\hand\\2\\fov\\90"}]}`,
	"ctf":  `{"map":"demo1","module":"ctf","seed":6,"cvars":{"skill":"1","deathmatch":"1","coop":"0","maxclients":"4","ctf":"1","cheats":"1","dmflags":"0","competition":"2","admin_password":"pw","electpercentage":"50","warp_list":"demo1 demo2"},"clients":[{"userinfo":"\\name\\r0\\skin\\male/grunt\\hand\\0\\fov\\90"},{"userinfo":"\\name\\b0\\skin\\female/athena\\hand\\0\\fov\\90"},{"userinfo":"\\name\\x0\\skin\\male/cipher\\hand\\0\\fov\\90"}]}`,
}

// newRobustServer starts a scenario (frame 0 done). entstring != "" overrides
// the map's entity string.
func newRobustServer(t testing.TB, js, entstring string) *Server {
	t.Helper()
	base := needDemoPak(t)
	sc, err := ParseScenario([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if entstring != "" {
		sc.EntstringOverride = &entstring
	}
	s, err := NewServer(sc, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	s.EndFrame()
	return s
}

// step runs one server frame with the given usercmds (client -> cmd).
func (s *Server) step(cmds map[int]shared.UserCmd) {
	s.beginPeriod()
	for c := 0; c < len(s.E.clients); c++ {
		if uc, ok := cmds[c]; ok {
			s.inputCmd(c, uc)
		}
	}
	s.framenum++
	s.E.Ge.RunFrame()
	s.EndFrame()
}

// reconnect drops and reconnects client slot i with userinfo (SV_DropClient +
// SVC_DirectConnect + new/begin).
func (s *Server) reconnect(i int, userinfo string) {
	e := s.E
	cl := &e.clients[i]
	if cl.state == cs_spawned {
		e.Ge.ClientDisconnect(cl.edict)
	}
	*cl = client{}
	ent := &e.Ge.Edicts()[i+1]
	cl.edict = ent
	userinfo, _ = shared.Info_SetValueForKey(userinfo, "ip", "loopback")
	ok, newinfo := e.Ge.ClientConnect(ent, userinfo)
	if !ok {
		return
	}
	cl.userinfo = newinfo
	e.Ge.ClientUserinfoChanged(ent, newinfo)
	cl.state = cs_connected
	s.executeUserCommand(i, "new")
	s.executeUserCommand(i, fmt.Sprintf("begin %d", s.spawncount))
}

// connectOnly drops client slot i and connects it again without "begin"
// (cs_connected: the client sends commands before it is spawned).
func (s *Server) connectOnly(i int, userinfo string) {
	e := s.E
	cl := &e.clients[i]
	if cl.state == cs_spawned {
		e.Ge.ClientDisconnect(cl.edict)
	}
	*cl = client{}
	ent := &e.Ge.Edicts()[i+1]
	cl.edict = ent
	userinfo, _ = shared.Info_SetValueForKey(userinfo, "ip", "loopback")
	ok, newinfo := e.Ge.ClientConnect(ent, userinfo)
	if !ok {
		return
	}
	cl.userinfo = newinfo
	e.Ge.ClientUserinfoChanged(ent, newinfo)
	cl.state = cs_connected
	s.executeUserCommand(i, "new")
}

// userinfoChanged is SV_UserinfoChanged for a connected client.
func (s *Server) userinfoChanged(i int, userinfo string) {
	cl := &s.E.clients[i]
	if cl.state < cs_connected {
		return
	}
	if len(userinfo) > MAX_INFO_STRING-1 {
		userinfo = userinfo[:MAX_INFO_STRING-1]
	}
	cl.userinfo = userinfo
	s.E.Ge.ClientUserinfoChanged(cl.edict, userinfo)
}

// knownCCrashes lists the panic sites (file:line func of internalError) that
// are reached only by malformed maps on which the C game dereferences NULL
// too; the game's guard turns them into gi.error (ERR_DROP of the instance).
// See docs/review/03-go-game.md.
var knownCCrashes = map[string]string{
	"g_turret.go (*Game).turret_breach_finish_init": "G-M01: turret_breach without a resolvable target or team (C: g_turret.c:211/215 NULL deref)",
	"g_turret.go (*Game).turret_driver_link":        "G-M01: turret_driver without a resolvable target / team (C: g_turret.c:363/364 NULL deref)",
	"g_misc.go (*Game).func_clock_think":            "G-M01: func_clock whose target has no use function (C: g_misc.c:1709 NULL call)",
	"g_combat.go (*Game).Killed":                    "G-M01: damage kills an entity without die (e.g. monster_commander_body telefragged) (C: g_combat.c:129 NULL call)",
}

// siteOf returns the "file.go:N func" part of an internal error message.
func siteOf(msg string) string {
	i := strings.Index(msg, "internal error: ")
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(msg, " at ")
	if j < 0 || j < i {
		return "?"
	}
	site := msg[j+4:]
	if k := strings.IndexByte(site, '\n'); k >= 0 {
		site = site[:k]
	}
	return site
}

// classify returns "" when msg is acceptable (no panic, a genuine gi.error
// or a known C crash), else the site key of the failure.
func classify(msg string) string {
	if msg == "" {
		return ""
	}
	if strings.Contains(msg, "goroutine ") { // raw Go panic (no guard)
		return "raw panic: " + firstLine(msg[strings.Index(msg, "panic: "):])
	}
	site := siteOf(msg)
	if site == "" {
		return "" // genuine gi.error: ERR_DROP in C too
	}
	if _, ok := knownCCrashes[stripLine(site)]; ok {
		return ""
	}
	return site
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// stripLine turns "g_x.go:12 fn" into "g_x.go fn" so the allowlist survives
// edits that move lines.
func stripLine(site string) string {
	f, fn, ok := strings.Cut(site, " ")
	if !ok {
		return site
	}
	if i := strings.IndexByte(f, ':'); i >= 0 {
		f = f[:i]
	}
	return f + " " + fn
}

// catchPanic runs f and returns a description of a Go runtime panic ("" when
// none). ComError panics (gi.error, also fatal in C) are returned with a
// "comerror:" prefix.
func catchPanic(f func()) (msg string) {
	defer func() {
		if p := recover(); p != nil {
			if ce, ok := p.(shared.ComError); ok {
				msg = "comerror: " + ce.Msg
				return
			}
			msg = fmt.Sprintf("panic: %v\n%s", p, debug.Stack())
		}
	}()
	f()
	return ""
}

var robustCommands = []string{
	"players", "say", "say_team", "steam", "score", "help", "use", "drop", "give", "god", "notarget",
	"noclip", "inven", "invnext", "invprev", "invnextw", "invprevw", "invnextp", "invprevp", "invuse",
	"invdrop", "weapprev", "weapnext", "weaplast", "kill", "putaway", "wave", "playerlist", "team", "id",
	"yes", "no", "ready", "notready", "ghost", "admin", "stats", "warp", "boot", "observer", "hello",
}

var robustArgs = []string{
	"", "all", "health", "weapons", "ammo", "armor", "Power Shield", "Power Screen", "tech", "red", "blue",
	"0", "1", "2", "3", "4", "5", "-1", "-2", "999999999", "-2147483648", "2147483647", "99999999999999",
	"0x7fffffff", "%", "%l %a %h %t %w %n %L", "%%", "\"", "\"quoted\"", "\"\"", strings.Repeat("A", 300),
	"pw", "demo1", "demo2", "Blaster", "Shotgun", "Grenades", "Rocket Launcher", "BFG10K", "Grapple",
	"Quad Damage", "Invulnerability", "Silencer", "Rebreather", "Environment Suit", "Adrenaline",
	"Bandolier", "Ammo Pack", "Data CD", "Blue Key", "Red Key", "Airstrike Marker", "Pyramid Key",
	"Power Cube", "Security Pass", "Commander's Head", "Red Flag", "Blue Flag", "Disruptor Shield",
	"Power Amplifier", "Time Accel", "AutoDoc", "Cells", "Shells", "Bullets", "Rockets", "Slugs",
	"Body Armor", "Combat Armor", "Jacket Armor", "Armor Shard", "Health", "Mega Health", "Ancient Head",
	"item_health", "weapon_railgun", "\xff\xfe", "\\", ";", "\n",
}

var robustUserinfoVals = []string{
	"", "0", "1", "-1", "2", "3", "999999", "-999999", "160", "161", "abc", "male/", "/", "//", "female",
	"male/grunt", "cyborg/../../x", strings.Repeat("n", 100), "%s%s%n", "\"", "\xff",
}

func (s *Server) randomCommand(r *rand.Rand) string {
	cmd := robustCommands[r.Intn(len(robustCommands))]
	n := r.Intn(4)
	var b strings.Builder
	b.WriteString(cmd)
	for i := 0; i < n; i++ {
		b.WriteByte(' ')
		if r.Intn(5) == 0 {
			// an item name taken from the running game's CS_ITEMS
			name := s.E.Configstrings[CS_ITEMS+r.Intn(MAX_ITEMS)]
			if name == "" {
				name = "unknown"
			}
			b.WriteString(name)
			continue
		}
		a := robustArgs[r.Intn(len(robustArgs))]
		if strings.ContainsAny(a, " ") && r.Intn(2) == 0 {
			a = "\"" + a + "\""
		}
		b.WriteString(a)
	}
	return b.String()
}

func randomUserinfo(r *rand.Rand) string {
	keys := []string{"name", "skin", "hand", "fov", "spectator", "gender", "password", "msg", "rate"}
	var b strings.Builder
	for _, k := range keys {
		if r.Intn(3) == 0 {
			continue
		}
		b.WriteString("\\" + k + "\\" + robustUserinfoVals[r.Intn(len(robustUserinfoVals))])
	}
	if r.Intn(10) == 0 {
		b.WriteString("\\") // malformed
	}
	return b.String()
}

func randomUsercmd(r *rand.Rand) shared.UserCmd {
	pick16 := func() int16 {
		switch r.Intn(4) {
		case 0:
			return int16(r.Intn(65536) - 32768)
		case 1:
			return []int16{-32768, 32767, 0, 400, -400}[r.Intn(5)]
		default:
			return int16(r.Intn(801) - 400)
		}
	}
	return shared.UserCmd{
		Msec:        uint8(r.Intn(256)),
		Buttons:     uint8(r.Intn(256)),
		Angles:      [3]int16{pick16(), pick16(), pick16()},
		ForwardMove: pick16(),
		SideMove:    pick16(),
		UpMove:      pick16(),
		Impulse:     uint8(r.Intn(256)),
		LightLevel:  uint8(r.Intn(256)),
	}
}

// monkey runs frames of random hostile input on s. It returns the first
// panic description.
func monkey(s *Server, r *rand.Rand, frames int, log func(string)) string {
	nc := len(s.Sc.Clients)
	for f := 0; f < frames; f++ {
		var inputs []string
		cmds := map[int]shared.UserCmd{}
		for c := 0; c < nc; c++ {
			cmds[c] = randomUsercmd(r)
		}
		if msg := catchPanic(func() {
			for k := r.Intn(4); k > 0; k-- {
				c := r.Intn(nc)
				switch r.Intn(20) {
				case 0:
					ui := randomUserinfo(r)
					inputs = append(inputs, fmt.Sprintf("userinfo %d %q", c, ui))
					s.userinfoChanged(c, ui)
				case 1:
					ui := randomUserinfo(r)
					inputs = append(inputs, fmt.Sprintf("reconnect %d %q", c, ui))
					s.reconnect(c, ui)
				case 2:
					inputs = append(inputs, fmt.Sprintf("disconnect %d", c))
					s.inputCommand(c, "disconnect")
				case 3:
					ui := randomUserinfo(r)
					inputs = append(inputs, fmt.Sprintf("connect-only %d %q", c, ui))
					s.connectOnly(c, ui)
				case 4:
					if s.E.clients[c].state == cs_connected {
						inputs = append(inputs, fmt.Sprintf("begin %d", c))
						s.executeUserCommand(c, fmt.Sprintf("begin %d", s.spawncount))
					}
				default:
					text := s.randomCommand(r)
					inputs = append(inputs, fmt.Sprintf("cmd %d %q", c, text))
					// SV_ExecuteUserCommand: connected and spawned clients
					if s.E.clients[c].state >= cs_connected {
						s.inputCommand(c, text)
					}
				}
			}
			s.step(cmds)
		}); msg != "" {
			return fmt.Sprintf("frame %d inputs %v: %s", f, inputs, msg)
		}
		if log != nil && len(inputs) > 0 {
			log(fmt.Sprintf("frame %d: %v", f, inputs))
		}
	}
	return ""
}

// TestRobustMonkey drives every mode with random hostile input. Env
// Q2_MONKEY_ITERS sets the number of seeds per mode (default 1), Q2_MONKEY_FRAMES
// the frames per seed (default 200).
func TestRobustMonkey(t *testing.T) {
	needDemoPak(t)
	iters, frames := 1, 200
	if v := os.Getenv("Q2_MONKEY_ITERS"); v != "" {
		fmt.Sscan(v, &iters)
	}
	if v := os.Getenv("Q2_MONKEY_FRAMES"); v != "" {
		fmt.Sscan(v, &frames)
	}
	seed0 := int64(1)
	if v := os.Getenv("Q2_MONKEY_SEED"); v != "" {
		fmt.Sscan(v, &seed0)
	}
	for _, mode := range []string{"sp", "coop", "dm", "ctf"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			for it := 0; it < iters; it++ {
				seed := seed0 + int64(it)
				s := newRobustServer(t, robustModes[mode], "")
				r := rand.New(rand.NewSource(seed))
				msg := monkey(s, r, frames, nil)
				if site := classify(msg); site != "" {
					t.Fatalf("seed %d: %s: %s", seed, site, msg)
				}
				if msg != "" {
					t.Logf("seed %d ended by gi.error: %s", seed, msg)
				}
			}
		})
	}
}

// TestRobustConcurrentInstances runs several game instances (every mode,
// random hostile input, save/load) concurrently in one process; under
// `go test -race` any shared mutable package-level state is reported.
func TestRobustConcurrentInstances(t *testing.T) {
	needDemoPak(t)
	frames := 150
	if testing.Short() {
		frames = 40
	}
	modes := []string{"sp", "coop", "dm", "ctf", "sp", "ctf"}
	errs := make(chan string, len(modes))
	for i, mode := range modes {
		go func(i int, mode string) {
			r := rand.New(rand.NewSource(int64(100 + i)))
			var msg string
			if p := catchPanic(func() {
				s := newRobustServer(t, robustModes[mode], "")
				for c := range s.Sc.Clients {
					s.inputCommand(c, "give all")
				}
				msg = monkey(s, r, frames, nil)
				if classify(msg) == "" && msg == "" {
					if _, err := s.E.Ge.WriteGame(false); err != nil {
						msg = err.Error()
					}
					if _, err := s.E.Ge.WriteLevel(); err != nil {
						msg = err.Error()
					}
				}
			}); p != "" {
				msg = p
			}
			if classify(msg) != "" {
				errs <- fmt.Sprintf("%s#%d: %s", mode, i, msg)
				return
			}
			errs <- ""
		}(i, mode)
	}
	for range modes {
		if e := <-errs; e != "" {
			t.Error(e)
		}
	}
}
