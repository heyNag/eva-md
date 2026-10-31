package hook

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/heyNag/eva-md/internal/catchup"
	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/inbox"
	"github.com/heyNag/eva-md/internal/resume"
)

// The thresholds of the update triggers (docs/design.md "Triggers").
const (
	stopBacklog     = 60 << 10         // Stop: the bytes yet to process that start an update
	promptGap       = 30 * time.Minute // a prompt this long after the session's file last changed...
	promptBacklog   = 600              // ...starts an update for this many bytes yet to process
	idleAfter       = 2 * time.Minute  // SessionStart: sessions whose file is unchanged this long...
	catchUpSessions = 5                // ...and holds a backlog, at most this many
)

// updatesOn reports whether the hooks start updates: updates = true in the
// config, and a wiki set up, without which a job could only fail.
func updatesOn(c config.Config) bool { return c.Updates && c.WikiPath != "" }

// queueUpdate asks for a detached eva-md update of sessions, each
// "<host>:<id>", unless updates are off.
func queueUpdate(spawns *[][]string, env Env, sessions ...string) {
	if !updatesOn(env.Config) || len(sessions) == 0 {
		return
	}
	args := []string{"update"}
	for _, s := range sessions {
		args = append(args, "--session", s)
	}
	*spawns = append(*spawns, args)
}

// pending is how many bytes of a session file size bytes long the update
// has yet to process, by its watermark. A watermark that can't be read
// counts as none, so a hook never spawns on it.
func pending(store inbox.Store, file string, size int64) int64 {
	m, err := store.Mark(file)
	if err != nil {
		return 0
	}
	return m.Pending(size)
}

// stopBacklogged reports whether the session's file holds 60 KB or more
// that the update has yet to process, the trigger for chats that never
// end. It costs one stat; the watermark is read only once the file is that
// large.
func stopBacklogged(store inbox.Store, file string) bool {
	info, err := os.Lstat(file)
	if err != nil || info.Size() < stopBacklog {
		return false
	}
	return pending(store, file, info.Size()) >= stopBacklog
}

// gapBacklogged reports whether a prompt comes 30 minutes or more after the
// session's file last changed, with 600 bytes or more that the update has
// yet to process: the burst before the gap counts as a finished chat.
// before is the file as one stat found it before the hook appended
// anything; the watermark is read only when the gap and the size allow.
func gapBacklogged(store inbox.Store, file string, before os.FileInfo, now time.Time) bool {
	if before == nil || now.Sub(before.ModTime()) < promptGap || before.Size() < promptBacklog {
		return false
	}
	return pending(store, file, before.Size()) >= promptBacklog
}

// startState is what capture found at SessionStart before it appended
// anything: this chat's own file as one stat found it, nil when it had
// none, and the sessions start-of-chat transcript recovery appended to.
type startState struct {
	before    os.FileInfo
	recovered []catchup.Recovered
}

// files describes this chat's own file as it was before the hook, its Mod
// zero when it had none, and names the session files recovery appended to.
func (s startState) files(store inbox.Store, key, host, id string) (resume.SessionFile, map[string]bool) {
	file := store.Path(key, host, id)
	own := resume.SessionFile{Name: filepath.Base(file), Path: file, Host: host, ID: id}
	if s.before != nil {
		own.Mod, own.Size = s.before.ModTime(), s.before.Size()
	}
	recovered := map[string]bool{}
	for _, r := range s.recovered {
		recovered[filepath.Base(store.Path(key, r.Host, r.Session))] = true
	}
	return own, recovered
}

// idleBacklogs picks the sessions the SessionStart catch-up updates, at most
// 5, each as "<host>:<id>". This chat's own comes first, when its file, as
// own describes it before the hook appended anything, had not changed for 2
// minutes or more and holds a backlog: a chat left idle and now resumed,
// whose next prompt finds the file this start has just changed. Then, from
// the session files the brief read, newest first, the project's other
// sessions that hold a backlog and whose file has not changed for 2 minutes
// or more, or that start-of-chat transcript recovery appended to, which it
// does only to sessions idle that long. It lists no folder, and reads one
// watermark for each idle file it considers.
func idleBacklogs(ctx context.Context, store inbox.Store, own resume.SessionFile, files []resume.SessionFile, recovered map[string]bool, now time.Time) []string {
	var out []string
	if !own.Mod.IsZero() && now.Sub(own.Mod) >= idleAfter && pending(store, own.Path, own.Size) > 0 {
		out = append(out, own.Host+":"+own.ID)
	}
	for _, f := range files {
		if len(out) == catchUpSessions || ctx.Err() != nil {
			break
		}
		if f.Name == own.Name || now.Sub(f.Mod) < idleAfter && !recovered[f.Name] {
			continue
		}
		if pending(store, f.Path, f.Size) > 0 {
			out = append(out, f.Host+":"+f.ID)
		}
	}
	return out
}
