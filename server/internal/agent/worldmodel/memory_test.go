package worldmodel

import (
	"fmt"
	"reflect"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// helpLayout is HelpComputer's layout (C: game/p_hud.c:301).
func helpLayout(k, km int) string {
	return fmt.Sprintf("xv 32 yv 8 picn help xv 202 yv 12 string2 \"medium\" xv 0 yv 24 cstring2 \"Outer Base\" "+
		"xv 0 yv 54 cstring2 \"Find the exit\" xv 0 yv 110 cstring2 \"\" "+
		"xv 50 yv 164 string2 \" kills     goals    secrets\" xv 50 yv 172 string2 \"%3d/%3d     %d/%d       %d/%d\" ",
		k, km, 0, 1, 0, 2)
}

func TestLevelMemoryRestore(t *testing.T) {
	s := newSim(t)
	key := LevelKey{Map: "floor"}
	s.ents = []shared.EntityState{soldierAt(20, Vec3{200, 0, 24}), {Number: 40, ModelIndex: mStim, Origin: Vec3{100, 30, 16}}}
	s.step()
	s.w.MarkBlocked("n12-n13")
	s.w.MarkBlocked("n2-n3")
	s.w.MarkBlocked("n12-n13")
	s.ps.Stats[q2const.STAT_HEALTH] = 0
	s.ps.PMove.PmType = q2const.PM_DEAD
	s.step()
	b := s.w.Belief()
	if !b.Self.Dead || !reflect.DeepEqual(b.Memory.DeathSpots, []Vec3{{0, 0, 24}}) ||
		!reflect.DeepEqual(b.Memory.Blocked, []string{"n12-n13", "n2-n3"}) || b.Memory.Entries != 1 {
		t.Fatalf("memory %+v dead %v", b.Memory, b.Self.Dead)
	}

	// reload: the level comes back as it was at entry; the static learnings stay
	s.w.Reset(Level{Key: key, CM: floorCM(t)})
	s.level = &perception.LevelStatic{Gen: 2, MapName: "floor"}
	s.ps.Stats[q2const.STAT_HEALTH] = 100
	s.ps.PMove.PmType = q2const.PM_NORMAL
	s.ents = nil
	s.ps.ViewAngles[q2const.YAW] = 180 // looking away from everything
	b = s.step()
	if len(b.Tracks) != 0 || len(b.Damage) != 0 || b.Memory.Entries != 2 || len(b.Memory.DeathSpots) != 1 ||
		len(b.Memory.Blocked) != 2 {
		t.Fatalf("after reload: tracks %v memory %+v", b.Tracks, b.Memory)
	}
	if len(b.Items) != 1 || !b.Items[0].Remembered || b.Items[0].Class != "item_health_small" ||
		b.Items[0].Pos != (Vec3{100, 30, 16}) || b.Items[0].Life != LifeAlive || b.Items[0].Visible {
		t.Fatalf("remembered items %+v", b.Items)
	}
	// seeing it again binds the remembered item, not a new one
	s.ps.ViewAngles[q2const.YAW] = 0
	s.ents = []shared.EntityState{{Number: 40, ModelIndex: mStim, Origin: Vec3{100, 30, 16}}}
	b = s.step()
	if len(b.Items) != 1 || b.Items[0].Remembered || !b.Items[0].Visible {
		t.Fatalf("re-seen item %+v", b.Items)
	}

	// another level starts empty; coming back restores its memory
	s.w.Reset(Level{Key: LevelKey{Map: "other"}, CM: floorCM(t)})
	s.level = &perception.LevelStatic{Gen: 3, MapName: "other"}
	s.step()
	if m := s.w.Memory(); m.Entries != 1 || len(m.DeathSpots) != 0 || len(m.Items) != 1 {
		t.Fatalf("other level memory %+v", m)
	}
	s.w.Reset(Level{Key: key, CM: floorCM(t)})
	s.w.Reset(Level{Key: key, CM: floorCM(t)}) // a repeated Reset is one entry
	s.level = &perception.LevelStatic{Gen: 4, MapName: "floor"}
	s.ents = nil
	s.step()
	if m := s.w.Memory(); m.Entries != 3 || len(m.DeathSpots) != 1 || len(m.Items) != 1 {
		t.Fatalf("back: %+v", m)
	}
	// a level generation without a Reset (a reload the caller did not
	// announce) is an entry of the same level too
	s.level = &perception.LevelStatic{Gen: 5, MapName: "floor"}
	s.step()
	if m := s.w.Memory(); m.Entries != 4 || s.w.Level().Key != key {
		t.Fatalf("auto entry: %+v %+v", m, s.w.Level().Key)
	}
	// the second visit of the same map is a different key
	if s.w.MemoryFor(LevelKey{Map: "floor", Visit: 1}) != nil {
		t.Fatal("visit 1 shares visit 0's memory")
	}
}

// TestRefreshFirstInventoryInCombat: the level's first inventory is asked
// for even in combat (the bot does not know its weapons without it); the
// help, and a stale inventory, wait for the end of the combat.
func TestRefreshFirstInventoryInCombat(t *testing.T) {
	s := newSim(t)
	s.step()
	w := s.w
	s.ps.Stats[q2const.STAT_HEALTH] = 90
	s.ps.Stats[q2const.STAT_FLASHES] = 1
	s.step()
	if !w.Belief().Self.InCombat || w.Belief().Inventory.Known {
		t.Fatalf("not in combat (%v) or inventory known", w.Belief().Self.InCombat)
	}
	if !w.WantsInventoryRefresh() || w.WantsHelpRefresh() {
		t.Fatal("the first inventory waits for the end of combat, or the help goes out in it")
	}
	w.NoteInventoryRequested()
	s.inv[2], s.inv[3] = 1, 20
	s.invSeq++
	s.step()
	s.ps.Stats[q2const.STAT_HEALTH] = 80
	s.ps.Stats[q2const.STAT_PICKUP_STRING] = int16(q2const.CS_ITEMS + 3)
	for i := 0; i < 25; i++ {
		s.step()
	}
	if b := w.Belief(); !b.Self.InCombat || !b.Inventory.Known || w.WantsInventoryRefresh() || w.WantsHelpRefresh() {
		t.Fatalf("in combat with a known inventory: inventory %v, help %v", w.WantsInventoryRefresh(), w.WantsHelpRefresh())
	}
}

func TestRefreshSignals(t *testing.T) {
	s := newSim(t)
	s.step()
	w := s.w
	if !w.WantsInventoryRefresh() || w.WantsHelpRefresh() {
		t.Fatal("first: inventory wanted, help after it")
	}
	w.NoteInventoryRequested()
	s.step()
	if w.WantsInventoryRefresh() || w.WantsHelpRefresh() {
		t.Fatal("asked within 2 s")
	}
	// the inventory arrives
	s.inv[2], s.inv[3] = 1, 20
	s.invSeq++
	for i := 0; i < 20; i++ {
		s.step()
	}
	b := w.Belief()
	if !b.Inventory.Known || b.Inventory.Stale || b.Inventory.Count("Shells") != 20 || b.Inventory.Count("Blaster") != 1 ||
		len(b.Inventory.Items) != 2 || b.Inventory.At != 300 {
		t.Fatalf("inventory %+v", b.Inventory)
	}
	if w.WantsInventoryRefresh() || !w.WantsHelpRefresh() {
		t.Fatalf("help wanted once the inventory is known")
	}
	w.NoteHelpRequested()
	s.ev.Layouts = []string{helpLayout(3, 12)}
	s.step()
	if b := w.Belief(); !b.HelpKnown || b.Help.Kills != 3 || b.Help.KillsMax != 12 || b.Help.LevelName != "Outer Base" {
		t.Fatalf("help %+v", b.Help)
	}
	if w.WantsHelpRefresh() {
		t.Fatal("help read")
	}
	// a pickup makes the inventory stale; combat holds the request back
	s.ps.Stats[q2const.STAT_PICKUP_STRING] = int16(q2const.CS_ITEMS + 3)
	s.ps.Stats[q2const.STAT_HEALTH] = 90
	s.ps.Stats[q2const.STAT_FLASHES] = 1
	s.step()
	if !w.Belief().Inventory.Stale || !w.Belief().Self.InCombat || w.WantsInventoryRefresh() {
		t.Fatal("stale inventory must wait for the end of combat")
	}
	for i := 0; i < 31; i++ {
		s.step()
	}
	if w.Belief().Self.InCombat || !w.WantsInventoryRefresh() {
		t.Fatal("after combat the inventory is wanted")
	}
	w.NoteInventoryRequested()
	// the help icon blinks: new objectives
	s.ps.Stats[q2const.STAT_HELPICON] = 1 // CS_IMAGES+1 "i_help"
	for i := 0; i < 120; i++ {
		s.step()
	}
	if !w.Belief().Self.HelpBlink || !w.WantsHelpRefresh() {
		t.Fatal("blinking help icon wants the help computer")
	}
	// dead or in an intermission: nothing is asked
	s.ps.PMove.PmType = q2const.PM_FREEZE
	s.ps.Stats[q2const.STAT_LAYOUTS] = 1
	s.step()
	if !w.Belief().Self.Intermission || w.WantsHelpRefresh() || w.WantsInventoryRefresh() {
		t.Fatal("intermission")
	}
}

// TestInventoryNotCarriedAcrossReset: the client keeps its last
// svc_inventory across a reload or level change, but the level-entry save
// restores another one; only an inventory parsed after the entry counts.
func TestInventoryNotCarriedAcrossReset(t *testing.T) {
	s := newSim(t)
	s.inv[3], s.invSeq = 50, 4 // an inventory from before the level: not this level's
	if b := s.step(); b.Inventory.Known {
		t.Fatalf("an inventory parsed before the level entry: %+v", b.Inventory)
	}
	s.invSeq++ // the answer to this level's "inven"
	b := s.step()
	if !b.Inventory.Known || b.Inventory.Count("Shells") != 50 {
		t.Fatalf("before: %+v", b.Inventory)
	}
	s.w.Reset(Level{Key: LevelKey{Map: "floor"}, CM: floorCM(t)})
	s.level = &perception.LevelStatic{Gen: 2, MapName: "floor"}
	for i := 0; i < 3; i++ {
		b = s.step() // the client still holds the old message
	}
	if b.Inventory.Known || b.Inventory.Count("Shells") != 0 || !s.w.WantsInventoryRefresh() {
		t.Fatalf("after the reload: %+v wants %v", b.Inventory, s.w.WantsInventoryRefresh())
	}
	s.w.NoteInventoryRequested()
	s.inv[3], s.invSeq = 20, 6
	if b = s.step(); !b.Inventory.Known || b.Inventory.Count("Shells") != 20 || s.w.WantsInventoryRefresh() {
		t.Fatalf("the new inventory: %+v", b.Inventory)
	}
	// an unannounced reload (a new generation without Reset) forgets it too
	s.level = &perception.LevelStatic{Gen: 3, MapName: "floor"}
	if b = s.step(); b.Inventory.Known {
		t.Fatalf("after an unannounced reload: %+v", b.Inventory)
	}
}

// TestRememberedItemGone: an item remembered from an earlier attempt whose
// spot is in view and empty is gone for this attempt (two frames, so the
// first frame of a level does not decide it), but stays in the memory for
// the next reload.
func TestRememberedItemGone(t *testing.T) {
	s := newSim(t)
	key := LevelKey{Map: "floor"}
	s.ents = []shared.EntityState{{Number: 40, ModelIndex: mStim, Origin: Vec3{100, 30, 16}}}
	s.step()
	reload := func(gen int) {
		s.w.Reset(Level{Key: key, CM: floorCM(t)})
		s.level = &perception.LevelStatic{Gen: gen, MapName: "floor"}
	}
	reload(2)
	s.ents = nil // not there in this attempt
	if b := s.step(); len(b.Items) != 1 || b.Items[0].Life != LifeAlive || !b.Items[0].Remembered {
		t.Fatalf("first frame: %+v", b.Items)
	}
	b := s.step()
	if len(b.Items) != 1 || b.Items[0].Life != LifeGone || b.Items[0].LifeAt != s.now {
		t.Fatalf("an empty spot in view: %+v", b.Items)
	}
	if len(s.w.Memory().Items) != 1 {
		t.Fatalf("memory lost the item: %+v", s.w.Memory().Items)
	}
	// it shows up at its spot after all: present again, same track
	s.ents = []shared.EntityState{{Number: 41, ModelIndex: mStim, Origin: Vec3{100, 30, 16}}}
	if b := s.step(); len(b.Items) != 1 || b.Items[0].Life != LifeAlive || b.Items[0].Num != 41 || !b.Items[0].Visible {
		t.Fatalf("seen again: %+v", b.Items)
	}
	// looking away it is not judged
	reload(3)
	s.ents = nil
	s.ps.ViewAngles[q2const.YAW] = 180
	for i := 0; i < 5; i++ {
		s.step()
	}
	if b := s.w.Belief(); len(b.Items) != 1 || b.Items[0].Life != LifeAlive || !b.Items[0].Remembered {
		t.Fatalf("out of view: %+v", b.Items)
	}
}
