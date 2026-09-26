package game

// Port of game/g_svcmds.c: "sv" server console commands and the IP packet
// filter. The filter list lives in memory (Game.ipfilters); writeip does not
// write listip.cfg (no file system access from the game), it only prints the
// same messages as C.

import (
	"fmt"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_svcmds.c:24 Svcmd_Test_f
func (g *Game) Svcmd_Test_f() {
	g.gi.Cprintf(nil, PRINT_HIGH, "Svcmd_Test_f()\n")
}

/*
==============================================================================

PACKET FILTERING


You can add or remove addresses from the filter list with:

addip <ip>
removeip <ip>

The ip address is specified in dot format, and any unspecified digits will match any value, so you can specify an entire class C network with "addip 192.246.40".

Removeip will only remove an address specified exactly the same way.  You cannot addip a subnet, then removeip a single host.

listip
Prints the current list of filters.

writeip
Dumps "addip <ip>" commands to listip.cfg so it can be execed at a later date.  The filter lists are not saved and restored by default, because I beleive it would cause too much confusion.

filterban <0 or 1>

If 1 (the default), then ip addresses matching the current list will be prohibited from entering the game.  This is the default setting.

If 0, then only addresses matching the list will be allowed.  This lets you easily set up a private game, or a game that only allows players from your local network.


==============================================================================
*/

// ipfilter_t: mask and compare are the 4 address bytes read as a
// little-endian unsigned (C: *(unsigned *)b).
// C: game/g_svcmds.c:60 ipfilter_t
type ipfilter_t struct {
	Mask    uint32
	Compare uint32
}

// C: game/g_svcmds.c:66 MAX_IPFILTERS
const MAX_IPFILTERS = 1024

func svcmdsBytesToU32(b [4]byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// C: game/g_svcmds.c:76 StringToFilter
func (g *Game) StringToFilter(s string, f *ipfilter_t) bool {
	var b, m [4]byte
	pos := 0
	at := func(k int) byte {
		if k < len(s) {
			return s[k]
		}
		return 0
	}

	for i := 0; i < 4; i++ {
		if at(pos) < '0' || at(pos) > '9' {
			g.cprintf(nil, PRINT_HIGH, "Bad filter address: %s\n", s[pos:])
			return false
		}

		j := pos
		for at(pos) >= '0' && at(pos) <= '9' {
			pos++
		}
		b[i] = byte(shared.Atoi(s[j:pos]))
		if b[i] != 0 {
			m[i] = 255
		}

		if at(pos) == 0 {
			break
		}
		pos++
	}

	f.Mask = svcmdsBytesToU32(m)
	f.Compare = svcmdsBytesToU32(b)

	return true
}

// C: game/g_svcmds.c:123 SV_FilterPacket
func (g *Game) SV_FilterPacket(from string) bool {
	var m [4]byte // uninitialized in C for the bytes not parsed; zero here
	i := 0
	p := 0
	at := func(k int) byte {
		if k < len(from) {
			return from[k]
		}
		return 0
	}
	for at(p) != 0 && i < 4 {
		m[i] = 0
		for at(p) >= '0' && at(p) <= '9' {
			m[i] = m[i]*10 + (at(p) - '0')
			p++
		}
		if at(p) == 0 || at(p) == ':' {
			break
		}
		i++
		p++
	}

	in := svcmdsBytesToU32(m)

	for i := int32(0); i < g.numipfilters; i++ {
		if in&g.ipfilters[i].Mask == g.ipfilters[i].Compare {
			return int32(g.filterban.Value) != 0
		}
	}

	return g.filterban.Value == 0
}

// C: game/g_svcmds.c:158 SVCmd_AddIP_f
func (g *Game) SVCmd_AddIP_f() {
	var i int32

	if g.gi.Argc() < 3 {
		g.gi.Cprintf(nil, PRINT_HIGH, "Usage:  addip <ip-mask>\n")
		return
	}

	for i = 0; i < g.numipfilters; i++ {
		if g.ipfilters[i].Compare == 0xffffffff {
			break // free spot
		}
	}
	if i == g.numipfilters {
		if g.numipfilters == MAX_IPFILTERS {
			g.gi.Cprintf(nil, PRINT_HIGH, "IP filter list is full\n")
			return
		}
		g.numipfilters++
		for int32(len(g.ipfilters)) < g.numipfilters {
			g.ipfilters = append(g.ipfilters, ipfilter_t{})
		}
	}

	if !g.StringToFilter(g.gi.Argv(2), &g.ipfilters[i]) {
		g.ipfilters[i].Compare = 0xffffffff
	}
}

// C: game/g_svcmds.c:189 SVCmd_RemoveIP_f
func (g *Game) SVCmd_RemoveIP_f() {
	var f ipfilter_t

	if g.gi.Argc() < 3 {
		g.gi.Cprintf(nil, PRINT_HIGH, "Usage:  sv removeip <ip-mask>\n")
		return
	}

	if !g.StringToFilter(g.gi.Argv(2), &f) {
		return
	}

	for i := int32(0); i < g.numipfilters; i++ {
		if g.ipfilters[i].Mask == f.Mask && g.ipfilters[i].Compare == f.Compare {
			for j := i + 1; j < g.numipfilters; j++ {
				g.ipfilters[j-1] = g.ipfilters[j]
			}
			g.numipfilters--
			g.gi.Cprintf(nil, PRINT_HIGH, "Removed.\n")
			return
		}
	}
	g.cprintf(nil, PRINT_HIGH, "Didn't find %s.\n", g.gi.Argv(2))
}

// C: game/g_svcmds.c:220 SVCmd_ListIP_f
func (g *Game) SVCmd_ListIP_f() {
	g.gi.Cprintf(nil, PRINT_HIGH, "Filter list:\n")
	for i := int32(0); i < g.numipfilters; i++ {
		c := g.ipfilters[i].Compare
		g.cprintf(nil, PRINT_HIGH, "%3d.%3d.%3d.%3d\n", byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
	}
}

// SVCmd_WriteIP_f prints the messages of C but keeps the filter list in
// memory: the text C would write to listip.cfg is built by svcmdsListIPText
// and not written anywhere.
// C: game/g_svcmds.c:238 SVCmd_WriteIP_f
func (g *Game) SVCmd_WriteIP_f() {
	var name string

	game := g.gi.Cvar("game", "", 0)

	if game.String == "" {
		name = fmt.Sprintf("%s/listip.cfg", GAMEVERSION)
	} else {
		name = fmt.Sprintf("%s/listip.cfg", game.String)
	}

	g.cprintf(nil, PRINT_HIGH, "Writing %s.\n", name)

	_ = g.svcmdsListIPText()
}

// svcmdsListIPText returns the contents C writes to listip.cfg.
func (g *Game) svcmdsListIPText() string {
	s := fmt.Sprintf("set filterban %d\n", int32(g.filterban.Value))
	for i := int32(0); i < g.numipfilters; i++ {
		c := g.ipfilters[i].Compare
		s += fmt.Sprintf("sv addip %d.%d.%d.%d\n", byte(c), byte(c>>8), byte(c>>16), byte(c>>24))
	}
	return s
}

// ServerCommand will be called when an "sv" command is issued.
// The game can issue gi.argc() / gi.argv() commands to get the rest
// of the parameters
// C: game/g_svcmds.c:282 ServerCommand
func (g *Game) ServerCommand() {
	cmd := g.gi.Argv(1)
	if shared.Q_stricmp(cmd, "test") == 0 {
		g.Svcmd_Test_f()
	} else if g.ctfmod {
		// C: ctf/g_svcmds.c:40 (older base: no IP filter commands)
		g.cprintf(nil, PRINT_HIGH, "Unknown server command \"%s\"\n", cmd)
	} else if shared.Q_stricmp(cmd, "addip") == 0 {
		g.SVCmd_AddIP_f()
	} else if shared.Q_stricmp(cmd, "removeip") == 0 {
		g.SVCmd_RemoveIP_f()
	} else if shared.Q_stricmp(cmd, "listip") == 0 {
		g.SVCmd_ListIP_f()
	} else if shared.Q_stricmp(cmd, "writeip") == 0 {
		g.SVCmd_WriteIP_f()
	} else {
		g.cprintf(nil, PRINT_HIGH, "Unknown server command \"%s\"\n", cmd)
	}
}
