package host

import (
	"encoding/json"
	"errors"

	"quake2web/server/internal/db"
	"quake2web/server/internal/sv"
)

// AutosaveSlot is the account save slot holding the server's autosave
// (sv's "save0", written on every level change and when a single player
// game is left).
const AutosaveSlot = "autosave"

// InstanceFile is the extra file stored in every account save bundle
// describing where the save came from (see SaveOrigin).
const InstanceFile = "q2web.json"

// SaveOrigin is the content of InstanceFile.
type SaveOrigin struct {
	Pakset string `json:"pakset"`
	Mode   string `json:"mode"`
}

// ServerSlot maps an account slot name to the server's slot name
// ("autosave" → "save0").
func ServerSlot(slot string) string {
	if slot == AutosaveSlot {
		return "save0"
	}
	return slot
}

// AccountSlot maps a server slot name to the account slot name
// ("save0" → "autosave").
func AccountSlot(slot string) string {
	if slot == "save0" {
		return AutosaveSlot
	}
	return slot
}

// accountSaves is the sv.SaveStore of an instance owned by an account: the
// scratch slot "current" stays in memory, every other slot is a row of the
// owner's saves (db.SlotFiles bundles).
type accountSaves struct {
	mem    *sv.MemSaveStore
	files  *db.SlotFiles
	origin []byte
}

func newAccountSaves(store db.SaveStore, userID int64, origin SaveOrigin) *accountSaves {
	o, _ := json.Marshal(origin)
	a := &accountSaves{mem: sv.NewMemSaveStore(), origin: o}
	a.files = &db.SlotFiles{
		Store:    store,
		UserID:   userID,
		NotFound: sv.ErrNoSave,
		Meta: func(slot string, files map[string][]byte) db.SaveMeta {
			m := db.SaveMeta{Mode: origin.Mode, Schema: sv.SaveVersion}
			var ss struct {
				Comment string `json:"comment"`
				MapCmd  string `json:"mapcmd"`
			}
			if b, ok := files["server.ssv"]; ok && json.Unmarshal(b, &ss) == nil {
				m.Comment, m.MapCmd = ss.Comment, ss.MapCmd
			}
			return m
		},
	}
	return a
}

func (a *accountSaves) Read(slot, name string) ([]byte, error) {
	if slot == "current" {
		return a.mem.Read(slot, name)
	}
	b, err := a.files.Read(AccountSlot(slot), name)
	if errors.Is(err, db.ErrNotFound) {
		return nil, sv.ErrNoSave
	}
	return b, err
}

func (a *accountSaves) Write(slot, name string, data []byte) error {
	if slot == "current" {
		return a.mem.Write(slot, name, data)
	}
	if name == "server.ssv" {
		if err := a.files.Write(AccountSlot(slot), InstanceFile, a.origin); err != nil {
			return err
		}
	}
	return a.files.Write(AccountSlot(slot), name, data)
}

func (a *accountSaves) Remove(slot, name string) error {
	if slot == "current" {
		return a.mem.Remove(slot, name)
	}
	return a.files.Remove(AccountSlot(slot), name)
}

func (a *accountSaves) List(slot string) ([]string, error) {
	if slot == "current" {
		return a.mem.List(slot)
	}
	return a.files.List(AccountSlot(slot))
}

var _ sv.SaveStore = (*accountSaves)(nil)
