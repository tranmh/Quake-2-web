package game

// Port of ctf/p_menu.c and ctf/p_menu.h: the in-game layout menus of the
// ctf module (join/credits/admin menus).
//
// Menus are not saved (the ctf fork's savegames do not handle them either);
// SelectFunc is a plain Go function value, not a registry handle.

import (
	"fmt"
)

// C: ctf/p_menu.h:21
const (
	PMENU_ALIGN_LEFT   = 0
	PMENU_ALIGN_CENTER = 1
	PMENU_ALIGN_RIGHT  = 2
)

// SelectFunc_t is C SelectFunc_t: void (*)(edict_t *ent, pmenuhnd_t *hnd).
type SelectFunc_t func(g *Game, ent *Edict, hnd *PMenuHnd)

// PMenu is C pmenu_t. Text "" stands for NULL.
// C: ctf/p_menu.h:36 pmenu_t
type PMenu struct {
	Text       string
	Align      int32
	SelectFunc SelectFunc_t
}

// PMenuHnd is C pmenuhnd_t.
// C: ctf/p_menu.h:27 pmenuhnd_t
type PMenuHnd struct {
	Entries []PMenu
	Cur     int32
	Num     int32
	Arg     any
}

// PMenu_Open: note that the pmenu entries are duplicated, this is so that a
// static set of pmenu entries can be used for multiple clients and changed
// without interference.
// C: ctf/p_menu.c:26 PMenu_Open
func (g *Game) PMenu_Open(ent *Edict, entries []PMenu, cur, num int32, arg any) *PMenuHnd {
	if ent.Client == nil {
		return nil
	}

	if ent.Client.Menu != nil {
		g.gi.Dprintf("warning, ent already has a menu\n")
		g.PMenu_Close(ent)
	}

	hnd := &PMenuHnd{}

	hnd.Arg = arg
	hnd.Entries = make([]PMenu, num)
	copy(hnd.Entries, entries[:num])

	hnd.Num = num

	var i int32
	if cur < 0 || entries[cur].SelectFunc == nil {
		for i = 0; i < num; i++ {
			if entries[i].SelectFunc != nil {
				break
			}
		}
	} else {
		i = cur
	}

	if i >= num {
		hnd.Cur = -1
	} else {
		hnd.Cur = i
	}

	ent.Client.Showscores = true
	ent.Client.Inmenu = true
	ent.Client.Menu = hnd

	g.PMenu_Do_Update(ent)
	g.gi.Unicast(ent, true)

	return hnd
}

// C: ctf/p_menu.c:73 PMenu_Close
func (g *Game) PMenu_Close(ent *Edict) {
	if ent.Client.Menu == nil {
		return
	}

	ent.Client.Menu = nil
	ent.Client.Showscores = false
}

// PMenu_UpdateEntry: only use on pmenu's that have been called with PMenu_Open.
// C: ctf/p_menu.c:94 PMenu_UpdateEntry
func PMenu_UpdateEntry(entry *PMenu, text string, align int32, SelectFunc SelectFunc_t) {
	entry.Text = text
	entry.Align = align
	entry.SelectFunc = SelectFunc
}

// C: ctf/p_menu.c:103 PMenu_Do_Update
func (g *Game) PMenu_Do_Update(ent *Edict) {
	var x int32
	alt := false

	if ent.Client.Menu == nil {
		g.gi.Dprintf("warning:  ent has no menu\n")
		return
	}

	hnd := ent.Client.Menu

	str := "xv 32 yv 8 picn inventory "

	for i := int32(0); i < hnd.Num; i++ {
		p := &hnd.Entries[i]
		if p.Text == "" {
			continue // blank line
		}
		t := p.Text
		if t[0] == '*' {
			alt = true
			t = t[1:]
		}
		str += fmt.Sprintf("yv %d ", 32+i*8)
		if p.Align == PMENU_ALIGN_CENTER {
			x = 196/2 - int32(len(t))*4 + 64
		} else if p.Align == PMENU_ALIGN_RIGHT {
			x = 64 + (196 - int32(len(t))*8)
		} else {
			x = 64
		}

		if hnd.Cur == i {
			str += fmt.Sprintf("xv %d ", x-8)
		} else {
			str += fmt.Sprintf("xv %d ", x)
		}

		if hnd.Cur == i {
			str += fmt.Sprintf("string2 \"\x0d%s\" ", t)
		} else if alt {
			str += fmt.Sprintf("string2 \"%s\" ", t)
		} else {
			str += fmt.Sprintf("string \"%s\" ", t)
		}
		alt = false
	}

	g.gi.WriteByteC(svc_layout)
	g.gi.WriteString(str)
}

// C: ctf/p_menu.c:152 PMenu_Update
func (g *Game) PMenu_Update(ent *Edict) {
	if ent.Client.Menu == nil {
		g.gi.Dprintf("warning:  ent has no menu\n")
		return
	}

	if float64(g.level.Time-ent.Client.Menutime) >= 1.0 {
		// been a second or more since last update, update now
		g.PMenu_Do_Update(ent)
		g.gi.Unicast(ent, true)
		ent.Client.Menutime = g.level.Time
		ent.Client.Menudirty = false
	}
	ent.Client.Menutime = float32(float64(g.level.Time) + 0.2)
	ent.Client.Menudirty = true
}

// C: ctf/p_menu.c:170 PMenu_Next
func (g *Game) PMenu_Next(ent *Edict) {
	if ent.Client.Menu == nil {
		g.gi.Dprintf("warning:  ent has no menu\n")
		return
	}

	hnd := ent.Client.Menu

	if hnd.Cur < 0 {
		return // no selectable entries
	}

	i := hnd.Cur
	for {
		i++
		if i == hnd.Num {
			i = 0
		}
		if hnd.Entries[i].SelectFunc != nil {
			break
		}
		if i == hnd.Cur {
			break
		}
	}

	hnd.Cur = i

	g.PMenu_Update(ent)
}

// C: ctf/p_menu.c:199 PMenu_Prev
func (g *Game) PMenu_Prev(ent *Edict) {
	if ent.Client.Menu == nil {
		g.gi.Dprintf("warning:  ent has no menu\n")
		return
	}

	hnd := ent.Client.Menu

	if hnd.Cur < 0 {
		return // no selectable entries
	}

	i := hnd.Cur
	for {
		if i == 0 {
			i = hnd.Num - 1
		} else {
			i--
		}
		if hnd.Entries[i].SelectFunc != nil {
			break
		}
		if i == hnd.Cur {
			break
		}
	}

	hnd.Cur = i

	g.PMenu_Update(ent)
}

// C: ctf/p_menu.c:230 PMenu_Select
func (g *Game) PMenu_Select(ent *Edict) {
	if ent.Client.Menu == nil {
		g.gi.Dprintf("warning:  ent has no menu\n")
		return
	}

	hnd := ent.Client.Menu

	if hnd.Cur < 0 {
		return // no selectable entries
	}

	p := &hnd.Entries[hnd.Cur]

	if p.SelectFunc != nil {
		p.SelectFunc(g, ent, hnd)
	}
}
