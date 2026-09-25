package gametest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"quake2web/server/internal/qcommon/shared"
)

// KV is one scenario cvar, in file order.
type KV struct{ Key, Value string }

// ScheduleEntry is one "schedule" element of a game scenario.
type ScheduleEntry struct {
	Frame   int              `json:"frame"`
	Client  int              `json:"client"`
	Cmd     *json.RawMessage `json:"cmd"`
	Command *string          `json:"command"`
	Walk    *struct {
		Seed  *json.Number `json:"seed"`
		Until *int         `json:"until"`
	} `json:"random_walk"`
}

// Scenario is a materialized game scenario (docs/FIXTURES.md "game/").
type Scenario struct {
	Map               string          `json:"map"`
	Module            string          `json:"module"`
	EntstringOverride *string         `json:"entstring_override"`
	Seed              json.Number     `json:"seed"`
	RawCvars          json.RawMessage `json:"cvars"`
	Clients           []struct {
		Userinfo string `json:"userinfo"`
	} `json:"clients"`
	Frames   *int            `json:"frames"`
	Schedule []ScheduleEntry `json:"schedule"`

	Cvars []KV `json:"-"`
}

// ParseScenario decodes a scenario (or a fixture header, which echoes it).
func ParseScenario(data []byte) (*Scenario, error) {
	var sc Scenario
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&sc); err != nil {
		return nil, err
	}
	if sc.Map == "" {
		sc.Map = "demo1"
	}
	if sc.Module == "" {
		sc.Module = "baseq2"
	}
	if sc.Seed == "" {
		sc.Seed = "1"
	}
	if sc.Frames == nil {
		f := 100
		sc.Frames = &f
	}
	kv, err := orderedObject(sc.RawCvars)
	if err != nil {
		return nil, fmt.Errorf("cvars: %w", err)
	}
	sc.Cvars = kv
	return &sc, nil
}

// LoadScenario reads a scenario file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseScenario(data)
}

// orderedObject decodes a flat JSON object keeping key order. Numbers are
// printed like the oracle's "%g".
func orderedObject(raw json.RawMessage) ([]KV, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	if t != json.Delim('{') {
		return nil, fmt.Errorf("not an object")
	}
	var out []KV
	for d.More() {
		kt, err := d.Token()
		if err != nil {
			return nil, err
		}
		vt, err := d.Token()
		if err != nil {
			return nil, err
		}
		var v string
		switch x := vt.(type) {
		case string:
			v = x
		case json.Number:
			f, _ := x.Float64()
			v = cFormatG(f)
		default:
			v = fmt.Sprint(x)
		}
		out = append(out, KV{kt.(string), v})
	}
	return out, nil
}

// cFormatG approximates C "%g" (6 significant digits).
func cFormatG(f float64) string {
	s := strconv.FormatFloat(f, 'g', 6, 64)
	// C strips trailing zeros in %g; Go's 'g' with precision already does.
	return s
}

func (sc *Scenario) seed() uint32 {
	f, _ := sc.Seed.Float64()
	return uint32(int64(f))
}

// xorshift32 is oracle/src/ojson.c xr_t.
type xorshift32 struct{ s uint32 }

func (r *xorshift32) seed(seed uint32) {
	if seed == 0 {
		seed = 0x9E3779B9
	}
	r.s = seed
}

func (r *xorshift32) next() uint32 {
	x := r.s
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	r.s = x
	return x
}

// rwalk is game_main.c rwalk_t.
type rwalk struct {
	active, client, start, until int
	r                            xorshift32
	yaw, pitch                   int16
}

var rwalkFwd = [8]int16{400, 400, 400, 200, 0, -200, 400, 400}
var rwalkSide = [8]int16{0, 0, 0, 0, 200, -200, 400, -400}

// cmd is game_main.c rwalk_cmd.
func (w *rwalk) cmd() shared.UserCmd {
	a := w.r.next()
	b := w.r.next()
	w.yaw = int16(int32(w.yaw) + (int32(a&0x3ff) - 512))
	if (a>>10)&31 == 0 {
		w.yaw = int16(int32(w.yaw) + 8192)
	}
	w.pitch = int16(int32((a>>16)&0xfff) - 2048)
	var c shared.UserCmd
	c.Msec = 100
	c.LightLevel = 128
	c.Angles[0] = w.pitch
	c.Angles[1] = w.yaw
	c.ForwardMove = rwalkFwd[b&7]
	c.SideMove = rwalkSide[(b>>3)&7]
	up := (b >> 6) & 31
	switch {
	case up < 2:
		c.UpMove = 200
	case up == 2:
		c.UpMove = -200
	default:
		c.UpMove = 0
	}
	if (b>>11)&7 == 0 {
		c.Buttons = 128 | 1 // BUTTON_ANY | BUTTON_ATTACK
	}
	return c
}

// parseUC is os_read_uc.
func parseUC(raw json.RawMessage) (shared.UserCmd, error) {
	var m struct {
		Msec, Buttons                 json.Number
		Angles                        []json.Number
		Forwardmove, Sidemove, Upmove json.Number
		Impulse, Lightlevel           json.Number
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return shared.UserCmd{}, err
	}
	n := func(x json.Number) int64 {
		if x == "" {
			return 0
		}
		v, err := x.Int64()
		if err != nil {
			f, _ := x.Float64()
			v = int64(f)
		}
		return v
	}
	var c shared.UserCmd
	c.Msec = uint8(n(m.Msec))
	c.Buttons = uint8(n(m.Buttons))
	for i := 0; i < 3 && i < len(m.Angles); i++ {
		c.Angles[i] = int16(n(m.Angles[i]))
	}
	c.ForwardMove = int16(n(m.Forwardmove))
	c.SideMove = int16(n(m.Sidemove))
	c.UpMove = int16(n(m.Upmove))
	c.Impulse = uint8(n(m.Impulse))
	c.LightLevel = uint8(n(m.Lightlevel))
	return c, nil
}
