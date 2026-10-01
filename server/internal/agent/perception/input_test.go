package perception

import (
	"testing"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// wire builds server messages for a passive client.
type wire struct{ w *msg.SizeBuf }

func newWire() *wire { return &wire{w: msg.NewSizeBuf(q2const.MAX_MSGLEN)} }

func (m *wire) bytes() []byte {
	b := append([]byte(nil), m.w.Bytes()...)
	m.w.SZ_Clear()
	return b
}

func (m *wire) serverdata(level string) *wire {
	m.w.MSG_WriteByte(q2const.Svc_serverdata)
	m.w.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	m.w.MSG_WriteLong(7)
	m.w.MSG_WriteByte(0)
	m.w.MSG_WriteString("baseq2")
	m.w.MSG_WriteShort(0)
	m.w.MSG_WriteString(level)
	return m
}

func (m *wire) configstring(i int, s string) *wire {
	m.w.MSG_WriteByte(q2const.Svc_configstring)
	m.w.MSG_WriteShort(int32(i))
	m.w.MSG_WriteString(s)
	return m
}

func (m *wire) baseline(s shared.EntityState) *wire {
	var null shared.EntityState
	m.w.MSG_WriteByte(q2const.Svc_spawnbaseline)
	m.w.MSG_WriteDeltaEntity(&null, &s, true, true)
	return m
}

func (m *wire) print(s string) *wire {
	m.w.MSG_WriteByte(q2const.Svc_print)
	m.w.MSG_WriteByte(2)
	m.w.MSG_WriteString(s)
	return m
}

func (m *wire) sound(num, ent int32) *wire {
	m.w.MSG_WriteByte(q2const.Svc_sound)
	m.w.MSG_WriteByte(q2const.SND_ENT)
	m.w.MSG_WriteByte(num)
	m.w.MSG_WriteShort(ent << 3)
	return m
}

// frame writes an uncompressed frame with the given entities.
func (m *wire) frame(n int32, ps shared.PlayerState, ents ...shared.EntityState) *wire {
	var null shared.PlayerState
	var nullEnt shared.EntityState
	m.w.MSG_WriteByte(q2const.Svc_frame)
	m.w.MSG_WriteLong(n)
	m.w.MSG_WriteLong(-1)
	m.w.MSG_WriteByte(0)
	m.w.MSG_WriteByte(1)
	m.w.MSG_WriteByte(0x02)
	m.w.WriteDeltaPlayerstate(&null, &ps) // writes svc_playerinfo itself
	m.w.MSG_WriteByte(q2const.Svc_packetentities)
	for i := range ents {
		m.w.MSG_WriteDeltaEntity(&nullEnt, &ents[i], true, true)
	}
	m.w.MSG_WriteShort(0)
	return m
}

func TestReader(t *testing.T) {
	c := fakeclient.NewPassive(fakeclient.Options{MaxHistory: 4})
	r := NewReader()
	feed := func(b []byte) {
		t.Helper()
		if _, err := c.FeedPayload(b); err != nil {
			t.Fatal(err)
		}
	}
	m := newWire()
	soldier := shared.EntityState{Number: 5, ModelIndex: 2, Origin: Vec3{100, 0, 24}, Solid: solidStd}
	feed(m.serverdata("demo1").configstring(q2const.CS_MODELS+1, "maps/demo1.bsp").
		configstring(q2const.CS_MODELS+2, modelSoldier).baseline(soldier).print("before\n").bytes())
	if _, ok := r.Next(c); ok {
		t.Fatal("input before the first frame")
	}
	var ps shared.PlayerState
	ps.PMove.Origin = [3]int16{8, 16, 24}
	feed(m.frame(1, ps, soldier).sound(1, 5).bytes())
	in, ok := r.Next(c)
	if !ok {
		t.Fatal("no input for the first frame")
	}
	if in.ServerFrame != 1 || in.Level.MapName != "demo1" || in.Level.Gen != 1 || in.OwnEntity() != 1 ||
		len(in.Entities) != 1 || in.Entities[0].Origin != soldier.Origin || in.AreaBytes != 1 || in.AreaBits[0] != 0x02 {
		t.Fatalf("input %+v", in)
	}
	if len(in.Level.Baselines) != 1 || in.Level.Baselines[0].Number != 5 || in.Level.Baselines[0].Origin != soldier.Origin {
		t.Fatalf("baselines %+v", in.Level.Baselines)
	}
	if len(in.Events.Prints) != 1 || in.Events.Prints[0] != "before\n" || len(in.Events.Sounds) != 1 || in.Events.Sounds[0].Ent != 5 {
		t.Fatalf("events %+v", in.Events)
	}
	if in.ConfigString(q2const.CS_MODELS+2) != modelSoldier || in.ConfigString(-1) != "" {
		t.Fatal("configstrings")
	}
	if _, ok := r.Next(c); ok {
		t.Fatal("the same frame reported twice")
	}

	// the input is a copy: the client reusing its buffers does not change it
	cs1, lv1 := in.CS, in.Level
	for i := 0; i < 10; i++ {
		m.print("spam\n")
	}
	feed(m.bytes())
	soldier.Origin[0] = 120
	feed(m.frame(2, ps, soldier).bytes())
	in2, ok := r.Next(c)
	if !ok || in2.ServerFrame != 2 || in2.Entities[0].Origin[0] != 120 || in.Entities[0].Origin[0] != 100 {
		t.Fatalf("second frame %+v (first %v)", in2.Entities, in.Entities[0].Origin)
	}
	if in.Events.Prints[0] != "before\n" {
		t.Fatal("events of an earlier input changed")
	}
	// 10 prints with MaxHistory 4: some were lost and say so
	if len(in2.Events.Prints) == 0 || in2.Events.Lost == 0 || uint64(len(in2.Events.Prints))+in2.Events.Lost != 10 {
		t.Fatalf("prints %d lost %d", len(in2.Events.Prints), in2.Events.Lost)
	}
	if in2.CS != cs1 || in2.Level != lv1 {
		t.Fatal("unchanged configstrings / level must be shared")
	}
	feed(m.configstring(q2const.CS_MODELS+3, "models/objects/rocket/tris.md2").frame(3, ps).bytes())
	in3, _ := r.Next(c)
	if in3.CS == cs1 || in3.ConfigString(q2const.CS_MODELS+3) == "" || cs1[q2const.CS_MODELS+3] != "" {
		t.Fatal("a changed configstring must make a new copy")
	}

	// a new level generation
	feed(m.serverdata("demo2").configstring(q2const.CS_MODELS+1, "maps/demo2.bsp").bytes())
	if _, ok := r.Next(c); ok {
		t.Fatal("input while loading")
	}
	feed(m.frame(1, ps).bytes())
	in4, ok := r.Next(c)
	if !ok || in4.Level.Gen != 2 || in4.Level.MapName != "demo2" || len(in4.Level.Baselines) != 0 {
		t.Fatalf("new level %+v", in4.Level)
	}
	// inventory freshness
	m.w.MSG_WriteByte(q2const.Svc_inventory)
	for i := 0; i < q2const.MAX_ITEMS; i++ {
		m.w.MSG_WriteShort(int32(i % 2))
	}
	feed(m.frame(2, ps).bytes())
	in5, _ := r.Next(c)
	if in5.InventorySeq != in4.InventorySeq+1 || in5.Inventory[1] != 1 {
		t.Fatalf("inventory seq %d -> %d", in4.InventorySeq, in5.InventorySeq)
	}
}
