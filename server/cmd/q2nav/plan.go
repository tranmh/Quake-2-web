package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/route"
)

// defaultRoutes are tried in order when -routes is not given (run from the
// repository root or from server/).
func defaultRoutes() []string {
	return []string{filepath.Join("fixtures", "agent", "routes"), filepath.Join("..", "fixtures", "agent", "routes")}
}

func runPlan(args []string, stdout io.Writer) error {
	fs := newFlags("plan")
	pakFile := fs.String("pak", "", "pak file holding the maps")
	routes := fs.String("routes", "", "route table directory (default fixtures/agent/routes)")
	skill := fs.Int("skill", -1, "skill level (default: the campaign's)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	dir := *routes
	if dir == "" {
		for _, d := range defaultRoutes() {
			if _, err := os.Stat(filepath.Join(d, route.CampaignFile)); err == nil {
				dir = d
				break
			}
		}
		if dir == "" {
			return fmt.Errorf("%w: no %s under %s; pass -routes", errUsage, route.CampaignFile, strings.Join(defaultRoutes(), " or "))
		}
	}
	c, err := route.Load(dir)
	if err != nil {
		return err
	}
	if *skill < 0 {
		*skill = c.Skill
	}
	src, err := openPak(*pakFile, mapdata.Options{Skill: *skill})
	if err != nil {
		return err
	}
	defer src.Close()

	maps := map[string]*mapdata.Map{}
	load := func(name string) (*mapdata.Map, error) {
		name = strings.ToLower(name)
		if m, ok := maps[name]; ok {
			return m, nil
		}
		m, err := src.load(name)
		if err != nil {
			return nil, err
		}
		maps[name] = m
		return m, nil
	}

	for _, name := range src.maps() {
		m, err := load(name)
		if err != nil {
			return err
		}
		printExits(stdout, m)
	}

	fmt.Fprintf(stdout, "route tables %s (campaign %q, skill %d)\n", dir, c.Name, *skill)
	for _, t := range c.Tables {
		printTable(stdout, t, maps[strings.ToLower(t.Map)])
	}
	err = route.ValidateCampaign(c, load)
	var verr *route.Error
	if errors.As(err, &verr) {
		fmt.Fprintf(stdout, "\n%d problem(s):\n", len(verr.Problems))
		for _, p := range verr.Problems {
			fmt.Fprintf(stdout, "  %s\n", p)
		}
		return fmt.Errorf("%d route table problem(s)", len(verr.Problems))
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\nall %d visit tables valid; campaign ends at %s (%s)\n", len(c.Tables), c.Terminal.Exit, c.Terminal.Kind)
	return nil
}

// printExits prints every exit of the map with the logic chains that fire
// it, from the target_changelevel back to the sources.
func printExits(w io.Writer, m *mapdata.Map) {
	fmt.Fprintf(w, "%s %q (skill %d, checksum %d): %d exit(s)\n", m.Name, m.Message, m.Skill, int32(m.Checksum), len(m.Exits))
	for _, x := range m.Exits {
		kind := ""
		if x.Level.Kind != mapdata.ExitLevel {
			kind = " [" + x.Level.Kind.String() + "]"
		}
		fmt.Fprintf(w, "  exit %s -> %s%s\n", label(m, x.Entity), x.Level.Raw, kind)
		printActivators(w, m, x.Entity, 2, map[int]bool{x.Entity: true})
	}
	fmt.Fprintln(w)
}

// printActivators prints, indented, every entity that fires i, how that
// entity is set off itself, and recursively who fires it.
func printActivators(w io.Writer, m *mapdata.Map, i, depth int, path map[int]bool) {
	links := m.FiredBy(i)
	if len(links) == 0 && depth == 2 {
		fmt.Fprintf(w, "%s(nothing fires it)\n", strings.Repeat("  ", depth))
	}
	for _, l := range links {
		if l.Kind == mapdata.LinkKill {
			continue // removing the entity does not fire it
		}
		pad := strings.Repeat("  ", depth)
		verb := "<-"
		switch l.Kind {
		case mapdata.LinkCarry:
			verb = "<- carried in by"
		case mapdata.LinkTeam:
			verb = "<- team master"
		}
		delay := ""
		if l.Delay != 0 {
			delay = fmt.Sprintf(" after %gs", l.Delay)
		}
		fmt.Fprintf(w, "%s%s %s%s%s%s\n", pad, verb, label(m, l.From), how(m, l.From), delay, sideEffects(m, l.From))
		if path[l.From] || depth > 12 {
			if path[l.From] {
				fmt.Fprintf(w, "%s  (loop)\n", pad)
			}
			continue
		}
		path[l.From] = true
		printActivators(w, m, l.From, depth+1, path)
		delete(path, l.From)
	}
}

// label names an entity: "#582 func_door *31 (t4)".
func label(m *mapdata.Map, i int) string { return m.Entity(i).String() }

// how describes how an entity is set off by itself and what it does when
// used.
func how(m *mapdata.Map, i int) string {
	var parts []string
	for _, s := range m.Sources(i) {
		switch {
		case s == mapdata.SrcTouch && m.Trigger(i) != nil && m.Trigger(i).Triggered:
			parts = append(parts, "touch once enabled")
		case s == mapdata.SrcTouch && m.Trigger(i) != nil && m.Trigger(i).Directional():
			parts = append(parts, fmt.Sprintf("touch facing %v", m.Trigger(i).Movedir))
		case s == mapdata.SrcDeath:
			mo := m.Entity(i)
			parts = append(parts, fmt.Sprintf("killed (spawned at %v)", mo.Origin))
		default:
			parts = append(parts, s.String())
		}
	}
	if t := m.Trigger(i); t != nil && t.Item != "" {
		parts = append(parts, "needs "+t.Item)
	}
	if t := m.Trigger(i); t != nil && t.Classname == "trigger_counter" {
		parts = append(parts, fmt.Sprintf("after %d uses", t.Count))
	}
	if r := m.Response(i); r != mapdata.RespNone && r != mapdata.RespRelay && r != mapdata.RespKey && r != mapdata.RespCounter {
		parts = append(parts, "used: "+r.String())
	}
	if mv := m.Mover(i); mv != nil && mv.TravelTime > 0 && (mv.Kind == mapdata.MoverButton || len(m.Fires(i)) > 0) {
		parts = append(parts, fmt.Sprintf("travel %.1fs", mv.TravelTime))
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, "; ") + "]"
}

// sideEffects lists the killtargets an entity removes when it fires.
func sideEffects(m *mapdata.Map, i int) string {
	var kills []string
	for _, l := range m.Fires(i) {
		if l.Kind == mapdata.LinkKill {
			kills = append(kills, label(m, l.To))
		}
	}
	if len(kills) == 0 {
		return ""
	}
	return " {kills " + strings.Join(kills, ", ") + "}"
}

// printTable prints a table's steps with the entities they resolve to.
func printTable(w io.Writer, t *route.Table, m *mapdata.Map) {
	from := t.From
	if from == "" {
		from = "(start)"
	}
	fmt.Fprintf(w, "  %s: %s visit %d %q, from %s, exit %s\n", t.Name, t.Map, t.Visit, t.Title, from, t.Exit.Map)
	for i, s := range t.Steps {
		target := ""
		if s.Target != nil {
			target = " " + s.Target.String()
			if m != nil {
				if e, err := route.Resolve(*s.Target, m); err == nil {
					target = " " + e.String()
				}
			}
		}
		if s.Class != "" {
			target += " " + s.Class
		}
		if s.Pos != nil {
			target += fmt.Sprintf(" at %v", *s.Pos)
		}
		if s.Until != "" {
			target += " until " + s.Until
		}
		if s.Seconds != 0 {
			target += fmt.Sprintf(" %gs", s.Seconds)
		}
		if s.Yaw != nil {
			target += fmt.Sprintf(" yaw %g", *s.Yaw)
		}
		var effs []string
		for _, e := range s.Effects {
			effs = append(effs, string(e.Kind)+" "+e.Target.String())
		}
		eff := ""
		if len(effs) > 0 {
			eff = " => " + strings.Join(effs, ", ")
		}
		fmt.Fprintf(w, "    %2d %-7s%s%s\n", i, s.Op, target, eff)
		if m != nil {
			printStepChain(w, &s, m)
		}
	}
	for _, a := range t.Avoid {
		fmt.Fprintf(w, "    avoid %s: %s\n", a.Target.String(), a.Why)
	}
}

// printStepChain prints what the step's own activation sets off, leaving
// out lights, speakers and messages.
func printStepChain(w io.Writer, s *route.Step, m *mapdata.Map) {
	var start int
	switch s.Op {
	case route.OpTouch, route.OpPress, route.OpShoot, route.OpRide:
		e, err := route.Resolve(*s.Target, m)
		if err != nil {
			return
		}
		start = e.Index
	case route.OpKill, route.OpPickup:
		found := false
		for _, mo := range m.Monsters {
			if s.Op == route.OpKill && mo.Classname == s.Class && s.Pos != nil && mo.Origin == mapdata.Vec3(*s.Pos) {
				start, found = mo.Entity, true
			}
		}
		for _, it := range m.Items {
			if s.Op == route.OpPickup && it.Classname == s.Class && s.Pos != nil && it.Origin == mapdata.Vec3(*s.Pos) {
				start, found = it.Entity, true
			}
		}
		if !found {
			return
		}
	default:
		return
	}
	var parts []string
	for _, r := range m.Reach(start) {
		what := r.Response.String()
		switch {
		case r.Removed:
			what = "removed"
		case r.Disabled:
			what = "touch (disabled)"
		case r.Response == mapdata.RespNone, r.Response == mapdata.RespOther, r.Response == mapdata.RespRelay && m.Trigger(r.Entity) != nil && !m.Trigger(r.Entity).HasVolume:
			continue
		case r.Response == mapdata.RespToggle && m.Laser(r.Entity) == nil && m.Mover(r.Entity) == nil:
			continue // lights, area portals
		case r.Response == mapdata.RespRelay && m.Trigger(r.Entity) == nil:
			continue // target_goal / target_secret counters
		}
		p := fmt.Sprintf("%s %s", what, label(m, r.Entity))
		if r.Delay != 0 {
			p += fmt.Sprintf(" +%.1fs", r.Delay)
		}
		if len(r.Requires) > 0 {
			p += " (needs " + strings.Join(r.Requires, ", ") + ")"
		}
		parts = append(parts, p)
	}
	if len(parts) > 0 {
		fmt.Fprintf(w, "         sets off: %s\n", strings.Join(parts, "; "))
	}
}
