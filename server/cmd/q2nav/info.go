package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"quake2web/server/internal/agent/mapdata"
)

func runInfo(args []string, stdout io.Writer) error {
	fs := newFlags("info")
	pakFile := fs.String("pak", "", "pak file holding maps/<map>.bsp")
	name := fs.String("map", "", "level name (e.g. demo1)")
	skill := fs.Int("skill", 1, "skill level (0-3)")
	dm := fs.Bool("deathmatch", false, "apply the deathmatch spawn filter")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("%w: -map is required", errUsage)
	}
	src, err := openPak(*pakFile, mapdata.Options{Skill: *skill, Deathmatch: *dm})
	if err != nil {
		return err
	}
	defer src.Close()
	m, err := src.load(*name)
	if err != nil {
		return err
	}
	printInfo(stdout, m)
	return nil
}

// printInfo writes the summary and the mover, trigger, exit, laser and
// spawn tables of m.
func printInfo(w io.Writer, m *mapdata.Map) {
	var present, inhibited, freed int
	for i := range m.Entities {
		switch e := &m.Entities[i]; {
		case e.Inhibited:
			inhibited++
		case e.Freed:
			freed++
		default:
			present++
		}
	}
	fmt.Fprintf(w, "map %s %q  checksum %d  skill %d  deathmatch %v\n",
		m.Name, m.Message, int32(m.Checksum), m.Skill, m.Options.Deathmatch)
	fmt.Fprintf(w, "entities %d (present %d, inhibited %d, freed at spawn %d)\n",
		len(m.Entities), present, inhibited, freed)
	fmt.Fprintf(w, "movers %d  triggers %d  exits %d  lasers %d  spawns %d  items %d  monsters %d\n\n",
		len(m.Movers), len(m.Triggers), len(m.Exits), len(m.Lasers), len(m.Spawns), len(m.Items), len(m.Monsters))

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MOVER\tMODEL\tCLASS\tACT\tTARGETNAME\tTARGET\tMASTER\tPOS1\tPOS2\tMOVEDIR\tSPEED\tWAIT\tTRAVEL\tTRIGGER")
	for _, mv := range m.Movers {
		master := ""
		if mv.TeamMaster != mv.Entity {
			master = fmt.Sprintf("#%d", mv.TeamMaster)
		}
		trig := ""
		if mv.Trigger != nil {
			trig = fmtBox(*mv.Trigger)
		}
		pos2 := fmtVec(mv.Pos2)
		if mv.Kind == mapdata.MoverTrain {
			pos2 = fmt.Sprintf("%d corners", len(mv.Path))
		}
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%g\t%g\t%.1fs\t%s\n",
			mv.Entity, mv.Model, mv.Classname, mv.Activation, mv.Targetname, mv.Target, master,
			fmtVec(mv.Pos1), pos2, fmtVec(mv.Movedir), mv.Speed, mv.Wait, mv.TravelTime, trig)
	}
	tw.Flush()
	fmt.Fprintln(w)

	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TRIGGER\tMODEL\tCLASS\tFLAGS\tTARGETNAME\tTARGET\tKILLTARGET\tBOX\tMOVEDIR")
	for _, t := range m.Triggers {
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			t.Entity, t.Model, t.Classname, triggerFlags(&t), t.Targetname, t.Target, t.Killtarget,
			fmtBoxIf(t.HasVolume, t.Box), fmtVecIf(t.Movedir))
	}
	tw.Flush()
	fmt.Fprintln(w)

	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "EXIT\tTARGETNAME\tMAP\tLEVEL\tSPAWNPOINT\tKIND\tNEWUNIT")
	for _, x := range m.Exits {
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%s\t%s\t%s\t%v\n", x.Entity, x.Targetname, x.Level.Raw, x.Level.Map,
			x.Level.Spawnpoint, x.Level.Kind, x.Level.NewUnit)
	}
	tw.Flush()

	if len(m.Lasers) > 0 {
		fmt.Fprintln(w)
		tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "LASER\tTARGETNAME\tON\tDMG\tSTART\tEND")
		for _, l := range m.Lasers {
			fmt.Fprintf(tw, "#%d\t%s\t%v\t%d\t%s\t%s\n", l.Entity, l.Targetname, l.StartOn, l.Dmg, fmtVec(l.Start), fmtVec(l.End))
		}
		tw.Flush()
	}

	fmt.Fprintln(w)
	tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SPAWN\tTARGETNAME\tORIGIN\tYAW")
	for _, s := range m.Spawns {
		fmt.Fprintf(tw, "#%d\t%s\t%s\t%g\n", s.Entity, s.Targetname, fmtVec(s.Origin), s.Angles[1])
	}
	tw.Flush()
}

func triggerFlags(t *mapdata.Trigger) string {
	var f []string
	if !t.HasVolume {
		f = append(f, "point")
	}
	if t.Monster {
		f = append(f, "monster")
	}
	if t.NotPlayer {
		f = append(f, "not_player")
	}
	if t.Triggered {
		f = append(f, "triggered")
	}
	if t.HasVolume && !t.StartsEnabled {
		f = append(f, "off")
	}
	if t.Directional() {
		f = append(f, "directional")
	}
	if t.Wait < 0 {
		f = append(f, "once")
	}
	if t.Delay != 0 {
		f = append(f, fmt.Sprintf("delay=%g", t.Delay))
	}
	if t.Item != "" {
		f = append(f, "item="+t.Item)
	}
	if t.Classname == "trigger_counter" {
		f = append(f, fmt.Sprintf("count=%d", t.Count))
	}
	return strings.Join(f, ",")
}

func fmtVec(v mapdata.Vec3) string { return fmt.Sprintf("(%g %g %g)", v[0], v[1], v[2]) }

func fmtVecIf(v mapdata.Vec3) string {
	if v == (mapdata.Vec3{}) {
		return ""
	}
	return fmtVec(v)
}

func fmtBox(b mapdata.Box) string { return fmtVec(b.Min) + "-" + fmtVec(b.Max) }

func fmtBoxIf(ok bool, b mapdata.Box) string {
	if !ok {
		return ""
	}
	return fmtBox(b)
}
