//go:build golden

package msg

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

type jES struct {
	Number      int32     `json:"number"`
	Origin      []float64 `json:"origin"`
	Angles      []float64 `json:"angles"`
	OldOrigin   []float64 `json:"old_origin"`
	ModelIndex  int32     `json:"modelindex"`
	ModelIndex2 int32     `json:"modelindex2"`
	ModelIndex3 int32     `json:"modelindex3"`
	ModelIndex4 int32     `json:"modelindex4"`
	Frame       int32     `json:"frame"`
	SkinNum     int32     `json:"skinnum"`
	Effects     uint32    `json:"effects"`
	RenderFX    int32     `json:"renderfx"`
	Solid       int32     `json:"solid"`
	Sound       int32     `json:"sound"`
	Event       int32     `json:"event"`
}

func (j *jES) es() shared.EntityState {
	return shared.EntityState{
		Number: j.Number, Origin: testutil.Vec3(j.Origin), Angles: testutil.Vec3(j.Angles),
		OldOrigin: testutil.Vec3(j.OldOrigin), ModelIndex: j.ModelIndex, ModelIndex2: j.ModelIndex2,
		ModelIndex3: j.ModelIndex3, ModelIndex4: j.ModelIndex4, Frame: j.Frame, SkinNum: j.SkinNum,
		Effects: j.Effects, RenderFX: j.RenderFX, Solid: j.Solid, Sound: j.Sound, Event: j.Event,
	}
}

type jUC struct {
	Msec        uint8   `json:"msec"`
	Buttons     uint8   `json:"buttons"`
	Angles      []int64 `json:"angles"`
	ForwardMove int16   `json:"forwardmove"`
	SideMove    int16   `json:"sidemove"`
	UpMove      int16   `json:"upmove"`
	Impulse     uint8   `json:"impulse"`
	LightLevel  uint8   `json:"lightlevel"`
}

func (j *jUC) uc() shared.UserCmd {
	return shared.UserCmd{Msec: j.Msec, Buttons: j.Buttons, Angles: testutil.Short3(j.Angles),
		ForwardMove: j.ForwardMove, SideMove: j.SideMove, UpMove: j.UpMove, Impulse: j.Impulse, LightLevel: j.LightLevel}
}

type jPMS struct {
	PmType      int32   `json:"pm_type"`
	Origin      []int64 `json:"origin"`
	Velocity    []int64 `json:"velocity"`
	PmFlags     uint8   `json:"pm_flags"`
	PmTime      uint8   `json:"pm_time"`
	Gravity     int16   `json:"gravity"`
	DeltaAngles []int64 `json:"delta_angles"`
}

type jPS struct {
	PMove      jPMS      `json:"pmove"`
	ViewAngles []float64 `json:"viewangles"`
	ViewOffset []float64 `json:"viewoffset"`
	KickAngles []float64 `json:"kick_angles"`
	GunAngles  []float64 `json:"gunangles"`
	GunOffset  []float64 `json:"gunoffset"`
	GunIndex   int32     `json:"gunindex"`
	GunFrame   int32     `json:"gunframe"`
	Blend      []float64 `json:"blend"`
	Fov        float64   `json:"fov"`
	RDFlags    int32     `json:"rdflags"`
	Stats      []int64   `json:"stats"`
}

func (j *jPS) ps() shared.PlayerState {
	ps := shared.PlayerState{
		PMove: shared.PmoveState{PmType: j.PMove.PmType, Origin: testutil.Short3(j.PMove.Origin),
			Velocity: testutil.Short3(j.PMove.Velocity), PmFlags: j.PMove.PmFlags, PmTime: j.PMove.PmTime,
			Gravity: j.PMove.Gravity, DeltaAngles: testutil.Short3(j.PMove.DeltaAngles)},
		ViewAngles: testutil.Vec3(j.ViewAngles), ViewOffset: testutil.Vec3(j.ViewOffset),
		KickAngles: testutil.Vec3(j.KickAngles), GunAngles: testutil.Vec3(j.GunAngles),
		GunOffset: testutil.Vec3(j.GunOffset), GunIndex: j.GunIndex, GunFrame: j.GunFrame,
		Fov: float32(j.Fov), RDFlags: j.RDFlags,
	}
	for i := 0; i < 4 && i < len(j.Blend); i++ {
		ps.Blend[i] = float32(j.Blend[i])
	}
	for i := 0; i < len(ps.Stats) && i < len(j.Stats); i++ {
		ps.Stats[i] = int16(j.Stats[i])
	}
	return ps
}

// runLines walks a JSONL fixture and stops at the first mismatch.
func runLines(t *testing.T, rel string, fn func(line []byte) error) {
	t.Helper()
	path := testutil.Fixture(t, rel)
	n := 0
	err := testutil.ReadJSONL(path, func(lineNo int, line []byte) error {
		n++
		return fn(line)
	})
	if err != nil {
		t.Fatalf("first mismatch: %v", err)
	}
	t.Logf("%s: %d cases OK", rel, n)
}

func TestGoldenEntity(t *testing.T) {
	runLines(t, "core/msg/entity.jsonl", func(line []byte) error {
		var c struct {
			From, To  jES
			Force     int
			NewEntity int `json:"newentity"`
			Bytes     string
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		from, to := c.From.es(), c.To.es()
		b := NewSizeBuf(1400 * 4)
		b.MSG_WriteDeltaEntity(&from, &to, c.Force != 0, c.NewEntity != 0)
		if got := hex.EncodeToString(b.Bytes()); got != c.Bytes {
			return fmt.Errorf("MSG_WriteDeltaEntity\n from %+v\n to   %+v\n force=%d new=%d\n got  %s\n want %s",
				from, to, c.Force, c.NewEntity, got, c.Bytes)
		}
		return nil
	})
}

func TestGoldenUsercmd(t *testing.T) {
	runLines(t, "core/msg/usercmd.jsonl", func(line []byte) error {
		var c struct {
			From, To jUC
			Bytes    string
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		from, to := c.From.uc(), c.To.uc()
		b := NewSizeBuf(64)
		b.MSG_WriteDeltaUsercmd(&from, &to)
		if got := hex.EncodeToString(b.Bytes()); got != c.Bytes {
			return fmt.Errorf("MSG_WriteDeltaUsercmd\n from %+v\n to   %+v\n got  %s\n want %s", from, to, got, c.Bytes)
		}
		if back := NewReader(b.Bytes()).MSG_ReadDeltaUsercmd(&from); back != to {
			return fmt.Errorf("MSG_ReadDeltaUsercmd round trip: got %+v want %+v", back, to)
		}
		return nil
	})
}

func TestGoldenPlayer(t *testing.T) {
	runLines(t, "core/msg/player.jsonl", func(line []byte) error {
		var c struct {
			From  *jPS
			To    jPS
			Bytes string
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		var from *shared.PlayerState
		if c.From != nil {
			f := c.From.ps()
			from = &f
		}
		to := c.To.ps()
		b := NewSizeBuf(1400 * 4)
		b.WriteDeltaPlayerstate(from, &to)
		if got := hex.EncodeToString(b.Bytes()); got != c.Bytes {
			return fmt.Errorf("WriteDeltaPlayerstate\n from %+v\n to   %+v\n got  %s\n want %s", from, to, got, c.Bytes)
		}
		return nil
	})
}

func TestGoldenScalar(t *testing.T) {
	runLines(t, "core/msg/scalar.jsonl", func(line []byte) error {
		var c struct {
			Op    string
			V     json.RawMessage
			Bytes string
		}
		if err := json.Unmarshal(line, &c); err != nil {
			return err
		}
		b := NewSizeBuf(4096)
		var desc string
		switch c.Op {
		case "long", "short", "char", "byte":
			var v int64
			if err := json.Unmarshal(c.V, &v); err != nil {
				return err
			}
			desc = fmt.Sprint(v)
			switch c.Op {
			case "long":
				b.MSG_WriteLong(int32(v))
			case "short":
				b.MSG_WriteShort(int32(v))
			case "char":
				b.MSG_WriteChar(int32(v))
			case "byte":
				b.MSG_WriteByte(int32(v))
			}
		case "coord", "angle", "angle16", "float":
			var v float64
			if err := json.Unmarshal(c.V, &v); err != nil {
				return err
			}
			f := float32(v)
			desc = testutil.FmtF32(f)
			switch c.Op {
			case "coord":
				b.MSG_WriteCoord(f)
			case "angle":
				b.MSG_WriteAngle(f)
			case "angle16":
				b.MSG_WriteAngle16(f)
			case "float":
				b.MSG_WriteFloat(f)
			}
		case "pos", "dir":
			var v []float64
			if err := json.Unmarshal(c.V, &v); err != nil {
				return err
			}
			if c.Op == "dir" && v == nil {
				desc = "null"
				b.MSG_WriteDir(nil)
				break
			}
			p := testutil.Vec3(v)
			desc = testutil.FmtVec3(p)
			if c.Op == "pos" {
				b.MSG_WritePos(p)
			} else {
				b.MSG_WriteDir(&p)
			}
		case "string":
			var v string
			if err := json.Unmarshal(c.V, &v); err != nil {
				return err
			}
			v = testutil.Latin1(v)
			desc = fmt.Sprintf("%q", v)
			b.MSG_WriteString(v)
		default:
			return fmt.Errorf("unknown op %q", c.Op)
		}
		if got := hex.EncodeToString(b.Bytes()); got != c.Bytes {
			return fmt.Errorf("op %s v=%s: got %s want %s", c.Op, desc, got, c.Bytes)
		}
		return nil
	})
}
