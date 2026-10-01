package perception

import (
	"strconv"
	"strings"

	"quake2web/server/internal/qcommon/shared"
)

// Help is the help computer page (the F1 layout) as the player reads it.
type Help struct {
	Skill      string // "easy", "medium", "hard", "hard+"
	LevelName  string
	Help1      string // primary objective text
	Help2      string // secondary objective text
	Kills      int
	KillsMax   int
	Goals      int
	GoalsMax   int
	Secrets    int
	SecretsMax int
}

// layoutStrings returns the text drawn by a layout program's string,
// string2, cstring and cstring2 commands, in order, and whether it draws
// the "help" picture.
// Conditional blocks ("if <stat> ... endif") are read as if the stat were
// set: the help layout has none.
// C: client/cl_scrn.c:941 SCR_ExecuteLayoutString (token grammar)
func layoutStrings(layout string) (texts []string, helpPic bool) {
	data := layout
	for {
		tok, rest, more := shared.COM_Parse(data)
		if !more {
			return texts, helpPic
		}
		data = rest
		switch tok {
		case "picn", "pic", "xl", "xr", "xv", "yt", "yb", "yv", "num", "stat_string", "if":
			arg, rest, more := shared.COM_Parse(data)
			if !more {
				return texts, helpPic
			}
			data = rest
			if tok == "picn" && arg == "help" {
				helpPic = true
			}
			if tok == "num" { // num <width> <stat>
				if _, rest, more = shared.COM_Parse(data); !more {
					return texts, helpPic
				}
				data = rest
			}
		case "string", "string2", "cstring", "cstring2":
			arg, rest, more := shared.COM_Parse(data)
			if !more {
				return texts, helpPic
			}
			data = rest
			texts = append(texts, arg)
		case "client": // client x y clientnum score ping time
			for i := 0; i < 6; i++ {
				if _, rest, more := shared.COM_Parse(data); more {
					data = rest
				}
			}
		case "ctf": // ctf x y clientnum score ping
			for i := 0; i < 5; i++ {
				if _, rest, more := shared.COM_Parse(data); more {
					data = rest
				}
			}
		}
	}
}

// ParseHelp reads the layout HelpComputer sends for the "help" command. ok
// is false for any other layout (inventory, scoreboard).
//
//	xv 32 yv 8 picn help xv 202 yv 12 string2 "<skill>" xv 0 yv 24 cstring2 "<level>"
//	xv 0 yv 54 cstring2 "<help1>" xv 0 yv 110 cstring2 "<help2>"
//	xv 50 yv 164 string2 " kills     goals    secrets"
//	xv 50 yv 172 string2 "%3i/%3i     %i/%i       %i/%i"
//
// C: game/p_hud.c:301 HelpComputer
func ParseHelp(layout string) (Help, bool) {
	texts, pic := layoutStrings(layout)
	if !pic || len(texts) < 6 || !strings.Contains(texts[4], "kills") {
		return Help{}, false
	}
	h := Help{Skill: texts[0], LevelName: texts[1], Help1: texts[2], Help2: texts[3]}
	nums := strings.Fields(strings.NewReplacer("/", " / ").Replace(texts[5]))
	var v []int
	for i := 0; i < len(nums); i++ {
		if nums[i] == "/" {
			continue
		}
		n, err := strconv.Atoi(nums[i])
		if err != nil {
			return Help{}, false
		}
		v = append(v, n)
	}
	if len(v) != 6 {
		return Help{}, false
	}
	h.Kills, h.KillsMax, h.Goals, h.GoalsMax, h.Secrets, h.SecretsMax = v[0], v[1], v[2], v[3], v[4], v[5]
	return h, true
}
