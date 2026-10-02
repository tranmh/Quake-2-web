package decide

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/worldmodel"
)

// update rewrites golden files (Q2_UPDATE_FIXTURES=1).
var update = os.Getenv("Q2_UPDATE_FIXTURES") == "1"

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(bytes.TrimSuffix(got, []byte{'\n'}), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with Q2_UPDATE_FIXTURES=1)", err)
	}
	if !bytes.Equal(bytes.TrimSuffix(want, []byte{'\n'}), bytes.TrimSuffix(got, []byte{'\n'})) {
		t.Fatalf("%s differs (regenerate with Q2_UPDATE_FIXTURES=1)\ngot:  %s\nwant: %s", name, got, want)
	}
}

// fixedSpace is a SpaceProbe with fixed clearances.
type fixedSpace [4]float32

func (f fixedSpace) Clearance(Vec3, float32) [4]float32 { return f }

const fixtureNow = 10000

// testBelief is a synthetic belief of a fight: the bot at the origin
// facing +y (yaw 90) with a shotgun, six monster tracks (one dead, one
// long forgotten), a barrel, four projectiles (one its own, one moving
// away), seven items, recent damage, effects, a pickup and the help
// computer read.
func testBelief() *worldmodel.Belief {
	mon := perception.KindMonster.String()
	std := func(t worldmodel.Track) worldmodel.Track {
		t.Kind, t.PosKnown, t.Mins, t.Maxs, t.Lump = mon, true, Vec3{-16, -16, -24}, Vec3{16, 16, 32}, -1
		t.Loc, t.LocKnown, t.LocSeen = t.Pos, true, true
		if t.Life == 0 && t.LastUpdate == 0 {
			t.LastUpdate = fixtureNow
		}
		return t
	}
	b := &worldmodel.Belief{
		Level: worldmodel.LevelKey{Map: "demo1"}, Map: "demo1", Time: fixtureNow, ServerFrame: 100, Frames: 100,
		Self: worldmodel.Self{Origin: Vec3{0, 0, 24}, Eye: Vec3{0, 0, 46}, ViewAngles: Vec3{0, 90, 0},
			Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}, OnGround: true, Health: 43, Armor: 25, Ammo: 12,
			Weapon: "Shotgun", Fov: 90, Pickup: "Shells", PickupAt: 9800, InCombat: true, LastDamage: 9500},
		Tracks: []worldmodel.Track{
			std(worldmodel.Track{ID: "e1", Num: 10, Class: "soldier", Pos: Vec3{100, 300, 24}, Visible: true, Shootable: true,
				LastSeen: fixtureNow, FirstSeen: 5000, Awareness: worldmodel.Attacking, Threat: 9, Confidence: 1}),
			std(worldmodel.Track{ID: "e2", Num: 11, Class: "gunner", Pos: Vec3{-400, 600, 24}, Visible: true, Shootable: true,
				LastSeen: fixtureNow, FirstSeen: 9300, Awareness: worldmodel.Attacking, Threat: 20, Confidence: 1}),
			std(worldmodel.Track{ID: "e3", Num: 12, Class: "berserk", Pos: Vec3{0, 150, 24}, Visible: true, Shootable: true,
				LastSeen: fixtureNow, FirstSeen: 4000, Awareness: worldmodel.Alert, Threat: 8, Wounded: true, Confidence: 1}),
			std(worldmodel.Track{ID: "e4", Num: 13, Class: "infantry", Pos: Vec3{500, -200, 24}, LastSeen: 8000, LastUpdate: 8000,
				FirstSeen: 3000, Awareness: worldmodel.Alert, Threat: 3, Confidence: 0.5}),
			std(worldmodel.Track{ID: "e5", Num: 14, Class: "soldier_light", Pos: Vec3{0, 1500, 24}, Visible: true, Shootable: true,
				LastSeen: fixtureNow, FirstSeen: 2000, Awareness: worldmodel.Idle, Threat: 1, Confidence: 1}),
			std(worldmodel.Track{ID: "e6", Num: 15, Class: "soldier", Pos: Vec3{50, 50, 24}, Life: worldmodel.LifeDead,
				LastUpdate: 8200, FirstSeen: 1000}),
			std(worldmodel.Track{ID: "e7", Num: 16, Class: "tank", Pos: Vec3{900, 0, 24}, LastSeen: 1000, LastUpdate: 1000,
				FirstSeen: 900, Awareness: worldmodel.Alert, Threat: 4}),
			{ID: "o1", Num: 20, Class: "barrel", Kind: perception.KindBarrel.String(), Pos: Vec3{30, 30, 0}, PosKnown: true,
				Loc: Vec3{30, 30, 0}, LocKnown: true, LocSeen: true, Visible: true, LastUpdate: fixtureNow},
		},
		Projectiles: []worldmodel.Projectile{
			{ID: "p1", Num: 30, Class: "rocket", Weapon: "rocket", Pos: Vec3{0, 200, 40}, TCA: 0.3, Miss: 10, Danger: true, DodgeSide: 1},
			{ID: "p2", Num: 31, Class: "blaster_bolt", Weapon: "blaster", Pos: Vec3{-100, 400, 30}, TCA: 1.0, Miss: 80},
			{ID: "p3", Num: 32, Class: "rocket", Weapon: "rocket", Pos: Vec3{0, 30, 40}, Own: true, TCA: -0.1},
			{ID: "p4", Num: 33, Class: "grenade", Weapon: "grenade", Pos: Vec3{300, 0, 10}, TCA: -1},
		},
		Items: []worldmodel.Item{
			{ID: "i1", Num: 40, Class: "item_health_large", Kind: "health", Pickup: "Health", Amount: 25, Pos: Vec3{200, 100, 8}},
			{ID: "i2", Num: 41, Class: "weapon_supershotgun", Kind: "weapon", Pickup: "Super Shotgun", Amount: 1, Pos: Vec3{-300, -100, 8}},
			{ID: "i3", Num: 42, Class: "ammo_shells", Kind: "ammo", Pickup: "Shells", Amount: 10, Pos: Vec3{50, -400, 8}},
			{ID: "i4", Num: 43, Class: "item_armor_shard", Kind: "armor", Pickup: "Armor Shard", Amount: 2, Pos: Vec3{10, 10, 8}},
			{ID: "i5", Num: 44, Class: "ammo_rockets", Kind: "ammo", Pickup: "Rockets", Amount: 5, Pos: Vec3{20, 20, 8}},
			{ID: "i6", Num: 45, Class: "item_health", Kind: "health", Pickup: "Health", Amount: 10, Pos: Vec3{5, 5, 8}, Life: worldmodel.LifeTaken},
			{ID: "i7", Num: 46, Class: "key_blue_key", Kind: "key", Pickup: "Blue Key", Amount: 1, Pos: Vec3{1000, 1000, 8}},
		},
		Damage: []worldmodel.DamageEvent{
			{At: 7000, Frame: 70, Health: 5, Cause: "hit"},
			{At: 9500, Frame: 95, Health: 8, Armor: 4, Cause: "hit", BearingKnown: true, Bearing: 60, Relative: -30, Source: "e1"},
		},
		Effects: []worldmodel.Effect{
			{Kind: worldmodel.EffectMonsterDead, Lump: 12, Ref: "e6", At: 8200, Frame: 82},
			{Kind: worldmodel.EffectLaserOff, Lump: 5, Ref: "", At: 9000, Frame: 90},
		},
		Inventory: worldmodel.Inventory{Known: true, Items: []worldmodel.InvItem{
			{Index: 7, Name: "Blaster", Count: 1}, {Index: 8, Name: "Shotgun", Count: 1}, {Index: 10, Name: "Machinegun", Count: 1},
			{Index: 12, Name: "Grenades", Count: 5}, {Index: 18, Name: "Shells", Count: 12}, {Index: 19, Name: "Bullets", Count: 50}}},
		Help:      perception.Help{Kills: 2, KillsMax: 12, Secrets: 0, SecretsMax: 3},
		HelpKnown: true,
		Memory:    worldmodel.MemoryView{Entries: 2, DeathSpots: []Vec3{{100, 100, 24}}},
	}
	return b
}

func testObjective() *ObjectiveView {
	return &ObjectiveView{Kind: "press", Desc: "press button *34 in the elevator car", Bearing: 25, PathDist: 640, NextWaypointBearing: 12}
}

func testProjector() *Projector {
	return NewProjector(ProjectorConfig{Space: fixedSpace{300, 40, 100, 200}})
}

// funcBackend is a DecisionBackend from a function.
type funcBackend struct {
	name string
	fn   func(ctx context.Context, req *Request) (*Response, error)
}

func (f *funcBackend) Name() string { return f.name }
func (f *funcBackend) Decide(ctx context.Context, req *Request) (*Response, error) {
	return f.fn(ctx, req)
}

// constBackend answers every question with fixed option keys (one-hot);
// a question without an entry is answered with its first option.
func constBackend(keys map[string]string) *funcBackend {
	return &funcBackend{name: "const", fn: func(_ context.Context, req *Request) (*Response, error) {
		ans := map[string]Answer{}
		for i := range req.Questions {
			q := &req.Questions[i]
			k, ok := keys[q.ID]
			if !ok || q.Index(k) < 0 {
				k = q.Options[0].Key
			}
			ans[q.ID] = OneHot(q, k)
		}
		return &Response{Seq: req.Seq, Answers: ans, Model: "const"}, nil
	}}
}
