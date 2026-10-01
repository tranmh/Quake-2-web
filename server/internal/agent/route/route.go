// Package route holds the per-visit subgoal tables the agent follows
// through the campaign (fixtures/agent/routes): their JSON schema, the
// loader, visit selection and the validation against the map data
// (Validate, ValidateCampaign).
//
// A table describes one visit of one map: the spawnpoint it arrives at, the
// target_changelevel it must leave through and the ordered steps in between
// (go somewhere, touch a trigger, press a button, kill the monster that
// guards something, pick up a key, ...), plus the activators to avoid
// because they lead to another exit. Steps name entities through Refs that
// Validate resolves against the entity lump.
package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SchemaVersion is the "schema" value of tables and campaign files.
const SchemaVersion = 1

// Op is a step operation.
type Op string

// Step operations.
const (
	// OpGoto: walk to Pos, or to (onto, for a mover) Target.
	OpGoto Op = "goto"
	// OpTouch: walk into the trigger volume Target (facing Yaw when the
	// trigger is directional).
	OpTouch Op = "touch"
	// OpPress: touch the func_button Target (the game never reads
	// BUTTON_USE: buttons are pressed by walking into them).
	OpPress Op = "press"
	// OpShoot: damage Target (a button, door or func_explosive with health).
	OpShoot Op = "shoot"
	// OpRide: stand on the mover Target until it reaches pose Until.
	OpRide Op = "ride"
	// OpWait: wait Seconds, or until the Effects (caused by earlier steps)
	// are observed; with both, Seconds is the expected wait after which the
	// step is overdue.
	OpWait Op = "wait"
	// OpFace: turn to Yaw degrees.
	OpFace Op = "face"
	// OpKill: kill the monster of classname Class spawned at Pos.
	OpKill Op = "kill"
	// OpPickup: pick up an item of classname Class (at Pos when several).
	OpPickup Op = "pickup"
	// OpConfirm: check that Effects claimed by earlier steps happened (a
	// guard before a step that depends on them).
	OpConfirm Op = "confirm"
)

// Vec is a JSON [x, y, z].
type Vec [3]float32

// Ref names an entity of the map. At least one of Entity, Model or
// Targetname must be given; every given field must match the entity.
type Ref struct {
	// Entity is the entity lump index (0 is worldspawn).
	Entity *int `json:"entity,omitempty"`
	// Model is the inline model of a brush entity ("*34").
	Model      string `json:"model,omitempty"`
	Classname  string `json:"classname,omitempty"`
	Targetname string `json:"targetname,omitempty"`
}

// String renders the reference for messages.
func (r Ref) String() string {
	var parts []string
	if r.Entity != nil {
		parts = append(parts, fmt.Sprintf("#%d", *r.Entity))
	}
	for _, s := range []string{r.Classname, r.Model} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if r.Targetname != "" {
		parts = append(parts, "("+r.Targetname+")")
	}
	if len(parts) == 0 {
		return "{}"
	}
	return strings.Join(parts, " ")
}

// EffectKind is an observable consequence of a step.
type EffectKind string

// Effect kinds.
const (
	// EffExit: the target_changelevel Target fires (the level ends).
	EffExit EffectKind = "exit"
	// EffLaserOff / EffLaserOn: the target_laser Target is toggled off / on.
	EffLaserOff EffectKind = "laserOff"
	EffLaserOn  EffectKind = "laserOn"
	// EffDoorOpen: the door Target (or its team) opens. Not for a
	// START_OPEN door: using one closes it (claim EffMoverAt "pos2").
	EffDoorOpen EffectKind = "doorOpen"
	// EffMoverAt: the use moves the mover Target to Pose ("pos2" by
	// default). Pose must be where that use sends it from its spawn state:
	// pos2 for doors, buttons and plats (Use_Plat sends a plat down), pos1
	// or pos2 for a secret door, a corner of the run for a train.
	EffMoverAt EffectKind = "moverAt"
	// EffEnable: the TRIGGERED trigger (or trigger-spawned wall or
	// monster) Target becomes active. Only a use does this: a mover
	// carrying the rider into a disabled trigger does not.
	EffEnable EffectKind = "enable"
	// EffRemove: Target is removed (a killtarget).
	EffRemove EffectKind = "remove"
	// EffWake: the monster Target wakes up (or trigger-spawns).
	EffWake EffectKind = "wake"
	// EffUse: Target is used in any way.
	EffUse EffectKind = "use"
)

// Effect is a claimed consequence: Validate checks that the step's logic
// chain reaches it.
type Effect struct {
	Kind   EffectKind `json:"kind"`
	Target Ref        `json:"target"`
	// Pose is the pose of an EffMoverAt: "pos1", "pos2", "top", "bottom"
	// or a func_train corner targetname.
	Pose string `json:"pose,omitempty"`
}

// Step is one subgoal. Which fields apply depends on Op.
type Step struct {
	Op     Op   `json:"op"`
	Target *Ref `json:"target,omitempty"`
	// Pos is a goto destination, or the spawn origin of a kill / pickup.
	Pos *Vec `json:"pos,omitempty"`
	// Radius is the arrival tolerance of a goto (units; 0 = default).
	Radius float32 `json:"radius,omitempty"`
	// Class is the classname of a kill / pickup.
	Class string `json:"class,omitempty"`
	// Until is the pose a ride ends at.
	Until string `json:"until,omitempty"`
	// Seconds is the duration of a wait.
	Seconds float32 `json:"seconds,omitempty"`
	// Yaw is the facing of a face step, or of a touch of a directional
	// trigger.
	Yaw *float32 `json:"yaw,omitempty"`
	// Effects are the consequences the step claims (press / touch / shoot
	// must claim at least one), or waits for (wait, confirm).
	Effects []Effect `json:"effects,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Avoid is an activator the agent must not set off.
type Avoid struct {
	Target Ref    `json:"target"`
	Why    string `json:"why"`
}

// ExitRef names the exit a table leaves through by its map string.
type ExitRef struct {
	// Map is the target_changelevel "map" value, e.g. "demo2$base1".
	Map string `json:"map"`
}

// Table is the subgoal table of one visit of one map.
type Table struct {
	Schema int `json:"schema"`
	// Name identifies the table (the file stem, e.g. "demo2a").
	Name string `json:"name"`
	Map  string `json:"map"`
	// Visit is the 0-based number of earlier visits of Map in the campaign.
	Visit int    `json:"visit"`
	Title string `json:"title,omitempty"`
	// From is the arrival spawnpoint ("" for a plain "map" start).
	From  string  `json:"from"`
	Exit  ExitRef `json:"exit"`
	Steps []Step  `json:"steps"`
	Avoid []Avoid `json:"avoid,omitempty"`
}

// Terminal is how the campaign ends.
type Terminal struct {
	// Exit is the map string of the last changelevel ("victory.pcx").
	Exit string `json:"exit"`
	// Kind is what that level string spawns: "pic", "cin", "demo" or
	// "level" (mapdata.ExitKind names).
	Kind string `json:"kind"`
}

// Campaign is the ordered list of visits.
type Campaign struct {
	Schema int    `json:"schema"`
	Name   string `json:"name"`
	// Skill is the skill level the tables are written (and validated) for.
	Skill int `json:"skill"`
	// Start is the first map, entered with a plain "map" command.
	Start string `json:"start"`
	// Visits lists the table files in order, relative to the campaign file.
	Visits   []string `json:"visits"`
	Terminal Terminal `json:"terminal"`

	// Tables are the loaded visit tables, parallel to Visits.
	Tables []*Table `json:"-"`
}

// CampaignFile is the campaign file name inside a routes directory.
const CampaignFile = "campaign.json"

// Load reads dir/campaign.json and every table it lists. Unknown JSON
// fields are errors, so a misspelt key cannot silently drop a step field.
func Load(dir string) (*Campaign, error) {
	var c Campaign
	if err := decodeFile(filepath.Join(dir, CampaignFile), &c); err != nil {
		return nil, err
	}
	if c.Schema != SchemaVersion {
		return nil, fmt.Errorf("%s: schema %d, want %d", CampaignFile, c.Schema, SchemaVersion)
	}
	for _, v := range c.Visits {
		t, err := LoadTable(filepath.Join(dir, v))
		if err != nil {
			return nil, err
		}
		c.Tables = append(c.Tables, t)
	}
	return &c, nil
}

// LoadTable reads one table file (see ParseTable).
func LoadTable(file string) (*Table, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	t, err := ParseTable(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	return t, nil
}

// ParseTable decodes a table from JSON. Unknown fields, trailing data and
// a schema other than SchemaVersion are errors.
func ParseTable(data []byte) (*Table, error) {
	var t Table
	if err := decodeStrict(data, &t); err != nil {
		return nil, err
	}
	if t.Schema != SchemaVersion {
		return nil, fmt.Errorf("schema %d, want %d", t.Schema, SchemaVersion)
	}
	return &t, nil
}

func decodeFile(file string, v any) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if err := decodeStrict(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(file), err)
	}
	return nil
}

func decodeStrict(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

// Select returns the table of the visit-th (0-based) arrival at mapName.
func (c *Campaign) Select(mapName string, visit int) (*Table, error) {
	for _, t := range c.Tables {
		if strings.EqualFold(t.Map, mapName) && t.Visit == visit {
			return t, nil
		}
	}
	return nil, fmt.Errorf("route: no table for visit %d of %s", visit, mapName)
}

// Index returns the position of the table in the campaign order, or -1.
func (c *Campaign) Index(t *Table) int {
	for i, x := range c.Tables {
		if x == t {
			return i
		}
	}
	return -1
}
