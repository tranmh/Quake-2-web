package campaign

import (
	"os"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
)

func TestZZProbe(t *testing.T) {
	if os.Getenv("ZZ_PROBE") == "" {
		t.Skip()
	}
	lib := demoLibrary(t)
	pol := os.Getenv("ZZ_PROBE")
	res, _ := runEpisode(t, episodeSpec{lib: lib, budget: 30 * time.Minute, config: func(c *Config) {
		c.EntryCommands = nil
		c.MaxDeaths = 20
		c.Logf = t.Logf
		if pol == "pipe" {
			p, err := decide.NewPipeline(decide.PipelineConfig{Backend: scripted.New(scripted.Config{Seed: 1}),
				Scheduler: decide.SchedulerConfig{Mode: decide.Lockstep}, Arbiter: decide.ArbiterConfig{AnswerSource: decide.SourceScripted}})
			if err != nil {
				t.Fatal(err)
			}
			c.Bot.Policy = p
		}
	}})
	t.Logf("result %s %s victory %v deaths %d\n%s", res.Outcome, res.Reason, res.Victory, res.Deaths, res.Diagnostics)
}
