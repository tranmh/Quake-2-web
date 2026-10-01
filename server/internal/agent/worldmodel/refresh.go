package worldmodel

import (
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
)

// RefreshInterval is the least time between two refresh requests (an
// "inven"+"putaway" or "help"+"putaway" pair).
const RefreshInterval = 2000

// refresher keeps the inventory and the help computer and decides when the
// control layer should ask the server for them again. Asking costs a layout
// flash on the player's screen, so it happens at most once per
// RefreshInterval and never in combat.
type refresher struct {
	lastAsk        int64 // last request of either kind (0: never)
	asked          bool
	invAskedAt     int64
	helpAskedAt    int64
	invSeq         uint64
	inventoryDirty bool // a pickup or armor loss since the last inventory
	helpBlinkSince int64
	helpKnownAt    int64
}

func (r *refresher) update(w *World, pc *perception.Percept) {
	inv := &w.b.Inventory
	if pc.InventorySeq != r.invSeq {
		r.invSeq = pc.InventorySeq
		if pc.InventorySeq != 0 {
			inv.Known, inv.Seq, inv.At, inv.Stale = true, pc.InventorySeq, w.now, false
			inv.Items = inv.Items[:0]
			for i := 1; i < q2const.MAX_ITEMS; i++ {
				if n := pc.Inventory[i]; n != 0 {
					inv.Items = append(inv.Items, InvItem{Index: i, Name: pc.Classifier().ItemName(i), Count: int(n)})
				}
			}
			r.inventoryDirty = false
		}
	}
	if r.inventoryDirty && inv.Known {
		inv.Stale = true
	}
	for _, l := range pc.Layouts {
		if h, ok := perception.ParseHelp(l); ok {
			w.b.Help, w.b.HelpKnown, w.b.HelpAt = h, true, w.now
			r.helpKnownAt = w.now
		}
	}
	if w.b.Self.HelpBlink {
		if r.helpBlinkSince == 0 {
			r.helpBlinkSince = w.now
		}
	} else {
		r.helpBlinkSince = 0
	}
}

func (w *World) mayAsk() bool {
	r := &w.refresh
	s := &w.b.Self
	if s.InCombat || s.Dead || s.Intermission || w.pc == nil {
		return false
	}
	return !r.asked || w.now-r.lastAsk >= RefreshInterval
}

// WantsInventoryRefresh reports whether the control layer should send
// "inven" and "putaway" now: the inventory was never received on this
// level, or something was picked up (or armor absorbed damage) since, and
// the rate limit and combat allow it.
func (w *World) WantsInventoryRefresh() bool {
	if !w.mayAsk() {
		return false
	}
	inv := &w.b.Inventory
	return !inv.Known || inv.Stale
}

// WantsHelpRefresh reports whether the control layer should send "help"
// and "putaway" now: the help computer was never read on this level, or
// its icon blinks (new objectives) and it was not read since. The
// inventory goes first when both are wanted.
func (w *World) WantsHelpRefresh() bool {
	if !w.mayAsk() || w.WantsInventoryRefresh() {
		return false
	}
	r := &w.refresh
	if !w.b.HelpKnown {
		return !r.asked || r.helpAskedAt == 0 || w.now-r.helpAskedAt >= 5*RefreshInterval
	}
	return r.helpBlinkSince != 0 && r.helpKnownAt < r.helpBlinkSince && w.now-r.helpAskedAt >= 5*RefreshInterval
}

// NoteInventoryRequested records that "inven"+"putaway" were sent.
func (w *World) NoteInventoryRequested() {
	r := &w.refresh
	r.asked, r.lastAsk, r.invAskedAt = true, w.now, w.now
	// do not ask again for the same reason before the answer can arrive
	r.inventoryDirty = false
	w.b.Inventory.Stale = false
}

// NoteHelpRequested records that "help"+"putaway" were sent.
func (w *World) NoteHelpRequested() {
	r := &w.refresh
	r.asked, r.lastAsk, r.helpAskedAt = true, w.now, w.now
}
