package hook

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/heyNag/eva-md/internal/config"
	"github.com/heyNag/eva-md/internal/health"
	"github.com/heyNag/eva-md/internal/paths"
)

// The jobs maintain runs as: the day's commit, push, lint and search index,
// and the retry of a pending push.
var (
	dailyJob = []string{"maintain"}
	retryJob = []string{"maintain", "--push-only"}
)

// retryEvery spaces the push retries chat starts spawn.
const retryEvery = 30 * time.Minute

// wikiOn reports whether a wiki is set up by init: its path and instance
// in the config.
func wikiOn(c config.Config) bool { return c.WikiPath != "" && c.Instance != "" }

// maintainOn reports whether the hooks start the day's maintain: with a
// wiki set up by init, and with no wiki at all, for maintain's search index
// step; not with a wiki path but no instance, a wiki whose commit
// maintain could only refuse.
func maintainOn(c config.Config) bool { return wikiOn(c) || c.WikiPath == "" }

// queueMaintain asks for the day's maintain, and at a chat start for a push
// retry while commits are on and a wiki is set up. Asking costs the hook
// nothing here: launch decides, once the output is written, whether each is
// due.
func queueMaintain(spawns *[][]string, env Env, start bool) {
	if !maintainOn(env.Config) {
		return
	}
	*spawns = append(*spawns, dailyJob)
	if start && env.Config.Commits && wikiOn(env.Config) {
		*spawns = append(*spawns, retryJob)
	}
}

// due reports whether launch starts args now. An update always is. The
// day's maintain is due for the day's first event, the one whose open
// creates <state>/commit/attempt-<YYYY-MM-DD>; every later event that day
// finds the file and starts nothing. A push retry is due when no maintain
// started with it and retryDue says so. started records whether this hook
// started the day's maintain.
func due(env Env, args []string, now time.Time, started *bool) bool {
	switch {
	case slices.Equal(args, dailyJob):
		*started = firstOfDay(env.Dirs.State, now)
		return *started
	case slices.Equal(args, retryJob):
		return !*started && retryDue(env.Dirs, now)
	}
	return true
}

// firstOfDay creates the day's attempt marker, and reports whether this
// call created it. Each event costs one stat, which finds the marker after
// the day's first event; that one creates it with O_EXCL in <state>/commit,
// which commitRoot opens, and makes when missing, through no link.
func firstOfDay(state string, now time.Time) bool {
	name := "attempt-" + now.Format(time.DateOnly)
	if _, err := os.Lstat(filepath.Join(state, "commit", name)); err == nil {
		return false
	}
	root, err := commitRoot(state)
	if err != nil {
		return false
	}
	defer root.Close()
	return create(root, name, os.O_EXCL) == nil
}

// retryDue reports whether a push retry is due: the newest commit record's
// push pending, as a check finds it at most every 30 minutes, by the
// modification time of <state>/commit/push-retry, which every check sets to
// now, due or not. Between checks a chat start costs one stat; a check adds
// a read of the newest commit records.
func retryDue(dirs paths.Dirs, now time.Time) bool {
	const marker = "push-retry"
	if info, err := os.Lstat(filepath.Join(dirs.State, "commit", marker)); err == nil {
		if age := now.Sub(info.ModTime()); age >= 0 && age < retryEvery {
			return false
		}
	}
	pending := health.RetryPush(dirs, now)
	root, err := commitRoot(dirs.State)
	if err != nil {
		return false
	}
	defer root.Close()
	if create(root, marker, 0) != nil || root.Chtimes(marker, now, now) != nil {
		return false
	}
	return pending
}

// commitRoot opens <state>/commit, making it if missing, as every state
// folder is opened: through no link in any part of its path but macOS's
// /var, /tmp and /etc aliases (paths.OpenRoot). Under a home that is
// itself a symlink, where eva-md's state folder is refused, the hooks then
// write no marker and start no maintain.
func commitRoot(state string) (*os.Root, error) {
	return paths.OpenRoot(filepath.Join(state, "commit"), true)
}

// create opens name in root for writing, creating it, and closes it. No
// symlink leads it out of root, and with O_EXCL none is followed.
func create(root *os.Root, name string, flag int) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|syscall.O_NOFOLLOW|flag, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}
