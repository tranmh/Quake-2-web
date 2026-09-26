package gametest

// Focused regression tests for the findings of docs/review/03-go-game.md.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime"
	"strings"
	"testing"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// runClientCommands starts mode and runs each "client:command" (client 0 when
// no prefix is given) followed by one idle frame; it fails on a Go panic.
func runClientCommands(t *testing.T, mode string, cmds ...string) *Server {
	t.Helper()
	s := newRobustServer(t, robustModes[mode], "")
	for _, c := range cmds {
		c := c
		if msg := catchPanic(func() {
			s.inputCommand(0, c)
			s.step(nil)
		}); msg != "" {
			t.Fatalf("command %q: %s", c, msg)
		}
	}
	return s
}

// G-01: invprev* with selected_item == -1 and no matching item indexed
// itemlist[-1].
func TestReviewInvPrevNoItem(t *testing.T) {
	for _, mode := range []string{"sp", "ctf"} {
		t.Run(mode, func(t *testing.T) {
			// ctf: "team red" closes the join menu first (the menu takes invnext/invprev)
			pre := []string{}
			if mode == "ctf" {
				pre = append(pre, "team red")
			}
			s := runClientCommands(t, mode, append(pre, "invnextp", "invprevp")...)
			if sel := s.E.Ge.Edicts()[1].Client.Pers.SelectedItem; sel != -1 {
				t.Fatalf("selected_item = %d, want -1 (no powerup)", sel)
			}
		})
	}
}

// tamper decodes a save blob, lets f edit the JSON tree and re-encodes it.
func tamper(t *testing.T, blob []byte, f func(root map[string]any)) []byte {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(zstdDec(t, blob)))
	d.UseNumber()
	var root map[string]any
	if err := d.Decode(&root); err != nil {
		t.Fatal(err)
	}
	f(root)
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return zstdEnc(t, out)
}

// mustRejectCleanly asserts that a tampered save is refused by the loader
// with a validation error (not a recovered Go panic) and without panicking.
func mustRejectCleanly(t *testing.T, a *Server, gd, ld []byte) {
	t.Helper()
	var lerr error
	if msg := catchPanic(func() { _, lerr = loadSave(t, a, gd, ld) }); msg != "" {
		t.Fatalf("load panicked: %s", msg)
	}
	if lerr == nil {
		t.Fatalf("tampered save was accepted")
	}
	if strings.Contains(lerr.Error(), "internal error") {
		t.Fatalf("tampered save caused a Go panic in the loader: %v", lerr)
	}
	t.Logf("rejected: %v", lerr)
}

func clientPers(root map[string]any, i int) map[string]any {
	return root["Clients"].([]any)[i].(map[string]any)["ClientPrivate"].(map[string]any)["Pers"].(map[string]any)
}

// G-03: pers.selected_item from the save indexes inventory/itemlist.
func TestReviewSaveSelectedItem(t *testing.T) {
	a, gd, ld := saveFixture(t, "sp", 5, rand.New(rand.NewSource(1)))
	for _, v := range []string{"256", "1000", "-2", "-100"} {
		bad := tamper(t, gd, func(root map[string]any) { clientPers(root, 0)["SelectedItem"] = json.Number(v) })
		mustRejectCleanly(t, a, bad, ld)
	}
	// -1 is a valid selected_item
	ok := tamper(t, gd, func(root map[string]any) { clientPers(root, 0)["SelectedItem"] = json.Number("-1") })
	b, err := loadSave(t, a, ok, ld)
	if err != nil {
		t.Fatalf("selected_item -1 rejected: %v", err)
	}
	if msg := catchPanic(func() {
		b.inputCommand(0, "invprev")
		b.inputCommand(0, "invnext")
		b.step(nil)
	}); msg != "" {
		t.Fatal(msg)
	}
}

// G-04: game.maxclients / maxentities / num_items from the save size the
// client array and bound loops; a tampered value must not allocate huge
// arrays or index out of range.
func TestReviewSaveGameCounts(t *testing.T) {
	a, gd, ld := saveFixture(t, "coop", 5, rand.New(rand.NewSource(1)))
	cases := map[string]func(root map[string]any){
		"maxclients 1 (cvar 4)": func(root map[string]any) {
			root["Game"].(map[string]any)["Maxclients"] = json.Number("1")
			root["Clients"] = root["Clients"].([]any)[:1]
		},
		"maxclients 0": func(root map[string]any) {
			root["Game"].(map[string]any)["Maxclients"] = json.Number("0")
			root["Clients"] = []any{}
		},
		"maxclients -1": func(root map[string]any) { root["Game"].(map[string]any)["Maxclients"] = json.Number("-1") },
		"maxclients huge": func(root map[string]any) {
			root["Game"].(map[string]any)["Maxclients"] = json.Number("2000000000")
		},
		"maxentities 4096": func(root map[string]any) {
			root["Game"].(map[string]any)["Maxentities"] = json.Number("4096")
		},
		"num_items 1000": func(root map[string]any) { root["Game"].(map[string]any)["NumItems"] = json.Number("1000") },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			mustRejectCleanly(t, a, tamper(t, gd, f), ld)
			runtime.ReadMemStats(&after)
			if d := after.TotalAlloc - before.TotalAlloc; d > 512<<20 {
				t.Fatalf("loader allocated %d MB for a tampered count", d>>20)
			}
		})
	}
}

// G-05: level fields used as array indices (num_edicts, body_que, trail
// head) must be validated.
func TestReviewSaveLevelIndices(t *testing.T) {
	a, gd, ld := saveFixture(t, "coop", 5, rand.New(rand.NewSource(1)))
	cases := map[string]func(root map[string]any){
		"num_edicts > maxentities": func(root map[string]any) { root["NumEdicts"] = json.Number("5000") },
		"num_edicts negative":      func(root map[string]any) { root["NumEdicts"] = json.Number("-5") },
		"body_que 100":             func(root map[string]any) { root["Level"].(map[string]any)["BodyQue"] = json.Number("100") },
		"body_que -1":              func(root map[string]any) { root["Level"].(map[string]any)["BodyQue"] = json.Number("-1") },
		"trail_head 99":            func(root map[string]any) { root["Extras"].(map[string]any)["TrailHead"] = json.Number("99") },
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			mustRejectCleanly(t, a, gd, tamper(t, ld, f))
		})
	}
}

// runEnts spawns demo1 (monsters and other entities stripped) plus extra
// entities in mode and runs frames; it fails on a Go panic or an internal
// error. It returns the gi.error text, if any.
func runEnts(t *testing.T, mode, extra string, frames int) string {
	t.Helper()
	base, _ := demoEntities(t)
	var s *Server
	msg := catchPanic(func() { s = newRobustServer(t, robustModes[mode], base+extra) })
	for f := 0; msg == "" && f < frames; f++ {
		msg = catchPanic(func() { s.step(nil) })
	}
	if site := classify(msg); site != "" {
		t.Fatalf("%s: %s", site, msg)
	}
	return msg
}

// G-06: a target loop (two trigger_relays targeting each other) recursed
// in G_UseTargets until the goroutine stack overflowed, a fatal error that
// kills the whole process (C: stack overflow, SIGSEGV of the one server).
func TestReviewTargetLoop(t *testing.T) {
	msg := runEnts(t, "sp", `{
"classname" "trigger_relay"
"targetname" "loopa"
"target" "loopb"
}
{
"classname" "trigger_relay"
"targetname" "loopb"
"target" "loopa"
}
{
"classname" "trigger_always"
"target" "loopa"
}
`, 5)
	if !strings.Contains(msg, "comerror") {
		t.Fatalf("target loop: want a gi.error, got %q", msg)
	}
	t.Log(msg)
}

// G-06: a func_train on two path_corners at the same spot with wait 0
// recursed train_next -> Move_Calc -> Move_Done -> train_wait -> train_next
// until the stack overflowed.
func TestReviewTrainLoop(t *testing.T) {
	_, start := demoEntities(t)
	o := fmt.Sprintf("%d %d %d", int(start[0]), int(start[1]), int(start[2])+64)
	msg := runEnts(t, "sp", `{
"classname" "func_train"
"model" "*1"
"target" "c1"
}
{
"classname" "path_corner"
"targetname" "c1"
"target" "c2"
"origin" "`+o+`"
}
{
"classname" "path_corner"
"targetname" "c2"
"target" "c1"
"origin" "`+o+`"
}
`, 5)
	if !strings.Contains(msg, "comerror") {
		t.Fatalf("train loop: want a gi.error, got %q", msg)
	}
	t.Log(msg)
}

// G-07: target_character with a negative count indexed message[-2] in
// target_string_use (Go panic; C reads the heap byte before the string).
func TestReviewTargetStringNegativeCount(t *testing.T) {
	msg := runEnts(t, "sp", `{
"classname" "target_string"
"targetname" "ts"
"team" "digits"
"message" "12:34"
}
{
"classname" "target_character"
"model" "*1"
"team" "digits"
"count" "-1"
}
{
"classname" "target_character"
"model" "*2"
"team" "digits"
"count" "3"
}
{
"classname" "trigger_always"
"target" "ts"
}
`, 5)
	if msg != "" {
		t.Fatalf("unexpected error: %s", msg)
	}
}

// G-08: flood_msgs outside 1..11 made CheckFlood index flood_when[] out of
// bounds (C reads the neighbouring gclient_t fields).
func TestReviewFloodMsgsRange(t *testing.T) {
	for _, fm := range []string{"12", "30", "-1", "-5", "0.5"} {
		for _, module := range []string{"baseq2", "ctf"} {
			js := `{"map":"demo1","module":"` + module + `","seed":5,"cvars":{"deathmatch":"1","maxclients":"4","flood_msgs":"` + fm +
				`"},"clients":[{"userinfo":"\\name\\p0\\skin\\male/grunt"},{"userinfo":"\\name\\p1\\skin\\male/grunt"}]}`
			s := newRobustServer(t, js, "")
			for i := 0; i < 25; i++ {
				text := "say hello"
				if i%2 == 1 {
					text = "say_team hi"
				}
				if msg := catchPanic(func() { s.inputCommand(0, text); s.step(nil) }); msg != "" {
					t.Fatalf("flood_msgs %s (%s), message %d: %s", fm, module, i, msg)
				}
			}
		}
	}
}

// G-09: a dead coop player toggling noclip twice has MOVETYPE_WALK; respawn
// copies it to a body-queue edict, and G_RunEntity has no case for
// MOVETYPE_WALK: gi.error("SV_Physics: bad movetype 4") ends the server
// (the same in C). Any coop player could kill the instance.
func TestReviewDeadNoclipBody(t *testing.T) {
	s := newRobustServer(t, robustModes["coop"], "")
	run := func(what string, f func()) {
		t.Helper()
		if msg := catchPanic(f); msg != "" {
			t.Fatalf("%s: %s", what, msg)
		}
	}
	for i := 0; i < 60; i++ { // Cmd_Kill_f needs 5 s since the spawn
		run("idle", func() { s.step(nil) })
	}
	run("kill", func() { s.inputCommand(0, "kill"); s.step(nil) })
	run("noclip", func() { s.inputCommand(0, "noclip"); s.inputCommand(0, "noclip"); s.step(nil) })
	for i := 0; i < 30; i++ {
		run("idle", func() { s.step(nil) })
	}
	attack := map[int]shared.UserCmd{0: {Msec: 100, Buttons: BUTTON_ATTACK}}
	for i := 0; i < 20; i++ {
		run("respawn and play", func() { s.step(attack) })
	}
}

// G-10: C printf "%-16.16s" pads and truncates by bytes; Go's fmt counts
// runes, so a UTF-8 netname shifted the columns of the ctf playerlist/stats.
func TestReviewCTFPlayerListBytePadding(t *testing.T) {
	js := strings.Replace(robustModes["ctf"], `\\name\\r0`, `\\name\\Jürgen`, 1)
	s := newRobustServer(t, js, "")
	s.beginPeriod()
	s.inputCommand(0, "playerlist")
	var text string
	for _, ev := range s.E.Events {
		if ev["t"] == "print" && ev["kind"] == "cprintf" {
			text += ev["text"].(string)
		}
	}
	want := "  1 J\xc3\xbcrgen" + strings.Repeat(" ", 16-len("J\xc3\xbcrgen")) + " 00:00"
	if !strings.HasPrefix(text, want) {
		t.Fatalf("playerlist line = %q, want prefix %q", text, want)
	}
}

// G-11: level.changemap (target_changelevel "map", worldspawn "nextmap",
// map data of uploaded maps) is pasted into the console command
// `gamemap "<map>"\n`; a "\n" escape (ED_NewString) or a quote in the map
// name injected arbitrary console commands (same in C).
func TestReviewChangemapInjection(t *testing.T) {
	base, _ := demoEntities(t)
	extra := `{
"classname" "target_changelevel"
"targetname" "tc"
"map" "demo2\nquit\nset rcon_password x\n"
}
{
"classname" "trigger_always"
"target" "tc"
"delay" "1"
}
`
	s := newRobustServer(t, robustModes["sp"], base+extra)
	var cmds []string
	for f := 0; f < 30; f++ {
		s.beginPeriod()
		if msg := catchPanic(func() { s.step(nil) }); msg != "" {
			t.Fatal(msg)
		}
		for _, ev := range s.E.Events {
			if ev["t"] == "cmd" {
				cmds = append(cmds, ev["text"].(string))
			}
		}
	}
	if len(cmds) == 0 {
		t.Fatal("no gamemap command issued")
	}
	for _, c := range cmds {
		if strings.Count(c, "\n") != 1 || !strings.HasSuffix(c, "\n") || strings.Count(c, "\"") != 2 {
			t.Fatalf("console command injection: %q", c)
		}
	}
	t.Logf("commands: %q", cmds)
}

// G-02: a Go runtime panic inside the game (here the C NULL dereference of
// turret_driver_link on a turret_driver without a target) must reach the
// engine as gi.error (ERR_DROP of this instance), never as a raw panic: the
// server's frame recovery re-panics anything that is not a ComError and the
// whole process (every instance) would die.
func TestReviewGuardConvertsPanics(t *testing.T) {
	base, _ := demoEntities(t)
	extra := `{
"classname" "turret_driver"
"origin" "0 0 0"
}
`
	var s *Server
	msg := catchPanic(func() { s = newRobustServer(t, robustModes["sp"], base+extra) })
	for f := 0; msg == "" && f < 5; f++ {
		msg = catchPanic(func() { s.step(nil) })
	}
	if !strings.HasPrefix(msg, "comerror: ") || !strings.Contains(msg, "internal error") ||
		!strings.Contains(msg, "turret_driver_link") {
		t.Fatalf("want gi.error naming the panic site, got %q", firstLine(msg))
	}
	t.Log(msg)
}

// G-20: a save blob is a zstd frame decompressed without a size limit: a
// few KB can declare/expand to gigabytes (found by FuzzReadSave: 69 KB ->
// 3.8 GB, the fuzz worker was OOM-killed), an out-of-memory kill of the
// whole process that no recover can stop.
func TestReviewSaveDecompressionBomb(t *testing.T) {
	a, _, ld := saveFixture(t, "sp", 5, rand.New(rand.NewSource(1)))
	bomb := zstdEnc(t, make([]byte, 1<<30)) // 1 GiB of zeros
	t.Logf("bomb: %d bytes", len(bomb))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var lerr error
	if msg := catchPanic(func() { _, lerr = loadSave(t, a, bomb, ld) }); msg != "" {
		t.Fatalf("load panicked: %s", msg)
	}
	runtime.ReadMemStats(&after)
	if lerr == nil {
		t.Fatal("bomb accepted")
	}
	if d := after.TotalAlloc - before.TotalAlloc; d > 256<<20 {
		t.Fatalf("loader allocated %d MB for a %d-byte save (err %v)", d>>20, len(bomb), lerr)
	}
	t.Logf("rejected: %v", lerr)
}
