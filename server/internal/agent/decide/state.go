package decide

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = worldmodel.Vec3

// State is a lane state: what one request tells the backend. The fast
// lane fills Me, Enemies, Incoming and Space; the slow lane adds Mode,
// Me.Weapons, Items, Objective, Level and Events. Fields are structs and
// slices in a fixed order, so the encoding is deterministic.
//
// Legend (also in the questions' preamble): bearings are degrees from the
// bot's view, positive to the left, negative to the right, 0 ahead and 180
// behind; elev is degrees above (+) or below (-) the eye; units are map
// units (a player is 32 wide, 56 tall).
type State struct {
	// Mode is the bot's current mode (slow lane).
	Mode      string     `json:"mode,omitempty"`
	Me        Me         `json:"me"`
	Enemies   []Enemy    `json:"enemies"`
	Incoming  []Incoming `json:"incoming,omitempty"`
	Space     *Space     `json:"space,omitempty"`
	Items     []ItemView `json:"items,omitempty"`
	Objective *Objective `json:"objective,omitempty"`
	Level     *LevelInfo `json:"level,omitempty"`
	Events    []string   `json:"events,omitempty"`
}

// Me is the bot's own state.
type Me struct {
	// HP is the health bucket: critical (<25), low (<50), ok (<100), full.
	HP     string `json:"hp"`
	Health int    `json:"health"`
	Armor  int    `json:"armor"`
	// Weapon is the current weapon's key ("none" without one).
	Weapon string `json:"weapon"`
	// Ammo is the current weapon's ammo bucket: none, low, ok, or inf
	// (the blaster).
	Ammo     string `json:"ammo"`
	OnGround bool   `json:"on_ground"`
	InWater  bool   `json:"in_water,omitempty"` // waist deep or more (present only when true)
	// DamageLast1s is the health and armor lost in the last second.
	DamageLast1s int `json:"damage_last_1s"`
	// HitFromBearing is the bearing of the last hit of the last second
	// whose direction is known.
	HitFromBearing *int `json:"hit_from_bearing,omitempty"`
	// Moving is the movement of my current decision (advance, retreat,
	// strafe, hold; fast lane): what I am doing now, for continuity.
	Moving string `json:"moving,omitempty"`
	// TargetSinceS is how long (seconds, one decimal) my current target
	// has been my target (present only with a current target).
	TargetSinceS *float64 `json:"target_since_s,omitempty"`
	// Weapons are the owned weapons with ammo (slow lane).
	Weapons []string `json:"weapons,omitempty"`
	// Empty are the owned weapons without ammo (slow lane; present only
	// when there are some): their ammo is worth a detour.
	Empty []string `json:"empty,omitempty"`
}

// Enemy is a believed hostile monster.
type Enemy struct {
	ID      string `json:"id"`
	Class   string `json:"class"`
	Bearing int    `json:"bearing"`
	Elev    int    `json:"elev"`
	// Dist is the distance bucket: close (<250), mid (<700), far; Units
	// the distance from the eye.
	Dist  string `json:"dist"`
	Units int    `json:"units"`
	// Visible: in view now (else remembered: the last known position, or
	// where it was heard: Heard).
	Visible bool `json:"visible"`
	// Heard is set for an enemy out of view that only hearing places (how
	// loud it sounded: near, mid or far). Hearing gives no position: its
	// bearing is then the side it sounded from in steps of 45° (ahead and
	// behind alike until a turn of the bot tells them apart), elev 0, and
	// units the middle distance of its loudness, in steps of 50.
	Heard string `json:"heard,omitempty"`
	// Shootable: a shot reached it when it was last seen.
	Shootable bool `json:"shootable"`
	// Aim is how close the crosshair is: on (the view ray meets the
	// body), near (within about 10 degrees of it), off.
	Aim string `json:"aim"`
	// State is idle, alert (noticed someone) or attacking.
	State   string `json:"state"`
	Wounded bool   `json:"wounded,omitempty"`
	// Threat is the damage it can deal now: low, med, high.
	Threat string `json:"threat"`
	// Current marks the bot's current target.
	Current bool `json:"current,omitempty"`
	// Objective marks the monster the route's current step needs dead
	// (a kill step's, once seen): listed while it is alive and its
	// position known, however long ago it was seen.
	Objective bool `json:"objective,omitempty"`
}

// Incoming is an enemy projectile flying at the bot.
type Incoming struct {
	Kind    string `json:"kind"`
	Bearing int    `json:"bearing"`
	// ETA: imminent (dangerous, closest approach < 0.4 s), soon (< 1.5 s),
	// none (passing or moving away).
	ETA string `json:"eta"`
	// Dodge is the strafe side that avoids it: left, right, none.
	Dodge string `json:"dodge"`
}

// Space is the free room around the bot along its view's forward, back,
// left and right: blocked (<48 units), tight (<128), open.
type Space struct {
	Front string `json:"front"`
	Back  string `json:"back"`
	Left  string `json:"left"`
	Right string `json:"right"`
}

// ItemView is a useful item (slow lane).
type ItemView struct {
	ID    string `json:"id"`
	Class string `json:"class"`
	// Gives is what taking it gives: health+25, armor+50, shells+10,
	// weapon:shotgun, key:blue_key, quad_damage, ...
	Gives   string `json:"gives"`
	Bearing int    `json:"bearing"`
	// Path is the path distance (straight-line when no path function).
	Path int `json:"path"`
}

// ObjectiveView is the caller's objective (the route executor's current
// step), input to the slow lane.
type ObjectiveView struct {
	Kind string // step kind: goto, touch, press, shoot, ride, kill, pickup, ...
	Desc string // short description ("press button *34")
	// Bearing is the bearing to the objective (degrees from the view, +
	// left), PathDist the path distance to it (negative: unknown) and
	// NextWaypointBearing the bearing of the next path waypoint.
	Bearing             float32
	PathDist            float32
	NextWaypointBearing float32
	// Stalled: no progress towards it for a while.
	Stalled bool
	// Exit: the steps left lead straight out of the level (no kill,
	// pickup or confirmation left before the exit): what the bot carries
	// now is what it starts the next level with.
	Exit bool
	// Target is the track of the monster a kill step needs dead once the
	// bot has seen it ("" none): the fast lane lists it (Enemy.Objective).
	Target string
}

// Objective is the encoded ObjectiveView.
type Objective struct {
	Kind    string `json:"kind"`
	Desc    string `json:"desc"`
	Bearing int    `json:"bearing"`
	Path    int    `json:"path"`
	Next    int    `json:"next"`
	Stalled bool   `json:"stalled"`
	Exit    bool   `json:"exit,omitempty"` // present only when true
}

// LevelInfo is the level's progress (slow lane): kills and secrets from
// the help computer when the bot has read it.
type LevelInfo struct {
	Map        string `json:"map"`
	Kills      string `json:"kills,omitempty"`
	Secrets    string `json:"secrets,omitempty"`
	DeathsHere int    `json:"deaths_here"`
}

// Size limits.
const (
	// HardCap is the default limit of an encoded state (bytes).
	HardCap = 3072
	// maxDesc is the longest objective description (runes).
	maxDesc = 64
)

// Projection defaults.
const (
	DefaultMaxEnemies  = 4
	DefaultMaxIncoming = 2
	DefaultMaxItems    = 4
	DefaultMaxEvents   = 4
	// DefaultEnemyMemory is how long (ms) an enemy not observed stays in
	// the state at its last known position.
	DefaultEnemyMemory = 5000
	// DefaultEventWindow is how recent (ms) an event must be.
	DefaultEventWindow = 3000
)

// Bucket thresholds (units, health points).
const (
	closeDist = 250
	midDist   = 700
	hpLow     = 50
	hpCrit    = 25
	spaceWall = 48
	spaceRoom = 128
)

// ErrStateTooLarge is returned when a state cannot be trimmed under the
// size cap.
var ErrStateTooLarge = errors.New("decide: state exceeds the size cap")

// PathFunc returns the path distance between two points (static map
// knowledge: the nav graph); ok false means unreachable.
type PathFunc func(from, to Vec3) (dist float32, ok bool)

// ProjectorConfig configures a Projector. Zero values take the defaults.
type ProjectorConfig struct {
	// Space measures the free room around the bot (nil: no space field).
	Space SpaceProbe
	// Path measures path distances to items (nil: straight lines).
	Path PathFunc
	// Classes is the class table (nil: perception.NewClassTable()).
	Classes *perception.ClassTable

	MaxEnemies, MaxIncoming, MaxItems, MaxEvents int
	EnemyMemory, EventWindow                     int64 // ms
	// MaxBytes caps an encoded state (default HardCap).
	MaxBytes int
}

// Context is what the caller adds to a projection: its current intent
// and objective.
type Context struct {
	Target string // current target track id ("" none)
	// TargetSince is when (session ms) Target became the current target
	// (0: unknown).
	TargetSince int64
	Mode        Mode     // current mode ("" unknown)
	Moving      Movement // current movement ("" unknown)
	Objective   *ObjectiveView
}

// Projector turns beliefs into lane states. It is not safe for concurrent
// use when its SpaceProbe is not.
type Projector struct {
	cfg     ProjectorConfig
	classes *perception.ClassTable
}

// NewProjector returns a Projector.
func NewProjector(cfg ProjectorConfig) *Projector {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&cfg.MaxEnemies, DefaultMaxEnemies)
	def(&cfg.MaxIncoming, DefaultMaxIncoming)
	def(&cfg.MaxItems, DefaultMaxItems)
	def(&cfg.MaxEvents, DefaultMaxEvents)
	def(&cfg.MaxBytes, HardCap)
	if cfg.EnemyMemory <= 0 {
		cfg.EnemyMemory = DefaultEnemyMemory
	}
	if cfg.EventWindow <= 0 {
		cfg.EventWindow = DefaultEventWindow
	}
	p := &Projector{cfg: cfg, classes: cfg.Classes}
	if p.classes == nil {
		p.classes = perception.NewClassTable()
	}
	return p
}

// MaxBytes is the size cap of the states it projects.
func (p *Projector) MaxBytes() int { return p.cfg.MaxBytes }

// Project returns the lane's state for belief b.
func (p *Projector) Project(lane Lane, b *worldmodel.Belief, cx Context) State {
	st := p.Fast(b, cx)
	if lane == LaneSlow {
		p.Extend(&st, b, cx)
	}
	return st
}

// view is the bot's viewpoint for bearings.
type view struct {
	eye     Vec3
	yaw     float32
	forward Vec3
}

func viewOf(b *worldmodel.Belief) view {
	v := view{eye: b.Self.Eye, yaw: b.Self.ViewAngles[q2const.YAW]}
	shared.AngleVectors(b.Self.ViewAngles, &v.forward, nil, nil)
	return v
}

// bearing is the signed horizontal angle from the view to p (+ left).
func (v *view) bearing(p Vec3) int {
	yaw := math.Atan2(float64(p[1]-v.eye[1]), float64(p[0]-v.eye[0])) * 180 / math.Pi
	return roundAngle(angleDiff(yaw, float64(v.yaw)))
}

// elev is the angle above (+) or below (-) the eye.
func (v *view) elev(p Vec3) int {
	dx, dy, dz := float64(p[0]-v.eye[0]), float64(p[1]-v.eye[1]), float64(p[2]-v.eye[2])
	return int(math.Round(math.Atan2(dz, math.Hypot(dx, dy)) * 180 / math.Pi))
}

// aim buckets how close the crosshair is to a body with box [lo, hi] and
// center c: on when the view ray passes through the box, near within
// max(3 angular radii, 10 degrees) of the center, else off.
func (v *view) aim(lo, hi, c Vec3) string {
	if rayHitsBox(v.eye, v.forward, lo, hi) {
		return "on"
	}
	dir := Vec3{c[0] - v.eye[0], c[1] - v.eye[1], c[2] - v.eye[2]}
	n := math.Sqrt(float64(dir[0]*dir[0] + dir[1]*dir[1] + dir[2]*dir[2]))
	if n < 1 {
		return "on"
	}
	dot := float64(dir[0]*v.forward[0]+dir[1]*v.forward[1]+dir[2]*v.forward[2]) / n
	ang := math.Acos(math.Max(-1, math.Min(1, dot))) * 180 / math.Pi
	half := math.Max(float64(hi[0]-lo[0])/2, 16)
	radius := math.Atan2(half, n) * 180 / math.Pi
	if ang <= math.Max(3*radius, 10) {
		return "near"
	}
	return "off"
}

// rayHitsBox reports whether the ray from o along d (unit) meets the box
// [lo, hi] in front of o (slab test).
func rayHitsBox(o, d, lo, hi Vec3) bool {
	tmin, tmax := 0.0, math.Inf(1)
	for i := 0; i < 3; i++ {
		if math.Abs(float64(d[i])) < 1e-9 {
			if o[i] < lo[i] || o[i] > hi[i] {
				return false
			}
			continue
		}
		t1 := float64(lo[i]-o[i]) / float64(d[i])
		t2 := float64(hi[i]-o[i]) / float64(d[i])
		if t1 > t2 {
			t1, t2 = t2, t1
		}
		tmin, tmax = math.Max(tmin, t1), math.Min(tmax, t2)
		if tmin > tmax {
			return false
		}
	}
	return true
}

// angleDiff returns a-b normalized to (-180, 180].
func angleDiff(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d > 180 {
		d -= 360
	} else if d <= -180 {
		d += 360
	}
	return d
}

func roundAngle(a float64) int {
	r := int(math.Round(a))
	if r <= -180 {
		r += 360
	}
	return r
}

func distance(a, b Vec3) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func distBucket(d float64) string {
	switch {
	case d < closeDist:
		return "close"
	case d < midDist:
		return "mid"
	}
	return "far"
}

func hpBucket(hp int) string {
	switch {
	case hp < hpCrit:
		return "critical"
	case hp < hpLow:
		return "low"
	case hp < 100:
		return "ok"
	}
	return "full"
}

func threatBucket(t float32) string {
	switch {
	case t < 4:
		return "low"
	case t < 10:
		return "med"
	}
	return "high"
}

func spaceBucket(d float32) string {
	switch {
	case d < spaceWall:
		return "blocked"
	case d < spaceRoom:
		return "tight"
	}
	return "open"
}

// currentWeapon is the key of the weapon in hand ("" if none or unknown).
func currentWeapon(b *worldmodel.Belief) WeaponKey { return WeaponFromPickup(b.Self.Weapon) }

// ammoBucket buckets the current weapon's ammo (STAT_AMMO).
func ammoBucket(k WeaponKey, ammo int) string {
	switch {
	case k == WeaponBlaster:
		return "inf"
	case k == "" || ammo <= 0 || ammo < k.AmmoPerShot():
		return "none"
	case ammo < k.LowAmmo():
		return "low"
	}
	return "ok"
}

// Fast returns the fast (combat) lane state.
func (p *Projector) Fast(b *worldmodel.Belief, cx Context) State {
	v := viewOf(b)
	objective := ""
	if cx.Objective != nil {
		objective = cx.Objective.Target
	}
	st := State{Me: p.me(b, &v), Enemies: p.enemies(b, &v, cx.Target, objective), Incoming: p.incoming(b, &v)}
	st.Me.Moving = string(cx.Moving)
	if cx.TargetSince > 0 && cx.TargetSince <= b.Time {
		for i := range st.Enemies {
			if st.Enemies[i].Current {
				s := math.Round(float64(b.Time-cx.TargetSince)/100) / 10
				st.Me.TargetSinceS = &s
				break
			}
		}
	}
	if p.cfg.Space != nil {
		c := p.cfg.Space.Clearance(b.Self.Origin, v.yaw)
		st.Space = &Space{Front: spaceBucket(c[0]), Back: spaceBucket(c[1]), Left: spaceBucket(c[2]), Right: spaceBucket(c[3])}
	}
	return st
}

func (p *Projector) me(b *worldmodel.Belief, v *view) Me {
	s := &b.Self
	k := currentWeapon(b)
	m := Me{HP: hpBucket(s.Health), Health: s.Health, Armor: s.Armor, Weapon: string(k), Ammo: ammoBucket(k, s.Ammo),
		OnGround: s.OnGround, InWater: s.Waterlevel >= 2}
	if k == "" {
		m.Weapon = "none"
	}
	for i := len(b.Damage) - 1; i >= 0; i-- {
		d := &b.Damage[i]
		if b.Time-d.At > 1000 {
			break
		}
		m.DamageLast1s += d.Health + d.Armor
		if d.BearingKnown && m.HitFromBearing == nil {
			r := roundAngle(float64(d.Relative))
			m.HitFromBearing = &r
		}
	}
	return m
}

// isEnemy reports whether a track belongs in the enemies list (the
// objective's monster, objective, however long ago it was observed).
func (p *Projector) isEnemy(b *worldmodel.Belief, t *worldmodel.Track, objective string) bool {
	if t.Kind != perception.KindMonster.String() || t.Life != worldmodel.LifeAlive || !t.LocKnown {
		return false
	}
	return t.Visible || t.ID == objective || b.Time-t.LastUpdate <= p.cfg.EnemyMemory
}

type rankedEnemy struct {
	e      Enemy
	threat float32
	dist   float64
}

func (p *Projector) enemies(b *worldmodel.Belief, v *view, target, objective string) []Enemy {
	var all []rankedEnemy
	for i := range b.Tracks {
		t := &b.Tracks[i]
		if !p.isEnemy(b, t, objective) {
			continue
		}
		var e Enemy
		var d float64
		if t.LocSeen {
			center := Vec3{t.Loc[0] + (t.Mins[0]+t.Maxs[0])/2, t.Loc[1] + (t.Mins[1]+t.Maxs[1])/2, t.Loc[2] + (t.Mins[2]+t.Maxs[2])/2}
			d = distance(center, v.eye)
			lo := Vec3{t.Loc[0] + t.Mins[0], t.Loc[1] + t.Mins[1], t.Loc[2] + t.Mins[2]}
			hi := Vec3{t.Loc[0] + t.Maxs[0], t.Loc[1] + t.Maxs[1], t.Loc[2] + t.Maxs[2]}
			e = Enemy{Bearing: v.bearing(center), Elev: v.elev(center), Dist: distBucket(d), Units: int(math.Round(d)),
				Shootable: t.Shootable, Aim: v.aim(lo, hi, center)}
		} else {
			// placed by ear: a side and a loudness, never a position
			d = distance(t.Loc, v.eye)
			e = Enemy{Bearing: int(45 * math.Round(float64(v.bearing(t.Loc))/45)), Dist: distBucket(d),
				Units: int(50 * math.Round(d/50)), Aim: "off", Heard: t.Ear.Loud.String()}
			if e.Bearing == -180 {
				e.Bearing = 180
			}
		}
		e.ID, e.Class, e.Visible, e.State, e.Wounded = t.ID, t.Class, t.Visible, t.Awareness.String(), t.Wounded
		e.Threat, e.Current, e.Objective = threatBucket(t.Threat), t.ID == target, t.ID == objective
		all = append(all, rankedEnemy{e: e, threat: t.Threat, dist: d})
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, c := &all[i], &all[j]
		if a.threat != c.threat {
			return a.threat > c.threat
		}
		if a.dist != c.dist {
			return a.dist < c.dist
		}
		return a.e.ID < c.e.ID
	})
	out := make([]Enemy, 0, min(len(all), p.cfg.MaxEnemies))
	for i := range all {
		if len(out) == p.cfg.MaxEnemies {
			break
		}
		out = append(out, all[i].e)
	}
	// keep the current target and the objective's monster in the list
	// (in place of the last ones that are neither)
	for _, id := range []string{target, objective} {
		if id == "" || len(all) == len(out) {
			continue
		}
		in := false
		for i := range out {
			in = in || out[i].ID == id
		}
		for i := len(out); !in && i < len(all); i++ {
			if all[i].e.ID != id {
				continue
			}
			for k := len(out) - 1; k >= 0; k-- {
				if !out[k].Current && !out[k].Objective {
					out[k] = all[i].e
					in = true
					break
				}
			}
			break
		}
	}
	return out
}

func (p *Projector) incoming(b *worldmodel.Belief, v *view) []Incoming {
	var ps []*worldmodel.Projectile
	for i := range b.Projectiles {
		pr := &b.Projectiles[i]
		if pr.Own || !(pr.Danger || pr.TCA > 0 && pr.TCA < 2) {
			continue
		}
		ps = append(ps, pr)
	}
	sort.SliceStable(ps, func(i, j int) bool {
		a, c := ps[i], ps[j]
		if a.Danger != c.Danger {
			return a.Danger
		}
		if a.TCA != c.TCA {
			return a.TCA < c.TCA
		}
		return a.ID < c.ID
	})
	var out []Incoming
	for _, pr := range ps {
		if len(out) == p.cfg.MaxIncoming {
			break
		}
		in := Incoming{Kind: pr.Class, Bearing: v.bearing(pr.Pos), ETA: "none", Dodge: "none"}
		switch {
		case pr.Danger && pr.TCA >= 0 && pr.TCA < 0.4:
			in.ETA = "imminent"
		case pr.TCA >= 0 && pr.TCA < 1.5:
			in.ETA = "soon"
		}
		if pr.Danger {
			switch {
			case pr.DodgeSide > 0:
				in.Dodge = "right"
			case pr.DodgeSide < 0:
				in.Dodge = "left"
			}
		}
		out = append(out, in)
	}
	return out
}

// Extend adds the slow lane's parts to a fast state of the same belief.
func (p *Projector) Extend(st *State, b *worldmodel.Belief, cx Context) {
	v := viewOf(b)
	st.Mode = string(cx.Mode)
	own := ownedWeapons(b)
	st.Me.Weapons, st.Me.Empty = nil, nil
	for _, k := range own {
		st.Me.Weapons = append(st.Me.Weapons, string(k))
	}
	for _, k := range ownedEmpty(b) {
		st.Me.Empty = append(st.Me.Empty, string(k))
	}
	st.Items = p.items(b, &v, own)
	if o := cx.Objective; o != nil {
		path := -1
		if o.PathDist >= 0 {
			path = int(math.Round(float64(o.PathDist)))
		}
		st.Objective = &Objective{Kind: o.Kind, Desc: truncate(o.Desc, maxDesc), Bearing: roundAngle(angleDiff(float64(o.Bearing), 0)),
			Path: path, Next: roundAngle(angleDiff(float64(o.NextWaypointBearing), 0)), Stalled: o.Stalled, Exit: o.Exit}
	}
	lv := &LevelInfo{Map: b.Map, DeathsHere: len(b.Memory.DeathSpots)}
	if b.HelpKnown {
		lv.Kills = fmt.Sprintf("%d/%d", b.Help.Kills, b.Help.KillsMax)
		lv.Secrets = fmt.Sprintf("%d/%d", b.Help.Secrets, b.Help.SecretsMax)
	}
	st.Level = lv
	st.Events = p.events(b)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// owns reports whether the bot owns weapon k: the blaster always, the
// weapon in hand, or one the last inventory lists.
func owns(b *worldmodel.Belief, k WeaponKey) bool {
	if k == WeaponBlaster || k == currentWeapon(b) {
		return true
	}
	return b.Inventory.Known && b.Inventory.Count(k.Pickup()) > 0
}

// ammoOf is the ammo count of weapon k (-1: unknown; the blaster needs
// none).
func ammoOf(b *worldmodel.Belief, k WeaponKey) int {
	switch {
	case k == WeaponBlaster:
		return math.MaxInt32
	case k == currentWeapon(b):
		return b.Self.Ammo
	case b.Inventory.Known:
		return b.Inventory.Count(k.AmmoName())
	}
	return -1
}

// ownedWeapons are the owned weapons with ammo for a shot, in inventory
// order. Hand grenades are never offered (throwing needs a hold the
// controller does not do).
func ownedWeapons(b *worldmodel.Belief) []WeaponKey {
	var out []WeaponKey
	for _, k := range Weapons() {
		if k == WeaponGrenades || !owns(b, k) {
			continue
		}
		if a := ammoOf(b, k); a >= k.AmmoPerShot() && a > 0 {
			out = append(out, k)
		}
	}
	return out
}

type rankedItem struct {
	v     ItemView
	score float64
	path  float64
}

func (p *Projector) items(b *worldmodel.Belief, v *view, own []WeaponKey) []ItemView {
	users := append(append([]WeaponKey(nil), own...), ownedEmpty(b)...) // owned weapons that use ammo
	var all []rankedItem
	for i := range b.Items {
		it := &b.Items[i]
		if it.Life != worldmodel.LifeAlive {
			continue
		}
		need := itemNeed(b, it, users)
		if need <= 0 {
			continue
		}
		var path float64
		if p.cfg.Path != nil {
			d, ok := p.cfg.Path(b.Self.Origin, it.Pos)
			if !ok {
				continue
			}
			path = float64(d)
		} else {
			path = distance(b.Self.Origin, it.Pos)
		}
		all = append(all, rankedItem{v: ItemView{ID: it.ID, Class: it.Class, Gives: itemGives(it), Bearing: v.bearing(it.Pos),
			Path: int(math.Round(path))}, score: need / (1 + path/400), path: path})
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, c := &all[i], &all[j]
		if a.score != c.score {
			return a.score > c.score
		}
		if a.path != c.path {
			return a.path < c.path
		}
		return a.v.ID < c.v.ID
	})
	var out []ItemView
	for i := range all {
		if len(out) == p.cfg.MaxItems {
			break
		}
		out = append(out, all[i].v)
	}
	return out
}

func snake(s string) string { return strings.ReplaceAll(strings.ToLower(s), " ", "_") }

func itemGives(it *worldmodel.Item) string {
	switch it.Kind {
	case "health":
		return fmt.Sprintf("health+%d", it.Amount)
	case "armor":
		if it.Amount == 0 {
			return snake(it.Pickup)
		}
		return fmt.Sprintf("armor+%d", it.Amount)
	case "ammo":
		return fmt.Sprintf("%s+%d", snake(it.Pickup), it.Amount)
	case "weapon":
		if k := WeaponFromPickup(it.Pickup); k != "" {
			return "weapon:" + string(k)
		}
		return "weapon:" + snake(it.Pickup)
	case "key":
		return "key:" + snake(it.Pickup)
	}
	return snake(it.Pickup)
}

// itemNeed is how much the bot wants an item now (0: not at all); users
// are the owned weapons (with or without ammo).
func itemNeed(b *worldmodel.Belief, it *worldmodel.Item, users []WeaponKey) float64 {
	s := &b.Self
	switch it.Kind {
	case "health":
		switch {
		case it.Class == "item_health_mega":
			return 1
		case s.Health < 100:
			return 0.2 + 0.8*float64(100-s.Health)/100
		}
		return 0
	case "armor":
		if it.Amount > 0 && it.Amount <= 2 {
			return 0.2
		}
		if s.Armor < 100 {
			return 0.5
		}
		return 0.15
	case "ammo":
		best := 0.0
		for _, k := range users {
			if k.AmmoName() != it.Pickup {
				continue
			}
			if a := ammoOf(b, k); a < 2*k.LowAmmo() {
				best = math.Max(best, 0.6)
			} else {
				best = math.Max(best, 0.2)
			}
		}
		return best
	case "weapon":
		k := WeaponFromPickup(it.Pickup)
		if k == "" {
			return 0
		}
		if !owns(b, k) {
			return 0.9
		}
		return 0.15
	case "powerup":
		return 0.5
	case "key":
		return 1
	}
	return 0
}

// ownedEmpty are owned weapons without ammo (their ammo is needed most).
func ownedEmpty(b *worldmodel.Belief) []WeaponKey {
	var out []WeaponKey
	for _, k := range Weapons() {
		if k == WeaponBlaster || k == WeaponGrenades || !owns(b, k) {
			continue
		}
		if a := ammoOf(b, k); a >= 0 && a < k.AmmoPerShot() {
			out = append(out, k)
		}
	}
	return out
}

type event struct {
	at   int64
	text string
}

// events are the recent events, newest first.
func (p *Projector) events(b *worldmodel.Belief) []string {
	var evs []event
	recent := func(at int64) bool { return at > 0 && b.Time-at <= p.cfg.EventWindow && at <= b.Time }
	for i := range b.Damage {
		d := &b.Damage[i]
		if !recent(d.At) {
			continue
		}
		t := fmt.Sprintf("took %d damage", d.Health+d.Armor)
		if d.Source != "" {
			t += " from " + d.Source
		} else if d.Cause != "hit" && d.Cause != "" {
			t += " (" + d.Cause + ")"
		}
		evs = append(evs, event{d.At, t})
	}
	for i := range b.Effects {
		e := &b.Effects[i]
		if !recent(e.At) {
			continue
		}
		var t string
		switch e.Kind {
		case worldmodel.EffectMonsterDead:
			t = e.Ref + " died"
		case worldmodel.EffectLaserOff:
			t = "laser off"
		case worldmodel.EffectLaserOn:
			t = "laser on"
		case worldmodel.EffectMoverMoved:
			t = e.Ref + " moved"
		case worldmodel.EffectItemTaken:
			t = e.Ref + " taken"
		default:
			t = string(e.Kind)
		}
		evs = append(evs, event{e.At, t})
	}
	if s := &b.Self; s.Pickup != "" && recent(s.PickupAt) {
		evs = append(evs, event{s.PickupAt, "picked up " + s.Pickup})
	}
	for i := range b.Tracks {
		t := &b.Tracks[i]
		if t.Kind == perception.KindMonster.String() && recent(t.FirstSeen) {
			evs = append(evs, event{t.FirstSeen, "spotted " + t.ID + " " + t.Class})
		}
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].at != evs[j].at {
			return evs[i].at > evs[j].at
		}
		return evs[i].text < evs[j].text
	})
	var out []string
	for _, e := range evs {
		if len(out) == p.cfg.MaxEvents {
			break
		}
		out = append(out, fmt.Sprintf("%s %.1fs ago", e.text, float64(b.Time-e.at)/1000))
	}
	return out
}

// marshal encodes v without HTML escaping.
func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte{'\n'}), nil
}

// FitState returns st trimmed to encode within maxBytes (HardCap if <= 0)
// and its encoding. Trimming is deterministic and drops the least
// important parts first: events, items, incoming projectiles, enemies
// (the lowest ranked first, never the current target, down to one), then
// the objective's description. st is not modified.
func FitState(st State, maxBytes int) (State, []byte, error) {
	if maxBytes <= 0 {
		maxBytes = HardCap
	}
	if st.Enemies == nil {
		st.Enemies = []Enemy{}
	}
	cp := func() {
		st.Events = append([]string(nil), st.Events...)
		st.Items = append([]ItemView(nil), st.Items...)
		st.Incoming = append([]Incoming(nil), st.Incoming...)
		st.Enemies = append([]Enemy{}, st.Enemies...)
		if st.Objective != nil {
			o := *st.Objective
			st.Objective = &o
		}
	}
	copied := false
	for {
		raw, err := marshal(&st)
		if err != nil {
			return st, nil, err
		}
		if len(raw) <= maxBytes {
			return st, raw, nil
		}
		if !copied {
			cp()
			copied = true
		}
		switch {
		case len(st.Events) > 0:
			st.Events = st.Events[:len(st.Events)-1]
		case len(st.Items) > 0:
			st.Items = st.Items[:len(st.Items)-1]
		case len(st.Incoming) > 0:
			st.Incoming = st.Incoming[:len(st.Incoming)-1]
		case len(st.Enemies) > 1:
			st.Enemies = dropEnemy(st.Enemies)
		case st.Objective != nil && st.Objective.Desc != "":
			st.Objective.Desc = ""
		default:
			return st, nil, fmt.Errorf("%w: %d > %d bytes", ErrStateTooLarge, len(raw), maxBytes)
		}
		if len(st.Events) == 0 {
			st.Events = nil
		}
		if len(st.Items) == 0 {
			st.Items = nil
		}
		if len(st.Incoming) == 0 {
			st.Incoming = nil
		}
	}
}

// dropEnemy removes the last enemy that is neither the current target nor
// the objective's monster (the projection puts a low-ranked one of those
// in place of the last of the top N), keeping the order of the rest. es is
// modified in place.
func dropEnemy(es []Enemy) []Enemy {
	i := len(es) - 1
	for i > 0 && (es[i].Current || es[i].Objective) {
		i--
	}
	return append(es[:i], es[i+1:]...)
}

// EncodeState returns the encoding of st trimmed under maxBytes (see
// FitState).
func EncodeState(st *State, maxBytes int) (json.RawMessage, error) {
	_, raw, err := FitState(*st, maxBytes)
	return raw, err
}
