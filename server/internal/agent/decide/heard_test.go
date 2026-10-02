package decide

import (
	"encoding/json"
	"strings"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/worldmodel"
)

// TestHeardEnemyIsCoarse: an enemy placed by ear alone is listed with the
// coarse cue only (a bearing in steps of 45°, no elevation, a distance in
// steps of 50, how loud it sounded, no aim): its true position (Pos, which
// hearing never sets) and velocity do not reach the lane state.
func TestHeardEnemyIsCoarse(t *testing.T) {
	p := testProjector()
	heard := func(pos, vel Vec3) State {
		b := testBelief()
		b.Tracks = append(b.Tracks, worldmodel.Track{ID: "e9", Num: 30, Class: "parasite", Kind: perception.KindMonster.String(),
			Lump: -1, Pos: pos, Vel: vel, LastHeard: fixtureNow, LastUpdate: fixtureNow, Awareness: worldmodel.Attacking,
			Ear: worldmodel.Ear{At: fixtureNow, Sound: "attack", Pan: perception.PanLeft, Loud: perception.LoudNear, Yaw: 170,
				Spread: 67, Dist: 330, Est: Vec3{-325, 57, 24}},
			Loc: Vec3{-325, 57, 24}, LocKnown: true, Threat: 12})
		return p.Fast(b, Context{})
	}
	a, b := heard(Vec3{-200, 120, 10}, Vec3{0, 300, 0}), heard(Vec3{-600, -40, 90}, Vec3{-250, 0, 0})
	var e *Enemy
	for i := range a.Enemies {
		if a.Enemies[i].ID == "e9" {
			e = &a.Enemies[i]
		}
	}
	if e == nil {
		t.Fatalf("heard enemy not listed: %+v", a.Enemies)
	}
	if e.Heard != "near" || e.Visible || e.Shootable || e.Aim != "off" || e.Elev != 0 || e.Bearing%45 != 0 || e.Units%50 != 0 {
		t.Fatalf("heard enemy %+v", *e)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Fatalf("the heard enemy's true position leaks into the lane state:\n%s\n%s", ja, jb)
	}
	q, _ := targetQuestion(&a)
	if i := q.Index("e9"); i < 0 || !strings.Contains(q.Options[i].Desc, "only heard (near)") {
		t.Fatalf("target question: %+v", q.Options)
	}
}
