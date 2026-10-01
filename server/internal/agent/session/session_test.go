package session_test

import (
	"errors"
	"testing"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/api"
)

func TestSpecServerSettings(t *testing.T) {
	ded, cvars, err := session.Spec{Skill: 2, Cvars: map[string]string{"password": "x"}}.ServerSettings()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, kv := range cvars {
		got[kv[0]] = kv[1]
	}
	if ded || got["deathmatch"] != "0" || got["coop"] != "0" || got["maxclients"] != "1" ||
		got["skill"] != "2" || got["password"] != "x" {
		t.Fatalf("dedicated %v cvars %v", ded, cvars)
	}
	gs, err := session.Spec{}.GameSpec()
	if err != nil || gs.Mode != "sp" || gs.Map != "demo1" || gs.Cvars["skill"] != "0" {
		t.Fatalf("default spec %+v %v", gs, err)
	}
	for name, spec := range map[string]session.Spec{
		"gravity":     {Cvars: map[string]string{"sv_gravity": "200"}},
		"airaccel":    {Cvars: map[string]string{"SV_AIRACCELERATE": "10"}},
		"maxvelocity": {Cvars: map[string]string{"sv_maxvelocity": "9000"}},
		"bob":         {Cvars: map[string]string{"bob_up": "0"}},
		"skill cvar":  {Cvars: map[string]string{"skill": "3"}},
		"skill range": {Skill: 4},
		"map inject":  {Map: "demo1;quit"},
		"not allowed": {Cvars: map[string]string{"rcon_password": "x"}}, // host.ModeSettings allowlist
	} {
		if _, _, err := spec.ServerSettings(); !errors.Is(err, api.ErrGameInvalid) {
			t.Errorf("%s: %v, want ErrGameInvalid", name, err)
		}
	}
}
