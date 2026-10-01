package perception

import (
	"strconv"
	"strings"

	"quake2web/server/internal/assets/md2"
)

// AnimState is what an animation frame shows a player.
type AnimState uint8

// Animation states.
const (
	AnimUnknown AnimState = iota
	AnimStand
	AnimWalk
	AnimRun
	AnimAttack
	AnimPain
	AnimDeath
	AnimDuck
	AnimMove // other motion: take-off, banking, falling, fidgets
)

// String returns the state's name.
func (a AnimState) String() string {
	switch a {
	case AnimUnknown:
		return "unknown"
	case AnimStand:
		return "stand"
	case AnimWalk:
		return "walk"
	case AnimRun:
		return "run"
	case AnimAttack:
		return "attack"
	case AnimPain:
		return "pain"
	case AnimDeath:
		return "death"
	case AnimDuck:
		return "duck"
	case AnimMove:
		return "move"
	}
	return "anim" + strconv.Itoa(int(a))
}

// FrameAnim describes one MD2 frame: its name, what it shows and the frame
// range of the sequence it belongs to ("death2" of "death201".."death225").
type FrameAnim struct {
	Name     string
	Sequence string
	State    AnimState
	First    int32 // first frame of the sequence
	Last     int32 // last frame of the sequence
}

// ModelAnims are the frame animations of one MD2 model.
type ModelAnims struct {
	Frames []FrameAnim
}

// At returns frame f's animation (State AnimUnknown when out of range).
func (m *ModelAnims) At(f int32) FrameAnim {
	if m == nil || f < 0 || int(f) >= len(m.Frames) {
		return FrameAnim{}
	}
	return m.Frames[f]
}

// NewModelAnims derives the animations of MD2 frame names. Frames named
// <sequence><counter> group into sequences: a 3 digit counter carries the
// sequence number in its first digit ("pain301" is sequence "pain3"), a
// shorter counter does not ("run04", "stand12" for berserk "stand1".."5" is
// sequence "stand").
func NewModelAnims(names []string) *ModelAnims {
	m := &ModelAnims{Frames: make([]FrameAnim, len(names))}
	for i, n := range names {
		seq := sequenceOf(n)
		m.Frames[i] = FrameAnim{Name: n, Sequence: seq, State: stateOf(seq)}
	}
	for i := 0; i < len(m.Frames); {
		j := i
		for j+1 < len(m.Frames) && m.Frames[j+1].Sequence == m.Frames[i].Sequence {
			j++
		}
		for k := i; k <= j; k++ {
			m.Frames[k].First, m.Frames[k].Last = int32(i), int32(j)
		}
		i = j + 1
	}
	return m
}

// ParseModelAnims parses an MD2 file and derives its animations.
func ParseModelAnims(data []byte) (*ModelAnims, error) {
	mod, err := md2.Parse(data)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(mod.Frames))
	for i, f := range mod.Frames {
		names[i] = f.Name
	}
	return NewModelAnims(names), nil
}

func sequenceOf(name string) string {
	n := strings.ToLower(name)
	i := len(n)
	for i > 0 && n[i-1] >= '0' && n[i-1] <= '9' {
		i--
	}
	digits := len(n) - i
	if digits >= 3 {
		return n[:i+1]
	}
	return n[:i]
}

// stateOf names a sequence by the words the model authors used.
func stateOf(seq string) AnimState {
	base := strings.TrimRight(seq, "0123456789")
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.HasPrefix(base, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("death", "dead", "die", "crdeath"):
		return AnimDeath
	case has("pain", "crpain"):
		return AnimPain
	case has("runs"): // run and shoot (soldier attack 6, gunner runandshoot)
		return AnimAttack
	case has("attak", "attack", "att_", "r_att", "slam", "drain", "shoot", "fire", "crattak", "melee",
		"swing", "punch", "smash", "spin", "hit", "atk"):
		return AnimAttack
	case has("run", "charge"):
		return AnimRun
	case has("walk", "crwalk"):
		return AnimWalk
	case has("stand", "idle", "crstnd", "gun"):
		return AnimStand
	case has("duck", "block", "defens", "crouch", "dodge"):
		return AnimDuck
	}
	return AnimMove
}

// AnimCache loads model animations on demand from game data and keeps them.
// It is not safe for concurrent use; the ModelAnims it returns are
// immutable.
type AnimCache struct {
	read   func(name string) ([]byte, error)
	models map[string]*ModelAnims
}

// NewAnimCache returns a cache reading MD2 files with read (nil: no
// animations are known).
func NewAnimCache(read func(name string) ([]byte, error)) *AnimCache {
	return &AnimCache{read: read, models: map[string]*ModelAnims{}}
}

// Set records the animations of a model path (replacing what was loaded).
func (c *AnimCache) Set(path string, m *ModelAnims) { c.models[path] = m }

// Model returns the animations of an MD2 model path, or nil when the file
// is missing or not an MD2 (the result is remembered either way).
func (c *AnimCache) Model(path string) *ModelAnims {
	if c == nil || path == "" || !strings.HasSuffix(path, ".md2") {
		return nil
	}
	if m, ok := c.models[path]; ok {
		return m
	}
	var m *ModelAnims
	if c.read != nil {
		if data, err := c.read(path); err == nil {
			if parsed, err := ParseModelAnims(data); err == nil {
				m = parsed
			}
		}
	}
	c.models[path] = m
	return m
}
