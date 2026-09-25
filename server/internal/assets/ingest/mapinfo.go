package ingest

import (
	"path"
	"strings"

	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/qcommon/md4"
	"quake2web/server/internal/qcommon/shared"
)

// skySuffixes is gl_warp.c suf[] (3dstudio environment map names).
var skySuffixes = [6]string{"rt", "bk", "lf", "ft", "up", "dn"}

// Entity is one parsed entity of the entity string (key order kept).
type Entity struct {
	Keys   []string
	Values []string
}

// Get returns the last value of key (ED_ParseEdict assigns in order, so the
// last duplicate wins) or "".
func (e *Entity) Get(key string) string {
	v := ""
	for i, k := range e.Keys {
		if k == key {
			v = e.Values[i]
		}
	}
	return v
}

// ParseEntities splits an entity string with COM_Parse the way
// ED_ParseEdict walks it. Parsing stops at the first syntax error, returning
// the entities read so far.
// C: game/g_spawn.c:497 ED_ParseEdict, :626 SpawnEntities
func ParseEntities(s string) []Entity {
	var out []Entity
	data := s
	for {
		tok, rest, more := shared.COM_Parse(data)
		if !more {
			return out
		}
		if tok != "{" {
			return out // "ED_LoadFromFile: found %s when expecting {"
		}
		data = rest
		var ent Entity
		for {
			key, rest, more := shared.COM_Parse(data)
			if !more {
				return out // "ED_ParseEntity: EOF without closing brace"
			}
			if key == "}" {
				data = rest
				break
			}
			val, rest2, more := shared.COM_Parse(rest)
			if !more {
				return out
			}
			if val == "}" {
				return out // "ED_ParseEntity: closing brace without data"
			}
			ent.Keys = append(ent.Keys, key)
			ent.Values = append(ent.Values, val)
			data = rest2
		}
		out = append(out, ent)
	}
}

type strset struct {
	seen map[string]bool
	list []string
}

func (s *strset) add(v string) {
	if v == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[v] {
		return
	}
	s.seen[v] = true
	s.list = append(s.list, v)
}

func (s *strset) slice() []string {
	if s.list == nil {
		return []string{}
	}
	return s.list
}

// MapInfo summarizes a BSP: CM_LoadMap checksum (Com_BlockChecksum of the
// whole file), worldspawn keys, texinfo texture paths and entity-referenced
// models/sounds. Errors are recorded in MapInfo.Error.
func MapInfo(name string, raw []byte) *manifest.MapInfo {
	base := path.Base(name)
	mi := &manifest.MapInfo{
		Name:       strings.TrimSuffix(base, path.Ext(base)),
		Path:       name,
		Checksum:   md4.Com_BlockChecksum(raw),
		Textures:   []string{},
		Models:     []string{},
		Sounds:     []string{},
		Classnames: []string{},
		SkyImages:  []string{},
	}
	f, err := bsp.Parse(raw)
	if err != nil {
		mi.Error = err.Error()
		return mi
	}
	m, err := cmodel.LoadMap(name, f, raw)
	if err != nil {
		mi.Error = err.Error()
	} else {
		mi.Checksum = m.Checksum
		mi.NumInlineModels = m.NumInlineModels()
	}
	mi.NumTexInfo = len(f.TexInfo)
	var tex strset
	for i := range f.TexInfo {
		// C: ref_gl/gl_model.c:472 Mod_LoadTexinfo "textures/%s.wal"
		tex.add("textures/" + f.TexInfo[i].TextureName() + ".wal")
	}
	mi.Textures = tex.slice()

	ents := ParseEntities(f.EntityString())
	var models, sounds, classes strset
	for i := range ents {
		e := &ents[i]
		cls := e.Get("classname")
		classes.add(cls)
		if i == 0 {
			mi.Message = e.Get("message")
			mi.Sky = e.Get("sky")
			mi.SkyRotate = e.Get("skyrotate")
			mi.SkyAxis = e.Get("skyaxis")
			mi.CDTrack = e.Get("sounds")
		}
		if mdl := e.Get("model"); mdl != "" && !strings.HasPrefix(mdl, "*") {
			models.add(mdl)
		}
		if noise := e.Get("noise"); noise != "" {
			// C: game/g_target.c SP_target_speaker appends ".wav" when missing;
			// client/snd_dma.c S_RegisterSound prefixes "sound/" (not for '#').
			if !strings.Contains(noise, ".wav") {
				noise += ".wav"
			}
			if strings.HasPrefix(noise, "#") {
				sounds.add(noise[1:])
			} else {
				sounds.add("sound/" + noise)
			}
		}
	}
	// C: game/g_spawn.c SP_worldspawn: sky defaults to "unit1_"
	if mi.Sky == "" {
		mi.Sky = "unit1_"
	}
	for _, s := range skySuffixes {
		mi.SkyImages = append(mi.SkyImages, "env/"+mi.Sky+s+".tga")
	}
	mi.Models = models.slice()
	mi.Sounds = sounds.slice()
	mi.Classnames = classes.slice()
	return mi
}
