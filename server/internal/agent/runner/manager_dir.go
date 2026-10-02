package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/api"
)

// runDir is the directory of run id.
func (m *Manager) runDir(id string) string { return filepath.Join(m.cfg.Dir, id) }

// endedRun is a run directory without a live bot: an ended bot of the
// Manager (bot.json) or a q2bot command line run (run.json only; it may
// still be going).
type endedRun struct {
	id   string
	dir  string
	meta botMeta
	cli  bool // no bot.json: a command line run
}

// cliRunName is the name of a run the command line wrote.
const cliRunName = "q2bot run"

// cliIdle is how long a command line run whose run.json is a progress
// snapshot (outcome incomplete) may write nothing before it counts as
// dead: a live run flushes its trace every few seconds.
const cliIdle = 5 * time.Minute

// readRun reads the run directory id (false: none, or not a run).
func (m *Manager) readRun(id string) (endedRun, bool) {
	if !ValidRunID(id) {
		return endedRun{}, false
	}
	dir := m.runDir(id)
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return endedRun{}, false
	}
	r := endedRun{id: id, dir: dir}
	var meta botMeta
	if readJSONFile(filepath.Join(dir, BotFile), &meta) == nil && meta.ID == id {
		r.meta = meta
		return r, true
	}
	// a q2bot command line run: public, without an owner
	var s metrics.RunSummary
	runFile := filepath.Join(dir, RunFile)
	if readJSONFile(runFile, &s) != nil || s.Schema != metrics.Schema {
		return endedRun{}, false
	}
	r.cli = true
	r.meta = botMeta{Schema: BotSchema, ID: id, Name: cliRunName, Public: true, Backend: s.Backend, Model: s.Model,
		Maps: s.Maps, Skill: s.Skill, Reason: s.Reason}
	switch s.Outcome {
	case OutcomeCompleted:
		r.meta.Status = api.BotFinished
	case OutcomeAborted:
		r.meta.Status = api.BotStopped
	case OutcomeFailed:
		r.meta.Status = api.BotFailed
	case metrics.OutcomeIncomplete:
		// a progress snapshot (written after each episode): still going
		// unless the run stopped writing
		if m.cfg.Now().Sub(lastWrite(dir)) < cliIdle {
			r.meta.Status = api.BotRunning
			break
		}
		fallthrough
	default:
		r.meta.Status, r.meta.Reason = api.BotFailed, "the run did not end"
	}
	if t, err := time.Parse(time.RFC3339, s.Started); err == nil {
		r.meta.StartedAt = t.UTC()
	} else {
		r.meta.StartedAt = st.ModTime().UTC()
	}
	if api.BotLive(r.meta.Status) {
		return r, true
	}
	end := r.meta.StartedAt.Add(time.Duration(s.WallMs) * time.Millisecond)
	if s.WallMs == 0 {
		if fi, err := os.Stat(runFile); err == nil {
			end = fi.ModTime().UTC()
		}
	}
	r.meta.EndedAt = &end
	return r, true
}

// lastWrite is when a command line run directory was last written: its
// run.json, or an episode's trace or log.
func lastWrite(dir string) time.Time {
	var last time.Time
	see := func(path string) {
		if fi, err := os.Lstat(path); err == nil && fi.ModTime().After(last) {
			last = fi.ModTime()
		}
	}
	see(filepath.Join(dir, RunFile))
	eps, _ := os.ReadDir(dir)
	for _, ep := range eps {
		if ep.IsDir() && strings.HasPrefix(ep.Name(), "ep-") {
			see(filepath.Join(dir, ep.Name(), TraceFile))
			see(filepath.Join(dir, ep.Name(), LogFile))
		}
	}
	return last
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// scanRuns returns the run directories, newest first.
func (m *Manager) scanRuns() ([]endedRun, error) {
	ents, err := os.ReadDir(m.cfg.Dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []endedRun
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if r, ok := m.readRun(e.Name()); ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].meta.StartedAt.Equal(out[j].meta.StartedAt) {
			return out[i].meta.StartedAt.After(out[j].meta.StartedAt)
		}
		return out[i].id > out[j].id
	})
	return out, nil
}

// endedInfo describes a run without a live bot (a command line run still
// going lists only its complete artifacts, like a live bot).
func (m *Manager) endedInfo(r endedRun, withSummary bool) api.BotInfo {
	info := infoOf(r.meta)
	info.Artifacts = artifacts(r.dir, api.BotLive(r.meta.Status))
	if withSummary {
		if raw, err := os.ReadFile(filepath.Join(r.dir, RunFile)); err == nil && json.Valid(raw) {
			info.Summary = raw
		}
	}
	return info
}

// recoverInterrupted marks the bots a previous process left live as
// failed (it stopped before their runs ended).
func (m *Manager) recoverInterrupted() {
	runs, err := m.scanRuns()
	if err != nil {
		m.log.Warn("bots: runs directory unreadable", "dir", m.cfg.Dir, "err", err)
		return
	}
	for _, r := range runs {
		if !api.BotLive(r.meta.Status) || r.cli {
			continue
		}
		meta := r.meta
		end := m.cfg.Now().UTC()
		if fi, err := os.Stat(filepath.Join(r.dir, BotFile)); err == nil {
			end = fi.ModTime().UTC()
		}
		meta.Status, meta.Reason, meta.EndedAt = api.BotFailed, "interrupted: the server stopped during the run", &end
		if err := metrics.WriteJSON(filepath.Join(r.dir, BotFile), meta); err != nil {
			m.log.Warn("bots: bot.json not updated", "bot", r.id, "err", err)
		}
	}
}

// prune deletes the ended runs beyond Keep, and an account's beyond
// KeepPerUser, oldest first. The runs of administrators, of the server and
// of the command line are only ever evicted by newer ones of theirs: the
// other accounts' runs are kept in the room Keep leaves them, so that no
// number of accounts starting bots can delete them. Live bots (a bot being
// created, a command line run still going) are never touched.
func (m *Manager) prune() {
	if m.cfg.Keep <= 0 {
		return
	}
	m.pruneMu.Lock()
	defer m.pruneMu.Unlock()
	runs, err := m.scanRuns()
	if err != nil {
		return
	}
	ended := runs[:0]
	privileged := 0
	for _, r := range runs {
		if api.BotLive(r.meta.Status) || m.live(r.id) != nil {
			continue
		}
		ended = append(ended, r)
		if !userRun(r.meta) {
			privileged++
		}
	}
	userRoom := m.cfg.Keep - min(privileged, m.cfg.Keep)
	keptPrivileged, keptUser, mine := 0, 0, map[int64]int{}
	for _, r := range ended {
		if !userRun(r.meta) {
			if keptPrivileged < m.cfg.Keep {
				keptPrivileged++
				continue
			}
		} else if keptUser < userRoom && mine[r.meta.OwnerID] < m.cfg.KeepPerUser {
			keptUser++
			mine[r.meta.OwnerID]++
			continue
		}
		if filepath.Dir(r.dir) != filepath.Clean(m.cfg.Dir) || !ValidRunID(filepath.Base(r.dir)) {
			continue
		}
		if err := os.RemoveAll(r.dir); err != nil {
			m.log.Warn("bots: old run not removed", "bot", r.id, "err", err)
		} else {
			m.log.Info("bots: old run removed", "bot", r.id)
		}
	}
}

// userRun reports whether a run is an account's that is not an
// administrator's (it has a share of the retention: KeepPerUser, within
// the room the other runs leave).
func userRun(meta botMeta) bool { return meta.OwnerID != 0 && !meta.OwnerAdmin }

// artifactName is the allowlist of the files a run directory serves.
var artifactName = regexp.MustCompile(`^(run\.json|ep-[0-9]{3,4}/(episode\.json|trace\.jsonl\.gz|demos/[0-9]{2,4}-[A-Za-z0-9_-][A-Za-z0-9._-]{0,63}\.dm2))$`)

// artifactKind returns an allowlisted name's kind ("" for another name).
func artifactKind(name string) string {
	if !artifactName.MatchString(name) {
		return ""
	}
	switch {
	case name == RunFile:
		return api.ArtifactRun
	case strings.HasSuffix(name, "/"+EpisodeFile):
		return api.ArtifactEpisode
	case strings.HasSuffix(name, "/"+TraceFile):
		return api.ArtifactTrace
	}
	return api.ArtifactDemo
}

// artifacts lists a run directory's allowlisted regular files. A live
// run's are those already complete: no trace (it is still being
// written) and not each episode's newest demo (the recorder may still be
// writing it).
func artifacts(dir string, live bool) []api.BotArtifactInfo {
	out := []api.BotArtifactInfo{}
	add := func(name string) {
		if artifactKind(name) == "" {
			return
		}
		fi, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || !fi.Mode().IsRegular() {
			return
		}
		out = append(out, api.BotArtifactInfo{Name: name, Size: fi.Size(), Kind: artifactKind(name)})
	}
	add(RunFile)
	eps, _ := os.ReadDir(dir)
	for _, ep := range eps {
		if !ep.IsDir() || !strings.HasPrefix(ep.Name(), "ep-") {
			continue
		}
		add(ep.Name() + "/" + EpisodeFile)
		if !live {
			add(ep.Name() + "/" + TraceFile)
		}
		demos, _ := os.ReadDir(filepath.Join(dir, ep.Name(), DemoDir))
		var names []string
		for _, d := range demos {
			if strings.HasSuffix(d.Name(), ".dm2") {
				names = append(names, d.Name())
			}
		}
		sort.Strings(names)
		if live && len(names) > 0 {
			names = names[:len(names)-1]
		}
		for _, n := range names {
			add(ep.Name() + "/" + DemoDir + "/" + n)
		}
	}
	return out
}

// OpenArtifact implements api.BotHost: an allowlisted regular file of the
// run directory (no symbolic link on the way); a live run's (or a command
// line run's still going) only once complete (see artifacts).
func (m *Manager) OpenArtifact(_ context.Context, id, name string, u api.BotUser) (*api.BotArtifact, error) {
	kind := artifactKind(name)
	if kind == "" {
		return nil, api.ErrBotNotFound
	}
	var dir string
	live := false
	if b := m.live(id); b != nil {
		meta := b.snapshot()
		if !visible(meta.OwnerID, meta.Public, u) {
			return nil, api.ErrBotNotFound
		}
		dir, live = b.dir, true
	} else {
		r, ok := m.readRun(id)
		if !ok || !visible(r.meta.OwnerID, r.meta.Public, u) {
			return nil, api.ErrBotNotFound
		}
		dir, live = r.dir, api.BotLive(r.meta.Status) // a command line run still going
	}
	if live {
		ok := false
		for _, a := range artifacts(dir, true) {
			ok = ok || a.Name == name
		}
		if !ok {
			return nil, api.ErrBotNotFound
		}
	}
	path := filepath.Join(dir, filepath.FromSlash(name))
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, api.ErrBotNotFound
	}
	if real, err := filepath.EvalSymlinks(path); err != nil || real != filepath.Join(realDir, filepath.FromSlash(name)) {
		return nil, api.ErrBotNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, api.ErrBotNotFound
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, api.ErrBotNotFound
	}
	a := &api.BotArtifact{Name: name, Kind: kind, Size: fi.Size(), ModTime: fi.ModTime(), Content: f}
	switch kind {
	case api.ArtifactRun, api.ArtifactEpisode:
		a.ContentType = "application/json"
	case api.ArtifactTrace:
		a.ContentType, a.ContentEncoding = "application/x-ndjson", "gzip"
	default:
		a.ContentType = "application/octet-stream"
	}
	return a, nil
}
